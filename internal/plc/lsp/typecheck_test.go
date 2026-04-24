//go:build plc || all

package lsp

import (
	"strings"
	"testing"
)

func TestParseTypeAnnotation(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"int", "number"},
		{"str", "string"},
		{"bool", "boolean"},
		{"None", "None"},
		{"any", "any"},
		{"Motor", "Motor"},
		{"list[int]", "list[number]"},
		{"dict[str, int]", "dict[string, number]"},
		{"tuple[int, str]", "tuple[number, string]"},
		{"Optional[int]", "number | None"},
		{"int | str", "number | string"},
		{"int | str | None", "number | string | None"},
		{"Union[int, str]", "number | string"},
		{"list[Motor]", "list[Motor]"},
		{"", "any"},
	}
	for _, tc := range cases {
		got := ParseTypeAnnotation(tc.in).String()
		if got != tc.want {
			t.Errorf("ParseTypeAnnotation(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsAssignable(t *testing.T) {
	parse := ParseTypeAnnotation
	cases := []struct {
		expected, actual string
		want             bool
	}{
		{"int", "int", true},
		{"int", "str", false},
		{"any", "int", true},
		{"int", "any", true},
		{"int | str", "int", true},
		{"int | str", "bool", false},
		{"Optional[int]", "None", true},
		{"Optional[int]", "int", true},
		{"Optional[int]", "str", false},
		{"list[int]", "list[int]", true},
		{"list[int]", "list[str]", false},
		{"list[any]", "list[int]", true},
		{"dict[str, int]", "dict[str, int]", true},
		{"dict[str, int]", "dict[int, int]", false},
		{"Motor", "Motor", true},
		{"Motor", "Pump", false},
	}
	for _, tc := range cases {
		got := IsAssignable(parse(tc.expected), parse(tc.actual))
		if got != tc.want {
			t.Errorf("IsAssignable(%q, %q) = %v; want %v",
				tc.expected, tc.actual, got, tc.want)
		}
	}
}

func newTypedProvider() *fakeProvider {
	return &fakeProvider{
		vars: map[string]*VariableInfo{
			"count":  {Name: "count", Datatype: "number"},
			"label":  {Name: "label", Datatype: "string"},
			"pump1":  {Name: "pump1", TemplateName: "Motor"},
		},
		templates: map[string]*TemplateInfo{
			"Motor": {
				Name: "Motor",
				Fields: []TemplateField{
					{Name: "speed", Type: "number"},
					{Name: "running", Type: "bool"},
				},
			},
		},
		fns: map[string]*FunctionInfo{
			"check": {
				Name:         "check",
				Program:      "check",
				HasSignature: true,
				Params: []FunctionParam{
					{Name: "thing", Type: "str", Required: true},
				},
				Returns: &FunctionReturn{Type: "bool"},
			},
			"scale": {
				Name:         "scale",
				Program:      "scale",
				HasSignature: true,
				Params: []FunctionParam{
					{Name: "x", Type: "number", Required: true},
					{Name: "factor", Type: "number", Required: true},
				},
				Returns: &FunctionReturn{Type: "number"},
			},
			"pick": {
				Name:         "pick",
				Program:      "pick",
				HasSignature: true,
				Params: []FunctionParam{
					{Name: "items", Type: "list[str]", Required: true},
				},
			},
			"lookup": {
				Name:         "lookup",
				Program:      "lookup",
				HasSignature: true,
				Params: []FunctionParam{
					{Name: "key", Type: "int | str", Required: true},
				},
			},
			"drive": {
				Name:         "drive",
				Program:      "drive",
				HasSignature: true,
				Params: []FunctionParam{
					{Name: "m", Type: "Motor", Required: true},
				},
			},
		},
	}
}

func findTypeDiag(diags []Diagnostic, substr string) *Diagnostic {
	for i, d := range diags {
		if strings.Contains(d.Message, substr) {
			return &diags[i]
		}
	}
	return nil
}

func TestTypeCheckFlagsLiteralMismatch(t *testing.T) {
	src := "def main():\n    check(123)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"thing\""); d == nil {
		t.Fatalf("expected type mismatch on thing, got %+v", diags)
	}
}

func TestTypeCheckAcceptsMatchingLiteral(t *testing.T) {
	src := "def main():\n    check(\"ok\")\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("did not expect type diagnostic, got %q", d.Message)
	}
}

func TestTypeCheckFlagsKeywordMismatch(t *testing.T) {
	src := "def main():\n    check(thing=42)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"thing\""); d == nil {
		t.Fatalf("expected kw type mismatch, got %+v", diags)
	}
}

func TestTypeCheckFlagsListElementType(t *testing.T) {
	src := "def main():\n    pick([1, 2, 3])\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"items\""); d == nil {
		t.Fatalf("expected list element mismatch, got %+v", diags)
	}
}

