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

- **Identity guard (NORMATIVE — M8/hk-039z):** your comms identity is
  `$HARMONIK_AGENT`, verified above — NOT a hardcoded `captain`. Pass
  `--from "$HARMONIK_AGENT"` on every **send** op (shell `export` does NOT persist
  between tool calls — pass the resolved value explicitly). For **recv/who/log**
  use `--agent "$HARMONIK_AGENT"` (your inbox identity) — `--from` is a sender
  filter and would give you only messages you already sent. Only assert
  `--from captain` if `$HARMONIK_AGENT == captain` AND no other live `captain` is in
  `comms who`. **An uncommissioned `--from captain` (a session resumed under a
  different lane) freezes the fleet** (two-captains collision, `reference_my_comms_identity`).
  If this session was resumed under a non-captain lane, you are NOT the captain —
  do not run this skill's captain ops.
- CWD MUST stay `$HARMONIK_PROJECT` all session. Never `cd` into a
  worktree (the daemon may `git worktree remove` it). Use `git -C <repo>` /
  `harmonik --project <repo>` for everything.

---

## Step 0a/0b — Read tier-3 then tier-2 context (fast boot reads)

Read these two files BEFORE loading skills or the handoff. They encode state
that changes on a weeks/days cadence — project phase, locked decisions, active
lanes — so you skip re-deriving them from scratch. Step 2 VERIFIES their claims
against live reality; it does not discover state you already have here.

**0a — Tier-3 (weeks cadence):**

```bash
cat .harmonik/context/project.yaml
# Encodes: phase, forbidden_actions, locked_decisions.
# If missing: use STATUS.md §Decisions for locked decisions; treat phase as "operational".
```

**0b — Tier-2 (days cadence):**

```bash
cat .harmonik/context/captain-lanes.md
# Encodes: active_lanes table (crew/epic/queue/model), operator_initiatives, parked, pipeline.
# If missing: treat active_lanes as unknown — Step 2 ground-truth derives it.
```

**0c — Tier-2 direction-log (READ BEFORE acting):**

```bash
cat .harmonik/context/direction-log.md
# Append-only sequencing intent: one entry per direction CHANGE
# (WHAT / WHY / RETURN-PATH / expires). This is the file a fresh /clear reads to
# recover "why we paused X for Y and in what order we resume." READ it before you
# form or act on any plan — its RETURN-PATH is ground truth for SEQUENCING intent.
# An entry past its `expires:` LAPSES to "resume the standing autonomous posture"
# (NEVER a hold) — surface an expired-but-present entry; do not obey it.
# Folder-scoped how-to-use directives: .harmonik/context/AGENTS.md (CLAUDE.md symlink).
# If missing: no recorded direction change — proceed from tier-2/tier-3 + the digest.
```

> **`lanes.json` is NOT a boot-read.** `.harmonik/context/lanes.json` (the
> machine-readable lane→epic index) is read by the every@5m ops-monitor, not by the
> captain at boot. You keep it in sync when you add/retask/park a lane (per
> `.harmonik/context/AGENTS.md`), but you do not cat it during boot.

> **Boot-read order (Track C):** tier-3 (project.yaml) → tier-2 (captain-lanes.md)
> → direction-log.md → orchestrator-rules. Read direction-log.md BEFORE acting.

> **Update discipline:** the tier-3 file changes rarely (phase shifts, new locked
> decisions). The tier-2 files change per session (lane assignments, epic
> handoffs; a direction-log entry on every direction CHANGE). Update captain-lanes.md
> and append any direction-log entry at the END of each session before writing
> HANDOFF.md so the next boot reads accurate lane state.

---

## Step 1 — Load context (the captain reads more than the handoff)

Read, in this order. The handoff is the LAST input, and it is INPUT not gospel —
ground-truth (Step 2) overrides anything it claims about live state.

1. `.claude/skills/captain/SKILL.md` — per-crew mechanics (spawn / mission schema
   / mail / attribution / surface-and-await). Load `beads-cli` alongside it (write
   discipline is used immediately). **SLIM COLD-BOOT (boot-spike Lever 2):** do NOT
   eager-load the full `agent-comms` and `harmonik-dispatch` SKILL.md bodies at boot
   — their boot-critical content (comms join/send/recv; the daily-loop summary) is
   already carried by `orchestrator-rules` (item 3). Load each in FULL on first use
   of its surface (first `harmonik comms` op / first queue dispatch). Deferral ≠ skip:
   load before the first call, not after.
2. **THIS file** (`.claude/skills/captain/STARTUP.md`) — the boot checklist.
3. the `orchestrator-rules` skill (`.claude/skills/orchestrator-rules/SKILL.md`) — the
   canonical standing rules (permanent dispatch/priority/review directives).
4. `HANDOFF.md` — prior session's narrative. Treat as a CLAIM to be verified, not
   a description of current reality. Extract: which lanes existed, which epics,
   any open blocker. Then VERIFY every claim against Step 2.

> If a handoff and ground-truth disagree, **ground-truth wins.** Note the
> discrepancy in your first operator status; do not act on the stale claim.

