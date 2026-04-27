# Unified Protocol Catalog

**Phase 2, Step 4 artifact.** Author: consolidation sub-agent, 2026-04-23. Track: planning-protocols.

This catalog consolidates every candidate protocol surfaced during Phase 1 (observed corpus patterns), Phase 2 Step 2 (ten external-domain surveys), and Phase 2 Step 3 (eight counter-pattern protocols) onto a single shared schema. It is the input to Step 5 (reviewer-challenged evaluation) and Step 6 (ranked recommendations). It does not rank, does not recommend, and does not favor any origin stream over another.

## Schema

Every entry has eight mandatory fields:

1. **Name** — `kebab-case-id` + human-readable name. Alternate source names listed where they help.
2. **Origin** — one or more of:
   - `observed` — from Phase 1 corpus analysis (research-statement §4, `phases/phase-1/tried-protocols.md`, or six lens reports).
   - `unexplored` — from research-statement §5 (unexplored-region candidates).
   - `external:<domain>` — from one of the 10 Step 2 external-source files (e.g., `external:medical-handoffs`, `external:military-briefings`).
   - `counter-pattern:<N>` — from the eight Step 3 counter-protocols (`counter-pattern-candidates.md`).
3. **One-line definition.**
4. **Dimension values** — the 10-dimension vector from research-statement §3. Only axes with clear values are listed; uncertain ones omitted (or flagged).
5. **Mechanism** — 1-3 sentences on theory of action.
6. **Predicted trade-offs** — 1-3 sentences on what worsens while the target improves.
7. **Strongest evidence** — one-sentence pointer to best evidence for/against.
8. **Evaluation plan** — one-sentence specification of the transcript-derivable or experimentally-measurable signal that would distinguish this protocol from its closest rival.

## Origin legend

| Tag | Source file | Content |
|---|---|---|
| `observed` | research-statement §4, `phases/phase-1/tried-protocols.md`, six lens reports | Patterns already tried in the user's own sessions |
| `unexplored` | research-statement §5 | Regions the user's practice has not meaningfully populated |
| `counter-pattern:N` | `phases/phase-2/analysis/counter-pattern-candidates.md` | Steel-manned inverses of Phase 1 findings (N = 1..8) |
| `external:pair-programming` | `external-sources/pair-programming.md` | Driver-navigator, strong-style, ping-pong, mob |
| `external:socratic-method` | `external-sources/socratic-method.md` | Elenchus, maieutics, dialectic, Bloom/Graesser/Paul-Elder, Five Whys |
| `external:medical-handoffs` | `external-sources/medical-handoffs.md` | SBAR, I-PASS, teach-back, CUS, mnemonic variants |
| `external:design-review` | `external-sources/design-review.md` | RFC, ADR, PEP, Google code review, ATAM, pre-mortem, role-split |
| `external:negotiation-mediation` | `external-sources/negotiation-mediation.md` | Fisher/Ury, Ury, Stone-Patton-Heen, NVC, narrative mediation |
| `external:incident-command` | `external-sources/incident-command.md` | ICS, commander's intent, IAP, sitrep, tactical pause, AAR, named-roles |
| `external:pilot-controller` | `external-sources/pilot-controller.md` | ICAO readback, fixed vocabulary, PACE, sterile cockpit, authority transfer |
| `external:therapy-intake` | `external-sources/therapy-intake.md` | OARS, EPE, Four-Process, readiness ruler, SCID-screener |
| `external:consulting-discovery` | `external-sources/consulting-discovery.md` | MECE, issue tree, SPIN, hypothesis-driven, Pyramid, SCQA, engagement letter, laddering |
| `external:military-briefings` | `external-sources/military-briefings.md` | Commander's intent, SMEAC, back-brief, WARNO/OPORD/FRAGO, METT-TC, rehearsal, TLP |

## Deduplication policy

Observed + counter-pattern pairs are NOT consolidated (counter-patterns are explicit inverses; Step 5 must evaluate both). Observed + external convergences (e.g., observed *pre-action plan disclosure* ≈ external *back-brief*) are noted but kept as separate entries because the mechanism framing differs. True surface-equivalent protocols from different external domains (e.g., `medical:read-back` ≈ `aviation:readback`) are consolidated into one entry with multiple origins.

## Index

### Group 1 — Opener structures (session-opener templates)

- `sbar-opener` — SBAR (Situation / Background / Assessment / Recommendation)
- `ipass-opener` — I-PASS (Illness / Patient / Action / Situation-awareness / Synthesis)
- `commanders-intent` — Commander's Intent (Purpose / Key Tasks / End State)
- `smeac-order` — Five-Paragraph Order (SMEAC)
- `scqa-opener` — SCQA (Situation / Complication / Question / Answer)
- `engagement-letter-opener` — Engagement Letter with Out-of-Scope
- `context-dump` — Rich-Brief Context-Dump (observed)
- `recovery-handoff` — Session-Recovery Handoff (observed)
- `autonomous-dispatch` — Minimal Dispatch + Pre-built Specs (observed)
- `sterile-phase-declaration` — Sterile Cockpit / Focus-Phase Designation
- `warno-prep` — Warning Order (prep-authorization-without-execution)
- `mett-tc-sweep` — METT-TC Mission-Analysis Checklist
- `batna-articulation` — BATNA Articulation
- `agenda-setting` — MI Agenda Setting / Focusing

### Group 2 — Question discipline

- `elenctic-probe` — Elenctic Probe
- `maieutic-drawout` — Maieutic Draw-Out
- `reduced-dialectic` — Reduced Dialectic (position / counter / synthesis)
- `socratic-seminar-turn` — Socratic Seminar Turn Frame
- `question-level-targeting` — Bloom / Graesser / Paul-Elder two-bucket filter
- `five-whys-bounded` — Bounded Causal-Antecedent Cascade (Five Whys)
- `laddering-acv` — Laddering (Attribute → Consequence → Value)
- `spin-sequence` — SPIN (Situation / Problem / Implication / Need-payoff)
- `interest-probe` — Interest-vs-Position Surfacing
- `option-invention` — Option Invention Before Commitment
- `objective-criteria` — Objective-Criteria Anchoring
- `evocation-darncat` — MI Evocation / Change-Talk
- `epe-information` — Elicit-Provide-Elicit
- `readiness-ruler` — Readiness / Confidence Ruler
- `numbered-question-close` — Numbered-Question Close (observed)
- `implication-question-discipline` — Implication-question requirement (SPIN-derived)

### Group 3 — Decision-locus and autonomy

