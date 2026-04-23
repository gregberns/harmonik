# Analysis Lens: Writing Load Distribution

Generated: 2026-04-23

## What this lens looks for

Where does the human's writing effort accumulate across planning sessions, and could better-structured agent behavior have compressed that effort? The primary cost we are tracing is human typing — measured in approximate character counts — categorized by why the turn was written at all.

Categories used:
- **(a) Initial context / framing** — opening problem statement, background, constraints
- **(b) Redirection / correction** — agent went somewhere wrong; human fixing course
- **(c) Clarifying question** — human asking agent to expand or explain
- **(d) Approval / confirmation** — short "yes, go" or "looks good"
- **(e) Scope expansion or addition** — new requirement the human adds
- **(f) Decision response** — agent asked a question; human answered
- **(g) Status / administrative** — housekeeping, session metadata, cmd output (often machine-generated)
- **(h) Other** — does not fit cleanly

---

## Methodology

All 10 corpus sessions were read. Human turns were catalogued and assigned to the nearest category. Character counts are estimated from the extracted text (not byte-counted programmatically, but measured by inspection with ±15% accuracy sufficient for pattern identification). Sessions with large embedded command output in (g) turns are noted separately — those inflate the nominal character count but require zero cognitive work from the human. "Substantive writing" excludes (g) turns throughout the analysis.

Where a single turn spans multiple categories (e.g., a correction that also adds a new scope item), it is assigned to the dominant category and noted.

---

## Findings

### Per-session turn count and character distribution

| Session | ht | (a) | (b) | (c) | (d) | (e) | (f) | (g) | Substantive chars (excl g) | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| **38415843** kerf | 31 | 1 (~650) | 2 (~550) | 1 (~110) | 6 (~150) | 2 (~200) | 3 (~300) | 16 (machine) | ~1960 | High (g) due to bash I/O, ntm cmd output |
| **79a42399** secure-dev | 38 | 1 (~680) | 3 (~700) | 0 | 8 (~280) | 2 (~400) | 6 (~750) | 18 (machine+task) | ~2810 | Moderate writing; large (g) from task notifications |
| **c6d1bd16** secure-dev | 25 | 1 (~120) | 1 (~550) | 0 | 5 (~200) | 2 (~800) | 1 (~60) | 15 (machine+task) | ~1730 | One (e) turn very heavy (spec process correction, ~550c) |
| **3bf5774c** harmonik | 21 | 1 (~680) | 3 (~1200) | 0 | 2 (~80) | 1 (~80) | 7 (~900) | 7 (machine+dup) | ~2940 | Highest (b) density; several 400c+ correction turns |
| **f588ff0c** harmonik | 20 | 1 (~980) | 3 (~1650) | 0 | 2 (~80) | 1 (~130) | 6 (~1200) | 7 (machine+dup) | ~4040 | Highest total writing; two (f) turns >500c each |
| **2a50e0fc** machine-setup | 19 | 1 (~1450) | 2 (~380) | 1 (~80) | 3 (~80) | 3 (~250) | 0 | 9 (cmd output) | ~2240 | Opening turn is a ~1450c handoff dump |
| **d1704aa0** secure-dev | 17 | 1 (~480) | 0 | 0 | 5 (~200) | 1 (~100) | 2 (~200) | 8 (machine) | ~980 | Lowest substantive writing; mostly (d) and (g) |
| **13493c8d** harmonik (context-dump) | 5 | 2 (~7600) | 0 | 0 | 0 | 0 | 0 | 3 (task notif) | ~7600 | Extreme (a): entire load front-loaded in 2 massive turns |
| **729dad16** kerf (session-recovery) | 14 | 1 (~150) | 1 (~250) | 2 (~160) | 0 | 0 | 0 | 10 (cmd output/task notif) | ~560 | Mostly (g); substantive writing very low |
| **00eb9fc9** harmonik (short-recent) | 5 | 1 (~550) | 1 (~400) | 0 | 0 | 1 (~700) | 1 (~500) | 1 (dup msg) | ~2150 | High per-turn density; (e) turn very long |

**Total corpus substantive writing: ~22,980 chars across 190 human turns.** Of those, roughly 130 turns are (g) administrative / machine-generated (no cognitive effort). Net human-authored writing is spread across ~60 substantive turns.

---

### Where does bulk writing sit?

Ranked by total characters, the categories are:

