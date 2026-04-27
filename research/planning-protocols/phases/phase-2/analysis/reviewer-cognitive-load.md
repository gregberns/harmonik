# Reviewer Report — Cognitive Load

> Phase 2, Step 5 reviewer artifact. Evaluative frame: **cognitive load**. Scores 87 protocols from `unified-protocol-catalog.md` on working-memory, attention-switching, reasoning overhead, agent-side context load, parallel-track tracking, and dual-process alignment. Does not defer to observed patterns — observed patterns have been iterated for *familiarity*, which is distinct from *low cognitive load*.
>
> Step 4.5 corpus-signal filter was not executed; ratings below are theory-grounded, not corpus-filtered. Protocols whose load prediction is sensitive to corpus evidence are flagged.
>
> Author: cognitive-load reviewer sub-agent, 2026-04-23.

---

## 1. Frame definition

**Cognitive load** is the working-memory, attention, and reasoning demand a protocol places on the two minds running it: the human (here, a senior solo developer) and the agent (a coding LLM with finite context window, finite reasoning budget per turn, and susceptibility to instruction-following priming). A protocol with **low** cognitive load leaves both minds' bandwidth free for the *problem* rather than for the *protocol*; one with **high** load forces the human to track phase markers, pending questions, protocol rules, and parked branches, and/or forces the agent to fill context with ceremony (restatements, checklists, fixed vocabularies) that crowds out load-bearing substantive reasoning.

