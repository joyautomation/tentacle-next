//go:build plc || all

package lsp

import (
	"fmt"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc"
	"go.starlark.net/syntax"
)

// ─── Type AST ──────────────────────────────────────────────────────────────

// TypeKind classifies a parsed annotation.
type TypeKind int

const (
	TypeAny TypeKind = iota
	TypeNumber
	TypeBoolean
	TypeString
	TypeNone
	TypeList     // Args[0] = element type
	TypeDict     // Args[0] = key, Args[1] = value
	TypeTuple    // Args[0..n-1] = elements
	TypeUnion    // Args[0..n-1] = members
	TypeTemplate // Name = template identifier
)

// TypeExpr is a parsed annotation. Kind drives comparisons; Name is set for
// templates; Args holds subexpressions for containers and unions.
type TypeExpr struct {
	Kind TypeKind
	Name string
	Args []TypeExpr
}

// String is the human form used in diagnostics.
func (t TypeExpr) String() string {
	switch t.Kind {
	case TypeAny:
		return "any"
	case TypeNumber:
		return "number"
	case TypeBoolean:
		return "boolean"
	case TypeString:
		return "string"
	case TypeNone:
		return "None"
	case TypeTemplate:
		return t.Name
	case TypeList:
		if len(t.Args) == 1 {
			return "list[" + t.Args[0].String() + "]"
		}
		return "list"
	case TypeDict:
		if len(t.Args) == 2 {
			return "dict[" + t.Args[0].String() + ", " + t.Args[1].String() + "]"
		}
		return "dict"
	case TypeTuple:
		parts := make([]string, 0, len(t.Args))
		for _, a := range t.Args {
			parts = append(parts, a.String())
		}
		return "tuple[" + strings.Join(parts, ", ") + "]"
	case TypeUnion:
		parts := make([]string, 0, len(t.Args))
		for _, a := range t.Args {
			parts = append(parts, a.String())
		}
		return strings.Join(parts, " | ")
	}
	return "?"
}

func anyType() TypeExpr     { return TypeExpr{Kind: TypeAny} }
func numberType() TypeExpr  { return TypeExpr{Kind: TypeNumber} }
func booleanType() TypeExpr { return TypeExpr{Kind: TypeBoolean} }
func stringType() TypeExpr  { return TypeExpr{Kind: TypeString} }
func noneType() TypeExpr    { return TypeExpr{Kind: TypeNone} }

// ─── Type parser ───────────────────────────────────────────────────────────

// ParseTypeAnnotation parses a raw annotation string (whatever the user
// wrote after the `:` or after `->` in a def header) into a TypeExpr.
// Unknown or empty annotations resolve to `any`, which is permissive by
// design — the checker never produces false positives when it can't
// confidently understand the annotation.
func ParseTypeAnnotation(s string) TypeExpr {
	s = strings.TrimSpace(s)
	if s == "" {
		return anyType()
	}
	p := typeParser{src: s, pos: 0}
	t, ok := p.parseUnion()
	if !ok {
		return anyType()
	}
	p.skipSpaces()
	if p.pos != len(p.src) {
		return anyType()
	}
	return t
}

type typeParser struct {
	src string
	pos int
}

func (p *typeParser) skipSpaces() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *typeParser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *typeParser) parseUnion() (TypeExpr, bool) {
	p.skipSpaces()
	first, ok := p.parseAtom()
	if !ok {
		return TypeExpr{}, false
	}
	members := []TypeExpr{first}
	for {
		p.skipSpaces()
		if p.peek() != '|' {
			break
		}
		p.pos++
		p.skipSpaces()
		next, ok := p.parseAtom()
		if !ok {
			return TypeExpr{}, false
		}
		members = append(members, next)
	}
	if len(members) == 1 {
		return members[0], true
	}
	return flattenUnion(members), true
}

