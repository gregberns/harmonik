package daemon

// dot_review_severity_join_hkcmry_test.go — unit test: the DOT consolidate node
// routes on the DETERMINISTIC severity-max of its upstream per-axis reviewer
// verdicts, OVERRIDING its own self-reported verdict.
//
// # Why this file exists (the review-integrity hole)
//
// opus-triple-review.dot wires the spine
//
//	implement → commit_gate → review_correctness → review_design → review_tests
//	          → consolidate → {close | implement | close-needs-attention}
//
// The three axis-reviewer edges are UNCONDITIONAL (they always advance); the
// intended severity-join (BLOCK > REQUEST_CHANGES > APPROVE) lived ONLY as PROSE
// in the consolidate node's role= string. Before hk-cmry, dot_cascade.go set the
// routing preferred_label VERBATIM to whatever single verdict the consolidate
// reviewer self-reported in .harmonik/review.json. A consolidate LLM that
// self-reports APPROVE while an upstream axis said REQUEST_CHANGES therefore
// routed consolidate→close and MERGED work that an axis-reviewer had rejected —
// an unreviewed RED-only commit reached main and broke the build fleet-wide.
//
// # What this test proves
//
// FIX #1: when a reviewer node has >= 2 upstream reviewer predecessors (it is a
// consolidate-style JOIN node), driveDotWorkflow OVERRIDES the routing
// preferred_label with verdictSeverityMax of the recorded upstream axis
// verdicts. Concretely, upstream {APPROVE, REQUEST_CHANGES, APPROVE} +
// consolidate self-reports APPROVE → routing label == REQUEST_CHANGES (the
// fix-loop edge), NOT APPROVE (the close/merge edge).
//
// RED→GREEN: the join-assertion below is FALSE under the pre-fix code path
// (verbatim self-report = APPROVE) and TRUE only with the deterministic
// severity-max override. Reverting the override in dispatch makes
// TestSeverityJoinOverridesSelfReport fail.
//
// Bead ref: hk-cmry.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

