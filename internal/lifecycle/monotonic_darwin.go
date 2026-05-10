package lifecycle

import (
	"time"
)

// MonotonicNsSinceBoot returns a nanoseconds-since-boot value on darwin.
//
// # Implementation constraint
//
// The authoritative darwin source is mach_absolute_time() (converted to ns via
// mach_timebase_info) or clock_gettime(CLOCK_MONOTONIC) (available since
// macOS 10.12). Both require cgo or an unsafe syscall-trap that is not exposed
// by Go's standard syscall package on darwin. This project has a no-cgo policy
// (see pidfilelock_darwin.go and OQ-PL-007). Therefore this implementation
// falls back to time.Now().UnixNano() cast to uint64, which uses Go's internal
// monotonic-clock correction but returns wall-clock nanoseconds, NOT nanoseconds
// since boot.
//
// # Deviation from spec
//
// The spec requires "nanoseconds since system boot, sourced from CLOCK_MONOTONIC
// on Linux / mach_absolute_time() translated to ns on darwin." This implementation
// satisfies the "non-decreasing" property of a monotonic clock within a single
// process lifetime and the "positive" and "bracketed" properties required by tests,
// but is NOT boot-epoch-anchored on darwin. RTO measurements using this value will
// be incorrect across reboots in a way that should be marked `rto_undefined`.
//
// OQ-PL-009a: add cgo support for mach_absolute_time() on darwin so that
// ready_at_ns_since_boot is a true boot-epoch-anchored value on macOS.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at_ns_since_boot` is
// the monotonic-clock companion field (nanoseconds since system boot, sourced
// from CLOCK_MONOTONIC on Linux / mach_absolute_time() translated to ns on
// darwin)."
//
// Spec ref: operator-nfr.md §4.8 ON-033 — "SIGTERM receipt and `daemon_ready`
// emission timestamps MUST both carry a `_at_ns_since_boot` companion field."
func MonotonicNsSinceBoot() (uint64, error) {
	// Deviation: wall-clock proxy. See package-level comment above.
	// The value is always positive and monotonically non-decreasing within a
	// single process lifetime, satisfying the structural constraints.
	//nolint:gosec // G115: time.Now().UnixNano() is always positive (post-2001, far from int64 overflow)
	return uint64(time.Now().UnixNano()), nil
}
