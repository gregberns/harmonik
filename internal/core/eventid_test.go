package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// mustParseEventID constructs an EventID from a UUID string, failing the test on error.
func mustParseEventID(t *testing.T, s string) EventID {
	t.Helper()

	var e EventID
	if err := e.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", s, err)
	}

	return e
}

func TestEventID_String(t *testing.T) {
	// UUIDv7 canonical form: 8-4-4-4-12 hex digits with hyphens.
	const raw = "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f6a7b"

	e := mustParseEventID(t, raw)
	if got := e.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}
}

func TestEventID_MarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000001"

	e := mustParseEventID(t, raw)

	text, err := e.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	if got := string(text); got != raw {
		t.Errorf("MarshalText() = %q, want %q", got, raw)
	}
}

func TestEventID_UnmarshalText_RoundTrip(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000002"

	e := mustParseEventID(t, raw)
	text, err := e.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	var e2 EventID
	if err := e2.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText(): %v", err)
	}

	if e != e2 {
		t.Errorf("round-trip mismatch: %v != %v", e, e2)
	}
}

func TestEventID_UnmarshalText_Invalid(t *testing.T) {
	var e EventID
	if err := e.UnmarshalText([]byte("not-a-uuid")); err == nil {
		t.Error("expected error for invalid UUID, got nil")
	}
}

// TestEventID_ZeroValue verifies that the zero value of EventID equals the
// nil UUID (uuid.Nil), which is the sentinel used in Event.Valid().
func TestEventID_ZeroValue(t *testing.T) {
	var e EventID
	if uuid.UUID(e) != uuid.Nil {
		t.Errorf("zero EventID is not uuid.Nil: %v", e)
	}
}

// TestEventID_NominalTyping verifies that EventID is a distinct named type and not
// interchangeable with uuid.UUID at the type level. This is enforced by the
// compiler: the test uses explicit conversion to confirm the relationship.
func TestEventID_NominalTyping(t *testing.T) {
	u := uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003")
	e := EventID(u)
	back := uuid.UUID(e)

	if back != u {
		t.Errorf("UUID round-trip failed: %v != %v", back, u)
	}
}

// TestEventID_JSONViaMarshalText verifies that EventID serialises correctly when
// used as a JSON field via encoding.TextMarshaler/TextUnmarshaler.
func TestEventID_JSONViaMarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000004"

	type payload struct {
		ID EventID `json:"event_id"`
	}

	e := mustParseEventID(t, raw)
	p := payload{ID: e}

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
