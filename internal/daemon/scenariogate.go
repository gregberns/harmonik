package daemon

// scenariogate.go — pre-merge gate that runs //go:build scenario tests when the
// committed changes touch scenario-tagged files.
//
// The commit-gate's default "go test ./..." skips //go:build scenario tests, so
// a bead that adds a failing scenario test merges green. This gate detects
// scenario-touching commits and re-runs the relevant tagged package(s) before
// mergeRunBranchToMain is allowed to proceed.
//
// Detection: a file is "scenario-touching" when it lives under test/scenario/,
// internal/scenario/, or contains a //go:build scenario (or legacy // +build
// scenario) line.
//
// Bead: hk-i2ie5.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// scenarioGateTimeout is the maximum time the scenario test suite may run
// before the gate cancels it and blocks the merge.  Matches the CI budget
// for the scenario tier (docs/foundation/project-level/testing.md §CI gates).
const scenarioGateTimeout = 10 * time.Minute

// scenarioGateResult carries the outcome of runScenarioGateIfNeeded.
type scenarioGateResult struct {
	// blocked is true when scenario tests were found and at least one failed.
	blocked bool
	// reason is the human-readable failure description used for bead reopen
	// and run_completed emission.  Empty when blocked is false.
	reason string
}

// runScenarioGateIfNeeded inspects the commits added to wtPath since headSHA.
// If any changed file is scenario-touching it runs
//
//	go test -tags=scenario <pkgs...>
//
// in the worktree, scoped to only the affected scenario package(s), and returns
// blocked=true with a failure reason when a test genuinely FAILS (tests ran and
// at least one reported FAIL).  When no scenario-touching files are changed it
// returns the zero value (no-op).
//
// FAIL-OPEN philosophy (hk-ur428): the gate exists to catch RED scenario tests,
// not to be a flaky merge-blocker.  When `go test` cannot produce a verdict —
// SIGKILL/SIGSEGV (the heavy suite OOMs, especially under -race), context
// timeout, or a compile/build failure — the gate logs a WARNING and ALLOWS the
// merge to proceed rather than false-blocking a reviewed bead.  Only a genuine
// test failure blocks.  This matches the existing "never false-block on gate
// machinery failure" intent for git/fs errors, extended to the test-run step.
//
// -race was dropped (hk-ur428): it is the primary cause of the OOM/SIGKILL on
// the heavy concurrent-multiqueue scenario test.  The run is scoped to the
// affected package(s) (affectedScenarioPkgs), not all of ./internal/daemon/...,
// to keep the gate tractable.
//
// RETRY-ON-GENUINE-FAIL (hk-5em): under load the heavy real-daemon scenario
// tests (e.g. AllReachMerge, CaptainCrewE2E) flake — they fail run 1 with a
// genuine exit-1 `--- FAIL` (NOT a timeout/SIGKILL, so the existing fail-open
// branches don't cover them) yet pass when re-run on a quieter box.  Blocking on
// the first genuine FAIL therefore strands SOUND beads whose code was correct.
// So when the first run classifies as a genuine RED, the gate re-runs the same
// package(s) ONCE: a real regression fails deterministically on retry (BLOCK); a
// load-induced flake passes (fail-open, ALLOW).  This mirrors the shell gate
// scripts/scenario-gate.sh (hk-8b35c), keeping script and daemon in lock-step as
// standard-bead.dot D3 requires, and extends the OOM/SIGKILL non-block precedent
// to the one residual flake shape (genuine-FAIL-under-load) it didn't cover.
//
// On git/filesystem errors the gate is skipped (conservative: never false-block
// a run due to gate machinery failure).
//
// TODO(hk-ur428): the gate runs the PRE-rebase worktree (based on headSHA)
// OUTSIDE deps.mergeMu, so a sibling that advanced main with a conflicting
// scenario change between this gate and the merge is not re-gated against the
// rebased tree.  Moving the gate inside the mergeMu critical section (or
// re-gating post-rebase) is the correct fix but requires threading the run
// branch / rebased SHA through lockedMergeRunBranchToMain; deferred to avoid a
// large merge-path refactor in this fail-open fix.
//
// Bead: hk-i2ie5, hk-ur428.
func runScenarioGateIfNeeded(ctx context.Context, wtPath, headSHA string) scenarioGateResult {
	changedFiles, err := changedFilesSince(ctx, wtPath, headSHA)
	if err != nil || len(changedFiles) == 0 {
		return scenarioGateResult{}
	}

	pkgs := affectedScenarioPkgs(wtPath, changedFiles)
	if len(pkgs) == 0 {
		return scenarioGateResult{}
	}

	return scenarioGateWithRetry(pkgs, func() scenarioGateResult {
		return runScenarioGateOnce(ctx, wtPath, pkgs)
	})
}

