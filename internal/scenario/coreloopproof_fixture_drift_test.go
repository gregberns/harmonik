package scenario

// coreloopproof_fixture_drift_test.go — fixture-drift guard for the codex
// empty-model contract in the core-loop-proof matrix fixtures (GAP-5).
//
// The live matrix runner (scripts/core-loop-*.sh) reads seed-beads.json and
// cells.json to seed beads and assert the model_selected event stream. Those
// fixtures encode the codex empty-model contract: a codex seed carries NO model:
// label and a null model_pin, and every codex cell expects model_selected.model
// to be null with the known claude/pi model strings in its no_leak_models forbid
// list. If someone "helpfully" pins a model on the codex seed (or drops the leak
// guards), the fixtures would silently start asserting the wrong thing while every
// Go unit test still passed. This is a plain go-test drift guard — no scenario
// build tag, no twin — that fails the moment the fixtures drift from the contract.
//
// Bead refs: hk-d170r (codex empty→account-default), hk-heh3t (retired guard).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coreLoopProofSeedFile is the parsed shape of scenarios/core-loop-proof/seed-beads.json.
// Only the fields this guard inspects are modelled.
type coreLoopProofSeedFile struct {
	Seeds []coreLoopProofSeed `json:"seeds"`
}

type coreLoopProofSeed struct {
	Key     string   `json:"key"`
	Harness string   `json:"harness"`
	Labels  []string `json:"labels"`
	// ModelPin is a pointer so a JSON null is distinguishable from an empty
	// string: null → nil, "ornith" → non-nil.
	ModelPin *string `json:"model_pin"`
}

// coreLoopProofCellsFile is the parsed shape of scenarios/core-loop-proof/cells.json.
type coreLoopProofCellsFile struct {
	Cells []coreLoopProofCell `json:"cells"`
}

type coreLoopProofCell struct {
	Cell    string `json:"cell"`
	Harness string `json:"harness"`
	Expect  struct {
		ModelSelected struct {
			// Model is a pointer so JSON null (codex) is distinguishable from a
			// non-null model string (pi/claude).
			Model        *string  `json:"model"`
			NoLeakModels []string `json:"no_leak_models"`
		} `json:"model_selected"`
	} `json:"expect"`
}

// coreLoopProofFixtureDir returns the absolute scenarios/core-loop-proof dir.
func coreLoopProofFixtureDir(t *testing.T) string {
	t.Helper()
	root := conformanceCorpusFixtureRepoRoot(t)
	return filepath.Join(root, "scenarios", "core-loop-proof")
}

// TestCoreLoopProofFixtureDrift_CodexEmptyModel asserts the codex empty-model
// contract encoded in the core-loop-proof matrix fixtures (GAP-5).
func TestCoreLoopProofFixtureDrift_CodexEmptyModel(t *testing.T) {
	t.Parallel()

	dir := coreLoopProofFixtureDir(t)

	// ── seed-beads.json ──────────────────────────────────────────────────────
	seedData, err := os.ReadFile(filepath.Join(dir, "seed-beads.json")) //nolint:gosec // G304: path from the in-repo fixture dir, not user input
	if err != nil {
		t.Fatalf("read seed-beads.json: %v", err)
	}
	var seedFile coreLoopProofSeedFile
	if err := json.Unmarshal(seedData, &seedFile); err != nil {
		t.Fatalf("unmarshal seed-beads.json: %v", err)
	}

	codexSeed := coreLoopProofFindSeed(t, seedFile.Seeds, "codex")
	// (1) No label carries the "model:" prefix.
	for _, lbl := range codexSeed.Labels {
		if strings.HasPrefix(lbl, "model:") {
			t.Errorf("codex seed carries a model: label %q; codex model must be unpinned (account default)", lbl)
		}
	}
	// (1) model_pin must be JSON null.
	if codexSeed.ModelPin != nil {
		t.Errorf("codex seed model_pin = %q; want null (codex model not harmonik-controlled)", *codexSeed.ModelPin)
	}

	// Optional guard: the pi seed must STILL carry a non-null model_pin — proves
	// the codex-null assertion above is not a false pass from a broken parse.
	piSeed := coreLoopProofFindSeed(t, seedFile.Seeds, "pi")
	if piSeed.ModelPin == nil {
		t.Error("pi seed model_pin = null; want a non-null pin (pi model IS harmonik-controlled)")
	}

	// ── cells.json ───────────────────────────────────────────────────────────
	cellsData, err := os.ReadFile(filepath.Join(dir, "cells.json")) //nolint:gosec // G304: path from the in-repo fixture dir, not user input
	if err != nil {
		t.Fatalf("read cells.json: %v", err)
	}
	var cellsFile coreLoopProofCellsFile
	if err := json.Unmarshal(cellsData, &cellsFile); err != nil {
		t.Fatalf("unmarshal cells.json: %v", err)
	}

	// Derive the pi model from the pi CELL's own expectation rather than hard-coding
	// it: the pi model drifts independently (deepseek-reasoner vs ornith) as the pi
	// harness swaps providers. Asserting the codex cells forbid *whatever the pi cell
	// currently declares* keeps the cross-harness leak guard meaningful AND
	// self-consistent within this one file, so a pi provider swap can never make the
	// codex contract fail spuriously.
	var piCellModel string
	sawPiCell := false
	for _, cell := range cellsFile.Cells {
		if cell.Harness != "pi" {
			continue
		}
		sawPiCell = true
		// pi cells' model_selected.model must be non-null (pi IS harmonik-controlled).
		if cell.Expect.ModelSelected.Model == nil {
			t.Errorf("pi cell %q model_selected.model = null; want a non-null model (pi IS harmonik-controlled)", cell.Cell)
			continue
		}
		piCellModel = *cell.Expect.ModelSelected.Model
	}

	sawCodexCell := false
	for _, cell := range cellsFile.Cells {
		if cell.Harness != "codex" {
			continue
		}
		sawCodexCell = true
		// (2) model_selected.model must be null.
		if cell.Expect.ModelSelected.Model != nil {
			t.Errorf("codex cell %q model_selected.model = %q; want null", cell.Cell, *cell.Expect.ModelSelected.Model)
		}
		// (2) no_leak_models must forbid the claude leak (stable string)...
		if !coreLoopProofContains(cell.Expect.ModelSelected.NoLeakModels, "claude-opus-4-8") {
			t.Errorf("codex cell %q no_leak_models missing %q; got %v",
				cell.Cell, "claude-opus-4-8", cell.Expect.ModelSelected.NoLeakModels)
		}
		// ...and the pi leak, derived from the pi cell above (never hard-coded).
		if piCellModel != "" && !coreLoopProofContains(cell.Expect.ModelSelected.NoLeakModels, piCellModel) {
			t.Errorf("codex cell %q no_leak_models missing the pi model %q declared by the pi cell; got %v",
				cell.Cell, piCellModel, cell.Expect.ModelSelected.NoLeakModels)
		}
	}
	if !sawCodexCell {
		t.Error("cells.json contains no codex cell; fixture-drift guard would be vacuous")
	}
	if !sawPiCell {
		t.Error("cells.json contains no pi cell; the pi leak cross-check would be vacuous")
	}
}

// coreLoopProofFindSeed returns the seed with the given key or fails the test.
func coreLoopProofFindSeed(t *testing.T, seeds []coreLoopProofSeed, key string) coreLoopProofSeed {
	t.Helper()
	for _, s := range seeds {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("seed key %q not found in seed-beads.json", key)
	return coreLoopProofSeed{}
}

// coreLoopProofContains reports whether want is in list.
func coreLoopProofContains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
