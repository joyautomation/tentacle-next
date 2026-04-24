//go:build ethernetip || all

package ethernetip

import (
	"testing"
)

func newTestScanner() *Scanner {
	return &Scanner{
		udts:       make(map[string]map[string]UdtExport),
		structTags: make(map[string]map[string]string),
	}
}

func TestExpandStructTagFlat(t *testing.T) {
	s := newTestScanner()
	s.storeSchema("rtu60", &BrowseResult{
		Udts: map[string]UdtExport{
			"Get_TOD": {
				Name: "Get_TOD",
				Members: []UdtMemberExport{
					{Name: "SECOND", CipType: "DINT"},
					{Name: "DAY", CipType: "DINT"},
				},
			},
		},
		StructTags: map[string]string{
			"RTU60_13XFR9_PLC_TOD": "Get_TOD",
		},
	})

	members, ok := s.expandStructTag("rtu60", "RTU60_13XFR9_PLC_TOD")
	if !ok {
		t.Fatalf("expected expansion")
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d: %+v", len(members), members)
	}
	if members["RTU60_13XFR9_PLC_TOD.SECOND"] != "DINT" {
		t.Errorf("SECOND: got %q", members["RTU60_13XFR9_PLC_TOD.SECOND"])
	}
	if members["RTU60_13XFR9_PLC_TOD.DAY"] != "DINT" {
		t.Errorf("DAY: got %q", members["RTU60_13XFR9_PLC_TOD.DAY"])
	}
}

// A struct member that is itself a struct should recurse into its
// template so nested UDTs get fully flattened to dotted leaf paths.
func TestExpandStructTagNested(t *testing.T) {
	s := newTestScanner()
	s.storeSchema("dev", &BrowseResult{
		Udts: map[string]UdtExport{
			"Motor": {
				Name: "Motor",
				Members: []UdtMemberExport{
					{Name: "Speed", CipType: "REAL"},
					{Name: "State", CipType: "STRUCT", UdtType: "MotorState"},
				},
			},
			"MotorState": {
				Name: "MotorState",
				Members: []UdtMemberExport{
					{Name: "Running", CipType: "BOOL"},
					{Name: "Faulted", CipType: "BOOL"},
				},
			},
		},
		StructTags: map[string]string{"M1": "Motor"},
	})

	members, ok := s.expandStructTag("dev", "M1")
	if !ok {
		t.Fatalf("expected expansion")
	}
	want := map[string]string{
		"M1.Speed":         "REAL",
		"M1.State.Running": "BOOL",
		"M1.State.Faulted": "BOOL",
	}
	for k, v := range want {
		if members[k] != v {
			t.Errorf("%s: got %q, want %q", k, members[k], v)
		}
	}
	if len(members) != len(want) {
		t.Errorf("got %d members, want %d: %+v", len(members), len(want), members)
	}
}

func TestExpandStructTagUnknown(t *testing.T) {
	s := newTestScanner()
	if _, ok := s.expandStructTag("dev", "Missing"); ok {
		t.Errorf("unknown tag should not expand")
	}
	if _, ok := s.expandStructTag("other-dev", "anything"); ok {
		t.Errorf("unknown device should not expand")
	}
}
