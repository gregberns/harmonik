# External Source Analysis -- Pair Programming

**Phase 2, Step 2 artifact.** Author: sub-agent, 2026-04-23. Scope: extract candidate planning protocols from pair-programming literature and practice, map each onto the dimensions of variation in `research/planning-protocols/phases/phase-2/analysis/evaluation-criteria-refinement.md`, and note how the mapping survives or breaks when one party is an LLM-agent rather than a human.

## Domain orientation

Pair programming is a cooperative work arrangement where two developers collaborate on one unit of work at one workstation. It is of interest to the planning-protocols track because the *coordination primitives* it has evolved over ~25 years are reusable shapes for any situation where one party is producing artifact-bearing output and another party is maintaining alignment in parallel. Classical accounts split responsibilities by role (driver/navigator); empirical studies (Plonka & Sharp 2014; Freudenberg 2013) show that the role-labels understate what actually happens -- the pair functions as a "cognitive tag team" with frequent, fluid switching to redistribute cognitive load. This gap between the prescriptive model and the observed dynamic is itself an interesting lens for planning-protocol design: the protocol the practitioners think they're following isn't always the protocol that's generating the value.

Four load-bearing empirical findings anchor the domain:

1. **Defect / quality effect.** Cockburn & Williams (2000) reported pairs pass 86-94% of test cases vs 73-78% for solo developers, at ~15% time cost, driven by real-time error catching (the "gripping immediacy" of an always-present reviewer).
2. **Role asymmetry is smaller than the model claims.** Freudenberg (2013) found both partners contribute new information equally across subtasks, and both talk at many abstraction levels -- the cognitive labor is not cleanly split by role.
3. **Switching is for cognitive-load redistribution, not pedagogical rotation.** Fluid, frequent, unannounced switches are the norm among high-functioning pairs; they serve to relieve the typing-plus-narrating multitask burden on whoever is currently driving.
4. **Knowledge transfer uses modelling + coaching but NOT articulation/reflection/exploration.** Plonka, Sharp, van der Linden & Dittrich (2015) find six teaching strategies in play (direct instruction through subtle hints), but the novice-side cognitive-apprenticeship moves (novice articulates their understanding, reflects on divergence from expert, explores alternatives) are systematically absent. This is a *gap* the planning-protocols track should flag, because it is recoverable in an agent setting by prompt design.

All protocols below are drawn from within this domain, mapped onto the planning-protocol dimensions, and evaluated for adaptation to a senior-solo-developer-with-LLM-agent context.

---

## Classical Driver-Navigator

**One-line description.** One partner (driver) holds the keyboard and makes tactical, line-level decisions; the other (navigator) watches in real time, thinking strategically, catching errors, and steering the larger direction. Roles switch periodically (classical recommendation: 15-30 min).

### Dimension values

- **Timing of alignment:** continuous (navigator is reviewing every keystroke).
- **Decision locus:** mixed -- tactical decisions are driver-autonomous within the navigator's strategic frame; strategic decisions are interactive.
- **Dialog form:** short-volley, hybrid (bursts of dialog around decision points; silence during flow).
- **Question style:** embedded, one-at-a-time (questions arise as the code is written).
- **Autonomy scope:** bounded-by-category -- driver has autonomy over syntax and micro-edits; navigator interrupts on strategic drift.
- **Context richness:** continuous-building -- shared mental model constructed keystroke by keystroke.
- **Branching/topic handling:** linear with human-tracked-implicit side-threads (navigator holds "come back to this" items mentally).
- **Plan expression form:** hybrid -- prose dialog plus the code-in-progress itself.
- **Knowledge direction:** bidirectional, often with a skew set by expertise asymmetry.
- **Review/critique integration:** continuous -- review is not a separate phase, it is the navigator's primary job.

### Mechanism

The navigator is, in effect, a live-running synchronous reviewer. The "gripping immediacy" (Fowler) argument is that errors caught in-the-moment are cheaper to fix than errors caught in asynchronous review, and the navigator's different cognitive mode (strategic, detached from keystroke-level working memory) catches errors the driver literally cannot see. A secondary mechanism is narration-forced articulation: verbalizing intent exposes fuzzy thinking.

