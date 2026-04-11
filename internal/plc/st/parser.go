//go:build plc || all

package st

import (
	"fmt"
	"strings"
)

// Parser is a recursive descent parser for IEC 61131-3 Structured Text.
type Parser struct {
	tokens []Token
	pos    int
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

	// Optional PROGRAM header.
	if p.peek().Type == TokenProgram {
		p.advance() // PROGRAM
		nameTok := p.advance()
		prog.Name = nameTok.Literal

		// Parse VAR blocks and statements until END_PROGRAM.
		for p.peek().Type != TokenEndProgram && p.peek().Type != TokenEOF {
			if p.isVarBlockStart() {
				vb, err := p.parseVarBlock()
				if err != nil {
					return nil, err
				}
				prog.VarBlocks = append(prog.VarBlocks, *vb)
			} else {
				stmt, err := p.parseStatement()
				if err != nil {
					return nil, err
				}
				if stmt != nil {
					prog.Statements = append(prog.Statements, stmt)
				}
			}
		}
		if p.peek().Type == TokenEndProgram {
			p.advance()
		}
	} else {
		// No PROGRAM wrapper — parse statements directly.
		prog.Name = "main"
		for p.peek().Type != TokenEOF {
			if p.isVarBlockStart() {
				vb, err := p.parseVarBlock()
				if err != nil {
					return nil, err
				}
				prog.VarBlocks = append(prog.VarBlocks, *vb)
			} else {
				stmt, err := p.parseStatement()
				if err != nil {
					return nil, err
				}
				if stmt != nil {
					prog.Statements = append(prog.Statements, stmt)
				}
			}
		}
	}

	return prog, nil
}

func (p *Parser) isVarBlockStart() bool {
	tt := p.peek().Type
	return tt == TokenVar || tt == TokenVarInput || tt == TokenVarOutput
}

func (p *Parser) parseVarBlock() (*VarBlock, error) {
	kindTok := p.advance()
	vb := &VarBlock{Kind: strings.ToUpper(kindTok.Literal)}

	for p.peek().Type != TokenEndVar && p.peek().Type != TokenEOF {
		decl, err := p.parseVarDecl()
		if err != nil {
			return nil, err
		}
		vb.Variables = append(vb.Variables, *decl)
	}
	p.match(TokenEndVar)
	return vb, nil
}

func (p *Parser) parseVarDecl() (*VarDecl, error) {
	nameTok, err := p.expect(TokenIdent)
	if err != nil {
		return nil, fmt.Errorf("var decl: %w", err)
	}
	if _, err := p.expect(TokenColon); err != nil {
		return nil, fmt.Errorf("var decl: %w", err)
	}

	dtTok := p.advance()
	dt := strings.ToUpper(dtTok.Literal)

	decl := &VarDecl{Name: nameTok.Literal, Datatype: dt}

	// Optional initial value: := expr
	if p.peek().Type == TokenAssign {
		p.advance()
		init, err := p.parseExpression()
		if err != nil {
			return nil, fmt.Errorf("var decl initial: %w", err)
		}
		decl.Initial = init
	}

	p.match(TokenSemicolon)
	return decl, nil
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
	case TokenSemicolon:
		p.advance() // skip extra semicolons
		return nil, nil
	case TokenIdent:
		return p.parseAssignOrCall()
	default:
		// Skip unknown tokens.
		p.advance()
		return nil, nil
	}
}

func (p *Parser) parseAssignOrCall() (Statement, error) {
	nameTok := p.advance()
	name := nameTok.Literal

	// Member access: name.member := ...
	fullName := name
	for p.peek().Type == TokenDot {
		p.advance()
		memberTok := p.advance()
		fullName += "." + memberTok.Literal
	}

	if p.peek().Type == TokenAssign {
		p.advance()
		val, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.match(TokenSemicolon)
		return &AssignStmt{Target: fullName, Value: val}, nil
	}

	if p.peek().Type == TokenLParen {
		call, err := p.parseCallArgs(fullName)
		if err != nil {
			return nil, err
		}
		p.match(TokenSemicolon)
		return &CallStmt{Call: call}, nil
	}

	p.match(TokenSemicolon)
	return &AssignStmt{Target: fullName, Value: &IdentExpr{Name: fullName}}, nil
}

func (p *Parser) parseCallArgs(name string) (*CallExpr, error) {
	p.expect(TokenLParen)
	call := &CallExpr{Name: name}
	for p.peek().Type != TokenRParen && p.peek().Type != TokenEOF {
		arg, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		if !p.match(TokenComma) {
			break
		}
	}
	p.expect(TokenRParen)
	return call, nil
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
		// Case values: expr [, expr] :
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
		body, err := p.parseStatementBlock(TokenEndCase, TokenElse)
		if err != nil {
			return nil, err
		}
		stmt.Cases = append(stmt.Cases, CaseClause{Values: values, Body: body})

		// Check if next is another case value (a number/ident at this level).
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
		operand, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", Operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expression, error) {
	tok := p.peek()
	switch tok.Type {
	case TokenNumber:
		p.advance()
		return &NumberLit{Value: tok.Literal}, nil
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
	case TokenLParen:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.expect(TokenRParen)
		return expr, nil
	case TokenIdent:
		p.advance()
		name := tok.Literal

		// Member access: name.member
		for p.peek().Type == TokenDot {
			p.advance()
			memberTok := p.advance()
			name += "." + memberTok.Literal
		}

		// Function call: name(args)
		if p.peek().Type == TokenLParen {
			call, err := p.parseCallArgs(name)
			if err != nil {
				return nil, err
			}
			return call, nil
		}
		return &IdentExpr{Name: name}, nil
	default:
		return nil, fmt.Errorf("line %d: unexpected token %q", tok.Line, tok.Literal)
	}
}
