package supervisecmd

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// RunRestart implements `harmonik supervise restart`.
//
// Stops the running supervisor, re-reads config.json, then starts a new shim.
// The restart sequence: stop → write fresh config → start.
//
// Exit codes:
//
//	0  — supervisor restarted
//	1  — argument or I/O error
//
// Spec ref: process-lifecycle.md §4.10 PL-028d.
func RunRestart(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	var watchRestart bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if _, err := fmt.Fprint(stdout, restartUsage); err != nil {
				return 1
			}
			return 0
		case args[i] == "--watch-restart":
			watchRestart = true
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise restart: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}

	stopArgs := []string{"--project", projectDir}
	if code := RunStop(stopArgs, stdout, stderr); code != 0 {
		if _, err := fmt.Fprintf(stderr, "harmonik supervise restart: stop failed (exit %d)\n", code); err != nil {
			return 1
		}
		return 1
	}

	if _, statErr := os.Stat(ConfigPath(projectDir)); statErr == nil {
		if _, err := ReadConfig(projectDir); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise restart: read config: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	} else if !os.IsNotExist(statErr) {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise restart: stat config: %v\n", statErr); writeErr != nil {
			return 1
		}
		return 1
	}

	startArgs := []string{"--project", projectDir}
	if watchRestart {
		startArgs = append(startArgs, "--watch-restart")
	}
	return RunStart(startArgs, stdout, stderr)
}

const restartUsage = `harmonik supervise restart — stop and restart the supervisor

USAGE
  harmonik supervise restart [--project DIR] [--watch-restart]

FLAGS
  --project DIR    Project directory (default: current working directory)
  --watch-restart  Enable restart-on-crash shim in the new supervisor

EXIT CODES
  0  Success
  1  Argument or I/O error

NOTES
  Re-reads config.json before relaunching. Parameter changes take effect on restart.
`
