package daemon_test

// scenario_ssh_localhost_hk8u2al_test.go — L2 SSH-localhost harness.
//
// # Problem reproduced
//
// The L1 harness (hk-52xnr) proved that the remote file-read paths (gate-verdict,
// review-verdict) are correctly routed through the injected CommandRunner. It used a
// fake in-process path-remapping runner (hk52xnrPathRemapRunner) that never touches
// a real transport. L2 replaces that in-process stub with a real SSHRunner pointed
// at localhost, catching the transport-and-quoting bug class (hk-fxy9/hk-538l):
// without SSHRunner's single-quote wrapping, a '#' in a worker path (or in any tmux
// format string like #{pane_id}) is treated as a comment by the remote login shell,
// silently truncating the command.
//
// # Topology
//
//	box A         = projectDir (real git repo + real worktrees under t.TempDir)
//	worker checkout = isolated temp dir seeded as a real git repo; path embeds '#'
//	                  to force shell-quoting to matter (without quoting the '#' would
//	                  start a comment and the cat/stat would target the wrong path)
//	runner        = hk8u2alSSHRemapRunner:
//	  step 1 — remap box-A worktree path → worker checkout (same strip logic as L1)
//	  step 2 — route through SSHRunner{Host:"localhost"} (real ssh binary)
//
// # Isolation axes
//
//	FS axis      : worker checkout is a distinct git repo from box A; verdict files
//	               exist ONLY there (absent on box A so nil-runner tests fail).
//	SSH transport: every cat/stat traverses a real ssh localhost → /bin/sh round-trip,
//	               exercising the single-quote wrapping that hk-fxy9/hk-538l fixed.
//	Quoting axis : the '#' in the worker-checkout directory name triggers the exact
//	               comment-truncation the fix prevents.
//	Separate checkout: worker checkout is git-init'd with an initial commit (a real
//	               checkout, not a plain temp dir).
//	Sandboxed SSH: each test creates its own UserKnownHostsFile (temp file) so the
//	               SSH connections never read or write ~/.ssh/known_hosts.
//	Tmux-socket isolation: not exercised in this file's cat/stat scenarios; the SSH
//	               worker would set TMUX_TMPDIR to a per-test socket dir to avoid
//	               hitting the user's tmux server. Relevant for future L2 tmux tests.
//
// # Skips
//
//	hk8u2alSkipIfSSHLocalhostUnavailable: skips any test in this file when
//	  `ssh localhost true` fails (no sshd / BatchMode passphrase required / firewall).
//	skipRealDaemonE2EInShort: skips the review-loop scenarios under -short (they
//	  run the real reviewloop hook × 2 invocations, ~3-6 s total).
//
// Bead: hk-8u2al. Spec ref: specs/remote-substrate.md L2 (order 2).
// Depends on: hk-52xnr (L1) → hk-hd2w6 (L0).
// Helper prefix: hk8u2alFixture (per implementer-protocol §Helper-prefix discipline).

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
// hk8u2alSSHRemapRunner — path-remap + real SSH transport
// ─────────────────────────────────────────────────────────────────────────────

// hk8u2alSSHRemapRunner is a two-stage CommandRunner:
//  1. Path remap: translates the last argument from a box-A worktree path to the
//     corresponding path inside the worker checkout, preserving the /.harmonik/…
//     suffix (same strip logic as hk52xnrPathRemapRunner in L1).
//  2. SSH transport: routes the remapped command through SSHRunner{Host:"localhost"}
//     so every cat/stat travels through a real ssh binary, exercising shell quoting.
//
// Because it is neither nil nor LocalRunner{}, runnerIsLocalFS classifies it as a
// remote runner, so all Via functions route through it rather than falling back to
// bare os.ReadFile / os.Stat on box A.
type hk8u2alSSHRemapRunner struct {
	worktreesRoot string // <projectDir>/.harmonik/worktrees
	workerDir     string // real git checkout seeded with the expected files
	ssh           tmux.SSHRunner
}

func (r hk8u2alSSHRemapRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	remapped := hk8u2alRemapArgs(r.worktreesRoot, r.workerDir, args)
	return r.ssh.Command(ctx, name, remapped...)
}

