package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestComputeRestartBackoffDelay verifies the exponential backoff formula
// and cap behaviour.
func TestComputeRestartBackoffDelay(t *testing.T) {
	base := 30 * time.Second
	cap := 10 * time.Minute

	tests := []struct {
		n    int
		want time.Duration
	}{
		{0, 0},                // first boot — no delay
		{-1, 0},               // guard: negative n treated as 0
		{1, 30 * time.Second}, // base × 2^0
		{2, 60 * time.Second}, // base × 2^1
		{3, 2 * time.Minute},  // base × 2^2
		{4, 4 * time.Minute},  // base × 2^3
		{5, 8 * time.Minute},  // base × 2^4
		{6, cap},              // base × 2^5 = 960s > cap → capped
		{100, cap},            // large n → capped
	}

	for _, tt := range tests {
		got := computeRestartBackoffDelay(tt.n, base, cap)
		if got != tt.want {
			t.Errorf("computeRestartBackoffDelay(%d) = %s, want %s", tt.n, got, tt.want)
		}
	}
}

// TestApplyBootBackoff_FirstBoot verifies that the first boot within the window
// incurs no delay and writes the boot record.
func TestApplyBootBackoff_FirstBoot(t *testing.T) {
	dir := t.TempDir()

	delay := applyBootBackoff(context.Background(), dir, DaemonRestartBackoffConfig{})
	if delay != 0 {
		t.Errorf("first boot: want 0 delay, got %s", delay)
	}

	// Record file must exist after the first boot.
	if _, err := os.Stat(restartRecordPath(dir)); err != nil {
		t.Fatalf("boot record not written: %v", err)
	}

	// Read back and verify exactly one entry.
	rec, err := readRestartRecord(restartRecordPath(dir))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(rec.BootTimesUnix) != 1 {
		t.Errorf("want 1 boot time, got %d", len(rec.BootTimesUnix))
	}
}

// TestApplyBootBackoff_SecondBoot verifies that one prior boot in the window
// produces a delay equal to defaultRestartBackoffBase.
func TestApplyBootBackoff_SecondBoot(t *testing.T) {
	dir := t.TempDir()

	// Seed the record with one recent boot (30 seconds ago).
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: []int64{time.Now().Add(-30 * time.Second).Unix()},
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	// Cancel the context immediately so the sleep is skipped (we only care about
	// the returned delay value, not the actual wait).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delay := applyBootBackoff(ctx, dir, DaemonRestartBackoffConfig{})
	if delay != defaultRestartBackoffBase {
		t.Errorf("second boot: want %s delay, got %s", defaultRestartBackoffBase, delay)
	}
}

// TestApplyBootBackoff_ThirdBoot verifies that two prior boots produce 2×base.
func TestApplyBootBackoff_ThirdBoot(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: []int64{
			now.Add(-20 * time.Minute).Unix(),
			now.Add(-10 * time.Minute).Unix(),
		},
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delay := applyBootBackoff(ctx, dir, DaemonRestartBackoffConfig{})
	want := 2 * defaultRestartBackoffBase
	if delay != want {
		t.Errorf("third boot: want %s delay, got %s", want, delay)
	}
}

// TestApplyBootBackoff_OldBootsIgnored verifies that boot times outside
// defaultRestartBackoffWindow are pruned and do not contribute to the delay.
func TestApplyBootBackoff_OldBootsIgnored(t *testing.T) {
	dir := t.TempDir()

	// Only a boot from 2 hours ago — outside the 1-hour window.
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: []int64{time.Now().Add(-2 * time.Hour).Unix()},
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	delay := applyBootBackoff(context.Background(), dir, DaemonRestartBackoffConfig{})
	if delay != 0 {
		t.Errorf("stale boot: want 0 delay, got %s", delay)
	}
}

// TestApplyBootBackoff_EmptyProjectDir verifies the nil-safe path: an empty
// project directory returns 0 immediately without touching the filesystem.
func TestApplyBootBackoff_EmptyProjectDir(t *testing.T) {
	delay := applyBootBackoff(context.Background(), "", DaemonRestartBackoffConfig{})
	if delay != 0 {
		t.Errorf("empty dir: want 0, got %s", delay)
	}
}

