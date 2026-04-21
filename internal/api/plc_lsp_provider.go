//go:build (api || all) && (plc || all)

package api

import (
	"github.com/joyautomation/tentacle/internal/plc/lsp"
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
