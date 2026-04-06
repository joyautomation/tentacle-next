//go:build ethernetip || all

package ethernetip

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/joyautomation/tentacle/types"
)

const browseTimeout = 30 * time.Second

// listTags reads all controller-scoped tags from the PLC using @tags.
func listTags(gateway string, port int) ([]TagEntry, error) {
	attrs := buildListTagAttrs(gateway, port)
	tag, err := createTag(attrs, browseTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create @tags tag: %w", err)
	}
	defer tag.Close()

	if err := tag.Read(browseTimeout); err != nil {
		return nil, fmt.Errorf("failed to read @tags: %w", err)
	}

	return parseTagList(tag)
}

// parseTagList parses the raw @tags response buffer into TagEntry structs.
func parseTagList(tag TagAccessor) ([]TagEntry, error) {
	size := tag.Size()
	offset := 0
	var tags []TagEntry

	for offset < size {
		if offset+22 > size {
			break
		}

		// Skip instance_id (4 bytes)
		offset += 4

		symbolType := tag.GetUint16(offset)
		offset += 2

		elemSize := tag.GetUint16(offset)
		offset += 2

		var arrayDims [3]uint32
		arrayDims[0] = tag.GetUint32(offset)
		offset += 4
		arrayDims[1] = tag.GetUint32(offset)
		offset += 4
		arrayDims[2] = tag.GetUint32(offset)
		offset += 4

		strLen := int(tag.GetUint16(offset))
		offset += 2

		if offset+strLen > size {
			break
		}

		nameBytes := tag.GetRawBytes(offset, strLen)
		name := string(nameBytes)
		offset += strLen

		// Skip system tags and program-scoped containers
		if symbolType&0x1000 != 0 {
			continue
		}

		tags = append(tags, TagEntry{
			Name:       name,
			SymbolType: symbolType,
			ElemSize:   elemSize,
			ArrayDims:  arrayDims,
		})
	}

	return tags, nil
}

// readUdtTemplate reads a UDT template definition using @udt/<id>.
func readUdtTemplate(gateway string, port int, templateID uint16) (*UdtTemplate, error) {
	attrs := buildUdtAttrs(gateway, port, templateID)
	tag, err := createTag(attrs, browseTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create @udt/%d tag: %w", templateID, err)
	}
	defer tag.Close()

	if err := tag.Read(browseTimeout); err != nil {
		return nil, fmt.Errorf("failed to read @udt/%d: %w", templateID, err)
	}

	return parseUdtTemplate(tag, templateID)
}

// parseUdtTemplate parses the raw @udt/<id> response buffer.
func parseUdtTemplate(tag TagAccessor, templateID uint16) (*UdtTemplate, error) {
	size := tag.Size()
	if size < 14 {
		return nil, fmt.Errorf("@udt/%d response too small: %d bytes", templateID, size)
	}

	memberDescWords := tag.GetUint32(2)
	instanceSize := tag.GetUint32(6)
	memberCount := tag.GetUint16(10)

	descOffset := 14
	descs := make([]UdtFieldDesc, 0, memberCount)
	for i := 0; i < int(memberCount); i++ {
		off := descOffset + i*8
		if off+8 > size {
			break
		}
		descs = append(descs, UdtFieldDesc{
			Metadata: tag.GetUint16(off),
			TypeCode: tag.GetUint16(off + 2),
			Offset:   tag.GetUint32(off + 4),
		})
	}

	stringOffset := 14 + int(memberDescWords)*4
	if stringOffset >= size {
		stringOffset = descOffset + int(memberCount)*8
	}

	allStrings := readNullTerminatedStrings(tag, stringOffset, size)

	udtName := ""
	var fieldNames []string
	if len(allStrings) > 0 {
		udtName = allStrings[0]
		if idx := strings.IndexByte(udtName, ';'); idx >= 0 {
			udtName = udtName[:idx]
		}
		if idx := strings.IndexByte(udtName, ':'); idx >= 0 {
			udtName = udtName[:idx]
		}
		fieldNames = allStrings[1:]
	}

	if udtName == "" {
		udtName = fmt.Sprintf("Template_%d", templateID)
	}

	fields := make([]UdtField, 0, len(descs))
	for i, desc := range descs {
		name := ""
		if i < len(fieldNames) {
			name = fieldNames[i]
		} else {
			name = fmt.Sprintf("_member%d", i)
		}

		isHidden := strings.HasPrefix(name, "ZZZZZ") || strings.HasPrefix(name, "__")
		isArray := desc.IsArray()

		var datatype, udtType string
		if desc.IsStruct() {
			datatype = "STRUCT"
		} else {
			rawType := desc.TypeCode & 0x00FF
			if info, ok := cipTypes[rawType]; ok {
				datatype = info.Name
			} else {
				datatype = "UNKNOWN"
			}
		}

		fields = append(fields, UdtField{
			Name:     name,
			Desc:     desc,
			Datatype: datatype,
			UdtName:  udtType,
			IsArray:  isArray,
			IsHidden: isHidden,
		})
	}

	return &UdtTemplate{
		ID:           templateID,
		Name:         udtName,
		InstanceSize: instanceSize,
		MemberCount:  memberCount,
		Fields:       fields,
	}, nil
}

