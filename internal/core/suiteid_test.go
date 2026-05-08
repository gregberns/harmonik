package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// mustParseSuiteID constructs a SuiteID from a UUID string, failing the test on error.
func mustParseSuiteID(t *testing.T, s string) SuiteID {
	t.Helper()

	var id SuiteID
	if err := id.UnmarshalText([]byte(s)); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", s, err)
	}

	return id
}

func TestSuiteID_String(t *testing.T) {
	// UUIDv7 canonical form: 8-4-4-4-12 hex digits with hyphens.
	const raw = "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f6a7b"

	id := mustParseSuiteID(t, raw)
	if got := id.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}
}

func TestSuiteID_MarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000001"

	id := mustParseSuiteID(t, raw)

	text, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	if got := string(text); got != raw {
		t.Errorf("MarshalText() = %q, want %q", got, raw)
	}
}

func TestSuiteID_UnmarshalText_RoundTrip(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000002"

	id := mustParseSuiteID(t, raw)
	text, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	var id2 SuiteID
	if err := id2.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText(): %v", err)
	}

	if id != id2 {
		t.Errorf("round-trip mismatch: %v != %v", id, id2)
	}
}

func TestSuiteID_UnmarshalText_Invalid(t *testing.T) {
	var id SuiteID
	if err := id.UnmarshalText([]byte("not-a-uuid")); err == nil {
		t.Error("expected error for invalid UUID, got nil")
	}
}

// TestSuiteID_NominalTyping verifies that SuiteID is a distinct named type and not
// interchangeable with uuid.UUID at the type level. This is enforced by the
// compiler: the test uses explicit conversion to confirm the relationship.
func TestSuiteID_NominalTyping(t *testing.T) {
	u := uuid.MustParse("0196a1b2-c3d4-7000-8a1b-000000000003")
	id := SuiteID(u)
	back := uuid.UUID(id)

	if back != u {
		t.Errorf("UUID round-trip failed: %v != %v", back, u)
	}
}

// TestSuiteID_JSONViaMarshalText verifies that SuiteID serialises correctly when
// used as a JSON field via encoding.TextMarshaler/TextUnmarshaler.
func TestSuiteID_JSONViaMarshalText(t *testing.T) {
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000004"

	type payload struct {
		ID SuiteID `json:"suite_id"`
	}

	id := mustParseSuiteID(t, raw)
	p := payload{ID: id}

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
