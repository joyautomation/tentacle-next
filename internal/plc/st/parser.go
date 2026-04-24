//go:build plc || all

package st

import (
	"fmt"
	"strings"
)

// Parser is a recursive descent parser for IEC 61131-3 Structured Text.
type Parser struct {
	tokens  []Token
	pos     int
	pending []VarDecl // carryover from multi-name var declarations ("a, b : INT;")
}

// Parse parses Structured Text source into a Program AST.
func Parse(source string) (*Program, error) {
	tokens := Lex(source)
	p := &Parser{tokens: tokens}
	return p.parseProgram()
}

func (p *Parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekAt(offset int) Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[idx]
}

func (p *Parser) advance() Token {
	tok := p.peek()
	p.pos++
	return tok
}

func (p *Parser) expect(tt TokenType) (Token, error) {
	tok := p.advance()
	if tok.Type != tt {
		return tok, fmt.Errorf("line %d: expected %d, got %q", tok.Line, tt, tok.Literal)
	}
	return tok, nil
}

func (p *Parser) match(tt TokenType) bool {
	if p.peek().Type == tt {
		p.advance()
		return true
	}
	return false
}

// ─── Top-level ──────────────────────────────────────────────────────────────

func (p *Parser) parseProgram() (*Program, error) {
	prog := &Program{}
	nameSet := false

	// Pre-program TYPE declarations are allowed.
	for {
		switch p.peek().Type {
		case TokenTypeKw:
			if err := p.parseTypeBlock(prog); err != nil {
				return nil, err
			}
			continue
		case TokenProgram:
			p.advance()
			nameTok := p.advance()
			prog.Name = nameTok.Literal
			nameSet = true
		}
		break
	}

	// Body: var blocks, type blocks (rarely but legal), and statements until
	// END_PROGRAM or EOF.
	terminator := TokenEOF
	if nameSet {
		terminator = TokenEndProgram
	}

	for p.peek().Type != terminator && p.peek().Type != TokenEOF {
		switch {
		case p.peek().Type == TokenTypeKw:
			if err := p.parseTypeBlock(prog); err != nil {
				return nil, err
			}
		case p.isVarBlockStart():
			vb, err := p.parseVarBlock()
			if err != nil {
				return nil, err
			}
			prog.VarBlocks = append(prog.VarBlocks, *vb)
		default:
			stmt, err := p.parseStatement()
			if err != nil {
				return nil, err
			}
			if stmt != nil {
				prog.Statements = append(prog.Statements, stmt)
			}
		}
	}
	if nameSet {
		p.match(TokenEndProgram)
	}
	if prog.Name == "" {
		prog.Name = "main"
	}
	return prog, nil
}

// parseTypeBlock parses TYPE Name : <typeExpr> ; [Name : <typeExpr> ;]* END_TYPE.
func (p *Parser) parseTypeBlock(prog *Program) error {
	p.advance() // TYPE
	for p.peek().Type != TokenEndType && p.peek().Type != TokenEOF {
		nameTok, err := p.expect(TokenIdent)
		if err != nil {
			return fmt.Errorf("type decl: %w", err)
		}
		if _, err := p.expect(TokenColon); err != nil {
			return fmt.Errorf("type decl: %w", err)
		}
		typ, err := p.parseTypeExpr()
		if err != nil {
			return fmt.Errorf("type decl %q: %w", nameTok.Literal, err)
		}
		p.match(TokenSemicolon)
		prog.TypeDecls = append(prog.TypeDecls, TypeDecl{Name: nameTok.Literal, Type: typ})
	}
	p.match(TokenEndType)
	p.match(TokenSemicolon)
	return nil
}

func (p *Parser) isVarBlockStart() bool {
	switch p.peek().Type {
	case TokenVar, TokenVarInput, TokenVarOutput, TokenVarInOut, TokenVarTemp, TokenVarGlobal, TokenVarExternal:
		return true
	}
	return false
}

func (p *Parser) parseVarBlock() (*VarBlock, error) {
	kindTok := p.advance()
	vb := &VarBlock{Kind: strings.ToUpper(kindTok.Literal)}

	// Optional modifiers: RETAIN / CONSTANT (may appear after the block kind).
	for {
		switch p.peek().Type {
		case TokenRetain:
			p.advance()
			vb.Retain = true
		case TokenConstant:
			p.advance()
			vb.Constant = true
		default:
			goto declarations
		}
	}
declarations:
	for p.peek().Type != TokenEndVar && p.peek().Type != TokenEOF {
		decl, err := p.parseVarDecl()
		if err != nil {
			return nil, err
		}
		vb.Variables = append(vb.Variables, *decl)
		for {
			more, ok := p.drainPending()
			if !ok {
				break
			}
			vb.Variables = append(vb.Variables, *more)
		}
	}
	p.match(TokenEndVar)
	p.match(TokenSemicolon)
	return vb, nil
}

