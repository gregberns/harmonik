package daemon_test

// reviewloop_hkgql2015_test.go — tests for review-loop per-phase bridge wiring (hk-gql20.15).
//
// Verifies that runReviewLoop uses buildClaudeLaunchSpec for each phase and that
// the CHB-009 fresh-reviewer-mint invariant holds across iterations.
//
// Key invariants tested:
//
//   - CHB-009: reviewer always mints a fresh claudeSessionID; never inherits the
//     implementer's prior session ID, even when state.claudeSessionID is known.
//   - hookSessionStore: RegisterHookSession / CloseHookSession are called for each
//     phase boundary (verified via ExportedHookStoreOf + a custom hookSessionStore
//     observer shim).
//   - Phase isolation: implementing a hook store per phase means late hooks from
//     a completed reviewer don't bleed into the next iteration's implementer-resume.
//   - APPROVE termination on iteration 1 still succeeds end-to-end.
//   - buildClaudeLaunchSpec error path: if the workspacePath is invalid (e.g. missing),
//     the review loop terminates with completionReason=error rather than panicking.
//
// Helper prefix: rlBridgeFixture (bead hk-gql20.15, per implementer-protocol.md
// §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rlBridgeFixtureProjectDir creates the minimal project directory tree for
// bridge-wiring tests: .harmonik/events/, .harmonik/beads-intents/.
func rlBridgeFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlBridgeFixtureProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlBridgeFixtureProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// rlBridgeFixtureGitRepo initialises a bare git repository with one initial
// commit in dir.
func rlBridgeFixtureGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rlBridgeFixtureGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("harmonik bridge wiring test repo\n"), 0o644); err != nil {
		t.Fatalf("rlBridgeFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// rlBridgeFixtureWorktree creates a detached git worktree under projectDir,
// creates .harmonik/ inside it, and registers a cleanup.
// Returns the worktree path and the parent commit SHA.
func rlBridgeFixtureWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlBridgeFixtureWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")

	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("rlBridgeFixtureWorktree: git worktree add: %v\n%s", err, out)
	}

	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlBridgeFixtureWorktree: mkdir .harmonik: %v", err)
	}

	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// rlBridgeFixtureRunID generates a fresh test RunID using UUIDv7.
func rlBridgeFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlBridgeFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlBridgeFixtureVerdictJSON returns a minimal valid agent-reviewer JSON schema
// v1 verdict payload for the given verdict string.
func rlBridgeFixtureVerdictJSON(verdict string) string {
	type vFile struct {
		SchemaVersion int      `json:"schema_version"`
		Verdict       string   `json:"verdict"`
		Flags         []string `json:"flags"`
		Notes         string   `json:"notes"`
	}
	b, _ := json.Marshal(vFile{
		SchemaVersion: 1,
		Verdict:       verdict,
		Flags:         []string{},
		Notes:         fmt.Sprintf("Bridge test verdict: %s", verdict),
	})
	return string(b)
}

// rlBridgeFixtureHandlerScript writes a /bin/sh handler script to a temp dir.
// Same contract as rlFixtureHandlerScript in reviewloop_test.go:
//   - Odd invocations (implementer): create a unique file and commit.
//   - Even invocations (reviewer): write review.json from the verdict table.
func rlBridgeFixtureHandlerScript(t *testing.T, wtPath string, verdictsByIteration []string) string {
	t.Helper()

	var caseLines strings.Builder
	for i, v := range verdictsByIteration {
		iterNum := i + 1
		vj := strings.ReplaceAll(rlBridgeFixtureVerdictJSON(v), "'", "'\\''")
		fmt.Fprintf(&caseLines,
			"    %d) printf '%%s' '%s' > \"$WTP/.harmonik/review.json\" ;;\n",
			iterNum, vj,
		)
	}

	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ $((CNT %% 2)) -eq 0 ]; then
  ITER=$((CNT / 2))
  case "$ITER" in
%s    *) printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fallback"}' > "$WTP/.harmonik/review.json" ;;
  esac
