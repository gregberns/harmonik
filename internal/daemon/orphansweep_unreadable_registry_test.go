package daemon

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

type badRegistryReclaim int

const (
	badRegistryViaDeadLease badRegistryReclaim = iota

	badRegistryViaAgePrune
)

const badRegistryWorktreeUnusedFor = 30 * 24 * time.Hour

const (
	badRegistryLiveRunID      = "0f0e0d0c-0b0a-4908-8706-05040302dd01"
	badRegistryAbandonedRunID = "0f0e0d0c-0b0a-4908-8706-05040302dd02"
)

const badRegistryTornRecord = "0199aaaa-0000-7000-8000-0000000000ff.json"

type badRegistryOutcome struct {
	liveSession      string
	abandonedSession string
	liveWorktree     string
	abandonedWT      string
	server           *surviveRecoverySessions
	adapter          *surviveRecoveryPanePIDs
}

func (o badRegistryOutcome) sessionKilled(name string) bool {
	return o.server.wasKilled(name) || o.adapter.wasKilled(name)
}

func badRegistryOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

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
	for i := len(paths) - 1; i >= 0; i-- {
		if err := os.Chtimes(paths[i], unused, unused); err != nil {
			t.Fatalf("badRegistry: backdate %s: %v", paths[i], err)
		}
	}
}

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
		if _, scanErr := runpkg.ScanRegistry(repo); scanErr == nil {
			t.Fatalf("badRegistry: the registry at %s still reads cleanly with %s in it.\n"+
				"This test is about what the sweep does when the scan FAILS, and here it did not.",
				repo, badRegistryTornRecord)
		}
	}

	adapter := &surviveRecoveryPanePIDs{live: map[string]int{
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
