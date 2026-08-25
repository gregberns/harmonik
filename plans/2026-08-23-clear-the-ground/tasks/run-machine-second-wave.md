---
id: run-machine-second-wave
title: runAgentLaunch and dispatchDotAgenticNode after runWorkLoop, same treatment, same owner
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-extract-pure-decisions]
blocks: [beadrunone-extract-phases, dotworkflow-extract-decisions]
workstream: W2
batch: 2
---

## Problem

`runWorkLoop` is the worst offender but not a lone one. Cognitive complexity measured 2026-08-24
with the linter this repo configures — `gocognit`, ceiling `min-complexity: 20` in `.golangci.yml`:

| Function | File | Cognitive complexity |
|---|---|---|
| `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 205 |
| `beadRunOne` | `internal/daemon/workloop.go` | 129 |
| `runAgentLaunch` | `internal/daemon/agentlaunch.go` | 111 |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | 100 |

An earlier revision of this table gave 94 / 77 / 69 / 69. Those figures come from no command in this
repo. Use the numbers above and the command in "Done when" item 1, which you can re-run.

`beadRunOne` was 2,289 lines at the start of the delete-and-rewrite program and is 501 now, so this
is not untouched ground — it is ground where the work stopped partway.

The charter names a specific reason the run machine is large: the reviewer is welded into it instead
of being a switchable stage. Unfusing it is explicitly in scope for the program.

**CORRECTION, measured 2026-08-24 — the reviewer is one file over, and the unfusing is still real
work.** An earlier revision of this file said the reviewer is welded into `beadRunOne`, and that
claim reached a dispatched implementer. `grep -in reviewer internal/daemon/workloop.go` returns
nothing, so the claim is false about that file. Do not read that as "there is nothing to unfuse".

`beadRunOne` reaches the reviewer through one call. It calls `driveDotWorkflow` directly, and every
piece of reviewer state lives inside that function in `internal/daemon/dot_cascade_core.go`: the
no-verdict retry counter `reviewerNoVerdictRetries`, the per-axis verdict map
`axisReviewerVerdicts`, the last implementer-reviewer harness `lastImplementerReviewerHarness`, the
`prevAgenticNodeWasReviewer` flag, and the `isReviewer` branches that `nodeIsReviewer` feeds.
`grep -ic reviewer internal/daemon/dot_cascade_core.go` returns 101 against 0 for `workloop.go`.

**There is no pass-through stage between the two.** A review of an earlier run reported a
`runReviewWorkflowStage` in `workloop.go` that forwards to `driveDotWorkflow`. No such symbol exists
anywhere in the tree — `git grep runReviewWorkflowStage` is empty. `beadRunOne` names
`driveDotWorkflow` itself.

**The unfusing is owned, and not by this task.** `dotworkflow-extract-decisions` owns
`driveDotWorkflow` and carries the criterion: the reviewer becomes a stage the cascade calls. Do not
attempt it here and do not treat it as unowned.

## Scope

**Two functions: `runAgentLaunch` in `internal/daemon/agentlaunch.go`, and `dispatchDotAgenticNode`
in `internal/daemon/dot_cascade_core.go`.** Those two files, and nothing else.

**CARVE-OUTS added 2026-08-24.** The other two rows in the table have their own task files and are
NOT done here: `driveDotWorkflow` belongs to `dotworkflow-extract-decisions`, and `beadRunOne` to
`beadrunone-extract-phases`. The table stays whole so the shape of the problem reads, but it is not
the scope. Every criterion in "Done when" is about the two named above.

Same method as `workloop-extract-pure-decisions`: name the state, lift one pure decision at a time,
prove each with a mutation.

**`dispatchDotAgenticNode` shares `dot_cascade_core.go` with `driveDotWorkflow`.** Edit only
`dispatchDotAgenticNode`. If a change reaches into `driveDotWorkflow`, stop — that is
`dotworkflow-extract-decisions`' function, and two lanes re-writing one file collide.

## Done when

The two functions in scope are `runAgentLaunch` and `dispatchDotAgenticNode`. Every criterion below
is about those two. `driveDotWorkflow` and `beadRunOne` are carved out — see Scope.

1. **Each reports a cognitive complexity of 20 or less.** 20 is the ceiling `.golangci.yml` sets
   under `gocognit: min-complexity`; there is no configured ceiling of 30, and no command in this
   repo reports one. Read the number with:

   ```
   .tools/golangci-lint run --enable-only=gocognit ./internal/daemon/...
   ```

   Measured 2026-08-24 on the unmodified tree, that command prints `dispatchDotAgenticNode` at 100.
   It prints nothing for `runAgentLaunch`, whose `//nolint:funlen,gocognit,cyclop` hides it; the
   figure behind the directive is 111. So a silent command is not evidence — criterion 3 is what
   makes this one readable.

