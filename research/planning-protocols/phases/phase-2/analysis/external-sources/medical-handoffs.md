# External Source: Medical Handoffs

**Phase 2 · External-Source Pass · Domain 2**
**Author:** research sub-agent
**Date:** 2026-04-23

## Domain intro

Medical handoffs — the structured transfer of patient care between clinicians at shift change, transport, or escalation — are arguably the most-studied structured-information-transfer protocol in any domain outside aviation. The stakes are concrete and measured: a Joint Commission analysis attributed roughly two-thirds of sentinel events to communication failures, and a substantial portion of those to handoff-specific breakdowns. Over four decades, the field has evolved from improvisational narrative ("sign-out") to formal bundles (I-PASS, SBAR) with mnemonic structure, required read-back, and written-plus-verbal redundancy.

Two features of the medical-handoff literature make it directly relevant to human-agent planning protocols:

1. **It is a forcing function for load-bearing structure.** Each component of SBAR and I-PASS survives in the protocol only because dropping it produces a measurable class of errors. This is empirical category discovery, not theoretical decomposition — useful as a template for deciding which categories of a planning-session opener are load-bearing.

2. **It has quantified the verbal / written / combined trade-off.** Retention studies give us something rare: numeric evidence for a dialog-form decision. Verbal-only handoffs retain 0–26% of patient information after five cycles; combined verbal + pre-printed written retain 96–100%. The decay curve alone is worth importing as a design constraint.

The disanalogies matter too. Medical handoffs are bounded in time (~3–15 minutes), occur between two trained peers who share ontology and vocabulary, and have legal / compliance overhead that a solo developer's planning session does not. The **alignment-preservation mechanisms** — the parts that protect shared understanding across a cognitive-context boundary — transfer cleanly; the ceremony does not.

### What this domain offers the planning-protocols track

- A **session-opener template** with empirically-tested load-bearing categories (SBAR, I-PASS).
- A **read-back / synthesis-by-receiver** move that is distinct from "review after draft" — it is inline, interleaved, and catches a different class of error.
- A **graceful-concern-escalation** protocol (CUS words) that gives the subordinate-authority party a scripted way to surface worry without direct confrontation — plausibly invertible to let an *agent* flag concerns without either deferring or over-asserting.
- A **failure-mode taxonomy** (omission, assumption, ambiguity, distortion, overload, funneling) that maps cleanly to the user's existing pain points (agent misaligned assumptions, agent deferring trivial decisions).

## SBAR — Situation / Background / Assessment / Recommendation

### Description

SBAR is a four-category structured template for communicating a clinical situation to another provider. Originally developed by the US Navy for nuclear submarine communication, adopted in healthcare by Kaiser Permanente in the 1990s, and now the most frequently cited handoff mnemonic (69.6% of handoff-literature studies per Riesenberg et al.). It is often used for *single-event* communication (nurse calling a physician about a deteriorating patient) rather than full shift handoff, which distinguishes it from I-PASS.

- **S — Situation**: The immediate issue. One or two sentences. "What is happening now that required this call."
- **B — Background**: Relevant prior context. Medical history, current medications, trajectory. "What you need to know that led here."
- **A — Assessment**: The communicator's interpretation. "What I think is going on." Explicitly includes uncertainty.
- **R — Recommendation**: The proposed action. "What I think should happen, or what I'm asking you to do."

### Why each element is load-bearing

The categories survived decades of iteration because dropping each produces a signature error class:

- **Drop Situation** → Receiver lacks urgency framing; treats the message as routine when it is acute, or vice versa. Triage error.
- **Drop Background** → Receiver applies default priors that may not fit this patient; wrong treatment selected because unmentioned comorbidity is not accounted for. *Assumption error.*
- **Drop Assessment** → Raw data transfer with no interpretation. Receiver must re-derive the sender's mental model from scratch; often fails to do so under time pressure. *Cognitive-load error.*
- **Drop Recommendation** → Ambiguous ownership of next step. Both parties believe the other will act, or neither knows what action is being requested. *Diffusion-of-responsibility error.*

The interesting design choice is that **Assessment and Recommendation are separated** rather than collapsed. The sender is required to distinguish "here is my interpretation (which you may revise)" from "here is what I propose we do (given my interpretation)." This is a load-bearing separation because it lets the receiver correct the interpretation without having to also re-propose the action.

### Dimension values

