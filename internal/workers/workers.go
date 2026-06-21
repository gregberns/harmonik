// Package workers loads the per-project remote-worker registry from
// .harmonik/workers.yaml.
//
// # File location
//
// The file lives at <repo-root>/.harmonik/workers.yaml. It is intended to be
// checked into the project repository so that the worker configuration travels
// with the team's code.
//
// # Missing file semantics
//
// If .harmonik/workers.yaml is absent, Load returns a zero-value Config and a
// nil error. The caller is responsible for treating an empty registry as
// "local execution only".
//
// # Version 1 invariant
//
// Version 1 allows at most one worker entry. Providing two or more workers
// returns *ErrTooManyWorkers.
package workers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const configRelPath = ".harmonik/workers.yaml"

const currentVersion = 1

// Worker describes a single remote execution host.
type Worker struct {
	Name      string `yaml:"name"`
	Transport string `yaml:"transport"`
	Host      string `yaml:"host"`
	OS        string `yaml:"os"`
	RepoPath  string `yaml:"repo_path"`
	MaxSlots  int    `yaml:"max_slots"`
	Enabled   bool   `yaml:"enabled"`

	// HarmonikPath is the absolute path to the harmonik binary ON THE WORKER.
	// It is written as the hook "command" field in the worker's per-run
	// .claude/settings.json so the worker-side claude can invoke `harmonik
	// hook-relay` regardless of the spawned tmux window's $PATH (hk-z8ek; mirrors
	// the box-A daemonBinaryPath / hk-kqdpf.6 rationale). When empty, callers fall
	// back to DefaultHarmonikPath. Optional in workers.yaml (yaml: harmonik_path).
	HarmonikPath string `yaml:"harmonik_path"`
}

// DefaultHarmonikPath is the conventional worker-side harmonik binary path used
// when a Worker entry does not set HarmonikPath. It matches the Go-install
// default ($HOME/go/bin/harmonik) on the canonical macOS worker, and is the same
// fallback the release pipeline documents (specs/release-pipeline.md §BIN).
// NOTE (hk-z8ek): harmonik MUST be installed at this path on the worker for the
// hook relay to fire — a worker-setup requirement, not something the daemon can
// fabricate.
const DefaultHarmonikPath = "/Users/gb/go/bin/harmonik"

