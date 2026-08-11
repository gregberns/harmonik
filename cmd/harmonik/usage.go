package main

import (
	"fmt"
	"os"
	"strings"
)

// exitUnknownSubcommand is the exit code for an argument that names no
// subcommand. It matches the code `harmonik agent brief` already documents for
// an unrecognised verb.
const exitUnknownSubcommand = 2

// unknownSubcommand reports the first argument, and true, when that argument is
// a positional word rather than a flag.
//
// It is only correct at the END of run's subcommand chain. Every block in that
// chain returns, so an argument still in hand at the end of it matched no verb.
// The chain stays the single source of truth for which verbs exist. A second
// list of verb names here would drift out of step with it.
//
// A leading "-" is never a subcommand, so a flag-first argv is declined here and
// caught by the daemon-start refusal instead — see daemonStartRefusal.
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

// daemonStartRefusal is the message for an argv that reached the end of the verb
// chain without naming a daemon. It tells the operator what they typed, what it
// used to do, and the one spelling that starts a daemon now.
//
// The trailing-word case is worth its own sentence. `harmonik --project DIR
// status` reads as a status check and there is no `status` subcommand, so the
// word was silently ignored and a daemon started instead. Naming the ignored
// word is what turns a confusing refusal into an obvious one.
func daemonStartRefusal(args []string) string {
	var b strings.Builder

	if len(args) < 2 {
		b.WriteString("harmonik: no subcommand given — this does not start a daemon\n")
	} else {
		b.WriteString("harmonik: no subcommand given, only flags — this does not start a daemon\n")
		if trailing := trailingPositional(args); trailing != "" {
			// Do NOT suggest `harmonik <trailing> …` here. The word that lands in
			// this position is usually one that is not a subcommand at all —
			// `status` is the case that caused the bug — so echoing it back as a
			// command recommends something that does not exist.
			fmt.Fprintf(&b, "  %q was ignored: a subcommand must come first, before any flags (see SUBCOMMANDS below)\n", trailing)
		}
	}

	b.WriteString("  To start a daemon, name it: `harmonik start daemon [--project DIR] [flags]`\n")
	return b.String()
}

// trailingPositional reports the first bare word after a flag, which is the
// shape of `harmonik --project DIR status`. It is a hint for the error message
// and deliberately approximate: a flag VALUE is a bare word too, so only the
// LAST argument is considered, and only when it is not itself a flag.
func trailingPositional(args []string) string {
	if len(args) < 3 {
		return ""
	}
	last := args[len(args)-1]
	if last == "" || strings.HasPrefix(last, "-") {
		return ""
	}
	// The last word is a flag's value when the argument before it is a flag that
	// takes one. Distinguishing those needs the flag set, which is not built yet,
	// so require a preceding bare word: `--project DIR status` has one, and
	// `--project DIR` does not.
	prev := args[len(args)-2]
	if strings.HasPrefix(prev, "-") {
		return ""
	}
	return last
}

// harmonikUsage prints the top-level help for the harmonik command and is
// assigned to flag.Usage so that both "harmonik --help" and flag parse errors
// show the full subcommand listing instead of the bare flag default.
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
