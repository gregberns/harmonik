package runlease

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// recorder collects the order in which resources were given back, which is the
// property three of the run's ordering edges depend on: the agent session must
// die before the worktree is removed, or `git worktree remove --force` races a
// live process inside the directory.
type recorder struct {
	mu    sync.Mutex
	order []Resource
}

func (rec *recorder) releaseOf(r Resource) func() error {
	return func() error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.order = append(rec.order, r)
		return nil
	}
}

func (rec *recorder) seen() []Resource {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]Resource(nil), rec.order...)
}

func sameResources(a, b []Resource) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAScopeGivesResourcesBackInTheReverseOfTheOrderItTookThem(t *testing.T) {
	t.Parallel()

	var rec recorder
	var s Scope
	taken := []Resource{WorkerSlot, TunnelPort, TunnelProcess, Worktree, AgentSession}
	for _, r := range taken {
		s.Hold(r, rec.releaseOf(r))
	}

	rep := s.Close(Reclaim)

	want := []Resource{AgentSession, Worktree, TunnelProcess, TunnelPort, WorkerSlot}
	if got := rec.seen(); !sameResources(got, want) {
		t.Errorf("gave back in order %v, want %v", got, want)
	}
	if !sameResources(rep.Released, want) {
		t.Errorf("report said %v, want %v", rep.Released, want)
	}
	if len(rep.Kept) != 0 || len(rep.Failures) != 0 {
		t.Errorf("a reclaim kept %v and failed on %v; it must do neither", rep.Kept, rep.Failures)
	}
}

func TestAScopeKeepsWhatTheDispositionKeepsAndDisarmsIt(t *testing.T) {
	t.Parallel()

	// Disarming is the point. If a kept lease stayed armed, a later deferred
	// Release would kill the session the run just decided to leave standing.
	var rec recorder
	var s Scope
	s.Hold(WorkerSlot, rec.releaseOf(WorkerSlot))
	wt := s.Hold(Worktree, rec.releaseOf(Worktree))
	sess := s.Hold(AgentSession, rec.releaseOf(AgentSession))

	rep := s.Close(Survive)

	if got := rec.seen(); !sameResources(got, []Resource{WorkerSlot}) {
		t.Errorf("a surviving run gave back %v, want only the worker slot", got)
	}
	if !sameResources(rep.Kept, []Resource{AgentSession, Worktree}) {
		t.Errorf("report kept %v, want the session then the worktree", rep.Kept)
	}
	if !sameResources(rep.Released, []Resource{WorkerSlot}) {
		t.Errorf("report released %v, want the worker slot", rep.Released)
	}

	for _, l := range []*Lease{wt, sess} {
		if !l.spent() {
			t.Errorf("kept lease on %s is still armed", l.Resource())
		}
		if err := l.Release(); err != nil {
			t.Errorf("Release on the disarmed %s lease returned %v", l.Resource(), err)
		}
	}
	if got := rec.seen(); !sameResources(got, []Resource{WorkerSlot}) {
		t.Errorf("releasing a disarmed lease gave back %v; a kept resource must stay standing", got)
	}
}

func TestClosingAScopeTwiceGivesNothingBackTwice(t *testing.T) {
	t.Parallel()

	// A deferred Close and an explicit one on the success path must not fight.
	var rec recorder
	var s Scope
	s.Hold(Worktree, rec.releaseOf(Worktree))

	first := s.Close(Reclaim)
	second := s.Close(Reclaim)

	if !sameResources(first.Released, []Resource{Worktree}) {
		t.Errorf("first Close released %v, want the worktree", first.Released)
	}
	if len(second.Released) != 0 || len(second.Kept) != 0 || len(second.Failures) != 0 {
		t.Errorf("second Close reported %+v, want an empty report", second)
	}
	if got := rec.seen(); len(got) != 1 {
		t.Errorf("two Closes made %d give-back calls, want 1", len(got))
	}
}

func TestAResourceGivenBackEarlyIsNotReportedByTheClose(t *testing.T) {
	t.Parallel()

	// The spawn slot goes back when the agent reports ready, not at run end,
	// and the hook session goes back at four sites inside the run.
	var rec recorder
	var s Scope
	spawn := s.Hold(SpawnSlot, rec.releaseOf(SpawnSlot))
	s.Hold(Worktree, rec.releaseOf(Worktree))

	if err := spawn.Release(); err != nil {
		t.Fatalf("early Release: %v", err)
	}
	rep := s.Close(Reclaim)

	if !sameResources(rep.Released, []Resource{Worktree}) {
		t.Errorf("Close released %v, want only the worktree", rep.Released)
	}
	if len(rep.Kept) != 0 {
		t.Errorf("Close kept %v, want nothing", rep.Kept)
	}
	if got := rec.seen(); !sameResources(got, []Resource{SpawnSlot, Worktree}) {
		t.Errorf("give-back calls %v, want the spawn slot early then the worktree", got)
	}
}

