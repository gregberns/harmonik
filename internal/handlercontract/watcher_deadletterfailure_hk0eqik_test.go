package handlercontract_test

// watcher_deadletterfailure_hk0eqik_test.go — regression for the silently
// dropped dead-letter failure (hk-0eqik).
//
// Bug: WatcherDeadLetterSink.Append's doc comment claimed "the watcher logs the
// failure", but every call site discarded the error with `_ = dl.Append(...)`.
// An event that failed to reach the bus AND failed to reach the dead-letter
// store was lost with no trace, so a broken sink was indistinguishable from a
// working one.
//
// Fix (hk-0eqik): every Append goes through Watcher.appendDeadLetter, which
// counts the failure on the Watcher handle (DeadLetterFailures /
// LastDeadLetterFailure — always on) and hands it to the optional
// SpawnWatcherConfig.OnDeadLetterFailure hook the daemon-side caller installs.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// errDLSinkDown is the sentinel a broken dead-letter sink returns, so the test
// can assert the exact error reaches the observability surface unwrapped.
var errDLSinkDown = errors.New("dead-letter sink is down")

// dlFailPublisher fails every Emit, forcing the watcher onto the dead-letter
// path for every event it handles.
type dlFailPublisher struct{}

func (dlFailPublisher) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	return errors.New("dlFailPublisher: bus unavailable")
}

func (p dlFailPublisher) EmitWithRunID(ctx context.Context, _ core.RunID, et core.EventType, pl []byte) error {
	return p.Emit(ctx, et, pl)
}

// dlFailSink is a WatcherDeadLetterSink whose Append always fails — the broken
// sink this bead is about.
type dlFailSink struct{}

func (dlFailSink) Append(_ core.EventType, _ []byte, _ string) error { return errDLSinkDown }

// dlOKSink is a WatcherDeadLetterSink whose Append always succeeds, used to
// prove the failure signal stays silent when the sink is healthy.
type dlOKSink struct{}

func (dlOKSink) Append(_ core.EventType, _ []byte, _ string) error { return nil }

// dlFailHookRecorder captures OnDeadLetterFailure invocations. The hook runs on
// the watcher goroutine, so access is mutex-guarded.
type dlFailHookRecorder struct {
	mu      sync.Mutex
	types   []string
	reasons []string
	errs    []error
}

func (r *dlFailHookRecorder) hook(eventType core.EventType, reason string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = append(r.types, string(eventType))
	r.reasons = append(r.reasons, reason)
	r.errs = append(r.errs, err)
}

func (r *dlFailHookRecorder) snapshot() (types, reasons []string, errs []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.types...), append([]string(nil), r.reasons...), append([]error(nil), r.errs...)
}

// dlFailLine is one well-formed agent_ready progress-stream line — a known
// message type, so the watcher publishes it (and, with a failing publisher,
// dead-letters it).
const dlFailLine = `{"type":"agent_ready","session_id":"s-hk0eqik"}` + "\n"

// TestWatcher_DeadLetterSinkFailure_IsObservable is the acceptance test for
// hk-0eqik: when both the bus and the dead-letter sink reject an event, the
// failure reaches BOTH observability surfaces — the always-on counter on the
// Watcher handle and the caller-supplied hook.
func TestWatcher_DeadLetterSinkFailure_IsObservable(t *testing.T) {
	rec := &dlFailHookRecorder{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:           core.SessionID("s-hk0eqik"),
		ProgressStream:      strings.NewReader(dlFailLine),
		Publisher:           dlFailPublisher{},
		DeadLetter:          dlFailSink{},
		OnDeadLetterFailure: rec.hook,
	})
	watcherFixtureWait(t, w)

	if got := w.DeadLetterFailures(); got == 0 {
		t.Errorf("DeadLetterFailures() = 0, want ≥1 — a failing dead-letter sink must be distinguishable from a working one")
	}
	if err := w.LastDeadLetterFailure(); !errors.Is(err, errDLSinkDown) {
		t.Errorf("LastDeadLetterFailure() = %v, want it to wrap errDLSinkDown", err)
	}

	types, reasons, errs := rec.snapshot()
	if len(types) == 0 {
		t.Fatal("OnDeadLetterFailure was never called, want ≥1 call")
	}
	if types[0] != "agent_ready" {
		t.Errorf("hook eventType = %q, want %q", types[0], "agent_ready")
	}
	if !strings.HasPrefix(reasons[0], "emit failed:") {
		t.Errorf("hook reason = %q, want it to start with %q", reasons[0], "emit failed:")
	}
	if !errors.Is(errs[0], errDLSinkDown) {
		t.Errorf("hook err = %v, want it to wrap errDLSinkDown", errs[0])
	}
	if uint64(len(types)) != w.DeadLetterFailures() {
		t.Errorf("hook fired %d times but DeadLetterFailures() = %d, want them to agree", len(types), w.DeadLetterFailures())
	}
}

