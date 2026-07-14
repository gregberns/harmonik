//go:build specaudit

package specaudit_test

// AR-027 binding test — four-surface byte-identity of the `agent_type` field name.
//
// Spec ref: specs/architecture.md §4.7 AR-027.
//
// AR-027 states: "The following four surfaces MUST reference `agent_type` with
// the identifier shape of §4.7.AR-025, byte-for-byte identical: (i) YAML
// policies (freedom-profile and role-assignment fields in
// docs/foundation/components.md §6); (ii) DOT node attributes (as a routing
// hint, per docs/foundation/components.md §2); (iii) `LaunchSpec.agent_type`
// (per docs/foundation/components.md §4); (iv) event payloads naming an agent
// (e.g., `agent_started`, per docs/foundation/components.md §3). The canonical
// field name across all four surfaces is `agent_type`."
//
// The four surfaces map to these normative spec files:
//
//   - Surface (i)  YAML policies   → specs/control-points.md   (§6.3 policy expressions)
//   - Surface (ii) DOT attributes  → specs/execution-model.md  (§6.3 DOT node attributes)
//   - Surface (iii) LaunchSpec     → specs/handler-contract.md (§6.1 LaunchSpec RECORD)
//   - Surface (iv) event payloads  → specs/event-model.md      (§8 event taxonomy table)
//
// This sensor checks three things for each surface file:
//
//  1. canonical-present: the canonical field name `agent_type` appears at
//     least once in the normative body of the spec file (before the
//     ## 12. Revision history section).
//
//  2. no-agentType-synonym: the camelCase synonym `agentType` does not appear
//     as a backtick-quoted or record-field form in the normative body.
//
//  3. no-handler_type-synonym: the legacy synonym `handler_type` does not
//     appear as a backtick-quoted or record-field form in the normative body.
//     (Revision-history rows describe past migrations and are excluded by the
//     scan boundary at ## 12.)
//
// Note: the compound-adjective form "per-agent-type" (e.g., "per-agent-type
// adapter") is prose, not a field-name occurrence. The synonym detector targets
// backtick-quoted forms (`` `agent-type` ``) only; unhyphenated prose compound
// nouns do not trigger it.
//
// # Failure modes
//
//  1. canonical-missing     — `agent_type` not found in the normative body of
//                             one of the four surface files.
//  2. agentType-synonym     — camelCase form found as a backtick-quoted or
//                             record field in a surface file's normative body.
//  3. handler_type-synonym  — legacy form found as backtick-quoted or record
//                             field in a surface file's normative body.
//  4. agent-type-field-syn  — hyphenated backtick form `` `agent-type` ``
//                             found in a surface file's normative body.
//
// # Design note
//
// The sensor scans lines up to (but not including) "## 12. Revision history"
// so that migration changelog entries (AR-MIG-001 rows that cite the old
// `handler_type` name to document the rename) do not produce false positives.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ar027FixtureRepoRoot returns the absolute path to the repository root.
func ar027FixtureRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar027FixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar027_four_surfaces_test.go
	// repo root is two directories up
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// ar027Surface describes one of the four AR-027 surfaces.
type ar027Surface struct {
	// name is a human-readable label used in test failure messages.
	name string
	// specFile is the repo-relative path to the normative spec file.
	specFile string
}

// ar027FixtureSurfaces returns the four AR-027 surface descriptors.
func ar027FixtureSurfaces() []ar027Surface {
	return []ar027Surface{
		{
			name:     "surface-i (YAML policies — control-points.md §6.3)",
			specFile: filepath.Join("specs", "control-points.md"),
		},
		{
			name:     "surface-ii (DOT node attributes — execution-model.md §6.3)",
			specFile: filepath.Join("specs", "execution-model.md"),
		},
		{
			name:     "surface-iii (LaunchSpec.agent_type — handler-contract.md §6.1)",
			specFile: filepath.Join("specs", "handler-contract.md"),
		},
		{
			name:     "surface-iv (event payloads — event-model.md §8.3)",
			specFile: filepath.Join("specs", "event-model.md"),
		},
	}
}

// ar027ScanResult holds the findings from scanning one spec file.
type ar027ScanResult struct {
	// canonicalCount is the number of lines containing `agent_type` (as a
	// backtick-quoted field name or bare record field).
	canonicalCount int
	// synonymLines accumulates (lineNum, lineText) for every line that
	// contains a forbidden synonym.
	synonymLines []string
}

