// Package digest — sentinel config reader.
//
// Reads the optional sentinel: block from .harmonik/config.yaml so the
// suppression resolver and movement governor can apply configurable parameters
// without depending on the daemon package.
//
// Spec ref: flywheel-motion.md §7 (sentinel: config block).
// Beads: hk-1f8f, hk-w0rm.
package digest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// DefaultSuppressionTTL is the default decaying TTL for operator-attached and
// operator-dialogue suppression when suppression_ttl is not configured.
const DefaultSuppressionTTL = 10 * time.Minute

// DefaultAttachedInactiveTimeout is the default guard for the
// operatorAttached-pins-forever bug when attached_inactive_timeout is not configured.
// Must be ≤ DefaultSuppressionTTL to be a meaningful inner guard.
const DefaultAttachedInactiveTimeout = 5 * time.Minute

// DefaultGovernorWindow is the default sliding-window duration for the movement
// governor (flywheel-motion.md §1.2).
const DefaultGovernorWindow = 30 * time.Minute

// DefaultGovernorWarmupWindow is the default cold-start watermark before the
// governor may trip (flywheel-motion.md §1.4).
const DefaultGovernorWarmupWindow = 30 * time.Minute

// DefaultGovernorSustainedWindows is the default number of consecutive low
// windows required before the governor trips (flywheel-motion.md §1.4).
const DefaultGovernorSustainedWindows = 2

// DefaultGovernorHighWeight is the movement score awarded for each
// terminal-progress event (matches sentinel.DefaultHighWeight).
const DefaultGovernorHighWeight = 10

// DefaultLivenessNoProgressN is the default number of consecutive governor
// cycles with no terminal progress before G-liveness triggers self-kill
// (flywheel-motion.md §6.1). Operator-tunable via sentinel.liveness_no_progress_n.
const DefaultLivenessNoProgressN = 10

// DefaultDoneDefinition is the default per-class completion definition:
// "merged" means the bead is done when its Refs: trailer lands on origin/main
// (flywheel-motion.md §5.2).
const DefaultDoneDefinition = "merged"

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

	// Window is the sliding-window duration for terminal-progress movement scoring
	// (flywheel-motion.md §1.2). Default: DefaultGovernorWindow (30m).
	Window time.Duration

	// WarmupWindow is the cold-start watermark before the governor may trip
	// (flywheel-motion.md §1.4). Default: DefaultGovernorWarmupWindow (30m).
	WarmupWindow time.Duration

	// SustainedWindows is the number of consecutive low windows required to
	// trip the governor (flywheel-motion.md §1.4).
	// Default: DefaultGovernorSustainedWindows (2).
	SustainedWindows int

	// MovementWeights is the per-event-type weight table (flywheel-motion.md §1.1).
	// Keys are event type strings (e.g. "bead_closed", "run_completed",
	// "reviewer_verdict"). Nil or empty uses the compiled defaults:
	// terminal-progress events = DefaultGovernorHighWeight (10), others = 0.
	MovementWeights map[string]int

	// LivenessNoProgressN is the number of consecutive governor cycles with
	// no terminal progress before G-liveness triggers a self-kill
	// (flywheel-motion.md §6.1). Default: DefaultLivenessNoProgressN (10).
	LivenessNoProgressN int

	// DoneDefinition is the per-class completion definition
	// (flywheel-motion.md §5.2). Keys are class names; values are
	// "merged" (default) or a Phase-2 deploy+verify command. Nil or empty
	// uses DefaultDoneDefinition ("merged") for all classes.
	DoneDefinition map[string]string

	// Mode controls sentinel activation behaviour (flywheel-motion.md §7).
	// "" or "observe" — evaluate the governor and emit governor_signal events,
	//                   but take no action (no EmitTrip, no halt). Default.
	// "act"           — full ACT behaviour added by FW3 (hk-4toh).
	Mode string
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

// GovernorWindow returns the effective sliding-window duration for the governor,
// falling back to DefaultGovernorWindow.
func (c SentinelConfig) GovernorWindow() time.Duration {
	if c.Window > 0 {
		return c.Window
	}
	return DefaultGovernorWindow
}

// GovernorWarmupWindow returns the effective cold-start watermark duration,
// falling back to DefaultGovernorWarmupWindow.
func (c SentinelConfig) GovernorWarmupWindow() time.Duration {
	if c.WarmupWindow > 0 {
		return c.WarmupWindow
	}
	return DefaultGovernorWarmupWindow
}

// GovernorSustainedWindows returns the effective consecutive-low-window count,
// falling back to DefaultGovernorSustainedWindows.
func (c SentinelConfig) GovernorSustainedWindows() int {
	if c.SustainedWindows > 0 {
		return c.SustainedWindows
	}
	return DefaultGovernorSustainedWindows
}

// GovernorMovementWeights returns the effective per-event-type weight table.
// Returns nil when not configured so callers can apply their own compiled defaults
// (the sentinel package's DefaultWeights).
func (c SentinelConfig) GovernorMovementWeights() map[string]int {
	return c.MovementWeights
}

// GovernorLivenessNoProgressN returns the effective G-liveness cycle threshold,
// falling back to DefaultLivenessNoProgressN.
func (c SentinelConfig) GovernorLivenessNoProgressN() int {
	if c.LivenessNoProgressN > 0 {
		return c.LivenessNoProgressN
	}
	return DefaultLivenessNoProgressN
}

