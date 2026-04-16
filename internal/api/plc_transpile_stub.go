//go:build (api || all) && !plc && !all

package api

import "net/http"

func (m *Module) handleTranspilePlcProgram(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "ST transpiler is not available in this build (rebuild with -tags plc)")
}
