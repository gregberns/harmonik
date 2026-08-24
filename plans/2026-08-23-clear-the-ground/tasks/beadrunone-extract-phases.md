---
id: beadrunone-extract-phases
title: beadRunOne runs eight phases of a bead run as one 501-line body with no phase boundary
type: task
priority: 1
labels: [daemon, run-machine, clear-the-ground]
depends_on: [dotworkflow-extract-decisions, run-machine-second-wave, runenv-narrow-inputs]
blocks: []
workstream: W2
batch: 3
---

## Problem

`internal/daemon/workloop.go` `beadRunOne` is **501 lines** (lines 83–583). Its signature is:

```go
func beadRunOne(ctx context.Context, env runloop.RunEnv, rp runloop.RunPorts,
	handles runloop.SharedHandles, extraContext string, preSelectedWorker *workers.Worker,
	localSlotHeld bool) (succeeded bool)
```

Seven parameters, and that number understates it: `runloop.RunEnv` has 29 fields and
`runloop.SharedHandles` has 18, so the function receives 47 fields plus a port struct and can
declare which of them it needs to nobody. Measured 2026-08-24 from the tree:

| Measure | Ceiling in `.golangci.yml` | `beadRunOne` today |
|---|---|---|
| `cyclop` (cyclomatic) | 15 | **77** |
| `gocognit` (cognitive) | 20 | **129** |
| `funlen` | 100 lines / 60 statements | **501 lines** |

501 lines and cyclomatic 77 agree with `run-machine-second-wave`. The cognitive figure of 129 and the
47-field input surface were not recorded anywhere.

**Unlike the other three W2.3 functions, this one is nearly all effect.** It is a sequence of phases
with no boundary between them, and the phases are legible in the source today:

1. Leases and the deferred terminal emit — lines 84–171, including `runScope`, `runExit`,
   `emitRunTerminalEff` and a `sync.WaitGroup` whose `Wait` is deferred on line 85.
2. Resolve the plan — lines 172–191: the `resolveRunPlan` call, the `plan.Verdict` branch, and the
   unpacking of the plan into locals. **This phase is already extracted and delegated.** It is the
   worked example the rest of the function should look like: the answer arrives as `plan.Verdict`,
   and the next line is `if plan.Verdict != runPlanReady`.
3. Choose local or remote — lines 196–246: a pre-selected worker, else `SelectWorkerByName`, else
   `SelectWorker`, else local.
4. Stand up the remote tunnel — lines 247–296, only when a worker was chosen.
5. Create the worktree and sync the base — lines 321–419.
6. Set up the run session and call `driveDotWorkflow` — lines 421–461, ending at the
   `dotResult := driveDotWorkflow(...)` call on line 458.
7. Land the work — lines 462–537: `bridge.Start`, the `bridge.WireSpine` merge configuration
   (including the `AmendTrailers` and `CarveOut` closures), and the `daemonStopping()` drain-and-exit
   on lines 529–537.
8. Classify the terminal outcome — lines 538–582, the `switch` over `dotResult.success`,
   `dotResult.subsumed` and everything else. **Resolving the worktree tip and charging the retry
   budget (lines 551–575) is a sub-step inside this phase, not a phase of its own** — it sits in the
   `default:` arm of that same `switch`. An earlier revision of this file listed it as phase 7 and
   put "land the work" nowhere, which asked for a call order the source cannot produce.

**Two stretches of the body are deliberately outside the phases**, and they are the reason a naive
lift fails. Lines 193–194 build `bridge` and `failRun`, and lines 297–319 define the
`notifyWorkerOffline` and `preMergeSync` closures — both capture `rbc`, `env`, `emit` and `handles`
from the enclosing frame, and both are used by phases that come later. When you cut the phase
boundaries, these have to become explicit arguments or move with the phase that uses them. Do not
leave them as free variables reaching across a boundary.

Phases 3 and 8 are decisions over values wearing effect clothes. Phase 3 answers "which worker, or
none" from `preSelectedWorker`, `plan.LocalOnly`, `plan.WorkerTarget` and the registry. Phase 8
answers "which terminal outcome" from `dotResult`, `daemonStopping()` and whether the retry budget is
exhausted, and today it interleaves that answer with `bridge.Feed` calls and an `Fprintf` to
`os.Stderr`.

**A correction you need before you start.** `run-machine-second-wave` says the reviewer is welded
into this function and asks for it to become a switchable stage here. It is not here. The reviewer
state — `axisReviewerVerdicts`, `reviewerNoVerdictRetries`, `prevAgenticNodeWasReviewer`,
`lastImplementerReviewerHarness` — is all inside `driveDotWorkflow` in
`internal/daemon/dot_cascade_core.go`. Do not go looking for it in `workloop.go`.

