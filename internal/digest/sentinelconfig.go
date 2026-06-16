// Package digest — sentinel config reader.
//
// Reads the optional sentinel: block from .harmonik/config.yaml so the
// suppression resolver can apply configurable TTLs and phase flags without
// depending on the daemon package.
//
// Spec ref: flywheel-motion.md §7 (sentinel: config block).
// Bead: hk-1f8f.
package digest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultSuppressionTTL is the default decaying TTL for operator-attached and
// operator-dialogue suppression when suppression_ttl is not configured.
const DefaultSuppressionTTL = 10 * time.Minute

// DefaultAttachedInactiveTimeout is the default guard for the
// operatorAttached-pins-forever bug when attached_inactive_timeout is not configured.
// Must be ≤ DefaultSuppressionTTL to be a meaningful inner guard.
const DefaultAttachedInactiveTimeout = 5 * time.Minute

// SentinelConfig holds the resolved sentinel: block from .harmonik/config.yaml
// (flywheel-motion.md §7).
//
// All fields are optional in the file; zero values signal "not configured —
// use compiled default". PhaseFlag requires PhaseFlagExpiry; loading returns
// ErrPhaseFlagMissingExpiry when PhaseFlag is set without an expiry.
type SentinelConfig struct {
	// SuppressionTTL is the decaying TTL for operator-attached and
	// operator-dialogue suppression (spec §3.2). Default: DefaultSuppressionTTL.
	SuppressionTTL time.Duration

	// AttachedInactiveTimeout is the inner guard that expires the
	// operator-attached suppression when the operator is attached but
	// no new session_keeper_operator_attached events arrive within this window
	// (guards the operatorAttached-pins-forever bug). Default: DefaultAttachedInactiveTimeout.
	// Effective check: suppress only when now-lastSeen < min(SuppressionTTL, AttachedInactiveTimeout).
	AttachedInactiveTimeout time.Duration

	// PhaseFlag is the optional operator-forced suppression label.
	// Empty = not set. When non-empty, PhaseFlagExpiry MUST be non-zero.
	PhaseFlag string

	// PhaseFlagExpiry is the mandatory expiry for PhaseFlag. Zero when PhaseFlag is empty.
	PhaseFlagExpiry time.Time
}

// suppressionTTL returns the effective SuppressionTTL, falling back to the default.
func (c SentinelConfig) suppressionTTL() time.Duration {
	if c.SuppressionTTL > 0 {
		return c.SuppressionTTL
	}
	return DefaultSuppressionTTL
}

// attachedInactiveTimeout returns the effective AttachedInactiveTimeout.
func (c SentinelConfig) attachedInactiveTimeout() time.Duration {
	if c.AttachedInactiveTimeout > 0 {
		return c.AttachedInactiveTimeout
	}
	return DefaultAttachedInactiveTimeout
}

// ErrPhaseFlagMissingExpiry is returned by LoadSentinelConfig when sentinel.phase_flag
// is set without a sentinel.phase_flag_expiry. A phase flag without expiry is
// invalid config per flywheel-motion.md §3.2.
type ErrPhaseFlagMissingExpiry struct {
	PhaseFlag string
}

func (e *ErrPhaseFlagMissingExpiry) Error() string {
	return fmt.Sprintf("digest: sentinel config: phase_flag %q requires phase_flag_expiry (mandatory per spec §3.2)", e.PhaseFlag)
}

// rawSentinelConfig is the YAML shape of the sentinel: block.
type rawSentinelConfig struct {
	SuppressionTTL          string `yaml:"suppression_ttl"`
	AttachedInactiveTimeout string `yaml:"attached_inactive_timeout"`
	PhaseFlag               string `yaml:"phase_flag"`
	PhaseFlagExpiry         string `yaml:"phase_flag_expiry"`
}

// rawConfigWithSentinel is the minimal top-level shape we need to extract sentinel:.
// Unknown sibling keys are silently ignored (forward-compat).
type rawConfigWithSentinel struct {
	Sentinel rawSentinelConfig `yaml:"sentinel"`
}

// LoadSentinelConfig reads the sentinel: block from projectDir/.harmonik/config.yaml.
//
// Behaviour:
//   - File absent → zero-value SentinelConfig (use all defaults), nil error.
//   - File present, no sentinel: block → zero-value SentinelConfig, nil error.
//   - sentinel.phase_flag set without sentinel.phase_flag_expiry → *ErrPhaseFlagMissingExpiry.
//   - Other parse errors → non-nil error.
func LoadSentinelConfig(projectDir string) (SentinelConfig, error) {
	path := filepath.Join(projectDir, ".harmonik", "config.yaml")
	//nolint:gosec // G304: path derived from operator-controlled projectDir
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SentinelConfig{}, nil
		}
		return SentinelConfig{}, fmt.Errorf("digest: sentinel config: reading %s: %w", path, err)
	}
	return parseSentinelConfig(data)
}

// parseSentinelConfig decodes raw YAML bytes and returns the SentinelConfig.
func parseSentinelConfig(data []byte) (SentinelConfig, error) {
	var raw rawConfigWithSentinel
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return SentinelConfig{}, fmt.Errorf("digest: sentinel config: YAML parse: %w", err)
	}
	s := raw.Sentinel
	cfg := SentinelConfig{}

	if s.SuppressionTTL != "" {
		d, err := time.ParseDuration(s.SuppressionTTL)
		if err != nil {
			return SentinelConfig{}, fmt.Errorf("digest: sentinel config: suppression_ttl %q: %w", s.SuppressionTTL, err)
		}
		if d > 0 {
			cfg.SuppressionTTL = d
		}
	}

	if s.AttachedInactiveTimeout != "" {
		d, err := time.ParseDuration(s.AttachedInactiveTimeout)
		if err != nil {
			return SentinelConfig{}, fmt.Errorf("digest: sentinel config: attached_inactive_timeout %q: %w", s.AttachedInactiveTimeout, err)
		}
		if d > 0 {
			cfg.AttachedInactiveTimeout = d
		}
	}

	if s.PhaseFlag != "" {
		if s.PhaseFlagExpiry == "" {
			return SentinelConfig{}, &ErrPhaseFlagMissingExpiry{PhaseFlag: s.PhaseFlag}
		}
		expiry, err := time.Parse(time.RFC3339, s.PhaseFlagExpiry)
		if err != nil {
			return SentinelConfig{}, fmt.Errorf("digest: sentinel config: phase_flag_expiry %q: %w", s.PhaseFlagExpiry, err)
		}
		cfg.PhaseFlag = s.PhaseFlag
		cfg.PhaseFlagExpiry = expiry
	}

	return cfg, nil
}
