package daemon_test

// escapedetect_hkooexj_test.go — regression tests for the implementer-escaped-
// worktree detector's false-positive on pre-existing / gitignored untracked
// files in the main repo tree, and the false-negative on same-file escapes
// masked by sibling-merge path exclusion.
//
// Bug (hk-ooexj): checkMainWorkingTreeDirty flagged ANY untracked path outside
// the harmonik churn allowlist as an "escape", even when (a) the file was
// gitignored, or (b) the file already existed before the run started and the
// implementer never touched it. This failed dispatched beads (hk-c6grw twice)
// with `implementer_escaped_worktree: 1 file(s) dirty in main: HANDOFF-flywheel.md`.
//
// The fix:
//   - snapshotUntrackedFiles captures pre-existing untracked paths at run-start.
//   - checkMainWorkingTreeDirty excludes baselined paths AND gitignored paths,
//     so only NET-NEW, non-ignored files outside the worktree flag as escapes.
//
// Bug (hk-xux36): the former siblingMergeChangedPaths exclusion (hk-77q8e)
// suppressed any file touched by a sibling merge from escape candidates,
// regardless of whether the implementer also wrote that same file — a false
// negative. The fix removes siblingMergeChangedPaths entirely: the caller
// (runAgentImplementer) holds mergeMu across the escape check (hk-zguy6), so
// the update-ref/reset-hard race window that motivated the exclusion can never
// occur in production.
//
// Helper prefix: escapeFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ooexj).

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// escapeFixtureGitRepo initialises a git repo in a temp dir with one commit on
// "main" and a `.gitignore` that ignores `HANDOFF-*.md` (mirroring the real
// harmonik repo's gitignore line 60). Returns the repo root.
func escapeFixtureGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	escapeFixtureWrite(t, dir, ".gitignore", "HANDOFF-*.md\n")
	escapeFixtureWrite(t, dir, "README", "test\n")
	// Track .beads/issues.jsonl so a later modification surfaces as "M
	// .beads/issues.jsonl" (the real repo's churn shape), exercising the
	// isHarmonikChurn allowlist rather than an untracked-dir collapse.
	escapeFixtureWrite(t, dir, ".beads/issues.jsonl", "{}\n")
	run("add", ".gitignore", "README", ".beads/issues.jsonl")
	run("commit", "-m", "init")

	return dir
}

// escapeFixtureWrite writes content to dir/rel, creating parent dirs as needed.
func escapeFixtureWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("escapeFixtureWrite: MkdirAll: %v", err)
	}
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("escapeFixtureWrite: WriteFile %s: %v", rel, err)
	}
}

// TestEscapeDetect_GitignoredPreExistingNotFlagged is the primary regression
// test (hk-ooexj): a run completes with TWO pre-existing files present in the
// project root before the run started — one gitignored (HANDOFF-flywheel.md)
// and one untracked-but-not-ignored (scratch-note.txt). Neither must be flagged
// as an escape.
func TestEscapeDetect_GitignoredPreExistingNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Pre-existing files present BEFORE the run starts.
	escapeFixtureWrite(t, dir, "HANDOFF-flywheel.md", "scratch handoff\n") // gitignored
	escapeFixtureWrite(t, dir, "scratch-note.txt", "a note\n")             // untracked, not ignored

	// Snapshot the baseline at run-start (the daemon does this before launching
	// the implementer).
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// After the run, with the implementer having touched nothing in main, the
	// escape check must report clean.
	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected NO escape for pre-existing gitignored + untracked files, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_GitignoredNotFlaggedEvenWithoutBaseline verifies the
// gitignore prong on its own: even with a nil baseline (e.g. snapshot failed at
// run-start), a gitignored file present at check time is NOT flagged — git
// status omits it by default and the explicit check-ignore pass is belt-and-
// suspenders.
func TestEscapeDetect_GitignoredNotFlaggedEvenWithoutBaseline(t *testing.T) {
	dir := escapeFixtureGitRepo(t)
	escapeFixtureWrite(t, dir, "HANDOFF-flywheel.md", "scratch handoff\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, nil)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected gitignored file to be ignored, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_NetNewUntrackedStillFlagged is the positive test: a NET-NEW,
// non-ignored file created DURING the run (not in the run-start baseline) is
// still flagged as an escape. This is the real cross-contamination the detector
// must catch.
func TestEscapeDetect_NetNewUntrackedStillFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Pre-existing baseline contains scratch-note.txt only.
	escapeFixtureWrite(t, dir, "scratch-note.txt", "a note\n")
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Implementer escapes its worktree and writes a NEW file into main. Written
	// at the repo root (not a new subdir) so git porcelain reports the full
	// filename rather than collapsing a new directory to "dir/".
	escapeFixtureWrite(t, dir, "leaked.go", "package main\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if !dirty {
		t.Fatalf("expected NET-NEW file to be flagged as escape, got dirty=%v files=%v", dirty, files)
	}
	found := false
	for _, f := range files {
		if f == "leaked.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected leaked.go in escape list, got %v", files)
	}
	// The pre-existing baselined scratch-note.txt must NOT appear.
	for _, f := range files {
		if f == "scratch-note.txt" {
			t.Fatalf("pre-existing baselined file scratch-note.txt should not be flagged, got %v", files)
		}
	}
}

