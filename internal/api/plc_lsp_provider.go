//go:build (api || all) && (plc || all)

package api

import (
	"github.com/joyautomation/tentacle/internal/plc/lsp"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

// plcLspProvider adapts the API module's KV-backed config to
// lsp.SymbolProvider so the in-process LSP can answer PLC-aware
// completion queries (e.g. `get_var("motor1").` → motor1's template
// fields).
//
// Each lookup re-reads the relevant KV entry — cheap enough for
// keystroke-rate completion and avoids any cache invalidation headache
// when the user edits templates or variables from the same UI session.
type plcLspProvider struct {
	mod   *Module
	plcID string
}

// Variable returns info about a configured PLC variable. Template-typed
// variables carry their template name in TemplateName; atomic variables
// return TemplateName="" so completion falls back to primitive handling.
func (p *plcLspProvider) Variable(name string) *lsp.VariableInfo {
	cfg, err := p.mod.getPlcConfig(p.plcID)
	if err != nil || cfg == nil {
		return nil
	}
	v, ok := cfg.Variables[name]
	if !ok {
		return nil
	}
	info := &lsp.VariableInfo{Name: v.ID, Datatype: v.Datatype}
	if tmpl, err := p.mod.getPlcTemplate(v.Datatype); err == nil && tmpl != nil {
		info.TemplateName = tmpl.Name
	}
	return info
}

// VariableNames returns the names of all variables currently configured
// for the PLC. Used by completion to populate variable-name suggestions
// inside calls like `get_num("|")`.
func (p *plcLspProvider) VariableNames() []string {
	cfg, err := p.mod.getPlcConfig(p.plcID)
	if err != nil || cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Variables))
	for name := range cfg.Variables {
		names = append(names, name)
	}
	return names
}

// Template returns the template definition (fields + methods) for a
// given name, or nil when no template with that name exists.
func (p *plcLspProvider) Template(name string) *lsp.TemplateInfo {
	tmpl, err := p.mod.getPlcTemplate(name)
	if err != nil || tmpl == nil {
		return nil
	}
	fields := make([]lsp.TemplateField, 0, len(tmpl.Fields))
	for _, f := range tmpl.Fields {
		fields = append(fields, lsp.TemplateField{
			Name:        f.Name,
			Type:        f.Type,
			Description: f.Description,
			Unit:        f.Unit,
		})
	}
	methods := make([]lsp.TemplateMethod, 0, len(tmpl.Methods))
	for _, m := range tmpl.Methods {
		methods = append(methods, lsp.TemplateMethod{Name: m.Name})
	}
	return &lsp.TemplateInfo{Name: tmpl.Name, Fields: fields, Methods: methods}
}

// Function returns the signature of a top-level function exported by any
// saved program. The LSP uses this for cross-program completion, hover,
// and arity diagnostics. Today the signature on PlcProgramKV documents the
// program's entry function (same name as the program); non-entry helpers
// don't carry signatures yet, so they won't surface here.
func (p *plcLspProvider) Function(name string) *lsp.FunctionInfo {
	prog, err := p.mod.getPlcProgram(name)
	if err != nil || prog == nil || prog.Signature == nil {
		return nil
	}
	return toFunctionInfo(prog.Name, prog.Description, prog.Signature)
}

// FunctionNames returns the names of every saved program that has a
// declared signature. Programs without signatures are skipped — there's
// nothing useful to offer for them yet.
func (p *plcLspProvider) FunctionNames() []string {
	keys, err := p.mod.bus.KVKeys(topics.BucketPlcPrograms)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		prog, err := p.mod.getPlcProgram(k)
		if err != nil || prog == nil || prog.Signature == nil {
			continue
		}
		names = append(names, prog.Name)
	}
	return names
}

func toFunctionInfo(program, description string, sig *itypes.PlcFunctionSig) *lsp.FunctionInfo {
	info := &lsp.FunctionInfo{
		Name:        program,
		Program:     program,
		Description: description,
	}
	if sig == nil {
		return info
	}
	info.Params = make([]lsp.FunctionParam, 0, len(sig.Params))
	for _, p := range sig.Params {
		info.Params = append(info.Params, lsp.FunctionParam{
			Name:        p.Name,
			Type:        p.Type,
			Description: p.Description,
			Required:    p.Required,
		})
	}
	if sig.Returns != nil {
		info.Returns = &lsp.FunctionReturn{
			Type:        sig.Returns.Type,
			Description: sig.Returns.Description,
		}
	}
	return info
}
