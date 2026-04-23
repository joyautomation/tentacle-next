//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestCompletionIncludesBuiltins(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0}, nil, "", "")
	if len(list.Items) == 0 {
		t.Fatalf("expected completion items, got 0")
	}
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	for _, name := range []string{"get_var", "set_var", "log", "NO", "TON", "clamp"} {
		if !seen[name] {
			t.Errorf("expected %q in completion list", name)
		}
	}
}

func TestCompletionBuiltinHasSnippet(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0}, nil, "", "")
	for _, it := range list.Items {
		if it.Label == "get_var" {
			if it.InsertTextFormat != InsertTextFormatSnippet {
				t.Errorf("expected snippet format for get_var, got %d", it.InsertTextFormat)
			}
			if !strings.Contains(it.InsertText, "$1") {
				t.Errorf("expected snippet placeholder in get_var insert text, got %q", it.InsertText)
			}
			if it.Detail == "" {
				t.Errorf("expected signature in Detail")
			}
			return
		}
	}
	t.Errorf("get_var not found in completion list")
}

func TestCompletionIncludesLocalDef(t *testing.T) {
	src := "def cool_helper():\n    pass\n\nx = 1\n"
	list := completeStarlark(src, Position{Line: 3, Character: 0}, nil, "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["cool_helper"] {
		t.Errorf("expected `cool_helper` in completions, got %v", completionLabels(list))
	}
	if !seen["x"] {
		t.Errorf("expected `x` in completions")
	}
}

func TestCompletionIncludesDefParams(t *testing.T) {
	src := "def main(motor_speed, setpoint):\n    y = 1\n"
	// Cursor on the `y = 1` line — inside def main's body.
	list := completeStarlark(src, Position{Line: 1, Character: 4}, nil, "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["motor_speed"] || !seen["setpoint"] {
		t.Errorf("expected def params in completions, got %v", completionLabels(list))
	}
}

func TestCompletionKeywordsPresent(t *testing.T) {
	list := completeStarlark("", Position{Line: 0, Character: 0}, nil, "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	for _, kw := range []string{"if", "else", "for", "def", "True", "False"} {
		if !seen[kw] {
			t.Errorf("expected keyword %q in completions", kw)
		}
	}
}

// ─── Member-access completion ───────────────────────────────────────

// fakeProvider is a minimal SymbolProvider backed by plain maps.
type fakeProvider struct {
	vars      map[string]*VariableInfo
	templates map[string]*TemplateInfo
	fns       map[string]*FunctionInfo
}

func (f *fakeProvider) Variable(name string) *VariableInfo { return f.vars[name] }
func (f *fakeProvider) Template(name string) *TemplateInfo { return f.templates[name] }
func (f *fakeProvider) TemplateNames() []string {
	names := make([]string, 0, len(f.templates))
	for name := range f.templates {
		names = append(names, name)
	}
	return names
}
func (f *fakeProvider) VariableNames() []string {
	names := make([]string, 0, len(f.vars))
	for name := range f.vars {
		names = append(names, name)
	}
	return names
}
func (f *fakeProvider) Function(name string) *FunctionInfo { return f.fns[name] }
func (f *fakeProvider) FunctionNames() []string {
	names := make([]string, 0, len(f.fns))
	for name := range f.fns {
		names = append(names, name)
	}
	return names
}

func newMotorProvider() *fakeProvider {
	tmpl := &TemplateInfo{
		Name: "Motor",
		Fields: []TemplateField{
			{Name: "speed", Type: "number", Unit: "rpm"},
			{Name: "running", Type: "bool"},
		},
		Methods: []TemplateMethod{{Name: "start"}},
	}
	return &fakeProvider{
		vars:      map[string]*VariableInfo{"motor1": {Name: "motor1", Datatype: "Motor", TemplateName: "Motor"}},
		templates: map[string]*TemplateInfo{"Motor": tmpl},
	}
}

func TestCompletionGetVarDotReturnsTemplateFields(t *testing.T) {
	src := `def main():
    get_var("motor1").`
	// Cursor sits right after the dot (line 1, char 22).
	list := completeStarlark(src, Position{Line: 1, Character: 22}, newMotorProvider(), "", "")
	labels := completionLabels(list)
	seen := map[string]bool{}
	for _, l := range labels {
		seen[l] = true
	}
	for _, want := range []string{"speed", "running", "start"} {
		if !seen[want] {
			t.Errorf("expected %q in member completion, got %v", want, labels)
		}
	}
	// Builtins must NOT leak in — the user asked for members.
	if seen["get_var"] {
		t.Errorf("member completion should not include builtins, got %v", labels)
	}
}

