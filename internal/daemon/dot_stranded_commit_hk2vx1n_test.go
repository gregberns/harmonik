package daemon

// dot_stranded_commit_hk2vx1n_test.go — a red gate that bounces a real commit
// must not be reported as "the implementer landed nothing" (hk-2vx1n).
//
// Measured live on 2026-08-10 at b49210d6 and recorded on hk-rqxz3: a codex
// implementer made a real change, committed it, and exited 0. `make full` ran
// 75001 tests across 108 packages, ONE of them failed, and it was racy rather
// than broken. The graph took the commit_gate→implement back edge seven minutes
// later, the implementer had nothing to add, and the run ended on a no-progress
// terminal whose reason says only that HEAD did not advance.
//
// The commit is still on run/<run_id> and on no other branch. The reason does
// not say so, so the run reads exactly like a harness that never landed
// anything — and the investigation went to the harness. That misdirection is
// the cost this guards against.
//
// The rule under test is strandedCommitNote. It is deliberately narrow: it
// speaks ONLY when the gate is what routed the implementer back. A no-progress
// loop with no gate in it has nothing to preserve and must stay silent.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workspace"
)

// strandedHeadSHA is the commit the gate bounced — the one that gets left on
// the run branch.
const strandedHeadSHA = "7482bfc0ad5faf7a98ca6ab2012e1466f769c3eb"

func TestStrandedCommitNote_GateBouncedARealCommit(t *testing.T) {
	t.Parallel()

	runID := gateLogNewRunID(t)
	note := strandedCommitNote(runID, true, false, "commit_gate", "commit_gate", strandedHeadSHA,
		"--- FAIL: TestFlaky (0.02s)\nFAIL\nmake: *** [full] Error 1\n")

	if note == "" {
		t.Fatal("a gate-red bounce over a real commit produced no note; the run would report only that HEAD did not advance")
	}
	if !strings.Contains(note, strandedHeadSHA) {
		t.Errorf("the note does not name the stranded commit, so nobody can go and look at it:\n%s", note)
	}
	wantBranch := workspace.TaskBranchPrefix + runID.String()
	if !strings.Contains(note, wantBranch) {
		t.Errorf("the note does not name %q, the only branch the commit is on:\n%s", wantBranch, note)
	}
	if !strings.Contains(note, "NOT merged") {
		t.Errorf("the note does not say the work was not merged:\n%s", note)
	}
	if !strings.Contains(note, "TestFlaky") {
		t.Errorf("the note drops the gate output, which is what says whether the failure was even this diff's:\n%s", note)
	}
}

// TestStrandedCommitNote_StaysSilent covers every shape that must NOT produce a
// note. Each one is a case where there is no bounced commit to preserve, and a
// note would assert something untrue.
func TestStrandedCommitNote_StaysSilent(t *testing.T) {
	t.Parallel()

	const notes = "--- FAIL: TestThing\n"
	cases := []struct {
		name      string
		committed bool
		passed    bool
		gateNode  string
		prevNode  string
		why       string
	}{
		{
			name: "no gate has ever run", committed: true, passed: false,
			gateNode: "", prevNode: "implement",
			why: "gatePassed is false before any gate runs too; this is the ordinary stuck loop with nothing committed by a gate",
		},
		{
			name: "the gate passed", committed: true, passed: true,
			gateNode: "commit_gate", prevNode: "commit_gate",
			why: "a green gate did not bounce anyone",
		},
		{
			name: "nothing was committed", committed: false, passed: false,
			gateNode: "commit_gate", prevNode: "commit_gate",
			why: "there is no commit to strand — this is the genuine no-commit nudge case",
		},
		{
			name: "a reviewer routed back, not the gate", committed: true, passed: false,
			gateNode: "commit_gate", prevNode: "review",
			why: "the reviewer is the proximate cause; blaming the gate would misdirect the reader the other way",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := strandedCommitNote(gateLogNewRunID(t), tc.committed, tc.passed,
				tc.gateNode, tc.prevNode, strandedHeadSHA, notes)
			if got != "" {
				t.Errorf("expected no note (%s), got:\n%s", tc.why, got)
			}
		})
	}
}

