# Empirical Evaluation of Planning Protocols — Design Document

> Sub-deliverable of Phase 2 Step 1 (evaluation-criteria refinement). Interrogates the question: *can planning protocols be evaluated empirically rather than by analysis, and at what cost?* Covers natural experiments in the existing corpus, a test-suite-of-planning-problems design, A/B feasibility, simulation, and a practitioner-diagnostic alternative. Concludes with a ranked, feasibility-tagged recommendation set.

Scope note: "empirical" here means evidence that is independent of a reviewer's post-hoc reading — numbers, pre-registered categories, repeatable procedures, replay against a fixed problem set. Analytical reading of the existing corpus (as done in the six Phase 1 lenses) is *not* empirical in that sense; it's interpretive. Both have value. This document is about what empirical instruments are available and whether they would pay back their cost.

---

## 1. Natural experiments already in the corpus

The corpus has two useful properties for natural-experiment mining: (a) the same human (user) appears in every session, so between-subject noise is zero; (b) the user has visibly changed interaction conventions over time and across projects, producing within-user shifts that look like naturally-assigned treatments.

### 1.1 Identifiable natural experiments

**NE-1 — Autonomy-partitioning opener (within-subject, between-sessions).**
The decision-delegation lens found that exactly one session in the 10-session primary corpus carries the explicit rule "trivial → decide yourself; critical → ask me" (79a42399 H#1). That session shows the lowest rate of "wasted A-or-B" questions and the best autonomy-stretch profile. All other sessions lack the opener and show repeated over-asking on trivia (38415843 H#3: four consecutive "I don't care" responses). This is a natural A/B: same user, same agent family, one session with the treatment, nine without.
- **Treatment:** autonomy-partitioning clause in H#1.
- **Outcome measures available:** count of "whatever you think"/"I don't care"/"your discretion" phrases per session; count of questions the human answered with zero substantive content; mean length of human's decision-response turns.
- **Caveat:** n=1 on the treatment side. The effect could be a session-content confound — 79a42399 happens to be a session where all the pending decisions were genuinely architectural. Control for this by classifying each question as trivial/architectural *before* observing how the human responded (see §1.4).

**NE-2 — Secure-dev "never ask questions" template (between-session, high-n).**
~100 of secure-dev's 133 sessions are dispatched with a template that includes "Never ask the human questions or wait for input." All target the same repo and use the same `specs/` + `.scratch/fix_plan.md` gating. Comparing template-dispatched sessions against the few secure-dev sessions that use a dialog opener (79a42399, c6d1bd16, d1704aa0) gives a within-project, between-protocol contrast on:
- How often the agent *violated* the never-ask clause (mechanically detectable: any human text turn after #1).
- How often the session ended up being restarted or the user had to intervene mid-run.
- Whether the "never ask" template produced downstream rework (detectable by cross-referencing git history for the repo — reverts, follow-up commits labeled "fix", etc.).

**NE-3 — Pre/post kerf reviewer-sub-agent adoption (natural time-series).**
Kerf evolved a parallel-reviewer pattern mid-way through its 52-session history. Sessions before vs after the adoption, on roughly similar work (spec writing), provide a within-project time-series natural experiment. Outcomes: did post-adoption spec-writing sessions need fewer human correction turns? Did they produce more review-surfaced issues that the human then had to decide?
- Need: identify the exact session in which the reviewer pattern became convention and split the corpus around that dividing session.
- Risk: confounded with general user learning over time.

**NE-4 — f588ff0c's within-session protocol shift (within-session contrast).**
f588ff0c has a clean mid-session phase transition: short-volley dialog for 5 turns, then H#6 "I'm going to bed, go for it" dispatches into a 48-minute autonomous run. Within one session: two protocols, two outcomes visible. The post-dispatch run produced work that the human evaluated the next morning; the pre-dispatch dialog produced course-correction turns. Measure: correction-cycles in dialog phase vs correction-cycles on the output of the autonomous phase (the latter live in subsequent sessions or in git history).

**NE-5 — Context-dump (13493c8d) vs dialog-first (3bf5774c) on the same project.**
Both are harmonik sessions. 13493c8d is a 5-turn context-dump founding-vision session; 3bf5774c is a 21-turn short-volley design session. Both produce documents that got committed. The comparison is: which one produced artifacts that were more stable over subsequent edits? (Git blame on the resulting files tells you.) This bridges planning evaluation into downstream-stability evaluation, which is one of the §2 criteria the user flagged as possibly missing (regret probability, maintainability).

**NE-6 — Numbered-question-close vs open-ended-close (within- and between-session).**
The form-vs-content lens established that agent turns ending in 2–4 numbered questions produced systematically shorter and cleaner human responses than agent turns ending in "let me know what you think." This is the highest-n natural experiment in the corpus because every agent turn is a data point. A script can classify each agent-turn close into {numbered-list, open-ended, declarative, other} and measure the length/category of the following human turn. This is the single most immediately operationalizable natural experiment.

**NE-7 — Template-dispatch violations as a leakage signal.**
Sessions opened with "never ask questions" that nevertheless ended up with human intervention are failures of the protocol. Counting them, categorizing *what* the agent surfaced despite the clause, and comparing that distribution against the class of questions asked in dialog sessions tells you: (a) what questions are so structurally load-bearing that even a strong no-question clause can't suppress them, and (b) whether those are the same architectural-framing questions the decision-delegation lens already flagged as under-asked elsewhere. If yes — that's convergent evidence on which question-class is real and which is noise.

### 1.2 Within-session shifts

Natural experiments where a single session contains two protocols:

- **Context-ladder sessions (3bf5774c, f588ff0c):** rich handoff → short-volley clarification → eventually a dispatch or not. Compare cost-per-turn in the short-volley phase against the dispatch phase on the same day, same task, same user state. Controls for everything except protocol shape.
- **Meta-moments (00eb9fc9):** agent proposes batched questions; human pushes back; subsequent turns use serial questions. Before-and-after protocol within a single session, ~30 minutes apart. The corpus contains at least 3 such corrective turns (the "this is a pattern we want to try and limit" exchange is explicit).
- **Correction-cycle containment:** each misalignment incident identified in the misaligned-assumption lens (f588ff0c H#4/5 label-listing, 3bf5774c H#3 locked-framing, 3bf5774c H#4 Beads framing, etc.) can be scored for how many turns it took from first-signal to resolution. Incidents correlate with preceding protocol features — e.g., incidents where the agent used declarative framing ("the mechanism is locked") vs candidate framing ("candidate position: locked") had different catch speeds.

### 1.3 Pre/post convention changes

Identifiable convention-change events:
- Adoption of kerf (harmonik, roughly around f588ff0c).
- Adoption of the secure-dev "never ask" template (identifiable by first session containing that phrasing).
- Memory-system adoption (visible in `~/.claude/projects` metadata).
- CLAUDE.md/AGENTS.md updates (trackable via git).
- Phase-1-of-this-research-track itself — sessions after the user started flagging batch-questions-are-bad (00eb9fc9 and later) should show shorter agent question batches.

Each event is a discrete intervention. Sessions within 2 weeks before and 2 weeks after each event form matched comparison sets.

### 1.4 What a within-corpus comparative analysis looks like concretely

**Data pipeline:**
1. Extract all 195 sessions with the existing `extract_dialog.py` filter.
2. Per session, compute a feature vector:
   - `ht` (already computed); per-turn char counts (human, agent); per-turn tool-call count; agent-turn-close category (numbered / open / declarative / mixed); question-count per agent turn; human-turn category from the writing-load lens ((a)/(b)/(c)/(d)/(e)/(f)/(g)); autonomy-stretch durations; opener features (has-autonomy-partition, has-context-dump, is-recovery-handoff).
   - Correction incidents: regex/LLM-classify human turns that express pushback ("wait", "no", "actually", "that's not", "I don't think", "you're asking me X but…"). Each incident gets a parent agent-turn reference.
3. Per session, compute outcome metrics:
   - Total substantive human chars.
   - Number of correction cycles.
   - Longest autonomous stretch.
   - Ratio of human-question-answering turns to total human turns.
4. Aggregate by protocol feature across sessions. For numbered-question-close, compute mean-next-human-turn-chars for `close_type = numbered` vs `close_type = open-ended`. Difference + Mann-Whitney U at p < 0.05 is a first-pass signal.
5. For natural-experiment pairs (NE-1, NE-3, NE-5), do case-study write-ups showing the full before/after behavior + the feature-vector evidence.

**What this produces:** a data-driven ranking of which protocol features correlate with lower-writing-cost sessions, with effect-size estimates and confidence. It does *not* prove causation — it produces hypotheses ranked by observational-data support.

**Cost estimate:** 2–4 hours of scripting (feature extraction), 2–3 hours of correction-incident classification (human-in-the-loop or LLM-assisted), ~1 day total to get a first pass on all 195 sessions.

### 1.5 Limits of corpus-only inference

- No counterfactual. Every session is a single trajectory; we don't observe what would have happened under a different protocol.
- User is the same person, so generalization beyond the user is zero.
- Session difficulty is not controlled. A short session might be short because the protocol was good or because the task was simple.
- Selection bias on which 10 sessions got hand-extracted — they were picked for dialog-richness, which may correlate with protocol quality.
- Confounding: convention changes coincide with general user learning, project maturity, and model version changes. Hard to disentangle without explicit instrumentation.

**Verdict on §1:** the corpus is rich enough to support several real natural experiments with measurable outcomes, especially NE-1, NE-2, NE-6, NE-7. A within-corpus comparative analysis is **cheap enough to do now** (~1 day) and produces quantitative support for/against the Phase 1 findings. It will not resolve questions that require counterfactuals (A/B), but it will filter which Phase 1 findings are supported by cross-session signal vs are artifacts of a small hand-picked set.

---

## 2. Test-suite-of-planning-problems

A test suite would be a fixed set of planning problems, each playable against multiple protocols, with pre-agreed outcome measures. This is the canonical empirical evaluation shape in ML/systems research and the user explicitly asked whether it applies here.

### 2.1 Essential shapes of planning problems

From the corpus and the harmonik/kerf context, the recurring planning-problem shapes are:

1. **New-subsystem design.** "Design a state-source-of-truth model that satisfies these constraints." Archetypes in corpus: 13493c8d, 3bf5774c.
2. **Refactor-across-modules.** "Introduce a new shared contract that three existing modules must conform to." Archetypes: kerf's jig evolution sessions.
3. **Bug investigation.** "This test is failing because X is not being updated when Y changes. Find the root cause and propose a fix." Archetypes: many secure-dev dispatch sessions.
4. **Spec refinement.** "Given this draft spec, identify the ambiguities that would cause implementation rework, and close them." Archetypes: kerf 38415843 late turns, harmonik foundation passes.
5. **Scope decomposition / roadmapping.** "Given this goal, decompose into sequenced tasks with dependencies." Archetypes: harmonik STATUS.md construction, machine-setup 2a50e0fc.
6. **Integration planning.** "These two existing systems must be combined. Identify the coupling surface and propose an interface." Archetypes: Beads-in-harmonik integration discussions.
7. **Tool-selection / adoption.** "Given this need, which off-the-shelf tool (if any) fits, and what's the onboarding path?" Archetypes: kerf f588ff0c (kerf-vs-ROADMAP.md decision).
8. **Research scoping.** "Frame a research question that is investigable and non-trivial." The 00eb9fc9 session itself is an example of this shape.

Each shape has a different expected protocol profile (e.g., bug investigation is agent-investigative; roadmapping is human-dominant with agent-elaboration; spec refinement is Socratic-compatible). A test suite should cover at least these 8 shapes.

### 2.2 Suite size and complexity range

**Minimum useful suite:** 8 problems, one per shape, each at medium complexity (roughly: needs 10–20 turns to plan under a medium protocol, has 2–4 load-bearing decisions, has at least one latent assumption that a good protocol should surface).

**Better suite:** 16 problems, two per shape, at different complexity levels (a small and a large instance of each shape). This lets you test whether protocol effectiveness is shape-dependent vs complexity-dependent.

**Stretch suite:** 24 problems, including adversarial cases — a problem with a hidden red herring (a wrong-but-plausible framing the protocol should help detect), a problem with contradictory requirements (tests whether the protocol surfaces the contradiction), a problem with multiple equally-valid solutions (tests whether the protocol over-commits).

### 2.3 Source of problems

Three candidate sources:
- **Real problems from harmonik's backlog.** High ecological validity (these are problems the user actually needs to solve). Downside: once a problem is "solved," it's burned — the user now knows the answer and can't credibly replay it.
- **Retrodictive reconstruction from corpus.** Take a concrete problem the user solved in an existing session, strip the dialog, keep only the opening framing, and play it against a different protocol. This gives natural comparison (was the alternate-protocol outcome similar? different? faster?). Downside: the user has already thought about it; cognitive state is not reset.
- **Synthetic problems from adjacent projects / open-source repos.** Pick small open-source codebases with well-documented planning artifacts (design docs, RFCs). Compose planning problems from their actual histories. High novelty for the user; downside: not harmonik-relevant, so generalizability-back-to-harmonik is uncertain.

A mix is probably right: 4 retrodictive reconstructions (where the user's own answer is the gold label), 2 real harmonik problems being burned for evaluation (the "cost"), 2 synthetic from outside projects (to test transfer).

### 2.4 Success measurement

Three candidate outcome measures, each with different tradeoffs:

**M1 — Produced-plan properties.** Score the plan output itself: does it name the load-bearing decisions? Does it close them or park them? Does it state assumptions? Does it have an actionable next step? Properties like these can be checked by a reviewer or by a rubric. Advantage: evaluable without downstream work. Disadvantage: the rubric IS the evaluation criteria in disguise — scoring against a rubric just pushes the "what makes a good protocol" question onto "what makes a good rubric," which is the same question.

**M2 — Downstream implementation outcomes.** After the plan is produced, implement against it. Measure: implementation produced working code? how many corrections from human during implementation? how much of the plan turned out to be wrong (required late-stage rewrite)? Advantage: real-world signal. Disadvantage: expensive — requires implementing every tested plan; might take weeks per test point. Infeasible for a solo user at any nontrivial n.

**M3 — Independent-rater plan evaluation.** Show the produced plan to one or more raters (human or LLM-based), have them score it against pre-agreed criteria. Advantage: cheaper than M2, more objective than M1. Disadvantage: rater-reliability is a real concern with n=1 human rater; LLM raters have their own biases and may score verbose plans higher regardless of quality.

**Proposed evaluation composite:** use M1 with a minimal rubric (5–7 binary properties derivable from the corpus analysis — e.g., "names the architectural decision explicitly," "states one assumption that might be wrong," "parks at most one decision") combined with M3 using 2 raters (the user + one LLM-as-rater-with-adversarial-prompt) plus a retrospective M2 check on a subset (say, 2 of 8 problems) several weeks after the evaluation to ground-truth the rubric scores.

### 2.5 Replayability across protocols

To replay a problem against multiple protocols, the planning agent cannot carry memory from the previous protocol run. Mechanisms:
- Fresh Claude Code session per trial (obvious).
- The problem statement is a fixed, canonical text that is identical across trials.
- The protocol is injected via a system-prompt prefix or a CLAUDE.md override (harmonik already structures CLAUDE.md this way).
- The human state is not resettable — this is the fundamental limit of single-user evaluation. Mitigation: randomize protocol order so user-state drift doesn't align with protocol identity; run trials over multiple days so user isn't in a "just ran 3 trials in a row" fatigued state.

### 2.6 Gaming and prevention

A planning agent primed to a specific protocol might optimize for the protocol's visible signals rather than for good plans. Example: if the rubric rewards "names the architectural decision explicitly," the agent might start every plan with "THE ARCHITECTURAL DECISION IS: [restates the problem]." This is goodharting — hitting the measured proxy without the underlying quality.

Prevention:
- **Rubric criteria should be evaluable by a rater who doesn't know the protocol.** Blind-score the plans. If the criterion can be satisfied by a trick sentence, it's a bad criterion.
- **Use adversarial raters.** An LLM rater specifically prompted with "identify three ways this plan could be wrong" catches surface-only satisfaction of criteria.
- **Downstream M2 check catches goodharting by construction** — a plan that satisfies the rubric but fails in implementation is a rubric failure, not a protocol success. This is the main reason to include even a small M2 sample.
- **Rotate the rubric.** If the rubric is known to the agent (via protocol prompts), rotate which criteria are scored across trials. Agent can't optimize for all criteria at once without becoming better in ways we'd actually want.

### 2.7 Rater reliability

Single-rater scoring from the user alone is a real concern: the user wrote the problems AND knows which protocols they hypothesize will win. Mitigations:
- **Blind the user to protocol identity when rating** (have the protocol names stripped from the transcripts shown for scoring).
- **Pre-register the rubric** — write the criteria before any trials run; don't tune after seeing results.
- **Inter-rater reliability with an LLM adversarial rater** — compare user scores against LLM scores; high disagreement on a criterion means the criterion isn't evaluable and should be removed.
- **Score only binary properties** — "names the architectural decision Y/N" has near-zero rater variance. Continuous scales ("how clear is the plan, 1-7") have huge single-rater variance.

### 2.8 Feasibility verdict

A test suite at the "minimum useful" scale (8 problems × 3 protocols × 1 trial each = 24 planning sessions) is the upper end of what a solo user can plausibly execute without burning out. Each session is 30–90 minutes of active dialog. Total: 12–36 hours of user time, spread across 2–6 weeks. That's real cost but not prohibitive.

**The largest cost is not the trials themselves but the preparation:** writing 8 high-quality problem statements, defining the rubric, blinding the data, setting up protocol-injection harness. Estimate 20–30 hours prep. This is a multi-session research commitment in its own right, probably at the scale of the Phase 1 work.

---

## 3. A/B testing feasibility

### 3.1 Within-subject design with the user

Yes. The user is the only subject, and within-subject A/B designs are well-suited to n=1. Basic shape:
- Pick one protocol feature comparison (e.g., numbered-question-close vs open-ended-close).
- Define a pair of matched planning problems (same shape, same approximate complexity).
- Randomize which protocol is applied to which problem.
- Run both sessions, separated by a cooldown day to reset user state.
- Compare on pre-registered metrics.

For harder comparisons (e.g., entire-protocol-vs-entire-protocol), more trial pairs are needed.

### 3.2 Minimum n for meaningful signal

For a single protocol-feature comparison with a clear outcome metric (e.g., next-human-turn-char-count after numbered-vs-open-ended agent-turn-close):
- If the effect is large and consistent, 3–5 pairs can show it. The corpus already gives a first signal; A/B just validates.
- If the effect is medium, 8–12 pairs.
- If the effect is small or noisy, 20+ pairs — infeasible for a solo user.

For a full between-protocol A/B (protocol A vs protocol B on complete planning sessions), the minimum useful n is probably 5–7 pairs (10–14 planning sessions) because session-level outcome noise is high.

### 3.3 Plausible budget

Realistic estimate for a senior solo developer working on harmonik: **5–10 A/B pairs over a 2-month period**, with interruption risk. That puts a hard ceiling on the A/B scope:
- **Feasible:** 1–2 carefully-chosen protocol-feature comparisons, each with 5–8 matched pairs. Each comparison answers one focused question.
- **Infeasible:** full-protocol vs full-protocol comparison with statistical rigor. Not enough pairs.

Strategy implication: **A/B is an instrument for validating the top 1–2 findings from corpus analysis, not for discovering new ones.** Use corpus comparison to generate hypotheses; use A/B to confirm the most important.

### 3.4 Biases in single-user A/B

Principal biases:
- **Order effects.** User may get better at a task during trial 2 just from having seen trial 1. Mitigate: randomize which protocol runs first per pair; include a washout period.
- **Expectation / confirmation bias.** User has hypotheses about which protocol will win. Mitigate: blind the protocol identity during rating (not during running — that's unavoidable because the protocol IS the interaction); pre-register hypotheses and metrics.
- **State drift.** User fatigue, mood, time of day, project pressure. Mitigate: run trials at a consistent time of day (e.g., morning); log user-reported state at session start; exclude sessions where state self-report is extreme.
- **Task-matching error.** The "matched pair" problems may not actually be equally difficult. Mitigate: use retrodictive reconstruction (§2.3) so both problems have a known prior answer, and match on that prior answer's complexity.
- **Protocol bleed.** User learns something from protocol A in trial 1 that affects performance on protocol B in trial 2 regardless of which protocol is actually active. Hard to mitigate. Only way: long gaps between trials and large number of trials so learning effects average out. For a solo user this isn't really achievable; it's an unavoidable limit.

### 3.5 Verdict on A/B

**Feasible but narrow.** Use A/B as a confirmation instrument for the 1–2 highest-leverage hypotheses emerging from corpus analysis or external-source pass. Do not try to A/B-test the full catalog — the budget doesn't cover it.

Concrete recommended A/B target for this research: **numbered-question-close vs open-ended-close**, because the corpus already gives strong observational signal, the outcome metric (next-human-turn char count) is quantitative and zero-judgment, and 5–8 pairs is enough to confirm. Cost: ~10 one-hour planning sessions over a month. Value: validates one of the Phase 1 lens findings with counterfactual evidence.

---

## 4. Simulation-based evaluation

### 4.1 LLM roleplaying a human user

The idea: instead of involving the real user in A/B or test-suite trials, have an LLM roleplay a human user with a specified persona and knowledge-state, interacting with a separate planning agent. Run many such simulated sessions cheaply; compare protocols in aggregate.

**Obvious limits:**
- **The roleplay-user doesn't share the real user's latent priorities.** Real user corrects "Beads isn't a cache" based on a precise mental model; a roleplay user corrects based on whatever the persona specification says, which is a lossy reflection of the real model.
- **Writing-effort measurement breaks.** Simulation-user "writing effort" is token-generation cost for the LLM, not typing-seconds for a human. The primary §2 criterion is human-scarce-resource, which a simulation structurally cannot preserve.
- **Simulation users may be too coherent.** Real humans have context-switch state drift, fatigue, sudden scope changes, half-formulated intents. Simulation users are consistently themselves. This biases the evaluation toward protocols that work for consistent users and against protocols that handle fuzziness.
- **Model-coupling artifacts.** If the simulator-user and planner-agent are the same model family, they will over-agree in ways that don't generalize. Using different families (GPT roleplay-user vs Claude planner) partially mitigates but doesn't eliminate.
- **Ground-truth plan quality is still evaluated by an LLM or human rater.** The downstream-implementation signal (the strongest ground truth) is unavailable — simulation can't implement against a plan and see what happens in reality.

### 4.2 What simulation can measure cheaply

Despite the limits, simulation is *good* at measuring structural / surface features of protocols that don't depend on deep user state:
- **Turn-count and character-count distributions.** "Does protocol P produce fewer turns on average?" This is model-intrinsic, not user-intrinsic.
- **Numbered-question-close → response-shape.** Whether agent-turn-close format changes next-turn format is largely a function of the conversational pattern, not user psychology. Simulation can test this at very high n.
- **Agent self-correction rates.** "Does protocol P produce more or fewer cases where the agent catches its own error before asking?" Self-correction is observable in the agent's text regardless of simulated user.
- **Sensitivity to opening-message shape.** Does the protocol still work when the opening is terse vs rich? Simulation can run hundreds of variations.
- **Rubric-satisfaction rates.** For rubrics that score the produced plan (§2.4 M1), simulation is fine — the rubric scoring doesn't care whether the user was real.

Simulation is *bad* at measuring:
- **Writing-effort reduction** — the core §2 criterion.
- **Robustness to user-state** — simulation users don't have variable states.
- **Alignment-to-latent-intent** — simulation users don't have latent intent.
- **Context-switch load** — not meaningful for an LLM "user."

### 4.3 External validity requirements for simulation

For simulation results to transfer to real use:
- **Calibrate the roleplay user against real corpus turns.** Sample the real user's actual messages from the corpus; build persona descriptions that reproduce their vocabulary, question patterns, and known preferences. Validate by having the simulation-user respond to a real planner session start and checking whether the simulated response is similar in distribution to the real one.
- **Use a held-out corpus for validation.** Reserve some real sessions that were not used to calibrate the simulation-user. Check that simulated sessions on matched problems converge with the real sessions on the simulated metrics.
- **Narrow claim scope.** Simulation can credibly claim "protocol P reduces next-turn length vs protocol Q, all else equal." It cannot credibly claim "protocol P is better overall."

### 4.4 Verdict on simulation

**Most valuable for specific, mechanistic sub-protocol features.** Especially:
- NE-6 (numbered-close vs open-close) at high n.
- Sensitivity of protocols to opener-length, opener-structure.
- Agent self-correction rate under different decision-delegation clauses.

**Not a substitute for A/B on the full protocols** because of the writing-effort and latent-intent problems. Best used as a hypothesis-generating filter: run many simulated trials to identify which sub-protocols merit a real A/B, then spend the real-user A/B budget only on those.

---

## 5. Observability-first / practitioner-diagnostic stance

The argument: rigorous empirical evaluation may be infeasible at solo-user scale. What is feasible is generating diagnostic signals that a practitioner can read during their actual work to tell whether a protocol is going well or poorly. Instead of "is protocol P better than protocol Q in general," the question becomes "is this particular session showing the signs of bad protocol fit right now, and should I switch."

### 5.1 Case for the practitioner-diagnostic framing

- **Protocols are probably context-sensitive.** §3 of the research statement already foregrounds task-shape / user-state / session-phase dependencies. Even if you had infinite A/B budget, the answer would be "it depends." A framework that reports session-health-now is more directly actionable than a static ranking.
- **Signals are cheap.** The corpus analysis has already produced candidate signals: "multi-question agent turns produce long next-human-turns" is a diagnostic an instrumented session could surface live. No comparison across sessions needed.
- **Observability matches harmonik's own thesis.** Harmonik is built around deterministic instrumentation of agentic work. A planning-protocol observability layer is architecturally aligned with the project's direction, not a research one-off.
- **Closes the loop faster.** A rigorous evaluation framework takes months to produce results. A diagnostic framework produces value during the next session.

### 5.2 What a practitioner-diagnostic framework looks like

Proposed shape:
- **Instrument session transcripts live** (or post-hoc daily). Extract the features from §1.4.
- **Surface a small set of signals during or after sessions:**
  - "Agent-turn-close numbered-question rate: 30% (historical avg: 70%). Consider checking if agent has drifted from short-volley mode."
  - "Correction incidents in last 5 turns: 2 (high). Most recent correction type: architecture-framing. Consider: did the agent assert rather than propose?"
  - "Decision-deferral rate to human: 45% (high). Most-deferred category: file-naming. Consider: add autonomy-partition clause in opener."
  - "Longest autonomous stretch this session: 0 min. (Expected ~10+ min if the intended protocol is context-dump.)"
- **Periodic review of signals across sessions.** Weekly or per-work-item review of which signals trended bad, paired with qualitative reflection.
- **No claims about "protocol P is better" without extra instrumentation.** The framework tells you "this session had these features," not "these features are optimal."

### 5.3 What it loses vs rigorous empirical

- Cannot prove a protocol is better. Only tracks within-user patterns over time.
- Vulnerable to local-maxima anchoring — diagnostic signals are derived from the user's own corpus, so they reinforce the user's existing practice. Unless external-source protocols are added to the signal vocabulary (e.g., "is this session Socratic-enough?"), the diagnostics can't surface moves from outside the corpus.
- Can drift: if the user adopts a bad protocol and stays with it, the diagnostics normalize to the bad baseline.

### 5.4 Verdict on practitioner-diagnostic

**Highly valuable as a complement to empirical evaluation, not as a replacement.** Specifically:
- It is the correct thing to build *if* the empirical framework is not built. It's the minimum-viable evaluation infrastructure.
- It is *still* valuable if the empirical framework is built — diagnostics run in parallel with real work, empirical runs in batches.
- It has near-zero cost to start (the extraction infrastructure exists; adding signals is incremental).
- Its main limitation (local-maxima) is addressable by deliberately baking in signals that detect absence of external-source patterns, not just presence of observed patterns.

This framing also aligns with the "evaluation framework may be the most valuable output" hypothesis in §2 of the research statement — a practitioner-diagnostic framework is itself a durable research output that would compose with downstream harmonik features.

---

## 6. Recommendations

Ranked by ROI for a senior solo developer over a realistic Phase 2 timeline (~4–8 weeks of part-time research).

### R1 — Within-corpus comparative analysis (highest ROI, can do now)

**What:** Extract feature vectors from all 195 sessions, run the natural-experiment tests in §1.4 (especially NE-1, NE-2, NE-6, NE-7). Produce a quantitative scorecard for each Phase 1 lens finding.

**Can measure:** correlation between protocol features and outcome metrics (writing volume, correction count, autonomy stretch length, next-turn response shape). Not causation.

**Cost:** ~1 day of scripting + 1 day of analysis.

**Who executes:** Can be done in the current Phase 2 session or by a sub-agent. Does not require explicit user authorization beyond "yes, run the analysis."

**What it buys:** separates Phase 1 findings that are robust across the corpus from findings that are artifacts of the 10-session hand-picked set. This filtering is critical before spending any further research budget.

### R2 — Practitioner-diagnostic framework (high ROI, start now, refine indefinitely)

**What:** Define 6–10 session-health signals derived from the corpus analysis. Instrument them in a script that runs against any session transcript. Produce a weekly diagnostic report during ongoing harmonik work.

**Can measure:** drift from known-good protocol patterns, presence/absence of observed signals, unusual sessions worth retrospective analysis.

**Cost:** ~1 day to specify signals + ~1–2 days to script + ongoing low-cost per report.

**Who executes:** Phase 2 can produce the specification; implementation is arguably out of scope for "no-code" research but the scripting is narrow enough to fit under "scripting for extraction/analysis is fine" in the locked choices.

**What it buys:** value during the next session, not at Phase 2 close. Composable with harmonik's direction.

### R3 — Simulation-based high-n tests of narrow sub-protocols (medium ROI, cheap at this scale)

**What:** Build an LLM-roleplay-user calibrated against the corpus. Run 50–200 simulated sessions varying numbered-close vs open-close (and 2–3 other narrow features). Compute distributions on next-turn shape.

**Can measure:** mechanistic/structural sub-protocol effects at high n. Cannot measure writing-effort-for-real-user.

**Cost:** ~2–3 days of setup + API cost (low for Haiku/Sonnet at this scale).

**Who executes:** requires explicit user authorization because it consumes API budget and involves writing code (even if just harness). Best scheduled after R1 identifies which sub-protocols deserve confirmation.

**What it buys:** high-n validation of 1–2 observational findings. Complements A/B because it can sweep parameters that A/B can't afford.

### R4 — Test suite of planning problems (high value, high cost)

**What:** Build the 8-problem minimum test suite (§2). Run 2–3 protocols × 8 problems = 16–24 real planning sessions over 4–6 weeks. Score with M1 rubric + blind-M3 raters.

**Can measure:** whether a protocol works across problem shapes, what its best-fit shapes are, cross-shape transferability.

**Cost:** 20–30 hours prep + 16–24 real planning sessions (roughly 30 hours of focused user time). Total: 50–60 hours over 4–6 weeks.

**Who executes:** cannot be done in the current Phase 2 session. Requires explicit user authorization *and* a committed timeline. This is the biggest-ticket evaluation artifact possible within the research statement's scope.

**What it buys:** the strongest empirical signal available to a solo user. But the cost is substantial and displaces other Phase 2 work (external-source pass, counter-pattern investigation, unified catalog).

### R5 — Focused A/B on top-1 protocol feature (medium-high ROI, feasible but time-bounded)

**What:** After R1 produces the top hypothesis, run 5–8 matched A/B pairs on that single feature. Pre-register metric and rubric.

**Can measure:** single counterfactual: does this one protocol feature produce the predicted effect?

**Cost:** 5–8 hours of planning sessions over 2–4 weeks, plus prep.

**Who executes:** requires explicit user authorization and commitment to a multi-week trial window. Can be prepared during Phase 2 but only executed after Phase 2 closes (because it needs real sessions across time).

**What it buys:** one well-confirmed finding. High credibility per finding, but narrow.

### R6 — Formal evaluation framework as a Phase 2 deliverable (yes, and here's the shape)

A formal evaluation framework can absolutely be a Phase 2 deliverable. Concrete sketch:

**Name:** `evaluation-framework.md` (already flagged in research-statement §7 deliverables).

**Contents:**
- Operationalized definitions of each §2 criterion, with measurement procedure.
- Catalog of natural-experiment pairs in the corpus, with feature-vector extraction specs.
- Rubric for plan quality (M1): 5–7 pre-registered binary properties.
- Signal catalog for practitioner diagnostic (R2).
- Simulation harness specification (R3).
- A/B protocol template for narrow feature comparisons (R5).
- Test-suite specification (R4), with explicit problem seeds.
- Rater-reliability plan (blinding procedure, pre-registration discipline, adversarial-rater configuration).
- Explicit statement of what the framework *cannot* measure at solo-user scale.

This framework would itself be the Phase 2 evaluation output — against which every candidate protocol from the unified catalog (research-statement §7 Step 4) gets scored. R1's corpus-data feeds into the score; R4/R5, if run later, update it.

The framework as a document is feasible within Phase 2. **The experimental runs against the framework (R4, R5) are post-Phase-2 work.** Phase 2 can produce the instrument; it cannot exhaust the measurements.

### Summary matrix

| Mechanism | Measures | Cost | Phase 2 session? | Post-Phase-2? |
|---|---|---|---|---|
| R1 — Corpus comparative analysis | Cross-session correlations; filter Phase 1 findings | 1–2 days | **Yes, can execute now** | N/A |
| R2 — Practitioner-diagnostic framework | Per-session health signals | 2–3 days spec + ongoing | **Spec can be done now**; implementation borderline | Implementation if not done |
| R3 — Simulation sweeps | Mechanistic sub-protocol effects at high n | 2–3 days + API | Specification can be drafted; execution needs user auth | Execution |
| R4 — Test suite trials | Protocol-vs-protocol across problem shapes | 50–60 hrs over 4–6 weeks | Suite spec only | All trials |
| R5 — Focused A/B | One counterfactual per feature | 8–15 hrs over 2–4 weeks | Design only | Execution |
| R6 — Evaluation framework doc | (itself the instrument) | 1–2 days after R1 | **Yes, highest-priority deliverable** | Updated as R3/R4/R5 run |

### What the main Phase 2 session should execute now

- **R1** in full. The corpus is there; the scripting is tractable; the output filters every subsequent claim.
- **R2** as a specification. Ship the signal catalog as part of the evaluation framework document.
- **R6** as the Phase 2 Step 1 output (or a close companion to it). This is the durable instrument.
- **R3, R4, R5** as *specifications only* inside R6. Execution is user-authorization-gated and post-Phase-2.

### What requires explicit user authorization

- R3 (API budget, harness code).
- R4 (multi-week commitment, real session time).
- R5 (multi-week commitment, real session time).
- Any evaluation that burns real harmonik backlog problems (§2.3).

### What is strictly infeasible at solo-user scale

- Multi-subject A/B.
- Full-protocol-vs-full-protocol rigorous comparison with statistical power.
- M2 (downstream implementation outcomes) at >2–3 problems.

---

## Closing observation

The evaluation question has a structural asymmetry the research statement didn't quite make explicit: **generating hypotheses is cheap, confirming them is expensive.** The corpus (R1) and external sources (research-statement Step 2) and counter-patterns (Step 3) generate hypotheses freely. A/B and test suites confirm one hypothesis at substantial cost.

This argues for a pipeline rather than a single evaluation event:
1. Generate broadly (corpus + external + counter-patterns) — Phase 2 Steps 2–4.
2. Filter cheaply (corpus comparison R1 + simulation R3).
3. Confirm narrowly (A/B R5 or test-suite subset R4) on the top 1–2 candidates.

The evaluation framework (R6) is the spine of this pipeline. It is the most valuable evaluation artifact Phase 2 can produce, because every future A/B, test-suite run, or practitioner-diagnostic session plugs into it. Candidate protocols come and go; the framework is durable.

This is the concrete shape of the research-statement §2 hypothesis that "a durable way to evaluate planning protocols may be a more valuable output than any specific protocol recommendation." Yes — and it is feasible to produce in Phase 2.
