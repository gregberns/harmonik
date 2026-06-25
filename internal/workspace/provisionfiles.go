package workspace

// provisionfiles.go — ProvisionWorktreeFiles copies a configured set of
// gitignored-but-required files from the canonical project root into a freshly
// created run worktree (hk-z8u).
//
// # Motivation
//
// `git worktree add` checks out only TRACKED files. Files that are required at
// run time but deliberately gitignored — most commonly a `.env` consumed by a
// `docker compose --env-file ../.env` test gate — are NOT present in the fresh
// worktree. A gate that needs such a file then fails instantly ("couldn't find
// env file") and the bead fails before any real work runs. Observed in the
// omatic-system-plan project, where every dispatched bead deadlocked on a
// missing `.env`, forcing a serial workaround (one shared running stack).
//
// ProvisionWorktreeFiles is the opt-in daemon-side fix: after a worktree is
// created, copy each operator-configured path from the project root into the
// same relative path in the worktree, so isolated/concurrent runs can use gates
// that need those files.
//
// # Backward compatibility
//
// An empty paths slice is a no-op — the default for every project that has not
// opted in. Existing behaviour is unchanged.
//
// # Scope
//
// Local runs only. Remote (SSH-worker) worktrees live on another host and are
// not reachable through the local filesystem; callers skip provisioning for
// remote runs (a future bead may stream these files over the SSHRunner).

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ProvisionWorktreeFiles copies each path in relPaths from projectRoot into
// worktreePath, preserving the relative path and creating parent directories as
// needed.
//
// Each entry in relPaths is interpreted relative to BOTH projectRoot (source)
// and worktreePath (destination). Absolute paths and paths that escape the
// worktree (via "..") are rejected — provisioning must never write outside the
// worktree.
//
// A source file that does not exist is skipped with a warning (it is an
// OPTIONAL provisioning input — a project may configure a file that only some
// developers have). A genuine copy error (permission denied, source is a
// directory, destination unwritable) IS returned so the caller can surface it.
//
// relPaths empty or nil → immediate no-op, nil error (the backward-compatible
// default).
func ProvisionWorktreeFiles(projectRoot, worktreePath string, relPaths []string) error {
	if len(relPaths) == 0 {
		return nil
	}

	for _, rel := range relPaths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) {
			return fmt.Errorf("workspace: ProvisionWorktreeFiles: configured path %q is absolute; must be relative to the project root", rel)
		}

		clean := filepath.Clean(rel)
		// Reject any path that escapes the worktree root.
		if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("workspace: ProvisionWorktreeFiles: configured path %q escapes the worktree root", rel)
		}

		src := filepath.Join(projectRoot, clean)
		dst := filepath.Join(worktreePath, clean)

		info, statErr := os.Stat(src)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				// Optional input: warn and skip rather than fail the run.
				slog.Warn("workspace: ProvisionWorktreeFiles: configured source file not found; skipping",
					"path", clean, "source", src)
				continue
			}
			return fmt.Errorf("workspace: ProvisionWorktreeFiles: stat %q: %w", src, statErr)
		}
		if info.IsDir() {
			return fmt.Errorf("workspace: ProvisionWorktreeFiles: configured path %q is a directory; only regular files are supported", clean)
		}

		if err := copyFilePreservingMode(src, dst, info); err != nil {
			return fmt.Errorf("workspace: ProvisionWorktreeFiles: copy %q -> %q: %w", src, dst, err)
		}
	}

	return nil
}

// copyFilePreservingMode copies the regular file at src to dst, creating dst's
// parent directories (0o755) and applying the source file's permission bits.
func copyFilePreservingMode(src, dst string, srcInfo os.FileInfo) error {
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	//nolint:gosec // G304: src is projectRoot + an operator-configured relative path
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	//nolint:gosec // G304: dst is worktreePath + the same operator-configured relative path
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Re-apply mode in case the file pre-existed with different bits (O_CREATE
	// does not chmod an existing file).
	return os.Chmod(dst, srcInfo.Mode().Perm())
}