> **DO NOT full-read `AGENT_INDEX.md` / `STATUS.md` / `TASKS.md` at boot
> (M5/hk-039z — context economy).** The general project `CLAUDE.md` reading order
> is written for an implementer orchestrator; the captain is a fleet orchestrator
> and needs only **phase + locked-decisions + lane-table + backlog**, which the
> tier-3/tier-2 files (Steps 0a/0b) and the boot digest (Step 2) already provide —
> and Step 2 ground-truths every live claim those files would carry anyway. Read a
> specific STATUS/TASKS *section* on demand only if the digest or a decision flags
> a gap. The keeper facts you need at boot are the ~10-line cheatsheet below
> (§ Keeper cheatsheet), NOT the full 484-line `keeper` SKILL.md.

---

## Step 2 — Verify live state (check tier-3/tier-2 claims against ground-truth)

> You have already READ claimed state from `.harmonik/context/` (Steps 0a/0b).
> This step VERIFIES those claims — daemon up, crews online, epics still assigned.
> It does NOT re-discover lanes from scratch. Any discrepancy between tier-2 claims
> and live state here means tier-2 is stale; update `captain-lanes.md` after Step 3.

> **MANDATORY — run the boot digest (M4/M5/hk-039z). This is the boot path, not an
> optional shortcut:**
> ```bash
> scripts/captain-boot-digest.sh        # in-repo, portable; --project DIR optional
> ```
> This executes ALL of Steps 2a–2g **and** Step 4 in one shell call and emits a
> single Markdown STATE DIGEST (daemon status, agents online, crew registry, tmux
> fleet, paused queues, recent comms, ready beads, open epics, kerf next, kerf map).
> **Read the digest, then go to Step 3.** Re-run an INDIVIDUAL command (from the
> reference list below) ONLY if a specific digest section is empty/ambiguous and
> needs a deeper look — never re-run the whole set (that is the double-run that
> defeats the savings).
>
> If the script is missing on this box, copy it from `scripts/` or fall back to the
> individual commands in the reference list below — but the digest is the intended
> path.

**Reference — the individual commands the digest already runs** (do NOT re-run these
wholesale after the digest; they are here only so you can rerun ONE if a digest
section needs a deeper look):

```bash
# a) Daemon up? (RPC surface) — exit 17 ⇒ daemon DOWN ⇒ jump to Step 2.1
harmonik queue status                         # "(no queue active)" = up-but-idle, NOT down
# b) Who is actually online on the bus right now (~120s TTL)
harmonik comms who --json
# c) Registered crews (LOCAL read; works daemon-down) — name/queue/session/handle
harmonik crew list --json
# d) tmux fleet — sessions and EVERY window (crew panes + stray worktree windows)
tmux list-sessions ; tmux list-windows -a
# f) Recent run activity (attribute later via br show --assignee, never by guessing)
harmonik comms log --since 30m --json | tail -40
# g) Paused / failed queues — 'up' ≠ 'dispatching'; paused-by-failure = NOT healthy.
#    HEALTHY criterion #6 (exit≠17) is a FALSE-GREEN for a paused main/crew queue.
harmonik queue list --json | jq -r '.queues[]|select(.status|test("paused|complete-with-failures"))|"\(.name)\t\(.status)"'
# Any output = those queues are BLOCKED. Resume with: harmonik queue resume --queue <name>
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

> **Already covered by the boot digest (M5/hk-039z).** Sections 7–10 of the digest
> you ran in Step 2 (ready beads, open epics, kerf next, kerf map) ARE this step's
> discovery — build the lane table from that output. Do NOT re-run the raw `br
> ready` / `br list` / `kerf next` / `kerf map` commands; rerun ONE only if its
> digest section was empty/ambiguous.

> kerf is the priority source of truth — and **executing that existing ranking is
> AUTONOMOUS** (captain skill §0 / R-C4.6). Organize the KNOWN `kerf next` / `br
> ready` feed into lanes and STAFF them without asking. **Resuming / un-parking /
> re-staffing a KNOWN parked or drained lane is equally AUTONOMOUS** — a lane
> recorded in ANY durable doc (captain-lanes / admiral-initiatives / lanes.json /
> direction-log / a prior HANDOFF) or ever ranked is KNOWN, even when it is parked or
> shows zero ready beads right now. You surface-and-await ONLY to rank a brand-NEW
> initiative **never recorded in any durable doc and never ranked** (§8). Canonical:
> orchestrator-rules §Autonomy.

Write the plan as a **lane table** (one lane = one epic = one crew). For each:

| lane (crew) | epic id | epic title (plain English) | ordered ready beads | keystone-gated? |
|---|---|---|---|---|

- **One crew per lane.** Decompose the backlog into non-conflicting epics so two
  crews never touch the same files/package (parallel-helper collision risk).
- **Assign a model per lane (model-tiering):** add a `model` column to the lane
  table. Decision rule: **Sonnet** = lane-drain crews working file-disjoint clean
  beads (set mission clause "escalate to captain on ANY run_failed, do NOT
  self-classify"); **Opus** = design, test, investigation, or any lane where the
  crew must triage failures itself. Record the choice in `captain-lanes.md` (tier-2)
  so it survives restarts without re-derivation.
- **Mark keystone-gated vs safe-now:** a bead is *keystone-gated* if it depends
  on an open epic or an in-flight keystone change — it will silently insta-fail at
  dispatch (group_failure, no run_started). Mark those "BLOCKED — do not dispatch
  yet." Only *safe-now* beads get dispatched this session.
- Fill every non-conflicting lane **that has ready beads** — idle lanes are wasted throughput. **LAZY BOOT (boot-spike Lever 3):** a lane whose epic has ZERO ready beads (`br ready` shows none) AND no in-flight run is marked **PARKED — no ready beads** in the lane table and is NOT staffed at boot. Booting a crew for an empty-backlog lane spends Opus cache_creation for a session that will immediately idle (the boot spike). The ops-monitor **backlog-ready flag** (Step 6 / CE4) re-staffs a PARKED lane the moment ready work + a free slot coexist — so lazy boot loses no throughput, it just defers the cost to when there is work. **"PARKED — no ready beads" is a FACT, fully decoupled from "operator-GATED."** It does NOT mean the lane is held for the operator: resuming it the instant ready work + a free slot coexist is AUTONOMOUS (§0). A lane is GATED only when a named, dated, owned, expiring gate is present (in `lanes.json`, a non-null unexpired `gate`); absence of a live named gate means KNOWN/resumable. Canonical: orchestrator-rules §Autonomy.

SURFACE the plan to the operator (dual-channel — status line AND `comms send --to
operator --topic status`) for VISIBILITY, then proceed to Step 5 to staff every
KNOWN ready lane. Do NOT block on a lane-assignment reply for work already ranked in
`kerf next` — surface-and-await only for a brand-NEW initiative (§8). And before any such surface-and-await, run the captain skill's §0.1 consensus-first gate — adopt a sound 3-agent consensus as a STATUS with a redline window; block only on a genuine split.

---

## Step 5 — Establish AND VERIFY the FULL fleet

For **EVERY** lane in the plan (not just the first), do all five sub-steps. A lane
is not "done" until it passes **5d verification**. `crew start` exiting 0 is NOT
verification.

**5·0 — Lazy-boot gate (boot-spike Lever 3):** before 5a–5c for a lane, confirm it has ready work: `br ready --limit 0` filtered to the lane's epic (or its `codename:` label). If ZERO ready beads AND no in-flight run for that epic, mark the lane **PARKED — no ready beads** and SKIP 5a–5d — do NOT `crew start`. The ops-monitor backlog-ready flag re-staffs it when ready beads appear. Only lanes with ready work proceed to 5a.

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

**5c STAGGER RULE (boot-spike Lever 1 — token-opt):** After each `crew start`, wait for `comms who` to show the crew online (~30–60s), THEN wait an additional **2 minutes** before launching the next crew. Do NOT batch `crew start` calls. The 2-min gap lets each crew's cache prefix warm against the captain's already-warm shared prefix instead of all crews creating cold `cache_creation` prefixes simultaneously (the boot spike). The existing 5d verification (comms-online + pane-truth) still gates moving to the next lane; the stagger adds the explicit 2-min cache-warm wait on top of it.

For an ALREADY-LIVE crew that just needs a new epic, this is a **comms re-task,
NOT a new `crew start`** (captain skill §4):

```bash
harmonik comms send --from "$HARMONIK_AGENT" --to <crew> --topic assign -- "<epic_id> <1-line goal>"   # --from = your verified lane (Step 0 identity guard), NOT a hardcoded "captain"
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

