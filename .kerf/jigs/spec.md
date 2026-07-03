---
name: spec
description: Maintain a living spec that defines your system. Spec is always right.
version: 1
phase: planning
tools: []
composable: false
status_values:
  - problem-space
  - decompose
  - research
  - change-design
  - spec-draft
  - integration
  - tasks
  - ready
passes:
  - name: "Problem Space"
    status: problem-space
    output: ["01-problem-space.md"]
  - name: "Decompose"
    status: decompose
    output: ["02-components.md"]
  - name: "Research"
    status: research
    output: ["03-research/{component}/findings.md"]
  - name: "Change Design"
    status: change-design
    output: ["04-design/{component}-design.md"]
  - name: "Spec Draft"
    status: spec-draft
    output: ["05-spec-drafts/{component}.md", "05-changelog.md"]
  - name: "Integration"
    status: integration
    output: ["06-integration.md"]
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
  - 02-components.md
  - "03-research/{component}/findings.md"
  - "04-design/{component}-design.md"
  - "05-spec-drafts/{component}.md"
  - 05-changelog.md
  - 06-integration.md
  - 07-tasks.md
---

# Spec Jig

## Overview

This jig guides you through a structured process for maintaining a living system specification. Changes to the system start as spec updates, then flow to code. You identify what specs need to change, research the impact, design the changes, draft new spec text, and produce implementation tasks.

Each pass produces one or more files. If work is not captured in a file, it is lost when the session ends.

**Commit each pass (durability).** Artifacts under `.kerf/works/<codename>/` are git-tracked so planning knowledge survives a machine loss. Do all of this work on a per-work branch `kerf/<codename>`, and at the END of every pass — after writing that pass's files and advancing status — commit and push:

```
git checkout -b kerf/<codename>   # first pass only; `git checkout kerf/<codename>` thereafter
git add .kerf/works/<codename>
git commit -m "kerf(<codename>): <pass name>"
git push -u origin kerf/<codename>
```

This keeps in-progress planning off `main` while still pushing to origin every pass. Never end a session with uncommitted bench artifacts. On `kerf finalize` the `kerf/<codename>` branch merges to `main`, landing the full reasoning chain alongside the finalized spec.

In this jig, the `{component}` placeholder expands to affected spec areas (typically spec filenames without the `.md` extension). In the Spec Draft pass, `{component}` expands to target spec filenames -- each draft in `05-spec-drafts/` maps 1:1 to a file in the system `specs/` directory.

Each content pass ships a template alongside the jig under `internal/jig/builtin/templates/spec/`. The template filename is named in each pass header below; kerf copies the template into the pass output path on status advance.

## Pass 1: Problem Space (problem-space)

**Output:** `01-problem-space.md` (template: `01-problem-space.md.template`)

Clarify what needs to change in the system and why. Identify which aspects of the system are affected.

This is a conversation, not a questionnaire. Ask 2-3 focused questions, listen to the answers, then ask follow-up questions based on what you learn. Do not fire all questions at once.

**What to do:**

1. Read any source material the user has provided. Understand the motivation for the change before asking questions.
2. Clarify the user's goals. Ask "what should be true about the system after this change?" and "what problem does this solve?" Focus on the system-level impact, not implementation.
3. Define scope boundaries. What spec areas are in scope? What is explicitly out of scope? If the user hasn't thought about boundaries, propose them.
4. Identify constraints -- backwards compatibility requirements, interactions with other spec areas, things that must not change.
5. Capture success criteria. These are concrete statements about what the specs should describe after the work is complete. "The jig system spec defines a review pattern" not "improve the review process."
6. Save to `01-problem-space.md`. Advance status to `decompose`.

**What done looks like:**

- `01-problem-space.md` contains: a summary of what's changing and why, goals, non-goals, constraints, success criteria, and a preliminary list of spec areas that may be affected
- The user has confirmed the problem space is accurate

## Pass 2: Decompose (decompose)

**Output:** `02-components.md` (template: `02-components.md.template`)

Identify which existing spec files are affected and what new spec files are needed. Define the scope of changes for each.

**What to do:**

1. Read `01-problem-space.md` to ground the decomposition in the agreed problem space.
2. Read the existing system specs that may be affected. For each spec file, understand its current content and scope.
3. Identify affected spec files. For each: what requirements does this change impose? What needs to be true after the change that isn't true now? State requirements in terms of what the spec should describe, not how the spec text should read -- that comes in Change Design.
4. Identify new spec files needed. For each: what is its scope, what requirements does it satisfy, and what is the intended filename?
5. Map dependencies between spec changes. Which changes must be made before others? Which spec files reference each other?
6. Ask the user: walk through each affected area together (guided), or complete the full breakdown for review (autonomous)? Default to autonomous if no user is present. In guided mode: present each affected area individually, get approval before proceeding. Track progress ("Area 3/5: commands.md").
7. Save to `02-components.md`.

