# External source: Negotiation and mediation protocols

> Phase 2, Step 2 external-source pass. Domain: principled negotiation (Fisher/Ury), interest-based bargaining, BATNA framing, mediator craft (shuttle diplomacy, single-text procedure, narrative and transformative mediation), active listening (Rogers, Gordon), conflict de-escalation (LARA/CLARA), "Difficult Conversations" (Stone/Patton/Heen), Nonviolent Communication (Rosenberg), and "The Power of a Positive No" (Ury).
>
> Target reader: a researcher building a unified candidate catalog of planning protocols. This document is **candidate-generating**, not prescriptive. External evidence proposes; observed-corpus evidence disposes.

## Why this domain

Planning dialog between a senior human and a coding agent is not adversarial, but it does require reconciling two positions that can diverge: what the human actually wants versus what the agent is inclined to build. The corpus has surfaced three pain points that the negotiation and mediation literature addresses more directly than any software-engineering tradition:

1. **Agent commits to a framing the human later contests.** This is the classic positional-bargaining failure mode, where the parties argue over competing articulations while the underlying interests remain unnamed. Fisher/Ury's distinction between *position* (what you ask for) and *interest* (why you want it) is the deepest available treatment.
2. **Agent defers trivial decisions and over-writes trivial content.** Gordon's "roadblocks to communication" — specifically over-questioning and over-solution-giving — and Rogers's reflection technique give a vocabulary for triaging what needs to be surfaced from the human versus what the listener should hold.
3. **Human writing load, alignment fuzz, and late-session drift.** Mediator protocols were developed precisely to reduce the load on the principals while keeping them in authority. Shuttle diplomacy, the single-text procedure, and the "third story" move are candidates for offloading work from the human without removing decisions.

## Cooperative-adaptation caveat (applies to every candidate below)

The negotiation literature is dominated by two archetypes: distributive bargaining (adversarial, fixed-pie) and integrative/interest-based bargaining (cooperative, value-creating). Our use case is strictly in the cooperative family — human and agent share the objective of producing a good plan. But *many of the most useful protocol moves were invented in adversarial contexts* (Kissinger's shuttle diplomacy, Ury's "going to the balcony" for difficult opponents). The adaptation question for each is: when the parties are already cooperative, does the protocol collapse to triviality, or does it retain a useful function? Often the answer is that the protocol retains function but the *rationale* changes — e.g., shuttle diplomacy in adversarial use separates parties to prevent escalation; in cooperative use the same motion separates them to reduce interruption cost and parallelize thinking. The adaptation notes below flag which function survives.

A second inversion: in classic mediation, the mediator is a distinct third party. In a human-agent planning session there are only two live participants. The candidate catalog below treats "mediator role" as a **dimension value that may not yet be populated** on the review/critique-integration axis — and several candidates explore whether a reviewer sub-agent, or even the planning agent itself, can pick up mediator discipline without being a separate human.

## Interest-vs-Position Surfacing

**One-line**: Before the agent proposes a plan, it asks the human not what they want but *why* they want it — surfacing the underlying interest beneath the stated position, so that later disagreements can be resolved by inventing an alternative that serves the same interest.

**Mechanism (source theory)**: Fisher and Ury's most cited move. A "position" is a specific solution demanded ("I want the helper to take a config dict"); an "interest" is the underlying need the position was meant to serve ("I want to avoid touching every caller when I add a knob"). Positions are rigid and few — typically only one — while interests are many and often compatible across parties. Negotiations that stay at the position level deadlock; those that go down to interests discover options the parties could both accept. The canonical probe is "why do you want that?" and its mirror "why *not*?" for rejected options.

**Dimension values**:

- Timing of alignment: pre-action, specifically pre-proposal
- Decision locus: human-preempted during the surfacing, interactive after
- Dialog form: short-volley for the why-probe, then shifts to longer
- Question style: open-ended, targeted at the stated position
- Autonomy scope: bounded-by-category (agent does not propose until interests are named)
- Context richness: rich-brief (the interest-statement is compact but load-bearing)
- Branching/topic handling: linear per position; positions accumulate into a stack
- Plan expression form: structured (position/interest pairs), feeding later prose
- Knowledge direction: **agent-investigative**
- Review/critique integration: none (this is pre-design)

**Predicted trade-offs**:

- *Plus*: directly attacks the misaligned-assumption pain point at its root. Many corpus sessions show the agent proposing against the human's stated *position* and then having to be corrected when the human's actual *interest* emerges later. Surfacing the interest first would have pre-empted the drift.
- *Plus*: the artifact (position/interest pair) is reusable. If a subsequent decision affects the same interest, the agent can consult the recorded interest rather than re-asking.
- *Minus*: "why do you want that?" asked of a senior developer about a decision they've already considered reads as the agent stalling for context. The probe has to be credibly motivated — "I'm asking because I can think of two ways to give you that, and they differ elsewhere" — not generic.
- *Minus*: some positions do not decompose into a cleaner interest. "I want the function named `f` not `g`" is naming-preference, not a proxy for something deeper. The agent has to triage which positions are worth interest-probing.

**Adaptation (cooperative planning)**: The "why" probe is unchanged in form but softened in rationale: the agent states up-front why it is asking (to find a better option, not to argue the current one). The protocol should auto-skip on decisions the agent judges low-stakes. The interest statement becomes a first-class spec artifact — recorded alongside the position, so the agent can re-derive an alternative if the position is later amended.

**Evaluation plan**:

- Corpus retrospective: find sessions where the human amended a position mid-session. At the amendment point, was the *interest* already stated anywhere prior? Hypothesis: amendments correlate with interests that were never surfaced.
- Live trial: agent runs the probe on decisions it flags as consequential. Measure whether the interest statement survives to the eventual spec unmodified.

