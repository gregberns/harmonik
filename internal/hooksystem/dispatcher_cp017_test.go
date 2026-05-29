package hooksystem_test

// dispatcher_cp017_test.go — requirement-traceable tests for CP-017
// "Hook evaluator MAY be cognition-tagged".
//
// Spec ref: specs/control-points.md §4.3.CP-017, §4.8 CP-039–CP-042, §7.2.
// Bead ref: hk-a8bg.16
//
// Coverage:
//
//	CP-017.1: Cognition hook fires hook_fired when evaluator returns success.
//	CP-017.2: Cognition hook emits hook_failed when evaluator returns failure verdict.
//	CP-017.3: Cognition hook fails deterministically without run-scoped event.
//	CP-017.4: Cognition hook fails deterministically without wired evaluator.
//	CP-017.5: Replay with matching hash consumes persisted verdict (no re-dispatch).
//	CP-017.6: Replay with mismatched hash emits verdict_envelope_mismatch + hook_failed.
//	CP-017.7: WithCognition panics when any arg is nil.
//
// All test-local identifiers use the cp017 prefix per implementer-protocol.md
// helper-prefix discipline.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

// ---------------------------------------------------------------------------
// Stub implementations
// ---------------------------------------------------------------------------

// cp017StubCognitionEval is a stub CognitionHookEvaluator.
type cp017StubCognitionEval struct {
	returnVerdict core.HookVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cp017StubCognitionEval) EvaluateCognitionHook(_ context.Context, _ core.ControlPoint, _ core.Event) (core.HookVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

// cp017StubVerdictWriter is a stub VerdictFileWriter that records writes.
type cp017StubVerdictWriter struct {
	writtenPath string
	returnSHA   string
	returnErr   error
}

func (w *cp017StubVerdictWriter) WriteAndCommit(_ context.Context, relPath string, _ []byte) (string, error) {
	if w.returnErr != nil {
		return "", w.returnErr
	}
	w.writtenPath = relPath
	return w.returnSHA, nil
}

// cp017StubVerdictReader is a stub VerdictReader.
type cp017StubVerdictReader struct {
	found   bool
	verdict core.HookVerdictRecord
	readErr error
}

func (r *cp017StubVerdictReader) LookupVerdict(_ context.Context, _ core.RunID, _ string, _ core.EventID) (core.HookVerdictRecord, bool, error) {
	if r.readErr != nil {
		return core.HookVerdictRecord{}, false, r.readErr
	}
	return r.verdict, r.found, nil
}

// Compile-time interface satisfaction checks.
var _ hooksystem.CognitionHookEvaluator = (*cp017StubCognitionEval)(nil)
var _ hooksystem.VerdictFileWriter = (*cp017StubVerdictWriter)(nil)
var _ hooksystem.VerdictReader = (*cp017StubVerdictReader)(nil)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func cp017RunID() core.RunID {
	return core.RunID(uuid.MustParse("019e7342-0000-7000-a000-000000000017"))
}

func cp017DelegationPath() core.DelegationPath {
	return core.DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "hook.review.input.v1",
		ResponseSchemaRef: "hook.review.response.v1",
		PromptTemplateRef: "hook.review.prompt.v1",
	}
}

func cp017MakeCognitionHookCP(name, triggerEvent string, sideEffectKind core.SideEffectKind, haltOnFailure bool) core.ControlPoint {
	dp := cp017DelegationPath()
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagCognition, DelegationPath: &dp},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:      triggerEvent,
				SideEffectKind:    sideEffectKind,
				HaltOnFailure:     haltOnFailure,
				SubsystemPriority: 0,
				IdempotencyClass:  core.IdempotencyClassNonIdempotent,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagCognition,
		SchemaVersion: 1,
	}
}

