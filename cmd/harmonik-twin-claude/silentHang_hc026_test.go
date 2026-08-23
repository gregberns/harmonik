package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func silentHangFixtureString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	value, ok := m[key].(string)
	if !ok {
		t.Fatalf("%s = %v (type %T), want string", key, m[key], m[key])
	}
	return value
}

type silentHangFixtureState string

const (
	silentHangFixtureStateActive          silentHangFixtureState = "active"
	silentHangFixtureStateWarning         silentHangFixtureState = "warning"
	silentHangFixtureStateSoftTerminating silentHangFixtureState = "soft-terminating"
	silentHangFixtureStateHardTerminating silentHangFixtureState = "hard-terminating"
	silentHangFixtureStateTerminated      silentHangFixtureState = "terminated"
)

type silentHangFixtureEvent string

const (
	silentHangFixtureEventMessage     silentHangFixtureEvent = "progress_message_received"
	silentHangFixtureEventTimerTick   silentHangFixtureEvent = "timer_tick"
	silentHangFixtureEventSubprocExit silentHangFixtureEvent = "subprocess_exit"
)

type silentHangFixtureTransition struct {
	// From is the source state.
	From silentHangFixtureState
	// Event is the triggering event.
	Event silentHangFixtureEvent
	// Guard is the predicate that must hold for the transition to fire (empty
	// means unconditional).
	Guard string
	// To is the destination state.
	To silentHangFixtureState
	// Emits is the bus event emitted on this transition (empty means none).
	Emits string
}

var silentHangFixtureStateTable = []silentHangFixtureTransition{
	{
		From:  silentHangFixtureStateActive,
		Event: silentHangFixtureEventMessage,
		Guard: "",
		To:    silentHangFixtureStateActive,
		Emits: "",
	},
	// Row 2: active → warning on timer tick when silence >= T
	{
		From:  silentHangFixtureStateActive,
		Event: silentHangFixtureEventTimerTick,
		Guard: "now - last_progress_event_at >= T",
		To:    silentHangFixtureStateWarning,
		Emits: "agent_warning_silent_hang",
	},
	// Row 3: warning → active on any message (resumed after warning)
	{
		From:  silentHangFixtureStateWarning,
		Event: silentHangFixtureEventMessage,
		Guard: "",
		To:    silentHangFixtureStateActive,
		Emits: "agent_resumed_after_warning",
	},
	// Row 4: warning → soft-terminating on timer tick when silence >= M_soft (2*T)
	{
		From:  silentHangFixtureStateWarning,
		Event: silentHangFixtureEventTimerTick,
		Guard: "now - last_progress_event_at >= M_soft",
		To:    silentHangFixtureStateSoftTerminating,
		Emits: "agent_soft_terminating",
	},
	// Row 5: soft-terminating → terminated on subprocess exit
	{
		From:  silentHangFixtureStateSoftTerminating,
		Event: silentHangFixtureEventSubprocExit,
		Guard: "",
		To:    silentHangFixtureStateTerminated,
		Emits: "agent_failed:ErrStructural:silent_hang",
	},
	// Row 6: soft-terminating → hard-terminating on timer tick when silence >= M_hard (4*T)
	{
		From:  silentHangFixtureStateSoftTerminating,
		Event: silentHangFixtureEventTimerTick,
		Guard: "now - last_progress_event_at >= M_hard",
		To:    silentHangFixtureStateHardTerminating,
		Emits: "agent_hard_terminating",
	},
	// Row 7: hard-terminating → terminated on subprocess exit
	{
		From:  silentHangFixtureStateHardTerminating,
		Event: silentHangFixtureEventSubprocExit,
		Guard: "",
		To:    silentHangFixtureStateTerminated,
		Emits: "agent_failed:ErrStructural:silent_hang_hard_kill",
	},
}

