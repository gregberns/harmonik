package claude_test

// This package builds launch specs, and BuildLaunchSpec calls
// workspace.EnsureWorktreeTrust, which takes an exclusive lock on Claude Code's
// user config and rewrites the whole file.
//
// For a while this package had no TestMain, so the file it locked and rewrote was
// the operator's real ~/.claude.json. Two measured consequences:
//
//   - Contention. Any live Claude session on the machine held the lock, and the
//     bounded acquire timed out with "write-lock acquire timed out". The core gate
//     then passed or failed depending on whether a sibling agent was awake.
//   - Unbounded growth. One run of this package appended 22 project entries keyed
//     by test temp directory, and nothing ever removed them. The real file had
//     reached 6426 entries, 4459 of them dead temp paths.
//
// internal/daemon had already fixed this with a package TestMain. This package was
// extracted afterwards and did not get one, which put a closed defect back into a
// core package. The test below is the guard that says so out loud, so the next
// extraction cannot lose it silently again.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeConfigIsIsolatedFromTheOperatorHome fails if this package's TestMain
// stops redirecting Claude's config, which is the exact state the package was in
// when the defect was found.
func TestClaudeConfigIsIsolatedFromTheOperatorHome(t *testing.T) {
	cfgPath := os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH")
	if cfgPath == "" {
		t.Fatal("HARMONIK_CLAUDE_CONFIG_PATH is unset, so BuildLaunchSpec locks and rewrites " +
			"the operator's real ~/.claude.json; this package needs a TestMain calling hermetic.Main")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if cfgPath == filepath.Join(home, ".claude.json") {
		t.Fatalf("HARMONIK_CLAUDE_CONFIG_PATH = %q, which is the operator's real config", cfgPath)
	}
	if strings.HasPrefix(cfgPath, filepath.Join(home, ".claude")+string(os.PathSeparator)) {
		t.Errorf("HARMONIK_CLAUDE_CONFIG_PATH = %q, which is inside the operator's Claude state", cfgPath)
	}
}
