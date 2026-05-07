package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func mustParseWorkflowID(t *testing.T, s string) WorkflowID {
	t.Helper()

	var w WorkflowID
	if err := w.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", s, err)
	}

	return w
}

func TestWorkflowID_String(t *testing.T) {
	const raw = "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001"

	w := mustParseWorkflowID(t, raw)
	if got := w.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}
}

func TestWorkflowID_MarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000011"

	w := mustParseWorkflowID(t, raw)

	text, err := w.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	if got := string(text); got != raw {
		t.Errorf("MarshalText() = %q, want %q", got, raw)
	}
}

func TestWorkflowID_UnmarshalText_RoundTrip(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000012"

	w := mustParseWorkflowID(t, raw)
	text, err := w.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	var w2 WorkflowID
	if err := w2.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText(): %v", err)
	}

	if w != w2 {
		t.Errorf("round-trip mismatch: %v != %v", w, w2)
	}
}

func TestWorkflowID_UnmarshalText_Invalid(t *testing.T) {
	var w WorkflowID
	if err := w.UnmarshalText([]byte("not-a-uuid")); err == nil {
		t.Error("expected error for invalid UUID, got nil")
	}
}

func TestWorkflowID_NominalTyping(t *testing.T) {
	u := uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000013")
	w := WorkflowID(u)
	back := uuid.UUID(w)

	if back != u {
		t.Errorf("UUID round-trip failed: %v != %v", back, u)
	}
}

func TestWorkflowID_JSONViaMarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000014"

	type payload struct {
		ID WorkflowID `json:"workflow_id"`
	}

	w := mustParseWorkflowID(t, raw)
	p := payload{ID: w}

	b, err := json.Marshal(&p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if !strings.Contains(string(b), raw) {
		t.Errorf("JSON output %q does not contain %q", b, raw)
	}

	var p2 payload
	if err := json.Unmarshal(b, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if p.ID != p2.ID {
		t.Errorf("JSON round-trip mismatch: %v != %v", p.ID, p2.ID)
	}
}
