package main

// captain.go — `harmonik captain` (alias `harmonik start captain`): the
// first-class, NATIVE-Go, full-parity launcher for the Captain LLM session,
// superseding the caller-side scripts/captain-tools/captain-launch.sh
// minting+tmux dance (D1: no bash on the launch path, cross-platform, testable).
//
// PARITY (ES2 / hk-bcd0, plan plans/2026-06-20-easy-start-commands §3 + review A):
// the launcher now does EVERYTHING captain-launch.sh did, natively:
//
//  1. project = --project or cwd; project-hash computed IN-PROCESS via
//     lifecycle.ComputeProjectHash (no `harmonik project-hash` shell-out, no
//     HK_PROJECT env var).
//  2. Launch into the HASHED namespace harmonik-<hash>-captain
//     (lifecycle.TmuxSessionName), NOT plain "captain", so reap/restart tooling
//     and probeCaptainSentinel (orphansweep.go:518) recognize the session.
//  3. Write .harmonik/cognition/captain.sentinel + captain.pid so the daemon
//     orphan sweep skips the captain while it is live (PL-006d ii). THIS is the
//     D6 fix: the old bare launcher wrote neither, so a captain launched via the
//     Go path was reaped on the next sweep.
//  4. Window-nesting: the captain claude runs in the "agent" window; the keeper
//     runs in a sibling "keeper" window of the SAME session (hk-z036), built via
//     the SHARED agentlaunch helper (review outcome A — same nesting+keeper-arm
//     the daemon's crew spawn uses; no third implementation).
//  5. Keeper WATCHER armed in the keeper window with the real warn/act band
//     (keeper.DefaultWarnAbsTokens / DefaultActAbsTokens), plus the keeper-enable
//     settings.json stanza wiring kept from hk-igek.
//  6. D7 idempotent pre-flight: if the target session already exists (the keeper
//     outlived a stopped agent), the launcher REAPS the stale session and
//     recreates it rather than erroring with tmux "duplicate session".
//
// WHY a STABLE caller-minted --session-id is load-bearing: it is what lets the
// session-keeper's handoff → /clear → /session-resume cycle re-bind to the same
// conversation (mirrors the crew model — internal/daemon/crewstart.go
// resolveSessionID). See scripts/captain-tools/captain-launch.sh for the full WHY.
//
// This is a LAUNCHER, not a daemon: it never acquires the daemon pidfile lock,
// so it cannot collide on it (exit 5 is impossible from this path).
//
// STILL EXCLUDED (ES3 / hk-z1rj): the `harmonik captain respawn` subcommand that
// the --respawn-cmd seam points at, and deleting the verified-restart wrapper.
// ES2 wires --respawn-cmd as a TODO-marked seam so ES3 slots in cleanly; see the
// respawnCmd hand-off below.
//
// Bead refs: hk-ly0n (bare launcher), hk-igek (keeper-enable wiring),
// hk-bcd0 (this — native full parity D1/D6/D7).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// captainLaunchRunFn is the seam tests inject to capture the assembled
// agent-window tmux new-session command without spawning a real session.
// Production passes runCaptainTmux. Kept (hk-ly0n) so existing argv tests on the
// agent-window launch continue to assert the exact `tmux new-session` argv.
type captainLaunchRunFn func(cmd *exec.Cmd) error

// keeperEnableFn is the seam tests inject to capture the keeper-enable call
// without touching the real ~/.claude/settings.json. Production passes
// runKeeperEnable (the testable core of `harmonik keeper enable`).
type keeperEnableFn func(cfg enableConfig, stdout, stderr io.Writer) int

// runCaptainTmux is the production run func for the agent-window launch: it
// actually runs the assembled `tmux new-session` command.
func runCaptainTmux(cmd *exec.Cmd) error { return cmd.Run() }

// captainTmuxOps is the seam for the orchestration steps that the single-command
// captainLaunchRunFn cannot express: the D7 existing-session pre-flight (list +
// reap), the sibling keeper-window creation, and reading the agent pane PID for
// captain.pid. Production is osCaptainTmuxOps (delegating to tmux.OSAdapter);
// tests inject a fake to drive the D7 branch and assert the keeper-window argv
// without a real tmux server.
type captainTmuxOps interface {
	// SessionExists reports whether a tmux session named sess is live. Used by
	// the D7 pre-flight to decide reap-then-recreate vs. plain create.
	SessionExists(ctx context.Context, sess string) (bool, error)
	// KillSession tears down a stale session (D7 reap). Idempotent.
	KillSession(ctx context.Context, sess string) error
	// SpawnKeeperWindow adds the sibling keeper window running the keeper argv
	// built from opts. Returns the window Outcome (Err non-nil on failure —
	// the captain WARNS but stays bootable, mirroring the bash launcher).
	SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome
	// AgentPanePID reads the PID of the captain agent window's active pane, for
	// captain.pid. Returns (0, err) when the pane PID can't be resolved.
	AgentPanePID(ctx context.Context, sess string) (int, error)
}