### Predicted trade-offs in human-agent context

Naively mapping: human-as-navigator, agent-as-driver. Pros: continuous review matches the "catch agent misaligned assumptions early" pain point. Cons: the classical model assumes the navigator can sustain attention through every keystroke -- for the senior-solo-developer this is exactly the attention-cost the track is trying to reduce. A literal port would make the problem worse. A better adaptation treats the *continuous-alignment posture* as optional (turn it on for risky regions, off for routine ones).

### Adaptation notes

What survives: the idea that review is a posture, not a phase; that short-volley embedded question style beats batched-at-end for misaligned-assumption prevention. What breaks: the 15-30 min switching rhythm is tuned to human physical fatigue (typing hands tire; narrating-while-typing overloads); agents don't fatigue the same way, so switching loses its original motivation. What changes: the agent driver can emit structured "I'm about to do X, stop me if wrong" checkpoints the human navigator skims asynchronously -- a kind of **asynchronous-navigator** variant that doesn't exist in the human literature because humans can't skim as fast as agents can emit.

### Evaluation plan

Measure: (a) count of misaligned-assumption incidents per planning session under continuous-navigator vs reviewer-after-dispatch postures; (b) human attention-seconds per incident caught (cheaper catch is the claim); (c) subjective "I know what the agent is doing right now" self-report. A/B by protocol.

---

## Strong-Style Pairing (Llewellyn Falco)

**One-line description.** "For an idea to go from your head into the computer, it MUST go through someone else's hands." The idea-holder is forced into the navigator seat; the driver is forbidden from implementing their own ideas and must trust the navigator, asking only clarifying "what" questions and deferring "why" discussions.

### Dimension values

- **Timing of alignment:** pre-action (navigator fully articulates each next step before driver acts).
- **Decision locus:** human-dominant, with the human being whoever currently holds the idea (which may be either party).
- **Dialog form:** short-volley (instruction / implementation / next instruction), shape-shifting to longer when abstraction level drops for a confused driver.
- **Question style:** one-at-a-time, forced-choice-with-default from the driver's side ("do you want me to ..."); embedded clarification-only from driver side.
- **Autonomy scope:** bounded-by-category -- driver has zero strategic autonomy, only syntactic execution autonomy.
- **Context richness:** rich-brief at the instruction level; continuous-building at the session level.
- **Branching/topic handling:** agent-tracked-with-return -- navigator holds the 2-20-item mental to-do list; completed items are struck through as driver finishes them.
- **Plan expression form:** dialog-log + running code; no separate plan artifact.
- **Knowledge direction:** strongly navigator-to-driver (and Falco explicitly recommends expert-navigates-novice for this reason).
- **Review/critique integration:** "just do it then refactor" -- review deferred to post-implementation, and done jointly.

### Mechanism

Strong-style solves two problems human-pair-programming has: (1) the expert-novice stall, where the novice-driver is slower than the expert would be alone and the expert impatiently takes the keyboard; (2) the keyboard-fighting and silent-driver patterns, where ideas stay in one head and aren't subjected to another brain's scrutiny. Forcing the idea through another pair of hands mechanically guarantees articulation: you cannot communicate an idea you don't actually have, so vague ideas surface immediately. Falco's explicit anti-pattern prevention includes: "I know this seems wrong, but could you please trust me for 4 minutes and then we will talk about the solution" -- an explicit trust-budget mechanism to prevent mid-implementation derailment into meta-discussion.

The abstraction-shift dynamic is important: the navigator is required to speak at the highest abstraction the driver can currently understand, and drops lower ("highlight lines 14-20, press ctrl+alt+m" instead of "extract that method") when the driver shows confusion, then climbs back up as the driver gains fluency. This is an explicit, in-protocol form of adaptive communication.

### Predicted trade-offs in human-agent context

