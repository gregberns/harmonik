---
id: red-gate-merges-without-reviewer
title: A failed build gate merges the work anyway when the workflow has no review step
type: bug
priority: 1
labels: [daemon, dispatch, gate, review-gate, clear-the-ground]
depends_on: []
blocks: []
workstream: W7
batch: 3
---

## Problem

A build gate that never passes can still end a run as a success and send the work to the merge. What
decides whether it does is the presence of an unrelated node somewhere else in the workflow graph.

**Reproduced on the current branch on 2026-08-24, with a test that is already in the tree.**
`TestDeterministicGateFail_TellsTheImplementerToFixTheFailure`
(`internal/daemon/dot_cascade_gatebackedge_test.go`) drives the real cascade over a graph with a
`commit_gate` node whose shell command always exits 2, and no reviewer node. The gate fails, the
retry cap is reached, and the cascade returns:

```
Success:true  TerminalNodeID:  NeedsAttention:false
Summary: dot: commit_gate cap-hit salvaged — committed tip present; auto-advancing to merge (hk-1vlz F42)
```

The test passes today because it prints that result with `t.Logf` and asserts nothing about it. Run
it yourself: `go test ./internal/daemon/ -run TestDeterministicGateFail_TellsTheImplementerToFixTheFailure -v -count=1`.
It takes about 15 seconds.

`Success: true` is what sends the work to the merge. `internal/daemon/workloop.go` switches on
`dotResult.success` and feeds `runexec.ModeSuccess` to the merge bridge on that branch.

### The code

`internal/daemon/dot_cascade_core.go`, in `driveDotWorkflow`, inside the `case decision.Failed:` arm:

```go
if decision.CompletionReason == "cap_hit" && currentNodeID == "commit_gate" && !graphHasReviewerNode(nodesByID) {
    if salvageHead, salvageErr := resolveDotWorktreeHEAD(ctx, runner, wtPath); salvageErr == nil &&
        salvageHead != "" && salvageHead != parentSHA {
        return dotWorkflowResult{
            success: true,
            summary: "dot: commit_gate cap-hit salvaged — committed tip present; auto-advancing to merge (hk-1vlz F42)",
        }
    }
}
```

`graphHasReviewerNode` and `nodeIsReviewer` are in `internal/daemon/dot_cascade_helpers.go`. A node
counts as a reviewer when `agent_type="reviewer"` or `handler_ref="claude-reviewer"`.

Four conditions must hold together, and all four are measured:

1. **`cap_hit`.** `internal/workflow` `DecideNextNode` sets `CompletionReason = "cap_hit"` only when
   `internal/core` `SelectNextEdge` returns `FailureClassCompilationLoop`, which it returns only for
   `"traversal cap reached"` on the highest-weight matching outgoing edge. It does **not** fall
   through to a lower-weight fallback edge when the cap is reached — it fails.
2. **The node ID is the literal string `commit_gate`.** Not the node type, not `gate_ref` — the ID.
3. **No reviewer node anywhere in the graph.** Not on the path from the gate. Anywhere.
4. **The worktree HEAD moved past the run's parent commit**, so the implementer committed something.

**Condition 1 means the gate was red.** In the production graph
(`/Users/gb/github/harmonik/workflow.dot`) the only capped edges out of `commit_gate` are the two
failure routes — `commit_gate -> implement` on a deterministic failure, capped at 3, and the
`commit_gate -> commit_gate` self-loop on a transient failure, capped at 2. The success edge
`commit_gate -> review` carries no cap. So a cap hit at `commit_gate` is proof the gate never went
green.

### Why nobody has been bitten yet

No `.dot` file in this repository pairs a `commit_gate` node with a graph that has no reviewer node.
Checked across every tracked `.dot`: the five that declare a `commit_gate` node — `workflow.dot`,
`sonnet-triple-review.dot`, `internal/daemon/standard-bead.dot`, `specs/examples/standard-bead.dot`
and `specs/examples/sub-workflow-example.dot` — all declare a reviewer node too. So the deployment is
fail-closed today — **by accident of topology, not by design.**

The unsafe shape is one edit away and nothing announces it. The daemon resolves a run's graph in
`internal/daemon/workloop_runplan.go` `resolveWorkflow`, and a graph can arrive three ways: a queue
item's `workflow_ref`, a bead label of the form `dot:<name>`, or the project's own `workflow.dot` at
the repository root. Any project that writes its own `workflow.dot` with a `commit_gate` and no
review step gets a build gate that does not gate, and the graph validator will not say a word.

### The rule this already breaks

