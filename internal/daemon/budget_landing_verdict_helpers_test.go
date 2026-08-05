package daemon_test

// budget_landing_verdict_helpers_test.go — four daemon helpers that no test
// reached.
//
// A mutation sweep broke each of the four functions below and the whole suite
// stayed green. Green meant nothing for them. Each test here was
// watched to go RED under the exact mutation from that sweep, and also under
// the laziest wrong version of the same function that a weak test still lets
// pass.
//
// The four claims:
//   - reviewBudgetForDiff  — a bigger diff buys the reviewer more wait, up to a cap.
//   - sumNumstatLines      — an empty or binary-only diff is zero lines, not a failure.
//   - resolveLandsOn       — an explicit landing branch wins over the spec default.
//   - gateVerdictExistsVia — the presence check answers for the filesystem the run is on.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// The knob values the production caller uses, restated here so a change to the
// production defaults cannot silently change what these tests claim.
const (
	budgetBase     = 10 * time.Minute
	budgetPerKLine = 10 * time.Minute
	budgetCeiling  = 60 * time.Minute
)

// TestReviewerBudget_ABiggerDiffBuysTheReviewerMoreWait pins the scaling itself.
// A version that ignores changedLines and always answers the base wait fails
// here, and so does the swept mutation that answers zero.
func TestReviewerBudget_ABiggerDiffBuysTheReviewerMoreWait(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		changedLines int
		want         time.Duration
	}{
		{"half a kilo-line earns half the per-kline slice", 500, 15 * time.Minute},
		{"one kilo-line earns one per-kline slice", 1000, 20 * time.Minute},
		{"four kilo-lines earn four slices", 4000, 50 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedReviewBudgetForDiff(tc.changedLines, budgetBase, budgetPerKLine, budgetCeiling)
			if got != tc.want {
				t.Fatalf("budget for %d changed lines = %v, want %v (base %v + %v per 1000 lines)",
					tc.changedLines, got, tc.want, budgetBase, budgetPerKLine)
			}
		})
	}

	// Positive evidence that the scaling is monotone, not one lucky constant.
	small := daemon.ExportedReviewBudgetForDiff(200, budgetBase, budgetPerKLine, budgetCeiling)
	large := daemon.ExportedReviewBudgetForDiff(4000, budgetBase, budgetPerKLine, budgetCeiling)
	if !(small > budgetBase && large > small) {
		t.Fatalf("budget did not grow with the diff: base=%v small=%v large=%v", budgetBase, small, large)
	}
}

// TestReviewerBudget_TheHardCeilingCapsAHugeDiff defends the upper bound. A
// version that drops the ceiling clamp would hand a million-line diff a
// multi-day wait and wedge the queue.
func TestReviewerBudget_TheHardCeilingCapsAHugeDiff(t *testing.T) {
	t.Parallel()

	got := daemon.ExportedReviewBudgetForDiff(1_000_000, budgetBase, budgetPerKLine, budgetCeiling)
	if got != budgetCeiling {
		t.Fatalf("budget for a 1,000,000-line diff = %v, want the ceiling %v", got, budgetCeiling)
	}
}

// TestReviewerBudget_AnUnknownDiffFallsBackToTheBaseWait covers the -1 sentinel
// the git probe returns when it cannot measure the diff, and the empty diff.
// Neither may collapse the wait to zero.
func TestReviewerBudget_AnUnknownDiffFallsBackToTheBaseWait(t *testing.T) {
	t.Parallel()

	for _, changedLines := range []int{-1, 0} {
		got := daemon.ExportedReviewBudgetForDiff(changedLines, budgetBase, budgetPerKLine, budgetCeiling)
		if got != budgetBase {
			t.Errorf("budget for changedLines=%d = %v, want the base wait %v", changedLines, got, budgetBase)
		}
	}

	// The ceiling still wins when it is tighter than the base — a per-node
	// override can set a ceiling below the base wait.
	tight := 5 * time.Minute
	if got := daemon.ExportedReviewBudgetForDiff(-1, budgetBase, budgetPerKLine, tight); got != tight {
		t.Errorf("unknown diff under a %v ceiling = %v, want the ceiling", tight, got)
	}
}

