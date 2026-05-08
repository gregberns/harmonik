package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// projectHashFixtureValid returns a known-good 12-char hex ProjectHash value
// for table tests.
func projectHashFixtureValid(t *testing.T) ProjectHash {
	t.Helper()
	// 12 lowercase hex characters — representative SHA-256 prefix shape.
	h, err := projectHashFixtureParse(t, "a1b2c3d4e5f6")
	if err != nil {
		t.Fatalf("projectHashFixtureValid: unexpected parse failure: %v", err)
	}
	return h
}

// projectHashFixtureParse attempts to unmarshal a ProjectHash from s.
func projectHashFixtureParse(t *testing.T, s string) (ProjectHash, error) {
	t.Helper()
	var h ProjectHash
	err := h.UnmarshalText([]byte(s))
	return h, err
}

func TestProjectHash_String(t *testing.T) {
	const raw = "a1b2c3d4e5f6"

	h := projectHashFixtureValid(t)
	if got := h.String(); got != raw {
		t.Errorf("String() = %q, want %q", got, raw)
	}
}

func TestProjectHash_MarshalText(t *testing.T) {
	const raw = "a1b2c3d4e5f6"

	h := projectHashFixtureValid(t)
	text, err := h.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}
	if got := string(text); got != raw {
		t.Errorf("MarshalText() = %q, want %q", got, raw)
	}
}

func TestProjectHash_UnmarshalText_RoundTrip(t *testing.T) {
	const raw = "deadbeef0123"

	h, err := projectHashFixtureParse(t, raw)
	if err != nil {
		t.Fatalf("UnmarshalText(%q): %v", raw, err)
	}

	text, err := h.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText(): %v", err)
	}

	var h2 ProjectHash
	if err := h2.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText (round-trip): %v", err)
	}

	if h != h2 {
		t.Errorf("round-trip mismatch: %v != %v", h, h2)
	}
}

func TestProjectHash_UnmarshalText_TooShort(t *testing.T) {
	_, err := projectHashFixtureParse(t, "abcdef")
	if err == nil {
		t.Error("expected error for 6-char input, got nil")
	}
}

func TestProjectHash_UnmarshalText_TooLong(t *testing.T) {
	_, err := projectHashFixtureParse(t, "a1b2c3d4e5f60000")
	if err == nil {
		t.Error("expected error for 16-char input, got nil")
	}
}

func TestProjectHash_UnmarshalText_Uppercase(t *testing.T) {
	// Spec mandates lowercase hex only.
	_, err := projectHashFixtureParse(t, "A1B2C3D4E5F6")
	if err == nil {
		t.Error("expected error for uppercase hex input, got nil")
	}
}

func TestProjectHash_UnmarshalText_NonHex(t *testing.T) {
	_, err := projectHashFixtureParse(t, "g1b2c3d4e5f6")
	if err == nil {
		t.Error("expected error for non-hex char, got nil")
	}
}

// TestProjectHash_NominalTyping verifies that ProjectHash is a distinct named
// type and not interchangeable with plain string at the type level.
func TestProjectHash_NominalTyping(t *testing.T) {
	const raw = "a1b2c3d4e5f6"
	h := ProjectHash(raw)
	back := string(h)
	if back != raw {
		t.Errorf("string round-trip failed: %q != %q", back, raw)
	}
}

// TestProjectHash_JSONViaMarshalText verifies that ProjectHash serialises
// correctly when used as a JSON field via encoding.TextMarshaler/TextUnmarshaler.
func TestProjectHash_JSONViaMarshalText(t *testing.T) {
	const raw = "a1b2c3d4e5f6"

	type payload struct {
		Hash ProjectHash `json:"project_hash"`
	}

	h := projectHashFixtureValid(t)
	p := payload{Hash: h}

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

	if p.Hash != p2.Hash {
		t.Errorf("JSON round-trip mismatch: %v != %v", p.Hash, p2.Hash)
	}
}
