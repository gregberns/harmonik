# External Source: Incident Command Systems

**Phase 2 · External-Source Pass · Domain 5**
**Author:** research sub-agent
**Date:** 2026-04-23

## Domain intro

Incident command is coordination-under-uncertainty across multiple actors, under time pressure, with explicit authority structure and mandatory post-event review. The canonical form is the US fire-service / FEMA **Incident Command System (ICS)**, codified in FEMA ICS-100 through ICS-400 training and adopted federally under the National Incident Management System (NIMS). Its protocols have been adapted twice for adjacent domains: by the US military as **Mission Command** (with the commander's-intent primitive) and, more recently, by software-incident-response shops (Google SRE's IMAG, PagerDuty) where a two-to-four person on-call team plays the roles a 40-person fire camp plays.

Coding-planning sessions are not high-urgency in the way a structure fire or SEV-1 outage is. The *urgency-dependent* parts of ICS (operational tempo, radio discipline, accountability roll-calls) do not transfer. But incident command has also, over decades, produced the cleanest solutions to four problems planning-protocols faces:

1. **Pre-authorized autonomy inside a stated outcome.** Commander's intent is a single-line artifact whose entire purpose is to let subordinates deviate from plans without losing direction.
2. **Objective-first planning at cadence.** The Incident Action Plan (IAP) is a re-drafted written artifact per operational period, structured around objectives, not tactics.
3. **Named role separation under load.** Incident commander / ops lead / scribe / comms lead is the minimum set; the roles exist because combining them causes a specific class of breakdown.
4. **Blameless structured review.** After-Action Review (AAR) has a four-question skeleton that has survived in roughly identical form from the Army to the NHS.

Two features of the ICS tradition make it unusually well-suited as a candidate source:

- **Roles are functionally defined, not headcount-defined.** "The incident commander holds all positions that they have not delegated" (Google SRE). This explicitly accommodates the solo case: one person plays every role until load forces delegation. The protocols survive the collapse.
- **Written artifacts at cadence, not on-demand.** The IAP is produced every operational period whether anything changed or not. This contrasts with the software-design-review tradition where artifacts appear only when triggered by change. The cadence-over-trigger pattern is relevant to a planning-session's checkpoint structure.

### What this domain offers the planning-protocols track

