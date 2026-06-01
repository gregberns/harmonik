package operatornfr_test

// budgetPipelineON045 — spec-level harness for hk-sx9r.64.
//
// Covers: ON-045 (budgets declared, enforced, attributed cross-subsystem).
//
// These are spec-artifact existence and structural-constraint tests. The
// §10.2 sensor obligation for ON-045 is: (a) Budget ControlPoint declared in
// policy YAML is registered in S02 registry without ingest failure; (b)
// over-limit dispatch is DENIED with budget_exhausted emitted; (c) within-limit
// budget_accrual events carry the ON-049 5-tuple; (d) no cross-tenant
// aggregation keys appear.
//
// Spec ref: specs/operator-nfr.md §4.11 ON-045, §10.2.

import (
	"strings"
	"testing"
)

// budgetPipelineON045Obligation models one of the three normative obligations
// declared by ON-045: declared-in-policy, enforced-at-dispatch,
// attributed-in-observability.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — "MUST be declared in policy …
// enforced at dispatch … and attributed in observability."
type budgetPipelineON045Obligation struct {
	Name      string // short identifier
	Keyword   string // text that must appear in the spec under ON-045 to satisfy this obligation
	CrossSpec string // cross-spec citation that anchors this obligation
	SpecRef   string
}

// budgetPipelineON045Obligations is the authoritative fixture encoding the
// three ON-045 normative obligations.
var budgetPipelineON045Obligations = []budgetPipelineON045Obligation{
	{
		Name:      "declared-in-policy",
		Keyword:   "Declared in policy",
		CrossSpec: "control-points.md §4.5",
		SpecRef:   "ON-045 obligation 1",
	},
	{
		Name:      "enforced-at-dispatch",
		Keyword:   "Enforced at dispatch",
		CrossSpec: "control-points.md §4.5 CP-023",
		SpecRef:   "ON-045 obligation 2",
	},
	{
		Name:      "attributed-in-observability",
		Keyword:   "Attributed in observability",
		CrossSpec: "event-model.md §8.4",
		SpecRef:   "ON-045 obligation 3",
	},
}

// budgetPipelineON045EventType models one budget-lifecycle event type that
// ON-045's attribution obligation requires to carry the 5-tuple.
//
// Spec ref: operator-nfr.md §4.11 ON-045 obligation 3 — "budget_accrual,
// budget_warning, or budget_exhausted … carrying the ON-049 attribution shape."
type budgetPipelineON045EventType struct {
	Name    string // event type name
	EV      string // event-model.md §8.4 sub-section
	SpecRef string
}

// budgetPipelineON045AttributionEvents is the authoritative fixture encoding
// the budget-lifecycle event types that must carry the ON-049 5-tuple.
var budgetPipelineON045AttributionEvents = []budgetPipelineON045EventType{
	{"budget_accrual", "event-model.md §8.4.2", "ON-045 attribution + ON-049"},
	{"budget_warning", "event-model.md §8.4.1", "ON-045 attribution + ON-049"},
	{"budget_exhausted", "event-model.md §8.4.3", "ON-045 attribution + ON-049"},
}

// --- spec-existence tests ---

// TestON045_AxesLinePresent verifies that ON-045 carries an Axes: line
// classifying its boundary axes.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — Axes classification per
// [architecture.md §4.1].
func TestON045_AxesLinePresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	// The Axes line must appear within the ON-045 section before ON-046 begins.
	on045Start := strings.Index(content, "ON-045")
	on046Start := strings.Index(content, "ON-046")
	if on045Start < 0 {
		t.Fatal("ON-045: spec section not found")
	}
	if on046Start < 0 {
		t.Fatal("ON-045: ON-046 section not found (needed to bound ON-045 section)")
	}
	on045Section := content[on045Start:on046Start]

	if !strings.Contains(on045Section, "Axes:") {
		t.Error("ON-045: Axes: line missing from ON-045 section; every mechanism requirement with I/O or state mutation must carry Axes")
	}
	if !strings.Contains(on045Section, "llm-freedom=none") {
		t.Error("ON-045: Axes line missing llm-freedom=none; ON-045 is mechanism-tagged")
	}
	if !strings.Contains(on045Section, "io-determinism=deterministic") {
		t.Error("ON-045: Axes line missing io-determinism=deterministic; budget enforcement is deterministic")
	}
	if !strings.Contains(on045Section, "idempotency=non-idempotent") {
		t.Error("ON-045: Axes line missing idempotency=non-idempotent; dispatch denial and attribution emission are non-idempotent")
	}
}

