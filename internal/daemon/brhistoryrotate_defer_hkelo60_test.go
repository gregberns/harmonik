package daemon

// brhistoryrotate_defer_hkelo60_test.go — discriminating regression guard for
// the archive-prune defer WIRING in runBrHistoryRotationPreflight
// (brhistoryrotate.go:108), bead hk-elo60.
//
// hk-8vnwg made pruneBrHistoryArchive fire on EVERY preflight invocation via a
// `defer pruneBrHistoryArchive(...)` placed BEFORE the "dir absent" / "within
// limit" early returns — so the archive is capped even when .br_history itself
// needs no rotation this call (the real 256 GB fill mode: every bead close runs
// the preflight, .br_history is trimmed to ≤20 so it early-returns, but the
// archive keeps growing). Its four existing tests all call pruneBrHistoryArchive
// DIRECTLY, so deleting that one defer line reintroduces the unbounded-growth
// regression with every test still green — a durability gap in the guard.
//
// This test closes the gap: it drives the PREFLIGHT (not the prune directly) on
// a WITHIN-LIMIT .br_history (which takes the early return) while the archive
// holds an over-age snapshot, and asserts the archive was pruned anyway. The
// only code path that prunes here is the deferred call — remove
// brhistoryrotate.go:108 and this test FAILS (the old snapshot survives).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHKelo60_PreflightDeferPrunesArchiveOnEarlyReturn proves the archive-prune
// defer fires even when .br_history is within-limit (the preflight early-returns
// without rotating). Discriminating: deleting the defer at brhistoryrotate.go:108
// makes this fail.
func TestHKelo60_PreflightDeferPrunesArchiveOnEarlyReturn(t *testing.T) {
	projectDir := brHistMakeHistoryDir(t)

	// .br_history holds 2 entries — well within brHistoryRotationDefaultKeep (20),
	// so the preflight takes the "within_limit" early return and does NOT rotate.
	// This is the exact steady-state mode where only the deferred prune can bound
	// the archive.
	brHistPopulate(t, projectDir, 2)

	archiveDir := filepath.Join(projectDir, ".beads", ".br_history-archive")
	//nolint:gosec // G301: 0755 matches the .beads dir conventions in this package's tests
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive dir: %v", err)
	}

	// One over-age snapshot (10 days old vs the 7-day brHistoryArchiveMaxAge the
	// preflight passes to the defer) that MUST be pruned, and one young snapshot
	// that MUST survive. The defer prunes against time.Now(); a 10-day age clears
	// the 7-day cap by a 3-day margin regardless of test timing.
	now := time.Now()
	oldP, oldS := writeArchived(t, archiveDir, "issues.old", "T0", now.Add(-10*24*time.Hour))
	newP, newS := writeArchived(t, archiveDir, "issues.new", "T0", now.Add(-1*time.Hour))

	// Drive the PREFLIGHT (not pruneBrHistoryArchive directly). With .br_history
	// within-limit, the sole thing that touches the archive is the line-108 defer.
	if err := runBrHistoryRotationPreflight(context.Background(), projectDir, brHistoryRotationDefaultKeep); err != nil {
		t.Fatalf("preflight returned error: %v", err)
	}

	// The over-age snapshot (and its sidecar) must be gone — proof the defer ran
	// on the early-return path. If brhistoryrotate.go:108 is deleted, these
	// survive and the test fails, catching the unbounded-growth regression.
	if exists(oldP) || exists(oldS) {
		t.Errorf("over-age archived snapshot survived the preflight — the archive-prune " +
			"defer (brhistoryrotate.go:108) did not fire on the within-limit early return; " +
			"this reintroduces the hk-8vnwg unbounded-growth regression")
	}
	// The young snapshot must survive (prune is bounded, not a blanket wipe).
	if !exists(newP) || !exists(newS) {
		t.Errorf("young archived snapshot was incorrectly pruned by the preflight defer")
	}
}
