// Package core — EM-002 requirement-traceable sensors.
//
// This file provides named requirement-traceable sensors for the
// deterministic-edge-fields invariant defined in
// execution-model.md §4.1 EM-002:
//
//	An Edge MUST carry from_node, to_node, an optional condition
//	expression, an optional preferred_label, a weight, and an
//	ordering_key. These fields MUST be sufficient to drive the
//	deterministic edge-selection cascade of §4.10 without consulting
//	any other store.
//
// Each test in this file cites EM-002 by name so that
//
//	go test -run EM002 ./internal/core/...
//
// finds all sensors for this requirement.
package core

import (
	"reflect"
	"testing"
)

// requiredEM002Fields enumerates the six field names and their expected Go
// types that EM-002 mandates on the Edge struct. Extensions (Label,
// TraversalCap) are allowed but not required by EM-002.
var requiredEM002Fields = []struct {
	name    string
	typeStr string
}{
	{"FromNode", "core.NodeID"},
	{"ToNode", "core.NodeID"},
	{"Condition", "*string"},
	{"PreferredLabel", "*string"},
	{"Weight", "int"},
	{"OrderingKey", "string"},
}

// TestEdgeEM002_FieldsSufficeForCascade is a reflect-based field-shape sensor
// that asserts the Edge struct exposes all six fields required by
// execution-model.md §4.1 EM-002 with the expected types. The test fails
// loudly on a per-field basis so that renaming (e.g. FromNode → Source)
// is caught as a regression.
//
// Extensions beyond the six EM-002 fields (e.g. Label, TraversalCap) are
// permitted; the test only asserts a superset relationship.
func TestEdgeEM002_FieldsSufficeForCascade(t *testing.T) {
	t.Parallel()

	edgeType := reflect.TypeOf(Edge{})

	// Build a map of field name → type string for O(1) lookup.
	present := make(map[string]string, edgeType.NumField())
	for i := 0; i < edgeType.NumField(); i++ {
		f := edgeType.Field(i)
		present[f.Name] = f.Type.String()
	}

	for _, req := range requiredEM002Fields {
		gotType, ok := present[req.name]
		if !ok {
			t.Errorf("EM-002: Edge is missing required field %q (execution-model.md §4.1 EM-002)", req.name)
			continue
		}
		if gotType != req.typeStr {
			t.Errorf("EM-002: Edge.%s has type %q, want %q (execution-model.md §4.1 EM-002)", req.name, gotType, req.typeStr)
		}
	}

	// Enumerate all struct fields and log them so test output documents the
	// full shape alongside the EM-002 superset assertion.
	if t.Failed() {
		t.Logf("Edge fields present: %v", present)
	}
}

// TestEdgeEM002_NoExternalStoreNeededForSelection is a structural sensor that
// constructs an Edge with all six EM-002 fields populated and asserts that
// Edge.Valid() returns true using only the struct's own data — no DB, git, or
// JSONL lookup is required. This documents the local-answering property of
// EM-002 (execution-model.md §4.1 EM-002).
func TestEdgeEM002_NoExternalStoreNeededForSelection(t *testing.T) {
	t.Parallel()

	cond := "outcome == 'success'"
	label := "preferred-success"
	e := Edge{
		FromNode:       "node-a",
		ToNode:         "node-b",
		Condition:      &cond,
		PreferredLabel: &label,
		Weight:         1,
		OrderingKey:    "a",
	}
	if !e.Valid() {
		t.Errorf("EM-002: fully-populated Edge.Valid() = false, want true (execution-model.md §4.1 EM-002)")
	}
}
