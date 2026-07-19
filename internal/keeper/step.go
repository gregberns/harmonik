package keeper

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// step.go — the PURE keeper cycle reactor (T7, SK-009/010/011; session-keeper-
// design §3). `Cycle` mirrors codexreactor.Reactor: it holds `CycleState`,
// exposes Step(ev Event) []Action and State(), and is drivable by the free
// function substrate.Run[keeper.Event, keeper.Action]. The transition function
// (stepCycle) is TOTAL and pure: no IO, no clock reads, no id minting — every
// timestamp comes from an event's Clock-stamped At field (D11/SK-008), the
// cycle id is minted by the shell and carried on the entry event (design §2a),
// and the handoff content for the stale-nonce predicate is sampled by the
// shell and carried on the entry event (design §3d "given file content on the
// event").
//
// States: Idle → AwaitingHandoff → AwaitModelDone → Clearing → Briefing →
// {Complete | Aborted}. Briefing is IMMEDIATE (no external event): the
// transition that reaches it emits the full brief/journal/terminal batch and
// lands the phase back at Idle in the same Step call (§3c "Briefing (entry)").
// Terminals therefore appear as Phase returning to Idle with LastTerminal set.
//
// Timers are events (SK-010): Step emits ArmTimer/CancelTimer actions and
// consumes TimerFired events; the shell (shell.go) owns the ClockPort
// deadlines and the 200ms detection poll. The two pre-rebuild blocking poll
// loops (pollForNonce, waitForNewSessionIDWithBackstop — deleted) and the
// backstop deadline are dissolved into exactly these event interleavings.
//
// T8 (SK-012/013/014, SK-INV-002): the four §8.20 interior-event emissions
// (session_keeper_handoff_written / model_done / clear_sent / new_session_up)
// are emitted at their named transitions (design §4), and SR4 — /clear MUST
// NOT be injected before the cycle's model-done signal — is STRUCTURAL:
// injectClearAction is the only ActInjectClear constructor and refuses until
// CycleState.ModelDoneSource is recorded by the single AwaitModelDone →
// Clearing edge. Model-done DETECTION (the .idle-mtime read, the transcript
// backstop) is shell-side (shell.go pollOnce); the reactor only consumes the
// resulting ModelDone / TimerFired(model_done_timeout) events.

// Phase is the reactor's state-machine phase.
type Phase string

// The keeper cycle phases (session-keeper-design §3c / SK §7.1).
const (
	PhaseIdle            Phase = "idle"
	PhaseAwaitingHandoff Phase = "awaiting_handoff"
	PhaseAwaitModelDone  Phase = "await_model_done"
	PhaseClearing        Phase = "clearing"
	// PhaseBriefing is documented for completeness: Briefing is an IMMEDIATE
	// pass-through (no external event is consumed in it), so Step never rests
	// here — the transition into Briefing emits the brief batch and returns
	// the machine to PhaseIdle in the same call.
	PhaseBriefing Phase = "briefing"
)

// EventKind discriminates the flat Event struct.
type EventKind string

// The shell→reactor event vocabulary (design §3a / SK §6.3).
const (
	EvGaugeTick         EventKind = "gauge_tick"
	EvPrecompactTrigger EventKind = "precompact_trigger"
	EvIdleRestartTick   EventKind = "idle_restart_tick"
	EvNonceObserved     EventKind = "nonce_observed"
	EvHandoffFreshSeen  EventKind = "handoff_fresh_seen"
	EvModelDone         EventKind = "model_done"
	EvSessionChanged    EventKind = "session_changed"
	EvTimerFired        EventKind = "timer_fired"
	EvCrashJournal      EventKind = "crash_journal"
)

// TimerKind names the four reactor timers (design §2c / SK-010).
type TimerKind string

// The four timer kinds. TimerModelDone is the SR4 fail-open liveness bound
// (SK-014/SR9): armed on entry to AwaitModelDone; its firing proceeds to
// Clearing anyway with model_done{source:"timeout", degraded:true}, so a lost
// .idle write can never wedge the cycle.
const (
	TimerHandoffTimeout TimerKind = "handoff_timeout"
	TimerModelDone      TimerKind = "model_done_timeout"
	TimerClearSettle    TimerKind = "clear_settle"
	TimerClearBackstop  TimerKind = "clear_backstop"
)

// Event is the flat, JSON-round-trippable reactor input (design §3a).
// Field population by kind:
//   - GaugeTick/PrecompactTrigger/IdleRestartTick: CF, Gates, At; CycleID is
//     the shell-minted candidate id (set only when the shell's ladder peek
//     says the entry fires — CycleIDGen call counts stay fire-aligned);
//     HandoffContent/HandoffReadOK carry the handoff file sample for the pure
//     stale-nonce predicate (same condition).
//   - NonceObserved: CycleID, At.
//   - HandoffFreshSeen: CycleID, Mtime, At (sampled by the shell at
//     handoff-timeout expiry, exactly where handoffWrittenAndFresh read today).
//   - ModelDone: CycleID, SessionID, Source, At.
//   - SessionChanged: CycleID, PrevSID, NewSID, At.
//   - TimerFired: CycleID, Timer, At.
//   - CrashJournal: Journal, At.
type Event struct {
	Kind           EventKind     `json:"kind"`
	At             time.Time     `json:"at"`
	CF             *CtxFile      `json:"cf,omitempty"`
	Gates          GateSnapshot  `json:"gates,omitempty"`
	CycleID        string        `json:"cycle_id,omitempty"`
	HandoffContent string        `json:"handoff_content,omitempty"`
	HandoffReadOK  bool          `json:"handoff_read_ok,omitempty"`
	Timer          TimerKind     `json:"timer,omitempty"`
	Mtime          time.Time     `json:"mtime,omitzero"`
	SessionID      string        `json:"session_id,omitempty"`
	Source         string        `json:"source,omitempty"`
	PrevSID        string        `json:"prev_sid,omitempty"`
	NewSID         string        `json:"new_sid,omitempty"`
	Journal        *CycleJournal `json:"journal,omitempty"`
}

// ActionKind discriminates the flat Action struct.
type ActionKind string

// The reactor→effector action vocabulary (design §3b / SK §6.3).
const (
	ActWriteJournal      ActionKind = "write_journal"
	ActTruncateHandoff   ActionKind = "truncate_handoff"
	ActSendEscape        ActionKind = "send_escape"
	ActInjectHandoffCmd  ActionKind = "inject_handoff_cmd"
	ActInjectClear       ActionKind = "inject_clear"
	ActInjectBrief       ActionKind = "inject_brief"
	ActSetTmuxEnv        ActionKind = "set_tmux_env"
	ActSetManagedSession ActionKind = "set_managed_session"
	ActClearPrecompact   ActionKind = "clear_precompact_marker"
	ActSetHold           ActionKind = "set_hold"
	ActEmit              ActionKind = "emit"
	ActArmTimer          ActionKind = "arm_timer"
	ActCancelTimer       ActionKind = "cancel_timer"
	ActForceRestart      ActionKind = "force_restart"
)

