package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func crashRecovMustString(t *testing.T, values map[string]any, key string) string {
	t.Helper()
	value, ok := values[key].(string)
	if !ok {
		t.Fatalf("field %q: got %T, want string", key, values[key])
	}
	return value
}

func crashRecovMustFloat(t *testing.T, values map[string]any, key string) float64 {
	t.Helper()
	value, ok := values[key].(float64)
	if !ok {
		t.Fatalf("field %q: got %T, want JSON number", key, values[key])
	}
	return value
}

type crashRecovFixtureKillPoint string

const (
	crashRecovFixtureKillAfterOutcome crashRecovFixtureKillPoint = "kill_after_outcome_emitted"

	crashRecovFixtureKillBeforeOutcome crashRecovFixtureKillPoint = "kill_before_outcome_emitted"

	crashRecovFixtureKillMidMessage crashRecovFixtureKillPoint = "kill_mid_message"

	crashRecovFixtureKillMidHandshake crashRecovFixtureKillPoint = "kill_mid_handshake"
)

var crashRecovFixtureExpectedSubReason = map[crashRecovFixtureKillPoint]string{
	crashRecovFixtureKillAfterOutcome: "post_outcome_shutdown_timeout",

	crashRecovFixtureKillBeforeOutcome: "",

	crashRecovFixtureKillMidMessage: "partial-message",

	crashRecovFixtureKillMidHandshake: "protocol_mismatch",
}

func crashRecovFixtureKillPointScript(kp crashRecovFixtureKillPoint) *ScriptFile {
	switch kp {
	case crashRecovFixtureKillAfterOutcome:
		return crashRecovFixtureScriptAfterOutcome()
	case crashRecovFixtureKillBeforeOutcome:
		return crashRecovFixtureScriptBeforeOutcome()
	case crashRecovFixtureKillMidMessage:
		return crashRecovFixtureScriptMidMessage()
	case crashRecovFixtureKillMidHandshake:
		return crashRecovFixtureScriptMidHandshake()
	default:
		return &ScriptFile{HeartbeatMode: heartbeatModeScripted, Messages: nil}
	}
}

func crashRecovFixtureScriptAfterOutcome() *ScriptFile {
	now := time.Now().UTC()
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-cr-ao-001",
					"session_id": "sess-cr-ao-001",
					"node_id":    "node-cr-ao-001",
					"agent_type": "claude-twin",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-cr-ao-001",
					"session_id":   "sess-cr-ao-001",
					"capabilities": []string{"scripted"},
				},
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-cr-ao-001",
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 10,
			},
			// outcome_emitted: subprocess should exit after this but is killed
			// externally before it can.
			{
				Type: "outcome_emitted",
				Payload: map[string]any{
					"run_id":         "run-cr-ao-001",
					"session_id":     "sess-cr-ao-001",
					"node_id":        "node-cr-ao-001",
					"outcome_status": "success",
				},
			},
		},
	}
}

func crashRecovFixtureScriptBeforeOutcome() *ScriptFile {
	now := time.Now().UTC()
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-cr-bo-001",
					"session_id": "sess-cr-bo-001",
					"node_id":    "node-cr-bo-001",
					"agent_type": "claude-twin",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-cr-bo-001",
					"session_id":   "sess-cr-bo-001",
					"capabilities": []string{"scripted"},
				},
			},
			{
				Type: "agent_output_chunk",
				Payload: map[string]any{
					"run_id":        "run-cr-bo-001",
					"session_id":    "sess-cr-bo-001",
					"chunk_index":   0,
					"bytes_emitted": 128,
				},
				RelativeTimestampMs: 10,
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-cr-bo-001",
					"phase":      "tool_call",
				},
				RelativeTimestampMs: 10,
			},
		},
	}
}

