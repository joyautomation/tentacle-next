//go:build (api || all) && !plc && !all

package api

import "net/http"

func (m *Module) handleTranspilePlcProgram(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "ST transpiler is not available in this build (rebuild with -tags plc)")
}

func (m *Module) handleParseLadder(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "Ladder parser is not available in this build (rebuild with -tags plc)")
}

func (m *Module) handleGenerateLadder(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "Ladder generator is not available in this build (rebuild with -tags plc)")
}