func TestCompletionGetVarDotPartialIdentifier(t *testing.T) {
	// After the partial member `sp`, completion should still offer all
	// fields; the client filters by prefix.
	src := `def main():
    get_var("motor1").sp`
	list := completeStarlark(src, Position{Line: 1, Character: 24}, newMotorProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["speed"] {
		t.Errorf("expected speed in completions after partial `sp`, got %v", completionLabels(list))
	}
}

func TestCompletionParamAnnotationOffersTemplatesAndPrimitives(t *testing.T) {
	// Cursor sits right after the colon in `def f(x: |`.
	src := `def f(x: `
	list := completeStarlark(src, Position{Line: 0, Character: 9}, newMotorProvider(), "", "")
	labels := completionLabels(list)
	seen := map[string]bool{}
	for _, l := range labels {
		seen[l] = true
	}
	for _, want := range []string{"Motor", "number", "bool", "string"} {
		if !seen[want] {
			t.Errorf("expected %q in annotation completion, got %v", want, labels)
		}
	}
	if seen["get_var"] || seen["if"] {
		t.Errorf("annotation completion should not include builtins/keywords, got %v", labels)
	}
}

func TestCompletionReturnAnnotationOffersTemplatesAndPrimitives(t *testing.T) {
	src := `def f() -> `
	list := completeStarlark(src, Position{Line: 0, Character: 11}, newMotorProvider(), "", "")
	labels := completionLabels(list)
	seen := map[string]bool{}
	for _, l := range labels {
		seen[l] = true
	}
	for _, want := range []string{"Motor", "number"} {
		if !seen[want] {
			t.Errorf("expected %q in return-annotation completion, got %v", want, labels)
		}
	}
	if seen["get_var"] {
		t.Errorf("return annotation should not include builtins, got %v", labels)
	}
}

func TestCompletionParamAnnotationWithPartialIdent(t *testing.T) {
	// `def f(x: Mo` — partial prefix, still offers full list (client
	// filters by prefix).
	src := `def f(x: Mo`
	list := completeStarlark(src, Position{Line: 0, Character: 11}, newMotorProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["Motor"] {
		t.Errorf("expected Motor in partial annotation completion, got %v", completionLabels(list))
	}
}

func TestCompletionParamTypeAnnotationResolvesTemplate(t *testing.T) {
	// `def modeHandler(motor: Motor):` and then `motor.` inside the body
	// should surface the Motor template's fields and methods — not the
	// default builtin list.
	src := `def modeHandler(motor: Motor):
    motor.`
	// Cursor sits right after the dot on line 1.
	list := completeStarlark(src, Position{Line: 1, Character: 10}, newMotorProvider(), "", "")
	labels := completionLabels(list)
	seen := map[string]bool{}
	for _, l := range labels {
		seen[l] = true
	}
	for _, want := range []string{"speed", "running", "start"} {
		if !seen[want] {
			t.Errorf("expected %q in member completion, got %v", want, labels)
		}
	}
	if seen["get_var"] {
		t.Errorf("member completion should not include builtins, got %v", labels)
	}
}

func TestCompletionMemberAccessAfterKeyword(t *testing.T) {
	// Regression: `if motor.` previously failed because beforeDot was
	// `"if motor"` which didn't match any resolver pattern.
	src := `def modeHandler(motor: Motor):
    if motor.`
	list := completeStarlark(src, Position{Line: 1, Character: 13}, newMotorProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	for _, want := range []string{"speed", "running", "start"} {
		if !seen[want] {
			t.Errorf("expected %q in `if motor.` completion, got %v", want, completionLabels(list))
		}
	}
	if seen["get_var"] {
		t.Errorf("`if motor.` should not fall back to builtins, got %v", completionLabels(list))
	}
}

func TestCompletionMemberAccessAfterAssignment(t *testing.T) {
	// `x = get_var("motor1").` — assignment LHS shouldn't leak into the
	// resolver.
	src := `x = get_var("motor1").`
	list := completeStarlark(src, Position{Line: 0, Character: 22}, newMotorProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["speed"] {
		t.Errorf("expected speed after `x = get_var(...).`, got %v", completionLabels(list))
	}
}

func TestCompletionIdentAssignedFromGetVar(t *testing.T) {
	// `m = get_var("motor1")` then `m.` — member completion should
	// follow the assignment.
	src := `def main():
    m = get_var("motor1")
    m.`
	list := completeStarlark(src, Position{Line: 2, Character: 6}, newMotorProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["speed"] {
		t.Errorf("expected speed after ident-assigned get_var, got %v", completionLabels(list))
	}
}

func TestCompletionNonTemplateVariableFallsThrough(t *testing.T) {
	// `counter` is atomic — member completion must not fire; the
	// general list (builtins) should still come back.
	provider := &fakeProvider{
		vars: map[string]*VariableInfo{"counter": {Name: "counter", Datatype: "number"}},
	}
	src := `def main():
    get_var("counter").`
	list := completeStarlark(src, Position{Line: 1, Character: 23}, provider, "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["get_var"] {
		t.Errorf("expected fallback to builtins for atomic var, got %v", completionLabels(list))
	}
}

func TestCompletionWithoutProviderSkipsMemberCompletion(t *testing.T) {
	src := `get_var("motor1").`
	list := completeStarlark(src, Position{Line: 0, Character: 18}, nil, "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["get_var"] {
		t.Errorf("expected builtin list when provider is nil, got %v", completionLabels(list))
	}
}

// ─── Variable-name argument completion ─────────────────────────────────

func newVarsProvider() *fakeProvider {
	return &fakeProvider{
		vars: map[string]*VariableInfo{
			"counter": {Name: "counter", Datatype: "number"},
			"running": {Name: "running", Datatype: "bool"},
			"motor1":  {Name: "motor1", Datatype: "Motor", TemplateName: "Motor"},
		},
		templates: map[string]*TemplateInfo{
			"Motor": {Name: "Motor"},
		},
	}
}

func TestCompletionGetVarArgReturnsVariableNames(t *testing.T) {
	src := `def main():
    get_var("")`
	// Cursor sits between the quotes on line 1: col 13.
	list := completeStarlark(src, Position{Line: 1, Character: 13}, newVarsProvider(), "", "")
	labels := completionLabels(list)
	seen := map[string]bool{}
	for _, l := range labels {
		seen[l] = true
	}
	for _, want := range []string{"counter", "running", "motor1"} {
		if !seen[want] {
			t.Errorf("expected %q in argument completion, got %v", want, labels)
		}
	}
	if seen["get_var"] {
		t.Errorf("argument completion should not include builtins, got %v", labels)
	}
}

func TestCompletionGetNumArgReturnsVariableNames(t *testing.T) {
	src := `get_num("`
	list := completeStarlark(src, Position{Line: 0, Character: 9}, newVarsProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["counter"] {
		t.Errorf("expected counter in get_num arg completion, got %v", completionLabels(list))
	}
	if seen["get_var"] {
		t.Errorf("argument completion should not include builtins, got %v", completionLabels(list))
	}
}

func TestCompletionSetVarArgReturnsVariableNames(t *testing.T) {
	src := `set_var("`
	list := completeStarlark(src, Position{Line: 0, Character: 9}, newVarsProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["counter"] {
		t.Errorf("expected counter in set_var arg completion, got %v", completionLabels(list))
	}
}

func TestCompletionLadderTagArgReturnsVariableNames(t *testing.T) {
	src := `NO("`
	list := completeStarlark(src, Position{Line: 0, Character: 4}, newVarsProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["running"] {
		t.Errorf("expected running in NO() arg completion, got %v", completionLabels(list))
	}
}

func TestCompletionArgDetailShowsType(t *testing.T) {
	src := `get_num("`
	list := completeStarlark(src, Position{Line: 0, Character: 9}, newVarsProvider(), "", "")
	var motorDetail, counterDetail string
	for _, it := range list.Items {
		if it.Label == "motor1" {
			motorDetail = it.Detail
		}
		if it.Label == "counter" {
			counterDetail = it.Detail
		}
	}
	if motorDetail != "Motor" {
		t.Errorf("expected motor1 detail=Motor, got %q", motorDetail)
	}
	if counterDetail != "number" {
		t.Errorf("expected counter detail=number, got %q", counterDetail)
	}
}

func TestCompletionOutsideStringFallsThrough(t *testing.T) {
	src := `get_num(`
	list := completeStarlark(src, Position{Line: 0, Character: 8}, newVarsProvider(), "", "")
	// Cursor is after `(` but not inside a string — should return the
	// full builtin list.
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if !seen["get_var"] {
		t.Errorf("expected builtins when cursor isn't in a string, got %v", completionLabels(list))
	}
}

func TestCompletionSecondArgDoesNotOfferVariables(t *testing.T) {
	// set_var's second arg is a value, not a variable name.
	src := `set_var("counter", "`
	list := completeStarlark(src, Position{Line: 0, Character: 20}, newVarsProvider(), "", "")
	seen := map[string]bool{}
	for _, it := range list.Items {
		seen[it.Label] = true
	}
	if seen["counter"] {
		t.Errorf("variable names should not populate non-first string args, got %v", completionLabels(list))
	}
}

func completionLabels(list CompletionList) []string {
	out := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.Label)
	}
	return out
}
