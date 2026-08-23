package main

import (
	"bytes"
	"os"
	"testing"
)

// TestKeeperScriptsEmbedInSync guards the invariant that each embedded keeper
// hook script at cmd/harmonik/assets/scripts/<name>.sh is byte-identical to the
// canonical scripts/<name>.sh at the repo root.
//
// Without this check, editing scripts/keeper-*.sh without re-copying to the
// embedded dir silently ships a stale binary that wires the old hook version on
// go-install deployments.
func TestKeeperScriptsEmbedInSync(t *testing.T) {
	for _, name := range keeperScriptNames {
		embedded, err := initSkillAssets.ReadFile("assets/scripts/" + name)
		if err != nil {
			t.Fatalf("read embedded assets/scripts/%s: %v\n"+
				"  Ensure the file was copied with:\n"+
				"  cp scripts/%s cmd/harmonik/assets/scripts/%s",
				name, err, name, name)
		}

		// Navigate two levels up from cmd/harmonik/ to reach the repo root.
		//nolint:gosec // G304: name comes from the fixed keeperScriptNames test fixture.
		canonical, err := os.ReadFile("../../scripts/" + name)
		if err != nil {
			t.Fatalf("read canonical scripts/%s: %v\n"+
				"  Ensure the file exists at scripts/%s in the repo root",
				name, err, name)
		}

		if !bytes.Equal(embedded, canonical) {
			t.Errorf("embedded assets/scripts/%s is OUT OF SYNC with scripts/%s.\n"+
				"Re-sync with:\n  cp scripts/%s cmd/harmonik/assets/scripts/%s",
				name, name, name, name)
		}
	}
}
