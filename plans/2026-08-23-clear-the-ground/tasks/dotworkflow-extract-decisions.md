---
id: dotworkflow-extract-decisions
title: driveDotWorkflow takes 24 parameters and carries 21 loop variables through one 388-line loop
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [workloop-extract-pure-decisions, run-machine-second-wave]
blocks: [beadrunone-extract-phases]
workstream: W2
batch: 3
---

## Problem

`internal/daemon/dot_cascade_core.go` `driveDotWorkflow` is **495 lines** (lines 29–523) and takes
**24 parameters**, one per line:

```go
func driveDotWorkflow(
	ctx context.Context, env runloop.RunEnv, ports runloop.RunPorts, handles runloop.SharedHandles,
	runID core.RunID, beadID core.BeadID, beadRecord core.BeadRecord, beadTitle string,
	beadDescription string, activeRepo string, wtPath string, parentSHA string, graph *dot.Graph,
	descriptor core.WorkflowDescriptor, resolvedModel string, resolvedEffort string,
	piProfile projectconfig.PiProfileConfig, extraContext string, baseBranch string,
	runner tmux.CommandRunner, workerBinaryPath string, workerHookSock string,
	workerSessionName string, workerSessionCwd string,
) dotWorkflowResult
```

Measured 2026-08-24 from the tree:

| Measure | Ceiling in `.golangci.yml` | `driveDotWorkflow` today |
|---|---|---|
| `cyclop` (cyclomatic) | 15 | **94** |
| `gocognit` (cognitive) | 20 | **205** |
| `funlen` | 100 lines / 60 statements | **495 lines** |

495 lines and cyclomatic 94 agree with what `run-machine-second-wave` recorded. **The 24 parameters
and the cognitive figure of 205 were not recorded anywhere**, and they are the two numbers that
matter: this is the widest signature left in the repo now that `runWorkLoop` is down to four, and its
cognitive complexity is second only to `runWorkLoop`'s.

**This is `runWorkLoop`'s disease one layer down, and the same two moves cure it.** Twenty-one
mutable locals are carried through a single `for visits := 0; visits < dotMaxNodeVisits; visits++`
loop that runs from line 130 to line 516: `currentNodeID`, `prevNodeID`, `iterationCount`,
`claudeSessionID`, `lastDiffHash`, `priorIterHeadSHA`, `priorVerdict`, `priorVerdictFlags`,
`priorVerdictNotes`, `lastGatePassed`, `lastGateNotes`, `lastGateClass`, `lastGateNodeID`,
`reviewerNoVerdictRetries`, `lastImplementerReviewerHarness`, `axisReviewerVerdicts`,
`noProgressGuardOff`, `noProgressGuardCap`, `consecutiveNoProgressCount`,
`prevAgenticNodeWasReviewer`, `cycles`. Nothing can be lifted out of the loop without dragging the
frame with it.

**The good news is that the hardest decision is already extracted and delegated.**
`workflow.DecideNextNode(graph, currentNodeID, outcome, run, cycles)` at line 464 is pure, and the
loop already calls it. That is the shape to copy, and it is a second worked example beside
`internal/orchestrator.SelectNextQueue`. What remains inside the loop is a `switch node.Type` with
four arms, and one of them is not like the others: `NodeTypeNonAgentic` is about 48 lines,
`NodeTypeGate` 17, `NodeTypeSubWorkflow` 42, and `NodeTypeAgentic` about **219**.

Two decisions are pure today and are sitting in effect code:

- **The no-progress guard.** Lines 114–123 — two declarations and a `switch` over
  `graph.NoProgressGuard` handling `"off"`, `"capped:<n>"` and the default, producing
  `noProgressGuardOff` and `noProgressGuardCap`, with the counter they drive
  (`consecutiveNoProgressCount`) declared on line 124. It parses a string into two values and touches
  nothing else. (Lines 91–99 are the plain local declarations — `lastDiffHash`, `priorIterHeadSHA`,
  `priorVerdict`, `priorVerdictFlags` — not the guard; an earlier revision of this file pointed
  there.)
- **Whether this iteration made progress.** The rule reads `lastDiffHash`, `priorIterHeadSHA` and the
  guard settings and produces `consecutiveNoProgressCount` and a stop-or-continue answer.

**Correction to `run-machine-second-wave`.** That task says the reviewer is welded into `beadRunOne`
and asks for the unfusing there. It is not in `beadRunOne`. The reviewer state — `axisReviewerVerdicts`,
`reviewerNoVerdictRetries`, `prevAgenticNodeWasReviewer`, `lastImplementerReviewerHarness` — is all
in this function, inside the agentic arm. If the reviewer becomes a stage the loop calls, it happens
here.

**The lint gate forces a whole landing.** `tools/lintreport/allow.txt` carries digest
`374d45e46d55…`, linter `gocognit`, comment `# internal/daemon/dot_cascade_core.go:29
driveDotWorkflow`. Since the tolerated findings were re-keyed by content identity, that key is
`sha256(linter, finding text, enclosing symbol body)` — so any body edit that leaves the finding
present produces a key the list does not hold, the gate fails, and adding the new key trips
`scripts/lint-allow-ratchet.sh`. Unlike `beadRunOne`, this function has **no `//nolint` directive**
today. Add one with a stated reason if you need intermediate commits to stay green, and delete it at
the end.

## Scope

