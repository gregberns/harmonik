package scenario

// coreloopproof_fixture_drift_test.go — fixture-drift guard for the model
// resolution contract in the core-loop-proof matrix fixtures (GAP-5).
//
// The live matrix runner (scripts/core-loop-*.sh) reads seed-beads.json and
// cells.json to seed beads and assert the model_selected event stream. If someone
// "helpfully" pins a model on the codex seed, or writes a literal model name back
// into a cell, the fixtures start asserting the wrong thing while every Go unit
// test still passes. This is a plain go-test drift guard — no scenario build tag,
// no twin — that fails the moment the fixtures drift from the contract.
//
// WHAT THE FIXTURES MEAN, which is what this guard checks. A model name is
// written down in exactly ONE place per seed: a model:<alias> label. A seed
// without one takes harnesses.<harness>.model from the scratch config (pi), or
// leaves the model uncontrolled entirely (codex, set by $CODEX_HOME/config.toml).
// cells.json names no model at all: it carries the sentinels "@resolved" and
// "@foreign", and the runner substitutes the resolved values before it folds the
// assertion.
//
// The guard checks the fixtures against that design, not against the literals the
// fixtures stopped carrying. Asserting the literals is what this file used to do,
// and it is the failure a08fa9de3 removed: three copies of one model name went
// stale together and the gate asserted a model the server would 404.
//
// BOTH halves of the resolution are in this repository, so both are in scope. The
// label half is seed-beads.json. The config half is
// scripts/scratch-config-overlay.yaml — scripts/scratch-daemon.sh appends that
// block onto the scratch config, and scripts/core-loop-matrix.sh resolves a seed's
// model against it. The config half degrades SILENTLY, which is why it needs a
// guard: the runner's own comment says a seed with neither a label nor a config
// value "resolves to empty, and the model check is skipped for that cell". Delete
// or rename harnesses.pi.model in the overlay and seed_model_for returns empty, the
// pi cells' model expectation becomes null, AND foreign_models_for drops the pi
// model from every other cell's leak set. Every leak guard weakens and nothing goes
// red. Check (1c) is the guard for that.
//
// Every check below is on a file this test can read, and every check is a positive
// statement that can fail.
//
// Bead refs: hk-d170r (codex empty→account-default), hk-heh3t (retired guard),
// hk-hyxkj (this rewrite).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

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
	// ModelPin is json.RawMessage, not *string, because the check on it is "this
	// key must be ABSENT". RawMessage keeps that distinction: an absent key leaves
	// it nil, a null one leaves it the four bytes "null". A *string flattens both
	// to nil, and that flattening is exactly how the old assertion went vacuous.
	ModelPin json.RawMessage `json:"model_pin"`
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
			// Model is a pointer so a JSON null is distinguishable from a string.
			// No cell carries either any more — every cell carries the "@resolved"
			// sentinel, and the check below treats a null as a failure. The pointer
			// is what lets that check report a null as itself.
			Model        *string  `json:"model"`
			NoLeakModels []string `json:"no_leak_models"`
		} `json:"model_selected"`
		// Dispatch is the gap4 run_started expectation. Every field is a pointer
		// so "the cell said nothing" is distinguishable from "the cell said the
		// empty string".
		Dispatch *struct {
			WorkflowMode            *string `json:"workflow_mode"`
			WorkflowID              *string `json:"workflow_id"`
			WorkflowIDPresent       *bool   `json:"workflow_id_present"`
			ReviewPolicy            *string `json:"review_policy"`
			WorkflowSelectionSource *string `json:"workflow_selection_source"`
			Nodes                   *struct {
				Required  []string `json:"required"`
				Forbidden []string `json:"forbidden"`
			} `json:"nodes"`
		} `json:"dispatch"`
	} `json:"expect"`
}

// coreLoopProofFixtureDir returns the absolute scenarios/core-loop-proof dir.
func coreLoopProofFixtureDir(t *testing.T) string {
	t.Helper()
	root := conformanceCorpusFixtureRepoRoot(t)
	return filepath.Join(root, "scenarios", "core-loop-proof")
}

