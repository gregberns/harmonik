package supervisecmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// PsResult is the structured output of `harmonik supervise ps`.
type PsResult struct {
	SchemaVersion     int                 `json:"schema_version"`
	ProjectDir        string              `json:"project_dir"`
	ProjectHash       string              `json:"project_hash"`
	ProcessSignatures []ProcessSignature  `json:"process_signatures"`
	TmuxSessions      []TmuxSessionTarget `json:"tmux_sessions"`
}

// ProcessSignature describes a canonical pgrep pattern for a supervisor-related
// process family.
type ProcessSignature struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Pattern     string `json:"pattern"`
	Command     string `json:"command"`
}

// TmuxSessionTarget describes a canonical tmux session name for a
// supervisor-related process family.
type TmuxSessionTarget struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Session     string `json:"session"`
	Command     string `json:"command"`
}

// RunPs implements `harmonik supervise ps`.
//
// It prints canonical process and tmux-session signatures for the project. The
// command is intentionally read-only and does not infer state from loose pgrep
// results; it tells the operator exactly which signatures are authoritative for
// this project.
func RunPs(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	var jsonOut bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, psUsage)
			return 0
		case args[i] == "--json":
			jsonOut = true
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
			fmt.Fprintf(stderr, "harmonik supervise ps: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	result, err := buildPsResult(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik supervise ps: %v\n", err)
		return 1
	}

	if jsonOut {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise ps: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	fmt.Fprintf(stdout, "project:      %s\n", result.ProjectDir)
	fmt.Fprintf(stdout, "project_hash: %s\n", result.ProjectHash)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "process_signatures:")
	for _, sig := range result.ProcessSignatures {
		fmt.Fprintf(stdout, "  %-24s %s\n", sig.Name+":", sig.Pattern)
		fmt.Fprintf(stdout, "  %-24s %s\n", "", sig.Command)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "tmux_sessions:")
	for _, sess := range result.TmuxSessions {
		fmt.Fprintf(stdout, "  %-24s %s\n", sess.Name+":", sess.Session)
		fmt.Fprintf(stdout, "  %-24s %s\n", "", sess.Command)
	}

	return 0
}

func buildPsResult(projectDir string) (PsResult, error) {
	realDir, err := canonicalProjectDir(projectDir)
	if err != nil {
		return PsResult{}, err
	}

	hash := projectHashForRealDir(realDir)
	processes := []ProcessSignature{
		{
			Name:        "supervisor-shim",
			Description: "in-binary flywheel supervisor started by `harmonik supervise start`",
			Pattern:     "harmonik supervise _shim " + realDir,
		},
		{
			Name:        "daemon",
			Description: "daemon process managed by the supervisor or fallback keeper",
			Pattern:     "harmonik --project " + realDir,
		},
		{
			Name:        "keeper-fallback",
			Description: "shell fallback loop from scripts/hk-keeper.sh",
			Pattern:     "hk-keeper.sh " + realDir,
		},
		{
			Name:        "supervise-fallback",
			Description: "shell fallback launcher from scripts/hk-supervise.sh",
			Pattern:     "hk-supervise.sh " + realDir,
		},
	}
	for i := range processes {
		processes[i].Command = "pgrep -af " + shellQuote(processes[i].Pattern)
	}

	sessions := []TmuxSessionTarget{
		{
			Name:        "flywheel",
			Description: "in-binary supervisor shim session",
			Session:     FlywheelSessionName(realDir),
		},
		{
			Name:        "auto-revive-supervisor",
			Description: "legacy supervisor watchdog session excluded from daemon spawn targets",
			Session:     ltmux.SupervisorSessionName(realDir),
		},
		{
			Name:        "daemon-default",
			Description: "default daemon dispatch target session",
			Session:     ltmux.DefaultSessionName(realDir),
		},
		{
			Name:        "keeper-fallback",
			Description: "scripts/hk-keeper.sh tmux session",
			Session:     "hk-" + hash + "-keeper",
		},
		{
			Name:        "supervise-fallback",
			Description: "scripts/hk-supervise.sh tmux session",
			Session:     "hk-" + hash + "-daemon-supervise",
		},
	}
	for i := range sessions {
		sessions[i].Command = "tmux has-session -t " + shellQuote(sessions[i].Session)
	}

	return PsResult{
		SchemaVersion:     1,
		ProjectDir:        realDir,
		ProjectHash:       hash,
		ProcessSignatures: processes,
		TmuxSessions:      sessions,
	}, nil
}

func canonicalProjectDir(projectDir string) (string, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", projectDir, err)
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve real path of %q: %w", absDir, err)
	}
	return realDir, nil
}

func projectHashForRealDir(realDir string) string {
	sum := sha256.Sum256([]byte(realDir))
	return fmt.Sprintf("%x", sum[:6])
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

const psUsage = `harmonik supervise ps — print canonical supervisor process signatures

USAGE
  harmonik supervise ps [--project DIR] [--json]

DESCRIPTION
  Prints the exact project-scoped process signatures and tmux session names used
  to verify supervisor, daemon, and shell-fallback liveness without broad pgrep
  guesses. Read-only: does not contact the daemon and does not start tmux.

FLAGS
  --project DIR  Project directory (default: current working directory)
  --json         Emit schema-versioned JSON to stdout

EXIT CODES
  0  Success
  1  Argument or path-resolution error

EXAMPLES
  harmonik supervise ps
  harmonik supervise ps --json
`
