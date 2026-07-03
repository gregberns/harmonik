# Instruction-Audit: Directive Conflicts Behind the ~2h Idle Stall

**Situation under audit:** a major program finished; the fleet then sat idle ~2h
because admiral + captain mis-classified "resume a known/parked/already-ranked lane"
(which they HAD standing authority to start) as "rank a brand-new initiative"
(the operator-only, surface-and-await class).

**Method:** read the actual instruction files; quote colliding directives with
`file:line`; show why each collides in the "drained queue + ready parked lane +
operator away" situation; propose concrete reconciled wording.

**The headline finding (Conflict 1):** the boundary that the whole stall turns on —
"resume an already-ranked / parked / KNOWN lane" (self-authorizable) vs "rank a
brand-NEW initiative" (operator-only) — is stated REPEATEDLY but the disambiguating
sub-case is never made explicit: **a lane that is PARKED with a gate, or that has just
re-acquired ready beads, is still a KNOWN lane — un-parking it is the AUTONOMOUS
"execute the existing ranking" act, not the forbidden "rank a brand-new initiative"
act.** Every skill defines the two poles; none defines the line between "parked-known"
and "brand-new." That single missing sentence is what let both roles classify a
token-opt resumption as the forbidden class.

---

## Ranked conflicts

### 1. THE CENTRAL GAP — "resume a parked/known lane" has no explicit home on the autonomous side of the bright line

This is the conflict that produced the stall, ranked #1.

**Directive A — the AUTONOMOUS duty (captain SKILL.md:67-79, R-C4.6 at :97-111):**
> *"AUTONOMOUS — do these every boot and continuously, WITHOUT being told: …
> Organize the KNOWN open backlog into lanes by **consuming the existing `kerf next`
> ranking** … Re-task a crew whose lane is **COMPLETE** to the next-ranked KNOWN
> lane … Fill every non-conflicting free slot. Keep the fleet moving; do NOT park it
> 'in case.'"* (`SKILL.md:67-79`)

**Directive B — the SURFACE-AND-AWAIT exception (captain SKILL.md:489-501, §8 "EXHAUSTIVE"):**
> *"1. **Ranking a brand-NEW initiative** not already in the known `kerf next` / `br`
> feed (no existing priority to execute — a never-before-seen body of work)."*
> (`SKILL.md:494-495`)

**Why they collide in "drained queue + ready parked lane + operator away":** The two
directives partition the world into "KNOWN feed → autonomous" and "brand-NEW → ask."
But a **PARKED** lane is neither cleanly: it WAS ranked (so it's KNOWN), yet at the
instant the queue drains it is not *currently* in the live `kerf next` feed (a parked
lane with no ready beads drops out of the feed — see STARTUP.md:250 LAZY-BOOT and
SKILL.md §0.2). So when the program finished and the parked token-opt lane became
the highest-value resumable work, the captain faced a lane that:
- is in the durable registry (`admiral-initiatives.md:24` "ACTIVE — but UNSTAFFED",
  `captain-lanes.md` PARKED list) → reads as KNOWN, and
- is NOT in the current live `kerf next` output (drained, parked) → reads as
  "no existing priority to execute = brand-NEW per the literal §8 wording."

Nothing in §0 or §8 says which reading wins. The `admiral-initiatives.md` status
vocabulary (`:14` "PARKED (deliberately held, has a gate)") even reinforces the wrong
reading — a "gate" sounds like an operator gate. So both roles defaulted to the safe
(but wrong) "ask the operator" branch and idled.

**Closest existing language — and why it's still ambiguous:** STARTUP.md:228-231
comes nearest:
> *"kerf is the priority source of truth — and **executing that existing ranking is
> AUTONOMOUS** … You surface-and-await ONLY to rank a brand-NEW initiative that has
> no existing `kerf next` priority (§8)."* (`STARTUP.md:228-231`)

This still keys the test on "has no existing `kerf next` priority" — which a
freshly-drained parked lane technically *fails* at that instant, because it's not in
the live feed. The test should key on "**was this lane ever ranked / is it in the
durable registry**," not "is it in the live feed right now."

**Proposed reconciliation** — add a third, explicit bullet to the AUTONOMOUS set in
captain SKILL.md §0 (after line 79) AND mirror it in STARTUP.md Step 4:

> - **RESUMING A PARKED / DRAINED / PREVIOUSLY-RANKED KNOWN LANE IS AUTONOMOUS — it
>   is NOT "ranking a brand-new initiative."** A lane that appears in
>   `captain-lanes.md`, `admiral-initiatives.md`, a prior HANDOFF, or any past
>   `kerf next` is a KNOWN lane *even if it is currently parked or has zero ready
>   beads in the live feed this instant.* When a higher-priority program finishes and
>   a KNOWN parked lane becomes the next-ranked resumable work, **un-park and staff it
>   without asking** — that is "execute the existing ranking," not §8. The §8
>   "brand-NEW initiative" case fires ONLY for a **never-before-seen** body of work
>   that has NEVER been ranked, registered, or named in any durable doc. The test is
>   "was it ever ranked / registered," NOT "is it in the live feed right now."

And tighten §8 case 1 (`SKILL.md:494`):
> *"1. Ranking a brand-NEW initiative **that has never appeared in any durable doc
> (`captain-lanes.md` / `admiral-initiatives.md` / a prior HANDOFF / any past
> `kerf next`)** — a never-before-seen body of work. (A PARKED or drained lane that
> was previously ranked is KNOWN — resuming it is AUTONOMOUS, not this case.)"*

---

### 2. The expired 2026-06-19 scale-out directive vs the never-defined "operator away → lean/park" posture

**Directive A — the STALE, EXPIRED scale-out block (captain-lanes.md:172-189):**
> *"set: 2026-06-19 · expires: ~2026-06-22 (3-day scale-out push — re-confirm or
> expire after the window) … STANDING for EVERY captain across ALL restarts within
> the window. These OVERRIDE any stale 'lean park / one-at-a-time / operator away'
> posture in a handoff. On conflict, THESE win."* (`captain-lanes.md:172-176`)
> *"ONE-AT-A-TIME IS RETIRED. Run multiple lanes/crews in parallel (file-disjoint)."*
> (`:182`)

