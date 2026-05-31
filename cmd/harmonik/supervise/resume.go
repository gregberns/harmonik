package supervisecmd

// resume.go — `harmonik supervise resume` (hk-ry8q1).
//
// Sends an operator-resume request to the running daemon via its Unix socket.
// The daemon responds by emitting an operator_resuming event and transitioning
// the active queue back to active status.
//
// Exit codes:
//
//	0  — daemon acknowledged the resume (or was not paused)
//	1  — argument or I/O error
//	17 — daemon not running (socket absent or ECONNREFUSED)
//
// Spec ref: specs/operator-nfr.md §4.3.
// Bead ref: hk-ry8q1.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// RunResume implements `harmonik supervise resume`.
func RunResume(args []string, stdout, stderr io.Writer) int {
	var projectDir string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, resumeUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		default:
			fmt.Fprintf(stderr, "harmonik supervise resume: unknown argument %q\n", args[i])
			return 1
		}
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise resume: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	sockPath := daemonSockPath(projectDir)
	code := sendOperatorOp(context.Background(), sockPath, "operator-resume", stdout, stderr)
	if code == 0 {
		fmt.Fprintln(stdout, "harmonik supervise resume: daemon resumed")
	}
	return code
}

const resumeUsage = `harmonik supervise resume — resume the daemon dispatch loop after a pause

USAGE
  harmonik supervise resume [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
  0   Success (daemon resumed or was not paused)
  1   Argument or I/O error
  17  Daemon not running

NOTES
  Counterpart to 'harmonik supervise pause'.
  Emits operator_resuming and re-enables bead dispatch.
`
