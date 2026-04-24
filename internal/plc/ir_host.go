//go:build plc || all

package plc

import (
	"encoding/json"
	"time"

	"github.com/joyautomation/tentacle/internal/plc/ir"
)

// irHost adapts a *VariableStore to the ir.Host interface so an IR program
// can read and write PLC-wide variables. Globals referenced by a lowered
// ST program go through this shim instead of the slot table.
type irHost struct {
	vars *VariableStore
}

func newIRHost(vars *VariableStore) *irHost { return &irHost{vars: vars} }

func (h *irHost) ReadGlobal(name string) (ir.Value, error) {
	return goToIRValue(h.vars.Get(name)), nil
}

func (h *irHost) WriteGlobal(name string, v ir.Value) error {
	h.vars.Set(name, irValueToGo(v), time.Now().UnixMilli())
	return nil
}

func (h *irHost) NowMs() int64 { return time.Now().UnixMilli() }

// goToIRValue converts the loosely-typed value carried in VariableStore to
// the IR's tagged-union Value. Unknown shapes collapse to TypeVoid so the
// caller can still detect the absence of a value.
func goToIRValue(v interface{}) ir.Value {
	if v == nil {
		return ir.Value{}
	}
	switch x := v.(type) {
	case bool:
		return ir.BoolVal(x)
	case int:
		return ir.IntVal(int64(x))
	case int32:
		return ir.IntVal(int64(x))
	case int64:
		return ir.IntVal(x)
	case float32:
		return ir.RealVal(float64(x))
	case float64:
		return ir.RealVal(x)
	case string:
		return ir.StringVal(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return ir.IntVal(i)
		}
		if f, err := x.Float64(); err == nil {
			return ir.RealVal(f)
		}
	}
	return ir.Value{}
}

// irValueToGo converts an IR Value into the canonical Go shape stored in
// VariableStore. Numbers normalise to float64 to match the existing
// Starlark↔Go bridge so /api/v1/variables stays consistent regardless of
// which engine produced the value.
func irValueToGo(v ir.Value) interface{} {
	switch v.Kind {
	case ir.TypeBool:
		return v.B
	case ir.TypeInt, ir.TypeTime:
		return float64(v.I)
	case ir.TypeReal:
		return v.F
	case ir.TypeString:
		return v.S
	}
	return nil
}
