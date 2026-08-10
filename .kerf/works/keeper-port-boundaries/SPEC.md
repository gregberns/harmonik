# Keeper Port Boundaries Specification

## Objective

Replace the keeper cycle’s dual injection system with one policy value, one environment value, and one validated dependency value. Preserve runtime behavior and on-disk formats. Make integration scenarios use production behavior by default.

## Main types

`CyclePolicy` contains thresholds, timers, grace windows, and retry limits. `CycleEnv` contains the agent name, project path, tmux target, and transcript path. `CycleDeps` contains the clock, cycle ID generator, and narrow ports.

The validated constructor is:

```go
func NewCyclerWithDeps(CyclePolicy, CycleEnv, CycleDeps) (*Cycler, error)
```

The legacy constructor remains as an adapter during migration.

## Required ports

- `PaneWriter`: inject text, send Escape, and set tmux environment values.
- `ContextStore`: read the gauge, write the managed session, and clear the precompact marker.
- `ActivityProbe`: read idle-marker time and the latest real user and assistant turns.
- `ManagedProbe`: read managed state.
- `IdleProbe`: read crisp-idle state.
- `DispatchProbe`: read dispatch hold state.
- `SleepProbe`: read session sleep state.
- `HoldProbe`: read manual keeper hold state.
- `OperatorPresenceProbe`: read operator attachment state.
- `HandoffDocument`: locate, read, stat, and scrub keeper nonces from the handoff.
- `CycleJournalStore`: read and write the cycle journal.
- `ClockPort`: drive all time.
- `CycleIDGenerator`: mint one ID after all cycle-entry gates pass.

The emitter can default to `NoopEmitter`. A nil respawn port disables escalation. All other dependencies are required.

Pane capture remains outside automatic-cycle dependencies.

## Observation and action flow

The shell composes `GateSnapshot` from source probes. It preserves all current lazy-read guards. The reactor remains pure. The shell executes returned actions through effect ports.

Recovery reads only the facts it needs. It does not trigger a full gate sample.

## Policy levels

Library defaults preserve current `applyDefaults` behavior. Effective production policy comes from fully resolved project configuration. The command layer does not invent transcript-window defaults.

## Scenario rules

`internal/keepertest` provides a scenario builder that requires resolved policy. It adds fake time and recording ports. Tests change behavior through methods. A method that disables an active gate requires a reason. The builder does not expose mutable policy.

The command package owns the isolated production-composition scenario. It can call command-private configuration and wiring helpers.

## Compatibility and removal

`internal/keeper/legacy_seams.txt` lists every temporary function field and broad adapter. `scripts/keeper-seam-ratchet.sh` blocks unlisted additions. Migration tasks remove all entries and then remove the ratchet.

## Acceptance

- Old and new defaults match field by field.
- Command production policy matches the new production policy.
- Missing dependencies fail before any effect.
- Differential scenarios match ordered effects, state, journal, events, and errors.
- Every entry gate has an isolated scenario.
- The transcript collision scenario parks without clear or manual hold and later retries.
- A real command composition scenario reaches a completed journal and event.
- Origin and dependency mutations make their scenarios fail.
- `scripts/keeper-seam-ratchet.sh` passes during migration.
- Final seam checks find no legacy function field or broad adapter.
- Keeper tests, integration tests, `make fast`, and `make full` pass.

## Detailed contracts

The implementation details and exact interfaces are in `05-specs/`. The research basis is in `04-research/`.
