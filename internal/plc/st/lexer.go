//go:build plc || all

package st

import (
	"strings"
	"unicode"
)

// Lexer tokenizes IEC 61131-3 Structured Text source.
type Lexer struct {
	input  string
	pos    int
	line   int
	col    int
	tokens []Token
}

// Lex tokenizes the entire input and returns a token slice.
func Lex(input string) []Token {
	l := &Lexer{input: input, line: 1, col: 1}
	l.lex()
	return l.tokens
}

func (l *Lexer) lex() {
	for l.pos < len(l.input) {
		l.skipWhitespaceAndComments()
		if l.pos >= len(l.input) {
			break
		}
		ch := l.input[l.pos]

		switch {
		case ch == ':' && l.peek(1) == '=':
			l.emit(TokenAssign, ":=", 2)
		case ch == '<' && l.peek(1) == '>':
			l.emit(TokenNotEqual, "<>", 2)
		case ch == '<' && l.peek(1) == '=':
			l.emit(TokenLessEq, "<=", 2)
		case ch == '>' && l.peek(1) == '=':
			l.emit(TokenGreaterEq, ">=", 2)
		case ch == '<':
			l.emit(TokenLess, "<", 1)
		case ch == '>':
			l.emit(TokenGreater, ">", 1)
		case ch == '=':
			l.emit(TokenEqual, "=", 1)
		case ch == '+':
			l.emit(TokenPlus, "+", 1)
		case ch == '-':
			l.emit(TokenMinus, "-", 1)
		case ch == '*':
			l.emit(TokenStar, "*", 1)
		case ch == '/':
			l.emit(TokenSlash, "/", 1)
		case ch == '(':
			l.emit(TokenLParen, "(", 1)
		case ch == ')':
			l.emit(TokenRParen, ")", 1)
		case ch == ';':
			l.emit(TokenSemicolon, ";", 1)
		case ch == ':':
			l.emit(TokenColon, ":", 1)
		case ch == ',':
			l.emit(TokenComma, ",", 1)
		case ch == '.':
			l.emit(TokenDot, ".", 1)
		case ch == '#':
			l.emit(TokenHash, "#", 1)
		case ch == '\'':
			l.lexString()
		case isDigit(ch):
			l.lexNumber()
		case isIdentStart(ch):
			l.lexIdentOrKeyword()
		default:
			// Skip unknown characters.
			l.advance(1)
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Col: l.col})
}

func (l *Lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.advance(1)
		} else if ch == '\n' {
			l.pos++
			l.line++
			l.col = 1
		} else if ch == '(' && l.peek(1) == '*' {
			// Block comment (* ... *)
			l.advance(2)
			for l.pos < len(l.input)-1 {
				if l.input[l.pos] == '*' && l.input[l.pos+1] == ')' {
					l.advance(2)
					break
				}
				if l.input[l.pos] == '\n' {
					l.line++
					l.col = 1
					l.pos++
				} else {
					l.advance(1)
				}
			}
		} else if ch == '/' && l.peek(1) == '/' {
			// Line comment // ...
			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.advance(1)
			}
		} else {
			break
		}
	}
}

func (l *Lexer) lexString() {
	l.advance(1) // skip opening '
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '\'' {
		l.advance(1)
	}
	lit := l.input[start:l.pos]
	if l.pos < len(l.input) {
		l.advance(1) // skip closing '
	}
	l.tokens = append(l.tokens, Token{Type: TokenString, Literal: lit, Line: l.line, Col: l.col})
}

func (l *Lexer) lexNumber() {
	start := l.pos
	for l.pos < len(l.input) && (isDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		l.advance(1)
	}
	// Handle exponent
	if l.pos < len(l.input) && (l.input[l.pos] == 'e' || l.input[l.pos] == 'E') {
		l.advance(1)
		if l.pos < len(l.input) && (l.input[l.pos] == '+' || l.input[l.pos] == '-') {
			l.advance(1)
		}
		for l.pos < len(l.input) && isDigit(l.input[l.pos]) {
			l.advance(1)
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenNumber, Literal: l.input[start:l.pos], Line: l.line, Col: l.col})
}

func (l *Lexer) lexIdentOrKeyword() {
	start := l.pos
	for l.pos < len(l.input) && isIdentPart(l.input[l.pos]) {
		l.advance(1)
	}
	lit := l.input[start:l.pos]
	upper := strings.ToUpper(lit)

	// Check for time literal: T#...
	if upper == "T" && l.pos < len(l.input) && l.input[l.pos] == '#' {
		l.advance(1) // skip #
		tstart := l.pos
		for l.pos < len(l.input) && (isDigit(l.input[l.pos]) || isIdentPart(l.input[l.pos])) {
			l.advance(1)
		}
		l.tokens = append(l.tokens, Token{Type: TokenTimeLiteral, Literal: l.input[tstart:l.pos], Line: l.line, Col: l.col})
		return
	}

	if tt, ok := keywords[upper]; ok {
		l.tokens = append(l.tokens, Token{Type: tt, Literal: lit, Line: l.line, Col: l.col})
	} else {
		l.tokens = append(l.tokens, Token{Type: TokenIdent, Literal: lit, Line: l.line, Col: l.col})
	}
}

func (l *Lexer) emit(tt TokenType, lit string, width int) {
	l.tokens = append(l.tokens, Token{Type: tt, Literal: lit, Line: l.line, Col: l.col})
	l.advance(width)
}

func (l *Lexer) advance(n int) {
	l.pos += n
	l.col += n
}

func (l *Lexer) peek(offset int) byte {
	idx := l.pos + offset
	if idx >= len(l.input) {
		return 0
	}
	return l.input[idx]
}

func isDigit(ch byte) bool    { return ch >= '0' && ch <= '9' }
func isIdentStart(ch byte) bool { return unicode.IsLetter(rune(ch)) || ch == '_' }
func isIdentPart(ch byte) bool  { return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_' }
