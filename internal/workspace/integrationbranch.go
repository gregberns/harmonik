package workspace

import "context"

// IntegrationBranchName returns the integration branch name for a given
// parent bead ID, implementing workspace-model.md §4.2 WM-006.
//
// When parentBeadID is empty, the caller has no parent-bead context; this
// function returns the default integration branch "harmonik/integration" per
// WM-006 and WM-008 (absent a parent-bead relationship, the merge target
// defaults to "harmonik/integration" per the operator-policy default of
// "integration").
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
//   - workspace-model.md §4.2 WM-008 — merge-target default when no parent-bead
//     context (default is "harmonik/integration").
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
