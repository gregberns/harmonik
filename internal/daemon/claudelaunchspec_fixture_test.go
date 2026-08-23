package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
)

func claudeLaunchSpecFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("claudeLaunchSpecFixtureWorkspace: MkdirAll .claude/: %v", err)
	}
	return dir
}
