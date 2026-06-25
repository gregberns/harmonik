# PLAN v1 — Admiral / Captain Operating-Framework Fix

> **STATUS: CONSOLIDATED v1. SUPERSEDES STRAWMAN-v0.** Folds in the critic's
> NEEDS-REWORK verdict (`CRITIC-v0.md`) and the operator's final design decisions.
> This is a PLAN, not an application: nothing here has been written into any live
> skill, mission, or config file. The Integration Path (Part 6) is the apply gate.

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

**More principle-text will NOT work.** The critic's single most important finding,
verified against the live files: the self-check mechanisms the strawman wanted to
"improve with nicer words" *already existed, fired during the 2h stall, and returned
the wrong answer.* Specifically, all of these were live on 2026-06-25:

- A surviving forcing function: the admiral mission's `/loop 1h` (re-armed on every
  `/clear`).
- The precise detector: admiral audit-question **C3** — *"Is the captain idling with
  ready work AND a free crew/queue slot (missed staffing)?"*
- The captain's hard-failure encoding: STARTUP "a monitoring cycle is FAILED if …
  ready beads AND a free slot exist AND the captain did not staff them."
- The explicit prohibition: captain anti-pattern **G** — *"Holding while ready work
  exists … is a FAILURE."*

All three were consulted and **mis-answered**, because they are **self-scored judgment
questions answered through the agent's current frame.** When the frame was "parked =
operator-gated," C3 self-answered "no missed staffing — the lane is correctly gated,"
the "free slot" predicate read false, and the audit scored the idle fleet "ALIGNED"
at every fire. A judgment question filtered through a wrong frame returns the wrong
answer no matter how it is worded. **The fix must remove the judgment from the trigger.**

### The fix: a DETERMINISTIC, AGENT-EXTERNAL trigger

The wake must be a **fact a script computes and pushes**, NOT a classification the
admiral makes. Two candidate signals were specified and compared:

#### Signal (a) — ops-monitor pushes a lane-named wake **[PRIMARY — RECOMMENDED]**

The ops-monitor already runs every ~5 min and already pushes `[IMMEDIATE]` comms
wakes. Extend it to compute, deterministically, in bash/Go:

```
program_drained  AND  a-known-ready-lane-exists  AND  a-free-slot-exists
```

…and when all three hold, **PUSH an `[IMMEDIATE]` wake that NAMES the specific ready
lane** ("program X drained; KNOWN lane Y has N ready beads + a free slot; staff it").

Predicate definitions (all machine-computable, no judgment):
- **program_drained** — the active program's beads are all closed (or the program's
  queue group reports drained / 0 in-flight).
- **a-known-ready-lane-exists** — for each lane recorded in `captain-lanes.md` /
  `admiral-initiatives.md` (the durable registries), `br ready --limit 0` filtered to
  that lane returns ≥1 bead. "KNOWN" = appears in a durable registry (a fact read from
  a file), NOT "is in the live `kerf next` feed right now."
- **a-free-slot-exists** — concurrency cap minus live runs > 0, or ≥1 idle crew.

This **bypasses the admiral's mis-classifying audit entirely.** The admiral cannot
self-score its way out of a wake it didn't generate. The wake names the lane, so the
captain has the answer in hand on receipt.

This is the audit's Conflict-5 carve-out, promoted from the audit doc to load-bearing.

#### Signal (b) — context-size-delta + bead/commit-progress as a liveness signal **[COMPLEMENTARY]**

The operator's signal idea: **log context-size across the whole stack over time**; if
total context isn't moving AND no bead/commit progress is being made, that is a
detectable "stuck" signal. Treat **context-size-delta + bead-close/commit-progress** as
a unified *movement* signal:

- If context is growing but no beads close and no commits land over a window → agents
  are *spinning* (talking, not producing). Flag.
- If context is flat AND no progress over a window → the fleet is *frozen* (the 2h
  stall shape: ~26 messages of pure idle-confirmation, zero throughput). Flag.
- Agents that have **COMPLETED their work and are idle** should be **torn down**
  (reclaim the slot; an idle completed crew is waste, and it inflates "free slot"
  bookkeeping noise).

