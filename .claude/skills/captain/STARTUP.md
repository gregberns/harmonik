---
name: captain-startup
description: >
  The captain's boot runbook — seven ordered steps at the top of every session: anchor
  identity and CWD, read the three tier files (the direction log's RETURN-PATH is ground
  truth for sequencing), load the captain's slice and nothing else, ground-truth live
  state with one scripts/captain-boot-digest.sh call, reconcile every crew, write the
  lane table before dispatching, establish and verify each lane on both comms-online and
  pane-truth, then arm the two standing watchers. Also carries the ops-monitor reaction
  map, the keeper WARN and lean-resume paths, the healthy-fleet checklist, and park/wake.
---

<!-- SOURCE OF TRUTH: cmd/harmonik/assets/skills/captain/STARTUP.md (Go //go:embed).
     The copy at .claude/skills/captain/STARTUP.md is GENERATED OUTPUT — `harmonik sync-assets`
     overwrites it from the embed and there is NO reverse sync, so an edit made
     only there silently drifts and is eventually reverted. To change this skill:
     edit the cmd/harmonik/assets/ copy, then mirror it byte-for-byte into
     .claude/skills/ in the SAME commit. The two paths must stay byte-identical. -->

# Captain boot runbook

Run every session, handoff or not. The handoff is one input; live state wins where they
disagree. `SKILL.md` owns the loop after boot — crew mechanics, the mission schema,
completion attribution, and the escalation list. This file owns the ordered sequence.

You act on your own authority: lane execution, dispatch, staffing, zombie reconciliation,
rerouting, and daemon restart are yours to decide and do. Announce intent; do not wait for
a reply. `SKILL.md` §8 owns the four cases that go to the operator, and this file does not
carry a second copy.

---

## Step 0 — Anchor identity and CWD

```bash
echo "agent=$HARMONIK_AGENT  cwd=$(pwd)"     # expect: agent=captain  cwd=$HARMONIK_PROJECT
```

- `$HARMONIK_AGENT` is your comms identity — pass the resolved value on every comms call,
  because a shell `export` does not survive between tool calls. `--from` on **send**,
  `--agent` on **recv / who / log** (`--from` is a sender filter there, and would return
  only your own messages).
- Two live sessions both claiming `captain` collide and freeze the fleet. Assert
  `--from captain` only when `$HARMONIK_AGENT == captain` and no other `captain` is in
  `harmonik comms who`. Resumed under a non-captain lane? You are not the captain.
- Stay in `$HARMONIK_PROJECT` all session. Never `cd` into a worktree — the daemon may
  remove it under you. Use `git -C <repo>` and `harmonik <verb> --project <repo>`, verb
  before flags (an argv starting with a flag matches no verb and exits 2).

---

## Step 0a/0b/0c — Read the tier files

```bash
cat .harmonik/context/project.yaml       # 0a tier-3: phase, forbidden_actions, locked_decisions
cat .harmonik/context/captain-lanes.md   # 0b tier-2: lane table, operator initiatives, parked
cat .harmonik/context/direction-log.md   # 0c tier-2: append-only direction changes
```

Read all three before skills or the handoff; Step 2 verifies their claims rather than
rediscovering them. The direction log is the one to read closely: each entry is one
direction CHANGE with WHAT / WHY / RETURN-PATH / `expires:`, and the **RETURN-PATH is
ground truth for sequencing** — why X was paused for Y and in what order they resume. An
entry past its `expires:` lapses to the standing autonomous posture, never to a hold.

A missing file is not an error: no `project.yaml` ⇒ phase is `operational` and locked
decisions come from `STATUS.md`; no `captain-lanes.md` ⇒ Step 2 derives the lanes.

Update `captain-lanes.md` and append a direction-log entry on any direction change at
session end, before the handoff.

---

## Step 1 — Load your slice, and only your slice

Your brief (`harmonik agent brief`) already carries identity, operating loop, active
triggers, and `HANDOFF-captain.md`. For everything else it carries **paths, not bodies**.
Pull three: this file, `SKILL.md`, and the **`orchestrator-rules` skill** — the standing
dispatch, priority, autonomy, and review rules, and the canonical statement of where
priority comes from, which this file and `SKILL.md` both defer to.

On demand, not at boot — load each in full before its first use: `beads-cli` (the `br`
read surface and the write discipline that keeps you off terminal transitions) ·
`agent-comms` (bus CLI, `event_id` dedupe) · `harmonik-dispatch` (`queue submit` /
`append` / `subscribe`, the Monitor pattern) · `keeper` (the context bands and the
`hold`/`release` co-working override) · `major-issue-fanout` (a blocker that has survived
two fix attempts).

**Do not boot-read** `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md`, `PRINCIPLES.md`, or the
programme charter. Phase, locked decisions, and lanes come from the tier files plus the
Step 2 digest. The backlog does NOT: the digest carries no bead listing, and where work
comes from is your direction's call. `PRINCIPLES.md` is the standard for code you write, and you do
not write code while a crew can take it. The programme is "delete the test mass, then
decompose the core"; if you need its sequence or its definition of done, read §2 and §6
of `plans/2026-07-27-delete-and-rewrite/CHARTER.md`, not the whole charter.

---

## Step 2 — Ground-truth the live state

One command reports fleet condition — daemon status, agents online, crew registry, tmux
fleet, paused queues, recent comms, open epics. It says what is RUNNING. It does not say
what to work on, and it carries no bead listing and no kerf map on purpose.

```bash
scripts/captain-boot-digest.sh        # in-repo, portable; --project DIR optional
```

Re-run an individual command only when a digest section is empty or ambiguous; the script
is the reference for what those commands are. Two things it cannot tell you:

- **Exit 17 from any daemon RPC means the daemon is DOWN** → Step 2.1. `queue status`
  printing "(no queue active)" is up-but-idle, not down.
- **A paused queue is not a healthy queue.** `harmonik queue list --json` sweeps every
  named queue; `up` is not `dispatching`. Read the `status` field. `paused-by-drain` is an
  operator drain and `harmonik queue resume --queue <name>` clears it. `paused-by-failure`
  is a queue a failed bead stopped, and only `harmonik queue recover --queue <name>`
  restarts it. Resume aimed at a failure-parked queue is refused and the refusal names the
  right verb, so you cannot get this wrong silently. Mechanism, and what recovery refuses:
  the **harmonik-dispatch** skill, § Restart a queue that stopped.

Build the live-state table from the digest, one row per registered crew — in `crew list`?
in `comms who`? tmux window alive? epic (`br show <epic> --format json` → assignee)?
dispatched a bead? Those first three columns intersect to classify it in Step 3.

### Step 2.1 — Daemon down

The supervisor (`hk-daemon-supervise` tmux session) usually revives it, and restart
backoff can delay the socket bind by a minute or more, so "(no socket)" right after a
deploy is expected. Let the supervisor win — a hand-launched daemon races the pidfile.
Supervisor confirmed dead (no session, backoff elapsed, still no socket) ⇒ run
`harmonik supervise start` yourself; that is routine self-authorized work, and
`docs/daemon-redeploy.md` has the swap procedure. `crew list`, `comms who`, and
`comms log` are local reads and keep working; do not spawn or mail until it is back.

---

## Step 3 — Reconcile crews

| Classification | Signature | Action |
|---|---|---|
| **HEALTHY** | in `crew list` ∧ in `comms who` ∧ tmux window alive ∧ has an epic ∧ recently dispatched | Keep. A real working lane. |
| **ZOMBIE** | in `crew list` ∧ tmux window alive **∧ NOT in `comms who`** past the 120s TTL | Stale or wedged. `harmonik crew stop <name>`, re-establish in Step 5. |
| **IDLE** | in `comms who` ∧ in `crew list` ∧ has an epic **but dispatched nothing** | Not a zombie. Re-task over comms, do NOT `crew stop`. |
| **GHOST RECORD** | in `crew list` ∧ no tmux window ∧ NOT in `comms who` | Orphan record. `harmonik crew stop <name>`. |
| **STRAY WORKTREE WINDOW** | tmux window named `.../worktrees/<uuid>`, not `hk-crew-<name>` | A daemon bead worktree. Leave it — and it is not evidence a crew is working. |

`harmonik crew stop <name>` removes the registry record, the pane, and the keeper marker.
`--pause-queue` only if the operator wants that queue halted; the default leaves it
draining.

Before any stop or start, check the bus for an operator teardown or relaunch already in
flight (`harmonik comms log --since 15m --topic status --json`, `comms who`). If one is,
announce intent, let it finish, re-check, proceed. No reply required.

**A name or queue collision fails fast and loud** — `crew start` into a name or queue a
live crew holds returns non-zero. Report the exact error; do not rename around it and do
not retry. The collision means the lane is already staffed, and relaunching under a free
name puts two crews on one epic. Find the holder (`crew list`, `comms who`,
`tmux capture-pane`): live ⇒ the lane is covered; confirmed dead ⇒ a zombie you stop and
re-establish under the **same** name.

---

## Step 4 — Write the plan before you dispatch

Size the plan to the work. Four lanes needs the table below; an incident — daemon down,
queue paused, fleet wedged — needs one line naming the fix, and then you fix it. Unwedge
first, plan second. The smell worth stopping for is dispatching work whose shape you have
not decided.

**Where work comes from is your direction's call, not this runbook's.** The operator's and
the admiral's named initiatives, the active plan's order, the dated directives in
`captain-lanes.md`, and the direction-log RETURN-PATH are the feed. Which ledger query
surfaces the rest, and in what order, changes — so it is stated in your direction and it is
not fixed here. Organizing that feed into lanes and staffing it is autonomous, as is
resuming a parked or drained lane any durable doc or ledger row records.

| lane (crew) | epic id | epic title (plain English) | ordered ready beads | keystone-gated? |
|---|---|---|---|---|

- **One crew per lane** — decompose into non-conflicting epics so two crews never touch
  the same files or package. Leave `harness` unset: the daemon today refuses any crew
  harness but `claude` (`internal/crewrun` `BuildCrewLaunchSpec` fails `crew start` with
  "not yet supported"), so you cannot staff a Codex crew orchestrator until that
  substrate lands.
- **Mark keystone-gated beads BLOCKED.** A bead depending on an open epic or an in-flight
  keystone insta-fails at dispatch (`group_failure`, no `run_started`). Only safe-now
  beads get dispatched.
- **Fill every non-conflicting lane with ready beads.** Zero ready beads and no in-flight
  run ⇒ **PARKED**, which `SKILL.md` §A explains is a fact about the backlog and not a
  hold — re-staff the moment ready work and a free slot coexist.

Surface the plan (status line and `comms send --to operator --topic status`), then go to
Step 5. Do not block on a reply for work a durable doc or ledger row already carries.

---

## Step 5 — Establish and verify every lane

All five sub-steps, for every lane. A lane is not done until it passes 5d — `crew start`
exiting 0 is not verification.

**5·0** Confirm the lane has ready work before you staff it — scoped to its epic or its
`codename:` label. None, and no in-flight run ⇒ **PARKED**, skip 5a–5d. This is a check on
one lane, not a ranking of the backlog: **where work comes from is your direction's call**,
and the `beads-cli` skill owns the listing surface and the flag that stops it truncating.

**5a** Write the mission handoff to `.harmonik/crew/missions/<crew>.md` FIRST. It is
tracked in git — commit it. Schema: `SKILL.md` §3. Contract: `specs/crew-handoff-schema.md`.

**5b** Mirror the assignment so run events attribute later (the crew also does this on
boot; setting it now makes attribution work immediately):

```bash
br update <epic_id> --assignee <crew>    # metadata-only; NOT a terminal transition
```

**5c** Start the crew — one call per lane, distinct name and distinct queue:

```bash
harmonik crew start <crew> --queue <crew>-q --mission .harmonik/crew/missions/<crew>.md
# 0  → session_id printed (informational; do NOT persist it in the handoff)
# 17 → daemon down → Step 2.1
# other → collision (never rename, never retry — Step 3) or launch failure (post the
#         exact error, then diagnose and retry on your own authority).
```

A live crew that just needs a new epic is a comms re-task, not a new `crew start` —
`SKILL.md` §4.

**5d** Verify on both axes:

```bash
# (a) comms-online — the crew ran its boot loop and called `comms join`. Capture first:
#     piping `comms who --json` into grep can lose `who` to SIGPIPE on a big roster.
who="$(harmonik comms who --json)"
grep -q '"agent":"<crew>"' <<<"$who" && echo ONLINE || echo "NOT ONLINE"

# (b) pane-truth — look for comms join, "crew <crew> online owning <epic>", a queue submit.
tmux capture-pane -p -t harmonik-<hash>-crew-<crew>:hk-crew-<crew> | tail -25
harmonik comms log --from <crew> --topic status --since 10m --json
harmonik queue status --json
```

A lane passes only on (a) **and** (b). Recovery is routine — act, do not wait. (a)
failing past ~120s ⇒ re-drive the crew and post a status; only declaring the crew
*failed* and killing its work is an escalation. (a) passing while (b) shows the pane
wedged at a prompt ⇒ clear-and-retype it (`SKILL.md` §6) and re-verify.

Boot is complete only when every planned lane passes 5d or is explicitly parked.

---

## Step 6 — Arm the watchers, then monitor

```bash
harmonik comms recv --agent captain --follow --json   # operator direction + crew status/errors
harmonik subscribe --types epic_completed --json      # structural completion trigger
```

Exactly these two, deduped on `event_id`. Re-arm the first on timeout and the second if
the daemon restarts. No run-level subscribe: `run_stale`, `heartbeat`, and every
`run_completed` belong to the crew that owns the run, and a heartbeat stream re-invokes
you every minute and burns the context this role exists to protect. A short-lived
diagnostic subscribe during an incident is fine — kill it when the incident closes.

**The loop is active, not passive.** Purely event-driven is the idle-fleet failure: when
lanes drain or block, no event fires, so nothing re-staffs them. Block on the feeds, and
between events — at least every 5 minutes — check for ready work and staff any free slot
with work beside it. Which listing answers that, and in what order, comes from your
direction, not from this runbook. This pull runs only while you are awake; a
dormant captain is woken solely by push — ops-monitor `[IMMEDIATE]`, watch escalations,
operator messages. Your own liveness is the ops-monitor's job; do not self-poll.

### Reacting to the ops-monitor

Six checks run every 5 minutes into `.harmonik/ops-monitor/latest.json`; a state flip
posts a comms message that reaches the first watcher. Read the map once
(`jq '.checks' .harmonik/ops-monitor/latest.json`) and act on flagged items only:

- **daemon-up** → rebuild and restart the daemon.
- **paused-queues** → surface, then restart with `harmonik queue recover --queue <name>`.
  Green here means nothing: the check fires only when the owning crew is online, and it
  guesses the crew name by stripping a trailing `-q` from the queue name, so it misses the
  common cases. Sweep `queue list --json` yourself.
- **crew-fresh** → capture-pane the named crew, nudge or reconcile. It misses both silent
  wedge shapes (`SKILL.md` §6), so it is a trigger, not a guarantee.
- **review-gate** → a completed run has no reviewer verdict, so review was bypassed.
  Surface the run ids from `.review_bypass_run_ids`.
- **backlog-ready** → staff a free slot. The monitor flags that ready work and a free slot
  coexist; the staffing decision is yours.
- **lull** → deploy and verify your own merged work (ff-after-push; mind the non-ff race).

A missing `latest.json`, or a `ts` over 15 minutes stale, means the monitor schedule is
down — surface it. An all-`ok` digest is a healthy fleet: do not narrate it.

### Crew process-liveness sweep

A verified crew self-manages, with two exceptions it cannot recover from — in both it is
not executing and sends nothing, so the comms bus is structurally blind to it:

- a **submit-wedge**: a directive was typed into its pane but the Enter never registered,
  so the text sits unsubmitted and nothing runs;
- a **dead wake-trigger**: it armed a queue-completion monitor and went idle, but its
  in-flight bead was closed out-of-band (an operator `br close`, not through the daemon
  queue), so no `run_completed` ever fires and its wake never comes.

Every 15–20 minutes while crews are staffed, capture each crew's agent pane
(`tmux capture-pane -p -t <session>:1`). Healthy is an advancing spinner or an empty `❯ `
input box (idle-armed, waiting on a wake). Flag any crew with stable non-whitespace text
after `❯ ` and no active spinner — no human types into a crew pane, so leftover input
means a submit that did not take. Re-capture ~15s later and flag only if it persists
across both samples; a frozen spinner over stale input is the same wedge.

```bash
tmux send-keys -t <session>:1 C-u                     # clear the stale input
tmux send-keys -t <session>:1 -l "<fresh directive>"  # retype it literally (-l)
tmux send-keys -t <session>:1 Enter                   # submit
tmux capture-pane -p -t <session>:1 | tail -5         # confirm: spinner up, input box EMPTY
```

A bare `Enter` on the stale buffer often fails to register; clear-and-retype works. For a
dead wake-trigger, re-drive with the crew's next directive or re-point it at its queue.

### Idle-triggered realign

Fires on genuine system idle, not on a clock — no flagged checks, no ready work anywhere,
no undeployed code, and no crew dispatching. Then read
`.harmonik/intent/goal-state.json` once (skip silently if absent) and compare its
`objectives` / `antigoals` / `operator_directives` to the lane table. Drift ⇒ one
`--topic intent` message to the operator. No drift ⇒ idle silently; do not narrate
"nothing to do". Do not fire in the first couple of cycles after a boot, and do not fire
on a genuinely exhausted backlog — that is a different message.

### Keeper

You are keeper-managed, not a keeper operator. `harmonik start captain` mints the stable
`--session-id`, writes the sentinel and pidfile that keep the daemon's orphan sweep off
you, and arms the keeper — you never arm it by hand, and a bare `claude --remote-control
captain` cannot be cycled at all because there is no session id to rebind. Re-running
`start captain` is idempotent: it reaps a half-dead session and refuses to clobber a live
one. A crew's keeper restart is a non-event; do not re-`crew start` one that cycled. The
`keeper` skill has the bands, the config block, FORCE-ACT, doctor, and await-ack.

**On WARN:** ack in one line and keep working. Do not stop mid crew-spawn, merge, or
submit, and do not re-narrate state — that re-summarizing once burned a captain through
forty idle warn-cycles. At the next clean idle point (no `.dispatching` in flight), write
`HANDOFF-captain.md` including the `<!-- KEEPER:<nonce> -->` line, run `harmonik keeper
restart-now --agent captain`, keep the turn open, and stop typing. **Never `/quit` or
self-terminate** — that exits the captain permanently, and this overrides any injected
`/quit` advisory.

### On resume after a restart-now cycle

A lean resume, not a cold boot. Re-drain comms FIRST with `harmonik comms recv --json`, which drains the
backlog and exits — not `--follow | head`, because `--follow` never ends and the SIGPIPE
when `head` exits reports as a failed drain. Then re-read Steps 0a/0b/0c and run the
digest once, trusting the cached tier state as input: mid epics and long-horizon goals
survive a restart, the handoff carries intent, and the direction-log RETURN-PATH carries
the sequencing `/clear` would otherwise destroy. Skip the full Step-2 re-derive unless the
digest flags a discrepancy — reconcile that item and go straight to Steps 5–6. Re-arm both
watchers; keeper arming survives the cycle, the watchers do not.

---

## Definition of a healthy fleet

1. Every planned lane has a crew in **both** `crew list` and `comms who`.
2. Each crew owns a distinct epic and a distinct named queue.
3. Each crew's epic is mirrored — `br show <epic> --format json` → `.assignee` is the
   owning crew, so run-event attribution needs no round-trip.
4. Each crew shows pane-truth: a recent `--topic status` post and a dispatched bead, or a
   clean "idling — no ready beads" drain status, which is also healthy.
5. No zombie or ghost records in `crew list`.
6. Daemon up (`queue status` ≠ exit 17), no queue paused, both watchers armed.

```bash
# The registered-but-offline signature, in one line. Any name printed = ZOMBIE/GHOST → Step 3.
comm -23 <(harmonik crew list --json | jq -r '.name' | sort) \
         <(harmonik comms who --json | jq -r '.agent' | sort)
```

Three confusions worth naming, because each has cost a fleet real time: a daemon worktree
window is not a crew working; a stale `comms who` entry is not a live crew; and a bead
running is not a reason to park on it while other lanes sit idle.

---

## Park / wake — fleet idle-down

A `park` message (topic `park`, from `daemon`) on the comms watcher, which then exits 0,
means the daemon's QuiesceArbiter has declared the fleet drained. The captain does not
self-exit; only its loops quiesce. Tell it from a normal disconnect by the last Monitor
output line containing `"topic":"park","from":"daemon"` — a code-0 exit with no park line
just means re-arm.

**Park:** stop the `epic_completed` subscribe and do not re-arm either watcher;
`harmonik crew stop <name>` each crew (state is durable in beads through the `--assignee`
mirror and the mission file, so `crew start` re-hydrates with no work lost); then leave
the pane open and idle and re-arm nothing until the daemon nudges it — on new queue work,
an `epic_completed`, a comms message directed at `captain`, or the 4-hour max-sleep
failsafe.

**Wake:** run the full boot sequence, Steps 1–6, as a fresh start. Do not trust the
pre-sleep snapshot.

Spec: `specs/park-resume-protocol.md` §3.3 and §4.1.
