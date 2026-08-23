package sentinel

import (
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// LayerAConfig holds the three per-run thresholds used by DetectLayerA.
//
// All fields are REQUIRED with no compiled default — callers must supply
// positive durations. DetectLayerA returns an ErrLayerAConfigInvalid error
// when any field is zero, so missing config fails loud rather than silently
// using a zero threshold that would fire on every run.
//
// Config source (when daemon-integrated): the sentinel: block in
// .harmonik/config.yaml, resolved via the digest package. Field keys in YAML:
//
//	run_silence_stall     — heartbeat-gap threshold (e.g. "22m")
//	review_finalize_stall — review-stall threshold (e.g. "10m")
//	run_max_age           — run-age backstop (e.g. "60m")
type LayerAConfig struct {
	// RunSilenceStall is the maximum duration a non-terminal run may go without
	// emitting any agent_heartbeat or agent_message event. Silence beyond this
	// threshold fires the heartbeat_gap signature.
	// REQUIRED — zero value causes DetectLayerA to return ErrLayerAConfigInvalid.
	RunSilenceStall time.Duration

	// ReviewFinalizeStall is the maximum duration allowed between a
	// reviewer_verdict event and the subsequent run_completed / run_failed
	// terminal event. Exceeding this threshold fires the review_stall signature.
	// REQUIRED — zero value causes DetectLayerA to return ErrLayerAConfigInvalid.
	ReviewFinalizeStall time.Duration

	// RunMaxAge is the absolute upper bound on how long a non-terminal run may
	// remain active. Any run older than this fires the run_age signature.
	// REQUIRED — zero value causes DetectLayerA to return ErrLayerAConfigInvalid.
	RunMaxAge time.Duration
}

// ErrLayerAConfigInvalid is returned by DetectLayerA when cfg contains one or
// more zero-value required thresholds.
type ErrLayerAConfigInvalid struct {
	// Field is the name of the zero-value field.
	Field string
}

func (e *ErrLayerAConfigInvalid) Error() string {
	return fmt.Sprintf(
		"sentinel: Layer A config: required field %q is zero; set it in .harmonik/config.yaml "+
			"(sentinel block, e.g. `run_silence_stall: 22m`) — there is no compiled default",
		e.Field,
	)
}

func validateLayerAConfig(cfg LayerAConfig) error {
	if cfg.RunSilenceStall <= 0 {
		return &ErrLayerAConfigInvalid{Field: "RunSilenceStall"}
	}
	if cfg.ReviewFinalizeStall <= 0 {
		return &ErrLayerAConfigInvalid{Field: "ReviewFinalizeStall"}
	}
	if cfg.RunMaxAge <= 0 {
		return &ErrLayerAConfigInvalid{Field: "RunMaxAge"}
	}
	return nil
}

// StallHit records one Layer A detection result.
//
// Callers convert a StallHit to a core.StallDetectedPayload for event emission.
// The separate type keeps the sentinel package free of event-bus dependencies
// for unit testing.
type StallHit struct {
	// RunID is the stalled run.
	RunID string
	// BeadID is the bead being executed.
	BeadID string
	// LaneName is the queue/lane the run belongs to.
	LaneName string
	// Signature is the stall class that fired.
	Signature core.StallSignature
	// Elapsed is the duration since the stall condition began:
	//   heartbeat_gap: since RunSignal.LastEventAt
	//   review_stall:  since RunSignal.VerdictAt
	//   run_age:       since RunSignal.StartedAt
	Elapsed time.Duration
}

// StallDetectedPayload converts h to the core.StallDetectedPayload shape for
// event emission. ElapsedMs is truncated to milliseconds (minimum 1).
func (h StallHit) StallDetectedPayload() core.StallDetectedPayload {
	ms := h.Elapsed.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return core.StallDetectedPayload{
		RunID:     h.RunID,
		BeadID:    h.BeadID,
		Signature: h.Signature,
		ElapsedMs: ms,
	}
}

// DetectLayerA inspects snap for per-run stall conditions and returns one
// StallHit per (run, signature) pair that exceeds its threshold.
//
// Only non-terminal runs are inspected (RunPhaseTerminal is skipped).
//
// The three signatures are checked independently:
//
//  1. heartbeat_gap: LastEventAge > cfg.RunSilenceStall
//  2. review_stall:  Phase == RunPhaseVerdictFired &&
//     snap.Now.Sub(VerdictAt) > cfg.ReviewFinalizeStall
//  3. run_age:       snap.Now.Sub(StartedAt) > cfg.RunMaxAge
//
// A run may produce more than one hit in a single pass (e.g., both heartbeat_gap
// and run_age). Callers should de-duplicate across repeated passes using the
// (RunID, Signature) pair as the key.
//
// Returns (nil, *ErrLayerAConfigInvalid) immediately if cfg has a zero-value
// required field.
func DetectLayerA(snap Snapshot, cfg LayerAConfig) ([]StallHit, error) {
	if err := validateLayerAConfig(cfg); err != nil {
		return nil, err
	}

	var hits []StallHit
	for _, rs := range snap.Runs {
		if rs.Phase == RunPhaseTerminal {
			continue
		}

		if rs.LastEventAge > cfg.RunSilenceStall {
			hits = append(hits, StallHit{
				RunID:     rs.RunID,
				BeadID:    rs.BeadID,
				LaneName:  rs.LaneName,
				Signature: core.StallSignatureHeartbeatGap,
				Elapsed:   rs.LastEventAge,
			})
		}

		if rs.Phase == RunPhaseVerdictFired && !rs.VerdictAt.IsZero() {
			sinceVerdict := snap.Now.Sub(rs.VerdictAt)
			if sinceVerdict > cfg.ReviewFinalizeStall {
				hits = append(hits, StallHit{
					RunID:     rs.RunID,
					BeadID:    rs.BeadID,
					LaneName:  rs.LaneName,
					Signature: core.StallSignatureReviewStall,
					Elapsed:   sinceVerdict,
				})
			}
		}

		sinceStart := snap.Now.Sub(rs.StartedAt)
		if sinceStart > cfg.RunMaxAge {
			hits = append(hits, StallHit{
				RunID:     rs.RunID,
				BeadID:    rs.BeadID,
				LaneName:  rs.LaneName,
				Signature: core.StallSignatureRunAge,
				Elapsed:   sinceStart,
			})
		}
	}

	return hits, nil
}