// Action is the flat, JSON-round-trippable reactor output (design §3b).
type Action struct {
	Kind    ActionKind     `json:"kind"`
	Journal CycleJournal   `json:"journal,omitzero"`   // WriteJournal — full contents (pure per §3d)
	CycleID string         `json:"cycle_id,omitempty"` // InjectHandoffCmd
	SID     string         `json:"sid,omitempty"`      // SetManagedSession ("" clears)
	Key     string         `json:"key,omitempty"`      // SetTmuxEnv
	Value   string         `json:"value,omitempty"`    // SetTmuxEnv
	Type    core.EventType `json:"type,omitempty"`     // Emit
	Payload []byte         `json:"payload,omitempty"`  // Emit — marshaled in Step (pure per §3d)
	Timer   TimerKind      `json:"timer,omitempty"`    // ArmTimer / CancelTimer
	D       time.Duration  `json:"d,omitempty"`        // ArmTimer
}

// CycleState is the full reactor state: the phase, the in-flight cycle
// fields, and every anti-loop/hysteresis field lifted verbatim from the
// pre-rebuild Cycler (design §3c). All timestamps are event-`At`-sourced.
type CycleState struct {
	Phase Phase

	// Anti-loop / hysteresis (formerly Cycler fields; semantics unchanged —
	// see the hk-vpnp / hk-qoz / hk-4f8 / hk-ibb / hk-hz9 / hk-4i0s comments
	// that traveled with each in cycle.go's history).
	LastFiredSID               string
	SeenLowPctAfterLastFire    bool
	LastFireWasAbort           bool
	LastForcedAttemptAt        time.Time
	LastOperatorAttachedEmit   time.Time
	LastIdleRestartAt          time.Time
	LastIdleCrewNotifiedSID    string
	ConsecutiveHandoffTimeouts int

	// Boot-grace tracking (hk-4f8, hk-ibb). SeenSessionIDs is updated
	// copy-on-write inside stepCycle so CycleState copies are value-safe
	// (the shell's ladder peek runs on a copy).
	CurrentSessionID      string
	CurrentSessionIDSince time.Time
	SeenSessionIDs        map[string]struct{}
	BootGraceFirstArmAt   time.Time

	// In-flight cycle fields (design §3c).
	CycleID           string
	EntryKind         EventKind // which entry event started the in-flight cycle
	EntryCF           CtxFile   // gauge reading at cycle entry (force math at abort)
	OpenedAt          time.Time
	InjectedAt        time.Time // handoff-injection anchor for the freshness recovery
	Reason            string    // "" | "handoff_timeout_recovered"
	HandoffFresh      bool      // set by HandoffFreshSeen before TimerFired(handoff_timeout)
	HandoffFreshMtime time.Time
	ClearAttempt      int    // 1-based settle-window counter (hk-vdqe2)
	PrevSID           string // session id being cleared

	// NonceConfirmedAt is t_nonce (SK-014 / design §5): the instant the handoff
	// was confirmed (NonceObserved, or the freshness-recovery TimerFired edge).
	// The shell's AwaitModelDone detection compares the .idle marker mtime /
	// assistant-transcript-turn timestamp against it — strict ≥, no CrispIdle
	// tolerance. Event-`At`-sourced, so it stays pure and replay-deterministic.
	NonceConfirmedAt time.Time

	// ModelDoneSource records which model-done signal was processed for the
	// in-flight cycle ("idle_marker" | "transcript_turn" | "timeout"); "" until
	// then. SR4's structural anchor (SK-INV-002): injectClearAction — the ONLY
	// ActInjectClear constructor — returns no action while this is empty, and
	// it is set exclusively by stepEnterClearing, the single AwaitModelDone →
	// Clearing edge.
	ModelDoneSource string

	// LastTerminal records the most recent terminal outcome ("complete" |
	// "aborted"); informational (the phase returns to Idle).
	LastTerminal string
}

// clone returns a value copy of the state. SeenSessionIDs is updated
// copy-on-write by stepCycle, so sharing the map here is safe.
func (s CycleState) clone() CycleState { return s }

// Cycle is the pure keeper reactor (the codexreactor.Reactor analog). It is
// NOT safe for concurrent use; like the pre-rebuild Cycler it is owned by the
// single watcher/shell goroutine.
type Cycle struct {
	cfg   *CyclerConfig // policy scalars + pure threshold math ONLY (no fn-field calls)
	state CycleState
}

// NewCycle constructs the reactor over the (defaulted) CyclerConfig scalars.
func NewCycle(cfg *CyclerConfig) *Cycle {
	return &Cycle{cfg: cfg, state: CycleState{Phase: PhaseIdle}}
}

// Step advances the machine: pure state transition + action emission.
func (m *Cycle) Step(ev Event) []Action {
	next, actions := stepCycle(m.cfg, m.state, ev)
	m.state = next
	return actions
}

// State returns a copy of the current reactor state (inspectable between
// steps, mirroring codexreactor).
func (m *Cycle) State() CycleState { return m.state.clone() }

// InCycle reports whether a cycle is in flight (Phase != Idle). The shell's
// InCycle suppression (SK-017 / D11) keys on this.
func (m *Cycle) InCycle() bool { return m.state.Phase != PhaseIdle }

// Run drives the reactor from a substrate EventSource into a substrate
// Effector — the one-line vertical wrapper over the free function (D1,
// mirroring codexreactor.Reactor.Run).
func (m *Cycle) Run(ctx context.Context, src substrate.EventSource[Event], eff substrate.Effector[Action]) error {
	return substrate.Run(ctx, src, m.Step, eff)
}

// peekFires reports whether ev would start a cycle, WITHOUT mutating state.
// The shell uses it to mint the cycle id (and sample the handoff content)
// only for entries that actually fire, keeping CycleIDGen call counts
// fire-aligned exactly as pre-rebuild (the generator was only invoked inside
// runCycle). Pure: runs the same total transition on a state copy.
func (m *Cycle) peekFires(ev Event) bool {
	next, _ := stepCycle(m.cfg, m.state.clone(), ev)
	return next.Phase != PhaseIdle
}

