package daemon_test

// scenario_fs_separation_hk52xnr_test.go — deterministic FS-separation harness.
//
// # Problem reproduced
//
// On a remote run the reviewer and cognition-gate subprocesses write their output
// files (review.json, gate-verdict.json) on the WORKER filesystem. A bare
// os.ReadFile / os.Stat on box A therefore returns "not found", causing
// false-failures ("verdict absent", gate never opens) that L0 (hk-hd2w6) fixed by
// routing those reads through the run's CommandRunner.
//
// # Topology
//
// box A  = real git repo + real worktrees (t.TempDir)
// worker = plain temp dir seeded with the expected files (no SSH/LLM/tmux)
// runner = hk52xnrPathRemapRunner — rewrites box-A worktree paths to workerDir
//
// Each test pair runs ~3–6 s total (reviewloop hook-grace × 2 calls).
//
// # Scenario A — review-verdict class (AC5 negative guard + hk-177oz)
//
// The reviewer handler exits 0 WITHOUT writing review.json locally. The file is
// seeded ONLY on the worker:
//   - nil runner   → ReadReviewVerdictVia reads os.ReadFile on box-A
//                    → verdict absent → result.Success == false  (NEGATIVE GUARD)
//   - path runner  → hk-177oz: the reviewer verdict read is now box-A-local
//                    UNCONDITIONALLY (the reviewer worktree lives on box A since
//                    fix-D / hk-fxy9), so runReviewLoop reads revWtPath with a nil
//                    runner regardless of the per-run runner. A worker-only verdict
//                    stays unreachable → result.Success == false even WITH a runner
//                    (regression guard: the verdict read must NOT honor the runner).
//                    The gate-verdict class (Scenario B) still routes via the
//                    runner — that seam is unchanged.
//
// # Scenario B — gate-verdict class (AC3B)
//
// gate-verdict.json seeded ONLY on the worker; no subprocess overhead:
//   - nil runner   → gateVerdictExistsVia / readGateVerdictVia hit box-A
//                    → false / error
//   - path runner  → routed to workerDir → true / GateActionAllow
//
// Bead: hk-52xnr. Spec ref: specs/remote-substrate.md AC3A/AC3B/AC5.
// Helper prefix: hk52xnrFixture (bead hk-52xnr, per implementer-protocol §Helper-prefix discipline).

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
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// hk52xnrPathRemapRunner — box-A worktree path → worker dir translation
// ─────────────────────────────────────────────────────────────────────────────

// hk52xnrPathRemapRunner is a non-local CommandRunner that translates the last
// path argument of every Command call: strips the common worktreesRoot prefix
// plus the run-scoped directory segment and prepends workerDir, preserving the
// /.harmonik/… suffix.
//
// Example:
//
//	<worktreesRoot>/<runID>-reviewer-1/.harmonik/review.json
//	→ <workerDir>/.harmonik/review.json
//
// Being a distinct (non-LocalRunner) type it is classified as non-local by
// runnerIsLocalFS, so Via functions route through it instead of falling back to
// os.ReadFile/os.Stat on box A.
type hk52xnrPathRemapRunner struct {
	worktreesRoot string // <projectDir>/.harmonik/worktrees
	workerDir     string // plain temp dir seeded with the expected files
}

func (r hk52xnrPathRemapRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	remapped := make([]string, len(args))
	copy(remapped, args)
	if len(remapped) > 0 {
		last := remapped[len(remapped)-1]
		prefix := r.worktreesRoot + "/"
		if strings.HasPrefix(last, prefix) {
			rest := last[len(prefix):]
			if idx := strings.Index(rest, "/"); idx >= 0 {
				// suffix = "/.harmonik/…" — the part after the run-scoped name
				remapped[len(remapped)-1] = r.workerDir + rest[idx:]
			}
		}
	}
	//nolint:gosec // G204: args are test-controlled paths, not user input
	return exec.CommandContext(ctx, name, remapped...)
}

