package dispatch

import (
	"errors"
	"fmt"
)

// ResultClass states what one dispatch transaction attempt proved.
type ResultClass string

const (
	// ResultCommitted means the requested phase became durable.
	ResultCommitted ResultClass = "committed"
	// ResultReplayable means exact prior facts allow the operation to continue.
	ResultReplayable ResultClass = "replayable"
	// ResultRefused means the operation made no admitted durable change.
	ResultRefused ResultClass = "refused"
	// ResultRepairRequired means durable facts conflict or cannot be classified.
	ResultRepairRequired ResultClass = "repair-required"
)

// ResultReason classifies a refusal or conflicting durable state.
type ResultReason string

const (
	// ReasonStateChanged reports a refused stale input.
	ReasonStateChanged ResultReason = "state_changed"
	// ReasonExternalRefusal reports that an external authority refused the step.
	ReasonExternalRefusal ResultReason = "external_refusal"
	// ReasonIdentityConflict reports two facts with different identities.
	ReasonIdentityConflict ResultReason = "identity_conflict"
	// ReasonCorruptFact reports a durable fact that cannot be decoded or validated.
	ReasonCorruptFact ResultReason = "corrupt_fact"
	// ReasonAuthorityUnavailable reports that a required authority cannot be read.
	ReasonAuthorityUnavailable ResultReason = "authority_unavailable"
)

// Result is the complete value returned by a dispatch transaction operation.
//
// No production caller returns one yet, so scripts/reachability.baseline holds
// Result.Validate as production-unreachable. The reason is not a producer that
// is still to come. It is that LB-008 in specs/live-bead-state.md is unmet on
// the path that already runs.
//
// dispatchstore.Store.Advance makes the durable phase change that LB-006 gives
// to the dispatch transaction, and daemon.dispatchReplayExecutor reaches it
// from advanceRunPhase and replayClaim. Advance returns a plain error, and two
// of the four classes LB-008 keeps apart do not survive it. A commit and an
// exact replay both come back as nil, so no caller can tell the write it made
// from the write it re-observed. A refused predecessor comes back as
// errors.New("dispatchstore: predecessor bytes changed"), or as
// errors.New("dispatchstore: predecessor changed before replace") on the
// re-read-before-rename path. Neither carries a type to match on, so a caller
// cannot separate a refusal from any other plain failure.
//
// Repair-required is the one class that does survive: the paths that lose
// parent durability return *dispatchstore.AmbiguousError, which is errors.As
// -able and which the dispatchstore tests already discriminate on. So the gap
// is narrower than "the error says nothing", and it is still a gap.
//
// The chain is reachable but unreached at run time. Store.Create has no
// production caller, and scripts/dispatch-activation-gate.sh keeps it that way
// until activation is designed. Store.List therefore answers empty here.
//
// Wiring Store.Advance to this type changes production behavior. It needs its
// own change, and it must not ride along with a reachability triage.
type Result struct {
	Class  ResultClass
	Intent *Intent
	Reason ResultReason
}

// Validate rejects result values that make two claims at once.
func (r Result) Validate() error {
	switch r.Class {
	case ResultCommitted, ResultReplayable:
		if r.Intent == nil {
			return errors.New("dispatch: committed and replayable results require an intent")
		}
		if r.Reason != "" {
			return errors.New("dispatch: committed and replayable results cannot carry a reason")
		}
		return r.Intent.Validate()
	case ResultRefused:
		if r.Intent != nil {
			return errors.New("dispatch: refused result cannot carry an intent")
		}
		if r.Reason != ReasonStateChanged && r.Reason != ReasonExternalRefusal {
			return errors.New("dispatch: refused result requires a refusal reason")
		}
		return nil
	case ResultRepairRequired:
		if r.Intent != nil {
			return errors.New("dispatch: repair-required result cannot carry an intent")
		}
		switch r.Reason {
		case ReasonIdentityConflict, ReasonCorruptFact, ReasonAuthorityUnavailable:
			return nil
		default:
			return errors.New("dispatch: repair-required result requires a repair reason")
		}
	default:
		return fmt.Errorf("dispatch: invalid result class %q", r.Class)
	}
}