Signal (b) is a **complementary liveness/anti-spin signal**, not the primary stall-
breaker. It catches the broader "no movement" class (including spin, and idle-completed-
crew teardown) that (a) does not. It is fuzzier (windows, thresholds) and therefore a
second line, not the trigger that names the lane.

#### Recommendation

**Primary = (a)** — deterministic, names the lane, bypasses the audit, directly would
have woken the 2h-stalled fleet. **Complementary = (b)** — a movement/liveness watchdog
plus idle-completed-crew teardown, catching the spin/frozen class (a) misses.

#### CRITICAL — this is CODE, and it must NOT run through the daemon it modifies

Signal (a) and (b) are **daemon / ops-monitor code changes.** They go through the
normal bead pipeline + review gate like any code. **But the daemon-self-fix bootstrap
trap applies:** a daemon-core change must NOT be implemented by a run executing inside
the very daemon it modifies — the in-flight fix can die to its own (old) bug, or the
daemon restart mid-run strands the work. **Apply this work out-of-band** (a worktree
sub-agent or a separate non-self build/deploy step), and salvage-by-content if a run
trips it. This is flagged again in Part 6 (Integration Path) and Part 5 (bead stubs).

---

## Part 1 — The three durable artifacts (operator-confirmed)

Operator decision: **epics + priority-order EXTEND existing docs; NO new files for
those.** The DIRECTION-LOG is the **only** genuinely new file. PLUS a co-located
forcing-function instruction file inside the context folder.

### (a) The EPICS set — EXTENDS existing docs; NO new file

- **Home (unchanged):** `admiral-initiatives.md` is the master "all the big rocks +
  status" registry; `captain-lanes.md` is "which crew is on which lane right now";
  beads (`br` / the epic graph) is the authoritative ledger underneath both.
- **What changes:** nothing structural. The fix is **not** a new PARKED-known/GATED
  enum (the critic's Risk 4 — a new bright line that rebuilds the stall one level
  down). Instead: **"PARKED" becomes a pure fact ("zero ready beads right now"),
  fully decoupled from "is gated."** A lane is GATED **only** if a *named, dated gate
  with an owner and an expiry* is present (Part 1b discipline). **Absence of a live
  named gate deterministically means KNOWN/resumable** — there is no judgment tag to
  mis-set. This is the strawman's item-3 intent, but it **deletes** the PARKED-known
  label rather than adding it (the label was the bright line that would bite).

### (b) The PRIORITY-ORDER list — EXTENDS existing docs; NO new file

- **Home (unchanged):** `kerf next` is the live ranked feed (priority source of truth);
  the **dated operator-directives block** in `captain-lanes.md` records the standing
  ordering that biases the feed; `admiral-initiatives.md` TOP/ON-DECK/PARKED is the
  durable snapshot. We do NOT add a hand-maintained ordered list that would instantly
  drift from `kerf next` and become a 4th source of truth.
- **The one real gap to close (the single best concrete mechanism in the whole effort,
  per the critic — promote it):** every dated directive gets:
  - an `expires:` field, AND
  - an **on-expiry default of "LAPSE → revert to the standing autonomous posture,
    NOT a hold,"** AND
  - an **owner**: the admiral's audit MUST flag an expired-but-present block and either
    re-confirm with the operator or strike it.

  This is what would have prevented the silent lean-park (audit Conflict 2: the
  2026-06-19 scale-out block expired 2026-06-22, nobody struck it, its lapse silently
  reactivated a hold posture). **Operator-confirmed fork (b): keep this — it's
  deterministic.**

### (c) The DIRECTION-LOG — GENUINELY NEW; one tiny file

The only new artifact. It holds the one thing no existing doc holds: **temporal
sequencing intent across direction changes** — the thing `/clear` destroys. `captain-
lanes.md` holds *current* lane state; `admiral-initiatives.md` holds *status*; neither
records *"why we paused X to do Y, and what order we resume in."* That gap is exactly
how "holding for operator" survived five context resets as settled ground truth.