func TestANestedScopeClosesBeforeThePerRunResourcesItSitsInside(t *testing.T) {
	t.Parallel()

	// A graph run holds one tunnel and one worktree for the whole run while it
	// takes and gives back one hook session and one agent session per node.
	var rec recorder
	var run Scope
	run.Hold(TunnelProcess, rec.releaseOf(TunnelProcess))
	run.Hold(Worktree, rec.releaseOf(Worktree))

	node := run.Nest()
	node.Hold(HookSession, rec.releaseOf(HookSession))
	node.Hold(AgentSession, rec.releaseOf(AgentSession))

	rep := run.Close(Reclaim)

	want := []Resource{AgentSession, HookSession, Worktree, TunnelProcess}
	if got := rec.seen(); !sameResources(got, want) {
		t.Errorf("gave back in order %v, want %v", got, want)
	}
	if !sameResources(rep.Released, want) {
		t.Errorf("report released %v, want %v", rep.Released, want)
	}
}

func TestANodeThatClosesItsOwnScopeLeavesTheRunScopeNothingToDo(t *testing.T) {
	t.Parallel()

	var rec recorder
	var run Scope
	run.Hold(Worktree, rec.releaseOf(Worktree))

	node := run.Nest()
	node.Hold(AgentSession, rec.releaseOf(AgentSession))
	nodeRep := node.Close(Reclaim)

	runRep := run.Close(Reclaim)

	if !sameResources(nodeRep.Released, []Resource{AgentSession}) {
		t.Errorf("the node released %v, want the agent session", nodeRep.Released)
	}
	if !sameResources(runRep.Released, []Resource{Worktree}) {
		t.Errorf("the run released %v, want only the worktree", runRep.Released)
	}
	if got := rec.seen(); !sameResources(got, []Resource{AgentSession, Worktree}) {
		t.Errorf("give-back calls %v, want the session then the worktree", got)
	}
}

func TestANestedScopeTakesTheParentsDisposition(t *testing.T) {
	t.Parallel()

	var rec recorder
	var run Scope
	run.Hold(Worktree, rec.releaseOf(Worktree))
	node := run.Nest()
	node.Hold(HookSession, rec.releaseOf(HookSession))
	node.Hold(SpawnSlot, rec.releaseOf(SpawnSlot))

	rep := run.Close(Survive)

	if got := rec.seen(); !sameResources(got, []Resource{SpawnSlot}) {
		t.Errorf("a surviving run gave back %v, want only the spawn slot", got)
	}
	if !sameResources(rep.Kept, []Resource{HookSession, Worktree}) {
		t.Errorf("report kept %v, want the hook session then the worktree", rep.Kept)
	}
}

func TestOneFailedReleaseDoesNotStopTheRestFromComingBack(t *testing.T) {
	t.Parallel()

	// Giving back the remaining resources matters more than the one that stuck.
	var rec recorder
	stuck := errors.New("tunnel process will not die")
	var s Scope
	s.Hold(WorkerSlot, rec.releaseOf(WorkerSlot))
	s.Hold(TunnelProcess, func() error { return stuck })
	s.Hold(Worktree, rec.releaseOf(Worktree))

	rep := s.Close(Reclaim)

	if got := rec.seen(); !sameResources(got, []Resource{Worktree, WorkerSlot}) {
		t.Errorf("gave back %v, want the worktree then the worker slot", got)
	}
	if !sameResources(rep.Released, []Resource{Worktree, WorkerSlot}) {
		t.Errorf("report released %v, want the two that succeeded", rep.Released)
	}
	if len(rep.Failures) != 1 || rep.Failures[0].Resource != TunnelProcess {
		t.Fatalf("report failures %v, want one on the tunnel process", rep.Failures)
	}
	if !errors.Is(rep.Err(), stuck) {
		t.Errorf("Report.Err() = %v, want it to carry %v", rep.Err(), stuck)
	}
}

func TestACleanReportCarriesNoError(t *testing.T) {
	t.Parallel()

	var s Scope
	s.Hold(Worktree, func() error { return nil })

	if err := s.Close(Reclaim).Err(); err != nil {
		t.Errorf("Report.Err() = %v on a clean close, want nil", err)
	}
}

func TestAResourceTakenAfterTheScopeClosedIsGivenBackAtOnce(t *testing.T) {
	t.Parallel()

	// Taking a resource after its scope closed is a caller mistake. Holding it
	// forever in a stack nobody will walk again is the worse answer to it.
	var rec recorder
	var s Scope
	s.Close(Reclaim)

	late := s.Hold(AgentSession, rec.releaseOf(AgentSession))

	if got := rec.seen(); !sameResources(got, []Resource{AgentSession}) {
		t.Errorf("a late lease gave back %v, want the agent session", got)
	}
	if !late.spent() {
		t.Error("a late lease is still armed")
	}
}