Strong-style maps to human-agent in a specific and non-obvious way: if the agent is the driver, the human's planning cost goes *up*, not down, because the human must articulate every step. This inverts the pain point. But if the agent is the *navigator* (agent articulates the plan, human executes or approves), the protocol becomes interesting: the agent is forced to produce a complete plan-as-instruction-sequence that the human can follow without guessing. That's a forcing function against underspecified plans -- an agent-navigator cannot hand-wave, because the human-driver needs actionable instructions.

A third framing: the agent is *both roles over time*, and strong-style enforces that ideas generated during implementation must be externalized to the human-reviewer before being typed. This is closer to "the agent narrates every decision point out loud and gates on the human" -- which is exactly the pre-action alignment posture worth testing.

### Adaptation notes

What survives: the mechanism of forced articulation is a general fix for implicit-assumption leakage, and LLM agents have the same failure mode (internal reasoning that never surfaces) as human experts. What changes: the navigator role's classical constraint is *cognitive load* (navigator holds 2-20 items); agents hold much more, so the bottleneck shifts to the human's ability to receive. What breaks: the 4-minute trust-budget assumes symmetric relationship equity that doesn't exist with an agent; the human is the principal, not a peer, and "trust me for 4 minutes" should probably be inverted to "the agent earns 4 minutes of autonomy by producing a plan the human approved in 30 seconds."

### Evaluation plan

Two arms: (a) agent-navigator plan-then-human-executes vs (b) agent-autonomous implement-and-summarize. Measure: rework rate, misaligned-assumption incidents, human writing-load. The prediction from strong-style theory is that (a) produces more complete plans and catches more misalignments pre-action, at the cost of more human reading-time.

---

## Ping-Pong Pairing (TDD variant)

**One-line description.** Partners alternate at test-writing and implementation. A writes a failing test; B writes the minimum code to pass it; B writes the next failing test; A writes the minimum code to pass it. Refactor only with all tests green.

### Dimension values

- **Timing of alignment:** pre-action (test is the pre-action specification of the next increment).
- **Decision locus:** pre-authorized per increment -- the test defines what is authorized; implementer has tight discretion within it.
- **Dialog form:** structured (the test is the message) + short-volley around ambiguity.
- **Question style:** implicit -- the test itself embodies the question "does the code do this?"
- **Autonomy scope:** bounded-by-acceptance-tests -- crisp and mechanical.
- **Context richness:** minimal-dispatch per increment (just the failing test); continuous-building via the accumulating suite.
- **Branching/topic handling:** linear, with the test-list acting as an explicit-parking queue.
- **Plan expression form:** test-cases (plus code).
- **Knowledge direction:** bidirectional; alternation forces symmetric contribution.
- **Review/critique integration:** continuous (every alternation is a micro-review by the other partner in the act of interpreting the test).

### Mechanism

Ping-pong enforces two disciplines simultaneously: (1) *minimum implementation* -- the implementer will hard-code a return value if the test doesn't force otherwise, which forces the test-writer to write the next test that invalidates the hard-coding, which in turn forces tests to cover the full space of behavior incrementally; (2) *no-skill-dominance* -- the alternation prevents the faster partner from monopolizing. Anthony Sciamanna argues the cycle produces unusually fast flow because "a much smaller problem space needs to be managed" at any moment.

A subtle feature: the protocol makes plan-refinement a *side-effect of play*. Each failing test is simultaneously a specification request ("here is what I want you to do") and a reification ("it is now checked in executable form"). The test-writer cannot make a vague request, because it must be runnable.

### Predicted trade-offs in human-agent context

This is the candidate most directly adaptable to agent-in-planning: the "test" generalizes to any executable or checkable acceptance criterion. Pattern: human writes one crisp acceptance check; agent implements minimally; agent writes the next check (proposing what should come next); human approves or edits it; agent implements. This produces a sequence of tiny approved contracts rather than one big upfront plan.

Trade-offs: (a) forces the human to know enough of the domain to accept/reject the agent's proposed next check -- may be too much cognitive load for domains the human is unfamiliar with; (b) in planning (vs coding), "tests" are fuzzier -- acceptance criteria for a plan artifact are less mechanical than for code; (c) the discipline of *minimum implementation* maps to "agent implements minimum to satisfy the check," which directly addresses agent-over-elaboration.