func TestTypeCheckAcceptsMatchingList(t *testing.T) {
	src := "def main():\n    pick([\"a\", \"b\"])\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("did not expect diagnostic, got %q", d.Message)
	}
}

func TestTypeCheckAcceptsUnionBranch(t *testing.T) {
	src := "def main():\n    lookup(5)\n    lookup(\"x\")\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("did not expect diagnostic for union branch, got %q", d.Message)
	}
}

func TestTypeCheckRejectsUnionNonMember(t *testing.T) {
	src := "def main():\n    lookup(True)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"key\""); d == nil {
		t.Fatalf("expected union mismatch, got %+v", diags)
	}
}

func TestTypeCheckUsesGetVarType(t *testing.T) {
	// get_var returns number -> passing to check(str) should flag.
	src := "def main():\n    check(get_var(\"count\"))\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"thing\""); d == nil {
		t.Fatalf("expected mismatch from get_var number -> string, got %+v", diags)
	}
}

func TestTypeCheckAcceptsGetVarMatching(t *testing.T) {
	src := "def main():\n    scale(get_var(\"count\"), 2)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("did not expect diagnostic, got %q", d.Message)
	}
}

func TestTypeCheckUsesTemplateVariable(t *testing.T) {
	src := "def main():\n    drive(get_var(\"pump1\"))\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("did not expect diagnostic for matching template, got %q", d.Message)
	}
}

func TestTypeCheckRejectsWrongTemplate(t *testing.T) {
	src := "def main():\n    drive(get_var(\"count\"))\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"m\""); d == nil {
		t.Fatalf("expected template mismatch, got %+v", diags)
	}
}

func TestTypeCheckAnnotatedSource(t *testing.T) {
	// The call site uses annotations in its own header — they should
	// be stripped before parsing so the checker can still look at args.
	src := "def main(x: int) -> None:\n    check(42)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"thing\""); d == nil {
		t.Fatalf("expected number->string mismatch, got %+v", diags)
	}
}

func TestTypeCheckInferLocalAssignment(t *testing.T) {
	src := "def main():\n    v = 1 + 2\n    check(v)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument \"thing\""); d == nil {
		t.Fatalf("expected mismatch from inferred local, got %+v", diags)
	}
}

func TestTypeCheckSkipsWhenActualAny(t *testing.T) {
	// unknown identifier → any → no diagnostic.
	src := "def main():\n    check(something_unknown)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "argument"); d != nil {
		t.Errorf("should not flag when actual is any, got %q", d.Message)
	}
}

// ─── Return type checking ──────────────────────────────────────────────────

func TestReturnTypeFlagsPrimitiveMismatch(t *testing.T) {
	src := "def main() -> bool:\n    return 42\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return type mismatch"); d == nil {
		t.Fatalf("expected return type mismatch, got %+v", diags)
	}
}

func TestReturnTypeAcceptsMatchingPrimitive(t *testing.T) {
	src := "def main() -> bool:\n    return True\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return type mismatch"); d != nil {
		t.Errorf("did not expect mismatch, got %q", d.Message)
	}
}

func TestReturnTypeFlagsUnknownDictKey(t *testing.T) {
	// "speed" is valid on Motor; "velocity" is not — the typo should flag.
	src := "def build() -> Motor:\n    return {\"speed\": 10, \"velocity\": 5}\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown field"); d == nil {
		t.Fatalf("expected unknown field diagnostic, got %+v", diags)
	}
}

func TestReturnTypeAcceptsValidDictKeys(t *testing.T) {
	src := "def build() -> Motor:\n    return {\"speed\": 10, \"running\": True}\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown field"); d != nil {
		t.Errorf("did not expect unknown field, got %q", d.Message)
	}
	if d := findTypeDiag(diags, "return type mismatch"); d != nil {
		t.Errorf("did not expect mismatch, got %q", d.Message)
	}
}

func TestReturnTypeFlagsMissingValueForNonNone(t *testing.T) {
	src := "def main() -> bool:\n    return\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return value missing"); d == nil {
		t.Fatalf("expected missing return value diagnostic, got %+v", diags)
	}
}

func TestReturnTypeAcceptsBareReturnForOptional(t *testing.T) {
	src := "def main() -> Optional[bool]:\n    return\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return value missing"); d != nil {
		t.Errorf("Optional should allow bare return, got %q", d.Message)
	}
}

func TestReturnTypeNestedDefHasOwnScope(t *testing.T) {
	// Inner def returns a number; its declared return is number. Outer def
	// declares bool. The inner's return must not be attributed to outer.
	src := "def outer() -> bool:\n" +
		"    def inner() -> number:\n" +
		"        return 5\n" +
		"    return True\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return type mismatch"); d != nil {
		t.Errorf("nested def return must not bleed to outer, got %q", d.Message)
	}
}

// ─── Unbound identifier checking ──────────────────────────────────────────

