package runexec

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// dispatch.go — the PURE per-agent-session Dispatch reactor (RSM-004/005/006;
// runexec-design §3). It mirrors internal/keeper/step.go: it holds
// DispatchState, exposes Step(ev) []Action / State() / InFlight(), and is
// drivable by the free function substrate.Run[Event, Action]. stepDispatch is
// TOTAL and pure — no I/O, no clock reads, no id minting; every timestamp comes
// from the event's shell-stamped At.
//
// States (RSM-004):
//   Idle → Launching → AwaitingReady → Briefing → Working →
//     {Completed | Exited | Stalled | ReadyTimeout→Failed | Failed | Aborted}
// A SkipReadyHandshake session (completion-by-process-exit harness) transitions
// Launching → Working directly, skipping AwaitingReady and Briefing.
//
// Liveness (RSM-005 / RSM-INV-002): the AwaitingReady agent_ready-timer edge is
// the SR9 edge — it emits an outgoing action set (kill + reap-timer +
// agent_ready_timeout emission), never a silent wait. Every timer-fired
// transition lands in a state with an action or a real state change.

// DispatchPhase is the Dispatch machine's state (RSM-004).
type DispatchPhase string

// The Dispatch phases. ReadyTimeout is a transient state on the way to Failed
// (it awaits the kill-reap); the rest of the trailing set are terminals with no
// outgoing edges (RSM-003).
const (
	DispatchIdle          DispatchPhase = "idle"
	DispatchLaunching     DispatchPhase = "launching"
	DispatchAwaitingReady DispatchPhase = "awaiting_ready"
	DispatchBriefing      DispatchPhase = "briefing"
	DispatchWorking       DispatchPhase = "working"
	DispatchReadyTimeout  DispatchPhase = "ready_timeout" // transient → Failed
	// Terminals.
	DispatchCompleted DispatchPhase = "completed"
	DispatchExited    DispatchPhase = "exited"
	DispatchStalled   DispatchPhase = "stalled"
	DispatchFailed    DispatchPhase = "failed"
	DispatchAborted   DispatchPhase = "aborted"
)

// dispatchTerminals is the set of terminal phases (structural: no outgoing
// edges, RSM-003). ReadyTimeout is NOT terminal (it steps to Failed).
var dispatchTerminals = map[DispatchPhase]struct{}{
	DispatchCompleted: {}, DispatchExited: {}, DispatchStalled: {},
	DispatchFailed: {}, DispatchAborted: {},
}

// DispatchConfig holds the policy scalars the pure machine needs. Durations are
// used only to populate ArmTimer actions (the shell owns the actual deadlines).
type DispatchConfig struct {
	// SkipReadyHandshake: a completion-by-process-exit harness (codex/pi) has no
	// readiness handshake — Launching → Working directly (RSM-004; RF P13).
	SkipReadyHandshake bool
	// IsResume selects the brief vs resume_prompt input on the ready edge, and is
	// carried for the run-attributed readiness requirement (RSM-005).
	IsResume bool
	// MaxInputAttempts bounds the brief-redelivery retry (RSM-INV-001 edge). A
	// value ≤1 means one attempt (no retry).
	MaxInputAttempts int

	ReadyTimeout  time.Duration
	InputAck      time.Duration
	ReadyKillReap time.Duration
}

// DispatchState is the full reactor state. All timestamps are event-At-sourced.
type DispatchState struct {
	Phase   DispatchPhase
	Session SessionRef
	Attempt int // brief-delivery attempt counter (1-based once briefing starts)

	// BriefInput correlates the in-flight brief/resume submission with its Ack.
	BriefInput InputID

	// ReadyAt / LastProgressAt are event-At-sourced. LastProgressAt advances ONLY
	// on agent-derived progress (ready, commit, ack, outcome) — never on a bare
	// daemon heartbeat (RSM-006).
	ReadyAt        time.Time
	LastProgressAt time.Time

	// LastAckedInput is the most recently confirmed input id; a duplicate ack for
	// it is dropped by Step (RSM-027).
	LastAckedInput InputID

	// Terminal detail (informational; the Run machine / sub-driver consumes it).
	Outcome  string
	ExitCode int
	Reason   string
}

func (s DispatchState) clone() DispatchState { return s }

// Dispatch is the pure per-session reactor. Not safe for concurrent use; owned
// by the single run-shell goroutine.
type Dispatch struct {
	cfg   DispatchConfig
	state DispatchState
}

// NewDispatch constructs the reactor in Idle.
func NewDispatch(cfg DispatchConfig) *Dispatch {
	return &Dispatch{cfg: cfg, state: DispatchState{Phase: DispatchIdle}}
}

// Step advances the machine: pure transition + action emission.
func (m *Dispatch) Step(ev Event) []Action {
	next, actions := stepDispatch(m.cfg, m.state, ev)
	m.state = next
	return actions
}