### Adaptation notes

What survives: alternation as a forcing function against one-party domination; the "next tiny check" as an atomic unit of planning; minimum-response discipline. What breaks: the test suite as a canonical medium -- in planning-phase work, the equivalent is probably a structured checklist or a spec fragment, and translating "red-green-refactor" to "unclear-clear-refine" loses the mechanical crispness that gives ping-pong its edge. What changes: in the agent setting, the agent could write *both* the check and its implementation and present the pair -- but that loses the alternation mechanism. The discipline needs an external adversary (the human) or a separate agent (a reviewer-sub-agent) to preserve the forcing function.

### Evaluation plan

Run ping-pong-style planning on a task: human writes acceptance bullet 1; agent proposes work to satisfy it + acceptance bullet 2; human edits bullet 2 + approves work 1; agent proposes work 2 + bullet 3; continue. Compare to a monolithic "here's the plan" interaction on the same task. Metrics: rework rate, plan-completeness-at-dispatch (did we miss anything obvious?), human-writing-minutes.

---

## Mob Programming (rotation dynamic)

**One-line description.** Whole team at one screen. Driver types (mechanically). Navigator(s) think. Driver rotates every 10-15 minutes on a fixed schedule. Crucially, the driver is "dumb" -- only the navigators may supply ideas; if no navigator has an idea, the driver waits.

### Dimension values (focusing on the rotation primitive, not the whole-team aspect)

- **Timing of alignment:** continuous.
- **Decision locus:** interactive (navigators collectively); the driver is pre-authorized only to type what navigators say.
- **Dialog form:** short-volley.
- **Question style:** open-ended (any navigator may ask or answer).
- **Autonomy scope:** none for driver; bounded-by-category for navigators.
- **Context richness:** continuous-building.
- **Branching/topic handling:** parallel-tracks -- multiple navigators may hold different side-threads and surface them when relevant.
- **Plan expression form:** dialog-log + code.
- **Knowledge direction:** bidirectional, n-way.
- **Review/critique integration:** parallel-reviewers -- every non-driver is a reviewer.

### Mechanism

Mob's innovation over pair is the *idle-while-waiting* discipline: "if the navigator isn't sure what to do, the driver does nothing and waits, even if they have an idea." This mechanically enforces the strong-style rule at scale. The fixed-timer rotation eliminates role-renegotiation as a social cost -- nobody has to claim the keyboard. The rotation *also* works as a forced context reconstruction: the incoming driver has to re-enter the state of the code because they weren't just typing it; this surfaces unshared context.

### Predicted trade-offs in human-agent context

The interesting adaptation is: **parallel-reviewer sub-agents**. Mob's value isn't the keyboard rotation per se; it's the presence of multiple independent reviewing minds. In the agent setting this maps to: dispatch the same plan to several reviewer-sub-agents with different priors (e.g., "critique for hidden assumptions", "critique for scope creep", "critique for missing edge cases") and return the union of concerns to the human. This is already in the user's observed patterns and mob provides theoretical backing.

Rotation of "who holds the pen" has a weaker analog: have the planning agent and a critique agent swap turns generating and critiquing, so neither becomes an anchor.

### Adaptation notes

What survives: parallel critique from heterogeneous viewpoints; fixed rotation to prevent anchoring. What breaks: the n-way-conversation overhead is high for one human and needs aggressive summarization. What changes: in human-agent pairings the "mob" can be cheap (sub-agents are instantiable on demand) where human mobs are expensive, so the ratio favors more reviewers than humans would tolerate.

### Evaluation plan

A/B: plan-with-single-agent vs plan-with-agent-plus-k-parallel-reviewers (k ∈ {1, 3, 5}). Measure: issues-found-per-hour-of-human-attention, redundant-issue rate (does k=5 say the same thing k=3 says?), plan-completeness.

---

## Cognitive Tag Team (Freudenberg's emergent model)

**One-line description.** Not a prescribed protocol but an empirically observed dynamic: experienced pairs swap roles fluidly and without announcement, and both partners contribute across abstraction levels regardless of nominal role. The value is in mutual cognitive-load redistribution, not in role specialization.

