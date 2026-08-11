package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

type intentDurabilityEmitter struct {
	mu       sync.Mutex
	types    []core.EventType
	payloads [][]byte
	err      error
}

func (e *intentDurabilityEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, eventType)
	e.payloads = append(e.payloads, append([]byte(nil), payload...))
	return e.err
}

func (e *intentDurabilityEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return e.Emit(ctx, "", nil)
}

func (e *intentDurabilityEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.types)
}

func (e *intentDurabilityEmitter) event(index int) (eventType core.EventType, payload []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.types[index], append([]byte(nil), e.payloads[index]...)
}

type intentDurabilityLedger struct{}

type intentCompletionStore func(context.Context, queue.CompletionRequest) queue.CompletionResult

func (f intentCompletionStore) Complete(ctx context.Context, req queue.CompletionRequest) queue.CompletionResult {
	return f(ctx, req)
}

func (intentDurabilityLedger) LookupStatus(context.Context, core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (intentDurabilityLedger) BlocksEdge(context.Context, core.BeadID, core.BeadID) (bool, error) {
	return false, nil
}

func intentDurabilityBlockedProjectDir(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	return path
}

func TestGroupCompletionCommitFailureEmitsNoIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-000000000101",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: "hk-intent-terminal", Status: queue.ItemStatusPending}},
		}},
	}
	store.SetQueue(q)
	port := reapSeamPort{
		bus:           emitter,
		projectDir:    intentDurabilityBlockedProjectDir(t),
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 1,
	}

	evaluateGroupAdvanceWithOutcome(t.Context(), port, queue.QueueNameMain, q.QueueID, 0, 0, true, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	if got := emitter.count(); got != 0 {
		t.Fatalf("emit calls = %d, want 0 after completion commit failure", got)
	}
	retained := store.Queue()
	if retained == nil || retained.Status != queue.QueueStatusActive || retained.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Fatalf("failed commit installed decision candidate in memory: %+v", retained)
	}
}

func TestGroupCompletionCleanupFailureEmitsCommittedIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-000000000102",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: "hk-intent-cleanup", Status: queue.ItemStatusPending}},
		}},
	}
	store.SetQueue(q)
	var completionRequest queue.CompletionRequest
	port := reapSeamPort{
		bus:           emitter,
		projectDir:    t.TempDir(),
		queueStore:    store,
		runRegistry:   newLocalRunRegistry(),
		maxConcurrent: 1,
		completionStore: intentCompletionStore(func(_ context.Context, req queue.CompletionRequest) queue.CompletionResult {
			completionRequest = req
			observationErr := req.Observe(queue.CompletionReceipt{})
			return queue.CompletionResult{
				NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable},
				Phase:           queue.CompletionPhaseObservationAttempted,
				ObservationErr:  observationErr,
				CleanupErr:      errors.New("cleanup failed"),
			}
		}),
	}

	evaluateGroupAdvanceWithOutcome(t.Context(), port, queue.QueueNameMain, q.QueueID, 0, 0, true, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	if got := emitter.count(); got != 1 {
		t.Fatalf("emit calls = %d, want 1 after durable completion with cleanup failure", got)
	}
	if completionRequest.ReceiptID == "" || completionRequest.ReceiptID != completionRequest.DecisionInput.CompletionReceiptID {
		t.Fatalf("completion receipt handshake = request %q decision %q", completionRequest.ReceiptID, completionRequest.DecisionInput.CompletionReceiptID)
	}
	eventType, payloadBytes := emitter.event(0)
	if eventType != core.EventTypeQueueGroupCompleted {
		t.Fatalf("event type = %q", eventType)
	}
	var payload core.QueueGroupCompletedPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CompletionReceiptID != completionRequest.ReceiptID {
		t.Fatalf("event receipt = %q, request receipt = %q", payload.CompletionReceiptID, completionRequest.ReceiptID)
	}
}

func TestFinalGroupCompletionMintFailureKeepsOriginalDiagnostic(t *testing.T) {
	want := errors.New("mint failed")
	execution := failedGroupCompletionExecution(queue.GroupCompletionDispositionQueueCompleted, want)
	effects, err := decideGroupCompletionEffects(execution.durability)
	if err != nil {
		t.Fatalf("policy replaced mint diagnostic: %v", err)
	}
	if !errors.Is(execution.err, want) || !effects.LogFailure || !effects.Refill {
		t.Fatalf("execution=%+v effects=%+v", execution, effects)
	}
}

