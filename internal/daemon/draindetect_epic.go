package daemon

// draindetect_epic.go — the ledger/epic axis of the genuine-drain oracle
// (hk-95uf defense #2, epic-portion).
//
// Phase A stub: returns DRAINED unconditionally. Phase B replaces the body with
// the real open-epic-with-ready-child scan (see GenuineDrain's call site).

import "context"

// scanOpenEpics evaluates defense #2's epic-portion: for every OPEN epic E, a
// child C that is otherwise-ready but blocked by E (BlocksEdge(E,C)) is hidden
// from `br ready` and would be lost to a naive empty-ready drain check. Such a
// child ⇒ HAS_WORK.
//
// Phase A: not yet implemented — returns DRAINED so the queue/run/ready axes
// stand alone as a working oracle. Phase B wires the real scan via d.lister
// (open beads) and d.ledger (BlocksEdge).
func (d *DrainDetector) scanOpenEpics(_ context.Context) (DrainResult, error) {
	return DrainResult{State: DrainStateDrained}, nil
}
