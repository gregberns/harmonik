package keeper

// delivery_decision_0nlqs_test.go — T7 acceptance for the K1 leader-warn delivery
// decision (SK-022/023/024/025, SK-INV-006). Drives deliverLeaderWarn against the
// tmuxRunFn (pane-write) and commsSendFn seams:
//   - Leader + Online  → comms send fires, ZERO pane write, no --wake.
//   - Leader + Offline → terminal fallback (InjectText → tmuxRunFn), no comms send.
//   - comms failure    → terminal fallback, never a silent no-op (SK-INV-006).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// writePresenceBeat writes an events.jsonl with one agent_presence beat for agent
// at the given wall time, returning the path. Fresh (now) → Online; old (>10m) →
// Offline, per presence.GetState (real clock).
func writePresenceBeat(t *testing.T, agent string, ts time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	payload, err := json.Marshal(map[string]any{
		"agent": agent, "status": "online", "last_seen": ts.UTC().Format(time.RFC3339), "reason": "join",
	})
	if err != nil {
		t.Fatalf("writePresenceBeat: marshal payload: %v", err)
	}
	ev, err := json.Marshal(map[string]any{
		"event_id": "01986000-0000-7000-8000-000000000001", "schema_version": 1,
		"type": "agent_presence", "timestamp_wall": ts.UTC(), "source_subsystem": "test",
		"payload": json.RawMessage(payload),
	})
	if err != nil {
		t.Fatalf("writePresenceBeat: marshal event: %v", err)
	}
	if err := os.WriteFile(path, append(ev, '\n'), 0o600); err != nil {
		t.Fatalf("writePresenceBeat: %v", err)
	}
	return path
}

// swapCommsSend replaces commsSendFn for the test, recording calls, and restores it.
func swapCommsSend(t *testing.T) *[]struct{ agent, body string } {
	t.Helper()
	calls := &[]struct{ agent, body string }{}
	orig := commsSendFn
	commsSendFn = func(_ context.Context, agent, body string) error {
		*calls = append(*calls, struct{ agent, body string }{agent, body})
		return nil
	}
	t.Cleanup(func() { commsSendFn = orig })
	return calls
}

// swapTmuxRun replaces tmuxRunFn, recording invocations, and restores it (plus the
// settle/retry sleeps so the terminal path runs instantly).
func swapTmuxRun(t *testing.T) *int {
	t.Helper()
	count := new(int)
	origRun, origSettle, origRetry := tmuxRunFn, submitSettle, submitRetryDelay
	tmuxRunFn = func(_ context.Context, _ string, _ ...string) ([]byte, error) { *count++; return nil, nil }
	submitSettle, submitRetryDelay = 0, 0
	t.Cleanup(func() { tmuxRunFn, submitSettle, submitRetryDelay = origRun, origSettle, origRetry })
	return count
}

func TestDeliverLeaderWarn_OnlineLeader_CommsPathZeroPaneWrite(t *testing.T) {
	path := writePresenceBeat(t, "captain", time.Now()) // fresh → Online
	comms := swapCommsSend(t)
	paneWrites := swapTmuxRun(t)

	w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: path, TmuxTarget: "s:0.0"}}
	ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-abc")
	if err != nil {
		t.Fatalf("deliverLeaderWarn: %v", err)
	}
	if ch != leaderDeliveryComms {
		t.Fatalf("channel = %q, want comms", ch)
	}
	if len(*comms) != 1 {
		t.Fatalf("commsSendFn called %d times, want 1", len(*comms))
	}
	if (*comms)[0].agent != "captain" {
		t.Errorf("comms --to = %q, want captain", (*comms)[0].agent)
	}
	// Body carries the K2 defer template + the restart-now command with the nonce.
	if !strings.Contains((*comms)[0].body, "harmonik keeper restart-now --agent captain --nonce cyc-abc") {
		t.Errorf("comms body missing nonce'd restart-now command:\n%s", (*comms)[0].body)
	}
	// SK-022: ZERO pane write on the comms path.
	if *paneWrites != 0 {
		t.Errorf("comms path wrote the pane %d times, want 0", *paneWrites)
	}
}

func TestDeliverLeaderWarn_OfflineLeader_TerminalFallback(t *testing.T) {
	path := writePresenceBeat(t, "captain", time.Now().Add(-20*time.Minute)) // stale → Offline
	comms := swapCommsSend(t)
	paneWrites := swapTmuxRun(t)

	w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: path, TmuxTarget: "s:0.0"}}
	ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-abc")
	if err != nil {
		t.Fatalf("deliverLeaderWarn: %v", err)
	}
	if ch != leaderDeliveryTerminal {
		t.Fatalf("channel = %q, want terminal", ch)
	}
	if len(*comms) != 0 {
		t.Errorf("comms send fired on the offline/terminal path (%d), want 0", len(*comms))
	}
	// The terminal fallback ran the InjectText pane path (retry-Enter loop preserved).
	if *paneWrites == 0 {
		t.Errorf("terminal fallback wrote the pane 0 times, want >0 (InjectText path)")
	}
}

