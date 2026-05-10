package operatornfr_test

// operatorOverrideFixture — spec-level harness for hk-sx9r.18.
//
// Covers: ON-014 (reconciliation operator override — pre-execution verdict
// pause; confirm-verdict / veto-verdict command surface; default opt-in
// policy; Cat 2, Cat 3, Cat 6a scope).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// enforcement of the verdict pause lives in the reconciliation subsystem;
// this file is the §10.2 sensor verifying the obligation catalog exists and
// is internally consistent with the command taxonomy.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-014, §10.2.

import (
	"strings"
	"testing"
)

// operatorOverrideFixtureCommand models one ON-014 operator-override command
// with its canonical CLI form and required semantic behaviour.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "harmonik confirm-verdict <run_id>
// / harmonik veto-verdict <run_id> [--promote-to escalate-to-human]".
type operatorOverrideFixtureCommand struct {
	CLIForm string // canonical CLI invocation pattern
	SpecRef string // normative spec section
	HasFlag bool   // true if the command carries a --promote-to flag
}

// operatorOverrideFixtureCommands is the authoritative fixture encoding of
// the two ON-014 operator-override commands.
//
// The confirm-verdict command resumes verdict execution; the veto-verdict
// command aborts it and optionally promotes to escalate-to-human.
var operatorOverrideFixtureCommands = []operatorOverrideFixtureCommand{
	{
		CLIForm: "harmonik confirm-verdict <run_id>",
		SpecRef: "operator-nfr.md §4.3 ON-014",
		HasFlag: false,
	},
	{
		CLIForm: "harmonik veto-verdict <run_id> [--promote-to escalate-to-human]",
		SpecRef: "operator-nfr.md §4.3 ON-014",
		HasFlag: true,
	},
}

// operatorOverrideFixtureReconciliationCategory models one reconciliation
// detection category to which the ON-014 override obligation applies.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "all investigator-dispatched
// reconciliation categories (Cat 2, 3, 6a per [reconciliation/spec.md §4.2]
// and [reconciliation/spec.md §8.12])".
type operatorOverrideFixtureReconciliationCategory struct {
	Name    string // canonical category name (e.g. "Cat 2")
	SpecRef string // normative spec section in reconciliation spec
}

// operatorOverrideFixtureCategories enumerates every reconciliation category
// subject to ON-014 operator override.
var operatorOverrideFixtureCategories = []operatorOverrideFixtureReconciliationCategory{
	{"Cat 2", "reconciliation/spec.md §4.2 — non-idempotent in-flight"},
	{"Cat 3", "reconciliation/spec.md §8.12 — store disagreement (generic)"},
	{"Cat 6a", "reconciliation/spec.md §8.12 — integrity violation, LLM-triageable"},
}

// TestON014_SpecSectionExists verifies that ON-014 exists in
// specs/operator-nfr.md with the required heading text.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "Reconciliation operator override
// (pre-execution verdict pause)".
func TestON014_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-014") {
		t.Error("ON-014: specs/operator-nfr.md does not contain 'ON-014'")
	}
	if !strings.Contains(content, "Reconciliation operator override") {
		t.Error("ON-014: specs/operator-nfr.md missing 'Reconciliation operator override' heading text")
	}
}

// TestON014_ConfirmVerdictCommandNamedInSpec verifies that the spec names the
// confirm-verdict command surface by its canonical CLI form.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "harmonik confirm-verdict <run_id>".
func TestON014_ConfirmVerdictCommandNamedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "confirm-verdict") {
		t.Error("ON-014: specs/operator-nfr.md missing 'confirm-verdict' command name")
	}
}

// TestON014_VetoVerdictCommandNamedInSpec verifies that the spec names the
// veto-verdict command surface by its canonical CLI form.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "harmonik veto-verdict <run_id>
// [--promote-to escalate-to-human]".
func TestON014_VetoVerdictCommandNamedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "veto-verdict") {
		t.Error("ON-014: specs/operator-nfr.md missing 'veto-verdict' command name")
	}
}

// TestON014_VetoVerdictPromoteFlagNamedInSpec verifies that the spec names the
// --promote-to flag and its escalate-to-human argument.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "--promote-to escalate-to-human".
func TestON014_VetoVerdictPromoteFlagNamedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "--promote-to") {
		t.Error("ON-014: specs/operator-nfr.md missing '--promote-to' flag in veto-verdict command surface")
	}
	if !strings.Contains(content, "escalate-to-human") {
		t.Error("ON-014: specs/operator-nfr.md missing 'escalate-to-human' as the --promote-to target value")
	}
}

// TestON014_DefaultIsOptIn verifies that the spec declares the operator-override
// policy as opt-in (default: execution proceeds without operator confirmation).
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "Default: execution proceeds without
// operator confirmation; operators opt in by policy."
func TestON014_DefaultIsOptIn(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "opt in by policy") {
		t.Error("ON-014: specs/operator-nfr.md missing 'opt in by policy' default-behaviour declaration")
	}
}

// TestON014_CommandFixtureHasTwoCommands verifies that the fixture encodes
// exactly two ON-014 operator-override commands.
//
// Spec ref: operator-nfr.md §4.3 ON-014.
func TestON014_CommandFixtureHasTwoCommands(t *testing.T) {
	t.Parallel()

	const wantCommands = 2
	if len(operatorOverrideFixtureCommands) != wantCommands {
		t.Errorf("ON-014: command fixture has %d entries, want %d (confirm-verdict and veto-verdict)",
			len(operatorOverrideFixtureCommands), wantCommands)
	}
}

