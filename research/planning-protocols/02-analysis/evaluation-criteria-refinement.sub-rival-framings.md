# Rival Framings for Planning-Protocol Evaluation

> Sub-agent deliverable for Phase 2, Step 1 of the planning-protocols research track. Task: propose 2-3 radically different evaluation frameworks for planning protocols, starting from different first principles than the provisional criteria in research-statement.md §2. Each framework is developed deeply enough that Phase 2 can decide whether to adopt, hybridize, or reject it.
>
> The provisional framework (writing effort / question targeting / correction cycles / trivial-decision autonomy / context-switch load) treats a planning protocol as an **attention-cost optimizer**: the fewer characters the human must produce and re-produce to reach an implementable plan, the better. That framing is coherent and measurable, but it assumes the human's moment-to-moment typing cost is the thing most in need of optimization. That assumption is the one each rival below challenges.
>
> Last updated: 2026-04-23

---

## Framework A: Commitment-Deferral (Option-Preserving Planning)

### Core premise

> **A planning protocol is good to the extent that it defers irreversible commitments until the latest moment consistent with progress, while still producing enough signal for the human to steer.**

A plan is a sequence of commitments: to a problem framing, to a vocabulary, to a decomposition, to a structure, to a set of boundaries. Each commitment closes off alternatives. The failure mode Phase 1 flagged as "vocabulary/framing errors (silent) are more expensive than implementation errors" is a **premature-commitment** failure: a word the agent adopts in turn 3 silently shapes turns 4-40, and by the time the human notices, rework is expensive because dependent commitments have accreted.

Under this framing, the point of a planning protocol is not to minimize typing — it is to **keep the decision tree wider, longer**, while still producing enough mid-plan signal that the human can collapse branches intentionally rather than by drift.

### Criteria

1. **Time-to-first-irreversible-commitment.** How many turns (or characters, or clock-minutes) pass before the protocol has locked in: a problem framing, a decomposition, a vocabulary, a structural choice. Later is better, subject to progress.
2. **Branch-width at mid-plan.** At the protocol's midpoint, how many viable alternatives are still legible to the human? Protocols that present "here are 3 framings, pick one or ask for more" preserve branch-width; protocols that write a draft spec and iterate narrow it fast.
3. **Cost of reversal.** If the human notices a mistaken commitment at turn N, how many turns must be rewound and redone? Protocols that keep commitments local have low reversal cost; protocols that cascade commitments (a vocabulary choice affects every subsequent paragraph) have high reversal cost.
4. **Drift-surfacing latency.** How many turns elapse between the agent silently committing to something mis-aligned and that commitment becoming visible to the human? Shorter is better. (Distinct from correction-cycle count — we care how fast the *disagreement becomes visible*, not how many rounds it takes to fix.)
5. **Exploratory yield.** How many protocol-surfaced alternatives did the human get to see that they would not have generated themselves? This rewards protocols that expand the space before narrowing it.

### How it differs from the provisional framework

The provisional framework treats corrections as cost and rewards protocols that minimize them. This framework treats **early, cheap corrections as valuable** — they are the mechanism that keeps the decision tree wide. A protocol that produces more corrections early (because it keeps options open and surfaces alternatives) but fewer catastrophic late rework cycles would score poorly on provisional criterion (3) and well here.

- **Would favor:** agent-investigative protocols; forced-choice-with-default; protocols that explicitly present branches before picking one; Socratic styles where the agent asks before proposing; incremental-step autonomy.
- **Would reject / downgrade:** context-dump (collapses the tree in one shot and eliminates the correction loop — Phase 1 already observed that "errors propagate" with context-dump, but provisional framework still rewards it on criterion 1 because the human only types once); pre-action plan disclosure when the plan is already narrow; decision-closure-enables-long-autonomy patterns.
- **Would measure differently:** a "short human turn" is good on provisional criterion (5) and **ambiguous here** — a short turn might be signal the tree collapsed too fast (human had nothing left to choose between) or signal efficient steering (tree was well-shaped so a nudge sufficed).

### Strongest objection and answer

**Objection:** Deferring commitment indefinitely is itself a failure mode — it produces endless exploration without convergence. The whole point of a spec is that it commits. A protocol that never commits is a protocol that never produces a spec.

