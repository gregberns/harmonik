package main

// captain_respawn.go — `harmonik captain respawn`: the NATIVE-Go replacement for
// the GENERATED captain-respawn.sh dead-pane self-heal helper (ES3 / hk-z1rj,
// plan plans/2026-06-20-easy-start-commands §3 step 6, review outcome B).
//
// WHAT IT REPLACES: the retired bash captain launcher used to `cat >` a bash file
// (captain-respawn.sh) that the keeper's idle-respawn path (internal/keeper/
// watcher.go maybeRespawn, run via `sh -c`) invoked when the captain agent pane
// died. That bash file did exactly two tmux calls:
//
//	tmux respawn-window -k -t <sess>:agent -e HARMONIK_AGENT=<name> \
//	  'claude --dangerously-skip-permissions --remote-control <name> --resume <SID>'
//	tmux display-message -p -t <sess>:agent '#{pane_pid}' > .../captain.pid
//
// This subcommand does the SAME two operations natively (no bash to emit, no
// quote-nesting, cross-platform, table-testable). D1 mandates no logic in a `.sh`
// we cannot unit-test; the keeper's `--respawn-cmd` seam now points at a
// `harmonik captain respawn …` invocation instead of a generated script path.
//
// INVARIANTS PRESERVED (hk-opuv / hk-z036):
//   - RESPAWN ONLY THE AGENT WINDOW. `respawn-window -k -t <sess>:agent`
//     re-launches the captain in the EXISTING agent window of the EXISTING
//     session. The sibling `keeper` window — and the keeper process running this
//     very command — survive untouched (hk-z036 invariant I1). It does NOT touch
//     the whole session and does NOT arm a second keeper (no dup keeper).
//   - --resume <SID>, NOT --session-id <SID>. Resuming the SAME minted session-id
//     keeps the captain's conversation AND the keeper's identity binding intact.
//     A fresh --session-id would fork a NEW conversation (the load-bearing bug
//     this guards against — see the test).
//   - REFRESH captain.pid. After the respawn the agent pane has a NEW PID; we
//     re-read it and rewrite .harmonik/cognition/captain.pid so the daemon orphan
//     sweep keeps skipping the freshly-relaunched session (PL-006d ii).
//
// Routed from runCaptainSubcommand (main.go → `harmonik captain` handler): the
// first sub-arg `respawn` dispatches here; anything else falls through to the
// launcher. Bead refs: hk-z1rj (this), hk-opuv (the behavior), hk-z036 (nesting).

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/keeper"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// captainRespawnRunFn is the seam tests inject to capture the assembled tmux
// commands (respawn-window, then display-message) WITHOUT a real tmux server.
// Production passes runCaptainRespawnTmux. It mirrors captainLaunchRunFn's shape
// (captain.go) so the respawn argv is asserted exactly the way the launch argv
// is. For display-message the run func must ALSO return the captured stdout (the
// new pane PID) so the caller can refresh captain.pid; production wires
// cmd.Output via the helper below.
type captainRespawnRunFn func(cmd *exec.Cmd) ([]byte, error)

// runCaptainRespawnTmux is the production run func: it runs the assembled tmux
// command and returns its stdout (used for the pane-pid read). respawn-window
// produces no stdout we need, so its bytes are ignored by the caller.
func runCaptainRespawnTmux(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }

// buildCaptainRespawnWindowCmd assembles the agent-window respawn command:
//
//	tmux respawn-window -k -t <session>:agent -e HARMONIK_AGENT=<name> \
//	  claude --dangerously-skip-permissions --remote-control <name> --resume <sessionID>
//
// -k KILLS the (dead) existing pane and re-launches in place — the agent window
// only, never the whole session, so the sibling keeper window survives. --resume
// (NOT --session-id) re-binds the SAME conversation. The agent-window target is
// "<session>:agent" (ltmux.WindowAgent).
func buildCaptainRespawnWindowCmd(name, tmuxTarget, sessionID string) *exec.Cmd {
	return exec.Command(
		"tmux", "respawn-window", "-k",
		"-t", tmuxTarget,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--remote-control", name,
		"--resume", sessionID,
	)
}

// buildCaptainPanePIDCmd assembles the pane-pid read used to refresh captain.pid:
//
//	tmux display-message -p -t <session>:agent #{pane_pid}
func buildCaptainPanePIDCmd(tmuxTarget string) *exec.Cmd {
	return exec.Command(
		"tmux", "display-message", "-p",
		"-t", tmuxTarget,
		"#{pane_pid}",
	)
}

// captainRespawnTarget resolves the agent-window tmux target the respawn acts on.
// An explicit --tmux may already be a "session:window" target (the keeper passes
// "<sess>:agent" — that is what `--respawn-cmd` is armed with). If it is a bare
// session name (no ":"), the agent window is appended so the respawn always hits
// the agent window, never whatever window happens to be active.
func captainRespawnTarget(tmuxFlag string) string {
	if strings.Contains(tmuxFlag, ":") {
		return tmuxFlag
	}
	return tmuxFlag + ":" + ltmux.WindowAgent
}

// runCaptainRespawnSubcommand is the entry point for `harmonik captain respawn`.
// It wires the production run func and delegates to the testable core.
func runCaptainRespawnSubcommand(subArgs []string) int {
	return runCaptainRespawn(subArgs, runCaptainRespawnTmux, os.Stdout, os.Stderr)
}

