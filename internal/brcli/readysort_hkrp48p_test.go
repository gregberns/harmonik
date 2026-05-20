package brcli_test

// readysort_hkrp48p_test.go — regression test for hk-rp48p.
//
// Symptom: harmonik daemon claimed a P1 stale bead while a higher-priority P0
// bead was simultaneously ready. Root cause: brcli.Ready invoked
// `br ready --format json` without an explicit --sort flag, so br's default
// hybrid sort (which factors bead age into ranking) returned an older P1 ahead
// of a newer P0. The daemon's br-ready fallback path in workloop.go picks
// `readyRecords[0]`, so a non-priority-monotonic Ready response causes the
// daemon to claim the wrong bead.
//
// Fix: Ready pins `--sort priority` so the returned slice is strictly priority-
// ordered (P0 before P1 before P2, ...), making readyRecords[0] always the
// highest-priority ready bead.
//
// Helper prefix: priorityFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-rp48p).

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
)

// TestReadyPinsSortPriority_hkrp48p verifies that Ready forwards
// `--sort priority` to br ready. A regression that drops the flag (or replaces
// the value with `hybrid`/`oldest`) would let br's default hybrid sort
// reorder priorities, which is exactly the failure hk-rp48p captures.
//
// The test uses the spy-args binary to capture the exact argv passed to `br`
// and asserts both that --sort is present and that its value is `priority`.
func TestReadyPinsSortPriority_hkrp48p(t *testing.T) {
	argsFile := priorityFixtureArgsFile(t)
	path := brcliFixtureEchoArgsToFileBinary(t, argsFile)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Ready will produce a parse error because the spy binary writes empty
	// stdout. That is expected and not under test here — we only care about
	// the argv forwarded to br.
	_, _ = adapter.Ready(context.Background())

	//nolint:gosec // G304: argsFile path is constructed from t.TempDir() — test-controlled
	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("reading spy args file: %v", readErr)
	}
	got := string(raw)

	if !strings.Contains(got, "--sort priority") {
		t.Fatalf("Ready did not forward `--sort priority` to br; spy args: %q (hk-rp48p regression)", got)
	}
	// Defence-in-depth: also confirm --format json is still present so this
	// fix does not regress the BI-025b JSON-mode discipline.
	if !strings.Contains(got, "--format json") {
		t.Errorf("Ready dropped --format json while adding --sort; spy args: %q", got)
	}
}

// TestReadyPriorityMonotonic_hkrp48p verifies the contract Ready depends on:
// when br returns a priority-sorted array, the adapter's first element is the
// highest-priority bead. The fixture simulates a `br ready --sort priority`
// response with a P0 bead ahead of a P1 bead (the exact shape that, under
// hybrid sort, was inverted at the time hk-rp48p was filed).
//
// This is a behavioural assertion on the adapter's pass-through ordering —
// the adapter MUST NOT reorder the response — and complements
// TestReadyPinsSortPriority_hkrp48p which assesses the flag is forwarded.
func TestReadyPriorityMonotonic_hkrp48p(t *testing.T) {
	jsonStr := priorityFixturePrioritySortedJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d; want 2", len(records))
	}
	// The P0 bead MUST be first; the workloop's readyRecords[0] pick relies
	// on this. A regression that reorders the slice (or that re-enables
	// hybrid sort upstream) would surface as the wrong BeadID here.
	if string(records[0].BeadID) != "hk-p0-fresh" {
		t.Errorf("records[0].BeadID = %q; want %q (P0 must precede P1 — hk-rp48p)",
			records[0].BeadID, "hk-p0-fresh")
	}
	if string(records[1].BeadID) != "hk-p1-stale" {
		t.Errorf("records[1].BeadID = %q; want %q", records[1].BeadID, "hk-p1-stale")
	}
}

// priorityFixtureArgsFile returns a path inside t.TempDir() suitable for the
// spy binary to record argv into.
func priorityFixtureArgsFile(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/spy-args.txt"
}

// priorityFixturePrioritySortedJSON returns the JSON shape a real `br ready
// --sort priority --format json` invocation would return for the hk-rp48p
// scenario: an older P1 bead and a newer P0 bead. Under hybrid sort, br's
// default policy, the older P1 was returned first (the bug). Under
// priority sort the P0 is returned first regardless of age.
func priorityFixturePrioritySortedJSON() string {
	return `[` +
		`{"id":"hk-p0-fresh","title":"P0 fresh bead","status":"open","priority":0,"issue_type":"bug","created_at":"2026-05-15T20:40:00Z"},` +
		`{"id":"hk-p1-stale","title":"P1 stale bead","status":"open","priority":1,"issue_type":"bug","created_at":"2026-05-15T13:17:00Z"}` +
		`]`
}
