//go:build plc || all

package ir

// TypeKind identifies the runtime representation of a value.
type TypeKind uint8

const (
	TypeVoid TypeKind = iota
	TypeBool
	TypeInt    // canonical integer: int64. All ST integer widths collapse here in phase 1.
	TypeReal   // canonical real: float64. LREAL and REAL collapse here in phase 1.
	TypeTime   // duration in milliseconds, stored in I.
	TypeString // UTF-8 string, stored in S.
	TypeStruct // UDT instance (phase 2+).
	TypeArray  // fixed-size array (phase 2+).
	TypeFB     // function block instance (phase 4+).
)

func (k TypeKind) String() string {
	switch k {
	case TypeVoid:
		return "VOID"
	case TypeBool:
		return "BOOL"
	case TypeInt:
		return "INT"
	case TypeReal:
		return "REAL"
	case TypeTime:
		return "TIME"
	case TypeString:
		return "STRING"
	case TypeStruct:
		return "STRUCT"
	case TypeArray:
		return "ARRAY"
	case TypeFB:
		return "FB"
	}
	return "?"
}

// Type is the resolved type of an IR value. Compound types carry extra data.
type Type struct {
	Kind       TypeKind
	Struct     *StructDef // Kind == TypeStruct
	Elem       *Type      // Kind == TypeArray
	ArrLen     int        // Kind == TypeArray
	ArrLoBound int        // Kind == TypeArray — IEC arrays may start at any integer
	FB         *FBDef     // Kind == TypeFB
}

// Singleton scalar types. Use these instead of allocating new *Type for every reference.
var (
	BoolT   = &Type{Kind: TypeBool}
	IntT    = &Type{Kind: TypeInt}
	RealT   = &Type{Kind: TypeReal}
	TimeT   = &Type{Kind: TypeTime}
	StringT = &Type{Kind: TypeString}
	VoidT   = &Type{Kind: TypeVoid}
)

// StructDef describes a UDT. Populated by phase 2.
type StructDef struct {
	Name   string
	Fields []StructField
	// FieldIndex maps field name to its slot index in Value.Fld.
	FieldIndex map[string]int
}

// StructField is a single named field within a UDT.
type StructField struct {
	Name string
	Type *Type
}

// FBDef describes a function block type. Populated by phase 4.
type FBDef struct {
	Name string
	// Slots, body, etc. added when FB support lands.
}

// String renders the type for diagnostic messages.
func (t *Type) String() string {
	if t == nil {
		return "?"
	}
	switch t.Kind {
	case TypeStruct:
		if t.Struct != nil && t.Struct.Name != "" {
			return t.Struct.Name
		}
		return "STRUCT"
	case TypeArray:
		elem := "?"
		if t.Elem != nil {
			elem = t.Elem.String()
		}
		return "ARRAY OF " + elem
	}
	return t.Kind.String()
}

// IsNumeric reports whether t permits arithmetic operators.
func (t *Type) IsNumeric() bool {
	if t == nil {
		return false
	}
	return t.Kind == TypeInt || t.Kind == TypeReal || t.Kind == TypeTime
}

// Equal reports structural equality of two types.
func (t *Type) Equal(other *Type) bool {
	if t == other {
		return true
	}
	if t == nil || other == nil {
		return false
	}
	if t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case TypeStruct:
		return t.Struct == other.Struct
	case TypeArray:
		return t.ArrLen == other.ArrLen && t.ArrLoBound == other.ArrLoBound && t.Elem.Equal(other.Elem)
	case TypeFB:
		return t.FB == other.FB
	}
	return true
}
