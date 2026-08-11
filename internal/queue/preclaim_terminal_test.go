package queue

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	preclaimRunID        = "0197d300-0000-7000-8000-000000000001"
	preclaimTransitionID = "0197d300-0000-7000-8000-000000000002"
)

func TestPreclaimTerminalBindingValidCauses(t *testing.T) {
	for _, cause := range []PreclaimTerminalCause{
		PreclaimTerminalMaxAttempts,
		PreclaimTerminalCrossQueue,
		PreclaimTerminalDependencyRefusal,
	} {
		binding := PreclaimTerminalBinding{RunID: preclaimRunID, ClaimTransitionID: preclaimTransitionID, Cause: cause}
		if err := binding.Validate(); err != nil {
			t.Fatalf("cause %q: %v", cause, err)
		}
	}
}

func TestPreclaimTerminalBindingRejectsUnknownWireField(t *testing.T) {
	data := `{"run_id":"` + preclaimRunID + `","claim_transition_id":"` + preclaimTransitionID + `","cause":"max_attempts","extra":true}`
	var binding PreclaimTerminalBinding
	if err := json.Unmarshal([]byte(data), &binding); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestPreclaimTerminalBindingWireRejectsInvalidValues(t *testing.T) {
	invalid := []PreclaimTerminalBinding{
		{ClaimTransitionID: preclaimTransitionID, Cause: PreclaimTerminalMaxAttempts},
		{RunID: preclaimRunID, Cause: PreclaimTerminalMaxAttempts},
		{RunID: preclaimRunID, ClaimTransitionID: preclaimTransitionID, Cause: "unknown"},
	}
	for index, binding := range invalid {
		if _, err := json.Marshal(binding); err == nil {
			t.Fatalf("marshal case %d accepted: %+v", index, binding)
		}
	}
	raw := []string{
		`{"claim_transition_id":"` + preclaimTransitionID + `","cause":"max_attempts"}`,
		`{"run_id":"` + preclaimRunID + `","cause":"max_attempts"}`,
		`{"run_id":"` + preclaimRunID + `","claim_transition_id":"` + preclaimTransitionID + `","cause":"unknown"}`,
	}
	for index, data := range raw {
		var binding PreclaimTerminalBinding
		if err := json.Unmarshal([]byte(data), &binding); err == nil {
			t.Fatalf("unmarshal case %d accepted: %s", index, data)
		}
	}
}

func TestQueueRejectsImpossiblePreclaimTerminalShapes(t *testing.T) {
	matchingRunID := preclaimRunID
	otherRunID := "0197d300-0000-7000-8000-000000000099"
	tests := []Item{
		{BeadID: "hk-preclaim", Status: ItemStatusPending, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalMaxAttempts)},
		{BeadID: "hk-preclaim", Status: ItemStatusDispatched, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalMaxAttempts)},
		{BeadID: "hk-preclaim", Status: ItemStatusCompleted, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalMaxAttempts)},
		{BeadID: "hk-preclaim", Status: ItemStatusDeferredForLedgerDep, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalMaxAttempts)},
		{BeadID: "hk-preclaim", Status: ItemStatusFailed, RunID: &matchingRunID, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalMaxAttempts)},
		{BeadID: "hk-preclaim", Status: ItemStatusFailed, RunID: &matchingRunID, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalCrossQueue)},
		{BeadID: "hk-preclaim", Status: ItemStatusFailed, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalDependencyRefusal)},
		{BeadID: "hk-preclaim", Status: ItemStatusFailed, RunID: &otherRunID, PreclaimTerminal: testPreclaimBinding(PreclaimTerminalDependencyRefusal)},
	}
	for index, item := range tests {
		q := preclaimQueue(item)
		if err := Persist(context.Background(), t.TempDir(), &q); err == nil {
			t.Fatalf("case %d persisted", index)
		}
		data, err := json.Marshal(q)
		if err != nil && strings.Contains(err.Error(), "UUID") {
			t.Fatalf("case %d failed for identity instead of shape: %v", index, err)
		}
		if err == nil {
			if _, err := UnmarshalQueue(data); err == nil {
				t.Fatalf("case %d loaded", index)
			}
		}
	}
}

