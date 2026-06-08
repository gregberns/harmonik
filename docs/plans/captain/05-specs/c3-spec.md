# C3 — Crew launch context + mission-handoff schema — Change Spec

> Component **C3** of the *Captain & Crew* kerf plan. This is an
> **INSTRUCTIONS / CONTRACT** artifact — a new launch-context skill + the shared
> mission-handoff schema. It is **not Go code**: the deliverables are
> markdown/skill files plus one schema doc. An implementing agent follows it
> verbatim.
>
> Scope: (1) the cross-component **mission-handoff schema** (C4/captain writes it,
> C2 reads its path to seed, C3/crew resumes into it), (2) the **crew launch
> context** the crew boots into, (3) the **progress feed** the crew is mandated to
> emit. **Out of scope** (do NOT spec): the captain's judgment (C4 owns the
> captain — ranking/failure handling out entirely), the keeper internals, the
> C1/C2 Go code. C3 is purely the crew's instructions + the shared schema.
>
> All cross-component refs verified against `c1-spec.md` / `c2-spec.md` on
> `main` @ `2272d9f1` (2026-06-08).

---

## 1. Requirements (carried forward from 03-components.md C3)

C3 is the crew's instructions plus the schema shared across C2/C3/C4.

- **R-C3.1 — Mission-handoff schema (THE cross-component contract).** A single
  durable shape that C4/captain **writes**, C2 **reads** (to seed the session
  pointing the crew at the file), and C3/crew **resumes into** (via
  `/session-resume`) and **re-hydrates from** on a keeper restart. Exact fields:
  `{schema_version, crew_name, queue, epic_id, goal, captain_name}`. The schema doc locks the
  on-disk format, the path convention, and the field names/types, since C2 and C4
  must agree byte-for-byte. The durable assignment is ALSO mirrored to beads
  (`br update --assignee` on the epic) so a restart can re-derive the assignment
  even if the handoff file is lost. **The crew's `session_id` is NOT in the
  handoff** — it is captured by C2 and lives in the crew registry
  (`.harmonik/crew/<name>.json`, C2 §3.3).

- **R-C3.2 — Standard launch context (a new skill).** What every crew member
  boots with, independent of which epic it owns:
  - **Identity** — its `crew_name`, used as `$HARMONIK_AGENT` for all comms/beads
    ops.
  - **Named queue** — its own `queue`; the crew dispatches ONLY to this queue,
    NEVER the shared `main` queue.
  - **Subscribe to its comms inbox** — `harmonik comms recv --follow --json` (the
    crew's own identity comes from `$HARMONIK_AGENT`), **deduping on `event_id`**
    (agent-comms N3, at-least-once).
  - **Operating loop** — work the assigned epic by submitting its ready beads to
    its OWN `--queue <name>` via the daemon (per harmonik-dispatch — NEVER
    Agent-tool sub-agents, NEVER the `main` queue).
  - **Self-restart via the keeper** — the crew does nothing special for
    context-full; the keeper handles wind-down + resume. The crew only needs to be
    *resumable*, which it is because its `session_id` is stable (C2 §3.2) and its
    assignment is durable (R-C3.1).

- **R-C3.3 — Progress feed (C3 OWNS success-criterion #3).** The launch context
  MANDATES the crew emit, as a first-class behavior (not a free-rider on the
  loop):
  - periodic `harmonik comms send --to <captain_name> --topic status -- <update>`
    (durable in `events.jsonl`), AND
  - `br comments add <epic_id> --body "<update>"` on the assigned epic (durable in
    SQLite + git-tracked JSONL).
  with a concrete **cadence trigger** (on each bead close observed + on a timer).

**Verifies success-criteria #2, #3, #5** (`01-problem-space.md`):
- **#2** — the captain assigns an epic via comms; the crew receives it (deduped on
  `event_id`) and begins dispatching that epic's beads to its own queue. *(C3
  defines the receive-and-dispatch behavior; C2 §AC-2 merely triggers it.)*
