package operatornfr_test

// budgetDefaultsFixture — spec-level harness for hk-sx9r.84.
//
// Covers: ON-047 (category defaults for resource budgets — 5-row table),
// ON-048 (exhaustion protocol — 4-step sequence), ON-049 (attribution shape —
// 5-tuple + delegation_path).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// budget enforcement is the implementation-level integration test surface; this
// file is the §10.2 sensor layer verifying the obligation catalog is internally
// consistent.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-047..ON-049, §10.2.

import (
	"strings"
	"testing"
)

// budgetDefaultsFixtureCategory models one row in the ON-047 category-defaults
// table.
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "5-row defaults table: token
// budget (per-run) 200_000, wall-clock (per-run) 30 min, iterations (per-run)
// 50, wall-clock (per-reconciliation-workflow) 10 min, warning threshold 80%."
type budgetDefaultsFixtureCategory struct {
	Category     string // human-readable category name
	DefaultValue int64  // numeric default (unit depends on category)
	Unit         string // tokens, seconds, iterations, percent
	AppliesTo    string // scope description from the spec table
	SpecRef      string
}

// budgetDefaultsFixtureCategoryDefaults is the authoritative fixture encoding
// of the ON-047 five-row category-defaults table.
//
// Units:
//   - Token budget: tokens (200_000)
//   - Wall-clock per-run: seconds (1800 = 30 * 60)
//   - Iterations per-run: iterations (50)
//   - Wall-clock per-reconciliation-workflow: seconds (600 = 10 * 60)
//   - Warning threshold: percent-fraction-times-100 (80)
var budgetDefaultsFixtureCategoryDefaults = []budgetDefaultsFixtureCategory{
	{
		Category:     "token-budget-per-run",
		DefaultValue: 200_000,
		Unit:         "tokens",
		AppliesTo:    "any agentic node",
		SpecRef:      "ON-047 row 1",
	},
	{
		Category:     "wall-clock-per-run",
		DefaultValue: 1800, // 30 minutes in seconds
		Unit:         "seconds",
		AppliesTo:    "any agentic node",
		SpecRef:      "ON-047 row 2",
	},
	{
		Category:     "iterations-per-run",
		DefaultValue: 50,
		Unit:         "iterations",
		AppliesTo:    "any agentic node",
		SpecRef:      "ON-047 row 3",
	},
	{
		Category:     "wall-clock-per-reconciliation-workflow",
		DefaultValue: 600, // 10 minutes in seconds
		Unit:         "seconds",
		AppliesTo:    "reconciliation-dispatched investigator runs",
		SpecRef:      "ON-047 row 4",
	},
	{
		Category:     "warning-threshold",
		DefaultValue: 80, // 80% represented as integer
		Unit:         "percent",
		AppliesTo:    "all categories",
		SpecRef:      "ON-047 row 5",
	},
}

// budgetDefaultsFixtureExhaustionStep models one step in the ON-048
// exhaustion protocol.
//
// Spec ref: operator-nfr.md §4.11 ON-048 — "MUST: 1. Emit budget_exhausted …
// 2. Terminate … 3. Route through exhaustion-routing policy … 4. Emit
// dispatch_deferred."
type budgetDefaultsFixtureExhaustionStep struct {
	Number  int    // 1-based step index
	Summary string // brief description
	SpecRef string
}

// budgetDefaultsFixtureExhaustionSequence is the authoritative fixture
// encoding of the ON-048 four-step exhaustion protocol.
var budgetDefaultsFixtureExhaustionSequence = []budgetDefaultsFixtureExhaustionStep{
	{1, "emit budget_exhausted event", "ON-048 step 1"},
	{2, "terminate at next safe boundary", "ON-048 step 2"},
	{3, "route through exhaustion-routing policy (pause-and-escalate default)", "ON-048 step 3"},
	{4, "emit dispatch_deferred if machine ceiling breached", "ON-048 step 4"},
}

// budgetDefaultsFixtureAttributionField models one field in the ON-049
// attribution 5-tuple.
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "conceptual shape (run_id, role,
// node_id, category, amount) plus delegation_path."
type budgetDefaultsFixtureAttributionField struct {
	Name         string // field name
	IsDelegation bool   // true for delegation_path (only for cognition-tagged steps)
	SpecRef      string
}

