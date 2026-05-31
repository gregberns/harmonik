package hooksystem_test

// dispatcher_cp042_test.go — requirement-traceable tests for CP-042
// "Verdict persistence is the boundary between mechanism and cognition".
//
// Spec ref: specs/control-points.md §4.8.CP-042.
// Bead ref: hk-a8bg.44
//
// Coverage:
//
//	CP-042.1: Production (cognition) and persistence (mechanism) are separate operations:
//	          evaluator called once; writer receives the stamped verdict at the canonical path.
//	CP-042.2: Writer path is exactly the canonical form declared by core.HookVerdictFilePath.
//	CP-042.3: Writer is NOT called when the cognition evaluator errors
//	          (no valid verdict to persist → mechanism side must not write).
//	CP-042.4: Writer is NOT called on replay (hash match) — mechanism persister is
//	          non-idempotent and must not duplicate the git write.
//
// The split invariant enforced here:
//   - CognitionHookEvaluator.EvaluateCognitionHook — Tags: cognition;
//     idempotency=idempotent (fresh LLM call per CP-042).
//   - VerdictFileWriter.WriteAndCommit — Tags: mechanism;
//     idempotency=non-idempotent (git write per CP-042).
//
// Neither crosses into the other's domain; these tests confirm the boundary
// is held on the success path, the error path, and the replay path.
//
// All test-local identifiers use the cp042 prefix.

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
// Test-local stubs
// ---------------------------------------------------------------------------

// cp042StubEval is a stub CognitionHookEvaluator that records call count.
type cp042StubEval struct {
	returnVerdict core.HookVerdictRecord
	returnErr     error
	callCount     int
}

func (e *cp042StubEval) EvaluateCognitionHook(_ context.Context, _ core.ControlPoint, _ core.Event) (core.HookVerdictRecord, error) {
	e.callCount++
	return e.returnVerdict, e.returnErr
}

// cp042StubVerdictWriter records every WriteAndCommit call.
type cp042StubVerdictWriter struct {
	calls     []cp042WriteCall
	returnSHA string
	returnErr error
}

type cp042WriteCall struct {
	Path     string
	Contents []byte
}

func (w *cp042StubVerdictWriter) WriteAndCommit(_ context.Context, relPath string, contents []byte) (string, error) {
	if w.returnErr != nil {
		return "", w.returnErr
	}
	w.calls = append(w.calls, cp042WriteCall{Path: relPath, Contents: append([]byte(nil), contents...)})
	return w.returnSHA, nil
}

// cp042StubReader is a stub VerdictReader.
type cp042StubReader struct {
	found   bool
	verdict core.HookVerdictRecord
}

func (r *cp042StubReader) LookupVerdict(_ context.Context, _ core.RunID, _ string, _ core.EventID) (core.HookVerdictRecord, bool, error) {
	return r.verdict, r.found, nil
}

// Compile-time interface satisfaction checks.
var _ hooksystem.CognitionHookEvaluator = (*cp042StubEval)(nil)
var _ hooksystem.VerdictFileWriter = (*cp042StubVerdictWriter)(nil)
var _ hooksystem.VerdictReader = (*cp042StubReader)(nil)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func cp042RunID() core.RunID {
	return core.RunID(uuid.MustParse("019e7342-0000-7000-a000-000000000042"))
}

func cp042MakeCognitionHookCP(name, triggerEvent string) core.ControlPoint {
	dp := core.DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "hook.review.input.v1",
		ResponseSchemaRef: "hook.review.response.v1",
		PromptTemplateRef: "hook.review.prompt.v1",
	}
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagCognition, DelegationPath: &dp},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:      triggerEvent,
				SideEffectKind:    core.SideEffectKindEmitEvent,
				HaltOnFailure:     false,
				SubsystemPriority: 0,
				IdempotencyClass:  core.IdempotencyClassNonIdempotent,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagCognition,
		SchemaVersion: 1,
	}
}

