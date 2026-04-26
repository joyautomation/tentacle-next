//go:build plc || all

package lad

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/plc/st"
)

// Parse decodes the canonical JSON form of a LAD diagram. Discriminator
// fields ("kind", "form") drive the dispatch from the polymorphic
// element/output/expression nodes; unknown discriminators fail loudly so
// schema drift surfaces at compile time, not at scan time.
func Parse(source string) (*Diagram, error) {
	var raw struct {
		Name      string          `json:"name"`
		Variables []VarDecl       `json:"variables"`
		Rungs     []json.RawMessage `json:"rungs"`
	}
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		return nil, fmt.Errorf("parse lad: %w", err)
	}
	d := &Diagram{Name: raw.Name, Variables: raw.Variables}
	for i, rr := range raw.Rungs {
		rung, err := parseRung(rr)
		if err != nil {
			return nil, fmt.Errorf("rung %d: %w", i, err)
		}
		d.Rungs = append(d.Rungs, rung)
	}
	return d, nil
}

func parseRung(data json.RawMessage) (*Rung, error) {
	var raw struct {
		Comment string            `json:"comment"`
		Logic   json.RawMessage   `json:"logic"`
		Outputs []json.RawMessage `json:"outputs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	rung := &Rung{Comment: raw.Comment}
	if len(raw.Logic) == 0 {
		return nil, fmt.Errorf("rung missing logic")
	}
	logic, err := parseElement(raw.Logic)
	if err != nil {
		return nil, fmt.Errorf("logic: %w", err)
	}
	rung.Logic = logic
	for i, or := range raw.Outputs {
		out, err := parseOutput(or)
		if err != nil {
			return nil, fmt.Errorf("output %d: %w", i, err)
		}
		rung.Outputs = append(rung.Outputs, out)
	}
	return rung, nil
}

func parseElement(data json.RawMessage) (Element, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Kind {
	case "contact":
		var c Contact
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		c.Form = strings.ToUpper(strings.TrimSpace(c.Form))
		if c.Form != "NO" && c.Form != "NC" {
			return nil, fmt.Errorf("contact form must be NO or NC, got %q", c.Form)
		}
		if c.Operand == "" {
			return nil, fmt.Errorf("contact missing operand")
		}
		return &c, nil
	case "series", "parallel":
		var raw struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if len(raw.Items) == 0 {
			return nil, fmt.Errorf("%s requires at least one item", head.Kind)
		}
		items := make([]Element, 0, len(raw.Items))
		for i, ir := range raw.Items {
			el, err := parseElement(ir)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", head.Kind, i, err)
			}
			items = append(items, el)
		}
		if head.Kind == "series" {
			return &Series{Items: items}, nil
		}
		return &Parallel{Items: items}, nil
	}
	return nil, fmt.Errorf("unknown element kind %q", head.Kind)
}

func parseOutput(data json.RawMessage) (Output, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Kind {
	case "coil":
		var c Coil
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		c.Form = strings.ToUpper(strings.TrimSpace(c.Form))
		switch c.Form {
		case "OTE", "OTL", "OTU":
		default:
			return nil, fmt.Errorf("coil form must be OTE, OTL, or OTU, got %q", c.Form)
		}
		if c.Operand == "" {
			return nil, fmt.Errorf("coil missing operand")
		}
		return &c, nil
	case "fb":
		var raw struct {
			Instance   string                     `json:"instance"`
			PowerInput string                     `json:"powerInput"`
			Inputs     map[string]json.RawMessage `json:"inputs"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Instance == "" {
			return nil, fmt.Errorf("fb missing instance")
		}
		fb := &FBCall{Instance: raw.Instance, PowerInput: raw.PowerInput}
		if len(raw.Inputs) > 0 {
			fb.Inputs = make(map[string]Expr, len(raw.Inputs))
			for k, v := range raw.Inputs {
				e, err := parseExpr(v)
				if err != nil {
					return nil, fmt.Errorf("fb input %q: %w", k, err)
				}
				fb.Inputs[k] = e
			}
		}
		return fb, nil
	}
	return nil, fmt.Errorf("unknown output kind %q", head.Kind)
}

func parseExpr(data json.RawMessage) (Expr, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Kind {
	case "ref":
		var r Ref
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		if r.Name == "" {
			return nil, fmt.Errorf("ref missing name")
		}
		return &r, nil
	case "int":
		var raw struct {
			Value json.Number `json:"value"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		v, err := strconv.ParseInt(string(raw.Value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("int literal: %w", err)
		}
		return &IntLit{V: v}, nil
	case "real":
		var raw struct {
			Value float64 `json:"value"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		return &RealLit{V: raw.Value}, nil
	case "bool":
		var raw struct {
			Value bool `json:"value"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		return &BoolLit{V: raw.Value}, nil
	case "time":
		var raw TimeLit
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Raw != "" && raw.Ms == 0 {
			raw.Ms = int64(st.ParseTimeMs(stripTimePrefix(raw.Raw)))
		}
		return &raw, nil
	case "string":
		var s StringLit
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return &s, nil
	}
	return nil, fmt.Errorf("unknown expr kind %q", head.Kind)
}
