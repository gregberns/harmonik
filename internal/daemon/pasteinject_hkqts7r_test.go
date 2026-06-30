package daemon_test

// pasteinject_hkqts7r_test.go — unit tests for the valid-complete-verdict gate in
// pasteInjectQuitOnReviewFile (hk-qts7r, codename:remote-hardening).
//
// Root cause being guarded: the verdict-detect branch used to /quit + force-kill
// claude the instant <wtPath>/.harmonik/review.json merely EXISTED (an
// existence-only stat). On a REMOTE worker claude's Write tool creates the file
// before flush/close, so the watchdog killed claude MID-WRITE → the worker's
// review.json was permanently truncated → the daemon's later SSH read failed
// ErrMalformed. The fix parses the file (ReadReviewVerdictVia) and only kills on a
// fully VALID, COMPLETE verdict.
//
// These tests drive the LOCAL path (verdictRunner nil → os.ReadFile + parse), which
// deterministically exercises the same gate logic without a real remote. The three
// cases mirror the three gate outcomes:
//   (a) PARTIAL/truncated review.json (ErrMalformed) → does NOT /quit+kill.
//   (c) ABSENT review.json                          → does NOT /quit+kill.
//   (b) COMPLETE valid review.json                  → DOES /quit+kill.
//
// Helper prefix: hkqts7r.
// Bead: hk-qts7r.

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

// hkqts7rQuitSender records SendQuitToLastPane calls.
type hkqts7rQuitSender struct {
	calls atomic.Int64
}

func (q *hkqts7rQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.calls.Add(1)
	return nil
}

// hkqts7rKiller records Kill calls.
type hkqts7rKiller struct {
	calls atomic.Int64
}

func (k *hkqts7rKiller) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkqts7rWriteReview writes content to <wtPath>/.harmonik/review.json.
func hkqts7rWriteReview(t *testing.T, wtPath, content string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hkqts7rWriteReview: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("hkqts7rWriteReview: write: %v", err)
	}
}

// hkqts7rLongBudgetTimeouts sets a long verdict budget (so the budget-kill path
// does NOT fire during the observation window) and a fast poll. Returns a restore
// function — call defer restore() immediately.
func hkqts7rLongBudgetTimeouts(t *testing.T) {
	t.Helper()
	origReviewTimeout := *daemon.ExportedReviewFileTimeout
	origReviewPoll := *daemon.ExportedReviewFilePollInterval
	origKillDelay := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedReviewFileTimeout = 30 * time.Second // budget won't fire in-window
	*daemon.ExportedReviewFilePollInterval = 5 * time.Millisecond
	*daemon.ExportedNoChangeKillDelay = 1 * time.Hour
	*daemon.ExportedPostQuitKillGrace = 20 * time.Millisecond
	t.Cleanup(func() {
		*daemon.ExportedReviewFileTimeout = origReviewTimeout
		*daemon.ExportedReviewFilePollInterval = origReviewPoll
		*daemon.ExportedNoChangeKillDelay = origKillDelay
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestQuitOnReviewFile_PartialVerdict_DoesNotKill verifies that a PARTIAL /
// truncated review.json (which parses to ErrMalformed) does NOT trigger the
// verdict-detect /quit+kill. Before the fix this file would have existed → kill
// mid-write. After the fix the gate keeps polling (the budget path, set far in the
// future here, would handle a genuinely-stuck reviewer).
func TestQuitOnReviewFile_PartialVerdict_DoesNotKill(t *testing.T) {
	hkqts7rLongBudgetTimeouts(t)

	wtPath := t.TempDir()
	// Truncated JSON: valid head, abrupt end → json: unexpected end of input.
	hkqts7rWriteReview(t, wtPath, `{"schema_version":1,"verdict":"APP`)

	qs := &hkqts7rQuitSender{}
	kl := &hkqts7rKiller{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)
	}()

	// Observe several poll ticks — the partial file must NOT trigger a kill.
	time.Sleep(120 * time.Millisecond)
	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 (partial verdict must not kill mid-write), got %d", got)
	}
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill: want 0 (partial verdict must not kill mid-write), got %d", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("goroutine did not exit after ctx cancel")
	}
}

// TestQuitOnReviewFile_AbsentVerdict_DoesNotKill verifies that an ABSENT
// review.json does NOT trigger the verdict-detect /quit+kill (the budget path,
// set far in the future here, owns the no-verdict case).
func TestQuitOnReviewFile_AbsentVerdict_DoesNotKill(t *testing.T) {
	hkqts7rLongBudgetTimeouts(t)

	wtPath := t.TempDir() // no review.json
	qs := &hkqts7rQuitSender{}
	kl := &hkqts7rKiller{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)
	}()

	time.Sleep(120 * time.Millisecond)
	if got := qs.calls.Load(); got != 0 {
		t.Errorf("SendQuitToLastPane: want 0 (absent verdict must not kill), got %d", got)
	}
	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill: want 0 (absent verdict must not kill), got %d", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("goroutine did not exit after ctx cancel")
	}
}

// TestQuitOnReviewFile_CompleteVerdict_Kills verifies that a COMPLETE, valid
// review.json DOES trigger the verdict-detect /quit and the post-grace Kill.
func TestQuitOnReviewFile_CompleteVerdict_Kills(t *testing.T) {
	hkqts7rLongBudgetTimeouts(t)

	wtPath := t.TempDir()
	qs := &hkqts7rQuitSender{}
	kl := &hkqts7rKiller{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.ExportedPasteInjectQuitOnReviewFile(ctx, qs, kl, nil, "", wtPath, nil, nil, 0)
	}()

	// Write a complete valid verdict after the goroutine is polling.
	time.Sleep(20 * time.Millisecond)
	hkqts7rWriteReview(t, wtPath, `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pasteInjectQuitOnReviewFile did not return after a complete verdict")
	}

	if got := qs.calls.Load(); got != 1 {
		t.Errorf("SendQuitToLastPane: want 1 (complete verdict detected), got %d", got)
	}
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill: want 1 (post-grace kill on complete verdict), got %d", got)
	}
}
