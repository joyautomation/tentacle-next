//go:build plc || all

package st

import (
	"fmt"
	"strconv"
	"strings"
)

const maxLoopIterations = 10000

// Transpile converts IEC 61131-3 Structured Text source to Starlark source.
// Returns the Starlark source and the extracted variable declarations.
func Transpile(source string) (starlark string, vars []VarDecl, err error) {
	prog, err := Parse(source)
	if err != nil {
		return "", nil, fmt.Errorf("st transpile: %w", err)
	}

	g := &generator{}
	g.generate(prog)

	// Collect all var declarations.
	for _, vb := range prog.VarBlocks {
		vars = append(vars, vb.Variables...)
	}

	return g.String(), vars, nil
}

type generator struct {
	buf    strings.Builder
	indent int
}

func (g *generator) String() string { return g.buf.String() }

func (g *generator) write(s string) { g.buf.WriteString(s) }

func (g *generator) writeLine(s string) {
	for i := 0; i < g.indent; i++ {
		g.write("    ")
	}
	g.write(s)
	g.write("\n")
}

func (g *generator) generate(prog *Program) {
	name := prog.Name
	if name == "" {
		name = "main"
	}
	g.writeLine(fmt.Sprintf("def %s():", name))
	g.indent++

	// Generate initial value assignments from VAR blocks.
	for _, vb := range prog.VarBlocks {
		for _, v := range vb.Variables {
			if v.Initial != nil {
				g.writeLine(fmt.Sprintf("set_var(%q, %s)", v.Name, g.expr(v.Initial)))
			}
		}
	}

	if len(prog.Statements) == 0 && len(prog.VarBlocks) == 0 {
		g.writeLine("pass")
	}

	for _, stmt := range prog.Statements {
		g.stmt(stmt)
	}

	g.indent--
}

func (g *generator) stmt(s Statement) {
	switch stmt := s.(type) {
	case *AssignStmt:
		g.writeLine(fmt.Sprintf("set_var(%q, %s)", stmt.Target, g.expr(stmt.Value)))

	case *IfStmt:
		g.writeLine(fmt.Sprintf("if %s:", g.expr(stmt.Condition)))
		g.indent++
		g.stmtBlock(stmt.Then)
		g.indent--
		for _, elsif := range stmt.ElsIfs {
			g.writeLine(fmt.Sprintf("elif %s:", g.expr(elsif.Condition)))
			g.indent++
			g.stmtBlock(elsif.Body)
			g.indent--
		}
		if len(stmt.Else) > 0 {
			g.writeLine("else:")
			g.indent++
			g.stmtBlock(stmt.Else)
			g.indent--
		}

	case *ForStmt:
		startExpr := g.expr(stmt.Start)
		// FOR i := start TO end → for i in range(start, end+1)
		// FOR i := start TO end BY step → for i in range(start, end+1, step)
		endExpr := fmt.Sprintf("%s + 1", g.expr(stmt.End))
		if stmt.Step != nil {
			g.writeLine(fmt.Sprintf("for %s in range(%s, %s, %s):", stmt.Variable, startExpr, endExpr, g.expr(stmt.Step)))
		} else {
			g.writeLine(fmt.Sprintf("for %s in range(%s, %s):", stmt.Variable, startExpr, endExpr))
		}
		g.indent++
		// In the body, the loop variable is a local, not a PLC var, so we reference it directly.
		g.stmtBlock(stmt.Body)
		g.indent--

	case *WhileStmt:
		// Starlark has no while. Transpile to bounded for + break.
		g.writeLine(fmt.Sprintf("for _ in range(%d):", maxLoopIterations))
		g.indent++
		g.writeLine(fmt.Sprintf("if not (%s):", g.expr(stmt.Condition)))
		g.indent++
		g.writeLine("break")
		g.indent--
		g.stmtBlock(stmt.Body)
		g.indent--

	case *RepeatStmt:
		// REPEAT ... UNTIL cond → for + break at end
		g.writeLine(fmt.Sprintf("for _ in range(%d):", maxLoopIterations))
		g.indent++
		g.stmtBlock(stmt.Body)
		g.writeLine(fmt.Sprintf("if %s:", g.expr(stmt.Condition)))
		g.indent++
		g.writeLine("break")
		g.indent--
		g.indent--

	case *CaseStmt:
		exprStr := g.expr(stmt.Expression)
		for i, c := range stmt.Cases {
			keyword := "if"
			if i > 0 {
				keyword = "elif"
			}
			var conds []string
			for _, v := range c.Values {
				conds = append(conds, fmt.Sprintf("%s == %s", exprStr, g.expr(v)))
			}
			g.writeLine(fmt.Sprintf("%s %s:", keyword, strings.Join(conds, " or ")))
			g.indent++
			g.stmtBlock(c.Body)
			g.indent--
		}
		if len(stmt.Else) > 0 {
			g.writeLine("else:")
			g.indent++
			g.stmtBlock(stmt.Else)
			g.indent--
		}

	case *CallStmt:
		g.writeLine(g.expr(stmt.Call))

	case *ReturnStmt:
		g.writeLine("return")
	}
}

