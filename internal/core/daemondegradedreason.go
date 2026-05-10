package core

// DaemonDegradedReason is the exhaustive enum for the `reason` field of the
// daemon_degraded event (event-model.md §8.7.5; §6.3 daemon_degraded block).
//
// Per event-model.md §8.7.5: this enum is exhaustive. New variants require
// an EV-027 amendment.
//
// Spec ref: event-model.md §8.7.5, §6.3.
// Bead ref: hk-hqwn.71.
type DaemonDegradedReason string

const (
	// DaemonDegradedReasonRTOBreach is emitted when the RTO measurement exceeds
	// the configured threshold per operator-nfr.md §4.8 ON-033.
	DaemonDegradedReasonRTOBreach DaemonDegradedReason = "rto_breach"

	// DaemonDegradedReasonReconstructionNotify is emitted when the reconciliation
	// subsystem notifies the daemon that reconstruction is required.
	DaemonDegradedReasonReconstructionNotify DaemonDegradedReason = "reconstruction_notify"

	// DaemonDegradedReasonClockRegression is emitted when the wall clock regresses
	// behind the event-ID high-water-mark (HWM) by more than 1 second per EV-002c.
	DaemonDegradedReasonClockRegression DaemonDegradedReason = "clock_regression"

	// DaemonDegradedReasonCat0PostReady is emitted when a Cat 0 prerequisite fails
	// after the daemon has already reached `ready`. Per reconciliation/spec.md §4.2
	// RC-012a: MUST NOT transition the daemon-status enum from `ready` to `degraded`.
	DaemonDegradedReasonCat0PostReady DaemonDegradedReason = "cat0_post_ready"

	// DaemonDegradedReasonInfrastructureUnavailable is emitted when a required
	// infrastructure prerequisite (br, git, filesystem) becomes unavailable.
	DaemonDegradedReasonInfrastructureUnavailable DaemonDegradedReason = "infrastructure_unavailable"

	// DaemonDegradedReasonSilentHangAggregate is the ON-040 silent-hang fan-out
	// aggregator signal per operator-nfr.md §4.9 ON-040.
	DaemonDegradedReasonSilentHangAggregate DaemonDegradedReason = "silent_hang_aggregate"
)

// Valid reports whether r is one of the six declared DaemonDegradedReason constants.
func (r DaemonDegradedReason) Valid() bool {
	switch r {
	case DaemonDegradedReasonRTOBreach,
		DaemonDegradedReasonReconstructionNotify,
		DaemonDegradedReasonClockRegression,
		DaemonDegradedReasonCat0PostReady,
		DaemonDegradedReasonInfrastructureUnavailable,
		DaemonDegradedReasonSilentHangAggregate:
		return true
	default:
		return false
	}
}
