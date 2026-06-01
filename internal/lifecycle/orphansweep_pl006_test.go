package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test fixtures (pl006 prefix) — scoped to this bead (hk-8mup.11)
// ──────────────────────────────────────────────────────────────────────────────

// pl006FixtureFakeTmuxLister is an injectable TmuxSessionLister that returns a
// deterministic set of session names without consulting the OS tmux binary.
type pl006FixtureFakeTmuxLister struct {
	sessions []string
	err      error
}

// ListTmuxSessions implements TmuxSessionLister.
func (f *pl006FixtureFakeTmuxLister) ListTmuxSessions(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sessions, nil
}

// pl006FixtureFakeTmuxKiller records kill calls without invoking the real tmux
// binary. It lets tests assert which sessions were targeted.
type pl006FixtureFakeTmuxKiller struct {
	killed []string
	err    error
}

// KillTmuxSession implements TmuxSessionKiller.
func (f *pl006FixtureFakeTmuxKiller) KillTmuxSession(_ context.Context, name string) error {
	if f.err != nil {
		return f.err
	}
	f.killed = append(f.killed, name)
	return nil
}

// pl006FixtureFakeHandlerLister is an injectable HandlerProcessLister that
// returns a deterministic list of handler PIDs without consulting the OS.
type pl006FixtureFakeHandlerLister struct {
	pids []int
	err  error
}

// ListOrphanHandlerPIDs implements HandlerProcessLister.
func (f *pl006FixtureFakeHandlerLister) ListOrphanHandlerPIDs(_ context.Context, _ core.ProjectHash) ([]int, error) {
	return f.pids, f.err
}

// ──────────────────────────────────────────────────────────────────────────────
// (a) Tmux session sweep
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_SweepOrphanTmuxSessions_Empty verifies that SweepOrphanTmuxSessions
// returns 0 killed and no error when the session lister returns an empty list.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — tmux bullet.
func TestPL006_SweepOrphanTmuxSessions_Empty(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	lister := &pl006FixtureFakeTmuxLister{}
	killer := &pl006FixtureFakeTmuxKiller{}

	killed, err := SweepOrphanTmuxSessions(t.Context(), hash, lister, killer, nil, nil)
	if err != nil {
		t.Fatalf("PL-006 tmux empty: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("PL-006 tmux empty: killed = %d, want 0", killed)
	}
	if len(killer.killed) != 0 {
		t.Errorf("PL-006 tmux empty: killer recorded %v, want none", killer.killed)
	}
}

// TestPL006_SweepOrphanTmuxSessions_MatchingPrefix verifies that sessions
// matching the project-hash prefix are killed and counted.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "kill every matching session
// via tmux kill-session."
func TestPL006_SweepOrphanTmuxSessions_MatchingPrefix(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)
	prefix := TmuxSessionPrefix(hash)

	matchingNames := []string{
		prefix + "run-aaa",
		prefix + "run-bbb",
	}
	nonMatchingNames := []string{
		"harmonik-000000000000-run-ccc", // different project hash
		"unrelated-session",
	}
	allNames := append(matchingNames, nonMatchingNames...)

	lister := &pl006FixtureFakeTmuxLister{sessions: allNames}
	killer := &pl006FixtureFakeTmuxKiller{}

	// Shorten the poll ceiling so the test doesn't wait 2 seconds.
	origCeiling := tmuxPollCeiling
	tmuxPollCeiling = 0
	t.Cleanup(func() { tmuxPollCeiling = origCeiling })

	killed, err := SweepOrphanTmuxSessions(t.Context(), hash, lister, killer, nil, nil)
	if err != nil {
		t.Fatalf("PL-006 tmux prefix: unexpected error: %v", err)
	}
	if killed != 2 {
		t.Errorf("PL-006 tmux prefix: killed = %d, want 2", killed)
	}

	// Exactly the matching sessions must have been killed.
	killedSet := make(map[string]bool, len(killer.killed))
	for _, name := range killer.killed {
		killedSet[name] = true
	}
	for _, name := range matchingNames {
		if !killedSet[name] {
			t.Errorf("PL-006 tmux prefix: matching session %q was not killed", name)
		}
	}
	for _, name := range nonMatchingNames {
		if killedSet[name] {
			t.Errorf("PL-006 tmux prefix: non-matching session %q was incorrectly killed", name)
		}
	}
}

