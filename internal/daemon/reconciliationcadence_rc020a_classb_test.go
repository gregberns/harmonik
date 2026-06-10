package daemon

// reconciliationcadence_rc020a_classb_test.go — unit tests for runScheduledClassBRepair.
//
// Tests are in package daemon (same-package) to access the unexported function.
//
// Three behaviours under test:
//   1. In-progress bead absent from all queues → ResetBead is called.
//   2. In-progress bead present in a queue → ResetBead is NOT called.
//   3. Nil Emitter in cfg → no panic.
//
// Bead ref: hk-m3ydd (reviewer feedback: test-coverage flag).

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// classBStubResetter records ResetBead calls for assertion.
type classBStubResetter struct {
	mu    sync.Mutex
	calls []core.BeadID
	err   error // returned from every ResetBead call; nil for success
}

func (r *classBStubResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, beadID)
	return r.err
}

func (r *classBStubResetter) resetCalls() []core.BeadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.BeadID, len(r.calls))
	copy(out, r.calls)
	return out
}

// classBStubEmitter records emitted events.
type classBStubEmitter struct {
	mu     sync.Mutex
	events []core.EventType
}

func (e *classBStubEmitter) Emit(_ context.Context, t core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, t)
	return nil
}

func (e *classBStubEmitter) count(t core.EventType) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev == t {
			n++
		}
	}
	return n
}

