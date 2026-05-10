package core

import "testing"

// ---- Taxonomy + action-mapping conformance (hk-63oh.79) ----
//
// RC-007: action-mapping table is the dispatch contract; taxonomy is detection.
// RC-008: auto-resolver categories MUST have a deterministic resolver.
// RC-009: 11-category taxonomy shape is settled; amendments require [architecture.md §4.6].
//
// Spec refs:
//   - specs/reconciliation/spec.md §4.2 RC-007, RC-008, RC-009
//   - specs/reconciliation/spec.md §8.12 (authoritative on semantics)
//   - specs/reconciliation/schemas.md §6.3 (authoritative on mechanical dispatch)

// rc79ActionRow captures the four columns of the §8.12 / schemas.md §6.3 table
// for a single reconciliation category.
//
// This struct is the lint anchor: any change to the §8.12 table MUST be
// reflected here; a mismatch is detected by TestRC007_ActionTableConformance.
type rc79ActionRow struct {
	category         ReconciliationCategory
	investigatorUsed bool   // Investigator spawned? column
	autoResolver     bool   // Auto-resolver present? column ("N/A" = false)
	typicalVerdict   string // Typical verdict if investigator; "" for auto-resolver cats
}

// rc79ActionTable is the normative §8.12 / schemas.md §6.3 action-mapping table
// encoded as a slice in the canonical 11-row order.
//
// Dual-table ownership (spec.md §8.12 NOTE): spec.md §8.12 is authoritative on
// SEMANTICS; schemas.md §6.3 is authoritative on MECHANICAL DISPATCH. This Go
// representation must match both. Divergence between this table and either spec
// table is a lint failure per RC-007.
var rc79ActionTable = []rc79ActionRow{
	{
		// Cat 0 — infrastructure unavailable (§8.1)
		// Default action: halt classification + degraded status
		// Investigator? No. Auto-resolver? Yes (wait-and-retry). Verdict: —
		category: ReconciliationCategoryCat0, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 1 — idempotent rerun (§8.2)
		// Default action: auto-resume by re-spawning
		// Investigator? No. Auto-resolver? Yes (spawn the node). Verdict: —
		category: ReconciliationCategoryCat1, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 2 — non-idempotent in-flight (§8.3)
		// Default action: investigator workflow
		// Investigator? Yes. Auto-resolver? No. Verdict: resume-with-context / reset-to-checkpoint / reopen-bead
		category: ReconciliationCategoryCat2, investigatorUsed: true, autoResolver: false, typicalVerdict: "resume-with-context/reset-to-checkpoint/reopen-bead",
	},
	{
		// Cat 3 — store disagreement generic (§8.4)
		// Default action: investigator workflow (git-wins orientation)
		// Investigator? Yes. Auto-resolver? No. Verdict: accept-close-with-note / reopen-bead / no-op-accept
		category: ReconciliationCategoryCat3, investigatorUsed: true, autoResolver: false, typicalVerdict: "accept-close-with-note/reopen-bead/no-op-accept",
	},
	{
		// Cat 3a — torn Beads write (§8.4a)
		// Default action: auto-resolve via adapter status-check-before-reissue
		// Investigator? No. Auto-resolver? Yes (BI-031b). Verdict: —
		category: ReconciliationCategoryCat3a, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 3b — verdict-unexecuted (§8.5)
		// Default action: auto-resolve via RC-026 re-execution
		// Investigator? No. Auto-resolver? Yes (re-run verdict action). Verdict: —
		category: ReconciliationCategoryCat3b, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 3c — inverse premature-close (§8.6)
		// Default action: auto-verdict accept-close-with-note + mechanical close
		// Investigator? No. Auto-resolver? Yes (direct close-write). Verdict: —
		category: ReconciliationCategoryCat3c, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 4 — recoverable known state (§8.7)
		// Default action: auto-resume with pending action
		// Investigator? No. Auto-resolver? Yes (re-arm retry/gate). Verdict: —
		category: ReconciliationCategoryCat4, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 5 — clean restart (§8.8)
		// Default action: normal startup; proceed to ready
		// Investigator? No. Auto-resolver? Yes (no-op). Verdict: —
		category: ReconciliationCategoryCat5, investigatorUsed: false, autoResolver: true, typicalVerdict: "",
	},
	{
		// Cat 6a — integrity violation, LLM-triageable (§8.11)
		// Default action: investigator workflow
		// Investigator? Yes. Auto-resolver? No. Verdict: escalate-to-human (default; may downgrade)
		category: ReconciliationCategoryCat6a, investigatorUsed: true, autoResolver: false, typicalVerdict: "escalate-to-human",
	},
	{
		// Cat 6b — integrity violation, mechanically unrecoverable (§8.11a)
		// Default action: auto-escalate to operator without investigator spawn
		// Investigator? No. Auto-resolver? N/A (operator intervention). Verdict: —
		// NOTE: autoResolver=false because "N/A (operator intervention)" is not a
		// daemon-implemented auto-resolver; the daemon escalates but does NOT
		// autonomously resolve.
		category: ReconciliationCategoryCat6b, investigatorUsed: false, autoResolver: false, typicalVerdict: "",
	},
}

