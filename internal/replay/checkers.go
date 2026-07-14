package replay

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// This file holds the five session-restart invariant Checkers. Each is keyed on
// the composite (agent_name, cycle_id) via CycleState. Semantics are taken
// verbatim from events-design §4.5 — the harness reports, it does not invent
// invariant meaning.
//
// Version-awareness (events-design §7.5): pre-change corpora (recordings with no
// §8.20 interior events) legitimately lack model_done/new_session_up. SR4 is
// naturally gated — it only fires on clear_sent, itself a §8.20 event, so it is
// never evaluated against a pre-change cycle. SR6 gates explicitly on
// hasInteriorEvents so a historical cycle_complete is not falsely flagged.

// DefaultCheckers returns the full SR3/SR4/SR6/SR7/SR9 checker set in a stable
// order. SR7 and SR9 carry state, so a fresh set must be built per Replay run.
func DefaultCheckers() []Checker {
	return []Checker{
		SR3Checker{},
		SR4Checker{},
		SR6Checker{},
		NewSR7Checker(),
		SR9Checker{},
	}
}

// SR3Checker — handoff-write-done before /clear.
//
// On clear_sent, handoff_written MUST already have been seen in the cycle
// (machine-checks the runCycle SAFETY invariant, cycle.go confirmed-phase).
type SR3Checker struct{}

func (SR3Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperClearSent}
}

func (SR3Checker) Check(ev core.Event, _ core.EventPayload, s *CycleState) []Violation {
	if _, ok := s.Seen[core.EventTypeSessionKeeperHandoffWritten]; !ok {
		return []Violation{{
			EventID: ev.EventID,
			CycleID: s.CycleID,
			Rule:    "SR3",
			Detail:  fmt.Sprintf("clear_sent before handoff_written (agent=%s cycle=%s)", s.AgentName, s.CycleID),
		}}
	}
	return nil
}

// SR4Checker — /clear NEVER before model-done (the headline new invariant, D12).
//
// On clear_sent, model_done MUST already have been seen in the cycle.
type SR4Checker struct{}

func (SR4Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperClearSent}
}

func (SR4Checker) Check(ev core.Event, _ core.EventPayload, s *CycleState) []Violation {
	if _, ok := s.Seen[core.EventTypeSessionKeeperModelDone]; !ok {
		return []Violation{{
			EventID: ev.EventID,
			CycleID: s.CycleID,
			Rule:    "SR4",
			Detail:  fmt.Sprintf("clear_sent before model_done (agent=%s cycle=%s)", s.AgentName, s.CycleID),
		}}
	}
	return nil
}

// SR6Checker — brief only after the new session is confirmed.
//
// On terminal cycle_complete, EITHER new_session_up OR clear_unconfirmed (the
// degraded path) MUST have been seen. A cycle_complete with neither is flagged.
// Version-gated: only post-change cycles (those with §8.20 interior events) are
// evaluated, so a historical cycle_complete is not falsely flagged.
type SR6Checker struct{}

func (SR6Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperCycleComplete}
}

func (SR6Checker) Check(ev core.Event, _ core.EventPayload, s *CycleState) []Violation {
	if !hasInteriorEvents(s) {
		return nil // pre-change corpus: reduced invariant set
	}
	_, up := s.Seen[core.EventTypeSessionKeeperNewSessionUp]
	_, unconfirmed := s.Seen[core.EventTypeSessionKeeperClearUnconfirmed]
	if !up && !unconfirmed {
		return []Violation{{
			EventID: ev.EventID,
			CycleID: s.CycleID,
			Rule:    "SR6",
			Detail:  fmt.Sprintf("cycle_complete before new_session_up (no degraded clear_unconfirmed either) (agent=%s cycle=%s)", s.AgentName, s.CycleID),
		}}
	}
	return nil
}

// SR7Checker — no overlapping restarts (structurally guaranteed by D11 Idle-only
// gating; the harness verifies it against recorded corpora).
//
// A handoff_started for an agent that already has a non-terminal (open) cycle is
// an overlap. SR7 is stateful: it tracks the open cycle per agent_name and
// clears it on that cycle's terminal.
type SR7Checker struct {
	// open maps agent_name → the cycle_id of its currently-open cycle.
	open map[string]string
}

