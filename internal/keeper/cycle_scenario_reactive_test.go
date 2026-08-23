package keeper_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

func reactiveSIDs() (s1, s2 string) {
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

	rs := newReactiveSession(s1, s2, true, true)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		500*time.Millisecond, // handoffTimeout
		300*time.Millisecond, // clearSettle
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

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

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("want 1 handoff_started; got %d", n)
	}

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

	if managedBinding != s2 {
		t.Errorf("managed-session port binding = %q; want %q (S2)", managedBinding, s2)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Errorf("want 0 cycle_parked on the happy path; got %d", n)
	}

	if !rs.sawClear() {
		t.Fatal("/clear was never injected — cannot have caused the SID flip")
	}
	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip was caused by %q; want exactly \"/clear\" (flip must be CAUSED by /clear, not temporal)", cause)
	}
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared in the gauge BEFORE /clear was injected — flip was not caused by /clear")
	}
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
	if handoffIdx >= clearIdx || clearIdx >= briefIdx {
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
	t.Skip("keeper-checkpoint-handshake: timeout abort is retired; pending-request scenarios replace this claim")
	t.Parallel()

	const (
		agent   = "reactive-abort-agent"
		cycleID = "cyc-reactive-abort-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

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

	for i, txt := range rs.snapshotInjected() {
		if txt == "/clear" {
			t.Errorf("inject[%d] == %q: /clear must NEVER be issued before nonce confirmation", i, txt)
		}
	}
	if rs.sawClear() {
		t.Error("harness recorded a /clear injection on the abort path — safety violation")
	}

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

	if rs.liveSID() != s1 {
		t.Errorf("gauge SID = %q after abort; want %q (S1 — never rotated)", rs.liveSID(), s1)
	}
}

// TestKeeperCycle_NonceTimeoutButFreshHandoff_Recovers is the hk-fi78d regression:
// the nonce echo is WITHHELD (writeNonce=false) so the poll times out, BUT the
// agent still WROTE a fresh handoff (writeHandoffNoNonce=true). The brief
// injection must SURVIVE the ack timeout: the cycle recovers and drives /clear +
// briefRestartCmd rather than blindly aborting before /clear.
//
// Asserts:
//   - /clear IS injected (a).
//   - briefRestartCmd (`--wake keeper-restart`) IS injected (b).
//   - the cycle is NOT a blind abort: NO cycle_aborted; cycle_complete emitted;
//     a distinct cycle_recovered event is emitted; journal ends "complete" with
//     reason "handoff_timeout_recovered" (c).
//   - the SID still flips S1->S2, CAUSED by /clear (d).
func TestKeeperCycle_NonceTimeoutButFreshHandoff_Recovers(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-recover-agent"
		cycleID = "cyc-reactive-recover-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	rs := newReactiveSession(s1, s2, false /*writeNonce*/, true /*flipOnClear*/)
	rs.writeHandoffNoNonce = true

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		40*time.Millisecond,  // handoffTimeout — poll must time out
		300*time.Millisecond, // clearSettle — reached on the recovery path
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	inj := rs.snapshotInjected()

	if !rs.sawClear() {
		t.Fatalf("/clear was NOT injected on the recovery path; injected=%v", inj)
	}

	clearIdx, briefIdx := -1, -1
	for i, txt := range inj {
		switch {
		case clearIdx == -1 && txt == "/clear":
			clearIdx = i
		case briefIdx == -1 && containsSubstr(txt, "agent brief") && containsSubstr(txt, "keeper-restart"):
			briefIdx = i
		}
	}
	if briefIdx == -1 {
		t.Fatalf("briefRestartCmd (--wake keeper-restart) was NOT injected; injected=%v", inj)
	}
	if clearIdx == -1 || clearIdx >= briefIdx {
		t.Errorf("injection order wrong: clear=%d brief=%d; want clear<brief (%v)", clearIdx, briefIdx, inj)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Errorf("want 0 cycle_parked on the recovery path; got %d", n)
	}
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete on recovery; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != s1 || cp.NewSessionID != s2 {
		t.Errorf("cycle_complete prev=%q new=%q; want prev=%q new=%q", cp.PrevSessionID, cp.NewSessionID, s1, s2)
	}
	recEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)
	if len(recEvts) != 1 {
		t.Fatalf("want 1 cycle_recovered on the recovery path; got %d", len(recEvts))
	}
	var rp core.SessionKeeperCycleRecoveredPayload
	if err := json.Unmarshal(recEvts[0].Payload, &rp); err != nil {
		t.Fatalf("unmarshal cycle_recovered: %v", err)
	}
	if rp.PhaseAtCrash != "handoff_timeout" {
		t.Errorf("cycle_recovered.phase_at_crash = %q; want \"handoff_timeout\"", rp.PhaseAtCrash)
	}
	phases := jc.snapshot()
	if last := phases[len(phases)-1]; last != "complete" {
		t.Errorf("last journal phase = %q; want \"complete\" (full=%v)", last, phases)
	}
	if lj := jc.lastJournal(); lj == nil {
		t.Error("no last journal captured")
	} else if lj.Reason != "handoff_timeout_recovered" {
		t.Errorf("journal reason = %q; want \"handoff_timeout_recovered\"", lj.Reason)
	}

	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip caused by %q; want \"/clear\"", cause)
	}
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared in the gauge BEFORE /clear on the recovery path")
	}
	if rs.liveSID() != s2 {
		t.Errorf("live gauge SID = %q after recovery; want %q (S2)", rs.liveSID(), s2)
	}
	if managedBinding != s2 {
		t.Errorf("managed-session port binding = %q; want %q (S2)", managedBinding, s2)
	}
}
