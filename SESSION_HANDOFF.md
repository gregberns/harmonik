# Session Handoff

> Message to the next agent picking up this project. Overwritten at each session boundary; git history preserves prior handoffs.
>
> Written 2026-04-21.

## Read this first

1. **`CLAUDE.md`** — agent instructions, kerf workflow guidance, knowledge-base reading order.
2. **`STATUS.md`** — project state; note the new decisions 11–14 added to the top of §Decisions before the original ten from 2026-04-19.
3. **`QUESTIONS.md`** — decision items and their status. §Resolved at the bottom now contains the 2026-04-20/21 decisions with full reasoning.
4. **`TASKS.md`** — Phase 0 work + backlog. New "Beads integration" section surfaced 2026-04-21.
5. **`OVERNIGHT_RUN_2026-04-19.md`** — narrative of the 2026-04-19 autonomous run (produced the foundation-work problem-space + decompose + research).

## What's decided as of 2026-04-21

Beyond the original ten decisions:

- **Workflow format: DOT.** Graphviz-renderable. Policies embed as DOT node/edge attributes referencing YAML policy documents.
- **DTW not adopted.** Harmonik uses JSONL events + git checkpoints + SQLite queue + deterministic restart reconciliation. Git history = source of truth for completion; queue is a cache.
- **Beads is the task ledger.** Specifically `github.com/Dicklesworthstone/beads_rust` (SQLite-backed; NOT the Dolt-backed fork). Harmonik is the workflow engine on top. Interaction via `br` CLI only — NOT Beads's MCP server. Agents get a Beads-CLI skill via the handler contract.
- **Workflow-state split.** Harmonik owns fine-grained workflow state in its event log. Beads sees only terminal transitions (claim / close / reopen).
- **Bead IDs in harmonik tracking.** Run metadata, checkpoint commit trailers (`Harmonik-Bead-ID`), and event payloads carry the bead ID when a run is tied to a bead.
- **Handler contract owns skill injection.** New foundation-amendment obligation: handlers ensure their agent process has the workflow-required skills/tools.

## Where the foundation kerf work stands

`harmonik-foundation` kerf work is at `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/`. Passes completed:

- Problem space — converged round 3
- Decompose (7 components) — converged round 2
- Research (7 parallel sub-agents) — complete; synthesis at `03-research/SYNTHESIS.md`