// failOpen rolls the machine back to Idle after the shell failed to execute
// the fatal opened-journal write (pre-rebuild: runCycle returned the error
// before any injection; lastForcedAttemptAt kept its fresh stamp, no
// anti-loop update). The idle-entry stamp unwind mirrors RunForIdle's
// post-call `if c.lastFireWasAbort { unwind }` check verbatim (hk-4i0s): at
// that point lastFireWasAbort still holds the PREVIOUS cycle's value.
func (m *Cycle) failOpen() {
	if m.state.EntryKind == EvIdleRestartTick && m.state.LastFireWasAbort {
		m.state.LastIdleRestartAt = time.Time{}
	}
	m.state.Phase = PhaseIdle
}

// stepCycle is the total pure transition function:
// (cfg, state, event) → (state', actions).
func stepCycle(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch s.Phase {
	case PhaseIdle:
		switch ev.Kind {
		case EvGaugeTick:
			return stepIdleGaugeTick(cfg, s, ev)
		case EvPrecompactTrigger:
			return stepIdlePrecompact(cfg, s, ev)
		case EvIdleRestartTick:
			return stepIdleRestartTick(cfg, s, ev)
		case EvCrashJournal:
			return stepIdleCrashJournal(cfg, s, ev)
		default:
			// any timer/detection event in Idle → ignored (no cycle in flight).
			return s, nil
		}
	case PhaseAwaitingHandoff:
		return stepAwaitingHandoff(cfg, s, ev)
	case PhaseAwaitModelDone:
		return stepAwaitModelDone(cfg, s, ev)
	case PhaseClearing:
		return stepClearing(cfg, s, ev)
	default:
		return s, nil
	}
}

// stepAwaitingHandoff handles the AwaitingHandoff phase (design §3c rows).
func stepAwaitingHandoff(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvNonceObserved:
		// Nonce confirmed → journal "confirmed", emit handoff_written
		// (SK-012, §4: the AwaitingHandoff → AwaitModelDone transition),
		// and await the model-done signal (SR4) under the fail-open
		// model_done_timeout bound (SK-014).
		s.Phase = PhaseAwaitModelDone
		s.NonceConfirmedAt = ev.At
		return s, []Action{
			journalAction(&s, "confirmed", ev.At),
			emitHandoffWrittenAction(cfg, s.CycleID, s.PrevSID, false, time.Time{}),
			{Kind: ActCancelTimer, Timer: TimerHandoffTimeout},
			{Kind: ActArmTimer, Timer: TimerModelDone, D: cfg.ModelDoneTimeout},
		}
	case EvHandoffFreshSeen:
		// The shell observed a fresh handoff (mtime ≥ injectedAt) at
		// handoff-timeout expiry; record it for the TimerFired edge.
		s.HandoffFresh = true
		s.HandoffFreshMtime = ev.Mtime
		return s, nil
	case EvTimerFired:
		if ev.Timer != TimerHandoffTimeout {
			return s, nil
		}
		if s.HandoffFresh {
			// hk-fi78d recovery: the nonce echo never landed but the agent
			// wrote a fresh, resumable handoff — proceed with /clear + brief.
			// A responsive-enough pane is NOT a stuck-pane timeout: reset the
			// escalation counter. handoff_written carries recovered:true +
			// the sampled handoff mtime (SK-012 / 00b R1).
			s.Reason = "handoff_timeout_recovered"
			s.ConsecutiveHandoffTimeouts = 0
			s.Phase = PhaseAwaitModelDone
			s.NonceConfirmedAt = ev.At
			return s, []Action{
				journalAction(&s, "confirmed", ev.At),
				emitHandoffWrittenAction(cfg, s.CycleID, s.PrevSID, true, s.HandoffFreshMtime),
				{Kind: ActArmTimer, Timer: TimerModelDone, D: cfg.ModelDoneTimeout},
			}
		}
		return stepAbort(cfg, s, ev)
	default:
		return s, nil
	}
}

// stepAwaitModelDone handles the AwaitModelDone phase (T8, SK-014).
func stepAwaitModelDone(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvModelDone:
		// The real model-done signal ("idle_marker" primary,
		// "transcript_turn" backstop — detected shell-side, §5).
		return stepEnterClearing(cfg, s, ev, ev.Source, false)
	case EvTimerFired:
		if ev.Timer != TimerModelDone {
			return s, nil
		}
		// Fail-open liveness bound (SK-014 / SR9): proceed to Clearing
		// anyway with model_done{source:"timeout", degraded:true} — the
		// degraded mode IS the pre-rebuild clear-immediately behavior, so
		// a lost .idle write can never wedge the cycle.
		return stepEnterClearing(cfg, s, ev, "timeout", true)
	default:
		return s, nil
	}
}

// stepClearing handles the Clearing phase (hk-vdqe2 hard gate).
func stepClearing(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvSessionChanged:
		if ev.NewSID == "" || ev.NewSID == s.PrevSID {
			return s, nil
		}
		// new_session_up (SK-012, §4: the Clearing → Briefing transition)
		// is emitted immediately BEFORE the managed-session rebind.
		actions := []Action{
			emitNewSessionUpAction(cfg, s.CycleID, s.PrevSID, ev.NewSID),
			{Kind: ActSetManagedSession, SID: ev.NewSID},
			{Kind: ActCancelTimer, Timer: TimerClearSettle},
			{Kind: ActCancelTimer, Timer: TimerClearBackstop},
		}
		return stepBriefing(cfg, s, ev, ev.NewSID, actions)
	case EvTimerFired:
		switch ev.Timer {
		case TimerClearSettle:
			return stepClearSettleExpired(cfg, s, ev)
		case TimerClearBackstop:
			return stepClearUnconfirmed(cfg, s, ev)
		default:
			return s, nil
		}
	default:
		return s, nil
	}
}

// stepClearSettleExpired is the Clearing settle-window expiry (hk-vdqe2 hard
// gate): retry the settle window (defensively re-injecting /clear) until
// retries are exhausted; the shell fires TimerClearBackstop instead when the
// wall-clock backstop has also elapsed (matching the pre-rebuild deadline
// check at each settle-window end).
func stepClearSettleExpired(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	if s.ClearAttempt >= cfg.ClearConfirmRetries {
		return stepClearUnconfirmed(cfg, s, ev)
	}
	s.ClearAttempt++
	var actions []Action
	if cfg.TmuxTarget != "" {
		// hk-u7j83: SUPPRESS the defensive /clear re-inject when the settle-expiry
		// gauge shows the context ALREADY dropped below the act threshold — the
		// /clear landed (the implicit gauge signal, hk-zj1y/hk-1ryc). Re-injecting
		// then would be a spurious extra /clear and break the exactly-one-/clear
		// safety property in the self-restart-races-the-auto-cycle case: the agent's
		// own `restart-now` /clear drops the gauge, and the keeper must not pile on
		// 19 more /clears while waiting for the new session_id to surface. The
		// re-inject is KEPT (hk-vdqe2's defense) only when the pane still reads high
		// — a nil CF (gauge unreadable → fail defensive) or above the act threshold
		// — i.e. the /clear was not consumed (the busy-pane case hk-vdqe2 targets).
		// The retry/backstop budget is unchanged: the settle window still re-arms
		// below, so the cycle keeps polling for the new sid and still bounds the
		// wait; only the redundant re-inject is dropped.
		gaugeDropped := ev.CF != nil && cfg.belowActThreshold(ev.CF)
		if !gaugeDropped {
			// Each defensive re-inject re-emits clear_sent with the
			// incremented attempt (SK-012 — makes the unconfirmed
			// forensics replayable).
			if clearAct, ok := injectClearAction(&s); ok {
				actions = append(actions, clearAct,
					emitClearSentAction(cfg, s.CycleID, s.PrevSID, s.ClearAttempt))
			}
		}
	}
	actions = append(actions, Action{Kind: ActArmTimer, Timer: TimerClearSettle, D: cfg.ClearSettle})
	return s, actions
}