// osCaptainTmuxOps is the production captainTmuxOps backed by tmux.OSAdapter.
// The CLI uses the adapter DIRECTLY (no internal/daemon import) — the shared
// agentlaunch helper is built against the tmux interface, not the daemon, so the
// captain launcher does not drag in daemon deps (the ES2 review-A requirement).
type osCaptainTmuxOps struct {
	adapter ltmux.OSAdapter
}

func (o osCaptainTmuxOps) SessionExists(ctx context.Context, sess string) (bool, error) {
	sessions, err := o.adapter.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s == sess {
			return true, nil
		}
	}
	return false, nil
}

func (o osCaptainTmuxOps) KillSession(ctx context.Context, sess string) error {
	return o.adapter.KillSession(ctx, sess)
}

func (o osCaptainTmuxOps) SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome {
	return agentlaunch.SpawnKeeperWindow(ctx, o.adapter, opts)
}

func (o osCaptainTmuxOps) AgentPanePID(ctx context.Context, sess string) (int, error) {
	// The agent window holds the captain pane; resolve its first-pane PID.
	return o.adapter.WindowPanePID(ctx, ltmux.WindowHandle(sess+":"+ltmux.WindowAgent))
}

// buildCaptainTmuxCmd assembles the exec.Cmd for the captain's agent-window
// session:
//
//	tmux new-session -d -s <session> -n agent -e HARMONIK_AGENT=<name> \
//	  claude --dangerously-skip-permissions --remote-control <name> --session-id <id>
//
// -n agent names the first window so the keeper can target "<session>:agent"
// (window-nesting, hk-z036). --dangerously-skip-permissions mirrors
// captain-launch.sh: a remote-control captain that hit a permission prompt would
// wedge unattended, so it is part of the launcher's correctness contract.
// Returning the fully-built *exec.Cmd (rather than running it inline) is what
// lets the test assert the exact argv via the injected run func.
func buildCaptainTmuxCmd(name, tmuxSession, sessionID string) *exec.Cmd {
	return exec.Command(
		"tmux", "new-session", "-d",
		"-s", tmuxSession,
		"-n", ltmux.WindowAgent,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--remote-control", name,
		"--session-id", sessionID,
	)
}

// runCaptainSubcommand is the main.go entry point for `harmonik captain` and
// `harmonik start captain`. It wires the production run + keeper-enable funcs and
// the production tmux ops.
func runCaptainSubcommand(subArgs []string) int {
	return runCaptainLaunchWithOps(subArgs, runCaptainTmux, runKeeperEnable, osCaptainTmuxOps{adapter: ltmux.OSAdapter{}})
}

// buildCaptainKeeperConfig assembles the enableConfig used to wire keeper hooks
// for a freshly-launched captain. Mirrors runKeeperEnableEntry's resolution
// (auto-detected scripts dir, ~/.claude/settings.json) so the launcher and
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

// runCaptainLaunch keeps the hk-ly0n/hk-igek signature for back-compat with the
// existing argv + keeper-enable tests. It delegates to runCaptainLaunchWithOps
// with the production tmux ops.
func runCaptainLaunch(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn) int {
	return runCaptainLaunchWithOps(subArgs, run, enableKeeper, osCaptainTmuxOps{adapter: ltmux.OSAdapter{}})
}

// captainTmuxSessionName resolves the tmux session name for the captain:
//   - an explicit --tmux value wins (operator override / back-compat);
//   - otherwise the HASHED namespace harmonik-<hash>-captain, computed in-process
//     from the realpath'd project dir (mirrors captain-launch.sh's
//     `harmonik project-hash` + "harmonik-${HASH}-captain", but no shell-out).
//
// Resolving the realpath first matches lifecycle.ComputeProjectHash's contract
// (callers resolve symlinks before hashing) so the Go launcher and the daemon's
// orphan sweep agree on the session name.
func captainTmuxSessionName(explicitTmux, project string) (string, error) {
	if explicitTmux != "" {
		return explicitTmux, nil
	}
	realDir, err := filepath.EvalSymlinks(project)
	if err != nil {
		// Fall back to the un-resolved abs path: better a deterministic name than
		// a failed launch. EvalSymlinks only fails when a path component is
		// missing, which a valid project root should not hit.
		realDir = project
	}
	hash := lifecycle.ComputeProjectHash(realDir)
	return lifecycle.TmuxSessionName(hash, "captain"), nil
}

