---
name: crew-launch
description: >
  Boot context for a crew orchestrator in the Captain & Crew system. Defines the
  complete boot sequence (parse handoff, confirm identity, join comms, mirror
  assignee, subscribe inbox), the operating loop scoped to the crew's OWN named
  queue (NEVER main), the mandatory progress feed (both comms --topic status AND
  br comments, on bead-close + ≤10-min timer while dispatching / ≤15-min when
  idle/draining + boot/drain bookends), and
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

## § How you were launched (operator reference)

The captain (or operator) starts you with the native umbrella verb:

```bash
harmonik start crew <name>          # simple form: one bare positional = your name
harmonik start crew --name paul --queue paul-q --mission /tmp/paul.md   # advanced
```

- **Positional-XOR-flags (D2):** the simple form is a bare name and NOTHING else;
  the moment any `--flag` appears, the name MUST move to `--name` (no bare
  positional alongside flags). Mixing them is a hard error
  (`positional name not allowed alongside flags — use --name paul`).
- **`--queue` defaults to `<name>-q`** (one named queue per crew). `harmonik start
  crew paul` binds you to `paul-q`.
- **`--mission` is operator-supplied and OPTIONAL — never auto-stubbed (D3).**
  Decisive rule: a **fresh** `harmonik start crew` reads ONLY the `--mission` flag
  and IGNORES any on-disk `.harmonik/crew/missions/<name>.md` — so a stale mission
  left by a prior same-named crew can NEVER be silently reused. A **keeper-restart**
  re-hydration is the exception (Step 1 / § Self-restart): there you DO re-read the
  on-disk mission, because it is your OWN just-written mission, not a stale one.
- `harmonik crew start <name> …` remains as the back-compat entry point;
  `start crew` is the easy alias. The keeper rides along automatically.

---

## § Boot sequence (do this first, in order)

> **MINIMAL LOAD (load-bearing).** Do NOT load fleet-level state (`ROADMAP.md`,
> `.harmonik/context/captain-lanes.md`, `project.yaml`, the orchestrator-rules
> standing-rules skill) — you are scoped to ONE epic + ONE queue; your mission
> file is your tier-1 state. Fleet-level state is the captain's concern.

> **One-call discovery shortcut — run the crew boot digest first:**
> ```bash
> scripts/crew-boot-digest.sh        # in-repo, portable (hk-039z); --crew / --project optional
> ```
> This reads your mission file, checks daemon status, agents online, your
> queue and epic state, ready beads, and recent comms — in one shell call —
> and emits a single Markdown STATE DIGEST. Read the digest, then continue
> with Steps 3–6 below (join, mirror, recv, boot-status — these are **actions**
> that cannot be scripted away; only discovery is collapsed).

### Step 1 — Read your mission

You were seeded with a `/session-resume` on your handoff file
(`.harmonik/crew/missions/<crew_name>.md`). The file has two sections:

**Stable front-matter (tier-2 — written once by the captain, survives restarts):**

```
{schema_version, crew_name, queue, epic_id, goal, captain_name[, model]}
```

All six base fields are **required**. `model` is optional (see §3 of
`specs/crew-handoff-schema.md`). If the file is missing, unreadable, or any
required field is absent, or `schema_version != 1`, go to **§ Invalid handoff**.

**`## Current State` block (tier-1 — updated by the captain on every /clear):**

The body may contain a `## Current State` section after the frontmatter. Parse
it for: `queue_id`, `in_flight` beads, armed monitor state, `next_action`,
open blockers, and any translations glossary. This section overrides any stale
claim in tier-2 about what is actively dispatching. If the section is absent,
treat all tier-1 fields as unknown and re-derive them via Steps 2–4 below.

```markdown
## Current State

queue_id: <uuid or "(none)">
in_flight: [hk-aaa, hk-bbb]          # beads submitted but not yet terminal
monitor: armed | not-armed
next_action: <1-sentence description>
blockers: <none | description>
translations: hk-abc = "short plain-English title"
```

### Step 2 — Confirm identity

Verify `$HARMONIK_AGENT == crew_name`. If `$HARMONIK_AGENT` is unset, use
`crew_name` from the handoff as your `--from`/`--agent` on every comms and beads
op for this session. Never operate without a confirmed crew identity.

### Step 3 — Announce presence

```bash
harmonik comms join
```