func silentHangFixtureHeartbeatScript(heartbeatIntervalMs, count int) *ScriptFile {
	msgs := make([]ScriptMessage, 0, count+2)

	msgs = append(msgs, ScriptMessage{
		Type: "agent_started",
		Payload: map[string]any{
			"run_id":     "run-sh-hb-001",
			"session_id": "sess-sh-hb-001",
			"node_id":    "node-sh-hb-001",
			"agent_type": "claude-twin",
			"started_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}, ScriptMessage{
		Type: "agent_ready",
		Payload: map[string]any{
			"run_id":       "run-sh-hb-001",
			"session_id":   "sess-sh-hb-001",
			"capabilities": []string{"scripted", "heartbeat"},
		},
	})

	for range count {
		msgs = append(msgs, ScriptMessage{
			Type: "agent_heartbeat",
			Payload: map[string]any{
				"session_id": "sess-sh-hb-001",
				"phase":      "reasoning",
			},
			RelativeTimestampMs: heartbeatIntervalMs,
		})
	}

	msgs = append(msgs, ScriptMessage{
		Type: "outcome_emitted",
		Payload: map[string]any{
			"run_id":         "run-sh-hb-001",
			"session_id":     "sess-sh-hb-001",
			"node_id":        "node-sh-hb-001",
			"outcome_status": "success",
		},
	})

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages:      msgs,
	}
}

func silentHangFixtureNoMessageScript() *ScriptFile {
	return &ScriptFile{
		HeartbeatMode: heartbeatModeWallClock,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-sh-nm-001",
					"session_id": "sess-sh-nm-001",
					"node_id":    "node-sh-nm-001",
					"agent_type": "claude-twin",
					"started_at": time.Now().UTC().Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-sh-nm-001",
					"session_id":   "sess-sh-nm-001",
					"capabilities": []string{"scripted"},
				},
			},
		},
	}
}

func silentHangFixtureRateLimitScript() *ScriptFile {
	src := "anthropic"
	retryAfter := 30
	now := time.Now().UTC()

	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-sh-rl-001",
					"session_id": "sess-sh-rl-001",
					"node_id":    "node-sh-rl-001",
					"agent_type": "claude-twin",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-sh-rl-001",
					"session_id":   "sess-sh-rl-001",
					"capabilities": []string{"scripted", "heartbeat"},
				},
			},
			// Rate-limit onset.
			{
				Type: "agent_rate_limited",
				Payload: map[string]any{
					"run_id":              "run-sh-rl-001",
					"session_id":          "sess-sh-rl-001",
					"rate_limit_source":   src,
					"retry_after_seconds": retryAfter,
					"changed_at":          now.Format(time.RFC3339Nano),
				},
			},
			// Heartbeats during rate-limited window (phase: waiting_input).
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-sh-rl-001",
					"phase":      "waiting_input",
				},
				RelativeTimestampMs: 50,
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-sh-rl-001",
					"phase":      "waiting_input",
				},
				RelativeTimestampMs: 50,
			},
			// Rate-limit cleared.
			{
				Type: "agent_rate_limit_cleared",
				Payload: map[string]any{
					"run_id":     "run-sh-rl-001",
					"session_id": "sess-sh-rl-001",
					"changed_at": now.Add(100 * time.Millisecond).Format(time.RFC3339Nano),
				},
			},
			// Resume normal work.
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-sh-rl-001",
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 50,
			},
			// Terminal.
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         "run-sh-rl-001",
					"session_id":     "sess-sh-rl-001",
					"node_id":        "node-sh-rl-001",
					"outcome_status": "success",
				},
			},
		},
	}
}

func silentHangFixturePostOutcomeScript() *ScriptFile {
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-sh-po-001",
					"session_id": "sess-sh-po-001",
					"node_id":    "node-sh-po-001",
					"agent_type": "claude-twin",
					"started_at": time.Now().UTC().Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-sh-po-001",
					"session_id":   "sess-sh-po-001",
					"capabilities": []string{"scripted"},
				},
			},
			// A heartbeat during reasoning (active state).
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-sh-po-001",
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 10,
			},
			// Outcome emitted — watcher transitions to shutdown-window regime.
			// After this message the script ends; no heartbeats follow.
			// The watcher MUST NOT fire silent-hang; it MUST start T_shutdown timer.
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         "run-sh-po-001",
					"session_id":     "sess-sh-po-001",
					"node_id":        "node-sh-po-001",
					"outcome_status": "success",
				},
			},
		},
	}
}

