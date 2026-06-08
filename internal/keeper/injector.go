package keeper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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

// InjectText delivers arbitrary text into the tmux pane at tmuxTarget using
// the bracketed-paste mechanism (tmux load-buffer → paste-buffer → send-keys Enter).
//
// tmuxTarget is a tmux pane address in any of tmux's accepted forms:
// "session:window.pane", "session:window", "%pane_id", or just the session name.
//
// The cycle core uses this as its InjectFn default.
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

	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxTarget, "Enter")
	if out, err := enterCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Enter: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	return nil
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
