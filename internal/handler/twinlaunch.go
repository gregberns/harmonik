// Package handler — twin-binary launch rules (HC-045).
//
// This file provides the TwinLaunchConfig record and VerifyTwinLaunch function
// that satisfy handler-contract.md HC-045:
//
//	"Twin binaries MUST also be launched from a known repo-relative path with
//	an expected-commit-hash check. The twin's expected commit hash MUST be
//	pinned at workflow/policy configuration time."
//
// # Relationship to real-handler launch rules
//
// Real handler binaries satisfy HC-042 (repo-relative path via ResolveLaunchPath)
// and HC-043 (commit-hash check via VerifyCommitHash).  HC-045 extends the same
// obligations to twin binaries.  VerifyTwinLaunch applies both checks in sequence
// so the daemon's launch site need not duplicate the logic.
//
// System handlers (system_handler=true, e.g. the Claude Code CLI) are NOT
// subject to this file; they are covered by HC-042's PATH-lookup carve-out.
// Twins are always in-repo binaries and never carry system_handler=true.
//
// # Configuration-time pinning
//
// HC-045 requires the twin's expected commit hash to be pinned at
// workflow/policy configuration time — i.e., resolved once when the workflow or
// policy is loaded, not re-derived at each launch.  TwinLaunchConfig captures
// that pin: the caller populates it from configuration (e.g. YAML policy, DOT
// node attributes) and passes it unchanged to VerifyTwinLaunch at launch time.
//
// Cite: specs/handler-contract.md §4.10.HC-045.
package handler

import (
	"fmt"
)

// ErrTwinLaunchConfigInvalid is returned by TwinLaunchConfig.Validate and by
// VerifyTwinLaunch when the configuration record is structurally incomplete
// (missing binary reference or expected commit hash).  It wraps ErrStructural
// so that callers routing on the error class see the correct class without an
// additional unwrap step.
//
// Cite: specs/handler-contract.md §4.10.HC-045.
var ErrTwinLaunchConfigInvalid = fmt.Errorf("handler: twin launch config invalid: %w", ErrStructural)

// TwinLaunchConfig is the configuration-time pin for a twin binary's launch
// constraints per HC-045.  Populate this record when the workflow or policy is
// loaded; pass it unchanged to VerifyTwinLaunch at subprocess launch time.
//
// BinaryRef holds the repo-relative path to the twin binary (e.g. "twins/claude-twin").
// It is interpreted the same way as the binaryRef argument to ResolveLaunchPath
// with systemHandler=false: PATH lookup is forbidden; the path is resolved
// against the daemon's repoRoot.
//
// ExpectedHash holds the commit hash string that MUST be found embedded in the
// twin binary's data segment (via -ldflags "-X main.commitHash=<sha>").  It
// MUST be a non-empty string.  The canonical form is a 40-character lowercase
// hex SHA-1 (git full hash), but VerifyTwinLaunch imposes no format constraint.
//
// Cite: specs/handler-contract.md §4.10.HC-045.
type TwinLaunchConfig struct {
	// BinaryRef is the repo-relative path to the twin binary.
	// Required; must not be empty or absolute.
	BinaryRef string

	// ExpectedHash is the commit hash pinned at workflow/policy configuration
	// time.  Required; must not be empty.
	ExpectedHash string
}

// Validate reports whether the TwinLaunchConfig is structurally complete:
//   - BinaryRef is non-empty.
//   - ExpectedHash is non-empty.
//
// It does NOT verify that the binary exists on disk or that the hash format is
// correct.  Those checks are performed at launch time by VerifyTwinLaunch.
//
// Returns ErrTwinLaunchConfigInvalid (which wraps ErrStructural) if the record
// is incomplete.
//
// Cite: specs/handler-contract.md §4.10.HC-045.
func (c TwinLaunchConfig) Validate() error {
	if c.BinaryRef == "" {
		return fmt.Errorf("TwinLaunchConfig: BinaryRef is empty: %w", ErrTwinLaunchConfigInvalid)
	}
	if c.ExpectedHash == "" {
		return fmt.Errorf("TwinLaunchConfig: ExpectedHash is empty: %w", ErrTwinLaunchConfigInvalid)
	}
	return nil
}

// VerifyTwinLaunch applies the full HC-045 pre-launch gate for a twin binary:
//
//  1. Validates the TwinLaunchConfig (BinaryRef and ExpectedHash non-empty).
//  2. Resolves the absolute binary path via ResolveLaunchPath with
//     systemHandler=false, enforcing the repo-relative path rule of HC-042.
//  3. Verifies the embedded commit hash via VerifyCommitHash, enforcing HC-043.
//
// repoRoot is the daemon's repo-root absolute path (configured at daemon startup).
//
// Return values:
//   - (absolutePath, nil) — all checks passed; absolutePath is the resolved
//     absolute path the caller MUST use for exec.  It is the same value that
//     ResolveLaunchPath would return.
//   - ("", ErrStructural-wrapping error) — any check failed; the twin binary
//     MUST NOT be launched.  The caller MUST emit agent_failed with mismatch
//     details per HC-043, HC-045.
//
// Cite: specs/handler-contract.md §4.10.HC-045.
func VerifyTwinLaunch(repoRoot string, cfg TwinLaunchConfig) (string, error) {
	// Step 1: structural completeness check.
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	// Step 2: path resolution — twins are always in-repo binaries (not system
	// handlers), so systemHandler=false enforces repo-relative-only resolution.
	// Any path-resolution failure (missing ref, absolute ref bypassing repo-root)
	// is a structural configuration defect per HC-042/HC-045.
	absPath, err := ResolveLaunchPath(repoRoot, cfg.BinaryRef, false)
	if err != nil {
		return "", fmt.Errorf("VerifyTwinLaunch: resolve path: %w: %w", err, ErrStructural)
	}

	// Step 3: commit-hash check — same gate as HC-043 for real handlers.
	if err := VerifyCommitHash(absPath, cfg.ExpectedHash); err != nil {
		return "", fmt.Errorf("VerifyTwinLaunch: %w", err)
	}

	return absPath, nil
}
