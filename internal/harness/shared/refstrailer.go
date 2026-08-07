package shared

// refstrailer.go — the Refs:<bead> commit-trailer machinery shared by the codex
// and pi harnesses (codex-harness C2/T9, hk-bpxci; pi PI-030/031, hk-mazln).
//
// # Why this is shared and not codex-private
//
// harmonik detects that a harness turn produced work by a git commit on the
// worktree HEAD that carries a "Refs: <bead-id>" trailer
// (WorktreeHEADHasRefsTrailer below, line-matching "Refs: <id>" exactly). That
// is a claim about THIS TURN, on THIS worktree HEAD — it is not evidence that
// the bead's work merged anywhere (hk-1a7yb; see the note where
// MainHistoryHasRefsTrailer used to be). The claude
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
// specs/pi-harness.md §3 (PI-030/PI-031). The trailer TEXT has one definition
// here (RefsTrailerLine), so the instruct, verify and fallback paths cannot
// drift apart.
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

// RefsTrailerLine returns the exact "Refs: <bead-id>" trailer line the daemon's
// commit-detection path (WorktreeHEADHasRefsTrailer, below) matches
// line-for-line. Centralised so the instruct/verify/fallback paths all agree on
// the exact text.
func RefsTrailerLine(beadID core.BeadID) string {
	return "Refs: " + string(beadID)
}

// WorktreeHEADHasRefsTrailer reports whether the worktree HEAD commit body
// carries an exact "Refs: <beadID>" trailer line.
//
// It uses the line-exact comparison of ContainsExactLine so that
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

// The agent-work pathspec — `-- . :(exclude).claude :(exclude).harmonik` —
// restricts a worktree scan to work the AGENT produced, excluding the daemon's
// own scaffolding.
//
// hk-jcrzn: `.harmonik/` holds files the DAEMON writes into the run worktree —
// agent-task.md, reviewer-feedback.iter-N.md, commit-gate.log, review.json. In a
// project whose .gitignore does not cover them (init scaffolds an ENUMERATED
// .harmonik/.gitignore that omits agent-task.md, and a gitignore inside
// .harmonik/ cannot un-untrack its own directory), a bare `git status
// --porcelain` reports `?? .harmonik/` for a run in which the agent did nothing.
// The fallback then read that as "edited but never committed", staged the
// daemon's own files, and produced a commit with a changed tree and a valid
// Refs trailer — which commit_gate passes legitimately, because by every check
// the gate makes it IS a real commit. An idle implementer looked like a success.
//
// `.claude/` is excluded for the reason given at commitResidualDelta (hk-igq3):
// it is only partially gitignored, so a blanket -A can stage
// credential-adjacent files and push them to origin.
//
// This mirrors the exclusions commitResidualDelta has carried since GH #7 /
// hk-znou. That hardening was applied to one committer and not to this one;
// these two helpers were the unhardened twin. The `:(exclude)` pathspec magic is
// honored by git >= 2.0 and matches the directory and all descendants.
//
// The pathspec is written out literally at each call site rather than shared
// through a slice variable, matching commitResidualDelta: the args stay a fixed
// literal list, which is both the house idiom and what keeps these calls clear
// of gosec G204 without a suppression.

// WorktreeDirty reports whether the worktree at wtPath has any uncommitted
// AGENT changes — staged, unstaged, or untracked — ignoring daemon-owned paths
// (see the agent-work pathspec note above). Used by the fallback to decide
// whether the harness edited files without committing.
//
// The question this asks is deliberately "did the AGENT change anything", not
// "is anything different": the daemon writing its own task file into the
// worktree must never count as the agent having done work (hk-jcrzn).
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
		out, err = runner.Command(ctx, "git", "-C", wtPath, "status", "--porcelain", "--",
			".", ":(exclude).claude", ":(exclude).harmonik").Output()
	} else {
		cmd := exec.CommandContext(ctx, "git", "status", "--porcelain", "--",
			".", ":(exclude).claude", ":(exclude).harmonik")
		cmd.Dir = wtPath
		out, err = cmd.Output()
	}
	if err != nil {
		return false, fmt.Errorf("daemon: codexWorktreeDirty: git status in %q: %w", wtPath, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// CommitAllWithHarnessRefsTrailer stages every AGENT change in the worktree
// (tracked, untracked, deletions) and creates a commit with a harness-specific
// message prefix and the Refs: trailer. Shared by the codex and pi harness
// fallbacks so the runner-routing logic (PI-031 / NFR7) has one authoritative
// copy.
//
// Daemon-owned paths are excluded from staging (see the agent-work pathspec note
// above), so this can never commit the daemon's own scaffolding as the agent's
// work (hk-jcrzn) nor sweep credential-adjacent .claude/ files onto a run branch
// (hk-igq3). The exclusions match commitResidualDelta's (GH #7, hk-znou).
//
// When runner is non-nil the git commands are routed through it (remote worker);
// when nil they fall back to bare local exec (NFR7 — byte-identical for local).
func CommitAllWithHarnessRefsTrailer(ctx context.Context, runner tmux.CommandRunner, wtPath string, beadID core.BeadID, msgPrefix string) error {
	msg := fmt.Sprintf("%s\n\n%s", msgPrefix, RefsTrailerLine(beadID))
	if runner != nil {
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "add", "-A", "--",
			".", ":(exclude).claude", ":(exclude).harmonik").CombinedOutput(); err != nil {
			return fmt.Errorf("git add -A: %w\ngit output: %s", err, out)
		}
		if out, err := runner.Command(ctx, "git", "-C", wtPath, "commit", "-m", msg).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit: %w\ngit output: %s", err, out)
		}
		return nil
	}
	addCmd := exec.CommandContext(ctx, "git", "add", "-A", "--",
		".", ":(exclude).claude", ":(exclude).harmonik")
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

// MainHistoryHasRefsTrailer is DELETED (hk-1a7yb). It reported whether beadID
// appeared as an exact "Refs: <id>" line in any commit on the literal branch
// `main`, and the daemon read that as "a prior run already completed this bead"
// — the subsumption close on the graph path, and the stale-blocker close on the
// claim-failure path.
//
// Its own doc comment had forbidden that use since hk-f38n: "a match proves a
// commit NAMES the bead. It does not prove the bead is done ... never use this
// as a standalone completion test." The comment did not stop it. Bead hk-2hfyt,
// a P1 fleet-down bug, closed as done because a whole-repo gofumpt run carried
// the line "Refs: hk-2hfyt"; the fix never landed. The branch was also wrong:
// this program merges to an integration branch, and `main` held none of its
// work.
//
// The daemon now asks internal/daemon.beadWorkLandedOn, which takes the branch
// from the run instead of a literal and requires the BI-022 evidence — a
// `Harmonik-Bead-ID` trailer the daemon itself writes when it lands a task
// branch, a non-docs diff, the commit still reachable from the branch tip, and
// no later revert. See internal/daemon/subsumptionevidence.go.
//
// Do not restore a mention-only probe here. If a caller needs "did this bead's
// work merge", it needs that evidence, and one copy of it already exists in
// lifecycle.GitMergeCommitScanner.

// ContainsExactLine reports whether body contains line as an exact line
// (line-for-line, CR-tolerant). WorktreeHEADHasRefsTrailer uses the same
// comparison, so "Refs: hk-foo.1" never matches "Refs: hk-foo.10".
func ContainsExactLine(body, line string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimRight(l, "\r") == line {
			return true
		}
	}
	return false
}
