# External domain: design review and code review

Phase 2, Step 2 sub-agent output. Track: planning-protocols. Domain: software-engineering design and code review protocols.

## Why this domain matters

This is the most direct ancestor domain for human-agent coding-planning protocols. The patterns already visible in the harmonik corpus — reviewer sub-agents, spec-iteration, structured handoffs — are descendants of these traditions. Pre-implementation protocols (RFCs, ADRs, design reviews, pre-mortems) are the primary focus because they operate in the same idea → plan → spec phase the track is investigating. Post-implementation protocols (code review, PR conventions) are investigated secondarily for their question-style and disagreement-resolution mechanics, which may back-port usefully.

The emphasis across this domain is **structured-artifact plan expression** (versus dialog-log) and **review integration as a named phase**. The user has adopted the review-integration pattern (kerf reviewer sub-agents). This report is especially attentive to what the user has *not* yet adopted: mandatory "alternatives considered" sections, pre-mortem as a pre-dispatch step, explicit role-splitting during review, and time-boxed critique.

A terminology note: throughout this document, "protocol" means an adapted-for-solo-plus-LLM version of the domain practice. Where the original requires multiple humans, the adaptation is called out explicitly.

---

## 1. IETF-style RFC (full form)

**Description.** A long-form structured-artifact protocol. A proposal is captured in a document with a fixed section skeleton; review happens asynchronously against the written artifact rather than through live dialog. Originated with IETF (RFC 2026 defines the standards process itself) and spread to software organizations as the "tech RFC" or "design doc" pattern (Rust RFCs, Python PEPs, and company-internal variants).

The canonical section skeleton, as codified in the Rust RFC template (`0000-template.md`), is:

1. Summary — one paragraph.
2. Motivation — problem being solved, use cases. "Changes should focus on solving a problem that users are having."
3. Guide-level explanation — teach the feature as if already shipped.
4. Reference-level explanation — technical portion, implementation details, corner cases, interactions with other features.
5. Drawbacks — reasons *not* to do this.
6. Rationale and alternatives — why this design vs. others; library/macro-sufficiency check.
7. Prior art — the good and the bad from other languages and communities.
8. Unresolved questions — what you expect to resolve during RFC discussion, what during implementation, what out of scope.
9. Future possibilities — natural extensions.

Python PEPs add a mandatory **backwards-compatibility** section; PEP 1 treats absence of sufficient motivation or backwards-compatibility analysis as grounds to reject the PEP outright. This is a notable mechanism: *missing a mandatory section is itself a protocol failure*, not a soft concern.

**Dimension values.**
- Timing of alignment: pre-action.
- Decision locus: mixed (author proposes, interactive critique, human-authoritative at final merge).
- Dialog form: structured (artifact) + long-message comments.
- Question style: batched-at-end, embedded in artifact.
- Autonomy scope: bounded-by-category (proposal constrained by problem statement).
- Context richness: rich-brief (the artifact is the brief).
- Plan expression form: structured-artifact (heavy).
- Knowledge direction: bidirectional (author presents, reviewers surface prior art and unknowns).
- Review/critique integration: pre-dispatch-plan-review; multi-reviewer asynchronous.

**Mechanism.** Each section is load-bearing for a *specific error class*:

- **Motivation** catches solution-in-search-of-a-problem; forces problem-first framing.
- **Guide-level explanation** catches unteachable designs — if you can't teach it, users can't adopt it.
- **Drawbacks** catches one-sided advocacy; authors must articulate the negative case.
- **Rationale and alternatives** catches anchoring on first solution; forces enumeration of rejected options.
- **Prior art** catches reinventing-the-wheel and blindness to failed prior attempts.
- **Unresolved questions** catches false-completeness; explicit acknowledgment of what isn't yet decided prevents "we thought we agreed."
- **Future possibilities** catches over-narrowing; invites thinking about extension without committing to it.

