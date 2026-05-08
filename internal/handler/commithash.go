// Package handler — commit-hash verification for in-repo binaries (HC-043).
//
// This file provides VerifyCommitHash, the pre-launch gate that satisfies
// handler-contract.md HC-043.  Call it before exec-ing an in-repo handler
// binary; a non-nil return means the binary MUST NOT be launched.
//
// # System-handler exception
//
// System handlers declared with system_handler=true (e.g. the Claude Code CLI)
// are NOT routed through VerifyCommitHash.  Per HC-043: "System handlers MAY
// log --version at startup in lieu of hash check; no signature verification is
// performed at MVH."  The launch path for system handlers ends at
// ResolveLaunchPath (launchpath.go); the commit-hash gate applies only to
// in-repo binaries.
//
// # Binary scanning mechanism
//
// VerifyCommitHash reads the binary's raw bytes and searches for the expected
// hash string.  Go's linker, when built with
//
//	-ldflags "-X main.commitHash=<sha>"
//
// writes the variable's value as a raw string literal into the binary's data
// segment.  Scanning the bytes for the expected value is therefore a direct
// read of the embedded string — no process execution required.
//
// Note: binary signing (cosign, full supply-chain verification) is deferred
// post-MVH per the locked decision referenced in specs/handler-contract.md
// §2.2.  The commit-hash check is the MVH gate.
//
// Cite: specs/handler-contract.md §4.10.HC-043.
package handler

import (
	"bytes"
	"fmt"
	"os"
)

// ErrCommitHashMismatch is a structural sub-sentinel that wraps ErrStructural.
// It is emitted when VerifyCommitHash finds that the binary's embedded commit
// hash does not match the expected hash supplied by the caller.  Because it
// wraps ErrStructural, errors.Is(err, ErrStructural) returns true for any
// error that wraps ErrCommitHashMismatch — callers that need hash-specific
// routing MUST check this sub-sentinel first (narrowest-first dispatch per
// HC-020).
//
// errors.Is(err, ErrCommitHashMismatch) == true  → hash-mismatch matched
// errors.Is(err, ErrStructural)         == true  → structural routing applies
//
// Cite: specs/handler-contract.md §4.10.HC-043, §4.5.HC-020.
var ErrCommitHashMismatch = fmt.Errorf("handler: commit hash mismatch: %w", ErrStructural)

// VerifyCommitHash reads binaryPath and verifies that the expected commit hash
// is embedded in the binary's data segment.
//
// The expected hash MUST be a non-empty string.  A 40-character lowercase hex
// SHA-1 (git full hash) is the canonical form, but VerifyCommitHash imposes no
// format constraint — it delegates format validation to the caller.
//
// Return values:
//   - nil             — the expected hash was found in the binary; proceed with launch.
//   - ErrCommitHashMismatch (wrapping ErrStructural) — hash not present in binary;
//     the caller MUST fail launch and emit agent_failed with mismatch details
//     per HC-043.  Note: agent_failed emission is the orchestrator's obligation,
//     not this verifier's.
//   - other error     — I/O failure reading the file; treat as ErrStructural at
//     the call site (the launch cannot safely proceed without a verified hash).
//
// Follow-up bead: an integration test that exercises VerifyCommitHash against
// a real ldflags-stamped binary built from cmd/harmonik-twin-claude/ is tracked
// as hk-uwie — see commithash_test.go.
//
// Cite: specs/handler-contract.md §4.10.HC-043.
func VerifyCommitHash(binaryPath, expected string) error {
	if expected == "" {
		return fmt.Errorf("handler: VerifyCommitHash: expected hash is empty: %w", ErrStructural)
	}

	//nolint:gosec // G304: binaryPath is the resolved repo-relative launch path from ResolveLaunchPath; provenance is daemon config
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("handler: VerifyCommitHash: read binary %q: %w", binaryPath, err)
	}

	if !bytes.Contains(data, []byte(expected)) {
		return fmt.Errorf("handler: binary %q: expected commit hash %q not found: %w",
			binaryPath, expected, ErrCommitHashMismatch)
	}

	return nil
}
