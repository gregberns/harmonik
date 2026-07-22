package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hk-qx065 — at max-concurrent 3, two of three claude:local workers parked on
// Claude Code's folder-trust modal. That modal renders BEFORE SessionStart, so
// the hook never fires, agent_ready is never synthesized, and the run dies at the
// 150s deadline. On disk the failed worktree's
// projects[<realpath>].hasTrustDialogAccepted was ABSENT from the config even
// though EnsureWorktreeTrust had run and returned success.
//
// The clobberer is Claude Code itself: a live claude process rewrites
// ~/.claude.json wholesale from its own in-memory snapshot and does NOT honor
// harmonik's advisory flock (same observation recorded in a964cbcb). More locking
// on our side cannot exclude a writer that never asks for the lock, so the fix is
// VERIFY-AND-REPAIR: re-read after every write, retry the whole read-modify-write
// on a lost key, and fail structurally if it never sticks.
//
// These tests use the trustPostWriteHook seam to stand in for that external
// writer. They are NOT parallel: the hook is a package-level var (the file's
// existing seam idiom), and Go pauses t.Parallel() tests for the duration of the
// sequential ones, so a serial test owns the hook exclusively.

// withTrustPostWriteHook installs fn as the post-write seam for the duration of
// the test. It takes trustWriteMu for the swap because the write path reads the
// var while holding that mutex — that is what makes the access race-free.
func withTrustPostWriteHook(t *testing.T, fn func(cfgPath string)) {
	t.Helper()
	trustWriteMu.Lock()
	prev := trustPostWriteHook
	trustPostWriteHook = fn
	trustWriteMu.Unlock()
	t.Cleanup(func() {
		trustWriteMu.Lock()
		trustPostWriteHook = prev
		trustWriteMu.Unlock()
	})
}

// hkqx065ClobberAddedProject is a project key the clobbering writer INVENTS. It
// is deliberately something no snapshot the trust writer read before its own
// write can contain.
const hkqx065ClobberAddedProject = "/added/by/the/clobberer"

// hkqx065ClobberGenerationKey is a top-level key the clobbering writer bumps on
// every clobber, for the same reason.
const hkqx065ClobberGenerationKey = "clobbererGeneration"

// hkqx065Clobber rewrites cfgPath the way a live claude process does: it drops
// worktreePath's trust entry (the key it never knew about) and, critically, ALSO
// COMMITS CONTENT OF ITS OWN — a new project entry plus a bumped top-level
// generation counter.
//
// The added content is what makes this a real test rather than a tautology. If
// the trust writer retried from a snapshot it took before its first write instead
// of re-reading disk, a delete-only clobber would be undetectable: writing back
// the stale snapshot is byte-indistinguishable from re-applying onto a fresh
// read, because the snapshot already contains everything except our own key.
// Adding content breaks that symmetry — a stale-snapshot retry silently erases
// generation `n`, which is precisely the lost-update behaviour that would make
// harmonik the clobberer of a sibling worker's trust entry.
func hkqx065Clobber(t *testing.T, cfgPath, worktreePath string, generation int) {
	t.Helper()
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("hk-qx065: clobber read %s: %v", cfgPath, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("hk-qx065: clobber unmarshal %s: %v", cfgPath, err)
	}
	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		projects = map[string]interface{}{}
		cfg["projects"] = projects
	}
	// Lose the key we never knew about...
	delete(projects, worktreePath)
	// ...and commit content of our own that no earlier snapshot has seen.
	projects[hkqx065ClobberAddedProject] = map[string]interface{}{
		"hasTrustDialogAccepted": true,
		"generation":             float64(generation),
	}
	cfg[hkqx065ClobberGenerationKey] = float64(generation)

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("hk-qx065: clobber marshal: %v", err)
	}
	if err := os.WriteFile(cfgPath, append(out, '\n'), 0o600); err != nil {
		t.Fatalf("hk-qx065: clobber write %s: %v", cfgPath, err)
	}
}