// TestPL006_SweepOrphanTmuxSessions_KillErrorNonFatal verifies that a
// kill-session error is treated as non-fatal (the session may have already
// exited), and the session is still counted as killed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — sessions can have already exited
// by the time the sweep runs.
func TestPL006_SweepOrphanTmuxSessions_KillErrorNonFatal(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)
	prefix := TmuxSessionPrefix(hash)

	lister := &pl006FixtureFakeTmuxLister{
		sessions: []string{prefix + "gone-already"},
	}
	killer := &pl006FixtureFakeTmuxKiller{err: errors.New("no server running on /tmp/tmux-xxx")}

	origCeiling := tmuxPollCeiling
	tmuxPollCeiling = 0
	t.Cleanup(func() { tmuxPollCeiling = origCeiling })

	// kill error must not surface as a function error.
	killed, err := SweepOrphanTmuxSessions(t.Context(), hash, lister, killer, nil, nil)
	if err != nil {
		t.Fatalf("PL-006 tmux kill-error: unexpected error: %v", err)
	}
	// The session matched the prefix, so it is counted even if the kill failed.
	if killed != 1 {
		t.Errorf("PL-006 tmux kill-error: killed = %d, want 1", killed)
	}
}

// TestPL006_SweepOrphanTmuxSessions_ListError verifies that an error from the
// session lister is propagated.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — tmux bullet.
func TestPL006_SweepOrphanTmuxSessions_ListError(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	wantErr := errors.New("tmux not installed")
	lister := &pl006FixtureFakeTmuxLister{err: wantErr}
	killer := &pl006FixtureFakeTmuxKiller{}

	_, err := SweepOrphanTmuxSessions(t.Context(), hash, lister, killer, nil, nil)
	if err == nil {
		t.Fatal("PL-006 tmux list-error: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("PL-006 tmux list-error: error %q does not mention list", err.Error())
	}
}

// TestPL006_SweepOrphanTmuxSessions_NilListerAndKillerUsesOS verifies that
// passing nil for lister and killer does not panic (OS implementations are
// used, which may return empty or error).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — tmux bullet.
func TestPL006_SweepOrphanTmuxSessions_NilListerAndKillerUsesOS(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	origCeiling := tmuxPollCeiling
	tmuxPollCeiling = 0
	t.Cleanup(func() { tmuxPollCeiling = origCeiling })

	// Must not panic even if tmux is not installed.
	_, err := SweepOrphanTmuxSessions(t.Context(), hash, nil, nil, nil, nil)
	if err != nil {
		t.Logf("PL-006 tmux nil-lister: tmux not available (acceptable): %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// (c) Handler subprocess sweep
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_SweepOrphanHandlers_Empty verifies that SweepOrphanHandlers returns
// 0 killed when the lister returns no PIDs.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — subprocess cleanup bullet.
func TestPL006_SweepOrphanHandlers_Empty(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	lister := &pl006FixtureFakeHandlerLister{}
	killed, err := SweepOrphanHandlers(t.Context(), hash, lister, nil)
	if err != nil {
		t.Fatalf("PL-006 handlers empty: unexpected error: %v", err)
	}
	if killed != 0 {
		t.Errorf("PL-006 handlers empty: killed = %d, want 0", killed)
	}
}

// TestPL006_SweepOrphanHandlers_DeadPIDs verifies that a list of known-dead PIDs
// is processed without error and does not appear in survivors.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "SIGTERM followed by SIGKILL
// after a bounded 5-second interval."
func TestPL006_SweepOrphanHandlers_DeadPIDs(t *testing.T) {
	t.Parallel()

	const deadPID = 99994
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-006 handlers dead-pid: PID %d is live on this host; skipping", deadPID)
	}

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	lister := &pl006FixtureFakeHandlerLister{pids: []int{deadPID}}

	origGrace := handlerSweepGracePeriod
	handlerSweepGracePeriod = 200 * time.Millisecond
	t.Cleanup(func() { handlerSweepGracePeriod = origGrace })

	_, err := SweepOrphanHandlers(t.Context(), hash, lister, nil)
	if err != nil {
		t.Fatalf("PL-006 handlers dead-pid: unexpected error: %v", err)
	}
}

// TestPL006_SweepOrphanHandlers_ListError verifies that a lister error is
// propagated from SweepOrphanHandlers.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — subprocess cleanup bullet.
func TestPL006_SweepOrphanHandlers_ListError(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	wantErr := errors.New("ps not found")
	lister := &pl006FixtureFakeHandlerLister{err: wantErr}

	_, err := SweepOrphanHandlers(t.Context(), hash, lister, nil)
	if err == nil {
		t.Fatal("PL-006 handlers list-error: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "enumerate") {
		t.Errorf("PL-006 handlers list-error: error %q does not mention enumerate", err.Error())
	}
}

// TestPL006_SweepOrphanHandlers_NilListerUsesOS verifies that passing nil for
// lister falls back to OSHandlerProcessLister without panicking.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — subprocess cleanup bullet.
func TestPL006_SweepOrphanHandlers_NilListerUsesOS(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	hash := ComputeProjectHash(projectDir)

	// Should not panic; may enumerate 0 or more real processes.
	killed, err := SweepOrphanHandlers(t.Context(), hash, nil, nil)
	if err != nil {
		t.Logf("PL-006 handlers nil-lister: OS lister returned error (acceptable): %v", err)
	}
	t.Logf("PL-006 handlers nil-lister: killed = %d", killed)
}

// ──────────────────────────────────────────────────────────────────────────────
// (d) Stale intent enumeration
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_EnumerateStaleIntents_Empty verifies that EnumerateStaleIntents
// returns 0 when the directory does not exist.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — stale intent files bullet.
func TestPL006_EnumerateStaleIntents_Empty(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	daemonStart := time.Now()

	count, err := EnumerateStaleIntents(projectDir, daemonStart)
	if err != nil {
		t.Fatalf("PL-006 intents empty: unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("PL-006 intents empty: count = %d, want 0", count)
	}
}

// TestPL006_EnumerateStaleIntents_CountsStale verifies that EnumerateStaleIntents
// returns the correct count of files older than daemonStartTime.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "entries older than the current
// daemon's start time."
func TestPL006_EnumerateStaleIntents_CountsStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	// Seed three stale intents before recording daemonStartTime.
	for _, id := range []string{"int-a", "int-b", "int-c"} {
		startupSweepFixtureSeedStaleIntent(t, projectDir, id)
	}

	// Record daemon start AFTER seeding (all files are stale relative to this).
	daemonStart := time.Now()

	count, err := EnumerateStaleIntents(projectDir, daemonStart)
	if err != nil {
		t.Fatalf("PL-006 intents count: unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("PL-006 intents count: count = %d, want 3", count)
	}
}

// TestPL006_EnumerateStaleIntents_NewFilesNotCounted verifies that files whose
// mtime is after daemonStartTime are NOT counted as stale.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "entries older than the current
// daemon's start time."
func TestPL006_EnumerateStaleIntents_NewFilesNotCounted(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	daemonStart := time.Now() // record start BEFORE creating the file

	// Seed one intent file after the start time.
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("PL-006 intents new: MkdirAll: %v", err)
	}
	newPath := filepath.Join(intentsDir, "new-intent.json")
	if err := os.WriteFile(newPath, []byte(`{"id":"new"}`), 0o600); err != nil {
		t.Fatalf("PL-006 intents new: WriteFile: %v", err)
	}
	// mtime is now (after daemonStart) — not stale.

	count, err := EnumerateStaleIntents(projectDir, daemonStart)
	if err != nil {
		t.Fatalf("PL-006 intents new: unexpected error: %v", err)
	}
	// File was created after daemonStart — should not be counted.
	if count != 0 {
		t.Errorf("PL-006 intents new: count = %d, want 0 (file is not stale)", count)
	}
}

// TestPL006_EnumerateStaleIntents_FilesLeftOnDisk verifies that
// EnumerateStaleIntents DOES NOT remove intent files from disk.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale entries MUST be LEFT on
// disk for classification by the reconciliation Cat 3a detector."
func TestPL006_EnumerateStaleIntents_FilesLeftOnDisk(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	seededPaths := make([]string, 0, 3)
	for _, id := range []string{"keep-a", "keep-b", "keep-c"} {
		p := startupSweepFixtureSeedStaleIntent(t, projectDir, id)
		seededPaths = append(seededPaths, p)
	}

	daemonStart := time.Now()

	if _, err := EnumerateStaleIntents(projectDir, daemonStart); err != nil {
		t.Fatalf("PL-006 intents on-disk: unexpected error: %v", err)
	}

	// All files must still exist on disk.
	for _, p := range seededPaths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("PL-006 intents on-disk: file %q was removed; MUST be left for reconciliation", p)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// (e) Reconciliation lock sweep
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_SweepStaleReconciliationLocks_NoDir verifies that
// SweepStaleReconciliationLocks returns 0 (no error) when the
// reconciliation-locks directory does not exist.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — reconciliation locks bullet.
func TestPL006_SweepStaleReconciliationLocks_NoDir(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("PL-006 recon-locks no-dir: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("PL-006 recon-locks no-dir: removed = %d, want 0", result.Removed)
	}
}

// TestPL006_SweepStaleReconciliationLocks_RemovesStale verifies that stale
// reconciliation lock files (acquirable + dead creator PID) are removed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale lock files (acquirable
// + the recorded creator-PID does NOT respond to kill(pid, 0)) MUST be removed
// via unlink followed by fsync(parent_directory_fd)."
func TestPL006_SweepStaleReconciliationLocks_RemovesStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99993
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-006 recon-locks remove: PID %d is live; skipping", deadPID)
	}

	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-sweep-stale", deadPID, false)

	// Verify exists before sweep.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatalf("PL-006 recon-locks remove: lock file absent before sweep")
	}

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("PL-006 recon-locks remove: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("PL-006 recon-locks remove: removed = %d, want 1", result.Removed)
	}

	// File must be gone after sweep.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("PL-006 recon-locks remove: lock file still exists after sweep; want removed")
	}
}

// TestPL006_SweepStaleReconciliationLocks_MultipleFiles verifies correct
// counting when multiple stale lock files are swept.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — reconciliation locks bullet.
func TestPL006_SweepStaleReconciliationLocks_MultipleFiles(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99992
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-006 recon-locks multi: PID %d is live; skipping", deadPID)
	}

	for _, name := range []string{"run-multi-a", "run-multi-b", "run-multi-c"} {
		startupSweepFixtureSeedReconciliationLock(t, projectDir, name, deadPID, false)
	}

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("PL-006 recon-locks multi: unexpected error: %v", err)
	}
	if result.Removed != 3 {
		t.Errorf("PL-006 recon-locks multi: removed = %d, want 3", result.Removed)
	}
}