func TestDeliverLeaderWarn_CommsFailure_FallsBackToTerminal(t *testing.T) {
	path := writePresenceBeat(t, "captain", time.Now()) // Online, but comms will fail
	origComms := commsSendFn
	commsSendFn = func(_ context.Context, _, _ string) error { return fmt.Errorf("daemon down") }
	t.Cleanup(func() { commsSendFn = origComms })
	paneWrites := swapTmuxRun(t)

	w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: path, TmuxTarget: "s:0.0"}}
	ch, err := w.deliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true, false, "cyc-abc")
	if err != nil {
		t.Fatalf("deliverLeaderWarn (comms-fail path): %v", err)
	}
	// SK-INV-006: a comms failure is NOT a silent no-op — it resolves to terminal.
	if ch != leaderDeliveryTerminal {
		t.Fatalf("channel = %q on comms failure, want terminal (no silent no-op)", ch)
	}
	if *paneWrites == 0 {
		t.Errorf("comms-failure fallback did not deliver via the terminal path")
	}
}

func TestCommsSendArgs_NoWake(t *testing.T) {
	args := commsSendArgs("captain", "the body")
	joined := strings.Join(args, " ")
	for _, want := range []string{"comms send", "--from keeper", "--to captain", "--topic keeper"} {
		if !strings.Contains(joined, want) {
			t.Errorf("commsSendArgs missing %q: %v", want, args)
		}
	}
	if strings.Contains(joined, "--wake") {
		t.Errorf("commsSendArgs must NEVER include --wake (SK-022): %v", args)
	}
	// Body is the final positional after the "--" terminator.
	if args[len(args)-1] != "the body" || args[len(args)-2] != "--" {
		t.Errorf("body not passed after -- terminator: %v", args)
	}
}

// TestMaybeDeliverLeaderWarn_Routing proves the Run-loop gate routes correctly:
// a leader on the production path (InjectFn unset) + Online → comms with ZERO pane
// write (handled+cleared); a leader + Offline → terminal (handled+cleared, pane
// written); an InjectFn-set leader and a crew both fall through un-handled so the
// existing pane path is preserved.
func TestMaybeDeliverLeaderWarn_Routing(t *testing.T) {
	onlinePath := writePresenceBeat(t, "captain", time.Now())
	offlinePath := writePresenceBeat(t, "captain", time.Now().Add(-20*time.Minute))
	noOp := func(string) bool { return false }

	t.Run("leader online -> comms, handled, zero pane write", func(t *testing.T) {
		comms := swapCommsSend(t)
		pane := swapTmuxRun(t)
		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: onlinePath, TmuxTarget: "s:0.0", OperatorAttachedFn: noOp}}
		handled, cleared := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true)
		if !handled || !cleared {
			t.Fatalf("handled=%v cleared=%v, want true,true", handled, cleared)
		}
		if len(*comms) != 1 || *pane != 0 {
			t.Errorf("comms=%d pane=%d, want comms=1 pane=0", len(*comms), *pane)
		}
	})

	t.Run("leader offline -> terminal, handled, pane written", func(t *testing.T) {
		comms := swapCommsSend(t)
		pane := swapTmuxRun(t)
		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: offlinePath, TmuxTarget: "s:0.0", OperatorAttachedFn: noOp}}
		handled, cleared := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true)
		if !handled || !cleared {
			t.Fatalf("handled=%v cleared=%v, want true,true", handled, cleared)
		}
		if len(*comms) != 0 || *pane == 0 {
			t.Errorf("comms=%d pane=%d, want comms=0 pane>0 (terminal fallback)", len(*comms), *pane)
		}
	})

	t.Run("InjectFn-set leader -> not handled (existing pane path)", func(t *testing.T) {
		w := &Watcher{cfg: WatcherConfig{AgentName: "captain", EventsJSONLPath: onlinePath,
			InjectFn: func(context.Context, string) error { return nil }}}
		if handled, _ := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true); handled {
			t.Errorf("InjectFn-set leader was handled by the T7 gate; want fall-through to the pane path")
		}
	})

	t.Run("crew -> not handled", func(t *testing.T) {
		w := &Watcher{cfg: WatcherConfig{AgentName: "delta", EventsJSONLPath: onlinePath, TmuxTarget: "s:0.0"}}
		if handled, _ := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true); handled {
			t.Errorf("crew was handled by the T7 leader gate; crew must keep the existing path")
		}
	})
}

func TestMintCycleID_FallbackFormat(t *testing.T) {
	// No Cycler → one-shot generator over the config clock; id is a cyc- id.
	w := &Watcher{cfg: WatcherConfig{AgentName: "captain", Clock: substrate.NewFakeClock(time.Now())}}
	id := w.mintCycleID()
	if !strings.HasPrefix(id, "cyc-") {
		t.Errorf("mintCycleID() = %q, want a cyc- prefixed id", id)
	}
}

func TestIsLeaderRole(t *testing.T) {
	for _, leader := range []string{"captain", "admiral"} {
		if !isLeaderRole(leader) {
			t.Errorf("isLeaderRole(%q) = false, want true", leader)
		}
	}
	for _, crew := range []string{"delta", "charlie", "echo", "watch", ""} {
		if isLeaderRole(crew) {
			t.Errorf("isLeaderRole(%q) = true, want false", crew)
		}
	}
}
