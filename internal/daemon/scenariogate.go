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
//	go test -race -tags=scenario <pkgs...>
//
// in the worktree and returns blocked=true with a failure reason when any test
// fails.  When no scenario-touching files are changed it returns the zero value
// (no-op).
//
// On git/filesystem errors the gate is skipped (conservative: never false-block
// a run due to gate machinery failure).
//
// Bead: hk-i2ie5.
func runScenarioGateIfNeeded(ctx context.Context, wtPath, headSHA string) scenarioGateResult {
	changedFiles, err := changedFilesSince(ctx, wtPath, headSHA)
	if err != nil || len(changedFiles) == 0 {
		return scenarioGateResult{}
	}

	pkgs := affectedScenarioPkgs(wtPath, changedFiles)
	if len(pkgs) == 0 {
		return scenarioGateResult{}
	}

	gateCtx, cancel := context.WithTimeout(ctx, scenarioGateTimeout)
	defer cancel()

	args := append([]string{"test", "-race", "-tags=scenario"}, pkgs...)
	cmd := exec.CommandContext(gateCtx, "go", args...)
	cmd.Dir = wtPath
	out, testErr := cmd.CombinedOutput()
	if testErr != nil {
		trimmed := strings.TrimSpace(string(out))
		const maxOut = 2000
		if len(trimmed) > maxOut {
			trimmed = trimmed[len(trimmed)-maxOut:]
		}
		return scenarioGateResult{
			blocked: true,
			reason: fmt.Sprintf(
				"scenario_gate_failed: go test -race -tags=scenario %s: %v\n%s",
				strings.Join(pkgs, " "), testErr, trimmed,
			),
		}
	}
	return scenarioGateResult{}
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