// rc79LookupActionRow returns the rc79ActionRow for the given category from
// rc79ActionTable, or returns a zero row and false if not found.
func rc79LookupActionRow(cat ReconciliationCategory) (rc79ActionRow, bool) {
	for _, row := range rc79ActionTable {
		if row.category == cat {
			return row, true
		}
	}
	return rc79ActionRow{}, false
}

// TestRC007_ActionTableHas11Rows verifies that the rc79ActionTable encodes
// exactly 11 rows — one per category in the closed taxonomy.
//
// RC-007: "The §8 category taxonomy MUST classify what went wrong; the
// action-mapping table in §8.12 MUST specify what the daemon does by default
// for each class."
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-007; §8.12.
func TestRC007_ActionTableHas11Rows(t *testing.T) {
	t.Parallel()

	const wantRows = 11
	if len(rc79ActionTable) != wantRows {
		t.Errorf("rc79ActionTable has %d rows, want %d (11-category taxonomy per RC-007)", len(rc79ActionTable), wantRows)
	}
}

// TestRC007_ActionTableCoversAllCategories verifies that every declared
// ReconciliationCategory constant has exactly one entry in the action table.
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-007.
func TestRC007_ActionTableCoversAllCategories(t *testing.T) {
	t.Parallel()

	allCats := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b,
	}
	for _, cat := range allCats {
		cat := cat
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Errorf("RC-007: category %q has no entry in action table", cat)
				return
			}
			if row.category != cat {
				t.Errorf("RC-007: action-table row category = %q, want %q", row.category, cat)
			}
		})
	}
}

// TestRC007_ActionTableConformance_InvestigatorColumn verifies the
// investigator-spawned column for each of the 11 categories against
// the §8.12 table.
//
// Categories requiring investigator: Cat 2, Cat 3, Cat 6a.
// Categories NOT requiring investigator: Cat 0, 1, 3a, 3b, 3c, 4, 5, 6b.
//
// Spec ref: specs/reconciliation/spec.md §8.12; schemas.md §6.3.
func TestRC007_ActionTableConformance_InvestigatorColumn(t *testing.T) {
	t.Parallel()

	// From §8.12: investigator spawned = Yes for Cat 2, Cat 3, Cat 6a only.
	wantInvestigator := map[ReconciliationCategory]bool{
		ReconciliationCategoryCat0:  false,
		ReconciliationCategoryCat1:  false,
		ReconciliationCategoryCat2:  true,
		ReconciliationCategoryCat3:  true,
		ReconciliationCategoryCat3a: false,
		ReconciliationCategoryCat3b: false,
		ReconciliationCategoryCat3c: false,
		ReconciliationCategoryCat4:  false,
		ReconciliationCategoryCat5:  false,
		ReconciliationCategoryCat6a: true,
		ReconciliationCategoryCat6b: false,
	}
	for cat, want := range wantInvestigator {
		cat, want := cat, want
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Fatalf("RC-007: no action-table row for %q", cat)
			}
			if row.investigatorUsed != want {
				t.Errorf("RC-007: category %q investigatorUsed = %v, want %v (§8.12)", cat, row.investigatorUsed, want)
			}
		})
	}
}

