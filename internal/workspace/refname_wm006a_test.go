package workspace

import (
	"errors"
	"testing"
)

// TestWM006a_BeadIDToRefSafe_VerbatimAccepted verifies that bead IDs whose
// verbatim embedding passes git check-ref-format are returned unchanged.
//
// Spec ref: workspace-model.md §4.2 WM-006a — "a zero exit code means the
// name is accepted verbatim".
func TestWM006a_BeadIDToRefSafe_VerbatimAccepted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		beadID  string
		wantOut string
	}{
		{
			name:    "standard alphanumeric bead ID",
			beadID:  "hk-8mwo",
			wantOut: "hk-8mwo",
		},
		{
			name:    "bead ID with dot-separated suffix (no trailing dot-lock)",
			beadID:  "hk-8mwo66",
			wantOut: "hk-8mwo66",
		},
		{
			name:    "all-lowercase",
			beadID:  "abc123",
			wantOut: "abc123",
		},
		{
			name:    "uppercase with digits",
			beadID:  "ABC123",
			wantOut: "ABC123",
		},
		{
			name:    "single internal slash (ref-safe by construction)",
			beadID:  "feature/abc",
			wantOut: "feature/abc",
		},
		{
			name:    "underscore and hyphen",
			beadID:  "some_thing-here",
			wantOut: "some_thing-here",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := BeadIDToRefSafe(t.Context(), tc.beadID)
			if err != nil {
				t.Fatalf("WM-006a: BeadIDToRefSafe(%q) unexpected error: %v", tc.beadID, err)
			}
			if got != tc.wantOut {
				t.Errorf("WM-006a: BeadIDToRefSafe(%q) = %q, want %q", tc.beadID, got, tc.wantOut)
			}
		})
	}
}

// TestWM006a_BeadIDToRefSafe_FallbackApplied verifies that bead IDs whose
// verbatim embedding fails git check-ref-format are transformed via the
// canonical hex-encode fallback and the transformed form is returned.
//
// Spec ref: workspace-model.md §4.2 WM-006a — "a non-zero exit code means a
// canonical fallback transformation MUST be applied and re-validated."
func TestWM006a_BeadIDToRefSafe_FallbackApplied(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		beadID     string
		wantSuffix string // expected safe bead-ID portion returned
	}{
		{
			// @{ is invalid in git refs.
			name:       "at-brace sequence",
			beadID:     "bead@{broken}",
			wantSuffix: "bead%40%7Bbroken%7D",
		},
		{
			// Leading dot is rejected by git check-ref-format.
			name:       "leading dot",
			beadID:     ".hidden-bead",
			wantSuffix: "%2Ehidden-bead",
		},
		{
			// Trailing .lock component is rejected by git check-ref-format.
			name:       "trailing dot-lock",
			beadID:     "bead.lock",
			wantSuffix: "bead%2Elock",
		},
		{
			// Control characters are forbidden in git refs.
			name:       "null byte",
			beadID:     "bead\x00null",
			wantSuffix: "bead%00null",
		},
		{
			// Newline is a control character forbidden in git refs.
			name:       "newline",
			beadID:     "bead\nnewline",
			wantSuffix: "bead%0Anewline",
		},
		{
			// Tab is a control character forbidden in git refs.
			name:       "tab",
			beadID:     "bead\ttab",
			wantSuffix: "bead%09tab",
		},
		{
			// Space is forbidden in git refs.
			name:       "space",
			beadID:     "bead space",
			wantSuffix: "bead%20space",
		},
		{
			// Double slash collapses to single slash after hex-encode step.
			// Double slash does not contain any non-[a-zA-Z0-9/_-] bytes, so the
			// hex step leaves them as-is; step (ii) then collapses them.
			name:       "double slash collapses",
			beadID:     "bead//double",
			wantSuffix: "bead/double",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := BeadIDToRefSafe(t.Context(), tc.beadID)
			if err != nil {
				t.Fatalf("WM-006a: BeadIDToRefSafe(%q) unexpected error: %v", tc.beadID, err)
			}
			if got != tc.wantSuffix {
				t.Errorf("WM-006a: BeadIDToRefSafe(%q) = %q, want %q", tc.beadID, got, tc.wantSuffix)
			}

			// The returned form MUST also pass git check-ref-format independently.
			if !refNameIsRefSafe(t, "harmonik/integration/"+got) {
				t.Errorf("WM-006a: returned bead ID %q (from %q) fails git check-ref-format",
					got, tc.beadID)
			}
		})
	}
}

