//go:build plc || all

package plc

import (
	"strings"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// DefSignature is a signature extracted from a Python-style annotated def
// header in a Starlark source. It's the bridge between the surface syntax
// (which Starlark's grammar rejects) and our internal PlcFunctionSig.
type DefSignature struct {
	Name       string
	Params     []DefParam
	ReturnType string
	HasReturn  bool
}

// DefParam captures a single annotated parameter. HasType/HasDefault
// distinguish "unspecified" from "empty string" so a downstream consumer
// can decide whether to include the param's type in completion output.
type DefParam struct {
	Name       string
	Type       string
	HasType    bool
	Default    string
	HasDefault bool
	Variadic   bool // *args
	Keyword    bool // **kwargs
}

// StripAnnotations rewrites a Starlark source so Python-style type
// annotations on def headers disappear, making the source parse under
// starlark-go. Stripped regions are replaced with ASCII spaces so byte
// offsets are preserved end-to-end — LSP diagnostics on the stripped
// source map back to correct columns in the user's original source.
//
// Returns the stripped source and the signatures extracted from every def
// header that parsed cleanly. Def headers with malformed annotations are
// left untouched; the subsequent starlark parse will surface the error to
// the user.
func StripAnnotations(source string) (string, []DefSignature) {
	s := []byte(source)
	out := make([]byte, len(s))
	copy(out, s)
	var sigs []DefSignature

	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '#':
			// Line comment. Skip to newline.
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '"' || c == '\'':
			i = skipStarlarkString(s, i)
		case c == 'd' && isDefStart(s, i):
			if end, sig, ok := rewriteDefHeader(s, out, i); ok {
				if sig.Name != "" {
					sigs = append(sigs, sig)
				}
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(out), sigs
}

// isDefStart reports whether s[i:] begins with the keyword `def` followed
// by a name-start character, at a word boundary.
func isDefStart(s []byte, i int) bool {
	if i+4 > len(s) {
		return false
	}
	if s[i] != 'd' || s[i+1] != 'e' || s[i+2] != 'f' {
		return false
	}
	if i > 0 && isIdentCharByte(s[i-1]) {
		return false
	}
	// Must be followed by whitespace then an identifier char.
	j := i + 3
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	return j < len(s) && isIdentStartByte(s[j])
}

// rewriteDefHeader parses the def header starting at s[start] (the 'd' of
// 'def'). On success, it clears annotations in `out` and returns the byte
// index of the character immediately after the header's closing ':',
// plus the extracted signature. On parse failure the function makes no
// changes and returns ok=false.
//
// The parser handles:
//   - simple and positional-only params
//   - `param: type`
//   - `param: type = default` and `param = default`
//   - `*args`, `**kwargs` (optionally annotated)
//   - `-> returnType` before the trailing `:`
//   - header continuation across lines inside the parameter list
func rewriteDefHeader(s, out []byte, start int) (int, DefSignature, bool) {
	// Skip `def`.
	i := start + 3
	i = skipSpacesTabs(s, i)

	// Function name.
	nameStart := i
	for i < len(s) && isIdentCharByte(s[i]) {
		i++
	}
	if i == nameStart {
		return 0, DefSignature{}, false
	}
	name := string(s[nameStart:i])

	i = skipSpacesTabs(s, i)
	if i >= len(s) || s[i] != '(' {
		return 0, DefSignature{}, false
	}
	i++ // consume '('

	sig := DefSignature{Name: name}

	// Parse params until we hit the matching ')'.
	for {
		i = skipHeaderWhitespace(s, i)
		if i >= len(s) {
			return 0, DefSignature{}, false
		}
		if s[i] == ')' {
			i++
			break
		}
		// Leading '*' or '**' for variadic / keyword capture.
		var variadic, keyword bool
		if s[i] == '*' {
			if i+1 < len(s) && s[i+1] == '*' {
				keyword = true
				i += 2
			} else {
				variadic = true
				i++
			}
			i = skipSpacesTabs(s, i)
			// Bare `*` (positional-only barrier) without a name — Starlark
			// doesn't support it, but don't choke: treat as no-param.
			if i < len(s) && (s[i] == ',' || s[i] == ')') {
				if s[i] == ',' {
					i++
					continue
				}
				i++ // ')'
				break
			}
		}

		pNameStart := i
		for i < len(s) && isIdentCharByte(s[i]) {
			i++
		}
		if i == pNameStart {
			return 0, DefSignature{}, false
		}
		pName := string(s[pNameStart:i])
		param := DefParam{Name: pName, Variadic: variadic, Keyword: keyword}

		i = skipHeaderWhitespace(s, i)

		// Type annotation?
		if i < len(s) && s[i] == ':' {
			annStart := i // points at ':'
			i++           // past ':'
			// The annotation expression runs until we hit a top-level ',' ')'
			// or '=' at our current paren depth.
			typeExprStart := skipSpacesTabs(s, i)
			typeExprEnd, end := scanAnnotationExpr(s, typeExprStart)
			if !end {
				return 0, DefSignature{}, false
			}
			param.Type = strings.TrimSpace(string(s[typeExprStart:typeExprEnd]))
			param.HasType = true
			// Blank out the annotation region (':' through the last char of
			// the expression) so Starlark sees whitespace in its place.
			blank(out, annStart, typeExprEnd)
			i = typeExprEnd
			i = skipHeaderWhitespace(s, i)
		}

		// Default value?
		if i < len(s) && s[i] == '=' {
			i++ // past '='
			defStart := skipSpacesTabs(s, i)
			defEnd, end := scanAnnotationExpr(s, defStart)
			if !end {
				return 0, DefSignature{}, false
			}
			param.Default = strings.TrimSpace(string(s[defStart:defEnd]))
			param.HasDefault = true
			i = defEnd
			i = skipHeaderWhitespace(s, i)
		}

		sig.Params = append(sig.Params, param)

		if i >= len(s) {
			return 0, DefSignature{}, false
		}
		if s[i] == ',' {
			i++
			continue
		}
		if s[i] == ')' {
			i++
			break
		}
		return 0, DefSignature{}, false
	}

	// Optional return annotation.
	i = skipSpacesTabs(s, i)
	if i+1 < len(s) && s[i] == '-' && s[i+1] == '>' {
		arrowStart := i
		i += 2
		retStart := skipSpacesTabs(s, i)
		retEnd, end := scanReturnAnnotation(s, retStart)
		if !end {
			return 0, DefSignature{}, false
		}
		sig.ReturnType = strings.TrimSpace(string(s[retStart:retEnd]))
		sig.HasReturn = true
		blank(out, arrowStart, retEnd)
		i = retEnd
		i = skipSpacesTabs(s, i)
	}

	// Trailing ':'.
	if i >= len(s) || s[i] != ':' {
		return 0, DefSignature{}, false
	}
	i++
	return i, sig, true
}

// scanAnnotationExpr advances past an annotation or default-value
// expression starting at i, stopping at a top-level ',', ')', or '='. It
// tracks paren/bracket/brace depth and string literals so nested commas
// or parens don't confuse it. Returns the end offset (exclusive) and true
// on success, or (0, false) if the expression is malformed.
func scanAnnotationExpr(s []byte, i int) (int, bool) {
	depth := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			i = skipStarlarkString(s, i)
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				if c == ')' {
					return i, true
				}
				return 0, false
			}
			depth--
		case depth == 0 && (c == ',' || c == '='):
			return i, true
		case c == '\n':
			// Line continuations inside brackets are fine; at depth 0 we
			// treat a newline as the end of the expression so partial
			// source (still being typed) doesn't swallow the whole file.
			if depth == 0 {
				return i, true
			}
		}
		i++
	}
	return 0, false
}