```bash
# (c) Workflow quality-bar: a completed run must have a matching reviewer_verdict —
#     a run_completed with NO verdict = review was BYPASSED (single-mode / no review loop).
#     ROBUST METHOD (M2/hk-039z): do NOT grep run_started for workflow_mode. That field
#     EXISTS on the type (core.RunStartedPayload `workflow_mode,omitempty`) but the daemon's
#     emit struct omits it, so it is ABSENT on live run_started events → a top-level
#     `.workflow_mode` grep silently always passes (false GREEN). If you must read it, it
#     nests under `.payload.workflow_mode` — but prefer the verdict join below.
for rid in $(jq -r 'select(.type=="run_completed") | .payload.run_id' \
               .harmonik/events/events.jsonl | tail -10); do
  vc=$(jq -r --arg r "$rid" \
        'select(.type=="reviewer_verdict" and .payload.run_id==$r) | .payload.run_id' \
        .harmonik/events/events.jsonl | head -1)
  [[ -z "$vc" ]] && echo "WARN: run $rid completed with NO reviewer_verdict (review bypassed)"
done
# Any output = surface to operator; do NOT let review-bypassed runs accumulate.
# NOTE (CE4): this deterministic check should MOVE to the Sonnet ops-monitor; it is
# here as an interim until that monitor absorbs it.
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

## Step 6 — Arm the HEALTH watchers, THEN run the ACTIVE monitor loop

> **The captain is the ENGINE that drives epics to completion — NOT a passive event
> router.** Role frame (load-bearing): the **admiral** sets strategy and direction;
> the **captain** is the engine that coordinates the pistons (the **crew**) to push
> every staffed epic to COMPLETION; the **crew** are the pistons. The captain's core
> job is **PUSHING EPICS TO COMPLETION** — owning end-to-end delivery of each lane:
> it watches each epic's progress, diagnoses blockers, decides how to unblock
> (redeploy, re-task, re-sequence, escalate), and keeps every lane MOVING. **No epic
> stalls without the captain actively working its unblock.** The captain does not
> just react and hand off to crews — it DRIVES. (Same mandate in SKILL.md §0.)
>
> **The monitor loop is ACTIVE, not passive — it drives epics, reacts to events, AND
> continuously pulls the backlog (SKILL.md §0 BACKLOG-PULL is the same mandate).**
> Reacting "only to events" is the IDLE-FLEET failure: when lanes drain or block, no
> event fires, so a purely event-driven captain never re-staffs them and the whole
> fleet goes idle.
> The captain therefore runs a CONTINUOUS loop: it blocks on the Watcher-1 /
> Watcher-2 feeds for events, AND between events (at least every **≤5 minutes**) it
> runs a `kerf next` + `br ready --limit 0` REFRESH-AND-STAFF pass. **If ANY free
> crew/queue slot exists AND ready beads are in the feed, STAFF them immediately —
> do NOT wait for an event to staff.** This is the SAME "No lane left idle while
> ready work exists" + BACKLOG-PULL mandate as captain SKILL.md §0; STARTUP and SKILL
> are consistent on it. An ops-monitor `backlog-ready` flag is a CONVENIENCE trigger,
> not the only trigger — the captain pulls the backlog on its own timer even when no
> flag fires.
> This ≤5-minute backlog pull runs ONLY while the captain is already AWAKE in its
> active loop; it does NOT re-introduce a dormant poll or 24/7 token burn. A DORMANT
> captain is still woken solely by the wake-economy push paths (ops-monitor `[IMMEDIATE]`
> + the watch staffing-starvation backstop / escalations + operator messages) — that
> wake path is unchanged (see watch SKILL.md: no health tick).

### Keeper cheatsheet (the ~15 lines the captain needs — M5/hk-039z)

The captain is keeper-MANAGED, not a keeper operator — it does NOT need the full
`keeper` SKILL.md at boot. The facts that matter:

- **Band (canonical):** warn 200k / act 215k ABSOLUTE tokens. Arm with
  `--warn-abs-tokens 200000 --act-abs-tokens 215000`. The pct flags are INERT on the
  captain's 1M window (keeper warns if passed). Source of truth = the launcher
  defaults (`keeper.DefaultWarnAbsTokens` / `DefaultActAbsTokens`, what
  `harmonik start captain` arms) / `.harmonik/config.yaml` `keeper:` block.
- **All keeper verbs are FLAG-ONLY (hk-nbft):** `--agent <name>`, never a positional
  (a positional exits 2).
- **On WARN:** terse-ack one line, keep working; at the next clean idle point write
  `HANDOFF-captain.md` (with the `<!-- KEEPER:<nonce> -->`) and run
  `harmonik keeper restart-now --agent captain`, keep the turn open, stop typing.
  **NEVER `/quit` / self-terminate** — that exits the captain permanently.
- **Self-restart is VERIFIED in-process** by `harmonik keeper restart-now --agent
  captain` itself (it does the synchronous verified clear→resume and survives your
  `/clear`). The native launcher no longer arms any external wrapper — the old
  `keeper-restart-verified.sh` is off the launch path (review B; the dead script
  file is deleted by ES8). A CREW restart you trigger, YOU verify with `keeper
  await-ack`.
- **Restart is a NON-EVENT for a crew** — do not re-`crew start` a crew that cycled.
- Full detail (config block, FORCE-ACT, doctor checks, await-ack handshake) is in the
  `keeper` SKILL.md — read it ON DEMAND, not at boot.

### Keeper arming — the captain MUST be launched with a stable `--session-id`

> **LOAD-BEARING — restart continuity.** The captain is a `claude --remote-control`
> session, exactly like a crew. The session-keeper's in-process wind-down cycle
> (handoff → `/clear` → `/session-resume`) can only rebind to the SAME conversation
> if the session was launched with a STABLE, caller-minted `--session-id` to
> `--resume`. A captain launched as a bare `claude --remote-control captain` (NO
> `--session-id`, the historical mistake) has no id for the keeper to rebind — so
> the keeper can only WARN it. So launch the captain via the native command below,
> NEVER as a bare `claude --remote-control captain`:
>
> ```bash
> # Launches the captain with a minted --session-id AND arms the keeper at the
> # canonical absolute-token band (warn 200k / act 215k). NO env var, NO script
> # path — `--project` defaults to the current working directory:
> harmonik start captain                 # or the back-compat alias: harmonik captain
> #   ⇒ ONE tmux session `harmonik-<hash>-captain` with TWO windows (hk-z036):
> #      window `agent`  → claude --dangerously-skip-permissions --remote-control captain --session-id <uuid>
> #      window `keeper` → harmonik keeper --agent captain --tmux <session>:agent \
> #                          --warn-abs-tokens 200000 --act-abs-tokens 215000
> #   The keeper targets the `agent` WINDOW (--tmux <session>:agent), so it
> #   injects/gauges the captain pane, never its own keeper window. A captain
> #   restart respawns ONLY the `agent` window; the keeper window survives.
> #   (No separate `hk-keeper-captain` session anymore.)
> #
> # `harmonik start captain` is the native Go launcher (ES2/hk-bcd0): it computes
> # the project hash in-process, writes captain.sentinel + captain.pid so the
> # daemon orphan-sweep skips it, nests the agent+keeper windows, and arms the
> # watcher — everything the RETIRED ~/.claude/captain-tools/captain-launch.sh did.
> # It is idempotent (D7): re-running it on a half-dead captain (keeper window
> # outlived a stopped agent) reaps the stale session and recreates it; a LIVE
> # captain already in the session is REFUSED, never clobbered.
> ```
>
> **Self-heal:** if the captain's agent pane dies, the keeper's `--respawn-cmd`
> seam runs `harmonik captain respawn …` (ES3/hk-z1rj — the native replacement for
> the old generated `captain-respawn.sh`) to respawn ONLY the agent window with
> `--resume <sid>`, preserving the conversation. You do not invoke this by hand.
>
> **Keeper band — canonical flags (M1/hk-039z):** the captain runs on a **1M-token
> window**, where the percent flags `--warn-pct` / `--act-pct` are **INERT** (the
> keeper ignores them and emits a warning if they are passed). The single source of
> truth for the band is the launcher's defaults (`keeper.DefaultWarnAbsTokens` /
> `keeper.DefaultActAbsTokens`, what `harmonik start captain` arms) and the
> `.harmonik/config.yaml` `keeper:` block. If you ever relaunch the keeper by hand, ALWAYS use the absolute
> flags, NEVER the inert pct flags:
> ```bash
> harmonik keeper --agent captain --tmux harmonik-<hash>-captain:agent \
>   --warn-abs-tokens 200000 --act-abs-tokens 215000
> ```
> (The `--tmux <session>:agent` WINDOW target is load-bearing, hk-z036: it points
> the keeper at the captain's `agent` window so it never injects into its own
> `keeper` window. The threshold VALUES are the operator's lowered band — see the on-WARN block
> below. Do not change them here; this fix is the flag SYNTAX only.)

### On-WARN procedure for the captain (LOAD-BEARING)

The keeper injects a **captain-specific** warn text (different from the default
crew advisory): *"[KEEPER WARNING — automated] Proactive context checkpoint — you have ample buffer remaining. Keep working. At a clean checkpoint only: write HANDOFF-captain.md (include the KEEPER nonce), then run: harmonik keeper restart-now --agent captain, keep the turn open, and stop typing. The keeper drives the clear→resume cycle."*

**`restart-now` does not WIDEN the band.** It bypasses only the act-pct idle gate;
all other safety gates (nonce-confirmed handoff, `.managed`, `HoldingDispatch`) are
intact. The operator HARD-NO is on **WIDENING** only — **LOWERING the band to
restart earlier (the current 200k/215k band) is operator-directed and correct**
(`feedback_keeper_band_no_retune` 2026-06-17 UPDATE). Do NOT re-apply the old
"no band-retune" lock to refuse a LOWERING directive.

> **VERIFY the warn text is captain-safe (M11/hk-039z — one-time at boot).** The
> compiled `on_demand_warn_text` MUST inject the captain `restart-now` advisory
> (the block below), NOT the shared crew `/quit` text — a captain that obeys `/quit`
> exits permanently. Confirm with:
> ```bash
> harmonik keeper doctor captain --project "$HARMONIK_PROJECT"
> ```
> If the injected text ends in `/quit` (or the config block's `on_demand_warn_text`
> is the shared fatal advisory), do NOT trust auto-restart — the "NEVER self-quit"
> rule below overrides ANY injected `/quit`; surface the misconfiguration.

> ~~**Old guidance (OBSOLETE — hk-4zy9):** "On a WARN, just keep holding / do nothing
> extra — wait for the keeper's ACT cycle."~~ This caused 40+ idle warn-cycles with
> context re-narration. `restart-now` at the next clean checkpoint is now REQUIRED.

**TERSE-ACK / NO-RE-NARRATION rule (HARD — hk-4zy9, ON-059):** On receiving a WARN,
ack with ONE terse line then keep working. **DO NOT** re-summarize or re-narrate
current state. `/clear` is the reset — no manual hand-trim.

**When you receive a WARN — restart-now is REQUIRED at the next clean checkpoint:**

1. **Terse-ack** the warn in one line (e.g. *"WARN received — triggering restart-now
   at next clean checkpoint"*). **Then immediately keep working.** Do NOT stop mid
   crew-spawn, merge, or submit.
2. **At the next clean idle point** (no `.dispatching` in flight — do not delay past it):
   - Write `HANDOFF-captain.md` with current state, including the `<!-- KEEPER:<nonce> -->` line.
   - Run: `harmonik keeper restart-now --agent captain [--project DIR]`
3. **Keep the turn OPEN and stop typing.** The keeper fires the cycle on its next
   tick (≤5 s): nonce-poll → `/clear` → `/session-resume`.
4. **NEVER exit or terminate your own session on a warn.** Self-terminating exits the captain permanently.

**On resume after a restart-now cycle (LEAN resume — M4/hk-039z):**

A keeper-restart resume is NOT a cold boot. The lower band exists to restart EARLIER
and more often, so the resume MUST be cheap — re-running the full heavy STARTUP every
time would burn the very context the lower band saves. So:

- Re-drain comms (`comms recv --follow --json | head -60`) before forming any plan.
- **Read tier-3/tier-2 (Steps 0a/0b/0c — incl. `direction-log.md`) + run
  `scripts/captain-boot-digest.sh` ONCE.** The digest is the SINGLE verification pass.
  **TRUST the cached tier-2/tier-3 state as INPUT** — mid epics and long-horizon goals
  are stable across a restart and you do NOT re-derive them. The handoff carries INTENT
  only; trust that intent. The direction-log's RETURN-PATH is the sequencing intent
  `/clear` would otherwise destroy — read it before acting (an expired entry LAPSES to
  the standing autonomous posture, never a hold).
- **Full Step-2 re-derivation (the individual reference commands) is only for a COLD
  boot or a digest-flagged discrepancy** — if the digest shows a crew/queue/daemon
  state that conflicts with tier-2, reconcile that specific item; otherwise proceed
  straight to Step 5/6.
- **Re-ground from goal-state (§4.3/FW6):** after the digest, read the durable
  goal-state with one command and align priorities before dispatching:
  ```bash
  cat .harmonik/intent/goal-state.json 2>/dev/null
  ```
  If it exists, scan `objectives` + `operator_directives` and align your current
  priorities with them. Goal-state is written by the goal-keeper agent (FW5) from
  operator comms; if its `last_event_id` predates comms you just drained, the keeper
  will refresh it on its next scheduled run. **No per-turn injection** (§4.5 — locked):
  this is a ONCE-per-restart read, not a persistent prompt fragment. If no
  `goal-state.json` exists yet (FW5 not yet scheduled), skip silently.
- Re-arm watchers (Step 6 below) — keeper arming survives the cycle, but the
  `comms recv --follow` (Watcher 1) and `epic_completed` subscribe (Watcher 2) must
  be re-armed after `/clear`. Captain liveness is ops-monitor-owned — do NOT arm
  any self-polling health timer.

> This reconciles the two pulls the operator flagged: "restart earlier" (lower band)
> vs "re-ground every resume." Cached tier-2/3 is TRUSTED input; the digest is the one
> verification pass; the heavy full re-derive is reserved for cold boot / flagged drift.

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
harmonik comms recv --agent captain --follow --json
```

