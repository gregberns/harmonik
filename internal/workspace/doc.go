// Package workspace holds filesystem-scenario tests for the workspace primitive
// defined in [specs/workspace-model.md].
//
// The workspace primitive is a per-run git worktree (§4.1 WM-001..004), governed by a
// lease-by-run rule (§4.3), and carrying a session-log directory and metadata sidecar
// (§4.7). The canonical worktree path is <repo>/.harmonik/worktrees/<run_id>/ per
// WM-002; the workspace_id is derived deterministically as "ws-"+run_id per WM-004.
//
// The Workspace TYPE itself is owned by hk-8mwo.24 (not yet shipped). This package
// currently contains only test files: the only non-test Go file is this doc.go.
// Helpers, types, and placeholder classifiers that are used exclusively by tests are
// declared in *_test.go files in this package.
//
// See [specs/workspace-model.md] §10.2 for the full test-surface obligations.
package workspace