// hk8u2alRemapArgs rewrites the last arg if it starts with worktreesRoot+"/":
// strips the common prefix plus the run-scoped directory segment and prepends
// workerDir, preserving the /.harmonik/… suffix.
//
// Example:
//
//	<worktreesRoot>/<runID>-reviewer-1/.harmonik/review.json
//	→ <workerDir>/.harmonik/review.json
func hk8u2alRemapArgs(worktreesRoot, workerDir string, args []string) []string {
	if len(args) == 0 {
		return args
	}
	result := make([]string, len(args))
	copy(result, args)
	last := result[len(result)-1]
	prefix := worktreesRoot + "/"
	if strings.HasPrefix(last, prefix) {
		rest := last[len(prefix):]
		if idx := strings.Index(rest, "/"); idx >= 0 {
			result[len(result)-1] = workerDir + rest[idx:]
		}
	}
	return result
}

// Compile-time assertion: hk8u2alSSHRemapRunner implements tmux.CommandRunner.
var _ tmux.CommandRunner = hk8u2alSSHRemapRunner{}

// ─────────────────────────────────────────────────────────────────────────────
// SSH availability guard
// ─────────────────────────────────────────────────────────────────────────────

// hk8u2alSkipIfSSHLocalhostUnavailable skips t when `ssh localhost true` fails
// within 15 seconds. Reasons include: no sshd running, no authorized key for
// the current user, BatchMode passphrase required, or firewall blocking loopback
// SSH. The skip message includes the combined output so callers can diagnose.
func hk8u2alSkipIfSSHLocalhostUnavailable(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	//nolint:gosec // G204: fixed args; no user input
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "LogLevel=QUIET",
		"localhost", "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("ssh localhost unavailable — skip hk-8u2al SSH tests: %s (%v)",
			strings.TrimSpace(string(out)), err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hk8u2alFixtureProjectSetup creates a minimal project directory with a git repo
// (initial commit on main). Mirrors hk52xnrFixtureProjectSetup.
func hk8u2alFixtureProjectSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{".harmonik/events", ".harmonik/beads-intents"} {
		//nolint:gosec // G301: 0755 matches .harmonik dir conventions
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("hk8u2alFixtureProjectSetup: mkdir %s: %v", sub, err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hk8u2alFixtureProjectSetup: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hk-8u2al ssh-localhost test\n"), 0o644); err != nil {
		t.Fatalf("hk8u2alFixtureProjectSetup: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// hk8u2alFixtureImplWorktree creates a detached git worktree for the implementer
// at HEAD. Returns the worktree path and the parent commit SHA. Mirrors
// hk52xnrFixtureImplWorktree.
func hk8u2alFixtureImplWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()
	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	out, err := headCmd.Output()
	if err != nil {
		t.Fatalf("hk8u2alFixtureImplWorktree: git rev-parse HEAD: %v", err)
	}
	parentSHA = strings.TrimSpace(string(out))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")
	//nolint:gosec // G204: git args are test-internal
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("hk8u2alFixtureImplWorktree: git worktree add: %v\n%s", addErr, addOut)
	}
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk8u2alFixtureImplWorktree: mkdir .harmonik: %v", err)
	}
	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})
	return wtPath, parentSHA
}

