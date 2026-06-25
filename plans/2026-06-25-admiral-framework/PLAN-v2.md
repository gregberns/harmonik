# PLAN v2 — Admiral / Captain Operating-Framework Fix

> **STATUS: REVISED v2. SUPERSEDES PLAN-v1.** Folds in the reviewer's
> APPLY-WITH-FIXES verdict (the 5 must-fixes) and three facts verified against the
> live tree on 2026-06-25. This is still a PLAN, not an application: nothing here has
> been written into any live skill, mission, or config file. **Part 6 (Integration
> Path) is the apply gate, and it is gated on operator approval (STEP 0).**
>
> **What changed from v1 (the 5 reviewer must-fixes):**
> 1. **Lane index is now a first-class, build-FIRST artifact** (`.harmonik/context/lanes.json`)
>    — the deterministic trigger's hard prerequisite. v1 hand-waved "read the lanes from
>    `captain-lanes.md`," which is prose + bead-IDs-in-blockquotes and needs the very
>    judgment we are removing. **New Part 0.5 + bead LI-1.**
> 2. **LI-1 is split OUT of the ranker workstream** — it ships immediately as SD-1's
>    prerequisite; it does NOT wait on the ranker/starvation assessment (which gates only
>    the priority-*order* lock, a different thing). **Part 4 + Part 5 sequencing.**
> 3. **"Prove it":** SD-1 is NOT "done" until the SD-4 scenario test *reproduces the
>    actual 2026-06-25 stall shape* (pyramid drains → token-opt lane has ready beads →
>    free slot) and asserts the IMMEDIATE fires + names `wake-economy`. **Part 5 SD-4.**
> 4. **Embedded-asset path named explicitly:** `cmd/harmonik/assets/skills/` — every
>    skill/mission edit in Track B must `cp` there. **Part 6 FLAG + each Track-B step.**
> 5. **Trigger home DECIDED (was OQ5): bash `scripts/ops-monitor-check.sh`.** Verified:
>    the daemon shells out via `bash scripts/ops-monitor-check.sh` from repo root and
>    re-reads the live file each 5-min fire (`internal/daemon/opsmonitor_schedule.go:41`,
>    `cmd/harmonik/schedule.go:503`). It is **NOT embedded** (absent from
>    `cmd/harmonik/assets/scripts/`). **Consequence — folded throughout: the primary
>    signal SD-1 has NO embedded re-sync, NO binary rebuild, and NO daemon-self-fix
>    bootstrap trap.** It is bash data the daemon reads, not daemon-core Go.

---

## Why this exists (one paragraph)

On 2026-06-25 the fleet finished the operator's #1 overnight program (the remote-
separation test pyramid) at 14:18Z and then sat at **zero work for ~2 hours** while
producing ~26 messages and ~30 wakeups — every one of them confirming it was idle.
Root cause (`../2026-06-25-transcript-retro-tool/out/ANALYSIS.md` +
`out/audit/conflicts.md`): the admiral and captain mis-classified *"resume a known,
parked, already-ranked lane"* (token-optimization — the standing #1) as *"rank a
brand-new initiative"* (the operator-only class that actually applied only to the Pi
model-gateway). They bundled the self-authorizable option with the operator-gated one
into a single escalation menu and sat on the whole thing — and the hold posture
**re-instantiated itself verbatim through every keeper `/clear`**, turning a momentary
lapse into a structural, self-reinstantiating config. The instant the operator said
"you had the authority," the admiral un-stuck the fleet in <10 minutes with **no new
information** — proving it was decision-avoidance, not a missing-authority or
missing-information gap.

---

## Part 0 — THE CORE PIVOT (the real fix)

**More principle-text will NOT work.** The reviewer's single most important finding,
verified against the live files: the self-check mechanisms the strawman wanted to
"improve with nicer words" *already existed, fired during the 2h stall, and returned
the wrong answer.* All of these were live on 2026-06-25:

- A surviving forcing function: the admiral mission's `/loop 1h` (re-armed on every `/clear`).
- The precise detector: admiral audit-question **C3** — *"Is the captain idling with
  ready work AND a free crew/queue slot (missed staffing)?"*
- The captain's hard-failure encoding: STARTUP "a monitoring cycle is FAILED if …
  ready beads AND a free slot exist AND the captain did not staff them."
- The explicit prohibition: captain anti-pattern **G** — *"Holding while ready work
  exists … is a FAILURE."*

All were consulted and **mis-answered**, because they are **self-scored judgment
questions answered through the agent's current frame.** When the frame was "parked =
operator-gated," C3 self-answered "no missed staffing — the lane is correctly gated,"
the "free slot" predicate read false, and the audit scored the idle fleet "ALIGNED" at
every fire. A judgment question filtered through a wrong frame returns the wrong answer
no matter how it is worded. **The fix must remove the judgment from the trigger.**

### The fix: a DETERMINISTIC, AGENT-EXTERNAL trigger

The wake must be a **fact a script computes and pushes**, NOT a classification the
admiral makes. The trigger lives in the existing **`scripts/ops-monitor-check.sh`**
(verified: a 1310-line bash one-pass health check that *already* computes
`idle-fleet`, `ready-unstaffed`, and `backlog-ready`, runs every ~5 min as a daemon
schedule, and already pushes `[IMMEDIATE]` comms wakes). SD-1 is an **extension of
checks 5/6/8**, not new infrastructure.

