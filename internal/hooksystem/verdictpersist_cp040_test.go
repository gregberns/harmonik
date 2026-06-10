package hooksystem_test

// verdictpersist_cp040_test.go — requirement-traceable tests for CP-040 Hook
// verdict persistence (specs/control-points.md §4.8 CP-040).
//
// Coverage:
//  1. PersistHookVerdict writes the JSON verdict to the canonical path.
//  2. PersistHookVerdict emits hook_verdict_persisted with correct fields.
//  3. PersistHookVerdict rejects an invalid HookVerdictRecord (defence-in-depth).
//  4. PersistHookVerdict propagates a WriteAndCommit error.
//  5. PersistHookVerdict propagates an event-bus emit error.
//  6. The canonical verdict path follows .harmonik/hooks/<run_id>/<invocation_id>.json.
//
// All test-local identifiers use the cp040Persist prefix.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

// ---------------------------------------------------------------------------
// Test-local stub implementations
// ---------------------------------------------------------------------------

// cp040PersistFileWriter is a stub VerdictFileWriter that records what was
// written and returns a predetermined commit SHA.
type cp040PersistFileWriter struct {
	writtenPath     string
	writtenContents []byte
	returnSHA       string
	returnErr       error
}

func (w *cp040PersistFileWriter) WriteAndCommit(_ context.Context, relPath string, contents []byte) (string, error) {
	if w.returnErr != nil {
		return "", w.returnErr
	}
	w.writtenPath = relPath
	w.writtenContents = append([]byte(nil), contents...) // deep copy
	return w.returnSHA, nil
}

// cp040PersistBus is a stub EventBus that records emitted events.
type cp040PersistBus struct {
	events  []cp040PersistEvent
	emitErr error
}

type cp040PersistEvent struct {
	RunID     core.RunID
	EventType core.EventType
	Payload   json.RawMessage
}