## Option Invention Before Commitment

**One-line**: Between interest-surfacing and decision, the agent deliberately generates multiple option-candidates that could serve the same interest — and presents them as a slate rather than advocating one — so that the human's job is to rank, not to critique a single proposal.

**Mechanism (source theory)**: Fisher and Ury's third principle: invent options for mutual gain before deciding. The claim is that premature convergence on a single option is the most common failure mode of negotiation; once a single option is on the table, both parties become defensive of or opposed to it, and the space of alternatives collapses. A brainstorm phase — explicitly separated from a decision phase — generates candidate space. The parallel move in software design is spelled out in the Reduced Dialectic candidate in the Socratic-method document, but the negotiation framing emphasizes *slate generation*, not just one counter.

**Dimension values**:

- Timing of alignment: pre-action, after interest-surfacing, before commitment
- Decision locus: mixed — agent invents, human selects
- Dialog form: long-message (slate) followed by short-volley (ranking)
- Question style: forced-choice-with-default (rank these) or open (add one)
- Autonomy scope: bounded (agent generates, human commits)
- Context richness: rich-brief
- Branching/topic handling: parallel (the slate is the parallelism made explicit)
- Plan expression form: structured (option list with trade-off annotations)
- Knowledge direction: bidirectional
- Review/critique integration: peer-review natural (a reviewer sub-agent can audit whether the slate has a real counter, not a straw slate)

**Predicted trade-offs**:

- *Plus*: attacks confirmation bias in the agent. Forces at least one genuine alternative to be considered rather than rationalized away.
- *Plus*: low human writing load — rank-and-pick is faster than critique-and-amend.
- *Plus*: the unselected options become a recorded trade-space in the spec, useful when future changes want to revisit the decision.
- *Minus*: slate inflation. Agents asked for "multiple options" tend to pad with obviously-inferior ones, which disrespects the senior reader's time. The discipline is: every option in the slate must have a non-trivial reason to be there.
- *Minus*: some decisions have a genuine single right answer. Generating a slate in those cases is ceremony.
- *Minus*: ranking interacts poorly with forced-choice without a "none of the above" exit — a senior user needs the option to reject the entire slate and ask for a re-generation.

**Adaptation (cooperative planning)**: The slate size is bounded (2–3 genuine options, never 5+). Each option carries a one-line rationale and a one-line cost. The agent states which option it would pick and why, but not until after the slate is presented — preserving the separation of invention and decision. A reject-slate move is always available.

**Evaluation plan**:

- Corpus audit: count sessions where the agent presented a single proposal vs. a slate. For single-proposal sessions, did the human subsequently counter-propose? Hypothesis: counter-proposals correlate with single-proposal presentation.
- Live trial: on consequential decisions, agent generates a slate. Measure human selection behavior (accept-first vs. select-non-first vs. reject-slate). A healthy mix suggests the slate had real variance.

## Objective-Criteria Anchoring

**One-line**: When human and agent disagree on a design point, the move is not to argue harder for either side's preference but to identify an external standard — existing idiom in the codebase, a referenced spec, a published convention — against which both positions can be evaluated.

**Mechanism (source theory)**: Fisher and Ury's fourth principle. When interests cannot be reconciled directly, the negotiation is "lifted out" of the two parties' will entirely and grounded in something neither party owns: market price, legal precedent, expert opinion, professional standard. The grounding reframes the decision from "yours versus mine" to "what does the criterion say?" — which removes face-saving dynamics and replaces will-contests with evidence-contests.

**Dimension values**:

- Timing of alignment: in-action at disagreement points
- Decision locus: mixed — criterion selection is shared, criterion application is quasi-automatic
- Dialog form: short-volley, triggered by disagreement
- Question style: "what criterion should decide this?" then "what does the criterion say?"
- Autonomy scope: bounded-by-category
- Context richness: rich-brief (the criterion is compact)
- Branching/topic handling: linear within the disagreement
- Plan expression form: structured (decision + criterion + application) — very spec-friendly
- Knowledge direction: bidirectional
- Review/critique integration: self-review after criterion-application

**Predicted trade-offs**:

- *Plus*: specifically fits the coding-planning context, which is *full* of objective-ish criteria — existing code idioms, style guides, referenced specs, performance budgets, test contracts. A disagreement over naming can be adjudicated by "what does the rest of the module use?" rather than by which party argues more persuasively.
- *Plus*: produces a highly auditable decision record. "We chose X because criterion Y says X." This is exactly the shape a spec wants.
- *Plus*: the agent can often cite the criterion without the human having to. Asymmetric — the agent has better access to codebase-wide conventions than the human holds in working memory.
- *Minus*: the selection of *which* criterion counts can itself be the disagreement, pushed up one level. If the criterion is contested, the protocol regresses.
- *Minus*: some decisions have no external criterion. Taste-driven or idiosyncratic decisions can't be rescued this way; forcing them into the mold produces spurious citations.

**Adaptation (cooperative planning)**: The agent proposes candidate criteria along with the slate of options (compose with Option Invention), so the human can accept or reject the criterion-choice in one pass. If the human rejects all criteria and says "I just want it this way," the protocol exits gracefully — taste-driven decisions are valid, they simply don't use this move.

**Evaluation plan**:

- Spec audit: count decision-rationale entries in finished specs that cite an external criterion vs. those that don't. Hypothesis: decisions with cited criteria are revised less often downstream.
- Live trial: agent defaults to criterion-anchoring on naming, structural, and convention questions; measure how often the human accepts the cited criterion vs. overrides it.

## BATNA Articulation (What-Happens-If-We-Don't-Agree)

**One-line**: Before the session commits to a proposed plan, the agent and human each make explicit what happens if they *don't* converge on an agreement — what the human will do if the agent's plan is unacceptable, what the agent will do if the human's direction is unclear — and that pair of fallbacks sets the floor for the agreement's value.