**Directive B — the "operator away → stay lean" posture this block is OVERRIDING**,
referenced by the admiral playbook's standing operator-knob list:
> *"lean-park LIFTED (scale-out window)"* (`admiral-playbook.md:62`) — i.e. the
> playbook tracks "lean-park" as a real, toggleable operator posture that is currently
> only *lifted because the scale-out window says so.*

**Why they collide today:** Today is **2026-06-25**. The scale-out window
**expired ~2026-06-22** (`captain-lanes.md:173`) — three days ago. Its own text says
"re-confirm or expire after the window." Nobody re-confirmed it; nobody expired it.
So the document is in an undefined state:
- If the scale-out block is **dead** (expired), the posture it was suppressing —
  "operator away → lean-park" — silently reactivates, and the captain reverts to a
  HOLD posture exactly when the operator is asleep (this session: "operator settled a
  new strategy (asleep now)" — `captain-lanes.md:13`). That HOLD posture is *itself*
  never defined anywhere as a normative rule — it lives only as a thing the
  now-expired block claims to override.
- If the scale-out block is **still live** (nobody expired it), then "fill every
  non-conflicting lane" is in force and the stall was a violation.

The captain cannot tell which, because the only instruction about the expiry is
"re-confirm or expire" — an action assigned to no one, with no default. In the
"operator away" branch, the ambiguity resolves toward HOLD (the conservative read),
which is exactly the stall.

**Compounding:** `feedback_captain_lean_while_operator_away` (a standing memory note)
says *"away + only low-pri work → keep fleet LEAN, don't auto-expand crews."* That memory
directly contradicts SKILL.md §0's "fill every non-conflicting free slot" and is the
unwritten "lean-park" posture the expired block was holding back. With the block
expired, this memory wins by default — and it pushes HOLD.

**Proposed reconciliation:**
1. **Give expiry a default and an owner.** Replace `captain-lanes.md:172-173` header:
   > *"set: 2026-06-19 · expires: 2026-06-22 · **ON EXPIRY (DEFAULT): these directives
   > LAPSE and the captain reverts to the standing autonomous posture in
   > orchestrator-rules + captain SKILL.md §0 — NOT to any 'lean-park' hold. The
   > admiral's hourly audit (admiral.md Hourly-audit C4) MUST flag an expired-but-still-
   > present directive block and either re-confirm with the operator or strike it.**"*
2. **Resolve the lean-vs-fill contradiction explicitly.** Because the expired block,
   SKILL.md §0, and the `feedback_captain_lean_while_operator_away` memory genuinely
   conflict, state the resolution once in SKILL.md §0:
   > *"'Operator away' is NOT a HOLD trigger. Away + ready KNOWN work = staff it
   > (autonomous). 'Lean' means do not SPIN UP NEW crews speculatively for empty-backlog
   > lanes — it does NOT mean leave ready, already-ranked work unstaffed. The only away-
   > sensitive restraint is: prefer re-tasking an existing crew over booting a new one."*

---

### 3. Admiral "advisory-but-authoritative, MUST escalate ranking" vs admiral "keep the operator's what's-next map current and direct the captain to staff it"

**Directive A — admiral MUST escalate, never decides ranking (admiral.md:130-131; reinforced by watch SKILL.md:121-122):**
> *"**Objective-level ambiguity** (a genuine §8 question only the operator can settle)
> → escalate to operator with concrete options + each option's consequence. Then
> STOP."* (`admiral.md:130-131`)
> watch parallel: *"**New-initiative ranking** — work not already in `kerf next`
> ranking. Flag 'ready work + free slot exists'; the captain picks which crew + which
> epic."* (`watch SKILL.md:121-122`)

**Directive B — admiral OWNS the registry of what's-active/on-deck and DIRECTS the captain to staff drift (admiral.md:60-72, 122-128; playbook Rule 0):**
> *"Each audit, RECONCILE the registry against ground truth … if a major initiative
> is live in comms but NOT in the captain's durable lane doc, direct the captain to
> mirror it there."* (`admiral.md:68-71`)
> *"**Lane/priority drift** → `comms send --from admiral --to captain --topic directive`
> … Name the exact lane and the exact change."* (`admiral.md:126-128`)
> *"Is the captain idling with ready work AND a free crew/queue slot (missed
> staffing)?"* (`admiral.md:115`, audit question C3)

