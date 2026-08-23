package replay

import (
	"fmt"
	"sort"

	"github.com/gregberns/harmonik/internal/core"
)

// RunChecker is one run-keyed invariant, mirroring Checker but handed a
// RunState (the per-run_id accumulator) instead of a CycleState. Types reports
// the event types it observes (empty ⇒ all); Check runs once per matching
// event, in EventID order, after RunState has recorded the current event.
type RunChecker interface {
	// Types returns the event types this checker observes. Empty ⇒ all types.
	Types() []core.EventType
	// Check is called for each matching event after RunState has recorded it.
	Check(ev core.Event, p core.EventPayload, s *RunState) []Violation
}

// RunFinalizer is the run-keyed companion to Finalizer for invariants decided
// only once the whole corpus has been seen (RSM9 bounded liveness). CheckRuns
// calls Finalize once, with every RunState in deterministic run_id order.
type RunFinalizer interface {
	Finalize(states []*RunState) []Violation
}

// RunState is the per-run_id running state CheckRuns maintains and hands to
// each RunChecker. It mirrors CycleState: CheckRuns owns the bookkeeping
// (Seen, Terminal, LastEventID) centrally so every checker sees a consistent
// view; checkers are pure readers of it.
type RunState struct {
	// RunID is the run_id join key (a UUID string, or the raw string for the
	// run_stale payload whose run_id is already a string).
	RunID string
	// Seen records the FIRST occurrence of each event type in this run.
	Seen map[core.EventType]core.Event
	// Terminal is the first run-terminal event type seen ("" until a terminal
	// review_loop_cycle_complete/run_completed/run_failed arrives).
	Terminal core.EventType
	// LastEventID is the EventID of the most recent event in this run.
	LastEventID core.EventID
}

var runTerminalTypes = map[core.EventType]bool{
	core.EventTypeReviewLoopCycleComplete: true,
	core.EventTypeRunCompleted:            true,
	core.EventTypeRunFailed:               true,
}

var runFailureClassTypes = map[core.EventType]bool{
	core.EventTypeAgentReadyTimeout: true,
	core.EventTypeRunStale:          true,
}

func isRunTerminal(t core.EventType) bool { return runTerminalTypes[t] }

func hasFailureClass(s *RunState) bool {
	for t := range runFailureClassTypes {
		if _, ok := s.Seen[t]; ok {
			return true
		}
	}
	return false
}

// DefaultRunCheckers returns the RSM4/RSM9 run-keyed checker set in a stable
// order. Neither carries per-run state (RunState is owned by CheckRuns), so a
// fresh set is not required per run, but the constructor mirrors
// DefaultCheckers for symmetry.
func DefaultRunCheckers() []RunChecker {
	return []RunChecker{
		RSM4Checker{},
		RSM9Checker{},
	}
}

// CheckRuns is the run-keyed peer of Replay. It scans the events.jsonl at path
// (EventID-sorted for determinism, D9), decodes each through the typed core
// registry, routes every event carrying a run_id into its RunState, and runs
// the RunCheckers. It reuses Replay's scan/decode plumbing verbatim; the only
// difference is the join key (run_id, not the (agent, cycle) composite).
//
// strict/since have the same meaning as Replay. On success it returns the
// populated Report (Events/Skipped/Malformed/SchemaMismatches/Violations) and a
// nil error; in strict mode a decode failure returns a partial Report + error.
func CheckRuns(path string, since core.EventID, strict bool, checkers []RunChecker) (Report, error) {
	var rep Report

	evs := collectSorted(path, since)
	states := map[string]*RunState{}

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
		rid, ok := runKey(p)
		if !ok {
			continue
		}
		st := runStateFor(states, rid)
		runRecordEvent(st, ev)
		for _, c := range checkers {
			if runCheckerMatches(c, string(ev.Type)) {
				rep.Violations = append(rep.Violations, c.Check(ev, p, st)...)
			}
		}
	}

	ordered := sortedRunStates(states)
	for _, c := range checkers {
		if f, ok := c.(RunFinalizer); ok {
			rep.Violations = append(rep.Violations, f.Finalize(ordered)...)
		}
	}
	return rep, nil
}

func runRecordEvent(st *RunState, ev core.Event) {
	et := ev.Type
	if _, dup := st.Seen[et]; !dup {
		st.Seen[et] = ev
	}
	st.LastEventID = ev.EventID
	if isRunTerminal(et) && st.Terminal == "" {
		st.Terminal = et
	}
}

func runStateFor(states map[string]*RunState, rid string) *RunState {
	st, ok := states[rid]
	if !ok {
		st = &RunState{RunID: rid, Seen: map[core.EventType]core.Event{}}
		states[rid] = st
	}
	return st
}

func sortedRunStates(states map[string]*RunState) []*RunState {
	out := make([]*RunState, 0, len(states))
	for _, s := range states {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID < out[j].RunID })
	return out
}

