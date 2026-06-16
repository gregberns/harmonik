package daemon_test

// pasteinject_hk7rgqs_test.go — unit tests for the reviewer SEED-SUBMIT RACE fix
// (hk-7rgqs).
//
// Bug: under concurrent claude cold-boots the Claude Code splash takes >750ms to
// clear.  pasteInjectReviewer did splash-dismiss Enter → fixed splashDismissDelay
// (750ms) → paste brief → a SINGLE submit Enter.  Under load that single submit
// Enter landed while the splash was still up and was SWALLOWED, leaving the review
// brief typed-but-UNSUBMITTED → the reviewer idled, never read review-target.md,
// never wrote review.json → stall until the 30-min budget.  The implementer path
// self-heals (activity-aware re-detection); the reviewer's
// pasteInjectQuitOnReviewFile had no re-seed.
//
// Fix (both parts tested here):
//   (a) PREVENTION — pasteInjectReviewer (and pasteInjectImplementerInitial) now
//       send the post-paste submit Enter with the bounded retry the resume path
//       uses (sendSubmitEnterWithRetry), so at least one Enter lands after the
//       splash clears.
//   (b) SAFETY NET — pasteInjectQuitOnReviewFile re-seeds the reviewer brief ONCE
//       if no review.json appears within reviewerReseedGrace and the pane is still
//       active (brief typed but submit Enter swallowed), before the verdict budget
//       can fire.
//
// Helper prefix: hk7rgqs.
// Bead: hk-7rgqs.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hk7rgqsPasteStub implements the daemon's pasteInjecter + enterSender +
// quitSender + paneLivenessChecker interfaces structurally.  It records the
// WriteLastPane (brief) and SendEnterToLastPane (submit) call counts and the
// order in which paste vs Enter occurred, and lets the test control pane
// liveness.  Concurrency-safe: the watchdog goroutine and the test read it.
type hk7rgqsPasteStub struct {
	mu sync.Mutex
	// writeCount counts WriteLastPane (brief paste) calls.
	writeCount int
	// enterAfterPaste counts SendEnterToLastPane calls that occur AFTER at least
	// one paste — i.e. the SUBMIT Enters (not the pre-paste splash-dismiss Enter).
	enterAfterPaste int
	// sawPaste flips true once WriteLastPane has been called, so subsequent Enters
	// are classified as submit Enters.
	sawPaste bool
	// onWrite, when non-nil, is invoked (under the lock) on every WriteLastPane.
	// Tests use it to model "the re-seed landed → the reviewer now submits and
	// writes its verdict", making the re-seed→verdict path deterministic.
	onWrite func()

	quitCalls atomic.Int64
	alive     atomic.Bool
}

func (s *hk7rgqsPasteStub) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCount++
	s.sawPaste = true
	if s.onWrite != nil {
		s.onWrite()
	}
	return nil
}

func (s *hk7rgqsPasteStub) SendEnterToLastPane(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sawPaste {
		s.enterAfterPaste++
	}
	return nil
}

func (s *hk7rgqsPasteStub) SendQuitToLastPane(_ context.Context) error {
	s.quitCalls.Add(1)
	return nil
}

func (s *hk7rgqsPasteStub) PaneHasActiveProcess(_ context.Context) bool {
	return s.alive.Load()
}

func (s *hk7rgqsPasteStub) snapshot() (writes, submitEnters int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeCount, s.enterAfterPaste
}

// Compile-time assertions that the stub satisfies the exported interface aliases.
var (
	_ daemon.PasteInjecterExported       = (*hk7rgqsPasteStub)(nil)
	_ daemon.EnterSenderExported         = (*hk7rgqsPasteStub)(nil)
	_ daemon.PaneLivenessCheckerExported = (*hk7rgqsPasteStub)(nil)
)

// hk7rgqsKiller records Kill calls.
type hk7rgqsKiller struct {
	calls atomic.Int64
}

func (k *hk7rgqsKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hk7rgqsWriteReviewTarget writes a non-empty review-target.md so
// pasteInjectReviewer's stat check passes.
func hk7rgqsWriteReviewTarget(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hk7rgqsWriteReviewTarget: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-target.md"), []byte("# Review target\n"), 0o644); err != nil {
		t.Fatalf("hk7rgqsWriteReviewTarget: write: %v", err)
	}
}

// hk7rgqsWriteVerdict writes a non-empty review.json into <wtPath>/.harmonik/.
func hk7rgqsWriteVerdict(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hk7rgqsWriteVerdict: mkdir: %v", err)
	}
	content := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("hk7rgqsWriteVerdict: write: %v", err)
	}
}