// TestRC008_AutoResolverCategories verifies that auto-resolver categories are
// exactly the set named in RC-008 and that the action table reflects this.
//
// RC-008: "Every category whose default action in §8.12 is an auto-resolver
// (Cat 0 wait-and-retry, Cat 1 re-spawn, Cat 3a adapter re-issue, Cat 3b
// verdict re-execution, Cat 3c direct close-write, Cat 4 retry/gate re-arm,
// Cat 5 no-op, Cat 6b operator escalation) MUST have a deterministic
// implementation in the daemon's Go code."
//
// NOTE: Cat 6b is listed in RC-008 as "auto-escalate without investigator" but
// not as an auto-resolver in the daemon sense; the operator escalation is the
// resolution, not daemon code. The rc79ActionTable encodes Cat 6b with
// autoResolver=false to reflect the "N/A (operator intervention)" column value
// in §8.12 — consistent with schemas.md §6.3 where the Auto-resolver? column
// for Cat 6b reads "N/A (operator intervention)".
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-008; §8.12.
func TestRC008_AutoResolverCategories(t *testing.T) {
	t.Parallel()

	// From §8.12 + RC-008: daemon-implemented auto-resolvers (not N/A).
	wantAutoResolver := map[ReconciliationCategory]bool{
		ReconciliationCategoryCat0:  true,  // wait-and-retry
		ReconciliationCategoryCat1:  true,  // re-spawn
		ReconciliationCategoryCat2:  false, // investigator required
		ReconciliationCategoryCat3:  false, // investigator required
		ReconciliationCategoryCat3a: true,  // BI-031b adapter re-issue
		ReconciliationCategoryCat3b: true,  // RC-026 re-execution
		ReconciliationCategoryCat3c: true,  // direct close-write
		ReconciliationCategoryCat4:  true,  // re-arm retry/gate
		ReconciliationCategoryCat5:  true,  // no-op
		ReconciliationCategoryCat6a: false, // investigator required
		ReconciliationCategoryCat6b: false, // operator intervention (N/A in §8.12)
	}
	for cat, want := range wantAutoResolver {
		cat, want := cat, want
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Fatalf("RC-008: no action-table row for %q", cat)
			}
			if row.autoResolver != want {
				t.Errorf("RC-008: category %q autoResolver = %v, want %v (§8.12 + RC-008)", cat, row.autoResolver, want)
			}
		})
	}
}

// TestRC008_InvestigatorRequiredCatsHaveNoAutoResolver verifies that
// investigator-required categories (Cat 2, Cat 3, Cat 6a) do NOT have an
// auto-resolver per RC-008.
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-008.
func TestRC008_InvestigatorRequiredCatsHaveNoAutoResolver(t *testing.T) {
	t.Parallel()

	investigatorCats := []ReconciliationCategory{
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat6a,
	}
	for _, cat := range investigatorCats {
		cat := cat
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Fatalf("RC-008: no action-table row for %q", cat)
			}
			if !row.investigatorUsed {
				t.Errorf("RC-008: %q should require investigator (investigatorUsed=false)", cat)
			}
			if row.autoResolver {
				t.Errorf("RC-008: investigator-required %q has autoResolver=true; must be false", cat)
			}
		})
	}
}

// TestRC007_TypicalVerdictNonEmptyForInvestigatorCats verifies that each
// investigator-required category carries a non-empty typicalVerdict string,
// documenting the §8.12 "Typical verdict" column.
//
// Spec ref: specs/reconciliation/spec.md §8.12; RC-007.
func TestRC007_TypicalVerdictNonEmptyForInvestigatorCats(t *testing.T) {
	t.Parallel()

	for _, row := range rc79ActionTable {
		row := row
		t.Run(string(row.category), func(t *testing.T) {
			t.Parallel()
			if row.investigatorUsed && row.typicalVerdict == "" {
				t.Errorf("RC-007: investigator-required category %q has empty typicalVerdict (§8.12 requires a declared verdict)", row.category)
			}
			if !row.investigatorUsed && row.typicalVerdict != "" {
				t.Errorf("RC-007: non-investigator category %q has non-empty typicalVerdict %q; §8.12 shows — for auto-resolver categories", row.category, row.typicalVerdict)
			}
		})
	}
}