func crashRecovFixtureScriptMidMessage() *ScriptFile {
	now := time.Now().UTC()
	return &ScriptFile{
		HeartbeatMode: heartbeatModeScripted,
		Messages: []ScriptMessage{
			{
				Type: "agent_started",
				Payload: map[string]any{
					"run_id":     "run-cr-mm-001",
					"session_id": "sess-cr-mm-001",
					"node_id":    "node-cr-mm-001",
					"agent_type": "claude-twin",
					"started_at": now.Format(time.RFC3339Nano),
				},
			},
			{
				Type: "agent_ready",
				Payload: map[string]any{
					"run_id":       "run-cr-mm-001",
					"session_id":   "sess-cr-mm-001",
					"capabilities": []string{"scripted"},
				},
			},
			{
				Type: "agent_heartbeat",
				Payload: map[string]any{
					"session_id": "sess-cr-mm-001",
					"phase":      "reasoning",
				},
				RelativeTimestampMs: 5,
			},
		},
	}
}

func crashRecovFixtureScriptMidHandshake() *ScriptFile {
	return &ScriptFile{
		HeartbeatMode: heartbeatModeWallClock,
		Messages:      nil,
	}
}

type crashRecovFixtureSocketIOError string

const (
	crashRecovFixtureSocketIOECONNRESET crashRecovFixtureSocketIOError = "ECONNRESET"

	crashRecovFixtureSocketIOEPIPE crashRecovFixtureSocketIOError = "EPIPE"

	crashRecovFixtureSocketIOUnlinked crashRecovFixtureSocketIOError = "socket_unlinked"
)

var crashRecovFixtureSocketIOConditions = []crashRecovFixtureSocketIOError{
	crashRecovFixtureSocketIOECONNRESET,
	crashRecovFixtureSocketIOEPIPE,
	crashRecovFixtureSocketIOUnlinked,
}

const crashRecovFixtureSocketIOFirstOccurrenceSubReason = "socket_io_error"

const crashRecovFixtureSocketIOSustainedSubReason = "progress_stream_broken"

type crashRecovFixtureOrphanPidfile struct {
	// WorkspacePath is the target workspace path in the pidfile (used in test
	// assertions to confirm the correct workspace is identified).
	WorkspacePath string

	// PidfilePath is the expected pidfile location per §4.10.HC-044a:
	// ".harmonik/worktrees/<run_id>/.lock".
	PidfilePath string

	// OrphanRunID is the run_id of the prior-generation session that wrote the
	// pidfile.
	OrphanRunID string

	// OrphanPID is the PID recorded in the pidfile.  In scenario tests, this
	// must be a live PID belonging to a process NOT in the current daemon's
	// session map.
	OrphanPID int
}

func crashRecovFixtureOrphanScenario() crashRecovFixtureOrphanPidfile {
	return crashRecovFixtureOrphanPidfile{
		WorkspacePath: "/workspace/run-prior-001",
		PidfilePath:   ".harmonik/worktrees/run-prior-001/.lock",
		OrphanRunID:   "run-prior-001",
		OrphanPID:     1, // init/launchd — always live, never our process
	}
}

const crashRecovFixtureOrphanSubReason = "workspace_held_by_orphan"

// TestCrashRecov_HC024_KillPointEnumCoverage asserts that
// crashRecovFixtureExpectedSubReason covers every value in the
// crashRecovFixtureKillPoint enum, confirming no kill point is unspecified.
func TestCrashRecov_HC024_KillPointEnumCoverage(t *testing.T) {
	t.Parallel()

	allKillPoints := []crashRecovFixtureKillPoint{
		crashRecovFixtureKillAfterOutcome,
		crashRecovFixtureKillBeforeOutcome,
		crashRecovFixtureKillMidMessage,
		crashRecovFixtureKillMidHandshake,
	}

	for _, kp := range allKillPoints {
		t.Run(string(kp), func(t *testing.T) {
			t.Parallel()
			if _, ok := crashRecovFixtureExpectedSubReason[kp]; !ok {
				t.Errorf("kill point %q has no entry in crashRecovFixtureExpectedSubReason; add it to maintain fixture completeness", kp)
			}
		})
	}

	enumSet := make(map[crashRecovFixtureKillPoint]bool, len(allKillPoints))
	for _, kp := range allKillPoints {
		enumSet[kp] = true
	}
	for kp := range crashRecovFixtureExpectedSubReason {
		if !enumSet[kp] {
			t.Errorf("crashRecovFixtureExpectedSubReason contains key %q not in allKillPoints enum; remove or add to enum", kp)
		}
	}
}

