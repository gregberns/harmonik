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

// InjectWrapUpWarning delivers the wrap-up-warning prompt into the tmux pane
// identified by tmuxTarget using the bracketed-paste mechanism (tmux
// load-buffer → paste-buffer → send-keys Enter).
//
// tmuxTarget is a tmux pane address in any of tmux's accepted forms:
// "session:window.pane", "session:window", "%pane_id", or just the session name.
//
// The injector is side-effect-only. Errors are returned but the watcher
// treats injection failure as non-fatal (warn event is still emitted).
func InjectWrapUpWarning(ctx context.Context, tmuxTarget string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: inject: tmuxTarget is empty")
	}

	// Step 1: load the warning text into a named tmux buffer via stdin.
	loadCmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", bufferName, "-")
	loadCmd.Stdin = strings.NewReader(wrapUpWarningText)
	if out, err := loadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux load-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	// Step 2: paste the buffer into the target pane (bracketed-paste mode).
	// The -d flag deletes the buffer after pasting.
	pasteCmd := exec.CommandContext(ctx, "tmux", "paste-buffer", "-b", bufferName, "-t", tmuxTarget, "-d")
	if out, err := pasteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux paste-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	// Step 3: send Enter to submit the pasted text.
	enterCmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxTarget, "Enter")
	if out, err := enterCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Enter: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}
