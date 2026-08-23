package lifecycle

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
		return false, nil
	}
	return false, fmt.Errorf("lifecycle: SentinelFileProvenanceChecker.Owns %q: %w", sentinelPath, err)
}
