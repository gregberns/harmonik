package tmuxhost

import (
	"context"

	"github.com/gregberns/harmonik/internal/keeper/panehost"
)

// Host implements panehost.PaneHost against a real tmux server. The zero
// value is ready to use.
type Host struct{}

// New returns a ready-to-use tmux Host.
func New() Host { return Host{} }

var _ panehost.PaneHost = Host{}

// Resolve implements panehost.PaneHost via ResolveTmuxTarget.
func (Host) Resolve(projectDir, agentName, explicit string) panehost.Target {
	return panehost.Target(ResolveTmuxTarget(projectDir, agentName, explicit, nil))
}

// Inject implements panehost.PaneHost via InjectText.
func (Host) Inject(ctx context.Context, t panehost.Target, text string) error {
	return InjectText(ctx, string(t), text)
}

// SendEscape implements panehost.PaneHost via SendEscapeKey.
func (Host) SendEscape(ctx context.Context, t panehost.Target) error {
	return SendEscapeKey(ctx, string(t))
}

// SetSessionEnv implements panehost.PaneHost via SetTmuxEnv.
func (Host) SetSessionEnv(ctx context.Context, t panehost.Target, key, value string) error {
	return SetTmuxEnv(ctx, string(t), key, value)
}

// Capture implements panehost.PaneHost via CaptureTmuxPane.
func (Host) Capture(ctx context.Context, t panehost.Target) (string, error) {
	return CaptureTmuxPane(ctx, string(t))
}

// Foreground implements panehost.PaneHost via a single #{pane_current_command}
// query (foregroundState).
func (Host) Foreground(ctx context.Context, t panehost.Target) panehost.ForegroundState {
	return foregroundState(ctx, string(t))
}

// OperatorAttached implements panehost.PaneHost via the package-level
// OperatorAttached.
func (Host) OperatorAttached(t panehost.Target) bool {
	return OperatorAttached(string(t))
}