2. Each extracted decision has a table test that a deliberate mutation of the production path breaks.

3. **No complexity suppression survives, and none is added.** The finding must be GONE, not moved and
   not re-suppressed. Two checks, both mechanical:
   - `awk -F'\t' '$2=="gocognit" || $2=="cyclop" || $2=="funlen"' tools/lintreport/allow.txt` names
     neither function. Today it holds one `gocognit` row for `dispatchDotAgenticNode` and none for
     `runAgentLaunch`. There is no `cyclop` row for either.
   - The diff adds no complexity `//nolint`. `git diff <base>..HEAD | grep -E
     '^\+.*nolint.*(funlen|gocognit|cyclop)'` prints nothing.

   Deleting a `//nolint` and deleting an allow-list entry are different acts. Do the one that matches
   the function. Never widen the list.

4. **A rename is not a decomposition, and a reviewer will read it as a suppression.** A previous run
   of this task renamed each function, moved the body verbatim into the renamed function under a
   fresh inline `//nolint:funlen,gocognit,cyclop`, and left a pass-through wrapper under the old
   name. Cognitive complexity did not move. The gate went green because the finding had relocated.
   That run was blocked. A wrapper that forwards its arguments unchanged is not a seam.

5. **`runAgentLaunch` carries a stated reason for its size. Answer it; do not delete it.** Its doc
   comment says the launch sequence exists once on purpose, and that splitting it to satisfy a
   complexity threshold would re-create the seams three earlier copies drifted through. The same
   previous run deleted that paragraph and then performed exactly the split it warns about. Two
   standing rules of this program cover that: a move is a move, and doc comments are not edited
   inside a landing that relocates code. If measurement shows the comment is right, that is a
   finding to report — say so and leave the function alone. If you decompose it anyway, say in the
   commit body which seam you created and why it does not re-open the drift the comment names.

6. Unfusing the reviewer into a callable stage is **not** a criterion of this task — the criterion
   lives in `dotworkflow-extract-decisions`, which owns `driveDotWorkflow`, where every piece of
   reviewer state actually is.

## Limits

- **Single owner for this whole task.** `runAgentLaunch`, `dispatchDotAgenticNode` and `runWorkLoop`
  touch overlapping files; two lanes working here will collide. `dispatchDotAgenticNode` also shares
  `dot_cascade_core.go` with `driveDotWorkflow`, which `dotworkflow-extract-decisions` owns — the two
  tasks must not run at the same time.
- Do not start before `workloop-extract-pure-decisions` has landed. It establishes the method and the
  review standard, and doing both at once is how the previous attempts got too large to review.
- Do not move any of these into a new package.
- `run() int` in `cmd/harmonik/main.go` is 774 lines and stays in THIS workstream — PLAN.md §W2.3
  says so by name, and the command-line task `cli-extract-logic` excludes it for the same reason. An
  earlier revision of this file said the opposite, which would have left it owned by nobody. It now
  has its own task, `run-verb-table`; do not do it here and do not do it twice.