**Why they collide in the stall situation:** The admiral's own audit question C3
(`admiral.md:115`) detects exactly the stall — "captain idling with ready work AND a
free slot." Directive B says: when you detect lane/priority drift, **DIRECT the
captain** with the exact lane and change. But the parked token-opt lane is precisely
the ambiguous "parked-known vs brand-new" case from Conflict 1. So the admiral hits a
fork:
- read it as **drift** → Directive B → "direct captain to staff the wake-economy lane
  now" (the correct, un-sticking move); or
- read it as **new-initiative ranking** → Directive A / watch:121 → "escalate to
  operator, then STOP" (the stall).

`admiral-initiatives.md:24` shows the admiral chose neither cleanly: it logged
*"Directed captain 3× to re-staff (01:11/01:55/02:05). Escalate to operator if still
idle next audit."* — i.e. it issued directives (B) but then queued an escalation (A)
and, with the operator asleep, the escalation could not resolve. The admiral was
authoritative enough to direct 3× but not authoritative enough to treat continued
non-staffing of a KNOWN parked lane as a captain ERROR it could keep pressing — because
the instructions leave open that the lane might be the operator-only class.

**Proposed reconciliation** — add to admiral.md (after the audit-question block, ~:120)
and mirror in watch SKILL.md §Boundary:
> *"**A PARKED / drained lane that was previously ranked is a KNOWN lane.** Directing
> the captain to RESUME a known parked lane is in-scope drift-correction (advisory-
> but-authoritative) — it is NOT 'new-initiative ranking' and does NOT require operator
> escalation. Escalate to the operator ONLY for a never-before-ranked body of work.
> If the captain leaves a KNOWN ready lane unstaffed across TWO consecutive audits with
> a free slot, that is a captain ERROR (missed staffing per C3) — keep directing, and
> escalate as 'captain not honoring autonomous-staffing duty,' NOT as 'please rank this
> for me.'"*

---

### 4. "Keep moving / don't ask shall-I-continue" (global) vs "surface-and-await + STOP" (captain/admiral) with no precedence statement for the ambiguous case

**Directive A — global Keep-moving (~/.claude/CLAUDE.md:24-30, 26):**
> *"**Don't stop to ask 'should I continue?'** — when you have a task list, an open
> queue, or a clearly-scoped brief, the answer is yes. Pause only on a genuine blocker
> the user alone can decide."* (`CLAUDE.md:26`)
> *"**Informational research output** … → synthesize and continue dispatching. Only
> pause when the output explicitly surfaces a decision the user must make."*
> (`CLAUDE.md:30`)

**Directive B — global Threshold for check-in (~/.claude/CLAUDE.md:32-35):**
> *"**Check in first:** new normative contract, renaming a published field/API,
> **reversing a previously locked decision**, anything destructive at the repo/infra
> level."* (`CLAUDE.md:35`)

**Why they collide:** The global file's two adjacent sections give opposite defaults
and the project layer never reconciles them for the parked-lane case. "Keep moving"
says the default is GO unless it's a "genuine blocker the user alone can decide."
"Check in first" lists the blockers — but **resuming a parked lane is on NEITHER
list**: it is not a new normative contract, not a locked-decision reversal, not
destructive. By the global file's own logic that means GO (Keep moving wins). But the
project captain skill's §8 re-frames "ranking" as a check-in trigger (Conflict 1), and
the captain has no precedence rule telling it that the global "Keep moving" default
should win when the project §8 case is genuinely ambiguous. AGENTS.md:11 sets
"orchestrator-rules > AGENTS.md prose > per-domain skills" but says nothing about
global-vs-project for the keep-moving/check-in axis, and CLAUDE.md:58 says "project
file wins" — which hands the tie to the *more conservative* project §8 reading.

