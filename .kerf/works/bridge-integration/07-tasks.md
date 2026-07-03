# 07 — Tasks

Bead decomposition for the bridge-integration initiative. ~20 child beads under one umbrella epic, organized into four streams plus a smoke gate. Each row maps a kerf-component acceptance criterion to a concrete deliverable; deps form a DAG; parallel-safe groups are called out.

## Umbrella

| ID | Title | Type | P | Notes |
|---|---|---|---|---|
| (epic) | bridge-integration: wire CHB pieces into daemon + tmux substrate | epic | 0 | Parent of A/B/C/D/E streams. Closes when E1 (single-mode smoke) is GREEN. |

## Stream A — Spec amendments (parallel; lands first)

| ID | Title | Type | P | Depends on | Notes |
|---|---|---|---|---|---|
| A1 | Apply PL amendments: PL-021b (direct-tmux substrate), PL-021c (window orphan sweep), PL-028 refinement (`hk tmux-start`), PL-028b ($TMUX-unset refusal) | task | 1 | — | Paste from `05-specs/process-lifecycle-amendments.md` into `specs/process-lifecycle.md`. Adds change-log row. |
| A2 | Apply WM-002a amendment: deterministic tmux window-name derivation | task | 1 | — | Paste from `05-specs/workspace-model-amendments.md` into `specs/workspace-model.md`. |
| A3 | Apply HC amendments: HC-054 (Attach pty contract), HC-055 (claude-flag allow-list), HC-056 (agent_ready timeout), HC-057 (heartbeat ownership MVH carve-out) | task | 1 | — | Paste from `05-specs/handler-contract-amendments.md` into `specs/handler-contract.md`. |
| A4 | CHB decision record: no amendment needed (twin parity stays wire-level) | docs | 2 | — | Add a one-paragraph decision record to `specs/claude-hook-bridge.md` change-log noting why CHB-028 is not filed. |

## Stream B — Tmux substrate (parallel after A2; some internal deps)

| ID | Title | Type | P | Depends on | Notes |
|---|---|---|---|---|---|
| B0 | (epic) Stream B umbrella — `internal/lifecycle/tmux` package + `hk tmux-start` subcommand | epic | 1 | A1 | Closes when B1..B6 land. |
| B1 | Create `internal/lifecycle/tmux` package skeleton: Adapter interface, error sentinels, types | task | 1 | A1 | New package. Depguard component-matrix entry; doc.go with spec citations. |
| B2 | Implement `OSAdapter`: probe tmux, list-sessions, list-windows, new-window, kill-window, display-message pane_pid | task | 1 | B1 | Shells out to tmux 3.0+; unit-tested with fake tmux on PATH. |
| B3 | Implement `WindowName` per WM-002a (single + review-loop + sentinel-prefix; truncation) | task | 1 | B1, A2 | Pure function; table tests. |
| B4 | Window-level orphan sweep (PL-021c): list windows across all sessions, kill `hk-<hash6>-` matches, emit `tmux_windows_killed` in payload | task | 1 | B2, A1 | Extends `SweepOrphanTmuxSessions` pattern. |
| B5 | Implement `hk tmux-start` subcommand per PL-028 refinement: probe `$TMUX`, ensure session, exec into attach | task | 1 | B2 | New subcommand in `cmd/harmonik`. |
| B6 | Add `Substrate` interface + `SubstrateSpawn`/`SubstrateSession` types to `internal/handler`; tmuxsubstrate adapter (constructor in daemon package) | task | 1 | B2 | Substrate seam threaded into `LaunchSpec`. |

## Stream C — Bridge daemon wiring (some parallel; mostly serial after C1)

