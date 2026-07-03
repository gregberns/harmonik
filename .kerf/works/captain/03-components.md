# Captain & Crew — Decomposition

Four components. **C1–C2 are code (Go); C3–C4 are launch-context / instruction artifacts.**
Reuse surfaces (comms, named queues, beads, keeper) are dependencies, not build-components.

## C1 — `epic_completed` event   [code: daemon + core]
Emit a structural event when an epic's last child bead closes.
- New `epic_completed` event type (`internal/core/eventtype.go`) + payload
  `{epic_id, last_child_bead_id, closed_at}` (struct in `internal/core`, registered in
  `eventreg_hqwn59.go`).
- At the bead-close site (`internal/daemon/workloop.go`, right after `emitBeadClosed`): if the
  closed bead has a parent (parent-child dep) and that parent now has zero remaining open
  children, emit it.
- Flows through `subscribe --types epic_completed` unchanged.
- **Completion query (resolved):** `br show <parent> --format json` returns every child in
  `dependents[]` WITH `status` inline — one call, not N+1. BUT the current adapter
  (`internal/brcli/show.go:20-23`, `brShowEdge`) DROPS the child `status` field, so C1 includes
  a small adapter change (extend `core.DependencyEdge` / add a method) to surface child status.
- **Idempotent emit (required):** crew/humans can `br close` out-of-band (own queues), i.e.
  outside the daemon's one-at-a-time merge lock — two siblings can race the "last child?"
  check. Emit must be read-after-write / idempotent: emit `epic_completed` **at most once per
  epic** (guard on the parent's first observed all-children-closed transition).
- Verifies success-criterion **#4**.

## C2 — Persistent crew-start path   [code: new command]
A captain-invokable way to start a long-lived crew session.
- New verb (e.g. `harmonik crew start <name> --queue <q> --mission <handoff-path>`): launch a
  **long-lived `claude remote-control --name <name>`** session seeded with the mission handoff,
  **capture its `session_id`** (from remote-control / `--output-format json` stdout), and ensure
  the named queue exists + owned. REPLACES the earlier tmux-paste-injection approach.
- Crew are addressed by **comms name** (tasking) and **`session_id`** (re-task / restart via
  `--resume`) — NOT tmux pane-id. Resolves the reviewer's keeper↔pane-identity gap and aligns
  with the keeper (which already keys identity on `session_id`).
- **Crew registry (small, new durable state):** one record per crew member —
  `{name, session_id, queue, epic, handle, started_at}` at `.harmonik/crew/<name>.json` — so
  captain + keeper can find and re-address it. `name` + `queue` are stable keys; `session_id`
  is updated on each (re)launch (it may rotate across a keeper restart).
- Stand up the per-crew keeper gauge + `.managed` marker at spawn so the keeper can attach
  (see R3). Retire/stop path minimal for this slice (`crew stop <name>`).
- Verifies success-criteria **#1, #6**.

## C3 — Crew launch context + mission-handoff format   [instructions + shared contract]
What a crew member boots with, plus the handoff schema shared across C2/C3/C4.
- **Mission-handoff schema (the cross-component contract — C4/captain writes it, C2 reads it
  to seed the session, C3/crew resumes into it):** `{crew_name, queue, epic_id, goal,
  captain_name}`. The durable assignment record (mirrored to beads `--assignee/--owner`) a
  restart re-hydrates from. The crew's `session_id` is captured by C2 and lives in the crew
  registry, NOT the handoff.
- Standard launch context (skill/template): identity (name), named queue, subscribe to
  messaging (`comms recv --follow --to <name>`, dedupe on `event_id`), operating loop (work
  the assigned epic via the daily-dispatch loop; self-restart via keeper).
- **Progress feed (success-criterion #3 owner):** the launch context mandates the crew emit
  periodic `comms send --to <captain> --topic status` updates AND append `br comments` on the
  epic as it goes — making #3 a concrete owned behavior, not a free-rider on the loop.
- Verifies success-criteria **#2, #3, #5**.

## C4 — Captain operating context   [instructions]
The captain's own launch context — **mechanics only; judgment is OUT of scope.**
- Spawn crew (C2), write their mission handoff (C3), mail epics (`comms send --to`), subscribe
  to `epic_completed` (C1), read roster/progress from beads + `comms --topic status`.
- In this slice the captain is an LLM session running this context — no Go supervisor.
- Cross-cuts **#1–#6** as the orchestrating role.

## Explicitly out (restated)
Ranking initiatives · stuck/failed-initiative handling · the keeper restart itself · remote-
control programmatic drive · a Go captain-supervisor · a joined `harmonik roster` view.
