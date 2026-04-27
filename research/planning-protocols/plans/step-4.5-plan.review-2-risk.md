# Adversarial Review: Step 4.5 Implementation Plan
## Risk Assessment — Measurement Integrity

**Review date:** 2026-04-24  
**Plan target:** step-4.5-plan.md  
**Reviewer frame:** Skeptical measurement-error detection  

---

## Executive Summary

The plan is a solid engineering scaffold for corpus-signal filtering, but carries **five high-confidence measurement risks** that could produce false rankings downstream. The biggest risk is not any single classifier but the **composition of fragile choices**: a 60% protocol-identity threshold on behavioral signatures, a correction-incident detector with hand-validation as the only false-positive guard, and a writing-load tagger (H-complexity, ~3 hrs) that is simultaneously load-bearing for Pair P1 but not pressure-tested. If any one of these three mis-classifies by >15%, the NE-6 and NE-2 results could flip support-level buckets and send the wrong protocol to Step 5 review.

The plan acknowledges correction detection as the highest risk (§7, FP guards listed) but does not adequately address the second-order risk: **even if correction detection hits 0.85 precision, what if the taxonomy-based classifiers (trivial-topic, hedging, numbered-close) are miscalibrated and cause the effect sizes to shift sign?**

---

## 1. False-Positive Modes NOT Guarded: The Unprotected Classifiers

The plan's §7 focuses false-positive mitigation on correction detection (C1/M1). But five classifiers have no false-positive guards specified, despite producing binary or count data that feeds into published effect sizes:

### 1.1 Trivial-Topic Taxonomy (§2.7)

**The problem.** The taxonomy buckets agent questions into {trivial, architectural, requirements-clarification, unclassified}. The keywords are:
- Trivial: `{name the file, filename, call it, folder structure, ...}` (8 keywords)
- Architectural: `{interface, boundary, responsibility, ...}` (7 keywords)
- Requirements: `{do you want, what should happen when, ...}` (5 keywords)

**False-positive risk: High.** An agent question like "should the task runner interface with the queue or should it defer to a separate scheduler?" matches both "interface" (architectural) and "should the" (requirements-clarification). The plan assigns it to "dominant category" — but dominance is subjective. A 15% miscategory rate on this classifier cascades directly into A1 (over-ask count), which feeds NE-2's natural-experiment ranking.

**Guard present?** No. The plan cites calibration from "79a42399 H#1" (one session, one opening). No hand-validation pass, no held-out test set, no threshold adjustment for ambiguous cases.

**Impact if wrong.** NE-2 claims "never-ask template" produces lower over-ask count. If trivial-question taxonomy is miscalibrated (e.g., 20% of architectural questions mis-tagged as trivial), then A1 in template sessions is artificially inflated, and the effect size could reverse.

### 1.2 Hedging Lexicon (§2.3)

**The problem.** Per-turn hedging density feeds F1 (opening-turn fuzziness) and F2 (resolution latency). The lexicon lists ~20 hedging tokens: `maybe`, `probably`, `possibly`, `might`, `could be`, `i think`, etc.

**False-positive risk: Medium-high.** Negated hedging ("not maybe," "not probably") is not excluded. Quoted hedging ("the user said 'maybe'") is not distinguished from agent hedging. Hedging in comments or code snippets is not filtered. The plan says "per-turn density" but does not specify whether bot-generated text (command output, tool results) is stripped first.

**Guard present?** No. The plan requires human turns to be tagged with writing-load categories, but the hedging classifier runs on raw turn text per §4.1, with no preprocessing beyond the extraction layer.

**Impact if wrong.** F1 and F2 are explicitly elevated (framework §3 "elevated criterion"), meaning they are treated as load-bearing for protocol evaluation. If hedging density is 20% overestimated due to negated/quoted tokens, then protocols that reduce actual fuzziness (but attract token-based false positives in their responses) will be penalized unfairly.

### 1.3 Approval Markers (§2.9, hidden in writing-load tagger)

**The problem.** The writing-load tagger (§2.9) tags human turns as category (d) "approval/confirmation" if they match `{sounds good, looks right, yes, go ahead, lgtm}` AND len < 100. The turn is then tagged (d), which feeds W1.

