package replay

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

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

// Types reports the event types SR3 subscribes to: clear_sent.
func (SR3Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperClearSent}
}

// Check flags a clear_sent that arrives before handoff_written in the cycle.
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

// Types reports the event types SR4 subscribes to: clear_sent.
func (SR4Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperClearSent}
}

// Check flags a clear_sent that arrives before model_done in the cycle.
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

// Types reports the event types SR6 subscribes to: cycle_complete.
func (SR6Checker) Types() []core.EventType {
	return []core.EventType{core.EventTypeSessionKeeperCycleComplete}
}

// Check flags a cycle_complete that reaches terminal without either
// new_session_up or the degraded clear_unconfirmed (post-change cycles only).
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
// clears the slot when that cycle leaves the machine's non-Idle phases.
//
// The slot closes on a terminal (isTerminal — cycle_complete live,
// cycle_aborted in recorded corpora) AND on session_keeper_cycle_parked. Park
// is not a terminal, but both park paths (internal/keeper stepParkPending and
// stepParkForOperator) set Phase back to Idle, and Idle is exactly the
// condition D11 gates a new cycle on. Leaving the slot open across a park would
// flag the next legitimate cycle — after an operator_turn_recent park the
// keeper mints a fresh cycle id on the very next tick, so that false positive
// would fire on every healthy corpus.
//
// What that gives up is real, not theoretical: a second handoff_started while a
// handoff_pending park is still resumable is not flagged, and the keeper can
// produce exactly that today. internal/keeper Cycler.resumePendingHandoff is
// the guard, but MaybeRun is its only caller — RunForIdle and RunForPrecompact
// reach runEntry unguarded, so either one mints a second cycle id on top of a
// parked request and orphans it. Measured by driving the cycler, not inferred.
// So closing the slot on park trades away the one signal that would have shown
// that defect in replay. Bead hk-keeper-park-bypassed-entry-points-1oi2h owns
// the fix; when it lands, reconsider whether this slot should stay open for a
// handoff_pending park specifically.
type SR7Checker struct {
	// open maps agent_name → the cycle_id of its currently-open cycle.
	open map[string]string
}

// NewSR7Checker returns a fresh, ready-to-use SR7 checker. A new instance is
// required per Replay run because it carries per-agent open-cycle state.
func NewSR7Checker() *SR7Checker { return &SR7Checker{open: map[string]string{}} }

// Types reports the event types SR7 subscribes to: handoff_started, the
// terminals, and cycle_parked — everything that opens or closes the slot.
func (*SR7Checker) Types() []core.EventType {
	return []core.EventType{
		core.EventTypeSessionKeeperHandoffStarted,
		core.EventTypeSessionKeeperCycleComplete,
		core.EventTypeSessionKeeperCycleAborted,
		core.EventTypeSessionKeeperCycleParked,
	}
}

// Check flags a handoff_started for an agent whose prior cycle is still open,
// adopting the newer cycle, and clears the open slot when the cycle returns to
// Idle (a terminal, or a park).
func (c *SR7Checker) Check(ev core.Event, _ core.EventPayload, s *CycleState) []Violation {
	if ev.Type == core.EventTypeSessionKeeperHandoffStarted {
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
		return nil
	}
	if isTerminal(ev.Type) || ev.Type == core.EventTypeSessionKeeperCycleParked {
		if c.open[s.AgentName] == s.CycleID {
			delete(c.open, s.AgentName)
		}
	}
	return nil
}

