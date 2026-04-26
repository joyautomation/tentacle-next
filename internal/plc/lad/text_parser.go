//go:build plc || all

package lad

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/st"
)

// ParseText decodes the text DSL form of a LAD diagram. It is the
// counterpart to Parse (JSON); the two forms are interchangeable on the
// AST: ParseText(Print(d)) is structurally equal to d.
func ParseText(source string) (*Diagram, error) {
	p := &textParser{src: source}
	if err := p.tokenize(); err != nil {
		return nil, err
	}
	return p.parseDiagram()
}

type tokKind int

const (
	tkEOF tokKind = iota
	tkIdent
	tkInt
	tkReal
	tkString
	tkTime
	tkAmp
	tkPipe
	tkLParen
	tkRParen
	tkComma
	tkColon
	tkAssign
	tkDot
	tkArrow
	tkAt
	tkComment
)

type token struct {
	kind tokKind
	text string
	line int
}

type textParser struct {
	src    string
	tokens []token
	pos    int
}

func (p *textParser) tokenize() error {
	src := p.src
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			j := i + 2
			for j < len(src) && src[j] != '\n' {
				j++
			}
			text := strings.TrimSpace(src[i+2 : j])
			p.tokens = append(p.tokens, token{kind: tkComment, text: text, line: line})
			i = j
		case c == '&':
			p.tokens = append(p.tokens, token{kind: tkAmp, text: "&", line: line})
			i++
		case c == '|':
			p.tokens = append(p.tokens, token{kind: tkPipe, text: "|", line: line})
			i++
		case c == '(':
			p.tokens = append(p.tokens, token{kind: tkLParen, text: "(", line: line})
			i++
		case c == ')':
			p.tokens = append(p.tokens, token{kind: tkRParen, text: ")", line: line})
			i++
		case c == ',':
			p.tokens = append(p.tokens, token{kind: tkComma, text: ",", line: line})
			i++
		case c == '.':
			p.tokens = append(p.tokens, token{kind: tkDot, text: ".", line: line})
			i++
		case c == '@':
			p.tokens = append(p.tokens, token{kind: tkAt, text: "@", line: line})
			i++
		case c == ':':
			if i+1 < len(src) && src[i+1] == '=' {
				p.tokens = append(p.tokens, token{kind: tkAssign, text: ":=", line: line})
				i += 2
			} else {
				p.tokens = append(p.tokens, token{kind: tkColon, text: ":", line: line})
				i++
			}
		case c == '-':
			if i+1 < len(src) && src[i+1] == '>' {
				p.tokens = append(p.tokens, token{kind: tkArrow, text: "->", line: line})
				i += 2
				continue
			}
			j := i + 1
			for j < len(src) && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			if j == i+1 {
				return fmt.Errorf("line %d: unexpected '-'", line)
			}
			text := src[i:j]
			if strings.Contains(text, ".") {
				p.tokens = append(p.tokens, token{kind: tkReal, text: text, line: line})
			} else {
				p.tokens = append(p.tokens, token{kind: tkInt, text: text, line: line})
			}
			i = j
		case c == '\'':
			j := i + 1
			var sb strings.Builder
			for j < len(src) && src[j] != '\'' {
				if src[j] == '\\' && j+1 < len(src) {
					sb.WriteByte(src[j+1])
					j += 2
					continue
				}
				if src[j] == '\n' {
					line++
				}
				sb.WriteByte(src[j])
				j++
			}
			if j >= len(src) {
				return fmt.Errorf("line %d: unterminated string", line)
			}
			p.tokens = append(p.tokens, token{kind: tkString, text: sb.String(), line: line})
			i = j + 1
		case c == 'T' && i+1 < len(src) && src[i+1] == '#':
			j := i + 2
			for j < len(src) && (isAlphanum(src[j]) || src[j] == '.' || src[j] == '_') {
				j++
			}
			p.tokens = append(p.tokens, token{kind: tkTime, text: src[i:j], line: line})
			i = j
		case isDigit(c):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.') {
				j++
			}
			text := src[i:j]
			if strings.Contains(text, ".") {
				p.tokens = append(p.tokens, token{kind: tkReal, text: text, line: line})
			} else {
				p.tokens = append(p.tokens, token{kind: tkInt, text: text, line: line})
			}
			i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			p.tokens = append(p.tokens, token{kind: tkIdent, text: src[i:j], line: line})
			i = j
		default:
			return fmt.Errorf("line %d: unexpected character %q", line, c)
		}
	}
	p.tokens = append(p.tokens, token{kind: tkEOF, line: line})
	return nil
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool      { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlphanum(c byte) bool   { return isAlpha(c) || isDigit(c) }
func isIdentStart(c byte) bool { return isAlpha(c) || c == '_' }
func isIdentPart(c byte) bool  { return isAlpha(c) || isDigit(c) || c == '_' }

func (p *textParser) peek() token { return p.tokens[p.pos] }

func (p *textParser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *textParser) expect(kind tokKind) (token, error) {
	t := p.peek()
	if t.kind != kind {
		return t, fmt.Errorf("line %d: expected %s, got %q", t.line, tokKindName(kind), t.text)
	}
	p.pos++
	return t, nil
}

func (p *textParser) expectIdent(name string) error {
	t := p.peek()
	if t.kind != tkIdent || t.text != name {
		return fmt.Errorf("line %d: expected %q, got %q", t.line, name, t.text)
	}
	p.pos++
	return nil
}

func (p *textParser) parseDiagram() (*Diagram, error) {
	for p.peek().kind == tkComment {
		p.advance()
	}
	if err := p.expectIdent("diagram"); err != nil {
		return nil, err
	}
	d := &Diagram{}
	if t := p.peek(); t.kind == tkIdent && !isKeyword(t.text) {
		d.Name = t.text
		p.advance()
	}

	var pendingComment string
	for {
		t := p.peek()
		switch {
		case t.kind == tkComment:
			if pendingComment != "" {
				pendingComment += "\n"
			}
			pendingComment += t.text
			p.advance()
		case t.kind == tkIdent && t.text == "var":
			v, err := p.parseVarDecl()
			if err != nil {
				return nil, err
			}
			d.Variables = append(d.Variables, v)
			pendingComment = ""
		case t.kind == tkIdent && t.text == "rung":
			r, err := p.parseRung()
			if err != nil {
				return nil, err
			}
			r.Comment = pendingComment
			pendingComment = ""
			d.Rungs = append(d.Rungs, r)
		case t.kind == tkIdent && t.text == "end":
			p.advance()
			if next := p.peek(); next.kind != tkEOF {
				return nil, fmt.Errorf("line %d: unexpected trailing %q", next.line, next.text)
			}
			return d, nil
		default:
			return nil, fmt.Errorf("line %d: unexpected token %q", t.line, t.text)
		}
	}
}

func (p *textParser) parseVarDecl() (VarDecl, error) {
	if err := p.expectIdent("var"); err != nil {
		return VarDecl{}, err
	}
	v := VarDecl{}
	if t := p.peek(); t.kind == tkIdent {
		switch t.text {
		case "global", "input", "output":
			v.Kind = t.text
			p.advance()
		}
	}
	nameTok, err := p.expect(tkIdent)
	if err != nil {
		return v, err
	}
	if isKeyword(nameTok.text) {
		return v, fmt.Errorf("line %d: variable name cannot be keyword %q", nameTok.line, nameTok.text)
	}
	v.Name = nameTok.text
	if _, err := p.expect(tkColon); err != nil {
		return v, err
	}
	typeTok, err := p.expect(tkIdent)
	if err != nil {
		return v, err
	}
	v.Type = typeTok.text

	if p.peek().kind == tkAssign {
		p.advance()
		init, err := p.parseInitText()
		if err != nil {
			return v, err
		}
		v.Init = init
	}
	if t := p.peek(); t.kind == tkIdent && t.text == "RETAIN" {
		v.Retain = true
		p.advance()
	}
	return v, nil
}

func (p *textParser) parseInitText() (string, error) {
	t := p.peek()
	switch t.kind {
	case tkInt, tkReal, tkTime:
		p.advance()
		return t.text, nil
	case tkString:
		p.advance()
		return "'" + strings.ReplaceAll(t.text, "'", `\'`) + "'", nil
	case tkIdent:
		if t.text == "TRUE" || t.text == "FALSE" {
			p.advance()
			return t.text, nil
		}
	}
	return "", fmt.Errorf("line %d: expected literal init value, got %q", t.line, t.text)
}

func (p *textParser) parseRung() (*Rung, error) {
	if err := p.expectIdent("rung"); err != nil {
		return nil, err
	}
	logic, err := p.parseOrExpr()
	if err != nil {
		return nil, err
	}
	r := &Rung{Logic: logic}
	for p.peek().kind == tkArrow {
		p.advance()
		out, err := p.parseOutput()
		if err != nil {
			return nil, err
		}
		r.Outputs = append(r.Outputs, out)
	}
	return r, nil
}

func (p *textParser) parseOrExpr() (Element, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkPipe {
		return left, nil
	}
	items := []Element{left}
	for p.peek().kind == tkPipe {
		p.advance()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	return &Parallel{Items: items}, nil
}

func (p *textParser) parseAndExpr() (Element, error) {
	left, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkAmp {
		return left, nil
	}
	items := []Element{left}
	for p.peek().kind == tkAmp {
		p.advance()
		right, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	return &Series{Items: items}, nil
}

func (p *textParser) parseAtom() (Element, error) {
	t := p.peek()
	if t.kind == tkLParen {
		p.advance()
		e, err := p.parseOrExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tkRParen); err != nil {
			return nil, err
		}
		return e, nil
	}
	if t.kind == tkIdent && (t.text == "NO" || t.text == "NC") {
		p.advance()
		if _, err := p.expect(tkLParen); err != nil {
			return nil, err
		}
		operand, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tkRParen); err != nil {
			return nil, err
		}
		return &Contact{Form: t.text, Operand: operand}, nil
	}
	return nil, fmt.Errorf("line %d: expected contact, got %q", t.line, t.text)
}

func (p *textParser) parseOperand() (string, error) {
	t, err := p.expect(tkIdent)
	if err != nil {
		return "", err
	}
	name := t.text
	for p.peek().kind == tkDot {
		p.advance()
		next, err := p.expect(tkIdent)
		if err != nil {
			return "", err
		}
		name += "." + next.text
	}
	return name, nil
}

func (p *textParser) parseOutput() (Output, error) {
	t := p.peek()
	if t.kind != tkIdent {
		return nil, fmt.Errorf("line %d: expected output, got %q", t.line, t.text)
	}
	if t.text == "OTE" || t.text == "OTL" || t.text == "OTU" {
		p.advance()
		if _, err := p.expect(tkLParen); err != nil {
			return nil, err
		}
		operand, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tkRParen); err != nil {
			return nil, err
		}
		return &Coil{Form: t.text, Operand: operand}, nil
	}
	if isKeyword(t.text) {
		return nil, fmt.Errorf("line %d: unexpected keyword %q in output position", t.line, t.text)
	}
	p.advance()
	fb := &FBCall{Instance: t.text}
	if p.peek().kind == tkAt {
		p.advance()
		piTok, err := p.expect(tkIdent)
		if err != nil {
			return nil, err
		}
		fb.PowerInput = piTok.text
	}
	if _, err := p.expect(tkLParen); err != nil {
		return nil, err
	}
	if p.peek().kind != tkRParen {
		for {
			nameTok, err := p.expect(tkIdent)
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkAssign); err != nil {
				return nil, err
			}
			ev, err := p.parseExprValue()
			if err != nil {
				return nil, err
			}
			if fb.Inputs == nil {
				fb.Inputs = make(map[string]Expr)
			}
			fb.Inputs[nameTok.text] = ev
			if p.peek().kind == tkComma {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(tkRParen); err != nil {
		return nil, err
	}
	return fb, nil
}

func (p *textParser) parseExprValue() (Expr, error) {
	t := p.peek()
	switch t.kind {
	case tkInt:
		p.advance()
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", t.line, err)
		}
		return &IntLit{V: v}, nil
	case tkReal:
		p.advance()
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", t.line, err)
		}
		return &RealLit{V: v}, nil
	case tkString:
		p.advance()
		return &StringLit{V: t.text}, nil
	case tkTime:
		p.advance()
		return &TimeLit{Raw: t.text, Ms: int64(st.ParseTimeMs(stripTimePrefix(t.text)))}, nil
	case tkIdent:
		if t.text == "TRUE" || t.text == "FALSE" {
			p.advance()
			return &BoolLit{V: t.text == "TRUE"}, nil
		}
		operand, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return &Ref{Name: operand}, nil
	}
	return nil, fmt.Errorf("line %d: expected expression, got %q", t.line, t.text)
}

