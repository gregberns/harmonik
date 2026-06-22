package cli

// submit_default_workflow_test.go — wire-shape tests for the submit default
// workflow_mode behaviour (hk-y3o51).
//
// Verifies that beadsToQueueDoc correctly omits workflow_mode when passed ""
// (so the daemon default applies) and stamps it when a non-empty value is given.
//
// Helper prefix: submitWfDefault (per implementer-protocol.md §Helper-prefix discipline).
// Bead ref: hk-y3o51.

import (
	"encoding/json"
	"testing"
)

// TestSubmitDefaultWorkflow_EmptyOmitsKey asserts that beadsToQueueDoc with an
// empty workflowMode produces items with NO workflow_mode key in the JSON.
// This is the hk-y3o51 contract: empty = inherit daemon default.
func TestSubmitDefaultWorkflow_EmptyOmitsKey(t *testing.T) {
	t.Parallel()

	doc, err := beadsToQueueDoc([]string{"hk-test01"}, "", "")
	if err != nil {
		t.Fatalf("beadsToQueueDoc: %v", err)
	}

	groupsRaw, ok := doc["groups"]
	if !ok {
		t.Fatal("beadsToQueueDoc: missing groups key")
	}

	var groups []struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(groupsRaw, &groups); err != nil {
		t.Fatalf("unmarshal groups: %v", err)
	}
	if len(groups) == 0 || len(groups[0].Items) == 0 {
		t.Fatal("beadsToQueueDoc: no items in group[0]")
	}

	item := groups[0].Items[0]
	if _, present := item["workflow_mode"]; present {
		t.Errorf("beadsToQueueDoc with empty workflowMode: workflow_mode key present in JSON; want absent (hk-y3o51)")
	}
}

// TestSubmitDefaultWorkflow_ExplicitStamps asserts that beadsToQueueDoc stamps
// workflow_mode when a non-empty value is given (e.g. "review-loop").
func TestSubmitDefaultWorkflow_ExplicitStamps(t *testing.T) {
	t.Parallel()

	doc, err := beadsToQueueDoc([]string{"hk-test02"}, "", "review-loop")
	if err != nil {
		t.Fatalf("beadsToQueueDoc: %v", err)
	}

	groupsRaw, ok := doc["groups"]
	if !ok {
		t.Fatal("beadsToQueueDoc: missing groups key")
	}

	var groups []struct {
		Items []struct {
			WorkflowMode string `json:"workflow_mode"`
		} `json:"items"`
	}
	if err := json.Unmarshal(groupsRaw, &groups); err != nil {
		t.Fatalf("unmarshal groups: %v", err)
	}
	if len(groups) == 0 || len(groups[0].Items) == 0 {
		t.Fatal("beadsToQueueDoc: no items in group[0]")
	}

	got := groups[0].Items[0].WorkflowMode
	if got != "review-loop" {
		t.Errorf("beadsToQueueDoc with workflowMode=%q: item workflow_mode = %q; want %q",
			"review-loop", got, "review-loop")
	}
}
