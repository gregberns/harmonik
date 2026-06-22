package core

// Tests for EV-002c HWM persistence utilities:
//
//	TestReadEventIDHWM_Missing       — missing file → (zero, false, nil)
//	TestReadEventIDHWM_Valid         — round-trip read after WriteEventIDHWMAtomicNoSync
//	TestReadEventIDHWM_Corrupt       — malformed content → (zero, false, err)
//	TestWriteEventIDHWMAtomicNoSync  — atomic write, readable back
//	TestExtractUUIDv7Timestamp       — high-48-bit extraction is within 1s of now
//	TestIsHWMClockRegression_Below   — wall clock ahead of HWM → false
//	TestIsHWMClockRegression_Above   — wall clock >1s behind HWM → true
//	TestIsHWMClockRegression_Edge    — wall clock exactly 1s behind → false (threshold is >1s)
//	TestNewEventIDGeneratorWithHWM   — first Next() is strictly > HWM under clock regression

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// hwmFixtureMakeID returns a UUIDv7 with the embedded timestamp set to wallClock,
// suitable for testing timestamp-extraction and clock-regression helpers.
func hwmFixtureMakeID(t *testing.T, wallClock time.Time) EventID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hwmFixtureMakeID: uuid.NewV7(): %v", err)
	}
	// Overwrite the high-48-bit ms timestamp to match wallClock.
	ms := uint64(wallClock.UnixMilli())
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)
	return EventID(u)
}

// TestReadEventIDHWM_Missing asserts that ReadEventIDHWM returns (zero, false, nil)
// when the HWM file does not exist.
func TestReadEventIDHWM_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event_id_hwm")
	hwm, exists, err := ReadEventIDHWM(path)
	if err != nil {
		t.Fatalf("EV-002c: missing HWM should return nil error, got: %v", err)
	}
	if exists {
		t.Fatal("EV-002c: missing HWM should return exists=false")
	}
	var zero EventID
	if hwm != zero {
		t.Fatalf("EV-002c: missing HWM should return zero EventID, got %v", hwm)
	}
}

// TestReadEventIDHWM_Corrupt asserts that ReadEventIDHWM returns (zero, false, err)
// when the HWM file exists but contains malformed content.
func TestReadEventIDHWM_Corrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event_id_hwm")

	for _, corrupt := range []string{"", "not-hex!", "deadbeef" /* too short */} {
		if writeErr := os.WriteFile(path, []byte(corrupt), 0o644); writeErr != nil {
			t.Fatalf("setup: write corrupt HWM: %v", writeErr)
		}
		_, _, err := ReadEventIDHWM(path)
		if err == nil {
			t.Errorf("EV-002c: corrupt content %q should return non-nil error", corrupt)
		}
	}
}

// TestWriteEventIDHWMAtomicNoSync asserts that WriteEventIDHWMAtomicNoSync
// writes a readable 32-char hex UUID and that a subsequent ReadEventIDHWM
// returns the same value.
func TestWriteEventIDHWMAtomicNoSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event_id_hwm")

	gen := NewEventIDGenerator()
	want, err := gen.Next()
	if err != nil {
		t.Fatalf("gen.Next(): %v", err)
	}

	if writeErr := WriteEventIDHWMAtomicNoSync(path, want); writeErr != nil {
		t.Fatalf("EV-002c: WriteEventIDHWMAtomicNoSync: %v", writeErr)
	}

	got, exists, readErr := ReadEventIDHWM(path)
	if readErr != nil {
		t.Fatalf("EV-002c: ReadEventIDHWM after write: %v", readErr)
	}
	if !exists {
		t.Fatal("EV-002c: ReadEventIDHWM: exists=false after write")
	}
	if got != want {
		t.Fatalf("EV-002c: round-trip mismatch: got %v, want %v", got, want)
	}
}

// TestWriteEventIDHWMAtomicNoSync_Overwrite asserts that a second write correctly
// replaces the first value.
func TestWriteEventIDHWMAtomicNoSync_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "event_id_hwm")
	gen := NewEventIDGenerator()

	first, _ := gen.Next()
	_ = WriteEventIDHWMAtomicNoSync(path, first)

	second, _ := gen.Next()
	if writeErr := WriteEventIDHWMAtomicNoSync(path, second); writeErr != nil {
		t.Fatalf("EV-002c: second write: %v", writeErr)
	}

	got, _, readErr := ReadEventIDHWM(path)
	if readErr != nil {
		t.Fatalf("EV-002c: read after overwrite: %v", readErr)
	}
	if got != second {
		t.Fatalf("EV-002c: overwrite: got %v, want %v", got, second)
	}
}