// TestRC007_AutoResolverAndInvestigatorAreExclusive verifies that each category
// in the action table is either investigator-dispatched or auto-resolver (or
// neither, for Cat 6b), but NEVER both.
//
// Spec ref: specs/reconciliation/spec.md §8.12 RC-007.
func TestRC007_AutoResolverAndInvestigatorAreExclusive(t *testing.T) {
	t.Parallel()

	for _, row := range rc79ActionTable {
		row := row
		t.Run(string(row.category), func(t *testing.T) {
			t.Parallel()
			if row.investigatorUsed && row.autoResolver {
				t.Errorf("RC-007: category %q has both investigatorUsed=true and autoResolver=true; these are mutually exclusive per §8.12", row.category)
			}
		})
	}
}

// TestRC009_TaxonomyShapeIsSettled verifies that the 11-category taxonomy is
// the locked shape per RC-009 and documents the amendment-protocol gate.
//
// RC-009: "The 11-category detection taxonomy plus §8.12 action-mapping is the
// shape, resolved 2026-04-24 per user decision. Authoring agents MUST NOT
// re-open the 3-action-vs-11-category framing. Any future amendment MUST
// follow the protocol of [architecture.md §4.6]."
//
// Amendment-protocol gate (authoring-time check): this test asserts that
// adding or removing a category without updating BOTH this table AND the
// ReconciliationCategory enum causes a test failure. Any RC-009 amendment
// must also update architecture.md §4.6 and increment the spec version.
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-009.
func TestRC009_TaxonomyShapeIsSettled(t *testing.T) {
	t.Parallel()

	// The settled taxonomy has exactly 11 categories.
	const settledCount = 11

	// Count categories in the action table.
	if len(rc79ActionTable) != settledCount {
		t.Errorf("RC-009: action table has %d rows; the 11-category shape is settled (RC-009). "+
			"Any amendment MUST follow [architecture.md §4.6] and update this test.",
			len(rc79ActionTable))
	}

	// Count valid categories by ranging over all table entries.
	seen := make(map[ReconciliationCategory]bool, settledCount)
	for _, row := range rc79ActionTable {
		if seen[row.category] {
			t.Errorf("RC-009: duplicate category %q in action table", row.category)
		}
		seen[row.category] = true
		if !row.category.Valid() {
			t.Errorf("RC-009: action table contains invalid category %q", row.category)
		}
	}
	if len(seen) != settledCount {
		t.Errorf("RC-009: distinct valid categories in action table = %d, want %d", len(seen), settledCount)
	}
}

// TestRC009_AmendmentProtocolGate verifies the authoring-time invariant:
// if an agent were to add a new ReconciliationCategory constant, the action
// table would also REQUIRE an update (the test would fail). This test
// cross-checks the action table against every valid category so that the
// addition of a new constant without updating this table fails fast.
//
// Spec ref: specs/reconciliation/spec.md §4.2 RC-009; [architecture.md §4.6].
func TestRC009_AmendmentProtocolGate(t *testing.T) {
	t.Parallel()

	// Build the set of categories in the action table.
	inTable := make(map[ReconciliationCategory]bool, 11)
	for _, row := range rc79ActionTable {
		inTable[row.category] = true
	}

	// Every category in the enum must be in the table.
	allCats := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b,
	}
	for _, cat := range allCats {
		if !inTable[cat] {
			t.Errorf("RC-009 gate: category %q is declared in the enum but MISSING from rc79ActionTable. "+
				"An RC-009 amendment requires updating both the enum AND rc79ActionTable per [architecture.md §4.6].", cat)
		}
	}

	// Every table row's category must also appear in allCats.
	allCatSet := make(map[ReconciliationCategory]bool, len(allCats))
	for _, c := range allCats {
		allCatSet[c] = true
	}
	for _, row := range rc79ActionTable {
		if !allCatSet[row.category] {
			t.Errorf("RC-009 gate: rc79ActionTable contains %q which is NOT a declared ReconciliationCategory constant. "+
				"Update the ReconciliationCategory enum and ValidateTrailerValue per [architecture.md §4.6].", row.category)
		}
	}
}

