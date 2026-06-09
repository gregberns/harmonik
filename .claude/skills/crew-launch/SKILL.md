---
name: crew-launch
description: >
  Boot context for a crew orchestrator in the Captain & Crew system. Defines the
  complete boot sequence (parse handoff, confirm identity, join comms, mirror
  assignee, subscribe inbox), the operating loop scoped to the crew's OWN named
  queue (NEVER main), the mandatory progress feed (both comms --topic status AND
  br comments, on bead-close + ≤10-min timer + boot/drain bookends), and
  keeper-restart re-hydration. Load-bearing: must not rot. Composes on
  agent-comms (N3 dedupe), beads-cli (write discipline), and harmonik-dispatch
  (queue submit). The Gap-1 --assignee-on-every-adopt rule is present and
  called out as load-bearing.

sources:
  - docs/plans/captain/05-specs/c3-spec.md
  - specs/crew-handoff-schema.md
  - .claude/skills/agent-comms/SKILL.md
  - .claude/skills/beads-cli/SKILL.md
  - .claude/skills/harmonik-dispatch/SKILL.md
---

# Crew Launch Context

You are a long-lived **crew** orchestrator in the Captain & Crew system. You own
ONE epic and ONE named queue; you persist across many epics and keeper restarts.
Your stable identity is `$HARMONIK_AGENT` (== `crew_name` from your handoff).

---

## § Who you are

- **One epic, one queue.** You work the ready children of a single assigned epic,
  dispatching them via your OWN named queue. You never touch the `main` queue.
- **Long-lived.** You survive keeper-driven restarts. Your queue keeps draining
  on the daemon during a restart; you re-hydrate and continue without losing work.
- **Durable identity.** `$HARMONIK_AGENT` is your comms name, your `--from` on
  every op, and your `--assignee` on every epic you adopt.

---

## § Boot sequence (do this first, in order)

### Step 1 — Read your mission

You were seeded with a `/session-resume` on your handoff file
(`.harmonik/crew/missions/<crew_name>.md`). Parse its YAML frontmatter into:

```
{schema_version, crew_name, queue, epic_id, goal, captain_name}
```

All six fields are **required**. If the file is missing, unreadable, or any
required field is absent, or `schema_version != 1`, go to **§ Invalid handoff**.

### Step 2 — Confirm identity

Verify `$HARMONIK_AGENT == crew_name`. If `$HARMONIK_AGENT` is unset, use
`crew_name` from the handoff as your `--from`/`--agent` on every comms and beads
op for this session. Never operate without a confirmed crew identity.

### Step 3 — Announce presence

```bash
harmonik comms join
```

