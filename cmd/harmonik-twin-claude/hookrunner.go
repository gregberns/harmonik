// Hook runner for harmonik-twin-claude (hk-e66ht).
//
// Executes Stop hook commands loaded from .claude/settings.json at YAML script
// cue "call_stop_hook". Implements Fix 11b from the twin parity audit:
// twin reads settings.json and calls the Stop hook in the same way real claude
// would (per user clarification in hk-wuu5h).
//
// # Env-var contract
//
// The hook subprocess inherits the twin's own environment (os.Environ) plus:
//   - CLAUDE_HOOK_TYPE=Stop  — mandatory; identifies the hook event kind.
//
// The HARMONIK_* env vars that real claude would inherit are expected to already
// be present in the twin's environment (passed by the daemon at launch via
// handler.Launch). The twin passes them through unchanged, matching real claude
// behaviour (CHB-006).
//
// # Cwd
//
// The hook subprocess runs with cwd = --worktree-path so that hook scripts that
// resolve relative paths (e.g., harmonik hook-relay) work correctly.
//
// # Exit-code policy
//
// Hook exits non-zero → emit twin_hook_called with code, do NOT exit the twin.
// Real claude doesn't either; the daemon-side outcome handler decides what to do.
//
// Cite: docs/twin-parity-audit-2026-05-14.md §4 item 2 (Fix 11b);
// specs/claude-hook-bridge.md §4.2.CHB-006 (env-var contract);
// implementer-protocol.md §Lint compliance (exec.CommandContext required).
package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// twinHookFixture — per-bead helper prefix for test helpers in this file.
// (Actual test helpers are in hookrunner_test.go; prefix declared here as a
// godoc anchor per implementer-protocol.md §Helper-prefix discipline.)

// callStopHook executes the Stop hook command with worktreePath as cwd.
//
// Returns the exit code and wall-clock duration in milliseconds.
// Any error launching the subprocess (binary not found, etc.) is mapped to
// exit code -1 (distinguishable from real OS exit codes).
//
// The hook is run with:
//   - os.Environ() passed through (includes HARMONIK_* vars injected at twin launch).
//   - CLAUDE_HOOK_TYPE=Stop appended.
//   - Cwd = worktreePath.
//
// stdout and stderr of the hook are not captured; they flow through to the
// twin's own fds (inherited). The daemon's hook-bridge reader ingests the hook
// output exactly as it would from real claude.
func callStopHook(ctx context.Context, hookCommand, worktreePath string) (exitCode, durationMs int) {
	start := time.Now()

	env := append(os.Environ(), "CLAUDE_HOOK_TYPE=Stop") //nolint:gocritic // appendAssign: intentional new slice, not appended to os.Environ itself

	//nolint:gosec // G204: hookCommand is extracted from settings.json at operator-controlled worktreePath
	cmd := exec.CommandContext(ctx, hookCommand)
	cmd.Dir = worktreePath
	cmd.Env = env
	// stdout/stderr not captured; inherit parent fds so daemon hook-bridge sees
	// hook output exactly as it would from real claude.

	err := cmd.Run()
	elapsed := int(time.Since(start).Milliseconds())

	if err == nil {
		return 0, elapsed
	}
	// Extract exit code from ExitError when available.
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), elapsed
	}
	// Subprocess could not be launched at all (e.g., binary not found).
	return -1, elapsed
}
