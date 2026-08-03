# Research — Capability Behavior

## Method

The current source has 16 declared private interfaces and 30 production
comma-ok assertions in 11 daemon files. The count excludes tests.

```sh
rg -n -g '*.go' -g '!**/*_test.go' \
  '\.\((substrateWithAdapter|substrateWithSessionName|substrateWithKeepalive|substrateWithSpawnCap|substrateWithSpawnCapSetter|substrateSpawnReadier|substrateDiagnosticHookSetter|paneTargeter|paneCaptureAdapter|pasteInjecter|sessionCreator|sessionEnsurer|runnerSwapper|crewSessionSpawner|crewSessionStopper|runSessionSpawner)\)' \
  internal/daemon | wc -l
```

## Missing-Capability Behavior

| Capability | Sites | Current behavior when absent | Planning result |
|---|---:|---|---|
| `substrateWithAdapter` | 6 | Boot skips orphan-window sweep data, quiesce nudging, periodic coordinator reaping, stale-run process-dead probes, surviving-run adoption, or crew mission paste. | Split these consumer outcomes. Do not give one generic adapter fallback. |
| `substrateWithSessionName` | 1 | The daemon-owned session name stays empty. The sweep cannot exclude it. | Treat absence as safe only when no daemon-owned session exists. Otherwise require an explicit failure or configuration rule. |
| `substrateWithKeepalive` | 1 | No keepalive goroutine starts. | Keep as an explicit best-effort optional behavior. |
| `substrateWithSpawnCap` | 1 | Queue concurrency does not know the substrate cap. | Define uncapped or unknown-cap behavior. |
| `substrateWithSpawnCapSetter` | 1 | Queue rejects an oversubscribing concurrency request. | Keep the explicit refusal fallback. |
| `substrateSpawnReadier` | 1 | Post-backoff dispatch has no readiness probe or wait channel. | Prove immediate non-blocking dispatch is the absent behavior. |
| `substrateDiagnosticHookSetter` | 1 | Spawn-cap and new-window timeout event hooks are not installed. | Preserve explicit observability degradation. |
| `paneTargeter` | 4 | Crew mission paste is skipped. Per-run pane targets stay empty, so later pane input or capture returns a structural no-window error. | Decide which managed-spawn cases require a target and pin each path. |
| `paneCaptureAdapter` | 2 | Capture returns `errPaneCaptureUnsupported`. Seed verification trusts a successful write. | Keep the explicit degraded verification behavior. |
| `pasteInjecter` | 3 | Launch and cognition paste are skipped. Reviewer re-seed is disabled. | Decide optionality by harness. Make absence visible where delivery matters. |
| `sessionCreator` | 2 | Independent crew or run session creation returns `handler.ErrStructural`. | Treat as required for each selected independent-session mode. |
| `sessionEnsurer` | 4 | Keepalive is a no-op. Local missing-session recovery does not retry. Readiness returns nil. Remote pre-ensure is skipped and later spawn can fail. | Separate local best-effort recovery from the remote required precondition. |
| `runnerSwapper` | 1 | Remote launch fails with `handler.ErrStructural`. | Keep as required for remote launch only. |
| `crewSessionSpawner` | 1 | Crew launch falls back to a window in the daemon session. | Keep and test the explicit fallback. |
| `crewSessionStopper` | 1 | Stop falls back to `crewPaneStopper` when it has a handle. With no handle, it makes no substrate stop call. | Keep and test both fallback cases. |
| `runSessionSpawner` | 0 | No assertion exists. `perRunSubstrate` calls concrete `*tmuxSubstrate.SpawnRunSession` when `runSessionID` is set, but current production code does not assign that trigger. | The design must remove this unused interface and unreachable concrete path, or make the trigger and port real. It must not remain an unowned sixteenth interface. |

## Grouping

The smallest useful groups are:

1. Boot and lifecycle observations: adapter, session identity, keepalive,
   readiness, diagnostics, and cap reader or setter.
2. Run input and observation: pane target, paste, and capture.
3. Tmux recovery and remote launch: session ensure or create, and runner swap.
4. Crew lifecycle: crew spawn and stop.

Do not make these four groups one wide interface. A consumer should receive
only the capability it uses.

## Step 12 Serialization Evidence

`bootState.wireWatchersAndObservers` owns both the quiesce adapter extraction
and diagnostic hook installation. Step 12 owns subsystem-switch construction
in the same function. Step 14 must wait for the Step 12 change at this symbol.
The later integrated edit must keep the same switch guards and the same absent
substrate behavior before it replaces the two assertions.
