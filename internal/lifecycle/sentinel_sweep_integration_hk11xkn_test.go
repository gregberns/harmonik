package lifecycle

// sentinel_sweep_integration_hk11xkn_test.go — integration test confirming that
// SentinelFileProvenanceChecker wired into SweepStaleInProgressBeads makes the
// PL-006 sixth-bullet reset path reachable when all claim intent files have been
// cleared by BI-031 recovery.
//
// Context: hk-11xkn. In the MVH-only code path (nil BeadProvenance), a bead
// without a claim intent file fails provenance and is never reset; a bead WITH a
// claim intent is excluded by exclusion (a). The sentinel file bridges this gap:
// ClaimBead writes .harmonik/beads-owned/<bead-id> on success; the file outlives
// BI-030 intent-log cleanup; SentinelFileProvenanceChecker.Owns returns true when
// it exists, enabling the reset path.
//
// This test exercises that full chain: sentinel present + empty intent log → reset
// fires. The unit tests in beadsowned_hk11xkn_test.go cover Owns in isolation; the
// unit tests in orphansweepbeads_test.go cover the sweep with a fake checker. This
// test is the integration that connects the two via the real production type.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (provenance OR clause);
// §4.4 PL-006a (project_hash discipline).
// Bead ref: hk-11xkn.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestSentinelFileProvenanceChecker_EnablesResetWhenIntentLogEmpty verifies that
// when:
//   - the intent log directory is empty (BI-031 recovery cleared claim intent), AND
//   - a sentinel file exists under .harmonik/beads-owned/<bead-id> (written by
//     ClaimBead on the prior run), AND
//   - SentinelFileProvenanceChecker is wired as cfg.Provenance,
//
// SweepStaleInProgressBeads resets the stale in_progress bead. This is the
// production scenario that hk-11xkn targets: the reset path was previously
// unreachable because provenance relied solely on claim intent presence, which
// BI-031 recovery removes on successful claim.
func TestSentinelFileProvenanceChecker_EnablesResetWhenIntentLogEmpty(t *testing.T) {
	t.Parallel()

	// Build a project root with an empty beads-owned directory.
	projectDir := t.TempDir()
	ownedDir := BeadsOwnedDir(projectDir)
	if err := os.MkdirAll(ownedDir, 0o755); err != nil { //nolint:gosec // test dir
		t.Fatalf("MkdirAll beads-owned: %v", err)
	}

	const beadID = core.BeadID("hk-11xkn-sentinel-integ")

	// Write the sentinel file as ClaimBead would on successful claim.
	sentinelPath := filepath.Join(ownedDir, string(beadID))
	if err := os.WriteFile(sentinelPath, nil, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// intent log is empty (BI-031 recovery already cleared the claim intent).
	cfg := imrestSweepBaseConfig(t)
	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(beadID))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	// Wire the real SentinelFileProvenanceChecker — this is the production path.
	cfg.Provenance = NewSentinelFileProvenanceChecker(ownedDir)
	// No MergeScanner, no exclusion (c).

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 1 {
		t.Errorf("sentinel provenance + empty intent log: ResetCount = %d, want 1 (reset path unreachable without hk-11xkn)", result.ResetCount)
	}
	if len(resetter.called) != 1 || resetter.called[0] != beadID {
		t.Errorf("ResetBead called = %v, want [%s]", resetter.called, beadID)
	}
}

// TestSentinelFileProvenanceChecker_NoResetWhenSentinelAbsent verifies that when
// neither the intent log nor the beads-owned sentinel establishes ownership, the
// bead is not reset. Paired with the above to confirm the sentinel is the sole
// enabling signal.
func TestSentinelFileProvenanceChecker_NoResetWhenSentinelAbsent(t *testing.T) {
	t.Parallel()

	// beads-owned dir exists but contains no sentinel for this bead.
	ownedDir := t.TempDir()

	cfg := imrestSweepBaseConfig(t)
	const beadID = core.BeadID("hk-11xkn-absent-sentinel")
	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(beadID))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	cfg.Provenance = NewSentinelFileProvenanceChecker(ownedDir)

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("absent sentinel + empty intent log: ResetCount = %d, want 0 (bead not owned)", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("ResetBead called on non-owned bead: %v", resetter.called)
	}
}