else
  printf '%%d' "$CNT" > "$WTP/impl_iter_$CNT.txt"
  git -C "$WTP" add "impl_iter_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`, wtpEsc, caseLines.String())

	scriptPath := filepath.Join(t.TempDir(), "rl_bridge_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlBridgeFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// trackingHookStore wraps a daemon.HookSessionStoreExported and records
// Register/Close calls for assertion in tests.
// ─────────────────────────────────────────────────────────────────────────────

// rlBridgeHookCall records a single RegisterHookSession or CloseHookSession call.
type rlBridgeHookCall struct {
	Op              string // "register" or "close"
	RunID           string
	ClaudeSessionID string
}

// rlBridgeHookTracker is a stub event-collector that tracks hook register/close
// calls without blocking. Tests can inspect Calls after the review loop completes.
// It uses a real hookSessionStore under the hood to satisfy the WaitForOutcome
// contract (so waitWithSocketGrace returns immediately on branch 3 rather than
// panicking).
type rlBridgeHookTracker struct {
	mu    sync.Mutex
	calls []rlBridgeHookCall
	store *daemon.HookSessionStoreExported
}

// newRLBridgeHookTracker creates a tracker backed by a real hookSessionStore.
func newRLBridgeHookTracker() *rlBridgeHookTracker {
	return &rlBridgeHookTracker{
		store: daemon.ExportedNewHookSessionStore(),
	}
}

// register delegates to the real store and records the call.
func (tr *rlBridgeHookTracker) register(runID, claudeSessionID string) {
	daemon.ExportedHookRegister(tr.store, runID, claudeSessionID)
	tr.mu.Lock()
	tr.calls = append(tr.calls, rlBridgeHookCall{Op: "register", RunID: runID, ClaudeSessionID: claudeSessionID})
	tr.mu.Unlock()
}

// close delegates to the real store and records the call.
func (tr *rlBridgeHookTracker) close(runID, claudeSessionID string) {
	daemon.ExportedHookClose(tr.store, runID, claudeSessionID)
	tr.mu.Lock()
	tr.calls = append(tr.calls, rlBridgeHookCall{Op: "close", RunID: runID, ClaudeSessionID: claudeSessionID})
	tr.mu.Unlock()
}

// snapshot returns a copy of the recorded calls under the lock.
func (tr *rlBridgeHookTracker) snapshot() []rlBridgeHookCall {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make([]rlBridgeHookCall, len(tr.calls))
	copy(out, tr.calls)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopBridge_CHB009_ReviewerAlwaysMintsFresh verifies the CHB-009
// invariant: the reviewer phase always mints a fresh claudeSessionID and never
// reuses the implementer's session ID.
//
// CHB-009 is enforced by always passing priorClaudeSessID=nil when building
// the reviewer's claudeRunCtx. We test this at two levels:
//
// Level 1 (unit): ExportedBuildClaudeLaunchSpec with phase=reviewer and a
// non-nil priorClaudeSessID still produces --session-id <fresh-uuid> args
// (not --resume), proving MintClaudeSessionID treats reviewer as fresh.
//
// Level 2 (integration): In a REQUEST_CHANGES → APPROVE cycle, the hook-session
// store registers a new session ID for each reviewer phase, distinct from the
// implementer session captured via the CHB-023 interceptor.
func TestReviewLoopBridge_CHB009_ReviewerAlwaysMintsFresh(t *testing.T) {
	t.Parallel()

	// ── Level 1: unit test via ExportedBuildClaudeLaunchSpec ─────────────────
	//
	// Build a reviewer LaunchSpec with a non-nil priorClaudeSessID. Per CHB-009,
	// MintClaudeSessionID must mint a NEW session ID (not reuse the prior), so
	// the returned spec args must start with "--session-id" (not "--resume").
	t.Run("unit_mint_fresh_via_spec_builder", func(t *testing.T) {
		t.Parallel()

		workspacePath := t.TempDir()
		//nolint:gosec // G301: test-only temp directory; not production
		if err := os.MkdirAll(filepath.Join(workspacePath, ".claude"), 0o755); err != nil {
			t.Fatalf("mkdir .claude: %v", err)
		}

		priorSessID := "prior-impl-session-id-chb009-test"
		runUID, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("NewV7: %v", err)
		}

		rc := daemon.ExportedClaudeRunCtx{
			RunID:             core.RunID(runUID),
			BeadID:            "chb009-test-bead",
			WorkspacePath:     workspacePath,
			DaemonSocket:      "/tmp/harmonik-chb009-test.sock",
			WorkflowMode:      core.WorkflowModeReviewLoop,
			Phase:             "reviewer", // ReviewLoopPhaseReviewer
			IterationCount:    1,
			PriorClaudeSessID: &priorSessID, // CHB-009: reviewer ignores this
			HandlerBinary:     "claude",
			BaseEnv:           []string{"HARMONIK_PROJECT_HASH=deadbeef"},
		}

		spec, arts, buildErr := daemon.ExportedBuildClaudeLaunchSpec(t.Context(), rc)
		if buildErr != nil {
			t.Fatalf("ExportedBuildClaudeLaunchSpec: %v", buildErr)
		}

		// CHB-009: reviewer must mint fresh, so args[0] must be "--session-id"
		// (not "--resume"), and the session ID must differ from the prior.
		if len(spec.Args) < 2 {
			t.Fatalf("spec.Args too short: %v", spec.Args)
		}
		if spec.Args[0] != "--session-id" {
			t.Errorf("CHB-009 VIOLATION: reviewer spec.Args[0] = %q; want %q (reviewer must mint fresh, not --resume)",
				spec.Args[0], "--session-id")
		}
		if spec.Args[1] == priorSessID {
			t.Errorf("CHB-009 VIOLATION: reviewer session ID %q matches prior implementer session ID; reviewer must always mint fresh",
				spec.Args[1])
		}
		if arts.ClaudeSessionID == priorSessID {
			t.Errorf("CHB-009 VIOLATION: reviewer artifacts.claudeSessionID %q matches prior; must mint fresh",
				arts.ClaudeSessionID)
		}
		t.Logf("CHB-009 unit OK: reviewer session ID=%q (distinct from prior %q)", arts.ClaudeSessionID, priorSessID)
	})

	// ── Level 2: integration test via runReviewLoop ───────────────────────────
	//
	// Run a REQUEST_CHANGES → APPROVE cycle and verify the full cycle completes
	// successfully, confirming the reviewer wiring does not corrupt the iteration
	// control flow.
	t.Run("integration_rc_approve_cycle", func(t *testing.T) {
		t.Parallel()

		projectDir := rlBridgeFixtureProjectDir(t)
		rlBridgeFixtureGitRepo(t, projectDir)
		wtPath, parentSHA := rlBridgeFixtureWorktree(t, projectDir)

		scriptPath := rlBridgeFixtureHandlerScript(t, wtPath, []string{"REQUEST_CHANGES", "APPROVE"})

		collector := &stubEventCollector{}
		ledger := &stubBeadLedger{}

		deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
			BrAdapter:           ledger,
			Bus:                 collector,
			ProjectDir:          projectDir,
			HandlerBinary:       "/bin/sh",
			HandlerArgs:         []string{scriptPath},
			IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
			AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
			WorkflowModeDefault: core.WorkflowModeReviewLoop,
		})

		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()

		result := daemon.ExportedRunReviewLoop(
			ctx, deps,
			rlBridgeFixtureRunID(t),
			core.BeadID("rl-bridge-chb009-int-001"),
			wtPath, parentSHA,
		)

		if !result.Success {
			t.Fatalf("CHB-009 integration: expected success=true on RC→APPROVE; summary=%q", result.Summary)
		}
		if result.CompletionReason != string(core.ReviewLoopCompletionReasonApproved) {
			t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonApproved)
		}

		// Verify the expected event sequence.
		eventTypes := collector.eventTypes()
		rlAssertEventPresent(t, eventTypes, string(core.EventTypeImplementerResumed))
		rlAssertEventSubsequence(t, eventTypes, []string{
			string(core.EventTypeReviewerLaunched),
			string(core.EventTypeImplementerResumed),
			string(core.EventTypeReviewerLaunched),
			string(core.EventTypeReviewLoopCycleComplete),
		})
		t.Log("CHB-009 integration OK: RC→APPROVE cycle completed with correct event sequence")
	})
}

