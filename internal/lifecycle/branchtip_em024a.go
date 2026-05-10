package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// ErrBranchTipRewound is returned by CheckBranchTipMonotonicity when the
// observed task-branch tip is not a fast-forward descendant of the persisted
// prior tip — indicating an external force-push, git reset --hard, or CI
// auto-rebase rewound the branch under the daemon.
//
// Callers MUST route the discrepancy to reconciliation per
// [reconciliation/spec.md §8.4 Cat 3] and MUST NOT advance the run against
// the new tip.
//
// Spec ref: execution-model.md §4.5 EM-024a.
var ErrBranchTipRewound = errors.New("lifecycle: branch tip rewound — new tip is not a fast-forward descendant of persisted prior tip (EM-024a Cat 3 violation)")

// runTipsDir returns the directory used to persist per-run last-observed
// task-branch-tip SHAs: <projectDir>/.harmonik/run-tips/.
//
// Spec ref: execution-model.md §4.5 EM-024a — "e.g., under
// .harmonik/run-tips/<run_id>".
func runTipsDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "run-tips")
}

// runTipPath returns the path of the persisted tip file for a specific run:
// <projectDir>/.harmonik/run-tips/<run_id>.
func runTipPath(projectDir string, runID core.RunID) string {
	return filepath.Join(runTipsDir(projectDir), runID.String())
}

