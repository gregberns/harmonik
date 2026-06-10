package daemon_test

// mergetomain_hksvieq_test.go — integration test for the non-FF push retry
// introduced by hk-svieq.
//
// Scenario: the daemon completes its local rebase + update-ref, then attempts
// to push.  Between the merge preparation and the push an out-of-band commit
// has been pushed directly to the bare-remote by a "captain" process.  The
// first push attempt is rejected with a non-fast-forward error.  The daemon
// must fetch the new remote tip, rebase the run-branch onto it, and retry the
// push successfully — not fail the bead.
//
// Assertions:
//
//	(A) CloseBead called exactly once (bead succeeds despite initial push failure).
//	(B) ReopenBead never called.
//	(C) refs/heads/main advanced past mainSHABefore.
//	(D) The final push reached the bare remote (remote's main matches local's new main).
//	(E) run_completed{success:true} is present in the event stream.
//
// Bead: hk-svieq.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// mergeToMainNonFFRetry_advanceOriginOutOfBand pushes a commit directly to the
// bare originDir without going through the daemon's local projectDir.  This
// simulates a captain out-of-band cherry-pick or a concurrent peer bead that
// pushed to origin while this bead's merge was being prepared.
func mergeToMainNonFFRetry_advanceOriginOutOfBand(t *testing.T, originDir string) {
	t.Helper()
	// Clone origin into a temp directory, commit a diverging file, push back.
	cloneDir := t.TempDir()
	cloneCmd := exec.CommandContext(t.Context(), "git", "clone", originDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("mergeToMainNonFFRetry_advanceOriginOutOfBand: git clone: %v\n%s", err, out)
	}
	// Configure identity inside the clone.
	for _, args := range [][]string{
		{"config", "user.email", "daemon@harmonik.local"},
		{"config", "user.name", "Harmonik Test"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = cloneDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("mergeToMainNonFFRetry_advanceOriginOutOfBand: git %v: %v\n%s", args, err, out)
		}
	}
	// Commit a file that does NOT conflict with the agent's work.txt.
	captainFile := filepath.Join(cloneDir, "captain.txt")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(captainFile, []byte("captain out-of-band push\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainNonFFRetry_advanceOriginOutOfBand: WriteFile: %v", err)
	}
	for _, args := range [][]string{
		{"add", "captain.txt"},
		{"commit", "-m", "chore: captain out-of-band advance"},
		{"push", "origin", "main"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = cloneDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("mergeToMainNonFFRetry_advanceOriginOutOfBand: git %v: %v\n%s", args, err, out)
		}
	}
}

// TestMergeToMain_NonFFPushRetry verifies that the daemon recovers from a
// non-fast-forward push rejection by fetching, rebasing, and retrying.
//
// The bare-remote origin is advanced out-of-band BEFORE the daemon starts, so
// every push attempt against that remote will initially be non-FF.  After the
// daemon fetches and rebases, the push succeeds.
//
// Spec refs: specs/execution-model.md §4.12 EM-052.
// Bead: hk-svieq.
func TestMergeToMain_NonFFPushRetry(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-nonff-push-retry-bead-svieq")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote (origin).
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// Wire projectDir's remote and prime origin with the initial main commit.
	primeRemoteCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	primeRemoteCmd.Dir = projectDir
	if out, err := primeRemoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	primePushCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	primePushCmd.Dir = projectDir
	if out, err := primePushCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (prime): %v\n%s", err, out)
	}

	// OUT-OF-BAND advance: push a commit to origin that the daemon's local
	// projectDir does NOT know about yet.  The daemon will discover this on its
	// fetch retry.
	mergeToMainNonFFRetry_advanceOriginOutOfBand(t, originDir)

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeToMainCommittingFactory(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the bead to be closed or reopened, or test timeout.
	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (A): CloseBead called exactly once (no reopen). ────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (non-FF push retry should succeed)", got)
	}

	// ── Assertion (B): ReopenBead never called. ───────────────────────────────
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0; reason = %q", got, ledger.getReopenReason())
	}

	// ── Assertion (C): refs/heads/main advanced. ─────────────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main HEAD unchanged after non-FF retry: still %s; want run-branch tip", mainSHABefore)
	}

	// ── Assertion (D): remote's main matches the new local main. ─────────────
	remoteRevCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "main")
	remoteRevCmd.Dir = originDir
	remoteRevOut, remoteRevErr := remoteRevCmd.Output()
	if remoteRevErr != nil {
		t.Fatalf("git rev-parse main (origin): %v", remoteRevErr)
	}
	remoteSHA := strings.TrimRight(string(remoteRevOut), "\n")
	if remoteSHA != mainSHAAfter {
		t.Errorf("origin main = %s; want %s (local main after retry push)", remoteSHA[:8], mainSHAAfter[:8])
	}

	// ── Assertion (E): run_completed{success:true} present. ──────────────────
	runCompletedEvs := mergeToMainFindEvents(collector, "run_completed")
	if len(runCompletedEvs) == 0 {
		t.Error("no run_completed events found")
	} else {
		var m map[string]interface{}
		if err := json.Unmarshal(runCompletedEvs[0].Payload, &m); err != nil {
			t.Fatalf("run_completed payload unmarshal: %v", err)
		}
		if success, _ := m["success"].(bool); !success {
			t.Errorf("run_completed success = false; want true (non-FF retry should succeed)")
		}
	}

	types := mergeToMainEventOrder(collector)
	t.Logf("hk-svieq non-FF push retry OK: main %s → %s, remote %s, events: %v",
		mainSHABefore[:8], mainSHAAfter[:8], remoteSHA[:8], types)
}
