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
}

// Config is the top-level structure decoded from .harmonik/workers.yaml.
type Config struct {
	Version int      `yaml:"version"`
	Workers []Worker `yaml:"workers"`
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
