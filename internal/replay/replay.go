// Package replay is the session-restart-substrate invariant-checking harness.
//
// It is the event registry's first production READER (EV-033 "observational
// consumer"): it reads a recorded events.jsonl log via eventbus.ScanAfter,
// decodes each envelope through the typed core registry, and runs a set of
// Checkers that flag violations of the session-restart (SR) invariants
// SR3/SR4/SR6/SR7/SR9 (events-design §4, decisions D6).
//
// The harness is a pure offline reader. It never emits, never mutates the log,
// and never spawns the daemon. It is the machine-checkable oracle for the
// keeper restart-cycle: given a corpus of session_keeper_* events it answers
// "did every restart cycle obey the ordering + liveness contracts?"
//
// Join key (D7 / events-design §3.2): cycles are keyed on the COMPOSITE
// (agent_name, cycle_id). cycle_id alone is not globally unique — newCycleIDGen
// resets its sequence per process, so ~476 distinct ids span 507 real cycles.
// The composite pair is the true cycle identity and every §8.20 payload carries
// both fields.
//
// Ordering (D9 / events-design §5.3): the daemon JSONLWriter and N keeper
// FileEmitters append to one events.jsonl, each with its own EventID generator,
// so file order is only approximate global order. Replay collects the scanned
// events and SORTS them by EventID before checking, making the ordering
// invariants (SR3/SR4/SR6) deterministic regardless of cross-writer interleave.
package replay

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// Checker is one invariant. Types reports the event types the checker cares
// about (an empty slice means "all types"); Replay invokes Check once per
// matching event, in EventID order, with the decoded payload and the running
// CycleState for that event's (agent_name, cycle_id) composite key. Check
// returns any violations the event triggers.
type Checker interface {
	// Types returns the event types this checker observes. Empty ⇒ all types.
	Types() []core.EventType
	// Check is called for each matching event after CycleState has already
	// recorded the current event. It returns zero or more violations.
	Check(ev core.Event, p core.EventPayload, s *CycleState) []Violation
}

// Finalizer is an OPTIONAL companion to Checker for invariants that can only be
// decided once the whole corpus has been seen (e.g. SR9 bounded liveness: a
// cycle that never reached a terminal). Replay calls Finalize once, after the
// scan loop drains, with every CycleState in deterministic (agent, cycle) order.
type Finalizer interface {
	Finalize(states []*CycleState) []Violation
}

// Violation is one flagged invariant breach. The pinned shape is
// (EventID, CycleID, Rule, Detail) per decisions D6; the agent name is folded
// into Detail because the composite key is (agent_name, cycle_id).
type Violation struct {
	// EventID is the event that triggered the violation. For a finalizer
	// violation (no single triggering event) it is the cycle's last EventID.
	EventID core.EventID
	// CycleID is the cycle_id of the offending cycle.
	CycleID string
	// Rule is the SR invariant tag ("SR3".."SR9").
	Rule string
	// Detail is a human-readable explanation, including the agent name.
	Detail string
}

// Report is the outcome of a Replay run.
type Report struct {
	// Events is the number of events considered (post-sort).
	Events int
	// Skipped is the number of unknown-type events skipped in observational
	// mode (EV-033).
	Skipped int
	// Malformed is the number of events with a genuine JSON decode error
	// (neither unknown-type nor skip) in observational mode.
	Malformed int
	// SchemaMismatches lists events whose envelope schema_version did not match
	// the registered per-type version (a writer/reader-drift finding, not fatal).
	SchemaMismatches []core.EventID
	// RegisteredNeverObserved lists session_keeper_* event types that are
	// registered in the catalog but never appeared in the corpus. Informational
	// (taxonomy-rot detection, e.g. session_keeper_operator_attached), never a
	// violation (events-design §4.6).
	RegisteredNeverObserved []core.EventType
	// Violations is every flagged invariant breach, in deterministic order.
	Violations []Violation
}

// CycleState is the per-(agent_name, cycle_id) running state Replay maintains
// and hands to each Checker. Replay owns the bookkeeping (Seen, Terminal,
// LastEventID) centrally so every checker sees a consistent view; checkers are
// pure readers of it.
type CycleState struct {
	// AgentName and CycleID form the composite join key.
	AgentName string
	CycleID   string
	// Seen records the FIRST occurrence of each event type in this cycle.
	Seen map[core.EventType]core.Event
	// Terminal is the first terminal event type seen ("" until a terminal
	// cycle_complete/cycle_aborted arrives).
	Terminal core.EventType
	// LastEventID is the EventID of the most recent event in this cycle.
	LastEventID core.EventID
}

