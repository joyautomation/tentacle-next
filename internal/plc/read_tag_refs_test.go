//go:build plc || all

package plc

import (
	"sort"
	"testing"
)

func TestExtractReadTagRefs(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []ReadTagRef
	}{
		{
			name:   "no calls",
			source: `def main(): log_info("hi")`,
			want:   nil,
		},
		{
			name:   "single literal call",
			source: `def main(): log_info(str(read_tag("rtu60", "RTU60_13XFR9_PLC_TOD.SECOND")))`,
			want:   []ReadTagRef{{DeviceID: "rtu60", Tag: "RTU60_13XFR9_PLC_TOD.SECOND"}},
		},
		{
			name: "multiple calls deduped",
			source: `def main():
    a = read_tag("rtu60", "RTU60_X.Y")
    b = read_tag("rtu60", "RTU60_X.Y")  # duplicate
    c = read_tag("rtu60", "RTU60_X.Z")`,
			want: []ReadTagRef{
				{DeviceID: "rtu60", Tag: "RTU60_X.Y"},
				{DeviceID: "rtu60", Tag: "RTU60_X.Z"},
			},
		},
		{
			name:   "dynamic args skipped",
			source: `def main(): read_tag(dev, tag); read_tag("rtu60", tag); read_tag(dev, "X")`,
			want:   nil,
		},
		{
			name:   "whitespace variations",
			source: `read_tag(  "rtu60" ,  "X.Y"  )`,
			want:   []ReadTagRef{{DeviceID: "rtu60", Tag: "X.Y"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractReadTagRefs(c.source)
			sortRefs := func(r []ReadTagRef) {
				sort.Slice(r, func(i, j int) bool {
					if r[i].DeviceID != r[j].DeviceID {
						return r[i].DeviceID < r[j].DeviceID
					}
					return r[i].Tag < r[j].Tag
				})
			}
			sortRefs(got)
			sortRefs(c.want)
			if len(got) != len(c.want) {
				t.Fatalf("len mismatch: got %v want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("refs[%d]: got %+v want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}