- **#3** — a crew member writes progress to a durable, captain-readable feed as it
  works (`comms --topic status` and/or `br comments`). *(C3 owns this outright via
  R-C3.3.)*
- **#5** — on keeper-driven restart, a crew member re-hydrates its assignment from
  durable state (its queue + assigned epic) and continues. *(C3 defines the
  re-hydration steps in the operating loop.)*

---

## 2. Research summary (the surfaces the context depends on)

From `02-analysis.md` (current-state map; every fact traceable) and the three
composed skills (`agent-comms`, `beads-cli`, `harmonik-dispatch`):

- **§A4 — comms bus (REUSE for tasking + status).** `harmonik comms send/recv`,
  `--topic` is first-class. Delivery is **at-least-once**; the recipient **MUST
  dedupe on `event_id`** (agent-comms N3, NORMATIVE). Identity resolves from
  `$HARMONIK_AGENT` (set in the launch env). `recv --follow` replays the backlog
  then tails live (no gap). `who` presence is the only ephemeral piece (~120s);
  `comms log` is a read-only operator view that does NOT advance the cursor — the
  crew uses `recv`, not `log`, for its inbox.
- **§A5 — named queues (REUSE for per-crew work).** A queue's `Name` is a stable
  durable routing key persisted to `.harmonik/queues/<name>.json`. The crew's
  queue is created/owned by C2's `crew start`; the crew binds to it by name. **No
  auto-resume of a queue across a *daemon* restart** (`specs/queue-model.md:283`)
  — but a *crew* (keeper) restart does not touch the daemon, so queued work keeps
  flowing.
- **§A6 — beads / `br` (REUSE for roster + journal).** Roster = `br update
  --assignee` on the epic (the durable assignment mirror). Journal = `br comments
  add/list` (append-only). **Write discipline (beads-cli, NORMATIVE):** agents
  MUST NOT issue terminal-transition writes (`--claim` / `close` / `reopen`) — the
  daemon owns those. The crew's permitted writes are `br comments add` and
  metadata-only `br update` (labels/notes/assignee). **The crew does NOT `br
  close` its beads** — the daemon closes them when their work merges; that close
  is what triggers C1's `epic_completed`.
- **§A3 — keeper (DEPENDENCY, not built here).** The keeper handles context-full
  wind-down (handoff → clear → resume-injection), keyed on `session_id`. The crew
  needs no keeper-aware code; it only needs to be resumable. C2 stands up the
  keeper-attach inputs at spawn (gauge, `.managed` marker, `--tmux` handle).
- **harmonik-dispatch (the daily loop).** Work is dispatched by **submitting beads
  to the daemon's queue** (`harmonik queue submit --beads ...` / `append` /
  `subscribe`), NOT by spawning Agent-tool sub-agents. The crew applies this loop
  **scoped to its OWN `--queue <name>`**, not the shared `main`.
- **Cross-component contract from C2/C1:**
  - C2 §3.2 seeds the live session by bracketed-pasting a single kick-off line:
    `"Please read <handoff-path> and run /session-resume on it, then begin your
    operating loop."` — so the **handoff MUST be a file `/session-resume` can
    consume**, and the crew's first turn is "resume the handoff, then start the
    loop." C2 passes only the `--mission <handoff-path>`; it does not author the
    handoff (C4/captain does).
  - C2 §3.3 captures the `session_id` into the registry, NOT the handoff — so the
    handoff schema deliberately omits it.
  - C1 emits `epic_completed{epic_id, last_child_bead_id, closed_at}` when an
    epic's last child closes. The crew dispatching its epic's beads to its own
    queue → daemon closes them on merge → C1 fires → the captain (C4) is notified.
    The crew does NOT self-report completion; it just works the beads.

---

## 3. Approach (locked decisions + rationale)

### 3.1 Mission-handoff schema — LOCKED

**Decision: the handoff is a Markdown file with a YAML frontmatter header carrying
the six contract fields, followed by a human-readable body. Path:
`.harmonik/crew/missions/<crew_name>.md`.**

