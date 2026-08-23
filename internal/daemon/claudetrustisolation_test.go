package daemon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeConfigStaysIsolatedFromTheOperatorHome fails if anything in this
// package's test binary has taken away the redirect hermetic.Main installed.
func TestClaudeConfigStaysIsolatedFromTheOperatorHome(t *testing.T) {
	t.Parallel()

	cfgPath := os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH")
	if cfgPath == "" {
		t.Fatal("HARMONIK_CLAUDE_CONFIG_PATH is unset by the time the parallel tests run, so every " +
			"daemon boot after that point locks and rewrites the operator's real ~/.claude.json and " +
			"leaves a trust entry per throwaway worktree behind. hermetic.Main sets this variable " +
			"once for the whole binary; a helper that redirects it must use t.Setenv, which RESTORES " +
			"the previous value, not os.Setenv paired with os.Unsetenv, which removes it (hk-85pqo)")
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