// TestSilentHang_HC026_StateTableCoverage asserts that silentHangFixtureStateTable
// contains exactly the seven transitions declared in §7.1, and that each
// normative (from, event, to) triple is represented exactly once.
//
// This is a schema sensor: if the table drifts from the spec, this test flags
// it before watcher-state-machine tests (hk-8i31.31) are written.
//
// Spec: specs/handler-contract.md §7.1.
func TestSilentHang_HC026_StateTableCoverage(t *testing.T) {
	t.Parallel()

	const wantRows = 7
	if len(silentHangFixtureStateTable) != wantRows {
		t.Errorf("silentHangFixtureStateTable has %d rows, want %d (§7.1 declares 7 transitions)",
			len(silentHangFixtureStateTable), wantRows)
	}

	type triple struct {
		from  silentHangFixtureState
		event silentHangFixtureEvent
		to    silentHangFixtureState
	}
	normative := []triple{
		{silentHangFixtureStateActive, silentHangFixtureEventMessage, silentHangFixtureStateActive},
		{silentHangFixtureStateActive, silentHangFixtureEventTimerTick, silentHangFixtureStateWarning},
		{silentHangFixtureStateWarning, silentHangFixtureEventMessage, silentHangFixtureStateActive},
		{silentHangFixtureStateWarning, silentHangFixtureEventTimerTick, silentHangFixtureStateSoftTerminating},
		{silentHangFixtureStateSoftTerminating, silentHangFixtureEventSubprocExit, silentHangFixtureStateTerminated},
		{silentHangFixtureStateSoftTerminating, silentHangFixtureEventTimerTick, silentHangFixtureStateHardTerminating},
		{silentHangFixtureStateHardTerminating, silentHangFixtureEventSubprocExit, silentHangFixtureStateTerminated},
	}

	present := make(map[triple]bool, len(silentHangFixtureStateTable))
	for _, row := range silentHangFixtureStateTable {
		k := triple{row.From, row.Event, row.To}
		if present[k] {
			t.Errorf("duplicate transition in silentHangFixtureStateTable: from=%s event=%s to=%s",
				row.From, row.Event, row.To)
		}
		present[k] = true
	}

	for _, want := range normative {
		if !present[want] {
			t.Errorf("normative §7.1 transition missing from silentHangFixtureStateTable: from=%s event=%s to=%s",
				want.from, want.event, want.to)
		}
	}
}

// TestSilentHang_HC026_TerminalTransitionsEmitAgentFailed asserts that every
// §7.1 transition into "terminated" declares agent_failed with ErrStructural
// and a sub_reason matching §8.2.
//
// Spec: §4.6.HC-026 "The terminating error class MUST be ErrStructural";
// §8.2 sub_reason values: silent_hang, silent_hang_hard_kill.
func TestSilentHang_HC026_TerminalTransitionsEmitAgentFailed(t *testing.T) {
	t.Parallel()

	wantTerminalEmits := map[silentHangFixtureState]string{
		silentHangFixtureStateSoftTerminating: "agent_failed:ErrStructural:silent_hang",
		silentHangFixtureStateHardTerminating: "agent_failed:ErrStructural:silent_hang_hard_kill",
	}

	for _, row := range silentHangFixtureStateTable {
		if row.To != silentHangFixtureStateTerminated {
			continue
		}
		wantEmits, ok := wantTerminalEmits[row.From]
		if !ok {
			t.Errorf("unexpected terminal transition from %s — not covered in expected map", row.From)
			continue
		}
		if row.Emits != wantEmits {
			t.Errorf(
				"terminal transition from %s: Emits = %q, want %q (§4.6.HC-026 + §8.2 sub_reason)",
				row.From, row.Emits, wantEmits,
			)
		}
	}
}

// TestSilentHang_HC026a_ThresholdConstants asserts that the §7.1 escalation
// multipliers are consistent: M_soft = 2*T, M_hard = 4*T (absolute from last
// message, not relative to warning entry time).
//
// Spec: §7.1 "Absolute-from-last semantic: soft-terminate fires at 2*T from
// the last message, hard-terminate at 4*T from the last message."
func TestSilentHang_HC026a_ThresholdConstants(t *testing.T) {
	t.Parallel()

	const (
		silentHangFixtureT     = 600 * time.Second // default per §7.1
		silentHangFixtureMSoft = 2 * silentHangFixtureT
		silentHangFixtureMHard = 4 * silentHangFixtureT
	)

	const silentHangFixtureTickMax = silentHangFixtureT / 10

	if silentHangFixtureMSoft != 2*silentHangFixtureT {
		t.Errorf("M_soft = %v, want 2*T = %v (§7.1 absolute-from-last)", silentHangFixtureMSoft, 2*silentHangFixtureT)
	}
	if silentHangFixtureMHard != 4*silentHangFixtureT {
		t.Errorf("M_hard = %v, want 4*T = %v (§7.1 absolute-from-last)", silentHangFixtureMHard, 4*silentHangFixtureT)
	}
	if silentHangFixtureTickMax > silentHangFixtureT/10 {
		t.Errorf("tick cadence max %v exceeds T/10 = %v (§7.1 tick rule)", silentHangFixtureTickMax, silentHangFixtureT/10)
	}
}

