package daemon_test

// worktreerefreshscope_hk7qmpp_test.go — the EM-054 refresh must be SCOPED to
// the merged commit's own paths (bead hk-7qmpp).
//
// THE INCIDENT THIS PINS. On 2026-07-22 the daemon destroyed uncommitted fleet
// state in the main project root — twice, 3m47s apart, once per completed
// merge. The refresh ran `git restore --staged .` + `git reset --hard HEAD`
// over the WHOLE tree, unconditionally, after every merge-to-main.
//
// What made it invisible rather than merely destructive: the pre-merge escape
// check (checkMainWorkingTreeDirty) FAILS a run when main is dirty, so a stray
// edit normally stops the run loudly. But its churn allowlist deliberately
// exempts `.harmonik/` and `.claude/` — where agent and fleet state live. The
// one region waved through as expected churn was the one region the refresh
// then deleted. Two features interacting exactly wrong, not a blanket reset.
//
// Test assertions:
//   (i)   an uncommitted edit on a path the merge does NOT touch SURVIVES;
//   (ii)  EM-054's own obligation still holds — the merged commit's paths are
//         clean against HEAD after the merge;
//   (iii) when the refresh DOES overwrite a local edit (the merged commit owns
//         that path), it is never silent: a
//         working_tree_local_edits_overwritten event names the paths and a
//         recovery patch is written.
//
// Assertion (i) is the mutation oracle: it FAILS against the pre-hk-7qmpp
// `git reset --hard HEAD` and passes against the scoped refresh.
//
// Helper prefix: wtScopeFixture (per implementer-protocol.md §Helper-prefix
// discipline).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-054 (amended by hk-7qmpp)
//   - specs/run-state-machine.md RSM-016 (why update-ref STAYS in Phase A)
//
// Bead: hk-7qmpp.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// wtScopeFixtureReadmeFactory wraps productionWorktreeFactory and commits a
// change to README — a file that ALREADY EXISTS in main — onto the run-branch.
// Needed for the overwrite path: the merge must own a path the main root can
// also have locally edited.
func wtScopeFixtureReadmeFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(filepath.Join(wtPath, "README"), []byte("merged content\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("wtScopeFixtureReadmeFactory: WriteFile: %w", err2)
		}
		//nolint:gosec // G204: fixed argv; runID is a test-generated identifier, not external input
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-am", "feat: agent edits README",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("wtScopeFixtureReadmeFactory: git commit: %w\n%s", err2, out)
		}
		return wtPath, cleanup, nil
	}
}

