// Package agentlaunch holds the SHARED, dependency-light helpers for bringing up
// a harmonik agent session (captain or crew) with its sibling session-keeper
// window. It is the single home for the keeper-window argv assembly + the
// generic "spawn agent session, then add the keeper window" orchestration that
// used to live ONLY in internal/daemon/tmuxsubstrate.go.
//
// WHY a shared package (review outcome A, plan plans/2026-06-20-easy-start-commands):
//
//	The daemon ALREADY spawns the crew + sibling-keeper windows natively in Go
//	(spawnCrewKeeperWindow / crewKeeperWindowArgv). The captain is structurally
//	"a crew + a sentinel". Rather than write a THIRD implementation in
//	cmd/harmonik/captain.go (bash=1, daemon=2), the daemon's nesting+keeper-arm is
//	EXTRACTED here behind an argv-builder + an injectable window-spawner seam, and
//	consumed by BOTH the daemon spawn path and the CLI captain launcher.
//
// Dependency discipline: this package imports ONLY internal/lifecycle/tmux (for
// the window-name constants + the Outcome/NewWindowIn types) and stdlib. It does
// NOT import internal/daemon, so the CLI launcher (cmd/harmonik) can consume it
// without dragging in daemon deps (the explicit requirement in the ES2 brief:
// "built against an interface/argv-builder, NOT the daemon's tmux Adapter
// directly").
package agentlaunch

import (
	"strconv"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// KeeperWindowOpts parameterizes the argv the sibling keeper window runs to
// watch an agent (captain or crew) pane.
//
// The keeper ALWAYS targets the sibling "agent" window ("--tmux <session>:agent")
// so it never pastes into its own "keeper" window. The remaining fields select
// the band:
//
//   - WarnOnly=true → "--warn-only": the keeper emits warn events but does NOT
//     drive a restart (the historical crew default, crewKeeperWindowArgv).
//   - WarnOnly=false → an explicit "--warn-abs-tokens/--act-abs-tokens" band: the
//     keeper drives the full warn→act→restart cycle (the captain default, and —
//     once ES5/hk-lcga flips it — the crew default per D4).
//
// RespawnCmd, when non-empty, wires the dead-pane self-heal ("--respawn-cmd"):
// the keeper runs it via `sh -c` when the agent pane drops to a shell prompt.
type KeeperWindowOpts struct {
	// KeeperBin is the path of the harmonik binary the keeper window runs
	// (os.Executable() of the launching process; "harmonik" on resolution
	// failure so PATH is used). Required.
	KeeperBin string

	// AgentName is the --remote-control / comms identity of the watched agent
	// (e.g. "captain", or a crew name). Required.
	AgentName string

	// Session is the tmux session both windows live in
	// (e.g. "harmonik-<hash>-captain"). The keeper's inject target is derived as
	// "<Session>:agent". Required.
	Session string

	// ProjectDir, when non-empty, pins the keeper to the agent's project root
	// ("--project <dir>").
	ProjectDir string

	// WarnOnly selects the warn-only band when true; otherwise the explicit
	// WarnAbsTokens/ActAbsTokens band below is emitted.
	WarnOnly bool

	// WarnAbsTokens / ActAbsTokens are the absolute-token band forwarded when
	// WarnOnly is false AND the value is > 0. The launcher imposes NO product
	// default: a 0 (unset) value is OMITTED from the argv so the spawned keeper
	// reads the operator's keeper: block in .harmonik/config.yaml (and refuses to
	// start if a required value is missing). An explicit value is still forwarded
	// for an unambiguous warn→act threshold (hk-5da7). Ignored when WarnOnly.
	WarnAbsTokens int64
	ActAbsTokens  int64

	// RespawnCmd, when non-empty, is wired as "--respawn-cmd <cmd>" so the
	// keeper's dead-pane self-heal path (internal/keeper/watcher.go maybeRespawn)
	// can relaunch the agent window. Empty → no self-heal arming.
	RespawnCmd string
}

// KeeperWindowArgv builds the argv the keeper window runs:
//
//	warn-only:  <bin> keeper --agent <name> --tmux <session>:agent --warn-only [--project <dir>] [--respawn-cmd <cmd>]
//	full band:  <bin> keeper --agent <name> --tmux <session>:agent [--warn-abs-tokens <w>] [--act-abs-tokens <a>] [--project <dir>] [--respawn-cmd <cmd>]
//
// The inject target is "<Session>:agent" (ltmux.WindowAgent) so the keeper
// injects into the sibling agent window, never its own keeper window (slice K).
//
// OPERATOR-REQUIRED CONFIG: the launcher imposes NO product-default band numbers.
// A WarnAbsTokens/ActAbsTokens of 0 (unset) is OMITTED from the argv, so the
// spawned keeper reads the operator's keeper: block in .harmonik/config.yaml
// (refusing to start if a required value is missing) rather than receiving a
// baked-in default. An explicitly-set (> 0) value is still forwarded.
//
// This is the single source of truth for the keeper-window argv, consumed by
// both the daemon's crew spawn (crewKeeperWindowArgv now delegates here) and the
// CLI captain launcher.
func KeeperWindowArgv(opts KeeperWindowOpts) []string {
	injectTarget := opts.Session + ":" + ltmux.WindowAgent
	argv := []string{
		opts.KeeperBin, "keeper",
		"--agent", opts.AgentName,
		"--tmux", injectTarget,
	}
	if opts.WarnOnly {
		argv = append(argv, "--warn-only")
	} else {
		// Only forward an EXPLICIT (> 0) band value; 0 = unset → let operator config drive.
		if opts.WarnAbsTokens > 0 {
			argv = append(argv, "--warn-abs-tokens", strconv.FormatInt(opts.WarnAbsTokens, 10))
		}
		if opts.ActAbsTokens > 0 {
			argv = append(argv, "--act-abs-tokens", strconv.FormatInt(opts.ActAbsTokens, 10))
		}
	}
	if opts.ProjectDir != "" {
		argv = append(argv, "--project", opts.ProjectDir)
	}
	if opts.RespawnCmd != "" {
		argv = append(argv, "--respawn-cmd", opts.RespawnCmd)
	}
	return argv
}