**Answer:** The framework rewards *latest consistent with progress*, not "never commit." Operationalization requires a progress baseline: protocols are compared at matched-progress checkpoints (e.g., "at the point a first-draft spec exists"), and the one that committed less along the way wins. It also requires measuring exploratory yield — a protocol that defers without surfacing alternatives is not option-preserving, it's just slow. A better objection is that "branch-width" is hard to observe from transcripts; see operationalization below.

### Operationalization sketch

- **Time-to-first-irreversible-commitment:** tag each turn for commitment-events (first use of a structural term without hedging; first decomposition into named pieces; first agreement on scope). First commitment-event per category per session.
- **Branch-width at mid-plan:** count distinct alternatives mentioned or proposed-and-still-open at turn N/2. Proxy: "count of phrases like 'we could X or Y', 'another option', 'if instead'."
- **Cost of reversal:** when a correction occurs at turn K, count turns between K and the turn the originally-committed thing was introduced. Weight by how much subsequent work depended on the original commitment.
- **Drift-surfacing latency:** for each mis-alignment that is eventually caught, count turns between introduction and catch.
- **Exploratory yield:** count alternatives the agent proposed that the human had not mentioned.

### Protocols this framework would rank highly that the provisional might miss

- **Branch-first protocols** — agent opens with "I see three plausible framings of this; which do you want, or describe a fourth?" Provisional framework penalizes the extra typing ("three framings" is more to read than one proposal); this framework rewards the preserved option.
- **Explicit-assumption-tagging protocols** — agent flags every silent assumption it's making with markers like [[assume: X, reversible]]. Provisional framework sees no benefit (same output, more noise); this framework rewards drift-surfacing latency of zero.
- **Commitment-ledger protocols** — agent maintains an explicit list of what's been locked vs. still-open, visible in each turn. Adds surface noise, but radically lowers cost-of-reversal.
- **Socratic-before-proposing** — agent refuses to propose until it has asked N questions. Provisional framework penalizes human writing load; this framework rewards deep mutual understanding before branch-collapse.

---

## Framework B: Mental-Model Coupling

### Core premise

> **A planning protocol is good to the extent that, at each checkpoint, the human and the agent share a mental model of the problem and the plan accurate enough that either could predict the other's next move.**

Under this framing, the spec is an *artifact of* alignment; alignment itself is the real deliverable. The provisional framework measures surface phenomena (typing, corrections) — this framework measures the latent state those phenomena are proxies for. A protocol that produces a beautiful spec the human nonetheless doesn't understand has failed; a protocol that produces a modest spec the human deeply owns has succeeded, because implementation (and future modifications, and the next planning session) will run on the mental model, not on the words.

The cognitive-science framing is specific: shared mental models are the *actual* substrate of coordination (Klein, Cannon-Bowers on team cognition). Words, diagrams, and specs are compressed transfer mechanisms for the model; they work only if the decompression on the other side reconstructs a compatible model.

### Criteria