```markdown
---
schema_version: 1
crew_name: alpha          # string, charset [a-z0-9-], 1–64; == comms identity == registry Name
queue: alpha-q            # string, charset [a-z0-9-], 1–64; the crew's named queue
epic_id: hk-tigaf         # string, opaque bead id of the assigned epic
goal: "Ship named-queues" # string, one-line plain-English mission statement
captain_name: captain     # string, charset [a-z0-9-], 1–64; comms name to report status to
---

# Mission: <goal>

You are crew member **<crew_name>**, owning epic **<epic_id>** on queue
**<queue>**. Report status to **<captain_name>**.

<optional free-text context the captain wants to convey: priorities, caveats,
links to design docs, etc. — NOT part of the machine contract.>
```

**Field contract (C2 and C4 MUST match this exactly):**

| Field | Type | Required | Meaning |
|---|---|---|---|
| `schema_version` | int (`== 1`) | yes | Schema version; bump on any breaking field change. |
| `crew_name` | string, `[a-z0-9-]`, 1–64 | yes | The crew's stable identity; equals its `$HARMONIK_AGENT`, its comms name, and the registry `Name` (C2 §3.3). |
| `queue` | string, `[a-z0-9-]`, 1–64 | yes | The crew's named queue; equals the registry `Queue` and the `.harmonik/queues/<queue>.json` name. |
| `epic_id` | string (opaque bead id) | yes | The assigned epic; the parent whose children the crew dispatches. |
| `goal` | string (one line) | yes | Plain-English mission statement (used in `/session-resume` framing + status updates). |
| `captain_name` | string, `[a-z0-9-]`, 1–64 | yes | The captain's comms name; the crew sends `--topic status` updates here. |

**Rationale for Markdown-with-YAML-frontmatter (vs pure JSON or pure prose):**
1. **`/session-resume` consumes Markdown.** C2's seed line literally runs
   `/session-resume` on this file (C2 §3.2). `/session-resume` reads a HANDOFF.md
   shaped document — a human-readable Markdown narrative — not a JSON blob. A pure
   JSON file would resume poorly (the agent reads it as data, not as
   "instructions to continue"). The body section gives the captain a place to
   convey priorities/caveats that a flat struct cannot.
2. **YAML frontmatter is a parseable, ordered header.** C4/captain writes the five
   fields deterministically; a future tool (or the crew on re-hydrate) can parse
   the frontmatter without NLP. This is the same `---`-delimited frontmatter
   convention every SKILL.md in this repo already uses (`.claude/skills/*/SKILL.md`),
   so it is idiomatic and the crew agent already recognizes it.
3. **Single shape for all three readers.** The captain authors one file; C2 pastes
   one path; the crew resumes one file and re-hydrates from the same frontmatter.
   No format translation across the contract boundary.

**Durable assignment mirror (REQUIRED, R-C3.1).** The handoff file is *convenient*
but not the source of truth for the assignment — it could be lost or stale across
a keeper restart. So the assignment is ALSO mirrored to beads: on first boot the
crew runs `br update <epic_id> --assignee <crew_name>` (a metadata-only write,
permitted by beads-cli — it is NOT a terminal-transition). **This `--assignee` mirror
is load-bearing for the Captain's `epic_completed` attribution (06-integration.md §4
Gap 1): the Captain attributes a completed epic to its owning crew by reading
`br show <epic_id> --assignee`, NOT a registry/`crew list` field. So the mirror MUST
run on EVERY epic the crew adopts — boot AND comms re-task (§3.2 below) — not only
for restart re-hydration.** On a keeper restart the
crew re-derives its assignment from the union of (a) the handoff frontmatter and
(b) `br show <epic_id> --format json` `assignee == crew_name`, preferring beads if
they disagree (beads is the durable roster per §A6). **`--assignee` is the chosen
mirror field** (not `--owner`): `02-analysis §A6` and C2 reference
`--assignee/--owner` interchangeably; C3 LOCKS `--assignee` so C4 and the crew
write/read the same field. (If `br` lacks `--assignee` at impl time, fall back to a
`crew:<crew_name>` label via `br update --add-label` — same metadata-write class.)

