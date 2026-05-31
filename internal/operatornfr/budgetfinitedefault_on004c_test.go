package operatornfr_test

// budgetFiniteDefaultFixture — spec-level harness for hk-0p9so.
//
// Covers: ON-004c (per-day USD budget cap config-inventory entry) and the
// finite-default requirement of CL-090 (cognition-loop.md §4.11).
//
// Observable behaviors this fixture documents:
//  1. With NO operator setting the cap is finite (not Infinity/"unlimited"):
//     a high-spend run that reaches the cap MUST enter budget-paused.
//  2. With FLYWHEEL_BUDGET_USD_PER_DAY=unlimited (explicit opt-out), no
//     budget trip fires — the loop is permitted to run unbounded.
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// budget enforcement is the integration-test surface; this file is the §10.2
// sensor verifying the obligation catalog and spec text encode the
// safer-default flip correctly.
//
// Spec refs:
//   - specs/operator-nfr.md §4.1 ON-004c
//   - specs/cognition-loop.md §4.11 CL-090 (finite default + unlimited opt-out)

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// budgetFiniteDefaultFixtureRecommendedCapUSD is the recommended finite
// default for the per-day USD budget cap per CL-090 / ON-004c.
//
// Spec ref: cognition-loop.md §4.11 CL-090 — "recommended 20 USD."
const budgetFiniteDefaultFixtureRecommendedCapUSD = 20.0

// budgetFiniteDefaultFixtureUnlimitedSentinel is the canonical string value
// an operator supplies to opt out of the finite cap.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "or `unlimited` / empty for an
// explicit opt-out."
const budgetFiniteDefaultFixtureUnlimitedSentinel = "unlimited"

// budgetFiniteDefaultFixturePrecedenceTier models one tier in the ON-004c
// three-tier precedence chain for the per-day USD budget cap.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Precedence layers
// (highest-to-lowest): (1) runtime flag `--budget-usd-per-day`; (2)
// `FLYWHEEL_BUDGET_USD_PER_DAY` env; (3) finite built-in default."
type budgetFiniteDefaultFixturePrecedenceTier struct {
	Rank        int    // 1 = highest precedence
	Source      string // human-readable source description
	IsFinite    bool   // whether this tier produces a finite cap when absent/unset
	SpecRef     string
}

// budgetFiniteDefaultFixturePrecedenceChain is the authoritative fixture
// encoding of the ON-004c three-tier precedence chain.
//
// The built-in default (tier 3) MUST produce a finite cap — it is the
// safer-default that the spec requires. Tiers 1 and 2 may carry the
// "unlimited" sentinel to opt out.
var budgetFiniteDefaultFixturePrecedenceChain = []budgetFiniteDefaultFixturePrecedenceTier{
	{
		Rank:     1,
		Source:   "--budget-usd-per-day runtime flag",
		IsFinite: false, // operator may pass "unlimited" to opt out
		SpecRef:  "operator-nfr.md §4.1 ON-004c tier 1",
	},
	{
		Rank:     2,
		Source:   "FLYWHEEL_BUDGET_USD_PER_DAY env var",
		IsFinite: false, // operator may set to "unlimited" or empty to opt out
		SpecRef:  "operator-nfr.md §4.1 ON-004c tier 2",
	},
	{
		Rank:     3,
		Source:   "finite built-in default",
		IsFinite: true, // MUST be finite; the safer-default requirement
		SpecRef:  "operator-nfr.md §4.1 ON-004c tier 3",
	},
}

// budgetFiniteDefaultFixtureReadCognitionLoopSpec reads
// specs/cognition-loop.md and returns its contents. Fatals on read error.
func budgetFiniteDefaultFixtureReadCognitionLoopSpec(t *testing.T, root string) []byte {
	t.Helper()

	specPath := filepath.Join(root, "specs", "cognition-loop.md")
	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("budgetFiniteDefaultFixtureReadCognitionLoopSpec: cannot read %s: %v", specPath, err)
	}
	return data
}

