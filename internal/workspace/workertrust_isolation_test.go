package workspace

// workertrust_isolation_test.go — the worker-side config writers must obey the
// configured config path, and must never wait on the lock forever (hk-g8d5x).
//
// WHAT WENT WRONG. internal/testhelpers/hermetic points
// HARMONIK_CLAUDE_CONFIG_PATH at a temp file so no test touches the operator's
// real ~/.claude.json. The Go writer honoured that variable. The python program
// the REMOTE writer feeds to python3 did not: it expanded ~ itself, and it took
// a plain blocking fcntl.flock(LOCK_EX) on the real ~/.claude.json.lock. Three
// internal/daemon dot-node tests are the only ones that pass a non-nil Runner,
// so they were the only ones that reached this program — and each of them
// blocked about 50 seconds on the operator's home directory while an orphaned
// test binary from another lane held that lock. The failure named the dispatch
// path, which was innocent.
//
// The three claims these tests hold down:
//
//  1. with HARMONIK_CLAUDE_CONFIG_PATH set, the program writes THAT file and
//     leaves the home directory alone;
//  2. when the lock is held, the program gives up after its budget and says which
//     file it was waiting on, instead of waiting forever; and
//  3. box A's CLAUDE_CONFIG_HOME never crosses the wire — it names a directory on
//     box A, and the file the worker writes must be the one the WORKER's claude
//     reads. Fixing claim 1 by sending every configured path across would have
//     broken every remote run on a daemon that follows docs/live-twin-testing.md.

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

// homeRedirectRunner runs each command locally with HOME pointed at a directory
// this test owns.
//
// It stands in for the remote runner: production hands EnsureWorktreeTrustVia an
// SSHRunner and the program runs in the WORKER's home, and the local passthrough
// runner the daemon's dot-node fixtures use runs it in whatever home the test
// binary inherited. Redirecting HOME lets the assertion below say "the home
// directory was not touched" without touching the operator's real one.
type homeRedirectRunner struct{ home string }

func (r homeRedirectRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "HOME="+r.home)
	return cmd
}

// workerEnvRunner runs each command locally but with an environment shaped like
// the FAR side of an ssh hop: home is the WORKER's home, configHome is the
// WORKER's own CLAUDE_CONFIG_HOME (empty for the usual case of none), and every
// config-path variable this process may hold is removed first.
//
// That scrubbing is the whole point. ssh forwards no environment by default, so
// a worker program sees the worker's variables and never box A's. A runner that
// inherited box A's variables would hide the exact regression these tests exist
// to catch.
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

// fixedResultRunner ignores the requested command and runs script through sh, so
// a test can drive the Go caller's handling of a specific exit status.
type fixedResultRunner struct{ script string }

func (r fixedResultRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	//nolint:gosec // G204: script is a literal written by this test to drive one exit status.
	return exec.CommandContext(ctx, "sh", "-c", r.script)
}

// isolationFixture makes a worktree directory, a redirected home, and a config
// path under a third directory, and points HARMONIK_CLAUDE_CONFIG_PATH at the
// config path.
func isolationFixture(t *testing.T) (worktree, home, cfgPath string) {
	t.Helper()
	worktree = t.TempDir()
	home = t.TempDir()
	cfgPath = filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", cfgPath)
	return worktree, home, cfgPath
}

