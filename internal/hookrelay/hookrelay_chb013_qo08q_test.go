package hookrelay_test

// hookrelay_chb013_qo08q_test.go — targeted tests for CHB-013 hook-event →
// progress-stream message mapping table.
//
// Bead: hk-qo08q.13 (CHB-013: Hook → progress-message mapping table).
// Spec: specs/claude-hook-bridge.md §4.5 CHB-013.
//
// CHB-013 mapping table (current spec, incl. hk-p63bz amendment):
//
//	hook event                              → progress-stream message
//	──────────────────────────────────────────────────────────────────
//	SessionStart {source: startup|resume}   → agent_ready{provenance="claude_session_start"}
//	SessionEnd                              → (no-op)
//	Stop (phase ∈ {single,impl-init,impl-resume}) → outcome_emitted{kind=WORK_COMPLETE}
//	Stop (phase = reviewer)                 → outcome_emitted{kind=REVIEWER_VERDICT} (CHB-014)
//	StopFailure {error_type: rate_limit}    → agent_rate_limited{retry_after_seconds=60}
//	StopFailure {error_type: server_error}  → outcome_emitted{kind=FAILURE_SIGNAL,suggested_class=transient}
//	StopFailure {error_type: <structural>}  → outcome_emitted{kind=FAILURE_SIGNAL,suggested_class=structural}
//	Notification {idle_prompt}              → agent_heartbeat{phase=waiting_input}
//	Notification {permission_prompt}        → agent_heartbeat{phase=waiting_input}
//	Notification {other}                    → agent_heartbeat{phase=reasoning}
//
// CHB-INV-002: the relay MUST NOT emit terminal events (agent_completed,
// agent_failed). These tests assert that no StopFailure mapping ever produces
// agent_failed — the handler-process is the sole terminal-event emitter.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// ─── SessionStart ────────────────────────────────────────────────────────────

// TestHookRelay_CHB013_SessionStart_AgentReady verifies that SessionStart
// synthesizes agent_ready with provenance="claude_session_start".
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 (as amended by hk-p63bz): relay
// synthesizes agent_ready on first SessionStart receipt under the tmux substrate;
// this is the first claude-originated lifecycle signal (HC-039).
func TestHookRelay_CHB013_SessionStart_AgentReady(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("SessionStart: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.Fatalf("SessionStart: unmarshal: %v", err)
		}
		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			t.Fatalf("SessionStart: unmarshal type: %v", err)
		}
		if msgType != "agent_ready" {
			t.Errorf("SessionStart: type=%q, want %q", msgType, "agent_ready")
		}
		var pl map[string]interface{}
		if err := json.Unmarshal(msg["payload"], &pl); err != nil {
			t.Fatalf("SessionStart: unmarshal payload: %v", err)
		}
		if pl["provenance"] != "claude_session_start" {
			t.Errorf("SessionStart: provenance=%v, want %q", pl["provenance"], "claude_session_start")
		}
	default:
		t.Error("SessionStart: no message received; expected agent_ready on socket")
	}
}

// ─── SessionEnd ──────────────────────────────────────────────────────────────

// TestHookRelay_CHB013_SessionEnd_NoOp verifies that SessionEnd is a no-op:
// exit 0, nothing written to the daemon socket.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 — SessionEnd → no-op;
// handler emits agent_completed on Wait-return.
func TestHookRelay_CHB013_SessionEnd_NoOp(t *testing.T) {
	t.Parallel()

	// DaemonSocket intentionally absent — no socket write should occur.
	e := hookRelayFixtureEnv(t.TempDir())
	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "SessionEnd", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionEnd", stdin, &stderr, &e)
	if code != 0 {
		t.Errorf("SessionEnd no-op: exit %d, want 0; stderr=%q", code, stderr.String())
	}
}

// ─── Stop ────────────────────────────────────────────────────────────────────

