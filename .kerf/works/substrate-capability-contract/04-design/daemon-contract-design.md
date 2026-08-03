# Design — Daemon Capability Contract

## Current State

Daemon consumers use 30 private type assertions to discover 16 capabilities.
The behavior when an assertion fails is scattered. `runSessionSpawner` has no
assertion and its only concrete path has no production trigger.

## Target State

Alpha defines a daemon-owned, typed capability resolution boundary. It resolves
the selected substrate once during daemon construction. Consumers receive only
the narrow capability value they use. They do not type-assert the base
`handler.Substrate` value.

The boundary has four groups:

1. Boot and lifecycle observations.
2. Run input and observation.
3. Tmux recovery and remote launch.
4. Crew lifecycle.

Each group may expose smaller consumer ports. The design forbids one wide
interface that combines all 16 methods. A capability is either absent with the
behavior recorded in `03-research/capability-behavior/findings.md`, or required
for the selected mode and rejected before dispatch with a structural error.

The `runSessionSpawner` disposition is removal. It has no production assertion
or trigger. Alpha removes its unused private interface and unreachable concrete
branch. The final change must not retain an unowned sixteenth interface.

## Capability Decisions

| Capability | Consumer port | Requirement and absent result | Focused proof |
|---|---|---|---|
| `substrateWithAdapter` | Separate reconcile, quiesce, coordinator-reap, stale-probe, adoption, and crew-paste ports. | Optional for each consumer. Absence disables only that consumer's sweep, nudge, probe, adoption, or paste. | One base-only test per consumer and one adapter-provider test. |
| `substrateWithSessionName` | Reconcile session identity. | Required only when the daemon selected an owned session. Absence then fails before the sweep. Otherwise the owned-session exclusion stays empty. | Owned-session missing and ambient-session cases. |
| `substrateWithKeepalive` | Session keepalive. | Optional. Absence starts no keepalive goroutine. | Base-only no-goroutine case and provider start case. |
| `substrateWithSpawnCap` | Queue concurrency cap reader. | Optional. Absence is an unknown cap, not a fabricated cap. | Uncapped set-concurrency request and provider cap read. |
| `substrateWithSpawnCapSetter` | Queue concurrency cap writer. | Optional. Absence keeps the existing oversubscription refusal. | Refusal fallback and provider resize case. |
| `substrateSpawnReadier` | Restart-backoff readiness. | Optional. Absence makes dispatch immediately eligible after backoff. | No wait channel and provider wait or error case. |
| `substrateDiagnosticHookSetter` | Launch diagnostics. | Optional. Absence installs no hooks and emits no hook event. | Base-only and hook-provider cases. |
| `paneTargeter` | Crew paste and managed-run pane observation. | Optional for ordinary spawn. Required only when a selected managed operation requests pane input or capture. Absence skips crew paste and makes that later operation structural. | Crew skip, ordinary spawn, and requested capture failure cases. |
| `paneCaptureAdapter` | Crew and run pane observation. | Optional. Absence returns `errPaneCaptureUnsupported` and keeps blind write trust. | Both capture callers with and without a provider. |
| `pasteInjecter` | Legacy launch, cognition, and reviewer re-seed. | Optional pending the AIS deletion boundary. Absence skips legacy paste and re-seed. | One absent and one provider case for each caller. |
| `sessionCreator` | Independent crew session creation. | Required only when independent crew mode is selected. Absence returns `handler.ErrStructural` before crew launch. | Selected-crew structural failure and provider success. |
| `sessionEnsurer` | Keepalive, local recovery, readiness, and remote pre-ensure. | Optional for local keepalive and recovery. Required for selected remote launch. Absence keeps local no-op behavior and fails remote launch before spawn. | Local no-op, local recovery, and remote structural cases. |
| `runnerSwapper` | Remote tmux runner. | Required for selected remote launch. Absence returns `handler.ErrStructural` before spawn. | Remote structural failure and provider success. |
| `crewSessionSpawner` | Independent crew session. | Optional. Absence falls back to a daemon-session window. | Independent provider and fallback launch cases. |
| `crewSessionStopper` | Independent crew stop. | Optional. Absence falls back to `crewPaneStopper` when a handle exists, or makes no substrate stop call with no handle. | Both fallback cases and provider stop. |
| `runSessionSpawner` | None. | **Remove.** Delete `runSessionSpawner`, `tmuxSubstrate.SpawnRunSession`, `tmuxSubstrate.runSessionName`, `perRunSubstrate.runSessionID`, and the independent-session branch in `perRunSubstrate.SpawnWindow`. Delete run-only tests and documentation. Keep `sessionCreator`, `ErrTmuxNewSessionTimeout`, `callNewSessionBounded`, and `WithNewWindowTimeout` only for independent crew creation. | Source ratchet proves that no removed symbol or run-only test remains. Two concurrent shared-session runs with distinct captured pane targets produce distinct valid input buffer names. |

The selected result removes `runSessionSpawner`. It is not an implementation
choice left to Alpha. A later initiative may add a real run-session mode, but
it must introduce its trigger, port, failure rule, and tests in that work.

## Run-Only Removal Closure

`inputBufferName` must not retain `runSessionID` as a hidden dependency. After
the removal, it derives its identifier from the run-local captured pane target.
That target is set only by the successful selected shared-session or remote
spawn. The name uses its sanitized value and the existing `input` purpose.
The fallback literal remains only for the pre-spawn error path, where no pane
write occurs.

The focused regression test creates two concurrent per-run wrappers in the
same shared session. It gives each wrapper a distinct captured pane target. It
then proves that their input buffer names are valid and different. This keeps
input isolation without reviving an independent run-session trigger.

## Alpha and Bravo Split

Alpha owns the capability record or ports, daemon configuration, all daemon
consumer cutovers, and final composition. Bravo receives work only after Alpha
lands that contract. Bravo may implement a named non-daemon carrier and its
focused tests. Bravo must not change `internal/daemon` to discover a contract.

## Requirements Traceability

| Planning requirement | Design response |
|---|---|
| One declared contract | Resolve typed capabilities once at construction. |
| Honest missing behavior | Classify each capability as optional or required by selected mode. |
| No false tmux semantics | Keep the base substrate port narrow and use consumer ports. |
| Dispose of run-session drift | Remove the full run-only API, state, branch, tests, and docs. Keep input buffers unique from the captured pane target. |
| Preserve existing behavior | Test every absent path from the research table. |

## Tests

Alpha adds focused tests for a base-only substrate and one provider for each
capability group. Each test asserts the present behavior and the exact absent
behavior in the research table. The existing tmux substrate remains the
integration provider.
