package tmuxhost

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const awaitAckScrollback = 200

// CaptureTmuxPane is the production PaneCapturer: it runs
// `tmux capture-pane -p -t <target> -S -<awaitAckScrollback>` to grab the pane
// text plus a bounded scrollback tail. The -S tail catches a fast ACK that
// already scrolled off the visible region between the inject and the first poll.
func CaptureTmuxPane(ctx context.Context, tmuxTarget string) (string, error) {
	if tmuxTarget == "" {
		return "", fmt.Errorf("keeper: capture-pane: tmuxTarget is empty")
	}
	//nolint:gosec // G204: tmuxTarget is a keeper-resolved tmux address, not attacker input
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", tmuxTarget, "-S", fmt.Sprintf("-%d", awaitAckScrollback))
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("keeper: tmux capture-pane: %w (stderr: %s)", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("keeper: tmux capture-pane: %w", err)
	}
	return string(out), nil
}
