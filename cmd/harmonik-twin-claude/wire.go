// Wire-protocol progress-stream emitter for the canonical twin binary.
//
// This file implements the NDJSON-framed message emitter that the twin sends
// over the Unix-domain socket to the daemon-side watcher, satisfying the
// parity surface declared in specs/handler-contract.md §4.8 (HC-035..HC-040).
//
// # Message types emitted (HC-007, HC-036)
//
//   - handler_capabilities     — first message on stream (HC-009)
//   - session_log_location     — after capabilities, before skills_provisioned (HC-010)
//   - skills_provisioned       — after session_log_location, before agent_ready (HC-049)
//   - agent_ready              — ready-state signal (HC-039, HC-040)
//   - agent_started            — subprocess is live (HC-007, §6.4)
//   - agent_output_chunk       — per-chunk output signal (HC-007, §6.4; event-model §8.3.3)
//   - agent_heartbeat          — liveness pulse at ≤T/2; scripted-mode carve-out (HC-026a)
//   - agent_rate_limited       — rate-limit onset; twin emits directly per HC-OQ-011 Interp.A
//   - agent_rate_limit_cleared — rate-limit clearance; twin emits directly per HC-OQ-011 Interp.A
//   - agent_completed          — clean subprocess exit after outcome_emitted (HC-024)
//   - agent_failed             — crash or fatal error (HC-024)
//   - outcome_emitted          — carries run Outcome; MUST be final message (HC-008)
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

type wireEmitter struct {
	w io.Writer
}

func newWireEmitter(w io.Writer) *wireEmitter {
	return &wireEmitter{w: w}
}

func (e *wireEmitter) emit(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wireEmitter.emit: marshal: %w", err)
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

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

type skillEntry struct {
	Name       string  `json:"name"`
	SourcePath string  `json:"source_path"`
	Version    *string `json:"version,omitempty"`
}

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

type heartbeatPhase string

const (
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

func (e *wireEmitter) emitAgentOutputChunk(runID, sessionID string, chunkIndex, bytesEmitted int, chunkDigest *string) error {
	type msg struct {
		Type         string  `json:"type"`
		RunID        string  `json:"run_id"`
		SessionID    string  `json:"session_id"`
		ChunkIndex   int     `json:"chunk_index"`
		BytesEmitted int     `json:"bytes_emitted"`
		ChunkDigest  *string `json:"chunk_digest,omitempty"`
	}
	return e.emit(msg{
		Type:         "agent_output_chunk",
		RunID:        runID,
		SessionID:    sessionID,
		ChunkIndex:   chunkIndex,
		BytesEmitted: bytesEmitted,
		ChunkDigest:  chunkDigest,
	})
}

func (e *wireEmitter) emitAgentRateLimited(runID, sessionID string, rateLimitSource *string, retryAfterSeconds *int, changedAt time.Time) error {
	type msg struct {
		Type              string  `json:"type"`
		RunID             string  `json:"run_id"`
		SessionID         string  `json:"session_id"`
		RateLimitSource   *string `json:"rate_limit_source,omitempty"`
		RetryAfterSeconds *int    `json:"retry_after_seconds,omitempty"`
		ChangedAt         string  `json:"changed_at"`
	}
	return e.emit(msg{
		Type:              "agent_rate_limited",
		RunID:             runID,
		SessionID:         sessionID,
		RateLimitSource:   rateLimitSource,
		RetryAfterSeconds: retryAfterSeconds,
		ChangedAt:         changedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (e *wireEmitter) emitAgentRateLimitCleared(runID, sessionID string, changedAt time.Time) error {
	type msg struct {
		Type      string `json:"type"`
		RunID     string `json:"run_id"`
		SessionID string `json:"session_id"`
		ChangedAt string `json:"changed_at"`
	}
	return e.emit(msg{
		Type:      "agent_rate_limit_cleared",
		RunID:     runID,
		SessionID: sessionID,
		ChangedAt: changedAt.UTC().Format(time.RFC3339Nano),
	})
}

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

func (e *wireEmitter) emitTwinSettingsLoaded(permissionsPresent, stopHookPresent bool, stopHookCommand string) error {
	const maxCmdLen = 200
	if len(stopHookCommand) > maxCmdLen {
		stopHookCommand = stopHookCommand[:maxCmdLen]
	}
	type msg struct {
		Type               string `json:"type"`
		PermissionsPresent bool   `json:"permissions_present"`
		StopHookPresent    bool   `json:"stop_hook_present"`
		StopHookCommand    string `json:"stop_hook_command"`
	}
	return e.emit(msg{
		Type:               "twin_settings_loaded",
		PermissionsPresent: permissionsPresent,
		StopHookPresent:    stopHookPresent,
		StopHookCommand:    stopHookCommand,
	})
}

func (e *wireEmitter) emitTwinHookCalled(hookType string, exitCode, durationMs int) error {
	type msg struct {
		Type       string `json:"type"`
		HookType   string `json:"hook_type"`
		ExitCode   int    `json:"exit_code"`
		DurationMs int    `json:"duration_ms"`
	}
	return e.emit(msg{
		Type:       "twin_hook_called",
		HookType:   hookType,
		ExitCode:   exitCode,
		DurationMs: durationMs,
	})
}

func (e *wireEmitter) emitTwinCommitted(commitSHA string, exitCode, durationMs int, stderrExcerpt string) error {
	type msg struct {
		Type          string `json:"type"`
		CommitSHA     string `json:"commit_sha"`
		ExitCode      int    `json:"exit_code"`
		DurationMs    int    `json:"duration_ms"`
		StderrExcerpt string `json:"stderr_excerpt,omitempty"`
	}
	return e.emit(msg{
		Type:          "twin_committed",
		CommitSHA:     commitSHA,
		ExitCode:      exitCode,
		DurationMs:    durationMs,
		StderrExcerpt: stderrExcerpt,
	})
}

func (e *wireEmitter) emitTwinError(reason string) error {
	type msg struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	return e.emit(msg{
		Type:   "twin_error",
		Reason: reason,
	})
}

type controlMsg struct {
	Type            string `json:"type"`
	SelectedVersion *int   `json:"selected_version,omitempty"`
}

type wireReader struct {
	scanner *bufio.Scanner
}

func newWireReader(r io.Reader) *wireReader {
	s := bufio.NewScanner(r)
	const maxLineBytes = 1 << 20 // 1 MiB per HC-007a
	s.Buffer(make([]byte, 4096), maxLineBytes)
	return &wireReader{scanner: s}
}

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