- **Home:** `.harmonik/context/direction-log.md` (tier-2; loaded by admiral + captain
  on every boot, right after tier-3/tier-2 reads). **Operator-confirmed fork (a):
  separate file YES.**
- **Format:** append-only. **ONE entry per direction CHANGE** — never a status update,
  never a per-tick note, never by crews. Newest-first. ~3–5 lines per entry:

  ```
  ## 2026-06-25 ~06:28Z — operator (via admiral) · expires: 2026-07-02
  WHAT: paused all lanes behind the remote test-hardening pyramid.
  WHY:  real-remote feedback loop too slow; build cheap L0-L5 separation harness instead.
  RETURN-PATH: pyramid lands → resume token-opt/wake-economy (standing #1) → then Pi gate
               decision → then other parked lanes. gb-mbp re-enable is a LATER phase.
  ```

  WHAT changed, WHY, the **RETURN-PATH/sequence**, and an **`expires:`** are the four
  load-bearing fields. The RETURN-PATH is the point: a fresh `/clear` reads it to know
  "the pyramid was a *detour*; token-opt resumes after."

- **Anti-rot — forced write + forced read with a freshness gate (critic Risk 2; this
  is the difference between this log and the docs that rotted INTO the bug):**
  - **Forced WRITE:** the same operator-directive-change or admiral re-sequencing event
    that the log records is *already* a comms message. Don't rely on the agent
    remembering to append — make "write the RETURN-PATH entry" a **non-optional step of
    the directive-issuance procedure**, and make the audit check it: **"a dated directive
    block exists with no matching direction-log entry = a FINDING."**
  - **Forced READ + freshness gate:** every entry carries the **same `expires:` +
    on-expiry-default as Part 1b.** An un-renewed RETURN-PATH past expiry **LAPSES to
    "resume standing autonomous posture,"** and the audit flags an expired-but-present
    entry. Without this the log becomes a museum of stale RETURN-PATHs a fresh `/clear`
    reads as live — the exact mechanism that survived five resets.
- **Retention:** capped one-screen-scannable (~10 most-recent entries / ~60 lines).
  Overflow: **delete** oldest (superseded by definition; a superseded RETURN-PATH has
  no future value). No archive file to boot-read.

### (d) Co-located forcing-function file — `.harmonik/context/AGENTS.md` (+ `CLAUDE.md` symlink) — NEW

Operator's folder-level forcing-function idea: put the directives for reading/using/
updating these artifacts **inside the folder that holds them**, so any agent touching
that folder cannot miss them (co-located instructions). This is the folder-scoped
analog of the repo-root `AGENTS.md`/`CLAUDE.md` pair.

- **File:** `.harmonik/context/AGENTS.md`, with `.harmonik/context/CLAUDE.md` as a
  **symlink** to it (same pattern as the repo root — one content source, two names).
- **Scope:** intentionally a ROUTER, not a contract restatement. It points at
  orchestrator-rules for the canonical KNOWN-vs-brand-new definition (Part 2); it owns
  only the *how-to-use-this-folder's-artifacts* directives.

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
- `admiral-initiatives.md` — the big-rocks registry (status snapshot).
- `direction-log.md` (tier-2) — APPEND-ONLY sequencing intent: one entry per direction
  CHANGE (WHAT / WHY / RETURN-PATH / expires). This is the file a fresh /clear reads to
  recover "why we paused X for Y and in what order we resume."

## Boot-read order (admiral + captain)
After tier-3 (project.yaml) and tier-2 (captain-lanes.md), READ direction-log.md before
acting. It is short by design. Its RETURN-PATH is ground truth for sequencing intent.

## KNOWN vs brand-new — DO NOT re-decide it here
The canonical definition lives in the orchestrator-rules skill (§Autonomy). In one line:
a lane that appears in ANY durable doc in this folder (or any past kerf-next ranking) is
KNOWN — resuming/un-parking/re-staffing it is AUTONOMOUS, NOT an operator escalation,
EVEN IF it is parked or shows zero ready beads in the live feed right now. Only a
NEVER-ranked body of work is the operator's to rank. A lane is GATED only if a NAMED,
DATED gate with an owner + expiry is present in this folder; absence of a live named
gate means KNOWN/resumable. There is no PARKED-known tag to set — "parked" just means
"zero ready beads now."

