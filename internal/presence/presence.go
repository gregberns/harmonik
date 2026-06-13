// Package presence is a leaf package holding the agent-presence projection and
// the hitl-decisions open-decision projection + orphan reaper.
//
// # Why this package exists (the hitl-decisions K5 layering decision)
//
// The hitl-decisions orphan reaper (component K5, bead hk-061) must run on the
// session-keeper watch tick (NOT the daemon's 1-hour reconciliation sweep — the
// orphan-latency bound is ≤ Offline-cutoff + one tick, SPEC §5 / N9). The
// keeper-tick reaper needs THREE things that historically lived in three
// different layers:
//
//  1. the open-decision set (the K3 projection, which lived in internal/daemon);
//  2. the Offline presence predicate (ComputePresenceRegistry / GetPresenceState
//     / the 10-min stale cutoff, which lived in cmd/harmonik — package main);
//  3. the event bus (to emit decision_withdrawn).
//
// internal/keeper's depguard rule (.golangci.yml) allows it to import ONLY
// $gostd + internal/core + internal/eventbus + internal/keeper — it MUST NOT
// import internal/daemon, and it could not import the package-main presence code
// at all. So neither the projection (in daemon) nor the presence predicate (in
// main) was reachable from the keeper tick.
//
// This package resolves that: it is a LEAF that depends only on internal/core +
// internal/eventbus (both already in keeper's allow-list — internal/presence is
// added alongside them). It now owns the canonical presence projection (lifted
// out of cmd/harmonik/comms.go) AND the open-decision projection (lifted out of
// internal/daemon), so the keeper tick can import this one package and call the
// reaper. cmd/harmonik and internal/daemon delegate to these canonical
// definitions (thin aliases) so there is a single source of truth and no
// divergence — the `harmonik comms who` presence behaviour is unchanged.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §5, §6 N9.
// Bead ref: hk-061 (component K5).
package presence