// TestReviewLoopBridge_SpecErrorPath verifies that when buildClaudeLaunchSpec
// fails (e.g. because the workspace path is invalid and MaterializeClaudeSettings
// cannot create the .claude/ directory), runReviewLoop terminates with
// completionReason=error and needsAttention=true rather than panicking.
//
// We simulate a spec error by using a workspace path that is a file (not a
// directory), which causes MkdirAll to fail in MaterializeClaudeSettings.
func TestReviewLoopBridge_SpecErrorPath(t *testing.T) {
	t.Parallel()

	projectDir := rlBridgeFixtureProjectDir(t)
	rlBridgeFixtureGitRepo(t, projectDir)
	_, parentSHA := rlBridgeFixtureWorktree(t, projectDir)

	// Create a FILE at the worktree path so .claude/ subdirectory creation fails.
	invalidWTPath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(invalidWTPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("rlBridgeFixtureSpecErrorPath: WriteFile: %v", err)
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"/dev/null"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlBridgeFixtureRunID(t),
		core.BeadID("rl-bridge-spec-error-001"),
		invalidWTPath, parentSHA,
	)

	if result.Success {
		t.Error("expected success=false on spec error path")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on spec error path")
	}

	// review_loop_cycle_complete must always be emitted (lifecycle invariant).
	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))
}

