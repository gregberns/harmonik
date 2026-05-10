package brcli_test

// BI-021 authority-direction sensor.
//
// Spec ref: specs/beads-integration.md §4.7 BI-021.
//
// BI-021: "If the daemon's in-memory cache and Beads disagree on a bead's
// title, description, type, or coarse status, Beads MUST win. Harmonik MUST
// reconcile its cache to Beads, not the other way around."
//
// This file implements three enforcement layers:
//
//  1. Spec-content sensor — asserts the BI-021 section of
//     specs/beads-integration.md encodes the authority-direction invariant with
//     canonical phrases. Protects against spec drift that would silently
//     un-anchor the invariant.
//
//  2. Adapter no-cache sensor — asserts that adapter.go godoc contains the
//     BI-005/BI-021 no-parallel-cache discipline. This documents the mechanism
//     by which the adapter enforces the invariant: every ShowBead call
//     unconditionally invokes `br show`; there is no memoisation.
//
//  3. Behavioral test — a caller that holds a stale BeadRecord and then calls
//     ShowBead MUST use the fresh result (Beads wins). The test constructs a
//     stale record, calls ShowBead with a mock `br` that returns a differing
//     value, and asserts the fresh value replaces the stale value.
//
// Requirement-traceable bead: hk-872.22.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// authorityFixtureSpecContent reads specs/beads-integration.md, locates the
// BI-021 anchor, and returns the paragraph that contains it. It fails the test
// if the file is unreadable or the anchor is absent.
func authorityFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("authorityFixtureSpecContent: runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/brcli/<file> → repo root (two levels up from internal/brcli)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("authorityFixtureSpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "BI-021"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf(
			"authorityFixtureSpecContent: spec %s does not contain %q; BI-021 may have been removed or renamed",
			specPath, anchor,
		)
	}

	// Return the paragraph starting at the anchor up to the next section
	// boundary so callers can assert on its contents.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// authorityFixtureAdapterGoContent reads internal/brcli/adapter.go and returns
// its text. It fails the test if the file is unreadable.
func authorityFixtureAdapterGoContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("authorityFixtureAdapterGoContent: runtime.Caller failed")
	}
	adapterPath := filepath.Join(filepath.Dir(thisFile), "adapter.go")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segment, not user input
	raw, err := os.ReadFile(adapterPath)
	if err != nil {
		t.Fatalf("authorityFixtureAdapterGoContent: cannot read %s: %v", adapterPath, err)
	}
	return string(raw)
}

// authorityFixtureStaleRecord returns a BeadRecord with stale field values
// that differ from what the mock br will return. Used to simulate a cached
// value that disagrees with Beads.
func authorityFixtureStaleRecord() core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID("hk-872.22"),
		Title:         "stale-title-from-cache",
		Description:   "stale-description-from-cache",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Edges:         nil,
		AuditTrailRef: "hk-872.22",
	}
}

// authorityFixtureFreshJSON returns a br show JSON response whose fields differ
// from authorityFixtureStaleRecord — simulating a Beads update that invalidates
// the cache.
func authorityFixtureFreshJSON(id string) string {
	return `[{"id":"` + id + `","title":"fresh-title-from-beads","description":"fresh-description-from-beads","status":"in_progress","issue_type":"task","dependencies":[],"dependents":[],"parent":""}]`
}

// ─── Layer 1: Spec-content sensor ─────────────────────────────────────────────

