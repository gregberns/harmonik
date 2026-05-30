package supervisecmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// RunAttach implements `harmonik supervise attach`.
//
// Execve-replaces the current process with `tmux attach-session -t <session>`
// per PL-028d. The attach verb MUST execve into the tmux session (not spawn a
// subprocess) so the operator gets a proper terminal.
//
// Exit codes:
//
//	0   — (never returns on success; process is replaced)
//	1   — tmux binary not found or execve failed
//
// Spec ref: process-lifecycle.md §4.10 PL-028d.
func RunAttach(args []string, stdout, stderr io.Writer) int {
	var projectDir string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, attachUsage)
			return 0
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
			fmt.Fprintf(stderr, "harmonik supervise attach: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	sessionName := FlywheelSessionName(projectDir)

	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise attach: tmux not found: %v\n", err)
		return 1
	}

	argv := []string{"tmux", "attach-session", "-t", sessionName}
	if execErr := syscall.Exec(tmuxBin, argv, os.Environ()); execErr != nil {
		fmt.Fprintf(stderr, "harmonik supervise attach: exec: %v\n", execErr)
		return 1
	}
	// Never reached on success.
	return 0
}

const attachUsage = `harmonik supervise attach — attach to the flywheel tmux session

USAGE
  harmonik supervise attach [--project DIR]

FLAGS
  --project DIR  Project directory (default: current working directory)

EXIT CODES
  0  (never returned on success; process is replaced by tmux)
  1  tmux not found or exec failed

NOTES
  execve-replaces the current process with:
    tmux attach-session -t harmonik-<project_hash>-flywheel
`
