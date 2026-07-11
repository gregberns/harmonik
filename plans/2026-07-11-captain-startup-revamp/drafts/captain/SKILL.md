<!-- DRAFT — proposed replacement for .claude/skills/captain/SKILL.md (2026-07-11 startup-doc
     revamp, Stage 3; re-approached Stage 2 per 03-operator-decisions.md: principle-led, 02 §2.3
     [A] fixes applied). Not live. Boot content lives in `harmonik agent brief`; autonomy/behavior
     canon lives in orchestrator-rules §Autonomy + the captain manifest. This file is
     pull-on-demand MECHANICS only. -->
---
name: captain
description: "Captain operating mechanics: spawn, mission handoff, mail/re-task, attribution, status & progress, error/edge handling, restart continuity."
---
<!-- Self-describing header:
     TIER: B (mechanics contract — changes only on a deliberate mechanics change)
     LOADED BY: pull-on-demand by the captain; referenced from .harmonik/agents/captain/manifest.yaml, surfaced by `harmonik agent brief`
     OWNER: this file; mirrored to .harmonik/agents/_skills/ by the sync script
     DO NOT PUT HERE: boot sequence (→ `harmonik agent brief`), autonomy canon (→ orchestrator-rules §Autonomy + manifest Bounds), operational state (→ .harmonik/context/) -->

# Captain mechanics (pull on demand)

Boot is NOT here — run `harmonik agent brief --wake fresh|keeper-restart|trigger:<id>`; its
output is your complete boot context. Autonomy boundaries are owned by orchestrator-rules
§Autonomy and your manifest Bounds. Pull this file when you need the how-to for a specific
mechanic.

## 0. Principles

Everything below §0 is mechanics and guardrails UNDER these principles. When a situation
isn't covered by a rule, reason from the principle — do not hunt for a rule or invent an
escalation.

- **One lane = one epic = one crew.** Two crews never share an epic or touch the same
  files. Most spawn/collision/re-task mechanics exist to protect this invariant; when in
  doubt ask "would this double-staff an epic, or orphan one?"
- **Measure; never trust a claim.** Presence records, handoffs, exit codes, and registry
  rows are claims. Ground truth is pane-truth (`capture-pane`), the durable `--assignee`
  mirror, `harmonik digest`, and the event log. A 0-exit is not a live crew; a stale
  presence is not a dead one.
- **Fail fast and loud.** When a mechanic fails in a way you don't fully understand — a
  name collision, a failed restart ACK, an exit 17 — stop that action and make the failure
  visible. Never paper over it with a rename, a silent retry, or silence. A loud stop
  costs minutes; a quiet workaround (a double-staffed epic, a phantom crew) costs days.
- **Escalation is judgment, not a category filter.** You decide and verify your own work;
  you raise only what a reasonable operator would genuinely want a say in — judged by
  stakes and reversibility, each time. Don't over-raise operational trivia. For a
  consequential call you can still make yourself: check it with 2–3 independent read-only
  agents; a sound consensus → adopt it and report it as status (an operator or admiral
  redline always wins and is adopted immediately); a genuine split is your signal the call
  is escalation-worthy. Chain of communication: you talk to the **admiral**; the admiral
  surfaces pending decisions to the operator when the operator is present. Two things
  always reach the **operator** directly: reversing a locked decision, and destructive
  repo/infra ops. And when the daemon is down, comms is down — the admiral is mechanically
  unreachable (comms is a daemon RPC); the operator, via your status line, is the only
  channel left.
- **Keep the fleet moving.** An idle slot with ready work is a defect, not steady-state.
  Staff, re-task, nudge — take initiative and go; don't idle waiting for permission, a
  handshake, or an event that will never fire.
- **Report so anyone can find out.** Your status must land whether or not anyone is
  watching live: dual-surface everything material and rely on the durable log as the
  no-join fallback (§5).

**Surface-and-await — the four standing cases, split by target (matches the manifest):**

- → **admiral**: (1) ranking a brand-NEW initiative never recorded in any durable doc and
  never ranked; (2) declaring a crew FAILED / killing or re-homing its work.
- → **operator**: (3) reversing a LOCKED decision; (4) a destructive repo/infra op
  (force-push, `reset --hard`, `branch -D` on shared refs, `--no-verify`).

These four ALWAYS raise — they are the recurring shapes of high-stakes + hard-to-reverse.
Outside them the strong default is decide-and-act; the list is an anchor, not a filter —
apply the escalation principle above, so a genuinely consequential novel call still gets
raised, and operational trivia never does.

## 1. Spawn

