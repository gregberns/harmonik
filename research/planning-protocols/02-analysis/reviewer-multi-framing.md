# Reviewer Sub-Agent — Multi-Framing Scoring

> Phase 2, Step 5. Reviewer role: **multi-framing.** Task: re-score top-25 catalog candidates on the provisional framework and on the three rival framings (A: Commitment-Deferral, B: Mental-Model Coupling, C: Regret-Adjusted Outcome). Locate disagreements — they localize the "unnamed-but-important criterion" risk the user flagged.
>
> Scores are analytical predictions, not corpus-validated; Step 4.5 corpus-signal filter was not executed this session. Author: multi-framing reviewer sub-agent, 2026-04-23.

## 1. Frame definitions (brief)

- **Provisional (P).** Pair-graph: Writing-Alignment (W1/C1), Autonomy-Coupling (A1/A2/M1), Gaps-Alignment (G1/C2), Wall-clock-Completeness (T1/S1), Fuzziness-stability (F1/F2). Treats planning as attention-cost optimization.
- **Framing A — Commitment-Deferral.** Time-to-first-irreversible-commitment (later is better, within progress), branch-width at mid-plan, cost-of-reversal, drift-surfacing latency, exploratory yield. Treats planning as option-preservation.
- **Framing B — Mental-Model Coupling.** Prediction-accuracy across the gap, novel-case transfer, vocabulary convergence, explanatory reciprocity, decay robustness. Treats the spec as a byproduct of alignment.
- **Framing C — Regret-Adjusted Outcome.** Post-implementation regret rate, scope-drift detection rate, requirement-surfacing completeness, spec-durability, revisit-cost. Treats planning as a bet on future system quality.

Rubric: **High / Medium / Low / Negative** per framing, with one-sentence reason. "Negative" means the framing actively penalizes the protocol (worse than absent).

## 2. Candidate selection logic

The 25 candidates below were chosen to span all eight catalog groups and all four origin streams (observed, unexplored, counter-pattern, external), with preference for protocols that the catalog flags as either (a) high-value in multiple origins, (b) in cross-origin convergences, or (c) mechanistically interesting inverses. The selection deliberately includes several observed patterns that rank high on the provisional; if they score poorly on rivals, that is signal, not noise.

Selected:

1. `context-dump` (observed)
2. `autonomous-dispatch` (observed)
3. `upfront-decision-partition` (observed)
4. `pre-action-plan-disclosure` (observed)
5. `numbered-question-close` (observed)
6. `recovery-handoff` (observed)
7. `kerf-parallel-reviewer` (observed)
8. `dialog-log-plan` (observed)
9. `controller-orchestration` (observed)
10. `example-led-emergence` (counter-pattern:1)
11. `assumption-bundle` (counter-pattern:2)
12. `emergent-partition` (counter-pattern:3)
13. `micro-step-incrementalism` (counter-pattern:5)
14. `dialogic-context-accretion` (counter-pattern:6)
15. `question-preserving-autonomy` (counter-pattern:7)
16. `knowledge-state-inventory` (counter-pattern:8)
17. `behavior-first-plan` (unexplored)
18. `forced-choice-with-default` (observed+unexplored)
19. `commanders-intent` + `autonomy-scope-grant` (external:military) — scored as the stack
20. `ipass-opener` (external:medical)
21. `back-brief-plan-quality` (external:military)
22. `read-back-comprehension` (external:medical/aviation)
23. `premortem-reviewer` (external:design-review)
24. `alternatives-considered-section` (external:design-review)
25. `single-text-procedure` (external:negotiation-mediation)

Honorable mentions not scored (Step 5 should re-visit): `hypothesis-driven-ghost-deck`, `teach-back-loop`, `spin-sequence`, `engagement-letter-opener`, `role-split-reviewer-library`, `fixed-token-status-vocabulary`, `aar-four-question`, `tactical-pause`. Scored 25 is already enough to exhibit the full disagreement pattern.

## 3. Scoring table

