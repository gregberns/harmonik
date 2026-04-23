# External Source: Pilot-Controller Communication

**Phase 2 · External-Source Pass · Domain: aviation (ATC / CRM)**
**Author:** research sub-agent
**Date:** 2026-04-23

## Domain intro

Pilot-controller communication is the most rigorously-engineered low-error high-frequency human coordination protocol in routine use. Unlike medical handoffs — which are structured single-transmission transfers between shift-partners — aviation communication is a **continuous short-volley exchange** between parties who share no physical context, cannot see each other, operate under time pressure measured in seconds, and whose errors have immediate irreversible consequences. The protocol has been iterated across roughly six decades of incident investigation into a compact, ICAO-mandated structure codified in *Doc 9432 (Manual of Radiotelephony)* and *Doc 4444 (PANS-ATM)*, with a parallel human-factors overlay (Crew Resource Management) that governs intra-cockpit behavior.

Three structural properties distinguish aviation from every other domain reviewed in this pass:

1. **Fixed vocabulary with fixed meanings.** "Affirm," "wilco," "roger," and "negative" each denote exactly one thing. Natural-language synonyms are banned. A pilot saying "OK" instead of a readback is a protocol violation, not a stylistic choice — because "OK" was the word that ambiguated the Tenerife takeoff clearance in 1977 and helped kill 583 people.

2. **Closed-loop confirmation on every exchange.** The transmission–readback–hearback loop is not a review at the end of a session; it is a **per-turn invariant**. Every instruction of consequence is immediately echoed back by the receiver and re-verified by the sender. There is no "I think they heard me" state.

3. **Protocols are traceable to specific incidents.** Each element of modern aviation communication can be traced to a named accident (Tenerife 1977 → "takeoff"-reserved-for-clearance + mandatory key-item readback; United 173 1978 → CRM + graded assertiveness; Avianca 052 1990 → standardized emergency fuel declarations; Eastern 212 1974 → sterile cockpit rule). The protocol is an *incident archive* in structured form.

The disanalogies matter too. Aviation runs at zero-tolerance for ambiguity because a misinterpreted heading is fatal within seconds. Coding-planning operates at a much higher ambiguity tolerance, slower clocks, and with one human making all calls rather than a crew of peers under authority gradient. Importing aviation ceremony wholesale would be both inappropriate and self-defeating — the user has explicitly flagged high-ceremony protocols as attention-corrosive. But the **alignment-preservation mechanisms** aviation has engineered — readback loops, fixed-token vocabularies, graduated assertiveness, explicit authority transfer, focus-phase designation, challenge-response callouts, pre-phase briefings — each address alignment failures that also occur in coding-planning, where no equivalent mechanism currently exists.

### What this domain offers the planning-protocols track

