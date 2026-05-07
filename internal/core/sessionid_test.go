package core

import (
	"encoding/json"
	"testing"
)

func TestSessionID_NominalTyping(t *testing.T) {
	// Verify that SessionID is a distinct named type and not interchangeable
	// with plain string at the type level.  The compiler enforces this; the
	// test confirms the conversion relationship.
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000001"
	s := SessionID(raw)
	back := string(s)
	if back != raw {
		t.Errorf("string round-trip failed: got %q, want %q", back, raw)
	}
}

func TestSessionID_JSONRoundTrip(t *testing.T) {
	// SessionID serialises as a plain JSON string via the underlying string type.
	type payload struct {
		ID SessionID `json:"session_id"`
	}

	const raw = "0196a1b2-c3d4-7000-8a1b-000000000002"
	p := payload{ID: SessionID(raw)}

	b, err := json.Marshal(&p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var p2 payload
	if err := json.Unmarshal(b, &p2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if p.ID != p2.ID {
		t.Errorf("JSON round-trip mismatch: got %q, want %q", p2.ID, p.ID)
	}
}

func TestSessionID_Comparison(t *testing.T) {
	// SessionIDs with the same underlying value are equal.
	const raw = "0196a1b2-c3d4-7000-8a1b-000000000003"
	a := SessionID(raw)
	b := SessionID(raw)
	if a != b {
		t.Errorf("equal SessionIDs compare unequal: %v vs %v", a, b)
	}

	c := SessionID("other-session")
	if a == c {
		t.Errorf("distinct SessionIDs compare equal: %v vs %v", a, c)
	}
}
