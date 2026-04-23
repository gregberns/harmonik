# External source: Therapy intake and motivational interviewing

> Phase 2, Step 2 external-source pass. Domain: motivational interviewing (Miller & Rollnick), Rogerian client-centered therapy, structured clinical interviews (SCID-5, M.I.N.I.), and adjacent non-directive elicitation traditions.
>
> Target reader: researcher building a unified candidate catalog of planning protocols. Candidate-generating, not prescriptive. External evidence proposes; observed-corpus evidence disposes.

## Why this domain

Clinical intake and motivational interviewing (MI) are the most elaborated external traditions for a specific, hard problem: **surfacing what a fuzzy-intent, sometimes-ambivalent, often-pre-articulate speaker actually wants, without the elicitor collapsing the speaker's framing into their own.** This maps directly onto the Phase 1-flagged "human with fuzzy intent" case — the one the harmonik corpus shows the agent handling worst (either over-writing the user's framing, or deferring trivial decisions because it did not know what to press on).

MI in particular is attractive as a source because:

- It is a **compact, named set of protocol moves** (OARS, EPE, the four processes, change talk) rather than a diffuse stance. The moves have evidence-based operationalizations and even inter-rater coding schemes (MITI).
- The meta-analytic evidence base is substantial — Hettema, Steele, & Miller (2005) pooled 72 trials; Magill et al. (2014) meta-analyzed the **technical hypothesis** (that therapist skills cause client change talk, which causes outcome); Magill et al. (2018) extended to the relational hypothesis. Short-term effect sizes ~0.77, longer-term ~0.30. The mechanism — eliciting the speaker's own reasons, not supplying them — has measurable correlational support (therapist MI-consistent skills → change talk: r = .26).
- MI has been operationalized outside therapy (health coaching, organizational change, public health). The domain already has a track record of being *portable* beyond its origin context.

The Rogerian tradition underneath MI supplies the core elicitation mechanic (reflective listening, non-directive stance). The structured-clinical-interview tradition (SCID, M.I.N.I.) sits at the opposite pole — fully scripted, branching, screener-gated — and is instructive as a counterweight even though the heavyweight form is not adaptable wholesale.

## Role-inversion caveat (applies to every candidate below)

Every move in this domain assumes a competent-elicitor / uncertain-speaker asymmetry. The therapist is the expert on *how to elicit*; the client is the expert on *their own content*. This is clean. It does not map cleanly to coding-planning.

In coding-planning:

1. The **agent** is the elicitor. The agent is a competent elicitor on **surface** technique (it can ask open questions, reflect, summarize) but is often **less domain-expert** than the human on the problem content. The Rogerian "I am barren of wisdom; only you hold it" stance is partially *true* for the agent, which is unusual — it is not a pose.
2. The **human** is a senior solo developer. Unlike a clinical client, the human does not want a therapeutic relationship. They want their intent elicited, compactly and without ritual.
3. The human is often **not ambivalent** (the MI target case) but **under-specified** (they have a working model but have not rendered it). MI's evocation machinery is overkill when the blocker is "agent did not ask the load-bearing question" and under-powered when the blocker is "agent proposed the wrong thing and human does not want to spend attention correcting it."
4. The **"righting reflex"** (see below) — the therapist's impulse to solve — is *the agent's whole job* in software. A pure MI stance for an LLM coding agent would be paralyzing. The question is not "should the agent suppress the righting reflex" but "which moments call for suppression vs. expression."

The adaptations below are chosen against these inversions. Therapeutic warmth, validation-for-its-own-sake, and pure non-directiveness are **rejected** as transplants. The elicitation mechanics — the *shapes* of moves — are what we extract.

## OARS Reflection Cascade

**One-line**: Instead of acknowledging or paraphrasing the human's input, the agent issues a *reflection* — a specific restatement form calibrated by depth (simple → complex → double-sided) — as its primary listening-response, with the reflection doing implicit hypothesis-testing about the agent's comprehension.

**Mechanism (source theory)**: Reflective listening is the central technical move of MI, inherited from Rogers. Miller & Rollnick (*Motivational Interviewing*, 4th ed.) and the Bethea/UNC workshop materials distinguish three depth tiers:

- **Simple reflection**: same-level restatement — repeat ("You want to start the medication") or slight rephrase ("Taking the medication matters to you"). Purpose: demonstrate hearing, invite continuation.
- **Complex reflection**: adds meaning not yet said — paraphrase, reflection of feeling, amplified reflection, continuing-the-paragraph. Purpose: test a hypothesis about the speaker's underlying stance. The speaker hears their thought rendered in a sharper or slightly different light and refines.
- **Double-sided reflection**: names both sides of the speaker's ambivalence in one utterance ("You want to ship fast, *and* you want the spec to be right first"). Purpose: make ambivalence *explicit and joint* rather than individually asserted, which changes how the speaker engages with it.

The key empirical finding (Magill et al. 2018; Apodaca et al. 2016): **complex reflections and open questions** are the moves most associated with clients moving from sustain talk to change talk. Simple reflections and paraphrases maintain engagement but do not shift stance. That is: depth of reflection is load-bearing for the *purpose* of the move, not just cosmetic.

Crucially, reflection is **grammatically a statement**, not a question. That is the MI-specific choice: the uptick in voice or the "did I hear you right?" is absent. The speaker is free to confirm, refine, or let it stand. This lowers the response cost.

**Dimension values**:

- Timing of alignment: continuous, per-turn
- Decision locus: human-dominant (reflections do not decide; they test)
- Dialog form: short-volley, statement-shaped
- Question style: n/a — the move is declarative, not interrogative
- Autonomy scope: none (reflection is not a decision)
- Context richness: continuous-building
- Plan expression form: dialog-log (reflections accrete into a shared vocabulary)
- Knowledge direction: **agent-investigative**, but with the speaker in the driver's seat
- Review/critique integration: self-correcting — a missed reflection is corrected in the next human turn
- Fuzziness-stability: the strongest move in the catalog for holding fuzziness without collapsing it; the grammatical-statement form lets the human refine without feeling interrogated

**Predicted trade-offs**:

