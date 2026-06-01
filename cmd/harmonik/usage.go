package main

import (
	"fmt"
	"os"
)

// harmonikUsage prints the top-level help for the harmonik command and is
// assigned to flag.Usage so that both "harmonik --help" and flag parse errors
// show the full subcommand listing instead of the bare flag default.
func harmonikUsage() {
	fmt.Fprint(os.Stderr, `harmonik — agent-driven bead execution daemon

USAGE
  harmonik [--project DIR] [--max-concurrent N]
  harmonik <subcommand> [flags]

SUBCOMMANDS
  run          Legacy/solo-bootstrap: submit to a running daemon, else run inline and exit
  handler      Inspect or resume a paused handler
  queue        Submit or inspect the bead queue (daemon must be running)
  reconcile    Close in_progress beads whose implementation has merged
  graph        Workflow graph utilities (validate, etc.)
  supervise    Manage the supervisor/cognition process (start/stop/status/attach/restart/logs)
  beads-merge  Git merge-driver for .beads/issues.jsonl (union-by-bead-ID)
  tmux-start   Bootstrap a tmux session and start the daemon inside it
  hook-relay   Forward a Claude hook event to the daemon (internal use)

DAEMON FLAGS (used without a subcommand)
  --project DIR          Project directory (default: current working directory)
  --max-concurrent N     Max simultaneous beads (default 1)
  --auto-pull            Enable br-ready fallback poll (historical topology; default OFF)
  --no-auto-pull         No-op alias; queue-only is the default (back-compat)

EXAMPLES
  # Canonical dispatch: start one persistent daemon (queue-only), then submit
  # beads to its queue. This is the primary path for ongoing work.
  harmonik --project /path/to/project --no-auto-pull --max-concurrent 4
  harmonik queue submit --beads hk-abc123,hk-def456
  harmonik subscribe --types run_completed,run_failed --json

  # Legacy/solo-bootstrap: submits to a running daemon if one exists, else runs
  # the beads inline and exits on completion.
  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2

Run 'harmonik <subcommand> --help' for subcommand-specific flags.
`)
}