func runCheckerMatches(c RunChecker, evType string) bool {
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

func runKey(p core.EventPayload) (rid string, ok bool) {
	switch v := p.(type) {
	case *core.RunStartedPayload:
		return runIDStr(v.RunID)
	case *core.RunStartedReadPayload:
		return runIDStr(v.RunID)
	case *core.RunCompletedPayload:
		return runIDStr(v.RunID)
	case *core.RunFailedPayload:
		return runIDStr(v.RunID)
	case *core.LaunchInitiatedPayload:
		return runIDStr(v.RunID)
	case *core.AgentReadyPayload:
		return runIDStr(v.RunID)
	case *core.AgentReadyTimeoutPayload:
		return runIDStr(v.RunID)
	case *core.ImplementerResumedPayload:
		return runIDStr(v.RunID)
	case *core.ReviewLoopCycleCompletePayload:
		return runIDStr(v.RunID)
	case *core.OutcomeEmittedPayload:
		return runIDStr(v.RunID)
	case *core.RunStalePayload:
		return v.RunID, v.RunID != ""
	default:
		return "", false
	}
}

func runIDStr(id core.RunID) (string, bool) {
	if id == (core.RunID{}) {
		return "", false
	}
	return id.String(), true
}

// RSM4Checker — launch-before-ready ordering (the run-keyed peer of SR4).
//
// On agent_ready, launch_initiated MUST already have been seen in the run: a
// readiness signal for a run whose launch was never observed is out of order
// (the daemon-side analog of SR4's "clear never before model-done"). This is
// the replay-visible half of the RSM-INV-002 structural rule that
// Dispatch: Launching → agent_ready is the only route into the ready state.
type RSM4Checker struct{}

// Types reports that RSM4 observes agent_ready only.
func (RSM4Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeAgentReady}
}

// Check flags an agent_ready whose run has no prior launch_initiated.
func (RSM4Checker) Check(ev core.Event, _ core.EventPayload, s *RunState) []Violation {
	if _, ok := s.Seen[core.EventTypeLaunchInitiated]; !ok {
		return []Violation{{
			EventID: ev.EventID,
			CycleID: s.RunID,
			Rule:    "RSM4",
			Detail:  fmt.Sprintf("agent_ready before launch_initiated (run=%s)", s.RunID),
		}}
	}
	return nil
}

// RSM9Checker — bounded liveness (RSM-INV-001, the SR9Checker mirror, D13).
//
// A finalizing checker: after the whole corpus is replayed, any run that emitted
// implementer_resumed but never reached a run-terminal AND never emitted a
// failure-class event (agent_ready_timeout / run_stale) is flagged — the
// "resumed but silent" gap the resume-hang fix closes. Silence is forbidden.
// Terminal exclusivity (run_completed XOR run_failed) is the companion check in
// the same checker.
type RSM9Checker struct{}

// Types reports the resume + terminal + failure-class events RSM9 tracks. The
// decision is made in Finalize; Types keeps the routing surface explicit.
func (RSM9Checker) Types() []core.EventType {
	return []core.EventType{
		core.EventTypeImplementerResumed,
		core.EventTypeRunCompleted,
		core.EventTypeRunFailed,
		core.EventTypeReviewLoopCycleComplete,
		core.EventTypeAgentReadyTimeout,
		core.EventTypeRunStale,
	}
}

// Check is a no-op: RSM9 is decided entirely in Finalize. CheckRuns owns the
// RunState bookkeeping, so per-event work here is unnecessary.
func (RSM9Checker) Check(core.Event, core.EventPayload, *RunState) []Violation { return nil }

// Finalize flags every resumed-but-silent run and every terminal-exclusivity
// breach across all runs.
func (RSM9Checker) Finalize(states []*RunState) []Violation {
	var out []Violation
	for _, s := range states {
		out = append(out, rsm9LivenessViolation(s)...)
		out = append(out, rsm9ExclusivityViolation(s)...)
	}
	return out
}

func rsm9LivenessViolation(s *RunState) []Violation {
	_, resumed := s.Seen[core.EventTypeImplementerResumed]
	if !resumed || s.Terminal != "" || hasFailureClass(s) {
		return nil
	}
	return []Violation{{
		EventID: s.LastEventID,
		CycleID: s.RunID,
		Rule:    "RSM9",
		Detail:  fmt.Sprintf("resumed run with no terminal or failure-class event: silence forbidden (run=%s)", s.RunID),
	}}
}

func rsm9ExclusivityViolation(s *RunState) []Violation {
	_, completed := s.Seen[core.EventTypeRunCompleted]
	_, failed := s.Seen[core.EventTypeRunFailed]
	if !completed || !failed {
		return nil
	}
	return []Violation{{
		EventID: s.LastEventID,
		CycleID: s.RunID,
		Rule:    "RSM9",
		Detail:  fmt.Sprintf("terminal exclusivity: both run_completed and run_failed seen (run=%s)", s.RunID),
	}}
}

var (
	_ RunChecker   = RSM4Checker{}
	_ RunChecker   = RSM9Checker{}
	_ RunFinalizer = RSM9Checker{}
)