The structure is adversarial-by-construction: the author must argue against themselves in sections 5, 6, and 8.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Sections map neatly to prompt scaffolding. An LLM agent producing an RFC-shaped plan is forced through the same error-catching discipline. Much of the value is in the discipline of filling sections, not in the reader count.
- *Con:* Full RFC form is heavyweight; overhead may exceed the value on small changes. The template optimizes for cross-team consensus, which is absent in the solo case.
- *Neutral:* The "asynchronous reviewer" role is what kerf reviewer sub-agents already play. The artifact side is less established in the corpus.

**Adaptation notes.** The solo-plus-agent adaptation: the artifact skeleton is preserved, the cross-team review ceremony is dropped. A sub-agent pass can explicitly check each mandatory section for substance (not just presence); PEP's "reject for missing motivation" rule maps to "reviewer sub-agent blocks advancement if motivation section is weak."

**Evaluation plan.** Take a recent plan from the harmonik corpus (e.g., a finalized kerf work) and check whether each section's error class was caught somewhere in that plan's history. Where a section's error class leaked through (e.g., an alternative that was never considered until implementation), the missing section is a candidate to add to the kerf scaffold.

## 2. Architectural Decision Record (Nygard format)

**Description.** Compact structured artifact: **Title, Status, Context, Decision, Consequences.** Target length one to two pages. Originated in Michael Nygard's 2011 post "Documenting Architecture Decisions." The format deliberately sacrifices completeness for maintainability: Nygard's premise is that long documents are never kept up to date and therefore go stale, so brevity is a correctness property, not a concession.

**Dimension values.**
- Timing of alignment: post-decision (captures the decision for future readers).
- Decision locus: agent-autonomous or mixed (the decision is made first; the ADR records it).
- Dialog form: structured, short.
- Question style: implicit (the Context section implicitly answers "why did this need deciding?").
- Autonomy scope: none (records a decision already made).
- Context richness: minimal.
- Plan expression form: structured-artifact (light).
- Knowledge direction: agent-dominant → human (or agent → future-agent).
- Review/critique integration: none by default; the Status field is the only soft hook (proposed → accepted → superseded).

**Mechanism.** The compactness forces **single-point-of-decision framing**. You can't write an ADR that bundles six decisions; if you try, the Context section bloats and the Decision section becomes a list rather than a sentence. Nygard's "We will…" active-voice rule is the enforcement mechanism.

Each field catches a specific error class:
- **Context** — catches decisions made in forgotten circumstances. Re-reading an ADR years later, the Context tells you whether the forces that drove it still apply.
- **Decision** — catches ambiguity about what was actually decided.
- **Consequences** — mandatory negative-consequence enumeration catches one-sided optimism.
- **Status** — catches dead decisions masquerading as live. "Superseded-by" gives a chain of reasoning across time.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Drastically lower overhead than RFC form. The compactness is a feature when the decision granularity is small (which many agent-scale decisions are).
- *Pro:* "Superseded-by" chain is inherently agent-friendly — agents picking up work can trace decision history without narrative archaeology.
- *Con:* Compactness is a *limitation* when the decision requires tradeoff reasoning. An ADR cannot hold an alternatives-considered matrix without violating its form. So ADRs do not substitute for RFCs on contested designs; they record the outcome, not the deliberation.
- *Con:* The single-decision-per-document discipline means coordinated decisions across multiple ADRs can drift.

**Adaptation notes.** ADRs fit naturally below RFCs in a two-tier structure: the RFC (or kerf work) holds the deliberation; ADRs hold the durable record of what was decided. Harmonik's locked-in architectural decisions (the "ten" referenced in STATUS.md) are already effectively in ADR shape; making that explicit would catch drift.

**Evaluation plan.** Test whether the ten locked-in decisions, if rewritten as formal ADRs with mandatory Context and Consequences, surface forces or consequences the current documentation elides. If not, ADR form adds no value; if yes, it does.

## 3. PEP-style RFC (Python Enhancement Proposal)

**Description.** A stricter, community-scale variant of the RFC pattern. PEP 1 defines the governance; individual PEPs follow a template with hard gates: **motivation, rationale, specification, backwards compatibility, security implications, reference implementation**. Critical mechanism: **missing sections are grounds for rejection without substantive review.**

