package runexec

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// dispatch_test.go — L0 per-transition + property tests for the Dispatch machine
// (RSM-004/005/006, RSM-INV-002). All tests are pure (no LLM, no clock, no I/O):
// they run zero-token by construction — the reactor mints no ids and reads no
// time source, so every assertion is a deterministic function of the fed events.

func at(sec int) time.Time { return time.Unix(int64(sec), 0).UTC() }

func stdDispatchCfg() DispatchConfig {
	return DispatchConfig{
		MaxInputAttempts: 2,
		ReadyTimeout:     30 * time.Second,
		InputAck:         10 * time.Second,
		ReadyKillReap:    5 * time.Second,
	}
}

func kinds(actions []Action) []ActionKind {
	out := make([]ActionKind, len(actions))
	for i, a := range actions {
		out[i] = a.Kind
	}
	return out
}

func eqKinds(a, b []ActionKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// drive a session down its happy path to Working.
func toWorking(t *testing.T, cfg DispatchConfig) *Dispatch {
	t.Helper()
	m := NewDispatch(cfg)
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)})
	m.Step(Event{Kind: EvAgentReady, Session: "s1", InputID: "i1", At: at(3)})
	m.Step(Event{Kind: EvInputAck, Session: "s1", InputID: "i1", At: at(4)})
	if m.State().Phase != DispatchWorking {
		t.Fatalf("toWorking: got phase %s", m.State().Phase)
	}
	return m
}

func TestDispatch_HappyPath(t *testing.T) {
	m := NewDispatch(stdDispatchCfg())

	got := kinds(m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)}))
	if !eqKinds(got, []ActionKind{ActLaunchAgent, ActArmTimer}) {
		t.Fatalf("start: %v", got)
	}
	if m.State().Phase != DispatchLaunching {
		t.Fatalf("phase %s", m.State().Phase)
	}

	got = kinds(m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)}))
	if !eqKinds(got, []ActionKind{ActEmit}) {
		t.Fatalf("launched: %v", got)
	}
	if m.State().Phase != DispatchAwaitingReady {
		t.Fatalf("phase %s", m.State().Phase)
	}

	got = kinds(m.Step(Event{Kind: EvAgentReady, Session: "s1", InputID: "i1", At: at(3)}))
	if !eqKinds(got, []ActionKind{ActCancelTimer, ActDeliverInput, ActArmTimer}) {
		t.Fatalf("ready: %v", got)
	}
	got = kinds(m.Step(Event{Kind: EvInputAck, Session: "s1", InputID: "i1", At: at(4)}))
	if !eqKinds(got, []ActionKind{ActCancelTimer}) {
		t.Fatalf("ack: %v", got)
	}
	got = kinds(m.Step(Event{Kind: EvOutcomeReceived, Outcome: "ok", At: at(5)}))
	if len(got) != 0 {
		t.Fatalf("outcome: %v", got)
	}
	if m.State().Phase != DispatchCompleted {
		t.Fatalf("phase %s", m.State().Phase)
	}
}

func TestDispatch_BriefDeliversResumeOnResume(t *testing.T) {
	cfg := stdDispatchCfg()
	cfg.IsResume = true
	m := NewDispatch(cfg)
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)})
	acts := m.Step(Event{Kind: EvAgentReady, Session: "s1", InputID: "i1", At: at(3)})
	for _, a := range acts {
		if a.Kind == ActDeliverInput && a.InputKind != InputResumePrompt {
			t.Fatalf("resume path delivered %s, want resume_prompt", a.InputKind)
		}
	}
}

func TestDispatch_SkipReadyHandshake(t *testing.T) {
	cfg := stdDispatchCfg()
	cfg.SkipReadyHandshake = true
	m := NewDispatch(cfg)
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	got := kinds(m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)}))
	if !eqKinds(got, []ActionKind{ActEmit, ActCancelTimer}) {
		t.Fatalf("skip launched: %v", got)
	}
	if m.State().Phase != DispatchWorking {
		t.Fatalf("skip harness must go Launching->Working, got %s", m.State().Phase)
	}
}

func TestDispatch_ReadyTimeoutSR9Edge(t *testing.T) {
	// RSM-005 / RSM-INV-002: the agent_ready timer edge is an outgoing action
	// set (kill + reap + agent_ready_timeout emit), never a silent wait.
	m := NewDispatch(stdDispatchCfg())
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)})
	got := m.Step(Event{Kind: EvTimerFired, Timer: TimerAgentReady, At: at(40)})
	if !eqKinds(kinds(got), []ActionKind{ActKillAgent, ActArmTimer, ActEmit}) {
		t.Fatalf("SR9 edge: %v", kinds(got))
	}
	var sawTimeout bool
	for _, a := range got {
		if a.Kind == ActEmit && a.Type == core.EventTypeAgentReadyTimeout {
			sawTimeout = true
		}
	}
	if !sawTimeout {
		t.Fatal("SR9 edge must emit agent_ready_timeout")
	}
	if m.State().Phase != DispatchReadyTimeout {
		t.Fatalf("phase %s", m.State().Phase)
	}
	// Kill-reap fires → Failed(agent_ready_timeout).
	m.Step(Event{Kind: EvTimerFired, Timer: TimerReadyKillReap, At: at(46)})
	if m.State().Phase != DispatchFailed || m.State().Reason != "agent_ready_timeout" {
		t.Fatalf("post-reap: phase=%s reason=%s", m.State().Phase, m.State().Reason)
	}
}

