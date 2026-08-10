package runmerge

import (
	"context"
	"encoding/json"
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

// TestEmitWorkspaceMergeStatusMerged_RefusesPayloadItsOwnValidRejects covers
// both directions of the emit guard (hk-3kw4a), the same shape the
// session_log_location guard is tested to. A merge event that names an empty
// target branch or an empty landing commit answers the question it exists to
// answer with a lie, so the emit path asks the payload first and drops it.
func TestEmitWorkspaceMergeStatusMerged_RefusesPayloadItsOwnValidRejects(t *testing.T) {
	t.Parallel()
	runID := core.RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000000003"))

	for _, tc := range []struct {
		name                string
		source, target, tip string
		wantEmit            bool
	}{
		{name: "complete", source: "run/abc", target: "main", tip: "deadbeef", wantEmit: true},
		{name: "empty target branch", source: "run/abc", target: "", tip: "deadbeef"},
		{name: "empty landing commit", source: "run/abc", target: "main", tip: ""},
		{name: "empty source branch", source: "", target: "main", tip: "deadbeef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			emitter := &eventPayloadCapture{}
			emitWorkspaceMergeStatusMerged(context.Background(), emitter, runID, tc.source, tc.target, tc.tip)

			emitted := emitter.eventType == core.EventTypeWorkspaceMergeStatus
			if emitted != tc.wantEmit {
				t.Fatalf("emitted = %v, want %v (payload: %s)", emitted, tc.wantEmit, emitter.payload)
			}
			if !tc.wantEmit {
				return
			}
			var pl core.WorkspaceMergeStatusPayload
			if err := json.Unmarshal(emitter.payload, &pl); err != nil {
				t.Fatalf("payload does not decode: %v\n%s", err, emitter.payload)
			}
			if !pl.Valid() {
				t.Errorf("emitted payload fails its own Valid(): %s", emitter.payload)
			}
			if pl.MergeCommitHash == nil || *pl.MergeCommitHash != tc.tip {
				t.Errorf("merge_commit_hash = %v, want %q", pl.MergeCommitHash, tc.tip)
			}
		})
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
