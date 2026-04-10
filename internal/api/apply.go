//go:build api || all

package api

import (
	"io"
	"net/http"

	"github.com/joyautomation/tentacle/internal/manifest"
)

// handleApply parses and applies a multi-document YAML manifest.
// POST /api/v1/apply
func (m *Module) handleApply(w http.ResponseWriter, r *http.Request) {
	source := r.Header.Get("X-Config-Source")
	if source == "" {
		source = "api"
	}

	resources, err := manifest.Parse(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}

	result, err := manifest.Apply(m.bus, resources, source)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "apply error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleValidate parses and validates a manifest without applying.
// POST /api/v1/validate
func (m *Module) handleValidate(w http.ResponseWriter, r *http.Request) {
	resources, err := manifest.Parse(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}

	if err := manifest.Validate(resources); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"valid":  false,
			"errors": err.(*manifest.ValidationError).Errors,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"valid":     true,
		"resources": len(resources),
	})
}

// handleDiff compares a manifest against the current system state.
// POST /api/v1/diff
func (m *Module) handleDiff(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	resources, err := manifest.ParseBytes(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}

	result, err := manifest.Diff(m.bus, resources)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "diff error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
