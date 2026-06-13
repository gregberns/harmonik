package daemon

// decisionsprojection.go — the open-decision projection (hitl-decisions
// component K3, bead hk-qed).
//
// The open-decision set is a PURE FOLD over events.jsonl, computed on demand
// (no persistent aggregator — SPEC §3 / C3). It mirrors ComputePresenceRegistry
// (cmd/harmonik/comms.go): a single forward eventbus.ScanAfter scan that folds
// the three hitl-decisions events into a map keyed by decision_id:
//
//   - decision_needed    → ADD a Decision keyed by that event's OWN event_id
//                          (the decision_id IS the decision_needed event_id —
//                          SPEC §1).
//   - decision_resolved  → REMOVE the Decision keyed by payload.decision_id.
//   - decision_withdrawn → REMOVE the Decision keyed by payload.decision_id.
//
// Open set = needed − (resolved ∪ withdrawn). The key asymmetry is load-bearing
// and correct: ADD keys on the event's own event_id; REMOVE keys on the
// terminal's payload.decision_id (which equals the original decision_needed
// event_id — SPEC §1, mirrors agent_message.in_reply_to).
//
// Dedupe (SPEC §6 N2): a re-delivered event_id is a no-op. Because delivery is
// at-least-once, the same decision_needed / decision_resolved / decision_withdrawn
// event_id can appear more than once in the log; folding it twice MUST be
// idempotent. ADD is naturally idempotent (re-adding the same decision_id
// overwrites with identical content). REMOVE is naturally idempotent (deleting
// an absent or already-removed key is a no-op). For belt-and-suspenders and to
// make the N2 guarantee explicit, we additionally track seen event_ids and skip
// any event whose own event_id was already folded.
//
// PURITY (de-risks SPEC S6 + makes S5 restart-survivability free): this function
// performs NO socket dial, NO daemon op, NO side effect. It is callable against
// any events.jsonl path with no daemon running — the daemon replays the same log
// on boot, so the open set is identical across restarts (SPEC §3, S5). It is the
// SHARED source of truth that K2 (raise/wait), K4 (operator list/answer), K5
// (orphan reaper), and K6 (keeper seam) all read (SPEC §3 / D2).
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §3, §1, §6 N2.
// Bead ref: hk-qed (component K3).

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
// Decision is the EXPORTED shape K2/K4/K5/K6 consume — keep it stable.
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
}

// decisionsProjection folds the events.jsonl log at eventsPath into the current
// OPEN-decision set, keyed by decision_id.
//
// The fold is a single forward eventbus.ScanAfter scan (mirroring
// ComputePresenceRegistry):
//
//   - on decision_needed: ADD a Decision keyed by the event's OWN event_id
//     (the decision_id);
//   - on decision_resolved / decision_withdrawn: REMOVE the Decision keyed by
//     payload.decision_id;
//   - dedupe on event_id (SPEC §6 N2): a re-delivered event_id is folded once.
//
// The result is the open set = needed − (resolved ∪ withdrawn).
//
// decisionsProjection is PURE: it opens and reads eventsPath via ScanAfter (a
// pure read-side function — EV-020) and performs no other I/O, no socket dial,
// no daemon op, and no mutation of the log. A missing or empty file yields an
// empty (non-nil) map. Callers may invoke it with no daemon running (SPEC S6),
// and it returns the same set across daemon restarts because it derives purely
// from the durable log (SPEC §3 / S5).
//
// Spec ref: SPEC.md §3 (open-set projection), §1 (decision_id keying), §6 N2.
// Bead ref: hk-qed (K3).
func decisionsProjection(eventsPath string) map[string]Decision {
	var zeroID core.EventID
	open := make(map[string]Decision)

	// seen guards N2 dedupe: at-least-once delivery can re-write the same
	// event_id into the log; fold each event_id at most once. (ADD/REMOVE are
	// each independently idempotent, so this is belt-and-suspenders, but it
	// makes the N2 contract explicit and immune to any non-idempotent change.)
	seen := make(map[string]struct{})

	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		// Dedupe on the event's own event_id (N2). An event with an empty/zero
		// event_id (malformed) is skipped — ScanAfter already drops unparseable
		// lines, but a zero EventID would also be unkeyable here.
		evID := ev.EventID.String()
		if _, dup := seen[evID]; dup {
			continue
		}

		switch ev.Type {
		case string(core.EventTypeDecisionNeeded):
			var p core.DecisionNeededPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			// ADD keyed by the decision_needed event's OWN event_id — that IS
			// the decision_id (SPEC §1). This asymmetry vs. the terminals
			// (which key on payload.decision_id) is correct and load-bearing.
			seen[evID] = struct{}{}
			open[evID] = Decision{
				DecisionID:     evID,
				Question:       p.Question,
				Options:        p.Options,
				BlockedAgent:   p.BlockedAgent,
				ContextLink:    p.ContextLink,
				ValueRequested: p.ValueRequested,
			}

		case string(core.EventTypeDecisionResolved):
			var p core.DecisionResolvedPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			// REMOVE keyed by payload.decision_id (the original decision_needed
			// event_id). delete is a no-op when the key is absent (unknown or
			// already-terminal decision — N3 idempotency at the projection level).
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)

		case string(core.EventTypeDecisionWithdrawn):
			var p core.DecisionWithdrawnPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)
		}
	}

	return open
}