// budgetDefaultsFixtureAttributionFields is the authoritative encoding of the
// ON-049 attribution shape (5-tuple + delegation_path = 6 fields total).
var budgetDefaultsFixtureAttributionFields = []budgetDefaultsFixtureAttributionField{
	{"run_id", false, "ON-049 5-tuple field 1"},
	{"role", false, "ON-049 5-tuple field 2"},
	{"node_id", false, "ON-049 5-tuple field 3"},
	{"category", false, "ON-049 5-tuple field 4"},
	{"amount", false, "ON-049 5-tuple field 5"},
	{"delegation_path", true, "ON-049 — for cognition-tagged steps (control-points.md §4.8 CP-039)"},
}

// budgetDefaultsFixtureAggregationLevel models one aggregation level per
// ON-049.
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "Aggregation levels are: per-run,
// per-role, per-workflow, and per-harmonik-instance."
type budgetDefaultsFixtureAggregationLevel struct {
	Name    string
	IndexOn string // what field is used to index this level
	SpecRef string
}

// budgetDefaultsFixtureAggregationLevels encodes the four ON-049 aggregation
// levels.
var budgetDefaultsFixtureAggregationLevels = []budgetDefaultsFixtureAggregationLevel{
	{"per-run", "run_id", "ON-049"},
	{"per-role", "role", "ON-049"},
	{"per-workflow", "workflow_id", "ON-049"},
	{"per-harmonik-instance", "daemon lifetime total", "ON-049"},
}

// --- ON-047 tests ---

// TestON047_SpecSectionExists verifies that ON-047 (category defaults for
// resource budgets) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.11 ON-047.
func TestON047_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-047") {
		t.Error("ON-047: specs/operator-nfr.md does not contain 'ON-047'")
	}
	if !strings.Contains(content, "Category defaults for resource budgets") {
		t.Error("ON-047: specs/operator-nfr.md missing 'Category defaults for resource budgets' heading")
	}
}

// TestON047_DefaultsTableHasFiveRows verifies the fixture category-defaults
// table has exactly five rows as declared in ON-047.
//
// Spec ref: operator-nfr.md §4.11 ON-047 — five-row defaults table.
func TestON047_DefaultsTableHasFiveRows(t *testing.T) {
	t.Parallel()

	const wantRows = 5
	if len(budgetDefaultsFixtureCategoryDefaults) != wantRows {
		t.Errorf("ON-047: category-defaults fixture has %d rows, want %d", len(budgetDefaultsFixtureCategoryDefaults), wantRows)
	}
}

// TestON047_TokenBudgetPerRunIs200K verifies that the per-run token budget
// default is 200,000 tokens.
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "Token budget (per-run): 200_000."
func TestON047_TokenBudgetPerRunIs200K(t *testing.T) {
	t.Parallel()

	found := false
	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		if row.Category == "token-budget-per-run" {
			found = true
			if row.DefaultValue != 200_000 {
				t.Errorf("ON-047: token-budget-per-run default = %d, want 200000", row.DefaultValue)
			}
			if row.Unit != "tokens" {
				t.Errorf("ON-047: token-budget-per-run unit = %q, want \"tokens\"", row.Unit)
			}
		}
	}
	if !found {
		t.Error("ON-047: 'token-budget-per-run' row missing from defaults fixture")
	}
}

// TestON047_WallClockPerRunIs30Minutes verifies that the per-run wall-clock
// budget default is 30 minutes (1800 seconds).
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "Wall-clock budget (per-run): 30 minutes."
func TestON047_WallClockPerRunIs30Minutes(t *testing.T) {
	t.Parallel()

	found := false
	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		if row.Category == "wall-clock-per-run" {
			found = true
			if row.DefaultValue != 1800 {
				t.Errorf("ON-047: wall-clock-per-run default = %d s, want 1800 s (30 min)", row.DefaultValue)
			}
		}
	}
	if !found {
		t.Error("ON-047: 'wall-clock-per-run' row missing from defaults fixture")
	}
}

// TestON047_IterationsPerRunIs50 verifies that the per-run iterations budget
// default is 50 iterations.
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "Iterations budget (per-run): 50."
func TestON047_IterationsPerRunIs50(t *testing.T) {
	t.Parallel()

	found := false
	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		if row.Category == "iterations-per-run" {
			found = true
			if row.DefaultValue != 50 {
				t.Errorf("ON-047: iterations-per-run default = %d, want 50", row.DefaultValue)
			}
		}
	}
	if !found {
		t.Error("ON-047: 'iterations-per-run' row missing from defaults fixture")
	}
}

