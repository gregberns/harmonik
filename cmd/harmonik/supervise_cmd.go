package main

import (
	"fmt"
	"os"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

func runSuperviseSubcommand(args []string) int {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}

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
	case "ps":
		return supervisecmd.RunPs(subArgs, os.Stdout, os.Stderr)
	case "attach":
		return supervisecmd.RunAttach(subArgs, os.Stdout, os.Stderr)
	case "restart":
		return supervisecmd.RunRestart(subArgs, os.Stdout, os.Stderr)
	case "logs":
		return supervisecmd.RunLogs(subArgs, os.Stdout, os.Stderr)
	case "pause":
		return supervisecmd.RunPause(subArgs, os.Stdout, os.Stderr)
	case "resume":
		return supervisecmd.RunResume(subArgs, os.Stdout, os.Stderr)
	case "reap":
		return supervisecmd.RunReap(subArgs, os.Stdout, os.Stderr)
	case "_shim":
		return supervisecmd.RunShim(subArgs, os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr,
			"harmonik supervise: unrecognised verb %q; verbs are: start, stop, status, ps, attach, restart, logs, pause, resume, reap\n",
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
  ps       Print canonical supervisor process signatures and tmux sessions
  attach   Attach terminal to the flywheel tmux session
  restart  Stop and restart the supervisor (re-reads config.json)
  logs     Capture recent flywheel pane output
  pause    Pause daemon dispatch (no new beads dispatched; in-flight complete)
  resume   Resume daemon dispatch after a pause
  reap     Reap dead flywheel tmux orphan sessions (boot also auto-reaps)

EXIT CODES
   0  Success
   1  Argument or operational error
   2  Unrecognised verb
  17  Daemon not running (start/restart/pause/resume)
  24  Flywheel session already exists (start)
  25  Supervisor already running (start)

EXAMPLES
  harmonik supervise start --watch-restart
  harmonik supervise status --json
  harmonik supervise ps
  harmonik supervise logs --lines 500
  harmonik supervise attach
  harmonik supervise stop
  harmonik supervise pause
  harmonik supervise resume
  harmonik supervise reap --json
`
