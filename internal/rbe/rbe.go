// Package rbe implements Report By Exception deadband logic.
// This is the single implementation used by gateway, sparkplug bridge, and history.
package rbe

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/joyautomation/tentacle/types"
)

// State tracks the last published value and time for a variable.
type State struct {
	LastValue interface{}
	LastTime  int64 // unix ms
}

// ShouldPublish determines whether a new value should be published based on
// the variable's RBE configuration.
//
// Rules (in order):
//  1. disableRBE → always publish
//  2. First value (LastTime == 0) → always publish
//  3. maxTime exceeded → force publish
//  4. minTime not elapsed → suppress
//  5. Numeric deadband: publish if |new - old| > deadband.Value
//  6. Non-numeric: publish if value changed
func ShouldPublish(state *State, newValue interface{}, nowMs int64, deadband *types.DeadBandConfig, disableRBE bool) bool {
	if disableRBE {
		return true
	}

	// First value — always publish
	if state.LastTime == 0 {
		return true
	}

	// No deadband — publish on any change
	if deadband == nil {
		return !ValuesEqual(state.LastValue, newValue)
	}

	elapsed := nowMs - state.LastTime

	// MaxTime exceeded — force publish
	if deadband.MaxTime > 0 && elapsed >= deadband.MaxTime {
		return true
	}

	// MinTime not elapsed — suppress
	if deadband.MinTime > 0 && elapsed < deadband.MinTime {
		return false
	}

	// Numeric deadband check
	oldFloat, oldOk := ToFloat64(state.LastValue)
	newFloat, newOk := ToFloat64(newValue)
	if oldOk && newOk {
		return math.Abs(newFloat-oldFloat) > deadband.Value
	}

	// Non-numeric — publish on any change
	return !ValuesEqual(state.LastValue, newValue)
}

// RecordPublish updates tracking state after a publish.
func RecordPublish(state *State, value interface{}, nowMs int64) {
	state.LastValue = value
	state.LastTime = nowMs
}

// ValuesEqual compares two values for equality.
// Numeric values are compared as float64. Others use string representation.
func ValuesEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	af, aOk := ToFloat64(a)
	bf, bOk := ToFloat64(b)
	if aOk && bOk {
		return af == bf
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// ToFloat64 attempts to convert a value to float64.
func ToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}
