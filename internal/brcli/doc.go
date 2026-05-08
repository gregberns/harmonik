// Package brcli is the sole translation layer between harmonik and the Beads
// `br` CLI. All Beads interactions from harmonik code MUST route through the
// [Adapter] type declared in this package; no other package may invoke `br`
// directly.
//
// Spec ref: specs/beads-integration.md §4.8 BI-025.
//
// # Responsibilities
//
// The adapter translates harmonik's typed queries and writes into `br`
// subprocess invocations (via [Adapter.Run]) and returns the raw subprocess
// outcome as a [Result]. Higher-level methods built on [Adapter.Run] will add
// the layers mandated by the BI-025 family:
//
//   - BI-024a — `br --version` handshake (hk-872.26)
//   - BI-025a — exit-code taxonomy / BrError enum (hk-872.28)
//   - BI-025b — mandatory --format json (hk-872.29)
//   - BI-025c — subprocess timeout discipline (hk-872.30)
//   - BI-025d — stderr capture + 1 MiB cap + scenario handling (hk-872.31)
//   - BI-025e — concurrent-invocation discipline (hk-872.32)
//
// # Single-change-point guarantee
//
// A breaking change in Beads MUST produce exactly one adapter change in this
// package; no scattered per-callsite updates are acceptable (BI-025).
//
// # Binary path injection
//
// The adapter is constructed with an explicit `br` binary path. Production
// callers MUST resolve `br` from PATH at daemon startup and pass the resolved
// absolute path to [New]; they MUST NOT inject a custom path at runtime. The
// injectable constructor parameter exists for testability only — unit tests MAY
// substitute a mock `br` binary at the injected path (BI-025, spec-template.md
// §10.2 contract-tests).
package brcli
