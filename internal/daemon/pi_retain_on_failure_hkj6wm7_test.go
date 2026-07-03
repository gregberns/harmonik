package daemon_test

// pi_retain_on_failure_hkj6wm7_test.go — regression tests for hk-j6wm7.
//
// Bug: a Pi run that fails fast (e.g. the ~4.5s exit0-no-commit against a
// locally-hosted OpenAI-compatible endpoint like ornith) had its run worktree
// AND pi-agent dir removed by the deferred wtCleanup, so there was NO surviving
// evidence of WHY it failed — the fast-fail stdout/stderr was unobservable.
//
// Fix (hk-j6wm7):
//   (A) On a Pi FAILURE the deferred wtCleanup is SKIPPED so the worktree (which
//       contains the pi-agent dir + the captured output) survives for
//       post-mortem inspection. Mirrors the hk-o85ye survive-cleanup gate.
//       Successful Pi runs and all non-Pi runs clean up exactly as before.
//   (B) The Pi child's stdout is TEE'd to <wt>/.harmonik/pi-agent/pi-stdout.log
//       (a COPY — the session-id interceptor still sees every byte) and, on
//       failure, the stderr tail is written to
//       <wt>/.harmonik/pi-agent/pi-stderr.log.
//
// These tests drive a real bead dispatch through ExportedRunWorkLoop with a
// launchSpecBuilder that stamps resolvedAgentType=Pi and a spy worktree factory
// (wrapping the REAL productionWorktreeFactory) that records whether the cleanup
// fired.
//
// Helper prefix: hkj6wm7 (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-j6wm7.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkj6wm7FailScript writes a shell script that models a Pi fast-fail: it emits a
// session-header NDJSON line on stdout, writes a diagnostic to stderr, and exits
// non-zero WITHOUT committing. This is the exit-no-commit shape whose evidence
// must survive.
func hkj6wm7FailScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pi-fast-fail.sh")
	content := "#!/bin/sh\n" +
		"echo '{\"type\":\"session\",\"version\":3,\"id\":\"hkj6wm7-sess\",\"cwd\":\".\"}'\n" +
		"echo 'pi: fast-fail against base_url ornith: api mismatch' 1>&2\n" +
		"exit 1\n"
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("hkj6wm7FailScript: WriteFile: %v", err)
	}
	return path
}

// hkj6wm7SuccessScript writes a shell script that commits a "Refs: <beadID>"
// change and exits 0 — the happy path whose worktree MUST still be cleaned up.
func hkj6wm7SuccessScript(t *testing.T, beadID core.BeadID) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pi-commit-exit.sh")
	content := "#!/bin/sh\n" +
		"set -e\n" +
		"export PATH=" + os.Getenv("PATH") + "\n" +
		"echo '{\"type\":\"session\",\"version\":3,\"id\":\"hkj6wm7-ok\",\"cwd\":\".\"}'\n" +
		"git config user.email test@harmonik.local\n" +
		"git config user.name 'Harmonik Test'\n" +
		"echo hkj6wm7 > .pichange\n" +
		"git add .pichange\n" +
		"MSG=$(printf 'hkj6wm7 pi success\\n\\nRefs: " + string(beadID) + "')\n" +
		"git commit -m \"$MSG\" >/dev/null 2>&1\n"
	//nolint:gosec // G306: test-only script; chmod 0755 required for execution
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("hkj6wm7SuccessScript: WriteFile: %v", err)
	}
	return path
}

// hkj6wm7SpyFactory wraps the real productionWorktreeFactory, recording the
// created worktree path and whether the deferred cleanup fired. The retained
// worktree (fail path) is removed at test end to avoid leaking git worktrees.
type hkj6wm7SpyState struct {
	wtPath       string
	cleanupFired bool
}

func hkj6wm7SpyFactory(t *testing.T, st *hkj6wm7SpyState) func(context.Context, string, string, string) (string, func(), error) {
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wt, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		st.wtPath = wt
		// Ensure the worktree is force-removed at test end even when retained.
		t.Cleanup(func() { cleanup() })
		wrapped := func() {
			st.cleanupFired = true
			cleanup()
		}
		return wt, wrapped, nil
	}
}

