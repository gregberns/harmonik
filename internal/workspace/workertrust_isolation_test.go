package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

type homeRedirectRunner struct{ home string }

func (r homeRedirectRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "HOME="+r.home)
	return cmd
}

type workerEnvRunner struct {
	home       string
	configHome string
}

func (r workerEnvRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	env := []string{"HOME=" + r.home}
	if r.configHome != "" {
		env = append(env, "CLAUDE_CONFIG_HOME="+r.configHome)
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") ||
			strings.HasPrefix(kv, "CLAUDE_CONFIG_HOME=") ||
			strings.HasPrefix(kv, "HARMONIK_CLAUDE_CONFIG_PATH=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	return cmd
}

type fixedResultRunner struct{ script string }

func (r fixedResultRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	//nolint:gosec // G204: script is a literal written by this test to drive one exit status.
	return exec.CommandContext(ctx, "sh", "-c", r.script)
}

func isolationFixture(t *testing.T) (worktree, home, cfgPath string) {
	t.Helper()
	worktree = t.TempDir()
	home = t.TempDir()
	cfgPath = filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath)
	return worktree, home, cfgPath
}

func homeConfigUntouched(t *testing.T, home string) {
	t.Helper()
	strayCfg := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(strayCfg); err == nil {
		//nolint:gosec // G304: strayCfg is inside this test's t.TempDir fixture.
		body, readErr := os.ReadFile(strayCfg)
		if readErr != nil {
			body = []byte("unreadable: " + readErr.Error())
		}
		t.Fatalf("the worker program wrote %s, so it expanded ~ instead of using the configured "+
			"HARMONIK_CLAUDE_CONFIG_PATH. On a real machine that file is the operator's own "+
			"~/.claude.json, and the program locks it too.\nwrote: %s", strayCfg, body)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", strayCfg, err)
	}
	strayLock := filepath.Join(home, ".claude.json.lock")
	if _, err := os.Stat(strayLock); err == nil {
		t.Fatalf("the worker program created %s, so it locked the home directory's config "+
			"rather than the configured one", strayLock)
	}
}

// TestEnsureWorktreeTrustVia_RemoteWritesTheConfiguredPath is claim 1 for the
// trust upsert: the remote leg records the trust entry in the configured config
// file, and creates nothing in the home directory.
func TestEnsureWorktreeTrustVia_RemoteWritesTheConfiguredPath(t *testing.T) {
	worktree, home, cfgPath := isolationFixture(t)

	if err := EnsureWorktreeTrustVia(t.Context(), homeRedirectRunner{home: home}, worktree); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia: %v", err)
	}

	homeConfigUntouched(t, home)

	key := worktree
	if resolved, err := filepath.EvalSymlinks(worktree); err == nil {
		key = resolved
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("the configured config %s was not written: %v", cfgPath, err)
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v\nraw: %s", cfgPath, err, data)
	}
	if entry, ok := cfg.Projects[key]; !ok || !entry.HasTrustDialogAccepted {
		t.Errorf("projects[%q].hasTrustDialogAccepted is not true in the configured config %s\nraw: %s",
			key, cfgPath, data)
	}

	homeConfigUntouched(t, home)
}

// TestWorkerTrustUpsert_BoundedWaitFailsWhenTheLockIsHeld is claim 2. The test
// process holds the sidecar lock, so the program can never get it. It must give
// up on its own budget and name the file it was waiting on.
//
// The context deadline is the safety net, not the assertion: it is far longer
// than the budget, so a program that went back to waiting forever is killed at
// the deadline and fails here with "signal: killed" instead of hanging the
// package. That is an *exec.ExitError with exit code -1, so it fails the
// exit-status check below rather than the nil check.
func TestWorkerTrustUpsert_BoundedWaitFailsWhenTheLockIsHeld(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	worktree := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	lockPath := cfgPath + ".lock"

	//nolint:gosec // G304: lockPath is inside this test's t.TempDir fixture.
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open lockfile %s: %v", lockPath, err)
	}
	defer func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Errorf("close lockfile: %v", closeErr)
		}
	}()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("hold the lock: %v", err)
	}

	const budget = time.Second
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	//nolint:gosec // G204: worktree is a path this test made under t.TempDir.
	cmd := exec.CommandContext(ctx, "python3", "-", worktree)
	cmd.Stdin = strings.NewReader(workerTrustUpsertProgram(cfgPath, budget))
	cmd.Env = append(os.Environ(), "HOME="+home)

	start := time.Now()
	out, runErr := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if runErr == nil {
		t.Fatalf("the program exited 0 although the lock was held for its whole run; it must fail rather than "+
			"pretend it wrote.\noutput: %s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != workerConfigLockTimeoutExit {
		t.Fatalf("the program failed with %v after %s, not with the lock-timeout status %d. "+
			"A program that waits forever is killed at the context deadline and reports "+
			"\"signal: killed\" here instead.\noutput: %s",
			runErr, elapsed.Round(time.Millisecond), workerConfigLockTimeoutExit, out)
	}
	if elapsed < budget {
		t.Errorf("the program gave up after %s, sooner than its %s budget — it is not really waiting for the lock",
			elapsed.Round(time.Millisecond), budget)
	}
	if !strings.Contains(string(out), lockPath) {
		t.Errorf("the failure message does not name the lock file %s, so nobody can find the holder.\noutput: %s",
			lockPath, out)
	}
}

