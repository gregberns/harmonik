package codex

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// EnsureRefsTrailer guarantees the worktree HEAD carries a
// "Refs: <beadID>" trailer after a codex turn exits, creating or amending a
// commit deterministically when codex edited files but did not produce a
// trailer-carrying commit.
//
// Parameters:
//   - ctx       — caller context, propagated to every git subprocess.
//   - runner    — the per-run CommandRunner (nil for local runs, sshRunner for
//     remote). All git operations are routed through runner so that HEAD reads,
//     dirty checks, amend, and commit all operate on the SAME host as the
//     no-commit guard (gitprobe.ResolveWorktreeHEADVia). nil is byte-identical to local
//     exec (NFR7).
//   - wtPath    — absolute path of the run's git worktree.
//   - parentSHA — the worktree HEAD SHA captured BEFORE the codex turn launched.
//     Used to decide whether codex produced a commit (HEAD != parentSHA) or only
//     dirtied the worktree (HEAD == parentSHA).
//   - beadID    — the bead correlation id; the trailer is "Refs: <beadID>".
//
// Returns the outcome (see shared.RefsOutcome) and an error. On error the caller
// MUST treat the run as failed (the trailer guarantee could not be established).
//
// Decision table (parentSHA = HEAD before the turn):
//
//	HEAD has trailer                         → shared.RefsAlreadyPresent (no-op)
//	HEAD advanced, no trailer                → amend HEAD to add trailer  → shared.RefsAmended
//	HEAD == parentSHA, worktree dirty        → stage all + commit w/ trailer → shared.RefsCommitted
//	HEAD == parentSHA, worktree clean        → shared.RefsNoChange (no commit fabricated)
func EnsureRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (shared.RefsOutcome, error) {
	if wtPath == "" {
		return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: wtPath must be non-empty")
	}
	if beadID == "" {
		return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: beadID must be non-empty")
	}

	hasTrailer, trailerErr := shared.WorktreeHEADHasRefsTrailer(ctx, runner, wtPath, beadID)
	if trailerErr == nil && hasTrailer {
		return shared.RefsAlreadyPresent, nil
	}

	curHead, headErr := gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
	if headErr != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: resolve HEAD: %w", headErr)
	}

	if parentSHA != "" && curHead != parentSHA {
		if err := shared.AmendHEADAddRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
			return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: amend: %w", err)
		}
		return shared.RefsAmended, nil
	}

	dirty, dirtyErr := shared.WorktreeDirty(ctx, runner, wtPath)
	if dirtyErr != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: status: %w", dirtyErr)
	}
	if !dirty {
		return shared.RefsNoChange, nil
	}

	if err := commitAllWithRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: commit: %w", err)
	}
	return shared.RefsCommitted, nil
}

func commitAllWithRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	return shared.CommitAllWithHarnessRefsTrailer(ctx, runner, wtPath, beadID,
		"feat(codex): codex turn output (auto-committed by daemon fallback)")
}
