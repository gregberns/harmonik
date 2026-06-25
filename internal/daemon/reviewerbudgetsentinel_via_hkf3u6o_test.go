package daemon

// reviewerbudgetsentinel_via_hkf3u6o_test.go — tests for
// ReadReviewerBudgetSentinelVia, the remote-aware budget-marker reader (hk-f3u6o).
//
// On a REMOTE run the reviewer's budget-kill marker is written into the worktree
// ON THE WORKER, so a box-A os.ReadFile (ReadReviewerBudgetSentinel) never finds
// it → the daemon cannot distinguish a budget kill from a true no-verdict.
// ReadReviewerBudgetSentinelVia routes the read through the run's CommandRunner.
//
// Mirrors the hk92ih3 / ComputeDiffHashVia idiom: nil/local runner → byte-
// identical local read; a non-local runner → routed read.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// catFromFileRunnerBudget is a non-local CommandRunner stub: every Command()
// invocation is rewritten to `cat <srcPath>`, returning the bytes of srcPath
// regardless of the requested argv. It simulates a remote worker whose budget
// marker lives at srcPath. A distinct (non-LocalRunner) type ⇒ runnerIsLocalFS
// classifies it non-local ⇒ ReadReviewerBudgetSentinelVia routes through it.
type catFromFileRunnerBudget struct {
	srcPath string // "" → a nonexistent path so cat fails (simulates absent marker)
}

func (r catFromFileRunnerBudget) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	src := r.srcPath
	if src == "" {
		src = filepath.Join(os.TempDir(), "hkf3u6o-nonexistent-budget-marker")
	}
	//nolint:gosec // G204: src is a test-controlled temp path, not user input
	return exec.CommandContext(ctx, "cat", src)
}

func writeBudgetSentinelFixture(t *testing.T, path string) {
	t.Helper()
	pl := reviewerBudgetSentinel{
		BudgetMS:     12000,
		ChangedLines: 4200,
		ElapsedMS:    13000,
		Reason:       "diff-scaled-budget-exceeded",
	}
	b, err := json.Marshal(pl)
	if err != nil {
		t.Fatalf("marshal sentinel fixture: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write sentinel fixture: %v", err)
	}
}

// TestReadReviewerBudgetSentinelVia_NilRunner_ReadsLocally verifies nil-runner is
// byte-identical to the local reader (NFR7).
func TestReadReviewerBudgetSentinelVia_NilRunner_ReadsLocally(t *testing.T) {
	t.Parallel()

	wtPath := t.TempDir()
	//nolint:gosec // G301: test dir
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeBudgetSentinelFixture(t, reviewerBudgetSentinelPath(wtPath))

	got, err := ReadReviewerBudgetSentinelVia(context.Background(), nil, wtPath)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinelVia(nil): %v", err)
	}
	if got == nil || got.Reason != "diff-scaled-budget-exceeded" {
		t.Fatalf("got = %+v; want the locally-written sentinel", got)
	}
}

// TestReadReviewerBudgetSentinelVia_RemoteRunner_ReadsViaRunner is the core
// regression: box-A worktree has NO marker (local read → (nil,nil)), but the
// non-local runner cats a valid worker-side marker. The marker MUST be read via
// the runner.
func TestReadReviewerBudgetSentinelVia_RemoteRunner_ReadsViaRunner(t *testing.T) {
	t.Parallel()

	workerMarker := filepath.Join(t.TempDir(), "worker-budget.json")
	writeBudgetSentinelFixture(t, workerMarker)

	boxAWt := t.TempDir() // no local marker
	if _, statErr := os.Stat(reviewerBudgetSentinelPath(boxAWt)); statErr == nil {
		t.Fatal("precondition: box-A worktree must NOT contain the budget marker")
	}

	runner := catFromFileRunnerBudget{srcPath: workerMarker}

	got, err := ReadReviewerBudgetSentinelVia(context.Background(), runner, boxAWt)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinelVia(remote): %v", err)
	}
	if got == nil {
		t.Fatal("ReadReviewerBudgetSentinelVia(remote) returned nil; want the worker-side marker routed via the runner")
	}
	if got.Reason != "diff-scaled-budget-exceeded" || got.ChangedLines != 4200 {
		t.Errorf("got = %+v; want reason=diff-scaled-budget-exceeded changed_lines=4200", got)
	}
}

// TestReadReviewerBudgetSentinelVia_RemoteRunner_AbsentReturnsNil verifies that
// when the runner's cat fails (worker marker absent), the result is (nil,nil) —
// the normal "no budget kill" case, matching the local not-exist branch.
func TestReadReviewerBudgetSentinelVia_RemoteRunner_AbsentReturnsNil(t *testing.T) {
	t.Parallel()

	boxAWt := t.TempDir()
	runner := catFromFileRunnerBudget{srcPath: ""}

	got, err := ReadReviewerBudgetSentinelVia(context.Background(), runner, boxAWt)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinelVia(remote-absent) error = %v; want nil", err)
	}
	if got != nil {
		t.Errorf("got = %+v; want nil (absent marker)", got)
	}
}

// Ensure tmux import is exercised (LocalRunner classification path) without a
// dedicated test duplicating the workspace one.
var _ tmux.CommandRunner = catFromFileRunnerBudget{}