// cp042ComputeEnvelopeHash mirrors computeHookEnvelopeHash (unexported) for use
// in replay tests that must pre-seed the reader with the correct hash.
func cp042ComputeEnvelopeHash(t *testing.T, cp core.ControlPoint, evPayload json.RawMessage) string {
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
		t.Fatalf("cp042ComputeEnvelopeHash: marshal: %v", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func cp042BuildBusWithCollector(t *testing.T, collector *cp012FixtureEventCollector, extraSubs ...func(eventbus.EventBus) error) eventbus.EventBus {
	t.Helper()
	bus := eventbus.NewBusImpl()
	for _, fn := range extraSubs {
		if err := fn(bus); err != nil {
			t.Fatalf("cp042BuildBusWithCollector: extra sub: %v", err)
		}
	}
	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.cp042.collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			collector.record(string(ev.Type))
			return nil
		},
	}); err != nil {
		t.Fatalf("cp042BuildBusWithCollector: subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("cp042BuildBusWithCollector: Seal: %v", err)
	}
	return bus
}

// ---------------------------------------------------------------------------
// CP-042.1: Production and persistence are separate operations
// ---------------------------------------------------------------------------

// TestCP042_ProductionAndPersistenceAreSeparateOperations verifies that on a
// first-time cognition hook invocation:
//  1. The cognition evaluator (Tags: cognition; idempotency=idempotent) is
//     called exactly once — it produces the verdict via a fresh LLM call.
//  2. The verdict writer (Tags: mechanism; idempotency=non-idempotent) is
//     called exactly once — it writes the verdict to git.
//  3. The written path follows the canonical .harmonik/hooks/<run_id>/<invocation_id>.json
//     shape with the run_id contained in the path (CP-040 / HookVerdictFilePath).
//  4. The written JSON carries the HookName stamped by the dispatcher (per CP-042:
//     "dispatcher owns the mechanical fields").
//
// Neither the evaluator nor the writer crosses into the other's domain:
// the evaluator does not call WriteAndCommit; the writer does not call the LLM.
// Separation is enforced by the interface boundary and confirmed here by
// verifying exactly one call on each side with no cross-contamination.
func TestCP042_ProductionAndPersistenceAreSeparateOperations(t *testing.T) {
	t.Parallel()

	cp := cp042MakeCognitionHookCP("review-hook", "on_agent_started")
	reg := cp012FixtureNewRegistry(cp)

	stubVerdict := core.HookVerdictRecord{
		HookName:     "will-be-overwritten-by-dispatcher",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-000000004201"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: strings.Repeat("e", 64), // dispatcher overwrites with computed hash
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	eval := &cp042StubEval{returnVerdict: stubVerdict}
	writer := &cp042StubVerdictWriter{returnSHA: "sha-cp042-one"}
	reader := &cp042StubReader{found: false} // first invocation — no prior verdict

	collector := &cp012FixtureEventCollector{}
	bus := cp042BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp := hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})

	runID := cp042RunID()
	payload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	if err := bus.EmitWithRunID(context.Background(), runID, "agent_started", payload); err != nil {
		t.Fatalf("EmitWithRunID: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	// CP-042: cognition evaluator (production) called exactly once.
	if eval.callCount != 1 {
		t.Errorf("CP-042.1: cognition evaluator (production, Tags:cognition) called %d times, want 1 "+
			"(idempotency=idempotent — one fresh LLM call per first invocation)", eval.callCount)
	}

	// CP-042: writer (persistence) called exactly once.
	if len(writer.calls) != 1 {
		t.Fatalf("CP-042.1: verdict writer (persistence, Tags:mechanism) called %d times, want 1 "+
			"(idempotency=non-idempotent — one git write per verdict)", len(writer.calls))
	}

	writtenPath := writer.calls[0].Path

	// Written path must carry the run_id component.
	if !strings.HasPrefix(writtenPath, ".harmonik/hooks/") {
		t.Errorf("CP-042.1: written path %q does not start with .harmonik/hooks/", writtenPath)
	}
	if !strings.HasSuffix(writtenPath, ".json") {
		t.Errorf("CP-042.1: written path %q does not end with .json", writtenPath)
	}
	if !strings.Contains(writtenPath, runID.String()) {
		t.Errorf("CP-042.1: written path %q does not contain run_id %q", writtenPath, runID.String())
	}

	// Written JSON must be valid and carry the dispatcher-stamped HookName.
	var written core.HookVerdictRecord
	if err := json.Unmarshal(writer.calls[0].Contents, &written); err != nil {
		t.Fatalf("CP-042.1: written contents not valid JSON: %v", err)
	}
	if written.HookName != cp.Name {
		t.Errorf("CP-042.1: written HookName = %q, want %q "+
			"(dispatcher stamps mechanical fields per CP-042)", written.HookName, cp.Name)
	}
	// Dispatcher must have generated a non-nil InvocationID.
	if written.InvocationID == uuid.Nil {
		t.Error("CP-042.1: written InvocationID is nil UUID; dispatcher must assign a non-nil ID")
	}
	// The path must incorporate the dispatcher-assigned InvocationID.
	if !strings.Contains(writtenPath, written.InvocationID.String()) {
		t.Errorf("CP-042.1: path %q does not contain InvocationID %q", writtenPath, written.InvocationID)
	}
}

// ---------------------------------------------------------------------------
// CP-042.2: Writer path is the canonical HookVerdictFilePath form
// ---------------------------------------------------------------------------

// TestCP042_WriterPathIsCanonical verifies that the path passed to the
// mechanism-tagged writer is exactly the canonical form produced by
// core.HookVerdictFilePath: .harmonik/hooks/<run_id>/<invocation_id>.json.
//
// CP-042 enforcement: the mechanism persister MUST use the canonical path so
// the replay-read (CP-041) can locate the verdict by the same derivation.
// A deviant path breaks the idempotent-replay contract.
func TestCP042_WriterPathIsCanonical(t *testing.T) {
	t.Parallel()

	cp := cp042MakeCognitionHookCP("review-hook", "on_run_completed")
	reg := cp012FixtureNewRegistry(cp)

	eval := &cp042StubEval{returnVerdict: core.HookVerdictRecord{
		HookName:     "review-hook",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-000000004202"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: strings.Repeat("c", 64),
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}}
	writer := &cp042StubVerdictWriter{returnSHA: "sha-cp042-two"}
	reader := &cp042StubReader{found: false}

	collector := &cp012FixtureEventCollector{}
	bus := cp042BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp := hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})

	runID := cp042RunID()
	payload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	if err := bus.EmitWithRunID(context.Background(), runID, "run_completed", payload); err != nil {
		t.Fatalf("EmitWithRunID: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	if len(writer.calls) != 1 {
		t.Fatalf("CP-042.2: expected 1 write call, got %d", len(writer.calls))
	}

	writtenPath := writer.calls[0].Path

	// Parse the written verdict to recover the dispatcher-assigned InvocationID.
	var written core.HookVerdictRecord
	if err := json.Unmarshal(writer.calls[0].Contents, &written); err != nil {
		t.Fatalf("CP-042.2: written contents not valid JSON: %v", err)
	}

	wantPath := core.HookVerdictFilePath(runID, written.InvocationID)
	if writtenPath != wantPath {
		t.Errorf("CP-042.2: written path = %q, want canonical %q "+
			"(mechanism persister must use HookVerdictFilePath so replay-read can locate the verdict)", writtenPath, wantPath)
	}
}

