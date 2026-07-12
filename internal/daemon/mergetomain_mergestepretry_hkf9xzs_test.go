package daemon_test

// mergetomain_mergestepretry_hkf9xzs_test.go — regression tests for the
// outer merge-step retry loop added in hk-f9xzs.
//
// # Problem addressed
//
// logmine iter-22 M1: 31 runs received an APPROVE reviewer_verdict then
// emitted outcome:rejected + run_failed at the merge step (dirty-index 11,
// non_ff 10, merge_fmt_failed 10) and re-launched the WHOLE implementer+
// reviewer from scratch. The per-cause fixes (hk-sfy7f, hk-2jeel) address
// specific failure modes, but the systemic fix is to retry only the merge step
// and preserve the APPROVE verdict rather than reopening and re-reviewing.
//
// # Fix (hk-f9xzs)
//
// When lockedMergeRunBranchToMain returns a retryable failure (rebase_conflict /
// non_ff_merge / merge_fmt_failed), the workloop retries the merge step up to
// maxMergeStepRetries (2) additional times before falling back to EM-053
// ReopenBead. The APPROVE verdict is preserved across retries.
//
// # Tests
//
//   TestIsRetryableMergeReason — unit-tests the classification function.
//   TestMergeStepRetry_RebaseConflictRetries_Succeeds — full integration test:
//     review loop APPROVE + first merge attempt fails with rebase_conflict
//     (pre-rebase hook exits 1 on call 1), second attempt succeeds.
//     Assertions: CloseBead called once, ReopenBead never called.
//
// Helper prefix: msr (per implementer-protocol.md §Helper-prefix discipline;
// bead hk-f9xzs).
//
// Spec ref: specs/execution-model.md §4.12 EM-052, EM-053.
// Bead: hk-f9xzs.

