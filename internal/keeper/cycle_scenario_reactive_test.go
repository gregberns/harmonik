package keeper_test

// cycle_scenario_reactive_test.go — scenario tests built on the reactive harness
// (cycle_reactive_harness_test.go). Unlike the call-count fakes in cycle_test.go,
// these drive the cycle through a session fake that MUTATES gauge + handoff state
// in response to the injected command, so the clear->session-id-flip is CAUSED by
// /clear rather than faked on a fixed gauge call-count.
//
// Fast offline unit tests — NO build tag.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// reactiveSIDs returns the seed (S1) and post-clear (S2) session ids used by the
// reactive scenarios. S2 MUST be a UUIDv4 (version nibble at index 14 == '4')
// so the cycle's waitForNewSessionID accepts it — it rejects UUIDv7 ids written
// by daemon-spawned implementers (Refs: hk-lap). S1 is a distinct UUIDv4.
func reactiveSIDs() (s1, s2 string) {
	// Index: 0123456789012345678901234567890123456
	//        xxxxxxxx-xxxx-Vxxx-Sxxx-xxxxxxxxxxxx   V = version nibble (idx 14)
	s1 = "11111111-1111-4111-8111-111111111111" // UUIDv4
	s2 = "22222222-2222-4222-8222-222222222222" // UUIDv4, distinct from S1
	return s1, s2
}

// TestKeeperCycle_FullReactiveCycle drives the complete happy-path cycle through
// the reactive harness and proves the post-clear session-id flip is CAUSED by the
// injected /clear (not faked on a call count).
//
// Asserts:
//   - journal phase progression: opened -> handoff_injected -> confirmed ->
//     cleared -> resumed -> complete.
//   - session_keeper_handoff_started emitted.
//   - session_keeper_cycle_complete emitted with prev_session_id==S1 AND
//     new_session_id==S2.
//   - the final .managed binding == S2.
//   - NO session_keeper_cycle_aborted.
//   - CAUSALITY: no new SID appears in the gauge until AFTER /clear is injected.
func TestKeeperCycle_FullReactiveCycle(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-full-agent"
		cycleID = "cyc-reactive-full-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	// writeNonce=true (handoff confirms), flipOnClear=true (/clear rotates SID).
	rs := newReactiveSession(s1, s2, true, true)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		500*time.Millisecond, // handoffTimeout
		300*time.Millisecond, // clearSettle
	)

	// Seed CtxFile is the live gauge (S1, over the act threshold).
	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) Journal phase progression.
	want := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	got := jc.snapshot()
	if len(got) != len(want) {
		t.Fatalf("journal phases = %v; want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("journal phase[%d] = %q; want %q (full=%v)", i, got[i], p, got)
		}
	}

	// (b) handoff_started emitted exactly once.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("want 1 handoff_started; got %d", n)
	}

	// (c) cycle_complete with prev==S1 AND new==S2.
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != s1 {
		t.Errorf("cycle_complete.prev_session_id = %q; want %q (S1)", cp.PrevSessionID, s1)
	}
	if cp.NewSessionID != s2 {
		t.Errorf("cycle_complete.new_session_id = %q; want %q (S2 — must be CAUSED by /clear)", cp.NewSessionID, s2)
	}

	// (d) .managed binding updated to S2.
	if managedBinding != s2 {
		t.Errorf("SetManagedSessionFn binding = %q; want %q (S2)", managedBinding, s2)
	}

	// (e) NO cycle_aborted on the happy path.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Errorf("want 0 cycle_aborted; got %d", n)
	}

	// (f) CAUSALITY — the SID flip must be CAUSED by /clear, not temporal.
	// (f1) /clear was actually injected.
	if !rs.sawClear() {
		t.Fatal("/clear was never injected — cannot have caused the SID flip")
	}
	// (f2) The exact command whose reaction rotated the SID must be "/clear".
	// This is the load-bearing causality witness: the SID changes INSIDE the
	// inject() that processes "/clear", so any other cause (e.g. a temporal flip
	// on /session-handoff) would be caught here.
	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip was caused by %q; want exactly \"/clear\" (flip must be CAUSED by /clear, not temporal)", cause)
	}
	// (f3) The harness never observed a non-seed SID in the gauge before /clear.
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared in the gauge BEFORE /clear was injected — flip was not caused by /clear")
	}
	// (f4) Injection ordering: handoff before /clear before agent brief (T8/I1),
	// and the live gauge ended on S2.
	inj := rs.snapshotInjected()
	handoffIdx, clearIdx, briefIdx := -1, -1, -1
	for i, txt := range inj {
		switch {
		case handoffIdx == -1 && containsSubstr(txt, "/session-handoff"):
			handoffIdx = i
		case clearIdx == -1 && txt == "/clear":
			clearIdx = i
		case briefIdx == -1 && containsSubstr(txt, "agent brief"):
			briefIdx = i
		}
	}
	if handoffIdx == -1 || clearIdx == -1 || briefIdx == -1 {
		t.Fatalf("missing injected commands: handoff=%d clear=%d brief=%d (%v)", handoffIdx, clearIdx, briefIdx, inj)
	}
	if !(handoffIdx < clearIdx && clearIdx < briefIdx) {
		t.Errorf("injection order wrong: handoff=%d clear=%d brief=%d; want handoff<clear<brief", handoffIdx, clearIdx, briefIdx)
	}
	if rs.liveSID() != s2 {
		t.Errorf("live gauge SID = %q after cycle; want %q (S2)", rs.liveSID(), s2)
	}
}

