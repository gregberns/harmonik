# Test Scenario Specification

## Requirements

Shared scenarios use production policy and fake external boundaries. A test gives a reason when it disables an active production gate.

## Research summary

Current tests copy partial configs. Raw library defaults disable gates that production enables.

## Approach

Add a reusable builder under `internal/keepertest`. Require a resolved `CyclePolicy` at construction. Command scenarios pass the policy created from resolved project configuration. Use one fake clock, a valid session, and recording narrow ports. Provide behavior methods instead of raw config mutation. Provide named gate-disable methods that reject an empty reason.

The builder provides `DisableBootGrace`, `DisableOperatorTurnGate`, `DisablePostAnswerGrace`, `DisableManagedGate`, `DisableCrispIdleGate`, `DisableDispatchGate`, `DisableSleepGate`, `DisableHoldGate`, `DisableAntiLoopGate`, and `DisableOperatorPresenceGate`. Each method records a non-empty reason. The act threshold is scenario input, not a disabled gate.

The builder does not expose mutable policy after construction.

Extend the discrete-event harness. Record a global ordered effect stream and typed per-port records.

## Files and changes

- Add `internal/keepertest/scenario.go` and `scenario_test.go`.
- Migrate one success scenario and the transcript-collision scenario first.
- Update the tmux twin to use production policy or explicit named gate opt-outs.
- Add the real command composition scenario under `cmd/harmonik` so it can call command-private wiring.

## Error handling and compatibility

Builder misuse fails the calling test with the disabled gate name. Fake effects remain deterministic.

## Acceptance criteria

- A command-composition scenario proves its resolved project policy has boot grace and both transcript gates enabled.
- Tests cover every named disable method and reject every empty reason.
- A compile-time API test proves callers cannot mutate policy through the builder.
- The Alpha collision replay parks, does not clear, does not write a hold, and later retries.
- The tracked scenario task runs the real command composition path to a completed cycle.
- The tracked exploratory task checks dependency validation through keeper doctor.

## Verification

Run `go test ./internal/keepertest ./internal/keeper` and `go test -tags=integration ./internal/keeper`.
