package main

// dashboard_projection_test.go — table-driven coverage for the pure projection
// helpers behind `harmonik dashboard`.
//
// These functions decide what the operator sees on the fleet dashboard: which
// decisions land in which mailbox and in what order, whether a lane reads as
// staffed or dead, and how a missing value is rendered. They are pure over
// their inputs and were entirely uncovered — a reordering or a dropped case
// would change what the operator acts on with nothing failing.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

func TestMailboxUrgencyRank_OrdersBlockerFirst(t *testing.T) {
	tests := []struct {
		urgency string
		want    int
	}{
		{string(core.DecisionUrgencyBlocker), 0},
		{string(core.DecisionUrgencyQuestion), 1},
		{string(core.DecisionUrgencyFYI), 2},
		{"", 3},
		{"not-an-urgency", 3},
	}
	for _, tc := range tests {
		t.Run(tc.urgency, func(t *testing.T) {
			if got := mailboxUrgencyRank(tc.urgency); got != tc.want {
				t.Errorf("mailboxUrgencyRank(%q) = %d, want %d", tc.urgency, got, tc.want)
			}
		})
	}

	// The ordering itself is the contract, not the specific integers.
	if mailboxUrgencyRank(string(core.DecisionUrgencyBlocker)) >=
		mailboxUrgencyRank(string(core.DecisionUrgencyQuestion)) {
		t.Error("blocker must sort before question")
	}
	if mailboxUrgencyRank(string(core.DecisionUrgencyFYI)) >= mailboxUrgencyRank("") {
		t.Error("fyi must sort before unspecified")
	}
}

func TestFilterDecisionsByTopic(t *testing.T) {
	decisions := []daemon.DashDecision{
		{DecisionID: "d-3", Topic: "ops", Urgency: string(core.DecisionUrgencyFYI)},
		{DecisionID: "d-1", Topic: "ops", Urgency: string(core.DecisionUrgencyBlocker)},
		{DecisionID: "d-9", Topic: "product", Urgency: string(core.DecisionUrgencyBlocker)},
		{DecisionID: "d-2", Topic: "ops", Urgency: string(core.DecisionUrgencyQuestion)},
		{DecisionID: "d-0", Topic: "ops", Urgency: string(core.DecisionUrgencyBlocker)},
		{DecisionID: "d-4", Topic: "ops"}, // no urgency → last
	}

	t.Run("selects only the requested topic", func(t *testing.T) {
		got := filterDecisionsByTopic(decisions, "product")
		if len(got) != 1 || got[0].DecisionID != "d-9" {
			t.Fatalf("got %+v, want exactly [d-9]", ids(got))
		}
	})

	t.Run("orders by urgency then decision_id", func(t *testing.T) {
		got := ids(filterDecisionsByTopic(decisions, "ops"))
		want := []string{"d-0", "d-1", "d-2", "d-3", "d-4"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	})

	t.Run("unknown topic yields nothing", func(t *testing.T) {
		if got := filterDecisionsByTopic(decisions, "nope"); len(got) != 0 {
			t.Errorf("got %v, want empty", ids(got))
		}
	})

	t.Run("does not mutate the input order", func(t *testing.T) {
		// filterDecisionsByTopic sorts its own slice; the caller's must survive.
		_ = filterDecisionsByTopic(decisions, "ops")
		if decisions[0].DecisionID != "d-3" {
			t.Errorf("input reordered: first element is now %q, want d-3", decisions[0].DecisionID)
		}
	})
}

func ids(ds []daemon.DashDecision) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.DecisionID)
	}
	return out
}

func TestFilterLanesByStatus(t *testing.T) {
	lanes := []daemon.DashLane{
		{Lane: "a", Status: "active"},
		{Lane: "b", Status: "parked"},
		{Lane: "c", Status: "active"},
	}
	got := filterLanesByStatus(lanes, "active")
	if len(got) != 2 || got[0].Lane != "a" || got[1].Lane != "c" {
		t.Errorf("got %+v, want lanes a and c in order", got)
	}
	if len(filterLanesByStatus(lanes, "done")) != 0 {
		t.Error("unknown status must yield nothing")
	}
	if len(filterLanesByStatus(nil, "active")) != 0 {
		t.Error("nil lanes must yield nothing")
	}
}

