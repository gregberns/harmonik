package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultClaudeProjectsDir_EnvVarWins is the seam itself. Without it there is
// nothing a TestMain can set, and every fix downstream is unenforceable.
func TestDefaultClaudeProjectsDir_EnvVarWins(t *testing.T) {
	want := filepath.Join(t.TempDir(), "claude-projects")
	t.Setenv("HARMONIK_CLAUDE_PROJECTS_DIR", want)
	t.Setenv("CLAUDE_CONFIG_HOME", filepath.Join(t.TempDir(), "should-lose"))

	if got := DefaultClaudeProjectsDir(); got != want {
		t.Errorf("DefaultClaudeProjectsDir() = %q, want %q", got, want)
	}
}

// TestDefaultClaudeProjectsDir_ConfigHomeIsSecond mirrors the precedence
// defaultClaudeGlobalConfigPath already uses, so an operator who sets Claude's
// own directory variable gets one answer from both functions.
func TestDefaultClaudeProjectsDir_ConfigHomeIsSecond(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARMONIK_CLAUDE_PROJECTS_DIR", "")
	t.Setenv("CLAUDE_CONFIG_HOME", dir)

	want := filepath.Join(dir, "projects")
	if got := DefaultClaudeProjectsDir(); got != want {
		t.Errorf("DefaultClaudeProjectsDir() = %q, want %q", got, want)
	}
}

// TestDefaultClaudeProjectsDir_FallsBackToHome pins the production default. The
// seam must not change where a real daemon looks.
func TestDefaultClaudeProjectsDir_FallsBackToHome(t *testing.T) {
	t.Setenv("HARMONIK_CLAUDE_PROJECTS_DIR", "")
	t.Setenv("CLAUDE_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".claude", "projects")
	if got := DefaultClaudeProjectsDir(); got != want {
		t.Errorf("DefaultClaudeProjectsDir() = %q, want %q", got, want)
	}
}

// TestDefaultClaudeProjectsDir_UnderTestPointsAwayFromHome is the regression
// guard for the whole point of the seam: while this package's tests run, the
// resolved directory must not be inside the operator's home. The value comes
// from the package TestMain (hermetic.Main), so this test fails the moment that
// hookup is removed.
func TestDefaultClaudeProjectsDir_UnderTestPointsAwayFromHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	got := DefaultClaudeProjectsDir()
	if strings.HasPrefix(got, filepath.Join(home, ".claude")) {
		t.Errorf("DefaultClaudeProjectsDir() = %q under test, which is the operator's real transcript store", got)
	}
}