// runCaptainLaunchWithOps is the full-parity launch core. Split from
// runCaptainSubcommand so tests can inject (a) the agent-window run func, (b) the
// keeper-enable seam, and (c) the captainTmuxOps for the D7 pre-flight +
// keeper-window + pid steps — all WITHOUT a real tmux server.
//
// Exit codes:
//
//	0  — captain launched (a keeper-enable OR keeper-window failure only WARNS;
//	     sentinel/pid write failures also WARN — the captain stays bootable)
//	1  — flag/arg error, non-UUIDv4 --session-id, cwd resolution, D7 reap failure,
//	     or the agent-window tmux launch itself failing.
func runCaptainLaunchWithOps(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn, ops captainTmuxOps) int {
	fs := flag.NewFlagSet("captain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity")
	tmuxFlag := fs.String("tmux", "", "tmux session name (default: harmonik-<project-hash>-captain)")
	projectFlag := fs.String("project", "", "project directory (default: current working directory)")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to launch with (minted when absent)")
	noKeeperFlag := fs.Bool("no-keeper", false, "skip wiring the keeper hooks into ~/.claude/settings.json")
	warnAbsFlag := fs.Int64("warn-abs-tokens", keeper.DefaultWarnAbsTokens, "keeper WARN band (absolute tokens)")
	actAbsFlag := fs.Int64("act-abs-tokens", keeper.DefaultActAbsTokens, "keeper ACT/restart band (absolute tokens)")

	if err := fs.Parse(subArgs); err != nil {
		// flag prints its own message; ErrHelp also lands here.
		return 1
	}

	name := *nameFlag
	if name == "" {
		fmt.Fprintln(os.Stderr, "harmonik captain: --name must not be empty")
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

	tmuxSession, err := captainTmuxSessionName(*tmuxFlag, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: cannot resolve tmux session name: %v\n", err)
		return 1
	}

	// Resolve the session-id: validate a supplied one (reject non-UUIDv4), mint a
	// fresh UUIDv4 otherwise. keeper.IsPrimarySID is the canonical lowercase
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

	ctx := context.Background()

	// D7 idempotent pre-flight: if the target session already exists (the keeper
	// outlived a stopped agent), REAP it before recreating. tmux `new-session -s`
	// on an existing name errors "duplicate session" (captain-launch.sh:80) — the
	// native launcher must never throw that. Killing the whole session tears down
	// BOTH the stale agent and keeper windows, so the recreate below brings up a
	// fresh, correctly-bound pair (and the keeper rebinds to the new agent
	// window). A list/kill failure WARNS but does not block the launch (the create
	// will surface a real collision as a tmux error).
	if exists, lerr := ops.SessionExists(ctx, tmuxSession); lerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: could not check for an existing session %q: %v — proceeding\n", tmuxSession, lerr)
	} else if exists {
		fmt.Printf("captain: existing tmux session %q found (keeper likely outlived a stopped agent) — reaping before recreate (D7)\n", tmuxSession)
		if kerr := ops.KillSession(ctx, tmuxSession); kerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: failed to reap stale session %q: %v\n", tmuxSession, kerr)
			return 1
		}
	}

	// Wire keeper hooks BEFORE launching tmux so the new `claude` session reads
	// the statusLine + Stop + PreCompact stanzas at session start. A failure here
	// only WARNS — the captain must stay bootable even if scripts can't be found.
	if !*noKeeperFlag {
		cfg, cerr := buildCaptainKeeperConfig(name, project)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: %v\n", cerr)
			return 1
		}
		if rc := enableKeeper(cfg, os.Stdout, os.Stderr); rc != 0 {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper enable returned %d — launching anyway; "+
				"run `harmonik keeper enable %s` manually to wire keeper hooks\n", rc, name)
		}
	}

	// 1) Launch the captain agent window. The agent-window run func is the hk-ly0n
	//    test seam; the assembled command is `tmux new-session -d -s <session>
	//    -n agent ... claude ...`.
	cmd := buildCaptainTmuxCmd(name, tmuxSession, sessionID)
	if rerr := run(cmd); rerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: launch tmux session %q: %v\n", tmuxSession, rerr)
		return 1
	}

	// 2) Write captain.sentinel + captain.pid to .harmonik/cognition/ so the
	//    daemon orphan sweep skips the captain session while it is live
	//    (PL-006d ii). THE D6 FIX. Best-effort: a write failure WARNS — the
	//    captain is already up, and the sweep also probes the live tmux session
	//    (probeCaptainSentinel PRIMARY path) so a missing pid is recoverable.
	if werr := writeCaptainSentinelAndPID(ctx, ops, project, tmuxSession); werr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: %v — the daemon orphan sweep may reap this captain "+
			"until the sentinel/pid is written; re-run the launcher or `harmonik supervise` to refresh\n", werr)
	}

	// 3) Arm the keeper WATCHER in a sibling "keeper" window with the real
	//    warn/act band (the parity gap the old bare launcher left open — settings
	//    stanzas alone never drive warn/act/restart). Built via the SHARED
	//    agentlaunch helper. Best-effort: a keeper-window failure WARNS — the
	//    captain agent window is already live.
	//
	//    --respawn-cmd seam (ES3 / hk-z1rj): the dead-pane self-heal respawn
	//    command goes here. ES2 leaves it empty (no respawn arming) until the
	//    `harmonik captain respawn --session-id … --tmux …` subcommand lands in
	//    ES3; at that point set RespawnCmd to that subcommand's invocation so the
	//    keeper relaunches ONLY the agent window with --resume <sid>.
	//    TODO(hk-z1rj): set RespawnCmd to the `harmonik captain respawn` invocation.
	if !*noKeeperFlag {
		keeperBin, exErr := os.Executable()
		if exErr != nil {
			keeperBin = "harmonik" // fallback: rely on PATH
		}
		outcome := ops.SpawnKeeperWindow(ctx, agentlaunch.KeeperWindowOpts{
			KeeperBin:     keeperBin,
			AgentName:     name,
			Session:       tmuxSession,
			ProjectDir:    project,
			WarnOnly:      false, // captain is FORCE-CUT: full warn→act→restart band.
			WarnAbsTokens: *warnAbsFlag,
			ActAbsTokens:  *actAbsFlag,
			RespawnCmd:    "", // TODO(hk-z1rj, ES3): wire `harmonik captain respawn`.
		})
		if outcome.Err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper watcher window failed (%v) — launching anyway; "+
				"the captain has NO warn/act/restart watcher until you arm one manually with "+
				"`harmonik keeper --agent %s --tmux %s:%s --warn-abs-tokens %d --act-abs-tokens %d`\n",
				outcome.Err, name, tmuxSession, ltmux.WindowAgent, *warnAbsFlag, *actAbsFlag)
		}
	}

	fmt.Printf("captain launched: name=%q tmux=%q session_id=%s project=%q warn_abs=%d act_abs=%d\n",
		name, tmuxSession, sessionID, project, *warnAbsFlag, *actAbsFlag)
	if *noKeeperFlag {
		fmt.Printf("captain launched; keeper wiring skipped (--no-keeper) — run `harmonik keeper enable %s` and arm a watcher to wire warn/act.\n", name)
	} else {
		fmt.Printf("captain launched; keeper hooks wired + watcher armed in sibling '%s:%s' window (pass --no-keeper to skip).\n", tmuxSession, ltmux.WindowKeeper)
	}
	return 0
}