// TestWM006a_BeadIDToRefSafe_RejectsUnrecoverable verifies that bead IDs that
// cannot produce a valid ref name even after fallback are rejected with
// ErrRefNameInvalid.
//
// Spec ref: workspace-model.md §4.2 WM-006a — "reject and fail-fast if the
// resulting name would be the bare '@' character, is empty, is a single '.'"
// and workspace-model.md §8 — RefNameInvalid.
//
// Note: the spec's bare-"@" and single-"." fast-fail guards apply to the
// RESULT of the fallback transformation, not the input. Via hex-encode,
// "@" → "%40" and "." → "%2E", both of which are ref-safe. Those guards fire
// only if a future alternate transformation emits "." or "@" literally. The
// canonical rejection case exercised here is the empty-string input (produces
// an empty fallback result), which triggers the "is empty" guard.
func TestWM006a_BeadIDToRefSafe_RejectsUnrecoverable(t *testing.T) {
	t.Parallel()

	t.Run("empty bead ID", func(t *testing.T) {
		t.Parallel()

		_, err := BeadIDToRefSafe(t.Context(), "")
		if !errors.Is(err, ErrRefNameInvalid) {
			t.Errorf("WM-006a: BeadIDToRefSafe(%q) = %v, want ErrRefNameInvalid", "", err)
		}
	})
}

// TestWM006a_BeadIDToRefSafe_ResultPassesCheckRefFormat is an end-to-end
// round-trip: for a set of representative bead IDs, BeadIDToRefSafe MUST
// return a value that independently passes git check-ref-format when embedded
// in the integration-branch template.
//
// Spec ref: workspace-model.md §4.2 WM-006a — git check-ref-format is the
// single source of truth for ref-name validity at each check.
func TestWM006a_BeadIDToRefSafe_ResultPassesCheckRefFormat(t *testing.T) {
	t.Parallel()

	ids := []string{
		"hk-8mwo",
		"hk-8mwo.11",
		"feature/add-thing",
		"bead@{bad}",
		".leading-dot",
		"bead.lock",
		"bead\x00null",
		"bead//double//slash",
		"ABC-123_xyz",
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			safe, err := BeadIDToRefSafe(t.Context(), id)
			if err != nil {
				// ErrRefNameInvalid is acceptable for genuinely unrecoverable IDs.
				if errors.Is(err, ErrRefNameInvalid) {
					t.Logf("WM-006a: BeadIDToRefSafe(%q) → ErrRefNameInvalid (expected for unrecoverable input)", id)
					return
				}
				t.Fatalf("WM-006a: BeadIDToRefSafe(%q) unexpected error: %v", id, err)
			}

			fullBranch := "harmonik/integration/" + safe
			if !refNameIsRefSafe(t, fullBranch) {
				t.Errorf("WM-006a: BeadIDToRefSafe(%q) returned %q but %q fails git check-ref-format",
					id, safe, fullBranch)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// refnameFixture helpers — prefixed to avoid sibling-bead collision.
// These helpers are local to this fixture (bead hk-8mwo.11).
// ---------------------------------------------------------------------------

// refNameIsRefSafe wraps refNameCheckRefFormat with a testing.T for use in
// table-driven tests that need to assert the outcome.
func refNameIsRefSafe(t *testing.T, branch string) bool {
	t.Helper()
	return refNameCheckRefFormat(t.Context(), branch)
}
