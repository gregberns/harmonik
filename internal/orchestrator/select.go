package orchestrator

import (
	"sort"

	"github.com/gregberns/harmonik/internal/core"
)

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
	// Kind is the group's dispatch kind projected as a string ("wave" | "stream"),
	// never queue.GroupKind (keeps orchestrator off internal/queue). Read only by
	// the eager-fill decision (EagerFillTarget, M5 slice 3B); the selector ignores it.
	Kind string
	// PendingCount is the number of ItemStatusPending items in the group — the
	// eager-fill deficit input (M5 slice 3B). Counted independently of Eligible so
	// it faithfully mirrors the daemon's original pending scan.
	PendingCount int
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
	// DefaultHarness is the persisted queue-owned tier-2 harness default. The
	// selector carries it opaquely; harness precedence is resolved downstream.
	DefaultHarness core.AgentType
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
	// SkipBeads holds the bead IDs the CALLER already knows it will refuse this
	// tick, keyed by string bead ID. A refusal here is specific to the ITEM, not
	// to the queue: the bead is claimed by a sibling queue, or it waits for a
	// captain to greenlight it. The selector steps over such an item and offers
	// the next eligible one, which is what stops one refused item from holding
	// up every ready item behind it (hk-nown4).
	//
	// The daemon owns the clock: it expires its own cooldowns and passes a plain
	// set, so the selector stays a pure function of its input. nil means nothing
	// is refused.
	SkipBeads map[string]bool
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
	// DefaultHarness is the selected queue's immutable tier-2 harness default.
	DefaultHarness core.AgentType
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
//     below its WorkerCap, and its first active group has ≥1 eligible item the
//     caller has not already refused (see FleetSnapshot.SkipBeads).
//   - Candidate names are sorted lexicographically, then the round-robin cursor
//     (advanced by the CALLER every tick, never reset to 0) selects the start
//     offset — this is what prevents a lexicographically-earlier queue from
//     perpetually starving a later one.
//   - Within the chosen queue the selector offers the FIRST eligible item the
//     caller has not refused. It does not stop at the head. A queue whose every
//     eligible item is refused contributes nothing this tick and hands its slot
//     to a sibling instead of consuming it (hk-nown4).
//
// The per-queue cap counts LOCAL runs only (hk-4tjt6): an all-remote queue
// admits up to its worker-slot capacity rather than being capped at
// max_concurrent — LocalInFlight excludes remote runs by construction.
func SelectNextQueue(f FleetSnapshot) (Selection, bool) {
	if len(f.Queues) == 0 {
		return Selection{}, false
	}

	candidates := make([]string, 0, len(f.Queues))
	byName := make(map[string]QueueSnapshot, len(f.Queues))
	sawNonContributing := false
	for _, q := range f.Queues {
		byName[q.Name] = q
		if !q.Active {
			sawNonContributing = true
			continue
		}
		if q.Blocked {
			sawNonContributing = true
			continue
		}
		if q.LocalInFlight >= q.WorkerCap {
			sawNonContributing = true
			continue
		}
		if q.ActiveGroup == nil || firstOfferable(q.ActiveGroup.Eligible, f.SkipBeads) < 0 {
			sawNonContributing = true
			continue
		}
		candidates = append(candidates, q.Name)
	}

	if len(candidates) == 0 {
		return Selection{}, false
	}
	sort.Strings(candidates)

	n := len(candidates)
	start := ((f.RRCursor % n) + n) % n // guard against negative cursor
	chosen := byName[candidates[start]]

	g := chosen.ActiveGroup
	var pick int
	if g == nil {
		pick = -1
	} else {
		pick = firstOfferable(g.Eligible, f.SkipBeads)
	}
	if pick < 0 {
		return Selection{SawNonContributing: sawNonContributing}, false
	}
	head := g.Eligible[pick]
	return Selection{
		QueueName:      chosen.Name,
		QueueID:        chosen.QueueID,
		GroupIndex:     g.GroupIndex,
		Item:           head,
		LocalOnly:      chosen.LocalOnly,
		WorkerTarget:   chosen.WorkerTarget,
		DefaultHarness: chosen.DefaultHarness,
	}, true
}

func firstOfferable(eligible []ItemSnapshot, skip map[string]bool) int {
	for i := range eligible {
		if !skip[string(eligible[i].BeadID)] {
			return i
		}
	}
	return -1
}
