package queue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func activeRunsFixturePersist(t *testing.T, projectDir, name string, bead core.BeadID, status queue.ItemStatus, runID string) {
	t.Helper()

	group := rpcFixtureWaveGroup(bead)
	group.Items[0].Status = status
	if runID != "" {
		group.Items[0].RunID = &runID
	}

	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-0000000000" + name[:2],
		Name:          name,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{group},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist %s: %v", name, err)
	}
}

func decodeStatus(t *testing.T, raw json.RawMessage) queue.QueueStatusResponse {
	t.Helper()
	var resp queue.QueueStatusResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode queue-status response: %v", err)
	}
	return resp
}

// The defect exactly: a bead dispatched from a named queue, and no "main" queue
// on disk at all. The Queue field is legitimately null here — there IS no main
// queue — so the run has to be visible somewhere else, or the answer is a lie.
func TestQueueStatusReportsInFlightWorkWhenNoMainQueueExists(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	activeRunsFixturePersist(t, projectDir, "matrix-codex-local", "cll-t4k",
		queue.ItemStatusDispatched, "019fec81-70a0-73d8-b53e-2aff866a0b2c")

	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("cll-t4k"), projectDir, nil, nil)

	raw, rpcErr := adapter.HandleQueueStatus(t.Context(), nil)
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: %+v", rpcErr)
	}
	resp := decodeStatus(t, raw)

	if len(resp.ActiveRuns) != 1 {
		t.Fatalf("active_runs = %d entries (%+v); want 1 — a dispatched bead is invisible and every reader concludes idle",
			len(resp.ActiveRuns), resp.ActiveRuns)
	}
	got := resp.ActiveRuns[0]
	if got.BeadID != "cll-t4k" {
		t.Errorf("active_runs[0].bead_id = %q; want cll-t4k", got.BeadID)
	}
	if got.Queue != "matrix-codex-local" {
		t.Errorf("active_runs[0].queue = %q; want matrix-codex-local — without it the reader cannot go and look at the queue", got.Queue)
	}
	if got.RunID != "019fec81-70a0-73d8-b53e-2aff866a0b2c" {
		t.Errorf("active_runs[0].run_id = %q; want the dispatched run id", got.RunID)
	}
}

// A lull must be distinguishable from a daemon that does not report. The key is
// always present and the value is [] rather than null, so len() is the answer.
func TestQueueStatusReportsAnEmptyListWhenNothingIsInFlight(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	activeRunsFixturePersist(t, projectDir, "main", "hk-idle", queue.ItemStatusPending, "")

	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("hk-idle"), projectDir, nil, nil)

	raw, rpcErr := adapter.HandleQueueStatus(t.Context(), nil)
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: %+v", rpcErr)
	}
	if len(decodeStatus(t, raw).ActiveRuns) != 0 {
		t.Errorf("active_runs is non-empty with only a pending item; a pending bead is not in flight")
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	body, present := envelope["active_runs"]
	if !present {
		t.Fatal("payload has no active_runs key; an absent key reads as zero in flight and the shutdown gate cannot fail")
	}
	if string(body) != "[]" {
		t.Errorf("active_runs encoded as %s; want [] so an empty lull is stated rather than inferred", body)
	}
}

// Naming a queue must not narrow the in-flight answer. A caller that asks about
// one queue still gets told the whole daemon is busy, which is the question the
// shutdown gate is really asking.
func TestQueueStatusReportsInFlightWorkFromEveryQueue(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	activeRunsFixturePersist(t, projectDir, "main", "hk-idle", queue.ItemStatusPending, "")
	activeRunsFixturePersist(t, projectDir, "paul", "hk-busy", queue.ItemStatusDispatched, "019fec81-0000-7000-8000-000000000001")

	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("hk-idle", "hk-busy"), projectDir, nil, nil)

	raw, rpcErr := adapter.HandleQueueStatus(t.Context(), json.RawMessage(`{"name":"main"}`))
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: %+v", rpcErr)
	}
	resp := decodeStatus(t, raw)

	if resp.Queue == nil || resp.Queue.Name != "main" {
		t.Fatalf("Queue = %+v; want the requested main queue — the named lookup must keep its meaning", resp.Queue)
	}
	if len(resp.ActiveRuns) != 1 || resp.ActiveRuns[0].BeadID != "hk-busy" {
		t.Errorf("active_runs = %+v; want the bead running in queue paul — asking about main must not hide it", resp.ActiveRuns)
	}
}