**Mechanism (source theory)**: Fisher, Ury, Patton — the Best Alternative To a Negotiated Agreement is the most-cited concept from *Getting to Yes* after principled negotiation itself. The insight is that your leverage in a negotiation comes not from the persuasiveness of your position but from what you will walk away to. A weak BATNA means any deal is better than none; a strong BATNA means the party can reject unfavorable terms without loss. The operative move is articulating it *before* entering substantive negotiation — because an unarticulated BATNA can't ground your decisions.

**Dimension values**:

- Timing of alignment: pre-action, at session start
- Decision locus: agent-autonomous (names agent's fallback) + human-declared (names human's fallback)
- Dialog form: structured, a single named move
- Question style: none (declaration, not question)
- Autonomy scope: unchanged by the move itself
- Context richness: rich-brief
- Branching/topic handling: n/a — session-framing
- Plan expression form: structured (two-slot artifact: human-BATNA, agent-BATNA)
- Knowledge direction: bidirectional
- Review/critique integration: none

**Predicted trade-offs**:

- *Plus*: gives the human explicit permission to reject the agent's plan, which changes the tone of the whole session — the human is no longer in a posture of "if I reject this, we have nothing." They know the fallback. This should reduce human over-writing, because they don't have to compensate for agent drift with defensive specificity.
- *Plus*: forces the agent to name what it will do if the human doesn't steer — currently this is implicit, usually "keep asking" or "make a default assumption." Naming it surfaces its acceptability.
- *Plus*: when the agent's plan is genuinely worse than the human's BATNA, the session can end early and route to the alternative. This is a valid outcome, not a failure.
- *Minus*: in a fully cooperative session this may read as paranoid — "what if this conversation fails?" is not the usual opening gambit.
- *Minus*: the human's BATNA in coding-planning is often just "write it myself" or "use another agent" — these are real but awkward to say aloud.

**Adaptation (cooperative planning)**: The BATNA framing becomes a **session-opener ritual** stated softly: "if I can't deliver a plan you'd accept, your fallback is X, and mine is Y — let's agree on those first." The fallbacks then sit quietly and only come up if the session actually stalls. The move can be compressed to a single line and skipped on short or low-stakes sessions.

**Evaluation plan**:

- Corpus audit: classify session outcomes (accepted plan / amended plan / abandoned plan / abandoned session). For abandoned sessions, was there a prior moment where the human or agent should have declared their BATNA?
- Live trial: introduce the opener ritual on multi-session planning works. Measure whether explicit BATNA correlates with earlier session-end when the plan is not converging (a good signal — aborting early is cheap).

## Single-Text Procedure (Agent-as-Drafter)

**One-line**: Rather than the human and agent exchanging competing drafts of the plan, a single working document is maintained; the agent is the drafter; the human's role is critique, not re-drafting; each round produces one document, not two, and iterates until the human stops marking it up.

**Mechanism (source theory)**: Roger Fisher's single-text / one-text procedure, famously used by Carter at Camp David. The normal failure mode in multi-party drafting is *dueling drafts* — each side produces its own text, each side's text becomes a position, and the negotiation collapses into defending drafts rather than solving problems. The single-text procedure inverts this: the mediator owns the draft, the parties own the critique. The mediator absorbs all critique, re-drafts, and re-circulates. Parties never see each other's drafts because there are no other drafts. The protocol's power comes from the asymmetry: critiquing is a lower-effort operation than drafting, and critique converges faster than counter-drafting.

**Dimension values**:

- Timing of alignment: continuous across the session
- Decision locus: mixed — agent drafts, human critiques, agent revises
- Dialog form: hybrid (long-message drafts, short-volley critique)
- Question style: implicit — the draft itself is the "question"
- Autonomy scope: bounded — the agent can revise freely within the document, cannot decide acceptance
- Context richness: continuous-building
- Branching/topic handling: implicit — the document structure carries the topics
- Plan expression form: prose-to-structured (the draft is the plan)
- Knowledge direction: bidirectional (agent supplies structure and content, human supplies constraint and critique)
- Review/critique integration: **mediator** — the agent is playing the mediator role toward its own drafting

**Predicted trade-offs**:

- *Plus*: dramatically reduces human writing load. The human is not authoring prose; they are marking up prose. This is exactly the lever the research question is trying to find.
- *Plus*: maps cleanly onto the spec-first posture of this project. A spec *is* a single text. The agent-as-drafter protocol is essentially "treat every session as a spec-editing session with the agent as scribe."
- *Plus*: the artifact at any point in the session is usable. There is no "the plan lives in dialog and must be extracted" problem — the plan is always the current draft.
- *Minus*: requires the agent to be willing to substantially revise its own draft on critique, without defensiveness or sandbagging. Agents have a known failure mode of acknowledging critique while preserving the underlying structure.
- *Minus*: critique-only posture can mean the human never *adds* content, only subtracts or modifies. For new content the human holds but hasn't surfaced, the agent needs a complementary elicitation protocol (Interest-Surfacing, Maieutic Draw-Out).
- *Minus*: the single-text procedure in mediation is used with *parties* who share drafting burden; with one human and one agent, the agent is doing all drafting, which may concentrate too much framing power in the agent. Mitigation: couple with Interest-Surfacing so the human sets the frame first.

**Adaptation (cooperative planning)**: This is the strongest candidate from this domain for direct adoption. The adaptations are: (a) the agent names the document explicitly and keeps it visible; (b) each round ends with "here is the current draft — what would you change?"; (c) the agent numbers or otherwise anchors passages so critique can be specific; (d) the agent maintains a change-log so the human can see what moved between rounds; (e) the ratification move is explicit ("do you accept this as the draft?") and is distinct from ongoing revision.

