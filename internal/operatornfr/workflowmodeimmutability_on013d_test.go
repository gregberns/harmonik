package operatornfr_test

// workflowModeImmutability — spec-level harness for hk-vj96j.
//
// Covers: operator-nfr.md §4.3 ON-013d — "Workflow mode is not an
// operator-control surface."
//
// The spec requires:
//   (1) workflow_mode is observable via `harmonik status` (both daemon default
//       and per-run resolved value for any in-flight run).
//   (2) workflow_mode MUST NOT be mutable via any operator command.
//   (3) There MUST NOT be a `harmonik set-mode` command or any equivalent
//       runtime tuning surface.
//   (4) There MUST NOT be a `pause-then-set-mode` workflow.
//   (5) Once a bead is claimed, the resolved workflow_mode is sealed into the
//       Run record per execution-model.md §4.3 and is immutable for the run's
//       lifetime.
//   (6) The iteration cap (3 for review-loop) MUST NOT be operator-tunable at
//       runtime.
//
// These tests are §10.2 sensor-layer checks: they verify the fixture
// declarations, spec text, and structural code invariants at the static level.
// Runtime enforcement (ensuring the daemon rejects set-mode RPCs) is an
// integration test surface.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-013d.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// workflowModeMutationVerbFixture models one operator command verb that ON-013d
// prohibits from existing in the harmonik operator-control surface.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "There MUST NOT be a
// `harmonik set-mode` command or any equivalent runtime tuning surface."
type workflowModeMutationVerbFixture struct {
	Verb    string // prohibited command verb
	SpecRef string // normative spec section
}

// workflowModeMutationVerbsFixture is the authoritative fixture encoding of
// prohibited workflow-mode mutation verbs. These MUST NOT appear as CommandName
// constants or in the CommandExitCodeSets declaration.
//
// Spec ref: operator-nfr.md §4.3 ON-013d.
var workflowModeMutationVerbsFixture = []workflowModeMutationVerbFixture{
	{
		"set-mode",
		"operator-nfr.md §4.3 ON-013d — 'There MUST NOT be a `harmonik set-mode` command'",
	},
	{
		"change-mode",
		"operator-nfr.md §4.3 ON-013d — no equivalent runtime tuning surface",
	},
	{
		"update-mode",
		"operator-nfr.md §4.3 ON-013d — no equivalent runtime tuning surface",
	},
	{
		"set-workflow-mode",
		"operator-nfr.md §4.3 ON-013d — no equivalent runtime tuning surface",
	},
}

// TestON013d_SpecSectionExists verifies that ON-013d text is present in the
// operator-nfr spec.
//
// Spec ref: operator-nfr.md §4.3 ON-013d.
func TestON013d_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-013d") {
		t.Error("ON-013d: specs/operator-nfr.md does not contain 'ON-013d'; the workflow-mode immutability section must exist")
	}

	if !strings.Contains(content, "Workflow mode is not an operator-control surface") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'Workflow mode is not an operator-control surface' heading")
	}
}

// TestON013d_NoSetModeCommandInExitCodeSets verifies that no prohibited
// workflow-mode mutation verb appears in CommandExitCodeSets. Any entry in
// that table represents a real operator-callable command; if a set-mode verb
// appeared there it would be a violation of ON-013d.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "MUST NOT be mutable via any
// operator command."
func TestON013d_NoSetModeCommandInExitCodeSets(t *testing.T) {
	t.Parallel()

	for _, verb := range workflowModeMutationVerbsFixture {
		verb := verb
		t.Run(verb.Verb, func(t *testing.T) {
			t.Parallel()

			_, found := operatornfr.CommandLookup(operatornfr.CommandName(verb.Verb))
			if found {
				t.Errorf("ON-013d: prohibited command %q found in CommandExitCodeSets; %s",
					verb.Verb, verb.SpecRef)
			}
		})
	}
}

// TestON013d_CommandStatusIsInExitCodeSets verifies that `harmonik status` is
// declared in CommandExitCodeSets. The status command is the required
// observation surface for workflow_mode per ON-013d.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "MUST be observable via
// `harmonik status`."
func TestON013d_CommandStatusIsInExitCodeSets(t *testing.T) {
	t.Parallel()

	_, found := operatornfr.CommandLookup(operatornfr.CommandStatus)
	if !found {
		t.Error("ON-013d: 'status' command not found in CommandExitCodeSets; ON-013d requires workflow_mode to be observable via `harmonik status`")
	}
}

