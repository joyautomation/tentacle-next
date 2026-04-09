package bus

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ChannelBus implements Bus using Go channels and in-memory maps.
// Used when running in monolith mode (all modules in one process).
type ChannelBus struct {
	mu      sync.RWMutex
	subs    map[string][]*channelSub // subject pattern -> subscriptions
	kvMu    sync.RWMutex
	kv      map[string]*kvBucket // bucket name -> bucket
	done    chan struct{}
	dataDir string // empty = no persistence
}

type channelSub struct {
	id      string
	pattern string
	queue   string // empty for non-queue subs
	handler MessageHandler
	active  atomic.Bool
}

type kvBucket struct {
	mu       sync.RWMutex
	name     string
	config   KVBucketConfig
	entries  map[string]*kvEntry
	revision uint64
	watchers []*kvWatcher
	persist  bool   // true if this bucket should be saved to disk
	dataDir  string // parent data directory
}

type kvEntry struct {
	value     []byte
	revision  uint64
	expiresAt time.Time // zero means no expiration
}

type kvWatcher struct {
	key     string // empty = watch all
	handler KVWatchHandler
	active  atomic.Bool
}

// kvSnapshot is the on-disk format for a persisted bucket.
type kvSnapshot struct {
	Entries map[string][]byte `json:"entries"`
}

// NewChannelBus creates a new in-process Bus implementation.
// dataDir is optional; if non-empty, non-TTL KV buckets are persisted to disk.
func NewChannelBus(dataDir ...string) *ChannelBus {
	dir := ""
	if len(dataDir) > 0 && dataDir[0] != "" {
		dir = dataDir[0]
	}
	cb := &ChannelBus{
		subs:    make(map[string][]*channelSub),
		kv:      make(map[string]*kvBucket),
		done:    make(chan struct{}),
		dataDir: dir,
	}
	go cb.ttlLoop()
	return cb
}

func (cb *ChannelBus) Publish(subject string, data []byte) error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Find matching queue groups and non-queue subs
	queueGroups := make(map[string][]*channelSub)
	var directSubs []*channelSub

	for pattern, subs := range cb.subs {
		if !matchSubject(pattern, subject) {
			continue
		}
		for _, s := range subs {
			if !s.active.Load() {
				continue
			}
			if s.queue != "" {
				queueGroups[s.queue] = append(queueGroups[s.queue], s)
			} else {
				directSubs = append(directSubs, s)
			}
		}
	}

	// Deliver to all direct subscribers
	dataCopy := copyBytes(data)
	for _, s := range directSubs {
		go s.handler(subject, dataCopy, nil)
	}

	// Deliver to one subscriber per queue group (round-robin is fine; pick first)
	for _, group := range queueGroups {
		if len(group) > 0 {
			go group[0].handler(subject, dataCopy, nil)
		}
	}

	return nil
}

func (cb *ChannelBus) Subscribe(subject string, handler MessageHandler) (Subscription, error) {
	return cb.subscribe(subject, "", handler)
}

func (cb *ChannelBus) QueueSubscribe(subject, queue string, handler MessageHandler) (Subscription, error) {
	return cb.subscribe(subject, queue, handler)
}

func (cb *ChannelBus) subscribe(subject, queue string, handler MessageHandler) (Subscription, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	s := &channelSub{
		id:      uuid.NewString(),
		pattern: subject,
		queue:   queue,
		handler: handler,
	}
	s.active.Store(true)
	cb.subs[subject] = append(cb.subs[subject], s)

	return &channelSubscription{bus: cb, sub: s}, nil
}

func (cb *ChannelBus) Request(subject string, data []byte, timeout time.Duration) ([]byte, error) {
	replyCh := make(chan []byte, 1)
	replySubject := "_INBOX." + uuid.NewString()

	// Subscribe to the reply subject
	sub, err := cb.Subscribe(replySubject, func(_ string, data []byte, _ ReplyFunc) {
		replyCh <- copyBytes(data)
	})
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()

	// Build the reply function that targets our inbox
	replyFn := func(respData []byte) error {
		return cb.Publish(replySubject, respData)
	}

	// Deliver to matching subscribers with the reply function
	cb.mu.RLock()
	var matched []*channelSub
	for pattern, subs := range cb.subs {
		if pattern == replySubject {
			continue // skip our own reply sub
		}
		if !matchSubject(pattern, subject) {
			continue
		}
		for _, s := range subs {
			if s.active.Load() {
				matched = append(matched, s)
			}
		}
	}
	cb.mu.RUnlock()

	if len(matched) == 0 {
		return nil, errors.New("no responders")
	}

	dataCopy := copyBytes(data)
	for _, s := range matched {
		go s.handler(subject, dataCopy, replyFn)
	}

	select {
	case resp := <-replyCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, errors.New("request timeout")
	}
}

