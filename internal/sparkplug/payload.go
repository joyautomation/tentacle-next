package sparkplug

import (
	"fmt"
	"time"

	pb "github.com/joyautomation/tentacle/internal/sparkplug/pb"
	"google.golang.org/protobuf/proto"
)

// Metric is a user-friendly Sparkplug B metric.
type Metric struct {
	Name         string
	Alias        uint64
	Timestamp    uint64
	Datatype     uint32
	IsHistorical bool
	IsTransient  bool
	IsNull       bool
	Value        interface{} // bool, int64, uint64, float32, float64, string, []byte, *Template
}

// Template is a user-friendly Sparkplug B template value.
type Template struct {
	Version      string
	TemplateRef  string
	IsDefinition bool
	Metrics      []Metric
	Parameters   []Parameter
}

// Parameter is a user-friendly Sparkplug B template parameter.
type Parameter struct {
	Name     string
	Datatype uint32
	Value    interface{} // bool, int64, uint64, float32, float64, string
}

// Payload is a user-friendly Sparkplug B payload.
//
// OmitSeq controls whether the seq field is set in the protobuf output. Per
// the Sparkplug B spec, NDEATH MUST NOT include a sequence number — set
// OmitSeq=true on NDEATH payloads. All other Sparkplug message types
// (NBIRTH, NDATA, DBIRTH, DDATA, DDEATH) include seq.
type Payload struct {
	Timestamp uint64
	Seq       uint64
	OmitSeq   bool
	Metrics   []Metric
}

// EncodePayload encodes a user-friendly Payload into protobuf bytes.
func EncodePayload(p *Payload) ([]byte, error) {
	pbPayload := &pb.Payload{
		Timestamp: proto.Uint64(p.Timestamp),
	}
	if !p.OmitSeq {
		pbPayload.Seq = proto.Uint64(p.Seq)
	}
	for i := range p.Metrics {
		pbMetric, err := encodeMetric(&p.Metrics[i])
		if err != nil {
			return nil, fmt.Errorf("metric %q: %w", p.Metrics[i].Name, err)
		}
		pbPayload.Metrics = append(pbPayload.Metrics, pbMetric)
	}
	return proto.Marshal(pbPayload)
}

// DecodePayload decodes protobuf bytes into a user-friendly Payload.
func DecodePayload(data []byte) (*Payload, error) {
	pbPayload := &pb.Payload{}
	if err := proto.Unmarshal(data, pbPayload); err != nil {
		return nil, err
	}
	p := &Payload{
		Timestamp: pbPayload.GetTimestamp(),
		Seq:       pbPayload.GetSeq(),
	}
	for _, pbm := range pbPayload.GetMetrics() {
		m := decodeMetric(pbm)
		p.Metrics = append(p.Metrics, m)
	}
	return p, nil
}

// NewMetric creates a metric with the given name, type, and value.
// Timestamp defaults to now.
func NewMetric(name string, datatype uint32, value interface{}) Metric {
	return Metric{
		Name:      name,
		Datatype:  datatype,
		Timestamp: uint64(time.Now().UnixMilli()),
		Value:     value,
	}
}

// NewBoolMetric creates a Boolean metric.
func NewBoolMetric(name string, value bool) Metric {
	return NewMetric(name, TypeBoolean, value)
}

// NewDoubleMetric creates a Double metric.
func NewDoubleMetric(name string, value float64) Metric {
	return NewMetric(name, TypeDouble, value)
}

// NewStringMetric creates a String metric.
func NewStringMetric(name string, value string) Metric {
	return NewMetric(name, TypeString, value)
}

// NewUInt64Metric creates a UInt64 metric.
func NewUInt64Metric(name string, value uint64) Metric {
	return NewMetric(name, TypeUInt64, value)
}

func encodeMetric(m *Metric) (*pb.Payload_Metric, error) {
	pbm := &pb.Payload_Metric{
		Datatype: proto.Uint32(m.Datatype),
	}
	if m.Name != "" {
		pbm.Name = proto.String(m.Name)
	}
	if m.Alias != 0 {
		pbm.Alias = proto.Uint64(m.Alias)
	}
	if m.Timestamp != 0 {
		pbm.Timestamp = proto.Uint64(m.Timestamp)
	}
	if m.IsHistorical {
		pbm.IsHistorical = proto.Bool(true)
	}
	if m.IsTransient {
		pbm.IsTransient = proto.Bool(true)
	}
	if m.IsNull {
		pbm.IsNull = proto.Bool(true)
		return pbm, nil
	}

	if err := setMetricValue(pbm, m.Datatype, m.Value); err != nil {
		return nil, err
	}
	return pbm, nil
}

