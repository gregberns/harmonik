package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// exitErrorWithCode runs a trivial shell that exits with the given code and
// returns the resulting *exec.ExitError, so tests exercise the real
// os.ProcessState exit-code semantics rather than a hand-rolled fake.
func exitErrorWithCode(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit "+itoa(code))
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected *exec.ExitError for exit %d", code)
	require.Equal(t, code, exitErr.ExitCode())
	return err
}

// killedExitError runs a process and SIGKILLs it, yielding an *exec.ExitError
// whose ProcessState reports termination-by-signal (Exited()==false,
// ExitCode()==-1) — the real OOM/SIGKILL shape the gate must treat as non-block.
func killedExitError(t *testing.T) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 60")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Process.Signal(syscall.SIGKILL))
	err := cmd.Wait()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected *exec.ExitError for SIGKILL")
	require.False(t, exitErr.Exited(), "killed process should report Exited()==false")
	return err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// skipScenarioGateSubprocessTests skips under -short: these tests spawn real
// shell subprocesses to obtain *exec.ExitError values with genuine OS signal
// shape. Excluded from check-short to de-saturate the -race ./... pass (hk-qpf2g).
func skipScenarioGateSubprocessTests(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("scenario-gate subprocess test — excluded from -short pass (hk-qpf2g)")
	}
}

func TestClassifyScenarioGateError_Pass(t *testing.T) {
	// testErr == nil → tests passed → non-block.
	res := classifyScenarioGateError(nil, nil, []byte("ok\tpkg\t0.5s\n"), []string{"./internal/daemon/..."})
	require.False(t, res.blocked)
	require.Empty(t, res.reason)
}

func TestClassifyScenarioGateError_Killed(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// SIGKILL (the OOM shape) → gate could not produce a verdict → non-block.
	killErr := killedExitError(t)
	res := classifyScenarioGateError(nil, killErr, []byte("signal: killed"), []string{"./internal/daemon/..."})
	require.False(t, res.blocked, "SIGKILL must NOT block (fail-open)")

	// Also covered via the output marker even if the error shape were opaque.
	res2 := classifyScenarioGateError(nil, exitErrorWithCode(t, 2), []byte("--- some output\nsignal: killed"), nil)
	require.False(t, res2.blocked, "`signal: killed` output marker must NOT block")
}

func TestClassifyScenarioGateError_Timeout(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Gate context deadline exceeded → non-block.
	res := classifyScenarioGateError(context.DeadlineExceeded, exitErrorWithCode(t, 1), []byte("panic: test timed out"), []string{"./internal/daemon/..."})
	require.False(t, res.blocked, "timeout must NOT block (fail-open)")

	// Also when the timeout surfaces on testErr (errors.Is chain).
	res2 := classifyScenarioGateError(nil, context.DeadlineExceeded, []byte(""), nil)
	require.False(t, res2.blocked)
}

func TestClassifyScenarioGateError_CompileFail(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Exit code 2 from `go test` = build failure → non-block.
	res := classifyScenarioGateError(nil, exitErrorWithCode(t, 2), []byte("# pkg\n./x.go:1:1: undefined: Foo\nFAIL\tpkg [build failed]\n"), []string{"./internal/daemon/..."})
	require.False(t, res.blocked, "compile/build failure must NOT block (fail-open)")

	// `[setup failed]` marker even on exit code 1.
	res2 := classifyScenarioGateError(nil, exitErrorWithCode(t, 1), []byte("FAIL\tpkg [setup failed]\n"), nil)
	require.False(t, res2.blocked, "[setup failed] must NOT block")

	// build-constraints-exclude marker.
	res3 := classifyScenarioGateError(nil, exitErrorWithCode(t, 1), []byte("build constraints exclude all Go files in ...\n"), nil)
	require.False(t, res3.blocked)
}

func TestClassifyScenarioGateError_GenuineTestFail(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Exit code 1 with a real --- FAIL marker = tests ran and failed → BLOCK.
	out := []byte("--- FAIL: TestSomething (0.01s)\n    foo_test.go:10: boom\nFAIL\ngithub.com/x/y\t0.02s\nFAIL\n")
	res := classifyScenarioGateError(nil, exitErrorWithCode(t, 1), out, []string{"./internal/daemon/..."})
	require.True(t, res.blocked, "a genuine test FAILURE must BLOCK")
	require.Contains(t, res.reason, "scenario_gate_failed")
	require.Contains(t, res.reason, "--- FAIL")
}

