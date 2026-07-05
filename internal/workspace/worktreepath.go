package workspace

import (
	"path/filepath"
	"sync"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// DefaultWorktreeRoot is the default worktree-root directory relative to the
// repo root, per workspace-model.md §4.1 WM-002 and §6.2.
const DefaultWorktreeRoot = ".harmonik/worktrees"

// WorktreeRootConfig carries the worktree-root override configuration per
// specs/control-points.md §4.7 CP-037.
//
// CP-037 defines four precedence layers (highest first):
//
//  1. RuntimeOverride  — runtime operator CLI flags
//  2. OperatorPolicy   — operator-policy YAML file
//  3. WorkflowDef      — per-workflow overrides
//  4. DefaultConfig    — harmonik built-in default (DefaultWorktreeRoot)
//
// At MVH only the RuntimeOverride slot is populated; the remaining layers are
// deferred until the full control-points config surface lands. The struct shape
// deliberately leaves room for those layers without exposing "single string"
// as the permanent public contract.
//
// Construct via [NoWorktreeRootOverride] (use default) or
// [WithWorktreeRootOverride] (set the runtime override slot).
// To route git commands through SSH, compose with [WithRunner].
type WorktreeRootConfig struct {
	// runtimeOverride is the highest-precedence slot per CP-037 §layer-1.
	// A nil or empty value means "no runtime override; fall through to default".
	// The override MUST be an absolute path or a path relative to repoRoot.
	runtimeOverride *string

	// runner is the CommandRunner used for all git subprocess invocations in
	// CreateWorktree and CreateReviewerWorktree.  A nil value falls back to
	// tmux.LocalRunner{} (exec.CommandContext, unchanged local behaviour).
	runner tmux.CommandRunner

	// createMu, when non-nil, is acquired at the start of CreateWorktree and
	// held for the duration of the git-worktree-add + HEAD-resolve retry loop
	// (hk-5qp7z). This serialises concurrent remote worktree-create calls that
	// share the same mutex pointer, preventing the empty-HEAD race that fires
	// when N concurrent "git worktree add" calls race on the worker's shared
	// repo (the single-create retry guard in CreateWorktree cannot recover from
	// N concurrent creates because the race persists across all retry attempts).
	// Local creates (nil runner) SHOULD also pass nil here — local concurrency
	// is governed by the existing mergeMu in the daemon workloop.
	createMu *sync.Mutex
}

// NoWorktreeRootOverride returns a WorktreeRootConfig with no override set;
// [WorktreeRootPath] will use [DefaultWorktreeRoot].
func NoWorktreeRootOverride() WorktreeRootConfig {
	return WorktreeRootConfig{}
}

// WithWorktreeRootOverride returns a WorktreeRootConfig whose runtime-override
// slot (CP-037 layer 1) is set to override. An empty string is treated as "no
// override" by [WorktreeRootPath] (equivalent to [NoWorktreeRootOverride]).
//
// The override MUST be an absolute path or a path relative to the repo root;
// callers sourcing it from operator config MUST resolve it against repoRoot
// before passing when a relative path is intended.
func WithWorktreeRootOverride(override string) WorktreeRootConfig {
	return WorktreeRootConfig{runtimeOverride: &override}
}

// WithRunner returns a copy of cfg with the given CommandRunner installed.
// CreateWorktree and CreateReviewerWorktree route every git subprocess through
// this runner; a nil runner or the zero value of WorktreeRootConfig uses
// tmux.LocalRunner{} (unchanged local behaviour).
func (cfg WorktreeRootConfig) WithRunner(r tmux.CommandRunner) WorktreeRootConfig {
	cfg.runner = r
	return cfg
}

// WithCreateMutex returns a copy of cfg with mu installed as the create
// serialisation lock (hk-5qp7z). All WorktreeRootConfig values that share the
// same *sync.Mutex pointer will serialize their git-worktree-add + HEAD-resolve
// loops. Pass nil (or omit) for local runs — they are already serialised by the
// daemon's mergeMu and don't exhibit the concurrent remote empty-HEAD race.
func (cfg WorktreeRootConfig) WithCreateMutex(mu *sync.Mutex) WorktreeRootConfig {
	cfg.createMu = mu
	return cfg
}

// commandRunner returns the effective CommandRunner: the caller-supplied runner
// when set, otherwise tmux.LocalRunner{}.
func (cfg WorktreeRootConfig) commandRunner() tmux.CommandRunner {
	if cfg.runner != nil {
		return cfg.runner
	}
	return tmux.LocalRunner{}
}

// WorktreeRootPath returns the absolute path to the worktree root directory.
//
// Per workspace-model.md §4.1 WM-002 and §6.2:
//
//	<repo>/.harmonik/worktrees/
//
// The worktree root MAY be overridden by operator configuration per
// [control-points.md §4.7 CP-037]. Supply the override via [WithWorktreeRootOverride];
// when the config carries no override (see [NoWorktreeRootOverride]), the
// default [DefaultWorktreeRoot] is used.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-002 — canonical worktree path convention.
//   - workspace-model.md §6.2 — on-disk path table.
//   - control-points.md §4.7 CP-037 — worktree-root operator override.
func WorktreeRootPath(repoRoot string, cfg WorktreeRootConfig) string {
	if cfg.runtimeOverride != nil && *cfg.runtimeOverride != "" {
		if filepath.IsAbs(*cfg.runtimeOverride) {
			return *cfg.runtimeOverride
		}
		return filepath.Join(repoRoot, *cfg.runtimeOverride)
	}
	return filepath.Join(repoRoot, DefaultWorktreeRoot)
}

// WorktreePath returns the canonical worktree path for the given run ID.
//
// Per workspace-model.md §4.1 WM-002 and §6.2:
//
//	<repo>/.harmonik/worktrees/<run_id>/
//
// where `<repo>` is the absolute path to the local clone of the backing
// repository and `<run_id>` is the run's stable identifier.
//
// The worktree root MAY be overridden by operator configuration per
// [control-points.md §4.7 CP-037]. Supply the override via
// [WithWorktreeRootOverride]; when the config carries no override (see
// [NoWorktreeRootOverride]), the default `<repo>/.harmonik/worktrees/` is used.
//
// WorktreePath does NOT validate runID against the [A-Za-z0-9-]+ regex
// mandated by WM-002 — validation is the caller's responsibility at
// run-create time. UUIDv7, the canonical run_id scheme, satisfies this
// constraint by construction.
//
// Spec refs:
//   - workspace-model.md §4.1 WM-002 — canonical worktree path convention.
//   - workspace-model.md §6.2 — on-disk path table.
//   - control-points.md §4.7 CP-037 — worktree-root operator override.
func WorktreePath(repoRoot, runID string, cfg WorktreeRootConfig) string {
	return filepath.Join(WorktreeRootPath(repoRoot, cfg), runID)
}