func TestDispatch_LaunchTimeoutNotSilent(t *testing.T) {
	// RSM-INV-002: a hung launch (no EvLaunched/EvLaunchFailed) whose agent_ready
	// deadline expires in Launching must ride the SR9 edge, not wait silently.
	m := NewDispatch(stdDispatchCfg())
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	got := m.Step(Event{Kind: EvTimerFired, Timer: TimerAgentReady, At: at(40)})
	if !eqKinds(kinds(got), []ActionKind{ActKillAgent, ActArmTimer, ActEmit}) {
		t.Fatalf("launch timeout edge: %v", kinds(got))
	}
	if m.State().Phase != DispatchReadyTimeout {
		t.Fatalf("phase %s", m.State().Phase)
	}
	var sawTimeout bool
	for _, a := range got {
		if a.Kind == ActEmit && a.Type == core.EventTypeAgentReadyTimeout {
			sawTimeout = true
		}
	}
	if !sawTimeout {
		t.Fatal("launch timeout must emit agent_ready_timeout")
	}
}

func TestDispatch_HeartbeatNotProgress(t *testing.T) {
	// RSM-006: a bare daemon heartbeat MUST NOT advance progress; a commit does.
	m := toWorking(t, stdDispatchCfg())
	before := m.State().LastProgressAt
	m.Step(Event{Kind: EvHeartbeat, At: at(100)})
	if m.State().LastProgressAt != before {
		t.Fatal("heartbeat advanced LastProgressAt (RSM-006 violation)")
	}
	if m.State().Phase != DispatchWorking {
		t.Fatalf("heartbeat changed phase to %s", m.State().Phase)
	}
	m.Step(Event{Kind: EvCommitObserved, SHA: "abc", At: at(200)})
	if m.State().LastProgressAt != at(200) {
		t.Fatal("commit did not advance LastProgressAt")
	}
}

func TestDispatch_BriefRetryThenFailClosed(t *testing.T) {
	// RSM-INV-001: input rejection retries within budget then fails closed.
	cfg := stdDispatchCfg() // caps brief attempts at two
	m := NewDispatch(cfg)
	m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
	m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)})
	m.Step(Event{Kind: EvAgentReady, Session: "s1", InputID: "i1", At: at(3)})
	// First rejection → retry (attempt 1 < 2).
	got := kinds(m.Step(Event{Kind: EvInputRejected, InputID: "i1", At: at(4)}))
	if !eqKinds(got, []ActionKind{ActDeliverInput, ActArmTimer}) {
		t.Fatalf("retry: %v", got)
	}
	if m.State().Phase != DispatchBriefing {
		t.Fatalf("phase %s", m.State().Phase)
	}
	// Second rejection → fail closed (attempt 2 == max).
	got = kinds(m.Step(Event{Kind: EvTimerFired, Timer: TimerInputAck, At: at(20)}))
	if !eqKinds(got, []ActionKind{ActCancelTimer}) {
		t.Fatalf("failclosed: %v", got)
	}
	if m.State().Phase != DispatchFailed || m.State().Reason != "input_undeliverable" {
		t.Fatalf("phase=%s reason=%s", m.State().Phase, m.State().Reason)
	}
}

func TestDispatch_DuplicateAckDropped(t *testing.T) {
	// RSM-027: a duplicate ack for an already-correlated submission is dropped.
	m := toWorking(t, stdDispatchCfg()) // acked i1
	got := m.Step(Event{Kind: EvInputAck, InputID: "i1", At: at(50)})
	if len(got) != 0 || m.State().Phase != DispatchWorking {
		t.Fatalf("duplicate ack not dropped: %v phase=%s", got, m.State().Phase)
	}
}

func TestDispatch_AbortFromAnyNonTerminal(t *testing.T) {
	phases := []func() *Dispatch{
		func() *Dispatch {
			m := NewDispatch(stdDispatchCfg())
			m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
			return m
		},
		func() *Dispatch {
			m := NewDispatch(stdDispatchCfg())
			m.Step(Event{Kind: EvStartDispatch, Session: "s1", At: at(1)})
			m.Step(Event{Kind: EvLaunched, Session: "s1", At: at(2)})
			return m
		},
		func() *Dispatch { return toWorking(t, stdDispatchCfg()) },
	}
	for i, mk := range phases {
		m := mk()
		got := kinds(m.Step(Event{Kind: EvAborted, Reason: "shutdown", At: at(99)}))
		if !eqKinds(got, []ActionKind{ActKillAgent}) {
			t.Fatalf("case %d abort: %v", i, got)
		}
		if m.State().Phase != DispatchAborted {
			t.Fatalf("case %d phase %s", i, m.State().Phase)
		}
	}
}