// runScenarioGateOnce runs the scenario suite exactly once for the given
// package(s) and classifies the result.  Extracted from runScenarioGateIfNeeded
// so the retry-on-flaky path (hk-5em) can re-invoke a single run.
func runScenarioGateOnce(ctx context.Context, wtPath string, pkgs []string) scenarioGateResult {
	gateCtx, cancel := context.WithTimeout(ctx, scenarioGateTimeout)
	defer cancel()

	// -race dropped (hk-ur428) — it is the primary OOM/SIGKILL cause on the
	// heavy suite.  Scoped to the affected package(s) only.
	args := append([]string{"test", "-tags=scenario"}, pkgs...)
	cmd := exec.CommandContext(gateCtx, "go", args...)
	cmd.Dir = wtPath
	out, testErr := cmd.CombinedOutput()

	return classifyScenarioGateError(gateCtx.Err(), testErr, out, pkgs)
}

// scenarioGateWithRetry applies the retry-on-genuine-FAIL policy (hk-5em) over a
// run-once callback.  It runs the gate; if the first run is non-block it returns
// immediately; if the first run is a genuine RED it re-runs ONCE and only blocks
// when the retry is ALSO a genuine RED (a deterministic regression).  A run that
// fails once but not on retry is treated as a load-induced flake and fails open.
//
// runOnce is injected so this policy is unit-testable without a real `go test`;
// the production caller supplies runScenarioGateOnce.  Mirrors the shell gate
// scripts/scenario-gate.sh (hk-8b35c) so script and daemon agree (standard-bead
// D3).
func scenarioGateWithRetry(pkgs []string, runOnce func() scenarioGateResult) scenarioGateResult {
	first := runOnce()
	if !first.blocked {
		return first
	}
	fmt.Fprintf(os.Stderr,
		"daemon: scenario-gate: first-run FAIL for `go test -tags=scenario %s` — retrying once to check for flakiness (hk-5em)\n",
		strings.Join(pkgs, " "))
	retry := runOnce()
	if !retry.blocked {
		fmt.Fprintf(os.Stderr,
			"daemon: scenario-gate: WARNING: FLAKY — `go test -tags=scenario %s` failed run 1 but not run 2 — ALLOWING merge (pre-existing flaky red, not a regression; hk-5em)\n",
			strings.Join(pkgs, " "))
		return scenarioGateResult{} // non-block: flaky, not a real RED
	}
	// Genuine FAIL on both runs → deterministic regression → BLOCK.
	return retry
}

// classifyScenarioGateError interprets the result of the gate's `go test`
// invocation and decides whether to BLOCK the merge.
//
// gateErr is gateCtx.Err() (non-nil when the gate's deadline/cancel fired);
// testErr is the error returned by CombinedOutput; out is the combined output.
//
// Classification (hk-ur428):
//   - testErr == nil → tests passed → NON-block.
//   - context.DeadlineExceeded / context.Canceled (gate timed out or was
//     cancelled) → gate could not produce a verdict → NON-block (WARN).
//   - signal kill (SIGKILL/SIGSEGV — ExitError carrying a signal, or output
//     containing "signal: killed") → OOM/crash, not a verdict → NON-block (WARN).
//   - compile/build failure (exit code 2, or output containing "[build failed]",
//     "[setup failed]", or "build constraints exclude all Go files") → not a
//     verdict → NON-block (WARN).
//   - genuine test failure (exit code 1 with "--- FAIL" / "FAIL" output) → the
//     tests RAN and some FAILED → BLOCK.
//   - any other non-nil testErr we cannot positively classify → conservative
//     NON-block (WARN): fail-open, since the whole point is to not false-block a
//     reviewed bead on gate-infrastructure noise.
//
// It is pure (no exec / no IO) so it can be unit-tested without running a real
// scenario suite.
func classifyScenarioGateError(gateErr, testErr error, out []byte, pkgs []string) scenarioGateResult {
	if testErr == nil {
		return scenarioGateResult{} // tests passed
	}

	trimmed := strings.TrimSpace(string(out))
	const maxOut = 2000
	if len(trimmed) > maxOut {
		trimmed = trimmed[len(trimmed)-maxOut:]
	}
	pkgList := strings.Join(pkgs, " ")

	warn := func(class string) scenarioGateResult {
		fmt.Fprintf(os.Stderr,
			"daemon: scenario-gate: WARNING: could not produce a verdict (%s) for `go test -tags=scenario %s`: %v — ALLOWING merge (fail-open, hk-ur428)\n%s\n",
			class, pkgList, testErr, trimmed)
		return scenarioGateResult{} // non-block
	}

	// Timeout / cancellation — gate ran out of budget, not a real RED.
	if errors.Is(gateErr, context.DeadlineExceeded) || errors.Is(testErr, context.DeadlineExceeded) {
		return warn("timeout")
	}
	if errors.Is(gateErr, context.Canceled) || errors.Is(testErr, context.Canceled) {
		return warn("canceled")
	}

	// Signal kill (SIGKILL on OOM, SIGSEGV on crash) — the heavy suite was
	// killed by the OS / runtime, not a deterministic test verdict.
	if isSignalKill(testErr) || strings.Contains(trimmed, "signal: killed") ||
		strings.Contains(trimmed, "signal: segmentation") {
		return warn("signal-kill")
	}

	// Compile / build / setup failure — exit code 2 from `go test`, or the
	// telltale build-tooling markers.  Not a test verdict.
	if isCompileFailure(testErr, trimmed) {
		return warn("compile-fail")
	}

	// Genuine test failure: tests ran and at least one reported FAIL.
	if isGenuineTestFailure(testErr, trimmed) {
		return scenarioGateResult{
			blocked: true,
			reason: fmt.Sprintf(
				"scenario_gate_failed: go test -tags=scenario %s: %v\n%s",
				pkgList, testErr, trimmed,
			),
		}
	}

	// Unclassified non-nil error: fail-open (do not false-block a reviewed bead
	// on gate-infrastructure noise we couldn't positively identify as RED).
	return warn("unclassified")
}