// terminalTypes are the events that terminate a restart cycle.
var terminalTypes = map[core.EventType]bool{
	core.EventTypeSessionKeeperCycleComplete: true,
	core.EventTypeSessionKeeperCycleAborted:  true,
}

// interiorTypes are the four §8.20 interior milestones. Their presence in a
// cycle marks it as a POST-change corpus recording, which selects the full
// SR3/SR4/SR6 invariant set (events-design §7.5, version-aware checks).
var interiorTypes = []core.EventType{
	core.EventTypeSessionKeeperHandoffWritten,
	core.EventTypeSessionKeeperModelDone,
	core.EventTypeSessionKeeperClearSent,
	core.EventTypeSessionKeeperNewSessionUp,
}

// isTerminal reports whether t terminates a cycle.
func isTerminal(t core.EventType) bool { return terminalTypes[t] }

// hasInteriorEvents reports whether the cycle carries any §8.20 interior event,
// i.e. whether it is a post-change recording (events-design §7.5).
func hasInteriorEvents(s *CycleState) bool {
	for _, t := range interiorTypes {
		if _, ok := s.Seen[t]; ok {
			return true
		}
	}
	return false
}

// Replay reads the events.jsonl at path (via eventbus.ScanAfter, starting after
// the since watermark), sorts the scanned events by EventID for deterministic
// order (D9), decodes each through the typed core registry, and runs checkers.
//
//   - since: the EventID watermark; core.EventID{} (zero) replays from the
//     beginning (the established idiom), any other id replays incrementally.
//   - strict: false (default) uses DispatchObservational — an unknown type is
//     SKIPPED (Report.Skipped++, EV-033). true uses DecodePayloadStrict — an
//     unknown type or an unknown payload field is a HARD finding and Replay
//     returns a non-nil error (for replaying the harness's OWN corpus, where an
//     unknown type means a mustRegister was forgotten).
//
// On success Replay returns the populated Report and a nil error. In strict
// mode a decode failure returns a partial Report and a non-nil error.
func Replay(path string, since core.EventID, strict bool, checkers []Checker) (Report, error) {
	var rep Report

	// 1. Collect + EventID-sort (D9). ScanAfter yields file order; a single
	//    log is written by multiple processes with independent EventID
	//    generators, so we re-sort by the 16-byte UUIDv7 before checking.
	var evs []core.Event
	for ev := range eventbus.ScanAfter(path, since) {
		evs = append(evs, ev)
	}
	sort.SliceStable(evs, func(i, j int) bool {
		a := [16]byte(evs[i].EventID)
		b := [16]byte(evs[j].EventID)
		return bytes.Compare(a[:], b[:]) < 0
	})

	states := map[string]*CycleState{}
	observed := map[string]struct{}{}

	// 2. Decode + assert loop.
	for _, ev := range evs {
		rep.Events++

		// 2a. Schema-version validation. A mismatch is a finding (writer/reader
		//     drift), not fatal — record it and, if the type's N-1 compat window
		//     is NOT declared safe, skip the checks for this event. An unknown
		//     type here is left to the decode step's skip-vs-fail policy.
		if err := core.ValidateEnvelopeSchemaVersion(ev); err != nil {
			if errors.Is(err, core.ErrSchemaVersionMismatch) {
				rep.SchemaMismatches = append(rep.SchemaMismatches, ev.EventID)
				if entry, ok := core.LookupPayloadCompatEntry(ev.Type); ok && !entry.CompatWindowHolds {
					continue
				}
			}
		}

		// 2b. Decode.
		var p core.EventPayload
		if strict {
			pp, err := ev.DecodePayloadStrict()
			if err != nil {
				if errors.Is(err, core.ErrUnknownEventType) {
					return rep, &core.DispatchUnknownEventError{EventType: ev.Type, EventID: ev.EventID}
				}
				return rep, fmt.Errorf("replay: strict decode %q (event_id=%s): %w", ev.Type, ev.EventID, err)
			}
			p = pp
		} else {
			pp, err := core.DispatchObservational(ev)
			if err != nil {
				if errors.Is(err, core.ErrSkipUnknown) {
					rep.Skipped++
					continue
				}
				rep.Malformed++
				continue
			}
			p = pp
		}
		observed[ev.Type] = struct{}{}

		// 2c. Route into the composite-keyed CycleState.
		agent, cid, ok := cycleKey(p)
		if !ok {
			// Non-cycle keeper payload or a foreign type with no join key:
			// counted as observed but not routed to a cycle.
			continue
		}
		st := stateFor(states, agent, cid)
		et := core.EventType(ev.Type)
		if _, dup := st.Seen[et]; !dup {
			st.Seen[et] = ev
		}
		st.LastEventID = ev.EventID
		if isTerminal(et) && st.Terminal == "" {
			st.Terminal = et
		}

		// 2d. Run the matching checkers over the (already-updated) state.
		for _, c := range checkers {
			if checkerMatches(c, ev.Type) {
				rep.Violations = append(rep.Violations, c.Check(ev, p, st)...)
			}
		}
	}

	// 3. Finalize (SR9 and any other end-of-corpus checker). Iterate states in
	//    deterministic (agent, cycle) order so the report is reproducible.
	ordered := sortedStates(states)
	for _, c := range checkers {
		if f, ok := c.(Finalizer); ok {
			rep.Violations = append(rep.Violations, f.Finalize(ordered)...)
		}
	}

	// 4. Registered-but-never-observed (events-design §4.6). Scoped to the
	//    session_keeper_* family — the taxonomy this harness is responsible for;
	//    reporting the entire 169-type registry would bury the signal. Reported,
	//    never a violation.
	for t := range core.AllPayloadSchemaVersions() {
		if strings.HasPrefix(t, "session_keeper_") {
			if _, seen := observed[t]; !seen {
				rep.RegisteredNeverObserved = append(rep.RegisteredNeverObserved, core.EventType(t))
			}
		}
	}
	sort.Slice(rep.RegisteredNeverObserved, func(i, j int) bool {
		return rep.RegisteredNeverObserved[i] < rep.RegisteredNeverObserved[j]
	})

	return rep, nil
}