// TestPL006_SweepStaleReconciliationLocks_WithVerdictTrailer verifies that
// stale lock files WITH "Harmonik-Verdict-Executed: true" are also removed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "a stale lock file whose
// investigator task branch carries a Harmonik-Verdict-Executed: true commit per
// RC-002b is also unlinked here (the lock outlived its useful purpose)."
func TestPL006_SweepStaleReconciliationLocks_WithVerdictTrailer(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99991
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-006 recon-locks verdict: PID %d is live; skipping", deadPID)
	}

	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-verdict", deadPID, true)

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("PL-006 recon-locks verdict: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("PL-006 recon-locks verdict: removed = %d, want 1", result.Removed)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("PL-006 recon-locks verdict: lock file still exists; want removed")
	}
	// RC-002b: stale lock WITH verdict-executed must NOT produce Cat 3b run IDs.
	if len(result.Cat3bRunIDs) != 0 {
		t.Errorf("PL-006 recon-locks verdict: Cat3bRunIDs = %v, want empty (verdict was executed)", result.Cat3bRunIDs)
	}
}

// TestPL006_SweepStaleReconciliationLocks_NonLockFilesIgnored verifies that
// files in the reconciliation-locks directory that do NOT have the .lock suffix
// are not processed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — ".harmonik/reconciliation-locks/*.lock".
func TestPL006_SweepStaleReconciliationLocks_NonLockFilesIgnored(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("PL-006 recon-locks non-lock: MkdirAll: %v", err)
	}

	// Write a non-.lock file.
	notLockPath := filepath.Join(lockDir, "README.txt")
	if err := os.WriteFile(notLockPath, []byte("readme"), 0o600); err != nil {
		t.Fatalf("PL-006 recon-locks non-lock: WriteFile: %v", err)
	}

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("PL-006 recon-locks non-lock: unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("PL-006 recon-locks non-lock: removed = %d, want 0", result.Removed)
	}
	// README.txt must still be present.
	if _, err := os.Stat(notLockPath); os.IsNotExist(err) {
		t.Error("PL-006 recon-locks non-lock: README.txt was removed; MUST be ignored")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RC-002b: SweepStaleReconciliationLocks Cat3b routing
// ──────────────────────────────────────────────────────────────────────────────

// TestRC002b_SweepWithVerdictExecutedNoCat3b verifies that a stale lock file
// carrying "Harmonik-Verdict-Executed: true" is removed and NOT included in
// Cat3bRunIDs. Per RC-002b: the lock outlived its useful purpose; no Cat 3b
// routing is required.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b.
func TestRC002b_SweepWithVerdictExecutedNoCat3b(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99988
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("RC-002b sweep with-verdict: PID %d is live; skipping", deadPID)
	}

	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-rc002b-with-verdict", deadPID, true)

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("RC-002b sweep with-verdict: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("RC-002b sweep with-verdict: Removed = %d, want 1", result.Removed)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("RC-002b sweep with-verdict: lock file still exists after sweep; want removed")
	}
	if len(result.Cat3bRunIDs) != 0 {
		t.Errorf("RC-002b sweep with-verdict: Cat3bRunIDs = %v, want empty (verdict was already executed)", result.Cat3bRunIDs)
	}
}

