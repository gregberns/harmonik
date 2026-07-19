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
			fmt.Fprint(stdout, restartUsage)
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
			fmt.Fprintf(stderr, "harmonik supervise restart: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	// Stop any running supervisor.
	stopArgs := []string{"--project", projectDir}
	if code := RunStop(stopArgs, stdout, stderr); code != 0 {
		fmt.Fprintf(stderr, "harmonik supervise restart: stop failed (exit %d)\n", code)
		return 1
	}

	// Re-read config.json to validate it's parseable before re-launch — but ONLY
	// when it exists. A MISSING config.json is NOT fatal: when reviving a
	// supervisor that never ran in this project (the standalone-daemon
	// supervisor-watchdog revive path — hk-ky7ye), there is no prior config to
	// carry forward, so restart must cold-start. start (RunStart) writes a fresh
	// config.json from project config + flags, exactly as a first-ever
	// `supervise start` does. Only a config that EXISTS but fails to parse is a
	// genuine corruption we refuse to relaunch over.
	if _, statErr := os.Stat(ConfigPath(projectDir)); statErr == nil {
		if _, err := ReadConfig(projectDir); err != nil {
			fmt.Fprintf(stderr, "harmonik supervise restart: read config: %v\n", err) //nolint:errcheck // diagnostic write to stderr/stdout; failure is non-actionable
			return 1
		}
	} else if !os.IsNotExist(statErr) {
		fmt.Fprintf(stderr, "harmonik supervise restart: stat config: %v\n", statErr) //nolint:errcheck // diagnostic write to stderr/stdout; failure is non-actionable
		return 1
	}

	// Re-launch via start (re-writes sentinel + config with fresh started_at).
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
