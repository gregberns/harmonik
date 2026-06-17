package daemon

// dot_severity_join_escalate_hk0gnt_test.go — unit test: consolidate self-BLOCK
// escalates the severity-join even when all upstream axes are APPROVE.
//
// Scenario A (the gap closed by hk-0gnt):
//   upstream: {APPROVE, APPROVE, APPROVE}; consolidate self-reports BLOCK
//   → routing MUST be BLOCK (consolidate caught something the axes missed)
//
// Scenario B (the hk-cmry property must still hold):
//   upstream: {BLOCK, APPROVE, APPROVE}; consolidate self-reports APPROVE
//   → routing MUST still be BLOCK (self-APPROVE cannot de-escalate upstream-BLOCK)
//
// Bead ref: hk-0gnt (child of hk-3js5m; triggered by hk-cmry review).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/workspace"
)

// TestConsolidateSelfBlockEscalates_0gnt: a consolidate node that self-reports
// BLOCK while all upstream axes report APPROVE must route BLOCK.
func TestConsolidateSelfBlockEscalates_0gnt(t *testing.T) {
	g, nodesByID := tripleReviewSpine()

	axisReviewerVerdicts := map[string]string{
		"review_correctness": workspace.ReviewVerdictApprove,
		"review_design":      workspace.ReviewVerdictApprove,
		"review_tests":       workspace.ReviewVerdictApprove,
	}

	// consolidate self-reports BLOCK (it caught something the axes missed).
	selfReport := workspace.ReviewVerdictBlock
	axisReviewerVerdicts["consolidate"] = selfReport
	routingLabel := selfReport

	// Replay the hk-0gnt override path (upstream + self).
	if upstream, isJoin := isConsolidateJoinNode(g, nodesByID, "consolidate"); isJoin {
		allVerdicts := make([]string, 0, len(upstream)+1)
		allVerdicts = append(allVerdicts, selfReport) // self
		for id := range upstream {
			if v, ok := axisReviewerVerdicts[id]; ok {
				allVerdicts = append(allVerdicts, v)
			}
		}
		if joined := verdictSeverityMax(allVerdicts); joined != "" {
			routingLabel = joined
		}
	}

	if routingLabel != workspace.ReviewVerdictBlock {
		t.Fatalf("consolidate self-BLOCK + upstream all-APPROVE: routing label = %q; want %q (self-BLOCK must escalate the join) [hk-0gnt]",
			routingLabel, workspace.ReviewVerdictBlock)
	}
}

// TestConsolidateSelfApproveCannotDeescalate_0gnt: when an upstream axis
// reports BLOCK, a consolidate self-APPROVE must NOT de-escalate — routing
// must still be BLOCK (the hk-cmry severity-integrity property preserved).
func TestConsolidateSelfApproveCannotDeescalate_0gnt(t *testing.T) {
	g, nodesByID := tripleReviewSpine()

	axisReviewerVerdicts := map[string]string{
		"review_correctness": workspace.ReviewVerdictBlock,
		"review_design":      workspace.ReviewVerdictApprove,
		"review_tests":       workspace.ReviewVerdictApprove,
	}

	// consolidate self-reports the lenient APPROVE (tries to de-escalate).
	selfReport := workspace.ReviewVerdictApprove
	axisReviewerVerdicts["consolidate"] = selfReport
	routingLabel := selfReport

	// Replay the hk-0gnt override path (upstream + self).
	if upstream, isJoin := isConsolidateJoinNode(g, nodesByID, "consolidate"); isJoin {
		allVerdicts := make([]string, 0, len(upstream)+1)
		allVerdicts = append(allVerdicts, selfReport) // self
		for id := range upstream {
			if v, ok := axisReviewerVerdicts[id]; ok {
				allVerdicts = append(allVerdicts, v)
			}
		}
		if joined := verdictSeverityMax(allVerdicts); joined != "" {
			routingLabel = joined
		}
	}

	if routingLabel != workspace.ReviewVerdictBlock {
		t.Fatalf("consolidate self-APPROVE + upstream-BLOCK: routing label = %q; want %q (self-APPROVE must not de-escalate an upstream BLOCK) [hk-0gnt]",
			routingLabel, workspace.ReviewVerdictBlock)
	}
}
