// Package branching loads and caches the per-project branching defaults from
// .harmonik/branching.yaml.
//
// # File location and ownership
//
// The file lives at <repo-root>/.harmonik/branching.yaml. It is intended to be
// checked into the project repository so that the branching convention travels
// with the team's code. It is NOT added to .gitignore by this package; operators
// who want per-developer overrides should add a .harmonik/branching.local.yaml
// sibling (a future extension; not parsed here).
//
// # Spec reference
//
// This loader implements the WM-005b resolution chain layer (b): project-level
// defaults from the config file, with lowest precedence over the per-bead
// ## Branching section but highest precedence over the daemon hardcoded defaults.
// See specs/workspace-model.md §4.2 WM-005b.
//
// # Missing file semantics
//
// If .harmonik/branching.yaml is absent, Load and LoadCached return a zero-value
// Defaults (all fields empty string, Version 0) and a nil error. The caller is
// responsible for substituting the spec-level defaults (start_from=main,
// lands_on=main, landing_strategy=squash).
package branching

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// configRelPath is the path of the branching config file relative to the
// repository root.
const configRelPath = ".harmonik/branching.yaml"

// currentVersion is the only schema version this loader accepts.
const currentVersion = 1

// LandingStrategy is the enumerated set of merge strategies supported by WM-019b.
type LandingStrategy string

const (
	LandingStrategySquash      LandingStrategy = "squash"
	LandingStrategyCherryPick  LandingStrategy = "cherry-pick"
	LandingStrategyUnspecified LandingStrategy = "" // file absent or field omitted
)

// Defaults holds the project-level branching defaults parsed from
// .harmonik/branching.yaml. All fields are optional in the file; zero values
// signal "no project default, fall back to daemon default."
type Defaults struct {
	// Version is the schema version declared in the file. Zero when file is absent.
	Version int

	// StartFrom is the git ref the worktree branch is cut from at workspace-create
	// time. Corresponds to the YAML key defaults.start_from. Spec default: "main".
	StartFrom string

	// LandsOn is the git ref the task branch's squash-merge (or cherry-pick) lands
	// on at run-terminal-success. Corresponds to the YAML key defaults.lands_on.
	// Spec default: "main".
	LandsOn string

	// LandingStrategy selects squash or cherry-pick merge behaviour.
	// Corresponds to the YAML key defaults.landing_strategy. Spec default: "squash".
	LandingStrategy LandingStrategy

	// ProtectBranches is the set of branch names the daemon must never use as
	// a merge target. Corresponds to the YAML key defaults.protect_branches.
	// The zero value (nil) means no project-level protection list.
	ProtectBranches []string
}

// rawFile is the top-level shape decoded from the YAML file.
// Unknown keys cause a warning (not an error) because we use yaml.v3 in
// non-strict mode at this level, and walk the node manually for the
// defaults sub-map.
type rawFile struct {
	Version  int                    `yaml:"version"`
	Defaults map[string]interface{} `yaml:"defaults"`
}

// ErrMalformedYAML is returned when the file exists but cannot be parsed.
type ErrMalformedYAML struct {
	Path  string
	Cause error
}

func (e *ErrMalformedYAML) Error() string {
	return fmt.Sprintf("branching: malformed YAML in %s: %v", e.Path, e.Cause)
}

func (e *ErrMalformedYAML) Unwrap() error { return e.Cause }

// ErrUnsupportedVersion is returned when the file declares a version other
// than currentVersion (1).
type ErrUnsupportedVersion struct {
	Path    string
	Version int
}

func (e *ErrUnsupportedVersion) Error() string {
	return fmt.Sprintf("branching: unsupported version %d in %s (want %d)", e.Version, e.Path, currentVersion)
}

// ErrInvalidLandingStrategy is returned when defaults.landing_strategy is
// present but not one of "squash" or "cherry-pick".
type ErrInvalidLandingStrategy struct {
	Path  string
	Value string
}

func (e *ErrInvalidLandingStrategy) Error() string {
	return fmt.Sprintf("branching: invalid landing_strategy %q in %s (must be \"squash\" or \"cherry-pick\")", e.Value, e.Path)
}