**Dimension values.**
- Timing of alignment: pre-action, gated.
- Decision locus: interactive, with a Steering Council decision.
- Dialog form: structured, long-lived.
- Question style: batched.
- Autonomy scope: bounded-by-category.
- Context richness: rich-brief.
- Plan expression form: structured-artifact.
- Knowledge direction: bidirectional.
- Review/critique integration: pre-dispatch, multi-reviewer, with formal gatekeeper.

**Mechanism.** The hard-gate rule is the interesting part. It converts "sections the author was supposed to fill out" into "sections the author *will* fill out because they cannot proceed otherwise." This is a protocol enforcement pattern orthogonal to the artifact structure itself — the artifact could be any shape, but the gate makes it work.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The gate translates directly to a reviewer sub-agent that blocks advancement on missing-or-weak sections. This is machine-checkable.
- *Con:* Over-strictness may push authors to produce pro-forma sections that tick the box without substance ("motivation: because we want to"). Gate enforcement must check for substance, not just presence.

**Adaptation notes.** Kerf jig advancement is already structurally analogous (problem space → decompose → research → design → spec → tasks). A PEP-style hard gate would mean: the reviewer sub-agent at each pass has a rejection authority, not just a comment authority. Currently, the user chooses whether to act on reviewer feedback.

**Evaluation plan.** Compare kerf works where reviewer feedback was acted on versus ignored; see whether the ignored feedback later resurfaced as a bug or rework. If yes, a hard gate would have prevented it.

## 4. Google-style code review (post-implementation, two variants)

**Description.** Two variants matter here:

(a) **Standard code review.** Google's published `eng-practices` doc. Primary purpose: keep overall code health improving. Reviewer approves when the CL is a net improvement, not when it is perfect. Response within one business day. Comments about the code, not the developer. Comments explain intent ("I suggest X because Y"), not just verdict.

(b) **Readability review.** A specialized secondary review: every CL requires approval from someone with "readability certification" in the relevant language. ~20% of Google engineers are participating in the readability process at any given time; ~1-2% are certified reviewers. This is a *separate* approval axis from correctness review.

**Dimension values (variant a).**
- Timing of alignment: post-action, pre-merge.
- Decision locus: interactive.
- Dialog form: short-volley, inline.
- Question style: one-at-a-time, embedded, open-ended ("did you consider X?").
- Autonomy scope: bounded-by-constraint (must meet code-health bar).
- Context richness: minimal.
- Plan expression form: not applicable — the code is the artifact.
- Knowledge direction: bidirectional, mentorship-weighted.
- Review/critique integration: named phase with formal approve/request-changes state.

**Mechanism.** The open-ended question style is the core protocol discipline. Empirical research (Bavota et al., OpenDev study) identifies "questions to understand design or implementation choices" as one of the most useful comment categories. Two forces drive this:

- *Author-preserving.* An open-ended question invites explanation rather than accusing. The author may have context the reviewer lacks; the question structure admits that possibility.
- *Mentorship-compatible.* The same question style teaches; a directive "change this" does not.

The "approve if net improvement" rule catches a different error — perfectionism as a stalling tactic. Without this explicit rule, reviewers default to blocking on minor issues.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Open-ended question style is an adopt-anywhere verbal protocol. "Why did you choose X?" as a reviewer-sub-agent question style is strictly more surfaces-assumptions than "X is wrong." This is high-value and near-zero-cost.
- *Con:* Post-implementation timing is the wrong phase; by the time code exists, choices are sunk. The *question style* adapts to planning-phase; the *timing* does not.

The **readability review** (variant b) is a distinct pattern worth separate attention. It operates as a dedicated review-axis — approving not correctness but style/idiom compliance. A solo + LLM-agent adaptation: a dedicated reviewer sub-agent with a single axis (e.g., "does this plan respect harmonik's locked-in decisions?" or "does this plan use harmonik's established vocabulary correctly?"). The kerf general reviewer is axis-unified; splitting into single-axis reviewers may catch different error classes.

**Adaptation notes.** Port the question style (open-ended, embedded, assume missing context); drop the timing (post-implementation inappropriate for planning). Add single-axis reviewer sub-agents alongside the general reviewer.

