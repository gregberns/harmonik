package pi

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type piRefsOutcome = shared.RefsOutcome

const (
	piRefsAlreadyPresent = shared.RefsAlreadyPresent
	piRefsAmended        = shared.RefsAmended
	piRefsCommitted      = shared.RefsCommitted
	piRefsNoChange       = shared.RefsNoChange
)

// EnsureRefsTrailer guarantees the worktree HEAD carries a "Refs: <beadID>"
// trailer after a Pi turn exits, creating or amending a commit deterministically
// when Pi edited files but did not produce a trailer-carrying commit.
//
// Decision table (parentSHA = HEAD before the turn):
//
//	HEAD has trailer                   → piRefsAlreadyPresent (no-op)
//	HEAD advanced, no trailer          → amend HEAD to add trailer → piRefsAmended
//	HEAD == parentSHA, worktree dirty  → stage all + commit w/ trailer → piRefsCommitted
//	HEAD == parentSHA, worktree clean  → piRefsNoChange (no commit fabricated)
//
// Parameters mirror ensureCodexRefsTrailer (internal/harness/codex/commit.go) verbatim.
// Runner-routing (PI-031): all git ops route through runner when non-nil so the
// remote SSH substrate works — the primitives themselves are the shared ones in
// internal/harness/shared/refstrailer.go.
//
// On error the caller MUST treat the run as failed.
func EnsureRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (shared.RefsOutcome, error) {
	if wtPath == "" {
		return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: wtPath must be non-empty")
	}
	if beadID == "" {
		return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: beadID must be non-empty")
	}

	hasTrailer, trailerErr := shared.WorktreeHEADHasRefsTrailer(ctx, runner, wtPath, beadID)
	if trailerErr == nil && hasTrailer {
		return piRefsAlreadyPresent, nil
	}

	curHead, headErr := gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
	if headErr != nil {
		return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: resolve HEAD: %w", headErr)
	}

	if parentSHA != "" && curHead != parentSHA {
		if err := shared.AmendHEADAddRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
			return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: amend: %w", err)
		}
		return piRefsAmended, nil
	}

	dirty, dirtyErr := shared.WorktreeDirty(ctx, runner, wtPath)
	if dirtyErr != nil {
		return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: status: %w", dirtyErr)
	}
	if !dirty {
		return piRefsNoChange, nil
	}

	if err := commitAllWithPiRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
		return piRefsNoChange, fmt.Errorf("daemon: EnsureRefsTrailer: commit: %w", err)
	}
	return piRefsCommitted, nil
}

func commitAllWithPiRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	return shared.CommitAllWithHarnessRefsTrailer(ctx, runner, wtPath, beadID,
		"feat(pi): pi turn output (auto-committed by daemon fallback)")
}
