# Planning Protocols -- Evaluation Framework

> First-class deliverable committed in Phase 2 Step 1 of the planning-protocols research track. Specifies *how* planning protocols are to be evaluated: the operational criteria, the transcript-only measurement instruments, the pipeline structure (generate → filter → confirm), the multi-framing scoring requirement, the practitioner-diagnostic signal catalog, and the templates for post-Phase-2 A/B and test-suite work.
>
> This document is the durable instrument. Candidate protocols come and go; the framework is the spine every candidate plugs into.
>
> Written as a specification. Not all procedures in §6–§9 will be executed during Phase 2; they are committed so that later work can pick up without re-deriving them.
>
> Last updated: 2026-04-23. Derived from [02-analysis/evaluation-criteria-refinement.md](02-analysis/evaluation-criteria-refinement.md) and its three sub-analyses.

## 1. Purpose and scope

This framework answers: **how do we know a planning protocol is better than another?** It provides:

- An operationalized set of criteria (§3) that replace and refactor the provisional list in research-statement §2.
- Transcript-only measurement instruments (§4) that can be scripted against the existing 195-session corpus with no user follow-up.
- A pipeline structure (§2) that matches evaluation effort to hypothesis confidence.
- A multi-framing scoring requirement (§5) as a guardrail against local-maxima anchoring on the provisional framework.
- Specifications for post-Phase-2 empirical instruments: practitioner diagnostics (§6), simulation sweeps (§7), single-feature A/B (§8), and a test-suite of planning problems (§9).
- An explicit statement of what the framework cannot measure at solo-user scale (§11).

The scope: evaluating planning protocols for a single senior solo developer (the research-track user) working with a coding agent on software projects. Multi-user generalization is explicitly out of scope.

## 2. Pipeline structure

The framework operates as a three-stage pipeline. Stages are ordered by cost-of-hypothesis-test, cheapest first:

| Stage | Mechanism | What it produces | Cost |
|---|---|---|---|
| **Generate** | Corpus lenses + external-source pass + counter-pattern design | Candidate protocols with dimension-values and predicted trade-offs | Sub-agent budget; no user time after Phase 1 |
| **Filter** | Transcript-only harness over all 195 sessions (§4); simulation sweeps of mechanistic sub-features (§7) | Quantitative signal on which candidates have any corpus support; mechanistic sub-protocol effect sizes | 1–3 days of scripting / simulation setup |
| **Confirm** | Matched-pair A/B trials (§8); test-suite runs (§9); retrospective outcome joins (§6.3) | Counterfactual evidence on the top 1–2 filtered hypotheses | Multi-week user commitment; user-authorization-gated |

Phase 2 executes Generate (Steps 2–4) and Filter-via-harness (Step 4.5). Simulation, A/B, and test-suite are specified here for later execution with user authorization.

Three pipeline rules:

1. **Filter before ranking.** Step 5 reviewer evaluation must receive corpus-signal-filtered candidates, not unfiltered ones. Ranking without filtering reverts to analytical-judgment-only and re-exposes the local-maxima risk.
2. **Confirm narrowly.** Only 1–2 hypotheses per round reach Confirm stage. Attempting to confirm broadly exhausts the user's real-session budget without resolving the most important cases.
3. **Pipeline is iterative.** A/B or test-suite results that disagree with filter predictions feed back into the harness (as new natural-experiment pairs) and into the catalog (as candidate-invalidation records).

## 3. Operationalized criteria (pair-graph)

Replaces the priority-ordered list in research-statement §2. Every reported score must include both members of its pair — single-criterion scores are gameable in isolation and explicitly forbidden.

### Pair P1 -- Writing-Alignment

- **W1. Human writing effort, category-decomposed.** Characters per human turn, tagged by category (a–h from Phase 1 writing-load lens: framing, correction, decision-response, clarification, scope-expansion, etc.). Report as category-weighted totals, not raw sum.
- **C1. Correction-cycle count, subtyped.** Count of framing corrections, content corrections, and same-thread repeated corrections as three separate numbers.

**Interpretation:** low W1 + low C1 = protocol reduced effort without hiding alignment work. Low W1 + zero C1 = suspicious (silence, not alignment). High W1 + low C1 = rich dialog, may be appropriate for founding sessions. High W1 + high C1 = protocol failure.

### Pair P2 -- Autonomy-Coupling