| # | Protocol | Provisional | Framing A (Commitment-Deferral) | Framing B (Mental-Model Coupling) | Framing C (Regret) |
|---|---|---|---|---|---|
| 1 | `context-dump` | **High** — very few human turns, high S1 once delivered; but high W1 front-loaded. | **Low/Negative** — collapses the decision tree in one shot; branch-width at mid-plan near zero; drift-surfacing latency catastrophic (errors propagate furthest before catch, per Phase 1). | **Low** — one-way transfer; no teach-back; human's model goes in, agent's model unverified; decay robustness fine (brief is durable) but prediction-accuracy untested. | **Low/Negative** — no requirement-surfacing step; "errors propagate" is the canonical regret pattern; spec-durability high if brief was accurate, catastrophic if not (bimodal). |
| 2 | `autonomous-dispatch` | **High** — zero writing in session; A1/A2 near zero (never asks); assumes prior planning session did the work. | **Negative** — no branch-width because no planning happens; if prior spec was narrow, commitment is pre-collapsed; no drift-surfacing at all during execution. | **Negative** — zero coupling work in-session; explanatory reciprocity zero; any model drift is silent; decay robustness zero (starts cold next time). | **Low** — silent drift when spec is ambiguous; no in-session surfacing; regret concentrates on interpretation gaps the spec didn't cover. |
| 3 | `upfront-decision-partition` | **High** — pre-solves A1/A2; observed zero-cost corrections in 79a42399. | **Medium/Low** — partition itself is an early irreversible commitment about which decisions are "trivial"; rigidifies trivial category before problem shape is known. | **Medium** — vocabulary-align on decision classes; does not probe human's actual intent model; moderate coupling work. | **Medium/Low** — prevents over-asking (scope-drift reduction on trivial) but can miss architectural items that got mis-classified as trivial; regret-failure mode is silent mis-partition. |
| 4 | `pre-action-plan-disclosure` | **High** — observed zero-cost corrections; high S1, low C1 when working. | **Medium** — pre-commits to a framing early (counter-hypothesis #1's argument); preserves reversal-cheapness because execution hasn't started; drift-surfacing latency is good (plan is visible before work). | **Medium/High** — forced re-encoding exposes model; human sees what agent thinks; vocabulary convergence is high if human actually reads the plan. | **Medium** — catches mis-framings at pre-commit where rework cheap; does not specifically surface omissions or non-goals; spec-durability depends on plan quality. |
| 5 | `numbered-question-close` | **High** — observationally shortens subsequent human turn. | **Low** — enumeration biases question-generation toward well-framed questions; suppresses unframed-concern surfacing (contested by aviation CRM evidence). | **Low/Medium** — batching forces human to answer without explaining *why*; compresses coupling work; less teach-back signal. | **Low** — lulls into feeling-aligned without surfacing what *wasn't* numbered; classic "feels good in-session" signature. **TRAP CANDIDATE.** |
| 6 | `recovery-handoff` | **High** — high S1 at session start; low first-N-turn correction rate. | **Medium** — carries commitments across sessions (pre-committed); reversal-cost high because new session inherits the framing; drift-surfacing limited to what the payload captured. | **High** — explicit model-state artifact; decay-robustness is the direct value prop; explanatory reciprocity if human verifies the payload. | **Medium** — prevents silent context-loss; payload bloat risk; doesn't address regret directly but reduces mid-work reinvention. |
| 7 | `kerf-parallel-reviewer` | **Medium/High** — overhead moderate; catches issues pre-dispatch. | **High** — reviewers preserve branch-width (can raise "what about X?"); drift-surfacing latency reduced; cost-of-reversal low when caught pre-dispatch. | **Medium** — reviewers probe agent's model but not human's; coupling work partial. | **High** — k-reviewer redundancy is the direct regret-prevention mechanism; observed to already work in kerf. **SAFE CANDIDATE.** |
| 8 | `dialog-log-plan` | **Medium** — low overhead; low S1 (no durable artifact); high revisit-cost later. | **High** — minimal commitment; branch-width preserved; every option stays live in the chat. | **Medium** — shared chat state is coupling; but state is unstructured, not portable; decay robustness poor. | **Low** — plan lives in chat; spec-durability near zero; revisit-cost high; scope-drift not surfaced. **TRAP CANDIDATE** (feels fluid, regret-prone). |
| 9 | `controller-orchestration` | **Medium/High** — observed in long sessions; matches harmonik design intent; turn-count inflated. | **Low** — opener directive commits to controller framing early; branching structural but not in decision-space. | **Medium** — human-dominant on directive, bidirectional on status; coupling moderate. | **Medium** — orchestration-specific; regret tends to be at interpretation of directive, not plan-regret. |
| 10 | `example-led-emergence` | **Medium** — wall-clock inflated; concrete cases are cheap to correct. | **High** — explicitly delays abstract commitment; case-by-case reveals framing cheaply; branch-width high until post-hoc plan step. | **High** — concrete cases are the best vehicle for shared-model construction; disagreements crisp; novel-case transfer high. | **High** — scope-drift detection baked in (cases force specificity); spec-durability high (behavior-shaped specs match reality better). **SAFE CANDIDATE — HIDDEN GEM if provisional under-scored it.** |
| 11 | `assumption-bundle` | **Low/Medium** — agent-side writing cost high; human reads a dense artifact. | **High** — dependencies visible = reversal-cost low (cascade edits); drift-surfacing latency near zero at bundle moment. | **High** — explicit assumptions make the agent's model legible; human can verify term-by-term; vocabulary convergence forced. | **High** — requirement-surfacing is the explicit mechanism; the dependency graph IS the requirements map; high spec-durability. **HIDDEN GEM.** |
| 12 | `emergent-partition` | **Medium** — more interruption than upfront; partition artifact is new writing. | **High** — partition itself is deferred; decision-class is re-derived as evidence arrives; preserves branch-width on classification. | **Medium** — partition discussion is coupling work; but agent's decision-flagging judgment is the load-bearing skill. | **Medium/High** — reclassification rate signal; catches mis-partitions the upfront version freezes in. **HIDDEN GEM.** |
| 13 | `micro-step-incrementalism` | **Low** — continuous human attention; propose/confirm overhead dominates. | **High** — each step is minimally committing; rework bounded to one step; branch-width preserved per step. | **High** — read-back at each step; coupling constantly verified; explanatory reciprocity maximal. | **Medium/High** — rework near zero but total-attention-to-completion high; spec emerges as step-log (durable if logged). |
| 14 | `dialogic-context-accretion` | **Medium** — sum-of-writing claim is plausibly lower but unverified; first-turns low-quality. | **High** — context requests triggered by what narrow work revealed = late-commitment architecture; strong drift-surfacing. | **High** — agent's "context I wish I had" IS the coupling signal; forced articulation of agent's model gaps. | **Medium** — narrow-interpretation first outputs are regret-fodder on large tasks; on small tasks, near-ideal. |
| 15 | `question-preserving-autonomy` | **Medium** — queue-writing is agent-side overhead; checkpoint turn is heavier but batched. | **High** — ambiguities preserved as deferred commitments (not silently resolved); cost-of-reversal low at checkpoint; drift-surfacing not real-time but not lost. | **Medium** — queue reveals agent's uncertainty model; coupling work concentrated at checkpoints. | **High** — late-correction-count reduction is the explicit mechanism; this directly addresses `autonomous-dispatch`'s regret failure mode. **HIDDEN GEM.** |
| 16 | `knowledge-state-inventory` | **Low/Medium** — heavy open-session cost; inventory maintenance drift. | **Medium** — inventory commits vocabulary early (possibly too early); but inventory ratification is an explicit commitment event, not hidden. | **High** — the inventory IS the shared model; vocabulary convergence is the mechanism; decay-robustness excellent (inventory is portable). | **Medium/High** — concept-alignment up-front catches silent term-drift (the Phase 1 most-expensive-error class). |
| 17 | `behavior-first-plan` | **Medium** — unexplored in corpus; crisp artifact but requires behavior-enumeration work. | **Medium** — commitment is to behaviors not structure (structural decisions deferred to implementer); branch-width preserved on structure. | **High** — behaviors are the best vocabulary for shared understanding; disagreements crisp. | **High** — spec-durability high (behaviors match reality); scope-drift surfaced via case-coverage gaps; revisit-cost low. **SAFE CANDIDATE.** |
| 18 | `forced-choice-with-default` | **High** — reduces writing to silence-ratifies-default. | **Low/Negative** — silent default = silent commitment; if default is wrong, drift-surfacing latency is unbounded (human never has to intervene). | **Low** — zero coupling work on silence; one-way. | **Low/Negative** — the exact "feels cheap, produces regret" signature — wrong defaults ratified silently become spec. **TRAP CANDIDATE.** |
| 19 | `commanders-intent` + `autonomy-scope-grant` | **High** — observed-convergent with `upfront-decision-partition`; A1/A2 low. | **Medium** — intent is a commitment (early); but intent is *diagnostic* (end-state), so downstream decisions retain optionality within it. | **High** — forces principal to articulate *why*; back-brief (when paired) is teach-back; coupling architecture complete. | **Medium/High** — intent-articulation catches scope ambiguities; "how-leak" failure mode is a real risk; durability depends on intent quality. **SAFE CANDIDATE.** |
| 20 | `ipass-opener` | **Medium** — five slots is heavier than SBAR; mandatory synthesis is writing. | **Medium** — slots commit framing but synthesis move is a built-in drift-surface. | **High** — synthesis-by-receiver is the highest-coupling move in the catalog; direct teach-back. | **High** — 30% error reduction is outcome-validated (in medicine); contingency pre-auth specifically addresses novel-state regret. **SAFE CANDIDATE.** |
| 21 | `back-brief-plan-quality` | **Medium** — one extra turn, rich content. | **High** — plan-as-re-encoding exposes the latent commitment before execution; subordinate often self-corrects mid-brief. | **High** — the canonical plan-quality-against-intent coupling check; bidirectional (commander may discover intent was ill-stated). | **High** — require one named uncertainty per back-brief → direct requirement-surfacing. **SAFE CANDIDATE.** |
| 22 | `read-back-comprehension` | **Medium** — structurally short move; degrades to parroting without discipline. | **Medium** — readback catches mis-hearing at the cheapest-reversal moment. | **High** — highest direct operationalization of mental-model verification available; teach-back variant has outcome data. | **Medium/High** — reduces regret from transmission errors; does not address omission-regret. |
| 23 | `premortem-reviewer` | **Medium** — adds a review turn. | **High** — prospective-hindsight explicitly preserves unexplored branches (every named failure is a branch that was narrowing invisibly). | **Medium** — reviewer's model vs. plan's model; some coupling signal. | **High** — Klein's 30% gain in failure-cause identification is directly regret-predictive; adversarial-by-construction catches omissions. **SAFE CANDIDATE.** |
| 24 | `alternatives-considered-section` | **Medium** — writing cost on agent side; hard gate if enforced. | **High** — the entire mechanism is preserving alternatives in-artifact; direct operationalization of branch-width. | **Medium** — vocabulary-clarity via explicit rejection reasons; moderate coupling signal. | **High** — anchoring-on-first-solution is a named regret-driver; pro-forma risk is the failure mode but substance-checked version is high-signal. **SAFE CANDIDATE.** |
| 25 | `single-text-procedure` | **Medium/High** — critiquing is lower-effort than drafting; artifact-is-the-plan eliminates extract cost. | **Medium** — draft commits early but is explicitly revisable; single-text avoids dueling-drafts branch-collapse. | **Medium** — human sees agent's model every round; critique-based. | **High** — spec-durability extremely high (document IS the spec); revisit-cost low; direct fit for spec-first posture. **SAFE CANDIDATE.** |

## 4. Top disagreements (ranked by importance)

Listed in descending order of signal-for-further-investigation. Disagreement definition: provisional vs. rival framing score differs by two rubric-steps (High↔Low or Medium↔Negative) or provisional ≥ Medium and rival = Negative.

### Tier 1 — Highest-signal disagreements

1. **`numbered-question-close` — Provisional High, Framing A Low, Framing C Low.**
   Interpretation: the provisional framework measures the subsequent-human-turn-length signal and rewards it; Framing A sees the enumeration suppressing unframed-concern surfacing; Framing C sees "feels aligned because batched" without outcome-validation. Aviation CRM evidence is a live external contradiction. **This is the exemplary trap.** The observed pattern looks good on the provisional because the provisional was derived from pain-points the user could articulate; the patterns that fail silently (Phase C) don't register in the provisional by construction.

2. **`context-dump` — Provisional High, Framings A/C Low-or-Negative.**
   Phase 1's own observation ("errors propagate furthest before detection") is the Framing A/C argument already in the user's corpus. That the provisional still scores it High is itself a framework diagnosis: provisional rewards "few human turns" and is blind to what happens *after* the turns stop.

3. **`autonomous-dispatch` — Provisional High, Framings A/B/C Low-or-Negative.**
   Three rivals agree this is a regret-shifter, not a regret-reducer: it moves the work off the planning session (where provisional measures) into the interpretation phase (where provisional doesn't look). User already expressed the concern ("solution to time-constrained decision-delegation"); the rivals name it mechanistically.

4. **`forced-choice-with-default` — Provisional High, Framing A Low/Negative, Framing C Low/Negative.**
   Silent-default = silent-commitment = inverted drift-surfacing: the protocol *hides* the commitment by making intervention the exceptional path. The exact "feels cheap, produces regret" shape the user flagged.

### Tier 2 — Upside disagreements (rivals reveal value provisional misses)

5. **`example-led-emergence` — Provisional Medium, Framings A/B/C High.**
   The provisional penalizes the wall-clock inflation and the many concrete turns; all three rivals see the same pattern as the *mechanism* (late commitment, shared-model-via-case, behavior-shaped durable specs). If corpus-signal filter ran, this would likely be the protocol where observational support is weak (user hasn't tried it) but analytical support across rivals is strong. **Classic hidden gem.**

6. **`assumption-bundle` — Provisional Low/Medium, Framings A/B/C High.**
   Agent-side writing cost and bundle-density depress the provisional; all three rivals see bundle-form as doing what each values: deferring single-assumption commitment while surfacing dependencies (A), making agent's model legible (B), requirement-mapping (C). The provisional under-reward is structural: the writing is on the agent side, not the human side, but provisional W1 doesn't distinguish.

7. **`question-preserving-autonomy` — Provisional Medium, Framings A/C High.**
   Provisional sees a medium-cost protocol; Framing A and C see it as the precise regret-antidote for `autonomous-dispatch`. The late-correction-reduction mechanism is invisible to provisional because it operates across sessions.

8. **`behavior-first-plan` — Provisional Medium, Framings B/C High.**
   Unexplored in corpus so provisional has no lift; the rival-framings unanimously score durable-behavior-spec architectures high. If recommended, this is a high-leverage adoption.

### Tier 3 — Within-expected-range agreement (low signal)

Protocols where the provisional and all three rivals agree within one rubric step are "safe bets" (if all High) or "skip" (if all Low-Medium). These include: `kerf-parallel-reviewer`, `back-brief-plan-quality`, `ipass-opener`, `premortem-reviewer`, `alternatives-considered-section`, `single-text-procedure`, `commanders-intent+autonomy-scope-grant`. These are the recommended floor.

## 5. Safe candidates (High on provisional AND High on ≥ 1 rival framing)

Protocols where independent framings converge → strongest bets.

- **`kerf-parallel-reviewer`** — Provisional Medium/High, Framing C High. Already adopted; evaluation should focus on role-split refinement (see `role-split-reviewer-library`).
- **`back-brief-plan-quality`** — Provisional Medium, Framings A/B/C all High. Single highest-convergence protocol in the set.
- **`ipass-opener`** — Framings B and C High; outcome-validated externally (30% medical-error reduction).
- **`premortem-reviewer`** — Framing A and C High; Klein's 30% failure-cause lift.
- **`alternatives-considered-section`** — Framings A and C High; directly addresses anchoring.
- **`single-text-procedure`** — Framings C High, provisional Medium/High; fits spec-first posture.
- **`commanders-intent` + `autonomy-scope-grant`** — Provisional High, Framings B/C High; observed-external-convergent.
- **`recovery-handoff`** — Provisional High, Framing B High; already in use; reinforce.

## 6. Trap candidates (High on provisional, Low or Negative on Framing C)

The precise failure mode the user named. Protocols that feel good in-session but the outcome-framing predicts regret.

- **`context-dump`** — Provisional High, Framings A/C Low/Negative. Phase 1 already has the observation.
- **`autonomous-dispatch`** — Provisional High, Framings A/B/C all Low/Negative. Three-rival convergence.
- **`numbered-question-close`** — Provisional High, Framings A/C Low. External counter-evidence (aviation CRM).
- **`forced-choice-with-default`** — Provisional High, Framings A/C Low/Negative. Silent-default = silent-commitment pathology.
- **`dialog-log-plan`** — Provisional Medium, Framing C Low. Not scored High on provisional but widely-used dominant corpus pattern; the trap here is sociological (the user's default), not score-based.

Note: `upfront-decision-partition` is borderline-trap (Provisional High, Framing C Medium/Low) because it freezes the trivial-vs-critical partition pre-evidence. Recommend investigating paired with `emergent-partition` (counter-protocol) as an A/B.

## 7. Hidden gems (Medium on provisional, High on Framing A or B)

Protocols the provisional under-rewards; rival framings reveal value.

- **`example-led-emergence`** — Provisional Medium, Framings A/B/C all High. Highest-leverage hidden gem. Addresses Phase 1 most-expensive-error (silent framing drift) via case-cheapness.
- **`assumption-bundle`** — Provisional Low/Medium, Framings A/B/C High. Agent-writing-cost masks its value on provisional; the dependency-graph mechanism is under-appreciated.
- **`emergent-partition`** — Provisional Medium, Framing A High. Re-derived partitions catch the mis-partition the upfront version freezes.
- **`question-preserving-autonomy`** — Provisional Medium, Framings A/C High. Direct antidote to autonomous-dispatch regret.
- **`dialogic-context-accretion`** — Provisional Medium, Framings A/B High. Late-commitment coupling architecture.
- **`knowledge-state-inventory`** — Provisional Low/Medium, Framing B High. Vocabulary-convergence machinery.
- **`micro-step-incrementalism`** — Provisional Low, Framings A/B High. Expensive on attention but near-zero rework.

## 8. Closing — 3-5 candidates most worth investigating

Ranked by magnitude and directness of framing-disagreement, plus leverage if the disagreement resolves in the rival's favor.

1. **`numbered-question-close`** (Provisional High, Framings A/C Low; external CRM counter-evidence). The exemplary provisional-local-maximum. An A/B against `single-most-important-question close` or `slot-acknowledgment close` (NE-6 already-strong observational signal, aviation CRM analog) is the recommended first A/B per evaluation-framework §8.3 and should be run through this multi-framing lens. If the numbered-close wins on provisional and loses on Framing C (late-session-issue-density), that is direct evidence that the provisional framework has a load-bearing blind spot.

2. **`example-led-emergence`** (Provisional Medium, all rivals High). Highest-leverage hidden gem. Unexplored in corpus, so adoption requires deliberate A/B, but three-framing convergence is as strong an analytical signal as the catalog exposes. If the user's current practice is context-dump or pre-action-plan-disclosure, swapping in example-led on one founding-vision session is the high-information-value experiment.

3. **`autonomous-dispatch` vs. `question-preserving-autonomy`** (direct protocol pair with opposite framing-scores). The observed dominant pattern vs. its counter-hypothesis. Running a matched A/B pair on a secure-dev-style task isolates whether the late-correction regret the rivals predict is actually observable. This is the highest-stakes comparison because the user's time-constrained-decision-delegation solution lives here.

4. **`assumption-bundle` on a founding-vision session** (Provisional Low, all rivals High). Counter-hypothesis #2 has no observational support but strong multi-framing analytical support. Running once on a harmonik-scale founding session (the exact task where context-dump is currently the default) would measure whether the dependency-graph structure is legible enough to the user to justify the agent-writing cost.

5. **`upfront-decision-partition` vs. `emergent-partition`** (observed successful pattern vs. its counter-protocol). The provisional loves the observed version; Framing A prefers the counter. Reclassification-rate is the exact discriminator, and the observational corpus has the upfront variant at n≥1. One kerf work with emergent-partition produces a direct comparison point.

These five are where the "unnamed-but-important criterion" the user flagged is most operative. Every one of them has the shape: the observed/dominant pattern scores well on the provisional (by construction, since the provisional was derived from observed pain-points), and at least one rival framing scores it poorly in a way that predicts the regret-class the user already suspects exists. The multi-framing discipline is doing its job — localizing the risk without having to name it directly.

---

**Methodological caveat (repeat from header):** all scores above are analytical predictions, not corpus-validated. Step 4.5 corpus-signal filter would bring observational support into the ranking; until it runs, these scores bear the weight of reasoning-not-measurement. Where the multi-framing scoring disagrees with eventual corpus evidence, the corpus evidence is the tiebreaker and this review should be updated.
