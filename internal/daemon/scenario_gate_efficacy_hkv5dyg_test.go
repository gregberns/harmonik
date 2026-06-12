package daemon_test

// scenario_gate_efficacy_hkv5dyg_test.go — gate-efficacy E2E (hk-v5dyg).
//
// The scenario-gate (internal/daemon/scenariogate.go, classifyScenarioGateError)
// is intentionally FAIL-OPEN: timeout / compile-fail / signal-kill ALLOW the
// merge so a reviewed bead is never false-blocked on gate-infrastructure noise.
// Only a genuinely-RED scenario test (exit 1 with a `--- FAIL` verdict) BLOCKs.
//
// The classifier itself is unit-tested in scenariogate_test.go. What was missing
// — and what this test adds — is an END-TO-END assertion driving the real work
// loop: a bead whose worktree commit lands a deterministically-RED scenario test
// must NOT merge to main and must be REOPENED (not closed). A sibling sub-test
// pins the fail-open contract: a scenario commit that fails to COMPILE must
// still merge (the gate must not block on build noise). Together they guard
// against silent gate erosion — if a future change makes the gate fail-CLOSED on
// compile errors, or fail-OPEN on a real FAIL, exactly one of these flips RED.
//
// Refs: hk-v5dyg (Refs hk-n7fw3, hk-i2ie5, hk-ur428).

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

// scenarioGateWorktreeFactory returns a worktree factory that, on top of the
// production worktree, lands a minimal self-contained Go module under
// scenariopkg/ whose //go:build scenario test file has the supplied body. The
// committed module makes the package buildable so that the gate's
// `go test -tags=scenario ./scenariopkg/...` produces a REAL verdict (genuine
// FAIL on a t.Fatal body, or a compile error on a malformed body), not a
// no-Go-files build-constraint skip.
//
// The module needs a go.mod at the worktree root because the fixture project dir
// is not itself a Go module. That root go.mod, however, also turns on the
// post-merge build gate (workloop.go: `go build ./...` + `go vet ./...`, run when
// go.mod is present in the merged tree). With ONLY the //go:build scenario probe
// file, the untagged merge-time `go vet ./...` matches no packages and exits 1
// ("no packages to vet") — a spurious merge_build_failed that wrongly reopened
// the otherwise-mergeable compile-fail bead (hk-ti4). Committing a trivial
// NON-test scenariopkg/doc.go gives that module one vettable package so the
// merge-time build gate passes, while the bad/RED probe stays behind the scenario
// tag and so is excluded from the untagged build/vet — the gate's own
// `-tags=scenario` run still sees the real compile error / FAIL verdict.
//
// scenarioTestBody is inserted as the body of TestScenarioGateProbe(t).
func scenarioGateWorktreeFactory(scenarioTestBody string) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}
		fail := func(format string, args ...any) (string, func(), error) {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf(format, args...)
		}

		pkgDir := filepath.Join(wtPath, "scenariopkg")
		//nolint:gosec // G301: test-only worktree dir; not production
		if mkErr := os.MkdirAll(pkgDir, 0o755); mkErr != nil {
			return fail("scenarioGateWorktreeFactory: mkdir scenariopkg: %w", mkErr)
		}

		// Minimal self-contained module so `go test` can build the package
		// in isolation (the fixture project dir is not itself a Go module).
		goMod := "module scenariopkg.test\n\ngo 1.21\n"
		if wErr := os.WriteFile(filepath.Join(wtPath, "go.mod"), []byte(goMod), 0o644); wErr != nil {
			return fail("scenarioGateWorktreeFactory: write go.mod: %w", wErr)
		}

		// A trivial NON-test file so the merge-time `go vet ./...` (which does
		// NOT carry -tags=scenario, so it excludes probe_test.go) has a package
		// to vet and exits 0 instead of failing with "no packages to vet"
		// (hk-ti4).
		if wErr := os.WriteFile(filepath.Join(pkgDir, "doc.go"), []byte("package scenariopkg\n"), 0o644); wErr != nil {
			return fail("scenarioGateWorktreeFactory: write doc.go: %w", wErr)
		}

		// A scenario-tagged test. The //go:build scenario line makes the file
		// scenario-touching (isScenarioTouching), so the gate runs it.
		testSrc := "//go:build scenario\n\npackage scenariopkg\n\nimport \"testing\"\n\n" +
			"func TestScenarioGateProbe(t *testing.T) {\n" + scenarioTestBody + "\n}\n"
		if wErr := os.WriteFile(filepath.Join(pkgDir, "probe_test.go"), []byte(testSrc), 0o644); wErr != nil {
			return fail("scenarioGateWorktreeFactory: write probe_test.go: %w", wErr)
		}

		for _, args := range [][]string{
			{"add", "go.mod", "scenariopkg/doc.go", "scenariopkg/probe_test.go"},
			{"commit", "-m", "test(scenario): probe for " + runID},
		} {
			//nolint:gosec // G204: git args are test-internal literals
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Dir = wtPath
			if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
				return fail("scenarioGateWorktreeFactory: git %v: %v\n%s", args, cmdErr, out)
			}
		}
		return wtPath, cleanup, nil
	}
}

