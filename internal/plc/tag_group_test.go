//go:build plc || all

package plc

import (
	"testing"

	"go.starlark.net/starlark"
)

// A tag_group must support both dot-access (Python-object style) and
// bracket-access (dict style), and nested paths must become nested
// tag_groups so both idioms compose.
func TestTagGroupAccess(t *testing.T) {
	g := newTagGroup("RTU60_13XFR9_PLC_TOD", map[string]interface{}{
		"SECOND":       42.0,
		"DAY":          12.0,
		"STATE.ACTIVE": true,
		"STATE.FAULT":  false,
	})

	// Dot access on a flat field.
	v, err := g.Attr("SECOND")
	if err != nil {
		t.Fatalf("SECOND: %v", err)
	}
	if f, ok := v.(starlark.Float); !ok || float64(f) != 42.0 {
		t.Errorf("SECOND: got %v", v)
	}

	// Bracket access on a flat field.
	v, found, err := g.Get(starlark.String("DAY"))
	if err != nil || !found {
		t.Fatalf("DAY lookup: err=%v found=%v", err, found)
	}
	if f, ok := v.(starlark.Float); !ok || float64(f) != 12.0 {
		t.Errorf("DAY: got %v", v)
	}

	// Nested: STATE is itself a tag_group.
	state, err := g.Attr("STATE")
	if err != nil {
		t.Fatalf("STATE: %v", err)
	}
	sub, ok := state.(*tagGroup)
	if !ok {
		t.Fatalf("STATE is %T, want *tagGroup", state)
	}
	active, err := sub.Attr("ACTIVE")
	if err != nil || active != starlark.True {
		t.Errorf("STATE.ACTIVE: got %v err=%v", active, err)
	}

	// Missing field returns (nil, nil) per Starlark HasAttrs contract.
	if v, err := g.Attr("NOPE"); v != nil || err != nil {
		t.Errorf("missing attr: got %v, err=%v", v, err)
	}

	// AttrNames includes both flat and nested fields.
	names := g.AttrNames()
	hasSecond, hasState := false, false
	for _, n := range names {
		if n == "SECOND" {
			hasSecond = true
		}
		if n == "STATE" {
			hasState = true
		}
	}
	if !hasSecond || !hasState {
		t.Errorf("AttrNames missing entries: %v", names)
	}
}

// read_tag should return a tag_group (not a dict) for template-instance
// reads, so user code can write tod.SECOND or tod["SECOND"].
func TestReadTagReturnsTagGroup(t *testing.T) {
	e := &Engine{deviceTags: newTestCache()}
	feedSubject(e.deviceTags,
		"ethernetip.data.rtu60.A", "rtu60", "RTU60_13XFR9_PLC_TOD.SECOND", 42.0)
	feedSubject(e.deviceTags,
		"ethernetip.data.rtu60.B", "rtu60", "RTU60_13XFR9_PLC_TOD.DAY", 12.0)

	args := starlark.Tuple{starlark.String("rtu60"), starlark.String("RTU60_13XFR9_PLC_TOD")}
	v, err := e.builtinReadTag(nil, starlark.NewBuiltin("read_tag", e.builtinReadTag), args, nil)
	if err != nil {
		t.Fatalf("read_tag: %v", err)
	}
	g, ok := v.(*tagGroup)
	if !ok {
		t.Fatalf("read_tag returned %T, want *tagGroup", v)
	}
	second, _ := g.Attr("SECOND")
	if f, ok := second.(starlark.Float); !ok || float64(f) != 42.0 {
		t.Errorf("tod.SECOND = %v, want 42.0", second)
	}
}
