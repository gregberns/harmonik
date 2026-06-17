package daemon

// projectconfig.go — per-project model/effort config loader for
// .harmonik/config.yaml (hk-bfvk7), extended with the daemon operational
// config block per PL-004b (hk-rcp7) and the keeper config block (hk-lhu2).
//
// Implements tier-2 of the EM-012b model/effort resolution chain:
// per-project .harmonik/config.yaml supplies per-agent-type defaults that take
// precedence over compiled-in tier-3 defaults but are overridden by per-bead
// labels (tier-1).
//
// Also implements the PL-004b daemon: block reader: LoadProjectConfig now parses
// the optional daemon: mapping under schema_version: 1, extracting workflow_mode,
// max_concurrent, and target_branch. Callers read these via ProjectConfig.Daemon
// to apply the flag > config > default precedence chain at startup.
//
// Also implements the hk-lhu2 keeper: block reader: LoadProjectConfig parses the
// optional keeper: mapping under schema_version: 1, extracting context thresholds
// and warn message overrides. Callers read these via ProjectConfig.Keeper and
// apply the CLI flag > config > default precedence chain at keeper startup.
//
// # File location
//
// .harmonik/config.yaml at the project root. Loaded ONCE at daemon startup and
// cached on the daemon Config struct. No mtime-invalidation: operators restart
// the daemon to reload (matches the pattern for WorkflowModeDefault and other
// startup-time-resolved fields). This is documented here so the decision is
// explicit.
//
// # Schema (v1)
//
//	schema_version: 1
//	agents:
//	  claude-code:
//	    model: sonnet      # optional alias; omitted = defer to tier 3
//	    effort: medium     # optional effort; omitted = defer to tier 3
//	  claude-twin:
//	    model: sonnet
//	    effort: medium
//	daemon:
//	  workflow_mode: dot       # review-loop or dot; single FORBIDDEN (PL-004a floor)
//	  max_concurrent: 4        # > 0 to override --max-concurrent default
//	  target_branch: main      # observability/symmetry only; authoritative source is branching.yaml
//	keeper:
//	  context_thresholds:
//	    warn_abs_tokens: 270000    # absolute warn gate (default 270000); ≤0 = not configured
//	    act_abs_tokens: 300000     # absolute act gate (default 300000); ≤0 = not configured
//	    force_act_abs_tokens: 340000  # hard ceiling, unconditional clear (default act+40000); ≤0 = not configured
//	    act_pct_ceil: 0.85         # pct-of-window cap for act gate (default 0.85); ≤0 = not configured
//	    warn_pct_ceil: 0.70        # pct-of-window cap for warn gate (default 0.70); ≤0 = not configured
//	  warn_messages:
//	    default_warn_text: ""      # warn injection text for non-captain agents; empty = compiled default
//	    on_demand_warn_text: ""    # warn injection text for captain (restart-now path); empty = compiled default
//
// Unknown agent keys are silently ignored (forward-compat).
// Unknown sibling keys under daemon: are silently ignored (forward-compat per PL-004b).
// Unknown sibling keys under keeper: are silently ignored (forward-compat per hk-lhu2).
// Unknown schema_version → ErrUnsupportedConfigVersion.
// Parse error on a present file → ErrMalformedConfigYAML.
// daemon.workflow_mode: single → ErrWorkflowModeFloorViolation (PL-004a floor).
// Absent file → zero-value ProjectConfig, nil error.
//
// # Spec refs
//
// specs/execution-model.md §4.3 EM-012b — tier-2 slot.
// specs/handler-contract.md §4.10 HC-055a — ModelPreference invariants.
// specs/process-lifecycle.md §4.1 PL-004a — review floor (never single from config).
// specs/process-lifecycle.md §4.1 PL-004b — flag > config > default precedence chain.
//
// Beads: hk-bfvk7, hk-rcp7, hk-lhu2.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/core"
)

