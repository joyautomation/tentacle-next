//go:build plc || all

// Package plc contains the round-trip parser for Starlark ladder programs.
// It parses the Starlark AST to reconstruct a LadderProgram struct
// suitable for rendering in a graphical ladder diagram editor.
package plc

import (
	"fmt"
	"strconv"

	"go.starlark.net/syntax"
)

// LadderProgram represents a complete ladder logic program parsed from Starlark source.
type LadderProgram struct {
	Rungs []LadderRung `json:"rungs"`
}

// LadderRung represents a single rung in a ladder program.
type LadderRung struct {
	Comment    string          `json:"comment,omitempty"`
	Conditions []LadderElement `json:"conditions"`
	Outputs    []LadderElement `json:"outputs"`
}

// LadderElement represents a single ladder logic element.
type LadderElement struct {
	Type     string            `json:"type"`              // "NO", "NC", "OTE", "OTL", "OTU", "TON", "TOF", "CTU", "CTD", "RES", "branch", "series"
	Tag      string            `json:"tag,omitempty"`
	Preset   int               `json:"preset,omitempty"`  // for timers (ms) and counters
	Children [][]LadderElement `json:"children,omitempty"` // for branch: each child is a path; for series: single path
}

// isCondition returns true if the element type is a condition (contact or structural).
func (e *LadderElement) isCondition() bool {
	switch e.Type {
	case "NO", "NC", "branch", "series":
		return true
	default:
		return false
	}
}

