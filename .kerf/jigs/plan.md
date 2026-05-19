---
name: plan
description: Write a plan before changing code. For existing projects.
version: 1
phase: planning
tools: []
composable: false
aliases: [feature]
status_values:
  - problem-space
  - analyze
  - decompose
  - research
  - change-spec
  - integration
  - tasks
  - ready
passes:
  - name: "Problem Space"
    status: problem-space
    output: ["01-problem-space.md"]
  - name: "Analyze"
    status: analyze
    output: ["02-analysis.md"]
  - name: "Decompose"
    status: decompose
    output: ["03-components.md"]
  - name: "Research"
    status: research
    output: ["04-research/{component}/findings.md"]
  - name: "Change Spec"
    status: change-spec
    output: ["05-specs/{component}-spec.md"]
  - name: "Integration"
    status: integration
    output: ["06-integration.md", "SPEC.md"]
  - name: "Tasks"
    status: tasks
    output: ["07-tasks.md"]
  - name: "Ready"
    status: ready
    output: []
file_structure:
  - spec.yaml
  - SESSION.md
  - 01-problem-space.md
  - 02-analysis.md
  - 03-components.md
  - "04-research/{component}/findings.md"
  - "05-specs/{component}-spec.md"
  - 06-integration.md
  - SPEC.md
  - 07-tasks.md
---

# Plan Jig

## Overview

This jig guides you through a structured process for planning a change to an existing codebase. You describe what to change, the jig walks you through analysis, decomposition, research, and spec writing, and you end with an implementation-ready task list.

Each pass produces one or more files. If work is not captured in a file, it is lost when the session ends.

## Pass 1: Problem Space (problem-space)

**Output:** `01-problem-space.md`

Clarify what is changing and why through 2-3 conversational exchanges with the user.

You are clarifying the user's intent. This is a conversation, not a questionnaire. Your goal is to produce a document that a new agent could read and fully understand the scope, constraints, and success criteria of this change.

**What to do:**

1. Read any source material the user has provided or referenced. Understand their starting point before asking questions.
2. Ask about the goal and motivation. "What problem does this solve?" and "Who benefits?" -- not "What should the API look like?" You are in problem space, not solution space.
3. Define scope boundaries explicitly. State what is in scope and what is explicitly out of scope. If the user hasn't thought about boundaries, propose them and get confirmation.
4. Identify constraints -- technical limitations, dependencies on other systems, performance requirements, backwards-compatibility needs, timeline pressure.
5. Capture what success looks like. These must be concrete, verifiable statements: "a user can do X" or "the system handles Y within Z milliseconds." Reject vague criteria like "it should be fast" -- push for specifics.
6. Summarize back in structured form and get the user's confirmation before saving.
7. **Save to disk:** Write `01-problem-space.md`. Advance status to `analyze`.

**What done looks like:**

- `01-problem-space.md` exists and contains:
  - A one-paragraph summary of the change
  - Goals (what this work achieves)
  - Non-goals (what this work explicitly does not attempt)
  - Constraints (technical, business, or timeline)
  - Success criteria (concrete, verifiable statements)
- The user has confirmed the problem space is accurate

## Pass 2: Analyze (analyze)

**Output:** `02-analysis.md`

Read the existing codebase and document the current state of the areas that will be affected by the change.

You are building a map of the territory before proposing changes to it. The output is a factual description of what exists, not a proposal for what to build.

**What to do:**

1. Read `01-problem-space.md` to understand what areas of the codebase are relevant.
2. Explore the codebase systematically. For each area affected by the change:
   - Read the relevant source files. Understand the current structure, patterns, and conventions.
   - Identify existing abstractions, interfaces, and data flows that the change will interact with.
   - Note architectural patterns in use.
   - Check for existing tests -- what is tested, what testing patterns are used.
3. Identify constraints imposed by the existing code. What must be preserved? What interfaces are public? What would break if changed?
4. Note code health issues relevant to the change -- areas of tech debt that will complicate the work, missing abstractions that will be needed, or existing patterns that should be followed.
5. Check git history for recent activity in the affected areas. Recent changes may indicate work in progress or related efforts.
6. **Save to disk:** Write `02-analysis.md`. Advance status to `decompose`.

**What done looks like:**

- `02-analysis.md` exists and contains:
  - A list of affected areas in the codebase with file paths
  - For each area: current structure, patterns in use, relevant interfaces
  - Existing constraints (public interfaces, backwards-compatibility requirements)
  - Conventions to follow (naming, error handling, testing patterns)
  - Any relevant recent changes from git history
- The analysis is factual and traceable to specific files and code

## Pass 3: Decompose (decompose)

