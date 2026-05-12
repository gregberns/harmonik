package handlercontract_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// claudesessionFixture builds a JSON string that mimics `claude -p ...
// --output-format json` output for test purposes.
//
// Helper prefix: claudesessionFixture (bead hk-7om2q.18; distinct from other
// handlercontract helper prefixes).
func claudesessionFixtureJSON(sessionID string) string {
	if sessionID == "" {
		return `{"result":"done","cost_usd":0.001,"duration_ms":1234}`
	}
	return `{"session_id":"` + sessionID + `","result":"done","cost_usd":0.001,"duration_ms":1234}`
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseClaudeSessionID happy path
// ─────────────────────────────────────────────────────────────────────────────

// TestParseClaudeSessionID_ExtractsSessionID verifies that a well-formed
// claude --output-format json payload returns the correct session_id string.
func TestParseClaudeSessionID_ExtractsSessionID(t *testing.T) {
	t.Parallel()

	const want = "sess_01AbCdEfGhIjKlMnOpQrStUv"
	payload := claudesessionFixtureJSON(want)

	got, err := handlercontract.ParseClaudeSessionID(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ParseClaudeSessionID: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("ParseClaudeSessionID: got %q, want %q", got, want)
	}
}

// TestParseClaudeSessionID_AdditionalFieldsIgnored verifies that extra fields
// in the claude JSON output do not prevent session_id extraction.
func TestParseClaudeSessionID_AdditionalFieldsIgnored(t *testing.T) {
	t.Parallel()

	payload := `{
		"session_id": "sess_extra",
		"result": "Task complete",
		"cost_usd": 0.042,
		"duration_ms": 9876,
		"num_turns": 3,
		"unknown_future_field": true
	}`

	got, err := handlercontract.ParseClaudeSessionID(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("ParseClaudeSessionID: unexpected error: %v", err)
	}
	if got != "sess_extra" {
		t.Errorf("ParseClaudeSessionID: got %q, want %q", got, "sess_extra")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseClaudeSessionID missing session_id
// ─────────────────────────────────────────────────────────────────────────────

// TestParseClaudeSessionID_MissingSessionID verifies that absent session_id
// returns ErrMissingClaudeSessionID (wrapping ErrStructural).
func TestParseClaudeSessionID_MissingSessionID(t *testing.T) {
	t.Parallel()

	payload := claudesessionFixtureJSON("") // produces JSON without session_id key

	_, err := handlercontract.ParseClaudeSessionID(strings.NewReader(payload))
	if err == nil {
		t.Fatal("ParseClaudeSessionID: expected error for absent session_id, got nil")
	}
	if !errors.Is(err, handlercontract.ErrMissingClaudeSessionID) {
		t.Errorf("ParseClaudeSessionID: errors.Is(err, ErrMissingClaudeSessionID) = false; got %v", err)
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("ParseClaudeSessionID: errors.Is(err, ErrStructural) = false; want true (ErrMissingClaudeSessionID wraps ErrStructural)")
	}
}

// TestParseClaudeSessionID_EmptySessionIDField verifies that an explicit
// empty-string session_id field is treated as missing and returns
// ErrMissingClaudeSessionID.
func TestParseClaudeSessionID_EmptySessionIDField(t *testing.T) {
	t.Parallel()

	payload := `{"session_id":"","result":"done"}`

	_, err := handlercontract.ParseClaudeSessionID(strings.NewReader(payload))
	if err == nil {
		t.Fatal("ParseClaudeSessionID: expected error for empty session_id field, got nil")
	}
	if !errors.Is(err, handlercontract.ErrMissingClaudeSessionID) {
		t.Errorf("ParseClaudeSessionID: errors.Is(err, ErrMissingClaudeSessionID) = false; got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseClaudeSessionID parse errors
// ─────────────────────────────────────────────────────────────────────────────

// TestParseClaudeSessionID_MalformedJSON verifies that invalid JSON returns a
// wrapped ErrStructural.
func TestParseClaudeSessionID_MalformedJSON(t *testing.T) {
	t.Parallel()

	payload := `this is not json`

	_, err := handlercontract.ParseClaudeSessionID(strings.NewReader(payload))
	if err == nil {
		t.Fatal("ParseClaudeSessionID: expected error for malformed JSON, got nil")
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("ParseClaudeSessionID: errors.Is(err, ErrStructural) = false; got %v", err)
	}
}

// TestParseClaudeSessionID_EmptyInput verifies that an empty reader returns a
// wrapped ErrStructural (JSON parse failure on zero bytes).
func TestParseClaudeSessionID_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := handlercontract.ParseClaudeSessionID(strings.NewReader(""))
	if err == nil {
		t.Fatal("ParseClaudeSessionID: expected error for empty input, got nil")
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("ParseClaudeSessionID: errors.Is(err, ErrStructural) = false; got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ErrMissingClaudeSessionID sentinel checks
// ─────────────────────────────────────────────────────────────────────────────

// TestErrMissingClaudeSessionID_WrapsErrStructural verifies the sentinel error
// chain at package level (independent of ParseClaudeSessionID).
func TestErrMissingClaudeSessionID_WrapsErrStructural(t *testing.T) {
	t.Parallel()

	if !errors.Is(handlercontract.ErrMissingClaudeSessionID, handlercontract.ErrStructural) {
		t.Errorf("ErrMissingClaudeSessionID: errors.Is(ErrMissingClaudeSessionID, ErrStructural) = false; want true")
	}
}
