# STRAWMAN v0 — Admiral / Captain Operating Framework

> **STATUS: STRAWMAN FOR OPERATOR RED-PEN. NOT FINAL. NOT YET APPLIED.**
> Nothing in here has been written into a live skill, mission, or config file. This
> doc proposes principles + artifact homes; every `[OPEN-Q]` mark and the closing
> OPEN-QUESTIONS list are places I expect you to redline. Do not treat any sentence
> here as a decision until you've signed off.

---

## Why this exists (one paragraph)

On 2026-06-25 the fleet finished your #1 overnight program (the remote-separation
test pyramid) at ~14:18Z and then sat at **zero work for ~2 hours** while producing
~26 messages and ~30 wakeups — all of them confirming it was idle. Root cause
(`plans/2026-06-25-transcript-retro-tool/out/ANALYSIS.md` + `out/audit/conflicts.md`):
the admiral and captain mis-classified *"resume a known, parked, already-ranked
lane"* (token-optimization — your standing #1) as *"rank a brand-new initiative"*
(the operator-only class that actually applied only to the Pi gateway). They bundled
the self-authorizable option with the operator-gated one into a single escalation
menu and sat on the whole thing — and the hold posture **re-instantiated itself
verbatim through every keeper `/clear`**, turning a momentary lapse into a structural,
self-reinstantiating config. The instant you said "you had the authority," the admiral
un-stuck the fleet in <10 minutes with **no new information** — proving it was
decision-avoidance, not a missing-authority or missing-information gap.

The deeper problem you named: **rules keep biting us.** Every conflict in the audit is
a bright-line rule meeting a case it didn't anticipate, and the agent picking the
conservative (HOLD) side. This framework answers with **principles that confer agency**,
not more rules — plus three durable artifacts so sequencing *intent* survives `/clear`.

---

## Part 1 — The three durable artifacts (EXTEND-vs-NEW)

Hard reconciliation goal: **avoid a 4th/5th overlapping doc.** Two of the three
already have a home; only one is genuinely new, and it's deliberately tiny.

### (a) The EPICS set — **EXTENDS existing docs; no new file**

- **Home:** the existing pair already covers this. `admiral-initiatives.md` is the
  master "all the big rocks + status" registry (ACTIVE / ON-DECK / PARKED / DONE);
  `captain-lanes.md` is "which crew is on which lane right now." Beads (`br` / the
  epic graph) is the authoritative ledger underneath both.
- **What changes:** nothing structural — these two docs are the right shape. The fix
  is the **self-authorization principle (Part 2.3)**: "PARKED" must stop reading as
  "operator-gated." Proposed: add to the `admiral-initiatives.md` status vocabulary a
  one-line gloss distinguishing **PARKED-known** (admiral may un-park) from a true
  **operator-gate** (the rare case the operator must lift). `[OPEN-Q 1]`
- **Format / who / when / retention:** unchanged from today. Admiral owns
  `admiral-initiatives.md` (one line + status per initiative; reconciled each audit).
  Captain owns `captain-lanes.md` (the lane table; updated at session end or on any
  crew/epic change). Both stay SHORT; both committed the same session they're edited.

### (b) The PRIORITY-ORDER list — **EXTENDS existing docs; no new file**

- **Home:** the priority *ordering* already lives in three layered places, and the
  layering is correct: `kerf next` is the live ranked feed (the priority **source of
  truth**); the **dated operator directives** block in `captain-lanes.md` records the
  standing ordering (e.g. "token-opt is #1") that biases that feed; `admiral-initiatives.md`
  TOP/ON-DECK/PARKED is the durable human-readable snapshot.
- **Why it "drifts slightly over time" is already handled:** `kerf next` re-ranks
  continuously; the directives block is dated and append/amended. We do NOT want a
  separate hand-maintained ordered list that would immediately drift from `kerf next`
  and become a 4th source of truth to reconcile.
