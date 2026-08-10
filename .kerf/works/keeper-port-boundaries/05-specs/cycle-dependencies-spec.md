# Cycle Dependencies Specification

## Requirements

The cycler receives one explicit dependency value. Construction validates required dependencies before any effect runs.

## Research summary

The current constructor has many callers and cannot change signatures in one step. Respawn and emission have valid no-op behavior.

## Approach

Add `CycleDeps` and `NewCyclerWithDeps(policy, env, deps) (*Cycler, error)`. Validate required contracts without calling them. Use explicit no-op emitter and optional respawn capability.

`CycleDeps` contains `Clock`, `CycleIDs`, `Pane`, `Context`, `Activity`, `Managed`, `Idle`, `Dispatch`, `Sleep`, `Hold`, `Operator`, `Handoff`, `Journal`, `Emitter`, and `Respawn`.

All fields except `Emitter` and `Respawn` are required. A nil emitter becomes `NoopEmitter`. A nil respawn port is valid and keeps escalation dormant.

`CycleIDGenerator` has `Next() string`. The shell calls it only after all entry gates pass. The production generator uses the shared clock. Test generators remain deterministic.

## Files and changes

- Add dependency definitions and validation to `internal/keeper/ports.go`.
- Add the validated constructor to `internal/keeper/cycle.go`.
- Add production dependency wiring beside `cmd/harmonik/keeper_cmd.go` `buildKeeperConfigs`.
- Keep `NewCycler` as a compatibility adapter until all callers migrate.

## Error handling and compatibility

Validation returns one stable error that lists missing contract names. It detects typed nil dependencies. No port method runs before success.

## Acceptance criteria

- A table removes each required dependency and gets the expected error.
- A multi-missing test proves stable name order in the error.
- A typed-nil test covers each required interface.
- Every failure leaves all recording ports empty.
- Production starts through the validated constructor.
- Nil emitter and respawn behavior matches the legacy constructor.

## Verification

Run `go test ./internal/keeper ./cmd/harmonik`.