Cognitive load is **not** the same as writing effort (a 3000-char brief that flows naturally is lower-load than a 200-char answer to an ambiguous multi-part question). It is **not** the same as ergonomic comfort (a protocol can be uncomfortable because it's unfamiliar yet low-load once internalized). It is **not** the same as task-fit. It is the density of non-problem reasoning the protocol requires while it is running. I evaluate six sub-dimensions: (a) human working-memory demand, (b) attention-switching / mode-changing, (c) reasoning-about-protocol overhead, (d) agent-side context-window load, (e) parallel-track bookkeeping, (f) dual-process alignment (does the protocol cooperate with fast intuitive processing or fight it?). Ratings below are my integrated judgment across these; I call out the dominant sub-dimension when load diverges between human and agent.

---

## 2. Per-protocol scoring

Score: **L** = low, **M** = medium, **H** = high. Where human and agent load diverge substantially, I use split notation `H:x / A:y`. The "key driver" column names the dominant sub-dimension.

### Group 1 — Opener structures

| ID | Score | Key driver / rationale |
|---|---|---|
| `sbar-opener` | M | Four named slots is the upper edge of what a senior practitioner can mentally template without a visible form. Low attention-switching (one-shot at opener), but human must remember the slot order and decide *what counts as* each slot — reasoning-about-protocol is real. |
| `ipass-opener` | H | Five slots + mandatory receiver synthesis + contingency-pre-auth = three protocol moves layered. Synthesis gate adds attention-switching; contingency authoring forces scenario imagination. Agent-side: context bloat from the template. |
| `commanders-intent` | L | Three short slots (purpose / key tasks / end state) is near the minimum viable structure; written once, stands for the whole session — no mid-session bookkeeping. Load is in *writing good intent* (a skill) not in *running the protocol*. |
| `smeac-order` | H | Five paragraphs with distinct error-class-targeting is heavyweight; senior user must remember which slot catches which class or fills mechanically. Agent-side: long template crowds context. |
| `scqa-opener` | L | Three-move structure, close to natural expository prose; low reasoning-about-protocol because SCQA matches how many senior practitioners already open arguments. Dual-process-aligned. |
| `engagement-letter-opener` | H | Explicit Out-of-Scope enumeration + governance + change-order process is load-heavy at opener time (human must imagine scope boundaries before problem is fully shaped). Parallel-track tracking of in/out items. |
| `context-dump` | H:M / A:L | Human writes a lot in one burst (high raw effort, but low *protocol* overhead — single flow, no mode-switching). Agent receives structured-ish prose and parses. Splits because load for human is front-loaded and "single-pass," which is easier than interleaved. Rating: M overall but flagged for split. |
| `recovery-handoff` | L | Structured catch-up is a read, not a compose, for the agent; human load was in prior session. Low reasoning-about-protocol once the template is established. |
| `autonomous-dispatch` | L | The lowest-load opener by construction — one line of instruction, no synthesis, no back-brief. Cost moves elsewhere (silent-drift risk) but *cognitive load during the session* is near-floor. Dual-process aligned (just go). |
| `sterile-phase-declaration` | M | Naming the phase is cheap; enforcing the sterile rule means each party tracks "am I drifting?" which is continuous mild background cost. Parallel-track bookkeeping is low (linear during sterile) but mode-transition cost exists at phase boundary. |
| `warno-prep` | M | Two-tier authorization (prep-yes, execute-no) forces human to hold a staged mental model. Not high, but above single-tier. |
| `mett-tc-sweep` | M | Six named slots run as a self-administered checklist. If implicit (internal sweep) it's L for human, M for agent; if surfaced as artifact, M-H on both. Rating: M, flagged for surfacing-sensitivity. |
| `batna-articulation` | L | Two-slot artifact, once per session. Low running-cost. Dual-process-fit is weak (articulating a walk-away is deliberate work), but the work is bounded. |
| `agenda-setting` | L | Pick from a proposed slate — receptive move, not constructive. Low-load by design. |

### Group 2 — Question discipline

| ID | Score | Key driver / rationale |
|---|---|---|
| `elenctic-probe` | H | Human must hold stated thesis + agent-derived consequence + detect contradiction across turns. Demands sustained deliberate reasoning; fights dual-process (forces slow over fast). |
| `maieutic-drawout` | M:H / A:L | Agent-side is light (ask open questions). Human-side is heavy — senior user facing relentless open questions is forced to verbalize latent positions. Reasoning-about-protocol is high ("why isn't the agent contributing?"). |
| `reduced-dialectic` | M | Three named moves (position/counter/synthesis) is manageable. Heavier than maieutic on agent (must generate substantive counter); lighter on human (read triad, rule). |
| `socratic-seminar-turn` | M | Three-phase exchange is above-baseline load but bounded per topic. Closing-ritual artifact is a small additional move. |
| `question-level-targeting` | L:human / M:agent | Meta-protocol invisible to human; agent pre-classifies each question. Agent-side cost is real but contained. Low-load for the human is the point. |
| `five-whys-bounded` | L | Bounded (2-3), patterned, predictable. Dual-process-aligned (each "what earlier made this necessary?" is a single-step query). |
| `laddering-acv` | L | Attribute → Consequence → Value is a fixed 3-step chain; patterned, low running cost. |
| `spin-sequence` | M | Four sequenced types requires agent to hold "which phase are we in" across many turns. Human-side load is low (they just answer). Flagged as H:L / A:M. |
| `interest-probe` | L | Single move ("why do you want that?"). Low-load. Watch for repetition-fatigue. |
| `option-invention` | M | Slate of 2-3 options: human must rank and possibly add. Parallel tracks (the alternative options) held briefly. Moderate. |
| `objective-criteria` | L | Cite an external standard; evaluate against it. Lifts load off working memory by outsourcing to the criterion. Dual-process-aligned when the criterion is obvious. |
| `evocation-darncat` | H:human / M:agent | MI register tracking is heavy for agent (classify every utterance). For senior user, "why would you want to?" probing is cognitively *and* affectively loading. Rating H. |
| `epe-information` | M | Three moves per information-delivery adds protocol-reasoning on every info turn. Moderate, concentrated. |
| `readiness-ruler` | M | Numerical scale + two follow-ups is above-minimum; senior-user fit is poor (apparatus-heavy). |
| `numbered-question-close` | L | Single structural closer; zero reasoning-about-protocol; matches respondent's natural answering mode (per-number). **Classic low-load primitive.** The aviation-CRM critique concerns *quality*, not *load*; flagged as corpus-evidence-sensitive. |
| `implication-question-discipline` | M | "Must ask implication before proposing" is a rule the agent has to track; moderate agent-side, negligible human-side. |

### Group 3 — Decision-locus and autonomy

| ID | Score | Key driver / rationale |
|---|---|---|
| `upfront-decision-partition` | L | One opener move; after it fires, no further bookkeeping (the partition just runs). Classic low-load pre-authorization. |
| `emergent-partition` | H | Continuous classification of each decision, periodic partition refreshes, reclassification events — parallel-track bookkeeping is central. Human must track "what did we put where, and when did that change?" **High by construction.** |
| `mission-command` | L | Intent-based; once intent is stated, most decisions dissolve into "does this serve intent?" One-shot reasoning template. |
| `autonomy-scope-grant` | L | Single-sentence paired-grant/bound; no running cost. |
| `contingency-preauthorization` | M | Authoring contingencies is front-loaded imagination cost. Once written, running cost is low. Moderate at opener, low thereafter. |
| `incremental-step-autonomy` | M | Continuous human attention across many small steps. Each step is low but frequency compounds. |
| `micro-step-incrementalism` | H | 2-5 min steps mean 12-30 mode-switches per hour (propose / accept / continue / redirect). Attention-switching overhead is the load. **High.** |
| `question-preserving-autonomy` | H:agent / L:human | Agent maintains queue with rework-cost + confidence estimates; checkpoint synthesis. Heavy agent context. Human sees one checkpoint turn and is lightly loaded. |
| `authority-transfer-microritual` | L | Two-token move, rare use. Near-zero load. |
| `pre-action-plan-disclosure` | L | Agent discloses plan; human corrects if needed. Single read-and-rule move; dual-process aligned (reading is fast; flagging mis-framing is fast). |
| `fixed-token-status-vocabulary` | M | Small controlled vocabulary — the "small" is doing work; still requires human to adopt and agent to follow. Parallel-track tracking (each plan element has a state) is the load driver. |
| `hypothesis-driven-ghost-deck` | H | Human must maintain an honestly-held falsifiable hypothesis while also considering alternatives — deliberate dual-process fight. Agent-side: ghost deck artifact bloat. |
| `example-led-emergence` | M | Each concrete example is low individual load, but 3-5 accumulated examples + eventual generalize step = parallel-track bookkeeping (what cases have we seen, what's the shape?). |
| `forced-choice-with-default` | L | Silence = consent; converts decision to override. Dual-process aligned (system 1 is fine with "no objection"). |

### Group 4 — Review and read-back

| ID | Score | Key driver / rationale |
|---|---|---|
| `read-back-comprehension` | L:human / M:agent | Agent reads back; human checks. Easy on human (recognition, not recall). Agent must generate faithful paraphrase. |
| `load-bearing-token-readback` | M | Per-turn readback adds cadence overhead; human must tolerate the preamble on every turn. Rhythm-load, not comprehension-load. |
| `back-brief-plan-quality` | M | Bigger than read-back: plan + intent-service + uncertainties. Moderate structured turn. |
| `teach-back-loop` | H | Human restates agent's proposal — asking senior user to restate junior-agent output is insulting *and* cognitively backwards (senior's working memory is fuller). |
| `iap-cadence-artifact` | H | Full written artifact every period regardless of change — pure ceremony-tax. Agent context bloat; human must skim. |
| `sitrep-at-cadence` | M | Shorter than IAP; five slots but compact. Acceptable cadence overhead. |
| `premortem-reviewer` | L | Sub-agent does the work; human reads output. Low load on principal. |
| `kerf-parallel-reviewer` | L:human / M-H:agent(s) | Human reviews synthesized output. Sub-agents absorb the cost. Classic load-distribution win. |
| `alternatives-considered-section` | M | Mandatory section forces enumeration; author must imagine rejected options with substance, not fill-in. |
| `pyramid-answer-first` | L | Answer-first is dual-process-aligned — matches scan pattern. |
| `so-what-test` | L | Self-review discipline; imposed on agent, not human. Low-load for the pair. |
| `utility-tree` | M | Tree construction has parallel-track bookkeeping (quality attributes as branches). Low per-branch but cumulative. |
| `four-category-output` | M | Four named output slots; not high, but imposes classification discipline on every pass close. |
| `design-review-categorization` | M | Similar to above — four quadrants per reviewer output. Moderate. |
| `aar-four-question` | L | Post-session, four questions, bounded. Low running cost. |
| `soapy-challenge-response-gate` | H | Every load-bearing claim goes through a spoken challenge-response — pure attention-switching repetition. |
| `confirmation-brief` | L | Single restatement at handoff. Bounded. |
| `closing-summary-ritual` | L | Session-end summary; low frequency (once). |
| `articulate-override-rule` | M | Forces reason-articulation on every reviewer disagreement. Meta-load on a routine move. |
| `hard-gate-missing-section` | M | Gate enforcement is external-tooling load mostly; human faces it only when blocked. |

### Group 5 — Mediator / role-split

| ID | Score | Key driver / rationale |
|---|---|---|
| `shuttle-diplomacy-reviewer` | M:human / H:agent | Agent holds positions for two parties. Heavy agent context. Human benefits from lower direct load but must trust the mediator. |
| `single-text-procedure` | L | One living document, agent drafts, human critiques. Critique-recognition is lower-load than counter-drafting. Classic low-load structure. |
| `role-split-reviewer-library` | L:human / H:agent(s) | Multiple specialized sub-agents; user sees synthesized output. Load off-loaded. |
| `named-role-separation` | H | Role-typing of every agent operation; solo-collapse rule means *human* must enforce mode-switching on themselves — reasoning-about-protocol is perpetual. |
| `can-report-format` | L | Three-slot status template; compact. |
| `scribe-sub-agent` | L:human / M:agent | Log-maintenance sub-agent absorbs the scribe load. Low for principal. |
| `asymmetric-abstraction-shifting` | M | Agent must continuously monitor comprehension signals and adjust level. Meta-load on every turn. |
| `cognitive-tag-team` | H | Fluid role-switching without announcement means both parties track "who's driving now?" continuously. Parallel-track cost. |
| `classical-driver-navigator` | H | Continuous navigator attention is *exactly* the attention-cost the track is trying to reduce. |
| `strong-style-pairing` | H | Forces full articulation of every next step before action. Heavy dual-process fight (system 2 on everything). |
| `ping-pong-alternation` | M | Alternation of spec/implement per increment — moderate rhythm load, patterned. |
| `mob-parallel-reviewers` | L:human / H:reviewers | Same pattern as kerf-parallel: human reviews union. |
| `asynchronous-navigator` | L:human / M:agent | Agent emits skimmable checkpoints; human skims at their own pace. Novel load distribution. |

### Group 6 — Plan expression / artifact form

| ID | Score | Key driver / rationale |
|---|---|---|
| `rfc-full-form` | H | Nine named sections; heaviest artifact template in the catalog. Context bloat on agent; section-tracking on human. |
| `adr-nygard` | L | Five slots, 1-2 pages. Light artifact, high density. |
| `pep-style-rfc` | H | RFC + hard gates. Same bloat as RFC plus gate-friction. |
| `issue-tree-diagnostic` | M | Tree construction; uneven-depth is good for reading but authoring requires parallel-branch holding. |
| `issue-tree-solution` | M | Same as diagnostic. |
| `mece-decomposition` | M | ME + CE as self-review disciplines on every decomposition; reasoning-about-protocol every time you split a category. |
| `dialog-log-plan` | L | No artifact maintenance; plan lives in chat. **Lowest-load plan form by construction.** (The cost shows up later as extraction work.) |
| `behavior-first-plan` | L | "Given-when-then" matches natural case-by-case reasoning. Dual-process-friendly. |
| `assumption-bundle` | H | Dependency-graph of assumptions with explicit arrows; cascade re-review on any edit. Parallel-track bookkeeping + reasoning-about-graph. **Highest-load artifact form.** |
| `knowledge-state-inventory` | H | Heavy opener concept inventory + per-move check against inventory + inventory maintenance drift risk. |
| `nvc-four-slot` | M | Four slots per message is heavy-if-explicit; protocol says "use implicitly" which drops to L. Rating: M, flagged for surfacing-sensitivity. |
| `coding-opord-4slot` | L | Four slots, compressed SMEAC. Light. |
| `frago-modification` | L | Diff-style mod; inheritance reduces re-statement load. |
| `position-interest-pair` | L | Two-slot per decision. Compact. |
| `test-cases-as-plan` | L | Table of input/output. Natural unit of thought for many problems. |

### Group 7 — Meta-protocols / shape-shifting / stance

| ID | Score | Key driver / rationale |
|---|---|---|
| `protocol-shapeshifting` | H | Both parties must detect phase changes and remember which protocol is active. Pure reasoning-about-protocol. |
| `four-process-arc` | M | Four named phases; internal labels (not user-facing) reduce human load. Agent-side: phase-transition bookkeeping. |
| `aporia-graceful-stop` | L | Named exit move; low running cost. |
| `tactical-pause` | L | Scripted trigger phrase, rare use, bounded deliberation. |
| `narrative-reframe` | M | Reframe construction is heavy but rare. |
| `going-to-the-balcony` | L | Single named move on pushback. Dual-process-aligned (pause-before-respond). |
| `rolling-with-resistance` | M | On repeated pushback, agent must suppress re-explain reflex and switch to reflect-and-ask — mode-switch under pressure. |
| `third-story-framing` | M | Neutral-observer restatement is deliberate work; rare trigger. |
| `positive-no` | M | Three-slot structured disagreement; bounded. |
| `non-directive-stance` | L | Micro-moment, mostly agent-side restraint. |
| `active-listening-readback` | M | Per-turn prepend of restatement; rhythm cost. |
| `gordon-roadblock-filter` | L:human / M:agent | Agent-side rejection list. Invisible to human. |
| `directional-clean-repetition` | L:human / M:agent | Preserve verbatim nouns/verbs — agent-side discipline, low human load. |
| `affirmations-competence` | L | One-line acknowledgment; low overhead. |
| `graded-assertiveness-pace` | M | Four-level ladder; agent picks level based on confidence. Moderate protocol-reasoning on each escalation event (rare). |
| `cus-graduated-concern` | M | Three-tier ladder; similar to PACE. |
| `agent-surfaced-parking` | L:human / M:agent | Agent maintains queue; surfaces at moments. Low-load for human, moderate for agent. |
| `screener-gated-branching` | L | 2-3 yes/no screeners at branch entry. Fast, closed-form. |
| `handoff-closed-acknowledgment` | L | One-turn acknowledgment. Minimal. |
| `error-catching-posture` | M | Continuous review-posture (via sub-agent); cost lands on sub-agent, not human. But cumulative. |
| `pre-reply-self-review` | L:human / M:agent | Invisible to human; doubles agent reasoning. |
| `watcher-tier-orientation` | L | Single opener tag. |
| `onethird-twothirds-time` | L | Time-budget rule; one meta-constraint. |
| `rehearsal-hierarchy` | M | Graded menu — choosing the right tier is reasoning-about-protocol. |
| `controller-orchestration` | H | Human directs a running multi-agent system while monitoring status streams — parallel-track tracking is inherent. |
| `dialogic-context-accretion` | M:human / H:agent | Agent holds "context I wish I had" list across every turn; human just answers narrow requests. Split-load. |

### Group 8 — Methodological borrows

| ID | Score | Key driver / rationale |
|---|---|---|
| `incident-driven-catalog` | N/A | Methodological, not an in-session protocol. Not load-evaluable. |
| `summary-as-transition` | L | Summary at phase boundaries; bounded frequency. |

---

## 3. Top 20 lowest-cognitive-load protocols

Ordered roughly by lowest load first, with rationale for top positioning.

1. **`autonomous-dispatch`** — One-line opener, no synthesis gate, no running protocol overhead. The lowest-load opener by construction; cost shifts to silent-drift risk but the *in-session* load is near-floor. Corpus-sensitive (its viability depends on prior-session spec quality, which I cannot verify without Step 4.5).
2. **`dialog-log-plan`** — No artifact maintenance. Plan lives in the chat. Dual-process-aligned: natural flow, no bookkeeping. The hidden cost is downstream extraction, not in-session load.
3. **`commanders-intent`** — Three slots, once per session, stands for the entire work. After it is stated, most decisions dissolve into "does this serve intent?" — a single-template reasoning pattern that is low-load *because it is template-shaped*.
4. **`numbered-question-close`** — Single structural closer on an agent turn; respondent answers per-number, which matches natural answer-chunking. Zero reasoning-about-protocol. (Quality is contested; load is not.)
5. **`pre-action-plan-disclosure`** — Single read-and-rule move. Recognition not recall. Dual-process aligned.
6. **`forced-choice-with-default`** — Silence = consent. Converts a decision to an override. System 1 is fine with "no objection."
7. **`scqa-opener`** — Three moves that match how many senior practitioners already open arguments. Structure without imposition.
8. **`adr-nygard`** — Five slots, compact. Single decision per document. Light enough to run, dense enough to be useful.
9. **`autonomy-scope-grant`** — One sentence paired grant + bound. Running cost: zero.
10. **`single-text-procedure`** — Critiquing is lower-load than counter-drafting. Classic load-distribution structure.
11. **`agenda-setting`** — Pick from a slate. Receptive move. No construction.
12. **`five-whys-bounded`** — Patterned, bounded (2-3), predictable.
13. **`laddering-acv`** — Fixed 3-step chain. Predictable.
14. **`test-cases-as-plan`** — Natural unit of thought for many problems. Case-by-case reasoning is dual-process-friendly.
15. **`batna-articulation`** — Two-slot, once per session.
16. **`recovery-handoff`** — Agent reads a structured catch-up; human load was paid in a prior session.
17. **`aporia-graceful-stop`** — Named exit. Low running cost; high legitimization value.
18. **`closing-summary-ritual`** — Once per session, bounded.
19. **`confirmation-brief`** — Single restatement at handoff.
20. **`tactical-pause`** / **`handoff-closed-acknowledgment`** (tied) — Scripted trigger / one-turn acknowledgment. Near-zero.

Honorable low-load mentions kept off the top-20 because their load is concentrated at one moment (high-intensity briefly, zero otherwise): `interest-probe`, `objective-criteria`, `premortem-reviewer`, `aar-four-question`, `screener-gated-branching`, `pyramid-answer-first`, `so-what-test`, `behavior-first-plan`, `position-interest-pair`, `coding-opord-4slot`.

**Theme:** low-load protocols share two properties: (a) bounded frequency (single-shot, or rare-trigger), and (b) recognition-dominant rather than construction-dominant human moves. They do *not* demand protocol-reasoning on every turn.

---

## 4. Bottom 10 highest-cognitive-load protocols

1. **`assumption-bundle`** — A dependency graph of assumptions with explicit arrows and cascade re-review on edits. Parallel-track bookkeeping on every edit, plus reasoning-about-graph structure. The highest-load artifact in the catalog.
2. **`knowledge-state-inventory`** — Opener concept inventory of 5-10 items, per-move check against inventory, inventory maintenance drift as a protocol-failure mode. Continuous reasoning-about-inventory.
3. **`emergent-partition`** — Continuous classification of every decision + periodic partition refresh + reclassification events. Human tracks what went where and when the classification changed. Parallel-track bookkeeping is *the protocol*.
4. **`micro-step-incrementalism`** — 12-30 propose/accept mode-switches per hour. Attention-switching is the dominant cost. Fights dual-process (forces system 2 on every micro-increment).
5. **`named-role-separation`** (with solo-collapse) — Role-typing of every agent operation, and the *human* enforces mode-switching on themselves. Perpetual reasoning-about-protocol.
6. **`protocol-shapeshifting`** — Both parties detect phase changes and remember which protocol is active. Meta-load on everything.
7. **`classical-driver-navigator`** — Continuous navigator attention is the specific cost the track is trying to reduce; naive port makes the problem worse.
8. **`strong-style-pairing`** — Forces articulation of every next step before action. Whole-session dual-process fight.
9. **`rfc-full-form`** — Nine sections; the heaviest artifact template. Section-tracking + context bloat. Overhead dominates on anything short of a major subsystem design.
10. **`controller-orchestration`** — Human directs a running multi-agent system while monitoring multiple status streams. Parallel-track tracking is inherent to the role; this is high-load *even when it's working*.

Other high-load protocols just off the bottom-10: `ipass-opener`, `smeac-order`, `engagement-letter-opener`, `pep-style-rfc`, `teach-back-loop`, `iap-cadence-artifact`, `cognitive-tag-team`, `soapy-challenge-response-gate`, `elenctic-probe`, `hypothesis-driven-ghost-deck`, `evocation-darncat`.

**Theme:** high-load protocols share (a) continuous bookkeeping of mutable state (partition, inventory, role-assignment, phase-identity), (b) parallel-track tracking (multiple branches, multiple positions, multiple streams), and/or (c) dual-process fights (forcing deliberate restatement / articulation where fast recognition would do).

---

## 5. Human-vs-agent load asymmetry flags

Protocols whose load falls disproportionately on one side deserve flagging because their recommendation depends on *which side's bandwidth is the binding constraint*.

### Low human / high agent

- `question-level-targeting` — human sees cleaner questions; agent does classification work.
- `question-preserving-autonomy` — human sees one checkpoint; agent maintains annotated queue.
- `shuttle-diplomacy-reviewer` — human talks to mediator; agent absorbs positions-for-two.
- `role-split-reviewer-library`, `mob-parallel-reviewers`, `kerf-parallel-reviewer` — human synthesizes; sub-agents do the critiquing.
- `scribe-sub-agent` — log maintenance off-loaded entirely.
- `asymmetric-abstraction-shifting` — agent monitors and adjusts; human just converses.
- `gordon-roadblock-filter`, `directional-clean-repetition` — agent-side self-filters; invisible to human.
- `agent-surfaced-parking` — agent maintains parked-queue; human receives surfacing prompts.
- `pre-reply-self-review` — agent doubles its reasoning; human sees nothing.
- `dialogic-context-accretion` — agent holds "context I wish I had"; human just answers narrow requests.

These are **good choices when the human is the binding constraint** (solo developer, limited attention windows). They load the agent, which has cheap re-invocation and parallelism; that is the correct load distribution in the solo-senior setting. Note: "cheap" is relative — agent context is still finite, and these protocols consume context that could otherwise hold substantive reasoning. The asymmetry is a feature only while the agent's context budget is not saturated.

### High human / low agent

- `maieutic-drawout` — agent just asks open questions; human verbalizes latent positions repeatedly.
- `evocation-darncat` — agent elicits; senior user feels interrogated.
- `teach-back-loop` — senior user restates junior agent's proposal (cognitively backwards).
- `strong-style-pairing` — human articulates every step in full detail.

**Flag:** these are reverse-loaded for the target user. They ask the senior practitioner to produce, on demand, artifacts that the agent should be producing. They may suit a learning user but not a senior solo user. Load-recommendation: **reject for this user unless used as rare micro-moves**.

### High on both sides

- `assumption-bundle`, `knowledge-state-inventory`, `emergent-partition`, `rfc-full-form`, `ipass-opener`, `iap-cadence-artifact`.

These are not good choices unless the problem is genuinely at the scale where the bookkeeping pays for itself (multi-month cross-team subsystem design). For most planning sessions they are over-spec'd.

---

## 6. Composition analysis

### Additive-stacker compositions (each adds load, small increments, total remains manageable)

- **`commanders-intent` + `autonomy-scope-grant` + `pre-action-plan-disclosure`** — all three are recognition-dominant and one-shot. Stack: L + L + L ≈ L-to-M. The canonical low-load mission-command-adjacent opener.
- **`scqa-opener` + `pyramid-answer-first` + `so-what-test`** — all dual-process-aligned exposition disciplines. Stack: L + L + L ≈ L. Consulting-output stack composes cleanly.
- **`adr-nygard` + `alternatives-considered-section`** — two structured bounded artifacts. Stack: L + M ≈ M.
- **`numbered-question-close` + `forced-choice-with-default`** — two question-discipline primitives. Stack: L + L ≈ L.
- **`read-back-comprehension` + `aar-four-question`** — bookend moves (session open / session close). Stack: L + L ≈ L.
- **`closing-summary-ritual` + `recovery-handoff` (next session)** — sequential across sessions; stack is serial not concurrent. Stack: L.

### Multiplicatively stacking compositions (each amplifies prior, quickly overloading)

- **Mission-command stack as catalog describes:** `commanders-intent` + `coding-opord-4slot` + `sitrep-at-cadence` + `back-brief-plan-quality` + `aar-four-question`. Individually moderate; combined, the cadenced artifacts (`sitrep`, `back-brief`) intersect with the per-period artifact (`coding-opord`) and produce **continuous artifact-production overhead** dominating session time. Stack: L + L + M + M + L, but the interaction between cadence-triggered and per-period produces a M→H jump.
- **Medical handoff stack:** `ipass-opener` + `read-back-comprehension` + `contingency-preauthorization` + `cus-graduated-concern`. The opener is already H; adding read-back (L) doesn't help because the opener's synthesis gate has *already* absorbed the comprehension check. Redundancy tips to overload.
- **Counter-pattern stack:** `example-led-emergence` + `dialogic-context-accretion` + `micro-step-incrementalism`. All three demand continuous engagement at different granularities. Stack: M + M + H, but they interact — `micro-step` fires on every example and on every dialogic context-request, producing frequency-compounding. Pure multiplicative: projected H→very high.
- **MI elicitation stack:** `four-process-arc` + `agenda-setting` + `evocation-darncat` + `epe-information` + `summary-as-transition` + `rolling-with-resistance`. Phase tracking + register tracking + per-info-turn three-move discipline = three orthogonal bookkeeping streams. Multiplicative.
- **Any stack containing `assumption-bundle` or `knowledge-state-inventory`** — these saturate agent context and are inherently multiplicative with anything that adds additional parallel-tracking (`emergent-partition`, `named-role-separation`, `controller-orchestration`).

**Rule of thumb from cognitive-load theory:** stacks remain additive when each element is *one-shot or rare-trigger* and *recognition-dominant*; they go multiplicative the moment two or more elements require *continuous* bookkeeping or *cross-referencing*.

---

## 7. Counterfactual: working-memory constraint relaxed

If the user had protocol-state displayed externally at all times — a persistent panel showing active phase, parked branches, partition rules, assumption bundle, etc. — would my rankings change?

**Yes, substantially — for a specific subset.** The high-load protocols I flagged for *continuous bookkeeping* (emergent-partition, knowledge-state-inventory, assumption-bundle, named-role-separation, controller-orchestration, protocol-shapeshifting) lose most of their human-side load when state is externalized. They remain agent-side-loaded (the agent still has to maintain the state), but the human cost drops from "hold in working memory continuously" to "glance at the panel when relevant." Under this counterfactual:

- `emergent-partition` drops H → M (the classification is visible; reclassification events are surfaced).
- `knowledge-state-inventory` drops H → M (the inventory is the panel; per-move check becomes a glance).
- `assumption-bundle` drops H → M (the dependency graph is rendered; cascade propagation is visible).
- `named-role-separation` drops H → M (current role is tagged).
- `protocol-shapeshifting` drops H → L (current protocol is displayed).
- `controller-orchestration` drops H → M (status streams are in panes; human dispatches, doesn't memorize).

**No change** for protocols whose load is in *deliberate construction* (strong-style-pairing, teach-back-loop, maieutic-drawout, evocation-darncat, classical-driver-navigator) — these don't tax working memory, they tax the articulation / restatement engine, which no panel helps with. No change for protocols whose load is *attention-switching frequency* (micro-step-incrementalism, rolling-with-resistance) — panel doesn't reduce mode-switches.

**No change for the low-load list** — it is low-load for reasons other than working-memory (boundedness, recognition-dominance, dual-process alignment), so externalization doesn't shift them.

**Implication:** the dominant cognitive-load constraint for the target user is probably *not* working-memory alone — many of the high-load protocols fight other constraints (attention-switching frequency, articulation demand, dual-process compatibility). External state display is a large win for bookkeeping-heavy protocols but does not rescue articulation-heavy or frequency-heavy ones. A recommendation that leans on "we'll just build a status panel and tolerate complexity" fixes one class of high-load protocol and leaves the other two classes still high-load.

---

## 8. Corpus-evidence flags

Items in my ranking whose load-prediction depends on corpus evidence I cannot verify without Step 4.5:

- `numbered-question-close` — rated L on load, but the aviation-CRM literature contests its *quality*. Cognitive-load rating stands; quality caveat acknowledged.
- `autonomous-dispatch` — rated L on in-session load, but the reliance on "prior session produced a complete spec" means load is displaced, not removed. Corpus filter would reveal how often the pre-built spec is actually complete.
- `context-dump` — load rating is sensitive to what counts as "load." Human effort is high (many characters) but *protocol overhead* is low (single flow). Rating of M is a compromise; the split H:M / A:L flag stands.
- `mett-tc-sweep`, `nvc-four-slot` — load is sensitive to whether these are run as *internal* self-administered sweeps (L) or *surfaced artifacts* (M-H). Corpus would tell us how they actually get used.
- `dialogic-context-accretion` — rated M human / H agent, but load depends on how often "context I wish I had" fires. Low-frequency-fire → L; high-frequency → M-H.

These protocols should not be finalized until Step 4.5 corpus data is available.

---

## 9. Closing summary: what cognitive-load theory recommends

**Headline:** favor **recognition over construction**, **one-shot over continuous**, **agent-side-loaded over human-side-loaded**, and **dual-process-aligned over dual-process-fighting**.

**Core recommendations:**

1. **Pick a low-load opener.** `commanders-intent` + `autonomy-scope-grant` + `pre-action-plan-disclosure` is a strong triple: recognition-dominant, one-shot, dual-process-aligned. `scqa-opener` is an alternative for non-implementation sessions.
2. **Pick a low-load plan form.** `dialog-log-plan` for exploratory; `adr-nygard` or `coding-opord-4slot` for decisions that deserve a compact artifact; `test-cases-as-plan` when behavior decomposes cleanly.
3. **Off-load to agent-side sub-agents.** `kerf-parallel-reviewer`, `scribe-sub-agent`, `question-preserving-autonomy`, `agent-surfaced-parking`, `gordon-roadblock-filter`, `directional-clean-repetition`, `pre-reply-self-review` — all bend load toward the agent while the human receives synthesized output. This is the correct asymmetry for a senior solo developer whose attention is the binding constraint.
4. **Use low-load stance primitives sparingly:** `tactical-pause`, `aporia-graceful-stop`, `going-to-the-balcony`, `forced-choice-with-default`, `handoff-closed-acknowledgment`. Each is near-zero load individually; they add up additively.
5. **Reject, for load reasons, on this user:** `strong-style-pairing`, `teach-back-loop`, `maieutic-drawout` and `evocation-darncat` as defaults (reserve as rare micro-moves), `classical-driver-navigator`, `emergent-partition` (unless working-memory externalized), `assumption-bundle` and `knowledge-state-inventory` (unless working-memory externalized AND the problem warrants the artifact depth), `protocol-shapeshifting` (meta-load compounds everything).
6. **Treat `autonomous-dispatch` as a load-optimal *conditional* choice.** It is the lowest-load shape *when the prior spec is adequate*. It is not the lowest-load shape when it isn't — the load just displaces to reconciling silent drift later. Conditional endorsement.
7. **Watch for multiplicative stacking.** Any pair of protocols with *continuous bookkeeping* requirements should be flagged; they will interact multiplicatively. In particular, avoid running mission-command's cadenced artifact stack and medical handoff's synthesis-gated stack simultaneously.

**Observed-pattern critique:** the corpus-dominant `context-dump` opener and `dialog-log-plan` form are load-favorable *by accident of single-flow construction*, not because they are optimal. `context-dump` specifically shifts load to front-loaded one-shot writing (cognitively efficient) at the cost of correction-loop absence (cognitive efficiency comes from never checking, which is why errors propagate far). The observed pattern is a local optimum on the cognitive-load axis; it does not generalize. The cognitive-load recommendation is to **keep the single-flow + dialog-log structure** (it is genuinely low-load) **while adding a single bounded read-back or pre-action plan disclosure gate** to close the correction loop without introducing continuous bookkeeping.

**The one highest-leverage single change on the cognitive-load axis:** add `pre-action-plan-disclosure` (or equivalently `back-brief-plan-quality`) to any opener that currently lacks a pre-commit checkpoint. It is L-load, recognition-dominant, dual-process-aligned, and addresses the specific error mode that the corpus-dominant low-load patterns leave unaddressed. It is the cognitive-load-best answer to the "how do we get correction loops without inflating attention cost?" question.

---

*Reviewer sub-agent closing: my ranking is cognitive-load-only. It does not evaluate robustness, ergonomics, task-fit, or outcome quality — those are other reviewers' frames. Step 6 synthesis should cross me against those reviewers. Where they agree with me, the protocol is safe on my axis and theirs. Where they disagree, the disagreement localizes a legitimate load-vs-other-property trade the user has to make explicitly.*
