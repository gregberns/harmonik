package main

import (
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
//	--tmux <target>  tmux pane target (optional; reserved for future watcher logic)
//	--warn-pct N     context-use percentage that triggers a warning (default 80)
//	--act-pct N      context-use percentage that triggers handoff action (default 90)
//
// Behaviour (Phase-1 scaffold — no watcher/statusLine/injector logic):
//  1. Acquire .harmonik/keeper/<agent>.lock; exit 2 if another live keeper holds it.
//  2. Check .harmonik/keeper/<agent>.managed; if absent, log no-op message and exit 0.
//  3. If present: log started, block awaiting SIGINT/SIGTERM, release lock on exit.
//
// Exit codes:
//
//	0  — exited cleanly (no-op or signal shutdown)
//	1  — argument or I/O error
//	2  — lock already held by another keeper
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-fzzc6.
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
	fs.StringVar(&tmuxFlag, "tmux", "", "tmux pane target (optional; reserved for future watcher logic)")
	fs.IntVar(&warnPctFlag, "warn-pct", 80, "context-use percentage that triggers a warning")
	fs.IntVar(&actPctFlag, "act-pct", 90, "context-use percentage that triggers handoff action")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if agentFlag == "" {
		fmt.Fprintln(os.Stderr, "harmonik keeper: --agent is required")
		return 1
	}

	// Reserved for future phases; suppress "assigned but not used" until wired.
	_ = tmuxFlag
	_ = warnPctFlag
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

	// Step 3: agent is managed — block until signal.
	fmt.Fprintf(os.Stderr, "keeper started for %s\n", agentFlag)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	return 0
}

const keeperTopUsage = `harmonik keeper — context watcher for a managed agent pane (session-keeper, hk-ekap1)

USAGE
  harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N]

FLAGS
  --agent <name>    Agent name (required); identifies the lockfile and .managed marker
  --tmux <target>   tmux pane target (optional; reserved for future watcher logic)
  --warn-pct N      Context-use percentage that triggers a warning (default 80)
  --act-pct N       Context-use percentage that triggers handoff action (default 90)

BEHAVIOUR (Phase-1 scaffold)
  1. Acquires .harmonik/keeper/<agent>.lock; exits 2 if another keeper is live.
  2. Checks .harmonik/keeper/<agent>.managed; exits 0 (no-op) if absent.
  3. If managed: logs "keeper started for <agent>" and blocks awaiting SIGINT/SIGTERM.

EXIT CODES
  0  Success (no-op or clean signal shutdown)
  1  Argument or I/O error
  2  Lock held by another live keeper

EXAMPLES
  harmonik keeper --agent orchestrator
  harmonik keeper --agent flywheel --tmux harmonik:0
`
