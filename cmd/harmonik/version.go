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
// or `go test`), resolvedCommitHash() falls back to the VCS revision embedded
// by the Go toolchain via runtime/debug.ReadBuildInfo() (available since
// Go 1.18 when building from a git worktree without -trimpath).  If neither
// source yields a hash, the sentinel "unknown" is returned.
//
// Cite: specs/event-model.md §8.7.1 (daemon_started payload); bead hk-mz0x4,
// bead hk-v3nv (runtime fallback).
package main

import "runtime/debug"

// commitHash is stamped at build time via:
//
//	-ldflags "-X main.commitHash=$(git rev-parse HEAD)"
//
// It defaults to "unknown" so that binaries built without the stamp emit a
// recognisable sentinel rather than an empty string.
//
// Cite: specs/event-model.md §8.7.1; bead hk-mz0x4.
var commitHash = "unknown" //nolint:gochecknoglobals // build-time injection target

// version is stamped at build time via:
//
//	-ldflags "-X main.version=$(git describe --tags --exact-match)"
//
// It defaults to "dev" so that binaries built without the stamp emit a
// recognisable sentinel rather than an empty string.
//
// Cite: specs/release-pipeline.md §2.3; bead hk-t0yvy.
var version = "dev" //nolint:gochecknoglobals // build-time injection target

// resolvedCommitHash returns the best available commit hash for the running
// binary.  It prefers the ldflags-stamped commitHash (set at build time via
// -X main.commitHash=<sha>).  When that value is still the sentinel "unknown",
// it falls back to the VCS revision embedded by the Go toolchain in the
// binary's build info (go build / go install from a git worktree since Go
// 1.18).  Returns "unknown" when neither source has a value.
//
// Cite: bead hk-v3nv (TA4 tokenaudit — unblocks version<->cost correlation).
func resolvedCommitHash() string {
	if commitHash != "unknown" && commitHash != "" {
		return commitHash
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			return s.Value
		}
	}
	return "unknown"
}
