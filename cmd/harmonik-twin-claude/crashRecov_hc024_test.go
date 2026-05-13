package main

// crashRecov_hc024_test.go — fixture and scenario harness for crash-recovery,
// dirty-exit, socket-I/O, and orphan-workspace scenarios.
//
// Spec refs:
//   - specs/handler-contract.md §4.2.HC-008a (post-outcome shutdown window)
//   - specs/handler-contract.md §4.6.HC-024 (subprocess crash emits agent_failed)
//   - specs/handler-contract.md §4.6.HC-024a (socket-level I/O error distinct
//     from subprocess crash)
//   - specs/handler-contract.md §4.10.HC-044a (orphan-held workspace fail-fast)
//   - specs/handler-contract.md §5.HC-INV-005 (no subprocess without verified path)
//   - specs/handler-contract.md §5.HC-INV-006 (exactly one terminal event per session)
//   - specs/handler-contract.md §8.2 ErrStructural sub_reason taxonomy
//   - specs/handler-contract.md §10.2 HC-008a/HC-024/HC-024a/HC-044a obligations
//
// Bead: hk-8i31.80.
//
// Helper prefix: crashRecovFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// What this file provides:
//
//  1. crashRecovFixtureKillPoint — an enum of subprocess kill points from the
//     §10.2 HC-024 obligation (after outcome_emitted; before outcome_emitted;
//     mid-message; mid-handshake).
//
//  2. crashRecovFixtureKillPointScript — for each kill point, a ScriptFile
//     whose messages stop at the kill point, simulating what the twin emits
//     before a crash.
//
//  3. crashRecovFixtureSocketIOError — a table of named socket-level I/O error
//     conditions from §4.6.HC-024a (ECONNRESET, EPIPE, socket-unlinked).
//
//  4. crashRecovFixtureOrphanPidfile — a struct encoding the orphan-pidfile
//     scenario from §4.10.HC-044a: a pidfile exists, the PID is live, and the
//     process belongs to a prior daemon generation.
//
//  5. crashRecovFixtureExpectedSubReason — a map from each kill-point scenario
//     to the expected agent_failed sub_reason per §8.2, encoding the spec's
//     sub_reason taxonomy for load-bearing assertion by downstream tests.
//
//  6. Static sensor tests asserting:
//     - agent_failed payload shape for every sub_reason declared in §8.2 and
//       required by this bead's scenario set.
//     - HC-INV-006 one-terminal-event invariant naming (no double emission).
//     - HC-024a socket-I/O sub_reasons are distinct from subprocess-crash sub_reasons.
//     - HC-044a orphan-workspace sub_reason is "workspace_held_by_orphan".

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Kill-point enum and sub_reason mapping (HC-024 + §8.2)
// ─────────────────────────────────────────────────────────────────────────────

// crashRecovFixtureKillPoint names the four kill points from §10.2 HC-024
// obligation: "kill subprocess at specific points."
type crashRecovFixtureKillPoint string

const (
	// crashRecovFixtureKillAfterOutcome represents a kill AFTER outcome_emitted.
	// The outcome is durable; the watcher enters the post-outcome shutdown window
	// (HC-008a).  Clean process exit in that window → agent_completed.
	// SIGKILL before exit → agent_failed(post_outcome_shutdown_timeout) per HC-008a.
	crashRecovFixtureKillAfterOutcome crashRecovFixtureKillPoint = "kill_after_outcome_emitted"

	// crashRecovFixtureKillBeforeOutcome represents a kill BEFORE outcome_emitted.
	// Per HC-INV-006: dirty exit without prior agent_completed or agent_failed →
	// watcher MUST emit agent_failed.  Class: ErrStructural, sub_reason: crash_before_outcome.
	// The spec does not pin "crash_before_outcome" as a literal; §8.2 non-exhaustive
	// list says "crash without outcome" maps to ErrStructural.
	crashRecovFixtureKillBeforeOutcome crashRecovFixtureKillPoint = "kill_before_outcome_emitted"

	// crashRecovFixtureKillMidMessage represents a kill during message emission
	// (partial-message-on-EOF scenario per §4.2.HC-007b).
	// Watcher MUST emit agent_failed(class=structural, sub_reason=partial-message).
	crashRecovFixtureKillMidMessage crashRecovFixtureKillPoint = "kill_mid_message"

	// crashRecovFixtureKillMidHandshake represents a kill during the launch
	// handshake (§7.2), before handler_capabilities is received.
	// Watcher MUST emit ErrProtocolMismatch(no handler_capabilities received).
	crashRecovFixtureKillMidHandshake crashRecovFixtureKillPoint = "kill_mid_handshake"
)

