# Counter-Pattern Candidates — Steel-Manned Protocols

Phase 2, Step 3 output for the planning-protocols research track. For each of the eight counter-hypotheses enumerated in `research-statement.md` §6, this document designs one concrete protocol that would win *if the counter-hypothesis were true*. Each protocol is taken at full strength. Comparison to observed-pattern rivals is Step 5's job; Step 3's discipline is steel-manning, not adjudication.

Two methodological notes:

1. **Distinctness.** Counter-hypotheses 1, 3, 5, and 6 all push against some flavor of "up-front commitment." They are kept separate: #1 is about *plan disclosure* (should the plan be stated), #3 is about *decision-partition rules* (who decides what, declared when), #5 is about *autonomy stretch length* (how long between touches), and #6 is about *context loading* (is the brief monolithic or dialogic). The protocols below respect those distinctions.
2. **Steel-man plus honest assessment.** Where, after designing the strongest version, I still believe a counter-hypothesis is weaker than its framing suggests, the protocol ends with an explicit "honest reassessment" note. The protocol itself is still presented at full strength above that note.

Dimensions in §4 of each protocol follow the ten-axis schema from research-statement §3 (timing, decision locus, dialog form, question style, autonomy scope, context richness, branching, plan expression, knowledge direction, review integration).

---

## 1. Example-Led Emergence (counter-hypothesis #1: plan disclosure causes premature commitment)

### Core stance

The plan is an *artifact that emerges from a few concrete cases*, not a contract stated up front. Alignment is verified by reaction-to-examples, not by approval-of-abstractions. Any attempt to describe the intended behavior in abstract terms before seeing two or three specific input-output cases will lock in framing the human cannot yet critique.

### Interaction shape

**Open.** Human states a task in one or two sentences and names a *first example* — an input, an output, or a narrative walk-through of "what would happen here." No plan is requested or offered. If the human gives no example, the agent picks the most interesting-to-probe example and proposes it ("Here is the case I'd start with unless you object: ...").

**Mid-session turn.** Agent produces a *concretion*: for example A, here is what the agent would do, step by step or output-by-output. Human reacts to the concretion — correction, extension, or "yes, now do example B." The agent does not generalize aloud; it accumulates examples silently. The human is free to ask "why did you do that in A?" at which point the agent justifies from the specific case, not from an unspoken general rule.

**Transition to plan.** When the agent has three to five concretions that seem to share structure, it writes a *post-hoc plan draft* — framed explicitly as "this is my read of what we have converged on, not a commitment" — and invites the human to mark any example that does not fit.

**Close.** The session ends when the human says "now generalize" and the agent produces a spec that cites each example as a row in an acceptance table. Implementation follows, validated against the example set. No separate approval step on the abstract plan; the examples *are* the approval.

Sample opener (human): "I want to add retry semantics to the task runner. Start with: a task that fails because the DB is down for 2s, then recovers."

Sample mid-session turn (agent): "For that case I would retry with backoff [2s, 4s, 8s] and mark transient, then succeed on attempt 3. Concrete trace: [shows the trace]. Next example on my list: a task that fails because of a bug in the task code. Want me to walk that one or do you have a different second case?"

### Dimension values

- **Timing of alignment:** in-action, example-by-example.
- **Decision locus:** interactive but narrow-grained — the human ratifies specific behavior on specific cases, not general rules.
- **Dialog form:** short-volley, concrete, trace-heavy.
- **Question style:** implicit (assumption-stating via concrete example).
- **Autonomy scope:** bounded-by-example-set.
- **Context richness:** continuous-building, case-indexed.
- **Branching:** agent-tracked (agent maintains a list of candidate next-examples).
- **Plan expression:** test-cases / behavior list (the case table is the plan).
- **Knowledge direction:** bidirectional, agent-investigative (agent picks probing examples).
- **Review integration:** none during examples; self-review at the post-hoc generalize step.

### Mechanism

Premature commitment effects (Green & Petre; more generally, the anchoring literature) predict that articulating an abstract plan causes both parties to interpret subsequent discussion *through* the plan's framing, suppressing alternatives that don't fit its vocabulary. Concrete examples are lower-commitment: they leave the space of possible generalizations open, and disagreement surfaces as "that case should do X, not Y," which is easier to produce than "the underlying framing is wrong." The human's reactive mode (judging a concrete output) is also cognitively cheaper than their generative mode (writing a plan), so writing cost shifts to the agent's trace production — which is free.

### Predicted trade-offs

- Wall-clock time may grow: the agent produces many trace sketches, some of which will be discarded.
- Risk of combinatorial blow-up: if examples are not chosen to be *span*ning, the agent may converge on a plan that handles the shown cases but misses an adjacent region.
- Bad fit for tasks whose value is in a globally-coherent structural decision (e.g., "choose a schema"): the example set for a schema is poorly defined.
- Hard to resume across sessions: the accumulated concretions are long; recovery handoff bloats.

### Evaluation plan

