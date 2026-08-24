package runmerge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/gitprobe"
)

// RunContextDirPrefix is the directory prefix under .harmonik/ for run-context
// files. Full path: <worktree>/.harmonik/run-context/<run_id>/context.json
//
// The constant lives here rather than beside its CHB-023 writer because
// internal/runmerge may not import internal/daemon (P2 E5 RT13 deny edge) and a
// duplicated literal would silently fork the strip from the write.
const RunContextDirPrefix = ".harmonik/run-context"

// stripRunContextCommitMessage is the message StripRunContextFromMerge commits.
//
// It is named rather than written inline so the commit-message gate test can
// read the string PRODUCTION writes. The test used to hold its own copy, which
// meant the daemon could change how it spells the `Trivial: true` exemption and
// the test would keep passing against the old spelling.
const stripRunContextCommitMessage = "chore: strip run-context from merge (hk-4je)\n\n" +
	"Remove .harmonik/run-context/** that was force-committed by CHB-023 for\n" +
	"crash-recovery (EM-031). The files remain valid on the run-branch reflog;\n" +
	"they must not land on the merge target.\n" +
	"Trivial: true"

// StripRunContextFromMerge removes any .harmonik/run-context/** paths from the
// run-branch index and commits the removal, so they cannot land on the merge
// target via the subsequent fast-forward update-ref.
//
// The function is a no-op when:
//   - wtPath does not exist (worktree already removed, very rare edge case), or
//   - no .harmonik/run-context/** files are tracked in the worktree index.
//
// A stat failure that is NOT "does not exist" is an error, not a no-op: it
// leaves the index state unknown, and reporting stripped=false would let the
// caller fast-forward the target with run-context files still present.
//
// When a strip commit IS made, the run-branch HEAD advances by one commit and
// the caller MUST re-resolve runTip to pick up the new SHA.
//
// Returns stripped=true when the strip commit was created, false otherwise.
//
// Bead: hk-4je.
func StripRunContextFromMerge(ctx context.Context, wtPath string) (stripped bool, err error) {
	if _, statErr := os.Stat(wtPath); statErr != nil {
		if !errors.Is(statErr, fs.ErrNotExist) {
			return false, fmt.Errorf("daemon: StripRunContextFromMerge: stat worktree %s: %w", wtPath, statErr)
		}
		return false, nil
	}

	lsOut, lsErr := gitprobe.Output(ctx, wtPath, "ls-files", "--cached", "--", RunContextDirPrefix)
	if lsErr != nil {
		return false, fmt.Errorf("daemon: StripRunContextFromMerge: git ls-files --cached: %w", lsErr)
	}
	if strings.TrimSpace(string(lsOut)) == "" {
		return false, nil
	}

	// --ignore-unmatch makes this command mean the same thing every time it
	// runs, so gitprobe may run it again when the git process does not complete.
	if out, rmErr := gitprobe.CombinedOutput(ctx, wtPath, "rm", "--cached", "-r", "--ignore-unmatch", "--", RunContextDirPrefix); rmErr != nil {
		return false, fmt.Errorf("daemon: StripRunContextFromMerge: git rm --cached -r: %w\ngit output: %s", rmErr, out)
	}

	if out, commitErr := commitStrip(ctx, wtPath); commitErr != nil {
		return false, fmt.Errorf("daemon: StripRunContextFromMerge: git commit: %w\ngit output: %s", commitErr, out)
	}

	return true, nil
}

// commitStrip creates the strip commit. `git commit` is the one command on
// this path that does not mean the same thing twice, so it does not go through
// gitprobe's blind retry — see runCommitOnce for how a stopped commit is
// decided instead.
//
// Bead: hk-jbtj6.
func commitStrip(ctx context.Context, wtPath string) ([]byte, error) {
	return runCommitOnce(ctx, wtPath, func() ([]byte, error) {
		return commitStripOnce(ctx, wtPath)
	})
}

func commitStripOnce(ctx context.Context, wtPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", stripRunContextCommitMessage)
	cmd.Dir = wtPath
	return cmd.CombinedOutput()
}