// parseVarDecl parses `name [, name ...]: <typeExpr> [:= initial];`
// Comma-separated names share the same type/initializer (IEC allows `a, b : INT;`).
func (p *Parser) parseVarDecl() (*VarDecl, error) {
	names := []string{}
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return nil, fmt.Errorf("var decl: %w", err)
	}
	names = append(names, nameTok.Literal)
	for p.match(TokenComma) {
		extra, err := p.expect(TokenIdent)
		if err != nil {
			return nil, fmt.Errorf("var decl: %w", err)
		}
		names = append(names, extra.Literal)
	}
	if _, err := p.expect(TokenColon); err != nil {
		return nil, fmt.Errorf("var decl: %w", err)
	}

	typeExpr, err := p.parseTypeExpr()
	if err != nil {
		return nil, fmt.Errorf("var decl: %w", err)
	}

	var init Expression
	if p.peek().Type == TokenAssign {
		p.advance()
		init, err = p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("var decl initial: %w", err)
		}
	}
	p.match(TokenSemicolon)

	// Single-name is the overwhelmingly common case; keep the return shape
	// that callers already expect. Multi-name declarations synthesize extra
	// decls that the caller flattens on its own.
	if len(names) == 1 {
		return &VarDecl{Name: names[0], Datatype: typeExpr.String(), Type: typeExpr, Initial: init}, nil
	}
	// Stash extras back into the token stream is awkward; instead we emit
	// the first decl here and the parseVarBlock loop picks up the rest via
	// the leftover-name mechanism. Simpler: return a synthetic block-level
	// error if this path is hit — but IEC really does allow it, so we
	// flatten inline: all N decls end up in the surrounding block through
	// successive calls. Since our contract is one-decl-per-call, we push
	// the extra names onto a sibling mechanism: synthesize them as a
	// peek-ahead in the parser is overkill. Pragmatic path: only the
	// first name gets the initializer, subsequent names share the type;
	// we return the first and stash the rest onto p.pending for the next
	// parseVarDecl call.
	first := &VarDecl{Name: names[0], Datatype: typeExpr.String(), Type: typeExpr, Initial: init}
	for _, n := range names[1:] {
		p.pending = append(p.pending, VarDecl{Name: n, Datatype: typeExpr.String(), Type: typeExpr})
	}
	return first, nil
}

// pending holds additional VarDecls produced by multi-name declarations.
// parseVarBlock drains these before reading the next token.
// Declared as a field via a compile-time init below to avoid touching every
// existing Parser construction site.
func (p *Parser) drainPending() (*VarDecl, bool) {
	if len(p.pending) == 0 {
		return nil, false
	}
	decl := p.pending[0]
	p.pending = p.pending[1:]
	return &decl, true
}

// ─── Type expressions ───────────────────────────────────────────────────────

func (p *Parser) parseTypeExpr() (TypeExpr, error) {
	switch p.peek().Type {
	case TokenArray:
		return p.parseArrayType()
	case TokenStruct:
		return p.parseStructType()
	}
	// A scalar or named type: consume a single identifier-like token.
	tok := p.advance()
	name := strings.ToUpper(tok.Literal)
	if IsScalarTypeName(name) {
		return &ScalarType{Name: name}, nil
	}
	// Otherwise treat as a user-defined type (UDT or FB instance type).
	return &NamedType{Name: tok.Literal}, nil
}

func (p *Parser) parseArrayType() (TypeExpr, error) {
	p.advance() // ARRAY
	if _, err := p.expect(TokenLBracket); err != nil {
		return nil, fmt.Errorf("array type: %w", err)
	}
	var dims []ArrayDim
	for {
		lo, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("array dim lo: %w", err)
		}
		if _, err := p.expect(TokenDotDot); err != nil {
			return nil, fmt.Errorf("array dim: %w", err)
		}
		hi, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("array dim hi: %w", err)
		}
		dims = append(dims, ArrayDim{Lo: lo, Hi: hi})
		if !p.match(TokenComma) {
			break
		}
	}
	if _, err := p.expect(TokenRBracket); err != nil {
		return nil, fmt.Errorf("array type: %w", err)
	}
	if _, err := p.expect(TokenOf); err != nil {
		return nil, fmt.Errorf("array type: %w", err)
	}
	elem, err := p.parseTypeExpr()
	if err != nil {
		return nil, fmt.Errorf("array elem: %w", err)
	}
	return &ArrayType{Dims: dims, Elem: elem}, nil
}

