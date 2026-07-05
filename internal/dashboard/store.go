package dashboard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ErrNotFound is returned by Read when dashboard.json does not exist yet.
var ErrNotFound = errors.New("dashboard.json not found")

// Read reads and unmarshals dashboard.json from the project directory.
// Returns ErrNotFound when the file is absent (first-run / never-written case).
func Read(projectDir string) (*DashboardState, error) {
	data, err := os.ReadFile(Path(projectDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var ds DashboardState
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}

// Write atomically writes ds to dashboard.json in the project directory.
// It creates .harmonik/context/ if absent. The write is atomic (temp-file +
// rename) so a reader never sees a partial file.
func Write(projectDir string, ds *DashboardState) error {
	ds.SchemaVersion = SchemaVersion

	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Join(projectDir, ".harmonik", "context")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	dst := Path(projectDir)
	tmp := dst + ".tmp"

	//nolint:gosec // G306: 0644 is appropriate for a readable context file
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Default returns a DashboardState with schema_version set and empty slices so
// marshalled JSON always contains all top-level fields.
func Default() *DashboardState {
	return &DashboardState{
		SchemaVersion:      SchemaVersion,
		UpdatedBy:          "",
		PrioritiesCurrent:  []PriorityCurrent{},
		PrioritiesFuture:   []PriorityFuture{},
		ThroughputExpected: []ThroughputExpected{},
		Notes:              "",
	}
}
