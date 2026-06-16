package main

// captain.go — `harmonik captain` (alias `harmonik start captain`): the
// first-class BARE launcher for the Captain LLM session, superseding the
// caller-side scripts/captain-tools/captain-launch.sh minting+tmux dance.
//
// Scope (deliberately bare — hk-ly0n): mint/validate a stable UUIDv4
// --session-id, then build and run the tmux invocation that brings up
// `claude --remote-control <name> --session-id <id>` with HARMONIK_AGENT in the
// env. A STABLE caller-minted session-id is load-bearing: it is what lets the
// session-keeper's handoff → /clear → /session-resume cycle re-bind to the same
// conversation (mirrors the crew model — internal/daemon/crewstart.go
// resolveSessionID). See scripts/captain-tools/captain-launch.sh for the WHY.
//
// EXCLUDED here (each a separate bead): keeper enable/hook wiring (the 43KB
// keeper_enable_doctor_cmd.go surface), --respawn-cmd self-heal (hk-opuv), and
// keeper --warn/--act arming. This launcher only prints a hint pointing at
// `harmonik keeper enable captain` for the hook wiring.
//
// This is a LAUNCHER, not a daemon: it never acquires the daemon pidfile lock,
// so it cannot collide on it (exit 5 is impossible from this path).
//
// Bead ref: hk-ly0n.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/keeper"
)

// captainLaunchRunFn is the seam tests inject to capture the assembled tmux
// command without spawning a real session. Production passes runCaptainTmux.
type captainLaunchRunFn func(cmd *exec.Cmd) error

// runCaptainTmux is the production run func: it actually launches the session.
func runCaptainTmux(cmd *exec.Cmd) error { return cmd.Run() }

// buildCaptainTmuxCmd assembles the exec.Cmd for the captain's tmux session:
//
//	tmux new-session -d -s <tmux> -e HARMONIK_AGENT=<name> \
//	  claude --dangerously-skip-permissions --remote-control <name> --session-id <id>
//
// --dangerously-skip-permissions mirrors captain-launch.sh: a remote-control
// captain that hit a permission prompt would wedge unattended, so it is part of
// the launcher's correctness contract, not an optional extra. Returning the
// fully-built *exec.Cmd (rather than running it inline) is what lets the test
// assert the exact argv via the injected run func.
func buildCaptainTmuxCmd(name, tmuxSession, sessionID string) *exec.Cmd {
	return exec.Command(
		"tmux", "new-session", "-d",
		"-s", tmuxSession,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--remote-control", name,
		"--session-id", sessionID,
	)
}

// runCaptainSubcommand is the main.go entry point for `harmonik captain` and
// `harmonik start captain`. It wires the production run func.
func runCaptainSubcommand(subArgs []string) int {
	return runCaptainLaunch(subArgs, runCaptainTmux)
}

// runCaptainLaunch parses flags, resolves/validates the session-id, builds the
// tmux invocation, and hands it to run. Split from runCaptainSubcommand so the
// test can inject a capturing run func (no real tmux).
//
// Exit codes:
//
//	0  — captain launched
//	1  — flag/arg error, non-UUIDv4 --session-id, cwd resolution, or tmux failure
func runCaptainLaunch(subArgs []string, run captainLaunchRunFn) int {
	fs := flag.NewFlagSet("captain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity")
	tmuxFlag := fs.String("tmux", "captain", "tmux session name to launch the captain in")
	projectFlag := fs.String("project", "", "project directory (default: current working directory)")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to launch with (minted when absent)")

	if err := fs.Parse(subArgs); err != nil {
		// flag prints its own message; ErrHelp also lands here.
		return 1
	}

	name := *nameFlag
	if name == "" {
		fmt.Fprintln(os.Stderr, "harmonik captain: --name must not be empty")
		return 1
	}
	tmuxSession := *tmuxFlag
	if tmuxSession == "" {
		fmt.Fprintln(os.Stderr, "harmonik captain: --tmux must not be empty")
		return 1
	}

	project := *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: cannot determine working directory: %v\n", err)
			return 1
		}
		project = wd
	}

	// Resolve the session-id: validate a supplied one (reject non-UUIDv4), mint
	// a fresh UUIDv4 otherwise. keeper.IsPrimarySID is the canonical lowercase
	// UUIDv4 check the keeper itself trusts for PRIMARY identity (sessionid.go);
	// reusing it keeps the launcher and the watcher in lock-step.
	sessionID := *sessionIDFlag
	if sessionID == "" {
		sessionID = uuid.New().String()
	} else if !keeper.IsPrimarySID(sessionID) {
		fmt.Fprintf(os.Stderr, "harmonik captain: --session-id %q is not a canonical lowercase UUIDv4 "+
			"(interactive captain/crew sessions use UUIDv4; the keeper's clear→resume cycle requires it)\n", sessionID)
		return 1
	}

	cmd := buildCaptainTmuxCmd(name, tmuxSession, sessionID)
	if err := run(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: launch tmux session %q: %v\n", tmuxSession, err)
		return 1
	}

	fmt.Printf("captain launched: name=%q tmux=%q session_id=%s project=%q\n", name, tmuxSession, sessionID, project)
	fmt.Printf("captain launched; run `harmonik keeper enable %s` to wire keeper hooks.\n", name)
	return 0
}
