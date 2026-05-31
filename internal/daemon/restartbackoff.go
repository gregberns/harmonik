package daemon

// restartbackoff.go — persistent boot-record backoff for the daemon startup path.
//
// On 2026-05-30 the daemon was started 10 times in a day (last five within ~14
// minutes), each boot auto-pulling `br ready` and dispatching immediately. This
// file adds a per-project persistent boot record so that rapid successive starts
// incur an exponentially-growing startup delay, capping the crash-and-re-pull
// multiplier.
//
// Record file: <projectDir>/.harmonik/cognition/restart-record.json
//
// Algorithm (applyBootBackoff):
//
//	n = number of boot times in the record that fall within restartBackoffWindow
//	delay = base × 2^(n−1), capped at restartBackoffCap (0 when n == 0)
//	record current boot, write state, sleep delay (ctx-interruptible)
//
// All I/O errors are non-fatal: the function logs and skips the delay rather
// than refusing to start. The cognition/ directory is created on demand.
//
// Spec ref: docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md §"Recommended beads".
// Bead ref: hk-7t9g1.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// restartBackoffBase is the initial startup delay applied on the second
// rapid daemon boot (n == 1 prior boot in window).
const restartBackoffBase = 30 * time.Second

// restartBackoffCap is the maximum startup delay imposed by boot-record backoff.
const restartBackoffCap = 10 * time.Minute

// restartBackoffWindow is the sliding window within which boot times are
// counted. Boots older than this are pruned from the record and not considered.
const restartBackoffWindow = 1 * time.Hour

// restartRecordPath returns the absolute path of the persistent boot-record
// file for the given project directory.
func restartRecordPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "cognition", "restart-record.json")
}

// restartRecord is the on-disk schema for the persistent boot-record file.
// N-1 readers MUST tolerate unknown fields (json.Unmarshal ignores extras).
type restartRecord struct {
	SchemaVersion int     `json:"schema_version"`
	BootTimesUnix []int64 `json:"boot_times_unix_sec"`
}

// applyBootBackoff records the current daemon boot in the persistent
// boot-record file and sleeps for the computed exponential backoff delay before
// returning. The delay is 0 on the first rapid boot within the window; it
// grows as base × 2^(n−1), capped at restartBackoffCap, where n is the number
// of prior boots recorded within restartBackoffWindow.
//
// The sleep is cancelled early if ctx expires.
//
// All read/write errors are non-fatal: the function prints a warning to stderr
// and returns without sleeping so the daemon continues to start.
//
// The cognition/ directory under projectDir is created on demand.
//
// Bead ref: hk-7t9g1.
func applyBootBackoff(ctx context.Context, projectDir string) time.Duration {
	if projectDir == "" {
		return 0
	}

	path := restartRecordPath(projectDir)
	now := time.Now()

	// Read existing record. A missing file is fine (first boot).
	rec, readErr := readRestartRecord(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		fmt.Fprintf(os.Stderr, "daemon: restart-backoff: read %q: %v (skipping backoff)\n", path, readErr)
		// Record the current boot even on read failure, best-effort.
		_ = writeRestartRecord(path, restartRecord{
			SchemaVersion: 1,
			BootTimesUnix: []int64{now.Unix()},
		})
		return 0
	}

	// Prune boot times outside the sliding window.
	windowStart := now.Add(-restartBackoffWindow)
	recent := make([]int64, 0, len(rec.BootTimesUnix)+1)
	for _, t := range rec.BootTimesUnix {
		if time.Unix(t, 0).After(windowStart) {
			recent = append(recent, t)
		}
	}

	// Compute delay from the count of prior boots in the window.
	n := len(recent) // boots before this one
	delay := computeRestartBackoffDelay(n, restartBackoffBase, restartBackoffCap)

	// Append this boot and persist.
	rec.SchemaVersion = 1
	rec.BootTimesUnix = append(recent, now.Unix())
	if writeErr := writeRestartRecord(path, rec); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: restart-backoff: write %q: %v\n", path, writeErr)
		// Non-fatal: proceed even if we can't persist.
	}

	if delay > 0 {
		fmt.Fprintf(os.Stderr,
			"daemon: restart-backoff: %d rapid boot(s) in the last hour — delaying startup by %s\n",
			n, delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
	}

	return delay
}

// computeRestartBackoffDelay returns base × 2^(n−1), capped at cap.
// Returns 0 when n <= 0 (no prior rapid boots — first boot or window has cleared).
func computeRestartBackoffDelay(n int, base, cap time.Duration) time.Duration {
	if n <= 0 {
		return 0
	}
	// Guard against int overflow in the exponent: beyond n=30 the cap is always
	// reached regardless, so clamp n before the float computation.
	if n > 30 {
		n = 30
	}
	delay := time.Duration(float64(base) * math.Pow(2, float64(n-1)))
	if delay > cap || delay < 0 {
		return cap
	}
	return delay
}

// readRestartRecord reads and parses the boot-record file at path.
// Returns (zero, *os.PathError) when path does not exist.
func readRestartRecord(path string) (restartRecord, error) {
	//nolint:gosec // G304: path derived from projectDir (operator-controlled daemon arg)
	data, err := os.ReadFile(path)
	if err != nil {
		return restartRecord{}, err
	}
	var rec restartRecord
	if unmarshalErr := json.Unmarshal(data, &rec); unmarshalErr != nil {
		return restartRecord{}, fmt.Errorf("restartRecord unmarshal: %w", unmarshalErr)
	}
	return rec, nil
}

// writeRestartRecord writes rec atomically (temp+rename+fsync) to path.
// The directory is created on demand.
func writeRestartRecord(path string, rec restartRecord) error {
	dir := filepath.Dir(path)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return fmt.Errorf("mkdir %s: %w", dir, mkErr)
	}
	data, marshalErr := json.MarshalIndent(rec, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("marshal: %w", marshalErr)
	}
	data = append(data, '\n')

	tmp, tmpErr := os.CreateTemp(dir, "restart-record-*.json.tmp")
	if tmpErr != nil {
		return fmt.Errorf("create temp: %w", tmpErr)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		return writeErr
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		return syncErr
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return closeErr
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		return renameErr
	}
	ok = true
	return nil
}
