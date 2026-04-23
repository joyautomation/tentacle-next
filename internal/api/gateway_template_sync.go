//go:build api || all

package api

import (
	"encoding/json"

	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// syncBrowseCacheUdtsToPlcTemplates turns UDTs discovered by a scanner
// browse into PlcTemplates, so Starlark programs can reference the
// device's structs by type name without the user authoring anything.
//
// Called from the browse-completion path right after the cache is
// persisted. Existing templates with the same name are left alone — the
// user's hand-authored types win over auto-import.
func (m *Module) syncBrowseCacheUdtsToPlcTemplates(cacheJSON []byte) {
	if len(cacheJSON) == 0 {
		return
	}

	var payload struct {
		Udts []struct {
			Name    string `json:"name"`
			Members []struct {
				Name     string `json:"name"`
				Datatype string `json:"datatype"`
				UdtType  string `json:"udtType"`
				IsArray  bool   `json:"isArray"`
			} `json:"members"`
		} `json:"udts"`
	}
	if err := json.Unmarshal(cacheJSON, &payload); err != nil {
		return
	}
	if len(payload.Udts) == 0 {
		return
	}

	existing := map[string]bool{}
	if keys, err := m.bus.KVKeys(topics.BucketPlcTemplates); err == nil {
		for _, k := range keys {
			existing[k] = true
		}
	}

	for _, u := range payload.Udts {
		if u.Name == "" || !templateNameRE.MatchString(u.Name) {
			continue
		}
		if existing[u.Name] {
			continue
		}
		fields := make([]itypes.PlcTemplateField, 0, len(u.Members))
		for _, mem := range u.Members {
			if mem.Name == "" || !fieldNameRE.MatchString(mem.Name) {
				continue
			}
			base := mem.UdtType
			if base == "" {
				base = mapScannerDatatype(mem.Datatype)
			}
			ftype := base
			if mem.IsArray {
				ftype = base + "[]"
			}
			fields = append(fields, itypes.PlcTemplateField{
				Name: mem.Name,
				Type: ftype,
			})
		}
		if len(fields) == 0 {
			continue
		}
		tmpl := &itypes.PlcTemplate{
			Name:      u.Name,
			Fields:    fields,
			UpdatedBy: "browse-sync",
		}
		if err := m.putPlcTemplate(tmpl); err != nil {
			m.log.Warn("auto-import plc template failed", "name", u.Name, "error", err)
			continue
		}
		existing[u.Name] = true
	}
}

// mapScannerDatatype maps a scanner's NATS datatype string to a PlcTemplate
// primitive. Falls back to "string" for unknowns so the template still
// validates — the user can refine the type later.
func mapScannerDatatype(dt string) string {
	switch dt {
	case "boolean", "bool":
		return "bool"
	case "number", "int", "integer", "float", "double", "real":
		return "number"
	case "string":
		return "string"
	case "bytes":
		return "bytes"
	default:
		return "string"
	}
}
