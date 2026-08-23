package keeper

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

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
	EvGaugeTick          EventKind = "gauge_tick"
	EvPrecompactTrigger  EventKind = "precompact_trigger"
	EvIdleRestartTick    EventKind = "idle_restart_tick"
	EvNonceObserved      EventKind = "nonce_observed"
	EvHandoffFreshSeen   EventKind = "handoff_fresh_seen"
	EvPendingHandoffSeen EventKind = "pending_handoff_seen"
	EvOperatorTurnRecent EventKind = "operator_turn_recent"
	EvModelDone          EventKind = "model_done"
	EvSessionChanged     EventKind = "session_changed"
	EvTimerFired         EventKind = "timer_fired"
	EvCrashJournal       EventKind = "crash_journal"
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
//   - OperatorTurnRecent: CycleID, At.
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
	ActWriteJournal ActionKind = "write_journal"
	// ActTruncateHandoff SCRUBS the stale keeper nonce marker(s) out of the
	// handoff file; the crew's handoff body is preserved. Name retained for
	// compatibility with the on-disk/action vocabulary (hk-4tjyj).
	ActTruncateHandoff   ActionKind = "truncate_handoff"
	ActSendEscape        ActionKind = "send_escape"
	ActInjectHandoffCmd  ActionKind = "inject_handoff_cmd"
	ActInjectClear       ActionKind = "inject_clear"
	ActInjectBrief       ActionKind = "inject_brief"
	ActSetTmuxEnv        ActionKind = "set_tmux_env"
	ActSetManagedSession ActionKind = "set_managed_session"
	ActClearPrecompact   ActionKind = "clear_precompact_marker"
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
	// "aborted" | "parked"); informational (the phase returns to Idle).
	LastTerminal string
}

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

func (m *Cycle) peekFires(ev Event) bool {
	next, _ := stepCycle(m.cfg, m.state.clone(), ev)
	return next.Phase != PhaseIdle
}

func (m *Cycle) failOpen() {
	if m.state.EntryKind == EvIdleRestartTick && m.state.LastFireWasAbort {
		m.state.LastIdleRestartAt = time.Time{}
	}
	m.state.Phase = PhaseIdle
}

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
		case EvPendingHandoffSeen:
			return stepResumePendingHandoff(cfg, s, ev)
		default:
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

func stepAwaitingHandoff(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvNonceObserved:
		return stepConfirmHandoff(cfg, s, ev, false, time.Time{})
	case EvHandoffFreshSeen:
		s.HandoffFresh = true
		s.HandoffFreshMtime = ev.Mtime
		return s, nil
	case EvOperatorTurnRecent:
		return stepParkForOperator(cfg, s, ev)
	case EvTimerFired:
		if ev.Timer != TimerHandoffTimeout {
			return s, nil
		}
		if s.HandoffFresh {
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
		return stepParkPending(cfg, s, ev)
	default:
		return s, nil
	}
}

func stepConfirmHandoff(cfg *CyclerConfig, s CycleState, ev Event, recovered bool, mtime time.Time) (CycleState, []Action) {
	s.Phase = PhaseAwaitModelDone
	s.NonceConfirmedAt = ev.At
	return s, []Action{
		journalAction(&s, "confirmed", ev.At),
		emitHandoffWrittenAction(cfg, s.CycleID, s.PrevSID, recovered, mtime),
		{Kind: ActCancelTimer, Timer: TimerHandoffTimeout},
		{Kind: ActArmTimer, Timer: TimerModelDone, D: cfg.ModelDoneTimeout},
	}
}

func stepParkPending(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	s.Reason = "handoff_pending"
	s.Phase = PhaseIdle
	s.LastTerminal = "pending"
	s.LastFireWasAbort = false
	return s, []Action{
		journalAction(&s, "pending", ev.At),
		emitCycleParkedAction(cfg, s.CycleID, s.EntryCF.SessionID, s.Reason),
		{Kind: ActCancelTimer, Timer: TimerHandoffTimeout},
	}
}