func TestClassifyScenarioGateError_Unclassified(t *testing.T) {
	// A non-ExitError error we cannot positively classify as RED → fail-open.
	res := classifyScenarioGateError(nil, errors.New("exec: \"go\": executable file not found in $PATH"), []byte(""), nil)
	require.False(t, res.blocked, "unclassified gate-infra error must NOT block (fail-open)")
}

func TestIsSignalKill(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	require.True(t, isSignalKill(killedExitError(t)))
	require.False(t, isSignalKill(exitErrorWithCode(t, 1)))
	require.False(t, isSignalKill(exitErrorWithCode(t, 2)))
	require.False(t, isSignalKill(errors.New("not an exit error")))
	require.False(t, isSignalKill(nil))
}

func TestIsCompileFailure(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	require.True(t, isCompileFailure(exitErrorWithCode(t, 2), ""))
	require.True(t, isCompileFailure(exitErrorWithCode(t, 1), "FAIL\tpkg [build failed]"))
	require.True(t, isCompileFailure(nil, "FAIL\tpkg [setup failed]"))
	require.False(t, isCompileFailure(exitErrorWithCode(t, 1), "--- FAIL: TestX"))
}

func TestIsGenuineTestFailure(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	require.True(t, isGenuineTestFailure(exitErrorWithCode(t, 1), "--- FAIL: TestX (0.0s)"))
	require.True(t, isGenuineTestFailure(exitErrorWithCode(t, 1), "ok pkg\nFAIL\n"))
	require.False(t, isGenuineTestFailure(exitErrorWithCode(t, 2), "--- FAIL: TestX"), "exit 2 is a build failure, not a test verdict")
	require.False(t, isGenuineTestFailure(killedExitError(t), "signal: killed"))
	require.False(t, isGenuineTestFailure(errors.New("plain"), "--- FAIL"))
}

// genuineFailResult builds the scenarioGateResult that classifyScenarioGateError
// returns for a genuine exit-1 `--- FAIL` (the shape AllReachMerge /
// CaptainCrewE2E produce when they flake under load).
func genuineFailResult(t *testing.T) scenarioGateResult {
	t.Helper()
	out := []byte("--- FAIL: TestAllReachMerge (0.01s)\n    x_test.go:10: boom\nFAIL\ngithub.com/x/y\t0.02s\nFAIL\n")
	res := classifyScenarioGateError(nil, exitErrorWithCode(t, 1), out, []string{"./internal/daemon/..."})
	require.True(t, res.blocked, "precondition: a genuine FAIL must classify as blocked")
	return res
}

func TestScenarioGateWithRetry_PassFirstRun(t *testing.T) {
	// A clean first run never retries and never blocks.
	calls := 0
	res := scenarioGateWithRetry([]string{"./internal/daemon/..."}, func() scenarioGateResult {
		calls++
		return scenarioGateResult{} // pass
	})
	require.False(t, res.blocked)
	require.Equal(t, 1, calls, "a passing first run must NOT retry")
}

func TestScenarioGateWithRetry_FlakyThenPass(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Genuine FAIL on run 1, pass on run 2 = load-induced flake → fail-open
	// (hk-5em). This is the AllReachMerge / CaptainCrewE2E-under-load case the
	// old gate false-blocked.
	calls := 0
	res := scenarioGateWithRetry([]string{"./internal/daemon/..."}, func() scenarioGateResult {
		calls++
		if calls == 1 {
			return genuineFailResult(t)
		}
		return scenarioGateResult{} // retry passes
	})
	require.False(t, res.blocked, "flaky red (fail run 1, pass run 2) must NOT block (fail-open)")
	require.Empty(t, res.reason)
	require.Equal(t, 2, calls, "a first-run FAIL must trigger exactly one retry")
}

