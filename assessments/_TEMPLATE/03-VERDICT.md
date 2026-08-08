# Verdict

> Written last. This is a reasoned judgement, not a tally. An empty findings list never by itself
> means PASS.

## PASS | BLOCK

One line. Then the reasoning — what the evidence supports and why it lands here.

## Commits graded

| What | Commit |
|---|---|
| Branch under audit | `<bare hash>` |
| Other lane merged in | `<bare hash>` |
| Merge result built in the scratch clone | `<bare hash>` |

**A verdict that cannot name its revision is not a verdict.** If any row above is a branch name
rather than a hash, re-run the gate on a pinned tree instead of reporting.

## Not graded

Anything deliberately excluded, and why. If a carve-out was declared in `00-MISSION.md`, it is
repeated here on the face of the verdict — naming the commits not graded and who reviewed them
instead. A partial verdict that says it is partial is honest. One that stays quiet is not.

## What the four legs found

| Leg | Result | Weight in this verdict |
|---|---|---|
| Live-verify | | |
| Break-testing | | |
| Code review | | |
| Merge gate | | |

`make core` is the hard gate — red means BLOCK. `make full` is the merge decision and what CI runs;
a red `full` beside a green `core` is a real answer, not a contradiction. Say which failures are
branch-introduced and which are inherited.

## Residual risk

What could still be wrong that this assessment would not have caught. Every gate has some. Naming it
is what separates a verdict from a reassurance — and it tells whoever holds the release what they
are actually deciding about.

## For the operator

The decision being asked for, and anything outside the assessor's authority that needs a human.
