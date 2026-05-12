package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
)

// ComputeDiffHash returns the SHA-256 hex digest of the output of
// `git diff <parentSHA>..<headSHA>` run in worktreePath.
//
// This is the v1 no-progress detector described in execution-model.md
// §4.3.EM-015e: before each reviewer launch from iteration 2 onward, the
// daemon computes this hash and compares it to Run.context.last_diff_hash from
// the prior iteration. An identical hash means the implementer produced a
// bit-identical diff against the parent — i.e., no progress was made.
//
// The function is intentionally pure over git output: it runs one subprocess
// and hashes its stdout. Empty diff output (no changes relative to parent) is
// not an error; it returns the SHA-256 of an empty byte slice.
//
// Spec ref: execution-model.md §4.3.EM-015e — "the SHA-256 hash of
// `git diff <parent>..<head>` output on the run's task branch".
func ComputeDiffHash(ctx context.Context, worktreePath, parentSHA, headSHA string) (string, error) {
	rangeArg := parentSHA + ".." + headSHA
	cmd := exec.CommandContext(ctx, "git", "diff", rangeArg)
	cmd.Dir = worktreePath

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("workspace: ComputeDiffHash: git diff %s: %w", rangeArg, err)
	}

	sum := sha256.Sum256(out)
	return fmt.Sprintf("%x", sum), nil
}