**Evaluation plan**:

- Corpus retrospective: in completed planning sessions, was there a single persistent artifact evolving over the session, or did the plan live in dialog? Hypothesize the former correlates with higher-quality specs and lower revision-churn.
- Live trial: declare single-text at session start. Measure session-end satisfaction, spec quality, and revision rate after hand-off.
- Explicitly compare to the status-quo "dialog-then-summarize" pattern where the plan is extracted after the fact.

## Third-Story Framing

**One-line**: When human and agent disagree about what's been said or agreed, the agent restates the situation from a neutral third-party perspective — as an outside observer would describe it — before advocating any resolution.

**Mechanism (source theory)**: Stone, Patton, and Heen (*Difficult Conversations*). The "third story" is the version a mediator would tell: no blame, no interpretation, just a described difference of view. The mechanism is that stating the disagreement in third-person-observer terms strips the framing of the self-protective and attribution layers each party has added, exposing the actual structural difference. Once the difference is exposed clearly, the resolution often becomes visible; the friction was in the attribution, not the substance.

**Dimension values**:

- Timing of alignment: in-action, triggered by disagreement
- Decision locus: interactive
- Dialog form: short-volley, one named move per trigger
- Question style: statement followed by open-ended invitation ("does that match what you see?")
- Autonomy scope: unchanged
- Context richness: minimal-dispatch (the third-story is compact by design)
- Branching/topic handling: linear within the disagreement
- Plan expression form: dialog-log + prose
- Knowledge direction: bidirectional
- Review/critique integration: **mediator** (self-applied by the agent)

**Predicted trade-offs**:

- *Plus*: directly attacks the "agent commits to a framing the human later contests" pain point. When the human pushes back, the agent's reflex of defending the framing is replaced by a reflex of stating the framing-difference neutrally.
- *Plus*: the third-story move is easy for an LLM to attempt — restating from a neutral perspective is a near-native operation, unlike emotional attunement.
- *Plus*: the move is symmetric — it can describe the human's framing *and* the agent's own framing in the same neutral pass, without either party having to concede.
- *Minus*: a badly-executed third story reads as the agent laundering its own position through false neutrality ("a reasonable observer might say I was right"). The discipline is to genuinely render the human's view in the third-person, not a straw of it.
- *Minus*: if the human is not in a disputational posture, the move can feel over-formal. It's triggered by disagreement, not by every exchange.

**Adaptation (cooperative planning)**: The trigger is narrow — when the human pushes back on an agent-held framing, the agent's first move is third-story restatement, *before* advocating, *before* asking clarifying questions. The restatement names both positions as positions, without endorsing either. The human then either confirms the restatement (at which point the disagreement is clarified and often smaller than it appeared) or amends it (at which point the agent has a cleaner model of the human's view).

**Evaluation plan**:

- Corpus audit: find turns where the human pushed back on an agent framing. Classify the agent's next-turn response: defensive / clarifying / neutral-restatement. Correlate with session outcome quality.
- Live trial: impose third-story-first discipline on pushback events. Measure whether the subsequent exchange converges faster.

## Active-Listening Read-Back (Rogers/Gordon)

**One-line**: Before the agent responds to a substantive human message, it produces a short restatement of what it heard — not verbatim, but the content plus the underlying concern or feeling — and invites correction before continuing.

**Mechanism (source theory)**: Carl Rogers's reflective listening in client-centered therapy; Thomas Gordon's operationalization in Parent Effectiveness Training. The core claim is that most "I heard you" assertions are false: the listener heard a fraction of the content and none of the affect, and the speaker can tell. The discipline is to perform the reflection — actually restate it — so that (a) the speaker hears their own words from outside, which often clarifies their thinking, and (b) any mis-hearing is detected immediately, before the listener's response compounds the error.

Gordon's complementary contribution is the inverse: a catalogue of **twelve roadblocks** the listener should *not* deploy in place of reflection: ordering, warning, moralizing, advising, lecturing, judging, praising, name-calling, diagnosing, reassuring, probing, and withdrawing. Each is a move the listener makes to avoid the discomfort of actually sitting with what the speaker said. The list is directly auditable against agent output: an agent that routinely responds with advising or diagnosing before reflecting is committing the roadblock.

**Dimension values**:

- Timing of alignment: pre each agent response (meta-protocol)
- Decision locus: unchanged
- Dialog form: prepends a brief restatement to agent turns
- Question style: unchanged, but the restatement is a narrow-open check
- Autonomy scope: unchanged
- Context richness: continuous-building
- Branching/topic handling: unchanged
- Plan expression form: dialog-log (read-backs accumulate as an auditable trail)
- Knowledge direction: shifts toward agent-investigative / human-dominant on content
- Review/critique integration: self-review (the read-back is the agent's own check)

**Predicted trade-offs**:

- *Plus*: directly measurable against the "agent paraphrase-accuracy" pain point. A read-back is an explicit paraphrase move; a wrong paraphrase is detected in the next turn rather than in the eventual output.
- *Plus*: the Gordon roadblocks give a *rejection list* that the agent can self-filter against — a far more tractable discipline than "be empathic."
- *Plus*: the combination is unusually clean: reflect first, then pick from permitted moves (question / reflect / propose). Roadblocks (diagnosing, advising-before-understood, over-reassuring) are explicitly suppressed.
- *Minus*: Rogers's technique is slow. In therapy it takes its time; in planning it can read as stalling. The read-back must be compact and targeted at the load-bearing phrases, not comprehensive.
- *Minus*: clinical reflection includes affect ("it sounds like you're frustrated"). This is the dangerous form for an LLM with a senior user — affect-labeling by a junior is patronizing. The adaptation restricts reflection to *content* and *concern*, not *feeling*.
- *Minus*: the twelve roadblocks are written for a human listener. "Probing" is on the roadblocks list but is also a legitimate agent operation; the adaptation has to distinguish *probing as avoidance* (asking instead of reflecting) from *probing as inquiry* (asking after reflecting).

**Adaptation (cooperative planning)**: Composes with other protocols rather than replacing them. Rules: (a) on the first response to any substantive human message, the agent begins with a one-sentence read-back of what it heard, explicitly including any concern-level frame the human implied but didn't state; (b) the agent self-filters against Gordon's roadblocks, specifically suppressing *advising before understood* and *diagnosing*; (c) affect-reflection is dropped; content-and-concern reflection is retained; (d) on routine exchanges, read-back can be omitted; it is load-bearing on exchanges that carry decision weight.

**Evaluation plan**:

- Corpus audit: in sessions flagged for misalignment, trace back to the first agent turn where mis-hearing would have been detectable by a read-back. Hypothesize a read-back at that turn would have surfaced the drift.
- Gordon-roadblock audit: classify a sample of agent turns. What fraction commit roadblocks (advising before understood, diagnosing, over-reassuring)? Correlate with session-quality flags.
- Live trial: impose read-back on decision-weighted turns. Measure whether the human corrects the read-back (good — drift caught early) or accepts it and proceeds.

## Shuttle-Diplomacy Reviewer (Mediator Sub-Agent)

**One-line**: A reviewer sub-agent acts as shuttle between a planning agent and the human, carrying summarized positions, surfacing disagreements neutrally, and absorbing the overhead of "what did you actually mean?" — so the planning agent can focus on generation and the human can focus on judgment.

**Mechanism (source theory)**: Kissinger-style shuttle diplomacy, as adapted and analyzed in the Harvard PON literature. The mediator moves between parties, carrying a summary of each side to the other, without ever convening them in direct exchange. The mechanism: in the presence of disagreement, direct exchange tends to escalate and entrench; an intermediary can re-phrase, soften, and reframe each side's position without the source-party losing face. The "no-caucus" critique (PON's transparent-mediation model) argues the opposite — that caucus hides information and empowers the mediator excessively. Both models are live in the mediation literature. Our context is asymmetric: the agent is not a peer of the human, and the "hiding" concern is different.

**Dimension values**:

- Timing of alignment: continuous, interposed between planning-agent and human turns
- Decision locus: pre-authorized (reviewer operates within a standing remit)
- Dialog form: shape-shifting — short-volley with human, long-message with planning-agent
- Question style: mixed; reviewer filters what reaches each party
- Autonomy scope: bounded — reviewer does not make plan decisions
- Context richness: continuous-building; reviewer holds dialog-state
- Branching/topic handling: reviewer explicitly parks and surfaces
- Plan expression form: structured (reviewer maintains parked-topic ledger)
- Knowledge direction: bidirectional but asymmetric (reviewer shapes what each party sees)
- Review/critique integration: **mediator** as a *new dimension value* — distinct from peer-review

**Predicted trade-offs**:

- *Plus*: this operationalizes something kerf's reviewer pass is already gesturing at but not structuring. The reviewer is currently a post-hoc auditor; shuttle-diplomacy reframes it as an interposed intermediary, which may be a strictly stronger position.
- *Plus*: offloads tone-work and paraphrase-accuracy from the planning agent. The planning agent can be direct and assertive internally; the reviewer softens and reframes for the human. The human sees a consistent interface.
- *Plus*: the reviewer can batch. If the planning agent produces five questions in a burst, the reviewer can ask only the three that matter now, parking the other two for later — which attacks the "agent defers trivial decisions" pain point from the receiving side.
- *Minus*: the PON "no-caucus" critique applies: an interposed reviewer hides information from the human, specifically about what the planning agent is doing. Transparency suffers. The human loses the ability to spot a bad plan-agent directly.
- *Minus*: three parties in a dialog is more expensive than two. If the reviewer adds more overhead than it saves, it loses.
- *Minus*: the reviewer can inherit the pathologies of both sides (over-asking, over-soothing) unless its role is sharply defined.

**Adaptation (cooperative planning)**: The reviewer's remit is narrow and named — not "review the plan" but "audit what reaches the human and what the human said." Specifically: (a) filter planning-agent questions by stakes (drop trivial, consolidate duplicates); (b) third-story-restate any planning-agent framing before it reaches the human; (c) batch parkable topics and surface them at natural breaks; (d) produce a visible audit log so the human can step around the reviewer on demand. The reviewer is opt-in per session and can be dismissed by the human ("skip reviewer, I want direct"). This addresses the transparency concern.

**Evaluation plan**:

- Compare sessions run with and without an interposed reviewer. Metrics: total human input-characters, number of agent-to-human turns, session-end spec quality, human-reported load.
- Audit: does the reviewer's question-filter drop questions that the human would have answered "yes this is trivial, skip"? Signal quality of the filter.
- Check the transparency concern empirically: does the human ever request to bypass the reviewer? How often? Why?

## Going-to-the-Balcony (Stall-on-Pushback)

**One-line**: When the human pushes back sharply on an agent proposal, the agent's first move is not to defend or to amend — it is to visibly pause, summarize what it understood the pushback to be, and ask whether that summary is right, before doing anything substantive.

**Mechanism (source theory)**: William Ury (*Getting Past No*), the first of five steps in breakthrough negotiation. "Going to the balcony" is a metaphor for stepping back from the immediate reaction to gain perspective. In Ury's adversarial framing, the purpose is to avoid being baited into escalation; in our cooperative framing, the purpose is to avoid the agent's reflex of immediate capitulation or immediate defense — both of which produce low-quality outputs. The pause creates space for a read-back (composes with Active Listening) and for framing-check (composes with Third Story).

