# 03 — Components

Four components, matching the design doc's layer map (§2) and recommended kerf shape (§4). DAG: T1 and T2
are independent (parallel); T3 depends on both (needs T1's candidate-order primitives + T2's hub/predicate
primitives proven first); T4 depends on the specific T1/T2 beads that produced the B1/B2 evidence.

## T1 — L0 pure-projection suite
**Requirement → goal:** closes G1 (predicate table), G2 (presence TTL matrix), G3 (wake-candidate ordering
L0 half); hardens existing cursor-race coverage.
**Interface:** pure functions over JSONL fixtures — `eventbus.ScanAfter`, `presence.ComputeRegistry`,
`daemon.NewCursorStore`. No daemon, no concurrency, no tmux.
**Components (4 beads, independent, fan-out):**
- N1 `MatchAgentMessage` predicate table (directed/broadcast/topic/from-wildcard).
- Presence TTL + `EffectiveLastSeen` clock matrix (scenario 4 half).
- Cursor monotonicity / no-regress-under-race (harden existing).
- `commsWakePaneCandidates` / `resolveProjectPath` ordering + symlink-hash (scenario 6 L0 half).

## T2 — L1 in-process bus/hub suite
**Requirement → goal:** closes G1 (N3 fan-out+dedupe, B1 pin), G4 (back-pressure liveness); satisfies spec
N1/N2 (live==replay boundary).
**Interface:** `eventbus.NewBusImpl*` → `Subscribe`/`Seal`/`Emit`; `daemon.NewSubscribeHub` with injected
`Now`/`NewTimer`; `net.Pipe()` fake client.
**Components (4 beads, independent, fan-out):**
- N1/N2 live==replay boundary (scenario 3).
- N3 multi-consumer fan-out + recipient dedupe on `event_id` (scenario 2).
- B1 follow-starves-recv pin + inline doc (scenario 1).
- Back-pressure drop-oldest + `subscription_gap` emission (G4 liveness).

## T3 — L2 socket/CLI e2e (serial tail)
**Requirement → goal:** closes G4 (daemon-restart no-loss), G3 (wake real-pane paste + dead-pane-delivers),
G6 (socket-level multi-consumer sanity).
**Interface:** `scratch-daemon.sh` clean-reset real daemon + real `harmonik comms` CLI processes + tmux.
**Dependency:** runs AFTER T1 (needs the L0 candidate-order assertions as a base) and T2 (needs the L1
fan-out primitives proven) — and must run as ONE serial queue, not fanned out, because process-kill and
tmux tests race the same daemon socket.
**Components (3 beads):**
- Daemon-restart reconnect, no message loss (scenario 5).
- `--wake` real-pane paste + dead-pane-still-delivers (scenario 6 L2 half).
- One multi-consumer socket sanity run (G6 e2e).

## T4 — spec/doc reconciliation (operator-in-the-loop)
**Requirement → goal:** pins B1 (cursor-sharing) and B2 (who-vs-pane gap) as executable specs + doc notes,
per the explicit non-goal of NOT changing runtime behavior without operator sign-off.
**Interface:** the agent-comms skill doc + an executable assertion referencing the T1 presence-matrix bead
and the T2 B1-pin bead.
**Dependency:** depends on T1's presence-TTL bead and T2's B1-pin bead (needs their evidence to write the
spec correctly). Blocks nothing else in-epic; gates on operator input before any related code changes.

## Interfaces between components
T1 → T4 (presence-TTL evidence), T2 → T4 (B1-pin evidence), T1+T2 → T3 (proven primitives feed the serial
L2 tail). No circular dependencies. 4 components, within the 3–7 range.
