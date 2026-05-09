package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// showBeadFixtureVerifyBrSchemaMismatch is a helper that asserts err wraps
// BrSchemaMismatch per BI-025b (parse failure classification).
func showBeadFixtureVerifyBrSchemaMismatch(t *testing.T, err error, context string) {
	t.Helper()
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("%s: errors.Is(err, BrSchemaMismatch) = false; got %v", context, err)
	}
}

// showBeadFixtureValidJSON returns canonical JSON for a br show response
// with the given bead ID. The JSON includes:
//   - one outgoing dependency (parent-child to hk-872)
//   - one outgoing dependency (waits-for to hk-872.45)
//   - one incoming dependent (blocks from hk-872.22)
//
// This covers both dependency (outgoing) and dependents (incoming) edge paths.
// Only EdgeKind values declared in core.EdgeKind constants are used.
func showBeadFixtureValidJSON(id string) string {
	return `[{"id":"` + id + `","title":"Implement bead-detail query","description":"Build ShowBead method on top of Run.","status":"in_progress","issue_type":"task","dependencies":[{"id":"hk-872","title":"Parent bead","status":"open","priority":2,"dependency_type":"parent-child"},{"id":"hk-872.45","title":"Sibling bead","status":"closed","priority":2,"dependency_type":"waits-for"}],"dependents":[{"id":"hk-872.22","title":"Downstream bead","status":"open","priority":2,"dependency_type":"blocks"}],"parent":"hk-872"}]`
}

// showBeadFixtureNotFoundJSON returns the br error envelope for ISSUE_NOT_FOUND.
func showBeadFixtureNotFoundJSON(searchedID string) string {
	return `{"error":{"code":"ISSUE_NOT_FOUND","message":"Issue not found: ` + searchedID + `","hint":"Check the bead ID and try again.","retryable":false,"context":{"searched_id":"` + searchedID + `"}}}`
}

// showBeadFixtureOtherErrorJSON returns a br error envelope for a non-NOT_FOUND error.
func showBeadFixtureOtherErrorJSON() string {
	return `{"error":{"code":"INTERNAL_ERROR","message":"something went wrong internally","hint":"","retryable":true,"context":{}}}`
}

func TestShowBeadSuccess(t *testing.T) {
	id := core.BeadID("hk-872.15")
	jsonStr := showBeadFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	// Verify all fields.
	if record.BeadID != id {
		t.Errorf("BeadID = %q; want %q", record.BeadID, id)
	}
	if record.Title != "Implement bead-detail query" {
		t.Errorf("Title = %q; want %q", record.Title, "Implement bead-detail query")
	}
	if record.Description != "Build ShowBead method on top of Run." {
		t.Errorf("Description = %q; want %q", record.Description, "Build ShowBead method on top of Run.")
	}
	if record.BeadType != "task" {
		t.Errorf("BeadType = %q; want %q", record.BeadType, "task")
	}
	if record.Status != core.CoarseStatusInProgress {
		t.Errorf("Status = %q; want %q", record.Status, core.CoarseStatusInProgress)
	}
	if record.AuditTrailRef != string(id) {
		t.Errorf("AuditTrailRef = %q; want %q", record.AuditTrailRef, string(id))
	}

	// Verify the record passes the Valid() check.
	if !record.Valid() {
		t.Error("record.Valid() = false; want true")
	}
}

func TestShowBeadEdgesOutgoingAndIncoming(t *testing.T) {
	id := core.BeadID("hk-872.15")
	jsonStr := showBeadFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	// Fixture has 2 outgoing (dependencies) + 1 incoming (dependents) = 3 edges total.
	if len(record.Edges) != 3 {
		t.Fatalf("len(Edges) = %d; want 3", len(record.Edges))
	}

	// Verify at least one outgoing edge: FromBeadID == id.
	var hasOutgoing bool
	for _, e := range record.Edges {
		if e.FromBeadID == id {
			hasOutgoing = true
			break
		}
	}
	if !hasOutgoing {
		t.Error("no outgoing edge found (FromBeadID == id); want at least one")
	}

	// Verify at least one incoming edge: ToBeadID == id.
	var hasIncoming bool
	for _, e := range record.Edges {
		if e.ToBeadID == id {
			hasIncoming = true
			break
		}
	}
	if !hasIncoming {
		t.Error("no incoming edge found (ToBeadID == id); want at least one")
	}
}

