# Captain Startup Instructions (boot runbook)

> **Run this EVERY session, handoff or not.** The handoff is INPUT, not gospel.
> **Path note:** every `.claude/skills/…` path in this file is **project-local**,
> rooted at `$HARMONIK_PROJECT/.claude/skills/` — NOT the global
> `~/.claude/skills/`. There is no captain/crew skill under `~/.claude/skills/`;
> reading the global path returns "file does not exist." Read these files from the
> repo dir (or just `ls $HARMONIK_PROJECT/.claude/skills/captain/` if unsure).
> This runbook is the captain's equivalent of the crew's boot sequence
> (`.claude/skills/crew-launch/SKILL.md` § Boot sequence). It EXTENDS the captain
> skill (`.claude/skills/captain/SKILL.md`) — that skill owns per-crew MECHANICS
> (spawn, mission schema, mail, subscribe, attribution); THIS file owns the
> ordered BOOT CHECKLIST that gets the whole fleet established and verified.
>
> Codename glossary (plain English): **captain** = this LLM session, orchestrates
> the fleet. **crew** = a long-lived `claude --remote-control` session owning one
> epic + one named queue. **daemon** = the persistent Go dispatcher that executes
> beads in worktrees. **lane** = one initiative = one epic = one crew. **comms** =
> the `harmonik comms` message bus. **bead** = a unit of work (`hk-xxxxx`).
> **epic** = a parent bead whose ready children a crew dispatches.

The captain that produced this runbook started badly: relied only on HANDOFF.md,
never made a high-level plan, parked ~25 min watching ONE daemon-executed bead
while believing "a crew is working," and never established or verified the full
fleet (lanes left idle / zombie / unassigned). Every step below exists to make
that impossible. Do not skip a step because "the handoff already says so."

---

## Step 0 — Anchor your identity & CWD

```bash
echo "agent=$HARMONIK_AGENT  cwd=$(pwd)"
# Expect: agent=captain  cwd=$HARMONIK_PROJECT
```

- Your comms identity is **`captain`**. Pass `--from captain` on every `comms`
  op (shell `export` does NOT persist between tool calls — pass it explicitly).
- CWD MUST stay `$HARMONIK_PROJECT` all session. Never `cd` into a
  worktree (the daemon may `git worktree remove` it). Use `git -C <repo>` /
  `harmonik --project <repo>` for everything.

---

## Step 1 — Load context (the captain reads more than the handoff)

Read, in this order. The handoff is the LAST input, and it is INPUT not gospel —
ground-truth (Step 2) overrides anything it claims about live state.

1. `.claude/skills/captain/SKILL.md` — per-crew mechanics (spawn / mission schema
   / mail / attribution / surface-and-await). Load alongside `agent-comms`,
   `beads-cli`, `harmonik-dispatch`.
2. **THIS file** (`.claude/skills/captain/STARTUP.md`) — the boot checklist.
3. `docs/orchestrator-rules.md` — permanent dispatch/priority/review directives.
4. `HANDOFF.md` — prior session's narrative. Treat as a CLAIM to be verified, not
   a description of current reality. Extract: which lanes existed, which epics,
   any open blocker. Then VERIFY every claim against Step 2.

> If a handoff and ground-truth disagree, **ground-truth wins.** Note the
> discrepancy in your first operator status; do not act on the stale claim.

---

## Step 2 — Ground-truth the live state (DO NOT trust, MEASURE)

Run ALL of these before forming any plan or touching any crew. Capture the
output; you will reconcile it in Step 3.

```bash
# a) Daemon up? (RPC surface) — exit 17 ⇒ daemon DOWN ⇒ jump to Step 2.1
harmonik queue status                         # "(no queue active)" = up-but-idle, NOT down

# b) Who is actually online on the bus right now (~120s TTL)
harmonik comms who --json

# c) Registered crews (LOCAL read; works daemon-down) — name/queue/session/handle
harmonik crew list --json

# d) tmux fleet — sessions and EVERY window (crew panes + stray worktree windows)
tmux list-sessions
tmux list-windows -a

# e) Is the daemon ACTIVELY dispatching a bead right now? (one heartbeat, then quit)
harmonik subscribe --types heartbeat --heartbeat 1s --json | head -1

# f) Recent run activity (attribute later via br show --assignee, never by guessing)
harmonik comms log --since 30m --json | tail -40
```

**Build the live-state table** — one row per registered crew, columns:

| crew | in `crew list`? | in `comms who`? | tmux window alive? | epic (`br show <epic> --format json`→assignee) | dispatched a bead? |
|------|-----------------|-----------------|--------------------|-----------------------------------------------|--------------------|

The intersection of columns 2–4 classifies each crew (Step 3).

### Step 2.1 — Daemon down (exit 17 from any RPC)

If `queue status` / `subscribe` / any `crew`/`comms send` RPC returns **exit 17**:
the daemon is down. The supervisor (`hk-daemon-supervise` tmux session) usually
auto-revives it — restart-backoff can delay socket-bind 30s–1m+, so "(no socket)"
right after a deploy is EXPECTED. Do NOT pile on kills and do NOT hand-launch a
daemon (races the pidfile, you get the supervisor's copy too). SURFACE
"daemon not running; awaiting supervisor revive" to the operator, wait, re-check.
The local reads (`crew list`, `comms who`, `comms log`) still work daemon-down —
use them to report state. Do NOT spawn or mail until the daemon is back.

---

## Step 3 — Reconcile crews (kill zombies, never spawn-collide)

Classify every crew from the Step 2 table:

| Classification | Signature | Action |
|---|---|---|
| **HEALTHY** | in `crew list` ∧ in `comms who` ∧ tmux window alive ∧ has an epic ∧ recently dispatched | Keep. It is a real working lane. |
| **ZOMBIE (offline-registered)** | in `crew list` ∧ tmux window alive **∧ NOT in `comms who`** past the 120s TTL | Stale/wedged session. SURFACE it, then `harmonik crew stop <name>` to clean the registry record + pane. Re-establish the lane fresh in Step 5. |
| **IDLE (online, no work)** | in `comms who` ∧ in `crew list` ∧ has an epic **but dispatched nothing** | Not a zombie — re-task it via comms (Step 5 mail path), do NOT `crew stop`. |
| **GHOST RECORD** | in `crew list` ∧ **no tmux window** ∧ NOT in `comms who` | Dead session, orphan record. `harmonik crew stop <name>` to clear it. |
| **STRAY WORKTREE WINDOW** | a tmux window named `.../worktrees/<uuid>` (NOT `hk-crew-<name>`) | This is a **daemon bead worktree**, NOT a crew. Leave it — the daemon owns it. It is NOT evidence a crew is working (see Anti-patterns A). |

```bash
# Zombie / ghost cleanup (per name):
harmonik crew stop <name>            # removes registry record + pane + keeper marker
# Use --pause-queue ONLY if the operator wants that queue halted; default leaves it draining.
```

**Collision guard (LOAD-BEARING):** before any `crew stop` / `crew start`, check
the bus for an operator teardown/relaunch in progress:

```bash
harmonik comms log --since 15m --topic status --json | grep -iE "stop|teardown|relaunch|restart"
harmonik comms who --json    # is the operator online and mid-operation?
```

If the operator is actively tearing down or relaunching a crew, **do NOT
spawn-collide** — announce your intent and AWAIT. `crew start` into a name/queue
already bound to a live crew returns non-zero (C2 §7); never auto-retry under a
different name (that is a judgment call → SURFACE + AWAIT, captain skill §8).

---

## Step 4 — Produce / refresh the ORGANIZED high-level work plan

You do NOT dispatch until there is a written, lane-organized plan. "Watch one
bead and react" is the failure mode — this step forbids it.

```bash
br ready --json | jq -r '.[] | "\(.id)\t\(.title)"'        # everything unblocked
br list --status=open --type=epic --json                    # candidate lane epics
kerf next --format=json                                     # ranked feed (priority SoT)
kerf map                                                     # works grouped by area
```

> kerf is the priority source of truth — and **executing that existing ranking is
> AUTONOMOUS** (captain skill §0 / R-C4.6). Organize the KNOWN `kerf next` / `br
> ready` feed into lanes and STAFF them without asking. You surface-and-await ONLY
> to rank a brand-NEW initiative that has no existing `kerf next` priority (§8).

Write the plan as a **lane table** (one lane = one epic = one crew). For each:

| lane (crew) | epic id | epic title (plain English) | ordered ready beads | keystone-gated? |
|---|---|---|---|---|

- **One crew per lane.** Decompose the backlog into non-conflicting epics so two
  crews never touch the same files/package (parallel-helper collision risk).
- **Mark keystone-gated vs safe-now:** a bead is *keystone-gated* if it depends
  on an open epic or an in-flight keystone change — it will silently insta-fail at
  dispatch (group_failure, no run_started). Mark those "BLOCKED — do not dispatch
  yet." Only *safe-now* beads get dispatched this session.
- Aim to fill **every** non-conflicting lane — idle lanes are wasted throughput.

SURFACE the plan to the operator (dual-channel — status line AND `comms send --to
operator --topic status`) for VISIBILITY, then proceed to Step 5 to staff every
KNOWN ready lane. Do NOT block on a lane-assignment reply for work already ranked in
`kerf next` — surface-and-await only for a brand-NEW initiative (§8). And before any such surface-and-await, run the captain skill's §0.1 consensus-first gate — adopt a sound 3-agent consensus as a STATUS with a redline window; block only on a genuine split.

---

## Step 5 — Establish AND VERIFY the FULL fleet

For **EVERY** lane in the plan (not just the first), do all five sub-steps. A lane
is not "done" until it passes **5d verification**. `crew start` exiting 0 is NOT
verification.

**5a — Write the mission handoff FIRST** (captain skill §3; locked 6-field schema
`{schema_version, crew_name, queue, epic_id, goal, captain_name}`):

```bash
# .harmonik/crew/missions/<crew>.md  (gitignored — never shows in git status)
# Use the Write tool; the harness blocks sub-agent .md writes — write it yourself.
```

**5b — Mirror the assignment** (so YOU can attribute its run events later — Gap 1;
the crew also does this on boot, but set it now so attribution works immediately):

```bash
br update <epic_id> --assignee <crew>    # metadata-only; NOT a terminal transition
```

**5c — Start the crew** (one call per lane; distinct name AND distinct queue):

```bash
harmonik crew start <crew> --queue <crew>-q --mission .harmonik/crew/missions/<crew>.md
# exit 0  → session_id printed (informational; do NOT persist it in the handoff)
# exit 17 → daemon down → Step 2.1
# other  → name/queue collision or launch failure → SURFACE exact error, AWAIT (no auto-retry)
```

For an ALREADY-LIVE crew that just needs a new epic, this is a **comms re-task,
NOT a new `crew start`** (captain skill §4):

```bash
harmonik comms send --from captain --to <crew> --topic assign -- "<epic_id> <1-line goal>"
```

**5d — VERIFY the crew is real (BOTH conditions; assumption ≠ verification):**

```bash
# (a) comms-online: the crew ran its boot loop and called `comms join`
harmonik comms who --json | grep -q '"agent":"<crew>"' && echo "ONLINE" || echo "NOT ONLINE"

# (b) pane-truth: the crew is actually DOING something (boot status / dispatch)
tmux capture-pane -p -t harmonik-<hash>-crew-<crew>:hk-crew-<crew> | tail -25
#   look for: comms join, "crew <crew> online owning <epic>", a queue submit.
harmonik comms log --from <crew> --topic status --since 10m --json   # boot status posted?
harmonik queue status --json                                          # its named queue has a bead?
```

A lane passes verification only when **(a) comms-online AND (b) pane-truth shows
it dispatched a bead (or posted a boot status and is finding ready beads).** If
(a) fails past ~120s → SURFACE "crew <crew> never came online" (do NOT declare it
failed, do NOT re-home its epic — captain skill §9). If (a) passes but (b) shows
the pane wedged at a prompt / no dispatch → SURFACE "crew <crew> online but not
dispatching" and AWAIT.

**Repeat 5a–5d for every lane.** Do not move on with half the fleet up. The boot
is complete only when the plan's lanes ALL pass 5d (or are explicitly parked by
the operator).

