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
// Keys are (deviceId, rawTagPath). Values are raw Go primitives as
// decoded from PlcDataMessage.Value. Raw tag paths (carried in
// PlcDataMessage.VariableID by publishers) preserve the dotted
// structure — e.g. "RTU60_13XFR9_PLC_TOD.SECOND" — which the NATS
// subject cannot (sanitization flattens dots to underscores). That
// faithful structure is what lets read_tag return an aggregate dict
// for a template-instance path.
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

// Get returns the most recent cached value for (deviceId, tagPath).
// tagPath is compared against raw (unsanitized) paths — pass the
// human-readable form (e.g. "Motor1.Speed").
func (c *DeviceTagCache) Get(deviceID, tagPath string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.values[deviceID]
	if !ok {
		return nil, false
	}
	v, ok := d[tagPath]
	return v, ok
}

// GetAggregate returns every child path of basePath on deviceID, keyed
// by the portion of the path *after* `basePath.`. For example, if the
// device has tags "RTU60_13XFR9_PLC_TOD.SECOND" and
// "RTU60_13XFR9_PLC_TOD.DAY", GetAggregate(_, "RTU60_13XFR9_PLC_TOD")
// returns {"SECOND": ..., "DAY": ...}. The second return is false when
// no children are found, so callers can distinguish "empty aggregate"
// from "no such path".
//
// Only direct children are flattened to top-level keys. Nested
// grandchildren keep their dotted segments in the returned map — that
// way a template instance with its own nested struct still round-trips
// faithfully.
func (c *DeviceTagCache) GetAggregate(deviceID, basePath string) (map[string]interface{}, bool) {
	prefix := basePath + "."
	c.mu.RLock()
	defer c.mu.RUnlock()
	d, ok := c.values[deviceID]
	if !ok {
		return nil, false
	}
	out := make(map[string]interface{})
	for path, val := range d {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		out[path[len(prefix):]] = val
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (c *DeviceTagCache) handle(subject string, data []byte) {
	var msg types.PlcDataMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	deviceID := msg.DeviceID
	tag := msg.VariableID
	if deviceID == "" || tag == "" {
		// Subject format: {protocol}.data.{deviceId}.{sanitizedTag}.
		// Fall back to the subject if the publisher didn't populate
		// VariableID — the sanitized tag is still usable for direct
		// lookup by already-sanitized callers.
		parts := strings.SplitN(subject, ".", 4)
		if len(parts) < 4 {
			return
		}
		if deviceID == "" {
			deviceID = parts[2]
		}
		if tag == "" {
			tag = parts[3]
		}
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