// coreLoopProofSeedModelControl is the per-harness-family rule for whether a seed
// may name its own model. It is the whole of harmonik's control over the model a
// seed's run selects, so the table IS the contract.
//
// The claude row is what keeps the other two honest. Two "must not carry a label"
// rules on their own pass just as happily when label parsing is broken as when the
// fixtures are correct, which is exactly the vacuous check hk-hyxkj was filed for.
// One family that MUST carry a label fails loudly in that case.
//
// modelFromConfig splits the two label-less families, which are label-less for
// opposite reasons. pi HAS a model and it is written in
// scripts/scratch-config-overlay.yaml, so an empty resolution there is a defect and
// check (1c) forbids it. codex has NO harmonik-controlled model at all — the
// operator's $CODEX_HOME/config.toml sets it — so codex resolving to empty is the
// designed behaviour and codex must be excluded from that check.
var coreLoopProofSeedModelControl = map[string]struct {
	wantModelLabel  bool
	modelFromConfig bool
	why             string
}{
	"codex":       {false, false, "the codex model is not harmonik-controlled — it comes from $CODEX_HOME/config.toml"},
	"pi":          {false, true, "the pi model is resolved from harnesses.pi.model in the scratch config, which the runner reads"},
	"claude-code": {true, false, "the claude model IS pinned per bead, and the pin is what the leak guards forbid on other harnesses"},
}

// coreLoopProof sentinels. cells.json carries these instead of model names;
// scripts/core-loop-matrix.sh substitutes the resolved values before it folds a
// cell's assertion.
const (
	coreLoopProofResolvedSentinel = "@resolved"
	coreLoopProofForeignSentinel  = "@foreign"
)

