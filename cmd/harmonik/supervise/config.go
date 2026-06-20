// Package supervisecmd implements the harmonik supervise subcommand family.
//
// Verbs: start, stop, status, attach, restart, logs (PL-028d).
//
// File surface under .harmonik/cognition/:
//
//	supervisor.lock     — fd-lifetime advisory flock (PL-019c)
//	supervisor.pid      — inner process PID (PL-019d)
//	supervisor.sentinel — schema_version=1\n exclusion marker (PL-006d)
//	config.json         — atomic config snapshot (PL-019e)
//
// Spec ref: specs/process-lifecycle.md §4.5 PL-019, PL-028d.
package supervisecmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const configSchemaVersion = 1

// CognitionDir returns the path to the .harmonik/cognition/ directory.
func CognitionDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "cognition")
}

// LockPath returns the supervisor flock file path.
func LockPath(projectDir string) string {
	return filepath.Join(CognitionDir(projectDir), "supervisor.lock")
}

// PidfilePath returns the supervisor pidfile path.
func PidfilePath(projectDir string) string {
	return filepath.Join(CognitionDir(projectDir), "supervisor.pid")
}

// SentinelPath returns the supervisor sentinel file path (PL-006d).
func SentinelPath(projectDir string) string {
	return filepath.Join(CognitionDir(projectDir), "supervisor.sentinel")
}

// ConfigPath returns the supervisor config.json path.
func ConfigPath(projectDir string) string {
	return filepath.Join(CognitionDir(projectDir), "config.json")
}

// FlywheelSessionName returns the tmux session name for the flywheel pane.
// Format: harmonik-<project_hash>-flywheel (PL-019f, PL-006a).
func FlywheelSessionName(projectDir string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolved = projectDir
	}
	sum := sha256.Sum256([]byte(resolved))
	hash := fmt.Sprintf("%x", sum[:6])
	return "harmonik-" + hash + "-flywheel"
}

// Config is the schema for .harmonik/cognition/config.json per PL-028d §(e).
// N-1 readers MUST tolerate unknown fields (json.Unmarshal ignores extras).
type Config struct {
	SchemaVersion      int      `json:"schema_version"`
	Model              string   `json:"model,omitempty"`
	TokenCap           int      `json:"token_cap,omitempty"`
	MaxConcurrent      int      `json:"max_concurrent,omitempty"`
	BudgetCapUSDPerDay float64  `json:"budget_cap_usd_per_day,omitempty"`
	InstructionsPath   string   `json:"instructions_path,omitempty"`
	PrioritySource     string   `json:"priority_source,omitempty"`
	Areas              []string `json:"areas,omitempty"`
	Epic               string   `json:"epic,omitempty"`
	DebounceMS         int      `json:"debounce_ms,omitempty"`
	WatchdogMS         int      `json:"watchdog_ms,omitempty"`
	WarnThreshold      float64  `json:"warn_threshold,omitempty"`
	ForceThreshold     float64  `json:"force_threshold,omitempty"`
	RestartPolicy      string   `json:"restart_policy,omitempty"`
	RestartMax         int      `json:"restart_max,omitempty"`
	RestartBaseMS      int      `json:"restart_base_ms,omitempty"`
	RestartCapMS       int      `json:"restart_cap_ms,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"` // RFC3339
	DaemonInstanceID   string   `json:"daemon_instance_id,omitempty"`
	// Command is the supervisee argv; Command[0] is the binary. Not in the
	// original schema list but required for the shim to know what to run.
	Command []string `json:"command,omitempty"`
	// APIKey is the Pi-scoped ANTHROPIC_API_KEY read from the non-committed
	// scoped source at `supervise start` time (CI-006). Never read by the
	// daemon; injected into Pi's env by the shim at exec time (CI-005).
	// config.json lives under .harmonik/cognition/ which is gitignored, so
	// the value is never committed (CI-007).
	APIKey string `json:"api_key,omitempty"`

	// AssetSync gates the supervisor's asset version-skew behaviour (hk-yqx9,
	// doc 10 §Daemon-safety). When absent (the common case) all fields default to
	// their zero values: detection+notify run, AUTO-APPLY IS OFF.
	AssetSync AssetSyncConfig `json:"asset_sync,omitempty"`
}

// AssetSyncConfig is the supervisor's asset-sync knob block. The DEFAULT (zero
// value) is the safe one: AutoApply is false, so the supervisor only DETECTS skew
// and NOTIFIES the captain — it never writes project files automatically. Auto-apply
// is strictly opt-in and, even when enabled, is limited to Managed + FastForward
// items applied during a confirmed daemon lull (every conflict / content-owned
// change is still surfaced for human review, never applied).
type AssetSyncConfig struct {
	// AutoApply, when true AND a true lull is confirmed, lets the supervisor apply
	// ONLY the safe (Managed + FastForward) skill fast-forwards. OFF by default.
	AutoApply bool `json:"auto_apply,omitempty"`
}

// WriteConfigAtomic writes cfg to .harmonik/cognition/config.json atomically
// via temp+rename+fsync per WM-026.
func WriteConfigAtomic(projectDir string, cfg Config) error {
	configPath := ConfigPath(projectDir)
	dir := filepath.Dir(configPath)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: close: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("supervisecmd: WriteConfigAtomic: rename: %w", err)
	}
	success = true

	// fsync parent directory to make rename durable.
	//nolint:gosec // G304: dir is derived from operator-controlled projectDir
	if dirFd, err := os.Open(dir); err == nil {
		_ = dirFd.Sync()
		_ = dirFd.Close()
	}
	return nil
}

// ReadConfig reads and parses .harmonik/cognition/config.json.
func ReadConfig(projectDir string) (Config, error) {
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(ConfigPath(projectDir))
	if err != nil {
		return Config{}, fmt.Errorf("supervisecmd: ReadConfig: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("supervisecmd: ReadConfig: unmarshal: %w", err)
	}
	return cfg, nil
}

// WriteSentinel writes the supervisor.sentinel file (PL-006d exclusion marker).
// Content: schema_version=1\n
func WriteSentinel(projectDir string) error {
	dir := CognitionDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervisecmd: WriteSentinel: mkdir: %w", err)
	}
	return os.WriteFile(SentinelPath(projectDir), []byte("schema_version=1\n"), 0o644)
}

// RemoveSentinel removes the supervisor.sentinel file; ignores ENOENT.
func RemoveSentinel(projectDir string) error {
	err := os.Remove(SentinelPath(projectDir))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("supervisecmd: RemoveSentinel: %w", err)
	}
	return nil
}

// WritePidfile writes the supervisor PID to supervisor.pid.
func WritePidfile(projectDir string, pid int) error {
	dir := CognitionDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervisecmd: WritePidfile: mkdir: %w", err)
	}
	content := fmt.Sprintf("%d\n", pid)
	return os.WriteFile(PidfilePath(projectDir), []byte(content), 0o644)
}

// ReadPidfile reads the supervisor PID from supervisor.pid.
func ReadPidfile(projectDir string) (int, error) {
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(PidfilePath(projectDir))
	if err != nil {
		return 0, fmt.Errorf("supervisecmd: ReadPidfile: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("supervisecmd: ReadPidfile: parse: %w", err)
	}
	return pid, nil
}
