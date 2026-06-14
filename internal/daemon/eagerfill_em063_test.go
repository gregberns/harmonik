package daemon

// eagerfill_em063_test.go — unit tests for the EM-063 pre-screen and
// provenance guard in the eager-refill path.
//
// Observable behaviours covered:
//
//  1. Phase 1: a bead already present in the queue with pending/dispatched/
//     completed/failed status is excluded from survivors.
//
//  2. Phase 2: beadLandedOnOriginMain returns (false, "", nil) when the
//     git working directory does not contain a remote tracking branch —
//     the call does not crash and treats the bead as not-landed.
//
//  3. kerfNextBeads returns an error when the kerf binary path is absent.
//
//  4. eagerRefillEval returns immediately (no panic) when kerfPath is empty.
//
//  5. eagerRefillEval returns immediately when queueStore is nil.
//
// Spec ref: specs/execution-model.md §4.13 EM-063.
// Bead ref: hk-9321v.

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// em063FixtureStreamQueueWithBeads builds an active stream queue that has
// the given bead IDs as dispatched or pending items.  The returned queue has
// one group (index 0) in active state.
func em063FixtureStreamQueueWithBeads(beadIDs ...string) *queue.Queue {
	now := time.Now().UTC()
	items := make([]queue.Item, 0, len(beadIDs))
	for i, id := range beadIDs {
		status := queue.ItemStatusPending
		if i%2 == 0 {
			status = queue.ItemStatusDispatched
		}
		items = append(items, queue.Item{
			BeadID: core.BeadID(id),
			Status: status,
		})
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "em063-test-queue",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      items,
			},
		},
	}
}

// em063FixtureDeps builds a minimal workLoopDeps with only the fields
// required by preScreenCandidates and eagerRefillEval.  kerfPath is left
// empty (no eager-refill) unless overridden by the caller.
func em063FixtureDeps(t *testing.T, qs *QueueStore) workLoopDeps {
	t.Helper()
	return workLoopDeps{
		queueStore:    qs,
		kerfPath:      "",
		projectDir:    t.TempDir(),
		maxConcurrent: 4,
		runRegistry:   newLocalRunRegistry(),
		bus:           &noopEmitter{},
		queueLedger:   nil,
	}
}

// noopEmitter satisfies handlercontract.EventEmitter for test stubs that do
// not need event inspection.
type noopEmitter struct{}

func (n *noopEmitter) Emit(_ context.Context, _ core.EventType, _ []byte) error { return nil }
func (n *noopEmitter) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	return nil
}

// ---------------------------------------------------------------------------
// Phase 1: already-in-queue guard
// ---------------------------------------------------------------------------

// TestEM063_Phase1_AlreadyInQueue_PendingExcluded verifies that a bead present
// in the active queue with ItemStatusPending is excluded from pre-screen
// survivors (EM-063 Phase 1).
func TestEM063_Phase1_AlreadyInQueue_PendingExcluded(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-inqueue-01", "hk-inqueue-02")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	candidates := []core.BeadID{"hk-inqueue-01", "hk-inqueue-02", "hk-new-bead"}
	survivors := preScreenCandidates(context.Background(), deps, candidates, "em063-test-queue")

	// Only the bead NOT already in the queue should survive Phase 1.
	if len(survivors) != 1 {
		t.Fatalf("Phase 1: survivors = %v, want [hk-new-bead]", survivors)
	}
	if survivors[0] != "hk-new-bead" {
		t.Errorf("Phase 1: survivors[0] = %q, want 'hk-new-bead'", survivors[0])
	}
}

// TestEM063_Phase1_AlreadyInQueue_DispatchedExcluded verifies that a bead
// present with ItemStatusDispatched is also excluded (EM-063 Phase 1
// covers pending, dispatched, completed, and failed).
func TestEM063_Phase1_AlreadyInQueue_DispatchedExcluded(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	now := time.Now().UTC()
	runID := "019e0000-0000-7000-0000-000000000001"
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "em063-dispatched-queue",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-dispatched", Status: queue.ItemStatusDispatched, RunID: &runID},
			},
		}},
	}
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)

	candidates := []core.BeadID{"hk-dispatched", "hk-fresh"}
	survivors := preScreenCandidates(context.Background(), deps, candidates, "em063-dispatched-queue")

	if len(survivors) != 1 || survivors[0] != "hk-fresh" {
		t.Errorf("Phase 1: survivors = %v, want [hk-fresh]", survivors)
	}
}