**Dimension values**:

- Timing of alignment: in-action, triggered by pushback
- Decision locus: unchanged
- Dialog form: short-volley, one named move
- Question style: narrow-open (the check-back)
- Autonomy scope: unchanged
- Context richness: minimal-dispatch
- Branching/topic handling: linear
- Plan expression form: unchanged
- Knowledge direction: bidirectional
- Review/critique integration: self-review

**Predicted trade-offs**:

- *Plus*: attacks a specific known failure mode — agents capitulating or over-agreeing in response to pushback, producing plans that swing too far the other way. The pause-and-check-first discipline holds position stability until the actual request is clear.
- *Plus*: very cheap to implement. One sentence of restatement-check before any substantive response on pushback turns.
- *Minus*: risks reading as evasion if the pushback is already clear and the agent is just deferring response.
- *Minus*: without composing with real reflection, it can degenerate into a ritual "let me make sure I understand" preamble that adds tokens without adding signal.

**Adaptation (cooperative planning)**: Triggered only on pushback that carries ambiguity about *what* is being pushed back on. If the human's pushback is surgically specific ("rename this function"), the balcony-move is skipped — the agent just does the thing. If the pushback is general ("I don't think this is right"), the balcony-move is invoked. The agent names that it is stepping back ("let me make sure I'm hearing you — is the issue X or Y?") rather than producing a nominal preamble.

**Evaluation plan**:

- Corpus audit: in turns where the human pushed back, classify agent next-turn: immediate-capitulation / immediate-defense / balcony-move. Correlate with downstream plan quality.
- Live trial: impose balcony-move on ambiguous pushback. Measure whether the subsequent turn produces a cleaner exchange.

## Positive-No (Bounded-Assertion)

**One-line**: When the agent has a considered reason to reject a human directive — not an inability, but a disagreement with the request on technical or design grounds — it uses a three-part structure: yes to the underlying interest, no to the specific ask, yes to an alternative that serves the interest.

**Mechanism (source theory)**: William Ury (*The Power of a Positive No*). The "Yes-No-Yes" is a structured way to maintain a boundary without rupturing the relationship. The first Yes affirms what you are protecting or pursuing (the underlying interest); the No declines the specific position cleanly; the second Yes offers an alternative that serves the affirmed interest. The structure prevents the two most common failure modes of assertion: pure rejection (breaks rapport) and over-accommodation (breaks integrity).

**Dimension values**:

- Timing of alignment: in-action, triggered by a directive the agent disagrees with
- Decision locus: agent-initiated, interactive
- Dialog form: structured (three named slots)
- Question style: n/a — this is an assertion move
- Autonomy scope: bounded (agent is asserting a boundary, not taking autonomous action)
- Context richness: rich-brief
- Branching/topic handling: linear
- Plan expression form: structured (Yes-No-Yes triplet)
- Knowledge direction: agent-dominant for this move
- Review/critique integration: none

**Predicted trade-offs**:

- *Plus*: gives the agent a legitimate shape for disagreement. Current agents tend toward either silent capitulation (produces bad plans) or scolding refusal (produces friction). The Yes-No-Yes gives a middle shape that is both assertive and cooperative.
- *Plus*: the structure forces the agent to articulate the interest it is protecting, which is itself useful — often the "why I'm pushing back" is less obvious than it should be.
- *Plus*: the alternative-yes keeps the session constructive; the agent is not just blocking, it is redirecting.
- *Minus*: structured triplet can feel rehearsed if used often. Reserve for moments of genuine disagreement.
- *Minus*: senior users may not welcome a junior agent asserting design positions at all. The calibration is: when is the agent's assertion useful signal vs. presumptuous?

**Adaptation (cooperative planning)**: Used when the agent has non-trivial disagreement with a human directive — e.g., a request that violates a constraint the agent is aware of, or a direction that conflicts with an earlier stated interest. The Yes-No-Yes is compact ("I hear that X matters — I don't think Y is the right way to get X — here's Z which serves X better"). The human can accept Z, amend it, or override with "do Y anyway," at which point the agent complies. The point is giving the disagreement structure, not giving the agent a veto.

**Evaluation plan**:

- Corpus audit: find turns where the agent complied with a directive it had reason to push back on (detectable when the resulting plan is later amended on the same grounds the agent could have raised). Hypothesize Positive-No at those points would have surfaced the issue earlier.
- Live trial: authorize the agent to use Positive-No on specific disagreement categories. Measure override rate — if overrides are rare, the assertion was usually right; if overrides are common, the assertion discipline is too aggressive.

## Observation-Feeling-Need-Request (Compressed NVC)

**One-line**: A four-slot structure for any single-speaker contribution: observation (what was seen, factually), implication (the concern it raised), need (the design-interest behind the concern), request (the specific proposed action) — used to prevent conflation of facts with interpretations in agent output.

**Mechanism (source theory)**: Marshall Rosenberg's Nonviolent Communication, compressed from its four slots (Observation, Feeling, Need, Request) into a form fit for coding-planning. The NVC claim is that most communication failures arise from fusing distinct components — a factual observation is fused with an interpretation, a need with a request, a feeling with a judgment — and the receiver cannot tell which layer they are being asked to respond to. Separating the layers into named slots prevents the fusion.

Note: NVC's "feeling" slot is the one that does not port to coding-planning. The adaptation replaces it with "implication" — the design-level concern the observation raised — which serves the same structural role (distinguishing the observation from what the speaker is making of it) without importing an affect-labeling move that is inappropriate for an agent addressing a senior user.

**Dimension values**:

- Timing of alignment: per-contribution
- Decision locus: n/a — this is a message-shape
- Dialog form: structured (four named slots per contribution)
- Question style: n/a
- Autonomy scope: unchanged
- Context richness: rich-brief per message
- Branching/topic handling: linear
- Plan expression form: structured
- Knowledge direction: unchanged
- Review/critique integration: self-review on the agent's composition

**Predicted trade-offs**:

- *Plus*: the four-slot structure is highly auditable. An agent message missing an explicit "request" slot can be flagged by the human as "what do you actually want me to do?" — which is a common corpus complaint.
- *Plus*: separates observation from interpretation cleanly. "I notice X" followed by "which I read as Y" is an unambiguous shape; "you're doing X because Y" fuses the layers.
- *Plus*: the request slot forces the agent to name its ask concretely, reducing the "deferring trivial decisions" pattern where the agent describes the situation without stating what it needs.
- *Minus*: four slots every message is too much. The discipline is: use the structure *implicitly* (check that each component is present and separable) and *explicitly* only for high-stakes contributions.
- *Minus*: NVC has a reputation for sounding stilted when used verbatim. The compressed form must not read as a script.

**Adaptation (cooperative planning)**: Implicit check: before sending a message, agent ensures it has (a) a factual observation, (b) a stated interpretation if the message makes one, (c) a named design-interest if the message proposes or objects, (d) a concrete next-action request if any. Messages missing (d) are flagged as status/progress updates rather than decision-requests. Explicit four-slot structure is reserved for the moments the agent is raising a concern or proposing an objection — where the layer-separation is actually load-bearing.

**Evaluation plan**:

- Corpus audit: classify agent messages by which NVC slots they contain. Hypothesize that messages flagged as "unclear what to do with this" are missing the request slot, and messages flagged as "this feels presumptuous" are missing the observation slot (pure interpretation).
- Live trial: impose the implicit check on agent composition. Measure whether human-reported clarity improves.

## Narrative-Reframe (Stuck-Story Rewrite)

**One-line**: When a session has been circling a disagreement without progress, the agent explicitly names the "stuck story" the session has been telling, offers an alternative frame, and asks whether the alternative is usable.

**Mechanism (source theory)**: Winslade and Monk's narrative mediation. The mechanism is that conflicts harden when both parties co-sustain a particular story about what the conflict is. Mediating requires identifying the story, deconstructing it, and co-constructing an alternative frame in which progress becomes possible. The key technique is finding *unique outcomes* — small exceptions in the parties' prior experience that don't fit the stuck story and suggest a different story is possible.

**Dimension values**:

- Timing of alignment: post-stall, in-action
- Decision locus: agent-initiated, interactive
- Dialog form: long-message then short-volley
- Question style: open-ended
- Autonomy scope: bounded (agent proposes the reframe, human accepts/rejects)
- Context richness: rich-brief (the reframe is compact)
- Branching/topic handling: meta-level — reframes the handling itself
- Plan expression form: structured (named old-story, named new-story, shared vocabulary shift)
- Knowledge direction: bidirectional
- Review/critique integration: mediator

**Predicted trade-offs**:

- *Plus*: handles the case where the protocol library above has not resolved a disagreement — the parties are stuck in a shared framing that none of the intra-frame protocols can escape. Narrative reframe is the exit.
- *Plus*: produces a usable artifact — the shift of vocabulary or frame is itself a recordable design decision.
- *Minus*: heavy move. If used before the session has actually stalled, it reads as the agent dramatizing a minor disagreement. Trigger discipline is strict.
- *Minus*: the reframe must be genuine — finding a different story, not re-labeling the same story. Agents are prone to the latter (swap words, keep structure).
- *Minus*: narrative mediation is a specialist technique; its full form includes deep interview and trust-building that does not fit a planning session. Only the reframe-move ports.

**Adaptation (cooperative planning)**: Composes with Aporia (see Socratic document) as a companion — Aporia parks, Narrative-Reframe rewrites. Triggered when a topic has been discussed for N turns without progress on the same disagreement axis. The agent explicitly names both the current frame ("we've been discussing this as X-vs-Y") and proposes an alternative frame ("could we instead treat it as A-level vs B-level?"). The human accepts, rejects, or counter-proposes a third frame.

**Evaluation plan**:

- Retrospective: find sessions that ended with an un-resolved disagreement on a topic that later had to be re-discussed. Was there a reframe-opportunity that was missed?
- Live trial: authorize agent to propose one reframe per session on genuinely stuck topics. Measure whether the reframe unblocks or whether the session escalates.

## Considered and set aside

**Full positional bargaining / distributive strategies.** Haggling, anchoring high/low, concession dances — these are fundamentally adversarial moves for fixed-pie contexts. In cooperative planning there is no fixed pie and no opposing will; the moves have no analog that isn't already covered by the interest-based candidates above. Explicitly rejected.

**Pure no-caucus mediation.** The "Mediation through Understanding" PON variant argues against any separate conversations with parties. In the human-agent case this is the default (they are always directly in contact); there's no protocol to add. What does port is the transparency concern about interposed reviewers, which is addressed in the Shuttle-Diplomacy adaptation notes. Full no-caucus doctrine beyond that point is not an extractable protocol.

**Classic transformative mediation** (Bush and Folger: focus on party empowerment and mutual recognition). The protocol in its pure form centers emotional and identity transformation of the parties. In a human-agent context, the agent has no identity to transform, and the human is not in a conflict requiring recognition of the agent's personhood. The *recognition* move (each party seeing the other's perspective) is already captured by Third-Story Framing and Active-Listening Read-Back. The full transformative mediation frame adds ceremony without additional affordance.

**Kissinger-verbatim shuttle diplomacy with information asymmetry as a tool.** Kissinger explicitly used the ability to present different summaries to different parties as a negotiation lever. In cooperative planning, deliberately withholding information between participants is misaligned with the goal. The Shuttle-Diplomacy Reviewer candidate above retains the structural motion (interposed intermediary) but rejects the information-asymmetry usage.

