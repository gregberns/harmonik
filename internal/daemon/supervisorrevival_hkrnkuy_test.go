package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers (helper-prefix: supervisorRevivalTest)
// ─────────────────────────────────────────────────────────────────────────────

// supervisorRevivalTestEmitter is a simple EventEmitter stub that records every
// Emit call for the detection function tests.
type supervisorRevivalTestEmitter struct {
	mu    sync.Mutex
	calls []supervisorRevivalTestEmit
}

type supervisorRevivalTestEmit struct {
	eventType core.EventType
	payload   []byte
}

func (e *supervisorRevivalTestEmitter) Emit(_ context.Context, et core.EventType, p []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, supervisorRevivalTestEmit{eventType: et, payload: p})
	return nil
}

func (e *supervisorRevivalTestEmitter) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, p []byte) error {
	return e.Emit(context.Background(), et, p)
}

func (e *supervisorRevivalTestEmitter) emitCount(et core.EventType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, c := range e.calls {
		if c.eventType == et {
			n++
		}
	}
	return n
}

// supervisorRevivalTestWriteJSONL writes a slice of raw event JSON objects (one per
// line) to a temp file and returns its path.
func supervisorRevivalTestWriteJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("supervisorRevivalTest: create JSONL: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, l := range lines {
		if _, werr := f.WriteString(l + "\n"); werr != nil {
			t.Fatalf("supervisorRevivalTest: write JSONL line: %v", werr)
		}
	}
	return path
}

// supervisorRevivalTestEvent builds a minimal JSONL line for a daemon lifecycle
// event. event_id is a deterministic non-zero UUID derived from the index so
// ScanAfter (which skips IDs ≤ zero UUID) yields every line.
func supervisorRevivalTestEvent(index int, eventType string, payload any) string {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		panic("supervisorRevivalTest: marshal payload: " + err.Error())
	}
	// Use a deterministic non-zero UUID based on index (16 bytes, first byte = index+1).
	uuid := [16]byte{}
	uuid[0] = byte(index + 1)
	uuidStr := fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16],
	)
	ev := map[string]any{
		"event_id":         uuidStr,
		"schema_version":   1,
		"type":             eventType,
		"timestamp_wall":   "2026-07-04T00:00:00Z",
		"source_subsystem": "daemon",
		"payload":          json.RawMessage(payloadBytes),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		panic("supervisorRevivalTest: marshal event: " + err.Error())
	}
	return string(b)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSupervisorRevival_EmptyPath verifies that an empty events path is a no-op.
func TestSupervisorRevival_EmptyPath(t *testing.T) {
	t.Parallel()
	em := &supervisorRevivalTestEmitter{}
	detectAndEmitSupervisorRevival(context.Background(), "", em)
	if em.emitCount(core.EventTypeSupervisorRevival) != 0 {
		t.Fatal("expected no supervisor_revival emit for empty path")
	}
}

// TestSupervisorRevival_SingleSession verifies that a JSONL with only one
// daemon_started (no prior session) does not emit supervisor_revival.
func TestSupervisorRevival_SingleSession(t *testing.T) {
	t.Parallel()
	lines := []string{
		supervisorRevivalTestEvent(0, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T10:00:00Z", PID: 100, BinaryCommitHash: "aabbcc",
		}),
	}
	path := supervisorRevivalTestWriteJSONL(t, lines)
	em := &supervisorRevivalTestEmitter{}
	detectAndEmitSupervisorRevival(context.Background(), path, em)
	if em.emitCount(core.EventTypeSupervisorRevival) != 0 {
		t.Fatal("expected no supervisor_revival emit for single session")
	}
}

// TestSupervisorRevival_PriorSessionGraceful verifies that when the prior
// daemon session ended with a daemon_shutdown event, supervisor_revival is NOT
// emitted.
func TestSupervisorRevival_PriorSessionGraceful(t *testing.T) {
	t.Parallel()
	lines := []string{
		// Prior session: started + shutdown (graceful).
		supervisorRevivalTestEvent(0, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T10:00:00Z", PID: 200, BinaryCommitHash: "deadbeef",
		}),
		supervisorRevivalTestEvent(1, "daemon_shutdown", map[string]string{"reason": "sigterm"}),
		// Current session (second daemon_started in log).
		supervisorRevivalTestEvent(2, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T11:00:00Z", PID: 300, BinaryCommitHash: "cafebabe",
		}),
	}
	path := supervisorRevivalTestWriteJSONL(t, lines)
	em := &supervisorRevivalTestEmitter{}
	detectAndEmitSupervisorRevival(context.Background(), path, em)
	if em.emitCount(core.EventTypeSupervisorRevival) != 0 {
		t.Fatal("expected no supervisor_revival emit when prior session shut down gracefully")
	}
}

