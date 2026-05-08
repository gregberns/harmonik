// Build-time commit-hash stamp for the harmonik-twin-claude binary (HC-043).
//
// The commitHash variable is the sole hook for the ldflags stamp injected at
// build time:
//
//	go build -ldflags "-X main.commitHash=$(git rev-parse HEAD)" \
//	    ./cmd/harmonik-twin-claude
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
//	harmonik-twin-claude commit=<sha>
//
// Output format (when stamp is absent — unstamped build):
//
//	harmonik-twin-claude commit=(unstamped)
//
// Per HC-043: "System handlers MAY log the --version output at startup in
// lieu of a hash check." For twin binaries HC-045 requires an explicit
// commit-hash check via VerifyCommitHash; --version is the human-readable
// complement.
func versionLine() string {
	stamp := commitHash
	if stamp == "" {
		stamp = "(unstamped)"
	}
	return fmt.Sprintf("harmonik-twin-claude commit=%s", stamp)
}

// writeVersion writes the version line followed by a newline to w.
// Called by run() when --version is set.
func writeVersion(w io.Writer) {
	fmt.Fprintln(w, versionLine())
}