// wtScopeFixtureRunMerge drives one full work-loop pass over the given
// worktree factory and returns the event collector.
func wtScopeFixtureRunMerge(t *testing.T, projectDir string, beadID core.BeadID, factory func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error)) *stubEventCollector {
	t.Helper()

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  factory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck // loop exits on ctx cancel; the test asserts on emitted events, not this return
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Fatal("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (merge must still succeed)", got)
	}
	return collector
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion (i) + (ii): the incident, pinned
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkingTreeRefreshScope_PreservesUncommittedWorkOutsideMergedPaths is the
// regression test for the 2026-07-22 data loss. An uncommitted edit sits in the
// main root on README — a path the merged commit never touches — while a merge
// completes. The edit MUST still be there afterwards.
//
// Against the pre-hk-7qmpp `git reset --hard HEAD`, README is silently reverted
// and this test fails. That is the point.
//
// Bead: hk-7qmpp.
func TestWorkingTreeRefreshScope_PreservesUncommittedWorkOutsideMergedPaths(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("wtscope-hk7qmpp-bead-001")
	const precious = "initial\nUNCOMMITTED FLEET STATE — MUST SURVIVE THE MERGE\n"

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)
	wtRefreshFixtureOriginDir(t, projectDir)

	// The uncommitted edit an agent (or the operator) has in the main root.
	// README is tracked and the merged commit does NOT touch it — the merge
	// adds work.txt.
	readme := filepath.Join(projectDir, "README")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(readme, []byte(precious), 0o644); err != nil {
		t.Fatalf("seed uncommitted edit: %v", err)
	}

	collector := wtScopeFixtureRunMerge(t, projectDir, beadID, mergeToMainCommittingFactory(t))

	// ── Assertion (i): the uncommitted edit survived. ────────────────────────
	got, err := os.ReadFile(readme) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("read README after merge: %v", err)
	}
	if string(got) != precious {
		t.Errorf("README after merge = %q; want %q — the post-merge refresh destroyed uncommitted work outside the merged commit's paths (hk-7qmpp)", got, precious)
	}

	// ── Assertion (ii): EM-054 still holds for the merged commit's own paths. ─
	if status := wtRefreshFixtureGitStatus(t, projectDir, "work.txt"); status != "" {
		t.Errorf("git status --porcelain work.txt = %q; want empty (EM-054: the merged commit's files must match HEAD)", status)
	}

	// Nothing was overwritten, so no overwrite event may fire.
	if ev := mergeToMainFindEvents(collector, "working_tree_local_edits_overwritten"); len(ev) > 0 {
		t.Errorf("working_tree_local_edits_overwritten emitted with nothing overwritten; want absent: %v", ev)
	}
	if ev := mergeToMainFindEvents(collector, "working_tree_refresh_failed"); len(ev) > 0 {
		t.Errorf("working_tree_refresh_failed emitted on the success path; want absent: %v", ev)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion (iii): an overwrite is allowed, but never silent
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkingTreeRefreshScope_NamesOverwrittenLocalEdits verifies the second
// acceptance criterion: when the refresh DOES overwrite an uncommitted local
// edit — legitimate, because the merged commit is authoritative for its own
// paths — it emits working_tree_local_edits_overwritten naming the path, and
// parks the lost content in a recovery patch.
//
// Silence is the defect being fixed here, not the overwrite.
//
// Bead: hk-7qmpp.
func TestWorkingTreeRefreshScope_NamesOverwrittenLocalEdits(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("wtscope-hk7qmpp-bead-002")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)
	wtRefreshFixtureOriginDir(t, projectDir)

	// Local edit on README — the SAME path the merged commit rewrites.
	readme := filepath.Join(projectDir, "README")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(readme, []byte("initial\nlocal edit about to be overwritten\n"), 0o644); err != nil {
		t.Fatalf("seed local edit: %v", err)
	}

	collector := wtScopeFixtureRunMerge(t, projectDir, beadID, wtScopeFixtureReadmeFactory(t))

	// The merged content wins — that half is intended.
	got, err := os.ReadFile(readme) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("read README after merge: %v", err)
	}
	if string(got) != "merged content\n" {
		t.Errorf("README after merge = %q; want the merged content (the merge owns its own paths)", got)
	}

	// ── The part that matters: it was NAMED. ─────────────────────────────────
	events := mergeToMainFindEvents(collector, "working_tree_local_edits_overwritten")
	if len(events) != 1 {
		t.Fatalf("working_tree_local_edits_overwritten emitted %d time(s); want exactly 1 — an overwrite must never be silent (hk-7qmpp)", len(events))
	}

	var pl core.WorkingTreeLocalEditsOverwrittenPayload
	if err := json.Unmarshal(events[0].Payload, &pl); err != nil {
		t.Fatalf("unmarshal working_tree_local_edits_overwritten payload: %v", err)
	}
	if !pl.Valid() {
		t.Errorf("payload.Valid() = false; payload = %+v", pl)
	}
	if len(pl.Paths) != 1 || pl.Paths[0] != "README" {
		t.Errorf("payload.Paths = %v; want [README] — the event must name what it overwrote", pl.Paths)
	}
	if pl.BeadID != string(beadID) {
		t.Errorf("payload.BeadID = %q; want %q", pl.BeadID, beadID)
	}

	// ── And the lost work was parked, not just announced. ────────────────────
	if pl.RecoveryPatch == "" {
		t.Fatal("payload.RecoveryPatch is empty; want a written patch — park it before you delete it")
	}
	patch, err := os.ReadFile(pl.RecoveryPatch)
	if err != nil {
		t.Fatalf("read recovery patch %q: %v", pl.RecoveryPatch, err)
	}
	if len(patch) == 0 {
		t.Errorf("recovery patch %q is empty; want the overwritten diff", pl.RecoveryPatch)
	}
}
