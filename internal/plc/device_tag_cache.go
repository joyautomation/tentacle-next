//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// DeviceTagCache holds the latest scanner-published value for every tag
// that has been observed on any device/protocol. PLC programs read from
// it via the read_tag(deviceId, tagPath) builtin.
//
// Keys are (deviceId, sanitizedTag). Values are raw Go primitives as
// decoded from PlcDataMessage.Value. The cache is write-through from
// NATS subscriptions — we never round-trip through the KV layer.
type DeviceTagCache struct {
	b   bus.Bus
	log *slog.Logger

	mu     sync.RWMutex
	values map[string]map[string]interface{}

	subs []bus.Subscription
}

// NewDeviceTagCache creates an empty cache. Call Start to subscribe.
func NewDeviceTagCache(b bus.Bus, log *slog.Logger) *DeviceTagCache {
	return &DeviceTagCache{
		b:      b,
		log:    log,
		values: make(map[string]map[string]interface{}),
	}
}

// Start subscribes to scanner data across every known protocol. The PLC
// publishes its own variables on plc.data.> too — we skip those to avoid
// a self-echo in the cache.
func (c *DeviceTagCache) Start() error {
	protocols := []string{"ethernetip", "opcua", "modbus", "snmp", "gateway", "profinet"}
	for _, p := range protocols {
		sub, err := c.b.Subscribe(topics.DataWildcard(p), func(subject string, data []byte, reply bus.ReplyFunc) {
			c.handle(subject, data)
		})
		if err != nil {
			c.log.Warn("device_tag_cache: subscribe failed", "protocol", p, "error", err)
			continue
		}
		c.subs = append(c.subs, sub)
	}
	return nil
}

// Stop unsubscribes everything and drops cached values.
func (c *DeviceTagCache) Stop() {
	for _, s := range c.subs {
		if s != nil {
			s.Unsubscribe()
		}
	}
	c.subs = nil
	c.mu.Lock()
	c.values = make(map[string]map[string]interface{})
	c.mu.Unlock()
}

// Get returns the most recent cached value for (deviceId, tagPath). The
// tagPath is sanitized the same way as the publisher's subject so users
// can pass the human-readable tag path (e.g. "Motor1.Speed").
func (c *DeviceTagCache) Get(deviceID, tagPath string) (interface{}, bool) {
	sanitized := types.SanitizeForSubject(tagPath)
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.values[deviceID]
	if !ok {
		return nil, false
	}
	v, ok := d[sanitized]
	return v, ok
}

func (c *DeviceTagCache) handle(subject string, data []byte) {
	// Subject format: {protocol}.data.{deviceId}.{sanitizedTag}
	parts := strings.SplitN(subject, ".", 4)
	if len(parts) < 4 {
		return
	}
	deviceID := parts[2]
	tag := parts[3]

	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	c.mu.Lock()
	d, ok := c.values[deviceID]
	if !ok {
		d = make(map[string]interface{})
		c.values[deviceID] = d
	}
	d[tag] = msg.Value
	c.mu.Unlock()
}
