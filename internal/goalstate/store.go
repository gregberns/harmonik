package goalstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ErrNotFound is returned by Read when goal-state.json does not exist yet.
var ErrNotFound = errors.New("goal-state.json not found")

// Read reads and unmarshals goal-state.json from the project directory.
// Returns ErrNotFound when the file is absent (first-run case).
func Read(projectDir string) (*GoalState, error) {
	path := Path(projectDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var gs GoalState
	if err := json.Unmarshal(data, &gs); err != nil {
		return nil, err
	}
	return &gs, nil
}

// Write atomically writes gs to goal-state.json in the project directory.
// It creates .harmonik/intent/ if absent. The write is atomic (temp-file +
// rename) so a reader never sees a partial file.
func Write(projectDir string, gs *GoalState) error {
	gs.SchemaVersion = SchemaVersion

	data, err := json.MarshalIndent(gs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Join(projectDir, ".harmonik", "intent")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	dst := Path(projectDir)
	tmp := dst + ".tmp"

	//nolint:gosec // G306: 0644 is appropriate for a readable intent file
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Default returns a GoalState with schema_version set and empty slices so
// marshalled JSON always contains all top-level fields.
func Default() *GoalState {
	return &GoalState{
		SchemaVersion:      SchemaVersion,
		Objectives:         []string{},
		Antigoals:          []string{},
		OperatorDirectives: []string{},
	}
}