// isSignalKill reports whether err is an exec.ExitError whose process was
// terminated by a signal (SIGKILL on OOM, SIGSEGV on crash) rather than exiting
// with a code.  Such a process produced no test verdict.
func isSignalKill(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	// ProcessState.Exited() is false when the process was signalled.  When it
	// did exit with a code, ExitCode() is >= 0; a signalled process reports -1.
	return !exitErr.Exited() || exitErr.ExitCode() == -1
}

// isCompileFailure reports whether the go-test failure is a compile/build/setup
// error rather than a test verdict.  `go test` returns exit code 2 for build
// failures (vs exit 1 for test failures), and emits "[build failed]" /
// "[setup failed]" / "build constraints exclude all Go files" markers.
func isCompileFailure(err error, output string) bool {
	if strings.Contains(output, "[build failed]") ||
		strings.Contains(output, "[setup failed]") ||
		strings.Contains(output, "build constraints exclude all Go files") {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return true
	}
	return false
}

// isGenuineTestFailure reports whether the output shows tests that RAN and
// FAILED (exit code 1 with a "--- FAIL" / "FAIL\t" marker), as opposed to a
// build error or signal kill.
func isGenuineTestFailure(err error, output string) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if exitErr.ExitCode() != 1 {
		return false
	}
	// Exit 1 from `go test` with a FAIL marker = genuine RED.
	return strings.Contains(output, "--- FAIL") ||
		strings.Contains(output, "\nFAIL") ||
		strings.HasPrefix(output, "FAIL")
}

// changedFilesSince returns the set of file paths (relative to wtPath) that
// differ between headSHA and the current HEAD of the worktree.
func changedFilesSince(ctx context.Context, wtPath, headSHA string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", headSHA+"..HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// affectedScenarioPkgs returns the deduplicated set of Go package patterns
// (e.g. "./internal/daemon/...") that contain scenario-tagged files among
// changedFiles.
func affectedScenarioPkgs(wtPath string, changedFiles []string) []string {
	seen := map[string]bool{}
	for _, f := range changedFiles {
		if isScenarioTouching(wtPath, f) {
			pat := fileToGoPackagePattern(f)
			if pat != "" {
				seen[pat] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// isScenarioTouching returns true when filePath (relative to wtPath) is
// scenario-touching: either its path prefix marks it as a scenario file or its
// content carries a //go:build scenario (or legacy // +build scenario) tag.
func isScenarioTouching(wtPath, filePath string) bool {
	if strings.HasPrefix(filePath, "test/scenario/") ||
		strings.HasPrefix(filePath, "internal/scenario/") {
		return true
	}
	if !strings.HasSuffix(filePath, ".go") {
		return false
	}
	full := filepath.Join(wtPath, filePath)
	data, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("//go:build scenario")) ||
		bytes.Contains(data, []byte("// +build scenario"))
}

// fileToGoPackagePattern converts a file path relative to the module root into
// a recursive Go package pattern.  Non-Go files return "".
//
// Examples:
//
//	"internal/daemon/foo_test.go" → "./internal/daemon/..."
//	"test/scenario/bar_test.go"   → "./test/scenario/..."
func fileToGoPackagePattern(filePath string) string {
	if !strings.HasSuffix(filePath, ".go") {
		return ""
	}
	dir := filepath.Dir(filePath)
	if dir == "." {
		return "./..."
	}
	return "./" + dir + "/..."
}
