//go:build plc || all

package lad

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Print emits the canonical text DSL form of d. The output round-trips
// through ParseText to produce a structurally-equal *Diagram.
func Print(d *Diagram) string {
	var b strings.Builder
	b.WriteString("diagram")
	if d.Name != "" {
		b.WriteByte(' ')
		b.WriteString(d.Name)
	}
	b.WriteByte('\n')
	if len(d.Variables) > 0 {
		for _, v := range d.Variables {
			printVar(&b, v)
		}
		b.WriteByte('\n')
	}
	for i, r := range d.Rungs {
		if i > 0 {
			b.WriteByte('\n')
		}
		printRung(&b, r)
	}
	b.WriteString("end\n")
	return b.String()
}

func printVar(b *strings.Builder, v VarDecl) {
	b.WriteString("  var")
	if v.Kind != "" {
		b.WriteByte(' ')
		b.WriteString(v.Kind)
	}
	b.WriteByte(' ')
	b.WriteString(v.Name)
	b.WriteString(" : ")
	b.WriteString(v.Type)
	if v.Init != "" {
		b.WriteString(" := ")
		b.WriteString(v.Init)
	}
	if v.Retain {
		b.WriteString(" RETAIN")
	}
	b.WriteByte('\n')
}

func printRung(b *strings.Builder, r *Rung) {
	if r.Comment != "" {
		for _, line := range strings.Split(r.Comment, "\n") {
			b.WriteString("  // ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("  rung ")
	b.WriteString(printElement(r.Logic, 0))
	for _, o := range r.Outputs {
		b.WriteString(" -> ")
		b.WriteString(printOutput(o))
	}
	b.WriteByte('\n')
}

// elementPrec assigns a binding strength so the printer can decide when a
// child needs parens. Contacts are atomic (∞), `&` (Series) binds tighter
// than `|` (Parallel), matching the conventional reading of the operators.
func elementPrec(e Element) int {
	switch e.(type) {
	case *Contact:
		return 100
	case *Series:
		return 2
	case *Parallel:
		return 1
	}
	return 0
}

func printElement(e Element, parentPrec int) string {
	inner := printElementInner(e)
	if elementPrec(e) < parentPrec {
		return "(" + inner + ")"
	}
	return inner
}

func printElementInner(e Element) string {
	switch x := e.(type) {
	case *Contact:
		return fmt.Sprintf("%s(%s)", x.Form, x.Operand)
	case *Series:
		parts := make([]string, 0, len(x.Items))
		for _, it := range x.Items {
			parts = append(parts, printElement(it, 2))
		}
		return strings.Join(parts, " & ")
	case *Parallel:
		parts := make([]string, 0, len(x.Items))
		for _, it := range x.Items {
			parts = append(parts, printElement(it, 1))
		}
		return strings.Join(parts, " | ")
	}
	return ""
}

func printOutput(o Output) string {
	switch x := o.(type) {
	case *Coil:
		return fmt.Sprintf("%s(%s)", x.Form, x.Operand)
	case *FBCall:
		var b strings.Builder
		b.WriteString(x.Instance)
		if x.PowerInput != "" {
			b.WriteByte('@')
			b.WriteString(x.PowerInput)
		}
		b.WriteByte('(')
		if len(x.Inputs) > 0 {
			keys := make([]string, 0, len(x.Inputs))
			for k := range x.Inputs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(k)
				b.WriteString(" := ")
				b.WriteString(printExpr(x.Inputs[k]))
			}
		}
		b.WriteByte(')')
		return b.String()
	}
	return ""
}

func printExpr(e Expr) string {
	switch x := e.(type) {
	case *Ref:
		return x.Name
	case *IntLit:
		return strconv.FormatInt(x.V, 10)
	case *RealLit:
		return strconv.FormatFloat(x.V, 'g', -1, 64)
	case *BoolLit:
		if x.V {
			return "TRUE"
		}
		return "FALSE"
	case *TimeLit:
		if x.Raw != "" {
			return x.Raw
		}
		return fmt.Sprintf("T#%dms", x.Ms)
	case *StringLit:
		return "'" + strings.ReplaceAll(x.V, "'", `\'`) + "'"
	}
	return ""
}