// TestON047_WallClockPerReconciliationIs10Minutes verifies that the
// per-reconciliation-workflow wall-clock default is 10 minutes (600 seconds).
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "Wall-clock budget
// (per-reconciliation-workflow): 10 minutes."
func TestON047_WallClockPerReconciliationIs10Minutes(t *testing.T) {
	t.Parallel()

	found := false
	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		if row.Category == "wall-clock-per-reconciliation-workflow" {
			found = true
			if row.DefaultValue != 600 {
				t.Errorf("ON-047: wall-clock-per-reconciliation-workflow default = %d s, want 600 s (10 min)", row.DefaultValue)
			}
		}
	}
	if !found {
		t.Error("ON-047: 'wall-clock-per-reconciliation-workflow' row missing from defaults fixture")
	}
}

// TestON047_WarningThresholdIs80Percent verifies that the warning threshold
// default is 80%.
//
// Spec ref: operator-nfr.md §4.11 ON-047 — "Warning threshold: 80% of budget."
func TestON047_WarningThresholdIs80Percent(t *testing.T) {
	t.Parallel()

	found := false
	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		if row.Category == "warning-threshold" {
			found = true
			if row.DefaultValue != 80 {
				t.Errorf("ON-047: warning-threshold default = %d%%, want 80%%", row.DefaultValue)
			}
		}
	}
	if !found {
		t.Error("ON-047: 'warning-threshold' row missing from defaults fixture")
	}
}

// TestON047_DefaultsTableRowsHaveRequiredFields verifies all rows have
// non-empty Category, Unit, AppliesTo, and SpecRef.
//
// Spec ref: operator-nfr.md §4.11 ON-047.
func TestON047_DefaultsTableRowsHaveRequiredFields(t *testing.T) {
	t.Parallel()

	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		row := row
		t.Run(row.Category, func(t *testing.T) {
			t.Parallel()

			if row.Category == "" {
				t.Error("ON-047: category-defaults row has empty Category")
			}
			if row.Unit == "" {
				t.Errorf("ON-047: category %q has empty Unit", row.Category)
			}
			if row.AppliesTo == "" {
				t.Errorf("ON-047: category %q has empty AppliesTo", row.Category)
			}
			if row.SpecRef == "" {
				t.Errorf("ON-047: category %q has empty SpecRef", row.Category)
			}
		})
	}
}

// TestON047_DefaultValuesMustBePositive verifies all default values are
// positive (zero-defaults would mean no budget constraint).
//
// Spec ref: operator-nfr.md §4.11 ON-047 — defaults exist to make "no policy
// declared" a safe state.
func TestON047_DefaultValuesMustBePositive(t *testing.T) {
	t.Parallel()

	for _, row := range budgetDefaultsFixtureCategoryDefaults {
		row := row
		t.Run(row.Category, func(t *testing.T) {
			t.Parallel()

			if row.DefaultValue <= 0 {
				t.Errorf("ON-047: category %q has default value %d, want > 0", row.Category, row.DefaultValue)
			}
		})
	}
}

// --- ON-048 tests ---

// TestON048_SpecSectionExists verifies that ON-048 (exhaustion protocol)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.11 ON-048.
func TestON048_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-048") {
		t.Error("ON-048: specs/operator-nfr.md does not contain 'ON-048'")
	}
	if !strings.Contains(content, "Exhaustion protocol") {
		t.Error("ON-048: specs/operator-nfr.md missing 'Exhaustion protocol' heading")
	}
}

// TestON048_ExhaustionProtocolHasFourSteps verifies the fixture encodes
// exactly four steps per ON-048.
//
// Spec ref: operator-nfr.md §4.11 ON-048 — "MUST: 1. … 2. … 3. … 4. …"
func TestON048_ExhaustionProtocolHasFourSteps(t *testing.T) {
	t.Parallel()

	const wantSteps = 4
	if len(budgetDefaultsFixtureExhaustionSequence) != wantSteps {
		t.Errorf("ON-048: exhaustion-protocol fixture has %d steps, want %d", len(budgetDefaultsFixtureExhaustionSequence), wantSteps)
	}
}

// TestON048_ExhaustionStepsAreOrdered verifies the fixture step Numbers are
// strictly increasing (1, 2, 3, 4).
//
// Spec ref: operator-nfr.md §4.11 ON-048 — ordered steps.
func TestON048_ExhaustionStepsAreOrdered(t *testing.T) {
	t.Parallel()

	for i := 1; i < len(budgetDefaultsFixtureExhaustionSequence); i++ {
		prev := budgetDefaultsFixtureExhaustionSequence[i-1]
		curr := budgetDefaultsFixtureExhaustionSequence[i]
		if curr.Number != prev.Number+1 {
			t.Errorf("ON-048: exhaustion step %d follows step %d; steps must be contiguous 1..4",
				curr.Number, prev.Number)
		}
	}
}

