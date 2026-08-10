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

	"github.com/gregberns/harmonik/internal/core"
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
		// Dispatch is the gap4 run_started expectation. Every field is a pointer
		// so "the cell said nothing" is distinguishable from "the cell said the
		// empty string".
		Dispatch *struct {
			WorkflowMode            *string `json:"workflow_mode"`
			ReviewPolicy            *string `json:"review_policy"`
			WorkflowSelectionSource *string `json:"workflow_selection_source"`
		} `json:"dispatch"`
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

// TestCoreLoopProofFixtureDrift_RunStartedDispatchContract asserts that the gap4
// dispatch expectations in cells.json still describe the run_started record the
// daemon is specified to emit.
//
// A version-2 run_started record carries workflow_mode = "dot" for EVERY run.
// The mode names the execution engine, and there is only one engine: the daemon
// resolves a DOT graph before it emits the event. A legacy workflow:single bead
// label and a tier-0 queue item with workflow_mode = single are graph SELECTION
// requests, not execution modes — both select the registered embedded
// no-review-bead graph and record their provenance in workflow_selection_source.
// Sources, in order of precedence and all normative:
//
//   - specs/execution-model.md §4.3 EM-012a tier 0 and tier 1 ("Both resolve to
//     execution mode dot") and its closing clause ("The event payload MUST
//     surface the descriptor, workflow_mode = dot, review policy, and selection
//     source").
//   - specs/execution-model.md §4.4 run_started emission ("The version-2 payload
//     MUST carry ... workflow_mode = dot").
//   - specs/event-model.md §8.1 workflow_mode payload-field rule ("run_started
//     schema version 2 MUST carry workflow_mode = \"dot\"").
//
// core.RunStartedPayload.Valid rejects any other mode, so a cell expecting
// "single" asserts against a record the daemon cannot emit and can never go
// green. That is what this guard catches, and it is why the single-versus-
// reviewed distinction now rides on review_policy and workflow_selection_source
// instead: those two fields carry the fact the mode field used to carry.
//
// Bead refs: hk-oeqn9, hk-gap4-workflow-mode-drift-7xwat.
func TestCoreLoopProofFixtureDrift_RunStartedDispatchContract(t *testing.T) {
	t.Parallel()

	cellsData, err := os.ReadFile(filepath.Join(coreLoopProofFixtureDir(t), "cells.json"))
	if err != nil {
		t.Fatalf("read cells.json: %v", err)
	}
	var cellsFile coreLoopProofCellsFile
	if err := json.Unmarshal(cellsData, &cellsFile); err != nil {
		t.Fatalf("unmarshal cells.json: %v", err)
	}

	sawDispatch := false
	for _, cell := range cellsFile.Cells {
		d := cell.Expect.Dispatch
		if d == nil {
			continue
		}
		sawDispatch = true

		if d.WorkflowMode == nil {
			t.Errorf("cell %q expect.dispatch has no workflow_mode; gap4 cannot assert it", cell.Cell)
		} else if *d.WorkflowMode != "dot" {
			t.Errorf("cell %q expects run_started.workflow_mode = %q; want \"dot\" — a version-2 run_started record carries \"dot\" for every run (execution-model.md §4.3 EM-012a, event-model.md §8.1)",
				cell.Cell, *d.WorkflowMode)
		}

		if d.ReviewPolicy == nil {
			t.Errorf("cell %q expect.dispatch has no review_policy; with workflow_mode a constant, review_policy is what still tells single-mode work apart from reviewed work", cell.Cell)
			continue
		}
		if *d.ReviewPolicy != "reviewed" && *d.ReviewPolicy != "no_review" {
			t.Errorf("cell %q expects review_policy = %q; want \"reviewed\" or \"no_review\"", cell.Cell, *d.ReviewPolicy)
		}
		if d.WorkflowSelectionSource == nil {
			t.Errorf("cell %q expect.dispatch has no workflow_selection_source; it is the field that names WHY the graph was chosen", cell.Cell)
			continue
		}
		// The pairing runs both ways, and core owns which sources are the
		// no-review ones. A one-directional check let a cell name a
		// compatibility source and still expect a review, which the resolver
		// never produces: both compatibility inputs land on the no-review graph
		// (internal/daemon resolveNoReviewWorkflow), so the policy follows from
		// the source with nothing left to choose.
		selectsNoReview := core.WorkflowSelectionSource(*d.WorkflowSelectionSource).SelectsNoReview()
		switch {
		case *d.ReviewPolicy == "no_review" && !selectsNoReview:
			t.Errorf("cell %q pairs review_policy=no_review with workflow_selection_source=%q; only legacy_single_label and queue_item_single_mode may select no_review (execution-model.md §4.3 EM-012a)",
				cell.Cell, *d.WorkflowSelectionSource)
		case *d.ReviewPolicy == "reviewed" && selectsNoReview:
			t.Errorf("cell %q pairs review_policy=reviewed with workflow_selection_source=%q; that source selects the no-review graph, so the run carries no_review (execution-model.md §4.3 EM-012a)",
				cell.Cell, *d.WorkflowSelectionSource)
		}
	}
	if !sawDispatch {
		t.Error("no cell in cells.json declares expect.dispatch; the gap4 drift guard would be vacuous")
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