// TestHookRelay_CHB013_Stop_WorkComplete_AllImplementerPhases verifies that
// Stop in all implementer phases (single, implementer-initial, implementer-resume)
// emits outcome_emitted{kind=WORK_COMPLETE}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 Stop row — kind=WORK_COMPLETE for
// phase ∈ {single, implementer-initial, implementer-resume}.
func TestHookRelay_CHB013_Stop_WorkComplete_AllImplementerPhases(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"single", "implementer-initial", "implementer-resume", ""} {
		phase := phase
		t.Run("phase="+phase, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			e.Phase = phase
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", map[string]interface{}{
				"message": "work done summary",
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("Stop", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("Stop WORK_COMPLETE (phase=%q): exit %d, want 0; stderr=%q", phase, code, stderr.String())
			}

			select {
			case msgBytes := <-received:
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					t.Fatalf("Stop WORK_COMPLETE (phase=%q): unmarshal: %v", phase, err)
				}
				var msgType string
				if err := json.Unmarshal(msg["type"], &msgType); err != nil {
					t.Fatalf("Stop WORK_COMPLETE (phase=%q): unmarshal type: %v", phase, err)
				}
				if msgType != "outcome_emitted" {
					t.Errorf("Stop WORK_COMPLETE (phase=%q): type=%q, want outcome_emitted", phase, msgType)
				}
				var pl map[string]interface{}
				if err := json.Unmarshal(msg["payload"], &pl); err != nil {
					t.Fatalf("Stop WORK_COMPLETE (phase=%q): unmarshal payload: %v", phase, err)
				}
				if pl["kind"] != "WORK_COMPLETE" {
					t.Errorf("Stop WORK_COMPLETE (phase=%q): kind=%v, want WORK_COMPLETE", phase, pl["kind"])
				}
			default:
				t.Errorf("Stop WORK_COMPLETE (phase=%q): no message received", phase)
			}
		})
	}
}

