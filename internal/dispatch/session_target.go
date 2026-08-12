package dispatch

import "strconv"

// SessionTargetProbeStatus is the closed outcome of one adapter probe.
type SessionTargetProbeStatus string

const (
	// SessionTargetSessionAbsent means the exact session does not exist.
	SessionTargetSessionAbsent SessionTargetProbeStatus = "session_absent"
	// SessionTargetWindowAbsent means the exact window does not exist.
	SessionTargetWindowAbsent SessionTargetProbeStatus = "window_absent"
	// SessionTargetPaneAbsent means the exact pane does not exist.
	SessionTargetPaneAbsent SessionTargetProbeStatus = "pane_absent"
	// SessionTargetExact means all target values were read.
	SessionTargetExact SessionTargetProbeStatus = "exact"
	// SessionTargetDuplicate means more than one target matched.
	SessionTargetDuplicate SessionTargetProbeStatus = "duplicate"
	// SessionTargetSessionUnreadable means the session query failed.
	SessionTargetSessionUnreadable SessionTargetProbeStatus = "session_unreadable"
	// SessionTargetWindowUnreadable means the window query failed.
	SessionTargetWindowUnreadable SessionTargetProbeStatus = "window_unreadable"
	// SessionTargetOptionsUnreadable means the identity options could not be read.
	SessionTargetOptionsUnreadable SessionTargetProbeStatus = "options_unreadable"
	// SessionTargetPaneUnreadable means pane liveness could not be read.
	SessionTargetPaneUnreadable SessionTargetProbeStatus = "pane_unreadable"
)

// SessionTargetObservation is one adapter-owned read of an exact tmux pane.
type SessionTargetObservation struct {
	Status            SessionTargetProbeStatus
	RunID             string
	ClaimTransitionID string
	SessionName       string
	WindowName        string
	PanePID           string
	PaneDead          string
}

// ClassifySessionTarget converts exact pane values into one replay fact.
func ClassifySessionTarget(intent Intent, observation SessionTargetObservation) SessionFact {
	if intent.Validate() != nil {
		return SessionConflict
	}
	switch observation.Status {
	case SessionTargetSessionAbsent, SessionTargetWindowAbsent, SessionTargetPaneAbsent:
		if observation.hasValues() {
			return SessionConflict
		}
		return SessionAbsent
	case SessionTargetExact:
	case SessionTargetDuplicate,
		SessionTargetSessionUnreadable,
		SessionTargetWindowUnreadable,
		SessionTargetOptionsUnreadable,
		SessionTargetPaneUnreadable:
		return SessionConflict
	default:
		return SessionConflict
	}
	if intent.Phase != PhaseHandoffDurable || !observation.matches(intent) {
		return SessionConflict
	}
	pid, err := strconv.Atoi(observation.PanePID)
	if err != nil || pid <= 0 {
		return SessionConflict
	}
	switch observation.PaneDead {
	case "0":
		return SessionLive
	case "1":
		return SessionDead
	default:
		return SessionConflict
	}
}

func (o SessionTargetObservation) hasValues() bool {
	return o.RunID != "" || o.ClaimTransitionID != "" || o.SessionName != "" ||
		o.WindowName != "" || o.PanePID != "" || o.PaneDead != ""
}

func (o SessionTargetObservation) matches(intent Intent) bool {
	return o.RunID == intent.Binding.RunID.String() &&
		o.ClaimTransitionID == intent.Binding.ClaimTransitionID.String() &&
		o.SessionName == intent.Handoff.SessionName && o.WindowName == intent.Handoff.WindowName
}
