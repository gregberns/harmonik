package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func runStartedV2Fixture() RunStartedPayload {
	return RunStartedPayload{
		RunID:                   RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000020")),
		WorkflowID:              WorkflowID("standard-bead"),
		WorkflowVersion:         WorkflowVersion("1.0"),
		WorkflowMode:            WorkflowModeDot,
		ReviewPolicy:            ReviewPolicyReviewed,
		WorkflowSelectionSource: WorkflowSelectionEmbeddedDefault,
		WorkspacePath:           "/tmp/workspace",
		InputRef:                "bead:hk-example",
		StartedAt:               time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}
}

func TestRunStartedPayloadV2Valid(t *testing.T) {
	if !runStartedV2Fixture().Valid() {
		t.Fatal("complete version-2 payload is invalid")
	}
}

func TestRunStartedPayloadV2RejectsInvalidContractValues(t *testing.T) {
	for name, mutate := range map[string]func(*RunStartedPayload){
		"missing descriptor": func(p *RunStartedPayload) { p.WorkflowID = "" },
		"missing version":    func(p *RunStartedPayload) { p.WorkflowVersion = "" },
		"old mode":           func(p *RunStartedPayload) { p.WorkflowMode = WorkflowModeSingle },
		"missing policy":     func(p *RunStartedPayload) { p.ReviewPolicy = "" },
		"missing source":     func(p *RunStartedPayload) { p.WorkflowSelectionSource = "" },
		"missing start time": func(p *RunStartedPayload) { p.StartedAt = time.Time{} },
		"no review without legacy source": func(p *RunStartedPayload) {
			p.WorkflowID = "no-review-bead"
			p.ReviewPolicy = ReviewPolicyNoReview
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := runStartedV2Fixture()
			mutate(&p)
			if p.Valid() {
				t.Fatal("invalid version-2 payload passed validation")
			}
		})
	}
}

func TestRunStartedPayloadV2AllowsOnlyRegisteredNoReviewBinding(t *testing.T) {
	p := runStartedV2Fixture()
	p.WorkflowID = "no-review-bead"
	p.ReviewPolicy = ReviewPolicyNoReview
	p.WorkflowSelectionSource = WorkflowSelectionLegacySingleLabel
	if !p.Valid() {
		t.Fatal("registered legacy no-review binding is invalid")
	}
	p.WorkflowVersion = "2.0"
	if p.Valid() {
		t.Fatal("unregistered no-review descriptor passed validation")
	}
}

// ValidPolicyBinding is now the one owner of the descriptor/policy/source rule:
// the run_started payload validator and the daemon's resolvedWorkflow.Valid both
// call it. It is exercised directly so a change to the rule cannot pass by
// hiding behind either caller's other checks.
func TestValidPolicyBinding(t *testing.T) {
	noReview := WorkflowDescriptor{WorkflowID: "no-review-bead", WorkflowVersion: "1.0"}
	standard := WorkflowDescriptor{WorkflowID: "standard-bead", WorkflowVersion: "1.0"}

	for name, tc := range map[string]struct {
		descriptor WorkflowDescriptor
		policy     ReviewPolicy
		source     WorkflowSelectionSource
		want       bool
	}{
		"no review from a bead label":                                {noReview, ReviewPolicyNoReview, WorkflowSelectionLegacySingleLabel, true},
		"no review from a queue item":                                {noReview, ReviewPolicyNoReview, WorkflowSelectionQueueItemSingleMode, true},
		"no review from an ordinary source":                          {noReview, ReviewPolicyNoReview, WorkflowSelectionProjectDefault, false},
		"no review on another graph":                                 {standard, ReviewPolicyNoReview, WorkflowSelectionLegacySingleLabel, false},
		"reviewed on an ordinary graph":                              {standard, ReviewPolicyReviewed, WorkflowSelectionProjectDefault, true},
		"reviewed claiming the no-review graph through a bead label": {noReview, ReviewPolicyReviewed, WorkflowSelectionLegacySingleLabel, false},
		"reviewed on the no-review graph by explicit ref":            {noReview, ReviewPolicyReviewed, WorkflowSelectionExplicitRef, true},
		"undeclared policy":                                          {standard, ReviewPolicy("audited"), WorkflowSelectionProjectDefault, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ValidPolicyBinding(tc.descriptor, tc.policy, tc.source); got != tc.want {
				t.Fatalf("ValidPolicyBinding(%v, %q, %q) = %v, want %v",
					tc.descriptor, tc.policy, tc.source, got, tc.want)
			}
		})
	}
}

func TestWorkflowSelectionSourceSelectsNoReview(t *testing.T) {
	for source, want := range map[WorkflowSelectionSource]bool{
		WorkflowSelectionLegacySingleLabel:   true,
		WorkflowSelectionQueueItemSingleMode: true,
		WorkflowSelectionEmbeddedDefault:     false,
		WorkflowSelectionProjectDefault:      false,
		WorkflowSelectionExplicitRef:         false,
		WorkflowSelectionSource("invented"):  false,
	} {
		if got := source.SelectsNoReview(); got != want {
			t.Errorf("%q.SelectsNoReview() = %v, want %v", source, got, want)
		}
	}
}

func TestRunStartedPayloadV2JSONRoundTrip(t *testing.T) {
	want := runStartedV2Fixture()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got RunStartedPayload
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.Valid() || got != want {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestRunStartedPayloadV2AllowsExplicitNullWorkerFields(t *testing.T) {
	encoded, err := json.Marshal(runStartedV2Fixture())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got RunStartedPayload
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("explicit null worker fields rejected: %v", err)
	}
	if got.WorkerName != nil || got.WorkerOS != nil {
		t.Fatalf("worker fields = %v, %v; want explicit null", got.WorkerName, got.WorkerOS)
	}
}

func TestRunStartedPayloadV2RejectsOmittedWorkerFields(t *testing.T) {
	for _, omitted := range []string{"worker_name", "worker_os"} {
		t.Run(omitted, func(t *testing.T) {
			var fields map[string]json.RawMessage
			encoded, err := json.Marshal(runStartedV2Fixture())
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatalf("Unmarshal fixture: %v", err)
			}
			delete(fields, omitted)
			encoded, err = json.Marshal(fields)
			if err != nil {
				t.Fatalf("Marshal fields: %v", err)
			}
			var got RunStartedPayload
			if err := json.Unmarshal(encoded, &got); err == nil {
				t.Fatalf("omitted %s decoded as version 2", omitted)
			}
		})
	}
}

func TestRunStartedPayloadV2RejectsLegacyShape(t *testing.T) {
	var got RunStartedPayload
	err := json.Unmarshal([]byte(`{"run_id":"01942b3c-0000-7000-8000-000000000020","workflow_id":"01942b3c-0000-7000-8000-000000000021","workflow_version":"1.0","workspace_path":"/tmp/workspace","input_ref":"bead:hk-example"}`), &got)
	if err == nil {
		t.Fatal("legacy version-1 shape decoded as a version-2 payload")
	}
}

func TestRunStartedRegisteredAtVersion2(t *testing.T) {
	got, ok := LookupTypeSchemaVersion("run_started")
	if !ok || got != 2 {
		t.Fatalf("run_started schema version = %d, %v; want 2, true", got, ok)
	}
}
