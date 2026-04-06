package plc

import (
	"math"
	"sync"
	"time"

	ttypes "github.com/joyautomation/tentacle/types"
)

// RBERule matches variables by glob pattern and applies deadband settings.
type RBERule struct {
	Pattern  string                // Simple glob: "*" matches all, "temp*" matches prefix
	Deadband ttypes.DeadBandConfig
}

// rbeState tracks per-variable RBE state for publish filtering.
type rbeState struct {
	mu        sync.Mutex
	lastValue interface{}
	lastTime  time.Time
	config    *ttypes.DeadBandConfig
}

func (r *rbeState) shouldPublish(newVal interface{}) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// No deadband — always publish.
	if r.config == nil {
		r.lastValue = newVal
		r.lastTime = now
		return true
	}

	// MaxTime: force publish if exceeded.
	if r.config.MaxTime > 0 && !r.lastTime.IsZero() {
		if now.Sub(r.lastTime).Milliseconds() >= r.config.MaxTime {
			r.lastValue = newVal
			r.lastTime = now
			return true
		}
	}

	// MinTime: suppress if too soon.
	if r.config.MinTime > 0 && !r.lastTime.IsZero() {
		if now.Sub(r.lastTime).Milliseconds() < r.config.MinTime {
			return false
		}
	}

	// Value deadband check.
	if !exceedsDeadband(r.lastValue, newVal, r.config.Value) {
		return false
	}

	r.lastValue = newVal
	r.lastTime = now
	return true
}

func exceedsDeadband(old, new interface{}, threshold float64) bool {
	if threshold == 0 {
		return old != new
	}
	oldF, okO := toFloat64(old)
	newF, okN := toFloat64(new)
	if okO && okN {
		return math.Abs(newF-oldF) > threshold
	}
	// Non-numeric: any change exceeds deadband.
	return old != new
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	default:
		return 0, false
	}
}

// ResolveDeadband returns the effective deadband for a variable.
// Priority: per-variable config > matching rule > defaultDB.
func ResolveDeadband(variableID string, cfg VariableConfig, rules []RBERule, defaultDB *ttypes.DeadBandConfig) *ttypes.DeadBandConfig {
	if cfg.DisableRBE {
		return nil
	}
	if cfg.Deadband != nil {
		return cfg.Deadband
	}
	for _, rule := range rules {
		if matchGlob(rule.Pattern, variableID) {
			db := rule.Deadband
			return &db
		}
	}
	return defaultDB
}

// matchGlob performs simple glob matching: "*" matches everything,
// "prefix*" matches a prefix, otherwise exact match.
func matchGlob(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	n := len(pattern)
	if n > 0 && pattern[n-1] == '*' {
		prefix := pattern[:n-1]
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	return pattern == s
}