This emits `agent_presence{online, reason:"join"}` so `harmonik comms who` shows you
online (success-criterion #1). Do this BEFORE dispatching any beads.

**Heartbeat discipline (hk-ru45u):** once `comms recv --follow` is armed (Step 5 or
wherever you open it), it auto-refreshes presence every ~60s and emits a `leave` beat
on clean exit — no separate `comms join` timer is needed. If you need a manual
heartbeat when `--follow` is NOT armed, use `harmonik comms join --reason=refresh`
(not bare `comms join`) so the beat is not persisted to events.jsonl.

### Step 4 — Mirror the assignment to beads (LOAD-BEARING — Gap 1)

```bash
br update <epic_id> --assignee <crew_name>
```

This is a **metadata-only write** (permitted by beads-cli write discipline — it
is NOT a terminal transition). **This mirror is load-bearing for the Captain's
attribution on ALL run events (06-integration.md §4 Gap 1):** the Captain
attributes epics (on `epic_completed`) AND individual task beads (on
`run_failed`/`run_stale`) to their owning crew by reading `br show <epic_id>
--format json` → `assignee` field. Without this mirror the Captain cannot identify
which crew owns a failing or wedged bead, causing "whose bead is this?"
round-trips (logmine F13: ≥4 such exchanges observed over hk-w6y70/xdxws/kbqto/3kyh3).

**MUST run on EVERY epic adoption — at boot AND on every `topic == assign`
comms re-task.** Not only on first boot, not only for restart re-hydration.

**`--assignee` goes on the EPIC ONLY — NEVER on a child / dispatchable bead.** The
daemon claims dispatchable beads via `br claim`, which REFUSES an already-assigned
bead (→ `max_attempts_exceeded`, `run_id=null`, never dispatches). Mirror the
assignee on the parent epic for attribution; leave every child you submit
UNASSIGNED. (Refs hk-kr791, hk-amed0.)

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

### Idle-crew-wake protocol (keep `--follow` armed)

**A crew does NOT reliably wake the instant a `comms send` arrives.** A
one-shot / idle Claude session only processes a delivered message when one of
two things is true:

1. it has an **armed `harmonik comms recv --follow --json` stream still
   running** (the boot sequence starts this — Step 5 — and you MUST keep it
   running for the whole life of the session), OR
2. its tmux pane gets a **nudge** — e.g. the sender used
   `comms send --to <crew> --wake` (see the agent-comms skill, § Waking an idle
   peer), or the captain pokes the pane manually.

**Rule (load-bearing):** keep `comms recv --follow --json` running continuously —
**this obligation does NOT relax when you drain or go idle.** A drained / idle crew
is the EXACT case the captain re-tasks with an `assign` `comms send`, so the
`--follow` stream is most load-bearing precisely when you have no work. Concretely:

1. **Never stop `--follow` on drain.** When you exhaust your epic's ready beads
   (§ Operating loop step 6/7) you post the drain status and idle — but you KEEP
   `comms recv --follow --json` armed the whole time. Stopping it on drain is the
   bug that strands a re-task: the captain's `assign` arrives, nothing is listening,
   and the crew sleeps through its own re-staffing. (The ONLY case where you may
   stop `--follow` is a genuine `park` message from the daemon — § Park/wake — and
   even then you do NOT re-arm until the pane nudge.)
2. **Re-arm on every keeper-restart / resume.** The `/clear` tears down your
   `--follow` stream. Re-arming it is part of the boot sequence (Step 5) and you
   MUST re-run it on EVERY restart/resume, not just first boot — see § Self-restart
   via the keeper. A resumed crew that forgets to re-arm `--follow` is deaf to the
   captain until something nudges its pane.
3. **Re-arm on stream death mid-session.** If you ever find your `--follow` stream
   has died (disconnect exit with NO `park` line), re-arm it immediately as part of
   your loop — do not wait for the next bead event to notice.

If a crew has gone fully idle WITHOUT an armed `--follow`, it may miss a wake until
something nudges it — a bare `send` alone is **not** guaranteed to rouse an idle
Claude pane. (This reflects observed behavior: idle crews do not reliably wake on a
bare send.)

**Captain side (the complement — documented here so both ends are explicit):** when
the captain re-tasks an IDLE crew, a bare `comms send --to <crew> --topic assign`
does NOT wake an idle Claude pane on its own. The captain MUST **pane-nudge** the
crew — use `comms send --to <crew> --wake` (which capture-panes + injects the
nudge; see the agent-comms skill § Waking an idle peer) or poke the pane manually —
otherwise the re-task lands in the inbox unread. The crew's armed `--follow` plus
the captain's `--wake` are the two halves of the same guarantee; both are required.

### Message handling

| `topic` | Action |
|---|---|
| `assign` (or a message naming a new `epic_id`) | Adopt the new epic: update your working `epic_id`, run `br update <new_epic> --assignee <crew_name>` (**load-bearing, every re-task**), and begin dispatching its ready beads to your queue. |
| `reprioritize` / other directives | Act per the body. |
| `park` (from `daemon`) | **Fleet sleep — QUIESCE all loops** (see § Park/wake below). |
| Anything else | Log and no-op (do not crash). |

The `--assignee` mirror on every `topic == assign` re-task is **NOT optional** — see Step 4 rationale.

---

## § Park / wake — fleet idle-down (hk-s8qi M2, codename:sleep-wake)

### Default behaviour (pinned)

**When `comms recv --follow` delivers a `park` message (topic="park", from="daemon")
and then exits with code 0** — the daemon's QuiesceArbiter has declared the fleet
genuinely drained and is putting this session to sleep.

**PARK procedure (do this immediately):**

1. **Stop the `harmonik subscribe` Monitor.** Kill or allow it to expire. Do NOT
   re-arm it.
2. **Do NOT re-arm `comms recv --follow`.** It self-exited on the park message;
   leave it stopped.
3. **Pause the ≤10-min progress-feed timer.** No status posts while parked.
4. **Await the pane nudge.** The daemon will inject Enter into your pane when new
   work arrives or a WAKE trigger fires.

> **CRITICAL: do NOT re-arm ANY loop between the park message and the pane nudge.**
> Re-arming the subscribe Monitor or `comms recv --follow` while parked defeats
> the idle-down goal — each heartbeat re-invokes a Claude turn and burns tokens.

### Detecting the park exit

The park path is:
1. `comms recv --follow` Monitor delivers a line with `"topic":"park","from":"daemon"`.
2. The Monitor exits with code 0 immediately after delivering the line.

Distinguish from a normal disconnect exit (code 0 but NO park line): a normal
disconnect SHOULD be re-armed; a park exit MUST NOT be re-armed. Check whether
the last Monitor output line contained `"topic":"park"` before deciding.

### WAKE procedure

On pane nudge (daemon injects Enter into your pane):

1. Re-run the **full boot sequence** (§ Boot sequence above, Steps 1–6).
2. Re-arm `comms recv --follow`.
3. Re-arm the `harmonik subscribe` Monitor.
4. Resume the progress-feed timer.
5. Re-poll `br ready` and continue draining the epic.

Treat a wake exactly like a fresh crew start — re-derive live state, do NOT trust
the pre-sleep snapshot.

### Note on captain-managed sleep

The captain MAY also call `harmonik crew stop <name>` to fully terminate the crew
pane when the fleet sleeps. In that case this park/wake procedure is not needed
— the crew is simply re-started by the captain on wake. The park procedure above
applies only when the crew pane remains open but its loops are quiesced.

Spec ref: `specs/park-resume-protocol.md` §3.2 and §4.2.

---

## § Operating loop — work YOUR epic on YOUR queue

This loop composes on `harmonik-dispatch`, scoped exclusively to `--queue <queue>`.

### 1. Find ready beads under the epic

```bash
# List epic's children and intersect with ready (no blocked/draft)
br list --format json --label codename:<epic_id>   # get beads in this epic's scope
br ready --format json --limit 0            # get ALL ready beads (--limit 0 = unlimited)
# intersect the two sets
```

**RULE — `br ready` = dispatchable-now, NOT is-there-work.** Always pass `--limit 0`:
bare `br ready` silently caps at 20 and can make your epic's queue look shorter — or
empty — when it isn't. Never read an empty `br ready` as "fleet drained" without ALSO
checking in-progress beads + beads blocked-by-an-open-epic + paused/failed queues.

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
drain trigger) and idle on the comms inbox waiting for the next assignment. **Keep
`comms recv --follow --json` armed while idle** — this is the case the captain
re-tasks you, so a dropped `--follow` here strands the re-task (§ Idle-crew-wake
protocol, Rule 1). You do NOT need to detect epic completion yourself — C1 does that
structurally when the epic's last child closes, and notifies the captain.