// TestEM063_Phase1_EmptyQueueAllSurvive verifies that when no queue is loaded
// all candidates pass Phase 1 (no in-queue entries to exclude).
func TestEM063_Phase1_EmptyQueueAllSurvive(t *testing.T) {
	t.Parallel()

	deps := em063FixtureDeps(t, newQueueStore())

	candidates := []core.BeadID{"hk-a", "hk-b", "hk-c"}
	// Phase 2 git check will not find anything (temp dir has no git history).
	survivors := preScreenCandidates(context.Background(), deps, candidates, "no-queue")

	if len(survivors) != 3 {
		t.Errorf("Phase 1 with empty queue: survivors = %v, want all 3 candidates", survivors)
	}
}

// ---------------------------------------------------------------------------
// Phase 2: already-landed git guard
// ---------------------------------------------------------------------------

// TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote verifies that
// beadLandedOnOriginMain returns (false, "", nil) when the project directory
// is a git repo with no origin/main remote tracking branch.  This models the
// most common CI/test environment where origin/main doesn't exist yet.
func TestEM063_Phase2_BeadLandedOnOriginMain_MissingRemote(t *testing.T) {
	t.Parallel()

	// Use a temp dir with an empty git repo.
	dir := t.TempDir()
	// initialise a bare git repo so `git log` has something to work with.
	if out, err := runSimpleCmd("git", "-C", dir, "init"); err != nil {
		t.Skipf("git init failed: %v (%s)", err, out)
	}

	found, sha, err := beadLandedOnOriginMain(context.Background(), dir, "hk-test-bead")
	if err != nil {
		t.Fatalf("beadLandedOnOriginMain: unexpected error: %v", err)
	}
	if found {
		t.Errorf("beadLandedOnOriginMain: found = true, want false (no origin/main)")
	}
	if sha != "" {
		t.Errorf("beadLandedOnOriginMain: sha = %q, want empty", sha)
	}
}

// runSimpleCmd is a test helper that runs a command and returns (output, error).
func runSimpleCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ---------------------------------------------------------------------------
// kerfNextBeads — binary-absent error path
// ---------------------------------------------------------------------------

// TestEM063_KerfNextBeads_BinaryAbsent verifies that kerfNextBeads returns an
// error when the kerf binary path does not exist (EM-062 relies on this to
// detect a non-installed kerf).
func TestEM063_KerfNextBeads_BinaryAbsent(t *testing.T) {
	t.Parallel()

	_, err := kerfNextBeads(context.Background(), "/nonexistent/kerf-binary", 4)
	if err == nil {
		t.Fatal("kerfNextBeads with absent binary: expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// eagerRefillEval — guard gates
// ---------------------------------------------------------------------------

// TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty verifies that
// eagerRefillEval returns immediately (no panic, no queue mutation) when
// kerfPath is empty — the "kerf not installed" fast-path.
func TestEM063_EagerRefillEval_NoopWhenKerfPathEmpty(t *testing.T) {
	t.Parallel()

	qs := newQueueStore()
	q := em063FixtureStreamQueueWithBeads("hk-existing")
	qs.SetQueue(q)

	deps := em063FixtureDeps(t, qs)
	deps.kerfPath = "" // kerf not installed

	// Must not panic, must not mutate queue.
	eagerRefillEval(context.Background(), deps)

	// Queue should be unchanged.
	got := qs.Queue()
	if got == nil || len(got.Groups[0].Items) != 1 {
		t.Error("eagerRefillEval with empty kerfPath mutated the queue; expected no-op")
	}
}

// TestEM063_EagerRefillEval_NoopWhenQueueStoreNil verifies that
// eagerRefillEval returns immediately when queueStore is nil.
func TestEM063_EagerRefillEval_NoopWhenQueueStoreNil(t *testing.T) {
	t.Parallel()

	deps := em063FixtureDeps(t, nil)
	deps.kerfPath = "/some/kerf" // set a kerf path to get past the first guard
	deps.queueStore = nil

	// Must not panic.
	eagerRefillEval(context.Background(), deps)
}