// ar027FixtureScanSpec scans the normative body of specPath (stopping before
// "## 12. Revision history") and returns an ar027ScanResult.
func ar027FixtureScanSpec(t *testing.T, specPath string) ar027ScanResult {
	t.Helper()

	//nolint:gosec // G304: path is constructed from ar027FixtureRepoRoot (repo-relative); not user input.
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("ar027FixtureScanSpec: open %s: %v", specPath, err)
	}
	defer func() { _ = f.Close() }()

	const revHistoryHeader = "## 12. Revision history"

	// Canonical field name patterns we accept as legitimate occurrences.
	// We match the bare word `agent_type` with a word-boundary heuristic:
	// the substring `agent_type` not immediately preceded or followed by
	// an alphanumeric or underscore character.
	isWordBoundary := func(r byte) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_')
	}
	countCanonical := func(line string) int {
		const target = "agent_type"
		count := 0
		for i := 0; i <= len(line)-len(target); i++ {
			if line[i:i+len(target)] == target {
				before := i == 0 || isWordBoundary(line[i-1])
				after := i+len(target) >= len(line) || isWordBoundary(line[i+len(target)])
				if before && after {
					count++
				}
			}
		}
		return count
	}

	// Synonym patterns: backtick-quoted form or bare record-field form.
	// We check for the exact strings wrapped in backticks or preceded by
	// whitespace (record field column).
	synonymPatterns := []string{
		"`agentType`",    // camelCase backtick-quoted
		"`handler_type`", // legacy name backtick-quoted
		"`agent-type`",   // hyphenated backtick-quoted
	}
	// Also catch bare record-field forms like:  agent-type  :  or  agentType  :
	// These are identified by "word  :" patterns in record definitions.
	bareFieldSynonyms := []struct {
		token string
		label string
	}{
		{"agentType", "camelCase synonym agentType"},
		{"handler_type", "legacy synonym handler_type"},
	}

	result := ar027ScanResult{}
	lineNum := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		// Stop scanning at the revision-history boundary.
		if strings.TrimSpace(line) == revHistoryHeader {
			break
		}

		// Count canonical occurrences.
		result.canonicalCount += countCanonical(line)

		// Check for backtick-quoted synonym patterns.
		for _, pat := range synonymPatterns {
			if strings.Contains(line, pat) {
				result.synonymLines = append(result.synonymLines,
					fmt.Sprintf("  line %d: %s", lineNum, strings.TrimSpace(line)))
			}
		}

		// Check for bare field synonyms (preceded by whitespace and followed
		// by whitespace or non-word character, to catch record definitions).
		for _, bfs := range bareFieldSynonyms {
			idx := strings.Index(line, bfs.token)
			if idx < 0 {
				continue
			}
			// Require word boundary on both sides.
			before := idx == 0 || isWordBoundary(line[idx-1])
			end := idx + len(bfs.token)
			after := end >= len(line) || isWordBoundary(line[end])
			if before && after {
				result.synonymLines = append(result.synonymLines,
					fmt.Sprintf("  line %d [%s]: %s", lineNum, bfs.label, strings.TrimSpace(line)))
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar027FixtureScanSpec: scan %s: %v", specPath, scanErr)
	}

	return result
}

// TestAR027FourSurfacesByteIdentity is the binding test for AR-027.
//
// It verifies that each of the four spec surfaces that must carry the
// `agent_type` field name:
//
//  1. Contains at least one occurrence of the canonical field name `agent_type`
//     in its normative body.
//
//  2. Contains no backtick-quoted or bare-record-field occurrences of the
//     forbidden synonyms `agentType`, `handler_type`, or `agent-type`.
//
// The test scans up to (but not including) "## 12. Revision history" in each
// spec file, so that migration changelog entries that mention the old
// `handler_type` name do not produce false positives.
func TestAR027FourSurfacesByteIdentity(t *testing.T) {
	repoRoot := ar027FixtureRepoRoot(t)
	surfaces := ar027FixtureSurfaces()

	for _, surf := range surfaces {
		surf := surf // capture loop variable for subtests
		t.Run(surf.name, func(t *testing.T) {
			specPath := filepath.Join(repoRoot, surf.specFile)
			result := ar027FixtureScanSpec(t, specPath)

			// Check 1: canonical field name must be present.
			if result.canonicalCount == 0 {
				t.Errorf(
					"AR-027 canonical-missing: no `agent_type` field-name occurrence found "+
						"in the normative body of %s\n"+
						"Fix: ensure the spec declares `agent_type` as the field name on "+
						"this surface (before the ## 12. Revision history section).",
					surf.specFile,
				)
			} else {
				t.Logf("AR-027 canonical-present: %d occurrence(s) of `agent_type` in %s",
					result.canonicalCount, surf.specFile)
			}

			// Check 2: no synonym occurrences.
			if len(result.synonymLines) > 0 {
				t.Errorf(
					"AR-027 synonym-found: forbidden synonym (agentType / handler_type / `agent-type`) "+
						"found in normative body of %s — field name MUST be `agent_type` byte-for-byte:\n%s\n"+
						"Fix: replace the synonym with the canonical field name `agent_type`.",
					surf.specFile,
					strings.Join(result.synonymLines, "\n"),
				)
			} else {
				t.Logf("AR-027 no-synonym: no forbidden synonym in normative body of %s", surf.specFile)
			}
		})
	}
}
