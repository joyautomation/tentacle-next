//go:build plc || all

package plc

import "regexp"

// ReadTagRef is a (deviceId, tag) pair harvested from a literal
// read_tag("dev", "tag") call inside a Starlark program. These refs drive
// automatic scanner subscriptions so users don't have to declare a PLC
// input variable for every tag they want to read.
type ReadTagRef struct {
	DeviceID string
	Tag      string
}

// readTagCallRE matches read_tag("dev", "tag") where both args are plain
// double-quoted string literals. Single quotes, f-strings, or non-literal
// args (variables, concatenations) are intentionally skipped — those call
// sites must still be backed by a declared PLC input variable.
var readTagCallRE = regexp.MustCompile(`read_tag\s*\(\s*"([^"\\]*(?:\\.[^"\\]*)*)"\s*,\s*"([^"\\]*(?:\\.[^"\\]*)*)"\s*\)`)

// extractReadTagRefs scans a single source file for literal read_tag calls
// and returns the deduped set of (device, tag) pairs it references.
func extractReadTagRefs(source string) []ReadTagRef {
	matches := readTagCallRE.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	refs := make([]ReadTagRef, 0, len(matches))
	for _, m := range matches {
		key := m[1] + "\x00" + m[2]
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ReadTagRef{DeviceID: m[1], Tag: m[2]})
	}
	return refs
}

// collectReadTagRefs merges extracted refs from every program source into a
// single deduped slice.
func collectReadTagRefs(sources map[string]string) []ReadTagRef {
	seen := make(map[string]struct{})
	var refs []ReadTagRef
	for _, src := range sources {
		for _, r := range extractReadTagRefs(src) {
			key := r.DeviceID + "\x00" + r.Tag
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, r)
		}
	}
	return refs
}