// classBFixtureDir creates a temp project dir with .harmonik/queues/ and
// .harmonik/beads-intents/ sub-trees. Optionally writes a queue JSON file
// containing the given bead IDs as pending items.
func classBFixtureDir(t *testing.T, queueBeadIDs ...core.BeadID) string {
	t.Helper()
	projectDir := t.TempDir()

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	for _, d := range []string{queuesDir, intentsDir} {
		//nolint:gosec // G301: test-only temp directory
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("classBFixtureDir: mkdir %s: %v", d, err)
		}
	}

	if len(queueBeadIDs) == 0 {
		return projectDir
	}

	// Build a minimal valid queue JSON with schema_version:1.
	type itemJSON struct {
		BeadID string `json:"bead_id"`
		Status string `json:"status"`
	}
	type groupJSON struct {
		GroupIndex int        `json:"group_index"`
		Kind       string     `json:"kind"`
		Status     string     `json:"status"`
		Items      []itemJSON `json:"items"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	type queueJSON struct {
		SchemaVersion int         `json:"schema_version"`
		QueueID       string      `json:"queue_id"`
		Name          string      `json:"name"`
		SubmittedAt   time.Time   `json:"submitted_at"`
		Status        string      `json:"status"`
		Groups        []groupJSON `json:"groups"`
	}

	items := make([]itemJSON, len(queueBeadIDs))
	for i, id := range queueBeadIDs {
		items[i] = itemJSON{BeadID: string(id), Status: "pending"}
	}
	q := queueJSON{
		SchemaVersion: 1,
		QueueID:       "00000000-0000-0000-0000-000000000001",
		Name:          "main",
		SubmittedAt:   time.Now().UTC(),
		Status:        "active",
		Groups: []groupJSON{
			{
				GroupIndex: 0,
				Kind:       "stream",
				Status:     "active",
				Items:      items,
				CreatedAt:  time.Now().UTC(),
			},
		},
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("classBFixtureDir: marshal queue: %v", err)
	}
	queuePath := filepath.Join(queuesDir, "main.json")
	//nolint:gosec // G306: test-only temp file
	if err := os.WriteFile(queuePath, data, 0o644); err != nil {
		t.Fatalf("classBFixtureDir: write queue file: %v", err)
	}

	return projectDir
}

// classBInFlightRecord returns a minimal in-progress BeadRecord for the given ID.
func classBInFlightRecord(id core.BeadID) core.BeadRecord {
	return core.BeadRecord{
		BeadID: id,
		Status: core.CoarseStatusInProgress,
	}
}

// TestRunScheduledClassBRepair_AbsentBeadIsReset verifies that an in_progress
// bead absent from all queue files has ResetBead called on it.
func TestRunScheduledClassBRepair_AbsentBeadIsReset(t *testing.T) {
	t.Parallel()

	orphan := core.BeadID("hk-orphan1")
	projectDir := classBFixtureDir(t) // no queue files → every bead is absent

	resetter := &classBStubResetter{}
	emitter := &classBStubEmitter{}

	cfg := ReconciliationSchedulerConfig{
		ProjectDir: projectDir,
		Emitter:    emitter,
		LogWriter:  io.Discard,
	}

	runScheduledClassBRepair(
		context.Background(),
		cfg,
		resetter,
		[]core.BeadRecord{classBInFlightRecord(orphan)},
		io.Discard,
	)

	// ResetBead must have been called for the orphaned bead.
	calls := resetter.resetCalls()
	if len(calls) != 1 || calls[0] != orphan {
		t.Errorf("ResetBead calls = %v, want [%s]", calls, orphan)
	}

	// reconciliation_mismatch_observed must also have been emitted.
	if n := emitter.count(core.EventTypeReconciliationMismatchObserved); n != 1 {
		t.Errorf("reconciliation_mismatch_observed count = %d, want 1", n)
	}
}

// TestRunScheduledClassBRepair_PresentBeadIsSkipped verifies that an
// in_progress bead that appears in a live queue file is NOT reset.
func TestRunScheduledClassBRepair_PresentBeadIsSkipped(t *testing.T) {
	t.Parallel()

	queuedBead := core.BeadID("hk-queued1")
	// Write a queue containing queuedBead.
	projectDir := classBFixtureDir(t, queuedBead)

	resetter := &classBStubResetter{}
	emitter := &classBStubEmitter{}

	cfg := ReconciliationSchedulerConfig{
		ProjectDir: projectDir,
		Emitter:    emitter,
		LogWriter:  io.Discard,
	}

	runScheduledClassBRepair(
		context.Background(),
		cfg,
		resetter,
		[]core.BeadRecord{classBInFlightRecord(queuedBead)},
		io.Discard,
	)

	// ResetBead must NOT have been called — bead is in the queue.
	if calls := resetter.resetCalls(); len(calls) != 0 {
		t.Errorf("ResetBead called for queued bead %s; want no calls, got %v", queuedBead, calls)
	}

	// No mismatch event either.
	if n := emitter.count(core.EventTypeReconciliationMismatchObserved); n != 0 {
		t.Errorf("reconciliation_mismatch_observed count = %d, want 0 (bead is in queue)", n)
	}
}

// TestRunScheduledClassBRepair_NilEmitterNoPanic verifies that passing a nil
// Emitter in cfg does not cause a panic (observe path is safely skipped).
func TestRunScheduledClassBRepair_NilEmitterNoPanic(t *testing.T) {
	t.Parallel()

	orphan := core.BeadID("hk-orphan-nil-emitter")
	projectDir := classBFixtureDir(t)

	resetter := &classBStubResetter{}

	cfg := ReconciliationSchedulerConfig{
		ProjectDir: projectDir,
		Emitter:    nil, // nil emitter — must not panic
		LogWriter:  io.Discard,
	}

	// The function must not panic.
	require_noPanic(t, func() {
		runScheduledClassBRepair(
			context.Background(),
			cfg,
			resetter,
			[]core.BeadRecord{classBInFlightRecord(orphan)},
			io.Discard,
		)
	})

	// ResetBead is still called even when the emitter is nil.
	calls := resetter.resetCalls()
	if len(calls) != 1 || calls[0] != orphan {
		t.Errorf("ResetBead calls = %v, want [%s]", calls, orphan)
	}
}

// require_noPanic fails the test if fn panics.
func require_noPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

// TestRunScheduledClassBRepair_MixedBeads verifies that when the in-flight
// list contains both a queued bead and an orphaned bead, only the orphan is
// reset and only one mismatch event is emitted.
func TestRunScheduledClassBRepair_MixedBeads(t *testing.T) {
	t.Parallel()

	queued := core.BeadID("hk-queued-mixed")
	orphan := core.BeadID("hk-orphan-mixed")

	// Queue contains only the queued bead.
	projectDir := classBFixtureDir(t, queued)

	resetter := &classBStubResetter{}
	emitter := &classBStubEmitter{}

	cfg := ReconciliationSchedulerConfig{
		ProjectDir: projectDir,
		Emitter:    emitter,
		LogWriter:  io.Discard,
	}

	runScheduledClassBRepair(
		context.Background(),
		cfg,
		resetter,
		[]core.BeadRecord{
			classBInFlightRecord(queued),
			classBInFlightRecord(orphan),
		},
		io.Discard,
	)

	calls := resetter.resetCalls()
	if len(calls) != 1 || calls[0] != orphan {
		t.Errorf("ResetBead calls = %v, want [%s]", calls, orphan)
	}
	if n := emitter.count(core.EventTypeReconciliationMismatchObserved); n != 1 {
		t.Errorf("reconciliation_mismatch_observed count = %d, want 1", n)
	}
}

// Compile-time check: classBStubResetter satisfies lifecycle.BeadResetter.
var _ lifecycle.BeadResetter = (*classBStubResetter)(nil)
