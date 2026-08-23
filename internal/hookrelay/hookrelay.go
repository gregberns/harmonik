// Package hookrelay implements the harmonik hook-relay subcommand.
//
// The relay is a short-lived subprocess invoked by Claude Code via command-type
// hooks declared in .claude/settings.json. It reads Claude's hook JSON from
// stdin, constructs one NDJSON progress-stream message, and ships it to the
// daemon's Unix domain socket using the one-shot connection regime.
//
// Spec: specs/claude-hook-bridge.md §4.4 CHB-010..012, §4.5 CHB-013..014,
// §4.6 CHB-015..017, §6.1 HookRelayMessage, §6.2 HookRelayAck, §8 error taxonomy.
// Spec: specs/handler-contract.md §4.10 HC-045b (hook-bridge connection regime for
// short-lived subprocesses: one-shot NDJSON, run_id+claude_session_id envelope,
// dial timeout ≤ 5 s, message ≤ 1 MiB, ack-line read with 5 s deadline, then close).
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
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var knownEventKinds = map[string]bool{
	"SessionStart": true,
	"Stop":         true,
	"SessionEnd":   true,
	"StopFailure":  true,
	"Notification": true,
}

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

type reviewVerdict struct {
	SchemaVersion int      `json:"schema_version"`
	Verdict       string   `json:"verdict"`
	Flags         []string `json:"flags"`
	Notes         string   `json:"notes"`
}

type hookRelayMessage struct {
	Type             string          `json:"type"`
	RunID            string          `json:"run_id"`
	ClaudeSessionID  string          `json:"claude_session_id"`
	HandlerSessionID string          `json:"handler_session_id"`
	EmittedAtNs      int64           `json:"emitted_at_ns"`
	Payload          json.RawMessage `json:"payload"`
}

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

var requiredEnvKeys = []string{
	"HARMONIK_RUN_ID",
	"HARMONIK_DAEMON_SOCKET",
	"HARMONIK_WORKSPACE_PATH",
	"HARMONIK_HANDLER_SESSION_ID",
	"HARMONIK_CLAUDE_SESSION_ID",
	"HARMONIK_WORKFLOW_ID",
	"HARMONIK_NODE_ID",
	"HARMONIK_AGENT_TYPE",
}

var optionalEnvKeys = []string{
	"HARMONIK_PHASE",
}

var errNotHarmonikSession = errors.New("hook-relay: not a harmonik-managed session")

func envFromOS() (Env, error) {
	var e Env
	dests := []*string{
		&e.RunID,
		&e.DaemonSocket,
		&e.WorkspacePath,
		&e.HandlerSessionID,
		&e.ClaudeSessionID,
		&e.WorkflowID,
		&e.NodeID,
		&e.AgentType,
	}

	var absent, blank []string
	for i, key := range requiredEnvKeys {
		v, present := os.LookupEnv(key)
		switch {
		case !present:
			absent = append(absent, key)
		case v == "":
			blank = append(blank, key)
		default:
			*dests[i] = v
		}
	}
	if len(absent) == len(requiredEnvKeys) {
		return Env{}, errNotHarmonikSession
	}
	if len(absent) > 0 || len(blank) > 0 {
		var parts []string
		if len(absent) > 0 {
			parts = append(parts, "absent: "+strings.Join(absent, ", "))
		}
		if len(blank) > 0 {
			parts = append(parts, "present but empty: "+strings.Join(blank, ", "))
		}
		return Env{}, fmt.Errorf(
			"bridge_malformed_hook_payload: this session is harmonik-managed but %d of %d required env vars carry no value (%s); the agent completion signal cannot be delivered",
			len(absent)+len(blank), len(requiredEnvKeys), strings.Join(parts, "; "))
	}

	optionalDests := []*string{
		&e.Phase,
	}
	for i, key := range optionalEnvKeys {
		*optionalDests[i] = os.Getenv(key)
	}
	return e, nil
}

// Run is the entry-point called from main. eventKind is the first positional
// argument to hook-relay. stdin, stdout, and stderr are the process I/O streams.
// env is the process environment (nil → read from OS).
//
// Returns 0 on success, 1 on any unrecoverable failure per CHB-017.
func Run(eventKind string, stdin io.Reader, stderr io.Writer, envOverride *Env) int {
	start := time.Now()

	if !knownEventKinds[eventKind] {
		return 0
	}

	e, exitCode, ok := resolveEnv(stderr, envOverride)
	if !ok {
		return exitCode
	}

	payload, err := io.ReadAll(stdin)
	if err != nil {
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: stdin read error: %v\n", err)
		return 1
	}

	var inp hookInput
	if err := json.Unmarshal(payload, &inp); err != nil {
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: invalid JSON: %v\n", err)
		return 1
	}

	if inp.SessionID == "" {
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: required field session_id is absent\n")
		return 1
	}
	if inp.TranscriptPath == "" {
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: required field transcript_path is absent\n")
		return 1
	}
	if inp.HookEventName == "" {
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: required field hook_event_name is absent\n")
		return 1
	}

	if inp.SessionID != e.ClaudeSessionID {
		writeDiagnostic(stderr,
			"bridge_session_id_mismatch: stdin session_id %q != HARMONIK_CLAUDE_SESSION_ID %q\n",
			inp.SessionID, e.ClaudeSessionID)
		return 1
	}

	if inp.HookEventName != eventKind {
		writeDiagnostic(stderr,
			"bridge_event_kind_mismatch: stdin hook_event_name %q != argv event-kind %q\n",
			inp.HookEventName, eventKind)
		return 1
	}

	msgType, msgPayload, noOp, buildErr := buildMessage(eventKind, inp, e, start)
	if buildErr != nil {
		writeDiagnostic(stderr, "%v\n", buildErr)
		return 1
	}

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
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: JSON marshal: %v\n", err)
		return 1
	}

	if err := sendToSocket(e.DaemonSocket, msgBytes, stderr); err != nil {
		writeDiagnostic(stderr, "%v\n", err)
		return 1
	}

	return 0
}

