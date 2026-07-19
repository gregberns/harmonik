package workspace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MergeNodeKind is the typed discriminator for the two merge-node shapes
// declared by WM-018a. Every workflow that produces a merge-back MUST declare
// its merge step as exactly one of these shapes.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "Every workflow that produces
// a merge-back MUST declare the merge step as a workflow node of one of two
// shapes: (a) a non-agentic merge node dispatched directly by the
// orchestrator … (b) an agentic merge node dispatched through
// [handler-contract.md §4.1]."
type MergeNodeKind string

const (
	// MergeNodeKindNonAgentic is the merge-node shape where the orchestrator
	// executes `git merge --squash` + commit directly, with no handler
	// subprocess. Author and committer are both set to the daemon identity.
	//
	// Spec ref: workspace-model.md §4.5 WM-018a clause (a).
	MergeNodeKindNonAgentic MergeNodeKind = "non-agentic"

	// MergeNodeKindAgentic is the merge-node shape where a handler subprocess
	// executes the merge operations plus any pre-merge validation per its
	// LaunchSpec. Author = implementer handler identity; committer = daemon
	// identity.
	//
	// Spec ref: workspace-model.md §4.5 WM-018a clause (b).
	MergeNodeKindAgentic MergeNodeKind = "agentic"
)

// Valid reports whether k is a declared MergeNodeKind constant.
func (k MergeNodeKind) Valid() bool {
	switch k {
	case MergeNodeKindNonAgentic, MergeNodeKindAgentic:
		return true
	}
	return false
}

// ErrUnknownMergeNodeKind is returned when an unrecognised MergeNodeKind is
// encountered.
var ErrUnknownMergeNodeKind = errors.New("workspace: unknown MergeNodeKind")

// MergeIdentity holds the git author/committer name and email fields for a
// squash-merge commit per WM-019.
//
// For non-agentic merges, Author and Committer are both set to the daemon
// identity. For agentic merges, Author is the implementer-handler identity
// (from LaunchSpec) and Committer is the daemon identity.
//
// Spec ref: workspace-model.md §4.5 WM-019; WM-018a — "author/committer split."
type MergeIdentity struct {
	// Name is the git author or committer name.
	Name string
	// Email is the git author or committer email.
	Email string
}

// MergeNodeDispatch holds the parameters that identify how a merge-back node
// is dispatched. Callers construct this from the workflow definition and pass
// it to SquashMergeConfig or equivalent merge-execution helpers.
//
// Spec ref: workspace-model.md §4.5 WM-018a.
type MergeNodeDispatch struct {
	// Kind declares which of the two WM-018a shapes this merge node takes.
	Kind MergeNodeKind

	// Author is the git author identity for the squash-merge commit.
	// For non-agentic merges this equals Committer (daemon identity).
	// For agentic merges this is the implementer-handler identity.
	Author MergeIdentity

	// Committer is always the daemon identity per WM-019. Both non-agentic
	// and agentic shapes use the same committer.
	Committer MergeIdentity
}

// Valid reports whether d is a well-formed MergeNodeDispatch.
//
// Rules:
//   - Kind must be a declared MergeNodeKind constant.
//   - Author.Name and Author.Email must be non-empty.
//   - Committer.Name and Committer.Email must be non-empty.
func (d MergeNodeDispatch) Valid() bool {
	if !d.Kind.Valid() {
		return false
	}
	if d.Author.Name == "" || d.Author.Email == "" {
		return false
	}
	if d.Committer.Name == "" || d.Committer.Email == "" {
		return false
	}
	return true
}

// ConflictDetectionResult holds the outcome of mechanical conflict detection
// for a squash-merge attempt.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "Conflict detection is
// mechanical: a non-zero exit from `git merge --squash` or the presence of
// conflict markers in `git status --porcelain` output MUST be treated as
// conflict entry per WM-020."
type ConflictDetectionResult struct {
	// HasConflict is true when conflict was detected.
	HasConflict bool
	// Reason describes the conflict source ("merge-exit-nonzero" or
	// "porcelain-conflict-marker").
	Reason string
}

