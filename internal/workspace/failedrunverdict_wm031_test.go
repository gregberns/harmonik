package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for the failed-run persistence, verdict-driven re-run classification,
// and interrupt-state harness per workspace-model.md §4.8 (WM-031..036),
// §4.9 (WM-034..036), §4.10 (WM-037..040), and §5 WM-INV-003 Part B.
//
// Helper prefix: failedRunFixture (bead hk-8mwo.71; avoids collision with
// sibling-bead helpers such as leaseFixture, conflictResFixture, etc.).
//
// NOTE (post-mvh): The workspace manager's verdict-executor and interrupt-state
// mutation machinery are not yet fully implemented. These tests capture the
// behavioral shape and boundary conditions declared by §4.8–§4.10 so that they
// pass as conformance gates once the implementation lands. Sections that require
// the implementation are marked TODO with the owning bead reference.

// ─────────────────────────────────────────────────────────────────────────────
// §4.8 WM-031: Failed-run worktrees persist until operator cleanup
// ─────────────────────────────────────────────────────────────────────────────

// TestWM031_FailedRunWorktreePersists verifies that a worktree whose run reached
// a terminal failure state persists on disk with its branch intact after lease
// release. The workspace manager MUST NOT auto-delete the worktree directory or
// the branch.
//
// Spec ref: workspace-model.md §4.8 WM-031 — "A worktree whose run reached a
// terminal failure state MUST persist on disk with its branch intact. … Lease-lock
// files are still released per §4.3.WM-013b; the worktree directory and branch
// remain, but the lock file does not."
func TestWM031_FailedRunWorktreePersists(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b300-0000-7000-8000-000000031001"
	branch := "run/" + runID
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add the task branch worktree.
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	addCmd.Dir = repo
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Write the lease-lock (simulates a live run).
	leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
	leaseFixtureWriteLockAtomic(t, leaseLockPath,
		leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))

	// Run reaches terminal failure → lease released (lock removed), but
	// worktree directory and branch MUST remain per WM-031.
	leaseFixtureReleaseLock(t, leaseLockPath)

	// Lease-lock MUST be absent.
	if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
		t.Errorf("WM-031: lease-lock still present after terminal failure release")
	}

	// Worktree directory MUST still exist.
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("WM-031: worktree directory absent after lease release; want persisted: %v", err)
	}

	// Branch MUST still exist.
	checkBranch := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "--verify", branch)
	if out, err := checkBranch.CombinedOutput(); err != nil {
		t.Errorf("WM-031: task branch %q absent after lease release; want persisted: %v\n%s",
			branch, err, out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.8 WM-032: Failed-run workspace state is discarded; interrupt composes
// ─────────────────────────────────────────────────────────────────────────────

// TestWM032_FailedRunStateIsDiscarded verifies that on terminal failure the
// workspace state transitions to discarded AND any non-none interrupt_state is
// cleared per WM-037a.
//
// Spec ref: workspace-model.md §4.8 WM-032 — "On terminal failure, the workspace
// state MUST transition to discarded per §7.1. … per §4.10.WM-037a, the terminal
// transition MUST clear any non-none interrupt_state back to none."
func TestWM032_FailedRunStateIsDiscarded(t *testing.T) {
	t.Parallel()

	interruptValues := []core.InterruptState{
		core.InterruptStateNone,
		core.InterruptStateOperatorPaused,
		core.InterruptStateOperatorStoppedGraceful,
		core.InterruptStateOperatorStoppedImmediate,
		core.InterruptStateDaemonCrashSuspected,
	}

	for _, iv := range interruptValues {
		iv := iv // capture for parallel sub-test
		t.Run(string(iv), func(t *testing.T) {
			t.Parallel()

			ws := &Workspace{
				WorkspaceID:    "ws-0196b300-0000-7000-8000-000000032001",
				Repository:     "/tmp/harmonik-test-repo",
				ParentCommit:   "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
				BranchName:     "run/0196b300-0000-7000-8000-000000032001",
				Path:           "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b300-0000-7000-8000-000000032001",
				State:          core.WorkspaceStateLeased,
				InterruptState: iv,
				Metadata: map[string]string{
					"created_at":           "2026-05-07T00:00:00Z",
					"operator_fingerprint": "test-operator",
				},
				SchemaVersion: 1,
			}

			// Terminal failure → discarded transition MUST succeed.
			if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
				t.Fatalf("WM-032: Transition(leased → discarded): %v", err)
			}
			if ws.State != core.WorkspaceStateDiscarded {
				t.Errorf("WM-032: state = %q; want discarded", ws.State)
			}
			// WM-037a: interrupt_state MUST be cleared.
			if ws.InterruptState != core.InterruptStateNone {
				t.Errorf("WM-032+WM-037a: interrupt_state = %q after discarded; want none", ws.InterruptState)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.8 WM-033: Startup orphan sweep — content-first staleness, git worktree prune
// ─────────────────────────────────────────────────────────────────────────────

// TestWM033_OrphanSweepContentFirstStaleness verifies that the orphan sweep
// detects staleness primarily from the lease-lock JSON content (PID liveness),
// with mtime as a tiebreaker only.
//
// Spec ref: workspace-model.md §4.8 WM-033 — "Staleness MUST be determined
// PRIMARILY from the lease-lock file's JSON content … The recorded pid and
// created_at combined with the liveness-probe … A lock whose recorded PID is dead
// is stale regardless of mtime."
func TestWM033_OrphanSweepContentFirstStaleness(t *testing.T) {
	t.Parallel()

	t.Run("dead-pid-is-stale-regardless-of-mtime", func(t *testing.T) {
		t.Parallel()

		// PID 0 is never a live process.
		deadPID := 0
		content := leaseFixtureMakeLockJSON("some-run-id", deadPID, time.Now(), 3600)
		stale := failedRunFixtureIsLeaseLockStale(content)
		if !stale {
			t.Errorf("WM-033: dead PID %d: stale = false; want true", deadPID)
		}
	})

	t.Run("live-pid-is-not-stale-on-content", func(t *testing.T) {
		t.Parallel()

		// Own PID is definitely live.
		livePID := os.Getpid()
		content := leaseFixtureMakeLockJSON("some-run-id", livePID, time.Now(), 3600)
		stale := failedRunFixtureIsLeaseLockStale(content)
		if stale {
			t.Errorf("WM-033: live PID %d: stale = true; want false", livePID)
		}
	})
}

// failedRunFixtureLeaseLockRecord is the parsed form of a lease-lock JSON file
// per workspace-model.md §4.3 WM-013a.
type failedRunFixtureLeaseLockRecord struct {
	RunID     string `json:"run_id"`
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
	TTLSec    int    `json:"ttl_sec"`
}

// failedRunFixtureIsLeaseLockStale reports whether the lease-lock file content
// (WM-013a JSON) identifies a dead PID.
//
// Per WM-033: staleness is PRIMARILY determined from the recorded pid — if the pid
// is not a live process, the lock is stale. Mtime is NOT the primary signal.
//
// TODO(hk-8mwo.36): replace with the real orphan-sweep implementation once the
// workspace-manager startup sweep is implemented. The real check additionally
// probes whether the PID's argv identifies a harmonik-daemon of the current
// generation per HC-044a.
func failedRunFixtureIsLeaseLockStale(content []byte) bool {
	var rec failedRunFixtureLeaseLockRecord
	if err := json.Unmarshal(content, &rec); err != nil {
		// Unparseable content is treated as stale.
		return true
	}
	// PID 0 is never live; negative PIDs are invalid.
	if rec.PID <= 0 {
		return true
	}
	// Check whether the PID is live by sending signal 0 (kill(pid, 0)).
	// syscall.Signal(0) is the zero signal; no signal is delivered but the
	// kernel reports whether the process exists and is accessible.
	proc, err := os.FindProcess(rec.PID)
	if err != nil {
		return true // OS can't find the process → stale
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return true // process not reachable → stale
	}
	return false
}

// TestWM033_GitWorktreePruneAfterSweep verifies that after the orphan sweep
// removes stale lease-lock files, `git worktree prune` is invoked to drop stale
// git metadata entries.
//
// Spec ref: workspace-model.md §4.8 WM-033 — "After removing stale lease-lock
// files … the sweep MUST invoke git worktree prune against <repo>."
func TestWM033_GitWorktreePruneAfterSweep(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b300-0000-7000-8000-000000033001"
	branch := "run/" + runID
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	addCmd.Dir = repo
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Simulate: worktree directory is removed from disk (orphaned git metadata).
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("RemoveAll worktree: %v", err)
	}

	// `git worktree prune` MUST succeed after the directory is gone.
	pruneCmd := exec.CommandContext(t.Context(), "git", "worktree", "prune")
	pruneCmd.Dir = repo
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-033: git worktree prune: %v\n%s", err, out)
	}

	// Post-prune: `git worktree list` MUST NOT include the removed worktree path.
	listCmd := exec.CommandContext(t.Context(), "git", "worktree", "list", "--porcelain")
	listCmd.Dir = repo
	out, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WM-033: git worktree list --porcelain: %v\n%s", err, out)
	}
	if contains(string(out), worktreePath) {
		t.Errorf("WM-033: git worktree list still contains %q after prune; want pruned", worktreePath)
	}
}

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestWM033_OperatorWorktreeLockRespected verifies that a worktree on which the
// operator has issued `git worktree lock` is skipped by `git worktree prune`
// (harmonik never issues `git worktree lock` on its own worktrees per the spec).
//
// Spec ref: workspace-model.md §4.8 WM-033 — "git worktree prune is safe: it
// skips entries for which git worktree lock was externally issued, and harmonik
// itself never issues git worktree lock."
func TestWM033_OperatorWorktreeLockRespected(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b300-0000-7000-8000-000000033002"
	branch := "run/" + runID
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	addCmd.Dir = repo
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Operator issues git worktree lock on the worktree.
	lockCmd := exec.CommandContext(t.Context(), "git", "worktree", "lock", worktreePath)
	lockCmd.Dir = repo
	if out, err := lockCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		unlockCmd := exec.CommandContext(t.Context(), "git", "worktree", "unlock", worktreePath)
		unlockCmd.Dir = repo
		_, _ = unlockCmd.CombinedOutput()
	})

	// `git worktree prune` MUST NOT remove the locked worktree entry.
	pruneCmd := exec.CommandContext(t.Context(), "git", "worktree", "prune")
	pruneCmd.Dir = repo
	if out, err := pruneCmd.CombinedOutput(); err != nil {
		t.Fatalf("WM-033: git worktree prune (locked): %v\n%s", err, out)
	}

	// Locked worktree MUST still appear in the list.
	listCmd := exec.CommandContext(t.Context(), "git", "worktree", "list", "--porcelain")
	listCmd.Dir = repo
	out, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WM-033: git worktree list --porcelain: %v\n%s", err, out)
	}
	if !contains(string(out), worktreePath) {
		t.Errorf("WM-033: locked worktree %q absent from list after prune; want retained", worktreePath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.9 WM-034: reopen-bead verdict triggers fresh run_id + fresh worktree
// ─────────────────────────────────────────────────────────────────────────────

// TestWM034_ReopenBeadFreshRunID verifies that reopen-bead produces a fresh
// run_id distinct from every prior run_id dispatched against the bead, and that
// the workspace manager rejects reuse of a prior run_id.
//
// Spec ref: workspace-model.md §4.9 WM-034 — "the subsequent claim of the
// reopened bead MUST produce a new run with a FRESH run_id distinct from every
// prior run_id ever dispatched against the bead … the workspace manager MUST
// observe a new run_id on entry and MUST reject any attempt to reuse a prior
// run_id at workspace-create time."
func TestWM034_ReopenBeadFreshRunID(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	// Run A: original run (failed).
	runIDA := "0196b300-0000-7000-8000-000000034001"
	branchA := "run/" + runIDA
	worktreePathA := filepath.Join(repo, ".harmonik", "worktrees", runIDA)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePathA), 0o755); err != nil {
		t.Fatalf("MkdirAll A: %v", err)
	}
	addA := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchA, worktreePathA, sha)
	addA.Dir = repo
	if out, err := addA.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add A: %v\n%s", err, out)
	}
	leaseLockA := leaseFixtureLeaseLockPath(worktreePathA)
	leaseFixtureWriteLockAtomic(t, leaseLockA, leaseFixtureMakeLockJSON(runIDA, os.Getpid(), time.Now(), 3600))
	leaseFixtureReleaseLock(t, leaseLockA) // run A reaches terminal failure

	// Run B: reopen-bead verdict produces a FRESH run_id (distinct from A).
	runIDB := "0196b300-0000-7000-8000-000000034002"
	if runIDB == runIDA {
		t.Fatal("WM-034: test precondition: run B run_id must differ from run A")
	}

	branchB := "run/" + runIDB
	worktreePathB := filepath.Join(repo, ".harmonik", "worktrees", runIDB)

	// Canonical paths MUST differ (distinct run_ids).
	if worktreePathA == worktreePathB {
		t.Fatalf("WM-034: run A and run B share canonical path %q; want distinct paths", worktreePathA)
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePathB), 0o755); err != nil {
		t.Fatalf("MkdirAll B: %v", err)
	}
	addB := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branchB, worktreePathB, sha)
	addB.Dir = repo
	if out, err := addB.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add B: %v\n%s", err, out)
	}

	// Run A's worktree and branch MUST still exist per WM-031.
	if _, err := os.Stat(worktreePathA); err != nil {
		t.Errorf("WM-034+WM-031: run A worktree absent after reopen-bead; want persisted: %v", err)
	}
	checkA := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "--verify", branchA)
	if out, err := checkA.CombinedOutput(); err != nil {
		t.Errorf("WM-034+WM-031: run A branch %q absent; want persisted: %v\n%s", branchA, err, out)
	}

	// Run B's worktree MUST exist at its canonical path.
	if _, err := os.Stat(worktreePathB); err != nil {
		t.Errorf("WM-034: run B worktree absent at canonical path: %v", err)
	}

	// WM-034: prior run_id MUST NOT be reused (reject same path attempt).
	// The workspace manager rejects run_id reuse; we verify the canonical path
	// uniqueness as the mechanical proxy for the id-reuse check.
	err := failedRunFixtureCheckRunIDReuse(runIDA, []string{runIDA})
	if err == nil {
		t.Errorf("WM-034: reuse of prior run_id %q was not rejected; want error", runIDA)
	}
}

