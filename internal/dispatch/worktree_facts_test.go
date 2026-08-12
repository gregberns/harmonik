package dispatch

import "testing"

func TestClassifyWorktreeObservationsAbsentAndExactLease(t *testing.T) {
	intent := queueFactIntent(PhaseRunDurable, "")
	if got := ClassifyWorktreeObservations(intent, nil); got != WorktreeAbsent {
		t.Fatalf("absent = %q", got)
	}
	observation := worktreeFactObservation()
	if got := ClassifyWorktreeObservations(intent, []WorktreeObservation{observation}); got != WorktreeLeased {
		t.Fatalf("exact lease = %q", got)
	}
}

func TestClassifyWorktreeObservationsRejectsPartialAndConflictingAuthority(t *testing.T) {
	intent := queueFactIntent(PhaseRunDurable, "")
	for _, tc := range []struct {
		name   string
		mutate func(*WorktreeObservation)
	}{
		{name: "path absent", mutate: func(w *WorktreeObservation) { w.Path = "" }},
		{name: "path run mismatch", mutate: func(w *WorktreeObservation) { w.Path = "/tmp/other-run" }},
		{name: "not registered", mutate: func(w *WorktreeObservation) { w.Registered = false }},
		{name: "lease absent", mutate: func(w *WorktreeObservation) { w.LeasePresent = false }},
		{name: "lease unreadable", mutate: func(w *WorktreeObservation) { w.LeaseReadable = false }},
		{name: "lease run mismatch", mutate: func(w *WorktreeObservation) { w.LeaseRunID = testQueueID }},
		{name: "lease run malformed", mutate: func(w *WorktreeObservation) { w.LeaseRunID = "not-a-run" }},
		{name: "lease pid", mutate: func(w *WorktreeObservation) { w.LeasePID = 0 }},
		{name: "lease created", mutate: func(w *WorktreeObservation) { w.LeaseCreatedAt = "not-time" }},
		{name: "lease ttl", mutate: func(w *WorktreeObservation) { w.LeaseTTLSec = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation := worktreeFactObservation()
			tc.mutate(&observation)
			if got := ClassifyWorktreeObservations(intent, []WorktreeObservation{observation}); got != WorktreeConflict {
				t.Fatalf("fact = %q, want conflict", got)
			}
		})
	}
}

func TestClassifyWorktreeObservationsRejectsDuplicateExactCandidates(t *testing.T) {
	intent := queueFactIntent(PhaseRunDurable, "")
	observation := worktreeFactObservation()
	if got := ClassifyWorktreeObservations(intent, []WorktreeObservation{observation, observation}); got != WorktreeConflict {
		t.Fatalf("duplicate = %q", got)
	}
}

func TestClassifyWorktreeObservationsIgnoresOtherRuns(t *testing.T) {
	intent := queueFactIntent(PhaseRunDurable, "")
	other := worktreeFactObservation()
	other.RunID = testQueueID
	other.LeaseRunID = testQueueID
	if got := ClassifyWorktreeObservations(intent, []WorktreeObservation{other}); got != WorktreeAbsent {
		t.Fatalf("other run = %q", got)
	}
}

func TestClassifyWorktreeObservationsRejectsForeignPathClaimingIntentLease(t *testing.T) {
	intent := queueFactIntent(PhaseRunDurable, "")
	foreign := worktreeFactObservation()
	foreign.RunID = testQueueID
	foreign.Path = "/tmp/" + testQueueID
	if got := ClassifyWorktreeObservations(intent, []WorktreeObservation{foreign}); got != WorktreeConflict {
		t.Fatalf("foreign lease collision = %q", got)
	}
}

func worktreeFactObservation() WorktreeObservation {
	return WorktreeObservation{
		RunID: testRunID, Path: "/tmp/" + testRunID, Registered: true,
		LeasePresent: true, LeaseReadable: true, LeaseRunID: testRunID,
		LeasePID: 42, LeaseCreatedAt: "2026-08-11T12:13:14Z", LeaseTTLSec: 60,
	}
}