// DetectSquashMergeConflict runs `git merge --squash --strategy=ort <branch>`
// in workDir and then `git status --porcelain` to perform mechanical conflict
// detection per WM-018a.
//
// A non-zero exit from `git merge --squash` indicates a conflict. If the merge
// command succeeds, `git status --porcelain` is checked for unmerged paths
// (lines starting with "U" or "AA"/"DD" etc. — porcelain conflict markers).
//
// The returned ConflictDetectionResult carries HasConflict=true and a Reason
// string on conflict. It returns an I/O or subprocess error only for unexpected
// failures (e.g., git not found); the "merge failed" case is encoded in
// ConflictDetectionResult.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "Conflict detection is
// mechanical: a non-zero exit from `git merge --squash` OR the presence of
// conflict markers in `git status --porcelain` output MUST be treated as
// conflict entry per WM-020."
// DetectSquashMergeConflict is a detection PROBE only: whatever the outcome
// (conflict, clean, or error), it resets the worktree and index back to HEAD
// before returning, so it has no side effects. Without the reset, a conflicting
// `--squash` merge (which sets no MERGE_HEAD) would leave conflict markers and
// a half-staged index behind, and a clean trial merge would leave a staged
// squash that a later real merge double-applies.
func DetectSquashMergeConflict(workDir, taskBranch string) (result ConflictDetectionResult, retErr error) {
	mergeCmd := exec.Command("git", "merge", "--squash", "--strategy=ort", taskBranch) //nolint:noctx // called from non-context path; caller responsible for lifecycle
	mergeCmd.Dir = workDir
	mergeErr := mergeCmd.Run()

	// Always undo the trial merge's mutations (staged squash on success,
	// conflict markers + half-staged index on conflict) before returning.
	defer func() {
		if err := resetSquashProbe(workDir); err != nil && retErr == nil {
			retErr = err
			result = ConflictDetectionResult{}
		}
	}()

	if mergeErr != nil {
		// Non-zero exit from git merge --squash → conflict per WM-018a.
		return ConflictDetectionResult{
			HasConflict: true,
			Reason:      "merge-exit-nonzero",
		}, nil
	}

	// Merge succeeded; check porcelain for conflict markers.
	statusCmd := exec.Command("git", "status", "--porcelain") //nolint:noctx // called from non-context path
	statusCmd.Dir = workDir
	out, err := statusCmd.Output()
	if err != nil {
		return ConflictDetectionResult{}, fmt.Errorf("workspace: DetectSquashMergeConflict: git status --porcelain: %w", err)
	}

	for _, line := range porcelainLines(string(out)) {
		if len(line) >= 2 && isConflictMarker(line[:2]) {
			return ConflictDetectionResult{
				HasConflict: true,
				Reason:      "porcelain-conflict-marker",
			}, nil
		}
	}

	return ConflictDetectionResult{HasConflict: false}, nil
}

// resetSquashProbe restores the worktree at workDir to a clean state after a
// trial `git merge --squash`. `git reset --hard HEAD` discards the staged
// squash / conflicted index and working-tree changes (files the trial merge
// added to the index are removed from the working tree too, since they are
// tracked in the index but absent from HEAD). A squash merge sets no
// MERGE_HEAD, so `git merge --abort` is not applicable; the leftover
// SQUASH_MSG/MERGE_MSG scratch files are removed best-effort.
func resetSquashProbe(workDir string) error {
	resetCmd := exec.Command("git", "reset", "--hard", "HEAD") //nolint:noctx // called from non-context path
	resetCmd.Dir = workDir
	if out, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("workspace: DetectSquashMergeConflict: git reset --hard after squash probe: %w (output: %s)", err, out)
	}
	// Best-effort: clear the squash scratch files so a later unrelated commit
	// does not inherit the trial merge's prepared message.
	gitDirCmd := exec.Command("git", "rev-parse", "--absolute-git-dir") //nolint:noctx // called from non-context path
	gitDirCmd.Dir = workDir
	if out, err := gitDirCmd.Output(); err == nil {
		gitDir := string(out)
		for gitDir != "" && (gitDir[len(gitDir)-1] == '\n' || gitDir[len(gitDir)-1] == '\r') {
			gitDir = gitDir[:len(gitDir)-1]
		}
		_ = os.Remove(filepath.Join(gitDir, "SQUASH_MSG")) //nolint:errcheck // best-effort scratch-file cleanup; absence is fine
		_ = os.Remove(filepath.Join(gitDir, "MERGE_MSG"))  //nolint:errcheck // best-effort scratch-file cleanup; absence is fine
	}
	return nil
}

// porcelainLines splits git status --porcelain output into individual lines.
func porcelainLines(out string) []string {
	if out == "" {
		return nil
	}
	var lines []string
	start := 0
	for i, c := range out {
		if c == '\n' {
			if i > start {
				lines = append(lines, out[start:i])
			}
			start = i + 1
		}
	}
	if start < len(out) {
		lines = append(lines, out[start:])
	}
	return lines
}

// isConflictMarker returns true for git porcelain status codes that indicate
// an unmerged or conflict state. In git's porcelain v1 format, the first two
// characters are the XY status code:
//
//   - 'U' in X or Y position indicates unmerged
//   - "AA" (both added), "DD" (both deleted) are conflict states
//
// Spec ref: git-status(1) porcelain format; WM-018a conflict detection.
func isConflictMarker(xy string) bool {
	if len(xy) < 2 {
		return false
	}
	x, y := xy[0], xy[1]
	if x == 'U' || y == 'U' {
		return true
	}
	// Both added (AA) or both deleted (DD) are also conflict states.
	if (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
		return true
	}
	return false
}
