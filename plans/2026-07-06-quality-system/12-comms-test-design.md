# 12 — Comms Bus Test-Validation System

**Scope:** the `harmonik comms` inter-agent message bus (send/recv/log/join/leave/who + `--follow`/`--wake`) and the `harmonik subscribe` run-event stream, both projected off the append-only event log via `internal/eventbus`.

**Authoritative sources read:**
- Skill: `.claude/skills/agent-comms/SKILL.md` (operating contract; N3 dedupe, flag-split trap, presence-refresh rule).
- Spec (FINALIZED 2026-06-01): `~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md` — Q1 (daemon-persisted cursor), Q2 (join/leave + 60s refresh / 120s TTL), Q3 (at-least-once), N1 (single shared `matchAgentMessage` predicate), N2 (replay-before-follow), N3 (recipient dedupe on `event_id`). NB: `specs/event-model.md` does **not** carry the comms taxonomy — the bench draft is canonical.
- Source: `cmd/harmonik/comms.go` (1907 lines, thin socket client); `internal/eventbus/{busimpl.go,jsonlwriter.go}`; `internal/daemon/{commscursor.go,commsrecvhandler_nnwaa.go,subscribe.go,scheduletick.go}`; `internal/presence/presence.go`.
- Corpus/plans: `plans/2026-07-06-quality-system/{00-SYNTHESIS.md,06-phase2-plan.md,02-bug-corpus-classification.md}`; `BUGS.md` (B1/B2/B3).

**Headline finding — comms has the LOWEST shared-infra dependency of any quality lane, and it is already partly seam-tested.** The whole surface is pure projection off a JSONL log plus a Unix-socket RPC. Almost every defect class is reproducible with (a) a JSONL fixture + pure projection call, or (b) an in-process `SubscribeHub` with an injected clock — **no Docker, no scripted-twin, no digital twin**. Only two defect classes (tmux `--wake`, daemon-restart reconnect) need a real socket. This lane can be designed AND built now, fully in parallel with the daemon-dispatch/testbed spine.

A second finding: the codebase **already carries several comms scenario tests** — `scenario_comms_recv_follow_gap_hk7xvf_test.go`, `scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go`, `commscursor_race_hkfvo9e_test.go`, `comms_send_scheduled_hk0lwje_test.go`, `scenario_comms_recv_follow_e2e_hkyw5c_test.go`. This lane is therefore **harden-and-close-gaps**, not greenfield — the acceptance corpus below marks each scenario as EXISTS / EXTEND / NEW so the captain doesn't rebuild what's there.

---

## 1. Problem space — what actually breaks in comms

Grouped by mechanism, each citing the real incident and the code seam.