// TestON013d_WorkflowModeNotMutableInSpec verifies that the spec explicitly
// states workflow_mode MUST NOT be mutable via operator commands and MUST NOT
// have a runtime tuning surface.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "MUST NOT be mutable via any
// operator command … MUST NOT be a `harmonik set-mode` command or any
// equivalent runtime tuning surface."
func TestON013d_WorkflowModeNotMutableInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST NOT be mutable") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'MUST NOT be mutable' prohibition text in ON-013d")
	}

	if !strings.Contains(content, "set-mode") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'set-mode' prohibition example in ON-013d")
	}

	if !strings.Contains(content, "runtime tuning surface") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'runtime tuning surface' prohibition text in ON-013d")
	}
}

// TestON013d_WorkflowModeSealedAtClaimTimeInSpec verifies that the spec
// declares workflow_mode is sealed into the Run record at claim time and is
// immutable for the run's lifetime.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "Once a bead is claimed, the
// resolved workflow_mode is sealed into the Run record … and is immutable for
// the run's lifetime."
func TestON013d_WorkflowModeSealedAtClaimTimeInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "sealed into the Run record") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'sealed into the Run record' text in ON-013d")
	}

	if !strings.Contains(content, "immutable for the run") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'immutable for the run' text in ON-013d")
	}
}

// TestON013d_IterationCapNotOperatorTunableInSpec verifies that the spec
// explicitly prohibits operator-runtime tuning of the iteration cap.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "The iteration cap … MUST NOT be
// operator-tunable at runtime."
func TestON013d_IterationCapNotOperatorTunableInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "iteration cap") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'iteration cap' reference in ON-013d")
	}

	if !strings.Contains(content, "MUST NOT be operator-tunable at runtime") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'MUST NOT be operator-tunable at runtime' prohibition for iteration cap in ON-013d")
	}
}

// TestON013d_DaemonRestartRequiredToChangeModeInSpec verifies that the spec
// states operators MUST restart the daemon with a different config to change
// the daemon default workflow_mode.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "Operators wishing to change the
// daemon default MUST restart the daemon with a different config."
func TestON013d_DaemonRestartRequiredToChangeModeInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST restart the daemon") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'MUST restart the daemon' requirement for changing workflow_mode default in ON-013d")
	}
}

// TestON013d_ProposalsToAddMutationSurfaceMustBeRejectedInSpec verifies that
// the spec contains the explicit requirement that proposals to introduce a
// runtime mode-mutation surface MUST be rejected.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "Proposals to introduce a runtime
// mode-mutation surface MUST be rejected as violations of this requirement."
func TestON013d_ProposalsToAddMutationSurfaceMustBeRejectedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST be rejected") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'MUST be rejected' language for mode-mutation proposals in ON-013d")
	}
}

// TestON013d_MutationVerbFixtureIsNonEmpty verifies that the prohibited-verb
// fixture is non-empty so that the sensor tests are not vacuously passing
// against an empty fixture.
//
// Spec ref: operator-nfr.md §4.3 ON-013d.
func TestON013d_MutationVerbFixtureIsNonEmpty(t *testing.T) {
	t.Parallel()

	const minRequired = 1
	if len(workflowModeMutationVerbsFixture) < minRequired {
		t.Errorf("ON-013d: workflowModeMutationVerbsFixture has %d entries, want at least %d; the fixture must enumerate at least the 'set-mode' verb", len(workflowModeMutationVerbsFixture), minRequired)
	}
}

// TestON013d_MutationVerbFixtureHasSpecRefs verifies that every prohibited
// verb entry has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.3 ON-013d.
func TestON013d_MutationVerbFixtureHasSpecRefs(t *testing.T) {
	t.Parallel()

	for _, v := range workflowModeMutationVerbsFixture {
		v := v
		t.Run(v.Verb, func(t *testing.T) {
			t.Parallel()

			if v.SpecRef == "" {
				t.Errorf("ON-013d: prohibited verb %q has empty SpecRef", v.Verb)
			}
		})
	}
}

// TestON013d_WorkflowModeObservableViaStatusInSpec verifies that the spec
// declares workflow_mode to be observable via `harmonik status`.
//
// Spec ref: operator-nfr.md §4.3 ON-013d — "MUST be observable via
// `harmonik status` — both the daemon's default mode and the per-run resolved
// value for any in-flight run."
func TestON013d_WorkflowModeObservableViaStatusInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "harmonik status") {
		t.Error("ON-013d: specs/operator-nfr.md missing 'harmonik status' observation surface reference in ON-013d")
	}
}