// cp017ComputeEnvelopeHash mirrors the production logic in
// computeHookEnvelopeHash (unexported). Used to build persisted verdicts with
// the correct hash for replay tests. Any drift from the production function
// surfaces as hash-mismatch failures in CP-017.5.
func cp017ComputeEnvelopeHash(t *testing.T, cp core.ControlPoint, evPayload json.RawMessage) string {
	t.Helper()
	type hookInputEnvelope struct {
		ControlPointName string              `json:"control_point_name"`
		DelegationPath   core.DelegationPath `json:"delegation_path"`
		EventPayload     json.RawMessage     `json:"event_payload"`
		SchemaVersion    int                 `json:"schema_version"`
	}
	envelope := hookInputEnvelope{
		ControlPointName: cp.Name,
		DelegationPath:   *cp.Evaluator.DelegationPath,
		EventPayload:     evPayload,
		SchemaVersion:    cp.SchemaVersion,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("cp017ComputeEnvelopeHash: marshal: %v", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// cp017BuildBusWithCollector builds a bus + event-type collector, applies
// extra subscriptions, and seals. Reuses cp012FixtureEventCollector from
// dispatcher_cp012_test.go (same test package).
func cp017BuildBusWithCollector(t *testing.T, collector *cp012FixtureEventCollector, extraSubs ...func(eventbus.EventBus) error) eventbus.EventBus {
	t.Helper()
	bus := eventbus.NewBusImpl()
	for _, fn := range extraSubs {
		if err := fn(bus); err != nil {
			t.Fatalf("cp017BuildBusWithCollector: extra sub: %v", err)
		}
	}
	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.cp017.collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			collector.record(string(ev.Type))
			return nil
		},
	}); err != nil {
		t.Fatalf("cp017BuildBusWithCollector: subscribe collector: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("cp017BuildBusWithCollector: Seal: %v", err)
	}
	return bus
}

// cp017EmitRunEvent emits an event scoped to a runID.
func cp017EmitRunEvent(t *testing.T, bus eventbus.EventBus, eventType string, runID core.RunID) json.RawMessage {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	if err := bus.EmitWithRunID(context.Background(), runID, core.EventType(eventType), payload); err != nil {
		t.Fatalf("EmitWithRunID(%q): %v", eventType, err)
	}
	return payload
}

// cp017MakeSuccessVerdict builds a non-failing HookVerdictRecord stub.
func cp017MakeSuccessVerdict(hookName string) core.HookVerdictRecord {
	return core.HookVerdictRecord{
		HookName:     hookName,
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-0000000000aa"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           hookName,
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: strings.Repeat("a", 64),
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

// ---------------------------------------------------------------------------
// CP-017.1: Cognition hook fires hook_fired on success
// ---------------------------------------------------------------------------

// TestCP017_CognitionHookFiresOnSuccess verifies that a cognition-tagged Hook
// dispatches to the evaluator and emits hook_fired when the evaluator returns
// a non-failed verdict per CP-017.
func TestCP017_CognitionHookFiresOnSuccess(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	eval := &cp017StubCognitionEval{returnVerdict: cp017MakeSuccessVerdict("review-hook")}
	writer := &cp017StubVerdictWriter{returnSHA: "abc123"}
	reader := &cp017StubVerdictReader{found: false}

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})
	_ = disp

	cp017EmitRunEvent(t, bus, "agent_started", cp017RunID())
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	found := false
	for _, et := range events {
		if et == "hook_fired" {
			found = true
		}
	}
	if !found {
		t.Errorf("CP-017.1: hook_fired not emitted for cognition hook success; events: %v", events)
	}
	if eval.callCount != 1 {
		t.Errorf("CP-017.1: evaluator called %d times, want 1", eval.callCount)
	}
}

// ---------------------------------------------------------------------------
// CP-017.2: Cognition hook emits hook_failed on failure verdict
// ---------------------------------------------------------------------------

// TestCP017_CognitionHookEmitsHookFailedOnFailureVerdict verifies that when
// the cognition evaluator returns a verdict with Failed=true, the dispatcher
// emits hook_failed (not hook_fired) per CP-017.
func TestCP017_CognitionHookEmitsHookFailedOnFailureVerdict(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	reason := "reviewer rejected: insufficient quality"
	failedVerdict := core.HookVerdictRecord{
		HookName:     "review-hook",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-0000000000bb"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            true,
		Reason:            &reason,
		InputEnvelopeHash: strings.Repeat("b", 64),
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	eval := &cp017StubCognitionEval{returnVerdict: failedVerdict}
	writer := &cp017StubVerdictWriter{returnSHA: "abc123"}
	reader := &cp017StubVerdictReader{found: false}

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})
	_ = disp

	cp017EmitRunEvent(t, bus, "agent_started", cp017RunID())
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	hasFailed := false
	hasFired := false
	for _, et := range events {
		if et == "hook_failed" {
			hasFailed = true
		}
		if et == "hook_fired" {
			hasFired = true
		}
	}
	if !hasFailed {
		t.Errorf("CP-017.2: hook_failed not emitted when evaluator returned failure verdict; events: %v", events)
	}
	if hasFired {
		t.Errorf("CP-017.2: hook_fired emitted despite failure verdict; events: %v", events)
	}
}

