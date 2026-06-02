//go:build scenario

package daemon_test

// scenario_reviewloop_em015de_hkintln_test.go — non-gated scenario tests for
// review-loop termination paths (EM-015d / EM-015e).
//
// # Coverage gap addressed
//
// Assessment 2026-06-02: review-loop termination paths had no //go:build scenario
// coverage. The only gated E2E test was e2e_real_claude_reviewloop_test.go behind
// HARMONIK_E2E_REAL_CLAUDE=1 (requires a live Claude session). The unit-level
// reviewloop_test.go covers most paths but without the tmux-substrate (nil-watcher)
// or the scenario build tag. These tests fill that gap without a live Claude session.
//
// Run via:
//
//	go test -tags scenario -timeout 120s ./internal/daemon/...
//
// # Scenarios covered
//
//   EM-015d (T-WM-021)  — REQUEST_CHANGES × 3 → iteration_cap_hit + needs-attention
//   EM-015d (BLOCK)     — BLOCK on first verdict → immediate needs-attention close
//   EM-015e (T-WM-022)  — identical diff hash → no_progress_detected, reviewer NOT
//                         launched on iteration 2
//
// # Substrate
//
// All three tests use em015FixtureNilStdoutSubstrate — a handler.Substrate that
// executes the real command but returns Stdout()==nil, causing
// handler.launchViaSubstrate to return watcher=nil. This mirrors the tmux-hosted
// path (spawn_path_sc7_hk04azt pattern) without requiring a live tmux session.
//
// Consequence: each invocation waits the full stopHookGrace window (3 s) in
// waitWithSocketGrace before proceeding on exit code, because no Stop-hook relay
// is running. Tests set context timeouts accordingly:
//   - cap-hit (6 invocations): 90 s
//   - BLOCK   (2 invocations): 30 s
//   - no-progress (3 invocations, no iter-2 reviewer): 30 s
//
// # Handler scripts
//
// The implementer phase uses a commit-only shell script (with an optional twin
// binary invocation when harmonik-twin-claude is present) — mirroring SC-7.
// The reviewer phase always uses a scripted shell handler that writes the verdict
// JSON required for each termination scenario.
//
// rlFixtureHandlerScript (reviewloop_test.go) and rlFixtureNoProgressHandlerScript
// are reused directly since they share the daemon_test package.
//
// # Helper prefix
//
// Helpers in this file use the prefix "em015Fixture" (bead hk-intln; per
// implementer-protocol.md §Helper-prefix discipline).
//
// Spec refs:
//   - specs/execution-model.md §4.3 EM-015d (review-loop iteration cap + BLOCK)
//   - specs/execution-model.md §4.3 EM-015e (no-progress termination)
//   - specs/scenario-harness.md §4 (twin-driven spawn-path coverage)
//
// Bead: hk-intln. Refs: EM-015d, EM-015e.

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/handler"
)

// ─────────────────────────────────────────────────────────────────────────────
// em015FixtureNilStdoutSubstrate — handler.Substrate with nil Stdout
// ─────────────────────────────────────────────────────────────────────────────

// em015FixtureNilStdoutSubstrate is a handler.Substrate whose SpawnWindow runs
// the real Argv as a subprocess but returns Stdout()==nil so that
// handler.launchViaSubstrate returns watcher=nil — the tmux-hosted path.
// This is the same mechanism as sc7FixtureNilStdoutSubstrate (hk-04azt).
type em015FixtureNilStdoutSubstrate struct{}

// SpawnWindow runs the real command and returns a session with Stdout() == nil.
func (s *em015FixtureNilStdoutSubstrate) SpawnWindow(ctx context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("em015FixtureNilStdoutSubstrate: Argv is empty")
	}
	//nolint:gosec // G204: Argv comes from test-internal WorkLoopDepsParams.HandlerBinary; not user input
	cmd := exec.CommandContext(ctx, in.Argv[0], in.Argv[1:]...)
	cmd.Dir = in.Cwd
	cmd.Env = in.Env
	// Do NOT set cmd.Stdout — subprocess stdout goes to /dev/null (like tmux).
	// em015FixtureNilStdoutSession.Stdout() returns nil, causing
	// handler.launchViaSubstrate to return watcher=nil.
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("em015FixtureNilStdoutSubstrate: Start: %w", err)
	}
	return &em015FixtureNilStdoutSession{cmd: cmd}, nil
}

