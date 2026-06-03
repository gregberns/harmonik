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
//	--tmux <target>  tmux pane target (optional; injected into on warn crossing)
//	--warn-pct N     context-use percentage that triggers a warning (default 80)
//	--act-pct N      context-use percentage that triggers handoff action (default 90; reserved Phase-2)
//
// Behaviour (Phase-1 warn-mode):
//  1. Acquire .harmonik/keeper/<agent>.lock; exit 2 if another live keeper holds it.
//  2. Check .harmonik/keeper/<agent>.managed; if absent, log no-op message and exit 0.
//  3. If present: start the watcher loop. On the first upward crossing of warn-pct,
//     inject a wrap-up-warning prompt into the managed pane (via --tmux or derived target)
//     and emit session_keeper_warn. Emit session_keeper_no_gauge when the gauge file
//     is absent or stale. Block until SIGINT/SIGTERM.
//
// Exit codes:
//
//	0  — exited cleanly (no-op or signal shutdown)
//	1  — argument or I/O error
//	2  — lock already held by another keeper
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-8vzek.
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
	fs.IntVar(&actPctFlag, "act-pct", 90, "context-use percentage that triggers handoff action (Phase-2; reserved)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if agentFlag == "" {
		fmt.Fprintln(os.Stderr, "harmonik keeper: --agent is required")
		return 1
	}

	// Phase-2 handoff action is reserved; suppress "assigned but not used".
	_ = actPctFlag

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
	fmt.Fprintf(os.Stderr, "keeper started for %s (warn-pct=%d, tmux=%q)\n", agentFlag, warnPctFlag, tmuxFlag)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := keeper.WatcherConfig{
		AgentName:  agentFlag,
		ProjectDir: projectDir,
		WarnPct:    float64(warnPctFlag),
		TmuxTarget: tmuxFlag,
	}
	w := keeper.NewWatcher(cfg, keeper.NewFileEmitter(projectDir))
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
  --tmux <target>   tmux pane target (optional; injected into on warn-pct crossing)
  --warn-pct N      Context-use percentage that triggers a warning (default 80)
  --act-pct N       Context-use percentage that triggers handoff action (default 90; Phase-2 reserved)

BEHAVIOUR (Phase-1 warn-mode)
  1. Acquires .harmonik/keeper/<agent>.lock; exits 2 if another keeper is live.
  2. Checks .harmonik/keeper/<agent>.managed; exits 0 (no-op) if absent.
  3. If managed: starts the watcher loop — polls .harmonik/keeper/<agent>.ctx every 5s.
     On the first upward crossing of --warn-pct, injects a wrap-up-warning into the
     tmux pane (if --tmux is set) and emits session_keeper_warn.
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