```bash
# Watcher 2 — epic_completed direct subscribe (structural completion trigger).
#   Re-arm if the daemon restarts. Fold into comms recv --follow if preferred —
#   the daemon also delivers epic_completed over the comms bus.
harmonik subscribe --types epic_completed --json
```

> **Captain liveness is ops-monitor-owned (WE4 / §5).** The every@5m ops-monitor
> (`scripts/ops-monitor-check.sh`) runs the captain-liveness probe and posts an
> `[IMMEDIATE]` comms message on a state change — that message reaches Watcher 1.
> The captain does **NOT** arm any self-polling health timer. Captain-liveness
> monitoring is **external** (ops-monitor); the long-heartbeat liveness fallback is a
> probe owned by the ops-monitor, not a self-scheduling captain timer.

> **CE4 — the deterministic slice is offloaded; only the JUDGMENT slice is captain-owned.**
> The six deterministic checks (daemon-up, paused-queues, crew-freshness, review-gate,
> backlog-readiness, lull) run in the every@5m bash ops-monitor and land in
> `.harmonik/ops-monitor/latest.json`. When a check flips state, the ops-monitor posts
> an `[IMMEDIATE]`/`[DIGEST]` comms (Watcher 1), waking the captain. On each such comms
> event, read the `checks` map once (`jq '.checks' .harmonik/ops-monitor/latest.json`;
> each entry is `{state: ok|flag, detail}`) and take the JUDGMENT action on each
> flagged item — do NOT re-derive the green ones: daemon-up flag ⇒ rebuild+restart the
> daemon; paused-queues flag ⇒ surface+resume; crew-fresh flag ⇒ capture-pane the named
> crew, nudge/reconcile (CONVENIENCE trigger, NOT a guarantee — it misses the silent
> submit-wedge / dead-wake-trigger shapes, which the §4.3 crew process-liveness sweep
> owns); review-gate flag ⇒ a completed run has NO reviewer_verdict
> (review BYPASSED) — surface to operator with the run_ids from `.review_bypass_run_ids`;
> backlog-ready flag ⇒ STAFF: run `kerf next` for the ranked lane, assign a free
> crew/queue slot (the monitor flags WHEN ready work + a free slot coexist; the staffing
> DECISION is yours); lull flag ⇒ if a true lull, deploy+verify own merged work
> (ff-after-push, mind the non-ff race). If `latest.json` is missing or its `ts` is
> >15m stale, the monitor schedule is down — surface to the operator. A self-audit
> question: is any initiative stalled for a reason other than a genuine §8
> surface-and-await case? If so, unblock it. An all-`ok` digest = a healthy fleet:
> do NOT narrate. The judgment-only responsibilities (staffing decision, lull-deploy,
> stalled-initiative unblock, review-bypass escalation) STAY on the captain (leanfleet D6).

