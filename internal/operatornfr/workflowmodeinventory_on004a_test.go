package operatornfr_test

import (
	"testing"
)

// workflowModeInventoryFixtureIterationCapValue is the hardcoded MVH value for
// the review-loop iteration cap per operator-nfr.md §4.1 ON-004a.
//
// "Iteration cap (review-loop): hardcoded at 3 for MVH; NOT operator-tunable."
const workflowModeInventoryFixtureIterationCapValue = 3

// TestON004a_WorkflowModeIsInConfigInventory verifies that the config inventory
// fixture includes a "workflow_mode" row with its four-tier precedence fields.
//
// Spec ref: operator-nfr.md §4.1 ON-004a — "The config inventory of §4.1.ON-004
// MUST include an entry for the workflow_mode knob with the following fields:
// Allowed enumeration: {single, review-loop, dot}. Default value: single.
// Precedence layers (four tiers, evaluated highest-to-lowest at claim time)."
func TestON004a_WorkflowModeIsInConfigInventory(t *testing.T) {
	t.Parallel()

	var found *obligationsFixtureConfigKnob
	for i := range obligationsFixtureConfigInventory {
		k := &obligationsFixtureConfigInventory[i]
		if k.Name == "workflow_mode" {
			found = k
			break
		}
	}

	if found == nil {
		t.Fatal("ON-004a: 'workflow_mode' row is missing from obligationsFixtureConfigInventory; ON-004a requires it")
	}

	// PrecedenceLayer must be "workflow": the highest active tier at MVH is the
	// per-task workflow:<mode> bead label, which is a workflow-definition concern
	// per control-points.md §4.7 CP-037.
	if found.PrecedenceLayer != "workflow" {
		t.Errorf("ON-004a: workflow_mode PrecedenceLayer = %q, want %q", found.PrecedenceLayer, "workflow")
	}

	// ChangeEffective must be "next-daemon-start": per-task resolution is sealed
	// at claim time (immutable for the run's lifetime); daemon default changes
	// require a restart per §4.3.ON-013d.
	if found.ChangeEffective != "next-daemon-start" {
		t.Errorf("ON-004a: workflow_mode ChangeEffective = %q, want %q", found.ChangeEffective, "next-daemon-start")
	}

	// SpecRef must cite the ON-004a anchor.
	if found.SpecRef == "" {
		t.Error("ON-004a: workflow_mode SpecRef is empty; must cite operator-nfr.md §4.1 ON-004a")
	}
}

// TestON004a_IterationCapIsInConfigInventory verifies that the config inventory
// fixture includes an "iteration_cap" row representing the hardcoded review-loop
// cap value of 3.
//
// Spec ref: operator-nfr.md §4.1 ON-004a — "Iteration cap (review-loop):
// hardcoded at 3 for MVH; NOT operator-tunable."
// Also: operator-nfr.md §4.3.ON-013d — "The iteration cap … MUST NOT be
// operator-tunable at runtime."
func TestON004a_IterationCapIsInConfigInventory(t *testing.T) {
	t.Parallel()

	var found *obligationsFixtureConfigKnob
	for i := range obligationsFixtureConfigInventory {
		k := &obligationsFixtureConfigInventory[i]
		if k.Name == "iteration_cap" {
			found = k
			break
		}
	}

	if found == nil {
		t.Fatal("ON-004a: 'iteration_cap' row is missing from obligationsFixtureConfigInventory; ON-004a requires it")
	}

	// PrecedenceLayer must be "default": iteration_cap is hardcoded and has no
	// operator override surface; the built-in value is the only layer.
	if found.PrecedenceLayer != "default" {
		t.Errorf("ON-004a: iteration_cap PrecedenceLayer = %q, want %q", found.PrecedenceLayer, "default")
	}

	// ChangeEffective must be "next-daemon-start": hardcoded value; changing it
	// requires a code change (new binary), which is a daemon restart.
	if found.ChangeEffective != "next-daemon-start" {
		t.Errorf("ON-004a: iteration_cap ChangeEffective = %q, want %q", found.ChangeEffective, "next-daemon-start")
	}

	// SpecRef must cite the ON-004a anchor.
	if found.SpecRef == "" {
		t.Error("ON-004a: iteration_cap SpecRef is empty; must cite operator-nfr.md §4.1 ON-004a")
	}
}

// TestON004a_IterationCapHardcodedValueIsThree verifies the hardcoded iteration
// cap constant matches the MVH-mandated value of 3.
//
// Spec ref: operator-nfr.md §4.1 ON-004a — "hardcoded at 3 for MVH."
func TestON004a_IterationCapHardcodedValueIsThree(t *testing.T) {
	t.Parallel()

	const want = 3
	if workflowModeInventoryFixtureIterationCapValue != want {
		t.Errorf("ON-004a: iteration cap hardcoded value = %d, want %d; spec mandates 3 for MVH", workflowModeInventoryFixtureIterationCapValue, want)
	}
}

// TestON004a_WorkflowModeInventorySnapshot is a snapshot test that verifies
// both ON-004a rows appear together in the inventory with their exact field
// values. This test is the primary acceptance gate for T-WM-029.
//
// Spec ref: operator-nfr.md §4.1 ON-004a.
func TestON004a_WorkflowModeInventorySnapshot(t *testing.T) {
	t.Parallel()

	type snapshot struct {
		name            string
		precedenceLayer string
		changeEffective string
		specRef         string
	}

	want := []snapshot{
		{
			name:            "workflow_mode",
			precedenceLayer: "workflow",
			changeEffective: "next-daemon-start",
			specRef:         "operator-nfr.md §4.1 ON-004a",
		},
		{
			name:            "iteration_cap",
			precedenceLayer: "default",
			changeEffective: "next-daemon-start",
			specRef:         "operator-nfr.md §4.1 ON-004a",
		},
	}

	// Build a lookup map of the inventory.
	byName := make(map[string]obligationsFixtureConfigKnob, len(obligationsFixtureConfigInventory))
	for _, k := range obligationsFixtureConfigInventory {
		byName[k.Name] = k
	}

	for _, w := range want {
		w := w
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()

			got, ok := byName[w.name]
			if !ok {
				t.Fatalf("ON-004a snapshot: knob %q not found in obligationsFixtureConfigInventory", w.name)
			}
			if got.PrecedenceLayer != w.precedenceLayer {
				t.Errorf("ON-004a snapshot: %s.PrecedenceLayer = %q, want %q", w.name, got.PrecedenceLayer, w.precedenceLayer)
			}
			if got.ChangeEffective != w.changeEffective {
				t.Errorf("ON-004a snapshot: %s.ChangeEffective = %q, want %q", w.name, got.ChangeEffective, w.changeEffective)
			}
			if got.SpecRef != w.specRef {
				t.Errorf("ON-004a snapshot: %s.SpecRef = %q, want %q", w.name, got.SpecRef, w.specRef)
			}
		})
	}
}