func TestUnboundFlagsUnknownInReturnExpr(t *testing.T) {
	// Screenshot case: param is `ai` but user wrote `analog.VALUE` — the
	// head `analog` is unbound and should surface as "unknown name".
	src := "def f(ai) -> Motor:\n    return {\"speed\": analog.VALUE}\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name \"analog\""); d == nil {
		t.Fatalf("expected unknown name diagnostic on analog, got %+v", diags)
	}
}

func TestUnboundAcceptsParamReference(t *testing.T) {
	src := "def f(x):\n    return x\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("did not expect unknown name, got %q", d.Message)
	}
}

func TestUnboundAcceptsBuiltin(t *testing.T) {
	src := "def f():\n    log(\"hi\")\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("did not expect unknown name, got %q", d.Message)
	}
}

func TestUnboundAcceptsForwardReferenceInFunction(t *testing.T) {
	// Starlark is function-scoped; a name bound anywhere in the function
	// is considered in-scope throughout — a linter pass shouldn't flag
	// statically legal code even if the reference order looks odd.
	src := "def f():\n    x = y\n    y = 1\n    return x\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("forward ref should be allowed, got %q", d.Message)
	}
}

func TestUnboundAcceptsComprehensionTarget(t *testing.T) {
	src := "def f():\n    return [i*2 for i in range(10)]\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("comprehension target should be bound, got %q", d.Message)
	}
}

func TestUnboundAcceptsForLoopTarget(t *testing.T) {
	src := "def f():\n    for x in range(3):\n        log(x)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("for target should be bound, got %q", d.Message)
	}
}

func TestUnboundAcceptsTupleUnpacking(t *testing.T) {
	src := "def f():\n    pair = (1, 2)\n    a, b = pair\n    log(a)\n    log(b)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("tuple unpacking should bind both names, got %q", d.Message)
	}
}

func TestUnboundAcceptsCrossProgramFunction(t *testing.T) {
	// `check` is a provider-exposed cross-program function.
	src := "def main():\n    check(\"hello\")\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("cross-program fn should be known, got %q", d.Message)
	}
}

func TestUnboundAcceptsProviderVariable(t *testing.T) {
	// `count` is a provider-exposed variable name — users can reference
	// it directly in programs (e.g. for get_var calls by name).
	src := "def main():\n    return count\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name"); d != nil {
		t.Errorf("provider var should be known, got %q", d.Message)
	}
}

func TestUnboundFlagsTypoedBuiltin(t *testing.T) {
	src := "def f():\n    lag(\"hi\")\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name \"lag\""); d == nil {
		t.Fatalf("expected unknown name for typo'd builtin, got %+v", diags)
	}
}

func TestUnboundFlagsInnerParamLeakToOuter(t *testing.T) {
	// Inner def's param `x` must not be visible from outer's body.
	src := "def outer():\n" +
		"    def inner(x):\n" +
		"        return x\n" +
		"    return x\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name \"x\""); d == nil {
		t.Fatalf("expected inner param not visible from outer, got %+v", diags)
	}
}

func TestUnboundDotExpressionFieldNotChecked(t *testing.T) {
	// In `a.b.c`, only `a` is a name reference. Fields `b` and `c` must
	// never be flagged — they belong to the resolved object's shape.
	src := "def f(m):\n    return m.nonexistent.chain\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown name \"nonexistent\"") ||
			strings.Contains(d.Message, "unknown name \"chain\"") {
			t.Errorf("dot field should not be checked: %q", d.Message)
		}
	}
}

func TestUnboundKeywordArgNameNotChecked(t *testing.T) {
	// In `scale(x=1, factor=2)`, `x`/`factor` are param names on the
	// callee, not local reads. They must not be flagged.
	src := "def main():\n    scale(x=1, factor=2)\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "unknown name \"x\""); d != nil {
		t.Errorf("kwarg name must not be flagged: %q", d.Message)
	}
	if d := findTypeDiag(diags, "unknown name \"factor\""); d != nil {
		t.Errorf("kwarg name must not be flagged: %q", d.Message)
	}
}

func TestUnboundOnlyRunsWithProvider(t *testing.T) {
	// Without a provider we have no authoritative cross-program name set,
	// so the pass should stay silent to avoid false positives.
	src := "def f():\n    return totally_unknown\n"
	diags := Analyze(src, "starlark")
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown name") {
			t.Errorf("unbound pass must not run without provider: %q", d.Message)
		}
	}
}

func TestReturnTypeWalksControlFlow(t *testing.T) {
	// `return` inside `if` still belongs to the enclosing def.
	src := "def main() -> bool:\n" +
		"    if True:\n" +
		"        return 99\n" +
		"    return True\n"
	diags := AnalyzeWithProvider(src, "starlark", newTypedProvider(), "")
	if d := findTypeDiag(diags, "return type mismatch"); d == nil {
		t.Fatalf("expected mismatch for `return 99` inside if, got %+v", diags)
	}
}