// readNullTerminatedStrings reads all null-terminated strings from offset to end.
func readNullTerminatedStrings(tag TagAccessor, offset, size int) []string {
	var strs []string
	start := offset

	for i := offset; i < size; i++ {
		b := tag.GetUint8(i)
		if b == 0 {
			if i > start {
				buf := tag.GetRawBytes(start, i-start)
				strs = append(strs, string(buf))
			}
			start = i + 1
		}
	}

	if start < size {
		buf := tag.GetRawBytes(start, size-start)
		s := string(buf)
		if len(s) > 0 && isPrintable(s) {
			strs = append(strs, s)
		}
	}

	return strs
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// browseDevice lists all tags and reads all UDT templates from a device.
func browseDevice(gateway string, port int, deviceID string, moduleID string, browseID string, publishProgress func(types.BrowseProgressMessage)) (*BrowseResult, error) {
	if port == 0 {
		port = 44818
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:  browseID,
		ModuleID:  moduleID,
		DeviceID:  deviceID,
		Phase:     "discovering",
		Message:   "Listing all tags...",
		Timestamp: time.Now().UnixMilli(),
	})

	tags, err := listTags(gateway, port)
	if err != nil {
		return nil, fmt.Errorf("tag listing failed: %w", err)
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:  browseID,
		ModuleID:  moduleID,
		DeviceID:  deviceID,
		Phase:     "discovering",
		TotalTags: len(tags),
		Message:   fmt.Sprintf("Found %d tags", len(tags)),
		Timestamp: time.Now().UnixMilli(),
	})

	// Collect unique UDT template IDs
	templateIDs := make(map[uint16]bool)
	for _, t := range tags {
		if t.IsStruct() && !t.IsSystem() {
			templateIDs[t.TemplateID()] = true
		}
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:  browseID,
		ModuleID:  moduleID,
		DeviceID:  deviceID,
		Phase:     "expanding",
		TotalTags: len(tags),
		Message:   fmt.Sprintf("Reading %d UDT templates...", len(templateIDs)),
		Timestamp: time.Now().UnixMilli(),
	})

	templates := make(map[uint16]*UdtTemplate)
	toProcess := make([]uint16, 0, len(templateIDs))
	for id := range templateIDs {
		toProcess = append(toProcess, id)
	}

	for len(toProcess) > 0 {
		id := toProcess[0]
		toProcess = toProcess[1:]

		if _, done := templates[id]; done {
			continue
		}

		tmpl, err := readUdtTemplate(gateway, port, id)
		if err != nil {
			continue
		}

		templates[id] = tmpl

		for _, field := range tmpl.Fields {
			if field.Desc.IsStruct() {
				nestedID := field.Desc.NestedTemplateID()
				if _, done := templates[nestedID]; !done {
					toProcess = append(toProcess, nestedID)
				}
			}
		}
	}

	// Build templateID → name lookup
	templateIDToName := make(map[uint16]string)
	for id, tmpl := range templates {
		templateIDToName[id] = tmpl.Name
	}

	// Resolve nested UDT names
	for _, tmpl := range templates {
		for i, field := range tmpl.Fields {
			if field.Desc.IsStruct() {
				nestedID := field.Desc.NestedTemplateID()
				if name, ok := templateIDToName[nestedID]; ok {
					tmpl.Fields[i].UdtName = name
				}
			}
		}
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:      browseID,
		ModuleID:      moduleID,
		DeviceID:      deviceID,
		Phase:         "reading",
		TotalTags:     len(tags),
		CompletedTags: len(tags),
		Message:       "Building browse result...",
		Timestamp:     time.Now().UnixMilli(),
	})

	var atomicVars []VariableInfo
	var structCandidates []candidateVar
	structTags := make(map[string]string)

	for _, t := range tags {
		if strings.HasPrefix(t.Name, "__DEFVAL_") {
			continue
		}

		if t.IsStruct() {
			tid := t.TemplateID()
			tmpl, ok := templates[tid]
			if !ok {
				continue
			}
			structTags[t.Name] = tmpl.Name
			expandMembers(&structCandidates, t.Name, tmpl, templates, deviceID, moduleID, gateway, port, 0, 1)
		} else {
			rawType := t.SymbolType & 0x00FF
			cipType := resolveCipType(rawType)
			natsType := cipToNatsDatatype(cipType)

			atomicVars = append(atomicVars, VariableInfo{
				ModuleID:    moduleID,
				DeviceID:    deviceID,
				VariableID:  t.Name,
				Value:       nil,
				Datatype:    natsType,
				CipType:     cipType,
				Quality:     "unknown",
				Origin:      "plc",
				LastUpdated: 0,
			})
		}
	}

	readableStructVars := filterReadable(structCandidates, gateway, port, publishProgress, browseID, deviceID, moduleID)

	variables := make([]VariableInfo, 0, len(atomicVars)+len(readableStructVars))
	variables = append(variables, atomicVars...)
	variables = append(variables, readableStructVars...)

	udts := make(map[string]UdtExport)
	for _, tmpl := range templates {
		members := make([]UdtMemberExport, 0)
		for _, field := range tmpl.Fields {
			if field.IsHidden {
				continue
			}
			if field.Datatype == "STRUCT" {
				continue
			}
			natsType := cipToNatsDatatype(field.Datatype)
			members = append(members, UdtMemberExport{
				Name:     field.Name,
				Datatype: natsType,
				CipType:  field.Datatype,
				UdtType:  field.UdtName,
				IsArray:  field.IsArray,
			})
		}
		udts[tmpl.Name] = UdtExport{
			Name:    tmpl.Name,
			Members: members,
		}
	}

	result := &BrowseResult{
		Variables:  variables,
		Udts:       udts,
		StructTags: structTags,
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:      browseID,
		ModuleID:      moduleID,
		DeviceID:      deviceID,
		Phase:         "completed",
		TotalTags:     len(variables),
		CompletedTags: len(variables),
		Message:       fmt.Sprintf("Browse complete: %d variables, %d UDTs", len(variables), len(udts)),
		Timestamp:     time.Now().UnixMilli(),
	})

	return result, nil
}