func resolveEnv(stderr io.Writer, envOverride *Env) (env Env, exitCode int, ok bool) {
	if envOverride != nil {
		return *envOverride, 0, true
	}
	e, err := envFromOS()
	if errors.Is(err, errNotHarmonikSession) {
		return Env{}, 0, false
	}
	if err != nil {
		writeDiagnostic(stderr, "%v\n", err)
		return Env{}, 1, false
	}
	return e, 0, true
}

func buildMessage(eventKind string, inp hookInput, e Env, _ time.Time) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	switch eventKind {
	case "SessionStart":
		return buildSessionStartMessage(e)

	case "SessionEnd":
		return "", nil, true, nil

	case "Stop":
		return buildStopMessage(inp, e)

	case "StopFailure":
		return buildStopFailureMessage(inp)

	case "Notification":
		return buildNotificationMessage(inp)
	}

	return "", nil, true, nil
}

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

func buildStopMessage(inp hookInput, e Env) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	phase := e.Phase

	if phase == "reviewer" {
		verdictPath := filepath.Join(e.WorkspacePath, ".harmonik", "review.json")
		//nolint:gosec // G304: verdictPath derived from HARMONIK_WORKSPACE_PATH env var (operator-controlled)
		verdictBytes, readErr := os.ReadFile(verdictPath)
		if readErr != nil {
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

func extractFinalMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return truncate4KiB(s)
	}

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

func truncate4KiB(s string) string {
	const maxBytes = 4096
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for i := 0; i < utf8.UTFMax-1 && !utf8.ValidString(b); i++ {
		b = b[:len(b)-1]
	}
	return b
}

func buildStopFailureMessage(inp hookInput) (
	msgType string, payload json.RawMessage, noOp bool, err error,
) {
	errorType := inp.ErrorType

	switch errorType {
	case "rate_limit":
		pl, marshalErr := json.Marshal(map[string]int{"retry_after_seconds": 60})
		if marshalErr != nil {
			return "", nil, false, fmt.Errorf("bridge_malformed_hook_payload: marshal rate_limit payload: %w", marshalErr)
		}
		return "agent_rate_limited", pl, false, nil

	case "server_error":
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

const hookEndpointTCPPrefix = "tcp://"

func resolveDialTarget(endpoint string) (network, address string) {
	if strings.HasPrefix(endpoint, hookEndpointTCPPrefix) {
		return "tcp", strings.TrimPrefix(endpoint, hookEndpointTCPPrefix)
	}
	return "unix", endpoint
}

func isRetryableDialErr(network, address string, err error) bool {
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return false // any other dial error is a fatal misconfiguration
	}
	if network == "unix" {
		if fi, statErr := os.Stat(address); statErr == nil && fi.Mode()&os.ModeSocket == 0 {
			return false
		}
	}
	return true
}

func isConnectionLostErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENOTCONN) ||
		errors.Is(err, syscall.ESHUTDOWN)
}

var errReconnect = errors.New("reconnect")

