package core

// WorkspaceRef is a workspace reference carried on a Run (workspace-model.md §4.1;
// consumed by execution-model.md §6.1 Run.input).
//
// WorkspaceRef is a named type (not a Go alias) to distinguish workspace references
// from other string-backed identifiers at compile time. The spec defines WorkspaceRef
// as a stable reference to a leased git worktree; no format constraint is imposed
// beyond non-emptiness.
type WorkspaceRef string