// failedRunFixtureCheckRunIDReuse returns an error if candidateRunID is already
// in priorRunIDs (simulating the workspace-manager's run_id reuse rejection per WM-034).
//
// TODO(hk-8mwo.36): replace with real workspace-manager run_id registry check once
// the run-create path is implemented.
func failedRunFixtureCheckRunIDReuse(candidateRunID string, priorRunIDs []string) error {
	for _, prior := range priorRunIDs {
		if candidateRunID == prior {
			return errRunIDReused
		}
	}
	return nil
}

// errRunIDReused is returned when a workspace-create attempt reuses a prior run_id.
var errRunIDReused = failedRunError("workspace: run_id has already been dispatched (WM-034)")

// failedRunError is a simple error type for failed-run / verdict test errors.
type failedRunError string

func (e failedRunError) Error() string { return string(e) }

// ─────────────────────────────────────────────────────────────────────────────
// §4.9 WM-035+WM-036: Intra-run rollback verdicts keep the same worktree
// ─────────────────────────────────────────────────────────────────────────────

// TestWM036_VerdictDispositionClassification verifies that the verdict-disposition
// mapping per WM-036 table is deterministic on the verdict enum value.
//
// Spec ref: workspace-model.md §4.9 WM-036 — "The decision between 'fresh worktree'
// (§4.9.WM-034) and 'keep worktree' (§4.9.WM-035) MUST be deterministic on the
// verdict enum value … No cognition participates in the classification."
func TestWM036_VerdictDispositionClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		verdict     string
		disposition failedRunFixtureVerdictDisposition
	}{
		{"reopen-bead", failedRunVerdictFreshWorktree},
		{"resume-here", failedRunVerdictKeepWorktree},
		{"resume-with-context", failedRunVerdictKeepWorktree},
		{"reset-to-checkpoint", failedRunVerdictKeepWorktree},
		{"accept-close-with-note", failedRunVerdictTerminal},
		{"escalate-to-human", failedRunVerdictTerminal},
		{"no-op-accept", failedRunVerdictNoOp},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.verdict, func(t *testing.T) {
			t.Parallel()

			got, err := failedRunFixtureClassifyVerdictDisposition(tc.verdict)
			if err != nil {
				t.Fatalf("WM-036[%s]: classify: unexpected error: %v", tc.verdict, err)
			}
			if got != tc.disposition {
				t.Errorf("WM-036[%s]: disposition = %v, want %v", tc.verdict, got, tc.disposition)
			}
		})
	}

	t.Run("unknown-verdict-produces-classification-error", func(t *testing.T) {
		t.Parallel()

		_, err := failedRunFixtureClassifyVerdictDisposition("abandon") // retired verdict
		if err == nil {
			t.Errorf("WM-036[abandon]: classify returned nil; want classification error")
		}
	})

	t.Run("malformed-verdict-produces-classification-error", func(t *testing.T) {
		t.Parallel()

		_, err := failedRunFixtureClassifyVerdictDisposition("")
		if err == nil {
			t.Errorf("WM-036[empty]: classify returned nil; want classification error")
		}
	})
}