**The lint gate behaves differently here from the other three, and it is worse, not better.**
`beadRunOne` carries a `//nolint:funlen,gocognit,cyclop` directive on line 82, so the three
complexity findings never reach `tools/lintreport/allow.txt` and intermediate commits stay green on
those. But the list holds **three other findings keyed to this function's body**: `contextcheck`
(digest `1d2d0f04095b…`, comment `workloop.go:332`), `nakedret` (digest `32d3d719d2bb…`, comment
`workloop.go:389`) and a second `contextcheck` (digest `684fac0b6b6a…`, comment `workloop.go:551`).
Since the tolerated findings were re-keyed by content identity, each of those keys is
`sha256(linter, finding text, enclosing symbol body)` — so **the first edit you make to this body
invalidates all three**, `scripts/lint-allow.sh` fails, and re-seeding them trips
`scripts/lint-allow-ratchet.sh`. There is one way through that does not widen the list: fix those
three findings rather than carry them. The naked return goes when the named result `succeeded` stops
being the return channel; the two `contextcheck` findings go when the phases that need a
non-cancellable context take one as a parameter instead of swapping in `context.Background()` in the
middle of the body.

## Scope

- `internal/daemon/workloop.go` — `beadRunOne`, and a home for each extracted phase.
- `internal/daemon/export_workloop_test.go` line 99 — the test-side entry point.
- `internal/daemon/scheduler.go` line 1173 — the one production call site,
  `runOK := beadRunOne(runCtx, env, rp, handles, extraContext, preSelectedWorker, localSlotHeld)`.
- `tools/lintreport/allow.txt` — three lines deleted, none added.

Name the phases first, then lift the two decisions. `resolveRunPlan` shows what a named phase looks
like in this file: a request type in, a result value out, the caller branching on the result.

**`runenv-narrow-inputs` runs first — it is in `depends_on` above, so this is a scheduling fact and
not only advice.** That task gives each phase of a run an input type carrying only the fields it
reads. Use the input types it created. Where it did not reach into `beadRunOne`'s region, extend its
types in the same shape — do not invent a competing set here. The two tasks touch the same
signatures, so a race produces two vocabularies for one thing.

## Done when

1. `beadRunOne` is under all three ceilings: `cyclop` below 15, `gocognit` below 20, `funlen` below
   both 100 lines and 60 statements. Quote all three in the commit body. **Strip the `//nolint`
   before you measure with `golangci-lint`** — it obeys the directive and will report nothing, which
   reads as success. `~/go/bin/gocognit internal/daemon/` ignores it.
2. **Each of the eight phases is a named function** that `beadRunOne` calls, and the sequence of
   calls is readable as the sequence of phases. Eight calls in phase order, with the tip-resolve and
   retry-budget step called from inside the terminal-classification phase rather than beside it —
   that step lives in the `default:` arm of phase 8's `switch` and cannot be sequenced ahead of it.
3. **Worker choice is a value.** A pure function takes the pre-selected worker, `plan.LocalOnly`,
   `plan.WorkerTarget` and a registry snapshot and returns which worker, or none. A table test covers
   at least: a pre-selected worker wins; `LocalOnly` refuses a worker; a named target that exists; a
   named target that does not; and no target with an empty registry.
4. **Terminal classification is a value.** A pure function takes the workflow result, whether the
   daemon is stopping and whether the retry budget is exhausted, and returns the outcome. A table
   test covers success, subsumed, plain failure, and budget-exhausted-with-needs-attention. The
   `bridge.Feed` calls and the `Fprintf` stay at the call site.
5. Every extracted decision has a table test, and a **deliberate mutation of the production call path
   makes that test fail**. Record one such mutation per decision in the commit body.
6. The `//nolint:funlen,gocognit,cyclop` directive on line 82 is **deleted** because the findings are
   gone, not because it was moved.
7. The three `contextcheck` and `nakedret` lines for `beadRunOne` are **deleted** from
   `tools/lintreport/allow.txt` because those findings are fixed. `git diff
   tools/lintreport/allow.txt` shows deletions and no additions.
8. Daemon behaviour is unchanged. The existing `internal/daemon` tests pass without any change to
   their assertions.

## Limits

- **One phase or one decision per commit.** This function was 2,289 lines at the start of the
  delete-and-rewrite program and is 501 now. It got that far because the work was incremental, and
  every attempt to finish it in one pass has failed review.
- **Do not widen `tools/lintreport/allow.txt`.** Not one added line, and specifically not a re-seed
  of the three findings whose digests your first edit will invalidate. Fix them.
- **Do not delete a doc comment to move a metric.** `funlen` runs with `ignore-comments: true`, so
  comments buy nothing here. Two comments in this body are the only written record of a rule and must
  survive: the `//nolint:contextcheck` explanation on line 134 — *a cancelled per-run ctx must not
  drop the terminal; Background swap by design* — and the `ControlMaster=no` reason on the
  `sshRunner` construction at line 206. A previous implementer on this program lost a comment that
  carried the specification during a move.
- **Do not touch `driveDotWorkflow` or `dispatchDotAgenticNode`.** They are in
  `internal/daemon/dot_cascade_core.go`; `driveDotWorkflow` is `dotworkflow-extract-decisions`, which
  lands before this task and changes the call at line 458. Adjusting that one call to the new
  signature is expected; changing the callee is not.
- **Do not touch `runAgentLaunch`** in `internal/daemon/agentlaunch.go`. Separate row.
- **Do not add a field to `runloop.RunEnv` or `runloop.SharedHandles`**, for any reason.
- **Do not create a port, an interface or a seam that has one caller and no test that needed it.**
  Say in the commit body what each new abstraction buys.
- Do not move this function or this file into a new package.
