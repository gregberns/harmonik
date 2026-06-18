---
title: kerf
status: explored
type: component
category: internal
source: ~/github/kerf
related: [docs/components/external/ntm.md, docs/components/external/agent-mail.md, docs/problems/agent-persistence-gap.md]
created: 2026-04-13
updated: 2026-04-13
---

# kerf

## Summary
Kerf is a spec-writing CLI tool for AI agents. Single Go binary. It manages the "thinking before coding" phase -- structured planning that decomposes problems into implementable units before any code is written. Kerf enforces a spec-first workflow where work progresses through defined stages, and its jig system provides process templates that agents follow without improvisation.

## Key Capabilities

### Spec-First Workflow
Work progresses through a fixed stage sequence: problem-space, decompose, research, spec, tasks, ready. Each stage has defined entry/exit criteria. This prevents agents from jumping straight to implementation without adequate planning.

### Jigs (Process Templates)
Jigs define the passes an agent makes over a piece of work. Built-in jigs: plan, spec, bug, implementation, spike, retrofit. Each jig prescribes what the agent does at each pass, what artifacts it produces, and what checks must pass before advancing. Custom jigs are supported for project-specific workflows.

### Work Management
Works have codenames, statuses, and sessions. They live on a bench (~/.kerf/) outside of git until finalization. This keeps speculative and in-progress planning out of the repository until it reaches a publishable state.

### Multi-Agent Coordination via Agent-Mail
The orchestrator registers with Agent Mail, spawns workers via NTM, sends beads (task units) per worker, and polls for completion. This is kerf's native multi-agent dispatch model.

### Beads as Task Decomposition
Plans break into implementation beads -- discrete, implementable units with explicit dependency graphs. Beads declare what they depend on, enabling layer-based parallelization where independent beads execute concurrently.

### Session and Resumability
SESSION.md preserves context across agent invocations. Shelve/resume operations reload full context. Stale session detection prevents agents from working with outdated state.

### Dependency Tracking
Works can declare dependencies on other works, including cross-project dependencies. This enables multi-repo planning where a change in one project requires coordinated changes in others.

### Verification (Square Checks)
Structural verification that work matches jig expectations. Square checks validate artifacts, stage completeness, and structural correctness without requiring LLM judgment.

### Finalization to Git
Moves specs from the bench into the repository with pre-flight checks. This is the publish gate -- work is not visible to other agents or humans until it passes finalization.

## Integration Points for Harmonik

Kerf is the **planning and specification layer**. Its role in the system:

- **Upstream of execution**: Kerf produces the specs and bead graphs that the workflow execution layer (Kilroy or equivalent) consumes. No code is written without a kerf-produced plan.
- **Agent-Mail native**: Kerf already integrates with Agent Mail for multi-agent coordination. This is the primary communication channel for dispatching beads to workers.
- **NTM as process substrate**: Kerf spawns worker agents through NTM. The planning layer directly drives the process management layer.
- **Bead graphs feed parallelization**: The dependency structure in bead graphs maps to fan-out/fan-in execution patterns. Layers of independent beads can execute concurrently.
- **Session persistence addresses P02**: Kerf's session and shelve/resume model directly addresses the Agent Persistence Gap (P02) for planning work.

## Limitations and Gaps

- **No runtime replanning**: Once beads are dispatched, the plan is fixed. If implementation reveals the spec was wrong, there is no built-in mechanism to revise the plan mid-execution.
- **Single-orchestrator model**: The current agent-mail integration assumes one orchestrator coordinating workers. Nested or hierarchical orchestration is not yet supported.
- **Bench is local**: The ~/.kerf/ bench is machine-local. Multi-machine planning requires manual coordination or external sync.

## Open Questions

1. How should kerf's bead graphs integrate with Kilroy's DOT pipeline format? Is there a natural mapping, or do we need an adapter?
2. Should kerf own the "what to work on next" decision, or should that be a separate orchestration concern?
3. How do we handle plan invalidation when implementation feedback contradicts the spec?

## Commands & Workflow (for planning agents)

This project is **spec-first**: the spec describes how the system operates; code is updated to match. The `spec` jig is the default. Before non-trivial changes (new subsystems, cross-subsystem refactors, cross-cutting contracts), create a kerf work; trivial changes (typos, one-liners) skip kerf.

### Key commands

    kerf new <codename>              Create a new work
    kerf show <codename>             See current state + jig instructions for next steps
    kerf status <codename>           Check current status
    kerf status <codename> <status>  Advance to next pass
    kerf shelve <codename>           Save progress when ending a session
    kerf resume <codename>           Pick up where you left off
    kerf square <codename>           Verify the work is complete
    kerf finalize <codename> --branch <name>  Package for implementation

### Queue + work-attachment surface

    kerf next                        Ranked feed of bead IDs ready to dispatch
    kerf triage                      Drift report (suggested bead reattachments, stale links)
    kerf triage --ack                Advance kerf's baseline after acting on the report
    kerf pin <bead> <work>           Attach a bead to a kerf work
    kerf work edit <codename>        Edit a work's bead-attachment config (bead_filter etc.)
    kerf map                         Works grouped by area
    kerf areas                       Manage areas (list/add/edit)

### Agent loop pattern (informal)

`kerf next` returns ranked bead IDs → orchestrator dispatches them via harmonik → on completion `br close <id>` is invoked → `kerf triage --ack` advances kerf's baseline. kerf manages the queue and work-attachment; harmonik executes.

### When to use kerf

- New subsystems, cross-cutting spec changes → `kerf new --jig spec`
- Non-trivial feature plans → `kerf new --jig plan`
- Bug investigations → `kerf new --jig bug`
- Trivial changes (typos, one-line fixes) → skip kerf

### Workflow

1. `kerf new <codename>` — read the output; it tells you exactly what to do
2. Follow each pass: write the artifacts, advance status
3. `kerf show <codename>` — if you lose context, this shows where you are
4. `kerf shelve` / `kerf resume` — for multi-session work
5. `kerf square` — verify everything is complete
6. `kerf finalize` — package into a git branch for implementation

### Beta-test caveat

kerf is in **beta-test** in this project. Known issues: `kerf next` may report empty for works lacking `bead_filter` clauses; `kerf init` emits stale + duplicated agent-instruction blocks; `kerf triage` mixes good and phantom suggestions. Log issues to `docs/kerf-beta-feedback.md` (convention: `KERF-FEEDBACK.md`).
