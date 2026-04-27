# External domain: consulting discovery and scoping protocols

Phase 2, Step 2 sub-agent output. Track: planning-protocols. Domain: management- and technology-consulting discovery-phase protocols (MECE / issue trees, SPIN, stakeholder interviews, hypothesis-driven consulting, laddering / so-what, Pyramid Principle / SCQA, engagement-letter scoping, steering-committee realignment).

## Why this domain matters

Consulting firms are paid to rapidly build enough understanding of a client's situation to propose useful work — and the engagement letter, not the engagement itself, is what the client buys on first meeting. That forces the discovery-phase protocols to optimize for **efficient intent-extraction from a busy stakeholder** under two hard constraints: (a) the consultant is the less-domain-expert party and must admit it openly without losing authority, and (b) billable time is scarce enough that every question has to earn its place. The direct analog to our setting: a coding agent rapidly building enough understanding of a solo senior developer's intent to propose a plan. The senior-responder inversion that dominated the Socratic-method adaptation applies here too — consulting is one of the few domains where the tradition *already assumes* the responder knows more about their own situation than the questioner, and has formalized teachable moves for that asymmetry.

The emphasis across this domain is on three moves the harmonik corpus currently under-uses:

1. **Structured-artifact framing** — the engagement letter, ghost deck, issue tree, and SCQA intro are all artifacts that travel between sessions and let the agent anchor its model explicitly.
2. **Proposed decomposition as a first move** — the consultant's MECE / issue-tree proposal is an *offered* decomposition that the client confirms or corrects, not a request for the client to author one. This is agent-investigative in the sense of "agent proposes, human rules," rather than "agent interrogates, human supplies."
3. **Explicit non-goals** — engagement letters contain an out-of-scope section by convention. The observed corpus rarely states what the plan is *not* doing, and this is a named failure mode in the practitioner-diagnostic catalog.

A terminology note: throughout this document, "protocol" means an adapted-for-solo-plus-LLM version of the consulting practice. Where the original requires billable-time leverage or a multi-person engagement, the adaptation is called out explicitly.

**Cross-cutting caveat — removing billable time.** Consulting discovery protocols are shaped by the fact that the consultant has 30-60 minutes with a senior stakeholder, charging high rates, and will not get a second shot. This forces compression: the consultant must show value in the first meeting or lose the engagement. An LLM agent with a solo developer faces no such pressure — tokens are cheap, the session can continue, and there is no lost-engagement risk. For each candidate below, the *Adaptation notes* section explicitly asks: what was the technique doing for the billable-time-constrained consultant, and what falls off when that constraint is removed? Some techniques (compact SCQA openers, Pyramid Principle) survive intact because they address cognitive load, not time cost. Others (mega-stakeholder-interview, one-shot engagement letters) should be relaxed, broken into smaller moves, or run multiple times.

---

## 1. MECE decomposition (Mutually Exclusive, Collectively Exhaustive)

**Description.** Originated with Barbara Minto at McKinsey in the late 1960s. A decomposition is MECE when its sub-categories do not overlap (mutually exclusive) and together cover the entire problem space (collectively exhaustive). The protocol move in the consulting setting: the consultant proposes an MECE breakdown of the client's problem; the client confirms, corrects a category, or identifies a missing branch. The decomposition itself is an artifact that travels through the rest of the engagement.

**Dimension values.**
- Timing of alignment: pre-action, early-phase.
- Decision locus: agent-proposes, human-rules (agent authors decomposition; human confirms or corrects).
- Dialog form: structured-artifact (the tree) + short-volley (confirmation turns).
- Question style: batched, forced-choice ("does this cover it?" / "which of these is most consequential?").
- Autonomy scope: bounded-by-category (agent runs analysis within a confirmed branch).
- Context richness: rich-brief (the tree *is* the context structure).
- Plan expression form: structured-artifact.
- Knowledge direction: **agent-investigative** (agent offers a structure and probes for where it fails).
- Review/critique integration: the ME and CE checks are themselves self-review disciplines applied by the author before proposing.

**Mechanism (source theory).** Two forces:

- **Mutually exclusive** catches *double-counting* — the same phenomenon appearing in two analytical categories, producing phantom effect size. In consulting, this is the failure mode where a team reports growth drivers as "new customers" and "increased customer spend" without noticing that upsells to existing customers appear in both columns.
- **Collectively exhaustive** catches *blind-spots* — entire causal branches the team hasn't considered. CE is enforced by the final "other" bucket: if the "other" category is > ~10% of the phenomenon, the decomposition is failing CE.

Together, ME + CE force the consultant to propose a decomposition that can be *disproved* at the structural level, not just at the content level. A non-MECE decomposition is not an argument; it's a bucket of observations.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* MECE as a self-review discipline maps directly to a reviewer sub-agent prompt: "for this decomposition, name any two categories that overlap, and name any category of cause you have not listed." Free to run; forces the agent to stress-test its own framing before presenting.
- *Pro:* The structured-artifact output is directly consumable by the human — a tree is easier to ratify than a dialog.
- *Pro:* Attacks the observed "silent framing drift" failure mode. When the agent proposes an explicit decomposition, misalignment becomes visible at category-naming time, not after three turns of dialog that assumed the wrong frame.
- *Con:* True MECE is often impossible for messy real-world problems. Enforcing it strictly produces false-crispness: the agent may label a decomposition MECE that merely *looks* MECE. The discipline is only useful when the agent is willing to mark a decomposition as "MECE-ish" or "best-effort-exhaustive."
- *Con:* Agent-proposed decomposition risks **anchoring** — once the human sees the agent's tree, they may not notice a missing branch, because the explicit frame shapes their retrieval. Presenting the decomposition with the explicit invitation "which branch have I missed?" partly counters this, but the anchoring effect is real.

**Adaptation notes.** What consulting's MECE does that survives billable-time removal: it compresses the agent's internal model into a stance the human can ratify quickly. That compression is valuable even when time is cheap, because it shifts the human's work from authoring to judging — lower writing-load (pair P1). What falls off: the consulting version is a one-shot proposal because the consultant cannot iterate cheaply; the agent version can propose, revise, and re-propose in the same session. MECE becomes iterative rather than one-shot.

**Evaluation plan.** Take finalized kerf works where "misaligned framing" was flagged retroactively. Construct a candidate MECE decomposition of the problem from the opening session state. Check: would the decomposition's categories have exposed the framing that was eventually found to be wrong? If yes, MECE-as-opener is a candidate. Secondary: run a reviewer sub-agent with the prompt "identify overlap or gaps in this decomposition" on three current plans; measure whether it surfaces category failures the general reviewer missed.

## 2. Issue tree (diagnostic and solution variants)

**Description.** The issue tree (also called "logic tree") is the standard consulting operationalization of MECE. A top-level question is decomposed vertically into sub-questions; sub-questions decompose further; leaves are answerable with data or judgment. Two named variants, per Chevallier's formalization of McKinsey practice:

- **Diagnostic tree** — breaks down a "why" question (why has revenue fallen?). Each branch is a candidate causal path. The leaves are testable hypotheses.
- **Solution tree** — breaks down a "how" question (how can we raise margins?). Each branch is a candidate intervention class.

Key discipline: branches at the same level must be MECE; the tree is grown **top-down** (question-first) not bottom-up (cause-first); depth is uneven — some branches terminate at level 2, others need level 4 or 5, depending on where the load-bearing analysis is.

**Dimension values.**
- Timing of alignment: pre-action, structuring.
- Decision locus: agent-proposes, human-rules-per-branch (human can prune, extend, or re-weight branches).
- Dialog form: structured-artifact (tree) + per-branch confirmation.
- Question style: embedded (each node is a question); open-ended at internal nodes, forced-choice at leaves.
- Autonomy scope: bounded-by-branch (agent runs analysis within a selected branch; human controls which branches get investigated).
- Context richness: rich-brief.
- Plan expression form: structured-artifact (tree is the plan backbone).
- Knowledge direction: bidirectional — agent proposes structure, human supplies context and prunes.
- Review/critique integration: tree-shape is itself inspectable — a reviewer sub-agent can be asked whether any level fails MECE or whether depth allocation matches importance.

**Mechanism.** Two mechanisms are substantive:

- **Top-down construction.** Starting with the question, not with the observations, forces the tree to reflect the *decision* being made. Bottom-up trees accrete observations without a decision at the top — they are catalogs, not plans.
- **Uneven depth.** Forcing uniform depth would waste analysis on low-importance branches. The tree's asymmetry is load-bearing: where depth grows, the consultant is signaling "this is where the real work is." A human scanning a tree reads depth as importance.

The diagnostic/solution distinction matters because a tree built to answer "why" cannot be re-used to answer "how" — they produce different branches even from the same root problem. In practice, a consulting engagement often runs a diagnostic tree first to locate the root cause, then a solution tree rooted at that cause.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Kerf's pass structure is already tree-adjacent (problem-space → decompose → research → design). Making the decompose pass output an explicit issue tree — rather than prose — would produce a reviewable structure.
- *Pro:* The diagnostic-versus-solution distinction maps onto a useful planning-time move: "are we currently asking why or how?" The answer changes which questions the agent should ask. Observed corpus sessions frequently conflate the two.
- *Pro:* Asymmetric depth is a compact way for the human to signal focus without writing long prose — "go deep on branch 3" is one sentence.
- *Con:* Issue trees can become ceremony if applied to problems that are already well-framed. For a bug whose cause is obvious, the tree is overhead. Triage is required: trees for ambiguous problems, prose for clear ones.
- *Con:* Tree construction is sensitive to the choice of top-level question. A badly-phrased root makes every branch wrong; the agent must iterate on the root before committing to sub-structure. This is a known consulting failure mode ("framing the question").

**Adaptation notes.** Survives billable-time removal intact — issue trees were always for cognitive organization, not time compression. What does shift: consulting typically produces one issue tree per engagement because re-doing it is expensive in client-time; an agent can re-construct the tree as understanding grows. This suggests a "living issue tree" that updates across sessions — not the traditional static one-shot artifact.

**Evaluation plan.** Retrospectively construct diagnostic and solution issue trees for two finalized kerf works. For each: did the final spec occupy a branch the tree exposed, or did it end up at a branch the tree missed entirely? The second outcome is a failure signal for the tree as a candidate. Separately, classify each current kerf work's root question as diagnostic or solution-shaped — and check whether the research pass stayed in the corresponding mode.

## 3. SPIN sequence (Situation / Problem / Implication / Need-payoff)

**Description.** Neil Rackham's 1988 formalization, derived from analysis of 35,000+ sales calls. SPIN is a sequenced question-type protocol for large-sale discovery:

- **Situation questions** — factual background about the buyer's current state. ("What stack are you currently on?")
- **Problem questions** — specific dissatisfactions or pain points. ("Where does that stack fail you?")
- **Implication questions** — consequences of the problems, surfacing downstream cost. ("What happens when those failures occur — how does it cascade?")
- **Need-payoff questions** — elicit the buyer's own articulation of what a solution would be worth. ("If this were solved, what would change for you?")

Rackham's empirical finding: top performers asked ~4× more implication questions than average performers. Situation questions, despite being intuitive, had *no* measurable correlation with deal success when asked in excess — they extract information the salesperson could have gotten from pre-reading.

**Dimension values.**
- Timing of alignment: pre-action, early-phase (discovery).
- Decision locus: human-dominant (the human does most of the talking under correct SPIN).
- Dialog form: short-volley, sequenced.
- Question style: one-at-a-time, shifting-type-across-phases (open-ended → increasingly specific).
- Autonomy scope: bounded (agent is in elicitation mode).
- Context richness: continuous-building.
- Plan expression form: dialog-log at first; the agent later restructures.
- Knowledge direction: **agent-investigative**, heavily.
- Review/critique integration: none inherent; the sequence is its own self-discipline.

**Mechanism.** Each question type does a specific job; the sequence matters:

- **Situation** is context load — cheap information. The discipline is to *minimize* this class and pre-research what can be pre-researched.
- **Problem** questions force the respondent to articulate pain; articulation is the first step toward willingness to change.
- **Implication** is the real work — the respondent is led to trace causal chains from the stated problem to its downstream cost. This is where the buyer's internal model of "how painful is this, really?" shifts. It is the *analytical* part of SPIN; rushing past it to a solution is the most common failure mode.
- **Need-payoff** elicits the buyer's own vocabulary for what success looks like. The salesperson does not supply the benefits; the buyer does. This avoids the "I told you so" dynamic where the seller names benefits the buyer doesn't recognize.

