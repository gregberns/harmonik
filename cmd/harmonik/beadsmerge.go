package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type beadRow struct {
	// id is the bead ID extracted from the raw map for merge-key purposes.
	id string
	// updatedAt is the parsed updated_at timestamp for LWW collision resolution.
	updatedAt time.Time
	// raw is the original JSON object; written back unchanged so we never reformat
	// fields we don't understand.
	raw map[string]json.RawMessage
}

func beadsMergeUsage() {
	fmt.Print(`harmonik beads-merge — custom git merge-driver for .beads/issues.jsonl

USAGE
  harmonik beads-merge %O %A %B %P

ARGUMENTS
  %O  git merge ancestor (base) temp file path
  %A  git merge current-branch temp file path (also the output path)
  %B  git merge other-branch temp file path
  %P  working-tree path (used for conflict-log location)

NOTES
  This command is invoked automatically by git when merging .beads/issues.jsonl
  via the merge driver registered in .git/config and .gitattributes.
  It implements union-by-bead-ID with last-writer-wins on updated_at.
  Labels and dependencies are union-merged (monotonic-additive).
  Unresolvable conflicts are appended to .beads/merge-conflicts.log.

EXIT CODES
  0  Merge succeeded; %A contains the merged result.
  1  Argument or file-parse error.

EXAMPLES
  # Registered automatically; direct invocation for testing:
  harmonik beads-merge /tmp/git-merge-base /tmp/git-merge-a /tmp/git-merge-b .beads/issues.jsonl
`)
}

func runBeadsMergeSubcommand(subArgs []string) int {
	for _, arg := range subArgs {
		if arg == "--help" || arg == "-h" {
			beadsMergeUsage()
			return 0
		}
	}

	if len(subArgs) != 4 {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: expected 4 arguments (%%O %%A %%B %%P), got %d\n", len(subArgs))
		beadsMergeUsage()
		return 1
	}

	ancestorPath := subArgs[0] // %O
	currentPath := subArgs[1]  // %A (also the output file)
	otherPath := subArgs[2]    // %B
	workingPath := subArgs[3]  // %P (working-tree path for conflict log)

	ancestorRows, err := parseBeadsJSONL(ancestorPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse ancestor (%s): %v\n", ancestorPath, err)
		return 1
	}
	currentRows, err := parseBeadsJSONL(currentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse current (%s): %v\n", currentPath, err)
		return 1
	}
	otherRows, err := parseBeadsJSONL(otherPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse other (%s): %v\n", otherPath, err)
		return 1
	}

	merged, conflicts := mergeBeadRows(ancestorRows, currentRows, otherRows)

	if len(conflicts) > 0 {
		if logErr := appendConflictLog(workingPath, conflicts); logErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik beads-merge: cannot write conflict log: %v\n", logErr)
		}
	}

	if writeErr := writeBeadsJSONL(currentPath, merged); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: write merged result: %v\n", writeErr)
		return 1
	}

	return 0
}

// parseBeadsJSONL reads a .beads/issues.jsonl file and returns an ordered slice
// of beadRows. Lines that cannot be parsed as JSON objects are silently skipped
// (forward-compat: partial writes during crash recovery).
//
//nolint:gosec // G304: caller adds nolint at call sites; this func is package-internal
func parseBeadsJSONL(path string) ([]beadRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik beads-merge: close %s: %v\n", path, closeErr)
		}
	}()

	var rows []beadRow
	scanner := bufio.NewScanner(f)
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(line), &raw); jsonErr != nil {
			continue
		}
		id := extractStringField(raw, "id")
		if id == "" {
			continue
		}
		updatedAt := extractTimeField(raw, "updated_at")
		rows = append(rows, beadRow{id: id, updatedAt: updatedAt, raw: raw})
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, scanErr
	}
	return rows, nil
}

func mergeBeadRows(ancestor, current, other []beadRow) (merged []beadRow, conflicts []conflictRecord) {
	ancestorMap := rowsToMap(ancestor)
	currentMap := rowsToMap(current)
	otherMap := rowsToMap(other)

	allIDs := make(map[string]struct{})
	for _, r := range ancestor {
		allIDs[r.id] = struct{}{}
	}
	for _, r := range current {
		allIDs[r.id] = struct{}{}
	}
	for _, r := range other {
		allIDs[r.id] = struct{}{}
	}

	sortedIDs := make([]string, 0, len(allIDs))
	for id := range allIDs {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)

	for _, id := range sortedIDs {
		aRow, hasA := ancestorMap[id]
		cRow, hasC := currentMap[id]
		oRow, hasO := otherMap[id]

		var winner beadRow
		switch {
		case hasC && hasO:
			if cRow.updatedAt.Equal(oRow.updatedAt) {
				if !rowsEqual(cRow, oRow) {
					conflicts = append(conflicts, conflictRecord{
						BeadID:  id,
						AStatus: extractStringField(cRow.raw, "status"),
						BStatus: extractStringField(oRow.raw, "status"),
					})
				}
				winner = cRow
			} else if oRow.updatedAt.After(cRow.updatedAt) {
				winner = oRow
			} else {
				winner = cRow
			}
			winner.raw = unionLabelsAndDeps(winner.raw, cRow.raw, oRow.raw, aRow)
		case hasC:
			winner = cRow
		case hasO:
			winner = oRow
		default:
			if hasA {
				winner = aRow
			}
			continue
		}
		merged = append(merged, winner)
	}
	return merged, conflicts
}

