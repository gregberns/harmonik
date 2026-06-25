// Package run owns the durable run-session registry: per-run JSON records stored
// under .harmonik/runs/<runID>.json. Each record tracks a bead-run that was
// launched in an independent tmux session so the daemon can discover and adopt
// surviving sessions after a SIGKILL restart.
//
// Parallel to internal/crew/registry.go — same atomic-write pattern, simpler
// schema (no name validation needed; RunIDs are UUIDs, always valid filenames).
package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	runsSubDir    = "runs"
	schemaVersion = 1
)

// ErrNotFound is returned by Load and Remove when the record file is absent.
var ErrNotFound = errors.New("run: record not found")

// Record is a single run-session registry entry.
// Persisted to .harmonik/runs/<RunID>.json when a bead-run starts in an
// independent tmux session; removed on normal run completion.
type Record struct {
	SchemaVersion int       `json:"schema_version"`
	RunID         string    `json:"run_id"`
	BeadID        string    `json:"bead_id"`
	QueueName     string    `json:"queue_name"`
	QueueID       string    `json:"queue_id"`
	GroupIndex    int       `json:"group_index"`
	ItemIndex     int       `json:"item_index"`
	SessionName   string    `json:"session_name"`
	StartedAt     time.Time `json:"started_at"`
}

func runsDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", runsSubDir)
}

func recordPath(projectDir, runID string) string {
	return filepath.Join(runsDir(projectDir), runID+".json")
}

// Write atomically writes r to .harmonik/runs/<r.RunID>.json.
// The directory is created if absent.
func Write(projectDir string, r Record) error {
	dir := runsDir(projectDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("run: mkdir %q: %w", dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("run: marshal %q: %w", r.RunID, err)
	}
	tmp := recordPath(projectDir, r.RunID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("run: write-tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, recordPath(projectDir, r.RunID)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("run: rename %q: %w", r.RunID, err)
	}
	return nil
}

// Load reads the record for runID from .harmonik/runs/<runID>.json.
// Returns ErrNotFound when the file is absent.
func Load(projectDir, runID string) (Record, error) {
	data, err := os.ReadFile(recordPath(projectDir, runID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("run: load %q: %w", runID, err)
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return Record{}, fmt.Errorf("run: unmarshal %q: %w", runID, err)
	}
	return r, nil
}

// Remove deletes .harmonik/runs/<runID>.json.
// Returns ErrNotFound when the file is absent.
func Remove(projectDir, runID string) error {
	path := recordPath(projectDir, runID)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("run: remove %q: %w", runID, err)
	}
	return nil
}

// List returns all run records found in .harmonik/runs/.
// An empty or missing directory returns a nil slice without error.
func List(projectDir string) ([]Record, error) {
	dir := runsDir(projectDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("run: list %q: %w", dir, err)
	}
	var records []Record
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()
		runID := name[:len(name)-len(".json")]
		r, loadErr := Load(projectDir, runID)
		if loadErr != nil {
			continue // skip corrupt or tmp records
		}
		records = append(records, r)
	}
	return records, nil
}
