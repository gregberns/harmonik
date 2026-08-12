package dispatch

import "testing"

func TestClassifySessionTargetStates(t *testing.T) {
	intent := testIntent(PhaseHandoffDurable)
	live := sessionTargetObservation(intent)
	dead := live
	dead.PaneDead = "1"
	for _, tc := range []struct {
		name        string
		observation SessionTargetObservation
		want        SessionFact
	}{
		{name: "session absent", observation: SessionTargetObservation{Status: SessionTargetSessionAbsent}, want: SessionAbsent},
		{name: "window absent", observation: SessionTargetObservation{Status: SessionTargetWindowAbsent}, want: SessionAbsent},
		{name: "pane absent", observation: SessionTargetObservation{Status: SessionTargetPaneAbsent}, want: SessionAbsent},
		{name: "live", observation: live, want: SessionLive},
		{name: "dead", observation: dead, want: SessionDead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifySessionTarget(intent, tc.observation); got != tc.want {
				t.Fatalf("fact = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifySessionTargetRejectsEveryIdentityAndLivenessFault(t *testing.T) {
	intent := testIntent(PhaseHandoffDurable)
	tests := []struct {
		name   string
		mutate func(*SessionTargetObservation)
	}{
		{name: "duplicate", mutate: func(o *SessionTargetObservation) { o.Status = SessionTargetDuplicate }},
		{name: "session unreadable", mutate: func(o *SessionTargetObservation) { o.Status = SessionTargetSessionUnreadable }},
		{name: "window unreadable", mutate: func(o *SessionTargetObservation) { o.Status = SessionTargetWindowUnreadable }},
		{name: "options unreadable", mutate: func(o *SessionTargetObservation) { o.Status = SessionTargetOptionsUnreadable }},
		{name: "pane unreadable", mutate: func(o *SessionTargetObservation) { o.Status = SessionTargetPaneUnreadable }},
		{name: "unknown status", mutate: func(o *SessionTargetObservation) { o.Status = "unknown" }},
		{name: "run", mutate: func(o *SessionTargetObservation) { o.RunID = testQueueID }},
		{name: "claim", mutate: func(o *SessionTargetObservation) { o.ClaimTransitionID = testQueueID }},
		{name: "session", mutate: func(o *SessionTargetObservation) { o.SessionName = "other" }},
		{name: "window", mutate: func(o *SessionTargetObservation) { o.WindowName = "other" }},
		{name: "empty pid", mutate: func(o *SessionTargetObservation) { o.PanePID = "" }},
		{name: "text pid", mutate: func(o *SessionTargetObservation) { o.PanePID = "pid" }},
		{name: "zero pid", mutate: func(o *SessionTargetObservation) { o.PanePID = "0" }},
		{name: "negative pid", mutate: func(o *SessionTargetObservation) { o.PanePID = "-1" }},
		{name: "dead empty", mutate: func(o *SessionTargetObservation) { o.PaneDead = "" }},
		{name: "dead invalid", mutate: func(o *SessionTargetObservation) { o.PaneDead = "true" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			observation := sessionTargetObservation(intent)
			tc.mutate(&observation)
			if got := ClassifySessionTarget(intent, observation); got != SessionConflict {
				t.Fatalf("fact = %q, want conflict", got)
			}
		})
	}
}

func TestClassifySessionTargetRejectsPartialAbsenceAndPreHandoffTarget(t *testing.T) {
	intent := testIntent(PhaseHandoffDurable)
	partial := SessionTargetObservation{Status: SessionTargetSessionAbsent, RunID: intent.Binding.RunID.String()}
	if got := ClassifySessionTarget(intent, partial); got != SessionConflict {
		t.Fatalf("partial absence = %q, want conflict", got)
	}
	for _, phase := range []Phase{PhasePrepared, PhaseClaimDurable, PhaseClaimRefused, PhaseRunDurable} {
		t.Run(string(phase), func(t *testing.T) {
			if got := ClassifySessionTarget(testIntent(phase), sessionTargetObservation(intent)); got != SessionConflict {
				t.Fatalf("present pre-handoff target = %q, want conflict", got)
			}
		})
	}
}

func TestClassifySessionTargetAbsenceIsValidBeforeHandoff(t *testing.T) {
	for _, phase := range []Phase{PhasePrepared, PhaseClaimDurable, PhaseClaimRefused, PhaseRunDurable, PhaseHandoffDurable} {
		t.Run(string(phase), func(t *testing.T) {
			observation := SessionTargetObservation{Status: SessionTargetSessionAbsent}
			if got := ClassifySessionTarget(testIntent(phase), observation); got != SessionAbsent {
				t.Fatalf("absent target = %q, want absent", got)
			}
		})
	}
}

func TestClassifySessionTargetRejectsInvalidIntentAndZeroObservation(t *testing.T) {
	intent := testIntent(PhaseHandoffDurable)
	intent.Binding.BeadID = ""
	if got := ClassifySessionTarget(intent, SessionTargetObservation{Status: SessionTargetSessionAbsent}); got != SessionConflict {
		t.Fatalf("invalid intent fact = %q, want conflict", got)
	}
	if got := ClassifySessionTarget(testIntent(PhaseHandoffDurable), SessionTargetObservation{}); got != SessionConflict {
		t.Fatalf("zero observation fact = %q, want conflict", got)
	}
}

func sessionTargetObservation(intent Intent) SessionTargetObservation {
	return SessionTargetObservation{
		Status:            SessionTargetExact,
		RunID:             intent.Binding.RunID.String(),
		ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName:       intent.Handoff.SessionName,
		WindowName:        intent.Handoff.WindowName,
		PanePID:           "1234",
		PaneDead:          "0",
	}
}
