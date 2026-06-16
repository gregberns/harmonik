package daemon_test

// pasteinject_hkjimbc_test.go — unit tests for pasteInjectQuitOnReviewFile
// (hk-jimbc).
//
// pasteInjectQuitOnReviewFile mirrors pasteInjectQuitOnCommit but watches for
// <wtPath>/.harmonik/review.json instead of a new git commit.  Three paths:
//
//  1. Verdict-detected: review.json appears (non-empty) → /quit sent,
//     postQuitKillGrace waited, Kill called, function returns.
//  2. Timeout: reviewFileTimeout fires without a verdict file → /quit sent,
//     noChangeKillDelay waited, Kill called, function returns.
//  3. Brief-gate: briefDelivered is respected before polling begins.
//     3a. Gate blocks: briefDelivered not yet closed → no /quit even if file
//         already exists.
//     3b. Gate timeout: briefDeliveredTimeout fires without closure → poll
//         starts anyway (liveness guarantee).
//
// Helper prefix: hkjimbc.
// Bead: hk-jimbc.

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkjimbcQuitSender records SendQuitToLastPane calls.
type hkjimbcQuitSender struct {
	calls atomic.Int64
}

func (q *hkjimbcQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hkjimbcKiller records Kill calls.
type hkjimbcKiller struct {
	calls atomic.Int64
}

func (k *hkjimbcKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkjimbcShortTimeouts overrides timing vars and returns a restore function.
// Call defer restore() immediately.
//
// postQuitKillGrace is always set explicitly so callers can control whether
// the verdict-path kill fires quickly or is deferred.
func hkjimbcShortTimeouts(reviewTimeout, reviewPoll, killDelay, postQuit time.Duration) func() {
	origReviewTimeout := *daemon.ExportedReviewFileTimeout
	origReviewPoll := *daemon.ExportedReviewFilePollInterval
	origKillDelay := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedReviewFileTimeout = reviewTimeout
	*daemon.ExportedReviewFilePollInterval = reviewPoll
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = postQuit
	return func() {
		*daemon.ExportedReviewFileTimeout = origReviewTimeout
		*daemon.ExportedReviewFilePollInterval = origReviewPoll
		*daemon.ExportedNoChangeKillDelay = origKillDelay
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// hkjimbcWriteVerdict writes a non-empty review.json into <wtPath>/.harmonik/.
func hkjimbcWriteVerdict(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hkjimbcWriteVerdict: mkdir: %v", err)
	}
	content := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("hkjimbcWriteVerdict: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInjectQuitOnReviewFile_VerdictDetected verifies that when
// review.json appears with non-zero size, /quit is sent and Kill is called
// after postQuitKillGrace.
func TestPasteInjectQuitOnReviewFile_VerdictDetected(t *testing.T) {
	// short poll so we find the file quickly; long timeout so it doesn't fire;
	// short postQuitKillGrace so Kill fires synchronously within the function.
	// noChangeKillDelay=1h so the timeout kill path never fires.
	restore := hkjimbcShortTimeouts(
		10*time.Second,      // reviewFileTimeout (long — won't fire)
		5*time.Millisecond,  // reviewFilePollInterval
		1*time.Hour,         // noChangeKillDelay (timeout path — won't fire)
		40*time.Millisecond, // postQuitKillGrace
	)
	defer restore()

	wtPath := t.TempDir()
	qs := &hkjimbcQuitSender{}
	kl := &hkjimbcKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)
	}()

	// Write verdict after a short delay so the goroutine is already polling.
	time.Sleep(20 * time.Millisecond)
	hkjimbcWriteVerdict(t, wtPath)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pasteInjectQuitOnReviewFile did not return after verdict detected")
	}

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls (postQuitKillGrace path): want 1, got %d", got)
	}
}

// TestPasteInjectQuitOnReviewFile_TimeoutSendsQuitAndKills verifies that when
// reviewFileTimeout elapses without review.json appearing, /quit is sent once
// and Kill is called once (after noChangeKillDelay).
func TestPasteInjectQuitOnReviewFile_TimeoutSendsQuitAndKills(t *testing.T) {
	// short review timeout and kill delay; postQuitKillGrace=1h so the verdict
	// path kill doesn't fire (no file → verdict path is never hit anyway).
	restore := hkjimbcShortTimeouts(
		30*time.Millisecond, // reviewFileTimeout
		5*time.Millisecond,  // reviewFilePollInterval
		20*time.Millisecond, // noChangeKillDelay
		1*time.Hour,         // postQuitKillGrace
	)
	defer restore()

	wtPath := t.TempDir()
	// Intentionally do NOT write review.json.
	qs := &hkjimbcQuitSender{}
	kl := &hkjimbcKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Runs synchronously: timeout fires, /quit sent, kill delay waited, Kill called.
	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane calls: want 1, got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls (timeout path): want 1, got %d", got)
	}
}

