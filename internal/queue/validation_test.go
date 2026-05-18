package queue_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Fake BeadLedger for validation tests
// ---------------------------------------------------------------------------

// validFixtureFakeLedger is a fake BeadLedger for validation_test.go unit tests.
// It records per-bead statuses and per-pair blocks edges for deterministic
// test control.
type validFixtureFakeLedger struct {
	// statuses maps bead ID → BeadStatus. Unknown IDs return BeadStatusNotFound.
	statuses map[core.BeadID]queue.BeadStatus

	// edges records (blocker, blocked) pairs. BlocksEdge returns true iff the
	// pair is present.
	edges map[[2]core.BeadID]bool
}

func (f *validFixtureFakeLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s, ok := f.statuses[id]; ok {
		return s, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *validFixtureFakeLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// validFixtureSingleGroup constructs a ValidationRequest with one wave group
// containing the given bead IDs, against no active queue (submit path).
func validFixtureSingleGroup(ids ...core.BeadID) queue.ValidationRequest {
	items := make([]queue.Item, len(ids))
	for i, id := range ids {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}
	return queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items:      items,
			},
		},
		ActiveQueue: nil,
		IsAppend:    false,
	}
}

// validFixtureOpenLedger returns a fake ledger where the given IDs are all "open".
func validFixtureOpenLedger(ids ...core.BeadID) *validFixtureFakeLedger {
	m := make(map[core.BeadID]queue.BeadStatus, len(ids))
	for _, id := range ids {
		m[id] = queue.BeadStatusOpen
	}
	return &validFixtureFakeLedger{statuses: m, edges: map[[2]core.BeadID]bool{}}
}

// ---------------------------------------------------------------------------
// QM-027: single active queue (submit-only)
// ---------------------------------------------------------------------------

// TestValidateQM027SingleActiveQueue verifies that submitting while an
// existing non-completed queue is held fails with queue_already_active, and
// that submitting when no queue (or a completed queue) exists passes.
//
// Spec ref: queue-model.md §6.8 QM-027.
func TestValidateQM027SingleActiveQueue(t *testing.T) {
	t.Parallel()

	const idA = core.BeadID("hk-aaa")

	t.Run("pass_no_active_queue", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idA)
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("pass_completed_queue", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idA)
		req.ActiveQueue = &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "completed-queue-id",
			Status:        queue.QueueStatusCompleted,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass for completed queue, got errors: %v", errs)
		}
	})

	t.Run("fail_active_queue_exists", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idA)
		req.ActiveQueue = &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "existing-queue-id",
			Status:        queue.QueueStatusActive,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonQueueAlreadyActive {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonQueueAlreadyActive)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-024: append target validity
// ---------------------------------------------------------------------------

