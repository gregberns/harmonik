package daemon_test

// mergetomain_hkpphof_test.go — integration tests for .beads/issues.jsonl
// conflict handling during daemon-driven rebase (BL-MRG-005 / hk-t48rg).
//
// Test assertions:
//   (p)  When ONLY .beads/issues.jsonl conflicts during rebase AND the
//        beads-union git merge driver is registered, the driver resolves
//        the conflict and the bead closes (not reopens).
//   (q)  When a real code file also conflicts, the daemon escalates
//        (ReopenBead, outcome_emitted{rejected, rebase_conflict}).
//
// Helper prefix: mergeToMainFixture (shared with sibling mergetomain_* tests;
// per implementer-protocol.md §Helper-prefix discipline — bead hk-pphof).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052 step 2, EM-053
//   - plans/2026-06-22-beads-integration-conformance-audit.md §BL-MRG-005
//
// Beads: hk-pphof, hk-t48rg.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture helpers for .beads/issues.jsonl conflict scenarios
// ─────────────────────────────────────────────────────────────────────────────

// mergeToMainFixtureRegisterBeadsUnionDriver writes a mock beads-union merge
// driver script to repoRoot and registers it in the repo's git config.
// The driver performs a simple union merge (unique lines from both sides),
// mirroring what the real "harmonik beads-merge" driver does for JSONL ledgers.
// It also writes and commits a .gitattributes file that routes .beads/issues.jsonl
// through merge=beads-union, matching the production .gitattributes convention.
func mergeToMainFixtureRegisterBeadsUnionDriver(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureRegisterBeadsUnionDriver: git %v: %v\n%s", args, err, out)
		}
	}

	// Write a mock union-merge driver. Git calls it as:
	//   driver %O %A %B %P  ($1=ancestor, $2=ours/output, $3=theirs, $4=path)
	// Exit 0 = conflict resolved; result written to $2.
	scriptPath := filepath.Join(repoRoot, ".harmonik", "mock-beads-merge.sh")
	scriptContent := "#!/bin/sh\nset -e\nsort -u \"$2\" \"$3\" > \"$2.tmp\" && mv \"$2.tmp\" \"$2\"\nexit 0\n"
	//nolint:gosec // G306: 0755 required for executable script
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureRegisterBeadsUnionDriver: WriteFile driver: %v", err)
	}

	// Register the driver in the repo's local git config.
	run("config", "merge.beads-union.name", "Bead Ledger Union Merge (mock)")
	run("config", "merge.beads-union.driver", scriptPath+" %O %A %B %P")

	// Write and commit .gitattributes so git uses the driver for the ledger file.
	attrPath := filepath.Join(repoRoot, ".gitattributes")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(attrPath, []byte(".beads/issues.jsonl merge=beads-union\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureRegisterBeadsUnionDriver: WriteFile .gitattributes: %v", err)
	}
	run("add", ".gitattributes")
	run("commit", "-m", "test: register beads-union merge driver via .gitattributes")
}

// mergeToMainFixtureInitBeadsLedger writes a minimal .beads/issues.jsonl into
// the repo root and commits it on main so that subsequent modifications produce
// a real merge conflict.
func mergeToMainFixtureInitBeadsLedger(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureInitBeadsLedger: git %v: %v\n%s", args, err, out)
		}
	}

	beadsDir := filepath.Join(repoRoot, ".beads")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mergeToMainFixtureInitBeadsLedger: MkdirAll: %v", err)
	}
	ledgerPath := filepath.Join(beadsDir, "issues.jsonl")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(ledgerPath, []byte(`{"id":"bead-init","status":"open"}`+"\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureInitBeadsLedger: WriteFile: %v", err)
	}
	run("add", ".beads/issues.jsonl")
	run("commit", "-m", "init beads ledger")
}

