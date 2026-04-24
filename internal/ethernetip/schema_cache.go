//go:build ethernetip || all

package ethernetip

// storeSchema snapshots the UDT definitions and struct-tag mappings
// from a completed browse, so later subscribe requests can expand a
// struct tag into its member paths without the subscriber needing to
// know the schema.
func (s *Scanner) storeSchema(deviceID string, result *BrowseResult) {
	if result == nil {
		return
	}
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()

	if len(result.Udts) > 0 {
		udts := make(map[string]UdtExport, len(result.Udts))
		for name, def := range result.Udts {
			udts[name] = def
		}
		s.udts[deviceID] = udts
	}
	if len(result.StructTags) > 0 {
		st := make(map[string]string, len(result.StructTags))
		for inst, tmpl := range result.StructTags {
			st[inst] = tmpl
		}
		s.structTags[deviceID] = st
	}
}

// expandStructTag returns member tag paths (with CIP types) for a
// subscribed struct tag, recursing through nested struct members. It
// returns (nil, false) if the tag is not a known struct instance, so
// the caller can fall back to the tag as-is.
//
// The recursion terminates at primitive members; nested STRUCT members
// whose template isn't in the UDT cache are skipped (browse should
// include all reachable templates, but if it doesn't, we'd rather
// under-subscribe than enumerate garbage).
func (s *Scanner) expandStructTag(deviceID, tagName string) (members map[string]string, ok bool) {
	s.schemaMu.RLock()
	defer s.schemaMu.RUnlock()

	st := s.structTags[deviceID]
	if st == nil {
		return nil, false
	}
	tmplName, isStruct := st[tagName]
	if !isStruct {
		return nil, false
	}
	udts := s.udts[deviceID]
	if udts == nil {
		return nil, false
	}
	out := make(map[string]string)
	s.walkMembers(udts, tmplName, tagName, out, 0)
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

const maxExpandDepth = 8

func (s *Scanner) walkMembers(udts map[string]UdtExport, tmplName, basePath string, out map[string]string, depth int) {
	if depth >= maxExpandDepth {
		return
	}
	tmpl, ok := udts[tmplName]
	if !ok {
		return
	}
	for _, m := range tmpl.Members {
		path := basePath + "." + m.Name
		if m.CipType == "STRUCT" && m.UdtType != "" {
			s.walkMembers(udts, m.UdtType, path, out, depth+1)
			continue
		}
		out[path] = m.CipType
	}
}
