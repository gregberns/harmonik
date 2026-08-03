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
behavior recorded in `03-research/capability-behavior/findings.md`, required by
the selected daemon configuration before dispatch, or checked immediately
before its operation. An operation check uses the declared operation result. It
is not described as a pre-dispatch requirement.

The `runSessionSpawner` disposition is removal. It has no production assertion
or trigger. Alpha removes its unused private interface and unreachable concrete
branch. The final change must not retain an unowned sixteenth interface.

## Capability Decisions

### Check timing

A construction requirement is checked before `run_started` only when the
selected daemon configuration needs it for every possible dispatch. A missing
construction requirement may refuse ready state or dispatch. An operation
requirement is checked immediately before its operation. A run-plan or worker
choice can be known only after `run_started`. Its missing capability uses the
normal run-failure path. Crew-start checks occur before crew launch, not before
a daemon run.

| Capability | Consumer port | Requirement and absent result | Focused proof |
|---|---|---|---|
| `substrateWithAdapter` | Separate reconcile, quiesce, coordinator-reap, stale-probe, adoption, and crew-paste ports. | Optional for each consumer. Absence disables only that consumer's sweep, nudge, probe, adoption, or paste. | One base-only test per consumer and one adapter-provider test. |
| `substrateWithSessionName` | Reconcile session identity. | Required only when the daemon selected an owned session. Absence then fails before the sweep. Otherwise the owned-session exclusion stays empty. | Owned-session missing and ambient-session cases. |
| `substrateWithKeepalive` | Session keepalive. | Optional. Absence starts no keepalive goroutine. | Base-only no-goroutine case and provider start case. |
| `substrateWithSpawnCap` | Queue concurrency cap reader. | Optional. Absence is an unknown cap, not a fabricated cap. | Uncapped set-concurrency request and provider cap read. |
| `substrateWithSpawnCapSetter` | Queue concurrency cap writer. | Optional. Absence keeps the existing oversubscription refusal. | Refusal fallback and provider resize case. |
| `substrateSpawnReadier` | Restart-backoff readiness. | Optional. Absence makes dispatch immediately eligible after backoff. | No wait channel and provider wait or error case. |
| `substrateDiagnosticHookSetter` | Launch diagnostics. | Optional. Absence installs no hooks and emits no hook event. | Base-only and hook-provider cases. |
| `paneTargeter` | Per-run pane capture after `SpawnWindow`; independent-crew mission seed. | Optional for hosting. A missing target does not undo a successful ordinary spawn. A later pane input or capture operation returns its existing structural no-window error. The independent-crew mission seed skips when the session has no target. | Ordinary spawn without target; requested run input and capture fail structurally; crew mission seed skips. |
| `paneCaptureAdapter` | Run seed verification and crew mission seed verification. | Optional observation only. A successful input write is trusted when capture is unsupported. Capture never creates an input acknowledgment. | Run and crew provider verification; absent provider trusts a successful write. |
| `pasteInjecter` | Legacy daemon-run seed compatibility, cognition-gate seed, and reviewer re-seed. | This work does not select an input transport. On a tmux/Claude daemon run, `handler.InputPort` owns delivery and the AIS async event owns positive acceptance. Direct `WriteLastPane` is test-double compatibility until AIS C6 removes it. Absence skips launch and cognition-gate seeds and disables reviewer re-seed. It does not change workflow or invent a retry. | One absent and one provider case for every caller and harness row below. |
| `sessionCreator` | Independent crew session creation. | Required only when independent crew mode is selected. Absence returns `handler.ErrStructural` before crew launch. | Selected-crew structural failure and provider success. |
| `sessionEnsurer` | Local keepalive, local `ErrNoSession` recovery, readiness probe, and conditional remote worker-session bootstrap. | Optional. Local absence keeps no-op keepalive, no recovery retry, and ready-probe success. For remote launch, call `EnsureSession` when the swapped adapter offers it. A returned error is structural before `new-window`. This work does not declare missing `EnsureSession` a selected-mode failure. | Local no-op and recovery cases; remote offered-success, offered-error, and unavailable-capability cases. |
| `runnerSwapper` | Remote worker adapter conversion. | Operation-required when a run has selected a remote worker. Check immediately before any worker tmux operation. Absence returns `handler.ErrStructural`; no worker session ensure or window spawn occurs. The current work loop reaches this after `run_started`. | Remote missing-swapper and provider cases; assert no worker tmux call when absent. |
| `crewSessionSpawner` | Independent crew session. | Optional. Absence falls back to a daemon-session window. | Independent provider and fallback launch cases. |
| `crewSessionStopper` | Independent crew stop. | Optional. Absence falls back to `crewPaneStopper` when a handle exists, or makes no substrate stop call with no handle. | Both fallback cases and provider stop. |
| `runSessionSpawner` | None. | **Remove.** Delete `runSessionSpawner`, `tmuxSubstrate.SpawnRunSession`, `tmuxSubstrate.runSessionName`, `perRunSubstrate.runSessionID`, and the independent-session branch in `perRunSubstrate.SpawnWindow`. Delete run-only tests and documentation. Keep `sessionCreator`, `ErrTmuxNewSessionTimeout`, `callNewSessionBounded`, and `WithNewWindowTimeout` only for independent crew creation. | Source ratchet proves that no removed symbol or run-only test remains. Two concurrent shared-session runs with distinct captured pane targets produce distinct valid input buffer names. |

The selected result removes `runSessionSpawner`. It is not an implementation
choice left to Alpha. A later initiative may add a real run-session mode, but
it must introduce its trigger, port, failure rule, and tests in that work.

### Paste Caller and Harness Matrix

| Caller | Harness or path | Current absent result | Contract boundary |
|---|---|---|---|
| `pasteInjectOnLaunch` for initial, resumed, and reviewer launch | Tmux/Claude daemon run | No seed is sent. | `InputPort` owns daemon-run delivery. The AIS async event supplies positive acceptance. The bake fallback is not acceptance. |
| `pasteInjectOnLaunch` | Codex or Pi process-exit harness | `PasteTarget` is nil. No pane seed is attempted. | The harness receives input through its own launch path. |
| `pasteInjectCognitionGate` | Tmux cognition-gate pane | No gate seed is sent and its completion channel closes. | This is compatibility behavior. Step 14 does not define a gate input contract. |
| `pasteInjectQuitOnReviewFile` re-seed | Reviewer only | Re-seed is disabled. | It is a recovery aid, not primary delivery. |
| `pasteCrewMissionToSession` | Independent crew RPC, not a run | Mission seed skips when target or adapter is absent. | Best effort. AIS daemon-run input rules do not apply. |

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
| One declared contract | Resolve the typed capability record at construction. Check each consumer at its declared construction or operation boundary. |
| Honest missing behavior | Classify each capability as optional, construction-required, or operation-required. |
| No false tmux semantics | Keep the base substrate port narrow and use consumer ports. |
| Dispose of run-session drift | Remove the full run-only API, state, branch, tests, and docs. Keep input buffers unique from the captured pane target. |
| Preserve existing behavior | Test every absent path from the research table. |

## Tests

Alpha adds focused tests for a base-only substrate and one provider for each
capability group. Each test asserts the present behavior and the exact absent
behavior in the research table. The existing tmux substrate remains the
integration provider.