// projectConfigRelPath is the path of the config file relative to the project root.
const projectConfigRelPath = ".harmonik/config.yaml"

// projectConfigCurrentVersion is the only schema_version this loader accepts.
const projectConfigCurrentVersion = 1

// ErrMalformedConfigYAML is returned when .harmonik/config.yaml is present but
// cannot be parsed as valid YAML, or contains structurally invalid content.
type ErrMalformedConfigYAML struct {
	// Path is the absolute path to the file.
	Path string
	// Cause is the underlying parse or structural error.
	Cause error
}

func (e *ErrMalformedConfigYAML) Error() string {
	return fmt.Sprintf("daemon: project config: malformed YAML in %s: %v", e.Path, e.Cause)
}

func (e *ErrMalformedConfigYAML) Unwrap() error { return e.Cause }

// ErrUnsupportedConfigVersion is returned when .harmonik/config.yaml declares a
// schema_version other than projectConfigCurrentVersion (1).
type ErrUnsupportedConfigVersion struct {
	// Path is the absolute path to the file.
	Path string
	// Version is the declared version.
	Version int
}

func (e *ErrUnsupportedConfigVersion) Error() string {
	return fmt.Sprintf("daemon: project config: unsupported schema_version %d in %s (want %d)",
		e.Version, e.Path, projectConfigCurrentVersion)
}

// ErrWorkflowModeFloorViolation is returned when .harmonik/config.yaml carries
// daemon.workflow_mode: single, violating the PL-004a review floor. The daemon
// MUST refuse to start (fail-fast) when this error is returned.
//
// The only path to single-mode dispatch remains an explicit per-bead
// workflow:single label audited via the review_bypassed event per PL-004a.
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-004a, PL-004b.
// Bead ref: hk-rcp7.
type ErrWorkflowModeFloorViolation struct {
	// Path is the absolute path to the file.
	Path string
	// Value is the disallowed workflow_mode string (always "single").
	Value string
}

func (e *ErrWorkflowModeFloorViolation) Error() string {
	return fmt.Sprintf(
		"daemon: project config: daemon.workflow_mode %q in %s violates the PL-004a review floor: "+
			"single is not a valid daemon-level default; only an explicit per-bead workflow:single label may enable single mode",
		e.Value, e.Path,
	)
}

// rawDaemonConfig is the per-daemon block in the config.yaml daemon: mapping.
// Unknown keys at this level are silently ignored (forward-compat per PL-004b).
type rawDaemonConfig struct {
	WorkflowMode  string   `yaml:"workflow_mode"`
	MaxConcurrent int      `yaml:"max_concurrent"`
	TargetBranch  string   `yaml:"target_branch"` // observability/symmetry only per PL-004b
	AllowedRepos  []string `yaml:"allowed_repos"` // cross-repo dispatch safelist (hk-xfuc)
}

// rawKeeperContextThresholds holds configurable threshold values in the
// keeper.context_thresholds block. Values ≤ 0 are treated as not configured
// (defer to CLI flag or compiled default). Unknown keys are silently ignored.
type rawKeeperContextThresholds struct {
	WarnAbsTokens     int64   `yaml:"warn_abs_tokens"`
	ActAbsTokens      int64   `yaml:"act_abs_tokens"`
	ForceActAbsTokens int64   `yaml:"force_act_abs_tokens"`
	ActPctCeil        float64 `yaml:"act_pct_ceil"`
	WarnPctCeil       float64 `yaml:"warn_pct_ceil"`
}

// rawKeeperWarnMessages holds configurable warn text overrides in the
// keeper.warn_messages block. Empty strings are treated as not configured.
type rawKeeperWarnMessages struct {
	DefaultWarnText  string `yaml:"default_warn_text"`
	OnDemandWarnText string `yaml:"on_demand_warn_text"`
}

