# Planning Protocols -- Index

> Table of contents for the planning-protocols research track. Start here if you are new to this research, or if you are returning and want to orient quickly before diving in.
>
> This document explains *what exists, where to find it, what it's for, and what order to read it in.* It does not repeat content from the artifacts themselves.
>
> Last updated: 2026-04-27 (per-phase folder structure + skill trial added).

---

## 1. What this research is

Investigation of **planning protocols** — reusable shapes of human-agent interaction during the idea → plan → spec phase of software work. The user is a senior systems architect working solo with a coding agent; the user-reported pain points (human over-writing, agent misaligned assumptions, agent deferring trivial decisions) frame the problem.

Two-phase research. **Phase 1** (scoping) mined the user's own transcripts and produced a focused research statement. **Phase 2** (deep research) populated external-domain and counter-pattern candidates, evaluated them under multiple reviewer frames, and produced ranked recommendations plus a durable evaluation framework.

Findings are intended to feed back into kerf (harmonik's planning CLI). Findings are NOT themselves normative specs — they are inputs to later spec work.

## 2. Current state at a glance

- **Phase 1:** CLOSED. Produced [`research-statement.md`](phases/phase-1/research-statement.md).
- **Phase 2 main session:** CLOSED. Produced [`phase-2-findings.md`](phases/phase-2/findings.md), [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md), and [`phase-2-kerf-integration-draft.md`](phases/phase-2/kerf-integration-draft.md).
- **Skill trial:** ACTIVE. `/session-handoff` + `/session-resume` user-level skills. Adopt-and-notice testing across real working sessions. See [`protocol-trial-roadmap.md`](protocol-trial-roadmap.md). **Trial finding 1 recorded 2026-04-27** at [`trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](trial-findings/2026-04-27-skills-too-verbose-and-procedural.md) — schema-heavy skills produce verbose, procedural output and leak internal jargon into user-facing asks. **v2 deployed 2026-04-28** ([`skill-iterations/v2-2026-04-28/`](skill-iterations/v2-2026-04-28/)) — 16 + 14 lines vs v1's 103 + 101.
- **Step 4.5 (corpus-signal filter):** DEFERRED pending user authorization. This is the main blocker on empirically weighting the Phase 2 rankings. Plan + reviews in [`plans/`](plans/).
- **Layer 7 A/B experiments:** SPEC'd, not RUN. User authorization needed.
- **Kerf integration:** DRAFT only. User review needed before turning into a kerf work.

See [`STATUS.md`](STATUS.md) for session history and detailed state.

## 3. Top findings at a glance

Read these short statements first; they orient the rest of the material. Each points to where the full argument lives.

1. **A durable *evaluation framework* emerged as the most valuable Phase 2 output.** The user's concern that the right evaluation criteria were unknown is partly resolved, partly made permanent as a methodological commitment (pair-graph + multi-framing). → [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md).
2. **7 of 8 Phase 1 observed findings lost to external-source or counter-pattern rivals** under the challenge-observed reviewer. Observed patterns are locally optimal but globally suboptimal in predictable ways. → [`phases/phase-2/analysis/reviewer-challenge-observed.md`](phases/phase-2/analysis/reviewer-challenge-observed.md) and [`phase-2-findings.md`](phases/phase-2/findings.md) §7.
3. **`numbered-question-close` is flagged as an *anti-pattern* in aviation CRM** — the single strongest external-evidence-backed displacement case in the catalog. Replace with `load-bearing-token-readback`. → [`phases/phase-2/analysis/external-sources/pilot-controller.md`](phases/phase-2/analysis/external-sources/pilot-controller.md) + Phase 2 findings §4.6 Safe Swap #1.
4. **Trap candidates identified:** protocols that score high on the provisional framework but low on Framing C (regret-adjusted outcome). These are exactly the "unnamed-but-important criterion" failure mode the user flagged. → [`phases/phase-2/analysis/reviewer-multi-framing.md`](phases/phase-2/analysis/reviewer-multi-framing.md).
5. **Convergent winners** across ≥4 reviewer frames: `commanders-intent`, `back-brief-plan-quality`, `autonomy-scope-grant`, `alternatives-considered-section`, `role-split-reviewer-library`, `premortem-reviewer`, `load-bearing-token-readback`, `single-text-procedure`. → [`phase-2-findings.md`](phases/phase-2/findings.md) §3.1.
6. **Hidden gems** under-rewarded by the provisional framework: `example-led-emergence`, `emergent-partition`, `assumption-bundle`, `question-preserving-autonomy`, `asynchronous-navigator`, `dialogic-context-accretion`. → `phase-2-findings.md` §3.3.
7. **Recommended adoption comes in composition layers** (always-on foundation; task-shape openers; mid-session stack; user-state adapters; close-of-session; safe swaps; experiments). → `phase-2-findings.md` §4 and §6.

## 4. Reading paths by audience

Pick the path that matches your situation.

### Path A -- User doing a deep-dive (most likely)

You want to digest what was done, form opinions, and decide on next actions. Suggested reading order (≈2–3 hours):

1. **This INDEX** (you are here). 5 min.
2. [`phase-2-findings.md`](phases/phase-2/findings.md) §1–§4 first, then §7–§10. 30 min. This is the Phase 2 deliverable; §10 lists the direct user decisions.
3. [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md) §1–§5. 20 min. Understand the instrument before judging the output.
4. [`phases/phase-2/analysis/reviewer-challenge-observed.md`](phases/phase-2/analysis/reviewer-challenge-observed.md). 20 min. The most controversial reviewer output; the local-maxima-guardian frame is explicitly anti-deferential. Read this to calibrate how much to trust the §3.1 convergent winners.
5. [`phases/phase-2/analysis/reviewer-multi-framing.md`](phases/phase-2/analysis/reviewer-multi-framing.md). 15 min. The trap-candidate list names your stated "unnamed-but-important criterion" concern concretely.
6. [`phase-2-kerf-integration-draft.md`](phases/phase-2/kerf-integration-draft.md). 15 min. Concrete integration points + §8 questions for you.
7. **Optional deep-dives** as curiosity or disagreement guides:
   - [`phases/phase-2/analysis/unified-protocol-catalog.md`](phases/phase-2/analysis/unified-protocol-catalog.md) for any protocol you want to examine in full schema.
   - [`phases/phase-2/analysis/counter-pattern-candidates.md`](phases/phase-2/analysis/counter-pattern-candidates.md) if you want to read each of the 8 steel-manned inverses of Phase 1 findings.
   - [`phases/phase-2/analysis/evaluation-criteria-refinement.md`](phases/phase-2/analysis/evaluation-criteria-refinement.md) for the Step 1 synthesis.
   - [`phases/phase-2/analysis/external-sources/`](phases/phase-2/analysis/external-sources/) for any of the 10 external-domain catalogs whose protocols caught your eye.
   - [`phases/phase-2/analysis/reviewer-ergonomics.md`](phases/phase-2/analysis/reviewer-ergonomics.md), [`reviewer-cognitive-load.md`](phases/phase-2/analysis/reviewer-cognitive-load.md), [`reviewer-fatigue-robustness.md`](phases/phase-2/analysis/reviewer-fatigue-robustness.md), [`reviewer-adaptability.md`](phases/phase-2/analysis/reviewer-adaptability.md) for the other four reviewer frames.

### Path B -- Returning agent continuing the research work

You're picking up where the last session left off. Read order:

1. [`CLAUDE.md`](CLAUDE.md) (track rules). 2 min.
2. [`STATUS.md`](STATUS.md) (current state + session history). 10 min.
3. This INDEX. 5 min.
4. [`phase-2-findings.md`](phases/phase-2/findings.md) §10 (user decisions open). 5 min.
5. Whatever the user's current instruction points you at.

### Path C -- Fresh agent, first time

You've never seen this research track before. Read order:

1. [`../CLAUDE.md`](../CLAUDE.md) (research/ dir rules). 2 min.
2. [`CLAUDE.md`](CLAUDE.md) (this track's rules). 2 min.
3. [`METHODOLOGY.md`](METHODOLOGY.md) (the process the research follows). 10 min.
4. [`research-statement.md`](phases/phase-1/research-statement.md) (Phase 1 output; the original briefing). 20 min.
5. [`STATUS.md`](STATUS.md) (current state). 10 min.
6. This INDEX. 5 min.
7. [`phase-2-findings.md`](phases/phase-2/findings.md). 30 min.

### Path D -- Reviewer validating the work

You're checking whether the Phase 2 conclusions are defensible. Read order:

1. [`research-statement.md`](phases/phase-1/research-statement.md) §7 (Phase 2 methodology) and §9 (what NOT to do). 10 min.
2. [`phase-2-findings.md`](phases/phase-2/findings.md) §1 (what was produced), §7 (what survives challenge), §8 (honest limits). 15 min.
3. Cross-check convergent winners / trap candidates against:
   - [`phases/phase-2/analysis/reviewer-challenge-observed.md`](phases/phase-2/analysis/reviewer-challenge-observed.md)
   - [`phases/phase-2/analysis/reviewer-multi-framing.md`](phases/phase-2/analysis/reviewer-multi-framing.md)
   - [`phases/phase-2/analysis/unified-protocol-catalog.md`](phases/phase-2/analysis/unified-protocol-catalog.md)
4. Validate the evaluation framework itself: [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md) §3, §5, §11.

## 5. Document map (by purpose)

### Entry and governance

| File | Purpose |
|---|---|
| [`CLAUDE.md`](CLAUDE.md) | Rules for any agent working in this track. Read first when entering. |
| [`METHODOLOGY.md`](METHODOLOGY.md) | The research process. Locked choices. Session-safety rules. |
| [`STATUS.md`](STATUS.md) | Live state + full session history. Updated at every session close. |
| [`INDEX.md`](INDEX.md) | This file. Reading paths and document map. |

### Phase 1 artifacts (scoping)

| File | Purpose |
|---|---|
| [`research-statement.md`](phases/phase-1/research-statement.md) | Phase 1 output and Phase 2 briefing. Sharpened question, evaluation-criteria caveat, dimensions of variation, observed regions, unexplored regions, counter-hypotheses, Phase 2 methodology. Still the authoritative briefing despite Phase 2 refinements. |
| [`phases/phase-1/corpus/INDEX.md`](phases/phase-1/corpus/INDEX.md) | Per-project catalogs of the user's session corpus (195 sessions across 4 projects). |
| [`phases/phase-1/corpus/<project>/<session-id>.md`](phases/phase-1/corpus/) | 10 extracted planning-dialog sessions. Primary evidence for Phase 1 lens analyses. |
| [`phases/phase-1/analysis/decision-delegation.md`](phases/phase-1/analysis/decision-delegation.md), [`misaligned-assumption.md`](phases/phase-1/analysis/misaligned-assumption.md), [`writing-load.md`](phases/phase-1/analysis/writing-load.md), [`form-vs-content.md`](phases/phase-1/analysis/form-vs-content.md), [`topic-tree.md`](phases/phase-1/analysis/topic-tree.md), [`context-switch.md`](phases/phase-1/analysis/context-switch.md) | Six Phase 1 lens reports. Evidence, not conclusions. Cited by Phase 2 but not authoritative. |
| [`phases/phase-1/tried-protocols.md`](phases/phase-1/tried-protocols.md) | 5-variant taxonomy of interaction shapes in the observed corpus. |
| [`phases/phase-1/session-type-discriminator.md`](phases/phase-1/session-type-discriminator.md) | Mechanical filter for isolating real human-text turns in JSONL transcripts. |
| [`references/perplexity-initial-research.md`](references/perplexity-initial-research.md) | Starting-point brainstorm. Acknowledged as shallow; useful only for original-framing reminder. |

### Phase 2 Step 1 artifacts (criteria interrogation)

| File | Purpose |
|---|---|
| [`phases/phase-2/analysis/evaluation-criteria-refinement.md`](phases/phase-2/analysis/evaluation-criteria-refinement.md) | Synthesis of Step 1. Decides: no fundamental issues with criteria; refactor to pair-graph; add multi-framing requirement; elevate evaluation-framework.md as first-class deliverable. |
| [`phases/phase-2/analysis/evaluation-criteria-refinement.sub-rival-framings.md`](phases/phase-2/analysis/evaluation-criteria-refinement.sub-rival-framings.md) | Three first-principles alternative frameworks (A: Commitment-Deferral; B: Mental-Model Coupling; C: Regret-Adjusted Outcome). Plus four framings considered-and-rejected with rationale. |
| [`phases/phase-2/analysis/evaluation-criteria-refinement.sub-operationalization-audit.md`](phases/phase-2/analysis/evaluation-criteria-refinement.sub-operationalization-audit.md) | Audit of provisional + candidate-addition criteria for measurability, cost, gameability, trade-offs. |
| [`phases/phase-2/analysis/evaluation-criteria-refinement.sub-empirical-design.md`](phases/phase-2/analysis/evaluation-criteria-refinement.sub-empirical-design.md) | Natural-experiment catalog (7 NEs in existing corpus), test-suite design, A/B feasibility, simulation, practitioner-diagnostic. Recommendations R1–R6. |
| [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md) | **First-class Phase 2 deliverable.** Operational criteria (pair-graph), transcript-only harness, multi-framing requirement, diagnostic signal catalog, A/B / test-suite / simulation templates. The most durable artifact of Phase 2. |

### Phase 2 Step 2 artifacts (external-source pass)

| File | Purpose |
|---|---|
| [`phases/phase-2/analysis/external-sources/pair-programming.md`](phases/phase-2/analysis/external-sources/pair-programming.md) | Driver-navigator, strong-style, ping-pong, mob, cognitive tag team, abstraction-shifting. |
| [`phases/phase-2/analysis/external-sources/socratic-method.md`](phases/phase-2/analysis/external-sources/socratic-method.md) | Elenctic probe, maieutic draw-out, reduced dialectic, Graesser taxonomy, bounded Five Whys, aporia, directional clean-repetition. |
| [`phases/phase-2/analysis/external-sources/medical-handoffs.md`](phases/phase-2/analysis/external-sources/medical-handoffs.md) | SBAR, I-PASS, read-back, CUS, teach-back, watcher tier. |
| [`phases/phase-2/analysis/external-sources/design-review.md`](phases/phase-2/analysis/external-sources/design-review.md) | RFC, ADR, ATAM, pre-mortem, utility tree, role-split review, alternatives-considered. |
| [`phases/phase-2/analysis/external-sources/negotiation-mediation.md`](phases/phase-2/analysis/external-sources/negotiation-mediation.md) | Interest-surfacing, single-text procedure, shuttle diplomacy, third-story framing, NVC, positive-no. |
| [`phases/phase-2/analysis/external-sources/incident-command.md`](phases/phase-2/analysis/external-sources/incident-command.md) | Commander's intent, IAP, sitrep, AAR, tactical pause, role separation. |
| [`phases/phase-2/analysis/external-sources/pilot-controller.md`](phases/phase-2/analysis/external-sources/pilot-controller.md) | Load-bearing-token readback, fixed-token vocabulary, PACE assertiveness, sterile cockpit, authority transfer. Where the numbered-close anti-pattern evidence lives. |
| [`phases/phase-2/analysis/external-sources/therapy-intake.md`](phases/phase-2/analysis/external-sources/therapy-intake.md) | OARS reflection cascade, elicit-provide-elicit, rolling with resistance, four-process arc, screener-gated branching. |
| [`phases/phase-2/analysis/external-sources/consulting-discovery.md`](phases/phase-2/analysis/external-sources/consulting-discovery.md) | MECE, issue tree, SPIN, hypothesis-driven ghost deck, Pyramid principle, SCQA, engagement-letter out-of-scope. |
| [`phases/phase-2/analysis/external-sources/military-briefings.md`](phases/phase-2/analysis/external-sources/military-briefings.md) | Commander's intent, five-paragraph order, back-brief, mission command, METT-TC, rehearsal hierarchy, TLP, AAR. |

### Phase 2 Step 3 artifact (counter-patterns)

| File | Purpose |
|---|---|
| [`phases/phase-2/analysis/counter-pattern-candidates.md`](phases/phase-2/analysis/counter-pattern-candidates.md) | 8 steel-manned counter-protocols, one per Phase 1 cross-cutting finding. Example-led emergence, assumption-bundle disclosure, emergent partition, open-ended hand-off, micro-step incrementalism, dialogic context accretion, question-preserving autonomy, knowledge-state mapping. |

### Phase 2 Step 4 artifact (unified catalog)

| File | Purpose |
|---|---|
| [`phases/phase-2/analysis/unified-protocol-catalog.md`](phases/phase-2/analysis/unified-protocol-catalog.md) | **87 distinct protocols** on shared 8-field schema (name + origin + definition + dimension values + mechanism + trade-offs + evidence + evaluation plan). Grouped by structural kind. Includes cross-cutting observations on dimension coverage, novelties, triangulation signals. Input to Step 5 and Step 6. |

### Phase 2 Step 5 artifacts (reviewer evaluations)

All reviewers operate on the unified catalog. Each applies a distinct challenge frame. All six outputs should be read together; any single one is incomplete.

| File | Frame |
|---|---|
| [`phases/phase-2/analysis/reviewer-ergonomics.md`](phases/phase-2/analysis/reviewer-ergonomics.md) | Moment-to-moment ease-of-use. Rewards silent-agent-side work; demotes heavy per-turn ceremony. |
| [`phases/phase-2/analysis/reviewer-cognitive-load.md`](phases/phase-2/analysis/reviewer-cognitive-load.md) | Working memory, attention-switching, reasoning overhead. Rewards bounded frequency + recognition-dominant moves. |
| [`phases/phase-2/analysis/reviewer-fatigue-robustness.md`](phases/phase-2/analysis/reviewer-fatigue-robustness.md) | Graceful degradation under tired / distracted user state. Identifies the dangerous "rubber-stamp class" (looks safe but silently drifts under fatigue). |
| [`phases/phase-2/analysis/reviewer-adaptability.md`](phases/phase-2/analysis/reviewer-adaptability.md) | Cross-task-shape fit. 8 task shapes × top 30 protocols matrix. Per-shape recommendations. |
| [`phases/phase-2/analysis/reviewer-challenge-observed.md`](phases/phase-2/analysis/reviewer-challenge-observed.md) | **Local-maxima guardian.** Explicitly anti-deferential to observed patterns. 7 of 8 Phase 1 findings lose to rivals; 3 observed patterns survive with augmentation. |
| [`phases/phase-2/analysis/reviewer-multi-framing.md`](phases/phase-2/analysis/reviewer-multi-framing.md) | Top 25 candidates scored on all four framings (provisional + A/B/C). Safe candidates, trap candidates, hidden gems. |

### Phase 2 Step 6 artifact (ranked recommendations)

| File | Purpose |
|---|---|
| [`phase-2-findings.md`](phases/phase-2/findings.md) | **Main Phase 2 deliverable.** Composition layers 1–7 of recommendations, cross-reviewer convergent winners/losers, composition stacks, what survived the challenge, honest limits, open questions. §10 names direct user decisions. |

### Phase 2 Step 7 artifact (kerf integration)

| File | Purpose |
|---|---|
| [`phase-2-kerf-integration-draft.md`](phases/phase-2/kerf-integration-draft.md) | **DRAFT only.** Maps Layer 1 foundation + Layer 6 safe swaps onto kerf's pass/jig/reviewer structure. §8 names open questions needing user input before being turned into a kerf work. |

### Forward work — active trial and parked plans

| File | Purpose |
|---|---|
| [`protocol-trial-roadmap.md`](protocol-trial-roadmap.md) | **Active.** Roadmap for the `/session-handoff` + `/session-resume` skill trial; calibration items to watch; layered-in additions parked behind trial signal. |
| [`plans/step-4.5-plan.md`](plans/step-4.5-plan.md) | Parked. Implementation plan for Step 4.5 corpus-signal filter (transcript-only harness across 195 sessions). Authorization-gated. |
| [`plans/step-4.5-plan.review-1-coherence.md`](plans/step-4.5-plan.review-1-coherence.md) | Parked. Coherence review of Step 4.5 plan — flagged FP-inflation in correction-detection as primary concern. |
| [`plans/step-4.5-plan.review-2-risk.md`](plans/step-4.5-plan.review-2-risk.md) | Parked. Risk review of Step 4.5 plan — flagged NE-6 phase confound as primary push-back. |

### Trial findings

Findings from real-session use of the active trial skills. Each finding separates observations (what occurred) from analysis (causes), and lists open followups. Findings accumulate over the trial; they are not specs.

| File | Purpose |
|---|---|
| [`trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](trial-findings/2026-04-27-skills-too-verbose-and-procedural.md) | **Finding 1.** Schema-heavy skills induce verbose, procedural mid-session output. Internal jargon leaks into user-facing asks. Triggered by user pushback in basata `426257cc` (under `/session-resume`) and harmonik `a121e7f1` (NOT under `/session-resume`, deep in pilot-review work — same failure shape, different protocol). Triggered v2 skill iteration. |

### Skill iterations

Versioned rewrites of trial skills, one iteration per directory. Each iteration follows the convention in `skill-iterations/CONVENTIONS.md` (research / drafts / reviews / process documentation).

| File | Purpose |
|---|---|
| [`skill-iterations/CONVENTIONS.md`](skill-iterations/CONVENTIONS.md) | Iteration directory layout convention. Read before starting a new iteration. |
| [`skill-iterations/v1-baseline/`](skill-iterations/v1-baseline/) | **v1 snapshot** — the version retired on 2026-04-28. 103 + 101 lines. Kept for revert and retrospective comparison. |
| [`skill-iterations/v2-2026-04-28/`](skill-iterations/v2-2026-04-28/) | **v2 iteration** — first rewrite triggered by trial finding 1. Research (I-PASS deep-dive, external-form comparison, anti-anchored fresh draft), drafts (initial + post-review), reviews (4 parallel angles), process documentation. **Deployed 2026-04-28.** Revised drafts at `drafts/session-{handoff,resume}-revised.md` are what's now live at `~/.claude/skills/`. |

### Session-starter prompts (paste-ins)

| File | Purpose |
|---|---|
| [`prompts/phase-2-kickoff-prompt.md`](prompts/phase-2-kickoff-prompt.md) | Paste-in prompt used to start the Phase 2 session. Historical / reference only — Phase 2 main session is closed. |
| [`prompts/deep-dive-prompt.md`](prompts/deep-dive-prompt.md) | Paste-in prompt for a fresh agent session that wants to walk the user through digesting Phase 1 + Phase 2 output. Renamed from `HANDOFF.md` 2026-04-27 to disambiguate from the `/session-handoff` skill convention. |

### Supporting infrastructure

| File | Purpose |
|---|---|
| [`scripts/extract_dialog.py`](scripts/extract_dialog.py) | Extracts clean dialog-only markdown from JSONL session logs. Used in Phase 1 corpus extraction; usable as the starting point for Step 4.5 if authorized. |

## 6. Open user-decision points (blocking)

Direct from [`STATUS.md`](STATUS.md) and [`phase-2-findings.md`](phases/phase-2/findings.md) §10:

1. **Step 4.5 corpus-signal filter authorization.** ~1–2 days of scripting to build the transcript-only harness ([`evaluation-framework.md`](phases/phase-2/evaluation-framework.md) §4) and run it across all 195 sessions. This is the primary blocker on empirically weighting Phase 2 rankings.
2. **Adopt Layer 1 foundation stack** in next kerf works? Low cost; high reviewer convergence.
3. **Adopt Layer 6 safe swaps** in next kerf works? Each is near-free; `load-bearing-token-readback` + `alternatives-considered-section` are the two cheapest.
4. **Authorize Layer 7 A/B experiments?** 5 candidates specified. Experiment #1 (numbered-close A/B) is the highest confirmation-value per hour.
5. **Review [`phase-2-kerf-integration-draft.md`](phases/phase-2/kerf-integration-draft.md) §8 open questions** before turning it into a kerf work.
6. **Retrospective outcome-join for Framing C validation** (kerf historical works × spec-revision-within-30-days)? Cheapest Framing C instrument available.

## 7. What's authoritative, what's provisional, what's deferred

- **Authoritative:**
  - [`CLAUDE.md`](CLAUDE.md), [`METHODOLOGY.md`](METHODOLOGY.md): governance rules. Reopening requires user sign-off.
  - [`research-statement.md`](phases/phase-1/research-statement.md): Phase 1 output; Phase 2's briefing.
  - [`evaluation-framework.md`](phases/phase-2/evaluation-framework.md): the evaluation instrument. Phase 2 established; durable beyond individual protocol recommendations.
- **Provisional (analytical, not corpus-filter-validated):**
  - [`phase-2-findings.md`](phases/phase-2/findings.md) rankings in §3 and §4. Treat as high-priority hypotheses, not settled findings.
  - All reviewer outputs in [`phases/phase-2/analysis/reviewer-*.md`](phases/phase-2/analysis/). Each carries `[filter-dep]` tags where applicable.
- **Draft (not to be acted on without user review):**
  - [`phase-2-kerf-integration-draft.md`](phases/phase-2/kerf-integration-draft.md).
- **Deferred:**
  - Step 4.5 corpus-signal filter execution.
  - All Layer 7 A/B experiments.
  - Behaviors-first plan-expression investigation (research-statement §8 flagged; Phase 2 did not fully explore).
  - Scope gaps flagged in the catalog (dependency-aware decomposition; research-scoping question-quality).

## 8. How artifacts relate

```
research-statement.md (Phase 1 output, Phase 2 briefing)
  │
  ├─► Phase 2 Step 1 ─► evaluation-criteria-refinement.md
  │                      + 3 sub-analyses
  │                      + evaluation-framework.md (durable output)
  │
  ├─► Phase 2 Step 2 ─► 10 × external-sources/<domain>.md
  │
  ├─► Phase 2 Step 3 ─► counter-pattern-candidates.md
  │
  ├─► Phase 2 Step 4 ─► unified-protocol-catalog.md (87 protocols)
  │                      ▲
  │                      │ (input to reviewers)
  ├─► Phase 2 Step 5 ─► 6 × reviewer-<frame>.md
  │                      ▲
  │                      │ (synthesized into)
  └─► Phase 2 Step 6 ─► phase-2-findings.md (main deliverable)
                        │
                        └─► Phase 2 Step 7 ─► phase-2-kerf-integration-draft.md
```

Phase 1 evidence (lens reports, corpus, tried-protocols) feeds research-statement.md and then informs the observed-pattern entries in the unified catalog. External sources + counter-patterns are independent inputs to the catalog. The catalog is the single consolidation point; reviewers operate on it; findings synthesize reviewer outputs.

## 9. Update discipline

- New findings go in new files or dated append-sections. Prior artifacts are not scratchpads; never overwrite.
- Every session updates [`STATUS.md`](STATUS.md) with a history entry before closing.
- This INDEX stays in sync with new artifacts: any new deliverable should be added to §5 (document map) with its purpose.
- Locked choices in [`METHODOLOGY.md`](METHODOLOGY.md) do not reopen without user sign-off.
