// Build-time commit-hash stamp for the harmonik daemon binary (hk-mz0x4).
//
// The commitHash variable is the sole hook for the ldflags stamp injected at
// build time:
//
//	go build -ldflags "-X main.commitHash=$(git rev-parse HEAD)" \
//	    ./cmd/harmonik
//
// The linker writes the provided value as a raw string into the binary's data
// segment. The value is forwarded to the daemon_started event payload
// (binary_commit_hash field, §8.7.1) so that the running commit is observable
// from the event log without inspecting the binary.
//
// When the binary is built without the ldflags stamp (e.g. plain `go build`
// or `go test`), commitHash retains its initialiser value "unknown".  The
// event payload will show "unknown" rather than a real SHA.
//
// Cite: specs/event-model.md §8.7.1 (daemon_started payload); bead hk-mz0x4.
package main

import (
	"fmt"
	"runtime"
)

// commitHash is stamped at build time via:
//
//	-ldflags "-X main.commitHash=$(git rev-parse HEAD)"
//
// It defaults to "unknown" so that binaries built without the stamp emit a
// recognisable sentinel rather than an empty string.
//
// Cite: specs/event-model.md §8.7.1; bead hk-mz0x4.
var commitHash = "unknown" //nolint:gochecknoglobals // build-time injection target

// version is the human-facing release version, stamped at build time via:
//
//	-ldflags "-X main.version=$(git describe --tags --always --dirty)"
//
// It defaults to "dev" so that binaries built without the stamp (plain
// `go build` / `go test`) report a recognisable sentinel rather than an empty
// string. Consumed by the `harmonik version` subcommand (hk-release-prep).
var version = "dev" //nolint:gochecknoglobals // build-time injection target

// buildDate is the UTC build timestamp, stamped at build time via:
//
//	-ldflags "-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// It defaults to "unknown" so that unstamped binaries report a recognisable
// sentinel rather than an empty string. Consumed by the `harmonik version`
// subcommand (hk-release-prep).
var buildDate = "unknown" //nolint:gochecknoglobals // build-time injection target

// versionLine renders the single-line version string printed by the `version`
// subcommand:
//
//	harmonik {version} (commit {short-commit}, built {buildDate}) {GOOS}/{GOARCH}
//
// The commit is truncated to its first 12 characters when it is a full SHA;
// the "unknown" sentinel (and any value shorter than 12 chars) is printed
// verbatim. GOOS/GOARCH come from runtime so the line reflects the binary's
// target platform.
func versionLine() string {
	shortCommit := commitHash
	if len(shortCommit) > 12 {
		shortCommit = shortCommit[:12]
	}
	return fmt.Sprintf("harmonik %s (commit %s, built %s) %s/%s",
		version, shortCommit, buildDate, runtime.GOOS, runtime.GOARCH)
}
