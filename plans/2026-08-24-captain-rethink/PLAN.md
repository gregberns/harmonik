# Plan: Captain rethink — fewer instructions, a smaller boot, a clearer job

## Objective

Rebuild the captain's instruction corpus down to four short pages — rules, principles,
pointers, commands — cut what a captain starts with, and put a gate in place so it does
not grow back, as it has after every previous cut.

## Status

active — Step 1a landed 2026-08-24

## The problem, measured live

**A captain started at about 110,000 tokens. Step 1a took that to about 74,000.** Those are
measured from live session transcripts, not summed from disk — the first attempt at this
number summed file sizes and divided bytes by four, and was wrong by half in both halves.
Working detail in [`01-inventory.md`](01-inventory.md) §2.

| Source | Tokens | Kind |
|---|---:|---|
| **Harness floor, before any harmonik file is read** | **39,000** | untouchable here |
| — of which tool schemas | 21,000 | harness config, not writing |
| — of which `AGENTS.md`, injected into every agent | 8,800 | instruction |
| — of which skill descriptions and memory index | 5,300 | harness |
| — of which base system prompt | 3,600 | harness |
| `.claude/skills/captain/STARTUP.md` | 8,729 | instruction |
| `.claude/skills/captain/SKILL.md` | 8,323 | instruction |
| The captain's own boot thinking and tool calls | 8,000–19,000 | unavoidable |
| The three context tier files | 3,059 | state |
| `harmonik agent brief` output | 2,757 | generated |
| `orchestrator-rules/SKILL.md` | 2,016 | instruction |
| Boot digest, after Step 1a | ~1,450 | live data |
| *Boot digest, before Step 1a* | *36,848* | *removed* |

Three facts here set what the rest of this plan can honestly claim.

**The single largest item was never instructions.** The boot digest was a third of the whole
boot, and 85 KB of it was a bead listing and a kerf map. Removing them was worth about
35,400 tokens for one section of shell. That is done.

**The harness floor is larger than the entire captain corpus.** 39,000 tokens arrive before
a captain reads one harmonik file. `STARTUP.md`, `SKILL.md` and `orchestrator-rules/SKILL.md`
together are about 19,000. **So the honest ceiling for Steps 2 and 3 is roughly 14,000 to
19,000 tokens** — deleting the entire corpus saves 19,000, and cutting it to four pages saves
about 14,000. Worth doing. Not where the remaining bulk is, and this plan says so rather than
implying otherwise.

**The biggest untouched number is not a writing problem.** 21,000 tokens of tool-schema text
reaches every agent in this project. That is more than the whole captain corpus, nobody has
looked at it, and it is a harness-configuration question. Out of scope here, and it should not
stay out of scope for long.

So the case for Steps 2 and 3 rests on the second cost, not the first.

**Steerability.** An operator directive is one paragraph arguing against ninety kilobytes of
standing text. That is why the captain has been hard to steer: the corpus outvotes the
operator. Cutting the corpus is what gives a directive weight — and that argument does not
depend on the token count at all.

## What the record says, and what this plan must not repeat

Five previous efforts have aimed at this. The detail is in
[`01-inventory.md`](01-inventory.md) §7; the conclusions that change how this plan is built:

**1. Cutting works. Cutting alone does not last.** The 2026-08-23 pass took the three captain
files from 120,744 bytes to 41,959. One day later they were at 43,811 — about 3.5 % of the cut
recovered per day, which puts the corpus back at pre-cut size inside a month. **A deletion
pass with no size gate buys a month, and this plan does not get to repeat it.** The gate is
built before the cutting starts.

**2. Consolidation is not reduction.** The June economy plan's own deliverable, a commit
titled "lean boot + de-conflict captain skill set", *added* 5,793 bytes to the captain files
by moving duplicated text into them. Moving text toward the reader is the opposite of the goal
even when it removes a duplicate.

