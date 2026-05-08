package operatornfr_test

import (
	"testing"
)

// exitCodeFixtureEntry describes one row in the §8 exit-code taxonomy.
// Fields mirror the taxonomy table columns: Code, Category, EmittedEvent,
// and whether the entry is a startup-path code (consumed by §4.1.ON-003
// startup failure-mode catalog).
type exitCodeFixtureEntry struct {
	Code         int
	Category     string
	EmittedEvent string // empty string where §8 states "—"
	IsStartup    bool   // true if the code appears in the ON-003 startup-failure catalog
}

// exitCodeFixtureTable is the authoritative in-code mirror of the §8 taxonomy
// table in specs/operator-nfr.md. Any change to the spec table MUST be
// reflected here.
//
// Spec ref: operator-nfr.md §8 — "Exit-code taxonomy. Every non-zero code
// maps to one category."
var exitCodeFixtureTable = []exitCodeFixtureEntry{
	{0, "success", "", false},
	{1, "generic-failure", "run_failed", false},
	{2, "queue-format-unsupported", "daemon_startup_failed", true},
	{3, "checkpoint-schema-unsupported", "daemon_startup_failed", true},
	{4, "event-schema-unsupported", "daemon_startup_failed", true},
	{5, "pidfile-locked", "daemon_startup_failed", true},
	{6, "socket-bind-failed", "daemon_startup_failed", true},
	{7, "git-bad-state", "daemon_startup_failed", true},
	{8, "beads-unavailable", "daemon_startup_failed", true},
	{9, "filesystem-unwritable", "daemon_startup_failed", true},
	{10, "disk-full", "daemon_startup_failed", true},
	{11, "drain-timeout-escalated", "operator_stopped", false},
	{12, "rto-hard-ceiling-exceeded", "daemon_degraded", false},
	{13, "upgrade-requires-paused", "operator_upgrade_rejected", false},
	{14, "upgrade-hash-mismatch", "operator_upgrade_rejected", false},
	{15, "upgrade-schema-incompatible", "operator_upgrade_rejected", false},
	{16, "operator-control-invalid-state", "operator_command_rejected", false},
	{17, "multi-daemon-target-missing", "", false},
	{18, "machine-ceiling-exhausted", "dispatch_deferred", false},
	{19, "runtime-panic", "daemon_startup_failed", true},
	{20, "signal-terminated", "", false},
	{21, "drain-step-errored", "daemon_shutdown", false},
	{22, "ntm-unavailable", "daemon_startup_failed", true},
	{23, "orchestrator-agent-unavailable", "daemon_startup_failed", true},
}

// exitCodeFixtureLookup returns the exitCodeFixtureEntry for the given exit
// code, plus a boolean indicating whether the code is present in the taxonomy.
// Code 0 (success) is included for completeness.
func exitCodeFixtureLookup(code int) (exitCodeFixtureEntry, bool) {
	for _, e := range exitCodeFixtureTable {
		if e.Code == code {
			return e, true
		}
	}
	return exitCodeFixtureEntry{}, false
}

// exitCodeFixtureStartupCodes returns all entries from the §8 taxonomy that
// are part of the ON-003 startup failure-mode catalog (IsStartup == true).
func exitCodeFixtureStartupCodes() []exitCodeFixtureEntry {
	var out []exitCodeFixtureEntry
	for _, e := range exitCodeFixtureTable {
		if e.IsStartup {
			out = append(out, e)
		}
	}
	return out
}

// TestON001_ZeroMeansSuccess verifies that exit code 0 maps to the "success"
// category and has no emitted event, satisfying the ON-001 requirement that
// "Zero MUST mean success."
//
// Spec ref: operator-nfr.md §4.1 ON-001 — "Zero MUST mean success."
func TestON001_ZeroMeansSuccess(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeFixtureLookup(0)
	if !ok {
		t.Fatal("ON-001: exit code 0 not found in taxonomy table")
	}
	if e.Category != "success" {
		t.Errorf("ON-001: code 0 category = %q, want %q", e.Category, "success")
	}
	if e.EmittedEvent != "" {
		t.Errorf("ON-001: code 0 emitted_event = %q, want empty (no event on success)", e.EmittedEvent)
	}
}

// TestON001_NonZeroCodesMustHaveCategories verifies that every non-zero exit
// code in the §8 taxonomy table has a non-empty category string.
//
// Spec ref: operator-nfr.md §4.1 ON-001 — "Non-zero codes MUST map one-to-one
// to a failure category declared in the exit-code taxonomy of §8."
func TestON001_NonZeroCodesMustHaveCategories(t *testing.T) {
	t.Parallel()

	for _, e := range exitCodeFixtureTable {
		e := e
		if e.Code == 0 {
			continue
		}
		t.Run(e.Category, func(t *testing.T) {
			t.Parallel()
			if e.Category == "" {
				t.Errorf("ON-001: exit code %d has empty category; every non-zero code MUST have a category", e.Code)
			}
		})
	}
}