- **A1. Over-ask count.** Number of agent questions on topics in the trivial-topic taxonomy (file layout, naming, small split-vs-single decisions). Detected via lexicon + topic classifier.
- **A2. Autonomy-grant latency.** Turn index at which the human first grants blanket autonomy ("whatever", "your discretion", "don't care about this stuff"). Session-opener = 1; late = 3+. Lower is better (protocol pre-authorized rather than patched).
- **M1. Framing-correction count.** Subset of C1 (above) where the human contests the agent's *framing*, not its content. Best transcript-only proxy for mental-model-coupling failure. Phase 1 flagged these as the most expensive correction class.

**Interpretation:** low A1 + low A2 + low M1 = coupling is working (agent is deciding right things autonomously and modeling user intent correctly). Low A1 + high M1 = agent is quiet but wrong about intent. High A1 + low M1 = agent over-asks but stays aligned.

### Pair P3 -- Gaps-Alignment

- **G1. Context-switch gap statistics.** Median inter-turn gap, p95 gap, long-gap count (gaps > threshold, e.g. 10 minutes), and short-ping-pong count (turns < 1 minute apart).
- **C2. Mid-gap alignment-check count.** Agent turns within or at the boundary of a long gap that explicitly surface a decision or checkpoint for the user.

**Interpretation:** long gaps without alignment checks = error-compounding. Long gaps with checkpoint-surfacing = correct context-switch use. Short-ping-pong rate is a corrosive-pattern indicator orthogonal to either.

### Pair P4 -- Wall-clock-Completeness

- **T1. Wall-clock time.** Active (sum of inter-turn gaps under threshold) and calendar (first-to-last timestamp).
- **S1. Spec-completeness proxy.** One of: implementer-time-to-first-blocker (if implementation has started), checklist-coverage against locked-decision-list, or count of TODO / decide-later / FIXME markers in the produced spec.

**Interpretation:** fast + complete = good. Fast + incomplete = premature convergence. Slow + complete = rich planning. Slow + incomplete = protocol failure.

### Elevated criterion -- Fuzziness stability

- **F1. Opening-turn fuzziness-index.** Lexical hedging-language density in turns 1–3 ("maybe", "I'm not sure", "either way", "haven't decided"). Establishes the starting fuzziness bucket.
- **F2. Fuzziness-resolution latency.** Turns from opening until the session reaches a decision the human does not hedge.

**Interpretation:** protocols that help fuzzy-intent sessions firm up gracefully, without forcing premature commitment, score well. Protocols that collapse fuzziness prematurely may look fast but produce regret later — this pair must be cross-checked against outcome signals (§6.3).

### Outcome-side sanity check (expensive, retrospective)

- **R1. Spec-revision-within-30-days rate.** Cheapest real-world proxy for post-implementation regret. Applied retrospectively to kerf's historical works via git joins.
- **R2. Implementer-time-to-first-blocker.** Time between first implementation commit and first later session that reopens a spec decision. Signal of spec-thinness.

These are not per-session criteria but corpus-level corrections. They tell you whether the in-session metrics correlate with downstream outcomes; if they don't, the in-session metrics are measuring the wrong thing.

### Criteria explicitly demoted

These were in the provisional framework or candidate-addition list but are too expensive, noisy, or confounded to use as quantitative session scores. They remain as *qualitative overlays* for Step 6 commentary but do not carry numeric weight:

- **Downstream implementation quality** -- too noisy for solo development without external reviewers.
- **Downstream maintainability** -- too far from planning-protocol variation; dominated by implementation confounds.
- **Trust accumulation over time** -- under-powered at 10-session scale.
- **Robustness to user-state** -- user state mostly unobservable from transcripts; usable only as natural-experiment stratification (late-night vs daytime sessions).
- **Transferability** -- becomes a *requirement-on-the-map* in Step 6 (does the recommendation map cover observed task types?), not a per-protocol score.

## 4. Transcript-only measurement harness

All pair-graph metrics above are derivable from JSONL transcripts with no user follow-up. The harness composes:

### 4.1 Feature extraction

For each session:

1. **Text-turn decomposition** (extend existing `scripts/extract_dialog.py`): separate human-text turns (`type=="user" AND isSidechain==false AND content-is-string`) from agent-text turns (`type=="assistant" AND isSidechain==false`) and from machine turns (tool results, sidechain sub-agent events, paste wrappers).
2. **Per-turn features:** character count, word count, has-question (regex + enumerated-choice detector), has-hedging-language (lexicon), turn-close-category (numbered-list / open-ended / declarative / mixed), category-a-through-h tag for human turns (from writing-load lens taxonomy).
3. **Inter-turn features:** gap duration (timestamp diff), tool-call count in intervening agent work, sidechain-event count.
4. **Incident detection:** correction incidents (pushback-lexicon match at human turn start: "wait", "no", "actually", "that's not", "I don't think", "you're asking me X but"); wasted-question incidents (human response to an agent question with lexicon-match on "whatever", "your discretion", "I don't care", "fine"); autonomy-grant events (blanket-grant language).
5. **Opener features:** context-dump (single human turn > 1500 chars at turn 1), recovery-handoff (structured header tokens in turn 1), autonomy-partition (turn 1 contains both "trivial" and "ask me" or equivalent), never-ask-template (system-prompt match).

### 4.2 Metric computation

Per session, compute the pair-graph numbers (W1/C1, A1/A2/M1, G1/C2, T1/S1, F1/F2). Per protocol (when sessions are tagged with protocol identity), aggregate across matched-task subsets.

### 4.3 Natural-experiment tests

For each protocol feature with observational support, run the relevant natural-experiment test from the empirical-design sub-analysis (§1 of that document):

- NE-1: autonomy-partition opener (1 vs 9 sessions in primary corpus).
- NE-2: secure-dev never-ask template (~100 template-dispatched vs 3 dialog-opener sessions in secure-dev).
- NE-3: pre/post kerf reviewer adoption (time-series split on adoption event).
- NE-4: within-session phase shifts in f588ff0c (short-volley → dispatch).
- NE-5: context-dump (13493c8d) vs dialog-first (3bf5774c) on matched harmonik work.
- NE-6: numbered-question-close vs open-ended-close (every agent turn is a data point — highest-n test in the corpus).
- NE-7: template-dispatch leakage (never-ask sessions with human intervention — tells you which questions are so load-bearing that a strong no-question clause fails).

### 4.4 Output format

Harness emits, per session and per protocol aggregate:

```
session_id, protocol_tag, task_type, W1_by_category, C1_by_subtype, A1, A2, M1,
G1 (median/p95/long-count/ping-pong-count), C2, T1 (active/calendar), S1, F1, F2
```

Plus a corpus-level `natural-experiment-report.md` with per-NE effect sizes and confidence bands.

### 4.5 Harness scope discipline

The harness is **transcript-only** by design. It does not:
- Ask the user anything.
- Read implementation artifacts (spec files, commits) except for the explicit R1/R2 outcome joins.
- Apply interpretive judgment — all classifiers are lexical or small fixed taxonomies.

Anything the harness cannot measure is noted as a qualitative overlay, not forced into a number.

## 5. Multi-framing scoring requirement

Every top-ranked protocol in Step 6 must be independently scored on at least one rival framing (from [rival-framings sub-analysis](02-analysis/evaluation-criteria-refinement.sub-rival-framings.md)):

- **Framing A — Commitment-Deferral.** Time-to-first-irreversible-commitment, branch-width at mid-plan, cost-of-reversal, drift-surfacing latency, exploratory yield.
- **Framing B — Mental-Model Coupling.** Prediction-accuracy across the gap, novel-case transfer, vocabulary convergence, explanatory reciprocity, decay robustness.
- **Framing C — Regret-Adjusted Outcome.** Post-implementation regret rate, scope-drift detection rate, requirement-surfacing completeness, spec-durability, revisit-cost.

### Rules

1. **Choose the framing that maximizes expected disagreement with the provisional.** If a candidate scores well on the provisional (low writing effort, few corrections), score it also on Framing C (would this plan produce regret?). If a candidate scores well because it produces rich mutual-understanding artifacts, score it also on the provisional (is the cost justified?).
2. **Agreement is noisy; disagreement is diagnostic.** When the provisional framework and a rival both rank a candidate high, that candidate is a safe bet. When they disagree, investigate *why* — the disagreement localizes where the "unnamed-but-important criterion" the user flagged is operative.
3. **Any final recommendation functionally identical to an observed pattern must cite a considered-and-rejected alternative from either Step 2 (external-source) or Step 3 (counter-pattern), and must record the rejection reason.** This is the research-statement §7 discipline, preserved and extended here.

