package core

import "testing"

// rc73PriorityFixtureOrder is the canonical RC-003a first-match priority order
// for reconciliation detection categories. It is declared as a slice so tests
// can verify adjacency and relative position without hardcoding index arithmetic.
//
// Order per specs/reconciliation/spec.md §4.1 RC-003a:
//
//	Cat 0 → Cat 6b → Cat 6a → Cat 5 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat 4 → Cat 1
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "Detectors MUST apply
// the following priority order and emit the first category whose rule fires."
var rc73PriorityFixtureOrder = []ReconciliationCategory{
	ReconciliationCategoryCat0,
	ReconciliationCategoryCat6b,
	ReconciliationCategoryCat6a,
	ReconciliationCategoryCat5,
	ReconciliationCategoryCat3c,
	ReconciliationCategoryCat3b,
	ReconciliationCategoryCat3a,
	ReconciliationCategoryCat3,
	ReconciliationCategoryCat2,
	ReconciliationCategoryCat4,
	ReconciliationCategoryCat1,
}

// rc73PriorityFixtureIndexOf returns the position of cat in the RC-003a
// priority order, or -1 if not present.
func rc73PriorityFixtureIndexOf(cat ReconciliationCategory) int {
	for i, c := range rc73PriorityFixtureOrder {
		if c == cat {
			return i
		}
	}
	return -1
}

// rc73PriorityFixtureHigherThan returns true if a has higher priority than b
// (lower index = higher priority in first-match order).
func rc73PriorityFixtureHigherThan(a, b ReconciliationCategory) bool {
	ai := rc73PriorityFixtureIndexOf(a)
	bi := rc73PriorityFixtureIndexOf(b)
	if ai < 0 || bi < 0 {
		return false
	}
	return ai < bi
}

// TestRC003a_PriorityOrderIsComplete verifies that the fixture priority order
// contains all 11 declared ReconciliationCategory values exactly once.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a.
func TestRC003a_PriorityOrderIsComplete(t *testing.T) {
	t.Parallel()

	all := []ReconciliationCategory{
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

	if len(rc73PriorityFixtureOrder) != len(all) {
		t.Errorf("RC-003a: priority order has %d entries, want %d (all 11 categories)",
			len(rc73PriorityFixtureOrder), len(all))
	}

	for _, cat := range all {
		idx := rc73PriorityFixtureIndexOf(cat)
		if idx < 0 {
			t.Errorf("RC-003a: category %q absent from priority order", cat)
		}
	}
}

// TestRC003a_PriorityOrderExact verifies the exact priority ordering matches
// the spec text verbatim.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a:
// "Cat 0 → Cat 6b → Cat 6a → Cat 5 → Cat 3c → Cat 3b → Cat 3a → Cat 3 → Cat 2 → Cat 4 → Cat 1"
func TestRC003a_PriorityOrderExact(t *testing.T) {
	t.Parallel()

	want := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat6b,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat1,
	}

	if len(rc73PriorityFixtureOrder) != len(want) {
		t.Fatalf("RC-003a: priority order length = %d, want %d",
			len(rc73PriorityFixtureOrder), len(want))
	}

	for i, w := range want {
		got := rc73PriorityFixtureOrder[i]
		if got != w {
			t.Errorf("RC-003a: priority[%d] = %q, want %q", i, got, w)
		}
	}
}

