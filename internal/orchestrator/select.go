package orchestrator

import (
	"sort"

	"github.com/gregberns/harmonik/internal/core"
)

// select.go — the pure NQ-B1 cross-queue round-robin selector, ported faithfully
// from internal/daemon/workloop.go selectNextQueue WITHOUT semantic change (M5
// slice 3 sub-slice 3A). The daemon projects live QueueStore/RunRegistry state
// into FleetSnapshot under the write lock (see internal/daemon snapshotFleet),
// calls SelectNextQueue, then maps Selection back onto its queueSelection shape.
// The per-queue LOCAL cap (LocalInFlight vs WorkerCap) is the only capacity gate
// here; the global ceiling is enforced by the daemon caller before this runs.
//
// Spec ref: specs/queue-model.md §9.3 QM-062, §9.7 QM-066, §9.8 QM-067.
// Bead ref: hk-tigaf.4 (NQ-B1).

// ItemSnapshot is the minimal per-item projection the selector returns as its
// pick. ItemIdx is the ABSOLUTE index into the group's Items slice (matching the
// daemon's write-back index), NOT an index into the eligible sub-slice.
type ItemSnapshot struct {
	ItemIdx        int // absolute index into Group.Items (for the dispatch stamp)
	BeadID         core.BeadID
	Context        string
	WorkflowMode   string
	WorkflowRef    string
	TemplateParams map[string]string
}

// GroupSnapshot is the queue's first active group projected head-first: its
// identity plus the order-preserved eligible-item list. The daemon builds
// Eligible from queue.EligibleItems, stamping each item's absolute index.
type GroupSnapshot struct {
	GroupIndex int
	Eligible   []ItemSnapshot
}

// QueueSnapshot is one queue's point-in-time dispatch-relevant state, projected
// under the QueueStore write lock at the top of a dispatch tick.
type QueueSnapshot struct {
	Name          string // map key (already normalised)
	QueueID       string // staleness guard downstream
	Active        bool   // q.Status == queue.QueueStatusActive
	Blocked       bool   // blockedQueues[name] (hk-xg6rw dashboard forcing-gate)
	LocalInFlight int    // reg.LenForQueueLocal(name)
	WorkerCap     int    // queue.DefaultWorkers(q.Workers, globalCap) — precomputed
	LocalOnly     bool   // mirrors Queue.LocalOnly
	WorkerTarget  string // mirrors Queue.WorkerTarget
	// ActiveGroup is the FIRST active group (nil when none). It carries the
	// eligible-item head; a nil or empty-eligible group makes the queue a
	// non-candidate this tick (but does not block siblings).
	ActiveGroup *GroupSnapshot
}

// FleetSnapshot bundles the per-tick selector input. RRCursor is passed BY VALUE
// (daemon owns the increment); the selector never mutates it.
type FleetSnapshot struct {
	Queues   []QueueSnapshot
	RRCursor int
}

// Selection is the pure selector result. It maps 1:1 onto the daemon's
// queueSelection shape (the daemon shell copies the fields back).
type Selection struct {
	QueueName    string
	QueueID      string
	GroupIndex   int
	Item         ItemSnapshot
	LocalOnly    bool
	WorkerTarget string
	// SawNonContributing mirrors the daemon's anyPausedOrEmpty flag: at least
	// one queue existed but contributed nothing this tick. Only meaningful on
	// the (Selection{}, false) return; false on a successful pick.
	SawNonContributing bool
}

// SelectNextQueue implements the QM-062/QM-067 two-level capacity gate plus the
// cross-queue round-robin dispatch policy. It scans every projected queue and
// returns the next (queue, active group, first eligible item) to dispatch, or
// (Selection{}, false) when no queue can contribute under its per-queue LOCAL
// Workers cap.
//
// Policy (NQ-B1), preserved EXACTLY from the daemon's selectNextQueue:
//   - A queue is a candidate iff it is Active, not Blocked, its LocalInFlight is
//     below its WorkerCap, and its first active group has ≥1 eligible item.
//   - Candidate names are sorted lexicographically, then the round-robin cursor
//     (advanced by the CALLER every tick, never reset to 0) selects the start
//     offset — this is what prevents a lexicographically-earlier queue from
//     perpetually starving a later one.
//
// The per-queue cap counts LOCAL runs only (hk-4tjt6): an all-remote queue
// admits up to its worker-slot capacity rather than being capped at
// max_concurrent — LocalInFlight excludes remote runs by construction.
func SelectNextQueue(f FleetSnapshot) (Selection, bool) {
	if len(f.Queues) == 0 {
		return Selection{}, false
	}

	// Build the candidate set: queues with eligible work under their own LOCAL cap.
	candidates := make([]string, 0, len(f.Queues))
	byName := make(map[string]QueueSnapshot, len(f.Queues))
	sawNonContributing := false
	for _, q := range f.Queues {
		byName[q.Name] = q
		if !q.Active {
			// Paused-by-failure / paused-by-drain / completed queues contribute
			// nothing but MUST NOT block sibling queues.
			sawNonContributing = true
			continue
		}
		if q.Blocked {
			// hk-xg6rw: captain-curated queue gated by a stale dashboard.json.
			sawNonContributing = true
			continue
		}
		// Per-queue LOCAL cap (hk-4tjt6): skip when already at its ceiling.
		if q.LocalInFlight >= q.WorkerCap {
			sawNonContributing = true
			continue
		}
		// Must have a first active group with at least one eligible item.
		if q.ActiveGroup == nil || len(q.ActiveGroup.Eligible) == 0 {
			sawNonContributing = true
			continue
		}
		candidates = append(candidates, q.Name)
	}

	if len(candidates) == 0 {
		return Selection{}, false
	}
	sort.Strings(candidates)

	// Round-robin: start at the cursor offset (mod candidate count). The caller
	// advances RRCursor every tick so the start offset rotates.
	n := len(candidates)
	start := ((f.RRCursor % n) + n) % n // guard against negative cursor
	chosen := byName[candidates[start]]

	g := chosen.ActiveGroup
	if g == nil || len(g.Eligible) == 0 {
		// Defensive: a candidate always has an eligible head, so this is
		// unreachable, but mirror the daemon's non-selection fall-through.
		return Selection{SawNonContributing: sawNonContributing}, false
	}
	head := g.Eligible[0]
	return Selection{
		QueueName:    chosen.Name,
		QueueID:      chosen.QueueID,
		GroupIndex:   g.GroupIndex,
		Item:         head,
		LocalOnly:    chosen.LocalOnly,
		WorkerTarget: chosen.WorkerTarget,
	}, true
}