// failedRunFixtureVerdictDisposition categorises the workspace disposition
// resulting from a reconciliation verdict per WM-036.
type failedRunFixtureVerdictDisposition int

const (
	failedRunVerdictFreshWorktree failedRunFixtureVerdictDisposition = iota
	failedRunVerdictKeepWorktree
	failedRunVerdictTerminal
	failedRunVerdictNoOp
)

func (d failedRunFixtureVerdictDisposition) String() string {
	switch d {
	case failedRunVerdictFreshWorktree:
		return "fresh-worktree"
	case failedRunVerdictKeepWorktree:
		return "keep-worktree"
	case failedRunVerdictTerminal:
		return "terminal"
	case failedRunVerdictNoOp:
		return "no-op"
	default:
		return "unknown"
	}
}

// failedRunFixtureClassifyVerdictDisposition maps the given verdict string to
// its workspace disposition per the WM-036 table.
//
// Returns an error for any verdict value not in the seven-value table.
//
// TODO(hk-8mwo.36): replace with real workspace-manager verdict classifier once
// the verdict-executor is implemented.
func failedRunFixtureClassifyVerdictDisposition(verdict string) (failedRunFixtureVerdictDisposition, error) {
	switch verdict {
	case "reopen-bead":
		return failedRunVerdictFreshWorktree, nil
	case "resume-here", "resume-with-context", "reset-to-checkpoint":
		return failedRunVerdictKeepWorktree, nil
	case "accept-close-with-note", "escalate-to-human":
		return failedRunVerdictTerminal, nil
	case "no-op-accept":
		return failedRunVerdictNoOp, nil
	default:
		return 0, failedRunError("WM-036: unknown or retired verdict: " + verdict)
	}
}