### 7. When no ready beads are available (all blocked / draft)

Post a `--topic status` message:

```bash
harmonik comms send --to "$STATUS_TARGET" --topic status \
  -- "crew <crew_name>: epic <epic_id> has no ready beads; idling"
```

Wait on the comms inbox. Do NOT spin-poll `br ready` more frequently than every
10 minutes. Do NOT try to unblock beads yourself — that is captain/dependency
judgment.

---

## § Progress feed (MANDATORY — success-criterion #3)

The crew MUST emit status on BOTH surfaces, on ALL four triggers.

### Two durable surfaces (both required)

**Surface 1 — Status feed comms (live feed):**

Resolve the routing target once at boot from the project config (WE7 —
defaults to 'captain' when `watch.status_target` is absent):

```bash
STATUS_TARGET=$(python3 -c "
import re, sys, os
cfg = os.path.join(os.environ.get('HK_PROJECT', '.'), '.harmonik/config.yaml')
try:
    if os.path.exists(cfg):
        m = re.search(r'^\s*status_target:\s*(.+)', open(cfg).read(), re.MULTILINE)
        if m: sys.stdout.write(m.group(1).strip().strip('\"')); sys.exit(0)
except Exception: pass
sys.stdout.write('captain')
" 2>/dev/null || echo captain)
```