// ─── KV Operations ──────────────────────────────────────────────────────────

func (cb *ChannelBus) KVCreate(bucket string, config KVBucketConfig) error {
	cb.kvMu.Lock()
	defer cb.kvMu.Unlock()

	if _, exists := cb.kv[bucket]; !exists {
		shouldPersist := cb.dataDir != "" && config.TTL == 0
		b := &kvBucket{
			name:    bucket,
			config:  config,
			entries: make(map[string]*kvEntry),
			persist: shouldPersist,
			dataDir: cb.dataDir,
		}
		cb.kv[bucket] = b

		// Load persisted data if available
		if shouldPersist {
			b.load()
		}
	}
	return nil
}

func (cb *ChannelBus) KVGet(bucket, key string) ([]byte, uint64, error) {
	b, err := cb.getBucket(bucket)
	if err != nil {
		return nil, 0, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	entry, ok := b.entries[key]
	if !ok {
		return nil, 0, errors.New("key not found")
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return nil, 0, errors.New("key not found")
	}
	return copyBytes(entry.value), entry.revision, nil
}

func (cb *ChannelBus) KVPut(bucket, key string, value []byte) (uint64, error) {
	b, err := cb.getBucket(bucket)
	if err != nil {
		return 0, err
	}
	b.mu.Lock()
	b.revision++
	rev := b.revision

	var expiresAt time.Time
	if b.config.TTL > 0 {
		expiresAt = time.Now().Add(b.config.TTL)
	}

	b.entries[key] = &kvEntry{
		value:     copyBytes(value),
		revision:  rev,
		expiresAt: expiresAt,
	}

	// Snapshot watchers while locked
	watchers := make([]*kvWatcher, len(b.watchers))
	copy(watchers, b.watchers)
	b.mu.Unlock()

	// Persist to disk (outside lock)
	if b.persist {
		b.save()
	}

	// Notify watchers outside the lock
	for _, w := range watchers {
		if w.active.Load() && (w.key == "" || w.key == key) {
			go w.handler(key, copyBytes(value), KVOpPut)
		}
	}

	return rev, nil
}

func (cb *ChannelBus) KVDelete(bucket, key string) error {
	b, err := cb.getBucket(bucket)
	if err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.entries, key)
	watchers := make([]*kvWatcher, len(b.watchers))
	copy(watchers, b.watchers)
	b.mu.Unlock()

	// Persist to disk
	if b.persist {
		b.save()
	}

	for _, w := range watchers {
		if w.active.Load() && (w.key == "" || w.key == key) {
			go w.handler(key, nil, KVOpDelete)
		}
	}
	return nil
}

func (cb *ChannelBus) KVKeys(bucket string) ([]string, error) {
	b, err := cb.getBucket(bucket)
	if err != nil {
		return nil, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	now := time.Now()
	keys := make([]string, 0, len(b.entries))
	for k, entry := range b.entries {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			continue
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (cb *ChannelBus) KVWatch(bucket, key string, handler KVWatchHandler) (Subscription, error) {
	b, err := cb.getBucket(bucket)
	if err != nil {
		return nil, err
	}
	w := &kvWatcher{key: key, handler: handler}
	w.active.Store(true)

	b.mu.Lock()
	b.watchers = append(b.watchers, w)

	// Replay existing entries to match NATS JetStream KV watch behavior.
	// This ensures watchers see current state even if entries were written
	// before the watch was established (critical for loose coupling where
	// producers and consumers start independently).
	now := time.Now()
	existing := make([]struct {
		key   string
		value []byte
	}, 0, len(b.entries))
	for k, entry := range b.entries {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			continue
		}
		if key == "" || key == k {
			existing = append(existing, struct {
				key   string
				value []byte
			}{k, copyBytes(entry.value)})
		}
	}
	b.mu.Unlock()

	// Fire replay callbacks outside the lock
	for _, e := range existing {
		if w.active.Load() {
			go handler(e.key, e.value, KVOpPut)
		}
	}

	return &kvWatchSubscription{bucket: b, watcher: w}, nil
}

func (cb *ChannelBus) KVWatchAll(bucket string, handler KVWatchHandler) (Subscription, error) {
	return cb.KVWatch(bucket, "", handler)
}

func (cb *ChannelBus) Close() error {
	close(cb.done)
	return nil
}

// ─── Bucket Persistence ─────────────────────────────────────────────────────

func (b *kvBucket) filePath() string {
	return filepath.Join(b.dataDir, b.name+".json")
}

// load reads persisted entries from disk into the bucket.
func (b *kvBucket) load() {
	data, err := os.ReadFile(b.filePath())
	if err != nil {
		return // file doesn't exist yet — normal on first run
	}
	var snap kvSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return // corrupt file — start fresh
	}
	for k, v := range snap.Entries {
		b.revision++
		b.entries[k] = &kvEntry{
			value:    v,
			revision: b.revision,
		}
	}
}

// save writes all bucket entries to disk atomically.
func (b *kvBucket) save() {
	b.mu.RLock()
	snap := kvSnapshot{Entries: make(map[string][]byte, len(b.entries))}
	for k, entry := range b.entries {
		if entry.expiresAt.IsZero() || time.Now().Before(entry.expiresAt) {
			snap.Entries[k] = entry.value
		}
	}
	b.mu.RUnlock()

	data, err := json.Marshal(snap)
	if err != nil {
		return
	}

	// Ensure directory exists
	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		return
	}

	// Atomic write: write to temp file, then rename
	tmp := b.filePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, b.filePath())
}