// writeCaptainSentinelAndPID writes .harmonik/cognition/captain.sentinel
// (schema_version=1, mirroring supervisor.sentinel) and captain.pid (the agent
// pane PID) so the daemon orphan sweep skips the live captain session (PL-006d
// ii). The path layout MUST match orphansweep.go's captainSentinelPath /
// captainPidfilePath (.harmonik/cognition/) or the sweep won't find them.
//
// The pid is resolved from the live agent pane via ops.AgentPanePID; on failure
// the sentinel is still written (the sweep's PRIMARY path probes the live tmux
// session, so a missing pid degrades gracefully to that probe) and the pid write
// is skipped with the error returned for a WARN.
func writeCaptainSentinelAndPID(ctx context.Context, ops captainTmuxOps, project, tmuxSession string) error {
	cognitionDir := filepath.Join(project, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		return fmt.Errorf("create cognition dir %q: %w", cognitionDir, err)
	}
	sentinelPath := filepath.Join(cognitionDir, "captain.sentinel")
	if err := os.WriteFile(sentinelPath, []byte("schema_version=1\n"), 0o644); err != nil {
		return fmt.Errorf("write captain.sentinel: %w", err)
	}

	pid, perr := ops.AgentPanePID(ctx, tmuxSession)
	if perr != nil || pid <= 0 {
		return fmt.Errorf("captain.sentinel written but could not resolve agent pane PID for captain.pid (%v)", perr)
	}
	pidPath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		return fmt.Errorf("write captain.pid: %w", err)
	}
	return nil
}