// Compile-time assertion.
var _ handler.Substrate = (*em015FixtureNilStdoutSubstrate)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// em015FixtureNilStdoutSession — SubstrateSession with Stdout() == nil
// ─────────────────────────────────────────────────────────────────────────────

// em015FixtureNilStdoutSession is a handler.SubstrateSession backed by a real
// exec.Cmd whose Stdout() returns nil, simulating a tmux-hosted session.
type em015FixtureNilStdoutSession struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// Kill terminates the subprocess.
func (s *em015FixtureNilStdoutSession) Kill(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// Wait waits for the subprocess to exit; exit-code errors are suppressed.
func (s *em015FixtureNilStdoutSession) Wait(_ context.Context) error {
	_ = s.cmd.Wait()
	return nil
}

// Outcome returns a zero-value Outcome (exit info not surfaced in this stub).
func (s *em015FixtureNilStdoutSession) Outcome() handler.Outcome {
	return handler.Outcome{}
}

// PID returns the subprocess PID.
func (s *em015FixtureNilStdoutSession) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// Stdout returns nil — causes handler.launchViaSubstrate to return watcher=nil.
func (s *em015FixtureNilStdoutSession) Stdout() io.Reader { return nil }

// Compile-time assertion.
var _ handler.SubstrateSession = (*em015FixtureNilStdoutSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// em015FixtureHandlerScript — handler script with optional twin for implementer
// ─────────────────────────────────────────────────────────────────────────────

// em015FixtureHandlerScript writes a /bin/sh handler script.
//
// It delegates to rlFixtureHandlerScript (reviewloop_test.go) for the
// counter-and-verdict logic, then optionally splices in a twin binary invocation
// for the implementer phase (odd invocations) when twinPath is non-empty.
//
// When twinPath is empty the implementer phase is commit-only (same as the
// non-twin fallback in SC-7).
func em015FixtureHandlerScript(t *testing.T, wtPath string, verdictsByIteration []string, twinPath string) string {
	t.Helper()

	if twinPath == "" {
		// No twin available — use the standard commit-only script from reviewloop_test.go.
		return rlFixtureHandlerScript(t, wtPath, verdictsByIteration)
	}

	// Twin is available: build a variant of rlFixtureHandlerScript that adds a
	// twin invocation on odd (implementer) calls, mirroring SC-7 sc7FixtureHandlerScript.
	//
	// The twin runs with --scenario single-happy-path; its NDJSON output is
	// discarded (nil-stdout substrate), but the subprocess executes normally.
	var caseLines strings.Builder
	for i, v := range verdictsByIteration {
		iterNum := i + 1
		vj := strings.ReplaceAll(rlFixtureVerdictJSON(v), "'", "'\\''")
		fmt.Fprintf(&caseLines,
			"    %d) printf '%%s' '%s' > \"$WS/.harmonik/review.json\" ;;\n",
			iterNum, vj,
		)
	}

	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	//nolint:gocritic // twinPath comes from TwinBinaryPath(); not user input
	twinLine := `  "` + twinPath + `" --scenario single-happy-path >/dev/null 2>&1 || true`

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ $((CNT %% 2)) -eq 0 ]; then
  ITER=$((CNT / 2))
  mkdir -p "$WS/.harmonik"
  case "$ITER" in
%s    *) printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"fallback"}' > "$WS/.harmonik/review.json" ;;
  esac
else
  printf '%%d' "$CNT" > "$WS/impl_iter_$CNT.txt"
  git -C "$WS" add "impl_iter_$CNT.txt" >/dev/null 2>&1
  git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