#### Signal (a) — ops-monitor pushes a lane-named wake **[PRIMARY — RECOMMENDED]**

Extend `ops-monitor-check.sh` to compute, deterministically:

```
program_drained  AND  a-known-ready-lane-exists  AND  a-free-slot-exists
```

…and when all three hold, **PUSH an `[IMMEDIATE]` wake that NAMES the specific ready
lane** ("program X drained; KNOWN lane Y has N ready beads + a free slot; staff it").

Predicate definitions (all machine-computable, no judgment — and see **Part 0.5** for
the lane index that makes the middle predicate possible):
- **program_drained** — the active program's beads are all closed (or the program's
  queue group reports drained / 0 in-flight).
- **a-known-ready-lane-exists** — for each lane in `.harmonik/context/lanes.json`
  (Part 0.5), `br ready --parent <epic_id> --limit 0 --json` returns ≥1 bead and the
  lane is not GATED (no live named/dated/owned/unexpired gate in the index entry).
  "KNOWN" = *present in the index*, a fact read from a file — NOT "is in the live
  `kerf next` feed right now."
- **a-free-slot-exists** — concurrency cap minus live runs > 0, or ≥1 idle crew (both
  already derivable in the script).

This **bypasses the admiral's mis-classifying audit entirely.** The admiral cannot
self-score its way out of a wake it didn't generate. The wake names the lane, so the
captain has the answer in hand on receipt. This is the audit's Conflict-5 carve-out,
promoted to load-bearing.

#### Signal (b) — movement/liveness watchdog **[COMPLEMENTARY]**

Log context-size across the stack + bead-close/commit progress over time as a unified
*movement* signal:
- Context growing but no beads close + no commits over a window → agents *spinning*. Flag.
- Context flat AND no progress over a window → fleet *frozen* (the 2h-stall shape). Flag.
- Crews that have **COMPLETED their work and gone idle** → **tear down** (reclaim slot;
  an idle completed crew is waste and inflates "free slot" bookkeeping).

Signal (b) is a fuzzier second line (windows, thresholds) that catches the spin/frozen
class (a) misses. It is **not** the primary stall-breaker.

#### Recommendation

**Primary = (a)** — deterministic, names the lane, bypasses the audit, directly would
have woken the 2h-stalled fleet. **Complementary = (b)** — movement/liveness watchdog
plus idle-completed-crew teardown.

#### Bootstrap-trap status — CORRECTED from v1

v1 said "Signal (a) and (b) are daemon code changes → the daemon-self-fix bootstrap
trap applies." **For SD-1 this is now FALSE and dropped:** SD-1 lives in
`scripts/ops-monitor-check.sh`, which the daemon *shells out to* and re-reads live each
fire — it is bash data, not daemon-core Go, requires no rebuild and no daemon restart,
and is picked up on the next 5-min tick. **No bootstrap trap, no out-of-band build for
SD-1.** Edit the file through the normal bead pipeline + review gate like any script.
The **out-of-band caveat survives only for SD-2/SD-3** *if* they need Go-side telemetry
(context-size logging) or daemon-core crew-teardown — those are evaluated when scoped,
not assumed.

---

## Part 0.5 — THE LANE INDEX (BLOCKER #1 — build this FIRST)

**This is the load-bearing must-fix.** Signal (a)'s middle predicate —
*a-known-ready-lane-exists* — is **not computable from today's `captain-lanes.md`**: it
is freeform prose with bead IDs inside blockquotes and lane scope mixed into a markdown
table cell. Parsing it requires the exact judgment SD-1 exists to remove. So SD-1 cannot
be built until a machine-readable lane→epic map exists.

### Decision: `.harmonik/context/lanes.json` (a durable registry), joined on `epic_id`

A small, durable JSON file the ops-monitor reads with `jq`. One object per lane:

```json
{
  "schema_version": 1,
  "updated": "2026-06-25T18:30:00Z",
  "lanes": [
    {
      "lane": "wake-economy",
      "label": "codename:wake-economy",
      "epic_id": "hk-var9b",
      "status": "active",
      "gate": null
    },
    {
      "lane": "remote-hardening",
      "label": "codename:remote-hardening",
      "epic_id": "hk-gx0dl",
      "status": "active",
      "gate": null
    },
    {
      "lane": "pi-gateway",
      "label": "codename:pi-openrouter",
      "epic_id": null,
      "plan_path": "plans/2026-06-23-pi-openrouter-harness/",
      "status": "parked",
      "gate": { "owner": "operator", "reason": "not before remote-worker proven", "expires": "2026-07-09" }
    }
  ]
}
```

> *(Example uses live epics verified 2026-06-25: `wake-economy`=`hk-var9b` (OPEN),
> `remote-hardening`=`hk-gx0dl` (OPEN, the live remote-worker lane). `pi-gateway` is the
> deliberate epic-less case — see the rule below. The LI-1 seed reads the real lane
> table; this is illustrative.)*