Transcript-derivable signal: count turns in which the human *corrected the plan's framing* vs turns in which the human *corrected behavior on a specific case*. Framing corrections late in a session are a signal the abstract plan locked in prematurely; case corrections are a signal the example-led flow surfaced the issue cheaply. Run side-by-side on sessions where the task had a latent framing decision (e.g., "what counts as a retry?") — does Example-Led Emergence surface the framing issue at turn 3 while pre-action plan disclosure surfaces it at turn 15?

### Honest failure mode

Collapses when the task has no natural example granularity — architectural decisions, naming conventions, cross-cutting invariants. Also collapses when the human's intent depends on a *property* of the plan as-a-whole (completeness, performance envelope) that no finite example set can exhibit.

---

## 2. Assumption-Bundle Disclosure (counter-hypothesis #2: batched questions surface connected assumptions)

### Core stance

Planning errors cluster in *webs of connected assumptions*. Asking one question at a time forces the human to answer that question in a context they do not yet see — often causing them to answer coherently-looking but locally-incoherent sets of answers when the next questions arrive. The protocol deliberately batches connected assumptions and exposes their connection explicitly, so the human can answer the *set* coherently.

### Interaction shape

**Open.** Human issues a task. Agent does not answer; instead, it performs a silent read and produces an *assumption bundle* — a numbered list of interdependent assumptions with explicit dependency arrows between them, in the shape "I am assuming {A1}; this forces {A2} unless {A3}; together these imply the approach {P}. Confirm / edit / flag wrong anchor."

**Mid-session turn.** Human responds by editing entries in the bundle. Agent revises the bundle by propagating edits through the dependency graph — entries that depended on an edited one are re-flagged for re-confirmation. The bundle is the persistent artifact; turns update it rather than accumulating prose.

**Close.** When the bundle has zero unconfirmed entries, it is frozen and becomes the dispatch context. Implementation proceeds against it.

Sample opener (human): "Add a reservation system for the task workers."

Sample mid-session turn (agent): "Assumption bundle v1. (1) Reservations are per-worker, not per-queue. (2) Given (1), reservation is coordinated via the Beads SQLite, not via a separate service, because the service would need state (1) enforces. (3) Given (2), stale-reservation detection is pull-based from the daemon, not push from the worker, because SQLite does not push. (4) A reservation lost on crash is recovered via TTL, default 60s. (5) Given (4), work items must be idempotent within 60s of reservation loss. Confirm / edit entries; edits to (1) or (2) will cascade."

### Dimension values

- **Timing of alignment:** pre-action, via bundle confirmation.
- **Decision locus:** interactive, but surfaced as a graph not a stream.
- **Dialog form:** structured (typed artifact with dependency links).
- **Question style:** batched, explicitly connected.
- **Autonomy scope:** bounded-by-frozen-bundle.
- **Context richness:** rich brief, but built *by the agent from the task* rather than written *by the human*.
- **Branching:** explicit parallel (bundle entries exist simultaneously; edits cascade).
- **Plan expression:** structured (the frozen bundle is the spec).
- **Knowledge direction:** agent-investigative, human-ratifying.
- **Review integration:** cascade re-review on any edit (self-review by the agent as it propagates).

### Mechanism

If assumptions co-vary, then answering A1 commits to A2 whether or not A2 is asked. Serial one-at-a-time asking forces the human to answer each in isolation, and later questions then either (a) contradict earlier answers or (b) are dropped by the agent as already resolved. Presenting the set *with its coupling visible* lets the human pick the highest-leverage entry (the "anchor") and let the rest cascade — which is how senior architects reason about design questions already.

### Predicted trade-offs

- Cognitively heavier per turn: the human must read and process a structured artifact. Short tasks pay a tax.
- Writing-cost on the agent side grows: constructing bundles well is expensive.
- Risk of anchoring on the agent's proposed bundle structure — if the real assumption set is differently shaped, the bundle miscues the human.
- Depends on the agent being able to see assumption couplings. If it can't, the bundle reads like a list of disconnected questions, which is worse than one-at-a-time.

### Evaluation plan

Signal: per-session count of *consistency corrections* — turns where the human says "wait, that contradicts what I said earlier about X." Assumption-Bundle Disclosure should produce more of these at bundle-v1 time and fewer later. Compare against one-at-a-time flow, where consistency corrections tend to cluster near implementation start. A secondary signal: does the human edit a bundle *anchor* (a load-bearing entry with many dependents) vs. an *isolated* entry? Edits to anchors demonstrate the coupling-surfacing value.

### Honest failure mode

Collapses on tasks where assumptions are genuinely independent. Also collapses if the agent's bundle structure is wrong — the human then spends effort *fixing the bundle shape* before answering it, which is worse than a plain conversation. Needs an escape hatch: "reject bundle structure, go linear."

---

## 3. Emergent-Partition Protocol (counter-hypothesis #3: mid-session decision-partition adapts to problem shape)

### Core stance

