---
name: bug
description: Investigate and specify a fix for a defect.
version: 2
phase: bug-fix
tools: []
composable: false
status_values:
  - reported
  - research
  - reproducing
  - root-cause
  - fix-spec
  - ready
passes:
  - name: "Report"
    status: reported
    output: ["01-report.md"]
  - name: "Research"
    status: research
    output: ["02-research.md"]
  - name: "Reproduce"
    status: reproducing
    output: ["03-reproduction.md"]
  - name: "Root Cause"
    status: root-cause
    output: ["04-root-cause.md"]
  - name: "Fix Spec"
    status: fix-spec
    output: ["05-fix-spec.md"]
  - name: "Ready"
    status: ready
    output: []
file_structure:
  - spec.yaml
  - SESSION.md
  - 01-report.md
  - 02-research.md
  - 03-reproduction.md
  - 04-root-cause.md
  - 05-fix-spec.md
---

# Bug Investigation Jig

## Overview

Something is broken. This jig guides you through understanding what is wrong, proving it, finding the cause, and defining the fix -- before any code is written. It produces a complete investigation record and fix specification through six passes.

Each pass produces one or more files. If work is not captured in a file, it is lost when the session ends.

## Pass 1: Report (reported)

**Output:** `01-report.md`

Capture a clear, precise bug report that establishes the facts before any investigation begins.

**What to do:**

1. Read the bug report or user description carefully. Identify what behavior was observed and what behavior was expected.
2. Capture the reported behavior and expected behavior as precise, observable statements. "The CLI returns exit code 0 when validation fails" not "it doesn't work right."
3. Identify the affected area -- the command, endpoint, module, or subsystem where the bug manifests.
4. Record environment details if known: OS, versions, configuration, dependencies.
5. Collect any existing evidence: error messages, log output, stack traces, screenshots, or links to related issues.
6. Record steps to reproduce if the reporter provided them. If not, note that reproduction steps are unknown -- Research and Reproduce passes will establish them.
7. Assess severity and impact:
   - **Critical** -- data loss, security vulnerability, or complete feature unavailability
   - **High** -- major functionality broken, no workaround
   - **Medium** -- functionality impaired, workaround exists
   - **Low** -- cosmetic, minor inconvenience, or edge case
8. Determine whether this is actually a bug, a feature gap, or a misunderstanding. If it is not a bug, document that conclusion and stop.
9. Save to `01-report.md`. Advance status to `research`.

**What done looks like:**

- `01-report.md` contains: reported behavior, expected behavior, affected area, environment (if known), evidence collected, steps to reproduce (if known), severity with justification, and assessment (confirmed bug / feature gap / needs investigation)
- The report is precise enough that another agent reading it could begin investigating without asking clarifying questions

## Pass 2: Research (research)

**Output:** `02-research.md`

Investigate the codebase to understand the problem area before attempting reproduction. Structure the investigation around explicit questions.

**What to do:**

1. Based on the bug report, formulate 3-5 specific investigation questions. These should guide the research and produce concrete answers. Examples:
   - "What code handles this endpoint/command/path?"
   - "How is this input validated and processed?"
   - "When was this code last modified, and what changed?"
   - "Are there existing tests covering this behavior?"
   - "Are there related bug reports or known issues?"
2. For each question, investigate systematically:
   - Read the relevant source code. Trace the execution path from the entry point through the affected area.
   - Check git history for recent changes to the affected code.
   - Search for related issues, error patterns, or similar bugs elsewhere in the codebase.
   - Review existing tests to understand what is and is not covered.
3. Document each question and its findings. Be specific -- name files, functions, line ranges, commit hashes.
4. Identify any surprising discoveries or additional questions that emerged during research.
5. Save to `02-research.md`. Advance status to `reproducing`.

**What done looks like:**

- `02-research.md` contains: each investigation question with its findings, relevant code paths identified, recent changes noted, test coverage assessment, and any additional questions or surprises discovered
- The research provides enough understanding of the problem area to inform reproduction and root cause analysis

## Pass 3: Reproduce (reproducing)

**Output:** `03-reproduction.md`

Build a minimal reproduction case that proves the bug exists and isolates the triggering conditions.

**What to do:**

1. Using the bug report and research findings, define exact steps to reproduce the bug. Number each step. Include specific inputs, commands, or actions.
2. Execute the steps and confirm the bug occurs. Record the actual output.
3. Narrow to the minimal reproduction -- the smallest set of steps, inputs, and configuration that triggers the bug. Remove anything unnecessary.
4. Document environment requirements: OS, versions, configuration, dependencies, or state that must be present for the bug to manifest.
5. If the bug cannot be reproduced:
   - Document every approach attempted and why each failed.
   - Note whether the bug is intermittent, environment-specific, or dependent on state that is difficult to recreate.
   - Escalate to the user with a clear summary of what was tried. Do not proceed to Pass 4 without either a reproduction or an explicit decision from the user to continue.
6. Save to `03-reproduction.md`. Advance status to `root-cause`.

**What done looks like:**

- `03-reproduction.md` contains: numbered steps to reproduce, minimal reproduction case, environment requirements, observed output, and reproduction status (reliably reproduced / intermittent / not reproduced)
- Another agent or developer can follow the steps and observe the same bug

## Pass 4: Root Cause (root-cause)

**Output:** `04-root-cause.md`

Based on research and reproduction, identify why the bug exists -- not just where the code fails, but the underlying defect.

