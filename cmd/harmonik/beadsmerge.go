package main

// beadsmerge.go — `harmonik beads-merge` subcommand implementation.
//
// # Purpose (hk-jon6r)
//
// Custom git merge-driver for .beads/issues.jsonl. Replaces the lossy
// `git checkout --theirs .beads/issues.jsonl` workaround documented in
// HANDOFF.md. The driver implements a union-by-bead-ID merge with
// last-writer-wins (LWW) collision resolution on the updated_at timestamp.
//
// # Algorithm
//
//  1. Parse %O/%A/%B as map[bead_id]beadRow.
//  2. Union on bead_id: any bead present in any ancestor is included.
//  3. On collision (same bead_id in multiple ancestors), pick the row with
//     the larger updated_at timestamp (LWW).
//  4. Labels and dependencies: monotonic-additive union (never removed).
//  5. Write sorted result to %P (the path of the file in the working tree).
//  6. Cases NOT covered (acceptable for quick win):
//     - Semantic conflicts on same field within seconds → logged to
//       .beads/merge-conflicts.log for operator audit.
//     - Bead deletion → out-of-band (whole-row LWW fallback).
//     - Schema migrations → whole-row LWW fallback.
//
// # Registration
//
// Register via:
//
//	.gitattributes:   .beads/issues.jsonl  merge=beads-union
//	.git/config:      [merge "beads-union"]
//	                    name = Bead Ledger Union Merge
//	                    driver = harmonik beads-merge %O %A %B %P
//
// Post-merge, run `br sync --import-only` to refresh the SQLite ledger from
// the newly merged JSONL.
//
// Spec ref: bead hk-jon6r; internal/daemon/workloop.go:1891-2036 (mergeRunBranchToMain).
// Bead ref: hk-jon6r.

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

// beadRow is a single line from .beads/issues.jsonl decoded as a loose map so
// unknown fields are preserved verbatim (forward-compatible with schema changes).
type beadRow struct {
	// id is the bead ID extracted from the raw map for merge-key purposes.
	id string
	// updatedAt is the parsed updated_at timestamp for LWW collision resolution.
	updatedAt time.Time
	// raw is the original JSON object; written back unchanged so we never reformat
	// fields we don't understand.
	raw map[string]json.RawMessage
}

// beadsMergeUsage prints the help text for `harmonik beads-merge`.
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

// runBeadsMergeSubcommand implements `harmonik beads-merge %O %A %B %P`.
// subArgs is os.Args[2:] (everything after "beads-merge").
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

	//nolint:gosec // G304: paths provided by git invocation via registered merge driver
	ancestorRows, err := parseBeadsJSONL(ancestorPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse ancestor (%s): %v\n", ancestorPath, err)
		return 1
	}
	//nolint:gosec // G304: paths provided by git invocation via registered merge driver
	currentRows, err := parseBeadsJSONL(currentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse current (%s): %v\n", currentPath, err)
		return 1
	}
	//nolint:gosec // G304: paths provided by git invocation via registered merge driver
	otherRows, err := parseBeadsJSONL(otherPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-merge: parse other (%s): %v\n", otherPath, err)
		return 1
	}

	merged, conflicts := mergeBeadRows(ancestorRows, currentRows, otherRows)

	// Write conflicts to .beads/merge-conflicts.log for operator audit.
	if len(conflicts) > 0 {
		if logErr := appendConflictLog(workingPath, conflicts); logErr != nil {
			// Non-fatal: merge still succeeds; log the warning.
			fmt.Fprintf(os.Stderr, "harmonik beads-merge: cannot write conflict log: %v\n", logErr)
		}
	}

	// Write merged result to %A (current-branch file, which git uses as output).
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
		// An empty or missing ancestor file is valid (e.g., first merge on a new repo).
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var rows []beadRow
	scanner := bufio.NewScanner(f)
	// Set a generous buffer: some bead descriptions are long.
	setLargeScanBuffer(scanner)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(line), &raw); jsonErr != nil {
			// Skip malformed lines (forward-compat).
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

// mergeBeadRows implements the union-by-bead-ID merge algorithm.
//
// Algorithm:
//  1. Build a map from each input set keyed by bead_id.
//  2. Union all bead_ids seen across ancestor, current, and other.
//  3. For each bead_id: pick the row with the largest updated_at (LWW).
//     - If current and other have the same updated_at and differ: record conflict.
//  4. Labels and dependencies: union-merge (monotonic-additive across all three).
//  5. Return rows sorted by id (deterministic output).
func mergeBeadRows(ancestor, current, other []beadRow) (merged []beadRow, conflicts []conflictRecord) {
	ancestorMap := rowsToMap(ancestor)
	currentMap := rowsToMap(current)
	otherMap := rowsToMap(other)

	// Collect all bead_ids.
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
			// Both sides have the bead: pick LWW.
			if cRow.updatedAt.Equal(oRow.updatedAt) {
				// Same timestamp: check if rows are actually identical.
				if !rowsEqual(cRow, oRow) {
					conflicts = append(conflicts, conflictRecord{
						BeadID:  id,
						AStatus: extractStringField(cRow.raw, "status"),
						BStatus: extractStringField(oRow.raw, "status"),
					})
				}
				// Use current as tiebreaker (no data loss; conflict logged above).
				winner = cRow
			} else if oRow.updatedAt.After(cRow.updatedAt) {
				winner = oRow
			} else {
				winner = cRow
			}
			// Union labels and dependencies monotonically.
			winner.raw = unionLabelsAndDeps(winner.raw, cRow.raw, oRow.raw, aRow)
		case hasC:
			winner = cRow
		case hasO:
			winner = oRow
		default:
			// Only in ancestor (deleted on both sides — include from ancestor as
			// the safest fallback; bead deletion is out-of-band per spec).
			if hasA {
				winner = aRow
			}
			continue
		}
		merged = append(merged, winner)
	}
	return merged, conflicts
}