// TestEnsureWorktreeTrustVia_LockTimeoutIsStructural covers the Go half of claim
// 2: a lock timeout on the worker reaches the dispatch path as the SAME
// structural error the in-process writer returns, so the bead reopens instead of
// failing with an opaque exit status.
func TestEnsureWorktreeTrustVia_LockTimeoutIsStructural(t *testing.T) {
	t.Parallel()

	runner := fixedResultRunner{script: "echo 'workspace: write-lock acquire timed out' >&2; exit 75"}
	err := EnsureWorktreeTrustVia(t.Context(), runner, t.TempDir())
	if !errors.Is(err, ErrTrustLockTimeout) {
		t.Errorf("err = %v, want ErrTrustLockTimeout", err)
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("err = %v, want it to classify as structural so the launch fails fast and the bead reopens", err)
	}
}

// TestWorkerConfigPrograms_RemoteNeverBakesInBoxAsConfigHome is claim 3, and it
// is the one that keeps a real remote run working.
//
// CLAUDE_CONFIG_HOME is Claude Code's own variable and it describes the box it is
// set on. docs/live-twin-testing.md REQUIRES exporting it to the daemon process,
// so a daemon that has it set is normal. If box A's value were baked into the
// worker program, a directory that exists only on box A would reach the worker
// and the program would die on the missing lock file — a fatal launch failure
// where the old program correctly wrote the worker's own ~/.claude.json.
//
// The test asserts both halves of the split: box A's CLAUDE_CONFIG_HOME still
// steers the LOCAL writer (which is what the live-twin runbook needs), and it
// never leaves box A on the remote path.
func TestWorkerConfigPrograms_RemoteNeverBakesInBoxAsConfigHome(t *testing.T) {
	worktree := t.TempDir()
	workerHome := t.TempDir()

	boxAConfigHome := filepath.Join(t.TempDir(), "box-a-only")
	t.Setenv("CLAUDE_CONFIG_HOME", boxAConfigHome)
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", "")

	if got, want := claudeGlobalConfigPath(), filepath.Join(boxAConfigHome, ".claude.json"); got != want {
		t.Fatalf("the LOCAL config path is %q, want %q — CLAUDE_CONFIG_HOME must keep steering box A's own writer, "+
			"which is what docs/live-twin-testing.md relies on", got, want)
	}

	if got := claudeConfigPathForWorker(); got != "" {
		t.Errorf("claudeConfigPathForWorker() = %q, want \"\" — only HARMONIK_CLAUDE_CONFIG_PATH may cross the wire, "+
			"and CLAUDE_CONFIG_HOME describes the box it is set on", got)
	}
	for name, prog := range map[string]string{
		"trust": workerTrustUpsertProgram(claudeConfigPathForWorker(), defaultTrustLockTimeout),
	} {
		if strings.Contains(prog, boxAConfigHome) {
			t.Errorf("the %s program sent to the worker contains box A's directory %s. On a worker that path need not "+
				"exist, and the program dies on the missing lock file.", name, boxAConfigHome)
		}
	}

	ctx := t.Context()
	runner := workerEnvRunner{home: workerHome}
	if err := EnsureWorktreeTrustVia(ctx, runner, worktree); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia against a worker that has no CLAUDE_CONFIG_HOME: %v\n"+
			"a box-A path baked into the program fails here with FileNotFoundError on the lock file", err)
	}
	if _, err := os.Stat(boxAConfigHome); err == nil {
		t.Errorf("the worker programs created box A's directory %s, so they used box A's config path", boxAConfigHome)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", boxAConfigHome, err)
	}

	workerCfg := filepath.Join(workerHome, ".claude.json")
	//nolint:gosec // G304: workerCfg is inside this test's t.TempDir fixture.
	data, err := os.ReadFile(workerCfg)
	if err != nil {
		t.Fatalf("the worker's own config %s was not written: %v", workerCfg, err)
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v\nraw: %s", workerCfg, err, data)
	}
	key := worktree
	if resolved, err := filepath.EvalSymlinks(worktree); err == nil {
		key = resolved
	}
	if entry, ok := cfg.Projects[key]; !ok || !entry.HasTrustDialogAccepted {
		t.Errorf("projects[%q].hasTrustDialogAccepted is not true in the worker's own config %s\nraw: %s",
			key, workerCfg, data)
	}
}

// TestWorkerConfigPrograms_WorkerHonoursItsOwnConfigHome is the other half of the
// same rule. Box A's CLAUDE_CONFIG_HOME must not cross the wire, and the WORKER's
// own must still be obeyed — that is what keeps the precedence list one list
// rather than two, and it is what a worker with its own scratch config needs.
func TestWorkerConfigPrograms_WorkerHonoursItsOwnConfigHome(t *testing.T) {
	worktree := t.TempDir()
	workerHome := t.TempDir()
	workerConfigHome := t.TempDir()
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", "")
	t.Setenv("CLAUDE_CONFIG_HOME", "")

	runner := workerEnvRunner{home: workerHome, configHome: workerConfigHome}
	if err := EnsureWorktreeTrustVia(t.Context(), runner, worktree); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia: %v", err)
	}

	homeConfigUntouched(t, workerHome)

	workerCfg := filepath.Join(workerConfigHome, ".claude.json")
	//nolint:gosec // G304: workerCfg is inside this test's t.TempDir fixture.
	data, err := os.ReadFile(workerCfg)
	if err != nil {
		t.Fatalf("the worker's own CLAUDE_CONFIG_HOME config %s was not written: %v\n"+
			"the program must apply step 2 of the precedence list in the WORKER's environment", workerCfg, err)
	}
	if !strings.Contains(string(data), "hasTrustDialogAccepted") {
		t.Errorf("%s has no trust entry\nraw: %s", workerCfg, data)
	}
}