// TestReviewerBudget_TheWaitNeverDropsBelowTheBaseFloor defends the floor. A
// negative per-kline knob is a value the signature admits, and it must not buy
// the reviewer LESS time than a zero-line diff gets.
func TestReviewerBudget_TheWaitNeverDropsBelowTheBaseFloor(t *testing.T) {
	t.Parallel()

	got := daemon.ExportedReviewBudgetForDiff(2000, budgetBase, -5*time.Minute, budgetCeiling)
	if got != budgetBase {
		t.Fatalf("budget with a negative per-kline knob = %v, want the base floor %v", got, budgetBase)
	}
}

// TestNumstatSum_AddedAndDeletedLinesAcrossEveryFileAreTotalled is the plain
// claim: the reviewer budget is driven by the real size of the diff. A version
// that always answers zero fails here.
func TestNumstatSum_AddedAndDeletedLinesAcrossEveryFileAreTotalled(t *testing.T) {
	t.Parallel()

	const numstat = "3\t4\tinternal/a.go\n10\t2\tinternal/b.go\n"
	total, ok := daemon.ExportedSumNumstatLines(numstat)
	if !ok {
		t.Fatalf("a well-formed numstat was reported unreadable")
	}
	if total != 19 {
		t.Fatalf("changed lines = %d, want 19 (3+4 in a.go plus 10+2 in b.go)", total)
	}
}

// TestNumstatSum_ABinaryRowIsSkippedNotFailed defends the binary case. Git
// writes "-\t-\tpath" for a binary file. Those rows carry no line count, so
// they are skipped, and a diff made only of them is still a readable zero.
func TestNumstatSum_ABinaryRowIsSkippedNotFailed(t *testing.T) {
	t.Parallel()

	total, ok := daemon.ExportedSumNumstatLines("-\t-\tassets/logo.png\n5\t5\tinternal/a.go\n")
	if !ok {
		t.Fatalf("a numstat with a binary row was reported unreadable")
	}
	if total != 10 {
		t.Fatalf("changed lines = %d, want 10 — the binary row must add nothing, not fail the read", total)
	}

	onlyBinary, ok := daemon.ExportedSumNumstatLines("-\t-\tassets/logo.png\n")
	if !ok {
		t.Fatalf("a binary-only numstat was reported unreadable")
	}
	if onlyBinary != 0 {
		t.Fatalf("changed lines for a binary-only diff = %d, want 0", onlyBinary)
	}
}

// TestNumstatSum_AnEmptyDiffIsZeroLinesNotAFailure separates "no changes" from
// "cannot tell". They lead to the same budget today, but only one of them is
// the truth, and the caller logs them apart.
func TestNumstatSum_AnEmptyDiffIsZeroLinesNotAFailure(t *testing.T) {
	t.Parallel()

	for _, numstat := range []string{"", "\n", "   \n\t\n"} {
		total, ok := daemon.ExportedSumNumstatLines(numstat)
		if !ok {
			t.Errorf("empty numstat %q was reported unreadable, want a readable zero", numstat)
		}
		if total != 0 {
			t.Errorf("empty numstat %q gave %d changed lines, want 0", numstat, total)
		}
	}
}

// TestLandingTarget_AnExplicitTargetBranchWinsOverTheDefault is the claim that
// a bead can steer its own merge-back. A version that always answers the spec
// default would land every task branch on main and silently ignore the bead.
func TestLandingTarget_AnExplicitTargetBranchWinsOverTheDefault(t *testing.T) {
	t.Parallel()

	for _, branch := range []string{"integration", "release/2026-08", "main"} {
		got := daemon.ExportedResolveLandsOn(daemon.ExportedBranchingConfig{LandsOn: branch})
		if got != branch {
			t.Errorf("landing target for lands_on=%q = %q, want %q", branch, got, branch)
		}
	}
}

// TestLandingTarget_AnAbsentTargetBranchFallsBackToMain defends the safety net
// for an ad-hoc config that never went through resolveBranching. An empty
// answer here would make the merge-back target an empty git ref.
func TestLandingTarget_AnAbsentTargetBranchFallsBackToMain(t *testing.T) {
	t.Parallel()

	got := daemon.ExportedResolveLandsOn(daemon.ExportedBranchingConfig{})
	if got != "main" {
		t.Fatalf("landing target for an empty config = %q, want the spec default \"main\"", got)
	}
}