**Done when reviewer approves on:**

Inputs the reviewer reads: `02-components.md`, `01-problem-space.md`, the existing spec files listed as affected.

Criteria:
- `02-components.md` lists each affected spec file (existing and new) with: a one-line description of the change, concrete requirements for what the spec should describe after the change, and dependencies on other spec changes
- Every goal from `01-problem-space.md` maps to at least one spec area
- No spec area is listed that isn't justified by a goal or requirement
- Requirements describe what should be true, not how the text should change
- All relevant existing spec files are accounted for (no missing affected areas)
- Dependencies between spec changes are correctly identified

Review follows the protocol in `jig-system.md` §Review Pattern. Up to 3 review rounds. Save findings to `decompose-review.md`. After approval (or escalation after the final round), advance status to `research`.

## Pass 3: Research (research)

**Output:** `03-research/{component}/findings.md` (one file per affected spec area; template: `03-research.findings.md.template`)

Investigate each affected area to inform the design. The `{component}` placeholder expands to one directory per affected spec area from Pass 2.

**Parent owns the write.** Research is delegated per area to a sub-agent with fresh context, but the sub-agent returns findings as text -- it does not write files. The parent agent collects the returned text and writes each area's `findings.md`. This keeps the pattern portable across harnesses that restrict sub-agent file writes (see `jig-system.md` §Sub-agent file writes). The parent is the single accountable writer for these artifacts.

**What to do:**

1. Read `02-components.md` to understand what each spec area needs.
2. For each affected area, identify 3-5 research questions. Examples: "What does the current spec say about finalization?" "How do other spec files cross-reference this one?" "Are there existing patterns in the spec corpus for this kind of structure?" "What does the source material say about this topic?"
3. Delegate research per area to a sub-agent with fresh context. Provide the component requirements from `02-components.md`, read access to the existing specs and any source material, and the research questions. Instruct the sub-agent to return findings as a single text payload -- not to write to disk.
4. The sub-agent reads existing spec files, checks source material, identifies patterns and constraints, and returns findings.
5. As the parent agent, write each returned payload to `03-research/{component}/findings.md`. One file per area.
6. Present key findings to the user. Flag decisions needed -- especially where existing specs have patterns that should be followed or where the proposed change conflicts with existing content.

**What done looks like:**

- For each affected area, `03-research/{component}/findings.md` contains: research questions, findings with evidence (spec references, source material quotes), patterns to follow, and risks or conflicts identified
- All research questions are addressed with evidence
- No unresolved blockers prevent writing a change design

Advance status to `change-design`.

## Pass 4: Change Design (change-design)

**Output:** `04-design/{component}-design.md` (one file per affected spec area; template: `04-design.component-design.md.template`)

Document the intended changes for each affected spec area. This is the design document -- it explains the *intent* of each spec change, not the spec text itself.

**One design file per affected spec area is the default.** Each design document covers a single spec area's change. Co-located designs are easier to review independently, easier to traceback from `05-changelog.md`, and easier to revise without re-touching unrelated areas. For very small changes that touch only one or two spec areas with tightly coupled intent, a single aggregate `04-design/design.md` is acceptable; if used, the changelog still names each affected spec file individually. Default to per-area files unless the change is clearly aggregate in nature.

**What to do:**

1. Read the research findings for the area you are designing (`03-research/{component}/findings.md`).
2. Read the existing spec file (if modifying an existing spec). Understand its current structure and content.
3. Document the **current state**: what the spec says now (or "new file" if creating a new spec).
4. Document the **target state**: what the spec should say after the change. Be specific about sections, content, and structure. This is not the final spec text -- it is a description of the change.
5. Document the **rationale**: why this change is needed, which requirements it satisfies, and how the research findings informed the design.
6. Ask the user: guided or autonomous? Default to autonomous if no user is present. In guided mode: present each area's design individually, get approval before proceeding.
7. Save per area to `04-design/{component}-design.md`.

**Done when reviewer approves on:**

Inputs the reviewer reads: all files in `04-design/`, `02-components.md`, the relevant `03-research/` findings, the existing spec files being modified.

Criteria:
- For each affected area, `04-design/{component}-design.md` contains: current state, target state, rationale, and requirements traceability
- Every requirement from `02-components.md` is addressed by a target state
- No target state exists that isn't backed by a requirement
- Current state accurately reflects what the spec says now
- Target state is specific enough that a spec writer can produce the final text from it
- Rationale references research findings where applicable
- No contradictions between different areas' target states

