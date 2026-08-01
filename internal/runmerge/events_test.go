package runmerge

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

type eventPayloadCapture struct {
	eventType core.EventType
	runID     core.RunID
	payload   []byte
}

func (e *eventPayloadCapture) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return nil
}

func (e *eventPayloadCapture) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	e.runID = runID
	e.eventType = eventType
	e.payload = append([]byte(nil), payload...)
	return nil
}

func TestEmitWorkingTreeRefreshFailed_UsesCorePayloadWireShape(t *testing.T) {
	t.Parallel()
	runID := core.RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000000001"))
	emitter := &eventPayloadCapture{}

	emitWorkingTreeRefreshFailed(context.Background(), emitter, runID, core.BeadID("hk-refresh"), errors.New("reset failed"))

	if emitter.eventType != core.EventTypeWorkingTreeRefreshFailed {
		t.Fatalf("event type = %q, want %q", emitter.eventType, core.EventTypeWorkingTreeRefreshFailed)
	}
	if emitter.runID != runID {
		t.Fatalf("run ID = %s, want %s", emitter.runID, runID)
	}
	const want = `{"run_id":"018f1e2a-0000-7000-8000-000000000001","bead_id":"hk-refresh","error":"reset failed"}`
	if got := string(emitter.payload); got != want {
		t.Errorf("payload = %s, want %s", got, want)
	}
}

func TestEmitMergeBuildFailed_UsesCorePayloadWireShape(t *testing.T) {
	t.Parallel()
	runID := core.RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000000002"))
	emitter := &eventPayloadCapture{}

	emitMergeBuildFailed(context.Background(), emitter, runID, core.BeadID("hk-build"), errors.New("go build failed"), []byte("compile failed\n"))

	if emitter.eventType != core.EventTypeMergeBuildFailed {
		t.Fatalf("event type = %q, want %q", emitter.eventType, core.EventTypeMergeBuildFailed)
	}
	if emitter.runID != runID {
		t.Fatalf("run ID = %s, want %s", emitter.runID, runID)
	}
	const want = `{"run_id":"018f1e2a-0000-7000-8000-000000000002","bead_id":"hk-build","error":"go build failed\ncompile failed"}`
	if got := string(emitter.payload); got != want {
		t.Errorf("payload = %s, want %s", got, want)
	}
}
