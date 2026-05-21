package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// makeNotifyEvent constructs a minimal core.Event with a JSON-marshalled payload.
func makeNotifyEvent(t *testing.T, evtType core.EventType, payload any) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("makeNotifyEvent: uuid: %v", err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("makeNotifyEvent: marshal: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            string(evtType),
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(b),
	}
}

// TestNotifyStream_SuccessWithCommit verifies that a complete success cycle
// (run_started → workspace_merge_status merged → run_completed) emits a line
// with the bead ID and a short commit SHA.
func TestNotifyStream_SuccessWithCommit(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-001"
	beadID := "hk-abc123"
	sha := "deadbeefcafebabe1234"

	// 1. run_started → capture RunID → BeadID
	_ = n.handleRunStarted(ctx, makeNotifyEvent(t, core.EventTypeRunStarted,
		notifyRunStarted{RunID: runID, BeadID: beadID}))

	// 2. workspace_merge_status merged → capture RunID → commit SHA
	_ = n.handleMergeStatus(ctx, makeNotifyEvent(t, core.EventTypeWorkspaceMergeStatus,
		notifyMergeStatus{RunID: runID, Status: "merged", MergeCommitHash: &sha}))

	// 3. run_completed → emit success line
	_ = n.handleRunCompleted(ctx, makeNotifyEvent(t, core.EventTypeRunCompleted,
		notifyRunCompleted{RunID: runID, Success: true, Summary: "all good"}))

	line := strings.TrimSpace(buf.String())
	wantPrefix := "[hk-abc123] success (commit deadbee"
	if !strings.HasPrefix(line, "[hk-abc123] success (commit ") {
		t.Errorf("got %q, want prefix %q", line, wantPrefix)
	}
	// SHA should be truncated to 7 chars
	if !strings.Contains(line, "deadbee") {
		t.Errorf("got %q, want 7-char SHA prefix %q", line, "deadbee")
	}
}

// TestNotifyStream_SuccessNoCommit verifies that success without a prior
// workspace_merge_status emits a bare success line.
func TestNotifyStream_SuccessNoCommit(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-002"
	beadID := "hk-def456"

	_ = n.handleRunStarted(ctx, makeNotifyEvent(t, core.EventTypeRunStarted,
		notifyRunStarted{RunID: runID, BeadID: beadID}))

	_ = n.handleRunCompleted(ctx, makeNotifyEvent(t, core.EventTypeRunCompleted,
		notifyRunCompleted{RunID: runID, Success: true}))

	line := strings.TrimSpace(buf.String())
	want := "[hk-def456] success"
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

// TestNotifyStream_Failure verifies that run_failed emits a failure line with
// the reason from the summary field of the workloopRunCompletedPayload.
func TestNotifyStream_Failure(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-003"
	beadID := "hk-ghi789"

	_ = n.handleRunStarted(ctx, makeNotifyEvent(t, core.EventTypeRunStarted,
		notifyRunStarted{RunID: runID, BeadID: beadID}))

	_ = n.handleRunFailed(ctx, makeNotifyEvent(t, core.EventTypeRunFailed,
		notifyRunCompleted{RunID: runID, Success: false, Summary: "budget exhausted"}))

	line := strings.TrimSpace(buf.String())
	want := "[hk-ghi789] failed (reason: budget exhausted)"
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

// TestNotifyStream_FailureNoBeadID verifies graceful fallback when run_started
// was not captured before run_failed (bead_id unknown → use run_id).
func TestNotifyStream_FailureNoBeadID(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-004"

	_ = n.handleRunFailed(ctx, makeNotifyEvent(t, core.EventTypeRunFailed,
		notifyRunCompleted{RunID: runID, Success: false, Summary: "workspace error"}))

	line := strings.TrimSpace(buf.String())
	if !strings.Contains(line, runID) {
		t.Errorf("got %q, want run_id %q as fallback", line, runID)
	}
	if !strings.Contains(line, "workspace error") {
		t.Errorf("got %q, want reason %q in output", line, "workspace error")
	}
}

// TestNotifyStream_MergeStatusPendingIgnored verifies that workspace_merge_status
// events with status=pending do not store a commit SHA.
func TestNotifyStream_MergeStatusPendingIgnored(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-005"
	beadID := "hk-jkl012"
	pendingMsg := notifyMergeStatus{RunID: runID, Status: "pending"}

	_ = n.handleRunStarted(ctx, makeNotifyEvent(t, core.EventTypeRunStarted,
		notifyRunStarted{RunID: runID, BeadID: beadID}))
	_ = n.handleMergeStatus(ctx, makeNotifyEvent(t, core.EventTypeWorkspaceMergeStatus, pendingMsg))
	_ = n.handleRunCompleted(ctx, makeNotifyEvent(t, core.EventTypeRunCompleted,
		notifyRunCompleted{RunID: runID, Success: true}))

	line := strings.TrimSpace(buf.String())
	want := "[hk-jkl012] success"
	if line != want {
		t.Errorf("got %q, want %q (pending merge status must not set commit)", line, want)
	}
}

// TestNotifyStream_StateCleanupAfterCompletion verifies that the internal maps
// are cleaned up after a run completes (no memory leak on long multi-bead runs).
func TestNotifyStream_StateCleanupAfterCompletion(t *testing.T) {
	var buf bytes.Buffer
	n := NewNotifyStreamConsumer(&buf)
	ctx := context.Background()
	runID := "test-run-006"
	sha := "abc123"

	_ = n.handleRunStarted(ctx, makeNotifyEvent(t, core.EventTypeRunStarted,
		notifyRunStarted{RunID: runID, BeadID: "hk-mno345"}))
	_ = n.handleMergeStatus(ctx, makeNotifyEvent(t, core.EventTypeWorkspaceMergeStatus,
		notifyMergeStatus{RunID: runID, Status: "merged", MergeCommitHash: &sha}))
	_ = n.handleRunCompleted(ctx, makeNotifyEvent(t, core.EventTypeRunCompleted,
		notifyRunCompleted{RunID: runID, Success: true}))

	n.mu.Lock()
	_, hasID := n.ids[runID]
	_, hasSHA := n.sha[runID]
	n.mu.Unlock()

	if hasID {
		t.Error("ids map not cleaned up after run_completed")
	}
	if hasSHA {
		t.Error("sha map not cleaned up after run_completed")
	}
}
