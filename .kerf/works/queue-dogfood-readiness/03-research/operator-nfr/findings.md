# Operator NFR research findings

## Questions

1. Does ordered drain cover work committed before merge?
2. Which timeout and operator result apply to it?
3. What controlled-load evidence is required before the first batch?

## Findings

`ON-008` requires pause and upgrade to wait for all in-flight runs and drain
steps. `ON-027` stops queue advance, reaches a checkpoint, drains handlers and
events, unlocks workspaces, then exits or pauses. `ON-029` makes drain
timeouts per-step configurable. `ON-018` requires N-1 readable durable
artifacts.

`PL-011` consumes this ordering. `exitClean` in
`internal/daemon/scheduler.go` instead has one fixed ten-second wait and then
cancels active queues. That path can turn a slow terminal ladder into
cancellation without a durable recovery outcome.

## Patterns and risks

ON owns cross-subsystem order, timeout policy, and the operator meaning. A
commit before merge is a stricter safe point than a checkpoint. Unlocking a
workspace after only a checkpoint conflicts with terminal recovery.

## Design constraints

- Define committed-before-merge as a special drain outcome.
- Its timeout must leave an inspectable recovery record, not ambiguous
  cancellation.
- Stop dispatch, resolve durable intent work, flush observations, and release
  resources only when the recovery owner permits it.
- Controlled-load evidence records load, timeout values, stop point, and
  retained artifacts. Use additive, N-1 compatible storage where possible.
