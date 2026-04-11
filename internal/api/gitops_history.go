//go:build api || all

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joyautomation/tentacle/internal/manifest"
)

// ─── Types ─────────────────────────────────────────────────────────────────

// CommitEntry represents a single git log entry.
type CommitEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// HistoryDiffResult is the structured diff between two commits.
type HistoryDiffResult struct {
	FromSHA string              `json:"fromSha"`
	ToSHA   string              `json:"toSha"`
	Changes []HistoryDiffChange `json:"changes"`
	Summary DiffSummary         `json:"summary"`
}

// HistoryDiffChange describes a single resource difference between commits.
type HistoryDiffChange struct {
	Kind   string      `json:"kind"`
	Name   string      `json:"name"`
	Action string      `json:"action"` // "added", "removed", "modified", "unchanged"
	Fields []FieldDiff `json:"fields,omitempty"`
}

// FieldDiff describes a single field-level change within a resource.
type FieldDiff struct {
	Path     string `json:"path"`
	Action   string `json:"action"` // "added", "removed", "modified"
	OldValue any    `json:"oldValue,omitempty"`
	NewValue any    `json:"newValue,omitempty"`
}

// DiffSummary provides quick counts for the visualization header.
type DiffSummary struct {
	Added     int `json:"added"`
	Modified  int `json:"modified"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
}

// ─── Git CLI Helpers ───────────────────────────────────────────────────────

var validSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

func (m *Module) execGitops(args ...string) (string, error) {
	repoDir := m.getGitopsRepoDir()
	keyPath := m.getGitopsSSHKeyPath()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir

	if keyPath != "" {
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -i %s -o StrictHostKeyChecking=accept-new", keyPath),
		)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (m *Module) getGitopsRepoDir() string {
	dataDir := os.Getenv("TENTACLE_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/tentacle"
	}
	return filepath.Join(dataDir, "gitops", "repo")
}

func (m *Module) getGitopsConfigPath() string {
	if data, _, err := m.bus.KVGet("tentacle_config", "gitops.GITOPS_PATH"); err == nil && len(data) > 0 {
		return string(data)
	}
	return "config"
}

// ─── Core Logic ────────────────────────────────────────────────────────────

func (m *Module) gitLog(limit int) ([]CommitEntry, error) {
	out, err := m.execGitops("log",
		fmt.Sprintf("--max-count=%d", limit),
		"--format=%H%x00%an%x00%aI%x00%s",
	)
	if err != nil {
		return nil, err
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return []CommitEntry{}, nil
	}

	var entries []CommitEntry
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		entries = append(entries, CommitEntry{
			SHA:     parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Message: parts[3],
		})
	}
	return entries, nil
}

func (m *Module) resourcesAtCommit(sha string) ([]any, error) {
	configPath := m.getGitopsConfigPath()

	// List all files under config path at this commit.
	out, err := m.execGitops("ls-tree", "-r", "--name-only", sha, configPath)
	if err != nil {
		return nil, fmt.Errorf("ls-tree: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return []any{}, nil
	}

	var allResources []any
	for _, filePath := range strings.Split(out, "\n") {
		filePath = strings.TrimSpace(filePath)
		if !strings.HasSuffix(filePath, ".yaml") && !strings.HasSuffix(filePath, ".yml") {
			continue
		}

		content, err := m.execGitops("show", sha+":"+filePath)
		if err != nil {
			continue // skip files that can't be read
		}

		resources, err := manifest.ParseBytes([]byte(content))
		if err != nil {
			continue // skip unparseable files
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

func diffResources(fromResources, toResources []any) *HistoryDiffResult {
	fromMap := buildResourceMap(fromResources)
	toMap := buildResourceMap(toResources)

	result := &HistoryDiffResult{}

	// Check resources in "to" — added or modified.
	for key, toRes := range toMap {
		kind, name := splitResourceKey(key)
		fromRes, exists := fromMap[key]
		if !exists {
			result.Changes = append(result.Changes, HistoryDiffChange{
				Kind:   kind,
				Name:   name,
				Action: "added",
			})
			result.Summary.Added++
			continue
		}

		fields := deepFieldDiff(specOf(fromRes), specOf(toRes), "")
		if len(fields) > 0 {
			result.Changes = append(result.Changes, HistoryDiffChange{
				Kind:   kind,
				Name:   name,
				Action: "modified",
				Fields: fields,
			})
			result.Summary.Modified++
		} else {
			result.Changes = append(result.Changes, HistoryDiffChange{
				Kind:   kind,
				Name:   name,
				Action: "unchanged",
			})
			result.Summary.Unchanged++
		}
	}

	// Check resources only in "from" — removed.
	for key := range fromMap {
		if _, exists := toMap[key]; !exists {
			kind, name := splitResourceKey(key)
			result.Changes = append(result.Changes, HistoryDiffChange{
				Kind:   kind,
				Name:   name,
				Action: "removed",
			})
			result.Summary.Removed++
		}
	}

	// Sort changes for deterministic output: by kind then name.
	sort.Slice(result.Changes, func(i, j int) bool {
		if result.Changes[i].Kind != result.Changes[j].Kind {
			return result.Changes[i].Kind < result.Changes[j].Kind
		}
		return result.Changes[i].Name < result.Changes[j].Name
	})

	return result
}

// ─── Diff Helpers ──────────────────────────────────────────────────────────

func buildResourceMap(resources []any) map[string]any {
	m := make(map[string]any)
	for _, res := range resources {
		key := resourceKey(res)
		if key != "" {
			m[key] = res
		}
	}
	return m
}

func resourceKey(res any) string {
	kind := resourceKind(res)
	name := resourceName(res)
	if kind == "" || name == "" {
		return ""
	}
	return kind + "/" + name
}

func splitResourceKey(key string) (kind, name string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return key, ""
}

func resourceKind(res any) string {
	switch r := res.(type) {
	case *manifest.GatewayResource:
		return r.Kind
	case *manifest.ServiceResource:
		return r.Kind
	case *manifest.ModuleConfigResource:
		return r.Kind
	case *manifest.NftablesResource:
		return r.Kind
	case *manifest.NetworkResource:
		return r.Kind
	default:
		return ""
	}
}

func resourceName(res any) string {
	switch r := res.(type) {
	case *manifest.GatewayResource:
		return r.Metadata.Name
	case *manifest.ServiceResource:
		return r.Metadata.Name
	case *manifest.ModuleConfigResource:
		return r.Metadata.Name
	case *manifest.NftablesResource:
		return r.Metadata.Name
	case *manifest.NetworkResource:
		return r.Metadata.Name
	default:
		return ""
	}
}

func specOf(res any) any {
	switch r := res.(type) {
	case *manifest.GatewayResource:
		return r.Spec
	case *manifest.ServiceResource:
		return r.Spec
	case *manifest.ModuleConfigResource:
		return r.Spec
	case *manifest.NftablesResource:
		return r.Spec
	case *manifest.NetworkResource:
		return r.Spec
	default:
		return nil
	}
}

// deepFieldDiff recursively compares two values and produces field-level diffs.
func deepFieldDiff(a, b any, prefix string) []FieldDiff {
	// Marshal both to JSON maps for comparison.
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)

	// Quick check: if identical, no diffs.
	if string(aJSON) == string(bJSON) {
		return nil
	}

	var aMap, bMap map[string]any
	isAMap := json.Unmarshal(aJSON, &aMap) == nil && aMap != nil
	isBMap := json.Unmarshal(bJSON, &bMap) == nil && bMap != nil

	if isAMap && isBMap {
		return diffMaps(aMap, bMap, prefix)
	}

	// Not both maps — treat as a leaf change.
	path := prefix
	if path == "" {
		path = "value"
	}
	return []FieldDiff{{
		Path:     path,
		Action:   "modified",
		OldValue: simplifyValue(a),
		NewValue: simplifyValue(b),
	}}
}

func diffMaps(a, b map[string]any, prefix string) []FieldDiff {
	var diffs []FieldDiff

	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		aVal, aOK := a[k]
		bVal, bOK := b[k]

		if !aOK {
			diffs = append(diffs, FieldDiff{
				Path:     path,
				Action:   "added",
				NewValue: simplifyValue(bVal),
			})
			continue
		}
		if !bOK {
			diffs = append(diffs, FieldDiff{
				Path:     path,
				Action:   "removed",
				OldValue: simplifyValue(aVal),
			})
			continue
		}

		// Both exist — check if they're sub-maps to recurse.
		aChild, aIsMap := aVal.(map[string]any)
		bChild, bIsMap := bVal.(map[string]any)

		if aIsMap && bIsMap {
			diffs = append(diffs, diffMaps(aChild, bChild, path)...)
			continue
		}

		// Leaf comparison via JSON.
		aJSON, _ := json.Marshal(aVal)
		bJSON, _ := json.Marshal(bVal)
		if string(aJSON) != string(bJSON) {
			diffs = append(diffs, FieldDiff{
				Path:     path,
				Action:   "modified",
				OldValue: simplifyValue(aVal),
				NewValue: simplifyValue(bVal),
			})
		}
	}

	return diffs
}

// simplifyValue converts complex nested values to a concise string representation.
func simplifyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		if len(val) <= 3 {
			return val
		}
		// For large maps, return key count.
		return fmt.Sprintf("{%d keys}", len(val))
	case []any:
		if len(val) <= 3 {
			return val
		}
		return fmt.Sprintf("[%d items]", len(val))
	default:
		return v
	}
}

// ─── Handlers ──────────────────────────────────────────────────────────────

// handleGetGitopsHistory returns recent commit entries from the gitops repo.
// GET /api/v1/gitops/history?limit=50
func (m *Module) handleGetGitopsHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	entries, err := m.gitLog(limit)
	if err != nil {
		writeJSON(w, http.StatusOK, []CommitEntry{})
		return
	}

	writeJSON(w, http.StatusOK, entries)
}

// handleGetGitopsHistoryDiff returns a structured diff between two commits.
// GET /api/v1/gitops/history/diff?from=SHA&to=SHA
func (m *Module) handleGetGitopsHistoryDiff(w http.ResponseWriter, r *http.Request) {
	fromSHA := r.URL.Query().Get("from")
	toSHA := r.URL.Query().Get("to")

	if fromSHA == "" || toSHA == "" {
		writeError(w, http.StatusBadRequest, "both 'from' and 'to' query parameters are required")
		return
	}

	if !validSHARegex.MatchString(fromSHA) || !validSHARegex.MatchString(toSHA) {
		writeError(w, http.StatusBadRequest, "invalid SHA format")
		return
	}

	fromResources, err := m.resourcesAtCommit(fromSHA)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resources at 'from' commit: "+err.Error())
		return
	}

	toResources, err := m.resourcesAtCommit(toSHA)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resources at 'to' commit: "+err.Error())
		return
	}

	result := diffResources(fromResources, toResources)
	result.FromSHA = fromSHA
	result.ToSHA = toSHA

	writeJSON(w, http.StatusOK, result)
}
