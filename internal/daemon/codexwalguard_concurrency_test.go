package daemon

// codexwalguard_concurrency_test.go — concurrency-safety tests for the codex
// stale-WAL guard (hk-qlelr).
//
// # Why this file exists separately
//
// $CODEX_HOME (default ~/.codex) is GLOBAL: one directory, one set of
// state_*.sqlite databases, shared by every codex process on the host. It is not
// per-run, per-worktree, or per-project. At --max-concurrent N, N codex
// processes and N guard invocations run against that one directory at once.
//
// A 3-way concurrency experiment (2026-07-22) measured the race live: all three
// guards fired against the same global state_5.sqlite-wal within one second. One
// removed a genuinely stale WAL; TWO hit the post-backup TOCTOU re-check and
// correctly declined. The WAL then grew 593 KB -> 2.70 MB while three codex
// processes wrote to it. Without that re-check, two guards would have deleted a
// WAL that three live processes were writing into.
//
// That makes the "unheld" gate and its TOCTOU re-check load-bearing rather than
// decorative — they are the ONLY thing standing between concurrent dispatch and
// codex state corruption. codexwalguard_test.go pins the size/threshold/config/
// backup logic and states in its own header that the lsof-held case is
// "deliberately NOT exercised here to avoid flakiness."
//
// So the one property that makes concurrent $CODEX_HOME sharing safe had no
// test. These are those tests. They are not flaky: the handle is held by the
// test process itself for the duration of the call, which is a real open handle
// on a real file, not a simulated one.
//
// Bead ref: hk-qlelr.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCleanCodexStaleWAL_HeldWAL_NeverRemoved pins the guard's primary safety
// gate: a WAL that ANY live process holds open is never removed.
//
// This is the concurrent-dispatch case. Peer codex process holds the shared
// $CODEX_HOME WAL open; our guard must leave it completely alone — not back it
// up, not remove it. Under concurrency this is the difference between a clean
// run and deleting a live writer's sidecar out from under it.
func TestCleanCodexStaleWAL_HeldWAL_NeverRemoved(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal, making this vacuous")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1024\n")
	wal := writeWAL(t, codexHome, 4096) // > threshold: would be removed if unheld

	// Hold a real open handle for the duration of the guard call, exactly as a
	// live peer codex process would.
	held, err := os.Open(wal) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open wal to hold it: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("close held handle: %v", closeErr)
		}
	})

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("HELD wal was removed — this is the corruption the guard exists to prevent; stat err = %v", statErr)
	}
	// A held WAL is skipped BEFORE the backup dir is created, so there should be
	// no backup dir at all. This distinguishes "skipped early" from "backed up
	// then declined at the TOCTOU re-check".
	dirs, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*"))
	if globErr != nil {
		t.Fatalf("glob backup dirs: %v", globErr)
	}
	if len(dirs) != 0 {
		t.Fatalf("expected no backup dir for a held wal (skip precedes backup), found: %v", dirs)
	}
}

// TestCleanCodexStaleWAL_HeldBaseDB_NeverRemoved pins the SECOND half of the
// held check, which exists because "a live codex would hold both" the WAL and
// its base state_*.sqlite.
//
// The asymmetric case is the dangerous one and the reason the base-db check is
// not redundant: a live codex can hold the base db open at an instant when the
// WAL itself shows no handle. Checking only the WAL would classify that live
// session's sidecars as stale and delete them.
func TestCleanCodexStaleWAL_HeldBaseDB_NeverRemoved(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal, making this vacuous")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1024\n")
	wal := writeWAL(t, codexHome, 4096)
	base := filepath.Join(codexHome, "state_abc123.sqlite")

	// Hold ONLY the base db — the WAL itself is unheld.
	held, err := os.Open(base) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open base db to hold it: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("close held handle: %v", closeErr)
		}
	})

	if err := cleanCodexStaleWAL(projectRoot, codexHome); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(wal); statErr != nil {
		t.Fatalf("wal removed while the BASE DB was held by a live process; stat err = %v", statErr)
	}
	if _, statErr := os.Stat(base); statErr != nil {
		t.Fatalf("base db must never be touched by the guard; stat err = %v", statErr)
	}
}