func stepResumePendingHandoff(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	if s.LastTerminal != "pending" || ev.CycleID == "" || ev.CycleID != s.CycleID {
		return s, nil
	}
	s.LastTerminal = ""
	s.Reason = "handoff_observed_after_wait"
	return stepConfirmHandoff(cfg, s, ev, true, ev.Mtime)
}

func stepParkForOperator(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	s.Reason = "operator_turn_recent"
	s.Phase = PhaseIdle
	s.LastTerminal = "parked"
	return s, []Action{
		journalAction(&s, "parked", ev.At),
		emitCycleParkedAction(cfg, s.CycleID, s.EntryCF.SessionID, s.Reason),
		{Kind: ActCancelTimer, Timer: TimerHandoffTimeout},
	}
}

func stepAwaitModelDone(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvModelDone:
		return stepEnterClearing(cfg, s, ev, ev.Source, false)
	case EvTimerFired:
		if ev.Timer != TimerModelDone {
			return s, nil
		}
		return stepEnterClearing(cfg, s, ev, "timeout", true)
	default:
		return s, nil
	}
}

func stepClearing(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	switch ev.Kind {
	case EvSessionChanged:
		if ev.NewSID == "" || ev.NewSID == s.PrevSID {
			return s, nil
		}
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

func stepClearSettleExpired(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	if s.ClearAttempt >= cfg.ClearConfirmRetries {
		return stepClearUnconfirmed(cfg, s, ev)
	}
	s.ClearAttempt++
	var actions []Action
	if cfg.TmuxTarget != "" {
		gaugeDropped := ev.CF != nil && cfg.belowActThreshold(ev.CF)
		if !gaugeDropped {
			if clearAct, ok := injectClearAction(&s); ok {
				actions = append(actions, clearAct,
					emitClearSentAction(cfg, s.CycleID, s.PrevSID, s.ClearAttempt))
			}
		}
	}
	actions = append(actions, Action{Kind: ActArmTimer, Timer: TimerClearSettle, D: cfg.ClearSettle})
	return s, actions
}

// stepIdleGaugeTick is the MaybeRun 11-gate ladder (SK-011): a pure predicate
// over the event-carried GateSnapshot with the UNCONDITIONAL prelude (re-arm
// observation, same-SID escape hatch, boot-grace SID tracking) running before
// gating — on the fail path too (§3f: a "clean" short-circuit would change
// observable state). Gate order preserved exactly.
//
//nolint:cyclop // stepIdleGaugeTick is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
func stepIdleGaugeTick(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	cf := ev.CF
	snap := ev.Gates

	if !snap.Managed {
		return s, nil
	}
	if cf == nil || cf.SessionID == "" {
		return s, nil
	}

	s = applyAntiLoopPrelude(cfg, s, cf)
	s = trackBootGraceSID(cfg, s, ev.At, cf.SessionID)

	if bootGraceHolds(cfg, s, ev.At, cf) {
		return s, nil
	}

	if cfg.HardBandCycleOnly && !cfg.aboveForceThreshold(cf) {
		return s, nil
	}
	if !cfg.HardBandCycleOnly && cfg.belowActThreshold(cf) {
		return s, nil
	}
	if !snap.CrispIdle && !cfg.aboveForceThreshold(cf) {
		return s, nil
	}
	if snap.HoldingDispatch {
		return s, nil
	}
	if cf.SessionID != "" && snap.Sleeping {
		return s, nil
	}
	if snap.Held {
		return s, nil
	}
	if gateOperatorTurnHolds(cfg, snap, ev.At, cf.SessionID) {
		return s, nil
	}
	if gatePostAnswerGraceHolds(cfg, snap, ev.At, cf.SessionID) {
		return s, nil
	}
	if gateAntiLoopSuppresses(cfg, s, ev.At, cf) {
		return s, nil
	}
	if snap.OperatorAttached {
		return s, nil
	}

	return stepStartCycle(cfg, s, ev, cf)
}

func gateOperatorTurnHolds(cfg *CyclerConfig, snap GateSnapshot, at time.Time, sid string) bool {
	return cfg.OperatorTurnLookback > 0 && sid != "" && !snap.LastUserTurnAt.IsZero() &&
		at.Sub(snap.LastUserTurnAt) <= cfg.OperatorTurnLookback
}

func gatePostAnswerGraceHolds(cfg *CyclerConfig, snap GateSnapshot, at time.Time, sid string) bool {
	return cfg.PostAnswerGrace > 0 && sid != "" && !snap.LastAssistantTurnAt.IsZero() &&
		at.Sub(snap.LastAssistantTurnAt) <= cfg.PostAnswerGrace
}

func applyAntiLoopPrelude(cfg *CyclerConfig, s CycleState, cf *CtxFile) CycleState {
	if s.LastFiredSID != "" && cf.SessionID != s.LastFiredSID && cfg.belowWarnThreshold(cf) {
		s.SeenLowPctAfterLastFire = true
	}
	if s.LastFiredSID != "" && cf.SessionID == s.LastFiredSID &&
		!s.LastFireWasAbort && cfg.belowWarnThreshold(cf) {
		s.LastFiredSID = ""
		s.SeenLowPctAfterLastFire = false
		s.LastFireWasAbort = false
	}
	return s
}

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
	next := make(map[string]struct{}, len(s.SeenSessionIDs)+1)
	for k := range s.SeenSessionIDs {
		next[k] = struct{}{}
	}
	next[sid] = struct{}{}
	s.SeenSessionIDs = next
	s.CurrentSessionID = sid
	return s
}

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

func gateAntiLoopSuppresses(cfg *CyclerConfig, s CycleState, at time.Time, cf *CtxFile) bool {
	if s.LastFiredSID == "" {
		return false
	}
	if cf.SessionID == s.LastFiredSID {
		if !cfg.aboveForceThreshold(cf) {
			return true
		}
		return !s.LastForcedAttemptAt.IsZero() && at.Sub(s.LastForcedAttemptAt) < cfg.ForceRetryInterval
	}
	if !s.SeenLowPctAfterLastFire {
		if !cfg.aboveForceThreshold(cf) {
			return true
		}
		return !s.LastForcedAttemptAt.IsZero() && at.Sub(s.LastForcedAttemptAt) < cfg.ForceRetryInterval
	}
	return false
}

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

	if !snap.Managed {
		return s, blocked("not_managed")
	}
	if sessionID == "" {
		return s, blocked("hold_dispatch_skip")
	}
	if bootGraceHolds(cfg, s, ev.At, cf) {
		return s, blocked("boot_grace")
	}

	if cf != nil {
		s = applyAntiLoopPrelude(cfg, s, cf)
	}

	if snap.HoldingDispatch {
		return s, blocked("hold_dispatch_skip")
	}
	if snap.Held {
		return s, blocked("hold_skip")
	}
	if s.LastFiredSID != "" {
		if sessionID == s.LastFiredSID || !s.SeenLowPctAfterLastFire {
			return s, blocked("anti_loop_suppressed")
		}
	}
	if snap.OperatorAttached {
		return s, blocked("operator_attached")
	}

	actions := blocked("cycle_triggered")
	if cf == nil {
		cf = &CtxFile{SessionID: sessionID}
	}
	next, startActions := stepStartCycle(cfg, s, ev, cf)
	return next, append(actions, startActions...)
}

