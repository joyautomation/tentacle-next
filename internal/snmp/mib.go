//go:build snmp || all

package snmp

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MibNode represents a parsed OID definition from a MIB file.
type MibNode struct {
	Name       string
	OID        string
	Parent     string
	Index      int
	Syntax     string
	MaxAccess  string
	SequenceOf string   // table objects: "SEQUENCE OF X" -> "X"
	IndexNames []string // entry objects: INDEX { a, b } -> ["a", "b"]
}

// TableColumn describes a column in an SNMP table.
type TableColumn struct {
	Name     string
	OID      string
	SubId    int
	Syntax   string
	Datatype string // "number", "string", "boolean"
}

// TableInfo describes a detected SNMP table.
type TableInfo struct {
	TableName string
	TableOID  string
	EntryName string
	EntryOID  string
	TypeName  string // the SEQUENCE type name (e.g., "IfEntry")
	Columns   []TableColumn
}

// MibTree provides OID-to-name resolution from loaded MIB files.
type MibTree struct {
	ByOID  map[string]*MibNode
	ByName map[string]string
	Tables map[string]*TableInfo // entryOID -> TableInfo
}

// Well-known OID roots that don't need to be defined in any MIB file.
var wellKnownRoots = map[string]string{
	"iso":          ".1",
	"org":          ".1.3",
	"dod":          ".1.3.6",
	"internet":     ".1.3.6.1",
	"directory":    ".1.3.6.1.1",
	"mgmt":         ".1.3.6.1.2",
	"mib-2":        ".1.3.6.1.2.1",
	"system":       ".1.3.6.1.2.1.1",
	"interfaces":   ".1.3.6.1.2.1.2",
	"at":           ".1.3.6.1.2.1.3",
	"ip":           ".1.3.6.1.2.1.4",
	"icmp":         ".1.3.6.1.2.1.5",
	"tcp":          ".1.3.6.1.2.1.6",
	"udp":          ".1.3.6.1.2.1.7",
	"egp":          ".1.3.6.1.2.1.8",
	"transmission": ".1.3.6.1.2.1.10",
	"snmp":         ".1.3.6.1.2.1.11",
	"experimental": ".1.3.6.1.3",
	"private":      ".1.3.6.1.4",
	"enterprises":  ".1.3.6.1.4.1",
	"security":     ".1.3.6.1.5",
	"snmpV2":       ".1.3.6.1.6",
	"snmpDomains":  ".1.3.6.1.6.1",
	"snmpProxys":   ".1.3.6.1.6.2",
	"snmpModules":  ".1.3.6.1.6.3",
}

type rawDefinition struct {
	name       string
	parent     string
	index      int
	syntax     string
	maxAccess  string
	sequenceOf string   // "SEQUENCE OF X" -> "X"
	indexNames []string // INDEX { a, b }
}

// Precompiled regexes for MIB parsing.
var (
	commentRe          = regexp.MustCompile(`--.*$`)
	oidIdentRe         = regexp.MustCompile(`([\w-]+)\s+OBJECT\s+IDENTIFIER\s*::=\s*\{\s*([\w-]+)\s+(\d+)\s*\}`)
	moduleIdentRe      = regexp.MustCompile(`(?s)([\w-]+)\s+MODULE-IDENTITY.*?::=\s*\{\s*([\w-]+)\s+(\d+)\s*\}`)
	objectTypeRe       = regexp.MustCompile(`(?s)([\w-]+)\s+OBJECT-TYPE\s+(.*?)::=\s*\{\s*([\w-]+)\s+(\d+)\s*\}`)
	objectIdentityRe   = regexp.MustCompile(`(?s)([\w-]+)\s+OBJECT-IDENTITY.*?::=\s*\{\s*([\w-]+)\s+(\d+)\s*\}`)
	notificationTypeRe = regexp.MustCompile(`(?s)([\w-]+)\s+NOTIFICATION-TYPE.*?::=\s*\{\s*([\w-]+)\s+(\d+)\s*\}`)
	syntaxRe           = regexp.MustCompile(`SYNTAX\s+([\w().\-\s]+?)(?:\s+(?:MAX-ACCESS|ACCESS|STATUS|DESCRIPTION|INDEX|DEFVAL|AUGMENTS|REFERENCE)\s)`)
	accessRe           = regexp.MustCompile(`(?:MAX-ACCESS|ACCESS)\s+([\w-]+)`)
	sequenceOfRe       = regexp.MustCompile(`SEQUENCE\s+OF\s+(\w+)`)
	indexRe            = regexp.MustCompile(`INDEX\s*\{\s*([\w\s,]+)\}`)
)

// importsBlockRe matches IMPORTS ... ; blocks that can confuse the object parsers.
var importsBlockRe = regexp.MustCompile(`(?s)\bIMPORTS\b.*?;`)

// preprocess strips MIB comments and IMPORTS blocks.
func preprocess(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = commentRe.ReplaceAllString(line, "")
	}
	cleaned := strings.Join(lines, "\n")
	// Remove IMPORTS blocks so keywords listed as imports don't get matched as definitions.
	cleaned = importsBlockRe.ReplaceAllString(cleaned, "")
	return cleaned
}