- **The one real gap to close:** the dated-directives block has **no expiry default
  and no owner** (audit Conflict 2 — the 2026-06-19 scale-out block expired
  2026-06-22, nobody re-confirmed or struck it, and its lapse silently reactivated a
  "lean-park" posture). Proposed: every dated directive gets an `expires:` AND an
  on-expiry default of **"LAPSE → revert to the standing autonomous posture, NOT to a
  hold"**, and the admiral's audit MUST flag an expired-but-present block and either
  re-confirm with you or strike it. `[OPEN-Q 2]`

### (c) The DIRECTION-LOG — **GENUINELY NEW; one tiny file**

This is the only new artifact. It exists because the retro found that **sequencing
intent is exactly what `/clear` destroys** — and nothing in today's doc set holds it.
`captain-lanes.md` holds *current* lane state; `admiral-initiatives.md` holds *status*;
neither records *"why we paused X to do Y, and what order we resume in."* When the
captain/admiral `/clear` and re-read, they inherit the *posture* but not the *plan
behind it* — which is precisely how "holding for operator" survived five context
resets as settled ground truth.

- **Home:** `.harmonik/context/direction-log.md` (tier-2, sits beside `captain-lanes.md`;
  loaded by admiral + captain on every boot). `[OPEN-Q 3 — name/location]`
- **Format:** append-only. **ONE entry per direction *change*** — never a status
  update, never a per-tick note. Newest-first. Each entry is ~3-5 lines:

  ```
  ## 2026-06-25 ~06:28Z — operator (via admiral)
  WHAT: paused all lanes behind the remote test-hardening pyramid.
  WHY:  real-remote feedback loop too slow; build cheap L0-L5 separation harness instead.
  RETURN-PATH: pyramid lands → resume token-opt/wake-economy (standing #1) → then Pi gate
               decision → then other parked lanes. gb-mbp re-enable is a LATER phase, not now.
  ```

  WHAT changed, WHY, and the intended **RETURN-PATH / sequence** are the three
  load-bearing fields. The RETURN-PATH is the whole point: it's the thing a fresh
  `/clear` session reads to know "the pyramid was a *detour*, token-opt resumes after."

- **Who / when:** written ONLY on (i) an operator-directive change, or (ii) a **major
  admiral sequencing decision** (a real re-ordering of the program, not a staffing
  tweak). **Never** by crews. **Never** per-tick. **Never** for a status update. If an
  entry would just restate current lane state, it does not belong here.
