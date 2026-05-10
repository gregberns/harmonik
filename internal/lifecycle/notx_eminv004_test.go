package lifecycle

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notx_eminv004_test.go — cross-subsystem static sensor for EM-INV-004 (no
// subsystem may implement workflow-level transactionality).
//
// EM-INV-004 spans four subsystems: execution-model, workspace-model,
// beads-integration, and reconciliation. Each could independently introduce a
// composable undo-previous-N primitive that, when combined with primitives
// from other subsystems, yields composition-level atomic undo — the failure
// mode the invariant forbids.
//
// Spec ref: specs/execution-model.md §5 EM-INV-004 — "any subsystem that
// writes to git, Beads, or workspace state MUST NOT implement an
// undo-previous-N-operations primitive that atomically rolls back prior
// checkpoints, prior bead status writes, or prior workspace branch advances
// when a later transition fails."
//
// Helper prefix: noTxSubsystemFixture (per bead hk-b3f.64 brief).

// noTxSubsystemFixtureForbiddenPatterns enumerates source-code patterns whose
// presence in a non-test Go source file constitutes an EM-INV-004 violation.
//
// Each pattern is a substring match. Patterns are chosen to match:
//   - Function/method names that implement or expose atomic multi-step undo,
//     rewind, or rollback primitives at the authoring surface.
//   - CLI command names or string literals that name such a primitive.
//
// Patterns are derived directly from the EM-INV-004 spec text ("undo-previous-N",
// "rewind-to-merge", "atomic-rollback-of-N-checkpoints") and the illustrative
// example ("rewind to last merge" CLI) in the invariant body.
//
// Patterns intentionally excluded:
//   - "rollback" alone — appears legitimately in transition_kind values
//     (architectural-rollback, policy-rollback) which are new-transition
//     representations, NOT undo primitives. EM-INV-004 permits rollback as a
//     new transition; it forbids primitives that atomically remove N prior
//     durable writes.
//   - "reset" alone — git reset is legitimate in reconciliation recovery code
//     working on a single non-durable scratch state; excluded to avoid false
//     positives.
//   - "revert" alone — git revert adds a new commit (append-only); it is not
//     an undo primitive that removes prior durable writes.
var noTxSubsystemFixtureForbiddenPatterns = []string{
	// Direct named primitives from the EM-INV-004 spec text.
	"UndoPreviousN",
	"undoPreviousN",
	"rewindToMerge",
	"RewindToMerge",
	"rewindToLastMerge",
	"RewindToLastMerge",
	"atomicRollbackN",
	"AtomicRollbackN",
	"AtomicRollbackOfN",
	"atomicRollbackOfN",

	// Undo primitives using N-checkpoint or N-step language.
	"rollbackNCheckpoints",
	"RollbackNCheckpoints",
	"rollbackNBeadWrites",
	"RollbackNBeadWrites",
	"undoNCheckpoints",
	"UndoNCheckpoints",
	"undoNTransitions",
	"UndoNTransitions",

	// CLI command strings naming the EM-INV-004 illustrative example.
	// Any Go source that registers or dispatches "rewind-to-last-merge" or
	// "undo-previous" as a CLI command name violates the invariant.
	`"rewind-to-last-merge"`,
	`"rewind-to-merge"`,
	`"undo-previous-n"`,
	`"undo-previous-N"`,
	`"atomic-rollback"`,
	`"rollback-n-checkpoints"`,
	`"rollback-N-checkpoints"`,
}

// noTxSubsystemFixtureScannedRoots is the set of source-tree subdirectories
// that constitute the full codebase for EM-INV-004 purposes. Test files
// (_test.go) are excluded by the walker. The scan covers all four subsystems
// named in the invariant (execution-model in internal/lifecycle,
// workspace-model in internal/workspace, beads-integration in internal/brcli,
// reconciliation in internal/lifecycle) plus the CLI surface in cmd/.
var noTxSubsystemFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

// noTxSubsystemFixtureRepoRoot resolves the absolute path of the repo root
// (the directory containing go.mod) by walking upward from os.Getwd.
// Calls t.Fatalf on failure.
func noTxSubsystemFixtureRepoRoot(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("noTxSubsystemFixtureRepoRoot: os.Getwd: %v", err)
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("noTxSubsystemFixtureRepoRoot: go.mod not found above %q", cwd)
		}
		dir = parent
	}
}

