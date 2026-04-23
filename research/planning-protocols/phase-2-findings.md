# Planning Protocols -- Phase 2 Findings

> Main output of Phase 2 of the planning-protocols research track. Produced after Step 1 (evaluation-criteria interrogation), Step 2 (external-source pass over 10 domains), Step 3 (counter-pattern generation), Step 4 (unified catalog of 87 protocols), and Step 5 (six reviewer sub-agents with distinct challenge frames). Step 4.5 (corpus-signal filter over all 195 sessions) was intentionally deferred — see §9.
>
> Presents: the refined evaluation criteria, the ranked recommendations with boundary conditions, the "safe swaps" and "high-leverage experiments," the composition-stack recommendations, and honest limits of the findings.
>
> The Phase 2 hypothesis that "a durable evaluation framework may be more valuable than any specific protocol recommendation" was confirmed; `evaluation-framework.md` is the most durable Phase 2 artifact. This document is subsidiary to that framework.
>
> Last updated: 2026-04-23

---

## 1. What Phase 2 produced

| Artifact | Path | Summary |
|---|---|---|
| Refined evaluation criteria | [02-analysis/evaluation-criteria-refinement.md](02-analysis/evaluation-criteria-refinement.md) | Provisional criteria → pair-graph of required-paired metrics; 3 rival framings (A/B/C); no fundamental rejections. |
| Evaluation framework | [evaluation-framework.md](evaluation-framework.md) | First-class deliverable. Pipeline (generate→filter→confirm), operationalized criteria, transcript-only harness, multi-framing requirement, A/B and test-suite specs. |
| External-source catalogs (10) | [02-analysis/external-sources/](02-analysis/external-sources/) | 10 domains, ~70 candidate protocols. |
| Counter-pattern candidates | [02-analysis/counter-pattern-candidates.md](02-analysis/counter-pattern-candidates.md) | 8 steel-manned counter-protocols. |
| Unified catalog | [02-analysis/unified-protocol-catalog.md](02-analysis/unified-protocol-catalog.md) | 87 distinct protocols on shared schema. |
| Reviewer evaluations (6) | [02-analysis/reviewer-*.md](02-analysis/) | Ergonomics / cognitive-load / fatigue-robustness / adaptability / challenge-observed / multi-framing. |

Scope the user should know: **every concrete finding below is an analytical prediction, not a corpus-filter-validated one.** Step 4.5 (corpus signal filter over 195 sessions) was not executed this session because it requires scripting work the locked choices constrain. The recommendations below should be treated as high-priority hypotheses for filter validation and targeted A/B confirmation, not as settled answers.

## 2. Evaluation criteria (refined)

**Pair-graph replaces the priority-ordered list.** Every evaluation reports both members of each pair; single-criterion scores are forbidden because each criterion is gameable in isolation.

- **P1 Writing–Alignment:** category-decomposed human writing + subtyped correction count (framing / content / repeated).
- **P2 Autonomy–Coupling:** over-ask count + autonomy-grant latency + framing-correction count.
- **P3 Gaps–Alignment:** gap stats + mid-gap alignment-check count.
- **P4 Wall-clock–Completeness:** wall-clock + spec-completeness proxy.
- **F1/F2 Fuzziness stability (elevated):** opening fuzziness-index + fuzziness-resolution latency.
- **R1/R2 Outcome sanity (retrospective):** spec-revision-within-30-days + implementer-time-to-first-blocker.

**Multi-framing requirement** (new): every top-ranked protocol must be independently scored on at least one rival framing —
- Framing A (Commitment-Deferral),
- Framing B (Mental-Model Coupling),
- Framing C (Regret-Adjusted Outcome).

When framings agree, the candidate is a safe bet. When they disagree, the disagreement localizes where the "unnamed-but-important criterion" the user flagged is operative.

Demoted to qualitative overlays (too noisy at 10-session scale): downstream implementation quality, downstream maintainability, trust accumulation, robustness to user state, transferability. These become commentary in §5 below, not numeric scores.

## 3. Cross-reviewer convergence

Six reviewers with distinct frames. Strong convergence on a small set of winners and losers across frames:

### 3.1 Convergent winners (pass ≥ 4 of 6 reviewer frames)