func TestShowBeadParentNotDoubleAdded(t *testing.T) {
	// The fixture has the parent-child entry in dependencies[] and sets parent="hk-872".
	// We must NOT add a second parent-child edge from the parent field.
	id := core.BeadID("hk-872.15")
	jsonStr := showBeadFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	var parentChildCount int
	for _, e := range record.Edges {
		if e.EdgeKind == core.EdgeKindParentChild {
			parentChildCount++
		}
	}
	if parentChildCount != 1 {
		t.Errorf("parent-child edge count = %d; want exactly 1 (parent field must not be double-added)", parentChildCount)
	}
}

func TestShowBeadEdgeDirectionsCorrect(t *testing.T) {
	id := core.BeadID("hk-872.15")
	jsonStr := showBeadFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	// Outgoing (dependency): From=id, To=hk-872, kind=parent-child.
	var foundOutgoing bool
	for _, e := range record.Edges {
		if e.FromBeadID == id && e.ToBeadID == "hk-872" && e.EdgeKind == core.EdgeKindParentChild {
			foundOutgoing = true
			break
		}
	}
	if !foundOutgoing {
		t.Error("expected outgoing parent-child edge (id -> hk-872) not found")
	}

	// Incoming (dependent): From=hk-872.22, To=id, kind=blocks.
	var foundIncoming bool
	for _, e := range record.Edges {
		if e.FromBeadID == "hk-872.22" && e.ToBeadID == id && e.EdgeKind == core.EdgeKindBlocks {
			foundIncoming = true
			break
		}
	}
	if !foundIncoming {
		t.Error("expected incoming blocks edge (hk-872.22 -> id) not found")
	}
}

func TestShowBeadNotFound(t *testing.T) {
	searchedID := "nonexistent-bead"
	jsonStr := showBeadFixtureNotFoundJSON(searchedID)
	// br exits 3 on ISSUE_NOT_FOUND.
	path := brcliFixtureMockBinary(t, jsonStr, "", 3)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID(searchedID))
	if err == nil {
		t.Fatal("expected ErrBeadNotFound error, got nil")
	}
	if !errors.Is(err, brcli.ErrBeadNotFound) {
		t.Errorf("errors.Is(err, ErrBeadNotFound) = false; got %v", err)
	}
}

func TestShowBeadOtherNonZeroExit(t *testing.T) {
	jsonStr := showBeadFixtureOtherErrorJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected ErrBrShowFailed error, got nil")
	}
	if !errors.Is(err, brcli.ErrBrShowFailed) {
		t.Errorf("errors.Is(err, ErrBrShowFailed) = false; got %v", err)
	}
}

func TestShowBeadEmptyArray(t *testing.T) {
	path := brcliFixtureMockBinary(t, `[]`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for empty JSON array, got nil")
	}
	showBeadFixtureVerifyBrSchemaMismatch(t, err, "TestShowBeadEmptyArray")
}

func TestShowBeadMultiElementArray(t *testing.T) {
	id := "hk-872.15"
	// Two elements — should be rejected.
	jsonStr := showBeadFixtureValidJSON(id)
	// Insert a second element by replacing the closing bracket.
	twoElements := jsonStr[:len(jsonStr)-1] + `,` + jsonStr[1:]
	path := brcliFixtureMockBinary(t, twoElements, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID(id))
	if err == nil {
		t.Fatal("expected error for multi-element JSON array, got nil")
	}
	showBeadFixtureVerifyBrSchemaMismatch(t, err, "TestShowBeadMultiElementArray")
}

func TestShowBeadMalformedJSON(t *testing.T) {
	path := brcliFixtureMockBinary(t, `not-json-at-all`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	// Per BI-025b: parse failures MUST classify as BrSchemaMismatch.
	showBeadFixtureVerifyBrSchemaMismatch(t, err, "TestShowBeadMalformedJSON")
}

func TestShowBeadUnknownCoarseStatus(t *testing.T) {
	// Replace "in_progress" with an unknown status value.
	jsonStr := `[{"id":"hk-872.15","title":"Some bead","description":"","status":"weirdstatus","issue_type":"task","dependencies":[],"dependents":[],"parent":""}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for unknown CoarseStatus, got nil")
	}
}

func TestShowBeadUnknownEdgeKind(t *testing.T) {
	// An edge with an unknown dependency_type value.
	jsonStr := `[{"id":"hk-872.15","title":"Some bead","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-872","title":"Parent","status":"open","priority":2,"dependency_type":"unknown-edge-kind"}],"dependents":[],"parent":"hk-872"}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for unknown EdgeKind, got nil")
	}
}

func TestShowBeadExecFailure(t *testing.T) {
	// Use a non-existent binary to trigger exec failure.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}