// NewSR7Checker returns a fresh, ready-to-use SR7 checker. A new instance is
// required per Replay run because it carries per-agent open-cycle state.
func NewSR7Checker() *SR7Checker { return &SR7Checker{open: map[string]string{}} }

func (*SR7Checker) Types() []core.EventType {
	return []core.EventType{
		core.EventTypeSessionKeeperHandoffStarted,
		core.EventTypeSessionKeeperCycleComplete,
		core.EventTypeSessionKeeperCycleAborted,
	}
}

func (c *SR7Checker) Check(ev core.Event, _ core.EventPayload, s *CycleState) []Violation {
	switch core.EventType(ev.Type) {
	case core.EventTypeSessionKeeperHandoffStarted:
		if cur, ok := c.open[s.AgentName]; ok && cur != s.CycleID {
			v := Violation{
				EventID: ev.EventID,
				CycleID: s.CycleID,
				Rule:    "SR7",
				Detail:  fmt.Sprintf("overlapping restart: cycle=%s started while cycle=%s still open (agent=%s)", s.CycleID, cur, s.AgentName),
			}
			c.open[s.AgentName] = s.CycleID // adopt the newer cycle as the open one
			return []Violation{v}
		}
		c.open[s.AgentName] = s.CycleID
	case core.EventTypeSessionKeeperCycleComplete, core.EventTypeSessionKeeperCycleAborted:
		if c.open[s.AgentName] == s.CycleID {
			delete(c.open, s.AgentName)
		}
	}
	return nil
}

// SR9Checker — bounded liveness (the "unterminated 1 → must be 0" anchor, D13).
//
// A finalizing checker: after the whole corpus is replayed, any cycle that saw
// handoff_started but never reached a terminal is flagged. Terminal exclusivity
// (cycle_complete XOR cycle_aborted) is the companion check in the same checker.
type SR9Checker struct{}

func (SR9Checker) Types() []core.EventType {
	return []core.EventType{
		core.EventTypeSessionKeeperHandoffStarted,
		core.EventTypeSessionKeeperCycleComplete,
		core.EventTypeSessionKeeperCycleAborted,
	}
}

// Check is a no-op: SR9 is decided entirely in Finalize. Replay owns the
// CycleState bookkeeping, so per-event work here is unnecessary.
func (SR9Checker) Check(core.Event, core.EventPayload, *CycleState) []Violation { return nil }

// Finalize implements Finalizer: it flags every unterminated cycle and every
// terminal-exclusivity breach across all cycles.
func (SR9Checker) Finalize(states []*CycleState) []Violation {
	var out []Violation
	for _, s := range states {
		_, started := s.Seen[core.EventTypeSessionKeeperHandoffStarted]
		if started && s.Terminal == "" {
			out = append(out, Violation{
				EventID: s.LastEventID,
				CycleID: s.CycleID,
				Rule:    "SR9",
				Detail:  fmt.Sprintf("unterminated cycle: handoff_started with no cycle_complete/cycle_aborted (agent=%s cycle=%s)", s.AgentName, s.CycleID),
			})
		}
		_, complete := s.Seen[core.EventTypeSessionKeeperCycleComplete]
		_, aborted := s.Seen[core.EventTypeSessionKeeperCycleAborted]
		if complete && aborted {
			out = append(out, Violation{
				EventID: s.LastEventID,
				CycleID: s.CycleID,
				Rule:    "SR9",
				Detail:  fmt.Sprintf("terminal exclusivity: both cycle_complete and cycle_aborted seen (agent=%s cycle=%s)", s.AgentName, s.CycleID),
			})
		}
	}
	return out
}

// Compile-time assertions that the stateful/finalizing checkers satisfy the
// interfaces Replay relies on.
var (
	_ Checker   = SR3Checker{}
	_ Checker   = SR4Checker{}
	_ Checker   = SR6Checker{}
	_ Checker   = (*SR7Checker)(nil)
	_ Checker   = SR9Checker{}
	_ Finalizer = SR9Checker{}
)
