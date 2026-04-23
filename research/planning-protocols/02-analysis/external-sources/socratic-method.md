# External source: Socratic method and question-driven elicitation

> Phase 2, Step 2 external-source pass. Domain: Socratic method (elenchus, dialectic, maieutics, Socratic seminar, question taxonomies, Five Whys, adjacent clean-language / coaching traditions).
>
> Target reader: a researcher building a unified candidate catalog of planning protocols. This document is **candidate-generating**, not prescriptive. External evidence proposes; observed-corpus evidence disposes.

## Why this domain

The harmonik corpus shows a dominant shape in which the **human** supplies context, intent, and constraints, and the **agent** responds. Phase 1 flagged "agent-investigative protocols" — where the agent actively interrogates the human to build a model of intent — as an under-explored region. Socratic method is the deepest well in the Western tradition on question-driven elicitation; it inverts the observed pattern and so is a structural counterweight to anchoring on the corpus.

Socratic method is not one thing. It is a family: the refutative elenchus, Platonic dialectic, maieutic ("midwife") questioning, and modern classroom derivatives. Adjacent traditions — Graesser's question taxonomy, Paul-Elder critical-thinking question frames, Bloom-level question stems, Toyota's Five Whys, David Grove's Clean Language — formalize parts of what Socrates did tacitly. Each one yields at least one candidate.

## Role-inversion caveat (applies to every candidate below)

Classical Socratic method is premised on an **expert questioner and a novice (or pretender-to-expertise) responder**. In our context the polarity is reversed in two ways at once:

1. The **agent** is the questioner. The agent is typically less domain-expert than the human it is questioning.
2. The **human** is a senior solo developer. They can tell when questioning is performative, circular, or fishing for something the agent already privately decided.

A naive port of Socratic method ("agent pretends not to know and asks leading questions until the human arrives at agent's pre-held answer") produces the pathology the Boston College Law School critique describes as "public humiliation" — except now it's the senior being toyed with by a junior. The adaptations below are chosen to avoid that failure mode: the agent must either (a) ask questions whose answer it genuinely does not know, or (b) be transparent about what it is probing and why.

## Elenctic Probe

**One-line**: Agent takes a stated assumption, derives a consequence from it, and asks the human whether they accept the consequence — probing for contradiction between stated intent and implied commitments.

**Mechanism (source theory)**: In Plato's early dialogues, Socrates accepts an interlocutor's thesis, draws further admissions that seem individually obvious, then shows the admissions are jointly inconsistent with the thesis. The respondent is forced to either refine, retract, or re-ground the thesis. The power is in the *secondary questions feeling inescapable while the primary thesis felt certain* — the contradiction surfaces without the questioner having to assert a counter-position.

**Dimension values**:

- Timing of alignment: pre-action (catches misalignment before code is written)
- Decision locus: interactive — the agent surfaces, the human decides
- Dialog form: short-volley, one question at a time
- Question style: one-at-a-time, forced-choice or narrow-open
- Autonomy scope: bounded-by-category — agent runs the probe within a stated topic
- Context richness: minimal-dispatch (the probe itself carries the context)
- Plan expression form: dialog-log (the probe leaves a trace of refined commitments)
- Knowledge direction: **agent-investigative**
- Review/critique integration: none (it *is* a form of review)

**Predicted trade-offs**:

- *Plus*: directly attacks the misaligned-assumption pain point. Every probe-cycle terminates either with a strengthened commitment or a corrected drift — both are wins.
- *Plus*: the human does not have to author a position first; they only have to answer yes/no-ish. This should reduce human writing load.
- *Minus*: with a senior responder, repetitive or shallow elenchus reads as the agent stalling. The probes have to land on a genuine derivation the human had not traced themselves.
- *Minus*: the method is adversarial in posture. In the corpus, the user has flagged friction when agents push back on stated decisions. An elenctic probe framed as "are you sure?" triggers defensiveness; framed as "here is the consequence I'd draw — do you also draw it?" invites partnership.
- *Minus*: in a fast-paced planning session, short-volley doubles turn count. Needs to be budgeted.