// stubRemoteRunner is a CommandRunner that is not the local runner, so the
// presence check must route through it. It records every argv and answers each
// command with a fixed exit status.
type stubRemoteRunner struct {
	inner   tmuxPkg.RecordingRunner
	succeed bool
}

func (r *stubRemoteRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	r.inner.CmdFunc = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		if r.succeed {
			return exec.CommandContext(ctx, "sh", "-c", "exit 0")
		}
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	return r.inner.Command(ctx, name, args...)
}

var _ tmuxPkg.CommandRunner = (*stubRemoteRunner)(nil)

// writeVerdictFile writes a gate-verdict file of the given contents and returns
// its path.
func writeVerdictFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gate-verdict.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write verdict fixture: %v", err)
	}
	return path
}

// TestGateVerdictPresence_AWrittenVerdictReadsAsPresent is the plain claim. A
// version that always reports the verdict absent leaves the gate waiting for a
// file that is already on disk.
func TestGateVerdictPresence_AWrittenVerdictReadsAsPresent(t *testing.T) {
	t.Parallel()

	path := writeVerdictFile(t, `{"action":"proceed"}`)
	for _, runner := range []tmuxPkg.CommandRunner{nil, tmuxPkg.LocalRunner{}} {
		if !daemon.ExportedGateVerdictExistsVia(context.Background(), runner, path) {
			t.Errorf("a written verdict at %s read as absent under runner %T", path, runner)
		}
	}
}

// TestGateVerdictPresence_AnEmptyOrMissingVerdictReadsAsAbsent defends the
// half-written case. The evaluator creates the file before it writes the bytes,
// so a zero-byte file means "not yet", not "decided".
func TestGateVerdictPresence_AnEmptyOrMissingVerdictReadsAsAbsent(t *testing.T) {
	t.Parallel()

	empty := writeVerdictFile(t, "")
	if daemon.ExportedGateVerdictExistsVia(context.Background(), nil, empty) {
		t.Errorf("a zero-byte verdict at %s read as present — the gate would parse a half-written file", empty)
	}

	missing := filepath.Join(t.TempDir(), "gate-verdict.json")
	if daemon.ExportedGateVerdictExistsVia(context.Background(), nil, missing) {
		t.Errorf("a verdict that was never written read as present")
	}
}

// TestGateVerdictPresence_ARemoteRunAsksTheWorkerNotTheDaemonBox is the claim
// that matters on a remote run: the verdict lives on the worker's filesystem,
// so the check must go through the runner. The daemon box deliberately has no
// such file here, so a version that always calls os.Stat answers absent and
// fails.
func TestGateVerdictPresence_ARemoteRunAsksTheWorkerNotTheDaemonBox(t *testing.T) {
	t.Parallel()

	// Path exists on the worker only. Nothing is written on this box.
	const workerPath = "/harmonik/worker/only/gate-verdict.json"
	runner := &stubRemoteRunner{succeed: true}

	if !daemon.ExportedGateVerdictExistsVia(context.Background(), runner, workerPath) {
		t.Fatalf("the worker reported the verdict present, but the check answered absent — it did not use the runner")
	}

	calls := runner.inner.Calls
	if len(calls) != 1 {
		t.Fatalf("runner saw %d commands, want exactly 1: %+v", len(calls), calls)
	}
	got := append([]string{calls[0].Name}, calls[0].Args...)
	want := []string{"test", "-s", workerPath}
	if len(got) != len(want) {
		t.Fatalf("runner argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("runner argv = %v, want %v", got, want)
		}
	}
}

// TestGateVerdictPresence_ARemoteWorkerSayingNoOutranksALocalFile is the other
// half of the pair above. A file of the same name on the daemon box must not
// make a remote gate look decided.
func TestGateVerdictPresence_ARemoteWorkerSayingNoOutranksALocalFile(t *testing.T) {
	t.Parallel()

	path := writeVerdictFile(t, `{"action":"proceed"}`)
	runner := &stubRemoteRunner{succeed: false}

	if daemon.ExportedGateVerdictExistsVia(context.Background(), runner, path) {
		t.Fatalf("the worker has no verdict, but the check answered present — it read the daemon box's own filesystem")
	}
	if len(runner.inner.Calls) != 1 {
		t.Fatalf("runner saw %d commands, want exactly 1 — the remote path was skipped", len(runner.inner.Calls))
	}
}
