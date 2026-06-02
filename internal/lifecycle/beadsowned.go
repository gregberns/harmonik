package lifecycle

// beadsowned.go — SentinelFileProvenanceChecker, a ProvenanceChecker backed
// by the .harmonik/beads-owned/ sentinel directory.
//
// A file at .harmonik/beads-owned/<bead-id> is written by brcli.Adapter.ClaimBead
// on successful claim and deleted on successful CloseBead, ReopenBead, or
// ResetBead. The sentinel outlives the BI-030 claim intent file (which is
// deleted in step 6 after claim success) and provides an independent provenance
// signal for the PL-006 sixth-bullet orphan sweep. When all intent files have
// been cleared by prior crash-recovery runs, the sentinel file is the only
// remaining evidence of ownership.
//
// SentinelFileProvenanceChecker.Owns is a pure filesystem stat — no subprocess
// invocation, no network call, no SQLite access. It reports true iff the
// sentinel file exists for the given bead ID.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (provenance OR clause);
// §4.4 PL-006a (project_hash discipline).
// Bead ref: hk-11xkn.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

// SentinelFileProvenanceChecker implements ProvenanceChecker using the
// .harmonik/beads-owned/ sentinel directory. Its Owns method returns true iff
// the file .harmonik/beads-owned/<bead-id> exists under the configured
// ownedDir, indicating that this project's daemon has previously claimed the
// bead via brcli.Adapter.ClaimBead.
//
// Construct with NewSentinelFileProvenanceChecker. ownedDir MUST be the
// absolute path returned by lifecycle.BeadsOwnedDir(projectDir).
type SentinelFileProvenanceChecker struct {
	ownedDir string
}

// NewSentinelFileProvenanceChecker returns a SentinelFileProvenanceChecker
// whose Owns method probes ownedDir/<bead-id> for existence.
//
// ownedDir MUST be the absolute path of the beads-owned/ directory
// (lifecycle.BeadsOwnedDir(projectDir)). It MAY not exist yet on disk —
// a missing directory means no sentinels exist, so Owns returns false for all
// beads.
func NewSentinelFileProvenanceChecker(ownedDir string) *SentinelFileProvenanceChecker {
	return &SentinelFileProvenanceChecker{ownedDir: ownedDir}
}

// Owns reports true iff the sentinel file .harmonik/beads-owned/<beadID>
// exists. A missing directory or missing file both return (false, nil). Only
// unexpected I/O errors (other than os.ErrNotExist / os.ErrNotDir) are
// returned as errors.
//
// Implements ProvenanceChecker.
func (c *SentinelFileProvenanceChecker) Owns(_ context.Context, beadID core.BeadID) (bool, error) {
	sentinelPath := filepath.Join(c.ownedDir, string(beadID))
	_, err := os.Stat(sentinelPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		// Directory or file absent — not owned.
		return false, nil
	}
	return false, fmt.Errorf("lifecycle: SentinelFileProvenanceChecker.Owns %q: %w", sentinelPath, err)
}
