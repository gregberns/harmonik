package daemon_test

// pasteinject_hksah87_test.go — unit tests for the diff-scaled, progress-aware
// reviewer-verdict budget and the budget-kill marker in
// pasteInjectQuitOnReviewFile (hk-sah87).
//
// Problem addressed: reviewFileTimeout was a FLAT 10-minute deadline.  On a
// heavy/large-diff bead the reviewer claude needs longer than 10 min just to
// READ the diff before it can write .harmonik/review.json — so the flat deadline
// /quit+killed it mid-review, ReadReviewVerdict returned nil, and the run
// false-failed as "verdict absent at iteration N".  The implementer phase, by
// contrast, has a 90-minute progress-aware ceiling (commitHardCeiling).
//
// The fix scales the verdict budget by diff size — base reviewFileTimeout plus
// reviewFilePerKLineBudget per 1000 changed lines, capped at
// reviewFileHardCeiling (well below the implementer's 90 min so a hung reviewer
// is still bounded).  A pane-liveness check extends a deadline that would land
// while the reviewer is still actively working.  On a budget kill a marker file
// (reviewer-budget-exceeded.json) is written so the caller can emit a distinct
// "reviewer budget exceeded" diagnostic instead of the generic "verdict absent".
//
// Test matrix:
//   - BudgetForDiff: reviewBudgetForDiff scaling + clamping table.
//   - SumNumstatLines: numstat parsing (added+deleted, binary skip, empty).
//   - BudgetKillWritesSentinel: a no-verdict timeout writes the marker with the
//     budget/elapsed/reason fields and is readable via ReadReviewerBudgetSentinel.
//   - PaneLivenessExtendsBudget: an active reviewer pane past the base budget is
//     extended (no kill within a window > base budget) up to the hard ceiling.
//   - HardCeilingKillsActivePane: an always-active reviewer pane is still killed
//     once the hard ceiling elapses (reason "hard-ceiling").
//
// Helper prefix: hksah87.
// Bead: hk-sah87.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// hksah87WriteVerdict writes a non-empty review.json into <wtPath>/.harmonik/.
func hksah87WriteVerdict(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hksah87WriteVerdict: mkdir: %v", err)
	}
	content := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("hksah87WriteVerdict: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hksah87QuitSender records SendQuitToLastPane calls (no liveness).
type hksah87QuitSender struct {
	calls atomic.Int64
}

func (q *hksah87QuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hksah87LivenessQuitSender implements quitSender + paneLivenessChecker; alive
// controls PaneHasActiveProcess.
type hksah87LivenessQuitSender struct {
	quitCalls atomic.Int64
	alive     atomic.Bool
}

func (q *hksah87LivenessQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.quitCalls.Add(1)
	return nil
}

func (q *hksah87LivenessQuitSender) PaneHasActiveProcess(_ context.Context) bool {
	return q.alive.Load()
}

var _ daemon.PaneLivenessCheckerExported = (*hksah87LivenessQuitSender)(nil)

// hksah87Killer records Kill calls.
type hksah87Killer struct {
	calls atomic.Int64
}

func (k *hksah87Killer) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hksah87SetBudget overrides the reviewer-budget timing vars and returns a
// restore function.  briefDeliveredTimeout and postQuitKillGrace are pinned long
// so neither interferes with the budget-kill path under test.
func hksah87SetBudget(base, perKLine, ceiling, poll, killDelay time.Duration) func() {
	origBase := *daemon.ExportedReviewFileTimeout
	origPerK := *daemon.ExportedReviewFilePerKLineBudget
	origCeil := *daemon.ExportedReviewFileHardCeiling
	origPoll := *daemon.ExportedReviewFilePollInterval
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedReviewFileTimeout = base
	*daemon.ExportedReviewFilePerKLineBudget = perKLine
	*daemon.ExportedReviewFileHardCeiling = ceiling
	*daemon.ExportedReviewFilePollInterval = poll
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedReviewFileTimeout = origBase
		*daemon.ExportedReviewFilePerKLineBudget = origPerK
		*daemon.ExportedReviewFileHardCeiling = origCeil
		*daemon.ExportedReviewFilePollInterval = origPoll
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// reviewBudgetForDiff
// ─────────────────────────────────────────────────────────────────────────────

func TestReviewBudgetForDiff(t *testing.T) {
	base := 10 * time.Minute
	perK := 5 * time.Minute
	ceiling := 30 * time.Minute

	cases := []struct {
		name    string
		changed int
		want    time.Duration
	}{
		{"unknown-negative-uses-base", -1, base},
		{"zero-uses-base", 0, base},
		{"small-diff-100-lines", 100, base + 30*time.Second}, // 5m * 100/1000
		{"1000-lines-one-perK", 1000, base + perK},
		{"2000-lines-two-perK", 2000, base + 2*perK},
		{"huge-diff-clamped-to-ceiling", 100000, ceiling},
		{"exactly-at-ceiling-4000-lines", 4000, ceiling}, // 10m + 20m = 30m
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := daemon.ExportedReviewBudgetForDiff(tc.changed, base, perK, ceiling)
			if got != tc.want {
				t.Errorf("reviewBudgetForDiff(%d): want %v, got %v", tc.changed, tc.want, got)
			}
		})
	}
}

// TestReviewBudgetForDiff_BaseAboveCeilingClampsToCeiling guards the defensive
// clamp: a misconfiguration where base > ceiling must not exceed the ceiling.
func TestReviewBudgetForDiff_BaseAboveCeilingClampsToCeiling(t *testing.T) {
	got := daemon.ExportedReviewBudgetForDiff(0, 40*time.Minute, 5*time.Minute, 30*time.Minute)
	if got != 30*time.Minute {
		t.Errorf("base>ceiling with zero diff: want ceiling 30m, got %v", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// sumNumstatLines
// ─────────────────────────────────────────────────────────────────────────────

func TestSumNumstatLines(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   int
		wantOK bool
	}{
		{"empty", "", 0, true},
		{"single-file", "5\t2\tcmd/main.go\n", 7, true},
		{"two-files", "5\t2\ta.go\n10\t0\tb.go\n", 17, true},
		{"binary-skipped", "-\t-\timg.png\n3\t1\tc.go\n", 4, true},
		{"all-binary", "-\t-\ta.bin\n-\t-\tb.bin\n", 0, true},
		{"trailing-whitespace", "  4\t4\tx.go  \n", 8, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := daemon.ExportedSumNumstatLines(tc.input)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("sumNumstatLines(%q): want (%d,%v), got (%d,%v)", tc.input, tc.want, tc.wantOK, got, ok)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// budget-kill marker
// ─────────────────────────────────────────────────────────────────────────────

// TestQuitOnReviewFile_BudgetKillWritesSentinel verifies that when the verdict
// budget elapses without review.json appearing, /quit is sent, Kill fires, AND a
// budget-kill marker is written that ReadReviewerBudgetSentinel can read back.
// The temp worktree is not a git repo, so worktreeDiffLineCount returns -1 and
// the base budget applies — exactly the reproduce surface for the original bug.
func TestQuitOnReviewFile_BudgetKillWritesSentinel(t *testing.T) {
	restore := hksah87SetBudget(
		30*time.Millisecond, // base (the effective budget for an unknown diff)
		5*time.Minute,       // perKLine (unused — diff unknown)
		1*time.Hour,         // hardCeiling (won't be hit)
		5*time.Millisecond,  // poll
		15*time.Millisecond, // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()
	// Do NOT write review.json — the budget path fires.
	qs := &hksah87QuitSender{}
	kl := &hksah87Killer{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (budget kill), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (budget kill), got %d", got)
	}

	present, reason, budgetMS, elapsedMS, changedLines, err := daemon.ExportedReadReviewerBudgetSentinelFields(wtPath)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinel: %v", err)
	}
	if !present {
		t.Fatal("budget-kill marker absent; want present after budget kill")
	}
	if reason != "budget-exceeded" {
		t.Errorf("marker reason: want %q, got %q", "budget-exceeded", reason)
	}
	if budgetMS != (30 * time.Millisecond).Milliseconds() {
		t.Errorf("marker budget_ms: want %d, got %d", (30 * time.Millisecond).Milliseconds(), budgetMS)
	}
	if elapsedMS <= 0 {
		t.Errorf("marker elapsed_ms: want > 0, got %d", elapsedMS)
	}
	if changedLines != -1 {
		t.Errorf("marker changed_lines: want -1 (non-git tmpdir), got %d", changedLines)
	}
}

// TestQuitOnReviewFile_NoMarkerWhenVerdictWritten verifies that the happy path
// (verdict appears before the budget) does NOT write a budget-kill marker.
func TestQuitOnReviewFile_NoMarkerWhenVerdictWritten(t *testing.T) {
	restore := hksah87SetBudget(
		10*time.Second,     // base (long — won't fire)
		5*time.Minute,      // perKLine
		1*time.Hour,        // hardCeiling
		5*time.Millisecond, // poll
		1*time.Hour,        // noChangeKillDelay (budget path won't fire)
	)
	defer restore()
	// Short postQuitKillGrace so the verdict-path Kill fires within the test.
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedPostQuitKillGrace = 40 * time.Millisecond
	defer func() { *daemon.ExportedPostQuitKillGrace = origPostQuit }()

	wtPath := t.TempDir()
	qs := &hksah87QuitSender{}
	kl := &hksah87Killer{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil)
	}()

	time.Sleep(20 * time.Millisecond)
	hksah87WriteVerdict(t, wtPath)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("function did not return after verdict written")
	}

	present, _, _, _, _, err := daemon.ExportedReadReviewerBudgetSentinelFields(wtPath)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinel: %v", err)
	}
	if present {
		t.Error("budget-kill marker present on the verdict-detected path; want absent")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// pane-liveness extension + hard ceiling
// ─────────────────────────────────────────────────────────────────────────────

// TestQuitOnReviewFile_PaneLivenessExtendsBudget is the reproduce-and-fix test
// for the heavy-review case: a reviewer pane that is genuinely active past the
// base budget must NOT be killed within a window several times the base budget,
// as long as it stays under the hard ceiling.
//
// BEFORE the fix (flat reviewFileTimeout, no liveness): the kill fired at the
// base budget regardless of pane activity → a Kill would be observed → FAIL.
// AFTER the fix: the active pane extends the deadline each tick → no Kill.
func TestQuitOnReviewFile_PaneLivenessExtendsBudget(t *testing.T) {
	restore := hksah87SetBudget(
		30*time.Millisecond, // base budget
		5*time.Minute,       // perKLine
		5*time.Second,       // hardCeiling (far away)
		5*time.Millisecond,  // poll
		10*time.Millisecond, // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()
	qs := &hksah87LivenessQuitSender{}
	qs.alive.Store(true) // pane is actively working
	kl := &hksah87Killer{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil)
	}()

	// Observe for ~6x the base budget — an active pane must survive (no kill).
	time.Sleep(180 * time.Millisecond)
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill: want 0 (active pane extends budget), got %d", got)
	}
	if got := qs.quitCalls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 (active pane extends budget), got %d", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("function did not return after ctx cancel")
	}
}

// TestQuitOnReviewFile_HardCeilingKillsActivePane verifies the firm backstop: a
// reviewer pane that stays active forever (the hk-m5axg hung-reviewer shape) is
// still killed once the hard ceiling elapses, with marker reason "hard-ceiling".
func TestQuitOnReviewFile_HardCeilingKillsActivePane(t *testing.T) {
	restore := hksah87SetBudget(
		20*time.Millisecond, // base budget
		5*time.Minute,       // perKLine
		60*time.Millisecond, // hardCeiling (close — fires under test)
		5*time.Millisecond,  // poll
		10*time.Millisecond, // noChangeKillDelay
	)
	defer restore()

	wtPath := t.TempDir()
	qs := &hksah87LivenessQuitSender{}
	qs.alive.Store(true) // pane is "active" forever (hung reviewer)
	kl := &hksah87Killer{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil)

	if got := qs.quitCalls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (hard ceiling kill), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (hard ceiling kill), got %d", got)
	}

	present, reason, _, _, _, err := daemon.ExportedReadReviewerBudgetSentinelFields(wtPath)
	if err != nil {
		t.Fatalf("ReadReviewerBudgetSentinel: %v", err)
	}
	if !present {
		t.Fatal("budget-kill marker absent after hard-ceiling kill")
	}
	if reason != "hard-ceiling" {
		t.Errorf("marker reason: want %q, got %q", "hard-ceiling", reason)
	}
}