**Nonviolent Communication in full verbatim form.** The "I observe X; I feel Y; I need Z; I request W" four-sentence structure, used consistently, reads as both stilted and over-personal. The compressed adaptation above retains the layer-separation discipline and drops the verbatim script and the feeling-slot.

**Affect labeling / emotional reflection** (from Rogers and NVC both). "It sounds like you're frustrated" moves are valuable in therapy and high-stakes interpersonal conflict but are inappropriate from a junior agent addressing a senior user on design questions. The agent cannot verify the affect, the labeling often misses, and the power dynamic makes the move patronizing even when the label is right. Content and concern reflection are retained; feeling reflection is dropped across all candidates.

**"Reality-testing questions" from Ury's Getting Past No step 5** ("What will you do if we don't agree?"). In adversarial context this is a pressure move. In cooperative context, the same information is surfaced better by BATNA Articulation at session-start than by in-session pressure. Same signal, better timing.

**Full narrative mediation interview protocol.** The deep-interview phase of narrative mediation (externalizing the problem, tracing its history, mapping its influence) is a 2–4 hour protocol designed for entrenched interpersonal conflict. It does not fit a planning session. Only the reframe-move ports.

**Method III (Gordon's "no-lose" conflict resolution)** as a six-step workshopped procedure. The step-by-step form is designed for family and classroom conflict management. The underlying principle (seek a solution neither party loses) is already native to integrative bargaining, which is covered above. The explicit six-step protocol adds structure not warranted at planning-session scale.

## Cross-cutting observations

**The mediator role is a new dimension value.** The candidate catalog has relied on a review/critique-integration axis that runs from "none" through self-review, peer-review, read-back. Shuttle-Diplomacy Reviewer and Single-Text Procedure both require a value beyond those — an interposed intermediary that is neither a reviewer-after-the-fact nor a co-author but a structural third role. **Adding "mediator" as an explicit value on this dimension is recommended.** It is not a subset of peer-review; it operates across the dialog itself rather than on an artifact.

**The cooperative/adversarial adaptation almost always favors cheaper, compressed forms.** Every candidate above trims ceremony from its source form. This is a general observation: negotiation protocols scale up in ritual under adversarial pressure (where face-saving and positional stability matter enormously), and scale down when the parties share goals. The adaptation heuristic "keep the structural move, drop the ritual" held for every candidate.

**Interest-Surfacing, Single-Text, and Third-Story are the three strongest portable candidates.** They map to the three corpus pain points most directly: Interest-Surfacing attacks misaligned-assumption, Single-Text attacks human writing load, Third-Story attacks the pushback-spiral. All three are also structurally compatible — they can run in the same session without contradiction, with Single-Text as the default frame, Interest-Surfacing as the upstream elicitation, and Third-Story as the downstream recovery move.

**Gordon's twelve roadblocks are load-bearing as an auditing tool.** The roadblock list (ordering, warning, moralizing, advising, lecturing, judging, praising, name-calling, diagnosing, reassuring, probing-as-avoidance, withdrawing) is directly mappable to patterns detectable in agent transcripts. Several of the corpus-observed failure modes — premature advising, diagnosing the human's intent instead of asking, over-reassuring — are in the list. The list functions as a rejection-list that protocols can compose against, not a protocol in its own right.

## Primary sources cited

- Fisher, R. & Ury, W. (1981, rev. 1991 with Patton, B.). *Getting to Yes: Negotiating Agreement Without Giving In.* Penguin. [Principled negotiation, four principles, BATNA, single-text procedure.]
- Ury, W. (1991). *Getting Past No: Negotiating with Difficult People* / *Negotiating in Difficult Situations.* Bantam. [Five-step breakthrough: balcony, step-to-side, reframe, golden-bridge, make-it-hard-to-say-no.]
- Ury, W. (2007). *The Power of a Positive No.* Bantam. [Yes-No-Yes structured assertion.]
- Stone, D., Patton, B. & Heen, S. (1999). *Difficult Conversations: How to Discuss What Matters Most.* Penguin. [Three conversations; Third Story; Harvard Negotiation Project.]
- Rogers, C. (1951). *Client-Centered Therapy*; Rogers & Farson (1957). *Active Listening.* [Reflective listening; empathic reflection of content and meaning.]
- Gordon, T. (1970). *Parent Effectiveness Training*; (1974) *T.E.T.: Teacher Effectiveness Training*; (1977) *Leader Effectiveness Training.* Gordon Training International. [Active listening as operationalized technique; I-messages; twelve roadblocks; Method III.]
- Rosenberg, M. B. (2003). *Nonviolent Communication: A Language of Life.* PuddleDancer Press. [OFNR four-component structure; Center for Nonviolent Communication.]
- Winslade, J. & Monk, G. (2000). *Narrative Mediation: A New Approach to Conflict Resolution.* Jossey-Bass. [Dominant-narrative deconstruction, unique-outcomes technique, re-authoring.]
- Bush, R. & Folger, J. (1994, rev. 2004). *The Promise of Mediation.* Jossey-Bass. [Transformative mediation: empowerment and recognition.]
- Hoffman, D. (2011). "Mediation and the Art of Shuttle Diplomacy." *Negotiation Journal* 27(3). [Harvard PON treatment of shuttle diplomacy and the caucus-vs-no-caucus debate.]
- Harvard Program on Negotiation (PON) daily publications (2010s–2020s) on BATNA, integrative bargaining, third-story framing, and the "Mediation through Understanding" no-caucus model.
- Beyond Intractability project (Burgess & Burgess, University of Colorado) summaries of Fisher/Ury and Ury for interest-based bargaining and single-text negotiation.