func stepIdleRestartTick(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	cf := ev.CF
	if cf == nil {
		return s, nil
	}
	sessionID := cf.SessionID
	snap := ev.Gates

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
	if !cfg.belowActThreshold(cf) {
		return s, nil
	}
	if !snap.CrispIdle {
		return s, nil
	}
	if snap.HoldingDispatch {
		return s, nil
	}
	if snap.Held {
		return s, nil
	}
	if !s.LastIdleRestartAt.IsZero() && ev.At.Sub(s.LastIdleRestartAt) < cfg.IdleRestartCooldown {
		return s, nil
	}
	if s.LastFiredSID != "" && sessionID == s.LastFiredSID {
		return s, nil
	}

	s.LastIdleRestartAt = ev.At
	return stepStartCycle(cfg, s, ev, cf)
}

func stepIdleCrashJournal(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	j := ev.Journal
	if j == nil {
		return s, nil
	}
	switch j.Phase {
	case "pending":
		s.CycleID = j.CycleID
		s.EntryKind = EvGaugeTick
		s.EntryCF = CtxFile{SessionID: j.SessionID}
		s.PrevSID = j.SessionID
		s.OpenedAt = j.OpenedAt
		s.LastTerminal = "pending"
		s.Reason = "handoff_pending"
		return s, nil
	case "cleared":
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
		done := *j
		done.Phase = "aborted"
		done.UpdatedAt = ev.At.UTC()
		done.Reason = "crash_before_clear"
		return s, []Action{{Kind: ActWriteJournal, Journal: done}}
	default:
		return s, nil
	}
}

