# Planning Protocols -- Research Statement (Phase 1 Output)

> Produced at the close of Phase 1 of the planning-protocols research track. This document is the handoff artifact for a fresh Phase 2 session: it states what we are investigating, what we already know from empirical Phase 1 work, what risks that empirical work introduces (local-maxima anchoring), and how Phase 2 should proceed to find genuinely better protocols rather than refinements of observed ones.

Last updated: 2026-04-23

---

## 1. Sharpened research question

The original Phase 1 question was: *"What planning protocols minimize human-attention cost of reaching a correct, implementable plan, while maintaining alignment between human intent and agent output?"*

After Phase 1 (corpus mining of the user's own practice), the question sharpens to:

> **What is the full space of planning protocols -- shapes of human-agent interaction during the idea → plan → spec phase of software work -- and where within that space are the highest-leverage protocols for minimizing human attention cost while maintaining alignment? Where are the user's existing practices locally optimal but globally suboptimal, and what unexplored regions of the space deserve investigation?**

Sub-questions:

- What dimensions does the space of planning protocols vary along?
- What trade-offs govern movement along each dimension?
- Where does observed user practice sit? Where doesn't it?
- What protocols from adjacent domains (pair programming, medical handoffs, Socratic method, design review, mediation, incident command, pilot-controller communication) map usefully into coding-agent contexts?
- What underlying cognitive mechanisms make some interaction shapes better than others (working-memory load, dual-process reasoning, premature-commitment effects)?

## 2. Evaluation criteria

**Caveat -- these criteria are provisional.** The user flagged explicitly that the correct evaluation criteria for planning protocols are not known. A protocol that scores well on these criteria but poorly on an unnamed-but-important criterion is a false positive. One of Phase 2's explicit early tasks is to interrogate and possibly refine or replace these criteria -- see §7 Step 1.

**If Phase 2 produces a durable way to evaluate planning protocols -- empirically or analytically -- that evaluation framework may be a more valuable output than any specific protocol recommendation.** This possibility should be actively pursued, not treated as a side effect.

Provisional criteria, in priority order:

| Priority | Criterion | Rationale |
|---|---|---|
| 1 | Human writing / clarification effort | The user's most scarce resource is typing and reading time. A good protocol reduces characters-the-human-has-to-produce to reach equivalent alignment. |
| 2 | Targeting of agent questions | Agents that ask trivial questions waste turns; agents that fail to surface architectural questions cause expensive correction cycles later. Good protocols improve question quality. |
| 3 | Correction-cycle count | Each correction is attention cost + context-reload cost. Fewer corrections = more time for other work. |
| 4 | Agent autonomy on trivial decisions | The user does not want to decide data structures, file names, or library choices. Protocols should let the agent make these calls. |
| 5 | Human context-switch load | Short human turns separated by long productive agent runs lets the human do other work. High-frequency ping-pong is attention-corrosive. |

Secondary / tertiary considerations (not primary but not to be regressed):
- Downstream implementation quality (already strong; must not degrade)
- Spec completeness at implementation-start (already strong; must not degrade)
- Total wall-clock time (cheaper than human-attention time; acceptable to spend more wall-clock if human-attention is preserved)

**A new protocol wins** when it produces better alignment at equal-or-lower cost on (1) and (3). It may be acceptable to trade (2-5) against each other when the human-writing metric improves.

## 3. Dimensions of variation

A planning protocol can be characterized as a position along multiple axes. These are the dimensions Phase 1 surfaced; Phase 2 may add more.

| Dimension | Values (non-exhaustive) |
|---|---|
| **Timing of alignment** | Pre-action plan disclosure; in-action question-surfacing; post-action review; continuous per-turn alignment-check; no explicit alignment (dead-reckoning) |
| **Decision locus** | Agent autonomous; pre-authorized (human sets rules upfront, agent executes within); interactive (agent asks, human answers); human-preempted (human states preference before agent would have asked); mixed rules (trivial autonomous, architectural interactive) |
| **Dialog form** | Short-volley (many small turns); long-message (few big turns); structured (tables, lists, numbered questions); narrative prose; hybrid; shape-shifting (protocol changes mid-session) |
| **Question style** | One-at-a-time; batched-at-end; embedded throughout turn; implicit (assumption-stating); open-ended ("what do you think?"); forced-choice with default |
| **Autonomy scope** | Unbounded; bounded-by-constraint-list; bounded-by-acceptance-tests; bounded-by-category; incremental (small step + report); none |
| **Context richness** | Minimal dispatch (one-liner); rich brief (detailed upfront); recovery handoff (inherits structured state); continuous context-building across turns |
| **Branching / topic handling** | Linear; parallel tracks; explicit parking list; human-tracked implicitly; agent-tracked with return-surfacing |
| **Plan expression form** | Prose spec; constraint list + acceptance tests; dialog log (plan lives in chat); structured jig artifact; behavior list; test cases; graph/diagram |
| **Knowledge direction** | Human-dominant (human teaches agent); agent-dominant (agent surfaces options, human selects); bidirectional; agent-investigative (agent elicits human intent via questions) |
| **Review / critique integration** | None; human self-review; sub-agent reviewer after draft; parallel reviewers (kerf pattern); pre-dispatch plan review; pre-reply self-review |

A protocol is a *tuple* of values across these dimensions. Most observed patterns in Phase 1 occupy a narrow region of this space; large regions are empirically untested.

## 4. Observed region of the space

**This section is descriptive, not prescriptive.** These are the patterns the user has already tried; Phase 1 evidence for each is in `phases/phase-2/analysis/` (six lens reports) and `phases/phase-1/tried-protocols.md`. They are data points, not the starting protocol set for Phase 2.

Observed patterns mapped to dimensions:

| Pattern | Key dimension-values |
|---|---|
| Planning dialog (primary corpus) | Timing: continuous; Decision locus: interactive; Form: hybrid; Questions: embedded (often batched); Autonomy: bounded-by-category; Context: continuous; Knowledge: bidirectional |
| Controller orchestration | Timing: post-action; Decision locus: agent-autonomous with mid-stream direction; Autonomy: unbounded; Branching: parallel |
| Autonomous dispatch ("study specs, never ask") | Timing: none; Decision locus: agent-autonomous; Autonomy: unbounded; Context: minimal dispatch + rich pre-built specs |
| Context-dump (13493c8d) | Timing: pre-action (all upfront); Form: long-message; Context: rich brief; Knowledge: human-dominant |
| Session-recovery handoff (729dad16) | Context: recovery handoff; Timing: pre-action |
| **Pre-action plan disclosure** (appears in 79a42399, f588ff0c A2) | Timing: pre-action; Decision locus: interactive if corrected; zero-cost corrections observed |
| **Upfront decision partition** (79a42399 H#1) | Decision locus: pre-authorized; Question style: forced to trivial-vs-critical split |
| **Numbered-question close** (agent ends turn with numbered list) | Question style: batched-at-end; observed to shorten subsequent human turns |

Observed trade-offs (from the lens analyses):
- Multi-question-per-turn is the single most avoidable writing-cost sink.
- Vocabulary/framing errors (silent) are more expensive than implementation errors (silent).
- Decision closure predicts autonomy-stretch length more than message length does.
- Context-dump reduces correction cycles but eliminates the correction loop -- errors propagate.

## 5. Unexplored regions of the space

Regions where the user's observed practice is thin or absent. Phase 2 should actively explore these. The list is non-exhaustive; more will emerge.

- **Agent-investigative protocols** -- agent actively asks clarifying questions to build a model of human intent before proposing, rather than proposing-then-correcting. Rare in observed corpus (the user almost always opens; the agent almost always responds).
- **Pre-dispatch plan review** -- agent's plan gets reviewed (by self or by another agent) *before* execution, not after. Observed once (kerf's parallel-reviewer pattern) but applied to output, not to plans.
- **Agent-surfaced parking** -- agent tracks parked branches and proactively offers to surface them at appropriate moments. Absent in observed corpus (branches are parked implicitly by the human and often lost).
- **Human-teaches-agent flows** -- explicit "let me explain how this domain works, then ask questions" phase. Partial in observed corpus.
- **Incremental-step autonomy** -- agent takes one small step, reports, next small step, vs unbounded run. Present but not well-characterized.
- **Protocol shape-shifting** -- explicitly changing protocol mid-session based on task phase (e.g., context-dump for founding, short-volley for refinement). Present implicitly; not deliberately chosen.
- **Behavior-first plan expression** -- plans written as behavior lists or test cases rather than structural specs. User flagged this as potentially interesting; absent from observed sessions.
- **Bidirectional knowledge direction** where agent contributes substantive domain knowledge, not just analysis of user-provided inputs. Partial.
- **Forced-choice with default** -- agent proposes "I'll do X unless you object" rather than "A or B?" Rare.
- **Pre-reply self-review by agent** -- agent drafts response, critiques own response, revises before sending. Observable effects would be visible only in reply quality; not studiable directly from transcripts.

## 6. Counter-pattern hypotheses

**Critical warning:** the observed patterns from Phase 1 may be locally optimal. A user who has iterated on their practice will have converged on what works for them -- but may have never explored paths they abandoned early or never considered. Phase 2 must seriously test inverse hypotheses.

For each cross-cutting Phase 1 finding, a counter-hypothesis that Phase 2 should actively investigate:

| Phase 1 finding | Counter-hypothesis to test |
|---|---|
| Pre-action plan disclosure is high-leverage | Plan disclosure causes premature commitment; better to explore without pre-committing and let alignment emerge from examples |
| Multi-question-per-turn is avoidable cost | Batched questions surface connected assumptions that one-at-a-time misses; single-question flows mislead about overall context and multiply turns |
| Upfront decision partition (79a42399) is best | Upfront partition rigidifies the agent's sense of what's "trivial"; mid-session partition adapts to actual problem shape |
| Numbered-question close is structural lever | Numbered questions constrain agent's own exploration; open-ended queries surface deeper issues a rigid format suppresses |
| Decision closure enables long autonomy | Decision closure prematurely commits to a path; incremental runs with frequent tiny checkpoints are safer and surface misalignments earlier |
| Context-dump trades writing for correction reduction | Context-dump is brittle; dialogic context-building accommodates thinking-that-reveals-itself-only-mid-conversation, which a brief cannot anticipate |
| "Don't ask questions" clause is load-bearing for autonomy | Removing questions creates false confidence; autonomous runs produce more, longer corrections later because the agent cannot self-correct for human-frame issues |
| Form-of-discussion matters independently of content | Form is epiphenomenal; underlying knowledge-state mismatch is the real driver and form is just a surface symptom |

Each counter-hypothesis is a research bet. Phase 2 should take each seriously enough to design an evaluation.

## 7. Phase 2 methodology

**Principle:** Phase 2 does NOT start by refining observed patterns. That structure anchors on local maxima. Instead, Phase 2 starts by populating unexplored regions of the space with candidates from external sources, then evaluates observed and external candidates together on equal footing.

### Required Phase 2 steps (in order)

**Step 1: Interrogate the evaluation criteria themselves.**

Before external-source work begins, Phase 2 must pause to question the provisional criteria in §2. This is not a formality; the user flagged the criteria as uncertain. Questions to answer:

- Are these the right criteria? What might be missing? Candidates worth exploring:
  - Quality of mental-model transfer between human and agent
  - Probability of post-implementation regret (did the plan produce what the user actually wanted?)
  - Robustness to user state (tired vs fresh, novice vs expert, attention-available vs drained)
  - Trust accumulation / erosion across repeated protocol use
  - Downstream maintainability signals (does the plan produce work easy to modify later?)
  - Transferability (does the protocol work across task types, or is it task-specific?)
  - Stability to human fuzziness (does the protocol still work when the human's own intent is fuzzy?)
- Can each criterion be **operationalized**? "Human writing effort" is measurable (character counts). "Correction-cycle count" is measurable. "Targeting of agent questions" is harder -- what proxy would work?
- Are there meta-criteria about the planning *process*: does the protocol produce a plan that is reviewable, reusable, composable? Does it leave an audit trail?
- Is there a way to evaluate protocols **empirically** rather than by analysis? Can sessions be A/B-tested at reasonable cost? What natural experiments exist in the existing corpus or in kerf's historical works?
- Could a formal evaluation framework itself be a research deliverable? Test-suite-of-planning-problems that protocols are run against?

Output: `phases/phase-2/analysis/evaluation-criteria-refinement.md`. Any refinement should be applied back to §2 (with a revision note).

**If this step surfaces fundamental issues with the criteria, pause and surface to the user before proceeding to Step 2.** The rest of Phase 2 is premised on the criteria being at least roughly right.

**Step 2: External-source pass (must happen before any refinement of observed patterns).**

Mine the following adjacent domains for planning / alignment / coordination protocols:

- **Pair programming** -- driver-navigator, ping-pong pair, strong-style pair. What governs knowledge-transfer?
- **Socratic method** -- elenchus, dialectic, question-driven elicitation. Teacher-as-asker-not-answerer.
- **Medical handoffs** -- SBAR (Situation / Background / Assessment / Recommendation), structured shift reports.
- **Design review / code review** -- RFC protocols, pull-request conventions, architectural decision records.
- **Negotiation and mediation** -- interest-based negotiation, principled negotiation (Fisher/Ury), shuttle diplomacy.
- **Incident command systems** -- ICS protocols for coordinated response under uncertainty.
- **Pilot-controller communication** -- read-back/hear-back, phraseology standards, authority transfer.
- **Therapy intake** -- motivational interviewing, open-ended elicitation protocols.
- **Consulting engagements** -- discovery phase structures, stakeholder interview protocols.
- **Military briefings** -- commander's intent, five-paragraph order, back-brief.

For each domain, extract candidate protocols and map them onto the dimensions of variation (Section 3). Produce: `phases/phase-2/analysis/external-sources/<domain>.md` per domain.

**Step 3: Counter-pattern generation.**

For each counter-hypothesis in Section 6, design a specific protocol instance that embodies it. Map it onto the dimensions. Do not dismiss -- steel-man.

**Step 4: Unified candidate catalog.**

Consolidate observed protocols (Section 4), unexplored-region candidates (Section 5), external-domain candidates (Step 2), and counter-pattern candidates (Step 3) into a single catalog. Every protocol gets the same schema: name, one-line definition, dimension-values, mechanism (why it might work), predicted trade-offs, evaluation plan.

**Step 5: Reviewer-challenged evaluation.**

Run reviewer sub-agents with explicit instructions to challenge observed patterns (not defer to them). Evaluate every candidate against the (possibly-refined-by-Step-1) criteria in Section 2. Use multiple reviewers with different frames (ergonomics, cognitive-load, robustness-to-user-fatigue, adaptability-to-different-task-types).

**Step 6: Ranked recommendations with boundary conditions.**

Rank protocols not absolutely but *conditionally*: which protocol is best for which task-type / session-phase / user-state? Recommend compositions where applicable.

**Step 7: Kerf integration proposal.**

For the top-ranked recommendations, propose concrete integration points into kerf's existing jig structure. This is where research feeds back into the product.

### Phase 2 deliverable spec

Phase 2 output is a document (tentatively `phase-2-findings.md`) containing:

1. **Evaluation-criteria refinement** (from Step 1) -- whatever version of §2 Phase 2 converged on, with justification.
2. External-domain catalog (one entry per domain, mapped to dimensions).
3. Unified protocol catalog (observed + unexplored + external + counter-patterns, on equal schema).
4. Evaluation of each candidate against (refined) criteria, with reviewer critiques.
5. Ranked recommendations by task-type / session-phase / user-state.
6. Proposed kerf integration points for top recommendations.
7. Open questions for further work / Phase 3.

A formal evaluation framework (if one emerges from Step 1) may warrant its own document (`evaluation-framework.md`).

Phase 2 should NOT deliver a single "winning" protocol. The hypothesis is that multiple protocols are useful in different situations, and the research output is a *map* of when-to-use-what.

### Guardrails against local-maxima anchoring

- Step 1 (criteria interrogation) must complete before Step 2.
- Step 2 (external sources) must complete before Steps 3-5 or any refinement of observed patterns.
- Reviewer sub-agents must explicitly be told to challenge, not defer to, observed patterns.
- Any recommendation that is functionally identical to an observed pattern must be justified against at least one external-source or counter-pattern alternative that was considered and rejected. Write the rejection reason.
- At least one reviewer pass must ask: "if the opposite of the Phase 1 finding were true, would the recommendations change?"
- If Step 1 surfaces new criteria, at least one reviewer pass must re-evaluate top candidates against the new criteria.

## 8. Open questions

Questions this research statement surfaces but does not resolve. Phase 2 should treat these as candidates for investigation.

- **What is the right way to evaluate planning protocols?** Is the criteria set in §2 adequate? Can protocols be evaluated empirically via A/B testing on actual planning sessions? Can criteria be derived from first-principles analysis of cognitive-coordination mechanisms? A durable evaluation framework may be more valuable than any specific protocol recommendation.
- Do protocol dimensions (Section 3) compose freely, or are there coupling constraints (e.g., does a given timing-value only work with certain question-styles)?
- Is there a cognitive-science model (working memory, dual-process, attention budget) that predicts which dimensions matter most for which tasks?
- The user flagged "behaviors vs implementation details" as a candidate scoping lever. Does this become a plan-expression-form protocol, or a separate dimension?
- Can an agent be trained or prompted to self-select a protocol based on task-type signals? What signals?
- Are there user-state dependencies -- e.g., a tired user wants context-dump, a fresh user wants dialog? Does the protocol need to adapt?
- Is there a notion of "protocol debt" analogous to technical debt -- patterns that work short-term but compound poorly over a long planning session?
- Does the effectiveness of a protocol depend on the model (Opus vs Sonnet vs Haiku)? On the human's experience level?

## 9. What Phase 2 should *not* do

- Do not start by refining observed patterns. That anchors on local maxima.
- Do not treat the counter-hypotheses as rhetorical devices to be dismissed. They are research bets.
- Do not treat "the user has been doing X" as strong evidence X is optimal. It's evidence X is a local optimum in a large space.
- Do not dismiss external-source protocols as "those are for humans, not agents." Most of the alignment problems are cognitive-coordination problems; agents don't change the underlying coordination dynamics.
- Do not optimize for "agent looks smart / fast / agreeable." Optimize for the criteria in Section 2.
- Do not deliver a single winning protocol. Deliver a *map* of when-to-use-what.

## 10. Phase 1 artifacts (inputs to Phase 2)

Phase 2 should read:

- This document (`research-statement.md`).
- [`METHODOLOGY.md`](METHODOLOGY.md) for research-track conventions and multi-session safety rules.
- [`phases/phase-1/tried-protocols.md`](phases/phase-1/tried-protocols.md) for the 5-variant taxonomy.
- [`phases/phase-1/session-type-discriminator.md`](phases/phase-1/session-type-discriminator.md) for how to identify planning-dialog sessions if more corpus is needed.
- [`phases/phase-2/analysis/*.md`](phases/phase-2/analysis/) -- the six Phase 1 lens reports (decision-delegation, misaligned-assumption, writing-load, form-vs-content, topic-tree, context-switch). These are *evidence*, not *conclusions* to build on.
- [`phases/phase-1/corpus/INDEX.md`](phases/phase-1/corpus/INDEX.md) for the 10 extracted planning-dialog sessions, if direct inspection is needed.
- [`references/perplexity-initial-research.md`](references/perplexity-initial-research.md) for the starting-point brainstorm (shallow but useful as a reminder of the original framing).

Phase 2 should **not** treat any of the Phase 1 findings as established. They are the *starting provocations*.