// runCaptainRespawn is the testable core. It assembles + runs (via the injected
// run func) the two tmux commands and refreshes captain.pid. Returns:
//
//	0  — agent window respawned (a captain.pid refresh failure only WARNS — the
//	     captain pane is already back up; the sweep's PRIMARY path probes the live
//	     session, so a stale pid degrades gracefully).
//	1  — flag/arg error, a non-UUIDv4 --session-id, cwd resolution failure, or the
//	     respawn-window tmux call itself failing (nothing to refresh then).
func runCaptainRespawn(subArgs []string, run captainRespawnRunFn, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("captain respawn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity to respawn")
	tmuxFlag := fs.String("tmux", "", "tmux session (or session:window) target of the captain agent window")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to --resume (NOT --session-id: resume keeps the same conversation)")
	projectFlag := fs.String("project", "", "project directory holding .harmonik/cognition/captain.pid (default: current working directory)")

	if err := fs.Parse(subArgs); err != nil {
		return 1
	}

	name := *nameFlag
	if name == "" {
		fmt.Fprintln(stderr, "harmonik captain respawn: --name must not be empty")
		return 1
	}
	if *tmuxFlag == "" {
		fmt.Fprintln(stderr, "harmonik captain respawn: --tmux is required (the captain agent-window target)")
		return 1
	}
	sessionID := *sessionIDFlag
	if sessionID == "" {
		fmt.Fprintln(stderr, "harmonik captain respawn: --session-id is required (the minted SID to --resume)")
		return 1
	}
	// The keeper's clear→resume cycle only trusts a canonical lowercase UUIDv4;
	// the same gate the launcher (captain.go) applies — reuse keeper.IsPrimarySID
	// so respawn and launch agree on identity.
	if !keeper.IsPrimarySID(sessionID) {
		fmt.Fprintf(stderr, "harmonik captain respawn: --session-id %q is not a canonical lowercase UUIDv4 "+
			"(the keeper's resume binding requires it)\n", sessionID)
		return 1
	}

	project := *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik captain respawn: cannot determine working directory: %v\n", err)
			return 1
		}
		project = wd
	}

	target := captainRespawnTarget(*tmuxFlag)

	// 1) Respawn ONLY the agent window, resuming the same session-id. A failure
	//    here is fatal — there is nothing to refresh if the relaunch did not run.
	if _, err := run(buildCaptainRespawnWindowCmd(name, target, sessionID)); err != nil {
		fmt.Fprintf(stderr, "harmonik captain respawn: respawn-window %q: %v\n", target, err)
		return 1
	}

	// 2) Refresh captain.pid from the NEW agent pane PID so the daemon orphan
	//    sweep keeps skipping the relaunched session. Best-effort: a failure WARNS
	//    but the captain is already back up.
	if err := refreshCaptainPID(run, project, target); err != nil {
		fmt.Fprintf(stderr, "harmonik captain respawn: %v — the daemon orphan sweep may reap this captain "+
			"until captain.pid is refreshed\n", err)
	}

	fmt.Fprintf(stdout, "captain respawn: agent window %q relaunched with --resume %s (agent window only, keeper window survives, no dup keeper)\n", target, sessionID)
	return 0
}

// refreshCaptainPID reads the new agent pane PID via display-message and rewrites
// .harmonik/cognition/captain.pid. The path layout MUST match orphansweep.go's
// captainPidfilePath (.harmonik/cognition/) — the same path captain.go writes.
func refreshCaptainPID(run captainRespawnRunFn, project, target string) error {
	out, err := run(buildCaptainPanePIDCmd(target))
	if err != nil {
		return fmt.Errorf("read agent pane PID for captain.pid: %w", err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return fmt.Errorf("read agent pane PID for captain.pid: empty pane_pid")
	}
	cognitionDir := filepath.Join(project, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		return fmt.Errorf("create cognition dir %q: %w", cognitionDir, err)
	}
	pidPath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidPath, []byte(pid+"\n"), 0o644); err != nil {
		return fmt.Errorf("write captain.pid: %w", err)
	}
	return nil
}

// captainRespawnCmdString builds the `--respawn-cmd` payload captain.go arms the
// keeper with: a fully-resolved `harmonik captain respawn …` invocation the
// keeper runs (via `sh -c`) when the agent pane dies. keeperBin is resolved by
// the caller (os.Executable()) so the keeper's `sh -c` finds the SAME binary.
// Each arg is shell-quoted so a path/SID with spaces survives the `sh -c`.
//
// The --tmux value is the agent-window target "<session>:agent" so the respawn
// hits the agent window directly (captainRespawnTarget treats an already-targeted
// value idempotently, but arming it explicitly is unambiguous).
func captainRespawnCmdString(keeperBin, name, tmuxSession, sessionID, project string) string {
	agentTarget := tmuxSession + ":" + ltmux.WindowAgent
	argv := []string{
		keeperBin, "captain", "respawn",
		"--name", name,
		"--tmux", agentTarget,
		"--session-id", sessionID,
		"--project", project,
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuoteRespawnArg(a)
	}
	return strings.Join(quoted, " ")
}

// shellQuoteRespawnArg single-quotes an argv element so it survives the keeper's
// `sh -c "<cmd>"`. Mirrors agentlaunch.ShellJoinArgv's quoting rule (single-quote,
// escaping embedded single quotes) but is local so this file does not depend on
// the agentlaunch package's unexported helper.
func shellQuoteRespawnArg(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`*?[]&;|<>(){}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
