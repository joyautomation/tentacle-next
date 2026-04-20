//go:build api || all

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// Templates are stored in BucketPlcTemplates keyed by template name.
// The {plcId} URL segment is preserved for URL symmetry but templates
// are effectively a global namespace (one per tentacle instance), same
// pattern used for plc programs.

var (
	templateNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	fieldNameRE    = regexp.MustCompile(`^[a-z_][a-zA-Z0-9_]*$`)
	primitiveTypes = map[string]bool{
		"bool":   true,
		"number": true,
		"string": true,
		"bytes":  true,
	}
)

// parseTypeRef splits a type reference like "Motor[]" or "Zone{}" into
// (baseType, collectionKind). collectionKind is "", "array", or "record".
func parseTypeRef(ref string) (base, collection string, ok bool) {
	if strings.HasSuffix(ref, "[]") {
		return strings.TrimSuffix(ref, "[]"), "array", true
	}
	if strings.HasSuffix(ref, "{}") {
		return strings.TrimSuffix(ref, "{}"), "record", true
	}
	return ref, "", true
}

// ─── Storage ───────────────────────────────────────────────────────────────

func (m *Module) getPlcTemplate(name string) (*itypes.PlcTemplate, error) {
	data, _, err := m.bus.KVGet(topics.BucketPlcTemplates, name)
	if err != nil {
		return nil, err
	}
	var tmpl itypes.PlcTemplate
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return nil, err
	}
	return &tmpl, nil
}

func (m *Module) listPlcTemplates() ([]itypes.PlcTemplate, error) {
	keys, err := m.bus.KVKeys(topics.BucketPlcTemplates)
	if err != nil {
		return nil, err
	}
	out := make([]itypes.PlcTemplate, 0, len(keys))
	for _, k := range keys {
		tmpl, err := m.getPlcTemplate(k)
		if err != nil {
			m.log.Warn("skipping plc template", "key", k, "err", err)
			continue
		}
		out = append(out, *tmpl)
	}
	return out, nil
}

func (m *Module) putPlcTemplate(tmpl *itypes.PlcTemplate) error {
	tmpl.UpdatedAt = time.Now().UnixMilli()
	if tmpl.UpdatedBy == "" {
		tmpl.UpdatedBy = "api"
	}
	data, err := json.Marshal(tmpl)
	if err != nil {
		return err
	}
	_, err = m.bus.KVPut(topics.BucketPlcTemplates, tmpl.Name, data)
	return err
}

func (m *Module) deletePlcTemplate(name string) error {
	return m.bus.KVDelete(topics.BucketPlcTemplates, name)
}

// ─── Validation ────────────────────────────────────────────────────────────