// hkqx065AssertClobbererContentSurvived asserts that everything the clobbering
// writer committed at `generation` is still in the config. This is the
// merge-onto-fresh-read property: our repair write must land on top of the other
// writer's file, never revert it.
func hkqx065AssertClobbererContentSurvived(t *testing.T, cfgPath string, generation int) {
	t.Helper()
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("hk-qx065: read %s: %v", cfgPath, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("hk-qx065: unmarshal %s: %v", cfgPath, err)
	}
	if got, want := cfg[hkqx065ClobberGenerationKey], float64(generation); got != want {
		t.Errorf("hk-qx065: %s = %v, want %v — the repair write reverted the other writer's file "+
			"instead of merging onto it (lost update: harmonik became the clobberer)",
			hkqx065ClobberGenerationKey, got, want)
	}
	projects, _ := cfg["projects"].(map[string]interface{})
	entry, ok := projects[hkqx065ClobberAddedProject].(map[string]interface{})
	if !ok {
		t.Fatalf("hk-qx065: the clobberer's own project entry %q was erased by the repair write — "+
			"a sibling worker's trust entry would be lost the same way", hkqx065ClobberAddedProject)
	}
	if got, want := entry["generation"], float64(generation); got != want {
		t.Errorf("hk-qx065: clobberer's project entry generation = %v, want %v (stale snapshot re-applied)", got, want)
	}
}

// hkqx065ClobberHook builds a post-write hook that clobbers cfgPath on its first
// `times` invocations and leaves it alone afterwards. *attempts counts every time
// the write path reached the post-write point, i.e. the number of
// read-modify-write cycles actually performed. Writes to any OTHER config path
// are ignored, so the shared hook cannot disturb an unrelated test.
//
// The returned int pointer *clobbers reports the last generation the clobberer
// committed (0 = it never fired), for the survived-content assertion.
func hkqx065ClobberHook(t *testing.T, cfgPath, worktreePath string, times int, attempts, clobbers *int) func(string) {
	t.Helper()
	return func(gotPath string) {
		if gotPath != cfgPath {
			return
		}
		*attempts++
		if *attempts <= times {
			*clobbers++
			hkqx065Clobber(t, cfgPath, worktreePath, *clobbers)
		}
	}
}

// hkqx065Trusted reports whether cfgPath records worktreePath as trusted.
func hkqx065Trusted(t *testing.T, cfgPath, worktreePath string) bool {
	t.Helper()
	data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("hk-qx065: read %s: %v", cfgPath, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("hk-qx065: unmarshal %s: %v", cfgPath, err)
	}
	projects, _ := cfg["projects"].(map[string]interface{})
	entry, _ := projects[worktreePath].(map[string]interface{})
	trusted, _ := entry["hasTrustDialogAccepted"].(bool)
	return trusted
}

// TestHkqx065_HappyPath_OneAttemptVerified is the baseline: with nobody
// clobbering, the key persists, verification passes on the first read-back, and
// exactly ONE read-modify-write cycle runs. The attempt count is the point — the
// retry loop must not cost extra writes in the normal case.
func TestHkqx065_HappyPath_OneAttemptVerified(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-happy")

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, cfgPath, worktreePath, 0, &attempts, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-qx065: happy path errored: %v", err)
	}
	if attempts != 1 {
		t.Errorf("hk-qx065: happy path performed %d write cycles, want exactly 1", attempts)
	}
	if !hkqx065Trusted(t, cfgPath, worktreePath) {
		t.Error("hk-qx065: happy path reported success but the key is not on disk")
	}
}

// TestHkqx065_TransientClobber_Recovers is the repair case: an external writer
// erases our key once, right after our first write. The verification read must
// catch it and the retry must re-apply the key on top of the clobberer's file —
// so the call succeeds and the key is on disk, after exactly 2 cycles.
func TestHkqx065_TransientClobber_Recovers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-transient")

	// Pre-existing content the clobberer keeps: proves the retry merges onto the
	// other writer's file rather than onto a stale in-memory snapshot of our own.
	seed := map[string]interface{}{
		"theme": "dark",
		"projects": map[string]interface{}{
			"/some/other/project": map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	raw, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("hk-qx065: seed config: %v", err)
	}

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, cfgPath, worktreePath, 1, &attempts, &clobbers))

	start := time.Now()
	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-qx065: single clobber should have been repaired, got error: %v", err)
	}
	elapsed := time.Since(start)

	if attempts != 2 {
		t.Errorf("hk-qx065: performed %d write cycles after one clobber, want 2 (initial + repair)", attempts)
	}
	if !hkqx065Trusted(t, cfgPath, worktreePath) {
		t.Error("hk-qx065: reported success but the key is not on disk after the repair")
	}
	if elapsed < trustWriteRetryBackoff {
		t.Errorf("hk-qx065: repaired in %v, less than one backoff (%v) — the retry did not back off", elapsed, trustWriteRetryBackoff)
	}

	// The pre-existing content must survive.
	data, _ := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("hk-qx065: unmarshal after repair: %v", err)
	}
	if got["theme"] != "dark" {
		t.Errorf("hk-qx065: repair clobbered a top-level key; theme=%v", got["theme"])
	}
	projects, _ := got["projects"].(map[string]interface{})
	if _, ok := projects["/some/other/project"]; !ok {
		t.Error("hk-qx065: repair dropped an unrelated project entry")
	}

	// THE DISCRIMINATING ASSERTION: content the clobberer committed AFTER our first
	// read must survive too. Only a retry that re-reads disk can preserve it — a
	// retry that re-applies the snapshot taken before attempt 1 would silently
	// revert it, and the two are indistinguishable without this check.
	if clobbers != 1 {
		t.Fatalf("hk-qx065: clobberer fired %d times, want 1 (test fixture wrong)", clobbers)
	}
	hkqx065AssertClobbererContentSurvived(t, cfgPath, clobbers)
}

