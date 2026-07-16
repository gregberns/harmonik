// Build-time commit-hash stamp for the harmonik-twin-pi binary (HC-043).
//
// The commitHash variable is the sole hook for the ldflags stamp injected at
// build time:
//
//	go build -ldflags "-X main.commitHash=$(git rev-parse HEAD)" \
//	    ./cmd/harmonik-twin-pi
//
// The daemon's pre-launch gate (internal/handler.VerifyCommitHash) verifies the
// stamp by scanning the binary's raw bytes — no subprocess execution required.
// When the binary is built without the stamp (plain `go build` / `go test`),
// commitHash is the zero string ""; VerifyCommitHash treats an absent stamp as a
// mismatch per HC-043: binaries lacking an embedded hash MUST NOT be launched by
// the daemon.
//
// Cite: specs/handler-contract.md §4.10.HC-043, §4.10.HC-045.
package main

import (
	"fmt"
	"io"
)

// commitHash is stamped at build time via:
//
//	-ldflags "-X main.commitHash=$(git rev-parse HEAD)"
//
// It is the zero string when the binary is built without the stamp.
var commitHash string

// versionLine returns the human-readable version string for this binary.
//
// Output format:
//
//	harmonik-twin-pi commit=<sha>        (stamped)
//	harmonik-twin-pi commit=(unstamped)  (plain build / test)
func versionLine() string {
	stamp := commitHash
	if stamp == "" {
		stamp = "(unstamped)"
	}
	return fmt.Sprintf("harmonik-twin-pi commit=%s", stamp)
}

// writeVersion writes the version line followed by a newline to w.
func writeVersion(w io.Writer) {
	fmt.Fprintln(w, versionLine())
}