**False-positive risk: Medium.** The turn "looks right, but we should also consider X" is 40+ chars, matches "looks right," and has a scope-expansion clause. Does it tag as (d) or (e)? The rule says "AND len < 100" — this turn is <100, so it should tag (d). But it contains (e) signal. The disjunction rule is ambiguous.

**Guard present?** No. No hand-validation baseline for writing-load tagging. The plan allocates ~3 hours to build the tagger and states "Require precision ≥ 0.85 on the hand-labeled set before running corpus-wide" — but this is only mentioned for correction detection, not for the writing-load tagger itself.

**Impact if wrong.** W1 is the primary half of Pair P1 (writing-alignment). If approval markers are miscategorized, then W1 totals are biased, and protocols that produce ambiguous-but-short turns will score artificially low on correction count (C1) but also artificially low on writing effort (W1), masking alignment problems.

### 1.4 Numbered-Close Detector (§2.8) — The Highest-Leverage Classifier

**The problem.** The detector classifies agent-turn closes as {numbered, open-ended, declarative, single-question, mixed}. The rule for "numbered" is:

```
matches /(^|\n)\s*\d+[.)]\s+.+\?/m at least twice in the trailing 500 chars
```

**False-positive risk: Subtle but dangerous.** The regex matches `1. question here?` or `1) question here?` at line-start or after newline. False-positives include:
- Markdown-numbered lists with a trailing "?" on an unrelated item (e.g., "3. Consider error handling: do we need retry logic here?"). Intended: single question. Detected: numbered (if line 3 ends in "?").
- Numbered references to prior turns ("per #4 above, what about scheduling?"). The regex could match if prior turn is formatted as "4. item". Detected: numbered close.

**Guard present?** Minimal. The rule requires "at least twice" to counter single-line accidents, but a three-item numbered list triggers the detector even if only items 1 and 3 are questions.