### Dimension values

- **Timing of alignment:** continuous.
- **Decision locus:** mixed, shifting fluidly.
- **Dialog form:** shape-shifting.
- **Question style:** embedded, open-ended.
- **Autonomy scope:** bounded-by-constraint (shared mental model of the work) but not by role.
- **Context richness:** continuous-building.
- **Branching/topic handling:** parallel-tracks, jointly tracked.
- **Plan expression form:** dialog-log + code.
- **Knowledge direction:** bidirectional.
- **Review/critique integration:** continuous, symmetric.

### Mechanism

The discovered value of pair programming, per the empirical record, is not that two brains specialize but that two brains *share* a running computation, and switch physical roles to manage the asymmetric cognitive cost of typing-while-narrating. The prescriptive driver-navigator model is a scaffold novices use while learning; experienced pairs dissolve it.

### Predicted trade-offs in human-agent context

This one is the most cautionary finding for adaptation: if the observed value comes from *symmetric cognitive participation*, protocols that force the agent into a subordinate role (pure executor) may be capturing the scaffold rather than the value. The planning-protocols track should test whether the best patterns involve genuine back-and-forth idea generation, not just the human directing and the agent implementing.

### Adaptation notes

What survives: the finding that specialization is overrated; alignment comes from shared state, not divided responsibilities. What breaks: symmetric "fluid" switching presumes peer equity that doesn't exist human-to-agent. What changes: the agent-as-peer pattern is worth testing explicitly, especially in the planning phase where the agent may have domain knowledge the human lacks and should be allowed to propose, not just execute.

### Evaluation plan

Compare: (a) strict-role-separation protocol (human specs, agent implements) vs (b) fluid-role protocol (either party may propose plans, challenge plans, write artifacts, switch mid-task). Expect (b) to find more completeness issues early but require more human cognitive engagement. Cross this with task familiarity: hypothesis is (a) wins when the human is expert, (b) wins when the human is learning the domain.

---

## Asymmetric Abstraction-Shifting (extracted from strong-style navigator behavior)

**One-line description.** A sub-protocol within strong-style: the information-producer dynamically adjusts the abstraction level of their instructions based on the receiver's signals of comprehension, starting high, dropping on confusion, climbing back as fluency builds.

### Dimension values

- **Timing of alignment:** in-action.
- **Decision locus:** held by the producer, with constant recalibration from receiver feedback.
- **Dialog form:** shape-shifting (high-abstraction <-> low-abstraction).
- **Question style:** forced-choice-with-default when recovering from confusion ("did you mean X or Y? default X"); open-ended in normal flow.
- **Context richness:** continuously adapted to the receiver.
- **Plan expression form:** hybrid.
- **Knowledge direction:** producer-to-receiver, but with receiver's comprehension as the driving signal.

### Mechanism

