//go:build api || all

package api

import (
	"testing"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

func TestValidateTemplate(t *testing.T) {
	cases := []struct {
		name       string
		tmpl       itypes.PlcTemplate
		existing   map[string]bool
		wantCodes  []string
	}{
		{
			name: "valid minimal",
			tmpl: itypes.PlcTemplate{
				Name:   "Motor",
				Fields: []itypes.PlcTemplateField{{Name: "running", Type: "bool"}},
			},
		},
		{
			name: "valid nested + collections + methods",
			tmpl: itypes.PlcTemplate{
				Name: "Motor",
				Fields: []itypes.PlcTemplateField{
					{Name: "running", Type: "bool", Default: false},
					{Name: "speed", Type: "number", Default: 0, Unit: "rpm"},
					{Name: "fault", Type: "Fault"},
					{Name: "zones", Type: "Zone{}"},
					{Name: "history", Type: "number[]"},
				},
				Methods: []itypes.PlcTemplateMethod{
					{Name: "start", Function: itypes.PlcFunctionRef{Module: "plc", Name: "motor_start"}},
				},
			},
			existing: map[string]bool{"Fault": true, "Zone": true},
		},
		{
			name: "self-reference allowed",
			tmpl: itypes.PlcTemplate{
				Name: "Node",
				Fields: []itypes.PlcTemplateField{
					{Name: "value", Type: "number"},
					{Name: "next", Type: "Node"},
				},
			},
		},
		{
			name: "missing name",
			tmpl: itypes.PlcTemplate{
				Fields: []itypes.PlcTemplateField{{Name: "x", Type: "bool"}},
			},
			wantCodes: []string{"required"},
		},
		{
			name: "bad name",
			tmpl: itypes.PlcTemplate{
				Name:   "1Motor",
				Fields: []itypes.PlcTemplateField{{Name: "x", Type: "bool"}},
			},
			wantCodes: []string{"invalid_identifier"},
		},
		{
			name: "no fields",
			tmpl: itypes.PlcTemplate{
				Name: "Empty",
			},
			wantCodes: []string{"required"},
		},
		{
			name: "duplicate field",
			tmpl: itypes.PlcTemplate{
				Name: "Dup",
				Fields: []itypes.PlcTemplateField{
					{Name: "x", Type: "bool"},
					{Name: "x", Type: "number"},
				},
			},
			wantCodes: []string{"duplicate_field"},
		},
		{
			name: "unknown type",
			tmpl: itypes.PlcTemplate{
				Name: "Motor",
				Fields: []itypes.PlcTemplateField{
					{Name: "fault", Type: "Fault"},
				},
			},
			wantCodes: []string{"unknown_type"},
		},
		{
			name: "method collides with field",
			tmpl: itypes.PlcTemplate{
				Name: "Motor",
				Fields: []itypes.PlcTemplateField{
					{Name: "start", Type: "bool"},
				},
				Methods: []itypes.PlcTemplateMethod{
					{Name: "start", Function: itypes.PlcFunctionRef{Module: "plc", Name: "motor_start"}},
				},
			},
			wantCodes: []string{"name_collision"},
		},
		{
			name: "method missing function name",
			tmpl: itypes.PlcTemplate{
				Name:   "Motor",
				Fields: []itypes.PlcTemplateField{{Name: "running", Type: "bool"}},
				Methods: []itypes.PlcTemplateMethod{
					{Name: "start", Function: itypes.PlcFunctionRef{Module: "plc"}},
				},
			},
			wantCodes: []string{"required"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := validateTemplate(&c.tmpl, c.existing)
			if len(c.wantCodes) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no issues, got: %+v", got)
				}
				return
			}
			for _, want := range c.wantCodes {
				found := false
				for _, issue := range got {
					if issue.Code == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected issue with code %q, got: %+v", want, got)
				}
			}
		})
	}
}

func TestParseTypeRef(t *testing.T) {
	cases := []struct {
		in              string
		wantBase        string
		wantCollection  string
	}{
		{"bool", "bool", ""},
		{"number[]", "number", "array"},
		{"Motor[]", "Motor", "array"},
		{"Zone{}", "Zone", "record"},
		{"Motor", "Motor", ""},
	}
	for _, c := range cases {
		base, coll, ok := parseTypeRef(c.in)
		if !ok || base != c.wantBase || coll != c.wantCollection {
			t.Errorf("parseTypeRef(%q) = (%q, %q, %v); want (%q, %q, true)",
				c.in, base, coll, ok, c.wantBase, c.wantCollection)
		}
	}
}
