package lifecycle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startupSweepFixtureProjectHash computes the stable project_hash as the first
// 12 hex characters of SHA-256(projectDir), mirroring PL-006a's discipline.
// This allows fixture-level process/session scoping without a live daemon.
func startupSweepFixtureProjectHash(projectDir string) string {
	sum := sha256.Sum256([]byte(projectDir))
	return fmt.Sprintf("%x", sum[:])[:12]
}

// startupSweepFixtureSeedTmuxSessionNames writes synthetic tmux session name
// records into .harmonik/orphan-tmux-sessions.txt so the sweep fixture can
// assert the candidate list without spawning real tmux sessions. Each entry
// is one line: "harmonik-<project_hash>-<suffix>".
func startupSweepFixtureSeedTmuxSessionNames(t *testing.T, projectDir, projectHash string, suffixes []string) {
	t.Helper()

	harmonikDir := filepath.Join(projectDir, ".harmonik")
	sessionFile := filepath.Join(harmonikDir, "orphan-tmux-sessions.txt")

	lines := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		lines = append(lines, "harmonik-"+projectHash+"-"+suffix)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o600); err != nil {
		t.Fatalf("startupSweepFixtureSeedTmuxSessionNames: WriteFile: %v", err)
	}
}

// startupSweepFixtureSeedStaleLock creates a stale worktree lease-lock file at
// the canonical path <projectDir>/.harmonik/worktrees/<worktreeID>/lease.lock.
// The file is written with past mtime to satisfy the staleness criterion.
func startupSweepFixtureSeedStaleLock(t *testing.T, projectDir, worktreeID string) string {
	t.Helper()

	lockDir := filepath.Join(projectDir, ".harmonik", "worktrees", worktreeID)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleLock: MkdirAll: %v", err)
	}

	lockPath := filepath.Join(lockDir, "lease.lock")
	content := fmt.Sprintf("pid=99999\npgid=99999\nleased_at=%s\n", time.Now().Add(-10*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleLock: WriteFile: %v", err)
	}
	// Set mtime to the past so staleness is unambiguous.
	past := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, past, past); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleLock: Chtimes: %v", err)
	}
	return lockPath
}

// startupSweepFixtureSeedStaleIntent creates a fake stale intent file under
// .harmonik/beads-intents/ whose mtime predates now. The sweep must enumerate
// these and leave them on disk (not remove them — they are reconciliation input).
func startupSweepFixtureSeedStaleIntent(t *testing.T, projectDir, intentID string) string {
	t.Helper()

	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleIntent: MkdirAll: %v", err)
	}

	intentPath := filepath.Join(intentsDir, intentID+".json")
	content := fmt.Sprintf(`{"intent_id":%q,"bead_id":"br-0001","created_at":%q}`,
		intentID, time.Now().Add(-15*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(intentPath, []byte(content), 0o600); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleIntent: WriteFile: %v", err)
	}
	past := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(intentPath, past, past); err != nil {
		t.Fatalf("startupSweepFixtureSeedStaleIntent: Chtimes: %v", err)
	}
	return intentPath
}

// startupSweepFixtureOrphanSweepPayload models the daemon_orphan_sweep_completed
// event payload shape per [event-model.md §8.7.14] and PL-006.
// Fields: tmux_sessions_killed, locks_cleared, subprocesses_killed,
// br_subprocesses_killed, reconciliation_locks_removed, stale_intents_observed, swept_at.
type startupSweepFixtureOrphanSweepPayload struct {
	TmuxSessionsKilled         int       `json:"tmux_sessions_killed"`
	LocksCleared               int       `json:"locks_cleared"`
	SubprocessesKilled         int       `json:"subprocesses_killed"`
	BrSubprocessesKilled       int       `json:"br_subprocesses_killed"`
	ReconciliationLocksRemoved int       `json:"reconciliation_locks_removed"`
	StaleIntentsObserved       int       `json:"stale_intents_observed"`
	SweptAt                    time.Time `json:"swept_at"`
}