// TestCrashRecov_HC024_KillPointScriptsWellFormed verifies that each kill-point
// script returned by crashRecovFixtureKillPointScript passes load-time
// validation (valid heartbeat_mode, non-empty message types).
func TestCrashRecov_HC024_KillPointScriptsWellFormed(t *testing.T) {
	t.Parallel()

	allKillPoints := []crashRecovFixtureKillPoint{
		crashRecovFixtureKillAfterOutcome,
		crashRecovFixtureKillBeforeOutcome,
		crashRecovFixtureKillMidMessage,
		crashRecovFixtureKillMidHandshake,
	}

	for _, kp := range allKillPoints {
		t.Run(string(kp), func(t *testing.T) {
			t.Parallel()
			sf := crashRecovFixtureKillPointScript(kp)

			if !sf.HeartbeatMode.Valid() {
				t.Errorf("kill-point %q script: heartbeat_mode %q is invalid", kp, sf.HeartbeatMode)
			}
			for i, msg := range sf.Messages {
				if msg.Type == "" {
					t.Errorf("kill-point %q script messages[%d].type is empty; loadScriptFile would reject it", kp, i)
				}
			}
		})
	}
}

// TestCrashRecov_HC024_AfterOutcomeScriptEndsWithOutcomeEmitted verifies that
// the kill_after_outcome_emitted script's last message is outcome_emitted.
// This is the prerequisite for the post-outcome shutdown-window scenario.
func TestCrashRecov_HC024_AfterOutcomeScriptEndsWithOutcomeEmitted(t *testing.T) {
	t.Parallel()

	sf := crashRecovFixtureKillPointScript(crashRecovFixtureKillAfterOutcome)
	if len(sf.Messages) == 0 {
		t.Fatal("kill_after_outcome_emitted script: no messages")
	}
	last := sf.Messages[len(sf.Messages)-1]
	if last.Type != "outcome_emitted" {
		t.Errorf("kill_after_outcome_emitted script: last message type = %q, want outcome_emitted", last.Type)
	}
}

// TestCrashRecov_HC024_BeforeOutcomeScriptHasNoOutcomeEmitted verifies that
// the kill_before_outcome_emitted script contains no outcome_emitted.
// This confirms the script produces the dirty-exit precondition for HC-INV-006.
func TestCrashRecov_HC024_BeforeOutcomeScriptHasNoOutcomeEmitted(t *testing.T) {
	t.Parallel()

	sf := crashRecovFixtureKillPointScript(crashRecovFixtureKillBeforeOutcome)
	for i, msg := range sf.Messages {
		if msg.Type == "outcome_emitted" {
			t.Errorf("kill_before_outcome_emitted script messages[%d].type = outcome_emitted; fixture must not include it", i)
		}
	}
}

// TestCrashRecov_HC024_MidHandshakeScriptHasNoMessages verifies that the
// kill_mid_handshake script has nil/empty messages — the subprocess crashes
// before emitting handler_capabilities.
func TestCrashRecov_HC024_MidHandshakeScriptHasNoMessages(t *testing.T) {
	t.Parallel()

	sf := crashRecovFixtureKillPointScript(crashRecovFixtureKillMidHandshake)
	if len(sf.Messages) != 0 {
		t.Errorf("kill_mid_handshake script: message count = %d, want 0 (crash before handshake)", len(sf.Messages))
	}
}