// ─── Idle entries ────────────────────────────────────────────────────────────

// stepIdleGaugeTick is the MaybeRun 11-gate ladder (SK-011): a pure predicate
// over the event-carried GateSnapshot with the UNCONDITIONAL prelude (re-arm
// observation, same-SID escape hatch, boot-grace SID tracking) running before
// gating — on the fail path too (§3f: a "clean" short-circuit would change
// observable state). Gate order preserved exactly.
func stepIdleGaugeTick(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	cf := ev.CF
	snap := ev.Gates

	// Gate 1: .managed opt-in (DEFECT-3).
	if !snap.Managed {
		return s, nil
	}
	// Gate 2: nil/empty session_id → cannot establish anti-loop identity
	// (DEFECT-1). A CF-less gauge tick likewise cannot establish identity;
	// nil-guard cf exactly as the precompact entry (stepIdlePrecompact) does so
	// a CF-less event can never deref-panic the keeper here.
	if cf == nil || cf.SessionID == "" {
		return s, nil
	}

	// Preludes (unconditional): re-arm observation + same-SID escape hatch,
	// then boot-grace SID tracking (hk-4f8, hk-ibb, hk-hz9).
	s = applyAntiLoopPrelude(cfg, s, cf)
	s = trackBootGraceSID(cfg, s, ev.At, cf.SessionID)

	// Boot-grace gate (force-path exempt — hk-ibb fix 1; total ceiling — fix 2).
	if bootGraceHolds(cfg, s, ev.At, cf) {
		return s, nil
	}

	// Gate 3: act threshold.
	if cfg.belowActThreshold(cf) {
		return s, nil
	}
	// Gate 4: CrispIdle unless above the hard force threshold (hk-0uu).
	if !snap.CrispIdle && !cfg.aboveForceThreshold(cf) {
		return s, nil
	}
	// Gate 5: no in-flight queue work (fail-closed).
	if snap.HoldingDispatch {
		return s, nil
	}
	// Gate 5b: session sleeping (M3 / hk-l3gs).
	if cf.SessionID != "" && snap.Sleeping {
		return s, nil
	}
	// Gate 5c: operator HOLD (D5 / hk-9waz).
	if snap.Held {
		return s, nil
	}
	// Gate 5d: auto-hold on a recent inbound operator user turn (hk-74iyd).
	// Prelude-class side effect on the FAIL path: the SetHold marker write.
	if gateOperatorTurnHolds(cfg, snap, ev.At, cf.SessionID) {
		return s, []Action{{Kind: ActSetHold}}
	}
	// Gate 5e: post-answer grace (hk-74iyd) — transient tick-level deferral.
	if gatePostAnswerGraceHolds(cfg, snap, ev.At, cf.SessionID) {
		return s, nil
	}
	// Gate 6: full anti-loop suppression + force-retry exceptions (hk-qoz, hk-hz9 fix 2).
	if gateAntiLoopSuppresses(cfg, s, ev.At, cf) {
		return s, nil
	}
	// Gate 7: operator-attached guard (warn-only, hk-6qf). The emission is a
	// deliberate NO-OP (logmine TA3/F55 — do NOT resurrect); only the
	// once-per-interval sampling state advances (hk-2yvx).
	if snap.OperatorAttached {
		return gateOperatorAttachedSample(cfg, s, ev.At), nil
	}

	return stepStartCycle(cfg, s, ev, cf)
}

// gateOperatorTurnHolds is the Gate 5d predicate: a recent inbound operator
// user turn within the lookback window (hk-74iyd).
func gateOperatorTurnHolds(cfg *CyclerConfig, snap GateSnapshot, at time.Time, sid string) bool {
	return cfg.OperatorTurnLookback > 0 && sid != "" && !snap.LastUserTurnAt.IsZero() &&
		at.Sub(snap.LastUserTurnAt) <= cfg.OperatorTurnLookback
}

// gatePostAnswerGraceHolds is the Gate 5e predicate: within the post-answer
// grace window after the last assistant turn (hk-74iyd).
func gatePostAnswerGraceHolds(cfg *CyclerConfig, snap GateSnapshot, at time.Time, sid string) bool {
	return cfg.PostAnswerGrace > 0 && sid != "" && !snap.LastAssistantTurnAt.IsZero() &&
		at.Sub(snap.LastAssistantTurnAt) <= cfg.PostAnswerGrace
}

// gateOperatorAttachedSample advances Gate 7's once-per-interval sampling
// state (hk-2yvx). The emission itself is a deliberate NO-OP (logmine
// TA3/F55 — do NOT resurrect).
func gateOperatorAttachedSample(cfg *CyclerConfig, s CycleState, at time.Time) CycleState {
	if s.LastOperatorAttachedEmit.IsZero() ||
		at.Sub(s.LastOperatorAttachedEmit) >= cfg.OperatorAttachedSampleInterval {
		s.LastOperatorAttachedEmit = at
	}
	return s
}

// applyAntiLoopPrelude is the UNCONDITIONAL anti-loop prelude shared by the
// GaugeTick and Precompact entries (§3f: a "clean" short-circuit would change
// observable state): the re-arm observation, then the same-SID escape hatch
// (hk-uxu) gated on the last fire having COMPLETED (hk-vpnp / Bug 3a).
func applyAntiLoopPrelude(cfg *CyclerConfig, s CycleState, cf *CtxFile) CycleState {
	// Prelude: re-arm observation.
	if s.LastFiredSID != "" && cf.SessionID != s.LastFiredSID && cfg.belowWarnThreshold(cf) {
		s.SeenLowPctAfterLastFire = true
	}
	// Prelude: same-SID anti-loop escape hatch.
	if s.LastFiredSID != "" && cf.SessionID == s.LastFiredSID &&
		!s.LastFireWasAbort && cfg.belowWarnThreshold(cf) {
		s.LastFiredSID = ""
		s.SeenLowPctAfterLastFire = false
		s.LastFireWasAbort = false
	}
	return s
}