func (p *Parser) parseStructType() (TypeExpr, error) {
	p.advance() // STRUCT
	st := &StructType{}
	for p.peek().Type != TokenEndStruct && p.peek().Type != TokenEOF {
		field, err := p.parseVarDecl()
		if err != nil {
			return nil, fmt.Errorf("struct field: %w", err)
		}
		st.Fields = append(st.Fields, *field)
		// Absorb any sibling names produced by a comma-declaration.
		for {
			more, ok := p.drainPending()
			if !ok {
				break
			}
			st.Fields = append(st.Fields, *more)
		}
	}
	p.match(TokenEndStruct)
	return st, nil
}

// ─── Statements ─────────────────────────────────────────────────────────────

func (p *Parser) parseStatement() (Statement, error) {
	switch p.peek().Type {
	case TokenIf:
		return p.parseIfStmt()
	case TokenFor:
		return p.parseForStmt()
	case TokenWhile:
		return p.parseWhileStmt()
	case TokenRepeat:
		return p.parseRepeatStmt()
	case TokenCase:
		return p.parseCaseStmt()
	case TokenReturn:
		p.advance()
		p.match(TokenSemicolon)
		return &ReturnStmt{}, nil
	case TokenExit:
		p.advance()
		p.match(TokenSemicolon)
		return &ExitStmt{}, nil
	case TokenContinue:
		p.advance()
		p.match(TokenSemicolon)
		return &ContinueStmt{}, nil
	case TokenSemicolon:
		p.advance()
		return nil, nil
	case TokenIdent:
		return p.parseAssignOrCall()
	default:
		p.advance()
		return nil, nil
	}
}

// parseAssignOrCall parses a statement that starts with an identifier:
// either an assignment (possibly to a complex lvalue with member/index
// access) or a call statement. The structured lvalue is preserved in
// AssignStmt.TargetExpr; the legacy dot-joined string is kept in Target
// for back-compat with the Starlark codegen.
func (p *Parser) parseAssignOrCall() (Statement, error) {
	lhs, err := p.parsePostfixChain()
	if err != nil {
		return nil, err
	}

	// Call statement: the postfix chain already consumed the call if it was one.
	if call, ok := lhs.(*CallExpr); ok {
		p.match(TokenSemicolon)
		return &CallStmt{Call: call}, nil
	}

	if p.peek().Type == TokenAssign {
		p.advance()
		val, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.match(TokenSemicolon)
		return &AssignStmt{
			Target:     flattenLValue(lhs),
			TargetExpr: lhs,
			Value:      val,
		}, nil
	}

	// Bare identifier on a line — legacy code paths produced an identity
	// assignment here. Preserve that behavior for backward compat.
	p.match(TokenSemicolon)
	return &AssignStmt{
		Target:     flattenLValue(lhs),
		TargetExpr: lhs,
		Value:      lhs,
	}, nil
}

// flattenLValue renders a structured lvalue as the dot-joined string form
// the old codegen understands. Index expressions render as "base[0]"-style
// placeholders; the Starlark codegen ignores those shapes.
func flattenLValue(e Expression) string {
	switch n := e.(type) {
	case *IdentExpr:
		return n.Name
	case *MemberExpr:
		return flattenLValue(n.Object) + "." + n.Member
	case *IndexExpr:
		return flattenLValue(n.Array) + "[]"
	}
	return ""
}