This is not only a judgment call about shape. `specs/execution-model.md` §4.3 **EM-015b** says
`run_completed` emits "when the run enters a node in `terminal_node_ids`", and `run_failed` emits
"when the cascade returns `FAIL` (per §4.10.EM-046a or §4.10.EM-043)". §4.10.EM-043 is the per-edge
traversal cap. The salvage returns `success: true` with an empty `terminalNodeID`, so the run reports
`run_completed` after a cascade FAIL and without entering any terminal node. The spec is normative
here and the code is expected to match it.

### How it got this shape

Two commits, both in the log.

- `ea796f801` added the salvage. Its problem was real: a cap hit at `commit_gate` discarded the
  implementer's commit **silently**, so committed work was lost. It had no reviewer check at all.
- `31020375f` narrowed it with `!graphHasReviewerNode`, after three confirmed production merges of
  unreviewed work.

So the first commit was too broad, and the second narrowed it by topology instead of by principle.
What is left is exactly the slice where nothing else is watching.

## Scope

Make the gate's own outcome decide whether a merge may happen, and let no other node's presence
change that.

**`internal/daemon/dot_cascade_core.go`, `driveDotWorkflow`:**

- Delete the salvage branch shown above. A `cap_hit` at `commit_gate` returns
  `success: false, needsAttention: true`, the same as a cap hit at any other node. There is no
  green-gate cap hit at `commit_gate` left to protect (see condition 1 above), so nothing worth
  salvaging is lost.
- Keep the HEAD probe and change what it is for. On a `cap_hit` failure, call
  `resolveDotWorktreeHEAD(ctx, runner, wtPath)`. When it returns a SHA that is neither empty nor
  equal to `parentSHA`, name that SHA in the failure summary, so an operator reading the run can find
  the commit. When the probe errors, leave the SHA out and still fail. **This is the part that keeps
  the original intent** — the work must not vanish without a word — while dropping the action that
  was wrong, which was merging it.

**`internal/daemon/dot_cascade_helpers.go`:**

- Delete `graphHasReviewerNode`. The branch above is its only caller.
- **Keep `nodeIsReviewer`.** It has three other callers that route work rather than decide merges:
  the per-node `isReviewer` dispatch and the `prevNode` check in `dot_cascade_core.go`, and
  `upstreamReviewerNodeIDs` in the helpers file.

### Committed work is not lost by this change

Measured, because it is the one thing that could make the removal worse than the defect:

- `internal/daemon/workloop.go` already resolves the worktree HEAD on the failure branch and stores
  it in `runTipSHA`, which goes into the `run_completed` / `run_failed` payload and into the
  session-data `CommitSHA`. The tip is reported.
- `internal/lifecycle` `ReapBranches` skips an unmerged `run/*` branch that is younger than
  `OrphanMaxAge`, which defaults to 30 days. The branch survives.

So after this change a red gate produces a failed run, a named tip SHA, and a branch that is still
there. That is the correct outcome for a red gate, and it is not the silent discard that
`ea796f801` set out to fix.

### Why the fix is at the gate and not at graph load

`internal/workflow/dot` `Validate` is a genuine single choke point — every path that turns `.dot`
bytes into an executable graph passes through it — so a load-time rule is possible. It is still the
wrong fix, for three measured reasons:

1. **It would keep the coupling.** A rule of the form "a graph with a `commit_gate` must also carry a
   reviewer" still makes a build gate's safety depend on an unrelated node. It moves the check
   earlier without removing the dependence.
2. **Reviewer-less graphs are legal and shipped.** `internal/daemon/no-review-bead.dot` is embedded
   in the binary and selected by a queue item's `workflow_mode: "single"` or the legacy
   `workflow:single` bead label, through `resolveNoReviewWorkflow`. `scenarios/_workflows/smoke-one-node.dot`
   and the daemon test fixture `dotFixtureGraph` are reviewer-less too. A blanket rule breaks all
   three.
3. **The validator has no concept of a commit gate.** It keys nothing on a node's ID, and the
   salvage keys on the literal ID `"commit_gate"`. A coextensive rule would have to encode that same
   literal name — a law about a name, in a file whose other rules are about shape.

## Done when