- **Join key = `epic_id`.** The ops-monitor computes ready-count per lane via
  `br ready --parent <epic_id> --limit 0 --json`. This is more reliable than a label
  scan because not every child bead carries the `codename:` label, but every bead under
  the lane hangs off its epic. (`label` is kept as a secondary/cross-check field, since
  most lanes already use `codename:<name>` — see the live `captain-lanes.md` table.)
- **Epic-less lanes (the Pi case — reviewer-flagged design gap):** a parked initiative
  can exist with **no epic yet** (Pi lives only as `plans/2026-06-23-pi-openrouter-
  harness/`). An `epic_id`-keyed index cannot compute ready-beads for it — so the rule
  is: **`epic_id: null` REQUIRES a non-null `gate`.** A lane with no epic AND no gate is
  an INVALID entry the audit must flag, because it has no ready-bead source and therefore
  cannot be a staffing candidate. While `epic_id` is null the lane is GATED-by-definition
  (it contributes zero to *known-ready-lane-exists*) until it is decomposed into an epic;
  `plan_path` records where its design lives. This keeps SD-1's predicate total — an
  epic-less lane can never spuriously fire the wake.
- **`gate`** is the *only* thing that makes an epic-bearing lane non-resumable: a
  structured object with `owner` + `reason` + `expires`. **`null` (or an expired gate)
  deterministically means KNOWN/resumable** — there is no judgment tag to mis-set. This
  is Part 1a's "PARKED is a fact, GATED is a named object" rule, made machine-readable.
  An *expired* gate is treated as absent (the LAPSE→autonomous default, Part 1b) and the
  admiral audit flags it for re-confirm-or-strike.
- **Ownership + anti-rot:** `lanes.json` is admiral-owned, updated in the same action as
  any lane add/retask/park (the same discipline as the direction-log forced-write). The
  admiral audit reconciles it against `captain-lanes.md` + `kerf next`; a lane present in
  a durable doc but absent from `lanes.json` (or vice versa) is a FINDING. It is small
  (one screen) and tracked in git.

### Sequencing (must-fix #2): LI-1 is SPLIT OUT of the ranker workstream

`lanes.json` (bead **LI-1**) is SD-1's direct prerequisite and ships **immediately**,
independent of Workstream 2 (the kerf/bv/ranker + small-bead-starvation assessment).
Workstream 2 gates only the priority-*order* **lock** (Part 1b ordering semantics) —
a different decision. The lane index answers "*which* lanes exist and are ready," not
"*in what order* to rank them"; SD-1 needs only the former. **Do not let LI-1 wait on
the ranker assessment.**

**Apply order inside Track D:** `LI-1 (lanes.json) → SD-1 (ops-monitor reads it) →
SD-4 (scenario test proves it) → SD-2/SD-3 (complementary)`.

---

## Part 1 — The three durable artifacts (operator-confirmed)

Operator decision: **epics + priority-order EXTEND existing docs; NO new files for
those.** The DIRECTION-LOG is the only genuinely new prose file; `lanes.json` (Part 0.5)
is the new machine-readable file; PLUS the co-located forcing-function instruction file.

### (a) The EPICS set — EXTENDS existing docs; lane facts now also in `lanes.json`

- **Home (unchanged):** `admiral-initiatives.md` = the master "big rocks + status"
  registry; `captain-lanes.md` = "which crew is on which lane now"; beads/`br` = the
  authoritative ledger underneath. **NEW:** `lanes.json` is the machine-readable mirror
  the ops-monitor reads (Part 0.5) — it does not replace the prose docs, it makes the
  lane→epic→ready-count join computable.
- **What changes:** **"PARKED" becomes a pure fact ("zero ready beads right now"),
  fully decoupled from "is gated."** A lane is GATED **only** if a *named, dated, owned,
  expiring gate* is present (Part 1b discipline; in `lanes.json` this is the `gate`
  object). **Absence of a live named gate deterministically means KNOWN/resumable.** We
  **delete** the PARKED-known label rather than add it (critic Risk 4: a new enum just
  rebuilds the stall one level down).

### (b) The PRIORITY-ORDER list — EXTENDS existing docs; NO new file

- **Home (unchanged):** `kerf next` is the live ranked feed; the **dated operator-
  directives block** in `captain-lanes.md` records the standing ordering that biases the
  feed; `admiral-initiatives.md` TOP/ON-DECK/PARKED is the durable snapshot. We do NOT
  add a hand-maintained ordered list (it would instantly drift and become a 4th source
  of truth).