// State returns a copy of the current reactor state.
func (m *Dispatch) State() DispatchState { return m.state.clone() }

// InFlight reports whether a dispatch is active (not Idle and not terminal).
func (m *Dispatch) InFlight() bool {
	if m.state.Phase == DispatchIdle {
		return false
	}
	_, terminal := dispatchTerminals[m.state.Phase]
	return !terminal
}

// Run drives the reactor from a substrate EventSource into an Effector — the
// one-line wrapper over the free function (mirrors keeper.Cycle.Run).
func (m *Dispatch) Run(ctx context.Context, src substrate.EventSource[Event], eff substrate.Effector[Action]) error {
	return substrate.Run(ctx, src, m.Step, eff)
}

// stepDispatch is the total pure transition (cfg, state, event) → (state', []action).
// Terminal phases have no outgoing edges (every event → no-op).
func stepDispatch(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	if _, terminal := dispatchTerminals[s.Phase]; terminal {
		return s, nil
	}
	// EvAborted is a uniform non-terminal edge (runexec-design §3 "any non-terminal").
	if ev.Kind == EvAborted {
		s.Phase = DispatchAborted
		s.Reason = ev.Reason
		return s, []Action{{Kind: ActKillAgent, Session: s.Session}}
	}
	switch s.Phase {
	case DispatchIdle:
		return stepDispatchIdle(cfg, s, ev)
	case DispatchLaunching:
		return stepDispatchLaunching(cfg, s, ev)
	case DispatchAwaitingReady:
		return stepDispatchAwaitingReady(cfg, s, ev)
	case DispatchBriefing:
		return stepDispatchBriefing(cfg, s, ev)
	case DispatchWorking:
		return stepDispatchWorking(cfg, s, ev)
	case DispatchReadyTimeout:
		return stepDispatchReadyTimeout(cfg, s, ev)
	default:
		return s, nil
	}
}

// stepDispatchIdle: the shell's StartDispatch entry launches the agent and arms
// the agent-ready deadline.
func stepDispatchIdle(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	if ev.Kind != EvStartDispatch {
		return s, nil
	}
	s.Phase = DispatchLaunching
	s.Session = ev.Session
	return s, []Action{
		{Kind: ActLaunchAgent, Session: ev.Session, SpecRef: ev.Detail},
		{Kind: ActArmTimer, Timer: TimerAgentReady, D: cfg.ReadyTimeout},
	}
}

// stepDispatchLaunching: on EvLaunched emit the held-back launch_initiated
// (RF :4667) and advance. A SkipReadyHandshake harness goes straight to Working
// (RSM-004); otherwise it awaits the readiness handshake.
func stepDispatchLaunching(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	switch ev.Kind {
	case EvLaunched:
		s.Session = ev.Session
		emit := Action{Kind: ActEmit, Type: core.EventTypeLaunchInitiated}
		if cfg.SkipReadyHandshake {
			s.Phase = DispatchWorking
			s.LastProgressAt = ev.At
			return s, []Action{emit, {Kind: ActCancelTimer, Timer: TimerAgentReady}}
		}
		s.Phase = DispatchAwaitingReady
		return s, []Action{emit}
	case EvLaunchFailed:
		s.Phase = DispatchFailed
		s.Reason = ev.Reason
		return s, []Action{
			{Kind: ActEmit, Type: launchFailedEventType(ev.Reason), Detail: ev.Reason},
			{Kind: ActCancelTimer, Timer: TimerAgentReady},
		}
	default:
		return s, nil
	}
}

// stepDispatchAwaitingReady: the readiness handshake or its timeout (the SR9
// edge). Readiness is signalled by a run-attributed agent_ready (RSM-005).
func stepDispatchAwaitingReady(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	switch ev.Kind {
	case EvAgentReady:
		s.Phase = DispatchBriefing
		s.ReadyAt = ev.At
		s.LastProgressAt = ev.At
		s.Attempt = 1
		s.BriefInput = ev.InputID
		return s, []Action{
			{Kind: ActCancelTimer, Timer: TimerAgentReady},
			deliverInputAction(s.Session, ev.InputID, cfg.IsResume),
			{Kind: ActArmTimer, Timer: TimerInputAck, D: cfg.InputAck},
		}
	case EvTimerFired:
		if ev.Timer != TimerAgentReady {
			return s, nil
		}
		// RSM-005 / RSM-INV-002 — the SR9 edge: kill, arm the reap deadline, and
		// emit agent_ready_timeout. NEVER a silent wait.
		s.Phase = DispatchReadyTimeout
		s.Reason = "agent_ready_timeout"
		return s, []Action{
			{Kind: ActKillAgent, Session: s.Session},
			{Kind: ActArmTimer, Timer: TimerReadyKillReap, D: cfg.ReadyKillReap},
			{Kind: ActEmit, Type: core.EventTypeAgentReadyTimeout},
		}
	case EvAgentExited:
		s.Phase = DispatchExited
		s.ExitCode = ev.ExitCode
		return s, []Action{{Kind: ActCancelTimer, Timer: TimerAgentReady}}
	default:
		return s, nil
	}
}