// TestON045_ThreeObligationsNamed verifies the spec names all three
// normative obligations within ON-045 (declared, enforced, attributed).
//
// Spec ref: operator-nfr.md §4.11 ON-045 — three-obligation pipeline.
func TestON045_ThreeObligationsNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	on045Start := strings.Index(content, "ON-045")
	on046Start := strings.Index(content, "ON-046")
	if on045Start < 0 || on046Start < 0 {
		t.Fatal("ON-045: could not bound ON-045 section")
	}
	on045Section := content[on045Start:on046Start]

	for _, ob := range budgetPipelineON045Obligations {
		ob := ob
		t.Run(ob.Name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(on045Section, ob.Keyword) {
				t.Errorf("ON-045: obligation %q — keyword %q not found in ON-045 section; all three obligations must be named", ob.Name, ob.Keyword)
			}
		})
	}
}

// TestON045_ThreeObligationsCount verifies the fixture encodes exactly three
// obligations as declared by ON-045.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — "declared … enforced … attributed."
func TestON045_ThreeObligationsCount(t *testing.T) {
	t.Parallel()

	const wantObligation = 3
	if len(budgetPipelineON045Obligations) != wantObligation {
		t.Errorf("ON-045: obligations fixture has %d entries, want %d (declared/enforced/attributed)", len(budgetPipelineON045Obligations), wantObligation)
	}
}

// TestON045_ObligationsHaveRequiredFields verifies every obligation fixture
// entry has non-empty Name, Keyword, CrossSpec, and SpecRef.
//
// Spec ref: operator-nfr.md §4.11 ON-045.
func TestON045_ObligationsHaveRequiredFields(t *testing.T) {
	t.Parallel()

	for _, ob := range budgetPipelineON045Obligations {
		ob := ob
		t.Run(ob.Name, func(t *testing.T) {
			t.Parallel()

			if ob.Name == "" {
				t.Error("ON-045: obligation has empty Name")
			}
			if ob.Keyword == "" {
				t.Errorf("ON-045: obligation %q has empty Keyword", ob.Name)
			}
			if ob.CrossSpec == "" {
				t.Errorf("ON-045: obligation %q has empty CrossSpec", ob.Name)
			}
			if ob.SpecRef == "" {
				t.Errorf("ON-045: obligation %q has empty SpecRef", ob.Name)
			}
		})
	}
}

// TestON045_ControlPointsCitationPresent verifies the spec cites
// [control-points.md §4.5] within ON-045 — the cross-subsystem anchor for
// the declaration and enforcement obligations.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — "[control-points.md §4.5]" is the
// Budget semantics section.
func TestON045_ControlPointsCitationPresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	on045Start := strings.Index(content, "ON-045")
	on046Start := strings.Index(content, "ON-046")
	if on045Start < 0 || on046Start < 0 {
		t.Fatal("ON-045: could not bound ON-045 section")
	}
	on045Section := content[on045Start:on046Start]

	if !strings.Contains(on045Section, "control-points.md §4.5") {
		t.Error("ON-045: [control-points.md §4.5] citation missing from ON-045; Budget ControlPoint semantics are defined there")
	}
}

// TestON045_PerTenantDeferralNamed verifies the spec acknowledges per-tenant
// cost attribution is out of scope for MVH, citing ON-042.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — "Cost attribution per tenant is
// out of scope for MVH per §4.10.ON-042."
func TestON045_PerTenantDeferralNamed(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	on045Start := strings.Index(content, "ON-045")
	on046Start := strings.Index(content, "ON-046")
	if on045Start < 0 || on046Start < 0 {
		t.Fatal("ON-045: could not bound ON-045 section")
	}
	on045Section := content[on045Start:on046Start]

	if !strings.Contains(on045Section, "ON-042") {
		t.Error("ON-045: ON-042 deferral cross-reference missing; per-tenant cost attribution deferral must cite ON-042")
	}
	if !strings.Contains(on045Section, "tenant") {
		t.Error("ON-045: 'tenant' keyword missing from ON-045 section; per-tenant deferral scope must be stated explicitly")
	}
}

