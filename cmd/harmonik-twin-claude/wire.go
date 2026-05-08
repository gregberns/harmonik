// Wire-protocol progress-stream emitter for the canonical twin binary.
//
// This file implements the NDJSON-framed message emitter that the twin sends
// over the Unix-domain socket to the daemon-side watcher, satisfying the
// parity surface declared in specs/handler-contract.md §4.8 (HC-035..HC-040).
//
// # Message types emitted (HC-007, HC-036)
//
//   - handler_capabilities  — first message on stream (HC-009)
//   - session_log_location  — after capabilities, before skills_provisioned (HC-010)
//   - skills_provisioned    — after session_log_location, before agent_ready (HC-049)
//   - agent_ready           — ready-state signal (HC-039, HC-040)
//   - agent_started         — subprocess is live (HC-007, §6.4)
//   - agent_heartbeat       — liveness pulse at ≤T/2; scripted-mode carve-out (HC-026a)
//   - agent_completed       — clean subprocess exit after outcome_emitted (HC-024)
//   - agent_failed          — crash or fatal error (HC-024)
//   - outcome_emitted       — carries run Outcome; MUST be final message (HC-008)
//
// # NDJSON framing (HC-007a)
//
// Each message is one JSON object terminated by exactly one 0x0A byte.
// No embedded unescaped newlines inside a JSON object.
// Max line length enforced by the watcher (1 MiB); twin emitter never exceeds it.
//
// # Mechanism tagging (HC-037)
//
// All emission is mechanism-tagged (no cognition).
//
// Cite: specs/handler-contract.md §4.2.HC-007, §4.2.HC-007a, §4.2.HC-008,
// §4.6.HC-026a, §4.9.HC-039, §4.9.HC-040, §4.8.HC-036, §4.8.HC-037.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// twinWireFixture — per-bead helper prefix for test helpers in this file.
// (Actual test helpers are in wire_test.go; the prefix is declared here as
// a godoc anchor per implementer-protocol.md §Helper-prefix discipline.)

// ────────────────────────────────────────────────────────────────────────────
// §6.2 Wire-protocol message envelope
// ────────────────────────────────────────────────────────────────────────────

// wireMsg is the NDJSON-framed envelope for all progress-stream messages.
//
// Fields:
//   - Type: the message-type discriminator (e.g. "handler_capabilities").
//   - Payload: type-specific fields inlined into the JSON object via the
//     json.RawMessage approach — each Emit* function builds the full object
//     directly so there is no nested "payload" key; the type field is
//     alongside the payload fields in one flat JSON object per HC-007a.
//
// The envelope uses a flat structure: {"type":"...", <payload fields>}.
// There is no separate "payload" wrapper key in the wire format; event-model
// §4.1 wraps at the bus layer, not the progress-stream layer.
type wireMsg struct {
	Type string `json:"type"`
}

// ────────────────────────────────────────────────────────────────────────────
// Emitter
// ────────────────────────────────────────────────────────────────────────────

// wireEmitter writes NDJSON-framed progress-stream messages to a net.Conn.
//
// Each write method serialises one JSON object and appends a single newline
// (0x0A), satisfying HC-007a.  The emitter is NOT goroutine-safe; callers
// must serialise calls.
type wireEmitter struct {
	w io.Writer
}

// newWireEmitter wraps w in a wireEmitter.  w is typically the net.Conn
// returned by dialSocket.  A bufio.Writer is NOT used here because the spec
// requires each message to be flushed promptly; callers that need buffering
// should wrap w themselves.
func newWireEmitter(w io.Writer) *wireEmitter {
	return &wireEmitter{w: w}
}