### Where operationalizations for framings A/B/C exist

- Framing A: drift-surfacing latency and cost-of-reversal are transcript-computable (they are subsumed by Pair P1's correction subtyping plus the turn-index at which a correction refers back to). Branch-width and exploratory yield require hand-judgment or LLM-rater.
- Framing B: vocabulary convergence is transcript-computable via term-usage tracking across turns. Paraphrase-accuracy is detectable lexically. Prediction-accuracy and novel-case transfer require post-session probes (expensive; see §6).
- Framing C: spec-revision rate is the cheapest operational form (R1). Requirement-surfacing completeness requires outcome-linked analysis. Scope-drift detection rate is computable via scope-mention count at plan vs implementation time.

## 6. Practitioner-diagnostic signal catalog

Alongside the batch-evaluation harness, the framework specifies an in-session diagnostic layer. The goal: during or shortly after an active planning session, surface signals the user can act on *now*, not after a multi-week evaluation cycle.

### 6.1 Signals

Six initial signals (extensible):

| Signal | Trigger condition | Interpretation |
|---|---|---|
| **Numbered-close rate low** | Agent-turn-close numbered-question rate < 40% in last 10 agent turns (historical median ~70%) | Agent may have drifted from short-volley mode; check if batched vague questions have crept in. |
| **Correction burst** | ≥ 2 correction incidents in the last 5 human turns | Alignment is slipping. Most-recent correction type (framing vs content) narrows the cause. |
| **Decision-deferral high** | ≥ 40% of recent agent questions received wasted-question-count responses | Agent is over-asking. Add autonomy-partition clause to the opener of the next session. |
| **No autonomous stretches** | Longest autonomous agent run under 5 minutes in a session that had context-dump opener | Protocol-intent mismatch: opener said "run autonomously," agent is still asking. |
| **Framing-correction depth** | A single framing correction has spawned ≥ 2 follow-up corrections on the same topic | Silent-framing-drift is expensive; pause and reset the vocabulary explicitly. |
| **Fuzziness-not-resolving** | Opening fuzziness-index high, still high at turn 10+ | Either the task really is exploratory (fine) or the protocol isn't helping the human firm up (failure). Qualitative check. |

### 6.2 Delivery

Options for how these signals reach the user:
- **Post-session digest.** Weekly script runs over the recent session logs and emits a diagnostic report.
- **Mid-session nudges.** Harder to deliver inside Claude Code today without custom tooling, but the signals are computable fast enough to surface within a few turns if instrumented.
- **Session-opener hint.** At the start of a planning session, surface the "what drifted last time" signal so the opener can compensate.

Only the post-session digest is specified here as committable now. Mid-session nudges and session-opener hints are noted as extensions.

### 6.3 Outcome-side sanity check (retrospective)

The diagnostic layer's value is limited if the signals don't correlate with actual planning quality. A monthly retrospective joins signal values to outcome signals (R1 spec-revision rate; R2 implementer-time-to-first-blocker) on completed works and reports:

- Which signals best predicted spec-revision events?
- Which sessions had "all signals green" but produced work the user later regretted? (These are the failure-mode diagnoses.)
- Which signals are redundant?

This loop calibrates the diagnostic catalog over time. First pass cannot be run until the corpus has enough recent sessions with downstream implementation to join — likely after 3–6 months of ongoing use.

## 7. Simulation sweep template

For mechanistic sub-protocols where the effect is narrow enough that an LLM-roleplay-user can credibly stand in for the real user, simulation sweeps permit high-n tests cheaply.

### 7.1 Applicable targets

Simulation is appropriate for:
- Numbered-close vs open-close (NE-6 extended).
- Sensitivity to opener-length / opener-structure.
- Agent self-correction rate under different decision-delegation opener clauses.
- Paraphrase-accuracy trends under teach-back vs non-teach-back prompts.

Simulation is **not** appropriate for:
- Anything whose primary outcome is real-user writing effort.
- Anything measuring latent-intent alignment.
- Anything context-switch-load dependent.

### 7.2 Setup

1. **Calibrate a roleplay user** against the corpus. Sample the real user's actual opening turns, pushback patterns, vocabulary. Build a persona prompt that reproduces the distribution. Validate by having the roleplay-user respond to 3–5 real planner openings and checking that simulated responses fall within the real-response distribution (e.g., hedging-density, turn length, question-response-style).
2. **Isolate the planner** in a fresh session per trial; inject the protocol under test via system-prompt prefix or a CLAUDE.md-style directive.
3. **Fix the problem statement** — a single canonical planning problem, identical across trials.
4. **Sweep the target feature.** 50–200 trials per condition.
5. **Apply the transcript-only harness** (§4) to the simulated transcripts.

### 7.3 Claim scope

Simulation supports claims of the form: "protocol P produces distribution-shifted measurement Q versus protocol R, within a calibrated-user simulation." It does not support claims of the form: "protocol P is better for the real user."

### 7.4 Feasibility

~2–3 days of setup; API cost low at this scale (Sonnet/Haiku). **Requires user authorization** — API budget and code.

## 8. A/B trial template

For the top 1–2 hypotheses emerging from the Filter stage, a matched-pair A/B against the real user provides one counterfactual data point per pair.

### 8.1 Shape

- **Pair size:** 5–8 matched pairs (i.e., 10–16 real planning sessions). Fewer than 5 pairs gives insufficient signal; more than 8 pairs exceeds realistic solo-user budget.
- **Matching criterion:** same task shape (§9.1), similar complexity (e.g., both "new subsystem design" at 2–3 load-bearing decisions). Retrodictive-reconstruction pairs (real corpus sessions where the user's own original answer is known) simplify matching.
- **Randomization:** which protocol runs first per pair is randomized to counter order effects.
- **Cooldown:** a cool-down day between pair members to reset user state.
- **Blinding:** protocol identity is not blindable to the user during the session (the protocol *is* the interaction). But rating of the produced plan should be blinded — show anonymized transcripts to the user or a co-rater days later.

### 8.2 Pre-registration

Before trials begin, register:
- The specific protocol feature being tested.
- The outcome metric (ideally a single harness-computed number).
- The expected effect direction and minimum-effect-of-interest.
- The decision rule ("protocol A wins if metric Q is lower on ≥ 5 of 8 pairs and median effect exceeds X").

This is lightweight but essential — without pre-registration, any post-hoc analysis is interpretive, not confirmatory.

### 8.3 Recommended first A/B

Per the empirical-design sub-analysis: **numbered-question-close vs open-ended-close**, because the corpus (NE-6) already gives strong observational signal, the outcome metric (next-human-turn char count) is quantitative and zero-judgment, and 5–8 pairs suffice.

### 8.4 Feasibility

5–8 matched pairs ≈ 10–16 real planning sessions over 2–4 weeks. Requires user commitment to a multi-week trial window. **User-authorization-gated.**

## 9. Test-suite specification

The largest-scale empirical instrument. Committed as a spec; execution is post-Phase-2 work.

### 9.1 Problem shapes to cover

Eight canonical planning-problem shapes, drawn from the empirical-design sub-analysis:

1. New-subsystem design.
2. Refactor-across-modules.
3. Bug investigation.
4. Spec refinement.
5. Scope decomposition / roadmapping.
6. Integration planning.
7. Tool-selection / adoption.
8. Research scoping.

Minimum useful suite: 8 problems, one per shape, medium complexity. Better: 16 problems, two per shape at different complexity levels.

### 9.2 Problem sources

Mixed, per the empirical-design sub-analysis:
- **4 retrodictive reconstructions** from the existing corpus: strip dialog, keep opening framing, replay. The user's historical answer is the comparison point.
- **2 real harmonik backlog problems** burned for evaluation. High ecological validity; the cost is losing those problems for real use.
- **2 synthetic problems** from adjacent open-source projects with documented planning artifacts (RFCs, design docs). High novelty for the user.

### 9.3 Scoring

Composite:
- **M1 — Binary-property rubric.** 5–7 pre-registered binary properties derived from corpus analysis (e.g., "names the architectural decision explicitly," "states one assumption explicitly," "parks at most one decision," "produces an actionable next step"). Binary to minimize rater variance.
- **M3 — Blind rater scoring.** User + one LLM-as-rater (with adversarial prompt: "identify three ways this plan could be wrong"). Disagreement signals rubric-criterion weakness.
- **M2 — Downstream sanity check** on a subset (2 of 8 problems). Implement against the produced plan several weeks after evaluation; join rubric scores to outcome signals.

### 9.4 Anti-gaming

- Rubric criteria must be evaluable by a rater who does not know the protocol (blind the rater).
- Use the adversarial LLM rater as a catcher for surface-only criterion satisfaction.
- Rotate which rubric criteria are scored across trials so the protocol-under-test cannot optimize for all criteria simultaneously.
- The M2 downstream sanity check is the ultimate goodhart-catcher: a plan that hits the rubric but fails implementation exposes the rubric, not the protocol.

### 9.5 Feasibility

20–30 hours of prep + 16–24 real sessions (30 hours) = ~50–60 hours over 4–6 weeks. **Multi-session research commitment in its own right.** Executed only with explicit user commitment to the timeline.

## 10. Application to Phase 2

Within the Phase 2 session:

- **Steps 2–4** populate the unified candidate catalog (Generate stage of the pipeline).
- **Step 4.5 (NEW)** runs the transcript-only harness (§4) across all 195 sessions, ranking Step 4 candidates by corpus-signal support. This is the Filter stage of the pipeline, executable in the current session with ~1 day of work.
- **Step 5** reviewer evaluation operates on filtered candidates. Multi-framing scoring (§5) is applied here — every top candidate scored on at least one rival framing as well as the provisional.
- **Step 6** ranked recommendations are delivered as a map (when-to-use-what) with boundary conditions per task-type / session-phase / user-state. Each top recommendation includes its multi-framing scorecard and its corpus-signal support level.

Post-Phase-2 (user-authorization-gated):
- **Simulation sweeps (§7)** on 1–2 mechanistic sub-protocols identified in Step 5.
- **A/B trial (§8)** on the highest-leverage single feature.
- **Test suite (§9)** if the user commits to a 4–6 week evaluation project.

## 11. What this framework cannot measure

Stated explicitly so that no later session mis-scopes its claims:

- **Multi-user generalization.** Single-user evaluation cannot resolve whether findings apply to other users. Every claim this framework supports is of the form "for this user, on this corpus." Transfer to other users is a hypothesis, not a result.
- **True mental-model coupling.** Framing-correction count is a proxy, not the thing. The actual alignment of internal models between human and agent is inferred, never directly measured.
- **True regret.** Spec-revision rate is a proxy. The real-world value of the plans produced may differ from what revision rates indicate.
- **Long-term protocol effects.** Trust accumulation, erosion, learning effects over months are under-powered at 10-session scale. The framework reports trends but cannot distinguish signal from drift.
- **Causation.** Everything except the A/B stage is correlational. Natural experiments and simulation sweeps support hypotheses; only matched-pair A/B or test-suite trials support causal claims.
- **Ground-truth on what "a good plan" is.** The rubric in §9.3 makes a binary-property call that is itself a bet. The framework assumes those properties are load-bearing; if they turn out not to be, every downstream conclusion is suspect.

These limits are features, not bugs. The framework is deliberately modest about what it proves.

## 12. Versioning and durability

This framework is intended to outlast the specific Phase 2 protocol recommendations it produces. When recommendations change (new protocols added, old ones falsified), the framework may be refined but is unlikely to be replaced wholesale. Expected future edits:

- **Signal catalog (§6.1) expansion** as new diagnostic patterns emerge.
- **External-framing catalog (§5) expansion** as Step 2 produces external-source protocols that introduce new dimensions.
- **Rubric refinement (§9.3)** as A/B and test-suite trials calibrate which binary properties actually predict outcomes.

Expected non-edits:

- **Pair-graph structure (§3).** The commitment to reporting in pairs, not single numbers, is a methodological position that should not be weakened.
- **Pipeline discipline (§2).** Filter before Confirm, narrow Confirm before broad generalization — these are guardrails against the central research risk.

## 13. Open questions the framework surfaces

Carrying forward to later Phase 2 work or user attention:

- Can the harness (§4) be implemented within this Phase 2 session as part of Step 4.5? Or does it require its own scoped effort? Empirical-design estimated 1–2 days; user confirmation on whether to spend that time is the next action after Step 2.
- Does the user-state / task-type dependency invalidate single-pair-graph scoring, or merely require the ranked-map output Step 6 already commits to? Framework assumes the latter; the test-suite (§9) would confirm.
- Is there a Framing D (or more) that the three rival framings missed? Phase 2 Step 2 may surface new framings from external domains (e.g., incident command or medical handoffs may introduce framings not yet on the list). Framework can absorb additional framings in §5 without structural change.
