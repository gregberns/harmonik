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