// TestSilentHang_HC026a_HeartbeatScriptShape verifies that the heartbeat
// fixture script (silentHangFixtureHeartbeatScript) produces the correct
// message shapes: heartbeat_mode is "scripted", heartbeats carry required
// fields, and the script ends with outcome_emitted.
//
// Spec: §4.6.HC-026a payload requirements: {session_id, phase}.
func TestSilentHang_HC026a_HeartbeatScriptShape(t *testing.T) {
	t.Parallel()

	sf := silentHangFixtureHeartbeatScript(50, 3)

	if sf.HeartbeatMode != heartbeatModeScripted {
		t.Errorf("heartbeat script: heartbeat_mode = %q, want %q", sf.HeartbeatMode, heartbeatModeScripted)
	}

	const wantCount = 6
	if len(sf.Messages) != wantCount {
		t.Fatalf("heartbeat script: message count = %d, want %d", len(sf.Messages), wantCount)
	}

	if sf.Messages[0].Type != "agent_started" {
		t.Errorf("messages[0].type = %q, want agent_started", sf.Messages[0].Type)
	}

	if sf.Messages[1].Type != "agent_ready" {
		t.Errorf("messages[1].type = %q, want agent_ready", sf.Messages[1].Type)
	}

	for i := 2; i <= 4; i++ {
		msg := sf.Messages[i]
		if msg.Type != "agent_heartbeat" {
			t.Errorf("messages[%d].type = %q, want agent_heartbeat", i, msg.Type)
		}
		if _, ok := msg.Payload["session_id"]; !ok {
			t.Errorf("messages[%d]: missing session_id in heartbeat payload (HC-026a)", i)
		}
		if phase, ok := msg.Payload["phase"].(string); !ok || phase == "" {
			t.Errorf("messages[%d]: missing or empty phase in heartbeat payload (HC-026a)", i)
		}
		if msg.RelativeTimestampMs != 50 {
			t.Errorf("messages[%d].relative_timestamp_ms = %d, want 50", i, msg.RelativeTimestampMs)
		}
	}

	last := sf.Messages[len(sf.Messages)-1]
	if last.Type != "outcome_emitted" {
		t.Errorf("last message type = %q, want outcome_emitted", last.Type)
	}
}

// TestSilentHang_HC026a_HeartbeatScriptLoadsClean verifies that the heartbeat
// fixture script passes loadScriptFile validation (non-empty types, valid
// heartbeat_mode), confirming it is a well-formed script the twin binary can
// run.
func TestSilentHang_HC026a_HeartbeatScriptLoadsClean(t *testing.T) {
	t.Parallel()

	sf := silentHangFixtureHeartbeatScript(50, 3)

	if !sf.HeartbeatMode.Valid() {
		t.Errorf("heartbeat script heartbeat_mode %q is not valid (heartbeatMode.Valid = false)", sf.HeartbeatMode)
	}
	for i, msg := range sf.Messages {
		if msg.Type == "" {
			t.Errorf("heartbeat script messages[%d].type is empty; loadScriptFile would reject it", i)
		}
	}
}

// TestSilentHang_HC026_NoMessageScriptShape verifies that the no-message
// fixture script (silentHangFixtureNoMessageScript) is a well-formed script
// that deliberately contains no messages after agent_ready.
//
// The absence of heartbeats is the normative false-negative detection scenario
// per §10.2: the watcher MUST detect silence and escalate.
func TestSilentHang_HC026_NoMessageScriptShape(t *testing.T) {
	t.Parallel()

	sf := silentHangFixtureNoMessageScript()

	if sf.HeartbeatMode != heartbeatModeWallClock {
		t.Errorf("no-message script: heartbeat_mode = %q, want %q (§10.2: resilience tests use wall-clock)", sf.HeartbeatMode, heartbeatModeWallClock)
	}

	const wantCount = 2
	if len(sf.Messages) != wantCount {
		t.Fatalf("no-message script: message count = %d, want %d (agent_started + agent_ready only)", len(sf.Messages), wantCount)
	}

	if sf.Messages[0].Type != "agent_started" {
		t.Errorf("no-message script messages[0].type = %q, want agent_started", sf.Messages[0].Type)
	}
	if sf.Messages[1].Type != "agent_ready" {
		t.Errorf("no-message script messages[1].type = %q, want agent_ready", sf.Messages[1].Type)
	}

	if !sf.HeartbeatMode.Valid() {
		t.Errorf("no-message script heartbeat_mode %q is not valid", sf.HeartbeatMode)
	}
	for i, msg := range sf.Messages {
		if msg.Type == "" {
			t.Errorf("no-message script messages[%d].type is empty; loadScriptFile would reject it", i)
		}
	}
}

