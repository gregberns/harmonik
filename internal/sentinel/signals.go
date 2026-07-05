// signals.go — signal library for stall-sentinel detection.
//
// Deterministic primitive that reads events.jsonl + the run registry and
// computes, per active run, last-event-age and phase state, plus lane-level
// rollups (last forward-progress per lane, live-crew set, queue/assignment
// state) for the Layer B expectation-of-progress predicate.
//
// No side effects. No LLM. Both Layer A and Layer B stall detectors are
// thin decision logic over the Snapshot this library produces.
//
// Spec: .kerf/works/stall-sentinel/SPEC.md §1 (Signal contract),
//
//	02-analysis.md §Signal-library-core, DESIGN.md §7.1 (Signal library).
//
// Bead: hk-mxxsl.
package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/run"
)

// RunPhase is the highest-watermark phase a run has reached, derived from
// its event stream. Transitions are strictly forward:
//
//	started → in-implementation → verdict-fired → terminal
type RunPhase int

const (
	// RunPhaseStarted means run_started has been seen but no later phase
	// event has arrived yet.
	RunPhaseStarted RunPhase = iota
	// RunPhaseInImplementation means implementer_phase_complete was seen.
	RunPhaseInImplementation
	// RunPhaseVerdictFired means reviewer_verdict was seen but no terminal
	// event (run_completed / run_failed) has fired yet.
	RunPhaseVerdictFired
	// RunPhaseTerminal means run_completed or run_failed was seen.
	RunPhaseTerminal
)

