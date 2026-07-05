package dashboard_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
)

func TestRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// First read on an absent file returns ErrNotFound.
	_, err := dashboard.Read(dir)
	if !errors.Is(err, dashboard.ErrNotFound) {
		t.Fatalf("want ErrNotFound; got %v", err)
	}

	updated := time.Date(2026, 7, 3, 17, 0, 0, 0, time.UTC)
	by := time.Date(2026, 7, 3, 20, 0, 0, 0, time.UTC)

	ds := &dashboard.DashboardState{
		Updated:   updated,
		UpdatedBy: "captain",
		PrioritiesCurrent: []dashboard.PriorityCurrent{
			{
				Rank: 1, Lane: "pi-sandbox", Crew: "leto",
				Headline: "Pi-in-a-sandbox via srt", Expected: "acceptance test green by EOD",
			},
		},
		PrioritiesFuture: []dashboard.PriorityFuture{
			{
				Lane: "stall-sentinel", Headline: "deterministic stall detector",
				Gate: "after sandbox completes",
			},
		},
		ThroughputExpected: []dashboard.ThroughputExpected{
			{Lane: "pi-sandbox", BeadsExpected: 4, By: by},
		},
		Notes: "all good",
	}

	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify file exists at the expected path.
	want := filepath.Join(dir, ".harmonik", "context", "dashboard.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("dashboard.json not created at %s: %v", want, err)
	}

	got, err := dashboard.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.SchemaVersion != dashboard.SchemaVersion {
		t.Errorf("SchemaVersion: got %d; want %d", got.SchemaVersion, dashboard.SchemaVersion)
	}
	if !got.Updated.Equal(updated) {
		t.Errorf("Updated: got %v; want %v", got.Updated, updated)
	}
	if got.UpdatedBy != "captain" {
		t.Errorf("UpdatedBy: got %q; want %q", got.UpdatedBy, "captain")
	}
	if len(got.PrioritiesCurrent) != 1 || got.PrioritiesCurrent[0].Lane != "pi-sandbox" {
		t.Errorf("PrioritiesCurrent mismatch: %v", got.PrioritiesCurrent)
	}
	if len(got.PrioritiesFuture) != 1 || got.PrioritiesFuture[0].Lane != "stall-sentinel" {
		t.Errorf("PrioritiesFuture mismatch: %v", got.PrioritiesFuture)
	}
	if len(got.ThroughputExpected) != 1 || got.ThroughputExpected[0].BeadsExpected != 4 {
		t.Errorf("ThroughputExpected mismatch: %v", got.ThroughputExpected)
	}
	if got.Notes != "all good" {
		t.Errorf("Notes: got %q; want %q", got.Notes, "all good")
	}
}

func TestDefault(t *testing.T) {
	ds := dashboard.Default()
	if ds.SchemaVersion != dashboard.SchemaVersion {
		t.Errorf("Default SchemaVersion: got %d; want %d", ds.SchemaVersion, dashboard.SchemaVersion)
	}
	if ds.PrioritiesCurrent == nil || ds.PrioritiesFuture == nil || ds.ThroughputExpected == nil {
		t.Error("Default must initialise all slices (not nil) to avoid null in JSON")
	}
}

func TestPath(t *testing.T) {
	p := dashboard.Path("/foo/bar")
	want := "/foo/bar/.harmonik/context/dashboard.json"
	if p != want {
		t.Errorf("Path: got %q; want %q", p, want)
	}
}
