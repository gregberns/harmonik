package main

import (
	"fmt"
	"os"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

// runSuperviseSubcommand dispatches `harmonik supervise <verb> [flags]`.
//
// Pre-flag.Parse dispatch per PL-028d: this runs before flag.Parse so the
// global flag set does not reject subcommand-specific flags.
//
// Exit codes:
//
//	0  — success
//	1  — argument or operational error
//	2  — unrecognised verb
//	17 — daemon not running (start/restart only)
//	25 — supervisor already running (start only)
//
// Spec ref: process-lifecycle.md §4.10 PL-028d.
func runSuperviseSubcommand(args []string) int {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}

	// --help/-h on verb position.
	if verb == "--help" || verb == "-h" || verb == "" {
		fmt.Print(superviseTopUsage)
		return 0
	}

	subArgs := []string{}
	if len(args) > 1 {
		subArgs = args[1:]
	}

	switch verb {
	case "start":
		return supervisecmd.RunStart(subArgs, os.Stdout, os.Stderr)
	case "stop":
		return supervisecmd.RunStop(subArgs, os.Stdout, os.Stderr)
	case "status":
		return supervisecmd.RunStatus(subArgs, os.Stdout, os.Stderr)
	case "attach":
		return supervisecmd.RunAttach(subArgs, os.Stdout, os.Stderr)
	case "restart":
		return supervisecmd.RunRestart(subArgs, os.Stdout, os.Stderr)
	case "logs":
		return supervisecmd.RunLogs(subArgs, os.Stdout, os.Stderr)
	case "_shim":
		// Internal subcommand: runs inside the flywheel tmux pane.
		return supervisecmd.RunShim(subArgs, os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr,
			"harmonik supervise: unrecognised verb %q; verbs are: start, stop, status, attach, restart, logs\n",
			verb)
		return 2
	}
}

const superviseTopUsage = `harmonik supervise — manage the supervisor (cognition/flywheel) process

USAGE
  harmonik supervise <verb> [flags]

VERBS
  start    Launch the supervisor in a tmux session
  stop     Terminate the supervisor
  status   Show supervisor process state (file-surface, no daemon required)
  attach   Attach terminal to the flywheel tmux session
  restart  Stop and restart the supervisor (re-reads config.json)
  logs     Capture recent flywheel pane output

EXIT CODES
   0  Success
   1  Argument or operational error
   2  Unrecognised verb
  17  Daemon not running (start/restart)
  25  Supervisor already running (start)

EXAMPLES
  harmonik supervise start --watch-restart
  harmonik supervise status --json
  harmonik supervise logs --lines 500
  harmonik supervise attach
  harmonik supervise stop
`
