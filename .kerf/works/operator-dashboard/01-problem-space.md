# Operator Dashboard + Mailbox — Problem Space

**Authoritative design:** `plans/2026-07-03-operator-dashboard/DESIGN.md` (read that first — this
artifact summarizes and points at it; the DESIGN is normative).

## Summary

Give the operator (a) a single **dashboard** view of what the fleet is doing / should be doing /
how fast, and (b) a single durable **mailbox** where all operator-facing escalations land — without
building two new subsystems. The dashboard is a **read-time JOIN** over surfaces that already exist
(`harmonik state` / `LiveStateBuilder.Build()`, `lanes.json`, WS1 `session-data.jsonl`,
`stall_detected` / ops-monitor `latest.json`) plus ONE new captain-curated file
`.harmonik/context/dashboard.json`. The mailbox is a thin superset of the finalized `hitl-decisions`
queue (durable, ordered, fsync'd, ack'd, async-answerable). A **forcing gate** keeps the captain's
planning layer current: a stale `dashboard.json` degrades captain-lane staffing to "finish in-flight,
staff nothing new" until refreshed.

## Goals

- Dashboard: priorities (current/future), crew↔lane↔epic↔queue map with health, expected-vs-actual
  throughput, bottleneck rows, mailbox unread count. CLI-first (`harmonik dashboard [--json]`) plus a
  rendered `.harmonik/dashboard/latest.md` written by the existing ops-monitor loop.
- Mailbox: one durable inbox (`hitl-decisions` `--topic operator-mailbox`) for ALL operator
  escalations — crew-raised, stall-sentinel Tier-3, reconciliation `operator_escalation_required`.
- Forcing mechanism: structural, not behavioral — stale-dashboard blocks NEW captain-lane dispatch
  (never blocks running work, mailbox, runs, or reconcile), with keeper pre-nag + operator override.

## Non-goals

- No TUI / served HTTP page in v1 (deferred; the `--json` op + rendered file are the stable contract).
- No second messaging bus (comms bus stays ephemeral/pane-oriented; mailbox reuses hitl-decisions).
- No new persisted store for the dashboard (read-time projection over existing files + `events.jsonl`).
- Do NOT extend `lanes.json` (ops-monitor gate-parse-sensitive); the new file is a sibling.

## Constraints

- `dashboard_max_staleness` is **config-driven, fail-loud if unset** (no-hardcoded-threshold mandate;
  resolve like keeper/sentinel config).
- Forcing gate MUST be scoped to captain-curated lanes, degrade staffing only, never abort a run,
  never block mailbox / daemon-core / reconcile; MUST have an operator override.
- Mailbox touches the **finalized** `hitl-decisions` spec → spec addition needs operator sign-off (D3).

## Cross-work soft-dependencies (express in prose; NOT br-deps onto other works' beads)

- **Throughput aggregation** (bead 8) soft-depends on **eval-program WS1** `session-data.jsonl`
  (`plans/2026-07-03-eval-program` beads #2/#3/#4). Dashboard renders the throughput axis as
  "unavailable" until WS1 lands — degraded, NOT hard-blocked.
- **Escalation routing** (bead 5) soft-depends on **stall-sentinel Tier-3**
  (`plans/2026-07-02-stall-sentinel/DESIGN.md` §3). Bottleneck axis degrades until it lands.

## Open decisions (for operator / consensus — see DESIGN §5)

- **D1 — surface ceiling for v1:** CLI + rendered `latest.md` file (recommended) vs a served
  `--serve` HTTP page / TUI. Rec: CLI+file first (stable JSON contract), page later.
- **D2 — forcing-gate blast radius:** gate blocks dispatch to *all* captain-owned queues (rec) vs
  only the specific stale lane's queue. Narrower is safer but weaker forcing.
- **D3 — mailbox = extend `hitl-decisions` with `urgency`/`topic` convention (rec) vs a net-new
  `operator-mailbox` subsystem. Rec strongly favors reuse — but it touches the FINALIZED
  hitl-decisions spec, so it **NEEDS OPERATOR SIGN-OFF** (spec-change rule).
- **D4 — expected-throughput authorship:** operator sets `throughput_expected` per lane (rec, keeps
  it honest) vs the captain estimates it (risks self-graded targets).

## Success criteria

- `harmonik dashboard [--json]` returns a joined snapshot (state + dashboard.json + lanes.json +
  windowed session-data + open decisions + stall_detected).
- `.harmonik/dashboard/latest.md` (+ `.json`) written by the ops-monitor loop, no new scheduler.
- `harmonik mailbox` lists open `operator-mailbox` decisions; sentinel-Tier-3 and reconciliation
  escalations land there too.
- A stale `dashboard.json` (older than `dashboard_max_staleness`) halts NEW captain-lane dispatch,
  emits `dashboard_stale`, and can be cleared by refreshing the file or via the operator override;
  in-flight runs, mailbox, and reconcile paths are never blocked.
