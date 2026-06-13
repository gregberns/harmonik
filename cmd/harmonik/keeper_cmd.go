package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// runKeeperSubcommand implements `harmonik keeper`.
//
// Flags:
//
//	--agent <name>        agent name (required); identifies the lockfile and .managed marker
//	--tmux <target>       tmux pane target (optional; injected into on warn/act crossing)
//	--warn-pct N          context-use percentage that triggers a warning (default 80)
//	--act-pct N           context-use percentage that triggers handoff action (default 90; .managed-gated)
//	--window-size N       assumed context-window token size when gauge reports WindowSize==0 (default 200000)
//	--warn-abs-tokens N   absolute-token warn threshold (default 240000)
//	--act-abs-tokens N    absolute-token act threshold (default 300000)
//
// Behaviour (Phase-2, .managed-gated):
//  1. Acquire .harmonik/keeper/<agent>.lock; exit 2 if another live keeper holds it.
//  2. Check .harmonik/keeper/<agent>.managed; if absent, log no-op message and exit 0.
//  3. If present: run crash recovery (resume any interrupted cycle from a prior crash),
//     then start the watcher loop. On the first upward crossing of warn-pct, inject a
//     wrap-up-warning prompt into the managed pane (via --tmux) and emit
//     session_keeper_warn. On crossing act-pct with CrispIdle and no in-flight dispatch,
//     run the intent-preserving handoff→/clear→resume cycle. Emit session_keeper_no_gauge
//     when the gauge file is absent or stale. Block until SIGINT/SIGTERM.
//
// Exit codes:
//
//	0  — exited cleanly (no-op or signal shutdown)
//	1  — argument or I/O error
//	2  — lock already held by another keeper
//
// Spec ref: codename:session-keeper (hk-ekap1); beads hk-8vzek, hk-22i70, hk-lm9it.
func runKeeperSubcommand(args []string) int {
	fs := flag.NewFlagSet("keeper", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		agentFlag         string
		tmuxFlag          string
		warnPctFlag       int
		actPctFlag        int
		windowSizeFlag    int64
		warnAbsTokensFlag int64
		actAbsTokensFlag  int64
		respawnCmdFlag    string
	)

	fs.StringVar(&agentFlag, "agent", "", "agent name (required)")
	fs.StringVar(&tmuxFlag, "tmux", "", "tmux pane target (optional; injected into on warn crossing)")
	fs.IntVar(&warnPctFlag, "warn-pct", 80, "context-use percentage that triggers a warning")
	fs.IntVar(&actPctFlag, "act-pct", 90, "context-use percentage that triggers handoff action (.managed-gated)")
	fs.Int64Var(&windowSizeFlag, "window-size", 0, "assumed context-window token size when the gauge reports WindowSize==0 (default 200000)")
	fs.Int64Var(&warnAbsTokensFlag, "warn-abs-tokens", 0, "absolute-token warn threshold (default 240000)")
	fs.Int64Var(&actAbsTokensFlag, "act-abs-tokens", 0, "absolute-token act threshold (default 300000)")
	fs.StringVar(&respawnCmdFlag, "respawn-cmd", "", "shell command to re-launch the agent after it exits (supervised respawn path; hk-3w2)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if agentFlag == "" {
		fmt.Fprintln(os.Stderr, "harmonik keeper: --agent is required")
		return 1
	}

	projectDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper: cannot determine working directory: %v\n", err)
		return 1
	}

	// Step 1: acquire single-keeper lockfile.
	lock, err := keeper.AcquireLock(projectDir, agentFlag)
	if err != nil {
		if errors.Is(err, keeper.ErrLockHeld) {
			fmt.Fprintf(os.Stderr, "harmonik keeper: agent %q already has a live keeper; exiting\n", agentFlag)
			return 2
		}
		fmt.Fprintf(os.Stderr, "harmonik keeper: acquire lock: %v\n", err)
		return 1
	}
	defer func() { _ = lock.Release() }() //nolint:errcheck // best-effort on shutdown

	// Step 2: run doctor at boot as a loud diagnostic (non-fatal).
	{
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			settingsPath := home + "/.claude/settings.json"
			runKeeperDoctorAtBoot(projectDir, agentFlag, settingsPath)
		}
	}

	// Step 3: check .managed opt-in guard (fail-safe: absent = no-op).
	if !keeper.IsManaged(projectDir, agentFlag) {
		fmt.Fprintf(os.Stderr, "keeper: %s not opted-in (.managed marker missing); no-op\n", agentFlag)
		return 0
	}

	// Step 4: resolve the effective tmux target.
	// If --tmux was provided, use it as-is. Otherwise attempt to auto-derive the
	// session name from the harmonik convention: "harmonik-<hash12>-<agent>".
	resolvedTmux := keeper.ResolveTmuxTarget(projectDir, agentFlag, tmuxFlag, nil)
	if resolvedTmux != "" && resolvedTmux != tmuxFlag {
		fmt.Fprintf(os.Stderr, "keeper: auto-resolved tmux target from convention: %q\n", resolvedTmux)
	}

	// Step 5: agent is managed — start the watcher and block until signal.
	fmt.Fprintf(os.Stderr, "keeper started for %s (warn-pct=%d, act-pct=%d, tmux=%q)\n",
		agentFlag, warnPctFlag, actPctFlag, resolvedTmux)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	emitter := keeper.NewFileEmitter(projectDir)

	cycler := keeper.NewCycler(keeper.CyclerConfig{
		AgentName:       agentFlag,
		ProjectDir:      projectDir,
		TmuxTarget:      resolvedTmux,
		ActPct:          float64(actPctFlag),
		ActAbsTokens:    actAbsTokensFlag,
		WarnAbsTokens:   warnAbsTokensFlag,
		SendEscapeFn:    keeper.SendEscapeKey,
		BootGracePeriod: 5 * time.Minute, // hk-4f8: defer cycles during post-/session-resume boot
	}, emitter)

	// Crash recovery: if a previous keeper was killed mid-cycle, self-heal before
	// starting the watcher loop (resume any interrupted /clear, or abort cleanly).
	if recoverErr := cycler.RecoverFromCrash(ctx); recoverErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper: crash recovery: %v\n", recoverErr)
	}

	cfg := keeper.WatcherConfig{
		AgentName:          agentFlag,
		ProjectDir:         projectDir,
		WarnPct:            float64(warnPctFlag),
		TmuxTarget:         resolvedTmux,
		Cycler:             cycler,
		FallbackWindowSize: windowSizeFlag,
		WarnAbsTokens:      warnAbsTokensFlag,
		RespawnCmd:         respawnCmdFlag,
		// hitl-decisions K5 (hk-061): the keeper watch tick is the SOLE emitter of
		// decision_withdrawn(orphaned, by=keeper). Enable the orphan reaper on the
		// standalone keeper; it reuses the FileEmitter (appends to the same
		// events.jsonl) and derives EventsJSONLPath from ProjectDir in applyDefaults.
		ReapDecisions: true,
	}
	w := keeper.NewWatcher(cfg, emitter)
	if runErr := w.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "harmonik keeper: watcher: %v\n", runErr)
		return 1
	}

	return 0
}

