// Package panehost defines the keeper's substrate-facing seam: everything the
// keeper needs from the terminal pane that hosts its managed agent, captured
// as one interface so a substrate (tmux today, herdr later) has ONE owner in
// the keeper's production wiring instead of ~11 free functions assigned
// piecemeal. Scoped to exactly what the keeper calls today — no kill, no
// spawn, no layout. Refs: plans/2026-09-07-keeper-herdr-substrate/README.md §4.1.
package panehost

import "context"

// Target is the substrate-specific address of the agent's pane. For tmux this
// is "session:window"; a future herdr adapter would use the stable agent
// name. Opaque to callers — only the PaneHost that minted it interprets it.
type Target string

// ForegroundState is what runs in the pane's foreground. It collapses
// tmux's historical IsPaneIdle/IsPaneAlive pair (documented mutually
// exclusive) into one enum.
type ForegroundState int

const (
	// ForegroundUnknown means the probe failed. Callers fail closed: take no
	// destructive action (no respawn, no live-pane recovery) on Unknown.
	ForegroundUnknown ForegroundState = iota
	// ForegroundShell means the managed agent has exited to a shell prompt —
	// respawn-eligible.
	ForegroundShell
	// ForegroundAgent means the managed agent process is present.
	ForegroundAgent
)

// PaneHost is everything the keeper needs from the pane that hosts its
// managed agent. It captures exactly what the keeper calls today; a
// substrate implements it in full to become a keeper backend.
type PaneHost interface {
	// Resolve finds the agent's pane. explicit passes through when
	// non-empty. "" means no usable target — the keeper proceeds
	// inject-less.
	Resolve(projectDir, agentName, explicit string) Target

	// Inject delivers text plus submit into the pane.
	Inject(ctx context.Context, t Target, text string) error
	// SendEscape sends an Escape keypress, clearing partial input.
	SendEscape(ctx context.Context, t Target) error
	// SetSessionEnv sets an environment variable inherited by processes
	// started in the pane's session after this call. Advisory: a failure is
	// treated as non-fatal by callers.
	SetSessionEnv(ctx context.Context, t Target, key, value string) error

	// Capture returns the pane's visible text plus a bounded scrollback
	// tail.
	Capture(ctx context.Context, t Target) (string, error)
	// Foreground reports what runs in the pane's foreground. An error probe
	// maps to ForegroundUnknown.
	Foreground(ctx context.Context, t Target) ForegroundState

	// OperatorAttached reports whether a human operator has been recently,
	// actively present at the pane's owning session. Implementations fail
	// open (false) on error or when the substrate carries no such signal.
	OperatorAttached(t Target) bool
}