"What's trivial vs architectural" cannot be known up front because it depends on the problem shape, which is itself under construction. An up-front partition rule freezes the agent's sense of trivial before the problem is understood. The protocol instead lets the partition *form mid-session*, at the moment when the distinction becomes evidentially supportable.

### Interaction shape

**Open.** Human states task. No partition rule is declared. The agent begins by noting *decision points* it encounters as it sketches an approach, without classifying them.

**Mid-session turn.** Each decision point gets a short marker: `[D: <question>]`. After three to five such markers accumulate, the agent pauses and proposes a partition:

> "I've hit 5 decisions so far. Here is my first-pass partition. **Trivial** (I'll decide): D1 (file name), D3 (dict vs list internal). **Architectural** (I won't decide): D2 (does this block the scheduler?), D4 (is this in the hot path?), D5 (does this change the agent-handler contract?). Shifts in this partition are expected. Confirm, or reshuffle."

Human confirms, reshuffles, or adds a rule ("anything touching the scheduler is architectural"). Agent then proceeds and *refreshes the partition every N decisions* — the rule that governs trivial-vs-architectural is itself re-derived from accumulated context, not frozen.

**Close.** At handoff-to-implementation time, the partition is captured as a spec annotation: each decision is recorded with its class at the time it was taken. This lets future sessions see *how the partition evolved*, not just what was decided.

Sample opener (human): "Let's wire beads to the orchestrator."

Sample mid-session turn (agent): "Decisions encountered so far: D1 [which table stores workflow-to-bead link], D2 [does the link persist across orchestrator restarts], D3 [snake_case or camelCase column]. My partition proposal: D3 trivial (I'll pick snake_case, matches rest of schema). D1 and D2 architectural — D2 especially because restart semantics are an open decision area. OK to proceed on D3 on that basis?"

### Dimension values

- **Timing of alignment:** continuous, partition-refreshing.
- **Decision locus:** mixed, re-tuned mid-session.
- **Dialog form:** hybrid, with periodic partition micro-artifacts.
- **Question style:** forced-choice-with-default on partition placement; open-ended on the architectural decisions themselves.
- **Autonomy scope:** bounded-by-category, with category itself revisited.
- **Context richness:** continuous-building.
- **Branching:** implicit; decisions are serial.
- **Plan expression:** structured (decision log with class annotations).
- **Knowledge direction:** bidirectional; agent proposes partitions, human refines the rule.
- **Review integration:** periodic self-review by the agent at each partition refresh.

### Mechanism

The trivial-vs-architectural distinction is *task-relative*. A file name is trivial in most work but load-bearing in a filesystem-layout task. An up-front partition forces the human to predict which decisions will be which, which is exactly the kind of specification-at-a-distance the user otherwise wants to avoid. Re-deriving the partition when decisions arise delays the specification work to the moment when the evidence is present. This is analogous to continuous re-planning in incident command — the incident action plan is re-issued each operational period, not frozen at start.

### Predicted trade-offs

- More interruption than the up-front partition: the agent pauses every N decisions.
- If the human is inattentive, partition refreshes accumulate without confirmation and the agent stalls.
- Partition-evolution artifact is verbose; adds post-implementation reading cost.
- An agent bad at *spotting decisions worth flagging* will silently resolve architectural decisions as trivial and only later surface them.

### Evaluation plan

Signal: count of *reclassifications* — decisions the agent initially marked trivial that were later reclassified architectural (or vice versa) within the same session. A high reclassification count is the *value* of the protocol, not a failure: it shows the partition adapted. Compare to up-front-partition sessions, which by construction have zero reclassifications. If reclassified decisions correlate with late-session rework in the up-front-partition group, the counter-hypothesis is supported.

### Honest failure mode

Collapses in sessions that are entirely trivial (no architectural decisions surface, partition refresh is pure overhead) or entirely architectural (every decision halts the agent, producing a worse-than-interactive flow). Best on tasks with mixed decision density that isn't visible up front.

### Distinctness note