// parseMibText extracts OID definitions from a single MIB file.
func parseMibText(text string) []rawDefinition {
	clean := preprocess(text)
	var defs []rawDefinition
	seen := make(map[string]bool)

	addDef := func(name, parent string, index int, syntax, maxAccess string) {
		if seen[name] {
			return
		}
		seen[name] = true
		defs = append(defs, rawDefinition{name: name, parent: parent, index: index, syntax: syntax, maxAccess: maxAccess})
	}

	// Pattern 1: OBJECT IDENTIFIER ::= { parent index }
	for _, m := range oidIdentRe.FindAllStringSubmatch(clean, -1) {
		idx, _ := strconv.Atoi(m[3])
		addDef(m[1], m[2], idx, "", "")
	}

	// Pattern 2: MODULE-IDENTITY ... ::= { parent index }
	for _, m := range moduleIdentRe.FindAllStringSubmatch(clean, -1) {
		idx, _ := strconv.Atoi(m[3])
		addDef(m[1], m[2], idx, "", "")
	}

	// Pattern 3: OBJECT-TYPE ... ::= { parent index }
	for _, m := range objectTypeRe.FindAllStringSubmatch(clean, -1) {
		name := m[1]
		body := m[2]
		idx, _ := strconv.Atoi(m[4])

		var syntax, maxAccess, seqOf string
		var indexNames []string
		if sm := syntaxRe.FindStringSubmatch(body); sm != nil {
			syntax = strings.TrimSpace(sm[1])
		}
		if am := accessRe.FindStringSubmatch(body); am != nil {
			maxAccess = am[1]
		}
		// Detect table objects: SYNTAX SEQUENCE OF X
		if sqm := sequenceOfRe.FindStringSubmatch(body); sqm != nil {
			seqOf = sqm[1]
		}
		// Detect entry objects: INDEX { col1, col2 }
		if im := indexRe.FindStringSubmatch(body); im != nil {
			parts := strings.Split(im[1], ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					indexNames = append(indexNames, p)
				}
			}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		defs = append(defs, rawDefinition{
			name: name, parent: m[3], index: idx,
			syntax: syntax, maxAccess: maxAccess,
			sequenceOf: seqOf, indexNames: indexNames,
		})
	}

	// Pattern 4: OBJECT-IDENTITY ... ::= { parent index }
	for _, m := range objectIdentityRe.FindAllStringSubmatch(clean, -1) {
		idx, _ := strconv.Atoi(m[3])
		addDef(m[1], m[2], idx, "", "")
	}

	// Pattern 5: NOTIFICATION-TYPE ... ::= { parent index }
	for _, m := range notificationTypeRe.FindAllStringSubmatch(clean, -1) {
		idx, _ := strconv.Atoi(m[3])
		addDef(m[1], m[2], idx, "", "")
	}

	return defs
}

// resolveOids builds a MibTree from raw definitions, using well-known roots as seeds.
func resolveOids(allDefs []rawDefinition) *MibTree {
	byOID := make(map[string]*MibNode)
	byName := make(map[string]string)

	// Seed with well-known roots
	for name, oid := range wellKnownRoots {
		byName[name] = oid
		byOID[oid] = &MibNode{Name: name, OID: oid}
	}

	// Iteratively resolve: if parent is resolved, resolve children
	resolved := make(map[string]bool)
	changed := true
	for changed {
		changed = false
		for _, def := range allDefs {
			if resolved[def.name] {
				continue
			}
			parentOID, ok := byName[def.parent]
			if !ok {
				continue
			}
			oid := fmt.Sprintf("%s.%d", parentOID, def.index)
			byName[def.name] = oid
			byOID[oid] = &MibNode{
				Name:       def.name,
				OID:        oid,
				Parent:     def.parent,
				Index:      def.index,
				Syntax:     def.syntax,
				MaxAccess:  def.maxAccess,
				SequenceOf: def.sequenceOf,
				IndexNames: def.indexNames,
			}
			resolved[def.name] = true
			changed = true
		}
	}

	// Build table info from resolved nodes
	tables := buildTableInfo(byOID, byName)

	return &MibTree{ByOID: byOID, ByName: byName, Tables: tables}
}

