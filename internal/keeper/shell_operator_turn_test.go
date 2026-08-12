package keeper

import (
	"testing"
	"time"
)

type operatorTurnActivity struct {
	at time.Time
	ok bool
}

func (a operatorTurnActivity) IdleMarkerModTime() (time.Time, bool)  { return time.Time{}, false }
func (a operatorTurnActivity) LastUserTurn(string) (time.Time, bool) { return a.at, a.ok }
func (a operatorTurnActivity) LastAssistantTurn(string) (time.Time, bool) {
	return time.Time{}, false
}

func TestRecentOperatorTurn_UsesTranscriptLookback(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
	}, activity: operatorTurnActivity{at: now.Add(-time.Minute), ok: true}}

	st := CycleState{PrevSID: "session-1"}
	if !c.recentOperatorTurn(st, now) {
		t.Fatal("recent real user turn did not hold pane action")
	}
	if c.recentOperatorTurn(st, now.Add(6*time.Minute)) {
		t.Fatal("stale user turn held pane action")
	}
}

func TestRecentOperatorTurn_IgnoresInjectionArtifacts(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
	}, activity: operatorTurnActivity{at: now.Add(time.Second), ok: true}}
	st := CycleState{PrevSID: "session-1", InjectedAt: now}
	if c.recentOperatorTurn(st, now.Add(3*time.Second)) {
		t.Fatal("keeper injection artifact reported as operator activity")
	}
}

func TestRecentOperatorTurn_DetectsTurnAfterInjectionWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &Cycler{cfg: CyclerConfig{
		OperatorTurnLookback: 5 * time.Minute,
	}, activity: operatorTurnActivity{at: now.Add(3 * time.Second), ok: true}}
	st := CycleState{PrevSID: "session-1", InjectedAt: now}
	if !c.recentOperatorTurn(st, now.Add(4*time.Second)) {
		t.Fatal("real post-injection operator turn was not detected")
	}
}