**Why `session_id` is NOT in the handoff.** C2 mints and owns the `session_id`
(registry `.harmonik/crew/<name>.json`). The handoff is captain-authored and
re-used verbatim across restarts; embedding a session id there would make the
captain responsible for a value C2 owns and that rotates on `--fork-session`.
Keeping it out of the handoff means C4 can write the handoff before C2 ever
launches the session.

### 3.2 The crew launch context — a new skill — LOCKED

**Decision: a new skill at `.claude/skills/crew-launch/SKILL.md`.** It composes on
(does not duplicate) `agent-comms`, `beads-cli`, and `harmonik-dispatch`, and
references the handoff-schema doc.

**Why a skill (not a one-off template):** skills are the repo's load-bearing,
non-rotting instruction surface (every other agent-facing contract — comms, beads,
dispatch — is a skill). A crew session is a long-lived orchestrator that must boot
the same way every restart; a skill is the durable, discoverable home for that
invariant boot procedure. The per-crew variable bits (which epic, which queue) come
from the handoff frontmatter, not the skill — so one skill serves all crew.

**Content outline of `crew-launch/SKILL.md`:**

1. **Frontmatter** (`name: crew-launch`, `description`, `sources:` →
   `.kerf/works/captain/05-specs/c3-spec.md`, the handoff-schema doc, and the three
   composed skills).
2. **§ Who you are** — "You are a long-lived *crew* orchestrator. You own ONE epic
   and ONE named queue; you persist across many epics. Your identity is
   `$HARMONIK_AGENT` (== `crew_name`)."