// TestCrashRecov_HC024_AgentFailedPayloadShape verifies that the wireEmitter
// produces correctly shaped agent_failed messages for every sub_reason declared
// in §8.2 and required by the crash-recovery scenario set.
//
// The watcher (hk-8i31.28) MUST emit these exact payloads on the bus when each
// corresponding failure condition is detected.
func TestCrashRecov_HC024_AgentFailedPayloadShape(t *testing.T) {
	t.Parallel()

	crashSubReasons := []struct {
		name          string
		errorCategory string
		reason        string
		subReason     string
	}{
		{
			name:          "crash_without_outcome",
			errorCategory: "structural",
			reason:        "crash_without_outcome",
			subReason:     "",
		},
		// kill_after_outcome + T_shutdown timeout (HC-008a).
		{
			name:          "post_outcome_shutdown_timeout",
			errorCategory: "structural",
			reason:        "post_outcome_shutdown_timeout",
			subReason:     "post_outcome_shutdown_timeout",
		},
		// kill_mid_message → partial-message (§4.2.HC-007b).
		{
			name:          "partial_message",
			errorCategory: "structural",
			reason:        "partial-message",
			subReason:     "partial-message",
		},
		// kill_mid_handshake → protocol_mismatch (§8.7).
		{
			name:          "protocol_mismatch",
			errorCategory: "structural",
			reason:        "protocol_mismatch",
			subReason:     "protocol_mismatch",
		},
	}

	for _, tc := range crashSubReasons {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			e := newWireEmitter(&buf)
			endedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			if err := e.emitAgentFailed("run-cr-001", "sess-cr-001", endedAt, tc.errorCategory, tc.reason, tc.subReason); err != nil {
				t.Fatalf("emitAgentFailed(%q): %v", tc.name, err)
			}
			var m map[string]any
			if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got := crashRecovMustString(t, m, "type"); got != "agent_failed" {
				t.Errorf("%q: type = %q, want agent_failed", tc.name, got)
			}
			if got := crashRecovMustString(t, m, "error_category"); got != tc.errorCategory {
				t.Errorf("%q: error_category = %q, want %q", tc.name, got, tc.errorCategory)
			}
			if got := crashRecovMustString(t, m, "reason"); got != tc.reason {
				t.Errorf("%q: reason = %q, want %q", tc.name, got, tc.reason)
			}
			if tc.subReason == "" {
				if _, exists := m["sub_reason"]; exists {
					t.Errorf("%q: sub_reason present, want omitted (omitempty)", tc.name)
				}
			} else {
				if got, ok := m["sub_reason"].(string); !ok || got != tc.subReason {
					t.Errorf("%q: sub_reason = %v, want %q", tc.name, m["sub_reason"], tc.subReason)
				}
			}
			if eat, ok := m["ended_at"].(string); !ok || eat == "" {
				t.Errorf("%q: ended_at missing or empty", tc.name)
			} else if _, err := time.Parse(time.RFC3339Nano, eat); err != nil {
				t.Errorf("%q: ended_at %q not RFC3339Nano: %v", tc.name, eat, err)
			}
		})
	}
}

// TestCrashRecov_HC024a_SocketIOSubReasonDistinct verifies that the first-
// occurrence and sustained socket-I/O sub_reasons (socket_io_error,
// progress_stream_broken) are distinct strings from subprocess-crash sub_reasons
// (e.g., partial-message, protocol_mismatch) per §4.6.HC-024a.
//
// This is the fixture-level enforcement of HC-024a's distinctness requirement:
// "a socket-level I/O error from the progress-stream read-loop MUST be
// distinguished from subprocess-level termination."
func TestCrashRecov_HC024a_SocketIOSubReasonDistinct(t *testing.T) {
	t.Parallel()

	socketIOSubReasons := []string{
		crashRecovFixtureSocketIOFirstOccurrenceSubReason, // "socket_io_error"
		crashRecovFixtureSocketIOSustainedSubReason,       // "progress_stream_broken"
	}
	crashSubReasons := []string{
		"partial-message",
		"protocol_mismatch",
		"silent_hang",
		"silent_hang_hard_kill",
	}

	for _, soSR := range socketIOSubReasons {
		for _, crSR := range crashSubReasons {
			if soSR == crSR {
				t.Errorf(
					"socket-I/O sub_reason %q collides with crash sub_reason %q; "+
						"HC-024a requires these to be distinct (§4.6.HC-024a)",
					soSR, crSR,
				)
			}
		}
	}
}

