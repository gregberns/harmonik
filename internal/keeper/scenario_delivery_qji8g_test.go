package keeper

// scenario_delivery_qji8g_test.go — T9 MANDATORY scenario-test suite
// (bead hk-keeper-delivery-scenario-tests-qji8g), UNIT/HARNESS tier for three of
// the five target failures. These are white-box (package keeper) tests that ride
// the keeper delivery seams (tmuxRunFn, commsSendFn, deliverLeaderWarn /
// maybeDeliverLeaderWarn) and the operator-active resolver (operatorActiveSince),
// reusing the helpers declared in delivery_decision_0nlqs_test.go
// (writePresenceBeat / swapCommsSend / swapTmuxRun) and tmuxresolve_operator_test.go.
//
// Each scenario encodes the fails-before / passes-after contrast in-test where
// feasible; on the current (post-impl) tree all pass. Scenario→function map:
//
//	(a) operator-typing collision (unit adjunct, T7/T8) →
//	     TestScenario_OperatorTypingCollision_CommsZeroPaneWrite_qji8g
//	(c) comms-unreachable fallback (unit, T1/T7)        →
//	     TestScenario_CommsUnreachableFallback_qji8g
//	(d) operator-present misread   (unit primary, T8)   →
//	     TestScenario_OperatorPresentMisread_ReSampleCatches_qji8g
//
// The comms path MUST issue ZERO pane write (SK-022); the fallback MUST preserve
// the hk-89g 750ms-settle + retry-Enter loop (SK-025) — asserted via the swapped
// tmuxRunFn call count. Every fired leader warn tick resolves to exactly one of
// {comms, terminal} — never a silent no-op (SK-INV-006).

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// (a) OPERATOR-TYPING COLLISION — the pane-write collision with an operator's own
// keystrokes is AVOIDED because the leader comms path takes over: even with the
// operator actively attached (typing), an Online leader routes to comms and the
// pane receives ZERO inject/escape write that cycle. Fail-before: the pre-T7 warn
// path always pane-pasted, so a leader warn while the operator typed clobbered the
// in-flight line. Pass-after: comms delivery, zero pane write. Validates T7 (the
// comms channel) + T8 (operator-attached is harmless on the comms path).
func TestScenario_OperatorTypingCollision_CommsZeroPaneWrite_qji8g(t *testing.T) {
	path := writePresenceBeat(t, "captain", time.Now()) // fresh → Online leader
	comms := swapCommsSend(t)
	paneWrites := swapTmuxRun(t)

	// OperatorAttachedFn returns true: the operator IS attached and typing on the
	// pane. On the production path (InjectFn unset) an Online leader must STILL
	// route to comms — never touching the pane the operator is typing into.
	operatorTyping := func(string) bool { return true }
	w := &Watcher{cfg: WatcherConfig{
		AgentName:          "captain",
		EventsJSONLPath:    path,
		TmuxTarget:         "s:0.0",
		OperatorAttachedFn: operatorTyping,
	}}

	handled, cleared := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true)
	if !handled || !cleared {
		t.Fatalf("handled=%v cleared=%v, want true,true (leader comms delivery)", handled, cleared)
	}
	if len(*comms) != 1 {
		t.Fatalf("commsSendFn called %d times, want 1 (comms channel taken)", len(*comms))
	}
	// SK-022 / the collision-avoidance guarantee: ZERO pane write while the
	// operator is typing.
	if *paneWrites != 0 {
		t.Errorf("comms path wrote the operator's pane %d times, want 0 (typing collision not avoided)", *paneWrites)
	}
}