func TestQueuePersistsAndLoadsEveryPreclaimTerminalCause(t *testing.T) {
	for _, cause := range []PreclaimTerminalCause{
		PreclaimTerminalMaxAttempts,
		PreclaimTerminalCrossQueue,
		PreclaimTerminalDependencyRefusal,
	} {
		t.Run(string(cause), func(t *testing.T) {
			item := admittedPreclaimItem(cause)
			q := preclaimQueue(item)
			if err := Persist(context.Background(), t.TempDir(), &q); err != nil {
				t.Fatalf("Persist: %v", err)
			}
			data, err := json.Marshal(q)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := UnmarshalQueue(data)
			if err != nil {
				t.Fatalf("UnmarshalQueue: %v", err)
			}
			if got := loaded.Groups[0].Items[0].PreclaimTerminal; got == nil || *got != *item.PreclaimTerminal {
				t.Fatalf("loaded binding = %+v", got)
			}
		})
	}
}

func TestReactivateFailedItemClearsPreclaimTerminalBinding(t *testing.T) {
	runID := preclaimRunID
	item := Item{
		BeadID:           "hk-preclaim",
		Status:           ItemStatusFailed,
		RunID:            &runID,
		PreclaimTerminal: testPreclaimBinding(PreclaimTerminalDependencyRefusal),
	}
	if err := ReactivateFailedItem(&item); err != nil {
		t.Fatal(err)
	}
	if item.PreclaimTerminal != nil {
		t.Fatalf("preclaim terminal binding survived retry: %+v", item.PreclaimTerminal)
	}
}

func TestPreclaimTerminalBindingRejectsInvalidAuthority(t *testing.T) {
	tests := []PreclaimTerminalBinding{
		{RunID: "", ClaimTransitionID: preclaimTransitionID, Cause: PreclaimTerminalMaxAttempts},
		{RunID: preclaimRunID, ClaimTransitionID: "0197d300000070008000000000000002", Cause: PreclaimTerminalMaxAttempts},
		{RunID: preclaimRunID, ClaimTransitionID: preclaimTransitionID, Cause: "text_reason"},
	}
	for index, binding := range tests {
		if err := binding.Validate(); err == nil {
			t.Fatalf("case %d accepted: %+v", index, binding)
		}
	}
}

func testPreclaimBinding(cause PreclaimTerminalCause) *PreclaimTerminalBinding {
	return &PreclaimTerminalBinding{RunID: preclaimRunID, ClaimTransitionID: preclaimTransitionID, Cause: cause}
}

func preclaimQueue(item Item) Queue {
	return Queue{SchemaVersion: schemaVersion, Groups: []Group{{Items: []Item{item}}}}
}

func admittedPreclaimItem(cause PreclaimTerminalCause) Item {
	item := Item{BeadID: "hk-preclaim", Status: ItemStatusFailed, PreclaimTerminal: testPreclaimBinding(cause)}
	if cause == PreclaimTerminalDependencyRefusal {
		runID := preclaimRunID
		item.RunID = &runID
	}
	return item
}

func TestPreclaimTerminalBindingWireAndClone(t *testing.T) {
	binding := &PreclaimTerminalBinding{RunID: preclaimRunID, ClaimTransitionID: preclaimTransitionID, Cause: PreclaimTerminalDependencyRefusal}
	runID := preclaimRunID
	item := Item{BeadID: "hk-preclaim", Status: ItemStatusFailed, RunID: &runID, PreclaimTerminal: binding}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Item
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PreclaimTerminal == nil || *decoded.PreclaimTerminal != *binding {
		t.Fatalf("decoded binding = %+v", decoded.PreclaimTerminal)
	}
	cloned := cloneQueueItem(item)
	cloned.PreclaimTerminal.Cause = PreclaimTerminalMaxAttempts
	if item.PreclaimTerminal.Cause != PreclaimTerminalDependencyRefusal {
		t.Fatalf("clone changed source binding: %+v", item.PreclaimTerminal)
	}
}
