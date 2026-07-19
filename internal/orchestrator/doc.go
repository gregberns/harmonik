// Package orchestrator holds the pure queue-dispatch DECISION predicates: the
// cross-queue round-robin selector and (in later sub-slices) the eager-fill,
// pre-screen, and group-advance planners.
//
// It is the M5-slice-3 work-loop-brain leaf, extracted from internal/daemon
// WITHOUT semantic change (mirroring internal/policy). The daemon shell owns
// every effect: it holds the QueueStore write lock, projects the live
// *LockedQueueStore / *RunRegistry into the narrow FleetSnapshot below at the
// call site (see internal/daemon/workloop.go snapshotFleet), calls the pure
// decision, then acts on the returned Selection (Phase-3 claim-time
// re-validation, dispatch stamp, event emission all stay daemon-side).
//
// # Purity
//
// This package performs NO I/O, reads NO clock (time.Now), mints NO IDs
// (uuid), emits NO bus events, holds NO lock, and mutates NO external state.
// Every decision is value-in / value-out (the internal/mergeq "critical func"
// discipline; the same contract internal/policy satisfies).
//
// Enum-typed queue fields (QueueStatus / GroupKind / ItemStatus) are projected
// as booleans/strings at the daemon boundary so orchestrator never imports
// internal/queue. The dependency edge is strictly daemon → orchestrator, never
// reverse. The only non-test imports are the Go standard library and
// internal/core; the depguard rule (.golangci.yml) permits exactly $gostd +
// internal/core.
package orchestrator
