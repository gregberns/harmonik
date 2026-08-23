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

func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func callStopHook(ctx context.Context, hookCommand, worktreePath string) (exitCode, durationMs int) {
	start := time.Now()

	env := append(os.Environ(), "CLAUDE_HOOK_TYPE=Stop")

	cmd := exec.CommandContext(ctx, hookCommand)
	cmd.Dir = worktreePath
	cmd.Env = env

	err := cmd.Run()
	elapsed := int(time.Since(start).Milliseconds())

	if err == nil {
		return 0, elapsed
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), elapsed
	}
	return -1, elapsed
}