// TestValidateQM024AppendTargetValidity verifies that appending to a valid
// stream group passes, and that invalid targets (wave group, terminal group,
// out-of-bounds index, paused queue) fail with the correct reason.
//
// Spec ref: queue-model.md §6.5 QM-024.
func TestValidateQM024AppendTargetValidity(t *testing.T) {
	t.Parallel()

	const idA = core.BeadID("hk-bbb")

	baseActiveQueue := func(kind queue.GroupKind, status queue.GroupStatus) *queue.Queue {
		return &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "q-id",
			Status:        queue.QueueStatusActive,
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       kind,
					Status:     status,
					Items:      []queue.Item{},
				},
			},
		}
	}

	appendReq := func(q *queue.Queue, ids ...core.BeadID) queue.ValidationRequest {
		items := make([]queue.Item, len(ids))
		for i, id := range ids {
			items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
		}
		return queue.ValidationRequest{
			Groups:           []queue.Group{{GroupIndex: 0, Kind: queue.GroupKindStream, Status: queue.GroupStatusPending, Items: items}},
			ActiveQueue:      q,
			IsAppend:         true,
			AppendGroupIndex: 0,
		}
	}

	t.Run("pass_stream_active", func(t *testing.T) {
		t.Parallel()
		q := baseActiveQueue(queue.GroupKindStream, queue.GroupStatusActive)
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), appendReq(q, idA), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_wave_group", func(t *testing.T) {
		t.Parallel()
		q := baseActiveQueue(queue.GroupKindWave, queue.GroupStatusActive)
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), appendReq(q, idA), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonAppendTargetInvalid {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonAppendTargetInvalid)
		}
	})

	t.Run("fail_completed_stream", func(t *testing.T) {
		t.Parallel()
		q := baseActiveQueue(queue.GroupKindStream, queue.GroupStatusCompleteSuccess)
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), appendReq(q, idA), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonAppendTargetInvalid {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonAppendTargetInvalid)
		}
	})

	t.Run("fail_paused_by_failure", func(t *testing.T) {
		t.Parallel()
		q := baseActiveQueue(queue.GroupKindStream, queue.GroupStatusActive)
		q.Status = queue.QueueStatusPausedByFailure
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), appendReq(q, idA), ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonQueueNotAdvancing {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonQueueNotAdvancing)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-020: bead existence
// ---------------------------------------------------------------------------

// TestValidateQM020BeadExistence verifies that beads not in the ledger return
// bead_not_found, and known beads pass.
//
// Spec ref: queue-model.md §6.1 QM-020.
func TestValidateQM020BeadExistence(t *testing.T) {
	t.Parallel()

	const idKnown = core.BeadID("hk-known")
	const idMissing = core.BeadID("hk-missing")

	t.Run("pass_bead_exists", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idKnown)
		ledger := validFixtureOpenLedger(idKnown)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_bead_not_found", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idMissing)
		// Empty ledger: idMissing is unknown.
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{},
			edges:    map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonBeadNotFound {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonBeadNotFound)
		}
		if errs[0].Detail["bead_id"] != string(idMissing) {
			t.Errorf("detail bead_id: got %v, want %q", errs[0].Detail["bead_id"], idMissing)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-021: bead status
// ---------------------------------------------------------------------------

// TestValidateQM021BeadStatus verifies that only open beads pass, and beads in
// other statuses (closed, in_progress, etc.) fail with bead_not_open.
//
// Spec ref: queue-model.md §6.2 QM-021.
func TestValidateQM021BeadStatus(t *testing.T) {
	t.Parallel()

	const id = core.BeadID("hk-ccc")

	t.Run("pass_open", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(id)
		ledger := validFixtureOpenLedger(id)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_closed_bead", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(id)
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{
				id: queue.BeadStatus("closed"),
			},
			edges: map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonBeadNotOpen {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonBeadNotOpen)
		}
		if errs[0].Detail["bead_id"] != string(id) {
			t.Errorf("detail bead_id: got %v, want %q", errs[0].Detail["bead_id"], id)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-022: no double dispatch
// ---------------------------------------------------------------------------

// TestValidateQM022NoDoubleDispatch verifies that a bead already in_progress
// in the ledger fails with bead_already_dispatched.
//
// Spec ref: queue-model.md §6.3 QM-022.
func TestValidateQM022NoDoubleDispatch(t *testing.T) {
	t.Parallel()

	const id = core.BeadID("hk-ddd")

	t.Run("pass_open_bead", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(id)
		ledger := validFixtureOpenLedger(id)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_in_progress", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(id)
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{
				id: queue.BeadStatusInProgress,
			},
			edges: map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonBeadAlreadyDispatched {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonBeadAlreadyDispatched)
		}
		if errs[0].Detail["bead_id"] != string(id) {
			t.Errorf("detail bead_id: got %v, want %q", errs[0].Detail["bead_id"], id)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-023: no cross-group or intra-group duplicates
// ---------------------------------------------------------------------------

// TestValidateQM023NoDuplicateBeadID verifies that duplicate bead IDs across
// groups (submit) or within the appended set fail with duplicate_bead_id, and
// that unique IDs pass.
//
// Spec ref: queue-model.md §6.4 QM-023.
func TestValidateQM023NoDuplicateBeadID(t *testing.T) {
	t.Parallel()

	const idA = core.BeadID("hk-eee")
	const idB = core.BeadID("hk-fff")

	t.Run("pass_unique_across_groups", func(t *testing.T) {
		t.Parallel()
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{GroupIndex: 0, Kind: queue.GroupKindWave, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: idA, Status: queue.ItemStatusPending}}},
				{GroupIndex: 1, Kind: queue.GroupKindWave, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: idB, Status: queue.ItemStatusPending}}},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		ledger := validFixtureOpenLedger(idA, idB)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_intra_group_duplicate", func(t *testing.T) {
		t.Parallel()
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       queue.GroupKindWave,
					Status:     queue.GroupStatusPending,
					Items: []queue.Item{
						{BeadID: idA, Status: queue.ItemStatusPending},
						{BeadID: idA, Status: queue.ItemStatusPending}, // duplicate
					},
				},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonDuplicateBeadID {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonDuplicateBeadID)
		}
	})

	t.Run("fail_cross_group_duplicate", func(t *testing.T) {
		t.Parallel()
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{GroupIndex: 0, Kind: queue.GroupKindWave, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: idA, Status: queue.ItemStatusPending}}},
				{GroupIndex: 1, Kind: queue.GroupKindWave, Status: queue.GroupStatusPending, Items: []queue.Item{{BeadID: idA, Status: queue.ItemStatusPending}}},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		ledger := validFixtureOpenLedger(idA)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonDuplicateBeadID {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonDuplicateBeadID)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-025: parallelism-narrowed informational notice
// ---------------------------------------------------------------------------

// TestValidateQM025ParallelismNarrowed verifies that a blocks edge within a
// group produces a LedgerDepPair notice but does NOT fail validation.
//
// Spec ref: queue-model.md §6.6 QM-025.
func TestValidateQM025ParallelismNarrowed(t *testing.T) {
	t.Parallel()

	const idX = core.BeadID("hk-ggg")
	const idY = core.BeadID("hk-hhh")

	t.Run("pass_with_notice_when_blocks_edge", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idX, idY)
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{
				idX: queue.BeadStatusOpen,
				idY: queue.BeadStatusOpen,
			},
			// idX blocks idY
			edges: map[[2]core.BeadID]bool{
				{idX, idY}: true,
			},
		}
		errs, notices, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("QM-025 must not fail validation; got errors: %v", errs)
		}
		if len(notices) == 0 {
			t.Fatal("expected at least one LedgerDepPair notice for blocks edge, got none")
		}
		found := false
		for _, n := range notices {
			if n.BeadID == idY && n.BlockerBeadID == idX {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected notice {BeadID:%q, BlockerBeadID:%q}, got %v", idY, idX, notices)
		}
	})

	t.Run("pass_no_blocks_edge_no_notice", func(t *testing.T) {
		t.Parallel()
		req := validFixtureSingleGroup(idX, idY)
		ledger := validFixtureOpenLedger(idX, idY)
		errs, notices, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
		if len(notices) != 0 {
			t.Fatalf("expected no notices, got %v", notices)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-026: persisted-size bound
// ---------------------------------------------------------------------------

// TestValidateQM026PersistedSizeBound verifies that a proposed mutation that
// would exceed 1 MiB fails with queue_too_large, and a normal-size queue passes.
//
// Spec ref: queue-model.md §6.7 QM-026.
func TestValidateQM026PersistedSizeBound(t *testing.T) {
	t.Parallel()

	t.Run("pass_normal_size", func(t *testing.T) {
		t.Parallel()
		const id = core.BeadID("hk-iii")
		req := validFixtureSingleGroup(id)
		ledger := validFixtureOpenLedger(id)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass, got errors: %v", errs)
		}
	})

	t.Run("fail_oversized_queue", func(t *testing.T) {
		t.Parallel()
		// Synthesise a large group by packing many items, each with a unique
		// bead ID long enough that the marshalled JSON envelope exceeds 1 MiB.
		// IDs are formatted as "hk-oversize-%07d" (18 chars each), giving ~100
		// bytes per item in JSON; 12000 items × ~100 bytes ≈ 1.2 MiB > 1 MiB.
		const numItems = 14000
		ids := make([]core.BeadID, numItems)
		for i := range ids {
			ids[i] = core.BeadID(fmt.Sprintf("hk-oversize-%07d", i))
		}
		items := make([]queue.Item, numItems)
		for i, id := range ids {
			items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
		}
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       queue.GroupKindWave,
					Status:     queue.GroupStatusPending,
					Items:      items,
				},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		// Ledger: all beads are open.
		ledger := &validFixtureFakeLedger{
			statuses: func() map[core.BeadID]queue.BeadStatus {
				m := make(map[core.BeadID]queue.BeadStatus, numItems)
				for _, id := range ids {
					m[id] = queue.BeadStatusOpen
				}
				return m
			}(),
			edges: map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error (queue_too_large), got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonQueueTooLarge {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonQueueTooLarge)
		}
		if errs[0].Detail["limit"] != 1048576 {
			t.Errorf("detail limit: got %v, want 1048576", errs[0].Detail["limit"])
		}
	})
}

// ---------------------------------------------------------------------------
// QM-027: single active queue (submit-only) — explicit skipped-for-append check
// ---------------------------------------------------------------------------

// TestValidateQM027SkippedForAppend verifies that QM-027 is not evaluated for
// append requests, i.e., an active queue does NOT cause append to fail with
// queue_already_active.
//
// Spec ref: queue-model.md §6.11 QM-029a (QM-027 is submit-only).
func TestValidateQM027SkippedForAppend(t *testing.T) {
	t.Parallel()

	const idA = core.BeadID("hk-jjj")

	activeQueue := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "active-queue-id",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{},
			},
		},
	}
	req := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: idA, Status: queue.ItemStatusPending}},
			},
		},
		ActiveQueue:      activeQueue,
		IsAppend:         true,
		AppendGroupIndex: 0,
	}
	ledger := validFixtureOpenLedger(idA)
	errs, _, err := queue.Validate(context.Background(), req, ledger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range errs {
		if e.Reason == queue.ReasonQueueAlreadyActive {
			t.Errorf("QM-027 must be skipped for append; got queue_already_active error")
		}
	}
}