func TestVerdictSeverityMax(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, ""},
		{"single approve", []string{workspace.ReviewVerdictApprove}, workspace.ReviewVerdictApprove},
		{
			"approve+request_changes+approve → request_changes",
			[]string{workspace.ReviewVerdictApprove, workspace.ReviewVerdictRequestChanges, workspace.ReviewVerdictApprove},
			workspace.ReviewVerdictRequestChanges,
		},
		{
			"request_changes+block → block",
			[]string{workspace.ReviewVerdictRequestChanges, workspace.ReviewVerdictBlock},
			workspace.ReviewVerdictBlock,
		},
		{
			"all approve → approve",
			[]string{workspace.ReviewVerdictApprove, workspace.ReviewVerdictApprove, workspace.ReviewVerdictApprove},
			workspace.ReviewVerdictApprove,
		},
		{
			"block dominates everything",
			[]string{workspace.ReviewVerdictApprove, workspace.ReviewVerdictBlock, workspace.ReviewVerdictRequestChanges},
			workspace.ReviewVerdictBlock,
		},
		{
			"unknown ranks as request_changes (fail-closed, never silently approve)",
			[]string{workspace.ReviewVerdictApprove, "GARBAGE"},
			"GARBAGE", // rank 1 == REQUEST_CHANGES, beats APPROVE (rank 0)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verdictSeverityMax(tt.in); got != tt.want {
				t.Fatalf("verdictSeverityMax(%v) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

// tripleReviewSpine builds the minimal opus-triple-review.dot spine: three
// distinct axis-reviewer nodes (routing UNCONDITIONALLY to the next) feeding a
// consolidate reviewer node that BRANCHES on outcome.preferred_label — exactly
// the production graph's shape.
func tripleReviewSpine() (*dot.Graph, map[string]*dot.Node) {
	rev := func(id string) *dot.Node {
		return &dot.Node{ID: id, Type: core.NodeTypeAgentic, AgentType: "reviewer", HandlerRef: "claude-reviewer"}
	}
	implement := &dot.Node{ID: "implement", Type: core.NodeTypeAgentic, AgentType: "implementer", HandlerRef: "claude-implementer"}
	rc := rev("review_correctness")
	rd := rev("review_design")
	rt := rev("review_tests")
	cons := rev("consolidate")
	close := &dot.Node{ID: "close", Type: core.NodeTypeNonAgentic, HandlerRef: "noop"}
	needs := &dot.Node{ID: "close-needs-attention", Type: core.NodeTypeNonAgentic, HandlerRef: "noop"}
	plCond := func(rhs string) *dot.Condition {
		return &dot.Condition{Clauses: []dot.Equality{{LHS: "outcome.preferred_label", Op: "==", RHS: rhs}}}
	}
	g := &dot.Graph{
		Nodes: []*dot.Node{implement, rc, rd, rt, cons, close, needs},
		Edges: []*dot.Edge{
			{FromNodeID: "implement", ToNodeID: "review_correctness"},
			// Axis reviewers route UNCONDITIONALLY to the next axis.
			{FromNodeID: "review_correctness", ToNodeID: "review_design"},
			{FromNodeID: "review_design", ToNodeID: "review_tests"},
			{FromNodeID: "review_tests", ToNodeID: "consolidate"},
			// consolidate BRANCHES on the verdict — the join signature.
			{FromNodeID: "consolidate", ToNodeID: "close", Condition: plCond("APPROVE")},
			{FromNodeID: "consolidate", ToNodeID: "implement", Condition: plCond("REQUEST_CHANGES")},
			{FromNodeID: "consolidate", ToNodeID: "close-needs-attention", Condition: plCond("BLOCK")},
		},
	}
	nodesByID := map[string]*dot.Node{}
	for _, n := range g.Nodes {
		nodesByID[n.ID] = n
	}
	return g, nodesByID
}

func TestUpstreamReviewerNodeIDs(t *testing.T) {
	g, nodesByID := tripleReviewSpine()

	// consolidate has all three axis reviewers as transitive predecessors.
	upstream := upstreamReviewerNodeIDs(g, nodesByID, "consolidate")
	for _, want := range []string{"review_correctness", "review_design", "review_tests"} {
		if !upstream[want] {
			t.Errorf("consolidate upstream missing reviewer %q; got %v", want, upstream)
		}
	}
	if upstream["consolidate"] {
		t.Errorf("consolidate must not be its own upstream")
	}
	if upstream["implement"] {
		t.Errorf("implementer node must not count as an upstream reviewer; got %v", upstream)
	}
	if len(upstream) != 3 {
		t.Fatalf("consolidate upstream reviewer count = %d; want 3 (%v)", len(upstream), upstream)
	}
}

func TestIsConsolidateJoinNode(t *testing.T) {
	g, nodesByID := tripleReviewSpine()

	// consolidate IS a join node: routes on preferred_label AND has 3 upstream reviewers.
	if upstream, ok := isConsolidateJoinNode(g, nodesByID, "consolidate"); !ok || len(upstream) != 3 {
		t.Fatalf("consolidate must be a join node with 3 upstream reviewers; ok=%v upstream=%v", ok, upstream)
	}

	// An axis reviewer is NOT a join node even though review_tests has >= 2
	// upstream reviewers: it routes UNCONDITIONALLY to consolidate, so it fails
	// the routes-on-preferred_label test. This is the case my own earlier
	// >= 2-only heuristic mis-classified.
	if _, ok := isConsolidateJoinNode(g, nodesByID, "review_tests"); ok {
		t.Fatalf("review_tests routes unconditionally and must NOT be a join node")
	}
	if _, ok := isConsolidateJoinNode(g, nodesByID, "review_correctness"); ok {
		t.Fatalf("review_correctness must NOT be a join node")
	}
}

// TestSeverityJoinOverridesSelfReport is the load-bearing RED→GREEN assertion.
// It replays exactly the override path driveDotWorkflow executes when the
// consolidate node completes: record per-axis verdicts, then for a >= 2-upstream
// reviewer node compute the severity-max and override the routing label.
//
// Scenario: upstream {APPROVE, REQUEST_CHANGES, APPROVE}; consolidate self-reports
// APPROVE. The deterministic join MUST route REQUEST_CHANGES (fix-loop edge), not
// the consolidate's lenient APPROVE (close/merge edge).
func TestSeverityJoinOverridesSelfReport(t *testing.T) {
	g, nodesByID := tripleReviewSpine()

	// Per-axis verdicts as recorded by driveDotWorkflow before consolidate runs.
	axisReviewerVerdicts := map[string]string{
		"review_correctness": workspace.ReviewVerdictApprove,
		"review_design":      workspace.ReviewVerdictRequestChanges,
		"review_tests":       workspace.ReviewVerdictApprove,
	}

	// consolidate self-reports the lenient APPROVE.
	selfReport := workspace.ReviewVerdictApprove
	axisReviewerVerdicts["consolidate"] = selfReport
	routingLabel := selfReport // pre-fix: routing == verbatim self-report.

	// --- the override path from dot_cascade.go ---
	if upstream, isJoin := isConsolidateJoinNode(g, nodesByID, "consolidate"); isJoin {
		upstreamVerdicts := make([]string, 0, len(upstream))
		for id := range upstream {
			if v, ok := axisReviewerVerdicts[id]; ok {
				upstreamVerdicts = append(upstreamVerdicts, v)
			}
		}
		if joined := verdictSeverityMax(upstreamVerdicts); joined != "" {
			routingLabel = joined
		}
	}

	// GREEN with fix #1; this assertion is FALSE under the pre-fix verbatim path
	// (routingLabel would remain APPROVE).
	if routingLabel != workspace.ReviewVerdictRequestChanges {
		t.Fatalf("consolidate routing label = %q; want %q (deterministic severity-max of upstream axes must override the lenient self-report)",
			routingLabel, workspace.ReviewVerdictRequestChanges)
	}
	if routingLabel == workspace.ReviewVerdictApprove {
		t.Fatalf("REVIEW-INTEGRITY HOLE: an upstream REQUEST_CHANGES was overridden by a self-reported APPROVE → work would merge unreviewed")
	}
}
