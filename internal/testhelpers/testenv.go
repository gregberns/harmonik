package testhelpers

import (
	"os"
	"path/filepath"
	"testing"
)

// Env is a sandbox test environment rooted at a temporary directory.
// It holds a fully-structured .harmonik/ subtree as defined by
// specs/process-lifecycle.md §4.1 (PL-004).
//
// Usage:
//
//	env := testhelpers.NewEnv(t)
//	// env.Root    — the project root (contains .harmonik/)
//	// env.Harmonik — shorthand for filepath.Join(env.Root, ".harmonik")
//
// The entire directory tree is removed when t finishes.
type Env struct {
	// Root is the synthetic project root — the directory that would contain the
	// real repo's .git and .harmonik directories.
	Root string

	// Harmonik is the .harmonik/ directory path (Root + "/.harmonik").
	Harmonik string
}

// NewEnv constructs a sandbox .harmonik/ directory layout under a fresh temp
// directory and registers cleanup with t.Cleanup.
//
// Directories created (per PL-004):
//   - .harmonik/
//   - .harmonik/events/
//   - .harmonik/beads-intents/
//   - .harmonik/reconciliation-locks/
//   - .harmonik/worktrees/
func NewEnv(t *testing.T) *Env {
	t.Helper()

	root := TempDir(t)
	harmonik := filepath.Join(root, ".harmonik")

	dirs := []string{
		harmonik,
		filepath.Join(harmonik, "events"),
		filepath.Join(harmonik, "beads-intents"),
		filepath.Join(harmonik, "reconciliation-locks"),
		filepath.Join(harmonik, "worktrees"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("NewEnv: failed to create %q: %v", d, err)
		}
	}

	return &Env{
		Root:     root,
		Harmonik: harmonik,
	}
}

// HarmonikPath returns the path of a file or subdirectory under .harmonik/.
// Example: env.HarmonikPath("daemon.pid") → "<root>/.harmonik/daemon.pid"
func (e *Env) HarmonikPath(elem ...string) string {
	parts := append([]string{e.Harmonik}, elem...)
	return filepath.Join(parts...)
}
