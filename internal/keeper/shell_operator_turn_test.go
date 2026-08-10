package keeper

import (
	"testing"
	"time"
)

func TestRecentOperatorTurn_UsesTranscriptLookback(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	var gotRole string
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
		RecentTranscriptTurnFn: func(_, _, role string) (time.Time, bool) {
			gotRole = role
			return now.Add(-time.Minute), true
		},
	}}

	if !c.recentOperatorTurn("session-1", now) {
		t.Fatal("recent real user turn did not hold pane action")
	}
	if gotRole != "user" {
		t.Fatalf("transcript role = %q; want user", gotRole)
	}
	if c.recentOperatorTurn("session-1", now.Add(6*time.Minute)) {
		t.Fatal("stale user turn held pane action")
	}
}