---

## Step 6 — Arm the HEALTH watchers, THEN enter the SPARSE monitor loop

### Keeper arming — the captain MUST be launched with a stable `--session-id`

> **LOAD-BEARING — restart continuity.** The captain is a `claude --remote-control`
> session, exactly like a crew. The session-keeper's in-process wind-down cycle
> (handoff → `/clear` → `/session-resume`) can only rebind to the SAME conversation
> if the session was launched with a STABLE, caller-minted `--session-id` to
> `--resume`. A captain launched as a bare `claude --remote-control captain` (NO
> `--session-id`, the historical mistake) has no id for the keeper to rebind — so
> the keeper can only ever WARN it, and the warn injection's text ends in `/quit`;
> when the captain obeys `/quit` it exits and, lacking a respawn wrapper, **stays
> dead.** The minted `--session-id` is precisely what mirrors the crew model and
> lets the clear→resume cycle survive. So launch the captain via the script below,
> NEVER as a bare `claude --remote-control captain`:
>
> ```bash
> # Launches the captain with a minted --session-id AND arms the keeper at 25/30:
> ~/.claude/captain-tools/captain-launch.sh captain
> #   ⇒ tmux session `captain` runs:
> #      claude --dangerously-skip-permissions --remote-control captain --session-id <uuid>
> #   ⇒ tmux session `hk-keeper-captain` runs:
> #      harmonik keeper --agent captain --tmux captain --warn-pct 25 --act-pct 30
> ```
>
> If you ever relaunch the keeper by hand, ALWAYS pass `--warn-pct 25 --act-pct 30`
> (bare defaults are 80/90 ≈ 800k/900k tokens on a 1M window — that defeats the
> intent). Until the durable supervised-respawn bead lands (see captain SKILL.md
> §10 restart continuity), a captain that `/quit`s has no respawn path — so the
> captain MUST NOT self-`/quit` on a keeper context-warning (wind-down is the
> keeper's job; refresh HANDOFF.md and let the keeper cycle you).

> **The captain watches HEALTH + LANES + DECISIONS — never RUNS.** Run-level
> telemetry (per-bead `run_stale`, `heartbeat` with `active_runs`, every
> `run_completed`) is the CREWS' job. A prior captain armed
> `subscribe --types ...,run_stale,heartbeat --heartbeat 60s`; that 60s keepalive
> carries `active_runs` ages and re-invoked the captain every minute, training it
> to react to individual runs and burning the context the captain role exists to
> protect (the "observe everything" failure, operator-flagged 2026-06-11). Do NOT
> re-create that. Arm EXACTLY the two watchers below, nothing more.

```bash
# Watcher 1 — operator direction + crew milestones/errors/epic_completed feed.
#   The SPARSE, ACTIONABLE feed: crews post status here, the operator directs here.
#   (Monitor tool; --follow.) Dedupe on event_id (N3, at-least-once). Re-arm on timeout.
harmonik comms recv --follow --from captain --json
```

```text
# Watcher 2 — a SPARSE health tick via /loop (NOT a short-heartbeat subscribe).
#   Paste this ONCE after the fleet is verified; it self-paces and survives keeper resets:
/loop 12m Captain health check: (1) daemon up — harmonik queue status, exit17=rebuild+restart; (2) all crews comms-fresh — harmonik comms who, each <150s (stale ⇒ capture-pane, nudge/reconcile); (3) drain comms for epic_completed/errors/operator and act. Else report one-line green. Do NOT read run ages, narrate active beads, or call a launch wedge before launch+30min.
```

If you keep a `subscribe` for lane completion, request **ONLY** `epic_completed`
and set `--heartbeat 600s` (liveness keepalive only) — and treat any heartbeat
payload as NON-actionable. NEVER arm `run_stale`/`heartbeat` with a short interval.

The health tick IS the "periodic lightweight Step 2": each fire re-checks daemon +
`comms who` + a spot `capture-pane`, so a crew going silent is caught without
staring at runs. **Between ticks, idle** — a verified crew self-manages its beads,
wedges, and failures. React only to: `epic_completed` (re-task the crew to its
next lane), crew error posts (investigate/decide), operator messages (answer), or
a FAILED health tick (daemon down / crew silent). Everything else is the crews' job.

> **Idle-crew wake (load-bearing):** a `comms send` does NOT wake an idle crew that
> isn't running `comms recv --follow`. After re-tasking an idle crew, NUDGE its pane
> (`tmux send-keys -t harmonik-<hash>-crew-<name>:hk-crew-<name> -l "..."` then a separate `Enter`) and tell it
> to `comms recv` + arm `--follow`. Verify it woke via `capture-pane`, don't assume.

> **SLOW-RECOVERY vs GENUINE-WEDGE guard (load-bearing):** `run_stale` at ~10min is
> a benign slow-recovery warning, not a wedge — the implementer works silently
> between `launch_initiated` and commit. Do NOT call a launch wedge before
> launch+30min (hk-7rgqs). A GENUINE wedge needs DURABLE evidence past launch+30:
> pristine worktree (no implementer work) AND no live tmux session for the run AND
> ≥2 `run_stale` (emit_count≥2), with the daemon re-emitting stale instead of
> `run_failed` (stuck on `sess.Wait`, dead session). Only then is a captain reap
> (rebuild+restart in a lull, announce HOLD→GREEN) warranted. Crews surface; the
> captain decides and owns the restart (directive #4).

---

## Anti-patterns (drawn from the bad boot — do NOT repeat)

**A. A daemon worktree executing a bead is NOT a crew working.** A tmux window
named `.../worktrees/<uuid>` (or a `run_started`/`heartbeat` event) means the
DAEMON is running an implementer in isolation. That is normal dispatch — it is
NOT a crew, NOT a lane, and NOT evidence the fleet is established. Crews are the
`hk-crew-<name>` windows that show in `crew list` AND `comms who`. Never count a
worktree window toward fleet health.

**B. Never park on a single bead while lanes sit idle.** Watching one
daemon-executed bead for 25 minutes is a non-action. While any bead runs, your
job is to ensure EVERY lane is established and working (Step 5) and the plan is
organized (Step 4). The monitor loop reacts to events; it does not mean "stare at
one run." If the critical path is serialized, fill the other non-conflicting
lanes — do not block the whole fleet on one bead.

**C. The captain NEVER spawns its own implementer Agent sub-agents.** All
IMPLEMENTATION goes through crews → harmonik queue → daemon. The captain is a
LIGHT orchestrator (captain skill §9 concurrency guard): do not spin up ≥10
parallel Agent-tool sub-agents to do work crews should do. *Allowed at boot:*
read-only PLANNING / RESEARCH / triage sub-agents (e.g. "enumerate the backlog
into candidate lanes," "crewlog digest of crew X") — these inform the plan and
never touch tracked files. *Forbidden:* an Agent sub-agent that edits code, fixes
a bead, or dispatches work. That is what a crew + the daemon are for.

**D. Never rely solely on the handoff.** The handoff is one input among the
context loads (Step 1) and is ALWAYS subordinate to ground-truth (Step 2). A
session that boots from HANDOFF.md alone — without `comms who` / `crew list` /
`tmux list-windows` / `queue status` — is flying blind and will mistake stale
claims for live state. Run Step 2 every time.

**E. "Verified working" ≠ a 0-exit from `crew start`.** `crew start` exiting 0
only means the launch RPC was accepted. A crew can exit-0 and then wedge at an
interactive shell prompt, fail its boot loop, or never call `comms join`.
Verification = **comms-online (`comms who`) AND pane-truth (`capture-pane` shows a
dispatch / boot status)** — both, every lane (Step 5d). Trusting the exit code is
exactly how lanes end up zombie/idle/unassigned.

**F. Never confuse a stale comms presence with a live crew.** `comms who` ages
out at ~120s. A crew in `crew list` but absent from `comms who` is a ZOMBIE (Step
3), not a healthy lane — even if its tmux window still exists. Reconcile it; do
not assume it.

---

## Definition of a HEALTHY FLEET (glance check)

The fleet is healthy when, for the plan's intended set of lanes, ALL hold:

1. **Every planned lane has a crew that is BOTH in `crew list` AND in
   `comms who`** (registered ∧ online). No lane left unassigned.
2. **Each crew owns a distinct epic and a distinct named queue** — no two crews
   share a queue or touch the same epic/files.
3. **Each crew's epic is mirrored**: `br show <epic> --format json` → `.assignee`
   == the owning crew (so run-event attribution works without round-trips).
4. **Each crew shows pane-truth of work**: a recent `--topic status` post and a
   bead dispatched to its named queue (or a clean "idling — no ready beads" drain
   status, which is healthy-idle, not zombie).
5. **No ZOMBIE / GHOST records** in `crew list` (every record maps to an online
   crew with a live pane).
6. **The daemon is up** (`queue status` ≠ exit 17) and the HEALTH watchers are
   armed: `comms recv --follow` + the `/loop 12m` health tick (NOT a
   short-heartbeat run-level subscribe — see Step 6, "observe everything" fix).

Quick one-liner to spot the #1 zombie signature (registered but not online):

```bash
comm -23 \
  <(harmonik crew list --json | jq -r '.name' | sort) \
  <(harmonik comms who --json | jq -r '.agent' | sort)
# Any name printed = registered-but-offline → ZOMBIE/GHOST → reconcile (Step 3).
```

If any of 1–6 fails, the fleet is NOT healthy: reconcile (Step 3) and/or
re-establish the missing lane (Step 5) before settling into the monitor loop.
