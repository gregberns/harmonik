// Package main is the fail-immediately twin binary for exploratory testing.
//
// Purpose: models "the handler crashed at startup" per EXPLORATORY_TESTING_PLAN.md §4 P2.
// Exits immediately with code 1 and writes nothing to stdout.
// Stderr receives a single diagnostic line for operator clarity.
//
// Build:
//
//	go build -o /tmp/twin-fail ./test/twins/fail-immediately
//
// Contract (EXPLORATORY_TESTING_PLAN.md §4 P2 acceptance):
//   - exits with code 1
//   - writes nothing to stdout
//   - stderr: one-line diagnostic
//
// Hash-pinning: build with -ldflags "-X main.commitHash=<sha>" when
// VerifyTwinLaunch hash-checking is required (deferred per bead body; optional
// for exploratory wave using --no-verify or equivalent).
package main

import (
	"fmt"
	"os"
)

// commitHash is optionally injected at build time via
// -ldflags "-X main.commitHash=<sha>" so that VerifyTwinLaunch can verify
// the binary against a pinned hash (HC-045).  Empty string is acceptable for
// exploratory runs that bypass hash verification.
var commitHash string //nolint:gochecknoglobals // build-time injection target

func main() {
	fmt.Fprintln(os.Stderr, "fail-immediately twin")
	os.Exit(1)
}