// TestON014_CommandFixtureHasSpecRefs verifies that every command in the
// fixture carries a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.3 ON-014.
func TestON014_CommandFixtureHasSpecRefs(t *testing.T) {
	t.Parallel()

	for _, cmd := range operatorOverrideFixtureCommands {
		cmd := cmd
		t.Run(cmd.CLIForm, func(t *testing.T) {
			t.Parallel()

			if cmd.SpecRef == "" {
				t.Errorf("ON-014: command %q has empty SpecRef", cmd.CLIForm)
			}
		})
	}
}

// TestON014_VetoVerdictHasPromoteFlag verifies that exactly the veto-verdict
// command carries the --promote-to flag (confirm-verdict does not).
//
// Spec ref: operator-nfr.md §4.3 ON-014 — flag applies to veto only.
func TestON014_VetoVerdictHasPromoteFlag(t *testing.T) {
	t.Parallel()

	for _, cmd := range operatorOverrideFixtureCommands {
		cmd := cmd
		t.Run(cmd.CLIForm, func(t *testing.T) {
			t.Parallel()

			isVeto := strings.Contains(cmd.CLIForm, "veto-verdict")
			if isVeto && !cmd.HasFlag {
				t.Errorf("ON-014: veto-verdict command %q HasFlag=false; --promote-to flag is required per spec", cmd.CLIForm)
			}
			if !isVeto && cmd.HasFlag {
				t.Errorf("ON-014: non-veto command %q HasFlag=true; only veto-verdict carries --promote-to", cmd.CLIForm)
			}
		})
	}
}

// TestON014_CategoryFixtureHasThreeCategories verifies that the fixture
// encodes exactly three reconciliation categories subject to ON-014.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "Cat 2, 3, 6a".
func TestON014_CategoryFixtureHasThreeCategories(t *testing.T) {
	t.Parallel()

	const wantCategories = 3
	if len(operatorOverrideFixtureCategories) != wantCategories {
		t.Errorf("ON-014: category fixture has %d entries, want %d (Cat 2, Cat 3, Cat 6a)",
			len(operatorOverrideFixtureCategories), wantCategories)
	}
}

// TestON014_CategoryFixtureCoversCat2Cat3Cat6a verifies that Cat 2, Cat 3,
// and Cat 6a are all present in the fixture.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "investigator-dispatched
// reconciliation categories (Cat 2, 3, 6a per [reconciliation/spec.md §4.2]
// and [reconciliation/spec.md §8.12])".
func TestON014_CategoryFixtureCoversCat2Cat3Cat6a(t *testing.T) {
	t.Parallel()

	required := map[string]bool{
		"Cat 2":  false,
		"Cat 3":  false,
		"Cat 6a": false,
	}
	for _, cat := range operatorOverrideFixtureCategories {
		required[cat.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-014: reconciliation category %q is missing from the fixture; spec §4.3.ON-014 declares all three as in-scope", name)
		}
	}
}

// TestON014_CategoryFixtureHasSpecRefs verifies that every category in the
// fixture carries a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.3 ON-014.
func TestON014_CategoryFixtureHasSpecRefs(t *testing.T) {
	t.Parallel()

	for _, cat := range operatorOverrideFixtureCategories {
		cat := cat
		t.Run(cat.Name, func(t *testing.T) {
			t.Parallel()

			if cat.SpecRef == "" {
				t.Errorf("ON-014: reconciliation category %q has empty SpecRef", cat.Name)
			}
		})
	}
}

// TestON014_InvalidStateExitCodeExistsInTaxonomy verifies that §8 code 16
// (operator-control-invalid-state) is present in the taxonomy, as required
// for confirm-verdict / veto-verdict commands issued when no pending verdict
// exists.
//
// Spec ref: operator-nfr.md §4.3 ON-014; §8 code 16
// operator-control-invalid-state.
func TestON014_InvalidStateExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	const invalidStateCode = 16
	e, ok := exitCodeFixtureLookup(invalidStateCode)
	if !ok {
		t.Errorf("ON-014: §8 taxonomy missing code %d (operator-control-invalid-state); confirm/veto commands depend on this code for invalid-state rejection", invalidStateCode)
		return
	}
	if e.Category != "operator-control-invalid-state" {
		t.Errorf("ON-014: §8 code %d category = %q, want %q",
			invalidStateCode, e.Category, "operator-control-invalid-state")
	}
}

// TestON014_FoundationOwnsNamingReconciliationOwnsExecution verifies that the
// spec declares the ownership split: foundation owns the command naming
// convention and reconciliation/spec.md owns the execution-step specifics.
//
// Spec ref: operator-nfr.md §4.3 ON-014 — "Foundation owns naming convention;
// [reconciliation/spec.md §4.5] owns the execution-step specifics."
func TestON014_FoundationOwnsNamingReconciliationOwnsExecution(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "Foundation owns") {
		t.Error("ON-014: specs/operator-nfr.md missing 'Foundation owns' ownership declaration")
	}
	if !strings.Contains(content, "reconciliation/spec.md") {
		t.Error("ON-014: specs/operator-nfr.md missing 'reconciliation/spec.md' cross-spec reference in ON-014 context")
	}
}
