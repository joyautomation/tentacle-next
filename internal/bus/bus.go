// Package bus defines the inter-module communication abstraction.
// All tentacle modules communicate through a Bus, which can be backed
// by Go channels (monolith mode) or NATS (distributed mode).
package bus

import "time"

// Bus is the inter-module communication interface.
// Modules receive a Bus at startup and never import transport-specific packages.
type Bus interface {
	// Publish sends data on a subject. Fire-and-forget.
	Publish(subject string, data []byte) error

	// Subscribe registers a handler for messages on a subject pattern.
	// Supports NATS-style wildcards: * matches one token, > matches remainder.
	Subscribe(subject string, handler MessageHandler) (Subscription, error)

	// Request sends data and waits for a single reply within timeout.
	Request(subject string, data []byte, timeout time.Duration) ([]byte, error)

	// QueueSubscribe is like Subscribe but with load-balancing across a named group.
	QueueSubscribe(subject, queue string, handler MessageHandler) (Subscription, error)

	// KVGet retrieves a value from a KV bucket. Returns value, revision, error.
	KVGet(bucket, key string) ([]byte, uint64, error)

	// KVPut stores a value in a KV bucket. Returns revision, error.
	KVPut(bucket, key string, value []byte) (uint64, error)

	// KVDelete removes a key from a KV bucket.
	KVDelete(bucket, key string) error

	// KVKeys returns all keys in a KV bucket.
	KVKeys(bucket string) ([]string, error)

	// KVWatch watches a specific key for changes.
	KVWatch(bucket, key string, handler KVWatchHandler) (Subscription, error)

	// KVWatchAll watches all keys in a bucket for changes.
	KVWatchAll(bucket string, handler KVWatchHandler) (Subscription, error)

	// KVCreate creates a KV bucket with the given configuration.
	// If the bucket already exists with compatible settings, this is a no-op.
	KVCreate(bucket string, config KVBucketConfig) error

	// Close shuts down the bus and releases all resources.
	Close() error
}

// MessageHandler processes a message delivered via Subscribe or QueueSubscribe.
// reply is non-nil only for request/reply messages; call it to send the response.
type MessageHandler func(subject string, data []byte, reply ReplyFunc)

// ReplyFunc sends a response to a Request() call.
// It is nil for non-request messages.
type ReplyFunc func(data []byte) error

// KVWatchHandler is called when a watched key changes.
type KVWatchHandler func(key string, value []byte, op KVOperation)

// KVBucketConfig configures a KV bucket.
type KVBucketConfig struct {
	TTL      time.Duration // Time-to-live for entries. 0 = no expiration.
	History  int           // Number of historical values to keep. 0 = default (1).
	MaxBytes int64         // Maximum total size. 0 = unlimited.
}

// KVOperation indicates what happened to a key.
type KVOperation int

const (
	KVOpPut    KVOperation = iota // Key was created or updated.
	KVOpDelete                    // Key was deleted.
)

// Subscription represents an active subscription that can be stopped.
type Subscription interface {
	Unsubscribe() error
}
