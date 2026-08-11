package dispatch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

const (
	testQueueID      = "0197d100-0000-7000-8000-000000000001"
	testRunID        = "0197d100-0000-7000-8000-000000000002"
	testTransitionID = "0197d100-0000-7000-8000-000000000003"
)

func testIntent(phase Phase) Intent {
	runID := core.RunID(uuid.MustParse(testRunID))
	i := Intent{
		SchemaVersion: intentSchemaVersion,
		Phase:         phase,
		Binding: Binding{
			QueueID:           testQueueID,
			QueueName:         "main",
			GroupIndex:        2,
			ItemIndex:         3,
			BeadID:            "hk-dispatch",
			RunID:             runID,
			ClaimTransitionID: core.TransitionID(uuid.MustParse(testTransitionID)),
		},
	}
	if phase == PhaseRunDurable || phase == PhaseHandoffDurable {
		i.Run = &RunBinding{RecordRunID: runID}
	}
	if phase == PhaseClaimRefused {
		i.Refusal = &ClaimRefusalBinding{Cause: ClaimRefusalDependency}
	}
	if phase == PhaseHandoffDurable {
		i.Handoff = &HandoffBinding{
			SessionName:        "harmonik-run-0197d100",
			WorktreeLeaseRunID: runID,
		}
	}
	return i
}