// ---------------------------------------------------------------------------
// QM-029a: order-of-evaluation (first-failure short-circuit)
// ---------------------------------------------------------------------------

// TestValidateQM029aOrderOfEvaluation constructs a request that would fail
// multiple validation rules simultaneously and asserts that only the FIRST
// rule in the QM-029a sequence fires. The daemon MUST short-circuit on the
// first failing rule and return exactly one ValidationError.
//
// QM-029a order: QM-027 → QM-024 → QM-020 → QM-021 → QM-022 → QM-023 → QM-026.
//
// Spec ref: queue-model.md §6.11 QM-029a.
func TestValidateQM029aOrderOfEvaluation(t *testing.T) {
	t.Parallel()

	// QM-027 fires before QM-020: active queue + missing bead.
	// Both rules would fire independently, but QM-027 is first in sequence.
	t.Run("qm027_before_qm020", func(t *testing.T) {
		t.Parallel()
		const missingID = core.BeadID("hk-missing-order-027")
		req := validFixtureSingleGroup(missingID)
		req.ActiveQueue = &queue.Queue{
			SchemaVersion: 1,
			QueueID:       "order-test-queue",
			Status:        queue.QueueStatusActive,
		}
		// Empty ledger: missingID is not found (would trigger QM-020 independently).
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{},
			edges:    map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected exactly 1 error (first-failure short-circuit), got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonQueueAlreadyActive {
			t.Errorf("QM-029a: expected QM-027 (%q) to fire before QM-020; got %q",
				queue.ReasonQueueAlreadyActive, errs[0].Reason)
		}
	})

	// QM-020 fires before QM-021: missing bead preempts not-open bead.
	// Use two beads: one missing (QM-020), one closed (would trigger QM-021).
	// QM-020 is first, so bead_not_found must be the returned reason.
	t.Run("qm020_before_qm021", func(t *testing.T) {
		t.Parallel()
		const missingID = core.BeadID("hk-missing-order-020")
		const closedID = core.BeadID("hk-closed-order-021")
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       queue.GroupKindWave,
					Status:     queue.GroupStatusPending,
					Items: []queue.Item{
						{BeadID: missingID, Status: queue.ItemStatusPending},
						{BeadID: closedID, Status: queue.ItemStatusPending},
					},
				},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		// closedID is in the ledger as "closed"; missingID is absent (not_found).
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{
				closedID: queue.BeadStatus("closed"),
			},
			edges: map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected exactly 1 error (first-failure short-circuit), got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonBeadNotFound {
			t.Errorf("QM-029a: expected QM-020 (%q) to fire before QM-021; got %q",
				queue.ReasonBeadNotFound, errs[0].Reason)
		}
	})

	// QM-021 fires before QM-022: not-open bead preempts in-progress (double-dispatch) bead.
	// Use two beads: one closed (QM-021), one in_progress (would trigger QM-022).
	// QM-021 is iterated first, so bead_not_open must be the returned reason.
	t.Run("qm021_before_qm022", func(t *testing.T) {
		t.Parallel()
		const closedID = core.BeadID("hk-closed-order-021b")
		const inProgressID = core.BeadID("hk-inprogress-order-022")
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{
					GroupIndex: 0,
					Kind:       queue.GroupKindWave,
					Status:     queue.GroupStatusPending,
					Items: []queue.Item{
						{BeadID: closedID, Status: queue.ItemStatusPending},
						{BeadID: inProgressID, Status: queue.ItemStatusPending},
					},
				},
			},
			ActiveQueue: nil,
			IsAppend:    false,
		}
		ledger := &validFixtureFakeLedger{
			statuses: map[core.BeadID]queue.BeadStatus{
				closedID:     queue.BeadStatus("closed"),
				inProgressID: queue.BeadStatusInProgress,
			},
			edges: map[[2]core.BeadID]bool{},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected exactly 1 error (first-failure short-circuit), got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonBeadNotOpen {
			t.Errorf("QM-029a: expected QM-021 (%q) to fire before QM-022; got %q",
				queue.ReasonBeadNotOpen, errs[0].Reason)
		}
	})
}

