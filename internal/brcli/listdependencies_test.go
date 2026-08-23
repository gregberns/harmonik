package brcli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

func listDependenciesFixtureValidJSON(id string) string {
	return `[` +
		`{"issue_id":"` + id + `","depends_on_id":"hk-872","type":"parent-child","title":"Parent bead","status":"open","priority":2},` +
		`{"issue_id":"hk-872.22","depends_on_id":"` + id + `","type":"blocks","title":"Downstream bead","status":"open","priority":2},` +
		`{"issue_id":"` + id + `","depends_on_id":"hk-872.45","type":"waits-for","title":"Sibling bead","status":"closed","priority":2}` +
		`]`
}

func listDependenciesFixtureNotFoundJSON(searchedID string) string {
	return `{"error":{"code":"ISSUE_NOT_FOUND","message":"Issue not found: ` + searchedID + `","hint":"Check the bead ID and try again.","retryable":false,"context":{"searched_id":"` + searchedID + `"}}}`
}

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

	if len(edges) != 3 {
		t.Fatalf("len(edges) = %d; want 3", len(edges))
	}

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
	if !errors.Is(err, brcli.BrSchemaMismatch) {
		t.Errorf("errors.Is(err, BrSchemaMismatch) = false per BI-025b; got %v", err)
	}
}

func TestListDependenciesUnknownEdgeKind(t *testing.T) {
	jsonStr := `[{"issue_id":"hk-872.14","depends_on_id":"hk-872","type":"related","title":"Parent","status":"open","priority":2}]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	edges, err := adapter.ListDependencies(context.Background(), core.BeadID("hk-872.14"))
	if err != nil {
		t.Fatalf("ListDependencies: unexpected error for unknown EdgeKind 'related': %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges (unknown kind skipped), got %d", len(edges))
	}
}

func TestListDependenciesMixedKnownUnknownEdgeKinds(t *testing.T) {
	jsonStr := `[` +
		`{"issue_id":"hk-a","depends_on_id":"hk-b","type":"blocks","title":"A blocks B","status":"open","priority":2},` +
		`{"issue_id":"hk-a","depends_on_id":"hk-c","type":"related","title":"A related C","status":"open","priority":2}` +
		`]`
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	edges, err := adapter.ListDependencies(context.Background(), core.BeadID("hk-a"))
	if err != nil {
		t.Fatalf("ListDependencies: unexpected error: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge (blocks only, related skipped), got %d", len(edges))
	}
	if edges[0].EdgeKind != core.EdgeKindBlocks {
		t.Errorf("expected EdgeKindBlocks, got %q", edges[0].EdgeKind)
	}
}
