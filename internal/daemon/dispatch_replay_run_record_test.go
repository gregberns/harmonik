package daemon

import (
	"reflect"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

func TestReplayWriteRunRecordUsesExactBindingAndInjectedTime(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	wantTime := time.Date(2026, 8, 13, 20, 30, 40, 123456789, time.FixedZone("offset", 3600))
	executor := dispatchReplayExecutor{projectDir: projectDir, now: func() time.Time { return wantTime }}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.WriteRunRecord}); err != nil {
		t.Fatal(err)
	}
	records, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Dispatch) != 1 {
		t.Fatalf("dispatch records = %+v", records.Dispatch)
	}
	want, err := runpkg.NewDispatchRecord(intent.Binding, wantTime)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records.Dispatch[0], want) {
		t.Fatalf("dispatch record = %+v, want %+v", records.Dispatch[0], want)
	}
}

func TestReplayAdvanceRunPhaseUsesExactIntentCAS(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhaseClaimDurable)
	writeReplayReaderIntent(t, projectDir, intent)
	executor := dispatchReplayExecutor{projectDir: projectDir}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: intent, Action: dispatch.AdvanceRunPhase}); err != nil {
		t.Fatal(err)
	}
	got, err := dispatchstore.New(projectDir).Load(intent.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != dispatch.PhaseRunDurable || got.Run == nil || got.Run.RecordRunID != intent.Binding.RunID {
		t.Fatalf("advanced intent = %+v", got)
	}
}

func TestReplayRunActionsRejectWrongPhasesBeforeWrite(t *testing.T) {
	projectDir := t.TempDir()
	prepared := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, prepared)
	executor := dispatchReplayExecutor{projectDir: projectDir, now: time.Now}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: prepared, Action: dispatch.WriteRunRecord}); err == nil {
		t.Fatal("write run record accepted a prepared intent")
	}
	if err := executor.execute(t.Context(), dispatchReplayStep{Intent: prepared, Action: dispatch.AdvanceRunPhase}); err == nil {
		t.Fatal("advance run phase accepted a prepared intent")
	}
	records, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Dispatch) != 0 {
		t.Fatalf("wrong phase wrote dispatch records = %+v", records.Dispatch)
	}
	got, err := dispatchstore.New(projectDir).Load(prepared.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, prepared) {
		t.Fatalf("wrong phase changed intent: got %+v, want %+v", got, prepared)
	}
}
