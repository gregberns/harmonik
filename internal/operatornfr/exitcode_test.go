package operatornfr_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// exitCodeRegistryAll returns all entries from the production ExitCodes
// registry as a slice. This helper avoids direct array-indexing in tests.
func exitCodeRegistryAll() []operatornfr.ExitCodeEntry {
	out := make([]operatornfr.ExitCodeEntry, len(operatornfr.ExitCodes))
	for i := range operatornfr.ExitCodes {
		out[i] = operatornfr.ExitCodes[i]
	}
	return out
}

// exitCodeRegistryLookup wraps LookupExitCode for use in table-driven tests.
func exitCodeRegistryLookup(code int) (operatornfr.ExitCodeEntry, bool) {
	return operatornfr.LookupExitCode(code)
}

// TestExitCodeRegistry_AllCodesPresent verifies that ExitCodes contains exactly
// 24 entries (0..23) and that no code in that range is absent.
//
// Spec ref: specs/operator-nfr.md §8 — "Codes 1–23 are the MVH surface."
func TestExitCodeRegistry_AllCodesPresent(t *testing.T) {
	t.Parallel()

	const maxMVHCode = 23
	const wantCount = maxMVHCode + 1 // 0..23 inclusive

	entries := exitCodeRegistryAll()
	if len(entries) != wantCount {
		t.Errorf("ExitCodes has %d entries, want %d (codes 0..%d)", len(entries), wantCount, maxMVHCode)
	}

	seen := make(map[int]bool, wantCount)
	for _, e := range entries {
		seen[e.Code] = true
	}
	for code := 0; code <= maxMVHCode; code++ {
		if !seen[code] {
			t.Errorf("exit code %d is absent from ExitCodes (gap in 0..%d)", code, maxMVHCode)
		}
	}
}

// TestExitCodeRegistry_MonotonicOrdering verifies that ExitCodes is ordered
// strictly from code 0 to code 23 with no duplicate codes.
//
// Monotonic ordering is not a spec requirement, but it is a design invariant
// of the registry that makes audits and diffs straightforward.
func TestExitCodeRegistry_MonotonicOrdering(t *testing.T) {
	t.Parallel()

	entries := exitCodeRegistryAll()
	for i := 1; i < len(entries); i++ {
		if entries[i].Code <= entries[i-1].Code {
			t.Errorf("ExitCodes[%d].Code = %d is not greater than ExitCodes[%d].Code = %d; registry must be strictly monotonic",
				i, entries[i].Code, i-1, entries[i-1].Code)
		}
	}
}

// TestExitCodeRegistry_RequiredFieldsPopulated verifies that every entry has
// all required fields populated: Symbol, Category, Detection, and Remediation
// are non-empty; Event may be empty (§8 allows "—" for some codes).
//
// Spec ref: specs/operator-nfr.md §8 — taxonomy table columns.
func TestExitCodeRegistry_RequiredFieldsPopulated(t *testing.T) {
	t.Parallel()

	for _, e := range exitCodeRegistryAll() {
		e := e
		t.Run(e.Symbol, func(t *testing.T) {
			t.Parallel()

			if e.Symbol == "" {
				t.Errorf("code %d: Symbol is empty; every entry must have a Symbol", e.Code)
			}
			if e.Category == "" {
				t.Errorf("code %d: Category is empty; every entry must have a Category", e.Code)
			}
			if e.Detection == "" {
				t.Errorf("code %d (%s): Detection is empty; every entry must have a Detection rule", e.Code, e.Symbol)
			}
			if e.Remediation == "" {
				t.Errorf("code %d (%s): Remediation is empty; every entry must have a Remediation pointer", e.Code, e.Symbol)
			}
			// Event is intentionally allowed to be empty (§8 marks some as "—").
		})
	}
}

// TestExitCodeRegistry_ZeroMeansSuccess verifies that code 0 has category
// "success" and no emitted event.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Zero MUST mean success."
func TestExitCodeRegistry_ZeroMeansSuccess(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeRegistryLookup(0)
	if !ok {
		t.Fatal("LookupExitCode(0) returned not-found; code 0 must be in the registry")
	}
	if e.Category != "success" {
		t.Errorf("code 0: Category = %q, want %q", e.Category, "success")
	}
	if e.Event != "" {
		t.Errorf("code 0: Event = %q, want empty (no event on success)", e.Event)
	}
}

// TestExitCodeRegistry_NonZeroCodesAreNonSuccess verifies that no non-zero code
// carries the "success" category.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — zero MUST mean success.
func TestExitCodeRegistry_NonZeroCodesAreNonSuccess(t *testing.T) {
	t.Parallel()

	for _, e := range exitCodeRegistryAll() {
		e := e
		if e.Code == 0 {
			continue
		}
		t.Run(e.Symbol, func(t *testing.T) {
			t.Parallel()
			if e.Category == "success" {
				t.Errorf("code %d (%s) is non-zero but has category %q; only code 0 may be 'success'",
					e.Code, e.Symbol, e.Category)
			}
		})
	}
}

