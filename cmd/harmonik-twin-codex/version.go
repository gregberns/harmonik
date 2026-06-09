// Build-time commit-hash stamp for the harmonik-twin-codex binary (HC-043).
//
// The commitHash variable is the sole hook for the ldflags stamp injected at
// build time:
//
//	go build -ldflags "-X main.commitHash=$(git rev-parse HEAD)" \
//	    ./cmd/harmonik-twin-codex
//
// The linker writes the provided value as a raw string into the binary's data
// segment. The daemon's pre-launch gate (internal/handler.VerifyCommitHash,
// hk-8i31.50) verifies the stamp by scanning the binary's raw bytes — no
// subprocess execution required.
//
// When the binary is built without the ldflags stamp (e.g. plain `go build`
// or `go test`), commitHash is the zero string "". VerifyCommitHash treats an
// absent stamp as a mismatch per HC-043: binaries lacking an embedded hash
// MUST NOT be launched by the daemon.
//
// Cite: specs/handler-contract.md §4.10.HC-043, §4.10.HC-045;
// codex-harness C6-migration-test-spec.md.
package main

import (
	"fmt"
	"io"
)

// commitHash is stamped at build time via:
//
//	-ldflags "-X main.commitHash=$(git rev-parse HEAD)"
//
// It is the zero string when the binary is built without the stamp (plain
// `go build` or `go test`). The daemon's VerifyCommitHash gate (hk-8i31.50)
// treats an absent stamp as a hash mismatch per HC-043.
//
// Cite: specs/handler-contract.md §4.10.HC-043.
var commitHash string

// versionLine returns the human-readable version string for this binary.
//
// Output format (when stamp is present):
//
//	harmonik-twin-codex commit=<sha>
//
// Output format (when stamp is absent — unstamped build):
//
//	harmonik-twin-codex commit=(unstamped)
func versionLine() string {
	stamp := commitHash
	if stamp == "" {
		stamp = "(unstamped)"
	}
	return fmt.Sprintf("harmonik-twin-codex commit=%s", stamp)
}

// writeVersion writes the version line followed by a newline to w.
func writeVersion(w io.Writer) {
	fmt.Fprintln(w, versionLine())
}