func (p *Parser) parseIfStmt() (Statement, error) {
	p.advance() // IF
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokenThen); err != nil {
		return nil, err
	}

	stmt := &IfStmt{Condition: cond}
	stmt.Then, err = p.parseStatementBlock(TokenElsif, TokenElse, TokenEndIf)
	if err != nil {
		return nil, err
	}

	for p.peek().Type == TokenElsif {
		p.advance()
		elsifCond, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.expect(TokenThen)
		body, err := p.parseStatementBlock(TokenElsif, TokenElse, TokenEndIf)
		if err != nil {
			return nil, err
		}
		stmt.ElsIfs = append(stmt.ElsIfs, ElsIfClause{Condition: elsifCond, Body: body})
	}

	if p.peek().Type == TokenElse {
		p.advance()
		stmt.Else, err = p.parseStatementBlock(TokenEndIf)
		if err != nil {
			return nil, err
		}
	}

	p.match(TokenEndIf)
	p.match(TokenSemicolon)
	return stmt, nil
}

func (p *Parser) parseForStmt() (Statement, error) {
	p.advance() // FOR
	varTok, err := p.expect(TokenIdent)
	if err != nil {
		return nil, err
	}
	p.expect(TokenAssign)
	start, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.expect(TokenTo)
	end, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	stmt := &ForStmt{Variable: varTok.Literal, Start: start, End: end}

	if p.peek().Type == TokenBy {
		p.advance()
		step, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Step = step
	}

	p.expect(TokenDo)
	stmt.Body, err = p.parseStatementBlock(TokenEndFor)
	if err != nil {
		return nil, err
	}
	p.match(TokenEndFor)
	p.match(TokenSemicolon)
	return stmt, nil
}

func (p *Parser) parseWhileStmt() (Statement, error) {
	p.advance() // WHILE
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.expect(TokenDo)
	body, err := p.parseStatementBlock(TokenEndWhile)
	if err != nil {
		return nil, err
	}
	p.match(TokenEndWhile)
	p.match(TokenSemicolon)
	return &WhileStmt{Condition: cond, Body: body}, nil
}

func (p *Parser) parseRepeatStmt() (Statement, error) {
	p.advance() // REPEAT
	body, err := p.parseStatementBlock(TokenUntil)
	if err != nil {
		return nil, err
	}
	p.expect(TokenUntil)
	cond, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.match(TokenEndRepeat)
	p.match(TokenSemicolon)
	return &RepeatStmt{Body: body, Condition: cond}, nil
}

func (p *Parser) parseCaseStmt() (Statement, error) {
	p.advance() // CASE
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.expect(TokenOf)

	stmt := &CaseStmt{Expression: expr}

	for p.peek().Type != TokenEndCase && p.peek().Type != TokenElse && p.peek().Type != TokenEOF {
		var values []Expression
		for {
			val, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			values = append(values, val)
			if !p.match(TokenComma) {
				break
			}
		}
		p.expect(TokenColon)
		body, err := p.parseCaseBody()
		if err != nil {
			return nil, err
		}
		stmt.Cases = append(stmt.Cases, CaseClause{Values: values, Body: body})

		if p.peek().Type == TokenEndCase || p.peek().Type == TokenElse || p.peek().Type == TokenEOF {
			break
		}
	}

	if p.peek().Type == TokenElse {
		p.advance()
		stmt.Else, err = p.parseStatementBlock(TokenEndCase)
		if err != nil {
			return nil, err
		}
	}

	p.match(TokenEndCase)
	p.match(TokenSemicolon)
	return stmt, nil
}

