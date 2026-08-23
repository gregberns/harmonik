package main

import (
	"fmt"
	"os"
	"strings"
)

const exitUnknownSubcommand = 2

func unknownSubcommand(args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}
	arg := args[1]
	if arg == "" || strings.HasPrefix(arg, "-") {
		return "", false
	}
	return arg, true
}

func daemonStartRefusal(args []string) string {
	var b strings.Builder

	if len(args) < 2 {
		b.WriteString("harmonik: no subcommand given — this does not start a daemon\n")
	} else {
		b.WriteString("harmonik: no subcommand given, only flags — this does not start a daemon\n")
		if trailing := trailingPositional(args); trailing != "" {
			fmt.Fprintf(&b, "  %q was ignored: a subcommand must come first, before any flags (see SUBCOMMANDS below)\n", trailing)
		}
	}

	b.WriteString("  To start a daemon, name it: `harmonik start daemon [--project DIR] [flags]`\n")
	return b.String()
}

func trailingPositional(args []string) string {
	if len(args) < 3 {
		return ""
	}
	last := args[len(args)-1]
	if last == "" || strings.HasPrefix(last, "-") {
		return ""
	}
	prev := args[len(args)-2]
	if strings.HasPrefix(prev, "-") {
		return ""
	}
	return last
}

func harmonikUsage() {
	fmt.Fprint(os.Stderr, `harmonik — agent-driven bead execution daemon

USAGE
  harmonik <subcommand> [flags]

  Every subcommand is a verb. Nothing starts a daemon by accident: "harmonik"
  on its own, and "harmonik --project DIR", print this help and exit 2.
  To start a daemon, name it: "harmonik start daemon".

SUBCOMMANDS
  version          Print semver + commit hash and exit (also: --version);
                   "version --binary PATH [--contains COMMIT]" reads a binary's
                   embedded vcs.revision stamp — the build-provenance check that
                   generalises across fixes (version --help lists the exit codes)
  init             Bootstrap a new project: create .harmonik/, init beads DB, write configs, render AGENTS.md
  start            Launch a daemon or an agent session (start daemon | start captain | start crew <name> | start commodore | start admiral | start assessor); "start daemon" is the ONLY way to start a daemon
  sync-assets      Reconcile a project's instruction files with the binary's embedded assets (dry-run by default)
  run              Legacy/solo-bootstrap: submit to a running daemon, else run inline and exit
  handler          Inspect or resume a paused handler
  queue            Submit or inspect the bead queue (daemon must be running)
  subscribe        Stream daemon events (run_completed/run_failed/run_stale/heartbeat) as NDJSON
  comms            Agent-to-agent messaging bus (send/recv/who/log/join/leave)
  crew             Captain & crew session management (start/stop/list)
  reconcile        Close in_progress beads whose implementation has merged
  confirm-verdict  NOT CONNECTED — nothing parks a reconciliation verdict, so this always exits 16
  veto-verdict     NOT CONNECTED — nothing parks a reconciliation verdict, so this always exits 16
  graph            Workflow graph utilities (validate, etc.)
  promote          Cherry-pick banked SHA(s) to target with build gate + push, or open a PR (--pr)
  release          Release ledger management (ledger, certify, yank)
  supervise        Manage the supervisor/cognition process (start/stop/status/attach/restart/logs)
  keeper           Context watcher for a managed agent pane (session-keeper Phase-1)
  sleep            Park all LLM sessions now (manual quiesce override; gated on GenuineDrain unless --force)
  wake             Wake sleeping LLM sessions (--agent <name> or --all; fleet-stall escape hatch)
  beads-merge      Git merge-driver for .beads/issues.jsonl (union-by-bead-ID)
  beads-dedup      Deduplicate .beads/issues.jsonl in-place (keeps newest record per bead ID)
  smoke            5-signal end-to-end verification of a live daemon (hk-4rkrg)
  harness          Run the scenario harness (--list, --dry-run, --cadence, --scenario)
  goal-keeper      Update .harmonik/intent/goal-state.json from operator comms (flywheel V6)
  project-hash     Print the PL-006a project hash for a directory (no daemon required)
  remote-control-prefix  Print the per-project Claude RC label prefix (no daemon required)
  tmux-start       Create a detached tmux session and attach to it (starts no daemon)
  hook-relay       Forward a Claude hook event to the daemon (internal use)
  usage            Token cost analysis: join transcripts × events by run_id (no daemon required)

DAEMON FLAGS (used with "start daemon")
  --project DIR          Project directory (default: current working directory)
  --max-concurrent N     Max simultaneous beads (default 1)
  --auto-pull            Enable br-ready fallback poll (historical topology; default OFF)
  --no-auto-pull         No-op alias; queue-only is the default (back-compat)

EXAMPLES
  # Canonical dispatch: start one persistent daemon (queue-only), then submit
  # beads to its queue. This is the primary path for ongoing work.
  harmonik start daemon --project /path/to/project --no-auto-pull --max-concurrent 4
  harmonik queue submit --beads hk-abc123,hk-def456
  harmonik subscribe --types run_completed,run_failed --json

  # Legacy/solo-bootstrap: submits to a running daemon if one exists, else runs
  # the beads inline and exits on completion.
  harmonik run hk-abc123
  harmonik run --beads hk-abc123,hk-def456 --max-concurrent 2

Run 'harmonik <subcommand> --help' for subcommand-specific flags.
`)
}
