// Build-time commit-hash stamp for the harmonik-twin-generic binary (HC-043).
//
// The commitHash variable is the sole hook for the ldflags stamp injected at
// build time:
//
//	go build -ldflags "-X main.commitHash=$(git rev-parse HEAD)" \
//	    ./cmd/harmonik-twin-generic
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
// The Makefile target that wires up the ldflags stamp for CI and local builds
// is tracked in bead hk-ahvq.48.5 (Makefile build-twin-claude target). That
// bead owns the authoritative build invocation; the variable declaration here
// is the source-side prerequisite.
//
// Cite: specs/handler-contract.md §4.10.HC-043, §4.10.HC-045.
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
	return fmt.Sprintf("harmonik-twin-generic commit=%s", stamp)
}

func writeVersion(w io.Writer) error {
	_, err := fmt.Fprintln(w, versionLine())
	return err
}