// ---------------------------------------------------------------------------
// QM-025: parallelism-narrowed multi-event count
// ---------------------------------------------------------------------------

// TestValidateQM025ParallelismNarrowedMultiEvent submits a wave group with
// multiple blocker→blocked pairs and asserts that Validate returns exactly one
// LedgerDepPair notice per blocked item, validation passes (no errors), and the
// notice count implies parallelism_narrowed=true in QueueDryRunResponse.
//
// Spec ref: queue-model.md §6.6 QM-025.
func TestValidateQM025ParallelismNarrowedMultiEvent(t *testing.T) {
	t.Parallel()

	// Build a wave group with numPairs blocker→blocked pairs.
	// Pair k: blockerIDs[k] blocks blockedIDs[k].
	const numPairs = 3
	blockerIDs := [numPairs]core.BeadID{
		core.BeadID("hk-multi-blocker-0"),
		core.BeadID("hk-multi-blocker-1"),
		core.BeadID("hk-multi-blocker-2"),
	}
	blockedIDs := [numPairs]core.BeadID{
		core.BeadID("hk-multi-blocked-0"),
		core.BeadID("hk-multi-blocked-1"),
		core.BeadID("hk-multi-blocked-2"),
	}

	// Assemble one flat group containing all 2*numPairs beads.
	items := make([]queue.Item, 0, numPairs*2)
	for i := 0; i < numPairs; i++ {
		items = append(items,
			queue.Item{BeadID: blockerIDs[i], Status: queue.ItemStatusPending},
			queue.Item{BeadID: blockedIDs[i], Status: queue.ItemStatusPending},
		)
	}
	req := queue.ValidationRequest{
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items:      items,
			},
		},
		ActiveQueue: nil,
		IsAppend:    false,
	}

	// Ledger: all beads open; edges declare each pair as blocker→blocked.
	statuses := make(map[core.BeadID]queue.BeadStatus, numPairs*2)
	edges := make(map[[2]core.BeadID]bool, numPairs)
	for i := 0; i < numPairs; i++ {
		statuses[blockerIDs[i]] = queue.BeadStatusOpen
		statuses[blockedIDs[i]] = queue.BeadStatusOpen
		edges[[2]core.BeadID{blockerIDs[i], blockedIDs[i]}] = true
	}
	ledger := &validFixtureFakeLedger{statuses: statuses, edges: edges}

	errs, notices, err := queue.Validate(context.Background(), req, ledger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// QM-025 must NOT fail validation.
	if len(errs) != 0 {
		t.Fatalf("QM-025 must not produce validation errors; got %d: %v", len(errs), errs)
	}

	// Exactly numPairs notices — one per blocked item.
	if len(notices) != numPairs {
		t.Fatalf("expected %d LedgerDepPair notices (one per blocked item), got %d: %v",
			numPairs, len(notices), notices)
	}

	// Each blocked bead must appear exactly once with its correct blocker.
	for i := 0; i < numPairs; i++ {
		wantBlocked := blockedIDs[i]
		wantBlocker := blockerIDs[i]
		found := false
		for _, n := range notices {
			if n.BeadID == wantBlocked && n.BlockerBeadID == wantBlocker {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pair %d: expected notice {BeadID:%q, BlockerBeadID:%q}, not found in %v",
				i, wantBlocked, wantBlocker, notices)
		}
	}

	// parallelism_narrowed is true when notices is non-empty (QueueDryRunResponse.ParallelismNarrowed).
	parallelismNarrowed := len(notices) > 0
	if !parallelismNarrowed {
		t.Errorf("expected parallelism_narrowed=true (notices non-empty), got false")
	}
}

