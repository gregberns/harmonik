package orchestrator

import "github.com/gregberns/harmonik/internal/core"

// eagerfill.go — the pure EM-062 eager-refill + EM-063 pre-screen DECISIONS,
// ported faithfully from internal/daemon/eagerfill_em063.go WITHOUT semantic
// change (M5 slice 3B). The daemon shell owns every effect: it holds the
// QueueStore write lock, projects live state into FleetSnapshot / the in-queue
// set, runs kerf, does the git Phase-2 check, emits events, and appends items.
// These functions only decide: where the deficit is, how many to over-fetch,
// how many survivors to keep, and which candidates are already queued.
//
// Spec ref: specs/execution-model.md §4.13 EM-062, EM-063.
// Bead ref: hk-9321v (eagerRefill).

// groupKindStream is the string projection of queue.GroupKindStream. The daemon
// projects the enum to this string at the snapshot boundary so orchestrator
// never imports internal/queue (mirrors GroupSnapshot.Kind's doc).
const groupKindStream = "stream"

// eagerfillOverfetchFactor is the EM-062 OVERFETCH_FACTOR: kerf next is called
// with limit = deficit × factor so that pre-screen rejections do not leave an
// avoidable gap in the filled stream. Mirrors the daemon constant of the same
// name (execution-model.md §4.13 EM-062).
const eagerfillOverfetchFactor = 2

// FillTarget names the active stream group that has an available-slot deficit,
// the pick EagerFillTarget returns. GroupPos is the group's index within its
// queue; Deficit is how many slots the refill should try to fill.
type FillTarget struct {
	QueueName string
	QueueID   string
	GroupPos  int
	Deficit   int
}

// EagerFillTarget computes the EM-062 available-slot deficit and picks the first
// active stream group that has one, scanning queues in FleetSnapshot order. It
// returns (target, true) for the first match, or (FillTarget{}, false) when no
// slot is available or no active stream group is short of pending work.
//
// Deficit math, preserved EXACTLY from the daemon's eagerRefillEval:
//
//	available = maxConcurrent - inFlight   // global slots free right now
//	deficit   = available - pendingCount   // per active stream group
//
// A group qualifies iff its queue is Active, its (first) active group is a
// stream, and deficit > 0. Pending items already in the group are subtracted
// because they will fill slots without any refill action.
func EagerFillTarget(f FleetSnapshot, maxConcurrent, inFlight int) (FillTarget, bool) {
	available := maxConcurrent - inFlight
	if available <= 0 {
		return FillTarget{}, false
	}
	for _, q := range f.Queues {
		if !q.Active {
			continue
		}
		g := q.ActiveGroup
		if g == nil || g.Kind != groupKindStream {
			continue
		}
		deficit := available - g.PendingCount
		if deficit <= 0 {
			continue
		}
		return FillTarget{
			QueueName: q.Name,
			QueueID:   q.QueueID,
			GroupPos:  g.GroupIndex,
			Deficit:   deficit,
		}, true
	}
	return FillTarget{}, false
}

// OverfetchLimit returns the EM-062 kerf-next limit for a given deficit:
// deficit × OVERFETCH_FACTOR. It over-fetches so pre-screen rejections do not
// leave an avoidable gap in the filled stream.
func OverfetchLimit(deficit int) int {
	return deficit * eagerfillOverfetchFactor
}

// ClampSurvivors caps survivors at deficit entries, preserving kerf's priority
// order (kerf returns candidates highest-priority first). When deficit is >=
// len(survivors) it returns survivors unchanged; a non-positive deficit yields
// an empty slice.
func ClampSurvivors(survivors []core.BeadID, deficit int) []core.BeadID {
	if deficit < 0 {
		deficit = 0
	}
	if len(survivors) > deficit {
		return survivors[:deficit]
	}
	return survivors
}

// ScreenAlreadyQueued is the EM-063 Phase-1 set-membership filter: it drops any
// candidate already present in the in-queue set (beads with a pending,
// dispatched, completed, or failed item somewhere in the loaded queues). The
// daemon builds inQueue under the lock (the effect) and passes it in; Phase-2's
// git-landed check stays a daemon loop over these survivors.
//
// Order is preserved and duplicates in candidates are passed through unchanged
// (the daemon's original loop did the same). A nil inQueue drops nothing.
func ScreenAlreadyQueued(candidates []core.BeadID, inQueue map[core.BeadID]struct{}) []core.BeadID {
	survivors := make([]core.BeadID, 0, len(candidates))
	for _, id := range candidates {
		if _, alreadyIn := inQueue[id]; alreadyIn {
			continue
		}
		survivors = append(survivors, id)
	}
	return survivors
}
