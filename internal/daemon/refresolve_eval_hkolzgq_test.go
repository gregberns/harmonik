package daemon_test

// refresolve_eval_hkolzgq_test.go — unit tests for the tier-1.5 eval-bead
// routing added by hk-olzgq.
//
// Tier-1.5 routes beads carrying exactly the label "codename:eval" (and no
// explicit dot: label) to "eval-bead.dot". This prevents eval task beads from
// falling through to the project-level workflow.dot (sonnet-triple-review),
// whose heavyweight 3-reviewer cascade times out on small eval diffs.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func TestResolveWorkflowRef_EvalTier15(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		beadLabels      []string
		itemWorkflowRef string
		wantRef         string
	}{
		{
			name:       "codename:eval routes to eval-bead.dot",
			beadLabels: []string{"codename:eval", "difficulty:simple", "harness:pi"},
			wantRef:    "eval-bead.dot",
		},
		{
			name:       "codename:eval with other labels still routes to eval-bead.dot",
			beadLabels: []string{"codename:eval", "difficulty:medium", "harness:pi", "workflow:dot"},
			wantRef:    "eval-bead.dot",
		},
		{
			name: "dot: label wins over codename:eval (tier 1 before tier 1.5)",
			beadLabels: []string{"codename:eval", "dot:custom-eval"},
			wantRef:    "custom-eval.dot",
		},
		{
			name:            "tier-0 item ref wins over codename:eval",
			beadLabels:      []string{"codename:eval", "difficulty:simple"},
			itemWorkflowRef: "explicit-override.dot",
			wantRef:         "explicit-override.dot",
		},
		{
			// codename:eval-program is NOT codename:eval (exact-match only)
			name:       "codename:eval-program does NOT route to eval-bead.dot",
			beadLabels: []string{"codename:eval-program", "ws:metrics"},
			wantRef:    "",
		},
		{
			// codename:eval-harness is NOT codename:eval
			name:       "codename:eval-harness does NOT route to eval-bead.dot",
			beadLabels: []string{"codename:eval-harness"},
			wantRef:    "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bead := core.BeadRecord{
				BeadID:   core.BeadID("hk-eval-fixture"),
				Title:    "eval tier-1.5 fixture",
				BeadType: "task",
				Status:   core.CoarseStatusOpen,
				Labels:   tc.beadLabels,
			}
			got := daemon.ExportedResolveWorkflowRef(bead, tc.itemWorkflowRef)

			if got != tc.wantRef {
				t.Errorf("resolveWorkflowRef: got %q; want %q", got, tc.wantRef)
			}
		})
	}
}
