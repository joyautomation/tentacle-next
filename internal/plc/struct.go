//go:build plc || all

package plc

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// StructValue is the runtime representation of a template-typed PLC
// variable. It implements starlark.Value + HasAttrs + HasSetField so
// programs can read/write fields and call bound methods.
//
// Dot-access dispatches in two steps: if `name` is a field it returns
// the field value; otherwise if `name` is a template method it returns
// a bound callable closing over the struct. Free-function style
// (`start(motor)`) is enabled separately by RegisterTemplateMethods.
type StructValue struct {
	engine   *Engine
	template *itypes.PlcTemplate

	mu     sync.RWMutex
	fields map[string]starlark.Value
	frozen bool
}

// NewStruct builds a StructValue instance. Missing fields get template
// defaults; unknown fields in `values` are rejected.
func (e *Engine) NewStruct(templateName string, values map[string]starlark.Value) (*StructValue, error) {
	tmpl, ok := e.getTemplate(templateName)
	if !ok {
		return nil, fmt.Errorf("unknown template %q", templateName)
	}

	knownFields := make(map[string]*itypes.PlcTemplateField, len(tmpl.Fields))
	for i := range tmpl.Fields {
		knownFields[tmpl.Fields[i].Name] = &tmpl.Fields[i]
	}
	for name := range values {
		if _, ok := knownFields[name]; !ok {
			return nil, fmt.Errorf("template %q has no field %q", templateName, name)
		}
	}

	s := &StructValue{
		engine:   e,
		template: tmpl,
		fields:   make(map[string]starlark.Value, len(tmpl.Fields)),
	}
	for _, f := range tmpl.Fields {
		if v, ok := values[f.Name]; ok {
			s.fields[f.Name] = v
			continue
		}
		s.fields[f.Name] = defaultStarlarkValue(&f)
	}
	return s, nil
}

// defaultStarlarkValue derives the zero/default Starlark value for a
// template field. A field without an explicit default gets the
// language-appropriate zero (0, "", False, []) for primitives, or None
// for nested struct types until constructed.
func defaultStarlarkValue(f *itypes.PlcTemplateField) starlark.Value {
	if f.Default != nil {
		return goToStarlark(f.Default)
	}
	base := f.Type
	collection := ""
	if strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
		collection = "array"
	} else if strings.HasSuffix(base, "{}") {
		base = strings.TrimSuffix(base, "{}")
		collection = "record"
	}
	if collection == "array" {
		return starlark.NewList(nil)
	}
	if collection == "record" {
		return starlark.NewDict(0)
	}
	switch base {
	case "bool", "boolean":
		return starlark.False
	case "number":
		return starlark.Float(0)
	case "string":
		return starlark.String("")
	case "bytes":
		return starlark.Bytes("")
	default:
		// Nested template — no instance yet.
		return starlark.None
	}
}

// ── starlark.Value interface ─────────────────────────────────────────────

func (s *StructValue) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parts := make([]string, 0, len(s.fields))
	for _, f := range s.template.Fields {
		v := s.fields[f.Name]
		parts = append(parts, fmt.Sprintf("%s=%s", f.Name, v.String()))
	}
	return fmt.Sprintf("%s(%s)", s.template.Name, strings.Join(parts, ", "))
}

func (s *StructValue) Type() string         { return s.template.Name }
func (s *StructValue) Truth() starlark.Bool { return starlark.True }
func (s *StructValue) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", s.template.Name)
}

func (s *StructValue) Freeze() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		return
	}
	s.frozen = true
	for _, v := range s.fields {
		v.Freeze()
	}
}

// MarshalJSON emits a struct value as a flat JSON object with a `_type`
// discriminator, so downstream consumers (MQTT, history, HMI) can both
// read field values directly and identify the template. Nested
// StructValues recurse through their own MarshalJSON; arrays/records of
// structs nest naturally via json.Marshal.
func (s *StructValue) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]interface{}, len(s.fields)+1)
	out["_type"] = s.template.Name
	for _, f := range s.template.Fields {
		out[f.Name] = starlarkToGo(s.fields[f.Name])
	}
	return json.Marshal(out)
}

// ── HasAttrs / HasSetField ────────────────────────────────────────────────

// Attr returns the value bound to `name` on this struct.
// Field lookups hit storage directly; method lookups return a bound
// callable that passes the struct as the first argument.
func (s *StructValue) Attr(name string) (starlark.Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if v, ok := s.fields[name]; ok {
		return v, nil
	}
	for _, m := range s.template.Methods {
		if m.Name == name {
			fn, err := s.engine.lookupFunction(m.Function.Module, m.Function.Name)
			if err != nil {
				return nil, fmt.Errorf("method %s.%s: %w", s.template.Name, name, err)
			}
			return &boundMethod{recv: s, name: name, fn: fn}, nil
		}
	}
	return nil, nil // starlark treats nil,nil as "no such attr"
}