// ---------------------------------------------------------------------------
// CP-017.3: Cognition hook requires run-scoped event
// ---------------------------------------------------------------------------

// TestCP017_CognitionHookRequiresRunScopedEvent verifies that a cognition hook
// emits hook_failed when the triggering event has no RunID (unscoped events
// are forbidden for cognition hooks per OQ-CP-004 default per
// specs/control-points.md §10).
func TestCP017_CognitionHookRequiresRunScopedEvent(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	eval := &cp017StubCognitionEval{}
	writer := &cp017StubVerdictWriter{returnSHA: "sha"}
	reader := &cp017StubVerdictReader{}

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})
	_ = disp

	// Emit without a RunID (plain Emit, not EmitWithRunID).
	payload, _ := json.Marshal(map[string]any{})
	if err := bus.Emit(context.Background(), "agent_started", payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	hasFailed := false
	for _, et := range events {
		if et == "hook_failed" {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Errorf("CP-017.3: hook_failed not emitted for unscoped event; events: %v", events)
	}
	if eval.callCount != 0 {
		t.Errorf("CP-017.3: evaluator called %d times for unscoped event, want 0", eval.callCount)
	}
}

// ---------------------------------------------------------------------------
// CP-017.4: Cognition hook without wired evaluator emits hook_failed
// ---------------------------------------------------------------------------

// TestCP017_CognitionHookWithoutEvaluatorEmitsHookFailed verifies that a
// cognition hook emits hook_failed when the Dispatcher has no cognition
// components wired (WithCognition not called).
func TestCP017_CognitionHookWithoutEvaluatorEmitsHookFailed(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		// WithCognition NOT called — cognition not wired.
		return disp.Subscribe()
	})
	_ = disp

	cp017EmitRunEvent(t, bus, "agent_started", cp017RunID())
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	hasFailed := false
	for _, et := range events {
		if et == "hook_failed" {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Errorf("CP-017.4: hook_failed not emitted for unwired cognition evaluator; events: %v", events)
	}
}

// ---------------------------------------------------------------------------
// CP-017.5: Replay with matching hash consumes persisted verdict
// ---------------------------------------------------------------------------

