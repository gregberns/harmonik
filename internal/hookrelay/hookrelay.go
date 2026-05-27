// Package hookrelay implements the harmonik hook-relay subcommand.
//
// The relay is a short-lived subprocess invoked by Claude Code via command-type
// hooks declared in .claude/settings.json. It reads Claude's hook JSON from
// stdin, constructs one NDJSON progress-stream message, and ships it to the
// daemon's Unix domain socket using the one-shot connection regime.
//
// Spec: specs/claude-hook-bridge.md §4.4 CHB-010..012, §4.5 CHB-013..014,
// §4.6 CHB-015..017, §6.1 HookRelayMessage, §6.2 HookRelayAck, §8 error taxonomy.
//
// Tags: mechanism. No cognition. All behaviour is deterministic.
package hookrelay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// knownEventKinds is the set of event kinds the relay handles.
// Per CHB-011, all other event kinds are no-op exit 0.
var knownEventKinds = map[string]bool{
	"SessionStart": true,
	"Stop":         true,
	"SessionEnd":   true,
	"StopFailure":  true,
	"Notification": true,
}

// hookInput is the minimal shape of Claude's hook stdin JSON per CHB-012.
type hookInput struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	// StopFailure-specific
	ErrorType string `json:"error_type,omitempty"`
	// Notification-specific
	NotificationType string `json:"notification_type,omitempty"`
	// Stop-specific: assistant message for WORK_COMPLETE payload
	Message json.RawMessage `json:"message,omitempty"`
}

// reviewVerdict is the agent-reviewer JSON verdict schema v1 per
// specs/claude-hook-bridge.md §4.5 CHB-014 and workspace-model.md §4.7 WM-027a.
type reviewVerdict struct {
	SchemaVersion int      `json:"schema_version"`
	Verdict       string   `json:"verdict"`
	Flags         []string `json:"flags"`
	Notes         string   `json:"notes"`
}

// hookRelayMessage is the NDJSON envelope the relay writes to the daemon socket
// per §6.1 HookRelayMessage.
type hookRelayMessage struct {
	Type             string          `json:"type"`
	RunID            string          `json:"run_id"`
	ClaudeSessionID  string          `json:"claude_session_id"`
	HandlerSessionID string          `json:"handler_session_id"`
	EmittedAtNs      int64           `json:"emitted_at_ns"`
	Payload          json.RawMessage `json:"payload"`
}

// hookRelayAck is the ACK/error shape the daemon returns per §6.2 HookRelayAck.
type hookRelayAck struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Env holds the HARMONIK_* env vars required by the relay per CHB-006.
type Env struct {
	RunID            string
	DaemonSocket     string
	WorkspacePath    string
	HandlerSessionID string
	ClaudeSessionID  string
	WorkflowID       string
	NodeID           string
	AgentType        string
	Phase            string // optional; "" when absent
}

// envFromOS reads HARMONIK_* env vars from the process environment.
// Returns an error if any required variable is absent.
func envFromOS() (Env, error) {
	required := []struct {
		key  string
		dest *string
	}{
		{"HARMONIK_RUN_ID", nil},
		{"HARMONIK_DAEMON_SOCKET", nil},
		{"HARMONIK_WORKSPACE_PATH", nil},
		{"HARMONIK_HANDLER_SESSION_ID", nil},
		{"HARMONIK_CLAUDE_SESSION_ID", nil},
		{"HARMONIK_WORKFLOW_ID", nil},
		{"HARMONIK_NODE_ID", nil},
		{"HARMONIK_AGENT_TYPE", nil},
	}

	var e Env
	ptrs := []*string{
		&e.RunID,
		&e.DaemonSocket,
		&e.WorkspacePath,
		&e.HandlerSessionID,
		&e.ClaudeSessionID,
		&e.WorkflowID,
		&e.NodeID,
		&e.AgentType,
	}
	for i := range required {
		required[i].dest = ptrs[i]
	}

	for _, r := range required {
		v := os.Getenv(r.key)
		if v == "" {
			return Env{}, fmt.Errorf("bridge_malformed_hook_payload: required env var %s is absent", r.key)
		}
		*r.dest = v
	}

	e.Phase = os.Getenv("HARMONIK_PHASE") // optional
	return e, nil
}

