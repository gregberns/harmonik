package daemon_test

// mergetomain_stripruncontext_hk4je_test.go — integration test asserting that
// .harmonik/run-context/** never appears in git ls-tree main after a run merges.
//
// Test assertion (hk-4je):
//   (q)  After a run that force-committed a context.json (CHB-023) is merged to
//        main, `git ls-tree --name-only -r main` does NOT contain any path under
//        .harmonik/run-context/.
//
// The factory force-adds a context.json to the run-branch (mirroring what
// sessioncontext_chb023.go does at runtime) so the stripping logic in
// mergeRunBranchToMain (stripRunContextFromMerge) has real work to do.
//
// Helper prefix: stripRunCtx (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-4je).
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.6.CHB-023
//   - specs/execution-model.md §4.12.EM-052
//
// Bead: hk-4je.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// stripRunCtxWorktreeFactory wraps productionWorktreeFactory and also
// force-commits a .harmonik/run-context/<runID>/context.json into the
// run-branch, mirroring the CHB-023 persistClaudeSessionID path.
func stripRunCtxWorktreeFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// 1. Commit real agent work so the run-branch is ahead of main.
		workFile := filepath.Join(wtPath, "work_hk4je.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if writeErr := os.WriteFile(workFile, []byte("agent work\n"), 0o644); writeErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: WriteFile work: %w", writeErr)
		}
		addCmd := exec.CommandContext(ctx, "git", "add", "work_hk4je.txt")
		addCmd.Dir = wtPath
		if out, addErr := addCmd.CombinedOutput(); addErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: git add work: %v\n%s", addErr, out)
		}
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: agent work",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: git commit work: %v\n%s", commitErr, out)
		}

		// 2. Force-add context.json, mirroring CHB-023 persistClaudeSessionID.
		ctxDir := filepath.Join(wtPath, ".harmonik", "run-context", runID)
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if mkErr := os.MkdirAll(ctxDir, 0o755); mkErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: MkdirAll run-context: %w", mkErr)
		}
		ctxFile := filepath.Join(ctxDir, "context.json")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if writeErr := os.WriteFile(ctxFile, []byte(`{"claude_session_id":"test-session-id","persisted_at":"2026-01-01T00:00:00Z"}`+"\n"), 0o644); writeErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: WriteFile context.json: %w", writeErr)
		}
		// git add -f: .harmonik/ is in .gitignore, so -f is required.
		relCtxPath := filepath.Join(".harmonik", "run-context", runID, "context.json")
		addCtxCmd := exec.CommandContext(ctx, "git", "add", "-f", relCtxPath)
		addCtxCmd.Dir = wtPath
		if out, addErr := addCtxCmd.CombinedOutput(); addErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: git add -f context.json: %v\n%s", addErr, out)
		}
		commitCtxCmd := exec.CommandContext(ctx, "git", "commit", "-m",
			"harmonik: persist claude_session_id to Run.context (CHB-023)",
			"--trailer", "Harmonik-Run-ID: "+runID,
			"--trailer", "Harmonik-Context-Event: claude_session_id_persisted",
		)
		commitCtxCmd.Dir = wtPath
		if out, commitErr := commitCtxCmd.CombinedOutput(); commitErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("stripRunCtxWorktreeFactory: git commit context.json: %v\n%s", commitErr, out)
		}

		return wtPath, cleanup, nil
	}
}

// TestStripRunContext_NeverLandsOnMain verifies that after a run which
// force-committed .harmonik/run-context/<id>/context.json (CHB-023) is merged
// to main, the path does NOT appear in `git ls-tree main` (assertion q).
//
// Bead: hk-4je.
func TestStripRunContext_NeverLandsOnMain(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("striprunctx-neveronmain-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote (origin) so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	addOriginCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addOriginCmd.Dir = projectDir
	if out, err := addOriginCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

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
		WorktreeFactory:  stripRunCtxWorktreeFactory(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

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

	// ── Assertion (q1): bead closed (not reopened). ───────────────────────────
	if got := ledger.getClosedCount(); got < 1 {
		t.Errorf("CloseBead call count = %d; want ≥ 1 (bead should be closed)", got)
	}
	if got := ledger.getReopenedCount(); got > 0 {
		t.Errorf("ReopenBead called %d times; want 0 (reason: %s)", got, ledger.getReopenReason())
	}

	// ── Assertion (q2): .harmonik/run-context/** absent from main. ───────────
	lsTreeCmd := exec.CommandContext(context.Background(), "git", "ls-tree", "--name-only", "-r", "main")
	lsTreeCmd.Dir = projectDir
	out, err := lsTreeCmd.Output()
	if err != nil {
		t.Fatalf("git ls-tree main: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, ".harmonik/run-context/") {
			t.Errorf("main contains .harmonik/run-context path after merge: %q", line)
		}
	}
}
