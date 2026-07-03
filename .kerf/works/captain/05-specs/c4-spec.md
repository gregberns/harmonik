# C4 — Captain operating context — Change Spec

> Component **C4** of the *Captain & Crew* kerf plan. This is an **INSTRUCTIONS
> artifact**, not Go code: it specifies a new launch/operating-context document
> (a skill) that a Captain LLM session boots with. The Captain is itself an LLM
> session running this context — there is **no Go captain-supervisor** in this
> slice.
>
> **Scope line (HARD):** this context covers **HOW to do the orchestrating role
> MECHANICALLY** — spawn crew, write handoffs, mail epics, subscribe to
> `epic_completed`, read progress. It MUST NOT encode **JUDGMENT**: ranking which
> initiative/epic to assign, deciding what to do when a crew is stuck/failed, or
> rebalancing load. On any completion or stuck signal the Captain's MECHANICAL
> behavior is to **surface it to the operator and await assignment** — the
> human/operator makes the ranking/failure call in this slice. Where the future
> judgment layer plugs in is noted, not built.
>
> All cross-component contracts (C1 event, C2 verbs, C3 handoff schema) are
> consumed by their exact CLI/contract form; C4 invents none of them.

---

## 1. Requirements

Carried forward from `03-components.md` C4 for traceability. The Captain operating
context (a new skill) MUST define the MECHANICAL actions below. C4 **cross-cuts
success-criteria #1–#6** (`01-problem-space.md`) as the orchestrating role — it is
the surface a human drives to exercise the whole Captain & Crew slice end-to-end.