- **Retention / size discipline:** capped to stay **one-screen scannable** (proposed:
  ~10 most-recent entries / ~60 lines). When it overflows, the oldest entries are
  rotated out — either deleted (they're superseded by definition) or moved to a
  `direction-log-archive.md` that nobody boot-reads. `[OPEN-Q 4 — cap size + delete vs archive]`
- **Read by:** every admiral boot and every captain boot, right after the tier-3/tier-2
  reads. It is short enough that this adds negligible boot cost.

**Why this is NOT doc fragmentation:** it holds the one thing no existing doc holds
(temporal sequencing *intent* across direction changes), it's tiny and append-only
(near-zero maintenance), and it's *read-mostly* — the value is at the read on a fresh
`/clear`, not the write. It does not duplicate lane state, status, or the ranked feed.

---

## Part 2 — The principles (agency, not rules)

These are written as **principles that confer judgment**, deliberately NOT as
bright lines. The retro's lesson is that every bright line eventually meets an
unanticipated case and the agent picks HOLD. A principle names the *intent* and the
*tiebreaker*, and trusts the agent to apply it.

### 2.1 — The admiral's JOB (stated as principles)

The admiral exists to keep the fleet pointed right and moving. Four duties:

1. **Keep WIP moving.** Your default is forward motion. An idle fleet with ready,
   known work and standing authority is itself a problem to solve — not a healthy
   "lean" posture to ratify. (The retro's core failure: the admiral's alignment audit
   scored the idle fleet "ALIGNED" every fire, because nothing in its loop could see a
   correctly-idle-with-ready-work fleet *as* the stall.)
2. **Give the captain direction.** Clarify which initiatives to push and in what
   order, especially after a program finishes and the next thing is ambiguous.
3. **Check the captain ~every couple of hours** for *direction-correctness* and
   *progress* — not just liveness. "Is the captain alive and posting?" is not the
   question; "is the captain advancing the right work?" is.
4. **Answer the captain's questions yourself** by inspecting recent operator
   directives + the established priority order + the direction-log, then **deciding**.
   A captain question is usually answerable from durable state — answer it; escalate to
   the operator only when the answer genuinely isn't there (a never-ranked initiative).

### 2.2 — REFRESH-THEN-ACT (the deepest principle)

> Before acting on any durable doc, **refresh the actual state**, reconcile/update the
> doc if it has drifted, THEN act on the fresh picture.

This **generalizes** a pattern that already exists only at boot — "HANDOFF is a claim,
not ground truth; the live boot digest overrides it" — and extends it from *boot* to
*every decision*. The retro showed agents acting on **stale state**: the captain
self-idled on a posture that was never re-litigated, the admiral relayed claims "on
trust," and the expired scale-out block was acted on as if live.

**Keep it LIGHT** — this is the failure mode to avoid. Refresh-then-act must NOT become
a heavyweight ritual that adds its own friction. The discipline is proportional:

- A big sequencing decision (re-staff the whole fleet, change the program order) →
  spot-check the load-bearing facts (`br ready --limit 0` for the lane, `kerf next`
  top, the direction-log) before acting.
- A routine in-lane action (re-task a crew to the next ranked lane) → a glance at the
  ranked feed is enough; don't re-derive the world.
- The test is "is the fact I'm about to act on one I last saw *before* a `/clear` or
  more than an audit-cycle ago?" If yes, refresh that *one* fact. If it's fresh,
  proceed. Refresh the fact you're betting on, not everything. `[OPEN-Q 5 — is this light enough?]`

### 2.3 — SELF-AUTHORIZATION (the principle that dissolves the stall)

> A lane recorded in **any durable doc** (`captain-lanes.md`, `admiral-initiatives.md`,
> the direction-log, a prior HANDOFF, or any past `kerf next`) — or **ever ranked** —
> is a **KNOWN** lane. Resuming it, un-parking it, or re-staffing it is the admiral's
> (and captain's) **own call** — *even when it is currently parked or shows zero ready
> beads in the live feed this instant.* Only a **never-before-recorded initiative** is
> the operator's to rank.

This is the single highest-leverage change (it's the audit's "single highest-leverage
fix"). Framed as a **principle, not a bright-line rule**: the *intent* is "executing an
existing priority is your job; setting a brand-new priority from scratch is the
operator's." The test keys on **"was this ever ranked / recorded,"** NOT "is it in the
live feed right now" (a parked lane with no ready beads drops out of the live feed —
which is exactly the trap that made token-opt *read* as brand-new).

Guidance for the genuinely ambiguous case: **if you're unsure whether a lane is
"known" or "brand-new," and it appears in any durable doc, treat it as KNOWN and act.**
The brand-new case is reserved for a body of work that has *never* been ranked,
registered, or named anywhere. (Pi was correctly that case; token-opt never was.)

### 2.4 — WIP-FIRST (a tiebreaker, never a veto)

> When picking the next thing, **default to advancing started work before unstarted
> epics.** This is a TIEBREAKER for "all else equal," not a rule.

Explicit guardrails, because this principle is the kind that calcifies into a rule
that bites:

- **The operator can reprioritize anything, anytime.** WIP-first never overrides a
  fresh operator directive.
- **No agent may EVER cite "it's WIP / already started" as a reason it *can't*
  reshuffle priorities.** "We can't drop this, it's in-flight" is a forbidden
  sentence. If you find yourself about to refuse a reprioritization on WIP grounds,
  that's the signal you've turned a tiebreaker into a veto — don't.
- WIP-first breaks ties *toward throughput* (finishing a started thing usually
  realizes value sooner and frees a slot). It does not protect started work from being
  re-ordered, paused, or dropped.

---

## Part 3 — Existing rules to SOFTEN

These are the rule-shaped directives in the live skill/mission files that the
principles above should reframe. **I have NOT edited any of these** — this is the
red-pen target list. Each entry: the file, what it says, why it bites, the softened
reframe.

1. **Captain `SKILL.md:489-507` — §8 "Surface-and-await: EXACTLY four cases
   (EXHAUSTIVE)."** Case 1 = "Ranking a brand-NEW initiative not already in the known
   `kerf next` / `br` feed." *Bites:* "not already in the known feed" is read against
   the **live** feed, where a parked/drained lane isn't present — so resuming
   token-opt classified as case 1. *Soften:* re-key case 1 on **"never recorded in any
   durable doc / never ranked,"** and add the explicit carve-out that a parked or
   drained previously-ranked lane is KNOWN (the self-authorization principle, 2.3). The
   word "EXHAUSTIVE" is fine; the *test inside case 1* is what's wrong.

2. **Captain `SKILL.md:67-79` (AUTONOMOUS set) + `STARTUP.md:228-231, 250-255`.** The
   autonomous duties are stated well, but **none of them explicitly covers "resume a
   parked / drained / previously-ranked lane."** *Bites:* the gap let the captain put
   resumption on the forbidden side by default. *Soften:* add resumption of a KNOWN
   parked lane as an explicit autonomous duty (mirrors 2.3). This is an *addition* that
   makes the existing principle reach the case, not a new rule.

3. **Captain `STARTUP.md:250` "LAZY BOOT" + the `PARKED — no ready beads` marking.**
   *Bites:* "PARKED" is overloaded — it means both "deliberately operator-gated" AND
   "merely has no ready beads at this instant." The retro shows the second silently
   read as the first. *Soften:* split the vocabulary — **PARKED-empty** (no ready
   beads now; auto-re-staffs the moment work appears; admiral/captain un-parks freely)
   vs **PARKED-gated** (a real operator gate; named gate required). Lazy-boot stays;
   it just stops implying "ask first."

4. **`admiral-initiatives.md` status vocabulary (`:14`): "PARKED (deliberately held,
   has a gate)."** *Bites:* "has a gate" makes *every* parked item sound
   operator-gated, reinforcing the wrong reading (audit Conflict 1). *Soften:*
   distinguish "PARKED-known (admiral may un-park)" from "GATED (operator must lift)";
   require a *named* gate for the latter (per 2.3).

5. **`admiral.md:129-131` — "Objective-level ambiguity (a genuine §8 question only the
   operator can settle) → escalate to operator … Then STOP."** *Bites:* combined with
   the missing parked-known distinction, the admiral routed a *self-authorizable* resume
   into "escalate, then STOP" (the stall). *Soften:* add that **directing the captain
   to resume a KNOWN parked lane is in-scope drift-correction, NOT a §8 escalation** —
   escalate to the operator only for a never-before-ranked body of work. And: a captain
   leaving a KNOWN ready lane unstaffed across two audits with a free slot is a captain
   ERROR to keep pressing, not a "please rank this for me."

