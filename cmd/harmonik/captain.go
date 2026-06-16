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
// Keeper wiring (hk-igek): the launcher now wires the 3 keeper stanzas into
// ~/.claude/settings.json BEFORE bringing up the tmux session, by reusing the
// `harmonik keeper enable` core (enableConfig + runKeeperEnable). Wiring must
// happen pre-launch so the freshly-started `claude` reads the statusLine + Stop
// + PreCompact hooks at session start. `--no-keeper` opts out; a keeper-enable
// failure WARNS but does not block the launch (the captain stays bootable).
//
// STILL EXCLUDED (each a separate bead): --respawn-cmd self-heal (hk-opuv) and
// keeper --warn/--act arming.
//
// This is a LAUNCHER, not a daemon: it never acquires the daemon pidfile lock,
// so it cannot collide on it (exit 5 is impossible from this path).
//
// Bead refs: hk-ly0n (bare launcher), hk-igek (keeper-enable wiring).

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/keeper"
)

// captainLaunchRunFn is the seam tests inject to capture the assembled tmux
// command without spawning a real session. Production passes runCaptainTmux.
type captainLaunchRunFn func(cmd *exec.Cmd) error

// keeperEnableFn is the seam tests inject to capture the keeper-enable call
// without touching the real ~/.claude/settings.json. Production passes
// runKeeperEnable (the testable core of `harmonik keeper enable`).
type keeperEnableFn func(cfg enableConfig, stdout, stderr io.Writer) int

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
// `harmonik start captain`. It wires the production run + keeper-enable funcs.
func runCaptainSubcommand(subArgs []string) int {
	return runCaptainLaunch(subArgs, runCaptainTmux, runKeeperEnable)
}

// buildCaptainKeeperConfig assembles the enableConfig used to wire keeper hooks
// for a freshly-launched captain. It mirrors runKeeperEnableEntry's resolution
// (auto-detected scripts dir, ~/.claude/settings.json) so the bare launcher and
// `harmonik keeper enable <name>` produce identical wiring. The captain is not a
// known-live agent, so no --yes-destructive gate applies.
func buildCaptainKeeperConfig(name, projectDir string) (enableConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return enableConfig{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return enableConfig{
		agentName:    name,
		projectDir:   projectDir,
		scriptsDir:   autoDetectScriptsDir(),
		settingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

// runCaptainLaunch parses flags, resolves/validates the session-id, builds the
// tmux invocation, and hands it to run. Split from runCaptainSubcommand so the
// test can inject a capturing run func (no real tmux).
//
// Exit codes:
//
//	0  — captain launched (a keeper-enable failure only WARNS, it does not block)
//	1  — flag/arg error, non-UUIDv4 --session-id, cwd resolution, or tmux failure
func runCaptainLaunch(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn) int {
	fs := flag.NewFlagSet("captain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity")
	tmuxFlag := fs.String("tmux", "captain", "tmux session name to launch the captain in")
	projectFlag := fs.String("project", "", "project directory (default: current working directory)")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to launch with (minted when absent)")
	noKeeperFlag := fs.Bool("no-keeper", false, "skip wiring the keeper hooks into ~/.claude/settings.json")

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

	// Wire keeper hooks BEFORE launching tmux so the new `claude` session reads
	// the statusLine + Stop + PreCompact stanzas at session start. A failure here
	// only WARNS — the captain must stay bootable even if scripts can't be found.
	if !*noKeeperFlag {
		cfg, err := buildCaptainKeeperConfig(name, project)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: %v\n", err)
			return 1
		}
		if rc := enableKeeper(cfg, os.Stdout, os.Stderr); rc != 0 {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper enable returned %d — launching anyway; "+
				"run `harmonik keeper enable %s` manually to wire keeper hooks\n", rc, name)
		}
	}

	cmd := buildCaptainTmuxCmd(name, tmuxSession, sessionID)
	if err := run(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: launch tmux session %q: %v\n", tmuxSession, err)
		return 1
	}

	fmt.Printf("captain launched: name=%q tmux=%q session_id=%s project=%q\n", name, tmuxSession, sessionID, project)
	if *noKeeperFlag {
		fmt.Printf("captain launched; keeper wiring skipped (--no-keeper) — run `harmonik keeper enable %s` to wire hooks.\n", name)
	} else {
		fmt.Printf("captain launched; keeper hooks wired (pass --no-keeper to skip).\n")
	}
	return 0
}