%s
fi
exit 0
`, wtpEsc, caseLines.String(), twinLine)

	scriptPath := filepath.Join(t.TempDir(), "em015_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("em015FixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// em015FixtureDeps builds the WorkLoopDepsParams for EM-015d/e scenario tests.
// Uses the nil-stdout substrate so watcher=nil (tmux path).
func em015FixtureDeps(t *testing.T, projectDir, scriptPath string, bus *stubEventCollector) daemon.WorkLoopDepsParams {
	t.Helper()
	return daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 bus,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           &em015FixtureNilStdoutSubstrate{},
		AdapterRegistry2:    NewEmptySealedAdapterRegistryForTest(t),
		// AgentReadyTimeout: default (waitAgentReady skipped by empty registry).
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_EM015d_CapHit_RequestChanges3x
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_EM015d_CapHit_RequestChanges3x verifies EM-015d (T-WM-021):
// three consecutive REQUEST_CHANGES verdicts hit the iteration cap (cap=3)
// and close the bead with needs-attention.
//
//	REQUEST_CHANGES (iter 1) → REQUEST_CHANGES (iter 2) → REQUEST_CHANGES (iter 3)
//	→ iteration_cap_hit + review_loop_cycle_complete (cap_hit) + needs-attention
//
// Uses em015FixtureNilStdoutSubstrate so watcher=nil (tmux substrate path).
//
// Note: with the nil-stdout substrate and no Stop-hook relay, each invocation
// waits stopHookGrace (3 s) in waitWithSocketGrace. Six invocations total
// (~18 s overhead). Context timeout is set to 90 s.
//
// Spec refs: specs/execution-model.md §4.3 EM-015d, §4.3 T-WM-021.
// Bead: hk-intln. Refs: EM-015d.
func TestScenario_EM015d_CapHit_RequestChanges3x(t *testing.T) {
	twinPath, twinAvailable := scenariotest.TwinBinaryPath()
	if twinAvailable {
		t.Logf("EM015d CapHit: twin binary found at %q", twinPath)
	} else {
		t.Logf("EM015d CapHit: twin binary absent; using commit-only implementer")
		twinPath = ""
	}

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := em015FixtureHandlerScript(t, wtPath,
		[]string{"REQUEST_CHANGES", "REQUEST_CHANGES", "REQUEST_CHANGES"},
		twinPath,
	)

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(em015FixtureDeps(t, projectDir, scriptPath, collector))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("em015d-cap-hit-001"),
		wtPath, parentSHA,
	)

	// ── Result assertions ────────────────────────────────────────────────────
	if result.Success {
		t.Error("EM-015d cap-hit: expected success=false")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonCapHit) {
		t.Errorf("EM-015d cap-hit: completion_reason = %q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonCapHit)
	}
	if !result.NeedsAttention {
		t.Error("EM-015d cap-hit: expected needs_attention=true")
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()
	t.Logf("EM015d CapHit: emitted events: %v", eventTypes)

	// iteration_cap_hit must be emitted.
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeIterationCapHit))

	// reviewer_launched must appear exactly 3 times (once per iteration).
	launchCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			launchCount++
		}
	}
	if launchCount != 3 {
		t.Errorf("EM-015d cap-hit: reviewer_launched emitted %d times; want 3", launchCount)
	}

	// reviewer_verdict must appear exactly 3 times.
	verdictCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerVerdict) {
			verdictCount++
		}
	}
	if verdictCount != 3 {
		t.Errorf("EM-015d cap-hit: reviewer_verdict emitted %d times; want 3", verdictCount)
	}

	// Ordering: iteration_cap_hit BEFORE review_loop_cycle_complete.
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeIterationCapHit),
		string(core.EventTypeReviewLoopCycleComplete),
	})

	t.Logf("EM015d CapHit PASS: completionReason=%q needsAttention=%v",
		result.CompletionReason, result.NeedsAttention)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_EM015d_Block_ImmediateClose
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_EM015d_Block_ImmediateClose verifies EM-015d (BLOCK path):
// a BLOCK verdict on the first iteration immediately closes the run with
// needs-attention, regardless of the iteration count.
//
//	BLOCK (iter 1) → review_loop_cycle_complete (blocked) + needs-attention
//	                 (implementer NOT resumed — no implementer_resumed event)
//
// Uses em015FixtureNilStdoutSubstrate so watcher=nil (tmux substrate path).
//
// Spec refs: specs/execution-model.md §4.3 EM-015d (BLOCK routing at §9 switch).
// Bead: hk-intln. Refs: EM-015d.
func TestScenario_EM015d_Block_ImmediateClose(t *testing.T) {
	twinPath, twinAvailable := scenariotest.TwinBinaryPath()
	if twinAvailable {
		t.Logf("EM015d Block: twin binary found at %q", twinPath)
	} else {
		t.Logf("EM015d Block: twin binary absent; using commit-only implementer")
		twinPath = ""
	}

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	// Single BLOCK verdict: reviewer writes BLOCK on the first (and only) reviewer call.
	scriptPath := em015FixtureHandlerScript(t, wtPath, []string{"BLOCK"}, twinPath)

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(em015FixtureDeps(t, projectDir, scriptPath, collector))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("em015d-block-001"),
		wtPath, parentSHA,
	)

	// ── Result assertions ────────────────────────────────────────────────────
	if result.Success {
		t.Error("EM-015d BLOCK: expected success=false")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonBlocked) {
		t.Errorf("EM-015d BLOCK: completion_reason = %q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonBlocked)
	}
	if !result.NeedsAttention {
		t.Error("EM-015d BLOCK: expected needs_attention=true")
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()
	t.Logf("EM015d Block: emitted events: %v", eventTypes)

	// reviewer_launched emitted exactly once.
	launchCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			launchCount++
		}
	}
	if launchCount != 1 {
		t.Errorf("EM-015d BLOCK: reviewer_launched emitted %d times; want 1", launchCount)
	}

	// reviewer_verdict emitted exactly once (the BLOCK verdict).
	verdictCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerVerdict) {
			verdictCount++
		}
	}
	if verdictCount != 1 {
		t.Errorf("EM-015d BLOCK: reviewer_verdict emitted %d times; want 1", verdictCount)
	}

	// implementer_resumed must NOT be emitted (BLOCK terminates immediately, no resume).
	for _, et := range eventTypes {
		if et == string(core.EventTypeImplementerResumed) {
			t.Errorf("EM-015d BLOCK: implementer_resumed emitted unexpectedly; BLOCK must terminate without resuming")
		}
	}

	// iteration_cap_hit must NOT be emitted (BLOCK is not a cap-hit).
	for _, et := range eventTypes {
		if et == string(core.EventTypeIterationCapHit) {
			t.Errorf("EM-015d BLOCK: iteration_cap_hit emitted unexpectedly; BLOCK is not a cap-hit")
		}
	}

	// Ordering: reviewer_verdict → review_loop_cycle_complete.
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeReviewerLaunched),
		string(core.EventTypeReviewerVerdict),
		string(core.EventTypeReviewLoopCycleComplete),
	})

	t.Logf("EM015d Block PASS: completionReason=%q needsAttention=%v",
		result.CompletionReason, result.NeedsAttention)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_EM015e_NoProgress_ReviewerNotLaunched
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_EM015e_NoProgress_ReviewerNotLaunched verifies EM-015e (T-WM-022):
// when the diff hash is identical between iteration 1 and iteration 2, the daemon
// emits no_progress_detected and closes the run WITHOUT launching the iter-2 reviewer.
//
//	Iter 1: implementer commits → reviewer issues REQUEST_CHANGES
//	Iter 2: implementer does nothing (same diff) → no_progress_detected
//	        → reviewer NOT launched → review_loop_cycle_complete (no_progress)
//
// Uses em015FixtureNilStdoutSubstrate so watcher=nil (tmux substrate path).
//
// Note: 3 invocations total (impl-1 + reviewer-1 + impl-2); no iter-2 reviewer.
// ~9 s stopHookGrace overhead. Context timeout is 30 s.
//
// Spec refs: specs/execution-model.md §4.3 EM-015e, §4.3 T-WM-022.
// Bead: hk-intln. Refs: EM-015e.
func TestScenario_EM015e_NoProgress_ReviewerNotLaunched(t *testing.T) {
	twinPath, twinAvailable := scenariotest.TwinBinaryPath()
	if twinAvailable {
		t.Logf("EM015e NoProgress: twin binary found at %q", twinPath)
	} else {
		t.Logf("EM015e NoProgress: twin binary absent; using commit-only implementer")
		twinPath = ""
	}

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	// rlFixtureNoProgressHandlerScript: iter-1 implementer commits; iter-1 reviewer
	// issues REQUEST_CHANGES; iter-2 implementer does nothing (no new commit) →
	// diff hash unchanged → no_progress_detected fires before iter-2 reviewer.
	//
	// When the twin binary is available, we patch the implementer phase to also
	// invoke the twin. The script already uses rlFixtureNoProgressHandlerScript's
	// structure; we re-use it when no twin is available.
	var scriptPath string
	if twinPath == "" {
		scriptPath = rlFixtureNoProgressHandlerScript(t, wtPath, "REQUEST_CHANGES")
	} else {
		scriptPath = em015FixtureNoProgressHandlerScriptWithTwin(t, wtPath, twinPath)
	}

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(em015FixtureDeps(t, projectDir, scriptPath, collector))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("em015e-noprogress-001"),
		wtPath, parentSHA,
	)

	// ── Result assertions ────────────────────────────────────────────────────
	if result.Success {
		t.Error("EM-015e no-progress: expected success=false")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonNoProgress) {
		t.Errorf("EM-015e no-progress: completion_reason = %q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonNoProgress)
	}
	if !result.NeedsAttention {
		t.Error("EM-015e no-progress: expected needs_attention=true")
	}

	// ── Event assertions ─────────────────────────────────────────────────────
	eventTypes := collector.eventTypes()
	t.Logf("EM015e NoProgress: emitted events: %v", eventTypes)

	// no_progress_detected must be emitted.
	rlAssertEventPresent(t, eventTypes, string(core.EventTypeNoProgressDetected))

	// reviewer_launched must appear exactly once (iteration 1 only).
	// Iteration 2 reviewer must NOT be launched.
	launchCount := 0
	for _, et := range eventTypes {
		if et == string(core.EventTypeReviewerLaunched) {
			launchCount++
		}
	}
	if launchCount != 1 {
		t.Errorf("EM-015e no-progress: reviewer_launched emitted %d times; want 1 (no iter-2 reviewer)", launchCount)
	}

	// Ordering: no_progress_detected BEFORE review_loop_cycle_complete.
	rlAssertEventSubsequence(t, eventTypes, []string{
		string(core.EventTypeNoProgressDetected),
		string(core.EventTypeReviewLoopCycleComplete),
	})

	t.Logf("EM015e NoProgress PASS: completionReason=%q needsAttention=%v",
		result.CompletionReason, result.NeedsAttention)
}

// ─────────────────────────────────────────────────────────────────────────────
// em015FixtureNoProgressHandlerScriptWithTwin — no-progress variant with twin
// ─────────────────────────────────────────────────────────────────────────────

// em015FixtureNoProgressHandlerScriptWithTwin builds a no-progress handler that
// runs the twin binary on the FIRST implementer invocation (odd invocation #1).
// Subsequent implementers do nothing (no commit), causing the diff hash to
// remain unchanged on iteration 2.
//
// This mirrors the rlFixtureNoProgressHandlerScript contract but adds a twin
// invocation for the implementer phase when the twin binary is available.
func em015FixtureNoProgressHandlerScriptWithTwin(t *testing.T, wtPath, twinPath string) string {
	t.Helper()

	vj := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	//nolint:gocritic // twinPath comes from TwinBinaryPath(); not user input
	twinLine := `  "` + twinPath + `" --scenario single-happy-path >/dev/null 2>&1 || true`

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ $((CNT %% 2)) -eq 0 ]; then
  mkdir -p "$WS/.harmonik"
  printf '%%s' '%s' > "$WS/.harmonik/review.json"
else
  IMPL_NUM=$(((CNT + 1) / 2))
  if [ "$IMPL_NUM" -eq 1 ]; then
    printf '%%d' "$CNT" > "$WS/impl_iter_$CNT.txt"
    git -C "$WS" add "impl_iter_$CNT.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
%s
  fi
fi
exit 0
`, wtpEsc, vj, twinLine)

	scriptPath := filepath.Join(t.TempDir(), "em015_noprogress_twin_handler.sh")
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("em015FixtureNoProgressHandlerScriptWithTwin: WriteFile: %v", err)
	}
	return scriptPath
}