// TestStrandedCommitNote_NoGateOutput — a gate that failed without recording
// output still strands a commit, and the note must still name it.
func TestStrandedCommitNote_NoGateOutput(t *testing.T) {
	t.Parallel()

	note := strandedCommitNote(gateLogNewRunID(t), true, false, "commit_gate", "commit_gate", strandedHeadSHA, "   \n  ")
	if !strings.Contains(note, strandedHeadSHA) {
		t.Fatalf("a gate that recorded no output still stranded a commit; the note must name it:\n%s", note)
	}
	if strings.Contains(note, "gate output:") {
		t.Errorf("the note offers a gate-output clause with nothing in it:\n%s", note)
	}
}

// TestGateFailureTail_KeepsTheEnd — a build or test gate names what failed at
// the END of its output, so a bounded excerpt must keep the tail. Keeping the
// head would reliably capture the toolchain banner and drop the failure.
func TestGateFailureTail_KeepsTheEnd(t *testing.T) {
	t.Parallel()

	head := strings.Repeat("go: downloading something irrelevant\n", 40)
	tail := "--- FAIL: TestTheOneThatMatters (0.02s)\nFAIL\n"
	got := gateFailureTail(head + tail)

	if !strings.Contains(got, "TestTheOneThatMatters") {
		t.Errorf("the excerpt dropped the failure at the end of the output:\n%s", got)
	}
	if !strings.HasSuffix(got, "FAIL ") && !strings.HasSuffix(got, "FAIL") {
		t.Errorf("the excerpt does not end at the end of the gate output:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("a 1.5 KB gate output produced an un-truncated excerpt, so the head was never dropped:\n%s", got)
	}
	if len(got) > gateFailureTailMaxBytes+len("; gate output: ")+len("…") {
		t.Errorf("the excerpt is unbounded at %d bytes; it travels into run_failed, which is read one line at a time", len(got))
	}
	if strings.Contains(got, "\n") {
		t.Errorf("the excerpt carries newlines into a one-line reason:\n%q", got)
	}
}

// TestGateFailureTail_Empty — no output, no clause.
func TestGateFailureTail_Empty(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "   ", "\n\n\t"} {
		if got := gateFailureTail(in); got != "" {
			t.Errorf("gateFailureTail(%q) = %q, want empty", in, got)
		}
	}
}

// TestStrandedCommitNote_RunIDIsNotConfusedWithTheCommit guards a transposition
// in the argument list: the note takes both a run id and a head SHA, and the
// two are the same shape at a glance.
func TestStrandedCommitNote_RunIDIsNotConfusedWithTheCommit(t *testing.T) {
	t.Parallel()

	runID := gateLogNewRunID(t)
	note := strandedCommitNote(runID, true, false, "commit_gate", "commit_gate", strandedHeadSHA, "")

	commitIdx := strings.Index(note, "commit "+strandedHeadSHA)
	branchIdx := strings.Index(note, workspace.TaskBranchPrefix+runID.String())
	if commitIdx < 0 || branchIdx < 0 {
		t.Fatalf("note is missing the commit or the branch:\n%s", note)
	}
	if !strings.Contains(note, "commit "+strandedHeadSHA+" is preserved on "+workspace.TaskBranchPrefix+runID.String()) {
		t.Errorf("the commit and the branch are transposed or separated:\n%s", note)
	}
	if strings.Contains(note, "commit "+runID.String()) {
		t.Errorf("the run id is being reported as the commit:\n%s", note)
	}
}

// runIDIsCoreRunID keeps the seam honest: strandedCommitNote takes the typed
// run id, not a string, so a caller cannot hand it a worktree path by mistake.
var _ func(core.RunID, bool, bool, string, string, string, string) string = strandedCommitNote