func sendToSocket(socketPath string, msgBytes []byte, stderr io.Writer) error {
	network, address := resolveDialTarget(socketPath)
	const (
		dialTimeout = 5 * time.Second
		readTimeout = 5 * time.Second
		retryBase   = 100 * time.Millisecond
		retryMax    = 2 * time.Second
		wallMax     = 25 * time.Second
	)

	const maxLine = 1 << 20
	if len(msgBytes)+1 > maxLine {
		return fmt.Errorf("bridge_malformed_hook_payload: message exceeds 1 MiB NDJSON line limit")
	}

	wallStart := time.Now()
	wallCtx, cancelWall := context.WithTimeout(context.Background(), wallMax)
	defer cancelWall()
	retryDelay := retryBase

	for {
		dialCtx, cancelDial := context.WithTimeout(wallCtx, dialTimeout)
		conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, network, address)
		cancelDial()

		if dialErr != nil {
			if wallErr := wallCtx.Err(); wallErr != nil {
				return fmt.Errorf("bridge_daemon_startup_window_exceeded: dial failed after %v: %w", time.Since(wallStart), wallErr)
			}
			if isRetryableDialErr(network, address, dialErr) {
				elapsed := time.Since(wallStart)
				if elapsed+retryDelay > wallMax {
					return fmt.Errorf("bridge_daemon_startup_window_exceeded: dial failed after %v: %w", elapsed, dialErr)
				}
				writeDiagnostic(stderr, "hook-relay: dial failed (%v), retrying in %v\n", dialErr, retryDelay)
				if waitErr := waitForRetry(wallCtx, retryDelay); waitErr != nil {
					return fmt.Errorf("bridge_daemon_startup_window_exceeded: dial failed after %v: %w", time.Since(wallStart), waitErr)
				}
				retryDelay *= 2
				if retryDelay > retryMax {
					retryDelay = retryMax
				}
				continue
			}
			return fmt.Errorf("bridge_dial_failed: %w", dialErr)
		}

		reconnectOrFail := func(stage string, cause error) error {
			closeErr := conn.Close()
			if !isConnectionLostErr(cause) {
				return errors.Join(fmt.Errorf("bridge_partial_write: %s: %w", stage, cause), closeErr)
			}
			elapsed := time.Since(wallStart)
			if elapsed+retryDelay > wallMax {
				return errors.Join(fmt.Errorf("bridge_daemon_startup_window_exceeded: %s after %v: %w", stage, elapsed, cause), closeErr)
			}
			writeDiagnostic(stderr, "hook-relay: %s (%v), reconnecting in %v\n", stage, cause, retryDelay)
			if waitErr := waitForRetry(wallCtx, retryDelay); waitErr != nil {
				return errors.Join(fmt.Errorf("bridge_daemon_startup_window_exceeded: %s after %v: %w", stage, time.Since(wallStart), waitErr), closeErr)
			}
			retryDelay *= 2
			if retryDelay > retryMax {
				retryDelay = retryMax
			}
			return errReconnect
		}

		if wallDeadline, ok := wallCtx.Deadline(); ok {
			if deadlineErr := conn.SetWriteDeadline(wallDeadline); deadlineErr != nil {
				if err := reconnectOrFail("set write deadline", deadlineErr); !errors.Is(err, errReconnect) {
					return err
				}
				continue
			}
		}

		if _, writeErr := conn.Write(msgBytes); writeErr != nil {
			if err := reconnectOrFail("write envelope", writeErr); !errors.Is(err, errReconnect) {
				return err
			}
			continue
		}
		if _, writeErr := conn.Write([]byte{'\n'}); writeErr != nil {
			if err := reconnectOrFail("write newline", writeErr); !errors.Is(err, errReconnect) {
				return err
			}
			continue
		}

		readDeadline := time.Now().Add(readTimeout)
		if wallDeadline, ok := wallCtx.Deadline(); ok && wallDeadline.Before(readDeadline) {
			readDeadline = wallDeadline
		}
		if deadlineErr := conn.SetReadDeadline(readDeadline); deadlineErr != nil {
			if err := reconnectOrFail("set read deadline", deadlineErr); !errors.Is(err, errReconnect) {
				return err
			}
			continue
		}

		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			scanErr := scanner.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			if err := reconnectOrFail("read ACK", scanErr); !errors.Is(err, errReconnect) {
				return err
			}
			continue
		}
		ackBytes := scanner.Bytes()
		if closeErr := conn.Close(); closeErr != nil {
			writeDiagnostic(stderr, "hook-relay: close after ACK failed: %v\n", closeErr)
		}

		var ack hookRelayAck
		if jsonErr := json.Unmarshal(ackBytes, &ack); jsonErr != nil {
			return fmt.Errorf("bridge_dial_failed: malformed ACK JSON: %w", jsonErr)
		}

		if ack.Status == "ok" {
			return nil
		}

		if ack.Status == "daemon_not_ready" {
			elapsed := time.Since(wallStart)
			if elapsed+retryDelay > wallMax {
				return fmt.Errorf("bridge_daemon_startup_window_exceeded: daemon_not_ready after %v", elapsed)
			}
			writeDiagnostic(stderr, "hook-relay: daemon_not_ready (%s), retrying in %v\n", ack.Reason, retryDelay)
			if waitErr := waitForRetry(wallCtx, retryDelay); waitErr != nil {
				return fmt.Errorf("bridge_daemon_startup_window_exceeded: daemon_not_ready after %v: %w", time.Since(wallStart), waitErr)
			}
			retryDelay *= 2
			if retryDelay > retryMax {
				retryDelay = retryMax
			}
			continue
		}

		return fmt.Errorf("bridge_dial_failed: daemon rejected message: status=%s reason=%s", ack.Status, ack.Reason)
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func writeDiagnostic(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}

// ErrMissingArg is returned when hook-relay is invoked without an event-kind argument.
var ErrMissingArg = errors.New("hook-relay: missing event-kind argument")