**Not yet done:** change-design, spec-draft, integration, tasks. These should proceed but the foundation will need some updates to absorb the 2026-04-20/21 decisions before advancing:
- Execution-model component must reflect DOT as the workflow format (lock in among the research pass's "workflow format TBD" options — Option DOT).
- Execution-model's checkpoint schema needs `Harmonik-Bead-ID` trailer (foundation amendment).
- A new component (or addition to an existing one) is needed for Beads integration — the task-ledger layer sits between the operator-queue surface and the workflow engine. The research already committed to "SQLite for the queue"; Beads IS the SQLite queue, just richer than originally envisioned.
- Handler-contract component must reflect the skill-injection obligation.

## Open discussion threads (TaskList)

From the 2026-04-21 session:

- **#11 Commit-per-node git pattern.** User likes Kilroy's one-commit-per-workflow-step pattern and wants it confirmed. Likely a quick confirmation + note in execution-model.md's checkpoint semantics.
- **#12 Feature-branch strategy.** User pushed back on "merge to main per task" — a feature = 10 tasks, feature branch holds the 10, main only gets the whole feature. Related to #14 via Beads's `parent-child` edge (feature bead → task beads). Worth a focused design exchange. **Recommended next conversation topic.**
- **#13 Task ingestion pipeline.** How does a kerf-produced spec become beads in harmonik? Batch import? Agent-authored? Ingestion mechanism is undefined.
- **#14 Task management model** — **resolved this session.** Beads is the store. Keeping the task in the list so the next session can see its history.

From original TASKS.md §Phase 0:
- Group A (bootstrap.md decisions)
- Group B (subsystem docs refresh — S02/S05/S09 still stale)
- Group C (parked architectural details)
- Group D (knowledge-base hygiene)

## Recommended next session flow

1. Read this file and the files it points to.
2. Pick a discussion thread. User's preference is discrete topics, not batches (see feedback memory). Recommend starting with **#15 (state source of truth)** — it's the precondition for #12 (feature-branch strategy) and #13 (task ingestion). See the §"State source of truth" section below for the framing.
3. Before reviving the kerf research-to-change-design advancement, address the four foundation updates listed above (DOT as workflow format, bead-ID trailer, Beads-integration component, skill-injection obligation). These are small and well-scoped; can be done as foundation amendments.

## State source of truth — the open design question

User flagged 2026-04-21: "We'll want to think about this — where are we storing state. We really need to nail this down. I'm slightly leaning toward US (harmonik) being the source of truth."

The underlying question has multiple layers; the next session should work through each:

### Layer 1: Completion (is this task actually done?)

**Currently decided:** git history is the source of truth. A task is "complete" iff a merge commit exists with `Harmonik-Bead-ID: <id>` carrying the terminal status. Queue (SQLite) and Beads are caches reconciled on restart by scanning git log.

This layer is stable; the user hasn't challenged it.

### Layer 2: Workflow execution state (where is this task in its workflow?)

**Currently decided (per Beads integration memory):** harmonik owns fine-grained workflow state in its JSONL event log; Beads sees only terminal transitions (claim / close / reopen).

This layer is stable; the user hasn't challenged it.

### Layer 3: Feature / task composition (how does a feature relate to its tasks?)

**Open.** This is what #15 / #12 are about. Three candidate structures:

- **Option A — Beads holds the parent-child tree.** A feature bead has child task beads via Beads's native `parent-child` edge type. Harmonik reads the tree from Beads and executes. Feature bead closes when all children close.
  - Pro: Beads's data model supports it natively; external tools see the structure.
  - Con: Two places define "what feature is this task part of" if harmonik also names features in its workflow graph.

- **Option B — Harmonik's workflow graph (DOT) holds feature composition.** A feature is a workflow; tasks are nodes (or sub-workflows) inside it. Beads beads are leaves (the actual work items); the structure connecting them is harmonik's workflow DOT graph.
  - Pro: One source of truth for workflow structure; feature is defined in-spec.
  - Con: Feature metadata (title, description, rationale) needs a home; if not in Beads, where? Also, if workflows are ephemeral (per-task), feature composition has no persistence across sessions.

- **Option C — Hybrid.** Feature = a kerf-produced spec artifact. Task beads are generated from the spec (inheriting a `spec_ref` field pointing back). Harmonik's runtime workflow is per-task; the feature-level composition is the spec doc + the set of beads it generated. Beads has parent-child but optionally (only when a feature spec actually produces structured sub-tasks).
  - Pro: Feature = spec (a thing that exists anyway); tasks are derived; nothing is duplicated.
  - Con: Requires a spec → beads ingestion mechanism (task #13).

**User's lean:** harmonik is source of truth. This maps most cleanly to Option B or Option C. Option A has Beads as structural authority, which the user is pulling against.

### Related: layer 4 — merge / branch strategy

User's prior point from task #12: "If a feature is 10 tasks, we don't want each task merged to main. Feature branch holds the 10; main gets the whole feature."

This ties back: whichever option above wins, it determines **when a merge to main happens**. If feature is in harmonik (Option B/C), harmonik decides "all tasks done → time to merge feature to main." If feature is in Beads (Option A), harmonik watches for the parent bead to close and triggers the merge.

### Next-session action

1. Confirm or challenge layers 1 and 2 (should be quick — these are current-design positions).
2. Focus discussion on layer 3: pick A / B / C.
3. Let layer 4 (#12 merge strategy) fall out of the layer-3 decision.

Then #13 (task ingestion) becomes answerable: "how do spec artifacts become beads?"

## What should NOT be re-opened

- The 10 locked decisions from 2026-04-19.
- The 4 decisions added 2026-04-20/21 (DOT, no DTW, Beads, skill injection) unless the user explicitly raises them.
- The foundation problem-space and decompose artifacts — both converged through multi-round review. Amendments via the amendment protocol, not rewrites.

## Notes on user collaboration style

Saved to auto-memory, but worth restating:

- **Design stage, no commitments yet.** Frame features as capabilities that enable other behaviors, not as requirements. Replay is the archetype: rarely used directly but the architecture it requires enables debugging, scenario tests, crash recovery.
- **Discuss one topic at a time.** Large batch responses are hard to work through. Use TaskCreate to track; handle each discretely.
- **Make the calls yourself for straightforward decisions.** Bring the user in on architecture- or UX-critical calls.
- **Describe content, not labels.** File names alone are not proposals.
- **Numerous reviewer agents on decisions.** Foundation works used 5 personas × 3 rounds.

## Files worth knowing about

- `.kerf/recon/` — the overnight recon findings (Kilroy, Attractor, subsystem audit, NFR inventory, Beads). Gitignored but present locally.
- `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/` — the kerf work artifacts; outside the repo. If you lose track, run `kerf show harmonik-foundation`.
- `/Users/gb/.claude/projects/-Users-gb-github-harmonik/memory/` — auto-memory; MEMORY.md is the index; individual feedback files carry design preferences and collaboration mode.
- `docs/methodology/TESTING.md` — testing methodology (five layers: unit, integration, scenario, crash-recovery, property). New 2026-04-21.

## Backlog context worth preserving

Things that might otherwise be forgotten across sessions:

- **Scenario test suite for Beads+harmonik crash recovery** is a named backlog item in TASKS.md. User explicitly requested it (2026-04-21) because the harmonik-Beads interaction introduces new failure modes (bead claimed but work not started; merge landed but bead not closed; etc.). This is the first real exercise of the crash-recovery test layer in `TESTING.md`.
- **Four foundation amendments** are queued in TASKS.md: DOT as workflow format, `Harmonik-Bead-ID` checkpoint trailer, Beads-integration component spec, handler-contract skill-injection obligation. Small changes, well-scoped; can happen before resuming the kerf research→change-design advancement.
- **Handler-contract skill injection** is a more general pattern than Beads-CLI: any workflow node that requires an agent to use a specific tool/skill should be able to declare it, and the handler should equip the agent. Beads is just the first instance.
- **Feature metadata** (title, description, rationale) has no settled home. If we pick Option B or C for state source of truth, this becomes an explicit design question.

Good luck.
