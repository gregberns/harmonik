// Package agentmanifest loads and validates agent type folders from .harmonik/agents/<type>/.
//
// Each type folder holds manifest.yaml (configuration), soul.md (identity provenance master),
// and operating.md (how-I-work loop). A type defines immutable config shared by all running
// instances of that role; per-instance mutable state (handoff, launch data) lives elsewhere.
//
// Spec ref: .kerf/works/agent-manifest/SPEC.md §1–§6.
package agentmanifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	sharedSkillsDir = "_skills"
	manifestFile    = "manifest.yaml"
	soulFile        = "soul.md"
	operatingFile   = "operating.md"
)

// Sentinel errors returned by Load and ResolveRef.
var (
	// ErrNotFound is returned when the requested type folder or manifest file is absent.
	ErrNotFound = errors.New("agentmanifest: type folder not found")
	// ErrReservedName is returned when a type name starts with '_' (reserved prefix).
	ErrReservedName = errors.New("agentmanifest: type name starting with '_' is reserved")
	// ErrInvalid is returned when the manifest is present but fails schema validation.
	ErrInvalid = errors.New("agentmanifest: manifest validation failed")
)

// MaxCardinality is the upper bound on live instances of a type.
// MaxUnlimited (-1) represents the "n" (unlimited) cardinality.
type MaxCardinality int

// MaxUnlimited signals an unbounded number of live instances ("n" in the manifest).
const MaxUnlimited MaxCardinality = -1

// UnmarshalYAML accepts an integer or the string "n" (== MaxUnlimited).
func (m *MaxCardinality) UnmarshalYAML(value *yaml.Node) error {
	if value.Value == "n" || value.Value == "N" {
		*m = MaxUnlimited
		return nil
	}
	var n int
	if err := value.Decode(&n); err != nil {
		return fmt.Errorf("agentmanifest: cardinality.max must be a non-negative integer or \"n\": %w", err)
	}
	if n < 0 {
		return errors.New("agentmanifest: cardinality.max must be >= 0 or \"n\"")
	}
	*m = MaxCardinality(n)
	return nil
}

// Cardinality declares the minimum and maximum live instances of a type.
// max: 1 makes a type a singleton; max: n (MaxUnlimited) allows any number.
type Cardinality struct {
	Min int            `yaml:"min"`
	Max MaxCardinality `yaml:"max"`
}

// Identity names the provenance master file and the parent role for intent grafting.
type Identity struct {
	// Soul is the path to soul.md relative to the type folder (always "soul.md").
	Soul string `yaml:"soul"`
	// ParentIntent is the parent type name or the reserved terminal "operator".
	// brief grafts the parent's one-line intent into the boot document at emit time.
	ParentIntent string `yaml:"parent_intent"`
}

// ContextEntry declares a resource to include in the boot document.
type ContextEntry struct {
	// Ref is a bare skill name (resolved against _skills/ then the type folder)
	// or a path-bearing literal (contains '/'; relative to repo root).
	Ref string `yaml:"ref"`
	// As declares the resource kind: instruction | skill | doc.
	As string `yaml:"as"`
	// Presence declares injection strategy: injected | retrieved | embodied.
	Presence string `yaml:"presence"`
}

// Trigger is a declared, toggleable wake subscription in the boot document.
type Trigger struct {
	ID string `yaml:"id"`
	// Source is the wake origin: queue | cron | interval | event | comms | manual | operator.
	Source  string `yaml:"source"`
	Enabled bool   `yaml:"enabled"`
	Every   string `yaml:"every,omitempty"`   // cron/interval period (e.g. "6h")
	Deliver string `yaml:"deliver,omitempty"` // delivery target for scheduled triggers ("comms")
	Message string `yaml:"message,omitempty"` // message to deliver on a cron/interval trigger
	// ActivityGuard is the fleet-activity window for cron/interval triggers.
	// When set (e.g. "24h"), the daemon fires this trigger only if fleet activity was
	// observed within the window. Empty means fire unconditionally on schedule.
	// Daemon-side enforcement is deferred; this field declares the guard contract.
	ActivityGuard string `yaml:"activity_guard,omitempty"`
}

