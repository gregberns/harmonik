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

var commitHash = "unknown" //nolint:gochecknoglobals // build-time injection target

var version = "dev" //nolint:gochecknoglobals // build-time injection target

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
