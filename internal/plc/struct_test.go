//go:build plc || all

package plc

import (
	"log/slog"
	"testing"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"go.starlark.net/starlark"
)

func mustFloat(t *testing.T, v starlark.Value) float64 {
	t.Helper()
	f, ok := v.(starlark.Float)
	if !ok {
		t.Fatalf("expected Float, got %T (%v)", v, v)
	}
	return float64(f)
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(NewVariableStore(), slog.Default())
	return e
}

var motorTemplate = &itypes.PlcTemplate{
	Name: "Motor",
	Fields: []itypes.PlcTemplateField{
		{Name: "running", Type: "bool", Default: false},
		{Name: "speed", Type: "number", Default: 0.0},
		{Name: "name", Type: "string", Default: "m1"},
	},
	Methods: []itypes.PlcTemplateMethod{
		{Name: "start", Function: itypes.PlcFunctionRef{Module: "plc", Name: "motor_start"}},
		{Name: "set_speed", Function: itypes.PlcFunctionRef{Module: "plc", Name: "motor_set_speed"}},
	},
}

const motorProgram = `
def motor_start(m):
    m.running = True
    return m.running

def motor_set_speed(m, rpm):
    m.speed = float(rpm)
    return m.speed

def main():
    pass
`

func TestStructDefaultsAndFieldAccess(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})

	m, err := e.NewStruct("Motor", nil)
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}

	if v, _ := m.Attr("running"); v != starlark.False {
		t.Errorf("default running = %v, want False", v)
	}
	if v, _ := m.Attr("speed"); mustFloat(t, v) != 0 {
		t.Errorf("default speed = %v, want 0", v)
	}
	if v, _ := m.Attr("name"); v != starlark.String("m1") {
		t.Errorf("default name = %v, want \"m1\"", v)
	}
	if v, _ := m.Attr("nonexistent"); v != nil {
		t.Errorf("unknown attr returned %v, want nil", v)
	}
}

func TestStructSetFieldTypeChecks(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})
	m, _ := e.NewStruct("Motor", nil)

	if err := m.SetField("running", starlark.True); err != nil {
		t.Errorf("SetField running=True: %v", err)
	}
	if err := m.SetField("speed", starlark.Float(1200)); err != nil {
		t.Errorf("SetField speed=1200: %v", err)
	}
	if err := m.SetField("speed", starlark.String("fast")); err == nil {
		t.Error("SetField speed=\"fast\" should have failed")
	}
	if err := m.SetField("unknown", starlark.True); err == nil {
		t.Error("SetField unknown should have failed")
	}
}

func TestMethodDispatchMotorStart(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})

	if err := e.Compile("plc", motorProgram); err != nil {
		t.Fatalf("compile: %v", err)
	}

	m, _ := e.NewStruct("Motor", nil)

	// Resolve `motor.start` — should return a bound method.
	startAttr, err := m.Attr("start")
	if err != nil {
		t.Fatalf("Attr(start): %v", err)
	}
	bm, ok := startAttr.(*boundMethod)
	if !ok {
		t.Fatalf("Attr(start) = %T, want *boundMethod", startAttr)
	}

	// Call it via starlark.Call so the receiver is threaded in.
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, bm, nil, nil)
	if err != nil {
		t.Fatalf("call motor.start(): %v", err)
	}
	if result != starlark.True {
		t.Errorf("motor.start() returned %v, want True", result)
	}

	// Side effect: running should now be True.
	if v, _ := m.Attr("running"); v != starlark.True {
		t.Errorf("after start: running = %v, want True", v)
	}
}

func TestMethodDispatchWithArgs(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})
	if err := e.Compile("plc", motorProgram); err != nil {
		t.Fatalf("compile: %v", err)
	}

	m, _ := e.NewStruct("Motor", nil)
	setSpeed, _ := m.Attr("set_speed")
	thread := &starlark.Thread{Name: "test"}
	result, err := starlark.Call(thread, setSpeed, starlark.Tuple{starlark.MakeInt(1500)}, nil)
	if err != nil {
		t.Fatalf("call set_speed(1500): %v", err)
	}
	if mustFloat(t, result) != 1500 {
		t.Errorf("set_speed result = %v, want 1500", result)
	}
	if v, _ := m.Attr("speed"); mustFloat(t, v) != 1500 {
		t.Errorf("speed after set = %v, want 1500", v)
	}
}

func TestFreeFunctionDispatch(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})
	if err := e.Compile("plc", motorProgram); err != nil {
		t.Fatalf("compile: %v", err)
	}

	bindings, err := e.RegisterTemplateMethods("Motor")
	if err != nil {
		t.Fatalf("RegisterTemplateMethods: %v", err)
	}
	if _, ok := bindings["start"]; !ok {
		t.Error("expected free binding `start`")
	}
	if _, ok := bindings["set_speed"]; !ok {
		t.Error("expected free binding `set_speed`")
	}

	// Call the free form with the receiver as positional arg 0.
	m, _ := e.NewStruct("Motor", nil)
	thread := &starlark.Thread{Name: "test"}
	_, err = starlark.Call(thread, bindings["start"], starlark.Tuple{m}, nil)
	if err != nil {
		t.Fatalf("start(motor): %v", err)
	}
	if v, _ := m.Attr("running"); v != starlark.True {
		t.Errorf("after start(m): running = %v, want True", v)
	}
}

func TestNewStructRejectsUnknownFields(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})
	_, err := e.NewStruct("Motor", map[string]starlark.Value{
		"bogus": starlark.True,
	})
	if err == nil {
		t.Fatal("expected error on unknown field, got nil")
	}
}

func TestNestedTemplateField(t *testing.T) {
	fault := &itypes.PlcTemplate{
		Name: "Fault",
		Fields: []itypes.PlcTemplateField{
			{Name: "code", Type: "number", Default: 0.0},
		},
	}
	motor := &itypes.PlcTemplate{
		Name: "MotorWithFault",
		Fields: []itypes.PlcTemplateField{
			{Name: "running", Type: "bool"},
			{Name: "fault", Type: "Fault"},
		},
	}
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{
		"Fault":           fault,
		"MotorWithFault": motor,
	})

	m, err := e.NewStruct("MotorWithFault", nil)
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}

	// Default for nested field is None; user instantiates + assigns.
	if v, _ := m.Attr("fault"); v != starlark.None {
		t.Errorf("default fault = %v, want None", v)
	}

	f, _ := e.NewStruct("Fault", nil)
	if err := m.SetField("fault", f); err != nil {
		t.Fatalf("SetField(fault, Fault): %v", err)
	}

	// Wrong nested type should fail.
	other, _ := e.NewStruct("Fault", nil)
	_ = other
	if err := m.SetField("fault", starlark.String("nope")); err == nil {
		t.Error("SetField(fault, string) should have failed")
	}
}

func TestAttrNamesSorted(t *testing.T) {
	e := newTestEngine(t)
	e.SetTemplates(map[string]*itypes.PlcTemplate{"Motor": motorTemplate})
	m, _ := e.NewStruct("Motor", nil)
	names := m.AttrNames()
	want := []string{"name", "running", "set_speed", "speed", "start"}
	if len(names) != len(want) {
		t.Fatalf("AttrNames = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("AttrNames[%d] = %q, want %q", i, n, want[i])
		}
	}
}
