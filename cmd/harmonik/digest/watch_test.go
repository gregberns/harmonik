package digestcmd

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/digest"
)

// TestFormatDuration verifies compact human-readable age formatting.
func TestFormatDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{time.Second, "1s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m00s"},
		{90 * time.Second, "1m30s"},
		{2*time.Minute + 5*time.Second, "2m05s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{time.Hour, "1h00m"},
		{time.Hour + 15*time.Minute, "1h15m"},
		{2*time.Hour + 3*time.Minute, "2h03m"},
	}
	for _, c := range cases {
		got := formatDuration(c.d)
		if got != c.want {
			t.Errorf("formatDuration(%v) = %q; want %q", c.d, got, c.want)
		}
	}
}

// TestFilterEventsByType verifies that only events with matching types are returned.
func TestFilterEventsByType(t *testing.T) {
	t.Parallel()
	events := []digest.EventSummary{
		{EventID: "aaa", Type: "run_completed"},
		{EventID: "bbb", Type: "run_failed"},
		{EventID: "ccc", Type: "run_started"},
		{EventID: "ddd", Type: "run_completed"},
	}

	// Filter for completions only.
	got := filterEventsByType(events, "run_completed", "run_failed")
	if len(got) != 3 {
		t.Fatalf("expected 3 events; got %d", len(got))
	}
	for _, ev := range got {
		if ev.Type != "run_completed" && ev.Type != "run_failed" {
			t.Errorf("unexpected type %q in results", ev.Type)
		}
	}

	// Filter for a type that is absent.
	none := filterEventsByType(events, "merge_conflict")
	if len(none) != 0 {
		t.Errorf("expected 0 events; got %d", len(none))
	}

	// Empty type list returns nothing.
	empty := filterEventsByType(events)
	if len(empty) != 0 {
		t.Errorf("expected 0 events with no filter; got %d", len(empty))
	}
}

// TestUUIDv7Age verifies UUIDv7 timestamp extraction and age computation.
func TestUUIDv7Age(t *testing.T) {
	t.Parallel()

	// A well-formed UUIDv7 whose first 48 bits encode a known Unix millisecond.
	// 2024-01-01 00:00:00 UTC = 1704067200000 ms.
	// Build a valid UUIDv7 manually: first 48 bits = timestamp, rest arbitrary.
	tsMillis := int64(1704067200000) // 2024-01-01T00:00:00Z
	var raw [16]byte
	raw[0] = byte(tsMillis >> 40)
	raw[1] = byte(tsMillis >> 32)
	raw[2] = byte(tsMillis >> 24)
	raw[3] = byte(tsMillis >> 16)
	raw[4] = byte(tsMillis >> 8)
	raw[5] = byte(tsMillis)
	raw[6] = 0x70 // version nibble (7) in top 4 bits
	raw[7] = 0x80
	// Remaining bytes can be zero.

	// Format as UUID string (8-4-4-4-12 hex).
	uuidStr := formatUUIDBytes(raw)

	// Reference "now" = 5 seconds after the encoded timestamp.
	refNow := time.Unix(tsMillis/1000, 0).Add(5 * time.Second)
	got := uuidv7Age(uuidStr, refNow)
	if got != "5s" {
		t.Errorf("uuidv7Age with 5s offset = %q; want %q", got, "5s")
	}

	// Reference "now" = exactly at the timestamp (age 0).
	at := time.Unix(tsMillis/1000, 0)
	got = uuidv7Age(uuidStr, at)
	if got != "0s" {
		t.Errorf("uuidv7Age at timestamp = %q; want %q", got, "0s")
	}

	// Invalid / short string returns "?".
	if v := uuidv7Age("", time.Now()); v != "?" {
		t.Errorf("uuidv7Age(\"\") = %q; want %q", v, "?")
	}
	if v := uuidv7Age("not-a-uuid", time.Now()); v != "?" {
		t.Errorf("uuidv7Age(\"not-a-uuid\") = %q; want %q", v, "?")
	}

	// Zero timestamp bytes returns "?".
	zeroUUID := "00000000-0000-0000-0000-000000000000"
	if v := uuidv7Age(zeroUUID, time.Now()); v != "?" {
		t.Errorf("uuidv7Age(zero) = %q; want %q", v, "?")
	}
}

// formatUUIDBytes converts a 16-byte array into a standard UUID string.
func formatUUIDBytes(b [16]byte) string {
	const hx = "0123456789abcdef"
	buf := make([]byte, 36)
	groups := [5][2]int{{0, 4}, {4, 6}, {6, 8}, {8, 10}, {10, 16}}
	pos := 0
	for gi, g := range groups {
		for i := g[0]; i < g[1]; i++ {
			buf[pos] = hx[b[i]>>4]
			buf[pos+1] = hx[b[i]&0xf]
			pos += 2
		}
		if gi < 4 {
			buf[pos] = '-'
			pos++
		}
	}
	return string(buf)
}