// TestBI021_SpecContainsAuthorityDirectionInvariant verifies that the BI-021
// section of specs/beads-integration.md encodes the authority-direction
// invariant with the required canonical phrases.
//
// Phrases required by the invariant (BI-021, hk-872.22):
//
//   - "Beads MUST win"      — the authority direction; cache must yield to Beads
//   - "reconcile its cache" — the reconciliation direction; cache → Beads, not Beads → cache
//
// A future rename of either phrase in the spec is a breaking change and MUST be
// accompanied by a corresponding update to this test.
//
// Spec ref: specs/beads-integration.md §4.7 BI-021.
func TestBI021_SpecContainsAuthorityDirectionInvariant(t *testing.T) {
	t.Parallel()

	para := authorityFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "Beads MUST win",
			hint:   "BI-021 must declare Beads as the winner on disagreement; renaming this breaks the hk-872.22 invariant",
		},
		{
			phrase: "reconcile its cache to Beads",
			hint:   "BI-021 must state the reconciliation direction as cache-to-Beads (never Beads-to-cache)",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"BI-021 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestBI021_SpecForbidsInverseReconciliation verifies that the BI-021 spec
// paragraph explicitly forbids the inverse direction (reconciling Beads to
// match the cache). This guards against a spec edit that establishes the
// correct direction but omits the prohibition on the wrong direction.
//
// Spec ref: specs/beads-integration.md §4.7 BI-021.
func TestBI021_SpecForbidsInverseReconciliation(t *testing.T) {
	t.Parallel()

	para := authorityFixtureSpecContent(t)

	// "not the other way around" is the canonical phrasing of the no-inverse rule.
	const inversePhrase = "not the other way around"
	if !strings.Contains(para, inversePhrase) {
		t.Errorf(
			"BI-021 spec paragraph does not contain %q; the no-inverse-reconciliation invariant "+
				"(hk-872.22) requires this exact phrase\nParagraph:\n%s",
			inversePhrase, para,
		)
	}
}

// ─── Layer 2: Adapter no-cache sensor ─────────────────────────────────────────

// TestBI021_AdapterNoCacheGodocPresent verifies that adapter.go contains the
// no-parallel-cache discipline in its package-level godoc. The godoc is the
// mechanism documentation for how the adapter enforces BI-021: every ShowBead
// invocation is unconditional; there is no memoisation layer.
//
// The checked phrases are stable identifiers in the godoc:
//
//   - "BI-005"              — the BI-005 citation that anchors the no-cache rule
//   - "invoking `br show` unconditionally" — the implementation mechanism
//   - "stale value MUST be"  — the direction-of-truth enforcement phrase
//
// Spec ref: specs/beads-integration.md §4.3 BI-005 / §4.7 BI-021.
func TestBI021_AdapterNoCacheGodocPresent(t *testing.T) {
	t.Parallel()

	content := authorityFixtureAdapterGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "BI-005",
			hint:   "adapter.go godoc must cite BI-005 (no-parallel-authoritative-cache) per hk-872.22",
		},
		{
			phrase: "invokes `br show` unconditionally",
			hint:   "adapter.go godoc must state that ShowBead invokes br show unconditionally (no memoisation)",
		},
		{
			phrase: "stale value MUST be",
			hint:   "adapter.go godoc must declare the stale-value discard rule (Beads wins on disagreement)",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(content, tc.phrase) {
			t.Errorf(
				"adapter.go does not contain %q — %s",
				tc.phrase, tc.hint,
			)
		}
	}
}

// ─── Layer 3: Behavioral test ─────────────────────────────────────────────────

// TestBI021_FreshBeadsResultReplacesStaleCache verifies the authority-direction
// behavioral invariant: a caller holding a stale BeadRecord MUST discard it and
// use the fresh ShowBead result when the two disagree.
//
// Test scenario:
//  1. Caller constructs a stale record (simulating a cached value).
//  2. Caller invokes ShowBead with a mock `br` that returns differing values.
//  3. Caller discards the stale record and uses the fresh result.
//  4. Test asserts the fresh result fields replace all stale values.
//
// This test does NOT assert that the adapter caches or doesn't cache; the
// no-cache invariant is structural (adapter.go godoc + BI-005). This test
// verifies the caller-facing contract: fresh ShowBead output is authoritative
// and MUST replace any previously observed value on any field (title,
// description, status) per BI-021.
//
// Spec ref: specs/beads-integration.md §4.7 BI-021.
func TestBI021_FreshBeadsResultReplacesStaleCache(t *testing.T) {
	t.Parallel()

	id := core.BeadID("hk-872.22")

	// Step 1: caller holds a stale record.
	stale := authorityFixtureStaleRecord()

	// Step 2: mock br returns values that differ from the stale record.
	freshJSON := authorityFixtureFreshJSON(string(id))
	path := brcliFixtureMockBinary(t, freshJSON, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("authorityFixture: New: %v", err)
	}

	// Step 3: caller invokes ShowBead and gets the fresh result.
	fresh, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("authorityFixture: ShowBead: %v", err)
	}

	// Step 4: assert that fresh fields differ from stale and match Beads output.
	// Beads wins on title.
	if fresh.Title == stale.Title {
		t.Errorf(
			"BI-021 violation: fresh.Title == stale.Title = %q; "+
				"Beads result MUST replace stale cache value (hk-872.22)",
			fresh.Title,
		)
	}
	if fresh.Title != "fresh-title-from-beads" {
		t.Errorf("fresh.Title = %q; want %q", fresh.Title, "fresh-title-from-beads")
	}

	// Beads wins on description.
	if fresh.Description == stale.Description {
		t.Errorf(
			"BI-021 violation: fresh.Description == stale.Description = %q; "+
				"Beads result MUST replace stale cache value (hk-872.22)",
			fresh.Description,
		)
	}
	if fresh.Description != "fresh-description-from-beads" {
		t.Errorf("fresh.Description = %q; want %q", fresh.Description, "fresh-description-from-beads")
	}

	// Beads wins on coarse status.
	if fresh.Status == stale.Status {
		t.Errorf(
			"BI-021 violation: fresh.Status == stale.Status = %q; "+
				"Beads result MUST replace stale cache value (hk-872.22)",
			fresh.Status,
		)
	}
	if fresh.Status != core.CoarseStatusInProgress {
		t.Errorf("fresh.Status = %q; want %q", fresh.Status, core.CoarseStatusInProgress)
	}

	// Fresh record must be structurally valid.
	if !fresh.Valid() {
		t.Error("BI-021: fresh BeadRecord.Valid() = false; Beads result must be well-formed")
	}
}
