package daemon

// workloop_perqueue_roundrobin_hktigaf4_test.go — NQ-B1 unit tests for the
// two-level capacity gate and cross-queue round-robin dispatch policy.
//
// Bead: hk-tigaf.4 (NQ-B1)
// Spec refs:
//   - specs/queue-model.md §9.3 QM-062 (capacity composition)
//   - specs/queue-model.md §9.7 QM-066 (per-queue worker count)
//   - specs/queue-model.md §9.8 QM-067 (cross-queue round-robin dispatch policy)
//
// Scope: pure in-memory unit tests of RunRegistry.LenForQueue (per-queue tally),
// queue.DefaultWorkers (per-queue worker default), and selectNextQueue (the
// round-robin selector). No live daemon, no event bus — these exercise the
// dispatch-gate primitives directly, fast.
//
// Helper prefix: perQueueRR (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// perQueueRRHandle returns a minimal *RunHandle tagged with queueName.
func perQueueRRHandle(beadID, queueName string) *RunHandle {
	return &RunHandle{
		BeadID:    core.BeadID(beadID),
		QueueName: queueName,
		StartedAt: time.Now(),
	}
}

// perQueueRRRunID mints a fresh core.RunID (UUIDv7) for registry registration.
func perQueueRRRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("perQueueRRRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// perQueueRRWaveQueue builds an active queue with one active wave group holding
// `pending` pending items (bead IDs prefix-<name>-<i>), and the given Workers.
func perQueueRRWaveQueue(name, queueID string, pending, workers int) *queue.Queue {
	items := make([]queue.Item, pending)
	for i := range items {
		items[i] = queue.Item{
			BeadID: core.BeadID(name + "-" + string(rune('a'+i))),
			Status: queue.ItemStatusPending,
		}
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          name,
		Workers:       workers,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// RunRegistry.LenForQueue — per-queue tally
// ---------------------------------------------------------------------------

func TestPerQueueRR_LenForQueue_FiltersByQueueName(t *testing.T) {
	t.Parallel()

	reg := NewRunRegistry()
	// Register 3 main runs, 1 investigate run, 1 br-ready (empty name) run.
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-m1", "main"))
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-m2", "main"))
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-m3", "main"))
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-i1", "investigate"))
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-r1", "")) // br-ready

	if got := reg.LenForQueue("main"); got != 3 {
		t.Errorf("LenForQueue(main) = %d, want 3", got)
	}
	if got := reg.LenForQueue("investigate"); got != 1 {
		t.Errorf("LenForQueue(investigate) = %d, want 1", got)
	}
	if got := reg.LenForQueue(""); got != 1 {
		t.Errorf("LenForQueue(\"\") = %d, want 1 (br-ready run)", got)
	}
	// Bare Len() stays the GLOBAL ceiling: counts every handle.
	if got := reg.Len(); got != 5 {
		t.Errorf("Len() = %d, want 5 (global ceiling counts all runs)", got)
	}
}

// ---------------------------------------------------------------------------
// queue.DefaultWorkers — per-queue worker default (QM-066)
// ---------------------------------------------------------------------------

func TestPerQueueRR_DefaultWorkers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		requested int
		globalCap int
		want      int
	}{
		{"zero defaults to global cap", 0, 4, 4},
		{"negative defaults to global cap", -1, 4, 4},
		{"positive honoured verbatim", 3, 4, 3},
		{"oversubscription permitted", 8, 4, 8},
		{"global cap floored at 1", 0, 0, 1},
		{"global cap floored at 1 (negative)", 0, -5, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := queue.DefaultWorkers(tc.requested, tc.globalCap); got != tc.want {
				t.Errorf("DefaultWorkers(%d, %d) = %d, want %d", tc.requested, tc.globalCap, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// selectNextQueue — per-queue cap gate
// ---------------------------------------------------------------------------

// TestPerQueueRR_PerQueueCapBoundsDispatch verifies that a queue already at its
// Workers ceiling is excluded from selection even when it still has pending
// items and the global ceiling has room.
func TestPerQueueRR_PerQueueCapBoundsDispatch(t *testing.T) {
	t.Parallel()

	qs := NewQueueStore()
	// main: workers=3 with 3 pending; investigate: workers=1 with 1 pending.
	qs.SetQueueByName("main", perQueueRRWaveQueue("main", "qid-main", 3, 3))
	qs.SetQueueByName("investigate", perQueueRRWaveQueue("investigate", "qid-inv", 1, 1))

	reg := NewRunRegistry()
	// investigate is at its cap (1 running, workers=1); main has 0 running.
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-i-running", "investigate"))

	lq := qs.LockForMutation()
	sel, ok := selectNextQueue(lq, reg, 4 /*globalCap*/, 0 /*cursor*/)
	lq.Done()

	if !ok {
		t.Fatal("selectNextQueue returned ok=false, want a selection from main")
	}
	if sel.queueName != "main" {
		t.Errorf("selected queue = %q, want main (investigate is at its per-queue cap)", sel.queueName)
	}
}

// TestPerQueueRR_AllAtCapSelectsNothing verifies that when every queue is at its
// per-queue Workers ceiling, selectNextQueue returns ok=false (the workloop then
// idles) — the per-queue gate composes with the global gate.
func TestPerQueueRR_AllAtCapSelectsNothing(t *testing.T) {
	t.Parallel()

	qs := NewQueueStore()
	qs.SetQueueByName("main", perQueueRRWaveQueue("main", "qid-main", 3, 1))
	qs.SetQueueByName("investigate", perQueueRRWaveQueue("investigate", "qid-inv", 1, 1))

	reg := NewRunRegistry()
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-m-running", "main"))        // main at cap (workers=1)
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-i-running", "investigate")) // investigate at cap

	lq := qs.LockForMutation()
	_, ok := selectNextQueue(lq, reg, 4, 0)
	lq.Done()

	if ok {
		t.Error("selectNextQueue returned ok=true, want false (both queues at per-queue cap)")
	}
}

// ---------------------------------------------------------------------------
// selectNextQueue — round-robin cursor advance (no starvation)
// ---------------------------------------------------------------------------

// TestPerQueueRR_CursorAdvanceRotatesSelection is the load-bearing NQ-B1 test:
// it proves the round-robin cursor — when ADVANCED every tick (not reset to 0) —
// rotates dispatch fairly among queues so the lexicographically-earlier queue
// ("investigate") does NOT starve the later one ("main").
//
// With cursor reset-to-0 each tick, "investigate" (sorts first) would win every
// tick and starve "main". Advancing the cursor every tick rotates the start
// offset so both queues are selected in turn.
func TestPerQueueRR_CursorAdvanceRotatesSelection(t *testing.T) {
	t.Parallel()

	qs := NewQueueStore()
	// Both queues have ample pending work and high per-queue caps so neither is
	// ever gated out — the only thing deciding the winner is the round-robin
	// cursor.
	qs.SetQueueByName("main", perQueueRRWaveQueue("main", "qid-main", 10, 10))
	qs.SetQueueByName("investigate", perQueueRRWaveQueue("investigate", "qid-inv", 10, 10))

	reg := NewRunRegistry() // empty: nothing in-flight, so caps never bind here

	picks := map[string]int{}
	cursor := 0
	const ticks = 10
	for i := 0; i < ticks; i++ {
		lq := qs.LockForMutation()
		sel, ok := selectNextQueue(lq, reg, 10, cursor)
		lq.Done()
		if !ok {
			t.Fatalf("tick %d: selectNextQueue ok=false, want a selection", i)
		}
		picks[sel.queueName]++
		cursor++ // ADVANCE every tick — the no-starvation invariant
	}

	// Sorted candidate names are ["investigate", "main"]. Over 10 ticks with the
	// cursor advancing each tick, selection must alternate, giving each queue 5.
	if picks["main"] == 0 {
		t.Errorf("main was never selected over %d ticks — STARVATION (cursor not advancing?)", ticks)
	}
	if picks["investigate"] == 0 {
		t.Errorf("investigate was never selected over %d ticks", ticks)
	}
	if picks["main"] != ticks/2 || picks["investigate"] != ticks/2 {
		t.Errorf("uneven round-robin: main=%d investigate=%d, want %d each",
			picks["main"], picks["investigate"], ticks/2)
	}
}

// TestPerQueueRR_CursorResetWouldStarve documents the failure mode the advancing
// cursor prevents: holding the cursor at 0 every tick makes the
// lexicographically-first queue win every time, starving the other. This is a
// regression guard — if someone "simplifies" selectNextQueue to ignore the
// cursor, this test makes the starvation explicit.
func TestPerQueueRR_CursorResetWouldStarve(t *testing.T) {
	t.Parallel()

	qs := NewQueueStore()
	qs.SetQueueByName("main", perQueueRRWaveQueue("main", "qid-main", 10, 10))
	qs.SetQueueByName("investigate", perQueueRRWaveQueue("investigate", "qid-inv", 10, 10))

	reg := NewRunRegistry()

	picks := map[string]int{}
	const ticks = 6
	for i := 0; i < ticks; i++ {
		lq := qs.LockForMutation()
		sel, ok := selectNextQueue(lq, reg, 10, 0 /*cursor pinned at 0 — the anti-pattern*/)
		lq.Done()
		if !ok {
			t.Fatalf("tick %d: ok=false", i)
		}
		picks[sel.queueName]++
	}

	// With the cursor pinned at 0, "investigate" (sorts first) wins every tick.
	// This asserts the documented anti-pattern so the test stays meaningful.
	if picks["investigate"] != ticks {
		t.Errorf("cursor-pinned-at-0: investigate=%d, want %d (sorts first, wins every tick)",
			picks["investigate"], ticks)
	}
	if picks["main"] != 0 {
		t.Errorf("cursor-pinned-at-0: main=%d, want 0 (starved) — confirms why the cursor MUST advance",
			picks["main"])
	}
}

// ---------------------------------------------------------------------------
// LenForQueueLocal — local-only per-queue tally (hk-4tjt6)
// ---------------------------------------------------------------------------

// perQueueRRRemoteHandle returns a *RunHandle tagged with queueName and Remote=true.
func perQueueRRRemoteHandle(beadID, queueName string) *RunHandle {
	h := &RunHandle{
		BeadID:    core.BeadID(beadID),
		QueueName: queueName,
		StartedAt: time.Now(),
	}
	h.Remote.Store(true)
	return h
}

// TestPerQueueRR_LenForQueueLocal_ExcludesRemote verifies that LenForQueueLocal
// counts only local (non-remote) handles and that LenForQueue still counts all.
func TestPerQueueRR_LenForQueueLocal_ExcludesRemote(t *testing.T) {
	t.Parallel()

	reg := NewRunRegistry()
	// 2 local runs on "jessica-sat", 3 remote runs on "jessica-sat".
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-j-local-1", "jessica-sat"))
	reg.Register(perQueueRRRunID(t), perQueueRRHandle("hk-j-local-2", "jessica-sat"))
	reg.Register(perQueueRRRunID(t), perQueueRRRemoteHandle("hk-j-remote-1", "jessica-sat"))
	reg.Register(perQueueRRRunID(t), perQueueRRRemoteHandle("hk-j-remote-2", "jessica-sat"))
	reg.Register(perQueueRRRunID(t), perQueueRRRemoteHandle("hk-j-remote-3", "jessica-sat"))

	if got := reg.LenForQueueLocal("jessica-sat"); got != 2 {
		t.Errorf("LenForQueueLocal(jessica-sat) = %d, want 2 (local only)", got)
	}
	if got := reg.LenForQueue("jessica-sat"); got != 5 {
		t.Errorf("LenForQueue(jessica-sat) = %d, want 5 (all runs)", got)
	}
}

// TestPerQueueRR_AllRemoteQueueAdmitsBeyondMaxConcurrent is the hk-4tjt6
// regression guard: an all-remote queue (local=0) must NOT be blocked by the
// per-queue Workers ceiling even when remote runs >= max_concurrent. Before
// this fix, LenForQueue counted remote runs and gated the queue at max_concurrent=4,
// preventing remote slots 5-6 from ever being offered.
func TestPerQueueRR_AllRemoteQueueAdmitsBeyondMaxConcurrent(t *testing.T) {
	t.Parallel()

	const globalCap = 4 // max_concurrent — the old gate would cap the queue here
	qs := NewQueueStore()
	// jessica-sat: no explicit Workers (defaults to globalCap), 3 pending items.
	qs.SetQueueByName("jessica-sat", perQueueRRWaveQueue("jessica-sat", "qid-jsat", 3, 0 /*Workers=0 → default*/))

	reg := NewRunRegistry()
	// Simulate 4 remote runs already in-flight (local=0). This is the exact
	// condition that caused the level-2 gate to block the queue before hk-4tjt6.
	for i := range 4 {
		reg.Register(perQueueRRRunID(t), perQueueRRRemoteHandle(
			"hk-jsat-remote-"+string(rune('a'+i)), "jessica-sat"))
	}

	lq := qs.LockForMutation()
	sel, ok := selectNextQueue(lq, reg, globalCap, 0)
	lq.Done()

	if !ok {
		t.Fatal("selectNextQueue returned ok=false for all-remote queue at remote=4, local=0 — level-2 gate must NOT block on remote runs (hk-4tjt6)")
	}
	if sel.queueName != "jessica-sat" {
		t.Errorf("selected queue = %q, want jessica-sat", sel.queueName)
	}
}