// trackBootGraceSID is the boot-grace SID-tracking prelude (hk-4f8, hk-ibb,
// hk-hz9): stamp CurrentSessionIDSince on a genuinely-new session id and
// maintain the grace-burst window anchor.
func trackBootGraceSID(cfg *CyclerConfig, s CycleState, at time.Time, sid string) CycleState {
	if sid == s.CurrentSessionID {
		return s
	}
	if s.CurrentSessionID != "" {
		if _, alreadySeen := s.SeenSessionIDs[sid]; !alreadySeen {
			s.CurrentSessionIDSince = at
			if s.BootGraceFirstArmAt.IsZero() ||
				(cfg.MaxBootGraceTotal > 0 && at.Sub(s.BootGraceFirstArmAt) >= cfg.MaxBootGraceTotal) {
				s.BootGraceFirstArmAt = at
			}
		}
	}
	// Copy-on-write so state copies (the shell peek) never alias.
	next := make(map[string]struct{}, len(s.SeenSessionIDs)+1)
	for k := range s.SeenSessionIDs {
		next[k] = struct{}{}
	}
	next[sid] = struct{}{}
	s.SeenSessionIDs = next
	s.CurrentSessionID = sid
	return s
}

// bootGraceHolds is the boot-grace gate predicate shared by the GaugeTick and
// Precompact entries: true when the grace window is still holding the entry
// back. Force-path exempt (hk-ibb fix 1; a nil cf is NOT exempt — the
// precompact entry's pre-rebuild check); the total ceiling (fix 2) overrides
// the hold.
func bootGraceHolds(cfg *CyclerConfig, s CycleState, at time.Time, cf *CtxFile) bool {
	if cfg.BootGracePeriod <= 0 || s.CurrentSessionIDSince.IsZero() {
		return false
	}
	if cf != nil && cfg.aboveForceThreshold(cf) {
		return false
	}
	if at.Sub(s.CurrentSessionIDSince) >= cfg.BootGracePeriod {
		return false
	}
	totalExceeded := cfg.MaxBootGraceTotal > 0 &&
		!s.BootGraceFirstArmAt.IsZero() &&
		at.Sub(s.BootGraceFirstArmAt) >= cfg.MaxBootGraceTotal
	return !totalExceeded
}

// gateAntiLoopSuppresses is Gate 6 of the MaybeRun ladder: full anti-loop
// suppression + the force-retry exceptions (hk-qoz, hk-hz9 fix 2). true means
// the entry is suppressed; false falls through (including the forced-clear
// retry paths).
func gateAntiLoopSuppresses(cfg *CyclerConfig, s CycleState, at time.Time, cf *CtxFile) bool {
	if s.LastFiredSID == "" {
		return false
	}
	if cf.SessionID == s.LastFiredSID {
		if !cfg.aboveForceThreshold(cf) {
			return true
		}
		// Retry the forced-clear once the retry interval has elapsed.
		return !s.LastForcedAttemptAt.IsZero() && at.Sub(s.LastForcedAttemptAt) < cfg.ForceRetryInterval
	}
	if !s.SeenLowPctAfterLastFire {
		if !cfg.aboveForceThreshold(cf) {
			return true
		}
		// Retry the forced-clear once the retry interval has elapsed.
		return !s.LastForcedAttemptAt.IsZero() && at.Sub(s.LastForcedAttemptAt) < cfg.ForceRetryInterval
	}
	return false
}

// stepIdlePrecompact is the RunForPrecompact entry ladder (gate subset; skips
// CrispIdle and the act threshold). The .precompact marker is ALWAYS cleared
// whichever gate fires (bounded-fallback contract), and every decision emits
// session_keeper_precompact_blocked with the action taken — including the
// empty-SID "hold_dispatch_skip" quirk (design §3c Idle rows / cycle.go:1349).
func stepIdlePrecompact(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	cf := ev.CF
	sessionID := ""
	if cf != nil {
		sessionID = cf.SessionID
	}
	snap := ev.Gates

	blocked := func(action string) []Action {
		return []Action{
			emitPrecompactBlockedAction(cfg, sessionID, action),
			{Kind: ActClearPrecompact},
		}
	}

	// Gate 1: .managed opt-in.
	if !snap.Managed {
		return s, blocked("not_managed")
	}
	// Gate 2: empty session_id (the hold_dispatch_skip quirk is pre-rebuild
	// behavior, preserved verbatim).
	if sessionID == "" {
		return s, blocked("hold_dispatch_skip")
	}
	// Gate 2b: boot-grace (hk-hz9 fix 3) — state kept current by the GaugeTick
	// prelude, which the watcher always delivers first. Force-path exempt.
	if bootGraceHolds(cfg, s, ev.At, cf) {
		return s, blocked("boot_grace")
	}

	// Preludes: re-arm observation + same-SID escape hatch (mirrors the
	// GaugeTick prelude; gated on !LastFireWasAbort — hk-vpnp).
	if cf != nil {
		s = applyAntiLoopPrelude(cfg, s, cf)
	}

	// Gate 3: HoldingDispatch (fail-closed).
	if snap.HoldingDispatch {
		return s, blocked("hold_dispatch_skip")
	}
	// Gate 3b: operator HOLD (hk-4rago).
	if snap.Held {
		return s, blocked("hold_skip")
	}
	// Gate 4: anti-loop suppression.
	if s.LastFiredSID != "" {
		if sessionID == s.LastFiredSID || !s.SeenLowPctAfterLastFire {
			return s, blocked("anti_loop_suppressed")
		}
	}
	// Gate 5: operator-attached (the second emitOperatorAttached is the
	// deliberate NO-OP — nothing additional to emit).
	if snap.OperatorAttached {
		return s, blocked("operator_attached")
	}

	// All gates passed.
	actions := blocked("cycle_triggered")
	if cf == nil {
		cf = &CtxFile{SessionID: sessionID}
	}
	next, startActions := stepStartCycle(cfg, s, ev, cf)
	return next, append(actions, startActions...)
}

