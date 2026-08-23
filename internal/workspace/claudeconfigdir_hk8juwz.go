package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const isolatedClaudeConfigDirName = "claude-config"

const fallbackFirstStartTime = "2024-01-01T00:00:00.000Z"

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
	if err := os.MkdirAll(configDir, 0o700); err != nil { //dirmode:allow tighter on purpose: .harmonik/claude-config/ holds claude credentials, 0o700 (never widen to core.HarmonikDirMode)
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

	trustKeyPath := workspacePath
	if resolved, rerr := filepath.EvalSymlinks(workspacePath); rerr == nil {
		trustKeyPath = resolved
	}
	if err := ensureWorktreeTrustAt(trustKeyPath, destCfg); err != nil {
		return "", fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: trust upsert into isolated config: %w", err)
	}

	return absDir, nil
}

func seedIsolatedClaudeConfig(destCfg string) error {
	srcPath, srcErr := isolatedConfigSourcePath()
	if srcErr == nil {
		data, readErr := os.ReadFile(srcPath) //nolint:gosec // G304: srcPath is the operator's own config file
		if readErr == nil {
			if err := os.WriteFile(destCfg, data, 0o600); err != nil {
				return fmt.Errorf("workspace: PrepareIsolatedClaudeConfigDir: copy source config to %s: %w", destCfg, err)
			}
			return nil
		}
	}

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