// scanReturnAnnotation is like scanAnnotationExpr but stops only at a
// top-level ':' — return annotations can't legally contain a bare ','.
func scanReturnAnnotation(s []byte, i int) (int, bool) {
	depth := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			i = skipStarlarkString(s, i)
			continue
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return 0, false
			}
			depth--
		case depth == 0 && c == ':':
			return i, true
		case c == '\n' && depth == 0:
			return i, true
		}
		i++
	}
	return 0, false
}

// skipStarlarkString advances past a string literal starting at s[i]. It
// handles triple-quoted strings and backslash escapes. Returns the index
// immediately after the closing quote, or len(s) if the literal is
// unterminated (callers fall back to linear advance).
func skipStarlarkString(s []byte, i int) int {
	quote := s[i]
	// Triple-quoted?
	if i+2 < len(s) && s[i+1] == quote && s[i+2] == quote {
		i += 3
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if i+2 < len(s) && s[i] == quote && s[i+1] == quote && s[i+2] == quote {
				return i + 3
			}
			i++
		}
		return len(s)
	}
	i++ // past opening quote
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if c == quote {
			return i + 1
		}
		if c == '\n' {
			return i // unterminated — let caller resume
		}
		i++
	}
	return len(s)
}

// skipSpacesTabs advances past any run of spaces or tabs.
func skipSpacesTabs(s []byte, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// skipHeaderWhitespace advances past spaces, tabs, and newlines — used
// between tokens inside a def's parameter list, where continuation across
// lines is implicit due to the open paren.
func skipHeaderWhitespace(s []byte, i int) int {
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		break
	}
	return i
}

