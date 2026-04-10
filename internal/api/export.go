//go:build api || all

package api

import (
	"net/http"
	"strings"

	"github.com/joyautomation/tentacle/internal/manifest"
)

// handleExport returns all system configuration as multi-document YAML.
// GET /api/v1/export
func (m *Module) handleExport(w http.ResponseWriter, r *http.Request) {
	opts := manifest.ExportOptions{}

	// Optional ?kind=Gateway,Service filter.
	if kinds := r.URL.Query().Get("kind"); kinds != "" {
		for _, k := range strings.Split(kinds, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				opts.Kinds = append(opts.Kinds, k)
			}
		}
	}

	resources, err := manifest.Export(m.bus, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}

	yamlBytes, err := manifest.Serialize(resources)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "serialize failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=tentacle-export.yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(yamlBytes)
}