func TestLaneHealth(t *testing.T) {
	snapWith := func(sessions ...daemon.StateSession) daemon.DashboardSnapshot {
		return daemon.DashboardSnapshot{State: daemon.StateSnapshot{Sessions: sessions}}
	}
	cognition := func(fill float64) *daemon.SessionCognition {
		return &daemon.SessionCognition{Context: daemon.SessionContext{FillFrac: fill}}
	}

	tests := []struct {
		name string
		lane daemon.DashLane
		snap daemon.DashboardSnapshot
		want string
	}{
		{
			name: "no crew assigned",
			lane: daemon.DashLane{Lane: "l1"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: true}),
			want: "unstaffed",
		},
		{
			name: "crew assigned but no session for it",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "ringo", Alive: true}),
			want: "absent",
		},
		{
			name: "crew session present but not alive",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: false}),
			want: "dead",
		},
		{
			name: "dead beats sleeping when both are set",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: false, AtRest: true}),
			want: "dead",
		},
		{
			name: "alive and at rest",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: true, AtRest: true}),
			want: "sleeping",
		},
		{
			name: "alive with a context gauge",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: true, Cognition: cognition(0.734)}),
			want: "alive fill=73%",
		},
		{
			name: "alive with no gauge",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(daemon.StateSession{Agent: "paul", Alive: true}),
			want: "alive",
		},
		{
			name: "first matching session wins",
			lane: daemon.DashLane{Lane: "l1", Crew: "paul"},
			snap: snapWith(
				daemon.StateSession{Agent: "paul", Alive: true},
				daemon.StateSession{Agent: "paul", Alive: false},
			),
			want: "alive",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := laneHealth(tc.lane, tc.snap); got != tc.want {
				t.Errorf("laneHealth = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestThroughputActualForLane(t *testing.T) {
	tp := &daemon.DashThroughput{
		Available: true,
		ByLane: []daemon.DashLaneThroughput{
			{Lane: "main-lane", BeadsClosed: 7},
			{Lane: "quality", BeadsClosed: 0},
		},
	}
	tests := []struct {
		name string
		lane string
		tp   *daemon.DashThroughput
		want string
	}{
		{"nil throughput", "main-lane", nil, "unavailable"},
		{"throughput not available", "main-lane", &daemon.DashThroughput{Available: false}, "unavailable"},
		{"known lane", "main-lane", tp, "7 beads"},
		{"known lane with zero closed", "quality", tp, "0 beads"},
		{"lane absent from the window", "nope", tp, "0 beads"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := throughputActualForLane(tc.lane, tc.tp); got != tc.want {
				t.Errorf("throughputActualForLane(%q) = %q, want %q", tc.lane, got, tc.want)
			}
		})
	}
}

// TestNvlAndOrDash covers the two independent empty-value renderers. They are
// separate functions in separate files with identical behaviour; the test pins
// that they agree so a change to one is visibly a change to both.
func TestNvlAndOrDash(t *testing.T) {
	for _, in := range []string{"", "x", " ", "-"} {
		gotNvl, gotOrDash := nvl(in), orDash(in)
		want := in
		if in == "" {
			want = "-"
		}
		if gotNvl != want {
			t.Errorf("nvl(%q) = %q, want %q", in, gotNvl, want)
		}
		if gotOrDash != want {
			t.Errorf("orDash(%q) = %q, want %q", in, gotOrDash, want)
		}
	}
}

func TestShort_TruncatesToTwelve(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"123456789012", "123456789012"},  // exactly 12 — untouched
		{"1234567890123", "123456789012"}, // 13 — truncated
		{"0e7b34524cde179c6c814a6e", "0e7b34524cde"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := short(tc.in); got != tc.want {
				t.Errorf("short(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
