package keeper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// wrapUpWarningText is the prompt injected into the managed pane when the
// context-window percentage crosses the warn threshold. It is NON-DESTRUCTIVE:
// it asks the agent to wrap up without forcing a /clear or handoff.
const wrapUpWarningText = "Context window is approaching its limit. " +
	"Please wrap up your current work: commit any in-progress changes, " +
	"write a brief handoff note if needed, then run /quit."

// bufferName is the tmux buffer name used for keeper injections. Using a
// keeper-specific name avoids clobbering buffers owned by the daemon's own
// paste-inject step (which uses buffers like "hk-<run_id>").
const bufferName = "hk-keeper-warn"

// submitSettle is the grace period between the bracketed-paste write and the
// first submit Enter. Without it the post-paste Enter can land before the REPL
// input handler has finished accepting the pasted text, and the line sits
// unsubmitted in the pane buffer (hk-89g; same race as the implementer path's
// hk-jzpqo / hk-wcv). Mirrors internal/daemon/pasteinject.go's splash-settle.
var submitSettle = 750 * time.Millisecond

// submitRetries / submitRetryDelay re-send the submit Enter a bounded number of
// extra times to defend against a dropped first keypress. A redundant Enter at
// an already-submitted REPL is a harmless empty line. Mirrors the daemon's
// resumeSubmitRetries / resumeSubmitRetryDelay (hk-ip33d, hk-7rgqs).
const (
	submitRetries    = 2
	submitRetryDelay = 400 * time.Millisecond
)

// InjectText delivers arbitrary text into the tmux pane at tmuxTarget using
// the bracketed-paste mechanism (tmux load-buffer → paste-buffer → settle →
// send-keys Enter with bounded retry).
//
// tmuxTarget is a tmux pane address in any of tmux's accepted forms:
// "session:window.pane", "session:window", "%pane_id", or just the session name.
//
// The submit Enter is delivered after a short settle and then re-sent a bounded
// number of times. This mirrors the WORKING implementer paste path
// (internal/daemon/pasteinject.go) and fixes the bracketed-paste submit race
// where the injected line (e.g. /session-resume) sits in the pane buffer until
// a manual Enter (hk-89g).
//
// The cycle core uses this as its InjectFn default, so /session-handoff,
// /clear, and /session-resume all inherit the fix.
func InjectText(ctx context.Context, tmuxTarget, text string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: inject: tmuxTarget is empty")
	}

	const buf = "hk-keeper-inject"

	loadCmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", buf, "-")
	loadCmd.Stdin = strings.NewReader(text)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux load-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	pasteCmd := exec.CommandContext(ctx, "tmux", "paste-buffer", "-b", buf, "-t", tmuxTarget, "-d")
	if out, err := pasteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux paste-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	// Settle so the REPL finishes ingesting the pasted text before the submit
	// Enter; otherwise the first Enter races ahead and is dropped (hk-89g).
	if !sleepCtx(ctx, submitSettle) {
		return ctx.Err()
	}

	// First submit Enter is load-bearing; a non-nil error here is surfaced.
	if err := sendEnter(ctx, tmuxTarget); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Enter: %w", err)
	}

	// Bounded retries defend against a dropped first keypress. Failures here are
	// non-fatal — the line is already submitted by the first Enter on the happy
	// path, and a redundant Enter is a harmless empty line.
	for i := 0; i < submitRetries; i++ {
		if !sleepCtx(ctx, submitRetryDelay) {
			break
		}
		_ = sendEnter(ctx, tmuxTarget) //nolint:errcheck // retry; best-effort
	}

	return nil
}

// sendEnter sends a bare Enter keypress to the pane as a real key event (NOT a
// bracketed paste), so the REPL's key-event handler submits the pending line.
func sendEnter(ctx context.Context, tmuxTarget string) error {
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxTarget, "Enter")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sleepCtx waits for d or until ctx is cancelled. Returns true if the full
// duration elapsed, false if ctx was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// InjectWrapUpWarning delivers the wrap-up-warning prompt into the tmux pane
// identified by tmuxTarget using the bracketed-paste mechanism.
//
// The injector is side-effect-only. Errors are returned but the watcher
// treats injection failure as non-fatal (warn event is still emitted).
func InjectWrapUpWarning(ctx context.Context, tmuxTarget string) error {
	return InjectText(ctx, tmuxTarget, wrapUpWarningText)
}

// SetTmuxEnv sets an environment variable in the tmux session that owns
// tmuxTarget. The variable is inherited by any new process started in that
// session after this call — including a Claude Code session resumed after /clear.
//
// Uses `tmux setenv -t <target> <key> <value>` which writes to the session
// environment table. This is intentionally NOT `setenv -g` (global) to avoid
// leaking across unrelated sessions.
func SetTmuxEnv(ctx context.Context, tmuxTarget, key, value string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: setenv: tmuxTarget is empty")
	}
	cmd := exec.CommandContext(ctx, "tmux", "setenv", "-t", tmuxTarget, key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux setenv %s: %w (stderr: %s)", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}