// ParseLadder parses a Starlark ladder program source and returns the structured representation.
// The source must follow the strict ladder DSL conventions:
//   - A def main(): function
//   - Inside: only rung(...) calls
//   - Inside rung: only DSL calls (NO, NC, OTE, branch, series, etc.)
func ParseLadder(source string) (*LadderProgram, error) {
	f, err := syntax.Parse("program.star", source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	// Find the main function.
	var mainFn *syntax.DefStmt
	for _, stmt := range f.Stmts {
		if def, ok := stmt.(*syntax.DefStmt); ok {
			if def.Name.Name == "main" {
				mainFn = def
				break
			}
		}
	}
	if mainFn == nil {
		return nil, fmt.Errorf("parse: no main() function found")
	}

	program := &LadderProgram{}

	// Walk the body of main() looking for rung() calls.
	for _, stmt := range mainFn.Body {
		switch s := stmt.(type) {
		case *syntax.ExprStmt:
			call, ok := s.X.(*syntax.CallExpr)
			if !ok {
				continue
			}
			rung, err := parseRungCall(call, source)
			if err != nil {
				return nil, err
			}
			if rung != nil {
				program.Rungs = append(program.Rungs, *rung)
			}
		}
	}

	return program, nil
}

// parseRungCall parses a rung(...) call expression.
func parseRungCall(call *syntax.CallExpr, source string) (*LadderRung, error) {
	name := callName(call)
	if name != "rung" {
		return nil, nil // skip non-rung calls
	}

	rung := &LadderRung{}

	// Extract comment from preceding line if available.
	// (Comments are tracked separately in the syntax tree; we use position info.)

	for _, arg := range call.Args {
		elem, err := parseElement(arg)
		if err != nil {
			return nil, fmt.Errorf("rung: %w", err)
		}
		if elem.isCondition() {
			rung.Conditions = append(rung.Conditions, elem)
		} else {
			rung.Outputs = append(rung.Outputs, elem)
		}
	}

	return rung, nil
}

// parseElement parses a single DSL call expression into a LadderElement.
func parseElement(expr syntax.Expr) (LadderElement, error) {
	call, ok := expr.(*syntax.CallExpr)
	if !ok {
		return LadderElement{}, fmt.Errorf("expected call expression, got %T", expr)
	}

	name := callName(call)
	switch name {
	case "NO", "NC":
		tag, err := extractStringArg(call, 0)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		return LadderElement{Type: name, Tag: tag}, nil

	case "OTE", "OTL", "OTU":
		tag, err := extractStringArg(call, 0)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		return LadderElement{Type: name, Tag: tag}, nil

	case "TON", "TOF":
		tag, err := extractStringArg(call, 0)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		preset, err := extractIntArg(call, 1)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		return LadderElement{Type: name, Tag: tag, Preset: preset}, nil

	case "CTU", "CTD":
		tag, err := extractStringArg(call, 0)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		preset, err := extractIntArg(call, 1)
		if err != nil {
			return LadderElement{}, fmt.Errorf("%s: %w", name, err)
		}
		return LadderElement{Type: name, Tag: tag, Preset: preset}, nil

	case "RES":
		tag, err := extractStringArg(call, 0)
		if err != nil {
			return LadderElement{}, fmt.Errorf("RES: %w", err)
		}
		return LadderElement{Type: "RES", Tag: tag}, nil

	case "branch":
		elem := LadderElement{Type: "branch"}
		for _, arg := range call.Args {
			child, err := parseElement(arg)
			if err != nil {
				return LadderElement{}, fmt.Errorf("branch: %w", err)
			}
			// Each branch arg becomes a path. If it's a series, unwrap it.
			if child.Type == "series" && len(child.Children) == 1 {
				elem.Children = append(elem.Children, child.Children[0])
			} else {
				elem.Children = append(elem.Children, []LadderElement{child})
			}
		}
		return elem, nil

	case "series":
		elem := LadderElement{Type: "series"}
		var path []LadderElement
		for _, arg := range call.Args {
			child, err := parseElement(arg)
			if err != nil {
				return LadderElement{}, fmt.Errorf("series: %w", err)
			}
			path = append(path, child)
		}
		elem.Children = [][]LadderElement{path}
		return elem, nil

	default:
		return LadderElement{}, fmt.Errorf("unknown element: %s", name)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func callName(call *syntax.CallExpr) string {
	if ident, ok := call.Fn.(*syntax.Ident); ok {
		return ident.Name
	}
	return ""
}

func extractStringArg(call *syntax.CallExpr, idx int) (string, error) {
	if idx >= len(call.Args) {
		return "", fmt.Errorf("missing argument %d", idx)
	}
	lit, ok := call.Args[idx].(*syntax.Literal)
	if !ok || lit.Token != syntax.STRING {
		return "", fmt.Errorf("argument %d: expected string literal", idx)
	}
	// The literal value includes quotes; unquote it.
	s, err := strconv.Unquote(lit.Raw)
	if err != nil {
		return lit.Raw, nil
	}
	return s, nil
}

func extractIntArg(call *syntax.CallExpr, idx int) (int, error) {
	if idx >= len(call.Args) {
		return 0, fmt.Errorf("missing argument %d", idx)
	}
	lit, ok := call.Args[idx].(*syntax.Literal)
	if !ok || lit.Token != syntax.INT {
		return 0, fmt.Errorf("argument %d: expected integer literal", idx)
	}
	n, err := strconv.Atoi(lit.Raw)
	if err != nil {
		return 0, fmt.Errorf("argument %d: %w", idx, err)
	}
	return n, nil
}

// GenerateLadder produces canonical Starlark source from a LadderProgram.
// This is the inverse of ParseLadder and ensures round-trip fidelity.
func GenerateLadder(prog *LadderProgram) string {
	var buf []byte
	buf = append(buf, "def main():\n"...)

	for i, rung := range prog.Rungs {
		if rung.Comment != "" {
			buf = append(buf, "    # "...)
			buf = append(buf, rung.Comment...)
			buf = append(buf, '\n')
		}

		buf = append(buf, "    rung(\n"...)
		allElems := append(rung.Conditions, rung.Outputs...)
		for _, elem := range allElems {
			buf = appendElement(buf, elem, 2)
			buf = append(buf, ",\n"...)
		}
		buf = append(buf, "    )\n"...)

		// Blank line between rungs.
		if i < len(prog.Rungs)-1 {
			buf = append(buf, '\n')
		}
	}

	return string(buf)
}

func appendElement(buf []byte, elem LadderElement, indent int) []byte {
	prefix := ""
	for i := 0; i < indent; i++ {
		prefix += "    "
	}
	buf = append(buf, prefix...)

	switch elem.Type {
	case "NO", "NC", "OTE", "OTL", "OTU", "RES":
		buf = append(buf, elem.Type...)
		buf = append(buf, '(')
		buf = append(buf, strconv.Quote(elem.Tag)...)
		buf = append(buf, ')')

	case "TON", "TOF", "CTU", "CTD":
		buf = append(buf, elem.Type...)
		buf = append(buf, '(')
		buf = append(buf, strconv.Quote(elem.Tag)...)
		buf = append(buf, ", "...)
		buf = append(buf, strconv.Itoa(elem.Preset)...)
		buf = append(buf, ')')

	case "branch":
		buf = append(buf, "branch(\n"...)
		for _, path := range elem.Children {
			if len(path) == 1 {
				buf = appendElement(buf, path[0], indent+1)
			} else {
				buf = append(buf, prefix...)
				buf = append(buf, "    series(\n"...)
				for _, child := range path {
					buf = appendElement(buf, child, indent+2)
					buf = append(buf, ",\n"...)
				}
				buf = append(buf, prefix...)
				buf = append(buf, "    )"...)
			}
			buf = append(buf, ",\n"...)
		}
		buf = append(buf, prefix...)
		buf = append(buf, ')')

	case "series":
		buf = append(buf, "series(\n"...)
		if len(elem.Children) > 0 {
			for _, child := range elem.Children[0] {
				buf = appendElement(buf, child, indent+1)
				buf = append(buf, ",\n"...)
			}
		}
		buf = append(buf, prefix...)
		buf = append(buf, ')')
	}

	return buf
}
