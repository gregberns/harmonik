package tmuxhost

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// SubmitSettle is the delay between the paste-buffer and the first submit
// Enter. A var (not a const) so tests can zero it to skip the wait.
var SubmitSettle = 750 * time.Millisecond

// SubmitRetries is the number of bounded retry Enters sent after the first,
// to defeat the bracketed-paste submit race (hk-89g).
const SubmitRetries = 2

// SubmitRetryDelay is the delay between retry Enters.
var SubmitRetryDelay = 400 * time.Millisecond

const injectBufferName = "harmonik-keeper-inject"

// RunFn runs a tmux subcommand and returns its combined output. A package var
// so tests can spy on tmux invocations without a real tmux server.
var RunFn = runTmuxCombined

func runTmuxCombined(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

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
	return injectTextClocked(ctx, substrate.SystemClock{}, tmuxTarget, text)
}

func injectTextClocked(ctx context.Context, clock substrate.ClockPort, tmuxTarget, text string) error {
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: inject: tmuxTarget is empty")
	}

	if out, err := RunFn(ctx, text, "load-buffer", "-b", injectBufferName, "-"); err != nil {
		return fmt.Errorf("keeper: tmux load-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	if out, err := RunFn(ctx, "", "paste-buffer", "-b", injectBufferName, "-t", tmuxTarget, "-d"); err != nil {
		return fmt.Errorf("keeper: tmux paste-buffer: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}

	if !clock.Sleep(ctx, SubmitSettle) {
		return ctx.Err()
	}

	if err := sendEnter(ctx, tmuxTarget); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Enter: %w", err)
	}

	for i := 0; i < SubmitRetries; i++ {
		if !clock.Sleep(ctx, SubmitRetryDelay) {
			break
		}
		_ = sendEnter(ctx, tmuxTarget) //nolint:errcheck // retry; best-effort
	}

	return nil
}

func sendEnter(ctx context.Context, tmuxTarget string) error {
	if out, err := RunFn(ctx, "", "send-keys", "-t", tmuxTarget, "Enter"); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SendEscapeKey sends an Escape keypress to the tmux pane at tmuxTarget.
// The cycle core calls this before injecting /session-handoff to preempt any
// in-progress input on a busy pane (e.g. partial text, a tool-call response
// being typed). Escape is harmless at a clean prompt and clears partial input
// in most REPL implementations. Refs: hk-qoz (forced-clear busy-pane fix).
func SendEscapeKey(ctx context.Context, tmuxTarget string) error {
	if tmuxTarget == "" {
		return fmt.Errorf("keeper: send-escape: tmuxTarget is empty")
	}
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxTarget, "Escape")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keeper: tmux send-keys Escape: %w (stderr: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