- `upfront-decision-partition` — Upfront Decision Partition (observed)
- `emergent-partition` — Emergent-Partition Protocol (counter-pattern #3)
- `mission-command` — Mission Command Doctrine (bounded-by-intent autonomy)
- `autonomy-scope-grant` — Bounded-by-intent autonomy grant
- `contingency-preauthorization` — Contingency Pre-Authorization (I-PASS if-then)
- `incremental-step-autonomy` — Incremental-Step Autonomy (observed + unexplored)
- `micro-step-incrementalism` — Micro-Step Incrementalism (counter-pattern #5)
- `question-preserving-autonomy` — Question-Preserving Autonomy (counter-pattern #7)
- `authority-transfer-microritual` — Authority-Transfer Micro-Ritual
- `pre-action-plan-disclosure` — Pre-Action Plan Disclosure (observed)
- `fixed-token-status-vocabulary` — Fixed-Token Status Vocabulary (proposed/locked/deferred)
- `hypothesis-driven-ghost-deck` — Hypothesis-Driven Commitment (Day-1 answer + ghost deck)
- `example-led-emergence` — Example-Led Emergence (counter-pattern #1)
- `forced-choice-with-default` — Forced-Choice-with-Default (observed + unexplored)

### Group 4 — Review and read-back

- `read-back-comprehension` — Comprehension Read-Back (medical / aviation / MI reflection)
- `load-bearing-token-readback` — Load-Bearing-Token Readback (per-turn, aviation-style)
- `back-brief-plan-quality` — Back-Brief (plan-quality check, distinct from read-back)
- `teach-back-loop` — Closed-Loop Teach-Back
- `iap-cadence-artifact` — Written Cadence Artifact (adapted IAP)
- `sitrep-at-cadence` — Sitrep at Cadence (structured mid-session checkpoint)
- `premortem-reviewer` — Pre-Mortem Reviewer (prospective hindsight)
- `kerf-parallel-reviewer` — Parallel Reviewer (kerf pattern, observed)
- `alternatives-considered-section` — Mandatory Alternatives-Considered Section
- `pyramid-answer-first` — Pyramid Principle / Answer-First Output
- `so-what-test` — So-What Test on Supporting Paragraphs
- `utility-tree` — Utility Tree (quality attributes → scenarios)
- `four-category-output` — ATAM Risks / Non-Risks / Sensitivities / Tradeoffs
- `design-review-categorization` — Structured Reviewer-Output Categorization
- `aar-four-question` — After-Action Review (four-question, blameless)
- `soapy-challenge-response-gate` — SOP Challenge-Response Completion Gate
- `confirmation-brief` — Confirmation Brief (pre-planning comprehension check)
- `closing-summary-ritual` — Stakeholder-Interview Closing-Summary Ritual
- `articulate-override-rule` — Articulated-Override for Reviewer Disagreement
- `hard-gate-missing-section` — Hard Gate on Mandatory Sections (PEP-style)

### Group 5 — Mediator / role-split

- `shuttle-diplomacy-reviewer` — Shuttle-Diplomacy Reviewer (mediator sub-agent)
- `single-text-procedure` — Single-Text Procedure (agent-as-drafter)
- `role-split-reviewer-library` — Role-Split Reviewer Library (devil's advocate, maintainer, simplifier, guardian)
- `named-role-separation` — Named-Role Separation (IC / OL / Scribe / Planning) with solo-collapse
- `can-report-format` — CAN-Report Format (Condition / Actions / Needs)
- `scribe-sub-agent` — Scribe Sub-Agent (dedicated session-log agent)
- `asymmetric-abstraction-shifting` — Asymmetric Abstraction-Shifting (strong-style adapted)
- `cognitive-tag-team` — Cognitive Tag Team (symmetric fluid role-switching)
- `classical-driver-navigator` — Classical Driver-Navigator
- `strong-style-pairing` — Strong-Style Pairing (Falco)
- `ping-pong-alternation` — Ping-Pong Pairing (TDD alternation)
- `mob-parallel-reviewers` — Mob Programming's Parallel-Reviewers adaptation
- `asynchronous-navigator` — Asynchronous-Navigator (agent-checkpoints + human-skim)

### Group 6 — Plan expression / artifact form

- `rfc-full-form` — IETF/Rust-style RFC (full structured artifact)
- `adr-nygard` — Architectural Decision Record (Nygard)
- `pep-style-rfc` — PEP-style RFC with hard gates
- `issue-tree-diagnostic` — Diagnostic Issue Tree (top-down, uneven-depth)
- `issue-tree-solution` — Solution Issue Tree (top-down, uneven-depth)
- `mece-decomposition` — MECE Decomposition
- `dialog-log-plan` — Dialog-Log Plan (plan lives in chat, observed)
- `behavior-first-plan` — Behavior-First Plan Expression (unexplored)
- `assumption-bundle` — Assumption-Bundle Disclosure (counter-pattern #2)
- `knowledge-state-inventory` — Knowledge-State Mapping inventory (counter-pattern #8)
- `nvc-four-slot` — Compressed NVC (Observation / Implication / Need / Request)
- `coding-opord-4slot` — Coding-OPORD 4-slot (Context / Outcome / Approach / Prerequisites-Comms)
- `frago-modification` — Fragmentary Order (mid-plan diff artifact)
- `position-interest-pair` — Position/Interest Pair (spec artifact)
- `test-cases-as-plan` — Test-Cases as Plan (ping-pong-derived)

### Group 7 — Meta-protocols / shape-shifting / stance

- `protocol-shapeshifting` — Protocol Shape-Shifting (unexplored)
- `four-process-arc` — MI Four-Process Arc (engage / focus / evoke / plan)
- `aporia-graceful-stop` — Aporia as Graceful-Stop Signal
- `tactical-pause` — Tactical Pause (fire-service)
- `narrative-reframe` — Narrative-Reframe (Stuck-Story Rewrite)
- `going-to-the-balcony` — Going-to-the-Balcony (stall-on-pushback)
- `rolling-with-resistance` — Rolling with Resistance
- `third-story-framing` — Third-Story Framing
- `positive-no` — Positive-No (Yes-No-Yes assertion)
- `non-directive-stance` — Non-Directive Stance (Rogerian core, micro-moment)
- `active-listening-readback` — Active-Listening Read-Back (Rogers/Gordon)
- `gordon-roadblock-filter` — Gordon's Twelve-Roadblock Rejection Filter
- `directional-clean-repetition` — Directional Clean-Repetition (Grove)
- `affirmations-competence` — Competence-Affirmation (non-obsequious)
- `graded-assertiveness-pace` — PACE Graded-Assertiveness
- `cus-graduated-concern` — CUS-Graduated-Concern (medical analog of PACE)
- `agent-surfaced-parking` — Agent-Surfaced Parking (unexplored)
- `screener-gated-branching` — Screener-Gated Branching (SCID/MINI-style)
- `handoff-closed-acknowledgment` — Handoff Closed-Acknowledgment
- `error-catching-posture` — Error-Catching as Posture (pair programming general)
- `pre-reply-self-review` — Pre-Reply Self-Review by Agent (unexplored)
- `watcher-tier-orientation` — Watcher-Tier Session Orientation
- `onethird-twothirds-time` — 1/3-2/3 Time Allocation Rule
- `rehearsal-hierarchy` — Graded Rehearsal Hierarchy
- `controller-orchestration` — Controller Orchestration (observed)
- `dialogic-context-accretion` — Dialogic Context Accretion (counter-pattern #6)

### Group 8 — Counter-commitments (methodological)

- `incident-driven-catalog` — Incident-Driven Change Catalog (methodological borrow)
- `summary-as-transition` — Summary-as-Transition (MI)

---

## Group 1 — Opener structures

### sbar-opener — SBAR (Situation / Background / Assessment / Recommendation)

- **Origin:** `external:medical-handoffs`
- **One-line:** Four-slot opener template: *Situation* (immediate framing), *Background* (priors default priors would miss), *Assessment* (human's interpretation), *Recommendation* (proposed direction).
- **Dimension values:**
  - Timing of alignment: pre-action
  - Decision locus: mixed (sender proposes, receiver may revise)
  - Dialog form: short-volley, structured
  - Question style: implicit (slots are the questions)
  - Autonomy scope: bounded-by-category (four slots)
  - Context richness: rich-brief
  - Plan expression form: structured-artifact
  - Knowledge direction: human-dominant with expected pushback at A and R
- **Mechanism:** Compresses priors into transmissible form, so receiver uses their bandwidth to challenge rather than gather; A/R separation lets the receiver correct framing without having to re-propose action.
- **Predicted trade-offs:** Four slots is thin for a multi-hour planning session; A/R may collapse into R if the human skips interpretation; doesn't include a scope/autonomy slot which coding-planning needs.
- **Strongest evidence:** SBAR is the most-cited handoff mnemonic (69.6% of handoff-literature studies, Riesenberg et al.); outcome evidence for full-handoff use is mixed — strongest when bundled with other moves.
- **Evaluation plan:** Classify existing openers against SBAR slots; correlate absent-B (Background) with downstream misaligned-assumption rate.

### ipass-opener — I-PASS (Illness / Patient / Action / Situation-awareness / Synthesis)

- **Origin:** `external:medical-handoffs`
- **One-line:** Five-slot shift-handoff template: severity tag, narrative summary, owned action list, if-then contingencies, and *mandatory receiver synthesis* before handoff closes.
- **Dimension values:**
  - Timing of alignment: pre-action, with explicit synthesis gate
  - Decision locus: pre-authorized on Action list + contingencies; interactive on novel cases
  - Dialog form: structured, medium-length, with mandatory synthesis
  - Autonomy scope: bounded-by-category with contingency-gated escalation
  - Plan expression form: structured-artifact
  - Review/critique integration: read-back (mandatory)
- **Mechanism:** Compresses complex cognitive state AND verifies the compression survived transport via receiver synthesis; contingency plans pre-delegate judgment for anticipated novel states; severity tag orients receiver before they process detail.
- **Predicted trade-offs:** Full bundle is heavy (5-15 min per handoff); contingency under-specification gives false pre-authorization; synthesis-by-receiver changes agent voice ("so what I'm hearing is…").
- **Strongest evidence:** I-PASS produced 30% reduction in serious preventable medical errors at 9 academic children's hospitals (Starmer et al., NEJM 2014) — the strongest causal evidence any communication protocol has for safety outcomes.
- **Evaluation plan:** Test severity-tag, read-back, and contingency-pre-auth as isolated ingredients and measure which single move dominates the bundle's value.

### commanders-intent — Commander's Intent (Purpose / Key Tasks / End State)

- **Origin:** `external:military-briefings`, `external:incident-command`
- **One-line:** A 2-3 sentence statement of *why* the operation exists (purpose), *what any plan must accomplish* (key tasks), and *what done looks like* (end state), designed to survive plan failure and pre-authorize subordinate deviation.
- **Dimension values:**
  - Timing of alignment: pre-action, session-standing
  - Decision locus: pre-authorized (subordinate/agent deviates within intent)
  - Dialog form: structured (short prose artifact)
  - Autonomy scope: **bounded-by-intent** (new value; broader than bounded-by-category or bounded-by-contingency)
  - Context richness: rich at intent level, deliberately thin on procedure
  - Plan expression form: intent-artifact (distinct from plan)
  - Knowledge direction: principal → subordinate with back-brief loop
- **Mechanism:** Plans fail on contact with reality; intent is the residual guidance that survives plan failure. Compresses principal's cognition into a transferable form. Refuses to specify *how* — the prescribed response to uncertainty is to specify *less* of the plan and *more* of the intent.
- **Predicted trade-offs:** Writing good intent is a senior skill that takes years; "how-leak" failure (developer specifies tooling and calls it intent); over-compression omits load-bearing constraints; intent is hard to verify the agent internalized without a paired back-brief.
- **Strongest evidence:** Doctrinal centerpiece of US Army ADP 6-0 Mission Command and Moltke's Auftragstaktik (≥150 years of iteration); "two echelons down" test is the doctrinal quality check.
- **Evaluation plan:** Classify corpus openers against {purpose, key-tasks, end-state} slots and correlate absent-end-state with agent-defers-trivial-decisions rate (A1 in the evaluation framework).

### smeac-order — Five-Paragraph Order (SMEAC)

- **Origin:** `external:military-briefings`
- **One-line:** Five-paragraph OPORD template — *Situation*, *Mission* (one-sentence who-what-where-when-why), *Execution* (the plan), *Admin & Logistics*, *Command & Signal* — where the Mission/Execution split preserves the ask when the plan fails.
- **Dimension values:**
  - Timing of alignment: pre-action, after intent is settled
  - Decision locus: principal retains Mission; Execution is where agent input is expected
  - Dialog form: structured-artifact (written + verbal)
  - Plan expression form: structured-artifact (paradigm case)
  - Knowledge direction: principal → subordinate with clarification
- **Mechanism:** Each paragraph targets a different error class — drop Situation → stale context, drop Mission → summary-loss, drop Execution → uncoordinated action, drop A&L → unsupported execution, drop C&S → coordination-under-degradation. Mission/Execution separation (like SBAR A/R) lets receivers correct plan without losing the ask.
- **Predicted trade-offs:** Overkill for one-line changes; the template becomes ceremonial if filled mechanically; A&L and C&S don't transfer directly to solo 2-party work.
- **Strongest evidence:** Standardized across USMC and US Army infantry for decades; the specific slot contents each trace to named error classes catalogued in doctrine.
- **Evaluation plan:** Audit corpus openers for Mission/Execution conflation rate (ask stated in terms of proposed implementation); high conflation supports the split as load-bearing.

### scqa-opener — SCQA (Situation / Complication / Question / Answer)

- **Origin:** `external:consulting-discovery`
- **One-line:** Minto's 3-5-sentence opening structure — establish shared Situation, name the Complication disrupting it, let the Question surface implicitly, lead with the Answer.
- **Dimension values:**
  - Timing of alignment: at session/artifact opener
  - Dialog form: structured opener
  - Context richness: rich-brief (compact)
  - Plan expression form: structured-artifact (opener)
- **Mechanism:** Situation aligns parties on baseline *before* disruption is introduced; Complication names the change; leaving the Question implicit invites audience to arrive at it themselves; Answer heads the Pyramid Principle body.
- **Predicted trade-offs:** Assumes author already knows the Answer (degenerates to SCQ when Answer is genuinely unknown); formulaic openers read as corporate.
- **Strongest evidence:** Minto's canonical opener for pyramid-structured business communication; adopted firm-wide at McKinsey.
- **Evaluation plan:** Classify corpus openers as SCQA-complete, SC-only, C-only, or dump; correlate structure with time-to-first-decision.

### engagement-letter-opener — Engagement Letter with Out-of-Scope

- **Origin:** `external:consulting-discovery`
- **One-line:** Contractual-style opener with mandatory sections: Objectives, Scope, **Out-of-Scope (explicit)**, Deliverables with acceptance criteria, Governance/decision rights, Change-order process.
- **Dimension values:**
  - Timing of alignment: pre-action, ratified
  - Decision locus: interactive, ratified
  - Autonomy scope: bounded-by-scope (Scope section is the autonomy boundary)
  - Plan expression form: structured-artifact
  - Review/critique integration: formal ratification move
- **Mechanism:** Explicit out-of-scope section converts scope creep from silent expansion into a named change-order event; acceptance criteria per deliverable catch done-ness ambiguity; governance names who decides what.
- **Predicted trade-offs:** Full ceremony is too heavy for small sessions; over-specifying out-of-scope can miss categories the enumerator didn't anticipate; needs a "catch-all: anything not explicitly in-scope requires a change order" clause.
- **Strongest evidence:** Practitioner-standard across consulting firms; out-of-scope ambiguity is a named top failure mode in the scope-creep literature (Sirion, nmsconsulting, Ignition).
- **Evaluation plan:** Retroactively construct out-of-scope sections for finalized kerf works using the eventual implementation; count how often out-of-scope items were raised as ambiguities mid-planning.

### context-dump — Rich-Brief Context-Dump

- **Origin:** `observed` (13493c8d harmonik, 5 human turns of 5294/1903/3441 chars)
- **One-line:** Human writes a long upfront brief (thousands of characters); agent produces many small responses against it; few human turns total.
- **Dimension values:**
  - Timing of alignment: pre-action (all upfront)
  - Dialog form: long-message
  - Context richness: rich-brief (extreme)
  - Knowledge direction: human-dominant
- **Mechanism:** Front-loads writing in one burst; minimizes context-switches during agent run; agent's work is "respond to the brief" rather than "co-design."
- **Predicted trade-offs:** High upfront cognitive load on human; no correction loop for misaligned assumptions until much later; eliminates the dialogic reveal-during-session effect; brittle if human's model shifts.
- **Strongest evidence:** Observed in 13493c8d (harmonik founding-vision session); lens reports identify this as the pattern where errors propagate furthest before detection.
- **Evaluation plan:** Compare sessions opened with context-dump vs. SCQA-shaped opener on (a) first-correction turn number and (b) total human character count across the session.

### recovery-handoff — Session-Recovery Handoff

- **Origin:** `observed` (729dad16 kerf)
- **One-line:** Opening message of the form "# Session Recovery Context" + structured checkpoint payload, used when resuming across sessions.
- **Dimension values:**
  - Timing of alignment: pre-action
  - Context richness: recovery-handoff (new value for a structured catch-up)
  - Plan expression form: structured-artifact
- **Mechanism:** Carries state across sessions without relying on agent memory; agent gets a structured catch-up rather than having to infer where prior session left off.
- **Predicted trade-offs:** Payload bloats on long-running works; requires discipline to keep current; silent-context-loss if the payload is out of date.
- **Strongest evidence:** 729dad16 (kerf) is the canonical observed instance; converges with incident-command's "handoff with closed acknowledgment."
- **Evaluation plan:** For multi-session works, compare sessions resumed with recovery-handoff vs. direct-continue on first-N-turn correction rate.

### autonomous-dispatch — Minimal Dispatch + Pre-built Specs

- **Origin:** `observed` (~100/133 secure-dev sessions)
- **One-line:** Single-line directive ("study specs/ and pick the most important thing to do") + test/commit rules + *"Never ask the human questions or wait for input."* — the planning dialog happened in a prior session.
- **Dimension values:**
  - Timing of alignment: none (in this session; alignment was in the prior planning session)
  - Decision locus: agent-autonomous
  - Autonomy scope: unbounded
  - Context richness: minimal dispatch + rich pre-built specs
- **Mechanism:** Pre-solves decision-delegation by forcing all remaining decisions to the agent; removes conversation overhead entirely; bets on the prior session's spec quality.
- **Predicted trade-offs:** Silent drift when spec is ambiguous; no correction loop; pre-built-spec must be exceptionally complete; "don't ask questions" creates false confidence.
- **Strongest evidence:** Dominant pattern in secure-dev corpus; the user's flagged solution to time-constrained decision-delegation.
- **Evaluation plan:** Track interruption/restart rate on autonomous-dispatch sessions vs. matched interactive sessions; interruption rate is the cost measure.

### sterile-phase-declaration — Sterile Cockpit / Focus-Phase Designation

- **Origin:** `external:pilot-controller`, related to `unexplored` (protocol shape-shifting)
- **One-line:** At session open, name the phase (Spec-drafting / Exploratory-decomposition / Research-synthesis) with declared on-topic and off-topic moves; agent empowered to redirect drift with a scripted one-liner.
- **Dimension values:**
  - Timing of alignment: phase-gated
  - Decision locus: pre-authorized (rule set before session)
  - Dialog form: short-volley only within sterile phases
  - Question style: strictly task-relevant during sterile
  - Autonomy scope: reduced during sterile
  - Branching: sterile = linear
- **Mechanism:** Focus is finite; the declared rule lets the *other* party interrupt a digression without social imposition; "sterile cockpit" as a spoken phrase is itself a protocol move.
- **Predicted trade-offs:** Rigidity — sometimes the tangent is the important thing; authority asymmetry in solo + agent (user can break rule at will); must define "sterile" narrowly.
- **Strongest evidence:** FAA 14 CFR § 121.542 after Eastern 212 (1974) loss-of-altitude-awareness during approach-chatter.
- **Evaluation plan:** Two kerf works, one with phase declaration + agent-redirection empowerment, one without; measure tangent count and tangent resolution (addressed / parked / lost).

### warno-prep — Warning Order (Prep-Without-Execution)

- **Origin:** `external:military-briefings`
- **One-line:** A session-opener form that authorizes preparation (gathering context, reading relevant files) but explicitly *not* implementation; becomes WARNO → OPORD split.
- **Dimension values:**
  - Timing of alignment: staged (pre-pre-action)
  - Decision locus: prep authorized, execution not
  - Context richness: graduated (thin at WARNO, rich at OPORD)
  - Plan expression form: structured-artifact with inheritance
- **Mechanism:** Amortizes planning over time — agent begins prep work while user finalizes the full ask; authorization gating keeps prep from becoming premature execution.
- **Predicted trade-offs:** Three-tier overhead for short sessions; FRAGO-overuse risk (incremental patches lose coherence).
- **Strongest evidence:** WARNO/OPORD/FRAGO is doctrinal across US Army, USMC, and NATO; specifically designed for parallel-planning efficiency.
- **Evaluation plan:** For kerf works with clear phases, A/B session openers using WARNO-shaped "heads up, gather X, no implementation yet" vs. direct dispatch; measure user-correction frequency in first five agent turns.

### mett-tc-sweep — METT-TC Mission-Analysis Checklist

- **Origin:** `external:military-briefings`
- **One-line:** A 6-slot context checklist the agent runs *before* proposing — Mission (ask), Opposing-forces (what fights the change), Terrain (codebase context), Troops (capabilities/tooling), Time (budgets), Users (downstream stakeholders) — making ignorance visible in named categories.
- **Dimension values:**
  - Timing of alignment: pre-plan
  - Dialog form: structured checklist, self-administered
  - Question style: explicit (each slot is a question)
  - Context richness: rich (exhaustive coverage by design)
- **Mechanism:** Without a checklist, the planner does not know what they do not know; each empty slot is a recognizable gap that either prompts info-gathering, is explicitly accepted as unknown, or is escalated.
- **Predicted trade-offs:** Checklist fatigue (6 slots is upper bound); becomes report-not-input if surfaced as artifact rather than internal sweep.
- **Strongest evidence:** Doctrinal across US Army / USMC small-unit leadership training; survived revision into METT-TC from METT-T specifically because civil considerations kept being missed.
- **Evaluation plan:** Agent runs METT-TC sweep as first action; classify corpus sessions by which slots were investigated; correlate absent slots with downstream rework.

### batna-articulation — BATNA Articulation

- **Origin:** `external:negotiation-mediation`
- **One-line:** At session start, each party names what they'll do if the session fails to produce an acceptable plan ("human will write it themselves"; "agent will make a default assumption and proceed").
- **Dimension values:**
  - Timing of alignment: pre-action, at session start
  - Decision locus: agent-autonomous (agent's fallback) + human-declared (human's fallback)
  - Dialog form: structured, single named move
  - Plan expression form: structured (two-slot artifact)
- **Mechanism:** Leverage in negotiation comes from what you'll walk away to; unarticulated BATNA can't ground decisions; explicit BATNAs give the human permission to reject the plan without it feeling like losing, which reduces defensive over-specification.
- **Predicted trade-offs:** In fully cooperative sessions reads as paranoid; human BATNA ("write it myself") is awkward to state aloud.
- **Strongest evidence:** Fisher/Ury/Patton, *Getting to Yes* — most-cited negotiation concept after principled negotiation itself.
- **Evaluation plan:** In multi-session works, compare sessions with vs. without opener BATNA for session-end behavior on non-converging plans (early abort = good signal).

### agenda-setting — MI Agenda Setting / Focusing

- **Origin:** `external:therapy-intake`
- **One-line:** Agent proposes a 3-5 item slate of candidate topics; human selects, reorders, or adds; un-selected items become the next-session agenda.
- **Dimension values:**
  - Timing of alignment: pre-session, pre-pass
  - Decision locus: mixed (agent proposes slate, human selects)
  - Dialog form: structured (list + selection), short
  - Plan expression form: structured (agenda artifact)
  - Fuzziness-stability: stabilizes by pinning one topic while parking others
- **Mechanism:** Having the speaker name the topic produces investment in it; prevents the agent's default topic from dominating; the un-selected items are useful carry-forward rather than lost.
- **Predicted trade-offs:** Bureaucratic for one-topic sessions; the agent's slate reflects agent's model — a wrong slate miscues selection.
- **Strongest evidence:** Operationalized in Miller & Rollnick (MI 4th ed.) as the focusing process; partnership/collaboration is most concretely realized at agenda-setting.
- **Evaluation plan:** Apply at kerf-pass transitions; check whether the agenda matches what actually gets discussed, and whether un-selected items are retained for later.

---

## Group 2 — Question discipline

### elenctic-probe — Elenctic Probe

- **Origin:** `external:socratic-method`
- **One-line:** Agent takes a stated assumption, derives a consequence, and asks whether the human accepts the consequence — probing for contradiction between stated intent and implied commitments.
- **Dimension values:**
  - Timing of alignment: pre-action
  - Decision locus: interactive (agent surfaces, human decides)
  - Dialog form: short-volley, one question at a time
  - Question style: one-at-a-time, narrow-open
  - Knowledge direction: agent-investigative
- **Mechanism:** Accept thesis, draw derivation that feels inescapable, expose joint inconsistency with thesis; forces refinement, retraction, or re-grounding.
- **Predicted trade-offs:** Repetitive elenchus reads as agent stalling; adversarial posture triggers defensiveness in senior responder; in fast sessions, short-volley doubles turn count.
- **Strongest evidence:** Plato's early dialogues (Meno, Euthyphro, Laches) as foundation; the "senior-responder" inversion warned against in the Socratic analysis.
- **Evaluation plan:** Corpus sessions flagged "misaligned-assumption" — reconstruct whether an elenctic probe at the first-drift turn would have surfaced the consequence earlier.

### maieutic-drawout — Maieutic Draw-Out

- **Origin:** `external:socratic-method`, convergent with `counter-pattern:1` (example-led emergence via concrete cases)
- **One-line:** Agent assumes the human holds a latent position and asks questions whose purpose is to make it explicit — *without contributing content of its own* until the position is on the page.
- **Dimension values:**
  - Timing of alignment: pre-action, often early-phase
  - Decision locus: human-preempted (agent deliberately not deciding)
  - Dialog form: short-volley, many small exchanges
  - Question style: one-at-a-time, open-ended, minimal-interference
  - Autonomy scope: none (draw-out mode)
  - Knowledge direction: agent-investigative (explicitly)
- **Mechanism:** Socrates' midwife metaphor; because the agent introduces no content, there is no drift to correct later; verbatim-repetition preserves speaker's structure undistorted.
- **Predicted trade-offs:** Senior user + junior LLM + relentless open questions = insulting; slow by design; risks narrowing prematurely to human's initial framing.
- **Strongest evidence:** Plato's *Theaetetus*; Grove's Clean Language as modern operationalization; matches observed failure mode where agent-paraphrase-drift enters at restatement.
- **Evaluation plan:** For one node per session where agent's internal model is weakest, agent enters maieutic mode; measure human character-count in that node vs. comparable non-maieutic nodes.

### reduced-dialectic — Reduced Dialectic (position / counter / synthesis)

- **Origin:** `external:socratic-method`, convergent with `external:consulting-discovery` hypothesis-driven consulting's counter-discipline
- **One-line:** Agent lays out position under consideration, lays out its strongest counter, and asks the human to rule on the synthesis — a three-move exchange rather than free dialogue.
- **Dimension values:**
  - Timing of alignment: pre-action
  - Decision locus: mixed (agent drafts triad, human rules)
  - Dialog form: structured (three named moves)
  - Plan expression form: structured (three named slots)
  - Review/critique integration: sub-agent-reviewer fits naturally (reviewer challenges the synthesis)
- **Mechanism:** A position held without its counter is held shallowly; forces the agent to generate the counter rather than advocating its first answer.
- **Predicted trade-offs:** Heavyweight for small decisions; straw-counter pathology — agents generate weak counters and pretend they're strong; the human decision is one-shot.
- **Strongest evidence:** Plato's middle dialogues; Smith 2024 multi-agent research on thesis-antithesis-synthesis; directly distinguishable from the observed kerf parallel-reviewer because kerf reviews output, not position-counter triads.
- **Evaluation plan:** For 5 consequential decisions in finished plans, retrospectively generate triads; score whether counter was substantive vs. straw.

### socratic-seminar-turn — Socratic Seminar Turn Frame

- **Origin:** `external:socratic-method`
- **One-line:** Three-phase exchange — *opening* (open, text-grounded, non-yes/no), *follow-up* (elaborate/challenge without resolving), *closing* (responder states what they now believe) — imported from classroom seminar practice.
- **Dimension values:**
  - Timing of alignment: continuous, per-topic
  - Decision locus: interactive
  - Dialog form: structured (three-phase)
  - Question style: open at open and close, narrowing in follow-up
  - Review/critique integration: human-self-review at closing
- **Mechanism:** Formal structure disciplines facilitator away from prosecuting a hidden answer; closing move forces production of commit-able artifact.
- **Predicted trade-offs:** Phase rigidity performative in short sessions; cross-examination feel with single responder.
- **Strongest evidence:** Facing History, Paideia, AVID Socratic-seminar protocols; directly borrows the "closing-ritual artifact" move.
- **Evaluation plan:** At kerf-pass transitions, audit whether closing-ritual artifact was produced or session drifted to end.

### question-level-targeting — Question-Level Targeting (Bloom / Graesser / Paul-Elder)

- **Origin:** `external:socratic-method`
- **One-line:** Agent classifies each question by cognitive level before asking; suppresses Bloom-level-1-2 (recall/understand) questions when the answer is already on the page, surfaces level-4-5 (analyze/evaluate) questions that change what gets built.
- **Dimension values (meta-protocol):**
  - Timing of alignment: per-question
  - Decision locus: agent-autonomous (the classification)
  - Review/critique integration: self-review on agent side
- **Mechanism:** Bad questioning in practice is asking shallow-type when situation called for deep; two-bucket filter ("could I answer this from information already on the page?") suppresses trivial and surfaces load-bearing.
- **Predicted trade-offs:** Pre-classification has its own cost; label-pleasing pathology if agent classifies to satisfy taxonomy rather than catch.
- **Strongest evidence:** Anderson & Krathwohl's Bloom revision; Graesser & Person 1994's 16-category taxonomy with levels 10-15 correlated to deep cognition; Paul-Elder intellectual standards.
- **Evaluation plan:** Audit sample of agent-to-human questions; hypothesize trivial-flagged questions cluster at Bloom 1-2; measure question-count drop after two-bucket filter deployment.

### five-whys-bounded — Bounded Causal-Antecedent Cascade

- **Origin:** `external:socratic-method` (bounded form of Toyota Five Whys)
- **One-line:** On a specific decision whose rationale has not been written, agent asks "what earlier choice made this necessary?" 2-3 times (not 5), substituting the specific causal form for the generic "why," and writes back the inferred root cause for ratification.
- **Dimension values:**
  - Dialog form: short-volley, predictable
  - Question style: one-at-a-time, causal-antecedent form
  - Plan expression form: dialog-log producing a chain artifact
  - Knowledge direction: agent-investigative
- **Mechanism:** Iterate toward structural reason; stopping rule gates against ceremony; the chain artifact is directly useful as decision rationale in the spec.
- **Predicted trade-offs:** Literal repeated "why?" reads as agent stalling; linear-cause-chain assumption brittle even in Toyota's home domain.
- **Strongest evidence:** Toyota Production System (Ohno); Minoura's internal critique that literal form is too shallow.
- **Evaluation plan:** Find a corpus decision whose eventual spec rationale is shallow; was there a point where a bounded cascade would have gone deeper?

### laddering-acv — Laddering (Attribute → Consequence → Value)

- **Origin:** `external:consulting-discovery`
- **One-line:** On a specific acceptance criterion the agent judges as a surface-attribute, agent asks "why does that matter?" twice — producing an Attribute → Consequence → Value chain — and records it as spec material.
- **Dimension values:**
  - Dialog form: short-volley (2-4 turns)
  - Question style: one-at-a-time, patterned
  - Plan expression form: A-C-V chain is a compact artifact
  - Knowledge direction: agent-investigative
- **Mechanism:** Surface statements are often not load-bearing; the actual decision criterion is further down the value ladder; A-C-V exposes the actual criterion.
- **Predicted trade-offs:** Mechanical "why is that important?" insulting to senior; presumes values pre-exist (not so in early-phase fuzzy sessions); distinguishable from Five Whys by stopping rule (laddering stops at value; Five Whys at root cause).
- **Strongest evidence:** Reynolds & Gutman 1988 means-end-chain theory; McKinsey "so what?" application.
- **Evaluation plan:** Classify corpus acceptance criteria as surface-attribute or value-grounded; hypothesize value-grounded criteria are revised less during implementation.

### spin-sequence — SPIN (Situation / Problem / Implication / Need-payoff)

- **Origin:** `external:consulting-discovery`
- **One-line:** Sequenced question-type protocol — minimize Situation questions (pre-read substitutes), elicit Problem, drill into Implication (the downstream-cascade question rarely asked), elicit Need-payoff in the human's own vocabulary.
- **Dimension values:**
  - Timing of alignment: pre-action, early-phase
  - Decision locus: human-dominant (human does most of the talking)
  - Dialog form: short-volley, sequenced
  - Question style: shifting-type-across-phases
  - Knowledge direction: agent-investigative (heavily)
- **Mechanism:** Top performers ask ~4× more Implication questions than average; Implication questions force respondent to trace causal chains from problem to downstream cost, which is where hidden scope and hidden priority get surfaced.
- **Predicted trade-offs:** Sales-pitch origin contaminates the vocabulary; rigid sequence reads performative; requires agent to pre-read the codebase to substitute for situation questions.
- **Strongest evidence:** Rackham 1988, 35,000+ sales-call analysis; Situation questions had zero measurable correlation with deal success when asked in excess.
- **Evaluation plan:** Classify agent-to-human questions in corpus by SPIN type; hypothesize low-quality sessions over-index situation, high-quality over-index implication.

### interest-probe — Interest-vs-Position Surfacing

- **Origin:** `external:negotiation-mediation`
- **One-line:** Before proposing, agent asks not *what* the human wants but *why* — surfacing the underlying interest beneath the stated position, so later alternatives can serve the same interest.
- **Dimension values:**
  - Timing of alignment: pre-action, pre-proposal
  - Decision locus: human-preempted (during surfacing)
  - Dialog form: short-volley for why-probe, then longer
  - Question style: open-ended, targeted
  - Knowledge direction: agent-investigative
- **Mechanism:** Positions are rigid and few, interests are many and often compatible; staying at position-level deadlocks, going to interest discovers options both can accept.
- **Predicted trade-offs:** "Why do you want that?" asked of a senior about an already-considered decision reads as stalling; some positions don't decompose (naming preferences); probe must be credibly motivated.
- **Strongest evidence:** Fisher/Ury, *Getting to Yes* — principled-negotiation centerpiece; directly applicable when corpus shows agent optimizing against stated position while human's actual interest emerges mid-session.
- **Evaluation plan:** Find corpus sessions where the human amended a position mid-session; was the interest stated anywhere prior? If not, the protocol would have pre-empted the amendment.

### option-invention — Option Invention Before Commitment

- **Origin:** `external:negotiation-mediation`
- **One-line:** Between interest-surfacing and decision, agent presents a 2-3-option slate that could serve the same interest — each with a one-line rationale and one-line cost — and lets the human rank, reject, or add.
- **Dimension values:**
  - Timing of alignment: pre-action, after interest-surfacing, before commitment
  - Decision locus: mixed (agent invents, human selects)
  - Dialog form: long-message (slate) then short-volley (ranking)
  - Question style: forced-choice-with-default or open (add one)
  - Branching: parallel (slate is explicit parallelism)
  - Plan expression form: structured option list with trade-off annotations
- **Mechanism:** Premature convergence on a single option collapses the alternative space and triggers defensiveness/opposition; slate-generation forces genuine alternatives; ranking-and-picking is faster than critique-and-amend.
- **Predicted trade-offs:** Slate inflation (padding with obviously-inferior options); some decisions have a single right answer; needs "reject entire slate" exit.
- **Strongest evidence:** Fisher/Ury third principle; convergent with `reduced-dialectic` from Socratic and `hypothesis-driven-ghost-deck` from consulting.
- **Evaluation plan:** Measure human selection distribution (accept-first / select-non-first / reject-slate); a healthy mix indicates real variance.

### objective-criteria — Objective-Criteria Anchoring

- **Origin:** `external:negotiation-mediation`
- **One-line:** On design disagreements, agent names an external standard — codebase idiom, referenced spec, published convention — and both positions are evaluated against it rather than argued.
- **Dimension values:**
  - Timing of alignment: in-action at disagreement points
  - Decision locus: mixed (criterion selection shared, application quasi-automatic)
  - Plan expression form: structured (decision + criterion + application) — spec-friendly
- **Mechanism:** Lifts negotiation out of will-contest into evidence-contest; asymmetric agent access to codebase-wide conventions vs. human working memory.
- **Predicted trade-offs:** Criterion selection itself can be contested (regresses); taste-driven decisions can't be rescued; forcing it produces spurious citations.
- **Strongest evidence:** Fisher/Ury fourth principle; coding-planning is full of objective-ish criteria unlike many negotiation contexts.
- **Evaluation plan:** Audit decisions with cited criteria vs. bare preference in finished specs; hypothesize criterion-cited decisions are revised less.

### evocation-darncat — MI Evocation / Change-Talk

- **Origin:** `external:therapy-intake`
- **One-line:** Agent elicits the human's own reasons/desire/ability/need/commitment toward a direction *without naming the direction first* — listening via the DARN-CAT register taxonomy and targeting follow-ups at missing registers.
- **Dimension values:**
  - Timing of alignment: pre-action, early-phase
  - Decision locus: human-preempted
  - Dialog form: one-at-a-time open questions
  - Knowledge direction: agent-investigative (maximally)
  - Fuzziness-stability: preserves fuzziness by construction
- **Mechanism:** Speaker's own verbalization of reasons causally precedes change (Magill 2014 meta-analysis); elicitor supplying reasons is inert, speaker articulating them is not.
- **Predicted trade-offs:** Pure evocation without closure is directionless; senior user with implicit preference experiences "why would you want to?" as insulting.
- **Strongest evidence:** Miller & Rollnick (MI 4th ed.); Magill et al. 2014 meta-analysis — MI-consistent skills → change talk r = .26.
- **Evaluation plan:** Code corpus sessions for DARN/CAT register distribution in human utterances; hypothesize absence of D+R early correlates with weaker commitment outcomes.

### epe-information — Elicit-Provide-Elicit (EPE)

- **Origin:** `external:therapy-intake`
- **One-line:** When agent has information to deliver, it first asks what the human already knows, asks permission to share, provides compactly, then asks how it lands — three moves total.
- **Dimension values:**
  - Timing of alignment: per-information-delivery
  - Decision locus: mixed (agent delivers, human rules on landing)
  - Dialog form: structured (three moves), short
  - Review/critique integration: second elicit is live review of provide
- **Mechanism:** Elicit-1 captures human's prior frame before agent's model contaminates; permission-asking is diagnostic (a "no" reveals where attention isn't invested); elicit-2 catches mislanded provision.
- **Predicted trade-offs:** Three moves for every delivery is heavy; elicit-1 becomes Bloom-1 filler if the agent already knows the human's background.
- **Strongest evidence:** Rollnick/Miller/Butler *MI in Health Care* — canonical for brief health-intervention contexts; complements corpus's observed "agent lecturing" pattern.
- **Evaluation plan:** Find agent info-deliveries followed by human corrections; was elicit-1 missing? Would it have shortcut the contradiction?

### readiness-ruler — Readiness / Confidence Ruler

- **Origin:** `external:therapy-intake`
- **One-line:** At decision points with ambiguous commitment, ask a 0-10 rating (or drop the scale), then "what's pulling you toward this already?" (reasons-held) and "what would flip it the other way?" (blockers).
- **Dimension values:**
  - Timing of alignment: at decision point
  - Decision locus: human-dominant
  - Question style: one closed followed by two open
  - Plan expression form: structured (importance, confidence, reasons, blockers) tuple
- **Mechanism:** The follow-ups are the point, not the rating; "why not a lower number?" forces articulation of reasons already held (different output from "what are the reasons?").
- **Predicted trade-offs:** Numerical rulers feel clinical to senior devs; strongest for behavior-change decisions; two-axis is apparatus-heavy.
- **Strongest evidence:** MI brief-intervention literature; the importance/confidence distinction is empirically diagnostic.
- **Evaluation plan:** For decisions with thin spec rationale, retrospectively check whether "why not lower" would have produced missing rationale.

### numbered-question-close — Numbered-Question Close

- **Origin:** `observed` (agent ends turn with numbered list); counter-hypothesis #4 lives separately
- **One-line:** Agent closes its turn with a numbered list of questions; observed to shorten subsequent human turns.
- **Dimension values:**
  - Question style: batched-at-end, enumerated
  - Dialog form: structured close
- **Mechanism:** Numbered structure gives the human a predictable answer-form (per-number response) which compresses the next turn; lowers re-parse cost.
- **Predicted trade-offs:** Enumeration biases question generation toward questions already well-framed; suppresses surfacing of unframed concerns; aviation-CRM literature flags "any questions?" closers as *ineffective* vs. interleaved slot-acknowledgment.
- **Strongest evidence:** Phase 1 lens reports observed it shortens the subsequent human turn; counter-hypothesis #4 contests the mechanism.
- **Evaluation plan:** A/B numbered close vs. single-most-important-question close vs. open-ended close on subsequent human turn length AND on late-session-issue-density.

### implication-question-discipline — Implication-Question Requirement

- **Origin:** `external:consulting-discovery` (SPIN-derived sub-discipline)
- **One-line:** Before proposing any solution, agent must have asked at least one "if we don't address this, what cascades downstream?" question.
- **Dimension values (meta-discipline):**
  - Timing of alignment: pre-proposal
  - Question style: implication-type required
- **Mechanism:** Implication questions are the pattern most absent from the corpus; they force scope and priority to surface through the human's own causal reasoning.
- **Predicted trade-offs:** Rigid "must ask" rule produces ceremonial implication questions; requires the problem to have downstream consequences worth tracing.
- **Strongest evidence:** Rackham's empirical finding that top SPIN performers ask 4× more implication questions.
- **Evaluation plan:** Instrument agent to require one implication question before any solution proposal; measure hidden-scope surfacing rate.

---

## Group 3 — Decision-locus and autonomy

### upfront-decision-partition — Upfront Decision Partition

- **Origin:** `observed` (79a42399 H#1)
- **One-line:** At session open, human declares which decision classes are pre-authorized to the agent ("pick naming, dict vs list, file layout") vs. which require check-in ("anything touching the scheduler, any cross-cutting contract").
- **Dimension values:**
  - Timing of alignment: pre-action
  - Decision locus: pre-authorized by rules
  - Question style: forced into trivial-vs-critical split
  - Autonomy scope: bounded-by-category (declared up front)
- **Mechanism:** Pre-solves the recurring "which decisions should the agent make autonomously?" question, removing per-decision renegotiation overhead.
- **Predicted trade-offs:** Rigidifies the "trivial" category before problem shape is known; requires human to predict decisions they'll encounter; observed to produce zero-cost corrections in the best cases but also freezes understanding early.
- **Strongest evidence:** Observed in 79a42399 (secure-dev) as a successful opener pattern; Phase 1 decision-delegation lens report flags it as a named high-leverage move.
- **Evaluation plan:** Compare upfront-partition sessions vs. `emergent-partition` sessions for reclassification rate — a high reclassification rate in emergent would be the value of the counter-protocol, not a failure.

### emergent-partition — Emergent-Partition Protocol

- **Origin:** `counter-pattern:3`
- **One-line:** No partition is declared up front; agent marks each decision `[D: <question>]` as encountered, and after 3-5 decisions accumulate, proposes a partition that the human can reshuffle; rule is re-derived every N decisions as evidence accumulates.
- **Dimension values:**
  - Timing of alignment: continuous, partition-refreshing
  - Decision locus: mixed, re-tuned mid-session
  - Dialog form: hybrid, with periodic partition micro-artifacts
  - Question style: forced-choice-with-default on partition placement; open on architectural decisions
  - Autonomy scope: bounded-by-category with category itself revisited
  - Plan expression form: structured (decision log with class annotations)
  - Review/critique integration: periodic self-review at each partition refresh
- **Mechanism:** Trivial-vs-architectural distinction is task-relative; up-front partition forces prediction the human otherwise wants to avoid; re-deriving delays specification to the moment evidence is present (analog: incident-command's per-operational-period replanning).
- **Predicted trade-offs:** More interruption than upfront; silent stalls if human inattentive; verbose partition-evolution artifact; an agent bad at spotting-decisions-worth-flagging silently resolves architectural as trivial.
- **Strongest evidence:** Counter-hypothesis #3 in research-statement §6; no direct external analog (nearest is IAP re-issuance) — novel contribution.
- **Evaluation plan:** Count *reclassifications* (decisions marked trivial that were later reclassified architectural); high reclassification = protocol is adapting, low = ceremony. Compare with upfront sessions, which by construction have zero.

### mission-command — Mission Command Doctrine

- **Origin:** `external:military-briefings`, `external:incident-command`
- **One-line:** Doctrinal stance: the principal owns *what* (intent, end state, mission), the subordinate owns *how*; principal absorbs risk of good-faith errors within intent; principal retains authority on scope and end state.
- **Dimension values:**
  - Timing of alignment: continuous, at every decision
  - Decision locus: pre-authorized to subordinate, bounded by intent
  - Autonomy scope: **bounded-by-intent** (new value)
  - Plan expression form: meta (governs artifacts rather than being one)
- **Mechanism:** Over-specification fails under uncertainty; specify intent, grant bounded autonomy, absorb the risk of errors-within-intent; disciplined-initiative-as-duty (subordinate *must* deviate to preserve intent if plan fails).
- **Predicted trade-offs:** Requires the principal to actually absorb error risk or the doctrine collapses; total-autonomy failure ("do what you think is best" is abdication, not mission command); over-narrow intent collapses back to directive command.
- **Strongest evidence:** Auftragstaktik (Moltke, 1857-1888); US Army ADP 6-0; Bungay's management adaptation.
- **Evaluation plan:** Classify corpus human-agent decisions as (a) agent-deferred and user-decided within-intent, (b) agent-decided unilaterally, (c) agent-proposed-and-user-chose; (a) cases within-intent are mission-command-failure events.

### autonomy-scope-grant — Bounded-by-Intent Autonomy Grant

- **Origin:** `external:military-briefings` (sub-component of `mission-command`)
- **One-line:** An explicit one-sentence session-opener clause: "You are pre-authorized to decide X, Y, Z unilaterally within this intent; pause for W, escalate on Z'."
- **Dimension values:**
  - Timing of alignment: pre-action, once per session
  - Decision locus: pre-authorized
  - Autonomy scope: bounded-by-intent (named grant + named bound)
- **Mechanism:** Paired-grant/bound shape — never a single value — prevents abdication while also preventing per-decision renegotiation.
- **Predicted trade-offs:** Short of full mission-command stance if principal still punishes within-intent errors; verbose for casual sessions.
- **Strongest evidence:** Cross-cuts military (ROE as pre-authorized escalation) and medical (I-PASS contingency plans).
- **Evaluation plan:** A/B explicit autonomy clause vs. "dispatch + details are yours" — if behavior is indistinguishable, the doctrine has not been operationalized.

### contingency-preauthorization — Contingency Pre-Authorization

- **Origin:** `external:medical-handoffs` (I-PASS if-then)
- **One-line:** Human states if-then rules in the opener — "if you find X, proceed; if Y, stop and surface; if Z, escalate" — pre-delegating judgment for anticipated novel states.
- **Dimension values:**
  - Timing of alignment: pre-action (authoring) + in-action (firing)
  - Decision locus: pre-authorized conditional actions
  - Autonomy scope: bounded-by-contingency-rule
  - Plan expression form: constraint-list
- **Mechanism:** Catches "novel state I didn't anticipate" errors *for anticipated novel states*; amortizes decision-delegation across many future per-decision moments.
- **Predicted trade-offs:** Requires human to anticipate scenarios (cognitive cost); under-specified contingencies give false pre-auth; needs safety valve ("case not covered by any rule = escalate, not guess-and-extend").
- **Strongest evidence:** I-PASS (Starmer 2014) — highest-outcome handoff bundle includes contingency plans; one of only two cross-variant differentiators of high-outcome mnemonics (with read-back).
- **Evaluation plan:** Measure contingency-fire-rate (how often a stated rule actually matched a case) vs. contingency-ignored-rate (novel cases not covered); well-tuned rules have both > 0.

### incremental-step-autonomy — Incremental-Step Autonomy

- **Origin:** `observed` (present but not well-characterized), `unexplored`
- **One-line:** Agent takes one small step, reports, proposes next small step, vs. unbounded run — step granularity is coarser than micro-step (minutes to hours per step rather than 2-5 min).
- **Dimension values:**
  - Timing of alignment: continuous, per-step
  - Decision locus: interactive at each step
  - Dialog form: short-volley per step
  - Autonomy scope: incremental
- **Mechanism:** Bounds cost of a wrong turn to one step's work; each step's report carries decision-closure forward naturally.
- **Predicted trade-offs:** Consumes more human attention than autonomous stretch; loses cross-step optimization; fragile under human latency.
- **Strongest evidence:** Observed in Phase 1 corpus partially; research-statement §5 flags as unexplored-well.
- **Evaluation plan:** Compare rework-ratio between incremental-step and autonomous-dispatch on the same task type; smaller step size should have lower rework but higher total human attention per minute.

### micro-step-incrementalism — Micro-Step Incrementalism

- **Origin:** `counter-pattern:5`
- **One-line:** Agent proposes a 2-5-minute micro-step, executes it, reports 1-3 sentences, proposes next micro-step; human accepts ("go"), redirects, or stops; *no plan is disclosed up front* — the plan emerges as the ratified step log.
- **Dimension values:**
  - Timing of alignment: continuous, per-micro-step
  - Decision locus: interactive at every step
  - Dialog form: short-volley, tightly rhythmic
  - Question style: forced-choice-with-default ("OK?")
  - Autonomy scope: incremental; one micro-step per ratification
  - Context richness: continuous-building, one-fact-per-step
  - Plan expression form: dialog-log (plan is the trace of accepted steps)
  - Review/critique integration: read-back at each micro-step
- **Mechanism:** If each step is 2-5 min, misalignment costs at most one step; reduces context-load (agent holds only next micro-step); converts trust-question from "did human bless plan" to "did human bless this step."
- **Predicted trade-offs:** Human attention consumed continuously; propose/confirm overhead dominates if steps too small; loses cross-step optimization; fragile under human latency; needs escape hatch to bounded-batch autonomy ("apply next 10 same way") for mechanical refactors.
- **Strongest evidence:** Counter-hypothesis #5; external analog in ping-pong pairing and military fragmentary order.
- **Evaluation plan:** Rework ratio (work later discarded / total work); micro-step should have near-zero, decision-closure protocols higher on tasks where plan turned out wrong. Secondary signal: total-attention-to-completion.

### question-preserving-autonomy — Question-Preserving Autonomy

- **Origin:** `counter-pattern:7`
- **One-line:** Agent runs autonomously but collects ambiguities into a visible queue annotated with (a) what it did instead of asking, (b) rework cost if wrong, (c) confidence; surfaces queue at checkpoints (every N minutes or on confidence drop).
- **Dimension values:**
  - Timing of alignment: hybrid — autonomous + checkpoint
  - Decision locus: pre-authorized-within-envelope
  - Dialog form: long-message at checkpoint; no short turns during autonomy
  - Question style: batched-at-checkpoint, annotated
  - Autonomy scope: bounded-by-question-rework-cost
  - Plan expression form: structured (queue + resolution log)
  - Review/critique integration: self-review on queue-entry composition
- **Mechanism:** "Don't ask questions" doesn't suppress ambiguities, only their visibility; preserving the detection signal (agent notices and queues) while sacrificing interrupt-immediately behavior mirrors military mission-command back-brief checkpoints.
- **Predicted trade-offs:** Queue-entry writing cost eats some autonomy gain; miscalibrated rework-cost is poison (high-cost logged low); long runs accumulate large queues making checkpoint turn expensive; depends on LLM confidence introspection, a known weak spot.
- **Strongest evidence:** Counter-hypothesis #7; direct analog in military mission command + back-brief; contests observed autonomous-dispatch "never ask questions" clause.
- **Evaluation plan:** *Late corrections per session* — "don't ask questions" autonomy should produce high late-correction count on tasks with human-frame ambiguity; question-preserving should push those corrections into the checkpoint turn as queue-entries-resolved.

### authority-transfer-microritual — Authority-Transfer Micro-Ritual

- **Origin:** `external:pilot-controller`
- **One-line:** On items where decision ownership is ambiguous, agent asks "[your-call?]" or declares "[taking-this-one]"; user overrides with "[mine]" or "[yours]"; rare but load-bearing tokens, never routine.
- **Dimension values:**
  - Timing of alignment: at-moment-of-transfer
  - Decision locus: explicit hand-off
  - Dialog form: fixed two-token move (lightweight adaptation of 3-part exchange)
  - Autonomy scope: single-owner at any instant
- **Mechanism:** Decision authority is a single-owner resource; implicit transfer creates race conditions (both act or neither acts); explicit transfer resolves ambiguity in one turn.
- **Predicted trade-offs:** Ritual collapse if asked too often; user will "blanket yours" and stop reading; depends on non-routine use.
- **Strongest evidence:** Aviation three-part handover (TCP-like handshake analog); survives in coding because the ambiguous-ownership failure mode is present (user may expect agent to decide; agent defers back).
- **Evaluation plan:** Corpus scan for decisions stalled or re-litigated due to ambiguous ownership; simulate whether a "[your-call?]" token at that point resolves in one turn.

### pre-action-plan-disclosure — Pre-Action Plan Disclosure

- **Origin:** `observed` (79a42399, f588ff0c A2) — distinct entry from counter-pattern #1 which inverts it
- **One-line:** Agent discloses plan before executing; human corrects; execution proceeds under corrected plan — observed to produce zero-cost corrections.
- **Dimension values:**
  - Timing of alignment: pre-action
  - Decision locus: interactive if corrected
- **Mechanism:** Catches misframings at pre-commit when rework is cheap; converts implicit-model assumptions into observable proposals the human can critique.
- **Predicted trade-offs:** Counter-hypothesis #1 argues pre-disclosure causes premature commitment (both parties interpret subsequent discussion through the disclosed plan's framing, suppressing alternatives).
- **Strongest evidence:** Observed zero-cost corrections in 79a42399 and f588ff0c-A2; contested by counter-hypothesis #1 (`example-led-emergence`).
- **Evaluation plan:** Count turns in which human *corrected the plan's framing* vs. turns in which human *corrected behavior on a specific case*; the ratio discriminates pre-disclosure's surface-drift from example-led's case-drift.

### fixed-token-status-vocabulary — Fixed-Token Status Vocabulary

- **Origin:** `external:pilot-controller`
- **One-line:** Small controlled vocabulary for plan-element states — *proposed*, *locked*, *deferred*, *will-investigate*, *rejected*, *open*, *blocked* — with token-level corrections from the human ("lock 3, defer 5, reject 7").
- **Dimension values (candidate new dimension: Vocabulary discipline):**
  - Plan expression form: structured (status-annotated plan)
- **Mechanism:** Eliminates synonym drift ("OK"/"yes"/"sure"/"will do"); reduces cognitive parsing load; creates verifiable compliance (readback can be checked token-by-token).
- **Predicted trade-offs:** Learning cost; token-list-wrong-case (protocol actively resists right vocabulary); token abuse (locked as rhetorical weapon); agent should probably never issue `locked` — only the user does.
- **Strongest evidence:** ICAO Doc 9432 fixed phraseology (Tenerife 1977 disaster caused by nonstandard "OK"); empirical convergence across aviation, rail, emergency comms.
- **Evaluation plan:** Run one kerf work with status-tokens + user's token-corrections; measure (a) user-correction rate on tokens, (b) user-turn character count vs. matched control, (c) re-litigation rate on `locked` items (should be zero).

### hypothesis-driven-ghost-deck — Hypothesis-Driven Commitment

- **Origin:** `external:consulting-discovery`
- **One-line:** Agent commits to a falsifiable one-sentence hypothesis at session start ("the right approach is X because Y") and produces a ghost-deck skeleton of the final spec, treating the hypothesis as something to *disprove* rather than defend; revises via explicit revision cycles.
- **Dimension values:**
  - Timing of alignment: pre-action, at session start
  - Decision locus: agent-proposes-commits, human-rules-at-review
  - Dialog form: structured-artifact + batched revision cycles
  - Autonomy scope: bounded-by-hypothesis
  - Plan expression form: structured-artifact (heavy)
  - Review/critique integration: falsification *is* the review
- **Mechanism:** Commitment forces prioritization (undirected investigation chases any branch; hypothesis-directed can only spend effort on evidence that could bear on the hypothesis); falsification discipline catches confirmation bias.
- **Predicted trade-offs:** "Honestly held" hypothesis is hard to enforce in LLMs with strong priors (may run falsification motions while internally treating as decided); premature commitment risk; reads as overconfidence to senior user unless framed as "stake-in-ground."
- **Strongest evidence:** McKinsey Day-1-answer and ghost-deck tradition (Rasiel, Minto); kindred to observed `pre-action-plan-disclosure` but with sharper falsification discipline.
- **Evaluation plan:** Classify finalized kerf works as hypothesis-compatible (final framing appeared early and refined) vs. hypothesis-incompatible (framing shifted radically); the ratio tells when this protocol helps vs. anchors.

### example-led-emergence — Example-Led Emergence

- **Origin:** `counter-pattern:1`, convergent with `maieutic-drawout` (both delay agent-introduced abstract frame)
- **One-line:** No upfront plan; agent produces a concretion (trace, IO example) for example A, then example B, etc.; accumulates 3-5 concretions; then writes a *post-hoc* plan draft framed as "my read of what we've converged on" and cites each example as a row in an acceptance table.
- **Dimension values:**
  - Timing of alignment: in-action, example-by-example
  - Decision locus: interactive but narrow-grained (ratify specific behavior on specific cases)
  - Dialog form: short-volley, concrete, trace-heavy
  - Question style: implicit (assumption-stating via concrete example)
  - Autonomy scope: bounded-by-example-set
  - Plan expression form: test-cases / behavior list (case table is the plan)
  - Knowledge direction: bidirectional, agent-investigative (picks probing examples)
  - Review integration: self-review at post-hoc generalize step
- **Mechanism:** Premature commitment effects — articulating abstract plan causes both parties to interpret subsequent discussion through its framing; concrete examples leave the space of generalizations open; disagreement as "that case should do X, not Y" is cheaper to produce than "the framing is wrong."
- **Predicted trade-offs:** Wall-clock grows (many trace sketches, some discarded); combinatorial blow-up if examples aren't spanning; bad fit for globally-coherent structural decisions (schema design); bloats recovery handoff.
- **Strongest evidence:** Counter-hypothesis #1; resembles socratic maieutic draw-out (external convergence) and TDD's red-green-refactor.
- **Evaluation plan:** Framing-corrections vs. case-corrections ratio; framing corrections late in a session signal pre-disclosure locked in prematurely; case corrections signal example-led surfaced framing cheaply.

### forced-choice-with-default — Forced-Choice-with-Default

- **Origin:** `observed` (rare), `unexplored`
- **One-line:** Agent proposes "I'll do X unless you object" rather than "A or B?" — shifts default to action, reduces required human intervention to an override.
- **Dimension values:**
  - Question style: forced-choice-with-default
  - Decision locus: pre-authorized-by-silence
- **Mechanism:** Converts neutral-choice-question into "intervene to stop"; silence carries decision; reduces writing cost for the common case.
- **Predicted trade-offs:** Defaults can be wrong and silently ratified; requires strong calibration on what's safely defaultable; may feel presumptuous if applied to load-bearing decisions.
- **Strongest evidence:** Research-statement §5 notes as rare in observed corpus; external analog in ICAO readback ("silence = confirmation" implicit hearback).
- **Evaluation plan:** Measure override rate — low override means defaults are well-calibrated; high override means agent is making too-bold defaults.

---

## Group 4 — Review and read-back

### read-back-comprehension — Comprehension Read-Back

- **Origin:** `external:medical-handoffs` (I-PASS synthesis-by-receiver; teach-back), `external:pilot-controller` (aviation readback), `external:therapy-intake` (MI reflection), `external:military-briefings` (confirmation brief)
- **One-line:** Before beginning work, agent restates its understanding including explicit inference statements ("I am assuming Z — if wrong, please correct") and invites correction.
- **Dimension values:**
  - Timing of alignment: in-action, interleaved (before autonomous work starts)
  - Dialog form: short-volley
  - Question style: the restatement is itself a question
  - Knowledge direction: bidirectional
  - Review/critique integration: **read-back** (canonical instance, distinct from self-review or peer-review)
- **Mechanism:** Forces explicit encoding by receiver; catches transmission errors (sender said X, receiver heard Y), integration errors (correct words, wrong mental integration), and receiver-assumption errors (filling in unspecified details with wrong defaults).
- **Predicted trade-offs:** Degenerates into parroting without paraphrase discipline; if human ignores the gate, provides zero protection; requires paraphrase-with-inference, not just summary.
- **Strongest evidence:** Medical (I-PASS 30% error reduction) + aviation (Tenerife post-crash mandate) convergence on read-back; teach-back in patient discharge has retention literature showing 96-100% with written+verbal vs. 0-26% verbal-only.
- **Evaluation plan:** Instrument session-opener read-backs; measure fraction that produce a correction (hypothesize >25% in first sessions); watch for ritualization (degrading over time = protocol needs inference-surfacing rule).

### load-bearing-token-readback — Load-Bearing-Token Readback

- **Origin:** `external:pilot-controller`
- **One-line:** After any user turn containing a constraint or decision, agent echoes the load-bearing tokens (named entities, quantifiers, scope boundaries) in a compact one-liner before replying; user's silence = confirmation.
- **Dimension values:**
  - Timing of alignment: continuous per-turn
  - Dialog form: short-volley, structured, fixed-vocabulary
  - Question style: implicit (readback = verification)
  - Autonomy scope: bounded-by-instruction
  - Review/critique integration: inline on every turn
- **Mechanism:** Exploits transcription-error asymmetry — silent mishear commits to wrong interpretation; vocalizing interpretation surfaces the error while correction is cheap; mirror not summary (unlike medical SBAR synthesis).
- **Predicted trade-offs:** Attention-expensive if verbatim; must adapt to "mirror the constraints, not the prose"; requires empirical derivation of which tokens are load-bearing.
- **Strongest evidence:** ICAO mandate, Tenerife 1977 as the incident archive entry; distinct from medical synthesis (per-utterance vs. end-of-bundle).
- **Evaluation plan:** For corpus sessions with misframing, construct the load-bearing-token readback that would have fired; measure (a) would user have corrected, (b) readback character count (target ≤ 15 tokens).

### back-brief-plan-quality — Back-Brief

- **Origin:** `external:military-briefings`
- **One-line:** Before executing, agent presents its proposed plan + *how it serves the intent* + named uncertainties; bidirectional — user may correct plan OR discover intent was ill-stated.
- **Dimension values:**
  - Timing of alignment: post-analysis, pre-execution (mid-action checkpoint)
  - Decision locus: verification, not decision (principal retains final say)
  - Dialog form: structured turn (subordinate speaks, principal corrects)
  - Context richness: rich (contains full plan + reasoning)
  - Plan expression form: verbal narration of structured-artifact
  - Review/critique integration: **this *is* the plan-quality check** (distinct from read-back's comprehension check)
- **Mechanism:** Forced re-encoding exposes comprehension gaps the subordinate would otherwise leave unresolved; plan-against-intent dialog forces mental model into observable form; subordinate often self-corrects mid-brief; commander sometimes discovers intent was ill-stated.
- **Predicted trade-offs:** Ceremonial back-brief (post-hoc rationalization rather than genuine reasoning); back-brief becoming the plan (doubles work without catching new errors).
- **Strongest evidence:** Bungay's management synthesis of Moltke; distinct from medical read-back in the comparison-vs-comprehension axis; direct fit for "agent defers trivial decisions" because it's a standing autonomy grant.
- **Evaluation plan:** Surface-disagreement audit — require agent to name at least one concrete uncertainty in each back-brief; measure how often that surfaced uncertainty was predictive of actual later rework. A back-brief that never disagrees is a failed back-brief.

### teach-back-loop — Closed-Loop Teach-Back

- **Origin:** `external:medical-handoffs` (patient-facing variant)
- **One-line:** Human restates the agent's proposed plan in their own words; correct-until-match cycle; handoff considered incomplete until the restatement is clean.
- **Dimension values:**
  - Timing of alignment: in-action (pre-execution, post-proposal)
  - Review/critique integration: read-back (reverse direction)
- **Mechanism:** Receiver-side verification catches sender's assumptions receiver didn't absorb; inversion of read-back direction.
- **Predicted trade-offs:** Asks senior user to restate an agent proposal — borderline insulting; only earns its place when the proposal is non-trivially complex.
- **Strongest evidence:** Teach-back in patient discharge: "low-cost, low-technology" but habituation-required; improves retention 0-26% → 96-100%.
- **Evaluation plan:** Reserve for proposals on consequential architectural decisions; measure whether human's restatement deviates from agent's proposal (deviation = misalignment caught).

### iap-cadence-artifact — Written Cadence Artifact (Adapted IAP)

- **Origin:** `external:incident-command`
- **One-line:** At each period boundary (every N agent turns, every sub-task completion, or wall-clock), agent produces an adapted IAP — objectives for this period, decision-ownership map, constraints, situation summary, synthesis — regardless of whether anything changed.
- **Dimension values:**
  - Timing of alignment: pre-action (per-period, cadenced)
  - Decision locus: mixed (command sets objectives; sections decide tactics)
  - Dialog form: structured-artifact + briefing (written + verbal)
  - Autonomy scope: bounded-by-objectives
  - Plan expression form: structured-artifact
  - Review/critique integration: cadence-triggered
- **Mechanism:** Cadence-over-trigger; ritual runs regardless of commander's judgment that "nothing changed" because that judgment is unreliable under fatigue/load; objectives-before-tactics catches tactics-then-justify anti-pattern.
- **Predicted trade-offs:** Overhead at solo scale; defining "operational period" for planning is not obvious (candidate mappings: one kerf pass, one major sub-question, N agent-turns — each has different trade-offs).
- **Strongest evidence:** FEMA IAP (Planning P cycle); convergent with SMEAC structure; the re-planning ritual was empirically derived from commanders' failure to know when re-plan was needed under load.
- **Evaluation plan:** Cadence sweep (5 / 10 / 20 turns); measure synthesis quality vs. interruption cost.

### sitrep-at-cadence — Sitrep at Cadence

- **Origin:** `external:incident-command`, `external:military-briefings`
- **One-line:** At regular cadence, agent emits a 5-slot structured update: current situation, actions since last sitrep, actions planned before next, resources required (decisions/context), risks and concerns (watcher channel).
- **Dimension values:**
  - Timing of alignment: cadenced (scheduled, not triggered)
  - Decision locus: none (sitrep informs; decisions separate)
  - Dialog form: structured (short-to-medium)
  - Plan expression form: structured-artifact (short)
- **Mechanism:** "Actions planned before next sitrep" is a pre-announcement of autonomy use — human has a predictable window to redirect without inspecting every turn; "risks/concerns" is the legitimized-intuition channel (watcher analog).
- **Predicted trade-offs:** Cadence overhead can become ritual if human skims; right cadence is context-dependent.
- **Strongest evidence:** ICS 209; Google SRE periodic updates; military tempo element; convergent with I-PASS's severity-tag and CUS's tier-1-concerned.
- **Evaluation plan:** Retrospective sitrep reconstruction on long corpus sessions — would the "risks/concerns" slot at each checkpoint have surfaced known failure points?

### premortem-reviewer — Pre-Mortem Reviewer

- **Origin:** `external:design-review`
- **One-line:** A reviewer sub-agent prompted: "imagine this plan has failed catastrophically — enumerate causes, score severity" — adversarial-by-construction, produces output class orthogonal to the general reviewer.
- **Dimension values:**
  - Timing of alignment: pre-action (late-stage, pre-commit)
  - Decision locus: interactive (team generates, leader decides)
  - Dialog form: short, structured
  - Question style: implicit (reversed — "what failed?")
  - Review/critique integration: named pre-dispatch step
- **Mechanism:** Prospective-hindsight reframing increases ability to identify reasons for future outcomes by ~30% (Mitchell/Russo/Pennington 1989); routes around groupthink, optimism bias, confirmation bias, planning fallacy.
- **Predicted trade-offs:** No "no-arguing" anchor conflict in solo context (author is failure-imaginer); periodic review (step 6 of Klein's original) has no good analog.
- **Strongest evidence:** Klein's HBR article; behavioral-decision literature consistently reports 20-30 min for high signal-to-time ratio; kerf has adopted parallel reviewers but not adversarial-by-construction framing.
- **Evaluation plan:** Run pre-mortem prompt on a completed kerf work; compare output to general reviewer's output; orthogonal-coverage = protocol adds value.

### kerf-parallel-reviewer — Parallel Reviewer (kerf pattern)

- **Origin:** `observed` (kerf adopts this structurally)
- **One-line:** Multiple reviewer sub-agents critique an output in parallel; human reviews synthesis or accepts any that fire.
- **Dimension values:**
  - Review/critique integration: parallel-reviewers (kerf pattern)
- **Mechanism:** Independent perspectives catch non-overlapping issue classes; k-reviewer redundancy beats k-review-depth for discovery.
- **Predicted trade-offs:** Redundancy if role prompts too similar; synthesizer role is hard (disagreement arbitration falls to user); applied to output, not to plans (`unexplored` opportunity).
- **Strongest evidence:** Kerf uses this structurally; external backing from Mob Programming parallel-reviewers and multi-agent architectures research.
- **Evaluation plan:** Plan-with-single-agent vs. plan-with-agent+k-parallel-reviewers (k ∈ {1, 3, 5}); measure issues-found-per-hour-of-human-attention and redundant-issue rate.

### alternatives-considered-section — Mandatory Alternatives-Considered Section

- **Origin:** `external:design-review` (RFC, Rust-RFC, PEP convention)
- **One-line:** Every spec/plan section has a mandatory "Rationale and Alternatives" subsection; kerf reviewer sub-agent blocks advancement on missing-or-weak substance (not just presence).
- **Dimension values:**
  - Plan expression form: structured-artifact (required section)
  - Review/critique integration: hard-gate on section presence
- **Mechanism:** Catches anchoring-on-first-solution; forces enumeration of rejected options with reasons; adversarial-by-construction at section level (author argues against themselves).
- **Predicted trade-offs:** Over-strictness produces pro-forma alternatives ("considered A, rejected because X" where X is obvious); gate enforcement must check for substance.
- **Strongest evidence:** Rust RFC template; PEP's "reject if missing motivation" rule; user has not adopted this in kerf despite adjacent practice.
- **Evaluation plan:** Retroactively check whether each finalized kerf work's decisions had meaningful alternatives documented; where absent, trace downstream rework.

### pyramid-answer-first — Pyramid Principle / Answer-First Output

- **Origin:** `external:consulting-discovery`
- **One-line:** Agent output over 500 chars leads with the answer; supporting reasons below (3-7 arguments, MECE-grouped); evidence below each argument; so-what test at each level.
- **Dimension values (meta-protocol):**
  - Plan expression form: top-down hierarchy
  - Review/critique integration: self-review via so-what test
- **Mechanism:** Decision-makers scan, they don't read; matches output to scan pattern; detail is opt-in.
- **Predicted trade-offs:** Answer-first is wrong when the audience doesn't trust the answer yet; over-pyramiding reads as corporate; doesn't apply to short ping-pong.
- **Strongest evidence:** Minto 1970s; canonical at McKinsey; empirically matches the corpus pattern where user asks "so what's the upshot?" on rambling agent output.
- **Evaluation plan:** Classify long agent responses as answer-first / answer-last / answer-implicit; correlate with subsequent human-turn length.

### so-what-test — So-What Test on Supporting Paragraphs

- **Origin:** `external:consulting-discovery`
- **One-line:** Self-review discipline: on every supporting paragraph, ask "does this contribute a specific insight upward? if deleted, does the parent conclusion become weaker?" — if no, delete.
- **Dimension values:**
  - Review/critique integration: self-review (pre-send)
- **Mechanism:** Agents (and consultants) under pressure produce prose that *describes* without *concluding*; test forces every paragraph to earn its place.
- **Predicted trade-offs:** Over-aggressive application trims useful context; taste-driven "what counts as insight" can be gamed.
- **Strongest evidence:** Minto's pyramid-principle enforcement mechanism; aligned with observed user complaint about agent-output-with-no-upshot.
- **Evaluation plan:** Pre/post: measure character count of agent's responses and whether the human's follow-up asks "so what?" after so-what-test adoption.

### utility-tree — Utility Tree

- **Origin:** `external:design-review` (ATAM component)
- **One-line:** Quality attributes (inspectability, determinism, replay, etc.) decomposed into child nodes with concrete scenarios attached — scenarios act as acceptance tests.
- **Dimension values:**
  - Plan expression form: structured-artifact (tree)
- **Mechanism:** Forces abstract quality goals to land on testable scenarios; decomposition is implicitly an acceptance-test structure.
- **Predicted trade-offs:** Standalone, this is one of the highest-value per-effort adoptions, but can devolve into fake specificity if scenarios aren't genuinely testable.
- **Strongest evidence:** SEI ATAM; maps well onto harmonik's quality-attribute decisions (inspectability, determinism, replay-ability).
- **Evaluation plan:** Retrospectively produce utility trees for 2-3 finalized kerf works; check whether tree surfaces attributes discussed ambiguously or not at all.

### four-category-output — ATAM Risks / Non-Risks / Sensitivities / Tradeoffs

- **Origin:** `external:design-review`
- **One-line:** End each design pass with explicit four-category output: *risks*, *non-risks* (sound decisions surfaced *as findings*), *sensitivity points* (a component materially drives a quality-attribute response), *tradeoff points* (the same decision drives competing attributes in opposite directions).
- **Dimension values:**
  - Review/critique integration: structured output template
- **Mechanism:** Without the four-way split, "risks" swallows everything; non-risks are made explicit (a sound decision is a *finding*); tradeoffs separated from risks (a tradeoff is a choice, not a bug).
- **Predicted trade-offs:** Adds output discipline to every design pass; requires classification skill (overlap between sensitivity and tradeoff).
- **Strongest evidence:** ATAM's SEI technical report; the four categories are empirically discovered — dropping any produces specific analytical blindness.
- **Evaluation plan:** Apply four-category discipline retroactively to a finalized kerf work; outputs surface items (non-risks, tradeoffs) the original elided = value added.

### design-review-categorization — Structured Reviewer-Output Categorization

- **Origin:** `external:design-review`
- **One-line:** Reviewer sub-agent produces output in a categorization template: *concerns / questions / alternative-ideas / positives* (a feedback-matrix quadrant).
- **Dimension values:**
  - Review/critique integration: structured reviewer-output template
- **Mechanism:** Organizing by category catches items that free-form discussion misses; "positives" is especially often omitted without explicit structure.
- **Predicted trade-offs:** Rigid quadrant may distort legitimate fuzziness; agents may pad "positives" to fill the slot.
- **Strongest evidence:** Design-review meeting whiteboard convention; consistently recommended across practitioner guides.
- **Evaluation plan:** Same reviewer agent in free-form vs. categorized form; check whether categorized form surfaces alternatives and positives the free-form omits.

### aar-four-question — After-Action Review (Four-Question, Blameless)

- **Origin:** `external:incident-command`, `external:military-briefings`
- **One-line:** Post-session structured review: (1) what was supposed to happen? (2) what actually happened? (3) why the differences? (4) what to sustain / improve? — blameless, rank-neutral, timed-immediately.
- **Dimension values:**
  - Timing of alignment: post-action
  - Decision locus: collective
  - Dialog form: structured question sequence
  - Plan expression form: structured-artifact (sustains/improves + action items)
- **Mechanism:** Four-question structure anchors review in plan's purpose and forward-looking change rather than event-narrative or blame; "sustain" slot specifically counters negativity bias.
- **Predicted trade-offs:** Immediate-timing is hard at solo scale (lowest user energy at session end); facilitator role is hard to replicate solo (mitigation: reviewer sub-agent facilitates Q3).
- **Strongest evidence:** US Army FM 7-0; widely adopted (NHS, NASA, Google postmortems, PagerDuty); forward-loop from AAR output into next-session opener is the canonical learning cycle.
- **Evaluation plan:** Corpus AAR reconstruction for 10 sessions; forward-loop test (inject prior AAR into opener) vs. baseline — measure framing-correction count (M1) on similar tasks.

### soapy-challenge-response-gate — SOP Challenge-Response Completion Gate

- **Origin:** `external:pilot-controller`
- **One-line:** At a completion gate (e.g., spec ready for implementation), agent runs a spoken-style challenge-response pass over each load-bearing claim: "Constraint: spec is normative. Covered?" / User: "covered, section 3."
- **Dimension values:**
  - Timing of alignment: at decision gates
  - Dialog form: fixed-vocabulary short-volley
  - Review/critique integration: per-item
- **Mechanism:** Silent ticks are compatible with automaticity; spoken verification engages different cognitive resources and is externally observable.
- **Predicted trade-offs:** Attention-expensive; only justifies at gates; heavier than SBAR's inline readback.
- **Strongest evidence:** Aviation SOP callouts; reliably outperforms silent checklist in incident data.
- **Evaluation plan:** Challenge-response completion gate vs. matched silent-review control on the same spec; measure gaps found and per-item cost.

### confirmation-brief — Confirmation Brief

- **Origin:** `external:military-briefings`
- **One-line:** Immediately after receiving intent/task, agent restates — in its own words — the intent and the specific ask before doing any mission-analysis or plan-drafting; purely a comprehension check.
- **Dimension values:**
  - Timing of alignment: immediately post-order (in-action)
  - Dialog form: structured turn
  - Review/critique integration: read-back (comprehension variant)
- **Mechanism:** Catches assimilation errors early, before subordinate wastes time planning against a misheard intent.
- **Predicted trade-offs:** Close cousin of `read-back-comprehension`; separation is by *when* — confirmation brief is at handoff, read-back is at pre-execution; both may be wanted in one session.
- **Strongest evidence:** US Army doctrine; distinct from back-brief in catching comprehension vs. plan-quality errors at different times.
- **Evaluation plan:** Sessions with confirmation brief + back-brief vs. back-brief only; does confirmation brief catch errors the back-brief would have caught later (= redundancy) or errors back-brief would have propagated (= unique value)?

### closing-summary-ritual — Closing-Summary Ritual

- **Origin:** `external:consulting-discovery` (stakeholder interviews), `external:therapy-intake` (MI Summary)
- **One-line:** Before session close, agent summarizes key messages retained and invites validation/correction; 2-3 turns mandatory before wrap.
- **Dimension values:**
  - Timing of alignment: at session close
  - Dialog form: structured (summary + invitation)
  - Plan expression form: prose or structured list (reusable spec material)
  - Review/critique integration: the summary is a review
- **Mechanism:** Catches drift at the last-cheap point; interviewer's model of what was said drifts from stakeholder's silently without ritual.
- **Predicted trade-offs:** Over-long summary is verbose; without content to summarize (early sessions) is filler.
- **Strongest evidence:** Standardized across consulting and MI; convergent with MI's Summary-as-Transition and AAR's "what actually happened."
- **Evaluation plan:** Sessions that end without explicit summary — correlate with subsequent-session restart rate.

### articulate-override-rule — Articulated-Override for Reviewer Disagreement

- **Origin:** `external:design-review`
- **One-line:** When the user disagrees with a reviewer sub-agent comment, the user must record a reason; silent dismissal is disallowed.
- **Dimension values:**
  - Review/critique integration: audit-trail on override
- **Mechanism:** Produces an auditable decision trail; silent dismissal hides rework-prone decisions; forcing articulation slows the path of least resistance.
- **Predicted trade-offs:** Friction on routine dismissals; may encourage rationalized-override text.
- **Strongest evidence:** Pull-request review conventions on contested threads; kerf currently allows silent override.
- **Evaluation plan:** Log reviewer-comment disposition (acted-on / overridden-with-reason / dismissed-silently); check whether silently-dismissed comments later resurface as rework.

### hard-gate-missing-section — Hard Gate on Mandatory Sections

- **Origin:** `external:design-review` (PEP style)
- **One-line:** Reviewer sub-agent blocks pass advancement when a mandatory section (motivation, alternatives, backwards-compatibility) is missing or pro-forma.
- **Dimension values:**
  - Review/critique integration: rejection authority (not just comment authority)
- **Mechanism:** Converts "sections the author was supposed to fill" into "sections the author *will* fill because they cannot proceed otherwise"; gate enforcement must check for substance, not just presence.
- **Predicted trade-offs:** Over-strictness produces tick-the-box filler; changes reviewer's authority model.
- **Strongest evidence:** PEP 1 governance; Python Steering Council practice.
- **Evaluation plan:** Compare kerf works where reviewer feedback was acted on vs. ignored; check whether ignored feedback resurfaced as rework.

---

## Group 5 — Mediator / role-split

### shuttle-diplomacy-reviewer — Shuttle-Diplomacy Reviewer (Mediator Sub-Agent)

- **Origin:** `external:negotiation-mediation`
- **One-line:** A reviewer sub-agent interposed between planning-agent and human; carries summarized positions, softens/reframes, filters questions by stakes, batches parkable topics; produces visible audit log so human can bypass on demand.
- **Dimension values:**
  - Timing of alignment: continuous, interposed between planning-agent and human
  - Decision locus: pre-authorized (standing remit)
  - Dialog form: shape-shifting
  - Branching: reviewer explicitly parks and surfaces
  - Review/critique integration: **mediator** (new dimension value)
- **Mechanism:** Offloads tone-work and paraphrase-accuracy from planning agent; batches questions (drops trivial, consolidates duplicates); third-story-restates planning-agent framing; surfaces parked topics at natural breaks.
- **Predicted trade-offs:** Transparency concern — interposed reviewer hides information from human about what planning agent is doing; three parties more expensive than two; reviewer can inherit both sides' pathologies.
- **Strongest evidence:** Harvard PON literature on shuttle diplomacy; convergent with I-PASS synthesis-by-receiver; operationalizes something kerf's reviewer gestures at but does not structure.
- **Evaluation plan:** Total human input-characters, agent-to-human turn count, session-end spec quality, human-reported load, with/without interposed reviewer.

### single-text-procedure — Single-Text Procedure (Agent-as-Drafter)

- **Origin:** `external:negotiation-mediation`
- **One-line:** A single working document is maintained throughout the session; agent drafts, human critiques; no dueling drafts; each round produces one updated document; ratification is explicit, distinct from ongoing revision.
- **Dimension values:**
  - Timing of alignment: continuous
  - Decision locus: mixed (agent drafts, human critiques, agent revises)
  - Dialog form: hybrid (long-message drafts, short-volley critique)
  - Plan expression form: prose-to-structured (draft is the plan)
  - Review/critique integration: **mediator** (agent plays mediator toward its own drafting)
- **Mechanism:** Critiquing is lower-effort than drafting; critique converges faster than counter-drafting; the artifact at any point is usable (no "plan lives in dialog and must be extracted" problem).
- **Predicted trade-offs:** Concentrates framing power in the agent; critique-only posture means human never *adds* new content (need complementary elicitation); requires agent willingness to substantially revise.
- **Strongest evidence:** Fisher's single-text / one-text procedure, famously used at Camp David; maps cleanly onto spec-first posture.
- **Evaluation plan:** Corpus retrospective — was there a single persistent artifact in successful planning sessions, or did the plan live in dialog? Hypothesize former correlates with higher-quality specs.

### role-split-reviewer-library — Role-Split Reviewer Library

- **Origin:** `external:design-review`, `external:pair-programming` (mob)
- **One-line:** Reviewer sub-agents with specific assigned perspectives run in parallel: devil's advocate, future-maintainer, scope-narrower, simplicity-guardian, locked-decisions-guardian; role prompts are a curated reusable asset.
- **Dimension values:**
  - Review/critique integration: parallel-reviewers with assigned roles
- **Mechanism:** A generic reviewer asked "is this good?" produces generic output; a reviewer prompted specifically "find every reason this is wrong" produces different output (not a subset).
- **Predicted trade-offs:** Redundancy if role prompts too similar; synthesizer role falls to user; role library must be curated.
- **Strongest evidence:** Advocatus diaboli (Catholic Church 11th c.); Smith 2024 multi-agent thesis-antithesis-synthesis architectures; kerf's current generic reviewer is the direct ancestor.
- **Evaluation plan:** Three role-split reviewers on a finalized kerf work vs. generic reviewer; measure orthogonality (complementary vs. redundant items).

### named-role-separation — Named-Role Separation (IC / OL / Scribe / Planning)

- **Origin:** `external:incident-command`
- **One-line:** Explicit role-typing of agent operations: Incident Commander (human — decides), Operations Lead (agent — modifies artifacts), Scribe (agent — maintains session log), Planning Lead (agent — tracks parked questions); commander holds all undelegated roles (solo-collapse rule).
- **Dimension values:**
  - Timing of alignment: continuous
  - Decision locus: role-partitioned pre-authorization
  - Dialog form: role-typed short-volleys
  - Autonomy scope: scoped-by-role (each role has a bounded action space)
- **Mechanism:** Cognitive-mode protection — each role defends one mode (deciding / fixing / recording / communicating) against contamination; when modes contaminate, the mode that loses is usually deciding.
- **Predicted trade-offs:** Role-switching overhead for single agent; ritual output if not gating behavior; solo-collapse discipline is the main risk (user must enforce mode-switching for themselves).
- **Strongest evidence:** FEMA ICS + Google SRE ("IC holds all positions not delegated"); PagerDuty's operational playbook.
- **Evaluation plan:** Classify corpus agent turns as OL-like / Scribe-like / Planning-like; sessions with heavy role-mixing predicted to correlate with drift events.

### can-report-format — CAN-Report Format

- **Origin:** `external:incident-command`
- **One-line:** Structured agent response template for "what have you been doing" moments: *Condition* (current state), *Actions* (what I did), *Needs* (what I require from you).
- **Dimension values:**
  - Dialog form: short-volley, structured
  - Plan expression form: structured-artifact (short)
- **Mechanism:** SBAR-adjacent, optimized for the "give me an update" case; Needs slot is the legitimized request-for-decision.
- **Predicted trade-offs:** Mechanical for trivial status; may produce ritual output.
- **Strongest evidence:** PagerDuty / Google SRE Operations Lead practice.
- **Evaluation plan:** A/B CAN-format vs. free-form status at session midpoints; measure downstream human-correction rate.

### scribe-sub-agent — Scribe Sub-Agent

- **Origin:** `external:incident-command`
- **One-line:** Dedicated sub-agent maintains living session log — decisions, hypotheses, evidence — while the primary agent focuses on Operations; solo-collapse backstop ("IC holds all roles").
- **Dimension values:**
  - Timing of alignment: continuous, parallel
  - Knowledge direction: multi-directional
- **Mechanism:** Separation of Operations-mode from Scribe-mode prevents the "modes contaminate, deciding loses" failure.
- **Predicted trade-offs:** Sub-agent spawn cost; duplicated context; synthesizing scribe log into decision artifact is an additional step.
- **Strongest evidence:** Scribe is a named role in both PagerDuty and Google SRE because combining it with IC or OL has specific failure signatures.
- **Evaluation plan:** Sessions with dedicated scribe vs. primary agent doing both; measure session-log completeness and decision-attribution clarity.

### asymmetric-abstraction-shifting — Asymmetric Abstraction-Shifting

- **Origin:** `external:pair-programming` (strong-style sub-protocol)
- **One-line:** Information-producer dynamically adjusts abstraction level of instructions based on receiver's comprehension signals — starts high, drops on confusion, climbs back as fluency builds.
- **Dimension values:**
  - Timing of alignment: in-action
  - Dialog form: shape-shifting (high-abstraction ↔ low)
- **Mechanism:** Most alignment failures come from mismatched assumed abstraction levels; treating receiver's comprehension as a control signal.
- **Predicted trade-offs:** Requires the agent to ask "should I explain more or continue?" rather than inferring; fails if receiver cues are ambiguous.
- **Strongest evidence:** Falco's strong-style documentation; LLMs are already reasonable at abstraction-matching when prompted.
- **Evaluation plan:** Fixed-abstraction-level plans vs. adaptive abstraction; measure human-reported comprehension and re-explanation-request rate.

### cognitive-tag-team — Cognitive Tag Team

- **Origin:** `external:pair-programming` (Freudenberg empirical)
- **One-line:** Symmetric fluid role-switching — both parties propose, challenge, write artifacts; either may be "driver" or "navigator" mid-exchange without announcement; alignment from shared state not divided responsibilities.
- **Dimension values:**
  - Dialog form: shape-shifting
  - Decision locus: mixed, shifting fluidly
  - Knowledge direction: bidirectional
- **Mechanism:** Empirically, experienced pairs dissolve the driver-navigator scaffold; value comes from symmetric cognitive participation, not specialization.
- **Predicted trade-offs:** Symmetric peer-equity presumption doesn't fit human-agent asymmetry; likely works when human is learning the domain, breaks when human is expert.
- **Strongest evidence:** Plonka & Sharp 2014 + Freudenberg 2013 empirical analyses of pair programming practice.
- **Evaluation plan:** Strict-role (human specs, agent implements) vs. fluid-role on same task; cross with task familiarity to detect the expected interaction.

### classical-driver-navigator — Classical Driver-Navigator

- **Origin:** `external:pair-programming`
- **One-line:** One party (driver) produces artifact tactically; other (navigator) reviews in real time, thinking strategically; roles switch periodically.
- **Dimension values:**
  - Timing of alignment: continuous
  - Decision locus: mixed
  - Review/critique integration: continuous (navigator's primary job)
- **Mechanism:** "Gripping immediacy" (Fowler) — errors caught in-the-moment are cheaper than in async review; narration-forced articulation exposes fuzzy thinking.
- **Predicted trade-offs:** Continuous navigator attention is exactly the attention-cost the track is trying to reduce; naive port makes the problem worse.
- **Strongest evidence:** Cockburn & Williams 2000 — pairs pass 86-94% of test cases vs. 73-78% solo at ~15% time cost.
- **Evaluation plan:** Continuous-navigator posture vs. reviewer-after-dispatch on misaligned-assumption incidents and human attention-seconds-per-incident-caught.

### strong-style-pairing — Strong-Style Pairing (Falco)

- **Origin:** `external:pair-programming`
- **One-line:** "For an idea to go from your head into the computer, it MUST go through someone else's hands" — the idea-holder is forced into the navigator seat; the driver trusts and implements.
- **Dimension values:**
  - Timing of alignment: pre-action (navigator fully articulates each next step)
  - Decision locus: human-dominant (whoever holds the idea navigates)
  - Question style: forced-choice-with-default from driver side; embedded clarification only
  - Autonomy scope: bounded-by-category
  - Review/critique integration: "just do it then refactor" — review deferred
- **Mechanism:** Forces articulation of vague ideas; you cannot communicate an idea you don't have, so vague ideas surface immediately.
- **Predicted trade-offs:** Inverts pain point if agent-as-driver (human articulates every step); interesting if agent-as-navigator (agent produces complete plan-as-instruction-sequence — forcing function against underspecified plans).
- **Strongest evidence:** Falco's documented practice; addresses expert-novice stall and silent-driver patterns.
- **Evaluation plan:** Agent-navigator plan-then-human-executes vs. agent-autonomous implement-and-summarize on rework rate, misalignment incidents, human writing-load.

### ping-pong-alternation — Ping-Pong Pairing (TDD Alternation)

- **Origin:** `external:pair-programming`
- **One-line:** Partners alternate at specifying and implementing: A writes a failing acceptance criterion; B implements minimally; B writes next acceptance criterion; A implements minimally.
- **Dimension values:**
  - Timing of alignment: pre-action (the criterion is pre-action spec)
  - Decision locus: pre-authorized per increment
  - Plan expression form: test-cases (plus code)
  - Review/critique integration: continuous (every alternation is a micro-review)
- **Mechanism:** Minimum-implementation discipline forces acceptance criteria to cover the full behavior space; alternation prevents one-party domination; each failing test is simultaneously a specification request AND a reification.
- **Predicted trade-offs:** For planning (vs. coding), "tests" are fuzzier; translating red-green-refactor to unclear-clear-refine loses the mechanical crispness.
- **Strongest evidence:** Sciamanna's practitioner writings; direct match to `behavior-first-plan` from research-statement §5.
- **Evaluation plan:** Monolithic plan vs. ping-pong (human bullet 1, agent work + bullet 2, human edit bullet 2 + approve work 1, etc.) on rework and plan-completeness-at-dispatch.

### mob-parallel-reviewers — Mob Programming's Parallel-Reviewers

- **Origin:** `external:pair-programming`
- **One-line:** Dispatch plan to several reviewer sub-agents with different priors ("critique for hidden assumptions," "critique for scope creep," "critique for missing edge cases") and return union of concerns.
- **Dimension values:**
  - Review/critique integration: parallel-reviewers
- **Mechanism:** Mob's value is multiple independent reviewing minds; fixed rotation prevents anchoring; in the agent setting, sub-agents are cheap where human mobs are expensive.
- **Predicted trade-offs:** Heavy summarization cost; depends on role prompts being genuinely different.
- **Strongest evidence:** Mob Programming documentation; converges with `role-split-reviewer-library`.
- **Evaluation plan:** plan-with-single-agent vs. plan-with-agent+k-parallel-reviewers (k ∈ {1, 3, 5}); plan-completeness and redundancy rate.

### asynchronous-navigator — Asynchronous-Navigator

- **Origin:** `external:pair-programming` (adapted — doesn't exist in human literature because humans can't skim as fast as agents can emit)
- **One-line:** Agent driver emits structured "I'm about to do X, stop me if wrong" checkpoints; human navigator skims asynchronously at their own pace; gates only on flagged-as-critical items.
- **Dimension values:**
  - Timing of alignment: in-action, agent-paced + human-sampled
  - Review/critique integration: review-as-posture via sub-agent or human-skim
- **Mechanism:** Captures pair-programming's "review as posture" benefit without requiring continuous human attention; relies on agent producing skimmable structured output.
- **Predicted trade-offs:** Human may not read the stream; protocol fails silently if stream quality degrades; only makes sense in an agent setting.
- **Strongest evidence:** Novel adaptation inspired by pair-programming's gripping-immediacy argument; no direct human analog.
- **Evaluation plan:** Measure skim rate (structured checkpoints read / emitted) and catch rate on flagged items.

---

## Group 6 — Plan expression / artifact form

### rfc-full-form — IETF/Rust-style RFC

- **Origin:** `external:design-review`
- **One-line:** Long-form structured artifact with fixed section skeleton: summary, motivation, guide-level explanation, reference-level explanation, drawbacks, rationale and alternatives, prior art, unresolved questions, future possibilities.
- **Dimension values:**
  - Plan expression form: structured-artifact (heavy)
  - Review/critique integration: pre-dispatch multi-reviewer asynchronous
- **Mechanism:** Each section catches a specific error class (drawbacks catches one-sided advocacy, prior art catches reinvention, etc.); adversarial-by-construction at section level.
- **Predicted trade-offs:** Too heavy for small changes; overhead exceeds value on simple tasks; optimizes for cross-team consensus absent in solo case.
- **Strongest evidence:** IETF RFC 2026, Rust RFCs, Python PEPs — cross-community convergence on the skeleton.
- **Evaluation plan:** For a recent kerf work, check whether each section's error class was caught somewhere in plan history; missing sections' classes correlate with leak-through.

### adr-nygard — Architectural Decision Record

- **Origin:** `external:design-review`
- **One-line:** Compact 5-slot artifact — Title / Status / Context / Decision / Consequences — target 1-2 pages; single decision per document.
- **Dimension values:**
  - Plan expression form: structured-artifact (light)
  - Decision locus: agent-autonomous or mixed (records decision already made)
- **Mechanism:** Compactness forces single-point-of-decision framing; "Status: Superseded-by X" chain gives reasoning across time; negative-Consequences slot is mandatory.
- **Predicted trade-offs:** Cannot hold tradeoff-reasoning; coordinated-decisions-across-ADRs can drift.
- **Strongest evidence:** Nygard 2011; harmonik's ten locked-in decisions are already in ADR shape effectively.
- **Evaluation plan:** Rewrite the ten locked-in decisions as ADRs with mandatory Context + Consequences; if they surface forces/consequences the current docs elide, ADR form adds value.

### pep-style-rfc — PEP-Style RFC with Hard Gates

- **Origin:** `external:design-review`
- **One-line:** RFC with mandatory sections (motivation, rationale, specification, backwards compatibility, security implications, reference implementation); missing any is grounds for rejection without substantive review.
- **Dimension values:**
  - Review/critique integration: pre-dispatch, multi-reviewer, with formal gatekeeper
  - Autonomy scope: bounded-by-section-completion
- **Mechanism:** Converts "sections the author was supposed to fill" into "sections the author *will* fill"; gate enforcement must check substance not just presence.
- **Predicted trade-offs:** Over-strictness pushes authors to pro-forma sections; gate requires substance-checker (reviewer sub-agent).
- **Strongest evidence:** PEP 1 + Python Steering Council practice.
- **Evaluation plan:** Kerf works where reviewer feedback was acted on vs. ignored; did ignored feedback resurface as bug/rework?

### issue-tree-diagnostic — Diagnostic Issue Tree

- **Origin:** `external:consulting-discovery`
- **One-line:** Top-down "why" tree: root is the why-question; branches are candidate causal paths; leaves are testable hypotheses; MECE at each level; uneven depth = importance allocation.
- **Dimension values:**
  - Plan expression form: structured-artifact (tree)
  - Decision locus: agent-proposes, human-rules-per-branch
- **Mechanism:** Top-down forces tree to reflect the decision at the top (not a catalog); uneven-depth lets the human read focus from the tree shape.
- **Predicted trade-offs:** Sensitive to root-question phrasing; can become ceremony if problem is well-framed.
- **Strongest evidence:** Chevallier's formalization of McKinsey practice; direct fit for kerf's decompose pass.
- **Evaluation plan:** Apply to finalized kerf works; check whether final spec lands on a branch the tree exposed or outside it.

### issue-tree-solution — Solution Issue Tree

- **Origin:** `external:consulting-discovery`
- **One-line:** Top-down "how" tree: root is the how-question; branches are candidate intervention classes; leaves are concrete actions; MECE at each level.
- **Dimension values:**
  - Plan expression form: structured-artifact (tree)
  - Decision locus: agent-proposes, human-rules-per-branch
- **Mechanism:** Unlike diagnostic-tree, built for action-planning; branches sort candidate approaches that could achieve the goal.
- **Predicted trade-offs:** Same as diagnostic issue tree; plus risk of branches being pseudo-alternatives.
- **Strongest evidence:** McKinsey practice; extends naturally from `issue-tree-diagnostic` when root question flips from why to how.
- **Evaluation plan:** Classify kerf-work root questions as diagnostic vs. solution; check whether research pass stayed in the corresponding mode.

### mece-decomposition — MECE Decomposition

- **Origin:** `external:consulting-discovery`
- **One-line:** Decomposition where sub-categories are mutually exclusive and collectively exhaustive; "Other" bucket > ~10% indicates CE failure.
- **Dimension values:**
  - Plan expression form: structured-artifact
  - Review/critique integration: ME + CE are self-review disciplines
- **Mechanism:** ME catches double-counting; CE catches blind-spots; structural testability beats content testability.
- **Predicted trade-offs:** True MECE is often impossible for messy problems; false-crispness if used strictly; agent anchoring risk when proposed decomposition is presented.
- **Strongest evidence:** Minto's origin at McKinsey late 1960s; the "Other" test is the operational discipline.
- **Evaluation plan:** Reviewer sub-agent asks "name any two categories that overlap, and name any category of cause you haven't listed" on a proposed decomposition; measure catch rate.

### dialog-log-plan — Dialog-Log Plan

- **Origin:** `observed` (primary corpus)
- **One-line:** The plan lives in the chat itself — extracted or summarized after the fact if needed.
- **Dimension values:**
  - Plan expression form: dialog-log
- **Mechanism:** Lightweight; no artifact maintenance overhead; natural for exploratory sessions.
- **Predicted trade-offs:** No single-point-of-truth artifact; resumption-cost is extraction work; silent decay if session chat is lost.
- **Strongest evidence:** Dominant in Phase 1 planning-dialog corpus; the counter-protocol `single-text-procedure` directly challenges this.
- **Evaluation plan:** Compare sessions that produced a persistent artifact vs. pure dialog-log on downstream reference rate and reuse.

### behavior-first-plan — Behavior-First Plan Expression

- **Origin:** `unexplored`, convergent with `ping-pong-alternation`
- **One-line:** Plans written as behavior lists or test cases rather than structural specs — "given X, when Y, then Z" rather than "module A has class B with method C."
- **Dimension values:**
  - Plan expression form: test-cases / behavior list
  - Timing of alignment: pre-action via behavior
- **Mechanism:** Behavior specifications are testable; structural specs are not; disagreements at behavior level are crisp ("this case should produce Y, not Z") where structural disagreements are fuzzy.
- **Predicted trade-offs:** Some cross-cutting concerns (performance envelopes, architectural invariants) don't decompose into behaviors; test-shaped specs may miss non-functional attributes.
- **Strongest evidence:** User flagged as potentially interesting; absent from observed sessions; direct fit for ping-pong and example-led-emergence.
- **Evaluation plan:** Behavior-first plan vs. structural plan on the same task; measure downstream implementation-divergence-from-plan.

### assumption-bundle — Assumption-Bundle Disclosure

- **Origin:** `counter-pattern:2`
- **One-line:** Agent performs a silent read, produces a numbered list of *interdependent* assumptions with explicit dependency arrows ("A1 → A2 unless A3 → approach P"); human edits entries; agent revises by propagating edits through the dependency graph.
- **Dimension values:**
  - Timing of alignment: pre-action, via bundle confirmation
  - Decision locus: interactive, surfaced as graph
  - Dialog form: structured (typed artifact with dependency links)
  - Question style: batched, explicitly connected
  - Autonomy scope: bounded-by-frozen-bundle
  - Context richness: rich brief, agent-built
  - Branching: explicit parallel (bundle entries exist simultaneously)
  - Plan expression form: structured (frozen bundle = spec)
  - Knowledge direction: agent-investigative, human-ratifying
  - Review integration: cascade re-review on any edit
- **Mechanism:** Assumptions co-vary; serial one-at-a-time forces answering in isolation, later questions contradict earlier answers; presenting with coupling visible lets the human pick the highest-leverage anchor and cascade.
- **Predicted trade-offs:** Cognitively heavier per turn; writing-cost on agent side (constructing bundles well is expensive); risk of anchoring on agent's proposed bundle structure; needs escape hatch "reject bundle structure, go linear."
- **Strongest evidence:** Counter-hypothesis #2; convergent external with SBAR Assessment/Recommendation block and consulting's MECE/Issue Tree.
- **Evaluation plan:** *Consistency corrections* — turns where human says "wait, that contradicts X I said earlier"; bundle disclosure should produce more at bundle-v1 and fewer later vs. one-at-a-time flow clustering near implementation.

### knowledge-state-inventory — Knowledge-State Mapping

- **Origin:** `counter-pattern:8`
- **One-line:** Agent opens with a 5-10-item concept inventory ("reservation: in-memory lock; TTL: 60s default renewed by heartbeat; timeout: new concept — the operation timeout on reserve()"); human marks ✓ / ! / ? / +; before any move, agent checks the move against the inventory.
- **Dimension values:**
  - Timing of alignment: continuous, concept-anchored
  - Decision locus: interactive on concepts, mixed on moves
  - Dialog form: shape-shifting (inventory is structured, between-inventory exchanges use whatever form)
  - Autonomy scope: bounded-by-inventory-ratification
  - Plan expression form: structured inventory + free-form moves
  - Review integration: implicit self-review against inventory on every move
- **Mechanism:** Form-vs-content findings may be mistaking covariates for causes; directly tracking concept-alignment state should produce same outcomes regardless of form; analogous to medical read-back checking aligned model of patient state.
- **Predicted trade-offs:** Heavy cognitive cost at session open; weakest for exploratory tasks with emergent concept set; freedom-of-form can devolve if human picks a bad form; inventory maintenance drift silently degrades the protocol.
- **Strongest evidence:** Counter-hypothesis #8; resembles I-PASS "Situation Awareness & Contingencies" layer and pilot-controller pre-phase briefing.
- **Evaluation plan:** *Protocol-form variation across successful sessions* — if form is epiphenomenal, inventory-successful sessions should show wide form variance with uniformly high inventory-ratification rates.

### nvc-four-slot — Compressed NVC

- **Origin:** `external:negotiation-mediation`
- **One-line:** Four-slot single-message structure — observation (factual), implication (design concern raised), need (design-interest behind concern), request (concrete proposed action) — used implicitly to audit agent messages, explicitly only on high-stakes contributions.
- **Dimension values:**
  - Dialog form: structured (four named slots per contribution)
  - Plan expression form: structured
  - Review/critique integration: self-review on agent composition
- **Mechanism:** Communication failures arise from fused layers (observation fused with interpretation, need with request); naming the slots prevents fusion; receiver can respond to specific layer.
- **Predicted trade-offs:** Four slots every message too much (use implicitly); NVC has reputation for sounding stilted; affect-labeling ("feeling" slot) dropped because inappropriate for senior-user context.
- **Strongest evidence:** Rosenberg's NVC; adapted with "implication" replacing "feeling" for coding context.
- **Evaluation plan:** Classify agent messages by which slots present; hypothesize "what to do with this?" messages are missing the request slot, "this feels presumptuous" messages are missing observation.

### coding-opord-4slot — Coding-OPORD 4-Slot

- **Origin:** `external:military-briefings` (compressed SMEAC)
- **One-line:** 4-slot plan artifact: *Context* (codebase/prior-decisions/state), *Outcome* (one-sentence ask), *Approach* (plan), *Prerequisites & Comms* (what must exist + how agent reports mid-work).
- **Dimension values:**
  - Plan expression form: structured-artifact
  - Timing of alignment: pre-action
- **Mechanism:** Compression of SMEAC appropriate to two-party work; preserves Mission/Execution split while collapsing A&L + C&S.
- **Predicted trade-offs:** Template becomes ceremonial if filled mechanically; Prerequisites & Comms slot may be skipped.
- **Strongest evidence:** Compression of doctrinal SMEAC + A/R from SBAR; convergent with engagement-letter's Out-of-Scope + Governance.
- **Evaluation plan:** Corpus audit for which of 4 slots are present/absent in final plans; correlate absent slots with downstream rework.

### frago-modification — Fragmentary Order

- **Origin:** `external:military-briefings`
- **One-line:** Explicit mid-session modification artifact with "amend:" keyword — only the *changed* paragraphs/sub-paragraphs are stated; unchanged elements inherited.
- **Dimension values:**
  - Timing of alignment: mid-action
  - Plan expression form: structured-artifact with inheritance (diff)
- **Mechanism:** Gives modifications a named form; changes don't accumulate silently; diff-protocol for plans.
- **Predicted trade-offs:** FRAGO-overuse risk (plan degenerates to patches); requires discipline to reissue full plan when patches exceed threshold.
- **Strongest evidence:** Doctrinal across US Army/USMC for mid-operation changes.
- **Evaluation plan:** Corpus audit for mid-session changes — communicated as explicit modification, implicit drift, or restated plan? Measure how often implicit drift produces inconsistency.

### position-interest-pair — Position/Interest Pair

- **Origin:** `external:negotiation-mediation`
- **One-line:** Spec artifact paired with each decision: the stated position ("I want the helper to take a config dict") AND the underlying interest ("I want to avoid touching every caller when adding a knob"); agent can re-derive alternatives if the position is later amended.
- **Dimension values:**
  - Plan expression form: structured (position/interest pair)
- **Mechanism:** The interest is the load-bearing part; amendments to position can be generated by re-querying against the interest.
- **Predicted trade-offs:** Not every position has a deeper interest; taste-driven decisions produce spurious interests.
- **Strongest evidence:** Fisher/Ury's first principle; direct fit for kerf decision-log.
- **Evaluation plan:** For finalized specs, audit how often a decision amendment could have been auto-derived from the interest; high rate = protocol adds value.

### test-cases-as-plan — Test-Cases as Plan

- **Origin:** `external:pair-programming` (ping-pong derivation); `external:therapy-intake` (case-table in example-led)
- **One-line:** Plan is expressed as a table of input/output test cases (or acceptance scenarios) rather than prose — structural decisions are left to the implementer as long as the cases pass.
- **Dimension values:**
  - Plan expression form: test-cases
  - Review/critique integration: continuous (each case is a micro-review)
- **Mechanism:** Test-shaped plans are crisp; disagreements at case level are easier to surface than at structural level; implementer has maximum tactical autonomy within case coverage.
- **Predicted trade-offs:** Doesn't cover quality-attribute concerns (performance, maintainability); acceptance-matrix can still be underspecified if cases don't span behavior.
- **Strongest evidence:** TDD literature; BDD frameworks; convergent with behavior-first and example-led.
- **Evaluation plan:** Test-shaped plan vs. prose plan on downstream implementation-divergence.

---

## Group 7 — Meta-protocols / shape-shifting / stance

### protocol-shapeshifting — Protocol Shape-Shifting

- **Origin:** `unexplored`
- **One-line:** Explicitly changing protocol mid-session based on task phase (context-dump for founding, short-volley for refinement, read-back for decision gate); the protocol itself is a first-class design choice.
- **Dimension values (meta):**
  - Dialog form: shape-shifting
  - Protocol chosen per phase
- **Mechanism:** Different phases of work have different optimal interaction shapes; using one protocol throughout undercovers some phases.
- **Predicted trade-offs:** Transitions themselves cost attention; requires both parties to detect phase changes.
- **Strongest evidence:** Research-statement §5 flags as implicitly present but not deliberately chosen.
- **Evaluation plan:** Sessions with explicit phase declarations + phase-specific protocol vs. single-protocol sessions on session-end artifact quality.

### four-process-arc — MI Four-Process Arc

- **Origin:** `external:therapy-intake`
- **One-line:** Session passes through named phases with distinct moves and exit conditions: *engage* (build model), *focus* (set agenda), *evoke* (elicit reasons), *plan* (consolidate); phase labels used internally by agent, not user-facing.
- **Dimension values (meta):**
  - Timing of alignment: continuous; each phase has its own timing
  - Plan expression form: structured (plan comes out of final phase)
  - Review/critique integration: phase-transition is a natural review gate
- **Mechanism:** Same moves mean different things at different phases; the arc is cumulative (cannot productively evoke before engage; cannot productively plan without evocation).
- **Predicted trade-offs:** Overkill for 5-turn sessions; phases feel performative if named out loud; MI's optional plan phase is non-optional in coding.
- **Strongest evidence:** Miller & Rollnick (MI 3rd/4th ed.); the engage-exit-gate ("agent can restate human's picture in human's terms without correction") is probably the single highest-value adaptation.
- **Evaluation plan:** Agent labels phase internally and uses it to gate moves (no proposals during engage; no reflection-only turns during plan); measure session-end artifact quality.

### aporia-graceful-stop — Aporia as Graceful-Stop Signal

- **Origin:** `external:socratic-method`
- **One-line:** A named protocol move: when a question cannot be resolved in the present session, it is flagged, its context recorded, and routed to a later moment (more data, a spike, a sub-agent, tomorrow).
- **Dimension values (meta):**
  - Timing of alignment: at any pass boundary
  - Decision locus: mixed (either party calls aporia)
  - Plan expression form: structured (aporia-artifact: question, ruled-out, unblock, revisit)
- **Mechanism:** Named impasse is distinguished from failure; session advances; future sessions pick up the parked question with context.
- **Predicted trade-offs:** Risk of overuse as escape hatch; honest diagnosis of "unresolvable now" vs. "just tired" is hard.
- **Strongest evidence:** Plato's productive-impasse scholarship; complements `agent-surfaced-parking` from unexplored.
- **Evaluation plan:** Sessions with aporia-move allowed (quota: 1/session) vs. not; measure session-end satisfaction and whether parked questions resurface productively later.

### tactical-pause — Tactical Pause

- **Origin:** `external:incident-command`
- **One-line:** A scripted trigger phrase ("I'm calling tactical pause — we're not aligned") that converts private stall into public deliberation; activity that's obviously required continues; resolution is an explicit statement of what changed.
- **Dimension values (meta):**
  - Timing of alignment: in-action (mid-operation)
  - Decision locus: pre-authorized (either party may call)
  - Dialog form: short trigger + structured deliberation
  - Autonomy scope: pauses autonomy explicitly
- **Mechanism:** Addresses under-load decision debt (decider either acts on too-little or stalls silently); scripted trigger legitimizes saying "I don't know yet" without uncertainty-admission cost.
- **Predicted trade-offs:** Trigger-phrase needs rehearsal; over-use inflates; needs explicit resumption rule or pause becomes stall.
- **Strongest evidence:** Fire-service doctrine; triggers: after 2+ framing corrections in a row, after 20+ turns silent-autonomy, on CUS tier-2, or on human scripted request.
- **Evaluation plan:** Retrospective pause-opportunity identification on corpus drift cases; false-positive rate by trigger; composition test (sitrep cadence + tactical-pause-on-N-corrections vs. either alone).

### narrative-reframe — Narrative-Reframe (Stuck-Story Rewrite)

- **Origin:** `external:negotiation-mediation`
- **One-line:** When a session circles without progress, agent names the "stuck story" ("we've been discussing this as X-vs-Y") and proposes an alternative frame ("could we instead treat it as A-level vs. B-level?"); human accepts, rejects, or counter-proposes.
- **Dimension values:**
  - Timing of alignment: post-stall
  - Decision locus: agent-initiated, interactive
  - Dialog form: long-message then short-volley
  - Branching: meta-level (reframes the handling)
  - Plan expression form: structured (named old-story, named new-story, vocabulary shift)
  - Review/critique integration: mediator
- **Mechanism:** Conflicts harden when parties co-sustain a story about the conflict; exit is finding a different story, not re-labeling the same one.
- **Predicted trade-offs:** Heavy move, dramatizes minor disagreement if premature; reframe must be genuine (agents prone to swap-words-keep-structure).
- **Strongest evidence:** Winslade & Monk narrative mediation; composes with aporia (aporia parks, reframe rewrites).
- **Evaluation plan:** Retrospectively identify un-resolved disagreements on topics that had to be re-discussed; was a reframe opportunity missed?

### going-to-the-balcony — Going-to-the-Balcony (Stall-on-Pushback)

- **Origin:** `external:negotiation-mediation`
- **One-line:** When human pushes back sharply, agent's first move is to visibly pause, summarize what it understood the pushback to be, check whether the summary is right, *before* defending or amending.
- **Dimension values:**
  - Timing of alignment: in-action, triggered by pushback
  - Dialog form: short-volley, one named move
  - Question style: narrow-open check-back
- **Mechanism:** Avoids agent's reflex of immediate capitulation or immediate defense; creates space for read-back + framing-check.
- **Predicted trade-offs:** Reads as evasion if pushback is clear and agent is just deferring; ritual preamble without signal if applied routinely.
- **Strongest evidence:** Ury's *Getting Past No*, first of five breakthrough steps; triggered only on *ambiguous* pushback.
- **Evaluation plan:** Corpus audit of pushback turns; classify agent next-turn as capitulation / defense / balcony-move; correlate with downstream plan quality.

### rolling-with-resistance — Rolling with Resistance

- **Origin:** `external:therapy-intake`, `external:negotiation-mediation`
- **One-line:** On *repeated* pushback on the same point, agent reflects, asks an open question about the contested point, and updates — does not re-explain, re-defend, or counter-argue.
- **Dimension values:**
  - Timing of alignment: reactive (triggered by pushback)
  - Decision locus: human-dominant (agent deliberately not advancing)
  - Dialog form: short-volley, reflection + question
  - Autonomy scope: reduced to zero for the duration
  - Review integration: self-corrective (agent assumes it was the one who erred)
- **Mechanism:** Counter-argument strengthens the position being countered (speaker hears themselves defend and become more committed); MI finding — clients who push back are often reacting to elicitor over-reach, not to the topic.
- **Predicted trade-offs:** Agent may be wrong on content not process (rolling when agent is actually wrong reads as deflecting); sustain-talk-vs-discord distinction is a judgment call.
- **Strongest evidence:** Miller & Rollnick; the trigger is *repeated* pushback (first pushback can be met with update/re-explain).
- **Evaluation plan:** Find sequences with 2+ pushbacks on same point + agent re-explaining; mark as rolling-failure events; A/B on whether switching to reflect-and-ask on second pushback improves third-turn convergence.

### third-story-framing — Third-Story Framing

- **Origin:** `external:negotiation-mediation`
- **One-line:** On disagreement about what's been said/agreed, agent restates the situation as a neutral third-party observer would — no blame, no interpretation — before advocating any resolution.
- **Dimension values:**
  - Timing of alignment: in-action, triggered by disagreement
  - Review/critique integration: mediator (self-applied)
- **Mechanism:** Stripping self-protective and attribution layers exposes structural difference; friction is often in attribution, not substance.
- **Predicted trade-offs:** Badly-executed reads as agent laundering its position through false neutrality; triggered by disagreement, not every exchange.
- **Strongest evidence:** Stone/Patton/Heen, *Difficult Conversations*; LLM can render neutral-perspective without emotional attunement.
- **Evaluation plan:** Pushback turns classified as defensive / clarifying / neutral-restatement; correlate with session-outcome quality.

### positive-no — Positive-No (Yes-No-Yes Assertion)

- **Origin:** `external:negotiation-mediation`
- **One-line:** When agent has a considered reason to reject a human directive, three-part structure: yes to underlying interest, no to specific ask, yes to an alternative that serves the interest.
- **Dimension values:**
  - Timing of alignment: in-action (triggered by disagreement)
  - Decision locus: agent-initiated, interactive
  - Dialog form: structured (three named slots)
  - Plan expression form: structured (Yes-No-Yes triplet)
- **Mechanism:** Prevents pure-rejection (breaks rapport) and over-accommodation (breaks integrity); gives agent a legitimate shape for disagreement.
- **Predicted trade-offs:** Structured triplet feels rehearsed if used often; senior users may not welcome junior-agent asserting design positions at all.
- **Strongest evidence:** Ury's *The Power of a Positive No*.
- **Evaluation plan:** Authorize Positive-No on specific disagreement categories; measure override rate (rare override = assertion was right; common override = assertion discipline too aggressive).

### non-directive-stance — Non-Directive Stance (Rogerian, Micro-Moment)

- **Origin:** `external:therapy-intake`
- **One-line:** A *micro-moment* stance (not a default) — at named moments like early-engage, after repeated pushback, or deep-listen when agent suspects its model is wrong, agent deliberately does not propose or advocate; elsewhere, agent is directional.
- **Dimension values (micro-protocol):**
  - Decision locus: human-dominant by design
  - Knowledge direction: agent-investigative (maximally)
- **Mechanism:** Zero drift from agent-proposal because agent does not propose; preserves human as competence-holder.
- **Predicted trade-offs:** Pure default-non-directiveness is wrong for a coding agent whose job includes proposing; performative if the model has content and withholds it.
- **Strongest evidence:** Rogers' core conditions as a *posture*; MI's "directional but non-coercive" is closer to correct for coding-planning than pure Rogerian.
- **Evaluation plan:** On one engage-phase per session, agent commits to non-directiveness; measure whether the picture that emerges is closer to the human's actual model.

### active-listening-readback — Active-Listening Read-Back

- **Origin:** `external:negotiation-mediation` (Rogers/Gordon)
- **One-line:** On first response to any substantive human message, agent begins with a one-sentence read-back of what it heard (content + concern-level, not affect); self-filters against Gordon's twelve roadblocks.
- **Dimension values (meta):**
  - Review/critique integration: self-review (read-back is agent's own check)
  - Dialog form: prepends brief restatement
- **Mechanism:** Most "I heard you" assertions are false; actually restating surfaces mis-hearing, lets speaker hear their words externally, and lets them clarify.
- **Predicted trade-offs:** Rogerian technique is slow; affect-labeling is patronizing from junior agent; load-bearing only on decision-weighted turns.
- **Strongest evidence:** Rogers 1951; Gordon *Parent Effectiveness Training*; aligns with `read-back-comprehension` but is per-turn not per-session.
- **Evaluation plan:** Classify agent turns by which Gordon roadblocks are committed (advising-before-understood, diagnosing, over-reassuring); correlate with session-quality flags.

### gordon-roadblock-filter — Gordon's Twelve-Roadblock Rejection Filter

- **Origin:** `external:negotiation-mediation`
- **One-line:** Rejection list for agent composition — avoid ordering, warning, moralizing, advising-before-understood, lecturing, judging, praising, name-calling, diagnosing, reassuring, probing-as-avoidance, withdrawing.
- **Dimension values (meta):**
  - Review/critique integration: rejection-list self-filter
- **Mechanism:** Listing the specific pathologies gives the agent a tractable discipline ("avoid X") instead of an intractable one ("be empathic").
- **Predicted trade-offs:** "Probing" is on the list but is also legitimate agent operation — must distinguish probing-as-avoidance from probing-as-inquiry.
- **Strongest evidence:** Gordon's original T.E.T./P.E.T. operationalization; several corpus-observed failure modes directly on the list.
- **Evaluation plan:** Classify a sample of agent turns by which roadblocks they commit; correlate with session-quality flags.

### directional-clean-repetition — Directional Clean-Repetition

- **Origin:** `external:socratic-method` (Grove's Clean Language)
- **One-line:** On decision moments where the human named something, agent preserves the human's verbatim nouns and verbs; paraphrase is reserved for moments where agent is introducing its own idea.
- **Dimension values (meta):**
  - Dialog form: prepends short restatement to agent turns
  - Question style: forces lexical fidelity
  - Knowledge direction: shifts toward agent-investigative / human-dominant
- **Mechanism:** Paraphrase is where drift enters; verbatim preserves speaker's structure; auditable (agent-introduced terms for human-introduced concepts = paraphrase event).
- **Predicted trade-offs:** Verbatim repetition reads stilted; must apply to load-bearing phrases only.
- **Strongest evidence:** Grove's Clean Language (via Sullivan & Rees 2008); directly auditable from transcripts (trace a term in a finished spec back to its first appearance).
- **Evaluation plan:** Trace spec terms back to first appearance; terms first appearing in agent text for human-introduced concepts = paraphrase event; correlate with downstream misalignments.

### affirmations-competence — Competence-Affirmation (non-obsequious)

- **Origin:** `external:therapy-intake` (MI Affirmations, narrow adaptation)
- **One-line:** "You've already established X, so I'll skip re-deriving it" — affirmation-in-function (acknowledges human's work) without affirmation-in-posture (praise). Generic praise ("great work!") is rejected.
- **Dimension values:**
  - Dialog form: one line, declarative
  - Knowledge direction: agent-investigative (agent notices and surfaces)
- **Mechanism:** Specifically naming competence shortcuts re-work (agent does not re-ask a thing the human settled).
- **Predicted trade-offs:** Low priority; generic affirmation is the failure mode the track reacts against.
- **Strongest evidence:** MI OARS (the A); narrow form retains the substantive use-case.
- **Evaluation plan:** Low priority; measure whether agent re-asks things human has settled (pre/post affirmation-discipline).

### graded-assertiveness-pace — PACE Graded-Assertiveness

- **Origin:** `external:pilot-controller`
- **One-line:** Agent-to-human four-level ladder: *Probe* (low-cost question), *Alert* (named concern), *Challenge* (short proposal of alternative), *Emergency* (refuse to proceed until resolved); agent picks the level based on confidence in the concern.
- **Dimension values:**
  - Timing of alignment: in-action
  - Decision locus: pre-authorized (subordinate pre-empowered to escalate)
  - Dialog form: short-volley, sequence of graduated volleys
  - Question style: graduated (open probe → narrowing alert/challenge)
- **Mechanism:** Reduces the marginal cost of each escalation step; converts single high-stakes decision (raise or don't) into series of small decisions (probe → alert → challenge).
- **Predicted trade-offs:** "Emergency" doesn't translate (agent has no override action); probe-inflation risk (probes must be one-liners).
- **Strongest evidence:** Aviation CRM after United 173 (1978); convergent with medical CUS.
- **Evaluation plan:** Corpus replay on sessions where agent did not raise a concern that mattered later — could a Probe have been answered in ≤ 1 line? If yes, the ladder has a plausible silent-deferral → graceful-surfacing path.

### cus-graduated-concern — CUS-Graduated-Concern

- **Origin:** `external:medical-handoffs`
- **One-line:** Three-tier agent-side concern-surfacing: *Concerned* (flagging, may matter), *Uncomfortable* (uncertain — can you confirm?), *Safety issue* (looks like data loss / spec contradiction / locked-decision violation; won't proceed without confirmation).
- **Dimension values:**
  - Timing of alignment: in-action / continuous
  - Decision locus: pre-authorized (agent pre-authorized to escalate)
  - Dialog form: short-volley, structured tiers
- **Mechanism:** Legitimizes low-cost tier-1 move that doesn't require certainty; graduated escalation prevents strong-move inflation; receiver obligation at tier-3.
- **Predicted trade-offs:** Tier inflation (overuse of tier-3 loses meaning); needs threshold-definition; no institutional backing for dispute arbitration in solo context.
- **Strongest evidence:** TeamSTEPPS / AHRQ; convergent with PACE aviation ladder; also convergent with I-PASS watcher.
- **Evaluation plan:** Classify agent outputs as silent-accommodate / tier-1 / tier-2 / tier-3 / other; measure human-response rate by tier and tier-3 false-positive rate.

### agent-surfaced-parking — Agent-Surfaced Parking

- **Origin:** `unexplored`
- **One-line:** Agent tracks parked branches and proactively offers to surface them at appropriate moments ("earlier we parked X — is now a good time to address it?").
- **Dimension values:**
  - Branching: agent-tracked with return-surfacing
  - Timing of alignment: continuous
- **Mechanism:** Prevents parked branches from being lost; converts human's implicit parking into an agent-maintained queue.
- **Predicted trade-offs:** Agent must accurately detect appropriate surfacing moments; over-surfacing becomes nag.
- **Strongest evidence:** Research-statement §5 flags as absent from observed corpus; direct fit for `fixed-token-status-vocabulary`'s `deferred` tag.
- **Evaluation plan:** Measure parked-branches surfaced / parked-branches accumulated per session; high ratio = protocol works.

### screener-gated-branching — Screener-Gated Branching

- **Origin:** `external:therapy-intake` (SCID/MINI adaptation)
- **One-line:** Tiny screener (2-3 yes/no/partially questions) only at transitions into new planning areas with ambiguous scope: "Before we go deep: is authentication in scope? Multi-tenancy? Migration-of-existing-data?" — each yes opens engage/evoke, each no parks.
- **Dimension values:**
  - Timing of alignment: per-topic-branch
  - Decision locus: agent-autonomous (gate logic)
  - Dialog form: scripted, branching
  - Question style: closed (yes/no/partially)
  - Plan expression form: structured matrix (checked/expanded/parked)
- **Mechanism:** Coverage attack on "agent forgot to ask about X"; compact — 5 screeners × 10 sec = 1 min to rule out 5 branches.
- **Predicted trade-offs:** Scripting reads as interrogation; binary screeners collapse legitimate fuzziness; agent's screener list itself reflects agent's priors.
- **Strongest evidence:** SCID-5, M.I.N.I. — screener-gated branching covers 20+ diagnostic categories in 15-30 min.
- **Evaluation plan:** Apply 3-item screener at kerf problem-space pass entry; does it surface categories the actual pass missed?

### handoff-closed-acknowledgment — Handoff Closed-Acknowledgment

- **Origin:** `external:incident-command`, `external:pilot-controller`
- **One-line:** State transition (session-resume, sub-agent spawn, multi-day kerf-work pickup) is not complete until the receiver explicitly acknowledges.
- **Dimension values:**
  - Timing of alignment: at role-transition
  - Dialog form: short-volley with closing acknowledgment
  - Review integration: read-back (acknowledgment is a read-back)
- **Mechanism:** Without closed acknowledgment, outgoing party believes ownership has transferred while incoming party believes not; no one is minding the store; aviation authority-transfer three-part exchange is the structural analog.
- **Predicted trade-offs:** Overhead if over-applied; every sub-agent spawn with full handoff ritual adds latency.
- **Strongest evidence:** Google SRE IC handoff ("You're now the incident commander, okay?" requires firm ack); convergent with aviation and medical handoffs.
- **Evaluation plan:** Sessions picked up after a break with explicit acknowledgment vs. direct-continue — measure first-N-turn correction rate.

### error-catching-posture — Error-Catching as Posture

- **Origin:** `external:pair-programming` (general mechanism)
- **One-line:** Review is a *posture* (continuous, unignorable) not a *phase* (async, deferrable, rubber-stampable); operationalized via sub-agent hooked into primary agent's output stream mid-generation.
- **Dimension values (meta):**
  - Review/critique integration: review-as-posture (distinct from review-as-phase)
- **Mechanism:** Pair programming's defect-reduction delta (86-94% vs. 73-78%) comes from always-present second perspective, not from post-hoc review the control group also had.
- **Predicted trade-offs:** Asymmetric attention cost — humans cannot cheaply maintain review-posture for agent output; sub-agents can.
- **Strongest evidence:** Williams 2000 delta; Fowler's "gripping immediacy."
- **Evaluation plan:** Review-sub-agent hooked inline vs. at end; measure catch rate and human-attention delta.

### pre-reply-self-review — Pre-Reply Self-Review by Agent

- **Origin:** `unexplored`
- **One-line:** Agent drafts response, critiques its own response, revises before sending; observable effects visible only in reply quality; not directly studiable from transcripts.
- **Dimension values (meta):**
  - Review/critique integration: pre-reply self-review
- **Mechanism:** Catches agent's own pathologies (pyramid-violation, so-what-failure, Gordon-roadblock commission) before they hit the user.
- **Predicted trade-offs:** Doubles reasoning cost; may degenerate into self-rubber-stamp; not directly measurable.
- **Strongest evidence:** Research-statement §5 flags; convergent with pyramid-answer-first's so-what-test and pre-send composition checks.
- **Evaluation plan:** Compare agent outputs with explicit pre-reply self-review step vs. without; measure human-correction rate.

### watcher-tier-orientation — Watcher-Tier Session Orientation

- **Origin:** `external:medical-handoffs` (I-PASS severity adaptation)
- **One-line:** One-line opener tag from the human: *routine-shape / watcher / uncertain* — before the agent has processed full context, this gives an orienting signal.
- **Dimension values:**
  - Timing of alignment: pre-action (opener)
  - Dialog form: structured (one tag)
- **Mechanism:** Under cognitive load, receiver may not process full summary; one-tag severity flag survives anyway; "watcher" legitimizes articulated-intuition transfer.
- **Predicted trade-offs:** May collapse to binary (human doesn't use the watcher tier); category may not be load-bearing in solo context.
- **Strongest evidence:** Destino et al. — "watcher" tier independently predicted overnight deterioration; convergent with CUS tier-1 and sitrep risks-and-concerns.
- **Evaluation plan:** Does a solo developer actually use the watcher tier if offered, or does it collapse to binary?

### onethird-twothirds-time — 1/3-2/3 Time Allocation Rule

- **Origin:** `external:military-briefings` (TLP)
- **One-line:** Session time-budget rule — the human takes no more than 1/3 of available time for their own planning; subordinates (the agent) get 2/3 for execution.
- **Dimension values (meta):**
  - Timing of alignment: session-time allocation
- **Mechanism:** Institutionalizes not-hoarding-time at the top; over-planning starves execution.
- **Predicted trade-offs:** Users can spend entire session refining plan and leave agent no time; imposing rule without enforcement may not change behavior.
- **Strongest evidence:** TLP doctrine; directly addresses corpus-observable "planning consumes all the time."
- **Evaluation plan:** Time spent planning vs. executing; correlation with session outcome.

### rehearsal-hierarchy — Graded Rehearsal Hierarchy

- **Origin:** `external:military-briefings`
- **One-line:** Graded menu of pre-commit validation levels matching cost to stakes: map-rehearsal (read plan aloud), sketch (walk through each step), sand-table (file-and-function level sketch), reduced-force (pseudo-code/stubs), full-dress (prototype/spike), combined-arms (full-dress + integration environment).
- **Dimension values:**
  - Timing of alignment: pre-action, post-plan, pre-execution
  - Plan expression form: simulated-executed plan
  - Review/critique integration: plan-critique via step-tracing
- **Mechanism:** Forces plan steps to be concretely traced rather than abstractly understood; graded hierarchy matches validation cost to operation cost.
- **Predicted trade-offs:** Rehearsal-becomes-implementation failure (if rehearsal goes to working prototype, it IS implementation); skipped when plan feels obvious (when rehearsal is most needed).
- **Strongest evidence:** Doctrine across US Army / USMC; rehearsal is the plan-critique mechanism.
- **Evaluation plan:** For sessions where plan produced rework, which rehearsal tier would have caught the rework-cause? Build the distribution.

### controller-orchestration — Controller Orchestration

- **Origin:** `observed` (59-turn b7eca5d2 secure-dev; 3fb3dc80, 69050eec kerf)
- **One-line:** Human writes a directive positioning the agent as a *controller* coordinating other agents (ntm panes); dialog-dense by turn count but directing a running system, not co-designing.
- **Dimension values:**
  - Timing of alignment: post-action (mid-stream)
  - Decision locus: agent-autonomous with mid-stream direction
  - Dialog form: hybrid (opener is structured directive, later short-volley updates)
  - Autonomy scope: unbounded (controller manages workers)
  - Branching: parallel
  - Knowledge direction: human-dominant (directive) then bidirectional (status)
- **Mechanism:** Human directs a running system; controller-agent parcels work to worker-agents; natural match for harmonik's orchestrator subsystem.
- **Predicted trade-offs:** Multi-message structured directive inflates turn count; controller-worker coordination adds its own failure modes.
- **Strongest evidence:** Observed shape in b7eca5d2 (59 turns), 3fb3dc80 (42), 69050eec (24), 7ff17283 (11); directly relevant to harmonik orchestrator design.
- **Evaluation plan:** Track interruption rate when controller drifts from intent vs. when controller holds intent; signal is how often human has to inject mid-stream correction.

### dialogic-context-accretion — Dialogic Context Accretion

- **Origin:** `counter-pattern:6`
- **One-line:** Human provides minimal dispatch; agent does *not* ask for more context upfront but begins work on narrowest-possible interpretation and reports; each report includes a "context I wish I had" section with 1-2 small requests triggered by what the narrow work revealed; repeat until "context I wish I had" is empty for two consecutive turns.
- **Dimension values:**
  - Timing of alignment: continuous, context-triggered
  - Decision locus: interactive, agent-initiated
  - Dialog form: short-volley, agent-led
  - Question style: embedded, 1-2 at a time, context-justified
  - Autonomy scope: bounded-by-current-interpretation
  - Context richness: continuous-building, agent-pulled
  - Branching: implicit (alternate interpretations held until context resolves)
  - Plan expression form: dialog-log + structured final summary
  - Knowledge direction: agent-investigative
  - Review integration: "context I wish I had" is self-review of own model completeness
- **Mechanism:** Three effects — human reveal-during-dialog (notices caveat at min 12 they wouldn't have included at min 0); relevance-filtered context (human only writes what agent asked for); evidenced requests ("I learned Y, I want X") build trust vs. open-ended "tell me about this system."
- **Predicted trade-offs:** Wall-clock grows; first-few-turns low-quality output from narrow interpretation; wastes available rich human model if present; agent blind spots stay blind.
- **Strongest evidence:** Counter-hypothesis #6; external analog in EPE and SPIN sequencing.
- **Evaluation plan:** Compare human writing cost *before first useful agent output* across protocols; context-dump has high-before, low-after; dialogic accretion low-before, distributed-after; hypothesis is sum-of-after-writing is lower.

---

## Group 8 — Methodological borrows (not protocols per se, but reviewer-evaluable)

### incident-driven-catalog — Incident-Driven Change Catalog

- **Origin:** `external:pilot-controller` (methodological)
- **One-line:** Each proposed protocol element must be traceable to a specific observed misalignment event in the corpus; protocols without a named failure mode are deprioritized relative to protocols that address one.
- **Dimension values (methodological):**
  - Review/critique integration: audit of protocol-vs-failure-mode mapping
- **Mechanism:** Aviation communication protocols are each traceable to named accidents (Tenerife, United 173, Avianca 052); template for evidence-driven protocol design.
- **Predicted trade-offs:** Discourages theoretical protocols that address latent failure modes not yet observed in corpus.
- **Strongest evidence:** Aviation's incident archive; directly feeds Task #9 corpus-signal filter.
- **Evaluation plan:** Build observed-misalignment → failure-mode → protocol-response table; entries without a named event get deprioritized.

### summary-as-transition — Summary-as-Transition

- **Origin:** `external:therapy-intake` (MI Summary — the S in OARS)
- **One-line:** At topic/phase transitions, agent produces a compact written restatement of what was established (in the human's own language), invites correction, then advances; applied at phase boundaries and any topic where 3+ exchanges accumulated.
- **Dimension values:**
  - Timing of alignment: at topic/phase transitions
  - Decision locus: human-dominant (ratifies or amends)
  - Dialog form: structured (paragraph + invitation)
  - Plan expression form: prose or structured list (reusable spec material)
  - Review integration: summary IS a review; amendment IS critique response
- **Mechanism:** Summary is a special kind of reflection that collects, links, or transitions; what's included is implicitly load-bearing; without it sessions end without commit-points.
- **Predicted trade-offs:** Verbose for small topic-shifts; early-session summaries are mostly agent's projection.
- **Strongest evidence:** Miller & Rollnick MI OARS; convergent with `closing-summary-ritual` and AAR Q1-Q2.
- **Evaluation plan:** Require summary at every kerf-pass transition; measure whether amendments surface drift that would otherwise have propagated.

---

## Cross-cutting observations

### Dimension coverage

**Well-covered across the catalog:**
- **Review/critique integration.** Multiple distinct values populated: none, self-review, peer-review, parallel-reviewers, read-back (comprehension), back-brief (plan-quality), mediator (interposed), pre-mortem, role-split, cadence-triggered, inline-per-turn. The domain most saturated with distinct primitives.
- **Decision locus** populated across: agent-autonomous, pre-authorized-by-rules, pre-authorized-by-intent, interactive, human-preempted, human-dominant, mixed, role-partitioned, per-item-transferred.
- **Plan expression form.** Populated: prose, dialog-log, structured-artifact (heavy and light), test-cases, behavior-list, tree, matrix, inventory, constraint-list, diff-inheritance, pair.
- **Question style** populated across: one-at-a-time, batched-at-end, batched-with-dependencies, embedded, implicit, open-ended, forced-choice-with-default, narrow-open, restatement-as-question, graduated.

**Sparsely covered:**
- **Branching / topic handling.** Most entries default to "linear" or "implicit"; agent-tracked-with-return-surfacing is populated by only `agent-surfaced-parking` and `aporia-graceful-stop` and a few mediator protocols. Explicit parking lists, parallel-tracks, and fluid branching are under-represented relative to the Phase 1 topic-tree lens report's signal about branch-loss.
- **Timing of alignment — continuous per-turn.** Populated (readback, reflection) but the space between "pre-action" and "continuous" has few inhabitants; the `cadence-triggered` value (from IAP, sitrep, steering-committee) is well-populated but `continuous-within-operational-period` is under-explored.
- **Context richness — new values needed.** The catalog keeps bumping into values the research-statement §3 doesn't list cleanly: *agent-pulled* (dialogic accretion), *inventory-indexed* (knowledge-state mapping), *case-indexed* (example-led), *recovery-handoff* (session resume). Suggests Section 3 should expand this axis.

### Observed + external convergences (strong-triangulation signal)

Where corpus observation converges with external-domain evidence, the protocol is unusually well-supported:

- `pre-action-plan-disclosure` (observed) ≈ `back-brief-plan-quality` (military) ≈ `hypothesis-driven-ghost-deck` (consulting) — pre-commit plan disclosure is a convergent high-confidence primitive.
- `upfront-decision-partition` (observed 79a42399) ≈ `commanders-intent` + `autonomy-scope-grant` (military) ≈ `contingency-preauthorization` (medical) — declaring autonomy scope at opener is a strong-triangulation move.
- `numbered-question-close` (observed) ≈ **anti-pattern** in aviation CRM (interleaved slot-ack beats "any questions?" closer) — observed pattern is *contested* by an external empirical finding, which is itself diagnostic.
- `kerf-parallel-reviewer` (observed) ≈ `mob-parallel-reviewers` (pair-programming) ≈ `role-split-reviewer-library` (design-review) — parallel critique is convergent.
- `context-dump` (observed) ≈ `sbar-opener` / `scqa-opener` (medical/consulting) ≈ `commanders-intent` (military) — the rich-brief opener family is convergent but internally differentiated by *slot discipline*.

### Counter-patterns without external analog (novel candidates)

From the counter-pattern-candidates analysis (§5 of that file):

- `emergent-partition` (counter-pattern #3) — no clean external analog; nearest is incident-command's IAP re-issuance per operational period. Novel contribution.
- `example-led-emergence` (counter-pattern #1) has a strong external analog (`maieutic-drawout` + TDD ping-pong), so not novel.
- `assumption-bundle` (counter-pattern #2) has external analogs (MECE, Issue Tree, SBAR-Assessment/Recommendation), so not novel per se but uniquely *structured as a dependency graph* in a way the external analogs are not.
- `micro-step-incrementalism` (counter-pattern #5) has external analogs (ping-pong, FRAGO).
- `dialogic-context-accretion` (counter-pattern #6) has external analogs (EPE, SPIN).
- `question-preserving-autonomy` (counter-pattern #7) has direct military analog (mission command + back-brief).
- `knowledge-state-inventory` (counter-pattern #8) has external analogs (I-PASS Situation Awareness; pilot-controller pre-phase briefing) but differs in its *form-agnostic* claim (form is epiphenomenal).

### New dimension values surfaced

The catalog surfaces several candidate additions to the research-statement §3 ten-axis schema:

- **Autonomy scope** — `bounded-by-intent` (from mission-command and commander's-intent) is distinct from the existing `bounded-by-constraint-list`, `bounded-by-acceptance-tests`, and `bounded-by-category` values, because the *bound is diagnostic* (end-state conditions) rather than *enumerative* (a list of permitted actions).
- **Review/critique integration** — `read-back` (comprehension), `back-brief` (plan-quality), and `mediator` (interposed) are each distinct from self-review, peer-review, and parallel-reviewers. The evaluation-framework doc already flags read-back as a candidate; this catalog confirms the distinction and adds the others.
- **Vocabulary discipline** (new axis) — `natural-language` / `shared-idiom` / `fixed-token-for-status-markers` / `fully-controlled` (from pilot-controller). Not reducible to any of the ten existing dimensions.
- **Context richness** — new values surfaced: `agent-pulled` (counter-pattern #6), `inventory-indexed` (counter-pattern #8), `case-indexed` (counter-pattern #1), `recovery-handoff` (observed 729dad16).

### Ambiguous / edge-case entries

A handful of entries sit uneasily in the catalog and should be flagged for Step 5 attention:

- `non-directive-stance` is a micro-moment posture, not a session-long protocol; it composes with others but doesn't stand alone.
- `gordon-roadblock-filter` is a rejection-list not a protocol; its role is as a meta-filter composing with other protocols.
- `affirmations-competence` has been narrowed to the point where it may duplicate `directional-clean-repetition`'s preservation-of-prior-work function; kept separate for Step 5 to rule on.
- `cognitive-tag-team` is an emergent-descriptive not a prescriptive protocol; retained because the finding (role-separation may be a scaffold, not the value) is load-bearing.
- `autonomous-dispatch` overlaps with `mission-command` at the level of autonomy scope but differs sharply on alignment timing (none vs. continuous-via-back-brief); kept separate.
- `error-catching-posture` is arguably not a distinct protocol but a *property* of protocols; retained because the property deserves explicit name.
- `incident-driven-catalog` is methodological not protocol; included because Step 5 needs a decision on whether it gets ranked.

### Compositions worth noting

Several protocols are explicitly complementary and should be considered as candidate bundles:

- **Mission-command stack:** `commanders-intent` (session-standing) + `coding-opord-4slot` (per-period) + `sitrep-at-cadence` (mid-period) + `back-brief-plan-quality` (pre-execution) + `aar-four-question` (post-session) is a complete alignment architecture per military-briefings cross-cutting observation.
- **Medical handoff stack:** `ipass-opener` + `read-back-comprehension` + `contingency-preauthorization` + `cus-graduated-concern` is a minimum viable medical adaptation.
- **MI elicitation stack:** `four-process-arc` + `agenda-setting` + `evocation-darncat` + `epe-information` + `summary-as-transition` + `rolling-with-resistance`.
- **Consulting-output stack:** `scqa-opener` + `pyramid-answer-first` + `so-what-test` + `mece-decomposition` + `issue-tree-diagnostic` or `issue-tree-solution` + `alternatives-considered-section`.
- **Counter-pattern stack:** `example-led-emergence` + `dialogic-context-accretion` + `micro-step-incrementalism` is an extreme-incremental alternative to the observed-corpus-dominant pre-disclosure + rich-brief pattern.

Step 5 (reviewer-challenged evaluation) and Step 6 (ranked recommendations) will need to evaluate whether these are stacks-as-bundles or stacks-of-independent-components.

---

## Summary counts

- **Total protocols:** 87.
- **By primary origin:** observed = 11; unexplored = 7; counter-pattern = 8; external = 61 (sum across domains; many entries have multiple origins, so double-counts are possible in this tally).
- **By group:**
  - Opener structures: 14
  - Question discipline: 16
  - Decision-locus and autonomy: 14
  - Review and read-back: 20
  - Mediator / role-split: 13
  - Plan expression / artifact form: 15
  - Meta-protocols / shape-shifting / stance: 26
  - Methodological borrows: 2
  - (Entries counted in one primary group even when cross-cutting.)

The catalog is the input to Step 5 (reviewer-challenged evaluation) and Step 6 (ranked recommendations). It does not rank, does not recommend, and does not favor any origin stream.
