package operatornfr_test

// credinjection_on008a_test.go — spec-text conformance tests for ON-008a.
//
// ON-008a has two obligations:
//  1. `harmonik supervise start` MUST inject the credential into Pi from the
//     non-committed scoped source (CI-006 / credential-isolation.md §4.4).
//  2. The `budget-paused` pause-reason MUST be surfaced to the operator via
//     `harmonik supervise` alongside `circuit-tripped` per §9.
//
// These are spec-artifact existence tests: they verify the operator-nfr.md and
// cognition-loop.md spec files contain the normative text for both obligations.
// Runtime enforcement is tested in cmd/harmonik/supervise/on008a_budgetpaused_test.go
// and cmd/harmonik/supervise/ci006_ci001_explore_hk96s75_test.go.
//
// Spec refs:
//   - specs/operator-nfr.md §4.3 ON-008a
//   - specs/cognition-loop.md §6 LoopStatus, §9 cross-spec coordination.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers — spec file readers (private to this file)
// ---------------------------------------------------------------------------

func on008aReadOperatorNFRSpec(t *testing.T, root string) []byte {
	t.Helper()
	specPath := filepath.Join(root, "specs", "operator-nfr.md")
	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("on008aReadOperatorNFRSpec: cannot read %s: %v", specPath, err)
	}
	return data
}

func on008aReadCognitionLoopSpec(t *testing.T, root string) []byte {
	t.Helper()
	specPath := filepath.Join(root, "specs", "cognition-loop.md")
	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("on008aReadCognitionLoopSpec: cannot read %s: %v", specPath, err)
	}
	return data
}

// ---------------------------------------------------------------------------
// ON-008a obligation 1 — credential injection via supervise start
// ---------------------------------------------------------------------------

// TestON008a_SpecSectionExists verifies that ON-008a is present in
// specs/operator-nfr.md as a named requirement.
func TestON008a_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadOperatorNFRSpec(t, root))

	if !strings.Contains(content, "ON-008a") {
		t.Error("ON-008a: specs/operator-nfr.md does not contain 'ON-008a'; the requirement must exist in the spec")
	}
}

// TestON008a_CredentialInjectionObligationIsStatedInSpec verifies that the
// operator-nfr spec names `supervise start` as the credential injection
// entry point per CI-006 (obligation 1 of ON-008a).
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a — "`harmonik supervise start`
// MUST inject the credential into the Pi cognition (holder) process from the
// non-committed scoped source per [credential-isolation.md §4.4 CI-006]."
func TestON008a_CredentialInjectionObligationIsStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadOperatorNFRSpec(t, root))

	// The spec must name the supervise start command as the injection entry point.
	if !strings.Contains(content, "supervise start") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'supervise start'; ON-008a must name it as the credential injection entry point")
	}

	// The spec must reference the CI-006 scoped source.
	if !strings.Contains(content, "CI-006") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'CI-006' reference; ON-008a must cite the scoped source per credential-isolation.md §4.4")
	}
}

// ---------------------------------------------------------------------------
// ON-008a obligation 2 — budget-paused surfacing via harmonik supervise
// ---------------------------------------------------------------------------

// TestON008a_BudgetPausedMustBeSurfacedIsStatedInSpec verifies that the
// operator-nfr spec declares `budget-paused` MUST be surfaced to the operator
// alongside `circuit-tripped` (obligation 2 of ON-008a).
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a — "The `budget-paused`
// pause-reason … MUST be surfaced to the operator per §9 alongside
// `circuit-tripped`."
func TestON008a_BudgetPausedMustBeSurfacedIsStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadOperatorNFRSpec(t, root))

	if !strings.Contains(content, "budget-paused") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'budget-paused'; ON-008a must name this pause-reason state")
	}

	if !strings.Contains(content, "circuit-tripped") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'circuit-tripped'; ON-008a must name it as the peer state surfaced alongside budget-paused")
	}

	// The spec uses MUST for this obligation.
	if !strings.Contains(content, "MUST be surfaced to the operator") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'MUST be surfaced to the operator'; the surfacing obligation must be normative (MUST)")
	}
}

// TestON008a_OperatorClearsBudgetPausedViaResumeIsStatedInSpec verifies that
// the spec states the operator clears budget-paused via `harmonik supervise resume`.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a — "The operator clears the
// budget-exhaustion handler pause via the existing handler-resume surface
// (`harmonik supervise resume`); reset is not automatic."
func TestON008a_OperatorClearsBudgetPausedViaResumeIsStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadOperatorNFRSpec(t, root))

	if !strings.Contains(content, "supervise resume") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'supervise resume'; ON-008a must name it as the operator clear path for budget-paused")
	}

	if !strings.Contains(content, "reset is not automatic") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'reset is not automatic'; ON-008a must state that reset is operator-driven, not automatic")
	}
}

// ---------------------------------------------------------------------------
// Cognition-loop spec cross-checks
// ---------------------------------------------------------------------------

// TestON008a_CognitionLoopSpecNamesBudgetPausedAsLoopStatus verifies that
// cognition-loop.md §6 declares `budget-paused` as a LoopStatus value.
//
// Spec ref: specs/cognition-loop.md §6 — LoopStatus type:
// `{starting, ready, paused, budget-paused, circuit-tripped, draining, stopped}`
func TestON008a_CognitionLoopSpecNamesBudgetPausedAsLoopStatus(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadCognitionLoopSpec(t, root))

	if !strings.Contains(content, "budget-paused") {
		t.Error("ON-008a: specs/cognition-loop.md missing 'budget-paused' in LoopStatus enum; the cognition loop spec must declare this as a valid loop state")
	}

	if !strings.Contains(content, "circuit-tripped") {
		t.Error("ON-008a: specs/cognition-loop.md missing 'circuit-tripped' in LoopStatus enum; CL-091 circuit breaker must name this state")
	}
}

// TestON008a_CognitionLoopSpecCrossRefNamesSuperviseSurface verifies that
// cognition-loop.md §9 (cross-spec coordination) names `harmonik supervise`
// as the operator-facing surface for budget-paused / circuit-tripped.
//
// Spec ref: specs/cognition-loop.md §9 — "`harmonik supervise` is the
// operator-facing surface; new pause-reason states (`budget-paused`,
// `circuit-tripped`) supervise SHOULD surface."
func TestON008a_CognitionLoopSpecCrossRefNamesSuperviseSurface(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadCognitionLoopSpec(t, root))

	// §9 of cognition-loop.md must reference harmonik supervise as the surface.
	if !strings.Contains(content, "harmonik supervise") {
		t.Error("ON-008a: specs/cognition-loop.md §9 missing 'harmonik supervise'; the cross-spec coordination section must name supervise as the operator surface for loop pause states")
	}
}

// TestON008a_SpecNamesHandlerPausePolicyIsTriggered verifies that
// operator-nfr.md ON-008a references the handler-pause policy (HP-012) as
// the mechanism that enters budget-paused state.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-008a — "the budget-exhaustion
// handler-pause policy fires ([handler-pause.md §4 HP-012])."
func TestON008a_SpecNamesHandlerPausePolicyIsTriggered(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	content := string(on008aReadOperatorNFRSpec(t, root))

	if !strings.Contains(content, "HP-012") {
		t.Error("ON-008a: specs/operator-nfr.md missing 'HP-012'; ON-008a must cite the handler-pause budget-exhaustion policy that triggers budget-paused entry")
	}
}
