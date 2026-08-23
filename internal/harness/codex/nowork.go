package codex

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

const codexNoWorkDurationFloorDefault = 10 * time.Second

// NoWorkFloor resolves the effective no-work duration floor: the per-deps
// override when set, otherwise the package default. Zero and negative overrides
// both fall back, so a zero-valued deps struct behaves as production does.
func NoWorkFloor(override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	return codexNoWorkDurationFloorDefault
}

// NoWorkSuspected reports whether an implement phase should be flagged as
// a no-work run: the refs-trailer fallback found nothing to commit AND the
// phase finished faster than the floor.
//
// Both conditions are required (see the file header). A non-positive duration
// is treated as unmeasured and never flags — a missing measurement is not
// evidence of anything, and flagging on it would make the detector fire on
// every run whose clock plumbing is absent.
func NoWorkSuspected(outcome shared.RefsOutcome, phaseDuration, floorOverride time.Duration) bool {
	if outcome != shared.RefsNoChange {
		return false
	}
	if phaseDuration <= 0 {
		return false
	}
	return phaseDuration < NoWorkFloor(floorOverride)
}

// EmitImplementerNoWorkSuspected emits the implementer_no_work_suspected event
// (hk-368i4). Non-fatal: marshal/emit errors are discarded, since the run's
// failure is already recorded by the no-commit path and this event is purely a
// diagnostic.
func EmitImplementerNoWorkSuspected(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, beadID core.BeadID, phaseDuration, floor time.Duration) {
	if bus == nil {
		return
	}
	pl := core.ImplementerNoWorkSuspectedPayload{
		RunID:           runID.String(),
		BeadID:          string(beadID),
		DurationSeconds: phaseDuration.Seconds(),
		FloorSeconds:    floor.Seconds(),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		slog.WarnContext(ctx, "implementer_no_work_suspected_marshal_failed",
			"run_id", runID.String(), "bead_id", string(beadID), "error", err.Error())
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerNoWorkSuspected, b); emitErr != nil {
		slog.WarnContext(ctx, "implementer_no_work_suspected_emit_failed",
			"run_id", runID.String(), "bead_id", string(beadID), "error", emitErr.Error())
	}
}
