package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

type legacyReconcileEmitter struct {
	runs []core.RunID
}

func (e *legacyReconcileEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }

func (e *legacyReconcileEmitter) EmitWithRunID(_ context.Context, runID core.RunID, _ core.EventType, _ []byte) error {
	e.runs = append(e.runs, runID)
	return nil
}

type legacyReconcileResetter struct {
	beads []core.BeadID
}

func (r *legacyReconcileResetter) ResetBead(_ context.Context, _ string, _ brcli.TimeoutConfig, beadID core.BeadID, _ core.ProjectHash, _ int64) error {
	r.beads = append(r.beads, beadID)
	return nil
}

type legacyReconcileStatusReader struct{}

func (legacyReconcileStatusReader) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{Status: core.CoarseStatusInProgress}, nil
}

func TestReconcileOrphanedRunsReadsVersion1RunStarted(t *testing.T) {
	runID := core.RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000020"))
	raw, err := json.Marshal(map[string]any{
		"run_id":         runID.String(),
		"bead_id":        "hk-historical",
		"workspace_path": "/tmp/historical",
		"started_at":     "2026-08-02T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal legacy run_started: %v", err)
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	eventID := core.EventID(uuid.MustParse("01942b3c-0000-7000-8000-000000000021"))
	event := core.Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            string(core.EventTypeRunStarted),
		TimestampWall:   time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		RunID:           &runID,
		SourceSubsystem: "daemon",
		Payload:         raw,
	}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal legacy event: %v", err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("write legacy event: %v", err)
	}

	emitter := &legacyReconcileEmitter{}
	resetter := &legacyReconcileResetter{}
	count := reconcileOrphanedRunsOnResume(
		context.Background(), path, emitter, resetter, legacyReconcileStatusReader{}, "",
		core.ProjectHash(""), 0, lifecycle.QueueDispatchedSet{}, nil,
	)
	if count != 1 || len(emitter.runs) != 1 || emitter.runs[0] != runID {
		t.Fatalf("reconcile emitted %d terminals for %v, want one for %s", count, emitter.runs, runID)
	}
	if len(resetter.beads) != 1 || resetter.beads[0] != "hk-historical" {
		t.Fatalf("reconcile resets = %v, want [hk-historical]", resetter.beads)
	}
}