| ID | Title | Type | P | Depends on | Notes |
|---|---|---|---|---|---|
| C0 | (epic) Stream C umbrella — daemon-side bridge wiring | epic | 1 | — | Closes when C1..C6 land. |
| C1 | Implement `buildClaudeLaunchSpec` helper in `internal/daemon/claudelaunchspec.go` | task | 1 | A1, A2, A3 | Single-mode + review-loop both call this. |
| C2 | Wire single-mode workloop to use `buildClaudeLaunchSpec`, emit PreExecMessages before Launch, start heartbeat goroutine | task | 1 | C1, B6, D2 | Replaces lines 486–541 of `workloop.go`. |
| C3 | Wire review-loop to use `buildClaudeLaunchSpec` per phase (implementer-initial / implementer-resume / reviewer); preserve CHB-009 fresh-reviewer-mint and CHB-023 StdoutWrapper | task | 1 | C1, B6, D2 | Edits `reviewloop.go`. |
| C4 | Forward AdapterRegistry into `handler.NewHandler` (close daemon.go:298 TODO) | task | 2 | — | Signature change; latent seam (no runtime behavior change). Unit test for nil-registry panic. |
| C5 | Daemon-side `agent_heartbeat` emission goroutine wrapped around `RunHeartbeatLoop` (HC-057) | task | 2 | A3 | Used by C2 and C3. |
| C6 | Implement `waitAgentReady` with 30s default timeout (HC-056); close `hk-do7te` follow-up bead | task | 1 | A3 | Observer goroutine on event bus filtered by run_id. Closes existing `hk-do7te`. |

## Stream D — Completion path (sequential)

| ID | Title | Type | P | Depends on | Notes |
|---|---|---|---|---|---|
| D0 | (epic) Stream D umbrella — Stop-hook completion path | epic | 1 | — | Closes when D1..D3 land. |
| D1 | Add `WaitForOutcome(ctx, runID, claudeSessID)` to `hookSessionStore` (notify channels, additive method) | task | 1 | — | Internal to `internal/daemon`; unit-tested. |
| D2 | Wire `hookSessionStore` into `RunSocketListener` from `daemon.Start` (`hr` arg currently `nil`); pass store into workloop deps | task | 1 | D1 | Closes the integration gap at `socket.go:124`. |
| D3 | Implement `waitWithSocketGrace` in workloop completion path with 3s post-Wait grace window (OQ2 resolution) | task | 1 | D1, D2, C2 | New helper consumed by C2 and C3. |

## Stream E — Dogfood smoke gate

| ID | Title | Type | P | Depends on | Notes |
|---|---|---|---|---|---|
| E1 | Re-run dogfood smoke against real claude with full bridge wired; produce `docs/dogfood-smoke-run-<date>-bridge.md` | task | 1 | A1,A2,A3, B0, C2, D3 | Closes existing `hk-w5vra.7` and possibly parent `hk-w5vra` + epic `hk-1n0cw` if GREEN. |
| E2 | (stretch) Re-run smoke with `workflow:review-loop` bead; verify iteration loop terminates correctly through bridge | task | 2 | E1 | Stretch within the initiative; OK to split into follow-up if E1 surfaces gaps. |

## Already-filed follow-ups subsumed

- `hk-nvrvp` (HARMONIK_PROJECT_HASH not injected) — naturally closed by C1 (`baseEnv` includes provenance env via `AppendProvenanceEnv`).
- `hk-do7te` (no agent_ready timeout) — naturally closed by C6.
- `hk-mz0x4` (binary_commit_hash always "unknown") — adjacent but NOT in scope; stays open.
- `hk-w5vra.7` — closed by E1 if GREEN.
- `hk-v34bz` (tmux pane bead I filed earlier today) — superseded by Stream B; close as duplicate.

## Parallelism waves

- **Wave 1** (4 parallel, pure-doc): A1, A2, A3, A4.
- **Wave 2** (3 parallel after A): B1, D1, C4.
- **Wave 3** (4 parallel after their deps): B2, B3, C1, C5.
- **Wave 4** (5 parallel after their deps): B4, B5, B6, C6, D2.
- **Wave 5** (2 parallel): C2, C3.
- **Wave 6** (1): D3.
- **Wave 7** (1, gate): E1.
- **Wave 8** (stretch): E2.

Maximum concurrency = 5 (Wave 4). Critical path = A* → C1 → C2 → D3 → E1 (~5 hops).

## Acceptance traceability

Every success criterion from §"Success criteria" of `01-problem-space.md` is covered:

| Success criterion | Beads |
|---|---|
| SC1 Real-claude single-mode smoke GREEN | E1 (depends on all above) |
| SC2 Review-loop smoke GREEN (stretch) | E2 |
| SC3 Operator can attach | B2, B5 |
| SC4 Standalone tmux-start | B5 |
| SC5 Determinism check | B3, A2 |
| SC6 Orphan recovery | B4 |
| SC7 Twin parity | C1 (twin-blind), D3 (substrate-engagement keyed on agent_type) |
| SC8 Spec amendments merged | A1, A2, A3, A4 |
