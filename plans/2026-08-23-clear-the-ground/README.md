# How this directory works

Three files matter, and they have different jobs.

| Path | What it is | Who reads it |
|---|---|---|
| `PLAN.md` | Why this program exists and what the eight workstreams are. Changes rarely. | Anyone picking up the program |
| `TASKS.md` | The semi-ordered list of tasks that are **ready to run**. | Charlie — this is the processing list |
| `tasks/` | One file per named task. Self-contained, shaped to become a bead. | Charlie, and whoever implements |

## The rule that keeps this honest

**A task reaches `TASKS.md` only when its dependencies are settled.** Writing the task file comes
first; entering it on the list is a second, separate act. A task file that exists but is not on the
list is a task whose dependencies are still open — that is a normal state, not an oversight.

`tasks/` therefore always holds at least as many files as `TASKS.md` holds rows.

## Task file shape

Every file in `tasks/` carries the same front matter so it converts to a bead without interpretation:

```markdown
---
id: <kebab-case-slug>          # becomes the bead's identifying label
title: <one line, plain English, no internal codes>
type: bug | task | chore
priority: 0 | 1 | 2 | 3
labels: [...]
depends_on: [<slug>, ...]      # other task slugs, or [] when free to start
blocks: [<slug>, ...]
workstream: W1..W8
batch: <n>
---

## Problem
What is wrong today, with a measured fact.

## Scope
The files and symbols in play. Named, not described.

## Done when
A structural acceptance test. Never a line count.

## Limits
What this task must NOT do.
```

## Division of labour

- **Charlie owns pushing beads through.** It converts task files to beads, dispatches them, and
  keeps the list moving. It is not expected to work out the details — if a task file is ambiguous,
  that is a defect in the task file and it comes back here.
- **Planning owns the task files.** Dependencies, ordering, and acceptance criteria are decided
  before a task is listed, not during execution.

## Batches

Tasks are written and reviewed in batches so that a reviewable unit exists before dozens are drafted.
**A batch is a writing unit, not an execution unit.** Do not run a batch as a group — run whatever
`TASKS.md` says is ready.
