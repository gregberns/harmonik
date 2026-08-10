# Components

## 1. Cycle policy

Cycle policy contains thresholds, durations, and retry limits.

Requirements:

- Production and test construction can use the same policy value.
- Default resolution occurs in one place.
- Policy contains no functions or effect interfaces.
- Existing zero-value defaults remain unchanged.

Dependencies: none.

## 2. Cycle dependencies

Cycle dependencies contain the effects and observations required by the cycle shell.

Requirements:

- The cycler receives one explicit dependency value.
- Production wiring creates all required dependencies.
- Construction reports a missing required dependency before a cycle starts.
- The reactor receives only values and remains pure.

Required dependencies are `CycleIDGenerator`, `PaneWriter`, `ContextStore`, `ActivityProbe`, six source probes, `HandoffDocument`, `CycleJournalStore`, and `ClockPort`. A nil emitter becomes `NoopEmitter`. A nil respawn port stays nil and disables escalation. Validation runs during construction. A validation error names each missing dependency. No effect runs before validation succeeds.

Dependencies: narrow ports only. The cycler constructor consumes cycle policy, cycle environment, and cycle dependencies as separate inputs.

## 3. Narrow ports

Small ports group operations that share one external source.

Requirements:

- Gauge file operations do not include transcript or tmux operations.
- Pane writes do not require pane capture.
- Transcript activity has a dedicated observation contract.
- Queue, sleep, and hold observations do not masquerade as gauge file operations.
- Crash-recovery formats and event formats remain unchanged.
- The handoff document and the cycle journal use distinct contracts.

Dependencies: none.

### Method ownership

| Current method | Target contract |
| --- | --- |
| `PanePort.Inject` | `PaneWriter.Inject` |
| `PanePort.SendEscape` | `PaneWriter.SendEscape` |
| `PanePort.SetEnv` | `PaneWriter.SetEnv` |
| `PanePort.Capture` | `PaneObserver.Capture` outside automatic cycle dependencies |
| `PanePort.OperatorAttached` | `OperatorPresenceProbe.Attached` |
| `GaugePort.ReadGauge` | `ContextStore.ReadGauge` |
| `GaugePort.SetManagedSession` | `ContextStore.SetManagedSession` |
| `GaugePort.ClearPrecompactTrigger` | `ContextStore.ClearPrecompactTrigger` |
| `GaugePort.IdleMarkerModTime` | `ActivityProbe.IdleMarkerModTime` |
| `GaugePort.LastAssistantTurn` | `ActivityProbe.LastAssistantTurn` |
| latest user transcript turn in `GaugePort.Snapshot` | `ActivityProbe.LastUserTurn` |
| `GaugePort.Snapshot` | Removed. The shell composes `GateSnapshot` from the six source probes and `ActivityProbe` |
| `HandoffPort.HandoffPath` | `HandoffDocument.Path` |
| `HandoffPort.ReadHandoff` | `HandoffDocument.Read` |
| `HandoffPort.HandoffModTime` | `HandoffDocument.ModTime` |
| `HandoffPort.TruncateHandoff` | `HandoffDocument.ScrubNonce` |
| `HandoffPort.WriteJournal` | `CycleJournalStore.Write` |
| `HandoffPort.ReadJournal` | `CycleJournalStore.Read` |
| `RespawnPort.ForceRestart` | unchanged |

Small source ports own managed, crisp-idle, dispatch, sleep, hold, and operator-attachment predicates. A shell-owned sampler composes `GateSnapshot`. It does not promise cross-source atomicity.

## 4. Production-parity test scenarios

A shared builder creates cycle tests from production policy and controlled dependencies.

Requirements:

- A new scenario uses production gate values by default.
- A test must call a named method with a reason when it disables a production gate.
- Scenarios can record pane effects, file effects, emitted events, and timer actions.
- Scenarios use fake time.
- The builder supports the handoff and transcript collision sequence.

Dependencies: cycle policy and cycle dependencies.

Production gates include managed state, act threshold, crisp idle, dispatch hold, sleep, manual hold, operator-turn lookback, post-answer grace, anti-loop suppression, operator attachment, and boot grace. The builder exposes narrow methods such as `DisableOperatorTurnGate(reason)` instead of raw zero values.

## 5. Migration checks

Checks prove that the new construction path preserves behavior.

Requirements:

- Existing keeper tests pass during each migration step.
- A contract test compares old and new default policy values during migration.
- An end-to-end scenario uses the production constructor with fake external boundaries.
- No compatibility seam remains without a named removal task.
- Characterization tests cover success, transient deferral, timeout, restart recovery, and transcript collision.

Dependencies: all other components.

Migration order:

1. Add cycle policy and default-parity tests.
2. Add narrow contracts and adapters.
3. Add cycle dependency validation and the validated constructor.
4. Add the production wiring function.
5. Add the production-parity scenario builder.
6. Migrate production and test call sites.
7. Remove function seams, `fnPane`, `fnGauge`, and `fnHandoff`.

## Interface summary

The validated constructor combines `CyclePolicy`, `CycleEnv`, and `CycleDeps`. The production wiring function creates real dependencies and calls that constructor. The shell reads observations through narrow ports. It sends value events to the reactor. The reactor returns actions. The shell executes those actions through narrow effect ports. The scenario builder calls the validated constructor with fake boundaries and production policy.