// Load reads .harmonik/branching.yaml under repoRoot and returns the decoded
// Defaults.
//
// Behaviour:
//   - File absent → zero-value Defaults, nil error.
//   - File present, malformed YAML → *ErrMalformedYAML.
//   - version != 1 → *ErrUnsupportedVersion.
//   - Unknown keys under defaults: → slog warning, not an error.
//   - landing_strategy not in {"squash","cherry-pick"} → *ErrInvalidLandingStrategy.
func Load(repoRoot string) (Defaults, error) {
	path := filepath.Join(repoRoot, configRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Defaults{}, nil
		}
		return Defaults{}, fmt.Errorf("branching: reading %s: %w", path, err)
	}
	return parse(path, data)
}

// parse decodes the raw YAML bytes into Defaults.
func parse(path string, data []byte) (Defaults, error) {
	var raw rawFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Defaults{}, &ErrMalformedYAML{Path: path, Cause: err}
	}

	// A completely empty file unmarshals to zero-value; treat as absent.
	if raw.Version == 0 && len(raw.Defaults) == 0 {
		return Defaults{}, nil
	}

	if raw.Version != currentVersion {
		return Defaults{}, &ErrUnsupportedVersion{Path: path, Version: raw.Version}
	}

	out := Defaults{Version: raw.Version}

	// Known keys under defaults:
	knownKeys := map[string]bool{
		"start_from":       true,
		"lands_on":         true,
		"landing_strategy": true,
		"protect_branches": true,
	}

	for k, v := range raw.Defaults {
		if !knownKeys[k] {
			slog.Warn("branching: unknown key under defaults — ignored",
				"key", k,
				"file", path,
			)
			continue
		}
		switch k {
		case "protect_branches":
			list, ok := v.([]interface{})
			if !ok {
				return Defaults{}, &ErrMalformedYAML{
					Path:  path,
					Cause: fmt.Errorf("key %q: expected list of strings, got %T", k, v),
				}
			}
			out.ProtectBranches = make([]string, 0, len(list))
			for i, item := range list {
				s, ok := item.(string)
				if !ok {
					return Defaults{}, &ErrMalformedYAML{
						Path:  path,
						Cause: fmt.Errorf("key %q: item %d: expected string, got %T", k, i, item),
					}
				}
				out.ProtectBranches = append(out.ProtectBranches, s)
			}
		default:
			str, ok := v.(string)
			if !ok {
				return Defaults{}, &ErrMalformedYAML{
					Path:  path,
					Cause: fmt.Errorf("key %q: expected string, got %T", k, v),
				}
			}
			switch k {
			case "start_from":
				out.StartFrom = str
			case "lands_on":
				out.LandsOn = str
			case "landing_strategy":
				switch LandingStrategy(str) {
				case LandingStrategySquash, LandingStrategyCherryPick:
					out.LandingStrategy = LandingStrategy(str)
				default:
					return Defaults{}, &ErrInvalidLandingStrategy{Path: path, Value: str}
				}
			}
		}
	}

	return out, nil
}

// cacheEntry holds a cached load result together with the file mtime at the
// time of the load. A zero mtime means the file was absent.
type cacheEntry struct {
	mtime   time.Time
	result  Defaults
	loadErr error
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*cacheEntry{}
)

// LoadCached is like Load but caches the result per repoRoot, invalidating the
// cache when the file's mtime changes. It is safe for concurrent callers.
//
// If the file is absent at load time, the zero-value Defaults is cached under
// the sentinel mtime time.Time{}; a subsequent appearance of the file at a
// later mtime will invalidate the cache and load fresh.
func LoadCached(repoRoot string) (Defaults, error) {
	path := filepath.Join(repoRoot, configRelPath)

	mtime, statErr := fileMtime(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		// Unexpected stat error; skip cache and delegate to Load.
		return Load(repoRoot)
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if e, ok := cache[repoRoot]; ok && e.mtime.Equal(mtime) {
		return e.result, e.loadErr
	}

	// Cache miss or mtime changed: reload (without holding the lock across I/O
	// for simplicity at MVH scale; the lock is re-acquired to write back).
	cacheMu.Unlock()
	result, loadErr := Load(repoRoot)
	cacheMu.Lock()

	cache[repoRoot] = &cacheEntry{mtime: mtime, result: result, loadErr: loadErr}
	return result, loadErr
}

// fileMtime returns the modification time of path, or the zero time if the
// file does not exist.
func fileMtime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