**What to do:**

1. Trace the reproduction steps through the code path identified during Research. Follow execution from the entry point to the failure.
2. Identify the specific defect: what the code does wrong and why. Explain the logical error, incorrect assumption, missing check, race condition, or other flaw.
3. Determine when the bug was introduced, if possible. Check git history for the commit that introduced or exposed the defect.
4. Assess the blast radius -- what other code paths, features, or behaviors depend on or are affected by the broken code. Cross-reference with Research findings on related patterns.
5. Summarize the root cause in one sentence that another developer would understand without reading the full analysis.
6. Save to `04-root-cause.md`. Advance status to `fix-spec`.

**What done looks like:**

- `04-root-cause.md` contains: one-sentence root cause summary, detailed explanation of the defect, execution trace from entry to failure (with file/function references), when introduced (if determinable), and blast radius assessment
- The root cause explains *why* the bug exists, not just *where* the code fails

### Review Criteria

After completing this pass, spawn a review sub-agent with:
- `04-root-cause.md`
- `02-research.md` (for cross-reference)
- `03-reproduction.md` (to verify the trace matches the reproduction)

The reviewer checks:
- The root cause explains *why*, not just *where*
- The execution trace is consistent with the reproduction steps
- The blast radius assessment is complete
- Research findings are incorporated, not contradicted
- The one-sentence summary accurately captures the defect

Up to 3 review rounds. Save findings to `root-cause-review.md`.

## Pass 5: Fix Spec (fix-spec)

**Output:** `05-fix-spec.md`

Specify the fix so that an implementing agent knows exactly what to change, without writing implementation code.

**What to do:**

1. Propose the fix approach. Describe the change at the level of logic and structure -- what condition to add, what function to modify, what data flow to correct. Do not write implementation code.
2. If there are multiple viable approaches, list them with tradeoffs and recommend one.
3. Identify risks or side effects of the fix. Will it change any public API? Affect performance? Alter behavior in cases that currently work correctly?
4. If this is a spec-first project: identify what spec changes are needed to reflect the correct behavior.
5. Define acceptance criteria -- concrete, testable conditions that confirm the fix works:
   - The original reproduction case passes after the fix.
   - Boundary conditions around the fix are covered.
   - Related patterns identified in Root Cause are addressed or explicitly scoped out.
6. Define tests to add or modify:
   - Tests that verify the fix resolves the bug.
   - Regression tests that prevent recurrence.
   - Tests for blast radius areas, if applicable.
7. Estimate scope: which files change, approximate number of changes, and complexity (trivial / straightforward / involved).
8. Save to `05-fix-spec.md`. Advance status to `ready`.

**What done looks like:**

- `05-fix-spec.md` contains: proposed fix approach, alternatives considered (if any), risks and side effects, spec changes needed (if spec-first), acceptance criteria, tests to add/modify, and scope estimate
- An implementing agent can read this file and know exactly what to change and how to verify the fix

### Validation / Acceptance Tests

**Required before advancing to Ready.** File at least two test beads and record their IDs in `05-fix-spec.md`:

1. **Scenario-test bead** — one bead verifying the bug is fixed end-to-end (twin or real-claude substrate). The bead must: name the reproduction steps from `03-reproduction.md`, define the CLI invocation or system trigger, and state the observable terminal condition that confirms the fix (e.g. no longer produces the failure mode, or produces the expected JSONL record / file). Title convention: `scenario: <bug-codename> — regression: <brief>`.
2. **Exploratory-test bead** — one bead exercising the fixed operator-facing surface to verify no regression in adjacent behavior. Title convention: `explore: <bug-codename> — <brief>`.

Use `br create "<title>" --type task --label scenario-test` and `br create "<title>" --type task --label exploratory-test`.

The bug bead may not close until these test beads are also closed.

**Motivation:** hk-37zy8 (handler-pause policy goroutine — wiring gap not caught at review because no scenario test existed). hk-aievp (stale-pane pollution) and hk-ry3be (no agent-termination detection) — both dogfood-blockers from the 2026-05-18 session that had no end-to-end test gating the bead close.

### Review Criteria

After completing this pass, spawn a review sub-agent with:
- `05-fix-spec.md`
- `04-root-cause.md` (to verify the fix addresses the root cause)
- `01-report.md` (to verify acceptance criteria cover the reported behavior)

The reviewer checks:
- The fix addresses the root cause, not just the symptom
- Acceptance criteria are concrete and testable
- Risks and side effects are realistic
- Scope estimate is plausible given the root cause and proposed approach
- If spec-first: spec changes are identified and consistent with the fix
- At least one scenario-test bead and one exploratory-test bead are listed with their IDs

Up to 3 review rounds. Save findings to `fix-spec-review.md`.

## Pass 6: Ready (ready)

**Output:** (none)

Run `kerf square <codename>` to verify all expected artifacts exist. The bug investigation is complete and the fix spec is ready for implementation.

**What to do:**

1. Run `kerf square <codename>` to verify all expected artifacts exist.
2. If square fails, return to the appropriate pass to produce the missing artifacts.
3. Once square passes, the bug investigation is complete.

**What done looks like:**

- `kerf square` reports SQUARE
- All 5 artifact files exist and are populated
- The fix spec is specific enough for an implementer to act on without additional context

## Finalization

When this work moves to `ready`, run `kerf square <codename>` to verify, then `kerf finalize <codename>` to package the investigation and fix spec for implementation handoff.
