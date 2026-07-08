package daemon_test

// pasteinject_hkzexsj_test.go — unit tests for the seed-paste land-verification +
// re-paste loop (hk-zexsj).
//
// Bug: on a REMOTE SSH worker under concurrent cold-boots, ~1/3 of runs hang
// because the seed paste (`tmux load-buffer` + `tmux paste-buffer`, bracketed
// paste) is silently lost — tmux returns exit 0 once it hands the buffer to the
// pane, NOT once claude's React/ink TUI has rendered it.  When the TUI reaches
// input-ready later than the blind 750ms splash wait, the paste lands on a
// not-ready TUI and is discarded; claude idles at an empty prompt and the run
// burns the 30-min timeout before failing.
//
// Fix: after WriteLastPane injects the seed (and before the submit Enter),
// injectAndVerifySeed captures the pane and checks for a stable marker from the
// seed text.  If the marker is absent it re-runs the paste (bounded retry); if
// every attempt fails it returns a non-empty failure reason so pasteinject_failed
// fires and the run fails loud/fast.
//
// Helper prefix: hkzexsj.
// Bead: hk-zexsj.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkzexsjPasteStub implements pasteInjecter + enterSender + paneCapturer.  It
// records WriteLastPane calls and models a seed paste that is silently dropped
// on the first (landOnAttempt-1) attempts and finally renders on landOnAttempt:
// CaptureLastPane returns a pane containing seedText only once writeCount has
// reached landOnAttempt (landOnAttempt == 0 ⇒ the seed never lands).
type hkzexsjPasteStub struct {
	mu            sync.Mutex
	writeCount    int
	captureCount  int
	landOnAttempt int    // marker first appears once writeCount >= this; 0 = never
	seedText      string // rendered into the fake pane once landed
}

func (s *hkzexsjPasteStub) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCount++
	return nil
}

func (s *hkzexsjPasteStub) SendEnterToLastPane(_ context.Context) error { return nil }

func (s *hkzexsjPasteStub) CaptureLastPane(_ context.Context, _ int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captureCount++
	if s.landOnAttempt > 0 && s.writeCount >= s.landOnAttempt {
		return "❯ " + s.seedText, nil
	}
	return "❯ ", nil // empty input box: paste was discarded
}

func (s *hkzexsjPasteStub) snapshot() (writes, captures int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeCount, s.captureCount
}

// hkzexsjNoCaptureStub implements pasteInjecter + enterSender but NOT
// paneCapturer — modelling a minimal substrate/test-double.  The verify loop
// must fall back to trusting the first successful paste (prior behaviour).
type hkzexsjNoCaptureStub struct {
	mu         sync.Mutex
	writeCount int
}

func (s *hkzexsjNoCaptureStub) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCount++
	return nil
}
func (s *hkzexsjNoCaptureStub) SendEnterToLastPane(_ context.Context) error { return nil }
func (s *hkzexsjNoCaptureStub) writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeCount
}

// Compile-time assertions.
var (
	_ daemon.PasteInjecterExported = (*hkzexsjPasteStub)(nil)
	_ daemon.PaneCapturerExported  = (*hkzexsjPasteStub)(nil)
	_ daemon.EnterSenderExported   = (*hkzexsjPasteStub)(nil)
	_ daemon.PasteInjecterExported = (*hkzexsjNoCaptureStub)(nil)
	_ daemon.EnterSenderExported   = (*hkzexsjNoCaptureStub)(nil)
)

// hkzexsjFastTimings shrinks every wall-time knob the verify path touches so the
// unit tests run instantly.
func hkzexsjFastTimings(t *testing.T) {
	t.Helper()
	origSplash := daemon.ExportedSplashDismissDelay()
	origRetry := daemon.ExportedResumeSubmitRetryDelay()
	origBackoff := daemon.ExportedPasteVerifyBackoff()
	daemon.ExportedSetSplashDismissDelay(1 * time.Millisecond)
	daemon.ExportedSetResumeSubmitRetryDelay(1 * time.Millisecond)
	daemon.ExportedSetPasteVerifyBackoff(1 * time.Millisecond)
	t.Cleanup(func() {
		daemon.ExportedSetSplashDismissDelay(origSplash)
		daemon.ExportedSetResumeSubmitRetryDelay(origRetry)
		daemon.ExportedSetPasteVerifyBackoff(origBackoff)
	})
}

