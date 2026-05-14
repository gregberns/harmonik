package daemon

// projectconfig.go — per-project model/effort config loader for
// .harmonik/config.yaml (hk-bfvk7).
//
// Implements tier-2 of the EM-012b model/effort resolution chain:
// per-project .harmonik/config.yaml supplies per-agent-type defaults that take
// precedence over compiled-in tier-3 defaults but are overridden by per-bead
// labels (tier-1).
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
//
// Unknown agent keys are silently ignored (forward-compat).
// Unknown schema_version → ErrUnsupportedConfigVersion.
// Parse error on a present file → ErrMalformedConfigYAML.
// Absent file → zero-value ProjectConfig, nil error.
//
// # Spec refs
//
// specs/execution-model.md §4.3 EM-012b — tier-2 slot.
// specs/handler-contract.md §4.10 HC-055a — ModelPreference invariants.
//
// Bead: hk-bfvk7.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
	"gopkg.in/yaml.v3"
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

// rawProjectConfig is the top-level YAML shape for .harmonik/config.yaml.
type rawProjectConfig struct {
	SchemaVersion int                       `yaml:"schema_version"`
	Agents        map[string]rawAgentConfig `yaml:"agents"`
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
// It is the zero value when the file is absent. Use LookupAgent to query per-type values.
type ProjectConfig struct {
	// entries maps core.AgentType to the configured (model, effort) pair.
	// Only known-at-parse-time entries are stored; unknown keys are discarded.
	entries map[core.AgentType]agentConfigEntry
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

	// Empty file (schema_version 0 + no agents) → absent semantics.
	if raw.SchemaVersion == 0 && len(raw.Agents) == 0 {
		return ProjectConfig{}, nil
	}

	if raw.SchemaVersion != projectConfigCurrentVersion {
		return ProjectConfig{}, &ErrUnsupportedConfigVersion{
			Path:    path,
			Version: raw.SchemaVersion,
		}
	}

	cfg := ProjectConfig{
		entries: make(map[core.AgentType]agentConfigEntry, len(raw.Agents)),
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