// Handoff describes where episodic session state is stored.
type Handoff struct {
	// Channel is the storage location: "private" == own HANDOFF-<name>.md.
	Channel string `yaml:"channel"`
}

// Keeper holds keeper threshold configuration.
type Keeper struct {
	// Thresholds is a named threshold profile: "default" or a custom profile name.
	Thresholds string `yaml:"thresholds"`
}

// Lifecycle controls agent restart behaviour.
type Lifecycle struct {
	// SelfRestart true means the agent is restarted (not duplicated) after exit.
	SelfRestart bool `yaml:"self_restart"`
}

// Markers holds declarative event constraints for watch to validate against the event stream.
type Markers struct {
	// NeverEmits lists event types this agent type must never emit.
	NeverEmits []string `yaml:"never_emits"`
}

// Manifest is the parsed content of a type folder's manifest.yaml.
// It mirrors the schema in SPEC.md §2.
type Manifest struct {
	// Type must equal the folder name.
	Type        string      `yaml:"type"`
	Cardinality Cardinality `yaml:"cardinality"`
	// Harness is the default actor: claude | codex | pi.
	Harness   string         `yaml:"harness"`
	Identity  Identity       `yaml:"identity"`
	Context   []ContextEntry `yaml:"context"`
	Triggers  []Trigger      `yaml:"triggers"`
	Handoff   Handoff        `yaml:"handoff"`
	Keeper    Keeper         `yaml:"keeper"`
	Lifecycle Lifecycle      `yaml:"lifecycle"`
	// ToolsDir is an inert forward-compatibility affordance (SPEC §8 — deferred).
	ToolsDir *string `yaml:"tools_dir"`
	Markers  Markers `yaml:"markers"`
}

// TypeFolder holds the loaded manifest and the identity file contents for a type.
type TypeFolder struct {
	// Name is the type name (== the folder name).
	Name string
	// Dir is the absolute path to the type folder.
	Dir string
	// Manifest is the parsed manifest.yaml.
	Manifest Manifest
	// SoulContent is the byte-for-byte content of soul.md (the provenance master).
	SoulContent string
	// OperatingContent is the content of operating.md.
	OperatingContent string
}

// Load reads .harmonik/agents/<typeName>/manifest.yaml, soul.md, and operating.md
// into a TypeFolder and validates the manifest schema.
//
// agentsDir is the absolute path to the .harmonik/agents/ directory.
// Returns ErrReservedName when typeName starts with '_'.
// Returns ErrNotFound when the folder or manifest file is absent.
// Returns ErrInvalid when the manifest is present but fails schema validation.
func Load(agentsDir, typeName string) (*TypeFolder, error) {
	if strings.HasPrefix(typeName, "_") {
		return nil, fmt.Errorf("%w: %q", ErrReservedName, typeName)
	}

	dir := filepath.Join(agentsDir, typeName)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, typeName)
		}
		return nil, fmt.Errorf("agentmanifest: stat type dir %q: %w", dir, err)
	}

	mPath := filepath.Join(dir, manifestFile)
	//nolint:gosec // G304: mPath is constructed from caller-supplied agentsDir + validated typeName
	mData, err := os.ReadFile(mPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: type %q has no %s", ErrNotFound, typeName, manifestFile)
		}
		return nil, fmt.Errorf("agentmanifest: read %q: %w", mPath, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(mData, &m); err != nil {
		return nil, fmt.Errorf("%w: parse %q: %v", ErrInvalid, mPath, err)
	}

	soulPath := filepath.Join(dir, soulFile)
	//nolint:gosec // G304: soulPath is constructed from caller-supplied agentsDir + validated typeName
	soulData, err := os.ReadFile(soulPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: type %q has no %s", ErrInvalid, typeName, soulFile)
		}
		return nil, fmt.Errorf("agentmanifest: read %q: %w", soulPath, err)
	}

	opPath := filepath.Join(dir, operatingFile)
	//nolint:gosec // G304: opPath is constructed from caller-supplied agentsDir + validated typeName
	opData, err := os.ReadFile(opPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: type %q has no %s", ErrInvalid, typeName, operatingFile)
		}
		return nil, fmt.Errorf("agentmanifest: read %q: %w", opPath, err)
	}

	tf := &TypeFolder{
		Name:             typeName,
		Dir:              dir,
		Manifest:         m,
		SoulContent:      string(soulData),
		OperatingContent: string(opData),
	}
	if err := validateManifest(tf); err != nil {
		return nil, err
	}
	return tf, nil
}