// Config is the top-level structure decoded from .harmonik/workers.yaml.
type Config struct {
	Version int      `yaml:"version"`
	Workers []Worker `yaml:"workers"`

	// ReportIntervalSeconds is the cadence, in seconds, of the recurring
	// worker-report poll (remote-substrate WR3). Optional; when unset (<= 0) the
	// loader applies DefaultReportIntervalSeconds (60s). A deployment with no
	// workers.yaml never reaches the poll at all (empty registry), so this field
	// is purely a tunable for deployments that DO configure a worker.
	ReportIntervalSeconds int `yaml:"report_interval_seconds"`

	// DiskFloorMB is the disk_pressure floor, in MB, threaded into CollectReport
	// for the recurring poll (remote-substrate WR3). Optional; when unset (<= 0)
	// CollectReport selects the package default DefaultDiskFloorMB (2048 MB).
	DiskFloorMB int64 `yaml:"disk_floor_mb"`

	// --- worker-report Phase 2 (PB3): resource-breach detection knobs. ---
	//
	// All optional + defaulted (see the accessor methods below) so a deployment
	// that omits them — or has no workers.yaml at all — behaves byte-identically
	// to Phase 1. The poll loop reads these only when at least one worker is
	// enabled AND BreachDetectionEnabled() is true.

	// BreachDetectionEnabledPtr is the master switch (workers.yaml
	// breach_detection_enabled). It is a *bool so "absent" (nil) can default to
	// TRUE while still letting an operator write `breach_detection_enabled: false`
	// to turn it off. Read via BreachDetectionEnabled().
	BreachDetectionEnabledPtr *bool `yaml:"breach_detection_enabled"`

	// BreachSampleIntervalSeconds is the fast cadence, in seconds, used while a
	// run is in flight on a worker (workers.yaml breach_sample_interval_secs).
	// Optional; <= 0 ⇒ DefaultBreachSampleIntervalSeconds (5s). Read via
	// BreachSampleInterval().
	BreachSampleIntervalSeconds int `yaml:"breach_sample_interval_secs"`

	// BreachDwellSeconds / ClearDwellSeconds are the sustain windows the state
	// machine waits before firing a breach / clear (workers.yaml
	// breach_dwell_secs / clear_dwell_secs). Optional; <= 0 ⇒ the breach.go
	// package defaults (20s / 15s). Read via BreachConfig().
	BreachDwellSeconds int `yaml:"breach_dwell_secs"`
	ClearDwellSeconds  int `yaml:"clear_dwell_secs"`

	// CPUSource pre-wires the cpu_source: load|top knob (spec §"CPU source").
	// Optional; empty ⇒ DefaultCPUSource ("load"). Only "load" is implemented in
	// Phase 2; "top" is reserved for a later true-%CPU upgrade and currently
	// behaves identically to "load". Read via CPUSourceOrDefault().
	CPUSource string `yaml:"cpu_source"`

	// Per-signal hysteresis thresholds (workers.yaml). All optional; <= 0 ⇒ the
	// breach.go package defaults. Read via BreachConfig().
	CPUEnter    float64 `yaml:"cpu_enter"`
	CPUExit     float64 `yaml:"cpu_exit"`
	MemFreeEnter float64 `yaml:"mem_free_enter"`
	MemFreeExit  float64 `yaml:"mem_free_exit"`
	SwapEnterMB  float64 `yaml:"swap_enter_mb"`
	SwapExitMB   float64 `yaml:"swap_exit_mb"`
}

// Phase-2 breach-detection defaults applied by the accessor methods when the
// corresponding workers.yaml field is unset. The threshold defaults live in
// breach.go (DefaultCPUEnter etc.); these are the loop-cadence + switch
// defaults that have no breach.go home.
const (
	// DefaultBreachSampleIntervalSeconds is the fast cadence used while a run is
	// in flight (spec §"Config knobs": breach_sample_interval_secs: 5).
	DefaultBreachSampleIntervalSeconds = 5
	// DefaultCPUSource is the cpu_source applied when unset. "load" ships the
	// load-ratio proxy (zero new collection); "top" is reserved.
	DefaultCPUSource = "load"
)

// defaultBreachDetectionEnabled is the master-switch default when
// breach_detection_enabled is absent from workers.yaml: TRUE (Phase 2 is on by
// default for a configured worker; an operator opts OUT explicitly).
const defaultBreachDetectionEnabled = true

// BreachDetectionEnabled reports whether resource-breach detection is enabled,
// applying defaultBreachDetectionEnabled (TRUE) when the workers.yaml field is
// absent (nil pointer). An explicit `breach_detection_enabled: false` disables
// it, returning Phase-1 behaviour (slow ticks, worker_report only).
func (c Config) BreachDetectionEnabled() bool {
	if c.BreachDetectionEnabledPtr == nil {
		return defaultBreachDetectionEnabled
	}
	return *c.BreachDetectionEnabledPtr
}

// BreachSampleInterval returns the fast poll cadence used while a run is in
// flight as a time.Duration, applying DefaultBreachSampleIntervalSeconds when
// unset (<= 0). Single home for the default so no caller hardcodes the cadence.
func (c Config) BreachSampleInterval() time.Duration {
	secs := c.BreachSampleIntervalSeconds
	if secs <= 0 {
		secs = DefaultBreachSampleIntervalSeconds
	}
	return time.Duration(secs) * time.Second
}

// CPUSourceOrDefault returns the configured cpu_source, applying
// DefaultCPUSource ("load") when unset. Only "load" is implemented; "top" is
// accepted and pre-wired but behaves as "load" in Phase 2.
func (c Config) CPUSourceOrDefault() string {
	if c.CPUSource == "" {
		return DefaultCPUSource
	}
	return c.CPUSource
}