- *Plus*: attacks the corpus-observed "human had to write a long clarifying paragraph because the agent misunderstood" pattern. A wrong complex reflection is corrected in one human line; a silent misunderstanding produces paragraphs of repair.
- *Plus*: double-sided reflection gives a language for the corpus-observed ambivalence-without-surfacing ("part of me wants X, part of me wants Y") case. Today the agent typically picks one side and the ambivalence is lost.
- *Plus*: reflections cost the human *less* than questions because they do not compel an answer; the human confirms by silence-then-proceed.
- *Minus*: reflection is grammatically non-action. Cascading reflections on a senior who already knows what they think is the maieutic-pathology from the Socratic document — the agent sounds like it is stalling. The technique must be *targeted at moments of under-specification or apparent ambivalence*, not applied uniformly.
- *Minus*: simple reflection in isolation reads as pure acknowledgment ("got it"), which is the existing failure mode. Only complex / double-sided reflections are load-bearing — and those require the agent to *commit to a hypothesis* about what the human meant, which agents are often reluctant to do.
- *Minus*: over-reflecting eats turn budget. MI sessions are 30-50 minutes; a coding-planning session is sometimes 5 turns.

**Adaptation (elicitor = LLM)**:

- Default to **complex reflection** on load-bearing moments. Reserve simple reflection for the rare case where the agent legitimately has not understood and is holding position while asking the next question.
- **Double-sided reflection** gets its own named trigger: when the human has expressed tension between two goals inside a single utterance, the agent names both sides. This produces an explicit tension-artifact useful for the spec.
- Drop the "statement-not-question" grammar for the LLM. The agent appending "— is that right?" is acceptable and in fact necessary to avoid the failure mode where the agent silently encodes a wrong interpretation and proceeds. Therapeutic non-questioning assumes real-time corrective feedback; async chat doesn't guarantee it.
- Suppress reflection when the human's stance was explicit and stable. Reflecting a clearly-stated decision reads as agent stalling or agent doubting.

**Evaluation plan**:

- Corpus audit: find turns where the human wrote a long clarifying paragraph correcting a prior agent misunderstanding. Hypothesize that a complex reflection by the agent *before* the misunderstanding propagated would have shortcut the repair. Count instances.
- Live trial: for one session-pass, require the agent to issue a complex reflection (one line, statement-shaped, ending "— is that right?") at any turn where the human's input contained two-or-more claims. Measure whether the reflection was confirmed, amended, or rejected. Amendment-rate is the signal of value.
- Specifically test double-sided reflection on a session the corpus already shows contained tension the human never resolved. Does the reflection produce a decision artifact?

## Evocation (change-talk elicitation)

**One-line**: Instead of proposing an option and asking the human to react, the agent asks a question whose purpose is to surface the human's own reasons, desire, ability, needs, or commitment toward a direction — **without the agent naming the direction first**.

**Mechanism (source theory)**: MI's most distinctive move. The DARN-CAT taxonomy names what the elicitor is listening for:

- **Preparatory change talk**: **D**esire ("I want to"), **A**bility ("I could"), **R**easons ("because Y"), **N**eed ("I have to")
- **Mobilizing change talk**: **C**ommitment ("I will"), **A**ctivation ("I'm willing"), **T**aking steps ("I already did X")

The evocation move asks a question whose answer will be DARN-CAT-shaped. Canonical forms: "What would be good about X?" "Why would you want to do that?" "If you did decide, how might you go about it?" "What makes this matter to you?" The empirical claim — supported by Magill et al. 2014's meta-analysis of the technical hypothesis — is that **the speaker's own verbalization of reasons causally precedes change**. Having the elicitor supply the reasons is inert; having the speaker articulate them is not.

The sharper point: **evocation deliberately refuses to introduce the content first.** It is an inversion of the usual agent-proposal-then-critique flow.

**Dimension values**:

- Timing of alignment: pre-action, especially early-phase
- Decision locus: human-preempted (agent deliberately withholds proposal)
- Dialog form: one-at-a-time open questions
- Question style: open-ended, specifically shaped to elicit DARN/CAT-type answers
- Autonomy scope: none (the agent is in drawing-out mode)
- Context richness: continuous-building
- Plan expression form: dialog-log → enumerated reasons / constraints / commitments
- Knowledge direction: **agent-investigative**, maximally
- Review/critique integration: none (this is elicitation)
- Fuzziness-stability: preserves fuzziness by construction — the agent has not named the shape the answer should take

**Predicted trade-offs**:

- *Plus*: attacks the corpus-observed "agent proposed, human spent attention correcting" pattern. If the agent does not propose, there is nothing to correct.
- *Plus*: the output is the human's own language — which per the Clean Language / Directional Clean-Repetition candidate from the Socratic document, is the vocabulary that should survive into the spec. Evocation and verbatim-preservation compose.
- *Plus*: the DARN-CAT taxonomy gives the agent a lens for what it is listening for. If the human produces R ("because Y") without D ("I want to"), the agent can ask a follow-up specifically targeted at the missing register. This is Graesser-taxonomy-shaped for the listening side.
- *Minus*: the senior developer often has a preference already. Asking "why would you want to?" when the human has implicitly said "I want to X" is the insulting-the-senior pathology from the Socratic document. Evocation works when the human is under-articulated, not when they are simply under-verbalized.
- *Minus*: evocation without closure is directionless. MI's **four processes** (engage → focus → evoke → plan) put evocation inside a containing arc; evocation in isolation becomes a fishing expedition. The planning phase is where evocation's output gets consolidated.
- *Minus*: therapeutic MI expects evocation over multiple sessions. A coding-planning session has one session. The agent has to elicit, consolidate, and move on faster than a therapist would.

**Adaptation (elicitor = LLM)**:

- Reserve evocation for moments the agent diagnoses as *under-specified-intent*. The diagnosis is: "the human's input does not contain DARN for this decision." If D and R are already present, skip evocation and propose.
- Use DARN-CAT as a **listening checklist** rather than a speaking framework. The agent does not ask "what do you desire?"; it asks open questions and classifies what comes back, then targets follow-ups on the missing register.
- Evocation should not replace proposal. The role-inverted rule: evoke to build the model, *then* propose against the model. The MI-pure "never propose" stance is wrong for this setting.
- **Mobilizing change talk** (CAT) is the human's own articulation of commitment — "I'll go with X." When the agent detects CAT, that is the transition-to-plan signal. Before CAT appears, the session is still in evoke; after it appears, the session moves to consolidation.

