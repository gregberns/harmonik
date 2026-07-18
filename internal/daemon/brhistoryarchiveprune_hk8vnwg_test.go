package daemon

// brhistoryarchiveprune_hk8vnwg_test.go — regression tests for the
// .br_history-archive retention prune (hk-8vnwg). Before this fix the archive
// grew without bound (25 GiB / 15,072 snapshots), the confirmed 256 GB disk-fill
// root cause. pruneBrHistoryArchive caps it by keep-N + max-age, sidecar-aware.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeArchived creates a payload snapshot "<base>.jsonl.archived-<tag>" and its
// "<base>.jsonl.meta.json.archived-<tag>" sidecar with the given mtime.
func writeArchived(t *testing.T, dir, base, tag string, mtime time.Time) (payload, sidecar string) {
	t.Helper()
	payload = filepath.Join(dir, base+".jsonl.archived-"+tag)
	sidecar = filepath.Join(dir, base+".jsonl.meta.json.archived-"+tag)
	if err := os.WriteFile(payload, []byte("snapshot"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := os.WriteFile(sidecar, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	if err := os.Chtimes(payload, mtime, mtime); err != nil {
		t.Fatalf("chtimes payload: %v", err)
	}
	if err := os.Chtimes(sidecar, mtime, mtime); err != nil {
		t.Fatalf("chtimes sidecar: %v", err)
	}
	return payload, sidecar
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// TestHK8vnwg_PruneKeepN keeps only the keepN most-recent payloads (and their
// sidecars); older ones are hard-deleted together.
func TestHK8vnwg_PruneKeepN(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	// 5 snapshots, 1 minute apart (newest = snap4). keepN=2 → snap3,snap4 survive.
	type sp struct{ payload, sidecar string }
	snaps := make([]sp, 5)
	for i := 0; i < 5; i++ {
		mt := now.Add(time.Duration(i) * time.Minute)
		p, s := writeArchived(t, dir, "issues.snap"+string(rune('0'+i)), "T0", mt)
		snaps[i] = sp{p, s}
	}

	pruneBrHistoryArchive(context.Background(), dir, 2, 0, now.Add(time.Hour))

	// Newest two (i=3,4) survive with sidecars; oldest three (0,1,2) gone entirely.
	for i := 0; i < 3; i++ {
		if exists(snaps[i].payload) {
			t.Errorf("snap%d payload should be pruned (beyond keepN=2)", i)
		}
		if exists(snaps[i].sidecar) {
			t.Errorf("snap%d sidecar should be pruned with its payload", i)
		}
	}
	for i := 3; i < 5; i++ {
		if !exists(snaps[i].payload) {
			t.Errorf("snap%d payload should survive (within keepN=2)", i)
		}
		if !exists(snaps[i].sidecar) {
			t.Errorf("snap%d sidecar should survive with its payload", i)
		}
	}
}

// TestHK8vnwg_PruneMaxAge drops payloads older than maxAge regardless of count.
func TestHK8vnwg_PruneMaxAge(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	oldP, oldS := writeArchived(t, dir, "issues.old", "T0", now.Add(-10*24*time.Hour)) // 10 days
	newP, newS := writeArchived(t, dir, "issues.new", "T0", now.Add(-1*time.Hour))      // 1 hour

	// keepN=0 (disabled) so ONLY the age arm acts. maxAge = 7 days.
	pruneBrHistoryArchive(context.Background(), dir, 0, 7*24*time.Hour, now)

	if exists(oldP) || exists(oldS) {
		t.Error("10-day-old snapshot (payload+sidecar) should be pruned by maxAge=7d")
	}
	if !exists(newP) || !exists(newS) {
		t.Error("1-hour-old snapshot should survive maxAge=7d")
	}
}

// TestHK8vnwg_OrphanSidecarSwept removes a sidecar with no payload once older
// than maxAge, so meta files never leak.
func TestHK8vnwg_OrphanSidecarSwept(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

	// Orphan sidecar (no payload), 10 days old.
	orphan := filepath.Join(dir, "issues.orphan.jsonl.meta.json.archived-T0")
	if err := os.WriteFile(orphan, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	// A young orphan sidecar that must survive.
	youngOrphan := filepath.Join(dir, "issues.young.jsonl.meta.json.archived-T0")
	if err := os.WriteFile(youngOrphan, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	yt := now.Add(-1 * time.Hour)
	if err := os.Chtimes(youngOrphan, yt, yt); err != nil {
		t.Fatal(err)
	}

	pruneBrHistoryArchive(context.Background(), dir, 0, 7*24*time.Hour, now)

	if exists(orphan) {
		t.Error("10-day-old orphan sidecar should be swept by maxAge")
	}
	if !exists(youngOrphan) {
		t.Error("1-hour-old orphan sidecar should survive")
	}
}

// TestHK8vnwg_LeavesUnrecognisedAndMissingDir is non-fatal on an absent dir and
// never touches files that don't match the archived-snapshot shape.
func TestHK8vnwg_LeavesUnrecognisedAndMissingDir(t *testing.T) {
	// Absent dir: must not panic or error.
	pruneBrHistoryArchive(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), 1, time.Hour, time.Now())

	dir := t.TempDir()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	// An unrelated file (not an archived snapshot), old — must be left alone.
	stray := filepath.Join(dir, "README.txt")
	if err := os.WriteFile(stray, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-30 * 24 * time.Hour)
	_ = os.Chtimes(stray, old, old)

	pruneBrHistoryArchive(context.Background(), dir, 0, 7*24*time.Hour, now)

	if !exists(stray) {
		t.Error("unrecognised non-archive file must never be pruned")
	}
}
