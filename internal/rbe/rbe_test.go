package rbe

import (
	"testing"

	"github.com/joyautomation/tentacle/types"
)

func TestShouldPublish_FirstValue(t *testing.T) {
	state := &State{}
	if !ShouldPublish(state, 42.0, 1000, nil, false) {
		t.Error("first value should always publish")
	}
}

func TestShouldPublish_DisableRBE(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	if !ShouldPublish(state, 42.0, 2000, nil, true) {
		t.Error("disableRBE should always publish")
	}
}

func TestShouldPublish_NoDeadband_Changed(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	if !ShouldPublish(state, 43.0, 2000, nil, false) {
		t.Error("changed value should publish with no deadband")
	}
}

func TestShouldPublish_NoDeadband_Same(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	if ShouldPublish(state, 42.0, 2000, nil, false) {
		t.Error("same value should not publish with no deadband")
	}
}

func TestShouldPublish_Deadband_BelowThreshold(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	db := &types.DeadBandConfig{Value: 5.0}
	if ShouldPublish(state, 44.0, 2000, db, false) {
		t.Error("change of 2.0 should not exceed deadband of 5.0")
	}
}

func TestShouldPublish_Deadband_AboveThreshold(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	db := &types.DeadBandConfig{Value: 5.0}
	if !ShouldPublish(state, 48.0, 2000, db, false) {
		t.Error("change of 6.0 should exceed deadband of 5.0")
	}
}

func TestShouldPublish_MinTime_Suppresses(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	db := &types.DeadBandConfig{Value: 1.0, MinTime: 5000}
	if ShouldPublish(state, 100.0, 2000, db, false) {
		t.Error("should suppress: minTime not elapsed (2000-1000=1000 < 5000)")
	}
}

func TestShouldPublish_MinTime_Allows(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	db := &types.DeadBandConfig{Value: 1.0, MinTime: 500}
	if !ShouldPublish(state, 100.0, 2000, db, false) {
		t.Error("should publish: minTime elapsed and value changed beyond deadband")
	}
}

func TestShouldPublish_MaxTime_Forces(t *testing.T) {
	state := &State{LastValue: 42.0, LastTime: 1000}
	db := &types.DeadBandConfig{Value: 100.0, MaxTime: 5000}
	if !ShouldPublish(state, 42.0, 7000, db, false) {
		t.Error("should force publish: maxTime exceeded")
	}
}

func TestShouldPublish_String_Changed(t *testing.T) {
	state := &State{LastValue: "hello", LastTime: 1000}
	db := &types.DeadBandConfig{Value: 1.0}
	if !ShouldPublish(state, "world", 2000, db, false) {
		t.Error("string change should publish")
	}
}

func TestShouldPublish_String_Same(t *testing.T) {
	state := &State{LastValue: "hello", LastTime: 1000}
	db := &types.DeadBandConfig{Value: 1.0}
	if ShouldPublish(state, "hello", 2000, db, false) {
		t.Error("same string should not publish")
	}
}

func TestShouldPublish_Bool_Changed(t *testing.T) {
	state := &State{LastValue: true, LastTime: 1000}
	// Booleans convert to float64 (true=1, false=0), so deadband of 0.5 triggers on change
	db := &types.DeadBandConfig{Value: 0.5}
	if !ShouldPublish(state, false, 2000, db, false) {
		t.Error("bool change should publish (1.0 - 0.0 = 1.0 > 0.5)")
	}
}

func TestRecordPublish(t *testing.T) {
	state := &State{}
	RecordPublish(state, 42.0, 1000)
	if state.LastValue != 42.0 {
		t.Errorf("LastValue = %v, want 42.0", state.LastValue)
	}
	if state.LastTime != 1000 {
		t.Errorf("LastTime = %v, want 1000", state.LastTime)
	}
}

func TestValuesEqual(t *testing.T) {
	tests := []struct {
		a, b interface{}
		want bool
	}{
		{nil, nil, true},
		{nil, 1, false},
		{1, nil, false},
		{42.0, 42.0, true},
		{42.0, 43.0, false},
		{int(42), float64(42), true},
		{"hello", "hello", true},
		{"hello", "world", false},
		{true, true, true},
		{true, false, false},
	}
	for _, tt := range tests {
		got := ValuesEqual(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("ValuesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		v    interface{}
		want float64
		ok   bool
	}{
		{float64(3.14), 3.14, true},
		{float32(2.5), 2.5, true},
		{int(42), 42, true},
		{int64(100), 100, true},
		{uint(7), 7, true},
		{true, 1, true},
		{false, 0, true},
		{"hello", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, ok := ToFloat64(tt.v)
		if ok != tt.ok {
			t.Errorf("ToFloat64(%v) ok = %v, want %v", tt.v, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("ToFloat64(%v) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
