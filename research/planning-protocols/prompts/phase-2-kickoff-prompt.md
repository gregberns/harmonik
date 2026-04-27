# Phase 2 Kickoff Prompt

> Copy-paste the block below into a new Claude Code session in the harmonik project directory to begin Phase 2 of the planning-protocols research track.

---

You are starting **Phase 2 of the planning-protocols research track** in the harmonik project. Phase 1 is complete; its output is a focused research statement that is your primary hand-off document.

This is a research track, not spec work -- kerf is NOT used. Artifacts go under `research/planning-protocols/`.

## First actions (do these before anything else)

Read these files in order:

1. `/Users/gb/github/harmonik/research/planning-protocols/CLAUDE.md` -- track rules for all sessions
2. `/Users/gb/github/harmonik/research/planning-protocols/STATUS.md` -- current state. Append a session-history entry when you pause or finish
3. `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-1/research-statement.md` -- Phase 1 output; your main briefing. Pay close attention to §2 (evaluation-criteria caveat), §3 (dimensions of variation), §5-6 (unexplored regions + counter-hypotheses), §7 (Phase 2 methodology), §9 (what NOT to do)
4. `/Users/gb/github/harmonik/research/planning-protocols/METHODOLOGY.md` -- research-track methodology and multi-session safety rules

Only after reading all four, begin the methodology in §7.

## Task

Execute the Phase 2 methodology in research-statement §7, in order. Go as far as makes sense in a single session.

**Target completion: through Step 6 (ranked recommendations with boundary conditions).** Step 7 (kerf integration proposal) benefits from user domain knowledge -- you may produce a *draft* for user review but do not commit it as final without user.

## Critical discipline (re-stated from research-statement §9 -- survives context)

1. **Do not start by refining observed patterns from Phase 1.** Step 2 (external-source pass) must complete first. Anchoring on observed patterns is the central research risk this phase is structured around.

2. **Step 1 (criteria interrogation) is mandatory before Step 2.** The user explicitly flagged that the correct evaluation criteria are not known. Do not skip or shortcut. If Step 1 surfaces fundamental issues with the provisional criteria, pause and surface to the user before proceeding. If Phase 2 produces a durable evaluation framework, that may be more valuable than any specific protocol recommendation -- treat it as a first-class deliverable, not a side effect.

3. **Reviewer sub-agents in Step 5 must explicitly be told to challenge observed patterns, not defer to them.** Default reviewer framing defers; explicit instruction is needed to invert.

4. **Any final recommendation functionally identical to an observed pattern must cite at least one considered-and-rejected external or counter-pattern alternative.** Write the rejection reason.

5. **Observed patterns from Phase 1 are data points, not the starting protocol set.** Treat them as one source among several (alongside external-domain sources and counter-patterns).

## Sub-agent usage

Fan out with parallel sub-agents wherever work decomposes cleanly:

- **Step 2 (external sources):** one sub-agent per domain. 10 domains are named in research-statement §7 Step 2 (pair programming, Socratic method, medical handoffs, design review, negotiation, incident command, pilot-controller, therapy intake, consulting discovery, military briefings). Outputs to `phases/phase-2/analysis/external-sources/<domain>.md`. Launch them in a single message for parallelism.

- **Step 5 (reviewer evaluation):** multiple reviewer sub-agents with distinct frames. At minimum: ergonomics, cognitive-load, robustness-to-user-fatigue, adaptability-to-task-types, and one explicitly-framed "challenge observed patterns" reviewer.

Every sub-agent prompt must be self-contained: include the research question, the step goal, relevant locked choices, and file paths. Do not assume sub-agents have read your context.

## Outputs per step

- Step 1: `phases/phase-2/analysis/evaluation-criteria-refinement.md`
- Step 2: `phases/phase-2/analysis/external-sources/<domain>.md` per domain
- Step 3: `phases/phase-2/analysis/counter-pattern-candidates.md`
- Step 4: `phases/phase-2/analysis/unified-protocol-catalog.md`
- Step 5: `phases/phase-2/analysis/reviewer-evaluation.md`
- Step 6: `phase-2-findings.md` in `research/planning-protocols/` (ranked recommendations, conditional on task-type / session-phase / user-state)
- Step 7: (deferred -- DRAFT only for user review if time permits, as `phase-2-kerf-integration-draft.md`)
- If a formal evaluation framework emerges from Step 1: `evaluation-framework.md` in the track root

## Session discipline

- Update STATUS.md with a session-history entry describing progress, blockers, and next-session recommendations whenever you pause or finish.
- Do not reopen locked choices in METHODOLOGY.md without user sign-off.
- Do not overwrite prior artifacts; append or create new dated files.
- If scope questions arise during the work, surface them rather than guess.
- If progress stalls (e.g., Step 1 reveals the criteria are fundamentally wrong and you cannot resolve without user), stop, update STATUS.md, and return to the user.

Begin with Step 1.