// --- ON-004c config-inventory tests ---

// TestON004c_SpecSectionExists verifies that ON-004c exists in
// specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.1 ON-004c.
func TestON004c_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-004c") {
		t.Error("ON-004c: specs/operator-nfr.md does not contain 'ON-004c'")
	}
	if !strings.Contains(content, "Per-day USD budget cap config-inventory entry") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'Per-day USD budget cap config-inventory entry' heading")
	}
}

// TestON004c_KnobNamesArePresentInSpec verifies the spec names both the env
// var and the flag form of the knob.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Knob:
// `FLYWHEEL_BUDGET_USD_PER_DAY` / `--budget-usd-per-day`."
func TestON004c_KnobNamesArePresentInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "FLYWHEEL_BUDGET_USD_PER_DAY") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'FLYWHEEL_BUDGET_USD_PER_DAY' knob name")
	}
	if !strings.Contains(content, "--budget-usd-per-day") {
		t.Error("ON-004c: specs/operator-nfr.md missing '--budget-usd-per-day' flag name")
	}
}

// TestON004c_DefaultMustBeFiniteStatedInSpec verifies the spec explicitly
// states the default MUST be finite (not unbounded).
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Default value: FINITE
// (recommended 20 USD; the default MUST NOT be unbounded)."
func TestON004c_DefaultMustBeFiniteStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST NOT be unbounded") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'MUST NOT be unbounded' finite-default constraint")
	}
	if !strings.Contains(content, "FINITE") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'FINITE' keyword for the default value declaration")
	}
}

// TestON004c_UnlimitedOptOutStatedInSpec verifies the spec names the explicit
// opt-out sentinel value.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Allowed values: a positive
// number (USD), or `unlimited` / empty for an explicit opt-out."
func TestON004c_UnlimitedOptOutStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "unlimited") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'unlimited' opt-out sentinel value")
	}
	if !strings.Contains(content, "explicit opt-out") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'explicit opt-out' for unlimited/empty values")
	}
}

// TestON004c_ThreeTierPrecedenceStatedInSpec verifies the spec declares a
// three-tier precedence chain (runtime flag > env > built-in default).
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Precedence layers
// (highest-to-lowest): (1) runtime flag; (2) env; (3) finite built-in default."
func TestON004c_ThreeTierPrecedenceStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "finite built-in default") {
		t.Error("ON-004c: specs/operator-nfr.md missing 'finite built-in default' as the lowest precedence tier")
	}
}

// TestON004c_IsInConfigInventoryFixture verifies the
// obligationsFixtureConfigInventory contains a row for budget_usd_per_day
// with the correct precedence layer and change-effective semantics.
//
// Spec ref: operator-nfr.md §4.1 ON-004c.
func TestON004c_IsInConfigInventoryFixture(t *testing.T) {
	t.Parallel()

	var found *obligationsFixtureConfigKnob
	for i := range obligationsFixtureConfigInventory {
		k := &obligationsFixtureConfigInventory[i]
		if k.Name == "budget_usd_per_day" {
			found = k
			break
		}
	}

	if found == nil {
		t.Fatal("ON-004c: 'budget_usd_per_day' row is missing from obligationsFixtureConfigInventory; ON-004c requires it")
	}

	// PrecedenceLayer must be "runtime-override": tier 1 is the --budget-usd-per-day
	// flag, which is a runtime override.
	if found.PrecedenceLayer != "runtime-override" {
		t.Errorf("ON-004c: budget_usd_per_day PrecedenceLayer = %q, want %q", found.PrecedenceLayer, "runtime-override")
	}

	// ChangeEffective must be "next-daemon-start" per the spec.
	if found.ChangeEffective != "next-daemon-start" {
		t.Errorf("ON-004c: budget_usd_per_day ChangeEffective = %q, want %q", found.ChangeEffective, "next-daemon-start")
	}

	// SpecRef must cite the ON-004c anchor.
	if !strings.Contains(found.SpecRef, "ON-004c") {
		t.Errorf("ON-004c: budget_usd_per_day SpecRef = %q; must cite ON-004c", found.SpecRef)
	}
}

