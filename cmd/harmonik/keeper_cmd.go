package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gregberns/harmonik/internal/keeper"
)

// runKeeperSubcommand implements `harmonik keeper`.
//
// Flags:
//
//	--agent <name>   agent name (required); identifies the lockfile and .managed marker
//	--tmux <target>  tmux pane target (optional; injected into on warn/act crossing)
//	--warn-pct N     context-use percentage that triggers a warning (default 80)
//	--act-pct N      context-use percentage that triggers handoff action (default 90; .managed-gated)
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
		agentFlag   string
		tmuxFlag    string
		warnPctFlag int
		actPctFlag  int
	)

	fs.StringVar(&agentFlag, "agent", "", "agent name (required)")
	fs.StringVar(&tmuxFlag, "tmux", "", "tmux pane target (optional; injected into on warn crossing)")
	fs.IntVar(&warnPctFlag, "warn-pct", 80, "context-use percentage that triggers a warning")
	fs.IntVar(&actPctFlag, "act-pct", 90, "context-use percentage that triggers handoff action (.managed-gated)")

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

	// Step 2: check .managed opt-in guard (fail-safe: absent = no-op).
	if !keeper.IsManaged(projectDir, agentFlag) {
		fmt.Fprintf(os.Stderr, "keeper: %s not opted-in (.managed marker missing); no-op\n", agentFlag)
		return 0
	}

	// Step 3: agent is managed — start the watcher and block until signal.
	fmt.Fprintf(os.Stderr, "keeper started for %s (warn-pct=%d, act-pct=%d, tmux=%q)\n",
		agentFlag, warnPctFlag, actPctFlag, tmuxFlag)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	emitter := keeper.NewFileEmitter(projectDir)

	cycler := keeper.NewCycler(keeper.CyclerConfig{
		AgentName:  agentFlag,
		ProjectDir: projectDir,
		TmuxTarget: tmuxFlag,
		ActPct:     float64(actPctFlag),
	}, emitter)

	// Crash recovery: if a previous keeper was killed mid-cycle, self-heal before
	// starting the watcher loop (resume any interrupted /clear, or abort cleanly).
	if recoverErr := cycler.RecoverFromCrash(ctx); recoverErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper: crash recovery: %v\n", recoverErr)
	}

	cfg := keeper.WatcherConfig{
		AgentName:  agentFlag,
		ProjectDir: projectDir,
		WarnPct:    float64(warnPctFlag),
		TmuxTarget: tmuxFlag,
		Cycler:     cycler,
	}
	w := keeper.NewWatcher(cfg, emitter)
	if runErr := w.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "harmonik keeper: watcher: %v\n", runErr)
		return 1
	}

	return 0
}

const keeperTopUsage = `harmonik keeper — context watcher for a managed agent pane (session-keeper, hk-ekap1)

USAGE
  harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N]

FLAGS
  --agent <name>    Agent name (required); identifies the lockfile and .managed marker
  --tmux <target>   tmux pane target (optional; injected into on warn/act-pct crossing)
  --warn-pct N      Context-use percentage that triggers a warning (default 80)
  --act-pct N       Context-use percentage that triggers handoff action (default 90; .managed-gated)

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
  Add to ~/.claude/settings.json:
    "statusLine": {
      "command": "HARMONIK_PROJECT=/path/to/project HARMONIK_AGENT=<agent> /path/to/scripts/keeper-statusline.sh"
    }

EXIT CODES
  0  Success (no-op or clean signal shutdown)
  1  Argument or I/O error
  2  Lock held by another live keeper

EXAMPLES
  harmonik keeper --agent orchestrator
  harmonik keeper --agent flywheel --tmux harmonik:0 --warn-pct 80
`