// TestEscapeDetect_NetNewGitignoredNotFlagged verifies that even a NET-NEW file
// the implementer creates is NOT flagged when it is gitignored — gitignored
// paths are never a real escape (they would not be committed regardless).
func TestEscapeDetect_NetNewGitignoredNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Implementer writes a NEW gitignored file during the run.
	escapeFixtureWrite(t, dir, "HANDOFF-newthread.md", "new handoff\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected NET-NEW gitignored file to be ignored, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_HarmonikChurnNotFlagged is a guard for the existing
// allowlist: expected harmonik churn must still be excluded regardless of
// baseline. Exercises a MODIFIED tracked .beads/issues.jsonl (the real repo's
// churn shape) and untracked files under .harmonik/ and .claude/.
func TestEscapeDetect_HarmonikChurnNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Modify the tracked bead ledger → "M .beads/issues.jsonl".
	escapeFixtureWrite(t, dir, ".beads/issues.jsonl", "{\"x\":1}\n")
	// Untracked daemon/orchestrator state under the churn-prefix dirs.
	escapeFixtureWrite(t, dir, ".harmonik/queue.json", "{}\n")
	escapeFixtureWrite(t, dir, ".claude/scratch.json", "{}\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, nil)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected harmonik churn to be excluded, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_AgentCommsNotFlagged is the regression for hk-77q8e case 2:
// AGENT_COMMS.md was the v0 file-outbox comms channel (retired by hk-8sm4f;
// use `harmonik comms send/recv` instead). The exemption and test are kept for
// the live-transition period — any session still using the old channel must not
// cause a false implementer_escape on in-flight beads.
func TestEscapeDetect_AgentCommsNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Baseline is empty — AGENT_COMMS.md did not exist at run-start.
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Concurrent agent creates AGENT_COMMS.md during the run.
	escapeFixtureWrite(t, dir, "AGENT_COMMS.md", "## ts · orchestrator\nhello\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected AGENT_COMMS.md to be excluded as churn, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_SiblingMergeRaceWindow documents that checkMainWorkingTreeDirty
// called directly (without mergeMu) can observe the update-ref/reset-hard race
// window and report dirty=true for sibling-changed files. In production this
// scenario is prevented: runAgentImplementer holds mergeMu across the check
// (hk-zguy6), so no sibling can be mid-flight when the check fires.
//
// The test is retained as documentation of the race-window mechanics; it does
// NOT test a code path reachable in production.
func TestEscapeDetect_SiblingMergeRaceWindow(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	// Sibling bead: commit sibling.go on a branch, advance main via update-ref
	// only (no reset-hard). The working tree is now inconsistent: HEAD points to
	// the commit that has sibling.go, but the working tree / index do not.
	run("checkout", "-b", "sibling-race")
	escapeFixtureWrite(t, dir, "sibling.go", "package daemon\n")
	run("add", "sibling.go")
	run("commit", "-m", "sibling bead")
	siblingTip := run("rev-parse", "HEAD")
	run("checkout", "main")
	run("update-ref", "refs/heads/main", siblingTip)

	// Without mergeMu, the race window is observable: sibling.go appears dirty.
	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if !dirty {
		t.Fatalf("expected race-window dirty=true without mergeMu, got dirty=%v files=%v", dirty, files)
	}
	found := false
	for _, f := range files {
		if f == "sibling.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sibling.go in dirty list (race window), got %v", files)
	}
}

// TestEscapeDetect_SiblingMergeSameFileEscapeDetected is the regression for
// hk-xux36: when a sibling bead fully merges foo.go (update-ref + reset-hard)
// and the implementer ALSO escapes into foo.go, the escape must be detected.
//
// The former siblingMergeChangedPaths exclusion (hk-77q8e) masked this: foo.go
// appeared in the sibling diff, so it was dropped from escape candidates even
// when the implementer had also written it — a false negative. Removing the
// exclusion (hk-xux36) and relying on mergeMu (hk-zguy6) to prevent
// false-positives restores correct detection.
func TestEscapeDetect_SiblingMergeSameFileEscapeDetected(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Baseline at run-start: tree is clean.
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	// Sibling bead FULLY merges foo.go: update-ref + reset-hard.
	// After this, foo.go is committed and the working tree is clean.
	run("checkout", "-b", "sibling-full-merge")
	escapeFixtureWrite(t, dir, "foo.go", "package daemon // sibling version\n")
	run("add", "foo.go")
	run("commit", "-m", "sibling merges foo.go")
	siblingTip := run("rev-parse", "HEAD")
	run("checkout", "main")
	run("update-ref", "refs/heads/main", siblingTip)
	run("reset", "--hard", "HEAD") // working tree now reflects sibling's foo.go

	// Implementer escapes: overwrites foo.go in the main working tree.
	// (In production this would mean the implementer wrote outside its worktree
	// to a file that a sibling bead had just landed.)
	escapeFixtureWrite(t, dir, "foo.go", "package daemon // implementer escape\n")

	// The escape MUST be detected. Without siblingMergeChangedPaths exclusion,
	// foo.go is correctly reported as dirty.
	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if !dirty {
		t.Fatalf("hk-xux36 false-negative: expected foo.go escape to be detected, got dirty=%v files=%v", dirty, files)
	}
	found := false
	for _, f := range files {
		if f == "foo.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected foo.go in escape list, got %v", files)
	}
}

// TestEscapeDetect_LockedPathNeverFiresOnConcurrentSiblingMerge is the hk-zguy6
// concurrent regression guard. Two goroutines share a mergeMu:
//
//   - Goroutine A (escape check): locks mu, calls checkMainWorkingTreeDirty,
//     unlocks — mirroring the hk-zguy6 production path in beadRunOne.
//
//   - Goroutine B (sibling merge): locks mu, runs update-ref → Gosched →
//     reset-hard, unlocks, then restores main HEAD under mu for the next
//     cycle — mirroring lockedMergeRunBranchToMain (hk-yyso7).
//
// With the lock held in A, the escape check always executes in a consistent
// state (before or after B's full merge, never in the transient window between
// update-ref and reset-hard). Without the lock on A, runtime.Gosched() after
// update-ref would allow A to observe that window and report a false positive;
// 100 cycles reliably catches that failure.
//
// The pre-fix race-window mechanics (without the lock) are documented by
// TestEscapeDetect_SiblingMergeRaceWindow, which passes with/without the lock
// because it calls checkMainWorkingTreeDirty directly, not through the
// production-path mutex. THIS test is the locked-path guard that cannot pass
// in the regressed (lock-removed) state.
//
// Helper prefix: escapeFixture (per implementer-protocol.md §Helper-prefix).
// Bead: hk-weabh.
func TestEscapeDetect_LockedPathNeverFiresOnConcurrentSiblingMerge(t *testing.T) {
	const cycles = 100

	dir := escapeFixtureGitRepo(t)

	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), runErr, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	// Sibling branch: commit sibling.go on a separate branch, then leave main
	// clean. This models a concurrent bead whose branch has a commit but whose
	// working-tree-refresh (reset --hard) has not yet run on the main repo.
	runGit("checkout", "-b", "sibling-concurrent")
	escapeFixtureWrite(t, dir, "sibling.go", "package daemon\n")
	runGit("add", "sibling.go")
	runGit("commit", "-m", "sibling bead (hk-weabh)")
	siblingTip := runGit("rev-parse", "HEAD")
	runGit("checkout", "main")
	originalTip := runGit("rev-parse", "HEAD")

	var mu sync.Mutex
	var falseEscapes int64 // atomic; >0 means the lock failed to exclude the transient window

	var wg sync.WaitGroup

	// Goroutine A — LOCKED escape check (hk-zguy6 production path).
	// Holds mu before calling checkMainWorkingTreeDirty, exactly as beadRunOne
	// does. With the lock, checkMainWorkingTreeDirty never runs in the transient
	// window between sibling's update-ref and reset-hard.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < cycles; i++ {
			if t.Context().Err() != nil {
				return
			}
			mu.Lock()
			dirty, files, _ := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline)
			mu.Unlock()
			if dirty {
				for _, f := range files {
					if f == "sibling.go" {
						atomic.AddInt64(&falseEscapes, 1)
					}
				}
			}
		}
	}()

	// Goroutine B — in-flight sibling merge (mirrors lockedMergeRunBranchToMain).
	// Holds mu across the full update-ref → reset-hard sequence (hk-yyso7).
	// runtime.Gosched() between the two git commands maximises scheduler pressure
	// while the transient window exists but mu is held: if A were NOT locked, it
	// would be scheduled here and observe the dirty state; with the lock, A is
	// parked waiting and never runs in the window.
	// After each forward merge, restores main to originalTip (also under mu) so
	// cycles repeat cleanly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < cycles; i++ {
			if t.Context().Err() != nil {
				return
			}
			// Forward: advance main HEAD to include sibling.go, then make the
			// working tree consistent (mirrors the EM-054 reset-hard in
			// mergeRunBranchToMain). Both git commands are under mu so the
			// transient update-ref-only state is never visible outside this section.
			mu.Lock()
			runGit("update-ref", "refs/heads/main", siblingTip)
			runtime.Gosched() // invite scheduler; A is locked out, so this is a no-op for A
			runGit("reset", "--hard", "HEAD")
			mu.Unlock()

			// Restore: move main HEAD back for the next cycle. Also under mu so
			// the restore's own transient window (update-ref without reset-hard)
			// is excluded from any concurrent escape check.
			mu.Lock()
			runGit("update-ref", "refs/heads/main", originalTip)
			runtime.Gosched()
			runGit("reset", "--hard", "HEAD")
			mu.Unlock()
		}
	}()

	wg.Wait()

	if fc := atomic.LoadInt64(&falseEscapes); fc > 0 {
		t.Fatalf("hk-zguy6 regression: locked escape check observed sibling.go as dirty %d time(s) — mergeMu is not preventing the update-ref/reset-hard race window", fc)
	}
}