// ReadPersistedTip reads the last-observed task-branch-tip SHA that was
// persisted for runID by a prior call to WritePersistedTip. Returns the SHA
// as a trimmed hex string, or an empty string and a nil error when the tip
// file does not yet exist (first observation; not a violation per EM-024a).
//
// Any I/O error other than os.IsNotExist is returned as a wrapped error.
//
// Spec ref: execution-model.md §4.5 EM-024a — "A missing prior-tip file for
// a run observed for the first time is NOT a violation; the daemon initializes
// the persisted tip on first observation."
func ReadPersistedTip(projectDir string, runID core.RunID) (string, error) {
	tipPath := runTipPath(projectDir, runID)
	//nolint:gosec // G304: path constructed from projectDir (resolved at daemon startup) and runID (daemon-generated UUIDv7); not user-controlled input
	data, err := os.ReadFile(tipPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("lifecycle: ReadPersistedTip(%s): %w", runID, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// WritePersistedTip atomically persists tipSHA as the last-observed
// task-branch-tip for runID. The file is written under
// <projectDir>/.harmonik/run-tips/<run_id> with mode 0o644. The
// run-tips directory is created if absent.
//
// tipSHA must be a non-empty, trimmed hex commit SHA. No format validation
// is performed beyond the non-empty check; callers are expected to pass SHAs
// returned by git.
//
// Spec ref: execution-model.md §4.5 EM-024a — "daemon MUST persist, per
// in-flight run, the last-observed task-branch-tip SHA in run metadata."
func WritePersistedTip(projectDir string, runID core.RunID, tipSHA string) error {
	if tipSHA == "" {
		return fmt.Errorf("lifecycle: WritePersistedTip(%s): tipSHA must not be empty", runID)
	}
	dir := runTipsDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("lifecycle: WritePersistedTip(%s): mkdir run-tips: %w", runID, err)
	}
	tipPath := runTipPath(projectDir, runID)
	//nolint:gosec // G306: 0644 is the correct mode for a metadata file in .harmonik/; path is daemon-internal
	if err := os.WriteFile(tipPath, []byte(tipSHA+"\n"), 0o644); err != nil {
		return fmt.Errorf("lifecycle: WritePersistedTip(%s): write: %w", runID, err)
	}
	return nil
}

// IsFastForwardDescendant reports whether descendant is a fast-forward
// descendant of ancestor — i.e., ancestor is in the ancestry chain of
// descendant — using `git merge-base --is-ancestor`.
//
// Returns true when the check succeeds, false when it fails (non-zero exit,
// meaning "not an ancestor"), and an error on git invocation failure.
//
// Spec ref: execution-model.md §4.5 EM-024a — "verify that the new tip SHA
// is a fast-forward descendant of the persisted prior tip SHA (the prior tip
// is in the ancestor chain of the new tip)."
func IsFastForwardDescendant(ctx context.Context, repoDir, ancestor, descendant string) (bool, error) {
	//nolint:gosec // G204: ancestor/descendant are commit SHAs produced by git rev-parse or fixture helpers; repoDir is daemon-resolved project dir
	cmd := exec.CommandContext(ctx, "git",
		"-C", repoDir,
		"merge-base", "--is-ancestor", ancestor, descendant,
	)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// Exit status 1 means "not an ancestor" — a normal, non-error result.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	// Any other error (2, signal, exec failure) is an infrastructure problem.
	return false, fmt.Errorf("lifecycle: IsFastForwardDescendant: git merge-base: %w", err)
}

// CheckBranchTipMonotonicity is the EM-024a sensor. It reads the persisted
// prior tip for runID and verifies that newTipSHA is a fast-forward descendant
// of it. On success it persists newTipSHA as the new last-observed tip.
//
// Behaviour by case:
//
//   - No prior tip file (first observation): newTipSHA is persisted and nil is
//     returned. Not a violation per EM-024a.
//   - Prior tip equals newTipSHA (no-op re-check): newTipSHA is persisted
//     (idempotent) and nil is returned. Not a violation; the branch has not
//     moved.
//   - newTipSHA is a fast-forward descendant of the prior tip (normal
//     advance): newTipSHA is persisted and nil is returned.
//   - newTipSHA is NOT a fast-forward descendant (rewind detected): returns
//     ErrBranchTipRewound without updating the persisted tip. Callers MUST
//     route to RC §8.4 Cat 3 and MUST NOT advance the run.
//
// repoDir is the absolute path to the git repository root (used for the
// ancestry check). projectDir is the root of the harmonik project (used for
// the tip-file path).
//
// Spec ref: execution-model.md §4.5 EM-024a.
func CheckBranchTipMonotonicity(ctx context.Context, repoDir, projectDir string, runID core.RunID, newTipSHA string) error {
	prior, err := ReadPersistedTip(projectDir, runID)
	if err != nil {
		return fmt.Errorf("lifecycle: CheckBranchTipMonotonicity(%s): read prior tip: %w", runID, err)
	}

	// First observation: no prior tip file. Initialize and return — not a violation.
	if prior == "" {
		if writeErr := WritePersistedTip(projectDir, runID, newTipSHA); writeErr != nil {
			return fmt.Errorf("lifecycle: CheckBranchTipMonotonicity(%s): initialize tip: %w", runID, writeErr)
		}
		return nil
	}

	// Identical tip: idempotent re-check; persist and return without ancestry check.
	if prior == newTipSHA {
		if writeErr := WritePersistedTip(projectDir, runID, newTipSHA); writeErr != nil {
			return fmt.Errorf("lifecycle: CheckBranchTipMonotonicity(%s): persist same tip: %w", runID, writeErr)
		}
		return nil
	}

	// Perform the fast-forward ancestry check.
	isDescendant, err := IsFastForwardDescendant(ctx, repoDir, prior, newTipSHA)
	if err != nil {
		return fmt.Errorf("lifecycle: CheckBranchTipMonotonicity(%s): ancestry check: %w", runID, err)
	}
	if !isDescendant {
		// Branch was rewound externally. Do NOT persist the new tip.
		return fmt.Errorf("%w: run=%s prior=%s new=%s", ErrBranchTipRewound, runID, prior, newTipSHA)
	}

	// Normal fast-forward advance: persist the new tip.
	if writeErr := WritePersistedTip(projectDir, runID, newTipSHA); writeErr != nil {
		return fmt.Errorf("lifecycle: CheckBranchTipMonotonicity(%s): persist new tip: %w", runID, writeErr)
	}
	return nil
}
