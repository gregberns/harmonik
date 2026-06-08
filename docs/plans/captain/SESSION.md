# Captain & Crew — Kerf Session Log

**Codename:** captain
**Project:** gregberns-harmonik
**Jig:** plan (v1)
**Driven through:** problem-space → analyze → decompose → research → change-spec → integration → tasks → ready
**Status at end of session:** ready

## What this kerf proposes

A long-lived **captain** orchestrator that spawns and coordinates a pool of long-lived
**crew** agents — each owning one epic and its own named work queue — wired together by
the `harmonik comms` bus plus a new structural `epic_completed` event. **In scope = the
mechanical wiring** (start crew, deliver work + status between captain and crew, notify
the captain when an epic finishes). **Out of scope = the captain's judgment** (ranking
which initiative to assign, handling stuck/failed crew, rebalancing) — those are a
future "judgment layer"; in this slice the captain **surfaces-and-awaits** the operator.

Four components — **C1/C2 are Go code; C3/C4 are instruction/skill artifacts. No Go
captain-supervisor** (the captain is an LLM session running a context).

- **C1 — `epic_completed` event** (daemon + core): emit `{epic_id, last_child_bead_id,
  closed_at}` when an epic's last child bead closes.
- **C2 — `harmonik crew start/stop/list`** + a new `internal/crew` registry.
- **C3 — crew launch context** (`crew-launch` skill) + the shared mission-handoff schema.
- **C4 — captain operating context** (`captain` skill), mechanics-only.

## Decisions made in this kerf

- **Crew launch = INTERACTIVE `claude --remote-control "<name>" --session-id <uuid>`**
  (a local, cloud-watchable, pasteable pane) — NOT server-mode `claude remote-control`
  (no local pane) and NOT the Agent-SDK / Managed-Agents sessions API (bills the API
  credit pool, not the Max subscription). `session_id` is **caller-minted** (harmonik
  already passes `--session-id <uuid>`), written to the registry before launch — not
  captured from output. Mission seeded via bracketed-paste; re-task via `claude -p
  --resume <uuid>` (same id; `--fork-session` would fork). Flags confirmed in claude
  v2.1.168.
- **C1 at-most-once** via an `emittedEpics` guard on `workLoopDeps`, boot-seeded from the
  event log (survives daemon restart); **single-level** (direct parent only). Adds
  `DependencyEdge.EndpointStatus` / `brShowEdge.Status` to surface child status (one `br
  show <parent>` call). At-least-once only on a crash in the emit window (accepted).
- **C2 is a daemon RPC** (`crew start/stop`; `crew list` local-read). New `internal/crew`
  package (+ depguard entry). "Ensure queue exists" = persist `Queue{Name, Workers:1}`
  if absent; the **crew→queue binding lives only in the registry** (the queue model gets
  no owner field). `--pause-queue` = `queue-set-concurrency <q> 0` (no `queue-pause` op).
- **Mission-handoff schema** `{schema_version, crew_name, queue, epic_id, goal,
  captain_name}` — Markdown + YAML frontmatter at `.harmonik/crew/missions/<name>.md`.
  Assignment is ALSO mirrored to beads via `br update <epic> --assignee <crew>` — this
  mirror is **load-bearing for the captain's `epic_completed` attribution** (the captain
  reads `br show <epic> --assignee`, NOT the spawn-time `Record.Epic`), so the crew
  re-asserts it on every adopt (boot AND comms re-task).
- **C4 captain = mechanics-only**: spawn crew → write handoffs → mail epics (`comms send
  --topic assign`) → `subscribe --types epic_completed` → on completion/stuck/contention,
  **surface-and-await** (status line + `comms send --to operator --topic status`), never
  decide. `operator` identity = dual-surface convention (`comms join --name operator`, or
  `comms log` no-join fallback).
- **Crew-offline structural event** = out of scope (judgment-layer follow-up); the `comms
  who` TTL heuristic stands.

## Specs created / amended

- **NEW (operator chose `specs/`, spec-first):** `specs/crew-handoff-schema.md` — the
  normative mission-handoff contract (authored by task T11).
- **Amended (additive, EV-029 N-1-safe):** `specs/event-model.md §8` — one taxonomy row
  for `epic_completed` (durability class O). This is the slice's **only** `specs/` edit;
  everything else is additive code + two new skills (`crew-launch`, `captain`).

## What ships next

`07-tasks.md` enumerates **15 implementation tasks (T1–T15)**. Build order:
**C1 (T1→T2→T3/T4) ∥ C2 (T5/T6→T7→T8/T9/T10)** run concurrently → **C3 (T11→T12)** →
**C4 (T13)** → the two gating test beads **T14** (`scenario: captain — end-to-end
captain+crew`) and **T15** (`explore: captain — operator CLI surface`). Caveat: T3 and T7
both touch `internal/daemon/daemon.go` (merge-serialized, not a literal parallel pair).

Beads are filed with the `codename:captain` label (kerf-work attachment — never an
epic-dep). **Neither the plan nor any impl bead closes until both T14 and T15 close.**
Beads are created `open` and are NOT auto-dispatched — they wait until the operator
chooses to feed them to the daemon's queue.