// Compile-time assertion: hk52xnrPathRemapRunner implements tmux.CommandRunner.
var _ tmux.CommandRunner = hk52xnrPathRemapRunner{}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures — shared helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk52xnrFixtureProjectSetup creates a minimal project directory with a git repo
// (initial commit on main). Layout mirrors nilwatcherFixtureProjectSetup so
// runReviewLoop can create reviewer worktrees under
// <projectDir>/.harmonik/worktrees/. Returns the project dir path.
func hk52xnrFixtureProjectSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		//nolint:gosec // G301: 0755 matches .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("hk52xnrFixtureProjectSetup: mkdir %s: %v", sub, err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hk52xnrFixtureProjectSetup: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hk-52xnr fs-isolation test\n"), 0o644); err != nil {
		t.Fatalf("hk52xnrFixtureProjectSetup: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// hk52xnrFixtureImplWorktree creates a detached git worktree for the implementer
// at HEAD. Returns the worktree path and the parent commit SHA.
func hk52xnrFixtureImplWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("hk52xnrFixtureImplWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("hk52xnrFixtureImplWorktree: git worktree add: %v\n%s", addErr, addOut)
	}
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk52xnrFixtureImplWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// hk52xnrFixtureWorkerDir creates a plain temp dir simulating the remote worker
// filesystem. Seeds:
//   - .harmonik/review.json       → APPROVE verdict (Scenario A)
//   - .harmonik/gate-verdict.json → allow verdict   (Scenario B)
//
// Neither file exists on box A, so nil-runner reads return absent/error.
func hk52xnrFixtureWorkerDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk52xnrFixtureWorkerDir: mkdir .harmonik: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(
		filepath.Join(dir, ".harmonik", "review.json"),
		[]byte(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"hk-52xnr worker verdict"}`),
		0o644,
	); err != nil {
		t.Fatalf("hk52xnrFixtureWorkerDir: write review.json: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(
		filepath.Join(dir, ".harmonik", "gate-verdict.json"),
		[]byte(`{"schema_version":1,"decision":"allow","reason":"hk-52xnr worker gate"}`),
		0o644,
	); err != nil {
		t.Fatalf("hk52xnrFixtureWorkerDir: write gate-verdict.json: %v", err)
	}
	return dir
}

// hk52xnrFixtureHandlerScript writes a shell handler:
//   - Odd invocations  (count=1, 3, …): implementer — commits one file to WTP.
//   - Even invocations (count=2, 4, …): reviewer   — exits 0 without writing
//     any verdict. The verdict is seeded ONLY on the worker dir so nil-runner →
//     verdict-absent (false-fail) and injected runner → APPROVE (pass).
func hk52xnrFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
CNT_FILE="$WTP/.harmonik/hk52xnr_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  # Reviewer: exit without writing any verdict file.
  # review.json lives ONLY on the worker; nil-runner can't find it; injected
  # runner remaps the revWtPath read to workerDir where APPROVE is seeded.
  exit 0
else
  # Implementer: commit one file to the implementer worktree.
  printf 'impl-%d' "$CNT" > "$WTP/hk52xnr_impl.txt"
  git -C "$WTP" add "hk52xnr_impl.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit \
    -m "hk-52xnr impl $CNT" --no-gpg-sign >/dev/null 2>&1
  exit 0
fi
`
	scriptPath := filepath.Join(t.TempDir(), "hk52xnr_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("hk52xnrFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// hk52xnrFixtureRunID generates a fresh UUIDv7-based RunID.
func hk52xnrFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hk52xnrFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// hk52xnrFixtureGatePaths creates (worktreesRoot, fakeWtPath, verdictPath) for
// Scenario B. fakeWtPath lives under worktreesRoot so the remapping runner's
// strip-then-prepend logic fires. verdictPath does NOT exist on disk.
func hk52xnrFixtureGatePaths(t *testing.T) (worktreesRoot, verdictPath string) {
	t.Helper()
	projectDir := t.TempDir()
	worktreesRoot = filepath.Join(projectDir, ".harmonik", "worktrees")
	fakeWtPath := filepath.Join(worktreesRoot, "test-wt-hk52xnr")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(fakeWtPath, 0o755); err != nil {
		t.Fatalf("hk52xnrFixtureGatePaths: MkdirAll: %v", err)
	}
	verdictPath = filepath.Join(fakeWtPath, ".harmonik", "gate-verdict.json")
	// Precondition: file must NOT exist on box A.
	if _, err := os.Stat(verdictPath); err == nil {
		t.Fatal("hk52xnrFixtureGatePaths: precondition failed: gate-verdict.json must not exist at box-A path")
	}
	return worktreesRoot, verdictPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — review-verdict class (AC3A + AC5 negative guard)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FSIsolation_ReviewVerdict_NilRunner_hk52xnr is the NEGATIVE GUARD
// (AC5). The reviewer exits 0 without writing review.json; the file is seeded
// ONLY on the worker. With nil runner ReadReviewVerdictVia falls back to
// os.ReadFile on the box-A reviewer worktree → verdict absent → success == false.
//
// If this test ever succeeds (i.e. the nil-runner path somehow finds the worker
// verdict), the seam is broken — that would mask regressions in the Via routing.
func TestScenario_FSIsolation_ReviewVerdict_NilRunner_hk52xnr(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := hk52xnrFixtureProjectSetup(t)
	wtPath, parentSHA := hk52xnrFixtureImplWorktree(t, projectDir)
	_ = hk52xnrFixtureWorkerDir(t) // seeded but NOT reachable via nil runner
	scriptPath := hk52xnrFixtureHandlerScript(t, wtPath)

	// Reuse the nil-stdout substrate from the nil-watcher test: runs the real
	// handler Argv as a subprocess, returns Stdout() == nil → watcher=nil path.
	sub := &nilwatcherFixtureNilStdoutSubstrate{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           sub,
		// Empty sealed registry: ForAgent returns error → waitAgentReady skipped.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoopWithRunner(
		ctx, deps,
		hk52xnrFixtureRunID(t),
		core.BeadID("hk-52xnr-nil-runner-negative-guard"),
		wtPath, parentSHA,
		nil, // nil runner → os.ReadFile on box-A → verdict absent → failure
	)

	if result.Success {
		t.Fatal("FSIsolation NilRunner: expected failure (verdict absent) but got success — " +
			"seam is broken: nil runner silently found the worker-side verdict")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("FSIsolation NilRunner: CompletionReason=%q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
}

// TestScenario_FSIsolation_ReviewVerdict_WithRunner_hk52xnr is the hk-177oz
// regression guard. Post fix-D (hk-fxy9) the reviewer worktree (revWtPath) is
// always box-A-local, so runReviewLoop reads the verdict with a NIL runner even
// when a per-run worker runner is present — reading a box-A-local file over the
// worker's SSH transport (`ssh <worker> cat <box-A-path>`) truncates under
// concurrent ControlMaster churn (the concurrent review.json ErrMalformed bug).
// Here review.json is seeded ONLY on the worker; injecting a fully-working
// path-remapping runner (the same runner Scenario B proves DOES route gate reads)
// must NOT make the verdict reachable — the verdict read ignores it → box-A read →
// verdict absent → result.Success == false. If this ever flips to success, the
// verdict read has regressed to honoring the runner again.
func TestScenario_FSIsolation_ReviewVerdict_WithRunner_hk52xnr(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := hk52xnrFixtureProjectSetup(t)
	wtPath, parentSHA := hk52xnrFixtureImplWorktree(t, projectDir)
	workerDir := hk52xnrFixtureWorkerDir(t)
	scriptPath := hk52xnrFixtureHandlerScript(t, wtPath)

	runner := hk52xnrPathRemapRunner{
		worktreesRoot: filepath.Join(projectDir, ".harmonik", "worktrees"),
		workerDir:     workerDir,
	}

	sub := &nilwatcherFixtureNilStdoutSubstrate{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 &stubEventCollector{},
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		Substrate:           sub,
		AdapterRegistry2:    NewEmptySealedAdapterRegistryForTest(t),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoopWithRunner(
		ctx, deps,
		hk52xnrFixtureRunID(t),
		core.BeadID("hk-52xnr-runner-approve"),
		wtPath, parentSHA,
		runner, // hk-177oz: verdict read ignores this runner (box-A-local read)
	)

	if result.Success {
		t.Fatalf("FSIsolation WithRunner (hk-177oz): expected failure (verdict absent — "+
			"the box-A-local verdict read must ignore the worker runner) but got success; summary=%q",
			result.Summary)
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("FSIsolation WithRunner (hk-177oz): CompletionReason=%q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — gate-verdict class (AC3B)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_FSIsolation_GateVerdictExists_NilRunner_hk52xnr: nil runner →
// gateVerdictExistsVia falls back to os.Stat on the box-A path → file absent →
// returns false.
func TestScenario_FSIsolation_GateVerdictExists_NilRunner_hk52xnr(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk52xnrFixtureGatePaths(t)

	got := daemon.ExportedGateVerdictExistsVia(context.Background(), nil, verdictPath)
	if got {
		t.Error("FSIsolation GateVerdictExists NilRunner: returned true; want false (file absent on box A)")
	}
}

// TestScenario_FSIsolation_GateVerdictExists_WithRunner_hk52xnr: the
// path-remapping runner translates the box-A verdict path to the worker dir
// where gate-verdict.json IS seeded → gateVerdictExistsVia returns true.
func TestScenario_FSIsolation_GateVerdictExists_WithRunner_hk52xnr(t *testing.T) {
	t.Parallel()

	worktreesRoot, verdictPath := hk52xnrFixtureGatePaths(t)
	workerDir := hk52xnrFixtureWorkerDir(t)

	runner := hk52xnrPathRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
	}

	got := daemon.ExportedGateVerdictExistsVia(context.Background(), runner, verdictPath)
	if !got {
		t.Error("FSIsolation GateVerdictExists WithRunner: returned false; want true (file seeded on worker)")
	}
}

// TestScenario_FSIsolation_ReadGateVerdict_NilRunner_hk52xnr: nil runner →
// readGateVerdictVia delegates to readGateVerdict (os.ReadFile) → file absent
// on box A → returns an error.
func TestScenario_FSIsolation_ReadGateVerdict_NilRunner_hk52xnr(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk52xnrFixtureGatePaths(t)

	_, err := daemon.ExportedReadGateVerdictVia(context.Background(), nil, verdictPath)
	if err == nil {
		t.Error("FSIsolation ReadGateVerdict NilRunner: expected error (file absent on box A); got nil")
	}
}

// TestScenario_FSIsolation_ReadGateVerdict_WithRunner_hk52xnr: the
// path-remapping runner routes the cat call to the worker dir → parse succeeds →
// returns GateActionAllow.
func TestScenario_FSIsolation_ReadGateVerdict_WithRunner_hk52xnr(t *testing.T) {
	t.Parallel()

	worktreesRoot, verdictPath := hk52xnrFixtureGatePaths(t)
	workerDir := hk52xnrFixtureWorkerDir(t)

	runner := hk52xnrPathRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
	}

	action, err := daemon.ExportedReadGateVerdictVia(context.Background(), runner, verdictPath)
	if err != nil {
		t.Fatalf("FSIsolation ReadGateVerdict WithRunner: unexpected error: %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("FSIsolation ReadGateVerdict WithRunner: action=%q; want %q", action, core.GateActionAllow)
	}
}
