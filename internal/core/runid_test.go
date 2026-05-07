package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// mustParseRunID constructs a RunID from a UUID string, failing the test on error.
func mustParseRunID(t *testing.T, s string) RunID {
	t.Helper()

	var r RunID
	if err := r.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", s, err)
	}

	return r
}

func TestRunID_String(t *testing.T) {
	// UUIDv7 canonical form: 8-4-4-4-12 hex digits with hyphens.
	const raw = "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f6a7b"

	r := mustParseRunID(t, raw)
	if got := r.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}
}

func TestRunID_MarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000001"

	r := mustParseRunID(t, raw)

	text, err := r.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	if got := string(text); got != raw {
		t.Errorf("MarshalText() = %q, want %q", got, raw)
	}
}

func TestRunID_UnmarshalText_RoundTrip(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000002"

	r := mustParseRunID(t, raw)
	text, err := r.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	var r2 RunID
	if err := r2.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText(): %v", err)
	}

	if r != r2 {
		t.Errorf("round-trip mismatch: %v != %v", r, r2)
	}
}

func TestRunID_UnmarshalText_Invalid(t *testing.T) {
	var r RunID
	if err := r.UnmarshalText([]byte("not-a-uuid")); err == nil {
		t.Error("expected error for invalid UUID, got nil")
	}
}

// TestRunID_NominalTyping verifies that RunID is a distinct named type and not
// interchangeable with uuid.UUID at the type level. This is enforced by the
// compiler: the test uses explicit conversion to confirm the relationship.
func TestRunID_NominalTyping(t *testing.T) {
	u := uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003")
	r := RunID(u)
	back := uuid.UUID(r)

	if back != u {
		t.Errorf("UUID round-trip failed: %v != %v", back, u)
	}
}

// TestRunID_JSONViaMarshalText verifies that RunID serialises correctly when
// used as a JSON field via encoding.TextMarshaler/TextUnmarshaler.
func TestRunID_JSONViaMarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000004"

	type payload struct {
		ID RunID `json:"run_id"`
	}

	r := mustParseRunID(t, raw)
	p := payload{ID: r}

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