- `internal/daemon/dot_cascade_core.go` — `driveDotWorkflow` only.
- `internal/daemon/workloop.go` line 458 — the one production call site, inside `beadRunOne`.
- `internal/daemon/export_dotrun_test.go` — four call sites, each passing all 24 arguments
  positionally.

Two moves, in this order, the same pair that worked on `runWorkLoop`:

1. **Name the inputs and the state.** The 24 parameters are four groups, and every one of the 24
   is in exactly one of them:
   - **The run's identity and collaborators (9)** — `ctx`, `env`, `ports`, `handles`, `runID`,
     `beadID`, `beadRecord`, `beadTitle`, `beadDescription`.
   - **The workspace and the revision (4)** — `activeRepo`, `wtPath`, `parentSHA`, `baseBranch`.
   - **The workflow to run and how to run it (6)** — `graph`, `descriptor`, `resolvedModel`,
     `resolvedEffort`, `piProfile`, `extraContext`. This group was missing from an earlier revision
     of this file, which named three groups covering only 18 parameters. It is not filler: `graph`
     drives the whole loop, `descriptor` identifies the workflow on every emitted event, and
     `resolvedModel`, `resolvedEffort`, `piProfile` and `extraContext` are handed to each agentic
     node. Whatever narrower signature you build, this group needs a home — most likely one value
     that says *which workflow, and with what agent settings*.
   - **The worker session (5)** — `runner`, `workerBinaryPath`, `workerHookSock`,
     `workerSessionName`, `workerSessionCwd`.

   9 + 4 + 6 + 5 = 24. The 21 loop variables become a named type, separate from all four groups.
   `beadTitle` and `beadDescription` are already `beadRecord.Title` and `beadRecord.Description` at
   the production call site — check whether they need to be parameters at all.
2. **Extract one decision at a time**, each with a table test, `DecideNextNode` as the model.

## Done when

1. `driveDotWorkflow`'s signature is **6 parameters or fewer**, and the four `export_dotrun_test.go`
   call sites read as named fields rather than 24 positional arguments.
2. The loop's carried state is a named type, not a set of locals in the enclosing frame.
3. `driveDotWorkflow` is under all three ceilings: `cyclop` below 15, `gocognit` below 20, `funlen`
   below both 100 lines and 60 statements. Quote all three in the commit body, measured with any
   suppression gone. `~/go/bin/gocognit internal/daemon/` ignores a `//nolint` directive and
   `golangci-lint` obeys it, so strip the directive before you trust a `golangci-lint` result.
4. The no-progress guard parse is a pure function with a table test covering `"off"`, `"capped:0"`,
   `"capped:3"`, a malformed `"capped:"` value and the empty default.
5. **The reviewer is a stage the cascade calls, not logic inlined into it.** The reviewer state this
   loop carries — `axisReviewerVerdicts`, `reviewerNoVerdictRetries`, `prevAgenticNodeWasReviewer`,
   `lastImplementerReviewerHarness` — sits behind one named entry point that the loop calls, and the
   `isReviewer` branches call that entry point rather than restating the rule. This criterion moved
   here from `run-machine-second-wave` on 2026-08-24, because the logic is in this function and not
   in the two functions that task kept. **This file is the only one that owns it**; that task now
   carries a pointer and no criterion.
6. Every extracted decision has a table test, and a **deliberate mutation of the production call path
   makes that test fail**. Record one such mutation per decision in the commit body.
7. The `gocognit` line for `driveDotWorkflow` is **deleted** from `tools/lintreport/allow.txt`
   because the finding is gone. `git diff tools/lintreport/allow.txt` shows deletions and no
   additions. Any `//nolint` you added is deleted too; `nolintlint` runs with `allow-unused: false`,
   so it will tell you if you forget.
8. Daemon behaviour is unchanged. The existing tests in `internal/daemon` pass with no change to
   their assertions — the `export_dotrun_test.go` call sites may be re-shaped to the new signature,
   and nothing else.

## Limits

- **One decision per commit** after the signature move. This is the same instruction
  `workloop-extract-pure-decisions` carries, for the same reason: earlier single-pass attempts on the
  run machine produced changes nobody could review.
- **Do not widen `tools/lintreport/allow.txt`.** Not one added line.
- **Do not delete a doc comment to move a metric.** `funlen` runs with `ignore-comments: true`, so
  comments buy nothing here. The `runner` parameter carries the only written statement of the
  remote-substrate contract — `SSHRunner for remote runs; nil for local (NFR7)` — and it must survive
  onto whatever field replaces it. A previous implementer on this program lost a comment that carried
  the specification during a move.
- **Do not touch `dispatchDotAgenticNode`** (same file, line 525, 408 lines, 28 parameters,
  cyclomatic 70). It is a separate row on this list and it is the reason this file cannot hold two
  lanes at once.
- **Do not touch `beadRunOne`.** That is `beadrunone-extract-phases`, and it runs after this one
  under the same owner.
- **Do not create a port, an interface or a seam that has one caller and no test that needed it.**
  Say in the commit body what each new abstraction buys.
- **Do not move this function or this file into a new package.** The review already rejected moving
  the run machine's files; moving a file does not fix a function. `harnesspick-extract` will edit this
  file's call sites into `internal/harnesspick`, which is another reason to leave placement alone
  here.
- Do not add a field to `runloop.RunEnv` or `runloop.SharedHandles`. `runenv-narrow-inputs` is
  narrowing both.