// Run is the entry-point called from main. eventKind is the first positional
// argument to hook-relay. stdin, stdout, and stderr are the process I/O streams.
// env is the process environment (nil → read from OS).
//
// Returns 0 on success, 1 on any unrecoverable failure per CHB-017.
func Run(eventKind string, stdin io.Reader, stderr io.Writer, envOverride *Env) int {
	start := time.Now()

	// CHB-011: unknown event kinds are no-op exit 0.
	if !knownEventKinds[eventKind] {
		return 0
	}

	// Load env vars.
	var e Env
	if envOverride != nil {
		e = *envOverride
	} else {
		var err error
		e, err = envFromOS()
		if err != nil {
			// Not a harmonik-managed session (e.g., user running Claude Code
			// directly in a project that has hook-relay settings.json). Exit 0
			// silently — the hook is a no-op outside harmonik. (hk-f0xb6)
			return 0
		}
	}

	// Read and validate stdin per CHB-012.
	payload, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "bridge_malformed_hook_payload: stdin read error: %v\n", err)
		return 1
	}

	var inp hookInput
	if err := json.Unmarshal(payload, &inp); err != nil {
		fmt.Fprintf(stderr, "bridge_malformed_hook_payload: invalid JSON: %v\n", err)
		return 1
	}

	// CHB-012: session_id MUST match HARMONIK_CLAUDE_SESSION_ID.
	if inp.SessionID != e.ClaudeSessionID {
		fmt.Fprintf(stderr,
			"bridge_session_id_mismatch: stdin session_id %q != HARMONIK_CLAUDE_SESSION_ID %q\n",
			inp.SessionID, e.ClaudeSessionID)
		return 1
	}

	// CHB-012: hook_event_name MUST match argv event-kind.
	if inp.HookEventName != eventKind {
		fmt.Fprintf(stderr,
			"bridge_event_kind_mismatch: stdin hook_event_name %q != argv event-kind %q\n",
			inp.HookEventName, eventKind)
		return 1
	}

	// Build the progress-stream message per CHB-013.
	msgType, msgPayload, noOp, buildErr := buildMessage(eventKind, inp, e, start)
	if buildErr != nil {
		fmt.Fprintln(stderr, buildErr)
		return 1
	}

	// SessionEnd is no-op; SessionStart synthesizes agent_ready (not a no-op).
	if noOp {
		return 0
	}

	emittedAtNs := time.Since(start).Nanoseconds()

	msg := hookRelayMessage{
		Type:             msgType,
		RunID:            e.RunID,
		ClaudeSessionID:  e.ClaudeSessionID,
		HandlerSessionID: e.HandlerSessionID,
		EmittedAtNs:      emittedAtNs,
		Payload:          msgPayload,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		fmt.Fprintf(stderr, "bridge_malformed_hook_payload: JSON marshal: %v\n", err)
		return 1
	}

	// CHB-015: one-shot UDS with retry per CHB-016.
	if err := sendToSocket(e.DaemonSocket, msgBytes, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

// buildMessage constructs the progress-stream message type and payload per CHB-013.
// noOp=true means the event maps to a no-op (exit 0 without writing to socket).
// Returns an error string suitable for stderr on failure.
func buildMessage(eventKind string, inp hookInput, e Env, _ time.Time) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	switch eventKind {
	case "SessionStart":
		// CHB-013 (as amended by hk-p63bz): relay synthesizes agent_ready with
		// provenance="claude_session_start" on first SessionStart receipt.
		// This is the first claude-originated lifecycle signal under the tmux
		// substrate and is the correct ready-state indicator (HC-039 / HC-041).
		return buildSessionStartMessage(e)

	case "SessionEnd":
		// CHB-013: no-op; handler emits agent_completed on Wait-return.
		return "", nil, true, nil

	case "Stop":
		return buildStopMessage(inp, e)

	case "StopFailure":
		return buildStopFailureMessage(inp)

	case "Notification":
		return buildNotificationMessage(inp)
	}

	// Unreachable: knownEventKinds check already handled unknowns.
	return "", nil, true, nil
}

// buildSessionStartMessage synthesizes agent_ready with provenance="claude_session_start"
// on receipt of a SessionStart hook, per CHB-013 (as amended by hk-p63bz).
//
// This is the first claude-originated lifecycle signal under the interactive
// (tmux) substrate and satisfies the HC-039 ready-state gate.  The daemon's
// waitAgentReady will observe this event and unblock work dispatch.
//
// Source detection: the relay does not currently distinguish between
// startup and resume SessionStart sources at the wire level; both synthesize
// agent_ready with provenance="claude_session_start" per CHB-013.
func buildSessionStartMessage(e Env) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	pl, marshalErr := json.Marshal(map[string]interface{}{
		"session_id":   e.HandlerSessionID,
		"capabilities": []string{},
		"provenance":   "claude_session_start",
	})
	if marshalErr != nil {
		return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal agent_ready payload: %w", marshalErr)
	}
	return "agent_ready", pl, false, nil
}

