package shared

// refstrailer.go — the Refs:<bead> commit-trailer machinery shared by the codex
// and pi harnesses (codex-harness C2/T9, hk-bpxci; pi PI-030/031, hk-mazln).
//
// # Why this is shared and not codex-private
//
// harmonik detects bead completion by a git commit on the worktree HEAD that
// carries a "Refs: <bead-id>" trailer (workloop.go beadAlreadySubsumedInMain /
// noCommitGuardShouldReopen; both line-match "Refs: <id>" exactly). The claude
// harness runs as an interactive TUI, so a commit without the trailer is caught
// by the reviewer/no-commit guard and the bead is re-driven. codex and pi are
// DIFFERENT: both are one-shot run-to-exit processes — there is no live REPL to
// re-prod and no second chance inside the same turn. Both therefore need a
// DETERMINISTIC commit-after-exit fallback: if the harness edited files but did
// not produce a trailer-carrying commit, the daemon creates/repairs the commit
// itself so the standard trailer-detection path succeeds.
//
// The VERIFY and FALLBACK primitives below are byte-for-byte the same for both
// harnesses — picommit.go's piRefsOutcome was literally a type alias of the
// codex enum — so they live here, BELOW both harness implementations. Only the
// harness-specific wrappers (ensureCodexRefsTrailer / ensurePiRefsTrailer and
// their fallback commit messages) stay with their harness.
//
// LOAD-BEARING (PI-031 / NFR7): every git operation routes through the run's
// tmux.CommandRunner when non-nil and falls back to bare local exec when nil, so
// the remote SSH substrate behaves identically to local.
//
// Spec: specs/harness-contract.md §2 N2 (CompletionProcessExit);
// specs/pi-harness.md §3 (PI-030/PI-031). The trailer contract is workloop.go
// beadAlreadySubsumedInMain.
//
// Origin: internal/daemon/codexcommit.go, split out by
// plans/2026-07-21-p2-extraction/E1a-codex-harness.md unit E1a-0.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// RefsOutcome classifies what a harness's ensure-refs-trailer fallback did so
// the caller can route the run (success → merge path; RefsNoChange → no_commit
// failure path).
type RefsOutcome int

const (
	// RefsAlreadyPresent means HEAD already carried the trailer; no action was
	// taken.
	RefsAlreadyPresent RefsOutcome = iota

	// RefsAmended means an existing turn commit lacked the trailer and was
	// amended to append it.
	RefsAmended

	// RefsCommitted means the harness edited files but produced no commit, so
	// the fallback staged the changes and created a trailer-carrying commit.
	RefsCommitted

	// RefsNoChange means HEAD did not advance past parent and the worktree is
	// clean — the harness did no work. The caller routes this to the standard
	// no_commit failure path; the fallback deliberately does NOT fabricate a
	// commit so a genuinely-idle turn stays detectable.
	RefsNoChange
)

// String renders the outcome for log/event diagnostics.
func (o RefsOutcome) String() string {
	switch o {
	case RefsAlreadyPresent:
		return "already_present"
	case RefsAmended:
		return "amended"
	case RefsCommitted:
		return "committed"
	case RefsNoChange:
		return "no_change"
	default:
		// Deliberately still says "codexRefsOutcome": this is observable
		// diagnostic output and the P2 E1a-0 split is a pure move. Renaming it
		// is a behaviour change and belongs in a follow-up, not here.
		return fmt.Sprintf("codexRefsOutcome(%d)", int(o))
	}
}

// RefsTrailerLine returns the exact "Refs: <bead-id>" trailer line the
// daemon's commit-detection path (workloop.go beadAlreadySubsumedInMain)
// matches line-for-line. Centralised so the instruct/verify/fallback paths all
// agree on the exact text.
func RefsTrailerLine(beadID core.BeadID) string {
	return "Refs: " + string(beadID)
}