// emit serialises v as compact JSON and appends a newline to w.
// It returns any write error.  The zero-overhead encoding path avoids
// encoder.Encode (which also appends a newline) in favour of Marshal +
// explicit newline so we keep full control of framing per HC-007a.
func (e *wireEmitter) emit(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wireEmitter.emit: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// Progress-stream message types (handler-to-daemon direction)
// ────────────────────────────────────────────────────────────────────────────

// emitHandlerCapabilities emits the handler_capabilities message.
//
// MUST be the FIRST message on the progress stream (HC-009).
// Carries the twin's supported wire-protocol version list.
//
// Payload (event-model §8.3.9): run_id, session_id, protocol_versions_supported[].
//
// Cite: specs/handler-contract.md §4.2.HC-009.
func (e *wireEmitter) emitHandlerCapabilities(runID, sessionID string, versions []int) error {
	return e.emit(struct {
		Type      string `json:"type"`
		RunID     string `json:"run_id"`
		SessionID string `json:"session_id"`
		Versions  []int  `json:"protocol_versions_supported"`
	}{
		Type:      "handler_capabilities",
		RunID:     runID,
		SessionID: sessionID,
		Versions:  versions,
	})
}

// emitSessionLogLocation emits the session_log_location message.
//
// MUST be emitted after handler_capabilities and before skills_provisioned /
// agent_ready (HC-010).
//
// Payload (event-model §8.3.7): run_id, session_id, node_id, agent_type,
// log_path, log_format, bead_id?.
//
// Cite: specs/handler-contract.md §4.2.HC-010.
func (e *wireEmitter) emitSessionLogLocation(
	runID, sessionID, nodeID, agentType, logPath, logFormat string,
	beadID *string,
) error {
	type msg struct {
		Type      string  `json:"type"`
		RunID     string  `json:"run_id"`
		SessionID string  `json:"session_id"`
		NodeID    string  `json:"node_id"`
		AgentType string  `json:"agent_type"`
		LogPath   string  `json:"log_path"`
		LogFormat string  `json:"log_format"`
		BeadID    *string `json:"bead_id,omitempty"`
	}
	return e.emit(msg{
		Type:      "session_log_location",
		RunID:     runID,
		SessionID: sessionID,
		NodeID:    nodeID,
		AgentType: agentType,
		LogPath:   logPath,
		LogFormat: logFormat,
		BeadID:    beadID,
	})
}

// skillEntry is one provisioned skill in the skills_provisioned payload.
//
// Cite: specs/event-model.md §8.3.8.
type skillEntry struct {
	Name       string  `json:"name"`
	SourcePath string  `json:"source_path"`
	Version    *string `json:"version,omitempty"`
}

// emitSkillsProvisioned emits the skills_provisioned message.
//
// MUST be emitted after session_log_location and before agent_ready (HC-049).
//
// Payload (event-model §8.3.8): run_id, session_id, skills[].
//
// Cite: specs/handler-contract.md §4.11.HC-049.
func (e *wireEmitter) emitSkillsProvisioned(runID, sessionID string, skills []skillEntry) error {
	type msg struct {
		Type      string       `json:"type"`
		RunID     string       `json:"run_id"`
		SessionID string       `json:"session_id"`
		Skills    []skillEntry `json:"skills"`
	}
	return e.emit(msg{
		Type:      "skills_provisioned",
		RunID:     runID,
		SessionID: sessionID,
		Skills:    skills,
	})
}

// emitAgentReady emits the agent_ready message.
//
// Signals that the handler subprocess is ready to accept work (HC-039).
// Twin MUST emit with the same shape and timing as real handlers (HC-040).
//
// Payload (event-model §8.3.1): run_id, session_id, capabilities[].
//
// Cite: specs/handler-contract.md §4.9.HC-039, §4.9.HC-040.
func (e *wireEmitter) emitAgentReady(runID, sessionID string, capabilities []string) error {
	type msg struct {
		Type         string   `json:"type"`
		RunID        string   `json:"run_id"`
		SessionID    string   `json:"session_id"`
		Capabilities []string `json:"capabilities"`
	}
	return e.emit(msg{
		Type:         "agent_ready",
		RunID:        runID,
		SessionID:    sessionID,
		Capabilities: capabilities,
	})
}

// emitAgentStarted emits the agent_started message.
//
// Emitted after subprocess spawn and before agent_ready (§6.4).
// MUST NOT include environment variables in payload (HC-029).
//
// Payload (event-model §8.3.2): run_id, session_id, node_id, agent_type, started_at.
//
// Cite: specs/handler-contract.md §4.7.HC-029, §6.4.
func (e *wireEmitter) emitAgentStarted(runID, sessionID, nodeID, agentType string, startedAt time.Time) error {
	type msg struct {
		Type      string `json:"type"`
		RunID     string `json:"run_id"`
		SessionID string `json:"session_id"`
		NodeID    string `json:"node_id"`
		AgentType string `json:"agent_type"`
		StartedAt string `json:"started_at"`
	}
	return e.emit(msg{
		Type:      "agent_started",
		RunID:     runID,
		SessionID: sessionID,
		NodeID:    nodeID,
		AgentType: agentType,
		StartedAt: startedAt.UTC().Format(time.RFC3339Nano),
	})
}

// heartbeatPhase is the extensible enum of phases for the agent_heartbeat message.
//
// The enum is additive-only; values declared here are the MVH set per HC-026a.
// Additional handler-specific values may be declared in subsystem envelopes.
//
// Cite: specs/handler-contract.md §4.6.HC-026a.
type heartbeatPhase string

const (
	// heartbeatPhaseStarting is emitted during subprocess initialisation.
	heartbeatPhaseStarting heartbeatPhase = "starting"
	// heartbeatPhaseReasoning is emitted while the agent is reasoning.
	heartbeatPhaseReasoning heartbeatPhase = "reasoning"
	// heartbeatPhaseToolCall is emitted while a tool call is in flight.
	heartbeatPhaseToolCall heartbeatPhase = "tool_call"
	// heartbeatPhaseWaitingInput is emitted while waiting for operator input
	// or during rate-limited windows (HC-026a).
	heartbeatPhaseWaitingInput heartbeatPhase = "waiting_input"
	// heartbeatPhaseRotating is emitted during an account-rotation turn
	// boundary (HC-013a).
	heartbeatPhaseRotating heartbeatPhase = "rotating"
	// heartbeatPhaseShuttingDown is emitted during the post-outcome shutdown
	// window.  Heartbeat emission is not required during the shutdown window
	// per HC-026a, but twin scripts MAY emit it to exercise the reader path.
	heartbeatPhaseShuttingDown heartbeatPhase = "shutting_down"
)

// emitAgentHeartbeat emits an agent_heartbeat message.
//
// MUST be emitted at ≤T/2 cadence while the subprocess is alive and has not
// emitted outcome_emitted (HC-026a).  The scripted-mode carve-out allows
// heartbeats to be driven at explicit relative timestamps from the script
// rather than a wall-clock timer (HC-026a scripted-mode carve-out).
//
// Payload: session_id, phase (per HC-026a).
//
// Cite: specs/handler-contract.md §4.6.HC-026a.
func (e *wireEmitter) emitAgentHeartbeat(sessionID string, phase heartbeatPhase) error {
	type msg struct {
		Type      string         `json:"type"`
		SessionID string         `json:"session_id"`
		Phase     heartbeatPhase `json:"phase"`
	}
	return e.emit(msg{
		Type:      "agent_heartbeat",
		SessionID: sessionID,
		Phase:     phase,
	})
}

// emitOutcomeEmitted emits the outcome_emitted message.
//
// MUST be the FINAL message before a clean exit (HC-008).  Carries the run's
// Outcome.  The daemon side translates this into the outcome_emitted bus event.
//
// Payload: run_id, session_id, node_id, outcome_status.
// (Full Outcome schema deferred to execution-model.md §6.1; this stub carries
// the minimum fields the watcher reads at MVH.)
//
// Cite: specs/handler-contract.md §4.2.HC-008.
func (e *wireEmitter) emitOutcomeEmitted(runID, sessionID, nodeID, outcomeStatus string) error {
	type msg struct {
		Type          string `json:"type"`
		RunID         string `json:"run_id"`
		SessionID     string `json:"session_id"`
		NodeID        string `json:"node_id"`
		OutcomeStatus string `json:"outcome_status"`
	}
	return e.emit(msg{
		Type:          "outcome_emitted",
		RunID:         runID,
		SessionID:     sessionID,
		NodeID:        nodeID,
		OutcomeStatus: outcomeStatus,
	})
}

// emitAgentCompleted emits the agent_completed message.
//
// Emitted on clean subprocess exit after outcome_emitted (HC-024).
// Carries ended_at, exit_code (observational), and outcome_ref.
//
// Payload (event-model §8.3.4): run_id, session_id, ended_at, exit_code,
// outcome_ref.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §6.4.
func (e *wireEmitter) emitAgentCompleted(runID, sessionID string, endedAt time.Time, exitCode int, outcomeRef string) error {
	type msg struct {
		Type       string `json:"type"`
		RunID      string `json:"run_id"`
		SessionID  string `json:"session_id"`
		EndedAt    string `json:"ended_at"`
		ExitCode   int    `json:"exit_code"`
		OutcomeRef string `json:"outcome_ref"`
	}
	return e.emit(msg{
		Type:       "agent_completed",
		RunID:      runID,
		SessionID:  sessionID,
		EndedAt:    endedAt.UTC().Format(time.RFC3339Nano),
		ExitCode:   exitCode,
		OutcomeRef: outcomeRef,
	})
}

// emitAgentFailed emits the agent_failed message.
//
// Emitted on crash or fatal typed error (HC-024).  The error_category maps
// to one of the five primary sentinel classes (HC-020).
//
// Payload (event-model §8.3.5): run_id, session_id, ended_at, error_category,
// reason.  sub_reason is optional; omit when empty.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020.
func (e *wireEmitter) emitAgentFailed(
	runID, sessionID string,
	endedAt time.Time,
	errorCategory, reason, subReason string,
) error {
	type msg struct {
		Type          string `json:"type"`
		RunID         string `json:"run_id"`
		SessionID     string `json:"session_id"`
		EndedAt       string `json:"ended_at"`
		ErrorCategory string `json:"error_category"`
		Reason        string `json:"reason"`
		SubReason     string `json:"sub_reason,omitempty"`
	}
	return e.emit(msg{
		Type:          "agent_failed",
		RunID:         runID,
		SessionID:     sessionID,
		EndedAt:       endedAt.UTC().Format(time.RFC3339Nano),
		ErrorCategory: errorCategory,
		Reason:        reason,
		SubReason:     subReason,
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Control-message reader (daemon-to-handler direction)
// ────────────────────────────────────────────────────────────────────────────

// controlMsg is the envelope for daemon-to-handler control messages received
// on the same bidirectional socket (HC-007a, §6.4).
//
// MVH catalog (§6.4):
//   - version_selected  {selected_version: int}
//   - cancel            {}
//   - shutdown          {}
//   - rotate_account    {}
//
// Unknown types MUST be ignored (forward-compatibility per §6.4).
type controlMsg struct {
	Type            string `json:"type"`
	SelectedVersion *int   `json:"selected_version,omitempty"`
}

// wireReader reads NDJSON-framed control messages from the daemon.
//
// Each line is expected to be a complete JSON object terminated by 0x0A.
// Lines exceeding 1 MiB would abort the daemon-side session per HC-007a; the
// daemon's own messages are well under that limit.
type wireReader struct {
	scanner *bufio.Scanner
}

// newWireReader wraps r in a wireReader using a bufio.Scanner set to a 1 MiB
// max-token size, matching the HC-007a watcher cap.
func newWireReader(r io.Reader) *wireReader {
	s := bufio.NewScanner(r)
	const maxLineBytes = 1 << 20 // 1 MiB per HC-007a
	s.Buffer(make([]byte, 4096), maxLineBytes)
	return &wireReader{scanner: s}
}

// readControlMsg reads the next control message from the socket.
// It returns (nil, io.EOF) at end-of-stream, or a parse error on malformed JSON.
// Unknown message types are returned as-is; callers must ignore them per §6.4.
func (r *wireReader) readControlMsg() (*controlMsg, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return nil, fmt.Errorf("wireReader.readControlMsg: scan: %w", err)
		}
		return nil, io.EOF
	}
	var m controlMsg
	if err := json.Unmarshal(r.scanner.Bytes(), &m); err != nil {
		return nil, fmt.Errorf("wireReader.readControlMsg: unmarshal: %w", err)
	}
	return &m, nil
}