// buildStopMessage handles the Stop hook → outcome_emitted mapping per CHB-013.
func buildStopMessage(inp hookInput, e Env) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	phase := e.Phase

	if phase == "reviewer" {
		// CHB-014: read and validate reviewer verdict file.
		verdictPath := filepath.Join(e.WorkspacePath, ".harmonik", "review.json")
		//nolint:gosec // G304: verdictPath derived from HARMONIK_WORKSPACE_PATH env var (operator-controlled)
		verdictBytes, readErr := os.ReadFile(verdictPath)
		if readErr != nil {
			// File absent.
			pl, marshalErr := json.Marshal(map[string]string{"error": "missing_review_file"})
			if marshalErr != nil {
				return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal error payload: %w", marshalErr)
			}
			return "outcome_emitted", pl, false, nil
		}

		var rv reviewVerdict
		if jsonErr := json.Unmarshal(verdictBytes, &rv); jsonErr != nil {
			pl, marshalErr := json.Marshal(map[string]string{"error": "malformed_review_file"})
			if marshalErr != nil {
				return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal error payload: %w", marshalErr)
			}
			return "outcome_emitted", pl, false, nil
		}

		// CHB-014: validate schema_version=1, verdict ∈ {APPROVE,REQUEST_CHANGES,BLOCK},
		// flags is string array, notes is string.
		if rv.SchemaVersion != 1 {
			pl, marshalErr := json.Marshal(map[string]string{"error": "malformed_review_file"})
			if marshalErr != nil {
				return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal error payload: %w", marshalErr)
			}
			return "outcome_emitted", pl, false, nil
		}
		validVerdicts := map[string]bool{"APPROVE": true, "REQUEST_CHANGES": true, "BLOCK": true}
		if !validVerdicts[rv.Verdict] {
			pl, marshalErr := json.Marshal(map[string]string{"error": "malformed_review_file"})
			if marshalErr != nil {
				return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal error payload: %w", marshalErr)
			}
			return "outcome_emitted", pl, false, nil
		}
		if rv.Flags == nil {
			rv.Flags = []string{}
		}

		// Build outcome_emitted with REVIEWER_VERDICT kind.
		pl, marshalErr := json.Marshal(map[string]interface{}{
			"kind": "REVIEWER_VERDICT",
			"verdict": map[string]interface{}{
				"schema_version": rv.SchemaVersion,
				"verdict":        rv.Verdict,
				"flags":          rv.Flags,
				"notes":          rv.Notes,
			},
		})
		if marshalErr != nil {
			return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal verdict payload: %w", marshalErr)
		}
		return "outcome_emitted", pl, false, nil
	}

	// Implementer phases: single, implementer-initial, implementer-resume.
	// Extract Claude's final assistant message text (truncated to 4 KiB) per CHB-013.
	summary := extractFinalMessage(inp.Message)

	pl, marshalErr := json.Marshal(map[string]interface{}{
		"kind":    "WORK_COMPLETE",
		"summary": summary,
	})
	if marshalErr != nil {
		return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal work_complete payload: %w", marshalErr)
	}
	return "outcome_emitted", pl, false, nil
}

// extractFinalMessage pulls the text from Claude's message field (Stop payload),
// truncating to 4 KiB per CHB-013.
func extractFinalMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Claude's Stop message field can be a string directly or a structured object.
	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return truncate4KiB(s)
	}

	// Try object with "content" field (Claude structured message).
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		if content, ok := obj["content"]; ok {
			var text string
			if err := json.Unmarshal(content, &text); err == nil {
				return truncate4KiB(text)
			}
		}
	}

	return ""
}