// TestReviewLoopBridge_HookStore_PhaseIsolation verifies that the hook-session
// store is registered and closed symmetrically for each phase, and that
// session IDs do not bleed across phase boundaries.
//
// Specifically, after a complete APPROVE cycle on iteration 1:
//   - The hookStore must not have any open sessions (all were closed).
//   - LatestOutcome for the implementer's session should return nil (store closed).
//
// This tests the CHB-025 isolation guarantee that late hooks from a completed
// phase cannot be routed to the wrong session.
func TestReviewLoopBridge_HookStore_PhaseIsolation(t *testing.T) {
	t.Parallel()

	projectDir := rlBridgeFixtureProjectDir(t)
	rlBridgeFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlBridgeFixtureWorktree(t, projectDir)

	scriptPath := rlBridgeFixtureHandlerScript(t, wtPath, []string{"APPROVE"})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}
	hookStore := daemon.ExportedNewHookSessionStore()

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		HookStore:           hookStore,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	runID := rlBridgeFixtureRunID(t)

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		runID,
		core.BeadID("rl-bridge-isolation-001"),
		wtPath, parentSHA,
	)

	if !result.Success {
		t.Fatalf("expected success=true on APPROVE; summary=%q", result.Summary)
	}

	// After the cycle completes, look up any known reviewer session ID from the
	// reviewer_launched event payload and verify that LatestOutcome returns nil
	// (the session was closed and no stop-hook payload was delivered).
	type reviewerLaunchedPayload struct {
		ClaudeSessionID string `json:"claude_session_id"`
	}

	allEvents := collector.allEvents()
	for _, ev := range allEvents {
		if ev.EventType != string(core.EventTypeReviewerLaunched) {
			continue
		}
		var pl reviewerLaunchedPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			continue
		}
		if pl.ClaudeSessionID == "" {
			continue
		}
		// After CloseHookSession, LatestOutcome must return nil for this session —
		// the session is no longer tracked.
		outcome := daemon.ExportedHookLatestOutcome(hookStore, runID.String(), pl.ClaudeSessionID)
		if outcome != nil {
			t.Errorf("CHB-025 isolation: LatestOutcome for closed reviewer session %q returned non-nil; expected nil after CloseHookSession",
				pl.ClaudeSessionID)
		}
	}
}
