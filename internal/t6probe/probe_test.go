// Package t6probe contains exploratory-testing probes for T6 (coverage baseline).
// These test files are NOT part of the production build.
// Run with: go test ./internal/t6probe/... -v
package t6probe_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func t6probeRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("t6probeRepoRoot: runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func t6probeGenerateProfile(t *testing.T, pkgPattern string) string {
	t.Helper()
	repoRoot := t6probeRepoRoot(t)
	profilePath := filepath.Join(t.TempDir(), "cov.out")
	//nolint:gosec // G204: profilePath and pkgPattern are t.TempDir()-based or constant test patterns
	cmd := exec.CommandContext(
		t.Context(),
		"go", "test",
		"-coverprofile="+profilePath,
		"-covermode=atomic",
		pkgPattern,
	)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("t6probeGenerateProfile(%s): go test failed: %v\n%s", pkgPattern, err, out)
	}
	return profilePath
}

func t6probeReadTotalPct(t *testing.T, profilePath string) string {
	t.Helper()
	repoRoot := t6probeRepoRoot(t)
	//nolint:gosec // G204: profilePath is t.TempDir()-based; not user input
	cmd := exec.CommandContext(t.Context(), "go", "tool", "cover", "-func="+profilePath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("t6probeReadTotalPct: go tool cover -func failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "total:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return strings.TrimSuffix(fields[len(fields)-1], "%")
			}
		}
	}
	t.Fatalf("t6probeReadTotalPct: no 'total:' line in:\n%s", out)
	return ""
}

func t6probePackagePath(t *testing.T, profilePath string) string {
	t.Helper()
	//nolint:gosec // G304: profilePath is t.TempDir()-based; not user input
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("t6probePackagePath: read %s: %v", profilePath, err)
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		slash := strings.LastIndex(line[:colon], "/")
		if slash < 0 {
			continue
		}
		seen[line[:slash]] = struct{}{}
	}
	if len(seen) != 1 {
		return ""
	}
	for pkg := range seen {
		return pkg
	}
	return ""
}

func t6probeRunGate(t *testing.T, profilePath, baselineContent string) (output string, exitCode int) {
	t.Helper()
	repoRoot := t6probeRepoRoot(t)

	baselineDir := t.TempDir()
	baselinePath := filepath.Join(baselineDir, "coverage.baseline")
	//nolint:gosec // G306: test fixture; permissions match project baseline file
	if err := os.WriteFile(baselinePath, []byte(baselineContent), 0o644); err != nil {
		t.Fatalf("t6probeRunGate: write baseline: %v", err)
	}

	origScriptPath := filepath.Join(repoRoot, "scripts", "coverage-gate.sh")
	//nolint:gosec // G304: origScriptPath is a repo-relative constant; not user input
	origScript, err := os.ReadFile(origScriptPath)
	if err != nil {
		t.Fatalf("t6probeRunGate: read gate script: %v", err)
	}

	patched := strings.Replace(
		string(origScript),
		`BASELINE_FILE="${REPO_ROOT}/coverage.baseline"`,
		`BASELINE_FILE="`+baselinePath+`"`,
		1,
	)
	if patched == string(origScript) {
		t.Fatal("t6probeRunGate: failed to patch BASELINE_FILE in gate script — line not found")
	}

	scriptDir := t.TempDir()
	patchedScriptPath := filepath.Join(scriptDir, "coverage-gate.sh")
	//nolint:gosec // G306: patched script; executable permission required
	if err := os.WriteFile(patchedScriptPath, []byte(patched), 0o755); err != nil {
		t.Fatalf("t6probeRunGate: write patched script: %v", err)
	}

	//nolint:gosec // G204: patchedScriptPath and profilePath are t.TempDir()-based; not user input
	cmd := exec.CommandContext(t.Context(), "/bin/bash", patchedScriptPath, profilePath)
	cmd.Dir = repoRoot // so `go tool cover -func` resolves the module
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("t6probeRunGate: unexpected exec error: %v", runErr)
		}
	}
	return string(out), exitCode
}