func setMetricValue(pbm *pb.Payload_Metric, dt uint32, value interface{}) error {
	if value == nil {
		pbm.IsNull = proto.Bool(true)
		return nil
	}

	switch dt {
	case TypeInt8, TypeInt16, TypeInt32:
		v, err := toUint32(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_IntValue{IntValue: v}

	case TypeUInt8, TypeUInt16, TypeUInt32:
		v, err := toUint32(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_IntValue{IntValue: v}

	case TypeInt64, TypeDateTime:
		v, err := toUint64(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_LongValue{LongValue: v}

	case TypeUInt64:
		v, err := toUint64(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_LongValue{LongValue: v}

	case TypeFloat:
		v, err := toFloat32(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_FloatValue{FloatValue: v}

	case TypeDouble:
		v, err := toFloat64(value)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_DoubleValue{DoubleValue: v}

	case TypeBoolean:
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
		pbm.Value = &pb.Payload_Metric_BooleanValue{BooleanValue: v}

	case TypeString, TypeText:
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		pbm.Value = &pb.Payload_Metric_StringValue{StringValue: v}

	case TypeBytes:
		v, ok := value.([]byte)
		if !ok {
			return fmt.Errorf("expected []byte, got %T", value)
		}
		pbm.Value = &pb.Payload_Metric_BytesValue{BytesValue: v}

	case TypeTemplate:
		tmpl, ok := value.(*Template)
		if !ok {
			return fmt.Errorf("expected *Template, got %T", value)
		}
		pbTmpl, err := encodeTemplate(tmpl)
		if err != nil {
			return err
		}
		pbm.Value = &pb.Payload_Metric_TemplateValue{TemplateValue: pbTmpl}

	default:
		// Fallback: try string
		if s, ok := value.(string); ok {
			pbm.Value = &pb.Payload_Metric_StringValue{StringValue: s}
		} else {
			return fmt.Errorf("unsupported datatype %d for value %T", dt, value)
		}
	}
	return nil
}

func encodeTemplate(t *Template) (*pb.Payload_Template, error) {
	pbt := &pb.Payload_Template{}
	if t.Version != "" {
		pbt.Version = proto.String(t.Version)
	}
	if t.TemplateRef != "" {
		pbt.TemplateRef = proto.String(t.TemplateRef)
	}
	// Per Sparkplug B `payloads-template-is-definition`, every Template
	// (both definitions and instances) MUST include this flag — true for
	// definitions in NBIRTH, false for instances in DBIRTH/DDATA.
	pbt.IsDefinition = proto.Bool(t.IsDefinition)
	for i := range t.Metrics {
		pbm, err := encodeMetric(&t.Metrics[i])
		if err != nil {
			return nil, err
		}
		pbt.Metrics = append(pbt.Metrics, pbm)
	}
	for i := range t.Parameters {
		pbp, err := encodeParameter(&t.Parameters[i])
		if err != nil {
			return nil, err
		}
		pbt.Parameters = append(pbt.Parameters, pbp)
	}
	return pbt, nil
}

func encodeParameter(p *Parameter) (*pb.Payload_Template_Parameter, error) {
	pbp := &pb.Payload_Template_Parameter{
		Name: proto.String(p.Name),
		Type: proto.Uint32(p.Datatype),
	}
	if p.Value == nil {
		return pbp, nil
	}
	switch p.Datatype {
	case TypeInt8, TypeInt16, TypeInt32, TypeUInt8, TypeUInt16, TypeUInt32:
		v, err := toUint32(p.Value)
		if err != nil {
			return nil, err
		}
		pbp.Value = &pb.Payload_Template_Parameter_IntValue{IntValue: v}
	case TypeInt64, TypeUInt64, TypeDateTime:
		v, err := toUint64(p.Value)
		if err != nil {
			return nil, err
		}
		pbp.Value = &pb.Payload_Template_Parameter_LongValue{LongValue: v}
	case TypeFloat:
		v, err := toFloat32(p.Value)
		if err != nil {
			return nil, err
		}
		pbp.Value = &pb.Payload_Template_Parameter_FloatValue{FloatValue: v}
	case TypeDouble:
		v, err := toFloat64(p.Value)
		if err != nil {
			return nil, err
		}
		pbp.Value = &pb.Payload_Template_Parameter_DoubleValue{DoubleValue: v}
	case TypeBoolean:
		v, ok := p.Value.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", p.Value)
		}
		pbp.Value = &pb.Payload_Template_Parameter_BooleanValue{BooleanValue: v}
	case TypeString, TypeText:
		v, ok := p.Value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", p.Value)
		}
		pbp.Value = &pb.Payload_Template_Parameter_StringValue{StringValue: v}
	}
	return pbp, nil
}

func decodeMetric(pbm *pb.Payload_Metric) Metric {
	m := Metric{
		Name:         pbm.GetName(),
		Alias:        pbm.GetAlias(),
		Timestamp:    pbm.GetTimestamp(),
		Datatype:     pbm.GetDatatype(),
		IsHistorical: pbm.GetIsHistorical(),
		IsTransient:  pbm.GetIsTransient(),
		IsNull:       pbm.GetIsNull(),
	}
	if m.IsNull {
		return m
	}

	dt := m.Datatype
	switch dt {
	case TypeInt8, TypeInt16, TypeInt32, TypeUInt8, TypeUInt16, TypeUInt32:
		if v, ok := pbm.Value.(*pb.Payload_Metric_IntValue); ok {
			m.Value = v.IntValue
		}
	case TypeInt64, TypeUInt64, TypeDateTime:
		if v, ok := pbm.Value.(*pb.Payload_Metric_LongValue); ok {
			m.Value = v.LongValue
		}
	case TypeFloat:
		if v, ok := pbm.Value.(*pb.Payload_Metric_FloatValue); ok {
			m.Value = v.FloatValue
		}
	case TypeDouble:
		if v, ok := pbm.Value.(*pb.Payload_Metric_DoubleValue); ok {
			m.Value = v.DoubleValue
		}
	case TypeBoolean:
		if v, ok := pbm.Value.(*pb.Payload_Metric_BooleanValue); ok {
			m.Value = v.BooleanValue
		}
	case TypeString, TypeText:
		if v, ok := pbm.Value.(*pb.Payload_Metric_StringValue); ok {
			m.Value = v.StringValue
		}
	case TypeBytes:
		if v, ok := pbm.Value.(*pb.Payload_Metric_BytesValue); ok {
			m.Value = v.BytesValue
		}
	case TypeTemplate:
		if v, ok := pbm.Value.(*pb.Payload_Metric_TemplateValue); ok && v.TemplateValue != nil {
			m.Value = decodeTemplate(v.TemplateValue)
		}
	}
	return m
}

func decodeTemplate(pbt *pb.Payload_Template) *Template {
	t := &Template{
		Version:      pbt.GetVersion(),
		TemplateRef:  pbt.GetTemplateRef(),
		IsDefinition: pbt.GetIsDefinition(),
	}
	for _, pbm := range pbt.GetMetrics() {
		t.Metrics = append(t.Metrics, decodeMetric(pbm))
	}
	for _, pbp := range pbt.GetParameters() {
		t.Parameters = append(t.Parameters, decodeParameter(pbp))
	}
	return t
}

func decodeParameter(pbp *pb.Payload_Template_Parameter) Parameter {
	p := Parameter{
		Name:     pbp.GetName(),
		Datatype: pbp.GetType(),
	}
	switch {
	case pbp.GetIntValue() != 0:
		p.Value = pbp.GetIntValue()
	case pbp.GetLongValue() != 0:
		p.Value = pbp.GetLongValue()
	case pbp.GetFloatValue() != 0:
		p.Value = pbp.GetFloatValue()
	case pbp.GetDoubleValue() != 0:
		p.Value = pbp.GetDoubleValue()
	case pbp.GetBooleanValue():
		p.Value = pbp.GetBooleanValue()
	case pbp.GetStringValue() != "":
		p.Value = pbp.GetStringValue()
	}
	return p
}

// ═══════════════════════════════════════════════════════════════════════════
// Numeric conversion helpers
// ═══════════════════════════════════════════════════════════════════════════

func toUint32(v interface{}) (uint32, error) {
	switch n := v.(type) {
	case int:
		return uint32(n), nil
	case int8:
		return uint32(n), nil
	case int16:
		return uint32(n), nil
	case int32:
		return uint32(n), nil
	case int64:
		return uint32(n), nil
	case uint:
		return uint32(n), nil
	case uint8:
		return uint32(n), nil
	case uint16:
		return uint32(n), nil
	case uint32:
		return n, nil
	case uint64:
		return uint32(n), nil
	case float32:
		return uint32(n), nil
	case float64:
		return uint32(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to uint32", v)
	}
}

func toUint64(v interface{}) (uint64, error) {
	switch n := v.(type) {
	case int:
		return uint64(n), nil
	case int8:
		return uint64(n), nil
	case int16:
		return uint64(n), nil
	case int32:
		return uint64(n), nil
	case int64:
		return uint64(n), nil
	case uint:
		return uint64(n), nil
	case uint8:
		return uint64(n), nil
	case uint16:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case uint64:
		return n, nil
	case float32:
		return uint64(n), nil
	case float64:
		return uint64(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to uint64", v)
	}
}

func toFloat32(v interface{}) (float32, error) {
	switch n := v.(type) {
	case float32:
		return n, nil
	case float64:
		return float32(n), nil
	case int:
		return float32(n), nil
	case int64:
		return float32(n), nil
	case uint64:
		return float32(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float32", v)
	}
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