// TestWatcher_DeadLetterSinkHealthy_NoFailureSignal is the negative half: a
// dead-letter sink that accepts the spill must leave the failure surfaces
// clean, so a non-zero count genuinely means "the sink is broken".
func TestWatcher_DeadLetterSinkHealthy_NoFailureSignal(t *testing.T) {
	rec := &dlFailHookRecorder{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:           core.SessionID("s-hk0eqik"),
		ProgressStream:      strings.NewReader(dlFailLine),
		Publisher:           dlFailPublisher{}, // bus still fails: the spill happens
		DeadLetter:          dlOKSink{},        // but the sink accepts it
		OnDeadLetterFailure: rec.hook,
	})
	watcherFixtureWait(t, w)

	if got := w.DeadLetterFailures(); got != 0 {
		t.Errorf("DeadLetterFailures() = %d, want 0 when the sink accepts the spill", got)
	}
	if err := w.LastDeadLetterFailure(); err != nil {
		t.Errorf("LastDeadLetterFailure() = %v, want nil when the sink accepts the spill", err)
	}
	if types, _, _ := rec.snapshot(); len(types) != 0 {
		t.Errorf("OnDeadLetterFailure fired %d times, want 0 when the sink accepts the spill", len(types))
	}
}

// TestWatcher_DeadLetterSinkFailure_NilHookIsSafe pins the nil-safe default:
// callers that predate the hook (it is optional) still get the counter, and the
// watcher must not panic on the nil function value.
func TestWatcher_DeadLetterSinkFailure_NilHookIsSafe(t *testing.T) {
	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("s-hk0eqik"),
		ProgressStream: strings.NewReader(dlFailLine),
		Publisher:      dlFailPublisher{},
		DeadLetter:     dlFailSink{},
		// OnDeadLetterFailure deliberately unset.
	})
	watcherFixtureWait(t, w)

	if err := w.Err(); err != nil {
		t.Errorf("Err() = %v, want nil — a dead-letter sink failure must not be terminal for the watcher", err)
	}
	if got := w.DeadLetterFailures(); got == 0 {
		t.Error("DeadLetterFailures() = 0 with no hook configured, want ≥1 — the counter must not depend on opt-in")
	}
}

// dlFailReasonsWithPrefix returns the hook reasons that start with prefix.
func dlFailReasonsWithPrefix(reasons []string, prefix string) []string {
	var out []string
	for _, r := range reasons {
		if strings.HasPrefix(r, prefix) {
			out = append(out, r)
		}
	}
	return out
}

