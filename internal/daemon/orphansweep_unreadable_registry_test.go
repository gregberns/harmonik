package daemon

// orphansweep_unreadable_registry_test.go — what the boot sweep is allowed to
// destroy when it cannot read the run registry.
//
// The registry is the only thing that separates a run whose agent is still
// working from a directory a crash left behind. Reading it is all-or-nothing:
// one torn write under .harmonik/runs/ and the scan answers with an error, not
// with the records it managed to parse. The sweep then held an EMPTY exemption
// set, which is byte-for-byte what "no run survived this restart" looks like —
// so it killed every session carrying the project hash and force-removed every
// worktree whose lease named the dead daemon, and reported a clean sweep over
// an agent's session and its uncommitted work.
//
// The claim these tests hold between them: a boot that cannot prove a run is
// dead destroys nothing, and a boot that can still reaps what is dead. The
// second one is what stops the first passing for a sweep that has gone inert.
//
// # Two ways a worktree is reclaimed, and both need covering
//
// The sweep can take a worktree down two different roads, and each has its own
// stand-down. A worktree whose lease names a dead process goes to the
// force-removal of pass (b2). A worktree with NO lease that nothing has touched
// for a week goes to the age prune of pass (b3), and the second road is the one
// a surviving run travels: the lease sweep RELEASES a lease it judged stale
// before it hands the path on, so the run that outlived the daemon reaches the
// NEXT boot holding no lease at all. Age says nothing about it. That checkout is
// old because it is old, not because its agent stopped.
//
// So the fixture drives both, selected by badRegistryReclaim. With only the
// dead-lease drive, the age prune never fires in this file and its gate could be
// deleted without turning anything red.
//
// Helper prefix: badRegistry.

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

// badRegistryReclaim selects which of the sweep's two worktree-reclaim roads the
// fixture puts its worktrees on.
type badRegistryReclaim int

const (
	// badRegistryViaDeadLease leaves a lease naming a process that is gone, which
	// is what pass (b2) force-removes.
	badRegistryViaDeadLease badRegistryReclaim = iota

	// badRegistryViaAgePrune leaves no lease and no recent activity, which is
	// what pass (b3) removes on age alone.
	badRegistryViaAgePrune
)

// badRegistryWorktreeUnusedFor is how far back the age-prune drive backdates a
// worktree. It is well past DefaultHarmonikWorktreeMaxAgeDays so the drive does
// not depend on the exact threshold, only on being over it.
const badRegistryWorktreeUnusedFor = 30 * 24 * time.Hour

// The two runs every drive of this fixture carries. One agent kept working
// after the daemon died; the other went with it.
const (
	badRegistryLiveRunID      = "0f0e0d0c-0b0a-4908-8706-05040302dd01"
	badRegistryAbandonedRunID = "0f0e0d0c-0b0a-4908-8706-05040302dd02"
)

// badRegistryTornRecord is a record file the scan cannot parse: a valid run-id
// basename over a JSON object that stops mid-key. It is what a write that lost
// power halfway leaves on disk, and it is enough to fail the whole scan.
const badRegistryTornRecord = "0199aaaa-0000-7000-8000-0000000000ff.json"

// badRegistryOutcome is one drive of the boot sweep over the fixture, together
// with the fakes that recorded what the sweep did.
type badRegistryOutcome struct {
	liveSession      string
	abandonedSession string
	liveWorktree     string
	abandonedWT      string
	server           *surviveRecoverySessions
	adapter          *surviveRecoveryPanePIDs
}

// sessionKilled asks both kill passes, because either one ends the agent.
func (o badRegistryOutcome) sessionKilled(name string) bool {
	return o.server.wasKilled(name) || o.adapter.wasKilled(name)
}

func badRegistryOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// badRegistryUnleaseAndAge puts a worktree on the age-prune road: it takes the
// lease off and backdates every mtime in the tree.
//
// Both halves are needed. Without a lease the worktree is unclassifiable by the
// lease sweep and lands in the no-lock list; the age prune then walks the tree
// for its NEWEST mtime, so one recent file anywhere under it keeps the whole
// worktree young and the prune never fires.
func badRegistryUnleaseAndAge(t *testing.T, wtPath string) {
	t.Helper()
	if err := workspace.ReleaseLeaseLock(workspace.LeaseLockPath(wtPath)); err != nil {
		t.Fatalf("badRegistry: release the lease on %s: %v", wtPath, err)
	}
	var paths []string
	if walkErr := filepath.WalkDir(wtPath, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}); walkErr != nil {
		t.Fatalf("badRegistry: walk %s to backdate it: %v", wtPath, walkErr)
	}
	unused := time.Now().Add(-badRegistryWorktreeUnusedFor)
	// Deepest first. Backdating a child cannot then re-bump a parent this loop
	// has already done.
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chtimes(paths[i], unused, unused); err != nil {
			t.Fatalf("badRegistry: backdate %s: %v", paths[i], err)
		}
	}
}

// badRegistrySweep builds a project holding one live run and one abandoned run,
// optionally drops an unparseable record beside them, and drives the boot sweep
// over it.
//
// reclaim decides what state the two worktrees are left in, and the two states
// reach the sweep's two different removal passes. Neither state says anything
// about which run is live: a dead lease holder is the DAEMON that took the lease
// and this fixture is the boot after that daemon died, and an old checkout is
// old because it is old. Only the registry tells the two runs apart — which is
// precisely what an unreadable registry takes away.
func badRegistrySweep(t *testing.T, withTornRecord bool, reclaim badRegistryReclaim) badRegistryOutcome {
	t.Helper()

	repo := surviveRunRepo(t)
	headSHA, headErr := resolveHEAD(t.Context(), repo)
	if headErr != nil {
		t.Fatalf("badRegistry: resolve HEAD: %v", headErr)
	}

	stage := func(wtPath string) {
		switch reclaim {
		case badRegistryViaDeadLease:
			wtLeaseOverwriteHolderDead(t, wtPath)
		case badRegistryViaAgePrune:
			badRegistryUnleaseAndAge(t, wtPath)
		}
	}

	liveWorktree, liveCleanup, liveErr := productionWorktreeFactory(t.Context(), repo, badRegistryLiveRunID, headSHA)
	if liveErr != nil {
		t.Fatalf("badRegistry: create the live run's worktree: %v", liveErr)
	}
	t.Cleanup(liveCleanup)
	stage(liveWorktree)

	abandonedWT, abandonedCleanup, abandonedErr := productionWorktreeFactory(t.Context(), repo, badRegistryAbandonedRunID, headSHA)
	if abandonedErr != nil {
		t.Fatalf("badRegistry: create the abandoned run's worktree: %v", abandonedErr)
	}
	t.Cleanup(abandonedCleanup)
	stage(abandonedWT)

	liveSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-0f0e0d0cdd01")
	abandonedSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-0f0e0d0cdd02")
	for _, rec := range []runpkg.Record{
		{
			SchemaVersion: 1,
			RunID:         badRegistryLiveRunID,
			BeadID:        "hk-still-being-worked",
			SessionName:   liveSession,
			StartedAt:     time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
		},
		{
			SchemaVersion: 1,
			RunID:         badRegistryAbandonedRunID,
			BeadID:        "hk-agent-already-exited",
			SessionName:   abandonedSession,
			StartedAt:     time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
		},
	} {
		if writeErr := runpkg.Write(repo, rec); writeErr != nil {
			t.Fatalf("badRegistry: write the record for run %s: %v", rec.RunID, writeErr)
		}
	}

	if withTornRecord {
		tornPath := filepath.Join(repo, ".harmonik", "runs", badRegistryTornRecord)
		if writeErr := os.WriteFile(tornPath, []byte(`{"schema_version":`), 0o600); writeErr != nil {
			t.Fatalf("badRegistry: write the torn record: %v", writeErr)
		}
		// Harness check, not decoration. If the scan still succeeds, this drive is
		// measuring the ordinary path and every assertion below is free.
		if _, scanErr := runpkg.ScanRegistry(repo); scanErr == nil {
			t.Fatalf("badRegistry: the registry at %s still reads cleanly with %s in it.\n"+
				"This test is about what the sweep does when the scan FAILS, and here it did not.",
				repo, badRegistryTornRecord)
		}
	}

	adapter := &surviveRecoveryPanePIDs{live: map[string]int{
		// The one PID a test can name that is certainly alive.
		liveSession: os.Getpid(),
		// Nothing is running in there any more.
		abandonedSession: 0,
	}}
	server := &surviveRecoverySessions{names: []string{liveSession, abandonedSession}}

	if _, sweepErr := RunOrphanSweep(t.Context(), repo, surviveRecoveryHash, time.Now(),
		OrphanSweepConfig{
			TmuxAdapter:   adapter,
			TmuxLister:    server,
			TmuxKiller:    server,
			HandlerLister: surviveRecoveryNoProcesses{},
			BrLister:      surviveRecoveryNoProcesses{},
		}); sweepErr != nil {
		// The sweep reports its non-fatal step failures this way and the daemon
		// proceeds anyway (PL-006). What it did is what these tests are about.
		t.Logf("RunOrphanSweep reported non-fatal step errors: %v", sweepErr)
	}

	return badRegistryOutcome{
		liveSession:      liveSession,
		abandonedSession: abandonedSession,
		liveWorktree:     liveWorktree,
		abandonedWT:      abandonedWT,
		server:           server,
		adapter:          adapter,
	}
}