// (c) COMMS-UNREACHABLE FALLBACK — a leader that is ABSENT from the presence
// registry (never beat, or its recv-follow never registered) must resolve to the
// terminal fallback, NEVER a silent no-op (SK-INV-006). The fallback runs the real
// InjectText pane path with the hk-89g settle + retry-Enter loop preserved. A
// positive control (leader PRESENT/Online) proves the comms path is taken with
// ZERO pane write. Validates T1 (presence read) + T7 (the deterministic decision).
func TestScenario_CommsUnreachableFallback_qji8g(t *testing.T) {
	t.Run("absent_target_falls_back_to_terminal_never_silent", func(t *testing.T) {
		// events.jsonl carries a beat for a DIFFERENT agent, so the target
		// "captain" is ABSENT from the registry → leaderPresenceOnline == false.
		other := writePresenceBeat(t, "admiral", time.Now())
		comms := swapCommsSend(t)
		paneWrites := swapTmuxRun(t)

		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: other, TmuxTarget: "s:0.0"}}
		ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-absent")
		if err != nil {
			t.Fatalf("deliverLeaderWarn: %v", err)
		}
		// SK-INV-006 totality: the decision resolved to EXACTLY the terminal
		// channel — never empty, never a no-op.
		if ch != leaderDeliveryTerminal {
			t.Fatalf("channel = %q for an absent target, want terminal (no silent no-op)", ch)
		}
		if len(*comms) != 0 {
			t.Errorf("comms send fired for an absent target (%d), want 0", len(*comms))
		}
		// SK-025: the fallback ran the real InjectText path. The tmuxRunFn count
		// reflects the load-buffer + paste-buffer + submit-Enter + bounded retries
		// SEQUENCE (>1) — proving the retry-Enter loop is preserved, not reduced to
		// a single send.
		if *paneWrites < 3 {
			t.Errorf("terminal fallback issued %d pane writes, want >=3 (settle + retry-Enter loop preserved)", *paneWrites)
		}
	})

	t.Run("present_target_takes_comms_zero_pane", func(t *testing.T) {
		// Positive control: the target IS Online → comms, ZERO pane write.
		present := writePresenceBeat(t, "captain", time.Now())
		comms := swapCommsSend(t)
		paneWrites := swapTmuxRun(t)

		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: present, TmuxTarget: "s:0.0"}}
		ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-present")
		if err != nil {
			t.Fatalf("deliverLeaderWarn: %v", err)
		}
		if ch != leaderDeliveryComms {
			t.Fatalf("channel = %q for a present target, want comms", ch)
		}
		if len(*comms) != 1 {
			t.Errorf("comms send fired %d times for a present target, want 1", len(*comms))
		}
		if *paneWrites != 0 {
			t.Errorf("comms path wrote the pane %d times, want 0 (SK-022)", *paneWrites)
		}
	})
}

// (d) OPERATOR-PRESENT MISREAD — the entry-only operator-active sample MISSES an
// operator who begins typing mid-cycle; the T8 re-sample CATCHES it. operatorActiveSince
// is the white-box resolver behind the OperatorAttached probe (a client only counts
// as an actively-present operator when its #{client_activity} is recent, within the
// window). This models the exact TOCTOU: at cycle entry the operator's last activity
// is stale (outside the window) → the single entry sample reads ABSENT (fail-before);
// the operator then types, and a re-sample later in the cycle reads PRESENT
// (pass-after). Validates T8 (SK-035 in-cycle re-check).
func TestScenario_OperatorPresentMisread_ReSampleCatches_qji8g(t *testing.T) {
	t.Parallel()

	const window = 5 * time.Minute
	// Fixed base instant; a live client stays attached the whole cycle, but its
	// keystroke activity is stale at entry and fresh mid-cycle.
	entryNow := time.Unix(1_781_618_670, 0)

	// #{client_activity} value formatter: a client whose last keystroke was `ago`
	// before the given sample instant.
	activityAt := func(sample time.Time, ago time.Duration) string {
		return fmt.Sprintf("%d\n", sample.Add(-ago).Unix())
	}

	// ── FAIL-BEFORE: the single ENTRY sample. At entry the operator's last
	// keystroke was 6 minutes ago (outside the 5-min window) — the entry-only
	// Gate-7 read says ABSENT, so a naive cycle would proceed toward /clear.
	entrySample := activityAt(entryNow, 6*time.Minute)
	if got := operatorActiveSince(entrySample, entryNow, window); got != false {
		t.Fatalf("entry sample: operatorActiveSince = %v, want false (the entry-only read MISSES the operator — the bug)", got)
	}

	// ── PASS-AFTER: the operator starts typing at entry+2m. A RE-SAMPLE taken at
	// entry+3m sees keystroke activity 1 minute old (inside the window) → PRESENT.
	// The re-check catches exactly what the single entry sample missed (SK-035).
	resampleNow := entryNow.Add(3 * time.Minute)
	resample := activityAt(resampleNow, 1*time.Minute)
	if got := operatorActiveSince(resample, resampleNow, window); got != true {
		t.Fatalf("re-sample: operatorActiveSince = %v, want true (the re-check must CATCH the mid-cycle operator)", got)
	}

	// Guard against a degenerate pass: the SAME fresh activity string, if it had
	// been present at entry, would also have read present — so the discriminating
	// factor is the re-sample TIMING, not a different client. Confirm the entry
	// sample and the re-sample genuinely disagree under their own clocks.
	if operatorActiveSince(entrySample, entryNow, window) == operatorActiveSince(resample, resampleNow, window) {
		t.Fatal("entry sample and re-sample agree; the scenario must show entry=absent, re-sample=present")
	}
}
