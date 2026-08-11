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