// hkzexsjWriteTaskFile writes a non-empty agent-task.md so the
// implementer-initial stat check passes.
func hkzexsjWriteTaskFile(t *testing.T, wtPath string) {
	t.Helper()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("hkzexsjWriteTaskFile: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-task.md"), []byte("# Task\nDo it.\n"), 0o644); err != nil {
		t.Fatalf("hkzexsjWriteTaskFile: write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPasteInject_RetriesPasteUntilSeedLands verifies the core fix: when the
// FIRST capture shows no seed marker (paste discarded by a not-ready TUI), the
// paste is RE-RUN, and once the marker appears the helper succeeds (no failure
// reason).
func TestPasteInject_RetriesPasteUntilSeedLands(t *testing.T) {
	hkzexsjFastTimings(t)
	if *daemon.ExportedPasteVerifyAttempts < 2 {
		t.Fatalf("precondition: pasteVerifyAttempts must be ≥2 for the re-paste to run; got %d",
			*daemon.ExportedPasteVerifyAttempts)
	}

	wtPath := t.TempDir()
	hkzexsjWriteTaskFile(t, wtPath)

	// Seed is dropped on attempt 1, lands on attempt 2.
	stub := &hkzexsjPasteStub{landOnAttempt: 2, seedText: "Please read .harmonik/agent-task.md and begin."}

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-implzexsj", wtPath)
	if reason != "" {
		t.Fatalf("expected success once the seed lands on attempt 2, got failure reason: %q", reason)
	}

	writes, captures := stub.snapshot()
	if writes != 2 {
		t.Errorf("hk-zexsj: want exactly 2 WriteLastPane calls (one discarded paste + one re-paste), got %d", writes)
	}
	if captures < 2 {
		t.Errorf("hk-zexsj: want ≥2 CaptureLastPane verifications (one per paste attempt), got %d", captures)
	}
}

// TestPasteInject_FailsLoudWhenSeedNeverLands verifies that when the marker never
// appears, the helper exhausts pasteVerifyAttempts and returns a non-empty
// failure reason — so pasteInjectOnLaunch emits pasteinject_failed and the run
// fails fast instead of burning the 30-min timeout.
func TestPasteInject_FailsLoudWhenSeedNeverLands(t *testing.T) {
	hkzexsjFastTimings(t)

	wtPath := t.TempDir()
	hkzexsjWriteTaskFile(t, wtPath)

	stub := &hkzexsjPasteStub{landOnAttempt: 0} // never lands

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-implzexsj", wtPath)
	if reason == "" {
		t.Fatalf("hk-zexsj: expected a non-empty failure reason when the seed never lands")
	}
	if !strings.Contains(reason, "unverified") {
		t.Errorf("hk-zexsj: failure reason should mention 'unverified'; got %q", reason)
	}

	writes, _ := stub.snapshot()
	if want := *daemon.ExportedPasteVerifyAttempts; writes != want {
		t.Errorf("hk-zexsj: want %d paste attempts before giving up, got %d", want, writes)
	}
}

// TestPasteInject_NoCaptureCapabilityTrustsFirstPaste verifies backward
// compatibility: a substrate that cannot capture the pane (a minimal test
// double) trusts the first successful WriteLastPane — exactly one paste, no
// failure reason.
func TestPasteInject_NoCaptureCapabilityTrustsFirstPaste(t *testing.T) {
	hkzexsjFastTimings(t)

	wtPath := t.TempDir()
	hkzexsjWriteTaskFile(t, wtPath)

	stub := &hkzexsjNoCaptureStub{}

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-implzexsj", wtPath)
	if reason != "" {
		t.Fatalf("hk-zexsj: a non-capturing substrate must trust the first paste, got reason: %q", reason)
	}
	if w := stub.writes(); w != 1 {
		t.Errorf("hk-zexsj: a non-capturing substrate must paste exactly once, got %d", w)
	}
}

// TestPasteInjectReviewer_VerifiesReviewTargetMarker verifies the reviewer path
// uses its own marker ("review-target.md") and re-pastes when the first capture
// shows no marker.
func TestPasteInjectReviewer_VerifiesReviewTargetMarker(t *testing.T) {
	hkzexsjFastTimings(t)

	wtPath := t.TempDir()
	dir := filepath.Join(wtPath, ".harmonik")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-target.md"), []byte("# Review target\n"), 0o644); err != nil {
		t.Fatalf("write review-target.md: %v", err)
	}

	stub := &hkzexsjPasteStub{landOnAttempt: 2, seedText: "Read .harmonik/review-target.md in this worktree."}

	reason := daemon.ExportedPasteInjectReviewer(t.Context(), stub, "01hwxyz-revzexsj", wtPath)
	if reason != "" {
		t.Fatalf("expected reviewer success once the seed lands on attempt 2, got: %q", reason)
	}
	if writes, _ := stub.snapshot(); writes != 2 {
		t.Errorf("hk-zexsj: reviewer want 2 WriteLastPane calls (discarded + re-paste), got %d", writes)
	}
}