**Evaluation plan.** For a set of kerf reviewer comments, classify each as directive-verdict vs. open-ended-question. Check whether open-ended comments surfaced more hidden assumptions than directive ones.

## 5. ATAM (Architecture Tradeoff Analysis Method)

**Description.** SEI's formal nine-step architecture evaluation. Stakeholders gather, present business drivers, build a **utility tree** of quality attributes, generate scenarios, and analyze each scenario against the architecture. Outputs are classified into four categories: **risks, non-risks, sensitivity points, tradeoff points.** A sensitivity point is where one component materially drives a quality attribute response; a tradeoff point is where the same decision drives competing quality attributes in opposite directions. Every sensitivity point must resolve to either risk or non-risk.

**Dimension values.**
- Timing of alignment: pre-action, mid-design.
- Decision locus: interactive, stakeholder-assembled.
- Dialog form: structured, multi-session.
- Question style: scenario-driven.
- Autonomy scope: bounded-by-category.
- Context richness: rich-brief (utility tree is the context).
- Plan expression form: structured-artifact (utility tree + scenario catalog).
- Knowledge direction: bidirectional.
- Review/critique integration: the entire protocol is the review.

**Mechanism.** Two ideas matter and the rest is ceremony.

- **Utility tree.** Quality attributes (performance, security, modifiability, etc.) are broken into child nodes with concrete scenarios attached. This forces abstract quality goals to land on testable conditions. Analog to "acceptance criteria" in agile, but organized by quality attribute rather than by feature.
- **Risk / non-risk / sensitivity / tradeoff quadruple.** Naming these four things separately is the core discipline. Without it, "risks" swallows everything. Non-risks are explicitly surfaced (a sound decision is a *finding*, not an absence of finding). Tradeoffs are separated from pure risks — a tradeoff is a choice, not a bug.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The four-category output is adoptable as a planning-protocol subroutine. Ending a design pass with "list the risks, non-risks, sensitivities, and tradeoffs" is a compact discipline and forces explicit surfacing of what would otherwise stay implicit.
- *Pro:* The utility tree maps well onto harmonik's quality-attribute-adjacent decisions (e.g., inspectability, determinism, replay-ability are effectively quality attributes).
- *Con:* Full ATAM is heavy ceremony (nine steps, two phases, stakeholder roster). Most of the ceremony is to align multiple humans with different stakes; solo context obviates it.
- *Con:* The scenario-generation step requires a broad stakeholder pool. A solo-plus-LLM context can synthesize "stakeholder perspectives" but risks a single point of view dressed up as many.

**Adaptation notes.** Keep the **four-category output discipline** and the **utility-tree concept**. Drop the nine-step choreography. A compact adapted form: at the end of a design pass, force a one-page output naming (a) the quality attributes in play, (b) the top three scenarios testing each, (c) the risks, non-risks, sensitivities, and tradeoffs. This is roughly 1-2 hours of agent+human work, not two days.

**Evaluation plan.** Apply the four-category discipline retroactively to a finalized kerf work. If the output surfaces items not present in the original kerf artifacts — particularly non-risks (sound decisions made but not named as such) or tradeoffs (choices made without naming the alternative) — the protocol adds value.

## 6. Pre-mortem (Gary Klein, "prospective hindsight")

**Description.** A pre-dispatch review technique. After a plan is drafted but before committing, the team is told: "Imagine it is [N months from now] and this project has failed catastrophically. Write down every reason why." Then the reasons are consolidated, prioritized, and the plan is revised against the top two-three.

Rooted in the 1989 Mitchell/Russo/Pennington finding that prospective hindsight — imagining an event has already occurred — increases ability to identify reasons for future outcomes by ~30% compared to forward-prediction.

