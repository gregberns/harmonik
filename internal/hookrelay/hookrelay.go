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

// requiredEnvKeys names every HARMONIK_* variable the relay cannot work without,
// per specs/claude-hook-bridge.md §4.2 CHB-006. The order matches the
// destination fields in envFromOS.
//
// It is a package-level list rather than a literal inside envFromOS so the tests
// can read the SAME list the code reads. A test that keeps its own copy of these
// names agrees with the code only until someone adds the ninth variable, and the
// disagreement then shows up as a failure that depends on whether the machine
// running the test happens to export the new name.
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

// optionalEnvKeys names the HARMONIK_* variables envFromOS reads but does not
// demand. Absence is normal and is never an error.
//
// Same rule as requiredEnvKeys, and for the same reason: envFromOS indexes this
// list to do the read. A list that no production code reads is a second copy of
// a name, free to drift from the literal the code actually uses, which is the
// hazard requiredEnvKeys exists to remove.
var optionalEnvKeys = []string{
	"HARMONIK_PHASE",
}

// errNotHarmonikSession reports that NOT ONE of the required HARMONIK_*
// variables is present in the environment, so this Claude Code session was not
// started by harmonik. The relay is a no-op there and says nothing (hk-f0xb6):
// an operator running Claude Code by hand in a project whose settings.json
// carries the hook must not see an error, and the daemon being down on a
// developer box is a normal state.
//
// It is deliberately NOT returned when SOME variables are present. A session
// with seven of the eight is harmonik-managed and has lost its wiring, which is
// never normal — see envFromOS (hk-stop-relay-cannot-fail-5n2t3).
var errNotHarmonikSession = errors.New("hook-relay: not a harmonik-managed session")

// envFromOS reads HARMONIK_* env vars from the process environment.
//
// PRESENCE, NOT EMPTINESS, picks the outcome. The test is os.LookupEnv, because
// a variable that is present and set to "" is not the same fact as a variable
// that was never exported: the first is a harmonik session whose wiring arrived
// broken, the second is a session harmonik never touched. Deciding with
// os.Getenv conflated the two and sent the broken-wiring case down the silent
// path — the exact vacuous success this function exists to remove.
//
// Three outcomes, and the difference between the last two is the whole point:
//   - every required variable present and non-empty -> (Env, nil).
//   - NOT ONE present                               -> errNotHarmonikSession.
//     The caller exits 0 in silence.
//   - any other mix                                 -> an error naming each
//     unusable variable and saying whether it is absent or present-but-empty.
//     The caller exits 1 and prints it. The session IS harmonik-managed and its
//     completion signal cannot be delivered, so the daemon waits out the whole
//     commit budget and records the finished agent as a budget overrun
//     (hk-stop-relay-cannot-fail-5n2t3). Exiting 1 does not shorten that wait —
//     a Claude implementer phase ends when a commit lands, so an agent that
//     finishes without committing burns its budget whatever this hook does.
//     What exit 1 changes is that the broken wiring becomes visible instead of
//     being reported as success. Per CHB-017 an env-var mismatch is an
//     unrecoverable failure and exits 1.
//
// A present-but-empty variable therefore lands in the loud third case, never in
// the silent second one. It counts as wiring, so it keeps the session on the
// loud path even when every other variable is absent.
func envFromOS() (Env, error) {
	var e Env
	// One destination per name in requiredEnvKeys, in the SAME ORDER. Both halves
	// of that sentence are load-bearing and neither is checked by the compiler: a
	// short slice is an index panic, and a permuted one silently files each value
	// under its neighbour's name. A test pins length and order together.
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

	// absent: no such variable in the environment.
	// blank:  the variable is exported, and its value is "".
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
	// Silence is correct only when the environment carries no harmonik wiring at
	// all. An exported empty variable IS wiring, so one of those makes this
	// condition unsatisfiable and the loud branch below takes the session.
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

	// The optional half, read the same indexed way. os.Getenv is right here:
	// absent and present-but-empty both mean "no phase", which is a normal state
	// and not a broken session.
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

	// CHB-011: unknown event kinds are no-op exit 0.
	if !knownEventKinds[eventKind] {
		return 0
	}

	// Load env vars.
	e, exitCode, ok := resolveEnv(stderr, envOverride)
	if !ok {
		return exitCode
	}

	// Read and validate stdin per CHB-012.
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

	// CHB-012: required fields must be present (non-empty).
	// "required field missing" maps to bridge_malformed_hook_payload per §8 error taxonomy.
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

	// CHB-012: session_id MUST match HARMONIK_CLAUDE_SESSION_ID.
	if inp.SessionID != e.ClaudeSessionID {
		writeDiagnostic(stderr,
			"bridge_session_id_mismatch: stdin session_id %q != HARMONIK_CLAUDE_SESSION_ID %q\n",
			inp.SessionID, e.ClaudeSessionID)
		return 1
	}

	// CHB-012: hook_event_name MUST match argv event-kind.
	if inp.HookEventName != eventKind {
		writeDiagnostic(stderr,
			"bridge_event_kind_mismatch: stdin hook_event_name %q != argv event-kind %q\n",
			inp.HookEventName, eventKind)
		return 1
	}

	// Build the progress-stream message per CHB-013.
	msgType, msgPayload, noOp, buildErr := buildMessage(eventKind, inp, e, start)
	if buildErr != nil {
		writeDiagnostic(stderr, "%v\n", buildErr)
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
		writeDiagnostic(stderr, "bridge_malformed_hook_payload: JSON marshal: %v\n", err)
		return 1
	}

	// CHB-015: one-shot UDS with retry per CHB-016.
	if err := sendToSocket(e.DaemonSocket, msgBytes, stderr); err != nil {
		writeDiagnostic(stderr, "%v\n", err)
		return 1
	}

	return 0
}