6. **`admiral.md:124-127` "Aligned → post one line … Then STOP" + playbook Rules 8-10
   ("one artifact per audit," "silent aligned audit is a success").** *Bites (subtle):*
   these are *good* anti-noise rules, but in the stall they meant the audit's only job
   was to ratify "aligned" — and an idle-with-ready-work fleet kept scoring "aligned."
   *Soften:* not a deletion — **add a detector**: "aligned" is NOT available as a verdict
   when there is **ready known work + a free slot + an idle crew**; that state is a
   *finding* (direct the captain to staff, or surface the throughput dial), never a
   clean audit. The anti-narration rules stay; they just can't apply to a stall.

7. **`captain-lanes.md:172-189` — the dated scale-out directive block.** *Bites:*
   expired 2026-06-22, never re-confirmed/struck, lapsed into a silent lean-park
   (audit Conflict 2). *Soften (this is the Part 1(b) fix):* give every dated directive
   an `expires:` + on-expiry default of "LAPSE → standing autonomous posture, not a
   hold," + make the admiral audit responsible for flagging expired-but-present blocks.

8. **Global `~/.claude/CLAUDE.md` "Keep moving" vs "Check in first" (cross-project) +
   the `feedback_captain_lean_while_operator_away` memory.** *Bites:* "away + only
   low-pri work → keep fleet LEAN, don't auto-expand crews" got over-read as "away →
   HOLD ready work." *Soften (project layer, in `orchestrator-rules`):* one line —
   **"operator away" is NOT a HOLD trigger; away + ready KNOWN work = staff it. "Lean"
   means don't speculatively spin up NEW crews for empty-backlog lanes; it does NOT
   mean leave ready, already-ranked work unstaffed.** Resuming known work is "keep
   moving," not "check in." `[OPEN-Q 6 — this one touches your global file; project-only override OK?]`

