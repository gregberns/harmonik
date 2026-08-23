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

var commitHash string

func versionLine() string {
	stamp := commitHash
	if stamp == "" {
		stamp = "(unstamped)"
	}
	return fmt.Sprintf("harmonik-twin-codex commit=%s", stamp)
}

func writeVersion(w io.Writer) error {
	_, err := fmt.Fprintln(w, versionLine())
	return err
}