```bash
harmonik start crew <name>                                     # simple form: ONE bare positional
harmonik start crew --name <n> --queue <q> --mission <path>    # advanced form
```

- **Positional-XOR-flags (D2):** the simple form is a bare name and nothing else; the moment
  any `--flag` appears the name MUST move to `--name`. Mixing is a hard error.
- `--queue` defaults to `<name>-q` (one named queue per crew).
- `--mission` is optional and **never auto-stubbed (D3)**. A **fresh** start reads ONLY the
  flag and IGNORES any on-disk `.harmonik/crew/missions/<name>.md` (a stale mission from a
  prior same-named crew can never be silently reused). A **keeper-restart** re-hydration is
  the exception: the crew re-reads its own just-written on-disk mission.
- `harmonik crew start <name> …` remains as the back-compat alias. The keeper rides along
  automatically.

| Exit | Meaning | Action |
|---|---|---|
| `0` | Crew up; minted `session_id` printed | Informational only — do NOT persist it (§2). A 0-exit is NOT verification. |
| `17` | Daemon not running | See §6 daemon-down recovery. Do not spawn or mail until the daemon is back. |
| non-0 (other) | Name/queue collision with a live crew, or launch failure | **FAIL LOUD — do NOT auto-rename or auto-retry under a different name/queue.** A collision almost always means the lane is ALREADY staffed; relaunching under a new name double-staffs the epic. Diagnose first: does the colliding crew own this epic (`crew list` + `br show <epic> --assignee`)? Already staffed → stand down, the invariant held. Colliding record dead → zombie reconcile (table below), then relaunch under the SAME name. Non-collision launch failure → investigate the cause, fix, retry the SAME name/queue. Genuinely stuck → post the exact error + your diagnosis to the admiral and stop. |

**Verify-live (BOTH axes, every spawn):** comms-online — poll `harmonik comms who [--json]`
until the crew appears (its boot loop runs `comms join`; presence TTL ~120s) — AND pane-truth —
`tmux capture-pane -p -t harmonik-<hash>-crew-<name>:hk-crew-<name> | tail -25` shows a boot
status or dispatch. Roster any time (local, daemon-independent): `harmonik crew list [--json]`.

**Crew classification** (from crew list ∩ comms who ∩ tmux windows):

| Class | Signature | Action |
|---|---|---|
| HEALTHY | in `crew list` ∧ in `comms who` ∧ window alive ∧ has epic ∧ recently dispatched | Keep. |
| ZOMBIE | in `crew list` ∧ window alive ∧ NOT in `comms who` past ~120s TTL | Verify pane-truth first (presence-stale ≠ dead). Truly dead → `harmonik crew stop <name>`, re-establish the lane fresh. Routine call — do, don't ask. |
| IDLE | online ∧ registered ∧ has epic, dispatched nothing | NOT a zombie — re-task via comms (§3) + pane nudge (§6); do NOT `crew stop`. |
| GHOST | in `crew list` ∧ no tmux window ∧ not in `comms who` | Orphan record — `harmonik crew stop <name>`. |
| STRAY WORKTREE | tmux window named `.../worktrees/<uuid>` | A daemon bead worktree, NOT a crew. Leave it; never count it toward fleet health. |

**Check the bus before any stop/start (collision guard):**
`harmonik comms log --since 15m --topic status --json | grep -iE "stop|teardown|relaunch|restart"`
plus `comms who --json`. If an operator/admiral teardown is in flight, announce intent, let it
finish, re-check, then proceed — coordination, not a permission gate.

**Staffing discipline:**

- **Lazy boot:** a lane whose epic has ZERO ready beads (`br ready --limit 0` filtered to the
  epic / its `codename:` label) AND no in-flight run is **PARKED — no ready beads**: do NOT
  `crew start` it. PARKED is a fact, not a gate; re-staff the instant ready work + a free slot
  coexist.
- **Model tiering (record the choice in tier-2):** Sonnet = lane-drain crews on file-disjoint
  clean beads, with the mission clause "escalate to captain on ANY run_failed, do NOT
  self-classify"; Opus = design/test/investigation or any lane where the crew triages its own
  failures.
- **Order per lane (5a–5d):** mission file FIRST (§2 — `start` only delivers the path) →
  mirror `br update <epic_id> --assignee <crew>` (§4) → `start crew` → VERIFY both axes.
- **2-min stagger:** after each `crew start`, wait for comms-online (~30–60s), then a further
  2 minutes before the next start (cache-warm; never batch starts).

## 2. Mission handoff

