//go:build integration && darwin

package keeper

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
	target := session

	const partial = "echo OPERATOR_MID_KEYSTROKE_qji8g"
	if out, err := exec.CommandContext(context.Background(), "tmux", "send-keys", "-t", target, "-l", partial).CombinedOutput(); err != nil { //nolint:gosec
		t.Fatalf("qji8g: send-keys partial input: %v (%s)", err, out)
	}
	time.Sleep(150 * time.Millisecond)
	if before := qji8gCapturePane(t, target); !strings.Contains(before, partial) {
		t.Fatalf("qji8g: partial operator line not visible in the pane before the warn:\n%s", before)
	}

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

	time.Sleep(150 * time.Millisecond)
	after := qji8gCapturePane(t, target)
	if !strings.Contains(after, partial) {
		t.Fatalf("qji8g: partial operator line vanished after the warn — the comms path collided with the pane:\n%s", after)
	}
	if strings.Contains(after, "\nOPERATOR_MID_KEYSTROKE_qji8g\n") {
		t.Errorf("qji8g: the operator's line appears to have been SUBMITTED (executed) by the warn:\n%s", after)
	}
}