// resolveEnv produces the Env that Run works from. It has three outcomes, not
// two, so the classification of an envFromOS error lives here rather than inline
// in Run: ok=true carries a usable Env, and ok=false carries the exit code Run
// must return.
//
// The two not-ok outcomes are deliberately different. A session with NO harmonik
// wiring is not an error at all, and a harmonik-managed session with BROKEN
// wiring must be loud.
func resolveEnv(stderr io.Writer, envOverride *Env) (env Env, exitCode int, ok bool) {
	if envOverride != nil {
		return *envOverride, 0, true
	}
	e, err := envFromOS()
	if errors.Is(err, errNotHarmonikSession) {
		// Not a harmonik-managed session (e.g., user running Claude Code
		// directly in a project that has hook-relay settings.json). Exit 0
		// silently — the hook is a no-op outside harmonik. (hk-f0xb6)
		return Env{}, 0, false
	}
	if err != nil {
		// A harmonik-managed session with broken wiring. Reporting it is the
		// point: the previous silent exit 0 made a green stop_hook_summary
		// evidence of nothing at all (hk-stop-relay-cannot-fail-5n2t3).
		writeDiagnostic(stderr, "%v\n", err)
		return Env{}, 1, false
	}
	return e, 0, true
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
	const maxBytes = 4096
	if len(s) <= maxBytes {
		return s
	}
	// A byte-boundary cut can split the final multibyte rune. Trim up to
	// utf8.UTFMax-1 trailing bytes until the result ends on a valid rune
	// boundary, so we never emit an invalid trailing rune (CHB-013 / RU-14).
	b := s[:maxBytes]
	for i := 0; i < utf8.UTFMax-1 && !utf8.ValidString(b); i++ {
		b = b[:len(b)-1]
	}
	return b
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

// hookEndpointTCPPrefix marks a HARMONIK_DAEMON_SOCKET value as a TCP loopback
// endpoint (the REMOTE-run reverse-tunnel transport) rather than a unix-socket
// path. A unix-socket path never starts with this prefix, so the dialer can pick
// the transport purely from the env value. Kept in sync with the daemon side
// (internal/transport/tunnel/tunnel.go tcpEndpointPrefix).
//
// hk-ege6: remote runs dial a TCP loopback listener on the worker
// (tcp://127.0.0.1:<port>) because the macOS-root sshd `-R` unix-socket bind is
// root-owned 0600 and unconnectable by this unprivileged hook subprocess. Local
// runs keep dialing box A's daemon unix socket (the default, no prefix).
const hookEndpointTCPPrefix = "tcp://"

// resolveDialTarget maps a HARMONIK_DAEMON_SOCKET value to the (network, address)
// pair for net.Dial: a "tcp://host:port" value → ("tcp", "host:port"); any other
// value is treated as a unix-socket path → ("unix", value). Unix is the default
// for backward compat with local runs.
func resolveDialTarget(endpoint string) (network, address string) {
	if strings.HasPrefix(endpoint, hookEndpointTCPPrefix) {
		return "tcp", strings.TrimPrefix(endpoint, hookEndpointTCPPrefix)
	}
	return "unix", endpoint
}

// isRetryableDialErr reports whether a DialContext failure reflects a daemon
// that has not yet begun listening — the cold-boot / in-place-swap startup race
// (CHB-016) — rather than a fatal misconfiguration. ENOENT ("no such file": the
// unix socket has not been created yet) and connection-refused (the endpoint is
// present but nothing is accepting yet) are transient and worth retrying within
// the startup window; anything else is fatal.
//
// ECONNREFUSED is AMBIGUOUS for the unix transport (hk-rupvi): dialing a REGULAR
// FILE as a unix socket returns ECONNREFUSED on Linux — the SAME errno as a real
// socket that is present-but-not-listening (macOS returns a distinct errno, so
// this only bit Linux CI). Retrying a non-socket path burns the whole startup
// window and masks the misconfiguration. Disambiguate by stat: for the unix
// transport, if the address path EXISTS and is NOT a socket, the failure is a
// fatal misconfiguration (return false → bridge_dial_failed, no retry). TCP
// endpoints have no filesystem path, so their ECONNREFUSED stays retryable
// (listener still starting) — network+address are threaded in for exactly this
// stat.
func isRetryableDialErr(network, address string, err error) bool {
	// ENOENT: the endpoint has not been created yet — a genuine cold-boot race.
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return false // any other dial error is a fatal misconfiguration
	}
	// ECONNREFUSED on a unix path: fatal iff the path exists and is not a socket.
	if network == "unix" {
		if fi, statErr := os.Stat(address); statErr == nil && fi.Mode()&os.ModeSocket == 0 {
			return false
		}
	}
	return true
}

