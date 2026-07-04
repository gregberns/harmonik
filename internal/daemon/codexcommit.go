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
//  2. VERIFY — worktreeHEADHasRefsTrailer inspects the worktree HEAD commit body
//     for an exact "Refs: <bead-id>" line.
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
//         codexNoChange so the caller can route it to the standard no_commit
//         failure path. The fallback never fabricates an empty commit, so a
//         genuinely-idle codex turn is still detectable as no-work.
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
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// codexRefsOutcome classifies what ensureCodexRefsTrailer did so the caller can
// route the run (success → merge path; codexNoChange → no_commit failure path).
type codexRefsOutcome int

const (
	// codexRefsAlreadyPresent: HEAD already carried the trailer; no action taken.
	codexRefsAlreadyPresent codexRefsOutcome = iota

	// codexRefsAmended: an existing turn commit lacked the trailer and was
	// amended to append it.
	codexRefsAmended

	// codexRefsCommitted: codex edited files but produced no commit; the fallback
	// staged the changes and created a trailer-carrying commit.
	codexRefsCommitted

	// codexRefsNoChange: HEAD did not advance past parent and the worktree is
	// clean — codex did no work. The caller routes this to the standard
	// no_commit failure path; the fallback deliberately does NOT fabricate a
	// commit so a genuinely-idle turn stays detectable.
	codexRefsNoChange
)

// String renders the outcome for log/event diagnostics.
func (o codexRefsOutcome) String() string {
	switch o {
	case codexRefsAlreadyPresent:
		return "already_present"
	case codexRefsAmended:
		return "amended"
	case codexRefsCommitted:
		return "committed"
	case codexRefsNoChange:
		return "no_change"
	default:
		return fmt.Sprintf("codexRefsOutcome(%d)", int(o))
	}
}

// codexRefsTrailerLine returns the exact "Refs: <bead-id>" trailer line the
// daemon's commit-detection path (workloop.go beadAlreadySubsumedInMain)
// matches line-for-line. Centralised so the instruct/verify/fallback paths all
// agree on the exact text.
func codexRefsTrailerLine(beadID core.BeadID) string {
	return "Refs: " + string(beadID)
}

// worktreeHEADHasRefsTrailer reports whether the worktree HEAD commit body
// carries an exact "Refs: <beadID>" trailer line.
//
// It uses the same line-exact comparison as beadAlreadySubsumedInMain so that
// "Refs: hk-foo.1" does NOT match a commit whose only trailer is
// "Refs: hk-foo.10". Returns (false, err) on any git error (e.g. no commits
// yet); the caller treats a git error as "trailer not present".
//
// When runner is non-nil the git command is routed through it (remote worker);
// when nil it falls back to bare local exec (NFR7 — byte-identical for local).
func worktreeHEADHasRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) (bool, error) {
	var out []byte
	var err error
	if runner != nil {
		out, err = runner.Command(ctx, "git", "-C", wtPath, "log", "-1", "--format=%B", "HEAD").Output()
	} else {
		cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%B", "HEAD")
		cmd.Dir = wtPath
		out, err = cmd.Output()
	}
	if err != nil {
		return false, fmt.Errorf("daemon: worktreeHEADHasRefsTrailer: git log HEAD in %q: %w", wtPath, err)
	}
	needle := codexRefsTrailerLine(beadID)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimRight(line, "\r") == needle {
			return true, nil
		}
	}
	return false, nil
}