**3. "Principles, not laws" is itself an accretion driver, and that is the tension in the
operator's third ask.** The largest single addition after the original runbook was +13,707
bytes, and it was the commit that turned rules into principles. Its own body says why: rules
"kept their force *and gained their reason*." A rule with its reason attached is three to five
times the length of the rule. **So the principles page cannot be every current rule restated
with its justification.** A principle earns its place by replacing several rules, not by
annotating one. Step 3 has to hold that line or it will produce more text than it deletes.

**4. Every incident-driven addition is multiplied by three,** because a shipped skill lives in
three places. One stopped-queue incident added about 1,100 bytes of mechanism and became 2,807
bytes across the mirror. Collapsing the mirror is worth more than any single deletion, because
it divides all future growth by three.

**5. The largest instruction-side reduction is a flip nobody made.** `harmonik agent brief`
was built in July to be the one generated boot document. The router flip — making `brief` the
boot path instead of one more thing to read alongside `STARTUP.md` — was called "the point of
no return" and never happened. Two boot models are live at once today. The July estimate put
its first stage at about 35 K tokens; the measurement above says the honest figure is nearer
11 K, because what the flip stops the captain reading is `STARTUP.md` plus `SKILL.md`. Still
the largest instruction-side saving available, and it needs no judgement about which lines
matter.

**6. The diagnosis of record is an enforcement gap, not a rule gap.** From the July synthesis:
"the anti-rot rules exist … but nothing mechanical enforces them. Enforcement gap, not rule gap
— the fix is a checkable gate, not another rule." So the anti-accretion answer is a script that
fails, not a paragraph that asks.

**7. The reason nobody deletes, named in the same document:** "The compaction's failure mode is
deleting a hard-won rule that exists in only one place." That fear produced the current corpus.
Step 2's method is built against it — cut blind in parallel, and let agreement rather than
nerve decide.

### Settled — do not re-open

`plans/2026-07-11-captain-startup-revamp/03-operator-decisions.md` holds twelve operator
rulings, listed in [`01-inventory.md`](01-inventory.md) §7. They stand, and this plan spends no
time on them. One is still open in the operator's own words — whether an always-on watch earns
its keep — and it stays open here.

---

## The target shape

Four pages, and nothing else at boot.

| Page | What it holds | Budget |
|---|---|---|
| **RULES** | Hard, mechanical constraints only. Safety and irreversibility. Each guards a failure judgement cannot. | ~1 page |
| **PRINCIPLES** | Directions to travel. Each names an intent and a tiebreaker, then trusts the agent. Each replaces two or more rules. | ~1 page |
| **POINTERS** | Where the detail lives, and the one line of judgement needed to use the pointer. | ~1 page |
| **COMMANDS** | 12–24 real invocations, so the captain never reads to remember a flag. | ~25 lines |

Everything the four pages do not hold moves behind a pointer, or goes.

## The cutting rule

**Default to delete.** A line survives only when someone can name the failure it prevents
*and* that failure is not already prevented by a shorter line inside the four pages.
"It might be useful" is not a reason. "It was expensive to learn" is not a reason — write it
in a document and point at the document.

## How the steps are ordered, and why

**Cut first. Decide what the captain is for afterwards, over what survived.**

The obvious alternative is to settle the captain's job first and use it as the knife. It is
the wrong order here, for a plain reason: nobody should have to hold fifty kilobytes of text
in their head to have that conversation. Most of what is in the corpus is not a live question
about the captain's job at all — it is duplication, incident scars, and runbook mechanics, and
it can go without anyone deciding anything. The charter conversation is worth having over five
kilobytes, and it is nearly impossible over fifty.

So: mechanical wins, then the cut, then refinement with the operator — and the "what is the
captain responsible for" question is settled inside that refinement, where its answers land
directly in the four pages.

The same argument applies to the size gate. The record says a cut with no gate decays within
a month, and that is still true — but the gate's budget number should come from what the cut
actually produces, not from a guess made beforehand. The gate is Step 6, and **this plan is
not done until it exists**.

---

## Step 1 — Inventory and measure

**Owner: this session and sub-agents. Complete.**

