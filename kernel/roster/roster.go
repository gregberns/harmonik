// Package roster is the peer-liveness state machine, expressed as pure
// functions of THIS box's own probe history. It holds no probe loop, opens no
// socket, and reads no clock: every count and every instant arrives as an
// argument, the same injected-time discipline the kernel holds elsewhere
// (internal/runloop's ports precedent). A later slice adds the probe loop and
// the network leg; this package is only the verdict math those will call.
//
// The verdict is LOCAL by contract. Liveness is computed from this box's own
// observations and is never gossiped — peers do not exchange opinions about a
// third party's clock (see the Liveness message comment in the kernel proto).
// "Left" is deliberately not a state: a graceful departure is not mechanically
// distinguishable from a death, so an announced intent is a fact we were TOLD
// (it sets the reason) laid over a state we INFERRED from probes.
package roster

import (
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// SuspectAfter and DeadAfter are the consecutive-probe-failure counts that move
// a peer between liveness states, exactly per the contract enum comments: three
// failures in a row is SUSPECT, six is DEAD.
const (
	SuspectAfter = 3
	DeadAfter    = 6
)

// Observation is this box's own accumulated view of ONE peer. It is the whole
// input to a verdict — nothing here is read from a clock or a socket at verdict
// time:
//
//   - ConsecutiveFailures counts probes that failed in a row since the last
//     success. Record keeps it; Evaluate reads it.
//   - Probed is false until the first probe result lands. A never-probed peer is
//     UNKNOWN, which is neither ALIVE nor DEAD.
//   - LastSeen is the instant of the most recent SUCCESSFUL probe, supplied by
//     the caller from its own clock. The zero value means never seen.
//   - Intent is what the peer last ANNOUNCED about its own departure. An
//     announced departure sets the verdict's reason and overrides the
//     probe-derived state (see Evaluate).
type Observation struct {
	ConsecutiveFailures int
	Probed              bool
	LastSeen            time.Time
	Intent              kernelv1.Node_Intent
}

// Verdict is the computed liveness of one peer: its state, the reason behind
// that state, and the last-success instant carried straight through from the
// Observation so the caller can stamp it onto the wire type without this package
// naming a timestamp format or reading a clock.
type Verdict struct {
	State    kernelv1.Liveness_State
	Reason   kernelv1.Liveness_Reason
	LastSeen time.Time
}

// Record folds ONE probe result into an Observation and returns the updated
// value. A success zeroes the consecutive-failure count and stamps LastSeen with
// the supplied instant; a failure increments the count and leaves LastSeen
// alone. Either result marks the peer Probed. The instant arrives as an
// argument — this function reads no clock — which is what keeps the flap
// transition (fail, fail, ok -> count back to zero) a pure, testable fold.
func Record(obs Observation, ok bool, now time.Time) Observation {
	obs.Probed = true
	if ok {
		obs.ConsecutiveFailures = 0
		obs.LastSeen = now
		return obs
	}
	obs.ConsecutiveFailures++
	return obs
}

// Evaluate computes a peer's liveness Verdict from an Observation. It is pure:
// the only inputs are the fields of obs.
//
// An announced departure wins first. A peer that told us it is going to sleep or
// to drain is treated as DEAD with the matching announced reason, whatever its
// probe count — the announcement does not wait to "consume" six failures. This
// is the TOLD-intent-over-INFERRED-state rule the package doc names.
//
// Absent an announced departure the state is a pure function of the
// consecutive-failure count: never probed is UNKNOWN; at or past DeadAfter is
// DEAD; at or past SuspectAfter is SUSPECT; below that is ALIVE. Any non-ALIVE
// probe verdict carries REASON_PROBE_TIMEOUT ("it just stopped answering");
// ALIVE and UNKNOWN carry no reason.
func Evaluate(obs Observation) Verdict {
	v := Verdict{LastSeen: obs.LastSeen}

	switch obs.Intent {
	case kernelv1.Node_INTENT_SLEEPING:
		v.State = kernelv1.Liveness_STATE_DEAD
		v.Reason = kernelv1.Liveness_REASON_ANNOUNCED_SLEEP
		return v
	case kernelv1.Node_INTENT_DRAINING:
		v.State = kernelv1.Liveness_STATE_DEAD
		v.Reason = kernelv1.Liveness_REASON_ANNOUNCED_DRAIN
		return v
	case kernelv1.Node_INTENT_UNSPECIFIED, kernelv1.Node_INTENT_UP:
		// Not a departure: the state is probe-derived below.
	}

	switch {
	case !obs.Probed:
		v.State = kernelv1.Liveness_STATE_UNKNOWN
	case obs.ConsecutiveFailures >= DeadAfter:
		v.State = kernelv1.Liveness_STATE_DEAD
		v.Reason = kernelv1.Liveness_REASON_PROBE_TIMEOUT
	case obs.ConsecutiveFailures >= SuspectAfter:
		v.State = kernelv1.Liveness_STATE_SUSPECT
		v.Reason = kernelv1.Liveness_REASON_PROBE_TIMEOUT
	default:
		v.State = kernelv1.Liveness_STATE_ALIVE
	}
	return v
}
