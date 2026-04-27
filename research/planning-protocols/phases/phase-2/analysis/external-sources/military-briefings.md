# External Source: Military Briefings

**Phase 2 · External-Source Pass · Domain: Military Briefings**
**Author:** research sub-agent
**Date:** 2026-04-23

## Domain intro

Military operational planning is the longest-running, most-iterated formal-protocol domain for the "idea → plan → aligned execution" loop. Two centuries of deliberate doctrinal revision — from the Prussian response to Napoleon, through Moltke the Elder's codification of *Auftragstaktik* (1857–1888), to the US Army's ADP 6-0 *Mission Command* (2012, 2019) — have produced a tight family of artifacts whose shapes survived because dropping them produced specific, named failure classes in large-scale, high-friction, partial-information, partially-adversarial environments.

Four features of this corpus make it unusually useful as a source for human-agent planning protocols, even though the surface conditions (life stakes, adversary, hierarchy, chain-of-command authority) do not transfer:

1. **It is the paradigm case of pre-authorizing an autonomous agent under a human principal.** Mission command doctrine exists *specifically* to answer "how does a superior give direction to a subordinate who will execute out of contact, under conditions the superior cannot foresee, in a way that preserves intent but permits local judgment." That is — almost word-for-word — the human-agent delegation problem in coding-planning.

2. **It has separated intent from plan from order from modification.** The family {commander's intent, operations order, warning order, fragmentary order, back-brief, rehearsal} is a *tiered information-release architecture* where each tier targets a specific alignment failure mode. Few other domains have made these separations explicit and normative.

3. **It has empirically discovered the load-bearing tension.** The core doctrinal tension — "too vague and subordinates guess; too specific and subordinates can't adapt when reality diverges from the plan" — has been written about for 150 years. The best-practice resolution (compress the *why* into 2–3 sentences, be generous with the *how much authority* and *what counts as done*, be miserly with the *how*) is directly applicable.

4. **It has a named, normative read-back move.** The back-brief is the military cousin of medical read-back and pair-programming's narration, but with a distinct mechanism: the subordinate restates *what they intend to do and why they think it serves the intent*, not just *what they heard*. This is a plan-synthesis readback rather than a comprehension readback, and it catches a different error class.

### Disanalogies that bound transfer

- **Authority structure.** Military protocols assume a principal with command authority. Coding-planning is cooperative between two non-hierarchical parties. This removes the "order" framing and the legal/disciplinary enforcement backstop — what remains is the information-structuring core.
- **Adversary.** Military plans must survive contact with a thinking opponent. Coding has friction (requirements drift, integration surprise, technical-debt landmines) but no adversary. Mechanisms motivated purely by denying information to an opponent (compartmentalization, OPSEC) do not transfer.
- **Stakes and time pressure.** Life stakes make verbal-only transmission unsafe; coding-planning has slower tempo and written-first culture. The forcing functions for brevity (a subordinate must memorize the order because they will not have it in the fight) are partially relaxed.
- **Scale.** Military orders coordinate hundreds to thousands of actors. Two-party coding sessions do not need the organizational redundancy of the five-paragraph order's Command & Signal paragraph. Compression is available.

### What this domain offers the planning-protocols track

- **Commander's intent:** a 2–3-sentence purpose + key-tasks + end-state primitive that is *the* canonical mechanism for bounded autonomy under uncertainty. Directly instantiates the "pre-authorized decision locus" dimension.
- **Five-paragraph order (SMEAC):** a structured-artifact plan-expression template whose overlap and divergence with SBAR (medical) are diagnostic about which categories are load-bearing in *planning* vs. *handoff*.
- **Back-brief:** a synthesis-by-receiver move that is meaningfully distinct from medical read-back — it verifies plan quality, not just comprehension.
- **Warning → Operations → Fragmentary order (WARNO → OPORD → FRAGO):** a three-tier information-release cadence that maps onto the "when should the plan be firmed up, and how are mid-stream changes communicated" question.
- **Mission command doctrine:** an explicit theory of how much to specify, why over-specification fails, and what the principal owes the subordinate (intent + end state + enough resources) vs. retains (veto on scope and end state).
- **METT-TC:** a mission-analysis checklist whose slots map cleanly to "which categories of context does the agent need before proposing a plan."
- **Rehearsal hierarchy:** a graded pre-commit validation menu (map → sand-table → reduced-force → full-dress) that operationalizes "how much pre-action validation is this plan worth."
- **After-action review (four questions):** covered in incident-command sources; retained here only for the military-specific framing.

## Commander's intent — purpose / key tasks / end state

### Description

The commander's intent is a 2–3-sentence statement from the commander that accompanies (and, in mission command's strong form, dominates) the rest of the operations order. US Army FM 6-0 / ADP 6-0 defines it as "a clear and concise expression of the purpose of the operation and the desired military end state that supports mission command, provides focus to the staff, and helps subordinate and supporting commanders act to achieve the commander's desired results without further orders, even when the operation does not unfold as planned."

Current doctrinal structure (the "purpose-method-end state" or "purpose / key tasks / end state" triad):

- **Purpose** — the *why*. The enduring portion of the mission that remains valid even when the plan fails. Answers: "if we achieve nothing else, what must we have achieved?"
- **Key tasks** — the handful of actions that must happen regardless of which course of action is executed. Not *the plan* — the parts of any plan that must be present.
- **End state** — the conditions that, when observed, mean the operation is complete. Typically expressed as conditions with respect to friendly forces, the enemy, terrain, and civilians. Diagnostic, not procedural.

The canonical failure modes are both named and doctrinally warned against:

- **Too long / lengthy intent** — collapses the *why* back into *how*. "Lengthy and/or vague intent statements from task force make it difficult for a company commander to focus on what is really important."
- **Too specific / "how" leaks into "why"** — commander pre-forecloses options subordinates would need when reality diverges. "Too detailed descriptions may limit the subordinate's initiative."
- **Too vague** — subordinates cannot discriminate between actions that serve intent and those that defeat it.
- **Commander-by-committee** — when staff drafts the intent without the commander's active voice, subordinates report the operation feels "like a collaborative project of the staff guessing what the commander wants."

### Dimension values

- **Timing of alignment:** pre-action, at the very top of a planning cycle; re-read by subordinates when reality diverges from the plan
- **Decision locus:** high-pre-authorization — subordinates explicitly authorized to deviate from the plan to preserve the intent; "if acting without fresh orders is required, act in the spirit of the commander's intent" is doctrinal
- **Dialog form:** monologue artifact (short), paired with back-brief turn-taking
- **Question style:** implicit — the three slots (purpose / key tasks / end state) structure what the principal must disclose
- **Autonomy scope:** **explicit autonomy grant with bounded scope** — scope is bounded by "must preserve intent + satisfy end state conditions," not by enumerating permitted actions
- **Context richness:** rich-brief at the *intent* level, deliberately thin on procedural detail
- **Plan expression form:** intent-artifact (short prose) — distinct from plan-artifact (the OPORD)
- **Knowledge direction:** principal → subordinate, but principal is required to answer subordinate clarification questions before back-brief
- **Review/critique integration:** none internal to the artifact; the back-brief is the paired critique move