func (b *cp040PersistBus) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	if b.emitErr != nil {
		return b.emitErr
	}
	b.events = append(b.events, cp040PersistEvent{RunID: runID, EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *cp040PersistBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.events = append(b.events, cp040PersistEvent{EventType: eventType, Payload: json.RawMessage(payload)})
	return nil
}

func (b *cp040PersistBus) Subscribe(_ core.Subscription) (core.Subscription, error) {
	return core.Subscription{}, nil
}
func (b *cp040PersistBus) Seal() error                                           { return nil }
func (b *cp040PersistBus) ReplayFrom(_ string, _ core.EventID) error             { return nil }
func (b *cp040PersistBus) DeadLetterReplay(_ string, _ *core.EventPattern) error { return nil }
func (b *cp040PersistBus) Drain(_ context.Context) error                         { return nil }

// Confirm cp040PersistBus satisfies the EventBus interface at compile time.
var _ eventbus.EventBus = (*cp040PersistBus)(nil)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func cp040PersistRunID() core.RunID {
	return core.RunID(uuid.MustParse("019e7309-1648-7412-9e67-000000000041"))
}

func cp040PersistInvocationID() uuid.UUID {
	return uuid.MustParse("019e7309-1648-7412-9e67-00000000aa41")
}

func cp040PersistSideEffect(t *testing.T) core.SideEffect {
	t.Helper()
	return core.SideEffect{
		Kind:             core.SideEffectKindEmitEvent,
		Target:           "hook.pre_merge.fired",
		Payload:          map[string]any{"run": "test"},
		IdempotencyClass: core.IdempotencyClassIdempotent,
	}
}

func cp040PersistVerdictFixture(t *testing.T) core.HookVerdictRecord {
	t.Helper()
	hash := strings.Repeat("a", 64)
	return core.HookVerdictRecord{
		HookName:          "pre-merge-hook",
		InvocationID:      cp040PersistInvocationID(),
		SideEffect:        cp040PersistSideEffect(t),
		Failed:            false,
		Reason:            nil,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-29T00:00:00Z",
	}
}

// ---------------------------------------------------------------------------
// (1) PersistHookVerdict writes the JSON verdict to the canonical path.
// ---------------------------------------------------------------------------

// TestPersistHookVerdict_WritesVerdictToCanonicalPath verifies that
// PersistHookVerdict calls WriteAndCommit with the canonical path
// .harmonik/hooks/<run_id>/<invocation_id>.json and with valid JSON content
// matching the verdict.
func TestPersistHookVerdict_WritesVerdictToCanonicalPath(t *testing.T) {
	t.Parallel()

	runID := cp040PersistRunID()
	verdict := cp040PersistVerdictFixture(t)

	writer := &cp040PersistFileWriter{returnSHA: "abc123commit"}
	bus := &cp040PersistBus{}

	if err := hooksystem.PersistHookVerdict(context.Background(), runID, verdict, writer, bus); err != nil {
		t.Fatalf("PersistHookVerdict: unexpected error: %v", err)
	}

	// Verify the path shape: .harmonik/hooks/<run_id>/<invocation_id>.json
	wantPath := core.HookVerdictFilePath(runID, verdict.InvocationID)
	if writer.writtenPath != wantPath {
		t.Errorf("written path = %q, want %q", writer.writtenPath, wantPath)
	}

	// Verify the written content is valid JSON that round-trips to the verdict.
	var decoded core.HookVerdictRecord
	if err := json.Unmarshal(writer.writtenContents, &decoded); err != nil {
		t.Fatalf("written contents are not valid JSON: %v", err)
	}
	if decoded.HookName != verdict.HookName {
		t.Errorf("decoded HookName = %q, want %q", decoded.HookName, verdict.HookName)
	}
	if decoded.InvocationID != verdict.InvocationID {
		t.Errorf("decoded InvocationID = %v, want %v", decoded.InvocationID, verdict.InvocationID)
	}
	if decoded.InputEnvelopeHash != verdict.InputEnvelopeHash {
		t.Errorf("decoded InputEnvelopeHash = %q, want %q", decoded.InputEnvelopeHash, verdict.InputEnvelopeHash)
	}
}

// ---------------------------------------------------------------------------
// (2) PersistHookVerdict emits hook_verdict_persisted with correct fields.
// ---------------------------------------------------------------------------

// TestPersistHookVerdict_EmitsHookVerdictPersistedEvent verifies that
// PersistHookVerdict emits exactly one hook_verdict_persisted event after
// writing the verdict, carrying all required fields per
// specs/event-model.md §8.2.3.
func TestPersistHookVerdict_EmitsHookVerdictPersistedEvent(t *testing.T) {
	t.Parallel()

	runID := cp040PersistRunID()
	verdict := cp040PersistVerdictFixture(t)
	wantCommitSHA := "deadbeef1234567890abcdef1234567890abcdef"

	writer := &cp040PersistFileWriter{returnSHA: wantCommitSHA}
	bus := &cp040PersistBus{}

	if err := hooksystem.PersistHookVerdict(context.Background(), runID, verdict, writer, bus); err != nil {
		t.Fatalf("PersistHookVerdict: unexpected error: %v", err)
	}

	// Exactly one event must be emitted.
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}

	ev := bus.events[0]
	if ev.EventType != core.EventTypeHookVerdictPersisted {
		t.Errorf("event type = %q, want %q", ev.EventType, core.EventTypeHookVerdictPersisted)
	}
	if ev.RunID != runID {
		t.Errorf("event RunID = %v, want %v", ev.RunID, runID)
	}

	// Decode and verify payload fields.
	var payload core.HookVerdictPersistedPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("unmarshal event payload: %v", err)
	}
	if !payload.Valid() {
		t.Error("HookVerdictPersistedPayload.Valid() = false; want true")
	}
	if payload.HookInvocationID != verdict.InvocationID.String() {
		t.Errorf("payload.HookInvocationID = %q, want %q", payload.HookInvocationID, verdict.InvocationID.String())
	}
	if string(payload.HookName) != verdict.HookName {
		t.Errorf("payload.HookName = %q, want %q", payload.HookName, verdict.HookName)
	}
	wantPath := core.HookVerdictFilePath(runID, verdict.InvocationID)
	if payload.VerdictPath != wantPath {
		t.Errorf("payload.VerdictPath = %q, want %q", payload.VerdictPath, wantPath)
	}
	if payload.CommitHash != wantCommitSHA {
		t.Errorf("payload.CommitHash = %q, want %q", payload.CommitHash, wantCommitSHA)
	}
}

// ---------------------------------------------------------------------------
// (3) PersistHookVerdict rejects an invalid HookVerdictRecord.
// ---------------------------------------------------------------------------

// TestPersistHookVerdict_RejectsInvalidVerdict verifies that PersistHookVerdict
// returns an error when the verdict is not Valid() (defence-in-depth guard
// per CP-040 §6.1.6).
func TestPersistHookVerdict_RejectsInvalidVerdict(t *testing.T) {
	t.Parallel()

	runID := cp040PersistRunID()
	// Invalid: HookName is empty.
	invalid := core.HookVerdictRecord{
		HookName:          "", // required
		InvocationID:      cp040PersistInvocationID(),
		SideEffect:        cp040PersistSideEffect(t),
		InputEnvelopeHash: strings.Repeat("b", 64),
		ProducedAt:        "2026-05-29T00:00:00Z",
	}

	writer := &cp040PersistFileWriter{returnSHA: "irrelevant"}
	bus := &cp040PersistBus{}

	err := hooksystem.PersistHookVerdict(context.Background(), runID, invalid, writer, bus)
	if err == nil {
		t.Fatal("expected error for invalid HookVerdictRecord, got nil")
	}

	// No write and no event should have been attempted.
	if writer.writtenPath != "" {
		t.Errorf("WriteAndCommit was called despite invalid verdict (path=%q)", writer.writtenPath)
	}
	if len(bus.events) != 0 {
		t.Errorf("event emitted despite invalid verdict (count=%d)", len(bus.events))
	}
}