1. **A new test fails against the unfixed tree, then passes.** Add
   `TestRedCommitGateWithNoReviewerNodeDoesNotMerge` in a new file
   `internal/daemon/dot_cascade_redgate_nomerge_test.go`. It drives the real cascade over a graph
   that has a `commit_gate` node and no reviewer node, with a gate `tool_command` that always exits
   non-zero, a `traversal_cap` low enough to be reached, and an implementer script that commits a
   new file on every entry so HEAD moves past `parentSHA`. It asserts `result.Success == false` and
   `result.NeedsAttention == true`.
   **Copy the fixture rather than inventing one:** `internal/daemon/dot_cascade_gatebackedge_test.go`
   already has exactly this graph as `gateBackEdgeDOT` and exactly this script as
   `gateBackEdgeScript`, and it already reaches the salvage. Reuse `rlcFixtureSetup`,
   `daemon.ExportedTestRuntime`, and `daemon.ExportedDriveDotWorkflow` from the same file. No build
   tag — that file has none and this one must not add one.
   Run it before touching `dot_cascade_core.go`:
   `go test ./internal/daemon/ -run TestRedCommitGateWithNoReviewerNodeDoesNotMerge -count=1 -v`.
   **Paste the observed RED output into the commit body**, including the line that quotes
   `dot: commit_gate cap-hit salvaged`. A test that was never seen red proves nothing here.

2. **The test that had the evidence stops throwing it away.** In
   `internal/daemon/dot_cascade_gatebackedge_test.go`,
   `TestDeterministicGateFail_TellsTheImplementerToFixTheFailure` currently does
   `t.Logf("cascade result: %+v", result)` and checks nothing. Add an assertion that the cascade did
   not report success. One line, at the place where this defect hid for the whole of its life.

3. **The topology condition is gone from the source, not disabled.** Both commands print nothing and
   exit non-zero:
   - `grep -rn 'graphHasReviewerNode' --include='*.go' .`
   - `grep -rn 'cap-hit salvaged' --include='*.go' .`

4. **A cap hit that left a commit names the commit.** The new test asserts that the failure summary
   contains the worktree HEAD SHA. An operator must be able to read the run's summary and find the
   work without going to look for a branch.

5. **No graph file changes.** `git diff --name-only <base>..HEAD | grep '\.dot$'` prints nothing.
   Reviewer-less graphs stay legal and `internal/daemon/no-review-bead.dot` is untouched.

6. **No suppression anywhere in the diff.** `git diff <base>..HEAD | grep '^+' | grep nolint` shows
   no `funlen`, `cyclop`, or `gocognit`, and `git diff <base>..HEAD -- tools/lintreport/allow.txt`
   adds no line. `driveDotWorkflow` carries a tolerated `gocognit` finding today and this task does
   **not** clear it — the two decomposition tasks own that. Do not claim it, and do not delete its
   allow-list line.

7. **`make full` is green**, and it is the merge decision. `go test ./internal/daemon/ -count=1`
   green on its own is not enough.

8. **The commit body names the rule the old branch broke** — `specs/execution-model.md` §4.3
   EM-015b, quoted above — so the next reader does not re-open the question from first principles.

## Limits

- **Do not add a rule to `internal/workflow/dot` `Validate`.** The three reasons are in Scope. If you
  believe one of them is wrong, say so in a bead rather than doing both fixes.
- **Do not delete `nodeIsReviewer`, `upstreamReviewerNodeIDs`, or any other reviewer-aware code.**
  Those route work. Only the one that decides a merge goes.
- **Do not touch the other `success: true` returns in the cascade** — the verdict-absent salvage, the
  advisory-`REQUEST_CHANGES` exemption, and the approved-and-done path. Each has its own history and
  its own bead. If one of them looks wrong while you are in the file, file it; do not change it here.
- **Do not restructure `driveDotWorkflow`.** `dotworkflow-extract-decisions` and
  `run-machine-second-wave` own that function. This change is a deletion of about ten lines inside
  one existing branch.

## Traps

- **`internal/daemon/dot_cascade_core.go` is held by `run-machine-second-wave`** under the ready
  list's single-owner rule for the run machine. This change is small enough to rebase onto whatever
  lands. Land it **before** the decomposition rather than after: a refactor that carries this branch
  forward buries the defect inside new code, and the next reader will read it as intended behaviour
  because it survived a rewrite.
- **No dispatched run can prove this either way.** The daemon binary running in production was built
  well before this fix and cannot carry it. It does not matter — the whole defect and the whole fix
  are observable in-repo with `go test`, which is how the finding above was measured. Do not wait on
  a redeploy and do not try to reproduce it through a queue.
- **Do not write a new graph fixture from scratch.** The one you need exists and is already wired to
  the real cascade driver. Copying `gateBackEdgeDOT` costs minutes; a hand-written graph that fails
  `dot.Validate` for an unrelated reason costs an hour.