// mainTipSHA returns the SHA that `main` points at in the fixture project dir.
func mainTipSHA(t *testing.T, projectDir string) string {
	t.Helper()
	//nolint:gosec // G204: test-internal literals
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "main")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mainTipSHA: git rev-parse main: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// runGateEfficacyWorkLoop dispatches a single bead in single (one-shot) mode
// with the given scenario-test body committed in its worktree, and returns the
// terminal state once the bead is closed OR reopened (or the deadline fires).
func runGateEfficacyWorkLoop(t *testing.T, beadID core.BeadID, scenarioBody string) (ledger *stubBeadLedger, collector *stubEventCollector, projectDir, mainBefore string) {
	t.Helper()

	projectDir = t2FixtureProjectDir(t)
	mainBefore = mainTipSHA(t, projectDir)

	ledger = &stubBeadLedger{ready: []core.BeadID{beadID}}
	collector = &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"}, // single-mode auto-close path
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorktreeFactory:  scenarioGateWorktreeFactory(scenarioBody),
	})

	// The gate runs `go test -tags=scenario`, which compiles + runs a tiny
	// package; allow generous budget for cold-cache compilation under CI.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	deadline := time.After(85 * time.Second)
	for {
		if len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-waitDone
			t.Fatalf("gate-efficacy: timed out; closed=%v reopened=%v events=%v",
				ledger.closedIDs(), ledger.reopenedIDs(), collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("gate-efficacy: work loop did not exit after context cancellation")
	}
	return ledger, collector, projectDir, mainBefore
}

// TestScenarioGateEfficacy_GenuineRedBlocksMerge is the core assertion of
// hk-v5dyg: a bead whose worktree commit lands a deterministically-RED scenario
// test must be BLOCKED — reopened, not closed, and main must NOT advance.
func TestScenarioGateEfficacy_GenuineRedBlocksMerge(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const beadID = core.BeadID("hk-v5dyg-genuine-red")
	// A genuine, deterministic FAIL: `go test -tags=scenario ./scenariopkg/...`
	// → exit 1 with a `--- FAIL` verdict → isGenuineTestFailure → BLOCK.
	ledger, collector, projectDir, mainBefore := runGateEfficacyWorkLoop(t, beadID,
		"\tt.Fatal(\"deterministic RED for gate-efficacy probe (hk-v5dyg)\")")

	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("genuine-red: closed=%v reopened=%v events=%v", closed, reopened, collector.eventTypes())

	if len(closed) > 0 {
		t.Errorf("FAIL: bead %q was CLOSED despite a genuinely-RED scenario test — the gate did not block the merge (silent gate erosion)", beadID)
	}
	if len(reopened) == 0 {
		t.Errorf("FAIL: bead %q was not reopened; a blocked merge must reopen the bead (closed=%v)", beadID, closed)
	}

	// main must NOT have advanced — the merge was blocked before it ran.
	mainAfter := mainTipSHA(t, projectDir)
	if mainAfter != mainBefore {
		t.Errorf("FAIL: main advanced from %s to %s — the RED gate did not block the merge", mainBefore[:8], mainAfter[:8])
	}

	// run_failed (not run_completed-success) carrying the gate reason.
	if !gateEfficacyHasFailureWithReason(collector, "scenario_gate_failed") {
		t.Errorf("FAIL: expected a run_failed event whose summary contains \"scenario_gate_failed\"; events=%v", collector.eventTypes())
	}
}

// TestScenarioGateEfficacy_CompileFailDoesNotBlock pins the FAIL-OPEN half of
// the contract (hk-ur428): a scenario commit that fails to COMPILE is gate
// infrastructure noise, not a test verdict, and must NOT block the merge — the
// bead is closed and main advances. If a future change made the gate
// fail-CLOSED on build errors, this flips RED.
func TestScenarioGateEfficacy_CompileFailDoesNotBlock(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	const beadID = core.BeadID("hk-v5dyg-compile-fail")
	// A body that does not compile: references an undefined identifier. `go test`
	// returns exit code 2 / `[build failed]` → isCompileFailure → fail-open ALLOW.
	ledger, collector, projectDir, mainBefore := runGateEfficacyWorkLoop(t, beadID,
		"\tthisIdentifierIsUndefined()")

	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("compile-fail: closed=%v reopened=%v events=%v", closed, reopened, collector.eventTypes())

	if len(closed) == 0 {
		t.Errorf("FAIL: bead %q was not closed; a COMPILE failure must fail-open (ALLOW the merge), not block (reopened=%v)", beadID, reopened)
	}
	if len(reopened) > 0 {
		t.Errorf("FAIL: bead %q was reopened on a COMPILE failure — the gate must fail-OPEN on build noise, not block (hk-ur428)", beadID)
	}

	// main MUST have advanced — the merge proceeded under fail-open.
	mainAfter := mainTipSHA(t, projectDir)
	if mainAfter == mainBefore {
		t.Errorf("FAIL: main did not advance (still %s) — the compile-fail commit should have merged under fail-open", mainBefore[:8])
	}
}

// gateEfficacyHasFailureWithReason reports whether a run_failed event was
// emitted whose summary payload contains the given substring.
func gateEfficacyHasFailureWithReason(c *stubEventCollector, reason string) bool {
	for _, e := range c.allEvents() {
		if e.EventType != string(core.EventTypeRunFailed) {
			continue
		}
		if strings.Contains(string(e.Payload), reason) {
			return true
		}
	}
	return false
}
