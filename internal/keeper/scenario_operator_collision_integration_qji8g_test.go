//go:build integration && darwin

package keeper

// scenario_operator_collision_integration_qji8g_test.go — T9 scenario (a),
// REAL-tmux integration tier (build tags: integration && darwin, mirroring
// cycle_operator_attached_integration_test.go). Bead
// hk-keeper-delivery-scenario-tests-qji8g.
//
// The operator-typing collision, on a REAL pane: partial operator input is placed
// on a live tmux pane (send-keys, NO Enter). A leader warn then fires on the
// PRODUCTION delivery path with the leader presence-Online → the K1 comms channel
// is taken. Because the comms path issues ZERO pane write (SK-022), the operator's
// partial, unsubmitted line is still sitting in the pane afterward — never
// clobbered, never submitted. Fail-before: the pre-T7 warn path pane-pasted on
// every warn, so this line would have been overwritten/submitted mid-keystroke.
//
// Safety: creates and destroys ONLY its own uniquely-named throwaway session
// (prefix "qji8g-collide-"); teardown kills BY EXACT NAME. Skips when tmux is
// absent. Reuses the package-keeper helpers writePresenceBeat / swapCommsSend /
// swapTmuxRun (delivery_decision_0nlqs_test.go).

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func qji8gRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("qji8g: tmux not found on PATH; skipping real-tmux collision test")
	}
}

// qji8gStartShellSession creates a detached bash pane (so typed keystrokes echo
// into the captured pane) and registers an exact-name teardown.
func qji8gStartShellSession(t *testing.T) string {
	t.Helper()
	//nolint:gosec // G404: test-local session-name uniqueness, no security relevance
	name := fmt.Sprintf("qji8g-collide-%d-%d", rand.Int64(), rand.Int64())
	if out, err := exec.CommandContext(context.Background(), "tmux", "new-session", "-d", "-s", name, "bash", "--norc").CombinedOutput(); err != nil {
		t.Fatalf("qji8g: new-session %q: %v (%s)", name, err, out)
	}
	t.Cleanup(func() {
		_ = exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", "="+name).Run() //nolint:errcheck,gosec
	})
	return name
}

func qji8gCapturePane(t *testing.T, target string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-t", target).CombinedOutput() //nolint:gosec
	if err != nil {
		t.Fatalf("qji8g: capture-pane %q: %v (%s)", target, err, out)
	}
	return string(out)
}

func TestIntegration_OperatorTypingCollision_CommsLeavesPartialLine_qji8g(t *testing.T) {
	qji8gRequireTmux(t)

	session := qji8gStartShellSession(t)
	// Address the pane by SESSION NAME (its active pane), NOT "<session>:0.0":
	// tmux with base-index=1 has no window 0, so ":0.0" errors "can't find window:
	// 0". Mirrors cycle_operator_attached_integration_test.go / the restart-now
	// smoke test, which target by session name.
	target := session

	// Operator types a partial command and does NOT press Enter — it sits in the
	// pane's input buffer, unsubmitted.
	const partial = "echo OPERATOR_MID_KEYSTROKE_qji8g"
	if out, err := exec.CommandContext(context.Background(), "tmux", "send-keys", "-t", target, "-l", partial).CombinedOutput(); err != nil { //nolint:gosec
		t.Fatalf("qji8g: send-keys partial input: %v (%s)", err, out)
	}
	// Let the pane settle, then confirm the partial line is present and unsubmitted.
	time.Sleep(150 * time.Millisecond)
	if before := qji8gCapturePane(t, target); !strings.Contains(before, partial) {
		t.Fatalf("qji8g: partial operator line not visible in the pane before the warn:\n%s", before)
	}

	// Leader is presence-Online → the comms channel must be taken. commsSendFn is
	// swapped to a no-op so no daemon is required; the point is that the pane is
	// NOT written.
	path := writePresenceBeat(t, "captain", time.Now())
	comms := swapCommsSend(t)
	paneWrites := swapTmuxRun(t) // guards the in-process injector seam: must stay 0

	w := &Watcher{cfg: WatcherConfig{
		AgentName:          "captain",
		EventsJSONLPath:    path,
		TmuxTarget:         target,
		OperatorAttachedFn: func(string) bool { return true }, // operator is typing
	}}
	handled, cleared := w.maybeDeliverLeaderWarn(context.Background(), &CtxFile{SessionID: "sid"}, true)
	if !handled || !cleared {
		t.Fatalf("handled=%v cleared=%v, want true,true (leader comms delivery)", handled, cleared)
	}
	if len(*comms) != 1 {
		t.Fatalf("commsSendFn called %d times, want 1 (comms channel taken)", len(*comms))
	}
	if *paneWrites != 0 {
		t.Errorf("injector seam wrote the pane %d times, want 0 (SK-022)", *paneWrites)
	}

	// THE COLLISION ASSERTION: the operator's partial line is STILL in the pane,
	// unsubmitted — the warn did not clobber the in-flight keystroke.
	time.Sleep(150 * time.Millisecond)
	after := qji8gCapturePane(t, target)
	if !strings.Contains(after, partial) {
		t.Fatalf("qji8g: partial operator line vanished after the warn — the comms path collided with the pane:\n%s", after)
	}
	// The echo must NOT have executed (no submission): a run would print its output
	// and a fresh prompt line. The literal token must not appear on its own output line.
	if strings.Contains(after, "\nOPERATOR_MID_KEYSTROKE_qji8g\n") {
		t.Errorf("qji8g: the operator's line appears to have been SUBMITTED (executed) by the warn:\n%s", after)
	}
}