// parseAtom handles a single type term: a primitive name, a bare
// identifier (template), or a generic application like `list[int]`.
func (p *typeParser) parseAtom() (TypeExpr, bool) {
	p.skipSpaces()
	// Identifier.
	start := p.pos
	for p.pos < len(p.src) && isTypeIdent(p.src[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return TypeExpr{}, false
	}
	name := p.src[start:p.pos]
	p.skipSpaces()

	// Generic application?
	if p.peek() == '[' {
		p.pos++
		args := []TypeExpr{}
		for {
			p.skipSpaces()
			// Allow `...` inside tuple/list for variadic spelling.
			if p.pos+2 < len(p.src) && p.src[p.pos] == '.' && p.src[p.pos+1] == '.' && p.src[p.pos+2] == '.' {
				p.pos += 3
				args = append(args, anyType())
			} else {
				t, ok := p.parseUnion()
				if !ok {
					return TypeExpr{}, false
				}
				args = append(args, t)
			}
			p.skipSpaces()
			if p.peek() == ',' {
				p.pos++
				continue
			}
			if p.peek() == ']' {
				p.pos++
				break
			}
			return TypeExpr{}, false
		}
		return constructGeneric(name, args), true
	}
	return classifyBare(name), true
}

func constructGeneric(name string, args []TypeExpr) TypeExpr {
	switch strings.ToLower(name) {
	case "list":
		if len(args) == 1 {
			return TypeExpr{Kind: TypeList, Args: args}
		}
	case "dict":
		if len(args) == 2 {
			return TypeExpr{Kind: TypeDict, Args: args}
		}
	case "tuple":
		return TypeExpr{Kind: TypeTuple, Args: args}
	case "optional":
		// Optional[T] == T | None.
		if len(args) == 1 {
			return flattenUnion([]TypeExpr{args[0], noneType()})
		}
	case "union":
		return flattenUnion(args)
	}
	// Unknown generic constructor — treat as opaque so we don't invent
	// false positives.
	return anyType()
}

func classifyBare(name string) TypeExpr {
	switch strings.ToLower(name) {
	case "int", "float", "number", "num", "double":
		return numberType()
	case "bool", "boolean":
		return booleanType()
	case "str", "string":
		return stringType()
	case "none", "nonetype", "null":
		return noneType()
	case "any", "object":
		return anyType()
	}
	// Anything else is a template / UDT reference.
	return TypeExpr{Kind: TypeTemplate, Name: name}
}

func flattenUnion(members []TypeExpr) TypeExpr {
	out := make([]TypeExpr, 0, len(members))
	for _, m := range members {
		if m.Kind == TypeUnion {
			out = append(out, m.Args...)
		} else {
			out = append(out, m)
		}
	}
	// Deduplicate (a union of `number | number` is just `number`).
	seen := map[string]bool{}
	uniq := make([]TypeExpr, 0, len(out))
	for _, m := range out {
		key := m.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, m)
	}
	if len(uniq) == 1 {
		return uniq[0]
	}
	return TypeExpr{Kind: TypeUnion, Args: uniq}
}

func isTypeIdent(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '.'
}

// ─── Compatibility ─────────────────────────────────────────────────────────

// IsAssignable reports whether a value whose inferred type is `actual`
// can be passed where the declared type is `expected`. The relation is
// intentionally lax — `any` on either side is accepted, unions match any
// branch, and generics compare element-wise. False positives are far
// worse than false negatives for a linter pass, so when in doubt we say
// "yes".
func IsAssignable(expected, actual TypeExpr) bool {
	if expected.Kind == TypeAny || actual.Kind == TypeAny {
		return true
	}
	// Union on the expected side: actual must match at least one branch.
	if expected.Kind == TypeUnion {
		for _, m := range expected.Args {
			if IsAssignable(m, actual) {
				return true
			}
		}
		return false
	}
	// Union on the actual side: every branch must satisfy expected —
	// otherwise the value could be of a branch that doesn't fit.
	if actual.Kind == TypeUnion {
		for _, m := range actual.Args {
			if !IsAssignable(expected, m) {
				return false
			}
		}
		return true
	}
	if expected.Kind != actual.Kind {
		return false
	}
	switch expected.Kind {
	case TypeNumber, TypeBoolean, TypeString, TypeNone:
		return true
	case TypeTemplate:
		return strings.EqualFold(expected.Name, actual.Name)
	case TypeList:
		if len(expected.Args) == 1 && len(actual.Args) == 1 {
			return IsAssignable(expected.Args[0], actual.Args[0])
		}
		return len(expected.Args) == len(actual.Args)
	case TypeDict:
		if len(expected.Args) != 2 || len(actual.Args) != 2 {
			return true
		}
		return IsAssignable(expected.Args[0], actual.Args[0]) &&
			IsAssignable(expected.Args[1], actual.Args[1])
	case TypeTuple:
		// Variadic tuples (any length) are treated permissively — if the
		// declared type has `tuple[T, ...]` we only check the head.
		if len(expected.Args) != len(actual.Args) {
			return false
		}
		for i := range expected.Args {
			if !IsAssignable(expected.Args[i], actual.Args[i]) {
				return false
			}
		}
		return true
	}
	return true
}

// ─── Expression inference ──────────────────────────────────────────────────

// typeEnv is the per-function view of local bindings. When inferring the
// type of a bare identifier, we first consult `locals`; missing names
// fall through to the default `any`.
type typeEnv struct {
	locals          map[string]TypeExpr
	provider        SymbolProvider
	currentProgram  string
	localDefs       map[string]bool // user-defined def names in current file
	builtinReturns  map[string]TypeExpr
	builtinAcceptsV map[string]bool // builtin names whose arg lists use *args
}

