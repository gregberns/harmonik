// Package release — ledger JSON file persistence.
//
// LedgerFile reads and writes the mutable release ledger at a configurable
// path. The on-disk format is a versioned JSON envelope so future schema
// evolution does not silently corrupt old ledger files.
//
// Default path: <project>/.harmonik/release-ledger.json
//
// Spec ref: specs/release-pipeline.md §4.4.
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// LedgerSchemaVersion is the current schema version for the on-disk ledger.
	LedgerSchemaVersion = 1

	// LedgerFileName is the default filename for the on-disk release ledger.
	LedgerFileName = "release-ledger.json"
)

// ledgerEnvelope is the top-level JSON structure for the on-disk ledger.
type ledgerEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Entries       []ReleaseEntry `json:"entries"`
}

// LedgerPath returns the default path for the release ledger within projectDir.
func LedgerPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", LedgerFileName)
}

// LoadLedgerFile reads the ledger from path. If the file does not exist, an
// empty ledger is returned without error (first-use case).
func LoadLedgerFile(path string) ([]ReleaseEntry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ReleaseEntry{}, nil
		}
		return nil, fmt.Errorf("release: read ledger %s: %w", path, err)
	}
	var env ledgerEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("release: parse ledger %s: %w", path, err)
	}
	if env.SchemaVersion != LedgerSchemaVersion {
		return nil, fmt.Errorf("release: ledger %s has schema_version %d; expected %d",
			path, env.SchemaVersion, LedgerSchemaVersion)
	}
	if env.Entries == nil {
		env.Entries = []ReleaseEntry{}
	}
	return env.Entries, nil
}

// SaveLedgerFile writes entries to path atomically (write to temp + rename).
func SaveLedgerFile(path string, entries []ReleaseEntry) error {
	if entries == nil {
		entries = []ReleaseEntry{}
	}
	env := ledgerEnvelope{
		SchemaVersion: LedgerSchemaVersion,
		Entries:       entries,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("release: marshal ledger: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: matches .harmonik dir conventions
		return fmt.Errorf("release: create ledger dir %s: %w", dir, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306: ledger is not a secret
		return fmt.Errorf("release: write temp ledger %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("release: rename ledger %s → %s: %w", tmp, path, err)
	}
	return nil
}
