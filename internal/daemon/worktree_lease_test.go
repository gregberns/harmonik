package daemon

// worktree_lease_test.go — a run's worktree says who holds it, and the boot
// sweep believes it.
//
// The lease file under a worktree's .harmonik/ is what separates "an agent is
// working in here" from "a crash left this behind". The sweep asks whether the
// process named in the lease is still running: alive means keep, dead means the
// directory is reclaimable and the step after force-removes it.
//
// Nothing wrote that file. Every worktree therefore classified as unleased, the
// sweep's primary protection had no input, and the only thing left between a
// run's checkout and `git worktree remove --force --force` was the age proxy —
// which was written as a backstop and had quietly become the whole guard.
//
// The second test here is the one that has to exist because of the first. A run
// that outlives the daemon holds a lease naming the daemon's process, and that
// process is gone. Read literally the lease then says "reclaim me" about a
// directory a live agent is working in. Writing the lease without that guard
// would have traded a slow leak for destroyed work.
//
// Helper prefix: wtLease.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

// wtLeaseNoSuchProcess is a process id no host has. It is above every
// configurable PID ceiling on the platforms this runs on, so kill(pid, 0)
// answers "no such process" without the test having to create and reap one and
// hope the number is not reused.
const wtLeaseNoSuchProcess = 2147483646

// wtLeaseRunID is the run every worktree in this file belongs to. A worktree
// directory is named after its run, which is how the sweep matches one to the
// other.
const wtLeaseRunID = "0f0e0d0c-0b0a-0908-0706-0504030201ff"

// wtLeaseWorktree makes a real git repository, creates the run's worktree
// through the PRODUCTION factory, and returns the repository and worktree paths.
//
// It goes through the factory rather than through git directly because the claim
// under test is about what the factory leaves behind, not about what a lease file
// looks like.
func wtLeaseWorktree(t *testing.T) (repo, wtPath string) {
	t.Helper()
	repo = surviveRunRepo(t)
	headSHA, headErr := resolveHEAD(t.Context(), repo)
	if headErr != nil {
		t.Fatalf("wtLease: resolve HEAD: %v", headErr)
	}
	wtPath, cleanup, err := productionWorktreeFactory(t.Context(), repo, wtLeaseRunID, headSHA)
	if err != nil {
		t.Fatalf("wtLease: create the run's worktree: %v", err)
	}
	t.Cleanup(cleanup)
	return repo, wtPath
}

// TestWorktreeLease_ACreatedWorktreeIsHeldRatherThanUnleased is the write half.
//
// The sweep sorts every worktree into one of three answers, and the difference
// between them is the whole point of the lease: held by a live process, held by
// a dead one, or not held at all. A run's fresh worktree must land in the first.
// It landed in the third — for every worktree, on every boot — because nothing
// wrote the file the sweep reads.
//
// The two negatives are checked separately because they are different failures.
// Unleased is the defect this test was written for. Reclaimable is the shape a
// lease with a wrong or invented process id produces, and it is worse than
// unleased: the age proxy does not cover it, so the very next boot force-removes
// a worktree the run is using.
func TestWorktreeLease_ACreatedWorktreeIsHeldRatherThanUnleased(t *testing.T) {
	t.Parallel()

	repo, wtPath := wtLeaseWorktree(t)

	leasePath := workspace.LeaseLockPath(wtPath)
	if _, err := os.Stat(leasePath); err != nil {
		t.Fatalf("no lease at %s: %v\n"+
			"The sweep reads this file and nothing else to decide whether a worktree is in use. "+
			"Without it the run's checkout is indistinguishable from an abandoned directory.",
			leasePath, err)
	}

	result, err := workspace.SweepStaleLeaseLocks(t.Context(), repo, workspace.NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("sweep the lease locks: %v", err)
	}

	if wtLeaseContains(result.NoLock, wtPath) {
		t.Errorf("the sweep classified %s as unleased.\n"+
			"That is the state every worktree was in while nothing wrote a lease. An unleased "+
			"worktree is protected only by the age proxy, which was written as a second line "+
			"and cannot be the only one.", wtPath)
	}
	if wtLeaseContains(result.Removed, wtPath) {
		t.Errorf("the sweep classified %s as reclaimable and released its lease.\n"+
			"The run holds it and this process is alive, so the lease must name a live process. "+
			"A lease naming a process that is not running is worse than none: the caller "+
			"force-removes the directory on the next boot.", wtPath)
	}
	if !wtLeaseContains(result.Skipped, wtPath) {
		t.Errorf("the sweep did not examine %s at all.\n"+
			"It reached neither the held nor the reclaimable answer, so this test is measuring "+
			"a worktree the sweep cannot see rather than the classification it makes.", wtPath)
	}
}

// TestWorktreeLease_ADeadHolderReleasesTheLeaseAndTheWorktreeGoes is the control
// for the test above, and it is what stops that one passing for a sweep that
// never reclaims anything.
//
// The lease here names a process that does not exist, which is what a run whose
// daemon and agent both died leaves behind. The sweep must release it, and the
// directory must be offered up for removal. That reclaim is the whole reason the
// lease exists, and the guard added for surviving runs must not blunt it.
func TestWorktreeLease_ADeadHolderReleasesTheLeaseAndTheWorktreeGoes(t *testing.T) {
	t.Parallel()

	repo, wtPath := wtLeaseWorktree(t)
	wtLeaseOverwriteHolder(t, wtPath, wtLeaseNoSuchProcess)

	result, err := workspace.SweepStaleLeaseLocks(t.Context(), repo, workspace.NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("sweep the lease locks: %v", err)
	}

	if !wtLeaseContains(result.Removed, wtPath) {
		t.Errorf("the sweep left the lease on %s standing, though the process holding it does "+
			"not exist.\n"+
			"Nothing then reclaims the directory, and a run that died mid-flight keeps its "+
			"checkout for ever.", wtPath)
	}
}

