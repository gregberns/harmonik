package daemon

// decisionsprojection.go — the open-decision projection (hitl-decisions
// component K3, bead hk-qed).
//
// CANONICAL HOME MOVED to internal/presence (hitl-decisions K5 lift, bead
// hk-061). The pure-fold open-decision projection used to be implemented here in
// package daemon. It was lifted into the leaf package internal/presence so the
// session-keeper orphan reaper (component K5) — which MUST NOT import
// internal/daemon (depguard keeper rule) — can read the SAME open set the daemon
// CLI handlers read. The symbols below are thin aliases keeping every daemon-side
// consumer (the K4 list/answer handlers in decisionshandler_k4_kba.go, the K2
// emit handlers, the K3 S5 scenario test) compiling unchanged; the projection
// logic is identical (single source of truth in internal/presence).
//
// The open-decision set is a PURE FOLD over events.jsonl, computed on demand
// (no persistent aggregator — SPEC §3 / C3). It mirrors presence.ComputeRegistry:
// a single forward eventbus.ScanAfter scan that folds the three hitl-decisions
// events into a map keyed by decision_id:
//
//   - decision_needed    → ADD a Decision keyed by that event's OWN event_id
//                          (the decision_id IS the decision_needed event_id —
//                          SPEC §1).
//   - decision_resolved  → REMOVE the Decision keyed by payload.decision_id.
//   - decision_withdrawn → REMOVE the Decision keyed by payload.decision_id.
//
// Open set = needed − (resolved ∪ withdrawn). Dedupe on event_id (SPEC §6 N2).
// PURE: no socket dial, no daemon op, no side effect; restart-survivable for free
// (SPEC §3, S5). The SHARED source of truth K2/K4/K5/K6 all read (SPEC §3 / D2).
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §3, §1, §6 N2.
// Bead refs: hk-qed (component K3), hk-061 (component K5 lift to internal/presence).

import (
	"github.com/gregberns/harmonik/internal/presence"
)

// Decision aliases presence.Decision — one entry in the open-decision projection
// (the value decisions list / show, the kerf cross-works view, and the orphan
// reaper render). It carries question · options · blocked_agent · context_link ·
// decision_id (SPEC §2). DecisionID is the decision_needed event's own bus-minted
// event_id (the projection key). Keep stable — K2/K4/K5/K6 consume it.
type Decision = presence.Decision

// decisionsProjection folds the events.jsonl log at eventsPath into the current
// OPEN-decision set, keyed by decision_id, by delegating to the canonical
// presence.OpenDecisions. See the file header / presence/decisions.go for the
// full fold semantics (ADD on decision_needed keyed by event_id; REMOVE on the
// terminals keyed by payload.decision_id; dedupe on event_id, N2).
//
// PURE: no socket, no daemon op, no side effect. A missing/empty file yields an
// empty (non-nil) map. Restart-survivable for free (SPEC §3 / S5).
func decisionsProjection(eventsPath string) map[string]Decision {
	return presence.OpenDecisions(eventsPath)
}
