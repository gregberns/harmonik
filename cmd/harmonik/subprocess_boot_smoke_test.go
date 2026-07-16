//go:build subprocess

package main

// subprocess_boot_smoke_test.go — WS2.4 non-docker subprocess daemon-boot smoke.
//
// This is the independent, non-docker leg of M6-PLAN §WS2.4: a smoke test that
// execs the *real built* `harmonik` binary as a separate OS process (not an
// in-process daemon.Start), waits for the daemon unix socket to appear, submits
// ONE bead through the real CLI (`harmonik queue submit`), and asserts the
// daemon drives that bead's run to a TERMINAL outcome (run_completed /
// run_failed / bead_closed) — proving the whole subprocess boot → socket →
// CLI-submit → dispatch → terminal-signal pipeline works end-to-end.
//
// Billing-free substrate (LOCKED: generic-twin). The real binary exposes no
// in-process HandlerBinary seam; its only composition-root substrate-swap is the
// structured Codex driver (HARMONIK_SUBSTRATE=codexdriver + --default-harness
// codex + --codex-binary). We point --codex-binary at the built `generic-twin`
// binary. The generic twin speaks harmonik-native NDJSON, not the Codex
// app-server wire protocol, so the driver's handshake fails fast and the run
// reaches a terminal outcome (run_failed) WITHOUT ever launching tmux, a real
// Claude/Codex agent, or touching the network — deterministic and zero-token.
// This isolates exactly what WS2.4's non-docker leg is chartered to cover: that
// the real binary boots as a subprocess and drives a CLI-submitted bead to a
// terminal run state. (A clean bead-close through the real binary is the docker
// / WS2.3 containerized variant's job and is out of scope here.)
//
// Behind the dedicated `subprocess` build tag (LOCKED: a new tag, NOT a reuse of
// `scenario`) so the default `go build ./...` / `go test ./...` never compile or
// run it. Idioms reused verbatim:
//   - go-build-a-cmd-binary: test/scenario/harness_test.go:78 scenarioFixtureBuildTwin
//   - wait-for-socket (os.ModeSocket + 0600): test/scenario harness socket poll
//   - project scaffold (git repo + bare origin + br init):
//     internal/daemon/scenario_queue_submit_dispatch_hksk00a_test.go:57,80,178
//   - terminal-event JSONL poll:
//     internal/daemon/scenario_queue_submit_dispatch_hksk00a_test.go:251
//
// Plan ref: plans/2026-07-13-code-revamp/M6-PLAN.md §WS2.4.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSubprocessDaemonBootSmoke is the WS2.4 non-docker smoke.
func TestSubprocessDaemonBootSmoke(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain required: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for subprocess boot smoke (not on PATH)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required for subprocess boot smoke (not on PATH)")
	}

	moduleRoot := subprocessSmokeModuleRoot(t, goTool)

	// Build the real harmonik binary and the generic twin into a temp dir.
	binDir := t.TempDir()
	harmonikBin := subprocessSmokeBuild(t, goTool, moduleRoot, binDir,
		"harmonik", "github.com/gregberns/harmonik/cmd/harmonik")
	genericTwinBin := subprocessSmokeBuild(t, goTool, moduleRoot, binDir,
		"generic-twin", "github.com/gregberns/harmonik/cmd/harmonik-twin-generic")

	// Minimal project scaffold: short socket-safe dir + .harmonik subtree +
	// git repo with a bare origin + a br workspace holding one open bead.
	projectDir := subprocessSmokeProjectDir(t)
	subprocessSmokeGitRepo(t, projectDir)
	beadID := subprocessSmokeInitBr(t, brPath, projectDir)

	jsonlPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")

	// Launch the real binary as a separate process. The codexdriver substrate
	// with the generic twin keeps dispatch billing-free and tmux-free.
	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	//nolint:gosec // G204: harmonikBin is a test-built binary; args are literals.
	daemonCmd := exec.CommandContext(daemonCtx, harmonikBin, "--project", projectDir)
	daemonCmd.Dir = projectDir
	daemonCmd.Env = append(os.Environ(),
		"HARMONIK_SUBSTRATE=codexdriver",
	)
	// --default-harness / --codex-binary are daemon-path flags, appended as args.
	daemonCmd.Args = append(daemonCmd.Args,
		"--default-harness", "codex",
		"--codex-binary", genericTwinBin,
	)
	var daemonOut strings.Builder
	daemonCmd.Stdout = &daemonOut
	daemonCmd.Stderr = &daemonOut
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start harmonik daemon subprocess: %v", err)
	}
	// Clean teardown: cancel (SIGKILL via CommandContext) + reap.
	t.Cleanup(func() {
		cancelDaemon()
		_ = daemonCmd.Wait()
		if t.Failed() {
			t.Logf("daemon subprocess output:\n%s", daemonOut.String())
		}
	})

	// Wait for the daemon socket to appear (proves the subprocess booted).
	if !subprocessSmokeWaitForSocket(t, sockPath, 30*time.Second) {
		t.Fatalf("daemon socket %s did not appear within 30s\ndaemon output:\n%s",
			sockPath, daemonOut.String())
	}

	// Submit ONE bead via the real CLI.
	//nolint:gosec // G204: harmonikBin test-built; beadID from br create.
	submitCmd := exec.CommandContext(daemonCtx, harmonikBin,
		"queue", "submit", "--project", projectDir, "--beads", beadID)
	submitCmd.Dir = projectDir
	if out, err := submitCmd.CombinedOutput(); err != nil {
		t.Fatalf("queue submit failed: %v\n%s", err, out)
	}

	// Assert the run reaches a terminal outcome.
	if !subprocessSmokeWaitTerminal(t, jsonlPath, 90*time.Second) {
		t.Fatalf("no terminal run event (run_completed/run_failed/bead_closed) within 90s\n"+
			"jsonl=%s\ndaemon output:\n%s", jsonlPath, daemonOut.String())
	}
}

