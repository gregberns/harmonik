package daemon

// codexcommit.go — Refs:<bead> trailer guarantee for the codex harness
// (codex-harness C2/T9, hk-bpxci).
//
// # Why codex needs an extra step claude does not
//
// harmonik detects bead completion by a git commit on the worktree HEAD that
// carries a "Refs: <bead-id>" trailer (workloop.go beadAlreadySubsumedInMain /
// noCommitGuardShouldReopen; both line-match "Refs: <id>" exactly). The claude
// harness guarantees this trailer two ways:
//
//  1. INSTRUCT — the implementer seed prompt / agent-task.md tells claude to
//     commit with the trailer.
//  2. DETECT — the daemon verifies HEAD advanced past parent (a commit exists);
//     a no-commit run is failed+reopened.
//
// claude runs as an interactive TUI, so when it commits without the trailer the
// reviewer/no-commit guard catches it and the bead is re-driven. codex is
// DIFFERENT: `codex exec --json` is one-shot run-to-exit (CompletionProcessExit,
// codexharness.go) — there is no live REPL to re-prod and no second chance
// inside the same turn. So codex needs a DETERMINISTIC commit-after-exit
// fallback: if codex edited files but did not produce a trailer-carrying commit,
// the daemon creates/repairs the commit itself so the standard trailer-detection
// path succeeds.
//
// This file is the codex-SPECIFIC realisation of that fallback. It is invoked
// after the codex subprocess exits (CompletionProcessExit), before the shared
// commit-detection / merge path runs. It does NOT touch dot_cascade.go or
// workloop.go (those are concurrently owned by another lane); the integration
// hook is a single function the codex turn-driver calls.
//
// # Three parts (matching the T9 bead)
//
//  1. INSTRUCT — codexSeedPromptTemplate (codexlaunchspec.go) already tells codex
//     to commit with the Refs: trailer. ensureCodexRefsTrailer relies on that as
//     the happy path; the fallback only fires when codex disobeyed.
//  2. VERIFY — shared.WorktreeHEADHasRefsTrailer (internal/harness/shared/
//     refstrailer.go) inspects the worktree HEAD commit body for an exact
//     "Refs: <bead-id>" line.
//  3. FALLBACK — ensureCodexRefsTrailer:
//       - HEAD already carries the trailer → no-op (clean path).
//       - HEAD does NOT carry the trailer but a commit exists for this turn
//         (HEAD advanced past parent) → AMEND that commit to append the trailer
//         (the edits are already in the commit). This mirrors the claude posture
//         of a single work-commit carrying the trailer rather than spraying an
//         empty follow-up commit.
//       - No commit advanced HEAD but the worktree is dirty / has staged changes
//         (codex edited but never committed) → stage everything and CREATE a
//         commit carrying the trailer.
//       - No commit and a clean worktree (codex did nothing) → return
//         shared.RefsNoChange so the caller can route it to the standard no_commit
//         failure path. The fallback never fabricates an empty commit, so a
//         genuinely-idle codex turn is still detectable as no-work.
//
// The harness-AGNOSTIC half of this file — the outcome enum, the trailer line,
// the HEAD/dirty probes, and the commit/amend primitives — moved to
// internal/harness/shared/refstrailer.go in P2 unit E1a-0, because picommit.go
// consumed ten of those symbols and piRefsOutcome was a type alias of the codex
// enum. What remains here is codex-specific: the decision table and the codex
// fallback commit message.
//
// Spec: specs/harness-contract.md §2 N2 (CompletionProcessExit); the trailer
// contract is workloop.go beadAlreadySubsumedInMain. Mirrors the git-commit
// mechanics of persistClaudeSessionID (sessioncontext_chb023.go) for the
// fallback commit.
//
// Bead: hk-bpxci [C2/T9]

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ensureCodexRefsTrailer guarantees the worktree HEAD carries a
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
func ensureCodexRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (shared.RefsOutcome, error) {
	if wtPath == "" {
		return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: wtPath must be non-empty")
	}
	if beadID == "" {
		return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: beadID must be non-empty")
	}

	// VERIFY: does HEAD already carry the trailer? If so we are done — this is
	// the happy path where codex obeyed the seed-prompt instruction.
	hasTrailer, trailerErr := shared.WorktreeHEADHasRefsTrailer(ctx, runner, wtPath, beadID)
	if trailerErr == nil && hasTrailer {
		return shared.RefsAlreadyPresent, nil
	}

	// Determine whether codex produced a commit this turn (HEAD advanced past
	// the parent SHA the caller captured before launch). Route through runner
	// so REMOTE HEAD is read from the worker, matching the no-commit guard.
	curHead, headErr := gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
	if headErr != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: resolve HEAD: %w", headErr)
	}

	if parentSHA != "" && curHead != parentSHA {
		// codex committed but the commit lacks the trailer. AMEND it to append
		// the trailer — the edits are already in the commit, so a follow-up
		// empty commit would be noise. This keeps a single work-commit carrying
		// the trailer, matching the claude posture (one commit, trailer-bearing).
		if err := shared.AmendHEADAddRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
			return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: amend: %w", err)
		}
		return shared.RefsAmended, nil
	}

	// HEAD did not advance. Either codex edited files without committing
	// (dirty worktree → deterministic commit) or did nothing (clean → no_change).
	dirty, dirtyErr := shared.WorktreeDirty(ctx, runner, wtPath)
	if dirtyErr != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: status: %w", dirtyErr)
	}
	if !dirty {
		// codex did no work. Do NOT fabricate a commit — let the caller route
		// this to the standard no_commit failure path.
		return shared.RefsNoChange, nil
	}

	// codex edited but never committed: stage everything and create the commit.
	if err := commitAllWithRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
		return shared.RefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: commit: %w", err)
	}
	return shared.RefsCommitted, nil
}

// commitAllWithRefsTrailer is the codex harness wrapper around
// shared.CommitAllWithHarnessRefsTrailer (internal/harness/shared/refstrailer.go)
// with the codex-specific fallback message.
func commitAllWithRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	return shared.CommitAllWithHarnessRefsTrailer(ctx, runner, wtPath, beadID,
		"feat(codex): codex turn output (auto-committed by daemon fallback)")
}
