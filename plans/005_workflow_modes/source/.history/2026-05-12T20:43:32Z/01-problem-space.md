# Workflow Modes — Problem Space

## Summary

Today the harmonik daemon drives every claimed task through a single hardcoded handler invocation: claim → worktree → one Claude invocation → close. This was the MVH shortcut. The locked-in architectural decisions (OVERVIEW.md #6, #16; orchestrator-core subsystem) describe tasks walking a DOT-defined workflow graph, but no graph walker exists in the code yet.

This kerf introduces **workflow modes** as a first-class daemon feature, with three modes forming a ladder of generality:

1. **`single`** — current MVH behavior. One Claude instance does the task and exits.
2. **`ralph`** — implementer + reviewer loop. A second Claude instance reviews the implementer's work and either approves or returns change requests, which the implementer addresses in a resumed session. Capped iteration count.
3. **`dot`** — full graph-walker per the locked-in design. Out of scope for this kerf; in-scope only insofar as the mode-selection mechanism must extend to it cleanly.

The ralph mode is the first non-trivial workflow. It also functions as a proto-DOT case: a hardcoded two-node graph (`implement → review → {approve: close, request_changes: implement}`) that proves out the daemon's ability to drive a task through multiple sequential steps with verdict-routed transitions. The shape it forces on the daemon — per-task multi-step state, artifact handoff between steps, structured verdicts — is exactly what the DOT interpreter will need later. Building ralph first lets the abstraction emerge from one concrete case before being generalized.

## Goals

- Daemon supports three workflow modes, selectable per project and per task.
- `ralph` mode runs implementer → reviewer → (loop or close) with a cap of 3 iterations, then closes the task with `needs-attention` status if the cap is hit.
- The implementer Claude instance persists across iterations (session-resume); each reviewer is a fresh Claude instance.
- The reviewer's verdict is a JSON file at a known path in the worktree, conforming to the existing `agent-reviewer` schema (`APPROVE` / `REQUEST_CHANGES` / `BLOCK`).
- Mode selection: project-level config sets a default; per-task label or field overrides it.
- The mechanism scales cleanly to `dot` mode later — the per-task lifecycle becomes a graph walk in code, with ralph as a hardcoded two-node case the same walker can also drive.

## Non-Goals

- **DOT loader / interpreter.** Out of scope; deferred to a later kerf.
- **Parallel execution within a single task** (two Claudes working concurrently on one task). The ralph loop is strictly sequential: implementer paused, reviewer runs, reviewer exits, implementer resumes.
- **Multi-task parallelism.** Separate scope (the parallelism roadmap). This kerf assumes one task at a time; the daemon's existing work loop is unchanged. Parallelism work happens in a separate kerf after this lands.
- **Configurable iteration cap.** Default is 3 hardcoded for v1; can be promoted to config later if real cases demand it.
- **Reviewer skill variants** (security-review, code-review, custom). v1 uses one reviewer prompt; multiple reviewer flavors come with DOT mode.
- **Workflow modes on a partial-task or sub-task level.** Mode is selected once at task claim time and applies to the whole task lifecycle.

## Constraints

- **Worktree shared between processes.** Implementer and reviewer run in the same worktree but never concurrently — implementer exits or pauses before reviewer starts, reviewer exits before implementer resumes. No concurrent file writes from the two processes.
- **Session-resume must work.** Implementer continuation depends on `claude --resume <session-id>` or equivalent preserving the prior session's context. Mechanism must be verified in design pass.
- **Verdict schema reuse.** The reviewer's `.harmonik/review.json` matches the existing `agent-reviewer` skill's JSON schema v1 (`schema_version`, `verdict`, `flags`, `notes`). Don't invent a parallel schema.
- **`needs-attention` task status.** On iteration-cap hit or `BLOCK` verdict, the task is closed with a `needs-attention` label (or equivalent — to be defined in design pass). Operator picks it up manually.
- **Event-model fidelity.** Each iteration's implementer run and reviewer run emits its own `run_started` / `run_completed` pair, distinguishable in JSONL. The whole ralph cycle for one task may span multiple `run_id`s, all tagged with a shared `task_id` (or equivalent grouping).
- **Mode-selection precedence.** Per-task setting wins over project-level default. If neither is set, daemon defaults to `single`.
- **Backwards compatibility.** Existing tasks without a mode set continue to run as `single`. No flag day.

## Success Criteria

After this kerf is finalized and implemented, the following are true about the system:

1. The daemon configuration spec defines a `workflow_mode` field with values `single`, `ralph`, `dot`, defaulting to `single`.
2. The beads schema (or task metadata) supports a per-task `workflow_mode` override; resolution order is task → project → daemon default → `single`.
3. The Handler Contract spec defines the `ralph` mode lifecycle: implementer-launch → wait → reviewer-launch → wait → verdict-route → (resume-implementer | close) with iteration cap.
4. The Event Model spec adds events for the ralph cycle: implementer resume, reviewer launch, reviewer verdict, iteration-cap-hit, ralph-cycle-complete.
5. The reviewer JSON contract is documented and points to the existing `agent-reviewer` schema; the daemon parses and routes on `verdict`.
6. A smoke test runs a `ralph`-mode task end-to-end: implementer commits, reviewer requests one change, implementer addresses it, reviewer approves, task closes.
7. A smoke test runs the iteration-cap path: reviewer requests changes 3 times, daemon stops the cycle, task is marked `needs-attention`.
8. The mode-selection mechanism is factored such that adding `dot` later is a new branch in one place, not a refactor.

## Preliminary List of Spec Areas Affected

- **Handler Contract** (`specs/handler-contract.md`) — primary. Ralph lifecycle is a handler-contract concept.
- **Execution Model** (`specs/execution-model.md`) — workflow-mode field on task/run.
- **Event Model** (`specs/event-model.md`) — new event types for the ralph cycle.
- **Process Lifecycle** (`specs/process-lifecycle.md`) — daemon config field for default mode.
- **Beads Integration** (`specs/beads-integration.md`) — per-task mode field/label on the bead.
- **Workspace Model** (`specs/workspace-model.md`) — confirm sequential same-worktree usage by implementer and reviewer is in spec.
- **Operator NFR** (`specs/operator-nfr.md`) — operator-visibility of mode setting and iteration-cap events.

## Open Questions to Resolve in Later Passes

- Exact mechanism for Claude session-resume in the daemon's subprocess shape. Verify `claude --resume` behavior vs `claude -c` vs a session-ID flag.
- Where does the reviewer's prompt come from — hardcoded in handler config, a skill the daemon dispatches, or a project-level file?
- What does "close with `needs-attention`" look like in beads — a label, a status, or a body annotation?
- Does the task get a single `task_id` or a single `run_id` with multiple implementer/reviewer phases? Likely one `task_id` umbrella with multiple `run_id`s under it, but this needs Event Model alignment.
- Does the iteration-cap-hit event distinguish "reviewer kept requesting changes" from "BLOCK verdict"?