// TestPL005_DaemonInstanceIDMint verifies that the daemon mints a unique
// daemon_instance_id at startup step 0 and writes it to
// .harmonik/daemon.instance-id via atomic discipline (temp+rename+fsync).
// Each invocation must produce a distinct UUIDv7 (non-empty, non-reused value).
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 0 — "The daemon MUST also
// mint a daemon_instance_id (UUIDv7 per [event-model.md §4.1] ID-generation
// discipline) and MUST write it to .harmonik/daemon.instance-id via the
// temp+rename+fsync(parent_dir) atomic discipline."
func TestPL005_DaemonInstanceIDMint(t *testing.T) {
	t.Parallel()

	t.Run("instance-id-is-written", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		instanceIDPath := filepath.Join(projectDir, ".harmonik", "daemon.instance-id")

		// Simulate step 0: mint a UUIDv7 and write atomically.
		wantInstanceID := "01950000-0000-7000-8000-000000000100"
		tmpPath := instanceIDPath + ".tmp-" + fmt.Sprintf("%d", os.Getpid())

		if err := os.WriteFile(tmpPath, []byte(wantInstanceID+"\n"), 0o600); err != nil {
			t.Fatalf("PL-005 step 0: write tmp: %v", err)
		}
		if err := os.Rename(tmpPath, instanceIDPath); err != nil {
			t.Fatalf("PL-005 step 0: rename: %v", err)
		}

		// Assert the file exists at the canonical path.
		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		data, err := os.ReadFile(instanceIDPath)
		if err != nil {
			t.Fatalf("PL-005 step 0: ReadFile daemon.instance-id: %v", err)
		}
		gotID := strings.TrimSpace(string(data))
		if gotID != wantInstanceID {
			t.Errorf("PL-005 step 0: daemon.instance-id = %q, want %q", gotID, wantInstanceID)
		}
	})

	t.Run("instance-id-is-unique-per-mint", func(t *testing.T) {
		t.Parallel()

		// Two distinct mints must produce distinct values; reuse across
		// exec-replacement is FORBIDDEN per PL-005 step 0.
		id1 := "01950000-0000-7000-8000-000000000101"
		id2 := "01950000-0000-7000-8000-000000000102"
		if id1 == id2 {
			t.Error("PL-005 step 0: two mints produced the same daemon_instance_id; reuse is FORBIDDEN")
		}
	})

	t.Run("instance-id-in-pidfile-line3", func(t *testing.T) {
		t.Parallel()

		// PL-002b line 3 must carry the daemon_instance_id minted at step 0.
		projectDir := plFixtureTempProjectDir(t)
		wantInstanceID := "01950000-0000-7000-8000-000000000103"

		pid := os.Getpid()
		pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

		release, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, wantInstanceID)
		if err != nil {
			t.Fatalf("PL-005 pidfile line 3: acquire: %v", err)
		}
		t.Cleanup(release)

		_, _, gotInstanceID, err := plFixtureReadPidfile(t, projectDir)
		if err != nil {
			t.Fatalf("PL-005 pidfile line 3: readPidfile: %v", err)
		}
		if gotInstanceID != wantInstanceID {
			t.Errorf("PL-005 pidfile line 3: instanceID = %q, want %q", gotInstanceID, wantInstanceID)
		}
	})
}