### Mechanism

Three interlocking mechanisms, each named in the doctrine:

1. **Plan-survivability under contact.** The plan will not survive contact with reality unchanged (Bungay; Moltke). If subordinates only have the plan, they have nothing when the plan fails. Intent is the portion of the message *designed to survive plan failure*. It is the residual guidance when the specific tactics are void.

2. **Compression of the principal's cognition into a transferable form.** The commander has a model of what "success" looks like that is deeper than any enumerated task list. Key tasks + end state are a lossy compression of that model — lossy by design, because the subordinate needs room for local judgment. The end-state conditions are the *test set* the subordinate uses to check their own course-of-action evaluation.

3. **Alignment-gap closure, not instruction.** Bungay's framing: the "alignment gap" is the difference between what we want people to do and what they actually do. Intent closes this gap by making the principal's *why* legible. Subordinates who have been given orders cannot bridge the alignment gap; subordinates who understand intent can.

The load-bearing innovation is the **refusal to specify "how."** The temptation under uncertainty is for the commander to specify in more detail — and this is what military doctrine explicitly warns against. The prescribed response to uncertainty is to specify *less* of the plan and *more* of the intent, not the reverse. This inverts the naive intuition and is the central teaching of mission command.

### Predicted trade-offs for coding-planning

**Helps with:** the agent-defers-trivial-decisions pain point and the agent-misaligned-assumption pain point simultaneously. A 2–3-sentence user's intent at the top of the session means (a) the agent has enough of the *why* to pre-authorize a class of local judgments rather than deferring, and (b) the agent has a diagnostic — the end-state conditions — against which to test its own plan before surfacing it.

**Helps with:** the compare-to-dispatch observation in the corpus. The prompt asks: "what would a one-paragraph 'user's intent' attached to every planning session do?" vs. the observed "dispatch" pattern. The key distinction is **what dispatch leaves unsaid**: dispatch typically specifies the *request* (what the user wants now) but not the *end state* (what "done" looks like) or the *key tasks* (which portions of any plan must survive). A commander's-intent-shaped opener forces all three.

**Costs:** the cognitive cost of writing a good intent is the highest-leverage move a principal makes but is *not cheap*. Military doctrine treats "writing intent" as a senior-commander skill that takes years to develop. Expecting a developer to write a well-formed intent at the top of every session is expensive unless the template is strongly scaffolded (explicit purpose / key tasks / end state prompts).

**Watch for:** the **how-leak** failure. Developers, like junior commanders, default to specifying *how* when they mean to specify *why*. An agent that sees an intent containing "use Postgres, add a migration, write tests in Jest" has been given a plan labeled as intent — and will not know which parts are negotiable. A good template must prompt separately: "Purpose (why — continue to guide even when the plan fails):", "Key tasks (what must be true of any plan):", "End state (what conditions mean this is done):".

**Watch for:** the **over-compression** failure. A 2-sentence intent that omits load-bearing constraints will produce agent action inside the intent but outside the principal's actual acceptance set. Intent length is load-bearing; terse is not always better.

### Adaptation notes — what transfers, what is an artifact of stakes

**Transfers (load-bearing in any high-coordination context):**
- The **purpose / key tasks / end state** triad structure.
- The **refusal to specify "how"** as the correct response to uncertainty.
- The **end-state-as-diagnostic** role — end-state conditions let the subordinate (agent) test their own plan before surfacing it.
- The **"survive plan failure"** criterion: the intent should be readable as guidance even when the current plan has failed.

**Artifacts of stakes / adversary / hierarchy (do not transfer):**
- The authority-grant framing ("subordinate *shall* deviate to preserve intent"). In a 2-party cooperative setting, the agent does not "shall" anything — this becomes "the agent is pre-authorized to decide within the intent without asking the human first."
- The concern about commander-by-committee. The user writes their own intent; there is no staff layer.
- The specific concern with survivability under kinetic contact. Replace with "survivability when the agent's first attempt fails and it must pick an alternative approach."

### Evaluation plan

1. **Corpus observational pass.** For each planning session in the Phase-1 corpus, extract what the user said at session opening and classify it against the {purpose / key tasks / end state} slots. Measure: how often is each slot present? Absent? Fused? Which fusions predict downstream misalignment?
2. **Compare against observed "dispatch" pattern.** If the observed dispatch pattern consistently omits end-state, that is diagnostic — dispatch is request-shaped, not intent-shaped.
3. **Counter-factual on failures.** For sessions that went wrong, reconstruct: would a commander's-intent-shaped opener have caught the misalignment? Classify by which slot would have caught it.
4. **Prospective micro-trial.** Inject a scaffolded intent template as a session opener. Compare (a) number of rounds to first aligned plan and (b) frequency of agent-defers-trivial-decisions events vs. matched controls.
5. **Reject-tests.** (a) Can users actually distinguish purpose from key tasks from end state when prompted, or do they collapse them? (b) Does intent-length correlate positively or negatively with alignment — the doctrine predicts a sweet spot, not monotone.

## Five-paragraph order (SMEAC)

### Description

SMEAC — Situation / Mission / Execution / Administration & Logistics / Command & Signal — is the standardized format for a full operations order (OPORD) at small-unit level (USMC, US Army infantry, and widely exported). It is the principal written-plus-verbal artifact that follows the commander's decision and precedes rehearsal and execution.

