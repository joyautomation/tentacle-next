//go:build plc || all

package plc

import (
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/starlark"
)

// tagGroup is what read_tag returns when the path names a template
// instance (i.e. has children). It's a lightweight, read-only record
// that supports both dot-access (tod.SECOND) and bracket-access
// (tod["SECOND"]) so it feels natural whether the user thinks in
// dict-of-fields or struct-of-fields terms.
//
// Nested paths like "MOTOR1.STATE.RUNNING" become nested tagGroups, so
// `tod.STATE.RUNNING` works the same as `tod["STATE"]["RUNNING"]`.
type tagGroup struct {
	name   string // basePath, used in String()/repr
	fields map[string]starlark.Value
}

// newTagGroup builds a tagGroup from a flat Go map of children keyed
// by the portion of the path *after* basePath. Dotted keys become
// nested tagGroups.
func newTagGroup(name string, children map[string]interface{}) *tagGroup {
	// Bucket children by top-level segment. Flat keys go straight in;
	// dotted keys (nested struct) get collected and recursed.
	flat := make(map[string]interface{})
	nested := make(map[string]map[string]interface{})
	for k, v := range children {
		if i := strings.IndexByte(k, '.'); i >= 0 {
			head, tail := k[:i], k[i+1:]
			if nested[head] == nil {
				nested[head] = make(map[string]interface{})
			}
			nested[head][tail] = v
			continue
		}
		flat[k] = v
	}
	out := &tagGroup{name: name, fields: make(map[string]starlark.Value, len(flat)+len(nested))}
	for k, v := range flat {
		out.fields[k] = goToStarlark(v)
	}
	for k, sub := range nested {
		out.fields[k] = newTagGroup(name+"."+k, sub)
	}
	return out
}

// ── starlark.Value ────────────────────────────────────────────────────

func (g *tagGroup) String() string {
	keys := make([]string, 0, len(g.fields))
	for k := range g.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, g.fields[k].String()))
	}
	return fmt.Sprintf("tag_group(%s)", strings.Join(parts, ", "))
}

func (g *tagGroup) Type() string             { return "tag_group" }
func (g *tagGroup) Truth() starlark.Bool     { return starlark.Bool(len(g.fields) > 0) }
func (g *tagGroup) Hash() (uint32, error)    { return 0, fmt.Errorf("unhashable: tag_group") }
func (g *tagGroup) Freeze()                  {}

// ── HasAttrs (dot-access) ─────────────────────────────────────────────

func (g *tagGroup) Attr(name string) (starlark.Value, error) {
	if v, ok := g.fields[name]; ok {
		return v, nil
	}
	return nil, nil
}

func (g *tagGroup) AttrNames() []string {
	out := make([]string, 0, len(g.fields))
	for k := range g.fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── Mapping / Indexable (bracket-access) ──────────────────────────────

func (g *tagGroup) Get(k starlark.Value) (starlark.Value, bool, error) {
	s, ok := k.(starlark.String)
	if !ok {
		return nil, false, fmt.Errorf("tag_group keys must be strings, got %s", k.Type())
	}
	v, ok := g.fields[string(s)]
	return v, ok, nil
}
