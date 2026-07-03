# 06 — Integration

## Cross-component contracts

The five components depend on each other only at well-defined seams. This section names every seam and verifies no spec drafts contradict.

### Seam 1 — `LaunchSpec.Substrate` (Component 2 ↔ Component 3)

Defined in: Component 2 design (`04-research/component-2/design.md` §4), referenced in Component 3 design (`04-research/component-3-4/design.md` §1, §0).

- Component 2 adds the `Substrate` interface to `internal/handler` package and exposes a tmux-backed implementation constructible from `tmux.OSAdapter` + resolved session name + project hash.
- Component 3's `buildClaudeLaunchSpec` writes `LaunchSpec.Substrate = substrate` when invoked from a daemon that has resolved tmux at startup. When `Substrate == nil`, `Handler.Launch` falls back to `exec.CommandContext` — preserves twin and unit-test paths.
- No contradiction: both designs agree that the bridge wire (CHB-018..025 NDJSON) flows through the daemon Unix socket, not through the pty.

### Seam 2 — `HookSessionStore` (Component 4 ↔ Component 3)

Defined in: Component 3+4 research (`04-research/component-3-4/findings.md` §HookSessionStore API surface).

- Component 4 adds `WaitForOutcome(ctx, runID, claudeSessionID) (json.RawMessage, error)` to the existing unexported `hookSessionStore`.
- Component 3 calls `RegisterHookSession` / `CloseHookSession` around each phase and `WaitForOutcome` from `waitWithSocketGrace`.
- Daemon composition root (Component 3) instantiates the store once and passes it both to the socket listener (Component 4) and into workloop deps (Component 3).
- No contradiction.

### Seam 3 — Sentinel-prefixed window names (Component 1 ↔ Component 2)

Defined in: `05-specs/workspace-model-amendments.md` WM-002a, `05-specs/process-lifecycle-amendments.md` PL-021b §3, §4 and PL-021c.

- WM-002a normatively defines the function `window_name(bead_id, phase, iteration_count)`; PL-021b §4 references it; PL-021c uses the `hk-<hash6>-` prefix to drive sweep at the window level.
- The three drafts agree on the prefix convention (`hk-<hash6>-` in $TMUX-reuse mode; bare name in owns-session mode).
- Component 2's `windowname.go` implements WM-002a verbatim.
- No contradiction.

### Seam 4 — Tmux availability (Component 1 ↔ Component 2 ↔ Component 3)

Defined in: PL-021b §2 (probe at PL-005 step 4), PL-028b (refusal when `$TMUX` unset).

- Component 2's `tmux.OSAdapter.Available()` runs at PL-005 step 4 from daemon composition root (Component 3).
- Component 2's `ResolveSession()` runs immediately after; ErrNoSession → daemon exit 24 (PL-028b).
- All three drafts agree on exit code mapping (22 = tmux missing; 24 = no session).

### Seam 5 — AdapterRegistry forwarding (Component 3 latent)

Defined in: `04-research/component-3-4/design.md` §6.

- Component 3 changes `handler.NewHandler` signature to accept `*handlercontract.AdapterRegistry`. The handler stores but does not consult it (latent seam, closes the daemon.go:298 TODO).
- No runtime contract change; no contradiction with any spec.

### Seam 6 — Heartbeat emission ownership (Component 1 ↔ Component 3)

Defined in: `05-specs/handler-contract-amendments.md` HC-057, `04-research/component-3-4/design.md` §4.

- HC-057 permissively carves out daemon-side emission for `agent_type=claude-code` at MVH; Component 3's workloop emits via `handler.RunHeartbeatLoop` with a daemon-side emitter.
- CHB-019 unchanged ("the handler-process emits heartbeats") — HC-057 is a permissive interpretation, not a CHB revision.
- No contradiction.

### Seam 7 — `agent_ready` timeout (Component 1 ↔ Component 3)

Defined in: HC-056, Component 3 design §5.

- HC-056 normatively introduces the timeout; Component 3 implements `waitAgentReady`.
- Closes follow-up bead `hk-do7te`.
- No contradiction.

## Initialization order

Daemon startup sequence (extends PL-005):

1. Process-lifecycle Cat 0 pre-check: `tmux.OSAdapter.Available()` (PL-021b §2). Exit 22 on failure.
2. Compose composition-root primitives: bus, hookSessionStore, BrAdapter, tmuxAdapter, substrate factory.
3. `tmux.OSAdapter.ResolveSession(projectHash)`. Exit 24 on ErrNoSession (PL-028b).
4. Orphan sweep — session-level (existing PL-006) AND window-level (new PL-021c). Emit `daemon_orphan_sweep_completed` with both counters.
5. AdapterRegistry construct + register + seal (existing; now also passed into `handler.NewHandler`).
6. Bind Unix socket; start `RunSocketListener` with `hookSessionStore` as HookRelayHandler.
7. Emit `daemon_started`; begin work loop.

## Shared state

- `hookSessionStore` — owned by daemon composition root; mutated by socket listener (write) and workloop (read via WaitForOutcome / LatestOutcome). Single internal mutex preserves CHB-026 ordering.
- `tmuxAdapter` — stateless wrapper around `tmux` binary; safe for concurrent use (each method opens its own subprocess).
- `substrate` (constructed from tmuxAdapter) — holds the resolved sessionName and ownsSession bool. Read-only after construction; threaded into LaunchSpec values.
- `Run.context` (existing) — workloop appends `claude_session_id` per CHB-023 and now also `handler_session_id` and `tmux_window_name`.

## Cross-component error handling

- Tmux unavailable at startup → exit 22 before any pidfile/socket binds. No event emitted (event bus not yet running).
- `$TMUX` unset at startup → exit 24 before pidfile/socket binds. Stderr directive names `hk tmux-start`.
- Window-creation collision in workloop → workloop reopens bead with reason `tmux_window_collision`; orphan sweep kills the colliding window on next startup.
- Settings.json materialization failure → workloop reopens bead with reason `settings_materialize_failed`; not retried inside the same workloop iteration.
- agent_ready timeout → kill, reap, reopen with reason `agent_ready_timeout`; emit `agent_failed{class=structural, sub_reason=agent_ready_timeout}`.
- Stop hook never arrives → after `stopHookGrace = 3s` post Wait-return, derive terminal event from exit code (CHB-020 branch 3).
- Forbidden flag detected pre-launch → workloop reopens bead with `forbidden_flag_detected`; no exec attempted.

## SPEC.md assembly

A consolidated normative SPEC.md is unnecessary at this initiative scope: every normative requirement lands in an existing repo-level spec file (`specs/process-lifecycle.md`, `specs/workspace-model.md`, `specs/handler-contract.md`) and the drafts in `05-specs/` are paste-ready. The kerf-internal SPEC.md role is satisfied by the union of those four draft files plus this integration doc. No additional cross-spec contradictions discovered during review.