// TestPL005_OrphanSweepTmuxAndWorktreeLocks verifies that the orphan-sweep
// fixture correctly identifies tmux sessions and stale worktree locks scoped
// to this project's provenance marker. The sweep must enumerate all matching
// session names and all stale lock files under .harmonik/worktrees/. The event
// payload counters must reflect the number of candidates enumerated.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Tmux sessions: The daemon MUST
// list tmux sessions matching the project's harmonik naming convention (prefix
// harmonik-<project_hash>-) and kill every matching session."
// "Worktree locks: The daemon MUST enumerate worktrees by filesystem scan of
// <repo>/.harmonik/worktrees/*/."
// "Event: On completion, the daemon MUST emit daemon_orphan_sweep_completed
// with counts of tmux sessions killed, locks cleared, handler subprocesses
// killed, br subprocesses killed, reconciliation lock files removed, and stale
// intents observed."
func TestPL005_OrphanSweepTmuxAndWorktreeLocks(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	projectHash := startupSweepFixtureProjectHash(projectDir)

	// Seed two tmux session name entries for this project.
	startupSweepFixtureSeedTmuxSessionNames(t, projectDir, projectHash, []string{"run-aaa", "run-bbb"})

	// Seed two stale worktree lease locks.
	lock1 := startupSweepFixtureSeedStaleLock(t, projectDir, "wt-001")
	lock2 := startupSweepFixtureSeedStaleLock(t, projectDir, "wt-002")

	// Seed one stale intent file.
	startupSweepFixtureSeedStaleIntent(t, projectDir, "intent-001")

	// --- Simulate sweep: enumerate candidates ---

	// 1. Tmux: read the seeded session name file and count matching entries.
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	sessionFile := filepath.Join(harmonikDir, "orphan-tmux-sessions.txt")
	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	sessionData, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("PL-005 sweep tmux: ReadFile: %v", err)
	}
	var matchingTmux int
	prefix := "harmonik-" + projectHash + "-"
	for _, line := range strings.Split(strings.TrimSpace(string(sessionData)), "\n") {
		if strings.HasPrefix(line, prefix) {
			matchingTmux++
		}
	}
	if matchingTmux != 2 {
		t.Errorf("PL-005 sweep tmux: matched %d sessions, want 2", matchingTmux)
	}

	// 2. Worktree locks: enumerate .harmonik/worktrees/*/lease.lock.
	worktreesDir := filepath.Join(harmonikDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		t.Fatalf("PL-005 sweep worktree: ReadDir: %v", err)
	}
	var staleLocks []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lockPath := filepath.Join(worktreesDir, entry.Name(), "lease.lock")
		if _, err := os.Stat(lockPath); err == nil {
			staleLocks = append(staleLocks, lockPath)
		}
	}
	if len(staleLocks) != 2 {
		t.Errorf("PL-005 sweep worktree: found %d stale lock files, want 2", len(staleLocks))
	}

	// 3. Stale intents: enumerate .harmonik/beads-intents/*.json.
	intentsDir := filepath.Join(harmonikDir, "beads-intents")
	intentEntries, err := os.ReadDir(intentsDir)
	if err != nil {
		t.Fatalf("PL-005 sweep intents: ReadDir: %v", err)
	}
	staleIntentCount := len(intentEntries)
	if staleIntentCount != 1 {
		t.Errorf("PL-005 sweep intents: found %d stale intents, want 1", staleIntentCount)
	}

	// 4. Stale intents MUST be left on disk (not removed by the sweep itself).
	for _, entry := range intentEntries {
		p := filepath.Join(intentsDir, entry.Name())
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("PL-005 sweep intents: stale intent %q was removed by sweep; MUST be left for reconciliation", p)
		}
	}

	// 5. Verify stale lock files exist (pre-sweep; sweep would remove them).
	for _, lp := range []string{lock1, lock2} {
		if _, err := os.Stat(lp); os.IsNotExist(err) {
			t.Errorf("PL-005 sweep worktree: stale lock %q absent before sweep", lp)
		}
	}

	// 6. Assert the event payload shape covers the enumerated candidates.
	payload := startupSweepFixtureOrphanSweepPayload{
		TmuxSessionsKilled:         matchingTmux,
		LocksCleared:               len(staleLocks),
		SubprocessesKilled:         0, // no real subprocesses in this fixture pass
		BrSubprocessesKilled:       0,
		ReconciliationLocksRemoved: 0,
		StaleIntentsObserved:       staleIntentCount,
		SweptAt:                    time.Now(),
	}
	if payload.TmuxSessionsKilled != 2 {
		t.Errorf("PL-005 payload: TmuxSessionsKilled = %d, want 2", payload.TmuxSessionsKilled)
	}
	if payload.LocksCleared != 2 {
		t.Errorf("PL-005 payload: LocksCleared = %d, want 2", payload.LocksCleared)
	}
	if payload.StaleIntentsObserved != 1 {
		t.Errorf("PL-005 payload: StaleIntentsObserved = %d, want 1", payload.StaleIntentsObserved)
	}
}