| Protocol | Origin | Why it wins |
|---|---|---|
| `commanders-intent` | external:military | Top on ergonomics, cognitive-load, adaptability; safe under multi-framing. One-paragraph opener naming end-state + key tasks + purpose. Pre-authorizes deviation-within-intent. |
| `back-brief-plan-quality` | external:military | Strongest four-framing convergence of any catalog entry. Agent's plan draft is restated against intent before execution; distinct from read-back (checks plan quality, not comprehension). |
| `autonomy-scope-grant` | observed+external | Pairs with commanders-intent. Explicit bounded grant ("decide X, ask on Y"). Top ergonomics, multi-framing safe. |
| `alternatives-considered-section` | external:design-review | Mandatory "alternatives & rejection rationale" slot in every spec draft. Multi-framing safe; adaptability widest; addresses silent-framing-commitment failure mode. |
| `role-split-reviewer-library` | external:design-review | Extension of kerf-parallel-reviewer: devil's-advocate + maintainer + simplifier + pre-mortem, each a named role. Top adaptability; multi-framing safe. |
| `premortem-reviewer` | external:design-review | "If this fails in 6 months, why?" reviewer sub-agent at plan-draft time. Absent from observed corpus; Framing-C safe. |
| `load-bearing-token-readback` | external:pilot-controller | Per-turn agent restatement of load-bearing tokens (not prose) to catch within-turn drift. Distinct from end-of-session synthesis. |
| `recovery-handoff` (augmented with closed-ack + watcher-tier) | observed+external | Top adaptability; survives the observed-pattern challenge with additions from medical/ICS handoff discipline. |
| `single-text-procedure` | external:negotiation | Agent maintains one persistent plan document; human critiques, never re-drafts. Top ergonomics; displaces `dialog-log-plan`. |

### 3.2 Convergent losers (fail ≥ 4 of 6 reviewer frames)

| Protocol | Why it loses |
|---|---|
| `numbered-question-close` (bare form) | Ergonomics demotes (suppresses unframed concerns); multi-framing trap (high provisional, low Framing A/C); external-source challenge: aviation CRM flags end-close "any questions?" as *anti-pattern* — interleaved slot-ack beats it, with decades of incident-archive evidence. This is the single strongest cross-frame displacement finding. |
| `autonomous-dispatch` "never ask questions" (bare form) | Multi-framing trap; challenge-observed rates as displaceable; external military doctrine is *opposed* to "never ask" (mission command relies on question-enabled judgment, not question-suppressed compliance). |
| `context-dump` (bare form) | Ergonomics demotes (upfront cost at worst moment); multi-framing trap. Redeemable only when paired with in-session correction checkpoints. |
| `dialog-log-plan` | Challenge-observed rates as displaceable by `single-text-procedure`; inconsistent with user's own spec-first posture (plan-in-chat is not the spec). |
| `forced-choice-with-default` | Ergonomics promotes (fast) but multi-framing trap (low Framing A: collapses decision tree). Use sparingly and only for truly-binary questions. |

### 3.3 Hidden gems (medium provisional, high on rivals)