// TestBootSweep_AnUnreadableRunRegistryDestroysNothing is the guard.
//
// One unparseable file under .harmonik/runs/ fails the whole scan, and the
// sweep used to read that failure as "nothing is live". It is not: it is the
// boot not knowing. Acting on it killed the tmux session of an agent that was
// still typing and ran `git worktree remove --force --force` on the checkout it
// was typing into, then reported the counts as a successful sweep.
//
// So the destructive passes stand down for the boot. The leak that costs is an
// orphan session or directory that survives until the registry is repaired, and
// anyone can clean that up at any later time. The other outcome is uncommitted
// work that no later boot can give back.
func TestBootSweep_AnUnreadableRunRegistryDestroysNothing(t *testing.T) {
	t.Parallel()

	out := badRegistrySweep(t, true, badRegistryViaDeadLease)

	if out.sessionKilled(out.liveSession) {
		t.Errorf("the boot sweep killed %q, the session of a run whose agent is still working.\n"+
			"The scan of .harmonik/runs/ failed, so the exemption set was empty — which reads "+
			"exactly like a restart no run survived. The sweep cannot tell those two apart, and "+
			"the one it guessed ends an agent mid-edit.", out.liveSession)
	}
	if !badRegistryOnDisk(out.liveWorktree) {
		t.Errorf("the boot sweep removed %s, the worktree of a run whose agent is still working in it.\n"+
			"Its lease names the daemon that took it and that daemon is gone, which is what every "+
			"surviving run looks like. The registry is the only thing that says otherwise, and this "+
			"boot could not read it.", out.liveWorktree)
	}

	// The abandoned run is spared too, and that is the trade rather than a miss:
	// with the registry unreadable there is no fact that separates it from the
	// live one. It leaks until the next boot reads a repaired registry.
	if out.sessionKilled(out.abandonedSession) || !badRegistryOnDisk(out.abandonedWT) {
		t.Errorf("the boot sweep reaped the abandoned run (session killed: %v, worktree gone: %v) "+
			"while it could not read the registry.\n"+
			"Nothing in this state distinguishes that run from the live one, so a sweep that acts "+
			"on one is acting on both.",
			out.sessionKilled(out.abandonedSession), !badRegistryOnDisk(out.abandonedWT))
	}
}