- **Timing of alignment:** pre-action (before the receiver acts) → in-action (during a call where action may be immediate)
- **Decision locus:** mixed — sender proposes, receiver may accept, revise, or override
- **Dialog form:** short-volley, structured
- **Question style:** implicit (categories *are* the questions); receiver may ask follow-ups after the R
- **Autonomy scope:** bounded-by-category (the four slots)
- **Context richness:** rich-brief (all four categories, compressed)
- **Plan expression form:** structured-artifact (the four-slot template itself)
- **Knowledge direction:** human-dominant (sender informs receiver) but with expected receiver pushback at A and R
- **Review/critique integration:** none formally — SBAR does not mandate read-back, which is part of why it tests "mixed" in outcomes

### Mechanism

SBAR's primary work is **compressing priors into transmissible form**. The receiver would, without structure, have to ask a series of questions to get to "what do you think is going on, what do you want me to do?" — and under time pressure often skips that interrogation. SBAR front-loads the compressed answer so the receiver can use the limited-bandwidth window to *challenge* rather than *gather*.

Error classes caught: omission (by checklist effect), ambiguity at the action layer (R makes the ask explicit), and the most important one — **deferred-decision ambiguity**. Without R, the sender often drops the conversation with "I thought you should know" and no action is named; the receiver then either over-acts (treats as request) or under-acts (treats as FYI). R forces disambiguation.

### Predicted trade-offs for coding-planning

**Helps with:** agent deferring trivial decisions. If the session opener forces the human to produce an *R* — an explicit proposed-next-action or proposed-constraint — the agent has something concrete to challenge rather than an underspecified situation in which the agent defaults to asking.

**Helps with:** agent misaligned assumptions. *B* forces the human to surface the priors that differentiate this work from default work, which are precisely the things the agent would otherwise infer incorrectly.

**Costs:** SBAR is built for *urgent, short* communications. For a multi-hour planning session, the four-category opener is too thin — it is a conversation-initiator, not a plan. Evidence for SBAR as a full-handoff tool is mixed in the literature; it is most effective when *bundled* with other interventions. This suggests SBAR is a useful *component* of a planning-session opener but not the whole thing.

**Watch for:** the A slot can collapse into R if the human skips interpretation and jumps to proposed action. In medicine this is caught by training; in a solo developer context there is no one to catch it. An agent could be prompted to detect collapsed A/R and ask the human to distinguish.

### Adaptation notes — load-bearing categories for a coding-session opener

Direct analogs:

| SBAR slot | Coding-planning analog | Error prevented |
|---|---|---|
| Situation | What change is being considered right now? What triggered this session? | Misdirected effort on a different problem than the one the human cares about |
| Background | What's true about this codebase / prior decisions / constraints that default assumptions would miss? | Agent-misaligned-assumption — the core pain point |
| Assessment | Human's current interpretation: what they think the shape of the work is, where they're uncertain | Agent treats underspecified problem as specified; human discovers divergence only after draft |
| Recommendation | The human's proposed direction (may be "I don't know, I want to explore" — explicitly) | Agent-defers-trivial-decision — forces the human to name a direction or explicitly disclaim one |

The load-bearing insight is the **A/R separation**. In coding-planning, this maps to separating "here's how I'm framing the problem" (Assessment) from "here's what I want to try" (Recommendation). The human often conflates these, producing a proposal whose underlying framing is unstated. An agent that sees only the Recommendation will optimize within a framing the human never disclosed.

The novel slot that SBAR *doesn't* directly cover but a coding-planning version should add: **Scope/Autonomy** — an explicit statement of how much the agent is authorized to decide unilaterally in this session. Medical handoffs don't need this because the authority structure is standing; coding-planning sessions have variable authority by topic and it is load-bearing.

### Evaluation plan

1. **Observational pass against corpus (Phase 1).** For each planning-session opener in the corpus (the ~15 sessions already coded), mark which SBAR slots are present, absent, or fused. Look for correlation between absent B (Background) and downstream misaligned-assumption errors.
2. **Counter-factual audit.** For sessions that went wrong, ask: would an SBAR-style opener have caught this? Classify by which missing slot would have caught the error.
3. **Prospective micro-trial.** Insert an SBAR-shaped opener as a templated first message in three new planning sessions; compare time-to-alignment (rounds until both parties agree on the problem shape) vs. matched control sessions.
4. **Reject-test.** A/R fusion — does the human actually separate these when prompted? If >50% of prompted openers still fuse, the category is cognitively unnatural and needs redesign.

## I-PASS — Illness severity / Patient summary / Action list / Situation awareness & contingencies / Synthesis by receiver

### Description

I-PASS is the handoff bundle developed by the I-PASS Study Group (Starmer et al., published in NEJM 2014). It is explicitly for *shift-change* handoffs, not one-shot communications. Implementation at nine academic children's hospitals produced a **30% reduction in serious preventable medical errors**, which is the strongest causal evidence any communication protocol has for patient-safety outcomes. I-PASS is therefore the closest-to-"proven" structured-information-transfer protocol we have.