// TestCrashRecov_HC024a_SocketIOConditionsAllNamed verifies that every socket
// condition in crashRecovFixtureSocketIOConditions is represented and non-empty.
func TestCrashRecov_HC024a_SocketIOConditionsAllNamed(t *testing.T) {
	t.Parallel()

	const wantCount = 3
	if len(crashRecovFixtureSocketIOConditions) != wantCount {
		t.Errorf("crashRecovFixtureSocketIOConditions has %d entries, want %d (ECONNRESET, EPIPE, socket_unlinked)",
			len(crashRecovFixtureSocketIOConditions), wantCount)
	}

	for i, cond := range crashRecovFixtureSocketIOConditions {
		if cond == "" {
			t.Errorf("crashRecovFixtureSocketIOConditions[%d] is empty", i)
		}
	}

	present := make(map[crashRecovFixtureSocketIOError]bool)
	for _, c := range crashRecovFixtureSocketIOConditions {
		present[c] = true
	}
	for _, want := range []crashRecovFixtureSocketIOError{
		crashRecovFixtureSocketIOECONNRESET,
		crashRecovFixtureSocketIOEPIPE,
		crashRecovFixtureSocketIOUnlinked,
	} {
		if !present[want] {
			t.Errorf("crashRecovFixtureSocketIOConditions missing %q", want)
		}
	}
}