// validateTemplate checks the template against syntactic and referential
// rules. existingNames is the set of already-known template names (used
// for resolving nested type references); the template being validated
// is implicitly considered known so self-reference is allowed.
func validateTemplate(tmpl *itypes.PlcTemplate, existingNames map[string]bool) []ValidationIssue {
	var issues []ValidationIssue

	if tmpl.Name == "" {
		issues = append(issues, ValidationIssue{
			Path: "name", Code: "required",
			Message: "name is required",
		})
	} else if !templateNameRE.MatchString(tmpl.Name) {
		issues = append(issues, ValidationIssue{
			Path: "name", Code: "invalid_identifier",
			Message: fmt.Sprintf("name %q must match [A-Za-z_][A-Za-z0-9_]*", tmpl.Name),
		})
	}

	if len(tmpl.Fields) == 0 {
		issues = append(issues, ValidationIssue{
			Path: "fields", Code: "required",
			Message: "at least one field is required",
		})
	}

	knownTemplates := map[string]bool{tmpl.Name: true}
	for n := range existingNames {
		knownTemplates[n] = true
	}

	seenFields := map[string]bool{}
	for i, f := range tmpl.Fields {
		path := fmt.Sprintf("fields[%d]", i)

		if f.Name == "" {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "required",
				Message: "field name is required",
			})
		} else if !fieldNameRE.MatchString(f.Name) {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "invalid_identifier",
				Message: fmt.Sprintf("field name %q must match [a-z_][a-zA-Z0-9_]*", f.Name),
			})
		} else if seenFields[f.Name] {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "duplicate_field",
				Message: fmt.Sprintf("duplicate field name %q", f.Name),
			})
		} else {
			seenFields[f.Name] = true
		}

		if f.Type == "" {
			issues = append(issues, ValidationIssue{
				Path: path + ".type", Code: "required",
				Message: "field type is required",
			})
		} else {
			base, _, _ := parseTypeRef(f.Type)
			if !primitiveTypes[base] && !knownTemplates[base] {
				issues = append(issues, ValidationIssue{
					Path: path + ".type", Code: "unknown_type",
					Message: fmt.Sprintf("unknown type %q — not a primitive (bool, number, string, bytes) or known template", base),
				})
			}
		}
	}

	seenMethods := map[string]bool{}
	for i, meth := range tmpl.Methods {
		path := fmt.Sprintf("methods[%d]", i)
		if meth.Name == "" {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "required",
				Message: "method name is required",
			})
		} else if !fieldNameRE.MatchString(meth.Name) {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "invalid_identifier",
				Message: fmt.Sprintf("method name %q must match [a-z_][a-zA-Z0-9_]*", meth.Name),
			})
		} else if seenMethods[meth.Name] {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "duplicate_method",
				Message: fmt.Sprintf("duplicate method name %q", meth.Name),
			})
		} else if seenFields[meth.Name] {
			issues = append(issues, ValidationIssue{
				Path: path + ".name", Code: "name_collision",
				Message: fmt.Sprintf("method %q collides with a field of the same name", meth.Name),
			})
		} else {
			seenMethods[meth.Name] = true
		}

		if meth.Function.Name == "" {
			issues = append(issues, ValidationIssue{
				Path: path + ".function.name", Code: "required",
				Message: "function.name is required",
			})
		}
	}

	return issues
}

// ─── Handlers ──────────────────────────────────────────────────────────────

func (m *Module) handleListPlcTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := m.listPlcTemplates()
	if err != nil {
		// Empty bucket — return empty list rather than 500.
		writeJSON(w, http.StatusOK, []itypes.PlcTemplate{})
		return
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].Name < templates[j].Name })
	writeJSON(w, http.StatusOK, templates)
}

func (m *Module) handleGetPlcTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	tmpl, err := m.getPlcTemplate(name)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("template %q not found: %v", name, err))
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

func (m *Module) handlePutPlcTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var tmpl itypes.PlcTemplate
	if err := readJSON(r, &tmpl); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	tmpl.Name = name

	existingNames := map[string]bool{}
	if keys, err := m.bus.KVKeys(topics.BucketPlcTemplates); err == nil {
		for _, k := range keys {
			existingNames[k] = true
		}
	}

	if issues := validateTemplate(&tmpl, existingNames); len(issues) > 0 {
		writeValidationError(w, issues)
		return
	}

	if err := m.putPlcTemplate(&tmpl); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("put plc template: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, &tmpl)
}

func (m *Module) handleDeletePlcTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Refuse to delete templates that are still referenced by other
	// templates' fields — dangling references would break validation of
	// the referrers on their next write.
	if issues := findDanglingReferrers(m, name); len(issues) > 0 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":  "template_in_use",
			"issues": issues,
		})
		return
	}

	if err := m.deletePlcTemplate(name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("delete plc template: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// findDanglingReferrers returns an issue for every other template that
// references `name` as a field type.
func findDanglingReferrers(m *Module, name string) []ValidationIssue {
	all, err := m.listPlcTemplates()
	if err != nil {
		return nil
	}
	var issues []ValidationIssue
	for _, t := range all {
		if t.Name == name {
			continue
		}
		for i, f := range t.Fields {
			base, _, _ := parseTypeRef(f.Type)
			if base == name {
				issues = append(issues, ValidationIssue{
					Path:    fmt.Sprintf("%s.fields[%d].type", t.Name, i),
					Code:    "in_use",
					Message: fmt.Sprintf("template %q references %q via field %q", t.Name, name, f.Name),
				})
			}
		}
	}
	return issues
}