## Forced WRITE
- Whenever you ISSUE or RELAY a direction change (operator directive, or a major admiral
  re-sequencing of the program), you MUST append a direction-log.md entry in the same
  action, with an `expires:`. A directive block with no matching log entry is a FINDING
  the next audit must raise.
- A dated operator-directive in captain-lanes.md MUST carry `expires:` and an owner.

## Forced READ + freshness (anti-rot)
- Every dated directive AND every direction-log entry has `expires:`. ON EXPIRY the
  DEFAULT is LAPSE → revert to the standing autonomous posture — NEVER to a hold.
- The admiral audit OWNS flagging expired-but-present directives/log-entries and either
  re-confirming with the operator or striking them. An expired-but-still-present block
  read as live is the exact bug that froze the fleet for 2h on 2026-06-25.

## Retention
direction-log.md is capped ~10 entries / ~60 lines, newest-first. Delete the oldest on
overflow (a superseded RETURN-PATH has no future value). No archive.

## Don't
- Don't add a 4th priority list here. kerf next is the live ranking; the dated block
  biases it; this folder snapshots it. No hand-maintained ordered list.
- Don't write status updates or per-tick notes into direction-log.md. Direction CHANGES
  only. Crews never write here.
```

---

## Part 2 — The principles (agency, not rules)

Operator was emphatic: **principles, not rules.** Each names the *intent* + the
*tiebreaker* and trusts the agent. **Canonical home = orchestrator-rules §Autonomy
(stated ONCE); every role file gets a one-line pointer (HYBRID — operator-confirmed
fork (c)).** This is the critic's Risk-3 fix: one definition, pointed-to, not the same
sentence copied verbatim into 6 files (which is the fragmentation the project's ROUTER
precedence rule exists to prevent).

### 2.1 — SELF-AUTHORIZATION [CONFIRMED — the principle that dissolves the stall]

> A lane recorded in **any durable doc** (`captain-lanes.md`, `admiral-initiatives.md`,
> the direction-log, a prior HANDOFF, or any past `kerf next`) — or **ever ranked** —
> is a **KNOWN** lane. Resuming it, un-parking it, or re-staffing it is the admiral's
> (and captain's) **own call** — *even when it is currently parked or shows zero ready
> beads in the live feed this instant.* Only a **never-before-recorded** initiative is
> the operator's to rank. A lane is **GATED only when a named, dated, owned, expiring
> gate is present**; absence of a live named gate means KNOWN/resumable. **Operator
> confirmed: resume known/parked lanes on own authority.**

Ambiguity guidance: if unsure whether a lane is "known" or "brand-new" and it appears
in any durable doc, **treat it as KNOWN and act.** (Pi was correctly brand-new;
token-opt never was.)

### 2.2 — WIP-FIRST is a TIEBREAKER, never a veto [operator emphatic]

> When picking the next thing, default to advancing started work before unstarted epics.
> This is a TIEBREAKER for "all else equal," NOT a rule.

Explicit guardrails (this is good rule-design the critic said keep untouched):
- The operator can reprioritize anything, anytime. WIP-first never overrides a fresh
  operator directive.
- **EXPLICIT GUARDRAIL: no agent may EVER cite started-work as a reason it "can't"
  reshuffle priorities.** "We can't drop this, it's in-flight" is a **forbidden
  sentence.** Catching yourself about to refuse a reprioritization on WIP grounds IS
  the signal you've turned a tiebreaker into a veto — don't.
- WIP-first breaks ties *toward throughput*; it does not protect started work from being
  re-ordered, paused, or dropped.

### 2.3 — REFRESH-THEN-ACT is LIGHT [operator: light, not a re-audit]

> Re-derive the **ONE fact you're about to act on** — NOT re-audit everything.

Critic Risk 5 fix (the strawman's introspective "when did I last see this fact?" test
is unanswerable by a fresh `/clear` agent that has no memory of "last saw"). Replace it
with a **mechanical default that requires no introspection:**

> Act on the **boot-digest's live numbers**, never on a claim carried in a doc or
> handoff. STARTUP already says this for HANDOFF ("a claim, not ground truth — the live
> boot digest overrides it"); **generalize it to ALL durable docs.** The digest output
> IS the fresh fact, by construction.

For a one-off in-loop action between boots, re-derive only the single fact you're
betting on (e.g. `br ready --limit 0` for the lane you're about to staff) — a glance,
not a re-audit. Refresh the fact you're betting on, not the world.

### 2.4 — The admiral's JOB (four duties, as principles)

1. **I keep WIP moving.** Default is forward motion. An idle fleet with ready, known
   work and standing authority is itself a problem to solve — not a healthy "lean"
   posture to ratify. (NOTE: the *deterministic detector* of this state is Part 0
   signal (a), pushed externally — NOT a self-scored audit verdict, which is exactly
   what failed.)
2. **I direct + clarify.** Tell the captain which initiatives to push and in what order,
   especially after a program finishes and the next thing is ambiguous.
3. **I check the captain ~every couple of hours** for *direction-correctness AND
   progress* — not just liveness. "Is the captain alive?" is not the question; "is the
   captain advancing the right work?" is.
4. **I answer the captain's questions** from recent operator directives + the
   established priority order + the direction-log, then **decide.** A captain question
   is usually answerable from durable state — answer it; escalate to the operator only
   when the answer genuinely isn't there (a never-ranked initiative).

### 2.5 — "operator away" is NOT a HOLD trigger [confirmed; project-only override]

> "Operator away" is NOT a HOLD trigger. Away + ready KNOWN work = staff it (autonomous).
> "Lean" means don't SPECULATIVELY spin up NEW crews for empty-backlog lanes — it does
> NOT mean leave ready, already-ranked work unstaffed. Resuming known work is "keep
> moving," not "check in."

**Global fix = project-only override in orchestrator-rules. DO NOT amend
`~/.claude/CLAUDE.md` [confirmed].** The `feedback_captain_lean_while_operator_away`
memory note got over-read as "away → HOLD ready work"; the project layer corrects it
without touching the cross-project file.

---

## Part 3 — Existing rule-text to SOFTEN (pointer-not-copy)

These are the live directives the principles reframe. **Each gets a one-line POINTER to
the canonical orchestrator-rules definition — NOT a re-stated copy** (critic Risk 3).
Drops a 6-file verbatim edit to 1 definition + 5 pointers, removing stale-copy risk.

| File / locus | What bites | Reframe (pointer) |
|---|---|---|
| Captain `SKILL.md` §8 case 1 ("brand-NEW … not already in the known feed") | "known feed" read against the LIVE feed, where a parked/drained lane is absent → resume mis-classified as case 1 | Re-key case 1 on "never recorded in any durable doc / never ranked." Add one-line pointer to orchestrator-rules §Autonomy. "EXHAUSTIVE" stays; the *test inside* case 1 is what's wrong. |
| Captain `SKILL.md` AUTONOMOUS set + `STARTUP.md` Step 4 | no duty explicitly covers "resume a parked/drained/previously-ranked lane" | Add resuming a KNOWN parked lane as an explicit autonomous duty (pointer to §Autonomy). An addition that makes the principle reach the case. |
| Captain `STARTUP.md` LAZY-BOOT + `PARKED — no ready beads` marking | "PARKED" overloaded: "operator-gated" vs "no ready beads now"; second silently read as first | "PARKED" = fact ("zero ready beads now"), decoupled from "gated." GATED requires a named live expiring gate. No new enum. Pointer to §Autonomy. |
| `admiral-initiatives.md` status vocab ("PARKED — deliberately held, has a gate") | "has a gate" makes every parked item sound operator-gated | Strike "has a gate" from the generic PARKED gloss; GATED is a *separate* state requiring a named/dated/owned/expiring gate object. Pointer to §Autonomy. |
| `admiral.md` "objective-level ambiguity → escalate, then STOP" | self-authorizable resume routed into "escalate, then STOP" (the stall) | Directing the captain to resume a KNOWN parked lane is in-scope drift-correction, NOT a §8 escalation. Captain leaving a KNOWN ready lane unstaffed across two audits = a captain ERROR to keep pressing, NOT "please rank this." Pointer to §Autonomy. |
| `admiral.md` "Aligned → post one line, then STOP" + playbook anti-narration rules | good anti-noise rules, but the audit's only job became ratifying "aligned"; idle-with-ready-work kept scoring aligned | Do NOT self-apply an "aligned-availability" rule (the frame-filtered judgment that failed). Bind the stall-detection to Part 0 signal (a) — external, machine-computed, lane-named. Anti-narration rules stay; they can't apply to a Part-0-detected stall. |
| `captain-lanes.md` dated scale-out directive block | expired, never struck, lapsed into silent lean-park (Conflict 2) | Part 1b: `expires:` + on-expiry-default "LAPSE → autonomous, not hold" + admiral-audit owns flagging. |
| Global "operator away → lean" reading | over-read as "away → HOLD" | Part 2.5 one-liner in orchestrator-rules. **Project-only — do NOT touch `~/.claude/CLAUDE.md`.** |
| `watch SKILL.md` LEDGER-ONLY `epic_completed` + no-wake staffing flag | the one un-sticking signal routed to the lowest-priority no-wake channel exactly when the captain is idle | Superseded by Part 0 signal (a): the ops-monitor pushes the IMMEDIATE lane-named wake. Watch carve-out becomes a pointer to that mechanism rather than a watch-judgment. |

---

## Part 4 — Parallel workstreams (stubs — define, do not execute)

Two investigations run alongside the apply. Each gets a bead/plan stub here; neither
blocks the Part 0 fix, but workstream (2) GATES the priority-order decision.

### Workstream 1 — INSTRUCTION-BLOAT AUDIT

- **Operator question:** "are the instructions too bloated, causing issues?"
- **Principle:** prefer LESS text + external triggers over more text (this whole plan's
  thesis: the stall happened *despite* C3 + FAILED-(c) + anti-pattern G all being
  present — adding more text did not help; the external trigger does).
- **Scope:** audit captain + admiral instruction *size* and *dilution* — measure
  total tokens loaded at boot per role; find duplicated/near-duplicated contracts;
  find directives that are restated in 3+ files; find principle-text that has no
  trigger (reads well, changes nothing). Output = a cut-list ranked by tokens-saved ×
  dilution-removed. **Bead stub:** `chore` / `codename:instruction-bloat-audit`,
  P2, no code, produces a report + cut-list.

### Workstream 2 — kerf/bv/alternative-ranker assessment + SMALL-BEAD-STARVATION

- **The problem:** major initiatives get attention; **small beads get forgotten.**
  Major-initiative gravity starves small/low-priority beads that never bubble to the
  top of `kerf next`. This **gates the priority-order decision** — we should not lock
  the priority-order extension (Part 1b) until we know whether `kerf next` alone, `bv`
  graph-metrics, or an alternative ranker is the right surface to surface starved
  small beads.
- **Scope:** assess `kerf next` vs `bv --robot-insights` (PageRank/betweenness) vs an
  alternative ranker for: (i) does it surface small starved beads, (ii) does it respect
  the standing dated-directive ordering, (iii) can the ops-monitor (Part 0 signal a)
  read its "known-ready-lane" predicate from it cheaply. **Bead stub:** `task` /
  `codename:ranker-starvation-assessment`, P1 (gates Part 1b lock), no code initially,
  produces a recommendation. **Dependency: Part 1b priority-order edits wait on this
  recommendation.**

---

## Part 5 — Bead stubs for the Part 0 CODE work

(Stubs only — not created/dispatched here. Codename `codename:stall-detector`.)

- **SD-1 (PRIMARY, code):** ops-monitor computes `program_drained AND known-ready-lane
  AND free-slot` and pushes an `[IMMEDIATE]` wake naming the lane. P0. **OUT-OF-BAND
  build/deploy — daemon-self-fix trap: do NOT run through the live daemon.**
- **SD-2 (complementary, code):** context-size-delta + bead/commit-progress movement
  watchdog; flags frozen/spinning fleet. P1. Same out-of-band caveat.
- **SD-3 (complementary, code):** tear down crews that have COMPLETED work and gone
  idle (reclaim slot). P1.
- **SD-4 (test):** scenario test reproducing the 2h-stall shape (program drains, known
  parked lane has ready beads, free slot) and asserting the IMMEDIATE fires + names the
  lane. Author via worktree sub-agent (scenario-test 30-min budget caveat).

All SD-* go through the normal bead pipeline + review gate. The review gate is NOT
optional for daemon-core code.

---

## Part 6 — INTEGRATION PATH

```
draft (DONE: STRAWMAN-v0)
  → review (DONE: CRITIC-v0 → NEEDS-REWORK)
  → consolidate (DONE: THIS doc, PLAN-v1)
  → OPERATOR APPROVAL  ← we are here
  → APPLY