func hkj6wm7RunOneBead(t *testing.T, beadID core.BeadID, scriptPath string, st *hkj6wm7SpyState) *stubBeadLedger {
	t.Helper()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Redirect EnsureWorktreeTrust away from ~/.claude.json (SC-1 pattern).
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	ledger := &stubBeadLedger{ready: []core.BeadID{beadID}}
	collector := &stubEventCollector{}

	// HarnessRegistry registers the Pi harness (SessionIDCaptured → exec path +
	// StdoutWrapper fires, enabling stdout tee capture).
	harnessReg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:    ledger,
		Bus:          collector,
		ProjectDir:   projectDir,
		IntentLogDir: filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// Empty adapter registry → waitAgentReady is skipped for the shell fixture.
		AdapterRegistry2:  NewEmptySealedAdapterRegistryForTest(t),
		HarnessRegistry:   harnessReg,
		LaunchSpecBuilder: daemon.ExportedPiProcessExitLaunchSpecBuilder(scriptPath),
		WorktreeFactory:   hkj6wm7SpyFactory(t, st),
		AgentReadyTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	terminalDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(terminalDeadline) {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(10 * time.Second):
		t.Fatal("hkj6wm7: work loop did not exit within 10 s after context cancel")
	}
	return ledger
}

// TestPiRetainOnFailure_WorktreeAndOutputRetained proves that a FAILED Pi run
// (A) does NOT clean up its worktree and (B) leaves the captured stdout + stderr
// on disk for post-mortem inspection.
func TestPiRetainOnFailure_WorktreeAndOutputRetained(t *testing.T) {
	const beadID = core.BeadID("hkj6wm7-pi-fail")
	st := &hkj6wm7SpyState{}
	ledger := hkj6wm7RunOneBead(t, beadID, hkj6wm7FailScript(t), st)

	t.Logf("hkj6wm7 FAIL-case: wtPath=%q cleanupFired=%v closed=%v reopened=%v",
		st.wtPath, st.cleanupFired, ledger.closedIDs(), ledger.reopenedIDs())

	if st.wtPath == "" {
		t.Fatal("hkj6wm7: spy factory never created a worktree (run did not reach launch)")
	}

	// (A) Cleanup must NOT have fired — the worktree is retained on failure.
	if st.cleanupFired {
		t.Error("hkj6wm7 FAIL: wtCleanup fired on a Pi FAILURE — worktree not retained (hk-j6wm7 gate absent)")
	}
	if _, statErr := os.Stat(st.wtPath); statErr != nil {
		t.Errorf("hkj6wm7 FAIL: retained worktree %q does not exist: %v", st.wtPath, statErr)
	}

	// (B) Captured stdout + stderr must survive under the pi-agent dir.
	stdoutPath := filepath.Join(st.wtPath, ".harmonik", "pi-agent", "pi-stdout.log")
	stderrPath := filepath.Join(st.wtPath, ".harmonik", "pi-agent", "pi-stderr.log")

	stdoutBytes, sErr := os.ReadFile(stdoutPath)
	if sErr != nil {
		t.Errorf("hkj6wm7 FAIL: captured pi-stdout.log missing: %v", sErr)
	} else if len(stdoutBytes) == 0 {
		t.Error("hkj6wm7 FAIL: pi-stdout.log is empty — stdout tee captured nothing")
	}

	stderrBytes, eErr := os.ReadFile(stderrPath)
	if eErr != nil {
		t.Errorf("hkj6wm7 FAIL: captured pi-stderr.log missing: %v", eErr)
	} else if len(stderrBytes) == 0 {
		t.Error("hkj6wm7 FAIL: pi-stderr.log is empty — stderr tail captured nothing")
	}
}

// TestPiRetainOnFailure_SuccessStillCleansUp proves the happy path is unchanged:
// a SUCCESSFUL Pi run still removes its worktree (no disk-leak regression).
func TestPiRetainOnFailure_SuccessStillCleansUp(t *testing.T) {
	const beadID = core.BeadID("hkj6wm7-pi-ok")
	st := &hkj6wm7SpyState{}
	ledger := hkj6wm7RunOneBead(t, beadID, hkj6wm7SuccessScript(t, beadID), st)

	t.Logf("hkj6wm7 SUCCESS-case: wtPath=%q cleanupFired=%v closed=%v reopened=%v",
		st.wtPath, st.cleanupFired, ledger.closedIDs(), ledger.reopenedIDs())

	if st.wtPath == "" {
		t.Fatal("hkj6wm7: spy factory never created a worktree (run did not reach launch)")
	}
	if len(ledger.closedIDs()) == 0 {
		t.Fatalf("hkj6wm7: bead %q did not close — success precondition not met (reopened=%v)",
			beadID, ledger.reopenedIDs())
	}

	// Success path: cleanup MUST have fired and the worktree MUST be gone.
	if !st.cleanupFired {
		t.Error("hkj6wm7 FAIL: wtCleanup did NOT fire on a successful Pi run — happy-path cleanup regressed")
	}
	if _, statErr := os.Stat(st.wtPath); statErr == nil {
		t.Errorf("hkj6wm7 FAIL: worktree %q still exists after a successful run — not cleaned up", st.wtPath)
	}
}