Full protocol (Klein's original):
1. Brief the team on the plan and context.
2. Set the failure premise: "the project has failed; during this exercise no one defends or argues."
3. Three minutes of silent individual list-generation.
4. Round-robin: each person contributes one item per pass until lists are exhausted.
5. Address the top two or three concerns immediately; schedule a follow-up for the rest.
6. Periodic review every three to four months.

Total time: 20-30 minutes.

**Dimension values.**
- Timing of alignment: pre-action (late-stage, pre-commit).
- Decision locus: interactive (team generates; leader decides).
- Dialog form: short, structured round-robin.
- Question style: implicit (reversed — "what failed?" rather than "what's wrong?").
- Autonomy scope: bounded.
- Context richness: rich-brief (plan already exists).
- Plan expression form: not applicable — operates on an existing plan.
- Knowledge direction: group-to-author.
- Review/critique integration: named pre-dispatch step.

**Mechanism.** The prospective-hindsight reframing is the entire protocol. By *assuming* the plan has failed, participants:

- Route around **groupthink** — no one is defending the plan; everyone is attacking it.
- Route around **optimism bias** — the question is about failure, not about probability of success.
- Route around **confirmation bias** — the frame inverts: evidence *for* failure is now the currency.
- Route around **planning fallacy** — the question "why did this take twice as long as planned?" is easier to answer concretely than "is this estimate right?"

The time-boxing (20-30 minutes) is deliberate: short enough that it doesn't invite rebuttal, long enough to surface the obvious failure modes. The "no arguing" rule is what makes it work; without it, the pre-mortem collapses back into a defend-the-plan session.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Extremely low overhead (20-30 minutes). High signal-to-time ratio reported across the behavioral-decision literature.
- *Pro:* Direct adapt: a reviewer sub-agent prompted specifically as "imagine this plan has failed; list the causes" produces a different output class than a general-reviewer prompt. This is a *prompt-shape* change, not an architecture change.
- *Pro:* Captures a gap the kerf corpus does not currently fill. The user has not adopted a pre-mortem-shaped reviewer.
- *Con:* In a solo context the "no arguing" rule has no anchoring conflict — the author is the failure-imaginer. LLM-agent sub-agents can do this without the human, which partly restores the separation-of-roles benefit.
- *Con:* Periodic review (step 6) has no good analog in the current kerf model. That suggests a scheduled-wake-up over the kerf work is worth considering.

**Adaptation notes.** The solo + LLM-agent adaptation is strong: a pre-mortem sub-agent at the end of the design pass, with the prompt explicitly framed as "this plan has failed — enumerate causes, then score severity." Distinct from the general reviewer which is asking "is this plan good?" — the pre-mortem reviewer is constitutionally adversarial.

**Evaluation plan.** Run the pre-mortem prompt on a completed kerf work; compare the output to the general reviewer's output on the same work. If the pre-mortem surfaces failure modes the general reviewer did not, the protocol adds orthogonal signal.

## 7. Design review meeting (informal / team)

**Description.** A scheduled meeting where a design author presents to peers for structured critique. Roles: **presenter** (not necessarily author), **reviewers**, **scribe**. Time-boxed (typically 30-60 minutes). Questions categorized (e.g., a whiteboard quadrant of "questions / concerns / critical ideas / positives" — a feedback-matrix technique).

**Dimension values.**
- Timing of alignment: pre-action.
- Decision locus: interactive.
- Dialog form: short-volley, live.
- Question style: mixed (one-at-a-time during, batched at end).
- Autonomy scope: bounded.
- Context richness: rich-brief (presenter provides).
- Plan expression form: hybrid (presentation + artifact).
- Knowledge direction: bidirectional.
- Review/critique integration: the meeting is the review.

**Mechanism.**
- **Scribe as distinct role.** The scribe is not an active reviewer; their job is capture. Without a scribe, the meeting's output evaporates. This is a named-role pattern worth noting: *capture* is a distinct job from *critique*.
- **Time-boxing.** "Use the time budget to decide when to move debate offline, and to enforce the cutoff" — forces prioritization. Unbounded review tends toward rabbit-holing on low-priority items.
- **Question categorization** (the whiteboard quadrant). Organizing comments by category (concerns / questions / ideas / positives) catches items that would be missed by a free-form discussion. Specifically, "positives" is often omitted without explicit structure, and its omission biases the author toward thinking everything in the plan is weak.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* The scribe role maps well to an agent-captured transcript; the ephemerality problem goes away if dialog is logged.
- *Pro:* Time-boxing adapts as a forced stopping criterion on review sub-agents ("produce at most N comments" or "spend at most M tokens").
- *Pro:* Question categorization maps to a structured reviewer-output template.
- *Con:* The live-meeting dialogue dynamic (anchoring off others' comments, piggybacking on surfaced concerns) doesn't translate directly. Parallel reviewer sub-agents replace this with independent perspectives, which is a different mechanism.

**Adaptation notes.** The most portable element is **structured categorization of reviewer output**. Asking a reviewer to produce comments classified as concerns / questions / alternative-ideas / positives is stricter than a free-form review and catches different things.

**Evaluation plan.** Have a reviewer sub-agent produce output in categorized form and compare to the same reviewer in free-form. Check whether the categorized form surfaces "positives" or "alternative-ideas" that free-form omits.

## 8. ATAM's utility tree alone (decomposed)

**Description.** Isolating one component of ATAM: the utility tree is an independently usable discipline. A plan is scored against its quality attributes by decomposing each attribute into child nodes with attached concrete scenarios.

**Dimension values.**
- Timing of alignment: pre-action.
- Decision locus: author-driven.
- Dialog form: structured.
- Question style: implicit (scenarios act as questions).
- Autonomy scope: bounded.
- Context richness: rich-brief.
- Plan expression form: structured-artifact.
- Knowledge direction: author-to-reader.
- Review/critique integration: none directly.

**Mechanism.** Forces quality attributes from abstract statement ("must be inspectable") to testable scenarios ("when the daemon is running, I can attach a TUI and see the live state of each node within 200ms"). The scenario is implicitly an acceptance test.

**Predicted trade-offs.** Standalone, this is one of the highest-value adoptions on a per-effort basis. Nearly zero ceremony; forces specificity.

**Adaptation notes.** Add "utility tree" as a pass in the kerf spec jig, or as a mandatory subsection of the design pass. The output is a structured-artifact addition; the rest of the kerf flow is unchanged.

**Evaluation plan.** Retrospectively produce utility trees for two or three finalized kerf works. Check whether the tree surfaces quality attributes that were discussed ambiguously or not at all in the original artifacts.

## 9. Role-split review (devil's advocate, multi-perspective)

**Description.** A review where specific perspectives are assigned to specific reviewers rather than all reviewers doing generic review. Classical form: one reviewer is assigned "devil's advocate" (find reasons this is wrong); another is "maintainer" (how does this age?); another is "performance" or "security" or similar. Origin: Catholic Church's 11th-century "devil's advocate" (advocatus diaboli) during canonization proceedings; adopted into modern red-team practice.

**Dimension values.**
- Timing of alignment: pre-action or post-action.
- Decision locus: interactive.
- Dialog form: varies.
- Question style: role-shaped.
- Autonomy scope: bounded.
- Context richness: rich-brief.
- Plan expression form: not applicable.
- Knowledge direction: bidirectional.
- Review/critique integration: parallel-reviewers with assigned roles.

**Mechanism.** A generic reviewer, asked to review a plan, produces a generic review. A reviewer prompted specifically as "find every reason this plan is wrong" produces *different* output than the generic reviewer — not simply a subset. Role assignment is a prompt-shape intervention.

Research on multi-agent architectures (Smith 2024 and others) confirms the same dynamic in LLM-agent systems: a thesis-antithesis-synthesis triple (worker + devil's advocate + reviewer-synthesizer) produces different output than any single agent. This is the pattern the kerf reviewer is *closest* to but not yet fully separated into.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Nearly free. Running three parallel reviewer sub-agents with distinct role prompts costs three agent invocations, not three human-hours.
- *Pro:* The role prompts become a testable library — "devil's advocate reviewer," "future-maintainer reviewer," "scope-narrowing reviewer," "simplicity reviewer." Each is a reusable asset.
- *Con:* Risk of redundancy if role prompts are too similar. Role library must be curated, not generated freely.
- *Con:* Synthesizer role is hard. If three reviewers disagree, someone (human or agent) must arbitrate. Currently this falls to the user; at scale, this becomes the bottleneck.

**Adaptation notes.** The kerf reviewer is currently generic. Splitting into a small role library (devil's advocate, simplifier, maintainer, locked-decisions-guardian) and running them in parallel is a direct extension of the existing pattern, not a new pattern.

**Evaluation plan.** Run three role-split reviewers on a finalized kerf work and compare their outputs to the generic reviewer's output on the same work. Measure: do the role-split reviewers together surface items the generic reviewer missed? Are the role-split reviewers' outputs orthogonal (complementary) or redundant (same items, different framing)?

## 10. Pull-request review conventions (conversation-resolution mechanics)

**Description.** GitHub/Gerrit-style per-line inline comments with formal approve/request-changes states. Mechanics of interest: **conversation resolution** (who marks a thread resolved? author or reviewer?) and **disagreement escalation** (what happens when reviewer and author disagree?).

**Dimension values.**
- Timing of alignment: post-action, pre-merge.
- Decision locus: interactive.
- Dialog form: short-volley, threaded, embedded.
- Question style: one-at-a-time, embedded.
- Autonomy scope: bounded.
- Context richness: minimal.
- Plan expression form: not applicable.
- Knowledge direction: bidirectional.
- Review/critique integration: named phase with formal state machine.

**Mechanism.** Two mechanics are substantive:

- **Resolution-authority rule.** Some teams mandate *reviewer-resolves-thread* (author cannot close their own thread); others mandate *author-resolves-thread*. The first rule catches authors prematurely closing unresolved concerns; the second catches reviewers forgetting to close addressed threads. Neither is universally better — they trade off different error classes. But *some* explicit rule is required; without it, threads accumulate ambiguously.
- **Disagreement-escalation rule.** When author and reviewer disagree substantively, explicit rules vary: "best argument wins" (requires articulated reasoning), "majority of two reviewers wins," "objective technical reasoning required to override." The structure matters; default-to-stalemate is the failure mode.

**Predicted trade-offs in a solo + LLM-agent context.**
- *Pro:* Resolution-authority is trivially adaptable — rule could be "reviewer sub-agent's concern must be addressed or explicitly overridden with a recorded reason."
- *Pro:* The disagreement-escalation rule "objective technical reasoning required to override" maps to: the user may override a reviewer comment, but must record the reason, producing an auditable decision trail.
- *Con:* In solo + LLM-agent context, disagreement is between human and agent, and the human is structurally the tiebreaker. The risk is always-overriding. The escalation rule catches this only if it requires *articulated* override reasoning.

**Adaptation notes.** Adopt the **articulated-override** rule: reviewer sub-agent comments that the user disagrees with must be resolved with a recorded reason, not silently dismissed. This produces a record that is itself useful for future decision archaeology.

**Evaluation plan.** Log reviewer comment disposition (acted-on / overridden-with-reason / dismissed-silently) across several kerf works; check whether silently-dismissed comments later resurface as rework.

---

## Considered and partially rejected

**Full ATAM ceremony.** The nine-step / two-phase choreography is team-scale. The utility-tree and four-category-output components port well standalone; the rest does not.

**Live design-review meetings.** The live-dialogue dynamic is specific to multi-human settings. What ports is the *structure* (scribe, time-box, question categories), not the meeting form.

**Readability certification at scale.** Google-style readability operates across ~20% of a company. The structural idea — a dedicated review-axis reviewer — ports, but the mentorship-at-scale aspect does not.

**PR-style inline comments on running code.** Post-implementation timing is wrong for the planning phase; the *question style* ports, the *timing* does not.

**PEP Steering Council governance.** Formal gatekeeping roles require a council; solo context has no council. The hard-gate idea (reject if missing section) is separable and does port.

**RFC community-comment phase.** The multi-week open-comment window is a consensus-building mechanism specific to public governance. The *structured-artifact* part of the RFC ports; the *public-comment* part does not.

---

## Patterns the user has not yet adopted (high-priority findings)

Ordered by estimated signal-to-effort ratio:

1. **Mandatory "alternatives considered" section** (from RFC, Rust, PEP). Zero tooling cost; directly fills an acknowledged gap in the kerf scaffolding. Per the prompt: the user's plans rarely enumerate rejected alternatives explicitly.
2. **Pre-mortem reviewer sub-agent** (Gary Klein). Adversarial-by-construction; produces output class orthogonal to the general reviewer. One prompt, one invocation.
3. **Role-split reviewer library** (devil's advocate, maintainer, simplifier, locked-decisions-guardian). Extends the existing reviewer pattern; small role library of reusable prompt shapes.
4. **ATAM four-category output** (risks / non-risks / sensitivities / tradeoffs). Forces explicit surfacing of non-risks and tradeoffs, categories currently implicit in kerf outputs.
5. **Utility tree for quality attributes**. One structured-artifact addition per kerf design pass; forces abstract quality goals to land on testable scenarios.
6. **Articulated-override rule for reviewer disagreement**. Every overridden reviewer comment produces a recorded reason; produces an auditable decision trail.
7. **Hard gate on mandatory sections** (PEP-style). Reviewer sub-agent blocks pass advancement on missing-or-weak mandatory sections.
8. **Time-boxing on reviewer output**. Forces prioritization; current reviewers are unbounded.

Items 1-3 are likely the highest return; they address named gaps in the corpus at minimal adaptation cost.

---

## Sources

- [Rust RFC template, rust-lang/rfcs 0000-template.md](https://github.com/rust-lang/rfcs/blob/master/0000-template.md)
- [Michael Nygard, "Documenting Architecture Decisions"](https://www.cognitect.com/blog/2011/11/15/documenting-architecture-decisions)
- [Architectural Decision Records site](https://adr.github.io/)
- [Python PEP 1 – PEP Purpose and Guidelines](https://peps.python.org/pep-0001/)
- [Python PEP 387 – Backwards Compatibility Policy](https://peps.python.org/pep-0387/)
- [IETF RFC 2026 – The Internet Standards Process, Revision 3](https://datatracker.ietf.org/doc/html/rfc2026)
- [Phil Calçado, "A Structured RFC Process"](https://philcalcado.com/2018/11/19/a_structured_rfc_process.html)
- [Google eng-practices, "What to look for in a code review"](https://google.github.io/eng-practices/review/reviewer/looking-for.html)
- [Google eng-practices, "The Standard of Code Review"](https://google.github.io/eng-practices/review/reviewer/standard.html)
- [Readability: Google's Temple to Engineering Excellence (Modern Descartes)](https://www.moderndescartes.com/essays/readability/)
- [Software Engineering at Google, Ch. 9 Code Review (Abseil / O'Reilly)](https://abseil.io/resources/swe-book/html/ch09.html)
- [SEI Technical Report, ATAM: Method for Architecture Evaluation (Kazman/Klein/Clements)](https://www.sei.cmu.edu/documents/629/2000_005_001_13706.pdf)
- [Architecture Tradeoff Analysis Method overview](https://en.wikipedia.org/wiki/Architecture_tradeoff_analysis_method)
- [Gary Klein, "Performing a Project Premortem" (HBR)](https://cltr.nl/wp-content/uploads/2020/11/Project-Pre-Mortem-HBR-Gary-Klein.pdf)
- [Gary Klein, PreMortem description](https://www.gary-klein.com/premortem)
- [Pre-mortem overview, Ness Labs](https://nesslabs.com/pre-mortem-anticipate-failure-with-prospective-hindsight)
- [Premortem Technique, Asian Development Bank](https://www.adb.org/sites/default/files/publication/29658/premortem-technique.pdf)
- [GitHub Docs, Reviewing proposed changes in a pull request](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/reviewing-proposed-changes-in-a-pull-request)
- [Stack Overflow Blog, "How to Make Good Code Reviews Better"](https://stackoverflow.blog/2019/09/30/how-to-make-good-code-reviews-better/)
- [Jerry A. Smith, "The Devil's Advocate Architecture"](https://medium.com/@jsmith0475/the-devils-advocate-architecture-how-multi-agent-ai-systems-mirror-human-decision-making-9c9e6beb09da)
- [Design Reviews: Meeting Format, Brian Tajuddin](https://www.briantajuddin.com/design-reviews-meeting-format/)
- [Roles and Responsibilities in Review, GeeksforGeeks](https://www.geeksforgeeks.org/roles-and-responsibilities-in-review/)