// stepIdleRestartTick is the RunForIdle entry ladder (hk-ee81): restart idle
// crews with large (≥ IdleRestartAbsTokens) context below the act threshold.
func stepIdleRestartTick(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	cf := ev.CF
	if cf == nil {
		return s, nil
	}
	sessionID := cf.SessionID
	snap := ev.Gates

	// Gate 2: below idle-restart floor → notify once per session_id (hk-qshh8).
	if cf.Tokens < cfg.IdleRestartAbsTokens {
		if cf.Tokens > 0 && sessionID != s.LastIdleCrewNotifiedSID {
			payload := mustMarshalPayload(map[string]any{
				"agent":  cfg.AgentName,
				"tokens": cf.Tokens,
				"reason": "below_idle_threshold",
			})
			s.LastIdleCrewNotifiedSID = sessionID
			return s, []Action{{Kind: ActEmit, Type: core.EventTypeSessionKeeperIdleCrew, Payload: payload}}
		}
		return s, nil
	}
	// Gate 3: at/above act threshold → MaybeRun's ladder owns it.
	if !cfg.belowActThreshold(cf) {
		return s, nil
	}
	// Gate 4: pane quiescent.
	if !snap.CrispIdle {
		return s, nil
	}
	// Gate 5: no in-flight dispatch (fail-closed).
	if snap.HoldingDispatch {
		return s, nil
	}
	// Gate 5b: operator HOLD (hk-4rago).
	if snap.Held {
		return s, nil
	}
	// Gate 6: cooldown.
	if !s.LastIdleRestartAt.IsZero() && ev.At.Sub(s.LastIdleRestartAt) < cfg.IdleRestartCooldown {
		return s, nil
	}
	// Gate 7: anti-loop.
	if s.LastFiredSID != "" && sessionID == s.LastFiredSID {
		return s, nil
	}

	// Stamp the cooldown BEFORE the cycle; the Aborted terminal unwinds it so
	// a failed idle restart can retry on the next tick (hk-4i0s).
	s.LastIdleRestartAt = ev.At
	return stepStartCycle(cfg, s, ev, cf)
}

// stepIdleCrashJournal is the boot-time RecoverFromCrash matrix (design §3c;
// cycle.go RecoverFromCrash) fed as the CrashJournal event. It is a one-shot
// fast-forward/close-out: no anti-loop update, no managed rebind, no drive
// loop — exactly the pre-rebuild matrix.
func stepIdleCrashJournal(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	j := ev.Journal
	if j == nil {
		return s, nil
	}
	switch j.Phase {
	case "cleared":
		// /clear was issued before the crash: inject the brief to complete the
		// interrupted cycle (I1 identity re-pin), close the journal, emit
		// cycle_recovered.
		var actions []Action
		if cfg.TmuxTarget != "" {
			actions = append(actions, Action{Kind: ActInjectBrief})
		}
		done := *j
		done.Phase = "complete"
		done.UpdatedAt = ev.At.UTC()
		done.Reason = "recovered_from_crash"
		actions = append(actions,
			Action{Kind: ActWriteJournal, Journal: done},
			emitCycleRecoveredAction(cfg, j.CycleID, "cleared"),
		)
		return s, actions
	case "resumed":
		done := *j
		done.Phase = "complete"
		done.UpdatedAt = ev.At.UTC()
		done.Reason = "recovered_from_crash"
		return s, []Action{
			{Kind: ActWriteJournal, Journal: done},
			emitCycleRecoveredAction(cfg, j.CycleID, "resumed"),
		}
	case "opened", "handoff_injected", "confirmed":
		// /clear was NOT issued; abort the journal safely (no injection).
		done := *j
		done.Phase = "aborted"
		done.UpdatedAt = ev.At.UTC()
		done.Reason = "crash_before_clear"
		return s, []Action{{Kind: ActWriteJournal, Journal: done}}
	default:
		// "complete" / "aborted" — terminal; nothing to recover.
		return s, nil
	}
}

// ─── Cycle start / abort / clear / brief ─────────────────────────────────────

// stepStartCycle opens a cycle: the ladder passed on an entry event. The
// cycle id was minted by the shell (ev.CycleID); the handoff content sample
// rides on the event for the pure stale-nonce truncate decision (hk-vpnp
// Bug 3b: truncate ONLY a stale keeper nonce; preserve a genuine handoff).
func stepStartCycle(cfg *CyclerConfig, s CycleState, ev Event, cf *CtxFile) (CycleState, []Action) {
	// Forced-attempt stamp BEFORE injection so Gate 6 rate-limits retries
	// whether this cycle completes or aborts (hk-qoz).
	if cfg.aboveForceThreshold(cf) {
		s.LastForcedAttemptAt = ev.At
	}

	s.Phase = PhaseAwaitingHandoff
	s.CycleID = ev.CycleID
	s.EntryKind = ev.Kind
	s.EntryCF = *cf
	s.OpenedAt = ev.At.UTC()
	s.InjectedAt = ev.At
	s.Reason = ""
	s.HandoffFresh = false
	s.HandoffFreshMtime = time.Time{}
	s.ClearAttempt = 0
	s.PrevSID = cf.SessionID
	s.NonceConfirmedAt = time.Time{}
	s.ModelDoneSource = "" // SR4: /clear is unconstructible until model-done

	actions := []Action{
		// Journal "opened" BEFORE any injection. This is the ONE fatal journal
		// write (pre-rebuild runCycle returned its error): the effector
		// propagates a failure and the shell rolls back via failOpen.
		journalAction(&s, "opened", ev.At),
		emitHandoffStartedAction(cfg, s.CycleID, cf.SessionID),
	}
	// Clear a STALE keeper nonce from a prior cycle so it cannot pre-satisfy
	// the poll (DEFECT-2); a genuine handoff with no keeper nonce is preserved.
	if ev.HandoffReadOK && handoffContentHasStaleNonce(ev.HandoffContent, nonceMarker(s.CycleID)) {
		actions = append(actions, Action{Kind: ActTruncateHandoff})
	}
	if cfg.TmuxTarget != "" {
		actions = append(actions,
			Action{Kind: ActSendEscape},
			Action{Kind: ActInjectHandoffCmd, CycleID: s.CycleID},
		)
	}
	actions = append(actions,
		journalAction(&s, "handoff_injected", ev.At),
		Action{Kind: ActArmTimer, Timer: TimerHandoffTimeout, D: cfg.HandoffTimeout},
	)
	return s, actions
}