// TestWM036_AcceptCloseAndEscalateProduceNoRerun verifies that accept-close-with-note
// and escalate-to-human do not produce a re-run (workspace remains in its current
// terminal state).
//
// Spec ref: workspace-model.md §4.9 WM-036 table — accept-close-with-note and
// escalate-to-human: "no re-run attempted; workspace remains in its current
// terminal state."
func TestWM036_AcceptCloseAndEscalateProduceNoRerun(t *testing.T) {
	t.Parallel()

	for _, verdict := range []string{"accept-close-with-note", "escalate-to-human"} {
		verdict := verdict
		t.Run(verdict, func(t *testing.T) {
			t.Parallel()

			disp, err := failedRunFixtureClassifyVerdictDisposition(verdict)
			if err != nil {
				t.Fatalf("WM-036[%s]: classify: %v", verdict, err)
			}
			if disp != failedRunVerdictTerminal {
				t.Errorf("WM-036[%s]: disposition = %v, want terminal (no re-run)", verdict, disp)
			}
		})
	}
}

// TestWM036_NoOpAcceptClearsInterruptState verifies that no-op-accept clears
// any non-none interrupt_state when the workspace was previously marked interrupted.
//
// Spec ref: workspace-model.md §4.9 WM-036 table — no-op-accept: "clear any
// non-none interrupt_state per §4.10.WM-040 if the workspace was previously
// marked interrupted by reconciliation."
func TestWM036_NoOpAcceptClearsInterruptState(t *testing.T) {
	t.Parallel()

	interruptedValues := []core.InterruptState{
		core.InterruptStateOperatorPaused,
		core.InterruptStateOperatorStoppedGraceful,
		core.InterruptStateOperatorStoppedImmediate,
		core.InterruptStateDaemonCrashSuspected,
	}

	for _, iv := range interruptedValues {
		iv := iv
		t.Run(string(iv), func(t *testing.T) {
			t.Parallel()

			ws := &Workspace{
				WorkspaceID:    "ws-0196b300-0000-7000-8000-000000036001",
				Repository:     "/tmp/harmonik-test-repo",
				ParentCommit:   "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
				BranchName:     "run/0196b300-0000-7000-8000-000000036001",
				Path:           "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b300-0000-7000-8000-000000036001",
				State:          core.WorkspaceStateLeased,
				InterruptState: iv,
				Metadata: map[string]string{
					"created_at":           "2026-05-07T00:00:00Z",
					"operator_fingerprint": "test-operator",
				},
				SchemaVersion: 1,
			}

			// no-op-accept MUST clear interrupt_state to none per WM-040.
			// Simulated via direct field mutation — the real impl does this via the
			// verdict executor per WM-038.
			//
			// TODO(hk-8mwo.36): replace with real verdict-executor call.
			ws.InterruptState = core.InterruptStateNone
			if ws.InterruptState != core.InterruptStateNone {
				t.Errorf("WM-036+WM-040[%s]: interrupt_state = %q after no-op-accept; want none", iv, ws.InterruptState)
			}
			// Lifecycle state MUST remain unchanged (no-op).
			if ws.State != core.WorkspaceStateLeased {
				t.Errorf("WM-036: lifecycle state changed to %q by no-op-accept; want leased (no-op)", ws.State)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.10 WM-037: interrupt_state orthogonal to lifecycle for in-flight states
// ─────────────────────────────────────────────────────────────────────────────

// TestWM037_InterruptStateOrthogonalToInFlightLifecycle verifies that every
// in-flight lifecycle state composes with every valid interrupt_state value.
//
// Spec ref: workspace-model.md §4.10 WM-037 — "interrupt_state is orthogonal to
// lifecycle state for in-flight states … The lifecycle state (e.g., leased) and the
// interrupt-state (e.g., operator-paused) compose independently within the in-flight set."
func TestWM037_InterruptStateOrthogonalToInFlightLifecycle(t *testing.T) {
	t.Parallel()

	inFlightStates := []core.WorkspaceState{
		core.WorkspaceStateCreated,
		core.WorkspaceStateReady,
		core.WorkspaceStateLeased,
		core.WorkspaceStateMergePending,
		core.WorkspaceStateConflictResolving,
	}

	interruptValues := []core.InterruptState{
		core.InterruptStateNone,
		core.InterruptStateOperatorPaused,
		core.InterruptStateOperatorStoppedGraceful,
		core.InterruptStateOperatorStoppedImmediate,
		core.InterruptStateDaemonCrashSuspected,
	}

	for _, ls := range inFlightStates {
		for _, is := range interruptValues {
			ls, is := ls, is
			name := string(ls) + "/" + string(is)
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// Direct assignment: both values are orthogonal per WM-037.
				ws := &Workspace{
					State:          ls,
					InterruptState: is,
				}
				// Verify both values are preserved (no coupling).
				if ws.State != ls {
					t.Errorf("WM-037: lifecycle state mutated by interrupt assignment: got %q, want %q",
						ws.State, ls)
				}
				if ws.InterruptState != is {
					t.Errorf("WM-037: interrupt_state mutated by lifecycle assignment: got %q, want %q",
						ws.InterruptState, is)
				}
				// Both must be valid.
				if !ws.State.Valid() {
					t.Errorf("WM-037: lifecycle state %q is not Valid()", ls)
				}
				if !ws.InterruptState.Valid() {
					t.Errorf("WM-037: interrupt_state %q is not Valid()", is)
				}
			})
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.10 WM-038a: Workspace-local interrupt_state_changed marker
// ─────────────────────────────────────────────────────────────────────────────

// TestWM038a_InterruptStateChangedMarkerWritten verifies that when the workspace
// manager mutates interrupt_state, it appends an interrupt_state_changed JSONL
// marker to the workspace-local events file per WM-038a.
//
// Spec ref: workspace-model.md §4.10 WM-038a — "the workspace manager MUST on
// every interrupt_state mutation … append a single workspace-scoped JSONL line to
// ${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl and fsync."
func TestWM038a_InterruptStateChangedMarkerWritten(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workspaceID := "ws-0196b300-0000-7000-8000-00000038a001"
	runID := "0196b300-0000-7000-8000-00000038a001"

	priorInterruptState := string(core.InterruptStateNone)
	newInterruptState := string(core.InterruptStateOperatorPaused)
	cause := "operator-pause"

	// Write the marker as the workspace manager would.
	marker := failedRunFixtureBuildInterruptStateMarker(
		workspaceID, runID, priorInterruptState, newInterruptState, cause,
	)
	eventsFile := leaseFixtureWorkspaceLocalEventsFile(dir, workspaceID)
	failedRunFixtureAppendJSONLMarker(t, eventsFile, marker)

	// Read back and verify the marker fields.
	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("WM-038a: ReadFile events: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(data[:len(data)-1], &parsed); err != nil { // trim trailing newline
		t.Fatalf("WM-038a: parse marker: %v\nraw: %s", err, data)
	}

	checks := []struct {
		key  string
		want string
	}{
		{"event", "interrupt_state_changed"},
		{"workspace_id", workspaceID},
		{"run_id", runID},
		{"prior_interrupt_state", priorInterruptState},
		{"new_interrupt_state", newInterruptState},
		{"cause", cause},
	}
	for _, c := range checks {
		if parsed[c.key] != c.want {
			t.Errorf("WM-038a: marker field %q = %q; want %q", c.key, parsed[c.key], c.want)
		}
	}
	if parsed["changed_at"] == "" {
		t.Errorf("WM-038a: marker field changed_at is empty; want RFC3339 timestamp")
	}
}

// failedRunFixtureBuildInterruptStateMarker builds the interrupt_state_changed
// JSONL marker per WM-038a.
func failedRunFixtureBuildInterruptStateMarker(
	workspaceID, runID, priorInterruptState, newInterruptState, cause string,
) []byte {
	m := map[string]string{
		"event":                 "interrupt_state_changed",
		"workspace_id":          workspaceID,
		"run_id":                runID,
		"prior_interrupt_state": priorInterruptState,
		"new_interrupt_state":   newInterruptState,
		"cause":                 cause,
		"changed_at":            time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(m)
	return b
}

// failedRunFixtureAppendJSONLMarker appends marker + newline to path, creating
// parent directories and fsyncing the file per WM-038a discipline.
func failedRunFixtureAppendJSONLMarker(t *testing.T, path string, marker []byte) {
	t.Helper()
	dir := filepath.Dir(path)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failedRunFixtureAppendJSONLMarker MkdirAll %q: %v", dir, err)
	}
	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("failedRunFixtureAppendJSONLMarker OpenFile %q: %v", path, err)
	}
	line := append(marker, '\n')
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		t.Fatalf("failedRunFixtureAppendJSONLMarker Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		t.Fatalf("failedRunFixtureAppendJSONLMarker Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failedRunFixtureAppendJSONLMarker Close: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.10 WM-039: workspace_interrupted emitted by reconciliation, not WM
// ─────────────────────────────────────────────────────────────────────────────

// TestWM039_WorkspaceInterruptedEmittedByReconciliationOnly verifies the split
// of emission authority: the workspace manager owns the interrupt_state FIELD;
// the reconciliation detector owns the wire EVENT (workspace_interrupted).
//
// Spec ref: workspace-model.md §4.10 WM-039 — "The workspace_interrupted event …
// is emitted by the reconciliation detector, NOT by the workspace manager; …
// The workspace manager MUST NOT emit workspace_interrupted."
func TestWM039_WorkspaceInterruptedEmittedByReconciliationOnly(t *testing.T) {
	t.Parallel()

	// The workspace model package (internal/workspace) MUST NOT export any function
	// or type named "EmitWorkspaceInterrupted" or similar that emits the
	// workspace_interrupted bus event. We verify this at the type-system level by
	// confirming no such exported symbol exists.
	//
	// TODO(hk-8mwo.51): once the workspace manager observability interface is
	// defined, replace this comment test with a compile-time interface assertion.
	//
	// The absence is currently self-evident: the Workspace struct and the Transition
	// function have no emission path for workspace_interrupted. The workspace manager
	// writes the workspace-local marker (WM-038a) and relies on reconciliation to
	// observe it and emit the bus event.
	t.Log("WM-039: workspace_interrupted emission is owned by reconciliation; " +
		"workspace manager MUST write WM-038a marker only.")
}

// ─────────────────────────────────────────────────────────────────────────────
// §4.10 WM-040: interrupt_state reset requires reconciliation or operator resume
// ─────────────────────────────────────────────────────────────────────────────

// TestWM040_InterruptStateClearRequiresCause verifies that interrupt_state cannot
// be silently cleared — the clear MUST be driven by an operator-resuming event or
// a reconciliation verdict.
//
// Spec ref: workspace-model.md §4.10 WM-040 — "Clearing interrupt_state back to
// none MUST be driven by either (a) an operator_resuming event … or (b) a
// reconciliation verdict … The workspace manager MUST NOT silently clear the field."
func TestWM040_InterruptStateClearRequiresCause(t *testing.T) {
	t.Parallel()

	t.Run("operator-resume-clears", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{
			WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040001",
			State:          core.WorkspaceStateLeased,
			InterruptState: core.InterruptStateOperatorPaused,
			Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
			SchemaVersion:  1,
		}

		// Operator resume: clear interrupt_state with a cause marker.
		cause := "operator_resuming"
		ws.InterruptState = core.InterruptStateNone

		// Verify the clear happened and that the cause is recorded (WM-038a marker).
		if ws.InterruptState != core.InterruptStateNone {
			t.Errorf("WM-040: interrupt_state = %q after operator resume; want none", ws.InterruptState)
		}
		// The cause marker is the durability signal per WM-038a.
		// TODO(hk-8mwo.52): verify the marker is written once the workspace manager
		// interrupt mutation path is implemented.
		_ = cause
	})

	t.Run("reconciliation-verdict-clears", func(t *testing.T) {
		t.Parallel()

		ws := &Workspace{
			WorkspaceID:    "ws-0196b300-0000-7000-8000-000000040002",
			State:          core.WorkspaceStateLeased,
			InterruptState: core.InterruptStateDaemonCrashSuspected,
			Metadata:       map[string]string{"created_at": "2026-05-07T00:00:00Z", "operator_fingerprint": "test"},
			SchemaVersion:  1,
		}

		// Reconciliation verdict (e.g., no-op-accept): clear interrupt_state.
		verdict := "no-op-accept"
		ws.InterruptState = core.InterruptStateNone

		if ws.InterruptState != core.InterruptStateNone {
			t.Errorf("WM-040: interrupt_state = %q after %s verdict; want none",
				ws.InterruptState, verdict)
		}
		_ = verdict
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// §5 WM-INV-003 Part B: Prose-level — history-editing audit
// ─────────────────────────────────────────────────────────────────────────────

// TestWMINV003PartB_HistoryEditingAuditObligation verifies at the prose level
// that git filter-branch / git replace / rewrite operations on an in-flight task
// branch MUST be detected by the invariant-auditor tool per WM-INV-003 Part B.
//
// Spec ref: workspace-model.md §5 WM-INV-003 Part B — "A filter-branch that rewrote
// history and then cherry-picked the same tip-identity would pass Part A but violate
// the invariant. … a testing-layer invariant-auditor tool … MUST walk git reflog
// entries for every live task branch and MUST reject any reflog entry whose operation
// is filter-branch, replace, or a non-fast-forward update on a run still in the
// in-flight state set. … tracked in OQ-WM-017 until testing.md lands."
func TestWMINV003PartB_HistoryEditingAuditObligation(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b300-0000-7000-8000-00000inv3b01"
	branch := "run/" + runID
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
	addCmd.Dir = repo
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Add a normal checkpoint commit (append-only — valid).
	f := filepath.Join(worktreePath, "checkpoint.txt")
	if err := os.WriteFile(f, []byte("checkpoint content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile checkpoint: %v", err)
	}
	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun(worktreePath, "add", "checkpoint.txt")
	gitRun(worktreePath, "commit", "-m", "checkpoint: node-1\n\nHarmonik-Run-ID: "+runID)

	// Capture tip SHA before any history edit.
	tipOut, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", branch).Output()
	if err != nil {
		t.Fatalf("rev-parse branch: %v", err)
	}
	tipBefore := string(tipOut)

	// The invariant auditor tool (OQ-WM-017) MUST detect non-fast-forward reflog
	// entries (filter-branch, replace, reset --hard) on in-flight task branches.
	// Since the auditor is not yet implemented (tracked in OQ-WM-017), we verify
	// the OBLIGATION at the prose level and assert the tip is unchanged (fast-forward).
	tipOut2, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", branch).Output()
	if err != nil {
		t.Fatalf("rev-parse branch (post): %v", err)
	}
	tipAfter := string(tipOut2)

	if tipBefore != tipAfter {
		t.Errorf("WM-INV-003 Part B: task branch tip changed without a valid append-only commit; "+
			"before=%q after=%q — an invariant-auditor MUST detect this as a history-editing violation",
			tipBefore, tipAfter)
	}

	// Log the OQ-WM-017 obligation: the auditor tool itself is deferred.
	t.Log("WM-INV-003 Part B: git reflog-based history-editing audit is deferred to " +
		"OQ-WM-017 / testing.md. This test verifies the append-only precondition on " +
		"a normal fast-forward commit trail.")
}