func stepStartCycle(cfg *CyclerConfig, s CycleState, ev Event, cf *CtxFile) (CycleState, []Action) {
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
		journalAction(&s, "opened", ev.At),
		emitHandoffStartedAction(cfg, s.CycleID, cf.SessionID),
	}
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

func stepEnterClearing(cfg *CyclerConfig, s CycleState, ev Event, source string, degraded bool) (CycleState, []Action) {
	s.Phase = PhaseClearing
	s.ClearAttempt = 1
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
		Action{Kind: ActArmTimer, Timer: TimerClearBackstop, D: cfg.ClearConfirmBackstop},
		Action{Kind: ActArmTimer, Timer: TimerClearSettle, D: cfg.ClearSettle},
	)
	return s, actions
}

func injectClearAction(s *CycleState) (Action, bool) {
	if s.ModelDoneSource == "" {
		return Action{}, false
	}
	return Action{Kind: ActInjectClear}, true
}

func stepClearUnconfirmed(cfg *CyclerConfig, s CycleState, ev Event) (CycleState, []Action) {
	actions := []Action{
		emitClearUnconfirmedAction(cfg, s.CycleID, s.EntryCF.SessionID),
		{Kind: ActSetManagedSession, SID: ""},
		{Kind: ActCancelTimer, Timer: TimerClearSettle},
		{Kind: ActCancelTimer, Timer: TimerClearBackstop},
	}
	return stepBriefing(cfg, s, ev, "", actions)
}

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

	s.LastFiredSID = s.EntryCF.SessionID
	s.SeenLowPctAfterLastFire = false
	s.LastFireWasAbort = false
	s.ConsecutiveHandoffTimeouts = 0
	s.BootGraceFirstArmAt = time.Time{}

	s.Phase = PhaseIdle
	s.LastTerminal = "complete"
	return s, actions
}

func journalAction(s *CycleState, phase string, at time.Time) Action {
	return Action{Kind: ActWriteJournal, Journal: CycleJournal{
		CycleID:   s.CycleID,
		SessionID: s.EntryCF.SessionID,
		Phase:     phase,
		OpenedAt:  s.OpenedAt,
		UpdatedAt: at.UTC(),
		Reason:    s.Reason,
	}}
}

func journalCompleteAction(s *CycleState, at time.Time) Action {
	return Action{Kind: ActWriteJournal, Journal: CycleJournal{
		CycleID:   s.CycleID,
		SessionID: s.EntryCF.SessionID,
		Phase:     "complete",
		OpenedAt:  s.OpenedAt,
		UpdatedAt: at.UTC(),
		Reason:    s.Reason,
	}}
}

func handoffContentHasStaleNonce(content, currentNonce string) bool {
	if !strings.Contains(content, nonceMarkerPrefix) {
		return false // no keeper nonce at all → genuine handoff; preserve it
	}
	return !isOnlyNonce(content, currentNonce)
}

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

func emitCycleParkedAction(cfg *CyclerConfig, cycleID, sessionID, reason string) Action {
	raw := mustMarshalPayload(core.SessionKeeperCycleParkedPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Reason:    reason,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperCycleParked, Payload: raw}
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

func emitClearSentAction(cfg *CyclerConfig, cycleID, sessionID string, attempt int) Action {
	raw := mustMarshalPayload(core.SessionKeeperClearSentPayload{
		AgentName: cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Attempt:   attempt,
	})
	return Action{Kind: ActEmit, Type: core.EventTypeSessionKeeperClearSent, Payload: raw}
}

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