func TestFinalCompletionExecutorMapsEveryStoreFaultPhase(t *testing.T) {
	stamp := time.Date(2026, 8, 11, 12, 0, 0, 123000000, time.UTC)
	q := queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197d001-0000-7000-8000-000000000001",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: "hk-final-phase", Status: queue.ItemStatusDispatched}},
		}},
	}
	input := queue.GroupCompletionInput{
		ExpectedQueueID:     q.QueueID,
		Location:            queue.GroupCompletionLocation{GroupIndex: 0, ItemIndex: 0},
		Outcome:             queue.GroupCompletionOutcomeCompleted,
		CompletedAt:         stamp,
		CompletionReceiptID: "0197d001-0000-7000-8000-000000000002",
	}
	decision, err := queue.DecideGroupCompletion(q, input)
	if err != nil || decision.Disposition != queue.GroupCompletionDispositionQueueCompleted {
		t.Fatalf("decision = %+v, err=%v", decision, err)
	}
	diagnostic := errors.New("fault diagnostic")
	tests := []struct {
		name       string
		result     queue.CompletionResult
		observe    bool
		wantErrors bool
	}{
		{name: "not committed", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeNotCommitted, Err: diagnostic}, Phase: queue.CompletionPhaseNotCommitted}, wantErrors: true},
		{name: "commit indeterminate", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommitIndeterminate, Err: diagnostic}, Phase: queue.CompletionPhaseCommitIndeterminate}, wantErrors: true},
		{name: "canonical committed receipt fault", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommitIndeterminate, Err: diagnostic}, Phase: queue.CompletionPhaseCanonicalCommitted}, wantErrors: true},
		{name: "receipt durable", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable}, Phase: queue.CompletionPhaseReceiptDurable}},
		{name: "observation fault", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable}, Phase: queue.CompletionPhaseObservationAttempted, ObservationErr: diagnostic}, observe: true, wantErrors: true},
		{name: "observation cleanup fault", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable}, Phase: queue.CompletionPhaseObservationAttempted, CleanupErr: diagnostic}, observe: true, wantErrors: true},
		{name: "marker fault", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable}, Phase: queue.CompletionPhaseMarkerFailed, MarkerErr: diagnostic}, observe: true, wantErrors: true},
		{name: "marker durable", result: queue.CompletionResult{NamespaceResult: queue.NamespaceResult{Outcome: queue.OutcomeCommittedDurable}, Phase: queue.CompletionPhaseMarkerDurable}, observe: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			emitter := &intentDurabilityEmitter{}
			observeCalls := 0
			port := reapSeamPort{
				bus:         emitter,
				projectDir:  t.TempDir(),
				queueStore:  queuewiring.NewQueueStore(),
				runRegistry: newLocalRunRegistry(),
				completionStore: intentCompletionStore(func(_ context.Context, req queue.CompletionRequest) queue.CompletionResult {
					if req.ReceiptID != input.CompletionReceiptID || req.Candidate != decision.NextQueue {
						t.Fatalf("completion request lost decision binding: %+v", req)
					}
					if tc.observe {
						observeCalls++
						if err := req.Observe(queue.CompletionReceipt{ReceiptID: req.ReceiptID}); err != nil {
							t.Fatal(err)
						}
					}
					return tc.result
				}),
			}
			execution := executeFinalGroupCompletion(t.Context(), port, queue.QueueSnapshot{Name: q.Name, Queue: &q, Generation: 1}, decision, input)
			want := groupCompletionDurability{
				Disposition:      queue.GroupCompletionDispositionQueueCompleted,
				Outcome:          tc.result.Outcome,
				Phase:            tc.result.Phase,
				ObservationError: tc.result.ObservationErr != nil,
				CleanupError:     tc.result.CleanupErr != nil,
				MarkerError:      tc.result.MarkerErr != nil,
			}
			if execution.durability != want {
				t.Fatalf("durability = %+v, want %+v", execution.durability, want)
			}
			if (execution.err != nil) != tc.wantErrors {
				t.Fatalf("execution error = %v, wantErrors=%v", execution.err, tc.wantErrors)
			}
			if got := emitter.count(); got != observeCalls || observeCalls > 1 {
				t.Fatalf("observation emits = %d, observe calls = %d", got, observeCalls)
			}
			effects, err := decideGroupCompletionEffects(execution.durability)
			if err != nil {
				t.Fatal(err)
			}
			if effects.EmitIntents || effects.Wake || effects.CancelQueueDrain != completionReleasedOwnership(tc.result.Phase) {
				t.Fatalf("effects = %+v", effects)
			}
		})
	}
}