- **I — Illness severity**: Three-tier acuity flag — *stable / watcher / unstable*. "Watcher" is explicitly the receiver-relevant category: "gut feeling that this patient is close to the edge."
- **P — Patient summary**: Diagnoses, trajectory, key events. Narrative, not just list.
- **A — Action list**: To-do items with timelines and explicit ownership ("You, by 0600, will...").
- **S — Situation awareness & contingency plans**: *If / then* statements. "If urine output doesn't resume by 1100, straight cath."
- **S — Synthesis by receiver**: The receiver restates, asks questions, and confirms.

### Why each element is load-bearing

- **Illness severity** is a single-tag summary at the head of the message. Under cognitive load, the receiver may not process the full Patient summary; the one-tag acuity flag survives anyway. The **watcher** tier is the load-bearing innovation — it gives the sender a way to transmit "I can't articulate why but something is wrong" which is otherwise suppressed by the requirement-to-justify norm.
- **Patient summary** is the narrative. It carries the "why we are where we are."
- **Action list** is the action-ownership artifact. The key design rule: each action has an owner and a time. Without both, diffusion of responsibility dominates.
- **Situation awareness & contingency plans** (if/then) is the most-cited innovation in I-PASS. It transfers not just current state but **decision rules for states the sender anticipates**. This is a pre-delegation of judgment: "I'm not going to be here, but here is what I would do in scenarios X, Y, Z."
- **Synthesis by receiver** is closed-loop confirmation. Not optional — the handoff is considered incomplete until the receiver has restated.

### The "watcher" innovation

Worth a paragraph of its own. Pre-I-PASS, illness acuity was binary-ish (stable / unstable). Clinicians knew there was a third state — the patient who doesn't meet unstable criteria but whom the departing clinician is worried about — but had no transmission vocabulary. "Watcher" legitimizes articulated-intuition transfer: the sender's gut is explicitly protocol-valid information. A study by Destino et al. later found "watcher" tier independently predicted overnight deterioration. **The protocol made a previously-invisible category of information transmissible.**

Parallel for agent contexts: is there a "watcher" equivalent — a legitimized channel for the agent to say "I can't articulate why but I think this plan is at risk" without either over-formalizing the concern or suppressing it?

### Dimension values

- **Timing of alignment:** pre-action (before receiver takes ownership), with an explicit synthesis gate
- **Decision locus:** sender has pre-authorized actions on Action list; contingencies are pre-authorized conditional actions; novel decisions are interactive
- **Dialog form:** structured, medium-length, with a mandatory verbal component
- **Question style:** receiver questions at synthesis step (embedded after presentation, not one-at-a-time during)
- **Autonomy scope:** bounded-by-category (pre-authorized on Action list), with incremental escalation via contingency rules
- **Context richness:** recovery-handoff (assumes no shared state; builds it fresh)
- **Plan expression form:** structured-artifact (written) + behavior-list (Action list) + constraint-list (contingencies)
- **Knowledge direction:** human-dominant during presentation, bidirectional at synthesis
- **Review/critique integration:** **read-back** — distinct from peer-review or self-review

### Mechanism

