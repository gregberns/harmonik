# Components — Declared Substrate Capability Contract

## Base substrate boundary

`handler.Substrate` is the public base dependency. The daemon currently asks
the runtime value for extra behavior with private type assertions. Step 14
must define the extra behavior without widening every consumer's base port.

## Capability families

The 16 private interfaces form four planning families:

1. **Session lifecycle:** `sessionCreator`, `sessionEnsurer`,
   `substrateWithSessionName`, `runSessionSpawner`, `crewSessionSpawner`, and
   `crewSessionStopper`.
2. **Pane and runner access:** `substrateWithAdapter`, `paneTargeter`,
   `paneCaptureAdapter`, `pasteInjecter`, and `runnerSwapper`.
3. **Launch control:** `substrateWithSpawnCap`, `substrateWithSpawnCapSetter`,
   `substrateSpawnReadier`, and `substrateWithKeepalive`.
4. **Diagnostics:** `substrateDiagnosticHookSetter`.

The design must decide which family is a declared optional capability and
which use remains internal to the tmux implementation. It must not expose an
adapter only because one caller currently wants it.

## Daemon wiring

`bootState.wireWatchersAndObservers` creates the quiesce adapter and installs
diagnostic hooks. Step 12 now owns subsystem switches in the same function.
The final Step 14 design must preserve those switch checks and must define one
edit order for that function.

`bootworkloop.go`, `scheduler.go`, and the run-path files consume the same
private adapter capability. Their absent behavior is part of the contract, not
an implementation detail.

## Test boundary

Focused test doubles must cover a base-only substrate, a substrate with each
declared capability family, and the current tmux implementation. Each absent
capability case must assert the current degraded behavior.

## Ownership boundary

Alpha owns the daemon-facing contract, the shared boot wiring, and the final
composition change. Bravo may receive a later scoped implementation handoff
for a non-daemon carrier and its focused tests. This Kerf work does not
authorize daemon edits.