**DRIVE every staffed epic to completion.** The captain is the delivery engine: for
each lane it OWNS the epic end-to-end — watch its progress, diagnose what is blocking
it, and decide how to unblock (redeploy the bead, re-task or nudge the crew,
re-sequence the lane, or escalate a genuine §8 blocker). No epic stalls without the
captain actively working its unblock. React to comms events (operator direction, crew
status, ops-monitor flags), `epic_completed` (re-task the crew to its next lane), and
crew error posts (investigate/decide) — **AND, between events, actively pull the
backlog and drive the in-flight epics.** A verified crew self-manages its beads,
wedges, and failures WITHIN its lane — **with TWO carve-outs it CANNOT self-recover
from, because in both the crew is NOT executing:** a **submit-wedge** (a directive was
typed into the crew's pane but the Enter never registered — the text sits unsubmitted,
nothing runs) and a **dead wake-trigger** (the crew armed a queue-completion monitor
and went idle, but its in-flight bead was closed/lost OUT-OF-BAND — e.g. an operator
`br close`, not via the daemon queue — so no `run_completed` ever fires and its wake
never comes). A crew in either state cannot rescue itself; catching it is the
captain's job (the crew process-liveness sweep, §4.3 below). Keeping every lane
STAFFED and every epic MOVING is also the captain's job, not the crews'. So in addition to reacting to events,
run the **≤5-minute REFRESH-AND-STAFF pass** (above): `kerf next` +
`br ready --limit 0`, and if any free crew/queue slot coexists with ready beads,
STAFF it now (establish a lane per Step 5, or comms-re-task a free crew per §4) —
do NOT wait for an event. "React only to events, everything else is the crews' job"
is the passive failure that idles the fleet when lanes drain or block; the captain
drives delivery and pulls the backlog on its own timer.