- A **pre-authorized-decision** primitive (commander's intent) directly usable as a session-opener clause.
- A **written-artifact-at-cadence** primitive (IAP per operational period) usable as a structured checkpoint artifact.
- A **named-role-collapse** model for how protocols survive when one actor plays multiple roles (solo dev + LLM agent).
- A **tactical-pause** primitive (deliberate stop when conditions are ambiguous) as a protocol-mandated reassessment trigger.
- A **four-question AAR** skeleton for post-session retrospective — directly importable.
- A **handoff protocol** from Google SRE ("You're now the incident commander, okay?" — requires firm acknowledgment) relevant to session-to-session and agent-to-agent transitions.

## Commander's Intent

### Description

Commander's intent is a one-to-three-sentence statement that captures the *purpose* of an operation, the *end state* that defines success, and (sometimes) a small set of *key tasks* that must not be omitted. It originated in Prussian Auftragstaktik, entered US Army doctrine as the core of Mission Command, and now appears verbatim in FM 3-0 and MCDP 1.

Canonical definition from US Field Manual 3-0: "a clear, concise statement of what the force must do and the conditions the force must establish with respect to the enemy, terrain, and civil considerations that represent the desired end state." The protocol rule is **"the mission and the commander's intent must be understood two echelons down"** — a subordinate separated by two organizational levels from the issuing commander must be able to correctly infer what to do when the plan breaks down.

Structure:
- **Purpose**: why this operation exists in the broader plan.
- **End state**: what success looks like when the force stops fighting.
- **Key tasks**: the two-to-five things that, if omitted, would fail the purpose regardless of other success.

### Why each element is load-bearing

- **Purpose** exists to make the intent *deviation-tolerant*. Subordinates encountering unanticipated situations must make trade-offs; without the purpose they cannot tell which parts of the plan are substitutable.
- **End state** is the *success-completion check*. "When are we done?" has a definite answer.
- **Key tasks** catch the "drift by a thousand deviations" failure mode — an execution that is locally reasonable at each step but collectively abandons the mission.

The three elements are deliberately ordered: purpose before end state before tasks. An execution forced to choose between purpose and tasks must preserve purpose. This is explicit in doctrine.

### Dimension values

- **Timing of alignment:** pre-action (authored before dispatch; referenced in-action when deviation required)
- **Decision locus:** **pre-authorized** — subordinate is pre-authorized to deviate from tactics as long as intent is preserved
- **Dialog form:** structured (one-to-three sentences, not a conversation)
- **Question style:** N/A (intent is a statement, not a question); subordinate-asked clarifications go through separate channels
- **Autonomy scope:** **bounded-by-intent** (a new dimension value — broader than bounded-by-category; broader than bounded-by-contingency; the scope is "anything that preserves intent")
- **Context richness:** minimal (deliberately terse; the intent is meant to survive memory decay)
- **Plan expression form:** structured-artifact (single paragraph, standing document)
- **Knowledge direction:** human-dominant (commander authors; subordinates execute under it)
- **Review/critique integration:** none in-line; reviewed retrospectively via AAR

### Mechanism

Commander's intent solves the **plan-brittleness under contact** problem: any plan sufficiently specific to be actionable will be invalidated the moment conditions deviate from what the plan anticipated. Three responses to this are possible: (a) rewrite the plan each time, which is slow and doesn't scale; (b) execute the plan regardless of reality, which produces disasters; (c) authorize subordinates to substitute tactics while preserving the outcome, which requires a substrate they can reason from.

Commander's intent is substrate (c). The "two echelons down" rule is an enforcement mechanism — it forbids intent so terse that only the author understands it, and forbids intent so specific that it is just the plan under another name. Intent that doesn't survive two transmissions is too ambiguous; intent that reads like tactics is too specific.

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Directly addresses the "agent defers trivial decisions" pain point.** An opener that states purpose + end state + key tasks pre-authorizes the agent to make every decision consistent with them without asking. This is not the same as "you have autonomy" (too broad) or "decide file layout yourself" (too narrow); it is scope-by-outcome.
- **Addresses framing drift across long sessions.** When the agent mid-session begins optimizing for the wrong sub-goal (a named pain point in the corpus), the intent is the anchor to return to. "Is this consistent with the stated end state?" is a cheaper check than "is this consistent with everything the human has said."
- **Composes with other protocol moves.** A session opener can carry an intent statement *and* a read-back (from medical-handoffs) *and* a contingency list (from I-PASS) without overlap — each addresses a distinct failure mode.

**Costs:**

- **Authoring cost is front-loaded on the human.** Writing a good intent statement is harder than writing a vague one; the "two echelons down" rule is painful to apply. Corpus evidence from Phase 1 suggests the user does write opener-style framing; whether that framing satisfies intent-quality criteria is testable.
- **Intent can be under-specified in exploratory sessions.** The domain assumes the commander *knows* the end state. In fuzzy-intent planning sessions, the end state itself is what's being discovered. A protocol that mandates intent-at-opener may collapse exploration into premature commitment. Mitigation: allow "intent = exploratory, the end state is `know which of these three options to commit to`."
- **Intent is hard to verify the agent has internalized.** Medical-handoffs offers the read-back as the corresponding check. The combination (intent + read-back of intent) is probably necessary, not either alone.

### Adaptation notes — solo developer + LLM agent

Direct adaptation: session opener contains a three-slot intent block.

| Mission Command slot | Coding-planning analog |
|---|---|
| Purpose | Why this work exists in the broader project. One sentence. |
| End state | What true-when-done looks like. Testable, not aspirational. |
| Key tasks | Two to four non-negotiable constraints (e.g., "preserve the public API", "must work with the existing lockfile format"). |

The "two echelons down" rule maps to: **intent must be understandable by a fresh session of the same agent, without access to prior turns**. This is a strong constraint because it rules out intent that references unstated context. It is also directly testable: have a second agent session read only the intent and the first result, and ask whether the result satisfies the intent. Failure mode: the second agent says "I can't tell — too much depends on context not in the intent."

For exploratory sessions, intent is **framed as a decision**: "the end state is that I know whether to commit to approach A, B, or C, with the reasoning recorded." This preserves the intent pattern without forcing premature convergence.

### Evaluation plan

1. **Observational pass against corpus (transcript-only harness).** For each planning session, tag whether the opener contains (a) purpose, (b) end state, (c) key tasks. Correlate presence of each slot with the evaluation-framework pair-graph metrics — specifically A1 over-ask count (predicted to drop with intent), M1 framing-correction count (predicted to drop with purpose), and S1 spec-completeness (predicted to rise with end state).
2. **Two-echelons test.** For sampled sessions, extract only the opener. Ask a fresh agent to predict what the final spec would look like from opener alone. Measure gap between prediction and actual outcome — large gap = intent under-specified.
3. **Intent-vs-recommendation disambiguation.** SBAR's A/R separation (medical-handoffs, §SBAR) has the same risk here: humans may fuse intent with proposed solution. Instrument opener classification to detect conflation.
4. **Exploratory-session adaptation test.** In sessions where opening fuzziness-index (F1) is high, does the "intent = a decision" reframing produce measurably better outcomes than either no intent or a forced end-state intent?

## Incident Action Plan (IAP) and the Operational Period

### Description

The IAP is a written plan produced for each **operational period** — a bounded chunk of time (typically 12 or 24 hours) during which a single set of objectives governs work. FEMA's Incident Action Planning Guide specifies the contents: incident objectives (ICS 202), organizational assignment (ICS 203), resource assignments (ICS 204), communications plan (ICS 205), medical plan (ICS 206), and safety message. The whole package is produced through the **Planning P** cycle: initial response → situational awareness → objectives → tactics → resources → plan assembly → operational period briefing → execution → evaluation → back to top.

Core design principle: **management by objectives**. The IAP does not specify tactics; it specifies *what outcomes* must be achieved in the operational period, leaves tactics to section chiefs, and is re-drafted every operational period regardless of whether anything materially changed. This contrasts with trigger-driven planning (new plan only when something breaks) and with plan-for-the-duration (one plan for the whole incident). Per FEMA: "incident objectives must be flexible enough to allow for strategic and tactical alternatives, and set guidance and strategic direction, but do not specify tactics."

### Why each element is load-bearing

The IAP's core elements each prevent a specific error class:

- **Time-boxed operational period** prevents plan rot. Even if nothing changed, the replanning ritual forces re-examination, catching drift before it compounds. This is a structural analog to the "mandatory motivation section" of PEPs — presence of the ritual is enforced regardless of whether the author thinks it's needed.
- **Objectives-before-tactics** catches the tactics-then-justify anti-pattern: without objectives first, tactical choices are made for local reasons and justified post-hoc.
- **Written-not-verbal** uses the verbal-vs-written trade-off documented in medical-handoffs: verbal-only retention is 0–26%; written+verbal is near-complete. Incident command converged on the same answer from a different domain.
- **Separate safety message** catches the "safety is everyone's responsibility therefore nobody's" pattern by explicitly assigning a slot.
- **Operational period briefing** is the transmission step — not just the artifact but the spoken walk-through, allowing synthesis-by-receiver on questions.

### Dimension values

- **Timing of alignment:** pre-action (artifact produced before each operational period; re-produced on cadence)
- **Decision locus:** mixed (command sets objectives; section chiefs decide tactics; objectives pre-authorize tactical autonomy within scope)
- **Dialog form:** structured-artifact + briefing (written + verbal, per above)
- **Question style:** implicit at artifact level (the slots are the questions); explicit at briefing (synthesis step)
- **Autonomy scope:** bounded-by-objectives (each section chief is authorized to choose tactics achieving assigned objectives)
- **Context richness:** rich-brief (compressed into the form skeleton)
- **Plan expression form:** structured-artifact (heavy)
- **Knowledge direction:** human-authoritative, distributed-authored (different sections produced by different chiefs, assembled by Planning section)
- **Review/critique integration:** cadence-triggered (end of operational period feeds into next IAP); AAR handles post-incident

### Mechanism

IAP's primary work is **cadence-enforced replanning**. The ritual runs regardless of whether the commander thinks a new plan is needed. This is a deliberate suppression of the "nothing changed, no need to replan" judgment — because that judgment is itself unreliable under fatigue and load. FEMA's operational-period design explicitly accounts for the fact that commanders under sustained load *cannot correctly tell* when replanning is needed, so the protocol substitutes a cadence rule.

Error classes caught:
- **Plan drift.** Fresh IAP each period forces comparison to actual state.
- **Untransmitted updates.** A section chief's local adjustments get incorporated into the formal plan on next cycle rather than living only in that chief's head.
- **Scope creep.** Objectives as explicit artifact make scope changes observable: "we didn't have this objective last period; is that deliberate?"
- **Silent assumption decay.** The safety message slot forces re-examination of hazard assumptions each period.

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Cadence-over-trigger checkpointing.** A long planning session that mandates a mid-session structured checkpoint artifact — not when "something changed" but at a cadence (e.g., after N decisions, or after M minutes of agent-autonomous work) — catches the drift the user identified as a pain point in long sessions. This is structurally distinct from read-back (which is before agent action) and from after-draft review (which is post-work).
- **Objectives-before-tactics** maps to a session-opener requirement that the human states *outcomes to achieve*, not *steps to take*. Agent is then pre-authorized to choose steps. Corpus evidence (Phase 1) shows humans often provide steps-framing, then dispute agent's execution of those steps; an objectives-only opener predicts lower framing-correction count.
- **Operational-period-briefing (synthesis-before-execution)** is the same move as I-PASS synthesis-by-receiver from medical-handoffs, but applied at every period boundary rather than only at opener. For multi-phase planning sessions this is plausibly high-leverage.

**Costs:**

- **The full IAP is far heavier than any coding-planning session can bear.** The adaptation is objectives + safety/constraint slot + synthesis; the rest (logistics, comms plan, medical plan) is team-ceremony that does not transfer.
- **Cadence-forced replanning has a cost-asymmetry problem at solo scale.** FEMA runs at multi-hundred-person scale where the cost of mandatory replanning is absorbed; at solo scale, the replanning ritual may consume more human time than it saves. Mitigation: tie cadence to *agent* work-units (e.g., every N agent turns, or every structured sub-task completion) rather than wall-clock, so the cost is paid by the agent not the human.
- **Defining the operational period for planning is not obvious.** Incidents have natural shift boundaries; planning sessions don't. Candidate mappings: one period = one kerf pass; one period = one major sub-question resolved; one period = up to N agent-turns. Each has different trade-offs.

### Adaptation notes

Minimum adapted IAP for coding-planning — a "session-period artifact" produced at each cadence point:

| FEMA IAP slot | Adapted slot |
|---|---|
| Incident objectives (ICS 202) | Objectives for this period: bulleted, outcome-framed |
| Organizational assignment (ICS 203) | Who decides what: which decisions human-reserved, which agent-authorized |
| Safety message | Constraints that must not be violated this period (data loss, spec contradiction, locked-decisions) |
| Situation awareness | Current state summary (two to three sentences) |
| Operational period briefing | Agent synthesis-by-receiver before the period begins |

The "period" for a planning session is a design choice; worth testing variants. A defensible default: **one period = up to N agent-turns of autonomous work, or until a pre-declared decision point, whichever first**. At period boundary, the cadence artifact is regenerated.

This pattern composes with commander's intent: **intent is standing (lives for the whole session); IAP is per-period (refreshed at cadence).** Intent is the answer to "what does success look like?"; IAP is the answer to "what are we doing this next chunk?" Both are needed because the first is stable and the second is not.

### Evaluation plan

1. **Corpus audit.** For long planning sessions (high T1 active-wall-clock), identify natural period boundaries post-hoc. Check whether an IAP-style artifact at each boundary would have caught known drift events.
2. **Compare cadence rules.** Simulation sweep: same planning problem, agent produces mid-session artifact at (a) agent-judged trigger, (b) every 10 turns, (c) every named sub-task completion. Measure S1 spec-completeness proxy downstream.
3. **Minimum-viable IAP.** Test whether the minimum form (objectives + constraints + synthesis) produces the same drift-catch rate as the full form. Hypothesis: synthesis is the majority of the value; the rest is ceremony.
4. **Objectives-only opener test.** A/B against recommendation-style opener (directly the SBAR-R slot). Does an objectives-only opener produce fewer framing-corrections at equal plan quality?

## Named Roles and the Solo-Collapse Rule

### Description

Incident command specifies a set of roles — **Incident Commander, Operations, Planning, Logistics, Finance/Administration** — with a key protocol rule that both FEMA and Google SRE state explicitly: **the commander holds all positions not delegated**. Google's phrasing: "De facto, the commander holds all positions that they have not delegated." PagerDuty: "It is not intended that every role be filled by a different person for every incident."

The roles most relevant at small-team (and therefore solo) scale, extracted from Google SRE and PagerDuty:

- **Incident Commander (IC):** Single source of truth; decides; delegates. Not a resolver.
- **Operations Lead (OL) / Subject Matter Expert:** The only party who modifies the system. "The operations team should be the only group modifying the system during an incident."
- **Scribe:** Tracks timeline, decisions, hypotheses, evidence. Not a decider; not a resolver.
- **Communications Lead:** Issues periodic structured updates.

Span-of-control rule: ICS doctrine specifies no one supervisor should have more than 3–7 direct reports (5 optimal). This is team-scale but has a solo analog — see below.

### Why role separation is load-bearing

Each role exists because combining it with another produces a signature failure:

- **IC + OL** collapses: commander becomes absorbed in fixing and stops coordinating. Loss of high-level state.
- **IC + Scribe** collapses: commander becomes absorbed in documentation and stops deciding. Timing lag on decisions.
- **OL + Scribe** collapses: nobody records what was done, because the only person doing it is also doing it. Postmortem fails.
- **No comms lead** collapses: stakeholders interrupt the IC, breaking focus.

The roles are **load-shedding structures**, not org-chart entries. Their function is to ensure a single cognitive mode is protected from intrusion by others.

### Dimension values

- **Timing of alignment:** continuous (roles operate throughout; they are not a handoff)
- **Decision locus:** **role-partitioned pre-authorization** — IC decides high-level; OL decides tactical; others have no decision authority on their own
- **Dialog form:** role-typed short-volleys (OL's CAN reports — Condition/Actions/Needs — are structured; Scribe's logs are structured; IC's directives are short)
- **Question style:** role-dependent
- **Autonomy scope:** **scoped-by-role** (each role has a bounded action space)
- **Context richness:** varies; IC holds the most, Scribe holds the most durable
- **Plan expression form:** mixed
- **Knowledge direction:** multi-directional with clear information flow (OL → IC, IC → all, Scribe ← all)
- **Review/critique integration:** continuous (OL's CAN reports and Scribe's logs feed IC's decisions)

### Mechanism and the solo-collapse rule

The core mechanism is **cognitive-mode protection**. Each role defends one mode (deciding / fixing / recording / communicating) against contamination by others. When modes contaminate, the mode that loses is usually deciding — because fixing and communicating feel more urgent. The protocol formally prevents this contamination.

The solo-collapse rule — "commander holds all positions not delegated" — is what makes this transfer to solo contexts. It says explicitly: the *roles* survive even when the *role-count per person* collapses. A solo responder is IC + OL + Scribe + Comms simultaneously, but must consciously switch modes and is accountable for each. The protocol failure pattern at solo scale is not "I don't have a scribe" but "I forgot to be a scribe for the last 20 minutes."

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Role-partitioning directly maps to solo dev + LLM agent.** The most natural split is:
  - **Human = Incident Commander** (decides direction, delegates).
  - **Agent = Operations Lead** (modifies the system — code / spec / artifacts).
  - **Agent = Scribe** (maintains session log, updates the living-document artifact).
  - **Agent = Planning Lead** (tracks long-term issues, files pending questions).
  - Communications is degenerate in solo context.
- **Explicit role-typing of agent responses** (CAN reports: Condition / Actions / Needs) is a structured-response format worth testing independently.
- **"Only the operations team modifies the system"** is a protocol rule about autonomy — the agent-as-OL makes edits; the agent-as-Scribe does not. This is directly testable in tool-use constraints.

**Costs:**

- **Role-switching has overhead.** Forcing a single agent to role-type every response ("I am speaking as Scribe: ...") may produce ritual output that doesn't actually gate behavior.
- **Sub-agent spawning vs role-typing.** Kerf already uses sub-agents for reviewer role separation. The question is whether *further* role-split (OL vs Scribe vs Planning as separate sub-agents) produces gain or just latency. Plausibly most value is in OL / Scribe separation; Planning can fold into either.
- **Solo-collapse discipline is the main risk.** If the human doesn't enforce mode-switching for themselves ("am I commanding or am I scribing right now?"), the agent's role-typing is meaningless. Parallel to "patient teach-back is ritual if the patient just repeats words" from medical-handoffs.

### Adaptation notes

Candidate solo-adapted role split:

| ICS role | Coding-planning occupant | Responsibility |
|---|---|---|
| Incident Commander | Human | Sets intent, decides at branch points, accepts/rejects synthesis |
| Operations Lead | Agent (primary session) | Modifies spec, code, artifacts; reports CAN updates |
| Scribe | Agent (or a scribe sub-agent) | Maintains living session log, tracks decisions and hypotheses |
| Planning Lead | Agent (or same as Scribe at solo scale) | Tracks parked questions and next-period objectives |
| Comms Lead | N/A at solo scale | Degenerate |

The **CAN report** (Condition / Actions / Needs) is a SBAR-adjacent short-form; worth testing as an agent response template for "what have you been doing" moments in long sessions.

The **span-of-control rule** (3–7 direct reports, 5 optimal) at solo scale becomes: **at most 5 parked sub-questions before the session is over-scoped and must be decomposed**. This is a hypothesis worth testing against the corpus.

### Evaluation plan

1. **Role-contamination detection in corpus.** Classify agent turns into OL-like (modifying artifacts) vs Scribe-like (recording decisions) vs Planning-like (tracking future work). Sessions with heavy role-mixing predicted to correlate with drift events.
2. **CAN-report A/B.** For a subset of session-midpoints, prompt agent to produce a CAN-format status vs free-form status. Measure downstream human-correction-rate as noise-free signal.
3. **Span-of-control test.** In the corpus, count parked sub-questions at session end. Does the 5-at-most rule predict spec-revision rate (R1) downstream?
4. **Scribe sub-agent spawn test.** Compare sessions where a dedicated scribe sub-agent maintains the living document vs sessions where the primary agent does both. Measure session-log completeness and decision-attribution clarity.

## Situation Reports (Sitreps)

### Description

A sitrep is a structured status update at regular cadence from a subordinate to a commander (or laterally among coordinating actors). In ICS it is a formal artifact (FEMA ICS 209 at incident level; shorter sitreps at operational-period boundaries); in Google SRE it appears as periodic IC-to-stakeholder updates; in military doctrine it is a core tempo element.

Canonical structure: **current situation / actions taken since last sitrep / actions planned before next sitrep / resources required / risks and concerns**. The sitrep cadence is time-boxed (every N hours), not trigger-driven. The implicit protocol rule is: "even if nothing changed, you still produce a sitrep saying so."

### Why each element is load-bearing

- **Current situation** catches sender-receiver state-divergence.
- **Actions since last sitrep** catches invisible work (actor did things but never surfaced them).
- **Actions planned before next** catches the "I'm about to do something surprising" failure mode — commander gets a predictable window to redirect.
- **Resources required** catches asymmetric-information failure — subordinate knows what they need; commander has not been told.
- **Risks/concerns** is the legitimized-intuition channel (analog to medical-handoffs' "watcher" tier and CUS tier-1 "concerned").
- **Cadence rule** catches the "I thought nothing had changed" failure — subordinate's sense of "worth reporting" is itself unreliable.

### Dimension values

- **Timing of alignment:** cadenced (scheduled, not triggered)
- **Decision locus:** none (sitrep informs; decisions happen separately)
- **Dialog form:** structured (short-to-medium)
- **Question style:** implicit at slots; "risks/concerns" is the close-ended ask-for-guidance channel
- **Autonomy scope:** N/A (reporting move, not scope-setter)
- **Context richness:** rich-brief
- **Plan expression form:** structured-artifact (short)
- **Knowledge direction:** subordinate → commander; commander may respond with guidance
- **Review/critique integration:** cadence-triggered

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Sitrep-style agent summaries at checkpoints** combine three moves: cadence-over-trigger, structured-artifact, legitimized-concern (the risks slot parallels CUS tier-1 and I-PASS watcher). For long planning sessions this is plausibly a high-leverage single protocol.
- **"Actions planned before next sitrep"** is a pre-announcement of autonomy use. If the agent states what it will do next before doing it, the human has a predictable window to redirect that doesn't require inspecting every turn. This is a softer form of commander's-intent that works mid-session.
- **"Resources required"** maps to parked-questions and unmet dependencies; structured over ad-hoc.

**Costs:**

- **Cadence overhead can become ritual.** If the agent emits a sitrep every N turns and the human skims them, the protocol has overhead without catch-rate.
- **Right cadence is context-dependent.** Too frequent = noise; too rare = drift catch-up cost. Worth testing.

### Adaptation notes

Adapted sitrep for a mid-planning-session checkpoint, emitted by the agent:

| Sitrep slot | Adapted slot |
|---|---|
| Current situation | "Where we are now in the plan" (one-to-two sentences) |
| Actions since last sitrep | "What I've done since the last checkpoint" (bullet list, linked to turns) |
| Actions planned before next | "What I plan to do in the next N turns" (bullet list, pre-announced) |
| Resources required | "Decisions I need from you" or "context I'm missing" |
| Risks and concerns | "Things I'm uncertain about" (legitimized-intuition slot — the watcher channel) |

Cadence candidates: every 10 agent turns; at each named sub-task boundary; on human request. **The "actions planned before next" slot is what distinguishes sitrep from post-hoc summary** — it's a commitment forward, not a report backward.

### Evaluation plan

1. **Retrospective sitrep reconstruction.** For existing long corpus sessions, reconstruct what a sitrep at each checkpoint *would have said*. Check whether the "risks and concerns" slot would have surfaced known failure points.
2. **Cadence sweep.** In simulation, vary cadence (5, 10, 20 turns). Measure synthesis quality vs interruption cost.
3. **Pre-announcement test.** Compare sessions where agent pre-announces next-turn-plan vs sessions with post-hoc summaries. Hypothesis: pre-announcement produces earlier human-redirection at lower cost.

## Tactical Pause

### Description

A tactical pause is a deliberate, protocol-legitimized stop in fireground operations called by the first-due officer when "the correct strategy and tactics are not immediately obvious." It is initiated by a specific trigger phrase ("Units stand by" or "Level I stage") that all personnel have rehearsed.

Three design features:
- **Scripted trigger.** A specific phrase known to all parties. No improvisation required.
- **Not-full-halt.** Activity that is obviously required (laying lines, suiting up) continues; only strategic and tactical commitments pause.
- **Resolves with a radio report.** Pause ends when the officer announces the decision. Pause has a defined end state.

The tactical pause resolves two classic failure modes: (a) **decisive action on incomplete information** (charging in); (b) **analysis paralysis** (over-deliberating while conditions deteriorate). The explicit pause names the ambiguity, bounds the deliberation time, and makes the resumption a protocol event.

### Why it works — error class caught

The pause addresses the **under-load decision debt** failure: when action is expected but the decider lacks enough information, the decider either acts on too-little (producing foreseeable error) or stalls silently (producing unclear state for others). Neither is observable to others until consequences appear.

The scripted trigger phrase converts private stall into public pause. It legitimizes saying "I don't know yet" without the social cost of uncertainty-admission.

### Dimension values

- **Timing of alignment:** in-action (mid-operation)
- **Decision locus:** pre-authorized (any first-due officer can call it)
- **Dialog form:** short-trigger-phrase then structured deliberation
- **Question style:** deliberative (during pause); declarative (on resumption)
- **Autonomy scope:** pauses autonomy explicitly
- **Context richness:** minimal at trigger
- **Plan expression form:** N/A (pause itself is not a plan artifact; resolution may produce one)
- **Knowledge direction:** bidirectional; pause invites inputs from others
- **Review/critique integration:** pre-action (the pause *is* deliberation time, explicit)

### Predicted trade-offs for coding-planning

**Strongest predicted gains:**

- **Protocol-mandated pause after N corrections in a row** (a trigger the evaluation-framework already tracks via C1 correction-cycle count) converts a private "oh, the human keeps correcting me" into a public "I'm calling a tactical pause; we're not aligned; let's reset framing." This addresses the corpus-observed framing-drift pain point without requiring either party to unilaterally escalate.
- **Protocol-mandated pause after long silent-autonomy stretches** converts the "agent ran a long way in the wrong direction" failure into an observable pause event. Agent pre-announcement (sitrep integration) + hard-stop after N turns = a tactical pause structure.
- **Pause as a bilateral move.** Either human or agent can call it. For the agent this is structurally similar to CUS tier-2 ("I am uncomfortable with X — can you confirm?") but bounded in deliberation time rather than open-ended.

**Costs:**

- **Trigger-phrase needs to be rehearsed.** In fire service this takes training; in coding-planning it takes consistent session conventions. Inventing one per session is counterproductive.
- **Pauses have a cost; over-use inflates.** Same risk as CUS tier inflation — if pause is called on every doubt, it becomes background noise.
- **Resumption rule needs to be explicit.** A pause without a defined resumption event becomes a stall. The fire-service rule ("resumption is a radio report") translates to: pause resumes with an explicit statement of what the reassessment concluded.

### Adaptation notes

Concrete candidate triggers for protocol-mandated tactical pause in coding-planning:

- After 2+ framing corrections in a row (M1-based trigger).
- After agent runs 20+ turns with zero human input (a "silent stretch" trigger; composes with sitrep cadence).
- When agent-side CUS tier-2 concerns arise (composes with medical-handoffs CUS adaptation).
- At human request with a scripted phrase.

Resumption form: a short structured artifact stating *what the pause reassessed* and *what changed*. Without this, pause is just a stall.

### Evaluation plan

1. **Retrospective pause-opportunity identification.** For corpus sessions with known drift, mark where a tactical pause would have been called under each candidate trigger. Measure how early the trigger would have fired relative to when drift became visible.
2. **False-positive rate.** For each candidate trigger, measure how often it fires when no actual drift follows. High false-positive rate = trigger is noise.
3. **Composition test.** Does combining sitrep cadence + tactical-pause-on-N-corrections outperform either alone?

## After-Action Review (AAR)

### Description

AAR is the US Army's post-event structured review protocol, now widely adopted (NHS, NASA, software postmortems). It is defined by four canonical questions:

1. **What was supposed to happen?** (Intent recovery)
2. **What actually happened?** (Ground truth)
3. **Why the differences?** (Gap analysis)
4. **What to do next time?** (Forward learning)

The later variant adds a "sustains and improves" framing: what worked and should be preserved, what didn't and should change. Google's Incident Management Guide and PagerDuty's documentation both inherit the AAR structure under the "postmortem" name.

Critical protocol rules:
- **Blameless.** "Assigning blame or issuing reprimands is antithetical to the purpose of an AAR." This is doctrine, not aspiration.
- **Rank-neutral.** Everyone present speaks freely regardless of rank.
- **Facilitator-led, not leader-led.** A neutral facilitator asks the questions; the leader does not defend decisions.
- **Timed-immediately.** Formal AARs happen as soon as possible after action, before memory decay and interpretation drift.
- **Forward-looking.** The output is not "who was at fault" but "what changes for next time."

Distinction between **formal AAR** (full structured session, trained facilitator, private space) and **informal AAR** (quick hot-wash by the team leader, minutes rather than an hour).

### Why each element is load-bearing

- **Question 1 (intent)** anchors the review in the plan's purpose, not in the events. Without it, review becomes narrative-retelling; with it, every observation is judged against intent.
- **Question 2 (ground truth)** forces a shared fact base before interpretation. Without it, parties argue from different event-models.
- **Question 3 (why)** is the causal-analysis slot, legitimized as a group activity. Without it, each participant privately reasons to different causes; with it, the causes are surfaced and can be compared.
- **Question 4 (forward)** prevents ritual review. Without an explicit output, the AAR is catharsis; with one, it is learning.
- **Blamelessness rule** is what makes Question 3 work. In a blame culture, Question 3 becomes "who to punish" and the group suppresses candid causal analysis.

### Dimension values

- **Timing of alignment:** post-action
- **Decision locus:** collective (participants speak freely; facilitator neutral)
- **Dialog form:** structured question sequence
- **Question style:** open (the four questions are broad prompts)
- **Autonomy scope:** N/A (review, not dispatch)
- **Context richness:** rich (full event retrospective)
- **Plan expression form:** structured-artifact (output document: sustains / improves / action items)
- **Knowledge direction:** bidirectional, rank-flat
- **Review/critique integration:** **this is the review protocol itself** — it is not integrated into another protocol; it's the named protocol

### Predicted trade-offs for coding-planning

**Direct candidate for a post-session planning-protocol retrospective.** The four-question form maps cleanly:

| AAR question | Planning-session analog |
|---|---|
| What was supposed to happen? | Recover the intent stated at opener (or infer if none was stated) |
| What actually happened? | What did the plan produce; what did the spec-draft commit to |
| Why the differences? | What framing corrections, scope changes, or drift events occurred |
| What to do next time? | Session-level sustains/improves: what to keep, what to change in next session's opener |

**Strongest predicted gains:**

- **Session-level learning artifact.** A post-session AAR output is a natural input to the next session's opener — especially the sustains/improves list. This closes the per-session learning loop.
- **Blamelessness in the human-agent case** is structurally simpler: there's no hierarchy. The rule still matters: the retrospective is about the *protocol*, not about "the human was unclear" or "the agent was off-base." Framing determines whether the review produces changes or re-litigation.
- **Hot-wash variant (informal AAR)** can run in under 5 minutes at session end. Cheap. Formal version runs monthly or at track boundaries.

**Costs:**

- **Immediate-timing is hard at solo scale.** Sessions end when humans step away; a mandatory retrospective at session end adds load right at the point of lowest user energy. Mitigation: agent auto-drafts the AAR for human review later, rather than requiring live walk-through.
- **Facilitator-led is hard to replicate solo.** The human is both participant and facilitator in a solo retrospective. Mitigation: use a reviewer-sub-agent as facilitator, specifically for the "why the differences" question where the human is most likely to rationalize.
- **Blameless framing may require explicit prompt engineering.** Agent can drift into "the human was unclear" or "the agent misunderstood" framing that locates fault rather than protocol gaps.

### Adaptation notes

Proposed solo planning-session AAR structure:

1. **Intent recovery:** paste or reconstruct the opener's stated intent.
2. **Ground truth:** agent auto-generates a chronological summary (Scribe role output).
3. **Why differences:** reviewer sub-agent analyzes, prompted with: "where did framing drift, where did the agent proceed on misaligned assumptions, where did the human provide framing corrections, in blameless framing (the protocol is what failed, not the participants)."
4. **Sustains / improves:** human chooses one of each; logged as input to next session opener.

This composes with the commander's-intent primitive: the intent stated at opener is what Question 1 recovers. The whole loop is: **intent (before) → IAP (during) → sitrep (cadence) → AAR (after) → intent for next session (feedback).**

### Evaluation plan

1. **Corpus AAR reconstruction.** For 10 corpus sessions, produce an after-action review retrospectively. Measure the rate at which reconstructed AARs surface drift events that were not already surfaced in prior analysis.
2. **Forward-loop test.** Sessions where AAR output from prior session is injected into opener vs sessions without. Measure framing-correction count (M1) — hypothesis: injected AARs reduce M1 on similar tasks.
3. **Formality sweep.** Compare hot-wash (informal, under 5 min) vs full AAR (structured, 20 min) on same session. Where does the extra formality buy anything?
4. **Blameless-prompt test.** Agent-generated AAR drafts with vs without explicit blameless framing. Measure whether fault-attribution language appears.

## Handoff protocol (closed acknowledgment)

### Description

Google SRE's incident management guide specifies that handoff of the IC role "must be clearly handed off at the end of the working day." The outgoing IC says explicitly: "You're now the incident commander, okay?" and must await **firm acknowledgment** before ending their shift. This is not a casual transition; it is a closed-loop protocol step.

This is structurally identical to aviation's read-back rule and medical-handoffs' synthesis-by-receiver: a state transition is not complete until the receiver acknowledges explicitly.

### Why it works

Without the closed acknowledgment, handoffs produce a signature gap: the outgoing party believes ownership has transferred; the incoming party believes it has not; no one is minding the store. This pattern is named in aviation ("handoff gap"), medicine ("fumbled handoff"), and military operations (relief-in-place).

### Dimension values

- **Timing of alignment:** at role-transition
- **Decision locus:** transferred from one party to another
- **Dialog form:** short-volley with closing acknowledgment
- **Question style:** declarative-plus-confirmation
- **Autonomy scope:** transfers autonomy itself
- **Context richness:** depends on state transferred
- **Plan expression form:** N/A
- **Knowledge direction:** bidirectional
- **Review/critique integration:** read-back (the closing acknowledgment is itself a read-back)

### Predicted trade-offs for coding-planning

**Helps with:**

- **Session-to-session handoffs.** When a multi-day planning effort is picked up in a new session, the handoff protocol applies: the prior session's intent, current state, and parked questions must be *explicitly acknowledged* by the new session before work begins.
- **Agent-to-agent handoffs via sub-agent spawning.** When a sub-agent is spawned for a review or specialized task, the handoff-with-acknowledgment pattern catches cases where the sub-agent proceeds without fully integrating the spawn context.
- **End-of-session parking.** Explicit "this is where we stopped; when we resume, confirm you've read X" avoids the silent-context-loss pattern Phase 1 flagged for context-dump openers.

**Costs:**

- **Overhead if over-applied.** Every sub-agent spawn with a full handoff ritual adds latency; most sub-agents don't need it.
- **Solo sessions with same agent over time** don't have a transition point where the protocol obviously fires. The "end of working day" marker has no solo-context analog. Candidate mapping: end of working day = end of kerf pass, or end of a session by wall-clock > N hours.

### Adaptation notes

Handoff protocol compresses to: **state summary + intent restatement + receiver acknowledgment**. For coding-planning this appears in three places:

- **Session resume:** prior session's closing artifact is re-read; new session confirms explicit understanding before proceeding.
- **Sub-agent spawn with state:** for high-context sub-agent spawns, require the sub-agent to restate the received context before proceeding.
- **Scheduled long-session break:** end-of-period handoff even to "future self" via written summary.

### Evaluation plan

1. **Session-resume catch rate.** For sessions picked up after a break, compare sessions with explicit-acknowledgment vs those with direct-continue. Measure first-N-turn correction rate.
2. **Sub-agent handoff test.** For spawned sub-agents, A/B between with-acknowledgment vs without. Check whether sub-agent proceeds on unstated assumptions.

## Unified Command

### Description

Unified Command is the ICS structure used when multiple agencies (fire, police, EMS, emergency management) respond to the same incident. Each agency has representation at command; they jointly set objectives; tactical execution is coordinated. The key innovation is that command is **shared**, not escalated to a single super-commander.

This is instructive but **largely team-scale and not a candidate primitive for solo coding-planning**. It is covered briefly because its structural move maps onto multi-agent orchestration (which harmonik's larger architecture contemplates) and because it offers a model for how *multiple specialized agents* could jointly set objectives rather than one being the orchestrator.

### Predicted trade-offs

**Where it might transfer:**

- **Multi-agent workflows with jurisdictional separation.** If harmonik spawns specialized agents (e.g., one for spec, one for code, one for docs) and those agents are authoritative in their domain, unified-command-style joint objective-setting (rather than human-as-IC) is a candidate coordination model.
- **Reviewer-sub-agent trios.** Three reviewers from different perspectives (architect / implementer / operator) could each own their section's objectives under a unified-command structure rather than reporting to a single review orchestrator.

**Why it is not prioritized for Phase 2:**

- Solo human + single LLM agent is the Phase 2 scope per the research statement.
- Unified command depends on clear jurisdictional boundaries between agencies; in agent contexts those boundaries are not currently stable.

Noted here as a pointer for later tracks on multi-agent orchestration.

## Considered and rejected

A handful of incident-command-adjacent ideas came up in research but were not promoted to candidate protocols, with brief reasoning:

- **ICS Form 201 through 215 full set** (25+ forms in FEMA's ICS Forms Descriptions) — most are team-ceremony artifacts (personnel accounting, demobilization plans, etc.) with no solo analog. The adapted IAP above captures what is load-bearing; the rest is overhead.
- **Span-of-control doctrine in its team-scale form** — the 3–7 direct reports rule is structurally interesting but ports only as the adapted "at most 5 parked questions" heuristic above.
- **Logistics and Finance sections** — these exist in full ICS because multi-hundred-person incidents need resource acquisition and cost accounting. Neither has a solo analog. Planning and Operations are the load-bearing functions that transfer.
- **Demobilization plans** — formal wind-down process for an incident. A session-end handoff artifact covers the structural move; the full form is team-ceremony.
- **Incident Type classification (1 through 5)** — the complexity-tier rubric mainly drives which ICS-level activates. At solo scale, the rubric analog is "how long is this session likely to be" and is already subsumed by session-shape tags from I-PASS severity adaptation (medical-handoffs domain).
- **Radio communication protocols (clear text, callsigns)** — specific to shared-channel voice operations. Read-back is the transferable substrate; the ceremony is not.
- **Resource tracking** (personnel / equipment check-in) — ceremony specific to physical incident staging. No coding-planning analog.
- **Accountability roll-call** (periodic personnel-accountability reports during active incidents) — urgency-dependent; no solo analog.
- **Liaison Officer / Public Information Officer / Safety Officer** command-staff roles — team-scale specializations; Safety Officer's function partly survives as the "constraints must not be violated" slot in the adapted IAP, but the role itself is team-scale.
- **OODA loop** (Observe / Orient / Decide / Act — Boyd, military origin) came up tangentially. It is an individual-cognition primitive rather than a coordination protocol; arguably relevant but is a different abstraction level (individual decision cycle) from the protocols this domain offers. Noted as a possible input to a later cognitive-architecture track.
- **Pre-mortem** (Gary Klein's variant of AAR run *before* action) — candidate, but it comes from the pre-mortem / design-review lineage already covered under the design-review domain. Not re-claimed here.
- **Crew Resource Management (CRM)** — aviation's safety-culture bundle; some components (closed-loop comms, graduated concern) already covered under medical-handoffs (read-back, CUS). Not re-claimed.

---

## Summary of candidate protocols to carry forward

In rough order of predicted value for coding-planning:

1. **Commander's intent** — highest predicted value; directly addresses agent-defers-trivial-decisions and framing-drift; introduces a new dimension value (**bounded-by-intent** autonomy scope); composes with every other candidate.
2. **Written cadence artifact (adapted IAP)** — objectives-before-tactics session-period artifact with synthesis step. Addresses plan drift in long sessions.
3. **Sitrep at cadence** — structured agent-emitted checkpoint with legitimized-concern slot. Composes with watcher-tier and CUS adaptations from medical-handoffs.
4. **After-Action Review (four-question, blameless)** — directly importable post-session retrospective. Output feeds next session's opener; closes the learning loop.
5. **Tactical pause on defined triggers** — converts private stall into public deliberation; composes with correction-count signals already in the evaluation-framework.
6. **Named-role separation with solo-collapse rule** — structural model for human-as-IC / agent-as-OL+Scribe; CAN-report format is an independently testable sub-feature.
7. **Closed-acknowledgment handoff** — session-to-session and sub-agent-to-sub-agent context transfer with explicit confirmation step.

Unified command is noted but not prioritized for Phase 2.

The **bounded-by-intent** value is the primary new entry this domain contributes to the autonomy-scope dimension. The **cadence-over-trigger** and **solo-collapse** patterns are cross-cutting design patterns that apply to multiple candidates (IAP, sitrep, AAR) rather than being candidates themselves.

Sources:
- [ICS 100 – Incident Command System (USDA/FEMA)](https://www.usda.gov/sites/default/files/documents/ICS100.pdf)
- [ICS Organizational Structure and Elements (FEMA)](https://training.fema.gov/emiweb/is/icsresource/assets/ics%20organizational%20structure%20and%20elements.pdf)
- [FEMA Incident Action Planning Guide (2012)](https://www.dco.uscg.mil/Portals/9/CG-5R/nsarc/FEMA%20Incident%20Action%20Planning%20Guide%20(IAP).pdf)
- [FEMA Incident Action Planning Process ("Planning P")](https://www.fema.gov/sites/default/files/documents/fema_incident-action-planning-process.pdf)
- [ICS 202 Incident Objectives Form](https://training.fema.gov/emiweb/is/icsresource/assets/ics%20forms/ics%20form%20202,%20incident%20objectives%20(v3.1).pdf)
- [Google SRE Book — Managing Incidents](https://sre.google/sre-book/managing-incidents/)
- [Google SRE Incident Management Guide (PDF)](https://sre.google/static/pdf/IncidentManagementGuide.pdf)
- [PagerDuty Incident Response — Different Roles](https://response.pagerduty.com/before/different_roles/)
- [PagerDuty Incident Response — Incident Commander](https://response.pagerduty.com/training/incident_commander/)
- [PagerDuty Incident Response — Scribe](https://response.pagerduty.com/training/scribe/)
- [PagerDuty Incident Response — Subject Matter Expert](https://response.pagerduty.com/training/subject_matter_expert/)
- [US Army FM 7-0 Appendix K — After Action Reviews](https://www.first.army.mil/Portals/102/FM%207-0%20Appendix%20K.pdf)
- [The Leader's Guide to After-Action Reviews (AAR)](https://pinnacle-leaders.com/wp-content/uploads/2018/02/Leaders_Guide_to_AAR.pdf)
- [After-action review — Wikipedia](https://en.wikipedia.org/wiki/After-action_review)
- [Commander's Intent — Wikipedia (Intent, military)](https://en.wikipedia.org/wiki/Intent_(military))
- [Commander's Intent: Less is Better (CALL)](https://www.globalsecurity.org/military/library/report/call/call_98-24_ch1.htm)
- [The Tactical Pause — Fire Engineering](https://www.fireengineering.com/firefighting/the-tactical-pause/)
- [Tactical Decision Points — Fire Engineering](https://www.fireengineering.com/firefighting/tactical-decision-points-proactive-wildland-fire-risk-management/)
- [Proper Use of Trigger Points — NWCG](https://www.nwcg.gov/6mfs/weather-fire-behavior/proper-use-of-trigger-points)