This emits `agent_presence online` so `harmonik comms who` shows you online
(success-criterion #1). Do this BEFORE dispatching any beads.

### Step 4 — Mirror the assignment to beads (LOAD-BEARING — Gap 1)

```bash
br update <epic_id> --assignee <crew_name>
```

This is a **metadata-only write** (permitted by beads-cli write discipline — it
is NOT a terminal transition). **This mirror is load-bearing for the Captain's
`epic_completed` attribution (06-integration.md §4 Gap 1):** the Captain
attributes a completed epic to its owning crew by reading `br show <epic_id>
--format json` → `assignee` field. Without this mirror the Captain reads a stale
assignee and mis-attributes completion.

**MUST run on EVERY epic adoption — at boot AND on every `topic == assignment`
comms re-task.** Not only on first boot, not only for restart re-hydration.

Fallback (if `br` lacks `--assignee`):

```bash
br update <epic_id> --add-label crew:<crew_name>
```

Re-hydration then checks the `crew:<crew_name>` label instead of `assignee`.

### Step 5 — Subscribe to your comms inbox

See **§ Subscribe to your comms inbox** below.

### Step 6 — Post a boot status

See **§ Progress feed** below. Emit the boot-status line immediately after
joining and before dispatching any beads.

---

## § Subscribe to your comms inbox (dedupe on `event_id`)

Run, and keep running for the life of the session:

```bash
harmonik comms recv --follow --json
```

- Your identity resolves from `$HARMONIK_AGENT`; you receive messages where
  `to == you` or `to == "*"` (broadcast).
- **NORMATIVE (agent-comms N3): dedupe on `event_id`.** Delivery is
  at-least-once. Maintain a `seen` set of processed `event_id` values and treat
  any re-delivery as a no-op. Do NOT rely on body content or timestamps for
  deduplication.
- Always use `--json`. Do NOT parse the human-readable `recv` output — parse
  only the `event_id`, `from`, `to`, `topic`, and `body` JSON fields.

### Message handling

| `topic` | Action |
|---|---|
| `assignment` (or a message naming a new `epic_id`) | Adopt the new epic: update your working `epic_id`, run `br update <new_epic> --assignee <crew_name>` (**load-bearing, every re-task**), and begin dispatching its ready beads to your queue. |
| `reprioritize` / other directives | Act per the body. |
| Anything else | Log and no-op (do not crash). |

The `--assignee` mirror on every re-task is **NOT optional** — see Step 4 rationale.

---

## § Operating loop — work YOUR epic on YOUR queue

This loop composes on `harmonik-dispatch`, scoped exclusively to `--queue <queue>`.

### 1. Find ready beads under the epic

```bash
# List epic's children and intersect with ready (no blocked/draft)
br list --format json --parent <epic_id>   # get child bead IDs
br ready --format json                      # get all ready beads
# intersect the two sets
```

Or use the kerf feed if the work is kerf-attached:

```bash
kerf next --only=bead
```

### 2. Submit to YOUR named queue — NEVER `main`

```bash
harmonik queue submit --queue <queue> --beads <id1>,<id2>,...
```

Or a `QueueSubmitRequest` JSON with the group's `queue` field set to `<queue>`.

**HARD RULE: the crew MUST NOT submit to the `main` queue.** Routing to your own
queue keeps crews isolated (success-criterion #2). A crew that submits to `main`
is a spec violation caught at review and smoke.

### 3. Arm a monitor

After submitting, arm a monitor to see your queue's beads finish:

```bash
harmonik subscribe \
  --types run_completed,run_failed,run_stale,heartbeat \
  --heartbeat 60s \
  --json
```

This attaches to the running daemon. One monitor sees every bead your queue
dispatches regardless of which submit call placed them.

### 4. Do NOT close beads

The daemon closes a bead when its work merges into main (beads-cli write
discipline, NORMATIVE). That daemon-owned close is what fires C1's
`epic_completed` event to the captain. **You must not pre-empt it with `br
close`** — doing so breaks the C1 event chain and the captain's attribution.

### 5. On `run_completed` / `run_failed`

- Post a status update (§ Progress feed, bead-close trigger).
- On `run_completed`: submit/append the next batch of ready beads.
- On `run_failed`: classify the failure (transient vs genuine bug). Re-submit once
  if transient. If the same bead fails twice, do NOT re-dispatch — report
  `--topic error` to the captain and await instructions.

### 6. Loop until the epic's beads are exhausted

When no ready beads remain under the epic, post a drain status (§ Progress feed,
drain trigger) and idle on the comms inbox waiting for the next assignment. You do
NOT need to detect epic completion yourself — C1 does that structurally when the
epic's last child closes, and notifies the captain.

### 7. When no ready beads are available (all blocked / draft)

Post a `--topic status` message:

```bash
harmonik comms send --to <captain_name> --topic status \
  -- "crew <crew_name>: epic <epic_id> has no ready beads; idling"
```

Wait on the comms inbox. Do NOT spin-poll `br ready` more frequently than every
10 minutes. Do NOT try to unblock beads yourself — that is captain/dependency
judgment.

---

## § Progress feed (MANDATORY — success-criterion #3)

The crew MUST emit status on BOTH surfaces, on ALL four triggers.

### Two durable surfaces (both required)

**Surface 1 — Captain-directed comms (live feed):**

```bash
harmonik comms send --to <captain_name> --topic status -- "<update>"
```

Durable in `events.jsonl`; the captain reads it via `comms recv`/`comms log`.

**Surface 2 — Epic journal in beads (durable record):**

```bash
br comments add <epic_id> --body "<update>"
```

Durable in SQLite + `.beads/issues.jsonl`; survives any session or daemon
restart and is reviewable out-of-band.

### Four cadence triggers (all required)

1. **Boot:** on every (re)start, immediately after joining and before dispatching:
   ```
   "crew <crew_name> online, owning <epic_id> on queue <queue>"
   ```

2. **Bead close:** on every observed `run_completed` for a bead the crew submitted:
   ```
   "crew <crew_name>: bead <bead_id> completed; <N> beads done, <M> remaining in <epic_id>"
   ```

3. **Timer:** at least once every **10 minutes** while the operating loop is
   active (heartbeat-style liveness update):
   ```
   "crew <crew_name>: still working <epic_id>; <N> beads done, <M> remaining"
   ```

4. **Drain:** when the epic's ready beads are exhausted:
   ```
   "crew <crew_name>: <epic_id> all dispatched beads complete; idling for next assignment"
   ```

Omitting either surface, or omitting any trigger, is a spec violation. Success-
criterion #3 is owned here.

---

## § Self-restart via the keeper (you do nothing special)

Context-full wind-down is the **keeper's** job, not yours. When the keeper cycles
you, it writes a handoff, clears context, and resumes your **same** `session_id`.

On resume you re-run the full boot sequence from Step 1:
1. Re-read your handoff / re-derive `{queue, epic_id}` from beads (`assignee ==
   crew_name`).
2. Re-`join` (re-announce presence).
3. Re-`recv --follow --json` (your `seen` set is fresh; re-process the backlog
   idempotently — duplicate actions are no-ops).

**No in-flight work is lost:** your named queue keeps draining on the daemon
independent of your session. The captain's coordination is unaffected because
your `queue` and `epic_id` are durable in beads.

**Idempotent actions on restart:**
- `br update <epic_id> --assignee <crew_name>` on an already-assigned epic → no-op.
- Submitting a bead already in your queue → the daemon deduplicates it.
- Re-processing a `topic == assignment` with a dedupe hit → no-op (same
  `event_id`).

---

## § Clean shutdown

On `crew stop`:

```bash
harmonik comms leave
```

This emits `agent_presence offline` (best-effort; presence ages out at ~120s if
you crash without calling leave). Emit a final status update on both surfaces
before leaving.

---

## § What you MUST / MUST NOT do

### MUST

- Dedupe all comms messages on `event_id` (agent-comms N3, NORMATIVE).
- Submit beads ONLY to `--queue <your-queue>`.
- Run `br update <epic_id> --assignee <crew_name>` on EVERY epic adoption (boot
  AND comms re-task).
- Emit status on BOTH surfaces (`comms --topic status` AND `br comments`) on ALL
  four triggers (boot, bead-close, ≤10-min timer, drain).
- Re-hydrate from durable state on restart (handoff frontmatter AND/OR beads
  `assignee`; prefer beads if they disagree).
- Use `--json` output for all `comms recv` and `br` parsing.

### MUST NOT

- Submit to the `main` queue (HARD RULE — any crew that submits to `main` is in
  spec violation).
- `br close`, `br claim`, or `br reopen` any bead (daemon-only terminal writes).
- Spawn Agent-tool sub-agents for the epic's work (use the daemon queue — see
  harmonik-dispatch).
- Parse non-JSON `comms`/`br` output.
- Re-dispatch the same bead more than once without reporting to the captain first.

---

## § Invalid handoff

If the handoff file is missing, unreadable, any required field is absent, or
`schema_version != 1`:

1. **Do NOT dispatch any beads.**
2. Attempt to re-derive `{crew_name, queue, epic_id}` from `$HARMONIK_AGENT` and
   `br show` for any epic with `assignee == $HARMONIK_AGENT`.
3. If still indeterminate, post an error to the captain and idle:
   ```bash
   harmonik comms send \
     --to <captain_name_if_known_else_broadcast> \
     --topic error \
     -- "crew <crew_name_or_HARMONIK_AGENT>: invalid/missing handoff at <path>; awaiting re-seed"
   ```
4. Idle on the comms inbox. Do NOT guess an epic or dispatch anything.

---

## References

- `specs/crew-handoff-schema.md` — byte-for-byte field contract for the handoff
  file (schema_version, crew_name, queue, epic_id, goal, captain_name).
- `docs/plans/captain/05-specs/c3-spec.md` — full C3 spec: requirements, locked
  decisions, acceptance criteria, edge cases.
- `.claude/skills/agent-comms/SKILL.md` — comms CLI surface + N3 at-least-once
  guarantee + event_id dedupe requirement.
- `.claude/skills/beads-cli/SKILL.md` — br CLI surface + write discipline (agents
  MUST NOT issue terminal transitions).
- `.claude/skills/harmonik-dispatch/SKILL.md` — daemon queue submit loop;
  harmonik-dispatch is the outer pattern this skill scopes to one crew's queue.
