//go:build (api || all) && (plc || all)

package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/st"
	"go.starlark.net/syntax"
)

type validateDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
}

type validateResponse struct {
	Diagnostics []validateDiagnostic `json:"diagnostics"`
}

var stLineRe = regexp.MustCompile(`^line (\d+):\s*(.*)$`)

func (m *Module) handleValidatePlcProgram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source   string `json:"source"`
		Language string `json:"language"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	resp := validateResponse{Diagnostics: []validateDiagnostic{}}
	switch strings.ToLower(body.Language) {
	case "st", "structured-text":
		resp.Diagnostics = validateST(body.Source)
	case "starlark", "python":
		resp.Diagnostics = validateStarlark(body.Source)
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported language: %q", body.Language))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func validateStarlark(source string) []validateDiagnostic {
	diags := []validateDiagnostic{}
	if source == "" {
		return diags
	}
	_, err := syntax.Parse("program.star", source, 0)
	if err == nil {
		return diags
	}
	// syntax.Error has Pos.Line / Pos.Col.
	var se syntax.Error
	if errors.As(err, &se) {
		diags = append(diags, validateDiagnostic{
			Severity: "error",
			Message:  se.Msg,
			Line:     int(se.Pos.Line),
			Col:      int(se.Pos.Col),
		})
		return diags
	}
	diags = append(diags, validateDiagnostic{
		Severity: "error",
		Message:  err.Error(),
		Line:     1,
		Col:      1,
	})
	return diags
}

func validateST(source string) []validateDiagnostic {
	diags := []validateDiagnostic{}
	if source == "" {
		return diags
	}
	_, err := st.Parse(source)
	if err == nil {
		return diags
	}
	msg := err.Error()
	line := 1
	if m := stLineRe.FindStringSubmatch(msg); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
			msg = m[2]
		}
	}
	diags = append(diags, validateDiagnostic{
		Severity: "error",
		Message:  msg,
		Line:     line,
		Col:      1,
	})
	return diags
}