**Adaptation (questioner = LLM)**:

- The agent must make the derivation explicit ("If we adopt X, then Y follows. Do you accept Y?"). Socrates could leave the derivation implicit because both parties were live humans; the agent cannot hide its reasoning without seeming sneaky.
- The agent should only probe when it has found a *non-trivial* derivation. Probing trivialities is the corpus-observed "deferring trivial decisions" failure mode wearing philosophical clothing.
- Termination rule: the agent stops probing a thesis when three consecutive derivations are accepted without revision, or when the human signals closure.

**Evaluation plan**:

- Take corpus sessions flagged as "misaligned-assumption." Identify the turn where drift first appeared. Simulate: could an elenctic probe earlier have surfaced the drift?
- Count probe-cycles to resolution in a short live trial. Target: fewer total turns than the status-quo clarification pattern.
- Survey the human after live trials: "did the probe feel substantive, or like the agent was stalling?"

## Maieutic Draw-Out

**One-line**: Agent assumes the human already holds a latent position and asks questions whose purpose is to make the latent position explicit — *without* contributing content of its own until the position is on the page.

**Mechanism (source theory)**: Socrates' midwife metaphor in the *Theaetetus*: "I am barren of wisdom. I only help you give birth to what you already hold." Maieutic questions do not introduce new concepts; they ask the responder to elaborate, compare, or locate their own thought more precisely. Grove's Clean Language is a modern operationalization: repeat the speaker's words verbatim, ask only questions that add no new metaphor or frame ("And what kind of X is that X?" "Is there anything else about that X?"), so the speaker's own structure emerges undistorted.

**Dimension values**:

- Timing of alignment: pre-action, often early-phase
- Decision locus: human-preempted — the agent is deliberately not deciding, by design
- Dialog form: short-volley, many small exchanges
- Question style: one-at-a-time, open-ended, minimal-interference
- Autonomy scope: none (the agent is in draw-out mode, not decision mode)
- Context richness: continuous-building — the point is that context accretes
- Plan expression form: dialog-log → prose (the draw-out produces raw material the agent can later restate)
- Knowledge direction: **agent-investigative**, explicitly
- Review/critique integration: none

**Predicted trade-offs**:

- *Plus*: directly attacks the writing-load pain point. The human answers short questions rather than authoring paragraphs. Transcript volume shifts from human-authored to agent-elicited.
- *Plus*: because the agent introduces no content, there is no drift to correct later. The content is the human's from the start.
- *Minus*: senior user + junior LLM + relentless open questions = insulting. "And what kind of system is that system?" reads as a failure of comprehension when the agent should have been able to form a working model from the first description.
- *Minus*: maieutic inquiry in its pure form is *slow*. It is designed for psychotherapy and deep pedagogy, not for someone who has 20 minutes before a meeting.
- *Minus*: risks narrowing prematurely by following the human's initial framing too closely (the inverse of the usual agent-drift problem).

**Adaptation (questioner = LLM)**:

- Hybrid rather than pure: the agent uses maieutic questioning only for the *one or two nodes* where it judges its internal model to be weakest. The rest of the planning uses normal interaction.
- The agent should state the branch on which it is drawing out: "I want to make sure I have your picture of the failure mode before I propose anything — can I ask two clarifying questions?" This consents the human to the mode.
- Preserve Grove's verbatim-repetition discipline: when the agent later restates the human's picture, it uses the human's own nouns and verbs, not paraphrases. Paraphrase is where drift enters.

**Evaluation plan**:

- Corpus search for sessions where the human wrote a long clarifying paragraph in response to an agent misunderstanding. Hypothesize: could a short maieutic question earlier have prevented the long paragraph?
- Live trial: for one node per session, agent enters maieutic mode. Measure human character-count in that node vs. comparable nodes in same-session non-maieutic interaction.
- Check for the insulting-the-senior failure: does the human report the questions felt "obvious" or "wasted"? If yes, maieutic mode was triggered on a node that did not need it.

## Reduced Dialectic (position → counter → synthesis)

