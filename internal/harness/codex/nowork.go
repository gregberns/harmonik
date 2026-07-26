package codex

// codexnowork_hk368i4.go — the sub-threshold-duration detector for a codex
// implement phase that reported success without doing any work (hk-368i4).
//
// SPLIT OUT of hk-jcrzn as defence-in-depth. hk-jcrzn fixes the CAUSE: the
// fallback committer read the daemon's own scaffolding as agent work and
// manufactured a passing commit. This file adds the independent DETECTOR, so a
// future silent-implementer failure of a DIFFERENT cause is still caught.
//
// THE ORACLE (measured on lima's isolated scratch project, implementer_phase_
// complete events; cross-checked against india's codex gate runs):
//
//	commit_landed=FALSE runs:  5.19s  4.00s  3.38s  3.27s
//	commit_landed=TRUE  runs: 28.9s  31.9s  39.3s  55.3s  44.1s  53.5s
//
// No overlap between the populations in any sample taken so far. A codex
// implement phase that returns in a few seconds has not done work, whatever
// the commit says.
//
// WHY THE CONJUNCTION, AND WHY IT IS SAFE TO BE LOUD. Duration alone is not a
// safe signal — a legitimately trivial bead (a one-line doc change) could in
// principle complete fast, and failing such a run outright would be wrong.
// Pairing sub-floor duration with shared.RefsNoChange is what makes the detector
// safe: a real fast run still produces a commit, so it never reaches the
// no-change outcome. The two together are a shape no observed real run has
// produced.
//
// THE DETECTOR DOES NOT DECIDE ANYTHING. shared.RefsNoChange already routes to
// the standard no_commit failure path — the run is failing with or without
// this event. What was missing was any record of WHY, which is precisely how
// hk-jcrzn stayed invisible at every layer that was checked.
//
// Bead ref: hk-368i4. Related: hk-jcrzn (the cause), hk-yhvrh (same family).

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

// codexNoWorkDurationFloorDefault is the default implement-phase duration below
// which a shared.RefsNoChange outcome is treated as corroborated evidence that the
// phase did no work.
//
// 10s sits in the empty band between the two measured populations (worst
// no-work run 5.19s, fastest real run 28.9s). The sample is small and all from
// one box, so this is a STARTING POINT and not a calibrated constant — but the
// gap it sits in is roughly 5x wide, so the exact value is not delicate. Tune
// it via workLoopDeps.codexNoWorkDurationFloor rather than editing this.
//
// Bead ref: hk-368i4.
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
		// Best-effort signal, but a dropped one means the "codex exited without
		// doing work" suspicion never reaches the operator at all.
		slog.WarnContext(ctx, "implementer_no_work_suspected_emit_failed",
			"run_id", runID.String(), "bead_id", string(beadID), "error", emitErr.Error())
	}
}