// ---------------------------------------------------------------------------
// QM-052a: handler-pause gate (submit-only)
// ---------------------------------------------------------------------------

// fakeHandlerPauseChecker is a test double for HandlerPauseChecker.
// It maps bead IDs to agent_types and records which agent_types are paused.
type fakeHandlerPauseChecker struct {
	// agentTypes maps bead ID → agent_type.
	agentTypes map[core.BeadID]core.AgentType

	// paused is the set of currently-paused agent_types.
	paused map[core.AgentType]bool
}

func (f *fakeHandlerPauseChecker) ResolvedAgentType(_ context.Context, id core.BeadID) (core.AgentType, error) {
	if at, ok := f.agentTypes[id]; ok {
		return at, nil
	}
	// Default to "claude-code" for any bead not explicitly mapped.
	return core.AgentTypeClaudeCode, nil
}

func (f *fakeHandlerPauseChecker) IsHandlerPaused(_ context.Context, agentType core.AgentType) (bool, error) {
	return f.paused[agentType], nil
}

// TestValidateQM052aHandlerPaused verifies that queue-submit is rejected with
// handler_paused when any bead's resolved agent_type maps to a paused handler,
// and that the check is skipped when PauseChecker is nil.
//
// Spec ref: specs/handler-pause.md §6 HP-025; queue-model.md §8.3a QM-052a.
// Bead ref: hk-siuo2.
func TestValidateQM052aHandlerPaused(t *testing.T) {
	t.Parallel()

	const (
		idA = core.BeadID("hk-aaa-pause")
		idB = core.BeadID("hk-bbb-pause")
		idC = core.BeadID("hk-ccc-pause")
	)

	t.Run("fail_paused_handler_single_bead", func(t *testing.T) {
		t.Parallel()
		// idA resolves to claude-code, which is paused.
		req := validFixtureSingleGroup(idA)
		ledger := validFixtureOpenLedger(idA)
		req.PauseChecker = &fakeHandlerPauseChecker{
			agentTypes: map[core.BeadID]core.AgentType{idA: core.AgentTypeClaudeCode},
			paused:     map[core.AgentType]bool{core.AgentTypeClaudeCode: true},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonHandlerPaused {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonHandlerPaused)
		}
		// Detail must include agent_type and bead_ids.
		if errs[0].Detail["agent_type"] != string(core.AgentTypeClaudeCode) {
			t.Errorf("detail agent_type: got %v, want %q", errs[0].Detail["agent_type"], core.AgentTypeClaudeCode)
		}
		beadIDs, ok := errs[0].Detail["bead_ids"].([]string)
		if !ok || len(beadIDs) == 0 {
			t.Errorf("detail bead_ids: expected non-empty []string, got %v", errs[0].Detail["bead_ids"])
		}
	})

	t.Run("fail_paused_handler_multiple_beads_same_type", func(t *testing.T) {
		t.Parallel()
		// idA and idB both resolve to claude-code (paused); idC resolves to pi (live).
		items := []queue.Item{
			{BeadID: idA, Status: queue.ItemStatusPending},
			{BeadID: idB, Status: queue.ItemStatusPending},
			{BeadID: idC, Status: queue.ItemStatusPending},
		}
		req := queue.ValidationRequest{
			Groups: []queue.Group{
				{GroupIndex: 0, Kind: queue.GroupKindWave, Status: queue.GroupStatusPending, Items: items},
			},
		}
		ledger := validFixtureOpenLedger(idA, idB, idC)
		req.PauseChecker = &fakeHandlerPauseChecker{
			agentTypes: map[core.BeadID]core.AgentType{
				idA: core.AgentTypeClaudeCode,
				idB: core.AgentTypeClaudeCode,
				idC: core.AgentTypePi,
			},
			paused: map[core.AgentType]bool{core.AgentTypeClaudeCode: true},
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if errs[0].Reason != queue.ReasonHandlerPaused {
			t.Errorf("reason: got %q, want %q", errs[0].Reason, queue.ReasonHandlerPaused)
		}
		// Both idA and idB must appear in bead_ids; idC (pi, live) must not.
		beadIDs, ok := errs[0].Detail["bead_ids"].([]string)
		if !ok {
			t.Fatalf("detail bead_ids: expected []string, got %T %v", errs[0].Detail["bead_ids"], errs[0].Detail["bead_ids"])
		}
		if len(beadIDs) != 2 {
			t.Errorf("expected 2 affected bead_ids (idA + idB), got %d: %v", len(beadIDs), beadIDs)
		}
	})

	t.Run("pass_live_handler", func(t *testing.T) {
		t.Parallel()
		// idA resolves to claude-code, which is NOT paused.
		req := validFixtureSingleGroup(idA)
		ledger := validFixtureOpenLedger(idA)
		req.PauseChecker = &fakeHandlerPauseChecker{
			agentTypes: map[core.BeadID]core.AgentType{idA: core.AgentTypeClaudeCode},
			paused:     map[core.AgentType]bool{}, // no handlers paused
		}
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass for live handler, got errors: %v", errs)
		}
	})

	t.Run("pass_nil_checker_skips_qm052a", func(t *testing.T) {
		t.Parallel()
		// PauseChecker is nil → QM-052a is skipped entirely.
		req := validFixtureSingleGroup(idA)
		ledger := validFixtureOpenLedger(idA)
		// req.PauseChecker is nil (zero value)
		errs, _, err := queue.Validate(context.Background(), req, ledger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(errs) != 0 {
			t.Fatalf("expected pass when PauseChecker is nil, got errors: %v", errs)
		}
	})
}
