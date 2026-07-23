package supervisecmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// RunLogs implements `harmonik supervise logs`.
//
// Captures recent pane output from the flywheel tmux session via
// `tmux capture-pane -p -S -<n>`.
//
// Exit codes:
//
//	0  — output written to stdout
//	1  — argument error or tmux failure
//
// Spec ref: process-lifecycle.md §4.10 PL-028d.
func RunLogs(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	lines := 200 // default: last 200 lines

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if _, err := fmt.Fprint(stdout, logsUsage); err != nil {
				return 1
			}
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--lines" && i+1 < len(args):
			i++
			if _, err := fmt.Sscanf(args[i], "%d", &lines); err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise logs: invalid --lines value %q\n", args[i]); writeErr != nil {
					return 1
				}
				return 1
			}
		case strings.HasPrefix(args[i], "--lines="):
			val := strings.TrimPrefix(args[i], "--lines=")
			if _, err := fmt.Sscanf(val, "%d", &lines); err != nil {
				if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise logs: invalid --lines value %q\n", val); writeErr != nil {
					return 1
				}
				return 1
			}
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise logs: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}

	sessionName := FlywheelSessionName(projectDir)
	startLine := fmt.Sprintf("-%d", lines)

	//nolint:gosec // G204: sessionName derived from project hash; startLine is a negative integer string
	cmd := exec.CommandContext(context.Background(), "tmux", "capture-pane", "-p", "-S", startLine, "-t", sessionName)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		if exitCode != 0 {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik supervise logs: tmux capture-pane failed (exit %d); is the session alive?\n", exitCode); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	return 0
}

const logsUsage = `harmonik supervise logs — capture recent flywheel pane output

USAGE
  harmonik supervise logs [--project DIR] [--lines N]

FLAGS
  --project DIR  Project directory (default: current working directory)
  --lines N      Number of lines to capture (default: 200)

EXIT CODES
  0  Success
  1  Argument error or tmux failure

NOTES
  Runs: tmux capture-pane -p -S -<N> -t harmonik-<project_hash>-flywheel
  The session must exist (i.e., the supervisor must have been started).
`