### Crew process-liveness sweep (§4.3 — the MIDDLE GROUND, catches the silent crew)

> **Principle: once a crew is GOING it self-manages; this sweep exists ONLY to catch
> the ones that have gone silent.** Watcher 1 (the comms bus) is structurally BLIND to
> a silent crew — a submit-wedged or dead-wake-triggered crew sends nothing, so no
> event ever fires and a purely event-driven captain never notices. This is a
> SEPARATE, lightweight captain duty — NOT the captain's-OWN liveness (that stays
> ops-monitor-owned, §5 — do not arm a self-polling health timer for it), and NOT a
> revival of the dropped 12-minute focus-check or the per-minute run-level heartbeat
> the orchestrator-rules correctly forbid. The ops-monitor `crew-fresh` probe is a
> CONVENIENCE trigger, not a guarantee — it misses both silent shapes below.

On a **≤15–20-minute** cadence while crews are staffed, capture each crew's agent pane
(`tmux capture-pane -p -t <session>:1`). A crew is HEALTHY if it shows EITHER an
**active spinner** (Cooked / Crunched / Reticulating / Running / thinking / Pouncing /
… that is advancing) OR an **EMPTY `❯ ` input box** (idle-armed, waiting on a wake).

**FLAG** any crew with **stable non-whitespace text after `❯ ` AND no active spinner**
— no human types into a crew pane, so leftover input means a submit that didn't take.
Use the **two-sample rule** to avoid tripping on a prompt caught mid-submit: re-capture
~15s later and flag ONLY if the same non-empty input box persists with no spinner
across both samples. A FROZEN spinner line (e.g. "Crunched for 45s" that never
increments) over stale input is the same wedge.