**One-line**: Agent lays out the position under consideration, lays out its strongest counter, and asks the human to rule on the synthesis — a three-move exchange rather than a free-form dialogue.

**Mechanism (source theory)**: Plato's dialectic, in its structural core, is a movement from a stated position to its contradiction and thence to a reconciled understanding. The Hegel-associated thesis/antithesis/synthesis framing post-dates Plato, but the structural pattern ("surface a tension, resolve it") is already in the middle dialogues. The core claim is that *a position held without its counter is held shallowly*; the counter is not decoration, it is the stress test.

**Dimension values**:

- Timing of alignment: pre-action
- Decision locus: mixed — agent drafts the triad, human rules on synthesis
- Dialog form: structured (three named moves)
- Question style: batched, with the human's response being the decisive one
- Autonomy scope: bounded-by-category — agent autonomous within the triad, human decides between the branches
- Context richness: rich-brief (the triad is a compact summary of the decision-space)
- Plan expression form: structured (three named slots)
- Knowledge direction: bidirectional (agent supplies alternatives, human supplies ruling)
- Review/critique integration: sub-agent-reviewer fits naturally (reviewer challenges the synthesis)

**Predicted trade-offs**:

- *Plus*: forces the agent to generate the counter-position rather than only advocating for its first answer. This attacks a known agent failure mode where "decisiveness" decays into confirmation bias.
- *Plus*: compact artifact. A position-counter-synthesis triad is a spec-shaped object — it maps cleanly to a design-decision record.
- *Plus*: the human decision is one-shot ("yes to synthesis A" / "no, re-do"), which is low writing-load.
- *Minus*: heavyweight for small decisions. Running a full triad on "should this helper take a config dict" is ceremony for its own sake.
- *Minus*: the quality hinges on the counter being a real counter. Agents are prone to straw-counter ("some might argue" followed by an obvious rebuttal). This pathology ships the appearance of dialectic without the substance.

**Adaptation (questioner = LLM)**:

- Reserve for decisions the agent pre-labels as *consequential* (cross-cutting, hard to reverse, or where the agent is uncertain). Triaging which decisions warrant a triad is itself a protocol question.
- The counter must be authored by a *different* agent role than the one authoring the position. Same-head counter is the straw-counter trap. A reviewer sub-agent can play this role.
- The synthesis is the agent's best guess; the human's job is to ratify, amend, or reject. Not to author synthesis from scratch.

**Evaluation plan**:

- Pick 5 consequential decisions from finished plans in the corpus. Retrospectively generate a triad for each. Score: (a) is the counter substantive? (b) would the triad have surfaced anything missed in the original session?
- Live trial on one consequential decision per session. Measure whether the synthesis is accepted, amended, or rejected. Amendment-rate is the useful signal (full acceptance suggests the counter was weak; full rejection suggests the agent mis-framed the position).

## Socratic Seminar Turn Frame

**One-line**: A three-phase exchange — opening / follow-up / closing — with explicit question types for each phase, imported from classroom Socratic seminar practice.

**Mechanism (source theory)**: Modern Socratic seminar protocols (Facing History, Paideia, AVID) have formalized the open-endedness of Socratic questioning into a protocol with stages. The *opening* question is always open, text-grounded, and cannot be answered yes/no. *Follow-up* questions elaborate or challenge responses without resolving them. *Closing* questions ask the responder to state what they now believe. The formal structure is load-bearing: it disciplines the facilitator away from prosecuting a hidden answer, and it gives the responder a predictable shape.

**Dimension values**:

- Timing of alignment: continuous, per-topic
- Decision locus: interactive
- Dialog form: structured (three-phase), but the moves within each phase are short-volley
- Question style: open-ended at open and close, narrowing only in follow-up
- Autonomy scope: bounded-by-category (the topic under discussion)
- Context richness: continuous-building
- Plan expression form: dialog-log, optionally synthesized to prose at close
- Knowledge direction: agent-investigative in opening, bidirectional in follow-up, human-dominant at closing
- Review/critique integration: human-self-review (the closing move *is* self-review)

