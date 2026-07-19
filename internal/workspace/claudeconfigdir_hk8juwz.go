package workspace

// claudeconfigdir_hk8juwz.go — per-launch CLAUDE_CONFIG_DIR isolation for the
// claude:LOCAL launch path (hk-8juwz).
//
// # Why this exists
//
// Claude Code >= 2.1.214 renders a first-run theme/onboarding modal at Stage 1
// (BEFORE SessionStart) unless the user-level config records onboarding as
// complete. A daemon-spawned pane has no human to dismiss it, so it parks the
// full 150s and agent_ready times out (turn_count=0, no transcript).
//
// The earlier mitigation seeded a top-level "theme" key into the SHARED global
// ~/.claude.json (EnsureClaudeTheme, d13ae1cf). That was LIVE-REFUTED: (a) ~15
// concurrent live `claude` processes rewrite the shared config withOUT honoring
// harmonik's flock, so the seed is lost-updated away; and (b) top-level "theme"
// is not even the modal-gating key — the operator's known-good, modal-dismissing
// config carries no top-level "theme" at all. The modal is dismissed by
// onboarding-complete STATE (firstStartTime, migrationVersion, *MigrationComplete,
// tipsHistory, seenNotifications, …), not by theme.
//
// The robust fix is ISOLATION, not seeding a shared file: give each local launch
// its OWN config directory via the CLAUDE_CONFIG_DIR env var (the real key claude
// v2.1.214 reads — 41 refs in the binary; NOT the stale CLAUDE_CONFIG_HOME the
// trust code's test-override uses, which claude ignores), and seed that private
// dir by COPYING the operator's real, already-onboarded ~/.claude.json. Because
// the dir is private to one worktree, the fleet's concurrent writers cannot
// clobber it, and copying the operator's real config reproduces the exact
// modal-dismissing state without reverse-engineering which key gates the modal.
//
// Auth is macOS-Keychain-based (machine-global), NOT stored in ~/.claude.json
// (which holds only oauthAccount metadata), so relocating CLAUDE_CONFIG_DIR does
// NOT lose auth.
//
// Scope: claude:LOCAL only. Remote-worker isolation is a deliberate follow-up.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// isolatedClaudeConfigDirName is the leaf directory (under the worktree's
// .harmonik/ control-plane dir) that holds the per-launch, isolated Claude Code
// config. Co-locating it under .harmonik/ means:
//
//   - it is already git-ignored by the repo's root-anchored "/.harmonik/*"
//     .gitignore rule (the same mechanism that keeps .harmonik/agent-task.md and
//     .harmonik/events/ out of commits), so the operator-config COPY it holds —
//     which includes userID and oauthAccount metadata — never enters a commit; and
//   - it is reaped automatically when the worktree is torn down
//     (`git worktree remove --force --force` deletes the whole worktree tree), so
//     no separate cleanup path is needed.
const isolatedClaudeConfigDirName = "claude-config"

// fallbackFirstStartTime is a fixed past RFC3339(millis) stamp written into the
// minimal FALLBACK config when the operator's real ~/.claude.json cannot be read.
// It mirrors the format claude itself uses for firstStartTime (e.g.
// "2026-07-18T22:11:32.736Z").
//
// RISK: the fallback is a best-effort last resort, NOT a proven modal-dismisser.
// The operator's real config dismisses the modal empirically; firstStartTime
// alone may be insufficient (claude may key the modal on other onboarding state
// such as migrationVersion / *MigrationComplete). PREFER the copy — the fallback
// only fires when the source is genuinely missing/unreadable.
const fallbackFirstStartTime = "2024-01-01T00:00:00.000Z"

// isolatedConfigSourcePath resolves the OPERATOR's real, default ~/.claude.json —
// the onboarded config whose state empirically dismisses the first-run modal.
//
// It deliberately resolves os.UserHomeDir()+"/.claude.json" DIRECTLY rather than
// via claudeGlobalConfigPath(): the latter honors the CLAUDE_CONFIG_HOME /
// HARMONIK_CLAUDE_CONFIG_PATH test/isolation overrides, which could point at an
// empty or non-onboarded config and thus fail to dismiss the modal. We always
// want the operator's actual onboarded config as the seed source.
//
// Exposed as a var so unit tests can redirect the source (mirroring
// claudeGlobalConfigPath's test-seam convention).
var isolatedConfigSourcePath = defaultIsolatedConfigSourcePath

func defaultIsolatedConfigSourcePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: UserHomeDir: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// PrepareIsolatedClaudeConfigDir provisions a PRIVATE, per-launch Claude Code
// config directory for a claude:LOCAL run and returns its absolute path, ready to
// be exported to the spawned process as CLAUDE_CONFIG_DIR (hk-8juwz).
//
// It:
//
//  1. Creates <workspacePath>/.harmonik/claude-config (MkdirAll 0o700).
//  2. Seeds <dir>/.claude.json by COPYING the operator's real ~/.claude.json
//     (isolatedConfigSourcePath). If that source is missing/unreadable, it falls
//     back to a minimal onboarding-complete config (firstStartTime only) — see the
//     fallbackFirstStartTime RISK note.
//  3. Upserts the worktree-trust entry
//     (projects[<realpath(workspacePath)>].hasTrustDialogAccepted = true) INTO the
//     isolated config, so the relocated config is also folder-trusted (claude reads
//     trust from CLAUDE_CONFIG_DIR/.claude.json once relocated). This reuses the
//     shared trust writer against an EXPLICIT path (ensureWorktreeTrustAt), so the
//     existing shared-global trust writer is not touched or regressed.
//
// # Failure semantics
//
// A failure to prepare the isolated dir is a STRUCTURAL error the caller MUST
// propagate WITHOUT exec'ing claude — an un-isolated launch would re-wedge on the
// modal exactly as before the fix. This mirrors the fatal posture of the trust
// seed (EnsureWorktreeTrust).
//
// # Parameters
//
//   - workspacePath: absolute path to the worktree root; MUST be the same path
//     claude is launched with as its working directory (LaunchSpec.WorkDir), so the
//     realpath-normalized trust key matches what claude looks up.
func PrepareIsolatedClaudeConfigDir(workspacePath string) (string, error) {
	configDir := filepath.Join(workspacePath, ".harmonik", isolatedClaudeConfigDirName)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: mkdir %s: %w", configDir, err)
	}
	absDir, err := filepath.Abs(configDir)
	if err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: abs %s: %w", configDir, err)
	}

	destCfg := filepath.Join(absDir, ".claude.json")
	if err := seedIsolatedClaudeConfig(destCfg); err != nil {
		return "", err
	}

	// Upsert the worktree-trust entry into the ISOLATED config. Resolve symlinks
	// first so the key matches claude's own realpath() of its cwd (mirrors
	// EnsureWorktreeTrust). ensureWorktreeTrustAt operates against destCfg
	// explicitly, so the shared-global trust writer is untouched.
	trustKeyPath := workspacePath
	if resolved, rerr := filepath.EvalSymlinks(workspacePath); rerr == nil {
		trustKeyPath = resolved
	}
	if err := ensureWorktreeTrustAt(trustKeyPath, destCfg); err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: trust upsert into isolated config: %w", err)
	}

	return absDir, nil
}

// seedIsolatedClaudeConfig writes the initial <dir>/.claude.json: a byte copy of
// the operator's real ~/.claude.json when readable, else a minimal
// onboarding-complete fallback. The trust upsert (ensureWorktreeTrustAt) runs
// against the result, so the seeded content must be valid JSON.
func seedIsolatedClaudeConfig(destCfg string) error {
	srcPath, srcErr := isolatedConfigSourcePath()
	if srcErr == nil {
		data, readErr := os.ReadFile(srcPath) //nolint:gosec // G304: srcPath is the operator's own config file
		if readErr == nil {
			// PREFERRED path: copy the operator's real, onboarded config verbatim.
			// It carries the exact modal-dismissing onboarding state.
			if err := os.WriteFile(destCfg, data, 0o600); err != nil {
				return fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: copy source config to %s: %w", destCfg, err)
			}
			return nil
		}
		// A source that exists but is unreadable for a reason OTHER than absence
		// still falls through to the fallback rather than failing the launch: an
		// un-isolated launch re-wedges, so a best-effort isolated config beats none.
	}

	// FALLBACK: the source is missing/unreadable. Write a minimal
	// onboarding-complete config. See fallbackFirstStartTime for the risk that this
	// alone may not dismiss the modal.
	fallback := map[string]interface{}{
		"firstStartTime": fallbackFirstStartTime,
	}
	out, err := json.MarshalIndent(fallback, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: marshal fallback config: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(destCfg, out, 0o600); err != nil {
		return fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: write fallback config to %s: %w", destCfg, err)
	}
	return nil
}