// parseCaseBody collects statements until it sees END_CASE, ELSE, EOF, or a
// token sequence that looks like a new case label (constant value[s] followed
// by `:`). Case labels aren't introduced by a keyword, so the body parse
// needs to peek ahead and stop before consuming them as statements.
func (p *Parser) parseCaseBody() ([]Statement, error) {
	var stmts []Statement
	for {
		tt := p.peek().Type
		if tt == TokenEOF || tt == TokenEndCase || tt == TokenElse {
			return stmts, nil
		}
		if p.looksLikeCaseLabel() {
			return stmts, nil
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
	}
}

// looksLikeCaseLabel returns true if the upcoming tokens form `<const>[, <const>]* :`.
// Case labels in ST are constant values, so an integer/typed literal or negative
// number followed by `,` or `:` unambiguously starts a new clause.
func (p *Parser) looksLikeCaseLabel() bool {
	i := 0
	// Optional leading '-' for negative numeric labels.
	if p.peekAt(i).Type == TokenMinus {
		i++
	}
	switch p.peekAt(i).Type {
	case TokenNumber, TokenBasedNumber, TokenTypedLiteral, TokenTrue, TokenFalse, TokenString, TokenTimeLiteral:
	default:
		return false
	}
	i++
	// Walk additional comma-separated values.
	for p.peekAt(i).Type == TokenComma {
		i++
		if p.peekAt(i).Type == TokenMinus {
			i++
		}
		switch p.peekAt(i).Type {
		case TokenNumber, TokenBasedNumber, TokenTypedLiteral, TokenTrue, TokenFalse, TokenString, TokenTimeLiteral:
		default:
			return false
		}
		i++
	}
	return p.peekAt(i).Type == TokenColon
}

func (p *Parser) parseStatementBlock(terminators ...TokenType) ([]Statement, error) {
	var stmts []Statement
	for {
		tt := p.peek().Type
		if tt == TokenEOF {
			break
		}
		for _, term := range terminators {
			if tt == term {
				return stmts, nil
			}
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
	}
	return stmts, nil
}

// ─── Expressions ────────────────────────────────────────────────────────────

func (p *Parser) parseExpression() (Expression, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (Expression, error) {
	left, err := p.parseXor()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenOr {
		p.advance()
		right, err := p.parseXor()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: "OR", Right: right}
	}
	return left, nil
}

func (p *Parser) parseXor() (Expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenXor {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: "XOR", Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expression, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenAnd {
		p.advance()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: "AND", Right: right}
	}
	return left, nil
}

func (p *Parser) parseComparison() (Expression, error) {
	left, err := p.parseAddition()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().Type {
		case TokenEqual:
			op = "="
		case TokenNotEqual:
			op = "<>"
		case TokenLess:
			op = "<"
		case TokenLessEq:
			op = "<="
		case TokenGreater:
			op = ">"
		case TokenGreaterEq:
			op = ">="
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseAddition()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
}

func (p *Parser) parseAddition() (Expression, error) {
	left, err := p.parseMultiplication()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenPlus || p.peek().Type == TokenMinus {
		op := p.advance().Literal
		right, err := p.parseMultiplication()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMultiplication() (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenStar || p.peek().Type == TokenSlash || p.peek().Type == TokenMod {
		tok := p.advance()
		op := tok.Literal
		if tok.Type == TokenMod {
			op = "MOD"
		}
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expression, error) {
	if p.peek().Type == TokenNot {
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", Operand: operand}, nil
	}
	if p.peek().Type == TokenMinus {
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", Operand: operand}, nil
	}
	return p.parsePrimary()
}

// parsePrimary handles atoms and starts a postfix chain (member / index / call).
func (p *Parser) parsePrimary() (Expression, error) {
	tok := p.peek()
	switch tok.Type {
	case TokenNumber:
		p.advance()
		return &NumberLit{Value: tok.Literal, Base: 10}, nil
	case TokenBasedNumber:
		p.advance()
		base, digits := splitBasedLiteral(tok.Literal)
		return &NumberLit{Value: digits, Base: base}, nil
	case TokenString:
		p.advance()
		return &StringLit{Value: tok.Literal}, nil
	case TokenTrue:
		p.advance()
		return &BoolLit{Value: true}, nil
	case TokenFalse:
		p.advance()
		return &BoolLit{Value: false}, nil
	case TokenTimeLiteral:
		p.advance()
		return &TimeLit{Raw: tok.Literal}, nil
	case TokenTypedLiteral:
		p.advance()
		return buildTypedLit(tok.Literal), nil
	case TokenLParen:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.expect(TokenRParen)
		return p.continuePostfix(expr)
	case TokenIdent:
		return p.parsePostfixChain()
	default:
		return nil, fmt.Errorf("line %d: unexpected token %q", tok.Line, tok.Literal)
	}
}

// parsePostfixChain parses `ident (. member | [index] | (args))*`.
// The head is always a bare identifier; postfix chains are left-associative.
func (p *Parser) parsePostfixChain() (Expression, error) {
	head, err := p.expect(TokenIdent)
	if err != nil {
		return nil, err
	}
	var expr Expression = &IdentExpr{Name: head.Literal}
	return p.continuePostfix(expr)
}

// continuePostfix extends expr with any member / index / call suffixes.
func (p *Parser) continuePostfix(expr Expression) (Expression, error) {
	for {
		switch p.peek().Type {
		case TokenDot:
			p.advance()
			memberTok, err := p.expect(TokenIdent)
			if err != nil {
				return nil, err
			}
			expr = &MemberExpr{Object: expr, Member: memberTok.Literal}
		case TokenLBracket:
			p.advance()
			var indices []Expression
			for {
				idx, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				indices = append(indices, idx)
				if !p.match(TokenComma) {
					break
				}
			}
			if _, err := p.expect(TokenRBracket); err != nil {
				return nil, err
			}
			expr = &IndexExpr{Array: expr, Indices: indices}
		case TokenLParen:
			// Call-like suffix is only valid when the head is a name path
			// (ident/member). We still allow it for any expression and let
			// the lowering pass reject invalid callees.
			name := flattenCalleeName(expr)
			call, err := p.parseCallArgs(name)
			if err != nil {
				return nil, err
			}
			expr = call
		default:
			return expr, nil
		}
	}
}

// flattenCalleeName renders a call target back into a dot-joined string
// so that existing codegen (which uses CallExpr.Name string) keeps working.
func flattenCalleeName(e Expression) string {
	switch n := e.(type) {
	case *IdentExpr:
		return n.Name
	case *MemberExpr:
		return flattenCalleeName(n.Object) + "." + n.Member
	}
	return ""
}

// parseCallArgs reads a call's argument list. Handles both positional and
// IEC-style named (`NAME := value`) arguments; they may not mix in the same
// call. Named args are detected by peeking two tokens ahead for `:=`.
func (p *Parser) parseCallArgs(name string) (*CallExpr, error) {
	p.expect(TokenLParen)
	call := &CallExpr{Name: name}
	for p.peek().Type != TokenRParen && p.peek().Type != TokenEOF {
		// Named arg: IDENT := expr
		if p.peek().Type == TokenIdent && p.peekAt(1).Type == TokenAssign {
			argName := p.advance().Literal
			p.advance() // :=
			val, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			call.NamedArgs = append(call.NamedArgs, NamedArg{Name: argName, Value: val})
		} else {
			arg, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			call.Args = append(call.Args, arg)
		}
		if !p.match(TokenComma) {
			break
		}
	}
	p.expect(TokenRParen)
	return call, nil
}

// splitBasedLiteral splits a "16#FF" literal into (16, "FF").
// Malformed input yields (10, raw).
func splitBasedLiteral(lit string) (int, string) {
	idx := strings.Index(lit, "#")
	if idx <= 0 || idx == len(lit)-1 {
		return 10, lit
	}
	base := 10
	switch lit[:idx] {
	case "2":
		base = 2
	case "8":
		base = 8
	case "10":
		base = 10
	case "16":
		base = 16
	}
	return base, lit[idx+1:]
}

// buildTypedLit converts a captured typed literal like "INT#42" or "BOOL#TRUE"
// into an Expression. The inner payload is re-parsed as the narrowest literal
// that fits (number / bool / string / time).
func buildTypedLit(raw string) Expression {
	idx := strings.Index(raw, "#")
	if idx <= 0 || idx == len(raw)-1 {
		return &NumberLit{Value: raw, Base: 10}
	}
	typeName := raw[:idx]
	payload := raw[idx+1:]

	// Nested based-number inside a typed prefix: INT#16#FF.
	if inner := strings.Index(payload, "#"); inner > 0 {
		base, digits := splitBasedLiteral(payload)
		return &TypedLit{TypeName: typeName, Inner: &NumberLit{Value: digits, Base: base}}
	}

	upperPayload := strings.ToUpper(payload)
	switch upperPayload {
	case "TRUE":
		return &TypedLit{TypeName: typeName, Inner: &BoolLit{Value: true}}
	case "FALSE":
		return &TypedLit{TypeName: typeName, Inner: &BoolLit{Value: false}}
	}

	// STRING#'hello' → strip the quotes and return a StringLit.
	if (typeName == "STRING" || typeName == "WSTRING") && len(payload) >= 2 && payload[0] == '\'' && payload[len(payload)-1] == '\'' {
		return &TypedLit{TypeName: typeName, Inner: &StringLit{Value: payload[1 : len(payload)-1]}}
	}

	// TIME#5s / LTIME#1h30m → TimeLit.
	if typeName == "TIME" || typeName == "LTIME" {
		return &TypedLit{TypeName: typeName, Inner: &TimeLit{Raw: payload}}
	}

	// Default: numeric literal. Detect real by presence of '.' or exponent.
	if strings.ContainsAny(payload, ".eE") {
		return &TypedLit{TypeName: typeName, Inner: &NumberLit{Value: payload, Base: 10}}
	}
	return &TypedLit{TypeName: typeName, Inner: &NumberLit{Value: payload, Base: 10}}
}
