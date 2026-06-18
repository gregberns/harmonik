package daemon

// draindetect_epic.go — the ledger/epic axis of the genuine-drain oracle
// (hk-95uf defense #2 epic-portion; Phase B, hk-rai2).
//
// Phase A shipped a DRAINED stub here so the queue/run/ready axes could stand
// alone. Phase B (this file) makes the axis real: an OPEN epic with a
// non-terminal child that the ledger says it blocks is genuine pending work
// that `br ready` cannot see (the child reports status "blocked" while the epic
// is open), so it MUST keep the fleet awake. Without this axis a false DRAINED
// could sleep the fleet with an open-epic ready-but-blocked child — the
// Phase-A reviewer's explicit safety flag, and the reason M1 (quiesce, which
// wires this oracle to the sleep decision) sequences AFTER this bead.

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// beadTypeEpic is the Beads issue_type string for an epic. BeadType is an
// opaque Beads-owned enum (core.BeadRecord.BeadType); "epic" is its epic
// member (CLAUDE.md §Beads types: task, bug, feature, epic, chore, docs,
// question).
const beadTypeEpic = "epic"

// scanOpenEpics evaluates defense #2's epic-portion: for every OPEN epic E, a
// non-terminal child C that the ledger declares E blocks (BlocksEdge(E,C)) is
// otherwise-ready work hidden from `br ready` — C reports status "blocked"
// while E is open. Any such child ⇒ HAS_WORK.
//
// Candidate-child universe: the union of the ledger's open and blocked beads.
// The open list supplies the epics (an epic has no blockers, so it reports
// "open") and the blocked list supplies the children waiting on an open
// blocker; both are scanned so the axis is robust to either status convention.
// A child blocked by an open epic AND some other open bead is still flagged —
// fail-safe favours HAS_WORK, and any such child is genuine pending work.
//
// Fail-closed (load-bearing — a false DRAINED could sleep the fleet with work
// pending): a nil lister/ledger seam, a bead-list error, or a BlocksEdge error
// yields UNSURE / a wrapped error (GenuineDrain maps both to UNSURE). DRAINED
// is returned ONLY on positive emptiness: no open epic blocks any non-terminal
// child. This axis MUST NOT consult `kerf next` (external, false-empty for
// works lacking a bead_filter); `br ready --limit 0` and the ledger are
// authoritative (defense #5).
func (d *DrainDetector) scanOpenEpics(ctx context.Context) (DrainResult, error) {
	if d.lister == nil || d.ledger == nil {
		// The epic axis cannot prove positive emptiness without its seams.
		// Fail-closed: UNSURE keeps the fleet awake. A nil seam must NEVER
		// license a DRAINED verdict.
		return unsure("ledger/epic axis not wired (lister/ledger nil)"), nil
	}

	open, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusOpen))
	if err != nil {
		return DrainResult{}, fmt.Errorf("list open beads: %w", err)
	}

	var epics []core.BeadRecord
	for _, b := range open {
		if b.BeadType == beadTypeEpic {
			epics = append(epics, b)
		}
	}
	if len(epics) == 0 {
		// No open epics ⇒ this axis is positively empty.
		return DrainResult{State: DrainStateDrained}, nil
	}

	blocked, err := d.lister.ListBeadsByStatus(ctx, string(core.CoarseStatusBlocked))
	if err != nil {
		return DrainResult{}, fmt.Errorf("list blocked beads: %w", err)
	}

	// Candidate children = open ∪ blocked. The first open-epic→child blocks
	// edge is sufficient to flag HAS_WORK and short-circuit (the reasons string
	// is diagnostic; one is enough to keep the fleet awake).
	candidates := make([]core.BeadRecord, 0, len(open)+len(blocked))
	candidates = append(candidates, open...)
	candidates = append(candidates, blocked...)

	for _, e := range epics {
		for _, c := range candidates {
			if c.BeadID == e.BeadID {
				continue
			}
			blocks, err := d.ledger.BlocksEdge(ctx, e.BeadID, c.BeadID)
			if err != nil {
				return DrainResult{}, fmt.Errorf("BlocksEdge(%s,%s): %w", e.BeadID, c.BeadID, err)
			}
			if blocks {
				res := DrainResult{State: DrainStateDrained}
				res.flagWork(fmt.Sprintf("open epic %s blocks otherwise-ready child %s", e.BeadID, c.BeadID))
				return res, nil
			}
		}
	}
	return DrainResult{State: DrainStateDrained}, nil
}
