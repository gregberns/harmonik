package daemon

import (
	"testing"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestReplayPreparedReservationPersistsExactBoundRun(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderQueue(t, projectDir, intent, false)
	if err := replayPreparedReservation(t.Context(), projectDir, intent); err != nil {
		t.Fatal(err)
	}
	durable, err := queue.Load(t.Context(), projectDir, intent.Binding.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	item := durable.Groups[0].Items[0]
	if item.Status != queue.ItemStatusDispatched || item.RunID == nil || *item.RunID != intent.Binding.RunID.String() || item.Attempts != 1 {
		t.Fatalf("replayed item = %+v", item)
	}
}

func TestReplayPreparedReservationUsesExistingAttemptBound(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	item := queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusPending, Attempts: queue.MaxItemAttempts - 1}
	snapshot := replayFactQueue(intent, item)
	if err := queue.Persist(t.Context(), projectDir, &snapshot); err != nil {
		t.Fatal(err)
	}
	if err := replayPreparedReservation(t.Context(), projectDir, intent); err != nil {
		t.Fatal(err)
	}
	durable, err := queue.Load(t.Context(), projectDir, intent.Binding.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	got := durable.Groups[0].Items[0]
	if got.Status != queue.ItemStatusFailed || got.RunID != nil || got.Attempts != queue.MaxItemAttempts ||
		got.PreclaimTerminal == nil || got.PreclaimTerminal.RunID != intent.Binding.RunID.String() ||
		got.PreclaimTerminal.ClaimTransitionID != intent.Binding.ClaimTransitionID.String() {
		t.Fatalf("bounded replay item = %+v", got)
	}
}

func TestReplayPreparedReservationRefusesCrossQueueOwner(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderQueue(t, projectDir, intent, false)
	other := replayFactQueue(intent, queue.Item{BeadID: intent.Binding.BeadID, Status: queue.ItemStatusDispatched})
	other.Name = "other"
	other.QueueID = "0197d100-0000-7000-8000-000000000072"
	otherRun := "0197d100-0000-7000-8000-000000000073"
	other.Groups[0].Items[0].RunID = &otherRun
	if err := queue.Persist(t.Context(), projectDir, &other); err != nil {
		t.Fatal(err)
	}
	if err := replayPreparedReservation(t.Context(), projectDir, intent); err == nil {
		t.Fatal("replay reservation accepted a cross-queue owner")
	}
	durable, err := queue.Load(t.Context(), projectDir, intent.Binding.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	if durable.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Fatalf("target item changed = %+v", durable.Groups[0].Items[0])
	}
}

func TestReplayPreparedReservationRejectsWrongPhaseBeforeQueueIO(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	writeReplayReaderQueue(t, projectDir, intent, false)
	if err := replayPreparedReservation(t.Context(), projectDir, intent); err == nil {
		t.Fatal("replay reservation accepted claim-durable intent")
	}
	durable, err := queue.Load(t.Context(), projectDir, intent.Binding.QueueName)
	if err != nil {
		t.Fatal(err)
	}
	item := durable.Groups[0].Items[0]
	if item.Status != queue.ItemStatusPending || item.RunID != nil || item.Attempts != 0 {
		t.Fatalf("wrong-phase replay changed queue item = %+v", item)
	}
}