import (
	"context"
	"encoding/json"
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

// ─────────────────────────────────────────────────────────────────────────────
// Unit test: isRetryableMergeReason
// ─────────────────────────────────────────────────────────────────────────────

// TestIsRetryableMergeReason verifies the retryable/non-retryable classification
// of merge failure reasons (hk-f9xzs).
func TestIsRetryableMergeReason(t *testing.T) {
	t.Parallel()

	retryable := []string{
		"rebase_conflict: exit status 1\nerror: cannot rebase: You have unstaged changes.",
		"rebase_conflict",
		"rebase_conflict_on_non_ff_merge_retry (attempt 2): exit status 1",
		"non_ff_merge: main advanced concurrently",
		"non_ff_merge_retry_rev_parse (attempt 1): exit status 128",
		"non_ff_merge",
		"merge_fmt_failed (gofumpt): internal/foo/bar.go",
		"merge_fmt_failed (gci): import order drift detected",
		"merge_fmt_failed (fmt commit): exit status 1",
		"merge_fmt_failed",
	}
	for _, reason := range retryable {
		if !daemon.ExportedIsRetryableMergeReason(reason) {
			t.Errorf("isRetryableMergeReason(%q) = false; want true", reason)
		}
	}

	nonRetryable := []string{
		"merge_build_failed (go build): exit status 1",
		"merge_build_failed (go vet): exit status 1",
		"push_failed: exit status 1\n[rejected]",
		"push_failed_fetch (attempt 2): exit status 128",
		"push_failed_rev_parse_remote (attempt 1): exit status 128",
		"strip_run_context_failed: exit status 1",
		"merge_target_empty: targetBranch must not be empty",
		"merge_target_protected: \"main\" is in ProtectBranches",
		"rebase_dropped_commits: rebase of run/x onto main produced no commits",
		"git rev-parse main: exit status 128",
		"",
		"unknown_reason",
	}
	for _, reason := range nonRetryable {
		if daemon.ExportedIsRetryableMergeReason(reason) {
			t.Errorf("isRetryableMergeReason(%q) = true; want false", reason)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// msrGitRun runs a git command in dir and fatals on error.
func msrGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("msrGitRun: git %v: %v\n%s", args, err, out)
	}
}

// msrSetupRepo creates a git repo in dir with an initial commit and an
// origin bare remote, pushing initial main to origin.
func msrSetupRepo(t *testing.T, dir string) (originDir string) {
	t.Helper()
	msrGitRun(t, dir, "init", "--initial-branch=main")
	msrGitRun(t, dir, "config", "user.email", "msr-test@harmonik.local")
	msrGitRun(t, dir, "config", "user.name", "MSR Test")
	readmePath := filepath.Join(dir, "README")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(readmePath, []byte("msr test\n"), 0o644); err != nil {
		t.Fatalf("msrSetupRepo: WriteFile: %v", err)
	}
	msrGitRun(t, dir, "add", "README")
	msrGitRun(t, dir, "commit", "-m", "init")

	originDir = t.TempDir()
	msrGitRun(t, dir, "init", "--bare", "--initial-branch=main", originDir)
	msrGitRun(t, dir, "remote", "add", "origin", originDir)
	msrGitRun(t, dir, "push", "origin", "main")
	return originDir
}

// msrInstallPreRebaseHook installs a pre-rebase hook in dir that exits 1 on the
// first invocation and exits 0 on all subsequent invocations. This simulates a
// transient rebase_conflict that clears on retry.
//
// The hook uses a counter file in .git/ (common dir) to track invocations.
func msrInstallPreRebaseHook(t *testing.T, projectDir string) {
	t.Helper()
	hooksDir := filepath.Join(projectDir, ".git", "hooks")
	//nolint:gosec // G301: test fixture
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("msrInstallPreRebaseHook: mkdir hooks: %v", err)
	}
	hookScript := `#!/bin/sh
# Fail only on the first invocation, succeed on all subsequent ones.
COMMON_DIR="$(git rev-parse --git-common-dir 2>/dev/null || echo .git)"
COUNTER_FILE="$COMMON_DIR/hooks/.msr-pre-rebase-count"
if [ ! -f "$COUNTER_FILE" ]; then
  printf '0' > "$COUNTER_FILE"
fi
COUNT=$(cat "$COUNTER_FILE")
COUNT=$((COUNT + 1))
printf '%d' "$COUNT" > "$COUNTER_FILE"
if [ "$COUNT" -le 1 ]; then
  printf 'msr-pre-rebase-hook: simulating conflict on attempt %d\n' "$COUNT" >&2
  exit 1
fi
exit 0
`
	hookPath := filepath.Join(hooksDir, "pre-rebase")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(hookPath, []byte(hookScript), 0o755); err != nil {
		t.Fatalf("msrInstallPreRebaseHook: WriteFile: %v", err)
	}
}

// msrHandlerScript writes a /bin/sh handler script to a temp file and returns
// its path. The script uses a counter file (counterPath) shared across
// invocations to distinguish implementer (odd) from reviewer (even) calls.
//
//   - Odd invocations (implementer): write + commit work.txt to $HARMONIK_WORKSPACE_PATH.
//   - Even invocations (reviewer): write APPROVE verdict JSON to
//     $HARMONIK_WORKSPACE_PATH/.harmonik/review.json.
func msrHandlerScript(t *testing.T, counterPath string) string {
	t.Helper()

	// Escape counterPath for embedding in single-quoted shell string.
	counterEsc := strings.ReplaceAll(counterPath, "'", "'\\''")

	approveJSON := func() string {
		type vFile struct {
			SchemaVersion int      `json:"schema_version"`
			Verdict       string   `json:"verdict"`
			Flags         []string `json:"flags"`
			Notes         string   `json:"notes"`
		}
		b, _ := json.Marshal(vFile{
			SchemaVersion: 1,
			Verdict:       "APPROVE",
			Flags:         []string{},
			Notes:         "msr-test APPROVE",
		})
		return string(b)
	}()
	approveEsc := strings.ReplaceAll(approveJSON, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
CNT_FILE='%s'
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
WS="${HARMONIK_WORKSPACE_PATH}"
if [ -z "$WS" ]; then
  printf 'msr-handler: HARMONIK_WORKSPACE_PATH not set\n' >&2
  exit 1
fi
if [ $((CNT %% 2)) -eq 1 ]; then
  # Implementer: commit a file.
  printf '%%d' "$CNT" > "$WS/work.txt"
  git -C "$WS" add "work.txt" >/dev/null 2>&1
  git -C "$WS" -c user.email=msr@harmonik.local -c user.name="MSR Test" commit \
    -m "feat: msr work iter $CNT" --no-gpg-sign >/dev/null 2>&1
else
  # Reviewer: write APPROVE verdict.
  mkdir -p "$WS/.harmonik"
  printf '%%s' '%s' > "$WS/.harmonik/review.json"
fi
exit 0
`, counterEsc, approveEsc)

	scriptPath := filepath.Join(t.TempDir(), "msr_handler.sh")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("msrHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration test
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeStepRetry_RebaseConflictRetries_Succeeds verifies that when the
// review loop APPROVEs and the first merge attempt fails with rebase_conflict
// (pre-rebase hook exits 1 on call 1), the workloop retries the merge step
// (up to maxMergeStepRetries=2) and closes the bead successfully without
// reopening it or re-running the implementer+reviewer cycle.
//
// Setup:
//   - git repo with bare remote
//   - pre-rebase hook that exits 1 on the first git-rebase invocation
//   - handler script: odd invocations = implementer (commits work.txt),
//     even invocations = reviewer (writes APPROVE verdict)
//   - workflow mode: review-loop
//
// Assertions:
//
//	(A) CloseBead called exactly once (bead closes successfully).
//	(B) ReopenBead never called (no full re-run triggered).
//	(C) outcome_emitted{kind=approved} is present in the event stream.
//	(D) run_completed{success:true} is present in the event stream.
//
// Bead: hk-f9xzs.
func TestMergeStepRetry_RebaseConflictRetries_Succeeds(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("msr-rebase-conflict-retry-bead-hkf9xzs")

	projectDir := mergeToMainFixtureProjectDir(t)
	msrSetupRepo(t, projectDir)

	// Install hook: rebase exits 1 on first call, exits 0 on subsequent calls.
	msrInstallPreRebaseHook(t, projectDir)

	// Counter file shared across handler invocations (implementer/reviewer).
	counterPath := filepath.Join(t.TempDir(), "msr_counter")

	scriptPath := msrHandlerScript(t, counterPath)
	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
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
	case <-time.After(10 * time.Second):
		t.Error("work loop did not exit within 10s after cancel")
	}

	// ── Assertion (A): CloseBead called exactly once. ─────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (merge-step retry should succeed)", got)
	}

	// ── Assertion (B): ReopenBead never called. ───────────────────────────────
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0; reason = %q", got, ledger.getReopenReason())
	}

	// ── Assertion (C): outcome_emitted{kind=approved} present. ───────────────
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	found := false
	for _, ev := range outcomeEvs {
		if mergeToMainPayloadKind(t, ev) == "approved" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("outcome_emitted{kind=approved} not found in event stream: %v",
			mergeToMainEventOrder(collector))
	}

	// ── Assertion (D): run_completed{success:true} present. ──────────────────
	rcEvs := mergeToMainFindEvents(collector, "run_completed")
	if len(rcEvs) == 0 {
		t.Errorf("run_completed not found in event stream: %v", mergeToMainEventOrder(collector))
	} else {
		var payload map[string]interface{}
		if err := json.Unmarshal(rcEvs[0].Payload, &payload); err == nil {
			if success, _ := payload["success"].(bool); !success {
				t.Errorf("run_completed.success = false; want true")
			}
		}
	}
}