```

**APPLY decomposes into four tracks. Two re-sync / gotcha flags are load-bearing:**

> **FLAG — embedded-asset re-sync gotcha:** editing anything under `.claude/skills/*`
> (orchestrator-rules, captain, admiral skills) requires **copying the edit to the
> embedded asset copy** (the binary ships an embedded copy) or the change WON'T TAKE on
> a fresh deploy. Every skill/mission edit below must be followed by the `cp`-to-embedded
> step. Same applies to `scripts/captain-tools/*` if the ops-monitor lives there.

> **FLAG — daemon-self-fix bootstrap trap (Part 0):** the SD-* ops-monitor/daemon code
> must be built/deployed **out-of-band** (worktree sub-agent or separate non-self build),
> NOT executed by a run inside the live daemon it modifies. Salvage-by-content if a run
> trips it.

**Track A — artifact files (no code, no daemon):**
1. Create `.harmonik/context/direction-log.md` (seed with the current pyramid→token-opt
   RETURN-PATH entry + `expires:`).
2. Create `.harmonik/context/AGENTS.md` (Part 1d content) and the `.harmonik/context/
   CLAUDE.md` **symlink** → AGENTS.md.
3. Add `expires:` + on-expiry-default + admiral-audit-owner to the `captain-lanes.md`
   dated-directives block format. **(Hold the priority-order lock pending Workstream 2.)**
4. Strike "has a gate" from the generic PARKED gloss in `admiral-initiatives.md`;
   define GATED as a separate named/dated/owned/expiring state.

**Track B — skill/mission edits (each → embedded-asset re-sync):**
5. orchestrator-rules §Autonomy: add the canonical KNOWN-vs-brand-new sentence (2.1),
   WIP-tiebreaker (2.2), refresh-then-act-light (2.3), "operator away ≠ hold" (2.5).
   **Project-only — do NOT touch `~/.claude/CLAUDE.md`.**
6. captain `SKILL.md` + `STARTUP.md`: one-line POINTERS (Part 3 rows) + add the
   resume-known-lane autonomous duty + wire direction-log + `.harmonik/context/AGENTS.md`
   into boot-read order after tier-2.
7. admiral `admiral.md` + playbook: pointers (Part 3 rows) + the four-duty JOB framing
   (2.4) + the expired-directive audit ownership + bind stall-detection to Part 0
   signal (a) instead of the self-scored aligned-verdict.
8. watch `SKILL.md`: replace the LEDGER-ONLY-with-no-wake staffing gap with a pointer
   to the Part-0 ops-monitor IMMEDIATE.

**Track C — boot-read wiring:**
9. Admiral + captain boot order: tier-3 → tier-2 (captain-lanes) → **direction-log.md**
   → orchestrator-rules. Verify `.harmonik/context/AGENTS.md` is honored when a session
   touches the folder.

**Track D — the Part 0 CODE bead (out-of-band, through the daemon pipeline + review):**
10. SD-1 (primary) → SD-2/SD-3 (complementary) → SD-4 (scenario test). Build/deploy
    out-of-band; review-gate every commit.

---

## Sequenced apply-task list (checkboxes)

- [ ] **0. Operator approval** of PLAN-v1 (gate; everything below is post-approval).
- [ ] **1. Track A — direction-log.md** created + seeded with pyramid→token-opt entry (+ `expires:`).
- [ ] **2. Track A — `.harmonik/context/AGENTS.md`** created (Part 1d content).
- [ ] **3. Track A — `.harmonik/context/CLAUDE.md` symlink** → AGENTS.md created.
- [ ] **4. Track A — `captain-lanes.md`** dated-directive format gains `expires:` + on-expiry-LAPSE-default + admiral-audit owner. (Priority-order *lock* waits on Workstream 2.)
- [ ] **5. Track A — `admiral-initiatives.md`** PARKED gloss de-gated; GATED defined as a named/dated/owned/expiring state.
- [ ] **6. Track B — orchestrator-rules §Autonomy** gains canonical 2.1 / 2.2 / 2.3 / 2.5 (project-only; NOT `~/.claude/CLAUDE.md`). → **embedded-asset re-sync.**
- [ ] **7. Track B — captain SKILL.md + STARTUP.md** pointers + resume-known autonomous duty + boot-read wiring. → **embedded-asset re-sync.**
- [ ] **8. Track B — admiral.md + playbook** pointers + four-duty JOB + expired-directive audit ownership + bind detection to Part-0 signal (a). → **embedded-asset re-sync.**
- [ ] **9. Track B — watch SKILL.md** LEDGER-ONLY gap → pointer to Part-0 IMMEDIATE. → **embedded-asset re-sync.**
- [ ] **10. Track C — boot-read order** wired (admiral + captain): tier-3 → tier-2 → direction-log → orchestrator-rules; folder AGENTS.md honored.
- [ ] **11. Track D — SD-1** (ops-monitor lane-named IMMEDIATE) bead created + dispatched **out-of-band** + review-gated.
- [ ] **12. Track D — SD-2 / SD-3** (movement watchdog + idle-completed-crew teardown) beads, out-of-band + review-gated.
- [ ] **13. Track D — SD-4** scenario test (2h-stall reproduction) via worktree sub-agent.
- [ ] **14. Workstream 1** instruction-bloat-audit bead created (parallel; non-blocking).
- [ ] **15. Workstream 2** ranker-starvation-assessment bead created (parallel; **gates task 4's priority-order lock**).
- [ ] **16. Verify** SD-1 fires on the next program-drain + known-ready-lane + free-slot; confirm the wake names the lane and the captain staffs without escalating.

---

## OPEN QUESTIONS (for the operator)

1. **Signal (b) thresholds.** The movement/liveness watchdog needs a window + a
   "no-progress" threshold (context-delta cap, bead-close/commit count over T). Pick
   defaults or have the assessment (Workstream 2) propose them? (Recommend: ship SD-1
   primary first; tune SD-2 thresholds after a real drain.)
2. **Idle-completed-crew teardown (SD-3) aggressiveness.** Tear down immediately on
   completion+idle, or after a grace window (in case the next lane is about to be
   assigned to the same crew)? Recommend a short grace window to avoid spawn churn.
3. **Workstream-2 gate on Part 1b.** Confirm the priority-order *lock* (the dated-
   directive ordering semantics) should wait on the ranker/starvation assessment, while
   the `expires:`/LAPSE/audit-owner mechanism (deterministic, independent of ranker
   choice) ships now in task 4. (Recommend: yes — mechanism now, ordering-semantics
   after.)
4. **direction-log seed scope.** Seed only the current pyramid→token-opt entry, or
   back-fill the last 1–2 prior direction changes so the first `/clear` after apply has
   context? (Recommend: seed the current one only; the log is forward-looking.)
5. **ops-monitor home.** SD-1 in the daemon's ops-monitor (Go) vs a
   `scripts/captain-tools/*` bash check the daemon invokes. The latter is easier to
   change out-of-band but adds an embedded-asset re-sync surface. (Recommend: bash check
   for fast iteration now; fold into the daemon later if it proves load-bearing.)
```
