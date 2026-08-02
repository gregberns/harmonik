package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

type runStartedEmitterT5 struct {
	runID   core.RunID
	typ     core.EventType
	payload []byte
}

func (*runStartedEmitterT5) Emit(context.Context, core.EventType, []byte) error { return nil }

func (e *runStartedEmitterT5) EmitWithRunID(_ context.Context, runID core.RunID, typ core.EventType, payload []byte) error {
	e.runID = runID
	e.typ = typ
	e.payload = append([]byte(nil), payload...)
	return nil
}

func TestEmitRunStartedWritesCoreVersion2Payload(t *testing.T) {
	runID := core.RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000020"))
	beadID := core.BeadID("hk-started-v2")
	queueID := "queue-a"
	queueGroupIndex := 1
	workerName := "worker-a"
	workerOS := "linux"
	emitter := &runStartedEmitterT5{}

	emitRunStarted(
		context.Background(), emitter, runID, beadID, "/tmp/workspace", &queueID, &queueGroupIndex,
		standardBeadDescriptor, core.WorkflowModeDot, core.ReviewPolicyReviewed,
		core.WorkflowSelectionEmbeddedDefault, &workerName, &workerOS,
	)

	if emitter.typ != core.EventTypeRunStarted || emitter.runID != runID {
		t.Fatalf("emission = type %q run %s, want run_started for %s", emitter.typ, emitter.runID, runID)
	}
	var got core.RunStartedPayload
	if err := json.Unmarshal(emitter.payload, &got); err != nil {
		t.Fatalf("decode emitted payload as version 2: %v\npayload: %s", err, emitter.payload)
	}
	if !got.Valid() || got.Descriptor() != standardBeadDescriptor || got.WorkflowMode != core.WorkflowModeDot ||
		got.ReviewPolicy != core.ReviewPolicyReviewed || got.WorkflowSelectionSource != core.WorkflowSelectionEmbeddedDefault ||
		got.BeadID == nil || *got.BeadID != beadID || got.InputRef != "bead:hk-started-v2" ||
		got.WorkerName == nil || *got.WorkerName != workerName || got.WorkerOS == nil || *got.WorkerOS != workerOS {
		t.Fatalf("emitted version-2 payload = %#v", got)
	}
}

func TestEmitRunStartedWritesExplicitNullLocalWorkerFields(t *testing.T) {
	runID := core.RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000021"))
	emitter := &runStartedEmitterT5{}

	emitRunStarted(
		context.Background(), emitter, runID, "hk-local", "/tmp/workspace", nil, nil,
		standardBeadDescriptor, core.WorkflowModeDot, core.ReviewPolicyReviewed,
		core.WorkflowSelectionEmbeddedDefault, nil, nil,
	)

	var got core.RunStartedPayload
	if err := json.Unmarshal(emitter.payload, &got); err != nil {
		t.Fatalf("decode emitted local payload as version 2: %v", err)
	}
	if got.WorkerName != nil || got.WorkerOS != nil {
		t.Fatalf("local worker fields = %v, %v; want explicit null", got.WorkerName, got.WorkerOS)
	}
}