**Predicted trade-offs**:

- *Plus*: the closing move is an explicit "state what you now believe" — this is the moment the session produces a commit-able artifact. Many corpus sessions drift to an end without this ritual.
- *Plus*: the phase labels let the human know which mode the agent is in, reducing the "what does this question mean?" meta-ambiguity.
- *Minus*: phase rigidity can feel performative in a live planning session. The opening-follow-up-closing arc is natural for a 45-minute classroom slot; for a 3-turn design decision it is overhead.
- *Minus*: the classroom protocol assumes a *group* of responders; with one human responder, the follow-up phase can feel like cross-examination rather than group inquiry.

**Adaptation (questioner = LLM)**:

- The "group" in single-responder adaptation can be the human plus named agent-roles (critic, historian, implementer). The agent moderates among its own roles and the human, which pluralizes the follow-up phase without needing multiple humans.
- Opening questions should be machine-generated from the spec / prior state, not fished live. This is the easiest phase for the agent to get wrong ("what do you think about X?" rather than "which of [A, B] does the current architecture make harder, and why?").
- Closing question is non-negotiable and must produce a written artifact.

**Evaluation plan**:

- Take kerf-pass transitions (problem-space → decomposition → research). Each transition is a natural closing. Audit: did the session produce a closing-ritual artifact, or did it drift?
- Live trial: impose the three-phase structure on one planning topic per session. Measure artifact quality at close vs. uninstructed-close baseline.

## Question-Level Targeting (Bloom / Graesser / Paul-Elder)

**One-line**: Agent classifies the question it is about to ask by cognitive level (remember/understand/apply/analyze/evaluate/create) and intellectual standard (clarity/accuracy/precision/relevance/depth/breadth/logic) *before* asking it — used as a filter against low-value questions.

**Mechanism (source theory)**:

- **Bloom's revised taxonomy** (Anderson & Krathwohl): six cognitive levels, from remember (recall) to create (generate original). Each level has canonical question stems.
- **Graesser & Person's taxonomy** (1994, 16 categories): verification, disjunctive, concept-completion, feature-specification, quantification, definitional, example, comparison, interpretation, causal-antecedent, causal-consequence, goal-orientation, instrumental, expectational, judgmental, assertion. Categories 10–15 correlate with Bloom's deep-cognition end.
- **Paul-Elder intellectual standards**: seven yardsticks (clarity, accuracy, precision, relevance, depth, breadth, logic) with canonical question stems for each ("could you elaborate on clarity?" "how can we verify accuracy?").

The shared claim is that questions have *types*, types are not interchangeable, and most of the bad questioning in practice consists of asking shallow-type questions when the situation called for deep-type ones (or the reverse).

**Dimension values** (meta-protocol; applies on top of other candidates):

- Timing of alignment: applied at each question
- Decision locus: agent-autonomous (the classification)
- Dialog form: unchanged; affects *which* questions are asked
- Question style: disciplined one-at-a-time
- Autonomy scope: unchanged
- Context richness: unchanged
- Plan expression form: unchanged
- Knowledge direction: unchanged
- Review/critique integration: self-review on the agent's side ("is this a level-1 question disguised as level-4?")

**Predicted trade-offs**:

- *Plus*: directly attacks "agent deferring trivial decisions." A Bloom-level-1 "remember" question asked of a senior developer is exactly the pattern the corpus complains about; flagging it pre-utterance lets the agent suppress or self-answer.
- *Plus*: lets the agent budget question-depth. Instead of 12 shallow questions, ask 3 deep ones.
- *Plus*: Paul-Elder's intellectual standards give the agent a second axis — is this question probing clarity, depth, or relevance? — which is orthogonal to cognitive level.
- *Minus*: pre-classification has its own cost. If every question drags a 200-token self-critique behind it, the agent is slower overall.
- *Minus*: taxonomies are academic artifacts. Agents may classify to satisfy the taxonomy rather than the underlying purpose (label-pleasing).

**Adaptation (questioner = LLM)**:

- The taxonomy is used implicitly, not surfaced. The agent does not tell the human "this is a level-4 analyze question"; it uses the classification to suppress shallow questions and re-draft them.
- Simplest form: a two-bucket filter. Before asking, the agent checks: "is this a question I could answer from information already on the page?" (level-1/2 → suppress, self-answer). "Is this a question whose answer will change what gets built?" (level-4/5 → ask).
- Graesser's 16 categories are too granular for live use. Fold to ~5 clusters: verification / causal / judgmental / goal / comparison.

**Evaluation plan**:

- Transcript audit: classify a sample of agent-to-human questions in the corpus by level. Hypothesis: a large fraction of "bad" questions (ones the user called trivial or deferrable) cluster at levels 1–2.
- Live trial: agent runs the two-bucket filter. Measure question-count drop and whether dropped questions were in fact trivial (by asking the human for a judgment on a subset).

## Five-Whys Cascade (bounded)

**One-line**: Agent, on hearing a stated reason, asks "why is that so?" up to a bounded number of times — driving from a surface reason toward the structural reason — then stops.

**Mechanism (source theory)**: Taiichi Ohno's Toyota Production System technique: iterate "why?" to expose the underlying cause. The discipline is the iteration plus the stopping rule ("five" is a heuristic, not a law). Criticism from within Toyota itself (Minoura): the method is too shallow, it assumes a linear cause-chain, and different investigators reach different root causes from the same symptom.

**Dimension values**:

- Timing of alignment: pre-action on a specific decision
- Decision locus: interactive
- Dialog form: short-volley, predictable
- Question style: one-at-a-time, always the same shape ("why?")
- Autonomy scope: bounded-by-category
- Context richness: minimal-dispatch
- Plan expression form: dialog-log, producing a chain artifact
- Knowledge direction: **agent-investigative**
- Review/critique integration: none built-in

**Predicted trade-offs**:

- *Plus*: very low cognitive overhead. Both parties know the shape of the exchange.
- *Plus*: the chain artifact it produces is directly useful as a "decision rationale" in the spec.
- *Minus*: the Toyota critique applies here unmodified. Repeated "why?" is mechanical; it lacks the branching that a real causal investigation needs. The senior-developer responder will experience "why is that?" four times in a row as the agent stalling for context it should have built.
- *Minus*: five is too many. In planning (as opposed to incident post-mortem) two or three usually suffices.
- *Minus*: "why?" is the most generic question-word. Graesser/Paul-Elder would say the Five Whys cascade is really a causal-antecedent cascade, and substituting "what condition produced this?" or "what earlier choice made this necessary?" sharpens the probe.

**Adaptation (questioner = LLM)**:

- Bound at 2–3 iterations. Replace the literal "why?" with the specific causal-antecedent form appropriate to the answer. The discipline carries; the word does not.
- Use only on a decision whose rationale has not yet been written. Do not apply to a decision the human has already justified; asking "why?" on a pre-justified decision is the "why are you sure?" pathology.
- End-of-cascade ritual: the agent writes back the inferred root cause and asks for ratification. This converts the cascade to a spec artifact.

**Evaluation plan**:

- Retrospective: find a corpus decision whose rationale in the eventual spec is shallow. Was there a point in the session where a bounded cascade would have gone deeper?
- Live trial on a new consequential decision. Measure whether the root-cause artifact survives the human's ratification pass unamended.

## Aporia as a Graceful-Stop Signal

**One-line**: A named protocol move: when the agent or the human concludes "we are puzzled here, we cannot resolve this now," that state is flagged, parked, and the session advances — rather than spinning.

**Mechanism (source theory)**: Aporia in early-Plato dialogues is the *endpoint* of many sessions: not resolution but productive impasse. Scholarship is split on whether Plato saw aporia as final or as a waystation; for our purposes, treating it as a valid closing-state is the useful move. The discipline is to *name* the impasse, distinguish it from a failure, and route on.

**Dimension values** (meta-protocol):