// TestON001_CodesAreContiguous verifies that the §8 taxonomy covers all
// integer codes from 0 to 23 with no gaps (per "Codes 1–23 are the MVH
// surface" informative note).
//
// Spec ref: operator-nfr.md §8 — "Codes 1–23 are the MVH surface."
func TestON001_CodesAreContiguous(t *testing.T) {
	t.Parallel()

	const maxMVHCode = 23

	seen := make(map[int]bool, maxMVHCode+1)
	for _, e := range exitCodeFixtureTable {
		seen[e.Code] = true
	}
	for code := 0; code <= maxMVHCode; code++ {
		if !seen[code] {
			t.Errorf("ON-001: exit code %d is missing from the taxonomy table (gap in 0..%d)", code, maxMVHCode)
		}
	}
}

// TestON001_CategoriesAreDistinct verifies that every non-zero category name
// is unique — codes map one-to-one to categories.
//
// Spec ref: operator-nfr.md §4.1 ON-001 — "Non-zero codes MUST map
// one-to-one to a failure category declared in the exit-code taxonomy of §8."
func TestON001_CategoriesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int)
	for _, e := range exitCodeFixtureTable {
		if e.Code == 0 {
			continue
		}
		if prev, ok := seen[e.Category]; ok {
			t.Errorf("ON-001: category %q appears for both code %d and code %d; categories must be one-to-one", e.Category, prev, e.Code)
		}
		seen[e.Category] = e.Code
	}
}

// TestON002_TaxonomyCoversAllMVHCodes verifies that the fixture table — which
// mirrors §8 — contains entries for every code in the declared MVH range
// (0..23), satisfying the ON-002 obligation that the taxonomy names every
// non-zero exit code emitted by any operator command.
//
// Spec ref: operator-nfr.md §4.1 ON-002 — "The spec-draft pass MUST produce a
// normative exit-code taxonomy naming every non-zero exit code emitted by any
// operator command."
func TestON002_TaxonomyCoversAllMVHCodes(t *testing.T) {
	t.Parallel()

	const maxMVHCode = 23

	if len(exitCodeFixtureTable) < maxMVHCode+1 {
		t.Errorf("ON-002: taxonomy table has %d entries, want at least %d (0..%d)", len(exitCodeFixtureTable), maxMVHCode+1, maxMVHCode)
	}

	for code := 1; code <= maxMVHCode; code++ {
		e, ok := exitCodeFixtureLookup(code)
		if !ok {
			t.Errorf("ON-002: code %d missing from taxonomy (required by MVH surface 1..%d)", code, maxMVHCode)
			continue
		}
		if e.Category == "" {
			t.Errorf("ON-002: code %d has empty category string; taxonomy entry is incomplete", code)
		}
	}
}

// TestON002_NegativePath_CodeNotInTaxonomyIsUnknown verifies that a code
// outside the declared taxonomy (e.g., 99) returns not-found from the lookup
// helper. This exercises the negative path: an implementation that emits an
// undeclared code violates ON-001's one-to-one mapping requirement.
//
// Spec ref: operator-nfr.md §4.1 ON-001 — codes MUST map to §8 entries.
func TestON002_NegativePath_CodeNotInTaxonomyIsUnknown(t *testing.T) {
	t.Parallel()

	undeclaredCodes := []int{-1, 24, 99, 255}
	for _, code := range undeclaredCodes {
		code := code
		t.Run("undeclared", func(t *testing.T) {
			t.Parallel()
			_, ok := exitCodeFixtureLookup(code)
			if ok {
				t.Errorf("ON-001 negative-path: code %d was found in the taxonomy but should not be — only 0..23 are declared MVH codes", code)
			}
		})
	}
}

// TestON001_NegativePath_EveryNonZeroCodeIsNonSuccess verifies that no
// non-zero code carries the "success" category.
//
// Spec ref: operator-nfr.md §4.1 ON-001 — zero MUST mean success; non-zero
// codes MUST map to failure categories.
func TestON001_NegativePath_EveryNonZeroCodeIsNonSuccess(t *testing.T) {
	t.Parallel()

	for _, e := range exitCodeFixtureTable {
		e := e
		if e.Code == 0 {
			continue
		}
		t.Run(e.Category, func(t *testing.T) {
			t.Parallel()
			if e.Category == "success" {
				t.Errorf("ON-001 negative-path: exit code %d is non-zero but has category %q; only code 0 may be 'success'", e.Code, e.Category)
			}
		})
	}
}