- **The one real gap to close (promote — the critic's single best concrete mechanism):**
  every dated directive gets:
  - an `expires:` field, AND
  - an **on-expiry default of "LAPSE → revert to the standing autonomous posture, NOT a
    hold,"** AND
  - an **owner**: the admiral's audit MUST flag an expired-but-present block and either
    re-confirm with the operator or strike it.

  This is what would have prevented the silent lean-park (Conflict 2: the 2026-06-19
  scale-out block expired 2026-06-22, nobody struck it, its lapse silently reactivated a
  hold). **Operator-confirmed: keep — it's deterministic.** The priority-order *lock*
  (ordering semantics) waits on Workstream 2; this `expires:`/LAPSE/owner *mechanism*
  ships now (it is independent of the ranker choice).

### (c) The DIRECTION-LOG — GENUINELY NEW; one tiny file

Holds the one thing no existing doc holds: **temporal sequencing intent across direction
changes** — the thing `/clear` destroys. That gap is exactly how "holding for operator"
survived five context resets as settled ground truth.

- **Home:** `.harmonik/context/direction-log.md` (tier-2; loaded by admiral + captain on
  every boot, right after tier-3/tier-2). **Operator-confirmed: separate file YES.**
- **Format:** append-only. **ONE entry per direction CHANGE** — never a status update,
  never per-tick, never by crews. Newest-first. ~3–5 lines per entry:

  ```
  ## 2026-06-25 ~06:28Z — operator (via admiral) · expires: 2026-07-02
  WHAT: paused all lanes behind the remote test-hardening pyramid.
  WHY:  real-remote feedback loop too slow; build cheap L0-L5 separation harness instead.
  RETURN-PATH: pyramid lands → resume token-opt/wake-economy (standing #1) → then Pi gate
               decision → then other parked lanes. gb-mbp re-enable is a LATER phase.
  ```

  WHAT / WHY / **RETURN-PATH/sequence** / **`expires:`** are the four load-bearing fields.
- **Anti-rot — forced write + forced read with a freshness gate:**
  - **Forced WRITE:** the directive-change event that the log records is *already* a comms
    message — make "write the RETURN-PATH entry" a **non-optional step of the directive-
    issuance procedure**, and make the audit check it: *"a dated directive block with no
    matching direction-log entry = a FINDING."*
  - **Forced READ + freshness gate:** every entry carries the **same `expires:` +
    on-expiry-default as Part 1b.** An un-renewed RETURN-PATH past expiry **LAPSES to
    "resume standing autonomous posture,"** and the audit flags an expired-but-present
    entry.
- **Retention:** capped ~10 entries / ~60 lines, newest-first. Overflow → **delete**
  oldest. No archive file to boot-read.

### (d) Co-located forcing-function file — `.harmonik/context/AGENTS.md` (+ `CLAUDE.md` symlink) — NEW

Put the directives for reading/using/updating these artifacts **inside the folder that
holds them**, the folder-scoped analog of the repo-root `AGENTS.md`/`CLAUDE.md` pair.

- **File:** `.harmonik/context/AGENTS.md`, with `.harmonik/context/CLAUDE.md` as a
  **symlink** to it.
- **Scope:** a ROUTER, not a contract restatement. Points at orchestrator-rules for the
  canonical KNOWN-vs-brand-new definition; owns only *how-to-use-this-folder's-artifacts*.

**Drafted content (`.harmonik/context/AGENTS.md`):**

```markdown
# .harmonik/context — operating directives for the artifacts in THIS folder

> CLAUDE.md is a symlink to this file. Same content. If you are an admiral or captain
> session and you are reading, editing, or reasoning about any file in this folder,
> these directives apply. They do NOT restate contracts — they point you at the
> canonical rule and tell you how to keep these specific artifacts honest.

## The artifacts here, and what each is for
- `project.yaml` (tier-3) — phase, locked decisions, guardrails.
- `captain-lanes.md` (tier-2) — current lanes + epics-in-progress + parked + the DATED
  OPERATOR-DIRECTIVES block (priority ordering).
- `lanes.json` — MACHINE-READABLE lane→epic index the ops-monitor reads (lane, epic_id,
  status, gate). Keep it in sync with the prose docs in the SAME action you change a lane.
- `admiral-initiatives.md` — the big-rocks registry (status snapshot).
- `direction-log.md` (tier-2) — APPEND-ONLY sequencing intent: one entry per direction
  CHANGE (WHAT / WHY / RETURN-PATH / expires). The file a fresh /clear reads to recover
  "why we paused X for Y and in what order we resume."

## Boot-read order (admiral + captain)
After tier-3 (project.yaml) and tier-2 (captain-lanes.md), READ direction-log.md before
acting. It is short by design. Its RETURN-PATH is ground truth for sequencing intent.

## KNOWN vs brand-new — DO NOT re-decide it here
The canonical definition lives in the orchestrator-rules skill (§Autonomy). In one line:
a lane that appears in ANY durable doc in this folder (or any past kerf-next ranking) is
KNOWN — resuming/un-parking/re-staffing it is AUTONOMOUS, NOT an operator escalation,
EVEN IF it is parked or shows zero ready beads in the live feed right now. Only a
NEVER-ranked body of work is the operator's to rank. A lane is GATED only if a NAMED,
DATED, OWNED, EXPIRING gate is present (in lanes.json: a non-null, unexpired `gate`
object); absence of a live named gate means KNOWN/resumable. There is no PARKED-known
tag to set — "parked" just means "zero ready beads now."

## Forced WRITE
- Whenever you ISSUE or RELAY a direction change, you MUST append a direction-log.md
  entry in the same action, with an `expires:`. A directive block with no matching log
  entry is a FINDING the next audit must raise.
- A dated operator-directive in captain-lanes.md MUST carry `expires:` and an owner.
- Whenever you ADD / RETASK / PARK a lane, update lanes.json in the same action.

## Forced READ + freshness (anti-rot)
- Every dated directive AND every direction-log entry has `expires:`. ON EXPIRY the
  DEFAULT is LAPSE → revert to the standing autonomous posture — NEVER to a hold.
- The admiral audit OWNS flagging expired-but-present directives/log-entries/gates and
  either re-confirming with the operator or striking them.

## Retention
direction-log.md is capped ~10 entries / ~60 lines, newest-first. Delete the oldest on
overflow. No archive.

## Don't
- Don't add a 4th priority list here. kerf next is the live ranking; the dated block
  biases it; this folder snapshots it.
- Don't write status updates or per-tick notes into direction-log.md. Direction CHANGES
  only. Crews never write here.
```

---

## Part 2 — The principles (agency, not rules)

**Principles, not rules.** Each names the *intent* + the *tiebreaker* and trusts the
agent. **Canonical home = orchestrator-rules §Autonomy (stated ONCE); every role file
gets a one-line pointer (HYBRID — operator-confirmed).** Critic Risk-3 fix: one
definition, pointed-to, not copied verbatim into 6 files.

### 2.1 — SELF-AUTHORIZATION [CONFIRMED — the principle that dissolves the stall]

> A lane recorded in **any durable doc** (`captain-lanes.md`, `admiral-initiatives.md`,
> `lanes.json`, the direction-log, a prior HANDOFF, or any past `kerf next`) — or **ever
> ranked** — is a **KNOWN** lane. Resuming it, un-parking it, or re-staffing it is the
> admiral's (and captain's) **own call** — *even when it is currently parked or shows
> zero ready beads in the live feed this instant.* Only a **never-before-recorded**
> initiative is the operator's to rank. A lane is **GATED only when a named, dated,
> owned, expiring gate is present**; absence of a live named gate means KNOWN/resumable.

