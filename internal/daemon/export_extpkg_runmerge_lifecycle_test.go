package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/runmerge"
)

// ExportedIsRetryableMergeReason exposes isRetryableMergeReason for unit tests.
//
// Bead ref: hk-f9xzs.
var ExportedIsRetryableMergeReason = runmerge.IsRetryableReason

// ExportedLoadQueueProvenance runs bootState.loadQueueProvenance for projectDir
// and returns the aggregated QueueDispatched / QueueOwned provenance sets. It is
// the test seam for hk-nddg1: loadQueueProvenance must enumerate ALL named
// queues (queue.EnumerateQueueNames), not just main, so a bead dispatched via a
// crew queue (e.g. queues/paul.json) lands in QueueDispatched and the orphan
// sweep's (a-queue) exclusion protects it from a double-dispatch reset.
func ExportedLoadQueueProvenance(ctx context.Context, projectDir string) (lifecycle.QueueDispatchedSet, lifecycle.QueueOwnedSet) {
	bs := &bootState{cfg: Config{ProjectDir: projectDir}}
	st := &reconcileState{}
	bs.loadQueueProvenance(ctx, st)
	return st.queueDispatched, st.queueOwned
}
