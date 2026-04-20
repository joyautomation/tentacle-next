package types

// PlcTemplate defines a reusable UDT/struct shape for PLC variables.
// Stored in the plc_templates KV bucket, keyed by Name.
type PlcTemplate struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Tags        []string             `json:"tags,omitempty"`
	Fields      []PlcTemplateField   `json:"fields"`
	Methods     []PlcTemplateMethod  `json:"methods,omitempty"`
	UpdatedAt   int64                `json:"updatedAt"`
	UpdatedBy   string               `json:"updatedBy,omitempty"`
}

// PlcTemplateField is one field in a template.
//
// Type is a primitive ("bool", "number", "string", "bytes"), another
// template name, or a collection suffix applied to either:
//   - "Motor[]"  — array of Motor
//   - "Motor{}"  — string-keyed record of Motor
//   - "number[]" — array of primitives
type PlcTemplateField struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description,omitempty"`
	Unit        string      `json:"unit,omitempty"`
}

// PlcTemplateMethod binds a free-standing function to a template. The
// same binding enables both method-style and free-function-style calls
// at the runtime layer:
//
//	motor.start()   →  plc.motor_start(motor)
//	start(motor)    →  plc.motor_start(motor)
type PlcTemplateMethod struct {
	Name     string          `json:"name"`
	Function PlcFunctionRef  `json:"function"`
}

// PlcFunctionRef points at a function defined in the programs bucket.
type PlcFunctionRef struct {
	Module string `json:"module"`
	Name   string `json:"name"`
}