3. **§ Boot sequence (do this first, in order)**:
   1. **Read your mission.** You were seeded with a `/session-resume` on your
      handoff file (`.harmonik/crew/missions/<crew_name>.md`). Parse its YAML
      frontmatter into `{schema_version, crew_name, queue, epic_id, goal, captain_name}`.
   2. **Confirm identity.** Verify `$HARMONIK_AGENT == crew_name`; if unset, use
      `crew_name` from the handoff as your `--from/--agent` on every comms/beads op.
   3. **Announce presence.** `harmonik comms join` (emits `agent_presence online`)
      so `harmonik comms who` shows you online (success-criterion #1).
   4. **Mirror the assignment to beads.** `br update <epic_id> --assignee
      <crew_name>` (metadata-only write — permitted; NOT a terminal transition).
   5. **Subscribe to your inbox** (next section).
   6. **Post a boot status** (progress-feed section).
4. **§ Subscribe to your comms inbox (dedupe on `event_id`)** — run, and keep
   running for the life of the session:
   ```bash
   harmonik comms recv --follow --json
   ```
   - Identity is `$HARMONIK_AGENT`; you receive messages where `to == you` or
     `to == "*"`.
   - **NORMATIVE (agent-comms N3): dedupe on `event_id`.** Delivery is
     at-least-once — keep a `seen` set of processed `event_id`s and treat any
     re-delivery as a no-op. Do NOT rely on body content or timestamps.
   - **Message handling:**
     - `topic == assignment` (or a captain message naming a new `epic_id`) → adopt
       the new epic: update your working `epic_id`, mirror `br update <new_epic>
       --assignee <crew_name>`, and begin dispatching its ready beads to your
       queue. *(This is success-criterion #2.)* **The `--assignee` mirror here is
       NOT optional and NOT just for restart: it is load-bearing for the Captain's
       `epic_completed` attribution (06-integration.md §4 Gap 1) — it MUST run on
       every re-task so the Captain attributes the new epic to this crew. Without it
       the Captain reads a stale assignee and mis-attributes completion.**
     - `topic == reprioritize` / other directives → act per the body.
     - Do NOT parse the human-readable `recv` output — always use `--json` (the
       `event_id`/`from`/`to`/`topic`/`body` fields).
5. **§ Operating loop — work YOUR epic on YOUR queue** — composes on
   `harmonik-dispatch`, scoped to `--queue <queue>`:
   1. Find ready beads under the epic: `br ready --format json` filtered to the
      epic's children (`br list --format json --parent <epic_id>` ∩ ready), or the
      kerf feed if the work is kerf-attached.
   2. **Submit to YOUR named queue, NEVER `main`:**
      ```bash
      harmonik queue submit --queue <queue> --beads <id1>,<id2>,...
      ```
      (or a `QueueSubmitRequest` JSON with the group's `queue` set to `<queue>`).
      **HARD RULE: the crew MUST NOT submit to the `main` queue.** Routing to your
      own queue is what keeps crews isolated (success-criterion #2) and is the
      check C2's roster + the captain rely on.
   3. **Arm a monitor:** `harmonik subscribe --types
      run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` to see
      your queue's beads finish.
   4. **Do NOT close beads.** The daemon closes a bead when its work merges
      (beads-cli write discipline). That close is what fires C1's `epic_completed`
      to the captain — you must not pre-empt it with `br close`.
   5. On `run_completed`/`run_failed` for one of your beads → post a status update
      (next section), then submit/append the next batch.
   6. Loop until the epic's beads are exhausted; idle on the comms inbox waiting
      for the next assignment. (You do NOT need to detect epic-completion yourself —
      C1 does that structurally and notifies the captain.)
6. **§ Progress feed (MANDATORY — success-criterion #3)** — see §3.3 below; the
   skill states the cadence + both surfaces.
7. **§ Self-restart via the keeper (you do nothing special)** — "Context-full
   wind-down is the keeper's job, not yours. When the keeper cycles you, it writes
   a handoff, clears context, and resumes your **same** `session_id`. On resume you
   will re-run this boot sequence: re-read your handoff/`br assignee`, re-`join`,
   re-`recv --follow`. Because your `queue` keeps flowing on the daemon and your
   `epic_id` is durable in beads, **no in-flight work is lost** and the captain's
   coordination is unaffected (success-criterion #5). Re-dedupe: your `seen` set is
   fresh after a restart, so re-process the comms backlog idempotently — actions
   (dispatch a bead already in your queue, re-`--assignee` an already-assigned
   epic) are no-ops."
8. **§ Clean shutdown** — `harmonik comms leave` on `crew stop` (best-effort;
   presence ages out at ~120s if you crash).
9. **§ What you MUST / MUST NOT do** (the guardrails, restated):
   - MUST: dedupe on `event_id`; submit only to `--queue <your-queue>`; emit
     status to the captain + `br comments` on the epic; re-hydrate from durable
     state on restart.
   - MUST NOT: submit to `main`; `br close`/`claim`/`reopen` any bead (daemon-only);
     spawn Agent-tool sub-agents for the epic's work (use the daemon queue); parse
     non-JSON `comms`/`br` output.

### 3.3 Progress feed — LOCKED (success-criterion #3 owner)

**Decision: the crew emits BOTH surfaces, on BOTH triggers.**

**Two durable surfaces (both required):**
1. **Captain-directed status over comms** (the live feed the captain watches):
   ```bash
   harmonik comms send --to <captain_name> --topic status -- "<update>"
   ```
   Durable in `events.jsonl`; the captain reads it via `comms recv`/`comms log`.
2. **Epic journal in beads** (the durable, git-tracked record on the work itself):
   ```bash
   br comments add <epic_id> --body "<update>"
   ```
   Durable in SQLite + `.beads/issues.jsonl`; survives any session/daemon restart
   and is reviewable out-of-band.

**Cadence trigger (LOCKED, concrete):** emit a status update —
- **on every observed bead close** for one of the epic's beads (a `run_completed`
  for a bead the crew submitted), AND
- **on a timer: at least once every 10 minutes** while the loop is active (a
  heartbeat-style "still working epic X, N beads done, M remaining"), so the
  captain sees liveness even during a long-running bead.
- **plus** a one-line **boot status** on every (re)start ("crew <name> online,
  owning <epic_id> on <queue>") and a **drain status** when the epic's ready beads
  are exhausted ("<epic_id>: all dispatched beads complete, idling for next
  assignment").

**Rationale for both surfaces + this cadence:** comms `--topic status` is the
captain's *live* feed (low-latency, addressed); `br comments` is the *durable
journal* attached to the work (survives the captain's own restart, reviewable by a
human later). Success-criterion #3 says "and/or," but mandating both makes #3 a
concrete owned behavior rather than a free-rider that might be skipped — and the two
serve different readers (captain-now vs auditor-later). The 10-minute timer bounds
the captain's blind window during a slow bead; bead-close events give event-driven
granularity without polling. The boot/drain bookends let the captain reconstruct
the crew's lifecycle from the feed alone.

---

## 4. Files & changes

All deliverables are **instruction/markdown artifacts** (no Go). Paths are repo-root
relative.

| Path | Create/Modify | Contents |
|---|---|---|
| `.claude/skills/crew-launch/SKILL.md` | **Create** | The crew launch-context skill (full outline in §3.2): boot sequence, comms-inbox subscribe + dedupe, operating loop scoped to the crew's own queue, progress-feed mandate, keeper-restart re-hydration, MUST/MUST NOT guardrails. Frontmatter `name: crew-launch` + `sources:` to this spec, the handoff-schema doc, and the three composed skills. |
| `docs/components/internal/crew-handoff-schema.md` | **Create** | The normative mission-handoff schema doc (§3.1): the field-contract table, the Markdown+YAML-frontmatter format, the path convention `.harmonik/crew/missions/<crew_name>.md`, the `--assignee` durable-mirror rule, the `session_id`-not-in-handoff rationale, and a worked example. This is the byte-for-byte contract C2 reads and C4 writes. |
| `.harmonik/crew/missions/example-handoff.md` | **Create (example, gitignored)** | A concrete filled-in example handoff (the `alpha`/`alpha-q`/`hk-tigaf` case from §3.1) so C2's smoke and C4's author have a copy-paste template. Lives under the gitignored `.harmonik/` tree (no `git status` noise; matches C2's `.harmonik/crew/` convention). |

**Skill-registry note (additive, non-rotting):** `crew-launch` joins the existing
skill set under `.claude/skills/`. If the project maintains a skill manifest /
registry list (the `agent-config-reviewer` tracks skill-registry drift), add
`crew-launch` to it. No existing skill is modified — `crew-launch` *references*
`agent-comms`, `beads-cli`, and `harmonik-dispatch` rather than copying their
content (the cross-project rule: skills compose, they don't duplicate).

**No change to:** any tracked Go file, the C1/C2 deliverables, the keeper, the queue
model, or any `specs/` normative file. (If the operator decides the handoff schema
should live under `specs/` rather than `docs/components/`, that is a one-line
relocation decision, not a content change — flagged in CONTRACT NOTES.)

---

## 5. Acceptance criteria (concrete / observable)

A crew booted with the `crew-launch` context and a valid handoff:

- **AC-1 (→ #2, auto-subscribe + dedupe).** On boot the crew runs `harmonik comms
  join` (visible in `comms who`) and `harmonik comms recv --follow --json`, and
  maintains a `seen` set keyed on `event_id`. **Observable:** sending the same
  `agent_message` `event_id` twice (forced re-delivery) results in the crew acting
  **once** — e.g. a duplicate `assignment` message does not double-submit the
  epic's beads. Verified by inspecting `comms log` (two sends, one resulting queue
  submission).

- **AC-2 (→ #2, owns its OWN queue, not `main`).** When the captain sends `--topic
  assignment` naming `epic_id`, the crew dispatches that epic's ready beads via
  `harmonik queue submit --queue <queue> --beads ...`. **Observable:** the beads
  appear in `.harmonik/queues/<queue>.json`, and **nothing** the crew submitted
  appears in `.harmonik/queues/main.json`. A crew attempting `--queue main` is a
  spec violation (caught in review / smoke).

- **AC-3 (→ #3, progress feed on both surfaces).** As the crew works, it emits
  `harmonik comms send --to <captain_name> --topic status` updates AND `br comments
  add <epic_id>` entries — on each observed bead close, on a ≤10-minute timer, and
  as boot/drain bookends. **Observable:** `harmonik comms log --to <captain_name>
  --topic status` shows the updates; `br show <epic_id> --format json` (or `br
  comments list <epic_id>`) shows the journal entries with matching content.

- **AC-4 (→ #5, resumable after a keeper cycle).** After the keeper winds the crew
  down and resumes its **same** `session_id` (`--resume <uuid>`), the crew re-runs
  its boot sequence, re-derives `{queue, epic_id}` from the handoff frontmatter
  AND/OR `br show <epic_id> --assignee == crew_name`, re-`join`s, re-`recv
  --follow`s, and continues working — **without** the captain re-sending the
  assignment and **without** losing in-flight queued work (the daemon kept
  draining `<queue>`). **Observable:** `comms who` shows the crew back online on
  the same name; the status feed shows a fresh boot status then continued updates;
  the queue's in-flight beads completed across the restart.

| AC | Maps to | Surface to observe |
|---|---|---|
| AC-1 | #2 | `comms log` (dedupe), `comms who` (online) |
| AC-2 | #2 | `.harmonik/queues/<queue>.json` vs `main.json` |
| AC-3 | #3 | `comms log --topic status`, `br comments list <epic_id>` |
| AC-4 | #5 | `comms who`, status feed, queue drain across restart |

---

## 6. Verification

These are instruction artifacts, so verification is a **manual smoke driving a real
crew session** (there is no Go test for a skill). Per the daemon/session lesson
(`reference_harmonik_daemon_session_nesting`), do this live under the supervisor —
reviewer reasoning alone is not sufficient for session/comms behavior.

1. **Pre-req:** a running daemon, C1 (`epic_completed`) and C2 (`crew start`) landed
   (or stubbed), and a test epic `hk-XXXX` with ≥2 ready child beads.
2. **Author a handoff:** write `.harmonik/crew/missions/smoke.md` per §3.1
   frontmatter (`crew_name: smoke`, `queue: smoke-q`, `epic_id: hk-XXXX`, a `goal`,
   `captain_name: captain`).
3. **Launch the crew (C2):** `harmonik crew start smoke --queue smoke-q --mission
   .harmonik/crew/missions/smoke.md`. The seed pastes the `/session-resume` line;
   the crew boots into `crew-launch`.
4. **Observe (AC-1):** `harmonik comms who` shows `smoke` online; re-send a
   duplicate assignment `event_id` and confirm a single resulting queue submission
   in `comms log`.
5. **Observe (AC-2):** `harmonik comms send --to smoke --topic assignment -- "work
   epic hk-XXXX"`; confirm the epic's beads land in
   `.harmonik/queues/smoke-q.json` and NONE in `main`.
6. **Observe (AC-3):** tail `harmonik comms log --to captain --topic status
   --follow` and `br comments list hk-XXXX` while beads run; confirm both feeds get
   updates on bead-close + the 10-minute timer + boot/drain bookends.
7. **Observe (AC-4):** trigger a keeper cycle (or simulate by `--resume <uuid>` the
   session); confirm `comms who` shows `smoke` re-online and the status feed resumes
   without a re-sent assignment.
8. **End-to-end (#3+#4 interplay, informational):** when the epic's last child
   merges + closes (daemon-owned), confirm C1's `epic_completed` fires to the
   captain — confirming the crew correctly did NOT self-report completion and did
   NOT `br close` its beads.

---

## 7. Error handling & edge cases

- **Missing handoff file.** If the seeded `/session-resume` path does not exist,
  the crew cannot derive its identity. The launch context says: try to re-derive
  `{crew_name, queue, epic_id}` from `$HARMONIK_AGENT` + `br show` for any epic with
  `assignee == $HARMONIK_AGENT`; if still indeterminate, post `comms send --to
  <captain_name> --topic error -- "missing/unreadable handoff; awaiting re-seed"`
  (using `$HARMONIK_AGENT` if known, else broadcast) and idle on the comms inbox.
  Do NOT guess an epic or dispatch anything.
- **Garbled / partial frontmatter** (missing a required field, bad
  `schema_version`). Treat as missing-handoff: do not dispatch; report `--topic
  error` to the captain; idle. The six fields are all required — a handoff missing
  any is invalid.
- **Comms dupes.** Covered by the NORMATIVE `event_id` dedupe (agent-comms N3): a
  re-delivered assignment/directive is a no-op. The crew's own actions are
  additionally idempotent in effect (re-submitting a bead already in the queue,
  re-`--assignee`ing an already-assigned epic) so even a dedupe-set reset on
  restart is safe.
- **Crew tries to write to `main`.** A spec violation — the launch context forbids
  it (HARD RULE, §3.2). Caught at review/smoke. (The daemon does not enforce
  per-crew queue isolation; isolation is an instruction-level contract, so the
  guardrail must be explicit in the skill.)
- **Epic with no ready beads** (all blocked, draft, or already done). The crew
  posts a `--topic status` drain message ("`<epic_id>`: no ready beads, idling")
  and waits on the comms inbox; it does NOT spin-poll `br ready` tightly (respect a
  ≥10-minute re-check, or wake on a captain message). It does NOT try to unblock
  beads itself (that is captain/dependency judgment, out of scope).
- **Keeper restart mid-loop.** In-flight beads are on the daemon's queue, which
  keeps draining independent of the crew session — so a restart loses no work
  (success-criterion #5). On resume the crew re-hydrates and continues; the comms
  backlog is re-read idempotently (dedupe set is fresh, actions are no-ops). The
  one thing the crew must NOT do on resume is re-`br close` or re-`claim` anything
  (it never does — the daemon owns those).
- **Captain offline when the crew posts status.** `comms send` is durable
  (events.jsonl); the captain reads it from its cursor whenever it comes back. No
  special handling — at-least-once delivery covers it. (`comms who` showing the
  captain offline is informational, not a blocker.)
- **`br update --assignee` unsupported by the installed `br`.** Fall back to a
  `crew:<crew_name>` label via `br update <epic_id> --add-label crew:<crew_name>`
  (same metadata-write class, permitted). The re-hydrate read then checks the label
  instead of the assignee field. Flagged in CONTRACT NOTES as the one impl-time
  branch.

---

## 8. Migration / back-compat

Purely **additive**:
- A new skill `.claude/skills/crew-launch/SKILL.md` — no existing skill changes;
  `crew-launch` *references* the three composed skills rather than editing them.
- A new schema doc `docs/components/internal/crew-handoff-schema.md` — new doc, no
  existing doc rewritten.
- A new gitignored example under `.harmonik/crew/missions/` — covered by the
  `/.harmonik/` gitignore rule (same as C2's `.harmonik/crew/`), so no `git status`
  noise.
- No Go, no spec-text-under-`specs/` change, no behavioral change to any existing
  command. A crew launched without this skill simply lacks the boot procedure —
  i.e. the skill must be present in the crew session's launch context (C2 sets
  `$HARMONIK_AGENT`; the skill is provisioned to the session the same way other
  agent skills are). If the operator later moves the schema doc under `specs/`, that
  is an additive relocation, not a contract change.

---

## OPEN DECISION FOR OPERATOR

1. **Schema-doc home: `docs/components/` vs `specs/`.** I placed the
   mission-handoff schema doc at `docs/components/internal/crew-handoff-schema.md`
   (a knowledge-base component doc) rather than `specs/` (normative). Rationale:
   the schema is a cross-component *contract* but C3 is an instructions artifact and
   the project's normative `specs/` are reserved for the spec-first code contract.
   If you want the handoff schema to be normative-spec-grade (so C2/C4 are bound by
   `specs/`-level review), relocate it to `specs/crew-handoff-schema.md` — additive,
   content unchanged. **This is the only genuine open decision; everything else is
   locked.**