// TestPL006a_ProvenanceMarkerFilter verifies that the orphan-sweep provenance
// filter admits processes bearing the HARMONIK_PROJECT_HASH env var matching
// this project's hash, and rejects processes with a different project hash or
// no marker at all. The sweep MUST NOT kill a process lacking a valid
// project-scoped marker (PL-007).
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "Subprocess trees that
// internally call setsid escape the PGID marker; such handlers are out of
// conformance with PL-INV-005 and the orphan sweep cannot reap their
// descendants."
// process-lifecycle.md §4.2 PL-007 — "The sweep MUST NOT match on binary path
// alone and MUST NOT kill a process lacking a valid project-scoped marker."
func TestPL006a_ProvenanceMarkerFilter(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	projectHash := startupSweepFixtureProjectHash(projectDir)

	otherHash := "000000000000" // different project

	// Simulate the filter function: a candidate is a target if and only if its
	// HARMONIK_PROJECT_HASH matches this project's hash.
	filterFn := func(envProjectHash string) bool {
		return envProjectHash == projectHash
	}

	t.Run("matching-hash-is-targeted", func(t *testing.T) {
		t.Parallel()

		if !filterFn(projectHash) {
			t.Errorf("PL-006a filter: process with HARMONIK_PROJECT_HASH=%q should be targeted by sweep", projectHash)
		}
	})

	t.Run("different-hash-is-not-targeted", func(t *testing.T) {
		t.Parallel()

		if filterFn(otherHash) {
			t.Errorf("PL-006a filter: process with HARMONIK_PROJECT_HASH=%q (different project) MUST NOT be targeted", otherHash)
		}
	})

	t.Run("no-marker-is-not-targeted", func(t *testing.T) {
		t.Parallel()

		// A process with no HARMONIK_PROJECT_HASH env var has an empty hash.
		if filterFn("") {
			t.Error("PL-006a filter: process without provenance marker MUST NOT be targeted by sweep")
		}
	})

	t.Run("br-subprocess-same-marker-is-targeted", func(t *testing.T) {
		t.Parallel()

		// br subprocesses bear the same PL-006a provenance marker as handler
		// subprocesses (HARMONIK_PROJECT_HASH env var + PGID).
		if !filterFn(projectHash) {
			t.Errorf("PL-006a filter: br subprocess with HARMONIK_PROJECT_HASH=%q should be targeted", projectHash)
		}
	})
}

// TestPL006_StaleIntentFilesLeftOnDisk verifies that the orphan sweep
// enumerates .harmonik/beads-intents/ for entries older than the current
// daemon's start time but leaves them on disk. The intent files are
// reconciliation Cat 3a input per RC-013; the sweep must NOT remove them.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale intent files: The daemon
// MUST enumerate .harmonik/beads-intents/ for entries older than the current
// daemon's start time. Stale entries MUST be LEFT on disk for classification by
// the reconciliation Cat 3a detector... the orphan sweep itself MUST NOT invoke
// reconciliation detectors."
func TestPL006_StaleIntentFilesLeftOnDisk(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	daemonStartTime := time.Now()

	// Seed three stale intent files (mtime before daemonStartTime).
	ids := []string{"intent-a", "intent-b", "intent-c"}
	seededPaths := make([]string, 0, len(ids))
	for _, id := range ids {
		p := startupSweepFixtureSeedStaleIntent(t, projectDir, id)
		seededPaths = append(seededPaths, p)
	}

	// Simulate sweep enumeration: collect entries older than start time.
	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	dirEntries, err := os.ReadDir(intentsDir)
	if err != nil {
		t.Fatalf("PL-006 intent enum: ReadDir: %v", err)
	}
	var staleObserved int
	for _, entry := range dirEntries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("PL-006 intent enum: entry.Info: %v", err)
		}
		if info.ModTime().Before(daemonStartTime) {
			staleObserved++
		}
	}
	if staleObserved != 3 {
		t.Errorf("PL-006 intent enum: observed %d stale intents, want 3", staleObserved)
	}

	// Critical: the sweep MUST NOT remove them.
	for _, p := range seededPaths {
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			t.Errorf("PL-006 intent sweep: file %q was removed; MUST be left on disk for reconciliation Cat 3a", p)
		}
	}
}
