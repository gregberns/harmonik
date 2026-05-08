// Package core — BI-009 atomic-claim invariant sensor.
//
// Per beads-integration.md §4.3 BI-009: Beads MUST provide atomic-claim
// semantics such that two agents or daemons cannot simultaneously observe the
// same bead as claimed-by-self. Harmonik's dispatch mechanism relies on this
// atomicity; a successful claim returns the bead in `in_progress` to exactly
// one caller.
//
// This file verifies the invariant at two levels:
//
//  1. Spec-content sensor — asserts that beads-integration.md §4.3 BI-009
//     contains the textual statement of the atomicity guarantee. If the spec
//     text is removed or softened, the sensor fails and forces a deliberate
//     review.
//
//  2. Forward-doc sensor — documents the harmonik-side dispatch invariant
//     ("trust Beads's claim verdict; dispatch exactly the caller that receives
//     claimed-by-self=true") for the future implementer of the dispatch loop.
//     The dispatcher is not yet a Go artifact (bootstrap phase), so this
//     sensor skips unconditionally. When dispatch lands, the implementer
//     SHOULD replace or extend this marker with concrete Shape-A assertions.
//
// Requirement-traceable bead: hk-872.9.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAtomicClaimBI009_SpecContainsAtomicityInvariant is a spec-content sensor
// for beads-integration.md §4.3 BI-009.
//
// It reads the spec file from the repo root (located via runtime.Caller so the
// test is working-directory-agnostic) and asserts that:
//
//   - The section heading "BI-009" is present.
//   - The word "atomic" appears within the spec file (the invariant is stated).
//   - The phrase "exactly one caller" is present (the exclusivity guarantee).
//
// Any softening or removal of the BI-009 atomicity text will fail this test,
// forcing a deliberate spec-amendment review before the change can merge.
//
// Spec reference: beads-integration.md §4.3 BI-009 (hk-872.9).
func TestAtomicClaimBI009_SpecContainsAtomicityInvariant(t *testing.T) {
	t.Parallel()

	// Locate beads-integration.md relative to this test file.
	// internal/core/atomicclaim_bi009_test.go → ../../.. → repo root → specs/
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate beads-integration.md")
	}
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", "beads-integration.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // specPath is constructed from runtime.Caller + known relative segments; not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v\nSpec must be present at repo root specs/beads-integration.md (BI-009)", specPath, err)
	}
	content := string(raw)

	// BI-009 section heading must be present.
	if !strings.Contains(content, "BI-009") {
		t.Error("beads-integration.md does not contain \"BI-009\"; " +
			"the atomic-claim invariant section is missing from the spec")
	}

	// The word "atomic" must appear — it is the core term of the guarantee.
	if !strings.Contains(strings.ToLower(content), "atomic") {
		t.Error("beads-integration.md does not contain the word \"atomic\"; " +
			"BI-009 atomicity guarantee appears to have been removed from the spec")
	}

	// "exactly one caller" is the exclusivity statement of BI-009: a successful
	// claim returns the bead in `in_progress` to exactly one caller.
	if !strings.Contains(content, "exactly one caller") {
		t.Error("beads-integration.md does not contain \"exactly one caller\"; " +
			"the BI-009 exclusivity guarantee appears to have been softened or removed")
	}
}

// TestAtomicClaimBI009_DispatchTrustsClaimVerdictForwardDoc is a forward-doc
// sensor for the harmonik-side dispatch invariant derived from BI-009.
//
// Invariant (to be enforced by the dispatch loop once it exists):
//
//	When Beads returns claimed-by-self=true to caller A and
//	claimed-by-self=false to caller B for the same bead, the harmonik
//	dispatcher MUST dispatch only caller A. Caller B MUST NOT be dispatched.
//	Harmonik trusts Beads's atomic claim verdict; it does not impose an
//	additional local mutex or re-check.
//
// This test skips unconditionally because the dispatch loop is not yet a Go
// artifact (we are in the bootstrap phase — records and enums only). When the
// dispatcher lands, the implementer of that bead SHOULD either:
//
//  1. Delete this marker and add concrete Shape-A assertions that inject a
//     mock claim result (claimed-by-self=true / false) and assert only one
//     caller is dispatched, OR
//  2. Extend this marker with those concrete assertions, retaining the BI-009
//     citation and hk-872.9 traceability.
//
// Spec reference: beads-integration.md §4.3 BI-009 (hk-872.9).
func TestAtomicClaimBI009_DispatchTrustsClaimVerdictForwardDoc(t *testing.T) {
	t.Log("BI-009 (hk-872.9): harmonik dispatch MUST trust Beads's atomic claim verdict.")
	t.Log("Invariant: given two concurrent claim attempts against the same bead,")
	t.Log("  Caller A receives claimed-by-self=true  → dispatched.")
	t.Log("  Caller B receives claimed-by-self=false → NOT dispatched.")
	t.Log("Harmonik does not impose a local mutex; it trusts Beads's exclusivity guarantee.")
	t.Log("")
	t.Log("The dispatcher is not yet a Go artifact. When it lands, the implementer SHOULD:")
	t.Log("  1. Delete or extend this marker with concrete Shape-A assertions.")
	t.Log("  2. Inject mock claim results (claimed-by-self=true/false) via a ClaimResult type.")
	t.Log("  3. Assert that only the caller receiving true is dispatched.")
	t.Log("Requirement-traceable bead: hk-872.9.")
	t.SkipNow()
}