var (
	allowedAs            = map[string]bool{"instruction": true, "skill": true, "doc": true}
	allowedPresence      = map[string]bool{"injected": true, "retrieved": true, "embodied": true}
	allowedTriggerSource = map[string]bool{
		"queue": true, "cron": true, "interval": true,
		"event": true, "comms": true, "manual": true, "operator": true,
	}
)

// validateManifest checks required fields and enumeration values.
// It returns ErrInvalid (wrapped) describing the first defect found.
func validateManifest(tf *TypeFolder) error {
	m := &tf.Manifest
	if m.Type == "" {
		return fmt.Errorf("%w: type %q: field \"type\" is required", ErrInvalid, tf.Name)
	}
	if m.Type != tf.Name {
		return fmt.Errorf("%w: type %q: field \"type\" (%q) must match the folder name", ErrInvalid, tf.Name, m.Type)
	}
	if m.Harness == "" {
		return fmt.Errorf("%w: type %q: field \"harness\" is required", ErrInvalid, tf.Name)
	}
	if m.Identity.Soul == "" {
		return fmt.Errorf("%w: type %q: field \"identity.soul\" is required", ErrInvalid, tf.Name)
	}
	if m.Identity.ParentIntent == "" {
		return fmt.Errorf("%w: type %q: field \"identity.parent_intent\" is required", ErrInvalid, tf.Name)
	}
	for i, c := range m.Context {
		if c.Ref == "" {
			return fmt.Errorf("%w: type %q: context[%d].ref is required", ErrInvalid, tf.Name, i)
		}
		if !allowedAs[c.As] {
			return fmt.Errorf("%w: type %q: context[%d].as %q must be one of {instruction,skill,doc}", ErrInvalid, tf.Name, i, c.As)
		}
		if !allowedPresence[c.Presence] {
			return fmt.Errorf("%w: type %q: context[%d].presence %q must be one of {injected,retrieved,embodied}", ErrInvalid, tf.Name, i, c.Presence)
		}
	}
	for i, t := range m.Triggers {
		if t.ID == "" {
			return fmt.Errorf("%w: type %q: triggers[%d].id is required", ErrInvalid, tf.Name, i)
		}
		if !allowedTriggerSource[t.Source] {
			return fmt.Errorf("%w: type %q: triggers[%d].source %q must be one of {queue,cron,interval,event,comms,manual,operator}", ErrInvalid, tf.Name, i, t.Source)
		}
	}
	return nil
}

// ResolveRef returns the filesystem path for a context ref under the given agentsDir.
//
// Resolution rule (SPEC §6):
//   - A path-bearing ref (contains '/') is returned as-is (caller resolves relative to repo root).
//   - A bare ref resolves against agentsDir/_skills/<ref> first, then agentsDir/<typeName>/<ref>.
//
// Returns ("", ErrNotFound) when a bare ref cannot be resolved in either location.
func ResolveRef(agentsDir, typeName, ref string) (string, error) {
	if strings.Contains(ref, "/") {
		return ref, nil
	}
	// Bare ref: shared _skills/ first.
	sharedPath := filepath.Join(agentsDir, sharedSkillsDir, ref)
	if _, err := os.Stat(sharedPath); err == nil {
		return sharedPath, nil
	}
	// Then the type's own folder.
	typePath := filepath.Join(agentsDir, typeName, ref)
	if _, err := os.Stat(typePath); err == nil {
		return typePath, nil
	}
	return "", fmt.Errorf("%w: ref %q not found in _skills/ or type folder %q", ErrNotFound, ref, typeName)
}