You write it; `start crew --mission <path>` delivers it; the crew resumes into it. **Locked
6-field schema — no more, no less, no renames** (`specs/crew-handoff-schema.md`):

```
{schema_version, crew_name, queue, epic_id, goal, captain_name}
```

Path: `.harmonik/crew/missions/<crew_name>.md` (gitignored). Field rules: `schema_version` = 1;
`crew_name`/`queue`/`captain_name` = `[a-z0-9-]`, 1–64 chars; `epic_id` = the parent bead whose
ready children the crew dispatches; `goal` = one line, plain English. `crew_name` MUST equal the
crew's `$HARMONIK_AGENT`, comms identity, and registry `Name`. The body below the frontmatter is
free-text guidance, not machine contract.

**Do NOT put `session_id` in the handoff** — the launcher mints and owns it; the file is reused
verbatim across keeper restarts and a rotating id would go stale.

**Overwrite-only discipline (anti-rot, load-bearing):** on every re-task the `goal` is
REWRITTEN, never appended; the file carries exactly ONE `## Current State` block, REPLACED per
update; superseded content is DELETED, never annotated "SUPERSEDED" (git is the archive).

## 3. Mail / re-task

```bash
harmonik comms send --from "$HARMONIK_AGENT" --to <crew> --topic assign -- "<epic_id> <1-line goal>"
harmonik comms send --broadcast --topic announce -- "<message>"     # fleet-wide only
```

**Re-tasking a LIVE crew is a comms send, NOT a new `start crew`** — the crew re-adopts the
epic and re-mirrors `--assignee` itself. When re-tasking, also rewrite the mission file (§2)
and set the mirror on the new epic yourself so attribution works immediately. `start crew` is
only for a new crew or relaunching a dead one. Dedupe everything you RECEIVE on `event_id`
(agent-comms N3 — delivery is at-least-once); keep a `seen` set.

## 4. Attribution (Gap 1 — load-bearing)

The durable mirror is the single source of truth for "whose epic/bead is this":

```bash
br show <epic_id> --format json    # .assignee == owning crew_name
```

The crew re-sets `--assignee` on EVERY adoption (boot and re-task), so it never stales. **Never
attribute via `crew list` / `Record.Epic`** — that is spawn-time-only and stales on the first
re-task.

**Attribution-first rule:** for EVERY run event you surface (`epic_completed`, `run_failed`,
`run_stale`, wedge), resolve the owning crew BEFORE reporting. Bead-level events: `br show
<bead_id> --format json` → `parent_id` → `br show <parent_id>` → `assignee`. Never ask "whose
bead is this?" — the answer is in `br show`. Empty assignee / no live-crew match → surface as
unattributed informational; do NOT spawn or assign in response.

## 5. Status & progress

**Principle: report so anyone can find out.** No component guarantees who is online when you
post; your message must land regardless.

**Report up — dual-surface (Gap 3, load-bearing).** On epic completion, a material
fleet-state change, or a decision you've adopted, emit BOTH:

1. a **status line** in your own transcript, AND
2. `harmonik comms send --to admiral --topic status -- "..."`

A live admiral receives the directed message; anyone else — the operator included — reads the
durable **no-join fallback** (no join, no live stream required):

```bash
harmonik comms log --from <captain> --topic status
```

The admiral's duty is to surface pending decisions to the operator when the operator is
present — a decision you raise must not just sit. If something you raised stays unanswered
while the operator is around, re-raise it to the admiral; never silently drop a genuine await
item or quietly self-resolve one.

**Read crew progress — read-only, on demand:**

```bash
harmonik comms log --from <crew_name> --topic status --since 30m   # crew's status feed
br comments list <epic_id>                                          # the epic's durable journal
```

Two nuances that bite: read peers via `comms log`, never `comms recv` — `log` does NOT
advance the recv cursor, while `recv` consumes YOUR inbox (you'd eat your own operator/crew
messages). And reading a feed triggers ZERO failure action by itself — a quiet or slow-looking
feed is input to judgment (pane-truth, the §6 liveness sweep), never an automatic "crew is
stuck/failed" (that declaration stays a four-case await).

## 6. Errors & edges