// TestRC002b_SweepWithoutVerdictExecutedYieldsCat3b verifies that a stale lock
// file NOT carrying "Harmonik-Verdict-Executed: true" is removed AND its run ID
// appears in Cat3bRunIDs. Per RC-002b: the daemon must route that run through
// Cat 3b (verdict-emitted-but-unexecuted) on the next reconciliation pass.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b.
func TestRC002b_SweepWithoutVerdictExecutedYieldsCat3b(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99987
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("RC-002b sweep no-verdict: PID %d is live; skipping", deadPID)
	}

	const runID = "run-rc002b-no-verdict"
	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, runID, deadPID, false)

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("RC-002b sweep no-verdict: unexpected error: %v", err)
	}
	if result.Removed != 1 {
		t.Errorf("RC-002b sweep no-verdict: Removed = %d, want 1", result.Removed)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("RC-002b sweep no-verdict: lock file still exists after sweep; want removed")
	}
	if len(result.Cat3bRunIDs) != 1 {
		t.Fatalf("RC-002b sweep no-verdict: Cat3bRunIDs = %v, want [%q]", result.Cat3bRunIDs, runID)
	}
	if result.Cat3bRunIDs[0] != runID {
		t.Errorf("RC-002b sweep no-verdict: Cat3bRunIDs[0] = %q, want %q", result.Cat3bRunIDs[0], runID)
	}
}