type candidateVar struct {
	info VariableInfo
	path string
}

func expandMembers(candidates *[]candidateVar, basePath string, tmpl *UdtTemplate, templates map[uint16]*UdtTemplate, deviceID, moduleID string, gateway string, port int, depth, maxDepth int) {
	if depth >= maxDepth {
		return
	}

	for _, field := range tmpl.Fields {
		if field.IsHidden {
			continue
		}

		memberPath := basePath + "." + field.Name

		if field.Datatype == "STRUCT" && field.Desc.IsStruct() {
			nestedID := field.Desc.NestedTemplateID()
			if nestedTmpl, ok := templates[nestedID]; ok {
				expandMembers(candidates, memberPath, nestedTmpl, templates, deviceID, moduleID, gateway, port, depth+1, maxDepth)
				continue
			}
		}

		natsType := cipToNatsDatatype(field.Datatype)

		*candidates = append(*candidates, candidateVar{
			path: memberPath,
			info: VariableInfo{
				ModuleID:    moduleID,
				DeviceID:    deviceID,
				VariableID:  memberPath,
				Value:       nil,
				Datatype:    natsType,
				CipType:     field.Datatype,
				Quality:     "unknown",
				Origin:      "plc",
				LastUpdated: 0,
			},
		})
	}
}

const (
	readableTestTimeout = 2 * time.Second
	readableWorkerCount = 10
)

func filterReadable(candidates []candidateVar, gateway string, port int, publishProgress func(types.BrowseProgressMessage), browseID, deviceID, moduleID string) []VariableInfo {
	type result struct {
		index    int
		readable bool
	}

	results := make(chan result, len(candidates))
	work := make(chan int, len(candidates))

	for i := range candidates {
		work <- i
	}
	close(work)

	var wg sync.WaitGroup
	for w := 0; w < readableWorkerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				readable := testTagReadable(gateway, port, candidates[idx].path)
				results <- result{idx, readable}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	readable := make([]bool, len(candidates))
	completed := 0
	for r := range results {
		readable[r.index] = r.readable
		completed++
		if completed%50 == 0 || completed == len(candidates) {
			publishProgress(types.BrowseProgressMessage{
				BrowseID:      browseID,
				ModuleID:      moduleID,
				DeviceID:      deviceID,
				Phase:         "reading",
				TotalTags:     len(candidates),
				CompletedTags: completed,
				Message:       fmt.Sprintf("Tested %d/%d tag paths", completed, len(candidates)),
				Timestamp:     time.Now().UnixMilli(),
			})
		}
	}

	var out []VariableInfo
	for i, c := range candidates {
		if readable[i] {
			out = append(out, c.info)
		}
	}
	return out
}

func testTagReadable(gateway string, port int, tagPath string) bool {
	attrs := buildTagAttrs(gateway, port, tagPath, 0)
	tag, err := createTag(attrs, readableTestTimeout)
	if err != nil {
		return false
	}
	defer tag.Close()
	if err := tag.Read(readableTestTimeout); err != nil {
		return false
	}
	return true
}