// BreachConfig assembles a breach.BreachConfig from the Phase-2 workers.yaml
// knobs. Every field is passed through as-is; the zero value of any field
// selects the breach.go package default inside NewBreachDetector
// (BreachConfig.withDefaults), so an unset knob defaults there rather than
// here. Dwell seconds <= 0 map to a zero Duration ⇒ the breach.go default.
func (c Config) BreachConfig() BreachConfig {
	dwell := func(secs int) time.Duration {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	return BreachConfig{
		BreachDwell: dwell(c.BreachDwellSeconds),
		ClearDwell:  dwell(c.ClearDwellSeconds),
		CPUEnter:    c.CPUEnter,
		CPUExit:     c.CPUExit,
		MemEnter:    c.MemFreeEnter,
		MemExit:     c.MemFreeExit,
		SwapEnter:   c.SwapEnterMB,
		SwapExit:    c.SwapExitMB,
	}
}

// DefaultReportIntervalSeconds is the worker-report poll cadence applied when
// workers.yaml omits report_interval_seconds (or sets it <= 0). 60s matches the
// WR3 spec default — frequent enough for operator observability, infrequent
// enough to never load the SSH transport or the work loop.
const DefaultReportIntervalSeconds = 60

// ReportInterval returns the configured worker-report poll cadence as a
// time.Duration, applying DefaultReportIntervalSeconds when unset (<= 0). It is
// the single home for the default so no caller hardcodes the cadence.
func (c Config) ReportInterval() time.Duration {
	secs := c.ReportIntervalSeconds
	if secs <= 0 {
		secs = DefaultReportIntervalSeconds
	}
	return time.Duration(secs) * time.Second
}

// ErrMalformedYAML is returned when the file exists but cannot be parsed.
type ErrMalformedYAML struct {
	Path  string
	Cause error
}

func (e *ErrMalformedYAML) Error() string {
	return fmt.Sprintf("workers: malformed YAML in %s: %v", e.Path, e.Cause)
}

func (e *ErrMalformedYAML) Unwrap() error { return e.Cause }

// ErrUnsupportedVersion is returned when the file declares a version other
// than currentVersion (1).
type ErrUnsupportedVersion struct {
	Path    string
	Version int
}

func (e *ErrUnsupportedVersion) Error() string {
	return fmt.Sprintf("workers: unsupported version %d in %s (want %d)", e.Version, e.Path, currentVersion)
}

// ErrTooManyWorkers is returned when Version == 1 but the file declares more
// than one worker entry.
type ErrTooManyWorkers struct {
	Path  string
	Count int
}

func (e *ErrTooManyWorkers) Error() string {
	return fmt.Sprintf("workers: version 1 allows at most 1 worker, got %d in %s", e.Count, e.Path)
}

// Load reads .harmonik/workers.yaml under repoRoot and returns the decoded
// Config.
//
// Behaviour:
//   - File absent → zero-value Config, nil error.
//   - File present, malformed YAML → *ErrMalformedYAML.
//   - version != 1 → *ErrUnsupportedVersion.
//   - version == 1 and len(workers) > 1 → *ErrTooManyWorkers.
func Load(repoRoot string) (Config, error) {
	path := filepath.Join(repoRoot, configRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("workers: reading %s: %w", path, err)
	}
	return parse(path, data)
}

func parse(path string, data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, &ErrMalformedYAML{Path: path, Cause: err}
	}

	if cfg.Version == 0 && len(cfg.Workers) == 0 {
		return Config{}, nil
	}

	if cfg.Version != currentVersion {
		return Config{}, &ErrUnsupportedVersion{Path: path, Version: cfg.Version}
	}

	if len(cfg.Workers) > 1 {
		return Config{}, &ErrTooManyWorkers{Path: path, Count: len(cfg.Workers)}
	}

	return cfg, nil
}
