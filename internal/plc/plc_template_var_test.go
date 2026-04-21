//go:build plc || all

package plc

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
	"go.starlark.net/starlark"
)

// TestGetVarReturnsStructValue verifies the runtime glue: a
// template-typed variable lives in the VariableStore as a *StructValue
// and is returned directly from get_var so programs can dot-access it.
func TestGetVarReturnsStructValue(t *testing.T) {
	tmpl := &itypes.PlcTemplate{
		Name: "Motor",
		Fields: []itypes.PlcTemplateField{
			{Name: "speed", Type: "number", Default: 0.0},
		},
	}
	vs := NewVariableStore()
	e := NewEngine(vs, slog.Default())
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": tmpl})

	sv, err := e.NewStruct("Motor", map[string]starlark.Value{
		"speed": starlark.Float(1200),
	})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	vs.Add(&RuntimeVariable{
		ID:       "motor1",
		Datatype: "Motor",
		Value:    sv,
	})

	prog := `
def main():
    m = get_var("motor1")
    return m.speed
`
	if err := e.Compile("test", prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	state := NewTaskState()
	state.thread = &starlark.Thread{Name: "test"}

	result, err := starlark.Call(state.thread, e.programs["test"].mainFn, nil, nil)
	if err != nil {
		t.Fatalf("call main(): %v", err)
	}
	if f, _ := result.(starlark.Float); float64(f) != 1200 {
		t.Errorf("main() returned %v, want 1200", result)
	}
}

// TestSetVarPreservesStructValue verifies that set_var on a template-
// typed variable stores the *StructValue directly (not a stringified
// copy) so subsequent reads get the same live instance.
func TestSetVarPreservesStructValue(t *testing.T) {
	tmpl := &itypes.PlcTemplate{
		Name: "Motor",
		Fields: []itypes.PlcTemplateField{
			{Name: "speed", Type: "number", Default: 0.0},
		},
	}
	vs := NewVariableStore()
	e := NewEngine(vs, slog.Default())
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": tmpl})

	sv, _ := e.NewStruct("Motor", nil)
	vs.Add(&RuntimeVariable{ID: "motor1", Datatype: "Motor", Value: sv})

	prog := `
def main():
    m = get_var("motor1")
    m.speed = 2000.0
    set_var("motor1", m)
`
	if err := e.Compile("test", prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	thread := &starlark.Thread{Name: "test"}
	if _, err := starlark.Call(thread, e.programs["test"].mainFn, nil, nil); err != nil {
		t.Fatalf("call main(): %v", err)
	}

	stored := vs.Get("motor1")
	got, ok := stored.(*StructValue)
	if !ok {
		t.Fatalf("stored value is %T, want *StructValue", stored)
	}
	v, _ := got.Attr("speed")
	if f, _ := v.(starlark.Float); float64(f) != 2000 {
		t.Errorf("speed after set = %v, want 2000", v)
	}
}

// TestInitialVariableValueAtomic confirms atomic vars pass through
// without attempting template instantiation.
func TestInitialVariableValueAtomic(t *testing.T) {
	e := NewEngine(NewVariableStore(), slog.Default())
	v := initialVariableValue(e, &itypes.PlcVariableConfigKV{
		ID:       "tank",
		Datatype: "number",
		Default:  42.0,
	}, slog.Default())
	if v != 42.0 {
		t.Errorf("got %v, want 42.0", v)
	}
}

// TestInitialVariableValueTemplate confirms template-typed vars get a
// StructValue with the Default map merged into its fields.
func TestInitialVariableValueTemplate(t *testing.T) {
	tmpl := &itypes.PlcTemplate{
		Name: "Motor",
		Fields: []itypes.PlcTemplateField{
			{Name: "speed", Type: "number", Default: 0.0},
			{Name: "running", Type: "bool", Default: false},
		},
	}
	e := NewEngine(NewVariableStore(), slog.Default())
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": tmpl})

	v := initialVariableValue(e, &itypes.PlcVariableConfigKV{
		ID:       "motor1",
		Datatype: "Motor",
		Default: map[string]interface{}{
			"speed":   1500.0,
			"running": true,
		},
	}, slog.Default())

	sv, ok := v.(*StructValue)
	if !ok {
		t.Fatalf("got %T, want *StructValue", v)
	}
	speed, _ := sv.Attr("speed")
	if f, _ := speed.(starlark.Float); float64(f) != 1500 {
		t.Errorf("speed = %v, want 1500", speed)
	}
	running, _ := sv.Attr("running")
	if running != starlark.True {
		t.Errorf("running = %v, want True", running)
	}
}

// TestApplyConfigInstantiatesTemplateVariable is the end-to-end
// integration test for Phase 3a: a template stored in the plc_templates
// bucket + a template-typed variable in the config must flow through
// applyConfig and land in the VariableStore as a live *StructValue that
// programs compiled on the engine can dot-access and mutate.
func TestApplyConfigInstantiatesTemplateVariable(t *testing.T) {
	b := bus.NewChannelBus()
	for bucket, cfg := range topics.BucketConfigs() {
		if err := b.KVCreate(bucket, cfg); err != nil {
			t.Fatalf("KVCreate %s: %v", bucket, err)
		}
	}

	tmpl := itypes.PlcTemplate{
		Name: "Motor",
		Fields: []itypes.PlcTemplateField{
			{Name: "speed", Type: "number", Default: 0.0},
			{Name: "running", Type: "bool", Default: false},
		},
	}
	data, _ := json.Marshal(tmpl)
	if _, err := b.KVPut(topics.BucketPlcTemplates, "Motor", data); err != nil {
		t.Fatalf("seed template: %v", err)
	}

	m := New("plc-test")
	m.b = b
	m.log = slog.Default()

	cfg := &itypes.PlcConfigKV{
		PlcID: "plc-test",
		Variables: map[string]itypes.PlcVariableConfigKV{
			"motor1": {
				ID:       "motor1",
				Datatype: "Motor",
				Default: map[string]interface{}{
					"speed":   1500.0,
					"running": true,
				},
			},
		},
		Tasks:     map[string]itypes.PlcTaskConfigKV{},
		UpdatedAt: time.Now().UnixMilli(),
	}
	m.applyConfig(cfg)
	defer m.Stop()

	sv, ok := m.variables.Get("motor1").(*StructValue)
	if !ok {
		t.Fatalf("motor1 is %T, want *StructValue", m.variables.Get("motor1"))
	}
	if v, _ := sv.Attr("speed"); mustFloat(t, v) != 1500 {
		t.Errorf("motor1.speed = %v, want 1500", v)
	}
	if v, _ := sv.Attr("running"); v != starlark.True {
		t.Errorf("motor1.running = %v, want True", v)
	}

	// Compile and run a program on the live engine to prove the struct
	// is reachable from Starlark end-to-end.
	prog := `
def main():
    m = get_var("motor1")
    m.speed = m.speed + 100
    set_var("motor1", m)
`
	if err := m.engine.Compile("bump", prog); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := m.engine.Execute("bump", NewTaskState()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	after := m.variables.Get("motor1").(*StructValue)
	if v, _ := after.Attr("speed"); mustFloat(t, v) != 1600 {
		t.Errorf("after bump: speed = %v, want 1600", v)
	}
}