Review follows the protocol in `jig-system.md` §Review Pattern. Up to 3 review rounds. Save findings to `change-design-review.md`. After approval (or escalation after the final round), advance status to `spec-draft`.

## Pass 5: Spec Draft (spec-draft)

**Output:** `05-spec-drafts/{component}.md` (one file per target spec file; template: `05-spec-drafts.component.md.template`), `05-changelog.md` (template: `05-changelog.md.template`)

Write the actual spec text as it should appear in the system specs. This is the most critical pass -- the drafted text will become the normative specification at finalization.

**Drafts are named to match their target spec files.** Each file in `05-spec-drafts/` maps 1:1 to a file in the system `specs/` directory:

- For an **existing** spec file: the draft uses the same filename and contains the complete updated spec file -- not a diff, not a patch, the full file as it should appear after the change.
- For a **new** spec file: the draft uses the intended filename and follows the conventions of existing spec files.

**What to do:**

1. Read the change design for the area you are drafting (`04-design/{component}-design.md`).
2. If modifying an existing spec: read the current spec file. Your draft must include the full updated file, incorporating all existing content that is not being changed alongside the new or modified content.
3. If creating a new spec: follow the conventions of existing spec files in the project (formatting, section structure, cross-reference style).
4. Write spec text that is normative -- "the system does X", not "we chose X because Y." Design rationale belongs in the change design documents, not in the spec.
5. Ensure cross-references are correct. If the spec links to other spec files, verify those links will be valid after all changes are applied.
6. Save the draft to `05-spec-drafts/{target-filename}.md`.
7. After all drafts are written, produce the changelog (`05-changelog.md`). The changelog documents for each spec file: the target filename, status (new/modified/removed), what was changed, and which change design motivated the change.

**Done when reviewer approves on:**

**This is the most critical review** -- the drafted text becomes normative at finalization.

Inputs the reviewer reads: all files in `05-spec-drafts/`, all files in `04-design/`, the existing spec files being modified, `05-changelog.md`.

Criteria:
- `05-spec-drafts/` contains one file per target spec file, named to match the target
- Each draft for an existing spec contains the full updated file (not a diff)
- Each draft for a new spec follows project conventions
- Every target state from the change designs is accurately reflected in the drafted spec text
- No spec content was added that isn't backed by a change design
- No existing spec content was accidentally removed or altered beyond what the change design calls for
- Spec text is normative (describes what the system does, not why decisions were made), not rationale or design discussion
- Cross-references between drafted specs are valid (and consistent with unchanged specs)
- Draft filenames match their target spec files exactly
- `05-changelog.md` accounts for every draft with changes and traceability to a change design
- Formatting and structure are consistent with the project's existing spec files

### Validation / Acceptance Tests

**Required before advancing to Integration.** For each spec area being introduced or substantially changed, file at least two test beads:

1. **Scenario-test bead** — one bead verifying the end-to-end workflow exercised by the new/changed spec section (twin or real-claude substrate). The bead must name: the `harmonik run` or CLI invocation under test, the bead lifecycle state or system behavior being validated, and the observable terminal condition (JSONL record, file written, or stdout output). Title convention: `scenario: <spec-file> — <brief>`.
2. **Exploratory-test bead** — one bead for the operator-facing surface defined by the spec change (the CLI command, API, or configuration a human or submitter agent would actually invoke). Title convention: `explore: <spec-file> — <brief>`.

Use `br create "<title>" --type task --label scenario-test` and `br create "<title>" --type task --label exploratory-test`.

**Motivation:** hk-37zy8 (handler-pause policy goroutine — unit-tested and reviewer-APPROVED, but never wired into the composition root; a scenario test would have caught the gap at PR time). hk-aievp and hk-ry3be (dogfood-blockers from the 2026-05-18 session: stale-pane pollution and missing agent-termination detection; neither had a scenario test gating the bead close).

Review follows the protocol in `jig-system.md` §Review Pattern. Up to 3 review rounds. Save findings to `spec-draft-review.md`. After approval (or escalation after the final round), advance status to `integration`.

## Pass 6: Integration (integration)

**Output:** `06-integration.md` (template: `06-integration.md.template`)

Cross-reference consistency check across all drafted spec changes and the existing system specs.

**What to do:**

1. Read all drafted specs in `05-spec-drafts/`.
2. Read all existing system specs -- not just the ones being modified. Changes to one spec can introduce contradictions with specs that aren't being changed.
3. Check for contradictions. Does any drafted spec state something that conflicts with an unchanged spec? Do drafted specs conflict with each other?
4. Verify cross-references. For every link between spec files (`[text](file.md)`), verify the target exists and the linked content is accurate.
5. Check terminology consistency. Are the same concepts referred to by the same names across all specs?
6. Verify that the changelog in `05-changelog.md` is complete and accurate against the actual drafts.
7. Save review notes to `06-integration.md`.