// compositeKey builds the map key for a (agent_name, cycle_id) pair. The NUL
// separator makes the join unambiguous even if either component contains a
// separator character (events-design §4.4).
func compositeKey(agent, cid string) string { return agent + "\x00" + cid }

// stateFor returns the CycleState for (agent, cid), creating it on first sight.
func stateFor(states map[string]*CycleState, agent, cid string) *CycleState {
	k := compositeKey(agent, cid)
	st, ok := states[k]
	if !ok {
		st = &CycleState{AgentName: agent, CycleID: cid, Seen: map[core.EventType]core.Event{}}
		states[k] = st
	}
	return st
}

// sortedStates returns the states in deterministic (agent, cycle) order.
func sortedStates(states map[string]*CycleState) []*CycleState {
	out := make([]*CycleState, 0, len(states))
	for _, s := range states {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentName != out[j].AgentName {
			return out[i].AgentName < out[j].AgentName
		}
		return out[i].CycleID < out[j].CycleID
	})
	return out
}

// checkerMatches reports whether checker c observes an event of type evType.
func checkerMatches(c Checker, evType string) bool {
	ts := c.Types()
	if len(ts) == 0 {
		return true
	}
	for _, t := range ts {
		if string(t) == evType {
			return true
		}
	}
	return false
}

// GetCycleID and GetAgentName are the structural mini-interfaces the harness
// uses to pull the composite (agent_name, cycle_id) join key off a decoded
// payload without knowing its concrete type (events-design §4.4). Any payload
// that implements them is routed by the join key it reports.
type (
	// cycleIDer exposes a payload's cycle_id join component.
	cycleIDer interface{ GetCycleID() string }
	// agentNamer exposes a payload's agent_name join component.
	agentNamer interface{ GetAgentName() string }
)

// cycleKey extracts the composite (agent_name, cycle_id) join key from a decoded
// payload. It prefers the GetCycleID/GetAgentName mini-interfaces; because
// internal/core is not modifiable in this task (the interior payloads do not yet
// declare those methods), it falls back to a type switch over the concrete
// keeper cycle payloads that carry the join key. ok is false for a payload with
// no cycle scope (a foreign type or a non-cycle keeper event).
func cycleKey(p core.EventPayload) (agent, cid string, ok bool) {
	// Preferred path: a payload that implements the mini-interfaces.
	if c, isCyc := p.(cycleIDer); isCyc {
		cid = c.GetCycleID()
		if a, isAgent := p.(agentNamer); isAgent {
			agent = a.GetAgentName()
		}
		return agent, cid, cid != ""
	}
	// Fallback: concrete keeper cycle payloads (all fields exported). Covers the
	// four §8.20 interior events plus the existing §8.16 cycle payloads, so the
	// harness sees whole cycles (handoff_started → terminal).
	switch v := p.(type) {
	case *core.SessionKeeperHandoffWrittenPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperModelDonePayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperClearSentPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperNewSessionUpPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperHandoffStartedPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperCycleCompletePayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperCycleAbortedPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperClearUnconfirmedPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperCycleRecoveredPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	default:
		return "", "", false
	}
}
