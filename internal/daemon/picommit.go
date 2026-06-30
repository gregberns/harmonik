package daemon

// picommit.go — Refs:<bead> trailer guarantee for the Pi harness (codename:pilot, PI-030/031).
//
// # Why Pi needs a daemon-side commit fallback
//
// Pi (--mode json) is unsandboxed, so it *can* self-commit. However a weak free
// model may omit the commit entirely, or commit without the "Refs: <bead-id>"
// trailer that harmonik's detection path (workloop.go beadAlreadySubsumedInMain)
// matches line-for-line. The fallback fires deterministically after Pi exits so
// the standard trailer-detection path always succeeds.
//
// # Three parts (mirroring codex, see codexcommit.go)
//
//  1. INSTRUCT — the Pi seed prompt (pilaunchspec.go) instructs Pi to commit with
//     the Refs: trailer. ensurePiRefsTrailer relies on that as the happy path.
//  2. VERIFY — worktreeHEADHasRefsTrailer (codexcommit.go) inspects HEAD exactly.
//  3. FALLBACK — ensurePiRefsTrailer:
//       - HEAD already carries the trailer → no-op.
//       - HEAD advanced but lacks the trailer → AMEND the commit to append it.
//       - No commit but the worktree is dirty → stage all + CREATE a commit.
//       - No commit and clean worktree → piRefsNoChange (caller routes to no_commit).
//
// The fallback is gated at the Completion()==ProcessExit seam in workloop.go
// (~4230) so it fires only for Pi and codex, never for interactive claude.
//
// LOAD-BEARING (PI-031): every git operation routes through the run's runner when
// non-nil and falls back to local exec when nil, so the remote SSH substrate
// works identically to local. All runner-routing mirrors codexcommit.go exactly:
// worktreeHEADHasRefsTrailer (129–149), codexWorktreeDirty (161–175),
// commitAllWithPiRefsTrailer (mirrors 266–291), amendHEADAddRefsTrailer (302–338).
//
// Spec: specs/pi-harness.md §3 (PI-030/PI-031).
// Design: ~/.kerf/projects/gregberns-harmonik/pilot/04-design/pi-harness-design.md §3.5.
// Mirrors: codexcommit.go:204–255 (ensureCodexRefsTrailer decision table).
// Bead: hk-mazln.

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// piRefsOutcome classifies what ensurePiRefsTrailer did so the caller can route
// the run (success → merge path; piRefsNoChange → no_commit failure path).
type piRefsOutcome int

const (
	// piRefsAlreadyPresent: HEAD already carried the trailer; no action taken.
	piRefsAlreadyPresent piRefsOutcome = iota

	// piRefsAmended: an existing turn commit lacked the trailer and was amended
	// to append it.
	piRefsAmended

	// piRefsCommitted: Pi edited files but produced no commit; the fallback staged
	// the changes and created a trailer-carrying commit.
	piRefsCommitted

	// piRefsNoChange: HEAD did not advance past parent and the worktree is clean —
	// Pi did no work. The caller routes this to the standard no_commit failure
	// path; the fallback deliberately does NOT fabricate a commit.
	piRefsNoChange
)

// String renders the outcome for log/event diagnostics.
func (o piRefsOutcome) String() string {
	switch o {
	case piRefsAlreadyPresent:
		return "already_present"
	case piRefsAmended:
		return "amended"
	case piRefsCommitted:
		return "committed"
	case piRefsNoChange:
		return "no_change"
	default:
		return fmt.Sprintf("piRefsOutcome(%d)", int(o))
	}
}

// ensurePiRefsTrailer guarantees the worktree HEAD carries a "Refs: <beadID>"
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
// Parameters mirror ensureCodexRefsTrailer (codexcommit.go:204–255) verbatim.
// Runner-routing (PI-031): all git ops route through runner when non-nil so the
// remote SSH substrate works (mirrors codexcommit.go:129–149/161–175/266–291/302–338).
//
// On error the caller MUST treat the run as failed.
func ensurePiRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (piRefsOutcome, error) {
	if wtPath == "" {
		return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: wtPath must be non-empty")
	}
	if beadID == "" {
		return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: beadID must be non-empty")
	}

	// VERIFY: does HEAD already carry the trailer? Happy path: Pi self-committed
	// with the correct trailer (the seed prompt instructs this).
	hasTrailer, trailerErr := worktreeHEADHasRefsTrailer(ctx, runner, wtPath, beadID)
	if trailerErr == nil && hasTrailer {
		return piRefsAlreadyPresent, nil
	}

	// Determine whether Pi produced a commit this turn (HEAD advanced past the
	// parent SHA captured before launch). Route through runner so remote HEAD is
	// read from the worker, matching the no-commit guard (resolveWorktreeHEADVia).
	curHead, headErr := resolveWorktreeHEADVia(ctx, runner, wtPath)
	if headErr != nil {
		return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: resolve HEAD: %w", headErr)
	}

	if parentSHA != "" && curHead != parentSHA {
		// Pi committed but the commit lacks the trailer. Amend to append the
		// trailer — the edits are already in the commit, so a follow-up empty
		// commit would be noise. Keeps a single work-commit carrying the trailer.
		if err := amendHEADAddRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
			return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: amend: %w", err)
		}
		return piRefsAmended, nil
	}

	// HEAD did not advance. Either Pi edited files without committing (dirty →
	// deterministic commit) or did nothing (clean → no_change).
	dirty, dirtyErr := codexWorktreeDirty(ctx, runner, wtPath)
	if dirtyErr != nil {
		return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: status: %w", dirtyErr)
	}
	if !dirty {
		// Pi did no work. Do NOT fabricate a commit — let the caller route this
		// to the standard no_commit failure path.
		return piRefsNoChange, nil
	}

	// Pi edited but never committed: stage everything and create the commit.
	if err := commitAllWithPiRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
		return piRefsNoChange, fmt.Errorf("daemon: ensurePiRefsTrailer: commit: %w", err)
	}
	return piRefsCommitted, nil
}

// commitAllWithPiRefsTrailer stages every change in the worktree (tracked,
// untracked, deletions) and creates a commit carrying the Refs: trailer.
//
// Mirrors commitAllWithRefsTrailer (codexcommit.go:266–291) with a Pi-specific
// fallback commit message. Runner-routing is identical (PI-031).
func commitAllWithPiRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	msg := fmt.Sprintf(
		"feat(pi): pi turn output (auto-committed by daemon fallback)\n\n%s",
		codexRefsTrailerLine(beadID),
	)
	if runner != nil {
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "add", "-A").CombinedOutput(); err != nil {
			return fmt.Errorf("git add -A: %w\ngit output: %s", err, out)
		}
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "commit", "-m", msg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit: %w\ngit output: %s", err, out)
		}
		return nil
	}
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add -A: %w\ngit output: %s", err, out)
	}
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	commitCmd.Dir = wtPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\ngit output: %s", err, out)
	}
	return nil
}