// stripTimePrefix removes a leading "T#" (or lowercase "t#") so the raw
// literal can be fed to st.ParseTimeMs, which expects only the duration
// payload ("5s", "100ms", "1h30m"). The original literal is preserved on
// TimeLit.Raw for round-trip printing.
func stripTimePrefix(s string) string {
	if len(s) >= 2 && (s[0] == 'T' || s[0] == 't') && s[1] == '#' {
		return s[2:]
	}
	return s
}

func isKeyword(s string) bool {
	switch s {
	case "diagram", "end", "var", "rung", "global", "input", "output",
		"RETAIN", "TRUE", "FALSE", "NO", "NC", "OTE", "OTL", "OTU":
		return true
	}
	return false
}

func tokKindName(k tokKind) string {
	switch k {
	case tkIdent:
		return "identifier"
	case tkInt:
		return "integer"
	case tkReal:
		return "real"
	case tkString:
		return "string"
	case tkTime:
		return "time literal"
	case tkAmp:
		return "&"
	case tkPipe:
		return "|"
	case tkLParen:
		return "("
	case tkRParen:
		return ")"
	case tkComma:
		return ","
	case tkColon:
		return ":"
	case tkAssign:
		return ":="
	case tkDot:
		return "."
	case tkArrow:
		return "->"
	case tkAt:
		return "@"
	case tkComment:
		return "comment"
	case tkEOF:
		return "end of input"
	}
	return "?"
}