// ---------------------------------------------------------------------------
// (4) PersistHookVerdict propagates a WriteAndCommit error.
// ---------------------------------------------------------------------------

// TestPersistHookVerdict_PropagatesWriteError verifies that when
// WriteAndCommit returns an error, PersistHookVerdict returns an error
// and emits no event (the write must precede the event per CP-040).
func TestPersistHookVerdict_PropagatesWriteError(t *testing.T) {
	t.Parallel()

	runID := cp040PersistRunID()
	verdict := cp040PersistVerdictFixture(t)

	writeErr := errors.New("ENOSPC: no space left on device")
	writer := &cp040PersistFileWriter{returnErr: writeErr}
	bus := &cp040PersistBus{}

	err := hooksystem.PersistHookVerdict(context.Background(), runID, verdict, writer, bus)
	if err == nil {
		t.Fatal("expected error when WriteAndCommit fails, got nil")
	}
	if !errors.Is(err, writeErr) {
		t.Errorf("error does not wrap writeErr: got %v", err)
	}

	// Event MUST NOT be emitted when the write failed.
	if len(bus.events) != 0 {
		t.Errorf("hook_verdict_persisted emitted despite write failure (count=%d)", len(bus.events))
	}
}

// ---------------------------------------------------------------------------
// (5) PersistHookVerdict propagates an event-bus emit error.
// ---------------------------------------------------------------------------

// TestPersistHookVerdict_PropagatesEmitError verifies that when the event bus
// returns an error, PersistHookVerdict returns that error. The write succeeded
// (the file is on the task branch) but the event emission failed.
func TestPersistHookVerdict_PropagatesEmitError(t *testing.T) {
	t.Parallel()

	runID := cp040PersistRunID()
	verdict := cp040PersistVerdictFixture(t)

	writer := &cp040PersistFileWriter{returnSHA: "sha1234"}
	emitErr := errors.New("bus sealed, cannot emit")
	bus := &cp040PersistBus{emitErr: emitErr}

	err := hooksystem.PersistHookVerdict(context.Background(), runID, verdict, writer, bus)
	if err == nil {
		t.Fatal("expected error when emit fails, got nil")
	}
	if !errors.Is(err, emitErr) {
		t.Errorf("error does not wrap emitErr: got %v", err)
	}

	// The write DID happen (file is on branch); only the event failed.
	if writer.writtenPath == "" {
		t.Error("WriteAndCommit was not called despite valid verdict and no write error")
	}
}

// ---------------------------------------------------------------------------
// (6) The canonical verdict path follows .harmonik/hooks/<run_id>/<invocation_id>.json
// ---------------------------------------------------------------------------

// TestHookVerdictFilePath_CP040Shape verifies that HookVerdictFilePath produces
// the canonical path shape required by specs/control-points.md §4.8.CP-040:
//
//	.harmonik/hooks/<run_id>/<invocation_id>.json
func TestHookVerdictFilePath_CP040Shape(t *testing.T) {
	t.Parallel()

	runID := core.RunID(uuid.Must(uuid.NewV7()))
	invocationID := uuid.Must(uuid.NewV7())

	got := core.HookVerdictFilePath(runID, invocationID)

	runStr := runID.String()
	invStr := invocationID.String()

	if !strings.HasPrefix(got, ".harmonik/hooks/") {
		t.Errorf("HookVerdictFilePath = %q, want prefix .harmonik/hooks/", got)
	}
	if !strings.HasSuffix(got, ".json") {
		t.Errorf("HookVerdictFilePath = %q, want suffix .json", got)
	}
	if !strings.Contains(got, runStr) {
		t.Errorf("HookVerdictFilePath = %q, does not contain run_id %q", got, runStr)
	}
	if !strings.Contains(got, invStr) {
		t.Errorf("HookVerdictFilePath = %q, does not contain invocation_id %q", got, invStr)
	}

	want := ".harmonik/hooks/" + runStr + "/" + invStr + ".json"
	if got != want {
		t.Errorf("HookVerdictFilePath = %q, want %q", got, want)
	}
}