// rawKeeperConfig is the keeper: block in config.yaml.
// Unknown keys at this level are silently ignored (forward-compat per hk-lhu2).
type rawKeeperConfig struct {
	ContextThresholds rawKeeperContextThresholds `yaml:"context_thresholds"`
	WarnMessages      rawKeeperWarnMessages      `yaml:"warn_messages"`
}

// KeeperConfig holds the keeper-level configuration read from the
// .harmonik/config.yaml keeper: block. All fields are optional in the file;
// zero/empty values signal "not configured — defer to CLI flag or built-in default".
// Precedence: CLI flag > config.yaml > compiled default (hk-lhu2).
//
// Bead ref: hk-lhu2.
type KeeperConfig struct {
	// WarnAbsTokens is the absolute warn threshold. Zero = not configured.
	WarnAbsTokens int64
	// ActAbsTokens is the absolute act threshold. Zero = not configured.
	ActAbsTokens int64
	// ForceActAbsTokens is the hard forced-clear ceiling. Zero = not configured.
	ForceActAbsTokens int64
	// ActPctCeil caps the act gate as a fraction of window size. Zero = not configured.
	ActPctCeil float64
	// WarnPctCeil caps the warn gate as a fraction of window size. Zero = not configured.
	WarnPctCeil float64
	// DefaultWarnText overrides the compiled-in wrap-up advisory for non-captain agents.
	// Empty = not configured (use compiled default).
	DefaultWarnText string
	// OnDemandWarnText overrides the compiled-in restart-now advisory for the captain.
	// Empty = not configured (use compiled default).
	OnDemandWarnText string
}

// DaemonConfig holds the daemon-level operational configuration read from the
// .harmonik/config.yaml daemon: block. All fields are optional in the file;
// zero values signal "not configured — defer to CLI flag or built-in default".
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-004b.
// Bead ref: hk-rcp7.
type DaemonConfig struct {
	// WorkflowMode is the daemon-level default workflow mode.
	// Empty = not configured (defer to --workflow-mode flag or dot default per PL-004a).
	// WorkflowModeSingle is NEVER a valid config value; LoadProjectConfig returns
	// *ErrWorkflowModeFloorViolation when it is found (PL-004a review floor).
	WorkflowMode core.WorkflowMode

	// MaxConcurrent is the daemon-level max-concurrent dispatch ceiling.
	// Zero = not configured (defer to --max-concurrent flag or its default).
	// Values ≤ 0 in the file are treated as not configured per PL-004b.
	MaxConcurrent int

	// TargetBranch is the daemon-level target branch value as written in config.yaml.
	// This field is observability/symmetry only per PL-004b: it MUST NOT override
	// the branching.yaml lands_on value in the resolution chain. Callers MUST use
	// branching.Load() for the authoritative target_branch.
	TargetBranch string

	// AllowedRepos is the safelist of absolute repository paths the daemon is
	// permitted to dispatch cross-repo beads against (hk-xfuc). A bead whose
	// target_repo is not in this list is refused with CrossRepoUnsafeError.
	// An empty list means no cross-repo dispatch is allowed.
	// See docs/cross-repo-dispatch.md.
	AllowedRepos []string
}

// rawProjectConfig is the top-level YAML shape for .harmonik/config.yaml.
type rawProjectConfig struct {
	SchemaVersion int                       `yaml:"schema_version"`
	Agents        map[string]rawAgentConfig `yaml:"agents"`
	Daemon        rawDaemonConfig           `yaml:"daemon"` // hk-rcp7: PL-004b daemon: block
	Keeper        rawKeeperConfig           `yaml:"keeper"` // hk-lhu2: keeper config block
}

// rawAgentConfig is the per-agent-type block inside the agents map.
type rawAgentConfig struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// agentConfigEntry holds the resolved (model, effort) pair for a single agent type.
type agentConfigEntry struct {
	model  string
	effort string
}

