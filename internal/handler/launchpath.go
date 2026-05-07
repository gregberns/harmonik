// Package handler — launch-path resolution discipline (HC-042).
//
// This file provides named, requirement-traceable sensors for the
// handler-launch path-resolution discipline defined in
// specs/handler-contract.md HC-042.  It is intentionally a pure helper:
// no filesystem I/O, no process management.  All policy is captured here so
// that the actual launch site (a future bead) can delegate entirely to
// ResolveLaunchPath and trust that HC-042 is satisfied.
package handler

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrLaunchPathMissing is returned by ResolveLaunchPath when binaryRef is
// empty and systemHandler is false (or when binaryRef is empty for any
// handler).  Per HC-042, a handler MUST fail launch if the configured
// absolute path is absent; an empty reference is the clearest form of
// "absent".
//
// Cite: specs/handler-contract.md HC-042.
var ErrLaunchPathMissing = errors.New("handler: launch path missing (HC-042)")

// ResolveLaunchPath returns the absolute path that MUST be used to launch
// a handler subprocess per handler-contract.md HC-042.
//
//	repoRoot     : the daemon's repo-root absolute path (configured at startup)
//	binaryRef    : the handler's binary reference (relative path or program name)
//	systemHandler: true iff the handler is operator-installed (e.g. Claude Code CLI)
//
// Behavior per HC-042:
//   - If systemHandler is true and binaryRef is a bare name (no path separator),
//     resolve via exec.LookPath (PATH lookup permitted).
//   - If systemHandler is true and binaryRef is already an absolute path,
//     return it as-is (the operator has pinned an explicit location).
//   - Otherwise, treat binaryRef as a repo-relative path; join with repoRoot;
//     return the joined absolute path.  PATH is NEVER consulted for in-repo
//     binaries.
//   - If binaryRef is empty, return ErrLaunchPathMissing.
//
// This function does NOT verify that the file exists on disk — that is a
// separate launch-time check covered by hk-8i31.50 (commit-hash check) and
// hk-8i31.68 (sensor: no launch without verified path).
//
// Absolute binaryRef for non-system handlers:
// HC-042 requires the path to be "resolved from a repo-relative prefix
// configured at daemon startup."  An already-absolute binaryRef bypasses that
// resolution contract and is therefore rejected with ErrLaunchPathMissing for
// non-system handlers.
func ResolveLaunchPath(repoRoot, binaryRef string, systemHandler bool) (string, error) {
	if binaryRef == "" {
		return "", ErrLaunchPathMissing
	}

	if systemHandler {
		// Absolute path already provided by the operator — honour it directly.
		if filepath.IsAbs(binaryRef) {
			return binaryRef, nil
		}
		// Bare name (no path separator): PATH lookup is permitted for system handlers.
		if !strings.ContainsRune(binaryRef, '/') {
			return exec.LookPath(binaryRef)
		}
		// Relative path with separators for a system handler: treat the same as
		// a repo-relative path (operator chose to pin a relative location).
		return filepath.Join(repoRoot, binaryRef), nil
	}

	// Non-system handler: PATH lookup is NEVER permitted.
	// An absolute binaryRef would bypass repo-relative resolution — reject it.
	if filepath.IsAbs(binaryRef) {
		return "", ErrLaunchPathMissing
	}

	return filepath.Join(repoRoot, binaryRef), nil
}
