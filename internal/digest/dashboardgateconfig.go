// dashboardgateconfig.go — dashboard forcing-gate config reader.
//
// Reads the optional dashboard: block from .harmonik/config.yaml so the
// dashboard staleness gate (plans/2026-07-03-operator-dashboard/DESIGN.md §4)
// can resolve its threshold without depending on the daemon package. Mirrors
// sentinelconfig.go's fail-loud convention for required, no-compiled-default
// thresholds (hk-drygf precedent).
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §4, §6 item 6.
// Bead ref: hk-xg6rw.
package digest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// DashboardGateConfig holds the resolved dashboard: block from
// .harmonik/config.yaml.
type DashboardGateConfig struct {
	// MaxStaleness is the max age of dashboard.json's `updated` timestamp
	// before the forcing gate blocks new dispatch to captain-curated queues.
	// REQUIRED once the dashboard: block is present in config.yaml — no
	// compiled default (no-hardcoded-threshold mandate). Zero when the
	// dashboard: block is absent entirely (gate disabled).
	MaxStaleness time.Duration

	// Unlock is the config-level operator kill-switch (DESIGN §4 guardrail)
	// that disables the gate entirely regardless of staleness. Mirrors the
	// `harmonik dashboard --unlock` CLI override.
	Unlock bool

	// configured reports whether the dashboard: block was present at all —
	// distinguishes "operator never touched dashboard config" (gate disabled,
	// no error) from "block present, max_staleness unset" (fail loud).
	configured bool
}

// Configured reports whether the dashboard: block was present in
// .harmonik/config.yaml. When false, the gate is disabled: the operator has
// not opted into the forcing mechanism yet.
func (c DashboardGateConfig) Configured() bool { return c.configured }

// ErrMissingDashboardMaxStaleness is returned by LoadDashboardGateConfig when
// the operator has a `dashboard:` block in .harmonik/config.yaml but did not
// set `max_staleness`. Fail-loud per the no-hardcoded-threshold mandate: the
// gate must never silently run with an unbounded staleness window once the
// operator has opted into the dashboard: block.
type ErrMissingDashboardMaxStaleness struct{}

func (e *ErrMissingDashboardMaxStaleness) Error() string {
	return "digest: dashboard config: dashboard: block is present in .harmonik/config.yaml but " +
		"max_staleness is not set; set it (e.g. `dashboard:\n  max_staleness: 45m`) — there is no compiled default"
}

// rawDashboardGateConfig is the YAML shape of the dashboard: block.
// Unknown keys are silently ignored (forward-compat).
type rawDashboardGateConfig struct {
	MaxStaleness string `yaml:"max_staleness"`
	Unlock       bool   `yaml:"unlock"`
}

// rawConfigWithDashboard is the minimal top-level shape needed to extract
// dashboard:. Unknown sibling keys are silently ignored (forward-compat).
type rawConfigWithDashboard struct {
	// Dashboard is a pointer so we can distinguish "block absent" (nil) from
	// "block present but empty" (non-nil zero value) — the former disables
	// the gate, the latter is a missing-max_staleness fail-loud case.
	Dashboard *rawDashboardGateConfig `yaml:"dashboard"`
}

// LoadDashboardGateConfig reads the dashboard: block from
// projectDir/.harmonik/config.yaml.
//
// Behaviour:
//   - File absent → zero-value DashboardGateConfig (gate disabled), nil error.
//   - File present, no dashboard: block → zero-value DashboardGateConfig
//     (gate disabled), nil error.
//   - dashboard: block present, max_staleness unset or non-positive →
//     *ErrMissingDashboardMaxStaleness.
//   - dashboard: block present, max_staleness malformed → parse error.
func LoadDashboardGateConfig(projectDir string) (DashboardGateConfig, error) {
	path := filepath.Join(projectDir, ".harmonik", "config.yaml")
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DashboardGateConfig{}, nil
		}
		return DashboardGateConfig{}, fmt.Errorf("digest: dashboard config: reading %s: %w", path, err)
	}
	return parseDashboardGateConfig(data)
}

// parseDashboardGateConfig decodes raw YAML bytes and returns the
// DashboardGateConfig.
func parseDashboardGateConfig(data []byte) (DashboardGateConfig, error) {
	var raw rawConfigWithDashboard
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return DashboardGateConfig{}, fmt.Errorf("digest: dashboard config: YAML parse: %w", err)
	}
	if raw.Dashboard == nil {
		return DashboardGateConfig{}, nil
	}

	if raw.Dashboard.MaxStaleness == "" {
		return DashboardGateConfig{}, &ErrMissingDashboardMaxStaleness{}
	}
	d, err := time.ParseDuration(raw.Dashboard.MaxStaleness)
	if err != nil {
		return DashboardGateConfig{}, fmt.Errorf("digest: dashboard config: max_staleness %q: %w", raw.Dashboard.MaxStaleness, err)
	}
	if d <= 0 {
		return DashboardGateConfig{}, &ErrMissingDashboardMaxStaleness{}
	}

	return DashboardGateConfig{
		MaxStaleness: d,
		Unlock:       raw.Dashboard.Unlock,
		configured:   true,
	}, nil
}