**Proposed reconciliation** — add a one-liner to orchestrator-rules §"Autonomy and
flow" (after `SKILL.md:114` "ACTIVE DISPATCH — DON'T PARK THE STREAM"):
> *"**RESUMING KNOWN WORK IS 'KEEP MOVING,' NOT 'CHECK IN.'** The global check-in
> triggers (new normative contract, locked-decision reversal, destructive op) do NOT
> include 'resume a parked/known lane' or 'staff ready ranked work.' When a finished
> program leaves a KNOWN lane as the next work, the default is GO. Only a NEVER-RANKED
> brand-new initiative reaches the check-in gate. If you are unsure whether a lane is
> 'known' or 'brand-new,' it is KNOWN if it appears in any durable doc — staff it."*

---

### 5. Watch "escalate, never decides" + "epic_completed is LEDGER-ONLY" vs the need for SOMETHING to re-staff on program completion

**Directive A — watch is escalate-only and LEDGER-ONLY on completion (watch SKILL.md:103-105, 119-127):**
> *"**`epic_completed` is LEDGER-ONLY at the watch.** … Record it; do not escalate
> it."* (`watch SKILL.md:105`)
> *"**Staffing** — which crew handles which epic. The watch may *flag* staffing
> readiness; the captain decides."* (`watch SKILL.md:124`)

**Directive B — the captain is supposed to auto-re-task on epic completion (captain SKILL.md:437-445):**
> *"**Re-task the now-free crew to the next-ranked KNOWN lane — AUTONOMOUS (§0).** …
> Only SURFACE + AWAIT when the next lane would be a brand-NEW initiative not in the
> known feed."* (`SKILL.md:437-445`)

**Why they collide in the stall:** On program completion, the *only* component that
auto-fires a re-staff is the captain's §5 epic_completed handler. But the watch (the
escalation tier between the bus and the captain) deliberately does NOT escalate
`epic_completed` (it's LEDGER-ONLY, to avoid triple-wake — `watch:105`). And the
backlog-ready / staffing flag is **PULL-DIGEST, no wake** (`watch:102`,
`watch SKILL.md:102`: *"backlog-ready + free slot (staffing flag) … never timed-send"*).
So when the program completes and a parked lane becomes ready, the signal that should
prompt re-staffing arrives as a **non-waking pull-digest** the captain only reads on
its own idle. If the captain has already (wrongly, per Conflict 1) classified the
resume as "await operator" and gone quiet, there is no waking event to correct it —
the watch is contractually forbidden from escalating the one thing (staffing
readiness) that would un-stick it. The architecture routes the un-sticking signal to
the lowest-priority, no-wake channel precisely in the scenario where the captain is
idle.

**Proposed reconciliation** — narrow the LEDGER-ONLY rule so program-scale completion
that *frees a slot with ready KNOWN backlog* becomes a waking PULL-DIGEST-promoted
escalation. Edit `watch SKILL.md:102` PULL-DIGEST row and add a carve-out near :105:
> *"**Carve-out — completion-with-idle-slot:** when `epic_completed` (or a program
> drain) leaves a free crew/queue slot AND `br ready --limit 0` shows ready beads in a
> KNOWN (previously-ranked) lane, the staffing flag is promoted from no-wake PULL-DIGEST
> to an **IMMEDIATE escalation** ('program X done; KNOWN lane Y has ready work + a free
> slot; captain to re-staff'). This is still flag-not-decide — the captain picks the
> crew — but it WAKES the captain so an idle captain cannot silently sit on resumable
> known work. (Routine `epic_completed` with no idle slot stays LEDGER-ONLY.)"*

---

## The single highest-leverage fix

**Define the parked-known vs brand-new boundary once, as a shared normative sentence,
and key the test on "was it ever ranked / registered" rather than "is it in the live
feed right now."** Conflicts 1, 3, 4, and 5 are all the same missing distinction
surfacing in four skills; Conflict 2 is the expired-directive accident that tips the
ambiguity toward HOLD. The one edit that dissolves the stall is adding to captain
SKILL.md §0 (and mirroring verbatim into STARTUP.md Step 4, admiral.md, watch
SKILL.md, and orchestrator-rules §Autonomy): *"A lane that appears in any durable doc
(`captain-lanes.md`, `admiral-initiatives.md`, a prior HANDOFF, or any past
`kerf next`) is a KNOWN lane — resuming, un-parking, or re-staffing it is AUTONOMOUS
'execute the existing ranking,' NOT the §8 'rank a brand-new initiative' case, even
when it is currently parked or shows zero ready beads in the live feed this instant.
§8 fires ONLY for a never-before-ranked body of work."* That sentence makes the
correct classification mechanical at every altitude (captain, admiral, watch) and
removes the conservative "ask the operator" default that, combined with the operator
being asleep and the expired scale-out block lapsing into a silent lean-park, produced
the two-hour idle.