**Deliverable:** [`01-inventory.md`](01-inventory.md) — the surface table, the measured boot
budget, the 25 duplicated rules, the three-copy state, the stale-instruction findings, and
what five previous efforts found.

---

## Step 1a — Take the mechanical wins now

**Owner: this session. No judgement calls. LANDED 2026-08-24** in commits
`2e4e5b5a6` (digest), `1b1462bbf` (tier files), `2e765cf79` (mirror gate).

| Item | Result |
|---|---|
| Work discovery out of the boot digest | 90,647 → ~3,800 bytes. **~35,400 tokens off every boot**, and off every keeper restart, which re-pays the boot cost each time. Measured at the real 2.5 bytes/token, not the 4 first assumed. |
| The standing `--limit 0` rule deleted | Gone from `AGENTS.md`, `STATUS.md`, both `AGENTS.template.md` copies, the scaffold index, and three sites in the captain runbook. The *tool fact* is kept, stated once, owned by `beads-cli`. |
| Stale tier files | `captain-lanes.md` 3,418 → 2,635. `direction-log.md` 7,676 → 2,007. `lanes.json` re-seeded. `captain-monitor.md` deleted. |
| The 76 KB pointer | The five-item "durable docs" enumeration is gone from the orchestrator reference and the captain skill. `admiral-initiatives.md` is still indexed for a captain elsewhere. |
| Mirror gate | `scripts/skill-mirror-check.sh` in `gate-static-product`, with an eight-case self-test wired into `script-tests`. |

**The true baseline is now measured, and it moved the target.** Live-transcript figures
replaced the disk sum: ~110,000 before Step 1a, ~74,000 after. See `01-inventory.md` §2.
The correction was worth making before Step 2 rather than after — it says the corpus cut is
worth 14,000 to 19,000 tokens rather than the larger number the disk sum implied, and it
found 21,000 tokens of tool schemas that no amount of editing prose will reach.

### What Step 1a proved, and it changes how Step 2 should be run

**Most of the cost was not instructions.** Nearly all of the roughly 38,600 tokens removed
so far came out of one shell script's output and four stale state files. The corpus itself
has barely been touched, and the measurement says what is left in it is worth 14,000 to
19,000. Do not assume the remaining savings live where the prose is — twice now they have
not.

**Some rules exist because a tool lies.** "A queue paused by failure needs recover, not
resume" was written into six separate instruction files because the digest printed the
wrong verb. One tool fix retires all six. **Before deleting a rule in Step 2, ask what
would have to become true for nobody to need it** — that is often a one-line fix
somewhere else, and it is worth more than the deletion.

**Some instructions are not merely stale, they are wrong.** Two of the three tier files
described a frozen fleet that had been dispatching for weeks, and `STATUS.md` told every
reader the daemon was intentionally down. A cut that only shortens leaves this class
untouched. Read for TRUTH as well as for length.

**A closed list is the trap, not its longest entry.** The pointer that cost a reader
nineteen thousand tokens was one item in a five-item illustration of a rule that did not
need illustrating.

**Four review rounds found nine defects, four of them in the change's own stated goals.**
Including one where the fix printed a false all-clear on the section a captain uses to
decide whether it is safe to dispatch. Step 2 is larger than Step 1a and touches judgement
rather than mechanism. **Budget for adversarial review at the same ratio, and expect the
reviewer to find things in what you just fixed.**

---

## Step 2 — The cut

**Owner: this session with a sub-agent fan-out. The long step. Do not rush it.**

The scope is the captain corpus, and the default is delete.

1. **Build a disposition ledger.** Every assertion gets a row: the claim in a few words, its
   source file, and a disposition of RULE, PRINCIPLE, POINTER, COMMAND, MOVE-TO-DOC, or
   DELETE. Nothing is exempt from getting a row.
2. **Cut blind, then reconcile.** Three sub-agents cut the same file independently under the
   same rule. What all three delete goes without a conversation. What they disagree on becomes
   the operator's list for Step 3. This is the counter to the timidity the record names as the
   reason nobody deletes.
