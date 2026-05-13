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

// VerifyCommitHash reads binaryPath and verifies that the expected commit hash
// is embedded in the binary's data segment.
//
// The expected hash MUST be a non-empty string.  A 40-character lowercase hex
// SHA-1 (git full hash) is the canonical form, but VerifyCommitHash imposes no
// format constraint — it delegates format validation to the caller.
//
// Return values:
//   - nil         — the expected hash was found in the binary; proceed with launch.
//   - ErrStructural (wrapping diagnostic detail) — hash not present in binary;
//     the caller MUST fail launch and emit agent_failed with mismatch details
//     per HC-043.  Note: agent_failed emission is the orchestrator's obligation,
//     not this verifier's.
//   - other error — I/O failure reading the file; treat as ErrStructural at
//     the call site (the launch cannot safely proceed without a verified hash).
//
// Integration test: TestVerifyCommitHash_HC043_RealBinary in commithash_test.go
// exercises this function against a real harmonik-twin-generic binary built with
// the production ldflags stamp (hk-uwie).
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
		return fmt.Errorf("commit-hash mismatch: expected %q not present in %q: %w",
			expected, binaryPath, ErrStructural)
	}

	return nil
}