- A **per-turn readback invariant** (distinct from medical handoffs' end-of-session synthesis) that catches a different failure mode: drift across a long session rather than loss at a boundary.
- A **fixed-token vocabulary** proposal that may warrant its own dimension value (or a new dimension) in the research-statement Section 3 table.
- A **graduated-assertiveness ladder** (PACE) that converges with medical CUS but, because it originated in an asymmetric-authority context, is structured to let a *subordinate* party escalate — directly relevant to an agent's position vs. the user.
- A **focus-phase rule** (sterile cockpit) mapping to the unexplored "protocol shape-shifting" region in research-statement §5.
- An **authority-transfer ritual** ("you have the controls / I have the controls / you have the controls") that explicitly names the otherwise-implicit moment when decision ownership changes hands — addresses a pain point visible in planning corpus (ambiguous "whose call is this").
- A **challenge-response SOP pattern** that treats checklist items as a spoken protocol, not a silent tick-list — suggests an agent should *announce* checkpoints and wait for explicit acknowledgment, not silently mark them done.
- An **incident-driven change log** that catalogs which kinds of misalignment each protocol correction targets — directly useful as a failure-mode taxonomy to test candidate coding protocols against.

## Readback / hearback loop

### Description

For every ATC instruction of consequence — altitude, heading, runway, clearance, frequency change — the receiving pilot must **read back** the key items in structured form, and the controller must **hear back** the readback to verify it matches the original. If the readback contains an error, the controller issues a correction; the pilot then reads back the correction; cycle closes only on a clean match. The canonical form is three legs: transmit → readback → hearback-confirm (often the controller is silent if the readback is correct, but the silence itself is a confirmation because any deviation would have triggered a correction). ICAO Doc 9432 specifies exactly which items must be read back (altitudes, headings, runway assignments, clearances to enter/cross/hold short, frequencies, transponder codes); others may be acknowledged with a callsign only.

Unlike medical SBAR readback, which synthesizes a multi-slot transmission at its end, aviation readback is **per-instruction and continuous**. A 30-minute flight leg may involve 20–40 readback cycles, each one lightweight.

### Why each element is load-bearing

- **Drop readback** → receiver silently misinterprets; controller has no signal until the aircraft deviates from expected trajectory. Tenerife failure mode.
- **Drop hearback** (controller ignores or misses the readback) → receiver made an error, reads back the error, it is not caught; receiver acts on the wrong value. This has been a repeated contributor to runway-incursion incidents — *readback-without-hearback is not the protocol; both legs must close*.
- **Drop key-item specification** (which items require readback) → readback devolves into a stylistic "roger" that carries no verification content. The ICAO specification of *which items must be read back* is load-bearing: it tells both parties what counts as a verifiable unit of instruction.

### Dimension values

- **Timing of alignment:** continuous per-turn (every instruction of consequence)
- **Decision locus:** interactive, but asymmetric — controller issues, pilot reads back, controller confirms/corrects
- **Dialog form:** short-volley, structured, fixed-vocabulary
- **Question style:** implicit (the readback *is* the verification; no explicit question)
- **Autonomy scope:** bounded-by-instruction (each instruction is a discrete commitment)
- **Context richness:** minimal-per-turn; richness comes from aggregation across many turns
- **Plan expression form:** dialog log (each turn is logged on a stripboard or by recording)
- **Knowledge direction:** bidirectional — controller transmits, pilot confirms, either can correct
- **Review/critique integration:** inline on every turn (distinct from medical post-handoff review and from design-review batch review)

### Mechanism

The readback loop exploits a property of **transcription error asymmetry**: a receiver who silently mis-hears and acts will commit resources to the wrong interpretation, but a receiver who must vocalize the interpretation is forced to *explicitly encode* what they believe was said, which surfaces the error while correction is still cheap. The hearback leg adds a second independent check: the sender, who knows the intended content, verifies the receiver's encoded version. This is a form of **two-phase commit over a noisy channel**.

The fixed-vocabulary overlay makes readback efficient: because allowable tokens are small and positional grammar is fixed, a readback is typically 1–3 seconds, not a paraphrase. Compare to medical SBAR readback, which is a synthesis — aviation readback is a *mirror*.

Error classes caught: misheard numbers (altitude 7000 vs. 17000), misassigned calls (clearance intended for another aircraft), omitted constraints (instruction to descend *and* turn, only one heard), runway confusion. These are precisely the failure modes that survive in coding-planning as misheard-intent errors: the user says "bounded context" and the agent hears "bounded context within the auth module," silently narrowing scope.

### Predicted trade-offs for coding-planning

**Helps with:** the vocabulary/framing errors the Phase 1 form-vs-content lens identified as the most expensive class. If an agent's turn-closer is structured as a compact readback of what it took from the user's turn — not a summary of the agent's reply, but a *mirror* of the user's load-bearing tokens — misframings surface on the next turn instead of on completion.

**Helps with:** drift in long sessions. SBAR-style end-of-session synthesis catches divergence at the boundary; aviation-style per-turn readback catches divergence within each turn. Coding-planning sessions are long (observed: 40–120 turns); boundary-only verification is too infrequent.

**Costs:** readback on every turn is attention-expensive if done verbatim. Aviation gets away with this because the vocabulary is compact; coding-planning vocabulary is not compact. A naive port where the agent echoes the whole user turn would be insufferable. The mechanism has to be adapted to readback *only load-bearing tokens* — a "mirror the constraints, not the prose" move.

**Watch for:** in aviation the receiver-to-sender direction is asymmetric and the roles are stable. In coding-planning, the agent is sometimes receiver (of a user directive) and sometimes sender (of a proposed plan). The protocol should probably run in *both* directions — the agent reads back user constraints, but also writes load-bearing plan elements in a form the user can cheaply read back or contradict.

**Distinct from medical readback:** medical CUS/SBAR-plus-readback is *end-of-synthesis*, checks the bundle. Aviation readback is *per-utterance*, checks the link. These are different protocols that would stack: a coding-planning session could use per-turn "mirror-the-constraint" readback *and* end-of-opener SBAR-style synthesis.

### Adaptation notes

A viable adaptation: require the agent, after any user turn that contains a constraint or decision, to echo back the **load-bearing tokens** (constraints, named systems, scope boundaries) in a compact one-liner before proceeding with reply content. User either lets it pass (hearback = silent) or corrects (hearback = explicit correction, pilot reads back the correction). The user would not be required to actively confirm — silence closes the loop — which preserves the writing-cost budget.

The hard design question is which tokens count as "load-bearing." A first-cut heuristic from the Phase 1 corpus: named entities (subsystem names, file names, tool names), quantifiers (must/should/may, all/any/some), and scope boundaries (in/not-in). This is a candidate for empirical derivation from the corpus, not theoretical specification.

### Evaluation plan

Build a test-set from Phase 1 corpus sessions where misframing was observed (decision-delegation lens, misaligned-assumption lens). For each, construct the load-bearing-token readback that would have been issued. Check:

- (a) Would the user have corrected it? (If yes, the misframing is caught earlier.)
- (b) How long is the readback in characters? (Target: ≤ 1 line, ≤ 15 tokens.)
- (c) Does it introduce a new attention sink? (The user has to read and implicitly-accept one additional short line per constraint-carrying turn.)

If the readback is consistently ≤ 15 tokens and catches > 50% of observed misframings, the mechanism is worth piloting.

## Fixed-vocabulary tokens (standard phraseology)

### Description

ICAO mandates a compact vocabulary with fixed meanings for every category of communication. "Affirm" means yes. "Negative" means no. "Wilco" means "will comply." "Roger" means "I received your transmission" and specifically **does not** mean "yes" or "I understand" or "I will do." "Unable" means "the instruction cannot be complied with." Clearances have fixed positional grammar ("[callsign], cleared to [destination] via [route], climb and maintain [altitude]"). Natural-language paraphrase is a protocol violation.

In 2007 ICAO changed "affirmative" → "affirm" specifically because "affirmative" and "negative" are phonetically confusable when the first syllable is clipped or distorted. That level of engineering attention to single-word ambiguity is the core of the design philosophy.

### Why this is load-bearing

Fixed vocabulary does three things natural language cannot:

1. **Eliminates synonym drift.** In natural language, "OK," "yes," "sure," "will do," "got it," and "understood" all roughly mean "affirmative" but each carries shades of commitment. "Sure" may mean "I heard but am not committing"; "will do" commits; "got it" may mean either. Fixed vocabulary collapses this to a single unambiguous token.
2. **Reduces cognitive parsing load.** A fixed token is pattern-matched, not parsed. Under time pressure this matters — and the same compression benefit applies when the *sender* is trying to produce an unambiguous utterance quickly.
3. **Creates verifiable compliance.** A readback can be checked token-by-token against the expected vocabulary. Deviations are immediately flagged. You cannot apply this check to free-form natural language.

### Dimension values

This is potentially a **new dimension-value** not explicitly on the Section 3 table, or a refinement of the Plan-expression-form axis.

Candidate new dimension: **Vocabulary discipline** with values: *natural-language* (no constraints) / *shared-idiom* (team-level conventions, informal) / *fixed-token for status markers* (specific words for specific states) / *fully-controlled* (aviation-style mandated vocabulary).

Current planning-protocols default is natural-language. The user's own corpus shows ad-hoc emergence of shared idioms ("park," "bootstrap," "spec-first") that function like low-grade fixed tokens — but without enforcement or shared definition.

### Mechanism

The specific applicability to coding-planning is for **status-marker tokens** — the small set of states a plan element can be in during a planning session. Proposed observed states from the corpus:

- **proposed** — this is an option the agent is offering; no commitment yet
- **locked** — this has been decided; reopening requires explicit justification
- **deferred** — acknowledged but pushed to a later phase / parking lot
- **will-investigate** — agent is taking this as a research action, will return with findings
- **rejected** — considered and declined (with a reason)
- **open** — under active discussion, no clear direction yet
- **blocked** — cannot proceed without a named input

Without fixed tokens, the user's corpus shows these states emerging in paraphrase ("let's not worry about that for now," "we'll come back to that," "that's out of scope") — which are semantically close but not identical and cannot be mechanically checked. With fixed tokens, the agent can annotate its own plan elements with machine-and-human-readable state, and the user can confirm or flip with a single-word correction ("lock 3, defer 5, reject 7").

### Predicted trade-offs for coding-planning

**Helps with:** decision-closure ambiguity (Phase 1 finding: decision closure predicts autonomy-stretch length). A proposed item with status `open` is not ready for autonomous execution; `locked` is. Current corpus leaves this implicit and the agent sometimes treats `open` as `locked` (premature commitment) or `locked` as `open` (re-litigates decided questions). Fixed tokens make the state transition explicit.

**Helps with:** parking-list fidelity. `deferred` items, explicitly tagged, are candidates for agent-surfaced parking (an unexplored-region capability named in §5) in a way that paraphrased "we'll come back to it" is not.

**Costs:** learning cost. The user must learn and use the tokens (though the agent could enforce them and the user could correct naturally; this is not a high barrier for a sole developer). Token set ossification — if the fixed token list is wrong, the protocol actively resists the right vocabulary. Mitigation: start with 4–6 tokens, grow only on demonstrated need.

**Watch for:** token abuse. "Locked" can become a rhetorical weapon where one party uses it to foreclose discussion; aviation avoids this because the authority structure is formal. In a solo-developer + agent context, the agent should probably never issue `locked` — only the user does — while the agent may issue `proposed`, `will-investigate`, and `blocked`.

### Adaptation notes

This is a strong candidate. It is light-weight (no new process, just a short vocabulary), compatible with natural-language dialog around it, mechanically checkable, and addresses a named Phase 1 pain point (decision-closure ambiguity). It should be tested in isolation before being bundled with heavier protocols.

### Evaluation plan

Run a single kerf work with the agent instructed to annotate every plan-element it proposes with one of the fixed status tokens, and with the user empowered to respond with token-only corrections. Measure:

- (a) How often does the user correct the token? (High = the protocol is actively disambiguating; low = either it was already clear or the user isn't engaging.)
- (b) Character count of user turns vs. a matched control session. (Target: reduced.)
- (c) Did any decisions get re-litigated that were marked `locked`? (Should be zero.)
- (d) Did any `deferred` items get lost? (Target: reduced vs. baseline parking-list fidelity.)

## Graded assertiveness (PACE)

### Description

PACE — *Probe, Alert, Challenge, Emergency* (sometimes *Escalate*) — is a four-step script for raising a concern across an authority gradient. Developed in aviation CRM (with parallel development in medicine and maritime) after incidents like United 173, where the first officer noticed the fuel problem but failed to escalate past the captain's focus on the landing gear:

1. **Probe** — low-cost question. "How much fuel do we have remaining?" Does not assert anything; gives the recipient a cheap opportunity to notice and self-correct.
2. **Alert** — named concern. "I'm concerned about our fuel state." Claims the ground of having a concern without yet proposing override.
3. **Challenge** — explicit disagreement. "Captain, we need to land now. Our fuel is critical." Proposes an action contrary to the current trajectory.
4. **Emergency** — override. "I am taking the controls." Direct action without waiting for authorization, invoked only when imminent danger is present.

The graduation matters. The *probe* is cheap — low social cost, low commitment — and often sufficient; the *emergency* is rare and only justified after graduated escalation has failed.

### Why this is load-bearing

A single-level protocol (just "say what you think") fails across authority gradients because the subordinate weighs the social cost of escalation against the expected value of being heard, and under uncertainty usually concludes the social cost is higher. PACE *reduces the marginal cost of each escalation step* so that the subordinate can commit to a probe cheaply, wait for response, then escalate only if warranted. This converts a single high-stakes decision (raise or don't raise) into a series of small decisions (probe → alert → challenge → ...) each of which is easier to take.

### Dimension values

- **Timing of alignment:** in-action (ongoing; escalation triggered by divergence the subordinate has noticed)
- **Decision locus:** pre-authorized (subordinate is pre-empowered to escalate up through Emergency); question-style ladder
- **Dialog form:** short-volley at each step; protocol is the *sequence* of volleys
- **Question style:** graduated — starts open (probe) and narrows (challenge, emergency)
- **Autonomy scope:** bounded until Emergency, at which point becomes unbounded
- **Plan expression form:** dialog log
- **Knowledge direction:** subordinate-initiated; authority party responds
- **Review/critique integration:** the protocol *is* the critique mechanism

### Mechanism

The escalation ladder makes it cheap for the *concerned party* to raise a concern without committing to a full challenge, and cheap for the *authority party* to respond at the minimum-required level. Both parties get to operate at the lowest-energy level until the situation forces escalation.

Converges with medical CUS ("I'm Concerned, I'm Uncomfortable, this is a Safety issue") — both protocols address the same problem: a subordinate with information the authority-holder doesn't have, and the need to transmit it across the gradient before a decision window closes.

### Predicted trade-offs for coding-planning

**This is the most important candidate for the agent's side of the protocol.** The user is the authority in a planning session; the agent is the party with potentially-relevant concerns (detected inconsistencies, flagged assumptions, noticed scope creep). Current corpus shows the agent mostly *does not* escalate — either it silently defers to the user's framing, or it produces a multi-question batch that treats every concern as equal weight. Neither is graduated.

**Helps with:** agent-investigative protocols (unexplored region §5). PACE gives the agent a *scripted way* to surface a concern without either deferring or over-asserting. A *probe* ("what drives the choice of [X] here?") is genuinely low-cost and different from a *challenge* ("I think [X] is wrong because [Y]").

**Helps with:** vocabulary-framing errors. An agent's probe — a question rather than an assertion — is a good instrument for catching framing errors without having to diagnose them confidently.

**Costs:** the "Emergency" level does not directly translate — an agent has no override action in a planning session. The ladder probably terminates at Challenge for coding-planning; the "Emergency" equivalent is something like *refusing to proceed until a disagreement is resolved*, which is a legitimate last-resort move.

**Watch for:** probe-inflation. Aviation probes are cheap because they're single questions; a verbose agent could convert probes into paragraphs, defeating the purpose. Probes must be *one-liners*.

### Adaptation notes

Three-level ladder for agent-to-user:

1. **Probe** — one-line question surfacing a possible concern. No assertion. ("What's driving the [X] choice here?")
2. **Alert** — one-line named concern. Still no proposal. ("I'm uncertain whether [X] is consistent with [previously-stated Y].")
3. **Challenge** — short proposal of alternative. ("I'd argue [X] should be [Z] because [brief reason]. Would you want me to proceed as stated or revisit?")

Agent picks the level based on confidence in the concern. The user can answer at any level and close the loop.

### Evaluation plan

Corpus replay: for a set of Phase 1 sessions where the agent *did not* raise a concern that turned out to matter later, reconstruct what a Probe would have said and check whether a Probe would plausibly have been answerable by the user in ≤ 1 line. If yes, the ladder has a plausible path from silent deferral → graceful surfacing without moving to full batched-question.

## Sterile cockpit / focus-phase designation

### Description

FAA 14 CFR § 121.542: below 10,000 feet, on the ground during taxi, and during takeoff and landing, flight crew members must engage **only** in activities directly required for safe operation of the aircraft. No non-essential conversation, no eating, no reading. Above 10,000 feet in cruise, normal conversation resumes. Instituted in 1981 after NTSB reviewed several accidents including Eastern 212 (1974), where idle chatter during approach contributed to loss of altitude awareness.

The rule is *phase-based*, not permanent. The cockpit is not always sterile; only during the phases where distraction is expensive relative to attention available.

### Why this is load-bearing

Focus is a finite resource and **splitting focus across task-relevant and task-irrelevant channels degrades performance on the task-relevant channel** even when the irrelevant channel is superficially engaging. The sterile cockpit rule is a formal acknowledgment that the system can't depend on crew self-discipline during high-load phases — it requires a stated rule and mutual enforcement.

### Dimension values

- **Timing of alignment:** phase-gated (alignment on which phase we're in is pre-negotiated)
- **Decision locus:** pre-authorized — the rule is set before the session; both parties honor it without mid-session negotiation
- **Dialog form:** short-volley only within sterile phases; other phases allow longer / exploratory turns
- **Question style:** strictly task-relevant during sterile; open during non-sterile
- **Autonomy scope:** reduced during sterile — neither party may unilaterally expand topic
- **Context richness:** minimal during sterile
- **Branching:** sterile = linear; non-sterile = may branch
- **Plan expression form:** unchanged by phase
- **Knowledge direction:** bidirectional but task-bounded during sterile

### Mechanism

The rule works because it is **declared and shared**. The utility is not that any given crew member would otherwise fail to focus — it's that the *other* crew member has standing to interrupt a digression. "Sterile cockpit" as a spoken phrase is itself a protocol move: one crew member can redirect another by invoking it. Without the declared rule, the redirection would be a social imposition; with the rule, it's enforcement.

### Predicted trade-offs for coding-planning

This maps onto the **protocol shape-shifting** unexplored region (§5). The observation is that planning sessions have phases (foundation-setting, exploratory decomposition, detailed design, spec drafting, review) with different optimal protocols — but in the current corpus the phase is implicit and the protocol doesn't shift.

**Helps with:** the context-switch load criterion. A declared focus phase suppresses tangents during the phase and defers them to explicit "non-sterile" slots. The user's corpus shows frequent tangent-introduction that the user later regrets having opened.

**Helps with:** structured branching / parking. When a tangent arises during a sterile phase, the explicit move is "park this" — which aligns with the agent-surfaced-parking capability.

**Costs:** rigidity. Sometimes the tangent *is* the important thing and suppressing it kills the session's best idea. Mitigation: define "sterile" narrowly (e.g., the spec-drafting pass specifically), and allow explicit phase exit.

**Watch for:** authority asymmetry. Aviation sterile-cockpit rule is a mutual commitment; in coding-planning the user is the authority and can break the rule at will. The agent can only *note* that the current phase was designated sterile and ask if the user wants to exit the phase. This is probably the right shape.

### Adaptation notes

Planning-session version:

- At session open, name the phase: "Spec-drafting" / "Exploratory-decomposition" / "Research-synthesis".
- Each phase has a designated scope (what's on-topic) and off-topic moves explicitly defined (tangent, parking, break).
- When a turn wanders off-topic, the agent is empowered to say: "That reads like it's outside the [Spec-drafting] scope. Park it, or switch phase?" — a single-line, low-cost redirection.
- Phase change is an explicit move, not a drift.

### Evaluation plan

Compare two kerf works: one with explicit phase declaration and agent-empowered redirection, one without (control). Measure tangent count, tangent resolution (addressed / parked / lost), and per-turn writing cost within the declared phase.

## Authority transfer ritual

### Description

When two pilots share a cockpit, transfer of flight controls is executed as a mandatory three-part exchange:

1. Pilot A: "You have the controls."
2. Pilot B: "I have the controls."
3. Pilot A: "You have the controls." (confirmation)

Only after leg 3 is the transfer complete; pilot A may not release the yoke before then. The rule is strict — transfers executed implicitly (one pilot lets go assuming the other has it) have caused fatal accidents.

### Why this is load-bearing

Authority over the aircraft is a **single-owner resource at all times**. Implicit transfer creates a race condition: either both pilots are actively flying (fighting the controls) or neither is (nobody is flying). The three-part exchange establishes a verified transition point.

### Dimension values

- **Timing of alignment:** at-moment-of-transfer (discrete event)
- **Decision locus:** explicit hand-off
- **Dialog form:** fixed three-utterance exchange
- **Question style:** none — fixed utterances
- **Autonomy scope:** single-owner at any instant
- **Plan expression form:** dialog log
- **Knowledge direction:** bidirectional confirmation

### Mechanism

The three-part structure is a minimal consensus protocol over a lossy channel: one-part transfers leave the receiver unverified; two-part transfers leave the sender unverified that the receiver heard correctly; three-part closes both legs. This is the same structural move as TCP's three-way handshake — not coincidentally, as both are solutions to the "who owns this resource now?" problem across independent parties.

### Predicted trade-offs for coding-planning

Coding-planning has an analog problem the corpus surfaces: **ambiguous decision ownership on specific items**. The user may expect the agent to decide a trivial data-structure choice; the agent may defer back to the user; neither acts; the item stalls. Or, the reverse: the user expects to be consulted on a library choice; the agent picks autonomously; the user discovers after the fact and corrects.

**Helps with:** decision locus clarity on specific items (aligned with §4 "upfront decision partition" observed pattern, but operates per-item rather than upfront).

**Adaptation:** two-token move. Agent asks "your call or mine?" on an item; user replies "your call" or "mine." The resource (decision authority on that item) now has a single owner. Further turns on that item are evaluated against the declared owner.

**Costs:** per-item ritual is too heavy for trivial items. Aviation doesn't use the three-part exchange for everything — only for the one resource that can only have one owner. Coding-planning analog is that the ritual should only fire on items where ownership is ambiguous, not on everything.

**Watch for:** ritual collapse. If the agent asks "your call or mine?" on every proposal, the user will stop reading it and just blanket "yours." The protocol depends on the question being non-routine.

### Adaptation notes

Lighter than the full three-part exchange: a single-token agent move (`[your-call?]` or `[taking-this-one]`) used only on items the agent judges ambiguous. User can override with `[mine]` or `[yours]`. Rare but load-bearing.

### Evaluation plan

Corpus scan for sessions where a decision was stalled or re-litigated because ownership was unclear. Reconstruct the `[your-call?]` that would have fired. Check whether it would have resolved the ambiguity in one turn.

## Pre-phase briefing (approach briefing / takeoff briefing)

### Description

Before takeoff and before final approach, the pilot flying conducts a structured briefing to the pilot monitoring covering specific mandatory content slots: intended procedure, critical speeds, callout expectations, automation plan, anomaly responses, missed approach procedure. The briefing is **interactive** — the PM is expected to confirm agreement and flag inconsistencies per slot, not silently wait for the end.

### Why this is load-bearing

A flight segment has enough decisions that *naming them during execution* is too late; rehearsing them *before* execution lets the crew synchronize mental models, catch missing data (wrong approach chart, wrong runway), and agree on responses to anomalies that have not yet occurred. The interactive element is critical — research on approach briefings found that monologue-style briefings were ineffective precisely because they allowed the PM to mentally check out. Slot-by-slot acknowledgment forces engagement.

### Dimension values

- **Timing of alignment:** pre-action (before executing the phase)
- **Decision locus:** mixed — PF proposes, PM confirms or challenges
- **Dialog form:** short-volley within a structured slot list; each slot is a mini-exchange
- **Question style:** implicit per slot; explicit "any questions" only as a backstop
- **Autonomy scope:** the briefing bounds what will happen next
- **Context richness:** rich-brief but interactive
- **Plan expression form:** structured artifact (the slot list)
- **Knowledge direction:** PF → PM with acknowledgment, but PM may correct
- **Review/critique integration:** inline, per slot

### Mechanism

Hybrid of SBAR (structured slots) and readback (per-slot acknowledgment). The slot structure forces the PF to produce coverage (can't skip something by mistake); the per-slot acknowledgment forces the PM to actively engage with each element rather than waiting for an end-of-briefing "any questions?" that invites checking-out.

### Predicted trade-offs for coding-planning

Direct analog: session opener for a kerf work or design pass. The user currently opens with a dispatch, a dump, or a question; there is no structured pre-phase briefing with mandatory content slots. Medical SBAR (from domain 2) proposes the slot structure; aviation approach-briefing adds the interactive-per-slot-acknowledgment move that turns a brief into a verification exchange.

**Helps with:** rich-brief mode making a one-shot dump that the agent may misinterpret silently. Per-slot acknowledgment converts the brief into a series of mini-verifications.

**Helps with:** capturing specific content categories that tend to be missed (e.g., anomaly response = "what do you want me to do if I discover X during execution?"). Aviation's anomaly-response slot is explicit; coding-planning's is usually absent.

**Costs:** ceremonial if overused. A 5-minute kerf work doesn't justify an interactive briefing. Probably only applies to significant planning passes (new subsystem, cross-cutting change).

### Adaptation notes

Candidate slot set for a planning-session pre-phase briefing (stackable with SBAR):

- **Intent:** what this session is trying to produce.
- **Constraints:** which previously-locked decisions still apply (and must not be reopened).
- **Scope boundaries:** what's explicitly in / out.
- **Callouts:** what the agent should surface along the way (matches the fixed-token status markers).
- **Anomaly response:** what to do if the agent finds something that contradicts the stated frame.

Each slot gets an agent-acknowledgment turn before the next slot. Stops being a dump; becomes a verified frame.

### Evaluation plan

Run a kerf work opener in this structured form and measure (a) frame-drift events later in the session, (b) session-opener writing cost vs. the user's current dispatch style, (c) whether the "anomaly response" slot actually fires during the session.

## SOP challenge-response callouts

### Description

Standard operating procedures in aviation are executed as **spoken call-and-response**, not silent tick-lists. "Gear down" / "Gear down, three green." "Flaps 30" / "Flaps 30, set." The challenging crew member names the item; the responding crew member verifies the system state and announces the verification.

The challenge-response form is load-bearing: a silent checklist where the crew visually ticks items is demonstrably less reliable than the spoken version, because the speech-production + speech-reception loop engages different cognitive resources than silent reading, and the spoken verification is externally observable (the other crew member, the CVR) rather than self-reported.

### Dimension values

- **Timing of alignment:** at-decision-point (each item is a mini-alignment)
- **Decision locus:** mixed — challenger names, responder verifies
- **Dialog form:** fixed-vocabulary short-volley
- **Question style:** implicit (the challenge *is* the question)
- **Autonomy scope:** bounded to the named item
- **Plan expression form:** checklist artifact consumed via spoken protocol
- **Knowledge direction:** peer verification
- **Review/critique integration:** per-item

### Mechanism

Spoken challenge-response is a forcing function for attention. Silent ticks are compatible with automaticity; spoken verification is not. The practice is expensive in attention and deliberately so — it surfaces configuration errors that automaticity would skip.

### Predicted trade-offs for coding-planning

Coding-planning has a silent-tick problem: specs and task lists are marked done without verification. The kerf "square" step is a batched version of a challenge-response pass, but currently executed as a single late-stage check, not as per-item spoken verification during execution.

**Helps with:** completeness verification in a way that doesn't require a second reviewer. A per-item challenge-response between user and agent at a check gate could catch partial completion.

**Costs:** attention expensive; only justifiable at decision gates, not continuously.

### Adaptation notes

Candidate application: at the end of a planning pass, instead of "I've finished the spec, here it is," the agent runs a challenge-response pass over the spec's load-bearing claims: "Constraint: spec is normative. Covered? User: covered, section 3." Each item's status is explicitly asserted and acknowledged. Heavier than SBAR's inline readback, lighter than a full formal review.

### Evaluation plan

Run a challenge-response-style completion gate on a completed spec and compare surfaced gaps against a matched silent-review control. Measure gaps found and per-item cost.

## Incident-driven change catalog (methodological borrow)

### Description

Aviation communication protocols are each traceable to named incidents. The protocol is, in a real sense, an **archive of failure modes encoded as preventive practice**. A non-exhaustive catalog useful for planning-protocols design:

| Incident | Failure mode | Protocol change |
|---|---|---|
| Tenerife 1977 | Nonstandard "OK" acknowledgment ambiguated takeoff clearance | "Takeoff" reserved for actual takeoff clearance; "departure" otherwise. Mandatory readback of key instruction items. |
| United 173 (Portland) 1978 | FO noticed fuel critical but failed to escalate through captain's fixation | CRM; graded assertiveness (PACE); authority-gradient training |
| Eastern 401 (Everglades) 1972 | Whole crew fixated on a landing-gear indicator; nobody flying the aircraft | PF/PM role separation; pilot monitoring duties formalized |
| Eastern 212 (Charlotte) 1974 | Idle chatter during approach contributed to altitude loss | Sterile cockpit rule below 10,000 ft |
| Avianca 052 (Cove Neck) 1990 | FO reported "minimum fuel" but not "emergency"; ATC did not escalate | Standardized fuel-state declarations (minimum fuel, emergency); CRM for ATC |
| KLM 4805 / Pan Am 1736 (Tenerife, same event) | Stress, frustration, disrupted schedule → captain rushed takeoff over FO's hesitation | Explicit authority-questioning training for FO |

### Why this is methodologically important

For the coding-planning protocol design, the aviation catalog is a **template for evidence-driven design**: each protocol element was introduced to address an identified failure mode, not theoretically derived. The planning-protocols track should similarly derive protocol elements from identified failure modes in the Phase 1 corpus — this is approximately what the corpus-signal filter (Task #9) is about.

Concrete catalog-item-shape the planning-protocols track should produce, by analogy:

| Observed misalignment event | Failure mode | Proposed protocol response |
|---|---|---|
| (from Phase 1 corpus) | (class) | (candidate element) |

### Adaptation notes

Not a protocol itself — a **methodological borrow**. When evaluating candidate planning protocols, ask: *which specific observed misalignment event does this protocol prevent?* If no such event exists in the corpus, the protocol is theoretical and should be deprioritized relative to protocols that address a named failure mode.

## Considered and rejected

Several aviation practices were investigated and set aside, either because they do not translate or because they duplicate mechanisms more cleanly captured elsewhere.

**Phonetic alphabet (Alpha-Bravo-Charlie...)**: solves a problem — transmission noise in radio channels — that coding-planning does not have. Text is already unambiguously transmitted. Rejected.

**Mandatory callsign prefixing every transmission**: aviation needs it because many aircraft share one frequency. In coding-planning, the channel is one-to-one user↔agent. No translation needed. Rejected.

**Full read-back verbatim of every instruction**: adapted-down to "read back load-bearing tokens only," see §Readback. Full verbatim does not transfer at coding-planning vocabulary sizes.

**CRM's "advocacy and inquiry" coaching model**: overlaps substantially with PACE (graded assertiveness) and with Socratic-method protocols from domain 3. Captured by those.

**Commander's intent doctrine** (imported into CRM from military): will be captured in the military-briefings domain file. Not duplicating here.

**Crew briefing's "any questions?" closer**: explicitly called out in CRM literature as **ineffective** compared to interleaved slot-by-slot acknowledgment. Rejected as anti-pattern; its absence is the point. Worth flagging — the user's observed "numbered questions at end of turn" pattern is an instance of this anti-pattern by the aviation literature's lights. Worth investigating whether the Phase 1 observation of numbered-question-close shortening human turns generalizes, or whether interleaved slot-acknowledgment would work better.

**Formal position-status reports ("FL350, heading 270, squawking 2345")**: aviation uses these for coordination across invisible parties. The coding-planning analog — periodic agent status reports — is not ill-conceived, but the mechanism is covered by the SBAR-style synthesis and the per-turn readback; a separate periodic status report adds bulk without new information.

**Hull-loss anonymization in incident reports**: a meta-practice (investigators publish without blaming individuals) that improves the quality of incident data available for protocol design. Not itself a protocol, but worth borrowing as a principle for the planning-protocols research corpus handling: when capturing observed misalignment events, structure them to describe the protocol failure rather than the participant, so the catalog remains usable as design input rather than becoming a critique of specific sessions.

## Summary of candidates for the unified catalog

| Candidate | Primary failure mode addressed | Evaluation priority |
|---|---|---|
| Load-bearing-token readback (per-turn) | Vocabulary/framing errors, silent misinterpretation drift | High — addresses Phase 1's most expensive error class |
| Fixed-token status vocabulary | Decision-closure ambiguity, parked-item loss | High — lightweight, mechanically checkable, may be a new dimension value |
| Graded assertiveness (Probe/Alert/Challenge) | Agent silent deferral; batched questions | High — unexplored-region §5 "agent-investigative" filler |
| Focus-phase / sterile-phase rule | Tangent-induced context-switch load | Medium — protocol-shape-shifting §5 |
| Authority-transfer micro-ritual | Ambiguous decision ownership per item | Medium — complements upfront partition |
| Pre-phase briefing with slot-acknowledgment | Frame drift from silent-briefing dump | Medium — stackable with SBAR |
| SOP challenge-response completion gate | Silent-tick completeness errors | Low-medium — heavy; only at gates |
| Incident-driven catalog (methodological) | Protocols-without-evidence | High (as method) — informs the corpus-signal filter, Task #9 |

## Sources

- [ICAO Doc 9432 Manual of Radiotelephony (4th ed. 2007)](https://www.ealts.com/documents/ICAO%20Doc%209432%20Manual%20of%20Radiotelephony%20(4th%20ed.%202007).pdf)
- [SKYbrary — Standard Phraseology](https://skybrary.aero/articles/standard-phraseology)
- [SKYbrary — Pilot-Controller Communications (OGHFA BN)](https://skybrary.aero/articles/pilot-controller-communications-oghfa-bn)
- [ICAO APAC ATM/SG/13 — Importance of ATC Readback and Hearback](https://www.icao.int/sites/default/files/APAC/Meetings/2025/2025%20ATMSG13/04-Information%20Papers/IP06%20Importance%20of%20ATC%20Readback%20and%20Hearback%20%20.pdf)
- [Wikipedia — Tenerife airport disaster](https://en.wikipedia.org/wiki/Tenerife_airport_disaster)
- [Flight Safety Foundation — Failure to Communicate (Tenerife)](https://flightsafety.org/asw-article/failure-to-communicate/)
- [Aviation Geek Club — The story of UA 173, which launched CRM](https://theaviationgeekclub.com/the-story-of-united-airlines-flight-173-the-plane-crash-that-launched-the-crew-resource-management-revolution-in-airline-training/)
- [FAA — The Evolution of CRM Training in Commercial Aviation (Helmreich et al.)](https://www.faa.gov/sites/faa.gov/files/2022-11/crmhistory.pdf)
- [Psych Safety — PACE: Graded Assertiveness](https://psychsafety.com/pace-graded-assertiveness/)
- [gCaptain — Graded Assertiveness: Captain, I Have a Concern](https://gcaptain.com/graded-assertiveness-captain-i-have-a-concern/)
- [The Human Diver — Navigating the Authority Gradient](https://www.thehumandiver.com/blog/navigating-the-authority-gradient-pt2)
- [Wikipedia — Sterile flight deck rule](https://en.wikipedia.org/wiki/Sterile_flight_deck_rule)
- [14 CFR § 121.542 — Flight crewmember duties](https://www.law.cornell.edu/cfr/text/14/121.542)
- [NASA ASRS — The Sterile Cockpit](https://asrs.arc.nasa.gov/publications/directline/dl4_sterile.htm)
- [SKYbrary — Flight Preparation and Conducting Effective Briefings](https://skybrary.aero/articles/flight-preparation-and-conducting-effective-briefings-oghfa-bn)
- [SKYbrary — Normal Checklists and Crew Coordination](https://www.skybrary.aero/index.php/Normal_Checklists_and_Crew_Coordination_(OGHFA_BN))
- [AOPA — I've got it, you've got it, who's got it?](https://www.aopa.org/news-and-media/all-news/2023/august/08/training-and-safety-tip-ive-got-it-youve-got-it-whos-got-it)
- [FAA Lessons Learned — Avianca 052](https://www.faa.gov/lessons_learned/transport_airplane/accidents/AVA052)
- [Wikipedia — Crew resource management](https://en.wikipedia.org/wiki/Crew_resource_management)
- [ICAOspeak — What Is ICAO Radiotelephony Phraseology?](https://icaospeak.com/what-is-icao-radiotelephony-phraseology-aviation-phraseology-safety-and-ai-innovations/)