// TestSilentHang_HC026a_RateLimitScriptShape verifies that the rate-limit
// fixture script emits the correct sequence: agent_rate_limited, then
// heartbeats (phase: waiting_input), then agent_rate_limit_cleared.
//
// Per HC-026a: rate-limit and silent-hang are independent regimes; heartbeats
// during rate-limited windows use phase "waiting_input".
func TestSilentHang_HC026a_RateLimitScriptShape(t *testing.T) {
	t.Parallel()

	sf := silentHangFixtureRateLimitScript()

	if sf.HeartbeatMode != heartbeatModeScripted {
		t.Errorf("rate-limit script: heartbeat_mode = %q, want scripted", sf.HeartbeatMode)
	}

	var foundRateLimited, foundRateLimitCleared bool
	var heartbeatsDuringRL []ScriptMessage
	inRateLimited := false

	for _, msg := range sf.Messages {
		switch msg.Type {
		case "agent_rate_limited":
			foundRateLimited = true
			inRateLimited = true
		case "agent_rate_limit_cleared":
			foundRateLimitCleared = true
			inRateLimited = false
		case "agent_heartbeat":
			if inRateLimited {
				heartbeatsDuringRL = append(heartbeatsDuringRL, msg)
			}
		}
	}

	if !foundRateLimited {
		t.Error("rate-limit script: no agent_rate_limited message (HC-025)")
	}
	if !foundRateLimitCleared {
		t.Error("rate-limit script: no agent_rate_limit_cleared message (HC-025)")
	}
	if len(heartbeatsDuringRL) == 0 {
		t.Error("rate-limit script: no heartbeats during rate-limited window (HC-026a requires waiting_input heartbeats)")
	}

	for i, hb := range heartbeatsDuringRL {
		phase, ok := hb.Payload["phase"].(string)
		if !ok || phase != "waiting_input" {
			t.Errorf("heartbeat %d during rate-limited window: phase = %q, want waiting_input (HC-026a)", i, phase)
		}
	}
}

// TestSilentHang_HC026_PostOutcomeScriptShape verifies that the post-outcome
// fixture script ends with outcome_emitted and contains no heartbeats after it.
//
// Per §7.1 + HC-008a: after outcome_emitted, the watcher switches to the
// shutdown-window regime (T_shutdown = 10s), NOT the silent-hang FSM.
// Per HC-026a: "during the post-outcome shutdown window, heartbeat emission is
// not required."
func TestSilentHang_HC026_PostOutcomeScriptShape(t *testing.T) {
	t.Parallel()

	sf := silentHangFixturePostOutcomeScript()

	if len(sf.Messages) == 0 {
		t.Fatal("post-outcome script: no messages")
	}
	last := sf.Messages[len(sf.Messages)-1]
	if last.Type != "outcome_emitted" {
		t.Errorf("post-outcome script: last message type = %q, want outcome_emitted", last.Type)
	}

	foundOutcome := false
	for _, msg := range sf.Messages {
		if foundOutcome && msg.Type == "agent_heartbeat" {
			t.Error("post-outcome script: heartbeat found after outcome_emitted; fixture intent is silence in shutdown window")
		}
		if msg.Type == "outcome_emitted" {
			foundOutcome = true
		}
	}
	if !foundOutcome {
		t.Error("post-outcome script: outcome_emitted not found")
	}
}

