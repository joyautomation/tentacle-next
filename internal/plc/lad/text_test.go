//go:build plc || all

package lad

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseText_BasicRung walks the canonical motor-latch shape end-to-
// end: parse text, lower expectations against the AST, then Print and
// re-parse to verify round-trip equality.
func TestParseText_BasicRung(t *testing.T) {
	src := `
diagram motor
  var global start : BOOL
  var global stop  : BOOL
  var global motor : BOOL
  var        latch : BOOL

  // Self-latching motor with stop interlock.
  rung (NO(start) | NO(latch)) & NC(stop) -> OTE(latch) -> OTE(motor)
end
`
	d, err := ParseText(src)
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if d.Name != "motor" {
		t.Errorf("name = %q, want motor", d.Name)
	}
	if len(d.Variables) != 4 {
		t.Fatalf("variables = %d, want 4", len(d.Variables))
	}
	if d.Variables[0] != (VarDecl{Name: "start", Type: "BOOL", Kind: "global"}) {
		t.Errorf("variables[0] = %+v", d.Variables[0])
	}
	if d.Variables[3] != (VarDecl{Name: "latch", Type: "BOOL"}) {
		t.Errorf("variables[3] = %+v", d.Variables[3])
	}
	if len(d.Rungs) != 1 {
		t.Fatalf("rungs = %d, want 1", len(d.Rungs))
	}
	r := d.Rungs[0]
	if r.Comment != "Self-latching motor with stop interlock." {
		t.Errorf("comment = %q", r.Comment)
	}
	// Logic: Series(Parallel(NO start, NO latch), NC stop)
	series, ok := r.Logic.(*Series)
	if !ok {
		t.Fatalf("logic = %T, want *Series", r.Logic)
	}
	if len(series.Items) != 2 {
		t.Fatalf("series items = %d, want 2", len(series.Items))
	}
	par, ok := series.Items[0].(*Parallel)
	if !ok {
		t.Fatalf("series[0] = %T, want *Parallel", series.Items[0])
	}
	if len(par.Items) != 2 {
		t.Fatalf("parallel items = %d, want 2", len(par.Items))
	}
	if c, _ := par.Items[0].(*Contact); c == nil || c.Form != "NO" || c.Operand != "start" {
		t.Errorf("parallel[0] = %+v", par.Items[0])
	}
	if c, _ := series.Items[1].(*Contact); c == nil || c.Form != "NC" || c.Operand != "stop" {
		t.Errorf("series[1] = %+v", series.Items[1])
	}
	if len(r.Outputs) != 2 {
		t.Fatalf("outputs = %d, want 2", len(r.Outputs))
	}
	if c, _ := r.Outputs[0].(*Coil); c == nil || c.Form != "OTE" || c.Operand != "latch" {
		t.Errorf("outputs[0] = %+v", r.Outputs[0])
	}

	// Round-trip through Print.
	round, err := ParseText(Print(d))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !reflect.DeepEqual(d, round) {
		t.Errorf("round-trip diverged:\noriginal=%+v\nround=%+v", d, round)
	}
}

// TestParseText_FBCallWithLiterals exercises FB-call output syntax —
// named arguments, time literal, integer literal, and the @PowerInput
// override — and confirms the AST shape.
func TestParseText_FBCallWithLiterals(t *testing.T) {
	src := `
diagram timed
  var global go : BOOL
  var        t1 : TON

  rung NO(go) -> t1@IN(PT := T#5s, count := 3)
end
`
	d, err := ParseText(src)
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if len(d.Rungs) != 1 || len(d.Rungs[0].Outputs) != 1 {
		t.Fatalf("unexpected shape: %+v", d)
	}
	fb, ok := d.Rungs[0].Outputs[0].(*FBCall)
	if !ok {
		t.Fatalf("output = %T, want *FBCall", d.Rungs[0].Outputs[0])
	}
	if fb.Instance != "t1" || fb.PowerInput != "IN" {
		t.Errorf("fb instance/power = %q/%q", fb.Instance, fb.PowerInput)
	}
	pt, ok := fb.Inputs["PT"].(*TimeLit)
	if !ok || pt.Ms != 5000 {
		t.Errorf("PT = %+v", fb.Inputs["PT"])
	}
	cnt, ok := fb.Inputs["count"].(*IntLit)
	if !ok || cnt.V != 3 {
		t.Errorf("count = %+v", fb.Inputs["count"])
	}

	round, err := ParseText(Print(d))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !reflect.DeepEqual(d, round) {
		t.Errorf("round-trip diverged:\noriginal=%+v\nround=%+v", d, round)
	}
}

// TestParseText_DottedOperand makes sure FB output reads (e.g. t1.Q)
// survive both contacts and FB-input expressions.
func TestParseText_DottedOperand(t *testing.T) {
	src := `
diagram dotted
  rung NO(t1.Q) -> OTE(out)
end
`
	d, err := ParseText(src)
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	c, ok := d.Rungs[0].Logic.(*Contact)
	if !ok || c.Operand != "t1.Q" {
		t.Errorf("contact operand = %+v", d.Rungs[0].Logic)
	}
}