- **S — Situation:** what is happening in the operational environment. Subdivided into *enemy* (what the opponent is doing / will do), *friendly* (adjacent and higher units' operations), and *attachments/detachments* (who is attached to or detached from this unit).
- **M — Mission:** one sentence, containing the five W's — *who, what, where, when, and why*. The mission statement is terse and executable, and is intentionally derivable from (but narrower than) the commander's intent.
- **E — Execution:** *how*. Concept of operations, scheme of maneuver, tasks to subordinate units, coordinating instructions. This is the longest paragraph and holds the actual plan.
- **A — Administration & Logistics:** sustainment. Medical, resupply, prisoner handling, etc. — the supporting apparatus without which execution fails.
- **C — Command & Signal:** command relationships (who is in charge, succession if the leader is lost), communications plan, call signs, frequencies.

### Why each paragraph is load-bearing

The five-paragraph structure is robust because each paragraph targets a different error class, and dropping any one produces predictable failures:

- **Drop Situation** → subordinates apply default priors about the environment; miss that the enemy or friendly disposition has changed since last they knew. **Stale-context error.**
- **Drop Mission** → subordinates receive execution detail without a one-line summary of what it is for; first soldier to lose the written order loses the mission. **Summary-loss-under-degraded-conditions error.**
- **Drop Execution** → intent and mission are clear but there is no scheme. Uncoordinated action; units get in each other's way. **Coordination error.**
- **Drop Administration & Logistics** → the plan is executable on paper but the sustainment apparatus to actually execute it is not named. Execution halts mid-operation. **Resource-underspecification error.**
- **Drop Command & Signal** → execution begins, communications fail or a leader is lost, and no one knows who is in charge or on what channel. **Coordination-under-degradation error.**

The load-bearing design choices:

1. **Mission is separate from Execution.** The plan (Execution) is distinct from the one-sentence statement of what must be accomplished (Mission). If the plan fails, Mission survives. This is structurally similar to Assessment-vs-Recommendation separation in SBAR, and serves the same error-prevention purpose: the receiver can correct or adapt the plan without losing the problem statement.
2. **Situation precedes Mission precedes Execution.** The order is not arbitrary — it mirrors the cognitive order in which the receiver must ingest the material. They cannot evaluate the plan without first understanding the environment (Situation) and the objective (Mission).
3. **Administration and Command are factored out.** Sustainment and coordination-under-degradation are not part of the plan proper; they are prerequisites for the plan to work at all. Factoring them out prevents them from being omitted when the plan is the focus of attention.

### Overlap and divergence with SBAR

Direct comparison (mapping SBAR's four slots onto SMEAC's five):

| SBAR | SMEAC | Both, or only one? |
|---|---|---|
| Situation | Situation | Both — same slot name, essentially same function |
| Background | *(merged into Situation)* | SBAR splits out "what led us here" as its own slot; SMEAC merges it into Situation's enemy/friendly history |
| Assessment | *(implicit in Execution's concept)* | SBAR makes "my interpretation" explicit; SMEAC implicit — commander's reasoning is in the commander's intent above the order, not in the order itself |
| Recommendation | Mission + Execution | SMEAC splits "what we are doing" into Mission (the one-sentence ask) and Execution (the plan) |
| — | Administration & Logistics | Only SMEAC — medical handoffs do not need a separate sustainment slot because the receiving clinician takes over an in-place apparatus |
| — | Command & Signal | Only SMEAC — medical handoffs are between two individuals; SMEAC coordinates multiple units |

**Diagnostic takeaways for coding-planning:**

- The **Mission / Execution split** is the analog of the **Ask / Plan split**. It is load-bearing: if a session conflates "what outcome is needed" with "what approach will get us there," the agent cannot challenge the approach without appearing to challenge the outcome. Military doctrine has institutionalized this separation; coding-planning does not, and the corpus should be audited for conflation.
- SBAR has an explicit **Assessment** slot; SMEAC puts assessment in the commander's intent document *above* the order. For coding, the right analog is probably to keep Assessment (the interpretation) near the user (in the intent) and Plan (Execution) near the agent (in its proposal). This matches how the observed planning sessions actually divide labor.
- **Administration & Logistics and Command & Signal do not transfer directly** for solo developer + single agent. Their surviving traces: "tooling / environment setup assumed by this plan" (A&L-like) and "what the human will be doing vs. what the agent will be doing and how they will communicate mid-work" (C&S-like). Include only when meaningfully non-default.

### Dimension values

- **Timing of alignment:** pre-action, after intent is settled, before rehearsal
- **Decision locus:** principal retains Mission; Execution is where subordinate input (and therefore agent input) is most expected
- **Dialog form:** structured-artifact (written, also delivered verbally)
- **Question style:** implicit (five slots are the questions); clarification after delivery
- **Autonomy scope:** bounded-by-category — within each paragraph, the category defines the permitted scope
- **Context richness:** rich-brief — assumes the receiver has no shared state, builds it in Situation
- **Plan expression form:** **structured-artifact (the paradigm case)**; five-slot template
- **Knowledge direction:** principal → subordinate, with subordinate clarification turn
- **Review/critique integration:** paired with back-brief and rehearsal — the order itself is not self-critiquing

### Mechanism

SMEAC's primary work is **exhaustive coverage of alignment-failure categories in one artifact.** The five paragraphs do not describe the plan — they describe the *five conditions under which the plan can fail*: stale context (S), lost mission (M), uncoordinated action (E), unsupported execution (A), and coordination-under-degradation (C). Each paragraph is a prophylactic against a class of error that history demonstrated was common enough to warrant a named slot.

The doctrinal form is deliberately rich. Under the alternative — narrative orders — commanders varied in what they included; error classes varied by commander. SMEAC's forcing function is that the subordinate can ask "what was paragraph 3?" and get a bounded, answerable question, vs. "what did the commander say about execution?" which is open-ended.

### Predicted trade-offs for coding-planning

**Helps with:** the **session-shape consistency** problem. If every planning session closes with an artifact that has the same slots, (a) the agent knows where to look for each class of information, (b) the user knows what categories they are expected to have covered, and (c) session comparison / handoff / resumption becomes possible.

**Helps with:** the **plan-that-omits-sustainment** failure. Plans routinely assume infrastructure that does not exist (tooling, CI, env vars, data fixtures). An A&L-analog slot forces the user or agent to name prerequisites before execution starts.

**Costs:** overkill for small, routine sessions. A one-line bug fix does not need a five-paragraph order; the military analog is the FRAGO, which is deliberately short. The protocol needs a size-graded variant — this is the warning/operations/fragmentary tiering below.

**Watch for:** the template becoming ceremonial. If the user fills in all five slots mechanically, the quality of the plan does not improve — the slots must be cognitively active (each one must actually catch something). This is the same failure mode as checklists that degrade to rote.

### Adaptation notes — what transfers

**Transfers:**
- The Mission / Execution split.
- The "cover each error category in a named slot" principle.
- The ordering — Situation before Mission before Execution — as a cognitive scaffold.

**Artifacts of scale (do not transfer directly):**
- The Administration & Logistics paragraph's specific content (rations, medevac).
- The Command & Signal paragraph's specific content (call signs, frequencies).
- The normative length (military OPORDs are pages; a coding OPORD-analog should be one screen).

**Coding-OPORD candidate template (4 slots, sized for solo developer + agent):**
- **Context:** What is true about the codebase / prior decisions / current state that a default-priors reader would get wrong?
- **Outcome:** One sentence, who-what-where-when-why. (The "ask.")
- **Approach:** The plan — scheme of work, ordered steps, subtask ownership (human vs. agent).
- **Prerequisites & Comms:** What must exist before execution starts; how the agent will report progress or ask for input mid-work.

The A&L and C&S slots collapse into the fourth, which matches the compression appropriate to two-party work.

### Evaluation plan

1. **Corpus audit.** For each planning-session output in the corpus, classify which of {Context, Outcome, Approach, Prerequisites & Comms} slots are present and which are absent. Correlate absent slots with downstream rework.
2. **Mission / Execution split test.** Check how often the corpus conflates Outcome with Approach — i.e., the ask is stated in terms of the proposed implementation rather than the desired end state. If conflation rate is high, that is a strong signal the split is missing and load-bearing.
3. **Prospective.** Templated 4-slot artifact at the end of intent-opened sessions; measure rework rate and agent-clarification-request rate against matched controls.
4. **Reject-test.** Compare the 4-slot artifact against the simpler {intent + plan} pairing — does the explicit Prerequisites & Comms slot actually prevent infra-assumption errors, or do users skip it and fail the same way?

## Back-brief / confirmation brief

### Description

Two paired doctrinal moves, related but distinct:

- **Confirmation brief:** Immediately after receiving the operations order, the subordinate restates — in their own words — the commander's intent, their specific task and purpose, and how their mission fits with adjacent units'. Purpose: verify *comprehension* of what was just said. Analogous to medical read-back and MI reflection.
- **Back-brief:** Later in the planning cycle (after the subordinate has done their own mission analysis but *before* they issue their own subordinate OPORD), the subordinate briefs the commander on **how they intend to accomplish the mission** — their concept of operations, task organization, timings. Purpose: verify *plan quality and intent-nesting* before the plan propagates downward.

In short: the confirmation brief is a **comprehension check**; the back-brief is a **plan-quality check**. Both feed the same goal (intent nesting up and down the chain) but catch different errors.

Bungay, synthesizing this in management-book form, treats the back-brief as the alignment-gap closure mechanism: "after briefings, subordinates go through a process of 'backbriefing' their superiors to check their understanding of the intent and its implications before passing it down the line."

### Why each brief is load-bearing

- **Drop confirmation brief** → subordinate misheard, reconciled the order with wrong priors, or suppressed a comprehension gap. Error surfaces only after subordinate begins planning. **Assimilation-error, late detection.**
- **Drop back-brief** → subordinate understood the order but produced a plan that does not actually serve the intent, or that conflicts with adjacent units' plans. Error surfaces during execution. **Plan-doesn't-serve-intent error; inter-unit conflict error.**

The key design choice is **temporal separation.** Comprehension errors are caught early, before the subordinate wastes time planning. Plan-quality errors are caught later, after the subordinate has actually done the analysis the commander cannot do (local terrain, troop strength, specific capabilities) but before that plan is set in motion. A single combined checkpoint would either happen too early (no plan to evaluate) or too late (comprehension error has already corrupted the plan).

### Divergence from medical read-back and MI reflection

| | Medical read-back | Back-brief |
|---|---|---|
| What is restated | The orders/information just received | The plan the receiver has now developed |
| When | Immediately | After the receiver has done their own analysis |
| What it checks | Comprehension | Plan quality + intent nesting |
| What the principal does with it | Correct or confirm | Correct intent, critique plan, or refine mission |
| Error class caught | Mishearing, misencoding | Plan-doesn't-serve-intent, unrecognized local constraint |

The **confirmation brief** is the military analog of medical read-back. The **back-brief** is distinctive and has no exact medical analog. For coding-planning, this means both moves are potentially valuable but at *different stages* of a session:

- Confirmation brief analog → "agent restates the user's intent + ask before starting to plan."
- Back-brief analog → "agent presents its proposed plan + how it serves the intent + its uncertainty, *before* beginning implementation."

The second is qualitatively different from "agent proposes plan, human reviews." The back-brief framing requires the agent to *show its reasoning about how the plan serves the intent*, not just present the plan for critique. This is a distinct demand on the artifact and is load-bearing.

### Dimension values

- **Timing of alignment:** confirmation brief — immediately post-order (in-action); back-brief — post-analysis, pre-execution (mid-action checkpoint)
- **Decision locus:** verification, not decision; the principal retains final say
- **Dialog form:** structured turn (subordinate speaks, principal confirms/corrects)
- **Question style:** restatement-as-question
- **Autonomy scope:** not applicable (gating move)
- **Context richness:** rich — the back-brief contains the subordinate's full plan, not just a restatement
- **Plan expression form:** verbal narration of structured-artifact
- **Knowledge direction:** subordinate → principal, with principal correction
- **Review/critique integration:** **this *is* the review/critique integration** — read-back at comprehension stage, plan-critique at back-brief stage

### Mechanism

Three mechanisms, matching the three errors caught:

1. **Forced re-encoding.** The subordinate cannot restate the intent without having actually encoded it internally. Passive receipt of the order does not produce a back-brief; only active re-processing does. This exposes comprehension gaps that the subordinate would otherwise leave unresolved.
2. **Plan-against-intent dialog.** By presenting the plan *and* the reasoning for how it serves the intent, the subordinate forces their own mental model of the intent into observable form. If the plan does not serve the intent, the gap becomes visible — often to the subordinate first, mid-brief. Many back-briefs end with the subordinate self-correcting before the commander speaks.
3. **Commander self-correction.** If the subordinate's plausible plan violates the commander's intent, the commander often discovers that their *intent* was ill-stated, not the subordinate's plan ill-conceived. Bungay's framing: the back-brief is bidirectional alignment — the commander learns what their order actually communicated.

### Predicted trade-offs for coding-planning

**Helps with:** agent-misaligned-assumption. An agent that is required to back-brief before implementing will surface its misaligned assumption as part of the brief, where the user can catch it before work is done.

**Helps with:** the **post-draft-review fatigue** problem. If the user reviews every completed draft, the review cost scales with draft size. A back-brief move front-loads the alignment check — the user reviews a *plan and reasoning*, not a *completed draft*. Cheaper per check, catches the same misalignments.

**Helps with:** agent-defers-trivial-decisions. An agent that has back-briefed a plan including "here is what I will do, here is why, here is what I'll decide unilaterally, here is what I'll pause for" has explicitly named its autonomy scope. Subsequent work does not need new deferrals because the decision locus has been pre-negotiated.

**Costs:** adds a turn. If every session has a back-brief, sessions are longer. The cost is justified when the implementation step is expensive; less so for one-line changes.

**Watch for:** **ceremonial back-brief.** An agent that produces a plan and a narration of why it serves the intent, but the narration is post-hoc rationalization rather than genuine reasoning, has simulated the protocol without gaining its value. Detection: does the back-brief ever surface a disagreement or uncertainty, or does it always agree? A back-brief that never disagrees is a failed back-brief.

**Watch for:** the back-brief becoming the plan. If the agent's back-brief is as detailed as the plan itself, the protocol has doubled work without catching new errors. The back-brief should be *shorter than the plan* and *focused on how-it-serves-intent*, not how-it-works-in-detail.

### Adaptation notes — what transfers

**Transfers (load-bearing):**
- The **comprehension-vs-plan-quality separation.** Two distinct moves at two distinct times.
- The **show-the-reasoning requirement.** Not just restate the plan — restate *why the plan serves the intent.*
- The **bidirectional alignment** property. The user may discover their intent was ill-stated.

**Artifacts of hierarchy (adapt):**
- The commander-corrects dynamic. In coding-planning, the user-corrects dynamic is symmetric: the agent may also flag that it thinks the user's intent is internally inconsistent. The back-brief is not one-way.
- The time-budget constraint ("10 minutes maximum per back-brief"). Translates to "back-brief should be read in under a minute."

### Evaluation plan

1. **Corpus observational.** Identify in-corpus sessions where the agent *did* produce something back-brief-shaped (even unprompted). Compare downstream rework rate against sessions where the agent began work without back-briefing.
2. **Front-loaded vs. back-loaded review test.** Compare sessions with explicit pre-draft back-brief against sessions with only post-draft review, on (a) user-attention time and (b) misalignment rate.
3. **Surface-disagreement audit.** Require the agent to name at least one concrete uncertainty or potential conflict with intent in each back-brief. Measure how often the surfaced uncertainty was predictive of actual later rework.
4. **Reject-test.** Is the back-brief meaningfully different from "restate the plan"? If the agent's back-briefs are isomorphic to plan summaries, the protocol has collapsed to read-back and is not capturing back-brief's distinctive value.

## Mission command doctrine — bounded autonomy

### Description

Mission command is the doctrinal stance — not an artifact — that governs *how much* detail the commander specifies and *how much* they leave to the subordinate. It is the operating philosophy within which intent, orders, and back-briefs sit. Rooted in Prussian *Auftragstaktik* (Moltke the Elder's 1857–1888 reforms; formally in the 1888 German infantry field manual), and adopted by the US Army formally in ADP 6-0 (2012, current 2019).

The six ADP 6-0 principles:
1. **Build cohesive teams** through mutual trust.
2. **Create shared understanding.**
3. **Provide a clear commander's intent.**
4. **Exercise disciplined initiative.**
5. **Use mission orders** (not task orders).
6. **Accept prudent risk.**

The core tension: mission command is a response to the observation that **over-specification fails under uncertainty.** Moltke's formulation (paraphrased in modern doctrine): "No plan survives first contact; therefore the commander's task is not to specify the plan but to prepare subordinates to adapt it." Clausewitz's friction — small unpredictable perturbations that cumulatively derail the plan — is taken as a given, not a failure.

The counter-doctrine is **directive command / "befehlstaktik"** — specify everything, permit no deviation, punish deviation. Directive command works under two conditions: (a) the environment is predictable and (b) subordinates are incompetent. Mission command is the correct stance when neither holds.

### Dimension values

- **Timing of alignment:** continuous, at every decision
- **Decision locus:** **explicitly pre-authorized to subordinate, bounded by intent and end state**
- **Dialog form:** not an artifact — a stance that governs other artifacts
- **Question style:** doctrinal
- **Autonomy scope:** **bounded by intent** — this is the canonical instance of the "bounded autonomy grant" dimension value
- **Context richness:** rich at intent level, intentionally thin at execution level
- **Plan expression form:** meta — governs what goes in plan and what is left out
- **Knowledge direction:** symmetric in emphasis, hierarchical in final authority
- **Review/critique integration:** the back-brief is the paired critique move

### Mechanism

Three mechanisms:

1. **Intent-as-boundary.** Instead of specifying "what to do," specify "what must be true at the end" and "why it matters." Subordinate action inside those bounds is pre-authorized.
2. **Disciplined-initiative-as-duty.** Subordinates are *obligated* (not merely permitted) to deviate from orders when the plan fails, *if* deviation serves the intent. Failure to deviate is the error, not deviation itself.
3. **Prudent-risk acceptance.** The commander explicitly absorbs the risk of subordinate error within intent. This is necessary because otherwise subordinates optimize for "don't be blamed" rather than "achieve intent."

The load-bearing insight is that **the autonomy grant is not vague or total.** "Do what you think is best" is *not* mission command — that is abdication. Mission command is "here is the intent; act within it; I absorb the risk of good-faith errors within it; I retain authority over scope and end state."

### Predicted trade-offs for coding-planning

**Helps with:** agent-defers-trivial-decisions (the core pain point). Mission command's entire theoretical work is to make subordinates *stop asking* for permission on things they are already authorized to decide. Directly applicable.

**Helps with:** the latent autonomy-scope ambiguity problem. Without a mission-command-style stance, agent and user renegotiate autonomy per-decision, which is exhausting. A once-per-session autonomy grant bounded by intent removes most of the renegotiation.

**Costs:** requires the user to *actually* absorb the risk of agent errors-within-intent. If the user treats any agent error as a failure regardless of whether it was within intent, the agent will learn to defer again. Mission command requires principal restraint — this is not cheap for the user.

**Watch for:** the **total-autonomy failure.** "Do whatever you think is best" is abdication, not mission command. Mission-command-shaped instructions always have an end state and key tasks attached; "you decide everything" does not, and it collapses to undirected action.

**Watch for:** the **over-narrow-intent failure.** If the intent is so narrow that the agent has no room to decide, mission command has collapsed back to directive command.

### Adaptation notes — what transfers

**Transfers:**
- The stance itself: **over-specification fails under uncertainty; specify intent, grant bounded autonomy.**
- The **bounded-not-total** principle — autonomy requires stated bounds.
- The **risk-absorption duty** — the principal must actually tolerate good-faith errors within intent or the doctrine collapses.

**Artifacts of hierarchy (do not transfer):**
- The chain-of-command enforcement mechanisms.
- The disciplined-initiative-as-duty framing (agent does not have a duty in the moral sense; adapt to "pre-authorized to proceed without asking").
- The trust-building-over-time element — two-party coding sessions do not have the tenure-based trust-accumulation that mission command assumes.

### Evaluation plan

1. **Corpus audit.** Classify each observed human-agent decision in the corpus as (a) agent deferred and user decided, (b) agent decided unilaterally, (c) agent proposed and user chose. For each (a) case, check if the decision was within stated intent — if it was, that is a mission-command-doctrine-failure moment.
2. **Prospective.** Insert an explicit autonomy clause into session openers: "You are pre-authorized to decide X, Y, Z unilaterally within this intent; pause for W." Measure reduction in agent-defers events.
3. **Risk-absorption test.** Audit whether the user actually accepts agent errors within intent without escalation, or whether the user's revealed behavior re-imposes directive-command expectations.
4. **Reject-test.** Is the explicit autonomy clause meaningfully different from a dispatch that includes "you decide the details"? Compare sessions using each formulation — if there is no difference, the doctrine has not been operationalized.

## Warning order → operations order → fragmentary order (three-tier cadence)

### Description

A three-tier information-release progression, each tier serving a specific coordination purpose:

- **Warning order (WARNO):** Advance notice that an operation is coming. Conveys enough for subordinates to begin preparation (task organization, movement to staging, equipment readiness) without yet specifying the plan. **Does not authorize execution** unless specifically stated. Structurally shorter than an OPORD; follows a reduced version of the same five-paragraph structure.
- **Operations order (OPORD):** Full order. Complete five-paragraph format. Authorizes execution.
- **Fragmentary order (FRAGO):** A modification to a previously-issued OPORD. By design, short — only the *changed* paragraphs or sub-paragraphs are included. Unchanged elements are inherited from the parent OPORD.

Three purposes are served by the tiering:

1. **Parallel planning.** Subordinates begin their own planning (using their available-time-budget, ideally 2/3 of the total) as soon as the WARNO arrives — not when the OPORD arrives. Without WARNO, subordinates are idle during the commander's planning.
2. **Change management without restatement.** Operations in reality require frequent adjustment. FRAGO's existence as a named tier means "change to paragraph 3" is a first-class concept, not an ambiguous verbal aside.
3. **Graded commitment.** WARNO does not authorize execution; OPORD does. The progression separates "here is what is coming" from "go."

### Dimension values

- **Timing of alignment:** staged — WARNO at earliest possibility, OPORD before execution, FRAGO mid-execution
- **Decision locus:** principal issues; subordinates prepare on WARNO, execute on OPORD, adjust on FRAGO
- **Dialog form:** progressive structured-artifact cascade
- **Question style:** each tier's template
- **Autonomy scope:** bounded by current-tier content
- **Context richness:** graduated — WARNO thin, OPORD rich, FRAGO minimal-delta
- **Plan expression form:** structured-artifact, with inheritance (FRAGO inherits from OPORD)
- **Knowledge direction:** principal → subordinate, with feedback loops (back-brief, rehearsal)
- **Review/critique integration:** back-brief and rehearsal sit between OPORD and execution

### Mechanism

1. **Amortizing planning over time.** By releasing a WARNO early, the commander lets subordinates begin work while the commander is still finalizing. Total elapsed time to execution readiness shrinks.
2. **Explicit change surface.** FRAGO gives modifications a named form, which means changes do not accumulate silently and do not require re-reading the whole OPORD to find what changed. This is a diff protocol.
3. **Authorization gating.** The WARNO/OPORD split ensures that preparation does not become premature execution. Subordinates know they are in a prep phase vs. a go phase.

### Predicted trade-offs for coding-planning

**Helps with:** the **plan-is-still-forming** problem. Many coding sessions start with the user saying "I'm thinking about X" and the agent's first move is unclear — is it too early to start? A WARNO-analog ("heads up, we're about to plan this; gather the following context but do not make changes") lets the agent begin prep work without committing to an implementation.

**Helps with:** the **mid-session-pivot** problem. When the user changes their mind mid-session, it is usually communicated informally, and the agent may not correctly distinguish "clarification" from "change of direction." A FRAGO-analog ("amending paragraph N: ...") disambiguates.

**Costs:** overhead. For short sessions, three tiers is more ceremony than value. The military analog: small units operating in familiar terrain often skip WARNO entirely and go straight to verbal OPORD. The right answer is probably "tiered when beneficial, collapsed by default."

**Watch for:** FRAGO-overuse. If every session accumulates ten FRAGOs instead of one coherent OPORD, the plan has degenerated to incremental patches and loses coherence. The military warning is the same: commanders are trained to reissue the full OPORD when FRAGOs exceed some threshold.

### Adaptation notes

**Transfers:**
- The **WARNO concept as "prep authorization without execution authorization."**
- The **FRAGO concept as a first-class diff to a plan.**
- The **inheritance** property — FRAGO inherits unchanged material.

**Does not transfer:**
- The rigid tier structure as required for every session. Small sessions collapse it.
- The military-specific paragraph numbering.

**Coding-planning analog:**
- Warning-order-shaped session openers: "Heads up — I'm going to ask you to do X. First, gather: ..." (agent does preparation, no implementation yet).
- Fragmentary-order-shaped mid-session modifications: "Amend approach: skip step 3; step 4 now depends on the output of step 2."

### Evaluation plan

1. **Corpus observational.** Identify sessions where the user communicated a mid-session change — how was it communicated? Classify as (a) explicit modification, (b) implicit drift, (c) restated plan. Measure how often (b) produces execution inconsistency.
2. **Prospective.** Introduce an explicit "amend:" keyword for in-session changes; measure whether downstream consistency improves.
3. **Reject-test.** Is the three-tier structure actually distinguishable from "clarification dialogue"? If users cannot reliably distinguish WARNO-shape from dispatch, the tiering is not operationalized.

## METT-TC — mission-analysis context checklist

### Description

METT-TC is the US Army/USMC mission-analysis framework. The small-unit leader runs METT-TC analysis between receipt-of-mission and production of the OPORD. It is a checklist of context categories that, if not covered, will produce specific error classes.

- **M — Mission:** who, what, where, when, why.
- **E — Enemy:** who opposes; capabilities, likely course of action.
- **T — Terrain:** physical environment; doctrinal sub-checklist KOCOA-W (Key terrain, Observation, Cover/concealment, Obstacles, Avenues of approach, Weather).
- **T — Troops:** one's own capabilities, attachments, supporting assets. Sub-checklist HAS/A (Higher, Adjacent, Supporting, Attachments/detachments).
- **T — Time:** available time for planning, preparation, execution (the 1/3 – 2/3 rule; leader uses 1/3, subordinates get 2/3).
- **C — Civil considerations:** impact on, and constraints from, civilian population in the operational area (added post-2000s).

It is a discovery framework: the leader walks each slot and notes *what they do not know* that they would need to before committing to a plan.

### Dimension values

- **Timing of alignment:** pre-plan (between receipt-of-mission and plan production)
- **Decision locus:** subordinate's own (the leader doing mission analysis)
- **Dialog form:** structured checklist, typically self-administered with staff input
- **Question style:** explicit (each slot is a question)
- **Autonomy scope:** covers the scope of the subordinate's own decision
- **Context richness:** rich — exhaustive context coverage by design
- **Plan expression form:** input to plan, not plan itself
- **Knowledge direction:** internal — subordinate interrogates their own knowledge
- **Review/critique integration:** none intrinsic; paired with back-brief

### Mechanism

METT-TC works by **making ignorance visible**. Without a structured checklist, the planner does not know what they do not know. With it, each empty slot is a recognizable gap that either (a) prompts information-gathering, (b) is explicitly accepted as an unknown and planned around, or (c) is escalated to higher for clarification.

The key design choice: **the categories are mutually-exclusive-ish and collectively-exhaustive-ish for the military operational environment.** For a coding-planning analog, the question is: what are the coding-planning equivalents of enemy, terrain, troops, time, civil considerations?

### Coding-planning analog checklist

Candidate slots for a coding-planning pre-plan context sweep:

- **Mission (ask):** as above — the one-sentence outcome.
- **Opposing forces:** not adversary per se, but *sources of failure that push back against the plan*. Broken integration tests, known flaky dependencies, hostile parts of the codebase, time-boxed external APIs. The "what will fight this change."
- **Terrain (codebase):** the code and environment the change lives in. Architectural context, modular boundaries, patterns the codebase uses, conventions the change must fit.
- **Troops (capabilities / tooling):** what the user and agent can actually do — tools available, language and framework familiarity, CI/test coverage, ability to deploy.
- **Time:** user's time budget, agent's context-window budget, any deadline.
- **Users/stakeholders:** the "civil considerations" analog — who else the change affects. Downstream consumers of the API, teammates, future-self.

### Predicted trade-offs for coding-planning

**Helps with:** the **context-gathering-is-incomplete** failure. An agent that runs a METT-TC-shaped sweep before proposing will surface what it doesn't know, in named categories, rather than either plowing ahead with wrong priors or exhaustively grep-ing for everything.

**Helps with:** the user's **what-did-I-forget-to-mention** anxiety. If the checklist slots are stable, the user can review their own intent against the same slots and catch their own omissions.

**Costs:** checklist fatigue. Six slots is close to the upper bound for a checklist that is routinely run; much longer and it gets skipped.

**Watch for:** the checklist becoming a report, not an input. METT-TC is a mission-*analysis* input — it feeds planning, it is not the plan. In coding, the analog is a *context sweep* the agent does in a few seconds, not an artifact to present.

### Evaluation plan

1. **Corpus audit.** For each session, code which METT-TC-analog slots the agent actually investigated before proposing a plan. Correlate absent slots with downstream rework.
2. **Prospective.** Have the agent run a named METT-TC sweep as the first action after receiving intent. Compare first-plan-quality against baseline.
3. **Reject-test.** Is this meaningfully different from "the agent reads the relevant files first"? If the slot-structure does not add anything the agent would not already do, it is ceremonial.

## Rehearsal hierarchy — graded pre-commit validation

### Description

Military doctrine names a graded menu of rehearsal techniques, each with a different resource cost and fidelity:

- **Map rehearsal:** commander walks the plan on a 2D map. Cheapest.
- **Sketch / rock drill:** leaders walk through actions while moving markers across a sketch or sand table. Higher engagement than map; still desk-bound.
- **Sand table / terrain model:** a 3D model of the actual terrain; participants physically move pieces. Richer spatial understanding.
- **Reduced-force rehearsal:** key leaders only, walking through the plan on or near actual terrain. Catches coordination issues between units.
- **Full-dress rehearsal:** every soldier and system in the operation walks through the real plan on the real terrain. Most expensive; catches the most errors.
- **Combined-arms rehearsal:** above, but including all supporting arms (armor, engineers, fires). Catches cross-unit synchronization errors.

The commander selects based on (a) available time, (b) available resources, (c) perceived plan complexity, and (d) risk of the specific operation.

### Dimension values

- **Timing of alignment:** pre-action, post-plan, pre-execution
- **Decision locus:** none — the rehearsal is for identifying plan flaws, not making new decisions
- **Dialog form:** active simulation, all levels of detail
- **Question style:** "what happens next?" at each step
- **Autonomy scope:** none — rehearsal does not grant authority
- **Context richness:** matches rehearsal tier (map = thin; full-dress = thick)
- **Plan expression form:** executed (or simulated-executed) plan
- **Knowledge direction:** bidirectional — every participant may flag an issue
- **Review/critique integration:** **this is the plan-critique mechanism** — rehearsal surfaces plan flaws before execution makes them expensive

### Mechanism

Rehearsal works by **forcing plan steps to be concretely traced rather than abstractly understood.** A plan that is coherent on paper may fail at the step-to-step transitions; rehearsal exposes those transitions. The graded hierarchy exists because the ratio of errors-caught to resources-spent differs by plan class.

Key doctrinal principle: **the rehearsal cost should be proportional to the cost of failure in execution.** A routine operation gets a map rehearsal; a high-stakes operation gets a full-dress rehearsal. The question is not "should we rehearse" but "which tier."

### Coding-planning analog

Candidate rehearsal tiers for a code plan:

- **Map rehearsal analog:** reading the plan aloud — does it make sense, are the steps in sensible order?
- **Rock drill / sketch analog:** walking through each step and naming inputs, outputs, files touched. No code written.
- **Sand table / terrain model analog:** sketch the change at file-and-function level — what gets modified, what is new, what is deleted. No code written.
- **Reduced-force analog:** write pseudo-code or API stubs for the change. Minimal code, catches interface issues.
- **Full-dress analog:** write a prototype or spike. Real code, real tests, at full fidelity but throwaway.
- **Combined-arms analog:** full-dress plus running it against the actual integration environment (CI, other services, staging data).

### Predicted trade-offs

**Helps with:** the **plan-that-fails-at-transitions** failure. Plans often pass high-level review but fail at a concrete step where an assumption doesn't hold. Rehearsal forces step-by-step tracing.

**Helps with:** graded-commitment. Not every plan deserves a prototype. The hierarchy makes "how much pre-commit validation" a named decision, not a silent one.

**Costs:** rehearsal is extra work before implementation. Skipped when the user feels the plan is obvious. The military teaching is that commanders feel the plan is obvious precisely when they need rehearsal most — this is probably true in coding as well.

**Watch for:** rehearsal-becomes-implementation. If the "rehearsal" goes all the way to a working prototype, it *is* implementation, and the protocol has collapsed. The military distinction: rehearsal is throwaway; execution is not.

### Adaptation notes

**Transfers:**
- The **graded hierarchy** concept — match rehearsal cost to stakes.
- The **step-tracing** mechanism — the plan must be walked concretely.
- The **throwaway** property — rehearsal output is not execution output.

**Does not transfer:**
- The specific rehearsal types (literal sand tables).
- The multi-echelon coordination focus — most coding rehearsals are solo.

### Evaluation plan

1. **Corpus audit.** For each session where the plan produced rework, retrospectively ask: would a rehearsal at which tier have caught the rework-cause? Build a distribution.
2. **Prospective.** Require the agent to produce a step-by-step trace (sketch-level rehearsal) for non-trivial plans before implementation. Measure rework reduction.
3. **Reject-test.** Is rehearsal distinguishable from "agent thinks harder before implementing"? If the rehearsal step does not produce a visible artifact or surface named risks, it has collapsed.

## Troop leading procedures (TLP) — 8-step procedural framework

### Description

TLP is the doctrinal 8-step sequence that small-unit leaders follow from receipt of mission to execution. It sits alongside the larger-unit Military Decision-Making Process (MDMP) as its small-unit analog.

1. Receive the mission.
2. Issue a warning order.
3. Make a tentative plan.
4. Initiate movement.
5. Conduct reconnaissance.
6. Complete the plan.
7. Issue the order.
8. Supervise and refine.

Key doctrinal points:
- **The steps are not strictly sequential.** Steps 1–3 are typically in order; 3–7 are "as needed"; step 8 is continuous.
- **The 1/3 – 2/3 rule.** The leader takes no more than 1/3 of the available time for their own planning; subordinates get 2/3. Institutionalizes not-hoarding-time.
- **Step 2 precedes Step 3.** Issue the warning order *before* completing your own plan, so subordinates can plan in parallel.

### Dimension values

- **Timing of alignment:** structured across the full plan-execute cycle
- **Decision locus:** leader-owned process
- **Dialog form:** procedural sequence
- **Question style:** implicit — each step is an activity
- **Autonomy scope:** not about autonomy directly
- **Context richness:** varies by step
- **Plan expression form:** process, not artifact
- **Knowledge direction:** varies
- **Review/critique integration:** step 8 (supervise and refine) is continuous feedback

### Mechanism

TLP's work is **making the plan-produce-execute sequence a checklist so under cognitive load the leader doesn't drop a step.** It is a process scaffold. The 1/3-2/3 rule is its load-bearing innovation: it codifies that subordinates' time is scarcer than the leader's and that over-planning by the leader starves subordinate preparation.

### Predicted trade-offs

**Helps with:** the **planning-consumes-all-the-time** failure. Users can spend the entire session refining the plan and leave the agent no time to execute. An explicit time-budget allocation addresses this.

**Costs:** procedural overhead. Most coding sessions are not 8-step-shaped.

**Watch for:** process-worship. Following all 8 steps mechanically does not produce better plans; the steps are prompts, not a recipe.

### Adaptation notes

**Transfers:**
- The **1/3 – 2/3 time allocation** principle.
- The **issue-WARNO-before-plan-is-complete** principle (plan-in-parallel).
- The **supervise-and-refine-throughout** principle.

**Does not transfer:**
- The specific 8-step sequence as required.
- The military-specific steps (initiate movement, conduct reconnaissance in their literal form).

### Evaluation plan

1. **Corpus audit.** Measure how much session time is spent planning vs. executing. If planning regularly exceeds 1/3 of session time, the time-budget rule is candidate for introduction.
2. **Prospective.** Impose a time-budget split; measure session outcomes against free-form sessions.
3. **Reject-test.** Is explicit time-budgeting meaningfully different from "try to move faster"? If users do not respect the budget, the mechanism is not load-bearing.

## After-action review (AAR) — four questions

### Description

Already substantially covered in the incident-command domain; retained here for the military-specific framing. The US Army form is four questions:

1. What was supposed to happen?
2. What actually happened?
3. Why was there a difference?
4. What should we sustain or improve?

Key military-specific features not emphasized in incident-command sources:
- **Participation is required for all participants, not just leaders.** The lowest-ranking soldier is explicitly invited to speak.
- **Spirit of openness and learning — not problem fixing or blame.** Doctrinally named as such.
- **Categorized output:** findings are bucketed as "sustain" or "improve," not left as a narrative.

### Dimension values

(Same as incident-command AAR.)

### Predicted trade-offs for coding-planning

Covered in incident-command. The military-specific addition: **the sustain / improve split.** Coding-planning retrospectives often list only improvements (a negativity bias). Military doctrine forces attention to what worked well, on the theory that if you do not name what sustains, you lose it. For a solo developer + agent, the same bias probably holds — an AAR that only names failures teaches the agent to change patterns that were working.

### Evaluation plan

1. **Corpus signal:** do existing planning-session retrospectives surface sustains, or only improves? If the ratio is heavily skewed, an explicit sustain slot is warranted.
2. **Prospective:** templated AAR with mandatory sustain entry.

## Considered and rejected

Several candidate protocols from the military domain were considered and not included as standalone candidates:

### Military decision-making process (MDMP) — rejected as too large

MDMP is the staff-level (battalion and above) planning process. It is a 7-step formal procedure (receipt-of-mission, mission analysis, course-of-action development, COA analysis/wargaming, COA comparison, COA approval, orders production). It is the *full staff version* of what TLP is to small units. Rejected because:
- It requires a staff — multiple independent analysts producing competing courses of action. Two-party coding sessions have no equivalent.
- The individual moves inside MDMP (mission analysis, COA development, wargaming) are separately captured by METT-TC, the plan-producing step, and rehearsal respectively.
- Including MDMP as a distinct candidate would duplicate material already covered under TLP, METT-TC, and rehearsal.

### Command and control — rejected as too broad

"Command and control" is the superset doctrine that includes mission command, directive command, and everything in between. Too general to evaluate as a single candidate; its two poles (mission command vs. directive command) are relevant, and mission command is captured as its own candidate above.

### Intelligence preparation of the battlefield (IPB) — rejected as too domain-specific

IPB is a four-step analytical process for characterizing the operational environment before planning. Its four steps (define the environment, describe the environment's effects on operations, evaluate the threat, determine threat courses of action) overlap heavily with METT-TC's E and T slots. Rejected because the military-specific adversarial modeling does not transfer cleanly — coding has friction but not a modeled adversary — and the parts that do transfer are captured in METT-TC.

### Rules of engagement (ROE) — rejected as stakes-artifact

ROE are pre-authorized rules for when to use force (escalation rules, target identification rules, etc.). Conceptually related to the mission-command autonomy grant ("here is what you may do without asking") but specifically shaped by the life-and-death context and international law. The general pattern — pre-authorization with escalation triggers — is already captured by mission command's intent-bounded autonomy and by the I-PASS contingency-plan slot.

### Orders-group (O-group) staff meeting format — rejected as a convening pattern

The O-group is the convening format in which an OPORD is issued verbally to subordinate leaders. Its features (all key subordinates present, synchronized delivery, clarification questions permitted at named times) are organizational logistics rather than alignment-preservation mechanisms. For 2-party coding, the O-group collapses to "user and agent are both present and attending" which is assumed.

### Sync matrix — rejected as a scale-artifact

The synchronization matrix is a tabular representation of which unit is doing what in which phase. Required when coordinating dozens of units; not meaningful for 2-party work. For coding, the surviving trace is the "ordered task list with owners" which is captured by the I-PASS Action list and by the Execution paragraph of the coding-OPORD analog.

### Principles of war — rejected as too abstract

The nine (US) or ten (UK) principles of war (mass, objective, offensive, etc.) are strategic aphorisms, not protocols. Useful as cultural background; not convertible to a planning-protocol candidate.

### Operational design / operational art — rejected as too senior

Operational design is the theater-commander-level doctrine for framing campaigns. Its vocabulary (center of gravity, lines of operations, decisive points) is rich, but the practice is specific to campaign-scale coordination and does not shrink to session-scale coding planning without losing its point.

---

## Cross-protocol observations

Four patterns that emerge across the military candidates and may be more valuable than any single candidate in isolation:

1. **The intent / plan / order / modification / review cascade is a complete architecture.** Commander's intent → OPORD → back-brief → rehearsal → FRAGO → AAR is not six independent things but a single integrated alignment architecture. Picking and choosing may lose the interaction effects. This architecture as a whole is a candidate for the planning-protocols catalog.

2. **The load-bearing separation is always "what" vs. "how."** Across every military candidate, the same cut is load-bearing: the principal owns the *what* (intent, end state, mission), the subordinate owns the *how* (plan, scheme of maneuver, tactics). Violating this cut (principal specifies how, or subordinate decides what) is the named failure mode in every candidate. This cut should probably be a first-class dimension in the evaluation framework — not just "decision locus" but specifically "does the artifact respect the what/how cut."

3. **Pre-authorization is always bounded and always named.** Mission command does not grant "act as you see fit"; it grants "act within this intent." The autonomy grant is always paired with its boundary. An autonomy grant without a stated boundary is, in military doctrine, a failure mode named "abdication." This should be reflected in the "pre-authorized decision locus" dimension — pre-authorization is always a pair (grant, bound), never a single value.

4. **Comprehension-check and plan-quality-check are separate moves at separate times.** Medical read-back is comprehension-check only; back-brief separates them. For coding-planning, the candidate protocols should consider whether one move can do both (probably not) and whether both are needed (probably yes for non-trivial sessions).