func TestScenarioGateWithRetry_GenuineFailBothRuns(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Genuine FAIL on both runs = deterministic regression → BLOCK. A real
	// code-break must still be caught; retry does not weaken coverage.
	calls := 0
	want := genuineFailResult(t)
	res := scenarioGateWithRetry([]string{"./internal/daemon/..."}, func() scenarioGateResult {
		calls++
		return genuineFailResult(t)
	})
	require.True(t, res.blocked, "a deterministic regression (FAIL on both runs) must BLOCK")
	require.Contains(t, res.reason, "scenario_gate_failed")
	require.Equal(t, want.reason, res.reason, "the retry's reason is returned")
	require.Equal(t, 2, calls, "block requires the second (confirming) run")
}

func TestScenarioGateWithRetry_FlakyThenNonBlockInfra(t *testing.T) {
	skipScenarioGateSubprocessTests(t)
	// Genuine FAIL on run 1, then a non-block infra outcome (e.g. timeout /
	// SIGKILL) on retry → still fail-open. The retry being non-block for ANY
	// reason means we did not confirm a deterministic regression.
	calls := 0
	res := scenarioGateWithRetry([]string{"./internal/daemon/..."}, func() scenarioGateResult {
		calls++
		if calls == 1 {
			return genuineFailResult(t)
		}
		// Retry surfaces as a timeout (non-block per classifyScenarioGateError).
		return classifyScenarioGateError(context.DeadlineExceeded, exitErrorWithCode(t, 1), []byte("panic: test timed out"), []string{"./internal/daemon/..."})
	})
	require.False(t, res.blocked, "an unconfirmed first-run FAIL (retry non-block) must NOT block")
	require.Equal(t, 2, calls)
}

func TestFileToGoPackagePattern(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"internal/daemon/foo_test.go", "./internal/daemon/..."},
		{"test/scenario/bar_test.go", "./test/scenario/..."},
		{"main.go", "./..."},
		{"internal/daemon/scenariogate.go", "./internal/daemon/..."},
		{"README.md", ""},
		{"docs/foo.txt", ""},
	}
	for _, tc := range tests {
		got := fileToGoPackagePattern(tc.file)
		require.Equal(t, tc.want, got, "file=%s", tc.file)
	}
}

func TestIsScenarioTouching_PathPrefix(t *testing.T) {
	dir := t.TempDir()
	// Files under test/scenario/ are always scenario-touching regardless of content.
	require.True(t, isScenarioTouching(dir, "test/scenario/foo_test.go"))
	require.True(t, isScenarioTouching(dir, "internal/scenario/bar.go"))
	// Files outside those paths that are not Go files are not scenario-touching.
	require.False(t, isScenarioTouching(dir, "internal/daemon/workloop.go"))
}

func TestIsScenarioTouching_BuildTag(t *testing.T) {
	dir := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	// File with //go:build scenario → touching.
	write("internal/daemon/x_test.go", "//go:build scenario\n\npackage daemon\n")
	require.True(t, isScenarioTouching(dir, "internal/daemon/x_test.go"))

	// File with legacy // +build scenario → touching.
	write("internal/daemon/y_test.go", "// +build scenario\n\npackage daemon\n")
	require.True(t, isScenarioTouching(dir, "internal/daemon/y_test.go"))

	// Ordinary Go file without the tag → not touching.
	write("internal/daemon/z.go", "package daemon\n")
	require.False(t, isScenarioTouching(dir, "internal/daemon/z.go"))

	// Non-existent file → not touching (conservative).
	require.False(t, isScenarioTouching(dir, "internal/daemon/missing.go"))
}

func TestAffectedScenarioPkgs(t *testing.T) {
	dir := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write("internal/daemon/scenario_foo_test.go", "//go:build scenario\n\npackage daemon\n")
	write("internal/daemon/workloop.go", "package daemon\n")
	write("test/scenario/bar_test.go", "package scenario_test\n")

	changed := []string{
		"internal/daemon/scenario_foo_test.go",
		"internal/daemon/workloop.go", // not scenario-touching
		"test/scenario/bar_test.go",   // path-prefix touching
	}

	pkgs := affectedScenarioPkgs(dir, changed)
	require.ElementsMatch(t, []string{
		"./internal/daemon/...",
		"./test/scenario/...",
	}, pkgs)
}

func TestAffectedScenarioPkgs_NoScenarioFiles(t *testing.T) {
	dir := t.TempDir()
	pkgs := affectedScenarioPkgs(dir, []string{"internal/daemon/workloop.go"})
	require.Empty(t, pkgs)
}
