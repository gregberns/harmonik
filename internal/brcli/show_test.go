package brcli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

func showBeadFixtureVerifyBrSchemaMismatch(t *testing.T, err error, context string) {
	t.Helper()
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("%s: errors.Is(err, BrSchemaMismatch) = false; got %v", context, err)
	}
}

func showBeadFixtureValidJSON(id string) string {
	return `[{"id":"` + id + `","title":"Implement bead-detail query","description":"Build ShowBead method on top of Run.","status":"in_progress","issue_type":"task","dependencies":[{"id":"hk-872","title":"Parent bead","status":"open","priority":2,"dependency_type":"parent-child"},{"id":"hk-872.45","title":"Sibling bead","status":"closed","priority":2,"dependency_type":"waits-for"}],"dependents":[{"id":"hk-872.22","title":"Downstream bead","status":"open","priority":2,"dependency_type":"blocks"}],"parent":"hk-872"}]`
}

func showBeadFixtureWithLabelsJSON(id string) string {
	return `[{"id":"` + id + `","title":"Label-bearing bead","description":"","status":"open","issue_type":"task","labels":["area:brcli","workflow:review-loop"],"dependencies":[],"dependents":[],"parent":""}]`
}

func showBeadFixtureNotFoundJSON(searchedID string) string {
	return `{"error":{"code":"ISSUE_NOT_FOUND","message":"Issue not found: ` + searchedID + `","hint":"Check the bead ID and try again.","retryable":false,"context":{"searched_id":"` + searchedID + `"}}}`
}

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

	if len(record.Edges) != 3 {
		t.Fatalf("len(Edges) = %d; want 3", len(record.Edges))
	}

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
	jsonStr := showBeadFixtureValidJSON(id)
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
	showBeadFixtureVerifyBrSchemaMismatch(t, err, "TestShowBeadMalformedJSON")
}

func TestShowBeadUnknownCoarseStatus(t *testing.T) {
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
	jsonStr := `[{"id":"hk-872.15","title":"Some bead","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-872","title":"Parent","status":"open","priority":2,"dependency_type":"related"}],"dependents":[],"parent":"hk-872"}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err != nil {
		t.Fatalf("ShowBead: expected no error for unknown EdgeKind on read surface, got: %v", err)
	}
	if len(record.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(record.Edges))
	}
	edge := record.Edges[0]
	if string(edge.EdgeKind) != "related" {
		t.Errorf("EdgeKind stored = %q, want %q", edge.EdgeKind, "related")
	}
	if edge.EdgeKind.Valid() {
		t.Errorf("EdgeKind.Valid() = true for pass-through value %q; write surface must stay locked", edge.EdgeKind)
	}
}

func TestShowBeadExecFailure(t *testing.T) {
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}

// TestShowBeadLabelsSurface verifies that ShowBead (BI-015) surfaces the
// labels array including workflow:<mode> labels per BI-009a so callers can
// extract per-task workflow-mode overrides.
func TestShowBeadLabelsSurface(t *testing.T) {
	id := core.BeadID("hk-7om2q.10")
	jsonStr := showBeadFixtureWithLabelsJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), id)
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	if len(record.Labels) != 2 {
		t.Fatalf("Labels length = %d; want 2 (got %v)", len(record.Labels), record.Labels)
	}

	wantLabel := "workflow:review-loop"
	found := false
	for _, l := range record.Labels {
		if l == wantLabel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Labels does not contain %q; got %v", wantLabel, record.Labels)
	}
}

// TestShowBeadDesignFieldSurfaced verifies that ShowBead appends the "design"
// field to BeadRecord.Description when non-empty. The design field carries bead
// enrichment (re-impl notes, spec-field-name constraints, BLOCK-iteration
// corrections) that must reach both the implementer and the reviewer via
// agent-task.md / review-target.md. Regression guard for hk-vh1jc.
func TestShowBeadDesignFieldSurfaced(t *testing.T) {
	desc := "Build the lifecycle FSM."
	design := "RE-IMPL NOTE: InvalidStateTransitionError fields MUST be From, To, SessionID string (HC-066) — NOT SessID."
	jsonStr := `[{"id":"hk-q0uba","title":"Port FSM","description":"` + desc + `","design":"` + design + `","status":"open","issue_type":"feature","dependencies":[],"dependents":[],"parent":""}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-q0uba"))
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	if !strings.Contains(record.Description, desc) {
		t.Errorf("Description does not contain original description %q; got %q", desc, record.Description)
	}

	if !strings.Contains(record.Description, design) {
		t.Errorf("Description does not contain design field %q; got %q", design, record.Description)
	}

	if !strings.Contains(record.Description, "## Implementation Notes") {
		t.Errorf("Description does not contain '## Implementation Notes' header; got %q", record.Description)
	}

	descIdx := strings.Index(record.Description, desc)
	designIdx := strings.Index(record.Description, design)
	if descIdx >= designIdx {
		t.Errorf("description text must precede design text; desc at %d, design at %d", descIdx, designIdx)
	}
}

// TestShowBeadDesignFieldAbsent verifies that ShowBead does not alter
// Description when the design field is absent (empty string or missing from JSON).
func TestShowBeadDesignFieldAbsent(t *testing.T) {
	wantDesc := "Build ShowBead method on top of Run."
	jsonStr := `[{"id":"hk-872.15","title":"Implement bead-detail query","description":"` + wantDesc + `","status":"in_progress","issue_type":"task","dependencies":[],"dependents":[],"parent":""}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-872.15"))
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	if record.Description != wantDesc {
		t.Errorf("Description = %q; want %q (design absent — must not alter Description)", record.Description, wantDesc)
	}
}

// TestShowBeadDescriptionFieldName is a regression guard for hk-nmiww:
// br show --format json always emits the bead body under the "description" key,
// NOT "body" (--body is only a CLI alias for br create --description).
// This test ensures the "description" JSON field round-trips through ShowBead
// into BeadRecord.Description so future test authors do not repeat the mistake
// of checking for a "body" key that does not exist in br show JSON output.
func TestShowBeadDescriptionFieldName(t *testing.T) {
	wantBody := "This is the bead body text stored via --body or --description."
	jsonStr := `[{"id":"hk-test-desc","title":"Description field name test","description":"` + wantBody + `","status":"open","issue_type":"task","dependencies":[],"dependents":[],"parent":""}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-test-desc"))
	if err != nil {
		t.Fatalf("ShowBead: unexpected error: %v", err)
	}

	if record.Description != wantBody {
		t.Errorf("Description = %q; want %q (hk-nmiww: check JSON field is \"description\", not \"body\")", record.Description, wantBody)
	}
}