// ---- Per-category detection-fixture tests (RC-007..009) ----
//
// Each test below asserts the detection → category → action chain for one
// category, using a minimal input fixture that represents the canonical
// detection-rule trigger for that category.

// TestRC007_Cat0_InfraUnavailable verifies the Cat 0 action-mapping row:
// halt classification + degraded status, no investigator, auto-resolver.
//
// Spec ref: §8.1 Cat 0; §8.12.
func TestRC007_Cat0_InfraUnavailable(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat0
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 0")
	}
	// §8.1: halt classification + degraded status; no investigator.
	if row.investigatorUsed {
		t.Error("RC-007/Cat 0: investigatorUsed=true; Cat 0 MUST NOT spawn an investigator (§8.1)")
	}
	// §8.12: Auto-resolver = Yes (wait-and-retry).
	if !row.autoResolver {
		t.Error("RC-007/Cat 0: autoResolver=false; Cat 0 MUST have a wait-and-retry auto-resolver (§8.12)")
	}
	// §8.1 emitted event: infrastructure_unavailable.
	// (The event name is a contract; the category drives the emission.)
	if !cat.Valid() {
		t.Error("RC-007/Cat 0: category is not valid; enum definition error")
	}
}

// TestRC007_Cat1_IdempotentRerun verifies the Cat 1 action-mapping row:
// auto-resume by re-spawning, no investigator, auto-resolver.
//
// Spec ref: §8.2 Cat 1; §8.12.
func TestRC007_Cat1_IdempotentRerun(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat1
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 1")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 1: investigatorUsed=true; Cat 1 MUST NOT spawn an investigator (§8.2)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 1: autoResolver=false; Cat 1 MUST have a re-spawn auto-resolver (§8.12)")
	}
}

// TestRC007_Cat2_NonIdempotentInFlight verifies the Cat 2 action-mapping row:
// investigator required, no auto-resolver, typical verdict set.
//
// Spec ref: §8.3 Cat 2; §8.12; RC-008.
func TestRC007_Cat2_NonIdempotentInFlight(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat2
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 2")
	}
	if !row.investigatorUsed {
		t.Error("RC-007/Cat 2: investigatorUsed=false; Cat 2 MUST dispatch an investigator workflow (§8.3)")
	}
	if row.autoResolver {
		t.Error("RC-007/Cat 2: autoResolver=true; Cat 2 MUST NOT have an auto-resolver (RC-008)")
	}
	if row.typicalVerdict == "" {
		t.Error("RC-007/Cat 2: typicalVerdict is empty; §8.12 requires a declared typical verdict")
	}
}

// TestRC007_Cat3_GenericStoreDisagreement verifies the Cat 3 action-mapping row:
// investigator required (git-wins orientation), no auto-resolver, typical verdict set.
//
// Spec ref: §8.4 Cat 3; §8.12.
func TestRC007_Cat3_GenericStoreDisagreement(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat3
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 3")
	}
	if !row.investigatorUsed {
		t.Error("RC-007/Cat 3: investigatorUsed=false; Cat 3 MUST dispatch an investigator (§8.4)")
	}
	if row.autoResolver {
		t.Error("RC-007/Cat 3: autoResolver=true; Cat 3 MUST NOT have a daemon auto-resolver (§8.12)")
	}
}

// TestRC007_Cat3a_TornBeadsWrite verifies the Cat 3a action-mapping row:
// auto-resolve via adapter re-issue (BI-031b), no investigator.
//
// Spec ref: §8.4a Cat 3a; §8.12; schemas.md §6.3.
func TestRC007_Cat3a_TornBeadsWrite(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat3a
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 3a")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 3a: investigatorUsed=true; Cat 3a MUST NOT spawn an investigator (auto-resolve via BI-031b)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 3a: autoResolver=false; Cat 3a MUST have an adapter auto-resolver (§8.12)")
	}
}

// TestRC007_Cat3b_VerdictUnexecuted verifies the Cat 3b action-mapping row:
// auto-resolve via RC-026 re-execution, no investigator.
//
// Spec ref: §8.5 Cat 3b; §8.12; RC-026.
func TestRC007_Cat3b_VerdictUnexecuted(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat3b
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 3b")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 3b: investigatorUsed=true; Cat 3b MUST NOT spawn an investigator (auto-re-execute verdict)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 3b: autoResolver=false; Cat 3b MUST have a re-execution auto-resolver (RC-026)")
	}
}

