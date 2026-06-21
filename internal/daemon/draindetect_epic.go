package daemon

// draindetect_epic.go — the ledger/epic axes of the drain-fact oracle
// (hk-pfr4 / hk-95uf defense #2 epic-portion; codename:fleet-state P1-a).
//
// gatherEpicFacts computes two axes in one pass over open epics:
//
//   - BlockedByOpenEpic: ALL open-epic → otherwise-ready-child edges (defense
//     #2 epic-portion). The child reports "blocked" while the epic is open, so
//     `br ready` cannot see it — genuine pending work. ALL edges are emitted
//     (no short-circuit on the first match — the captain needs the full picture).
//
//   - NeedsDecomposition: childless OPEN epics — epics for which no child edge
//     exists in the ledger. These are the ONE generative category: they are
//     flagged for the captain to act on (decompose the epic into tasks), not
//     scored or auto-decided.
//
// Design notes:
//   - Takes already-fetched open/blocked bead lists (from GatherDrainFacts) to
//     avoid a second br round-trip for the same data.
//   - Fail-closed: a nil lister/ledger seam, a bead-list error, or a
//     BlocksEdge error yields an error that GatherDrainFacts maps to Unsure.
//     DRAINED is never asserted from this function — it is a fact reporter.
//   - The old scanOpenEpics short-circuited on the first blocked edge
//     (draindetect_epic.go:92-96 in the prior shape). That short-circuit is
//     REMOVED so the bundle is complete: 5 in-progress + 3 blocked edges, not
//     "first edge found, stop reading."
//   - MUST NOT consult `kerf next` (defense #5).

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

// gatherEpicFacts evaluates the epic axes from the pre-fetched open and
// blocked bead lists.
//
// It returns:
//   - blocked: all EpicBlockEdge pairs where an open epic blocks a
//     non-terminal candidate child (defense #2 epic-portion).
//   - needsDecomp: IDs of open epics that have NO child edges in the ledger
//     (the generative axis — the captain must decompose them).
//   - err: non-nil if a BlocksEdge call failed (caller marks Unsure).
//
// The caller (GatherDrainFacts) is responsible for the nil-seam guard: this
// function is only called when d.lister != nil && d.ledger != nil.
func (d *DrainDetector) gatherEpicFacts(
	ctx context.Context,
	open []core.BeadRecord,
	blocked []core.BeadRecord,
) (blockedEdges []EpicBlockEdge, needsDecomp []core.BeadID, err error) {
	// Extract open epics from the open list.
	var epics []core.BeadRecord
	for _, b := range open {
		if b.BeadType == beadTypeEpic {
			epics = append(epics, b)
		}
	}
	if len(epics) == 0 {
		// No open epics — both axes are positively empty.
		return nil, nil, nil
	}

	// Candidate children = open ∪ blocked. We check all non-self candidates
	// for each epic. The open list includes the epics themselves, so we skip
	// same-ID pairs below.
	candidates := make([]core.BeadRecord, 0, len(open)+len(blocked))
	candidates = append(candidates, open...)
	candidates = append(candidates, blocked...)

	for _, e := range epics {
		epicHasChild := false
		for _, c := range candidates {
			if c.BeadID == e.BeadID {
				continue
			}
			blocks, blockErr := d.ledger.BlocksEdge(ctx, e.BeadID, c.BeadID)
			if blockErr != nil {
				return nil, nil, fmt.Errorf("BlocksEdge(%s,%s): %w", e.BeadID, c.BeadID, blockErr)
			}
			if blocks {
				epicHasChild = true
				blockedEdges = append(blockedEdges, EpicBlockEdge{
					EpicID:  e.BeadID,
					ChildID: c.BeadID,
				})
				// No break — emit ALL edges (no short-circuit).
			}
		}
		if !epicHasChild {
			// This open epic has no children in the ledger: the captain must
			// decompose it to generate actionable tasks.
			needsDecomp = append(needsDecomp, e.BeadID)
		}
	}
	return blockedEdges, needsDecomp, nil
}
