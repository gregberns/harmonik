# Analysis Lens: Topic Tree

Generated: 2026-04-23

## What this lens looks for

The topic tree lens reconstructs what branches of inquiry were raised during a planning session, tracks whether each was pursued, parked, or dropped, identifies who introduced new branches (human vs agent), and looks for patterns in how the conversation manages topic proliferation. The underlying question: is there a reusable shape for topic navigation that keeps conversations focused without losing important branches?

## Methodology

Five sessions analyzed in depth: 38415843 (kerf), 79a42399 (secure-dev), c6d1bd16 (secure-dev), 3bf5774c (harmonik), f588ff0c (harmonik). Turn references cite `Human #N` or `Agent #N` as labeled in the corpus extracts.

Topic status annotations:
- **PURSUED** — substantively addressed in the session
- **PARKED** — explicitly deferred, with a signal to return
- **DROPPED** — introduced then never addressed; may surface in a future session
- **RESOLVED** — closed with a decision

---

## Session-level topic reconstructions

### 38415843 — kerf spec bootstrap

Opening intent: turn docs into specs, set up agent configuration, determine spec structure.

- **Spec structure design** (H#1 → A#1 → H#2 → A#2 → H#3) — RESOLVED. Three rounds of narrowing: docs→specs split, file count, format conventions. Agent introduced sub-questions (one file vs per-command); human deferred most to agent judgment.
  - Sub-branch: AGENTS.md / CLAUDE.md enforcement — RESOLVED (A#3–H#4)
  - Sub-branch: CODE_CONVENTIONS.md — PARKED by human ("once we get into it", H#4); never recovered in this session.
- **Task tracking / TASKS.md** (H#5) — RESOLVED. Human introduced this mid-session as a prerequisite; agent folded it into the plan quickly.
- **ntm for spec writing** (H#5) — PURSUED heavily (H#5 → A#11). Human explicitly introduced this branch to start practicing multi-agent dispatch. Absorbed turns 5–17.
  - Sub-branch: agent mail for completion notification (A#12) — PURSUED and RESOLVED (A#13–H#17). Human explicitly elevated it at H#16: "Agent mail is the right path. Our objective is to hone our process right now."
  - Sub-branch: pane capture reliability (H#22) — PURSUED and partially RESOLVED. Human introduced after recurring misdiagnosis by agent. Linked to a practical failure pattern.
  - Sub-branch: ntm respawn vs kill-and-add (A#20) — RESOLVED incidentally during investigation.
- **Spec consistency review** (H#18) — RESOLVED. Agent proposed overlapping review clusters; executed successfully.
- **Beads / task decomposition for implementation** (H#18) — PARKED. Human acknowledged it: "Then we figure out beads/tasks." Explicitly deferred to "a good point to restart the session." Not recovered in session.

**Branch introduction:** human introduced 3 major branches (TASKS.md, ntm practice, restart discussion); agent introduced sub-branches (agent mail elaboration, pane capture investigation). Human's branch introductions were always scoping or process shifts. Agent's were mostly elaborations within an established branch.

**Disproportionate time:** agent mail + pane capture absorbed ~30% of the session. High value — this was exactly the human's stated goal ("hone our process"). Not a distraction.

**Fork pattern at H#5:** human interrupted spec-writing momentum with "could we spawn ntm sessions instead of sub-agents?" This is a deliberate human-initiated fork: pausing a productive path to establish a better process. Agent adapted without complaint and without losing track of where spec-writing stood.

---

### 79a42399 — secure-dev exploratory testing setup

Opening intent: design an agentic testing loop; figure out how to deploy agents to validate what works.

- **Testing loop design — 3 layers** (A#1) — RESOLVED quickly (H#2 approved all three).
  - Layer 1 (test infrastructure) — PARKED by human as "in progress"; returned only implicitly.
  - Layer 2 (exploratory agents) — PURSUED.
  - Layer 3 (fix loop) — PARKED explicitly by human ("put that on the todo list for later", H#2). Not recovered in session.
- **ntm research** (H#2) — PURSUED via sub-agent dispatch. Agent immediately forked off a background research task and continued design in parallel. Research returned at H#3 (task notification).
- **Build/environment isolation** (A#2) — PURSUED. Agent introduced this proactively as a prerequisite; human accepted.
- **What to test right now vs with backend** (A#2) — PURSUED. Agent decomposed scope into "no backend needed" vs "needs backend" categories. Human approved structure.
- **Reporting format** (A#2) — RESOLVED (H#4: "both" — report file + bd issues).
- **Agent prompt structure** (A#2) — RESOLVED (H#4: whatever agent thinks).
- **Agent count and mission assignment** (A#3) — RESOLVED (H#4: 3 agents, start simple).
- **Test mode variants** (H#5) — PARKED explicitly by human: "Lets add that to a task list we can take a look at later." Signal: "put it on the list, don't discuss now." Immediately followed by "once thats done, lets see what we can do to start up and do a dry run." Clean fork management.
- **Marching orders format limitation** (A#4/A#5) — RESOLVED incidentally during dry run.
- **Wrong pane dispatch** (H#7) — RESOLVED. Human introduced this as a problem; agent diagnosed and fixed within the same turn.

**Branch introduction pattern:** agent introduced most branches — build isolation, test categorization, reporting options, prompt structure. Human introduced only two major branches: test mode variants (H#5) and the dry-run-first caution (H#6). Both were scope-constraining moves, not expansions.

**Clean parking:** H#5's "put that on the task list for later" is the clearest example in the corpus of efficient branch parking. The human introduced a new branch AND immediately parked it, with no ambiguity. No recovery cost in session.

**Disproportionate time:** the dry-run debugging (H#6 → H#7 → A#7) took 3 turns for a mechanical fix. Low value relative to time, but unavoidable.

---

### c6d1bd16 — secure-dev in-memory + Docker backend

Opening intent: figure out lightweight testing, then scope implementation.

- **Exploratory testing scope** (H#1 → A#1) — PURSUED, then REDIRECTED by human. Agent proposed options A/B/C; human approved "both." But then immediately redirected at H#2.
- **Dev workflow formalization** (H#2) — INTRODUCED by human as a prerequisite gate. Absorbed a full autonomous agent run (A#2). This was a process branch, not a content branch. Human explicitly said: "Before anything else, lets get that set of steps defined and formalized in a skill."
  - Sub-branch: skill format and structure — RESOLVED autonomously.
  - Sub-branch: mandatory review gate — RESOLVED autonomously.
- **Scope alignment check** (H#3) — INTRODUCED by human. "Before we start, clearly define what you think we are trying to accomplish." Classic realignment move — human paused autonomous momentum to verify mental model alignment. Agent responded with a clean summary (A#3); human confirmed and narrowed at H#4 ("take out the exploratory testing").
- **Spec writing** (A#4 → H#5 → A#5) — MAJOR BRANCH. Absorbed turns 4–5 plus a 19-minute autonomous run. Human intervened at H#5 to update the dev-workflow skill (a process correction mid-execution), then told agent to continue without stopping.
  - Sub-branch: spec review (3-agent parallel) — PURSUED within autonomous run.
  - Sub-branch: plan writing — PURSUED within autonomous run.
  - Sub-branch: plan review (3-agent parallel) — PURSUED within autonomous run.
  - Sub-branch: bead creation — PURSUED within autonomous run.
- **Orchestrator mode** (H#6) — "Act as the orchestrator and coordinator. Delegate all work. Begin." This closed the planning phase entirely and transitioned to execution. One sentence from the human; switched session mode completely.

**Fork pattern at H#2:** human interrupted a content discussion with a process requirement ("we need to formalize the workflow before doing any work"). This is a meta-level branch that suspended content progress. Expensive in terms of turns, but the human correctly predicted it would be needed repeatedly.

**Fork pattern at H#3:** after the process branch resolved, human asked for alignment check before execution. This two-step (process first, align second, then execute) adds upfront cost but prevents the "agent is 20% into the wrong plan" recovery cost.

**Agent autonomy:** the longest autonomous run (A#5, 19 minutes, 74 tool calls) executed phases 3–7 of the workflow without checking in. Human had explicitly pre-authorized this ("DO NOT IMPLEMENT until instructed"). The gate was content (don't implement), not process (stop and ask).

---

### 3bf5774c — harmonik state source of truth

Opening intent: resolve open discussion threads, starting with state source of truth (#15/#4).

- **Thread queue management** (A#1) — RESOLVED quickly. Agent recreated prior session's task list from session handoff; framed 4 open threads.
- **L1 git-as-truth** (A#1 → H#2) — RESOLVED in one human turn. Human confirmed with nuance (reconciliation duty added). Agent correctly flagged this as quick-confirm territory.
- **L3: "what is a feature"** (A#1 → H#2 → A#2) — REFRAMED by human. Human introduced composability angle, Gas Town reference, and explicitly dropped the "feature" term. Agent correctly captured and reparked the real question as "composition vocabulary" (new task #5).
- **L1/L2 labels as jargon** (H#2) — INTRODUCED as a complaint. Human pointed out the agent was using internal labels without context. Agent absorbed this as a working-style correction and updated memory.
- **Commit-per-node (#1)** (A#2 → H#3 → A#3) — PURSUED across turns 2–5. Three passes: agent framed the question, human revealed knowledge gaps (checkpoint definition, trailers), agent corrected and refined. Substantial time.
  - Sub-branch: what is a checkpoint — INTRODUCED by human (H#3). Fundamental clarification need. Absorbed most of turn 3–4.
  - Sub-branch: commit trailers (H#3) — INTRODUCED by human as "I've never heard this term." Immediate clarification needed.
  - Sub-branch: failure checkpoints — INTRODUCED by agent (A#2). PARKED by human consensus at A#3 ("don't commit on failure; revisit if consumer surfaces").
- **Three stores / SQLite role** (H#4 → H#5) — INTRODUCED by human mid-thread. Human noticed a contradiction: "Beads (SQLite) — not a truth store?" Triggered a correction of the agent's imprecise framing. Absorbed significant time in H#4/H#5.
- **JSONL contradiction** (H#5) — INTRODUCED by human. "WAIT — you just said JSONL wasn't used." Human caught a genuine inconsistency in the agent's model. This is a high-value fork: the human's close reading found a real design hole.
- **Docs vs memory vs spec** (H#5) — INTRODUCED by human. "You updated a memory — but don't we want to be updating the spec?" Process question, not content.
- **Reconciliation agent** (H#5) — INTRODUCED by human ("When there's a disagreement, what is done? Is an agent going to investigate?"). PARKED by agent as "candidate role; capturing principle." Not resolved.
- **Process lifecycle gap** (H#5) — INTRODUCED by human as a broad framing question: "How does Harmonik start and stop?" Major new branch. Agent identified it as a real gap (A#5) and elevated it to #7. PURSUED in H#7 → A#6.
  - Sub-branch: per-project vs user-scoped daemon — RESOLVED in A#6 (per-project).
  - Sub-branch: attachable UI (S4) — PURSUED and folded into candidate weave.
  - Sub-branch: tmux-paired runner (H#7) — INTRODUCED by human. PURSUED and synthesized.
  - Sub-branch: orchestrator-agent vs daemon distinction — INTRODUCED by agent (A#6). High-value; human had been conflating the two.
- **Composition vocabulary** (A#4/A#5) — PARKED by human (H#7): "I'd like to delay this if we can. The terminology isn't critical."

**Branch introduction pattern:** heavily human-driven for major branches. All significant new branches (L3 reframing, SQLite correction, JSONL contradiction, docs-vs-spec, reconciliation, lifecycle gap) came from the human. Agent introduced sub-branches and sub-questions within established topics.

**High-value parkings:** failure checkpoints (parked cleanly), composition vocabulary (parked cleanly by human at H#7). Both were correct efficiency moves.

**Costly branches:** "what is a checkpoint" and "what are trailers" — both consumed significant turns to resolve terminology, not architecture. These are knowledge-gap branches. If the agent had surfaced an explicit glossary earlier, these branches might not have appeared.

**Disproportionate time:** the three-stores/JSONL contradiction cluster (H#4–H#5) absorbed ~3 human turns of dense clarification. High value — this found real design imprecision. But the imprecision was the agent's.

---

### f588ff0c — harmonik foundation kerf work

Opening intent: get caught up, figure out next steps, start building the plan.

- **Roadmap / process document** (A#1) — INTRODUCED by agent. Immediately REDIRECTED by human (H#2): "run the 'kerf' command real quick." Classic human branch interruption to introduce a better tool.
- **Kerf as the process** (H#2 → A#2) — PURSUED. Agent immediately recognized kerf collapsed most of what it was going to propose. Efficient pivot.
- **Spec-first vs plan-first jig** (A#2 → H#3) — RESOLVED by human (H#3): "I'd like this project Spec-First." Clean decision.
- **Scope of first work: one big vs multiple** (A#2) — RESOLVED by human deferral at H#3: "I don't have a strong lean — whatever you think is best." Agent picked option (c), one umbrella + per-subsystem.
- **Foundation questions framed by content not labels** (H#4/H#5) — INTRODUCED by human as meta-feedback. "You're asking me what should be in foundation... I don't know what you're talking about if you're listing file names." Agent acknowledged and rewrote in content form (A#4).
  - This branch consumed 2 turns but permanently changed the agent's communication mode.
- **Decision authority** (H#4/H#5, H#6) — INTRODUCED by human twice. "I don't need to dictate everything." Then: "If these are straightforward decisions, I don't need to be involved." Human explicitly expanded agent's autonomy to prevent micro-decision fatigue.
- **Reviewer agent quantity and personas** (H#6) — INTRODUCED by human: "even more passes with review agents." Expanded existing review discipline.
- **Node definition from Kilroy/Attractor** (H#6) — INTRODUCED by human as a research pointer: "consult Kilroy and the Attractor spec." DROPPED as an explicit branch — absorbed into research sub-agent work autonomously.
- **Non-functional requirements** (H#6) — INTRODUCED by human: "we may want to start thinking about non-functional requirements — what are we missing?" Phrased as an open question, not a task. PARKED; not explicitly recovered but potentially absorbed by recon agents.
- **Autonomous overnight push** (H#6 onward) — NOT a branch per se, but a mode shift. Human said "I'm going to bed... just go." This converted all remaining branches into autonomous delegation rather than dialog.
  - Sub-branches: 7 research sub-agents (event model, workspace model, handler contract, node types, failure taxonomy, control-points, composition) — PURSUED autonomously. Not topic-tree branches in the conversational sense; agent managed the topic tree internally.
- **Morning review of results** (H#9+ with task notifications) — PURSUED. Several turns of result ingestion.

**Branch introduction pattern:** human introduced 4 meta-level branches (kerf redirect, foundation-content-not-labels, decision authority, non-functional requirements). Agent introduced the roadmap document branch. The human's meta-branches were all process corrections that shaped how subsequent work would be done.

**Most valuable human intervention:** the "file names vs content" correction at H#4/H#5. One feedback turn that changed the agent's question-framing behavior for the rest of the session.

**Overnight autonomous run:** after H#6, the topic tree management transferred entirely to the agent. The agent managed ~7 parallel research branches, 2 rounds of review per branch, revisions, and status tracking — all without human check-ins. The human re-entered at the review stage to evaluate results. This is the highest-autonomy topic-tree shape in the corpus.

---

## Cross-cutting findings

### F1: Agent introduces content branches; human introduces scope and process branches

Across all sessions, agents generated most of the content sub-branches (option A vs B vs C, which spec files to create, what fields a schema needs). Humans introduced branches that changed the scope, redirected to a better process, or paused to verify alignment. The types differ qualitatively:

- Agent branches: elaborations, sub-options within an established topic.
- Human branches: forks that changed what topic was being pursued.

This asymmetry has an implication for planning protocols: if the agent could anticipate which human-type branches are likely (scope check, process correction, alignment verification), it could surface them proactively before the human has to interrupt.

### F2: Parking is mostly efficient, but knowledge-gap branches are expensive

Most parked topics were correctly parked: "Layer 3 fix loop" (79a42399, H#2), "test mode variants" (79a42399, H#5), "beads/task decomposition" (38415843, H#18), "composition vocabulary" (3bf5774c, H#7), "failure checkpoints" (3bf5774c, A#3). All were parked cleanly with a signal, and none incurred recovery cost in-session.

The expensive branches were knowledge-gap branches — cases where the agent used a term the human didn't know ("checkpoint," "trailer," "foundation") without defining it first. These branches didn't represent new topics; they were forks caused by an unshared vocabulary. Each cost 1–3 turns of clarification before the actual discussion could resume.

Pattern: **vocabulary branches are the most recoverable form of wasted turns.** A proactive glossary or "shall I define this term?" convention could eliminate most of them.

### F3: The "alignment check before execution" pattern

Three sessions show a consistent pattern: human pauses momentum to request a scope alignment check before the agent executes:

- c6d1bd16 H#3: "Before we start, clearly define what you think we are trying to accomplish."
- f588ff0c H#4/H#5: "You're asking me what should be in foundation — I don't know what you're talking about."
- 3bf5774c H#2: implicit, via the human correcting the agent's "locked decision" framing.

In each case, the check uncovered a real misalignment (scope too broad, jargon opaque, or framing too rigid). The cost was 1–2 turns; the benefit was avoiding 10–20 turns of correction later.

**Protocol candidate:** agent should offer a one-paragraph "here is my current understanding of what we are doing" before starting any significant autonomous run. Human confirms or corrects in one turn.

### F4: Who manages the topic tree — and does the agent make it visible?

In most sessions, the agent tracked the topic tree implicitly but did not make it visible. The human had no window into what was being held in reserve. Two exceptions where the agent explicitly tracked and surfaced the tree:

- 3bf5774c A#1: agent recreated the prior session's task list from the handoff document and surfaced 4 open threads. Clean.
- 3bf5774c A#4 and A#5: agent closed threads and proposed next by number. Partially clear.

In other sessions, the agent simply moved from topic to topic without signaling what it was holding or why. When branches multiplied (c6d1bd16 turn 5, f588ff0c before the overnight push), the conversation became denser, not more structured.

**Pattern:** explicit topic tree management (numbered threads, "closing #1, moving to #2") lowers the human's context-maintenance cost per topic. When the agent manages the tree silently, the human must either trust the agent or re-ask "what else is open?"

### F5: Human branch parking is fast and final; agent branch parking is sometimes lost

When the human parks a branch, the signal is explicit: "put that on a task list," "let's delay this," "that's secondary." These are never ambiguous and are almost always honored.

When the agent parks a branch (e.g., "we can revisit this if a concrete use case surfaces"), recovery is uncertain. In 3bf5774c, the reconciliation-agent branch (H#5) was parked by the agent without a numbered task or explicit tracking mechanism. It does not appear to have been recovered in-session, and it is substantive enough that loss is a cost.

**Protocol candidate:** agent-parked items should be placed in a visible tracker (e.g., TASKS.md or an open-questions list), not just mentioned as "parked." The tracker is the branch's recovery mechanism.

### F6: When branches multiply, the most expensive sessions stay focused through explicit sequencing

f588ff0c had the highest branch multiplicity (7 parallel research branches, plus ~5 meta branches). The session avoided scatter through two mechanisms: the human delegated all sub-branch management to the agent (H#6 "just go"), and the kerf jig imposed an external structure (problem-space → decompose → research). The human only reviewed outputs at pass transitions.

Conversely, 3bf5774c had moderate branch count but higher scatter because each branch required human input at multiple points within it. The human's knowledge gaps (checkpoint, trailers) created within-branch forks that interrupted flow.

**Pattern:** a structured jig or pass-based protocol offloads topic-tree management from the conversation to an external artifact. This reduces the human's attention cost per branch, but only works if the human trusts the structure enough to delegate.

---

## Candidate planning protocols this lens suggests

**CP-1: Proactive vocabulary check.** Before introducing a term-of-art (checkpoint, trailer, foundation, jig), agent offers a one-sentence definition or asks "familiar with this term?" Eliminates vocabulary branches.

**CP-2: Pre-execution scope summary.** Before any significant autonomous run, agent offers a one-paragraph scope statement: "I'm about to do X, scoped to Y, not covering Z." Human confirms or corrects in one turn. Collapses the alignment-check branch.

**CP-3: Visible numbered topic tracker.** Agent maintains an explicit numbered list of open branches in the conversation (or a shared file). When parking a branch, cites the number and says where it is stored. Recoveries are explicit ("returning to #3 from earlier"). Reduces human's context-maintenance load.

**CP-4: Human-initiated scope gates.** The pattern of "before we continue, tell me what you think we're doing" (c6d1bd16 H#3, f588ff0c H#4/H#5) is reliable and cheap. Codifying this as a periodic check-in (e.g., at the start of each major phase) would surface misalignments before they become corrections.

**CP-5: Jig-as-topic-tree-manager.** When branch count is expected to be high, external structure (kerf jig, named passes) can hold the topic tree so the conversation holds only the current branch. The human reviews at pass boundaries, not at every sub-branch. Optimal for large, well-scoped works.

---

## Open questions

1. **Does explicit numbered topic tracking save time, or does it add friction?** The 3bf5774c numbered-thread approach seemed effective, but the thread numbers disappeared in practice (H#7 stopped referring to them). Would a lighter signal work as well?

2. **Vocabulary branches: could a session-opening shared glossary eliminate them?** Or does the agent need to know in advance which terms are likely to be unfamiliar to this particular human?

3. **When the human parks a branch by saying "put it on the task list," does the agent reliably do so?** In 38415843, the human's "beads/tasks for later" was parked but not explicitly tracked within the session. Check whether these become recovery costs in subsequent sessions.

4. **The overnight autonomous run (f588ff0c) is the highest-autonomy shape in the corpus.** Is it replicable? What preconditions must hold? (Rich handoff document, structured jig, explicit "just go" authorization, no waiting on human for sub-decisions.) Phase 2 could test these preconditions systematically.

---

## Notes on variants

- **13493c8d (harmonik context-dump):** Not analyzed in depth. Topic tree structure would look very different — most branches are introduced in the opening message rather than emerging in dialog. Likely to show fewer in-session corrections but also fewer human-driven scope checks.
- **729dad16 (kerf session recovery):** The session-recovery format implies the topic tree was already established in a prior session. The opening message carries forward the prior tree explicitly. Recovery cost visible in first few turns.
- **00eb9fc9 (harmonik):** Not analyzed. Would likely show similar patterns to 3bf5774c given same project context.
