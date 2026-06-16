package goalstate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/goalstate"
)

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// First read on an absent file returns ErrNotFound.
	_, err := goalstate.Read(dir)
	if !errors.Is(err, goalstate.ErrNotFound) {
		t.Fatalf("want ErrNotFound; got %v", err)
	}

	// Write a populated GoalState.
	gs := &goalstate.GoalState{
		Objectives:         []string{"ship flywheel V6"},
		Antigoals:          []string{"break the build"},
		OperatorDirectives: []string{"focus on hk-owz1"},
		LastEventID:        "01924c00-0000-7fff-8000-000000000001",
	}
	if err := goalstate.Write(dir, gs); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify the file exists at the expected path.
	want := filepath.Join(dir, ".harmonik", "intent", "goal-state.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("goal-state.json not created at %s: %v", want, err)
	}

	// Round-trip: Read back and verify fields.
	got, err := goalstate.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.SchemaVersion != goalstate.SchemaVersion {
		t.Errorf("SchemaVersion: got %d; want %d", got.SchemaVersion, goalstate.SchemaVersion)
	}
	if len(got.Objectives) != 1 || got.Objectives[0] != "ship flywheel V6" {
		t.Errorf("Objectives mismatch: %v", got.Objectives)
	}
	if len(got.Antigoals) != 1 || got.Antigoals[0] != "break the build" {
		t.Errorf("Antigoals mismatch: %v", got.Antigoals)
	}
	if len(got.OperatorDirectives) != 1 || got.OperatorDirectives[0] != "focus on hk-owz1" {
		t.Errorf("OperatorDirectives mismatch: %v", got.OperatorDirectives)
	}
	if got.LastEventID != gs.LastEventID {
		t.Errorf("LastEventID: got %q; want %q", got.LastEventID, gs.LastEventID)
	}
}

func TestDefault(t *testing.T) {
	gs := goalstate.Default()
	if gs.SchemaVersion != goalstate.SchemaVersion {
		t.Errorf("Default SchemaVersion: got %d; want %d", gs.SchemaVersion, goalstate.SchemaVersion)
	}
	if gs.Objectives == nil || gs.Antigoals == nil || gs.OperatorDirectives == nil {
		t.Error("Default must initialise all slices (not nil) to avoid null in JSON")
	}
}

func TestPath(t *testing.T) {
	p := goalstate.Path("/foo/bar")
	want := "/foo/bar/.harmonik/intent/goal-state.json"
	if p != want {
		t.Errorf("Path: got %q; want %q", p, want)
	}
}