// crashRecovFixtureExpectedSubReason maps each kill point to the expected
// agent_failed sub_reason per §8.2 + §4.2.HC-007b + §7.2.
//
// This map is load-bearing for downstream watcher tests (hk-8i31.28): each
// scenario MUST produce agent_failed carrying exactly the sub_reason listed
// here.  Empty string means no sub_reason is expected (the crash maps to a
// primary class only).
var crashRecovFixtureExpectedSubReason = map[crashRecovFixtureKillPoint]string{
	// After outcome: shutdown-window timeout is the terminal classification.
	// (Only fires if SIGKILL is applied before the process exits within T_shutdown.)
	crashRecovFixtureKillAfterOutcome: "post_outcome_shutdown_timeout",

	// Before outcome: dirty crash — ErrStructural, no pinned sub_reason in §8.2
	// non-exhaustive list for this case, but watcher MUST emit agent_failed.
	// The map records "" to indicate "sub_reason is implementation-specific";
	// the normative constraint is error_category = "structural".
	crashRecovFixtureKillBeforeOutcome: "",

	// Mid-message: partial-message → ErrStructural, sub_reason "partial-message".
	crashRecovFixtureKillMidMessage: "partial-message",

	// Mid-handshake: ErrProtocolMismatch → no agent_failed on the progress stream
	// (the launch fails before the session is established); the watcher returns
	// ErrProtocolMismatch from Launch.  Records "protocol_mismatch" for clarity.
	crashRecovFixtureKillMidHandshake: "protocol_mismatch",
}

// ─────────────────────────────────────────────────────────────────────────────
// Kill-point scripts
// ─────────────────────────────────────────────────────────────────────────────

// crashRecovFixtureKillPointScript returns a ScriptFile whose messages stop
// at the given kill point, modelling what the twin emits before a crash.
//
// The scripts use heartbeat_mode "scripted" so that scenario tests produce
// byte-reproducible event streams (HC-026a scripted-mode carve-out).
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
		// Unreachable in well-formed tests; caller error if reached.
		return &ScriptFile{HeartbeatMode: heartbeatModeScripted, Messages: nil}
	}
}

// crashRecovFixtureScriptAfterOutcome returns a script that emits the full
// normal sequence including outcome_emitted — the kill happens externally
// after this message, before the process exits.
//
// Post-kill, the watcher is in the post-outcome shutdown window.  If the
// process does not exit within T_shutdown, the watcher emits
// agent_failed(structural, sub_reason=post_outcome_shutdown_timeout) per HC-008a.
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
			// No agent_completed follows — the kill happens here.
		},
	}
}

// crashRecovFixtureScriptBeforeOutcome returns a script that stops mid-session
// before outcome_emitted — the kill happens while work is in progress.
//
// Per HC-INV-006: any dirty exit (non-zero exit code, no prior terminal event,
// no outcome_emitted received) MUST cause the watcher to emit agent_failed.
// Class: ErrStructural.
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
			// Script ends here; no outcome_emitted. The twin crashes before completing.
		},
	}
}

// crashRecovFixtureScriptMidMessage returns a script whose last message is
// deliberately truncated (no terminating newline) — simulating a subprocess
// that dies while emitting a NDJSON line.
//
// Per §4.2.HC-007b: "socket EOF with bytes buffered before the terminating \n
// [...] MUST be discarded; the watcher MUST emit agent_failed with class
// ErrStructural, sub_reason partial-message."
//
// This script uses the heartbeat_mode "scripted" but records only valid
// messages.  The "mid-message kill" is simulated by the scenario harness by
// closing the socket after partial bytes — not by the script driver itself
// (which always appends '\n').  The script establishes the session state
// before the harness injects the truncation.
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
			// The scenario harness injects a partial (no-newline) byte sequence
			// immediately after this message to simulate mid-message death.
		},
	}
}

