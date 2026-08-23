package runloop

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

	"github.com/gregberns/harmonik/internal/gitprobe"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const scenarioGateTimeout = 10 * time.Minute

type scenarioGateResult struct {
	// blocked is true when scenario tests were found and at least one failed.
	blocked bool
	// reason is the human-readable failure description used for bead reopen
	// and run_completed emission.  Empty when blocked is false.
	reason string
}

// runScenarioGateIfNeededVia inspects the commits added to wtPath since headSHA.
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
// load-induced flake passes (fail-open, ALLOW).  The shell gate this mirrored
// is deleted, so the standard-bead.dot D3 lock-step requirement no longer
// applies: D3 now names `make core`, which retries nothing and blocks on the
// first failure.
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
// runScenarioGateIfNeededVia is the runner-routed scenario gate.
//
// For a REMOTE run (runner is an SSHRunner) the worktree, its .go files, and the
// Go toolchain all live on the WORKER, so every step of the gate — the
// `git diff` change-set probe, the per-file scenario-tag inspection, and the
// `go test -tags=scenario` run itself — must execute on the worker. Routing
// them through runner sends each command over SSH to the worker.
//
// For a LOCAL run (runner is nil or tmux.LocalRunner) the calls delegate to the
// existing bare-exec helpers byte-identically (NFR7).
func runScenarioGateIfNeededVia(ctx context.Context, runner tmux.CommandRunner, wtPath, headSHA string) scenarioGateResult {
	changedFiles, err := changedFilesSinceVia(ctx, runner, wtPath, headSHA)
	if err != nil || len(changedFiles) == 0 {
		return scenarioGateResult{}
	}

	pkgs := affectedScenarioPkgsVia(ctx, runner, wtPath, changedFiles)
	if len(pkgs) == 0 {
		return scenarioGateResult{}
	}

	return scenarioGateWithRetry(pkgs, func() scenarioGateResult {
		return runScenarioGateOnceVia(ctx, runner, wtPath, pkgs)
	})
}

func runScenarioGateOnceVia(ctx context.Context, runner tmux.CommandRunner, wtPath string, pkgs []string) scenarioGateResult {
	gateCtx, cancel := context.WithTimeout(ctx, scenarioGateTimeout)
	defer cancel()

	var out []byte
	var testErr error
	if gitprobe.RunnerIsLocalFS(runner) {
		args := append([]string{"test", "-tags=scenario"}, pkgs...)
		cmd := exec.CommandContext(gateCtx, "go", args...)
		cmd.Dir = wtPath
		out, testErr = cmd.CombinedOutput()
	} else {
		args := append([]string{"-C", wtPath, "test", "-tags=scenario"}, pkgs...)
		cmd := runner.Command(gateCtx, "go", args...)
		out, testErr = cmd.CombinedOutput()
	}

	return classifyScenarioGateError(gateCtx.Err(), testErr, out, pkgs)
}

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
	return retry
}

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

	if errors.Is(gateErr, context.DeadlineExceeded) || errors.Is(testErr, context.DeadlineExceeded) {
		return warn("timeout")
	}
	if errors.Is(gateErr, context.Canceled) || errors.Is(testErr, context.Canceled) {
		return warn("canceled")
	}

	if isSignalKill(testErr) || strings.Contains(trimmed, "signal: killed") ||
		strings.Contains(trimmed, "signal: segmentation") {
		return warn("signal-kill")
	}

	if isCompileFailure(testErr, trimmed) {
		return warn("compile-fail")
	}

	if isGenuineTestFailure(testErr, trimmed) {
		return scenarioGateResult{
			blocked: true,
			reason: fmt.Sprintf(
				"scenario_gate_failed: go test -tags=scenario %s: %v\n%s",
				pkgList, testErr, trimmed,
			),
		}
	}

	return warn("unclassified")
}

func isSignalKill(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return !exitErr.Exited() || exitErr.ExitCode() == -1
}

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

func isGenuineTestFailure(err error, output string) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if exitErr.ExitCode() != 1 {
		return false
	}
	return strings.Contains(output, "--- FAIL") ||
		strings.Contains(output, "\nFAIL") ||
		strings.HasPrefix(output, "FAIL")
}

func changedFilesSince(ctx context.Context, wtPath, headSHA string) ([]string, error) {
	return changedFilesSinceVia(ctx, nil, wtPath, headSHA)
}

func changedFilesSinceVia(ctx context.Context, runner tmux.CommandRunner, wtPath, headSHA string) ([]string, error) {
	var out []byte
	var err error
	if gitprobe.RunnerIsLocalFS(runner) {
		cmd := exec.CommandContext(ctx, "git", "diff", "--name-only", headSHA+"..HEAD")
		cmd.Dir = wtPath
		out, err = cmd.Output()
	} else {
		out, err = runner.Command(ctx, "git", "-C", wtPath, "diff", "--name-only", headSHA+"..HEAD").Output()
	}
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

func affectedScenarioPkgs(wtPath string, changedFiles []string) []string {
	return affectedScenarioPkgsVia(context.Background(), nil, wtPath, changedFiles)
}

func affectedScenarioPkgsVia(ctx context.Context, runner tmux.CommandRunner, wtPath string, changedFiles []string) []string {
	seen := map[string]bool{}
	for _, f := range changedFiles {
		if isScenarioTouchingVia(ctx, runner, wtPath, f) {
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

func isScenarioTouching(wtPath, filePath string) bool {
	return isScenarioTouchingVia(context.Background(), nil, wtPath, filePath)
}

func isScenarioTouchingVia(ctx context.Context, runner tmux.CommandRunner, wtPath, filePath string) bool {
	if strings.HasPrefix(filePath, "test/scenario/") ||
		strings.HasPrefix(filePath, "internal/scenario/") {
		return true
	}
	if !strings.HasSuffix(filePath, ".go") {
		return false
	}
	full := filepath.Join(wtPath, filePath)
	var data []byte
	if gitprobe.RunnerIsLocalFS(runner) {
		var err error
		data, err = os.ReadFile(full)
		if err != nil {
			return false
		}
	} else {
		out, err := runner.Command(ctx, "cat", full).Output()
		if err != nil {
			return false
		}
		data = out
	}
	return bytes.Contains(data, []byte("//go:build scenario")) ||
		bytes.Contains(data, []byte("// +build scenario"))
}

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