// homeConfigUntouched fails when the redirected home has a .claude.json, which
// is what the program writes when it expands ~ instead of using the configured
// path.
func homeConfigUntouched(t *testing.T, home string) {
	t.Helper()
	strayCfg := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(strayCfg); err == nil {
		//nolint:gosec // G304: strayCfg is inside this test's t.TempDir fixture.
		body, readErr := os.ReadFile(strayCfg)
		if readErr != nil {
			// The file exists, so the test has already failed. Report why the
			// contents are missing rather than dropping the diagnostic.
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
	// Not parallel: t.Setenv.
	worktree, home, cfgPath := isolationFixture(t)

	if err := EnsureWorktreeTrustVia(t.Context(), homeRedirectRunner{home: home}, worktree); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia: %v", err)
	}

	// Checked first: "it left the home directory alone" is the claim, and it gives
	// the sharper message when the program goes back to expanding ~.
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

// TestEnsureClaudeThemeVia_RemoteWritesTheConfiguredPath is claim 1 for the
// theme upsert. It runs immediately after the trust upsert in the launch-spec
// build and locks the same file, so isolating only the trust program would have
// moved the block one step down the path rather than removing it.
func TestEnsureClaudeThemeVia_RemoteWritesTheConfiguredPath(t *testing.T) {
	// Not parallel: t.Setenv.
	_, home, cfgPath := isolationFixture(t)

	if err := EnsureClaudeThemeVia(t.Context(), homeRedirectRunner{home: home}); err != nil {
		t.Fatalf("EnsureClaudeThemeVia: %v", err)
	}

	homeConfigUntouched(t, home)

	//nolint:gosec // G304: cfgPath is inside this test's t.TempDir fixture.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("the configured config %s was not written: %v", cfgPath, err)
	}
	var cfg struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal %s: %v\nraw: %s", cfgPath, err, data)
	}
	if cfg.Theme != claudeDefaultTheme {
		t.Errorf("theme = %q in the configured config %s, want %q\nraw: %s",
			cfg.Theme, cfgPath, claudeDefaultTheme, data)
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

// TestEnsureClaudeThemeVia_LockTimeoutIsStructural is the same claim for the
// theme leg. It is not redundant with the trust leg: the two calls have separate
// error paths, and deleting the theme leg's mapping left the whole package green
// until this test existed. The two programs take the SAME lock on the SAME file
// one after the other, so whatever starves one starves the other.
func TestEnsureClaudeThemeVia_LockTimeoutIsStructural(t *testing.T) {
	t.Parallel()

	runner := fixedResultRunner{script: "echo 'workspace: write-lock acquire timed out' >&2; exit 75"}
	err := EnsureClaudeThemeVia(t.Context(), runner)
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
	// Not parallel: t.Setenv.
	worktree := t.TempDir()
	workerHome := t.TempDir()

	// A directory that exists on box A and nowhere else. It is never created, so
	// any writer that resolves through it fails loudly.
	boxAConfigHome := filepath.Join(t.TempDir(), "box-a-only")
	t.Setenv("CLAUDE_CONFIG_HOME", boxAConfigHome)
	// hermetic.Main sets this for the whole package. Clear it so CLAUDE_CONFIG_HOME
	// is the only override in force, which is the live-twin daemon's shape.
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", "")

	// Half one: box A's own writer still honours CLAUDE_CONFIG_HOME.
	if got, want := claudeGlobalConfigPath(), filepath.Join(boxAConfigHome, ".claude.json"); got != want {
		t.Fatalf("the LOCAL config path is %q, want %q — CLAUDE_CONFIG_HOME must keep steering box A's own writer, "+
			"which is what docs/live-twin-testing.md relies on", got, want)
	}

	// Half two: nothing box-A-only crosses the wire.
	if got := claudeConfigPathForWorker(); got != "" {
		t.Errorf("claudeConfigPathForWorker() = %q, want \"\" — only HARMONIK_CLAUDE_CONFIG_PATH may cross the wire, "+
			"and CLAUDE_CONFIG_HOME describes the box it is set on", got)
	}
	for name, prog := range map[string]string{
		"trust": workerTrustUpsertProgram(claudeConfigPathForWorker(), defaultTrustLockTimeout),
		"theme": workerThemeUpsertProgram(claudeConfigPathForWorker(), defaultTrustLockTimeout),
	} {
		if strings.Contains(prog, boxAConfigHome) {
			t.Errorf("the %s program sent to the worker contains box A's directory %s. On a worker that path need not "+
				"exist, and the program dies on the missing lock file.", name, boxAConfigHome)
		}
	}

	// And the behaviour those assertions stand for: run both programs with a
	// worker-shaped environment and see them write the WORKER's own config.
	ctx := t.Context()
	runner := workerEnvRunner{home: workerHome}
	if err := EnsureWorktreeTrustVia(ctx, runner, worktree); err != nil {
		t.Fatalf("EnsureWorktreeTrustVia against a worker that has no CLAUDE_CONFIG_HOME: %v\n"+
			"a box-A path baked into the program fails here with FileNotFoundError on the lock file", err)
	}
	if err := EnsureClaudeThemeVia(ctx, runner); err != nil {
		t.Fatalf("EnsureClaudeThemeVia against a worker that has no CLAUDE_CONFIG_HOME: %v", err)
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
		Theme    string `json:"theme"`
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
	if cfg.Theme != claudeDefaultTheme {
		t.Errorf("theme = %q in the worker's own config %s, want %q\nraw: %s",
			cfg.Theme, workerCfg, claudeDefaultTheme, data)
	}
}

// TestWorkerConfigPrograms_WorkerHonoursItsOwnConfigHome is the other half of the
// same rule. Box A's CLAUDE_CONFIG_HOME must not cross the wire, and the WORKER's
// own must still be obeyed — that is what keeps the precedence list one list
// rather than two, and it is what a worker with its own scratch config needs.
func TestWorkerConfigPrograms_WorkerHonoursItsOwnConfigHome(t *testing.T) {
	// Not parallel: t.Setenv.
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