// isConnectionLostErr reports whether a POST-DIAL failure means the peer went
// away in the middle of the exchange, rather than the exchange itself being
// bad. EPIPE / ECONNRESET / EOF all say the same thing: the connection was
// established, and then the daemon on the other end stopped existing.
//
// This is the same daemon-restart / in-place-binary-swap race that CHB-016
// covers on the dial (docs/daemon-redeploy.md), reached one step later. Whether
// a restart surfaces as a dial error or as a mid-exchange drop is pure timing:
// dial before the daemon dies and the failure lands on the write or on the ACK
// read instead. Only the dial half was ever retried, so a relay that connected
// microseconds before the swap failed instantly while a relay that connected
// microseconds after recovered.
//
// A read-deadline expiry is deliberately NOT in this set. A timeout means the
// daemon is alive and slow, so a re-send could reach a daemon that already
// processed the first copy — the one case where re-sending is not provably
// harmless. Only re-send when the peer is known to be gone.
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

// errReconnect is an internal control signal, never returned to a caller: it
// tells the send loop to re-dial and re-send the same envelope.
var errReconnect = errors.New("reconnect")

// sendToSocket implements the one-shot write with daemon-not-ready retry per
// CHB-015 and CHB-016. socketPath is the HARMONIK_DAEMON_SOCKET value: a unix
// path for local runs, or a "tcp://127.0.0.1:<port>" reverse-tunnel endpoint for
// remote-worker runs (hk-ege6) — the transport is selected by resolveDialTarget.
//
// Re-sending an envelope is safe. The daemon accepts every message keyed by
// (run_id, claude_session_id) and takes the most recent one (CHB-025
// last-received-wins), so a duplicate of an identical envelope resolves to the
// same state as the original. After an actual daemon restart the question does
// not arise: the new daemon holds no memory of the session window, so the
// re-send is the only copy that can ever land.
func sendToSocket(socketPath string, msgBytes []byte, stderr io.Writer) error {
	network, address := resolveDialTarget(socketPath)
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
	wallCtx, cancelWall := context.WithTimeout(context.Background(), wallMax)
	defer cancelWall()
	retryDelay := retryBase

	for {
		// CHB-015: 5s dial timeout.
		dialCtx, cancelDial := context.WithTimeout(wallCtx, dialTimeout)
		conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, network, address)
		cancelDial()

		if dialErr != nil {
			if wallErr := wallCtx.Err(); wallErr != nil {
				return fmt.Errorf("bridge_daemon_startup_window_exceeded: dial failed after %v: %w", time.Since(wallStart), wallErr)
			}
			// CHB-016: a socket that is not yet listening (cold boot / in-place
			// binary swap per docs/daemon-redeploy.md) surfaces as a dial error,
			// not a daemon_not_ready ACK. Retry those within the startup window
			// on the same backoff schedule; anything else is fatal.
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

		// reconnectOrFail decides what a post-dial failure means. It always
		// closes conn. It returns errReconnect when the daemon went away and
		// the startup window still has room, so the caller re-dials and
		// re-sends; otherwise it returns the error to surface.
		//
		// None of these are dial failures. The dial SUCCEEDED — labelling them
		// bridge_dial_failed produced the error text "dial failed" for a
		// connection that had already been established, which sent at least one
		// investigation looking for a connect problem that never existed.
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

		// CHB-015: write exactly one NDJSON line terminated by \n.
		// Two sequential writes: the JSON bytes then the newline delimiter.
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

		// CHB-015: read back one NDJSON line within 5s.
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
			// A daemon that dies after accepting the envelope but before
			// acknowledging it closes the connection, which surfaces here as
			// EOF rather than as a write error — so this is the likelier half
			// of the restart race, not the rarer one. bufio.Scanner reports a
			// clean close as Err() == nil, hence the io.EOF substitution.
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

		// Any other non-ok status (bad_envelope, unknown_session, etc.) is unrecoverable.
		return fmt.Errorf("bridge_dial_failed: daemon rejected message: status=%s reason=%s", ack.Status, ack.Reason)
	}
}

// waitForRetry waits for delay or returns when the startup window ends.
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

// writeDiagnostic emits a best-effort stderr diagnostic. A failure writing the
// diagnostic cannot alter the already-determined hook result.
func writeDiagnostic(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}

// ErrMissingArg is returned when hook-relay is invoked without an event-kind argument.
var ErrMissingArg = errors.New("hook-relay: missing event-kind argument")