// WorktreeHEADHasRefsTrailer reports whether the worktree HEAD commit body
// carries an exact "Refs: <beadID>" trailer line.
//
// It uses the same line-exact comparison as beadAlreadySubsumedInMain so that
// "Refs: hk-foo.1" does NOT match a commit whose only trailer is
// "Refs: hk-foo.10". Returns (false, err) on any git error (e.g. no commits
// yet); the caller treats a git error as "trailer not present".
//
// When runner is non-nil the git command is routed through it (remote worker);
// when nil it falls back to bare local exec (NFR7 — byte-identical for local).
func WorktreeHEADHasRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) (bool, error) {
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
	needle := RefsTrailerLine(beadID)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimRight(line, "\r") == needle {
			return true, nil
		}
	}
	return false, nil
}

// WorktreeDirty reports whether the worktree at wtPath has any uncommitted
// changes — staged, unstaged, or untracked. Used by the fallback to decide
// whether the harness edited files without committing.
//
// Returns (false, err) on git error; the caller treats an error as "not dirty"
// only after also checking HEAD advancement, so a git failure cannot silently
// fabricate a commit.
//
// When runner is non-nil the git command is routed through it (remote worker);
// when nil it falls back to bare local exec (NFR7 — byte-identical for local).
func WorktreeDirty(ctx context.Context, runner tmux.CommandRunner, wtPath string) (bool, error) {
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

// CommitAllWithHarnessRefsTrailer stages every change in the worktree (tracked,
// untracked, deletions) and creates a commit with a harness-specific message
// prefix and the Refs: trailer. Shared by the codex and pi harness fallbacks so
// the runner-routing logic (PI-031 / NFR7) has one authoritative copy.
//
// When runner is non-nil the git commands are routed through it (remote worker);
// when nil they fall back to bare local exec (NFR7 — byte-identical for local).
func CommitAllWithHarnessRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID, msgPrefix string) error {
	msg := fmt.Sprintf("%s\n\n%s", msgPrefix, RefsTrailerLine(beadID))
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
	//nolint:gosec // G204: fixed git argv; msg is daemon-built from a caller-supplied
	// prefix and the bead ID, passed as a single -m argument (no shell).
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	commitCmd.Dir = wtPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\ngit output: %s", err, out)
	}
	return nil
}

// AmendHEADAddRefsTrailer appends the Refs: trailer to the existing HEAD commit
// message without changing its tree (the edits are already committed).
//
// It reads the current HEAD message, appends a blank line + the trailer if the
// trailer is not already present, and `git commit --amend`s with the new
// message. No files are staged, so the tree is preserved exactly.
//
// When runner is non-nil the git commands are routed through it (remote worker);
// when nil they fall back to bare local exec (NFR7 — byte-identical for local).
func AmendHEADAddRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID) error {
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

	trailer := RefsTrailerLine(beadID)
	// Defensive: if the exact trailer line is already present, amend is a no-op
	// on the message (still re-commit to keep the call deterministic, but avoid
	// duplicating the trailer).
	newMsg := existing
	if !ContainsExactLine(existing, trailer) {
		newMsg = existing + "\n\n" + trailer
	}

	if runner != nil {
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "commit", "--amend", "-m", newMsg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit --amend: %w\ngit output: %s", err, out)
		}
		return nil
	}
	//nolint:gosec // G204: fixed git argv; newMsg is the existing HEAD message plus
	// the Refs trailer, passed as a single -m argument (no shell).
	amendCmd := exec.CommandContext(ctx, "git", "commit", "--amend", "-m", newMsg)
	amendCmd.Dir = wtPath
	if out, err := amendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit --amend: %w\ngit output: %s", err, out)
	}
	return nil
}

// ContainsExactLine reports whether body contains line as an exact line
// (line-for-line, CR-tolerant), matching beadAlreadySubsumedInMain semantics.
func ContainsExactLine(body, line string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimRight(l, "\r") == line {
			return true
		}
	}
	return false
}
