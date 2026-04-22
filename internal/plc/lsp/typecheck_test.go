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