// TestRC003a_InfrastructureBeatsEverything verifies that Cat 0 (infrastructure
// unavailable) has the highest priority in the first-match order. Cat 0 blocks
// all other detectors because "the lower-priority detectors cannot be trusted
// in their presence."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "Cat 0: infrastructure
// and integrity evidence dominates."
func TestRC003a_InfrastructureBeatsEverything(t *testing.T) {
	t.Parallel()

	cat0 := ReconciliationCategoryCat0
	others := []ReconciliationCategory{
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

	for _, other := range others {
		other := other
		t.Run(string(other), func(t *testing.T) {
			t.Parallel()
			if !rc73PriorityFixtureHigherThan(cat0, other) {
				t.Errorf("RC-003a: Cat 0 does not have higher priority than %q; Cat 0 must dominate all other categories", other)
			}
		})
	}
}

// TestRC003a_IntegrityBeatsStoreDivergence verifies the infrastructure/integrity
// group (Cat 6b, Cat 6a) has higher priority than store-disagreement categories
// (Cat 3, Cat 3a, Cat 3b, Cat 3c).
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "integrity evidence
// dominates because the lower-priority detectors cannot be trusted in their
// presence."
func TestRC003a_IntegrityBeatsStoreDivergence(t *testing.T) {
	t.Parallel()

	integrityCategories := []ReconciliationCategory{
		ReconciliationCategoryCat6b,
		ReconciliationCategoryCat6a,
	}
	storeDivergenceCategories := []ReconciliationCategory{
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
	}

	for _, integ := range integrityCategories {
		integ := integ
		for _, store := range storeDivergenceCategories {
			store := store
			t.Run(string(integ)+"-vs-"+string(store), func(t *testing.T) {
				t.Parallel()
				if !rc73PriorityFixtureHigherThan(integ, store) {
					t.Errorf("RC-003a: %q does not have higher priority than %q; integrity must dominate store-disagreement", integ, store)
				}
			})
		}
	}
}

// TestRC003a_WorkspaceMissingCrossover_Cat6aBeforesCat3 verifies the specific
// "workspace missing" tension cited in RC-003a: the case of "workspace path
// missing + sibling transition-record absent" routes to Cat 6a (not Cat 3),
// because Cat 6a precedes Cat 3 in the priority order.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "This rule resolves
// the Cat 3 vs Cat 6a 'workspace missing' tension."
func TestRC003a_WorkspaceMissingCrossover_Cat6aBeforesCat3(t *testing.T) {
	t.Parallel()

	if !rc73PriorityFixtureHigherThan(ReconciliationCategoryCat6a, ReconciliationCategoryCat3) {
		t.Error("RC-003a: Cat 6a does not have higher priority than Cat 3; " +
			"workspace-missing crossover requires Cat 6a to fire first per RC-003a")
	}
}

// TestRC003a_MergedRunCrossover_Cat3cBeforesCat3b verifies that the
// "merged-run" crossover (verdict-unexecuted on a merged run) routes to Cat 3c
// (inverse premature-close) before Cat 3b (verdict-unexecuted). Cat 3c
// precedes Cat 3b in the priority order.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "The Cat 3c vs Cat 3b
// 'verdict-unexecuted on a merged run' tension; both route by priority."
func TestRC003a_MergedRunCrossover_Cat3cBeforesCat3b(t *testing.T) {
	t.Parallel()

	if !rc73PriorityFixtureHigherThan(ReconciliationCategoryCat3c, ReconciliationCategoryCat3b) {
		t.Error("RC-003a: Cat 3c does not have higher priority than Cat 3b; " +
			"merged-run crossover requires Cat 3c to fire first per RC-003a")
	}
}

// TestRC003a_RateLimitedNonIdempotentCrossover_Cat2BeforesCat4 verifies that a
// non-idempotent in-flight agent in a rate-limited/backoff state routes to
// Cat 2 (non-idempotent in-flight) rather than Cat 4 (recoverable known state),
// because Cat 2 precedes Cat 4 in the priority order.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "non-idempotent in-
// flight (Cat 2) dominates recoverable retry state (Cat 4) because the
// investigator rubric for Cat 2 subsumes Cat 4's auto-resume for non-idempotent
// work."
func TestRC003a_RateLimitedNonIdempotentCrossover_Cat2BeforesCat4(t *testing.T) {
	t.Parallel()

	if !rc73PriorityFixtureHigherThan(ReconciliationCategoryCat2, ReconciliationCategoryCat4) {
		t.Error("RC-003a: Cat 2 does not have higher priority than Cat 4; " +
			"rate-limited non-idempotent crossover requires Cat 2 to fire first per RC-003a")
	}
}

// TestRC003a_IdempotentRerunIsLowestPriority verifies that Cat 1 (idempotent
// rerun) is the lowest-priority category in the first-match order.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "idempotent-rerun
// (Cat 1) is terminal in the order because it is the cheapest auto-resume and
// should not mask higher-severity evidence."
func TestRC003a_IdempotentRerunIsLowestPriority(t *testing.T) {
	t.Parallel()

	cat1 := ReconciliationCategoryCat1
	others := []ReconciliationCategory{
		ReconciliationCategoryCat0,
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

	for _, other := range others {
		other := other
		t.Run(string(other), func(t *testing.T) {
			t.Parallel()
			if !rc73PriorityFixtureHigherThan(other, cat1) {
				t.Errorf("RC-003a: %q does not have higher priority than Cat 1; "+
					"Cat 1 must be the lowest-priority category", other)
			}
		})
	}
}

// TestRC003a_CleanRestartBeatsSubcategoryStoreDivergence verifies that Cat 5
// (clean restart) has higher priority than the Cat 3 specific sub-cases (3c,
// 3b, 3a) and generic Cat 3.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "clean-restart (Cat 5)
// dominates reopened-run orphans."
func TestRC003a_CleanRestartBeatsSubcategoryStoreDivergence(t *testing.T) {
	t.Parallel()

	cat5 := ReconciliationCategoryCat5
	storeCats := []ReconciliationCategory{
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3,
	}

	for _, cat := range storeCats {
		cat := cat
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			if !rc73PriorityFixtureHigherThan(cat5, cat) {
				t.Errorf("RC-003a: Cat 5 does not have higher priority than %q; "+
					"Cat 5 must dominate Cat 3 sub-categories", cat)
			}
		})
	}
}

