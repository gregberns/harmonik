package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadEventIDHWM reads the UUIDv7 high-water-mark from the file at path.
//
// Returns (hwm, true, nil) when the file exists and contains a valid 32-char
// lowercase hex UUID. Returns (zero, false, nil) when the file does not exist
// (first-run or .harmonik/ wiped). Returns (zero, false, err) when the file
// exists but is unreadable or its contents are corrupt.
//
// Spec ref: event-model.md §4.1 EV-002c.
func ReadEventIDHWM(path string) (EventID, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return EventID{}, false, nil
		}
		return EventID{}, false, fmt.Errorf("read event_id HWM %s: %w", path, err)
	}
	s := strings.TrimSpace(string(data))
	if len(s) != 32 {
		return EventID{}, false, fmt.Errorf("event_id HWM %s: malformed content (length %d, want 32)", path, len(s))
	}
	b, decErr := hex.DecodeString(s)
	if decErr != nil {
		return EventID{}, false, fmt.Errorf("event_id HWM %s: hex decode: %w", path, decErr)
	}
	var id EventID
	copy(id[:], b)
	return id, true, nil
}

// WriteEventIDHWMAtomicNoSync atomically overwrites the HWM file at path with
// the 32-char lowercase hex representation of hwm. Atomicity is achieved via
// a temp-file write + os.Rename (POSIX atomic on the same filesystem).
//
// No fsync is issued on the HWM file itself: the HWM write piggybacks on the
// JSONL fsync domain per EV-002c so no additional fsync cost is incurred.
// On crash between an F-class JSONL fsync and the next HWM write, the daemon
// MUST log a structured warning on next startup (missing/stale HWM) and seed
// from the wall clock; cross-restart ordering is not guaranteed in that case.
//
// Spec ref: event-model.md §4.1 EV-002c.
//
//nolint:gosec // G304: path is lifecycle.EventIDHWMPath, not user input.
func WriteEventIDHWMAtomicNoSync(path string, hwm EventID) error {
	dir := filepath.Dir(path)
	tmp, tmpErr := os.CreateTemp(dir, "event_id_hwm.*.tmp")
	if tmpErr != nil {
		return fmt.Errorf("event_id HWM write: create temp: %w", tmpErr)
	}
	tmpPath := tmp.Name()

	content := hex.EncodeToString(hwm[:])
	if _, writeErr := tmp.WriteString(content); writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("event_id HWM write: write temp: %w", writeErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("event_id HWM write: close temp: %w", closeErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("event_id HWM write: rename to %s: %w", path, renameErr)
	}
	return nil
}

// hwmClockRegressionThreshold is the minimum lag between the HWM embedded
// timestamp and the wall clock before IsHWMClockRegression returns true,
// per EV-002c ("more than 1 second").
const hwmClockRegressionThreshold = time.Second

// ExtractUUIDv7Timestamp extracts the 48-bit millisecond-precision wall-clock
// timestamp embedded in id per RFC 9562 §5.7.
//
// The high 48 bits of a UUIDv7 (bytes 0–5, big-endian) encode the number of
// milliseconds since the Unix epoch.
func ExtractUUIDv7Timestamp(id EventID) time.Time {
	ms := uint64(id[0])<<40 | uint64(id[1])<<32 | uint64(id[2])<<24 |
		uint64(id[3])<<16 | uint64(id[4])<<8 | uint64(id[5])
	return time.Unix(0, int64(ms)*int64(time.Millisecond)).UTC()
}

// IsHWMClockRegression reports whether wallClock is more than
// hwmClockRegressionThreshold (1 second) behind the timestamp embedded in hwm.
//
// Returns true iff: ExtractUUIDv7Timestamp(hwm) − wallClock > 1s
//
// A true result means the daemon MUST emit daemon_degraded{reason=clock_regression}
// and synthesize UUIDv7 timestamps ahead of the wall clock until it catches up.
//
// Spec ref: event-model.md §4.1 EV-002c.
func IsHWMClockRegression(hwm EventID, wallClock time.Time) bool {
	hwmTime := ExtractUUIDv7Timestamp(hwm)
	return hwmTime.Sub(wallClock) > hwmClockRegressionThreshold
}