// newTypeEnv seeds the environment with a single-pass scan of top-level
// and current-scope assignments. Shadowing inside inner scopes isn't
// modeled — this is intentionally coarse so we don't promise more than
// we deliver.
func newTypeEnv(file *syntax.File, provider SymbolProvider, currentProgram string) *typeEnv {
	env := &typeEnv{
		locals:          map[string]TypeExpr{},
		provider:        provider,
		currentProgram:  currentProgram,
		localDefs:       map[string]bool{},
		builtinReturns:  builtinReturnTable(),
		builtinAcceptsV: builtinVariadicTable(),
	}
	if file == nil {
		return env
	}
	// Record local def names so we don't misread calls to local helpers
	// as cross-program calls.
	for _, stmt := range file.Stmts {
		if def, ok := stmt.(*syntax.DefStmt); ok && def.Name != nil {
			env.localDefs[def.Name.Name] = true
		}
	}
	// Harvest simple top-level / local assignments: `x = EXPR`.
	syntax.Walk(file, func(n syntax.Node) bool {
		if a, ok := n.(*syntax.AssignStmt); ok && a.Op == syntax.EQ {
			if id, ok := a.LHS.(*syntax.Ident); ok {
				env.locals[id.Name] = env.inferExpr(a.RHS)
			}
		}
		return true
	})
	return env
}

// inferExpr returns the best-guess type of the expression. The inference
// is single-pass and context-free; when confidence drops, it returns
// `any` rather than guessing wrong.
func (e *typeEnv) inferExpr(n syntax.Expr) TypeExpr {
	if n == nil {
		return anyType()
	}
	switch x := n.(type) {
	case *syntax.Literal:
		return literalType(x)
	case *syntax.Ident:
		switch x.Name {
		case "True", "False":
			return booleanType()
		case "None":
			return noneType()
		}
		if t, ok := e.locals[x.Name]; ok {
			return t
		}
		return anyType()
	case *syntax.ListExpr:
		return listExprType(e, x.List)
	case *syntax.DictExpr:
		return dictExprType(e, x)
	case *syntax.TupleExpr:
		parts := make([]TypeExpr, 0, len(x.List))
		for _, el := range x.List {
			parts = append(parts, e.inferExpr(el))
		}
		return TypeExpr{Kind: TypeTuple, Args: parts}
	case *syntax.BinaryExpr:
		return e.inferBinary(x)
	case *syntax.UnaryExpr:
		// `-x`, `not x`, etc. `not` is always boolean; numeric unary
		// returns the operand's type.
		if x.Op == syntax.NOT {
			return booleanType()
		}
		return e.inferExpr(x.X)
	case *syntax.CallExpr:
		return e.inferCall(x)
	case *syntax.DotExpr:
		return e.inferDot(x)
	}
	return anyType()
}

func literalType(l *syntax.Literal) TypeExpr {
	switch l.Token {
	case syntax.INT, syntax.FLOAT:
		return numberType()
	case syntax.STRING, syntax.BYTES:
		return stringType()
	}
	return anyType()
}

func listExprType(e *typeEnv, elems []syntax.Expr) TypeExpr {
	if len(elems) == 0 {
		return TypeExpr{Kind: TypeList, Args: []TypeExpr{anyType()}}
	}
	var unified TypeExpr
	for i, el := range elems {
		t := e.inferExpr(el)
		if i == 0 {
			unified = t
			continue
		}
		if !typesEqual(unified, t) {
			unified = anyType()
			break
		}
	}
	return TypeExpr{Kind: TypeList, Args: []TypeExpr{unified}}
}

func dictExprType(e *typeEnv, x *syntax.DictExpr) TypeExpr {
	if len(x.List) == 0 {
		return TypeExpr{Kind: TypeDict, Args: []TypeExpr{anyType(), anyType()}}
	}
	var k, v TypeExpr
	for i, entry := range x.List {
		de, ok := entry.(*syntax.DictEntry)
		if !ok {
			return anyType()
		}
		kt := e.inferExpr(de.Key)
		vt := e.inferExpr(de.Value)
		if i == 0 {
			k, v = kt, vt
			continue
		}
		if !typesEqual(k, kt) {
			k = anyType()
		}
		if !typesEqual(v, vt) {
			v = anyType()
		}
	}
	return TypeExpr{Kind: TypeDict, Args: []TypeExpr{k, v}}
}

func (e *typeEnv) inferBinary(x *syntax.BinaryExpr) TypeExpr {
	left := e.inferExpr(x.X)
	right := e.inferExpr(x.Y)
	switch x.Op {
	case syntax.PLUS, syntax.MINUS, syntax.STAR, syntax.SLASH, syntax.SLASHSLASH, syntax.PERCENT:
		if x.Op == syntax.PLUS && left.Kind == TypeString && right.Kind == TypeString {
			return stringType()
		}
		if left.Kind == TypeNumber && right.Kind == TypeNumber {
			return numberType()
		}
		return anyType()
	case syntax.EQL, syntax.NEQ, syntax.LT, syntax.GT, syntax.LE, syntax.GE, syntax.IN, syntax.NOT_IN:
		return booleanType()
	}
	return anyType()
}