// TestHkqx065_PersistentClobber_FailsStructurally is the fatal case: the external
// writer erases our key after EVERY write. We must exhaust the bounded attempts
// and then return a structural error — never report success. Reporting success
// here is precisely the observed defect: the launch proceeds, claude parks on the
// trust modal, and the run dies at the 150s agent_ready deadline with nothing
// explaining why.
func TestHkqx065_PersistentClobber_FailsStructurally(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-persistent")

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, cfgPath, worktreePath, 1<<30, &attempts, &clobbers))

	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	if err == nil {
		t.Fatal("hk-qx065: a permanently-clobbered write reported success; it must fail instead")
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("hk-qx065: error is not structural (callers must not exec claude): %v", err)
	}
	if attempts != trustWriteMaxAttempts {
		t.Errorf("hk-qx065: performed %d write cycles, want trustWriteMaxAttempts=%d", attempts, trustWriteMaxAttempts)
	}
	// The message is the one artifact whoever hits this failure will read; it must
	// name what did not persist and why.
	if !strings.Contains(err.Error(), "did not persist") || !strings.Contains(err.Error(), "concurrent writer") {
		t.Errorf("hk-qx065: error message does not explain the failure: %v", err)
	}
	// And the disk really is untrusted — we are not failing on a false negative.
	if hkqx065Trusted(t, cfgPath, worktreePath) {
		t.Error("hk-qx065: returned an error although the key IS on disk")
	}
}

// TestHkqx065_FastPath_NoWriteWhenAlreadyTrusted guards hk-bfvby against this
// change: an already-trusted path must still short-circuit on the lock-free probe
// and never enter the read-modify-write. The hook firing at all would mean a
// write happened.
func TestHkqx065_FastPath_NoWriteWhenAlreadyTrusted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-fastpath")

	hkbfvbyWriteLargeConfig(t, cfgPath, worktreePath, 200)
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(cfgPath, old, old); err != nil {
		t.Fatalf("hk-qx065: chtimes: %v", err)
	}
	before := hkbfvbyMtime(t, cfgPath)

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, cfgPath, worktreePath, 0, &attempts, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-qx065: already-trusted call errored: %v", err)
	}
	if attempts != 0 {
		t.Errorf("hk-qx065: already-trusted call performed %d write cycles, want 0 (fast path must not write)", attempts)
	}
	if after := hkbfvbyMtime(t, cfgPath); !after.Equal(before) {
		t.Errorf("hk-qx065: already-trusted call rewrote the config (mtime %v -> %v)", before, after)
	}
}

// TestHkqx065_IsolatedConfigCaller_StillVerifies covers the other caller:
// PrepareIsolatedClaudeConfigDir drives the same writer against an EXPLICIT
// per-launch config path (hk-8juwz / hk-qxvc2). It must keep working, and it must
// get the same repair behaviour — a clobber of the isolated config is repaired,
// not silently accepted.
func TestHkqx065_IsolatedConfigCaller_StillVerifies(t *testing.T) {
	wt := t.TempDir()
	srcCfg := filepath.Join(t.TempDir(), ".claude.json")
	writeClaudeCfg(t, srcCfg, map[string]interface{}{"firstStartTime": "2026-01-01T00:00:00.000Z"})
	withIsolatedConfigSource(t, srcCfg)

	// The path PrepareIsolatedClaudeConfigDir will write to.
	isoCfg := filepath.Join(wt, ".harmonik", "claude-config", ".claude.json")
	trustKey := wt
	if resolved, err := filepath.EvalSymlinks(wt); err == nil {
		trustKey = resolved
	}

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, isoCfg, trustKey, 1, &attempts, &clobbers))

	dir, err := PrepareIsolatedClaudeConfigDir(wt)
	if err != nil {
		t.Fatalf("hk-qx065: PrepareIsolatedClaudeConfigDir under one clobber: %v", err)
	}
	if attempts != 2 {
		t.Errorf("hk-qx065: isolated-config caller performed %d write cycles, want 2 (initial + repair)", attempts)
	}
	if !hkqx065Trusted(t, filepath.Join(dir, ".claude.json"), trustKey) {
		t.Error("hk-qx065: isolated config is not trusted after the repair")
	}
	hkqx065AssertClobbererContentSurvived(t, isoCfg, clobbers)
}

