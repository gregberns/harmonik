package daemon

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

const beadTypeEpic = "epic"

func (d *DrainDetector) gatherEpicFacts(
	ctx context.Context,
	open []core.BeadRecord,
	blocked []core.BeadRecord,
) (blockedEdges []EpicBlockEdge, needsDecomp []core.BeadID, err error) {
	var epics []core.BeadRecord
	for _, b := range open {
		if b.BeadType == beadTypeEpic {
			epics = append(epics, b)
		}
	}
	if len(epics) == 0 {
		return nil, nil, nil
	}

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
			}
		}
		if !epicHasChild {
			needsDecomp = append(needsDecomp, e.BeadID)
		}
	}
	return blockedEdges, needsDecomp, nil
}