- **R-C4.1 — Spawn crew (mechanism #1).** The context tells the Captain to bring up
  each crew member by calling C2's verb
  `harmonik crew start <name> --queue <q> --mission <handoff-path>`, and to read the
  live roster via `harmonik crew list` (read-only, works even when the daemon is
  down). One `crew start` call per crew member.
- **R-C4.2 — Write the mission handoff (consumes C3's schema #2).** Before each
  `crew start`, the Captain writes a mission-handoff file in the **C3 locked schema**
  `{schema_version, crew_name, queue, epic_id, goal, captain_name}` and passes its path as
  `--mission`. The Captain MUST use this exact schema — it does NOT invent a
  different shape (C2 delivers the path; C3/crew resumes into it).
- **R-C4.3 — Mail epics / tasks (mechanism #3).** The Captain delivers ongoing work
  and re-tasks over the comms bus: `harmonik comms send --to <crew-name> -- <body>`
  (directed) and `--broadcast` where apt (e.g. a fleet-wide pause/announce). The
  Captain dedupes inbound on `event_id` (N3).
- **R-C4.4 — Subscribe to `epic_completed` (consumes C1 #4).** The Captain runs
  `harmonik subscribe --types epic_completed --json` as its **structural trigger**
  that a crew finished its epic — independent of any crew self-report. Payload is
  C1's `{epic_id, last_child_bead_id, closed_at}`.
- **R-C4.5 — Read progress (mechanism #5).** The Captain reads each crew's
  `--topic status` comms feed via `harmonik comms log --from <crew> --since <dur>`
  (operator view, no cursor advance) and the epic's `br comments` to follow work in
  flight.
- **R-C4.6 — Judgment-out boundary (restated, NORMATIVE for this context).** On an
  `epic_completed` event, a stuck/offline signal, or any contended ranking decision,
  the Captain's MECHANICAL behavior is to **surface** the situation (a status line +
  a `comms send --to operator --topic status` message) and **await** the operator's
  assignment. The Captain MUST NOT autonomously pick the next epic, declare a crew
  failed, reassign work, or rebalance. Those are the future judgment layer.

**Maps to success criteria** (each verified by the Captain *operating* this context;
the underlying mechanisms are owned by C1/C2/C3):

| Criterion (`01-problem-space.md`) | C4 role |
|---|---|
| #1 ≥2 named crew up, each on its own queue | C4 drives ≥2 `crew start` calls; confirms via `comms who` / `crew list` |
| #2 assign an epic via comms; crew receives it | C4 writes the handoff + mails the assignment |
| #3 crew writes a captain-readable progress feed | C4 *reads* it (`comms log --from <crew>` + `br comments`); the *emit* is C3 |
| #4 `epic_completed` fires; a subscribed captain receives it | C4 runs `subscribe --types epic_completed`, receives, **surfaces** |
| #5 crew re-hydrates after a keeper restart; coordination unaffected | C4 keeps coordinating off durable state (registry/queue/comms) across the crew's restart |
| #6 captain spawns a NEW crew non-interactively | C4's spawn step (handoff + `crew start`) is the trigger |

---

## 2. Research summary (the surfaces C4 composes — all owned elsewhere)

C4 builds **nothing new**; it is an instructions document that wires together
already-specified surfaces. The composition map:

- **C1 — `epic_completed` event** (`c1-spec.md`). Registered event type
  `epic_completed` with payload `{epic_id, last_child_bead_id, closed_at}` (all
  fields non-empty when valid). Fires when an epic's **direct** last child closes;
  C1 is **single-level** (a grandparent epic gets its own event only when its own
  last direct child closes — see C1 Open Question 1). Flows through
  `harmonik subscribe --types epic_completed --json` with no client change. **C4
  consumes it read-only as the completion trigger.**
- **C2 — persistent crew-start path** (`c2-spec.md`). CLI surface the Captain calls:
  - `harmonik crew start <name> --queue <q> --mission <handoff-path>` — daemon RPC;
    exits 0 and prints the minted `session_id`; ensures the named queue exists;
    launches the long-lived crew session; seeds the mission via bracketed-paste.
    **Exit 17 = daemon not running.** Name collision with a *live* crew → non-zero
    (not 17). Queue already bound to a different live crew → non-zero with
    "queue `<q>` already bound to crew `<other>`".
  - `harmonik crew stop <name> [--pause-queue]` — daemon RPC; deregisters + stops.
    (C4 uses this only on explicit operator instruction — retiring a crew is an
    operator decision, not Captain judgment.)
  - `harmonik crew list [--json]` — **read-only, local**; works with the daemon
    down. Returns the registry records `{name, session_id, queue, epic, handle,
    started_at}`.
- **C3 — mission-handoff schema + crew launch context** (`03-components.md` C3 /
  `c2-spec.md` §3.2). The cross-component contract C4 **writes**:
  `{schema_version, crew_name, queue, epic_id, goal, captain_name}`. C3 also defines the crew's own
  boot loop (subscribe to its inbox, claim its queue, dispatch its epic, emit
  `--topic status`) — C4 only *triggers* that loop (via the mission seed) and
  *reads* its output.
- **comms bus** (`agent-comms` skill). `comms send --to <name> | --broadcast`,
  `comms recv --follow --json` (cursor-advancing, dedupe on `event_id`, N3),
  `comms log --since <event_id|dur> [--from/--to/--topic]` (operator view, no
  cursor advance), `comms who [--json]` (presence within ~120s), `comms join` /
  `comms leave` (presence beats). **Exit 17 = daemon not running** for
  send/recv/join/leave; `log`/`who` work without the daemon.
- **beads / `br`** (`beads-cli` skill). C4's permitted writes are **comments only**:
  `br comments add <epic> --body "..."`. **The Captain MUST NOT issue
  terminal-transition writes** (`br claim/close/reopen`) — daemon-owned (BI-010).
  Read surface: `br show <epic> --format json`, `br comments list <epic>`,
  `br list --parent <epic> --format json`.
- **`subscribe`** (`harmonik-dispatch` skill / `cmd/harmonik/subscribe.go`).
  `harmonik subscribe --types <list> --json [--heartbeat <dur>]`. The Captain uses
  `--types epic_completed` (and may add `run_failed,run_stale` for surface-only
  awareness — see §7). It attaches to the running daemon and sees all events
  regardless of which agent submitted the underlying work.

**Concurrency note carried from `harmonik-dispatch`:** do NOT run the daemon
dispatching beads AND ≥10 parallel Agent-tool sub-agents on the same account. The
Captain is an orchestrator, not a heavy sub-agent dispatcher — it spawns a small,
coarse fleet of crew (rare, coarse lifecycle) and otherwise watches/mails. This
keeps it well inside the rate-limit rule.

---

## 3. Approach — the Captain operating context (mechanics only)

The deliverable is a **skill** the Captain LLM session loads at boot:
`.claude/skills/captain/SKILL.md`. It encodes the Captain's **mechanical operating
loop** and the judgment-out boundary. It does NOT contain a ranking algorithm, a
failure-recovery policy, or a rebalancer.

### 3.1 The mechanical loop

The context instructs the Captain to run this loop. Every step is a concrete CLI
call against an existing surface; the **only** decision the Captain makes
autonomously is *mechanical* (is the daemon up? did `crew start` exit 0? did an
`epic_completed` arrive?). Every *judgment* decision is surfaced-and-awaited.

```
BOOT
  0. Identity. Captain runs under $HARMONIK_AGENT (e.g. "captain"). `comms join`.
  1. Daemon check. `harmonik crew list` (local) + `harmonik queue status`
     (exit 17 ⇒ daemon down ⇒ surface to operator, do not proceed to spawn).

SPAWN (per crew assignment the OPERATOR has handed the Captain)
  2. Write the C3 mission handoff to a stable path, e.g.
     .harmonik/crew/missions/<crew_name>.md, in the LOCKED schema
     {schema_version, crew_name, queue, epic_id, goal, captain_name}.   ← C3 contract, verbatim
  3. harmonik crew start <crew_name> --queue <q> --mission <handoff-path>
       - exit 0  ⇒ record the printed session_id (informational); crew is up.
       - exit 17 ⇒ daemon down ⇒ surface, stop.
       - non-0   ⇒ collision / queue-bound / launch-fail ⇒ SURFACE to operator,
                    do NOT retry-with-different-name autonomously (that's a choice).
  4. Confirm liveness: poll `harmonik comms who` until <crew_name> appears (it
     comes online once its C3 boot loop runs). Bounded wait; if it never appears,
     SURFACE (see §7).

ASSIGN / MAIL  (mechanism #3)
  5. Mail the assignment / any ongoing epic or re-task:
       harmonik comms send --to <crew_name> --topic assign -- <epic_id + 1-line goal>
     Broadcast only for fleet-wide announcements:
       harmonik comms send --broadcast --topic announce -- <message>

WATCH  (the steady state — runs concurrently)
  6. Subscribe to completion (the STRUCTURAL trigger, mechanism #4, C1):
       harmonik subscribe --types epic_completed --json
     Dedupe inbound on event_id (N3). For each epic_completed:
       a. Attribute epic_id to the owning crew via `br show <epic_id> --format json`
          → `assignee` (the durable beads mirror the crew sets on every adopt;
          06 §4 Gap 1). Do NOT use `crew list`/`Record.Epic` — it goes stale on a
          comms re-task.
       b. SURFACE: post a status line + a comms message to the operator:
            harmonik comms send --to operator --topic status \
              -- "epic <epic_id> completed (crew <name>, last child <id>); awaiting next assignment"
       c. AWAIT. Do NOT pick the crew's next epic. (Judgment-out, R-C4.6.)
  7. Read progress on demand (mechanism #5):
       harmonik comms log --from <crew_name> --topic status --since 30m
       br comments list <epic_id> --format json
     Use these to answer operator questions and to populate the surfaced status —
     never to autonomously decide a crew is behind.

RECEIVE OPERATOR DIRECTION  (the assignment channel)
  8. harmonik comms recv --follow --json   (dedupe on event_id)
     When the operator assigns a new epic to a crew, loop back to step 2 (write a
     fresh/updated handoff) and step 5 (mail it). Re-tasking a LIVE crew is a comms
     send (step 5) + optionally a `crew start` re-launch only if the operator says
     to restart it.
```

### 3.2 How re-tasking a live crew works (no judgment)

The Captain re-tasks an already-running crew **over comms** — it does not
re-`crew start` for a new epic (that path is for bringing a *new* crew up or
relaunching a dead one; see C2 §7 collision handling). Mechanical rule the context
states: "to give a live crew a new epic, send it a `--topic assign` comms message
referencing the epic id; the crew picks it up via its C3 boot-loop `comms recv`."
The *which* epic is always the operator's call.

### 3.3 The surface-and-await contract (judgment-out, restated)

The context devotes an explicit section to the boundary. The Captain's three
"surface, do not decide" triggers:

1. **`epic_completed`** → surface "crew X finished epic E; awaiting next
   assignment." Do NOT assign the next epic.
2. **Stuck / offline** (crew drops from `comms who`, or a `run_failed`/`run_stale`
   the Captain happens to see) → surface "crew X appears stuck/offline (signal:
   ...); awaiting operator direction." Do NOT declare it failed, kill it, or
   re-home its work.
3. **Contention** (two ready epics, one free crew; or two crew idle, one epic) →
   surface the contention with the facts (`crew list`, ready epics from
   `br list`); do NOT rank.

> **Where the future judgment layer plugs in (NOTE, not built):** a later slice can
> replace each "surface-and-await" with a ranking/decision policy that consumes the
> same inputs the Captain already gathers here (`crew list`, `comms log --topic
> status`, `br` epic state, `epic_completed`) and *emits* the assignment the
> operator currently makes. The mechanical loop above is unchanged by that layer —
> only the "AWAIT operator" step becomes "consult the policy." This is explicitly
> out of scope for C4.

### 3.4 Rationale for "a skill, not Go"

`03-components.md` C4 classifies this component as **instructions** ("In this slice
the captain is an LLM session running this context — no Go supervisor"). A skill
under `.claude/skills/captain/` is the idiomatic launch-context artifact (mirrors
the existing `agent-comms`, `beads-cli`, `harmonik-dispatch` skills the Captain also
loads). It is consumed by the Captain session at boot exactly as those are; it
references C1/C2/C3 by their CLI/contract so it cannot drift into reimplementing
them.

---

## 4. Files & changes

| Path | Create/Modify | Why |
|---|---|---|
| `.claude/skills/captain/SKILL.md` | **Create** | The Captain operating context (the C4 deliverable). Markdown skill, not Go. Content outline below. |

> **Bench-only artifact today.** This change-spec lives on the gitignored bench
> (`.kerf/works/captain/05-specs/`). The skill file itself is a **tracked**
> repo artifact when implemented; an implementing agent creates it under
> `.claude/skills/captain/SKILL.md`. C4 touches no Go, no `specs/`, no build.

### `.claude/skills/captain/SKILL.md` — content outline

```
---
name: captain
description: >
  Operating context for a Captain LLM session in the Captain & Crew model.
  MECHANICS ONLY: spawn crew (harmonik crew start), write C3 mission handoffs,
  mail epics over comms, subscribe to epic_completed, read crew progress.
  JUDGMENT IS OUT OF SCOPE — on completion/stuck/contention the Captain SURFACES
  to the operator and AWAITS assignment; it does not rank, fail, or rebalance.
  Load alongside agent-comms, beads-cli, and harmonik-dispatch.
sources:
  - .kerf/works/captain/05-specs/c4-spec.md (this spec)
  - .kerf/works/captain/05-specs/c2-spec.md (crew start/stop/list)
  - .kerf/works/captain/05-specs/c1-spec.md (epic_completed event)
  - .kerf/works/captain/03-components.md C3 (mission-handoff schema)
---

# Captain operating context

## 0. What you are (and are NOT)
- You ARE the orchestrating role: you bring crew up, hand them missions, mail them
  work, and watch for epic completion.
- You are NOT a judgment engine. You do not rank initiatives, decide a crew has
  failed, or rebalance work. On any such moment you SURFACE to the operator and
  AWAIT (see §5). [restate R-C4.6 verbatim]

## 1. Identity & boot
- Run under $HARMONIK_AGENT (e.g. "captain"). `harmonik comms join` at start;
  `harmonik comms leave` at clean shutdown.
- Daemon check: `harmonik crew list` + `harmonik queue status`. Exit 17 anywhere ⇒
  daemon down ⇒ surface to operator; do not spawn.

## 2. Spawn a crew member  (mechanism #1; success-criterion #1, #6)
- Write the mission handoff (§3) FIRST.
- `harmonik crew start <name> --queue <q> --mission <handoff-path>`.
- Interpret exit codes: 0 ⇒ up (note the printed session_id); 17 ⇒ daemon down;
  non-0 ⇒ collision/queue-bound/launch-fail ⇒ SURFACE, do not auto-retry.
- Confirm with `harmonik comms who` (crew comes online via its C3 boot loop).
- Read the roster any time with `harmonik crew list [--json]`.

## 3. Write the mission handoff  (C3 contract — mechanism #2; success-criterion #2)
- LOCKED schema (do NOT deviate): {schema_version, crew_name, queue, epic_id, goal, captain_name}.
- Write to a stable per-crew path, e.g. .harmonik/crew/missions/<crew_name>.md.
- Pass that path to `crew start --mission`. C2 delivers it; C3/crew resumes into it.
- [show a concrete example handoff file]

## 4. Mail epics & re-task  (mechanism #3)
- Assign / re-task a LIVE crew over comms:
    harmonik comms send --to <crew_name> --topic assign -- <epic_id + 1-line goal>
- Fleet announcements only: harmonik comms send --broadcast --topic announce -- ...
- Re-tasking a live crew is a comms send, NOT a new `crew start`.
- Dedupe anything you RECEIVE on event_id (N3).

## 5. Watch for completion & surface  (mechanism #4 = C1; the judgment-out gate)
- Run `harmonik subscribe --types epic_completed --json` as your structural trigger.
- On each epic_completed {epic_id, last_child_bead_id, closed_at}:
    1. Attribute the owning crew: `br show <epic_id> --format json` → `assignee`
       (durable mirror, 06 §4 Gap 1).
    2. SURFACE: status line + `harmonik comms send --to operator --topic status --
       "epic <id> done (crew <name>); awaiting next assignment".
    3. AWAIT. Do NOT assign the next epic. (Judgment-out.)
- C1 is single-level: a parent epic completes only when ITS own last direct child
  closes; you may receive a sub-epic completion before the top-level one — surface
  each as it arrives, do not roll up.

## 6. Read progress  (mechanism #5; success-criterion #3 is read here, emitted by C3)
- harmonik comms log --from <crew_name> --topic status --since 30m   (no cursor move)
- br comments list <epic_id> --format json
- Use ONLY to answer operator questions / populate surfaced status. Never to decide
  a crew is behind.

## 7. Receive operator direction
- harmonik comms recv --follow --json  (dedupe on event_id).
- On a new operator assignment: write/refresh the handoff (§3) and mail it (§4).

## 8. What you MUST NOT do (judgment-out, restated)
- Do NOT rank/choose which epic a crew works next.
- Do NOT declare a crew failed, kill it, or re-home its work.
- Do NOT rebalance load across crew.
- Do NOT issue br terminal-transition writes (claim/close/reopen) — daemon-owned.
- Do NOT auto-retry a failed `crew start` under a different name/queue.
- In every one of these moments: SURFACE + AWAIT.

## 9. Error & edge handling  (see §7 of this change-spec)
- Daemon down (exit 17) · `crew start` non-0 · crew offline · epic_completed for an
  unknown/unassigned epic. Each ⇒ surface; the table in §9 says exactly what.

## 10. Where the future judgment layer plugs in (NOTE — not your job today)
- A later slice replaces each "AWAIT operator" with a ranking/decision policy fed by
  the same inputs you already gather (crew list, status comms, br epic state,
  epic_completed). You do not build it.
```

---

## 5. Acceptance criteria (concrete / testable)

A Captain session given this context can:

- **AC-1 (#1) — bring up ≥2 crew on distinct named queues.** Driving the context,
  the Captain issues ≥2 `harmonik crew start <name> --queue <q> --mission <path>`
  calls with **distinct** `name` and `queue`. Verifiable: `harmonik crew list`
  shows ≥2 records with distinct `name`+`queue`; `harmonik comms who` shows both
  online once their boot loops run.
- **AC-2 (#2) — assign each an epic via a C3 handoff.** For each crew, the Captain
  wrote a mission file whose JSON/fields are exactly
  `{schema_version, crew_name, queue, epic_id, goal, captain_name}` (no extra/renamed fields), and
  passed it as `--mission`; then mailed the assignment via
  `harmonik comms send --to <crew>`. Verifiable: the handoff file matches the
  schema; a `comms log --to <crew> --topic assign` entry exists.
- **AC-3 (#4) — receive `epic_completed` and surface it.** With
  `harmonik subscribe --types epic_completed --json` running, on a real
  `epic_completed{epic_id,...}` the Captain emits a `comms send --to operator
  --topic status` (or status-line) message naming the completed `epic_id` and the
  owning crew, and takes NO assignment action. Verifiable: a `comms log --from
  <captain> --topic status` entry referencing the `epic_id`, and the absence of any
  follow-on `--topic assign` to that crew until the operator responds.
- **AC-4 (judgment-out, NORMATIVE) — no autonomous ranking/failure decision.**
  Across AC-1..AC-3 the Captain makes **zero** of: choosing which epic to assign
  next, declaring a crew failed, killing/re-homing a crew's work, rebalancing. Every
  completion/stuck/contention moment results in a surface-and-await, not a decision.
  Verifiable by transcript inspection: no `crew start`/`comms send --topic assign`
  is issued in response to an `epic_completed` or stuck signal *before* an operator
  message arrives.
- **AC-5 (#3, read-side) — reads the progress feed without acting on it.** The
  Captain can answer "how is crew X doing?" using `comms log --from X --topic
  status` + `br comments list <epic>`, without those reads triggering any
  assignment/failure action.
- **AC-6 (#5, coordination-continuity) — survives a crew keeper restart.** When a
  crew's `session_id` rotates / it cycles via the keeper, the Captain keeps
  coordinating off durable state (`crew list`, the named queue, comms) — it does not
  treat the restart as a failure and does not need to re-`crew start` (C2 §7: a
  keeper relaunch `--resume`s the same id). Verifiable: a restart event produces no
  Captain failure-surface and no spurious re-spawn.

> AC-1..AC-3 + AC-5 map directly to success-criteria #1, #2, #4, #3 (read), #5.
> AC-4 is the judgment-out guarantee. (#6 is exercised by the AC-1 spawn step,
> whose mechanism is owned by C2.)

---

## 6. Verification

C4 is an instructions artifact, so verification is a **manual smoke** of a Captain
session driving the context against a live daemon (there is no Go to unit-test).

1. **Boot a daemon** under the supervisor (per `harmonik-dispatch`); confirm
   `harmonik crew list` and `harmonik queue status` respond.
2. **Drive a Captain session** loaded with `.claude/skills/captain/SKILL.md` (plus
   `agent-comms`, `beads-cli`). Hand it (as operator) two crew assignments
   referencing two distinct epics with children.
3. **Observe ≥2 crew come up:** the Captain writes two C3 handoffs and issues two
   `crew start` calls; `harmonik comms who` shows both crew online on distinct
   queues (AC-1, #1). `harmonik crew list` shows two distinct records.
4. **Observe an assignment land:** the Captain mails each crew its epic; confirm via
   `harmonik comms log --to <crew> --topic assign` (AC-2, #2).
5. **Observe an `epic_completed` surfaced:** close the last child of one assigned
   epic (operator action or via the daemon); confirm the Captain's
   `subscribe --types epic_completed` fires and the Captain emits a
   surface-and-await status message **without** assigning the next epic (AC-3 + AC-4,
   #4 + judgment-out).
6. **Negative check (AC-4):** confirm the transcript contains NO Captain-initiated
   ranking/failure/rebalance action across the run.

Per `reference_harmonik_daemon_session_nesting`: any code path the Captain *exercises*
(C1/C2) is live-smoked under the supervisor as part of those components' own
verification — C4's smoke is the *operator-experience* layer on top.

---

## 7. Error handling & edge cases

The context MUST tell the Captain exactly what to do in each. The rule is uniform:
**detect mechanically, surface to the operator, await** — never decide.

| Situation | Mechanical detection | Captain action (mechanics only) |
|---|---|---|
| **`crew start` fails (non-17)** — name collision with a live crew, queue already bound to another live crew, or launch failure (C2 §7) | `crew start` exits non-zero with C2's message | SURFACE the exact C2 error to the operator; do NOT auto-retry under a different name/queue (that is a choice). Await operator direction. |
| **Daemon down** | any daemon RPC (`crew start/stop`, `comms send/recv/join/leave`, `subscribe`) exits **17** | SURFACE "daemon not running"; do not proceed to spawn/mail. `crew list`, `comms log`, `comms who` still work (local) — use them to report state. |
| **Crew goes offline** | crew drops from `harmonik comms who` (past the ~120s TTL) and/or stops emitting `--topic status` | SURFACE "crew X appears offline (last seen ...); awaiting direction." Do NOT declare it failed, kill it, or re-home its epic. (A keeper restart can transiently drop presence — distinguish via AC-6: a returning crew that re-appears in `comms who` needs no action.) |
| **`epic_completed` for an unknown/unassigned epic** | `br show <epic_id> --assignee` is empty / matches no live crew in `crew list` | SURFACE it as an informational completion ("epic <id> completed; not tracked to any current crew"); do NOT spawn/assign in response. (Can happen for an epic closed out-of-band or one whose crew was already stopped.) |
| **Stuck dispatch / run failure the Captain happens to see** | a `run_failed`/`run_stale` on a subscription the Captain *also* watches (optional) | SURFACE as a stuck signal; do NOT classify or recover (failure handling is judgment-out + lives in `harmonik-dispatch` for the *crew's* loop, not the Captain's). |
| **Duplicate `epic_completed`** (at-least-once on the bus, or a C1 crash-window retry — C1 Open Question 2) | same `event_id` re-delivered, or a second `epic_completed` for an already-surfaced epic | Dedupe on `event_id` (N3); if a logically-duplicate completion for an already-surfaced epic arrives with a new `event_id`, surface at most one "epic <id> completed" to the operator (idempotent surfacing). |
| **Sub-epic completion before top-level** (C1 is single-level) | `epic_completed` for a child epic whose parent epic is still open | Surface each as it arrives; do NOT roll up to the parent (no judgment, no walking the tree — C1 Open Question 1). |

**Concurrency guard (carried from `harmonik-dispatch`):** the Captain is a light
orchestrator — it must not spin up ≥10 parallel Agent-tool sub-agents while the
daemon dispatches crew beads (rate-limit rule). Crew spawning is rare/coarse; the
Captain otherwise watches and mails.

---

## 8. Migration / back-compat

Purely **additive** and self-contained:

- New skill file `.claude/skills/captain/SKILL.md` — a new launch-context artifact;
  it adds a role, changes no existing skill, command, or spec.
- No Go, no `specs/` edit, no build, no schema change. Nothing depends on C4; C4
  depends (read-only, by CLI/contract) on C1/C2/C3 and the comms/beads/subscribe
  surfaces.
- A Captain session simply loads one more skill at boot. Sessions that do not load
  it are unaffected (no Captain role).
- Forward path: when the judgment layer lands, it slots into the "AWAIT operator"
  steps (§3.3 NOTE) without changing the mechanical loop — so C4's context is
  forward-compatible with that addition.

---

## CONTRACT NOTES

**C3 mission-handoff schema consumed (verbatim):** `{schema_version, crew_name,
queue, epic_id, goal, captain_name}` (schema_version: 1). C4 (the Captain) **writes**
this file; C2's `crew start --mission <path>` delivers the path and seeds the crew
with it; C3/crew resumes into it. C4 uses these exact six fields and invents no
others. Source of the lock:
`03-components.md` C3 + `c2-spec.md` §3.2 (both state the identical shape). The
crew's `session_id` is captured by C2 into the crew registry (`Record.SessionID`),
**not** carried in the handoff — C4 reads it via `crew list` if needed, but does not
write it.

**C1 event payload consumed:** `epic_completed{epic_id, last_child_bead_id,
closed_at}`, single-level (direct parent only). C4 subscribes
`--types epic_completed --json` and treats it as the structural completion trigger.

**C2 verbs consumed:** `crew start <name> --queue <q> --mission <path>` (daemon RPC,
prints `session_id`, exit 17 if daemon down, non-0 on collision/launch-fail),
`crew stop <name> [--pause-queue]`, `crew list [--json]` (local read).

**Places C4 needs something C1/C2/C3 don't yet provide — FLAGGED:**

1. **`epic_completed` attribution source — RESOLVED (06 §4 Gap 1).** Attribute the
   owning crew via the durable `br show <epic_id> --assignee` mirror the crew sets on
   every adopt (boot AND comms re-task — C3 §3.1). This is the attribution source of
   truth; it does NOT go stale on a comms re-task the way `crew list` / `Record.Epic`
   would. There is **no `crew set-epic` verb and no C4 in-memory handoff-derived map**
   used for attribution. (`crew set-epic` / `UpdateEpic` remains an optional future
   convenience for `crew list` display only — it is NOT the attribution source.) An
   epic whose `assignee` is empty or matches no live crew falls into the
   "unknown/unassigned epic" edge (§7).

2. **No structural "crew offline / stuck" event.** C4 detects offline only by
   `comms who` TTL drop and absence of `--topic status` — there is no
   `crew_offline`/`crew_stuck` event. That is fine for a *surface-only* slice (the
   Captain just reports it), but a future judgment layer will likely want a
   structural signal rather than a presence-poll heuristic. Out of scope for C4;
   noted for the judgment slice.

3. **No "operator" agent identity is contractually defined.** C4 surfaces via
   `comms send --to operator`. This assumes an agent named `operator` is on the bus
   (or that the operator reads `comms log`/a status line). No component defines
   that identity. **Recommend:** the operator either runs `comms join --name
   operator` or reads `comms log --from <captain> --topic status` directly. The
   context states both fallbacks (status line + comms) so the surface lands even if
   no `operator` agent is online.
