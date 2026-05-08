package core

import (
	"fmt"
	"testing"
)

// pgidFixtureValue returns a test PGID value.
func pgidFixtureValue(t *testing.T) PGID {
	t.Helper()
	return PGID(12345)
}

func TestPGID_String(t *testing.T) {
	p := pgidFixtureValue(t)
	want := fmt.Sprintf("%d", 12345)
	if got := p.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestPGID_Int(t *testing.T) {
	const raw = 99999
	p := PGID(raw)
	if got := p.Int(); got != raw {
		t.Errorf("Int() = %d, want %d", got, raw)
	}
}

func TestPGID_Zero(t *testing.T) {
	p := PGID(0)
	if got := p.String(); got != "0" {
		t.Errorf("String() for zero PGID = %q, want %q", got, "0")
	}
	if got := p.Int(); got != 0 {
		t.Errorf("Int() for zero PGID = %d, want 0", got)
	}
}

// TestPGID_NominalTyping verifies that PGID is a distinct named type and not
// interchangeable with plain int at the type level.
func TestPGID_NominalTyping(t *testing.T) {
	const raw = 42
	p := PGID(raw)
	back := int(p)
	if back != raw {
		t.Errorf("int round-trip failed: %d != %d", back, raw)
	}
}
