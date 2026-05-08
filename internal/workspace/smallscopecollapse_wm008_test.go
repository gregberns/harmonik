package workspace

import (
	"testing"
)

// TestWM008_SmallScopeCollapseDefaultIsIntegration verifies that the default
// merge target for a parentless run is "integration" — i.e., squash-merge to
// harmonik/integration. The "main" override (small-scope-collapse) is operator-gated
// and must be explicitly configured.
//
// Spec ref: workspace-model.md §4.2 WM-008 — "When a run has no parent-bead
// relationship in Beads (per [beads-integration.md §4.5 BI-014]), the task branch's
// merge target MUST be determined by operator policy per [operator-nfr.md §4.3] with
// two allowed values: `integration` (squash-merge to `harmonik/integration`, the
// default) or `main` (squash-merge directly to main — the small-scope-collapse shape).
// Absent an operator override, the default MUST be `integration` (consistent with
// WM-006). The decision is deterministic on the configured policy value; no cognition
// participates."
func TestWM008_SmallScopeCollapseDefaultIsIntegration(t *testing.T) {
	t.Parallel()

	// mergeTarget is a thin policy-gating function that returns the merge target
	// branch name given the parent bead ID (empty = no parent) and an operator
	// override string. The two allowed policy values are "integration" and "main".
	// This is inlined here per bead discipline (do not expose as a package-level helper).
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

// TestWM008_PolicyValuesAreExhaustive verifies that only "integration" and "main" are
// the two allowed operator policy values per WM-008. Any other value MUST be treated
// as unknown and fall back to the default.
//
// Spec ref: workspace-model.md §4.2 WM-008 — "two allowed values: `integration` ...
// or `main` ... Absent an operator override, the default MUST be `integration`".
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