// TestCP017_ReplayMatchingHashConsumesPersistedVerdict verifies that when a
// persisted verdict exists with a matching envelope hash, the dispatcher
// returns the persisted verdict without re-invoking the cognition evaluator
// per CP-041 / CP-INV-003.
func TestCP017_ReplayMatchingHashConsumesPersistedVerdict(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	// Compute the envelope hash that the dispatcher will compute for the event
	// we're about to emit, so we can pre-seed the reader with the correct hash.
	runID := cp017RunID()
	evPayload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	correctHash := cp017ComputeEnvelopeHash(t, cp, evPayload)

	persistedVerdict := core.HookVerdictRecord{
		HookName:     "review-hook",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-0000000000cc"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: correctHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp017StubCognitionEval{} // MUST NOT be called on replay
	writer := &cp017StubVerdictWriter{returnSHA: "sha-replay"}
	reader := &cp017StubVerdictReader{found: true, verdict: persistedVerdict}

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})
	_ = disp

	if err := bus.EmitWithRunID(context.Background(), runID, "agent_started", evPayload); err != nil {
		t.Fatalf("EmitWithRunID: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	// Evaluator MUST NOT be called on replay with matching hash (CP-INV-003).
	if eval.callCount != 0 {
		t.Errorf("CP-017.5: evaluator called %d times on replay with matching hash, want 0 (CP-INV-003)", eval.callCount)
	}

	// hook_fired MUST be emitted from the replayed verdict.
	events := collector.all()
	found := false
	for _, et := range events {
		if et == "hook_fired" {
			found = true
		}
	}
	if !found {
		t.Errorf("CP-017.5: hook_fired not emitted on replay with matching hash; events: %v", events)
	}
}

// ---------------------------------------------------------------------------
// CP-017.6: Replay with mismatched hash emits verdict_envelope_mismatch
// ---------------------------------------------------------------------------

// TestCP017_ReplayMismatchedHashEmitsVerdictEnvelopeMismatch verifies that
// when a persisted verdict exists but the envelope hash does not match, the
// dispatcher emits verdict_envelope_mismatch and hook_failed without
// re-invoking the evaluator per CP-041.
func TestCP017_ReplayMismatchedHashEmitsVerdictEnvelopeMismatch(t *testing.T) {
	t.Parallel()

	cp := cp017MakeCognitionHookCP("review-hook", "on_agent_started", core.SideEffectKindEmitEvent, false)
	reg := cp012FixtureNewRegistry(cp)

	staleHash := strings.Repeat("f", 64) // deliberately wrong hash
	persistedVerdict := core.HookVerdictRecord{
		HookName:     "review-hook",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-0000000000dd"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: staleHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp017StubCognitionEval{} // MUST NOT be called
	writer := &cp017StubVerdictWriter{}
	reader := &cp017StubVerdictReader{found: true, verdict: persistedVerdict}

	collector := &cp012FixtureEventCollector{}
	var disp *hooksystem.Dispatcher
	bus := cp017BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})
	_ = disp

	cp017EmitRunEvent(t, bus, "agent_started", cp017RunID())
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	hasMismatch := false
	hasFailed := false
	hasFired := false
	for _, et := range events {
		switch et {
		case string(core.EventTypeVerdictEnvelopeMismatch):
			hasMismatch = true
		case "hook_failed":
			hasFailed = true
		case "hook_fired":
			hasFired = true
		}
	}
	if !hasMismatch {
		t.Errorf("CP-017.6: verdict_envelope_mismatch not emitted; events: %v", events)
	}
	if !hasFailed {
		t.Errorf("CP-017.6: hook_failed not emitted after hash mismatch; events: %v", events)
	}
	if hasFired {
		t.Errorf("CP-017.6: hook_fired emitted despite hash mismatch; events: %v", events)
	}
	if eval.callCount != 0 {
		t.Errorf("CP-017.6: evaluator called %d times on hash mismatch, want 0 (CP-041)", eval.callCount)
	}
}

// ---------------------------------------------------------------------------
// CP-017.7: WithCognition panics on nil arg
// ---------------------------------------------------------------------------

// TestCP017_WithCognitionPanicsOnNilArg verifies that Dispatcher.WithCognition
// panics when any argument is nil, surfacing misconfiguration at construction
// time.
func TestCP017_WithCognitionPanicsOnNilArg(t *testing.T) {
	t.Parallel()

	reg := cp012FixtureNewRegistry()
	bus := eventbus.NewBusImpl()
	_ = bus.Seal()

	eval := &cp017StubCognitionEval{}
	writer := &cp017StubVerdictWriter{}
	reader := &cp017StubVerdictReader{}

	cases := []struct {
		name   string
		eval   hooksystem.CognitionHookEvaluator
		writer hooksystem.VerdictFileWriter
		reader hooksystem.VerdictReader
	}{
		{"nil eval", nil, writer, reader},
		{"nil writer", eval, nil, reader},
		{"nil reader", eval, writer, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CP-017.7: WithCognition(%s) did not panic, want panic", tc.name)
				}
			}()
			disp := hooksystem.NewDispatcher(reg, bus)
			disp.WithCognition(tc.eval, tc.writer, tc.reader)
		})
	}
}