func (e *typeEnv) inferCall(x *syntax.CallExpr) TypeExpr {
	ident, ok := x.Fn.(*syntax.Ident)
	if !ok {
		return anyType()
	}
	name := ident.Name
	// Special-case `get_var("name")` — resolves to the variable's declared
	// template or primitive datatype.
	if name == "get_var" && len(x.Args) >= 1 && e.provider != nil {
		if lit, ok := x.Args[0].(*syntax.Literal); ok && lit.Token == syntax.STRING {
			if varName, ok := lit.Value.(string); ok {
				if info := e.provider.Variable(varName); info != nil {
					return variableInfoType(info)
				}
			}
		}
		return anyType()
	}
	if rt, ok := e.builtinReturns[name]; ok {
		return rt
	}
	// Local def with no declared return — we don't walk bodies, so assume
	// `any`. Cross-program function? Use its declared return.
	if e.localDefs[name] {
		return anyType()
	}
	if e.provider != nil && name != e.currentProgram {
		if info := e.provider.Function(name); info != nil && info.Returns != nil {
			return ParseTypeAnnotation(info.Returns.Type)
		}
	}
	return anyType()
}

// inferDot handles `expr.field`. When the head is `get_var("x")` or a
// local bound to one, we resolve the template and return the field's
// type. Everything else falls back to `any`.
func (e *typeEnv) inferDot(x *syntax.DotExpr) TypeExpr {
	head := e.inferExpr(x.X)
	if head.Kind != TypeTemplate || e.provider == nil {
		return anyType()
	}
	tmpl := e.provider.Template(head.Name)
	if tmpl == nil {
		return anyType()
	}
	fieldName := x.Name.Name
	for _, f := range tmpl.Fields {
		if f.Name == fieldName {
			return ParseTypeAnnotation(f.Type)
		}
	}
	return anyType()
}

func variableInfoType(info *VariableInfo) TypeExpr {
	if info.TemplateName != "" {
		return TypeExpr{Kind: TypeTemplate, Name: info.TemplateName}
	}
	switch strings.ToLower(info.Datatype) {
	case "bool", "boolean":
		return booleanType()
	case "string", "str":
		return stringType()
	case "number", "int", "float", "num":
		return numberType()
	}
	return anyType()
}

// typesEqual is a shallow equality check (not subtype). Useful when
// unifying list/dict element types — if the second element doesn't
// exactly match the first we fall back to `any`.
func typesEqual(a, b TypeExpr) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == TypeTemplate {
		return strings.EqualFold(a.Name, b.Name)
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if !typesEqual(a.Args[i], b.Args[i]) {
			return false
		}
	}
	return true
}

// ─── Builtin return types ──────────────────────────────────────────────────

// builtinReturnTable maps the handful of builtins whose return type we
// can promise. The rest return `any` implicitly. Keep this list in sync
// with internal/plc/lsp/builtins.go when new typed builtins are added.
func builtinReturnTable() map[string]TypeExpr {
	return map[string]TypeExpr{
		"get_num":  numberType(),
		"get_bool": booleanType(),
		"get_str":  stringType(),
		"abs":      numberType(),
		"clamp":    numberType(),
		"sqrt":     numberType(),
		"pow":      numberType(),
		"min":      numberType(),
		"max":      numberType(),
		"round":    numberType(),
		"floor":    numberType(),
		"ceil":     numberType(),
		"str":      stringType(),
		"int":      numberType(),
		"float":    numberType(),
		"bool":     booleanType(),
		"len":      numberType(),
	}
}

// builtinVariadicTable flags builtins that take `*args` so the checker
// doesn't try to match each argument against a declared type — there
// isn't one to match against.
func builtinVariadicTable() map[string]bool {
	return map[string]bool{
		"log": true, "log_debug": true, "log_info": true,
		"log_warn": true, "log_error": true, "print": true,
	}
}

// ─── Diagnostic emission ───────────────────────────────────────────────────

// checkCallTypes compares each positional / keyword argument's inferred
// type against the declared parameter type. Only emits a diagnostic when
// the mismatch is confident (neither side is `any`).
func checkCallTypes(call *syntax.CallExpr, info *FunctionInfo, env *typeEnv) []Diagnostic {
	if info == nil || env == nil {
		return nil
	}
	// Map param name → index for keyword lookups.
	byName := make(map[string]int, len(info.Params))
	for i, p := range info.Params {
		byName[p.Name] = i
	}
	posIdx := 0
	var diags []Diagnostic
	for _, arg := range call.Args {
		// `*args` / `**kwargs` — skip.
		if _, ok := arg.(*syntax.UnaryExpr); ok {
			continue
		}
		if be, ok := arg.(*syntax.BinaryExpr); ok && be.Op == syntax.EQ {
			kwIdent, ok := be.X.(*syntax.Ident)
			if !ok {
				continue
			}
			pi, ok := byName[kwIdent.Name]
			if !ok {
				continue // unknown keyword — arity pass already flagged.
			}
			if d, ok := typeMismatchDiag(&info.Params[pi], be.Y, env); ok {
				diags = append(diags, d)
			}
			continue
		}
		// Positional.
		if posIdx >= len(info.Params) {
			break // arity pass already flagged.
		}
		p := info.Params[posIdx]
		posIdx++
		if d, ok := typeMismatchDiag(&p, arg, env); ok {
			diags = append(diags, d)
		}
	}
	return diags
}