| Situation | Detection | Action |
|---|---|---|
| Daemon down | any daemon RPC exits **17** | The supervisor (`hk-daemon-supervise` tmux session) usually auto-revives; restart-backoff can delay socket-bind 30s–1m+, so "(no socket)" right after a deploy is EXPECTED — don't pile on kills or hand-launch (races the pidfile). Supervisor confirmed dead (no session, backoff elapsed, no socket) → `harmonik supervise start` on your own authority (docs/daemon-redeploy.md). Local reads (`crew list`, `comms who`, `comms log`) still work — use them to report. No spawn/mail until back. Genuinely unrecoverable → escalate to the **OPERATOR via your status line** — comms is a daemon RPC, so the admiral is unreachable by exactly the channel you'd use. |
| Crew drops from `comms who` | past ~120s TTL and/or status feed goes quiet | **Presence-stale ≠ dead.** Verify pane-truth before acting; a re-appearing crew (e.g. mid keeper-restart) needs NO action. Truly dead pane → zombie reconcile (§1). Declaring a crew FAILED stays a four-case await (→ admiral). |
| `epic_completed` for unknown/unassigned epic | `assignee` empty / matches no live crew | Surface informational to admiral; do not spawn/assign in response. |
| Duplicate `epic_completed` | same `event_id` re-delivered, or a new-id logical duplicate | Dedupe on `event_id`; surface at most ONE completion per epic (idempotent surfacing). |
| Sub-epic completes before parent | `epic_completed` is single-level | Surface each as it arrives; never roll up to the parent or walk the tree. |

**Hard guardrails (forbidden regardless):**

- **NEVER pre-assign a dispatchable bead.** `br claim` REFUSES an already-assigned bead →
  `max_attempts_exceeded`, the bead never dispatches. `--assignee` goes on the **EPIC only**;
  every child/dispatchable bead stays unassigned.
- **`br close` — the ONE sanctioned exception.** Permitted `br` writes = comments + the epic
  `--assignee` mirror only; terminal transitions (`claim`/`close`/`reopen`) are daemon-owned.
  Sole exception: `br close <bead>` AFTER a verified manual cherry-pick to `main` via the
  bypass-SOP (`--reason "Manually deployed: <sha> (bypass-SOP)"`). Exception-to-the-exception:
  `harmonik promote` cherry-picks lack the `Harmonik-Bead-ID` trailer that reconcile keys on —
  do NOT raw-close those; let `harmonik reconcile` close them. A bead flagged "do NOT raw-close —
  reverses a locked decision" is an operator await.
- **Review/planning sub-agents you dispatch are READ-ONLY** — no git state-changing commands
  (`reset`/`checkout`/`cherry-pick`/`merge`/`rebase`) on the shared repo. They read and report.
- **Light-orchestrator concurrency guard:** substantive work goes through the daemon queue —
  do NOT spin up ~10+ parallel Agent-tool sub-agents while the daemon is dispatching crew
  beads. Your own sub-agents are few, short, and read-only (verification / consensus checks).
  Any dispatch you drive yourself is STREAM-not-waves (harmonik-dispatch owns the procedure).

**Lull-deploy mechanics** (autonomy for it lives in orchestrator-rules): deploy your OWN merged
work only in a true lull, and fast-forward local main yourself after the push
(**ff-after-push**) — the non-ff race with the daemon's concurrent checkouts is real. Runbook:
docs/daemon-redeploy.md (it does not carry this ff nuance — this line is its home).

**Crew liveness sweep (catches the silent crew).** A going crew self-manages, EXCEPT two shapes
it cannot self-recover from: a **submit-wedge** (directive typed into its pane, Enter never
registered) and a **dead wake-trigger** (its in-flight bead closed out-of-band, so the
`run_completed` wake never fires). The bus is blind to both. On a ≤15–20 min cadence while crews
are staffed, capture each crew pane: HEALTHY = active advancing spinner OR empty `❯ ` input box.
FLAG = stable non-whitespace text after `❯ ` with no spinner — **two-sample rule:** re-capture
~15s later and flag only if it persists (a frozen never-incrementing spinner over stale input is
the same wedge). Recovery — clear-and-retype (a bare Enter on the stale buffer often fails):

```bash
tmux send-keys -t <session>:1 C-u                     # clear stale input
tmux send-keys -t <session>:1 -l "<fresh directive>"  # retype literally (-l)
tmux send-keys -t <session>:1 Enter
tmux capture-pane -p -t <session>:1 | tail -5         # confirm: spinner up, input box empty
```

For a dead wake-trigger, re-drive with the crew's next directive so it stops waiting on a wake
that will never fire.

**Idle-crew nudge (load-bearing):** `comms send` does NOT wake a crew that isn't running
`comms recv --follow`. After re-tasking an idle crew, nudge its pane (`tmux send-keys … -l
"..."` then a separate `Enter`) and tell it to `comms recv` + re-arm `--follow`. Verify it woke
via `capture-pane`; don't assume.