// TestApplyBootBackoff_CorruptRecord verifies that a corrupt record file is
// tolerated: the function returns 0 (no backoff) rather than refusing to start.
func TestApplyBootBackoff_CorruptRecord(t *testing.T) {
	dir := t.TempDir()
	path := restartRecordPath(dir)
	//nolint:gosec // G301
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	delay := applyBootBackoff(context.Background(), dir, DaemonRestartBackoffConfig{})
	if delay != 0 {
		t.Errorf("corrupt record: want 0 delay, got %s", delay)
	}
}

// TestApplyBootBackoff_RecordGrowsThenPrunes verifies that old entries are
// pruned from the record across multiple calls and do not accumulate unboundedly.
func TestApplyBootBackoff_RecordGrowsThenPrunes(t *testing.T) {
	dir := t.TempDir()

	// Seed with 5 entries from just inside the window and 3 from outside.
	now := time.Now()
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: []int64{
			now.Add(-90 * time.Minute).Unix(), // outside window
			now.Add(-80 * time.Minute).Unix(), // outside window
			now.Add(-70 * time.Minute).Unix(), // outside window
			now.Add(-50 * time.Minute).Unix(), // inside window
			now.Add(-40 * time.Minute).Unix(), // inside window
			now.Add(-30 * time.Minute).Unix(), // inside window
			now.Add(-20 * time.Minute).Unix(), // inside window
			now.Add(-10 * time.Minute).Unix(), // inside window
		},
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	applyBootBackoff(ctx, dir, DaemonRestartBackoffConfig{})

	// After the call the record should contain only entries within the window
	// plus the current boot (5 recent + 1 new = 6).
	rec, err := readRestartRecord(restartRecordPath(dir))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(rec.BootTimesUnix) != 6 {
		t.Errorf("want 6 entries after prune+append, got %d", len(rec.BootTimesUnix))
	}
}

// TestApplyBootBackoff_CapEnforced verifies that six or more rapid boots result
// in a delay equal to defaultRestartBackoffCap rather than an unbounded value.
func TestApplyBootBackoff_CapEnforced(t *testing.T) {
	dir := t.TempDir()

	now := time.Now()
	times := make([]int64, 6)
	for i := range times {
		times[i] = now.Add(-time.Duration(i+1) * 5 * time.Minute).Unix()
	}
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: times,
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delay := applyBootBackoff(ctx, dir, DaemonRestartBackoffConfig{})
	if delay != defaultRestartBackoffCap {
		t.Errorf("cap: want %s, got %s", defaultRestartBackoffCap, delay)
	}
}

func TestResolveRestartBackoffConfig_DefaultsAndPartialOverride(t *testing.T) {
	got := resolveRestartBackoffConfig(DaemonRestartBackoffConfig{Base: 5 * time.Second})

	if got.Base != 5*time.Second {
		t.Errorf("Base = %s, want 5s", got.Base)
	}
	if got.Cap != defaultRestartBackoffCap {
		t.Errorf("Cap = %s, want %s", got.Cap, defaultRestartBackoffCap)
	}
	if got.Window != defaultRestartBackoffWindow {
		t.Errorf("Window = %s, want %s", got.Window, defaultRestartBackoffWindow)
	}
}

func TestApplyBootBackoff_CustomConfig(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	priorRec := restartRecord{
		SchemaVersion: 1,
		BootTimesUnix: []int64{
			now.Add(-2 * time.Minute).Unix(),
			now.Add(-90 * time.Second).Unix(),
			now.Add(-30 * time.Second).Unix(),
		},
	}
	if err := writeRestartRecord(restartRecordPath(dir), priorRec); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	delay := applyBootBackoff(ctx, dir, DaemonRestartBackoffConfig{
		Base:   2 * time.Second,
		Cap:    5 * time.Second,
		Window: 3 * time.Minute,
	})
	if delay != 5*time.Second {
		t.Errorf("custom config: want capped 5s delay, got %s", delay)
	}
}
