//go:build mqtt || all

package mqtt

import (
	"github.com/joyautomation/tentacle/internal/rbe"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/types"
)

// PlcVariable tracks a single PLC variable seen by the bridge.
type PlcVariable struct {
	ID              string
	Description     string
	Datatype        string // "number", "boolean", "string", "udt"
	Value           interface{}
	ModuleID        string // source module (ethernetip, plc, gateway, etc.)
	DeviceID        string
	Deadband        *types.DeadBandConfig
	MemberDeadbands map[string]types.DeadBandConfig
	DisableRBE      bool
	UdtTemplate     *types.UdtTemplateDefinition
	SparkplugType   uint32 // sparkplug DataType code
	RBEState        rbe.State
	// Per-member RBE for UDTs
	MemberRBEStates map[string]*rbe.State
}

// TemplateRegistry tracks Sparkplug B template definitions registered with the node.
type TemplateRegistry struct {
	// name → definition
	definitions map[string]*sparkplug.Template
}

func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{definitions: make(map[string]*sparkplug.Template)}
}

func (r *TemplateRegistry) Has(name string) bool {
	_, ok := r.definitions[name]
	return ok
}

func (r *TemplateRegistry) Get(name string) *sparkplug.Template {
	return r.definitions[name]
}

func (r *TemplateRegistry) Register(name string, tmpl *sparkplug.Template) {
	r.definitions[name] = tmpl
}

func (r *TemplateRegistry) All() map[string]*sparkplug.Template {
	return r.definitions
}
