package daemon_test

// reviewloop_hkkqdpf2_test.go — reviewer-phase waitAgentReady wiring (hk-kqdpf.2).
//
// Verifies that the reviewer phase calls waitAgentReady before forwarding work
// and that an HC-056 timeout produces the same error envelope as the implementer
// phase (completionReason=error, needsAttention=true, success=false).
//
// Helper prefix: rlReadyTimeout (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-kqdpf.2).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rlReadyTimeoutProjectDir creates the minimal project directory tree for
// reviewer ready-timeout tests: .harmonik/events/, .harmonik/beads-intents/.
func rlReadyTimeoutProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rlReadyTimeoutProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rlReadyTimeoutProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// rlReadyTimeoutGitRepo initialises a bare git repository with one initial
// commit in dir.
func rlReadyTimeoutGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("rlReadyTimeoutGitRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	readmePath := filepath.Join(dir, "README")
	if err := os.WriteFile(readmePath, []byte("harmonik reviewer-ready-timeout test repo\n"), 0o644); err != nil {
		t.Fatalf("rlReadyTimeoutGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
}

// rlReadyTimeoutWorktree creates a detached git worktree under projectDir,
// creates .harmonik/ inside it, and registers a cleanup.
// Returns the worktree path and the parent commit SHA.
func rlReadyTimeoutWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("rlReadyTimeoutWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")

	//nolint:gosec // G204: git args are test-internal literals; not user input
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("rlReadyTimeoutWorktree: git worktree add: %v\n%s", err, out)
	}

	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rlReadyTimeoutWorktree: mkdir .harmonik: %v", err)
	}

	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal; not user input
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// rlReadyTimeoutRunID generates a fresh test RunID using UUIDv7.
func rlReadyTimeoutRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("rlReadyTimeoutRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// rlReadyTimeoutAdapter is a minimal Adapter stub whose DetectReady always
// returns false (it never fires agent_ready). Used to exercise the timeout path.
type rlReadyTimeoutAdapter struct{}

func (a *rlReadyTimeoutAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (a *rlReadyTimeoutAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}
func (a *rlReadyTimeoutAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (a *rlReadyTimeoutAdapter) RotateAccount(_ context.Context) error { return nil }

// rlReadyTimeoutMakeRegistry constructs a sealed AdapterRegistry with the given
// adapter registered for claude-code.
func rlReadyTimeoutMakeRegistry(t *testing.T, adapter handlercontract.Adapter) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, adapter); err != nil {
		t.Fatalf("rlReadyTimeoutMakeRegistry: Register: %v", err)
	}
	return reg
}

// rlReadyTimeoutHandlerScript writes a /bin/sh handler script that:
//   - On odd invocations (implementer): creates a unique file and commits so
//     the diff hash advances (avoids false no-progress detection).
//   - On even invocations (reviewer): writes review.json then immediately exits
//     WITHOUT emitting agent_ready — so the reviewer waitAgentReady will hit
//     the timeout when the registry contains a never-ready adapter.
//
// For the timeout test we do NOT need the reviewer to emit agent_ready; the
// handler exits before the timeout only if the timeout is longer than the
// script's runtime. We pass a very short timeout (50ms) so the timeout fires
// before the script exits (which takes ~0ms for the sleep path below). In the
// normal (no-adapter) path the handler exits cleanly before waitAgentReady
// would even be called.
func rlReadyTimeoutHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()

	// The reviewer phase sleeps briefly so the timeout (50ms) fires before
	// the handler exits, ensuring ErrAgentReadyTimeout rather than the
	// watcher-done cancel path.
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := `#!/bin/sh
WTP='` + wtpEsc + `'
CNT_FILE="$WTP/.harmonik/rltimeout_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 1 ]; then
  # Implementer: commit a unique file so diff hash advances.
  printf '%d' "$CNT" > "$WTP/impl_timeout_$CNT.txt"
  git -C "$WTP" add "impl_timeout_$CNT.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "impl timeout iter $CNT" --no-gpg-sign >/dev/null 2>&1
else
  # Reviewer: sleep longer than the 50ms timeout so ErrAgentReadyTimeout fires,
  # then write a verdict so the watcher does not fail on a missing verdict file
  # (the review loop never reads it when the timeout fires, but the sleep is
  # what matters for triggering the timeout path).
  sleep 5
fi
exit 0
`

	scriptPath := filepath.Join(t.TempDir(), "rl_readytimeout_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("rlReadyTimeoutHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopReviewerReadyTimeout verifies that the reviewer phase calls
// waitAgentReady and that an HC-056 timeout produces the same error envelope as
// the implementer phase: success=false, completionReason=error,
// needsAttention=true.
//
// Mechanism:
//  1. An AdapterRegistry with a never-ready adapter (DetectReady always false)
//     is installed so waitAgentReady enters the timeout path.
//  2. AgentReadyTimeout is set to 50ms — much shorter than the reviewer script's
//     sleep (5s) — so the timeout fires before the reviewer exits.
//  3. review_loop_cycle_complete MUST still be emitted (lifecycle invariant).
func TestReviewLoopReviewerReadyTimeout(t *testing.T) {
	t.Parallel()

	projectDir := rlReadyTimeoutProjectDir(t)
	rlReadyTimeoutGitRepo(t, projectDir)
	wtPath, parentSHA := rlReadyTimeoutWorktree(t, projectDir)

	scriptPath := rlReadyTimeoutHandlerScript(t, wtPath)

	reg := rlReadyTimeoutMakeRegistry(t, &rlReadyTimeoutAdapter{})

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    reg,
		AgentReadyTimeout:   50 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlReadyTimeoutRunID(t),
		core.BeadID("rl-ready-timeout-reviewer-001"),
		wtPath, parentSHA,
	)

	// HC-056: timeout must produce same error envelope as implementer phase.
	if result.Success {
		t.Error("expected success=false on reviewer ready-timeout")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("completion_reason = %q; want %q", result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
	if !result.NeedsAttention {
		t.Error("expected needs_attention=true on reviewer ready-timeout")
	}
	if !strings.Contains(result.Summary, "agent_ready_timeout") {
		t.Errorf("summary %q does not contain %q", result.Summary, "agent_ready_timeout")
	}

	// review_loop_cycle_complete MUST always be emitted (lifecycle invariant).
	eventTypes := collector.eventTypes()
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeReviewLoopCycleComplete))

	t.Logf("reviewer ready-timeout OK: completionReason=%q summary=%q", result.CompletionReason, result.Summary)
}