| Protocol | Why it's under-rewarded |
|---|---|
| `example-led-emergence` (counter-pattern #1) | High on Framings A/B/C. Bug-investigation and founding-vision adapt well. No external analog in exactly this form, but Socratic `maieutic-drawout` and MI `elicit-provide-elicit` are mechanistically aligned. |
| `emergent-partition` (counter-pattern #3) | Genuinely novel; no external analog. Adapts the trivial/architectural split mid-session rather than up-front. Highest experimentation priority. |
| `assumption-bundle` (counter-pattern #2) | Agent proposes a dependency-graph of assumptions; human edits with cascading effects. External analog: MECE / issue-tree, but explicit dependency graph is distinctive. |
| `question-preserving-autonomy` (counter-pattern #7) | Agent runs autonomously but *preserves* questions in a visible queue. Displaces bare "never ask." Fatigue-robust; multi-framing safe. |
| `asynchronous-navigator` (external:pair-programming) | LLM-only adaptation. Reviewer sub-agent shadows the planning agent and surfaces concerns asynchronously. Ergonomics top, fatigue-robust. |
| `dialogic-context-accretion` (counter-pattern #6) | Agent pulls context via "context I wish I had" as narrow work reveals gaps. Multi-framing hidden gem; displaces `context-dump`. |

## 4. Ranked recommendations, by composition layer

The Phase 1 local-maxima-anchoring guardrail requires that any recommendation functionally identical to an observed pattern cite a considered-and-rejected alternative. That discipline is applied here: where an observed pattern is recommended, the reason for the recommendation over the alternative is stated.

### Layer 1 — Always-on foundation stack

Applicable across task types, session phases, and user states. These compose without conflict and together address the core Phase 1 pain points (framing drift, decision-deferral, writing overload).

1. **`commanders-intent` opener** (1–3 sentences: purpose + key tasks + end state).
   Considered-and-rejected alternative: `sbar-opener` (medical). Rejected because the S/B/A/R split doesn't map to coding-planning as cleanly as intent+tasks+endstate; kept available as a domain-specific variant for bug investigation sessions where Situation-Background-Assessment shape does fit.
2. **`autonomy-scope-grant`** (explicit bounded-autonomy clause in H#1).
   Considered-and-rejected: observed `upfront-decision-partition` without scope wording — the "decide X, ask Y" form is clearer than "trivial/critical" because it references the *work*, not a category. See §4.2 note on emergent-partition vs upfront-partition.
3. **`alternatives-considered-section`** in every produced spec.
   Considered-and-rejected: no observed alternative — this is a net addition. IETF RFCs, Nygard ADRs, and ATAM all converge on this slot; absent from observed corpus.
4. **`role-split-reviewer-library`** replacing generic kerf reviewer with named roles (devil's advocate, maintainer, simplifier, pre-mortem).
   Considered-and-rejected: observed `kerf-parallel-reviewer` with generic prompts. Rejected because generic reviewers double-count each other's outputs; named roles produce orthogonal critique with empirical support from design-review literature.
5. **`back-brief-plan-quality`** at plan-draft moments (before agent commits to implementation / major structural output).
   Considered-and-rejected: observed `pre-action-plan-disclosure` without structured back-brief. Rejected because bare plan-disclosure lacks a mechanism for the agent to check its own plan-quality — back-brief is the doctrinal name for that mechanism (military + medical independent convergence).

### Layer 2 — Session-opener, task-type-conditional

Choose one opener structure by task shape (from the 8 canonical shapes in evaluation-framework §9.1):

| Task shape | Recommended opener | Alternatives considered |
|---|---|---|
| 1. New-subsystem design | `commanders-intent` + `scqa-opener` (Situation/Complication/Question/Answer) + `assumption-bundle` | Context-dump (rejected: high upfront cost, brittle); SBAR (less fitted) |
| 2. Refactor-across-modules | `commanders-intent` + `utility-tree` (ATAM-derived) + `alternatives-considered-section` as skeleton | RFC full form (rejected: heavyweight per change); issue-tree solution-variant (secondary) |
| 3. Bug investigation | `sbar-opener` (Situation/Background/Assessment/Recommendation fits diagnostic work) + `hypothesis-driven-ghost-deck` | Commanders-intent (rejected: intent is "find the bug", doesn't help) |
| 4. Spec refinement | `commanders-intent` + `reduced-dialectic` (position/counter/synthesis) cycle | Pre-action-plan-disclosure alone (rejected: insufficient critique discipline) |
| 5. Scope decomposition / roadmapping | `commanders-intent` + `mece-decomposition` + `issue-tree-diagnostic` | Dependency-graph TBD — **gap** (catalog has no dependency-aware primary) |
| 6. Integration planning | `commanders-intent` + `interest-surfacing` (both systems' interests, not positions) + `single-text-procedure` for the coupling spec | Controller-orchestration (rejected: orchestration, not planning) |
| 7. Tool-selection / adoption | `spin-sequence` (Situation/Problem/Implication/Need-payoff) + `so-what-drilldown` | Engagement-letter scoping (secondary; useful for larger adoption efforts) |
| 8. Research scoping | `emergent-partition` (novel) + `bounded-five-whys` + `aporia-graceful-stop` for impasses | Socratic full `elenctic-probe` (rejected: too adversarial for research framing) |

The `commanders-intent` opener is the cross-shape foundation; task-shape-specific additions stack on top. Mid-session shape-shifts are expected and are supported by `summary-as-transition` (below).

### Layer 3 — Mid-session protocol stack

Applies continuously from opener through plan-draft; task-shape-agnostic.

- **`load-bearing-token-readback`** (aviation-origin) — per-turn. Agent restates the small set of domain tokens that appear in the human turn, surfacing vocabulary drift at turn 1 instead of turn 30. This is the single most consequential mid-session addition. Replaces the user's implicit-agreement on framing.
- **`sitrep-at-cadence`** (ICS-origin) — at checkpoints (e.g., every N turns or at decision points). Structured agent summary of what-we've-agreed, what's-pending, what's-blocked. Explicit `concern` slot.
- **`agent-surfaced-parking`** (unexplored) — agent tracks parked topics visibly and proactively offers to surface them at appropriate moments. Addresses the Phase 1 finding that branches are implicitly parked by the human and often lost.
- **`directional-clean-repetition`** (Socratic-origin, Grove's discipline) — agent uses the human's nouns verbatim when restating human-introduced concepts. Cheap; catches the silent-redefinition failure mode.
- **`summary-as-transition`** — between phases (context-building → plan-drafting, etc.) the agent writes a short transition summary that makes the phase-shift explicit and offers a shape-change probe.

### Layer 4 — User-state adapters

The user state is not always "fresh + engaged." These adapters maintain protocol quality under degraded user state:

- **Fresh + engaged** — all Layer 1–3 protocols apply at full depth. Socratic / MI-style elicitation additions (`maieutic-drawout` at novice subdomain moments, `oars-reflection-cascade` when intent is ambiguous) are available.
- **Tired / distracted** — substitute `question-preserving-autonomy` (counter-pattern #7) for any planned autonomous-dispatch. The agent runs autonomously but maintains a visible questions queue with rework-cost annotations, so the tired dispatcher can review asynchronously. Rubber-stamp risk (fatigue-reviewer flagged a dangerous class) is mitigated by the visible queue.
- **Novice subdomain** — lean on `elicit-provide-elicit` (MI): permission-ask before content delivery, reaction-elicit after. Prevents the agent from over-asserting in domains the agent itself doesn't fully understand.
- **Attention-drained** — `forced-choice-with-default` is permitted *with caveat*: use only on genuinely binary questions. Default is stated, silence = accept default, explicit "no, X" = override. The multi-framing trap flag still applies to broader use.

### Layer 5 — Close-of-session

- **Closing-summary ritual** (consulting-origin) — agent produces: what was decided, what was parked, what remains open, what the next session should start with. Artifact-form, not dialog-log.
- **`out-of-scope-section`** in the produced spec (consulting-origin) — explicit list of "not being built." Absent from observed corpus; high-leverage regret-prevention.
- **`after-action-review`** at multi-session-work completion: 4-question AAR (what was supposed to happen / what actually happened / differences / adjustments). Composes with Phase 2's own feedback into kerf.

### Layer 6 — Safe swaps (replace observed patterns with evidence-backed equivalents)

High-confidence displacements. These swap an observed pattern with an external-source-validated equivalent; evidence is independent of the user's corpus.

1. **`numbered-question-close` → `load-bearing-token-readback`.** Aviation CRM empirical evidence: interleaved slot-ack catches within-turn mis-hearings; end-close questions miss them. Decades of incident-archive data. The user's observed benefit (shortened next human turn) is exactly the aviation-predicted failure mode.
2. **`autonomous-dispatch` "never ask" → `mission-command` + `question-preserving-autonomy` + `back-brief-plan-quality`.** Military doctrine explicitly relies on enabled-question subordinate judgment; "never ask" inverts the doctrine. The substitution preserves the autonomy property the user values while restoring the question-enabled error-correction the bare form eliminates.
3. **`dialog-log-plan` → `single-text-procedure`.** Fisher's Camp David procedure: agent maintains one persistent plan document; human critiques, never re-drafts. Restores spec-first posture; eliminates the hunt-the-decisions-in-chat problem.
4. **`context-dump` → `dialogic-context-accretion`** (or pair context-dump with in-session correction checkpoints). Bare context-dump is a provisional-framework local maximum — it minimizes writing but collapses the decision tree in one shot and errors propagate. Accretion style pays writing cost incrementally against emergent understanding.

### Layer 7 — High-leverage experiments

Post-Phase-2 A/B candidates. Priority for user-authorized trials once the session budget permits:

1. **`numbered-question-close` A/B.** The exemplary provisional-local-maximum. Aviation evidence is external, but a within-user trial would confirm displacement direction. Cost: 5–8 matched pairs; transcript-measurable outcome (next-human-turn char count).
2. **`example-led-emergence` vs observed `pre-action-plan-disclosure` on founding-vision.** The hidden gem with highest Framing B score. Run on one new-subsystem session.
3. **`emergent-partition` vs observed `upfront-decision-partition` on one kerf work.** Genuinely novel; no external analog; the only counter-pattern that did not have a clean external convergence. Reviewer verdict was "draw" — head-to-head resolves it.
4. **`assumption-bundle` swap-in on a context-dump-default task.** Tests whether a dependency-graph of assumptions + cascading edits beats unstructured context dump.
5. **`question-preserving-autonomy` matched-pair with bare `autonomous-dispatch`.** Targets the fatigue-state use case; the bare dispatch is what the user currently uses when tired.

All five experiments fit within the A/B spec in `evaluation-framework.md` §8 (5–8 pairs, pre-registered, transcript-measurable outcomes).

## 5. Qualitative overlays

Demoted-from-quantitative criteria carried as commentary:

- **Implementation quality.** The user's solo practice has no external reviewers; implementation quality is dominated by human-agent coupling in the implementation phase, not the plan. The Layer 1 foundation stack + Layer 3 mid-session stack are predicted to maintain or improve implementation quality because they reduce silent-framing-commitment (the catalogued most-expensive failure mode).
- **Maintainability.** Downstream changes to specs produced by the recommended stack are expected to be cheaper because `alternatives-considered-section` and `out-of-scope-section` keep the plan-space legible to future-the-user. Not measurable within Phase 2.
- **Trust accumulation.** The `question-preserving-autonomy` pattern is specifically designed for trust calibration — it gives the human explicit visibility into what the agent chose to defer vs. decide. Over repeated use, this should accelerate trust growth.
- **Transferability.** The recommendations map (Layer 2) is itself the transferability answer. It is a *map*, not a single protocol, because adaptability varies by shape.
- **User-state robustness.** Layer 4 addresses this directly.

## 6. Composition stacks (explicit)

Named stacks that the review identifies as graceful (additive-compose, load-bearing functions are complementary):

- **Mission-command stack:** `commanders-intent` + `autonomy-scope-grant` + `back-brief-plan-quality` + `sitrep-at-cadence` + `after-action-review`. Most fully-specified stack; applies across task shapes; military-doctrine-validated as a *system*, not individual parts.
- **Founding-vision stack:** `commanders-intent` + `scqa-opener` + `assumption-bundle` + `alternatives-considered-section`. For new-subsystem sessions.
- **Diagnostic stack:** `sbar-opener` + `hypothesis-driven-ghost-deck` + `load-bearing-token-readback` + `so-what-drilldown`. For bug-investigation.
- **Elicitation stack:** `elicit-provide-elicit` + `directional-clean-repetition` + `agent-surfaced-parking`. For novice-subdomain sessions or fuzzy-intent openers.
- **Counter-pattern stack:** `example-led-emergence` + `dialogic-context-accretion` + `micro-step-incrementalism` + `emergent-partition`. An entire alternative protocol system; should be experimented with as a coherent whole on one work, not piecemeal.

Stacks that conflict (flagged by cognitive-load and ergonomics reviewers):

- `load-bearing-token-readback` + `numbered-question-close` — double turn-close ceremony; pick one.
- `ipass-opener` + `confirmation-brief` + `back-brief-plan-quality` — three overlapping handoff artifacts; choose one appropriate to the task shape.
- `micro-step-incrementalism` + any per-turn read-back — opposing turn densities.
- `kerf-parallel-reviewer` + `articulate-override-rule` applied by default — hidden writing load on every override; make override the exception, not the rule.

## 7. What survives the challenge-observed reviewer

7 of 8 Phase 1 cross-cutting findings: a counter-pattern or external-source rival wins on the challenge-observed frame. The exceptions:

- **Upfront-decision-partition:** a draw vs `emergent-partition`. Resolve via A/B (Layer 7 experiment #3).
- **Form-matters-independent-of-content:** challenged as possibly epiphenomenal, but the adaptability reviewer's finding (form adapts across task shapes; content doesn't travel) redeems it. Form remains a load-bearing dimension.

Three observed patterns survive with augmentation rather than displacement:
- `recovery-handoff` → augment with closed-ack + watcher-tier (medical+ICS).
- `controller-orchestration` → out of planning scope; keep as orchestration primitive.
- `kerf-parallel-reviewer` → replace with `role-split-reviewer-library` (subsumption, not displacement).

## 8. Honest limits

- **Analytical predictions, not corpus-filtered.** Step 4.5 deferred. Every ranking in §3–§7 should be treated as a hypothesis suitable for filter confirmation, not as settled finding. Several reviewers explicitly flagged corpus-dependent candidates (`[filter-dep]` tags).
- **Single user.** All findings are for this user's practice. Multi-user generalization is a separate research question.
- **No counterfactual evidence.** Every analytical recommendation is subject to being wrong. The recommended experiments in Layer 7 exist precisely because analytical ranking cannot resolve genuine ties.
- **Observed patterns under-defended.** The local-maxima guardian reviewer was explicitly anti-deferential to observed patterns. This is by design. A fair critique of these findings is: *where has the reviewer-panel been unfair to an observed pattern?* The multi-framing safe list (§3.1) contains the observed patterns that survived the challenge; those are the ones to trust.
- **The catalog had 87 entries; §4 recommendations name ~30.** The ~57 unmentioned protocols are not rejected — they are lower-priority given this review. Step 4.5 filter might elevate some.
- **Framing C (regret-adjusted outcome) is analytically weakest.** No outcome data joined in this session. The trap-candidate flags on `numbered-question-close`, `autonomous-dispatch`, `context-dump`, `forced-choice-with-default` are predicted regret, not measured. The retrospective spec-revision-rate analysis across kerf's historical works (evaluation-framework §3 R1) is the missing instrument.

## 9. Open questions carried forward

Questions that Phase 2 did not resolve, ordered by leverage:

1. **Step 4.5 corpus filter authorization.** Should the transcript-only harness be built and run across all 195 sessions? 1–2 days of scripting work; large information yield. Blocks empirical weighting of §4 recommendations. **Direct user decision.**
2. **Retrospective outcome-join for Framing C validation.** Kerf's historical works have downstream implementation artifacts; `spec-revision-rate-within-30-days` can be computed per work. This is the cheapest Framing C instrument. Blocks confidence in §3.2 trap-candidate flags.
3. **A/B authorization for the 5 Layer 7 experiments.** Each is 5–8 paired planning sessions. Some can be done in parallel with ongoing harmonik work; others need dedicated pairs.
4. **Scope gap: scope-decomposition / roadmapping** — catalog has no dependency-aware primary. Worth a targeted Step 2-style external-source pass into project-management / dependency-scheduling literature (Gantt, PERT, critical chain, etc.) if this becomes a limitation.
5. **Scope gap: research-scoping task shape** — catalog has no question-quality-evaluation protocol distinct from hypothesis-quality. Also worth targeted follow-up.
6. **Behaviors-first plan expression** (research-statement §8 flagged item). Framing C promotes behavior-first expression for spec-durability; research-statement hinted at it; Phase 2 did not fully explore. Worth a focused follow-up.
7. **Numbered-close CRM evidence strength.** The aviation anti-pattern finding is the single strongest external-to-observed displacement case in the corpus. Before acting, verify with a narrow A/B (Layer 7 experiment #1).
8. **Protocol-switching within session.** `protocol-shapeshifting` was identified as a meta-protocol need; adaptability reviewer named no protocol that explicitly handles mid-session shape shifts. `summary-as-transition` is partial; design of an explicit shape-shift primitive is an open question.
9. **Multi-framing cost of Framing B (mental-model coupling).** Without post-session probes, Framing B is measured via transcript proxies only. A cheap post-session "3 prediction questions" instrument could improve the signal substantially. Worth ~30 min per session to the user; explicit ask.
10. **Does kerf's existing jig structure accommodate these recommendations without structural change?** Phase 2 Step 7 produces a draft proposal — see `phase-2-kerf-integration-draft.md`.

## 10. Recommendation: what to do next

In approximate order of leverage, for the user's consideration:

1. Read §1–§4 of this document and the top of `evaluation-framework.md`. Decide whether the pair-graph framework and multi-framing requirement are shape-acceptable before committing to post-Phase-2 work.
2. Decide on Step 4.5 corpus filter authorization. This is the highest-leverage blocker: without it, §3–§7 rankings remain analytical predictions.
3. Adopt the Layer 1 Always-On Foundation Stack immediately in the *next* kerf work. Low cost; high-confidence from reviewer convergence; no experimentation needed.
4. Adopt the Layer 6 Safe Swaps in the next 2-3 kerf works. `load-bearing-token-readback` and `alternatives-considered-section` are effectively free.
5. Schedule the Layer 7 Experiment #1 (numbered-close A/B) — 5–8 paired sessions over a month. Highest confirmation-value per hour.
6. Review `phase-2-kerf-integration-draft.md` (Step 7 draft) and decide which integrations are desired before advancing.

This document is subsidiary to `evaluation-framework.md`. The framework endures across protocol recommendations; specific protocols may be swapped as evidence accumulates.