// codexWorktreeDirty reports whether the worktree at wtPath has any uncommitted
// changes — staged, unstaged, or untracked. Used by the fallback to decide
// whether codex edited files without committing.
//
// Returns (false, err) on git error; the caller treats an error as "not dirty"
// only after also checking HEAD advancement, so a git failure cannot silently
// fabricate a commit.
//
// When runner is non-nil the git command is routed through it (remote worker);
// when nil it falls back to bare local exec (NFR7 — byte-identical for local).
func codexWorktreeDirty(ctx context.Context, runner tmux.CommandRunner, wtPath string) (bool, error) {
	var out []byte
	var err error
	if runner != nil {
		out, err = runner.Command(ctx, "git", "-C", wtPath, "status", "--porcelain").Output()
	} else {
		cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		cmd.Dir = wtPath
		out, err = cmd.Output()
	}
	if err != nil {
		return false, fmt.Errorf("daemon: codexWorktreeDirty: git status in %q: %w", wtPath, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

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
//     no-commit guard (resolveWorktreeHEADVia). nil is byte-identical to local
//     exec (NFR7).
//   - wtPath    — absolute path of the run's git worktree.
//   - parentSHA — the worktree HEAD SHA captured BEFORE the codex turn launched.
//     Used to decide whether codex produced a commit (HEAD != parentSHA) or only
//     dirtied the worktree (HEAD == parentSHA).
//   - beadID    — the bead correlation id; the trailer is "Refs: <beadID>".
//
// Returns the outcome (see codexRefsOutcome) and an error. On error the caller
// MUST treat the run as failed (the trailer guarantee could not be established).
//
// Decision table (parentSHA = HEAD before the turn):
//
//	HEAD has trailer                         → codexRefsAlreadyPresent (no-op)
//	HEAD advanced, no trailer                → amend HEAD to add trailer  → codexRefsAmended
//	HEAD == parentSHA, worktree dirty        → stage all + commit w/ trailer → codexRefsCommitted
//	HEAD == parentSHA, worktree clean        → codexRefsNoChange (no commit fabricated)
func ensureCodexRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (codexRefsOutcome, error) {
	if wtPath == "" {
		return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: wtPath must be non-empty")
	}
	if beadID == "" {
		return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: beadID must be non-empty")
	}

	// VERIFY: does HEAD already carry the trailer? If so we are done — this is
	// the happy path where codex obeyed the seed-prompt instruction.
	hasTrailer, trailerErr := worktreeHEADHasRefsTrailer(ctx, runner, wtPath, beadID)
	if trailerErr == nil && hasTrailer {
		return codexRefsAlreadyPresent, nil
	}

	// Determine whether codex produced a commit this turn (HEAD advanced past
	// the parent SHA the caller captured before launch). Route through runner
	// so REMOTE HEAD is read from the worker, matching the no-commit guard.
	curHead, headErr := resolveWorktreeHEADVia(ctx, runner, wtPath)
	if headErr != nil {
		return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: resolve HEAD: %w", headErr)
	}

	if parentSHA != "" && curHead != parentSHA {
		// codex committed but the commit lacks the trailer. AMEND it to append
		// the trailer — the edits are already in the commit, so a follow-up
		// empty commit would be noise. This keeps a single work-commit carrying
		// the trailer, matching the claude posture (one commit, trailer-bearing).
		if err := amendHEADAddRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
			return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: amend: %w", err)
		}
		return codexRefsAmended, nil
	}

	// HEAD did not advance. Either codex edited files without committing
	// (dirty worktree → deterministic commit) or did nothing (clean → no_change).
	dirty, dirtyErr := codexWorktreeDirty(ctx, runner, wtPath)
	if dirtyErr != nil {
		return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: status: %w", dirtyErr)
	}
	if !dirty {
		// codex did no work. Do NOT fabricate a commit — let the caller route
		// this to the standard no_commit failure path.
		return codexRefsNoChange, nil
	}

	// codex edited but never committed: stage everything and create the commit.
	if err := commitAllWithRefsTrailer(ctx, runner, wtPath, beadID); err != nil {
		return codexRefsNoChange, fmt.Errorf("daemon: ensureCodexRefsTrailer: commit: %w", err)
	}
	return codexRefsCommitted, nil
}

// commitAllWithHarnessRefsTrailer stages every change in the worktree (tracked,
// untracked, deletions) and creates a commit with a harness-specific message
// prefix and the Refs: trailer. Shared by the codex and pi harness fallbacks so
// the runner-routing logic (PI-031 / NFR7) has one authoritative copy.
//
// When runner is non-nil the git commands are routed through it (remote worker);
// when nil they fall back to bare local exec (NFR7 — byte-identical for local).
func commitAllWithHarnessRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID, msgPrefix string) error {
	msg := fmt.Sprintf("%s\n\n%s", msgPrefix, codexRefsTrailerLine(beadID))
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

// commitAllWithRefsTrailer is the codex harness wrapper around
// commitAllWithHarnessRefsTrailer with the codex-specific fallback message.
func commitAllWithRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	return commitAllWithHarnessRefsTrailer(ctx, runner, wtPath, beadID,
		"feat(codex): codex turn output (auto-committed by daemon fallback)")
}

// amendHEADAddRefsTrailer appends the Refs: trailer to the existing HEAD commit
// message without changing its tree (the edits are already committed).
//
// It reads the current HEAD message, appends a blank line + the trailer if the
// trailer is not already present, and `git commit --amend`s with the new
// message. No files are staged, so the tree is preserved exactly.
//
// When runner is non-nil the git commands are routed through it (remote worker);
// when nil they fall back to bare local exec (NFR7 — byte-identical for local).
func amendHEADAddRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
	// Read the existing HEAD commit message body.
	var out []byte
	var err error
	if runner != nil {
		out, err = runner.Command(ctx, "git", "-C", wtPath, "log", "-1", "--format=%B", "HEAD").Output()
	} else {
		logCmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%B", "HEAD")
		logCmd.Dir = wtPath
		out, err = logCmd.Output()
	}
	if err != nil {
		return fmt.Errorf("git log HEAD: %w", err)
	}
	existing := strings.TrimRight(string(out), "\n")

	trailer := codexRefsTrailerLine(beadID)
	// Defensive: if the exact trailer line is already present, amend is a no-op
	// on the message (still re-commit to keep the call deterministic, but avoid
	// duplicating the trailer).
	newMsg := existing
	if !containsExactLine(existing, trailer) {
		newMsg = existing + "\n\n" + trailer
	}

	if runner != nil {
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "commit", "--amend", "-m", newMsg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit --amend: %w\ngit output: %s", err, out)
		}
		return nil
	}
	amendCmd := exec.CommandContext(ctx, "git", "commit", "--amend", "-m", newMsg)
	amendCmd.Dir = wtPath
	if out, err := amendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit --amend: %w\ngit output: %s", err, out)
	}
	return nil
}

// containsExactLine reports whether body contains line as an exact line
// (line-for-line, CR-tolerant), matching beadAlreadySubsumedInMain semantics.
func containsExactLine(body, line string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimRight(l, "\r") == line {
			return true
		}
	}
	return false
}