**Output:** `03-components.md`

Break the change into 3-7 components with concrete, testable requirements for each.

You are turning a problem statement and codebase analysis into a structured breakdown. The output is components with requirements -- not an implementation plan.

**What to do:**

1. Read `01-problem-space.md` and `02-analysis.md` to ground the decomposition in the agreed problem space and the current codebase state.
2. **Ask the user: guided or autonomous?**
   - **Guided mode:** Present each component's requirements individually, get approval before proceeding to the next. Track progress for the user: "Component 3/5: Authentication."
   - **Autonomous mode:** Complete the full breakdown, then present for review.
   - **Default:** If no user is present (autonomous workflow), use autonomous mode.
3. Identify 3-7 components. A component is a cohesive unit of functionality that can be specified and implemented somewhat independently. If you have more than 7, you are decomposing too finely -- group related pieces. If you have fewer than 3, the work may not need the full plan jig.
4. For each component, write concrete, testable requirements. Requirements describe WHAT the component does, not HOW it does it. Each requirement is verifiable -- "returns a 404 with an error body when the resource is not found" not "handles errors gracefully."
5. Identify dependencies between components. Which must be built before others? Which share interfaces?
6. Identify interfaces between components. Where does data flow from one to another? What contracts exist at the boundaries?
7. **Save to disk:** Write `03-components.md`.
8. Run the review process (see Review Criteria below).
9. After review completes, advance status to `research`.

**What done looks like:**

- `03-components.md` exists and contains:
  - A list of 3-7 named components
  - For each component: a one-line description, a list of concrete requirements, and its dependencies on other components
  - An interface summary showing data flow and contracts between components
- Every goal from `01-problem-space.md` maps to at least one component
- Requirements are concrete and testable throughout

### Review Criteria

After completing the component breakdown, spawn a review sub-agent with:
- `03-components.md`
- `01-problem-space.md` (for scope validation)
- `02-analysis.md` (for codebase grounding)

The reviewer checks:
- Every goal from `01-problem-space.md` maps to at least one component requirement
- No component requirement exists that cannot be traced back to a goal or constraint
- Requirements are concrete and testable -- "returns 404 with error body" not "handles errors"
- Component boundaries are clean (minimal cross-dependencies)
- 3-7 components (flag if outside this range with justification)
- Dependencies between components form a DAG (no circular dependencies)
- Interfaces between components are explicitly identified

Up to 3 review rounds. Save findings to `decompose-review.md`. After the final round, present artifacts and any unresolved findings to the user for approval. In autonomous mode: if no findings remain, advance. If unresolved findings remain after max rounds, advance with findings saved under an `## Unresolved` section in `decompose-review.md`.

## Pass 4: Research (research)

**Output:** `04-research/{component}/findings.md` (one file per component)

Investigate technical approaches, existing patterns, and risks for each component.

You are gathering the information needed to write detailed change specs. You are not making final decisions -- you are exploring options, identifying constraints, and presenting tradeoffs.

**What to do:**

1. Read `02-analysis.md` and `03-components.md` to understand what each component needs and what exists today.
2. For each component, identify 3-5 specific research questions. These are the things you need to know before you can write a change spec. Examples:
   - "Does the codebase already have a pattern for X?"
   - "What are the performance characteristics of approach Y?"
   - "How does library Z handle edge case W?"
   - "What tests exist for the code being modified?"
   - "Are there recent changes to this area that suggest ongoing work?"
3. **Delegate research to a sub-agent.** For each component (or batch of components), spawn a sub-agent with fresh context. Provide the component's requirements from `03-components.md`, the relevant section of `02-analysis.md`, the research questions, and access to the codebase. The sub-agent explores the codebase, checks external docs/APIs, identifies technical constraints, and returns findings.
4. For small components with 1-2 straightforward questions, research inline instead of delegating.
5. **Save to disk:** Write `04-research/{component}/findings.md` for each component as its research completes.
6. Present key findings to the user. Flag any decisions that need user input -- e.g., when research reveals multiple viable approaches with different tradeoffs.
7. Advance status to `change-spec`.

**What done looks like:**

- For each component, `04-research/{component}/findings.md` exists and contains:
  - Research questions that were investigated
  - Findings for each question, with evidence (code references, file paths, documentation links)
  - Options and tradeoffs for key decisions (2-3 approaches with pros/cons for non-trivial choices)
  - Identified risks and unknowns
- All research questions from the component requirements are addressed
- No component has unresolved blockers that would prevent writing a change spec

## Pass 5: Change Spec (change-spec)

**Output:** `05-specs/{component}-spec.md` (one file per component)

