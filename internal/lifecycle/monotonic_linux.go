package lifecycle

import (
	"fmt"
	"syscall"
)

// MonotonicNsSinceBoot returns the current value of CLOCK_MONOTONIC in
// nanoseconds since the host's boot epoch. The monotonic clock is unaffected
// by NTP adjustments or operator wall-clock changes, making it suitable for
// RTO measurement per operator-nfr.md §4.8 ON-033.
//
// Implementation: clock_gettime(CLOCK_MONOTONIC, &ts) → ts.Sec*1e9 + ts.Nsec.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at_ns_since_boot` is
// the monotonic-clock companion field (nanoseconds since system boot, sourced
// from CLOCK_MONOTONIC on Linux)."
//
// Spec ref: operator-nfr.md §4.8 ON-033 — "SIGTERM receipt and `daemon_ready`
// emission timestamps MUST both carry a `_at_ns_since_boot` companion field."
func MonotonicNsSinceBoot() (uint64, error) {
	var ts syscall.Timespec
	if err := syscall.ClockGettime(syscall.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, fmt.Errorf("lifecycle: MonotonicNsSinceBoot: clock_gettime(CLOCK_MONOTONIC): %w", err)
	}
	ns := uint64(ts.Sec)*1_000_000_000 + uint64(ts.Nsec) //nolint:gosec // ts.Sec and ts.Nsec are always non-negative from CLOCK_MONOTONIC
	return ns, nil
}
