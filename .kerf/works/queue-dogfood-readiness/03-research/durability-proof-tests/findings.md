# Research — Durability proof tests and ratchets

## Questions

1. Which production writes have durable-boundary tests?
2. Does each test read persistent state?
3. Does each ratchet fail when its claimed write edge is removed?
4. What does a ratchet not prove?

## Findings

- `TestReservationWriteFailure_NeverClaimsAndNeverLaunches` in
  `internal/daemon/reservationwritefail_e2e_test.go` drives the work loop. It
  blocks the queue write and asserts no claim, launch, run ID, or reservation.
- `TestCompleteAndUnlinkPersistsCompletedBeforeUnlink` in
  `internal/queue/persistence_internal_test.go` reads the canonical queue file
  at the persist-before-unlink boundary.
- `internal/queue/persistence.go` `completeAndUnlinkResult` persists the
  completed candidate before unlink. Its result separates persist failure from
  cleanup failure.
- `scripts/queue-status-writer-ratchet.go` checks eight named transition edges.
  Its test changes a named fixture call and asserts that the ratchet fails.

## Patterns to keep

- Use a ratchet as a source-surface guard.
- Pair it with a production-path test that reads durable state or injects a
  write failure.
- The shared release contract must name each observed durable state and owner
  before implementation work starts.

## Risks and decisions

- A method with the expected name can be a no-op. A green ratchet does not show
  that durable storage changed.
- Each new release test must fail when its persistence write is removed.
