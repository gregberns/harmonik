package lifecycle_test

// beadsowned_hk11xkn_test.go — unit tests for SentinelFileProvenanceChecker.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (provenance OR clause).
// Bead ref: hk-11xkn (audit-log actor=project_hash provenance for orphan-sweep
// bead-reset).

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// TestSentinelFileProvenanceChecker_OwnsMissingDir verifies that when the
// beads-owned/ directory does not exist, Owns returns (false, nil) — no error.
func TestSentinelFileProvenanceChecker_OwnsMissingDir(t *testing.T) {
	t.Parallel()

	checker := lifecycle.NewSentinelFileProvenanceChecker("/nonexistent/path/beads-owned")
	owned, err := checker.Owns(context.Background(), "hk-aaaaa")
	if err != nil {
		t.Fatalf("Owns on missing dir: unexpected error: %v", err)
	}
	if owned {
		t.Error("Owns on missing dir: want false, got true")
	}
}

// TestSentinelFileProvenanceChecker_OwnsMissingFile verifies that when the
// directory exists but the sentinel file for the bead does not, Owns returns
// (false, nil).
func TestSentinelFileProvenanceChecker_OwnsMissingFile(t *testing.T) {
	t.Parallel()

	ownedDir := t.TempDir()
	checker := lifecycle.NewSentinelFileProvenanceChecker(ownedDir)

	owned, err := checker.Owns(context.Background(), "hk-bbbbb")
	if err != nil {
		t.Fatalf("Owns on missing file: unexpected error: %v", err)
	}
	if owned {
		t.Error("Owns on missing file: want false, got true")
	}
}

// TestSentinelFileProvenanceChecker_OwnsExistingFile verifies that when the
// sentinel file for the bead exists, Owns returns (true, nil).
func TestSentinelFileProvenanceChecker_OwnsExistingFile(t *testing.T) {
	t.Parallel()

	ownedDir := t.TempDir()
	const beadID = core.BeadID("hk-ccccc")
	sentinelPath := filepath.Join(ownedDir, string(beadID))
	if err := os.WriteFile(sentinelPath, nil, 0o600); err != nil {
		t.Fatalf("create sentinel: %v", err)
	}

	checker := lifecycle.NewSentinelFileProvenanceChecker(ownedDir)
	owned, err := checker.Owns(context.Background(), beadID)
	if err != nil {
		t.Fatalf("Owns on existing file: unexpected error: %v", err)
	}
	if !owned {
		t.Error("Owns on existing file: want true, got false")
	}
}

// TestSentinelFileProvenanceChecker_OwnsUnrelatedFileNotCounted verifies that
// a sentinel for bead-A does not establish ownership for bead-B.
func TestSentinelFileProvenanceChecker_OwnsUnrelatedFileNotCounted(t *testing.T) {
	t.Parallel()

	ownedDir := t.TempDir()
	const beadA = core.BeadID("hk-aaa-owned")
	const beadB = core.BeadID("hk-bbb-notowned")

	sentinelPath := filepath.Join(ownedDir, string(beadA))
	if err := os.WriteFile(sentinelPath, nil, 0o600); err != nil {
		t.Fatalf("create sentinel for beadA: %v", err)
	}

	checker := lifecycle.NewSentinelFileProvenanceChecker(ownedDir)
	owned, err := checker.Owns(context.Background(), beadB)
	if err != nil {
		t.Fatalf("Owns for beadB: unexpected error: %v", err)
	}
	if owned {
		t.Errorf("Owns for beadB: want false (only beadA has sentinel), got true")
	}
}

// TestSentinelFileProvenanceChecker_BeadsOwnedDirHelper verifies that
// BeadsOwnedDir returns the expected path and that a sentinel written there is
// detected by SentinelFileProvenanceChecker.
func TestSentinelFileProvenanceChecker_BeadsOwnedDirHelper(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	ownedDir := lifecycle.BeadsOwnedDir(projectDir)

	// The directory doesn't exist yet — Owns should return false.
	checker := lifecycle.NewSentinelFileProvenanceChecker(ownedDir)
	const beadID = core.BeadID("hk-ddddd")
	owned, err := checker.Owns(context.Background(), beadID)
	if err != nil {
		t.Fatalf("pre-create Owns: unexpected error: %v", err)
	}
	if owned {
		t.Error("pre-create Owns: want false, got true")
	}

	// Create the directory and sentinel file.
	if err := os.MkdirAll(ownedDir, 0o755); err != nil { //nolint:gosec // test directory
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ownedDir, string(beadID)), nil, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Now Owns should return true.
	owned, err = checker.Owns(context.Background(), beadID)
	if err != nil {
		t.Fatalf("post-create Owns: unexpected error: %v", err)
	}
	if !owned {
		t.Error("post-create Owns: want true, got false")
	}
}
