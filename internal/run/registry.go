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
	"sync"
	"time"
)

const (
	runsSubDir    = "runs"
	schemaVersion = 1
)

// ErrNotFound is returned by Remove when the record file is absent.
var ErrNotFound = errors.New("run: record not found")

var runNamespaceMu sync.Mutex

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
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	if existing, err := os.ReadFile(recordPath(projectDir, r.RunID)); err == nil {
		pathRunID, pathErr := canonicalRunBasename(r.RunID + ".json")
		_, decodeErr := decodeLegacyRecord(existing, pathRunID)
		if pathErr != nil || decodeErr != nil {
			return &DispatchConflictError{Detail: "legacy writer cannot replace an unclassified or non-legacy record"}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("run: inspect existing record %q: %w", r.RunID, err)
	}
	dir := runsDir(projectDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("run: mkdir %q: %w", dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("run: marshal %q: %w", r.RunID, err)
	}
	tmp := recordPath(projectDir, r.RunID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		if cleanupErr := os.Remove(tmp); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			return fmt.Errorf("run: write-tmp %q: %w", tmp, errors.Join(err, cleanupErr))
		}
		return fmt.Errorf("run: write-tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, recordPath(projectDir, r.RunID)); err != nil {
		if cleanupErr := os.Remove(tmp); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			return fmt.Errorf("run: rename %q: %w", r.RunID, errors.Join(err, cleanupErr))
		}
		return fmt.Errorf("run: rename %q: %w", r.RunID, err)
	}
	return nil
}

// Remove deletes .harmonik/runs/<runID>.json.
// Returns ErrNotFound when the file is absent.
func Remove(projectDir, runID string) error {
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	path := recordPath(projectDir, runID)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("run: remove %q: %w", runID, err)
	}
	return nil
}
