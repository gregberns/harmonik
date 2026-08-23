package lifecycle

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var noTxSubsystemFixtureForbiddenPatterns = []string{
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

	"rollbackNCheckpoints",
	"RollbackNCheckpoints",
	"rollbackNBeadWrites",
	"RollbackNBeadWrites",
	"undoNCheckpoints",
	"UndoNCheckpoints",
	"undoNTransitions",
	"UndoNTransitions",

	`"rewind-to-last-merge"`,
	`"rewind-to-merge"`,
	`"undo-previous-n"`,
	`"undo-previous-N"`,
	`"atomic-rollback"`,
	`"rollback-n-checkpoints"`,
	`"rollback-N-checkpoints"`,
}

var noTxSubsystemFixtureScannedRoots = []string{
	"internal",
	"cmd",
}

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
//
// # WHAT THIS DOES NOT CATCH, stated so nobody mistakes it for the invariant
//
// It is a substring match against the named spellings in
// noTxSubsystemFixtureForbiddenPatterns, and those come from the prose of
// EM-INV-004. It fails on a primitive that carries one of those names. It says
// nothing about a primitive that does the same thing under any other name —
// RollbackAll, RevertBatch, an unnamed loop that walks a transition list
// backwards. So a green here is evidence that nobody wrote the FORBIDDEN NAMES,
// not evidence that the invariant holds.
//
// That is worth having and it is cheap, but it is a name check. The invariant
// itself is enforced by review.
func TestEMINV004_NoSubsystemImplementsWorkflowTransactionality(t *testing.T) {
	t.Parallel()

	repoRoot := noTxSubsystemFixtureRepoRoot(t)

	var sourceFiles []string
	for _, rel := range noTxSubsystemFixtureScannedRoots {
		root := filepath.Join(repoRoot, rel)
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("scanned root %q is unreadable (%v); noTxSubsystemFixtureScannedRoots is stale "+
				"and this sensor is covering less of the tree than it claims", root, err)
		}
		sourceFiles = append(sourceFiles, noTxSubsystemFixtureCollectGoSources(t, root)...)
	}

	if len(sourceFiles) == 0 {
		t.Fatalf("no Go source files found under %v; the sensor scanned nothing, so its result is not a pass",
			noTxSubsystemFixtureScannedRoots)
	}

	for _, filePath := range sourceFiles {
		for _, pattern := range noTxSubsystemFixtureForbiddenPatterns {
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
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("scanned root %q is unreadable (%v); noTxSubsystemFixtureScannedRoots is stale", root, err)
		}
		total += len(noTxSubsystemFixtureCollectGoSources(t, root))
	}

	if total == 0 {
		t.Error("EM-INV-004 sensor has no coverage: zero Go source files found under scanned roots " +
			"(internal/, cmd/); sensor is not operating")
	}
}