func TestIntentValidPhaseShapes(t *testing.T) {
	for _, phase := range []Phase{PhasePrepared, PhaseClaimDurable, PhaseClaimRefused, PhaseRunDurable, PhaseHandoffDurable} {
		t.Run(string(phase), func(t *testing.T) {
			if err := testIntent(phase).Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIntentRejectsIncompleteAndEarlyPhaseData(t *testing.T) {
	otherRunID := core.RunID(uuid.MustParse("0197d100-0000-7000-8000-000000000099"))
	tests := []struct {
		name   string
		mutate func(*Intent)
	}{
		{name: "unknown phase", mutate: func(i *Intent) { i.Phase = "unknown" }},
		{name: "refusal missing", mutate: func(i *Intent) { i.Phase = PhaseClaimRefused }},
		{name: "refusal early", mutate: func(i *Intent) { i.Refusal = &ClaimRefusalBinding{Cause: ClaimRefusalDependency} }},
		{name: "refusal unknown", mutate: func(i *Intent) { *i = testIntent(PhaseClaimRefused); i.Refusal.Cause = "unknown" }},
		{name: "refusal with run", mutate: func(i *Intent) { *i = testIntent(PhaseClaimRefused); i.Run = &RunBinding{RecordRunID: i.Binding.RunID} }},
		{name: "refusal with handoff", mutate: func(i *Intent) {
			*i = testIntent(PhaseClaimRefused)
			i.Handoff = &HandoffBinding{SessionName: "not-allowed", WorktreeLeaseRunID: i.Binding.RunID}
		}},
		{name: "run missing", mutate: func(i *Intent) { i.Phase = PhaseRunDurable }},
		{name: "run early", mutate: func(i *Intent) { i.Run = &RunBinding{RecordRunID: i.Binding.RunID} }},
		{name: "run mismatch", mutate: func(i *Intent) {
			i.Phase = PhaseRunDurable
			i.Run = &RunBinding{RecordRunID: otherRunID}
		}},
		{name: "handoff missing", mutate: func(i *Intent) {
			i.Phase = PhaseHandoffDurable
			i.Run = &RunBinding{RecordRunID: i.Binding.RunID}
		}},
		{name: "handoff early", mutate: func(i *Intent) { i.Handoff = &HandoffBinding{SessionName: "s", WorktreeLeaseRunID: i.Binding.RunID} }},
		{name: "session missing", mutate: func(i *Intent) { *i = testIntent(PhaseHandoffDurable); i.Handoff.SessionName = "" }},
		{name: "lease mismatch", mutate: func(i *Intent) { *i = testIntent(PhaseHandoffDurable); i.Handoff.WorktreeLeaseRunID = otherRunID }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i := testIntent(PhasePrepared)
			tc.mutate(&i)
			if err := i.Validate(); err == nil {
				t.Fatal("Validate() = nil")
			}
		})
	}
}

func TestClaimRefusedIntentJSONRoundTrip(t *testing.T) {
	want := testIntent(PhaseClaimRefused)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Intent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Refusal == nil || *got.Refusal != *want.Refusal || got.Phase != PhaseClaimRefused {
		t.Fatalf("round trip = %+v", got)
	}
	for name, raw := range map[string]string{
		"unknown cause": strings.Replace(string(data), `"dependency_refusal"`, `"unknown"`, 1),
		"unknown field": strings.Replace(string(data), `"cause":"dependency_refusal"`, `"cause":"dependency_refusal","extra":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatal("Unmarshal() = nil")
			}
		})
	}
	invalid := want
	invalid.Refusal.Cause = "unknown"
	if _, err := json.Marshal(invalid); err == nil {
		t.Fatal("Marshal() = nil")
	}
}

func TestIntentRejectsInvalidBaseBinding(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Binding)
	}{
		{name: "queue ID", mutate: func(b *Binding) { b.QueueID = "not-a-uuid" }},
		{name: "queue name", mutate: func(b *Binding) { b.QueueName = "Bad Name" }},
		{name: "group index", mutate: func(b *Binding) { b.GroupIndex = -1 }},
		{name: "item index", mutate: func(b *Binding) { b.ItemIndex = -1 }},
		{name: "bead ID", mutate: func(b *Binding) { b.BeadID = "" }},
		{name: "run ID", mutate: func(b *Binding) { b.RunID = core.RunID{} }},
		{name: "claim transition ID", mutate: func(b *Binding) { b.ClaimTransitionID = core.TransitionID{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			i := testIntent(PhasePrepared)
			tc.mutate(&i.Binding)
			if err := i.Validate(); err == nil {
				t.Fatal("Validate() = nil")
			}
		})
	}
}

func TestIntentJSONRoundTripAndStrictDecode(t *testing.T) {
	want := testIntent(PhaseHandoffDurable)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Intent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Binding != want.Binding || *got.Run != *want.Run || *got.Handoff != *want.Handoff {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	valid := string(data)
	tests := map[string]string{
		"unknown top field":    strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"extra":true`, 1),
		"unknown nested field": strings.Replace(valid, `"queue_id"`, `"extra":true,"queue_id"`, 1),
		"partial binding":      `{"schema_version":1,"phase":"prepared","binding":{"queue_id":"` + testQueueID + `"}}`,
		"missing group index":  strings.Replace(valid, `"group_index":2,`, "", 1),
		"missing item index":   strings.Replace(valid, `"item_index":3,`, "", 1),
		"uppercase run ID":     strings.Replace(valid, testRunID, strings.ToUpper(testRunID), 1),
		"multiple values":      valid + `{}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatal("Unmarshal() = nil")
			}
		})
	}
}

func TestIntentMarshalRejectsInvalidValue(t *testing.T) {
	i := testIntent(PhaseRunDurable)
	i.Run = nil
	if _, err := json.Marshal(i); err == nil {
		t.Fatal("Marshal() = nil")
	}
}

func TestIntentWireMovesSessionIdentityToHandoff(t *testing.T) {
	runData, err := json.Marshal(testIntent(PhaseRunDurable))
	if err != nil {
		t.Fatal(err)
	}
	wantRun := `"run":{"record_run_id":"` + testRunID + `"}`
	if !strings.Contains(string(runData), wantRun) || strings.Contains(string(runData), "session_name") {
		t.Fatalf("run-durable bytes = %s", runData)
	}
	handoffData, err := json.Marshal(testIntent(PhaseHandoffDurable))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(handoffData), `"handoff":{"session_name":"harmonik-run-0197d100"`) {
		t.Fatalf("handoff-durable bytes = %s", handoffData)
	}
	legacy := strings.Replace(string(runData), wantRun,
		`"run":{"record_run_id":"`+testRunID+`","session_name":"legacy"}`, 1)
	var decoded Intent
	if err := json.Unmarshal([]byte(legacy), &decoded); err == nil {
		t.Fatal("legacy run.session_name decode = nil")
	}
}