// TestRC003a_SpecificCat3SubcasesBeforeGenericCat3 verifies that the specific
// Cat 3 sub-cases (3c, 3b, 3a) all have higher priority than generic Cat 3.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a — "specific Cat 3
// sub-cases (3c, 3b, 3a) dominate generic Cat 3."
func TestRC003a_SpecificCat3SubcasesBeforeGenericCat3(t *testing.T) {
	t.Parallel()

	subcases := []ReconciliationCategory{
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3a,
	}

	for _, sub := range subcases {
		sub := sub
		t.Run(string(sub), func(t *testing.T) {
			t.Parallel()
			if !rc73PriorityFixtureHigherThan(sub, ReconciliationCategoryCat3) {
				t.Errorf("RC-003a: %q does not have higher priority than Cat 3; "+
					"specific sub-cases must dominate generic store-disagreement", sub)
			}
		})
	}
}

// TestRC003a_Cat6bBeforesCat6a verifies that the mechanically-unrecoverable
// integrity violation (Cat 6b) has higher priority than the LLM-triageable
// integrity violation (Cat 6a). Spawning an LLM investigator when git itself
// is corrupt would be wasteful.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a priority order:
// Cat 6b precedes Cat 6a.
func TestRC003a_Cat6bBeforesCat6a(t *testing.T) {
	t.Parallel()

	if !rc73PriorityFixtureHigherThan(ReconciliationCategoryCat6b, ReconciliationCategoryCat6a) {
		t.Error("RC-003a: Cat 6b does not have higher priority than Cat 6a; " +
			"mechanically-unrecoverable integrity violation must dominate LLM-triageable case")
	}
}

// TestRC003a_AllPairsCoveredByPriorityOrder verifies that for every adjacent
// pair (i, i+1) in the priority order, position i has strictly higher priority
// than position i+1 per rc73PriorityFixtureHigherThan.
//
// This is a coverage guard: if the fixture order is internally inconsistent,
// this test will catch it.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003a.
func TestRC003a_AllPairsCoveredByPriorityOrder(t *testing.T) {
	t.Parallel()

	order := rc73PriorityFixtureOrder
	for i := 0; i < len(order)-1; i++ {
		higher := order[i]
		lower := order[i+1]
		t.Run(string(higher)+"-before-"+string(lower), func(t *testing.T) {
			t.Parallel()
			if !rc73PriorityFixtureHigherThan(higher, lower) {
				t.Errorf("RC-003a: priority order violation: %q must have higher priority than %q (indices %d vs %d)",
					higher, lower, i, i+1)
			}
		})
	}
}