Write implementation-level change specifications for each component, informed by the research findings.

You are writing the spec that an implementing agent will follow. Everything an implementer needs to know goes here. Everything they do not need goes elsewhere.

**What to do:**

1. **Ask the user: guided or autonomous?**
   - **Guided mode:** Present each component's change spec individually, get approval before proceeding to the next. Track progress: "Change Spec 3/5: Authentication."
   - **Autonomous mode:** Complete all component specs, then present for review.
   - **Default:** If no user is present (autonomous workflow), use autonomous mode.
2. For each component, read `03-components.md` (requirements) and `04-research/{component}/findings.md` (research). Your spec must be consistent with the research findings. If the research identified multiple options, make a decision and record the rationale.
3. Write the change spec. Each component spec includes:
   - **Requirements** -- from `03-components.md`, carried forward for traceability
   - **Research summary** -- key findings that inform the approach
   - **Approach** -- how to implement this. Architecture decisions, patterns to follow. Reference the research findings.
   - **Files & changes** -- which files to create, modify, or delete. Be specific: file paths, what changes in each file, and why.
   - **Acceptance criteria** -- concrete, testable criteria. Each must be verifiable by running a test, executing a command, or observing specific behavior.
   - **Verification** -- how to confirm the component works. Commands to run, tests to execute, manual checks to perform.
4. Address error handling and edge cases explicitly. What errors can occur? How is each handled? What are the boundary conditions?
5. Address migration and backwards compatibility if applicable.
6. **Save to disk:** Write `05-specs/{component}-spec.md` for each component.
7. Run the review process (see Review Criteria below).
8. After review completes, advance status to `integration`.

**What done looks like:**

- For each component, `05-specs/{component}-spec.md` exists and contains all sections listed above
- Each spec is concrete enough that an implementing agent can work from it without additional design decisions
- Every requirement from `03-components.md` appears in a component spec
- File paths reference real locations in the codebase (validated against `02-analysis.md`)
- Acceptance criteria are testable -- no vague language

### Validation / Acceptance Tests

**Required before advancing to Integration.** File at least two test beads covering this work and record their IDs in the component spec(s):

1. **Scenario-test bead** — one bead covering the end-to-end workflow for each component (twin-substrate or real-claude). The bead must name: the harmonik CLI command under test, the bead lifecycle state being exercised, and the observable terminal condition (e.g. a specific JSONL record or file written to `.harmonik/`). Title convention: `scenario: <component> — <brief>`.
2. **Exploratory-test bead** — one bead for the operator-facing surface (any CLI command, API call, or stdout output a human or submitter agent would actually invoke). The bead must name the specific command and the expected output or side-effect. Title convention: `explore: <component> — <brief>`.

Use `br create "<title>" --type task --label scenario-test` and `br create "<title>" --type task --label exploratory-test`.

**Motivation:** hk-37zy8 (handler-pause policy goroutine — unit-tested and reviewer-APPROVED, but never wired into the composition root; a scenario test would have caught the gap at PR time). hk-aievp and hk-ry3be (dogfood-blockers found in the 2026-05-18 session: stale-pane pollution and missing agent-termination detection; neither had a scenario test gating the bead close).

### Review Criteria

After completing all component specs, spawn a review sub-agent with:
- All files in `05-specs/`
- `03-components.md` (requirements document)
- The relevant `04-research/` findings

The reviewer checks:
- Every requirement from `03-components.md` has a corresponding spec section
- No spec content exists that is not backed by a requirement
- Acceptance criteria are concrete and testable
- Files & changes sections reference real paths in the codebase
- Verification steps are runnable (commands exist, test patterns match the codebase)
- Error handling and edge cases are addressed
- Approaches are consistent with the research findings

Up to 3 review rounds. Save findings to `change-spec-review.md`.

## Pass 6: Integration (integration)

**Output:** `06-integration.md`, `SPEC.md`

Document how components connect to each other and assemble all artifacts into a single reference document.

You are ensuring the components form a coherent whole and producing the final assembled spec. This is where cross-component concerns -- initialization order, shared state, data flow between components -- get documented.

**What to do:**

1. Read all component specs in `05-specs/` and the component breakdown in `03-components.md`.
2. Write the integration plan (`06-integration.md`):
   - How do the components connect? What is the order of integration?
   - What shared state or resources exist across components?
   - What cross-cutting concerns are not covered in individual component specs (logging, configuration, error propagation across boundaries)?
   - What is the integration testing strategy?
3. Assemble the final spec document (`SPEC.md`). This is the single document an implementing agent reads first.
4. Review `SPEC.md` for completeness and internal consistency.
5. **Save to disk:** Write `06-integration.md` and `SPEC.md`.
6. Run the review process (see Review Criteria below).
7. After review completes, advance status to `tasks`.

