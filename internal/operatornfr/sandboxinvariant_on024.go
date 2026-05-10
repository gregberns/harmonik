package operatornfr

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrSandboxEscape is returned by IsPathWithinWorkspace when a resolved path
// escapes the leased workspace boundary.  The error is non-nil whenever the
// ON-024 sandbox invariant is violated.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — "Escape attempts — symlinks
// resolving outside the workspace, path-traversal patterns, git hooks sourced
// from untrusted paths — MUST be prevented."
var ErrSandboxEscape = errors.New("operatornfr: path escapes leased workspace boundary (ON-024)")

// IsPathWithinWorkspace reports whether resolvedPath is contained within
// workspaceRoot, enforcing the ON-024 command-execution sandbox invariant.
//
// Both arguments MUST be absolute, cleaned paths (i.e. the caller has already
// applied filepath.EvalSymlinks and filepath.Clean). The function adds a
// trailing separator to workspaceRoot before prefix-checking so that a path
// that equals workspaceRoot exactly is accepted, and a sibling directory that
// shares a common prefix with the workspace name is correctly rejected.
//
// Returns nil when resolvedPath is within workspaceRoot; returns
// ErrSandboxEscape otherwise.
//
// ON-024 enforcement responsibilities:
//   - This function provides the shared predicate.
//   - S04 (handler runner) MUST call this for every path the handler subprocess
//     requests (or that is derived from handler config).
//   - S06 (workspace manager) MUST call this before resolving any symlink target
//     or git hook path during workspace setup.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — "Agents MUST execute within a
// leased workspace directory per [workspace-model.md §4.3]. Escape attempts
// … MUST be prevented."
func IsPathWithinWorkspace(workspaceRoot, resolvedPath string) error {
	// Ensure both paths end with the separator so that prefix matching is
	// unambiguous: /workspace/foo matches /workspace/foo/ but not
	// /workspace/foobar.
	root := filepath.Clean(workspaceRoot)
	candidate := filepath.Clean(resolvedPath)

	// Exact match: the workspace root itself is within scope.
	if candidate == root {
		return nil
	}

	// Prefix match with separator guard.
	if !strings.HasPrefix(candidate, root+string(filepath.Separator)) {
		return ErrSandboxEscape
	}
	return nil
}

// SandboxViolationKind categorises the three escape-attempt patterns named by
// ON-024.  Callers record the violation kind alongside the path for structured
// logging.
//
// Spec ref: operator-nfr.md §4.7 ON-024 — three named escape patterns.
type SandboxViolationKind string

const (
	// SandboxViolationSymlink represents a symlink whose resolution target
	// escapes the workspace boundary.
	//
	// Spec ref: operator-nfr.md §4.7 ON-024 — "symlinks resolving outside the
	// workspace."
	SandboxViolationSymlink SandboxViolationKind = "symlink-escape"

	// SandboxViolationPathTraversal represents a path containing ".." sequences
	// or other traversal patterns that resolve outside the workspace.
	//
	// Spec ref: operator-nfr.md §4.7 ON-024 — "path-traversal patterns."
	SandboxViolationPathTraversal SandboxViolationKind = "path-traversal"

	// SandboxViolationGitHook represents a git hook sourced from an untrusted
	// (out-of-workspace) path.
	//
	// Spec ref: operator-nfr.md §4.7 ON-024 — "git hooks sourced from
	// untrusted paths."
	SandboxViolationGitHook SandboxViolationKind = "git-hook-untrusted"
)

// Valid reports whether k is a declared SandboxViolationKind constant.
func (k SandboxViolationKind) Valid() bool {
	switch k {
	case SandboxViolationSymlink, SandboxViolationPathTraversal, SandboxViolationGitHook:
		return true
	}
	return false
}