func rowsToMap(rows []beadRow) map[string]beadRow {
	m := make(map[string]beadRow, len(rows))
	for _, r := range rows {
		if existing, seen := m[r.id]; !seen || r.updatedAt.After(existing.updatedAt) {
			m[r.id] = r
		}
	}
	return m
}

func rowsEqual(a, b beadRow) bool {
	if len(a.raw) != len(b.raw) {
		return false
	}
	for k, av := range a.raw {
		bv, ok := b.raw[k]
		if !ok {
			return false
		}
		if string(av) != string(bv) {
			return false
		}
	}
	return true
}

func unionLabelsAndDeps(winner, current, other map[string]json.RawMessage, ancestor beadRow) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(winner))
	for k, v := range winner {
		result[k] = v
	}

	if mergedLabels, ok := unionStringArray(
		extractStringArray(current, "labels"),
		extractStringArray(other, "labels"),
		extractStringArray(ancestor.raw, "labels"),
	); ok {
		if labelsJSON, err := json.Marshal(mergedLabels); err == nil {
			result["labels"] = labelsJSON
		}
	}

	if mergedDeps, ok := unionDependencies(
		extractRawArray(current, "dependencies"),
		extractRawArray(other, "dependencies"),
		extractRawArray(ancestor.raw, "dependencies"),
	); ok {
		if depsJSON, err := json.Marshal(mergedDeps); err == nil {
			result["dependencies"] = depsJSON
		}
	}

	return result
}

func unionStringArray(a, b, c []string) ([]string, bool) {
	if a == nil && b == nil && c == nil {
		return nil, false
	}
	seen := make(map[string]struct{})
	var result []string
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range c {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result, true
}

func unionDependencies(a, b, c []json.RawMessage) ([]json.RawMessage, bool) {
	if a == nil && b == nil && c == nil {
		return nil, false
	}
	seen := make(map[string]json.RawMessage)
	var order []string

	addDeps := func(deps []json.RawMessage) {
		for _, raw := range deps {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			key := extractStringField(m, "depends_on_id")
			if key == "" {
				key = extractStringField(m, "issue_id") + ":" + extractStringField(m, "depends_on_id")
			}
			if key == "" || key == ":" {
				continue
			}
			if _, exists := seen[key]; !exists {
				seen[key] = raw
				order = append(order, key)
			}
		}
	}
	addDeps(a)
	addDeps(b)
	addDeps(c)

	if len(order) == 0 {
		return []json.RawMessage{}, true
	}
	sort.Strings(order)
	result := make([]json.RawMessage, 0, len(order))
	for _, key := range order {
		result = append(result, seen[key])
	}
	return result, true
}

type conflictRecord struct {
	BeadID  string
	AStatus string // status value on the current/ours (A) side
	BStatus string // status value on the other (B) side
}

func appendConflictLog(workingPath string, conflicts []conflictRecord) (err error) {
	dir := filepath.Dir(workingPath)
	logPath := filepath.Join(dir, "merge-conflicts.log")
	//nolint:gosec // G304: derived from git-provided working-tree path; G302: append-only log
	f, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openErr != nil {
		return openErr
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close conflict log %s: %w", logPath, closeErr)
		}
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range conflicts {
		if _, writeErr := fmt.Fprintf(f, "%s CONFLICT bead=%s field=status a=%s b=%s resolution=took-ours\n",
			now, c.BeadID, c.AStatus, c.BStatus,
		); writeErr != nil {
			return fmt.Errorf("append conflict log %s: %w", logPath, writeErr)
		}
	}
	return nil
}

func writeBeadsJSONL(path string, rows []beadRow) (err error) {
	//nolint:gosec // G304: path provided by git merge driver invocation
	f, openErr := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o644)
	if openErr != nil {
		return openErr
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, closeErr)
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if encErr := enc.Encode(r.raw); encErr != nil {
			return encErr
		}
	}
	return nil
}

func extractStringField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func extractTimeField(m map[string]json.RawMessage, key string) time.Time {
	s := extractStringField(m, key)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func extractStringArray(m map[string]json.RawMessage, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}

func extractRawArray(m map[string]json.RawMessage, key string) []json.RawMessage {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}
