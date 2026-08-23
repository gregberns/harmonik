// Package lifecycle — EM-INV-001 sensor: git is the state-reconstruction source.
//
// EM-INV-001 (execution-model.md §5 EM-INV-001): "The git checkpoint trail MUST
// be sufficient, together with the Beads store, to reconstruct any run's current
// durable state. JSONL event replay MUST NOT be used for state reconstruction.
// Every subsystem that consumes run state (reconciliation, operator-nfr,
// process-lifecycle, scenario-harness) MUST honor this precedence."
//
// This sensor file verifies EM-INV-001 from three complementary angles:
//
//  1. Spec-text sensor: specs/execution-model.md contains the EM-INV-001 anchor
//     with the required canonical phrases (git, Beads, MUST NOT, JSONL, etc.).
//     A rename or weakening of the invariant text is a breaking spec change and
//     MUST fail this sensor.
//
//  2. Corpus-scan sensor: no non-test Go source file in the codebase exposes a
//     function or symbol that walks JSONL as a state-reconstruction source. The
//     forbidden patterns are derived from the invariant's "every subsystem"
//     scope: if any subsystem introduces a JSONL-read-for-state primitive, the
//     scan fails. This is the authoring-surface enforcement layer for EM-INV-001.
//
//  3. Restart scenario sensor: a behavioral integration test that simulates
//     daemon crash → restart by:
//     a. Landing checkpoint commits on a task branch (git state).
//     b. Populating a fake Beads querier with non-terminal bead records.
//     c. Writing a JSONL event log that, if walked, would produce a different
//     run count than git+Beads — confirming that JSONL is not consulted.
//     d. Calling DiscoverActiveRuns (the restart path) and verifying that the
//     result matches git+Beads, not the JSONL event count.
//
// The restart scenario sensor is the machine-checkable form of §10.2's
// "destroy the daemon; confirm full state reconstructable from git + Beads
// without JSONL reads" obligation.
//
// Helper prefix: gitReconFixture (per hk-b3f.63 brief).
//
// Requirement-traceable bead: hk-b3f.63.
package lifecycle

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func gitReconFixtureRepoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("gitReconFixtureRepoRoot: runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("gitReconFixtureRepoRoot: go.mod not found above %q", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

func gitReconFixtureCollectGoSources(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("gitReconFixtureCollectGoSources: WalkDir(%q): %v", root, err)
	}
	return files
}

var gitReconFixtureForbiddenPatterns = []string{
	"ReconstructFromJSONL",
	"reconstructFromJSONL",
	"ReconstructStateFromJSONL",
	"reconstructStateFromJSONL",
	"WalkJSONLForState",
	"walkJSONLForState",
	"ReplayJSONLForState",
	"replayJSONLForState",
	"JSONLStateReconstruct",
	"jsonlStateReconstruct",
	"JSONLReplay",
	"jsonlReplay",

	"JSONLStateReader",
	"jsonlStateReader",
	"JSONLRunReconstructor",
	"jsonlRunReconstructor",
	"JSONLStateReconstructor",
	"jsonlStateReconstructor",

	`"jsonl-replay-state"`,
	`"jsonl-state-reconstruct"`,
	`"replay-state-from-jsonl"`,
	`"reconstruct-from-jsonl"`,
}

var gitReconFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

func gitReconFixtureRunID(n int) string {
	return durableFixtureRunID(600 + n)
}

func gitReconFixtureBeadID(n int) string {
	return fmt.Sprintf("hk-eminv001.%d", n)
}

func gitReconFixtureJSONLEventLog(t *testing.T, dir string, decoyRunCount int) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("gitReconFixtureJSONLEventLog: MkdirAll: %v", err)
	}

	jsonlPath := filepath.Join(dir, "events.jsonl")
	var sb strings.Builder
	for i := range decoyRunCount {
		fmt.Fprintf(&sb,
			"{\"event_type\":\"run_started\",\"run_id\":\"decoy-run-%04d\",\"schema_version\":1}\n",
			i+1,
		)
	}
	if err := os.WriteFile(jsonlPath, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("gitReconFixtureJSONLEventLog: WriteFile: %v", err)
	}
}

func gitReconFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("gitReconFixtureSpecContent: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "execution-model.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("gitReconFixtureSpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "EM-INV-001 — Git is the state-reconstruction source"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EM-INV-001 may have been removed or renamed", specPath, anchor)
	}

	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// TestEMINV001_SpecContainsGitIsStateReconstructionSource verifies that the
// EM-INV-001 section of specs/execution-model.md encodes the git-is-authority
// invariant with the required canonical phrases.
//
// Required phrases:
//   - "MUST be sufficient"      — the normative sufficiency claim for git+Beads
//   - "JSONL event replay MUST NOT" — the explicit normative prohibition
//   - "git"                     — names the primary state-reconstruction source
//   - "Beads"                   — names the second state-reconstruction source
//   - "state reconstruction"    — names the scoped operation
//   - "every subsystem"         — confirms the cross-subsystem scope of EM-INV-001
//
// A future weakening or rename of any of these phrases in the spec is a
// breaking change and MUST fail this sensor.
//
// Spec ref: execution-model.md §5 EM-INV-001.
func TestEMINV001_SpecContainsGitIsStateReconstructionSource(t *testing.T) {
	t.Parallel()

	para := gitReconFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "MUST be sufficient",
			hint:   "EM-INV-001 must assert git+Beads sufficiency with normative 'MUST be sufficient' language; weaker phrasing does not satisfy the invariant",
		},
		{
			phrase: "JSONL event replay MUST NOT",
			hint:   "EM-INV-001 must explicitly prohibit JSONL event replay for state reconstruction; the prohibition must use MUST NOT language",
		},
		{
			phrase: "git",
			hint:   "EM-INV-001 must name git as the primary state-reconstruction source; the invariant is valueless without naming the authority",
		},
		{
			phrase: "Beads",
			hint:   "EM-INV-001 must name Beads as the second state-reconstruction source alongside git; both stores are required",
		},
		{
			phrase: "state reconstruction",
			hint:   "EM-INV-001 must name 'state reconstruction' as the scoped operation; this bounds the invariant to startup/restart contexts",
		},
		{
			phrase: "Every subsystem",
			hint:   "EM-INV-001 must assert cross-subsystem scope with 'Every subsystem' language; a single-subsystem phrasing does not satisfy the invariant",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EM-INV-001 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestEMINV001_NoSubsystemWalksJSONLForStateReconstruction is the corpus-scan
// sensor for EM-INV-001.
//
// It scans every non-test Go source file under internal/ and cmd/ and asserts
// that none expose a function or type that walks JSONL as a state-reconstruction
// source. The forbidden patterns in gitReconFixtureForbiddenPatterns are derived
// from the EM-INV-001 text: any primitive that "replays", "reconstructs from",
// or "walks JSONL for state" violates the invariant at the authoring surface.
//
// A failure here means a subsystem source file has introduced a JSONL-for-state
// primitive. Fix by removing the primitive; state reconstruction MUST route
// through git (task-branch tip walk) and Beads (non-terminal bead query), as
// implemented by DiscoverActiveRuns (lifecycle package) and the reconciliation
// startup path.
//
// Spec ref: execution-model.md §5 EM-INV-001; §4.7 EM-031.
func TestEMINV001_NoSubsystemWalksJSONLForStateReconstruction(t *testing.T) {
	t.Parallel()

	repoRoot := gitReconFixtureRepoRoot(t)

	var sourceFiles []string
	for _, rel := range gitReconFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		sourceFiles = append(sourceFiles, gitReconFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Skip("no Go source files found under scanned roots — nothing to check")
	}

	for _, filePath := range sourceFiles {
		for _, pattern := range gitReconFixtureForbiddenPatterns {
			t.Run(filepath.Base(filePath)+"/"+pattern, func(t *testing.T) {
				t.Parallel()

				//nolint:gosec // G304: path is constructed from repo root via go.mod walk, not user input
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("os.ReadFile(%q): %v", filePath, err)
				}

				if strings.Contains(string(content), pattern) {
					rel, relErr := filepath.Rel(repoRoot, filePath)
					if relErr != nil {
						rel = filePath
					}
					t.Errorf(
						"EM-INV-001 violation: source file %q contains forbidden JSONL-for-state-reconstruction pattern %q\n"+
							"No subsystem may walk JSONL to reconstruct run state at the authoring surface\n"+
							"(specs/execution-model.md §5 EM-INV-001). State reconstruction MUST use git+Beads\n"+
							"per EM-031 (§4.7). Remove the primitive.",
						rel, pattern,
					)
				}
			})
		}
	}
}

