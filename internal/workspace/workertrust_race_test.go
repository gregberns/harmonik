package workspace

// workertrust_race_test.go — regression coverage for the concurrent-slot
// lost-update race in workerTrustUpsertProgram (the remote ~/.claude.json trust
// writer). Under max_slots>1 the daemon spawns several remote runs at once, each
// running this python program against the SAME worker ~/.claude.json. The naive
// unlocked read-modify-write loses updates: concurrent writers each read the
// config before either writes, add only their own worktree key, and the last
// os.replace clobbers the others. The clobbered run's worktree is then untrusted
// → Claude Code's folder-trust dialog → the launch hangs → agent_ready never
// fires. The fix adds an fcntl.flock(LOCK_EX) on a ~/.claude.json.lock sidecar
// held across the whole read-modify-write.
//
// TestWorkerTrustUpsert_UnlockedLosesUpdatesUnderBarrier reproduces the race
// against an embedded copy of the OLD unlocked program (RED) and
// TestWorkerTrustUpsert_ConcurrentAllKeysSurvive proves the shipped program keeps
// every concurrent writer's key (GREEN).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// unlockedTrustUpsertProgramWithBarrier is the PRE-FIX unlocked upsert plus a
// deterministic read-then-write barrier: it reads the config, waits HK_TEST_SLEEP
// seconds, then writes. Launched concurrently, every copy reads the initial
// (empty) config before any writes, so each writes back a config carrying ONLY
// its own key — the last os.replace wins and all but one key are lost. This makes
// the lost-update race deterministic so the RED assertion never flakes. It is the
// bug the shipped program's LOCK_EX closes.
const unlockedTrustUpsertProgramWithBarrier = `
import json, os, sys, tempfile, time
wt = os.path.realpath(sys.argv[1])
cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
cfg = {}
try:
    with open(cfg_path) as f:
        cfg = json.load(f)
except FileNotFoundError:
    cfg = {}
if not isinstance(cfg, dict):
    cfg = {}
projects = cfg.get("projects")
if not isinstance(projects, dict):
    projects = {}
    cfg["projects"] = projects
time.sleep(float(os.environ.get("HK_TEST_SLEEP", "0")))
entry = projects.get(wt)
if not isinstance(entry, dict):
    entry = {}
    projects[wt] = entry
if entry.get("hasTrustDialogAccepted") is True:
    sys.exit(0)
entry["hasTrustDialogAccepted"] = True
d = os.path.dirname(cfg_path) or "."
fd, tmp = tempfile.mkstemp(dir=d, prefix=".claude.json.tmp-")
try:
    with os.fdopen(fd, "w") as f:
        json.dump(cfg, f, indent=2)
        f.write("\n")
    os.replace(tmp, cfg_path)
except BaseException:
    try:
        os.unlink(tmp)
    except OSError:
        pass
    raise
`

// runTrustUpsert runs a trust-upsert python program against a private HOME with
// worktreePath as argv[1]. env holds extra environment entries (e.g. HK_TEST_SLEEP).
func runTrustUpsert(t *testing.T, home, program, worktreePath string, env ...string) error {
	t.Helper()
	cmd := exec.Command("python3", "-", worktreePath)
	cmd.Stdin = bytes.NewReader([]byte(program))
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run trust upsert: %w\noutput: %s", err, out)
	}
	return nil
}

// countTrustedWorktrees returns how many of worktreePaths are recorded as trusted
// (projects[realpath].hasTrustDialogAccepted == true) in home/.claude.json.
func countTrustedWorktrees(t *testing.T, home string, worktreePaths []string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal .claude.json: %v\nraw: %s", err, data)
	}
	n := 0
	for _, wp := range worktreePaths {
		rp, err := filepath.EvalSymlinks(wp)
		if err != nil {
			rp = wp
		}
		if e, ok := cfg.Projects[rp]; ok && e.HasTrustDialogAccepted {
			n++
		}
	}
	return n
}

// makeWorktreePaths creates n distinct existing worktree directories under home
// (they must exist so os.path.realpath resolves them consistently across procs).
func makeWorktreePaths(t *testing.T, home string, n int) []string {
	t.Helper()
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(home, "worktrees", fmt.Sprintf("run-%03d", i))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir worktree %d: %v", i, err)
		}
		paths[i] = p
	}
	return paths
}

// TestWorkerTrustUpsert_UnlockedLosesUpdatesUnderBarrier is the RED proof: the
// pre-fix unlocked program, run concurrently with a read-then-write barrier,
// loses updates — far fewer than N worktrees survive trusted. This is exactly the
// concurrent-slot race the shipped LOCK_EX fix closes.
func TestWorkerTrustUpsert_UnlockedLosesUpdatesUnderBarrier(t *testing.T) {
	t.Parallel()
	const n = 8
	home := t.TempDir()
	paths := makeWorktreePaths(t, home, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			// 0.5s barrier guarantees all copies read the empty config before any write.
			_ = runTrustUpsert(t, home, unlockedTrustUpsertProgramWithBarrier, p, "HK_TEST_SLEEP=0.5")
		}(paths[i])
	}
	wg.Wait()

	survived := countTrustedWorktrees(t, home, paths)
	if survived >= n {
		t.Fatalf("expected the unlocked program to LOSE updates under the barrier "+
			"(survived %d/%d), but all keys survived — the race did not reproduce", survived, n)
	}
	t.Logf("unlocked program under barrier: only %d/%d worktrees survived trusted (lost-update race reproduced)", survived, n)
}

// TestWorkerTrustUpsert_ConcurrentAllKeysSurvive is the GREEN guard: the shipped
// workerTrustUpsertProgram, run concurrently against the same HOME, keeps EVERY
// writer's worktree key. The LOCK_EX sidecar serializes the read-modify-write so
// no update is lost. Correctness holds regardless of scheduling, so this never
// flakes.
func TestWorkerTrustUpsert_ConcurrentAllKeysSurvive(t *testing.T) {
	t.Parallel()
	const n = 12
	home := t.TempDir()
	paths := makeWorktreePaths(t, home, n)

	// Pre-seed a non-trivial config so the read-modify-write has real work,
	// widening the window concurrent writers would race in.
	projects := map[string]interface{}{}
	for i := 0; i < 200; i++ {
		projects[fmt.Sprintf("/preexisting/run-%05d", i)] = map[string]interface{}{
			"hasTrustDialogAccepted": true,
		}
	}
	seed := map[string]interface{}{"theme": "dark", "projects": projects}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	// A small barrier still exercises overlap; the lock must serialize it correctly.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int, p string) {
			defer wg.Done()
			errs[idx] = runTrustUpsert(t, home, workerTrustUpsertProgram, p, "HK_TEST_SLEEP=0")
		}(i, paths[i])
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent trust upsert %d errored: %v", i, err)
		}
	}

	if survived := countTrustedWorktrees(t, home, paths); survived != n {
		t.Fatalf("lost-update race: only %d/%d concurrent worktree keys survived trusted; "+
			"the LOCK_EX read-modify-write must preserve every writer's key", survived, n)
	}

	// The pre-existing keys and top-level content must be preserved too.
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal final config: %v", err)
	}
	if got["theme"] != "dark" {
		t.Errorf("top-level key clobbered: theme=%v", got["theme"])
	}
	gotProjects, _ := got["projects"].(map[string]interface{})
	if _, ok := gotProjects["/preexisting/run-00000"]; !ok {
		t.Errorf("pre-existing project key was clobbered by the concurrent writers")
	}
}