- Timing of alignment: applies at any pass boundary
- Decision locus: mixed — either party can call aporia
- Dialog form: a single named move
- Question style: n/a (it's a closing move)
- Autonomy scope: unchanged
- Context richness: unchanged
- Plan expression form: structured (an aporia-artifact: what was asked, what is missing, what would resolve it)
- Knowledge direction: bidirectional
- Review/critique integration: the aporia-artifact is a hand-off to a reviewer or a later session

**Predicted trade-offs**:

- *Plus*: resolves a corpus pain point where sessions grind on a question no amount of present-session discussion will resolve. Naming aporia lets the session advance and queues the question for the right moment (more data, a sub-agent, a spike, tomorrow).
- *Plus*: produces a valuable artifact (the impasse statement) that future sessions can pick up.
- *Minus*: risk of overuse as an escape hatch from hard thinking. If the agent calls aporia every time a question is non-trivial, the session never makes decisions.
- *Minus*: requires an honest diagnosis of "is this unresolvable now, or are we just tired?" Neither party may be in a position to tell.

**Adaptation (questioner = LLM)**:

- Aporia-move must state (a) the question, (b) what was ruled out, (c) what would unblock, (d) when to revisit.
- Quota: at most one aporia per session-pass, or it becomes the default.
- Aporia on a *decision* (must-pick-now) is illegal; aporia on a *consideration* (can-defer) is legal.

**Evaluation plan**:

- Corpus audit: find sessions where a question went round for N+ turns without resolution. Would a formal aporia-move have freed capacity earlier?
- Live trial: allow one aporia per session; measure session-end satisfaction and whether the parked question resurfaced productively later.

## Directional Clean-Repetition

**One-line**: Before proposing anything, the agent repeats back the human's own words verbatim (not paraphrased), then asks one targeted question.

**Mechanism (source theory)**: From Grove's Clean Language: paraphrase introduces the questioner's metaphor and redefines the speaker's experience, even when unintentional. Verbatim repetition gives the speaker's structure priority. In Grove's clinical context, this is the difference between therapist-led and client-led inquiry.

**Dimension values** (meta-protocol; composes with other candidates):

- Timing of alignment: pre each agent question or proposal
- Decision locus: unchanged
- Dialog form: prepends a short restatement to each agent turn
- Question style: forces the restatement to be lexically faithful
- Autonomy scope: unchanged
- Context richness: continuous-building (the corpus of verbatim-preserved phrases becomes load-bearing)
- Plan expression form: unchanged, but the spec inherits the human's nouns rather than the agent's
- Knowledge direction: shifts weight toward agent-investigative / human-dominant
- Review/critique integration: none

**Predicted trade-offs**:

- *Plus*: directly counters a corpus-observed failure mode where the agent restates the user's intent using its own vocabulary and then implements against the restated intent. The drift enters at paraphrase.
- *Plus*: the user can detect mis-hearing on the spot, rather than downstream.
- *Minus*: verbatim repetition reads as slightly stilted. The trick is to use it only on load-bearing phrases (decision terms, requirement verbs, quantifiers) rather than everything.
- *Minus*: clean-language purity is for therapy; the agent does have its own vocabulary (for good reason) and must sometimes introduce it. The protocol is about which *moments* demand cleanness, not about never introducing new words.

**Adaptation (questioner = LLM)**:

- The rule: on decision moments (where the human named something), the agent keeps the human's nouns. On explanatory moments (agent introducing an idea), the agent uses its own vocabulary.
- Surface-test: if the eventual spec contains terms the human never used for concepts the human did introduce, the agent paraphrased. This is auditable.

**Evaluation plan**:

- Corpus audit: trace a term in a finished spec back to its first appearance. If the term appears first in agent text for a concept the human introduced, log a paraphrase event. Hypothesize paraphrase-events correlate with downstream misalignments.
- Live trial: agent operates with verbatim-on-decision-moments rule. Measure drift-correction frequency.

## Considered and set aside

**Full Hegelian dialectic (thesis/antithesis/synthesis as a *world-historical* movement)**. The schema is powerful at book-length; at the scale of a planning session, the Reduced Dialectic above captures the useful structural move (counter-position generation) without the metaphysical machinery. Including the full form is costume.

**Pure maieutics (as practiced by Socrates in the *Theaetetus*)**. The "I am barren; only you can give birth" stance is a bad posture for an LLM agent that *does* hold working models — adopting it requires the agent to hide its knowledge, which is a performative lie the senior user will detect. The Maieutic Draw-Out above keeps the verbatim-faithful, content-free-question technique but drops the stance.

**Rhetorical aporia (as a literary device, not a protocol)**. Plato-the-author sometimes uses aporia for effect — ending a dialogue unresolved to provoke the reader. This is outside our use. Aporia as a Graceful-Stop above is the procedural version.

**Exhaustive Graesser 16-category classification in the live loop**. The classification itself is valuable as a research lens and as a taxonomy underlying the two-bucket filter, but asking the agent to tag each question with one of 16 categories mid-conversation is ceremony the interaction cannot afford. It is a design-time taxonomy, not a runtime one.

**Five Whys verbatim (literal 5 iterations of "why?")**. The Toyota-internal critique (Minoura) and the literature on linear-cause-chain assumption say the literal form is brittle even in its home domain. The bounded, shape-substituted version above retains the discipline and drops the ritual.

**Classroom Socratic seminar as a *multi-participant* format**. Without a group of human responders the protocol loses half of what it is. The Turn Frame candidate above keeps the three-phase discipline and drops the group assumption by pluralizing with agent-roles.

**"Dumb" Socratic questioning** (agent pretends ignorance to lead human to a pre-held answer). This is the corpus-warned failure mode. External sources (Philosophy Institute, Psychology Today on Socratic ethics) converge on this being the mode most prone to producing defensiveness, shame, and adversarial feel. Explicitly rejected for adaptation.

## Cross-cutting observation

Six of the eight candidates above are at least partly **agent-investigative** on the knowledge-direction axis. This is the Phase-1-flagged unexplored region, and the Socratic tradition is where the most-developed machinery for it lives. The candidates differ on *how much* the agent probes vs. supplies, and on *when* the probe is appropriate — but the set collectively populates a region of the design space that empirical-corpus candidates (by construction) cannot reach, because the corpus predates the practice.

The senior-responder inversion is the single largest adaptation concern. Every candidate here has at least one failure mode of the form "this feels like a junior wasting a senior's time." The adaptations above are chosen against that risk; the evaluation plans are chosen to detect it.

## Primary sources cited

- Plato, *Theaetetus* (maieutics / midwife metaphor); *Meno*, *Euthyphro*, *Laches* (elenctic form); *Republic* Book VII (dialectic).
- Anderson, L. W. & Krathwohl, D. R. (2001). *A Taxonomy for Learning, Teaching, and Assessing: A Revision of Bloom's Taxonomy.*
- Graesser, A. C. & Person, N. K. (1994). "Question asking during tutoring." *American Educational Research Journal.* (16-category taxonomy.)
- Paul, R. & Elder, L. (2006). *The Miniature Guide to Critical Thinking: Concepts & Tools.* (Intellectual standards.)
- Ohno, T. *Toyota Production System.* (Five Whys.) Minoura, T. (critique within Toyota.)
- Grove, D. (1990s, via Sullivan & Rees, 2008, *Clean Language: Revealing Metaphors and Opening Minds*). (Verbatim-repetition discipline.)
- Stanford Encyclopedia of Philosophy, "Plato on Knowledge in the Theaetetus" (for the midwifery / aporia scholarship).
- Facing History / Paideia / AVID Socratic seminar protocols (modern formal classroom derivatives).
- LLMREI (arXiv 2507.02564): empirical evidence that LLM-as-interviewer captures ~70% of requirements with structured prompting; fails on non-verbal cues and hallucinates boundaries. Relevant as a sanity-check that role-inversion is viable but imperfect.
- Chang, E. Y. (arXiv 2303.08769): prompting LLMs with the Socratic method; SocraticAI / SocraSynth role-play patterns.
