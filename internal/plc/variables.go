//go:build plc || all

package plc

import (
	"encoding/json"
	"sync"
)

// RuntimeVariable holds the current value and metadata for a single PLC variable.
type RuntimeVariable struct {
	ID          string
	Datatype    string      // "number", "boolean", "string"
	Direction   string      // "input", "output", "internal"
	Value       interface{}
	Quality     string      // "good", "uncertain", "bad"
	LastUpdated int64       // unix millis
	changed     bool        // dirty flag for publish cycle
}

// VariableStore is a thread-safe container for all PLC runtime variables.
type VariableStore struct {
	mu   sync.RWMutex
	vars map[string]*RuntimeVariable
}

// NewVariableStore creates an empty VariableStore.
func NewVariableStore() *VariableStore {
	return &VariableStore{
		vars: make(map[string]*RuntimeVariable),
	}
}

// Get returns the current value of a variable. Returns nil if not found.
func (vs *VariableStore) Get(id string) interface{} {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	if v, ok := vs.vars[id]; ok {
		return v.Value
	}
	return nil
}

// GetNumber returns the variable as a float64, or 0 if not found/not numeric.
func (vs *VariableStore) GetNumber(id string) float64 {
	val := vs.Get(id)
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

// GetBool returns the variable as a bool, or false if not found.
func (vs *VariableStore) GetBool(id string) bool {
	val := vs.Get(id)
	if val == nil {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	// Truthy: non-zero numbers
	if n := vs.GetNumber(id); n != 0 {
		return true
	}
	return false
}

// GetString returns the variable as a string, or "" if not found.
func (vs *VariableStore) GetString(id string) string {
	val := vs.Get(id)
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// Set updates a variable's value, sets the dirty flag, and returns true if the variable exists.
func (vs *VariableStore) Set(id string, value interface{}, nowMs int64) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	v, ok := vs.vars[id]
	if !ok {
		return false
	}
	v.Value = value
	v.LastUpdated = nowMs
	v.changed = true
	return ok
}

// SetQuality updates the quality flag for a variable.
func (vs *VariableStore) SetQuality(id string, quality string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if v, ok := vs.vars[id]; ok {
		v.Quality = quality
	}
}

// Add registers a new variable in the store.
func (vs *VariableStore) Add(rv *RuntimeVariable) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.vars[rv.ID] = rv
}

// Clear removes all variables.
func (vs *VariableStore) Clear() {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.vars = make(map[string]*RuntimeVariable)
}

// DrainChanged returns all variables that have been modified since the last drain,
// clearing their dirty flags. Caller should publish these.
func (vs *VariableStore) DrainChanged() []*RuntimeVariable {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	var changed []*RuntimeVariable
	for _, v := range vs.vars {
		if v.changed {
			v.changed = false
			changed = append(changed, v)
		}
	}
	return changed
}

// All returns a snapshot of all variables. The returned map is safe to read without locking.
func (vs *VariableStore) All() map[string]*RuntimeVariable {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	snapshot := make(map[string]*RuntimeVariable, len(vs.vars))
	for k, v := range vs.vars {
		snapshot[k] = v
	}
	return snapshot
}

// MarkChanged flags a variable's current value for re-publication on the
// next cycle without changing it. Used when the value is mutated in
// place (e.g. a StructValue gaining a new field from a template update).
func (vs *VariableStore) MarkChanged(id string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if v, ok := vs.vars[id]; ok {
		v.changed = true
	}
}

// Count returns the number of variables.
func (vs *VariableStore) Count() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.vars)
}

// GetVariable returns the full RuntimeVariable, or nil if not found.
func (vs *VariableStore) GetVariable(id string) *RuntimeVariable {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	if v, ok := vs.vars[id]; ok {
		return v
	}
	return nil
}
