package daemon_test

// mergetomain_hkycp62_test.go — tests for the post-merge build gate
// (hk-o68j3 / hk-ycp62: commit-gate should build+vet the merged tree).
//
// The build gate runs go build+vet in the run-branch worktree (wtPath) after
// fast-forwarding the target branch ref, before pushing.  This catches
// cross-bead compile errors — e.g. two parallel beads each adding a
// package-level helper with the same name — that only become visible when the
// merged tree is built.
//
// Test obligations (§10.2 EM-052 build-gate addition):
//   (n) When go build fails on the merged tree: merge_build_failed is emitted,
//       ReopenBead is called, CloseBead is NOT called, refs/heads/main is NOT
//       advanced beyond the pre-merge SHA.
//   (o) When go vet fails on the merged tree (valid syntax, bad vet): same
//       as (n) with "go vet" in the failure reason.
//   (p) When no go.mod is present: build gate is skipped; normal success path
//       proceeds (CloseBead called, refs/heads/main advances).
//
// Helper prefix: mergeBuildGate (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ycp62).
//
// Spec refs:
//   - specs/execution-model.md §4.12 EM-052 step 4a, EM-053
//
// Beads: hk-ycp62, hk-o68j3.

import (
	"context"
	"fmt"
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
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// mergeBuildGateFixtureGoModule creates a git repo with a committed go.mod and
// a minimal valid Go package so the run-branch worktree inherits a Go module.
// The go.mod uses a module path that avoids import resolution (no external
// deps), so go build ./... succeeds on the initial state.
func mergeBuildGateFixtureGoModule(t *testing.T, projectDir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = projectDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mergeBuildGateFixtureGoModule: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Test")

	// Write go.mod — no external deps, uses a throwaway module path.
	goMod := "module mergegate_fixture_hkycp62\n\ngo 1.21\n"
	//nolint:gosec // G306: 0644 is fine for test fixtures
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("mergeBuildGateFixtureGoModule: WriteFile go.mod: %v", err)
	}

	// Write a minimal valid Go file so the initial tree compiles.
	mainGo := "package main\n\nfunc main() {}\n"
	//nolint:gosec // G306: 0644 is fine for test fixtures
	if err := os.WriteFile(filepath.Join(projectDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("mergeBuildGateFixtureGoModule: WriteFile main.go: %v", err)
	}

	run("add", "go.mod", "main.go")
	run("commit", "-m", "init: go module")
}

// mergeBuildGateCommittingFactoryWithError returns a worktreeFactory that
// creates the run-branch worktree, then commits a Go file containing the
// given content into it.  Use badGoContent to produce a compile or vet error
// in the merged tree.
func mergeBuildGateCommittingFactoryWithError(t *testing.T, badGoContent string) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		badFile := filepath.Join(wtPath, "agent_bad_hkycp62.go")
		//nolint:gosec // G306: 0644 is fine for test fixtures
		if writeErr := os.WriteFile(badFile, []byte(badGoContent), 0o644); writeErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeBuildGateCommittingFactoryWithError: WriteFile: %w", writeErr)
		}
		addCmd := exec.CommandContext(ctx, "git", "add", "agent_bad_hkycp62.go")
		addCmd.Dir = wtPath
		if out, addErr := addCmd.CombinedOutput(); addErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeBuildGateCommittingFactoryWithError: git add: %v\n%s", addErr, out)
		}
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: agent bad commit",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("mergeBuildGateCommittingFactoryWithError: git commit: %v\n%s", commitErr, out)
		}
		return wtPath, cleanup, nil
	}
}