3. **Collapse the scar clusters.** Find every passage justified by an incident — "this has
   actually happened", "cost real time more than once", a named past failure. Each cluster
   becomes at most one line; the incident detail moves to a document or goes. Expect the
   largest single reduction here.
4. **Delete every duplicate.** The Step 1 map names **25 rules that exist in two or more
   places**, several in five to ten, worth an estimated 12–15 KB of second and third copies
   inside the 66 KB the captain deliberately loads. Keep the copy in the file whose job it is
   and delete the rest — deleting, not relocating. Note that each captain file already carries
   a line claiming it holds no second copy: those lines are accurate about ownership and wrong
   about text, which is exactly how the duplication survived a previous audit.
5. **Demote the runbooks.** Boot, shutdown, deploy, and crew recovery are procedures, not
   standing instruction. A procedure belongs behind a pointer or inside a script that prints
   the next action. Ask of `STARTUP.md` specifically: how much of its 22 KB is a script's job
   rather than a reader's?
6. **Delete anything that prescribes how the captain finds work.** Ranking sources, sort
   orders, which query to run, which list is authoritative — all of it belongs in the mission,
   because all of it changes. The corpus says how to coordinate. The mission says what to work
   on and how to find it. Treat every "then run this listing and take the top item" passage as
   a delete.
7. **Leave the hard questions marked, not answered.** Where a cut turns on whether the captain
   plans, investigates, writes code, or owns deploys, mark the line and move on. Those are
   Step 3's, and they will be easier to answer with everything else gone.

**Deliverable:** `02-disposition.md`, the full ledger, plus draft cut files.

**Acceptance:** the always-loaded corpus is under a quarter of the Step 1a baseline. A draft
over budget means the cut was timid, not that the budget was wrong.

---

## Step 3 — Refine what survived, with the operator

**Owner: operator and this session, working closely. This is the step that needs a person.**

By now the corpus is small enough to hold in one conversation. Two things happen here.

### 3a — Settle what the captain is responsible for

The answers land directly in the four pages, and each one deletes or keeps a specific block
that Step 2 marked:

1. **Does the captain plan?** `STARTUP.md` Step 4 says "write the plan before you dispatch"
   and hands over a lane table. If planning moves to the admiral or a planning crew, that step
   and its supporting prose leave entirely.
2. **Does the captain investigate?** Today it reads enough to route and is told where to stop.
   If investigation is always delegated, the "read to route" body collapses to one line.
3. **Is steering through mission files and handoffs the captain's main instrument?** The
   proposal on the table is yes — the captain's product is that what the crews are working on
   matches what the operator wants, and the mission file is where that gets written down. If
   so, mission authoring gets *more* space and crew mechanics get less, and the mission format
   becomes a deliverable of Step 7.
4. **Does the captain write code?** Today: a one-line obvious fix when no crew can take it.
   Keep, tighten, or forbid.
5. **Does the captain merge, deploy, and restart the daemon?** Today yes, on its own
   authority, with 13 KB of mechanics behind it. Decide whether that is the job or a runbook
   the job calls.

### 3b — Write the four pages

Take the Step 2 drafts and the disagreement list, and settle each contested line in person.
The default in the room is still delete.

**The drafting standard, and it is the answer to how "principles not rules" went wrong.** The
previous conversion added 13.7 KB because every rule kept its force *and gained its reason*.
Reasons are what made it long. So:

- **A principle is one sentence.** It states a direction and, where two directions conflict, a
  tiebreaker. Nothing else.
- **The reason does not live in the file.** It goes in the commit body, or the document the
  pointer names. An agent that needs the reason can find it; an agent reading the page needs
  the direction.
- **No worked examples, no incident stories, no "this has happened" clauses.** Those are the
  scar tissue by another name.
- **A principle earns its place by replacing two or more rules.** One rule plus its
  justification is not a principle, it is a longer rule.
- **If the principles page comes out longer than the rules it replaced, the step failed** even
  if every sentence is good.