**Evaluation plan**:

- Corpus audit: find sessions where the agent proposed a direction in its first or second turn, and the human spent the rest of the session steering the agent away from the proposal. Hypothesize an evocation move would have produced a closer-to-correct proposal later.
- Code a sample of corpus sessions by DARN-CAT: do the human's utterances contain DARN registers early, and CAT registers late? If the pattern is absent, does the session end with a weaker commitment? This is the clinical finding adapted to corpus.
- Live trial: on one under-specified intent per session, agent opens with an evocation question and withholds proposal until it hears D+R (or equivalent). Measure whether the first proposal that follows survives the session without retraction.

## Elicit-Provide-Elicit (EPE) information exchange

**One-line**: When the agent has information to deliver (a design observation, a library choice, a caveat), it first asks what the human already knows, asks permission to share, provides compactly, then asks the human's reaction.

**Mechanism (source theory)**: MI's canonical pattern for information delivery without the delivery itself being a lecture or an imposition. Three-move structure:

1. **Elicit**: "What do you know about X?" / "What's your read on X?" (Establishes the baseline; avoids telling the human what they already know.)
2. **Provide**: "Can I share what I've seen on this?" → delivery, compactly. (Permission-gated. Content-dense rather than hedged.)
3. **Elicit**: "What do you make of that?" / "How does that land?" (Surfaces the human's reaction rather than assuming the delivery was accepted.)

The permission-asking is not a courtesy: it is a diagnostic. If the human declines the offer, the agent has learned that this branch is not where attention is currently invested, and re-routes. If the human accepts, the provision has authorization and is not an imposition.

**Dimension values**:

- Timing of alignment: per-information-delivery moment
- Decision locus: mixed — agent delivers, human rules on landing
- Dialog form: structured (three moves), short
- Question style: open at elicit-1 and elicit-2, closed permission-ask between
- Autonomy scope: bounded-by-topic
- Context richness: minimal-dispatch per move, but composes
- Plan expression form: dialog-log; the third-move reaction is the spec-useful artifact
- Knowledge direction: bidirectional (agent-investigative at elicit-1, human-investigative at provide, agent-investigative at elicit-2)
- Review/critique integration: the second elicit is a live review of the provide
- Fuzziness-stability: preserves the human's prior frame (elicit-1 captures it before the agent overwrites it)

**Predicted trade-offs**:

- *Plus*: directly attacks the "agent over-wrote the human's intent" pain point. Elicit-1 surfaces the human's existing model *before* the agent's model contaminates the discussion.
- *Plus*: permission-asking is a low-cost signal of autonomy-respect. For a senior user who dislikes agent-lecturing, this inverts the register without adding ceremony.
- *Plus*: the final elicit catches mislanded provision. Today, agents often deliver and move on; the human's non-reaction is read as assent when it may be skimming. Explicit reaction-solicitation is cheap insurance.
- *Minus*: three moves for every information delivery is heavy. A senior doesn't want to be asked "can I share?" before every observation.
- *Minus*: elicit-1 is a Bloom-level-1 question in disguise if the agent already has reason to believe the human knows the domain well. It can read as "warm-up" filler.
- *Minus*: permission-asking can become performative if the human always says yes. The discipline depends on the permission being *real* — the agent has to be willing to accept a no.

**Adaptation (elicitor = LLM)**:

- Reserve full three-move EPE for **non-trivial, non-obvious** information — architectural observations, library trade-offs, caveats the agent is uncertain will land. Do not apply to routine factual deliveries.
- Collapse to two moves (provide + elicit-2) when elicit-1 would be insulting ("what do you know about React?" to a senior React developer). The final elicit is the non-negotiable part.
- Permission-asking can be abbreviated. "One observation, if useful:" is a permission-ask in declarative form; the human can still decline by redirecting.
- EPE composes naturally with reflection: elicit-1 is often a reflection of what the human already said, with an invitation to add.

**Evaluation plan**:

- Corpus audit: find agent turns that delivered information and were followed by the human correcting or contradicting the information. Was elicit-1 missing? Would it have shortcut the contradiction?
- Audit for missed reactions: find places the agent delivered and moved on, and later turns revealed the human had misread or disagreed. Was the final elicit missing?
- Live trial: for one consequential information delivery per session, agent uses full three-move EPE. Measure whether the final elicit produces a signal that would not have appeared otherwise.

## Rolling with Resistance (as response to contest)

**One-line**: When the human contests an agent's framing or pushes back on a proposal, the agent *does not* defend, re-explain, or counter-argue. Instead, it reflects the pushback, asks an open question about the contested point, and lets the human re-state the concern in their own terms.

**Mechanism (source theory)**: Miller & Rollnick distinguish **sustain talk** (speaker's arguments for not changing — addressing the *topic*) from **discord** (speaker's arguments against the *elicitor* — addressing the *relationship*). Both call for the same technical response: do not counter. Counter-argument *strengthens* the position being countered — the speaker hears themselves defending it and becomes more committed. Rolling with resistance is the refusal of that trap. Responses: reflect (including amplified reflection — gently exaggerate the speaker's position until the speaker backs away from it themselves), shift focus, emphasize autonomy ("you're the one who decides"), reframe ("another way to see it…").

In MI 4th ed., the term "resistance" has been retired in favor of "sustain talk" and "discord" separately, because the older framing blamed the client for a clinician-produced effect. The clinical finding (Miller & Rose 2009; "counselor-behavior-caused-resistance" literature): **clients who push back are often reacting to elicitor over-reach, not to the topic itself.** The fix is to change the elicitor's behavior.

**Dimension values**:

- Timing of alignment: reactive — triggered by pushback
- Decision locus: human-dominant (agent is deliberately not advancing its own position)
- Dialog form: short-volley, reflection + question
- Question style: open-ended, focused on the contested point
- Autonomy scope: reduced to zero for the duration of the move
- Context richness: continuous-building
- Plan expression form: dialog-log
- Knowledge direction: **agent-investigative** (back into listening)
- Review/critique integration: self-corrective — the move assumes the agent was the one who erred
- Fuzziness-stability: maximally preserves fuzziness around the contested point

**Predicted trade-offs**:

- *Plus*: directly attacks a corpus-visible pattern where the agent, when contradicted, re-explains in more detail, which the human reads as the agent not listening. Reflecting-and-re-asking breaks the cycle.
- *Plus*: the MI finding that the elicitor's behavior *produces* the resistance is directly applicable. If the agent is getting repeated pushback, the rolling response forces the agent to rebuild the model rather than push harder against a wrong model.
- *Plus*: amplified reflection is a cheap diagnostic — stating the human's position more strongly than they stated it lets the human correct "no, not *that* strongly" without feeling attacked. This surfaces the real boundary.
- *Minus*: a senior developer sometimes pushes back because the *agent* is wrong on the content, not because the agent has over-reached on process. In that case, the agent must update, not reflect. Rolling when the agent is actually wrong reads as the agent deflecting.
- *Minus*: the MI move assumes the elicitor can wait for the speaker to come back around. In a time-bounded planning session, the agent may need to re-advance a position the human is contesting, not surrender it indefinitely.
- *Minus*: distinguishing sustain-talk-about-topic from discord-about-relationship requires a judgment call the agent may not reliably make.

**Adaptation (elicitor = LLM)**:

- The trigger is *repeated* pushback on the same point, not first pushback. First pushback can be met with re-explanation or update; second pushback on the same point is the signal to roll.
- The move is: (1) single-line reflection of the human's objection; (2) one open question about it ("what's under that?"); (3) update on the agent's side based on the answer. The agent does not defend in step 1.
- "Emphasizing autonomy" in therapy ("it's your choice") is trivially true in coding-planning (of course it's the user's project). Replace with an explicit fork: "I had X in mind; you're pushing toward Y. Want to hear the X case one more time, or are we on Y?"
- Reject the MI-pure stance that the agent should never counter-argue. A senior user sometimes wants the counter. The protocol move is: *don't counter on repeated pushback*; instead, surface whether the user wants the counter or not.

**Evaluation plan**:

- Corpus audit: find session-sequences where the human pushed back on the same point 2+ times and the agent re-explained each time. Mark these as rolling-failure events. Count frequency.
- Live trial: agent monitors for repeated pushback; on the second pushback, switches to reflect-and-ask. Measure whether the third turn converges (human updates, agent updates, or explicit-fork resolves) vs. the status-quo pattern of a fourth pushback.

## Agenda Setting (focusing / agenda mapping)

**One-line**: At session (or pass) start, agent and human explicitly negotiate what will be discussed — a compact list of candidate topics, with the human choosing which to address and in what order.

**Mechanism (source theory)**: MI's **focusing** process (second of four). The elicitor lays out the *space* of topics that could productively be discussed ("bubbles" on a shared page, in the clinical version), and the client chooses. The claim is that agenda-setting is not cosmetic: having the speaker name the topic produces investment in the topic in a way that therapist-chosen topics do not. It also prevents the therapist's default topic (usually: the most clinically pressing) from dominating a session where the client needed to discuss something else.

Partnership/collaboration (the P in the MI spirit's PACE) is operationalized most concretely at agenda-setting. If the therapist and client do not agree on what the session is about, OARS moves later in the session are unmoored.

**Dimension values**:

- Timing of alignment: pre-session, pre-pass
- Decision locus: mixed (agent proposes slate, human selects)
- Dialog form: structured (list + selection), short
- Question style: proposal-with-selection — the agent offers a menu
- Autonomy scope: bounded-by-category (the selected topic)
- Context richness: minimal-dispatch
- Plan expression form: structured (an agenda artifact)
- Knowledge direction: bidirectional (agent proposes slate from its model; human chooses from own model)
- Review/critique integration: the selection *is* a review of the agent's proposed slate
- Fuzziness-stability: stabilizes fuzziness by pinning one topic while leaving others parked

**Predicted trade-offs**:

- *Plus*: attacks the corpus-observed "agent jumped to the wrong thing first" pattern. The menu surfaces the options before action.
- *Plus*: an explicit agenda is a spec-shaped artifact — the rest of the session is indexed against it.
- *Plus*: low-cost. A one-line slate + one-line selection is 30 seconds of session time.
- *Plus*: the items *not* selected are themselves useful — they become the next-session agenda or the parked-question list. This composes with the aporia move from the Socratic document.
- *Minus*: for a one-topic session the menu is ceremony.
- *Minus*: the agent's slate reflects the agent's model of what matters. If the slate is wrong, the selection operates on a wrong space. (Counter-argument: a wrong slate is itself a signal — the human's reaction to the slate reveals their actual priorities.)
- *Minus*: naive agenda-setting at every kerf-pass transition would feel bureaucratic.

**Adaptation (elicitor = LLM)**:

- Apply at **pass transitions** (problem-space → decomposition, etc.) and at session start when multiple topics are plausibly in scope. Skip when the scope is unambiguous.
- Slate should be **3-5 items**, compact ("What shape does X take?" "How does Y interact with Z?" "Decide ordering of A/B"). Longer slates are a symptom of the agent not having prioritized.
- Human selection is not the only valid move — the human can add ("none of those; this other thing") or reorder. The slate is a starting point.
- Post-session, the un-selected items become a carry-forward artifact, not lost.

**Evaluation plan**:

- Corpus audit: for sessions that ended with the human saying "we didn't get to X," check whether X was on an implicit agenda from session start that wasn't surfaced. Hypothesize agenda-setting at start would have either (a) prioritized X, or (b) made the deferral explicit.
- Live trial: apply agenda-setting at kerf-pass transitions for one work. Measure whether the agenda matches what actually gets discussed, and whether un-selected items are retained for later.

## Readiness / Confidence Ruler

**One-line**: Agent asks the human to rate something on a 0-10 scale, then asks two follow-ups: "why not a lower number?" (elicits the existing reasons) and "what would it take to move up?" (elicits the blockers).

**Mechanism (source theory)**: A micro-elicitation technique. The rating itself is not the point — the *follow-up questions* are. "Why a 6 and not a 3?" forces the speaker to articulate the positive reasons they already hold (which the speaker may not have verbalized). "What would make it a 9?" forces articulation of the gap. Common applications: importance ruler ("how important is X?"), confidence ruler ("how confident are you in X?"), readiness ruler ("how ready to do X?"). In MI practice, the two axes are separately informative — someone can be high-importance / low-confidence (needs skill-building) or low-importance / high-confidence (needs motivation-building), and the intervention differs.

**Dimension values**:

- Timing of alignment: at a decision point where the human's commitment or certainty is ambiguous
- Decision locus: human-dominant (the rating is theirs)
- Dialog form: structured (number + two follow-ups), short
- Question style: one closed question (the rating) followed by two open questions
- Autonomy scope: n/a (the move is purely elicitation)
- Context richness: minimal-dispatch
- Plan expression form: structured — produces an (importance, confidence, reasons, blockers) tuple
- Knowledge direction: **agent-investigative**
- Review/critique integration: none built-in
- Fuzziness-stability: converts fuzzy stance into quantified stance without collapsing *why*

**Predicted trade-offs**:

- *Plus*: fast, compact, produces a spec-useful artifact (blockers list, reasons list).
- *Plus*: the "why not lower" question is non-obvious and high-yield — asking for *reasons already held* is different from asking "what are the reasons," and produces different answers.
- *Plus*: separating importance from confidence is a useful diagnostic the agent can act on. If the human says the decision matters but they don't feel confident, the agent's job shifts toward surfacing evidence; if the reverse, toward re-examining whether the decision is worth doing.
- *Minus*: numerical rulers feel clinical. Senior developers may find "rate your confidence 1-10" silly. The framing needs to be adapted ("scale of don't-care to bet-the-project").
- *Minus*: the ruler is strongest for behavior-change decisions (human deciding whether to act) and weaker for design decisions (agent and human jointly choosing between two architectures). The two follow-ups still work as questions, without the rating.
- *Minus*: two-axis rating (importance, confidence) is a lot of apparatus for most decisions.

**Adaptation (elicitor = LLM)**:

- Drop the literal 0-10 scale in most cases. Keep the two follow-ups as standalone questions: "What's already pulling you toward this?" (reasons-held) and "What would flip it the other way?" (blockers).
- The importance/confidence distinction *is* worth keeping for meta-questions: "How load-bearing is this decision?" vs. "How certain are you about it?" differ, and asking both at decision points exposes the "uncertain about a load-bearing thing" cell, which deserves more investigation than the "certain about a trivial thing" cell.

**Evaluation plan**:

- Corpus audit: find decisions in finished specs whose rationale is thin. Was there a point in the session where a "why not lower" follow-up would have produced the missing rationale?
- Live trial: use the two-follow-up form at 1-2 decision points per session. Check whether the produced rationale survives into the spec unchanged.

## Four-Process Arc (engage → focus → evoke → plan)

**One-line**: Impose a named four-phase structure on the planning session: *engage* (build model), *focus* (set agenda), *evoke* (elicit reasons), *plan* (consolidate and commit). Each phase has distinct moves and a distinct exit condition.

**Mechanism (source theory)**: Miller & Rollnick's (MI 3rd/4th ed.) reframing of MI away from a set of moves toward a set of **processes**. The insight: the same moves (OARS) mean different things at different phases. An open question during engage builds rapport; during evoke it elicits DARN; during plan it tests commitment. The phase is context for the move.

The four phases are not strictly linear — MI practice explicitly says they recur and overlap — but they are **cumulative**: you cannot productively evoke before the engage foundation is laid; you cannot productively plan without evocation surfacing the commitment-language.

**Dimension values** (meta-protocol):

- Timing of alignment: continuous; each phase has its own timing
- Decision locus: varies per phase (engage: n/a; focus: mixed; evoke: human-preempted; plan: bidirectional)
- Dialog form: structured at the top level, short-volley within phases
- Question style: varies per phase
- Autonomy scope: varies per phase
- Context richness: continuous-building across phases
- Plan expression form: structured (the plan artifact comes out of the last phase; earlier phases feed it)
- Knowledge direction: agent-investigative in engage/focus/evoke; bidirectional in plan
- Review/critique integration: the transition between phases is a natural review gate
- Fuzziness-stability: the arc *is* a fuzziness-management device — the early phases resist premature planning, the late phase resists never-planning

**Predicted trade-offs**:

- *Plus*: gives the agent a meta-model for "what mode am I in?" Today, agents oscillate between modes within a turn (half-reflection, half-proposal), which is a corpus-visible pathology. Naming the phase is a self-disciplining device.
- *Plus*: phase transitions are natural moments for the kerf-pass review-sub-agent pattern the user already employs. Engage → focus transition is when "do I actually understand the problem?" should be audited; evoke → plan is when "does the human's own articulation support this?" should be audited.
- *Plus*: the **exit condition** for each phase is a useful spec-shaped artifact. Engage exits when the agent can restate the human's picture in the human's terms without correction. Focus exits when the agenda is set. Evoke exits when DARN/CAT-shaped material is on the page. Plan exits when a commit-able artifact exists.
- *Minus*: imposing a four-phase arc on a 5-turn session is overkill. The arc assumes a 30+ minute session; the agent needs a collapsed form.
- *Minus*: phases can feel performative if named out loud. "Now we're in the focusing phase" is ceremony.
- *Minus*: the clinical planning phase in MI is optional and sometimes skipped entirely; translating that directly would produce specs-without-action, which is wrong for the coding context.

**Adaptation (elicitor = LLM)**:

- Use phase labels **internally** to select moves, not as user-facing language.
- Collapse to three phases when needed: **engage-focus** (understand what we're solving), **evoke** (elicit what the human holds), **plan** (consolidate). Or two phases: **understand**, **decide**.
- The engage-exit condition — "agent can restate human's picture in human's terms without correction" — is a concrete, testable gate and is probably the single highest-value adaptation from this framework. Having that gate *at all* is more important than the full arc.
- Plan phase is non-optional in coding-planning (unlike therapy). The commit-able artifact is the whole point.

**Evaluation plan**:

- Corpus audit: classify turns in finished sessions by implicit phase. Hypothesis: sessions rated high-quality have clearer phase boundaries; low-quality sessions show mode-oscillation within turns.
- Live trial: agent labels internally which phase it is in, and uses the phase to gate moves (no proposals during engage; no reflection-only turns during plan). Measure whether session-end artifact quality improves.
- Specifically test the engage-exit gate: for one session, the agent must produce a restatement of the human's picture and receive confirmation *before* advancing. Measure drift-correction frequency downstream.

## Summary-as-Transition

**One-line**: Before moving from one topic or phase to the next, the agent produces an explicit summary — a compact written restatement — of what was established, invites correction, and only then advances.

**Mechanism (source theory)**: The **S** in OARS. A summary in MI is a special kind of reflection that **collects** (several points at once), **links** (connects to earlier session content), or **transitions** (marks a pivot). Summaries are used at natural session breakpoints. The clinical effect: the client hears their own articulated content played back in aggregate, which either ratifies it or lets them correct drift before it propagates into the next phase.

Summaries are distinct from **simple recapping**: they select, they emphasize, they may re-order. The therapist's choice of *what to summarize* is itself a move — what is included is implicitly marked as load-bearing, what is excluded as not.

**Dimension values**:

- Timing of alignment: at topic or phase transitions
- Decision locus: human-dominant (human ratifies or amends)
- Dialog form: structured (a paragraph-length summary + invitation to correct)
- Question style: declarative with tail question ("did I capture it?")
- Autonomy scope: n/a
- Context richness: rich-brief (summary is compact but dense)
- Plan expression form: prose or structured list — directly reusable as spec draft material
- Knowledge direction: bidirectional (agent renders, human rules)
- Review/critique integration: the summary *is* a review; amendment *is* the critique response
- Fuzziness-stability: stabilizes what's been settled while leaving open items explicitly open

**Predicted trade-offs**:

- *Plus*: attacks the corpus-observed "session ended without an artifact, and the next session starts over" pattern. A summary-at-transition is a commit point.
- *Plus*: the summary's *selection* is itself a high-value move. Agent and human can disagree productively about what was load-bearing — that disagreement is diagnostic.
- *Plus*: composes directly with the closing-move in the Socratic Seminar Turn Frame candidate and with the exit conditions in the Four-Process Arc candidate.
- *Minus*: a full summary is verbose. On small topic-shifts, a one-line recap suffices; on phase transitions, the full summary is worth it.
- *Minus*: a summary produced when the human has not said much (early session) is mostly the agent's projection — a mini-paraphrase pathology. Summary requires content to summarize.

**Adaptation (elicitor = LLM)**:

- Apply at phase transitions (per Four-Process Arc) and at any turn where 3+ exchanges have accumulated on one topic.
- The summary should privilege the human's own language (Directional Clean-Repetition, from the Socratic document). Summarizing in agent-vocabulary is the same drift-risk as paraphrase.
- The trailing question should be open, not closed — "what's missing?" beats "did I capture it?" because the latter invites yes-assent without engagement.
- Every kerf-pass should end with a summary-as-transition to the next pass.

**Evaluation plan**:

- Corpus audit: count sessions that end without an explicit summary. Cross-reference with whether the next session in the same work restarts material. Hypothesize: missing-summary sessions cause more re-work.
- Live trial: require a summary at every kerf-pass transition. Measure whether amendments to the summary surface drift that would otherwise have propagated.

## Screener-Gated Branching (SCID/MINI-style)

**One-line**: At topic entry, agent asks 2-3 closed screening questions whose answers determine whether that topic-branch is expanded or skipped — gating against heavyweight detailed inquiry when the branch is irrelevant.

**Mechanism (source theory)**: The SCID-5 and M.I.N.I. are opposite-pole counterweights to MI's non-directive stance. They are fully scripted, modular, and branch-gated. Each module (SCID) or section (MINI) begins with 2-4 screening questions; a positive screen opens the full module, a negative screen skips it. This is how a structured clinical interview covers 20+ diagnostic categories in 15-30 minutes: the *depth* is gated by the *screener*.

The MINI uses branching tree logic explicitly: "most modules begin with a handful of simple questions; additional questions are asked only if responses to screen questions are positive." The format privileges coverage-at-low-cost over conversational flow. Symptoms are coded present/subthreshold/absent.

This is almost the *opposite* of MI's evocation, and it is useful precisely as contrast: the SCID trades flow for coverage; MI trades coverage for depth. Neither is right for all cases.

**Dimension values**:

- Timing of alignment: per-topic-branch
- Decision locus: agent-autonomous (the gate logic)
- Dialog form: scripted, branching
- Question style: closed (yes/no) at screener level, opens only in expanded branches
- Autonomy scope: bounded by the gate decisions
- Context richness: minimal-dispatch per screener; rich in expanded branches
- Plan expression form: structured (a matrix: which branches were expanded, what came out of each)
- Knowledge direction: agent-investigative, procedurally
- Review/critique integration: none
- Fuzziness-stability: collapses fuzziness by construction — the screener forces a binary

**Predicted trade-offs**:

- *Plus*: coverage. Attacks the corpus-observed "agent forgot to ask about X, which turned out to matter." A screener-checklist forecloses missed categories.
- *Plus*: compact. 5 screeners × 10 seconds each = 1 minute of session time to rule out 5 branches.
- *Plus*: the *matrix artifact* (which branches checked, which expanded, what found) is directly spec-shaped. It's an audit trail of where the planning did and did not go.
- *Minus*: scripting reads as interrogation. Senior developer + agent-with-a-checklist = bad. The MI community's criticism of over-scripted approaches applies: the flow is sacrificed.
- *Minus*: binary screeners collapse legitimate fuzziness. "Is authentication in scope for this plan? y/n" may elicit a yes when the real answer is "partially, depending on X."
- *Minus*: the agent's screener list is itself a prior — it reflects the agent's model of what might be relevant. Missing categories stay missing (the agent cannot screen for what it didn't think to ask).

**Adaptation (elicitor = LLM)**:

- Use a **tiny screener** (2-3 questions) only at a specific moment: when transitioning into a new planning area whose scope is ambiguous. "Before we go deep: is authentication in scope? is multi-tenancy? is migration-of-existing-data?" Each yes opens an engage/evoke cycle; each no parks the branch.
- Reject the binary-only form. Allow "yes", "no", and "partially-explain" responses. Partially-explain opens a short evocation.
- The matrix artifact (checked / expanded / parked) is the useful export. It gives later sessions a map of the decision-space coverage.
- Not a replacement for MI-style elicitation — complementary. Use when the human is in "help me triage" mode; use MI when the human is in "help me understand what I'm trying to do" mode.

**Evaluation plan**:

- Retrospectively apply a screener at kerf problem-space pass entry for one completed work. Does it surface categories the actual pass missed?
- Live trial: introduce a 3-item screener at the start of a new planning session. Measure whether it produces a useful coverage artifact vs. adding ceremony.

## Non-Directive Stance (Rogerian core)

**One-line**: Agent deliberately does not propose, interpret, or advocate; only reflects and asks open questions, positioning itself as content-free with the human as content-authority.

**Mechanism (source theory)**: Rogers' client-centered therapy rests on **core conditions**: accurate empathy, unconditional positive regard, congruence. The therapist does not direct, does not interpret, does not advise. The claim is that the client's own organizing tendency will produce forward motion if the relationship provides the right conditions — so introducing therapist-content is actively counter-productive.

The stance is **not a technique**; it is a posture that makes specific techniques (reflection, open questions) mean what they mean. Apply the techniques in a directive stance and they become manipulation; apply them in a non-directive stance and they are Rogerian.

MI inherits the non-directive stance *as a starting point* but modifies it: MI is explicitly directional (there is a change-goal), even as it remains non-coercive. This is the "dance, not a wrestling match" phrasing Miller uses.

**Dimension values**:

- Timing of alignment: continuous; governs all moves
- Decision locus: human-dominant by design
- Dialog form: any (it's a posture, not a form)
- Question style: any (it's a posture, not a form)
- Autonomy scope: minimal (agent does not advance own agenda)
- Context richness: continuous-building
- Plan expression form: whatever the human authors
- Knowledge direction: **agent-investigative**, maximally
- Review/critique integration: none
- Fuzziness-stability: fuzziness is preserved by default

**Predicted trade-offs**:

- *Plus*: zero drift from agent-proposal. The agent literally does not propose.
- *Plus*: frames the human as the competence-holder, which inverts the junior-LLM-toying-with-senior pathology.
- *Minus*: **this is the wrong posture for a coding agent whose job includes proposing**. A pure non-directive stance for an LLM coding agent is *not neutral* — it is an abdication. The user explicitly wants proposal, critique, pushback. Non-directiveness in this context under-serves the user who has said "do not defer trivial decisions."
- *Minus*: pure non-directiveness in an LLM is also *performative* — the model has content, and withholding it is a lie about capacity. The user can detect this.
- *Minus*: the "organizing tendency" premise — that the speaker, given space, will produce the right outcome — is plausibly true for psychological growth but not for software architecture. Software does not have an organizing tendency; designs do not self-organize without decision input.

**Adaptation (elicitor = LLM)**:

- **Non-directive stance is applied at specific moments, not as a default.** Named moments: early-engage when the agent genuinely does not have a model; moments after repeated pushback (per rolling-with-resistance); one-off deep-listen moves where the agent suspects its model is wrong.
- Outside those moments, the agent is *directional*. The MI adaptation — non-coercive but directional — is closer to correct for coding-planning than pure Rogerian non-directiveness.
- Even at its most non-directive, the agent preserves congruence (does not pretend to be empty) and autonomy-support (does not coerce). These two of Rogers' three conditions translate; unconditional positive regard does not (it is not the agent's job to affirm every human choice).

**Evaluation plan**:

- Pick corpus moments where agent-proposal led to drift correction. For a subset, construct an alternate: what would a non-directive turn have looked like? Does the alternate survive better?
- Live trial: on one engage-phase per session (under the Four-Process Arc), the agent commits to non-directiveness. Measure whether the picture that emerges is closer to the human's actual model than in proposal-first sessions.

## Affirmations (strengths-naming, non-obsequious)

**One-line**: Agent names a concrete competence, decision, or effort the human has already demonstrated — not as praise but as acknowledgment that informs what the agent will subsequently do.

**Mechanism (source theory)**: The **A** in OARS. Distinct from generic encouragement or flattery: an affirmation names a *specific* behavior or quality and treats it as load-bearing evidence. "You've traced the consequence of that choice three levels deep already" is an affirmation; "great work!" is not. The empirical claim is that affirmations build the speaker's confidence and reinforce behavior the elicitor wants to encourage (in MI: change-directed behavior). The reinforcement framing is more honest than the praise framing.

**Dimension values**:

- Timing of alignment: at moments of human demonstration of a competence
- Decision locus: agent-autonomous (agent decides what to affirm)
- Dialog form: one line, declarative
- Question style: n/a
- Autonomy scope: none
- Context richness: minimal-dispatch
- Plan expression form: dialog-log
- Knowledge direction: agent-investigative (agent is noticing and surfacing)
- Review/critique integration: none
- Fuzziness-stability: n/a

**Predicted trade-offs**:

- *Plus*: the *specific* form is low-cost and non-performative. "You've already decided X, which means Y is closed" names a competence by using it.
- *Minus*: **generic affirmations are the failure mode this entire research track is reacting against.** The anti-sycophancy position is clear: the agent praising the human is not value-adding and is often read as filler.
- *Minus*: the clinical move depends on the competence-asymmetry (therapist is the judge). For coding-planning, the agent-as-judge-of-human-competence is awkward.
- *Minus*: the user-instruction is to not import therapeutic relationship norms — affirmation is the one closest to that boundary.

**Adaptation (elicitor = LLM)**:

- Reject affirmation-as-praise. Retain only the narrow form: **"You've already established X, so I'll skip re-deriving it"** — which is an affirmation in function (acknowledges the human's work) without being an affirmation in posture.
- Alternatively, affirmations *up the chain* are honest: when the agent reports to a reviewer sub-agent or to a later session, noting "the human established X cleanly" is load-bearing context, not praise.

**Evaluation plan**:

- This candidate is low-priority. If adopted at all, evaluate by whether affirmation-of-competence shortcuts re-work (agent does not re-ask a thing the human already settled).

## Considered and set aside

**Unconditional positive regard (Rogers' third core condition)**. This is the therapy-specific posture of valuing the client regardless of content. It does not translate. The agent's job is to engage critically with the human's decisions, not to regard them unconditionally. Importing this produces sycophancy, which is the opposite of what the user wants.

**Full motivational interviewing as a session-long stance**. MI is a clinical discipline that takes training and ongoing supervision to do well; its full form is not an appropriate transplant to a 5-10 turn planning session. The moves (OARS, EPE, rolling, agenda-mapping) port; the *total stance* does not.

**MITI / MISC coding schemes (inter-rater reliability instruments)**. These are research tools for coding recorded therapy sessions against MI fidelity. Interesting as a validation instrument if we later want to measure how MI-like an agent's turns are, but not themselves a protocol the agent runs.

**SCID-5 full module coverage**. A clinical-interview-style 20-module fully-scripted pass over a planning session is ceremony of the highest order and would infuriate a senior developer. The screener-gated branching mechanic above retains the useful idea (cheap coverage via gated depth) without the full apparatus.

**Stages-of-change / transtheoretical model (Prochaska & DiClemente) stage-matching**. The five-stage model (precontemplation → contemplation → preparation → action → maintenance) is often paired with MI in clinical training. For coding-planning the stage analog is weak: the human is usually in "action readiness" from turn one (they already decided to build the thing) — they are not pre-contemplating *whether* to plan. The stage model imports poorly; the sub-move within it (matching elicitor behavior to speaker's current orientation) is useful but is subsumed by the phase-sensitivity of the Four-Process Arc.

**Empathic validation as a move**. Distinct from affirmation-as-acknowledgment, pure validation ("that must be frustrating") is a relationship-building move. It does not belong in planning. Rejected.

**Miracle question and solution-focused brief therapy techniques**. "If you woke up tomorrow and the problem was solved, what would be different?" — a question from solution-focused therapy, adjacent to MI. Useful occasionally as a framing device, but the construction is ornamental in a planning context where the agent can simply ask "what does done look like?" without the miracle framing.

**Therapist self-disclosure**. A Rogerian/humanistic move where the therapist shares a personal reaction to build the relationship. Does not apply: the agent does not have a personal history to disclose, and disclosures-about-its-processing ("as an AI, I feel…") are off-putting rather than relationship-building. Rejected.

**Motivational Enhancement Therapy (MET) as a four-session protocol**. MET is a manualized version of MI that delivers feedback in a specific early session and structures later sessions around commitment-language. The four-session structure is too long-form for planning. Specific MET elements (structured feedback delivery) are covered by EPE above.

## Cross-cutting observation

The most portable moves from this domain are those that **test the agent's model of the human's intent without advancing the agent's own position** — complex reflection, double-sided reflection, summary-as-transition, elicit-in-EPE. These are the agent-investigative moves par excellence, and they populate a region of the design space that the Socratic document also crowds into but from a different angle: Socratic is about *probing implications of stated positions*, MI is about *surfacing unstated positions*. Together the two external sources give a reasonably full coverage of agent-investigative technique.

The second strongly-generalizable pattern is **permission before content** (EPE's permission-ask, agenda-setting's collaborative menu, rolling-with-resistance's autonomy-emphasis). Permission is a cheap signal and a diagnostic: the human's willingness or refusal to let the agent proceed reveals whether the agent's current direction is landing. In a senior-user context, permission-asking is also a status-respect move — it treats the human as the authority, which they actually are.

The largest single caution against naive import is **non-directiveness as a default posture**. The user explicitly wants proposal, critique, and pushback. The non-directive Rogerian stance is right for specific micro-moments (early engage, after repeated pushback) and wrong as a default. MI's own position — directional but non-coercive — is closer to the adapted target. The research here is not "how can the agent be less directive" but "at which specific moments is temporary non-directiveness high-value, and how does the agent enter and exit those moments."

## Primary sources cited

- Miller, W. R., & Rollnick, S. (2013). *Motivational Interviewing: Helping People Change* (3rd ed.). Guilford Press. (OARS, four processes, spirit/PACE, DARN-CAT, sustain talk vs. discord.)
- Miller, W. R., & Rollnick, S. (2023). *Motivational Interviewing: Helping People Change and Grow* (4th ed.). Guilford Press. (Current formulation; retirement of "resistance" term.)
- Rogers, C. R. (1951). *Client-Centered Therapy*. Houghton Mifflin. (Non-directive stance, reflective listening foundation.)
- Rogers, C. R. (1957). "The Necessary and Sufficient Conditions of Therapeutic Personality Change." *Journal of Consulting Psychology*. (Six conditions, including the core three.)
- Hettema, J., Steele, J., & Miller, W. R. (2005). "Motivational interviewing." *Annual Review of Clinical Psychology*, 1, 91-111. (Meta-analysis; effect sizes ~0.77 short-term, ~0.30 long-term.)
- Magill, M., Gaume, J., Apodaca, T. R., Walthers, J., Mastroleo, N. R., Borsari, B., & Longabaugh, R. (2014). "The technical hypothesis of motivational interviewing: a meta-analysis of MI's key causal model." *Journal of Consulting and Clinical Psychology*. (MI-consistent skills → change talk, r = .26.)
- Magill, M., Apodaca, T. R., Borsari, B., et al. (2018). "A meta-analysis of motivational interviewing process: Technical, relational, and conditional process models of change." *Journal of Consulting and Clinical Psychology*, 86(2), 140-157. (Relational hypothesis; empathy role.)
- Apodaca, T. R., et al. (2016). "A sequential analysis of motivational interviewing technical skills and client responses." *Journal of Substance Abuse Treatment*. (Complex reflection and open questions most associated with sustain→change transitions.)
- Miller, W. R., & Rose, G. S. (2009). "Toward a theory of motivational interviewing." *American Psychologist*. (Clinician-behavior-produces-resistance finding.)
- Lundahl, B. W., Kunz, C., Brownell, C., Tollefson, D., & Burke, B. L. (2010). "A meta-analysis of motivational interviewing: Twenty-five years of empirical studies." *Research on Social Work Practice*. (Broader evidence base.)
- First, M. B., Williams, J. B. W., Karg, R. S., & Spitzer, R. L. (2015). *Structured Clinical Interview for DSM-5 (SCID-5)*. American Psychiatric Association. (Modular scripted interview; screener-gated branching.)
- Sheehan, D. V., Lecrubier, Y., et al. (1998). "The Mini-International Neuropsychiatric Interview (M.I.N.I.)." *Journal of Clinical Psychiatry*, 59(Suppl 20). (Branching-tree logic in compact diagnostic interview.)
- Rollnick, S., Miller, W. R., & Butler, C. C. (2008). *Motivational Interviewing in Health Care*. Guilford Press. (Elicit-Provide-Elicit formalization; importance/confidence rulers in brief-intervention settings.)
