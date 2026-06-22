package daemon

import (
	"context"
	"encoding/json"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// reconcileOrphanedRunsOnResume scans the durable event log for runs that
// emitted run_started but never emitted a terminal event (run_completed or
// run_failed). These runs were active when the daemon was last killed without
// a clean shutdown, so their in-memory RunRegistry state was lost and no
// terminal event was written before exit.
//
// For each orphaned run a run_failed event is emitted so downstream observers
// (e.g. the ops-monitor review-gate) see a terminal event rather than an open
// reviewer_launched/no-verdict state that trips on every restart.
//
// Returns the count of run_failed events emitted. Non-fatal: callers MUST NOT
// abort startup on a non-zero return — it is informational only.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 (hk-r73qr).
func reconcileOrphanedRunsOnResume(ctx context.Context, eventsPath string, bus handlercontract.EventEmitter) int {
	if eventsPath == "" {
		return 0
	}

	type runMeta struct {
		beadID string
	}
	started := make(map[core.RunID]runMeta)
	terminated := make(map[core.RunID]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, core.EventID{}) {
		if ev.RunID == nil {
			continue
		}
		switch core.EventType(ev.Type) {
		case core.EventTypeRunStarted:
			var pl workloopRunStartedPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil || pl.BeadID == "" {
				continue
			}
			started[*ev.RunID] = runMeta{beadID: pl.BeadID}
		case core.EventTypeRunCompleted, core.EventTypeRunFailed:
			terminated[*ev.RunID] = struct{}{}
		}
	}

	count := 0
	for runID, meta := range started {
		if _, done := terminated[runID]; done {
			continue
		}
		emitRunCompleted(ctx, bus, runID, meta.beadID, "", "", false,
			"run orphaned by daemon restart: no terminal event before shutdown", nil, nil, nil)
		count++
	}
	return count
}