// truncate4KiB truncates s to at most 4096 bytes (4 KiB) per CHB-013.
func truncate4KiB(s string) string {
	const max = 4096
	if len(s) <= max {
		return s
	}
	// Truncate at a valid UTF-8 boundary.
	b := []byte(s[:max])
	// Walk back to a valid rune boundary.
	for i := len(b); i > 0; i-- {
		if b[i-1]&0x80 == 0 || b[i-1]&0xC0 == 0xC0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// buildStopFailureMessage maps StopFailure error_type to progress-stream messages per CHB-013.
func buildStopFailureMessage(inp hookInput) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	errorType := inp.ErrorType

	switch errorType {
	case "rate_limit":
		// CHB-013: agent_rate_limited with synthesized retry_after_seconds=60.
		pl, marshalErr := json.Marshal(map[string]int{"retry_after_seconds": 60})
		if marshalErr != nil {
			return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal rate_limit payload: %w", marshalErr)
		}
		return "agent_rate_limited", pl, false, nil

	case "server_error":
		// CHB-013: outcome_emitted{kind=FAILURE_SIGNAL} with ErrTransient.
		pl, marshalErr := json.Marshal(map[string]string{
			"kind":            "FAILURE_SIGNAL",
			"error_type":      "claude_server_error",
			"sub_reason":      "claude_server_error",
			"suggested_class": "transient",
		})
		if marshalErr != nil {
			return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal server_error payload: %w", marshalErr)
		}
		return "outcome_emitted", pl, false, nil

	default:
		// authentication_failed, oauth_org_not_allowed, billing_error,
		// invalid_request, max_output_tokens, unknown → ErrStructural.
		claudeType := "claude_" + errorType
		pl, marshalErr := json.Marshal(map[string]string{
			"kind":            "FAILURE_SIGNAL",
			"error_type":      claudeType,
			"sub_reason":      claudeType,
			"suggested_class": "structural",
		})
		if marshalErr != nil {
			return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal failure_signal payload: %w", marshalErr)
		}
		return "outcome_emitted", pl, false, nil
	}
}

// buildNotificationMessage maps Notification hook events to agent_heartbeat per CHB-013.
func buildNotificationMessage(inp hookInput) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	phase := "reasoning"
	switch inp.NotificationType {
	case "idle_prompt", "permission_prompt":
		phase = "waiting_input"
	}

	pl, marshalErr := json.Marshal(map[string]string{"phase": phase})
	if marshalErr != nil {
		return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal heartbeat payload: %w", marshalErr)
	}
	return "agent_heartbeat", pl, false, nil
}

// sendToSocket implements the one-shot UDS write with daemon-not-ready retry
// per CHB-015 and CHB-016.
func sendToSocket(socketPath string, msgBytes []byte, stderr io.Writer) error {
	const (
		dialTimeout = 5 * time.Second
		readTimeout = 5 * time.Second
		retryBase   = 100 * time.Millisecond
		retryMax    = 2 * time.Second
		wallMax     = 25 * time.Second
	)

	// CHB-015: byte length ≤ 1 MiB.
	const maxLine = 1 << 20
	if len(msgBytes)+1 > maxLine {
		return fmt.Errorf("bridge_malformed_hook_payload: message exceeds 1 MiB NDJSON line limit")
	}

	wallStart := time.Now()
	retryDelay := retryBase

	for {
		// CHB-015: 5s dial timeout.
		dialCtx, cancelDial := context.WithTimeout(context.Background(), dialTimeout)
		conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", socketPath)
		cancelDial()

		if dialErr != nil {
			return fmt.Errorf("bridge_dial_failed: %w", dialErr)
		}

		// CHB-015: write exactly one NDJSON line terminated by \n.
		// Two sequential writes: the JSON bytes then the newline delimiter.
		if _, writeErr := conn.Write(msgBytes); writeErr != nil {
			_ = conn.Close()
			return fmt.Errorf("bridge_dial_failed: write: %w", writeErr)
		}
		if _, writeErr := conn.Write([]byte{'\n'}); writeErr != nil {
			_ = conn.Close()
			return fmt.Errorf("bridge_dial_failed: write newline: %w", writeErr)
		}

		// CHB-015: read back one NDJSON line within 5s.
		if deadlineErr := conn.SetReadDeadline(time.Now().Add(readTimeout)); deadlineErr != nil {
			_ = conn.Close()
			return fmt.Errorf("bridge_dial_failed: set read deadline: %w", deadlineErr)
		}

		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			_ = conn.Close()
			scanErr := scanner.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			return fmt.Errorf("bridge_dial_failed: read ACK: %w", scanErr)
		}
		ackBytes := scanner.Bytes()
		_ = conn.Close()

		var ack hookRelayAck
		if jsonErr := json.Unmarshal(ackBytes, &ack); jsonErr != nil {
			return fmt.Errorf("bridge_dial_failed: malformed ACK JSON: %w", jsonErr)
		}

		// CHB-015: ok → success.
		if ack.Status == "ok" {
			return nil
		}

		// CHB-016: daemon_not_ready → retry with exponential backoff.
		if ack.Status == "daemon_not_ready" {
			elapsed := time.Since(wallStart)
			if elapsed+retryDelay > wallMax {
				return fmt.Errorf("bridge_daemon_startup_window_exceeded: daemon_not_ready after %v", elapsed)
			}
			fmt.Fprintf(stderr, "hook-relay: daemon_not_ready (%s), retrying in %v\n", ack.Reason, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2
			if retryDelay > retryMax {
				retryDelay = retryMax
			}
			continue
		}

		// Any other non-ok status (bad_envelope, unknown_session, etc.) is unrecoverable.
		return fmt.Errorf("bridge_dial_failed: daemon rejected message: status=%s reason=%s", ack.Status, ack.Reason)
	}
}

// ErrMissingArg is returned when hook-relay is invoked without an event-kind argument.
var ErrMissingArg = errors.New("hook-relay: missing event-kind argument")
