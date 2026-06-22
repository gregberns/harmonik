package main

// keeper_scripts_embed_sync_test.go — sync-guard: embedded keeper hook scripts
// must be byte-identical to their canonical counterparts in scripts/.
//
// If this test fails, re-sync the out-of-date file with:
//
//	cp scripts/<name>.sh cmd/harmonik/assets/scripts/<name>.sh
//
// Bead ref: hk-ybmqp (portability — keeper hooks not embedded).

import (
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
		canonical, err := os.ReadFile("../../scripts/" + name)
		if err != nil {
			t.Fatalf("read canonical scripts/%s: %v\n"+
				"  Ensure the file exists at scripts/%s in the repo root",
				name, err, name)
		}

		if string(embedded) != string(canonical) {
			t.Errorf("embedded assets/scripts/%s is OUT OF SYNC with scripts/%s.\n"+
				"Re-sync with:\n  cp scripts/%s cmd/harmonik/assets/scripts/%s",
				name, name, name, name)
		}
	}
}