// TestKeeperCycle_NonceTimeoutAborts proves the load-bearing safety property:
// the keeper NEVER issues /clear for an unconfirmed handoff. The reactive harness
// is configured with writeNonce=false, so the /session-handoff reaction does NOT
// write the nonce; the cycle's nonce poll times out and the cycle ABORTS before
// /clear.
//
// Asserts:
//   - the cycle aborts: journal final phase == "aborted" with reason
//     "handoff_timeout".
//   - /clear is NEVER injected (the safety invariant).
//   - session_keeper_cycle_aborted emitted; cycle_complete NOT emitted.
//   - the gauge SID is never rotated (stays S1).
func TestKeeperCycle_NonceTimeoutAborts(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-abort-agent"
		cycleID = "cyc-reactive-abort-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	// writeNonce=false: handoff is injected but the nonce is never written ->
	// the poll cannot confirm -> abort. flipOnClear=true would only matter if
	// /clear were reached, which it must not be.
	rs := newReactiveSession(s1, s2, false /*writeNonce*/, true /*flipOnClear*/)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		40*time.Millisecond, // handoffTimeout — a few poll intervals (5ms each)
		30*time.Millisecond, // clearSettle (unreached)
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) Journal must end in "aborted" with reason "handoff_timeout".
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded")
	}
	if last := phases[len(phases)-1]; last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\" (full=%v)", last, phases)
	}
	if lj := jc.lastJournal(); lj == nil {
		t.Error("no last journal captured")
	} else if lj.Reason != "handoff_timeout" {
		t.Errorf("journal reason = %q; want \"handoff_timeout\"", lj.Reason)
	}

	// (b) THE SAFETY INVARIANT: /clear must NEVER be injected on an unconfirmed
	// handoff.
	for i, txt := range rs.snapshotInjected() {
		if txt == "/clear" {
			t.Errorf("inject[%d] == %q: /clear must NEVER be issued before nonce confirmation", i, txt)
		}
	}
	if rs.sawClear() {
		t.Error("harness recorded a /clear injection on the abort path — safety violation")
	}

	// (c) cycle_aborted emitted with reason handoff_timeout; cycle_complete NOT.
	abortedEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(abortedEvts) != 1 {
		t.Fatalf("want 1 cycle_aborted; got %d", len(abortedEvts))
	}
	var ap core.SessionKeeperCycleAbortedPayload
	if err := json.Unmarshal(abortedEvts[0].Payload, &ap); err != nil {
		t.Fatalf("unmarshal cycle_aborted: %v", err)
	}
	if ap.Reason != "handoff_timeout" {
		t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", ap.Reason)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete on abort; got %d", n)
	}

	// (d) The gauge SID was never rotated (flip is gated behind /clear, which
	// never ran).
	if rs.liveSID() != s1 {
		t.Errorf("gauge SID = %q after abort; want %q (S1 — never rotated)", rs.liveSID(), s1)
	}
}
