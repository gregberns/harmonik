//go:build subprocess

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSubprocessDaemonBoot_SubmitReachesAgentThenFailsStructurally proves that
// the real binary boots, accepts a command-line submit, and dispatches the
// implement node. It proves nothing about an agent doing work: the generic twin
// cannot speak the Codex app-server protocol that the driver speaks.
func TestSubprocessDaemonBoot_SubmitReachesAgentThenFailsStructurally(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain required: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Fatal("br required for subprocess boot smoke (not on PATH)")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git required for subprocess boot smoke (not on PATH)")
	}

	moduleRoot := subprocessSmokeModuleRoot(t, goTool)

	binDir := t.TempDir()
	harmonikBin := subprocessSmokeBuild(t, goTool, moduleRoot, binDir,
		"harmonik", "github.com/gregberns/harmonik/cmd/harmonik")
	genericTwinBin := subprocessSmokeBuild(t, goTool, moduleRoot, binDir,
		"generic-twin", "github.com/gregberns/harmonik/cmd/harmonik-twin-generic")

	projectDir := subprocessSmokeProjectDir(t)
	subprocessSmokeGitRepo(t, projectDir)
	beadID := subprocessSmokeInitBr(t, brPath, projectDir)

	jsonlPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")

	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	//nolint:gosec // G204: harmonikBin is a test-built binary; args are literals.
	daemonCmd := exec.CommandContext(daemonCtx, harmonikBin, "start", "daemon", "--project", projectDir)
	daemonCmd.Dir = projectDir
	daemonCmd.Env = append(os.Environ(),
		"HARMONIK_SUBSTRATE=codexdriver",
	)
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
	t.Cleanup(func() {
		cancelDaemon()
		_ = daemonCmd.Wait()
		if t.Failed() {
			t.Logf("daemon subprocess output:\n%s", daemonOut.String())
		}
	})

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

	deadline := time.Now().Add(90 * time.Second)
	var run subprocessSmokeRun
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: jsonlPath is under a t-owned /tmp dir; not user input.
		data, readErr := os.ReadFile(jsonlPath)
		if readErr == nil {
			run, err = scanSubprocessSmokeEvents(data, beadID)
			if err != nil {
				t.Fatalf("scan subprocess events: %v", err)
			}
			if run.Terminal != "" {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !run.Nodes["implement"] {
		t.Fatalf("submitted run %q did not dispatch the implement node", run.RunID)
	}
	if run.Terminal == "" {
		t.Fatalf("no terminal event for submitted run %q within 90s\njsonl=%s\ndaemon output:\n%s",
			run.RunID, jsonlPath, daemonOut.String())
	}
	if run.Terminal == "run_completed" {
		t.Fatalf("terminal outcome = %q; expected outcome is now wrong and must be re-decided, not widened", run.Terminal)
	}
	if run.Terminal != "run_failed" || run.Success || !strings.Contains(run.Summary, "class=structural") {
		t.Fatalf("terminal outcome = %q success=%t summary=%q; want run_failed, false, and class=structural",
			run.Terminal, run.Success, run.Summary)
	}
}

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

func subprocessSmokeInitBr(t *testing.T, brPath, projectDir string) string {
	t.Helper()
	initCmd := exec.Command(brPath, "init", "--prefix", "sub")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("br init: %v\n%s", err, out)
	}
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