// ProjectConfig is the decoded and cached representation of .harmonik/config.yaml.
// It is the zero value when the file is absent. Use LookupAgent to query per-type
// values, Daemon for daemon operational settings, and Keeper for keeper settings.
type ProjectConfig struct {
	// entries maps core.AgentType to the configured (model, effort) pair.
	// Only known-at-parse-time entries are stored; unknown keys are discarded.
	entries map[core.AgentType]agentConfigEntry

	// Daemon holds the daemon-level operational config read from the daemon: block.
	// Zero value when the block is absent.
	//
	// Spec ref: specs/process-lifecycle.md §4.1 PL-004b.
	// Bead ref: hk-rcp7.
	Daemon DaemonConfig

	// Keeper holds the keeper-level config read from the keeper: block.
	// Zero value when the block is absent.
	//
	// Bead ref: hk-lhu2.
	Keeper KeeperConfig
}

// LookupAgent returns the (model, effort) pair configured for agentType, or
// ("", "") when the type is absent from the config or the file was absent.
//
// Callers MUST treat an empty returned value as "not configured" and continue
// the resolution walk to tier 3 (compiled defaults).
func (c ProjectConfig) LookupAgent(agentType core.AgentType) (model, effort string) {
	if c.entries == nil {
		return "", ""
	}
	e, ok := c.entries[agentType]
	if !ok {
		return "", ""
	}
	return e.model, e.effort
}

// LoadProjectConfig reads .harmonik/config.yaml under repoRoot and returns the
// decoded ProjectConfig.
//
// Behaviour:
//   - File absent → zero-value ProjectConfig, nil error.
//   - File present, malformed YAML → *ErrMalformedConfigYAML (daemon MUST refuse to start).
//   - schema_version != 1 → *ErrUnsupportedConfigVersion (daemon MUST refuse to start).
//   - Unknown agent keys → silently ignored (forward-compat).
//   - Unknown schema_version for a zero-value file (empty YAML) → zero-value, nil error.
func LoadProjectConfig(repoRoot string) (ProjectConfig, error) {
	path := filepath.Join(repoRoot, projectConfigRelPath)

	//nolint:gosec // G304: path is constructed from operator-supplied ProjectDir, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProjectConfig{}, nil
		}
		return ProjectConfig{}, fmt.Errorf("daemon: project config: reading %s: %w", path, err)
	}

	return parseProjectConfig(path, data)
}

// parseProjectConfig decodes raw YAML bytes into a ProjectConfig.
func parseProjectConfig(path string, data []byte) (ProjectConfig, error) {
	var raw rawProjectConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ProjectConfig{}, &ErrMalformedConfigYAML{Path: path, Cause: err}
	}

	// Empty-file sentinel: schema_version 0 + no agents + no daemon block + no keeper block
	// → absent semantics. A file with only a daemon: or keeper: block but no schema_version: 1
	// falls through to the version check below and returns ErrUnsupportedConfigVersion (fail-fast).
	daemonAbsent := raw.Daemon.WorkflowMode == "" && raw.Daemon.MaxConcurrent == 0 &&
		raw.Daemon.TargetBranch == "" && len(raw.Daemon.AllowedRepos) == 0
	if raw.SchemaVersion == 0 && len(raw.Agents) == 0 &&
		daemonAbsent && raw.Keeper == (rawKeeperConfig{}) {
		return ProjectConfig{}, nil
	}

	if raw.SchemaVersion != projectConfigCurrentVersion {
		return ProjectConfig{}, &ErrUnsupportedConfigVersion{
			Path:    path,
			Version: raw.SchemaVersion,
		}
	}

	// hk-rcp7 PL-004b: parse and validate the daemon: block.
	daemonCfg, err := parseDaemonBlock(path, raw.Daemon)
	if err != nil {
		return ProjectConfig{}, err
	}

	// hk-lhu2: parse the keeper: block (no fail-fast errors; all values optional).
	keeperCfg := parseKeeperBlock(raw.Keeper)

	cfg := ProjectConfig{
		entries: make(map[core.AgentType]agentConfigEntry, len(raw.Agents)),
		Daemon:  daemonCfg,
		Keeper:  keeperCfg,
	}
	for key, agentRaw := range raw.Agents {
		at := core.AgentType(key)
		// Unknown agent keys are silently ignored (forward-compat per bead spec).
		// We store all keys since AgentType.Valid() is a syntax check; semantic
		// filtering happens at LookupAgent call time via the caller's key.
		cfg.entries[at] = agentConfigEntry{
			model:  agentRaw.Model,
			effort: agentRaw.Effort,
		}
	}

	return cfg, nil
}

