package supervisecmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// PresenceSource indicates how liveness was established: "pidfile" when the
	// cognition-shim pidfile was used, "keeper-loop" when a shell-based daemon-
	// revive loop (hk-keeper.sh / hk-supervise.sh) was detected instead.
	// Absent when Running=false.
	//
	// Bead ref: hk-yrnui — pen9 supervisor-up false positive fix.
	PresenceSource string `json:"presence_source,omitempty"`
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
	if result.PresenceSource != "" {
		fmt.Fprintf(stdout, "presence_src:  %s\n", result.PresenceSource)
	}
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
	return buildStatusWithProbe(projectDir, keeperLoopAlive)
}

// buildStatusWithProbe is the testable core of buildStatus. keeperProbe is
// called when the pidfile check fails; returning true means a shell-based
// revive loop is live (hk-yrnui false-positive fix).
func buildStatusWithProbe(projectDir string, keeperProbe func(string) bool) StatusResult {
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
		// Pidfile absent or unreadable. Before declaring "stopped", check whether
		// a shell-based daemon-revive loop (hk-keeper.sh / hk-supervise.sh) is
		// running: those loops never write supervisor.pid (doing so would corrupt
		// the orphansweep PL-006d sentinel logic), so pidfile absence alone does
		// not mean "no supervisor" when a keeper loop is live (hk-yrnui).
		if keeperProbe(projectDir) {
			result.Running = true
			result.Status = "running"
			result.PresenceSource = "keeper-loop"
			return result
		}
		result.Status = "stopped"
		return result
	}
	result.PID = pid

	// kill(pid, 0) probes liveness.
	if err := syscall.Kill(pid, 0); err != nil {
		// Stale pidfile. Same fallback: a keeper loop may have already relaunched
		// the supervisor and be running while the new shim hasn't written its
		// fresh pidfile yet.
		if keeperProbe(projectDir) {
			result.Running = true
			result.Status = "running"
			result.PresenceSource = "keeper-loop"
			return result
		}
		result.Status = "stopped"
		result.PID = 0
		return result
	}

	result.Running = true
	result.Status = "running"
	result.PresenceSource = "pidfile"
	return result
}

// keeperLoopAlive returns true when a shell-based daemon-revive loop
// (hk-keeper.sh or hk-supervise.sh) is detectable either as a live process
// or via its project-scoped tmux session name. These loops never write
// supervisor.pid (it would corrupt the orphansweep PL-006d flywheel-session
// protection), so their presence must be inferred from process/session
// signature instead of the pidfile.
//
// Bead ref: hk-yrnui — pen9 supervisor-up false positive.
func keeperLoopAlive(projectDir string) bool {
	// 1. Process signature: pgrep -f "hk-keeper.sh" or "hk-supervise.sh".
	for _, pattern := range []string{"hk-keeper.sh", "hk-supervise.sh"} {
		cmd := exec.Command("pgrep", "-f", pattern) //nolint:gosec // G204: fixed literals
		if err := cmd.Run(); err == nil {
			return true
		}
	}

	// 2. Session signature: hk-<hash>-daemon-supervise (hk-supervise.sh) and
	//    hk-<hash>-keeper (hk-keeper.sh). The hash uses the same SHA256 digest
	//    as FlywheelSessionName so session names match what the scripts produce.
	hash := supervisorProjectHash(projectDir)
	for _, suffix := range []string{"daemon-supervise", "keeper"} {
		sessionName := "hk-" + hash + "-" + suffix
		cmd := exec.Command("tmux", "has-session", "-t", sessionName) //nolint:gosec // G204: fixed prefix
		if err := cmd.Run(); err == nil {
			return true
		}
	}

	return false
}

// supervisorProjectHash returns the 6-byte SHA256 hex digest of projectDir's
// real path, matching the hash produced by FlywheelSessionName and by the
// project-hash subcommand used in hk-keeper.sh / hk-supervise.sh.
func supervisorProjectHash(projectDir string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolved = projectDir
	}
	sum := sha256.Sum256([]byte(resolved))
	return fmt.Sprintf("%x", sum[:6])
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