func TestEagerRefillPersistFailureEmitsNoIntent(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	store.SetQueue(em063FixtureStreamQueueWithBeads())

	root := t.TempDir()
	kerfPath := filepath.Join(root, "kerf")
	writeTestFile(t, kerfPath, "#!/bin/sh\nprintf '[{\"bead_id\":\"hk-eager-intent\"}]\\n'\n")
	//nolint:gosec // G302: the fixture is a shell script this test runs as a subprocess, so it needs the owner execute bit. 0o700 is the tightest mode that still runs.
	if err := os.Chmod(kerfPath, 0o700); err != nil {
		t.Fatalf("chmod kerf fixture: %v", err)
	}

	deps := em063FixtureDeps(t, store)
	deps.env.ProjectDir = intentDurabilityBlockedProjectDir(t)
	deps.ports.Emitter = emitter
	deps.queueSurface = newQueueSurfacePort(nil, intentDurabilityLedger{})
	port := deps.reap(eagerRefillPort{kerfPath: kerfPath})

	eagerRefillEval(t.Context(), port)

	if got := emitter.count(); got != 0 {
		t.Fatalf("emit calls = %d, want 0 after eager-refill persist failure", got)
	}
}

func TestEagerRefillStaleQueueIdentityDoesNotMutateReplacement(t *testing.T) {
	emitter := &intentDurabilityEmitter{}
	store := queuewiring.NewQueueStore()
	original := em063FixtureStreamQueueWithBeads()
	store.SetQueue(original)

	root := t.TempDir()
	readyPath := filepath.Join(root, "ready")
	releasePath := filepath.Join(root, "release")
	kerfPath := filepath.Join(root, "kerf")
	script := "#!/bin/sh\n: > '" + readyPath + "'\nwhile [ ! -e '" + releasePath + "' ]; do sleep 0.01; done\nprintf '[{\"bead_id\":\"hk-stale-intent\"}]\\n'\n"
	writeTestFile(t, kerfPath, script)
	//nolint:gosec // G302: the fixture is a shell script this test runs as a subprocess, so it needs the owner execute bit. 0o700 is the tightest mode that still runs.
	if err := os.Chmod(kerfPath, 0o700); err != nil {
		t.Fatalf("chmod kerf fixture: %v", err)
	}

	deps := em063FixtureDeps(t, store)
	deps.ports.Emitter = emitter
	deps.queueSurface = newQueueSurfacePort(nil, intentDurabilityLedger{})
	port := deps.reap(eagerRefillPort{kerfPath: kerfPath})
	done := make(chan struct{})
	go func() {
		defer close(done)
		eagerRefillEval(t.Context(), port)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("kerf fixture did not reach replacement window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	replacement := em063FixtureStreamQueueWithBeads("hk-replacement")
	store.SetQueue(replacement)
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatalf("release kerf fixture: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("eager refill did not return after queue replacement")
	}

	got := store.Queue()
	if got == nil || got.QueueID != replacement.QueueID {
		t.Fatalf("active queue = %+v, want replacement %s", got, replacement.QueueID)
	}
	if len(got.Groups[0].Items) != 1 || got.Groups[0].Items[0].BeadID != "hk-replacement" {
		t.Fatalf("replacement items = %+v, want only hk-replacement", got.Groups[0].Items)
	}
	if gotCalls := emitter.count(); gotCalls != 0 {
		t.Fatalf("emit calls = %d, want 0 for stale queue identity", gotCalls)
	}
}