func typeMismatchDiag(param *FunctionParam, arg syntax.Expr, env *typeEnv) (Diagnostic, bool) {
	expected := ParseTypeAnnotation(param.Type)
	if expected.Kind == TypeAny {
		return Diagnostic{}, false
	}
	actual := env.inferExpr(arg)
	if actual.Kind == TypeAny {
		return Diagnostic{}, false
	}
	if IsAssignable(expected, actual) {
		return Diagnostic{}, false
	}
	start, end := arg.Span()
	msg := fmt.Sprintf(
		"argument %q: expected %s, got %s",
		param.Name, expected.String(), actual.String(),
	)
	return rangeDiag(int(start.Line), int(start.Col)-1, int(end.Line), int(end.Col)-1, msg), true
}

// ─── Return type checking ──────────────────────────────────────────────────

// analyzeReturnTypes walks each annotated `def` and compares the type of
// every `return <expr>` in its body against the declared return type.
// Nested def scopes are handled independently — a return inside an inner
// def is attributed to the inner def's declared return type, not the
// enclosing one.
//
// Dict-literal returns against a template get a targeted key check: each
// literal key is compared to the template's field names, so typos like
// `{"vale": ...}` against `-> Analog:` surface as "unknown field".
func analyzeReturnTypes(file *syntax.File, sigs []plc.DefSignature, provider SymbolProvider, currentProgram string) []Diagnostic {
	if file == nil || len(sigs) == 0 {
		return nil
	}
	env := newTypeEnv(file, provider, currentProgram)

	// There may be multiple defs with the same name (rare, but nested
	// redefinition is legal in Starlark). Match sigs to AST nodes in
	// declaration order so each def gets its own return type.
	sigQueue := map[string][]plc.DefSignature{}
	for _, s := range sigs {
		sigQueue[s.Name] = append(sigQueue[s.Name], s)
	}

	var diags []Diagnostic
	var visit func(stmts []syntax.Stmt)
	visit = func(stmts []syntax.Stmt) {
		for _, stmt := range stmts {
			def, ok := stmt.(*syntax.DefStmt)
			if !ok || def.Name == nil {
				if blk, ok := containerBody(stmt); ok {
					visit(blk)
				}
				continue
			}
			// Pop the next sig matching this def's name.
			var sig plc.DefSignature
			haveSig := false
			if q := sigQueue[def.Name.Name]; len(q) > 0 {
				sig = q[0]
				sigQueue[def.Name.Name] = q[1:]
				haveSig = true
			}
			if haveSig && sig.HasReturn {
				expected := ParseTypeAnnotation(sig.ReturnType)
				if expected.Kind != TypeAny {
					diags = append(diags, checkDefReturns(def, expected, env, provider)...)
				}
			}
			visit(def.Body)
		}
	}
	visit(file.Stmts)
	return diags
}

// checkDefReturns emits diagnostics for every `return` in def's body that
// belongs to def's own scope. Nested defs are skipped — their returns
// target their own declared types.
func checkDefReturns(def *syntax.DefStmt, expected TypeExpr, env *typeEnv, provider SymbolProvider) []Diagnostic {
	var diags []Diagnostic
	for _, stmt := range def.Body {
		syntax.Walk(stmt, func(n syntax.Node) bool {
			if d, ok := n.(*syntax.DefStmt); ok && d != def {
				return false
			}
			if r, ok := n.(*syntax.ReturnStmt); ok {
				diags = append(diags, checkReturnStmt(r, expected, env, provider)...)
			}
			return true
		})
	}
	return diags
}