// TestON048_ExhaustionStepOneEmitsBudgetExhausted verifies that step 1 of
// the exhaustion protocol is the emission of `budget_exhausted`.
//
// Spec ref: operator-nfr.md §4.11 ON-048 step 1 — "Emit `budget_exhausted`."
func TestON048_ExhaustionStepOneEmitsBudgetExhausted(t *testing.T) {
	t.Parallel()

	if len(budgetDefaultsFixtureExhaustionSequence) == 0 {
		t.Fatal("ON-048: exhaustion-protocol fixture is empty")
	}
	step1 := budgetDefaultsFixtureExhaustionSequence[0]
	if step1.Number != 1 {
		t.Errorf("ON-048: first exhaustion step has Number=%d, want 1", step1.Number)
	}
	if !strings.Contains(step1.Summary, "budget_exhausted") {
		t.Errorf("ON-048: exhaustion step 1 Summary %q does not mention 'budget_exhausted'", step1.Summary)
	}
}

// TestON048_ExhaustionStep4EmitsDispatchDeferred verifies that step 4 relates
// to `dispatch_deferred` on machine-ceiling breach.
//
// Spec ref: operator-nfr.md §4.11 ON-048 step 4 — "Emit `dispatch_deferred`
// per §8 code 18 if the exhaustion cascades to a multi-run ceiling breach."
func TestON048_ExhaustionStep4EmitsDispatchDeferred(t *testing.T) {
	t.Parallel()

	if len(budgetDefaultsFixtureExhaustionSequence) < 4 {
		t.Fatal("ON-048: exhaustion-protocol fixture has fewer than 4 steps")
	}
	step4 := budgetDefaultsFixtureExhaustionSequence[3]
	if step4.Number != 4 {
		t.Errorf("ON-048: exhaustion step at index 3 has Number=%d, want 4", step4.Number)
	}
	if !strings.Contains(step4.Summary, "dispatch_deferred") {
		t.Errorf("ON-048: exhaustion step 4 Summary %q does not mention 'dispatch_deferred'", step4.Summary)
	}
}

// TestON048_DefaultExhaustionPolicyIsPauseAndEscalate verifies that the spec
// documents the default exhaustion-routing policy as `pause-and-escalate`.
//
// Spec ref: operator-nfr.md §4.11 ON-048 step 3 — "default is
// `pause-and-escalate`."
func TestON048_DefaultExhaustionPolicyIsPauseAndEscalate(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "pause-and-escalate") {
		t.Error("ON-048: specs/operator-nfr.md missing 'pause-and-escalate' default exhaustion-routing policy in ON-048")
	}
}

// TestON048_SafeBoundaryTerminationPerCategory verifies that the spec names
// safe boundaries per category (post-chunk for tokens, post-iteration for
// iterations, post-step for wall-clock).
//
// Spec ref: operator-nfr.md §4.11 ON-048 step 2 — "post-chunk for token
// budgets; post-iteration for iterations; post-step for wall-clock."
func TestON048_SafeBoundaryTerminationPerCategory(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "post-chunk") {
		t.Error("ON-048: specs/operator-nfr.md missing 'post-chunk' safe boundary for token budgets in ON-048 step 2")
	}
	if !strings.Contains(content, "post-iteration") {
		t.Error("ON-048: specs/operator-nfr.md missing 'post-iteration' safe boundary for iterations in ON-048 step 2")
	}
	if !strings.Contains(content, "post-step") {
		t.Error("ON-048: specs/operator-nfr.md missing 'post-step' safe boundary for wall-clock in ON-048 step 2")
	}
}

// --- ON-049 tests ---

// TestON049_SpecSectionExists verifies that ON-049 (attribution shape)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.11 ON-049.
func TestON049_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-049") {
		t.Error("ON-049: specs/operator-nfr.md does not contain 'ON-049'")
	}
	if !strings.Contains(content, "Attribution shape") {
		t.Error("ON-049: specs/operator-nfr.md missing 'Attribution shape' heading text")
	}
}