// TestRC002b_SweepMixedVerdictStateDiscriminates verifies that when both kinds
// of stale lock (with and without verdict-executed trailer) exist, only the
// without-verdict run ID appears in Cat3bRunIDs.
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-002b.
func TestRC002b_SweepMixedVerdictStateDiscriminates(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	const deadPID = 99986
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("RC-002b sweep mixed: PID %d is live; skipping", deadPID)
	}

	const withVerdictRunID = "run-rc002b-mix-with"
	const withoutVerdictRunID = "run-rc002b-mix-without"
	startupSweepFixtureSeedReconciliationLock(t, projectDir, withVerdictRunID, deadPID, true)
	startupSweepFixtureSeedReconciliationLock(t, projectDir, withoutVerdictRunID, deadPID, false)

	result, err := SweepStaleReconciliationLocks(projectDir, nil)
	if err != nil {
		t.Fatalf("RC-002b sweep mixed: unexpected error: %v", err)
	}
	if result.Removed != 2 {
		t.Errorf("RC-002b sweep mixed: Removed = %d, want 2", result.Removed)
	}
	if len(result.Cat3bRunIDs) != 1 {
		t.Fatalf("RC-002b sweep mixed: Cat3bRunIDs = %v, want [%q]", result.Cat3bRunIDs, withoutVerdictRunID)
	}
	if result.Cat3bRunIDs[0] != withoutVerdictRunID {
		t.Errorf("RC-002b sweep mixed: Cat3bRunIDs[0] = %q, want %q", result.Cat3bRunIDs[0], withoutVerdictRunID)
	}
}