func (s *StructValue) AttrNames() []string {
	names := make([]string, 0, len(s.template.Fields)+len(s.template.Methods))
	for _, f := range s.template.Fields {
		names = append(names, f.Name)
	}
	for _, m := range s.template.Methods {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

// SetField assigns a value to a struct field. Refuses unknown fields
// and refuses type-incompatible assignments; the editor can surface
// these as diagnostics.
func (s *StructValue) SetField(name string, val starlark.Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.frozen {
		return fmt.Errorf("cannot assign to field of frozen %s", s.template.Name)
	}

	var field *itypes.PlcTemplateField
	for i := range s.template.Fields {
		if s.template.Fields[i].Name == name {
			field = &s.template.Fields[i]
			break
		}
	}
	if field == nil {
		return starlark.NoSuchAttrError(fmt.Sprintf("%s has no field or method %q", s.template.Name, name))
	}
	if err := checkFieldAssignable(field, val); err != nil {
		return err
	}
	s.fields[name] = val
	return nil
}

// checkFieldAssignable enforces basic type compatibility on field
// writes. Nested template fields accept any StructValue whose template
// name matches; we rely on the validator to enforce template existence.
func checkFieldAssignable(f *itypes.PlcTemplateField, val starlark.Value) error {
	base := f.Type
	collection := ""
	if strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
		collection = "array"
	} else if strings.HasSuffix(base, "{}") {
		base = strings.TrimSuffix(base, "{}")
		collection = "record"
	}

	switch collection {
	case "array":
		if _, ok := val.(*starlark.List); !ok {
			return fmt.Errorf("field %q expects %s, got %s", f.Name, f.Type, val.Type())
		}
		return nil
	case "record":
		if _, ok := val.(*starlark.Dict); !ok {
			return fmt.Errorf("field %q expects %s, got %s", f.Name, f.Type, val.Type())
		}
		return nil
	}

	switch base {
	case "bool", "boolean":
		if _, ok := val.(starlark.Bool); !ok {
			return fmt.Errorf("field %q expects bool, got %s", f.Name, val.Type())
		}
	case "number":
		switch val.(type) {
		case starlark.Int, starlark.Float:
			// ok
		default:
			return fmt.Errorf("field %q expects number, got %s", f.Name, val.Type())
		}
	case "string":
		if _, ok := val.(starlark.String); !ok {
			return fmt.Errorf("field %q expects string, got %s", f.Name, val.Type())
		}
	case "bytes":
		if _, ok := val.(starlark.Bytes); !ok {
			return fmt.Errorf("field %q expects bytes, got %s", f.Name, val.Type())
		}
	default:
		// Nested template.
		sv, ok := val.(*StructValue)
		if val == starlark.None {
			return nil
		}
		if !ok || sv.template.Name != base {
			return fmt.Errorf("field %q expects %s, got %s", f.Name, base, val.Type())
		}
	}
	return nil
}

// ── Bound method dispatch ────────────────────────────────────────────────

// boundMethod wraps a free-standing callable together with its
// receiver so `motor.start(x, y)` reaches `motor_start(motor, x, y)`.
type boundMethod struct {
	recv *StructValue
	name string
	fn   starlark.Callable
}

func (b *boundMethod) String() string {
	return fmt.Sprintf("<bound method %s.%s>", b.recv.template.Name, b.name)
}
func (b *boundMethod) Type() string                            { return "bound_method" }
func (b *boundMethod) Truth() starlark.Bool                    { return starlark.True }
func (b *boundMethod) Hash() (uint32, error)                   { return 0, fmt.Errorf("unhashable: bound_method") }
func (b *boundMethod) Freeze()                                 {}
func (b *boundMethod) Name() string                            { return b.name }
func (b *boundMethod) CallInternal(thread *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	combined := make(starlark.Tuple, 0, len(args)+1)
	combined = append(combined, b.recv)
	combined = append(combined, args...)
	return starlark.Call(thread, b.fn, combined, kwargs)
}

// Position is required by starlark.Callable but we have no source span.
func (b *boundMethod) Position() syntax.Position { return syntax.Position{} }

// ── Engine template + function lookup ────────────────────────────────────

// SetTemplates replaces the engine's template registry. Called by the
// PLC module when config changes. Safe to call concurrently.
func (e *Engine) SetTemplates(templates map[string]*itypes.PlcTemplate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.templates = make(map[string]*itypes.PlcTemplate, len(templates))
	for k, v := range templates {
		e.templates[k] = v
	}
}

func (e *Engine) getTemplate(name string) (*itypes.PlcTemplate, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.templates[name]
	return t, ok
}

// lookupFunction resolves a function reference to a callable by
// peeking at the named program's compiled globals.
func (e *Engine) lookupFunction(module, name string) (starlark.Callable, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	prog, ok := e.programs[module]
	if !ok {
		return nil, fmt.Errorf("program %q not compiled", module)
	}
	val, ok := prog.globals[name]
	if !ok {
		return nil, fmt.Errorf("program %q has no function %q", module, name)
	}
	callable, ok := val.(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("%s.%s is not callable", module, name)
	}
	return callable, nil
}

// RegisterTemplateMethods returns a StringDict binding each method of
// the named template as a free top-level function, so callers can
// write `start(motor)` alongside `motor.start()`. The free form
// takes the receiver as its first positional argument.
func (e *Engine) RegisterTemplateMethods(templateName string) (starlark.StringDict, error) {
	tmpl, ok := e.getTemplate(templateName)
	if !ok {
		return nil, fmt.Errorf("unknown template %q", templateName)
	}
	out := make(starlark.StringDict, len(tmpl.Methods))
	for _, m := range tmpl.Methods {
		fn, err := e.lookupFunction(m.Function.Module, m.Function.Name)
		if err != nil {
			return nil, fmt.Errorf("method %s.%s: %w", templateName, m.Name, err)
		}
		// The free form is just the underlying callable — calling
		// start(motor) routes to motor_start(motor) with no rewrap.
		out[m.Name] = fn
	}
	return out, nil
}
