package supervisecmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

// StatusResult is the structured output of `harmonik supervise status`.
type StatusResult struct {
	SchemaVersion int    `json:"schema_version"`
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	Status        string `json:"status"`         // "running" | "stopped" | "unknown"
	RestartPolicy string `json:"restart_policy"` // from config.json
	RestartMax    int    `json:"restart_max"`    // from config.json
	StartedAt     string `json:"started_at,omitempty"`
	DaemonID      string `json:"daemon_instance_id,omitempty"`
	SentinelOK    bool   `json:"sentinel_ok"` // sentinel file present
	// LoopStatus is the current cognition loop state per specs/cognition-loop.md §6.
	// Present when the cognition loop has written loop-status.json; absent otherwise.
	// Includes "budget-paused" and "circuit-tripped" pause-reason states per ON-008a.
	//
	// Spec ref: specs/operator-nfr.md §4.3 ON-008a.
	LoopStatus  string `json:"loop_status,omitempty"`
	PauseReason string `json:"pause_reason,omitempty"`
}

// RunStatus implements `harmonik supervise status`.
//
// File-surface only (no daemon socket). Reads supervisor.pid and probes
// liveness via kill(pid, 0). Reads config.json for metadata.
//
// Exit codes:
//
//	0  — success (output written)
//	1  — argument error
//
// Spec ref: process-lifecycle.md §4.10 PL-028d.
func RunStatus(args []string, stdout, stderr io.Writer) int {
	var projectDir string
	var jsonOut bool

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, statusUsage)
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
			fmt.Fprintf(stderr, "harmonik supervise status: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}

	result := buildStatus(projectDir)

	if jsonOut {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik supervise status: marshal: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}

	// Human-readable output.
	fmt.Fprintf(stdout, "status:        %s\n", result.Status)
	if result.PID != 0 {
		fmt.Fprintf(stdout, "pid:           %d\n", result.PID)
	}
	fmt.Fprintf(stdout, "sentinel:      %v\n", result.SentinelOK)
	if result.StartedAt != "" {
		fmt.Fprintf(stdout, "started_at:    %s\n", result.StartedAt)
	}
	if result.RestartPolicy != "" {
		fmt.Fprintf(stdout, "restart_policy:%s (max %d)\n", result.RestartPolicy, result.RestartMax)
	}
	if result.DaemonID != "" {
		fmt.Fprintf(stdout, "daemon_id:     %s\n", result.DaemonID)
	}
	if result.LoopStatus != "" {
		fmt.Fprintf(stdout, "loop_status:   %s\n", result.LoopStatus)
	}
	if result.PauseReason != "" {
		fmt.Fprintf(stdout, "pause_reason:  %s\n", result.PauseReason)
	}
	return 0
}

// buildStatus constructs a StatusResult from the file surface.
func buildStatus(projectDir string) StatusResult {
	result := StatusResult{
		SchemaVersion: 1,
		Status:        "stopped",
	}

	// Check sentinel.
	if _, err := os.Stat(SentinelPath(projectDir)); err == nil {
		result.SentinelOK = true
	}

	// Read config.json metadata.
	if cfg, err := ReadConfig(projectDir); err == nil {
		result.RestartPolicy = cfg.RestartPolicy
		result.RestartMax = cfg.RestartMax
		result.StartedAt = cfg.StartedAt
		result.DaemonID = cfg.DaemonInstanceID
	}

	// Read cognition loop status (ON-008a: budget-paused / circuit-tripped surfacing).
	if ls, err := ReadLoopStatus(projectDir); err == nil && ls != nil {
		result.LoopStatus = string(ls.Status)
		result.PauseReason = ls.PauseReason
	}

	// Read pidfile and probe liveness.
	pid, err := ReadPidfile(projectDir)
	if err != nil {
		result.Status = "stopped"
		return result
	}
	result.PID = pid

	// kill(pid, 0) probes liveness.
	if err := syscall.Kill(pid, 0); err != nil {
		result.Status = "stopped"
		result.PID = 0
		return result
	}

	result.Running = true
	result.Status = "running"
	return result
}

const statusUsage = `harmonik supervise status — show supervisor process state

USAGE
  harmonik supervise status [--project DIR] [--json]

FLAGS
  --project DIR  Project directory (default: current working directory)
  --json         Emit schema-versioned JSON to stdout

EXIT CODES
  0  Success
  1  Argument error

NOTES
  File-surface only — does not connect to the daemon socket.
  Probes supervisor liveness via kill(pid, 0).
`
