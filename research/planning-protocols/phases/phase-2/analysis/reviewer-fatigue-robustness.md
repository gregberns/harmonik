# Reviewer Evaluation — Fatigue-Robustness Frame

**Phase 2, Step 5 artifact.** Author: reviewer sub-agent (fatigue-robustness framing), 2026-04-23. Input: `unified-protocol-catalog.md` (87 protocols). Input framework: `evaluation-framework.md`.

---

## 1. Frame definition

**Robustness-to-user-fatigue** is the degree to which a planning protocol *degrades gracefully* across the human's full range of operating states — fresh, tired, distracted, attention-drained, interrupted, or in a novice moment on an unfamiliar subdomain. A robust protocol produces acceptable output when the human gives terse answers, zones out for two turns, or types "whatever" to a mid-stream question. A brittle protocol requires sustained active generation (read-back, open-ended answer, multi-slot authoring, ad-hoc judgment) at every checkpoint; when that generation thins, the protocol either silently drifts, prematurely closes on a wrong answer, or stalls into a loop the tired human cannot break.

The scoring distinguishes four load-bearing sub-signals: (a) whether the protocol's checkpoints are *recognition* (pick from a menu / ratify a draft) or *recall/generation* (produce a slot, draft a position, answer open-ended); (b) whether the protocol has *auto-recovery* after a turn of inattention, or requires re-establishing context; (c) whether the protocol's *failure mode under terse input* is safe drift (protocol keeps working, agent's guess is flagged) or silent drift (protocol closes on whatever was last said); (d) whether the protocol can handle the *novice-moment* condition where even a senior user has no vocabulary for a subdomain. A hidden fifth signal is whether the protocol is *fatigue-targeting* by construction — dispatch-style autonomous runs explicitly move the work off the tired user. The observed corpus is biased toward the user's typical state; protocols the user has avoided are plausibly the ones that would fail at 11 PM, so absence-from-corpus is *not* evidence of weakness.

---

## 2. Per-protocol scoring table

Score key: **R = Robust** (degrades gracefully across fresh/tired/novice states); **M = Moderate** (works well fresh, acceptable when tired with caveats); **B = Brittle** (requires fresh-user active generation to function; fails predictably under fatigue). Fatigue-targeting protocols (designed to bypass tired-user engagement entirely) are marked **R\***.

### Group 1 — Opener structures

| ID | Score | Rationale |
|---|---|---|
| sbar-opener | M | Four slots require generation at session open (worst fatigue moment). But slot names prompt recognition of missing content; tolerates terse fills. Safer than free-form but harder than context-dump. |
| ipass-opener | B | Five slots + mandatory receiver synthesis = generation at both ends. Synthesis gate cannot be skipped; tired user cannot produce clean synthesis. Heavy. |
| commanders-intent | M | Short artifact (2-3 sentences), but writing *good* intent is a senior skill hardest exactly when tired. However intent can be written when fresh and *reused* — score elevates to R if the artifact is pre-built. |
| smeac-order | B | Five paragraphs of generation at session open. High ceremony cost. |
| scqa-opener | M | 3-5 sentence structure is light; Answer slot requires the user actually know the answer (degrades to SCQ under fuzziness, which is fine). |
| engagement-letter-opener | B | Contractual ceremony: Objectives, Scope, Out-of-Scope, Deliverables, Governance, Change-order. Heavy authoring at opener; tired user cannot produce a complete out-of-scope list. |
| context-dump | M | Front-loaded authoring when user is *already* committed to dumping — the dumping act selects for users with something to dump. *But* if tired user attempts context-dump without the mental state for it, errors propagate furthest before detection (catalog's own observation). State-dependent. |
| recovery-handoff | R | Payload is structured and was authored in a prior session. Current session just reads it. Auto-recovery built in by construction. Classic fatigue-robust move. |
| autonomous-dispatch | R\* | *The* canonical fatigue-targeting protocol: planning happened elsewhere, dispatch is one-line, agent runs without user input. When tired, this is the safest move in the catalog. Risk is silent drift from imperfect pre-built spec; failure is asymmetric (agent keeps producing, user sleeps). |
| sterile-phase-declaration | M | Named phase with declared moves — recognition-based once declared. Declaring requires lucidity; tired user might declare wrong phase. |
| warno-prep | M | Staged authorization (prep authorized, execution not) offloads some fatigue by amortizing planning. Lightweight WARNO itself is easy; follow-up OPORD is heavy. |
| mett-tc-sweep | B | 6-slot self-administered checklist requires the user to produce information in each slot at opener. Checklist fatigue is real. |
| batna-articulation | B | Asks tired user to articulate fallback behaviors — a reflective move requiring clarity about one's own alternatives. Rarely done at any state. |
| agenda-setting | R | Agent proposes slate, human selects. Pure recognition. Un-selected items are retained. Tired-user-compatible. |

### Group 2 — Question discipline

| ID | Score | Rationale |
|---|---|---|
| elenctic-probe | B | Requires user to spot contradiction between stated intent and implied commitments. Cognitively expensive at *every* probe, on *every* turn. The prototype fatigue-hostile protocol. |
| maieutic-drawout | B | Senior user + junior LLM + "explain what you think" at every turn = insulting when fresh, defeating when tired. Generation-heavy by design. |
| reduced-dialectic | M | Agent generates position+counter; user rules on synthesis. Ruling is recognition. But one-shot ruling under fatigue is brittle — tired user picks the first option. Depends on agent producing genuine counter. |
| socratic-seminar-turn | B | Closing phase requires user to state what they now believe. Generation at close is a fatigue-worst moment. |
| question-level-targeting | R | Meta-protocol the agent applies to itself; suppresses Bloom-1-2 questions. User sees fewer trivial questions. Pure win for tired user (protocol removes work). |
| five-whys-bounded | M | 2-3 rounds of "what earlier choice made this necessary?" Short and bounded is tolerable. Literal "why?" is fatigue-expensive. |
| laddering-acv | M | Short-volley 2-4 turns; mechanical. Recognition-ish (user sees the chain building). Tolerable. |
| spin-sequence | B | Implication questions are the cognitively heaviest question class; "trace cascading downstream costs" is exactly what tired users fail at. |
| interest-probe | M | "Why do you want that?" on a considered decision — senior + tired = insulting. Works better in exploratory state. |
| option-invention | R | Agent presents 2-3 option slate; user ranks or rejects. Ranking is recognition. Strong fatigue-fit. |
| objective-criteria | R | Agent cites an external standard; recognition-only on the user side. Low-generation. |
| evocation-darncat | B | "What pulls you toward this?" open-ended evocation is generation-heavy and senior-user-insulting when tired. |
| epe-information | M | Three moves (elicit / provide / elicit). Two elicits = two generation moments per info delivery. Heavy for tired user. |
| readiness-ruler | M | 0-10 number is easy; "what's pulling you?" is open-ended generation. Mixed fatigue-load. |
| numbered-question-close | R | Agent batches questions with numbers; user answers by number (recognition + short content). Lowers re-parse cost. Observed empirically to shorten subsequent human turn — that *is* a fatigue signal. Strong. |
| implication-question-discipline | B | Requires agent to force at least one implication question before solution; implication questions are fatigue-heaviest. |

### Group 3 — Decision-locus and autonomy

| ID | Score | Rationale |
|---|---|---|
| upfront-decision-partition | M | Partitioning at opener is authoring-heavy but pays off throughout session (fewer interruptions). State: authoring the partition requires clarity, not just attention; reasonable fresh, risky tired. |
| emergent-partition | B | Continuous re-derivation of the partition every 3-5 decisions. Each cycle demands judgment. Tired user drifts into mis-classification silently. |
| mission-command | R | Intent set once; subordinate deviates within intent; user absorbs risk. Explicit architecture for not needing user engagement mid-stream. |
| autonomy-scope-grant | R | One-sentence pre-authorization at opener, then protocol runs without user generation. |
| contingency-preauthorization | M | Authoring if-then rules takes effort; tired user under-anticipates novel states. But once authored, runs without input. |
| incremental-step-autonomy | B | Continuous per-step check-in. Tired user gives "go" to too much; misses drift. |
| micro-step-incrementalism | B | 2-5 minute cycles, propose/confirm on every step. Maximum attention-consumption of any protocol in the catalog. Worst-in-class for tired users. |
| question-preserving-autonomy | R | Agent collects ambiguities into a queue; surfaces at checkpoints, not every turn. User can triage the queue in one focused batch or ignore. Explicitly fatigue-aware design. |
| authority-transfer-microritual | M | Fixed-token move is low-generation once learned; but learning overhead up front. Routine abuse ("blanket yours") is itself a fatigue response the protocol accommodates. |
| pre-action-plan-disclosure | R | Agent discloses plan; user corrects or silently accepts. Recognition by default. Silent acceptance = safer drift than no-disclosure. |
| fixed-token-status-vocabulary | M | Short tokens ("lock 3, defer 5") are low-generation after learning. But learning cost and agent-must-never-issue-locked discipline is fragile. |
| hypothesis-driven-ghost-deck | M | Agent drafts hypothesis and ghost deck; user rules at review. Author-side work is agent's; user is in recognition mode. Reasonable fatigue-fit. |
| example-led-emergence | M | Examples are concrete (easier to ratify than abstract frames), but many small ratifications add up; tired user over-ratifies. |
| forced-choice-with-default | R\* | "I'll do X unless you object" = silence carries decision. The fatigue-targeting protocol for individual decisions. Safe only when default is well-calibrated. |

### Group 4 — Review and read-back

| ID | Score | Rationale |
|---|---|---|
| read-back-comprehension | M | Agent-side readback — user's job is to catch error. Recognition task. Tired user may rubber-stamp a wrong readback (degenerates to silent-confirmation). |
| load-bearing-token-readback | R | Per-turn token echo with silence-as-confirmation. Low-generation by design. |
| back-brief-plan-quality | M | Agent presents plan + uncertainties; user corrects. Recognition-leaning but requires the user to actually read the brief. |
| teach-back-loop | B | User restates agent's plan in user's own words. Generation-heavy on receiver side. Catalog itself flags as borderline insulting to senior; doubly bad tired. |
| iap-cadence-artifact | M | Agent produces structured artifact on cadence; user ratifies or amends. Recognition-leaning but assumes user attends each cadence. |
| sitrep-at-cadence | R | 5-slot structured update at cadence; user skims. Pre-announces planned actions — tired user has predictable windows to redirect without reading every turn. |
| premortem-reviewer | M | Sub-agent generates failure-modes; user reviews. Author-side is the sub-agent's; user is in recognition. But reviewing failure-modes *well* requires engagement. |
| kerf-parallel-reviewer | R | Parallel sub-agents produce critiques; user reads synthesis. Recognition-plus-triage. Fatigue-friendly because critique-volume is produced by agents. |
| alternatives-considered-section | M | Hard gate on author producing alternatives. Gate-enforcement is agent-side (reviewer sub-agent). User reads rather than writes. |
| pyramid-answer-first | R | Agent output restructured for scan pattern. Pure fatigue-win: reduces required reading depth. |
| so-what-test | R | Self-review discipline on agent side; user sees trimmed output. User-side workload strictly reduced. |
| utility-tree | M | Structured artifact; tree can be produced by agent, reviewed by user. Author-side heavy, review side acceptable. |
| four-category-output | R | Agent produces four-category output; user scans categories. Recognition. |
| design-review-categorization | R | Reviewer output in categorization template; user scans categories. |
| aar-four-question | B | Post-session review when user is at *lowest* energy. Four questions requiring reflective generation. Catalog itself flags timing problem. |
| soapy-challenge-response-gate | B | Per-item spoken verification at gates. Attention-expensive by design. |
| confirmation-brief | R | Agent restates intent in its own words; user corrects if wrong, else silence. Classic recognition pattern. |
| closing-summary-ritual | M | Agent summarizes, user validates. Recognition-leaning but user must actually read and judge. |
| articulate-override-rule | B | Forces user to articulate a *reason* to dismiss reviewer comment. Generation at the exact moment of fatigue-dismissal. The opposite of silence-as-default. |
| hard-gate-missing-section | R | Blocks advancement on missing sections. User doesn't have to remember; the gate remembers. |

### Group 5 — Mediator / role-split

| ID | Score | Rationale |
|---|---|---|
| shuttle-diplomacy-reviewer | R | Mediator filters/batches/softens between agent and user. Explicitly reduces user-facing interaction. Fatigue-targeting. |
| single-text-procedure | R | One persistent artifact; agent drafts, user critiques. Critique is lower-effort than drafting. Artifact is always current — no "plan lives in dialog and must be extracted" problem when user returns. |
| role-split-reviewer-library | R | Reviewer sub-agents run parallel; user sees synthesis. Recognition. |
| named-role-separation | M | Solo-collapse rule means tired user absorbs all undelegated roles — exactly the failure mode the protocol is trying to prevent. Agent-side role-typing is helpful; user-side role-discipline under fatigue is fragile. |
| can-report-format | R | Structured status template. Recognition-only on user side. |
| scribe-sub-agent | R\* | Sub-agent maintains session log autonomously. Direct offload of a role that user otherwise degrades at when tired. |
| asymmetric-abstraction-shifting | M | Agent adapts abstraction to user cues. Passive for user. But "comprehension signals" under fatigue are ambiguous — agent may mis-calibrate. |
| cognitive-tag-team | B | Symmetric peer-equity presumption. Requires *both* parties to be engaged. Breaks under any fatigue. |
| classical-driver-navigator | B | Continuous navigator attention is *exactly* the attention cost the research is trying to reduce. Catalog's own assessment. |
| strong-style-pairing | B | Idea-holder navigates; driver trusts and implements. If user is idea-holder + tired, navigation collapses. |
| ping-pong-alternation | B | Continuous alternation requires both-sided engagement. |
| mob-parallel-reviewers | R | Multiple reviewer sub-agents; user reads union. Recognition. |
| asynchronous-navigator | R | Agent emits structured checkpoints; user skims at own pace. Explicitly designed for asymmetric attention. Fatigue-friendly. |

### Group 6 — Plan expression / artifact form

| ID | Score | Rationale |
|---|---|---|
| rfc-full-form | B | Nine structured sections of authoring. Ceremony-heavy. |
| adr-nygard | M | 5-slot compact artifact; agent can draft, user can ratify. Moderate. |
| pep-style-rfc | M | Hard gates on mandatory sections are recognition-based for the user (gate fires, user knows), but the missing-section the user must fill is generation. |
| issue-tree-diagnostic | M | Agent produces tree; user rules per branch. Recognition-leaning. |
| issue-tree-solution | M | Same as diagnostic. |
| mece-decomposition | M | Agent proposes decomposition; user catches overlaps / blind spots. The catching is generation if user generates new categories, recognition if user only rules. |
| dialog-log-plan | M | No artifact maintenance required — fatigue-friendly in the moment. But resumption-cost is extraction work — fatigue-hostile across sessions. Mixed. |
| behavior-first-plan | M | Behavior cases are concrete, recognition-friendly; but authoring comprehensive case list is work. |
| assumption-bundle | M | Agent builds numbered dependency graph; user edits entries. Recognition + targeted edit. Heavier than simple ratification but lighter than free authoring. |
| knowledge-state-inventory | M | Agent opens with inventory, user marks ✓ / ! / ? / +. Low per-move cost. But inventory maintenance silently drifts under fatigue. |
| nvc-four-slot | B | Four-slot structure per message. Catalog itself flags as stilted; always-on is fatigue-hostile. |
| coding-opord-4slot | M | 4-slot artifact; lighter than SMEAC. Reasonable. |
| frago-modification | R | Diff artifact — only changed paragraphs restated. Lower writing cost than full re-issuance. |
| position-interest-pair | M | Two slots per decision; agent can propose position+interest, user confirms. Recognition-leaning. |
| test-cases-as-plan | M | Concrete cases are recognition-friendly; authoring comprehensive cases is work. |

### Group 7 — Meta-protocols / shape-shifting / stance

| ID | Score | Rationale |
|---|---|---|
| protocol-shapeshifting | B | Requires user to detect phase transitions and declare protocol. Meta-judgment is exactly the first thing fatigue takes. |
| four-process-arc | M | Agent labels phase internally; user need not. Phase-gating can be agent-side. Reasonable. |
| aporia-graceful-stop | R | Named impasse; park-and-move-on. Fatigue-robust *because* honest "unresolvable now" is legitimized — protocol specifically accommodates tired-user-cannot-decide case. |
| tactical-pause | R | Either party calls; scripted trigger converts stall into deliberation. Legitimizes "I don't know yet" without uncertainty-cost. Fatigue-compatible. |
| narrative-reframe | B | Heavy reframe move; requires judgment on whether reframe is genuine. Premature application dramatizes. Tired user misjudges. |
| going-to-the-balcony | R | On pushback, agent summarizes-and-checks before defending. Protects tired user from agent's immediate-capitulation reflex. |
| rolling-with-resistance | R | On repeated pushback, agent stops re-explaining. Tired user who is pushing back gets heard. |
| third-story-framing | M | Neutral restatement on disagreement; recognition-leaning. Trigger-on-disagreement scope bounds cost. |
| positive-no | M | Agent uses structured three-slot rejection. Agent-side discipline; user sees the slots. Reads rehearsed. |
| non-directive-stance | B | Deliberately withholds agent content. Tired user *wants* agent to drive; pure non-directiveness feels like abandonment. |
| active-listening-readback | M | Per-turn prepended restatement. Low-generation; recognition. Catalog flags "slow" but slowness is fatigue-compatible. |
| gordon-roadblock-filter | R | Rejection-list self-filter on agent side. User-side load strictly reduced. |
| directional-clean-repetition | R | Agent preserves user's verbatim nouns. Prevents paraphrase-drift — which is *exactly* how fatigued user's terms get silently replaced by agent's. Strong fatigue-protection. |
| affirmations-competence | R | Agent notices human's prior work; does not re-ask. Directly saves tired-user from answering the same thing twice. |
| graded-assertiveness-pace | M | Probe-Alert-Challenge-Emergency; agent escalates appropriately. User-side is recognition (see the escalation level). Moderate. |
| cus-graduated-concern | M | Three-tier agent-side concern move. Same as PACE. |
| agent-surfaced-parking | R | Agent tracks parked branches, surfaces at appropriate moments. Saves tired user from remembering. |
| screener-gated-branching | R | 2-3 yes/no questions at topic transitions. Binary answers are the lowest-fatigue question class in the catalog. |
| handoff-closed-acknowledgment | M | Requires explicit ack at transitions. Short but cannot be skipped. |
| error-catching-posture | R | Continuous review via sub-agent hooked inline. Offloads review to agent. |
| pre-reply-self-review | R | Agent critiques its own response. Pure user-side workload reduction. |
| watcher-tier-orientation | R | One-tag opener (routine / watcher / uncertain). Lowest-possible-cost opener in the catalog. |
| onethird-twothirds-time | M | Meta-rule; compliance requires user judgment. Useful mostly as background principle. |
| rehearsal-hierarchy | M | Graded menu — user picks appropriate tier. Picking is recognition; executing rehearsal varies by tier. |
| controller-orchestration | R\* | User directs a running system; mid-stream direction is sparse. Fatigue-targeting for orchestrator use-case. |
| dialogic-context-accretion | B | Continuous per-turn "context I wish I had" probes. Many small interruptions; tired user can't maintain the rhythm. |

### Group 8 — Methodological borrows

| ID | Score | Rationale |
|---|---|---|
| incident-driven-catalog | R | Methodological; no per-session user load. |
| summary-as-transition | R | Agent produces summary at phase transition; user ratifies or amends. Recognition-leaning. Strong fit for tired user at end of a topic. |

---

## 3. Top 20 most-robust

1. **autonomous-dispatch** (R\*). The canonical fatigue-targeting protocol. Planning happens elsewhere when user is fresh; dispatch is one-line; agent runs without user engagement. The research-track user's own observed solution to time-constrained delegation. Risk is pre-built-spec thinness; benefit is that user can be entirely absent.

2. **forced-choice-with-default** (R\*). Silence-as-decision inverts the normal fatigue-failure mode (tired user can't generate). The default *must* be well-calibrated; when calibrated, this protocol uses fatigue instead of fighting it.

3. **scribe-sub-agent** (R\*). Session log maintained autonomously. Directly offloads a role user drops first when tired (recordkeeping).

4. **controller-orchestration** (R\*). User directs running system with sparse mid-stream correction. Fatigue-targeting for orchestrator use.

5. **numbered-question-close** (R). Catalog's own observation: shortens subsequent human turn length. That is a direct fatigue-compatibility signal. Recognition-mode answers by number.

6. **agenda-setting** (R). Agent proposes slate; user selects. Pure recognition. Un-selected items retained for later.

7. **recovery-handoff** (R). Payload was authored when user was fresh; current session reads it. Auto-recovery by construction.

8. **question-preserving-autonomy** (R). Agent collects ambiguities into queue; user triages in one focused batch or ignores. Explicitly designed to avoid interrupting user mid-autonomy — fatigue-aware by construction.

9. **shuttle-diplomacy-reviewer** (R). Mediator filters, batches, and softens interactions. User-facing interaction volume drops.

10. **single-text-procedure** (R). One persistent artifact is always current; no dialog-extraction on return after a break. Critique is lower-effort than drafting.

11. **asynchronous-navigator** (R). Agent emits structured checkpoints; user skims at own pace. Designed for asymmetric attention.

12. **sitrep-at-cadence** (R). Structured periodic update with "actions planned before next sitrep" — pre-announces autonomy use, giving tired user predictable windows to redirect without reading every turn.

13. **pre-action-plan-disclosure** (R). Agent discloses plan; user corrects or silent-accepts. Silent acceptance is a safer default than no-disclosure because the plan was made visible.

14. **aporia-graceful-stop** (R). Names the "unresolvable now" case and routes it to later. Legitimizes tired-user impasse; converts honest fatigue into a protocol move instead of a failure.

15. **tactical-pause** (R). Scripted trigger converts private stall into deliberation — legitimizes "I don't know yet" without uncertainty-cost. Fatigue-compatible.

16. **directional-clean-repetition** (R). Agent preserves user's verbatim terms. Directly prevents paraphrase-drift, which is the mechanism by which fatigued-user's intent gets silently replaced by agent's interpretation.

17. **pre-reply-self-review** (R). Agent critiques its own response before sending. Pure user-side workload reduction; catches pyramid-violations and roadblock-commissions before they reach the tired user.

18. **kerf-parallel-reviewer** / **role-split-reviewer-library** / **mob-parallel-reviewers** (R, bundle). Critique volume produced by sub-agents; user reads synthesis. Recognition-plus-triage.

19. **summary-as-transition** (R). At topic transitions, agent produces compact summary; user ratifies or amends. Strong fit at topic-end when user is depleted.

20. **agent-surfaced-parking** (R). Agent maintains parked-branch queue and surfaces at appropriate moments. Saves tired user from remembering what was deferred — a task memory fails at first.

**Also-robust honorable mentions** (would be top-20 with different tie-breaking): `watcher-tier-orientation` (one-tag opener; the cheapest opener in the catalog), `load-bearing-token-readback` (silence = confirmation), `confirmation-brief` (recognition-only), `pyramid-answer-first` and `so-what-test` (both reduce user-side reading load), `hard-gate-missing-section` (the gate remembers so user doesn't have to), `gordon-roadblock-filter` and `error-catching-posture` (offload to agent), `screener-gated-branching` (binary questions are the lowest-fatigue question class).

---

## 4. Bottom 10 most-brittle

1. **elenctic-probe**. Requires user to spot contradiction between stated intent and implied commitments. Cognitively expensive at every probe. The prototype fatigue-hostile protocol. Degrades to silent capitulation ("sure, whatever you say") under fatigue — which is *worse* than silence-as-default because the capitulation is treated as ratification.

2. **maieutic-drawout**. Open-ended draw-out at every turn. Senior-user + tired = insulting *and* ineffective. Generation-heavy by design.

3. **micro-step-incrementalism**. 2-5 minute propose/confirm cycles are maximum attention consumption in the catalog. "OK?" at every step becomes automatic "yes" under fatigue — protocol silently drifts.

4. **classical-driver-navigator**. Continuous navigator attention is exactly the cost the research is trying to reduce. Explicitly acknowledged in catalog.

5. **cognitive-tag-team**. Symmetric peer-equity presumption. Requires *both* parties engaged.

6. **ping-pong-alternation**. Continuous alternation; breaks under any asymmetric attention.

7. **teach-back-loop**. Senior user restates agent's plan in their own words. Generation on receiver side, exactly the wrong direction for fatigue.

8. **articulate-override-rule**. Forces user to articulate reason to dismiss reviewer comment — generation at the exact moment of fatigue-dismissal. Inverts silence-as-default in a fatigue-hostile way. (Valuable for decision-quality; brittle for user-state-diversity.)

9. **dialogic-context-accretion**. Continuous per-turn "context I wish I had" probes. Many small interruptions that tired user cannot maintain rhythm through. Silent drift when user stops annotating requests.

10. **protocol-shapeshifting**. Requires user to detect phase transitions and choose protocol. Meta-judgment is the first casualty of fatigue.

**Near-miss brittle** (would be bottom-10 with different tie-breaking): `aar-four-question` (reflective generation at session's lowest-energy moment), `spin-sequence` and `implication-question-discipline` (implication questions are the cognitively heaviest class), `strong-style-pairing` (idea-holder navigates = tired idea-holder collapses), `evocation-darncat` (open-ended evocation on senior user), `non-directive-stance` (tired user *wants* agent to drive), `nvc-four-slot` (four slots per message), `mett-tc-sweep` (checklist fatigue), `narrative-reframe` (judgment-heavy), `batna-articulation` (reflective move requiring clarity about alternatives), `smeac-order` and `rfc-full-form` (authoring-heavy at opener).

---

## 5. State-specific fit

### Fatigue-targeting by construction (R\*)

These protocols are *for* the tired dispatch case. They do not merely survive fatigue; they harvest it. The design commitment is that the user's engagement is *deliberately* thin, and the protocol works because of that, not despite it:

- **autonomous-dispatch** — planning is elsewhere; session is agent solo.
- **forced-choice-with-default** — silence carries decision; inverts fatigue-failure into fatigue-use.
- **scribe-sub-agent** — offloads the role user fails at first.
- **controller-orchestration** — user directs running system with sparse correction.

These are the protocols a user-state-diverse user should have readily available and *know* to reach for when tired. They are over-represented in the observed corpus (per the catalog's f588ff0c / secure-dev patterns) exactly because the user has self-selected them for tired moments.

### Robust across states (R)

Produce acceptable output at fresh, moderately-tired, and novice states. Recognition-based, agent-side-authored, or explicitly fatigue-accommodating. Safe defaults for a user-state-diverse user. See Top 20.

### Moderate — work fresh, degrade tired (M)

The largest category. These protocols produce good output when the user is engaged but silently produce *worse* output when the user rubber-stamps under fatigue. They are the most dangerous category because their failure is invisible — the output looks like the fresh case.

Examples of particular concern: `read-back-comprehension` (degenerates to rubber-stamp), `knowledge-state-inventory` (inventory silently drifts), `upfront-decision-partition` (partition rigidifies on a wrong axis), `mece-decomposition` (user misses non-MECE).

### Fresh-state-only (B)

Require sustained active generation. Valuable when user has the capacity, but should not be deployed as session defaults for a user whose state varies. Use in deliberately-chosen fresh windows. See Bottom 10.

### Novice-moment handling

A separate axis. Even fresh senior users have novice moments on unfamiliar subdomains. Protocols that handle "I don't even know what to ask" gracefully:

- **Strong:** `agenda-setting` (agent proposes slate), `option-invention` (agent invents options), `screener-gated-branching` (binary screeners), `hypothesis-driven-ghost-deck` (agent commits a stake-in-ground), `context-dump` (inverts — user doesn't need to know yet what's load-bearing, dumps everything).
- **Weak:** `maieutic-drawout` (draw out what the novice does not yet have), `evocation-darncat` (elicit reasons novice cannot yet articulate), `elenctic-probe` (force novice to produce a stake to then contradict), `strong-style-pairing` (navigate what you don't yet understand).

Novice-moment robustness correlates strongly but not perfectly with fatigue-robustness. The commonality is that both states need *recognition-presentation-of-options* rather than *generation-from-scratch*.

### Late-night / overnight fit

Explicitly overnight-targeting protocols in the catalog: `autonomous-dispatch` is the archetype; `controller-orchestration` extends to running system; `scribe-sub-agent`, `question-preserving-autonomy`, `error-catching-posture`, `pre-reply-self-review` all offload to agent-side work. These are the protocols that target "user dispatches tired, agent runs overnight, user reviews fresh."

Contrast with catalog's f588ff0c signal (the within-session short-volley → dispatch shift): this is shape-shifting toward dispatch *as* the user tires. The capability is important; the protocol name `protocol-shapeshifting` is brittle itself (the shifting requires judgment), but the *instance* of shifting short-volley → dispatch is exactly the fatigue-aware move.

---

## 6. Degradation-mode taxonomy

When moderate or brittle protocols fail under fatigue, they fail in distinguishable ways. Enumerated:

- **Silent drift.** Protocol keeps running, user keeps rubber-stamping, output diverges from user's actual intent without visible correction event. Worst failure mode because it is undetectable in-session. Examples: `read-back-comprehension` (tired user confirms a wrong readback), `micro-step-incrementalism` (automatic "yes" on every step), `knowledge-state-inventory` (inventory diverges from shared model).

- **Premature close.** Protocol forces a decision at a moment user lacks capacity; user picks the first option to end the prompt. Examples: `reduced-dialectic` (one-shot synthesis ruling), `option-invention` when slate has a clear default that's actually wrong, any forced-choice with miscalibrated default.

- **Loop.** User cannot break out of a protocol's question cycle; each iteration adds fatigue. Examples: `elenctic-probe` (contradiction-generation loops), `maieutic-drawout` (draw-out without exit condition), `dialogic-context-accretion` (continuous "context I wish I had" without stopping rule for tired user).

- **Human-gives-up.** User explicitly abandons — "whatever," "your call," "let's just stop." Protocol may be correctly identifying a real question but asking it in a way the user cannot answer now. Examples: `evocation-darncat`, `teach-back-loop`, `interest-probe` on senior user. Not necessarily a bad outcome — "whatever" at least surfaces that the user isn't engaging, which is more honest than silent drift. But the protocol that produces it has failed to deliver value this session.

- **Ceremony collapse.** Protocol's ritual is performed mechanically without its substance. Examples: `mett-tc-sweep` filled tersely in all six slots, `smeac-order` with copy-paste Situation, `alternatives-considered-section` with obvious rejected-alternative filler. The artifact exists; it does not carry the load the structure was supposed to compel.

- **Role-collapse.** In solo + agent setting, the "solo-collapse rule" means tired user absorbs all undelegated roles — exactly the failure mode some protocols try to prevent. Examples: `named-role-separation`, `classical-driver-navigator`.

- **Meta-judgment failure.** Protocol requires user to decide *about* the protocol (detect phase transition, choose protocol, judge when to reframe). Examples: `protocol-shapeshifting`, `narrative-reframe`, `non-directive-stance` applied at wrong moment.

Silent-drift and ceremony-collapse are the two most dangerous because they look like success. Human-gives-up is the least dangerous because it's a legible signal.

---

## 7. Counterfactual — fresh-always vs. tired-always

### If user were always fresh

Several Brittle (B) protocols would rise substantially:

- **maieutic-drawout**, **elenctic-probe**, **evocation-darncat** would move from B to M or higher — their draw-out mechanism is valuable when user can generate, and the catalog's "insulting to senior" objection is a different axis. Would need independent evaluation on senior-senior-state question.
- **spin-sequence** and **implication-question-discipline** would rise — implication questions are the cognitively heaviest class, which also makes them the most valuable when the user has the capacity. Fresh users produce the cascade reasoning that tired users cannot.
- **teach-back-loop** would be M — still borderline insulting but usable.
- **dialogic-context-accretion** would be M — per-turn annotation is tolerable when user is attentive.
- **classical-driver-navigator**, **ping-pong-alternation**, **cognitive-tag-team** — peer-equity protocols become viable. Fresh user can maintain continuous engagement.
- **aar-four-question** — reflective generation is tractable when fresh, even at session end.

Several Robust (R\*) protocols would *not* fall — `autonomous-dispatch`, `scribe-sub-agent`, `controller-orchestration` remain valuable even fresh because they target scaling the user's reach, not surviving fatigue. Similarly, `pre-reply-self-review`, `pyramid-answer-first`, `so-what-test`, `error-catching-posture` all reduce user-side work regardless of state.

But several R protocols might fall slightly — `forced-choice-with-default` loses some value if fresh user is better at active ratification; `recovery-handoff` less necessary if user is always fresh.

### If user were always tired

The M category bifurcates sharply. Protocols that appeared moderate would reveal their fatigue-dependency:

- **upfront-decision-partition**, **knowledge-state-inventory**, **read-back-comprehension**, **MECE-decomposition**, **hypothesis-driven-ghost-deck** fall to B — each relies on user catching drift, and tired user rubber-stamps.
- **interest-probe**, **interest-vs-position-surfacing**, **readiness-ruler** fall to B — the open-ended follow-ups require lucidity.
- **upfront-decision-partition** specifically falls because authoring a good partition requires clarity; tired user partitions on the wrong axis and rigidifies.

Protocols that would rise:

- **autonomous-dispatch** becomes near-mandatory.
- **forced-choice-with-default** becomes near-mandatory.
- **agent-surfaced-parking**, **scribe-sub-agent**, **pre-reply-self-review**, **question-preserving-autonomy**, **error-catching-posture**, **mediator** patterns all become more valuable — any protocol that offloads to agent side rises.
- **aporia-graceful-stop** and **tactical-pause** become load-bearing — their legitimization of "unresolvable now" is the protocol move a tired user actually needs.
- **summary-as-transition**, **recovery-handoff** become more valuable because always-tired user needs always-warm context.

### Diagnostic pair

The difference between a protocol's fresh-always score and its tired-always score is that protocol's **fatigue-dependency**. Large gap = state-sensitive protocol; small gap = state-robust protocol.

| Protocol class | Fresh score | Tired score | Fatigue-dependency |
|---|---|---|---|
| Peer-equity pairing (`classical-driver-navigator`, `cognitive-tag-team`, `ping-pong-alternation`) | R-M | B | **Very high** |
| Socratic draw-out (`maieutic-drawout`, `elenctic-probe`, `evocation-darncat`) | M-R | B | **Very high** |
| Micro-incremental (`micro-step-incrementalism`, `dialogic-context-accretion`) | M | B | High |
| Generation-heavy openers (`smeac-order`, `rfc-full-form`, `engagement-letter-opener`) | M | B | High |
| Rubber-stampable recognition (`read-back-comprehension`, `knowledge-state-inventory`) | R | B | High (dangerous — looks robust) |
| Agent-side offload (`pre-reply-self-review`, `scribe-sub-agent`, `error-catching-posture`, `so-what-test`) | R | R | Very low |
| Silence-based defaults (`forced-choice-with-default`, `load-bearing-token-readback`) | R | R | Very low |
| Fatigue-targeting dispatch (`autonomous-dispatch`, `controller-orchestration`) | R (re-purpose for reach) | R* | Zero or negative |

The "rubber-stampable recognition" row is the most diagnostically interesting: these protocols score well on both the fresh and tired axes *individually*, but they fail under tired-state by **silent drift** — the class of failure the scoring cannot detect per-session. Protocols in this row require outcome-side verification (R1/R2 in the evaluation framework) to distinguish safe-recognition from drift-prone-rubber-stamp. The fatigue-dependency is latent.

---

## 8. Closing recommendations

### Deploy for a user-state-diverse user (always-available stack)

Build the default planning environment around these, so that whatever state the user arrives in, something works:

- **Opener tier:** `recovery-handoff` (for resume), `watcher-tier-orientation` (cheapest fresh opener), `agenda-setting` (for menu-based focus), `autonomous-dispatch` (for tired-dispatch case).
- **Mid-session tier:** `pre-action-plan-disclosure` (recognition default), `forced-choice-with-default` (silence-as-decision), `question-preserving-autonomy` (queue ambiguities, surface at checkpoint), `sitrep-at-cadence` (predictable redirection windows).
- **Agent-side stack (always on):** `pre-reply-self-review`, `gordon-roadblock-filter`, `so-what-test`, `pyramid-answer-first`, `directional-clean-repetition`, `error-catching-posture`, `numbered-question-close`.
- **Reviewer tier:** `kerf-parallel-reviewer` / `role-split-reviewer-library` (which the user already deploys), `shuttle-diplomacy-reviewer` (new addition — mediator filters/batches user-facing interaction).
- **State-transition tier:** `aporia-graceful-stop`, `tactical-pause`, `summary-as-transition`, `agent-surfaced-parking`.
- **Artifact tier:** `single-text-procedure` (one persistent artifact), `frago-modification` (diff-based updates), `asynchronous-navigator` (skim-at-own-pace).
- **Dispatch tier (for tired-user overnight):** `autonomous-dispatch`, `scribe-sub-agent`, `controller-orchestration`, `error-catching-posture`.

### Deploy only in targeted fresh-state windows

Valuable protocols whose value depends on user capacity. Reserve for deliberately-chosen engaged-state sessions; do not run as defaults:

- **Deep draw-out:** `maieutic-drawout`, `elenctic-probe`, `evocation-darncat`, `five-whys-bounded` — when user has capacity to generate, these surface content no other protocol does.
- **Peer engagement:** `ping-pong-alternation`, `strong-style-pairing` (agent-as-navigator variant), `cognitive-tag-team` — when user wants intense collaboration.
- **Reflective review:** `aar-four-question`, `teach-back-loop` — only when user explicitly commits energy.
- **Authoring-heavy openers:** `smeac-order`, `rfc-full-form`, `engagement-letter-opener` — reserve for sessions where the authoring IS the point.
- **Implication discipline:** `spin-sequence`, `implication-question-discipline` — when user can produce cascade reasoning.

### Flag for outcome-side verification

Protocols in the "rubber-stampable recognition" fatigue-dependency row — `read-back-comprehension`, `knowledge-state-inventory`, `upfront-decision-partition`, `MECE-decomposition`, `hypothesis-driven-ghost-deck` — score well in-session but silently drift under fatigue. Cannot be distinguished from safe-recognition by in-session metrics alone. Would benefit from R1 (spec-revision-rate) and R2 (implementer-time-to-first-blocker) outcome joins specifically conditioned on stratified user-state (late-night vs daytime sessions) as natural-experiment data becomes available.

### Build-in fatigue signals to the practitioner-diagnostic layer

The diagnostic catalog (evaluation-framework §6.1) should add at least two fatigue-specific signals:

- **Rubber-stamp rate:** fraction of read-back/teach-back/ratification turns that produce zero correction over N turns. High rate under otherwise-active sessions = silent-drift indicator.
- **Human-gives-up detection:** "whatever"/"your call"/"let's just stop" rate — already in the wasted-question lexicon, but worth tracking as a fatigue signal distinct from autonomy-grant.

### Absence-from-corpus discipline

A significant portion of the brittle-list (`maieutic-drawout`, `elenctic-probe`, `spin-sequence`, `teach-back-loop`, `classical-driver-navigator`, `ping-pong-alternation`, `cognitive-tag-team`) are *absent* from the observed corpus. The user has likely self-selected away from them — possibly because of fatigue-hostility, possibly for other reasons (senior-user objection, preference). This review cannot distinguish those causes, but it does flag: these protocols' absence from corpus is not confirmation that they should be permanently deprioritized. Their fatigue-hostility is one plausible reason; their peer-equity-presumption mismatch with senior-solo use is another. A fresh-state A/B on 1-2 such protocols (per framework §8) would disambiguate.

---

## Endnote on this review's own limits

This review ranks 87 protocols on a single axis (fatigue-robustness). The ranking *does not* say these protocols are the best overall — a fatigue-robust protocol may produce thin plans, and a brittle protocol may produce the best plans when the user is capable. Step 5 synthesis should weigh this ranking alongside the rival framings (Commitment-Deferral, Mental-Model Coupling, Regret-Adjusted Outcome) and the corpus-signal filter. Fatigue-robustness is necessary for a user-state-diverse deployment stack; it is not sufficient for protocol selection on any single session.
