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
// Templates are namespaced by device — a UDT named "UDT_PIT" browsed
// on device "rtu60" becomes the PlcTemplate "rtu60.UDT_PIT". This
// prevents collisions when two devices define a same-named struct, and
// groups imports in the UI under the owning device. Nested UDT refs
// inside a member's Type are rewritten to the same device's namespace.
//
// Called from the browse-completion path right after the cache is
// persisted. Existing templates with the same qualified name are left
// alone — the user's hand-authored types win over auto-import.
func (m *Module) syncBrowseCacheUdtsToPlcTemplates(cacheJSON []byte) {
	if len(cacheJSON) == 0 {
		return
	}

	var payload struct {
		DeviceID string `json:"deviceId"`
		Udts     []struct {
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
	if len(payload.Udts) == 0 || payload.DeviceID == "" {
		return
	}

	// Device IDs can contain chars that aren't valid in template names
	// (templateNameRE allows [A-Za-z_][A-Za-z0-9_]*). If this device ID
	// can't be used as a namespace prefix, skip import rather than
	// producing an invalid template.
	if !templateNameRE.MatchString(payload.DeviceID) {
		m.log.Warn("auto-import plc template skipped: device id not a valid namespace", "deviceId", payload.DeviceID)
		return
	}

	existing := map[string]bool{}
	if keys, err := m.bus.KVKeys(topics.BucketPlcTemplates); err == nil {
		for _, k := range keys {
			existing[k] = true
		}
	}

	qualify := func(bare string) string {
		return payload.DeviceID + "." + bare
	}

	// Clean up bare-named auto-imports left over from before we started
	// namespacing — any template with UpdatedBy="browse-sync" and a name
	// matching a UDT we're about to import belongs under the new scheme
	// and should be removed to avoid a duplicate flat entry in the UI.
	for _, u := range payload.Udts {
		if u.Name == "" || !existing[u.Name] {
			continue
		}
		if tmpl, err := m.getPlcTemplate(u.Name); err == nil && tmpl.UpdatedBy == "browse-sync" {
			_ = m.deletePlcTemplate(u.Name)
			delete(existing, u.Name)
		}
	}

	for _, u := range payload.Udts {
		if u.Name == "" || !templateNameRE.MatchString(u.Name) {
			continue
		}
		qualified := qualify(u.Name)
		if existing[qualified] {
			continue
		}
		fields := make([]itypes.PlcTemplateField, 0, len(u.Members))
		for _, mem := range u.Members {
			if mem.Name == "" || !fieldNameRE.MatchString(mem.Name) {
				continue
			}
			base := mem.UdtType
			if base != "" {
				// Nested UDTs come from the same device, so qualify them
				// with the same namespace so the reference resolves.
				base = qualify(base)
			} else {
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
			Name:      qualified,
			Fields:    fields,
			UpdatedBy: "browse-sync",
		}
		if err := m.putPlcTemplate(tmpl); err != nil {
			m.log.Warn("auto-import plc template failed", "name", qualified, "error", err)
			continue
		}
		existing[qualified] = true
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