**Recovery (submit-wedge or dead wake-trigger — re-drive the pane):**
```bash
tmux send-keys -t <session>:1 C-u                     # clear stale/unsubmitted input
tmux send-keys -t <session>:1 -l "<fresh directive>"  # retype the directive literally (-l)
tmux send-keys -t <session>:1 Enter                   # submit
tmux capture-pane -p -t <session>:1 | tail -5         # confirm: spinner up, input box EMPTY
```
A bare `Enter` on the stale buffer often fails to register — clear-and-retype is what
works. For a **dead wake-trigger**, re-drive with the crew's next directive (or
re-point it at its queue) so it stops waiting on a `run_completed` that will never
fire because its bead was closed out-of-band.

### Idle-triggered realign (§4.4 — replaces the dropped 12m focus-check)

Goal realignment fires on **genuine system idle**, NOT on a fixed clock timer (the
12-minute focus-check was dropped per operator steer 2026-06-14 — a busy captain
already knows its initiatives). An all-`ok` ops-monitor comms event (or absence of
any flagged item in the latest ops-monitor digest) is the natural detection point.

**Idle conditions (ALL must hold before firing):**
1. Ops-monitor all checks `ok` (no flagged items in the latest ops-monitor digest).
2. `br ready --limit 0` empty (no unblocked ready beads) and no undeployed code.
3. No crew actively dispatching (no in-flight runs in the comms feed).

**When idle is confirmed**, run the realign:

```bash
cat .harmonik/intent/goal-state.json 2>/dev/null
# Scan: objectives, antigoals, operator_directives.
# Compare to the current lane table and what kerf next says.
# Drift detected → comms send --from "$HARMONIK_AGENT" --to operator --topic intent \
#                  -- "<1-line: current goal vs stated objectives>"
# No drift → idle silently; do NOT narrate "nothing to do."
```

**Guards (do NOT fire when):**
- **Warm-up grace:** within the first 2 ops-monitor cycles after a cold boot or
  restart — the system may be spinning up naturally.
- **Work truly exhausted:** backlog is genuinely empty with no undeployed code and
  no defined unblocked initiatives — that is a different surface (tell the operator
  the backlog is empty), not a drift signal.
- **No goal-state file:** if `.harmonik/intent/goal-state.json` does not exist yet
  (FW5 not yet scheduled), skip silently.

