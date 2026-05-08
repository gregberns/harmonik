package scenario

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// SynthesizeProjectRoot creates the per-scenario synthetic project root at
// ScenarioProjectRoot(fixtureRoot, scenarioName) and initialises it as a fresh
// git repository. It is the harness-side implementation of the
// synthesize_project_root boundary operation declared in the SH subsystem
// envelope (SH-ENV-001).
//
// After SynthesizeProjectRoot returns successfully, the directory at the
// returned path exists on disk and contains a bare git repository produced by
// "git init". The daemon's startup sequence (PL-005 step 0) writes the
// .harmonik/ skeleton, beads.sqlite, and the event-log directory relative to
// this root when the per-scenario daemon is started — the harness MUST NOT
// pre-create those paths.
//
// fixtureRoot MUST be the absolute path to the per-suite ephemeral fixture root
// returned by NewFixtureRoot. scenarioName MUST be the scenario identifier as
// declared in the scenario file (SH-005 name-uniqueness ensures each scenario
// produces a distinct path within the fixture root).
//
// ctx is threaded through to the "git init" subprocess. Callers SHOULD use a
// context that reflects the scenario-level deadline (SH-025) so that a hung
// git init does not block the suite indefinitely.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-016a.
func SynthesizeProjectRoot(ctx context.Context, fixtureRoot, scenarioName string) (string, error) {
	projectRoot := ScenarioProjectRoot(fixtureRoot, scenarioName)

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return "", fmt.Errorf("synthesize project root: mkdir %q: %w", projectRoot, err)
	}

	cmd := exec.CommandContext(ctx, "git", "init", projectRoot) //nolint:gosec // projectRoot is an absolute path derived from fixture root, not user input
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("synthesize project root: git init %q: %w\n%s", projectRoot, err, out)
	}

	return projectRoot, nil
}