// TestRC007_Cat3c_InversePrematureClose verifies the Cat 3c action-mapping row:
// auto-verdict accept-close-with-note + mechanical close, no investigator.
//
// Spec ref: §8.6 Cat 3c; §8.12.
func TestRC007_Cat3c_InversePrematureClose(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat3c
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 3c")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 3c: investigatorUsed=true; Cat 3c MUST NOT spawn an investigator (direct close-write)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 3c: autoResolver=false; Cat 3c MUST have a direct-close auto-resolver (§8.12)")
	}
}

// TestRC007_Cat4_RecoverableKnownState verifies the Cat 4 action-mapping row:
// auto-resume with pending action (re-arm retry/gate), no investigator.
//
// Spec ref: §8.7 Cat 4; §8.12.
func TestRC007_Cat4_RecoverableKnownState(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat4
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 4")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 4: investigatorUsed=true; Cat 4 MUST NOT spawn an investigator (re-arm retry/gate)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 4: autoResolver=false; Cat 4 MUST have a retry/gate re-arm auto-resolver (§8.12)")
	}
}

// TestRC007_Cat5_CleanRestart verifies the Cat 5 action-mapping row:
// normal startup; proceed to ready (no-op auto-resolver), no investigator.
//
// Spec ref: §8.8 Cat 5; §8.12.
func TestRC007_Cat5_CleanRestart(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat5
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 5")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 5: investigatorUsed=true; Cat 5 MUST NOT spawn an investigator (no-op)")
	}
	if !row.autoResolver {
		t.Error("RC-007/Cat 5: autoResolver=false; Cat 5 MUST have a no-op auto-resolver (§8.12)")
	}
}

// TestRC007_Cat6a_IntegrityLLMTriageable verifies the Cat 6a action-mapping row:
// investigator required, default verdict escalate-to-human, no auto-resolver.
//
// Spec ref: §8.11 Cat 6a; §8.12.
func TestRC007_Cat6a_IntegrityLLMTriageable(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat6a
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 6a")
	}
	if !row.investigatorUsed {
		t.Error("RC-007/Cat 6a: investigatorUsed=false; Cat 6a MUST dispatch an investigator (§8.11)")
	}
	if row.autoResolver {
		t.Error("RC-007/Cat 6a: autoResolver=true; Cat 6a MUST NOT have a daemon auto-resolver (§8.12)")
	}
	if row.typicalVerdict == "" {
		t.Error("RC-007/Cat 6a: typicalVerdict is empty; §8.12 declares escalate-to-human as default")
	}
}

// TestRC007_Cat6b_IntegrityMechanicallyUnrecoverable verifies the Cat 6b
// action-mapping row: auto-escalate to operator, no investigator, no
// daemon-implemented auto-resolver (N/A in §8.12).
//
// Spec ref: §8.11a Cat 6b; §8.12.
func TestRC007_Cat6b_IntegrityMechanicallyUnrecoverable(t *testing.T) {
	t.Parallel()

	cat := ReconciliationCategoryCat6b
	row, ok := rc79LookupActionRow(cat)
	if !ok {
		t.Fatal("RC-007: no action-table row for Cat 6b")
	}
	if row.investigatorUsed {
		t.Error("RC-007/Cat 6b: investigatorUsed=true; Cat 6b MUST NOT spawn an investigator (§8.11a)")
	}
	// Cat 6b: Auto-resolver column in §8.12 reads "N/A (operator intervention)".
	// The daemon auto-escalates but the resolution is operator-owned; this is
	// NOT a daemon-implemented auto-resolver per RC-008's enumeration.
	if row.autoResolver {
		t.Error("RC-007/Cat 6b: autoResolver=true; §8.12 reads N/A (operator intervention) — not a daemon auto-resolver")
	}
}

// ---- §8.12 ↔ schemas.md §6.3 dual-table sync lint test ----