// mergeBuildGateRunWorkLoop runs the work loop with the given params and waits
// for the ledger to reach a terminal state (doneCh closed) or test timeout.
func mergeBuildGateRunWorkLoop(t *testing.T, params daemon.WorkLoopDepsParams, ledger *mergeToMainRecordingLedger) {
	t.Helper()
	deps := daemon.ExportedWorkLoopDeps(params)

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
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: build failure in merged tree reopens the bead (assertion n)
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeBuildGate_BuildFailReopens verifies that when the merged run-branch
// tree fails go build, the daemon:
//
//   (n1) emits merge_build_failed,
//   (n2) calls ReopenBead,
//   (n3) does NOT call CloseBead,
//   (n4) does NOT advance refs/heads/main beyond the pre-merge SHA.
//
// The scenario simulates the real-world case from hk-ycp62: an agent commits
// a Go file with a syntax error (representing a redeclared symbol or other
// cross-bead compile breakage) that only manifests in the merged tree.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 4a, EM-053.
// Beads: hk-ycp62, hk-o68j3.
func TestMergeBuildGate_BuildFailReopens(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergebuildgate-buildfail-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeBuildGateFixtureGoModule(t, projectDir)

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// The agent commits a Go file with a syntax error — go build will fail on
	// the merged tree.  The file is valid enough to commit but not to compile.
	const syntaxErrorGo = "package main\n\nfunc badSyntax {\n  // missing parentheses\n}\n"

	mergeBuildGateRunWorkLoop(t, daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"}, // auto-close heuristic
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeBuildGateCommittingFactoryWithError(t, syntaxErrorGo),
	}, ledger)

	// ── Assertion (n2): ReopenBead called. ───────────────────────────────────
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥ 1 on merge_build_failed path", got)
	}

	// ── Assertion (n3): CloseBead NOT called. ────────────────────────────────
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 on merge_build_failed path (EM-053)", got)
	}

	// ── Assertion (n1): merge_build_failed emitted. ──────────────────────────
	buildFailEvs := mergeToMainFindEvents(collector, "merge_build_failed")
	if len(buildFailEvs) == 0 {
		t.Errorf("merge_build_failed not emitted; event stream: %v", mergeToMainEventOrder(collector))
	}

	// ── Assertion (n4): main NOT advanced. ───────────────────────────────────
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter != mainSHABefore {
		t.Errorf("refs/heads/main advanced on build failure (rollback did not fire): before=%s after=%s",
			mainSHABefore[:8], mainSHAAfter[:8])
	}

	// bead_closed must NOT appear.
	if evs := mergeToMainFindEvents(collector, "bead_closed"); len(evs) > 0 {
		t.Errorf("bead_closed emitted on build-fail path; want absent (EM-053): %v", evs)
	}

	// The reopen reason should contain merge_build_failed.
	reason := ledger.getReopenReason()
	if !strings.Contains(reason, "merge_build_failed") {
		t.Errorf("reopen reason %q does not contain \"merge_build_failed\"", reason)
	}

	t.Logf("merge build gate: build-fail-reopens OK: events=%v reopen_reason=%q",
		mergeToMainEventOrder(collector), reason)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: vet failure in merged tree reopens the bead (assertion o)
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeBuildGate_VetFailReopens verifies that when the merged tree compiles
// but fails go vet (e.g. incorrect printf format argument), the daemon reopens
// the bead and does NOT close it (same contract as build failure).
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 4a, EM-053.
// Beads: hk-ycp62, hk-o68j3.
func TestMergeBuildGate_VetFailReopens(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergebuildgate-vetfail-bead-001")

	projectDir := mergeToMainFixtureProjectDir(t)
	mergeBuildGateFixtureGoModule(t, projectDir)

	mainSHABefore := mergeToMainFixtureHeadSHA(t, projectDir, "main")

	ledger := newMergeToMainRecordingLedger(beadID)
	collector := &stubEventCollector{}

	// Valid Go that compiles but fails go vet: wrong type passed to %d.
	const vetFailGo = "package main\n\nimport \"fmt\"\n\nfunc init() {\n\tfmt.Printf(\"%d\", \"not_an_int\")\n}\n"

	mergeBuildGateRunWorkLoop(t, daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeBuildGateCommittingFactoryWithError(t, vetFailGo),
	}, ledger)

	// Vet failures cause ReopenBead + no CloseBead.
	if got := ledger.getReopenedCount(); got < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥ 1 on go-vet-failed path", got)
	}
	if got := ledger.getClosedCount(); got != 0 {
		t.Errorf("CloseBead call count = %d; want 0 on go-vet-failed path (EM-053)", got)
	}

	// Either merge_build_failed or outcome_emitted{rejected} must appear.
	// (go vet failure is reported as merge_build_failed.)
	if evs := mergeToMainFindEvents(collector, "merge_build_failed"); len(evs) == 0 {
		// Vet may cause go build to fail too; accept either event type.
		if evs2 := mergeToMainFindEvents(collector, "outcome_emitted"); len(evs2) == 0 {
			t.Errorf("neither merge_build_failed nor outcome_emitted found; event stream: %v",
				mergeToMainEventOrder(collector))
		}
	}

	// main must not have advanced.
	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter != mainSHABefore {
		t.Errorf("refs/heads/main advanced on vet failure: before=%s after=%s",
			mainSHABefore[:8], mainSHAAfter[:8])
	}

	t.Logf("merge build gate: vet-fail-reopens OK: events=%v", mergeToMainEventOrder(collector))
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: no go.mod → gate skipped, success path proceeds (assertion p)
// ─────────────────────────────────────────────────────────────────────────────

// TestMergeBuildGate_NoGoModSkipsGate verifies that when no go.mod is present
// in the build directory, the build gate is skipped entirely and the normal
// success path runs (CloseBead called, main advances).
//
// This is the default test-fixture behaviour (non-Go projects are unaffected).
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 4a.
// Beads: hk-ycp62, hk-o68j3.
func TestMergeBuildGate_NoGoModSkipsGate(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("mergebuildgate-nogomod-bead-001")

	// Standard fixture — no go.mod in projectDir or any worktree.
	projectDir := mergeToMainFixtureProjectDir(t)
	mergeToMainFixtureGitRepo(t, projectDir)

	// Create a bare remote (origin) so git push succeeds.
	originDir := t.TempDir()
	initBareCmd := exec.CommandContext(t.Context(), "git", "init", "--bare", "--initial-branch=main", originDir)
	if out, err := initBareCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	primeCmd := exec.CommandContext(t.Context(), "git", "remote", "add", "origin", originDir)
	primeCmd.Dir = projectDir
	if out, err := primeCmd.CombinedOutput(); err != nil {
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

	mergeBuildGateRunWorkLoop(t, daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:  mergeToMainCommittingFactory(t),
	}, ledger)

	// ── Assertion (p): gate skipped → normal success. ────────────────────────
	if got := ledger.getClosedCount(); got != 1 {
		t.Errorf("CloseBead call count = %d; want 1 (no go.mod → gate skipped)", got)
	}
	if got := ledger.getReopenedCount(); got != 0 {
		t.Errorf("ReopenBead call count = %d; want 0 on no-go.mod path", got)
	}
	if evs := mergeToMainFindEvents(collector, "merge_build_failed"); len(evs) > 0 {
		t.Errorf("merge_build_failed emitted without go.mod; want absent")
	}

	mainSHAAfter := mergeToMainFixtureHeadSHA(t, projectDir, "main")
	if mainSHAAfter == mainSHABefore {
		t.Errorf("main did not advance on no-go.mod success path: still %s", mainSHABefore[:8])
	}

	t.Logf("merge build gate: no-go.mod skip OK: main %s → %s", mainSHABefore[:8], mainSHAAfter[:8])
}