// TestPasteInjectQuitOnReviewFile_BriefGateBlocksUntilClosed verifies that
// when briefDelivered is NOT closed, the poll loop does not start even if
// review.json already exists — the gate is blocking.
func TestPasteInjectQuitOnReviewFile_BriefGateBlocksUntilClosed(t *testing.T) {
	// briefDeliveredTimeout = 2s (long) so the gate doesn't time out during
	// the 100ms observation window.
	origBrief := *daemon.ExportedBriefDeliveredTimeout
	*daemon.ExportedBriefDeliveredTimeout = 2 * time.Second
	defer func() { *daemon.ExportedBriefDeliveredTimeout = origBrief }()

	restore := hkjimbcShortTimeouts(
		10*time.Second,     // reviewFileTimeout (long)
		5*time.Millisecond, // reviewFilePollInterval
		1*time.Hour,        // noChangeKillDelay
		1*time.Hour,        // postQuitKillGrace
	)
	defer restore()

	wtPath := t.TempDir()
	// Write verdict BEFORE calling the function — gate must still block it.
	hkjimbcWriteVerdict(t, wtPath)

	qs := &hkjimbcQuitSender{}

	// briefDelivered channel that is never closed — gate must stay blocking.
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, nil, nil, "", wtPath, briefDelivered, nil, 0)
	}()

	// Observe for 100ms — brief gate is blocking (timeout=2s), so /quit must be 0.
	time.Sleep(100 * time.Millisecond)

	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 (brief not yet delivered, gate blocking), got %d", got)
	}

	// Cancel ctx to unblock the goroutine via ctx.Done() in the gate select.
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("goroutine did not exit after ctx cancel")
	}
}

// TestPasteInjectQuitOnReviewFile_BriefDeliveredTimeoutProceedsWithPoll
// verifies that when briefDeliveredTimeout elapses without briefDelivered
// closing, the poll loop starts anyway (liveness guarantee: a broken session
// must not block the daemon indefinitely).
//
// After the brief-gate timeout, the review timeout fires (no verdict file),
// which sends /quit and calls Kill.
func TestPasteInjectQuitOnReviewFile_BriefDeliveredTimeoutProceedsWithPoll(t *testing.T) {
	// Short briefDeliveredTimeout so the gate fires quickly.
	origBrief := *daemon.ExportedBriefDeliveredTimeout
	*daemon.ExportedBriefDeliveredTimeout = 30 * time.Millisecond
	defer func() { *daemon.ExportedBriefDeliveredTimeout = origBrief }()

	// After brief timeout: short review timeout so the no-verdict path fires.
	restore := hkjimbcShortTimeouts(
		80*time.Millisecond, // reviewFileTimeout
		5*time.Millisecond,  // reviewFilePollInterval
		15*time.Millisecond, // noChangeKillDelay
		1*time.Hour,         // postQuitKillGrace
	)
	defer restore()

	wtPath := t.TempDir()
	// Do NOT write review.json — no-verdict timeout path fires after brief-gate timeout.
	qs := &hkjimbcQuitSender{}
	kl := &hkjimbcKiller{}

	// briefDelivered that is never closed.
	briefDelivered := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Synchronous call: brief-gate timeout (30ms) → poll starts → review
	// timeout (80ms) fires → /quit sent → noChangeKillDelay (15ms) → Kill.
	daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, briefDelivered, nil, 0)

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (poll started after brief-gate timeout), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (after noChangeKillDelay), got %d", got)
	}
}

// TestPasteInjectQuitOnReviewFile_CtxCancelExitsCleanly verifies that
// cancelling the context causes the function to return without sending /quit
// or calling Kill — daemon shutdown must not emit spurious /quit into panes.
func TestPasteInjectQuitOnReviewFile_CtxCancelExitsCleanly(t *testing.T) {
	// Long intervals so no path fires before ctx cancel.
	origBrief := *daemon.ExportedBriefDeliveredTimeout
	*daemon.ExportedBriefDeliveredTimeout = 5 * time.Second
	defer func() { *daemon.ExportedBriefDeliveredTimeout = origBrief }()

	restore := hkjimbcShortTimeouts(
		10*time.Second,      // reviewFileTimeout
		50*time.Millisecond, // reviewFilePollInterval
		1*time.Hour,         // noChangeKillDelay
		1*time.Hour,         // postQuitKillGrace
	)
	defer restore()

	wtPath := t.TempDir()
	qs := &hkjimbcQuitSender{}
	kl := &hkjimbcKiller{}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so ctx.Done() fires on the first select iteration.
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("function did not return after ctx cancel")
	}

	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 on ctx cancel, got %d", got)
	}
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill: want 0 on ctx cancel, got %d", got)
	}
}