// TestT6_FloorFailureWithMatchingBaseline exercises T6-coverage-baseline-spec.md
// AC3 + AC4:
//
//   - AC3: When coverage.baseline is populated with current real numbers, Gate 2
//     (floor) fires FLOOR violations for packages below their threshold. This is
//     CORRECT signal, not a bug.
//   - AC4: Gate 3 (regression) produces NO failures when the baseline equals
//     the current coverage.
//
// Uses internal/handlercontract (~84.9%, not a HIGH_THRESHOLD package) so the
// failure is reported as FLOOR rather than HIGH-THRESHOLD.
func TestT6_FloorFailureWithMatchingBaseline(t *testing.T) {
	t.Parallel()

	profilePath := t6probeGenerateProfile(t, "./internal/handlercontract")
	actualPct := t6probeReadTotalPct(t, profilePath)
	pkgPath := t6probePackagePath(t, profilePath)
	if pkgPath == "" {
		t.Fatal("TestT6_FloorFailureWithMatchingBaseline: could not determine package path from profile")
	}

	baseline := pkgPath + " " + actualPct + "\n"

	output, exitCode := t6probeRunGate(t, profilePath, baseline)
	t.Logf("gate output:\n%s", output)
	t.Logf("exit code: %d", exitCode)

	if exitCode == 0 {
		t.Errorf("expected gate exit 1 (floor failure for %s at %s%%), got 0", pkgPath, actualPct)
	}

	if !strings.Contains(output, "FLOOR") {
		t.Errorf("expected 'FLOOR' in gate output; got:\n%s", output)
	}

	if strings.Contains(output, "REGRESSION") {
		t.Errorf("unexpected 'REGRESSION' in gate output (baseline == current); got:\n%s", output)
	}
}

// TestT6_RegressionDetection exercises T6-coverage-baseline-spec.md AC5:
// When the recorded baseline is higher than the current profile by more than
// 0.3pp, Gate 3 fires a REGRESSION message.
//
// Uses internal/handlercontract; inflates the baseline by 5pp (well above the
// 0.3pp tolerance) and asserts "REGRESSION" appears in the output.
func TestT6_RegressionDetection(t *testing.T) {
	t.Parallel()

	profilePath := t6probeGenerateProfile(t, "./internal/handlercontract")
	actualPct := t6probeReadTotalPct(t, profilePath)
	pkgPath := t6probePackagePath(t, profilePath)
	if pkgPath == "" {
		t.Fatal("TestT6_RegressionDetection: could not determine package path from profile")
	}

	var actualFloat float64
	if _, err := fmt.Sscanf(actualPct, "%f", &actualFloat); err != nil {
		t.Fatalf("TestT6_RegressionDetection: parse %q: %v", actualPct, err)
	}

	inflatedPct := fmt.Sprintf("%.1f", actualFloat+5.0)
	baseline := pkgPath + " " + inflatedPct + "\n"

	output, exitCode := t6probeRunGate(t, profilePath, baseline)
	t.Logf("gate output:\n%s", output)
	t.Logf("exit code: %d (expected 1)", exitCode)

	if exitCode == 0 {
		t.Errorf("expected gate exit 1 (regression + floor), got 0")
	}
	if !strings.Contains(output, "REGRESSION") {
		t.Errorf("expected 'REGRESSION' in gate output; got:\n%s", output)
	}
}

// TestT6_VacuousPassOnEmptyBaseline verifies the gate exits without REGRESSION
// when the baseline file is empty (no entries). This is the pre-T6 state —
// the regression gate is vacuously satisfied (no baseline to regress from).
//
// Floor/core violations may still fire (they don't depend on the baseline); the
// test only asserts the regression gate is silent.
func TestT6_VacuousPassOnEmptyBaseline(t *testing.T) {
	t.Parallel()

	profilePath := t6probeGenerateProfile(t, "./internal/handlercontract")
	output, exitCode := t6probeRunGate(t, profilePath, "# empty baseline\n")
	t.Logf("gate output:\n%s", output)
	t.Logf("exit code: %d (floor failures may be present; regression gate must be silent)", exitCode)

	if strings.Contains(output, "REGRESSION") {
		t.Errorf("unexpected 'REGRESSION' with empty baseline (vacuous pass expected); got:\n%s", output)
	}
}
