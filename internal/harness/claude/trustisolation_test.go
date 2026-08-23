package claude_test

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
