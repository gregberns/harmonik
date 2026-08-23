package presence

import (
	"encoding/json"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// Decision is one entry in the open-decision projection — the value a reader
// (decisions list / show, the kerf cross-works view, the orphan reaper) renders
// for one open decision.
//
// The fields mirror the operator-facing "what-needs-me" row (SPEC §2): every
// open decision renders as question · options · blocked_agent · context_link ·
// decision_id. DecisionID is the decision_needed event's own bus-minted
// event_id (the projection key); the remaining fields are copied verbatim from
// the decision_needed payload (core.DecisionNeededPayload, K1).
//
// Decision is the EXPORTED shape K2/K4/K5/K6 consume — keep it stable. The
// internal/daemon Decision type is a thin alias of this one (decisionsprojection.go).
type Decision struct {
	// DecisionID is the decision_needed event's own event_id (UUIDv7, canonical
	// hyphenated string). It is the projection map key and the value the two
	// terminals carry as payload.decision_id.
	DecisionID string

	// Question is the decision the human must make (decision_needed.question).
	Question string

	// Options is the list of enumerated choices (decision_needed.options, ≥1).
	Options []string

	// BlockedAgent is the name of the emitting agent that is blocked on this
	// decision (decision_needed.blocked_agent). May be empty. Used by the orphan
	// reaper (K5) and keeper seam (K6) to find the agent to liveness-check.
	BlockedAgent string

	// ContextLink is the free-form context pointer for the decision
	// (decision_needed.context_link): a bead id, work codename, thread, or
	// run_id. May be empty.
	ContextLink string

	// ValueRequested is the v1.1 free-text-answer hook (decision_needed.value_requested).
	// v1 ignores it; carried for forward-compatibility so later readers need not
	// re-scan.
	ValueRequested bool

	// Topic is the optional routing tag (decision_needed.topic). The reserved
	// value core.DecisionTopicOperatorMailbox marks an operator-mailbox item
	// (bead hk-pltjs, pending operator sign-off). Empty means untagged.
	Topic string

	// Urgency is the optional operator-mailbox-flavor hint
	// (decision_needed.urgency): blocker | question | fyi (bead hk-pltjs,
	// pending operator sign-off). Empty means unspecified.
	Urgency core.DecisionUrgency
}

// OpenDecisions folds the events.jsonl log at eventsPath into the current
// OPEN-decision set, keyed by decision_id.
//
// The fold is a single forward eventbus.ScanAfter scan (mirroring ComputeRegistry):
//
//   - on decision_needed: ADD a Decision keyed by the event's OWN event_id
//     (the decision_id);
//   - on decision_resolved / decision_withdrawn: REMOVE the Decision keyed by
//     payload.decision_id;
//   - dedupe on event_id (SPEC §6 N2): a re-delivered event_id is folded once.
//
// The result is the open set = needed − (resolved ∪ withdrawn).
//
// OpenDecisions is PURE: it opens and reads eventsPath via ScanAfter (a pure
// read-side function — EV-020) and performs no other I/O, no socket dial, no
// daemon op, and no mutation of the log. A missing or empty file yields an empty
// (non-nil) map. Callers may invoke it with no daemon running (SPEC S6), and it
// returns the same set across daemon restarts because it derives purely from the
// durable log (SPEC §3 / S5).
//
// Spec ref: SPEC.md §3 (open-set projection), §1 (decision_id keying), §6 N2.
// Bead refs: hk-qed (K3), hk-061 (K5 lift).
func OpenDecisions(eventsPath string) map[string]Decision {
	var zeroID core.EventID
	open := make(map[string]Decision)

	seen := make(map[string]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		evID := ev.EventID.String()
		if _, dup := seen[evID]; dup {
			continue
		}

		switch ev.Type {
		case core.EventTypeDecisionNeeded:
			var p core.DecisionNeededPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			seen[evID] = struct{}{}
			open[evID] = Decision{
				DecisionID:     evID,
				Question:       p.Question,
				Options:        p.Options,
				BlockedAgent:   p.BlockedAgent,
				ContextLink:    p.ContextLink,
				ValueRequested: p.ValueRequested,
				Topic:          p.Topic,
				Urgency:        p.Urgency,
			}

		case core.EventTypeDecisionResolved:
			var p core.DecisionResolvedPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)

		case core.EventTypeDecisionWithdrawn:
			var p core.DecisionWithdrawnPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)

		default:
		}
	}

	return open
}
