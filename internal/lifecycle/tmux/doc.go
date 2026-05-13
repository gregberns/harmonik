// Package tmux provides the deterministic tmux substrate for harmonik agent
// sessions. It manages creating, naming, and destroying tmux windows that host
// Claude Code (and other agent) subprocesses under the operator-inspectable
// pane model required by locked decision #4 (tmux inspectability).
//
// # Scope
//
// This package covers window-level operations only: creating a new window
// inside an existing tmux session, deriving deterministic window names, and
// killing orphan windows on sweep. Session-level management (listing and
// killing entire sessions) lives in the parent [lifecycle] package
// ([lifecycle.SweepOrphanTmuxSessions]).
//
// # Interface
//
// [Adapter] is the primary interface consumed by the daemon and the handler.
// Production implementations delegate to the tmux binary via
// exec.CommandContext. Test implementations inject a fake via [Adapter].
//
// # Spec refs
//
//   - process-lifecycle.md §4.5 PL-021b — ntm adapter consumes only the
//     process/tmux surface; window-level operations exposed by this package.
//   - process-lifecycle.md §4.5 PL-021c — window-level orphan sweep; the
//     sweep implementation (hk-gql20.9) consumes [Adapter.ListWindows] and
//     [Adapter.KillWindow].
//   - workspace-model.md §4.1 WM-002a — deterministic tmux window-name
//     derivation; the [WindowName] function (hk-gql20.8) implements this rule.
package tmux
