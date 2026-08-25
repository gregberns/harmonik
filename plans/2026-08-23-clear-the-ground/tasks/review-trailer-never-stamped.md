---
id: review-trailer-never-stamped
title: Every commit the pipeline produces says no reviewer was reached, and one always was
type: bug
priority: 0
labels: [daemon, review-gate, clear-the-ground]
depends_on: []
blocks: []
workstream: W7
batch: 3
---

## Problem

The dispatch pipeline reaches a reviewer on almost every run. The commit it produces says it did
not. So the merged history of this repository understates its own review coverage, and a reader
cannot tell a commit nobody reviewed from a commit a reviewer approved.

Measured 2026-08-24 across all 5,885 commits on all branches:

| Shape | Count |
|---|---|
| Carry `Reviewed-By: none` and a `NOT_REVIEWED` verdict | **176** |
| Carry a real reviewer name | 1,573 |
| Carry **both** a placeholder and a real reviewer | **0** |

That last row is the measurement that matters. The daemon is supposed to amend the commit after the
reviewer returns, replacing the placeholder. The amend appends rather than replaces, so if it had
ever run on a placeholder-bearing commit it would have left both lines behind. None exists. **The
stamp has never once executed on a real pipeline run.**

This is the third time the finding has been filed. Two earlier records said the same thing and both
were closed without the behaviour changing.

**Reproduced end to end on a fresh commit, 2026-08-24.** Run
`01a03538-b4c5-7979-83af-5ceb786f9d41` produced two `reviewer_verdict` events, both APPROVE, at
19:34:13Z and 19:35:06Z. The commit it landed, `296c759d3` on `work/charlie-batch-1`, reads
`Reviewed-By: none` with a `NOT_REVIEWED` verdict, and it carries exactly one `Reviewed-By:` line,
so the amend did not even append beside the placeholder. It did not run at all.

This single run is better evidence than the 176-commit count. The count leaves room to argue those
runs never reached a reviewer. This leaves none: same run, same minutes, reviewer approved twice,
commit says nobody reviewed it.

## Why it never fires

Three separate defects sit in a line. All three were confirmed against the current branch.

**1. The implementer is told to write the false trailer.** `internal/workspace/templates/agent-task.md.tmpl`
instructs the implementer to keep `Reviewed-By: none` and a `NOT_REVIEWED` verdict exactly as
written. This text used to live in a Go file and now lives in the template, so a fix must edit the
template.

**2. The stamp is wired to a path production does not take.** The `approveVerdict` field on the
cascade result (`internal/daemon/dot_cascade_helpers.go`) is assigned in exactly one place:
`internal/daemon/dot_cascade_core.go`, inside the no-progress salvage branch that fires only when an
iteration makes no headway. The ordinary terminal-node close return never populates it. Then
`internal/daemon/workloop.go` early-returns when `approveVerdict` is nil — which is every normal
run. **This is the whole defect.** The other two are real but neither one alone would hide the
verdict.

**3. The stamp appends where it must replace.** `internal/runmerge/reviewtrailers.go`
`AppendReviewTrailersToHEAD` concatenates the new trailer onto the existing message. The moment
defect 2 is fixed, every stamped commit gets two `Reviewed-By:` lines — the shape
`scripts/commit-msg-gate.sh` refuses. So defect 3 must be fixed in the same landing as defect 2, not
after it.

## Scope

Make a commit produced by the pipeline carry the verdict the reviewer actually returned.

- Populate the approve verdict on the ordinary terminal-close return, not only on the salvage
  branch.
- Make the trailer write replace the placeholder instead of appending beside it. Rename the function
  so its name states what it does; `Append…` will be a lie once it replaces.
- Correct the implementer template so it stops asking for a trailer that claims an absent review.
  Keep the genuine absent-reviewer form for the case where no reviewer really is reached — that
  case is real and the wording for it is settled.

## Traps

**Do not cherry-pick from the abandoned attempt.** The previous run left work on
`run/01a0279c-9fb3-7e15-be69-e1c64a5c7496`. The review gate blocked it, correctly: its trailer
writer reversed the whole commit message body, moving the subject line to the bottom of every
commit it touched — worse than the defect it set out to fix. Its tests passed only because they
asserted that a trailer was present and never that the subject and body survived. The attempt also
drops two protections the current branch has: the retry around `gitprobe.Output`, and the
fork-safety wrapper around the commit call. Read the branch if it helps; do not adopt it.

**The reversal bug is not on the current tree.** The trailer writer here is plain concatenation and
reorders nothing. Do not go looking for a reversal to fix.

**You cannot prove the FIX through the pipeline, but you CAN reproduce the DEFECT through it.**
No running daemon can carry a fix that has not landed and been redeployed. That is true of every
daemon-behaviour fix at any build age, so it is not a property of this one and it is not a reason to
hand-land the code. To reproduce the defect on demand: dispatch any bead, wait for a
`reviewer_verdict` event with APPROVE, then read the commit the run landed. Run
`01a03538-b4c5-7979-83af-5ceb786f9d41` is the worked example (see Problem above).

What follows is a split, not a hand landing. Done-when 2 through 6 are in-repo tests. A dispatched
implementer writes them and the commit gate runs them in the worktree, so they are routable today.
Only done-when 1 needs the fixed daemon to be running, which is a redeploy step AFTER the batch
merges. Carry done-when 1 as an explicit post-redeploy verification and do not let it block the
rest.

## Done when

1. **(Post-redeploy — does not block the landing.)** A commit produced by a pipeline run whose
   reviewer returned APPROVE carries that reviewer's name and that verdict. Not a placeholder.
   Verify this after the batch merges and the daemon is redeployed, not during the run that writes
   the fix.
2. A test pins the ordinary path, not the salvage path. There is already a scenario test that drives
   implementer → APPROVE → terminal close and that holds the exported approve-verdict field but
   asserts only which terminal node was reached. Assert the verdict is present. This one assertion
   pins the exact regression that has now escaped three times, and it is nearly free.
3. A test proves the subject line and the message body survive the trailer write byte for byte. The
   blocked attempt failed precisely here and its tests did not notice, so an assertion that a
   trailer is present is not enough.
4. No commit the pipeline produces carries two `Reviewed-By:` lines. Assert it.
5. The genuine absent-reviewer path still works: when no reviewer is reached, the commit still says
   so and still does not claim an approval.
6. The commit body records the count of `Reviewed-By: none` commits at the time of the fix, so the
   next reader can tell whether the number stopped growing.