Ambiguity guidance: if unsure whether a lane is "known" or "brand-new" and it appears in
any durable doc, **treat it as KNOWN and act.** (Pi was correctly brand-new; token-opt
never was.)

### 2.2 — WIP-FIRST is a TIEBREAKER, never a veto [operator emphatic]

> When picking the next thing, default to advancing started work before unstarted epics.
> This is a TIEBREAKER for "all else equal," NOT a rule.

- The operator can reprioritize anything, anytime. WIP-first never overrides a fresh
  operator directive.
- **EXPLICIT GUARDRAIL: no agent may EVER cite started-work as a reason it "can't"
  reshuffle priorities.** "We can't drop this, it's in-flight" is a **forbidden
  sentence.** Catching yourself about to refuse a reprioritization on WIP grounds IS the
  signal you've turned a tiebreaker into a veto — don't.

### 2.3 — REFRESH-THEN-ACT is LIGHT [operator: light, not a re-audit]

> Re-derive the **ONE fact you're about to act on** — NOT re-audit everything.

Critic Risk 5 fix — replace the unanswerable introspective "when did I last see this
fact?" with a mechanical default:

> Act on the **boot-digest's live numbers**, never on a claim carried in a doc or
> handoff. STARTUP already says this for HANDOFF; **generalize it to ALL durable docs.**
> The digest output IS the fresh fact, by construction. For a one-off in-loop action
> between boots, re-derive only the single fact you're betting on (e.g. `br ready
> --parent <epic> --limit 0` for the lane you're about to staff) — a glance, not a
> re-audit.

### 2.4 — The admiral's JOB (four duties, as principles)

1. **I keep WIP moving.** An idle fleet with ready, known work and standing authority is
   itself a problem to solve — not a healthy "lean" posture to ratify. (The *deterministic
   detector* of this state is Part 0 signal (a), pushed externally — NOT a self-scored
   audit verdict, which is what failed.)
2. **I direct + clarify.** Tell the captain which initiatives to push and in what order,
   especially after a program finishes and the next thing is ambiguous.
3. **I check the captain ~every couple of hours** for *direction-correctness AND
   progress* — not just liveness. "Is the captain advancing the right work?" is the
   question.
4. **I answer the captain's questions** from recent operator directives + the established
   priority order + the direction-log, then **decide.** Escalate to the operator only
   when the answer genuinely isn't in durable state (a never-ranked initiative).

### 2.5 — "operator away" is NOT a HOLD trigger [confirmed; project-only override]

> "Operator away" is NOT a HOLD trigger. Away + ready KNOWN work = staff it (autonomous).
> "Lean" means don't SPECULATIVELY spin up NEW crews for empty-backlog lanes — it does
> NOT mean leave ready, already-ranked work unstaffed.

**Global fix = project-only override in orchestrator-rules. DO NOT amend
`~/.claude/CLAUDE.md` [confirmed].** The `feedback_captain_lean_while_operator_away`
memory note got over-read as "away → HOLD ready work"; the project layer corrects it
without touching the cross-project file.

---

## Part 3 — Existing rule-text to SOFTEN (pointer-not-copy)

Each gets a one-line POINTER to the canonical orchestrator-rules definition — NOT a
re-stated copy (critic Risk 3). Drops a 6-file verbatim edit to 1 definition + 5 pointers.

| File / locus | What bites | Reframe (pointer) |
|---|---|---|
| Captain `SKILL.md` §8 case 1 ("brand-NEW … not already in the known feed") | "known feed" read against the LIVE feed, where a parked/drained lane is absent → resume mis-classified as case 1 | Re-key case 1 on "never recorded in any durable doc / never ranked." One-line pointer to orchestrator-rules §Autonomy. |
| Captain `SKILL.md` AUTONOMOUS set + `STARTUP.md` Step 4 | no duty explicitly covers "resume a parked/drained/previously-ranked lane" | Add resuming a KNOWN parked lane as an explicit autonomous duty (pointer to §Autonomy). |
| Captain `STARTUP.md` LAZY-BOOT + `PARKED — no ready beads` marking | "PARKED" overloaded: "operator-gated" vs "no ready beads now" | "PARKED" = fact, decoupled from "gated." GATED requires a named live expiring gate. No new enum. Pointer to §Autonomy. |
| `admiral-initiatives.md` status vocab ("PARKED — deliberately held, has a gate") | "has a gate" makes every parked item sound operator-gated | Strike "has a gate" from the generic PARKED gloss; GATED is a *separate* state requiring a named/dated/owned/expiring gate object (mirrors `lanes.json`). Pointer to §Autonomy. |
| `admiral.md` "objective-level ambiguity → escalate, then STOP" | self-authorizable resume routed into "escalate, then STOP" (the stall) | Directing the captain to resume a KNOWN parked lane is in-scope drift-correction, NOT a §8 escalation. Pointer to §Autonomy. |
| `admiral.md` "Aligned → post one line, then STOP" + playbook anti-narration | the audit's only job became ratifying "aligned"; idle-with-ready-work kept scoring aligned | Do NOT self-apply an "aligned-availability" rule. Bind stall-detection to Part 0 signal (a) — external, machine-computed, lane-named. Anti-narration rules stay. |
| `captain-lanes.md` dated scale-out directive block | expired, never struck, lapsed into silent lean-park (Conflict 2) | Part 1b: `expires:` + on-expiry-LAPSE-default + admiral-audit owns flagging. |
| Global "operator away → lean" reading | over-read as "away → HOLD" | Part 2.5 one-liner in orchestrator-rules. **Project-only — do NOT touch `~/.claude/CLAUDE.md`.** |
| `watch SKILL.md` LEDGER-ONLY `epic_completed` + no-wake staffing flag | the one un-sticking signal routed to the lowest-priority no-wake channel exactly when the captain is idle | Superseded by Part 0 signal (a): the ops-monitor pushes the IMMEDIATE lane-named wake. Watch carve-out becomes a pointer to that mechanism. |

---

## Part 4 — Parallel workstreams (stubs — define, do not execute)

Neither blocks the Part 0 fix. **Note the sequencing correction (must-fix #2): the lane
index (LI-1) is NOT part of Workstream 2 — it is SD-1's prerequisite and ships first.**

### Workstream 1 — INSTRUCTION-BLOAT AUDIT

- **Operator question:** "are the instructions too bloated, causing issues?"
- **Principle:** prefer LESS text + external triggers over more text (this plan's thesis:
  the stall happened *despite* C3 + FAILED-(c) + anti-pattern G all present).
- **Scope:** measure total tokens loaded at boot per role; find duplicated/near-duplicated
  contracts; directives restated in 3+ files; principle-text with no trigger. Output = a
  cut-list ranked by tokens-saved × dilution-removed. **Bead stub:** `chore` /
  `codename:instruction-bloat-audit`, P2, no code, produces a report + cut-list.

### Workstream 2 — kerf/bv/alternative-ranker assessment + SMALL-BEAD-STARVATION

- **The problem:** major initiatives get attention; **small beads get forgotten.** This
  **gates the priority-ORDER lock** (Part 1b ordering semantics) — NOT the lane index,
  and NOT the `expires:`/LAPSE/owner mechanism (which is ranker-independent and ships now).
- **Scope:** assess `kerf next` vs `bv --robot-insights` (PageRank/betweenness) vs an
  alternative ranker for: (i) does it surface small starved beads, (ii) does it respect
  the standing dated-directive ordering, (iii) can the ops-monitor read its lane-ready
  predicate cheaply (note: with Part 0.5 the ops-monitor reads `lanes.json` + `br ready
  --parent`, so this third criterion is now largely de-risked). **Bead stub:** `task` /
  `codename:ranker-starvation-assessment`, P1, no code initially, produces a recommendation.
  **Dependency: Part 1b priority-order *lock* waits on this — Part 0.5 / LI-1 does NOT.**

---

## Part 5 — Bead stubs for the Part 0 CODE work

(Stubs only — not created/dispatched here.)

- **LI-1 (PREREQUISITE, `codename:lane-index`, data):** create `.harmonik/context/
  lanes.json` (Part 0.5 schema) seeded from the live `captain-lanes.md` lane table +
  `admiral-initiatives.md`; add the admiral forced-write/reconcile discipline. **P0.
  Ships FIRST — SD-1's hard prerequisite. NOT gated on Workstream 2.** Pure data/doc;
  no daemon code; no bootstrap trap.
- **SD-1 (PRIMARY, `codename:stall-detector`, bash):** extend `scripts/ops-monitor-
  check.sh` to compute `program_drained AND known-ready-lane (from lanes.json + br ready
  --parent) AND free-slot` and push an `[IMMEDIATE]` wake **naming the lane**. P0.
  **Bash, daemon-re-read, NO embedded re-sync, NO rebuild, NO bootstrap trap.** Normal
  bead pipeline + review gate. **Blocked-by LI-1.**
- **SD-4 (test, `codename:stall-detector`):** scenario test that **reproduces the actual
  2026-06-25 stall** — program (remote-pyramid) drains, the KNOWN parked lane
  (`wake-economy`/token-opt, the real lane that was stranded) has ready beads, a free slot
  exists — and asserts the IMMEDIATE fires AND names `wake-economy`. **SD-1 is NOT "done"
  until SD-4 is green (must-fix #3 — prove it).** Author via worktree sub-agent
  (scenario-test 30-min-budget caveat). **Blocked-by SD-1.**
- **SD-2 (complementary, code):** context-size-delta + bead/commit-progress movement
  watchdog; flags frozen/spinning fleet. P1. Out-of-band caveat applies **only if** it
  needs Go-side telemetry — evaluate when scoped.
- **SD-3 (complementary, code):** tear down crews that COMPLETED work and went idle. P1.
  Same conditional out-of-band note.

All SD-*/LI-1 go through the normal bead pipeline + review gate. The review gate is NOT
optional.

---

## Part 6 — INTEGRATION PATH

```
draft (DONE: STRAWMAN-v0)
  → review (DONE: CRITIC-v0 → NEEDS-REWORK)
  → consolidate (DONE: PLAN-v1)
  → re-review (DONE: APPLY-WITH-FIXES, 5 must-fixes)
  → revise (DONE: THIS doc, PLAN-v2)
  → OPERATOR APPROVAL  ← we are here (STEP 0)
  → APPLY
