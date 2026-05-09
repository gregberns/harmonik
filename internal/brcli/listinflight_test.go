package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// reconciliationFixtureListInFlightJSON returns canonical JSON for a
// `br list --status in_progress --json` response with two in-flight beads.
// The fixture uses dependency_count / dependent_count (no full edge details)
// matching the actual br list response shape.
func reconciliationFixtureListInFlightJSON() string {
	return `{"issues":[` +
		`{"id":"hk-8mup.5","title":"Reconcile on startup","description":"Run reconciliation during daemon init.","status":"in_progress","priority":2,"issue_type":"task","labels":["daemon"],"dependency_count":2,"dependent_count":6},` +
		`{"id":"hk-872.16","title":"Audit log + in-flight status queries","description":"BI-016 implementation.","status":"in_progress","priority":1,"issue_type":"task","labels":[],"dependency_count":0,"dependent_count":0}` +
		`]}`
}

// reconciliationFixtureListInFlightEmptyJSON returns a br list response with
// an empty issues array (no in-flight beads).
func reconciliationFixtureListInFlightEmptyJSON() string {
	return `{"issues":[]}`
}

// reconciliationFixtureListInFlightMissingIssueTypeJSON returns a br list
// response where one issue has an empty issue_type field. The adapter must
// reject this because BeadType would be empty and BeadRecord.Valid() fails.
func reconciliationFixtureListInFlightMissingIssueTypeJSON() string {
	return `{"issues":[` +
		`{"id":"hk-8mup.5","title":"Reconcile on startup","description":"","status":"in_progress","priority":2,"issue_type":"","labels":[],"dependency_count":0,"dependent_count":0}` +
		`]}`
}

// reconciliationFixtureListInFlightMissingTitleJSON returns a br list
// response where one issue has an empty title field. The adapter must
// reject this because Title would be empty and BeadRecord.Valid() fails.
func reconciliationFixtureListInFlightMissingTitleJSON() string {
	return `{"issues":[` +
		`{"id":"hk-8mup.5","title":"","description":"","status":"in_progress","priority":2,"issue_type":"task","labels":[],"dependency_count":0,"dependent_count":0}` +
		`]}`
}

// reconciliationFixtureListInFlightUnknownStatusJSON returns a br list
// response where one issue has a status value not in the declared CoarseStatus
// constants. The adapter MUST reject this via CoarseStatus.UnmarshalText.
func reconciliationFixtureListInFlightUnknownStatusJSON() string {
	return `{"issues":[` +
		`{"id":"hk-8mup.5","title":"Reconcile on startup","description":"","status":"weird_future_status","priority":2,"issue_type":"task","labels":[],"dependency_count":0,"dependent_count":0}` +
		`]}`
}

func TestListInFlightBeadsSuccess(t *testing.T) {
	jsonStr := reconciliationFixtureListInFlightJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.ListInFlightBeads(context.Background())
	if err != nil {
		t.Fatalf("ListInFlightBeads: unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("len(records) = %d; want 2", len(records))
	}

	// Verify first record.
	r0 := records[0]
	if r0.BeadID != "hk-8mup.5" {
		t.Errorf("records[0].BeadID = %q; want %q", r0.BeadID, "hk-8mup.5")
	}
	if r0.Title != "Reconcile on startup" {
		t.Errorf("records[0].Title = %q; want %q", r0.Title, "Reconcile on startup")
	}
	if r0.Description != "Run reconciliation during daemon init." {
		t.Errorf("records[0].Description = %q; want %q", r0.Description, "Run reconciliation during daemon init.")
	}
	if r0.BeadType != "task" {
		t.Errorf("records[0].BeadType = %q; want %q", r0.BeadType, "task")
	}
	if r0.Status != core.CoarseStatusInProgress {
		t.Errorf("records[0].Status = %q; want %q", r0.Status, core.CoarseStatusInProgress)
	}
	if r0.AuditTrailRef != "hk-8mup.5" {
		t.Errorf("records[0].AuditTrailRef = %q; want %q", r0.AuditTrailRef, "hk-8mup.5")
	}

	// Edges MUST be nil — br list does not return full edge details.
	if r0.Edges != nil {
		t.Errorf("records[0].Edges = %v; want nil (br list does not return edge details)", r0.Edges)
	}

	// Verify both records satisfy Valid().
	for i, r := range records {
		if !r.Valid() {
			t.Errorf("records[%d].Valid() = false; want true", i)
		}
	}

	// Verify second record's BeadID.
	r1 := records[1]
	if r1.BeadID != "hk-872.16" {
		t.Errorf("records[1].BeadID = %q; want %q", r1.BeadID, "hk-872.16")
	}
}

func TestListInFlightBeadsEdgesAreNil(t *testing.T) {
	// Explicit test for the Edges=nil carve-out (documented in godoc).
	jsonStr := reconciliationFixtureListInFlightJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.ListInFlightBeads(context.Background())
	if err != nil {
		t.Fatalf("ListInFlightBeads: %v", err)
	}

	for i, r := range records {
		if r.Edges != nil {
			t.Errorf("records[%d].Edges is non-nil; br list does not return edges — callers must call ShowBead or ListDependencies", i)
		}
	}
}

func TestListInFlightBeadsEmpty(t *testing.T) {
	jsonStr := reconciliationFixtureListInFlightEmptyJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	records, err := adapter.ListInFlightBeads(context.Background())
	if err != nil {
		t.Fatalf("ListInFlightBeads: unexpected error for empty result: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("len(records) = %d; want 0", len(records))
	}
}

func TestListInFlightBeadsNonZeroExit(t *testing.T) {
	// br list non-zero exit (general failure) — no ISSUE_NOT_FOUND semantics here.
	path := brcliFixtureMockBinary(t, `{"error":{"code":"INTERNAL_ERROR","message":"db locked"}}`, "", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected ErrBrListFailed error, got nil")
	}
	if !errors.Is(err, brcli.ErrBrListFailed) {
		t.Errorf("errors.Is(err, ErrBrListFailed) = false; got %v", err)
	}
}

func TestListInFlightBeadsMalformedJSON(t *testing.T) {
	path := brcliFixtureMockBinary(t, `not-json-at-all`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	// Per BI-025b: parse failures MUST classify as BrSchemaMismatch.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestListInFlightBeadsMissingIssueType(t *testing.T) {
	// Defensive: issue_type is empty → BeadType would be empty → Valid() fails.
	// Per BI-025b: missing required field is a schema-level invariant; must wrap BrSchemaMismatch.
	jsonStr := reconciliationFixtureListInFlightMissingIssueTypeJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected error for missing issue_type, got nil")
	}
	// Per BI-025b: missing required field is a schema-level invariant violation.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestListInFlightBeadsMissingTitle(t *testing.T) {
	// Defensive: title is empty → Valid() fails.
	// Per BI-025b: missing required field is a schema-level invariant; must wrap BrSchemaMismatch.
	jsonStr := reconciliationFixtureListInFlightMissingTitleJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	// Per BI-025b: missing required field is a schema-level invariant violation.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestListInFlightBeadsUnknownCoarseStatus(t *testing.T) {
	// An issue with a status value not in the declared CoarseStatus constants.
	// CoarseStatus.UnmarshalText rejects it.
	jsonStr := reconciliationFixtureListInFlightUnknownStatusJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown CoarseStatus, got nil")
	}
}

func TestListInFlightBeadsExecFailure(t *testing.T) {
	// Use a non-existent binary to trigger exec failure.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListInFlightBeads(context.Background())
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}