// blank overwrites out[from:to] with ASCII spaces, preserving newlines so
// line numbers in the stripped source still match the original.
func blank(out []byte, from, to int) {
	for i := from; i < to; i++ {
		if out[i] == '\n' || out[i] == '\r' {
			continue
		}
		out[i] = ' '
	}
}

func isIdentStartByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentCharByte(c byte) bool {
	return isIdentStartByte(c) || (c >= '0' && c <= '9')
}

// DeriveProgramSignature returns the PlcFunctionSig for the def in `source`
// whose name matches `programName`. Returns nil when the source has no
// matching def or only unannotated params and no return type (i.e. nothing
// worth surfacing to the IDE).
func DeriveProgramSignature(source, programName string) *itypes.PlcFunctionSig {
	if programName == "" {
		return nil
	}
	_, sigs := StripAnnotations(source)
	for _, s := range sigs {
		if s.Name != programName {
			continue
		}
		return defSignatureToPlcSig(s)
	}
	return nil
}

// defSignatureToPlcSig projects a parsed def header into the stored
// signature form. Params without any annotation or default are still
// surfaced (so the IDE knows the entry function takes N args) — callers
// that only want "rich" signatures can check HasSignature upstream.
func defSignatureToPlcSig(s DefSignature) *itypes.PlcFunctionSig {
	if len(s.Params) == 0 && !s.HasReturn {
		// Nothing to expose beyond the name itself.
		return nil
	}
	sig := &itypes.PlcFunctionSig{}
	sig.Params = make([]itypes.PlcFunctionParam, 0, len(s.Params))
	for _, p := range s.Params {
		sig.Params = append(sig.Params, itypes.PlcFunctionParam{
			Name:     p.Name,
			Type:     canonicalizeType(p.Type),
			Required: !p.HasDefault && !p.Variadic && !p.Keyword,
			Default:  plcDefaultValue(p),
		})
	}
	if s.HasReturn {
		sig.Returns = &itypes.PlcFunctionReturn{Type: canonicalizeType(s.ReturnType)}
	}
	return sig
}

// canonicalizeType maps Python-style short type names to the vocabulary
// used elsewhere in the PLC config (number/boolean/string). Unknown
// names pass through unchanged so template types (`Motor`, `Sensor`) and
// unrecognised annotations still work — completion happily surfaces
// whatever the user wrote.
func canonicalizeType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "float", "number", "num":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "str", "string":
		return "string"
	case "":
		return ""
	}
	return t
}

// plcDefaultValue converts the raw default-expression text we captured
// into the `interface{}` shape the KV expects. Booleans, numbers, and
// quoted strings are unwrapped; everything else is stored as raw text so
// nothing is lost.
func plcDefaultValue(p DefParam) interface{} {
	if !p.HasDefault {
		return nil
	}
	v := strings.TrimSpace(p.Default)
	switch v {
	case "True":
		return true
	case "False":
		return false
	case "None":
		return nil
	}
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