// GovernorConfig converts the digest SentinelConfig into a sentinel.Config
// suitable for passing to sentinel.Evaluate.
//
// The MovementWeights map[string]int field is translated to
// map[core.EventType]int by casting each key; unknown event-type strings are
// preserved (the governor ignores weights for event types it does not handle).
// When MovementWeights is nil the returned Config.Weights is also nil, which
// causes sentinel.Evaluate to fall back to sentinel.DefaultWeights.
//
// Bead: hk-y9fn (FW1 config-adapter).
func (c SentinelConfig) GovernorConfig() sentinel.Config {
	var weights map[core.EventType]int
	if len(c.MovementWeights) > 0 {
		weights = make(map[core.EventType]int, len(c.MovementWeights))
		for k, v := range c.MovementWeights {
			weights[core.EventType(k)] = v
		}
	}
	return sentinel.Config{
		Window:              c.Window,
		WarmupWindow:        c.WarmupWindow,
		SustainedWindows:    c.SustainedWindows,
		Weights:             weights,
		LivenessNoProgressN: c.LivenessNoProgressN,
	}
}

// DoneDefinitionFor returns the completion definition for the given class name,
// falling back to DefaultDoneDefinition ("merged") when not configured.
func (c SentinelConfig) DoneDefinitionFor(class string) string {
	if v, ok := c.DoneDefinition[class]; ok && v != "" {
		return v
	}
	return DefaultDoneDefinition
}

// Phase2Classes returns the class names whose done_definition requires more
// than a merge (i.e., any class whose value is not DefaultDoneDefinition
// "merged" and not empty). These classes opt into Phase-2 deploy+verify
// (flywheel-motion.md §5.3). The returned slice order is unspecified.
func (c SentinelConfig) Phase2Classes() []string {
	var classes []string
	for class, def := range c.DoneDefinition {
		if def != "" && def != DefaultDoneDefinition {
			classes = append(classes, class)
		}
	}
	return classes
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

// ErrTrivialVerifyCommand is returned by LoadSentinelConfig when a Phase-2
// done_definition entry carries a trivially-always-exit-0 verify command
// ("true", ":", or whitespace-only). A real observable post-condition is
// required per flywheel-motion.md §5.3.
type ErrTrivialVerifyCommand struct {
	Class   string
	Command string
}

func (e *ErrTrivialVerifyCommand) Error() string {
	return fmt.Sprintf(
		"digest: sentinel config: done_definition[%q] = %q is a trivially-always-exit-0 command; "+
			"provide an observable post-condition (flywheel-motion.md §5.3)",
		e.Class, e.Command,
	)
}

// isTrivialVerifyCommand reports whether cmd is trivially always-exit-0 and
// therefore cannot assert an observable post-condition (spec §5.3).
// The trivial set is: empty/whitespace-only, "true" (shell builtin), ":" (shell no-op).
func isTrivialVerifyCommand(cmd string) bool {
	t := strings.TrimSpace(cmd)
	return t == "" || t == "true" || t == ":"
}

// rawSentinelConfig is the YAML shape of the sentinel: block.
// Unknown keys are silently ignored (forward-compat).
type rawSentinelConfig struct {
	SuppressionTTL          string            `yaml:"suppression_ttl"`
	AttachedInactiveTimeout string            `yaml:"attached_inactive_timeout"`
	PhaseFlag               string            `yaml:"phase_flag"`
	PhaseFlagExpiry         string            `yaml:"phase_flag_expiry"`
	Window                  string            `yaml:"window"`
	WarmupWindow            string            `yaml:"warmup_window"`
	SustainedWindows        int               `yaml:"sustained_windows"`
	MovementWeights         map[string]int    `yaml:"movement_weights"`
	LivenessNoProgressN     int               `yaml:"liveness_no_progress_n"`
	DoneDefinition          map[string]string `yaml:"done_definition"`
	Mode                    string            `yaml:"mode"`
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

	if s.Window != "" {
		d, err := time.ParseDuration(s.Window)
		if err != nil {
			return SentinelConfig{}, fmt.Errorf("digest: sentinel config: window %q: %w", s.Window, err)
		}
		if d > 0 {
			cfg.Window = d
		}
	}

	if s.WarmupWindow != "" {
		d, err := time.ParseDuration(s.WarmupWindow)
		if err != nil {
			return SentinelConfig{}, fmt.Errorf("digest: sentinel config: warmup_window %q: %w", s.WarmupWindow, err)
		}
		if d > 0 {
			cfg.WarmupWindow = d
		}
	}

	if s.SustainedWindows > 0 {
		cfg.SustainedWindows = s.SustainedWindows
	}

	if len(s.MovementWeights) > 0 {
		cfg.MovementWeights = s.MovementWeights
	}

	if s.LivenessNoProgressN > 0 {
		cfg.LivenessNoProgressN = s.LivenessNoProgressN
	}

	if len(s.DoneDefinition) > 0 {
		for class, cmd := range s.DoneDefinition {
			if cmd != DefaultDoneDefinition && isTrivialVerifyCommand(cmd) {
				return SentinelConfig{}, &ErrTrivialVerifyCommand{Class: class, Command: cmd}
			}
		}
		cfg.DoneDefinition = s.DoneDefinition
	}

	if s.Mode != "" {
		cfg.Mode = s.Mode
	}

	return cfg, nil
}
