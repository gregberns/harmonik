package operatornfr_test

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// TestON024_IsPathWithinWorkspace_ExactMatch verifies that the workspace root
// itself is accepted as within scope.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — agents execute within leased workspace.
func TestON024_IsPathWithinWorkspace_ExactMatch(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	if err := operatornfr.IsPathWithinWorkspace(root, root); err != nil {
		t.Errorf("ON-024: exact workspace root should be accepted, got: %v", err)
	}
}

// TestON024_IsPathWithinWorkspace_Subdirectory verifies that a path inside the
// workspace is accepted.
//
// Spec ref: operator-nfr.md §4.7 ON-024.
func TestON024_IsPathWithinWorkspace_Subdirectory(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	sub := root + "/src/main.go"
	if err := operatornfr.IsPathWithinWorkspace(root, sub); err != nil {
		t.Errorf("ON-024: subdirectory %q should be accepted, got: %v", sub, err)
	}
}

// TestON024_IsPathWithinWorkspace_SymlinkEscape verifies that a path resolving
// outside the workspace returns ErrSandboxEscape.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — "symlinks resolving outside the
// workspace … MUST be prevented."
func TestON024_IsPathWithinWorkspace_SymlinkEscape(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	escaped := "/etc/passwd"
	err := operatornfr.IsPathWithinWorkspace(root, escaped)
	if err == nil {
		t.Errorf("ON-024: symlink-escape path %q should be rejected", escaped)
	}
	if !errors.Is(err, operatornfr.ErrSandboxEscape) {
		t.Errorf("ON-024: symlink-escape error = %v, want ErrSandboxEscape", err)
	}
}

// TestON024_IsPathWithinWorkspace_PathTraversal verifies that a path containing
// ".." that resolves outside the workspace returns ErrSandboxEscape.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — "path-traversal patterns … MUST be
// prevented."
func TestON024_IsPathWithinWorkspace_PathTraversal(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	// After Clean, ../../.. resolves to something well outside the workspace.
	traversal := root + "/../../outside"
	err := operatornfr.IsPathWithinWorkspace(root, traversal)
	if err == nil {
		t.Errorf("ON-024: path-traversal %q should be rejected after Clean", traversal)
	}
	if !errors.Is(err, operatornfr.ErrSandboxEscape) {
		t.Errorf("ON-024: path-traversal error = %v, want ErrSandboxEscape", err)
	}
}

// TestON024_IsPathWithinWorkspace_SiblingDirRejected verifies that a sibling
// directory that shares a common prefix with the workspace root is rejected.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — separator-guard prevents prefix
// collisions.
func TestON024_IsPathWithinWorkspace_SiblingDirRejected(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	sibling := "/project/.harmonik/worktrees/run-abc-evil"
	err := operatornfr.IsPathWithinWorkspace(root, sibling)
	if err == nil {
		t.Errorf("ON-024: sibling directory %q shares prefix with workspace root but is outside; must be rejected", sibling)
	}
	if !errors.Is(err, operatornfr.ErrSandboxEscape) {
		t.Errorf("ON-024: sibling error = %v, want ErrSandboxEscape", err)
	}
}

// TestON024_IsPathWithinWorkspace_ParentDirRejected verifies that the parent
// of the workspace root is rejected.
//
// Spec ref: operator-nfr.md §4.7 ON-024.
func TestON024_IsPathWithinWorkspace_ParentDirRejected(t *testing.T) {
	t.Parallel()

	const root = "/project/.harmonik/worktrees/run-abc"
	parent := "/project/.harmonik/worktrees"
	err := operatornfr.IsPathWithinWorkspace(root, parent)
	if err == nil {
		t.Errorf("ON-024: parent directory %q is outside workspace root; must be rejected", parent)
	}
	if !errors.Is(err, operatornfr.ErrSandboxEscape) {
		t.Errorf("ON-024: parent error = %v, want ErrSandboxEscape", err)
	}
}

// TestON024_IsPathWithinWorkspace_RootPathIsAlwaysEscape verifies that the
// filesystem root "/" is always rejected as an escape.
//
// Spec ref: operator-nfr.md §4.7 ON-024.
func TestON024_IsPathWithinWorkspace_RootPathIsAlwaysEscape(t *testing.T) {
	t.Parallel()

	err := operatornfr.IsPathWithinWorkspace("/project/.harmonik/worktrees/run-abc", "/")
	if err == nil {
		t.Error("ON-024: filesystem root '/' must always be rejected as sandbox escape")
	}
	if !errors.Is(err, operatornfr.ErrSandboxEscape) {
		t.Errorf("ON-024: root escape error = %v, want ErrSandboxEscape", err)
	}
}

// TestON024_SandboxViolationKind_Valid verifies that Valid() accepts the three
// declared SandboxViolationKind constants.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — three named escape patterns.
func TestON024_SandboxViolationKind_Valid(t *testing.T) {
	t.Parallel()

	valid := []operatornfr.SandboxViolationKind{
		operatornfr.SandboxViolationSymlink,
		operatornfr.SandboxViolationPathTraversal,
		operatornfr.SandboxViolationGitHook,
	}
	for _, k := range valid {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			if !k.Valid() {
				t.Errorf("ON-024: SandboxViolationKind %q.Valid() = false, want true", k)
			}
		})
	}
}

// TestON024_SandboxViolationKind_Invalid verifies that Valid() rejects unknown
// violation kinds.
//
// Spec ref: operator-nfr.md §4.7 ON-024.
func TestON024_SandboxViolationKind_Invalid(t *testing.T) {
	t.Parallel()

	invalid := []operatornfr.SandboxViolationKind{"", "unknown", "escape"}
	for _, k := range invalid {
		k := k
		t.Run("invalid/"+string(k), func(t *testing.T) {
			t.Parallel()
			if k.Valid() {
				t.Errorf("ON-024: SandboxViolationKind %q.Valid() = true, want false", k)
			}
		})
	}
}

// TestON024_ThreeViolationKindsAreDeclared verifies that exactly three
// SandboxViolationKind constants are declared (one per named escape pattern
// in ON-024).
//
// Spec ref: operator-nfr.md §4.7 ON-024 — "symlinks resolving outside the
// workspace, path-traversal patterns, git hooks sourced from untrusted paths."
func TestON024_ThreeViolationKindsAreDeclared(t *testing.T) {
	t.Parallel()

	kinds := []operatornfr.SandboxViolationKind{
		operatornfr.SandboxViolationSymlink,
		operatornfr.SandboxViolationPathTraversal,
		operatornfr.SandboxViolationGitHook,
	}

	const wantKinds = 3
	if len(kinds) != wantKinds {
		t.Errorf("ON-024: %d SandboxViolationKind constants declared, want %d (one per ON-024 escape pattern)", len(kinds), wantKinds)
	}

	seen := make(map[string]bool)
	for _, k := range kinds {
		if seen[string(k)] {
			t.Errorf("ON-024: SandboxViolationKind %q is duplicated", k)
		}
		seen[string(k)] = true
	}
}
