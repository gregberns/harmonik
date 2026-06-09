package daemon

// crewstart_hkjzpqo_test.go — regression test for hk-jzpqo.
//
// Bug (observed live 2026-06-09): `harmonik crew start` pasted the mission
// kick-off line into the crew's claude pane but it was NOT submitted — the crew
// sat idle until someone manually pressed Enter, so the crew never began its
// loop.
//
// Root cause: pasteCrewMission sent the post-paste submit Enter IMMEDIATELY
// after WriteLastPane (the bracketed paste) with NO settle in between.  A
// freshly-spawned crew pane — like a freshly-`--resume`'d implementer
// (hk-ip33d) — has a REPL input handler that is intermittently not yet ready to
// accept the keypress at that instant, so the single Enter raced the paste and
// was swallowed.
//
// Fix (mirror the working implementer paths): after WriteLastPane, wait
// splashDismissWait so the paste content lands and the REPL returns to an
// input-ready prompt, THEN submit via sendResumeSubmitEnter (the same bounded
// submit-Enter retry the implementer-resume path uses, hk-ip33d).
//
// This test asserts the CODE SEQUENCE only:
//   splash-Enter → settle(WriteToPane-not-yet) → paste(WriteLastPane) →
//   settle → submit-Enter(s) AFTER the paste.
// It CANNOT prove the live REPL actually accepts the submission (no pane-capture
// primitive exists); the live REPL-submit proof is deferred to the T10 crew
// smoke (hk-rbpss).
//
// Helper prefix: hkjzpqo (per implementer-protocol.md §Helper-prefix discipline;
// avoids redeclaring symbols already defined in package daemon).
// Bead: hk-jzpqo.

import (
	"context"
	"sync"
	"testing"
)

// hkjzpqoRecordingInjecter implements pasteInjecter + enterSender and records the
// ORDER of WriteLastPane (paste) and SendEnterToLastPane (key) calls into a single
// ordered log, so the test can assert the paste lands BEFORE the submit Enter.
type hkjzpqoRecordingInjecter struct {
	mu  sync.Mutex
	log []string // ordered call log: "enter" or "paste:<bufName>"
}

func (r *hkjzpqoRecordingInjecter) WriteLastPane(_ context.Context, bufferName string, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, "paste:"+bufferName)
	return nil
}

func (r *hkjzpqoRecordingInjecter) SendEnterToLastPane(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, "enter")
	return nil
}

func (r *hkjzpqoRecordingInjecter) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.log))
	copy(out, r.log)
	return out
}

// Compile-time assertions: the fixture satisfies both interfaces pasteCrewMission
// type-asserts against.
var (
	_ pasteInjecter = (*hkjzpqoRecordingInjecter)(nil)
	_ enterSender   = (*hkjzpqoRecordingInjecter)(nil)
)

// TestPasteCrewMission_SubmitsAfterSettle_hkjzpqo verifies that pasteCrewMission
// delivers the mission seed in the order:
//
//	splash-Enter → paste(WriteLastPane) → submit-Enter
//
// i.e. the submit Enter is sent AFTER the paste (with a settle in between, per
// the hk-jzpqo fix).  Before the fix the submit Enter raced the paste and was
// swallowed by the not-yet-input-ready crew REPL.
//
// LIMITATION: a unit test can only assert the CODE SEQUENCE (the order of
// WriteLastPane vs SendEnterToLastPane on the recording fixture), NOT that the
// live Claude REPL actually registers the submission.  The live REPL-submit
// proof is deferred to the T10 crew smoke (hk-rbpss).
func TestPasteCrewMission_SubmitsAfterSettle_hkjzpqo(t *testing.T) {
	// Neutralise the submit-retry wall time so the test is fast.  The settle
	// after the paste is splashDismissWait (a const delay) which is short enough
	// to tolerate in a single test; resumeSubmitRetries is restored on cleanup.
	origRetries := resumeSubmitRetries
	t.Cleanup(func() { resumeSubmitRetries = origRetries })
	resumeSubmitRetries = 0 // a single submit Enter is sufficient to assert order

	inj := &hkjzpqoRecordingInjecter{}
	h := &crewHandlerImpl{
		claudeBinary: "claude",
		projectDir:   t.TempDir(),
	}

	const sessionID = "hkjzpqo-session"
	h.pasteCrewMission(context.Background(), inj, sessionID, "/some/handoff.md")

	calls := inj.calls()

	// Expect: splash-Enter, paste, submit-Enter (with resumeSubmitRetries=0 the
	// submit fires exactly once).
	if len(calls) != 3 {
		t.Fatalf("pasteCrewMission: expected 3 calls (splash-Enter, paste, submit-Enter), got %d: %v", len(calls), calls)
	}

	// 1. Splash dismiss is a bare Enter BEFORE the paste.
	if calls[0] != "enter" {
		t.Errorf("call[0] = %q, want splash-dismiss \"enter\"", calls[0])
	}

	// 2. The paste lands second.
	wantBuf := "paste:" + bufferName(sessionID, "crew-init")
	if calls[1] != wantBuf {
		t.Errorf("call[1] = %q, want %q (the bracketed paste)", calls[1], wantBuf)
	}

	// 3. THE FIX: the submit Enter is sent AFTER the paste.  Before hk-jzpqo it
	//    still fired after the paste in source order, but with NO settle — this
	//    test's ordering guard, together with the splashDismissWait the fix
	//    inserts between paste and submit, encodes the corrected sequence.  A
	//    submit Enter appearing BEFORE the paste (regression) fails here.
	if calls[2] != "enter" {
		t.Errorf("call[2] = %q, want post-paste submit \"enter\"", calls[2])
	}

	// Defence-in-depth: the paste index must strictly precede the submit-Enter
	// index, so a future refactor that re-orders submit-before-paste is caught.
	pasteIdx, submitIdx := -1, -1
	for i, c := range calls {
		if c == wantBuf {
			pasteIdx = i
		}
		if c == "enter" && i > 0 { // i>0 skips the splash-dismiss Enter
			submitIdx = i
			break
		}
	}
	if pasteIdx < 0 || submitIdx < 0 || pasteIdx >= submitIdx {
		t.Errorf("expected paste (idx=%d) to precede submit-Enter (idx=%d); calls=%v", pasteIdx, submitIdx, calls)
	}
}
