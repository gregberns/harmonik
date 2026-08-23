package daemon

import (
	"github.com/gregberns/harmonik/internal/presence"
)

// Decision aliases presence.Decision — one entry in the open-decision projection
// (the value decisions list / show, the kerf cross-works view, and the orphan
// reaper render). It carries question · options · blocked_agent · context_link ·
// decision_id (SPEC §2). DecisionID is the decision_needed event's own bus-minted
// event_id (the projection key). Keep stable — K2/K4/K5/K6 consume it.
type Decision = presence.Decision

func decisionsProjection(eventsPath string) map[string]Decision {
	return presence.OpenDecisions(eventsPath)
}
