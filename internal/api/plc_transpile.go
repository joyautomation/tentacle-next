//go:build (api || all) && (plc || all)

package api

import (
	"fmt"
	"net/http"

	"github.com/joyautomation/tentacle/internal/plc"
)

func (m *Module) handleParseLadder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}
	if body.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}

	prog, err := plc.ParseLadder(body.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, prog)
}

func (m *Module) handleGenerateLadder(w http.ResponseWriter, r *http.Request) {
	var prog plc.LadderProgram
	if err := readJSON(r, &prog); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	source := plc.GenerateLadder(&prog)
	writeJSON(w, http.StatusOK, map[string]string{"source": source})
}