// String returns a human-readable label for the phase.
func (p RunPhase) String() string {
	switch p {
	case RunPhaseStarted:
		return "started"
	case RunPhaseInImplementation:
		return "in-implementation"
	case RunPhaseVerdictFired:
		return "verdict-fired"
	case RunPhaseTerminal:
		return "terminal"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// RunSignal holds the computed liveness state for one active run.
type RunSignal struct {
	// RunID is the run identifier string (UUIDv7).
	RunID string
	// BeadID is the bead this run is working on (from the run registry).
	BeadID string
	// LaneName is the queue_name from the run registry (identifies the lane).
	LaneName string
	// StartedAt is when the run was registered (from run.Record.StartedAt).
	StartedAt time.Time
	// LastEventAt is the timestamp of the most-recent event touching this
	// run. Initialised to StartedAt; updated by any event carrying this run_id.
	LastEventAt time.Time
	// LastEventAge is Snapshot.Now − LastEventAt. This is the primary
	// Layer A heartbeat-gap input.
	LastEventAge time.Duration
	// Phase is the highest-watermark phase derived from the event stream.
	Phase RunPhase
	// VerdictAt is the wall-clock time of the reviewer_verdict event.
	// Zero when no verdict has been seen (Phase < RunPhaseVerdictFired).
	VerdictAt time.Time
}

// LaneSignal holds rollup state for one lane (named queue).
type LaneSignal struct {
	// LaneName is the queue name that identifies the lane.
	LaneName string
	// LastForwardProgressAt is the most-recent time a forward-progress event
	// (run_started or bead_closed) was observed for a run in this lane.
	// Zero when no forward-progress event has been seen within the scan window.
	LastForwardProgressAt time.Time
	// LiveCrews is the sorted list of agent names that have an online
	// agent_presence beat within the presence TTL. Populated from the global
	// live-crew set because agent_presence events do not carry lane affinity;
	// Layer B callers may further filter by their own crew-to-lane knowledge.
	LiveCrews []string
	// MidFlightRunIDs is the sorted list of run_ids whose phase is not yet
	// terminal (started / in-implementation / verdict-fired).
	MidFlightRunIDs []string
	// QueueNonEmpty is injected from the caller: true when the lane's queue
	// has at least one submitted or appended item waiting for dispatch.
	QueueNonEmpty bool
	// HasAssignedBead is injected from the caller: true when at least one
	// open bead is assigned to a crew in this lane.
	HasAssignedBead bool
}

// ExpectsProgress returns true when any expectation-of-progress input holds:
// queue non-empty, assigned bead, or at least one mid-flight run.
//
// This is the critical false-positive guard for Layer B: if no expectation
// of progress exists, a correctly-idle crew MUST NOT trip the detector.
// Spec: 02-analysis.md §Layer B, DESIGN.md §2.
func (l LaneSignal) ExpectsProgress() bool {
	return l.QueueNonEmpty || l.HasAssignedBead || len(l.MidFlightRunIDs) > 0
}

// Snapshot is the output of one ComputeSnapshot call.
type Snapshot struct {
	// Now is the wall-clock instant used to compute all ages.
	Now time.Time
	// Runs maps run_id → RunSignal for every run in activeRuns.
	Runs map[string]RunSignal
	// Lanes maps lane name → LaneSignal for every lane observed in activeRuns
	// or laneStates.
	Lanes map[string]LaneSignal
}

// LaneStateInput injects queue/assignment state the caller knows but that
// cannot be derived from events.jsonl alone.
type LaneStateInput struct {
	// LaneName is the queue name of the lane.
	LaneName string
	// QueueNonEmpty is true when the lane's queue has waiting items.
	QueueNonEmpty bool
	// HasAssignedBead is true when a bead is open and assigned for this lane.
	HasAssignedBead bool
}

// DefaultPresenceTTL is the default duration after which an agent_presence
// beat is considered stale (applied when ComputeSnapshot receives presenceTTL ≤ 0).
const DefaultPresenceTTL = 15 * time.Minute

// ComputeSnapshot reads events.jsonl and the run registry to compute the
// per-run last-event-age and phase state, plus lane-level rollups.
//
// activeRuns is typically obtained from run.List(projectDir). Events are
// scanned starting from the earliest run start time (or now−presenceTTL
// when earlier) so the window covers all active runs and all presence beats.
//
// presenceTTL is the duration after which an agent_presence beat is considered
// stale. ≤ 0 uses DefaultPresenceTTL (15 minutes).
//
// laneStates injects queue/assignment state per lane; lanes omitted from
// laneStates get false for QueueNonEmpty and HasAssignedBead.
//
// eventsPath is the full path to events.jsonl
// (e.g. filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")).
func ComputeSnapshot(
	ctx context.Context,
	eventsPath string,
	activeRuns []run.Record,
	presenceTTL time.Duration,
	laneStates []LaneStateInput,
	now time.Time,
) Snapshot {
	if presenceTTL <= 0 {
		presenceTTL = DefaultPresenceTTL
	}

	// Index active runs by string run_id for O(1) lookup during the scan.
	byRunID := make(map[string]run.Record, len(activeRuns))
	for _, r := range activeRuns {
		byRunID[r.RunID] = r
	}

	// Determine the scan start: min(earliest run start, now−presenceTTL).
	// This ensures we cover presence beats even when no active runs exist.
	scanStart := now.Add(-presenceTTL)
	for _, r := range activeRuns {
		if r.StartedAt.Before(scanStart) {
			scanStart = r.StartedAt
		}
	}

	// Per-run mutable state accumulated during the scan.
	type runState struct {
		rec         run.Record
		lastEventAt time.Time
		phase       RunPhase
		verdictAt   time.Time
	}
	states := make(map[string]*runState, len(activeRuns))
	for _, r := range activeRuns {
		states[r.RunID] = &runState{
			rec:         r,
			lastEventAt: r.StartedAt, // baseline before any events are seen
		}
	}

	// Per-lane most-recent forward-progress timestamp.
	laneForwardProgress := make(map[string]time.Time)

	// Live-crew tracking: agent name → most-recent online last_seen timestamp.
	agentLastSeen := make(map[string]time.Time)

	// Scan events.jsonl starting from scanStart.
	cursor := eventIDFloorForTime(scanStart)
	for ev := range eventbus.ScanAfter(eventsPath, cursor) {
		// Guard: skip events before our window (UUIDv7 cursor is an
		// approximation; wall-clock check is authoritative).
		if ev.TimestampWall.Before(scanStart) {
			continue
		}

		evType := core.EventType(ev.Type)

		// --- agent_presence events are not run-scoped; handle separately ---
		// core.EventType("agent_presence") has no named constant in eventtype.go;
		// use the string literal from the event registry (eventreg_hqwn59.go).
		if ev.Type == "agent_presence" {
			var p core.AgentPresencePayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil || !p.Valid() {
				continue
			}
			switch p.Status {
			case core.AgentPresenceStatusOnline:
				t, err := time.Parse(time.RFC3339, p.LastSeen)
				if err == nil && t.After(agentLastSeen[p.Agent]) {
					agentLastSeen[p.Agent] = t
				}
			case core.AgentPresenceStatusOffline:
				delete(agentLastSeen, p.Agent)
			}
			continue
		}

		// --- All other events require a run_id on the envelope ---
		if ev.RunID == nil {
			continue
		}
		runIDStr := (*ev.RunID).String()
		st, ok := states[runIDStr]
		if !ok {
			// Event for a run not in the active registry (already completed
			// and its record removed). Ignore it.
			continue
		}

		// Update last-event timestamp for any run-scoped event.
		if ev.TimestampWall.After(st.lastEventAt) {
			st.lastEventAt = ev.TimestampWall
		}

		// Update phase and lane forward-progress based on event type.
		switch evType {
		case core.EventTypeRunStarted:
			if st.phase < RunPhaseStarted {
				st.phase = RunPhaseStarted
			}
			advanceLaneProgress(laneForwardProgress, st.rec.QueueName, ev.TimestampWall)

		case core.EventTypeImplementerPhaseComplete:
			if st.phase < RunPhaseInImplementation {
				st.phase = RunPhaseInImplementation
			}

		case core.EventTypeReviewerVerdict:
			if st.phase < RunPhaseVerdictFired {
				st.phase = RunPhaseVerdictFired
				st.verdictAt = ev.TimestampWall
			}

		case core.EventTypeRunCompleted, core.EventTypeRunFailed:
			st.phase = RunPhaseTerminal

		case core.EventTypeBeadClosed:
			advanceLaneProgress(laneForwardProgress, st.rec.QueueName, ev.TimestampWall)

			// agent_heartbeat and agent_message already covered by the
			// generic lastEventAt update above; no extra logic needed.
		}
	}

	// Compute the global live-crew set (within TTL).
	presenceThreshold := now.Add(-presenceTTL)
	var liveCrews []string
	for agent, lastSeen := range agentLastSeen {
		if !lastSeen.Before(presenceThreshold) {
			liveCrews = append(liveCrews, agent)
		}
	}
	sort.Strings(liveCrews)

	// Index lane state inputs for O(1) lookup.
	laneInputIdx := make(map[string]LaneStateInput, len(laneStates))
	for _, ls := range laneStates {
		laneInputIdx[ls.LaneName] = ls
	}

	// Collect all lane names from active runs and lane state inputs.
	laneNames := make(map[string]struct{})
	for _, r := range activeRuns {
		if r.QueueName != "" {
			laneNames[r.QueueName] = struct{}{}
		}
	}
	for _, ls := range laneStates {
		if ls.LaneName != "" {
			laneNames[ls.LaneName] = struct{}{}
		}
	}

	// Build per-lane mid-flight run lists.
	laneMidFlight := make(map[string][]string)
	for id, st := range states {
		if st.phase < RunPhaseTerminal {
			laneMidFlight[st.rec.QueueName] = append(laneMidFlight[st.rec.QueueName], id)
		}
	}
	for lane := range laneMidFlight {
		sort.Strings(laneMidFlight[lane])
	}

	// Build Snapshot.Runs.
	runs := make(map[string]RunSignal, len(states))
	for id, st := range states {
		runs[id] = RunSignal{
			RunID:        id,
			BeadID:       st.rec.BeadID,
			LaneName:     st.rec.QueueName,
			StartedAt:    st.rec.StartedAt,
			LastEventAt:  st.lastEventAt,
			LastEventAge: now.Sub(st.lastEventAt),
			Phase:        st.phase,
			VerdictAt:    st.verdictAt,
		}
	}

	// Build Snapshot.Lanes.
	lanes := make(map[string]LaneSignal, len(laneNames))
	for laneName := range laneNames {
		inp := laneInputIdx[laneName]
		lanes[laneName] = LaneSignal{
			LaneName:              laneName,
			LastForwardProgressAt: laneForwardProgress[laneName],
			LiveCrews:             liveCrews,
			MidFlightRunIDs:       laneMidFlight[laneName],
			QueueNonEmpty:         inp.QueueNonEmpty,
			HasAssignedBead:       inp.HasAssignedBead,
		}
	}

	return Snapshot{
		Now:   now,
		Runs:  runs,
		Lanes: lanes,
	}
}

// EventsPathForProject returns the canonical path to events.jsonl for projectDir.
// Convenience wrapper so callers don't hard-code the path.
func EventsPathForProject(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
}

// advanceLaneProgress sets m[lane] = max(current, t).
func advanceLaneProgress(m map[string]time.Time, lane string, t time.Time) {
	if cur, ok := m[lane]; !ok || t.After(cur) {
		m[lane] = t
	}
}
