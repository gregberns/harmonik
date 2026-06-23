package brcli

// BI-026 — Harmonik absorbs breakage rather than forking Beads.
//
// On a backwards-incompatible Beads change, harmonik MUST either:
//   (a) remain pinned to the prior Beads version and delay upgrade, or
//   (b) ship a harmonik release with an adapter change that handles the new
//       surface.
//
// Forking Beads to patch is forbidden.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026.
//
// # Structural enforcement
//
// This package ([brcli]) is the sole translation layer between harmonik and
// the Beads `br` CLI (BI-025). All `br` subprocess invocations MUST route
// through [Adapter.Run]; no other harmonik package may call `br` directly.
//
// A breaking change in Beads (JSON schema change, flag rename, exit-code
// reassignment) MUST produce exactly one change: in this package. No
// scattered per-callsite updates are acceptable. The breakage_test.go
// companion enforces the sole-adapter invariant structurally.
//
// # Breakage kinds
//
// | Kind              | Indicator                          | Adapter response         |
// |-------------------|------------------------------------|--------------------------|
// | Schema change     | JSON parse failure on br output    | BrSchemaMismatch (future) |
// | Flag rename       | Non-zero exit / argparse stderr    | BrOther / BrUnavailable  |
// | Exit-code change  | Unrecognized exit code             | BrOther                  |
// | Binary removal    | Exec failure at startup            | BrUnavailable (exit 8)   |
//
// Each kind surfaces through this package's error returns; callers outside
// brcli observe only the typed errors declared here (and in adapter.go,
// show.go, audit.go, etc.) — not raw `br` output or exit codes.
//
// # Version-pin relationship
//
// BI-026 and BI-024 (version-pin) are co-enforced: the [CheckBrVersion]
// handshake (BI-024a, amended by hk-m6243) catches version drift at daemon
// startup. A version delta (observed != pinned) is a loud WARNING — the daemon
// logs and continues. Hard-failure (exit code 8) is reserved for exec failure
// or unparseable output. Schema breakage arising from an actual incompatible
// `br` surface change surfaces as BrSchemaMismatch or BrOther at call time,
// regardless of the version pin.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

// ErrBrBreakageForked is a sentinel that MUST NOT appear in production code.
// It marks a policy violation: some code path attempted to bypass the adapter
// and invoke `br` directly. Any occurrence of this error is a structural
// defect in the caller, not a `br` runtime error.
//
// The breakage_test.go enforcement test asserts statically (via source
// analysis) that no harmonik package outside internal/brcli imports os/exec
// for the purpose of invoking `br`. This sentinel exists so that a future
// dynamic guard (e.g. an integration test that replaces the adapter with a
// trap) has a typed error to return and assert on.
//
// Spec ref: specs/beads-integration.md §4.8 BI-025, BI-026.
var ErrBrBreakageForked = errBrBreakageForked{}

// errBrBreakageForked is the concrete type backing ErrBrBreakageForked.
// It is unexported so that callers can only match via errors.Is; they cannot
// construct it.
type errBrBreakageForked struct{}

func (errBrBreakageForked) Error() string {
	return "brcli: BI-026 violation — br invoked outside the adapter (see specs/beads-integration.md §4.8)"
}