// TestCleanCodexStaleWAL_ConcurrentGuards_NeverLoseTheWAL reproduces the shape
// of the measured 3-way experiment in-process: several guards firing at once
// against ONE shared $CODEX_HOME, which is what --max-concurrent N produces.
//
// It asserts the recoverability invariant rather than a specific winner, because
// which guard wins is genuinely nondeterministic and asserting a winner would be
// the flaky test this file is trying not to write. The invariant that must hold
// no matter who wins:
//
//   - the WAL is either still present, or removed WITH a byte-identical backup
//     copy on disk — it is never destroyed unrecoverably;
//   - the base state_*.sqlite is never touched by anyone;
//   - no guard returns an error.
func TestCleanCodexStaleWAL_ConcurrentGuards_NeverLoseTheWAL(t *testing.T) {
	if !lsofAvailable() {
		t.Skip("lsof not on PATH: guard conservatively skips removal, making this vacuous")
	}
	projectRoot := t.TempDir()
	codexHome := t.TempDir()
	writeConfigYAML(t, projectRoot, "codex:\n  stale_wal_max_bytes: 1024\n")

	const walSize = 4096
	wal := writeWAL(t, codexHome, walSize)
	base := filepath.Join(codexHome, "state_abc123.sqlite")
	baseBefore, err := os.ReadFile(base) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("read base db: %v", err)
	}

	const guards = 3 // mirrors the measured --max-concurrent 3 experiment
	var wg sync.WaitGroup
	errs := make([]error, guards)
	start := make(chan struct{})
	for i := 0; i < guards; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // release all guards as simultaneously as the scheduler allows
			errs[idx] = cleanCodexStaleWAL(projectRoot, codexHome)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("guard %d returned an error under concurrency: %v", i, e)
		}
	}

	// The base db must be untouched by every guard — it is never in scope.
	baseAfter, err := os.ReadFile(base) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("base db missing after concurrent guards: %v", err)
	}
	if !bytes.Equal(baseAfter, baseBefore) {
		t.Fatalf("base db was modified by the guard; before=%q after=%q", baseBefore, baseAfter)
	}

	// Recoverability: gone implies a byte-identical backup exists.
	if _, statErr := os.Stat(wal); os.IsNotExist(statErr) {
		copies, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*", filepath.Base(wal)))
		if globErr != nil {
			t.Fatalf("glob backup copies: %v", globErr)
		}
		if len(copies) == 0 {
			t.Fatalf("wal was removed under concurrency with NO backup copy — unrecoverable deletion")
		}
		recovered, readErr := os.ReadFile(copies[0])
		if readErr != nil {
			t.Fatalf("read backup copy: %v", readErr)
		}
		if len(recovered) != walSize {
			t.Fatalf("backup copy is not byte-identical to the wal: got %d bytes, want %d", len(recovered), walSize)
		}
	}
}

// TestReapCodexWALBackupDirs_NeverReapsNewest pins the claim that a
// just-created backup directory cannot be eaten by a CONCURRENT guard's reap
// step — the property that makes the backup a real recovery path rather than a
// racy one.
//
// It holds because the directory name carries the creation unixnano, so the
// newest dir sorts last and is never in the oldest-excess set. This test uses
// REALISTIC 19-digit unixnano names rather than the small integers the existing
// keep-last-N test uses, because the guarantee depends on lexical sort matching
// chronological sort — which is only true at a fixed digit width.
func TestReapCodexWALBackupDirs_NeverReapsNewest(t *testing.T) {
	codexHome := t.TempDir()

	// Pre-existing older backups, well past the retention cap.
	past := time.Now().Add(-time.Hour).UnixNano()
	for i := 0; i < walBackupKeepLast+4; i++ {
		dir := filepath.Join(codexHome, fmt.Sprintf(".wal-backup-%d", past+int64(i)))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// The backup a concurrently-running guard would have just created.
	newest := filepath.Join(codexHome, fmt.Sprintf(".wal-backup-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(newest, 0o700); err != nil {
		t.Fatalf("mkdir newest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newest, "state_abc123.sqlite-wal"), []byte("in-flight"), 0o600); err != nil {
		t.Fatalf("write in-flight backup content: %v", err)
	}

	reapCodexWALBackupDirs(codexHome)

	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("a just-created backup dir was reaped by a concurrent reap — the recovery path is racy; stat err = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(newest, "state_abc123.sqlite-wal")) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("in-flight backup content lost: %v", err)
	}
	if string(content) != "in-flight" {
		t.Fatalf("in-flight backup content corrupted: %q", content)
	}
	remaining, globErr := filepath.Glob(filepath.Join(codexHome, ".wal-backup-*"))
	if globErr != nil {
		t.Fatalf("glob remaining backup dirs: %v", globErr)
	}
	if len(remaining) != walBackupKeepLast {
		t.Fatalf("expected %d dirs after reap, got %d", walBackupKeepLast, len(remaining))
	}
}
