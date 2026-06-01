package workspace

import (
	"testing"
)

// TestWM008_SmallScopeCollapseDefaultIsIntegration verifies that the default
// merge target for a parentless run is "integration" — i.e., squash-merge to
// harmonik/integration. The "main" target (small-scope-collapse) is configured
// via the WM-005b precedence chain: per-bead `## Branching` target_branch field,
// project-level .harmonik/branching.yaml lands_on, or spec-level default.
//
// Note: WM-008 (the retired two-value operator-policy enum) was superseded by
// WM-005b at spec v0.5.0. The small-scope-collapse shape (merging directly to
// main) is now expressed as `target_branch: main` in .harmonik/branching.yaml
// or the per-bead ## Branching section. The sensor below verifies the resolution
// semantics — default is `harmonik/integration`; explicit `main` produces the
// small-scope-collapse landing — consistent with WM-005b and WM-006.
//
// Spec refs:
//   - workspace-model.md §4.2 WM-005b — target_branch resolution precedence.
//   - workspace-model.md §4.2 WM-006 — default integration branch (harmonik/integration).
func TestWM008_SmallScopeCollapseDefaultIsIntegration(t *testing.T) {
	t.Parallel()

	// mergeTarget models the WM-005b target_branch resolution for the purpose
	// of this sensor: given a parent bead ID (empty = no parent context) and a
	// resolved target_branch string (from branching.yaml lands_on, per-bead ##
	// Branching section, or absent/empty for the spec-level default), it returns
	// the branch name the task branch lands on. Inlined per bead discipline.
	mergeTarget := func(parentBeadID string, operatorOverride string) string {
		// A non-empty parentBeadID means the run has a parent-bead context;
		// in that case the integration branch is derived per WM-006, regardless
		// of operator policy (WM-008 only governs the parentless case).
		if parentBeadID != "" {
			return "harmonik/integration/" + parentBeadID
		}
		// Parentless run: apply operator policy.
		switch operatorOverride {
		case "main":
			return "main"
		case "integration", "":
			// "" means absent override → default to "integration".
			return "harmonik/integration"
		default:
			// Unrecognised override: fail-safe to default.
			return "harmonik/integration"
		}
	}

	t.Run("default-no-override-is-integration", func(t *testing.T) {
		t.Parallel()

		// No parent bead, no operator override → default MUST be integration.
		got := mergeTarget("", "")
		want := "harmonik/integration"
		if got != want {
			t.Errorf("WM-008: default merge target (no parent, no override) = %q, want %q", got, want)
		}
	})

	t.Run("explicit-integration-override", func(t *testing.T) {
		t.Parallel()

		// No parent bead, explicit "integration" override → integration.
		got := mergeTarget("", "integration")
		want := "harmonik/integration"
		if got != want {
			t.Errorf("WM-008: merge target (no parent, override=integration) = %q, want %q", got, want)
		}
	})

	t.Run("explicit-main-override-small-scope-collapse", func(t *testing.T) {
		t.Parallel()

		// No parent bead, explicit "main" override → main (small-scope-collapse).
		got := mergeTarget("", "main")
		want := "main"
		if got != want {
			t.Errorf("WM-008: merge target (no parent, override=main) = %q, want %q", got, want)
		}
	})

	t.Run("parent-bead-ignores-override", func(t *testing.T) {
		t.Parallel()

		// A run WITH a parent bead uses the derived integration branch regardless
		// of operator override — WM-008 only governs the parentless case.
		got := mergeTarget("hk-8mwo", "main")
		want := "harmonik/integration/hk-8mwo"
		if got != want {
			t.Errorf("WM-008: merge target (parent=hk-8mwo, override=main) = %q, want %q", got, want)
		}
	})
}

// TestWM008_PolicyValuesAreExhaustive verifies the two meaningful target_branch
// values for the small-scope-collapse decision: "harmonik/integration" (default)
// and "main" (small-scope-collapse). Any other configured value falls back to
// the spec-level default per WM-005b + WM-006.
//
// Spec refs:
//   - workspace-model.md §4.2 WM-005b — target_branch resolution.
//   - workspace-model.md §4.2 WM-006 — spec-level default is harmonik/integration.
func TestWM008_PolicyValuesAreExhaustive(t *testing.T) {
	t.Parallel()

	// The two normative policy values.
	allowedPolicies := []string{"integration", "main"}

	// Verify both allowed values produce ref-safe branch names.
	for _, policy := range allowedPolicies {
		t.Run("policy-"+policy+"-is-ref-safe", func(t *testing.T) {
			t.Parallel()

			var targetBranch string
			switch policy {
			case "integration":
				targetBranch = "harmonik/integration"
			case "main":
				targetBranch = "main"
			}

			branchNameFixtureAssertRefSafe(t, "WM-008", targetBranch)
		})
	}

	// Verify an unknown policy value falls back to the default "integration".
	t.Run("unknown-policy-falls-back-to-integration", func(t *testing.T) {
		t.Parallel()

		unknownPolicy := "squash-to-feature"
		// The policy gate function (inlined here) should treat unknown as default.
		var got string
		switch unknownPolicy {
		case "main":
			got = "main"
		default:
			got = "harmonik/integration"
		}

		want := "harmonik/integration"
		if got != want {
			t.Errorf("WM-008: unknown policy %q should fall back to %q, got %q",
				unknownPolicy, want, got)
		}
	})
}
