//go:build plc || all

package plc

import (
	"log/slog"
	"testing"

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
