# Evaluation-Criteria Refinement (Phase 2, Step 1)

> Synthesis output for Phase 2, Step 1 of the planning-protocols research track. Interrogates the provisional evaluation criteria in `research-statement.md` §2 in light of the user's explicit flag that the correct criteria are uncertain. Consolidates three parallel sub-agent investigations (rival framings, operationalization audit, empirical-evaluation design). Decides: proceed to Step 2 without pausing, with refinements applied to §2 and a first-class companion document `evaluation-framework.md` committing the operational framework.
>
> Last updated: 2026-04-23

## 1. Sub-analyses

Three parallel sub-agents were run with distinct challenge frames to guard against convergence on the provisional framework:

- [`evaluation-criteria-refinement.sub-rival-framings.md`](evaluation-criteria-refinement.sub-rival-framings.md) — proposed three first-principles alternatives to the provisional framework (A: Commitment-Deferral, B: Mental-Model Coupling, C: Regret-Adjusted Outcome) and named four framings that were considered-and-rejected with rationale.
- [`evaluation-criteria-refinement.sub-operationalization-audit.md`](evaluation-criteria-refinement.sub-operationalization-audit.md) — audited each provisional criterion and each candidate addition on six dimensions (operationalizability, signal source, proxies, within- vs cross-session, ceiling/floor, interpretation dependencies). Produced cost tiers, trade-off pairs, and a keep/replace/elevate/demote recommendation set.
- [`evaluation-criteria-refinement.sub-empirical-design.md`](evaluation-criteria-refinement.sub-empirical-design.md) — identified 7 natural experiments in the existing 195-session corpus; designed a minimum-useful test suite; sized A/B feasibility for a solo user; assessed simulation and practitioner-diagnostic alternatives; ranked 6 recommendation tracks by ROI.

Those three documents are the substance. This synthesis extracts the cross-cutting conclusions and the decisions that change how Phase 2 proceeds.

## 2. Core findings

### 2.1 The provisional framework is measurable but individually gameable

Every primary criterion in §2 can be operationalized cheaply from transcripts alone. The audit lists transcript-only instruments for human-writing effort, correction cycles, context-switch gaps, wall-clock, question targeting (via *wasted-question count*), and agent-trivia autonomy (via *over-ask count* and *autonomy-grant latency*).

But each criterion is gameable in isolation:
- *Writing effort* → zero if the human writes nothing (no intent to plan against).
- *Correction cycles* → zero if the agent never takes initiative (pure read-back).
- *Context-switch gaps* → maximized by a single long autonomous run that eliminates alignment checking.
- *Trivia autonomy* → maximized by letting the agent decide architecture autonomously too.
- *Question targeting* → perfect if the agent asks zero questions (also catastrophic).

**Implication:** criteria must be reported in *pairs*, not individually. The audit names five required pairings (writing+corrections, autonomy+mental-model-transfer proxy, gaps+alignment, wall-clock+completeness, corrections+outcome-signal). This refactors §2 from a ranked list into a pair-graph.

### 2.2 Several Phase 1-derived metrics are stronger than their parent criteria

The audit discovered that two derived metrics from Phase 1 outperform the abstract criteria they were meant to operationalize:

- **Wasted-question count** (human responds to an agent question with "whatever / your discretion / I don't care") is a stronger instrument than either "targeting of agent questions" or "agent autonomy on trivia" in the abstract. It is lexically detectable, directly diagnostic, and collapses both criteria onto a single observable.
- **Autonomy-grant latency** (turn number at which the human grants blanket autonomy, as in 79a42399 H#1) captures how fast the human had to patch the agent's over-asking behavior. Session-openers with pre-authorized autonomy = latency 1; late grants = latency 3+.
- **Framing-correction count** (subclass of corrections where the human contests the agent's framing, not its content) is the best transcript-only proxy for mental-model transfer failure. Phase 1 already flagged framing errors as more expensive than content errors.

These three metrics move from Phase-1 artefacts to primary instruments in the refined framework.

### 2.3 Three rival framings offer complementary — not replacement — lenses

No rival framing fully displaces the provisional framework, but each catches failure modes the provisional framework cannot:

| Framing | Core premise | Failure mode it catches that provisional misses |
|---|---|---|
| **Provisional** | Attention-spend is the scarce resource; minimize it at matched output. | (baseline) |
| **A — Commitment-Deferral** | Good protocols defer irreversible commitments until the latest moment consistent with progress. | Context-dump scores well on provisional (low writing) but collapses the decision tree in one shot — errors propagate silently. Framing A penalizes this directly. |
| **B — Mental-Model Coupling** | The spec is a byproduct; aligned heads are the real deliverable. | Autonomous dispatch ("study specs, never ask") can produce correct output with zero coupling work — future sessions start cold, maintenance is harder. Framing B penalizes this. |
| **C — Regret-Adjusted Outcome** | Evaluate by what happens after implementation; session-internal measures cannot catch plans that feel good but miss real requirements. | The exact "unnamed-but-important criterion" the user worried about. Framing C catches protocols that are locally ergonomic but systematically produce wrong plans. |

**Implication:** Step 6 ranking must require every top candidate to be independently scored on at least one rival framing. A candidate that ranks high on the provisional framework but low on Framing C is *the* signal that the unnamed-but-important criterion is operative. Protocols where the framings *disagree* are the most informative cases, not the ones where they agree.

This is structurally the same guardrail already named in research-statement §7 ("any recommendation functionally identical to an observed pattern must cite a considered-and-rejected external or counter-pattern alternative"), extended from alternatives-level to framings-level.

### 2.4 Empirical evaluation is a pipeline, not a single event

The asymmetry identified by the empirical-design sub-agent is critical and not previously explicit: **hypothesis generation is cheap; hypothesis confirmation is expensive.** This argues for a three-stage pipeline:

1. **Generate broadly** — corpus analysis + external sources + counter-patterns (Phase 2 Steps 2–4 already cover this).
2. **Filter cheaply** — within-corpus comparative analysis over all 195 sessions (R1); LLM-simulation sweeps of mechanistic sub-protocols (R3); both confined to transcript-only or narrow synthetic-user signals.
3. **Confirm narrowly** — 5–8 matched A/B pairs on the top 1–2 filtered hypotheses (R5); test-suite runs on 2–3 protocols × 8 problems if the user commits (R4).

The filter stage is what the research hasn't yet structurally committed to. Without it, Step 5's reviewer evaluation would rank candidates on analytical judgment alone — a re-emergence of local-maxima risk, since reviewers draw on the same corpus intuitions the Phase 1 lenses produced.

**Implication:** A filtering step between Step 4 (unified catalog) and Step 5 (reviewer evaluation) is added to Phase 2 methodology — named **Step 4.5: Corpus-signal filter.** See §5 below.

### 2.5 A formal evaluation framework is a first-class Phase 2 deliverable

All three sub-agents converged on this, each from a different angle:

- Operationalization: "The transcript-only instruments can be bundled into a scriptable evaluation harness emitting a fixed numeric panel per session — this harness may itself be a Phase 2 deliverable on par with protocol recommendations."
- Empirical design: "The evaluation framework (R6) is the spine of this pipeline. It is the most valuable evaluation artifact Phase 2 can produce, because every future A/B, test-suite run, or practitioner-diagnostic session plugs into it. Candidate protocols come and go; the framework is durable."
- Rival framings: "Use provisional as the default measuring stick, but require every ranked candidate protocol to be independently re-scored on at least one rival framework" — a requirement that needs a framework document to live in.

**Decision:** a sibling document `evaluation-framework.md` is committed now. It specifies the operationalized criteria, transcript-only measurement procedures, multi-framing scoring requirement, diagnostic signal catalog, and the A/B / test-suite / simulation templates for post-Phase-2 work. Writing this document is the single largest Step-1 commitment; it is produced in the same session as this synthesis.

## 3. What changes in research-statement §2

The provisional §2 is not rejected. It is *refactored and supplemented*:

**Refactoring of primary criteria.** The five priority-ordered criteria are replaced by a pair-graph of transcript-only instruments that report in required pairs. Concretely:

- **Pair P1: Writing–Alignment.** *Human writing effort, decomposed by category* + *correction-cycle count, split into framing vs content vs repeated*. Report together; never report one alone.
- **Pair P2: Autonomy–Coupling.** *Over-ask count + autonomy-grant latency* + *framing-correction count* (the best transcript-only proxy for mental-model coupling failure). Report together.
- **Pair P3: Gaps–Alignment.** *Context-switch gap statistics (median, p95, long-gap count, short-ping-pong count)* + *mid-gap alignment-check count*. Report together; absence of the second invalidates the first.
- **Pair P4: Wall-clock–Completeness.** *Wall-clock time* + *spec-completeness proxy (implementer-time-to-first-blocker, or checklist coverage)*. Report together.

**Elevated: stability to human fuzziness.** From candidate addition → measured criterion. Operationalized via lexical fuzziness-index of opening turns and per-fuzziness-bucket performance on the pair-graph. Cheap and diagnostic.

**Demoted to qualitative overlays.** Robustness to user-state, trust accumulation, downstream maintainability — real concerns but too expensive or too noisy to measure quantitatively at 10-session scale. They remain as lenses for commentary in Step 6 recommendations but do not carry numeric weight.

**Replaced by outcome-linked proxy.** Post-implementation regret → *spec-revision rate within 30 days of first implementation commit*. Cheapest observable of the concept. Applied retrospectively to kerf's historical works as the outcome-side sanity check for corpus findings.

**Multi-framing scoring requirement.** Every top-ranked protocol candidate from Step 6 must be independently scored on at least one rival framing (A, B, or C). The framing used must be chosen to maximize expected disagreement with the provisional framework (e.g., Framing C for a protocol that minimizes within-session friction but produces rare downstream artifacts).

## 4. Escalation status: no pause required

The task specification explicitly allowed for pausing if Step 1 surfaced "fundamental issues with the criteria." None of the three sub-agents independently flagged such an issue:

- Operationalization: "No fundamental issues surfaced; Phase 2 may proceed to Step 2 (external sources)."
- Rival framings: "All three developed frameworks pass the 'produces testable predictions on the existing corpus' bar ... [recommend] running all four against the same 3-5 protocols and observing disagreement."
- Empirical design: produced a structured pipeline compatible with the remaining Phase 2 steps.

The refinements in §3 are replacements-by-proxy, decompositions, pair-graph restructuring, and a new framework deliverable — not rejections. Phase 2 proceeds to Step 2 (external-source pass) without pausing.

Three items *will* need user sign-off before execution but do not block Step 2:

1. **Running the corpus comparative analysis (R1 from empirical design)** — ~1–2 days of scripting + analysis over all 195 sessions. Proposed as **Step 4.5: Corpus-signal filter** in the updated methodology below.
2. **Committing to post-Phase-2 A/B trials (R5)** on the top 1–2 filtered hypotheses. Not required during this session; called out in `evaluation-framework.md` as a post-Phase-2 spec.
3. **Whether `evaluation-framework.md` as written here satisfies the "formal evaluation framework" research-statement §2 hypothesis**, or whether the user expected something different in shape. Surfacing it for review at session-end.

## 5. Updated Phase 2 methodology

The research-statement §7 step sequence becomes, with one insertion:

- Step 1: Criteria interrogation (this document, complete).
- Step 2: External-source pass (10 parallel sub-agents, one per domain).
- Step 3: Counter-pattern generation (one instance per counter-hypothesis in §6).
- Step 4: Unified candidate catalog (observed + unexplored + external + counter-pattern, on shared schema).
- **Step 4.5 (NEW): Corpus-signal filter.** Apply the transcript-only evaluation harness (specified in `evaluation-framework.md`) across all 195 sessions. Rank candidate protocols by the natural-experiment support each has in the existing corpus. Candidates with no corpus signal go to Step 5 with a flag; candidates with negative corpus signal are steel-manned in Step 5 rather than dropped. This step is a cheap filter and must not become a gatekeeper.
- Step 5: Reviewer-challenged evaluation (multiple reviewer sub-agents with distinct frames, one explicitly tasked to challenge observed patterns).
- Step 6: Ranked recommendations with boundary conditions, with multi-framing scoring requirement.
- Step 7: Kerf integration draft (deferred to user review).

Step 4.5 is what makes the framework shift (§2.4) concrete. Without it, the pipeline structure is aspirational.

**Scope note:** if Step 4.5 cannot be executed in the current session due to time, it should be queued as the explicit first action of the next session. Steps 5 and 6 should not run without the filter output. This is the pipeline discipline; skipping it returns to analytical-judgment-only ranking, which is exactly the failure mode the methodology is designed against.

## 6. What this Step 1 did not resolve

Carrying forward as open questions for later Phase 2 steps or for user attention:

- **The user-state / task-type dependency question.** All three sub-agents noted that "the right protocol" may be task-type-conditional. This becomes a requirement on the Step 6 deliverable (map of when-to-use-what) but is not an evaluation-criteria decision to make now.
- **The behaviors-first plan-expression question.** Research-statement §8 flagged this. It resurfaces in rival Framing C (which ranks behavior-first plan expression highly for spec-durability). Not resolved in this step; carry to Step 2 (see if external sources have mature behaviors-first protocols) and Step 4 (is this a dimension-value or a separate dimension).
- **Whether `evaluation-framework.md` scope matches user intent.** Written as the audit recommended; surfaced to user at session close for confirmation.
- **R1 corpus comparative analysis is within scope of this research track but touches a user-state question (authorization for extended scripting work across all 195 sessions).** Noted as `Step 4.5` in the revised methodology; user to confirm when raised.

## 7. Session discipline reminder

The two methodological guardrails in research-statement §7 that bind the remainder of Phase 2:

- **External-source pass (Step 2) must complete before any refinement of observed patterns.** The criteria refinement above is not a refinement of observed patterns — it is a structural commitment to how candidates will be scored. Step 2 remains untouched by Step 1.
- **Reviewer sub-agents in Step 5 must be explicitly instructed to challenge observed patterns.** The multi-framing scoring requirement in §3 above is the positive version of this same discipline: at least one reviewer pass uses a rival framing, not the provisional one.

With Step 1 complete and the escalation check negative, Step 2 begins next.