// subprocessSmokeModuleRoot derives the module root from `go env GOMOD`
// (mirrors test/scenario/harness_test.go:84).
func subprocessSmokeModuleRoot(t *testing.T, goTool string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	//nolint:gosec // G204: goTool from LookPath.
	cmd := exec.Command(goTool, "env", "GOMOD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// subprocessSmokeBuild builds pkg into binDir/name and returns the binary path
// (mirrors scenarioFixtureBuildTwin, test/scenario/harness_test.go:103).
func subprocessSmokeBuild(t *testing.T, goTool, moduleRoot, binDir, name, pkg string) string {
	t.Helper()
	binPath := filepath.Join(binDir, name)
	//nolint:gosec // G204: goTool from LookPath; pkg is a literal import path.
	cmd := exec.Command(goTool, "build", "-o", binPath, pkg)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, out)
	}
	return binPath
}

// subprocessSmokeProjectDir creates a socket-safe project dir with the minimal
// .harmonik subtree (mirrors scenarioFixtureProjectDir + the queue-submit
// scenario layout). Uses /tmp so the unix socket path stays under macOS's
// 104-byte sun_path limit.
func subprocessSmokeProjectDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hk-subproc-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(resolved) })
	for _, sub := range []string{
		filepath.Join(".harmonik", "events"),
		filepath.Join(".harmonik", "beads-intents"),
		filepath.Join(".harmonik", "queues"),
	} {
		//nolint:gosec // G301: 0755 matches .harmonik dir conventions.
		if err := os.MkdirAll(filepath.Join(resolved, sub), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", sub, err)
		}
	}
	return resolved
}

// subprocessSmokeGitRepo initialises a git repo with one commit + a bare origin
// (mirrors queueSubmitDispatchGitRepo, scenario_queue_submit...:80).
func subprocessSmokeGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("subprocess boot smoke\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")

	originDir, err := os.MkdirTemp("/tmp", "hk-subproc-origin-")
	if err != nil {
		t.Fatalf("MkdirTemp origin: %v", err)
	}
	originDir, err = filepath.EvalSymlinks(originDir)
	if err != nil {
		t.Fatalf("EvalSymlinks origin: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(originDir) })
	initBare := exec.Command("git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBare.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	run("remote", "add", "origin", originDir)
	run("push", "origin", "main")
}

// subprocessSmokeInitBr runs `br init` + `br create` in projectDir and returns
// the new bead's ID (mirrors queueSubmitDispatchInitBr, scenario...:178). The
// real daemon runs br with cmd.Dir=projectDir, so it shares this .beads DB.
func subprocessSmokeInitBr(t *testing.T, brPath, projectDir string) string {
	t.Helper()
	initCmd := exec.Command(brPath, "init", "--prefix", "sub")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("br init: %v\n%s", err, out)
	}
	// model:<name> label: without it the codex harness refuses to build a
	// launch spec (pre-dispatch), so the run never reaches the --codex-binary
	// exec. With it, dispatch actually execs the generic twin (which does not
	// speak the Codex wire protocol), and the run terminates as run_failed —
	// a genuine dispatch-to-twin terminal outcome, still billing-free.
	createCmd := exec.Command(brPath, "create",
		"subprocess boot smoke bead", "--status", "open",
		"--labels", "model:o4-mini", "--silent")
	createCmd.Dir = projectDir
	out, err := createCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("br create: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		t.Fatal("br create returned empty ID")
	}
	return id
}

// subprocessSmokeWaitForSocket polls for the daemon socket at sockPath, matching
// the scenario harness idiom: a socket file with mode&os.ModeSocket set and
// 0600 perms (test/scenario harness socket poll).
func subprocessSmokeWaitForSocket(t *testing.T, sockPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(sockPath); err == nil {
			if info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600 {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// subprocessSmokeWaitTerminal polls events.jsonl for a terminal run event
// (mirrors queueSubmitDispatchWaitRunTerminal, scenario...:251), extended to
// accept bead_closed as an equally-terminal outcome.
func subprocessSmokeWaitTerminal(t *testing.T, jsonlPath string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: jsonlPath is under a t-owned /tmp dir; not user input.
		data, err := os.ReadFile(jsonlPath)
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.Contains(line, "run_completed") ||
					strings.Contains(line, "run_failed") ||
					strings.Contains(line, "bead_closed") {
					t.Logf("terminal event observed: %s", strings.TrimSpace(line))
					return true
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