```bash
harmonik comms send --to "$STATUS_TARGET" --topic status -- "<update>"
```

Durable in `events.jsonl`; the captain reads it via `comms recv`/`comms log`.

**Surface 2 — Epic journal in beads (durable record):**

```bash
br comments add <epic_id> "<update>"   # TEXT is positional (or --message "<update>"); there is NO --body flag
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

3. **Timer:** at least once every **10 minutes** while actively dispatching
   beads; at least once every **15 minutes** when idle (no ready beads) or
   draining (all beads submitted, awaiting completion) — heartbeat-style liveness:
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

> For the gauge thresholds (warn vs act and their real default values),
> `keeper doctor`, and confirming liveness (`live-watcher` check in `doctor`),
> see the **`keeper` skill** (`.claude/skills/keeper/SKILL.md`). This section
> only covers what a crew does on restart; the keeper skill owns the mechanism.

On restart, re-run the full boot sequence from Step 1:
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
- Re-processing a `topic == assign` with a dedupe hit → no-op (same
  `event_id`).

### § Restart verification — who confirms the ACK (hk-uldg)

You do NOT verify your OWN restart-now: the `/clear` wipes your context before the
keeper's `[KEEPER ACK <nonce>] received restart` line could ever be read by you. An
**external** watcher confirms it instead — the **captain** runs `harmonik keeper
await-ack --agent <you> --kind restart` after triggering your restart (captain
watches crews; see the captain skill §10 and the keeper skill § Verifying a restart
with await-ack). If that ACK times out the captain re-arms your keeper — that is
their job, not yours.

**Self-service liveness check (this you CAN do, while live).** If you are unsure the
keeper is even reachable for your pane, ping it and confirm the ACK in your OWN pane
— use a FRESH unique nonce each time:

```bash
n=ping-$(date +%s%3N)
harmonik keeper ping --agent <you> --nonce "$n"
harmonik keeper await-ack --agent <you> --nonce "$n" --kind ping --timeout 15s
```

Exit 0 = keeper alive and watching your pane; exit 3 = it emitted
`session_keeper_ack_timeout` — surface it to the captain over comms
(`--from <you>`), do NOT silently continue assuming the keeper will save you.

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
  AND comms re-task). This is the captain's attribution source for ALL run events —
  `epic_completed`, `run_failed`, `run_stale`, wedge. Stale or missing assignee =
  "whose bead is this?" round-trips (Gap 1, F13).
- Emit status on BOTH surfaces (`comms --topic status` AND `br comments`) on ALL
  four triggers (boot, bead-close, ≤10-min timer while dispatching / ≤15-min when
  idle or draining, drain).
- Re-hydrate from durable state on restart (handoff frontmatter AND/OR beads
  `assignee`; prefer beads if they disagree).
- Keep `comms recv --follow --json` armed for the whole life of the session,
  **including while drained/idle**, and **RE-ARM it on every keeper-restart/resume
  and on any mid-session stream death** — idle crews do not reliably wake on a bare
  `send` without it, and a dropped `--follow` strands a captain re-task
  (§ Idle-crew-wake protocol — load-bearing).
- Use `--json` output for all `comms recv` and `br` parsing.

### MUST NOT

- Submit to the `main` queue (HARD RULE — any crew that submits to `main` is in
  spec violation).
- **Pre-assign a dispatchable bead.** The daemon claims dispatchable beads via
  `br claim`, which **REFUSES an already-assigned bead** → `max_attempts_exceeded`,
  `run_id=null`, and the bead **never dispatches**. `--assignee` goes on the
  **EPIC only** (Step 4 — the captain's attribution mirror); every child /
  dispatchable bead you submit **stays UNASSIGNED**. (Refs hk-kr791, hk-amed0.)
- `br close`, `br claim`, or `br reopen` any bead (daemon-only terminal writes).
- Spawn Agent-tool sub-agents for the epic's work (use the daemon queue — see
  harmonik-dispatch).
- Parse non-JSON `comms`/`br` output.
- Re-dispatch the same bead more than once without reporting to the captain first.
- **Self-`/quit`, `/clear`, or exit on a keeper WARN.** A warn is informational;
  only the keeper's ACT path performs the reset cycle (§ Self-restart via the
  keeper). Self-quitting on a warn is a known failure mode.

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
- `.claude/skills/keeper/SKILL.md` — the session-keeper contract: warn-vs-act
  thresholds, `keeper doctor`, and the crew-restart re-hydration mechanism
  referenced by § Self-restart via the keeper.