// --- attribution event fixture tests ---

// TestON045_AttributionEventFixtureHasThreeTypes verifies the fixture encodes
// exactly three budget-lifecycle event types as declared in ON-045.
//
// Spec ref: operator-nfr.md §4.11 ON-045 obligation 3 — "budget_accrual,
// budget_warning, or budget_exhausted."
func TestON045_AttributionEventFixtureHasThreeTypes(t *testing.T) {
	t.Parallel()

	const wantTypes = 3
	if len(budgetPipelineON045AttributionEvents) != wantTypes {
		t.Errorf("ON-045: attribution-event fixture has %d entries, want %d (accrual/warning/exhausted)",
			len(budgetPipelineON045AttributionEvents), wantTypes)
	}
}

// TestON045_AttributionEventNamesInSpec verifies that each budget-lifecycle
// event type named in the fixture also appears in the ON-045 section of the
// spec or in the event-model §8.4 citation block.
//
// Spec ref: operator-nfr.md §4.11 ON-045 — attribution events are budget_accrual,
// budget_warning, budget_exhausted per [event-model.md §8.4].
func TestON045_AttributionEventNamesInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	for _, ev := range budgetPipelineON045AttributionEvents {
		ev := ev
		t.Run(ev.Name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(content, ev.Name) {
				t.Errorf("ON-045: attribution event %q not found in specs/operator-nfr.md; budget-lifecycle events must be named in the spec", ev.Name)
			}
		})
	}
}

// TestON045_AttributionEventFixtureFieldsNonEmpty verifies every attribution
// event fixture entry has non-empty Name, EV, and SpecRef.
//
// Spec ref: operator-nfr.md §4.11 ON-045.
func TestON045_AttributionEventFixtureFieldsNonEmpty(t *testing.T) {
	t.Parallel()

	for _, ev := range budgetPipelineON045AttributionEvents {
		ev := ev
		t.Run(ev.Name, func(t *testing.T) {
			t.Parallel()

			if ev.Name == "" {
				t.Error("ON-045: attribution event fixture entry has empty Name")
			}
			if ev.EV == "" {
				t.Errorf("ON-045: attribution event %q has empty EV (event-model citation)", ev.Name)
			}
			if ev.SpecRef == "" {
				t.Errorf("ON-045: attribution event %q has empty SpecRef", ev.Name)
			}
		})
	}
}

// TestON045_Section10_2SensorPresent verifies that the §10.2 test-surface
// obligation for ON-041—ON-046 includes an ON-045 sensor annotation with the
// four sub-assertions from the spec elaboration.
//
// Spec ref: operator-nfr.md §10.2 — ON-041–ON-046 bullet with ON-045 sensor.
func TestON045_Section10_2SensorPresent(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-045 declared-enforced-attributed pipeline sensor") {
		t.Error("ON-045: §10.2 ON-041–ON-046 bullet missing 'ON-045 declared-enforced-attributed pipeline sensor' annotation; §10.2 must name the four sub-assertions")
	}
}

// TestON045_SectionFourElevenExists verifies §4.11 (resource budgets section)
// exists in specs/operator-nfr.md and that ON-045 is within it.
//
// Spec ref: operator-nfr.md §4.11 Resource budgets.
func TestON045_SectionFourElevenExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "### 4.11 Resource budgets") {
		t.Error("ON-045: specs/operator-nfr.md missing '### 4.11 Resource budgets' section header")
	}

	// ON-045 must be subordinate to §4.11 (appear after the header).
	sec411Idx := strings.Index(content, "### 4.11 Resource budgets")
	on045Idx := strings.Index(content, "ON-045")
	if sec411Idx < 0 || on045Idx < 0 {
		t.Fatal("ON-045: cannot verify section ordering — one of the expected markers is missing")
	}
	if on045Idx < sec411Idx {
		t.Error("ON-045: 'ON-045' appears before '### 4.11 Resource budgets' header; it must be subordinate to §4.11")
	}
}