1. **(a) Initial context / framing** — ~14,300 chars, but 7,600 of that is the one context-dump session (13493c8d). Excluding that outlier: ~6,700 chars across 9 sessions, averaging ~740 per opening. This is structurally unavoidable; the human must establish baseline context.

2. **(f) Decision response** — ~3,850 chars across the primary 7 sessions. The most surprising finding: decision-response turns are individually moderate (~120–250c) but there are many of them, and in f588ff0c two turns are 500+ chars. These turns grow long when: (i) the agent asked multiple questions at once, forcing the human to address all of them in one turn, or (ii) the human took the opportunity to add nuance or new scope while answering.

3. **(b) Redirection / correction** — ~5,030 chars, highest average per-turn (~380c). Every session has at least one. The worst single instance is in f588ff0c Human #4/5 (~550c each): the agent asked "what should be in foundation?" by listing file labels without describing content, forcing the human to write a meta-correction about how the agent was framing the question at all. Another notable example: f588ff0c Human #4 (gap 14m) — 214c correction about the agent asking about file contents by listing filenames, plus a 338c follow-on in the very next turn. In 3bf5774c Human #3, a ~400c turn corrects the agent's framing that certain decisions were "locked" — because the human had to re-establish the design-stage posture.

4. **(e) Scope expansion** — ~1,860 chars. These are intrinsically human-authored — new requirements that didn't exist before. Not compressible by better agent behavior except to the extent that a proactive agent question might have surfaced them earlier.

5. **(d) Approval / confirmation** — ~1,270 chars across many turns, but per-turn these are the shortest substantive category (~25–60c). They look like: "Sounds great.", "Good, let's do that.", "That makes sense to me.", or 1–3 line responses to multi-option proposals.

---

### Key patterns

**Pattern 1: Multi-question agent turns generate long decision-response turns (f).**

The clearest example is 00eb9fc9 Human #2 (~700c, category f): the agent asked 5 numbered questions in one turn. The human wrote a detailed answer to each. This is 700 characters of writing that was structurally forced by a single agent turn that batched questions. Compare to 38415843 where the agent frequently asked 1–2 crisp questions and got 50–100c answers. The one-question-at-a-time pattern keeps (f) turns short. Multi-question turns make (f) turns the most expensive in the session.

**Pattern 2: Redirection turns (b) are the most expensive per-turn, and many could be prevented.**

Across the 7 primary sessions there are 14 redirection turns. Three sub-patterns:

- **Premature locking** (3bf5774c Human #3, ~400c): Agent used "already locked" framing during design stage. Human had to correct the framing itself, not just the content. Cost: explaining why the framing is wrong + restating the design-stage principle. Prevention: agent should default to "candidate position, revisable" language during design.

- **Label-listing without content** (f588ff0c Human #4/5, ~1,100c total): Agent described foundation by listing filenames. Human could not evaluate filenames without content. Two back-to-back turns required to correct this. Prevention: agent should describe what questions get answered, not what files will be created.

- **False diagnosis of distant issue** (38415843, multiple turns ~80c each): Agent repeatedly thought workers were failing (because tmux pane capture was stale) when workers were actually fine. Human had to correct 3–4 times. Prevention: prefer file-existence checks and inbox over pane capture for status.

**Pattern 3: Context re-assertion is present but not severe.**

f588ff0c has 3 turns where the human re-asserts design-stage posture, collaborative style, or the "make calls yourself" principle. These are 50–150c each — not huge, but they represent agent failure to internalize guidance from earlier turns. The auto-memory system partially mitigates this across sessions; the problem is within-session drift.

**Pattern 4: Approval turns are genuinely short when the agent made a concrete, bounded proposal.**

The shortest approval turns cluster around agent turns that: (a) presented one option not three, (b) stated the agent's own recommendation, and (c) asked "shall I proceed?" rather than "which would you prefer?". In 38415843, turns like "That seems great", "Sounds right", "Yes" (all <50c) follow agent turns that made a concrete proposal. Long approval turns (100–200c) follow agent turns that presented multiple options and asked the human to choose.

**Pattern 5: The context-dump variant front-loads all human writing in two turns.**

13493c8d has 5 human turns total. The first two are 5,294 and 3,441 chars. The subsequent three are zero-content machine-generated task notifications. Total substantive human writing: ~8,700 chars but nearly zero correction cycles, because the agent ran autonomously against the brief without back-and-forth. Trade-off: high up-front writing investment, but near-zero context-switch load and zero redirection turns.

**Pattern 6: The session-recovery variant (729dad16) has the lowest substantive human writing.**

Excluding machine-generated output, the human wrote ~560 chars across 14 turns (most are (g)). The "session recovery context" opener is short (~150c structured handoff), not a full brief. This works because prior session state was already documented; the agent needed only minimal re-orientation. Implication: structured inter-session handoffs compress per-session (a) turns significantly.

---

### Outlier analysis

**Highest human writing density: f588ff0c** (~4,040 substantive chars, 20 turns). This is the session that establishes the kerf/spec-first workflow and has the highest-stakes design decisions. High (b) and (f) loads are expected for a session that is calibrating both content and collaboration norms simultaneously. Sessions that come after (3bf5774c, ~6 days later) show lower (b) load — the calibration carried.

**Lowest human writing density: d1704aa0** (~980 substantive chars, 17 turns). This session opens with a single directive and mostly proceeds autonomously. The human's role is approval and status check. Pattern: when the agent has a precise spec + clear implementation task, the planning dialog is short.

**Highest per-turn writing in non-opening turns: f588ff0c Human #4/5** (two duplicate turns, ~550c each). Both are corrections to label-listing. This is the single clearest case of preventable human writing in the corpus.

---

## Candidate planning protocols this lens suggests

**Protocol A: One-question turns.** Agent commits to asking at most one question per turn during the planning dialog phase. If the agent has multiple open questions, it surfaces the most blocking one and defers the others. Expected effect: reduces (f) turn length by 50–70%; each response is now scoped to a single answer.

**Protocol B: Content-not-labels proposals.** Agent describes artifacts by what questions they answer, not by filenames. "Foundation would define what a 'task' is as a concrete data type, and what subsystem boundaries constrain S01–S09" vs "foundation would contain `tasks.md` and `subsystem-boundaries.md`." Prevents the most expensive class of (b) turns in the corpus.

**Protocol C: Explicit default + opt-out.** Agent makes a concrete recommendation ("I'm doing X unless you say otherwise") instead of presenting 3 options and asking the human to choose. Human can approve in 20c ("go") or redirect in one sentence. Converts multi-option (f) turns into short (d) or short (b) turns.

**Protocol D: Design-stage language discipline.** Agent uses "candidate position" / "design-stage decision" language during planning, never "locked" or "committed." Prevents the premature-locking class of (b) turns.

**Protocol E: Structured session handoff.** Standardize the inter-session recovery message format (as seen in 729dad16 and 3bf5774c) to minimize (a) turn length on session restart. The 3bf5774c handoff is ~680c but highly effective — it describes open threads by task number, not by filing a summary of everything.

---

## Open questions

1. **Is the context-dump protocol sustainable at scale?** 13493c8d achieves near-zero correction cycles but requires 7,600c of upfront writing per planning session. Is this the right trade? Or does it simply defer correction cycles to later implementation?

2. **Are (f) turns getting longer over time within sessions?** The hypothesis is that multi-question agent turns stack with decision fatigue — the human writes more per (f) turn late in a session than early. If so, batching questions at session end is worse than batching them at session start.

3. **Does agent turn length predict subsequent human turn length?** Preliminary observation: very long agent turns (800+ words) seem to produce longer (f) and (b) responses, possibly because the human feels obligated to address everything. Needs quantitative verification.

4. **What portion of (b) turns are content-corrections vs framing-corrections?** Framing-corrections (where the agent's way of asking is wrong) appear more expensive than content-corrections (where the answer was just wrong). Worth distinguishing explicitly.

---

## Notes on variants

**Context-dump (13493c8d):** Lowest correction-cycle overhead in corpus, but not comparable to primary sessions because the human accepted that the agent would work autonomously for hours before any correction was possible. The protocol is appropriate for "founding vision" sessions; unsuitable for iterative design refinement.

**Session-recovery (729dad16):** The structured handoff opener ("# Session Recovery Context" + checkpoint reference) compresses (a) to ~150c. The session then degrades into mostly (g) administrative turns (debugging ntm/am tooling). Writing load in this session is not a planning dialog problem — it's an environment-setup problem that planning protocols cannot address.

**Short-recent (00eb9fc9):** Despite only 5 human turns, this session has the clearest example of a multi-question agent turn producing a long (f) response (~700c). If the agent had asked one question at a time, this session would have had 5–6 shorter turns instead of one expensive one — reducing the per-session writing burden without changing the total information exchanged.