// runKeeperSetDispatching implements `harmonik keeper set-dispatching <agent>`.
//
// Writes .harmonik/keeper/<agent>.dispatching so HoldingDispatch returns true.
// The orchestrator calls this before submitting a batch to the daemon queue so
// the session-keeper cycle defers the handoff action until dispatch completes.
//
// Exit codes:
//
//	0  — marker written successfully
//	1  — argument error, path-traversal validation failure, or I/O error
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-rc51s.
func runKeeperSetDispatching(args []string) int {
	fs := flag.NewFlagSet("keeper set-dispatching", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var projectFlag string
	fs.StringVar(&projectFlag, "project", "", "project directory (default: current working directory)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "harmonik keeper set-dispatching: agent name argument is required")
		return 1
	}
	agent := fs.Arg(0)
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper set-dispatching: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	if err := keeper.SetDispatching(projectFlag, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper set-dispatching: %v\n", err)
		return 1
	}
	return 0
}

// runKeeperClearDispatching implements `harmonik keeper clear-dispatching <agent>`.
//
// Removes .harmonik/keeper/<agent>.dispatching so HoldingDispatch returns false.
// Idempotent: an already-absent marker is not an error. The orchestrator calls
// this once all in-flight queue work has completed so the session-keeper cycle
// may resume normal threshold checks.
//
// Exit codes:
//
//	0  — marker removed (or was already absent)
//	1  — argument error, path-traversal validation failure, or I/O error
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-rc51s.
func runKeeperClearDispatching(args []string) int {
	fs := flag.NewFlagSet("keeper clear-dispatching", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var projectFlag string
	fs.StringVar(&projectFlag, "project", "", "project directory (default: current working directory)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "harmonik keeper clear-dispatching: agent name argument is required")
		return 1
	}
	agent := fs.Arg(0)
	if projectFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper clear-dispatching: cannot determine working directory: %v\n", err)
			return 1
		}
		projectFlag = wd
	}
	if err := keeper.ClearDispatching(projectFlag, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper clear-dispatching: %v\n", err)
		return 1
	}
	return 0
}

