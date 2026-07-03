# Operator Dashboard + Mailbox — DESIGN

**Date:** 2026-07-03. **Status:** design-only (no code, no beads created by this doc).
**Author:** design pass, grounded in the live surfaces cited below.
**Parks this addresses:** `HANDOFF-admiral.md` "⚠️ PARKED" item **#2 (operator mailbox)** —
and, as a byproduct, gives item **#1 (fleet-must-not-stall)** a single place to *surface*,
and item **#3 (role-decoupled-from-agent)** a clean crew↔lane map to render.

> One-line thesis: **do not build two new subsystems.** harmonik already ships (a) a live
> fleet-state snapshot (`harmonik state`, `internal/daemon/stategather.go` + `statetypes.go`,
> spec `system-state.md`) that is the dashboard's live spine, and (b) a durable, event-sourced,
> fsync'd operator-decision queue (`hitl-decisions`: `decisions-raise/list/answer/wait`,
> `internal/daemon/decisionshandler_*.go`, `cmd/harmonik/decisions*.go`) that IS the mailbox
> mechanism. This design **composes those two**, adds a thin captain-curated *planning* layer
> (priorities / throughput / crew-map that live state can't self-derive), and binds a
> **forcing mechanism** so the captain keeps that layer current.

---

## 0. What already exists (studied first, cited)

| Need | Existing surface | Where |
|---|---|---|
| Live runs, queues, sessions, per-session context/cognition, work-axes, activity roll-up | **`harmonik state [--json]`** → `StateSnapshot` (SS-001), built by `LiveStateBuilder.Build` | `internal/daemon/stategather.go:78`, `statetypes.go:28`; socket op `"state"` `socket.go:742`; spec `specs/system-state.md` |
| Run → owning epic + owning assignee (crew) | `StateRun.OwningEpicID` / `OwningAssignee` (the Gap-1 `br show --assignee` mirror) | `statetypes.go:62-63` |
| Durable operator escalations w/ ack + async answer | **`hitl-decisions`**: `decisions-raise` (emits `decision_needed`, mints `decision_id`), `decisions-list`, `decisions-answer`, client `wait` (N8 arm-then-check); F-class fsync'd, projected over `events.jsonl` | `internal/daemon/decisionshandler_xz9.go`, `decisionshandler_k4_kba.go`; `cmd/harmonik/decisions.go`; spec `specs/hitl-decisions.md` |
| Operator-escalation event (already typed) | `operator_escalation_required` / `_cleared` | `internal/core/eventtype.go:513,630` |
| Lane ↔ epic ↔ crew ↔ queue map (priorities) | `.harmonik/context/lanes.json` (admiral-owned, machine-readable, ops-monitor reads it) | `.harmonik/context/lanes.json` |
| Throughput (per-run wall-time + tokens + cost) | **WS1 session-data** `<project>/.harmonik/session-data.jsonl` (design in `plans/2026-07-03-eval-program/01-…md` §2) — one normalized record per run, `emitDone` hook | not yet built (that doc's beads #2/#3/#4) |
| Bottleneck / stall signals | **stall-sentinel** `stall_detected` events + ops-monitor `latest.json` | `plans/2026-07-02-stall-sentinel/DESIGN.md`; `.harmonik/ops-monitor/latest.json` |
| Operator-intent doc (objectives/antigoals/directives) | `goalstate` `.harmonik/intent/goal-state.json` | `internal/goalstate/` |

**Design consequence:** the dashboard is a **read-time JOIN** over these sources plus ONE new
captain-curated file; the mailbox is a **thin rename/superset of `hitl-decisions`**. Net new code
is small.

---

## 1. Dashboard data model

Two tiers, by who can derive them:

**Tier A — machine-derived (no captain action; already live or WS1).** Sourced by joining
`StateSnapshot` + `session-data.jsonl` + `lanes.json` + `stall_detected`/`latest.json` on
`{run_id, epic_id, queue, agent}`:

- **Agents-assigned + health** ← `StateSnapshot.Sessions[]` (agent, session_type, alive,
  context fill-frac + band, sid_desync, sleep/at_rest).
- **Runs in flight** ← `StateSnapshot.Runs[]` (run_id, bead, queue, epic, assignee, lifecycle_state, age).
- **Crew ↔ epic ↔ queue map** ← join `Runs[].OwningEpicID/OwningAssignee` with `lanes.json`
  (`lane`, `epic_id`, `crew`, `queue`, `status`, `gate`). This is the item-#3 role/lane map.
- **Throughput ACTUAL** ← aggregate `session-data.jsonl` over a window: beads closed, mean/p50
  wall-time per bead, tokens+cost per outcome, grouped by crew/queue/harness/model.
- **Bottlenecks** ← active `stall_detected` events (per-run heartbeat-gap / review-stall / lane
  no-forward-progress) + `latest.json` flags (paused-by-failure queues, stale crews,
  spawn_cap, backlog-ready-but-idle). Each renders as a row with signature + age + owning lane.
- **Work-done log** ← `bead_closed` + `run_completed` from `events.jsonl` over the window
  (the append-only ledger IS the log; the dashboard just tails+joins it to crew/epic).

**Tier B — captain-curated (the planning layer live state CANNOT self-derive).** A single new
file **`.harmonik/context/dashboard.json`** (admiral/captain-owned, sibling to `lanes.json`):

```jsonc
{
  "schema_version": 1,
  "updated": "2026-07-03T17:00:00Z",         // freshness stamp — the forcing-gate key (§3)
  "updated_by": "captain",
  "priorities_current": [                     // ranked NOW — plain-English, lane-keyed
    {"rank": 1, "lane": "pi-sandbox", "epic_id": null, "crew": "leto",
     "headline": "Pi-in-a-sandbox via srt so DGX-local models can run",
     "expected": "acceptance test green by EOD"}
  ],
  "priorities_future": [                      // on-deck / next, ranked
    {"lane": "stall-sentinel", "headline": "deterministic fleet-must-not-stall detector",
     "gate": "after sandbox completes"}
  ],
  "throughput_expected": [                    // the ONLY expected-vs-actual input a human sets
    {"lane": "pi-sandbox", "beads_expected": 4, "by": "2026-07-03T20:00:00Z"}
  ],
  "notes": "free-text operator-facing status, <=3 lines"
}
```

The dashboard renders **expected-vs-actual** by pairing Tier-B `throughput_expected` against
Tier-A actuals from `session-data.jsonl` (e.g. "pi-sandbox: 2 of 4 expected beads closed, on
pace / behind"). `priorities_*` and the crew map come from Tier B joined to `lanes.json`.

**Why a NEW file, not extend `lanes.json`:** `lanes.json` is the ops-monitor's machine index
(strict schema, gate-parse-sensitive per `.harmonik/context/CLAUDE.md`); adding operator-facing
prose + expected-throughput to it risks the ops-monitor stall-wake logic. Keep `lanes.json` the
staffing index; `dashboard.json` is the *narrative + expectations* layer that references lanes by
`lane` key. One join key (`lane`), two files, single ownership (admiral/captain).

## 2. Where it LIVES + how it's SURFACED

**Lives in daemon state as a read-time projection — NOT a new persisted store.** Add a daemon
socket op **`"dashboard"`** (mirrors the `"state"` op, `socket.go:742`) whose handler builds a
`DashboardSnapshot` = `LiveStateBuilder.Build()` (reuse verbatim) + reads of `dashboard.json`,
`lanes.json`, a windowed `session-data.jsonl` aggregation, and open `decisions-list` +
`stall_detected`. The daemon already owns run-registry/queues/sessions; the three files are
read-only joins. **No new durable store** — the durable substrate is the three files that already
exist plus `events.jsonl`.

**Surface = a CLI first, a rendered file second (both, one code path):**
- **`harmonik dashboard [--json]`** — the primary surface. `--json` for machines; default renders
  a compact operator-readable panel (priorities, crew↔lane table with health, expected-vs-actual
  bars, bottleneck rows, mailbox-unread count). Same builder as the socket op.
- **A rendered snapshot file** `.harmonik/dashboard/latest.md` (+ `latest.json`), written by the
  same ops-monitor loop that already writes `latest.json`, so an operator can `cat`/tail it or a
  future served page can read it without a live socket. This reuses the existing ops-monitor
  cadence — no new scheduler (avoids the "session-hosted probe" trap the sentinel doc flags).
- **TUI / served page = explicitly deferred.** The `--json` op + rendered file are the stable
  contract; a TUI or `harmonik dashboard --serve` HTTP page is a later skin over the same JSON.
  (Open decision D1 below.)

## 3. Operator mailbox

**Build it as `hitl-decisions` with an operator-escalation flavor — do NOT invent a second bus.**
The comms bus (`harmonik comms`) is ephemeral at-least-once and pane-nudge-oriented; that is
exactly the "dumping on a tmux pane" the operator rejected. `hitl-decisions` is already the
durable, ordered, fsync'd, ack'd, async-answerable operator queue. Reuse its full lifecycle:

- **POST (crew/captain → operator):** `harmonik decisions raise --topic operator-mailbox
  --from <agent> --urgency <blocker|question|fyi> --body "<plain-English issue + options+consequences>"`.
  Emits `decision_needed` (F-class, fsync'd before return), mints a `decision_id`. Distinct from
  the comms bus — it lands in the durable projection, survives restarts, is NOT a pane write.
- **READ (operator, async):** `harmonik mailbox` (thin alias of `decisions list --topic
  operator-mailbox`) → the open-item set from the K3 projection over `events.jsonl`; also rendered
  as the dashboard's "mailbox" section with an unread count. The operator answers on their own clock.
- **RESPOND + ACK:** `harmonik decisions answer <decision_id> --body "<answer>"` emits the
  terminal `decision_answered`; the raising agent's `decisions wait` (N8 arm-then-check) unblocks
  and consumes it. Delivery is the durable projection (at-least-once + terminal-state fold), not a
  live subscribe — so an answer given while the crew was `/clear`'d is still delivered on resume.
- **Compose with stall-sentinel Tier-3:** the sentinel's operator tier (§3 of its DESIGN) fires a
  `decisions raise --topic operator-mailbox --from stall-sentinel` instead of a pane dump — the
  mailbox is the sentinel's Tier-3 sink. Same for reconciliation's existing
  `operator_escalation_required` (Cat-6): route those into the mailbox projection too, so ALL
  operator-facing escalations (crew-raised, sentinel-raised, daemon-raised) land in ONE inbox.

**Net new work here is small:** a `--topic operator-mailbox` convention + `urgency` field on the
raise payload, the `harmonik mailbox` alias, the dashboard "mailbox" render, and pointing
sentinel-Tier-3 + reconciliation escalations at it. The durable queue, ack, and async-answer
already exist and are spec'd.

## 4. Forcing mechanism — RECOMMENDATION

**Options considered:**
- **(A) Stale-dashboard blocks new dispatch (a gate).** If `dashboard.json.updated` is older than
  `dashboard_max_staleness` (config, fail-loud), the daemon **refuses to dispatch new beads** to
  captain-owned queues (in-flight runs finish; the queue just won't pull the next item) and emits
  `dashboard_stale`. The captain clears it by refreshing `dashboard.json`. "The crew wouldn't work
  without it" — literally true: staffing halts until the picture is current.
- **(B) Required periodic dashboard-heartbeat the keeper enforces.** Treat dashboard-refresh like
  a keeper duty: on each keeper tick, if `dashboard.json` is stale, the keeper `send-keys` a nudge
  to the captain pane. Soft — a nudge, not a gate; a wedged captain ignores it (the exact failure
  mode the sentinel doc warns about).
- **(C) Crew-launch precondition.** `harmonik start crew` refuses unless the lane exists in a fresh
  `dashboard.json`. Only bites at spawn time; a long-running fleet drifts stale between spawns.

**RECOMMEND (A) as the primary gate, with (B) as the soft pre-nag — hybrid.** (A) is the only
option that makes the forcing property *structural* rather than *behavioral*: the operator's steer
was "the crew wouldn't work without it," and a dispatch-gate delivers exactly that — a stale
dashboard degrades the fleet to "finish in-flight, staff nothing new" until refreshed, which is
also a *safe* degradation (it never kills running work). (B) as a pre-expiry keeper nudge gives
the captain a chance to refresh *before* the gate bites, so the gate rarely actually fires.

**Trade-off / guardrails (the make-or-break, mirroring stall-sentinel's Layer-B guard):**
- The gate MUST be **scoped to captain-curated lanes only** and MUST have an **operator override**
  (`harmonik dashboard --unlock` / a config kill-switch) — a forcing mechanism that can wedge the
  whole fleet on a bookkeeping file is worse than the problem. It degrades staffing, never aborts a
  run, never blocks the mailbox, never blocks a daemon-core/reconcile path.
- `dashboard_max_staleness` is **config-driven, fail-loud if unset** (per the no-hardcoded-threshold
  mandate — resolve like keeper/sentinel config). A generous default window (e.g. operator sets
  30–60 min) so normal captain cadence never trips it; only a *silently dead* captain does.
- Emit `dashboard_stale` / `dashboard_refreshed` typed events so the watch/ops-monitor can surface
  "captain stopped curating" — which is itself a fleet-health signal (a stale dashboard ≈ a
  wedged captain, tying back to park item #1).

---

## 5. Open decisions (for operator/consensus)

- **D1 — surface ceiling for v1:** CLI + rendered `latest.md` file (recommended), OR go straight to
  a served `--serve` HTTP page / TUI. Rec: CLI+file first (stable JSON contract), page later.
- **D2 — forcing-gate blast radius:** gate blocks dispatch to *all* captain-owned queues (rec) vs
  only the specific stale lane's queue. Narrower is safer but weaker forcing.
- **D3 — mailbox = extend `hitl-decisions` with an `urgency`/`topic` convention (rec)** vs a
  net-new `operator-mailbox` subsystem. Rec strongly favors reuse; flag only because it touches the
  finalized hitl-decisions spec (a spec addition, operator sign-off per the spec-change rule).
- **D4 — expected-throughput authorship:** operator sets `throughput_expected` per lane (rec, keeps
  it honest) vs the captain estimates it (risks self-graded targets).

---

## 6. Beads / kerf-work to create (NOT created by this doc)

Recommend **one kerf work `codename:operator-dashboard`** with these beads. Hard dependency:
several are gated on WS1 session-data (`plans/2026-07-03-eval-program` beads #2/#3/#4) for the
throughput axis and on stall-sentinel for the bottleneck axis — dashboard renders those axes as
"unavailable" until they land, so it is NOT hard-blocked, only degraded.

1. **`dashboard.json` schema + read/write store** — new file under `.harmonik/context/`, Go
   read/write (mirror `goalstate` store), schema doc. *P1.* (task)
2. **`DashboardSnapshot` builder + `"dashboard"` socket op + `harmonik dashboard [--json]` CLI** —
   join `LiveStateBuilder.Build()` + `dashboard.json` + `lanes.json` + windowed
   `session-data.jsonl` + open decisions + `stall_detected`. *P1 — the core.* (feature/epic)
3. **Rendered `latest.md` + `latest.json`** written by the ops-monitor loop (reuse cadence, no new
   scheduler). *P2.* (task)
4. **Mailbox: `--topic operator-mailbox` + `urgency` field on `decisions raise`; `harmonik
   mailbox` alias; dashboard mailbox render.** Spec addition to `hitl-decisions.md` (D3 sign-off).
   *P1.* (task; touches finalized spec)
5. **Route sentinel-Tier-3 + reconciliation `operator_escalation_required` into the mailbox
   projection** — one inbox for all operator escalations. *P2 — depends on stall-sentinel.* (task)
6. **Forcing gate: `dashboard_max_staleness` config (fail-loud) + dispatch-gate on stale
   captain-lane dispatch + `dashboard_stale`/`_refreshed` events + `--unlock` override + keeper
   pre-nag.** *P1 — the operator's forcing-mechanism ask.* (feature)
7. **Captain/crew skill updates** — `captain`/`crew-launch` skills: "refresh `dashboard.json` on
   every staffing change + on the keeper nag; the gate blocks dispatch if stale." Make the duty
   canonical alongside the existing lanes.json forced-write rule. *P2.* (docs/skill)
8. **Throughput aggregation view over `session-data.jsonl`** — windowed roll-up (beads closed,
   wall-time, tokens/cost per crew/queue/harness/model) feeding the expected-vs-actual render.
   *P2 — depends on WS1 session-data beads.* (task)
```