// hk7rgqsFastSplash shrinks splashDismissDelay + the submit-retry delay so the
// paste-inject helpers run quickly, and returns a restore function.
func hk7rgqsFastSplash(t *testing.T) {
	t.Helper()
	origSplash := *daemon.ExportedSplashDismissDelay
	origRetryDelay := *daemon.ExportedResumeSubmitRetryDelay
	*daemon.ExportedSplashDismissDelay = 1 * time.Millisecond
	*daemon.ExportedResumeSubmitRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() {
		*daemon.ExportedSplashDismissDelay = origSplash
		*daemon.ExportedResumeSubmitRetryDelay = origRetryDelay
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// (a) PREVENTION — robust submit: pasteInjectReviewer sends multiple submit Enters
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectReviewer_RobustSubmitSendsMultipleEnters verifies that the
// reviewer kick-off sends the post-paste submit Enter MORE THAN ONCE (the bounded
// retry), so a single swallowed Enter (splash still up under load) no longer
// leaves the brief unsubmitted.  Pre-fix, exactly one submit Enter was sent.
func TestPasteInjectReviewer_RobustSubmitSendsMultipleEnters(t *testing.T) {
	hk7rgqsFastSplash(t)
	// Pin retries ≥1 so the retry actually runs (this is the fix; with 0 retries
	// only a single submit Enter would fire — the pre-fix behaviour).
	if *daemon.ExportedResumeSubmitRetries < 1 {
		t.Fatalf("precondition: resumeSubmitRetries must be ≥1 for the robust submit to retry; got %d",
			*daemon.ExportedResumeSubmitRetries)
	}

	wtPath := t.TempDir()
	hk7rgqsWriteReviewTarget(t, wtPath)

	stub := &hk7rgqsPasteStub{}

	reason := daemon.ExportedPasteInjectReviewer(t.Context(), stub, "01hwxyz-rev7rgqs", wtPath)
	if reason != "" {
		t.Fatalf("pasteInjectReviewer returned failure reason: %q", reason)
	}

	writes, submitEnters := stub.snapshot()
	if writes != 1 {
		t.Errorf("reviewer brief: want 1 WriteLastPane (paste) call, got %d", writes)
	}
	// The fix sends 1 initial submit Enter + resumeSubmitRetries retries.
	wantMinEnters := 1 + *daemon.ExportedResumeSubmitRetries
	if submitEnters < 2 {
		t.Errorf("hk-7rgqs REGRESSION: only %d post-paste submit Enter(s) sent; the bounded retry did not fire, "+
			"so a single swallowed Enter would leave the review brief unsubmitted", submitEnters)
	}
	if submitEnters != wantMinEnters {
		t.Errorf("reviewer submit Enters: want %d (1 initial + %d retries), got %d",
			wantMinEnters, *daemon.ExportedResumeSubmitRetries, submitEnters)
	}
}

// TestPasteInjectImplementerInitial_RobustSubmitSendsMultipleEnters verifies the
// same robust-submit hardening was applied to the implementer-initial path.
func TestPasteInjectImplementerInitial_RobustSubmitSendsMultipleEnters(t *testing.T) {
	hk7rgqsFastSplash(t)
	if *daemon.ExportedResumeSubmitRetries < 1 {
		t.Fatalf("precondition: resumeSubmitRetries must be ≥1; got %d", *daemon.ExportedResumeSubmitRetries)
	}

	wtPath := t.TempDir()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-task.md"), []byte("# Task\nDo it.\n"), 0o644); err != nil {
		t.Fatalf("write agent-task.md: %v", err)
	}

	stub := &hk7rgqsPasteStub{}

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-impl7rgqs", wtPath)
	if reason != "" {
		t.Fatalf("pasteInjectImplementerInitial returned failure reason: %q", reason)
	}

	_, submitEnters := stub.snapshot()
	if submitEnters < 2 {
		t.Errorf("hk-7rgqs REGRESSION: implementer-initial sent only %d post-paste submit Enter(s); "+
			"the bounded retry did not fire", submitEnters)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (b) SAFETY NET — re-seed on stall in pasteInjectQuitOnReviewFile
// ─────────────────────────────────────────────────────────────────────────────

// TestQuitOnReviewFile_ReseedsOnceWhenStalledAndPaneActive verifies that when no
// review.json appears within reviewerReseedGrace AND the pane is still active, the
// watchdog re-seeds the reviewer brief EXACTLY ONCE (one additional WriteLastPane)
// before the budget logic would fire.  The verdict is written shortly after the
// re-seed, so the loop then exits via the verdict path (no kill).
func TestQuitOnReviewFile_ReseedsOnceWhenStalledAndPaneActive(t *testing.T) {
	hk7rgqsFastSplash(t)

	// Short re-seed grace; budget long enough that the kill path is NOT the thing
	// under test (we want the re-seed → verdict path).
	restoreBudget := hksah87SetBudget(
		10*time.Second,     // base budget (won't elapse in this test)
		5*time.Minute,      // perKLine (diff unknown → base applies)
		1*time.Hour,        // hard ceiling
		5*time.Millisecond, // poll
		5*time.Millisecond, // noChangeKillDelay
	)
	defer restoreBudget()
	origGrace := *daemon.ExportedReviewerReseedGrace
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedReviewerReseedGrace = 20 * time.Millisecond
	*daemon.ExportedPostQuitKillGrace = 5 * time.Millisecond
	defer func() {
		*daemon.ExportedReviewerReseedGrace = origGrace
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}()

	wtPath := t.TempDir()
	hk7rgqsWriteReviewTarget(t, wtPath) // so the re-seed's pasteInjectReviewer stat passes

	stub := &hk7rgqsPasteStub{}
	stub.alive.Store(true) // pane active: brief typed but unsubmitted
	// Deterministic: the re-seed's WriteLastPane models the reviewer finally
	// submitting and writing its verdict, so the loop then exits via the verdict
	// path (no fixed-timer race against the grace).
	stub.onWrite = func() { hk7rgqsWriteVerdict(t, wtPath) }
	kl := &hk7rgqsKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, stub, kl, stub, "01hwxyz-rev7rgqs", wtPath, nil, nil, 0)

	writes, _ := stub.snapshot()
	// Exactly one re-seed paste (the watchdog calls pasteInjectReviewer once).
	if writes != 1 {
		t.Errorf("hk-7rgqs: want exactly 1 re-seed WriteLastPane (one-shot re-seed), got %d", writes)
	}
	// Verdict appeared → loop exited via the verdict path; the budget kill must NOT
	// be the reason.  A SendQuit fires on the verdict path too, so we assert the
	// verdict was honoured by checking no budget sentinel was written.
	if sentinel, err := daemon.ReadReviewerBudgetSentinel(wtPath); err != nil {
		t.Fatalf("ReadReviewerBudgetSentinel: %v", err)
	} else if sentinel != nil {
		t.Errorf("hk-7rgqs: budget-kill sentinel written (%+v) — re-seed→verdict path should not budget-kill", sentinel)
	}
}

// TestQuitOnReviewFile_ReseedsOnlyOnce verifies the once-only semantics: even when
// the reviewer never produces a verdict and the pane stays active across MANY
// poll ticks, the brief is re-seeded at most once (not on every tick).  The loop
// ends via the budget kill after the re-seed.
func TestQuitOnReviewFile_ReseedsOnlyOnce(t *testing.T) {
	hk7rgqsFastSplash(t)

	// Short HARD CEILING so the budget kill fires even though the (active) pane
	// keeps extending the base budget — otherwise an always-active stub would
	// extend to the hard ceiling and the test would run that long.
	restoreBudget := hksah87SetBudget(
		20*time.Millisecond,  // base budget
		5*time.Minute,        // perKLine (diff unknown → base applies)
		120*time.Millisecond, // hard ceiling — the firm backstop that kills the active pane
		5*time.Millisecond,   // poll
		5*time.Millisecond,   // noChangeKillDelay
	)
	defer restoreBudget()
	origGrace := *daemon.ExportedReviewerReseedGrace
	*daemon.ExportedReviewerReseedGrace = 10 * time.Millisecond
	defer func() { *daemon.ExportedReviewerReseedGrace = origGrace }()

	wtPath := t.TempDir()
	hk7rgqsWriteReviewTarget(t, wtPath)

	stub := &hk7rgqsPasteStub{}
	stub.alive.Store(true) // active forever; never writes a verdict
	kl := &hk7rgqsKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, stub, kl, stub, "01hwxyz-rev7rgqs", wtPath, nil, nil, 0)

	writes, _ := stub.snapshot()
	if writes != 1 {
		t.Errorf("hk-7rgqs: re-seed must be one-shot; want exactly 1 re-seed WriteLastPane across many ticks, got %d", writes)
	}
	// The reviewer never produced a verdict → budget kill eventually fires.
	if kl.calls.Load() == 0 {
		t.Errorf("hk-7rgqs: expected a budget kill after the one-shot re-seed failed to produce a verdict")
	}
}

// TestQuitOnReviewFile_NoReseedWhenPaneDead verifies that a DEAD pane is NOT
// re-seeded (re-seeding a dead pane cannot help) — the watchdog leaves it to the
// budget path.  No WriteLastPane (re-seed) should occur.
func TestQuitOnReviewFile_NoReseedWhenPaneDead(t *testing.T) {
	hk7rgqsFastSplash(t)

	restoreBudget := hksah87SetBudget(
		40*time.Millisecond,
		5*time.Minute,
		1*time.Hour,
		5*time.Millisecond,
		5*time.Millisecond,
	)
	defer restoreBudget()
	origGrace := *daemon.ExportedReviewerReseedGrace
	*daemon.ExportedReviewerReseedGrace = 15 * time.Millisecond
	defer func() { *daemon.ExportedReviewerReseedGrace = origGrace }()

	wtPath := t.TempDir()
	hk7rgqsWriteReviewTarget(t, wtPath)

	stub := &hk7rgqsPasteStub{}
	stub.alive.Store(false) // pane is dead
	kl := &hk7rgqsKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, stub, kl, stub, "01hwxyz-rev7rgqs", wtPath, nil, nil, 0)

	writes, _ := stub.snapshot()
	if writes != 0 {
		t.Errorf("hk-7rgqs: a dead pane must NOT be re-seeded; want 0 re-seed WriteLastPane, got %d", writes)
	}
}
