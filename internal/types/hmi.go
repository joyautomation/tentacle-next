package types

// HmiAppConfig is the full HMI app configuration stored in the hmi_config
// NATS KV bucket, keyed by appId. An app contains screens and reusable
// UDT-bound components.
type HmiAppConfig struct {
	AppID       string                       `json:"appId"`
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Screens     map[string]HmiScreenConfig   `json:"screens"`
	Components  map[string]HmiComponentConfig `json:"components,omitempty"`
	UpdatedAt   int64                        `json:"updatedAt"`
}

// HmiScreenConfig is a free-form canvas of widgets.
type HmiScreenConfig struct {
	ScreenID string      `json:"screenId"`
	Name     string      `json:"name"`
	Width    int         `json:"width,omitempty"`  // canvas width in px (0 = fluid)
	Height   int         `json:"height,omitempty"` // canvas height in px (0 = fluid)
	Widgets  []HmiWidget `json:"widgets"`
}

// HmiComponentConfig is a reusable widget cluster, optionally bound to a UDT
// template. When UdtTemplate is set, member-bound widgets reference UDT
// member names rather than concrete variables.
type HmiComponentConfig struct {
	ComponentID string      `json:"componentId"`
	Name        string      `json:"name"`
	UdtTemplate string      `json:"udtTemplate,omitempty"`
	Width       int         `json:"width,omitempty"`
	Height      int         `json:"height,omitempty"`
	Widgets     []HmiWidget `json:"widgets"`
}

// HmiWidget is a single widget instance placed on a screen or inside a
// component. Type identifies which renderer (e.g. "label", "numeric",
// "indicator", "gauge", "stack"). Bindings map widget prop names to a
// data source. Container widgets (e.g. "stack") hold ordered Children
// that render in document flow inside the container; for those children
// X/Y/W/H are ignored.
type HmiWidget struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	X        float64                `json:"x"`
	Y        float64                `json:"y"`
	W        float64                `json:"w"`
	H        float64                `json:"h"`
	Props    map[string]interface{} `json:"props,omitempty"`
	Bindings map[string]HmiBinding  `json:"bindings,omitempty"`
	Children []HmiWidget            `json:"children,omitempty"`
}

// HmiBinding describes how a widget prop gets its value.
//
// Kind values:
//   - "variable"   — bound to a gateway variable (Gateway + Variable required)
//   - "udtMember"  — bound to a UDT instance member; either UdtVariable
//                     references a concrete UDT instance, or, when used inside
//                     a UDT-typed component, only Member is set and the
//                     instance is resolved at render time
type HmiBinding struct {
	Kind        string `json:"kind"`
	Gateway     string `json:"gateway,omitempty"`
	Variable    string `json:"variable,omitempty"`
	UdtVariable string `json:"udtVariable,omitempty"`
	Member      string `json:"member,omitempty"`
}