```

> **FLAG — embedded-asset re-sync (must-fix #4):** editing anything under
> `.claude/skills/*` (orchestrator-rules, captain, admiral skills) requires copying the
> edit to **`cmd/harmonik/assets/skills/<skill>/`** (the binary ships an embedded copy)
> or the change WON'T TAKE on a fresh deploy. Every Track-B step below names this `cp`.
> **NOTE:** `scripts/ops-monitor-check.sh` is **NOT** embedded (verified absent from
> `cmd/harmonik/assets/scripts/`; the daemon runs it from the repo root) — so SD-1 needs
> **no** re-sync. The `.harmonik/context/*` artifact files are likewise live-read, not
> embedded.

> **FLAG — daemon-self-fix bootstrap trap:** applies ONLY to any SD-2/SD-3 work that
> turns out to need daemon-core Go. **SD-1 (bash) and LI-1 (data) are trap-free** (Part 0
> correction). Build daemon-core Go out-of-band (worktree sub-agent), salvage-by-content
> if a run trips it.

**Track A — artifact files (no code, no daemon):**
1. Create `.harmonik/context/direction-log.md` (seed the current pyramid→token-opt
   RETURN-PATH entry + `expires:`).
2. Create `.harmonik/context/AGENTS.md` (Part 1d content) + `.harmonik/context/CLAUDE.md`
   **symlink** → AGENTS.md.
3. **Create `.harmonik/context/lanes.json` (LI-1, Part 0.5)** seeded from the live lane
   table. *(This is Track-A data, but it is also SD-1's prerequisite — do it before
   Track D.)*
4. Add `expires:` + on-expiry-LAPSE-default + admiral-audit-owner to the
   `captain-lanes.md` dated-directives block. *(Priority-order **lock** waits on
   Workstream 2; this **mechanism** ships now.)*
5. Strike "has a gate" from the generic PARKED gloss in `admiral-initiatives.md`; define
   GATED as a separate named/dated/owned/expiring state (mirrors `lanes.json.gate`).

**Track B — skill/mission edits (each → `cp` to `cmd/harmonik/assets/skills/<skill>/`):**
6. orchestrator-rules §Autonomy: add canonical 2.1 / 2.2 / 2.3 / 2.5. **Project-only — do
   NOT touch `~/.claude/CLAUDE.md`.** → `cp` to `cmd/harmonik/assets/skills/orchestrator-rules/`.
7. captain `SKILL.md` + `STARTUP.md`: Part-3 pointers + resume-known autonomous duty +
   wire direction-log + `lanes.json` + `.harmonik/context/AGENTS.md` into boot-read order.
   → `cp` to `cmd/harmonik/assets/skills/captain/`.
8. admiral `admiral.md` + playbook: pointers + four-duty JOB (2.4) + expired-directive
   audit ownership + bind stall-detection to Part 0 signal (a). → `cp` to the embedded
   admiral skill/mission path.
9. watch `SKILL.md`: LEDGER-ONLY gap → pointer to the Part-0 ops-monitor IMMEDIATE.
   → `cp` to `cmd/harmonik/assets/skills/watch/`.

**Track C — boot-read wiring:**
10. Admiral + captain boot order: tier-3 → tier-2 (captain-lanes) → **direction-log.md**
    → orchestrator-rules; `lanes.json` read by the ops-monitor (not a boot-read).

**Track D — the Part 0 code (LI-1 first, then bash SD-1, then prove with SD-4):**
11. **LI-1** (`lanes.json`) — already created in Track A step 3; here = the admiral
    forced-write/reconcile discipline + the `br ready --parent` predicate wiring SD-1 uses.
12. **SD-1** (ops-monitor lane-named IMMEDIATE) — bash edit to `scripts/ops-monitor-
    check.sh`; bead + review gate; **no out-of-band, no re-sync.** Blocked-by LI-1.
13. **SD-4** scenario test (2026-06-25 stall reproduction) via worktree sub-agent;
    **SD-1 not "done" until SD-4 green.** Blocked-by SD-1.
14. **SD-2 / SD-3** (movement watchdog + idle-completed-crew teardown) — scope, then apply
    (out-of-band only if daemon-core Go).

**Track E — parallel investigations (non-blocking):**
15. Workstream 1 instruction-bloat-audit bead.
16. Workstream 2 ranker-starvation-assessment bead (**gates Track A step 4's priority-
    order LOCK only — NOT LI-1**).

---

## Sequenced apply-task list (checkboxes)

- [ ] **0. Operator approval** of PLAN-v2 (gate; everything below is post-approval).
- [ ] **1. `.harmonik/context/lanes.json` (LI-1)** created + seeded — *do this first; SD-1 depends on it.*
- [ ] **2. `direction-log.md`** created + seeded with pyramid→token-opt entry (+ `expires:`).
- [ ] **3. `.harmonik/context/AGENTS.md`** created (Part 1d) + **`CLAUDE.md` symlink**.
- [ ] **4. `captain-lanes.md`** dated-directive format gains `expires:` + LAPSE-default + audit owner. (Order *lock* waits on Workstream 2.)
- [ ] **5. `admiral-initiatives.md`** PARKED gloss de-gated; GATED defined as named/dated/owned/expiring (mirrors lanes.json).
- [ ] **6. orchestrator-rules §Autonomy** gains 2.1 / 2.2 / 2.3 / 2.5 (project-only). → `cp` `cmd/harmonik/assets/skills/orchestrator-rules/`.
- [ ] **7. captain SKILL.md + STARTUP.md** pointers + resume-known duty + boot-read wiring (direction-log + lanes.json). → `cp` `cmd/harmonik/assets/skills/captain/`.
- [ ] **8. admiral.md + playbook** pointers + four-duty JOB + expired-directive audit ownership + bind to Part-0 signal (a). → `cp` embedded admiral path.
- [ ] **9. watch SKILL.md** LEDGER-ONLY gap → pointer to Part-0 IMMEDIATE. → `cp` `cmd/harmonik/assets/skills/watch/`.
- [ ] **10. boot-read order** wired (admiral + captain): tier-3 → tier-2 → direction-log → orchestrator-rules.
- [ ] **11. SD-1** (bash extend `scripts/ops-monitor-check.sh`) bead + review gate. No re-sync, no out-of-band. Blocked-by LI-1.
- [ ] **12. SD-4** scenario test reproduces the 2026-06-25 stall + asserts IMMEDIATE names `wake-economy`; SD-1 not done until green.
- [ ] **13. SD-2 / SD-3** (movement watchdog + idle-completed-crew teardown) scoped + applied.
- [ ] **14. Workstream 1** instruction-bloat-audit bead (parallel; non-blocking).
- [ ] **15. Workstream 2** ranker-starvation-assessment bead (parallel; gates task-4 priority-order LOCK only).
- [ ] **16. Verify** SD-1 fires on the next program-drain + known-ready-lane + free-slot; the wake names the lane and the captain staffs without escalating.

---

## OPEN QUESTIONS (for the operator) — REDUCED from v1

Two v1 questions are now RESOLVED and removed:
- ~~OQ5 (ops-monitor home)~~ → **DECIDED: bash `scripts/ops-monitor-check.sh`** (it
  already runs the analogous checks, is daemon-re-read, needs no re-sync/rebuild, no
  bootstrap trap). Go fold-in only later, only if it proves load-bearing.
- ~~"read the known-ready-lane predicate from the ranker"~~ → **RESOLVED by Part 0.5:**
  the ops-monitor reads `lanes.json` + `br ready --parent`, independent of the ranker.

Remaining:
1. **Signal (b) thresholds.** SD-2's movement watchdog needs a window + no-progress
   threshold. Recommend: ship SD-1 primary first; tune SD-2 after a real drain.
2. **Idle-completed-crew teardown (SD-3) aggressiveness.** Immediately on completion+idle,
   or after a short grace window (avoid spawn churn when the next lane reuses the crew)?
   Recommend a short grace window.
3. **Workstream-2 gate scope.** Confirm: the priority-order **lock** (dated-directive
   ordering semantics) waits on the ranker/starvation assessment, while the
   `expires:`/LAPSE/audit-owner *mechanism* AND the lane index (LI-1) ship now. Recommend: yes.
4. **direction-log seed scope.** Seed only the current pyramid→token-opt entry, or
   back-fill 1–2 prior changes? Recommend: current one only; the log is forward-looking.
5. **lanes.json seed completeness.** Seed only the *active + on-deck + parked* lanes now
   in `captain-lanes.md` / `admiral-initiatives.md`, or also enumerate the DONE ones?
   Recommend: active + on-deck + parked only (DONE lanes aren't staffing candidates).
```