// SR9Checker — bounded liveness (the "unterminated 1 → must be 0" anchor, D13).
//
// A finalizing checker: after the whole corpus is replayed, it flags every
// cycle that opened with handoff_started and never reached a terminal, in the
// two shapes that are real breaches:
//
//   - AUTHORIZED and unterminated. handoff_written is where the machine grants
//     restart authority (internal/keeper stepConfirmHandoff): past it the cycle
//     owns the destructive tail — /clear and the re-brief. session-keeper.md
//     SK-INV-005 bounds exactly that: "For every authorized restart c, the
//     destructive tail MUST reach exactly one terminal outcome."
//   - OPENED AND ABANDONED. handoff_started with no authority, no terminal, and
//     no park either. Nothing recorded why the cycle stopped, which is the
//     wedge shape D13 was written against. The frozen 507-cycle baseline under
//     testdata/keeper-cycles/ is entirely pre-authority (no handoff_written
//     anywhere) and its one known unterminated cycle lands here, so dropping
//     this arm would make that whole baseline report zero.
//
// A cycle that parked without ever reaching authority is NOT flagged. Park is
// the machine SAYING it stopped and why: handoff_pending is a suspension the
// same cycle_id resumes from, and operator_turn_recent is a final decision not
// to restart over a live operator turn. SK-INV-005 covers this directly — "A
// pending context request is not an authorized restart and can remain pending"
// — and SK-025 requires the resume path, which is exercised by
// internal/keeper TestScenario_LateMarkedHandoff_ResumesOriginalRequest.
//
// There is no terminal-exclusivity companion check any more. It tested
// cycle_complete && cycle_aborted, and nothing has emitted cycle_aborted since
// keeper checkpoints became agent-paced, so the condition was unreachable —
// and CycleState.Seen keeps first occurrences only, so it could not have caught
// a duplicate terminal either. Extending it to complete && parked would be
// worse than dead: the SK-025 resume path emits exactly that pair on a HEALTHY
// cycle. The counterpart worth having — a terminal with no preceding
// handoff_written — is already SR3's finding on the same corpus, so it was
// deleted rather than restated here.
type SR9Checker struct{}

// Types reports the event types SR9 subscribes to. Finalize reads CycleState
// rather than the event stream, so this list only has to keep those events
// routed; the set is the one Finalize correlates.
func (SR9Checker) Types() []core.EventType {
	return []core.EventType{
		core.EventTypeSessionKeeperHandoffStarted,
		core.EventTypeSessionKeeperHandoffWritten,
		core.EventTypeSessionKeeperCycleComplete,
		core.EventTypeSessionKeeperCycleAborted,
		core.EventTypeSessionKeeperCycleParked,
	}
}

// Check is a no-op: SR9 is decided entirely in Finalize. Replay owns the
// CycleState bookkeeping, so per-event work here is unnecessary.
func (SR9Checker) Check(core.Event, core.EventPayload, *CycleState) []Violation { return nil }

// Finalize implements Finalizer: it flags every unterminated cycle, split by
// whether the cycle ever held restart authority. Both details carry the phrase
// "unterminated cycle" so a corpus-level classifier can group them, and then
// name the case so a human reading one violation knows which they hit.
func (SR9Checker) Finalize(states []*CycleState) []Violation {
	var out []Violation
	for _, s := range states {
		if _, started := s.Seen[core.EventTypeSessionKeeperHandoffStarted]; !started {
			continue
		}
		if s.Terminal != "" {
			continue
		}
		_, authorized := s.Seen[core.EventTypeSessionKeeperHandoffWritten]
		_, parked := s.Seen[core.EventTypeSessionKeeperCycleParked]
		switch {
		case authorized:
			out = append(out, Violation{
				EventID: s.LastEventID,
				CycleID: s.CycleID,
				Rule:    "SR9",
				Detail: fmt.Sprintf("unterminated cycle: restart authorized at handoff_written, no terminal outcome followed (agent=%s cycle=%s)",
					s.AgentName, s.CycleID),
			})
		case parked:
		default:
			out = append(out, Violation{
				EventID: s.LastEventID,
				CycleID: s.CycleID,
				Rule:    "SR9",
				Detail: fmt.Sprintf("unterminated cycle: handoff_started reached neither restart authority (handoff_written), a terminal, nor a park (agent=%s cycle=%s)",
					s.AgentName, s.CycleID),
			})
		}
	}
	return out
}

var (
	_ Checker   = SR3Checker{}
	_ Checker   = SR4Checker{}
	_ Checker   = SR6Checker{}
	_ Checker   = (*SR7Checker)(nil)
	_ Checker   = SR9Checker{}
	_ Finalizer = SR9Checker{}
)
