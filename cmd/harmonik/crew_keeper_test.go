package main

// crew_keeper_test.go — unit tests for crew keeper watcher integration (hk-yfcc).
//
// Tests cover:
//  - seedSID: session ID file is written to the correct path with the correct
//    content (trim + newline).
//  - spawnCrewKeeper / stopCrewKeeper: keeper session names are correctly derived
//    from the crew name ("hk-keeper-<name>").
//
// spawnCrewKeeper and stopCrewKeeper shell out to tmux (not available in unit
// tests). Their keeper-session-name convention is the load-bearing constraint,
// and is verified here via the name-derivation helper. The keeper --warn-only
// flag acceptance is proven by TestWatcher_WarnOnly_* in the keeper package.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeedSID_WritesFile verifies that seedSID creates the .sid file at the
// expected path with the session ID followed by a newline. Refs: hk-yfcc.
func TestSeedSID_WritesFile(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	name := "alpha"
	sessionID := "12345678-1234-4234-8234-123456789abc"

	seedSID(projectDir, name, sessionID)

	sidPath := filepath.Join(projectDir, ".harmonik", "keeper", name+".sid")
	raw, err := os.ReadFile(sidPath)
	if err != nil {
		t.Fatalf("seedSID: .sid file not created at %q: %v", sidPath, err)
	}
	got := strings.TrimRight(string(raw), "\n")
	if got != sessionID {
		t.Errorf("seedSID: .sid content = %q; want %q", got, sessionID)
	}
}

// TestSeedSID_Idempotent verifies that a second seedSID call overwrites the
// existing file without error (the SessionStart hook overwrites it too).
// Refs: hk-yfcc.
func TestSeedSID_Idempotent(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	name := "beta"
	sid1 := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sid2 := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	seedSID(projectDir, name, sid1)
	seedSID(projectDir, name, sid2) // must not error

	sidPath := filepath.Join(projectDir, ".harmonik", "keeper", name+".sid")
	raw, err := os.ReadFile(sidPath)
	if err != nil {
		t.Fatalf("second seedSID: cannot read .sid: %v", err)
	}
	got := strings.TrimRight(string(raw), "\n")
	if got != sid2 {
		t.Errorf("second seedSID: .sid content = %q; want %q", got, sid2)
	}
}

// TestSeedSID_MkdirAll verifies that seedSID creates the .harmonik/keeper/
// directory tree when it does not yet exist. Refs: hk-yfcc.
func TestSeedSID_MkdirAll(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	name := "gamma"
	sessionID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	// Confirm the keeper dir doesn't exist before the call.
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if _, err := os.Stat(keeperDir); !os.IsNotExist(err) {
		t.Fatalf("expected keeper dir to be absent before seedSID; err=%v", err)
	}

	seedSID(projectDir, name, sessionID) // must create the dir

	if _, err := os.Stat(keeperDir); os.IsNotExist(err) {
		t.Errorf("seedSID did not create keeper dir at %q", keeperDir)
	}
}

// TestCrewKeeperSessionName verifies the naming convention for the keeper tmux
// session ("hk-keeper-<name>") used by spawnCrewKeeper and stopCrewKeeper. The
// convention is the load-bearing constraint: crew stop kills exactly the session
// that crew start created. Refs: hk-yfcc.
func TestCrewKeeperSessionName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		wantSes string
	}{
		{"alpha", "hk-keeper-alpha"},
		{"my-crew", "hk-keeper-my-crew"},
		{"crew-01", "hk-keeper-crew-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Assert against the REAL derivation helper used by both
			// spawnCrewKeeper and stopCrewKeeper. A change to the naming scheme
			// in crew.go must break this test (it is no longer tautological —
			// the expected value is a literal, the actual comes from prod code).
			got := crewKeeperSessionName(tc.name)
			if got != tc.wantSes {
				t.Errorf("keeper session name for %q = %q; want %q", tc.name, got, tc.wantSes)
			}
		})
	}
}