// parseDaemonBlock validates and converts a rawDaemonConfig into a DaemonConfig.
//
// Validation rules per PL-004b:
//   - workflow_mode absent → zero DaemonConfig.WorkflowMode (defer to flag/default).
//   - workflow_mode present but not in {review-loop, dot, single} → *ErrMalformedConfigYAML.
//   - workflow_mode == single → *ErrWorkflowModeFloorViolation (PL-004a review floor).
//   - max_concurrent ≤ 0 → treated as not configured (zero DaemonConfig.MaxConcurrent).
//   - target_branch → stored for observability/symmetry only; not used in resolution chain.
func parseDaemonBlock(path string, raw rawDaemonConfig) (DaemonConfig, error) {
	cfg := DaemonConfig{
		TargetBranch: raw.TargetBranch, // observability/symmetry only per PL-004b
	}

	if raw.WorkflowMode != "" {
		wm := core.WorkflowMode(raw.WorkflowMode)
		if !wm.Valid() {
			return DaemonConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("daemon.workflow_mode %q: unknown value; must be one of review-loop, dot (single is forbidden at daemon level)", raw.WorkflowMode),
			}
		}
		// PL-004a review floor: single MUST NOT be reachable from the daemon-level
		// default or any config file path.  Only an explicit per-bead workflow:single
		// label (audited via review_bypassed) may dispatch in single mode.
		if wm == core.WorkflowModeSingle {
			return DaemonConfig{}, &ErrWorkflowModeFloorViolation{Path: path, Value: raw.WorkflowMode}
		}
		cfg.WorkflowMode = wm
	}

	// Values ≤ 0 are treated as "not configured" per PL-004b.
	if raw.MaxConcurrent > 0 {
		cfg.MaxConcurrent = raw.MaxConcurrent
	}

	// allowed_repos: stored as-is; nil/empty = cross-repo dispatch not permitted.
	cfg.AllowedRepos = raw.AllowedRepos

	return cfg, nil
}

// parseKeeperBlock converts a rawKeeperConfig into a KeeperConfig.
// All values are optional; ≤ 0 / empty strings are stored as zero values so
// callers can detect "not configured" and defer to the CLI flag or compiled
// default. Unknown YAML keys at any level are silently ignored (forward-compat
// per hk-lhu2).
//
// Bead ref: hk-lhu2.
func parseKeeperBlock(raw rawKeeperConfig) KeeperConfig {
	cfg := KeeperConfig{}
	t := raw.ContextThresholds
	// Values ≤ 0 are treated as "not configured" — defer to CLI flag or compiled default.
	if t.WarnAbsTokens > 0 {
		cfg.WarnAbsTokens = t.WarnAbsTokens
	}
	if t.ActAbsTokens > 0 {
		cfg.ActAbsTokens = t.ActAbsTokens
	}
	if t.ForceActAbsTokens > 0 {
		cfg.ForceActAbsTokens = t.ForceActAbsTokens
	}
	if t.ActPctCeil > 0 {
		cfg.ActPctCeil = t.ActPctCeil
	}
	if t.WarnPctCeil > 0 {
		cfg.WarnPctCeil = t.WarnPctCeil
	}
	// Empty strings are treated as "not configured" — defer to compiled default.
	cfg.DefaultWarnText = raw.WarnMessages.DefaultWarnText
	cfg.OnDemandWarnText = raw.WarnMessages.OnDemandWarnText
	return cfg
}