// hk8u2alFixtureWorkerCheckout creates an isolated git checkout ("the worker")
// seeded with review.json and gate-verdict.json. The directory name embeds '#' to
// force SSH quoting to matter: without SSHRunner's single-quote wrapping the remote
// shell treats '#' as a comment character and silently truncates the command.
//
// Seeded files:
//
//	.harmonik/review.json       → APPROVE verdict (Scenario A)
//	.harmonik/gate-verdict.json → allow verdict   (Scenario B)
//
// Neither file exists on box A; nil-runner reads against box-A paths therefore fail.
func hk8u2alFixtureWorkerCheckout(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	// '#' in the directory name: without SSHRunner quoting the remote shell
	// reads '#hk8u2al' as a comment → cat/stat targets the parent dir → wrong.
	dir := filepath.Join(parent, "worker#hk8u2al")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk8u2alFixtureWorkerCheckout: mkdir .harmonik: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hk8u2alFixtureWorkerCheckout: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hk-8u2al worker checkout\n"), 0o644); err != nil {
		t.Fatalf("hk8u2alFixtureWorkerCheckout: WriteFile README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(
		filepath.Join(dir, ".harmonik", "review.json"),
		[]byte(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"hk-8u2al ssh-localhost verdict"}`),
		0o644,
	); err != nil {
		t.Fatalf("hk8u2alFixtureWorkerCheckout: write review.json: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(
		filepath.Join(dir, ".harmonik", "gate-verdict.json"),
		[]byte(`{"schema_version":1,"decision":"allow","reason":"hk-8u2al ssh-localhost gate"}`),
		0o644,
	); err != nil {
		t.Fatalf("hk8u2alFixtureWorkerCheckout: write gate-verdict.json: %v", err)
	}
	return dir
}

// hk8u2alFixtureSSHRunner returns an SSHRunner targeting localhost with options
// that prevent interactive prompts and sandbox the known_hosts file to a per-test
// temp dir (never touches ~/.ssh/known_hosts).
func hk8u2alFixtureSSHRunner(t *testing.T) tmux.SSHRunner {
	t.Helper()
	// Per-test known_hosts file: isolates this connection from the user's SSH config.
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	return tmux.SSHRunner{
		Host: "localhost",
		Opts: []string{
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "UserKnownHostsFile=" + knownHosts,
			"-o", "ConnectTimeout=10",
			"-o", "LogLevel=QUIET",
		},
	}
}

// hk8u2alFixtureGatePaths creates (worktreesRoot, verdictPath) for Scenario B.
// verdictPath lives under worktreesRoot so the path-remap step fires; it does NOT
// exist on disk (only the worker checkout has gate-verdict.json).
func hk8u2alFixtureGatePaths(t *testing.T) (worktreesRoot, verdictPath string) {
	t.Helper()
	projectDir := t.TempDir()
	worktreesRoot = filepath.Join(projectDir, ".harmonik", "worktrees")
	fakeWtPath := filepath.Join(worktreesRoot, "test-wt-hk8u2al")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(fakeWtPath, 0o755); err != nil {
		t.Fatalf("hk8u2alFixtureGatePaths: MkdirAll: %v", err)
	}
	verdictPath = filepath.Join(fakeWtPath, ".harmonik", "gate-verdict.json")
	if _, err := os.Stat(verdictPath); err == nil {
		t.Fatal("hk8u2alFixtureGatePaths: precondition failed: gate-verdict.json must not exist at box-A path")
	}
	return worktreesRoot, verdictPath
}

// hk8u2alFixtureRunID generates a fresh UUIDv7-based RunID.
func hk8u2alFixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hk8u2alFixtureRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// hk8u2alFixtureHandlerScript writes a shell handler that alternates roles:
//   - Odd invocations  (count=1, 3, …): implementer — commits one file to wtPath.
//   - Even invocations (count=2, 4, …): reviewer   — exits 0 without writing any
//     verdict. review.json is seeded ONLY in the worker checkout, so nil-runner →
//     verdict-absent (failure) and SSHRemapRunner → APPROVE (success).
func hk8u2alFixtureHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
WTP='` + wtPath + `'
CNT_FILE="$WTP/.harmonik/hk8u2al_count"
if [ ! -f "$CNT_FILE" ]; then
  printf '0' > "$CNT_FILE"
fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%d' "$CNT" > "$CNT_FILE"
if [ $((CNT % 2)) -eq 0 ]; then
  # Reviewer: exit without writing any verdict file.
  # review.json lives ONLY in the worker checkout; nil-runner can't find it;
  # SSHRemapRunner maps the reviewer-wt path to workerDir where APPROVE is seeded.
  exit 0
else
  # Implementer: commit one file to the implementer worktree.
  printf 'impl-%d' "$CNT" > "$WTP/hk8u2al_impl.txt"
  git -C "$WTP" add "hk8u2al_impl.txt" >/dev/null 2>&1
  git -C "$WTP" -c user.email=test@harmonik.local -c user.name="Test" commit \
    -m "hk-8u2al impl $CNT" --no-gpg-sign >/dev/null 2>&1
  exit 0
fi
`
	scriptPath := filepath.Join(t.TempDir(), "hk8u2al_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("hk8u2alFixtureHandlerScript: WriteFile: %v", err)
	}
	return scriptPath
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — gate-verdict class: GateVerdictExists
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_SSHLocalhost_GateVerdictExists_NilRunner_hk8u2al is the NEGATIVE
// GUARD for Scenario B. gate-verdict.json exists ONLY in the worker checkout. With
// nil runner, gateVerdictExistsVia falls back to os.Stat on the box-A path →
// returns false (file absent). If this ever returns true the seam is broken.
func TestScenario_SSHLocalhost_GateVerdictExists_NilRunner_hk8u2al(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk8u2alFixtureGatePaths(t)

	got := daemon.ExportedGateVerdictExistsVia(context.Background(), nil, verdictPath)
	if got {
		t.Error("SSHLocalhost GateVerdictExists NilRunner: returned true; want false (file absent on box A)")
	}
}

// TestScenario_SSHLocalhost_GateVerdictExists_SSHRunner_hk8u2al: the SSHRemapRunner
// translates the box-A path to the worker checkout (via path remap) then executes
// `test -s` over ssh localhost. gate-verdict.json IS seeded there → returns true.
// The '#' in the worker checkout path verifies SSHRunner's single-quote wrapping
// prevents comment-truncation on the remote shell.
func TestScenario_SSHLocalhost_GateVerdictExists_SSHRunner_hk8u2al(t *testing.T) {
	t.Parallel()
	hk8u2alSkipIfSSHLocalhostUnavailable(t)

	worktreesRoot, verdictPath := hk8u2alFixtureGatePaths(t)
	workerDir := hk8u2alFixtureWorkerCheckout(t)

	runner := hk8u2alSSHRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
		ssh:           hk8u2alFixtureSSHRunner(t),
	}

	got := daemon.ExportedGateVerdictExistsVia(context.Background(), runner, verdictPath)
	if !got {
		t.Error("SSHLocalhost GateVerdictExists SSHRunner: returned false; want true (seeded in worker checkout)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — gate-verdict class: ReadGateVerdict
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_SSHLocalhost_ReadGateVerdict_NilRunner_hk8u2al: nil runner →
// readGateVerdictVia falls back to os.ReadFile on box A → file absent → error.
func TestScenario_SSHLocalhost_ReadGateVerdict_NilRunner_hk8u2al(t *testing.T) {
	t.Parallel()

	_, verdictPath := hk8u2alFixtureGatePaths(t)

	_, err := daemon.ExportedReadGateVerdictVia(context.Background(), nil, verdictPath)
	if err == nil {
		t.Error("SSHLocalhost ReadGateVerdict NilRunner: expected error (file absent on box A); got nil")
	}
}

// TestScenario_SSHLocalhost_ReadGateVerdict_SSHRunner_hk8u2al: the SSHRemapRunner
// translates the box-A path to the worker checkout, then runs
// `cat <worker-path>` over ssh localhost. The file IS seeded → parse succeeds →
// returns GateActionAllow.
func TestScenario_SSHLocalhost_ReadGateVerdict_SSHRunner_hk8u2al(t *testing.T) {
	t.Parallel()
	hk8u2alSkipIfSSHLocalhostUnavailable(t)

	worktreesRoot, verdictPath := hk8u2alFixtureGatePaths(t)
	workerDir := hk8u2alFixtureWorkerCheckout(t)

	runner := hk8u2alSSHRemapRunner{
		worktreesRoot: worktreesRoot,
		workerDir:     workerDir,
		ssh:           hk8u2alFixtureSSHRunner(t),
	}

	action, err := daemon.ExportedReadGateVerdictVia(context.Background(), runner, verdictPath)
	if err != nil {
		t.Fatalf("SSHLocalhost ReadGateVerdict SSHRunner: unexpected error: %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("SSHLocalhost ReadGateVerdict SSHRunner: action=%q; want %q", action, core.GateActionAllow)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — review-verdict class (AC3A negative guard + AC5)
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_SSHLocalhost_ReviewVerdict_NilRunner_hk8u2al is the NEGATIVE GUARD
// (AC5). The reviewer handler exits 0 without writing review.json; the file is
// seeded ONLY in the worker checkout. With nil runner ReadReviewVerdictVia falls
// back to os.ReadFile on the box-A reviewer worktree → verdict absent → failure.
//
// If this test ever succeeds the seam is broken.
func TestScenario_SSHLocalhost_ReviewVerdict_NilRunner_hk8u2al(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := hk8u2alFixtureProjectSetup(t)
	wtPath, parentSHA := hk8u2alFixtureImplWorktree(t, projectDir)
	_ = hk8u2alFixtureWorkerCheckout(t) // seeded but NOT reachable via nil runner
	scriptPath := hk8u2alFixtureHandlerScript(t, wtPath)

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
		hk8u2alFixtureRunID(t),
		core.BeadID("hk-8u2al-nil-runner-negative-guard"),
		wtPath, parentSHA,
		nil, // nil runner → os.ReadFile on box-A → verdict absent → failure
	)

	if result.Success {
		t.Fatal("SSHLocalhost ReviewVerdict NilRunner: expected failure (verdict absent) but got success — " +
			"seam is broken: nil runner silently found the worker-side verdict")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("SSHLocalhost ReviewVerdict NilRunner: CompletionReason=%q; want %q",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
}

// TestScenario_SSHLocalhost_ReviewVerdict_SSHRunnerBypassed_hk8u2al locks in the
// box-A-local reviewer-verdict contract established by hk-fxy9/hk-177oz/hk-1hgjr.
// Since fix-D the reviewer worktree is created on box A, and the daemon reads
// review.json via workspace.ReadReviewVerdictLocalRetry with a NIL runner
// (reviewloop.go): the per-run worker runner is DELIBERATELY bypassed for the
// reviewer verdict, because routing it as `ssh <worker> cat <box-A-path>`
// truncates under concurrent ControlMaster churn (hk-cnp17 class).
//
// This test proves the bypass under a REAL remote runner: an SSHRemapRunner that
// WOULD serve APPROVE from the worker checkout (where review.json is seeded) is
// injected, yet the run still FAILS with verdict-absent — the reviewer verdict is
// read box-A-local, where the handler script's reviewer role writes nothing. A
// regression that re-routes the reviewer-verdict read back through the worker
// runner would surface the worker APPROVE and flip this to success, catching the
// seam re-introduction.
//
// (Superseded contract: this test previously asserted the OPPOSITE — that the
// SSHRunner routed the reviewer-verdict read to the worker checkout for a success.
// That seam was removed by hk-fxy9; the SSH transport + '#'-quoting path is still
// positively exercised by the sibling gate-verdict SSHRunner tests, which DO
// route via the runner. The '#' in the worker checkout dir name is retained so
// this test keeps sharing that fixture. Bead: hk-vbkv1.)
func TestScenario_SSHLocalhost_ReviewVerdict_SSHRunnerBypassed_hk8u2al(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()
	hk8u2alSkipIfSSHLocalhostUnavailable(t)

	projectDir := hk8u2alFixtureProjectSetup(t)
	wtPath, parentSHA := hk8u2alFixtureImplWorktree(t, projectDir)
	workerDir := hk8u2alFixtureWorkerCheckout(t) // seeds APPROVE in the worker checkout…
	scriptPath := hk8u2alFixtureHandlerScript(t, wtPath)

	runner := hk8u2alSSHRemapRunner{
		worktreesRoot: filepath.Join(projectDir, ".harmonik", "worktrees"),
		workerDir:     workerDir,
		ssh:           hk8u2alFixtureSSHRunner(t),
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
		hk8u2alFixtureRunID(t),
		core.BeadID("hk-8u2al-ssh-runner-bypassed"),
		wtPath, parentSHA,
		runner, // …but the reviewer verdict is read box-A-local, so this runner is NOT consulted for it.
	)

	// The injected worker runner must NOT rescue the run: the reviewer verdict is
	// read box-A-local (absent) → verdict-absent failure, per hk-177oz.
	if result.Success {
		t.Fatalf("SSHLocalhost ReviewVerdict SSHRunnerBypassed: expected FAILURE — the reviewer verdict " +
			"is read box-A-local (hk-fxy9/hk-177oz) so the injected worker runner's APPROVE must not be " +
			"consulted; success means the reviewer-verdict read regressed to routing through the runner")
	}
	if result.CompletionReason != string(core.ReviewLoopCompletionReasonError) {
		t.Errorf("SSHLocalhost ReviewVerdict SSHRunnerBypassed: CompletionReason=%q; want %q (verdict absent)",
			result.CompletionReason, core.ReviewLoopCompletionReasonError)
	}
}
