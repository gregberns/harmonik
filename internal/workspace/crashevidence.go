package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// OrphanEvidenceType is the string label for a crash-evidence state detected
// by startup discovery (WM-003a). The defined types are:
//
//   - [EvidenceBareWorktreeNoLease] — registered worktree, no lease-lock, no sessions dir.
//   - [EvidenceSidecarWithoutLease] — registered worktree, sidecar present, no lease-lock.
//
// Both arise from SIGKILL / power loss between `git worktree add` and the
// lease-lock fsync gate of WM-016. Cat 3 reconciliation routing is required.
//
// Spec ref: workspace-model.md §4.1 WM-003a, §3 Glossary "orphan evidence types".
type OrphanEvidenceType string

const (
	// EvidenceBareWorktreeNoLease is the evidence type for a registered worktree
	// with no lease-lock file and no session-log directory.
	//
	// Reconciliation Cat 3 routes this to `reopen-bead` (discard partial worktree,
	// fresh run_id per WM-034) or `accept-close-with-note` with operator cleanup.
	EvidenceBareWorktreeNoLease OrphanEvidenceType = "bare-worktree-no-lease"

	// EvidenceSidecarWithoutLease is the evidence type for a registered worktree
	// where at least one session sidecar (`harmonik.meta.json`) is present but the
	// lease-lock file is absent.
	//
	// Reconciliation Cat 3 routes this to `reopen-bead` or `accept-close-with-note`.
	EvidenceSidecarWithoutLease OrphanEvidenceType = "sidecar-without-lease"
)

// ClassifyCrashEvidence classifies a partially-crashed worktree at
// worktreePath into one of the two WM-003a orphan evidence types.
//
// The classification rules per WM-003a:
//
//   - If the lease-lock file (`${workspace_path}/.harmonik/lease.lock`) is
//     absent AND no session sidecar (`harmonik.meta.json`) exists under
//     `${workspace_path}/.harmonik/sessions/`, the evidence type is
//     [EvidenceBareWorktreeNoLease].
//
//   - If the lease-lock file is absent AND at least one session sidecar
//     exists, the evidence type is [EvidenceSidecarWithoutLease].
//
// Both evidence types arise from a SIGKILL / power loss between `git worktree
// add` completion and the lease-lock fsync gate of WM-016; neither the
// `leased` nor any post-`ready` event has been durably emitted.
//
// ClassifyCrashEvidence does NOT verify that the worktree is registered in git
// (`git worktree list --porcelain`) — that check is the caller's responsibility
// via [DiscoverWorktrees] (WM-013c step b) before invoking the classifier.
//
// Returns [ErrBareWorktreeNoLease] (as a typed evidence) when the evidence type
// is [EvidenceBareWorktreeNoLease], and [ErrSidecarWithoutLease] when the
// evidence type is [EvidenceSidecarWithoutLease]. These sentinel errors allow
// callers to use errors.Is/errors.As for routing decisions; the evidence type
// string is also returned directly for logging.
//
// Returns an error when:
//   - The lease-lock file EXISTS at worktreePath — this is NOT a crash-evidence
//     state; callers should not invoke ClassifyCrashEvidence on live workspaces.
//   - I/O errors (other than non-existence) are encountered during classification.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-003a — orphan evidence types and routing rules.
//   - workspace-model.md §3 Glossary — "orphan evidence types" definition.
//   - workspace-model.md §8 — error taxonomy: BareWorktreeNoLease, SidecarWithoutLease.
func ClassifyCrashEvidence(worktreePath string) (OrphanEvidenceType, error) {
	leaseLockPath := LeaseLockPath(worktreePath)
	_, err := os.Stat(leaseLockPath)
	switch {
	case err == nil:
		// Lease-lock is present — not a crash-evidence state.
		return "", fmt.Errorf("workspace: ClassifyCrashEvidence: lease-lock present at %q; not an orphan evidence state", leaseLockPath)
	case !os.IsNotExist(err):
		return "", fmt.Errorf("workspace: ClassifyCrashEvidence: Stat lease-lock %q: %w", leaseLockPath, err)
	}

	// Lease-lock is absent. Check for session sidecars under the sessions root.
	sidecarFound, err := hasSidecar(worktreePath)
	if err != nil {
		return "", fmt.Errorf("workspace: ClassifyCrashEvidence: sidecar check: %w", err)
	}

	if sidecarFound {
		return EvidenceSidecarWithoutLease, fmt.Errorf("%w: %q", ErrSidecarWithoutLease, worktreePath)
	}
	return EvidenceBareWorktreeNoLease, fmt.Errorf("%w: %q", ErrBareWorktreeNoLease, worktreePath)
}

// hasSidecar reports whether at least one harmonik.meta.json sidecar exists
// under ${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json.
//
// Returns (false, nil) when the sessions root does not exist.
func hasSidecar(worktreePath string) (bool, error) {
	sessionsRoot := SessionLogRootPath(worktreePath)
	entries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("ReadDir %q: %w", sessionsRoot, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sidecarPath := filepath.Join(sessionsRoot, entry.Name(), "harmonik.meta.json")
		if _, err := os.Stat(sidecarPath); err == nil {
			return true, nil
		}
	}
	return false, nil
}