// --- Finite-default fixture tests ---

// TestON004c_PrecedenceChainHasThreeTiers verifies the fixture encodes exactly
// three tiers per ON-004c.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — three-tier precedence chain.
func TestON004c_PrecedenceChainHasThreeTiers(t *testing.T) {
	t.Parallel()

	const wantTiers = 3
	if len(budgetFiniteDefaultFixturePrecedenceChain) != wantTiers {
		t.Errorf("ON-004c: precedence chain fixture has %d tiers, want %d", len(budgetFiniteDefaultFixturePrecedenceChain), wantTiers)
	}
}

// TestON004c_PrecedenceTiersAreStrictlyRanked verifies the fixture tiers
// are ranked 1, 2, 3 with no gaps.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "(highest-to-lowest): (1) … (2) … (3) …"
func TestON004c_PrecedenceTiersAreStrictlyRanked(t *testing.T) {
	t.Parallel()

	for i, tier := range budgetFiniteDefaultFixturePrecedenceChain {
		want := i + 1
		if tier.Rank != want {
			t.Errorf("ON-004c: precedence tier at index %d has Rank=%d, want %d; tiers must be strictly ranked 1..3", i, tier.Rank, want)
		}
	}
}

// TestON004c_BuiltInDefaultTierIsFinite verifies that the tier-3 built-in
// default is marked IsFinite=true in the fixture — this is the safer-default
// flip the spec mandates.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "Default value: FINITE; the
// default MUST NOT be unbounded." Also cognition-loop.md §4.11 CL-090 — "An
// unlimited budget MUST be an explicit operator opt-out."
func TestON004c_BuiltInDefaultTierIsFinite(t *testing.T) {
	t.Parallel()

	if len(budgetFiniteDefaultFixturePrecedenceChain) < 3 {
		t.Fatal("ON-004c: precedence chain fixture has fewer than 3 tiers; cannot check built-in default tier")
	}

	builtIn := budgetFiniteDefaultFixturePrecedenceChain[2]
	if builtIn.Rank != 3 {
		t.Fatalf("ON-004c: tier at index 2 has Rank=%d, want 3", builtIn.Rank)
	}
	if !builtIn.IsFinite {
		t.Error("ON-004c: built-in default tier (rank 3) IsFinite=false; the spec requires the built-in default to be FINITE (the safer-default constraint)")
	}
}

// TestON004c_OperatorTiersPermitUnlimited verifies that the operator-supplied
// tiers (rank 1 and 2) are NOT forced to be finite — they may carry the
// "unlimited" sentinel to opt out.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "unlimited / empty for an
// explicit opt-out."
func TestON004c_OperatorTiersPermitUnlimited(t *testing.T) {
	t.Parallel()

	if len(budgetFiniteDefaultFixturePrecedenceChain) < 2 {
		t.Fatal("ON-004c: precedence chain fixture has fewer than 2 tiers")
	}

	for _, tier := range budgetFiniteDefaultFixturePrecedenceChain[:2] {
		tier := tier
		t.Run(tier.Source, func(t *testing.T) {
			t.Parallel()

			if tier.IsFinite {
				t.Errorf("ON-004c: operator tier %d (%s) IsFinite=true; operator tiers MUST permit the 'unlimited' opt-out sentinel", tier.Rank, tier.Source)
			}
		})
	}
}