func (g *generator) stmtBlock(stmts []Statement) {
	if len(stmts) == 0 {
		g.writeLine("pass")
		return
	}
	for _, s := range stmts {
		g.stmt(s)
	}
}

func (g *generator) expr(e Expression) string {
	switch expr := e.(type) {
	case *NumberLit:
		return expr.Value

	case *StringLit:
		return strconv.Quote(expr.Value)

	case *BoolLit:
		if expr.Value {
			return "True"
		}
		return "False"

	case *IdentExpr:
		return fmt.Sprintf("get_var(%q)", expr.Name)

	case *BinaryExpr:
		op := g.mapOp(expr.Op)
		return fmt.Sprintf("(%s %s %s)", g.expr(expr.Left), op, g.expr(expr.Right))

	case *UnaryExpr:
		op := g.mapOp(expr.Op)
		return fmt.Sprintf("%s %s", op, g.expr(expr.Operand))

	case *CallExpr:
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.expr(a))
		}
		return fmt.Sprintf("%s(%s)", expr.Name, strings.Join(args, ", "))

	case *MemberExpr:
		return fmt.Sprintf("get_var(%q)", g.memberPath(expr))

	case *TimeLit:
		return strconv.Itoa(parseTimeMs(expr.Raw))

	default:
		return "None"
	}
}

func (g *generator) mapOp(op string) string {
	switch op {
	case "=":
		return "=="
	case "<>":
		return "!="
	case "AND":
		return "and"
	case "OR":
		return "or"
	case "NOT":
		return "not"
	case "XOR":
		return "^"
	case "MOD":
		return "%"
	default:
		return op
	}
}

func (g *generator) memberPath(m *MemberExpr) string {
	if inner, ok := m.Object.(*MemberExpr); ok {
		return g.memberPath(inner) + "." + m.Member
	}
	if ident, ok := m.Object.(*IdentExpr); ok {
		return ident.Name + "." + m.Member
	}
	return m.Member
}

// parseTimeMs converts a time literal like "5s", "100ms", "2m" to milliseconds.
func parseTimeMs(raw string) int {
	raw = strings.TrimSpace(raw)
	if strings.HasSuffix(raw, "ms") {
		n, _ := strconv.Atoi(strings.TrimSuffix(raw, "ms"))
		return n
	}
	if strings.HasSuffix(raw, "s") {
		n, _ := strconv.Atoi(strings.TrimSuffix(raw, "s"))
		return n * 1000
	}
	if strings.HasSuffix(raw, "m") {
		n, _ := strconv.Atoi(strings.TrimSuffix(raw, "m"))
		return n * 60000
	}
	if strings.HasSuffix(raw, "h") {
		n, _ := strconv.Atoi(strings.TrimSuffix(raw, "h"))
		return n * 3600000
	}
	n, _ := strconv.Atoi(raw)
	return n
}