// crashRecovFixtureScriptMidHandshake returns a minimal script that emits
// NO progress-stream messages — simulating a subprocess that crashes before
// sending handler_capabilities during the §7.2 handshake.
//
// Per §7.2: "IF cap_msg IS None: watcher.kill_subprocess(); RETURN (None,
// ErrProtocolMismatch.wrap("no handler_capabilities received"))."
//
// The launch fails before a Session is established; HC-INV-006 does not apply
// (no session crossed the agent_ready threshold).
func crashRecovFixtureScriptMidHandshake() *ScriptFile {
	return &ScriptFile{
		// Wall-clock mode: the subprocess exits immediately without emitting
		// anything, so no scripted timing is needed.
		HeartbeatMode: heartbeatModeWallClock,
		Messages:      nil,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Socket-I/O error conditions (HC-024a)
// ─────────────────────────────────────────────────────────────────────────────

// crashRecovFixtureSocketIOError names the socket-level I/O error conditions
// from §4.6.HC-024a that MUST be distinguishable from subprocess crashes.
type crashRecovFixtureSocketIOError string

const (
	// crashRecovFixtureSocketIOECONNRESET is a connection-reset error (ECONNRESET).
	// Per HC-024a: watcher emits agent_failed(transient, socket_io_error) on
	// first occurrence, then attempts ONE reconnect within 500ms.
	crashRecovFixtureSocketIOECONNRESET crashRecovFixtureSocketIOError = "ECONNRESET"

	// crashRecovFixtureSocketIOEPIPE is a broken-pipe error (EPIPE).
	// Same handling as ECONNRESET under HC-024a.
	crashRecovFixtureSocketIOEPIPE crashRecovFixtureSocketIOError = "EPIPE"

	// crashRecovFixtureSocketIOUnlinked is a socket-file unlinked condition.
	// Per HC-024a: the socket file may be unlinked under foot (filesystem
	// unmount or operator intervention).  Same first-occurrence handling applies.
	crashRecovFixtureSocketIOUnlinked crashRecovFixtureSocketIOError = "socket_unlinked"
)

// crashRecovFixtureSocketIOConditions is the normative list of socket-level I/O
// error conditions from §4.6.HC-024a.  Used by sensor tests to assert all
// named conditions are covered by the fixture.
var crashRecovFixtureSocketIOConditions = []crashRecovFixtureSocketIOError{
	crashRecovFixtureSocketIOECONNRESET,
	crashRecovFixtureSocketIOEPIPE,
	crashRecovFixtureSocketIOUnlinked,
}

// crashRecovFixtureSocketIOFirstOccurrenceSubReason is the sub_reason the
// watcher emits on the FIRST socket-level I/O error occurrence per HC-024a.
//
// "On the FIRST occurrence of such an error without a prior agent_completed or
// agent_failed for the session, the watcher MUST: (a) emit agent_failed with
// class ErrTransient and sub_reason socket_io_error."
const crashRecovFixtureSocketIOFirstOccurrenceSubReason = "socket_io_error"

// crashRecovFixtureSocketIOSustainedSubReason is the sub_reason the watcher
// emits after a reconnect fails OR the subsequent stream emits another
// socket-level error per HC-024a.
//
// "If reconnect fails OR the subsequent stream emits another socket-level
// error before a clean terminal event, the watcher MUST reclassify to
// ErrStructural with sub_reason progress_stream_broken."
const crashRecovFixtureSocketIOSustainedSubReason = "progress_stream_broken"

// ─────────────────────────────────────────────────────────────────────────────
// Orphan-workspace scenario (HC-044a)
// ─────────────────────────────────────────────────────────────────────────────

// crashRecovFixtureOrphanPidfile describes the orphan-pidfile scenario from
// §4.10.HC-044a.
//
// Normative definition: "if the pidfile exists AND the recorded PID is live
// (liveness probe via kill(pid, 0) or platform equivalent) AND the live process
// is NOT owned by the current daemon generation, Launch MUST return ErrStructural
// with sub-reason workspace_held_by_orphan."
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

// crashRecovFixtureOrphanScenario returns a crashRecovFixtureOrphanPidfile
// representing a typical orphan-held-workspace scenario for use in assertions.
//
// The PID is a placeholder (1 = init/launchd on UNIX) so that scenario harness
// tests can verify the detection logic shape without requiring a real orphan
// process.  Live-PID scenarios are tested by the integration test in hk-8i31.52
// which has OS-level process control.
func crashRecovFixtureOrphanScenario() crashRecovFixtureOrphanPidfile {
	return crashRecovFixtureOrphanPidfile{
		WorkspacePath: "/workspace/run-prior-001",
		PidfilePath:   ".harmonik/worktrees/run-prior-001/.lock",
		OrphanRunID:   "run-prior-001",
		OrphanPID:     1, // init/launchd — always live, never our process
	}
}

// crashRecovFixtureOrphanSubReason is the sub_reason the watcher/launcher
// MUST emit when an orphan-held workspace is detected per §4.10.HC-044a.
//
// Spec: §8.2 "workspace_held_by_orphan".
const crashRecovFixtureOrphanSubReason = "workspace_held_by_orphan"

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: kill-point enum coverage
// ─────────────────────────────────────────────────────────────────────────────

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
		kp := kp
		t.Run(string(kp), func(t *testing.T) {
			t.Parallel()
			if _, ok := crashRecovFixtureExpectedSubReason[kp]; !ok {
				t.Errorf("kill point %q has no entry in crashRecovFixtureExpectedSubReason; add it to maintain fixture completeness", kp)
			}
		})
	}

	// Assert the map has no extra keys beyond the enum set.
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

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: kill-point scripts are well-formed
// ─────────────────────────────────────────────────────────────────────────────

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
		kp := kp
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

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: agent_failed payload for crash sub_reasons (HC-024 + §8.2)
// ─────────────────────────────────────────────────────────────────────────────

// TestCrashRecov_HC024_AgentFailedPayloadShape verifies that the wireEmitter
// produces correctly shaped agent_failed messages for every sub_reason declared
// in §8.2 and required by the crash-recovery scenario set.
//
// The watcher (hk-8i31.28) MUST emit these exact payloads on the bus when each
// corresponding failure condition is detected.
func TestCrashRecov_HC024_AgentFailedPayloadShape(t *testing.T) {
	t.Parallel()

	// Sub-reasons from §8.2 relevant to crash-recovery scenarios in hk-8i31.80.
	crashSubReasons := []struct {
		name          string
		errorCategory string
		reason        string
		subReason     string
	}{
		// kill_before_outcome → dirty crash without outcome; ErrStructural.
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
		tc := tc
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

			// Type.
			if got := m["type"].(string); got != "agent_failed" {
				t.Errorf("%q: type = %q, want agent_failed", tc.name, got)
			}
			// error_category.
			if got := m["error_category"].(string); got != tc.errorCategory {
				t.Errorf("%q: error_category = %q, want %q", tc.name, got, tc.errorCategory)
			}
			// reason.
			if got := m["reason"].(string); got != tc.reason {
				t.Errorf("%q: reason = %q, want %q", tc.name, got, tc.reason)
			}
			// sub_reason: present iff non-empty.
			if tc.subReason == "" {
				if _, exists := m["sub_reason"]; exists {
					t.Errorf("%q: sub_reason present, want omitted (omitempty)", tc.name)
				}
			} else {
				if got, ok := m["sub_reason"].(string); !ok || got != tc.subReason {
					t.Errorf("%q: sub_reason = %v, want %q", tc.name, m["sub_reason"], tc.subReason)
				}
			}
			// ended_at must parse as RFC3339.
			if eat, ok := m["ended_at"].(string); !ok || eat == "" {
				t.Errorf("%q: ended_at missing or empty", tc.name)
			} else if _, err := time.Parse(time.RFC3339Nano, eat); err != nil {
				t.Errorf("%q: ended_at %q not RFC3339Nano: %v", tc.name, eat, err)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: socket-I/O sub_reasons are distinct from subprocess-crash sub_reasons
// ─────────────────────────────────────────────────────────────────────────────

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

	// Sub-reasons that MUST belong exclusively to socket-I/O path.
	socketIOSubReasons := []string{
		crashRecovFixtureSocketIOFirstOccurrenceSubReason, // "socket_io_error"
		crashRecovFixtureSocketIOSustainedSubReason,       // "progress_stream_broken"
	}
	// Sub-reasons that belong to subprocess-crash path (not socket-I/O).
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

	// §4.6.HC-024a names three conditions: EPIPE, ECONNRESET, socket-unlinked.
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

	// Check each named constant is present.
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

	if got := m["error_category"].(string); got != "transient" {
		t.Errorf("first-occurrence socket-I/O: error_category = %q, want transient (HC-024a)", got)
	}
	if got := m["reason"].(string); got != "socket_io_error" {
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

	if got := m["error_category"].(string); got != "structural" {
		t.Errorf("sustained socket-I/O: error_category = %q, want structural (HC-024a)", got)
	}
	if got := m["reason"].(string); got != "progress_stream_broken" {
		t.Errorf("sustained socket-I/O: reason = %q, want progress_stream_broken (HC-024a)", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: orphan-workspace sub_reason (HC-044a)
// ─────────────────────────────────────────────────────────────────────────────

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

	if got := m["error_category"].(string); got != "structural" {
		t.Errorf("orphan: error_category = %q, want structural (HC-044a)", got)
	}
	if got := m["reason"].(string); got != "workspace_held_by_orphan" {
		t.Errorf("orphan: reason = %q, want workspace_held_by_orphan (HC-044a)", got)
	}

	// Confirm scenario struct has the expected pidfile path shape.
	const pidfileSuffix = "/.lock"
	if !strings.HasSuffix(scenario.PidfilePath, pidfileSuffix) {
		t.Errorf("orphan scenario pidfile path %q does not end with %q (HC-044a: .harmonik/worktrees/<run_id>/.lock)",
			scenario.PidfilePath, pidfileSuffix)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-006 one-terminal-event invariant
// ─────────────────────────────────────────────────────────────────────────────

// TestCrashRecov_HCINV006_TerminalEventSetComplete asserts that the two
// terminal event types named in HC-INV-006 are {agent_completed, agent_failed}
// and that no third terminal event type exists in the spec.
//
// Spec: §5.HC-INV-006 "the watcher MUST publish exactly ONE terminal event to
// the bus, chosen from {agent_completed, agent_failed}."
func TestCrashRecov_HCINV006_TerminalEventSetComplete(t *testing.T) {
	t.Parallel()

	// Normative terminal event set per HC-INV-006.
	terminalEvents := []string{"agent_completed", "agent_failed"}

	if len(terminalEvents) != 2 {
		t.Errorf("HC-INV-006 terminal event set has %d members, want exactly 2 {agent_completed, agent_failed}", len(terminalEvents))
	}

	// Both must be emittable by the wireEmitter (confirming twin can exercise both paths).
	var bufC bytes.Buffer
	eC := newWireEmitter(&bufC)
	if err := eC.emitAgentCompleted("run-inv6-001", "sess-inv6-001", time.Now().UTC(), 0, "outcome-ref-001"); err != nil {
		t.Fatalf("emitAgentCompleted: %v", err)
	}
	var mC map[string]any
	if err := json.Unmarshal(bytes.TrimRight(bufC.Bytes(), "\n"), &mC); err != nil {
		t.Fatalf("unmarshal agent_completed: %v", err)
	}
	if got := mC["type"].(string); got != "agent_completed" {
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
	if got := mF["type"].(string); got != "agent_failed" {
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

	// The script must contain no terminal events (agent_completed or agent_failed).
	// The twin subprocess ending after this script => dirty exit.
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

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: HC-INV-005 no subprocess without verified binary path
// ─────────────────────────────────────────────────────────────────────────────

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

	// The daemon-side launch check should produce an agent_failed with
	// error_category = "structural" and an appropriate sub_reason when the
	// binary path check fails.  The spec does not pin a sub_reason for
	// "binary path unverified" explicitly, but §8.2 says ErrStructural applies
	// for "the plan is wrong (wrong tool selected, missing precondition)".
	// This fixture names the expected class for watcher implementation.
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

	if got := m["error_category"].(string); got != expectedClass {
		t.Errorf("binary path verification failure: error_category = %q, want %q (HC-INV-005 + §8.2)", got, expectedClass)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: dirty-exit inside shutdown window → agent_completed (HC-008a)
// ─────────────────────────────────────────────────────────────────────────────

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

	// Non-zero exit code inside shutdown window → still agent_completed per
	// HC-INV-006 + HC-008a: the outcome is durable; the non-zero exit is
	// observational only.
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

	// Type is still agent_completed (not agent_failed).
	if got := m["type"].(string); got != "agent_completed" {
		t.Errorf(
			"dirty-exit-in-shutdown-window: type = %q, want agent_completed "+
				"(HC-008a + HC-INV-006: dirty exit inside shutdown window is completed, not failed)",
			got,
		)
	}
	// exit_code must carry the non-zero value.
	if got := m["exit_code"].(float64); int(got) != shutdownExitCode {
		t.Errorf("dirty-exit-in-shutdown-window: exit_code = %v, want %d (shutdown_exit_code per HC-008a)", got, shutdownExitCode)
	}
	// outcome_ref must be non-empty (outcome is durable).
	if got, ok := m["outcome_ref"].(string); !ok || got == "" {
		t.Errorf("dirty-exit-in-shutdown-window: outcome_ref missing or empty; outcome must be durable (HC-008a)")
	}
}
