package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// readyFixtureJSON returns canonical JSON for a `br ready --format json`
// response with two ready beads. The fixture matches the actual br ready
// flat-array response shape.
func readyFixtureJSON() string {
	return `[` +
		`{"id":"hk-872.13","title":"Implement br ready query (ready-work)","description":"Per BI-013.","status":"open","priority":2,"issue_type":"task","created_at":"2026-04-27T16:39:31.450384Z","created_by":"gb","updated_at":"2026-05-06T18:07:16.631451Z"},` +
		`{"id":"hk-872.32","title":"Allow concurrent br invocations","description":"Per BI-025e.","status":"open","priority":2,"issue_type":"task","created_at":"2026-04-27T16:39:33.289318Z","created_by":"gb","updated_at":"2026-05-06T18:07:15.835758Z"}` +
		`]`
}

// readyFixtureWithLabelsJSON returns a br ready response where one bead carries
// a workflow:<mode> label per BI-009a. Used to verify BI-013 label surfacing.
func readyFixtureWithLabelsJSON() string {
	return `[` +
		`{"id":"hk-7om2q.10","title":"Surface labels on ready-work","description":"Per BI-013.","status":"open","priority":2,"issue_type":"task","labels":["area:brcli","workflow:review-loop"]},` +
		`{"id":"hk-7om2q.11","title":"Exclude needs-attention beads","description":"Per BI-013a.","status":"open","priority":2,"issue_type":"task","labels":[]}` +
		`]`
}

// readyFixtureNeedsAttentionJSON returns a br ready response with two beads:
// one carrying needs-attention (must be excluded) and one without (must be
// included). Used to verify BI-013a exclusion at adapter read time.
func readyFixtureNeedsAttentionJSON() string {
	return `[` +
		`{"id":"hk-7om2q.20","title":"Bead needing attention","description":"Per BI-013a.","status":"open","priority":2,"issue_type":"task","labels":["needs-attention","area:brcli"]},` +
		`{"id":"hk-7om2q.21","title":"Normal ready bead","description":"Per BI-013a.","status":"open","priority":2,"issue_type":"task","labels":["area:brcli"]}` +
		`]`
}

// readyFixtureEmptyJSON returns a br ready response with an empty array
// (no ready beads). This is a valid result and MUST NOT be an error.
func readyFixtureEmptyJSON() string {
	return `[]`
}

// readyFixtureMissingIDJSON returns a br ready response where one element has
// an empty id field. The adapter must reject this with BrSchemaMismatch.
func readyFixtureMissingIDJSON() string {
	return `[{"id":"","title":"Some bead","status":"open","priority":2,"issue_type":"task"}]`
}

func TestReadySuccess(t *testing.T) {
	jsonStr := readyFixtureJSON()
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

	if records[0].BeadID != core.BeadID("hk-872.13") {
		t.Errorf("records[0].BeadID = %q; want %q", records[0].BeadID, "hk-872.13")
	}
	if records[1].BeadID != core.BeadID("hk-872.32") {
		t.Errorf("records[1].BeadID = %q; want %q", records[1].BeadID, "hk-872.32")
	}
}

func TestReadyEmpty(t *testing.T) {
	// Empty array is a valid result — no ready beads. Must NOT be an error.
	jsonStr := readyFixtureEmptyJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: unexpected error for empty result: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d; want 0", len(records))
	}
}

func TestReadyNonZeroExit(t *testing.T) {
	// Non-zero br exit must return ErrBrReadyFailed.
	path := brcliFixtureMockBinary(t, `{"error":{"code":"INTERNAL_ERROR","message":"db locked"}}`, "", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Ready(context.Background())
	if err == nil {
		t.Fatal("expected ErrBrReadyFailed error, got nil")
	}
	if !errors.Is(err, brcli.ErrBrReadyFailed) {
		t.Errorf("errors.Is(err, ErrBrReadyFailed) = false; got %v", err)
	}
}

func TestReadyMalformedJSON(t *testing.T) {
	// Malformed JSON output must classify as BrSchemaMismatch per BI-025b.
	path := brcliFixtureMockBinary(t, `not-json-at-all`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Ready(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	// Per BI-025b: parse failures MUST classify as BrSchemaMismatch.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestReadyMissingIDField(t *testing.T) {
	// An element with an empty id field must be rejected as BrSchemaMismatch.
	// Per BI-025b: missing required field is a schema-level invariant violation.
	jsonStr := readyFixtureMissingIDJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Ready(context.Background())
	if err == nil {
		t.Fatal("expected error for missing id field, got nil")
	}
	// Per BI-025b: missing required field is a schema-level invariant violation.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestReadyExecFailure(t *testing.T) {
	// Non-existent binary triggers exec failure.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Ready(context.Background())
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}

// TestReadyLabelsSurface verifies BI-013: the ready-work response payload MUST
// surface each bead's labels array so the daemon's claim path can extract any
// workflow:<mode> label per BI-009a for per-task workflow-mode overrides.
func TestReadyLabelsSurface(t *testing.T) {
	jsonStr := readyFixtureWithLabelsJSON()
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

	// First bead carries area:brcli and workflow:review-loop labels.
	if records[0].BeadID != core.BeadID("hk-7om2q.10") {
		t.Errorf("records[0].BeadID = %q; want %q", records[0].BeadID, "hk-7om2q.10")
	}
	if len(records[0].Labels) != 2 {
		t.Fatalf("records[0].Labels length = %d; want 2 (got %v)", len(records[0].Labels), records[0].Labels)
	}
	wantLabel := "workflow:review-loop"
	found := false
	for _, l := range records[0].Labels {
		if l == wantLabel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("records[0].Labels does not contain %q; got %v", wantLabel, records[0].Labels)
	}

	// Second bead has an empty labels array — must not be nil but may be empty.
	if records[1].BeadID != core.BeadID("hk-7om2q.11") {
		t.Errorf("records[1].BeadID = %q; want %q", records[1].BeadID, "hk-7om2q.11")
	}
	// Labels may be nil or empty for a bead with no labels — both are acceptable.
	if len(records[1].Labels) != 0 {
		t.Errorf("records[1].Labels = %v; want empty", records[1].Labels)
	}
}

// TestReadyNeedsAttentionExcluded verifies BI-013a: beads carrying the
// needs-attention label MUST be excluded from the dispatchable set even when
// status is open. The adapter applies the exclusion at read time.
func TestReadyNeedsAttentionExcluded(t *testing.T) {
	jsonStr := readyFixtureNeedsAttentionJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: unexpected error: %v", err)
	}

	// Only the bead without needs-attention must be returned.
	if len(records) != 1 {
		t.Fatalf("len(records) = %d; want 1 (needs-attention bead must be excluded)", len(records))
	}
	if records[0].BeadID != core.BeadID("hk-7om2q.21") {
		t.Errorf("records[0].BeadID = %q; want %q", records[0].BeadID, "hk-7om2q.21")
	}
}

// TestReadyNeedsAttentionOnlyExcludesAll verifies BI-013a with a payload that
// contains only needs-attention beads: the result must be an empty (non-nil)
// slice, not an error.
func TestReadyNeedsAttentionOnlyExcludesAll(t *testing.T) {
	// Single bead with needs-attention; should yield empty dispatchable set.
	jsonStr := `[{"id":"hk-7om2q.22","title":"Needs triage","status":"open","priority":2,"issue_type":"task","labels":["needs-attention"]}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.Ready(context.Background())
	if err != nil {
		t.Fatalf("Ready: unexpected error when all beads filtered: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d; want 0 (all beads carry needs-attention)", len(records))
	}
}