// stepDispatchReadyTimeout: the kill-reap awaits either the agent's exit or the
// reap deadline, then settles into Failed(agent_ready_timeout). The Run machine
// reopens (RSM-025).
func stepDispatchReadyTimeout(_ DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	switch ev.Kind {
	case EvAgentExited, EvTimerFired:
		if ev.Kind == EvTimerFired && ev.Timer != TimerReadyKillReap {
			return s, nil
		}
		s.Phase = DispatchFailed
		s.Reason = "agent_ready_timeout"
		return s, nil
	default:
		return s, nil
	}
}

// stepDispatchBriefing: await the input Ack. A rejection or input-ack timeout
// retries the delivery (Attempt++ < max) or fails closed (RSM-INV-001).
func stepDispatchBriefing(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	switch ev.Kind {
	case EvInputAck:
		// Drop a duplicate ack for an already-correlated submission (RSM-027).
		if ev.InputID != "" && ev.InputID == s.LastAckedInput {
			return s, nil
		}
		s.Phase = DispatchWorking
		s.LastAckedInput = ev.InputID
		s.LastProgressAt = ev.At
		return s, []Action{{Kind: ActCancelTimer, Timer: TimerInputAck}}
	case EvInputRejected:
		return dispatchBriefRetry(cfg, s, ev)
	case EvTimerFired:
		if ev.Timer != TimerInputAck {
			return s, nil
		}
		return dispatchBriefRetry(cfg, s, ev)
	case EvAgentExited:
		s.Phase = DispatchExited
		s.ExitCode = ev.ExitCode
		return s, []Action{{Kind: ActCancelTimer, Timer: TimerInputAck}}
	default:
		return s, nil
	}
}

// dispatchBriefRetry is the RSM-INV-001 edge: retry the brief while attempts
// remain, else fail closed (input_undeliverable). Either branch is an outgoing
// action, never a silent no-op (RSM-INV-002).
func dispatchBriefRetry(cfg DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	if s.Attempt < cfg.MaxInputAttempts {
		s.Attempt++
		return s, []Action{
			deliverInputAction(s.Session, s.BriefInput, cfg.IsResume),
			{Kind: ActArmTimer, Timer: TimerInputAck, D: cfg.InputAck},
		}
	}
	s.Phase = DispatchFailed
	s.Reason = "input_undeliverable"
	return s, []Action{{Kind: ActCancelTimer, Timer: TimerInputAck}}
}

// stepDispatchWorking: only agent-derived signals sustain/advance progress
// (RSM-006). A bare daemon heartbeat is explicitly NOT progress.
func stepDispatchWorking(_ DispatchConfig, s DispatchState, ev Event) (DispatchState, []Action) {
	switch ev.Kind {
	case EvHeartbeat:
		// RSM-006: daemon-goroutine liveness is NOT agent progress — no-op, and
		// LastProgressAt deliberately does NOT advance.
		return s, nil
	case EvCommitObserved:
		s.LastProgressAt = ev.At // observed worktree-HEAD advance IS progress
		return s, nil
	case EvOutcomeReceived:
		s.Phase = DispatchCompleted
		s.Outcome = ev.Outcome
		s.LastProgressAt = ev.At
		return s, nil
	case EvAgentExited:
		s.Phase = DispatchExited
		s.ExitCode = ev.ExitCode
		return s, []Action{{Kind: ActDriveLifecycleTerminated, ExitCode: ev.ExitCode, WaitErr: ev.WaitErr}}
	case EvNoChangeTimeout, EvHeartbeatStale:
		s.Phase = DispatchStalled
		s.Reason = string(ev.Kind)
		return s, []Action{{Kind: ActKillAgent, Session: s.Session}}
	default:
		return s, nil
	}
}

// launchFailedEventType maps the launch-failure reason to its event type
// (runexec-design §3: ActEmit(spawn_cap_blocked/tmux…)). The reason string is
// the shell-classified launch error (RF :4639–:4661).
func launchFailedEventType(reason string) core.EventType {
	if reason == string(core.EventTypeTmuxNewWindowTimeout) {
		return core.EventTypeTmuxNewWindowTimeout
	}
	return core.EventTypeSpawnCapBlocked
}

// deliverInputAction builds the brief-vs-resume input delivery (RSM-005: brief
// on first launch, resume_prompt on resume).
func deliverInputAction(sess SessionRef, id InputID, isResume bool) Action {
	kind := InputBrief
	if isResume {
		kind = InputResumePrompt
	}
	return Action{Kind: ActDeliverInput, Session: sess, InputID: id, InputKind: kind}
}
