//go:build integration

package keeper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestResetTmuxInput_RemovesQueuedClear drives ResetTmuxInput against a real
// tmux server: it queues "/clear" at a live prompt, resets, submits, and
// asserts the queued text never executed. It is the only test of
// ResetTmuxInput.
//
// It lives in the integration tier because it shells out to real tmux, which
// is what every other real-tmux keeper test does here (tmuxresolve,
// cycle_operator_attached, watcher_real_env, ...). The unit tier now refuses
// real tmux: internal/keeper/tmux_unit_guard_test.go installs a fake tmux on
// PATH for the whole untagged test binary and fails the package if anything
// reaches it. A LookPath skip does not save an untagged test from that guard,
// because the fake IS a file named tmux on PATH.
//
//nolint:gosec,errcheck // Real tmux commands use test-owned names and best-effort cleanup.
func TestResetTmuxInput_RemovesQueuedClear(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	session := fmt.Sprintf("keeper-reset-input-%d", time.Now().UnixNano())
	start := exec.CommandContext(t.Context(), "tmux", "new-session", "-d", "-s", session, "bash --noprofile --norc")
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", session).Run() })

	if out, err := exec.CommandContext(t.Context(), "tmux", "send-keys", "-t", session, "-l", "/clear").CombinedOutput(); err != nil {
		t.Fatalf("queue clear: %v: %s", err, out)
	}
	capture := func() string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "tmux", "capture-pane", "-p", "-t", session).CombinedOutput()
		if err != nil {
			t.Fatalf("capture tmux: %v: %s", err, out)
		}
		return string(out)
	}
	if got := capture(); !strings.Contains(got, "/clear") {
		t.Fatalf("queued clear not visible before reset: %q", got)
	}
	if err := ResetTmuxInput(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "tmux", "send-keys", "-t", session, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("submit prompt after reset: %v: %s", err, out)
	}
	time.Sleep(50 * time.Millisecond)
	if got := capture(); strings.Contains(got, "bash: /clear") || strings.Contains(got, "/clear: command not found") {
		t.Fatalf("queued clear executed after reset: %q", got)
	}
}