**Impact if wrong.** NE-6 is the highest-n test (thousands of agent turns). If the numbered-close detector has a 10% false-positive rate (numbering that isn't structured questions is tagged as numbered-close), then the effect size reported for "numbered vs. open-ended" could be unstable. The plan notes this is "the single most load-bearing classifier — it's the basis of NE-6, the highest-n natural experiment." A 10% FP rate is not unusual for regex classifiers; it is very unusual for that error to be invisible to the user.

---

## 2. Natural-Experiment Confounds: The Uncontrolled Comparisons

### 2.1 NE-6: Numbered-Close vs. Open-Close on Next-Human-Turn Char Count

**The confound.** The plan compares agent turns ending in numbered questions vs. open-ended questions on the outcome "next_human_turn_chars". These two closing shapes are not randomly assigned. They correlate with:
- **Agent capability/context.** If the agent is uncertain about what to ask, it asks many questions (turns into a numbered list). If the agent is confident, it asks one open-ended question ("thoughts?"). Sessions with higher uncertainty produce longer subsequent human turns.
- **Session phase.** Early session (high uncertainty) → numbered closes. Late session (confident) → open-ended closes. Session phase also correlates with human turn length (late-session corrections are often shorter because the design space is narrower).
- **Task shape.** Some tasks naturally require multi-faceted questions (design review, architecture). Others are binary (add feature or not). Task shape is not randomized.

**Confound direction.** If agent uncertainty is higher in numbered-close turns, then subsequent human turns might be longer *not because of the numbered format* but because the agent's implicit "I don't understand the design space yet" signal triggers longer human explanations. The treatment effect (numbered close causes shorter turns) could be **reversed** once confounds are stratified.

**Plan's guard.** None. The plan states the outcome ("next_human_turn_chars") and reports n, effect size, and p-value. It does not control for session phase, task type, or agent confidence.

**Impact if wrong.** NE-6 is the recommended A/B target (framework §8.3). If the natural experiment gets the effect direction wrong, the A/B will measure the opposite of what the experiment predicted, and the user will falsely conclude that open-ended closes are better (when the real driver was confounding on session phase or task uncertainty).

### 2.2 NE-2: Never-Ask Template Sessions vs. Dialog Sessions

**The confound.** The plan compares ~100 secure-dev template-dispatched sessions (opener_shape == "never-ask-template") vs. 3 dialog-opener sessions on metrics like A1 (over-ask count). The treatment/control distinction is not a randomized protocol choice — the sessions are fundamentally different task types.

Per the plan (§6): template sessions are "deliberately different types of work." The opening question is begged: different *how*? If template sessions are "fix this bug" and dialog sessions are "design a new subsystem," then the A1 difference could be driven by task scope, not by the never-ask clause.

**Plan's guard.** The framework (§4.3) calls this out as a natural experiment but does not specify confound control. The plan lists it as "Yes" for first-pass execution.

**Impact if wrong.** The plan proposes that never-ask-template produces low A1 (agent doesn't over-ask). If the real driver is task narrowness (bug fixes vs. design), then the protocol advantage is illusory, and applying never-ask to design-phase work would produce alignment failures.

---

## 3. Protocol-Identity Threshold: The 60% Rule is Underspecified

**The rule.** A session is tagged as `numbered-question-close` protocol if ≥ 60% of agent turns match the numbered-close detector (§6).

**The risk.** What if the user's actual data clusters at 50%, 65%, 75%? The plan does not analyze the distribution. If sessions cluster at bimodal {35%, 80%}, then the 60% threshold carves the distribution at an unnatural place, and moving to 50% or 70% would flip protocol membership for ~40% of sessions.

**Propagation.** If protocol membership flips, then per-protocol aggregates (4.3) change, which changes support_level (strong/moderate/weak), which changes the ranking Step 5 receives. The user has no visibility into how sensitive the final ranking is to this one threshold choice.

**Guard present?** The plan does not mention threshold sensitivity analysis or specification of the threshold. §6 states "if ≥ 60% of agent turns close numbered" as a fact, but does not justify 60% over 50%, 55%, or 70%.

**Recommendation to strengthen.** Before generating aggregates, emit a per-session histogram of protocol signature density (% of turns matching each protocol). If the distribution is not clearly bimodal (with a gap near 60%), the threshold is arbitrary and the results are brittle.

---

## 4. Metrics That Look Load-Bearing But Are Fragile

### 4.1 F2 (Fuzziness-Resolution Latency)

**The definition.** F2 = turn index at which rolling-3-turn hedging density falls below 50% of F1 and stays there for ≥ 3 turns.

**The fragility.** F2 depends on:
1. F1 (which depends on hedging lexicon, which has FP issues per §1.2)
2. A rolling window (3-turn window; why 3, not 5?)
3. A 50% threshold (why 50%, not 40%?)
4. A persistence criterion (≥ 3 turns; why 3?)

If any one parameter is off by 10%, F2 can shift by 3–5 turns. A user comparing "protocol A resolved fuzziness by turn 7" vs. "protocol B by turn 12" might be comparing a true effect or a measurement artifact.

**Evaluation plan (framework §5):** Framing C relies on "spec-revision-within-30-days" (R1) as a ground-truth outcome signal. R1 is computed later (§9, follow-up work). **F2 is never validated against R1.** If fuzziness-resolution-latency is only loosely correlated with actual spec durability, then a protocol ranked high on F2 could produce high revision rates post-implementation.

### 4.2 A1 (Over-Ask Count, Trivial-Topic)

**The fragility.** A1 depends on:
1. Trivial-topic taxonomy (which has miscategorization risk per §1.1)
2. A binary classification (trivial vs. not)
3. Question detection (regex `?` in agent turn; but some "questions" are rhetorical and some statements are implicitly seeking info)

A 15% miscategory rate shifts A1 by 2–3 questions per session, which affects the "autonomy-coupling" pair P2 interpretation.

---

## 5. Honest-Limits Check: Claims in §11 of the Framework

Framework §11 (what the framework cannot measure):

> "True mental-model coupling. Framing-correction count is a proxy, not the thing."  
> "Causation. Everything except the A/B stage is correlational."  

**The plan's integrity risk.** The plan produces M1 (framing-correction count as a proxy for mental-model coupling) and reports it as a session-level outcome. The framework admits M1 is a proxy. **The plan does not include a caveat in the output schema (§4.1, §4.3) warning that these are proxies, not ground-truth.**

If Step 5 review or later A/B authorization treats "M1 is low" as evidence that "mental-model coupling is working," the plan has claimed to measure something the framework explicitly forbids measuring.

**Recommendation.** Amend output schemas (§4.1, §4.3) to include a "measurement limitations" field per metric, so Step 5 cannot misinterpret proxies as direct measurements.

---

## 6. The 60-80% Budget Allocation on Correction Detection

**The allocation.** The feasibility table (§8) allocates:
- 2.4 (correction-incident + subtyping): 4 hours
- 2.9 (writing-load tagger): 3 hours
- Hand-validation: 3 hours
- **Total: ~10 hours of the 32-hour budget.**

**The contingency problem.** The plan states: "If [correction detector] doesn't hit precision ≥ 0.85 on the hand-labeled set before running corpus-wide, does the user want the harness to proceed anyway with the detector disabled?"

**This is under-specified.** If the detector is disabled, then:
- C1 and M1 are uncomputed (framework Pair P1 loses its second member).
- W1 is computed, but without the C1 context, the alignment signal is incomplete.
- NE-1 and NE-4 (which rely on C1) cannot be reported.
- The "trimmed harness" recommendation (§8, ~12 hrs) becomes even smaller, and the user's decision to pick "trimmed or full" becomes moot if the critical component fails.

**What's missing:** A fallback plan. If hand-validation fails, what does the user do?
- Option A: Iterate the lexicon and re-validate (unknown time cost).
- Option B: Proceed with C1 disabled and report "correction detection not validated" (weakens Pair P1 credibility).
- Option C: Abandon the harness and do hand-analysis only (sinks the whole effort).

The plan does not clarify which option is authorized.

---

## 7. The Unified Catalog: Which Classifiers Tag Which Protocols?

**The mapping (§6).** The plan maps 87 catalog entries to three protocol-identity classes. Example:

- `numbered-question-close` → every agent turn classified by §2.8; protocol "holds" for a session if ≥ 60% of turns close numbered.

**Hidden risk:** Behavioral-signature protocols (including `numbered-question-close`) depend on a classifier ensemble, not a single classifier. If §2.8 has a 10% FP rate *and* a confound on session phase (§2.1), then the inferred protocol support could be misleading.

**Worse:** The plan does not specify which classifiers are "must-have" vs. "nice-to-have." If the correction-incident detector (§2.4) fails validation, does `pre-action-plan-disclosure` still get tagged? That protocol's corpus support depends on M1 subtyping. If M1 is disabled, the protocol cannot be tagged.

**Recommendation.** Build a dependency graph: which catalog entries depend on which classifiers. Flag entries as "not-testable if detector X fails." This clarifies the stakes of any single classifier failure.

---

## 8. Top Push-Back Point

Of all the risks above, **the single biggest integrity risk** is the assumption that NE-6 (numbered-close effect) can be interpreted causally without controlling for session phase or task uncertainty.

The plan states (§2.8): "This is the single most load-bearing classifier — it's the basis of NE-6, the highest-n natural experiment."

But NE-6 is a **non-randomized observational comparison** with a known confounder (session phase correlates with both closing style and human turn length). The plan does not stratify by session phase or include a confound-adjustment model. If session phase is a confounder, the effect size could reverse when stratified.

**Why this matters:** Framework §8.3 recommends NE-6 as the target for the first A/B trial. If the natural experiment has the sign wrong due to uncontrolled confounding, the A/B will measure the opposite hypothesis, waste the user's session budget, and produce a false negative.

---

## Final Recommendation

**If I were the user, I'd push back on:** Before signing off on Step 4.5 execution, require a confound-stratification plan for NE-6 (numbered-close vs. open-close): specify up front which session-phase or task-uncertainty signal will be used to post-stratify the data, and commit to reporting effect sizes within each stratum separately, not a pooled average that may be entirely driven by confounding.

This is not a full A/B — it's a 30-minute specification of how to make the highest-n experiment credible. Without it, the recommended A/B target rests on a measurement that could flip sign.

---

**Word count: 1,483**