// TestSupervisorRevival_PriorSessionUnexpected verifies that when the prior
// daemon session has no daemon_shutdown event, supervisor_revival IS emitted
// with the correct prior PID and binary hash.
func TestSupervisorRevival_PriorSessionUnexpected(t *testing.T) {
	t.Parallel()
	const wantPID = 54777
	const wantHash = "81434151abcdef"
	lines := []string{
		// Prior session: started but NO shutdown (crash / SIGKILL / OOM).
		supervisorRevivalTestEvent(0, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T17:14:00Z", PID: wantPID, BinaryCommitHash: wantHash,
		}),
		// Current session.
		supervisorRevivalTestEvent(1, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T17:17:00Z", PID: 61210, BinaryCommitHash: wantHash,
		}),
	}
	path := supervisorRevivalTestWriteJSONL(t, lines)
	em := &supervisorRevivalTestEmitter{}
	detectAndEmitSupervisorRevival(context.Background(), path, em)

	if got := em.emitCount(core.EventTypeSupervisorRevival); got != 1 {
		t.Fatalf("expected 1 supervisor_revival emit, got %d", got)
	}

	em.mu.Lock()
	payloadBytes := em.calls[0].payload
	em.mu.Unlock()

	var p core.SupervisorRevivalPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		t.Fatalf("unmarshal supervisor_revival payload: %v", err)
	}
	if p.PriorPID != wantPID {
		t.Errorf("PriorPID: got %d, want %d", p.PriorPID, wantPID)
	}
	if p.PriorBinaryCommitHash != wantHash {
		t.Errorf("PriorBinaryCommitHash: got %q, want %q", p.PriorBinaryCommitHash, wantHash)
	}
	if p.Cause != core.SupervisorRevivalCauseUnexpectedExit {
		t.Errorf("Cause: got %q, want %q", p.Cause, core.SupervisorRevivalCauseUnexpectedExit)
	}
	if p.RevivedAt == "" {
		t.Error("RevivedAt must be non-empty")
	}
}

// TestSupervisorRevival_ThreeSessions verifies that with three daemon sessions
// where the second-to-last had no shutdown, supervisor_revival is emitted with
// the prior session's PID.
func TestSupervisorRevival_ThreeSessions(t *testing.T) {
	t.Parallel()
	const wantPID = 11111
	lines := []string{
		// Session 1: clean shutdown.
		supervisorRevivalTestEvent(0, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T06:00:00Z", PID: 99, BinaryCommitHash: "first",
		}),
		supervisorRevivalTestEvent(1, "daemon_shutdown", map[string]string{"reason": "sigterm"}),
		// Session 2 (prior): no shutdown.
		supervisorRevivalTestEvent(2, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T06:36:00Z", PID: wantPID, BinaryCommitHash: "second",
		}),
		// Session 3 (current): clean start.
		supervisorRevivalTestEvent(3, "daemon_started", core.DaemonStartedPayload{
			StartedAt: "2026-07-04T06:57:00Z", PID: 22222, BinaryCommitHash: "third",
		}),
	}
	path := supervisorRevivalTestWriteJSONL(t, lines)
	em := &supervisorRevivalTestEmitter{}
	detectAndEmitSupervisorRevival(context.Background(), path, em)

	if got := em.emitCount(core.EventTypeSupervisorRevival); got != 1 {
		t.Fatalf("expected 1 supervisor_revival emit, got %d", got)
	}

	em.mu.Lock()
	payloadBytes := em.calls[0].payload
	em.mu.Unlock()

	var p core.SupervisorRevivalPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.PriorPID != wantPID {
		t.Errorf("PriorPID: got %d, want %d (should be session-2 PID, not session-1)", p.PriorPID, wantPID)
	}
}
