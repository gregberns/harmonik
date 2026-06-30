package daemon_test

// pasteinject_hk89dye_test.go — unit tests for the capture-infra-failure vs
// marker-absent distinction in injectAndVerifySeed (hk-89dye).
//
// Follow-up from hk-zexsj review (reviewer-2): the verify loop treated a
// CaptureLastPane error (capErr != nil) the same as a marker-absent retry.  With
// ControlPath=none pinning each capture as a fresh SSH connection, 3 consecutive
// transient CAPTURE failures on a run whose paste (WriteLastPane) all succeeded
// would return pasteinject_failed and kill a run whose seed actually landed.
//
// Fix: track whether any capture ever succeeded (capErr == nil).  If every
// capture failed at the infra level while every paste write succeeded, trust the
// write (return "") rather than failing loud.  A genuine marker-absent (capture
// succeeded, marker not there) still fails loud, unchanged.
//
// Helper prefix: hk89dye.
// Bead: hk-89dye.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// hk89dyeCaptureErrStub implements pasteInjecter + enterSender + paneCapturer.
// WriteLastPane always succeeds (paste exit 0); CaptureLastPane always returns a
// transient infra error (models a churning ControlPath=none SSH connection that
// can't capture the pane).  failCaptures < 0 ⇒ every capture fails; otherwise
// the first failCaptures captures fail and subsequent ones succeed with seedText.
type hk89dyeCaptureErrStub struct {
	mu           sync.Mutex
	writeCount   int
	captureCount int
	failCaptures int    // -1 = always fail; N = first N fail then succeed
	seedText     string // rendered once captures start succeeding
}

func (s *hk89dyeCaptureErrStub) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeCount++
	return nil
}

func (s *hk89dyeCaptureErrStub) SendEnterToLastPane(_ context.Context) error { return nil }

func (s *hk89dyeCaptureErrStub) CaptureLastPane(_ context.Context, _ int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captureCount++
	if s.failCaptures < 0 || s.captureCount <= s.failCaptures {
		return "", errors.New("ssh: connection reset (ControlPath=none, transient)")
	}
	return "❯ " + s.seedText, nil
}

func (s *hk89dyeCaptureErrStub) snapshot() (writes, captures int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeCount, s.captureCount
}

// Compile-time assertions.
var (
	_ daemon.PasteInjecterExported = (*hk89dyeCaptureErrStub)(nil)
	_ daemon.PaneCapturerExported  = (*hk89dyeCaptureErrStub)(nil)
	_ daemon.EnterSenderExported   = (*hk89dyeCaptureErrStub)(nil)
)

// TestPasteInject_TrustsWriteWhenCaptureInfraFailsThroughout verifies the core
// hk-89dye fix: when EVERY CaptureLastPane fails at the infra level but every
// WriteLastPane (paste) succeeds, injectAndVerifySeed trusts the write and
// returns "" — it does NOT fail-loud a run whose seed very likely landed.
func TestPasteInject_TrustsWriteWhenCaptureInfraFailsThroughout(t *testing.T) {
	hkzexsjFastTimings(t)

	wtPath := t.TempDir()
	hkzexsjWriteTaskFile(t, wtPath)

	stub := &hk89dyeCaptureErrStub{failCaptures: -1} // every capture errors

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-impl89dye", wtPath)
	if reason != "" {
		t.Fatalf("hk-89dye: capture-infra failure on every attempt (paste exit-0) must trust the write, got failure reason: %q", reason)
	}

	writes, captures := stub.snapshot()
	if want := *daemon.ExportedPasteVerifyAttempts; writes != want {
		t.Errorf("hk-89dye: want %d paste attempts (capture retried across all attempts before trusting), got %d", want, writes)
	}
	if want := *daemon.ExportedPasteVerifyAttempts; captures != want {
		t.Errorf("hk-89dye: want %d capture attempts before falling back to trust-the-write, got %d", want, captures)
	}
}

// TestPasteInject_StillSucceedsWhenCaptureRecoversMidLoop verifies a transient
// capture error that clears on a later attempt still lands the marker normally
// (the infra hiccup self-heals, no trust-the-write fallback needed).
func TestPasteInject_StillSucceedsWhenCaptureRecoversMidLoop(t *testing.T) {
	hkzexsjFastTimings(t)
	if *daemon.ExportedPasteVerifyAttempts < 2 {
		t.Fatalf("precondition: pasteVerifyAttempts must be ≥2; got %d", *daemon.ExportedPasteVerifyAttempts)
	}

	wtPath := t.TempDir()
	hkzexsjWriteTaskFile(t, wtPath)

	// First capture errors; the second succeeds and shows the marker.
	stub := &hk89dyeCaptureErrStub{failCaptures: 1, seedText: "Please read .harmonik/agent-task.md and begin."}

	reason := daemon.ExportedPasteInjectImplementerInitial(t.Context(), stub, "01hwxyz-impl89dye", wtPath)
	if reason != "" {
		t.Fatalf("hk-89dye: a recovered capture that shows the marker must succeed, got: %q", reason)
	}
}