// ---------------------------------------------------------------------------
// CP-042.3: Writer NOT called when cognition evaluator errors
// ---------------------------------------------------------------------------

// TestCP042_WriterNotCalledOnEvaluatorError verifies that when the cognition
// evaluator (production, Tags: cognition) returns an error, the mechanism
// persister is NOT called.
//
// CP-042 enforcement: if production fails, there is no valid verdict to persist.
// Calling the writer would commit an invalid record and violate the non-idempotent
// git-write contract (once written, the record is authoritative). The boundary
// holds on the failure path: cognition-tagged production failure must not reach
// the mechanism-tagged persistence step.
func TestCP042_WriterNotCalledOnEvaluatorError(t *testing.T) {
	t.Parallel()

	cp := cp042MakeCognitionHookCP("review-hook", "on_agent_started")
	reg := cp012FixtureNewRegistry(cp)

	eval := &cp042StubEval{
		returnVerdict: core.HookVerdictRecord{},
		returnErr:     fmt.Errorf("cognition dispatch failed: model timeout"),
	}
	writer := &cp042StubVerdictWriter{returnSHA: "should-not-be-used"}
	reader := &cp042StubReader{found: false}

	collector := &cp012FixtureEventCollector{}
	bus := cp042BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp := hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})

	runID := cp042RunID()
	payload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	if err := bus.EmitWithRunID(context.Background(), runID, "agent_started", payload); err != nil {
		t.Fatalf("EmitWithRunID: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	// Evaluator must have been called (production was attempted).
	if eval.callCount != 1 {
		t.Errorf("CP-042.3: evaluator callCount = %d, want 1", eval.callCount)
	}

	// Writer MUST NOT be called — no valid verdict to persist.
	if len(writer.calls) != 0 {
		t.Errorf("CP-042.3: writer called %d times despite evaluator error, want 0 "+
			"(mechanism persister must not write when production fails)", len(writer.calls))
	}

	// hook_failed must be emitted to signal the production failure.
	events := collector.all()
	hasFailed := false
	for _, et := range events {
		if et == "hook_failed" {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Errorf("CP-042.3: hook_failed not emitted after evaluator error; events: %v", events)
	}
}

// ---------------------------------------------------------------------------
// CP-042.4: Writer NOT called on replay (hash match)
// ---------------------------------------------------------------------------

// TestCP042_WriterNotCalledOnReplay verifies that when a persisted verdict
// exists with a matching envelope hash, neither the cognition evaluator nor
// the mechanism writer is called.
//
// CP-042 enforcement:
//   - Cognition evaluator not called: idempotency=idempotent per CP-042 means
//     the persisted result is reused without a second LLM call (CP-INV-003).
//   - Mechanism writer not called: idempotency=non-idempotent per CP-042 means
//     re-writing a verdict that is already persisted would duplicate the git
//     commit and violate the at-most-once write contract.
//
// The replay path consumes the stored verdict directly and emits hook_fired
// without touching either tagged operation.
func TestCP042_WriterNotCalledOnReplay(t *testing.T) {
	t.Parallel()

	cp := cp042MakeCognitionHookCP("review-hook", "on_agent_started")
	reg := cp012FixtureNewRegistry(cp)

	runID := cp042RunID()
	evPayload, _ := json.Marshal(map[string]any{"run_id": runID.String()})
	correctHash := cp042ComputeEnvelopeHash(t, cp, evPayload)

	persistedVerdict := core.HookVerdictRecord{
		HookName:     "review-hook",
		InvocationID: uuid.MustParse("019e7342-0000-7000-a000-000000004203"),
		SideEffect: core.SideEffect{
			Kind:             core.SideEffectKindEmitEvent,
			Target:           "review-hook",
			IdempotencyClass: core.IdempotencyClassNonIdempotent,
		},
		Failed:            false,
		InputEnvelopeHash: correctHash,
		ProducedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	eval := &cp042StubEval{}                                   // MUST NOT be called on replay
	writer := &cp042StubVerdictWriter{returnSHA: "no-write"}   // MUST NOT be called on replay
	reader := &cp042StubReader{found: true, verdict: persistedVerdict}

	collector := &cp012FixtureEventCollector{}
	bus := cp042BuildBusWithCollector(t, collector, func(b eventbus.EventBus) error {
		disp := hooksystem.NewDispatcher(reg, b)
		disp.WithCognition(eval, writer, reader)
		return disp.Subscribe()
	})

	if err := bus.EmitWithRunID(context.Background(), runID, "agent_started", evPayload); err != nil {
		t.Fatalf("EmitWithRunID: %v", err)
	}
	cp012FixtureWaitDrain(t, bus)

	// Cognition evaluator MUST NOT be called on replay (CP-INV-003, idempotency=idempotent).
	if eval.callCount != 0 {
		t.Errorf("CP-042.4: cognition evaluator (production) called %d times on replay, want 0 "+
			"(CP-INV-003: idempotency=idempotent — persisted verdict reused, no second LLM call)", eval.callCount)
	}

	// Mechanism writer MUST NOT be called on replay (idempotency=non-idempotent — must not re-write).
	if len(writer.calls) != 0 {
		t.Errorf("CP-042.4: verdict writer (persistence) called %d times on replay, want 0 "+
			"(idempotency=non-idempotent — already persisted, duplicate git write forbidden)", len(writer.calls))
	}

	// hook_fired MUST be emitted from the replayed verdict.
	events := collector.all()
	hasFired := false
	for _, et := range events {
		if et == "hook_fired" {
			hasFired = true
		}
	}
	if !hasFired {
		t.Errorf("CP-042.4: hook_fired not emitted for replay path; events: %v", events)
	}
}