// TestBootSweep_DoesNotRemoveTheWorktreeOfARunThatOutlivedTheDaemon is the guard
// the write half made necessary.
//
// The lease names the daemon, because the daemon is what takes it. A run in a
// tmux session of its own outlives that daemon, so on the next boot the lease
// names a process that is gone while the agent named nowhere in it is still
// working. Read literally, the sweep reclaims a directory that is in active use
// and `git worktree remove --force --force` takes every uncommitted change with
// it.
//
// The run registry is what says otherwise, and it is the same source the session
// exclusion reads a few lines earlier.
//
// The second worktree is the control. "The live run's worktree survived" is a
// claim that something did NOT happen, which is free in a sweep that removed
// nothing. The abandoned run goes through the same call with the same registry
// and the same dead-process lease, and its directory IS removed.
func TestBootSweep_DoesNotRemoveTheWorktreeOfARunThatOutlivedTheDaemon(t *testing.T) {
	t.Parallel()

	repo := surviveRunRepo(t)
	headSHA, headErr := resolveHEAD(t.Context(), repo)
	if headErr != nil {
		t.Fatalf("wtLease: resolve HEAD: %v", headErr)
	}

	// The run whose agent kept working after the daemon died.
	const liveRun = "0f0e0d0c-0b0a-0908-0706-05040302aa01"
	liveWT, liveCleanup, err := productionWorktreeFactory(t.Context(), repo, liveRun, headSHA)
	if err != nil {
		t.Fatalf("wtLease: create the live run's worktree: %v", err)
	}
	defer liveCleanup()
	// Both leases name a process that is gone, which is what a daemon restart
	// leaves behind. The registry is the only thing that tells the two runs apart.
	wtLeaseOverwriteHolder(t, liveWT, wtLeaseNoSuchProcess)

	// The run whose agent went with the daemon. Nothing is working in here.
	const abandonedRun = "0f0e0d0c-0b0a-0908-0706-05040302aa02"
	abandonedWT, abandonedCleanup, err := productionWorktreeFactory(t.Context(), repo, abandonedRun, headSHA)
	if err != nil {
		t.Fatalf("wtLease: create the abandoned run's worktree: %v", err)
	}
	defer abandonedCleanup()
	wtLeaseOverwriteHolder(t, abandonedWT, wtLeaseNoSuchProcess)

	liveSession := lifecycle.TmuxSessionName(surviveRecoveryHash, "run-0f0e0d0c0b0a")
	if writeErr := runpkg.Write(repo, runpkg.Record{
		SchemaVersion: 1,
		RunID:         liveRun,
		BeadID:        "hk-still-being-worked",
		SessionName:   liveSession,
	}); writeErr != nil {
		t.Fatalf("wtLease: write the live run's record: %v", writeErr)
	}

	adapter := &surviveRecoveryPanePIDs{live: map[string]int{liveSession: os.Getpid()}}
	server := &surviveRecoverySessions{names: []string{liveSession}}

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

	if _, statErr := os.Stat(abandonedWT); statErr == nil {
		t.Fatalf("the sweep left the abandoned run's worktree at %s on disk.\n"+
			"The reclaim did not run in this fixture, so the check below would report a "+
			"protected live worktree when the truth is that nothing was removed at all.",
			abandonedWT)
	}

	if _, statErr := os.Stat(liveWT); statErr != nil {
		t.Errorf("the boot sweep removed %s, the worktree of a run whose agent is still "+
			"working in it: %v\n"+
			"The lease on it names the daemon that took it, and that daemon is dead — which is "+
			"exactly what a run surviving a restart looks like. Removing it is not a leak being "+
			"cleaned up, it is the agent's uncommitted work being deleted while it is being "+
			"written.", liveWT, statErr)
	}
}

// wtLeaseOverwriteHolder rewrites the worktree's lease so it names pid.
//
// The file is replaced rather than edited, because the write path refuses to
// take a lease a second time — that refusal is what stops two runs claiming one
// worktree, and it is not what this file is testing.
func wtLeaseOverwriteHolder(t *testing.T, wtPath string, pid int) {
	t.Helper()
	leasePath := workspace.LeaseLockPath(wtPath)
	existing, readErr := os.ReadFile(leasePath) //nolint:gosec // the path is built from a temp dir by this test
	if readErr != nil {
		t.Fatalf("wtLease: read the lease this test is about to rewrite: %v", readErr)
	}
	if len(existing) == 0 {
		t.Fatal("wtLease: the lease is empty, so the run never took one and rewriting it " +
			"measures nothing")
	}
	if err := workspace.ReleaseLeaseLock(leasePath); err != nil {
		t.Fatalf("wtLease: release the lease before rewriting it: %v", err)
	}
	runUUID, parseErr := uuid.Parse(filepath.Base(wtPath))
	if parseErr != nil {
		t.Fatalf("wtLease: the worktree directory %q is not named after a run: %v",
			filepath.Base(wtPath), parseErr)
	}
	if err := workspace.WriteLeaseLockAtomic(leasePath, &core.LeaseLockFile{
		RunID:     core.RunID(runUUID),
		PID:       pid,
		CreatedAt: time.Now().UTC(),
		TTLSec:    worktreeLeaseTTLSec,
	}); err != nil {
		t.Fatalf("wtLease: write the lease naming pid %d: %v", pid, err)
	}
}

func wtLeaseContains(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
