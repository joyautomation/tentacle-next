//go:build (api || all) && (plc || all)

package api

import (
	"fmt"
	"net/http"

	"github.com/joyautomation/tentacle/internal/plc"
	"github.com/joyautomation/tentacle/internal/plc/st"
)

// transpileVarResponse is a JSON-friendly representation of an st.VarDecl.
// The full AST contains an Expression interface that doesn't marshal cleanly,
// so the API exposes only the name and datatype.
type transpileVarResponse struct {
	Name     string `json:"name"`
	Datatype string `json:"datatype"`
}

type transpileResponse struct {
	Starlark string                 `json:"starlark"`
	Vars     []transpileVarResponse `json:"vars"`
}

func (m *Module) handleTranspilePlcProgram(w http.ResponseWriter, r *http.Request) {
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

	starlark, vars, err := st.Transpile(body.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("transpile: %v", err))
		return
	}

	resp := transpileResponse{
		Starlark: starlark,
		Vars:     make([]transpileVarResponse, 0, len(vars)),
	}
	for _, v := range vars {
		resp.Vars = append(resp.Vars, transpileVarResponse{Name: v.Name, Datatype: v.Datatype})
	}
	writeJSON(w, http.StatusOK, resp)
}

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