// buildTableInfo detects SNMP tables from resolved MIB nodes.
// A table is an object with SYNTAX "SEQUENCE OF X".
// Its entry child has INDEX { ... } and column children.
func buildTableInfo(byOID map[string]*MibNode, byName map[string]string) map[string]*TableInfo {
	tables := make(map[string]*TableInfo)

	// Find all table objects (those with SequenceOf set)
	for _, node := range byOID {
		if node.SequenceOf == "" {
			continue
		}
		// This is a table. Find its entry child (should be subId .1)
		entryOID := node.OID + ".1"
		entryNode, ok := byOID[entryOID]
		if !ok {
			// Try to find entry by looking for a child with INDEX
			for _, candidate := range byOID {
				if candidate.Parent == node.Name && len(candidate.IndexNames) > 0 {
					entryNode = candidate
					entryOID = candidate.OID
					break
				}
			}
			if entryNode == nil {
				continue
			}
		}

		// Collect columns: children of the entry node
		var columns []TableColumn
		for _, col := range byOID {
			if col.Parent != entryNode.Name {
				continue
			}
			columns = append(columns, TableColumn{
				Name:     col.Name,
				OID:      col.OID,
				SubId:    col.Index,
				Syntax:   col.Syntax,
				Datatype: snmpSyntaxToDatatype(col.Syntax),
			})
		}

		// Sort columns by sub-ID
		sortColumns(columns)

		tables[entryOID] = &TableInfo{
			TableName: node.Name,
			TableOID:  node.OID,
			EntryName: entryNode.Name,
			EntryOID:  entryOID,
			TypeName:  node.SequenceOf,
			Columns:   columns,
		}
	}

	return tables
}

// snmpSyntaxToDatatype maps MIB SYNTAX to tentacle datatype.
func snmpSyntaxToDatatype(syntax string) string {
	s := strings.ToLower(strings.TrimSpace(syntax))
	switch {
	case strings.Contains(s, "integer") || strings.Contains(s, "counter") ||
		strings.Contains(s, "gauge") || strings.Contains(s, "timetick") ||
		strings.Contains(s, "unsigned"):
		return "number"
	case strings.Contains(s, "truth"):
		return "boolean"
	default:
		return "string"
	}
}

// sortColumns sorts table columns by sub-ID.
func sortColumns(cols []TableColumn) {
	for i := 1; i < len(cols); i++ {
		for j := i; j > 0 && cols[j].SubId < cols[j-1].SubId; j-- {
			cols[j], cols[j-1] = cols[j-1], cols[j]
		}
	}
}

// LoadMibs loads and parses MIB files from the given paths.
func LoadMibs(paths []string) *MibTree {
	var allDefs []rawDefinition
	for _, path := range paths {
		// Skip directories
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("snmp: could not load MIB file", "path", path, "error", err)
			continue
		}
		defs := parseMibText(string(data))
		allDefs = append(allDefs, defs...)
	}
	return resolveOids(allDefs)
}

// LoadMibDir loads all MIB files from a directory.
// Picks up .mib, .txt, .my files and extensionless files (IETF standard).
func LoadMibDir(dirPath string) *MibTree {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		slog.Warn("snmp: could not read MIB directory", "path", dirPath, "error", err)
		return resolveOids(nil)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext == ".mib" || ext == ".txt" || ext == ".my" || ext == "" {
			paths = append(paths, filepath.Join(dirPath, name))
		}
	}
	return LoadMibs(paths)
}

// LoadMibDirs loads MIBs from multiple directories (colon-separated path string).
func LoadMibDirs(pathStr string) *MibTree {
	if pathStr == "" {
		return resolveOids(nil)
	}
	dirs := strings.Split(pathStr, ":")
	var allDefs []rawDefinition
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		tree := LoadMibDir(dir)
		for _, node := range tree.ByOID {
			if _, isRoot := wellKnownRoots[node.Name]; isRoot {
				continue
			}
			allDefs = append(allDefs, rawDefinition{
				name:       node.Name,
				parent:     node.Parent,
				index:      node.Index,
				syntax:     node.Syntax,
				maxAccess:  node.MaxAccess,
				sequenceOf: node.SequenceOf,
				indexNames: node.IndexNames,
			})
		}
	}
	return resolveOids(allDefs)
}

// ResolveOidName resolves a numeric OID to a human-readable name.
// Returns the resolved name, or a sanitized OID string if no match.
func (t *MibTree) ResolveOidName(oid string) string {
	if t == nil {
		return sanitizeOidToName(oid)
	}

	// Normalize: ensure leading dot
	if !strings.HasPrefix(oid, ".") {
		oid = "." + oid
	}

	// Exact match
	if node, ok := t.ByOID[oid]; ok {
		return node.Name
	}

	// Longest prefix match
	parts := strings.Split(oid, ".")
	for i := len(parts) - 1; i >= 2; i-- {
		prefix := strings.Join(parts[:i], ".")
		if node, ok := t.ByOID[prefix]; ok {
			suffix := strings.Join(parts[i:], ".")
			return node.Name + "." + suffix
		}
	}

	return sanitizeOidToName(oid)
}

// sanitizeOidToName converts an OID to a safe name when no MIB match is found.
func sanitizeOidToName(oid string) string {
	s := strings.TrimPrefix(oid, ".")
	return "oid_" + strings.ReplaceAll(s, ".", "_")
}

// SanitizeNameForKey converts a resolved name to a safe key (dots/hyphens -> underscores).
func SanitizeNameForKey(name string) string {
	r := strings.NewReplacer(".", "_", "-", "_")
	return r.Replace(name)
}

// EmptyMibTree returns a MibTree with only well-known roots.
func EmptyMibTree() *MibTree {
	return resolveOids(nil)
}