// checkReturnStmt compares one return statement against the declared
// return type. Returns diagnostics; empty slice when the return is valid
// or the check can't produce a confident verdict.
func checkReturnStmt(ret *syntax.ReturnStmt, expected TypeExpr, env *typeEnv, provider SymbolProvider) []Diagnostic {
	if ret.Result == nil {
		if IsAssignable(expected, noneType()) {
			return nil
		}
		start, end := ret.Span()
		return []Diagnostic{rangeDiag(
			int(start.Line), int(start.Col),
			int(end.Line), int(end.Col),
			fmt.Sprintf("return value missing: expected %s", expected.String()),
		)}
	}
	// Specialized check: dict literal returned where a template is expected
	// — verify each key matches a template field.
	if expected.Kind == TypeTemplate && provider != nil {
		if dict, ok := ret.Result.(*syntax.DictExpr); ok {
			if tmpl := provider.Template(expected.Name); tmpl != nil {
				return checkDictAgainstTemplate(dict, tmpl)
			}
		}
	}
	actual := env.inferExpr(ret.Result)
	if actual.Kind == TypeAny {
		return nil
	}
	if IsAssignable(expected, actual) {
		return nil
	}
	start, end := ret.Result.Span()
	return []Diagnostic{rangeDiag(
		int(start.Line), int(start.Col),
		int(end.Line), int(end.Col),
		fmt.Sprintf("return type mismatch: expected %s, got %s", expected.String(), actual.String()),
	)}
}

// checkDictAgainstTemplate flags dict keys that don't correspond to a
// field on the template. Dynamic (non-literal) keys are ignored — they
// could resolve to anything at runtime.
func checkDictAgainstTemplate(dict *syntax.DictExpr, tmpl *TemplateInfo) []Diagnostic {
	fieldSet := make(map[string]bool, len(tmpl.Fields))
	for _, f := range tmpl.Fields {
		fieldSet[f.Name] = true
	}
	var diags []Diagnostic
	for _, entry := range dict.List {
		de, ok := entry.(*syntax.DictEntry)
		if !ok {
			continue
		}
		lit, ok := de.Key.(*syntax.Literal)
		if !ok || lit.Token != syntax.STRING {
			continue
		}
		keyName, ok := lit.Value.(string)
		if !ok || keyName == "" {
			continue
		}
		if fieldSet[keyName] {
			continue
		}
		start, end := de.Key.Span()
		diags = append(diags, rangeDiag(
			int(start.Line), int(start.Col),
			int(end.Line), int(end.Col),
			fmt.Sprintf("unknown field %q: template %q has no field by that name", keyName, tmpl.Name),
		))
	}
	return diags
}

// ─── Unbound identifier checking ──────────────────────────────────────────
//
// analyzeUnboundNames flags every identifier *read* that doesn't resolve to
// something bound in an enclosing scope, a builtin, or a provider-exposed
// cross-program name. The pass is deliberately conservative: when in doubt
// (e.g. we can't tell if a name is a binding or a read) it stays silent.
// Severity is warning because a false positive would punish the user for
// perfectly valid code the checker simply doesn't understand.
//
// The one tricky bit is scope. Starlark is function-scoped, so forward
// references inside a def body are legal — we do a pre-pass over each
// function to collect every binding before walking reads. Comprehensions
// and lambdas each introduce their own nested scope.
func analyzeUnboundNames(file *syntax.File, provider SymbolProvider) []Diagnostic {
	if file == nil {
		return nil
	}
	external := knownExternalNames(provider)
	module := &nameScope{names: map[string]bool{}, parent: &nameScope{names: external}}
	collectStmtBindings(file.Stmts, module)
	var diags []Diagnostic
	walkReads(file.Stmts, module, &diags)
	return diags
}

// nameScope is one level of the lexical scope stack used by the unbound
// identifier checker. `names` holds bindings introduced at this level;
// `parent` chains up to enclosing scopes.
type nameScope struct {
	names  map[string]bool
	parent *nameScope
}

func (s *nameScope) has(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.names[name] {
			return true
		}
	}
	return false
}

// knownExternalNames is the set of identifiers the user may reference
// that aren't bound locally — Starlark keywords, our PLC builtin catalog,
// Starlark's built-in functions, and every cross-program symbol the
// provider knows about.
func knownExternalNames(provider SymbolProvider) map[string]bool {
	names := map[string]bool{
		"True": true, "False": true, "None": true,
	}
	for _, b := range catalog {
		names[b.Name] = true
	}
	// Starlark core builtins that aren't in our PLC catalog.
	for _, n := range []string{
		"any", "all", "bool", "bytes", "chr", "dict", "dir", "enumerate",
		"fail", "float", "getattr", "hasattr", "hash", "int", "len", "list",
		"max", "min", "ord", "print", "range", "repr", "reversed", "set",
		"sorted", "str", "tuple", "type", "zip",
	} {
		names[n] = true
	}
	if provider != nil {
		for _, n := range provider.FunctionNames() {
			names[n] = true
		}
		for _, n := range provider.VariableNames() {
			names[n] = true
		}
		for _, n := range provider.TemplateNames() {
			names[n] = true
		}
	}
	return names
}