// TestHookRelay_CHB013_Stop_ReviewerVerdictKind verifies that a reviewer-phase
// Stop emits outcome_emitted{kind=REVIEWER_VERDICT}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 Stop row — kind=REVIEWER_VERDICT for
// phase=reviewer; verdict file read per CHB-014.
func TestHookRelay_CHB013_Stop_ReviewerVerdictKind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	verdictJSON := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"ok"}`
	//nolint:gosec // G306: test fixture with non-secret content
	if err := os.WriteFile(filepath.Join(harmonikDir, "review.json"), []byte(verdictJSON), 0o644); err != nil {
		t.Fatalf("write review.json: %v", err)
	}

	e := hookRelayFixtureEnv(dir)
	e.Phase = "reviewer"
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Stop", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("Stop REVIEWER_VERDICT: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.Fatalf("Stop REVIEWER_VERDICT: unmarshal: %v", err)
		}
		var pl map[string]interface{}
		if err := json.Unmarshal(msg["payload"], &pl); err != nil {
			t.Fatalf("Stop REVIEWER_VERDICT: unmarshal payload: %v", err)
		}
		if pl["kind"] != "REVIEWER_VERDICT" {
			t.Errorf("Stop REVIEWER_VERDICT: kind=%v, want REVIEWER_VERDICT", pl["kind"])
		}
	default:
		t.Error("Stop REVIEWER_VERDICT: no message received")
	}
}

// ─── StopFailure ─────────────────────────────────────────────────────────────

// TestHookRelay_CHB013_StopFailure_RateLimit verifies that
// StopFailure{error_type=rate_limit} → agent_rate_limited{retry_after_seconds=60}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 StopFailure row.
func TestHookRelay_CHB013_StopFailure_RateLimit(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "rate_limit",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure rate_limit: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.Fatalf("StopFailure rate_limit: unmarshal: %v", err)
		}
		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			t.Fatalf("StopFailure rate_limit: unmarshal type: %v", err)
		}
		if msgType != "agent_rate_limited" {
			t.Errorf("StopFailure rate_limit: type=%q, want agent_rate_limited", msgType)
		}
		var pl map[string]interface{}
		if err := json.Unmarshal(msg["payload"], &pl); err != nil {
			t.Fatalf("StopFailure rate_limit: unmarshal payload: %v", err)
		}
		if pl["retry_after_seconds"] != float64(60) {
			t.Errorf("StopFailure rate_limit: retry_after_seconds=%v, want 60", pl["retry_after_seconds"])
		}
	default:
		t.Error("StopFailure rate_limit: no message received")
	}
}

// TestHookRelay_CHB013_StopFailure_AllStructuralTypes verifies that every
// structural StopFailure error_type (authentication_failed, oauth_org_not_allowed,
// billing_error, invalid_request, max_output_tokens, unknown) maps to
// outcome_emitted{kind=FAILURE_SIGNAL, suggested_class=structural,
// sub_reason="claude_<error_type>"}.
//
// CHB-INV-002: the relay MUST NOT emit agent_failed. This table-driven test
// asserts that none of these produce agent_failed.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 StopFailure rows.
func TestHookRelay_CHB013_StopFailure_AllStructuralTypes(t *testing.T) {
	t.Parallel()

	structuralTypes := []string{
		"authentication_failed",
		"oauth_org_not_allowed",
		"billing_error",
		"invalid_request",
		"max_output_tokens",
		"unknown",
	}

	for _, errorType := range structuralTypes {
		errorType := errorType
		t.Run(errorType, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
				"error_type": errorType,
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("StopFailure %s: exit %d, want 0; stderr=%q", errorType, code, stderr.String())
			}

			select {
			case msgBytes := <-received:
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					t.Fatalf("StopFailure %s: unmarshal: %v", errorType, err)
				}
				// CHB-INV-002: relay MUST NOT emit agent_failed.
				var msgType string
				if err := json.Unmarshal(msg["type"], &msgType); err != nil {
					t.Fatalf("StopFailure %s: unmarshal type: %v", errorType, err)
				}
				if msgType == "agent_failed" {
					t.Errorf("StopFailure %s: CHB-INV-002 violated — relay emitted agent_failed (terminal event)", errorType)
				}
				if msgType != "outcome_emitted" {
					t.Errorf("StopFailure %s: type=%q, want outcome_emitted", errorType, msgType)
				}
				var pl map[string]interface{}
				if err := json.Unmarshal(msg["payload"], &pl); err != nil {
					t.Fatalf("StopFailure %s: unmarshal payload: %v", errorType, err)
				}
				if pl["kind"] != "FAILURE_SIGNAL" {
					t.Errorf("StopFailure %s: kind=%v, want FAILURE_SIGNAL", errorType, pl["kind"])
				}
				if pl["suggested_class"] != "structural" {
					t.Errorf("StopFailure %s: suggested_class=%v, want structural", errorType, pl["suggested_class"])
				}
				wantSubReason := "claude_" + errorType
				if pl["sub_reason"] != wantSubReason {
					t.Errorf("StopFailure %s: sub_reason=%v, want %q", errorType, pl["sub_reason"], wantSubReason)
				}
			default:
				t.Errorf("StopFailure %s: no message received", errorType)
			}
		})
	}
}

// TestHookRelay_CHB013_StopFailure_ServerError verifies that
// StopFailure{error_type=server_error} → outcome_emitted{kind=FAILURE_SIGNAL,
// suggested_class=transient, sub_reason="claude_server_error"}.
//
// CHB-INV-002: relay MUST NOT emit agent_failed.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 StopFailure server_error row.
func TestHookRelay_CHB013_StopFailure_ServerError(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e.DaemonSocket = sockPath

	stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "StopFailure", map[string]interface{}{
		"error_type": "server_error",
	})
	var stderr bytes.Buffer
	code := hookrelay.Run("StopFailure", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("StopFailure server_error: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.Fatalf("StopFailure server_error: unmarshal: %v", err)
		}
		var msgType string
		if err := json.Unmarshal(msg["type"], &msgType); err != nil {
			t.Fatalf("StopFailure server_error: unmarshal type: %v", err)
		}
		// CHB-INV-002: relay MUST NOT emit agent_failed.
		if msgType == "agent_failed" {
			t.Errorf("StopFailure server_error: CHB-INV-002 violated — relay emitted agent_failed (terminal event)")
		}
		if msgType != "outcome_emitted" {
			t.Errorf("StopFailure server_error: type=%q, want outcome_emitted", msgType)
		}
		var pl map[string]interface{}
		if err := json.Unmarshal(msg["payload"], &pl); err != nil {
			t.Fatalf("StopFailure server_error: unmarshal payload: %v", err)
		}
		if pl["kind"] != "FAILURE_SIGNAL" {
			t.Errorf("StopFailure server_error: kind=%v, want FAILURE_SIGNAL", pl["kind"])
		}
		if pl["suggested_class"] != "transient" {
			t.Errorf("StopFailure server_error: suggested_class=%v, want transient", pl["suggested_class"])
		}
		if pl["sub_reason"] != "claude_server_error" {
			t.Errorf("StopFailure server_error: sub_reason=%v, want claude_server_error", pl["sub_reason"])
		}
	default:
		t.Error("StopFailure server_error: no message received")
	}
}

// ─── Notification ────────────────────────────────────────────────────────────

// TestHookRelay_CHB013_Notification_WaitingInput verifies that
// Notification{idle_prompt} and Notification{permission_prompt} both map to
// agent_heartbeat{phase=waiting_input}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 Notification row.
func TestHookRelay_CHB013_Notification_WaitingInput(t *testing.T) {
	t.Parallel()

	for _, notifType := range []string{"idle_prompt", "permission_prompt"} {
		notifType := notifType
		t.Run(notifType, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Notification", map[string]interface{}{
				"notification_type": notifType,
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("Notification", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("Notification %s: exit %d, want 0; stderr=%q", notifType, code, stderr.String())
			}

			select {
			case msgBytes := <-received:
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					t.Fatalf("Notification %s: unmarshal: %v", notifType, err)
				}
				var msgType string
				if err := json.Unmarshal(msg["type"], &msgType); err != nil {
					t.Fatalf("Notification %s: unmarshal type: %v", notifType, err)
				}
				if msgType != "agent_heartbeat" {
					t.Errorf("Notification %s: type=%q, want agent_heartbeat", notifType, msgType)
				}
				var pl map[string]interface{}
				if err := json.Unmarshal(msg["payload"], &pl); err != nil {
					t.Fatalf("Notification %s: unmarshal payload: %v", notifType, err)
				}
				if pl["phase"] != "waiting_input" {
					t.Errorf("Notification %s: phase=%v, want waiting_input", notifType, pl["phase"])
				}
			default:
				t.Errorf("Notification %s: no message received", notifType)
			}
		})
	}
}

// TestHookRelay_CHB013_Notification_Reasoning verifies that Notification events
// with any type other than idle_prompt/permission_prompt map to
// agent_heartbeat{phase=reasoning}.
//
// Spec: claude-hook-bridge.md §4.5 CHB-013 Notification row — other types → reasoning.
func TestHookRelay_CHB013_Notification_Reasoning(t *testing.T) {
	t.Parallel()

	for _, notifType := range []string{"progress", "tool_use", "thinking", "", "unknown_future_type"} {
		notifType := notifType
		t.Run("type="+notifType, func(t *testing.T) {
			t.Parallel()

			e := hookRelayFixtureEnv(t.TempDir())
			sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
			e.DaemonSocket = sockPath

			stdin := hookRelayFixtureStdin(e.ClaudeSessionID, "Notification", map[string]interface{}{
				"notification_type": notifType,
			})
			var stderr bytes.Buffer
			code := hookrelay.Run("Notification", stdin, &stderr, &e)
			if code != 0 {
				t.Fatalf("Notification (type=%q): exit %d, want 0; stderr=%q", notifType, code, stderr.String())
			}

			select {
			case msgBytes := <-received:
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(msgBytes, &msg); err != nil {
					t.Fatalf("Notification (type=%q): unmarshal: %v", notifType, err)
				}
				var pl map[string]interface{}
				if err := json.Unmarshal(msg["payload"], &pl); err != nil {
					t.Fatalf("Notification (type=%q): unmarshal payload: %v", notifType, err)
				}
				if pl["phase"] != "reasoning" {
					t.Errorf("Notification (type=%q): phase=%v, want reasoning", notifType, pl["phase"])
				}
			default:
				t.Errorf("Notification (type=%q): no message received", notifType)
			}
		})
	}
}
