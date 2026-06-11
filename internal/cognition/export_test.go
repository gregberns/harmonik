package cognition

// export_test.go — test-only exports for clock-injectable constructors.

import "time"

// ExportNewHeartbeatLivenessCheckerWithClock exposes the unexported
// newHeartbeatLivenessCheckerWithClock constructor for unit tests.
func ExportNewHeartbeatLivenessCheckerWithClock(k int, interval time.Duration, now func() time.Time) *HeartbeatLivenessChecker {
	return newHeartbeatLivenessCheckerWithClock(k, interval, now)
}