func TestDispatch_WorkingExitDrivesLifecycle(t *testing.T) {
	m := toWorking(t, stdDispatchCfg())
	got := kinds(m.Step(Event{Kind: EvAgentExited, ExitCode: 0, At: at(60)}))
	if !eqKinds(got, []ActionKind{ActDriveLifecycleTerminated}) {
		t.Fatalf("exit: %v", got)
	}
	if m.State().Phase != DispatchExited {
		t.Fatalf("phase %s", m.State().Phase)
	}
}

func TestDispatch_WorkingStallKills(t *testing.T) {
	for _, k := range []EventKind{EvNoChangeTimeout, EvHeartbeatStale} {
		m := toWorking(t, stdDispatchCfg())
		got := kinds(m.Step(Event{Kind: k, At: at(60)}))
		if !eqKinds(got, []ActionKind{ActKillAgent}) {
			t.Fatalf("%s: %v", k, got)
		}
		if m.State().Phase != DispatchStalled {
			t.Fatalf("%s phase %s", k, m.State().Phase)
		}
	}
}

// ─── Property tests (pure, zero-token) ──────────────────────────────────────

// allDispatchPhases enumerates every phase for the exhaustive properties.
var allDispatchPhases = []DispatchPhase{
	DispatchIdle, DispatchLaunching, DispatchAwaitingReady, DispatchBriefing,
	DispatchWorking, DispatchReadyTimeout, DispatchCompleted, DispatchExited,
	DispatchStalled, DispatchFailed, DispatchAborted,
}

var allDispatchEventKinds = []EventKind{
	EvStartDispatch, EvLaunched, EvLaunchFailed, EvAgentReady, EvInputAck,
	EvInputRejected, EvHeartbeat, EvCommitObserved, EvOutcomeReceived,
	EvAgentExited, EvNoChangeTimeout, EvHeartbeatStale, EvAborted, EvTimerFired,
}

var allTimerKinds = []TimerKind{TimerAgentReady, TimerInputAck, TimerReadyKillReap}

// TestDispatch_TotalityNoPanic: Step is total — every (phase, event) pair is
// defined (RSM-003). Driven over synthetic states with all timer kinds.
func TestDispatch_TotalityNoPanic(t *testing.T) {
	for _, ph := range allDispatchPhases {
		for _, ek := range allDispatchEventKinds {
			for _, tk := range allTimerKinds {
				m := &Dispatch{cfg: stdDispatchCfg(), state: DispatchState{Phase: ph, Session: "s1", Attempt: 1}}
				_ = m.Step(Event{Kind: ek, Timer: tk, At: at(1)})
			}
		}
	}
}

// TestDispatch_TerminalExclusivity: terminal phases have NO outgoing edges —
// every event leaves state unchanged and emits nothing (RSM-003).
func TestDispatch_TerminalExclusivity(t *testing.T) {
	for term := range dispatchTerminals {
		for _, ek := range allDispatchEventKinds {
			m := &Dispatch{cfg: stdDispatchCfg(), state: DispatchState{Phase: term}}
			got := m.Step(Event{Kind: ek, At: at(1)})
			if len(got) != 0 {
				t.Fatalf("terminal %s emitted actions on %s: %v", term, ek, kinds(got))
			}
			if m.State().Phase != term {
				t.Fatalf("terminal %s changed phase on %s -> %s", term, ek, m.State().Phase)
			}
		}
	}
}

// TestDispatch_TimerFiredNeverSilent: RSM-INV-002 — every reachable
// (state, timer-fired) pair that is armed in that state produces an outgoing
// action OR a real state change; no armed timer edge is a silent no-op.
func TestDispatch_TimerFiredNeverSilent(t *testing.T) {
	// The armed-timer map: which timer is live in which non-terminal phase.
	armed := map[DispatchPhase]TimerKind{
		DispatchLaunching:     TimerAgentReady, // armed at Idle→Launching, live through Launching
		DispatchAwaitingReady: TimerAgentReady,
		DispatchBriefing:      TimerInputAck,
		DispatchReadyTimeout:  TimerReadyKillReap,
	}
	for ph, tk := range armed {
		m := &Dispatch{cfg: stdDispatchCfg(), state: DispatchState{Phase: ph, Session: "s1", Attempt: 99}}
		before := m.State().Phase
		got := m.Step(Event{Kind: EvTimerFired, Timer: tk, At: at(500)})
		if len(got) == 0 && m.State().Phase == before {
			t.Fatalf("RSM-INV-002 violation: (%s, %s) is a silent no-op", ph, tk)
		}
	}
}