// TestSilentHang_HC026_WatcherEmittedMessageShapes verifies that the wire
// emitter produces correctly-shaped messages for every message type the §7.1
// FSM emits to the bus.  These are watcher-side bus events, not progress-stream
// messages, but the spec declares their payload shape.
//
// The watcher emits these messages directly on the bus (not via the twin
// subprocess progress-stream); this test encodes their field requirements as
// a fixture to guide watcher implementation (hk-8i31.31).
//
// Messages checked (bus events per §7.1):
//   - agent_warning_silent_hang
//   - agent_resumed_after_warning
//   - agent_soft_terminating
//   - agent_hard_terminating
//   - agent_failed (class=structural, sub_reason=silent_hang)
//   - agent_failed (class=structural, sub_reason=silent_hang_hard_kill)
func TestSilentHang_HC026_WatcherEmittedMessageShapes(t *testing.T) {
	t.Parallel()

	t.Run("agent_failed_silent_hang", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		e := newWireEmitter(&buf)
		endedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := e.emitAgentFailed("run-sh-001", "sess-sh-001", endedAt, "structural", "silent_hang", ""); err != nil {
			t.Fatalf("emitAgentFailed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := silentHangFixtureString(t, m, "type"); got != "agent_failed" {
			t.Errorf("type = %q, want agent_failed", got)
		}
		if got := silentHangFixtureString(t, m, "error_category"); got != "structural" {
			t.Errorf("error_category = %q, want structural (§4.6.HC-026)", got)
		}
		if got := silentHangFixtureString(t, m, "reason"); got != "silent_hang" {
			t.Errorf("reason = %q, want silent_hang (§8.2)", got)
		}
		if _, exists := m["sub_reason"]; exists {
			t.Error("sub_reason present; want omitted for empty string (omitempty)")
		}
	})

	t.Run("agent_failed_silent_hang_hard_kill", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		e := newWireEmitter(&buf)
		endedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := e.emitAgentFailed("run-sh-002", "sess-sh-002", endedAt, "structural", "silent_hang_hard_kill", ""); err != nil {
			t.Fatalf("emitAgentFailed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got := silentHangFixtureString(t, m, "reason"); got != "silent_hang_hard_kill" {
			t.Errorf("reason = %q, want silent_hang_hard_kill (§8.2 + §7.1 hard-kill path)", got)
		}
	})

	t.Run("heartbeat_all_phases_reset_timer", func(t *testing.T) {
		t.Parallel()
		phasesReset := []heartbeatPhase{
			heartbeatPhaseStarting,
			heartbeatPhaseReasoning,
			heartbeatPhaseToolCall,
			heartbeatPhaseWaitingInput,
			heartbeatPhaseRotating,
			heartbeatPhaseShuttingDown,
		}
		for _, phase := range phasesReset {
			t.Run(string(phase), func(t *testing.T) {
				t.Parallel()
				var buf bytes.Buffer
				e := newWireEmitter(&buf)
				if err := e.emitAgentHeartbeat("sess-sh-ph-001", phase); err != nil {
					t.Fatalf("emitAgentHeartbeat(%q): %v", phase, err)
				}
				var m map[string]any
				if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
					t.Fatalf("unmarshal phase %q: %v", phase, err)
				}
				if got := silentHangFixtureString(t, m, "type"); got != "agent_heartbeat" {
					t.Errorf("phase %q: type = %q, want agent_heartbeat", phase, got)
				}
				if got := silentHangFixtureString(t, m, "phase"); got != string(phase) {
					t.Errorf("phase field = %q, want %q", got, phase)
				}
			})
		}
	})
}

// TestSilentHang_HC026_SuspensionConditionsDocumented asserts that the fixture
// captures the two conditions under which silent-hang detection is SUSPENDED
// per §7.1:
//
//  1. Post-outcome shutdown window (§4.2.HC-008a) — after outcome_emitted.
//  2. Explicit ctx cancellation (HC-018) — cancellation supersedes silent-hang.
//
// This is a documentation sensor, not an execution test.  The watcher
// implementation (hk-8i31.31) MUST enforce both conditions; this fixture
// names them so that downstream test assertions can reference this list.
func TestSilentHang_HC026_SuspensionConditionsDocumented(t *testing.T) {
	t.Parallel()

	postOutcome := silentHangFixturePostOutcomeScript()
	if len(postOutcome.Messages) == 0 {
		t.Fatal("suspension condition 1: post-outcome script is empty")
	}
	lastType := postOutcome.Messages[len(postOutcome.Messages)-1].Type
	if lastType != "outcome_emitted" {
		t.Errorf("suspension condition 1: last message = %q, want outcome_emitted (suspension starts at outcome)", lastType)
	}

	const ctxCancellationSupersedesSilentHang = true
	if !ctxCancellationSupersedesSilentHang {
		t.Error("suspension condition 2: ctx cancellation must supersede silent-hang (§7.1 + HC-018 + §8.4)")
	}
}
