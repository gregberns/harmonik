package workspace

import "context"

// IntegrationBranchName returns the integration branch name for a given
// parent bead ID, implementing workspace-model.md §4.2 WM-006.
//
// When parentBeadID is empty, the caller has no parent-bead context; this
// function returns the spec-level default integration branch "harmonik/integration"
// per WM-006. The small-scope-collapse override (merging directly to main) is
// expressed via WM-005b's target_branch field in .harmonik/branching.yaml or
// the per-bead ## Branching section; callers that need the full resolution chain
// must consult resolveBranching rather than this function.
//
// When parentBeadID is non-empty, the returned branch is
// "harmonik/integration/<parent_bead_id_refsafe>", where
// <parent_bead_id_refsafe> is produced by [BeadIDToRefSafe]. Any error from
// that call is propagated unchanged (typically [ErrRefNameInvalid] when the
// bead ID cannot be made ref-safe after the canonical fallback per WM-006a).
//
// OQ-WM-002 (substitution template operator-configurable) is OUT OF SCOPE for
// this implementation. The default verbatim bead-ID substitution via
// [BeadIDToRefSafe] is the only supported path. A separate bead tracks the
// operator-configurable template path.
//
// Spec refs:
//   - workspace-model.md §4.2 WM-006 — integration branch naming convention.
//   - workspace-model.md §4.2 WM-006a — ref-safe bead-ID substitution via
//     git check-ref-format(1).
//   - workspace-model.md §4.2 WM-005b — full target_branch resolution chain
//     (per-bead → branching.yaml → spec-level default).
func IntegrationBranchName(ctx context.Context, parentBeadID string) (string, error) {
	const defaultIntegrationBranch = "harmonik/integration"

	if parentBeadID == "" {
		return defaultIntegrationBranch, nil
	}

	safe, err := BeadIDToRefSafe(ctx, parentBeadID)
	if err != nil {
		return "", err
	}

	return defaultIntegrationBranch + "/" + safe, nil
}
