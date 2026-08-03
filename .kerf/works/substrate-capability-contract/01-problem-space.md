# Problem Space — Declared Substrate Capability Contract

## Summary

The daemon discovers substrate behavior through 16 private interfaces and 30
comma-ok assertions. A second substrate must know those hidden contracts before
it can behave correctly. An absent capability can silently disable a feature.

Step 14 must replace that discovery pattern with a declared contract. The
contract must preserve the explicit degraded behavior for capabilities that a
substrate does not provide.

## Goals

- State the capability surface that the daemon may require or probe.
- Make each absent-capability path deliberate, observable, and testable.
- Let a second substrate implement the declared surface without daemon-private
  knowledge.
- Keep the current tmux behavior and the non-tmux fallback behavior intact.

## Measured Evidence

The 16 interfaces are `substrateWithAdapter`, `substrateWithSessionName`,
`substrateWithKeepalive`, `substrateWithSpawnCap`,
`substrateWithSpawnCapSetter`, `substrateSpawnReadier`,
`substrateDiagnosticHookSetter`, `paneTargeter`, `paneCaptureAdapter`,
`pasteInjecter`, `sessionCreator`, `sessionEnsurer`, `runnerSwapper`,
`crewSessionSpawner`, `crewSessionStopper`, and `runSessionSpawner`.

There are 30 production probes in 11 `internal/daemon` files. Eleven are in
`tmuxsubstrate.go`. The other 19 are in `bootreconcile.go`, `bootsocket.go`,
`bootstate.go`, `bootworkloop.go`, `crewstart.go`, `daemon.go`,
`dot_cascade_core.go`, `dot_gate.go`, `pasteinject.go`, and `scheduler.go`.

The shared Step 12 site is `bootState.wireWatchersAndObservers`. It probes
`substrateWithAdapter` for the quiesce adapter and
`substrateDiagnosticHookSetter` for diagnostic hooks. Step 12 now adds
subsystem switches in that function. Step 14 must preserve those switches and
must not edit the shared hunk in parallel with Step 12.

## Boundaries

Step 10 and Step 13 are complete. This work does not restore the removed
imperative workflow tail. It does not change workflow selection, the no-review
DOT graph, or the planned tmux-host package move.

The event-bus extension interfaces are a separate surface. Consumer-defined
one-method ports are also outside this work. They are deliberate dependency
narrowing, not capability discovery.

## Ownership

Alpha defines the daemon-facing contract, owns the final contract decision,
and performs the daemon composition change. Bravo takes a scoped
implementation handoff after that contract is accepted. Bravo may implement
the non-daemon carrier and focused tests. Bravo must not change daemon call
sites before Alpha's handoff.

## Constraints

- A capability is optional only when its missing behavior is explicit and
  tested.
- The contract must not require every tmux helper from every substrate.
- The `wireWatchersAndObservers` switch behavior must survive the change.
- Do not create a general substrate framework or a second composition root.

## Success Criteria

- The final design accounts for all 16 interfaces and 30 production probes.
- Every retained optional capability has a defined missing path and a test.
- The final contract states Alpha's daemon boundary and Bravo's scoped work.
- The Step 12 collision has a clear serialization plan.

## Possible Spec Areas

- `specs/handler-contract.md`
- `specs/execution-model.md`
- `specs/process-lifecycle.md`
