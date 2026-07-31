package runlease

import "testing"

// disposition_test.go pins the polarity of the survive question. Four sites in
// the daemon's run function spelled that question three different ways before
// this package existed, and getting it backwards is the failure this step
// exists to prevent: a run reclaimed when it should survive costs a
// re-dispatch, and a run that survives when it should not strands its bead in
// progress with nothing alive to adopt it.

// allResources is every value of the Resource axis, so a test that walks it
// fails the day someone adds a resource without deciding its disposition.
var allResources = []Resource{
	ResourceUnnamed, WorkerSlot, LocalSlot, TunnelPort, TunnelProcess,
	Worktree, RunRecord, HookSession, AgentSession, SpawnSlot, ColdStartToken,
}

func TestSurvivalNeedsBothAnIndependentSessionAndAStoppingDaemon(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		exit Exit
		want Disposition
	}{
		{"nothing set at all", Exit{}, Reclaim},
		{
			"an independent session on a normal end is reclaimed like any other",
			Exit{SessionRunsIndependently: true},
			Reclaim,
		},
		{
			"a stopping daemon whose agent shares its session has nothing to leave",
			Exit{DaemonStopping: true},
			Reclaim,
		},
		{
			"both facts, and only then, survive",
			Exit{SessionRunsIndependently: true, DaemonStopping: true},
			Survive,
		},
		{
			"a failure worth reading keeps its evidence",
			Exit{EvidenceWorthKeeping: true},
			RetainEvidence,
		},
		{
			"evidence plus an independent session that is not shutting down",
			Exit{SessionRunsIndependently: true, EvidenceWorthKeeping: true},
			RetainEvidence,
		},
		{
			"evidence plus a stopping daemon with no independent session",
			Exit{DaemonStopping: true, EvidenceWorthKeeping: true},
			RetainEvidence,
		},
		{
			"survival wins over evidence, because it keeps the worktree anyway",
			Exit{SessionRunsIndependently: true, DaemonStopping: true, EvidenceWorthKeeping: true},
			Survive,
		},
	}

	if len(cases) != 8 {
		t.Fatalf("Exit has three booleans, so the table must cover 8 combinations, got %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Decide(tc.exit); got != tc.want {
				t.Errorf("Decide(%+v) = %s, want %s", tc.exit, got, tc.want)
			}
		})
	}
}

func TestReclaimGivesEveryResourceBack(t *testing.T) {
	t.Parallel()

	for _, r := range allResources {
		if !Reclaim.Releases(r) {
			t.Errorf("Reclaim kept %s; reclaim keeps nothing", r)
		}
	}
}

func TestSurvivalKeepsWhatTheAgentAndTheNextBootStillNeed(t *testing.T) {
	t.Parallel()

	// The session and the worktree are what the agent is working in. The run
	// record is how a later boot finds the session by name. The hook session
	// and the tunnel are how the agent reports; before this package they were
	// torn down regardless, which left a surviving agent unable to report.
	kept := map[Resource]bool{
		AgentSession: true, Worktree: true, RunRecord: true,
		HookSession: true, TunnelProcess: true, TunnelPort: true,
	}
	for _, r := range allResources {
		want := !kept[r]
		if got := Survive.Releases(r); got != want {
			t.Errorf("Survive.Releases(%s) = %v, want %v", r, got, want)
		}
	}
}

func TestSurvivalStillGivesBackThisProcessesOwnBookkeeping(t *testing.T) {
	t.Parallel()

	// The four accounting slots are counters inside the daemon. A surviving
	// agent does not hold them, so holding them on its behalf would only
	// over-count the dispatch gate.
	for _, r := range []Resource{WorkerSlot, LocalSlot, SpawnSlot, ColdStartToken} {
		if !Survive.Releases(r) {
			t.Errorf("Survive kept %s; the surviving agent does not hold it", r)
		}
	}
}

func TestRetainingEvidenceKeepsTheWorktreeAndNothingElse(t *testing.T) {
	t.Parallel()

	for _, r := range allResources {
		want := r != Worktree
		if got := RetainEvidence.Releases(r); got != want {
			t.Errorf("RetainEvidence.Releases(%s) = %v, want %v", r, got, want)
		}
	}
}

func TestAnUnnamedResourceIsGivenBackUnderEveryDisposition(t *testing.T) {
	t.Parallel()

	// The zero Resource is a caller mistake. Giving it back leaks nothing;
	// keeping it would leak something nobody named.
	for _, d := range []Disposition{Reclaim, Survive, RetainEvidence} {
		if !d.Releases(ResourceUnnamed) {
			t.Errorf("%s kept the unnamed resource", d)
		}
	}
}

func TestOnlyASurvivingRunLeavesItsBeadInProgress(t *testing.T) {
	t.Parallel()

	for _, d := range []Disposition{Reclaim, Survive, RetainEvidence} {
		if got, want := d.LeavesBeadInProgress(), d == Survive; got != want {
			t.Errorf("%s.LeavesBeadInProgress() = %v, want %v", d, got, want)
		}
	}
}

func TestTheZeroDispositionReclaims(t *testing.T) {
	t.Parallel()

	// A caller who never decided must take the recoverable mistake. Reclaiming
	// a run that should have survived costs a re-dispatch; surviving a run that
	// should have been reclaimed strands the bead.
	var d Disposition
	if d != Reclaim {
		t.Fatalf("the zero Disposition is %s, want reclaim", d)
	}
}

func TestEveryResourceAndDispositionHasAName(t *testing.T) {
	t.Parallel()

	// The names reach test failures and diagnostics. A new value that reports
	// itself as an older one makes both lie.
	seen := map[string]Resource{}
	for _, r := range allResources {
		name := r.String()
		if name == "" {
			t.Errorf("resource %d has no name", int(r))
		}
		if prior, dup := seen[name]; dup && r != ResourceUnnamed {
			t.Errorf("resources %d and %d both report %q", int(prior), int(r), name)
		}
		seen[name] = r
	}
	for _, d := range []Disposition{Reclaim, Survive, RetainEvidence} {
		if d.String() == "" {
			t.Errorf("disposition %d has no name", int(d))
		}
	}
}

func TestADispositionOutsideTheClosedSetGivesEverythingBack(t *testing.T) {
	t.Parallel()

	// The zero value is reclaim, so an out-of-range value can only come from a
	// cast. It must not be the one thing that keeps a resource standing.
	rogue := Disposition(99)
	for _, r := range allResources {
		if !rogue.Releases(r) {
			t.Errorf("a disposition outside the closed set kept %s", r)
		}
	}
	if rogue.LeavesBeadInProgress() {
		t.Error("a disposition outside the closed set left the bead in progress")
	}
	if rogue.String() != Reclaim.String() {
		t.Errorf("Disposition(99).String() = %q, want it to read as reclaim", rogue.String())
	}
}

func TestAResourceOutsideTheClosedSetIsGivenBack(t *testing.T) {
	t.Parallel()

	rogue := Resource(99)
	for _, d := range []Disposition{Reclaim, Survive, RetainEvidence} {
		if !d.Releases(rogue) {
			t.Errorf("%s kept a resource outside the closed set", d)
		}
	}
	if rogue.String() != ResourceUnnamed.String() {
		t.Errorf("Resource(99).String() = %q, want it to read as unnamed", rogue.String())
	}
}