// TestCoreLoopProofFixtureDrift_ModelResolutionContract asserts the model
// resolution contract encoded in the core-loop-proof matrix fixtures (GAP-5).
//
// It replaces a guard that asserted the pre-a08fa9de3 fixture design — literal
// model names in every cell and a model_pin field on every seed. That design is
// gone, so the old guard could only fail, and one of its assertions had gone
// vacuous: "the codex seed's model_pin is null" passed because the field was
// absent from every seed, not because the codex seed declared null (hk-hyxkj).
func TestCoreLoopProofFixtureDrift_ModelResolutionContract(t *testing.T) {
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
	if len(seedFile.Seeds) == 0 {
		t.Fatal("seed-beads.json declares no seeds; every check below would be vacuous")
	}

	// (1) Each seed names its own model, or does not, according to its harness
	// family. A model: label is the ONLY place a seed may write a model name.
	seenFamily := map[string]bool{}
	for _, seed := range seedFile.Seeds {
		rule, known := coreLoopProofSeedModelControl[seed.Harness]
		if !known {
			t.Errorf("seed %q declares harness %q, which coreLoopProofSeedModelControl does not cover; add the family's rule rather than leaving its model control unchecked",
				seed.Key, seed.Harness)
			continue
		}
		seenFamily[seed.Harness] = true

		gotLabel := coreLoopProofModelLabel(seed.Labels)
		switch {
		case rule.wantModelLabel && gotLabel == "":
			t.Errorf("seed %q (harness %q) carries no model: label; want one — %s", seed.Key, seed.Harness, rule.why)
		case !rule.wantModelLabel && gotLabel != "":
			t.Errorf("seed %q (harness %q) carries a model: label %q; want none — %s", seed.Key, seed.Harness, gotLabel, rule.why)
		}
	}

	// (1b) Every family the table covers must appear. A family that silently
	// stops being seeded takes its rule out of force without deleting it.
	for family := range coreLoopProofSeedModelControl {
		if !seenFamily[family] {
			t.Errorf("no seed declares harness %q; its model-control rule is no longer in force", family)
		}
	}

	// (1c) A family whose rule says the model comes from config must actually find
	// one there. This is the other half of the resolution, and it fails silently:
	// scripts/core-loop-matrix.sh seed_model_for falls back to
	// harnesses.<harness>.model, and when that is missing it returns empty, the
	// cell's model expectation is rewritten to null, and foreign_models_for drops
	// the value from every OTHER cell's leak set. Nothing goes red. That is the same
	// defect shape as hk-hyxkj, one file over, and it is checkable from the repo.
	// The pair of conditions is the message's own premise: config is the model's
	// only source exactly when the family declares one there AND check (1) has
	// already forbidden its seeds a label.
	overlayModels := coreLoopProofScratchOverlayModels(t)
	for family, rule := range coreLoopProofSeedModelControl {
		if rule.wantModelLabel || !rule.modelFromConfig {
			continue
		}
		if overlayModels[family] == "" {
			t.Errorf("scripts/scratch-config-overlay.yaml declares no non-empty harnesses.%s.model, and no seed of that family carries a model: label; seed_model_for %s then resolves to empty, the %s cells assert a null model, and the %s model drops out of every other cell's no_leak_models — %s",
				family, family, family, family, rule.why)
		}
	}

	// (2) model_pin must stay deleted. a08fa9de3 removed it as a field with no
	// reader — a second written-down copy of a fact the label already carries.
	// coreLoopProofSeed.ModelPin is json.RawMessage so an absent key (nil) stays
	// distinct from a declared null ("null"). A *string flattens the two, and that
	// flattening is precisely how the old assertion went vacuous.
	for _, seed := range seedFile.Seeds {
		if seed.ModelPin != nil {
			t.Errorf("seed %q carries a model_pin field; it was deleted because nothing reads it, and a second copy of the model fact is what went stale before (a08fa9de3)",
				seed.Key)
		}
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
	if len(cellsFile.Cells) == 0 {
		t.Fatal("cells.json declares no cells; every check below would be vacuous")
	}

	// (3) No cell names a model. Both expectations carry their sentinel, and the
	// runner substitutes the resolved value. A literal here bypasses resolution
	// and is the exact rot a08fa9de3 removed.
	for _, cell := range cellsFile.Cells {
		got := cell.Expect.ModelSelected.Model
		if got == nil {
			t.Errorf("cell %q expect.model_selected.model is null; want the sentinel %q — the runner resolves a null model for an unpinned harness, and a fixture that hard-codes the answer stops asking",
				cell.Cell, coreLoopProofResolvedSentinel)
		} else if *got != coreLoopProofResolvedSentinel {
			t.Errorf("cell %q expect.model_selected.model = %q; want the sentinel %q — cells.json names no model",
				cell.Cell, *got, coreLoopProofResolvedSentinel)
		}

		leak := cell.Expect.ModelSelected.NoLeakModels
		if len(leak) != 1 || leak[0] != coreLoopProofForeignSentinel {
			t.Errorf("cell %q expect.model_selected.no_leak_models = %v; want exactly [%q] — the runner derives the forbidden set from the other seeds, so a written-down list can only go stale",
				cell.Cell, leak, coreLoopProofForeignSentinel)
		}
	}

	// (4) The sentinels bind to their only reader. If the runner's spelling and
	// the fixture's spelling part company, the runner folds the assertion against
	// the literal string "@resolved" and every cell goes red — but only on a live
	// matrix run, which is expensive and rare. Catch it here instead.
	//
	// This is a SPELLING check on the runner, not a behaviour check. It confirms the
	// sentinel strings still appear in the script. It does not confirm the runner
	// still substitutes a resolved value for them.
	runner := coreLoopProofMatrixRunnerSource(t)
	for _, sentinel := range []string{coreLoopProofResolvedSentinel, coreLoopProofForeignSentinel} {
		if !strings.Contains(runner, sentinel) {
			t.Errorf("scripts/core-loop-matrix.sh does not mention the sentinel %q; the fixtures carry a placeholder nothing substitutes", sentinel)
		}
	}

	// (5) The cross-harness leak guard cannot be vacuous. For an unpinned cell the
	// runner's forbidden set is every OTHER seed's resolved model, and a seed
	// whose model comes from the scratch config resolves to a value this test
	// cannot see. At least one seed from another family must carry a model:
	// label, so the forbidden set is provably non-empty from the repo alone.
	//
	// While the table above names one family that MUST carry a label, checks (1)
	// and (1b) together already force that seed to exist, so a fixture edit alone
	// cannot make this check the only failure. Nor can a table edit alone: flip the
	// last wantModelLabel row to false and check (1) fails alongside this one,
	// because the seed still carries the label the table now forbids.
	//
	// This check is the SOLE failure only under the two-part edit — flip the last
	// wantModelLabel row to false AND delete that seed's model: label. That pair is
	// internally consistent, so every other check stays green, and yet no seed pins
	// a model any more and the leak guard has nothing left to forbid. This check is
	// the one that says so.
	for family, rule := range coreLoopProofSeedModelControl {
		if rule.wantModelLabel {
			continue
		}
		if !coreLoopProofHasPinnedSeedOutside(seedFile.Seeds, family) {
			t.Errorf("no seed outside harness family %q carries a model: label; the forbidden-model set for its cells could be empty, and the leak guard would assert nothing", family)
		}
	}
}

// coreLoopProofModelLabel returns the alias of the seed's model: label, or "" when
// it carries none.
func coreLoopProofModelLabel(labels []string) string {
	const prefix = "model:"
	for _, lbl := range labels {
		if strings.HasPrefix(lbl, prefix) {
			return strings.TrimPrefix(lbl, prefix)
		}
	}
	return ""
}

// coreLoopProofHasPinnedSeedOutside reports whether some seed of a family other
// than the given one carries a model: label.
func coreLoopProofHasPinnedSeedOutside(seeds []coreLoopProofSeed, family string) bool {
	for _, seed := range seeds {
		if seed.Harness == family {
			continue
		}
		if coreLoopProofModelLabel(seed.Labels) != "" {
			return true
		}
	}
	return false
}

// coreLoopProofScratchOverlayModels returns harnesses.<family>.model for every
// harness family declared in scripts/scratch-config-overlay.yaml.
//
// That overlay is the config half of the model resolution. scripts/scratch-daemon.sh
// appends it onto the scratch .harmonik/config.yaml, and the runner's
// harness_cfg_value reads harnesses.<harness>.model back out of the result. The key
// this map is looked up by is therefore the harness family name itself, which is how
// the runner spells it too.
func coreLoopProofScratchOverlayModels(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join(conformanceCorpusFixtureRepoRoot(t), "scripts", "scratch-config-overlay.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from the in-repo scripts dir, not user input
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var overlay struct {
		Harnesses map[string]struct {
			Model string `yaml:"model"`
		} `yaml:"harnesses"`
	}
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	models := make(map[string]string, len(overlay.Harnesses))
	for family, h := range overlay.Harnesses {
		models[family] = h.Model
	}
	return models
}

// coreLoopProofMatrixRunnerSource returns the text of the matrix runner — the one
// program that reads the cells.json sentinels.
func coreLoopProofMatrixRunnerSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(conformanceCorpusFixtureRepoRoot(t), "scripts", "core-loop-matrix.sh")
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from the in-repo scripts dir, not user input
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
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
// The guard checks five things about every cell that declares expect.dispatch:
//
//  1. workflow_mode is "dot".
//  2. workflow_id_present is true and workflow_id names a graph. A record whose
//     descriptor is empty cannot be emitted, so a cell expecting the field
//     absent asserts less than the daemon guarantees.
//  3. review_policy and workflow_selection_source are both declared and both
//     legal values.
//  4. The (policy, source, workflow_id) tuple is one core.RunStartedPayload
//     accepts — checked in BOTH directions. See
//     checkCoreLoopProofPolicyBinding.
//  5. The cell declares a dispatched node set, and a no_review cell forbids the
//     reviewer node ids.
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

		// workflow_id is always emitted, so a cell that expects it absent asserts
		// less than the daemon guarantees. core.RunStartedPayload.Valid rejects a
		// record whose descriptor is invalid, and the descriptor reaching
		// emitRunStarted came from a resolvedWorkflow that was already checked the
		// same way (internal/daemon/workloop_runplan.go resolveWorkflow). An empty
		// workflow_id therefore cannot appear on the wire.
		if d.WorkflowIDPresent == nil {
			t.Errorf("cell %q expect.dispatch has no workflow_id_present; gap4 cannot assert the descriptor resolved", cell.Cell)
		} else if !*d.WorkflowIDPresent {
			t.Errorf("cell %q expects workflow_id_present = false; want true — core.RunStartedPayload.Valid rejects an invalid descriptor, so every emitted run_started carries a workflow_id (specs/workflow-graph.md WG-055)", cell.Cell)
		}
		if d.WorkflowID == nil || *d.WorkflowID == "" {
			t.Errorf("cell %q expect.dispatch has no workflow_id; the selection source fixes which graph runs, so the cell can and must name it", cell.Cell)
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
		workflowID, workflowMode := "", ""
		if d.WorkflowID != nil {
			workflowID = *d.WorkflowID
		}
		if d.WorkflowMode != nil {
			workflowMode = *d.WorkflowMode
		}
		checkCoreLoopProofPolicyBinding(t, cell.Cell, workflowMode, workflowID, *d.ReviewPolicy, *d.WorkflowSelectionSource)

		// The dispatched node set is the second, independent signal that the run
		// took the path the policy claims. It is a containment check: required
		// names what the run must dispatch, forbidden what it must never dispatch.
		var required, forbidden []string
		if d.Nodes != nil {
			required, forbidden = d.Nodes.Required, d.Nodes.Forbidden
		}
		if len(required) == 0 {
			t.Errorf("cell %q expect.dispatch declares no nodes.required; the DOT cascade dispatches the graph's start_node on its first iteration, so every cell can name at least that (event-model.md §8.1.11)", cell.Cell)
		}
		if *d.ReviewPolicy == "no_review" {
			for _, reviewerNode := range []string{"review", "reviewer"} {
				if !slices.Contains(forbidden, reviewerNode) {
					t.Errorf("cell %q is no_review but does not forbid node id %q; the no-review-bead graph declares no reviewer node, so a dispatched reviewer is the observable form of the policy being wrong",
						cell.Cell, reviewerNode)
				}
			}
		}
	}
	if !sawDispatch {
		t.Error("no cell in cells.json declares expect.dispatch; the gap4 drift guard would be vacuous")
	}
}

// checkCoreLoopProofPolicyBinding fails when a cell's (workflow_mode,
// workflow_id, review_policy, workflow_selection_source) tuple is one the daemon
// could never emit.
//
// It does NOT restate the rule. It builds the run_started payload the cell
// describes and asks core.RunStartedPayload.Valid — the same method the daemon's
// own resolver output must satisfy — whether that record is legal. A restated
// copy of the rule is what went wrong the first time: the earlier guard listed
// the two sources that may select no_review and checked only that direction, so
// flipping a legacy_single_label cell from no_review to reviewed stayed green
// even though core.ValidPolicyBinding rejects the pair. Calling the real
// validator cannot drift from it.
//
// TWO VALUES THE FIXTURE DOES NOT SUPPLY are filled in here, and neither weakens
// the check:
//
//   - workflow_version. Valid compares the whole descriptor, ID and version. All
//     three graphs the matrix reaches declare version "1.0" (workflow.dot,
//     specs/examples/review-loop.dot, specs/examples/no-review-bead.dot), so the
//     version never varies across the matrix and a second constant in cells.json
//     would be a copy of a fact that lives in the graph file.
//   - run_id, workspace_path, input_ref, started_at. Valid requires them
//     non-zero. They carry no policy meaning, so any legal value does.
func checkCoreLoopProofPolicyBinding(t *testing.T, cellName, mode, workflowID, policy, source string) {
	t.Helper()

	const graphVersion = "1.0"
	payload := core.RunStartedPayload{
		RunID:                   core.RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:              core.WorkflowID(workflowID),
		WorkflowVersion:         core.WorkflowVersion(graphVersion),
		WorkflowMode:            core.WorkflowMode(mode),
		ReviewPolicy:            core.ReviewPolicy(policy),
		WorkflowSelectionSource: core.WorkflowSelectionSource(source),
		WorkspacePath:           "/w",
		InputRef:                "bead:seed",
		StartedAt:               time.Unix(0, 0).UTC(),
	}
	if payload.Valid() {
		return
	}
	t.Errorf("cell %q describes a run_started record core.RunStartedPayload.Valid rejects: workflow_mode=%q workflow_id=%q (version %q) review_policy=%q workflow_selection_source=%q — the daemon can never emit it, so gap4 could never go green (execution-model.md §4.3 EM-012a)",
		cellName, mode, workflowID, graphVersion, policy, source)
}