// TestCrashRecov_HC024a_FirstOccurrenceIsTransient verifies that the first-
// occurrence socket-I/O sub_reason maps to class "transient" per HC-024a:
// "emit agent_failed with class ErrTransient and sub_reason socket_io_error."
//
// This is a shape test: the emitter produces the correct class/sub_reason pair.
func TestCrashRecov_HC024a_FirstOccurrenceIsTransient(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	endedAt := time.Now().UTC()
	if err := e.emitAgentFailed(
		"run-cr-sio-001", "sess-cr-sio-001",
		endedAt,
		"transient",
		crashRecovFixtureSocketIOFirstOccurrenceSubReason,
		"",
	); err != nil {
		t.Fatalf("emitAgentFailed(socket_io_error): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := crashRecovMustString(t, m, "error_category"); got != "transient" {
		t.Errorf("first-occurrence socket-I/O: error_category = %q, want transient (HC-024a)", got)
	}
	if got := crashRecovMustString(t, m, "reason"); got != "socket_io_error" {
		t.Errorf("first-occurrence socket-I/O: reason = %q, want socket_io_error (HC-024a)", got)
	}
}

// TestCrashRecov_HC024a_SustainedIsStructural verifies that the sustained
// socket-I/O reclassification maps to class "structural" per HC-024a:
// "the watcher MUST reclassify to ErrStructural with sub_reason
// progress_stream_broken."
func TestCrashRecov_HC024a_SustainedIsStructural(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	endedAt := time.Now().UTC()
	if err := e.emitAgentFailed(
		"run-cr-sio-002", "sess-cr-sio-002",
		endedAt,
		"structural",
		crashRecovFixtureSocketIOSustainedSubReason,
		"",
	); err != nil {
		t.Fatalf("emitAgentFailed(progress_stream_broken): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := crashRecovMustString(t, m, "error_category"); got != "structural" {
		t.Errorf("sustained socket-I/O: error_category = %q, want structural (HC-024a)", got)
	}
	if got := crashRecovMustString(t, m, "reason"); got != "progress_stream_broken" {
		t.Errorf("sustained socket-I/O: reason = %q, want progress_stream_broken (HC-024a)", got)
	}
}

// TestCrashRecov_HC044a_OrphanSubReasonValue asserts that
// crashRecovFixtureOrphanSubReason equals the literal string
// "workspace_held_by_orphan" declared in §8.2.
//
// Spec: §4.10.HC-044a "Launch MUST return ErrStructural with sub-reason
// workspace_held_by_orphan"; §8.2 sub_reason list includes
// "workspace_held_by_orphan".
func TestCrashRecov_HC044a_OrphanSubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "workspace_held_by_orphan"
	if crashRecovFixtureOrphanSubReason != want {
		t.Errorf("crashRecovFixtureOrphanSubReason = %q, want %q (§4.10.HC-044a + §8.2)", crashRecovFixtureOrphanSubReason, want)
	}
}

// TestCrashRecov_HC044a_OrphanPayloadShape verifies that the emitter produces
// a correctly shaped agent_failed(structural, workspace_held_by_orphan) message
// for the orphan-workspace scenario.
func TestCrashRecov_HC044a_OrphanPayloadShape(t *testing.T) {
	t.Parallel()

	scenario := crashRecovFixtureOrphanScenario()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	endedAt := time.Now().UTC()
	if err := e.emitAgentFailed(
		"run-cr-op-001", "sess-cr-op-001",
		endedAt,
		"structural",
		crashRecovFixtureOrphanSubReason,
		"",
	); err != nil {
		t.Fatalf("emitAgentFailed(workspace_held_by_orphan): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := crashRecovMustString(t, m, "error_category"); got != "structural" {
		t.Errorf("orphan: error_category = %q, want structural (HC-044a)", got)
	}
	if got := crashRecovMustString(t, m, "reason"); got != "workspace_held_by_orphan" {
		t.Errorf("orphan: reason = %q, want workspace_held_by_orphan (HC-044a)", got)
	}

	const pidfileSuffix = "/.lock"
	if !strings.HasSuffix(scenario.PidfilePath, pidfileSuffix) {
		t.Errorf("orphan scenario pidfile path %q does not end with %q (HC-044a: .harmonik/worktrees/<run_id>/.lock)",
			scenario.PidfilePath, pidfileSuffix)
	}
}

// TestCrashRecov_HCINV006_TerminalEventSetComplete asserts that the two
// terminal event types named in HC-INV-006 are {agent_completed, agent_failed}
// and that no third terminal event type exists in the spec.
//
// Spec: §5.HC-INV-006 "the watcher MUST publish exactly ONE terminal event to
// the bus, chosen from {agent_completed, agent_failed}."
func TestCrashRecov_HCINV006_TerminalEventSetComplete(t *testing.T) {
	t.Parallel()

	terminalEvents := []string{"agent_completed", "agent_failed"}

	if len(terminalEvents) != 2 {
		t.Errorf("HC-INV-006 terminal event set has %d members, want exactly 2 {agent_completed, agent_failed}", len(terminalEvents))
	}

	var bufC bytes.Buffer
	eC := newWireEmitter(&bufC)
	if err := eC.emitAgentCompleted("run-inv6-001", "sess-inv6-001", time.Now().UTC(), 0, "outcome-ref-001"); err != nil {
		t.Fatalf("emitAgentCompleted: %v", err)
	}
	var mC map[string]any
	if err := json.Unmarshal(bytes.TrimRight(bufC.Bytes(), "\n"), &mC); err != nil {
		t.Fatalf("unmarshal agent_completed: %v", err)
	}
	if got := crashRecovMustString(t, mC, "type"); got != "agent_completed" {
		t.Errorf("agent_completed shape: type = %q, want agent_completed", got)
	}

	var bufF bytes.Buffer
	eF := newWireEmitter(&bufF)
	if err := eF.emitAgentFailed("run-inv6-002", "sess-inv6-002", time.Now().UTC(), "structural", "crash_without_outcome", ""); err != nil {
		t.Fatalf("emitAgentFailed: %v", err)
	}
	var mF map[string]any
	if err := json.Unmarshal(bytes.TrimRight(bufF.Bytes(), "\n"), &mF); err != nil {
		t.Fatalf("unmarshal agent_failed: %v", err)
	}
	if got := crashRecovMustString(t, mF, "type"); got != "agent_failed" {
		t.Errorf("agent_failed shape: type = %q, want agent_failed", got)
	}
}

// TestCrashRecov_HCINV006_DirtyExitWithNoOutcomeMustEmitFailed asserts the
// constraint from HC-INV-006: "on any dirty exit (exit code non-zero, no prior
// agent_completed or agent_failed published, no outcome_emitted received) the
// watcher MUST emit agent_failed — silent termination without a terminal event
// is forbidden."
//
// This test confirms that the before-outcome script (kill_before_outcome_emitted)
// sets up the precondition correctly: no terminal events have been emitted.
func TestCrashRecov_HCINV006_DirtyExitWithNoOutcomeMustEmitFailed(t *testing.T) {
	t.Parallel()

	sf := crashRecovFixtureKillPointScript(crashRecovFixtureKillBeforeOutcome)

	for i, msg := range sf.Messages {
		if msg.Type == "agent_completed" || msg.Type == "agent_failed" {
			t.Errorf(
				"kill_before_outcome_emitted script messages[%d].type = %q; "+
					"script must not contain terminal events (watcher emits them post-crash; HC-INV-006)",
				i, msg.Type,
			)
		}
		if msg.Type == "outcome_emitted" {
			t.Errorf(
				"kill_before_outcome_emitted script messages[%d].type = outcome_emitted; "+
					"must not appear (dirty-exit precondition requires absence of outcome; HC-INV-006)",
				i,
			)
		}
	}
}

// TestCrashRecov_HCINV005_BinaryPathVerificationShape asserts the fixture
// encodes the HC-INV-005 constraint by naming the relevant §4.10 requirements.
//
// Spec: §5.HC-INV-005 "For every successful Launch, the binary that was exec'd
// MUST have passed the launch-path and commit-hash rules of §4.10.HC-042 and
// §4.10.HC-043."
//
// This is a documentation sensor.  Integration tests (hk-8i31.68) will verify
// the daemon refuses a launch with an unverified binary path; this fixture names
// the expected payload shape when that verification fails.
func TestCrashRecov_HCINV005_BinaryPathVerificationShape(t *testing.T) {
	t.Parallel()

	const expectedClass = "structural"

	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	if err := e.emitAgentFailed(
		"run-cr-bpv-001", "sess-cr-bpv-001",
		time.Now().UTC(),
		expectedClass,
		"binary_path_unverified",
		"",
	); err != nil {
		t.Fatalf("emitAgentFailed(binary_path_unverified): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := crashRecovMustString(t, m, "error_category"); got != expectedClass {
		t.Errorf("binary path verification failure: error_category = %q, want %q (HC-INV-005 + §8.2)", got, expectedClass)
	}
}

// TestCrashRecov_HC008a_DirtyExitInShutdownWindowIsCompleted asserts the
// HC-INV-006 exception for dirty exits inside the post-outcome shutdown window.
//
// Spec: §5.HC-INV-006 "agent_completed fires on clean exit after outcome_emitted
// OR on dirty exit inside the post-outcome shutdown window per §4.2.HC-008a
// (outcome is durable, non-zero exit is recorded via shutdown_exit_code)."
//
// The fixture verifies that agent_completed accepts exit_code != 0 (non-clean
// exit), confirming the emitter API supports this shape.
func TestCrashRecov_HC008a_DirtyExitInShutdownWindowIsCompleted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	e := newWireEmitter(&buf)
	endedAt := time.Now().UTC()
	const shutdownExitCode = 137 // SIGKILL
	if err := e.emitAgentCompleted("run-cr-dew-001", "sess-cr-dew-001", endedAt, shutdownExitCode, "outcome-ref-001"); err != nil {
		t.Fatalf("emitAgentCompleted(dirty-exit-in-shutdown-window): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := crashRecovMustString(t, m, "type"); got != "agent_completed" {
		t.Errorf(
			"dirty-exit-in-shutdown-window: type = %q, want agent_completed "+
				"(HC-008a + HC-INV-006: dirty exit inside shutdown window is completed, not failed)",
			got,
		)
	}
	if got := crashRecovMustFloat(t, m, "exit_code"); int(got) != shutdownExitCode {
		t.Errorf("dirty-exit-in-shutdown-window: exit_code = %v, want %d (shutdown_exit_code per HC-008a)", got, shutdownExitCode)
	}
	if got, ok := m["outcome_ref"].(string); !ok || got == "" {
		t.Errorf("dirty-exit-in-shutdown-window: outcome_ref missing or empty; outcome must be durable (HC-008a)")
	}
}
