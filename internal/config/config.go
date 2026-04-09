// Package config provides a KV-backed configuration manager with env var fallback.
// Ported from tentacle-mqtt/nats-config.ts ConfigManager.
//
// Resolution order per key: KV value → environment variable → default value.
// The manager watches KV for runtime changes and notifies listeners.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
)

// FieldDef describes a configuration field.
type FieldDef struct {
	EnvVar      string `json:"envVar"`                // Environment variable name
	Default     string `json:"default,omitempty"`      // Default value
	Required    bool   `json:"required,omitempty"`     // If true, Start fails without a value
	Type        string `json:"type"`                   // "string", "number", "boolean", "password"
	Description string `json:"description,omitempty"`
	EnvOnly     bool   `json:"envOnly,omitempty"`      // If true, never read from KV (e.g., NATS credentials)
	Label       string `json:"label"`                  // Display label for the UI
	Group       string `json:"group"`                  // Group name for visual grouping
	GroupOrder  int    `json:"groupOrder"`             // Sort order among groups
	SortOrder   int    `json:"sortOrder"`              // Sort order within a group
	Toggleable  bool   `json:"toggleable,omitempty"`   // If true, UI shows a switch; input only visible when on
	ToggleLabel string `json:"toggleLabel,omitempty"`  // Label for the toggle (e.g. "Override Device ID")
	DependsOn   string `json:"dependsOn,omitempty"`    // EnvVar of a boolean field; this field is hidden unless that field is "true"
}

// Manager reads configuration from KV with env var fallback.
type Manager struct {
	b        bus.Bus
	moduleID string
	schema   map[string]FieldDef
	values   map[string]string
	mu       sync.RWMutex
	onChange []func(key, value string)
	watchSub bus.Subscription
}

// New creates a ConfigManager, loads initial values, and begins watching KV.
func New(b bus.Bus, moduleID string, schema map[string]FieldDef) (*Manager, error) {
	// Ensure bucket exists
	if err := b.KVCreate(topics.BucketTentacleConfig, topics.BucketConfigs()[topics.BucketTentacleConfig]); err != nil {
		slog.Warn("config: failed to create bucket", "error", err)
	}

	m := &Manager{
		b:        b,
		moduleID: moduleID,
		schema:   schema,
		values:   make(map[string]string),
	}

	// Load initial values: KV → env → default
	for key, field := range schema {
		kvKey := fmt.Sprintf("%s.%s", moduleID, field.EnvVar)
		var val string

		// Try KV first (unless envOnly)
		if !field.EnvOnly {
			if data, _, err := b.KVGet(topics.BucketTentacleConfig, kvKey); err == nil {
				val = string(data)
			}
		}

		// Fall back to env var
		if val == "" {
			val = os.Getenv(field.EnvVar)
		}

		// Fall back to default
		if val == "" {
			val = field.Default
		}

		if val == "" && field.Required {
			return nil, fmt.Errorf("config: required field %q (%s) has no value", key, field.EnvVar)
		}

		m.values[key] = val
	}

	// Watch for KV changes
	sub, err := b.KVWatchAll(topics.BucketTentacleConfig, func(key string, value []byte, op bus.KVOperation) {
		// Key format: {moduleId}.{ENVVAR}
		prefix := moduleID + "."
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			return // not for this module
		}
		envVar := key[len(prefix):]

		// Find which config key this envVar maps to
		m.mu.Lock()
		for cfgKey, field := range m.schema {
			if field.EnvVar == envVar && !field.EnvOnly {
				if op == bus.KVOpDelete {
					m.values[cfgKey] = field.Default
				} else {
					m.values[cfgKey] = string(value)
				}
				// Notify listeners outside the lock
				listeners := make([]func(string, string), len(m.onChange))
				copy(listeners, m.onChange)
				val := m.values[cfgKey]
				m.mu.Unlock()
				for _, fn := range listeners {
					fn(cfgKey, val)
				}
				return
			}
		}
		m.mu.Unlock()
	})
	if err != nil {
		slog.Warn("config: failed to watch KV", "error", err)
	} else {
		m.watchSub = sub
	}

	return m, nil
}

// GetString returns a config value as a string.
func (m *Manager) GetString(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.values[key]
}

// GetInt returns a config value as an int. Returns 0 on parse error.
func (m *Manager) GetInt(key string) int {
	v := m.GetString(key)
	n, _ := strconv.Atoi(v)
	return n
}

// GetBool returns a config value as a bool.
func (m *Manager) GetBool(key string) bool {
	v := m.GetString(key)
	return v == "true" || v == "1"
}

// Set writes a value to KV (and updates the local cache).
func (m *Manager) Set(key, value string) error {
	m.mu.RLock()
	field, ok := m.schema[key]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("config: unknown key %q", key)
	}
	if field.EnvOnly {
		return fmt.Errorf("config: key %q is env-only", key)
	}

	kvKey := fmt.Sprintf("%s.%s", m.moduleID, field.EnvVar)
	_, err := m.b.KVPut(topics.BucketTentacleConfig, kvKey, []byte(value))
	return err
}

// OnChange registers a listener called when any config value changes.
// Returns a cancel function.
func (m *Manager) OnChange(fn func(key, value string)) func() {
	m.mu.Lock()
	m.onChange = append(m.onChange, fn)
	idx := len(m.onChange) - 1
	m.mu.Unlock()

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if idx < len(m.onChange) {
			m.onChange = append(m.onChange[:idx], m.onChange[idx+1:]...)
		}
	}
}

// Destroy stops the KV watcher.
func (m *Manager) Destroy() {
	if m.watchSub != nil {
		_ = m.watchSub.Unsubscribe()
	}
}

// RegisterSchema subscribes to the "{serviceType}.config.schema" bus request topic
// and responds with the given field definitions. Returns the subscription for cleanup.
func RegisterSchema(b bus.Bus, serviceType string, fields []FieldDef) (bus.Subscription, error) {
	subject := serviceType + ".config.schema"
	return b.Subscribe(subject, func(_ string, _ []byte, reply bus.ReplyFunc) {
		if reply == nil {
			return
		}
		data, err := json.Marshal(fields)
		if err != nil {
			slog.Warn("config: failed to marshal schema", "error", err)
			return
		}
		_ = reply(data)
	})
}