// mergeToMainFixtureAdvanceMainBeadsLedger advances main with a diverging commit
// that modifies .beads/issues.jsonl only — producing a conflict with any
// run-branch that also modifies the ledger.
func mergeToMainFixtureAdvanceMainBeadsLedger(t *testing.T, repoRoot string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeToMainFixtureAdvanceMainBeadsLedger: git %v: %v\n%s", args, err, out)
		}
	}
	ledgerPath := filepath.Join(repoRoot, ".beads", "issues.jsonl")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(ledgerPath, []byte(`{"id":"bead-init","status":"open"}`+"\n"+
		`{"id":"main-bead","status":"closed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("mergeToMainFixtureAdvanceMainBeadsLedger: WriteFile: %v", err)
	}
	run("add", ".beads/issues.jsonl")
	run("commit", "-m", "main: close main-bead in ledger")
}

// mergeToMainBeadsCommittingFactory wraps productionWorktreeFactory and commits
// both work.txt AND a modified .beads/issues.jsonl onto the run-branch. This
// simulates an agent that wrote bead state — the run-branch and main both
// diverge in .beads/issues.jsonl.
func mergeToMainBeadsCommittingFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		// Write agent work (different file — no conflict here).
		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(workFile, []byte("agent work\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"mergeToMainBeadsCommittingFactory: WriteFile work.txt: " + err2.Error()}
		}

		// Also modify .beads/issues.jsonl in the worktree — diverges from main.
		beadsDir := filepath.Join(wtPath, ".beads")
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err2 := os.MkdirAll(beadsDir, 0o755); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"mergeToMainBeadsCommittingFactory: MkdirAll: " + err2.Error()}
		}
		ledgerPath := filepath.Join(beadsDir, "issues.jsonl")
		//nolint:gosec // G306: 0644 is fine for a test fixture file
		if err2 := os.WriteFile(ledgerPath, []byte(`{"id":"bead-init","status":"open"}`+"\n"+
			`{"id":"agent-bead","status":"closed"}`+"\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"mergeToMainBeadsCommittingFactory: WriteFile ledger: " + err2.Error()}
		}

		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt", ".beads/issues.jsonl")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"mergeToMainBeadsCommittingFactory: git add: " + string(out)}
		}

		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: agent work + bead close",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, &testSetupError{"mergeToMainBeadsCommittingFactory: git commit: " + string(out)}
		}

		return wtPath, cleanup, nil
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (p): ONLY .beads/issues.jsonl conflicts → auto-resolve → bead closes
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_BeadsLedgerOnlyConflict_AutoResolved verifies that when main
// advances with a commit that modifies only .beads/issues.jsonl (same as the
// run-branch), git invokes the registered beads-union merge driver which
// resolves the conflict transparently so the daemon:
//
//	(p1) CloseBead is called (not ReopenBead),
//	(p2) outcome_emitted{kind=approved} is emitted,
//	(p3) refs/heads/main advances to the rebased run-branch tip.
//
// Beads: hk-pphof, hk-t48rg.
func TestMergeToMain_BeadsLedgerOnlyConflict_AutoResolved(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-beads-autoresolve-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)
	mergeToMainFixtureRegisterBeadsUnionDriver(t, projectDir) // register driver + .gitattributes
	mergeToMainFixtureInitBeadsLedger(t, projectDir)

	// Create a bare remote so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	addRemoteCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	addRemoteCmd.Dir = projectDir
	if out, err := addRemoteCmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	pushInitCmd := exec.CommandContext(t.Context(), "git", "push", "origin", "main")
	pushInitCmd.Dir = projectDir
	if out, err := pushInitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git push origin main (initial): %v\n%s", err, out)
	}

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Factory: commit work.txt + .beads/issues.jsonl on run-branch, THEN advance
	// main with a conflicting .beads/issues.jsonl-only commit + push to origin.
	beadsConflictFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := mergeToMainBeadsCommittingFactory(t)(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		// Advance main with a conflict on .beads/issues.jsonl only.
		mergeToMainFixtureAdvanceMainBeadsLedger(t, projectDir)
		// Push the advanced main so origin is up to date.
		pushAdvCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
		pushAdvCmd.Dir = projectDir
		if out, pushErr := pushAdvCmd.CombinedOutput(); pushErr != nil {
			cleanup()
			return "", nil, &testSetupError{"git push origin main (after advance): " + string(out)}
		}
		return wtPath, cleanup, nil
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  beadsConflictFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (p1): CloseBead called, ReopenBead NOT called. ─────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (beads-union driver resolved conflict)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 (beads-union driver resolved conflict)", got)
	}

	// ── Assertion (p2): outcome_emitted{kind=approved}. ─────────────────────
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Errorf("no outcome_emitted events; stream: %v", mergeToMainEventOrder(collector))
	} else {
		kind := mergeToMainPayloadKind(t, outcomeEvs[0])
		if kind != "approved" {
			t.Errorf("outcome_emitted kind = %q; want %q", kind, "approved")
		}
	}

	// ── Assertion (p3): main advanced. ───────────────────────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main HEAD unchanged after beads-union driver resolved conflict: still %s", mainSHABefore)
	}

	t.Logf("merge-to-main beads-union driver OK: main %s → %s, events: %v",
		mainSHABefore[:8], mainSHAAfter[:8], mergeToMainEventOrder(collector))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test (q): real code conflict alongside .beads/issues.jsonl → escalates
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeToMain_RealConflictWithBeadsLedger_Escalates verifies that when
// both work.txt AND .beads/issues.jsonl conflict during rebase, the rebase
// fails (work.txt cannot be auto-resolved by the beads-union driver) and the
// daemon escalates:
//
//	(q1) calls ReopenBead,
//	(q2) emits outcome_emitted{kind=rejected, reason containing "rebase_conflict"},
//	(q3) does NOT call CloseBead.
//
// Beads: hk-pphof, hk-t48rg.
func TestMergeToMain_RealConflictWithBeadsLedger_Escalates(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergetomain-beads-escalate-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)
	mergeToMainFixtureInitBeadsLedger(t, projectDir)

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Factory: commit work.txt + .beads/issues.jsonl on run-branch, THEN advance
	// main with conflicts on BOTH work.txt AND .beads/issues.jsonl.
	realAndBeadsConflictFactory := func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := mergeToMainBeadsCommittingFactory(t)(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		// Advance main with conflicting work.txt AND .beads/issues.jsonl.
		mergeToMainFixtureAdvanceMainConflicting(t, projectDir) // edits work.txt
		mergeToMainFixtureAdvanceMainBeadsLedger(t, projectDir) // edits .beads/issues.jsonl
		return wtPath, cleanup, nil
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  realAndBeadsConflictFactory,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.doneCh:
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for bead close/reopen")
	}

	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s")
	}

	// ── Assertion (q1): ReopenBead called. ───────────────────────────────────
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥1 on real-conflict escalate path", got)
	}

	// ── Assertion (q3): CloseBead NOT called. ────────────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 on real-conflict escalate path", got)
	}

	// ── Assertion (q2): outcome_emitted{kind=rejected, reason=rebase_conflict}. ─
	outcomeEvs := mergeToMainFindEvents(collector, "outcome_emitted")
	if len(outcomeEvs) == 0 {
		t.Errorf("no outcome_emitted events; stream: %v", mergeToMainEventOrder(collector))
	} else {
		kind := mergeToMainPayloadKind(t, outcomeEvs[0])
		if kind != "rejected" {
			t.Errorf("outcome_emitted kind = %q; want %q", kind, "rejected")
		}
		reason := mergeToMainPayloadReason(t, outcomeEvs[0])
		if !strings.Contains(reason, "rebase_conflict") {
			t.Errorf("outcome_emitted reason %q does not contain %q", reason, "rebase_conflict")
		}
	}

	t.Logf("merge-to-main real-conflict escalate OK: events: %v", mergeToMainEventOrder(collector))
}
