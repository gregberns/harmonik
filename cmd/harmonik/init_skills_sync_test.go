package main

// init_skills_sync_test.go — sync-guard: embedded fleet skill files must be
// byte-identical to their canonical counterparts in .claude/skills/.
//
// If this test fails, re-sync the out-of-date file with:
//
//	cp .claude/skills/<skill>/<file> cmd/harmonik/assets/skills/<skill>/<file>
//
// Bead ref: hk-7iyh (fleet-portability T11).

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSkillAssetsEmbedInSync guards the invariant documented in
// init_skill_assets.go: each embedded asset at
// cmd/harmonik/assets/skills/<skill>/<file> must be byte-identical to the
// canonical .claude/skills/<skill>/<file>.
//
// Without this check, an edit to only one copy silently diverges at runtime:
// `harmonik init` provisions the stale binary-embedded version while the repo
// shows the correct one (or vice versa).
func TestSkillAssetsEmbedInSync(t *testing.T) {
	skillEntries, err := initSkillAssets.ReadDir("assets/skills")
	if err != nil {
		t.Fatalf("read embedded assets/skills: %v", err)
	}

	// Track which skills the guard actually walked, so we can assert that
	// load-bearing additions (e.g. the orchestrator standing-rules contract)
	// are embedded and therefore covered — not silently absent.
	seen := map[string]bool{}

	for _, skillEntry := range skillEntries {
		if !skillEntry.IsDir() {
			continue
		}
		skill := skillEntry.Name()
		seen[skill] = true

		fileEntries, err := initSkillAssets.ReadDir("assets/skills/" + skill)
		if err != nil {
			t.Fatalf("read embedded assets/skills/%s: %v", skill, err)
		}

		for _, fileEntry := range fileEntries {
			if fileEntry.IsDir() {
				continue
			}
			fname := fileEntry.Name()

			embedded, err := initSkillAssets.ReadFile("assets/skills/" + skill + "/" + fname)
			if err != nil {
				t.Fatalf("read embedded assets/skills/%s/%s: %v", skill, fname, err)
			}

			// Navigate two levels up from cmd/harmonik/ to reach the repo root.
			canonicalPath := filepath.Join("..", "..", ".claude", "skills", skill, fname)
			canonical, err := os.ReadFile(canonicalPath)
			if err != nil {
				t.Fatalf("read canonical .claude/skills/%s/%s: %v\n"+
					"  Ensure the file exists at the repo root under .claude/skills/%s/",
					skill, fname, err, skill)
			}

			if string(embedded) != string(canonical) {
				t.Errorf("embedded assets/skills/%s/%s is OUT OF SYNC with .claude/skills/%s/%s.\n"+
					"Re-sync with:\n  cp .claude/skills/%s/%s cmd/harmonik/assets/skills/%s/%s",
					skill, fname, skill, fname,
					skill, fname, skill, fname)
			}
		}
	}

	// The orchestrator skill is the universal standing-rules contract; it must
	// ship with the binary (and thus be guarded above). If it is missing from
	// the embedded bundle, the sync-guard would silently never check it.
	if !seen["orchestrator-rules"] {
		t.Errorf("orchestrator skill is NOT embedded under assets/skills/ — " +
			"the standing-rules contract must ship with the binary.\n" +
			"Mirror it with:\n  cp .claude/skills/orchestrator/SKILL.md cmd/harmonik/assets/skills/orchestrator/SKILL.md")
	}
}