import (
	"encoding/json"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TTL is the online window for the presence projection (agent-comms spec §4 /
// Q2 — APPROVED → C = 60s refresh cadence, TTL = ~2× = 120s).
//
// Lifted verbatim from cmd/harmonik/comms.go (was the unexported presenceTTL).
const TTL = 120 * time.Second

// StaleCutoff is the outer window beyond which a stale agent is considered fully
// offline. online=[0,TTL), stale=[TTL,StaleCutoff), offline=[StaleCutoff,∞) —
// unless an explicit leave beat fires first.
//
// Lifted verbatim from cmd/harmonik/comms.go (was the unexported
// presenceStaleCutoff). This 10-minute cutoff is the K5 "truly gone" boundary
// (SPEC §5 / N9): a blocked agent past it is Offline → reapable; a merely-Stale
// agent (TTL ≤ age < StaleCutoff) is presumed still-blocked and NOT reaped.
const StaleCutoff = 10 * time.Minute

// Record is one entry in the presence registry projection. Used by comms
// join/leave and consumed by comms who and the hitl-decisions orphan reaper.
//
// Lifted from cmd/harmonik/comms.go (was PresenceRecord, package main).
type Record struct {
	Agent    string
	Status   string    // "online" or "offline" — from the latest agent_presence beat
	LastSeen time.Time // wall time from the latest agent_presence beat
	// EffectiveLastSeen is max(LastSeen, latest agent_message send timestamp for this agent).
	// Incorporates activity-derived liveness so an agent sending messages stays online
	// even when its explicit presence beat is stale (hk-6vwi3 fix #1).
	EffectiveLastSeen time.Time
	// SessionID is the session_id field from the most recent agent_presence beat
	// that carried a non-empty session_id. Used for two-captains conflict detection
	// (hk-z0f02): if a new session claims this name but reports a different session_id,
	// the CLI warns before sending.
	SessionID string
}

// State is the computed liveness state for a Record.
//
// Lifted from cmd/harmonik/comms.go (was PresenceState, package main).
type State int

const (
	// StateOnline: effective_last_seen < TTL (120s).
	StateOnline State = iota
	// StateStale: TTL ≤ effective_last_seen < StaleCutoff (10m).
	StateStale
	// StateOffline: explicit leave beat OR effective_last_seen ≥ StaleCutoff.
	StateOffline
)

// GetState returns the computed State for r.
//
// A clean leave beat (Status=="offline") short-circuits to StateOffline
// immediately, bypassing the TTL check — so a departing agent never reads as
// stale.
//
// Lifted from cmd/harmonik/comms.go (was GetPresenceState, package main).
func GetState(r Record) State {
	if r.Status == "offline" {
		return StateOffline
	}
	age := time.Since(r.EffectiveLastSeen)
	if age < TTL {
		return StateOnline
	}
	if age < StaleCutoff {
		return StateStale
	}
	return StateOffline
}

// IsOnline reports whether r represents an agent that is currently online
// (effective_last_seen < TTL and no leave beat).
func IsOnline(r Record) bool {
	return GetState(r) == StateOnline
}

// IsStale reports whether r represents an agent in the stale window
// (TTL ≤ effective_last_seen < StaleCutoff, no leave beat).
func IsStale(r Record) bool {
	return GetState(r) == StateStale
}

// IsOffline reports whether r represents an agent that is fully offline (an
// explicit leave beat OR effective_last_seen ≥ StaleCutoff). This is the K5
// "truly gone" predicate's (b) clause — see ReapOrphanedDecisions.
func IsOffline(r Record) bool {
	return GetState(r) == StateOffline
}

// ComputeRegistry returns the current presence projection over events.jsonl.
//
// The projection folds two event types in a single forward scan (hk-6vwi3 fix #1):
//   - agent_presence: latest beat per agent determines Status and LastSeen.
//   - agent_message:  latest send timestamp per agent (from==agent) extends
//     EffectiveLastSeen so active agents remain visible even without a recent beat.
//
// EffectiveLastSeen = max(LastSeen, latest agent_message.from timestamp).
// A missing or empty file returns an empty map.
//
// Lifted verbatim (logic unchanged) from cmd/harmonik/comms.go
// (was ComputePresenceRegistry, package main).
//
// Spec ref: agent-comms spec §4 (presence registry projection).
// Bead ref: hk-7t27s (T10); consumed by T11 (comms who); hk-6vwi3 (fix #1);
// hk-061 (consumed by the hitl-decisions K5 orphan reaper).
func ComputeRegistry(eventsPath string) map[string]Record {
	var zeroID core.EventID
	byAgent := make(map[string]Record)
	// lastActivity tracks the most recent agent_message.from timestamp per agent.
	lastActivity := make(map[string]time.Time)

	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		switch ev.Type {
		case "agent_presence":
			var p core.AgentPresencePayload
			if decErr := json.Unmarshal(ev.Payload, &p); decErr != nil {
				continue
			}
			if p.Agent == "" {
				continue
			}
			lastSeen, parseErr := time.Parse(time.RFC3339, p.LastSeen)
			if parseErr != nil {
				continue
			}
			// Always overwrite: later entries in the file are more recent (UUIDv7 ordering).
			// Carry forward SessionID from the previous record when the new beat omits it
			// (e.g. recv-refresh beats never carry a session_id) so we don't lose the
			// session binding established by the last explicit join/send beat.
			prev := byAgent[p.Agent]
			sessionID := p.SessionID
			if sessionID == "" {
				sessionID = prev.SessionID
			}
			byAgent[p.Agent] = Record{
				Agent:     p.Agent,
				Status:    string(p.Status),
				LastSeen:  lastSeen,
				SessionID: sessionID,
				// EffectiveLastSeen filled in post-scan pass below.
			}
		case "agent_message":
			var p core.AgentMessagePayload
			if decErr := json.Unmarshal(ev.Payload, &p); decErr != nil {
				continue
			}
			if p.From == "" {
				continue
			}
			if ev.TimestampWall.After(lastActivity[p.From]) {
				lastActivity[p.From] = ev.TimestampWall
			}
		}
	}

	// Post-scan: compute EffectiveLastSeen for agents with an explicit presence beat.
	for agent, rec := range byAgent {
		effective := rec.LastSeen
		if act := lastActivity[agent]; act.After(effective) {
			effective = act
		}
		rec.EffectiveLastSeen = effective
		byAgent[agent] = rec
	}

	// Synthesize entries for send-only agents — agents that appear only as senders
	// in agent_message events but never emitted an explicit agent_presence beat.
	// agent_message is F-class (fsync'd), so these entries survive daemon crashes
	// even when the O-class implicit refresh beats were not flushed to disk (hk-nf111).
	for agent, act := range lastActivity {
		if _, known := byAgent[agent]; known {
			continue // already covered above
		}
		byAgent[agent] = Record{
			Agent:             agent,
			Status:            "online",
			EffectiveLastSeen: act,
		}
	}

	return byAgent
}
