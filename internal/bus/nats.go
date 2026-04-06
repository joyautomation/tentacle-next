package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSBus implements Bus using NATS for pub/sub and JetStream for KV.
// Used when running as standalone microservices.
type NATSBus struct {
	nc      *nats.Conn
	js      jetstream.JetStream
	kvCache sync.Map // bucket name -> jetstream.KeyValue
}

// NewNATSBus creates a Bus backed by a NATS connection.
// The connection must already be established.
func NewNATSBus(nc *nats.Conn) (*NATSBus, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}
	return &NATSBus{nc: nc, js: js}, nil
}

// ConnectNATSBus creates a new NATS connection with standard tentacle options
// and returns a NATSBus. Retries forever on failure.
func ConnectNATSBus(servers string) (*NATSBus, error) {
	nc, err := nats.Connect(servers,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(5*time.Second),
	)
	if err != nil {
		return nil, err
	}
	return NewNATSBus(nc)
}

func (nb *NATSBus) Publish(subject string, data []byte) error {
	return nb.nc.Publish(subject, data)
}

func (nb *NATSBus) Subscribe(subject string, handler MessageHandler) (Subscription, error) {
	sub, err := nb.nc.Subscribe(subject, func(msg *nats.Msg) {
		var reply ReplyFunc
		if msg.Reply != "" {
			reply = func(data []byte) error {
				return msg.Respond(data)
			}
		}
		handler(msg.Subject, msg.Data, reply)
	})
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

func (nb *NATSBus) QueueSubscribe(subject, queue string, handler MessageHandler) (Subscription, error) {
	sub, err := nb.nc.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		var reply ReplyFunc
		if msg.Reply != "" {
			reply = func(data []byte) error {
				return msg.Respond(data)
			}
		}
		handler(msg.Subject, msg.Data, reply)
	})
	if err != nil {
		return nil, err
	}
	return &natsSubscription{sub: sub}, nil
}

func (nb *NATSBus) Request(subject string, data []byte, timeout time.Duration) ([]byte, error) {
	msg, err := nb.nc.Request(subject, data, timeout)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

// ─── KV Operations ──────────────────────────────────────────────────────────

func (nb *NATSBus) KVCreate(bucket string, config KVBucketConfig) error {
	history := config.History
	if history == 0 {
		history = 1
	}
	kvConfig := jetstream.KeyValueConfig{
		Bucket:  bucket,
		History: uint8(history),
		TTL:     config.TTL,
	}
	if config.MaxBytes > 0 {
		kvConfig.MaxBytes = config.MaxBytes
	}
	kv, err := nb.js.CreateOrUpdateKeyValue(context.Background(), kvConfig)
	if err != nil {
		return err
	}
	nb.kvCache.Store(bucket, kv)
	return nil
}

func (nb *NATSBus) KVGet(bucket, key string) ([]byte, uint64, error) {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return nil, 0, err
	}
	entry, err := kv.Get(context.Background(), key)
	if err != nil {
		return nil, 0, err
	}
	return entry.Value(), entry.Revision(), nil
}

func (nb *NATSBus) KVPut(bucket, key string, value []byte) (uint64, error) {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return 0, err
	}
	rev, err := kv.Put(context.Background(), key, value)
	if err != nil {
		return 0, err
	}
	return rev, nil
}

func (nb *NATSBus) KVDelete(bucket, key string) error {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return err
	}
	return kv.Delete(context.Background(), key)
}

func (nb *NATSBus) KVKeys(bucket string) ([]string, error) {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return nil, err
	}
	keys, err := kv.Keys(context.Background())
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (nb *NATSBus) KVWatch(bucket, key string, handler KVWatchHandler) (Subscription, error) {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return nil, err
	}

	watcher, err := kv.Watch(context.Background(), key)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					continue // initial nil sentinel
				}
				op := KVOpPut
				if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
					op = KVOpDelete
				}
				handler(entry.Key(), entry.Value(), op)
			}
		}
	}()

	return &natsKVWatchSubscription{watcher: watcher, done: done}, nil
}

func (nb *NATSBus) KVWatchAll(bucket string, handler KVWatchHandler) (Subscription, error) {
	kv, err := nb.getKV(bucket)
	if err != nil {
		return nil, err
	}

	watcher, err := kv.WatchAll(context.Background())
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					continue
				}
				op := KVOpPut
				if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
					op = KVOpDelete
				}
				handler(entry.Key(), entry.Value(), op)
			}
		}
	}()

	return &natsKVWatchSubscription{watcher: watcher, done: done}, nil
}

func (nb *NATSBus) Close() error {
	nb.nc.Close()
	return nil
}

// ─── Internal ───────────────────────────────────────────────────────────────

func (nb *NATSBus) getKV(bucket string) (jetstream.KeyValue, error) {
	if v, ok := nb.kvCache.Load(bucket); ok {
		return v.(jetstream.KeyValue), nil
	}
	// Try to get existing bucket
	kv, err := nb.js.KeyValue(context.Background(), bucket)
	if err != nil {
		return nil, errors.New("bucket not found: " + bucket)
	}
	nb.kvCache.Store(bucket, kv)
	return kv, nil
}

// ─── Subscription types ─────────────────────────────────────────────────────

type natsSubscription struct {
	sub *nats.Subscription
}

func (s *natsSubscription) Unsubscribe() error {
	return s.sub.Unsubscribe()
}

type natsKVWatchSubscription struct {
	watcher jetstream.KeyWatcher
	done    chan struct{}
}

func (s *natsKVWatchSubscription) Unsubscribe() error {
	close(s.done)
	return s.watcher.Stop()
}
