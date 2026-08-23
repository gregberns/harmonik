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
	// arrives; see terminalTypes). session_keeper_cycle_parked is NOT a
	// terminal — a parked cycle can resume and complete under its own id.
	Terminal core.EventType
	// LastEventID is the EventID of the most recent event in this cycle.
	LastEventID core.EventID
}

var terminalTypes = map[core.EventType]bool{
	core.EventTypeSessionKeeperCycleComplete: true,
	core.EventTypeSessionKeeperCycleAborted:  true,
}

var interiorTypes = []core.EventType{
	core.EventTypeSessionKeeperHandoffWritten,
	core.EventTypeSessionKeeperModelDone,
	core.EventTypeSessionKeeperClearSent,
	core.EventTypeSessionKeeperNewSessionUp,
}

func isTerminal(t core.EventType) bool { return terminalTypes[t] }

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

	evs := collectSorted(path, since)

	states := map[string]*CycleState{}
	observed := map[string]struct{}{}

	for _, ev := range evs {
		rep.Events++

		if schemaMismatchSkips(&rep, ev) {
			continue
		}

		p, skip, err := decodeEvent(&rep, ev, strict)
		if err != nil {
			return rep, err
		}
		if skip {
			continue
		}
		observed[string(ev.Type)] = struct{}{}

		agent, cid, ok := cycleKey(p)
		if !ok {
			continue
		}
		st := stateFor(states, agent, cid)
		recordEvent(st, ev)

		for _, c := range checkers {
			if checkerMatches(c, string(ev.Type)) {
				rep.Violations = append(rep.Violations, c.Check(ev, p, st)...)
			}
		}
	}

	ordered := sortedStates(states)
	for _, c := range checkers {
		if f, ok := c.(Finalizer); ok {
			rep.Violations = append(rep.Violations, f.Finalize(ordered)...)
		}
	}

	rep.RegisteredNeverObserved = neverObservedKeeperTypes(observed)

	return rep, nil
}

func collectSorted(path string, since core.EventID) []core.Event {
	//nolint:prealloc // ScanAfter streams an unbounded event count; no length is known up front.
	var evs []core.Event
	for ev := range eventbus.ScanAfter(path, since) {
		evs = append(evs, ev)
	}
	sort.SliceStable(evs, func(i, j int) bool {
		a := [16]byte(evs[i].EventID)
		b := [16]byte(evs[j].EventID)
		return bytes.Compare(a[:], b[:]) < 0
	})
	return evs
}

func schemaMismatchSkips(rep *Report, ev core.Event) bool {
	err := core.ValidateEnvelopeSchemaVersion(ev)
	if err == nil || !errors.Is(err, core.ErrSchemaVersionMismatch) {
		return false
	}
	rep.SchemaMismatches = append(rep.SchemaMismatches, ev.EventID)
	if entry, ok := core.LookupPayloadCompatEntry(ev.Type); ok && !entry.CompatWindowHolds {
		return true
	}
	return false
}

func decodeEvent(rep *Report, ev core.Event, strict bool) (p core.EventPayload, skip bool, err error) {
	if ev.Type == core.EventTypeRunStarted {
		readPayload, derr := core.DecodeRunStartedForRead(ev)
		if derr == nil {
			return &readPayload, false, nil
		}
		if strict {
			return nil, false, fmt.Errorf("replay: strict decode %q (event_id=%s): %w", ev.Type, ev.EventID, derr)
		}
		rep.Malformed++
		return nil, true, nil
	}
	if strict {
		pp, derr := ev.DecodePayloadStrict()
		if derr != nil {
			if errors.Is(derr, core.ErrUnknownEventType) {
				return nil, false, &core.DispatchUnknownEventError{EventType: ev.Type, EventID: ev.EventID}
			}
			return nil, false, fmt.Errorf("replay: strict decode %q (event_id=%s): %w", ev.Type, ev.EventID, derr)
		}
		return pp, false, nil
	}
	pp, derr := core.DispatchObservational(ev)
	if derr != nil {
		if errors.Is(derr, core.ErrSkipUnknown) {
			rep.Skipped++
			return nil, true, nil
		}
		rep.Malformed++
		return nil, true, nil
	}
	return pp, false, nil
}

func recordEvent(st *CycleState, ev core.Event) {
	et := ev.Type
	if _, dup := st.Seen[et]; !dup {
		st.Seen[et] = ev
	}
	st.LastEventID = ev.EventID
	if isTerminal(et) && st.Terminal == "" {
		st.Terminal = et
	}
}

func neverObservedKeeperTypes(observed map[string]struct{}) []core.EventType {
	var out []core.EventType
	for t := range core.AllPayloadSchemaVersions() {
		if strings.HasPrefix(string(t), "session_keeper_") {
			if _, seen := observed[string(t)]; !seen {
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func compositeKey(agent, cid string) string { return agent + "\x00" + cid }

func stateFor(states map[string]*CycleState, agent, cid string) *CycleState {
	k := compositeKey(agent, cid)
	st, ok := states[k]
	if !ok {
		st = &CycleState{AgentName: agent, CycleID: cid, Seen: map[core.EventType]core.Event{}}
		states[k] = st
	}
	return st
}

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

func cycleKey(p core.EventPayload) (agent, cid string, ok bool) {
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
	case *core.SessionKeeperCycleParkedPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperClearUnconfirmedPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	case *core.SessionKeeperCycleRecoveredPayload:
		return v.AgentName, v.CycleID, v.CycleID != ""
	default:
		return "", "", false
	}
}