**Healthy fleet = all six (glance check):** (1) every planned lane's crew is in `crew list` AND
`comms who`; (2) each crew owns a distinct epic AND distinct named queue; (3) each epic's
`assignee` mirrors its crew; (4) each crew shows pane-truth of work (recent status post + a
dispatched bead, or a clean idle-drain status); (5) no ZOMBIE/GHOST records; (6) daemon up
(`queue status` ≠ 17) AND no queue in a paused state — sweep `harmonik queue list --json` for
`paused|complete-with-failures`, not just `paused-by-failure` (a paused queue is not
dispatching even though exit ≠ 17). Zombie one-liner:

```bash
comm -23 <(harmonik crew list --json | jq -r '.name' | sort) \
         <(harmonik comms who --json | jq -r '.agent' | sort)
# any name printed = registered-but-offline → reconcile (§1)
```

**Anti-patterns:** (a) a `worktrees/<uuid>` window / `run_started` event is the DAEMON running
a bead — NOT a crew, never evidence the fleet is established; (b) never serialize the whole
fleet behind one bead/lane — while anything runs, keep every non-conflicting lane staffed;
(c) never confuse a stale comms presence with a live crew — registered-but-offline is a zombie
to reconcile, even with a live-looking window.

## 7. Restart continuity

- **You must be launched via `harmonik start captain`** — WHY: it mints the stable
  `--session-id` the keeper rebinds to across `/clear` → `/session-resume` cycles; a bare
  `claude --remote-control captain` has no stable session id and cannot be keeper-cycled.
- **Re-arm `comms recv --follow` after EVERY `/clear` and EVERY park** — first thing, before
  settling into monitor. `--wake` pane-nudge is best-effort; your armed `--follow` is the only
  reliable inbound channel. Can't re-arm (daemon down) → §6 daemon-down recovery; if
  unrecoverable, escalate to the **operator** (status line) — comms cannot reach the admiral
  while the daemon is down.
- **A crew's keeper restart is a NON-EVENT:** it resumes the same `session_id`, re-hydrates
  from its mission + the `--assignee` mirror, and its queue kept draining. Do not
  failure-surface or re-`start crew`; a transient presence drop needs no action.
- **Verify restarts YOU trigger** (your process is external to theirs, so you can):

  ```bash
  out=$(harmonik keeper restart-now --agent <crew> --project "$HARMONIK_PROJECT")
  nonce=$(printf '%s\n' "$out" | sed -n 's/.*nonce=\(rn-[0-9]*\).*/\1/p')
  harmonik keeper await-ack --agent <crew> --nonce "$nonce" --kind restart --timeout 30s \
    --project "$HARMONIK_PROJECT"
  ```

  Non-zero `await-ack` → fail loud, do NOT trust the restart: alert the admiral
  (`comms send --to admiral --topic keeper-alert --from <your-lane>` — your lane, not a
  hardcoded "captain"), check the failure reason (`no_tmux_target` / `sid_not_primary` /
  `handoff_missing` / `handoff_stale` / `ack_inject_failed`), and re-arm that crew's keeper.
  A keeper-DRIVEN automatic crew restart needs no such verification.
- **Your OWN restart:** on a keeper WARN, ack with ONE terse line ("WARN received — restart-now
  at next clean checkpoint") and keep working — **no re-narration/re-summarizing** (that burn
  is what caused 40+ idle warn-cycles). At the next clean idle point (nothing `.dispatching`):
  write `HANDOFF-captain.md` with the KEEPER nonce, run
  `harmonik keeper restart-now --agent captain`, keep the turn open, stop typing. `restart-now`
  is synchronous and self-verifying in-process (it survives your `/clear`) — no external
  wrapper, no self `await-ack`. **NEVER exit or terminate your own session on a WARN** — the
  keeper cannot rebind to a session that already exited.
- **RE-CLASSIFY on every resume:** a handoff's "operator asked X / awaiting Y" is a CLAIM, not
  law. Re-test every inherited pending item against the §0 four-case list (with its split
  targets): autonomous → ACT immediately, do not re-surface; an "await" survives only if it IS
  one of the four cases. When WRITING a handoff, never record "NEXT CAPTAIN: decide X" for an
  autonomous item — write "re-task X (autonomous)" / "staff lane X (autonomous)".

References: `specs/crew-handoff-schema.md` (mission contract) · orchestrator-rules skill
(autonomy + standing rules) · harmonik-dispatch (queue/monitor detail) · agent-comms (N3
dedupe) · beads-cli (write discipline) · keeper skill (bands, doctor) ·
docs/daemon-redeploy.md (daemon recovery) · specs/park-resume-protocol.md (park/wake).
