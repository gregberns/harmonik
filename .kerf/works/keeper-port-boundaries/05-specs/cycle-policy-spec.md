# Cycle Policy Specification

## Requirements

Policy contains thresholds, durations, and retry limits. It contains no functions, paths, names, or effect interfaces. Production and tests can use the same resolved value.

## Research summary

The library and command layers currently resolve different defaults. The new types must preserve that distinction.

## Approach

Add `CyclePolicy` and `CycleEnv`. Add `DefaultCyclePolicy` for library defaults. Add a conversion from the fully resolved command configuration. Keep configuration overlays in `cmd/harmonik`.

`CycleEnv` contains `AgentName`, `ProjectDir`, `TmuxTarget`, and `TranscriptDir`. These values identify resources. They are not policy or effects.

`CyclePolicy` contains `ActAbsTokens`, `ActPctCeil`, `WarnAbsTokens`, `WarnPctCeil`, `ForceActAbsTokens`, `ForceActPctCeil`, `ActPct`, `WarnPct`, `ForceActPct`, `HandoffTimeout`, `ClearSettle`, `PollInterval`, `ClearConfirmBackstop`, `ClearConfirmRetries`, `ModelDoneTimeout`, `ForceRetryInterval`, `IdleRestartAbsTokens`, `IdleRestartCooldown`, `BootGracePeriod`, `MaxBootGraceTotal`, `MaxHandoffTimeouts`, `HoldTTL`, `OperatorTurnLookback`, and `PostAnswerGrace`.

Library defaults match `CyclerConfig.applyDefaults`. The command layer creates effective production policy after it resolves required project configuration. It does not invent nonzero transcript defaults.

## Files and changes

- Add `internal/keeper/cycle_policy.go` with the two value types and resolution helpers.
- Update `internal/keeper/step.go` so the reactor stores policy and derived capability values.
- Add parity tests in `internal/keeper/cycle_policy_test.go`.

## Error handling and compatibility

Preserve every zero-value sentinel. Keep `CyclerConfig` and `NewCycle` during migration.

## Acceptance criteria

- A field-by-field test proves library-default parity.
- A command test proves resolved project-policy parity.
- Tests cover every listed field and each disabled zero-value sentinel.
- `CyclePolicy` has no function or interface fields.

## Verification

Run `go test ./internal/keeper ./cmd/harmonik`.
