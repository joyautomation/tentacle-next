//go:build plc || all

package plc

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/joyautomation/tentacle/types"
)

func newTestCache() *DeviceTagCache {
	return NewDeviceTagCache(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// feedSubject pushes a PlcDataMessage into the cache the same way the
// bus subscription would. VariableID carries the raw tag path — that's
// the form user code passes to read_tag.
func feedSubject(c *DeviceTagCache, subject, deviceID, rawTag string, value interface{}) {
	msg := types.PlcDataMessage{DeviceID: deviceID, VariableID: rawTag, Value: value}
	data, _ := json.Marshal(msg)
	c.handle(subject, data)
}

func TestDeviceTagCacheGetByRawPath(t *testing.T) {
	c := newTestCache()
	feedSubject(c,
		"ethernetip.data.rtu60.RTU60_13XFR9_PLC_TOD_SECOND",
		"rtu60", "RTU60_13XFR9_PLC_TOD.SECOND", 42.0)

	v, ok := c.Get("rtu60", "RTU60_13XFR9_PLC_TOD.SECOND")
	if !ok {
		t.Fatalf("expected direct lookup to succeed")
	}
	if v != 42.0 {
		t.Errorf("got %v, want 42.0", v)
	}
}

func TestDeviceTagCacheGetAggregate(t *testing.T) {
	c := newTestCache()
	feedSubject(c, "ethernetip.data.rtu60.X", "rtu60", "RTU60_13XFR9_PLC_TOD.SECOND", 42.0)
	feedSubject(c, "ethernetip.data.rtu60.Y", "rtu60", "RTU60_13XFR9_PLC_TOD.DAY", 12.0)
	feedSubject(c, "ethernetip.data.rtu60.Z", "rtu60", "OTHER_TAG.FIELD", 7.0)

	agg, ok := c.GetAggregate("rtu60", "RTU60_13XFR9_PLC_TOD")
	if !ok {
		t.Fatalf("expected aggregate to succeed")
	}
	if len(agg) != 2 {
		t.Fatalf("expected 2 fields, got %d: %+v", len(agg), agg)
	}
	if agg["SECOND"] != 42.0 {
		t.Errorf("SECOND: got %v", agg["SECOND"])
	}
	if agg["DAY"] != 12.0 {
		t.Errorf("DAY: got %v", agg["DAY"])
	}
	if _, leaked := agg["OTHER_TAG"]; leaked {
		t.Errorf("aggregate bled into OTHER_TAG")
	}
}

func TestDeviceTagCacheGetAggregateMissing(t *testing.T) {
	c := newTestCache()
	feedSubject(c, "ethernetip.data.rtu60.A", "rtu60", "SOMETHING.ELSE", 1.0)
	if _, ok := c.GetAggregate("rtu60", "NO_SUCH_INSTANCE"); ok {
		t.Errorf("expected no aggregate for unknown prefix")
	}
	if _, ok := c.GetAggregate("unknown_device", "X"); ok {
		t.Errorf("expected no aggregate for unknown device")
	}
}

// A tag prefix like "Foo" must not swallow a sibling "Foobar" — the
// separator is a literal dot, not a string prefix.
func TestDeviceTagCacheAggregateRejectsSiblingPrefix(t *testing.T) {
	c := newTestCache()
	feedSubject(c, "eip.data.d.a", "d", "Foo.a", 1.0)
	feedSubject(c, "eip.data.d.b", "d", "Foobar.b", 2.0)

	agg, ok := c.GetAggregate("d", "Foo")
	if !ok {
		t.Fatalf("expected aggregate")
	}
	if len(agg) != 1 || agg["a"] != 1.0 {
		t.Errorf("sibling prefix leaked: %+v", agg)
	}
}

// When a publisher forgets to populate VariableID, we should still
// cache the value under the subject-derived sanitized key so older
// paths keep working.
func TestDeviceTagCacheFallsBackToSubject(t *testing.T) {
	c := newTestCache()
	msg := types.PlcDataMessage{Value: 99.0}
	data, _ := json.Marshal(msg)
	c.handle("modbus.data.dev1.SENSOR_TEMP", data)

	if v, ok := c.Get("dev1", "SENSOR_TEMP"); !ok || v != 99.0 {
		t.Errorf("subject fallback failed: %v ok=%v", v, ok)
	}
}