// TestParseText_PrecedenceParens ensures `&` binds tighter than `|` so
// that "NO(a) | NO(b) & NC(c)" parses as a Parallel containing a Series
// for the right side.
func TestParseText_PrecedenceParens(t *testing.T) {
	d, err := ParseText("diagram p\nrung NO(a) | NO(b) & NC(c) -> OTE(out)\nend\n")
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	par, ok := d.Rungs[0].Logic.(*Parallel)
	if !ok {
		t.Fatalf("logic = %T, want *Parallel", d.Rungs[0].Logic)
	}
	if len(par.Items) != 2 {
		t.Fatalf("parallel items = %d", len(par.Items))
	}
	if _, ok := par.Items[0].(*Contact); !ok {
		t.Errorf("parallel[0] = %T", par.Items[0])
	}
	if _, ok := par.Items[1].(*Series); !ok {
		t.Errorf("parallel[1] = %T, want *Series", par.Items[1])
	}
}

// TestPrint_AddsParensForParallelInsideSeries verifies the printer
// disambiguates lower-precedence children — a Parallel inside a Series
// must be wrapped to round-trip correctly.
func TestPrint_AddsParensForParallelInsideSeries(t *testing.T) {
	d := &Diagram{
		Name: "p",
		Rungs: []*Rung{{
			Logic: &Series{Items: []Element{
				&Parallel{Items: []Element{
					&Contact{Form: "NO", Operand: "a"},
					&Contact{Form: "NO", Operand: "b"},
				}},
				&Contact{Form: "NC", Operand: "c"},
			}},
			Outputs: []Output{&Coil{Form: "OTE", Operand: "out"}},
		}},
	}
	out := Print(d)
	if !strings.Contains(out, "(NO(a) | NO(b)) & NC(c)") {
		t.Errorf("expected parens around parallel, got:\n%s", out)
	}
	round, err := ParseText(out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !reflect.DeepEqual(d, round) {
		t.Errorf("round-trip diverged:\noriginal=%+v\nround=%+v", d, round)
	}
}

// TestParseText_VarKindsAndInit covers var declarations across all
// kinds, with an init value and the RETAIN modifier, and round-trips.
func TestParseText_VarKindsAndInit(t *testing.T) {
	src := `
diagram inits
  var global trigger : BOOL := FALSE
  var input  setpoint : INT := 42
  var output result : REAL := 3.14
  var        counter : INT := 0 RETAIN

  rung NO(trigger) -> OTE(result)
end
`
	d, err := ParseText(src)
	if err != nil {
		t.Fatalf("ParseText: %v", err)
	}
	if len(d.Variables) != 4 {
		t.Fatalf("variables = %d, want 4", len(d.Variables))
	}
	want := []VarDecl{
		{Name: "trigger", Type: "BOOL", Kind: "global", Init: "FALSE"},
		{Name: "setpoint", Type: "INT", Kind: "input", Init: "42"},
		{Name: "result", Type: "REAL", Kind: "output", Init: "3.14"},
		{Name: "counter", Type: "INT", Init: "0", Retain: true},
	}
	if !reflect.DeepEqual(d.Variables, want) {
		t.Errorf("variables mismatch:\ngot  %+v\nwant %+v", d.Variables, want)
	}
	round, err := ParseText(Print(d))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !reflect.DeepEqual(d, round) {
		t.Errorf("round-trip diverged")
	}
}

// TestParseText_Errors covers the error surface so future regressions
// surface as test failures rather than silent acceptance.
func TestParseText_Errors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"missing diagram", `var global x : BOOL`, `expected "diagram"`},
		{"missing end", `diagram x` + "\n" + `rung NO(a) -> OTE(b)` + "\n", `unexpected token`},
		{"bad contact form", `diagram x` + "\n" + `rung XX(a) -> OTE(b)` + "\nend\n", `expected contact`},
		{"unterminated string", `diagram x` + "\n" + `var x : STRING := 'oops` + "\nend\n", `unterminated string`},
		{"unbalanced parens", `diagram x` + "\n" + `rung (NO(a) -> OTE(b)` + "\nend\n", `expected )`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseText(tc.src)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestPrintParseText_RoundTripFromJSON ensures a diagram parsed from
// JSON and then re-emitted as text round-trips to a structurally-equal
// AST — i.e. the two surface forms are interchangeable.
func TestPrintParseText_RoundTripFromJSON(t *testing.T) {
	jsonSrc := `{
		"name": "j",
		"variables": [
			{"name": "go", "type": "BOOL", "kind": "global"},
			{"name": "out", "type": "BOOL", "kind": "global"},
			{"name": "t1", "type": "TON"}
		],
		"rungs": [{
			"comment": "from json",
			"logic": {"kind": "series", "items": [
				{"kind": "contact", "form": "NO", "operand": "go"},
				{"kind": "parallel", "items": [
					{"kind": "contact", "form": "NC", "operand": "t1.Q"},
					{"kind": "contact", "form": "NO", "operand": "go"}
				]}
			]},
			"outputs": [
				{"kind": "coil", "form": "OTE", "operand": "out"},
				{"kind": "fb", "instance": "t1", "inputs": {"PT": {"kind": "time", "raw": "T#5s"}}}
			]
		}]
	}`
	dJSON, err := Parse(jsonSrc)
	if err != nil {
		t.Fatalf("Parse JSON: %v", err)
	}
	dText, err := ParseText(Print(dJSON))
	if err != nil {
		t.Fatalf("ParseText(Print(...)): %v", err)
	}
	if !reflect.DeepEqual(dJSON, dText) {
		t.Errorf("json↔text round-trip diverged:\njson=%+v\ntext=%+v", dJSON, dText)
	}
}