// noTxSubsystemFixtureCollectGoSources walks root and returns all non-test .go
// files found under it. Test files (_test.go suffix) are excluded because
// EM-INV-004 constrains the authoring surface (production code), not test code.
func noTxSubsystemFixtureCollectGoSources(t *testing.T, root string) []string {
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
		t.Fatalf("noTxSubsystemFixtureCollectGoSources: WalkDir(%q): %v", root, err)
	}
	return files
}

// TestEMINV004_NoSubsystemImplementsWorkflowTransactionality is the primary
// EM-INV-004 sensor.
//
// It scans every non-test Go source file under internal/ and cmd/ and asserts
// that none contain the forbidden undo-previous-N primitive patterns declared
// in noTxSubsystemFixtureForbiddenPatterns.
//
// The test is intentionally static (no subprocess invocation). EM-INV-004's
// verification statement is a source-inspection check: the invariant forbids
// primitives at the authoring surface, not at runtime. A runtime check could
// not detect a quiescent primitive that has been authored but not yet
// exercised.
//
// A failure here means a subsystem source file has introduced a primitive that
// atomically undoes N prior durable writes, violating EM-INV-004. Fix by
// removing the primitive; recovery from partial failure routes through
// reconciliation categories per specs/reconciliation/spec.md §8.
func TestEMINV004_NoSubsystemImplementsWorkflowTransactionality(t *testing.T) {
	t.Parallel()

	repoRoot := noTxSubsystemFixtureRepoRoot(t)

	var sourceFiles []string
	for _, rel := range noTxSubsystemFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			// Root doesn't exist yet (e.g., cmd/ before any binary is added);
			// skip silently — the sensor is still meaningful for roots that exist.
			continue
		}
		sourceFiles = append(sourceFiles, noTxSubsystemFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Skip("no Go source files found under scanned roots — nothing to check")
	}

	for _, filePath := range sourceFiles {
		filePath := filePath // capture for parallel subtest
		for _, pattern := range noTxSubsystemFixtureForbiddenPatterns {
			pattern := pattern // capture for parallel subtest
			t.Run(filepath.Base(filePath)+"/"+pattern, func(t *testing.T) {
				t.Parallel()

				//nolint:gosec // G304: path is constructed from repo root resolved by go.mod walk; not user-controlled
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
						"EM-INV-004 violation: source file %q contains forbidden workflow-transactionality pattern %q\n"+
							"No subsystem may implement an undo-previous-N primitive at the authoring surface\n"+
							"(specs/execution-model.md §5 EM-INV-004). Remove the primitive; recovery routes\n"+
							"through reconciliation categories per specs/reconciliation/spec.md §8.",
						rel, pattern,
					)
				}
			})
		}
	}
}

// TestEMINV004_SensorCoverage verifies that the EM-INV-004 sensor scanned at
// least one Go source file — a meta-test confirming the walker is not
// silently skipping the entire corpus.
//
// Without this guard, a misconfigured scanned-roots list would silently pass
// every pattern check against an empty file set, making the sensor useless.
func TestEMINV004_SensorCoverage(t *testing.T) {
	t.Parallel()

	repoRoot := noTxSubsystemFixtureRepoRoot(t)

	var total int
	for _, rel := range noTxSubsystemFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		total += len(noTxSubsystemFixtureCollectGoSources(t, root))
	}

	if total == 0 {
		t.Error("EM-INV-004 sensor has no coverage: zero Go source files found under scanned roots " +
			"(internal/, cmd/); sensor is not operating")
	}
}