I-PASS compresses a complex cognitive state (the sender's model of the patient) into transmissible form *and then verifies the compression survived transport*. This second part — synthesis by receiver — is what distinguishes it from SBAR. Error classes caught:

- **Illness severity:** catches under-urgency errors (sender didn't adequately convey acuity)
- **Action list with ownership:** catches diffusion-of-responsibility errors
- **Contingency plans:** catches "novel state I didn't anticipate" errors **for anticipated novel states** — the sender does the scenario-planning in advance rather than the receiver having to do it on the fly
- **Synthesis by receiver:** catches mishearing, misinterpretation, and — this is key — **unshared-priors errors** where the receiver heard the words but integrated them wrong with their own prior knowledge

The synthesis step catches an error class that no passive protocol catches: errors in the *receiver's* integration of the message with their own mental model. The sender didn't err; the receiver misintegrated. Without read-back, this error is undetectable until action time.

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Contingency plans** for a solo developer map to: "here are the branches I want the agent to autonomously follow; here are the branches that require me to weigh in." This is a **decision-delegation artifact**, which directly addresses the "agent defers trivial decisions" pain point. The human pre-authorizes a class of decisions by contingency rather than by item.
- **Synthesis by receiver** maps to the agent reading back its understanding before beginning work. This is a qualitatively different move from "agent proposes plan, human reviews" because it happens *before* the agent has invested work in a draft. Catches misaligned-assumption errors at near-zero cost.
- **Illness severity / watcher equivalent:** a one-tag flag from the human at the top of the session — "this is a routine-shape problem" vs "I am uncertain about the shape" vs "I have a specific concern I can't articulate." Gives the agent an orienting signal before it has processed the full context.

**Costs:**

- I-PASS is heavy. Full protocol takes 5–15 minutes per patient. Coding-planning does not need and cannot afford this for every session. The right move is likely to adopt the **structural moves** (read-back, contingency-as-delegation, severity-tag) rather than the full bundle.
- Contingency planning requires the human to anticipate scenarios. This is itself a cognitive cost. If the contingencies are under-specified, the agent gets false pre-authorization. Needs a safety valve: when the agent encounters a case not covered by any contingency, it must escalate, not guess-and-extend.
- Synthesis-by-receiver changes agent voice. An agent that opens with "so what I'm hearing is..." is a different collaborator than one that opens with "okay, here's the plan." User preference unclear; worth testing.

### Adaptation notes

The five-slot template for a planning-session opener, adapted:

| I-PASS slot | Coding-planning analog |
|---|---|
| Illness severity | Session-shape tag: *routine / watcher / uncertain* |
| Patient summary | What the change is, in prose |
| Action list | What the agent is pre-authorized to do; what requires check-in |
| Situation awareness & contingencies | If-then rules: "if you find X, proceed; if you find Y, stop and surface" |
| Synthesis by receiver | Agent restates its understanding before beginning |

The **synthesis** step deserves to be a dimension value of its own (see below) because it is structurally different from other forms of review integration.

### Evaluation plan

1. **Isolate the active ingredients.** The full I-PASS bundle is too heavy; test each move separately:
   - (a) Severity tag only (one line at session opener)
   - (b) Read-back only (agent restates before drafting)
   - (c) Contingency pre-authorization only (human supplies if-then delegation rules)
   - (d) All three combined.
2. **Error-class audit.** For each observed planning error in the corpus, ask which I-PASS move would have caught it. Hypothesis: (b) catches misaligned-assumption, (c) catches defer-trivial, (a) catches framing errors early.
3. **Cost audit.** Measure keystroke / token cost of each move. Read-back is cheap for the agent, moderate for the human (they must read the restatement). Contingency pre-authorization is expensive upfront but amortizes across the session.
4. **Watcher transfer test.** Does a human-solo developer actually use a "watcher" tier if offered, or does it collapse to binary? If it collapses, the category is not load-bearing in this context.

## Teach-back / Read-back

### Description

Read-back is the receiver-restates-the-message move. In medicine it appears in two contexts:
- **Clinician → clinician** (synthesis by receiver in I-PASS; closed-loop communication in resuscitation)
- **Clinician → patient** (teach-back: patient restates discharge instructions)

Aviation, where the technique originated, mandates read-back for every air-traffic-control clearance. NASA's Aviation Safety Reporting System documents that vague acknowledgments ("okay," "roger," mic clicks) are correlated with miscommunication incidents — full read-back is the protocol.

### Why it works — error classes caught

Read-back catches a specific class of errors that *no other protocol step catches*:

1. **Transmission errors** (sender said X, receiver heard Y). Structured-form protocols don't catch these; only read-back does.
2. **Integration errors** (receiver heard X correctly but integrated it with their prior knowledge wrong — e.g., "1 mg epinephrine" heard correctly but the receiver's prior is that the standard dose is 0.1 mg, so they mentally corrected it to 0.1).
3. **Assumption errors on the receiver side** (the receiver filled in unspecified details with defaults the sender didn't intend).

In pediatric trauma settings, one study found that without closed-loop protocol, up to 51% of critical orders went *unacknowledged*. Not misunderstood — unacknowledged. The sender didn't know whether the receiver had even heard. Read-back turns this into binary observable state.

### Cost

Teach-back is consistently described as "low-cost, low-technology." Per-interaction time cost is small (30–60 seconds). The implementation cost is mostly cultural: trainees experience read-back as insulting (as if their competence is being questioned) until habituated. Studies show training and repeated practice convert it from a confrontational to a routine move.

### Dimension values

- **Timing of alignment:** in-action (interleaved, not post-draft)
- **Decision locus:** this move itself doesn't decide; it verifies
- **Dialog form:** short-volley
- **Question style:** the receiver's restatement is itself a question ("is this what you meant?")
- **Autonomy scope:** not applicable (read-back is a gating move before autonomous action)
- **Context richness:** minimal (per-move); depends on the message being read back
- **Plan expression form:** N/A
- **Knowledge direction:** bidirectional (sender → receiver → sender confirmation)
- **Review/critique integration:** **read-back** — this is the canonical instance

### Read-back as a distinct dimension value

The evaluation-framework document already flags that read-back may be a distinct dimension value for review/critique integration. Medical evidence strongly supports this. Distinctions from other review forms:

| Form | When it happens | What it catches | Cost |
|---|---|---|---|
| Self-review | After draft, by same party | Internal consistency, obvious omission | Low |
| Peer-review | After draft, by second party | Alternative framings, missed constraints | High |
| Read-back | Before action, by receiver restating upstream message | Transmission + integration + receiver-assumption errors | Very low per instance |

Read-back's distinctive value: **it catches errors that are cheap to fix at the moment of read-back and expensive to fix after action has been taken.** Peer-review on a draft catches errors after investment has been made; read-back catches them before.

### Predicted trade-offs for coding-planning

**Highest-predicted-value move from this entire domain survey.** Direct agent analog:

> Before beginning work, the agent restates: "My understanding is [X]. I am planning to [Y]. I am assuming [Z] — if any of these are wrong, please correct."

This move is:
- **Cheap** — one agent message.
- **Catches the primary pain point** — agent misaligned assumptions.
- **Low ceremony** — no new tooling; just a protocol instruction to the agent.
- **Falsifiable** — we can measure catch-rate (how often the human corrects something in the read-back).

**Watch for:**
- Agent read-back can become ritual if it just parrots the user's words. Medical teach-back has the same failure mode ("the patient said the words back but didn't understand"). The protocol fix is to require **paraphrase not repetition**, and to include the agent's *inferences* (the Z in "I am assuming Z") which are the load-bearing part.
- Read-back is a *gate*. If the human ignores it because it looks like filler, it provides zero protection. The move only works if the human reads the restatement and actively checks it against their intent. Friction-for-friction's-sake is counterproductive; this move only earns its place if humans reliably use the gate.

### Evaluation plan

1. **Catch-rate study.** Instrument: every planning session begins with an agent read-back of the human's opening message, including explicit inference statements ("I am assuming..."). Measure: what fraction of read-backs produce a correction from the human? Hypothesis: >25% produce at least one correction in the first N sessions; declining over time if read-back becomes a habit that shapes the human's openers.
2. **Error-prevention audit.** For planning errors found downstream (in the draft or later), trace back: would a read-back at session opener have surfaced the misalignment? Use existing corpus retrospectively.
3. **Ritualization watch.** Does the agent's read-back quality degrade over sessions (shorter, more parrot-like)? If so, the protocol needs a rule forcing inference-surfacing rather than summary.

## CUS words — Concerned / Uncomfortable / Safety issue

### Description

CUS is a three-step escalation protocol developed as part of the TeamSTEPPS curriculum (AHRQ / DoD). It gives a subordinate-authority party scripted language to escalate a concern to a higher-authority party *without direct confrontation*:

- **"I am Concerned about X"** — first-tier flag. Names the worry.
- **"I am Uncomfortable with X"** — second-tier if dismissed. Strengthens.
- **"This is a Safety issue"** — third tier. Invokes the hard-stop norm: a safety issue must be acknowledged and addressed, not deflected.

The design insight is that direct confrontation ("you are wrong") is culturally costly in steep hierarchies (medicine, aviation) and is suppressed even when warranted. CUS provides **pre-authorized language** — saying "I'm concerned" is socially cheap in a way that "you're making a mistake" is not.

### Why it works — error class caught

CUS addresses the **authority-gradient-induced suppression** error: the lower-authority party sees a problem but does not surface it because the social cost of surfacing it is too high. In healthcare this kills patients; the CRM (crew resource management) literature from aviation has the same story about first officers not challenging captains.

The protocol works by:
1. **Legitimizing** a low-cost first-tier move ("concerned") that doesn't require certainty or confrontation.
2. **Providing graduated escalation** so the speaker doesn't have to jump to "safety issue" for any concern, which would make the strong move inflationary.
3. **Creating a receiver obligation** at the "safety issue" tier — the higher-authority party is protocol-bound to acknowledge, not deflect.

### Dimension values

- **Timing of alignment:** in-action / continuous (CUS can be invoked at any point)
- **Decision locus:** pre-authorized (the speaker is pre-authorized to escalate; the receiver must acknowledge)
- **Dialog form:** short-volley, structured
- **Question style:** embedded (CUS is inline in a larger conversation)
- **Autonomy scope:** N/A (CUS is a signaling move, not a scope-setter)
- **Context richness:** minimal per-move
- **Plan expression form:** N/A
- **Knowledge direction:** **agent-initiated in the medical analog** — this is the rare protocol where the sender is not the authority figure
- **Review/critique integration:** continuous critique with escalation

### The inversion that makes this interesting for agent work

Most handoff protocols assume the authority-carrying party is the sender (sign-out by departing clinician). CUS is different: it empowers the *receiver* or *subordinate* to interrupt the authority-carrier's plan.

In the human-agent analog, **the agent is the lower-authority party** (the human has final say on direction). The CUS pattern can be inverted to give the agent a legitimized channel to surface concerns about the human's plan without either:
- Capitulating silently (the "agent defers trivial decisions" pain point in one form), or
- Over-asserting in a way that feels confrontational.

### Predicted trade-offs for coding-planning

**Agent-side CUS** would look like:

- Tier 1 (Concerned): "I'm noticing X — flagging in case it matters to you."
- Tier 2 (Uncomfortable): "I'm uncertain about proceeding because of X — can you confirm?"
- Tier 3 (Safety): "This looks like it would [lose data / break the spec / contradict a locked decision]. I won't proceed without confirmation."

**Helps with:**
- Giving the agent scripted language to flag concerns at the *right intensity* — not every concern is a hard stop.
- Making tier 3 hard stops observable and unambiguous (an agent saying "this is a safety issue" is a protocol-level red flag, not a rhetorical flourish).
- Addressing the observed pattern where agents either silently accommodate or rigidly block.

**Costs:**
- Tier inflation. If the agent overuses tier-3, it loses meaning. Medical CUS has the same problem — overuse of "safety issue" gets ignored. Needs a threshold definition.
- In a solo developer context, there is no one to arbitrate disputes. If the human dismisses a tier-3 agent concern, the protocol has to define what happens — agent proceeds under protest, agent refuses, agent requires a documented override. Medical protocols have institutional backing for this; agent protocols would need explicit rules.
- Writing an agent that uses CUS tiers *well* (not over-inflating, not under-flagging) may require per-tier training or explicit threshold rules.

### Adaptation notes

A novel contribution to the planning-protocols catalog: **graduated agent-concern surfacing** as a dimension. Most existing agent protocols treat agent concerns as binary (proceed / ask). CUS suggests a three-tier (or N-tier) gradient where each tier has different expected human responses.

This also connects to the "watcher" tier in I-PASS. Both are **legitimized articulated-intuition channels** — protocol-valid ways to transmit "something feels off" without having to fully justify it. The design insight in both cases is that **suppressing unarticulated concerns is a systematic error**; protocols that create transmission vocabulary for them outperform protocols that require full justification before surfacing.

### Evaluation plan

1. **Instrument agent output.** Classify agent messages as: silent-accommodate / tier-1-flag / tier-2-concern / tier-3-hard-stop / other. Look at current corpus (pre-protocol) for natural baseline distribution.
2. **Tier-calibration test.** After introducing agent-side CUS, measure: does the agent distribute across tiers sensibly, or collapse to one (likely tier 1 or silence)?
3. **Human-response rate by tier.** Does the human actually respond differently to tier-3 than tier-1? If not, the tiers don't add signal; they just add ceremony.
4. **False-positive rate.** How often does the agent raise a tier-3 concern that turns out to be unfounded? Tier-3 inflation is the primary failure mode.

## BATON and other mnemonic variants

### Description

The handoff-mnemonic literature is thick. Riesenberg et al.'s systematic review found 24 mnemonics in 46 articles. A scoping review by Panesar et al. found more than 30. SBAR dominates citations (69.6%) but is not the only survivor. Brief catalog of notable variants:

- **I PASS the BATON** (DoD TeamSTEPPS) — Introduction, Patient, Assessment, Situation, Safety concerns, Background, Actions, Timing, Ownership, Next. Expanded SBAR with explicit safety-concern and ownership slots.
- **ANTICipate** (UK, ward-based) — Administrative data, New information, Tasks, Illness, Contingency plans.
- **JUMP** — Jobs outstanding, Unseen patients, Medical contacts, Patients to be aware of. Ward-handover focused.
- **SHARED** — Situation, History, Assessment, Request, Evaluate, Document. SBAR-adjacent with explicit documentation step.
- **iSoBAR** (Australia) — Identify, Situation, Observations, Background, Agreed plan, Read back. Explicitly includes read-back as a slot.
- **PSYCH** — psychiatric-specific variant.
- **5Ps** — Patient, Plan, Purpose, Problems, Precautions. Nursing-variant, commonly used.
- **HATRICC** — Handoffs and Transitions in Critical Care. Bespoke for OR-to-ICU transitions.
- **Traffic Light tool** — UK; a simulation study found it "yielded more accurate information transfer, took less time to use, and was preferred" over SBAR. Worth a closer look; uses color-coded acuity (analog to I-PASS severity).

### What the variants tell us

The proliferation of mnemonics is **not a sign of chaos**; it is a sign that the four-to-seven-slot structured template is a robust shape, and different settings need slightly different slots. Patterns across variants:

1. **Every variant has a Situation / Summary slot.** Unambiguous shared starting point. Load-bearing.
2. **Every variant has an Action / Plan slot.** Ownership of next step. Load-bearing.
3. **Most variants include acuity / severity.** Either explicit (I-PASS illness severity, Traffic Light) or embedded in Situation.
4. **Only some variants include contingency plans.** I-PASS, ANTICipate, I PASS the BATON. Those that include it are the ones with the strongest outcome data.
5. **Only some variants include read-back.** iSoBAR (explicit), I-PASS (explicit). Those that include it have stronger outcome data than those that don't.

The pattern: **contingency planning and read-back are the differentiators of the high-outcome variants.** They are the two moves most likely to transfer to coding-planning.

### Dimension values (aggregate)

Rather than per-variant, the shared structure:

- **Timing of alignment:** pre-action
- **Decision locus:** mostly mixed (sender proposes, receiver accepts/amends)
- **Dialog form:** structured, medium-length
- **Question style:** implicit via categories
- **Autonomy scope:** bounded-by-category
- **Context richness:** rich-brief or recovery-handoff
- **Plan expression form:** structured-artifact
- **Knowledge direction:** human-dominant to bidirectional
- **Review/critique integration:** none / read-back (depending on variant)

### Adaptation notes

The interesting question is not "which mnemonic" but "which slots must be present." Based on cross-variant analysis, a **minimum load-bearing set** for coding-planning appears to be:

1. Situation / task framing
2. Background / relevant priors
3. Assessment / framing-of-the-problem
4. Action / proposed direction
5. Contingency / if-then rules for agent autonomy
6. Read-back / synthesis

This is essentially I-PASS with an Assessment slot added from SBAR.

### Evaluation plan

Not run as a separate evaluation; folded into I-PASS and SBAR evaluation. The variant survey is primarily here to **establish that the slot structure is convergent across 30+ iterations** — which is stronger evidence for the load-bearing claim than any single mnemonic's outcome data.

## Verbal vs. written vs. combined handoff

### Description

The dialog-form decision in medical handoffs has been studied empirically. Representative findings from Pothier et al. (2005, often cited) and related studies:

| Method | Information retained after 5 handoff cycles |
|---|---|
| Verbal only | 0–26% |
| Verbal + note-taking | 31–58% |
| Verbal + pre-printed handout | 96–100% |

The mechanism: verbal-only handoffs decay rapidly because the receiver is simultaneously encoding and parsing, cannot replay, and has no reference artifact to verify against. Note-taking during verbal catches a modest amount but is bottlenecked by the receiver's writing speed and attention division. Pre-printed handout (written + verbal) is near-complete because the receiver can reference the written form asynchronously and the verbal layer fills in emphasis and tacit context.

**Verbal-only handoffs also introduce errors** — not just lose information. The cited studies found that verbal-only methods generated *incorrect* data in the receiver's notes (details the sender never stated but the receiver inferred). Written forms showed near-zero inserted-error rate.

### Dimension values

Not a protocol; a parameter of other protocols. Relevant dimension: **dialog form** and **plan expression form**.

### Mechanism

The verbal-only decay is a working-memory overflow problem. The receiver cannot simultaneously hold the sender's message, integrate with priors, and verify against own knowledge. Written form offloads storage; verbal form provides emphasis and tacit signaling. Combined uses each for what it is good at.

### Predicted trade-offs for coding-planning

Coding-planning is already primarily written (agent and human exchange text). The question becomes: **which portions should be structured written (artifact) vs. narrative written (conversation)?**

The medical evidence suggests:
- **Structured written** (the pre-printed form analog): better for reference, lower error rate, near-complete retention.
- **Narrative written** (verbal analog): better for emphasis and tacit context.
- **Combined** (both forms coexisting): better than either alone.

Adapted prediction: a planning session that produces *both* a structured artifact (e.g., an SBAR-shaped opener, an I-PASS-shaped read-back) *and* unstructured conversation around it outperforms either form alone. The corpus already shows this implicitly — sessions that produce a structured artifact (a commit message, a kerf pass) tend to be the ones the user references later.

### Evaluation plan

1. **Session-artifact audit.** For each planning session in the corpus, classify: did it produce a structured artifact? An unstructured artifact? Both? Neither?
2. **Downstream-retention test.** For sessions where the user later references the output, correlate reference rate with artifact type. Hypothesis: structured-plus-narrative has the highest reference rate.
3. **Inserted-error test.** For verbal-only (rare in this context) or narrative-only sessions, does the agent generate inferences that the human never stated? (Analog to the "inserted error" finding in verbal handoffs.)

## Failure-mode taxonomy

### Description

Across the handoff literature, failure modes cluster into roughly six categories. This taxonomy is not owned by any single paper; it is a synthesis across AHRQ, Joint Commission, I-PASS study, and cognitive-load literature.

| Failure mode | Description | Protocol move that catches it |
|---|---|---|
| **Omission** | Load-bearing information not transmitted | Structured template (SBAR, I-PASS) — checklist effect |
| **Assumption** | Receiver fills in unspecified detail with wrong default | Read-back (forces explicit inference surfacing) |
| **Ambiguity** | Information transmitted but interpretation unclear | Action list with explicit ownership; read-back |
| **Distortion** | Information transmitted wrong (dose wrong, site wrong) | Closed-loop / read-back |
| **Overload** | Too much information; receiver can't process | Severity tag (I-PASS); hierarchical structure (headline → detail) |
| **Funneling** | Information lost progressively across multiple handoffs | Written artifact; each handoff replays from the artifact not from previous handoff |

### Map to planning-session failures

The mapping to the user's observed pain points is clean:

| Planning pain point | Handoff failure mode | Medical protocol move |
|---|---|---|
| Agent misaligned assumptions | Assumption (receiver filling in) | Read-back with inference surfacing |
| Agent deferring trivial decisions | Ambiguity at action layer | Contingency-plan pre-authorization |
| Human over-writing | Overload (agent processing too much) | Severity tag + hierarchical structure |
| Framing drift across long sessions | Funneling | Written structured artifact to anchor back to |

### Evaluation plan

Use this taxonomy as the **coding scheme** for corpus error-classification in Step 3 (counter-pattern) and Step 5 (reviewer-challenged evaluation). When classifying a planning-session failure, tag it with one or more of these six modes; then ask which protocol move would have caught it.

## Considered and rejected

A handful of medical-handoff-adjacent ideas came up in research but were not promoted to candidate protocols, with brief reasoning:

- **Full TeamSTEPPS CRM bundle** — includes many components (brief, huddle, debrief, CUS, call-outs, check-backs, handoff). Too broad to evaluate as a unit; components are already covered individually. Rejected as a gestalt; components kept.
- **Timeout protocols** (pre-surgical pause for site / patient / procedure verification) — a read-back variant specific to pre-action verification in OR. The structural move is already captured under read-back; the OR-specific ceremony doesn't transfer.
- **Sentinel-event root-cause-analysis protocols** — post-hoc investigation frameworks. Post-hoc analysis is a different phase of work than planning; may be relevant to a later track on retrospective-protocols but not to idea-to-plan.
- **Rounds protocols** (bedside rounds, multidisciplinary rounds) — these are *group* coordination rituals with several clinicians present. Coding-planning is dyadic (one human, one agent). Some structural moves (walking a plan through multiple viewpoints) might transfer to agent-internal reviewer sub-agents, but that is a separate design question covered elsewhere.
- **Electronic handoff tools** (Epic's sign-out module, etc.) — technology implementations of the written-form side. Relevant to coding-tool design but not to protocol design.
- **SOAP note structure** (Subjective, Objective, Assessment, Plan) — this is a *documentation* structure for patient encounters, not a handoff protocol. It is SBAR-adjacent and could be considered, but is oriented to documentation-for-later rather than transmission-now; does not add novel structural moves beyond SBAR.
- **Warm handoff / cold handoff distinction** — literature term for face-to-face vs. paperwork-only handoffs. The verbal-vs-written evidence already covers this; not a separate protocol.
- **Ticket-to-ride** (transport handoff form) — bespoke structured artifact for inter-facility transport. Instance of the combined-verbal-written pattern; already covered.

---

## Summary of candidate protocols to carry forward

In rough order of predicted value for coding-planning:

1. **Read-back / synthesis by receiver** — highest predicted value; addresses the primary pain point (agent misaligned assumptions) at near-zero cost; deserves its own dimension-value in review/critique integration.
2. **Contingency pre-authorization (from I-PASS)** — addresses agent-defers-trivial-decisions; expensive upfront but amortizes.
3. **SBAR-shape session opener** — load-bearing category template for what a planning-session opener must contain.
4. **CUS-style graduated agent-concern surfacing** — novel dimension; gives the agent scripted tier-language for flagging concerns.
5. **Severity / watcher tag** — one-line orienting signal at session start; cheap and possibly high-value for routing the rest of the session.
6. **Combined verbal + written (structured + narrative)** — likely already present but worth making explicit as a design commitment.

The failure-mode taxonomy is not itself a protocol but is the proposed coding scheme for error classification across the rest of the track.
