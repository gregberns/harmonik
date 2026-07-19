package daemon

// crewstart_hkdvcc7_test.go — regression test for hk-dvcc7.
//
// Bug: the crew boot seed was pasted into the pane but never submitted, so a
// fresh crew idled silently until someone manually pressed Enter. The prior
// hk-jzpqo fix added a settle before a BLIND submit Enter, but on a slow or
// concurrent cold-start the bracketed paste is still being absorbed when the
// (bounded) submit Enter fires — every retry lands inside the absorption window
// and is swallowed, leaving the seed typed-but-unsubmitted. The implementer and
// reviewer paths do not have this problem because they VERIFY the seed rendered
// (capture-pane, hk-zexsj) and re-paste before submitting; the crew path alone
// still used the blind WriteLastPane.
//
// Fix: pasteCrewMission now uses injectAndVerifySeed — it captures the pane and
// confirms the "/session-resume" marker rendered (re-pasting up to
// pasteVerifyAttempts) before the submit Enter fires. crewPasteInjector gained a
// CaptureLastPane so the verification actually runs on the live crew path.
//
// Helper prefix: hkdvcc7 (avoids redeclaring package-daemon symbols).

import (
	"context"
	"sync"
	"testing"
	"time"
)

// hkdvcc7VerifyInjecter implements pasteInjecter + enterSender + paneCapturer.
// CaptureLastPane returns a marker-bearing pane only once captureCount reaches
// markerAfter, modelling a seed that renders after a delay (or never, when
// markerAfter exceeds pasteVerifyAttempts).
type hkdvcc7VerifyInjecter struct {
	mu           sync.Mutex
	log          []string // ordered: "paste", "capture", "enter"
	pasteCount   int
	captureCount int
	markerAfter  int    // marker appears once captureCount >= markerAfter
	marker       string // the substring injectAndVerifySeed looks for
}

func (r *hkdvcc7VerifyInjecter) WriteLastPane(_ context.Context, _ string, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pasteCount++
	r.log = append(r.log, "paste")
	return nil
}

func (r *hkdvcc7VerifyInjecter) SendEnterToLastPane(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, "enter")
	return nil
}

func (r *hkdvcc7VerifyInjecter) CaptureLastPane(_ context.Context, _ int) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.captureCount++
	r.log = append(r.log, "capture")
	if r.captureCount >= r.markerAfter {
		return "input> Please read /h.md and run " + r.marker + " on it", nil
	}
	return "input> (empty prompt, paste not yet rendered)", nil
}

func (r *hkdvcc7VerifyInjecter) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.log))
	copy(out, r.log)
	return out
}

// Compile-time assertions: the fixture satisfies every interface pasteCrewMission
// / injectAndVerifySeed type-assert against.
var (
	_ pasteInjecter = (*hkdvcc7VerifyInjecter)(nil)
	_ enterSender   = (*hkdvcc7VerifyInjecter)(nil)
	_ paneCapturer  = (*hkdvcc7VerifyInjecter)(nil)
)

// hkdvcc7FastTimers neutralises the verify/submit wall-clock waits so the test
// runs instantly. Restores originals on cleanup.
func hkdvcc7FastTimers(t *testing.T) {
	t.Helper()
	origBackoff := pasteVerifyBackoffNs.Load()
	origSplash := splashDismissDelayNs.Load()
	origRetries := resumeSubmitRetries
	t.Cleanup(func() {
		pasteVerifyBackoffNs.Store(origBackoff)
		splashDismissDelayNs.Store(origSplash)
		resumeSubmitRetries = origRetries
	})
	pasteVerifyBackoffNs.Store(int64(time.Millisecond))
	splashDismissDelayNs.Store(int64(time.Millisecond))
	resumeSubmitRetries = 0 // one submit Enter is enough to assert order
}

// TestPasteCrewMission_VerifiesBeforeSubmit_hkdvcc7 asserts that when the seed
// does not render on the first capture, pasteCrewMission RE-PASTES and only
// submits the Enter after a capture confirms the marker landed — the submit is
// gated on the verified seed, not fired blind.
func TestPasteCrewMission_VerifiesBeforeSubmit_hkdvcc7(t *testing.T) {
	hkdvcc7FastTimers(t)

	inj := &hkdvcc7VerifyInjecter{markerAfter: 2, marker: "/session-resume"}
	h := &crewHandlerImpl{claudeBinary: "claude", projectDir: t.TempDir()}

	h.pasteCrewMission(context.Background(), inj, "hkdvcc7-session", "/h.md")

	// Marker absent on capture 1, present on capture 2 → exactly two pastes and
	// two captures before the submit.
	if inj.pasteCount < 2 {
		t.Errorf("hk-dvcc7 regression: expected the seed to be re-pasted until it rendered "+
			"(pasteCount>=2), got %d — the crew path is not re-pasting on a dropped seed", inj.pasteCount)
	}

	// The submit Enter (the LAST "enter" in the log) must come after the capture
	// that saw the marker, i.e. after the 2nd capture.
	calls := inj.calls()
	lastEnter, secondCapture, captures := -1, -1, 0
	for i, c := range calls {
		switch c {
		case "enter":
			lastEnter = i
		case "capture":
			captures++
			if captures == 2 {
				secondCapture = i
			}
		}
	}
	if secondCapture < 0 {
		t.Fatalf("expected at least 2 captures (marker landed on the 2nd); calls=%v", calls)
	}
	if lastEnter < secondCapture {
		t.Errorf("hk-dvcc7 regression: submit Enter (idx=%d) fired BEFORE the seed was verified "+
			"present (2nd capture idx=%d) — the submit is not gated on the rendered seed; calls=%v",
			lastEnter, secondCapture, calls)
	}
}

// TestPasteCrewMission_NoBlindSubmitWhenSeedNeverLands_hkdvcc7 is the crux: when
// the seed never renders (every capture marker-absent), pasteCrewMission must NOT
// send a submit Enter — the prior blind path fired an Enter regardless, which is
// exactly the swallowed-keypress footgun. Only the splash-dismiss Enter (before
// any paste) is allowed.
func TestPasteCrewMission_NoBlindSubmitWhenSeedNeverLands_hkdvcc7(t *testing.T) {
	hkdvcc7FastTimers(t)

	// markerAfter far beyond pasteVerifyAttempts → marker never appears.
	inj := &hkdvcc7VerifyInjecter{markerAfter: 999, marker: "/session-resume"}
	h := &crewHandlerImpl{claudeBinary: "claude", projectDir: t.TempDir()}

	h.pasteCrewMission(context.Background(), inj, "hkdvcc7-session", "/h.md")

	calls := inj.calls()

	// The only permitted Enter is the splash-dismiss keypress, which happens
	// BEFORE any paste. There must be no post-paste submit Enter.
	firstPaste := -1
	for i, c := range calls {
		if c == "paste" {
			firstPaste = i
			break
		}
	}
	if firstPaste < 0 {
		t.Fatalf("expected at least one paste attempt; calls=%v", calls)
	}
	for i, c := range calls {
		if c == "enter" && i > firstPaste {
			t.Errorf("hk-dvcc7 regression: a submit Enter fired at idx=%d AFTER the paste even though "+
				"the seed never rendered — the crew path is still blind-submitting; calls=%v", i, calls)
		}
	}

	// It should have exhausted the bounded re-paste attempts trying to land the seed.
	if inj.pasteCount != pasteVerifyAttempts {
		t.Errorf("expected %d paste attempts (pasteVerifyAttempts) when the seed never lands, got %d",
			pasteVerifyAttempts, inj.pasteCount)
	}
}