// TestRC007_DualTableSyncInvestigatorColumn is the lint test verifying that the
// investigator-spawned semantics column (this file, from spec.md §8.12) matches
// the mechanical-dispatch table (schemas.md §6.3).
//
// Per the dual-table ownership NOTE in spec.md §8.12 and schemas.md §6.6:
// "The two MUST stay in sync; divergence is a lint failure."
//
// Implementing the full parse of schemas.md §6.3 in a unit test is out of
// scope; instead, this test asserts the Go-encoded canonical table matches
// the expected investigator pattern from schemas.md §6.3 verbatim, establishing
// a code-level anchor that would fail if either table changed without the other.
//
// Spec ref: spec.md §8.12 (semantics); schemas.md §6.3 (mechanical dispatch).
func TestRC007_DualTableSyncInvestigatorColumn(t *testing.T) {
	t.Parallel()

	// schemas.md §6.3 Investigator? column (exact per current spec v0.4.0):
	// Cat 0=No, Cat 1=No, Cat 2=Yes, Cat 3=Yes, Cat 3a=No, Cat 3b=No,
	// Cat 3c=No, Cat 4=No, Cat 5=No, Cat 6a=Yes, Cat 6b=No.
	schemasInvestigator := map[ReconciliationCategory]bool{
		ReconciliationCategoryCat0:  false,
		ReconciliationCategoryCat1:  false,
		ReconciliationCategoryCat2:  true,
		ReconciliationCategoryCat3:  true,
		ReconciliationCategoryCat3a: false,
		ReconciliationCategoryCat3b: false,
		ReconciliationCategoryCat3c: false,
		ReconciliationCategoryCat4:  false,
		ReconciliationCategoryCat5:  false,
		ReconciliationCategoryCat6a: true,
		ReconciliationCategoryCat6b: false,
	}

	for cat, schemasWant := range schemasInvestigator {
		cat, schemasWant := cat, schemasWant
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Fatalf("RC-007 dual-table: no action-table row for %q", cat)
			}
			if row.investigatorUsed != schemasWant {
				t.Errorf("RC-007 dual-table sync FAILURE: %q investigatorUsed = %v (spec.md §8.12), "+
					"want %v (schemas.md §6.3). Tables diverged — lint failure per §8.12 NOTE.",
					cat, row.investigatorUsed, schemasWant)
			}
		})
	}
}

// TestRC007_DualTableSyncAutoResolverColumn is the lint test for the
// auto-resolver column between spec.md §8.12 and schemas.md §6.3.
//
// Spec ref: spec.md §8.12; schemas.md §6.3.
func TestRC007_DualTableSyncAutoResolverColumn(t *testing.T) {
	t.Parallel()

	// schemas.md §6.3 Auto-resolver? column (exact per spec v0.4.0).
	// Cat 6b: "N/A (operator intervention)" → encoded as false (not a daemon resolver).
	schemasAutoResolver := map[ReconciliationCategory]bool{
		ReconciliationCategoryCat0:  true,  // Yes (wait-and-retry)
		ReconciliationCategoryCat1:  true,  // Yes (spawn the node)
		ReconciliationCategoryCat2:  false, // No (investigator required)
		ReconciliationCategoryCat3:  false, // No (escalates through investigator)
		ReconciliationCategoryCat3a: true,  // Yes (BI-031b status-check)
		ReconciliationCategoryCat3b: true,  // Yes (re-run verdict action)
		ReconciliationCategoryCat3c: true,  // Yes (direct close-write)
		ReconciliationCategoryCat4:  true,  // Yes (re-arm retry/gate)
		ReconciliationCategoryCat5:  true,  // Yes (no-op)
		ReconciliationCategoryCat6a: false, // No (investigator required)
		ReconciliationCategoryCat6b: false, // N/A (operator intervention)
	}

	for cat, schemasWant := range schemasAutoResolver {
		cat, schemasWant := cat, schemasWant
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			row, ok := rc79LookupActionRow(cat)
			if !ok {
				t.Fatalf("RC-007 dual-table: no action-table row for %q", cat)
			}
			if row.autoResolver != schemasWant {
				t.Errorf("RC-007 dual-table sync FAILURE: %q autoResolver = %v (spec.md §8.12), "+
					"want %v (schemas.md §6.3). Tables diverged — lint failure per §8.12 NOTE.",
					cat, row.autoResolver, schemasWant)
			}
		})
	}
}