// ─── Internal ───────────────────────────────────────────────────────────────

func (cb *ChannelBus) getBucket(name string) (*kvBucket, error) {
	cb.kvMu.RLock()
	defer cb.kvMu.RUnlock()
	b, ok := cb.kv[name]
	if !ok {
		return nil, errors.New("bucket not found: " + name)
	}
	return b, nil
}

func (cb *ChannelBus) removeSub(sub *channelSub) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	sub.active.Store(false)
	subs := cb.subs[sub.pattern]
	for i, s := range subs {
		if s.id == sub.id {
			cb.subs[sub.pattern] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// ttlLoop expires KV entries every second.
func (cb *ChannelBus) ttlLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cb.done:
			return
		case <-ticker.C:
			cb.expireTTL()
		}
	}
}

func (cb *ChannelBus) expireTTL() {
	cb.kvMu.RLock()
	buckets := make([]*kvBucket, 0, len(cb.kv))
	for _, b := range cb.kv {
		if b.config.TTL > 0 {
			buckets = append(buckets, b)
		}
	}
	cb.kvMu.RUnlock()

	now := time.Now()
	for _, b := range buckets {
		b.mu.Lock()
		var expired []string
		for k, entry := range b.entries {
			if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
				expired = append(expired, k)
			}
		}
		for _, k := range expired {
			delete(b.entries, k)
		}
		watchers := make([]*kvWatcher, len(b.watchers))
		copy(watchers, b.watchers)
		b.mu.Unlock()

		// Notify watchers of deletions
		for _, k := range expired {
			for _, w := range watchers {
				if w.active.Load() && (w.key == "" || w.key == k) {
					go w.handler(k, nil, KVOpDelete)
				}
			}
		}
	}
}

// matchSubject matches a NATS-style subject pattern against a subject.
// * matches exactly one token, > matches one or more trailing tokens.
func matchSubject(pattern, subject string) bool {
	patternParts := strings.Split(pattern, ".")
	subjectParts := strings.Split(subject, ".")

	for i, pp := range patternParts {
		if pp == ">" {
			return i < len(subjectParts) // > must match at least one token
		}
		if i >= len(subjectParts) {
			return false
		}
		if pp != "*" && pp != subjectParts[i] {
			return false
		}
	}
	return len(patternParts) == len(subjectParts)
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// ─── Subscription types ─────────────────────────────────────────────────────

type channelSubscription struct {
	bus *ChannelBus
	sub *channelSub
}

func (s *channelSubscription) Unsubscribe() error {
	s.bus.removeSub(s.sub)
	return nil
}

type kvWatchSubscription struct {
	bucket  *kvBucket
	watcher *kvWatcher
}

func (s *kvWatchSubscription) Unsubscribe() error {
	s.watcher.active.Store(false)
	s.bucket.mu.Lock()
	defer s.bucket.mu.Unlock()
	for i, w := range s.bucket.watchers {
		if w == s.watcher {
			s.bucket.watchers = append(s.bucket.watchers[:i], s.bucket.watchers[i+1:]...)
			break
		}
	}
	return nil
}