// TestExtractUUIDv7Timestamp asserts that ExtractUUIDv7Timestamp returns a
// time within 2 seconds of now when applied to a freshly-minted UUIDv7.
func TestExtractUUIDv7Timestamp(t *testing.T) {
	before := time.Now()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7(): %v", err)
	}
	after := time.Now()

	id := EventID(u)
	got := ExtractUUIDv7Timestamp(id)

	if got.Before(before.Truncate(time.Millisecond)) {
		t.Errorf("EV-002c: extracted timestamp %v is before generation start %v", got, before)
	}
	if got.After(after.Add(time.Millisecond)) {
		t.Errorf("EV-002c: extracted timestamp %v is after generation end %v", got, after)
	}
}

// TestIsHWMClockRegression_Below asserts that IsHWMClockRegression returns false
// when the wall clock is at or ahead of the HWM timestamp.
func TestIsHWMClockRegression_Below(t *testing.T) {
	now := time.Now()
	hwm := hwmFixtureMakeID(t, now.Add(-5*time.Second)) // HWM 5s in the past
	if IsHWMClockRegression(hwm, now) {
		t.Error("EV-002c: wall clock ahead of HWM should not be regression")
	}
}

// TestIsHWMClockRegression_Above asserts that IsHWMClockRegression returns true
// when the wall clock is more than 1 second behind the HWM timestamp.
func TestIsHWMClockRegression_Above(t *testing.T) {
	now := time.Now()
	hwm := hwmFixtureMakeID(t, now.Add(2*time.Second)) // HWM 2s in the future
	if !IsHWMClockRegression(hwm, now) {
		t.Error("EV-002c: wall clock >1s behind HWM should be regression")
	}
}

// TestIsHWMClockRegression_Edge asserts that IsHWMClockRegression returns false
// when the wall clock is exactly 1 second behind the HWM timestamp.
// The spec says "more than 1 second."
func TestIsHWMClockRegression_Edge(t *testing.T) {
	now := time.Now()
	hwm := hwmFixtureMakeID(t, now.Add(time.Second)) // HWM exactly 1s ahead
	if IsHWMClockRegression(hwm, now) {
		t.Error("EV-002c: wall clock exactly 1s behind HWM should NOT be regression (threshold is >1s)")
	}
}

// TestNewEventIDGeneratorWithHWM asserts that a generator seeded with an HWM
// produces an EventID strictly greater than the HWM even when the injected
// newV7 clock source returns a value less than the HWM (simulating clock
// regression after daemon restart).
//
// This mirrors TestUUIDv7_CrossRestart but exercises the public constructor.
func TestNewEventIDGeneratorWithHWM(t *testing.T) {
	preRestart := NewEventIDGenerator()
	var hwm EventID
	for i := 0; i < 5; i++ {
		id, err := preRestart.Next()
		if err != nil {
			t.Fatalf("pre-restart Next() %d: %v", i, err)
		}
		hwm = id
	}

	// Seed from HWM; inject a regressed clock source.
	g := NewEventIDGeneratorWithHWM(hwm)
	g.newV7 = func() (uuid.UUID, error) {
		// Decrement HWM by 1 to simulate a regressed clock.
		regressed := uuid.UUID(hwm)
		for i := 15; i >= 0; i-- {
			if regressed[i] > 0 {
				regressed[i]--
				break
			}
			regressed[i] = 0xff
		}
		return regressed, nil
	}

	firstPost, err := g.Next()
	if err != nil {
		t.Fatalf("post-restart Next(): %v", err)
	}

	hwmHex := hex.EncodeToString(hwm[:])
	postHex := hex.EncodeToString(firstPost[:])
	// Compare byte-by-byte (big-endian UUIDv7 ordering).
	if hwmHex >= postHex {
		t.Fatalf("EV-002c: post-restart EventID %v is not strictly > HWM %v", firstPost, hwm)
	}
}
