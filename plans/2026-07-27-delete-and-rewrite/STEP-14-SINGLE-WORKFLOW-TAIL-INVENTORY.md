# Step 14 — substrate capability contract inventory

**Status:** source rechecked 2026-08-02 at `2224d2bfa`. No production code changed.

## Correction

This file used to describe the imperative single-workflow tail. That tail is
gone. Legacy `workflow:single` inputs now select the registered no-review DOT
graph. The old inventory was not Step 14 work and must not direct a new agent.

## Scope

Step 14 replaces the daemon's optional substrate type assertions with one
declared capability contract. The measured surface is 16 capability interfaces
and 30 production probes in 11 `internal/daemon` files. The capability is
optional only when its absent-path behavior is explicit and tested.

`internal/daemon/tmuxsubstrate.go` holds 11 probes. The other 19 are in
`bootreconcile.go`, `bootsocket.go`, `bootstate.go`, `bootworkloop.go`,
`crewstart.go`, `daemon.go`, `dot_cascade_core.go`, `dot_gate.go`,
`pasteinject.go`, and `scheduler.go`.

The important shared sites are:

- `bootState.wireWatchersAndObservers` reads `substrateWithAdapter` for the
  quiesce adapter and `substrateDiagnosticHookSetter` for diagnostic hooks.
- `newCoordinatorReapPort` and `wireStaleWatcherReapSeams` in
  `bootworkloop.go` read `substrateWithAdapter`.
- `extractTmuxAdapterFromSubstrate` in `scheduler.go` reads the same adapter.

## Order and ownership

Step 10 and Step 13 are complete. Step 14 can now start its required Kerf
design. Alpha owns the daemon contract and implementation because every live
probe is in `internal/daemon`. Bravo owns the planning work only.

1. Create a new Kerf spec work named `substrate-capability-contract`.
2. Re-measure all 30 probes and record each absent-path behavior.
3. Decide the minimal declared capability surface. Do not turn every tmux
   helper into a required method.
4. Define test doubles that prove both capability-present and capability-absent
   behavior.
5. Serialize the `wireWatchersAndObservers` edits with Step 12. The subsystem
   switches are now present there, so the Kerf design must preserve them.
6. Hand the finished contract to Alpha for implementation and composition.

Re-measure before each implementation pass:

```sh
rg -n -g '*.go' -g '!**/*_test.go' \
  '\.\((substrateWithAdapter|substrateWithSessionName|substrateWithKeepalive|substrateWithSpawnCap|substrateWithSpawnCapSetter|substrateSpawnReadier|substrateDiagnosticHookSetter|paneTargeter|paneCaptureAdapter|pasteInjecter|sessionCreator|sessionEnsurer|runnerSwapper|crewSessionSpawner|crewSessionStopper|runSessionSpawner)\)' \
  internal/daemon | wc -l
```

## Exclusions

- Do not restore the imperative single-workflow tail.
- Do not change no-review graph selection or workflow-mode compatibility.
- Do not begin the planned tmux-host package move. It needs Alpha's separate
  contract-first handoff.
- Do not combine this work with Step 27a. That is a separate public daemon
  configuration constructor contract.