Most alignment failures in pair programming come from mismatched assumed abstraction levels (the navigator says "extract that method" to a driver who doesn't know the IDE shortcut). The protocol's innovation is treating the receiver's comprehension as a control signal, not a side-effect. Climb back up once fluency recovers; otherwise detail-escalation ratchets indefinitely.

### Predicted trade-offs in human-agent context

This is probably the single most directly portable protocol, and it maps to both directions: (a) agent adjusts the abstraction level of its proposals to match the human's signaled fluency with the domain ("I understand, continue" vs "wait, what's a protocol buffer?"), (b) human does the same for the agent when specifying goals, but empirically the agent is more abstraction-flexible than a junior human.

### Adaptation notes

What survives: the technique is domain-independent and LLMs are already reasonable at abstraction-matching when prompted. What changes: agents can be told explicitly to ask "should I explain more or continue?" rather than inferring; this is more reliable than humans reading body language. What breaks: nothing obvious.

### Evaluation plan

Prompt-level A/B: agent instructed to produce plans at a fixed abstraction level vs agent instructed to start high and offer to drop on request. Measure: human-reported comprehension, questions-back-to-agent rate, re-explanation requests.

---

## Error-Catching as Posture, Not Review (general mechanism)

**One-line description.** Not a protocol but a mechanism all the above share: pair programming's defect-reduction benefit comes primarily from errors being caught *during* coding, not from a review phase. The reviewer is co-present.

### Mechanism

Williams' 86-94% vs 73-78% test-pass delta is not attributable to post-hoc review, because both groups had the same post-hoc review available. The delta comes from the always-present second perspective catching errors in flight. Fowler: "it is impossible to ignore the reviewer when he or she is sitting right next to you."

### Implication for planning protocols

The planning-protocols track should consider a distinction that is not cleanly present in other alignment literatures: **review-as-phase** (asynchronous, easy to defer, easy to rubber-stamp) vs **review-as-posture** (continuous, unignorable, expensive in attention). The two produce different outputs even with the same reviewer skill. This suggests the right protocol design variable isn't "is there review" but "is review synchronous with generation or not."

### Adaptation notes

The asymmetric attention costs make this hard: humans cannot cheaply maintain review-as-posture for agent output (that's the user's core pain point). But sub-agents can. A reviewer-sub-agent hooked into the primary agent's output stream mid-generation (not at end) is the posture-form of review, and it's cheap for the human because the human reads the sub-agent's summary, not the primary stream. This is an adaptation that only makes sense in an agent setting and may beat what the human literature offers.

---

## Knowledge-Transfer Gap (Plonka et al. finding)

**One-line description.** Not a protocol -- a diagnosis. Observed pair programming uses cognitive-apprenticeship moves only partially: modelling and coaching are present, but novice articulation, reflection, and exploration are absent. This is a gap that could be closed with explicit protocol support.

### Mechanism

Cognitive apprenticeship theory predicts learning proceeds through six stages: modelling (expert shows), coaching (expert guides as novice tries), scaffolding (expert steps back progressively), articulation (novice explains back), reflection (novice compares own approach to expert's), exploration (novice tries variations). Plonka et al. observed the first three but not the last three.

### Implication for planning protocols

Two directions. (a) If the agent is positioned as the "novice" receiving instruction from the human, the protocols can explicitly include articulation passes ("agent, restate the plan in your own words") -- the observed absence in human pairs is probably because it's awkward socially; it's not awkward for an agent. (b) If the human is the "novice" in the domain, the agent can be prompted to force articulation ("human, before I proceed, what outcome do you expect?") -- an explicit counter-move to the human writing-load pain point, because it converts writing into talking-back.

### Adaptation notes

This is the clearest case where the agent setting *improves* on the human source. The cognitive-apprenticeship moves that don't happen in human pairs can be made cheap and routine in human-agent pairs because an agent can ask "summarize your understanding so far" without social awkwardness.

### Evaluation plan

Insert an articulation checkpoint at the end of a planning session ("agent, in one paragraph state what you think we agreed on"; human corrects). Measure: implementation drift rate in the subsequent session, misaligned-assumption surface rate at handoff.

---

## Anti-Patterns (what to mechanically prevent)

The pair-programming literature's anti-pattern catalog maps surprisingly cleanly onto human-agent planning failure modes. Each one has a protocol-level antidote.

| Anti-pattern (human) | Human-agent analog | Protocol-level antidote |
|---|---|---|
| Silent driver | Agent implements without narrating reasoning; human can't steer. | Require continuous narration of decision points; gate on human acknowledgment for flagged categories. |
| Bored / sleeping navigator | Human rubber-stamps agent output because sustained attention is expensive. | Review-as-posture via sub-agent (see above); short-volley dialog to preserve engagement; forced-choice-with-default questions. |
| Role drift | Agent starts writing the plan; human starts executing. Neither is minding the overall direction. | Explicit role labeling per phase; fluid switching allowed but announced. |
| Keyboard hogging | Agent writes the whole plan; human hasn't contributed their priors. | Strong-style inversion -- ideas surface through articulation, not grab-the-output. |
| Driving too fast | Agent emits too much too quickly; human can't review. | Chunking discipline; minimum-implementation rule from ping-pong. |
| Leaping on errors (navigator) | Human interrupts agent mid-generation for trivial issues. | 5-second rule; buffered critique. |
| Low-level-instruction trap | Human over-specifies, collapsing agent's useful autonomy. | Abstraction-shifting protocol; start high, drop only on confusion. |
| Listening without hearing | Agent keeps generating while human types a correction. | Interrupt-and-reset protocol: new human input halts generation. |

The striking pattern: every human-pair anti-pattern has a direct human-agent analog, and most of the recommended human preventions translate. This is the strongest evidence that pair programming is a useful source domain, not a distraction.

---

## Protocols considered and rejected

- **Remote pair programming (screen-share + voice) as a protocol.** It's a logistics variant of driver-navigator, not a new protocol shape. Its novelties (latency handling, shared-editor conflict resolution) are artifacts of the medium, not reusable coordination primitives.
- **Pair programming with AI assistants (Copilot-style) as a protocol.** Already a human-agent setting, and its protocol content is dominated by IDE-integration mechanics (tab-complete, inline suggestion) rather than the dialog-level structure this track is after. Relevant if we later need concrete interaction affordances but not as a protocol source.
- **Personality-based pair matching.** Organizational-design work, not a protocol -- answers "who pairs with whom," not "what does a pair do."
- **Pair programming for onboarding / mentoring ceremonies.** Compliance/management framing; irrelevant for the senior-solo-developer context per the locked choice.
- **"Pairing with your future self" (rubber-ducking variants).** Interesting but properly belongs to the self-talk / reflection literature, which is a separate Phase 2 domain.

---

## Sources

- [Cockburn & Williams, "The costs and benefits of pair programming"](https://www.semanticscholar.org/paper/The-costs-and-benefits-of-pair-programming-Cockburn-Williams/5ff7b75b20fdbfae23587b660b7093aec2f48e69) -- foundational quantitative claims.
- [Plonka, Sharp, van der Linden, Dittrich, "Knowledge transfer in pair programming: an in-depth analysis" (Int. J. Human-Computer Studies, 2015)](https://www.sciencedirect.com/science/article/abs/pii/S1071581914001207) -- six teaching strategies, absence of novice articulation/reflection/exploration.
- [Plonka & Sharp, "Pair programming and the mysterious role of the navigator"](https://www.sciencedirect.com/science/article/abs/pii/S1071581907000456) -- challenges the prescriptive navigator role.
- [Freudenberg, "An alternative take on the 'Driver' and 'Navigator' roles in pair programming"](https://salfreudenberg.wordpress.com/2013/08/31/an-alternative-take-on-the-driver-and-navigator-roles-in-pair-programming/) -- cognitive tag-team model.
- [Falco, "Llewellyn's strong-style pairing"](http://llewellynfalco.blogspot.com/2014/06/llewellyns-strong-style-pairing.html) -- rules, golden rule, trust budget, abstraction-shifting.
- [Pyhajarvi, "The Driver-Navigator in Strong-Style Pairing"](https://medium.com/@maaretp/the-driver-navigator-in-strong-style-pairing-2df0ecb4f657) -- expert-as-navigator adaptation.
- [Fowler, "On Pair Programming"](https://martinfowler.com/articles/on-pair-programming.html) -- practitioner synthesis, styles, anti-patterns, "gripping immediacy" mechanism.
- [Sciamanna, "Ping Pong Pair Programming"](https://anthonysciamanna.com/2015/04/18/ping-pong-pair-programming.html) -- minimum-implementation discipline, flow, design-testability link.
- [Open Practice Library, "Ping-Pong Programming"](https://openpracticelibrary.com/practice/ping-pong-programming/) -- canonical cycle.
- [Zuill, "Mob Programming -- A Whole Team Approach" (Agile2014 experience report)](https://agilealliance.org/resources/experience-reports/mob-programming-agile2014/) -- whole-team formulation.
- [Mob Programming Basics](https://mobprogramming.org/mob-programming-basics/) -- rotation mechanics, "dumb driver" rule.
- [Tuple, "Pair Programming Antipatterns"](https://tuple.app/pair-programming-guide/antipatterns) -- enumerated failure modes and preventions.