// collectStmtBindings records every name bound at the current scope level
// without descending into nested defs, lambdas, or comprehensions (each
// starts its own scope). Starlark's function-scoped semantics mean forward
// references inside a function body are legal, so callers pre-populate
// the scope before walking reads.
func collectStmtBindings(stmts []syntax.Stmt, s *nameScope) {
	for _, stmt := range stmts {
		switch st := stmt.(type) {
		case *syntax.AssignStmt:
			collectLHSBindings(st.LHS, s)
		case *syntax.DefStmt:
			if st.Name != nil {
				s.names[st.Name.Name] = true
			}
		case *syntax.ForStmt:
			collectLHSBindings(st.Vars, s)
			collectStmtBindings(st.Body, s)
		case *syntax.IfStmt:
			collectStmtBindings(st.True, s)
			collectStmtBindings(st.False, s)
		case *syntax.WhileStmt:
			collectStmtBindings(st.Body, s)
		case *syntax.LoadStmt:
			for _, to := range st.To {
				if to != nil {
					s.names[to.Name] = true
				}
			}
		}
	}
}

// collectLHSBindings pulls bound names out of an assignment/for-clause
// target. Handles plain idents, tuple/list unpacking, and parenthesized
// targets. Subscript and dot targets don't introduce new names.
func collectLHSBindings(e syntax.Expr, s *nameScope) {
	switch x := e.(type) {
	case *syntax.Ident:
		s.names[x.Name] = true
	case *syntax.TupleExpr:
		for _, el := range x.List {
			collectLHSBindings(el, s)
		}
	case *syntax.ListExpr:
		for _, el := range x.List {
			collectLHSBindings(el, s)
		}
	case *syntax.ParenExpr:
		collectLHSBindings(x.X, s)
	}
}

// bindParams seeds a function/lambda scope with its parameter names,
// including defaulted params (`x=1`) and variadics (`*args`, `**kwargs`).
func bindParams(params []syntax.Expr, s *nameScope) {
	for _, p := range params {
		switch x := p.(type) {
		case *syntax.Ident:
			s.names[x.Name] = true
		case *syntax.BinaryExpr:
			if x.Op == syntax.EQ {
				if id, ok := x.X.(*syntax.Ident); ok {
					s.names[id.Name] = true
				}
			}
		case *syntax.UnaryExpr:
			if id, ok := x.X.(*syntax.Ident); ok {
				s.names[id.Name] = true
			}
		}
	}
}

// walkParamDefaults checks name references inside default-value expressions.
// Defaults are evaluated in the enclosing scope, not the function's own.
func walkParamDefaults(params []syntax.Expr, s *nameScope, diags *[]Diagnostic) {
	for _, p := range params {
		if be, ok := p.(*syntax.BinaryExpr); ok && be.Op == syntax.EQ {
			walkExprReads(be.Y, s, diags)
		}
	}
}

func walkReads(stmts []syntax.Stmt, s *nameScope, diags *[]Diagnostic) {
	for _, stmt := range stmts {
		walkStmtReads(stmt, s, diags)
	}
}

func walkStmtReads(stmt syntax.Stmt, s *nameScope, diags *[]Diagnostic) {
	switch st := stmt.(type) {
	case *syntax.AssignStmt:
		walkLHSReads(st.LHS, s, diags)
		walkExprReads(st.RHS, s, diags)
		// Augmented assignment (`x += 1`) reads the LHS ident as well.
		if st.Op != syntax.EQ {
			if id, ok := st.LHS.(*syntax.Ident); ok {
				checkIdent(id, s, diags)
			}
		}
	case *syntax.DefStmt:
		fnScope := &nameScope{names: map[string]bool{}, parent: s}
		bindParams(st.Params, fnScope)
		collectStmtBindings(st.Body, fnScope)
		walkParamDefaults(st.Params, s, diags)
		walkReads(st.Body, fnScope, diags)
	case *syntax.IfStmt:
		walkExprReads(st.Cond, s, diags)
		walkReads(st.True, s, diags)
		walkReads(st.False, s, diags)
	case *syntax.ForStmt:
		walkExprReads(st.X, s, diags)
		walkReads(st.Body, s, diags)
	case *syntax.WhileStmt:
		walkExprReads(st.Cond, s, diags)
		walkReads(st.Body, s, diags)
	case *syntax.ReturnStmt:
		if st.Result != nil {
			walkExprReads(st.Result, s, diags)
		}
	case *syntax.ExprStmt:
		walkExprReads(st.X, s, diags)
	}
}

// walkLHSReads visits read-positions inside an assignment target. A bare
// ident on the LHS is a pure write (skip it); a subscript or dot target's
// base is read so the assignment can take effect.
func walkLHSReads(e syntax.Expr, s *nameScope, diags *[]Diagnostic) {
	switch x := e.(type) {
	case *syntax.Ident:
		// Pure write — not a read.
	case *syntax.TupleExpr:
		for _, el := range x.List {
			walkLHSReads(el, s, diags)
		}
	case *syntax.ListExpr:
		for _, el := range x.List {
			walkLHSReads(el, s, diags)
		}
	case *syntax.ParenExpr:
		walkLHSReads(x.X, s, diags)
	case *syntax.IndexExpr:
		walkExprReads(x.X, s, diags)
		walkExprReads(x.Y, s, diags)
	case *syntax.DotExpr:
		walkExprReads(x.X, s, diags)
	default:
		walkExprReads(e, s, diags)
	}
}

