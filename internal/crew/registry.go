// Package crew owns the durable crew registry: per-crew JSON records stored
// under .harmonik/crew/<name>.json. It depends only on stdlib and internal/core.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.3.
package crew

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	crewSubDir    = "crew"
	schemaVersion = 1
	maxNameLen    = 64
)

// validName matches the allowed charset: lowercase letters, digits, hyphens.
var validName = regexp.MustCompile(`^[a-z0-9-]+$`)

// ErrInvalidName is returned when a crew name fails charset or length validation.
var ErrInvalidName = errors.New("crew: name must be 1–64 chars, charset [a-z0-9-], no '/' or '..'")

// ErrNotFound is returned by Load and Remove when the record does not exist.
var ErrNotFound = errors.New("crew: record not found")

// ErrWriteFailed is returned when any step in the atomic write sequence fails.
var ErrWriteFailed = errors.New("crew: atomic write to registry file failed")

// Record is a single crew-member registry entry.
// Schema-versioned per project convention; schema_version == 1.
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	SessionID     string    `json:"session_id"`
	Queue         string    `json:"queue"`
	Epic          string    `json:"epic"`
	Handle        string    `json:"handle"`
	StartedAt     time.Time `json:"started_at"`
}

// validateName rejects names that contain '/' or '..', fail the charset check,
// or are outside the 1–64 character length range.
func validateName(name string) error {
	if name == "" || len(name) > maxNameLen {
		return ErrInvalidName
	}
	if name == ".." {
		return ErrInvalidName
	}
	if !validName.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// crewDir returns .harmonik/crew under projectDir.
func crewDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", crewSubDir)
}

// recordPath returns the canonical path for a crew member's record file.
func recordPath(projectDir, name string) string {
	return filepath.Join(crewDir(projectDir), name+".json")
}

// Write atomically creates or overwrites .harmonik/crew/<name>.json using the
// WM-026 temp-write+rename sequence (write temp → fsync → rename → fsync parent).
// The record's SchemaVersion is set to 1 before writing.
func Write(projectDir string, r Record) error {
	if err := validateName(r.Name); err != nil {
		return err
	}

	r.SchemaVersion = schemaVersion
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("%w: marshal: %w", ErrWriteFailed, err)
	}

	dir := crewDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: mkdir crew: %w", ErrWriteFailed, err)
	}

	target := recordPath(projectDir, r.Name)
	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())

	//nolint:gosec // G304: tmpPath derived from projectDir/.harmonik/crew/ + Getpid
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create temp %q: %w", ErrWriteFailed, tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()                  //nolint:errcheck // cleanup; primary error returned below
		_ = os.Remove(tmpPath)         //nolint:errcheck // cleanup on write failure
		return fmt.Errorf("%w: write temp %q: %w", ErrWriteFailed, tmpPath, err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()                  //nolint:errcheck // cleanup; primary error returned below
		_ = os.Remove(tmpPath)         //nolint:errcheck // cleanup on sync failure
		return fmt.Errorf("%w: fsync temp %q: %w", ErrWriteFailed, tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on close failure
		return fmt.Errorf("%w: close temp %q: %w", ErrWriteFailed, tmpPath, err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on rename failure
		return fmt.Errorf("%w: rename %q → %q: %w", ErrWriteFailed, tmpPath, target, err)
	}

	//nolint:gosec // G304: dir is the daemon-internal .harmonik/crew directory
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w: open parent dir %q: %w", ErrWriteFailed, dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()          //nolint:errcheck // cleanup; primary error returned below
		return fmt.Errorf("%w: fsync parent dir %q: %w", ErrWriteFailed, dir, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("%w: close parent dir %q: %w", ErrWriteFailed, dir, err)
	}

	return nil
}

// Load reads .harmonik/crew/<name>.json and returns the parsed Record.
// Returns ErrNotFound when the file is absent.
func Load(projectDir, name string) (Record, error) {
	if err := validateName(name); err != nil {
		return Record{}, err
	}

	path := recordPath(projectDir, name)
	//nolint:gosec // G304: path derived from projectDir/.harmonik/crew/
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("crew: read %q: %w", path, err)
	}

	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("crew: parse %q: %w", path, err)
	}
	return r, nil
}

// List scans .harmonik/crew/*.json and returns all records sorted by name.
// Returns an empty slice (not an error) when the directory is absent.
func List(projectDir string) ([]Record, error) {
	dir := crewDir(projectDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("crew: readdir %q: %w", dir, err)
	}

	records := make([]Record, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()[:len(e.Name())-len(".json")]
		if validateName(name) != nil {
			continue
		}
		r, err := Load(projectDir, name)
		if err != nil {
			return nil, fmt.Errorf("crew: load %q: %w", name, err)
		}
		records = append(records, r)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})
	return records, nil
}

// UpdateSessionID performs a read-modify-write that mutates only the session_id
// field of the named record. All other fields are preserved.
func UpdateSessionID(projectDir, name, sessionID string) error {
	r, err := Load(projectDir, name)
	if err != nil {
		return err
	}
	r.SessionID = sessionID
	return Write(projectDir, r)
}

// Remove deletes .harmonik/crew/<name>.json.
// Returns ErrNotFound when the file is absent.
func Remove(projectDir, name string) error {
	if err := validateName(name); err != nil {
		return err
	}

	path := recordPath(projectDir, name)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("crew: remove %q: %w", path, err)
	}
	return nil
}