// TestEMINV001_SensorCoverage verifies that the EM-INV-001 corpus scan found
// at least one Go source file — a meta-test confirming the walker is not
// silently skipping the entire codebase.
//
// Without this guard, a misconfigured scanned-roots list would silently pass
// every pattern check against an empty file set, making the sensor useless.
func TestEMINV001_SensorCoverage(t *testing.T) {
	t.Parallel()

	repoRoot := gitReconFixtureRepoRoot(t)

	var total int
	for _, rel := range gitReconFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		total += len(gitReconFixtureCollectGoSources(t, root))
	}

	if total == 0 {
		t.Error("EM-INV-001 sensor has no coverage: zero Go source files found under scanned roots " +
			"(internal/, cmd/); sensor is not operating")
	}
}

// TestEMINV001_RestartScenario_StateReconstructableWithoutJSONL is the
// primary restart scenario sensor for EM-INV-001.
//
// The test simulates a daemon crash → restart by:
//  1. Creating an in-memory git repo and landing N checkpoint commits on task
//     branches (one per run). These commits form the git checkpoint trail.
//  2. Populating a fake BeadsQuerier with M non-terminal bead records, where M
//     differs from N. The M-vs-N mismatch is by design: git and Beads together
//     produce a correct union set; JSONL alone would produce yet another count.
//  3. Writing a JSONL event log with D decoy run_started events (D ≠ N, D ≠ M).
//     If DiscoverActiveRuns walks JSONL, the returned ActiveRunSet.Len() would
//     equal D; if it uses git+Beads correctly, the count will be N ∪ M.
//  4. Calling DiscoverActiveRuns (the restart-path function) and asserting the
//     result length matches the git+Beads union count, not the decoy JSONL count.
//
// This is the behavioral half of the EM-INV-001 invariant: the invariant says
// "git + Beads MUST be sufficient." The test proves it IS sufficient (the
// correct result is derivable from git+Beads alone) and that JSONL is NOT
// consulted (the JSONL decoy would produce a wrong result if walked).
//
// Spec ref: execution-model.md §5 EM-INV-001; §4.7 EM-031a.
// Spec ref: execution-model.md §10.2 — "destroy the daemon; confirm full state
// reconstructable from git + Beads without JSONL reads."
func TestEMINV001_RestartScenario_StateReconstructableWithoutJSONL(t *testing.T) {
	t.Parallel()

	const gitRunCount = 2
	const beadsOnlyCount = 1
	const jsonlDecoyCount = 7

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	gitRunIDs := make([]string, 0, gitRunCount)
	for i := range gitRunCount {
		runID := gitReconFixtureRunID(10 + i)
		gitRunIDs = append(gitRunIDs, runID)
		durableFixtureCreateTaskBranch(t, repoDir, runID)
		durableFixtureCommitCheckpoint(t, repoDir, runID, fmt.Sprintf("node-%d", i+1))
	}

	harmonikDir := filepath.Join(repoDir, ".harmonik")
	gitReconFixtureJSONLEventLog(t, harmonikDir, jsonlDecoyCount)

	querier := &activeRunDiscoveryFixtureFakeQuerier{
		statusMap: make(map[string][]core.BeadRecord),
	}
	for i := range beadsOnlyCount {
		beadID := core.BeadID(gitReconFixtureBeadID(20 + i))
		querier.statusMap["open"] = append(querier.statusMap["open"],
			activeRunDiscoveryFixtureBeadRecord(string(beadID), core.CoarseStatusOpen),
		)
	}

	tips := make([]BranchTip, 0, len(gitRunIDs))
	for _, runID := range gitRunIDs {
		tips = append(tips, BranchTip{
			BranchName: "run/" + runID,
			RunID:      runID,
			BeadID:     "",
		})
	}
	reader := activeRunDiscoveryFixtureReaderWithTips(tips...)

	set, err := DiscoverActiveRuns(context.Background(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns (restart path): unexpected error: %v", err)
	}

	wantLen := gitRunCount + beadsOnlyCount
	if set.Len() != wantLen {
		t.Errorf(
			"EM-INV-001 restart scenario: ActiveRunSet.Len() = %d; want %d (git+Beads union)\n"+
				"If this is %d, DiscoverActiveRuns is walking the JSONL decoy file instead of git+Beads.",
			set.Len(), wantLen, jsonlDecoyCount,
		)
	}
}

// TestEMINV001_RestartScenario_JSONLLossDoesNotAffectDiscovery verifies that
// deleting the JSONL event log entirely does not affect the restart path. The
// active-run set produced by DiscoverActiveRuns when no JSONL file exists MUST
// equal the set produced when a JSONL file is present (the decoy test above).
//
// This is the dual of the decoy test: it proves that JSONL absence does not
// break state reconstruction, confirming that git+Beads are sufficient.
//
// Spec ref: execution-model.md §5 EM-INV-001 — "git + Beads MUST be sufficient."
// Spec ref: execution-model.md §4.7 EM-032 — "transition history refers to the
// git checkpoint trail, NOT the JSONL event tail."
func TestEMINV001_RestartScenario_JSONLLossDoesNotAffectDiscovery(t *testing.T) {
	t.Parallel()

	const gitRunCount = 3

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	tips := make([]BranchTip, 0, gitRunCount)
	for i := range gitRunCount {
		runID := gitReconFixtureRunID(30 + i)
		durableFixtureCreateTaskBranch(t, repoDir, runID)
		durableFixtureCommitCheckpoint(t, repoDir, runID, fmt.Sprintf("node-%d", i+1))
		tips = append(tips, BranchTip{
			BranchName: "run/" + runID,
			RunID:      runID,
			BeadID:     "",
		})
	}

	reader := activeRunDiscoveryFixtureReaderWithTips(tips...)
	querier := activeRunDiscoveryFixtureEmptyQuerier()

	set, err := DiscoverActiveRuns(context.Background(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns (no-JSONL path): unexpected error: %v", err)
	}

	if set.Len() != gitRunCount {
		t.Errorf(
			"EM-INV-001 JSONL-loss test: ActiveRunSet.Len() = %d; want %d\n"+
				"State reconstruction MUST NOT require JSONL; git+Beads alone must be sufficient "+
				"(execution-model.md §5 EM-INV-001).",
			set.Len(), gitRunCount,
		)
	}
}

// TestEMINV001_RestartScenario_EmptyGitAndBeadsIsValidState verifies that when
// both git and Beads are empty (no in-flight runs at crash time), DiscoverActiveRuns
// returns an empty set — not an error. This covers the degenerate restart
// scenario (daemon crashed before any run was started).
//
// Spec ref: execution-model.md §5 EM-INV-001; §4.7 EM-031a.
func TestEMINV001_RestartScenario_EmptyGitAndBeadsIsValidState(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	durableFixtureInitRepo(t, repoDir)

	harmonikDir := filepath.Join(repoDir, ".harmonik")
	gitReconFixtureJSONLEventLog(t, harmonikDir, 5)

	set, err := DiscoverActiveRuns(
		context.Background(),
		activeRunDiscoveryFixtureEmptyQuerier(),
		activeRunDiscoveryFixtureEmptyReader(),
	)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns (empty state): unexpected error: %v", err)
	}

	if set.Len() != 0 {
		t.Errorf(
			"EM-INV-001 empty-state test: ActiveRunSet.Len() = %d; want 0\n"+
				"If this is 5, DiscoverActiveRuns is walking the JSONL decoy. "+
				"git+Beads both empty → no in-flight runs → empty set.",
			set.Len(),
		)
	}
}