1. **Prediction-accuracy across the gap.** If at some checkpoint the human is asked "what would the agent do next on this?" and the agent is asked "what would the human prefer here?", how often do their answers match? High match = tight coupling. (Symmetric: both directions matter.)
2. **Novel-case transfer.** If presented with a case the plan did not explicitly handle, can the human and agent independently reason to compatible answers? Tests whether the shared model generalizes or is just memorized surface.
3. **Vocabulary convergence.** Do the human and agent use the same terms for the same concepts, and do they disambiguate when terms diverge? (Phase 1's "vocabulary/framing errors silent" finding lives here.)
4. **Explanatory reciprocity.** Can the human explain back what the agent proposed? Can the agent explain back what the human wants? Can either catch an error in the other's explanation? This is "teach-back" from medical handoffs applied to planning.
5. **Decay robustness.** After a multi-day break, does the shared model survive enough that the next session can resume without reload? Protocols that produce transferable model-state beat protocols that leave model-state in the chat log only.

### How it differs from the provisional framework

The provisional framework is **output-focused** (the plan); this framework is **state-focused** (the alignment between heads). Two protocols that produce identical specs can score radically differently here: one produced the spec through dialog that left the human with a deep model; the other produced it via context-dump-and-execute, and the human has the document but not the model.

- **Would favor:** Socratic protocols where the agent asks the human to explain their own intent; teach-back protocols; protocols with explicit "summarize what you think I'm asking" moves; bidirectional-knowledge protocols; human-teaches-agent flows.
- **Would reject / downgrade:** autonomous-dispatch ("study specs, never ask") — even if output is correct, zero coupling work was done, and any future session starts cold; context-dump when the human pours context but never hears it reflected back; numbered-question-close when the human answers without having to explain *why*.
- **Would measure differently:** correction cycles are *diagnostic, not bad.* A correction is evidence of coupling work happening — the question is whether it converges quickly, not whether it happens.

### Strongest objection and answer

**Objection:** "Mental-model coupling" is unobservable from transcripts. You can't measure what's in someone's head from chat logs, and any proxy (prediction-accuracy tests, teach-backs) requires instrumenting the session — which changes the protocol you're measuring. This framework is pretty but not empirical.

**Answer:** Two levels of reply. (1) The instrumentation is itself a protocol feature worth measuring — "does this protocol embed teach-back moments?" is an observable binary. (2) The harder claim — measuring the coupling itself — can be done cheaply via structured post-session probes: at session end, ask the human 3 prediction questions about the agent's likely choices on related cases; ask the agent 3 questions about the human's likely preferences. Score agreement. The cost is low; the signal is direct. For historical transcripts (Phase 1 corpus) where post-session probing is impossible, use *within-session* teach-back moments as the observable: count them, score whether they caught errors.

A weaker form of this objection — "you're measuring something only indirectly related to the output" — is actually the framework's feature, not bug. The claim is that output-focused measurement of planning is the wrong level; the thing that makes plans good over time is the coupled model behind them.

### Operationalization sketch

- **Vocabulary convergence:** track term-usage across turns; count turns to first agreed definition of each load-bearing term; count silent term-redefinitions (where one side starts using a word differently).
- **Explanatory reciprocity:** count instances of "so what you're saying is..." or "let me restate this" from either side; count whether restatements triggered corrections.
- **Prediction-accuracy:** require protocols under test to run through a small standard planning problem, then administer post-session mutual-prediction probe. Score.
- **Novel-case transfer:** add one "how would you handle this neighboring case?" question post-session to each party.
- **Decay robustness:** longitudinal — same human + agent + problem, resumed after 7 days. Measure reload cost (characters / turns until productive work resumes).

### Protocols this framework would rank highly that the provisional might miss

- **Human-teaches-agent flows** — the first N turns are the human explaining domain context without the agent proposing. Heavy writing cost upfront (provisional framework penalizes); produces deep coupling.
- **Teach-back protocols** — agent periodically pauses to restate the human's intent; human corrects or confirms. Adds turns; reduces decay and silent drift.
- **Reciprocal-Socratic** — both sides ask questions, not just the agent of the human. Rare in observed corpus.
- **Model-snapshot protocols** — at checkpoints, the agent writes a "what I believe about your intent" artifact that the human edits. Explicit coupling-state artifact, transferable across sessions.
- **Vocabulary-pinning protocols** — before proposing, agent lists the load-bearing terms it's going to use and asks for confirmation/edit. High overhead; catches silent-framing drift at turn 1.

---

## Framework C: Regret-Adjusted Output Quality (Outcome-Centric)

### Core premise

> **A planning protocol is good to the extent that the plans it produces turn out — in retrospect, after implementation and usage — to have been the plans the human should have wanted.**

The provisional framework measures what happens inside the planning session. This framework measures what happens *after*: did the plan survive implementation without substantive rework? Did it scope the right problem? Did the human look at the finished work and wish they had planned for X? The protocol is a black box; the output is the plan; the evaluation happens downstream of both.

This framework takes seriously that planning is not a thing-in-itself — it is instrumental to building the right system. A protocol that makes planning sessions feel fast and aligned but systematically produces plans that miss real requirements is a high-grade local optimum and a real trap. The provisional framework cannot catch this because its evaluation never leaves the planning phase.

### Criteria

1. **Post-implementation regret rate.** How often does the human, after shipping, wish the plan had been different in a way they could plausibly have surfaced at plan time? Lower is better.
2. **Scope-drift detection rate.** How often does the protocol surface "this feature is actually two features" or "this is in scope for a different subsystem" at plan time rather than implementation time? Higher is better.
3. **Requirement-surfacing completeness.** When implementation discovers a real requirement that planning missed, was it foreseeable from the material available at plan time? Protocols that miss foreseeable requirements lose; protocols that miss only genuinely-unforeseeable ones are neutral.
4. **Spec-durability.** How many weeks/months after implementation does the spec still match the working system, vs. has reality diverged? Durable specs indicate the plan captured real structure; quickly-stale specs indicate the plan captured ceremony.
5. **Revisit-cost.** When the plan needs updating later, how much of the original planning session's output is reusable, and how much has to be redone? Protocols whose outputs are modular and legible-to-future-self beat protocols whose outputs are tightly-coupled or dialog-only.

### How it differs from the provisional framework

The provisional framework is **process-internal**: it measures what's happening in the session. This framework is **output-validated**: the session's quality is determined by facts that are not available until weeks or months later. This asymmetry is the whole point — a protocol that feels good internally but produces bad outputs is the exact failure mode the user flagged as "a protocol scoring well on criteria but poorly on the unnamed criterion."

- **Would favor:** protocols that spend heavily on requirement-surfacing (question-investigative, pre-action disclosure with challenge steps, reviewer sub-agents looking for omissions), protocols that produce structured artifacts (not just chat logs), protocols with an explicit "what are we *not* building" exit checklist.
- **Would reject / downgrade:** dialog-form plans (not durable), minimal-dispatch with autonomous execution (no requirement-surfacing step, regret is where you learn), any protocol where the spec exists only as implicit agreement.
- **Would measure differently:** "human wrote a lot" is neutral here — what matters is whether the writing produced the right plan. Correction cycles are neutral — late corrections are bad, but early corrections during planning are fine.

### Strongest objection and answer

**Objection:** Outcome-based evaluation is impossible on practical timescales. You can't run controlled experiments where the same work gets planned with two different protocols and then built twice. You can't even retrospectively measure regret on the corpus — the user would have to self-report regret on each of ten historical sessions, and that's both costly and biased by knowing outcomes. This framework is right in principle but unusable in practice.

**Answer:** Three partial replies.
1. **Natural-experiment mining.** Kerf has historical works. Each work's downstream implementation has already happened or not. Retrospective measurement is possible, cheap, and informative: for each past kerf work, score regret, scope-drift, requirement-miss, spec-durability. This gives outcome-signal for protocols the user has already tried.
2. **Leading indicators as proxies.** Certain in-session behaviors predict regret — unexplored scope questions, unstated assumptions, terms used without definition. These are observable in-session and can serve as proxies for predicted-regret scoring. (The framework thus becomes: measure proxies in-session, calibrate proxies against outcomes on historical works.)
3. **Prospective A/B on small problems.** For new work, run protocols in pairs on cheap subtasks. The cost of double-planning a small task is low; the outcome-signal arrives within days.

A more serious form of the objection is: *outcome quality is confounded by everything except the protocol* — the model version, the human's mood, the domain difficulty. True. But the confounds are shared with every other framework; this framework at least measures the thing we care about, rather than a proxy whose relationship to it is unvalidated.

### Operationalization sketch

- **Post-implementation regret rate:** structured retrospective per work — "what would you have wanted the plan to include that it didn't?" Score on 5-point regret scale. Aggregate by protocol.
- **Scope-drift detection rate:** count scope questions raised during planning vs. scope surprises during implementation. Ratio.
- **Requirement-surfacing completeness:** implementation-phase "we need to X" moments; judge foreseeability; attribute to protocol.
- **Spec-durability:** diff current system behavior against spec at N weeks post-implementation; count material divergences.
- **Revisit-cost:** measured when an update happens — characters-of-original-reusable / characters-of-original-total.

### Protocols this framework would rank highly that the provisional might miss

- **Devil's-advocate-reviewer protocols** — a reviewer sub-agent specifically tasked with "what's missing / what assumes / what's out of scope?" at plan draft time. Provisional framework sees overhead; this framework sees regret-prevention.
- **Pre-mortems** — planning ends with "if this fails in 6 months, why?" Cheap in session; high outcome signal.
- **Explicit non-goals** — protocols that require writing down what is *not* being built. Costs typing; prevents scope drift.
- **Behavior-first plan expression** — plans as behavior lists or test cases (user flagged this already). Durable because behaviors match reality better than structural decomposition.
- **Plan-durability-checkpoint protocols** — planning produces not just a spec but a list of "things we expect to change" and "things we expect to be stable." Catches implicit assumptions about stability.

---

## Cross-framework synthesis (brief)

The three frameworks are not mutually exclusive — a composite framework could weight criteria from each — but they conflict in their priors about what planning is *for*:

- **Provisional:** planning is attention-spend, minimize the spend for matched output.
- **A (Commitment-Deferral):** planning is decision-tree management, keep options open until progress forces commitment.
- **B (Mental-Model Coupling):** planning is model-transfer, the spec is a byproduct of the coupling.
- **C (Regret-Adjusted Outcome):** planning is a bet on future system quality, evaluate by whether the bet pays.

The provisional framework assumes the human knows what they want; Frameworks A and C drop that assumption (the human is discovering what they want through the protocol). Framework B assumes what the agent knows is as important as what the human knows; the provisional framework treats the agent as a tool.

**Recommendation to Phase 2 main thread:** do not collapse to one framework. Use provisional as the default measuring stick, but require every ranked candidate protocol to be independently re-scored on at least one rival framework. Disagreement between framings on the same protocol is the interesting signal — it localizes where the "unnamed-but-important criterion" the user worried about is living.

A concrete cheap experiment: take the top 3 Phase 1 observed protocols, score each on all four frameworks (provisional + A + B + C). If the rankings disagree, that disagreement is Phase 2's highest-leverage investigation target.

---

## Framings considered and rejected

### Rejected: Trust-Calibration-Centric

**Core premise tried:** a protocol is good to the extent that the human and agent maintain accurate trust-calibration — human under-trust produces over-asking; over-trust produces silent misalignment.

**Why rejected:** On serious development, this is not a distinct framework — it's a special case of Mental-Model Coupling (Framework B). "Accurate calibration of when to trust" is just "shared model of each other's competence boundaries," which is coupling. Keeping it separate would double-count without adding a distinct theory of planning. The one distinct predictor it offered — "explicit trust-delegation contracts at session start" — is capturable under Framework A's commitment-ledger idea anyway. Subsumed.

### Rejected: Anti-Fragility / Robustness-to-User-State

**Core premise tried:** a protocol is good to the extent that it degrades gracefully across user states (tired vs. fresh, novice vs. expert, attention-rich vs. attention-drained).

**Why rejected:** Important but not first-principle. Robustness is a *meta-criterion* — it asks how stable any other criterion is across operating conditions. Treating it as a primary framework conflates "the thing we want" with "how reliably we want it." The user is a single senior architect in roughly-stable working conditions; Phase 1's corpus does not give strong state-variation signal. Fold this in as a secondary lens applied to whichever primary framework wins: "score protocols on primary criterion across three simulated user-states, reject any that degrade catastrophically." Worth doing; not worth a whole framework.

### Rejected: Review-Economy-Centric (ratio of agent-review work to human-review work)

**Core premise tried:** protocols that shift review load onto agents (self-review, peer-agent-review) beat protocols that leave review as a human responsibility.

**Why rejected:** Conflates mechanism with goal. Shifting review onto agents is a *tactic* for reducing human attention cost — which is already what the provisional framework measures. Kerf's sub-agent reviewers already implement this; the question "is that the right tactic?" is answered by the provisional framework (does it reduce human writing, improve targeting, reduce correction cycles?). Elevating it to a framework implies agent-review is good in itself, which it isn't — bad agent reviews are worse than no reviews. This is a tactic to evaluate *within* a framework, not a framework itself.

### Rejected: Information-Theoretic (bits-of-human-intent-transferred-per-character)

**Core premise tried:** model the protocol as a channel; evaluate by the information-theoretic rate of intent-transfer.

**Why rejected:** Seductive but wrong unit. The information being transferred during planning is not fixed bits the human already possesses — much of it is being *generated* during the dialog (the human discovers what they want by trying to say it). Channel-rate metrics assume a sender with a message; planning is closer to joint construction. The math would measure a phenomenon that isn't really happening. Framework B (coupling) captures the real cognitive-coordination structure without the mis-applied formalism.

---

## Note to Phase 2 main thread

All three developed frameworks pass the "produces testable predictions on the existing corpus" bar. All three can be operationalized cheaply enough to score the 10 Phase 1 sessions as a pilot. The most productive next step is probably not picking one, but **running all four (provisional + A + B + C) against the same 3-5 protocols and observing disagreement**. The protocols the frameworks disagree on most are the ones where the "unnamed-but-important criterion" is probably hiding.