The Rackham empirical finding about situation questions is the load-bearing part: **pre-reading substitutes for situation questions**. Asking situation questions live is the lazy shortcut that signals the consultant didn't prepare.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The question-type sequencing is directly portable as an agent-investigative protocol discipline. An agent that has pre-read the repo and recent commits substitutes for the pre-read that kills situation questions; it can skip straight to problem questions. This matches the "situation pre-loading" practice observed in the richest opener sessions in the corpus.
- *Pro:* Implication questions are the pattern most absent from the corpus. The agent rarely asks "if we don't fix this, what cascades downstream?" — it either solves the stated problem or asks for more specification. Implication probing is where hidden scope and hidden priority get surfaced.
- *Pro:* Need-payoff questions produce an artifact (the user's own statement of what success looks like) that maps directly to acceptance criteria in the eventual spec.
- *Con:* The sales-pitch origin of SPIN contaminates it. Need-payoff questions in sales are a manipulation device (the buyer talks themselves into the purchase); in planning, the same question is just-elicitation. The adaptation must drop the pitch and keep the elicitation.
- *Con:* Rigid sequence mid-planning feels performative. The four types are better as a *classification lens* the agent applies to its own questions than as a strict script. "Is this question a situation, problem, implication, or need-payoff?" — with the discipline that too many situation questions means the agent hasn't done its homework.

**Adaptation notes.** The billable-time pressure in SPIN is severe — the salesperson has one meeting. Relaxing it means: the agent can ask cheap situation questions if pre-reading fails, then catch up. But the *discipline* that situation questions are the first thing to cut remains valuable: when the agent's early questions are mostly situation-type, it is a signal that pre-reading was insufficient, and the diagnostic in §6.1 of the framework should flag this.

**Evaluation plan.** Classify agent-to-human questions in a corpus sample by SPIN type. Hypothesize: low-quality planning sessions over-index on situation questions; high-quality ones over-index on implication questions. Live trial: prompt the agent to ask at least one implication question before proposing any solution. Measure whether this surfaces hidden-scope items that would otherwise emerge late.

## 4. Stakeholder interview protocol (opening / transversal / personalized / closing)

**Description.** Standardized consulting practice for structured one-hour interviews with key stakeholders during discovery. Widely formalized (Nielsen Norman Group, Codurance, think.design, DECODE): the 30–60 minute interview has four named phases. The transversal / personalized split is the subtlest part:

- **Opening.** Introductions, purpose, duration, confidentiality. Two to three minutes. Sets consent to the interview frame.
- **Transversal questions.** Identical across all stakeholders in the same engagement — designed to surface *divergence* between stakeholders. The same question ("what does success look like?") asked of CEO, CTO, and PM produces three possibly-incompatible answers; the incompatibility is the finding.
- **Personalized questions.** Specific to the stakeholder's expertise and role. The CTO gets technical-architecture questions the CEO would not.
- **Closing.** The interviewer summarizes key messages retained and has the interviewee validate or correct the summary. Explains next steps and how the interviewee will hear about results.

Active listening and verbal/non-verbal engagement cues are treated as protocol elements, not social niceties.

**Dimension values.**
- Timing of alignment: pre-action, discovery.
- Decision locus: **human-dominant** (the stakeholder supplies content; the interviewer captures).
- Dialog form: structured (four named phases) with short-volley inside the middle two.
- Question style: mixed — transversal is pre-written and batched; personalized is open-ended and live-generated.
- Autonomy scope: none (interviewer is in capture mode).
- Context richness: rich-brief at close (the summary is the brief).
- Plan expression form: dialog-log → summary artifact.
- Knowledge direction: **agent-investigative**, explicitly.
- Review/critique integration: closing summary is a human-author-self-review, with interviewer as scribe.

**Mechanism.** Four mechanisms are substantive:

- **The closing-summary ritual** is the single most valuable move in the protocol. Without it, the interviewer's model of what was said drifts from the stakeholder's model of what was said, silently. The ritual catches drift while the stakeholder is still present.
- **Transversal / personalized split.** Transversal questions are a comparison instrument, not an extraction instrument — their value is in *cross-stakeholder divergence*, not in any single stakeholder's answer. Personalized questions extract stakeholder-specific knowledge. Confusing the two produces wasted airtime.
- **Opening frame-setting.** Stating the purpose and duration consents the stakeholder to the mode. Missing this step produces the "where is this going?" anxiety that suppresses candor.
- **Active listening as instrument.** Mirroring, verbal acknowledgement, and well-placed pauses are treated as information-extraction tools, not politeness — they signal engagement and invite elaboration.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The closing-summary ritual is an extraordinarily high-value adoption. In the harmonik corpus, sessions frequently end with the agent's model of what was decided unstated and uncorrected. A forced "before we end, here is what I heard — correct me" move costs 2-3 turns and catches drift at the last-cheap point.
- *Pro:* The opening frame-setting is trivially adoptable — a one-line agent statement at session start about what mode the session is in (discovery / design / commitment) reduces the corpus-observed "what kind of conversation is this?" ambiguity.
- *Pro:* Active-listening mirroring maps onto the Socratic-sources "directional clean-repetition" candidate — verbatim repetition of the human's load-bearing phrases before probing. Different tradition, same move.
- *Con:* The transversal/personalized split has no direct analog in the solo-user case — there is only one stakeholder. But: the pattern *does* port if the "stakeholders" are the user-at-different-times or the user-in-different-roles. Transversal questions asked at session start vs. session end can surface intent-drift within the same user.
- *Con:* The protocol assumes a finite-length interview; planning sessions with an agent have no fixed duration. The closing-summary ritual must be triggered by something other than time — either a turn-count threshold, or a phase-transition signal.

**Adaptation notes.** Billable-time removal matters less here than the other candidates — stakeholder interviews are about *structure*, and structure survives cheap tokens. The main adaptation is re-purposing the transversal/personalized split: not for multiple humans, but for the same human across time or across roles (user-as-architect vs user-as-implementer asked the same transversal question). This is not trivially cheap and would need piloting.

**Evaluation plan.** On a corpus sample, identify sessions that ended without the agent's final model explicitly validated. Hypothesize: these correlate with later framing-corrections (the C1 metric). If yes, a mandatory closing-summary move is high-leverage. Separately, for a single live planning session, add a transversal question ("what does success look like?") at turn 5 and again at turn 30; compare the answers.

## 5. Hypothesis-driven consulting (Day-1 answer, ghost deck)

**Description.** The core McKinsey move, codified across Rasiel's *The McKinsey Way*, Minto's *Pyramid Principle*, and the firm's internal training. On day 1 of an engagement, before substantive data is collected, the team commits to an **initial hypothesis** — a one-sentence candidate answer to the client's top question. A **ghost deck** (also called shell or skeleton deck) is a draft of the final presentation, with slide titles already written, representing what the team currently *believes* the answer will be. Analysis is then structured to prove or disprove the hypothesis, not to gather undirected data. If data disconfirms, the hypothesis is revised and the ghost deck is rewritten; this is the cycle.

The discipline has two enforcement conditions:

1. **The hypothesis must be falsifiable.** "Revenue is down because of customer mix shift" is falsifiable; "there is a problem" is not. Unfalsifiable hypotheses are rejected at internal review.
2. **The hypothesis is held honestly.** A common failure mode (flagged internally at McKinsey and by Axiom Strategic) is treating the hypothesis as a conclusion to defend rather than a hypothesis to test — the "gut-driven, disguised as hypothesis-driven" pathology. Rasiel: "Hypotheses proven false can be just as valuable as hypotheses proven true."

**Dimension values.**
- Timing of alignment: pre-action, at session start.
- Decision locus: agent-proposes-commits, human-rules-at-review-points.
- Dialog form: structured-artifact (hypothesis statement + ghost deck) + batched revision cycles.
- Question style: the hypothesis *is* a compressed question (agent asks human: "is this the answer?").
- Autonomy scope: bounded-by-hypothesis (agent runs analysis structured to falsify the hypothesis within its scope).
- Context richness: rich-brief (hypothesis + ghost deck compress intent).
- Plan expression form: **structured-artifact** (heavy).
- Knowledge direction: bidirectional — agent proposes, human corrects, data disposes.
- Review/critique integration: falsification *is* the review; a reviewer sub-agent can explicitly hunt for hypothesis-disconfirming evidence.

**Mechanism.** Two mechanisms:

- **Commitment forces prioritization.** An undirected investigation can chase any branch. A hypothesis-directed investigation can only spend effort on evidence that would bear on the hypothesis. This is the 80/20 lever — the consultant is forced to pick which 20% of analysis matters before doing any of it.
- **Falsification discipline catches confirmation bias.** If the hypothesis is held honestly, every data-gathering move is scored on whether it *could* falsify. Moves that could only confirm are cut. This is the Popperian discipline brought into consulting practice; it is also where the practice most often breaks down, because honest falsification is psychologically expensive for the consultant who bet on the hypothesis.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Directly compares to the observed corpus pattern "pre-action plan disclosure" — where the agent proposes a plan before acting and invites pushback. Hypothesis-driven consulting is the same move with a sharpened edge: the agent commits to a *specific falsifiable answer* and structures dialog to expose where it's wrong.
- *Pro:* The ghost deck has a direct analog — a draft spec (or skeleton kerf artifact) produced early, treated as a hypothesis-to-refute rather than a decision-to-defend. This changes the reviewer sub-agent's job from "is this good?" to "what would prove this wrong?"
- *Pro:* Forces agent commitment, attacking the corpus-observed "agent deferral" failure mode. An agent that declines to commit to an initial hypothesis is failing the protocol.
- *Con:* The "honestly held" condition is hard to enforce in an agent that has strong priors from pre-reading. The agent may run through the falsification motions while internally treating the hypothesis as decided. The defense: a reviewer sub-agent specifically assigned to find evidence *against* the proposed hypothesis (role-split review, from the design-review external source).
- *Con:* Premature commitment is a real risk. A badly-formed hypothesis at turn 1 can lock the session into a frame that only surfaces its wrongness late. The bulkhead is the revision cycle — the hypothesis *must* be treated as living, with explicit revision events.
- *Con:* The "Day-1 answer" signal reads as overconfidence to a senior user ("how can you already know?"). The adaptation: the agent frames the hypothesis as a stake-in-the-ground, explicitly as a target for the human to shoot down, not as a conclusion.

**Adaptation notes.** Billable-time pressure is what originally forced Day-1 hypotheses — the client is paying from day 1 and wants to see a working hypothesis fast. Relaxing that pressure means the hypothesis can be revised more freely within the same session. But the underlying mechanism (commitment forces prioritization) is independent of the time pressure — it is a cognitive discipline, not a time discipline. Survives intact. Compare to the Socratic-sources "Reduced Dialectic" candidate: Reduced Dialectic proposes position + counter + synthesis; hypothesis-driven consulting proposes hypothesis + falsification plan + revised hypothesis. Same skeleton, different emphasis — Reduced Dialectic foregrounds the counter; hypothesis-driven foregrounds the evidence.

**Evaluation plan.** On a sample of finalized kerf works, identify the session where the final framing first appeared. If the framing appeared early and was merely refined, the work is hypothesis-compatible — adding an explicit early hypothesis would have formalized what happened naturally. If the framing shifted radically mid-work, the work was *not* hypothesis-compatible — the early hypothesis would have anchored the wrong direction. Measure the ratio. Live trial: explicit initial hypothesis stated at session start; track whether final spec matches, revises, or rejects it.

## 6. Pyramid Principle (answer-first communication)

**Description.** Barbara Minto's 1970s formalization, based on Aristotelian rhetoric and her observation of McKinsey communication patterns. The core rule: in business communication, put the answer at the top. Below the answer, three-to-seven supporting arguments, grouped MECE. Below each argument, the evidence. The reader (or listener) can stop at any level and still have a complete picture at that level of detail.

The three rules for building a correct pyramid:
1. Ideas at any level are summaries of the ideas grouped below.
2. Ideas in each group are the same *kind* of idea (logically or categorically).
3. Ideas in each group are logically ordered (deductive, chronological, structural, or comparative).

The **so-what test**: at each level, an idea that does not contribute a specific insight to the level above is cut. "Interesting data" without a "so what?" is not information — it's filler.

**Dimension values (meta-protocol; applies to any agent output).**
- Timing of alignment: applies at every agent-to-human communication moment.
- Decision locus: unchanged.
- Dialog form: structured output format.
- Question style: unchanged.
- Autonomy scope: unchanged.
- Context richness: unchanged.
- Plan expression form: forces agent output into top-down hierarchy.
- Knowledge direction: unchanged.
- Review/critique integration: self-review via the so-what test.

**Mechanism.** The key insight is that **decision-makers scan, they don't read**. A busy senior wants the answer first; supporting reasoning on request; raw evidence only if pressed. Pyramid Principle matches output structure to this scan pattern: the top line is the headline, and detail is opt-in.

The so-what test is a specific error-catcher: agents (and consultants) under pressure produce prose that *describes* without *concluding*. The test forces every paragraph to earn its place by contributing a specific insight upward. If the paragraph is deleted, does the parent conclusion become weaker? If no, delete the paragraph.

The MECE grouping of arguments is what makes the pyramid auditable: the senior can check "have we covered everything?" at each level by examining the group, without reading the children.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Directly adoptable as agent output discipline. A system-prompt-level rule that agent responses over a certain length lead with the answer (not the reasoning) matches what the senior-developer responder wants. Observed corpus sessions sometimes end with the user having to ask "so what's the upshot?" — this is pyramid-principle failure.
- *Pro:* The so-what test is a cheap self-review the agent can apply to its own draft responses before sending.
- *Pro:* Composes with structured reviewer output from the design-review external source (categories / scribe / time-box). Pyramid-structured reviewer comments are easier to triage.
- *Con:* Answer-first is wrong when the audience does not yet *trust* the answer — a senior may want to see reasoning before conclusion to assess whether the agent has understood. Pyramid Principle assumes the reader already trusts the author; in the agent-to-user direction, trust is not fully established, so the reasoning-first mode is sometimes correct. The adaptation: answer-first for routine responses; reasoning-first when the agent is making a consequential claim.
- *Con:* Over-pyramiding on every response reads as corporate. Short, conversational exchanges don't need a pyramid; they need a direct answer. The discipline applies to longer outputs (plans, reviews, synthesis), not to ping-pong.

**Adaptation notes.** Zero billable-time pressure dependency — Pyramid Principle is a cognitive-load technique. Survives cleanly. The only adjustment is the trust-asymmetry noted above: when the agent is proposing something it knows the user may reject, leading with reasoning can be the better move. This is a protocol variant, not a contradiction.

**Evaluation plan.** Sample agent-to-human turns in the corpus where the response exceeds 500 characters. Classify each as "answer-first," "answer-last," or "answer-implicit." Hypothesize: answer-first responses correlate with shorter next-human-turns (less effort to process) and fewer follow-up clarifying questions. If yes, answer-first is a low-cost adoption.

## 7. SCQA (Situation / Complication / Question / Answer)

**Description.** Also from Minto — the Pyramid Principle's companion protocol, but for the *introduction* of a plan or document rather than its body. SCQA provides the structure for the opening paragraph:

- **Situation:** a shared understanding of the status quo the audience will agree with.
- **Complication:** the change or disruption that makes the status quo inadequate.
- **Question:** the implicit question the audience now has.
- **Answer:** the recommendation — which then heads the Pyramid Principle body.

The question is often left implicit; the situation-complication pairing is what triggers it in the audience's mind. SCQA is compact (three to five sentences) and designed to orient a reader who is landing cold.

**Dimension values (meta-protocol).**
- Timing of alignment: at every significant session opener or artifact opener.
- Decision locus: unchanged.
- Dialog form: structured opener.
- Question style: unchanged (the question in SCQA is the *topic* question, not the dialog move).
- Autonomy scope: unchanged.
- Context richness: rich-brief (compact — the SCQA *is* the brief).
- Plan expression form: structured-artifact (opener).
- Knowledge direction: unchanged.
- Review/critique integration: none inherent.

**Mechanism.** The Situation-Complication pairing is the load-bearing move. The audience arrives with their own mental model of context; Situation aligns both parties on the baseline *before* the disruption is introduced. Complication then names the change. Without Situation, the audience has to reconstruct which status quo is being disrupted, and different audience members reconstruct differently. The compact, shared-agreement Situation eliminates that drift.

The implicit-Question move is subtler: naming the question is sometimes too directive (the audience feels led); leaving it implicit invites the audience to arrive at it themselves, producing better engagement with the Answer.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Directly addresses the "context-dump opener" pattern observed in the corpus — where the human writes a long framing paragraph. An SCQA-shaped opener is compact (three to five sentences), and forces the human to name the Complication explicitly, which is the most common missing element.
- *Pro:* Agent-authored SCQA at session start compresses the agent's pre-reading into a stance the human can ratify or correct fast. Maps onto "pre-action plan disclosure" with sharper structure.
- *Pro:* Re-usable across planning artifacts — each kerf pass could produce an SCQA intro, so readers (or the next agent session) have a compact orientation.
- *Con:* SCQA assumes the author already knows the Answer. When the Answer is genuinely unknown at session start, the final move cannot be done, and SCQA degrades to SC (situation-complication-question, *no* answer). That's the case where hypothesis-driven consulting (section 5) kicks in: the agent supplies a hypothesis-as-answer to hold the slot.
- *Con:* Formulaic openers can read as corporate. The adaptation uses the discipline (Situation, Complication named explicitly) without the fixed language.

**Adaptation notes.** No billable-time dependency. The adaptation is purely verbal: the agent's opener must establish context (Situation), name the change driving this session (Complication), and either answer or stake a hypothesis. This is a sub-second re-write of how agents open sessions; the payoff is reduced opener writing-load for the human.

**Evaluation plan.** Classify corpus opener-turns (first human turn, first agent turn) as "SCQA-complete," "SC-only," "C-only" (straight to complication, no shared situation), or "dump" (no structure). Hypothesize: SCQA-complete or SC-only openers correlate with faster time-to-first-decision in the session.

## 8. Engagement letter with out-of-scope section

**Description.** The formal contract opening a consulting engagement. The core sections per Sirion, Funding for Good, and nmsconsulting:

- **Objectives** — what the engagement is trying to accomplish.
- **Scope** — what activities and deliverables are included.
- **Out-of-scope** — explicit list of what is *not* included. Frequent examples: "tax consultations not included," "responding to direct messages not included." The list is as specific as possible.
- **Deliverables with acceptance criteria** — concrete outputs, each with a definition of done.
- **Timeline and milestones.**
- **Governance / decision rights** — who signs off on what.
- **Commercial terms.**
- **Change-order process** — how scope can be formally modified mid-engagement.

The out-of-scope section is the one most often under-specified and most often the source of downstream disputes ("scope creep"). Explicit out-of-scope naming is the prevention.

**Dimension values (planning-protocol lens).**
- Timing of alignment: pre-action, session or work-opening.
- Decision locus: interactive, ratified.
- Dialog form: structured-artifact (the letter) + confirmation.
- Question style: embedded (each section is a question the letter answers).
- Autonomy scope: bounded-by-scope (scope section is the autonomy boundary).
- Context richness: rich-brief (the letter is the brief).
- Plan expression form: structured-artifact.
- Knowledge direction: bidirectional.
- Review/critique integration: formal ratification move.

**Mechanism.** Four mechanisms:

- **Explicit out-of-scope** catches scope creep. When a topic arises later that was explicitly out-of-scope, the change-order process is triggered; if scope was only implicit, the topic is ambiguously in-scope and silently expands the work.
- **Acceptance criteria per deliverable** catches done-ness ambiguity. A deliverable with no acceptance criterion is never done because there is no "done" to test against.
- **Governance / decision rights** names who decides what. Without this, every disagreement escalates to the senior-most party.
- **Change-order process** is the bulkhead that allows scope to evolve *without* silent expansion. The mechanism is not "scope never changes"; it is "scope changes only through the process, which produces a record."

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The out-of-scope section is the highest-leverage adoption in this external source. The observed corpus rarely names explicit non-goals, and this is flagged as a failure mode in the framework's diagnostic catalog (§6.1 "fuzziness-not-resolving" and the "framing-correction depth" signal). Adding a forced "out-of-scope" section to kerf works, with substance-check by a reviewer sub-agent, directly fills this gap.
- *Pro:* Acceptance criteria per deliverable maps to the locked-decisions-guardian reviewer role — the sub-agent checks that every stated deliverable has a definition of done.
- *Pro:* Governance / decision rights has a direct analog: the decision-delegation opener (observed pattern in the corpus where the human specifies which decisions the agent should make autonomously vs. ask about). Formalizing this as a mandatory section in kerf works would make the currently-ad-hoc pattern universal.
- *Con:* Full engagement-letter ceremony is too heavy for small planning sessions. The adaptation: a one-line "out-of-scope" statement for short sessions; a structured section for finalized kerf works.
- *Con:* Over-specifying out-of-scope can *miss* things — the enumerated list implies that unlisted items are in-scope by default. The protocol needs a "catch-all" clause ("anything not explicitly in-scope requires a change order") to prevent this.

**Adaptation notes.** The engagement letter is the most billable-time-shaped of all candidates here: its existence is entirely to prevent the consulting firm from doing free work. Strip that pressure, and the contractual dimensions (commercial terms, change-order fees) fall off cleanly. What remains — out-of-scope, acceptance criteria, decision rights — are cognitive discipline moves that survive without the fee structure. The out-of-scope clause in particular is a lightweight-cheap addition: one sentence to one paragraph, non-contractual, purely for cognitive alignment.

**Evaluation plan.** Retroactively construct out-of-scope sections for five finalized kerf works, using the eventual implementation to see what *was* out-of-scope in hindsight. For each: was the out-of-scope item ever raised as an ambiguity during planning? If yes, the item was a latent out-of-scope that went unnamed and caused drift. Count how often this happens. Live trial: mandatory out-of-scope paragraph in the next three kerf works; measure whether items named as out-of-scope stay out, and whether later-raised ambiguities fall in the named out-of-scope zone.

## 9. Steering-committee periodic realignment (weekly / milestone)

**Description.** A scheduled governance meeting between the consulting team and the client's senior stakeholders, typically weekly or biweekly for high-priority projects, monthly for lower-urgency ones. Agenda is standardized: **status** against scope, **risks** surfacing, **decisions required** from the committee, **next-period plan**. The committee's authority is explicit: it can approve scope changes, reallocate priorities, or terminate the engagement. A project manager moderates; the committee rules.

Key design property: **the meeting exists whether or not there is news**. A committee that meets only when there is a problem trains the project team to hide problems until they're unavoidable. A committee that meets weekly normalizes surfacing.

**Dimension values.**
- Timing of alignment: mid-action, periodic.
- Decision locus: interactive with explicit authority structure.
- Dialog form: structured (four-part agenda).
- Question style: batched per agenda section.
- Autonomy scope: bounded-by-period (agent / team runs autonomously between committee meetings).
- Context richness: rich-brief (the status report is the brief).
- Plan expression form: structured-artifact (status report, decisions log).
- Knowledge direction: bidirectional.
- Review/critique integration: named realignment phase.

**Mechanism.** Three mechanisms:

- **Periodic, not triggered.** The unconditional schedule is the load-bearing property. It produces a rhythm of alignment rather than a failure-driven one. Protection against the "no news is good news" fallacy.
- **Named agenda.** Status / risks / decisions / next-plan — each slot has its own job. Without the structure, the meeting becomes status-only and decisions silently defer.
- **Explicit authority.** The committee can *decide*, not merely comment. This distinguishes a steering committee from a status meeting. A body that only reports is a waste of senior attention.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* For multi-week kerf works, a periodic realignment move is absent from the current practice. Sessions accumulate within the work without a structured "are we still on the right path?" check. Adding a lightweight weekly or per-pass-boundary realignment — with a fixed agenda (progress / risks / decisions-pending / next-pass-plan) — would fill this gap.
- *Pro:* Maps onto an agent-driven artifact: between sessions, the agent can produce a status report and surface pending decisions. The human then spends a fixed, small window reviewing and deciding. Authority structure is clean because the human *is* the decision authority.
- *Pro:* The "periodic, not triggered" discipline is the protection against silent drift within a multi-week kerf work. Without it, the work is reviewed only when something breaks.
- *Con:* Weekly ceremony for small works is overhead. The adaptation: realignment is triggered at kerf-pass boundaries (which are natural phase breaks) plus a time-based fallback (if no pass has advanced in N days, force a realignment).
- *Con:* A steering committee exists to mediate between factions; solo context has no factions. What survives is the *structure* (fixed agenda, explicit decisions-required list), not the adjudication role. This is less of a loss than it appears because the surfacing function is most of the value.

**Adaptation notes.** Billable-time pressure produces the *minimum* frequency (weekly is already as sparse as clients tolerate while still feeling value); removing that pressure lets the cadence drop to per-pass-boundary, which is much sparser. The agenda structure survives unchanged. Compare to the Socratic-source "Aporia as a Graceful-Stop Signal" — steering committees are the inverse: a named move to re-*align* rather than to park an unresolved question. Both are named mid-flight moves; they are complementary.

**Evaluation plan.** Audit multi-week kerf works for mid-work drift events (moments where the work's direction shifted substantively without an explicit realignment). For each drift, identify whether a periodic realignment would have caught it earlier. Live trial: impose an agent-generated status-and-decisions report at every kerf pass transition for one new work; measure whether the report surfaces items that would otherwise not have come up.

## 10. Laddering / "so what?" drill-down (analytical)

**Description.** Laddering, originating in Thomas Reynolds and Jonathan Gutman's means-end-chain theory (1988), is a qualitative-research technique for moving from surface statements toward underlying values. A respondent states an attribute; the interviewer asks "why is that important?" or "what does that mean to you?"; the respondent supplies a consequence; the interviewer re-asks; the respondent eventually surfaces a core value. The structure:

- **Attribute (A)** — stated surface feature or property ("I need the build to be fast")
- **Consequence (C)** — what the attribute produces ("so I can iterate quickly")
- **Value (V)** — the underlying reason the consequence matters ("because I can't hold the problem in my head otherwise")

The consulting variant emphasizes the **analytical** laddering (as distinct from the emotional laddering used in consumer research): consultants ladder from a stated symptom toward the root cause or underlying structural driver. The McKinsey "so what?" test is a form of this applied post-hoc to analysis: each finding must survive repeated "so what?" challenges or be cut.

**Dimension values.**
- Timing of alignment: per-decision, continuous.
- Decision locus: interactive.
- Dialog form: short-volley (2-4 turns per ladder).
- Question style: one-at-a-time, patterned ("why does that matter?" / "so what follows?").
- Autonomy scope: bounded-by-topic.
- Context richness: continuous-building.
- Plan expression form: the A-C-V chain is itself a compact artifact.
- Knowledge direction: **agent-investigative**.
- Review/critique integration: none inherent.

**Mechanism.** The mechanism is that surface statements are often wrong not because they are false but because they are *not yet load-bearing*. "I need the build to be fast" is an attribute statement; the actual decision criterion is further down the ladder. Laddering exposes the actual criterion, which frequently differs from the surface statement in ways that change the plan.

The distinction from the Socratic-source Five Whys (and the bounded cascade adaptation) is subtle: Five Whys drills down on *cause*; laddering drills down on *meaning / value*. They converge in practice but have different stopping rules — laddering stops when a core value is surfaced; Five Whys stops when a root cause is surfaced. For a planning-protocol, laddering is more appropriate when the uncertainty is about *why the user wants X*; Five Whys when the uncertainty is about *how X became a problem*.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Directly complementary to the bounded Five-Whys cascade from the Socratic-source pass. Where Five-Whys is about causal chains, laddering is about value chains. Both are candidate agent-investigative protocols; they cover different uncertainty types.
- *Pro:* The A-C-V artifact is useful as spec material. Acceptance criteria stated as A-C-V chains are more durable than as bare attributes, because the value level explains why the criterion matters and therefore when it can be relaxed.
- *Pro:* The "so what?" post-hoc application — challenging each finding in an analysis — composes well with the Pyramid Principle's requirement that every node contribute insight upward.
- *Con:* Laddering with a senior user risks reading as therapy. "Why is that important to you?" asked mechanically is insulting; asked with substance ("you've said you need X — what decision does that actually drive?") is useful. The framing matters.
- *Con:* The technique assumes the user has a coherent value structure to uncover. In early-phase fuzzy-intent sessions, the values are being constructed in the dialog, not discovered. Laddering presumes values pre-exist.

**Adaptation notes.** No billable-time dependency. Adaptation: use laddering on a *specific acceptance criterion* where the agent judges that the stated version may not be load-bearing. Bound the ladder at 2 steps (attribute → consequence → value). Produce the chain as a visible artifact the human can ratify. Do not ladder on already-justified items.

**Evaluation plan.** On corpus acceptance-criteria statements in finalized kerf works, classify each as surface-attribute or value-grounded. Hypothesize: value-grounded criteria are less likely to be revised during implementation. Live trial: agent ladders one acceptance criterion per session; measure whether the value-grounded version changes the eventual plan.

---

## Considered and partially rejected

**Full engagement-letter commercial terms.** Everything in the letter that is about money, invoicing, and payment schedule has no analog in the solo-user case. The letter's *cognitive* sections (scope, out-of-scope, deliverables with acceptance, governance) port; the *commercial* sections do not.

**Consulting pitch materials (capabilities decks, proposal cover letters).** These are pre-engagement instruments designed to win the contract. The track is investigating discovery-phase protocols, not sales. Explicitly out of scope per the prompt.

**Multi-day stakeholder-interview campaigns.** Consulting engagements interview many stakeholders over days; the output is cross-stakeholder divergence. With one user, this collapses. The interview *structure* (§4) ports; the *cross-interview comparison* does not, with the caveat that the transversal/personalized split may port re-cast as user-across-time.

**Full ATAM-adjacent quality-attribute scoring from consulting variants.** Consulting firms sometimes run stakeholder scoring on quality attributes (Airsaas steering-committee guides mention this). The structural move is already covered by the ATAM utility tree in the design-review external source; re-importing from the consulting variant adds no new dimensions.

**Weekly status-report ceremony as the full protocol.** The status report alone (without the decision-requiring meeting) is the failure mode consulting explicitly avoids: reporting without deciding. An adapted version that produces a report but never forces decisions is the anti-pattern. Adopted as §9 with the decision-requiring property intact.

**"Day-1 answer" as a *presented-to-client-on-day-1* ceremony.** In consulting, presenting the hypothesis to the client on day 1 is a relationship move — showing that the firm takes the problem seriously and has senior thinking engaged. In the solo-agent case, there is no relationship to build; the hypothesis is a cognitive instrument, not a client-facing artifact. The internal discipline (§5) ports; the ceremony does not.

**Pyramid Principle's 3-to-7-arguments rule as a hard cap.** Minto's actual rule is 3-to-7 because human short-term memory struggles with more. Adapted to agent output, where the reader is a single human scanning, the cap is softer — quality of grouping matters more than count. Adopted as a discipline (§6) without the hard numeric ceiling.

**Laddering in its therapeutic / motivational-values form.** Reynolds and Gutman's original used laddering to surface unconscious consumer values. The analytical variant in §10 is what ports; the therapeutic framing does not, for the same reason "Dumb Socratic questioning" was rejected in the Socratic-source pass — senior users experience it as condescension.

**Consulting firm-specific frameworks (7-S, Porter's Five Forces, BCG Matrix, etc.).** These are *content frameworks* (what to analyze), not *protocol frameworks* (how to interact). The track is investigating protocols. Content frameworks may usefully inform decomposition content (§1) but are not themselves candidate protocols.

---

## Patterns the user has not yet adopted (high-priority findings)

Ordered by estimated signal-to-effort ratio:

1. **Explicit out-of-scope section** (from engagement letters, §8). Fills a named gap in the observed corpus. One paragraph per kerf work; substance-checked by reviewer sub-agent. Near-zero cost; high expected drift-prevention.
2. **Closing-summary ritual at session end** (from stakeholder interviews, §4). 2-3 turns, mandatory before session close. Catches drift at the last-cheap point. Composes with the "directional clean-repetition" candidate from the Socratic sources.
3. **SCQA-shaped opener** (§7). Compact 3-5 sentence structure on agent session openers, forcing Situation + Complication + explicit-or-hypothetical Answer. Attacks context-dump openers and reduces opener writing-load.
4. **Agent-proposed MECE decomposition** (§1). Structured-artifact move at the start of a complex planning session. Agent offers the decomposition; human ratifies or corrects. Lower human writing-load; exposes framing drift earlier.
5. **Implication-question discipline in elicitation** (from SPIN, §3). An agent that has not asked at least one "if we don't fix this, what cascades?" question before proposing a solution is missing a known failure-mode probe.
6. **Periodic realignment at kerf-pass boundaries** (from steering committee, §9). Fixed agenda: status / risks / decisions-pending / next-plan. Enforces mid-work alignment without relying on the user to trigger it.
7. **Hypothesis-as-hypothesis framing for agent proposals** (§5). When the agent proposes a plan, explicitly labeling it as a hypothesis-to-falsify rather than a recommendation-to-accept shifts the reviewer sub-agent's job from "is this good?" to "what would prove this wrong?"
8. **Pyramid Principle / so-what discipline on long agent outputs** (§6). Answer-first; so-what test on every supporting paragraph. Applies selectively to outputs > 500 chars, not to ping-pong.
9. **Analytical laddering on acceptance criteria** (§10). Bounded 2-step A-C-V chain on criteria where the attribute may not be load-bearing. Produces more durable criteria.
10. **Issue-tree output for the kerf decompose pass** (§2). Explicit diagnostic or solution tree instead of prose. Reviewable structure; MECE-checkable.

Items 1-3 are likely the highest return; they address named gaps in the corpus at minimal adaptation cost. Items 4-6 require moderate adaptation but fill structural holes (decomposition, mid-work realignment, elicitation depth). Items 7-10 are refinements to existing agent output that compose with the other candidates.

---

## Cross-cutting observation

Six of the ten candidates above are agent-investigative or agent-proposes-human-rules on the knowledge-direction axis. This matches the Socratic-source observation: the under-explored region of the corpus is where the agent takes structural initiative, not where the agent waits for the human to supply structure. Consulting has particularly strong traditions here because consultants *must* take initiative — the client has hired them to. The LLM-agent case inherits the permission.

The defining consulting adaptation concern, distinct from the Socratic-source senior-responder worry, is the **billable-time strip**. Consulting protocols are compact because time is money; an LLM agent is not time-constrained, which means some of the compactness can be traded for iteration. Specifically: Day-1 hypothesis becomes Hour-1 hypothesis and is revised within the session rather than across weeks; weekly steering committee becomes per-pass realignment; one-shot engagement-letter scope becomes revisable scope with change-order discipline. The *structure* survives; the *cadence* relaxes.

A second cross-cutting observation: the most transferable consulting moves are the **output-structure moves** (Pyramid Principle, SCQA, MECE, engagement-letter sections) rather than the **question-style moves** (SPIN, laddering, interview protocols). Output structure transfers almost without adaptation because agents produce output and humans consume it — the cognitive-load arguments are invariant to the questioner/responder polarity. Question-style moves require more adaptation because they were designed for human-to-human interviewing dynamics that partially do not apply to agent-to-human dialog.

## Primary sources cited

- [MECE Principle (Wikipedia)](https://en.wikipedia.org/wiki/MECE_principle)
- [MECE Framework Explained (StrategyU)](https://strategyu.co/wtf-is-mece-mutually-exclusive-collectively-exhaustive/)
- [Issue Tree (Wikipedia)](https://en.wikipedia.org/wiki/Issue_tree)
- [Issue Tree Guide (Crafting Cases)](https://www.craftingcases.com/issue-tree-guide/)
- [Issue Tree Framework (Analyst Academy)](https://www.theanalystacademy.com/issue-tree-and-logic-tree-framework/)
- [SPIN Selling, Neil Rackham (Amazon)](https://www.amazon.com/SPIN-Selling-Neil-Rackham/dp/0070511136)
- [SPIN Selling excerpt PDF](https://edu.eccceg.com/wp-content/uploads/webinars/Sales%20skills%20based%20on%20spin%20and%20challenger%20methodology/resources-spinselling.pdf)
- [The 4 Steps to SPIN Selling (Lucidchart)](https://www.lucidchart.com/blog/the-4-steps-to-spin-selling)
- [Stakeholder Interviews 101 (Nielsen Norman Group)](https://www.nngroup.com/articles/stakeholder-interviews/)
- [How to structure stakeholder interviews (Codurance)](https://www.codurance.com/publications/how-to-structure-stakeholder-interviews-and-set-your-product-discovery-off-right)
- [Stakeholder Interview Guide (TestingTime)](https://www.testingtime.com/en/blog/stakeholder-interview-guide/)
- [Conducting Stakeholder Interviews: 12 Golden Rules (think.design)](https://think.design/blog/conducting-stakeholder-interviews-12-golden-rules/)
- [Flawless Consulting: A Guide to Getting Your Expertise Used, Peter Block (O'Reilly)](https://www.oreilly.com/library/view/peter-block-flawless/9780787948030/)
- [Flawless Consulting Contracting Overview (O'Reilly)](https://www.oreilly.com/library/view/peter-block-flawless/9780787948030/ch04.html)
- [Flawless Consulting summary (Consulting Success)](https://www.consultingsuccess.com/flawless-consulting)
- [How McKinsey uses Hypotheses (Stratechi)](https://www.stratechi.com/hypotheses/)
- [Hypothesis-Driven Approach (MyConsultingOffer)](https://www.myconsultingoffer.org/case-study-interview-prep/hypothesis-driven-approach/)
- [The McKinsey Way, Ethan Rasiel (PDF)](https://cdn.bookey.app/files/pdf/book/en/the-mckinsey-way.pdf)
- [Mastering Hypothesis-Driven Problem Solving (Spencer Tom)](https://www.spencertom.com/2026/03/14/inner-game-mastering-hypothesis-driven-problem-solving)
- [Ghost Decks explained (Medium / Jay)](https://medium.com/jong-park/how-to-apply-ghost-aka-shell-and-skeleton-decks-7692a942c802)
- [McKinsey Presentations: Ghost Decks (Working With McKinsey)](http://workingwithmckinsey.blogspot.com/2013/07/McKinsey-presentations-ghost-decks.html)
- [The Pyramid Principle: Logic in Writing and Thinking, Barbara Minto (Amazon)](https://www.amazon.com/Pyramid-Principle-Logic-Writing-Thinking/dp/0273710516)
- [Barbara Minto official site](https://www.barbaraminto.com/)
- [The Minto Pyramid Principle Explained (BetterUp)](https://www.betterup.com/blog/minto-pyramid)
- [The Pyramid Principle Applied (Management Consulted)](https://managementconsulted.com/pyramid-principle/)
- [Minto Pyramid & SCQA (ModelThinkers)](https://modelthinkers.com/mental-model/minto-pyramid-scqa)
- [SCQA Framework (Management Consulted)](https://managementconsulted.com/scqa-framework/)
- [SCQA Framework Explained (antonov.com.au)](https://antonov.com.au/scqa-framework)
- [Consulting Scope of Work Template (Funding for Good)](https://fundingforgood.org/consulting-scope-of-work-contract/)
- [Consulting SOW Template (nmsconsulting)](https://nmsconsulting.com/consulting-sow-template/)
- [The Engagement Letter: The Blueprint That Protects Every Professional Relationship (Sirion)](https://www.sirion.ai/library/contract-management/engagement-letter/)
- [Ultimate Guide to Scoping (StrategyU)](https://strategyu.co/scoping-in-consulting/)
- [What Is Out Of Scope Work? (Ignition)](https://www.ignitionapp.com/blog/what-is-out-of-scope-work-and-how-to-avoid-it)
- [Best practices for project steering committees (AirSaaS)](https://www.airsaas.io/en/project-management/project-steering-committee)
- [Steering Committee: A Strategic Guide (Boardwise)](https://www.boardwise.io/en/blog/steering-committee-a-strategic-guide-for-corporate-leaders)
- [Steering Committees Governance & Strategic Oversight (Adapt Consulting)](https://www.adaptconsultingcompany.com/2025/03/16/post-3-4-steering-committees-governance-and-strategic-oversight/)
- [Laddering Questions: Drilling Down (Interaction Design Foundation)](https://ixdf.org/literature/article/laddering-questions-drilling-down-deep-and-moving-sideways-in-ux-research)
- [Ladder Interview (Wikipedia)](https://en.wikipedia.org/wiki/Ladder_interview)
- [Laddering: Uncovering Core Values (UXmatters)](https://www.uxmatters.com/mt/archives/2009/07/laddering-a-research-interview-technique-for-uncovering-core-values.php)
- [Laddering Techniques in Qualitative Research (Murphy Research)](https://murphyresearch.com/laddering-techniques-qualitative-research/)