// rowsToMap converts a slice of beadRow to a map keyed by id.
// When the same id appears more than once in the input (within-file duplicates),
// the row with the newest updated_at is kept (LWW by timestamp).
func rowsToMap(rows []beadRow) map[string]beadRow {
	m := make(map[string]beadRow, len(rows))
	for _, r := range rows {
		if existing, seen := m[r.id]; !seen || r.updatedAt.After(existing.updatedAt) {
			m[r.id] = r
		}
	}
	return m
}

// rowsEqual returns true if two beadRows have identical raw JSON content.
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

// unionLabelsAndDeps merges the "labels" and "dependencies" fields from
// current and other into the winner's raw map. Both fields are monotonic-additive:
// any value present in current, other, or ancestor is preserved; none are removed.
func unionLabelsAndDeps(winner, current, other map[string]json.RawMessage, ancestor beadRow) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(winner))
	for k, v := range winner {
		result[k] = v
	}

	// Union labels.
	if mergedLabels, ok := unionStringArray(
		extractStringArray(current, "labels"),
		extractStringArray(other, "labels"),
		extractStringArray(ancestor.raw, "labels"),
	); ok {
		if labelsJSON, err := json.Marshal(mergedLabels); err == nil {
			result["labels"] = labelsJSON
		}
	}

	// Union dependencies (array of objects; union by depends_on_id).
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

// unionStringArray returns the union of up to three string slices. Returns
// (nil, false) when all inputs are nil (field absent in all ancestors).
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

// unionDependencies merges dependency arrays by depends_on_id key.
// Returns (nil, false) when all inputs are nil (field absent in all ancestors).
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
				// Fall back to issue_id+depends_on_id composite.
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

// conflictRecord captures a LWW collision that could not be deterministically
// resolved (same updated_at, different content).
type conflictRecord struct {
	BeadID  string
	AStatus string // status value on the current/ours (A) side
	BStatus string // status value on the other (B) side
}

// appendConflictLog appends conflict records to .beads/merge-conflicts.log.
// The log path is derived from the working-tree path of issues.jsonl.
// Format: <iso8601-timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value> resolution=took-ours
func appendConflictLog(workingPath string, conflicts []conflictRecord) error {
	dir := filepath.Dir(workingPath)
	logPath := filepath.Join(dir, "merge-conflicts.log")
	//nolint:gosec // G304: derived from git-provided working-tree path; G302: append-only log
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range conflicts {
		fmt.Fprintf(f, "%s CONFLICT bead=%s field=status a=%s b=%s resolution=took-ours\n",
			now, c.BeadID, c.AStatus, c.BStatus,
		)
	}
	return nil
}

// writeBeadsJSONL writes rows to path as JSONL (one JSON object per line).
func writeBeadsJSONL(path string, rows []beadRow) error {
	//nolint:gosec // G304: path provided by git merge driver invocation
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range rows {
		if encErr := enc.Encode(r.raw); encErr != nil {
			return encErr
		}
	}
	return nil
}

// extractStringField returns the string value of a JSON field, or "" if absent
// or not a JSON string.
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

// extractTimeField parses a time.Time from a JSON string field (RFC3339).
// Returns zero value if absent or unparseable.
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

// extractStringArray unmarshals a JSON string array field from a raw map.
// Returns nil if the field is absent.
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

// extractRawArray returns the raw JSON messages from an array field.
// Returns nil if the field is absent.
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