// TestON049_AttributionFiveTuplePlusDelegationPath verifies the fixture
// encodes six fields total (5-tuple + delegation_path).
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "(run_id, role, node_id, category,
// amount) plus delegation_path."
func TestON049_AttributionFiveTuplePlusDelegationPath(t *testing.T) {
	t.Parallel()

	const wantFields = 6
	if len(budgetDefaultsFixtureAttributionFields) != wantFields {
		t.Errorf("ON-049: attribution-field fixture has %d entries, want %d (5-tuple + delegation_path)",
			len(budgetDefaultsFixtureAttributionFields), wantFields)
	}
}

// TestON049_AttributionTupleFieldsPresent verifies that all five tuple fields
// plus delegation_path are present in the fixture.
//
// Spec ref: operator-nfr.md §4.11 ON-049.
func TestON049_AttributionTupleFieldsPresent(t *testing.T) {
	t.Parallel()

	required := map[string]bool{
		"run_id":          false,
		"role":            false,
		"node_id":         false,
		"category":        false,
		"amount":          false,
		"delegation_path": false,
	}
	for _, f := range budgetDefaultsFixtureAttributionFields {
		required[f.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-049: attribution field %q missing from fixture", name)
		}
	}
}

// TestON049_DelegationPathIsMarkedForCognitionTagged verifies that
// delegation_path is the only field marked IsDelegation=true.
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "delegation_path … for cost
// incurred by a cognition-tagged step."
func TestON049_DelegationPathIsMarkedForCognitionTagged(t *testing.T) {
	t.Parallel()

	for _, f := range budgetDefaultsFixtureAttributionFields {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			wantDelegation := f.Name == "delegation_path"
			if f.IsDelegation != wantDelegation {
				t.Errorf("ON-049: attribution field %q IsDelegation=%v, want %v",
					f.Name, f.IsDelegation, wantDelegation)
			}
		})
	}
}

// TestON049_AttributionFieldsHaveSpecRefs verifies all attribution fields have
// non-empty SpecRef values.
//
// Spec ref: operator-nfr.md §4.11 ON-049.
func TestON049_AttributionFieldsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, f := range budgetDefaultsFixtureAttributionFields {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			if f.SpecRef == "" {
				t.Errorf("ON-049: attribution field %q has empty SpecRef", f.Name)
			}
		})
	}
}

// TestON049_FourAggregationLevels verifies the fixture encodes exactly four
// aggregation levels.
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "Aggregation levels are: per-run,
// per-role, per-workflow, and per-harmonik-instance."
func TestON049_FourAggregationLevels(t *testing.T) {
	t.Parallel()

	const wantLevels = 4
	if len(budgetDefaultsFixtureAggregationLevels) != wantLevels {
		t.Errorf("ON-049: aggregation-level fixture has %d entries, want %d", len(budgetDefaultsFixtureAggregationLevels), wantLevels)
	}

	required := map[string]bool{
		"per-run":               false,
		"per-role":              false,
		"per-workflow":          false,
		"per-harmonik-instance": false,
	}
	for _, l := range budgetDefaultsFixtureAggregationLevels {
		required[l.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-049: aggregation level %q missing from fixture", name)
		}
	}
}

// TestON049_EmissionSideAttributionOnEveryBudgetAffectingOp verifies that
// the spec requires attribution on every budget-affecting operation (not only
// at aggregation time).
//
// Spec ref: operator-nfr.md §4.11 ON-049 — "the emission side MUST surface
// the attribution on every budget-affecting operation."
func TestON049_EmissionSideAttributionOnEveryBudgetAffectingOp(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "every budget-affecting operation") {
		t.Error("ON-049: specs/operator-nfr.md missing 'every budget-affecting operation' in ON-049 emission-side attribution requirement")
	}
}

// TestON049_AggregationLevelsHaveNonEmptyFields verifies all aggregation
// levels have non-empty Name, IndexOn, and SpecRef.
//
// Spec ref: operator-nfr.md §4.11 ON-049.
func TestON049_AggregationLevelsHaveNonEmptyFields(t *testing.T) {
	t.Parallel()

	for _, l := range budgetDefaultsFixtureAggregationLevels {
		l := l
		t.Run(l.Name, func(t *testing.T) {
			t.Parallel()

			if l.Name == "" {
				t.Error("ON-049: aggregation level has empty Name")
			}
			if l.IndexOn == "" {
				t.Errorf("ON-049: aggregation level %q has empty IndexOn", l.Name)
			}
			if l.SpecRef == "" {
				t.Errorf("ON-049: aggregation level %q has empty SpecRef", l.Name)
			}
		})
	}
}
