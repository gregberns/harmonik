package keeper

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
		other := writePresenceBeat(t, "admiral", time.Now())
		comms := swapCommsSend(t)
		paneWrites := swapTmuxRun(t)

		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: other, TmuxTarget: "s:0.0"}}
		ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-absent")
		if err != nil {
			t.Fatalf("deliverLeaderWarn: %v", err)
		}
		if ch != leaderDeliveryTerminal {
			t.Fatalf("channel = %q for an absent target, want terminal (no silent no-op)", ch)
		}
		if len(*comms) != 0 {
			t.Errorf("comms send fired for an absent target (%d), want 0", len(*comms))
		}
		if *paneWrites < 3 {
			t.Errorf("terminal fallback issued %d pane writes, want >=3 (settle + retry-Enter loop preserved)", *paneWrites)
		}
	})

	t.Run("present_target_takes_comms_zero_pane", func(t *testing.T) {
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
	entryNow := time.Unix(1_781_618_670, 0)

	activityAt := func(sample time.Time, ago time.Duration) string {
		return fmt.Sprintf("%d\n", sample.Add(-ago).Unix())
	}

	entrySample := activityAt(entryNow, 6*time.Minute)
	if got := operatorActiveSince(entrySample, entryNow, window); got != false {
		t.Fatalf("entry sample: operatorActiveSince = %v, want false (the entry-only read MISSES the operator — the bug)", got)
	}

	resampleNow := entryNow.Add(3 * time.Minute)
	resample := activityAt(resampleNow, 1*time.Minute)
	if got := operatorActiveSince(resample, resampleNow, window); got != true {
		t.Fatalf("re-sample: operatorActiveSince = %v, want true (the re-check must CATCH the mid-cycle operator)", got)
	}

	if operatorActiveSince(entrySample, entryNow, window) == operatorActiveSince(resample, resampleNow, window) {
		t.Fatal("entry sample and re-sample agree; the scenario must show entry=absent, re-sample=present")
	}
}