> **FAILED-monitoring definition (tightened):** a monitoring cycle is FAILED if:
> (a) the daemon is down, OR (b) any crew is comms-silent past 150s, OR
> **(c) `br ready --limit 0` shows ready beads AND a free crew/queue slot exists
> AND the captain did not staff them.** A quiet all-green ops-monitor digest while
> (c) holds is NOT a healthy cycle — it is a MISSED STAFFING FAILURE. The captain's
> job is to maximize throughput; idling with ready work in the feed is the same
> failure mode as watching a zombie crew.
>
> **On detecting condition (c): immediately run `kerf next` + `br ready --limit 0`
> per known lane and staff every ready lane with an idle slot; do NOT wait for the
> next event.** Establish a fresh lane (Step 5) or comms-re-task a free/idle crew
> (§4) for each ready-with-free-slot lane, then nudge its pane (idle-crew wake,
> below) so it actually picks up the work.

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

**G. Never be wishy-washy / never over-defer.** Holding while ready work exists,
punting a decidable question to the operator, or treating a satisfied past request
as a standing blocker are all FAILURES. Decide and act unless the matter is one of
the four genuine §8 cases (locked-reversal, destructive-op, brand-new-initiative,
authorization-scope you don't have). Every other decision is captain-owned: make it,
state the rationale in one line, and move.

**H. A crew idle with ready work in its lane is a DEFECT, not steady-state — GO,
don't investigate-then-idle or wait for a handshake.** When a lane is teed up (its
epic has ready beads) and its substrate is reachable (daemon up, crew online or
spawnable), the captain GOES: it staffs the lane (Step 5) or, for an already-online
crew, comms-re-tasks (§4) AND nudges the crew's pane (idle-crew wake, Step 6) so the
work actually starts. The captain does NOT "verify reachability, then idle waiting
for the crew to ask for work" — a teed-up reachable lane needs ACTION, not a
handshake. **And never sequence the ENTIRE fleet behind ONE lane:** keep the parallel
non-conflicting lanes staffed so one blocked/draining lane can never idle the whole
fleet (the failure that turned the whole fleet idle). If the critical path is
serialized, the other lanes still run — fill them.

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
6. **The daemon is up** (`queue status` ≠ exit 17) **AND no queue is
   `paused-by-failure`** — run Step 2g (`harmonik queue list --json`) to sweep
   all named queues; a `paused-by-failure` queue is NOT dispatching even though
   exit ≠ 17. Surface the paused set and resume before declaring the daemon
   healthy. HEALTH watchers are armed: `comms recv --agent captain --follow` (Watcher 1)
   + `harmonik subscribe --types epic_completed` (Watcher 2). Captain liveness is
   ops-monitor-owned (see Step 6).

Quick one-liner to spot the #1 zombie signature (registered but not online):

```bash
comm -23 \
  <(harmonik crew list --json | jq -r '.name' | sort) \
  <(harmonik comms who --json | jq -r '.agent' | sort)
# Any name printed = registered-but-offline → ZOMBIE/GHOST → reconcile (Step 3).
```

If any of 1–6 fails, the fleet is NOT healthy: reconcile (Step 3) and/or
re-establish the missing lane (Step 5) before settling into the monitor loop.

---

## § Park / wake — fleet idle-down (hk-s8qi M2, codename:sleep-wake)

### Default behaviour (pinned)

**When the captain's `comms recv --follow` Monitor delivers a `park` message
(topic="park", from="daemon") and exits with code 0** — the daemon's
QuiesceArbiter has declared the fleet genuinely drained and is putting the
captain to sleep.

The captain MUST NOT self-exit (R-C4.11). Only its loops quiesce.

### PARK procedure

1. **Stop Watcher 2** (`epic_completed` subscribe). Do not let it re-arm.
2. **Do NOT re-arm `comms recv --follow`.** The Monitor self-exited on the park
   message; leave it stopped.
3. Captain pane remains open but idle. Zero scheduled wakes = zero token burn.
4. **Stop each crew** via `harmonik crew stop <name>`. Crew state is durable in
   beads (via the `--assignee` mirror and the mission file), so `crew start`
   re-hydrates with zero work loss.
5. **Await the pane nudge.** The daemon will inject Enter into your pane when:
   - New work arrives on a queue (WakeCh trigger).
   - An `epic_completed` event fires.
   - A comms message directed at `captain` arrives.
   - The 4-hour max-sleep failsafe fires.

> **CRITICAL: do NOT re-arm ANY loop between the park message and the pane nudge.**

### Detecting the park exit

Same pattern as the crew (see crew-launch SKILL.md § Park/wake):
- Last Monitor output line contains `"topic":"park","from":"daemon"` → park exit.
- Normal disconnect (code 0, no park line) → re-arm the Monitor.

### WAKE procedure

On pane nudge (daemon injects Enter into your pane):

1. **Run the full STARTUP.md boot sequence** (Steps 1–6 above). Treat the wake
   exactly like a fresh session start — re-derive live state, do NOT trust the
   pre-sleep snapshot.
2. Re-arm `comms recv --agent captain --follow --json` (Watcher 1, Step 6).
3. Re-arm `harmonik subscribe --types epic_completed --json` (Watcher 2, Step 6).
4. Re-start each crew (`harmonik crew start <name>`) and staff all ready lanes.

Spec ref: `specs/park-resume-protocol.md` §3.3 and §4.1.
