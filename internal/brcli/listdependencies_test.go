package brcli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// listDependenciesFixtureValidJSON returns a JSON array fixture for br dep list
// containing edges in both directions relative to the given bead ID:
//   - outgoing edge: issue_id == id → depends_on_id == "hk-872" (parent-child)
//   - incoming edge: issue_id == "hk-872.22" → depends_on_id == id (blocks)
//   - outgoing edge: issue_id == id → depends_on_id == "hk-872.45" (waits-for)
//
// Only EdgeKind values declared in core.EdgeKind constants are used.
func listDependenciesFixtureValidJSON(id string) string {
	return `[` +
		`{"issue_id":"` + id + `","depends_on_id":"hk-872","type":"parent-child","title":"Parent bead","status":"open","priority":2},` +
		`{"issue_id":"hk-872.22","depends_on_id":"` + id + `","type":"blocks","title":"Downstream bead","status":"open","priority":2},` +
		`{"issue_id":"` + id + `","depends_on_id":"hk-872.45","type":"waits-for","title":"Sibling bead","status":"closed","priority":2}` +
		`]`
}

// listDependenciesFixtureNotFoundJSON returns the br error envelope for ISSUE_NOT_FOUND.
func listDependenciesFixtureNotFoundJSON(searchedID string) string {
	return `{"error":{"code":"ISSUE_NOT_FOUND","message":"Issue not found: ` + searchedID + `","hint":"Check the bead ID and try again.","retryable":false,"context":{"searched_id":"` + searchedID + `"}}}`
}

// listDependenciesFixtureOtherErrorJSON returns a br error envelope for a non-NOT_FOUND error.
func listDependenciesFixtureOtherErrorJSON() string {
	return `{"error":{"code":"INTERNAL_ERROR","message":"something went wrong internally","hint":"","retryable":true,"context":{}}}`
}

func TestListDependenciesSuccess(t *testing.T) {
	id := core.BeadID("hk-872.14")
	jsonStr := listDependenciesFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	edges, err := adapter.ListDependencies(context.Background(), id)
	if err != nil {
		t.Fatalf("ListDependencies: unexpected error: %v", err)
	}

	// Fixture has 3 edges: 2 outgoing + 1 incoming.
	if len(edges) != 3 {
		t.Fatalf("len(edges) = %d; want 3", len(edges))
	}

	// Verify all edges pass Valid().
	for i, e := range edges {
		if !e.Valid() {
			t.Errorf("edges[%d].Valid() = false; want true (from=%q to=%q kind=%q)",
				i, e.FromBeadID, e.ToBeadID, e.EdgeKind)
		}
	}
}

func TestListDependenciesBothDirections(t *testing.T) {
	id := core.BeadID("hk-872.14")
	jsonStr := listDependenciesFixtureValidJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	edges, err := adapter.ListDependencies(context.Background(), id)
	if err != nil {
		t.Fatalf("ListDependencies: unexpected error: %v", err)
	}

	// Verify outgoing edge: issue_id == id → FromBeadID == id, ToBeadID == "hk-872", kind == parent-child.
	var foundOutgoing bool
	for _, e := range edges {
		if e.FromBeadID == id && e.ToBeadID == "hk-872" && e.EdgeKind == core.EdgeKindParentChild {
			foundOutgoing = true
			break
		}
	}
	if !foundOutgoing {
		t.Error("expected outgoing parent-child edge (id -> hk-872) not found; FromBeadID must equal queried id for outgoing edges")
	}

	// Verify incoming edge: depends_on_id == id → ToBeadID == id, FromBeadID == "hk-872.22", kind == blocks.
	var foundIncoming bool
	for _, e := range edges {
		if e.ToBeadID == id && e.FromBeadID == "hk-872.22" && e.EdgeKind == core.EdgeKindBlocks {
			foundIncoming = true
			break
		}
	}
	if !foundIncoming {
		t.Error("expected incoming blocks edge (hk-872.22 -> id) not found; ToBeadID must equal queried id for incoming edges")
	}
}

func TestListDependenciesEmptyArray(t *testing.T) {
	path := brcliFixtureMockBinary(t, `[]`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	edges, err := adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err != nil {
		t.Fatalf("ListDependencies: unexpected error for empty array: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("len(edges) = %d; want 0 for empty response", len(edges))
	}
}

func TestListDependenciesNotFound(t *testing.T) {
	searchedID := "nonexistent"
	jsonStr := listDependenciesFixtureNotFoundJSON(searchedID)
	// br exits 3 on ISSUE_NOT_FOUND.
	path := brcliFixtureMockBinary(t, jsonStr, "", 3)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListDependencies(context.Background(), core.BeadID(searchedID))
	if err == nil {
		t.Fatal("expected ErrBeadNotFound error, got nil")
	}
	if !errors.Is(err, brcli.ErrBeadNotFound) {
		t.Errorf("errors.Is(err, ErrBeadNotFound) = false; got %v", err)
	}
}

func TestListDependenciesOtherNonZeroExit(t *testing.T) {
	jsonStr := listDependenciesFixtureOtherErrorJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err == nil {
		t.Fatal("expected ErrBrDepListFailed error, got nil")
	}
	if !errors.Is(err, brcli.ErrBrDepListFailed) {
		t.Errorf("errors.Is(err, ErrBrDepListFailed) = false; got %v", err)
	}
}

func TestListDependenciesExecFailure(t *testing.T) {
	// Use a non-existent binary to trigger exec failure.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}

func TestListDependenciesMalformedJSON(t *testing.T) {
	path := brcliFixtureMockBinary(t, `not json`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	// Per BI-025b: parse failures MUST classify as BrSchemaMismatch.
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestListDependenciesUnknownEdgeKind(t *testing.T) {
	// "related" is a real Beads dep-type but is not in core.EdgeKind constants.
	// UnmarshalText must reject it. Tracked at hk-872.55.
	jsonStr := `[{"issue_id":"hk-872.14","depends_on_id":"hk-872","type":"related","title":"Parent","status":"open","priority":2}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err == nil {
		t.Fatal("expected error for unknown EdgeKind 'related', got nil")
	}
	if !strings.Contains(err.Error(), "related") {
		t.Errorf("expected error message to contain %q for diagnostics; got: %v", "related", err)
	}
}
