---
id: reviewer-portability-review
title: Is the reviewer any good, and can another project use it without inheriting our protocols
type: task
priority: 1
labels: [agent-config, review-gate, clear-the-ground]
depends_on: [reviewer-subagent-drift]
blocks: []
workstream: W7
batch: 5
---

## Problem

Two questions the operator raised on 2026-08-23, both unanswered.

**Is it any good?** The reviewer runs on every non-trivial commit and its verdict lands in the commit
trailers. It is 41 KB. Nothing has assessed whether it catches what it claims to catch. There is at
least one recorded case of it doing harm: the normative-doc rule it applied to Go comments generated
a review loop that manufactured comment-only commits, which is why that rule was retired on
2026-08-23.

**Does it travel?** Other projects will need their own reviewer. If harmonik's protocols are baked
into the general review judgement, every other project inherits our commit trailer format, our bead
conventions, our branch model and our skill layout whether it wants them or not.

## Scope

Read `.claude/skills/agent-reviewer/SKILL.md` and `.claude/agents/agent-reviewer.md`, plus the
`agent-config-reviewer` pair.

Report:

1. **Which parts are general review judgement** — correctness, test adequacy, unwanted abstraction,
   spec alignment — and which are harmonik-specific: the trailer format, `Reviewed-By:` /
   `Review-Verdict:`, bead and codename matching, the branch model, the skill-copy conventions.
2. **Whether the split is clean enough to lift.** Could another project take the general half and
   supply its own specifics, or are the two interleaved sentence by sentence?
3. **Evidence it works.** Find commits where the reviewer's verdict changed the outcome. Find any
   where it was wrong. The retired comment rule is one known case; look for others.
4. Whether the JSON verdict schema is doing useful work or is ceremony.

## Done when

The assessment exists as a document under `plans/2026-08-23-clear-the-ground/`, answers all four
points, and states plainly whether the general half is separable — with a recommendation either way.

## Limits

- **Do not restructure the reviewer in this task.** Assess it. Splitting a load-bearing review
  contract is a change that needs its own review, and doing it inside an assessment hides it.
- Do not measure quality by length. A shorter reviewer that misses things is worse.
- The drift fix (`reviewer-subagent-drift`) must land first, or this assesses a file that is already
  known to be wrong.
