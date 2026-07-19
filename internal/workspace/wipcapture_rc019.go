package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// wipcapture_rc019.go — CaptureWIP: captures recoverable work-in-progress
// from a run's worktree before the investigator emits a reopen-bead verdict.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.
//
// RC-019 requires the investigator to:
//   (a) run `git status --porcelain` and enumerate untracked files;
//   (b) capture a diff plus file listing;
//   (c) include the capture in the reconciliation commit's body and/or as
//       annotated files under .harmonik/reconciliation/<investigator_run_id>/wip-capture/.
//
// This obligation is MANDATORY for reopen-bead verdicts.

// CaptureWIP captures the recoverable work-in-progress from the worktree at
// worktreePath. It runs `git status --porcelain` and `git diff HEAD` inside
// the worktree and returns a populated core.WIPCapture.
//
// CaptureWIP never returns an error for empty output (clean worktree); it
// returns an error only when a git subprocess fails unexpectedly (e.g., git not
// found, worktreePath is not a git repository).
//
// The caller is responsible for writing the returned WIPCapture to disk
// (WriteWIPCapture) and including it in the verdict commit.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.
func CaptureWIP(worktreePath string) (core.WIPCapture, error) {
	status, err := runGitStatus(worktreePath)
	if err != nil {
		return core.WIPCapture{}, fmt.Errorf("workspace: CaptureWIP: git status: %w", err)
	}

	diff, err := runGitDiff(worktreePath)
	if err != nil {
		return core.WIPCapture{}, fmt.Errorf("workspace: CaptureWIP: git diff: %w", err)
	}

	// Untracked-file extraction uses -z output: the human porcelain format
	// quotes/escapes special paths and renders renames as "new -> orig", both
	// of which would corrupt the extracted path list.
	statusZ, err := runGitStatusZ(worktreePath)
	if err != nil {
		return core.WIPCapture{}, fmt.Errorf("workspace: CaptureWIP: git status -z: %w", err)
	}
	untracked := extractUntrackedFiles(statusZ)

	return core.WIPCapture{
		WorktreePath:       worktreePath,
		GitStatusPorcelain: status,
		GitDiff:            diff,
		UntrackedFiles:     untracked,
	}, nil
}

// WriteWIPCapture writes the WIPCapture to the three canonical files under
// destDir (.harmonik/reconciliation/<investigator_run_id>/wip-capture/).
//
// destDir must already exist; WriteWIPCapture does not create it. Files are
// written unconditionally (overwrite if present).
//
// Spec ref: reconciliation/spec.md §4.4 RC-019.
func WriteWIPCapture(capture core.WIPCapture, destDir string) error {
	if err := writeFileIfNonEmpty(destDir, core.WIPCaptureStatusFile, capture.GitStatusPorcelain); err != nil {
		return fmt.Errorf("workspace: WriteWIPCapture: status file: %w", err)
	}
	if err := writeFileIfNonEmpty(destDir, core.WIPCaptureDiffFile, capture.GitDiff); err != nil {
		return fmt.Errorf("workspace: WriteWIPCapture: diff file: %w", err)
	}
	if len(capture.UntrackedFiles) > 0 {
		content := strings.Join(capture.UntrackedFiles, "\n") + "\n"
		if err := writeFileIfNonEmpty(destDir, core.WIPCaptureUntrackedFile, content); err != nil {
			return fmt.Errorf("workspace: WriteWIPCapture: untracked file: %w", err)
		}
	}
	return nil
}

// runGitStatus runs `git status --porcelain` in dir and returns the output.
func runGitStatus(dir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain") //nolint:noctx // called from non-context path
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status --porcelain: %w", err)
	}
	return string(out), nil
}

// runGitDiff runs `git diff HEAD` in dir and returns the output. An empty
// string is returned when the worktree is clean relative to HEAD.
func runGitDiff(dir string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD") //nolint:noctx // called from non-context path
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff HEAD: %w", err)
	}
	return string(out), nil
}

// writeFileIfNonEmpty writes content to filepath.Join(dir, name) when content
// is non-empty. It is a no-op when content is empty (avoids cluttering the
// wip-capture directory with empty placeholder files).
func writeFileIfNonEmpty(dir, name, content string) error {
	if content == "" {
		return nil
	}
	//nolint:gosec // G306: 0644 is the standard mode for text evidence files.
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

// runGitStatusZ runs `git status --porcelain -z` in dir and returns the
// NUL-separated output. Unlike the newline format, -z never quotes or escapes
// paths, so paths with spaces, quotes, or newlines round-trip exactly.
func runGitStatusZ(dir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain", "-z") //nolint:noctx // called from non-context path
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status --porcelain -z: %w", err)
	}
	return string(out), nil
}

// extractUntrackedFiles parses `git status --porcelain -z` output and returns
// the paths of all untracked files ("?? " entries). Entries are NUL-separated;
// rename/copy entries (X or Y of R/C) carry an extra NUL-terminated original
// path, which is skipped so it is never misread as a status entry.
func extractUntrackedFiles(porcelainZOutput string) []string {
	if porcelainZOutput == "" {
		return nil
	}
	entries := strings.Split(porcelainZOutput, "\x00")
	var untracked []string
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 3 {
			continue
		}
		xy, path := entry[:2], entry[3:]
		if xy == "??" && path != "" {
			untracked = append(untracked, path)
			continue
		}
		// Rename/copy entries are followed by the original path as a separate
		// NUL-terminated field; skip it.
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++
		}
	}
	return untracked
}