// TestON004c_RecommendedDefaultCapIsPositive verifies the recommended finite
// default constant is positive.
//
// Spec ref: cognition-loop.md §4.11 CL-090 — "recommended 20 USD."
func TestON004c_RecommendedDefaultCapIsPositive(t *testing.T) {
	t.Parallel()

	if budgetFiniteDefaultFixtureRecommendedCapUSD <= 0 {
		t.Errorf("ON-004c: recommended default cap = %g USD, want > 0; a zero or negative cap is not a safe default", budgetFiniteDefaultFixtureRecommendedCapUSD)
	}
}

// TestON004c_RecommendedDefaultCapIsNotInfinity verifies the recommended
// finite default is not positive infinity.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "the default MUST NOT be unbounded."
func TestON004c_RecommendedDefaultCapIsNotInfinity(t *testing.T) {
	t.Parallel()

	if math.IsInf(budgetFiniteDefaultFixtureRecommendedCapUSD, 1) {
		t.Error("ON-004c: recommended default cap is +Inf; the spec forbids an unbounded default — use a finite positive number")
	}
}

// TestON004c_UnlimitedSentinelIsNonEmpty verifies the unlimited opt-out
// sentinel constant is non-empty.
//
// Spec ref: operator-nfr.md §4.1 ON-004c — "unlimited."
func TestON004c_UnlimitedSentinelIsNonEmpty(t *testing.T) {
	t.Parallel()

	if budgetFiniteDefaultFixtureUnlimitedSentinel == "" {
		t.Error("ON-004c: unlimited sentinel constant is empty string; must be 'unlimited' per the spec")
	}
	if budgetFiniteDefaultFixtureUnlimitedSentinel != "unlimited" {
		t.Errorf("ON-004c: unlimited sentinel = %q, want %q", budgetFiniteDefaultFixtureUnlimitedSentinel, "unlimited")
	}
}

// TestON004c_PrecedenceTiersHaveNonEmptyFields verifies all precedence tiers
// have non-empty Source and SpecRef.
//
// Spec ref: operator-nfr.md §4.1 ON-004c.
func TestON004c_PrecedenceTiersHaveNonEmptyFields(t *testing.T) {
	t.Parallel()

	for _, tier := range budgetFiniteDefaultFixturePrecedenceChain {
		tier := tier
		t.Run(tier.Source, func(t *testing.T) {
			t.Parallel()

			if tier.Source == "" {
				t.Errorf("ON-004c: precedence tier rank %d has empty Source", tier.Rank)
			}
			if tier.SpecRef == "" {
				t.Errorf("ON-004c: precedence tier rank %d (%s) has empty SpecRef", tier.Rank, tier.Source)
			}
		})
	}
}

// --- CL-090 cognition-loop spec tests ---

// TestCL090_SpecSectionExists verifies that CL-090 exists in
// specs/cognition-loop.md.
//
// Spec ref: cognition-loop.md §4.11 CL-090.
func TestCL090_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := budgetFiniteDefaultFixtureReadCognitionLoopSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "CL-090") {
		t.Error("CL-090: specs/cognition-loop.md does not contain 'CL-090'")
	}
}

// TestCL090_FiniteDefaultStatedInCognitionLoopSpec verifies the
// cognition-loop spec explicitly states the finite-default requirement.
//
// Spec ref: cognition-loop.md §4.11 CL-090 — "The per-day USD cap default
// MUST be finite … An unlimited budget MUST be an explicit operator opt-out
// … The loop MUST NOT default to an unbounded cap."
func TestCL090_FiniteDefaultStatedInCognitionLoopSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := budgetFiniteDefaultFixtureReadCognitionLoopSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "MUST NOT default to an unbounded cap") {
		t.Error("CL-090: specs/cognition-loop.md missing 'MUST NOT default to an unbounded cap' finite-default constraint")
	}
	if !strings.Contains(content, "explicit operator opt-out") {
		t.Error("CL-090: specs/cognition-loop.md missing 'explicit operator opt-out' for the unlimited sentinel")
	}
}