func walkExprReads(e syntax.Expr, s *nameScope, diags *[]Diagnostic) {
	if e == nil {
		return
	}
	switch x := e.(type) {
	case *syntax.Ident:
		checkIdent(x, s, diags)
	case *syntax.Literal:
		// literal — no names
	case *syntax.BinaryExpr:
		walkExprReads(x.X, s, diags)
		walkExprReads(x.Y, s, diags)
	case *syntax.UnaryExpr:
		walkExprReads(x.X, s, diags)
	case *syntax.ParenExpr:
		walkExprReads(x.X, s, diags)
	case *syntax.CallExpr:
		walkCallReads(x, s, diags)
	case *syntax.DotExpr:
		walkExprReads(x.X, s, diags)
	case *syntax.ListExpr:
		for _, el := range x.List {
			walkExprReads(el, s, diags)
		}
	case *syntax.TupleExpr:
		for _, el := range x.List {
			walkExprReads(el, s, diags)
		}
	case *syntax.DictExpr:
		for _, entry := range x.List {
			if de, ok := entry.(*syntax.DictEntry); ok {
				walkExprReads(de.Key, s, diags)
				walkExprReads(de.Value, s, diags)
			}
		}
	case *syntax.DictEntry:
		walkExprReads(x.Key, s, diags)
		walkExprReads(x.Value, s, diags)
	case *syntax.IndexExpr:
		walkExprReads(x.X, s, diags)
		walkExprReads(x.Y, s, diags)
	case *syntax.SliceExpr:
		walkExprReads(x.X, s, diags)
		walkExprReads(x.Lo, s, diags)
		walkExprReads(x.Hi, s, diags)
		walkExprReads(x.Step, s, diags)
	case *syntax.CondExpr:
		walkExprReads(x.Cond, s, diags)
		walkExprReads(x.True, s, diags)
		walkExprReads(x.False, s, diags)
	case *syntax.LambdaExpr:
		lambdaScope := &nameScope{names: map[string]bool{}, parent: s}
		bindParams(x.Params, lambdaScope)
		walkParamDefaults(x.Params, s, diags)
		walkExprReads(x.Body, lambdaScope, diags)
	case *syntax.Comprehension:
		// A comprehension's for-clause targets are visible in every
		// following clause and in the body. Pre-collect them so order
		// doesn't matter; the X expression of each for-clause itself is
		// evaluated in the enclosing comprehension scope.
		compScope := &nameScope{names: map[string]bool{}, parent: s}
		for _, clause := range x.Clauses {
			if fc, ok := clause.(*syntax.ForClause); ok {
				collectLHSBindings(fc.Vars, compScope)
			}
		}
		for _, clause := range x.Clauses {
			switch c := clause.(type) {
			case *syntax.ForClause:
				walkExprReads(c.X, compScope, diags)
			case *syntax.IfClause:
				walkExprReads(c.Cond, compScope, diags)
			}
		}
		walkExprReads(x.Body, compScope, diags)
	}
}

// walkCallReads walks the args of a call, treating the `x` in `foo(x=1)`
// as a parameter name on the callee (not a read) but still checking the
// value and any variadic spreads.
func walkCallReads(call *syntax.CallExpr, s *nameScope, diags *[]Diagnostic) {
	walkExprReads(call.Fn, s, diags)
	for _, arg := range call.Args {
		if be, ok := arg.(*syntax.BinaryExpr); ok && be.Op == syntax.EQ {
			walkExprReads(be.Y, s, diags)
			continue
		}
		walkExprReads(arg, s, diags)
	}
}

func checkIdent(id *syntax.Ident, s *nameScope, diags *[]Diagnostic) {
	if id == nil || id.Name == "" {
		return
	}
	if s.has(id.Name) {
		return
	}
	start, end := id.Span()
	d := rangeDiag(
		int(start.Line), int(start.Col),
		int(end.Line), int(end.Col),
		fmt.Sprintf("unknown name %q", id.Name),
	)
	d.Severity = SeverityWarning
	*diags = append(*diags, d)
}

// containerBody returns the child statement list of a compound statement
// so we can recurse into nested defs that live inside control flow.
func containerBody(stmt syntax.Stmt) ([]syntax.Stmt, bool) {
	switch s := stmt.(type) {
	case *syntax.IfStmt:
		body := append([]syntax.Stmt{}, s.True...)
		body = append(body, s.False...)
		return body, true
	case *syntax.ForStmt:
		return s.Body, true
	case *syntax.WhileStmt:
		return s.Body, true
	}
	return nil, false
}