Then decide where the four pages live: inside the captain skill, in `roles/captain/`, or
emitted by `harmonik agent brief`. Step 4 argues for the third.

**Deliverable:** `00-charter.md` (the five answers) and the four pages, with the files they
replace deleted rather than stubbed.

---

## Step 4 — The router flip

**Owner: this session. Independent of Steps 2 and 3, and needs no judgement about which lines
matter.**

Two boot models are live at once. `harmonik agent brief` was built in July to be the one
generated boot document; `AGENTS.md` and `roles/captain/operating.md` still tell a captain to
read `STARTUP.md` first. Nobody made the flip.

1. **Make `brief` the boot path.** It emits the four pages inline and the pointer index, and
   nothing tells the captain to go read a runbook first. A pointer the agent always follows is
   only a slower inline.
2. **Move "check X, then do Y" into the digest.** Any instruction of that shape is a candidate
   for the boot digest printing the answer and the next action instead of the captain reading
   how to work it out.
3. **Retire the second model.** The old boot prose is deleted, not left as an alternative. The
   record's warning applies: one surviving line that re-arms the old path silently decays the
   change.
4. **Re-measure** against the Step 1a baseline.

**Deliverable:** `04-boot.md` — before and after token counts, plus the code and script
changes it names.

---

## Step 5 — Re-cut `AGENTS.md`

**Owner: this session, operator confirms.**

23 KB, injected into *every* agent in the project — the most expensive document here. Its own
text says it is a router and asks to be judged by that test, and it fails: it carries the
three-copy skill rule, the beads traps, the commit-trailer policy, and the harness-selection
tiers in full.

Same cutting rule. A router line is a path plus the one sentence a reader needs to decide
whether to follow it. Everything else moves to the file it points at. Note that Step 1a
removes the reason for two of its longest passages.

**Deliverable:** a cut `AGENTS.md` under a third of current size, with every removed claim
landed in the document that owns it.

---

## Step 6 — The crew pass

**Owner: after Steps 1a–5 land. Same method.**

`crew-launch/SKILL.md` is 13 KB, and the mission files total 210 KB with one file at 38 KB. If
Step 3a decides that steering through mission files is the captain's main instrument, the
mission *format* is a deliverable here and not an afterthought — a 38 KB mission is a mission
nobody can steer with.

**Deliverable:** the crew's four pages, and a mission template with a size budget.

---

## Step 7 — Verify with a live captain

**Owner: this session, assessor-style.**

A smaller corpus that produces a worse captain is a failure. Prove it does not.

1. Boot a real captain cold on the new corpus. Read the starting token count out of the live
   session.
2. Watch it reach its first correct dispatch. Record everything it had to go read that the
   four pages did not give it. Each item is either a missing line or a working pointer, and the
   difference matters.
3. Run one full cycle: staff a lane, react to a failure, hand off.
**Deliverable:** `07-verification.md` — boot cost, the look-up list, the verdict, and the
two-week growth figure.

---

## Step 8 — Governance: stop it growing back

**Owner: this session, operator decides which levers ship. The plan is not done without this.**

The record is unambiguous — five cuts, five regrowths, and the diagnosis of record is
"enforcement gap, not rule gap — the fix is a checkable gate, not another rule." But a gate
that is itself a large piece of software is a new maintenance burden that will break and drift.
So the bias is: **cheapest mechanism that actually fires, and prefer reusing something that
already exists over writing something new.**

Options to weigh, roughly in order of cost:

| Lever | Cost | Fires when | Notes |
|---|---|---|---|
| **A size cap per file**, checked by a short script in `make full` | ~20 lines, near-zero upkeep | every full test run | Needs a number per file. Straightforward, hard to game, easy to delete if it annoys. |
| **The trade rule** — adding a line names the line it replaces | zero code | at authoring time | Social, so it decays. Worth writing down, not worth relying on alone. |
| **Extend `agent-config-reviewer`** to reject net growth in the instruction corpus | zero new code — the reviewer already exists and already fires at session boundaries and configuration drift | session start and end | Probably the best value here. It is already the thing that watches these files. |
| **A cap on every skill, not just the captain's** | one number in the same size check | every full test run | The operator's point: a 21 KB skill is a defect regardless of who reads it. Set a ceiling and let it force the split. |
| **A home for scars that is not the corpus** | zero code | at incident time | An incident produces a document or a tracked issue. It produces an instruction line only when the trade above is paid. |
| **Write hooks that block edits to these files** | high, and it grows | at write time | **Considered and set aside.** It is a lot of new code to maintain, it will break, and blocking a write is a blunt answer to a judgement problem. Record the reasoning so it is not re-proposed. |

Whatever ships, the budget number goes at the top of each of the four pages where an author
will see it, and the check reports the current size against it.

**The two-week check.** Once the levers ship, re-measure the corpus two weeks later. Growth
since the cut is the only number that says whether any of this worked.

**Deliverable:** `08-governance.md` — the levers chosen, the numbers, and the reasoning for the
ones rejected.

---

## Done means

1. The true starting context of a live captain is recorded, work discovery is out of the boot
   digest and out of `AGENTS.md`, and the stale tier files are fixed or gone. Verified by a
   fresh digest byte count against the 90,059-byte baseline.
2. A captain's always-loaded corpus is four pages, and its measured cold-boot cost is at most a
   quarter of the Step 1a baseline. Verified by the numbers in `04-boot.md` and
   `07-verification.md`.
3. `RULES.md`, `PRINCIPLES.md`, `POINTERS.md`, `COMMANDS.md` exist, each inside its budget, and
   the files they replace are deleted, not stubbed. Verified by `git status` deletions and by
   the size check.
4. No skill in the project exceeds its size cap. Verified by the size check across
   `.claude/skills/`.
5. The captain's job and non-jobs are one page, with a recorded answer to each of the five
   Step 3a questions. Verified by `00-charter.md`.
6. `harmonik agent brief` is the only boot path. No live file tells a captain to read a boot
   runbook first. Verified by grepping for the retired instruction and finding nothing.
7. A live captain boots on the new corpus and reaches a correct first dispatch without reading
   anything outside the four pages and its pointers. Verified by the Step 7 run.
8. `AGENTS.md` is under a third of its current size and every removed claim has landed in the
   document that owns it. Verified by a byte count and by following each moved claim.
9. The size check fails on a deliberately padded page, and the mirror check fails on a
   deliberately diverged skill copy. Verified by running both against a planted violation.
10. Nothing in the corpus prescribes how the captain finds work. Verified by reading the four
    pages: ranking sources and listing commands appear in the mission template, not in them.
11. Two weeks after the cut, the corpus has grown by less than 2 %. Verified by the size check's
    recorded history. **This is the acceptance the previous five efforts failed.**

## Out of scope

- The admiral, assessor, and watch roles. The method should transfer; proving it on the captain
  comes first.
- Whether an always-on watch earns its keep. The operator left it open in July and it stays
  open — a fleet-design question, not an instruction-size one.
- `.memory/` — 238 files, 660 KB, and no code reads it. Not a context cost today, so not this
  plan's problem, but it deserves a separate operator decision.

## References

- Current corpus: `.claude/skills/captain/`, `.claude/skills/orchestrator-rules/`,
  `roles/captain/`, `.harmonik/agents/captain/manifest.yaml`, `.harmonik/context/`.
- The previous cut: commit `94b7c7c96`. The one-day regrowth: commit `c7d858e21`.
- Earlier attempts: `plans/2026-06-20-captain-economy/`,
  `plans/2026-07-11-captain-startup-revamp/` (especially `00-SYNTHESIS.md` and
  `03-operator-decisions.md`), `plans/2026-06-23-captain-wake-economy/`,
  `plans/2026-06-20-doc-instruction-audit/`, `docs/captain-boot-audit-2026-06-16.md`.
- House style this plan is written to: `AGENTS.md` "Write guidance as principles, not laws",
  and the `ste-writing` and `no-jargon` skills.