// stepAbort is the AwaitingHandoff handoff_timeout edge with NO fresh handoff
// — the ONLY path that never sends /clear (SK §8.2). NEVER /clear an
// unconfirmed handoff (hk-vpnp Bug 3).
func stepAbort(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	actions := []Action{
		journalAbortedAction(&s, ev.At),
		emitCycleAbortedAction(cfg, s.CycleID, s.EntryCF.SessionID, "handoff_timeout"),
	}
	// DEFECT-4: record suppression on abort; hk-vpnp Bug 3a: mark as ABORT so
	// the same-SID escape hatch does not re-arm on a post-abort gauge dip.
	s.LastFiredSID = s.EntryCF.SessionID
	s.SeenLowPctAfterLastFire = false
	s.LastFireWasAbort = true

	// Re-arm: clear .managed so the .sid channel can rebind — ONLY when a real
	// session-id change was previously observed (hk-ibb fix 3).
	if !s.CurrentSessionIDSince.IsZero() {
		actions = append(actions, Action{Kind: ActSetManagedSession, SID: ""})
	}

	// Escalation: consecutive timeouts above the force threshold march toward
	// ForceRestart (hk-qoz). The counter only resets on the escalation call
	// when a respawn port is actually wired (pre-rebuild: respawn != nil).
	if cfg.aboveForceThreshold(&s.EntryCF) {
		s.ConsecutiveHandoffTimeouts++
		if cfg.hasRespawn && cfg.MaxHandoffTimeouts > 0 &&
			s.ConsecutiveHandoffTimeouts >= cfg.MaxHandoffTimeouts {
			actions = append(actions, Action{Kind: ActForceRestart})
			s.ConsecutiveHandoffTimeouts = 0
		}
	} else {
		s.ConsecutiveHandoffTimeouts = 0
	}

	// hk-4i0s: an idle-entry cycle that aborted issued no /clear — unwind the
	// cooldown stamp so the next tick can retry.
	if s.EntryKind == EvIdleRestartTick {
		s.LastIdleRestartAt = time.Time{}
	}

	s.Phase = PhaseIdle
	s.LastTerminal = "aborted"
	return s, actions
}

// stepEnterClearing is the SINGLE AwaitModelDone → Clearing transition (the
// only entry into Clearing): record the processed model-done signal, emit
// model_done{source[, degraded]} (SK-012), set HARMONIK_AGENT, inject /clear
// + emit clear_sent{attempt:1}, journal "cleared", cancel the model-done
// bound, and arm the settle + backstop timers (hk-vdqe2 hard gate).
func stepEnterClearing(cfg *CyclerConfig, s CycleState, ev Event, source string, degraded bool) (CycleState, []Action) {
	s.Phase = PhaseClearing
	s.ClearAttempt = 1
	// SR4 (SK-014 / SK-INV-002): record the model-done signal BEFORE any
	// injectClearAction call — until this field is set, the /clear action
	// cannot be constructed anywhere in the reactor.
	s.ModelDoneSource = source
	actions := []Action{emitModelDoneAction(cfg, s.CycleID, s.PrevSID, source, degraded)}
	if cfg.TmuxTarget != "" {
		actions = append(actions,
			Action{Kind: ActSetTmuxEnv, Key: "HARMONIK_AGENT", Value: cfg.AgentName},
		)
		if clearAct, ok := injectClearAction(&s); ok {
			actions = append(actions, clearAct,
				emitClearSentAction(cfg, s.CycleID, s.PrevSID, s.ClearAttempt))
		}
	}
	actions = append(actions,
		journalAction(&s, "cleared", ev.At),
		Action{Kind: ActCancelTimer, Timer: TimerModelDone},
		// Backstop deadline first (pre-rebuild: computed at the wrapper entry,
		// before the first settle window), then the per-attempt settle window.
		Action{Kind: ActArmTimer, Timer: TimerClearBackstop, D: cfg.ClearConfirmBackstop},
		Action{Kind: ActArmTimer, Timer: TimerClearSettle, D: cfg.ClearSettle},
	)
	return s, actions
}

// injectClearAction is the ONLY constructor of an ActInjectClear action in
// the pure reactor. SR4 (SK-014, "/clear MUST NOT be injected before
// model-done") is enforced STRUCTURALLY here, not by call-site discipline:
// the action cannot be built while the in-flight cycle's ModelDoneSource is
// unset, and ModelDoneSource is set exclusively by stepEnterClearing — the
// single AwaitModelDone → Clearing edge, reached only by consuming EvModelDone
// or the model_done_timeout fail-open TimerFired. A Step ordering that emits
// InjectClear before processing a model-done event for the cycle is therefore
// unrepresentable. (SR3 rides along: AwaitModelDone is reachable only via the
// two handoff_written edges, and the abort path never clears — SK-INV-001.)
func injectClearAction(s *CycleState) (Action, bool) {
	if s.ModelDoneSource == "" {
		return Action{}, false
	}
	return Action{Kind: ActInjectClear}, true
}

// stepClearUnconfirmed is the Clearing backstop-exhaustion outcome: emit
// clear_unconfirmed, clear the managed binding, then fall through to the
// brief. NOT a terminal by itself — the brief still fires and the cycle still
// records cycle_complete (SK §8.3).
func stepClearUnconfirmed(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	actions := []Action{
		emitClearUnconfirmedAction(cfg, s.CycleID, s.EntryCF.SessionID),
		{Kind: ActSetManagedSession, SID: ""},
		{Kind: ActCancelTimer, Timer: TimerClearSettle},
		{Kind: ActCancelTimer, Timer: TimerClearBackstop},
	}
	return stepBriefing(cfg, s, ev, "", actions)
}

// stepBriefing is the immediate Briefing entry (no external event): inject
// the brief, journal resumed + complete, emit cycle_complete (and
// cycle_recovered on the hk-fi78d recovery path), run the anti-loop
// bookkeeping, and return to Idle with the Complete terminal.
func stepBriefing(cfg *CyclerConfig, s CycleState, ev Event, newSID string, actions []Action) (CycleState, []Action) {
	s.Phase = PhaseBriefing // transient; lands at Idle below
	if cfg.TmuxTarget != "" {
		actions = append(actions, Action{Kind: ActInjectBrief})
	}
	actions = append(actions,
		journalAction(&s, "resumed", ev.At),
		journalCompleteAction(&s, ev.At),
		emitCycleCompleteAction(cfg, s.CycleID, s.EntryCF.SessionID, newSID),
	)
	if s.Reason != "" {
		actions = append(actions, emitCycleRecoveredAction(cfg, s.CycleID, "handoff_timeout"))
	}

	// Anti-loop: suppress this session until BOTH a new session_id and a
	// below-warn reading on it are observed. This fire COMPLETED (/clear ran),
	// so the same-SID escape hatch may legitimately re-arm (hk-vpnp Bug 3a).
	s.LastFiredSID = s.EntryCF.SessionID
	s.SeenLowPctAfterLastFire = false
	s.LastFireWasAbort = false
	// Successful cycle: reset the escalation counter and the grace burst
	// window (hk-hz9 fix 1).
	s.ConsecutiveHandoffTimeouts = 0
	s.BootGraceFirstArmAt = time.Time{}

	s.Phase = PhaseIdle
	s.LastTerminal = "complete"
	return s, actions
}

// ─── Pure helpers ────────────────────────────────────────────────────────────