const keeperTopUsage = `harmonik keeper — context watcher for a managed agent pane (session-keeper, hk-ekap1)

USAGE
  harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N] [--warn-abs-tokens N] [--act-abs-tokens N]
  harmonik keeper enable <agent> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]
  harmonik keeper doctor <agent> [--project DIR]
  harmonik keeper set-dispatching <agent> [--project DIR]
  harmonik keeper clear-dispatching <agent> [--project DIR]

VERBS
  enable             Wire statusLine + Stop + PreCompact stanzas into ~/.claude/settings.json
                     (idempotent, JSON-aware, backs up first, normalizes env-var names).
                     Seeds HANDOFF-<agent>.md, validates tmux pane, prints the run command.
                     .managed creation requires --yes-destructive.
                     Run 'harmonik keeper enable --help' for full usage.
  doctor             Read-only drift validator: binary currency, all 3 hooks present,
                     gauge freshness, .idle written, .managed present, ANTHROPIC_API_KEY risk.
                     Exits non-zero on any gap.  Also runs automatically at keeper BOOT.
                     Run 'harmonik keeper doctor --help' for full usage.
  set-dispatching    Write the .dispatching marker for <agent>; HoldingDispatch → true.
                     Call BEFORE submitting a batch to the daemon queue so the keeper
                     cycle defers the handoff action while queue work is in flight.
  clear-dispatching  Remove the .dispatching marker for <agent>; HoldingDispatch → false.
                     Call when all in-flight queue work has completed. Idempotent.

FLAGS (watcher mode)
  --agent <name>         Agent name (required); identifies the lockfile and .managed marker
  --tmux <target>        tmux pane target (optional; injected into on warn/act-pct crossing)
  --warn-pct N           Context-use percentage that triggers a warning (default 80)
  --act-pct N            Context-use percentage that triggers handoff action (default 90; .managed-gated)
  --warn-abs-tokens N    Absolute-token warn threshold (default 240000); effective = min(warn-abs-tokens, warn-pct% * window)
  --act-abs-tokens N     Absolute-token act threshold (default 300000); effective = min(act-abs-tokens, act-pct% * window)
  --respawn-cmd <cmd>    Shell command to re-launch the agent when it exits (supervised respawn; hk-3w2).
                         After the gauge goes stale for 20s and the tmux pane is idle (shell prompt),
                         the keeper runs "sh -c <cmd>" to respawn the agent. Requires --tmux.
                         A 90s cooldown prevents tight respawn loops.
                         Example: --respawn-cmd '~/.claude/captain-tools/captain-launch.sh'

BEHAVIOUR (Phase-2, .managed-gated)
  1. Acquires .harmonik/keeper/<agent>.lock; exits 2 if another keeper is live.
  2. Checks .harmonik/keeper/<agent>.managed; exits 0 (no-op) if absent.
  3. If managed: runs crash recovery (resume any interrupted cycle from a prior crash),
     then starts the watcher loop — polls .harmonik/keeper/<agent>.ctx every 5s.
     On the first upward crossing of --warn-pct, injects a wrap-up-warning into the
     tmux pane (if --tmux is set) and emits session_keeper_warn.
     On crossing --act-pct with CrispIdle and no in-flight dispatch, runs the
     intent-preserving handoff→/clear→resume cycle (Cycler.MaybeRun).
     Emits session_keeper_no_gauge at boot and every 120s when the gauge file is absent
     or stale (a missing statusLine.command is visible, not silent).

GAUGE SETUP
  Add to ~/.claude/settings.json (via: harmonik keeper enable <agent> ...):
    "statusLine": {
      "type": "command",
      "command": "/path/to/scripts/keeper-statusline.sh"
    }
  The command carries no HARMONIK_PROJECT= prefix (ON-058b): project routing is
  resolved at runtime from each session's inherited HARMONIK_PROJECT env var.
  The script derives the agent name from the tmux session name at runtime, so a
  single project-agnostic entry works for all projects and concurrent sessions.

EXIT CODES (watcher mode)
  0  Success (no-op or clean signal shutdown)
  1  Argument or I/O error
  2  Lock held by another live keeper

EXIT CODES (set-dispatching / clear-dispatching)
  0  Success
  1  Argument, validation, or I/O error

EXAMPLES
  harmonik keeper --agent orchestrator
  harmonik keeper --agent flywheel --tmux harmonik:0 --warn-pct 80
  harmonik keeper set-dispatching orchestrator
  harmonik keeper clear-dispatching orchestrator
  harmonik keeper set-dispatching flywheel --project /path/to/project
`