// TestWatcher_DeadLetterSinkFailure_LifecycleTransitionRoute pins the
// Watcher.emitMachineTransition dead-letter routes, which the other tests in
// this file do NOT reach: they leave SpawnWatcherConfig.Machine nil, so
// Watcher.driveLifecycleFSM never runs and both of emitMachineTransition's
// emit-failure Appends could be reverted to `_ = dl.Append(...)` with every
// other test in this file still green.
//
// Distinguishing the route from the already-covered publishOrDeadLetter route
// is what the reason prefix is for: publishOrDeadLetter reports "emit failed:",
// emitMachineTransition reports "lifecycle_transition emit:".
//
// The two subtests select emitMachineTransition's two emit branches: a
// UUID-shaped run ID takes EmitWithRunID, anything else falls back to Emit.
// Both must record the failure.
//
// The line is agent_failed, not agent_ready: a fresh hclifecycle.Machine starts
// in StateSpawning, and the HC-065 edge table has no Spawning→Ready edge, so an
// agent_ready line makes Machine.Transition fail and emitMachineTransition
// return before it emits anything. Spawning→Failed IS an edge.
func TestWatcher_DeadLetterSinkFailure_LifecycleTransitionRoute(t *testing.T) {
	const dlFailLifecycleLine = `{"type":"agent_failed","session_id":"s-hk0eqik"}` + "\n"

	cases := []struct {
		name  string
		runID string
	}{
		// uuid.Parse succeeds → EmitWithRunID branch.
		{name: "uuid_run_id_emit_with_run_id_branch", runID: "9f1c2b7e-3d4a-4f5b-8c6d-7e8f90a1b2c3"},
		// uuid.Parse fails → plain Emit fallback branch.
		{name: "non_uuid_run_id_plain_emit_branch", runID: "not-a-uuid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &dlFailHookRecorder{}
			m := hclifecycle.New("s-hk0eqik", tc.runID)

			w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
				SessionID:           core.SessionID("s-hk0eqik"),
				ProgressStream:      strings.NewReader(dlFailLifecycleLine),
				Publisher:           dlFailPublisher{},
				DeadLetter:          dlFailSink{},
				Machine:             m,
				OnDeadLetterFailure: rec.hook,
			})
			watcherFixtureWait(t, w)

			types, reasons, _ := rec.snapshot()
			lt := dlFailReasonsWithPrefix(reasons, "lifecycle_transition emit:")
			if len(lt) == 0 {
				t.Errorf("no dead-letter failure reported for the lifecycle_transition emit route; reasons = %q", reasons)
			}

			var sawLifecycleType bool
			for _, ty := range types {
				if ty == string(core.EventTypeLifecycleTransition) {
					sawLifecycleType = true
				}
			}
			if !sawLifecycleType {
				t.Errorf("hook eventTypes = %q, want one of them to be %q", types, core.EventTypeLifecycleTransition)
			}

			// The agent_failed publish AND the lifecycle transition both
			// spill, so this line drives ≥2 failures through the counter.
			if got := w.DeadLetterFailures(); got < 2 {
				t.Errorf("DeadLetterFailures() = %d, want ≥2 (agent_failed publish + lifecycle_transition emit)", got)
			}
		})
	}
}

// TestWatcher_DeadLetterSinkFailure_BudgetAccrualRoute pins the CP-024
// co-emitted budget_accrual on the dead-letter failure path. The other tests in
// this file feed an agent_ready line, so Watcher.emitBudgetAccrualForChunk never
// runs at all.
//
// Scope note: this covers emitBudgetAccrualForChunk's publishOrDeadLetter
// route. Its OTHER dead-letter route — the json.Marshal failure branch — is
// unreachable by construction (core.BudgetAccrualPayload is a static struct of
// marshalable fields), so no test can drive it; it is defensive symmetry, not
// live code.
func TestWatcher_DeadLetterSinkFailure_BudgetAccrualRoute(t *testing.T) {
	rec := &dlFailHookRecorder{}
	const chunkLine = `{"type":"agent_output_chunk","session_id":"s-hk0eqik","chunk_index":0,"bytes_emitted":7}` + "\n"

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:           core.SessionID("s-hk0eqik"),
		ProgressStream:      strings.NewReader(chunkLine),
		Publisher:           dlFailPublisher{},
		DeadLetter:          dlFailSink{},
		OnDeadLetterFailure: rec.hook,
	})
	watcherFixtureWait(t, w)

	types, _, _ := rec.snapshot()
	var sawAccrual bool
	for _, ty := range types {
		if ty == string(core.EventTypeBudgetAccrual) {
			sawAccrual = true
		}
	}
	if !sawAccrual {
		t.Errorf("hook eventTypes = %q, want one of them to be %q — the CP-024 co-emitted accrual must reach the dead-letter failure surface too", types, core.EventTypeBudgetAccrual)
	}
}

// TestWatcher_NewWatcher_HasNoDeadLetterFailures pins the zero value: a watcher
// that has never spilled reports a clean failure surface.
func TestWatcher_NewWatcher_HasNoDeadLetterFailures(t *testing.T) {
	w, closeDone := handlercontract.NewWatcherForTest()
	closeDone()

	if got := w.DeadLetterFailures(); got != 0 {
		t.Errorf("DeadLetterFailures() = %d on a fresh watcher, want 0", got)
	}
	if err := w.LastDeadLetterFailure(); err != nil {
		t.Errorf("LastDeadLetterFailure() = %v on a fresh watcher, want nil", err)
	}
}
