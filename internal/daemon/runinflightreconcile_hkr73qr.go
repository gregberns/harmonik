package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

type beadStatusReader interface {
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
}

func reconcileOrphanedRunsOnResume(
	ctx context.Context,
	eventsPath string,
	bus handlercontract.EventEmitter,
	resetter runBeadResetter,
	statusLedger beadStatusReader,
	intentLogDir string,
	projectHash core.ProjectHash,
	daemonStartNS int64,
	dispatchedBeads lifecycle.QueueDispatchedSet,
	liveRunBeadIDs map[core.BeadID]struct{},
) int {
	if eventsPath == "" {
		return 0
	}

	type runMeta struct {
		beadID          string
		queueID         *string
		queueGroupIndex *int
	}
	started := make(map[core.RunID]runMeta)
	terminated := make(map[core.RunID]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, core.EventID{}) {
		if ev.RunID == nil {
			continue
		}
		switch ev.Type {
		case core.EventTypeRunStarted:
			pl, err := core.DecodeRunStartedForRead(ev)
			if err != nil || pl.BeadID == "" {
				continue
			}
			started[*ev.RunID] = runMeta{
				beadID:          pl.BeadID,
				queueID:         pl.QueueID,
				queueGroupIndex: pl.QueueGroupIndex,
			}
		case core.EventTypeRunCompleted, core.EventTypeRunFailed:
			terminated[*ev.RunID] = struct{}{}
		default:
		}
	}

	count := 0
	for runID, meta := range started {
		if _, done := terminated[runID]; done {
			continue
		}
		if meta.beadID != "" {
			if _, live := liveRunBeadIDs[core.BeadID(meta.beadID)]; live {
				continue
			}
		}
		emitRunCompleted(ctx, bus, runID, meta.beadID, "", "", false,
			"run orphaned by daemon restart: no terminal event before shutdown",
			meta.queueID, meta.queueGroupIndex, nil)
		count++

		if resetter != nil && meta.beadID != "" && statusLedger != nil {
			resetGuardedBead(ctx, resetter, statusLedger, core.BeadID(meta.beadID),
				intentLogDir, projectHash, daemonStartNS,
				fmt.Sprintf("run %s", runID))
		}
	}

	startedBeadIDs := make(map[core.BeadID]struct{}, len(started))
	terminatedBeadIDs := make(map[core.BeadID]struct{})
	for runID, meta := range started {
		if meta.beadID == "" {
			continue
		}
		if _, done := terminated[runID]; done {
			terminatedBeadIDs[core.BeadID(meta.beadID)] = struct{}{}
			continue
		}
		startedBeadIDs[core.BeadID(meta.beadID)] = struct{}{}
	}

	if len(dispatchedBeads) > 0 && resetter != nil && statusLedger != nil {
		for beadID := range dispatchedBeads {
			if _, seen := startedBeadIDs[beadID]; seen {
				continue
			}
			if _, live := liveRunBeadIDs[beadID]; live {
				continue
			}
			resetGuardedBead(ctx, resetter, statusLedger, beadID,
				intentLogDir, projectHash, daemonStartNS,
				"dispatch-tracker orphan")
		}
	}

	if resetter != nil && statusLedger != nil {
		for beadID := range terminatedBeadIDs {
			if _, seen := dispatchedBeads[beadID]; seen {
				continue
			}
			if _, live := liveRunBeadIDs[beadID]; live {
				continue
			}
			resetGuardedBead(ctx, resetter, statusLedger, beadID,
				intentLogDir, projectHash, daemonStartNS,
				"terminated-but-locked")
		}
	}

	return count
}

func resetGuardedBead(
	ctx context.Context,
	resetter runBeadResetter,
	statusLedger beadStatusReader,
	beadID core.BeadID,
	intentLogDir string,
	projectHash core.ProjectHash,
	daemonStartNS int64,
	logCtx string,
) {
	rec, showErr := statusLedger.ShowBead(ctx, beadID)
	switch {
	case showErr != nil:
		fmt.Fprintf(os.Stderr,
			"daemon: reconcileOrphanedRunsOnResume: ShowBead %s (%s): %v — skipping reset (will not risk reopening a landed bead)\n",
			beadID, logCtx, showErr)
	case rec.Status != core.CoarseStatusInProgress && rec.Status != core.CoarseStatusOpen:
	default:
		if resetErr := resetter.ResetBead(
			ctx,
			intentLogDir,
			brcli.TimeoutConfig{},
			beadID,
			projectHash,
			daemonStartNS,
		); resetErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: reconcileOrphanedRunsOnResume: ResetBead %s (%s): %v — queue item may stay dispatched; will retry next boot\n",
				beadID, logCtx, resetErr)
		}
	}
}