This protocol is about *when and how the partition rule is set* (mid-session, evolving). It is not about *whether to disclose a plan* (protocol #1) or *how long the agent runs before checking in* (protocol #5).

---

## 4. Open-Ended Hand-Off (counter-hypothesis #4: open-ended close surfaces deeper issues)

### Core stance

Numbered-question closes force the agent into a *closed set* of questions it has already chosen to ask. The deeper issues — the ones the agent *hasn't* framed yet — are precisely the ones suppressed by a closed enumeration. The protocol replaces the numbered close with an open-ended close that explicitly invites the agent to surface what it doesn't yet know how to ask.

### Interaction shape

**Open.** Same as any planning dialog: human states task, agent works toward understanding.

**Mid-session turn.** Agent closes each of its turns with *one of three moves*, chosen explicitly:

- **"Proceed."** Agent believes it has enough; no questions.
- **"Name the shape."** Agent names what class of question it would ask if it knew how ("I am worried about something around how this interacts with restart, but I can't yet name the question cleanly"). Human responds with either a reframing or an instruction to explore further.
- **"Free-form."** Agent writes two to four sentences of honest uncertainty, un-numbered, and asks the human to react to whichever sentence matters.

No numbered lists. The agent is explicitly forbidden from enumerating questions.

**Close.** A session reaches plan state when the agent issues a "Proceed." close and the human accepts.

Sample close (agent, "Name the shape"): "My open concern is not yet a question: something is off about how workers discover reservations after a crash. It's not a race, it's more of a time-of-knowledge issue. I'd rather explore that than commit to an approach. If you see the shape I'm groping for, please name it; if you don't, tell me to explore."

### Dimension values

- **Timing of alignment:** continuous, close-driven.
- **Decision locus:** interactive, with a broader question space.
- **Dialog form:** narrative, un-structured closes.
- **Question style:** open-ended, explicitly meta ("what question can't I ask yet?").
- **Autonomy scope:** none between turns — the close drives the next turn.
- **Context richness:** continuous-building.
- **Branching:** implicit; the "shape" close can seed parallel exploration.
- **Plan expression:** prose (emerges from the dialog).
- **Knowledge direction:** agent-investigative, meta-level.
- **Review integration:** self-review at close-composition time.

### Mechanism

Enumeration biases question generation toward the *form of question one already has words for*. The counter-hypothesis — consistent with research on framing effects and with Aristotle's aporia as a productive state — is that the most valuable question at the end of a planning turn is often one the agent *hasn't* framed, and a numbered list forces it to either skip the unframed question or list it badly. An open close makes room for "I don't know yet what to ask, and here is the vague shape" — which is often where the load-bearing issue lives.

### Predicted trade-offs

- Human writing cost rises: open-ended closes invite longer, less-targeted human responses.
- Turns take longer; short tasks over-spend.
- Agents without strong meta-cognition produce vague closes that waste turns.
- Loses the "short subsequent human turn" benefit of numbered closes.

### Evaluation plan

Signal: *late-session issue density* — count of issues surfaced in the last 25% of a session that were not anticipated in the first 25%. Open-ended-close sessions should surface more late-session issues *earlier* (in the "shape" moves) than numbered-close sessions. If numbered-close sessions surface late-stage architectural issues at implementation time, that's the counter-hypothesis confirmed.

### Honest failure mode

Collapses when the agent's meta-cognition is weak. A small model using "Name the shape" will produce word salad. Also collapses on well-defined tasks, where "what's the shape of what I can't ask?" is an unnecessary ritual — there genuinely is nothing unframed.

### Honest reassessment

The numbered close's advantage — shortening the human's next turn — is real and observed. The counter-hypothesis concedes this cost in exchange for surfacing deeper issues. A likely synthesis is that numbered closes work when the question space is well-understood and open closes work when it isn't — but the steel-man position here is that open closes dominate, because the case where the space *is* well-understood is the easy case where either close would have worked.

---

## 5. Micro-Step Incrementalism (counter-hypothesis #5: frequent tiny checkpoints beat decision-closure autonomy)

### Core stance

Decision closure trades two bad things for one good: it commits to a path earlier than necessary in exchange for uninterrupted agent execution. Incremental runs reverse the trade: every small step is ratified, misalignments surface within one step, and the cost of a wrong turn is bounded to one step's work. Closure is something that *accumulates* from many ratified micro-steps, not a lever pulled up front.

### Interaction shape

**Open.** Human states task. Agent does *not* propose a plan. Instead, it proposes a *first micro-step*: "I will do X next, estimated 2-5 minutes, and stop."

**Mid-session turn.** Agent executes the micro-step, reports a one-to-three-sentence result with a diff or output, and proposes the next micro-step. Human either accepts (a single word: "go"), redirects, or stops. There is no long run between human touches.

**Close.** The session ends when the agent proposes a micro-step, executes it, and the result is "this completes the task" — validated by the human in one word.

Sample opener (agent, after human states "add a timeout to the reservation API"): "First micro-step: read the reservation handler and return a one-paragraph summary of its current timeout story. 2 minutes. OK?"

Sample mid-session turn (agent): "Micro-step complete. Current reservations have no explicit timeout; the DB connection pool's 30s default is the only bound. Next micro-step: add a `timeout_ms` parameter to `reserve()`, default 5000, threaded to the pool call. 3 minutes. OK?"

### Dimension values

- **Timing of alignment:** continuous, per-micro-step.
- **Decision locus:** interactive at every step; the agent is nearly pre-authorized-within-step but not pre-authorized-across-steps.
- **Dialog form:** short-volley, tightly rhythmic.
- **Question style:** forced-choice-with-default on each step ("OK?").
- **Autonomy scope:** incremental; one micro-step per ratification.
- **Context richness:** continuous-building, each step adds one fact.
- **Branching:** linear; any branch forces a new micro-step proposal.
- **Plan expression:** dialog-log (the plan is the trace of accepted steps).
- **Knowledge direction:** bidirectional but step-local.
- **Review integration:** read-back at each micro-step (the agent reports what it did).

### Mechanism

If each micro-step is 2-5 minutes of agent work, a misalignment costs at most one step. Contrast with decision-closure autonomy, where a misalignment at step 0 is discovered at step N and costs N-1 steps. For tasks where correction cost is low-per-step but discovery-cost-late is high, incrementalism wins. It also reduces context-load: the agent never has to hold a long plan in working memory, only the next micro-step. And it turns the question-of-trust from "did the human bless the plan?" into "did the human bless this step?" — which is a cheaper and more-accurate-per-step decision.

### Predicted trade-offs

- Human attention is consumed continuously: the "do other work while agent runs" mode is lost.
- Overhead per micro-step (propose / confirm) dominates if micro-steps are too small.
- Loses cross-step optimization opportunities — decisions that only make sense in aggregate are hard to surface.
- Fragile under human latency: if the human takes 5 minutes to confirm each step, a 10-step task runs for 50 minutes of wall-clock regardless of agent speed.

### Evaluation plan

Signal: *rework ratio* — work done that was later discarded, divided by total work done. Micro-step protocols should have near-zero rework ratios (each step was confirmed before it was taken). Decision-closure protocols should have higher rework ratios, particularly on tasks where the plan turned out wrong. A secondary signal: human attention is higher per-minute under Micro-Step, but total attention to reach completion may be *lower* if rework avoidance dominates. Compare total attention time across the two protocols.

### Honest failure mode

Collapses when per-micro-step overhead exceeds the information-value of each step — e.g., a mechanical refactor where 40 similar changes need to be made and none of them is a decision point. The "OK?" prompt on each of 40 identical changes is pure tax. Needs an escape hatch into bounded-batch autonomy ("apply the next 10 the same way").

### Distinctness note

This protocol is about *autonomy-stretch length* — how long the agent runs between human touches. It is not about *whether a plan was disclosed at start* (protocol #1) or *how the partition of decisions evolves* (protocol #3). Micro-Step sidesteps plan disclosure entirely by making "the plan" emerge from the ratified step log.

---

## 6. Dialogic Context Accretion (counter-hypothesis #6: dialog-built context beats monolithic brief)

### Core stance

The mental model a human has at the start of a planning session is not the model they have at minute 20. Their own thinking *reveals itself* as the conversation proceeds. A context-dump assumes a fixed up-front model and is structurally unable to accommodate thinking that has not happened yet. The protocol therefore refuses a rich up-front brief and instead *accretes* context across turns, with the agent actively pulling context out as it becomes relevant.

### Interaction shape

**Open.** Human provides a minimal dispatch — one or two sentences. Agent does *not* ask for more context up front. It begins work on the narrowest-possible interpretation and reports.

**Mid-session turn.** Agent's report includes a "context I wish I had" section: one or two small context requests triggered by what it just learned from the narrow work. Human supplies the context. Agent expands interpretation. Repeat.

**Close.** The session ends when the agent's "context I wish I had" is empty for two consecutive turns.

Sample opener (human): "The orchestrator sometimes double-dispatches. Fix it."

Sample mid-session turn (agent, after 3 minutes of reading): "Looked at the dispatch path. Two candidates for double-dispatch: (a) idempotency key is scoped per-worker not per-task, (b) the retry-on-timeout doesn't check if the task already completed. Context I wish I had: (1) is this happening across restarts or only mid-run? (2) have you seen it with any specific task type, or all? Working interpretation pending your answer: (a) is the fix. Will proceed unless you tell me otherwise within a minute."

### Dimension values

- **Timing of alignment:** continuous, context-triggered.
- **Decision locus:** interactive, agent-initiated.
- **Dialog form:** short-volley, agent-led.
- **Question style:** embedded, one or two at a time, context-justified.
- **Autonomy scope:** bounded-by-current-interpretation; expands as context arrives.
- **Context richness:** continuous-building, agent-pulled.
- **Branching:** implicit; alternate interpretations are held by the agent until context resolves them.
- **Plan expression:** dialog-log + structured final summary.
- **Knowledge direction:** agent-investigative — the agent asks what it needs, the human answers on demand.
- **Review integration:** none explicit; the "context I wish I had" is a self-review of own model completeness.

### Mechanism

Three effects support this if the counter-hypothesis is true. (1) Human reveal-during-dialog: the human will notice a caveat at minute 12 that they would not have thought to include in the brief at minute 0. (2) Relevance-filtered context: the human only writes context the agent has asked for, reducing the writing that turns out unused. (3) Evidenced requests: the agent's "I want context X because I just learned Y" builds trust that the request is worth answering, unlike "tell me about this system" before any investigation.

### Predicted trade-offs

- Wall-clock time grows: narrow-first work that turns out to be wrong costs agent-time.
- First few turns produce low-quality output because the agent is working from a too-narrow interpretation.
- For tasks where the human *already has* a rich mental model and wants to hand it over, this protocol wastes that available model.
- Risk: the agent fails to ask for context it doesn't know it needs — blind spots stay blind.

### Evaluation plan

Signal: compare *human writing cost before first useful agent output* across protocols. Context-dump has high before-writing cost and low after-writing cost. Dialogic Accretion has low before-writing cost and distributed after-writing cost — and the hypothesis is that the *sum* of after-writing is lower, because much of the context in a big brief turns out irrelevant. Measure total human characters produced per session and per correction-avoided.

### Honest failure mode

Collapses when the agent is poor at detecting what it doesn't know, and on tasks where the human has a high-value mental model they want to dump and move on from (founding-document tasks). Also collapses if every turn requires a context pull — the human ends up paying handoff-cost on every turn instead of once.

### Distinctness note

This protocol is about *how context enters the session* (distributed, pulled by agent). It is not about plan disclosure (#1), decision-partitioning (#3), or autonomy-stretch length (#5). It can be composed with any of those: Dialogic Accretion + Micro-Step = very high interaction rate; Dialogic Accretion + Example-Led = agent pulls context to justify the next probe example.

---

## 7. Question-Preserving Autonomy (counter-hypothesis #7: removing questions produces more corrections later)

### Core stance

Autonomy is not the absence of questions; it is the absence of *interruption*. An agent running autonomously can still collect questions and surface them at the right moment. Removing questions entirely trades a small up-front attention saving for false confidence: the agent proceeds past ambiguities it should have flagged, and the corrections arrive as implementation diffs rather than as question-and-answer. The protocol preserves questions but *queues and batches* them instead of firing them at the human immediately.

### Interaction shape

**Open.** Human dispatches autonomous work with a *question queue protocol* declared: "Work autonomously. Collect questions into the queue. Surface the queue when you hit a checkpoint (every N minutes, or when you feel confidence-drop on a decision)."

**Mid-session turn (agent-to-self).** Agent works. When it encounters ambiguity, it *writes the question* to a visible queue file and picks the most-confident next-step that is compatible with the widest range of answers. It does not block; it keeps moving, but it records the question.

**Checkpoint turn.** At the checkpoint, agent surfaces the queue with each question annotated with (a) what it did instead of asking, (b) whether a different answer would require rework, and (c) how much rework. Human responds to the queue in one pass.

**Close.** Session ends when the queue is resolved and the work is done. Any remaining low-rework-cost questions may be deferred to post-implementation review.

Sample queue entry: "Q: Should reservation timeout default to 5s or 30s? What I did: went with 5s, conservative, easy to bump. Rework cost if wrong: one-line change, no ripples. Confidence this is fine: high."

Sample queue entry (harder): "Q: When a worker crashes with an active reservation, do we reclaim immediately or wait for TTL? What I did: TTL wait, 60s default. Rework cost if wrong: medium — reclaim path shares code with a branch I just wrote; flipping requires re-sketching the branch. Confidence: medium, please confirm before I continue past step 4."

### Dimension values

- **Timing of alignment:** hybrid — autonomous execution + checkpoint alignment.
- **Decision locus:** pre-authorized-within-envelope; questions are logged and surfaced rather than asked live.
- **Dialog form:** long-message at checkpoint; no short turns during autonomy.
- **Question style:** batched-at-checkpoint, annotated with what-the-agent-did-and-rework-cost.
- **Autonomy scope:** bounded-by-question-rework-cost (the agent keeps going past a question *only if* the rework cost of a wrong answer is low).
- **Context richness:** rich brief at dispatch + queue-based pulls at checkpoint.
- **Branching:** agent-tracked via queue.
- **Plan expression:** structured (the queue + resolution log is an artifact).
- **Knowledge direction:** agent-investigative, deferred.
- **Review integration:** self-review when writing queue entries (agent rates its own confidence and rework cost).

### Mechanism

The counter-hypothesis's claim is that "don't ask questions" produces more-but-later corrections. The mechanism: agents encounter human-frame ambiguities (naming, intent, scope) that no amount of code inspection resolves, and forced to "not ask" they resolve them in the most-plausible-looking but-often-wrong direction. Question-Preserving Autonomy preserves the ambiguity-detection signal (the agent notices the question) and sacrifices only the interrupt-immediately behavior. This is the same move military mission command makes: bounded autonomy + back-brief at planned moments. The annotation (what-I-did, rework-cost) gives the human a triage signal: low-rework-cost low-confidence questions can be left, high-rework-cost low-confidence questions block.

### Predicted trade-offs

- Cost of writing queue entries eats some of the autonomy gain.
- Requires the agent to be calibrated on rework-cost — miscalibration is poison (high-cost question logged as low-cost leads to deep rework).
- Long runs between checkpoints accumulate large queues; the checkpoint turn becomes expensive to read.
- Depends on the agent having good *confidence* introspection, which is a known weak spot for LLMs.

### Evaluation plan

Signal: *late corrections per session*, where "late" means after the first autonomous run terminates. "Don't ask questions" autonomy should produce high late-correction counts on any task with human-frame ambiguity. Question-Preserving Autonomy should push those corrections into the checkpoint turn — visible as queue-entries-resolved. If queue-entries-resolved roughly equals late-corrections-in-the-no-questions-protocol on the same task, the counter-hypothesis is confirmed: the questions were always there; only their timing changed.

### Honest failure mode

Collapses when the agent cannot self-assess confidence — it either logs everything (queue blows up) or nothing (silent failures return). Also collapses on tasks with truly no ambiguity (a question-preserving protocol has empty queues, and the overhead is pure tax).

---

## 8. Knowledge-State Mapping (counter-hypothesis #8: form is epiphenomenal; knowledge-mismatch is the driver)

### Core stance

The apparent effects of protocol form (numbered vs open, batched vs one-at-a-time, short vs long) are surface symptoms of an underlying cause: a *knowledge-state mismatch* between human and agent on specific, nameable concepts. Fix the mismatch directly and the form choice largely ceases to matter. The protocol is not a dialog shape but a *shared-model-maintenance protocol*.

### Interaction shape

**Open.** Session opens with a brief *knowledge-state inventory*: agent lists the five to ten concepts most load-bearing for the task, with its current reading of each. Human marks each entry: ✓ (shared), ! (wrong, here's the fix), ? (I don't know either), + (missing concept, let me add).

**Mid-session turn.** Before any design move, agent checks its move against the inventory: "this move depends on concept K; my reading of K is {...}; human-ratified status: ✓". If a move requires a concept not in the inventory, the agent *adds* the concept, with its reading, and flags it for ratification before proceeding. Form of subsequent dialog (short-volley, narrative, whatever) is explicitly free — the human picks whatever form they prefer for the *current exchange*, and the agent adapts.

**Close.** Session ends when the inventory has no unresolved entries and every planned action maps to ratified concepts.

Sample opener (agent): "Inventory for 'add reservation timeout' task.
- **reservation**: an in-memory lock on a task, held by a worker, expiring on TTL.
- **TTL**: default 60s, renewed by heartbeat.
- **timeout**: new concept — the *operation* timeout on reserve(), distinct from reservation TTL.
- **idempotency key**: scoped per-task.
- **worker crash**: reservation reclaimed via TTL expiry, not via heartbeat-loss push.

Mark each ✓ / ! / ? / +."

### Dimension values

- **Timing of alignment:** continuous, concept-anchored.
- **Decision locus:** interactive on concepts, mixed on moves (autonomous if concept is ✓).
- **Dialog form:** shape-shifting — the inventory is structured, but between-inventory exchanges use whatever form suits.
- **Question style:** inventory items are forced-choice-with-default on each concept; in-dialog questions are unrestricted.
- **Autonomy scope:** bounded-by-inventory-ratification.
- **Context richness:** structured via inventory; low ambient prose cost.
- **Branching:** agent-tracked against inventory entries.
- **Plan expression:** structured inventory + free-form moves.
- **Knowledge direction:** bidirectional, concept-centric.
- **Review integration:** implicit self-review against the inventory on every move.

### Mechanism

The form-vs-content finding in Phase 1 may be mistaking *covariates* for *causes*: sessions where the agent's and human's concept models are aligned happen to use certain forms; sessions where they aren't use others. If form is epiphenomenal, directly tracking the concept-alignment state should produce the same outcomes regardless of which form the dialog takes. The inventory is the direct instrument. This is the same move medical handoffs make with read-back — the content is the aligned model of the patient's state, not the shape of the verbal protocol.

### Predicted trade-offs

- Heavy cognitive cost at session open: building and ratifying the inventory is work.
- Weakest for tasks where the load-bearing concepts can't be enumerated up front (exploratory sessions).
- Freedom-of-form mid-session can devolve if the human picks a bad form for a given exchange.
- Inventory maintenance drift: if the agent stops updating the inventory as new concepts arise, the protocol silently degrades.

### Evaluation plan

Signal: *protocol-form variation across sessions that succeeded* — if form is epiphenomenal, successful Knowledge-State-Mapping sessions should exhibit wide variation in form (short-volley, long-message, numbered, open) while exhibiting uniform high inventory-ratification rates. Compare to observed sessions without explicit inventory: form variance in successful sessions should be *lower*, because the observed successes cluster around particular forms. High inventory-ratification rate + form variance = counter-hypothesis confirmed.

### Honest failure mode

Collapses on tasks where the concept set is emergent and can't be inventoried up front (early-phase exploration, research tasks). Also collapses if the human resists inventories as overhead on short tasks. And: if the counter-hypothesis is wrong — if form *does* have independent effects — the inventory fixes part of the problem but leaves the form-driven part unfixed, and the protocol will under-perform relative to form-specific observed patterns.

### Honest reassessment

Of the eight counter-hypotheses, this is the one where steel-manning most clearly exposes a likely partial-truth rather than full-truth. Knowledge-state mismatch almost certainly *is* a major driver — but Phase 1's form-specific findings (e.g., numbered close shortens the next human turn) are hard to explain as pure epiphenomena of concept mismatch. The likely synthesis is that form effects are *conditional on* the concept-alignment state: when the inventory is well-aligned, form may matter less; when it's mis-aligned, form matters a lot. The steel-manned protocol above assumes the strong form of the counter-hypothesis for the discipline of Step 3; the reviewer pass in Step 5 should probe whether the strong form survives.

---

## Cross-cutting observations

### Overlap between counter-protocols

- **Protocols #1 (Example-Led), #3 (Emergent-Partition), #5 (Micro-Step), and #6 (Dialogic Accretion)** all defer some form of up-front commitment — but along different axes. #1 defers *plan articulation*, #3 defers *decision-class rules*, #5 defers *autonomy-stretch authorization*, and #6 defers *context loading*. A session could in principle combine two or more (e.g., Micro-Step + Dialogic Accretion = extreme incrementalism with context pulled at each step). For Step 4's unified catalog, these are distinct primitives that compose.
- **Protocols #2 (Assumption-Bundle) and #7 (Question-Preserving Autonomy)** both batch questions, but in opposite temporal positions: #2 batches *before* execution (pre-action bundle), #7 batches *during* execution (post-hoc queue). The batching mechanism is shared; the timing is what separates them.
- **Protocol #4 (Open-Ended Hand-Off)** is a local-turn-level choice and composes with any of the others — any of the seven could be run with an open-ended close at each agent turn.
- **Protocol #8 (Knowledge-State Mapping)** is meta to the others: it claims form is epiphenomenal. If true, most of the other seven would collapse to the same underlying mechanism. Step 5 should probe #8 before ranking the rest.

### External-source analogs to note

Several of these counter-protocols have near-matches in Step 2's external-source catalog, which supports the steel-man position:

- **Counter-protocol #1 (Example-Led Emergence)** closely resembles **Maieutic Draw-Out** from `socratic-method.md` — Socrates' technique of drawing knowledge out through specific cases rather than stating general claims. If Maieutic Draw-Out is a substantial external-source candidate on its own, Step 4 should consider whether #1 and Maieutic Draw-Out are two names for the same protocol and merge them, noting the convergence as evidence.
- **Counter-protocol #2 (Assumption-Bundle Disclosure)** resembles **MECE decomposition** and **Issue Tree** from `consulting-discovery.md`, and the **Assessment / Recommendation** block of **SBAR** in `medical-handoffs.md` — all of which batch linked considerations and present them as a structured graph-like object.
- **Counter-protocol #3 (Emergent-Partition)** has no clean external analog; it is close to **Incident Action Plan re-issuance per operational period** from `incident-command.md`, where the partition (what's in-plan vs under-investigation) is explicitly revisited each period rather than frozen.
- **Counter-protocol #4 (Open-Ended Hand-Off)** resembles **Aporia as graceful-stop signal** in `socratic-method.md` and **OARS open-question reflection** in `therapy-intake.md`.
- **Counter-protocol #5 (Micro-Step Incrementalism)** is close to **Ping-Pong Pairing** in `pair-programming.md` and to the **Fragmentary Order** tier in `military-briefings.md`.
- **Counter-protocol #6 (Dialogic Context Accretion)** is close to **Elicit-Provide-Elicit (EPE)** in `therapy-intake.md` and to **SPIN sequencing** in `consulting-discovery.md`.
- **Counter-protocol #7 (Question-Preserving Autonomy)** has a direct analog in **Mission Command doctrine + Back-Brief** in `military-briefings.md`: bounded autonomy with planned checkpoint, not question-free execution.
- **Counter-protocol #8 (Knowledge-State Mapping)** resembles **I-PASS**'s "Situation Awareness & Contingencies" layer in `medical-handoffs.md` and **Pre-phase briefing** in `pilot-controller.md` — both enforce an explicit shared-model check independent of the form of subsequent dialog.

### Implications for Step 4

The convergence between counter-protocols (derived from inverting Phase 1 findings) and external-source candidates (derived from domain mining) is a useful signal. Where the two converge on the same protocol primitive, Step 4 should consolidate under one name and note the dual derivation. Where the counter-protocols have *no* external analog (e.g., #3 Emergent-Partition), they are novel contributions and should be catalogued as such — novelty here does not mean they're weak, but they carry more evaluation burden in Step 5 because there is no external empirical track record to draw on.

### Note on steel-manning discipline

Two protocols above close with "honest reassessment" notes (#4 Open-Ended Hand-Off, #8 Knowledge-State Mapping) where the design exercise surfaced specific reasons the counter-hypothesis may be weaker than its framing. The notes are kept *after* the steel-manned design, not as a replacement for it — Step 5 will weigh both.
