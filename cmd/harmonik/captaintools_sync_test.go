package main

// captaintools_sync_test.go — sync-guard: embedded captain-tools scripts must
// match their canonical counterparts in scripts/captain-tools/.
//
// If this test fails, re-sync with:
//
//	cp scripts/captain-tools/captain-launch.sh cmd/harmonik/captain-tools/captain-launch.sh
//
// Bead ref: hk-9df (fleet-portability T8).

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCaptainLaunchShEmbedInSync guards the invariant documented in
// init_captaintools_assets.go: the embedded cmd/harmonik/captain-tools/captain-launch.sh
// must be byte-identical to the canonical scripts/captain-tools/captain-launch.sh.
//
// Without this check, an edit to only one copy silently diverges at runtime: the
// binary carries a stale version while the repo shows the correct one (or vice versa).
func TestCaptainLaunchShEmbedInSync(t *testing.T) {
	// Navigate two levels up from cmd/harmonik/ to reach the repo root, then into
	// scripts/captain-tools/.
	canonicalPath := filepath.Join("..", "..", "scripts", "captain-tools", "captain-launch.sh")
	canonicalBytes, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read canonical scripts/captain-tools/captain-launch.sh: %v\n"+
			"  Ensure the file exists at the repo root under scripts/captain-tools/", err)
	}

	if string(captainLaunchSh) != string(canonicalBytes) {
		t.Fatalf("embedded cmd/harmonik/captain-tools/captain-launch.sh is OUT OF SYNC " +
			"with scripts/captain-tools/captain-launch.sh.\n" +
			"Re-sync with:\n" +
			"  cp scripts/captain-tools/captain-launch.sh cmd/harmonik/captain-tools/captain-launch.sh")
	}
}