**Done when reviewer approves on:**

Inputs the reviewer reads: `06-integration.md`, all files in `05-spec-drafts/`, all existing system spec files.

Criteria:
- `06-integration.md` contains: a list of all cross-reference checks performed, any contradictions found (with resolution), any consistency issues found (with resolution), and a final assessment of overall spec coherence
- The integration check examined all system specs, not just modified ones
- All contradictions are resolved (either by updating drafts or documenting why the apparent contradiction is acceptable)
- Cross-references are valid in both directions (nothing links to removed content, nothing was orphaned)
- Terminology is consistent across the spec corpus
- The changelog matches the actual drafted changes

Review follows the protocol in `jig-system.md` §Review Pattern. Up to 3 review rounds. Save findings to `integration-review.md`. After approval (or escalation after the final round), advance status to `tasks`.

## Pass 7: Tasks (tasks)

**Output:** `07-tasks.md` (template: `07-tasks.md.template`)

Break the spec changes into implementation tasks. Each task defines what code changes are needed to make the codebase match the updated specs.

**What to do:**

1. Read all drafted specs in `05-spec-drafts/` and the changelog in `05-changelog.md`.
2. For each spec change, identify what code changes are needed to make the codebase consistent with the updated spec.
3. Define implementation tasks. Each task specifies:
   - What to build or change
   - Which spec sections it implements (with file and section references)
   - Deliverables (files created or modified)
   - Acceptance criteria (how to verify the code matches the spec)
   - Dependencies on other tasks
4. Define the dependency graph. Which tasks must complete before others? Which can run in parallel?
5. Keep the format implementation-agnostic -- tasks should be portable to any tracker or execution system.
6. Save to `07-tasks.md`.

**Done when reviewer approves on:**

Inputs the reviewer reads: `07-tasks.md`, `05-changelog.md`, all files in `05-spec-drafts/`.

Criteria:
- `07-tasks.md` contains: a task list with spec traceability, a dependency graph, and a parallelization plan
- Every changelog entry has at least one implementing task
- Every task traces back to a specific spec section
- Dependencies form a valid DAG (no cycles, no missing prerequisites)
- Acceptance criteria are concrete and testable
- The parallelization plan is realistic (no undeclared dependencies between parallel tasks)
- Task granularity is appropriate (not too coarse, not too fine)
- Tasks are concrete enough for an implementing agent to execute without additional design decisions

### Validation / Acceptance Tests

**Required before advancing to Ready.** `07-tasks.md` MUST include at least two test tasks. Verify the scenario-test and exploratory-test beads from the Spec Draft pass exist and are listed as explicit tasks with dependencies on the implementation tasks they validate. If they don't exist yet, create them now:

1. **Scenario-test bead** — end-to-end workflow validation for the spec change (twin or real-claude substrate). Must appear as a task dependent on the implementation tasks it validates. Title convention: `scenario: <work-codename> — <brief>`.
2. **Exploratory-test bead** — operator-facing surface (CLI/API) validation. Must appear as an explicit task. Title convention: `explore: <work-codename> — <brief>`.

Neither this work nor its implementation beads may close until these test beads are also closed.

**Motivation:** hk-37zy8 (wire-up gap found post-merge), hk-aievp and hk-ry3be (dogfood-blockers from 2026-05-18) — all were approved without a scenario test gating the close.

Review follows the protocol in `jig-system.md` §Review Pattern. Up to 3 review rounds. Save findings to `tasks-review.md`. After approval (or escalation after the final round), advance status to `ready`.

## Pass 8: Ready (ready)

**Output:** (none)

Run `kerf square <codename>` to verify all expected artifacts exist. The work is ready for finalization and implementation.

**What to do:**

1. Run `kerf square <codename>` to verify structural completeness.
2. If square fails, identify and fix the missing artifacts.
3. Confirm the work is ready: spec drafts are consistent, changelog is complete, tasks are defined, integration check passed.

**What done looks like:**

- `kerf square` passes with no errors
- All spec drafts, the changelog, integration notes, and task list are complete and on disk
- The work is ready for `kerf finalize`

## Finalization

When this work moves to `ready`, run `kerf square <codename>` to verify, then `kerf finalize <codename>` to package it for implementation. Spec-first works have dual-destination finalization: process artifacts go to `repo_spec_path` and spec drafts are copied to `spec_path`.
