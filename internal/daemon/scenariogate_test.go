package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileToGoPackagePattern(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"internal/daemon/foo_test.go", "./internal/daemon/..."},
		{"test/scenario/bar_test.go", "./test/scenario/..."},
		{"main.go", "./..."},
		{"internal/daemon/scenariogate.go", "./internal/daemon/..."},
		{"README.md", ""},
		{"docs/foo.txt", ""},
	}
	for _, tc := range tests {
		got := fileToGoPackagePattern(tc.file)
		require.Equal(t, tc.want, got, "file=%s", tc.file)
	}
}

func TestIsScenarioTouching_PathPrefix(t *testing.T) {
	dir := t.TempDir()
	// Files under test/scenario/ are always scenario-touching regardless of content.
	require.True(t, isScenarioTouching(dir, "test/scenario/foo_test.go"))
	require.True(t, isScenarioTouching(dir, "internal/scenario/bar.go"))
	// Files outside those paths that are not Go files are not scenario-touching.
	require.False(t, isScenarioTouching(dir, "internal/daemon/workloop.go"))
}

func TestIsScenarioTouching_BuildTag(t *testing.T) {
	dir := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}

	// File with //go:build scenario → touching.
	write("internal/daemon/x_test.go", "//go:build scenario\n\npackage daemon\n")
	require.True(t, isScenarioTouching(dir, "internal/daemon/x_test.go"))

	// File with legacy // +build scenario → touching.
	write("internal/daemon/y_test.go", "// +build scenario\n\npackage daemon\n")
	require.True(t, isScenarioTouching(dir, "internal/daemon/y_test.go"))

	// Ordinary Go file without the tag → not touching.
	write("internal/daemon/z.go", "package daemon\n")
	require.False(t, isScenarioTouching(dir, "internal/daemon/z.go"))

	// Non-existent file → not touching (conservative).
	require.False(t, isScenarioTouching(dir, "internal/daemon/missing.go"))
}

func TestAffectedScenarioPkgs(t *testing.T) {
	dir := t.TempDir()

	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write("internal/daemon/scenario_foo_test.go", "//go:build scenario\n\npackage daemon\n")
	write("internal/daemon/workloop.go", "package daemon\n")
	write("test/scenario/bar_test.go", "package scenario_test\n")

	changed := []string{
		"internal/daemon/scenario_foo_test.go",
		"internal/daemon/workloop.go",       // not scenario-touching
		"test/scenario/bar_test.go",          // path-prefix touching
	}

	pkgs := affectedScenarioPkgs(dir, changed)
	require.ElementsMatch(t, []string{
		"./internal/daemon/...",
		"./test/scenario/...",
	}, pkgs)
}

func TestAffectedScenarioPkgs_NoScenarioFiles(t *testing.T) {
	dir := t.TempDir()
	pkgs := affectedScenarioPkgs(dir, []string{"internal/daemon/workloop.go"})
	require.Empty(t, pkgs)
}
