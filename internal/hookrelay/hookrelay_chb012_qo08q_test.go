package hookrelay_test

// hookrelay_chb012_qo08q_test.go — targeted tests for CHB-012 required-field
// presence validation on the hook-relay stdin payload.
//
// Bead: hk-qo08q.12 (CHB-012: stdin payload validation).
// Spec: specs/claude-hook-bridge.md §4.4 CHB-012, §8 error taxonomy.
//
// Error taxonomy (§8):
//   bridge_malformed_hook_payload — stdin JSON malformed OR required field missing
//   bridge_session_id_mismatch    — session_id present but does not match env var
//   bridge_event_kind_mismatch    — hook_event_name present but does not match argv
//
// These tests verify that absent fields produce bridge_malformed_hook_payload (not
// the mismatch codes), distinguishing "absent" from "present but wrong".

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// TestHookRelay_CHB012_SessionIDAbsent verifies that an empty session_id field
// produces bridge_malformed_hook_payload (not bridge_session_id_mismatch).
//
// Spec: claude-hook-bridge.md §4.4 CHB-012 — required field session_id must be present.
func TestHookRelay_CHB012_SessionIDAbsent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	// Build payload with session_id explicitly empty (absent value).
	payload := map[string]interface{}{
		"session_id":      "",
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/transcript.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
	}
	b, _ := json.Marshal(payload)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(b), &stderr, &e)

	if code != 1 {
		t.Errorf("absent session_id: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_malformed_hook_payload") {
		t.Errorf("absent session_id: stderr missing bridge_malformed_hook_payload, got %q", stderr.String())
	}
	// Must NOT emit the mismatch code — absent field is a different error class.
	if strings.Contains(stderr.String(), "bridge_session_id_mismatch") {
		t.Errorf("absent session_id: stderr must not contain bridge_session_id_mismatch for absent field, got %q", stderr.String())
	}
}

// TestHookRelay_CHB012_TranscriptPathAbsent verifies that an empty transcript_path
// field produces bridge_malformed_hook_payload.
//
// Spec: claude-hook-bridge.md §4.4 CHB-012 — required field transcript_path must be present.
func TestHookRelay_CHB012_TranscriptPathAbsent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	payload := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "Stop",
		"transcript_path": "",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
	}
	b, _ := json.Marshal(payload)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(b), &stderr, &e)

	if code != 1 {
		t.Errorf("absent transcript_path: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_malformed_hook_payload") {
		t.Errorf("absent transcript_path: stderr missing bridge_malformed_hook_payload, got %q", stderr.String())
	}
}

// TestHookRelay_CHB012_HookEventNameAbsent verifies that an empty hook_event_name
// field produces bridge_malformed_hook_payload (not bridge_event_kind_mismatch).
//
// Spec: claude-hook-bridge.md §4.4 CHB-012 — required field hook_event_name must be present.
func TestHookRelay_CHB012_HookEventNameAbsent(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	payload := map[string]interface{}{
		"session_id":      e.ClaudeSessionID,
		"hook_event_name": "",
		"transcript_path": "/tmp/transcript.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
	}
	b, _ := json.Marshal(payload)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(b), &stderr, &e)

	if code != 1 {
		t.Errorf("absent hook_event_name: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_malformed_hook_payload") {
		t.Errorf("absent hook_event_name: stderr missing bridge_malformed_hook_payload, got %q", stderr.String())
	}
	// Must NOT emit the mismatch code — absent field is a different error class.
	if strings.Contains(stderr.String(), "bridge_event_kind_mismatch") {
		t.Errorf("absent hook_event_name: stderr must not contain bridge_event_kind_mismatch for absent field, got %q", stderr.String())
	}
}

// TestHookRelay_CHB012_AllRequiredPresent_MismatchStillFires verifies that when
// all required fields are present, the existing mismatch checks remain operative.
// This guards against the required-field block accidentally short-circuiting the
// mismatch checks for non-empty but wrong values.
//
// Spec: claude-hook-bridge.md §4.4 CHB-012.
func TestHookRelay_CHB012_AllRequiredPresent_MismatchStillFires(t *testing.T) {
	t.Parallel()

	e := hookRelayFixtureEnv(t.TempDir())
	// All required fields present; session_id is wrong (non-empty but mismatched).
	payload := map[string]interface{}{
		"session_id":      "not-the-right-session-id",
		"hook_event_name": "Stop",
		"transcript_path": "/tmp/transcript.jsonl",
		"cwd":             "/tmp/ws",
		"permission_mode": "auto",
	}
	b, _ := json.Marshal(payload)
	var stderr bytes.Buffer
	code := hookrelay.Run("Stop", bytes.NewReader(b), &stderr, &e)

	if code != 1 {
		t.Errorf("session_id mismatch: exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "bridge_session_id_mismatch") {
		t.Errorf("session_id mismatch: stderr missing bridge_session_id_mismatch, got %q", stderr.String())
	}
}
