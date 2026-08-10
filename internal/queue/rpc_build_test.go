package queue_test

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestBuildQueueSubmitConstructsNormalizedPendingQueue(t *testing.T) {
	acceptedAt := time.Date(2026, 8, 10, 5, 6, 7, 890000000, time.FixedZone("test", -7*60*60))
	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "",
		Workers:       0,
		Groups: []queue.Group{{
			Kind: queue.GroupKindStream,
			Items: []queue.Item{
				{BeadID: "hk-builder-a", Status: queue.ItemStatusCompleted, Context: "context", WorkflowMode: "dot", WorkflowRef: "flow.dot", TemplateParams: map[string]string{"ROLE": "reviewer"}},
				{BeadID: "hk-builder-b"},
			},
		}},
	}
	deferred := []queue.LedgerDepPair{{BeadID: "hk-builder-b", BlockerBeadID: "hk-builder-a", GroupIndex: 0}}

	resp, got, rpcErr := queue.BuildQueueSubmit(req, deferred, "queue-id", acceptedAt, 3)
	if rpcErr != nil {
		t.Fatalf("BuildQueueSubmit returned error: %v", rpcErr)
	}
	if resp.QueueID != "queue-id" || resp.Status != queue.QueueStatusActive || resp.GroupCount != 1 {
		t.Fatalf("response = %#v", resp)
	}
	if got.Name != queue.QueueNameMain || got.Workers != 3 || got.SubmittedAt != acceptedAt.UTC() {
		t.Errorf("queue header = name %q workers %d submitted %v", got.Name, got.Workers, got.SubmittedAt)
	}
	group := got.Groups[0]
	if group.GroupIndex != 0 || group.Status != queue.GroupStatusPending || group.CreatedAt != acceptedAt.UTC() {
		t.Errorf("group = %#v", group)
	}
	first := group.Items[0]
	if first.Status != queue.ItemStatusPending || first.RunID != nil || first.AppendedAt != nil {
		t.Errorf("first item daemon fields were not reset: %#v", first)
	}
	if first.Context != "context" || first.WorkflowMode != "dot" || first.WorkflowRef != "flow.dot" || first.TemplateParams["ROLE"] != "reviewer" {
		t.Errorf("first item request fields were not preserved: %#v", first)
	}
	if got.Groups[0].Items[1].Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("deferred item status = %q", got.Groups[0].Items[1].Status)
	}
}

func TestBuildQueueSubmitRejectsPureRequestErrors(t *testing.T) {
	tests := []struct {
		name string
		req  queue.QueueSubmitRequest
		want string
	}{
		{
			name: "pi needs worker cap",
			req:  queue.QueueSubmitRequest{DefaultHarness: core.AgentTypePi},
			want: "pi_queue_missing_workers_cap",
		},
		{
			name: "template parameter is invalid",
			req: queue.QueueSubmitRequest{Groups: []queue.Group{{Items: []queue.Item{{
				BeadID: "hk-builder", TemplateParams: map[string]string{"bad-key": "value"},
			}}}}},
			want: "invalid_template_param",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, rpcErr := queue.BuildQueueSubmit(tc.req, nil, "queue-id", time.Unix(1, 0), 4)
			if rpcErr == nil || rpcErr.Message != tc.want {
				t.Fatalf("error = %#v, want message %q", rpcErr, tc.want)
			}
		})
	}
}

func TestBuildQueueSubmitAppliesWorkerCapAndHarnessRules(t *testing.T) {
	tests := []struct {
		name        string
		workers     int
		global      int
		harness     core.AgentType
		wantWorkers int
		wantHarness core.AgentType
	}{
		{name: "global floor", global: 0, wantWorkers: 1},
		{name: "global default", global: 4, wantWorkers: 4},
		{name: "request overrides global", workers: 7, global: 4, wantWorkers: 7},
		{name: "valid harness remains", workers: 1, global: 4, harness: core.AgentTypeCodex, wantWorkers: 1, wantHarness: core.AgentTypeCodex},
		{name: "invalid harness clears", workers: 1, global: 4, harness: core.AgentType("bad harness"), wantWorkers: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got, rpcErr := queue.BuildQueueSubmit(queue.QueueSubmitRequest{
				SchemaVersion:  1,
				Workers:        tc.workers,
				DefaultHarness: tc.harness,
			}, nil, "queue-id", time.Unix(1, 0), tc.global)
			if rpcErr != nil {
				t.Fatalf("BuildQueueSubmit returned error: %v", rpcErr)
			}
			if got.Workers != tc.wantWorkers || got.DefaultHarness != tc.wantHarness {
				t.Errorf("workers/harness = %d/%q, want %d/%q", got.Workers, got.DefaultHarness, tc.wantWorkers, tc.wantHarness)
			}
		})
	}
}