// TestBootSweep_AReadableRunRegistryStillReapsTheDeadRun is the control, and
// without it the test above passes for a sweep that stopped working entirely.
//
// Same fixture, same two runs, same dead-process leases — only the unparseable
// file is missing. The abandoned run's session MUST be killed and its worktree
// MUST be removed, so that "the live run survived" over there is the stand-down
// doing its job rather than a sweep that reaps nothing at all.
func TestBootSweep_AReadableRunRegistryStillReapsTheDeadRun(t *testing.T) {
	t.Parallel()

	out := badRegistrySweep(t, false, badRegistryViaDeadLease)

	if !out.sessionKilled(out.abandonedSession) {
		t.Errorf("the sweep left %q standing with nothing running in it.\n"+
			"The kill passes are inert in this fixture, so the unreadable-registry test proves "+
			"nothing: a sweep that never kills anything protects a live run for free.",
			out.abandonedSession)
	}
	if badRegistryOnDisk(out.abandonedWT) {
		t.Errorf("the sweep left the abandoned run's worktree at %s on disk.\n"+
			"The reclaim is inert in this fixture, so the unreadable-registry test proves nothing "+
			"about the worktree it claims to protect.", out.abandonedWT)
	}

	if out.sessionKilled(out.liveSession) {
		t.Errorf("the sweep killed %q, and the registry it needed was readable.", out.liveSession)
	}
	if !badRegistryOnDisk(out.liveWorktree) {
		t.Errorf("the sweep removed %s, and the registry it needed was readable.", out.liveWorktree)
	}
}

// TestBootSweep_AnUnreadableRunRegistrySparesAnUnleasedAgedWorktree is the same
// guard as the first test, on the OTHER road into `git worktree remove`.
//
// This is the state a surviving run is actually in on the second boot after its
// daemon died. The first boot's lease sweep judged the lease stale and RELEASED
// it, so there is no lease left to hold anything back, and the checkout is old
// because the agent has been editing files in place rather than creating them.
// Age is then the only fact the prune has, and age says nothing about whether an
// agent is still there.
//
// With the registry unreadable there is no second fact either. The prune stands
// down for the boot rather than force-removing a checkout it cannot vouch for.
func TestBootSweep_AnUnreadableRunRegistrySparesAnUnleasedAgedWorktree(t *testing.T) {
	t.Parallel()

	out := badRegistrySweep(t, true, badRegistryViaAgePrune)

	if !badRegistryOnDisk(out.liveWorktree) {
		t.Errorf("the age prune removed %s, the worktree of a run whose agent is still working in it.\n"+
			"It holds no lease because a previous boot released it, and it is old because the agent "+
			"edits files in place. Both are true of every long-running agent. The registry is the "+
			"only thing that says the agent is there, and this boot could not read it.", out.liveWorktree)
	}

	// The abandoned worktree is spared too, and that is the trade rather than a
	// miss: with the registry unreadable, nothing separates it from the live one.
	if !badRegistryOnDisk(out.abandonedWT) {
		t.Errorf("the age prune removed the abandoned run's worktree at %s while it could not read "+
			"the registry.\n"+
			"Nothing in this state distinguishes that worktree from the live one, so a prune that "+
			"acts on one is acting on both.", out.abandonedWT)
	}
}

// TestBootSweep_AReadableRunRegistryStillAgePrunesTheAbandonedWorktree is the
// control for the test above, and it is what makes that gate load-bearing.
//
// Same fixture, same two unleased and aged worktrees — only the unparseable file
// is missing. The abandoned worktree MUST go, through the age prune, because in
// this fixture there is no lease left for the force-removal of pass (b2) to act
// on. Without this the guard above passes for a prune that never removes
// anything, and the `liveRunsKnown` gate on that branch could be deleted with
// nothing turning red.
func TestBootSweep_AReadableRunRegistryStillAgePrunesTheAbandonedWorktree(t *testing.T) {
	t.Parallel()

	out := badRegistrySweep(t, false, badRegistryViaAgePrune)

	if badRegistryOnDisk(out.abandonedWT) {
		t.Errorf("the age prune left the abandoned run's worktree at %s on disk.\n"+
			"It has no lease and nothing has touched it for %v, which is the whole input that pass "+
			"has. The prune is inert in this fixture, so the unreadable-registry test next to it "+
			"proves nothing about the worktree it claims to protect.",
			out.abandonedWT, badRegistryWorktreeUnusedFor)
	}

	if !badRegistryOnDisk(out.liveWorktree) {
		t.Errorf("the age prune removed %s, and the registry it needed was readable.\n"+
			"The run's session is live, so the registry names it and the prune must hold it back.",
			out.liveWorktree)
	}
}