// TestExitCodeRegistry_CategoriesAreDistinct verifies one-to-one mapping
// between codes and categories for all non-zero entries.
//
// Spec ref: specs/operator-nfr.md §4.1 ON-001 — "Non-zero codes MUST map
// one-to-one to a failure category."
func TestExitCodeRegistry_CategoriesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int)
	for _, e := range exitCodeRegistryAll() {
		if e.Code == 0 {
			continue
		}
		if prev, ok := seen[e.Category]; ok {
			t.Errorf("category %q appears for both code %d and code %d; categories must be one-to-one",
				e.Category, prev, e.Code)
		}
		seen[e.Category] = e.Code
	}
}

// TestExitCodeRegistry_LookupUndeclaredReturnsFalse verifies that LookupExitCode
// returns (zero-value, false) for codes outside the MVH range.
//
// Spec ref: specs/operator-nfr.md §8 — "Codes 1–23 are the MVH surface."
func TestExitCodeRegistry_LookupUndeclaredReturnsFalse(t *testing.T) {
	t.Parallel()

	undeclared := []int{-1, 24, 99, 255}
	for _, code := range undeclared {
		code := code
		t.Run("", func(t *testing.T) {
			t.Parallel()
			_, ok := exitCodeRegistryLookup(code)
			if ok {
				t.Errorf("LookupExitCode(%d) returned found=true; only 0..23 are declared MVH codes", code)
			}
		})
	}
}

// TestExitCodeRegistry_SymbolMatchesCategory verifies that every entry's Symbol
// field equals its Category field.  The registry uses the category slug as the
// machine-readable symbol (consistent with the §8 table, where the identifier
// column IS the category slug).
func TestExitCodeRegistry_SymbolMatchesCategory(t *testing.T) {
	t.Parallel()

	for _, e := range exitCodeRegistryAll() {
		e := e
		t.Run(e.Symbol, func(t *testing.T) {
			t.Parallel()
			if e.Symbol != e.Category {
				t.Errorf("code %d: Symbol = %q, Category = %q; they must match (symbol IS the category slug)",
					e.Code, e.Symbol, e.Category)
			}
		})
	}
}

// TestExitCodeRegistry_StartupCodesEmitDaemonStartupFailed verifies that every
// code whose detection rule references startup (i.e., codes 2–10, 19, 22, 23)
// emits "daemon_startup_failed", consistent with the fixture startup catalog.
//
// Spec ref: specs/operator-nfr.md §8 — startup prerequisite failure codes.
func TestExitCodeRegistry_StartupCodesEmitDaemonStartupFailed(t *testing.T) {
	t.Parallel()

	// These are the codes the §8 table shows as emitting daemon_startup_failed.
	startupCodes := map[int]bool{
		2: true, 3: true, 4: true, 5: true, 6: true, 7: true,
		8: true, 9: true, 10: true, 19: true, 22: true, 23: true,
	}

	for code := range startupCodes {
		code := code
		t.Run("", func(t *testing.T) {
			t.Parallel()
			e, ok := exitCodeRegistryLookup(code)
			if !ok {
				t.Errorf("startup code %d not found in registry", code)
				return
			}
			if e.Event != "daemon_startup_failed" {
				t.Errorf("startup code %d (%s): Event = %q, want %q",
					code, e.Symbol, e.Event, "daemon_startup_failed")
			}
		})
	}
}

// TestExitCodeRegistry_FixtureAndRegistryAgree verifies that the production
// ExitCodes registry agrees with the fixture table (exitCodeFixtureTable)
// on Code, Category, and Event for every shared code.  This cross-check ensures
// that bead hk-sx9r.73 (production taxonomy) and bead hk-sx9r.74 (fixture) stay
// in sync.
//
// Spec ref: specs/operator-nfr.md §8 — single authoritative taxonomy.
func TestExitCodeRegistry_FixtureAndRegistryAgree(t *testing.T) {
	t.Parallel()

	for _, fe := range exitCodeFixtureTable {
		fe := fe
		t.Run(fe.Category, func(t *testing.T) {
			t.Parallel()

			pe, ok := exitCodeRegistryLookup(fe.Code)
			if !ok {
				t.Errorf("fixture code %d (%s) not found in production ExitCodes registry", fe.Code, fe.Category)
				return
			}

			if pe.Category != fe.Category {
				t.Errorf("code %d: production Category = %q, fixture Category = %q; must match",
					fe.Code, pe.Category, fe.Category)
			}
			if pe.Event != fe.EmittedEvent {
				t.Errorf("code %d (%s): production Event = %q, fixture EmittedEvent = %q; must match",
					fe.Code, fe.Category, pe.Event, fe.EmittedEvent)
			}
		})
	}
}
