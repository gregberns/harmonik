# Event intent research

## Questions

1. Does the current state function perform hidden effects?
2. Which event envelope fields survive at the daemon boundary?
3. What value shape keeps the contract deterministic?

## Findings

`internal/queue/state.go` `newEvent` marshals the payload, calls `uuid.NewV7`, and reads `time.Now`.
`AdvanceGroup` therefore performs hidden effects.

`internal/daemon/scheduler.go` discards the generated event ID and envelope time.
It marshals the payload again and calls the bus with only the type and payload bytes.
`internal/eventbus/busimpl.go` then creates the real event ID and envelope time.

A queue event intent needs only `core.EventType` and `json.RawMessage`.
This shape preserves deterministic payload bytes and keeps identity at the event bus edge.

The same hidden helper also affects `AppendItems`.
Removing `newEvent` from queue state alone would leave the queue package impure.
The event-intent migration should cover both `AdvanceGroup` and `AppendItems` as one compiler change.

## Options

Typed payload pointers avoid an early marshal but expose mutable interface values.
Raw JSON bytes are detached and match the bus input.
Raw JSON is the preferred contract.

## Risks

Tests and scenario fixtures inspect `core.Event` fields today.
The migration must update those callers and prove payload bytes stay stable.
