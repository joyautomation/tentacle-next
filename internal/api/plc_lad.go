//go:build (api || all) && (plc || all)

package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/lad"
)

// handleParseLadRung accepts a single rung's text DSL (e.g. "rung NO(a) ->
// OTE(b)"), parses it via the same parser used for full diagrams, and
// returns the rung in the canonical JSON wire form the editor consumes.
//
// Source must be a single rung (or rung+comment lines). Multi-rung input
// is rejected so the editor's swap-in stays unambiguous.
func (m *Module) handleParseLadRung(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid body: %v", err))
		return
	}

	src := strings.TrimSpace(body.Source)
	if src == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"diagnostics": []validateDiagnostic{{
				Severity: "error",
				Message:  "rung is empty",
				Line:     1,
				Col:      1,
			}},
		})
		return
	}

	wrapped := "diagram _tmp\n" + src + "\nend\n"
	d, err := lad.ParseText(wrapped)
	if err != nil {
		// Errors emitted from the parser carry "line N: ..." prefixes; we
		// shift the line number back down by 1 so it lines up with the
		// rung-only text the editor showed the user.
		line, msg := splitLadError(err.Error())
		if line > 1 {
			line--
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"diagnostics": []validateDiagnostic{{
				Severity: "error",
				Message:  msg,
				Line:     line,
				Col:      1,
			}},
		})
		return
	}
	if len(d.Rungs) != 1 {
		writeJSON(w, http.StatusOK, map[string]any{
			"diagnostics": []validateDiagnostic{{
				Severity: "error",
				Message:  fmt.Sprintf("expected exactly one rung, got %d", len(d.Rungs)),
				Line:     1,
				Col:      1,
			}},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rung":        rungToWire(d.Rungs[0]),
		"diagnostics": []validateDiagnostic{},
	})
}

func splitLadError(msg string) (int, string) {
	if m := stLineRe.FindStringSubmatch(msg); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, m[2]
		}
	}
	return 1, msg
}

// rungToWire converts a parsed Rung to the discriminated-union JSON shape
// the TS editor consumes. The Go AST uses interfaces without an explicit
// "kind" tag; the wire form uses a `kind` discriminator field on each
// node, mirroring web/src/lib/components/ladder/types.ts.
func rungToWire(r *lad.Rung) map[string]any {
	out := map[string]any{
		"logic": elementToWire(r.Logic),
	}
	if r.Comment != "" {
		out["comment"] = r.Comment
	}
	if len(r.Outputs) > 0 {
		outs := make([]map[string]any, 0, len(r.Outputs))
		for _, o := range r.Outputs {
			outs = append(outs, outputToWire(o))
		}
		out["outputs"] = outs
	}
	return out
}

func elementToWire(e lad.Element) map[string]any {
	switch x := e.(type) {
	case *lad.Contact:
		return map[string]any{"kind": "contact", "form": x.Form, "operand": x.Operand}
	case *lad.Series:
		items := make([]map[string]any, 0, len(x.Items))
		for _, it := range x.Items {
			items = append(items, elementToWire(it))
		}
		return map[string]any{"kind": "series", "items": items}
	case *lad.Parallel:
		items := make([]map[string]any, 0, len(x.Items))
		for _, it := range x.Items {
			items = append(items, elementToWire(it))
		}
		return map[string]any{"kind": "parallel", "items": items}
	}
	return map[string]any{}
}

func outputToWire(o lad.Output) map[string]any {
	switch x := o.(type) {
	case *lad.Coil:
		return map[string]any{"kind": "coil", "form": x.Form, "operand": x.Operand}
	case *lad.FBCall:
		out := map[string]any{"kind": "fb", "instance": x.Instance}
		if x.PowerInput != "" {
			out["powerInput"] = x.PowerInput
		}
		if len(x.Inputs) > 0 {
			inputs := map[string]any{}
			for k, v := range x.Inputs {
				inputs[k] = exprToWire(v)
			}
			out["inputs"] = inputs
		}
		return out
	}
	return map[string]any{}
}

func exprToWire(e lad.Expr) map[string]any {
	switch x := e.(type) {
	case *lad.Ref:
		return map[string]any{"kind": "ref", "name": x.Name}
	case *lad.IntLit:
		return map[string]any{"kind": "int", "value": x.V}
	case *lad.RealLit:
		return map[string]any{"kind": "real", "value": x.V}
	case *lad.BoolLit:
		return map[string]any{"kind": "bool", "value": x.V}
	case *lad.TimeLit:
		out := map[string]any{"kind": "time", "ms": x.Ms}
		if x.Raw != "" {
			out["raw"] = x.Raw
		}
		return out
	case *lad.StringLit:
		return map[string]any{"kind": "string", "value": x.V}
	}
	return map[string]any{}
}
