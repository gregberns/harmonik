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

	st := CycleState{PrevSID: "session-1"}
	if !c.recentOperatorTurn(st, now) {
		t.Fatal("recent real user turn did not hold pane action")
	}
	if gotRole != "user" {
		t.Fatalf("transcript role = %q; want user", gotRole)
	}
	if c.recentOperatorTurn(st, now.Add(6*time.Minute)) {
		t.Fatal("stale user turn held pane action")
	}
}

func TestRecentOperatorTurn_IgnoresInjectionArtifacts(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
		RecentTranscriptTurnFn: func(_, _, _ string) (time.Time, bool) {
			return now.Add(time.Second), true
		},
	}}
	st := CycleState{PrevSID: "session-1", InjectedAt: now}
	if c.recentOperatorTurn(st, now.Add(3*time.Second)) {
		t.Fatal("keeper injection artifact reported as operator activity")
	}
}

func TestRecentOperatorTurn_DetectsTurnAfterInjectionWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
		RecentTranscriptTurnFn: func(_, _, _ string) (time.Time, bool) {
			return now.Add(3 * time.Second), true
		},
	}}
	st := CycleState{PrevSID: "session-1", InjectedAt: now}
	if !c.recentOperatorTurn(st, now.Add(4*time.Second)) {
		t.Fatal("real post-injection operator turn was not detected")
	}
}