// TestHkqx065_MultiRoundClobber_MergesEachTime extends the repair case across
// SEVERAL rounds: the writer clobbers after attempts 1 and 2, committing new
// content each time, and only attempt 3 sticks. Every round's re-read must be
// fresh, so the content committed by the LAST clobber (generation 2) is what
// survives. A retry that re-applied any earlier snapshot would leave an older
// generation behind.
func TestHkqx065_MultiRoundClobber_MergesEachTime(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-multi")

	attempts, clobbers := 0, 0
	withTrustPostWriteHook(t, hkqx065ClobberHook(t, cfgPath, worktreePath, 2, &attempts, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-qx065: two clobbers should still be repaired, got error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("hk-qx065: performed %d write cycles after two clobbers, want 3", attempts)
	}
	if clobbers != 2 {
		t.Fatalf("hk-qx065: clobberer fired %d times, want 2 (test fixture wrong)", clobbers)
	}
	if !hkqx065Trusted(t, cfgPath, worktreePath) {
		t.Error("hk-qx065: key not on disk after the second repair")
	}
	hkqx065AssertClobbererContentSurvived(t, cfgPath, clobbers)
}

// TestHkqx065_TornRead_Retried covers the decode-error path. A foreign writer
// that does not commit via rename can be caught mid-write, and the resulting read
// is not valid JSON. A single such observation must NOT hard-fail the launch: it
// is retried within the same budget, and the next read sees the settled file.
func TestHkqx065_TornRead_Retried(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-torn")

	// The file starts as a truncated fragment — what a reader sees mid-write from a
	// writer that does not commit via rename. A background writer then lands the
	// complete file, well inside the first backoff, exactly as the foreign writer's
	// own write would settle a moment later.
	if err := os.WriteFile(cfgPath, []byte(`{"projects": {"/other": {"hasTrustDi`), 0o600); err != nil {
		t.Fatalf("hk-qx065: write torn fragment: %v", err)
	}
	settled := map[string]interface{}{
		"projects": map[string]interface{}{
			"/other": map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	raw, _ := json.MarshalIndent(settled, "", "  ")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(trustWriteRetryBackoff / 4)
		if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
			t.Errorf("hk-qx065: settle torn config: %v", err)
		}
	}()
	t.Cleanup(wg.Wait)

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-qx065: a transient torn read must be retried, not fatal; got: %v", err)
	}
	if !hkqx065Trusted(t, cfgPath, worktreePath) {
		t.Error("hk-qx065: key not on disk after recovering from the torn read")
	}
	// The settled content the other writer committed is preserved.
	if !hkqx065Trusted(t, cfgPath, "/other") {
		t.Error("hk-qx065: recovery dropped the other writer's settled content")
	}
}

// TestHkqx065_PermanentlyCorruptConfig_ReportsDecodeError is the other side of
// the decode-retry line: a config that is STILL unparseable after every attempt
// must surface the decode error itself, not a generic "did not persist", and must
// never have been overwritten (fail-rather-than-corrupt, WM-040b Preservation).
func TestHkqx065_PermanentlyCorruptConfig_ReportsDecodeError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-corrupt")

	const corrupt = "{not valid json"
	if err := os.WriteFile(cfgPath, []byte(corrupt), 0o600); err != nil {
		t.Fatalf("hk-qx065: write corrupt config: %v", err)
	}

	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	if err == nil {
		t.Fatal("hk-qx065: a permanently corrupt config must fail")
	}
	if !trustConfigDecodeErr(err) {
		t.Errorf("hk-qx065: error should be the decode failure itself, got: %v", err)
	}
	// The corrupt file is left exactly as found — we never replace it with a
	// freshly-marshalled config.
	data, readErr := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
	if readErr != nil {
		t.Fatalf("hk-qx065: read back corrupt config: %v", readErr)
	}
	if string(data) != corrupt {
		t.Errorf("hk-qx065: corrupt config was overwritten; got %q, want it untouched", data)
	}
}