// TestCL090_BudgetPausedStateStatedInSpec verifies the cognition-loop spec
// names "budget-paused" as the loop state entered when the cap is exhausted.
//
// Observable: with NO operator setting, a high-spend run that reaches the
// cap enters budget-paused.
//
// Spec ref: cognition-loop.md §4.11 CL-090 — "the loop SHOULD … enter
// `budget-paused` (§6)."
func TestCL090_BudgetPausedStateStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := budgetFiniteDefaultFixtureReadCognitionLoopSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "budget-paused") {
		t.Error("CL-090: specs/cognition-loop.md missing 'budget-paused' loop state; exhausted cap MUST enter this state")
	}
}

// TestCL090_BudgetExhaustedEventStatedInSpec verifies the spec names
// budget_exhausted as the event emitted when the cap is reached.
//
// Observable: with NO operator setting, a run reaching the cap emits
// budget_exhausted before entering budget-paused.
//
// Spec ref: cognition-loop.md §4.11 CL-090 — "the meter MUST emit
// `budget_exhausted{budget_scope=handler_account, …}`."
func TestCL090_BudgetExhaustedEventStatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := budgetFiniteDefaultFixtureReadCognitionLoopSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "budget_exhausted") {
		t.Error("CL-090: specs/cognition-loop.md missing 'budget_exhausted' event name; exhaustion MUST emit this event")
	}
	if !strings.Contains(content, "budget_scope=handler_account") {
		t.Error("CL-090: specs/cognition-loop.md missing 'budget_scope=handler_account' field on the exhaustion event")
	}
}

// TestCL090_UnlimitedOptOutDoesNotTripBudgetPaused verifies the fixture
// correctly models the semantics: when the operator supplies the "unlimited"
// sentinel at tier 1 or 2, no budget trip should fire.
//
// Observable: with FLYWHEEL_BUDGET_USD_PER_DAY=unlimited, no budget_exhausted
// event fires regardless of spend.
//
// This test encodes the spec contract at the fixture level: the two operator
// tiers are NOT finite, meaning the meter's IsFinite guard (checked against
// the active tier) evaluates false → no exhaustion check runs.
func TestCL090_UnlimitedOptOutDoesNotTripBudgetPaused(t *testing.T) {
	t.Parallel()

	// The "unlimited" opt-out contract: if an operator-tier (rank 1 or 2) is
	// active and set to the unlimited sentinel, the cap is effectively disabled.
	// The fixture models this by marking those tiers IsFinite=false.
	for _, tier := range budgetFiniteDefaultFixturePrecedenceChain {
		if tier.Rank == 3 {
			continue // built-in default: finite, NOT the opt-out path
		}
		if tier.IsFinite {
			t.Errorf("CL-090: operator tier %d (%s) IsFinite=true; when an operator supplies 'unlimited' via this tier, no budget_exhausted MUST fire — the fixture must NOT mark operator tiers as always-finite", tier.Rank, tier.Source)
		}
	}

	// The unlimited sentinel itself must parse as a non-numeric value that
	// disables the meter (the implementation recognises the literal string;
	// the fixture validates the sentinel constant is the expected literal).
	if budgetFiniteDefaultFixtureUnlimitedSentinel != "unlimited" {
		t.Errorf("CL-090: unlimited sentinel constant = %q; the cognition-loop spec names 'unlimited' as the opt-out value, which must match exactly", budgetFiniteDefaultFixtureUnlimitedSentinel)
	}
}

// TestCL090_ConformanceScenario6StatedInSpec verifies the spec includes
// conformance scenario 6 (the spend-meter / budget-paused scenario).
//
// Spec ref: cognition-loop.md §4.11 — "6. The unified spend meter halts
// dispatch: with a finite per-day cap …"
func TestCL090_ConformanceScenario6StatedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := budgetFiniteDefaultFixtureReadCognitionLoopSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "finite per-day cap") {
		t.Error("CL-090: specs/cognition-loop.md missing 'finite per-day cap' in conformance scenario 6 (spend-meter halts dispatch)")
	}
}
