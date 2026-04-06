package bus

import (
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// busFactory creates a Bus for testing and a cleanup function.
type busFactory func(t *testing.T) (Bus, func())

func channelFactory(t *testing.T) (Bus, func()) {
	b := NewChannelBus()
	return b, func() { b.Close() }
}

func natsFactory(t *testing.T) (Bus, func()) {
	// Start an embedded NATS server with JetStream
	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // random port
		NoLog:     true,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create nats server: %v", err)
	}
	ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}

	nb, err := NewNATSBus(nc)
	if err != nil {
		t.Fatalf("failed to create NATS bus: %v", err)
	}

	return nb, func() {
		nb.Close()
		ns.Shutdown()
	}
}

// runConformance runs the full test suite against a Bus implementation.
func runConformance(t *testing.T, name string, factory busFactory) {
	t.Run(name+"/PubSub", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		received := make(chan string, 1)
		_, err := b.Subscribe("test.topic", func(subject string, data []byte, reply ReplyFunc) {
			received <- string(data)
		})
		if err != nil {
			t.Fatalf("subscribe error: %v", err)
		}

		time.Sleep(50 * time.Millisecond) // let subscription settle
		if err := b.Publish("test.topic", []byte("hello")); err != nil {
			t.Fatalf("publish error: %v", err)
		}

		select {
		case msg := <-received:
			if msg != "hello" {
				t.Errorf("got %q, want %q", msg, "hello")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run(name+"/WildcardStar", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		received := make(chan string, 2)
		_, err := b.Subscribe("foo.*", func(subject string, data []byte, reply ReplyFunc) {
			received <- subject
		})
		if err != nil {
			t.Fatalf("subscribe error: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		b.Publish("foo.bar", []byte("match"))
		b.Publish("foo.bar.baz", []byte("no-match"))

		select {
		case subj := <-received:
			if subj != "foo.bar" {
				t.Errorf("got %q, want %q", subj, "foo.bar")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for foo.bar")
		}

		// Ensure foo.bar.baz did NOT match
		select {
		case subj := <-received:
			t.Errorf("should not have received %q", subj)
		case <-time.After(200 * time.Millisecond):
			// good — no extra message
		}
	})

	t.Run(name+"/WildcardGT", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		received := make(chan string, 3)
		_, err := b.Subscribe("foo.>", func(subject string, data []byte, reply ReplyFunc) {
			received <- subject
		})
		if err != nil {
			t.Fatalf("subscribe error: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		b.Publish("foo.bar", []byte("1"))
		b.Publish("foo.bar.baz", []byte("2"))
		b.Publish("foo", []byte("no-match")) // > requires at least one token after

		count := 0
		timeout := time.After(2 * time.Second)
		for count < 2 {
			select {
			case <-received:
				count++
			case <-timeout:
				t.Fatalf("only received %d messages, want 2", count)
			}
		}

		// Ensure "foo" alone did NOT match
		select {
		case subj := <-received:
			if subj == "foo" {
				t.Error("foo should not match foo.>")
			}
		case <-time.After(200 * time.Millisecond):
			// good
		}
	})

	t.Run(name+"/RequestReply", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		_, err := b.Subscribe("rpc.echo", func(subject string, data []byte, reply ReplyFunc) {
			if reply != nil {
				reply(append([]byte("echo:"), data...))
			}
		})
		if err != nil {
			t.Fatalf("subscribe error: %v", err)
		}
		time.Sleep(50 * time.Millisecond)

		resp, err := b.Request("rpc.echo", []byte("ping"), 2*time.Second)
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		if string(resp) != "echo:ping" {
			t.Errorf("got %q, want %q", string(resp), "echo:ping")
		}
	})

	t.Run(name+"/RequestTimeout", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		_, err := b.Request("no.responder", []byte("ping"), 200*time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
	})

	t.Run(name+"/KVPutGet", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		if err := b.KVCreate("test-bucket", KVBucketConfig{History: 1}); err != nil {
			t.Fatalf("create error: %v", err)
		}

		rev, err := b.KVPut("test-bucket", "key1", []byte("value1"))
		if err != nil {
			t.Fatalf("put error: %v", err)
		}
		if rev == 0 {
			t.Error("expected non-zero revision")
		}

		val, gotRev, err := b.KVGet("test-bucket", "key1")
		if err != nil {
			t.Fatalf("get error: %v", err)
		}
		if string(val) != "value1" {
			t.Errorf("got %q, want %q", string(val), "value1")
		}
		if gotRev != rev {
			t.Errorf("revision mismatch: got %d, want %d", gotRev, rev)
		}
	})

	t.Run(name+"/KVDelete", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		b.KVCreate("del-bucket", KVBucketConfig{History: 1})
		b.KVPut("del-bucket", "key1", []byte("value1"))

		if err := b.KVDelete("del-bucket", "key1"); err != nil {
			t.Fatalf("delete error: %v", err)
		}

		_, _, err := b.KVGet("del-bucket", "key1")
		if err == nil {
			t.Fatal("expected error after delete, got nil")
		}
	})

	t.Run(name+"/KVWatch", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		b.KVCreate("watch-bucket", KVBucketConfig{History: 1})

		received := make(chan string, 2)
		sub, err := b.KVWatch("watch-bucket", "mykey", func(key string, value []byte, op KVOperation) {
			if op == KVOpPut {
				received <- string(value)
			}
		})
		if err != nil {
			t.Fatalf("watch error: %v", err)
		}
		defer sub.Unsubscribe()
		time.Sleep(100 * time.Millisecond)

		b.KVPut("watch-bucket", "mykey", []byte("v1"))
		b.KVPut("watch-bucket", "otherkey", []byte("v2")) // should not trigger

		select {
		case val := <-received:
			if val != "v1" {
				t.Errorf("got %q, want %q", val, "v1")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for watch")
		}

		// Verify otherkey didn't trigger
		select {
		case val := <-received:
			t.Errorf("should not have received %q", val)
		case <-time.After(200 * time.Millisecond):
			// good
		}
	})

	t.Run(name+"/KVWatchAll", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		b.KVCreate("watchall-bucket", KVBucketConfig{History: 1})

		received := make(chan string, 3)
		sub, err := b.KVWatchAll("watchall-bucket", func(key string, value []byte, op KVOperation) {
			if op == KVOpPut {
				received <- key + "=" + string(value)
			}
		})
		if err != nil {
			t.Fatalf("watchall error: %v", err)
		}
		defer sub.Unsubscribe()
		time.Sleep(100 * time.Millisecond)

		b.KVPut("watchall-bucket", "a", []byte("1"))
		b.KVPut("watchall-bucket", "b", []byte("2"))

		count := 0
		timeout := time.After(2 * time.Second)
		for count < 2 {
			select {
			case <-received:
				count++
			case <-timeout:
				t.Fatalf("only received %d watch events, want 2", count)
			}
		}
	})

	t.Run(name+"/KVKeys", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		b.KVCreate("keys-bucket", KVBucketConfig{History: 1})
		b.KVPut("keys-bucket", "a", []byte("1"))
		b.KVPut("keys-bucket", "b", []byte("2"))

		keys, err := b.KVKeys("keys-bucket")
		if err != nil {
			t.Fatalf("keys error: %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("got %d keys, want 2", len(keys))
		}
	})

	t.Run(name+"/QueueSubscribe", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		count1 := make(chan struct{}, 10)
		count2 := make(chan struct{}, 10)

		b.QueueSubscribe("work.>", "workers", func(subject string, data []byte, reply ReplyFunc) {
			count1 <- struct{}{}
		})
		b.QueueSubscribe("work.>", "workers", func(subject string, data []byte, reply ReplyFunc) {
			count2 <- struct{}{}
		})
		time.Sleep(50 * time.Millisecond)

		// Publish several messages
		for i := 0; i < 4; i++ {
			b.Publish("work.item", []byte("job"))
		}

		time.Sleep(500 * time.Millisecond)
		total := len(count1) + len(count2)
		if total != 4 {
			t.Errorf("got %d total deliveries, want 4", total)
		}
	})

	t.Run(name+"/Unsubscribe", func(t *testing.T) {
		b, cleanup := factory(t)
		defer cleanup()

		received := make(chan struct{}, 2)
		sub, _ := b.Subscribe("unsub.test", func(subject string, data []byte, reply ReplyFunc) {
			received <- struct{}{}
		})
		time.Sleep(50 * time.Millisecond)

		b.Publish("unsub.test", []byte("1"))
		select {
		case <-received:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout before unsubscribe")
		}

		sub.Unsubscribe()
		time.Sleep(50 * time.Millisecond)

		b.Publish("unsub.test", []byte("2"))
		select {
		case <-received:
			t.Error("received message after unsubscribe")
		case <-time.After(200 * time.Millisecond):
			// good
		}
	})
}

func TestChannelBus(t *testing.T) {
	runConformance(t, "Channel", channelFactory)
}

func TestNATSBus(t *testing.T) {
	runConformance(t, "NATS", natsFactory)
}

func TestMatchSubject(t *testing.T) {
	tests := []struct {
		pattern string
		subject string
		want    bool
	}{
		{"foo", "foo", true},
		{"foo", "bar", false},
		{"foo.bar", "foo.bar", true},
		{"foo.bar", "foo.baz", false},
		{"foo.*", "foo.bar", true},
		{"foo.*", "foo.bar.baz", false},
		{"foo.>", "foo.bar", true},
		{"foo.>", "foo.bar.baz", true},
		{"foo.>", "foo", false},
		{"*.bar", "foo.bar", true},
		{"*.bar", "foo.baz", false},
		{"*.*", "foo.bar", true},
		{"*.*", "foo.bar.baz", false},
		{"foo.*.baz", "foo.bar.baz", true},
		{"foo.*.baz", "foo.bar.qux", false},
	}

	for _, tt := range tests {
		got := matchSubject(tt.pattern, tt.subject)
		if got != tt.want {
			t.Errorf("matchSubject(%q, %q) = %v, want %v", tt.pattern, tt.subject, got, tt.want)
		}
	}
}