// journalAction builds a WriteJournal action carrying the FULL journal
// contents (design §3d: journal struct contents are pure). Reason carries the
// in-flight cycle reason ("" on the clean path, "handoff_timeout_recovered"
// from the recovery edge onward) — byte-identical to the pre-rebuild j
// mutation flow.
func journalAction(s *CycleState, phase string, at time.Time) Action {
	return Action{Kind: ActWriteJournal, Journal: CycleJournal{
		CycleID:   s.CycleID,
		Phase:     phase,
		OpenedAt:  s.OpenedAt,
		UpdatedAt: at.UTC(),
		Reason:    s.Reason,
	}}
}

// journalAbortedAction is the abort journal write (Reason "handoff_timeout").
func journalAbortedAction(s *CycleState, at time.Time) Action {
	return Action{Kind: ActWriteJournal, Journal: CycleJournal{
		CycleID:   s.CycleID,
		Phase:     "aborted",
		OpenedAt:  s.OpenedAt,
		UpdatedAt: at.UTC(),
		Reason:    "handoff_timeout",
	}}
}

// journalCompleteAction is the final journal write; Reason is "" on the clean
// path and "handoff_timeout_recovered" on the recovery path.
func journalCompleteAction(s *CycleState, at time.Time) Action {
	return Action{Kind: ActWriteJournal, Journal: CycleJournal{
		CycleID:   s.CycleID,
		Phase:     "complete",
		OpenedAt:  s.OpenedAt,
		UpdatedAt: at.UTC(),
		Reason:    s.Reason,
	}}
}

// handoffContentHasStaleNonce is the pure form of the pre-rebuild
// Cycler.handoffHasStaleNonce: given the handoff file content, report whether
// it carries a keeper nonce from some OTHER (prior) cycle (hk-vpnp).
func handoffContentHasStaleNonce(content, currentNonce string) bool {
	if !strings.Contains(content, nonceMarkerPrefix) {
		return false // no keeper nonce at all → genuine handoff; preserve it
	}
	return !isOnlyNonce(content, currentNonce)
}

// ─── Pure emit-payload builders (design §3d: payload construction is pure) ──

// mustMarshalPayload marshals a keeper event payload. Every caller passes a
// fixed struct (or map) of scalar fields, so json.Marshal cannot fail; the sole
// justified errcheck suppression in the payload-builder path lives here rather
// than scattered across each builder.
func mustMarshalPayload(v any) []byte {
	raw, _ := json.Marshal(v) //nolint:errcheck,errchkjson // callers pass only fixed scalar-field payload structs/maps, which never fail to marshal; empty bytes on the impossible error
	return raw
}

func emitHandoffStartedAction(cfg *CyclerConfig, cycleID, sessionID string) Action {
	raw := mustMarshalPayload(core.SessionKeeperHandoffStartedPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperHandoffStarted, Payload: raw}
}

func emitCycleCompleteAction(cfg *CyclerConfig, cycleID, prevSID, newSID string) Action {
	raw := mustMarshalPayload(core.SessionKeeperCycleCompletePayload{
		AgentName:     cfg.AgentName,
		CycleID:       cycleID,
		PrevSessionID: prevSID,
		NewSessionID:  newSID,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperCycleComplete, Payload: raw}
}

func emitCycleAbortedAction(cfg *CyclerConfig, cycleID, sessionID, reason string) Action {
	raw := mustMarshalPayload(core.SessionKeeperCycleAbortedPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Reason:    reason,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperCycleAborted, Payload: raw}
}

func emitClearUnconfirmedAction(cfg *CyclerConfig, cycleID, sessionID string) Action {
	raw := mustMarshalPayload(core.SessionKeeperClearUnconfirmedPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperClearUnconfirmed, Payload: raw}
}

func emitCycleRecoveredAction(cfg *CyclerConfig, cycleID, phaseAtCrash string) Action {
	raw := mustMarshalPayload(core.SessionKeeperCycleRecoveredPayload{
		AgentName:    cfg.AgentName,
		CycleID:      cycleID,
		PhaseAtCrash: phaseAtCrash,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperCycleRecovered, Payload: raw}
}

// ─── The four §8.20 interior-event builders (T8, SK-012; payloads pinned by
// 00b R1/R2). All carry agent_name + the REQUIRED cycle_id; the envelope
// run_id stays absent (D7 — the shell's effector passes core.RunID{}).

// emitHandoffWrittenAction builds session_keeper_handoff_written. On the
// nonce path the confirmed nonce marker is carried for audit; on the
// hk-fi78d freshness-recovery edge recovered:true + the sampled handoff
// mtime (RFC3339) are carried instead (00b R1 union shape).
func emitHandoffWrittenAction(cfg *CyclerConfig, cycleID, sessionID string, recovered bool, handoffMtime time.Time) Action {
	p := core.SessionKeeperHandoffWrittenPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
	}
	if recovered {
		p.Recovered = true
		p.HandoffMtime = handoffMtime.UTC().Format(time.RFC3339)
	} else {
		p.Nonce = nonceMarker(cycleID)
	}
	raw := mustMarshalPayload(p)
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperHandoffWritten, Payload: raw}
}

// emitModelDoneAction builds session_keeper_model_done. Source is REQUIRED
// ("idle_marker" | "transcript_turn" | "timeout"); degraded is true only on
// the model_done_timeout fail-open path (omitempty per 00b R2).
func emitModelDoneAction(cfg *CyclerConfig, cycleID, sessionID, source string, degraded bool) Action {
	raw := mustMarshalPayload(core.SessionKeeperModelDonePayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Source:    source,
		Degraded:  degraded,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperModelDone, Payload: raw}
}

// emitClearSentAction builds session_keeper_clear_sent (attempt is 1-based;
// defensive re-injects increment it).
func emitClearSentAction(cfg *CyclerConfig, cycleID, sessionID string, attempt int) Action {
	raw := mustMarshalPayload(core.SessionKeeperClearSentPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Attempt:   attempt,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperClearSent, Payload: raw}
}

// emitNewSessionUpAction builds session_keeper_new_session_up (prev/new both
// REQUIRED and distinct — the pure SessionChanged guard already enforces the
// != check, matching the payload's Valid()).
func emitNewSessionUpAction(cfg *CyclerConfig, cycleID, prevSID, newSID string) Action {
	raw := mustMarshalPayload(core.SessionKeeperNewSessionUpPayload{
		AgentName:     cfg.AgentName,
		CycleID:       cycleID,
		PrevSessionID: prevSID,
		NewSessionID:  newSID,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperNewSessionUp, Payload: raw}
}

func emitPrecompactBlockedAction(cfg *CyclerConfig, sessionID, action string) Action {
	raw := mustMarshalPayload(core.SessionKeeperPrecompactBlockedPayload{
		AgentName: cfg.AgentName,
		SessionID: sessionID,
		Action:    action,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperPrecompactBlocked, Payload: raw}
}