### G1 — Cursor / delivery semantics (the highest-traffic operator-visible failure)
- **B1: `recv --agent` silently drains 0 when a `--follow` watcher is armed** (`BUGS.md` B1). Root cause is architectural, not a bug per se: **both** the one-shot `HandleCommsRecv` and the `SubscribeHub` advance the **same** daemon-owned cursor (`subscribe.go:390-404` batches delivered `event_id`s into `pendingCursorID`, flushed every `commsCursorFlushInterval=2s` and on return; the one-shot handler advances at `commsrecvhandler_nnwaa.go:217`). An armed `recv --follow` therefore consumes the cursor, so a later `recv --agent <me>` legitimately returns nothing — messages *were* delivered, to the follower. This is **correct at-least-once behavior that reads as a stall.** The test-system job here is not "fix a bug" but **pin the semantics with an executable assertion and document them** so the operator stops reading it as breakage.
- **recv-drains-0 + lost-gap (GH#8 / hk-7xvf):** when a drain matches nothing and no cursor is stored, `CursorAfter=""`; passing `since_event_id=""` to subscribe skips replay, losing events landing between drain-complete and hub-registration. Fixed via `ScanAnchor` fallback (`commsrecvhandler_nnwaa.go:190`, `comms.go:1416`). Test EXISTS (`scenario_comms_recv_follow_gap_hk7xvf_test.go`) — keep it green, it's a regression sentinel.
- **N3 at-least-once + dedupe (spec N3):** cursor advances *after* the batch returns (`commsrecvhandler_nnwaa.go:215-221`), so a crash between scan and advance replays the batch. Daemon does **not** dedupe across calls — dedupe on `event_id` is the recipient's NORMATIVE duty. Test EXISTS (`scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go`).
- **N1 single-predicate integrity (spec N1):** exactly one `MatchAgentMessage(payload, to, from, topic)` must serve replay, live-offer, and durable `comms-recv`. Two copies of addressing logic = catch-up/live message-set divergence. Spec mandates a shared table-driven test asserting live==replay verdicts.

### G2 — Presence / TTL false-offline
- **B2: presence ages out at ~120s → live idle crews show `stale`/offline in `comms who`** (`BUGS.md` B2). `presence.go:48` `TTL=120s`, `:58` `StaleCutoff=10m`; `GetState` (`:100-112`) buckets by `age = time.Since(EffectiveLastSeen)`. `EffectiveLastSeen = max(presence beat, latest agent_message send ts)` (`:140,199-207`) — so a crew that is **alive but not sending, receiving, or beating** for >120s reads Stale even though its pane is live. Idle `--follow` does **not** refresh presence; receiving does **not** either (skill lines 224-227). Drives false reconcile/restart churn. The gap the test-system must close: **`comms who` truth vs pane truth.** Either assert the documented aging semantics deterministically (injected clock) OR flag the who-vs-pane gap as a spec question for the captain.
- **O-class fragility:** `agent_presence` is not fsync'd, so a daemon crash drops refresh beats and ages an agent artificially (mitigated only by F-class `agent_message` synthesis, `presence.go:212`).

### G3 — `--wake` delivery to idle panes (tmux, best-effort, silently swallowed)
- `--wake` nudges the recipient's tmux pane after send (`comms.go:346-352`, directed-only). Failure modes: (a) **project-hash mismatch** — a symlinked project path hashes to a nonexistent session unless EvalSymlinks-then-hashed (`resolveProjectPath`, `comms.go:372`, the hk-z365 fix); (b) **captain-vs-crew naming asymmetry** (`reference_comms_wake_captain_pane_mismatch`) — captain pane is `harmonik-<hash>-<name>` with no `crew-` prefix and no registry record, candidate (3) in `commsWakePaneCandidates` (`comms.go:403`); (c) **any tmux failure is swallowed to stderr, exit code unaffected** — a "successful" send can silently not-wake. `comms --wake can't reach an idle pane` in MEMORY. Not in the task corpus; comms-lane only.

### G4 — Subscribe lifecycle across daemon restart
- `harmonik subscribe` (and `recv --follow`, which is subscribe under the hood) is an **in-process transient** stream: hub map + pre-`Seal()` bus registration exist only in daemon memory (`subscribe.go:250,345`). Daemon exit → socket close → client `json.Decoder` EOF. **The cursor survives on disk; the live stream does not.** Recovery is entirely client-side: `runCommsRecvFollowIO` (`comms.go:1484`) re-dials with 1s→10s backoff (hk-5xuvc) and re-anchors at `lastSeen`, which advances from delivered event_ids AND heartbeat `last_event_id` (EV-037a, `comms.go:1621`) to avoid watermark regression in quiet periods. `reference_watch_subscribe_dies_on_daemon_restart`, `reference_watch_cursor_frozen_false_positive`. Related: 256-slot drop-oldest back-pressure emits `subscription_gap{dropped:N}` (`subscribe.go:567`) — drop affects liveness only; `recv`/`--follow` re-reads dropped events from the durable log.

### G5 — Scheduled-send
- `internal/daemon/scheduletick.go`. **hk-0lwje:** every scheduled comms-send failed at flag-parse because it passed `--body` as a flag (no such flag → exit 1) and omitted `--from` for watch jobs (→ exit 1). Fixed: body emitted as positional after `--` (`commsSendArgv:321`), `--from` always supplied. Test EXISTS (`comms_send_scheduled_hk0lwje_test.go`) — drives corrected argv through the real parser, asserts failure only at socket-connect not flag-parse. `scheduled-comms-send --body de-hardcode bug` in MEMORY. NB: there is **no** general scheduled-send verb in the spec/skill — this is the daemon's internal watch-job send path only.

### G6 — Multi-consumer ordering / fan-out
- Broadcast (`to=="*"`) + directed delivery share the `MatchAgentMessage` predicate; each consumer has an independent per-agent cursor (`CursorStore` one file per agent, `commscursor.go`). No cross-consumer test currently asserts that **N independent consumers each receive every addressed/broadcast message at-least-once**. This is the main NEW coverage gap.
- **Zombie keys on outbound traffic** (`reference_watch_zombie_keys_on_outbound_traffic`, MEMORY) — presence/registry residue on send; watch-lane, lower priority.

---

## 2. What to test + at what layer

The code exposes three clean seams (confirmed in source). Map each defect class to the **cheapest faithful** layer.

**L0 — pure projection, JSONL fixture, zero concurrency, zero daemon.**
Primitives: `eventbus.ScanAfter(path, sinceID)` (`jsonlwriter.go:312`), `presence.ComputeRegistry(eventsPath)` + `GetState/IsOnline/IsStale/IsOffline`, `daemon.NewCursorStore(dir).Get/Advance/AgentMu`. Deterministic — write events, assert states.
- Cursor monotonicity + no-regress-under-race → L0 (`commscursor_race_hkfvo9e_test.go` EXISTS).
- Presence TTL bucketing (G2), `EffectiveLastSeen` = max(beat, send) → L0 with a fixture + injected `now`.
- `MatchAgentMessage` predicate table (N1: directed/broadcast/topic/from wildcards) → L0, the shared table-driven test the spec mandates.

**L1 — in-process bus + `SubscribeHub`, injected clock/timer, `net.Pipe()` fake client. No socket, no real time.**
Primitives: `eventbus.NewBusImpl*` → `Subscribe` → `Seal` → `Emit`; `daemon.NewSubscribeHub(SubscribeHubConfig{Bus, EventsJSONLPath, PresenceEmitter, Now, NewTimer})` — `Now`/`NewTimer` are explicit injection points; `HandleSubscribe(ctx, conn, req)` takes any `net.Conn`.
- N1 live==replay verdict equality → L1 (register hub, replay + live, assert identical set).
- N2 replay-before-follow (no catch-up→live gap) → L1.
- N3 at-least-once redelivery + recipient dedupe (G1) → L1 (`scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go` EXISTS, sits at daemon-scenario level; an L1 variant is cheaper for the pure cursor-advance-timing assertion).
- Cursor-shared-between-follow-and-oneshot (B1) → L1: arm a `SubscribeHub` follower, then call `HandleCommsRecv` for the same agent, assert the documented drain-0 and pin it.
- 256-slot drop-oldest + `subscription_gap` emission (G4 back-pressure) → L1 with injected timer.
- recv-drains-0 lost-gap `ScanAnchor` fallback (G1) → L1/scenario (`scenario_comms_recv_follow_gap_hk7xvf_test.go` EXISTS).

**L2 — real daemon over the Unix socket / CLI e2e. Needed ONLY for what can't be faked in-process.**
Primitive: `scratch-daemon.sh` clean-reset daemon + dial `<project>/.harmonik/daemon.sock`, or drive the CLI. This is the **lighter comms-only harness — a real daemon socket + N `harmonik comms` client processes, NO Docker, NO twin.**
- `--wake` tmux pane resolution (G3): hash-of-symlinked-path, captain-vs-crew candidate order, swallowed-failure-still-exits-0. Needs a real tmux + real session naming → L2. (Can partly unit-test `commsWakePaneCandidates` + `resolveProjectPath` at L0 for the candidate ordering and symlink-hash, leaving only the actual paste to L2.)
- Daemon-restart reconnect (G4): kill the daemon mid-`--follow`, assert client backoff + re-anchor + no message loss across the restart → L2 (needs a real process to kill).
- Multi-consumer fan-out over the socket (G6) end-to-end → L2 (the in-process version is L1; one L2 sanity run confirms the socket path).
- Scheduled-send argv (G5) → the EXISTING test drives the real parser and stops at socket-connect; keep at that level.

**Explicit shared-infra verdict:** **nothing in comms needs the Phase-2 shared substrate.** Confirmed against `06-phase2-plan.md`: the scripted-twin (chunk 2) injects at the `claude`-spawn seam (`agent_overrides`) — the agent boundary, not the comms/cursor/presence surface; Docker Layer-0 (chunk 3, greenfield) is reserved for env/disk dials. Comms defects are pure eventbus-client behavior with **no environment dependency**. L2 reuses only the `scratch-daemon.sh` clean-reset primitive (already exists), which is far lighter than the twin or Docker.

---

## 3. Acceptance corpus — top 6 scenarios to reproduce + assert

Ordered by operator-visible impact. Each: the assertion, the layer, and EXISTS/EXTEND/NEW.

1. **B1 — follow-starves-recv is documented + pinned (G1).** Arm `recv --follow` for agent A; send 3 messages to A; then call `recv --agent A`. Assert it returns 0 **because the follower consumed the shared cursor**, and assert `comms log --since <window>` (cursor-independent) still shows all 3. Codify as the intended semantics with an inline doc comment + a skill note. **Layer L1** (+ one L2 CLI reproduction for the operator-facing behavior). **NEW** (the drain-0 gap test exists for the lost-gap variant, not for the follow-vs-recv cursor-sharing case).

2. **N3 — two consumers each receive every message at-least-once and dedupe on `event_id` (G6+G1).** Two independent agents A,B; broadcast 5 + directed-to-A 3 + directed-to-B 2; simulate a crash between scan and cursor-advance (drop the advance) and re-drain. Assert: every consumer sees every message addressed to it (broadcast + own-directed), redelivery reproduces the pre-advance batch, and a recipient that tracks seen `event_id`s treats redelivery as no-op. **Layer L1** (fan-out) + keep `scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go` as the daemon-level sentinel. **EXTEND** (single-consumer redelivery exists; multi-consumer fan-out is NEW).

3. **N1/N2 — live and replay deliver the identical message set across the catch-up→live boundary (G1).** Register hub, emit a backlog, start `--follow` from a mid-backlog anchor, then emit live events. Assert the union has no gap and no duplicate at the boundary, and that a table of (to/from/topic/broadcast) addressing cases returns identical verdicts on the live path and the replay path (the spec-mandated shared-predicate test). **Layer L1** (+ L0 for the predicate table). **NEW** (spec mandates it; not yet present).

4. **B2 — presence reflects a live-but-idle client correctly, or the who-vs-pane gap is closed (G2).** Inject a join beat at t0, advance injected clock to t0+119s → assert Online; to t0+121s with no further beat → assert Stale; add a `send` at t0+90s → assert `EffectiveLastSeen` keeps it Online past 120s. Then the **gap assertion**: an idle `--follow`-only agent gets NO refresh → document that `comms who` will show it Stale and that pane-truth (`tmux capture-pane`) is the reconcile authority. Surface to the captain as a spec question: *should idle `--follow` emit a refresh beat?* **Layer L0** (deterministic clock). **NEW.**

5. **G4 — a `--follow`/`subscribe` client survives a daemon restart with no message loss.** Start `--follow` for A; send 2; restart the daemon (kill + `scratch-daemon.sh`); send 2 more. Assert the client reconnects (backoff observed), re-anchors at its last watermark (delivered + heartbeat `last_event_id`), and ultimately delivers all 4 with no gap and correct dedupe. **Layer L2** (needs a real process to kill). **NEW** (memory-noted behavior, no regression test).

6. **G3 — `--wake` targets the right pane and never masks a delivery.** L0: `commsWakePaneCandidates` returns crew→convention→captain-bare order, and `resolveProjectPath` EvalSymlinks-then-hashes so a symlinked project resolves to the live session name (hk-z365). L2: a real send `--wake` to a live tmux pane pastes+Enter; a send `--wake` to a **dead** pane still exits 0 and still delivers the message (failure to stderr only). **Layer L0 candidate-ordering + L2 paste.** **EXTEND** (partial coverage may exist for `resolveProjectPath`; the swallowed-failure-still-delivers assertion is NEW).

Keep-green sentinels (already exist, fold into the epic's CI gate, do not rebuild): `scenario_comms_recv_follow_gap_hk7xvf_test.go`, `scenario_comms_recv_follow_e2e_hkyw5c_test.go`, `commscursor_race_hkfvo9e_test.go`, `comms_send_scheduled_hk0lwje_test.go` (G5).

---

## 4. Shared-infra dependency + parallelizability

**Verdict: BUILD NOW, fully parallel. Comms is the lowest-coupling lane in the quality initiative.**
- **Zero dependency on the Phase-2 spine.** No twin, no Docker, no digital-twin. L0/L1 need only the in-tree seams that already exist and are already used by shipped comms tests. L2 needs only `scratch-daemon.sh` (exists) + tmux — both already present. This lane does **not** wait on chunk-1 core-loop-proof, the assertion package, or the scratch-substrate Dockerfile.
- **Immediately available seams (all confirmed in source):** `eventbus.ScanAfter`, `eventbus.NewBusImpl*`, `daemon.NewCursorStore`, `daemon.NewSubscribeHub` (injected `Now`/`NewTimer`), `presence.ComputeRegistry`, `net.Pipe()` for a fake `HandleSubscribe` client, `runCommsRecvFollowIO(..., w io.Writer)` for driving `--follow` without global stdout. The test ergonomics are already good — this is why it can move fast.
- **Sequencing inside the lane:** L0+L1 (scenarios 1,2,3,4,6-candidate) can all be authored concurrently by separate worktree agents — they share no files beyond the fixtures. L2 (scenarios 5,6-paste) is a smaller serial tail because daemon-restart and tmux tests can race the socket; run those in one queue, not fanned out.

### Recommended kerf shape (one epic + tranched tasks)
Follow the initiative's pattern (`00-SYNTHESIS.md`: one kerf work, tranched, per-chunk `integration/<codename>` epic reaching main via one human PR after the assessor gate passes on open P0/P1 `found-by:*` beads).

- **Kerf work / epic:** `codename:comms-test-harness`, branch `integration/comms-test-harness`. Bead-label `codename:comms-test-harness` per the project convention.
- **Tranche T1 — L0 pure-projection suite (parallel, ~4 beads):** predicate table (N1); presence TTL/`EffectiveLastSeen` clock matrix (scenario 4); cursor monotonicity/no-regress (harden existing); `commsWakePaneCandidates`/`resolveProjectPath` ordering + symlink-hash (scenario 6 L0 half).
- **Tranche T2 — L1 in-process bus/hub suite (parallel, ~4 beads):** N1/N2 live==replay boundary (scenario 3); N3 multi-consumer fan-out + dedupe (scenario 2); B1 follow-starves-recv pin + doc (scenario 1); back-pressure drop-oldest + `subscription_gap` (G4 liveness).
- **Tranche T3 — L2 socket/CLI e2e (serial tail, ~3 beads):** daemon-restart reconnect no-loss (scenario 5); `--wake` real-pane paste + dead-pane-still-delivers (scenario 6 L2 half); one multi-consumer socket sanity run.
- **Tranche T4 — spec/doc reconciliation (1-2 beads):** fold the B1 cursor-sharing semantics and the B2 who-vs-pane gap into the agent-comms skill + raise the "should idle `--follow` refresh presence?" spec question. This is where the two *design questions* (not bugs) get resolved with the operator.
- **Gate:** the assessor runs the L0/L1/L2 suites on the isolated scratch clone; the epic BLOCK set = open P0/P1 `found-by:comms-test-harness` beads; one human PR to main on green.
- **Staffing note for the captain:** T1 and T2 are ideal fan-out (8 independent worktree beads, no shared files); T3 must run as a single serial queue (process-kill + tmux tests race the socket). T4 is operator-in-the-loop — surface the two design questions, don't self-resolve.

---

## Exec summary (for the admiral)

**What breaks in comms, 6 groups:** (G1) cursor/delivery — B1 `recv` drains 0 because `--follow` shares the daemon cursor, plus N3 at-least-once + recipient dedupe and the lost-gap `ScanAnchor` fix; (G2) presence false-offline — B2 live-but-idle crews age out at 120s, `comms who` ≠ pane truth; (G3) `--wake` tmux delivery to idle panes — hash/symlink + captain-vs-crew naming, failures swallowed to stderr; (G4) subscribe dies on daemon restart — in-process transient stream, client-side backoff reconnect; (G5) scheduled-send `--body` de-hardcode (hk-0lwje, fixed+tested); (G6) multi-consumer fan-out — no test asserts N consumers each get every message.
**Acceptance corpus (6):** 1) B1 follow-starves-recv pinned+documented; 2) two consumers each receive every message at-least-once + dedupe on `event_id`; 3) live==replay identical set across catch-up→live boundary (N1/N2); 4) presence correct for a live-idle client (or who-vs-pane gap closed); 5) `--follow` survives a daemon restart with no loss; 6) `--wake` hits the right pane and never masks delivery. Several sentinels already ship (gap, redelivery-dedupe, cursor-race, scheduled-send) — this lane HARDENS + closes gaps, not greenfield.
**Can-build-in-parallel: YES — highest-priority parallel candidate.** Comms has ZERO dependency on the Phase-2 shared substrate (no Docker, no twin, no digital-twin). L0/L1 use in-tree seams that already exist (`ScanAfter`, `NewBusImpl`, `NewCursorStore`, `NewSubscribeHub` with injected clock, `net.Pipe`); only L2 (`--wake`, daemon-restart) needs a real socket, served by the existing `scratch-daemon.sh` clean-reset — lighter than anything the daemon-dispatch lane requires. Kerf as one epic `codename:comms-test-harness`, tranches T1(L0)/T2(L1) fanned out, T3(L2) serial tail, T4 operator-in-loop spec reconciliation.