**What done looks like:**

- `06-integration.md` exists and describes how components connect, integration order, shared state, and cross-cutting concerns
- `SPEC.md` exists and is a self-contained reference document that an implementing agent can use as its starting point
- All cross-references are consistent and no gaps remain between the problem space, components, specs, and integration plan

### Review Criteria

After completing the integration plan and assembled spec, spawn a review sub-agent with:
- `06-integration.md`
- `SPEC.md`
- All files in `05-specs/`
- `03-components.md`
- `01-problem-space.md`

The reviewer checks:
- Every success criterion from `01-problem-space.md` traces to a component, then to a change spec section
- Interface definitions between components are consistent (data types, contracts, error handling)
- No contradictions exist between component specs
- Integration concerns are addressed (initialization order, shared state, cross-component error handling)
- `SPEC.md` is a faithful assembly -- it does not add requirements or change decisions from the component specs

Up to 3 review rounds. Save findings to `integration-review.md`.

## Pass 7: Tasks (tasks)

**Output:** `07-tasks.md`

Break the spec into implementation tasks with dependencies.

You are producing a task list that makes the assembled spec actionable.

**What to do:**

1. Read `SPEC.md`, `06-integration.md`, and the component specs in `05-specs/`.
2. Break the spec into implementation tasks. Each task specifies:
   - **What to build** -- a concrete description of the work
   - **Spec reference** -- which sections of `SPEC.md` or which component spec it implements
   - **Deliverables** -- files to create or modify, tests to write
   - **Acceptance criteria** -- how to verify the task is complete
   - **Dependencies** -- which other tasks must be completed first
3. Order tasks by dependency. Identify which tasks can be parallelized.
4. Include integration tasks and test tasks.
5. Verify completeness: every section of `SPEC.md` must be covered by at least one task.
6. **Save to disk:** Write `07-tasks.md`.
7. Run the review process (see Review Criteria below).
8. After review completes, advance status to `ready`.

**What done looks like:**

- `07-tasks.md` exists and contains:
  - An ordered list of tasks with descriptions, spec references, deliverables, acceptance criteria, and dependencies
  - A dependency graph showing task ordering and parallelization opportunities
  - Complete coverage -- every spec section and acceptance criterion is assigned to a task
- Dependencies form a DAG (no circular dependencies)

### Validation / Acceptance Tests

**Required before advancing to Ready.** `07-tasks.md` MUST include at least two test beads in the task list. Verify that both beads from the Change Spec pass exist and are still open. If they don't exist yet, create them now:

1. **Scenario-test bead** — covering the end-to-end workflow using a twin or real-claude substrate. Must be an explicit task with a dependency on the implementation tasks it validates. Title convention: `scenario: <plan-codename> — <brief>`.
2. **Exploratory-test bead** — covering the operator-facing CLI/API surface. Must be an explicit task. Title convention: `explore: <plan-codename> — <brief>`.

These test tasks MUST appear in the dependency graph as dependents of the core implementation tasks. Neither the plan nor its implementation beads may close until these test beads are also closed.

**Motivation:** hk-37zy8 (wire-up gap found post-merge), hk-aievp and hk-ry3be (dogfood-blockers from 2026-05-18 session) — all were approved without a scenario test gating the close.

### Review Criteria

After completing the task list, spawn a review sub-agent with:
- `07-tasks.md`
- `SPEC.md`
- All files in `05-specs/`
- `06-integration.md`

The reviewer checks:
- Every section of `SPEC.md` is covered by at least one task
- Every acceptance criterion from every component spec appears in at least one task
- Dependencies are correct and form a DAG
- Tasks are appropriately sized
- Integration tasks exist and are correctly ordered
- At least one scenario-test bead and one exploratory-test bead appear in the task list with correct dependencies

Up to 3 review rounds. Save findings to `tasks-review.md`.

## Pass 8: Ready (ready)

**Output:** (none)

The work is complete. Run `kerf square <codename>` to verify all expected artifacts exist on disk.

**What to do:**

1. Run `kerf square <codename>` to verify all expected artifacts exist on disk.
2. If square fails, identify which artifacts are missing and return to the appropriate pass to produce them.
3. If square passes, the work is ready for implementation.

**What done looks like:**

- `kerf square` passes with no errors
- All 7 artifact files (plus per-component research and spec files) exist on disk
- The work is a self-contained change document ready for implementation handoff

## Finalization

When this work moves to `ready`, run `kerf square <codename>` to verify, then `kerf finalize <codename>` to package it for implementation.