func TestALateResourceIsGivenBackUnderTheDispositionTheCloseUsed(t *testing.T) {
	t.Parallel()

	var rec recorder
	var s Scope
	s.Close(Survive)

	s.Hold(AgentSession, rec.releaseOf(AgentSession))
	s.Hold(WorkerSlot, rec.releaseOf(WorkerSlot))

	if got := rec.seen(); !sameResources(got, []Resource{WorkerSlot}) {
		t.Errorf("late leases gave back %v, want only the worker slot", got)
	}
}

func TestAnEmptyScopeClosesCleanly(t *testing.T) {
	t.Parallel()

	var s Scope
	rep := s.Close(Reclaim)
	if len(rep.Released) != 0 || len(rep.Kept) != 0 || len(rep.Failures) != 0 {
		t.Errorf("an empty scope reported %+v, want an empty report", rep)
	}
}

func TestAScopeSurvivesConcurrentHolds(t *testing.T) {
	t.Parallel()

	var rec recorder
	var s Scope
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Hold(HookSession, rec.releaseOf(HookSession))
		}()
	}
	wg.Wait()

	rep := s.Close(Reclaim)

	if len(rep.Released) != 32 {
		t.Errorf("32 concurrent Holds gave back %d resources, want 32", len(rep.Released))
	}
	if len(rec.seen()) != 32 {
		t.Errorf("%d give-back calls, want 32", len(rec.seen()))
	}
}

func TestASecondCloseDoesNotChangeTheAnswerTheFirstOneGave(t *testing.T) {
	t.Parallel()

	// A deferred Close and an explicit one can carry different dispositions —
	// the deferred one usually carries the run's real answer and the explicit
	// one a default. The first close is the run's decision, and a later caller
	// must not be able to replace it for anything taken afterwards.
	var rec recorder
	var s Scope

	s.Close(Survive)
	s.Close(Reclaim)

	late := s.Hold(AgentSession, rec.releaseOf(AgentSession))

	if got := rec.seen(); len(got) != 0 {
		t.Errorf("a late session was given back as %v; the first close said survive", got)
	}
	if !late.spent() {
		t.Error("a late lease under a keeping disposition is still armed")
	}
}

func TestAGiveBackCallReachesTheWorldOutsideTheScopeLock(t *testing.T) {
	t.Parallel()

	// A give-back call kills a process or removes a directory. Holding the
	// scope lock across it would hold it for an unbounded time (principle 6),
	// and a call that reaches back into its own scope would deadlock outright.
	// This test reaches back, so the property is pinned rather than assumed.
	var s Scope
	reached := make(chan struct{})
	s.Hold(Worktree, func() error {
		s.Hold(WorkerSlot, func() error { return nil })
		close(reached)
		return nil
	})

	done := make(chan Report, 1)
	go func() { done <- s.Close(Reclaim) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return: the give-back call ran under the scope lock")
	}
	select {
	case <-reached:
	default:
		t.Error("the give-back call never ran")
	}
}

func TestReportErrCarriesEveryFailure(t *testing.T) {
	t.Parallel()

	first := errors.New("tunnel process will not die")
	second := errors.New("worktree busy")
	var s Scope
	s.Hold(TunnelProcess, func() error { return first })
	s.Hold(Worktree, func() error { return second })

	rep := s.Close(Reclaim)

	if len(rep.Failures) != 2 {
		t.Fatalf("report carried %d failures, want 2", len(rep.Failures))
	}
	err := rep.Err()
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Errorf("Report.Err() = %v, want both %v and %v", err, first, second)
	}
	if got := rep.Failures[0].Error(); got == "" {
		t.Error("a Failure has no readable message")
	}
}

func TestAResourceGivenBackEarlyIsNotReportedAsKept(t *testing.T) {
	t.Parallel()

	// The hook session can go back early on a run that then decides to survive.
	// It is already back, so the close neither keeps it nor gives it back — a
	// report that claimed it was kept would say a resource is standing when it
	// is not.
	var rec recorder
	var s Scope
	hook := s.Hold(HookSession, rec.releaseOf(HookSession))
	s.Hold(AgentSession, rec.releaseOf(AgentSession))

	if err := hook.Release(); err != nil {
		t.Fatalf("early Release: %v", err)
	}
	rep := s.Close(Survive)

	if !sameResources(rep.Kept, []Resource{AgentSession}) {
		t.Errorf("report kept %v, want only the agent session", rep.Kept)
	}
	if len(rep.Released) != 0 {
		t.Errorf("report released %v, want nothing", rep.Released)
	}
}