> **Note on principle-vs-rule consistency:** items 1-6 are all the *same* missing
> distinction (parked-known vs brand-new) surfacing in four files. The cleanest
> application of "principles not rules" is to state self-authorization (2.3) **once** as
> a shared principle and let each file *point* to it, rather than re-litigate a bright
> line in each. `[OPEN-Q 7 — one shared principle vs per-file softening]`

---

## OPEN-QUESTIONS (for the operator's red pen)

1. **PARKED vocabulary split.** OK to split "PARKED" into *PARKED-known* (admiral
   un-parks freely) vs *GATED* (operator must lift, named gate required) across
   `admiral-initiatives.md` and the captain docs? Or do you want a different word pair?

2. **Dated-directive expiry default.** Confirm the on-expiry default = "LAPSE → revert
   to standing autonomous posture, NOT a hold," and that the admiral audit owns
   flagging/striking expired blocks. (This is what would have prevented the silent
   lean-park.)

3. **Direction-log home + name.** `.harmonik/context/direction-log.md` as proposed, or
   would you rather it live inside `captain-lanes.md` as a section (one fewer file, but
   couples its retention to the lane doc)? My recommendation: separate file — its
   append-only retention discipline is different from the lane doc's.

4. **Direction-log cap + rotation.** ~10 entries / ~60 lines, oldest rotated out —
   delete (superseded by definition) or archive to a non-boot-read file? My lean:
   delete; the RETURN-PATH of a superseded direction has no future value.

5. **Is REFRESH-THEN-ACT light enough?** The principle says "refresh the *one* fact
   you're betting on, not everything." Does that proportional framing land, or do you
   want an even lighter touch (e.g. only at boot + before whole-fleet re-staffs)?

6. **Global-file softening (item 8).** The "operator away → lean" reframe touches your
   cross-project `~/.claude/CLAUDE.md`. Do you want the fix applied as a *project-only*
   override in `orchestrator-rules` (leaves the global file alone), or amended globally?

7. **One shared principle vs per-file softening.** Items 1-6 are the same missing
   distinction in four files. Prefer one canonical self-authorization principle that
   each file points to (less duplication, principles-not-rules-consistent), or explicit
   softened wording inline in each file (more redundant, more robust to a single file
   being missed on a `/clear`)?

8. **Admiral verdict "aligned" detector (item 6).** Is "an idle fleet with ready known
   work + a free slot can NEVER score 'aligned'" the right detector, or too blunt?
   (There may be a legitimate "intentionally lean while you sleep" posture — if so, it
   needs to live in the direction-log as an explicit RETURN-PATH, not be inferred.)
