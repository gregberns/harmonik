# Decomposition map — the rotten center of harmonik

**Date:** 2026-07-28
**Scope:** `internal/daemon/workloop.go` and the run path around it. Read-only analysis.
**Inputs:** the file itself (read end-to-end), `plans/2026-07-27-delete-and-rewrite/` (`_plan.md`,
`CARRY-FORWARD.md`, `NEXT_STEPS.md`), and the superseded-but-evidentiary
`plans/2026-07-24-code-health-audit/` (`_plan.md`, `CORE-QUEUE-PI-PLAN.md`, task cards).

---

## 0-CORRECTION (2026-07-28, late). The four are not peers. One of them already works.

**Read this before §0.** The headline below — "one algorithm written out four separate times" — is
true about the *code shape* and misleading about *what to do*. Measured against the live event log
(`.harmonik/events/events.jsonl`, 100 MB, runs through 2026-07-22):

- **The graph engine is the DEFAULT and has really run.** `moderesolve.go` tier 4 returns
  `WorkflowModeDot`; `single` is reachable only via an explicit `workflow:single` per-bead label and
  has been used **twice, ever** (2 of 2,153 `run_started`). The engine dispatched **1,697 `implement`
  nodes, 1,376 `commit_gate` nodes, and made 6,846 edge-routing decisions** across **three distinct
  graph files** — the project's own `workflow.dot` (`review`, `qa`), `sonnet-triple-review.dot`
  (`review_correctness`, `review_design`, `review_tests`, `consolidate`) and `eval-bead.dot` (`grade`,
  `judge`). All three node sets appear in the log. This is not a demo path.
- **Measure it with `node_dispatch_*`, NOT with `workflow_mode`.** §6 of `PRIOR-ART.md` is right that
  the `workflow_mode` field on `run_started` is stamped *before* the graph→review-loop demotion and so
  cannot distinguish them. The node-dispatch events can: `node_dispatch_requested` is emitted only from
  `dot_cascade_core.go` / `dot_cascade_helpers.go`, and `reviewloop.go` emits zero. Independently, the
  demotion branch is **unreachable on this project** — it is gated on `<projectDir>/workflow.dot` not
  existing, and it exists.
- **Graph-mode failures are not engine failures.** 866 started / 417 completed / 451 failed. Of the
  451: 144 orphaned by a daemon restart, ~138 the implementer agent not committing or not starting,
  ~52 merge/vet/format, ~25 a reviewer producing no verdict file — and ~32 that are the engine working
  correctly (retry cap hit, no-progress detected, routed to `close-needs-attention`). Review-loop's
  failure mix is the same shape (545 of 1,282).

**So Phase 3 is a deletion, not a rewrite of the run machine.** The engine exists; two hand-written
shadows of it do not need to. Removing the shadows needs a named amendment to `execution-model.md`
EM-012a-FLOOR / `process-lifecycle.md` PL-004a / `beads-integration.md` BI-009a, which all restate the
demotion. See `PRIOR-ART.md` §6.

**CORRECTION (2026-07-29): "kept alive by ONE line" is wrong as literal reachability.** It is right
about which path fires *unbidden*, and that distinction is the useful one. Review-loop mode is produced
at **five** non-test sites: the `EM-012a-FLOOR` demotion in `beadRunOne`; the `--review-loop` flag and
`--workflow-mode review-loop` in `cmd/harmonik/run.go`; `harnessApplyWorkflowDOT` in
`cmd/harmonik/harness.go`; and `DriveOrchestration` in `internal/scenario/orchdrive.go`. The last two
default to review-loop when a scenario declares no `workflow_path` — but all six shipped scenario
definitions under `scenarios/` do declare one, so both are **dormant**. `runBridgeConfig` in
`internal/runloop/runbridge.go` is a *consumer* of the mode (it grants review-loop `MaxMergeAttempts=3`
where dot gets 1), not an entry point — but retiring the mode must decide that retry's fate rather than
drop it silently.

So: **the floor is the only path that reaches review-loop when nobody asked for it.** The other four are
deliberate opt-ins. Deleting the floor does not free `reviewloop.go` on its own; the opt-ins must be
retired too.

**And the migration was already specified and never executed.** `specs/examples/review-loop.dot` draws
the hardcoded review loop as five nodes, and its own header says: *"once the C2 dispatch driver
subsumes the hardcoded Go path, this file becomes the source of truth and the Go path is deleted."*

**Caveat that made this task zero, not a conclusion — NOW DISCHARGED.** The event log ends
**2026-07-22** and Phase 1 deleted ~225,000 lines afterwards, so everything above rested on six-day-old
evidence.

**Task zero is GREEN, re-measured 2026-07-29 on the current tip (`cde2f54e`).** A real ticket ran
end-to-end through graph mode in a fully isolated scratch daemon (own socket, own ledger, own tmux
session, own binary built from the tip; the fleet daemon was never touched — `scripts/scratch-daemon.sh`).
The graph routed **six nodes** — `start → implement → commit_gate → review → qa → close` — in 2m32s, and
the run landed a **real commit** on the target branch, verified on disk and not merely inferred from
events. Codex ran the implementer; the reviewer node fired twice and a valid verdict file was produced
and consumed both times. So the engine is not merely historically real — it works on today's binary.

Two things worth carrying forward from that run:

- **The event-type names in this doc are approximate.** There is no `node_dispatch_started` /
  `_completed` / `_failed`. The real emissions are `node_dispatch_requested`, `node_dispatch_decided`,
  `node_started`, `node_completed`. A monitor filtered on the wrong names goes silent, and silence here
  is indistinguishable from "the engine did nothing" — which is exactly the false-negative this project
  keeps getting bitten by. Confirm event names against `internal/core` before filtering on them.
- **`working_tree_refresh_failed` fired on this run and is benign.** The scratch checkout sat on a
  different branch than the merge target, so the post-merge scoped refresh had nothing to check out. The
  registry already classifies EM-054 as *"ordinary — informational; merge already durable"*, which is
  precisely what happened. Do not read it as a failed merge.

---

## 0. The headline, before the detail

> **⚠ SUPERSEDED 2026-07-30. Do not brief anyone off the original headline below.** It said one
> algorithm was "written out four separate times", in 6,459 lines. Two of the four are gone. Measured
> at HEAD on 2026-07-30:
>
> | Function | File | Lines | State |
> |---|---|---:|---|
> | `runAgentLaunch` | `internal/daemon/agentlaunch.go` | ~~836~~ **558** | **the ONE launch path.** All three remaining sites call it. A CI gate (`scripts/readywait-freeze-gate.sh`) allows exactly one `runloop.DispatchSegment` in the tree, and it must be here |
> | `beadRunOne` | `internal/daemon/workloop.go` | ~~1,603~~ **1,705** | run driver. Its single-mode tail is **740 lines, 368 of them code** — see the ⚠ under §1c |
> | `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 997 | the graph walker. **The default and the traffic** |
> | `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | ~~543~~ **544** | per-node dispatch inside the walker |
> | `executeCognitionGate` | `internal/daemon/dot_gate.go` | ~~234~~ **230** | **cannot run.** No code sets `daemon.Config.CPRegistry`. The file is 749 lines, not 721 |
> | ~~`runReviewLoop`~~ | ~~`internal/daemon/reviewloop.go`~~ | ~~2,194~~ | **deleted 2026-07-28 (`3cec5afd7`)** |
>
> **RE-MEASURED 2026-07-31 against `1e22c0141`. Three rows had drifted, one was never right, and one
> is unchanged.** The `runAgentLaunch` row quoted **the size of the file, not the size of the
> function**. `agentlaunch.go` was exactly 836 lines at `fabc7cb21`, the tip on the day the row was
> written, when the function was 535. Every other row in this table is a function size, so the row was
> not comparable with its neighbours. At `1e22c0141` the function is 558 and the file is 859.
> `beadRunOne`, `dispatchDotAgenticNode` and `executeCognitionGate` had drifted by ordinary edits, and
> each was re-measured with `awk '/^func <name>/,/^}/' <file> | wc -l`. `driveDotWorkflow` is still
> exactly 997. Full working for the `beadRunOne` and tail figures is in
> [`STEP-7-MODE-BOUNDARY.md`](STEP-7-MODE-BOUNDARY.md) §1.
>
> `runWorkLoop`, the outer scheduler, also left this file — `756b6604c` (2026-07-29) moved it to
> `internal/daemon/scheduler.go`, and Step 5 (`4070c75ed`) moved the run-plan resolver out to
> `internal/daemon/workloop_runplan.go`. That is why `workloop.go` reads 3,356 lines and not 6,656.
>
> What is still duplicated, and what to do about it, is §2b. The short version: the launch step is
> one function; "decide whether it did the work" is still written twice.

The named center is `workloop.go`. It is not the only one, and the thing that makes it rotten is not
its size.

The rotten center is one algorithm — "launch an agent, watch it, decide whether it did the work,
merge, terminalize". The duplication was measurable, not inferred. These symbols each used to appear
in three or four files independently:

```
pasteInjectOnLaunch          → workloop, reviewloop, dot_cascade_core
pasteInjectQuitOnCommit      → workloop, reviewloop, dot_cascade_core
runloop.DispatchSegment{}    → workloop, reviewloop, dot_cascade_core, dot_gate
WaitWithSocketGrace          → workloop, reviewloop, dot_cascade_core, dot_gate
EffectiveAgentReadyTimeout   → workloop, reviewloop, dot_cascade_core, dot_gate
ForceTeardownSession         → workloop, reviewloop, dot_cascade_core, dot_gate
HookStore.RegisterHookSession→ workloop, reviewloop, dot_cascade_core, dot_gate
gitprobe.ResolveWorktreeHEADVia → workloop, reviewloop, dot_cascade_helpers
```

**All eight rows are now single-site.** Every symbol above except `gitprobe.ResolveWorktreeHEADVia`
appears once, inside `runAgentLaunch`. HEAD resolution is the one step the collapse deliberately left
with the caller, and it is therefore the one still written twice.

And the drift that duplication predicts is already present, in two places I confirmed:

- **`runmerge.CheckMainWorkingTreeDirty` (the implementer-escaped-worktree guard) has exactly one
  production caller: `beadRunOne`.** DOT runs do not run it. The run state
  machine has a slot for it (`runexec.ActCheckEscape` / `EvEscapeDetected`, `RunEffectors.CheckEscape`)
  but `RunBridge.WireSpine` wires that slot to a function that unconditionally returns
  `EvGuardsPassed`. The guard is imperative, single-mode, and the machine records a pass it never
  performed. **Correction 2026-07-30: this is not drift.** `specs/run-state-machine.md` RSM-008
  requires the DOT path to skip both guards. See §2b.
- ~~**`sandboxSpawnForRun` (the srt sandbox gate) is called from `workloop.go` and
  `dot_cascade_core.go` only.**~~ **FIXED 2026-07-29 by `30b5cf02c`.** There is one gate, and
  `sandbox.harnesses` in the config is the only switch. Every launch asks it.

`CARRY-FORWARD.md` names this exact bug class from the pasteinject archaeology: *"4× single-mode and
review-loop paths drifted"*, *"3× gate applied to one of N call sites"*. The bug class is real. The
two instances above are now one specified asymmetry and one closed defect, so read `CARRY-FORWARD` as
the reason the collapse was worth doing, not as a live inventory.

---

## 1. What is actually in there

### 1a. Whole-file metrics

**Re-measured 2026-07-30. Every number below moved, and one row died.** The table as first written
described a single 6,656-line file that held both functions. That file no longer exists in that
shape. `756b6604c` moved `runWorkLoop` out to `internal/daemon/scheduler.go`, and `3cec5afd7`
retired review-loop mode. The original numbers are kept in the right-hand column so a reader can see
the size of the drift.

**Re-measured again 2026-07-30, after Step 5.** The middle column below moved a second time in one
day. `4070c75ed` pulled the run-plan resolver out to `internal/daemon/workloop_runplan.go` (580
lines), which took 162 lines off the file and 161 off `beadRunOne`.

> **⚠ This table's "@ HEAD" column is dated 2026-07-30 and was not re-measured in full on 07-31.**
> Two cells are known stale at `1e22c0141`: total lines is **3,356**, not 3,227, and `beadRunOne` is
> **1,705**, not 1,603 — so the one-function share is 51%, not 50%. The other rows were not
> re-counted. Treat the whole column as of its own date, and re-run the commands below before
> quoting any cell.

> **⚠ Re-measured in full 2026-08-01 at `95dff0bf5`. This supersedes the 07-31 note above, which is
> itself now stale.** Six of the nine cells moved. Every command is the one shown under the table.
>
> | Cell | Table says | 2026-08-01 |
> |---|---:|---:|
> | Total lines | 3,227 | **3,366** |
> | Comment lines | 1,672 (52%) | **1,793 (53%)** |
> | Commits | 384, last 2026-07-30 | **399, last 2026-08-01** |
> | Commits by month | May 108, Jun 140, Jul 136 | **May 108, Jun 140, Jul 145, Aug 6** |
> | `hk-…` bead refs | 130 | 130 — unchanged |
> | `//nolint` | 6 | **8** |
> | `workLoopDeps` struct | 81 fields, 744 lines, 78% comment | **81 fields, 770 lines, 79% comment** |
> | Top-level functions | 21 | 21 — unchanged |
> | `beadRunOne` | 1,603 = 50% | **1,704 = 51%** |
>
> The `workLoopDeps` comment share is `sed -n '111,880p' internal/daemon/workloop.go |
> grep -cE '^\s*(//|/\*|\*)'` — 608 of 770. The field count is `go/ast`: parse the file, find the
> `workLoopDeps` `TypeSpec`, sum `len(Field.Names)` over `StructType.Fields.List`.
>
> **Prefer the field count to the line span.** The span is 743, 744 and 770 on three different days
> in one week, and this file is under active edit, so it will be wrong again. The field count has
> held at 81 for the whole program (see Step 10 below). A field count is a stable number. A line
> span is not.
>
> **On the 743-versus-744 disagreement between this row and Step 10 below:** it was a date artifact,
> not an error. The declaration measured 743 lines on 2026-07-28 and 744 on 2026-07-30. Both were
> true when written. It is 770 now, and both figures are superseded.

| Measure | `workloop.go` @ HEAD | before Step 5 | as first written |
|---|---:|---:|---:|
| Total lines | **3,227** | 3,389 | 6,656 |
| Comment lines | 1,672 (52%) | 1,744 (51%) | 3,103 (47%) |
| Commits | 384 (first 2026-05-12, last 2026-07-30) | 382 | 370 |
| Commits by month | May 108, Jun 140, Jul 136 — **still not decaying** | — | Jul 122 |
| Distinct `hk-…` bead refs cited in comments | **130** | 138 | 256 |
| `//nolint` directives | 6 | 10 | 19 |
| `workLoopDeps` struct | 81 fields, 744 lines, 78% comment | same | 81 fields, ~740 lines, ~89% comment |
| Top-level functions | 21 | 21 | 39 |
| One function | `beadRunOne` 1,603 = **50% of the file** | 1,764 | two functions = 60% |

Reproduce the counts with `wc -l internal/daemon/workloop.go`,
`grep -oE 'hk-[a-z0-9]+' internal/daemon/workloop.go | sort -u | wc -l`,
`git log --oneline -- internal/daemon/workloop.go | wc -l`,
`grep -cE '^\s*(//|/\*|\*)' internal/daemon/workloop.go` for the comment count, and
`awk '/^func beadRunOne/,/^}/' internal/daemon/workloop.go | wc -l` for the function.

The bead-reference count is still the number that matters, and it is still large. This is not a
program; it is a changelog with executable annotations. About half the file is prose explaining why
the other half is shaped the way it is, and much of that prose describes code that is no longer
there. The count fell from 256 to 130 because deletions and extractions took the annotations with the
code, not because anyone pruned the prose.

**The distinct-spec-ID row is removed. It was never reproduced.** The original table claimed 81 spec
requirement IDs, the same number as the `workLoopDeps` field count. No command was recorded, and the
coincidence is more likely a transcription error than a measurement.

### 1b. `runWorkLoop` — the outer scheduler

**This function is no longer in `workloop.go`.** `756b6604c` ("daemon: split the dispatch scheduler
out of workloop.go") moved it to `internal/daemon/scheduler.go`, where it is 1,281 lines. Seam A is
therefore cut. The responsibility table below was measured before the move and is a record of what
that function did at that time, not a map of the current file.

One goroutine. Owns the poll loop, all admission policy, the claim write, and goroutine spawn.

| # | Responsibility | ~Lines | External world |
|---|---|---:|---|
| 1 | Merge-queue ownership (create/Start/cancel `mergeq`), claim semaphore, ~12 pieces of loop-local state | 135 | — |
| 2 | `exitClean`: bounded drain (10 s), kill orphan tmux windows, cancel-drain every active queue | 38 | tmux, filesystem (queue.json) |
| 3 | Spawn-substrate readiness gate after restart-backoff boot | 14 | tmux |
| 4 | Adopt live run sessions surviving a prior daemon (spawns a monitor goroutine per record) | 24 | tmux, filesystem (`.harmonik/runs/`) |
| 5 | Dispatch-halt + governor-halt checks | 17 | — |
| 6 | Schedule tick — fire due recurring jobs (spawn-crew, comms-send, shell command) | 6 (delegates to `scheduletick.go`, 433) | tmux, `harmonik comms`, shell |
| 7 | Periodic flywheel-coordinator tmux session reaper (5 min) | 22 | tmux |
| 8 | Periodic disk watermark probe + reactive `go clean -cache` (10 min) | 21 | filesystem, `go` toolchain |
| 9 | Split local/remote capacity gate | 23 | — |
| 10 | Dashboard staleness forcing gate + `dashboard_stale`/`dashboard_refreshed` edges | 47 | filesystem (dashboard.json, lanes.json, config.yaml), bus |
| 11 | EM-062 eager refill | 8 | **kerf CLI**, br |
| 12 | Sentinel governor OBSERVE mode | 46 | br, `events.jsonl` scan, bus |
| 13 | Sentinel governor ACT mode: trip/clear/halt, ack files, **spawn an adversary crew** | 148 | filesystem (ack files), tmux (crew spawn), `harmonik comms`, bus |
| 14 | Queue selection: bootstrap pending groups, re-evaluate deferred items, cross-queue round-robin | 214 | filesystem (queue.json persist) |
| 15 | Cooldown guard, handler-pause gate, decision-required gate, sentinel queue gate | 66 | bus |
| 16 | Pre-claim `ShowBead` + bounded retry + BI-013c skip + stranded auto-reset + deferred stamping | 172 | **br**, filesystem, bus |
| 17 | `needs-greenlight` label gate | 23 | — |
| 18 | Hoisted secondary local-cap guard | 22 | — |
| 19 | Phase-3 dispatch stamp: cross-queue bead dedup, attempts bound, persist | 138 | filesystem |
| 20 | Bead-record hydration from the pre-claim read | 26 | — |
| 21 | br-ready fallback path (no-auto-pull, operator pause, `Ready` poll, attempt bound, re-gates) | 109 | **br**, bus |
| 22 | RunID mint (uuid v7), queue RunID patch + second persist, TID mint | 43 | filesystem |
| 23 | br-ready pre-claim `ShowBead` + label hydration | 49 | **br** |
| 24 | Claim semaphore → `ClaimBead` → blocked detection → stale-blocker auto-close → item revert | 83 | **br**, git (`MainHistoryHasRefsTrailer`), filesystem |
| 25 | Queue-path label hydration (a third `ShowBead`) | 16 | **br** |
| 26 | `RunHandle` construction + registry Register under the cache-reap read lock | 51 | — |
| 27 | Worker pre-selection + local/remote in-flight accounting | 20 | worker registry (ssh) |
| 28 | Goroutine spawn: build `RunEnv`, call `beadRunOne`, group-advance, staged-bead generator | 38 | **br** (staged bead create) |

Note responsibilities 6, 7, 8, 10, 11, 12, 13 — schedule ticks, tmux reaping, disk reaping, dashboard
gating, kerf refill, and a governor that can *spawn an LLM crew* — are all inside the bead-dispatch
poll loop. None of them are about dispatching a bead. That is roughly 300 lines of unrelated
cadenced maintenance riding the dispatcher's tick.

Note also **three separate `ShowBead` round-trips per dispatch** on the queue path (#16 pre-claim,
#25 post-claim hydration, plus #24's blocked-status re-read on failure), each with its own retry
budget and its own failure semantics.

### 1c. `beadRunOne` — the per-run driver (1,705 lines)

**Re-measured 2026-07-31 against `1e22c0141`. The function is 1,705 lines — 815 code, 824 comment,
66 blank — and its signature takes 7 parameters.** It read 1,603 after Step 5, 1,764 on 2026-07-29,
and 2,289 as first written. Step 6's migration commits put lines back. Find it with
`grep -n '^func beadRunOne' internal/daemon/workloop.go` and size it with
`awk '/^func beadRunOne/,/^}/' internal/daemon/workloop.go | wc -l`.

> ⚠ **CORRECTED 2026-07-31 — the warning below had the wrong premise and pointed the wrong way.**
> It said the 722 figure was unsafe because Step 5 removed an unrecorded number of lines from the
> single-mode tail. **Step 5 removed none of them.** Measured across every commit that touched
> `workloop.go`: Step 5 (`4070c75ed`) took `beadRunOne` from 1,764 to 1,591 and left the tail at
> exactly 711 lines and 357 lines of code. Every one of its 173 lines came from above the mode
> switch. **And the tail has since GROWN, not shrunk** — 711 → 740, because Step 6 landed inside it.
> Anyone who rescaled 722 downward for Step 5 got a number wrong in both magnitude and direction.
>
> **The re-derived figure at `1e22c0141` is 740 lines: 368 code, 342 comment, 30 blank**, bracketing
> the tail from the close of the mode switch to the end of the function. **Quote 368, not 740** —
> comments are 46% of the tail and a raw count overstates the work by close to a factor of two. And
> quote 368 as "code touched", never as a deletion figure: eighteen capabilities in the tail must be
> ported onto the graph node rather than deleted. Full working in
> [`STEP-7-MODE-BOUNDARY.md`](STEP-7-MODE-BOUNDARY.md) §1.
>
> **722 is not reproducible from any natural bracket** at `756b6604c`, the commit the retired text
> in §2b names. The candidates there are 945, 933, 774, 711 and 710. It was an estimate.
>
> **Where the old figure still appears.** §0's table, §2b and the Step 7 entry are corrected in the
> same commit as this note. Two mentions are deliberately left alone, and both sit inside the
> `<details>` block under §2b, which is marked as retired text and kept so a reader can date the
> decay. Nowhere else in this file still quotes 722. Treat it as superseded wherever it appears.

Row 17 below is
dead: `3cec5afd7` retired review-loop mode and deleted its driver, so `beadRunOne` now dispatches two
modes, not three. The rest of the table was measured before that deletion and before the Seam A
split. Treat it as a shape, not as current line counts.

| # | Responsibility | ~Lines | External world |
|---|---|---:|---|
| 1 | Alias 13 `env` fields to locals; register local-slot and worker-slot release defers | 55 | — |
| 2 | Owning-epic attribution (find parent edge → look up assignee) | 11 | **br** |
| 3 | Run-terminal emission effector + `sessiondata.Collect` goroutine | 53 | bus, filesystem (`~/.claude/projects`) |
| 4 | Workflow-mode and workflow-ref resolution (4-tier + per-item tier-0) | 20 | bus |
| 5 | Construct the `RunBridge` (run state machine) | 14 | — |
| 6 | Model/effort resolution, Pi provider-profile resolution, `provider_selected` emission | 72 | config, bus, **br** |
| 7 | Cross-repo `activeRepo` resolution + allowed-repos safelist refusal | 48 | bead-body parse, filesystem |
| 8 | Parent-commit (`start_from`) resolution; `lands_on` resolution; protected-branch refusal | 61 | **git** |
| 9 | Remote worker selection (pre-selected or fallback) + slot re-accounting | 86 | worker registry, **ssh** |
| 10 | Reverse SSH tunnel: port alloc, worker mkdir, socket-path-length validation, `ssh -N -R` spawn, connectability probe | 119 | **ssh**, network, filesystem |
| 11 | `notifyWorkerOffline` + `preMergeSync` closures | 37 | **ssh**, **git** |
| 12 | Worktree factory selection (local vs remote-over-SSH) + base-sync + create, inside the merge exclusion domain | 103 | **git**, **ssh**, mergeq |
| 13 | Cleanup defers: run-registry record removal, Pi-failure worktree retention | 50 | filesystem |
| 14 | Pre-run untracked-file snapshot (escape-check baseline) | 10 | **git** |
| 15 | `run_started` emission | 8 | bus |
| 16 | DOT graph preload + review-loop safety floor | 26 | filesystem, DOT parser |
| 17 | ~~**Mode dispatch: review-loop**~~ — **GONE.** `3cec5afd7` retired the mode and deleted `internal/daemon/reviewloop.go`. Only history comments remain. | 0 | — |
| 18 | **Mode dispatch: DOT** — graph load, goal injection, remote params, call, `WireSpine` w/ carve-out, orphan-salvage tip SHA | 175 | (delegates 1,884 lines), **git** |
| 19 | Single-mode: `shared.LaunchCtx` assembly (26 fields) | 61 | — |
| 20 | Agent-type + Pi flags onto the run handle | 11 | — |
| 21 | Per-run substrate: `perRunSubstrate` wrap, worker session naming, independent-tmux-session election + run-registry record write | 86 | **tmux**, filesystem |
| 22 | srt sandbox gate + **engagement canary verification** + exec-path argv wrap + GOCACHE/GOPATH redirect | 104 | **srt subprocess**, filesystem |
| 23 | SessionIDCaptured handling: stdout interceptor, Pi stdout tee-to-file | 60 | filesystem |
| 24 | Hook-session registration | 4 | unix socket registry |
| 25 | Pre-exec message emission | 9 | bus |
| 26 | Per-run event tap + handler construction | 6 | — |
| 27 | Cold-start spawn semaphore (cap 3, remote only) | 22 | — |
| 28 | D2 credential refusal at the launch boundary | 11 | env inspection |
| 29 | Completion-mode + adapter resolution | 24 | — |
| 30 | **`DispatchSegment` construction** — 7 hooks: Launch, OnLaunchFailed, OnLaunched, Deliver, KillReady, EmitReadyTimeout, KillAbort. Contains: paste injection, `/quit`-on-commit watchdog spawn, heartbeat goroutine, comms presence join, agent-ready callback registration, spawn-cap and tmux-timeout event emission, bounded kill+reap | **279** | **tmux**, unix socket, bus, **git** |
| 31 | Post-launch cleanup defers: Pi stderr capture, force teardown, presence offline, heartbeat close | 63 | filesystem, tmux, bus |
| 32 | Ready-timeout terminal | 8 | — |
| 33 | `WaitWithSocketGrace`, per-run abort check, lifecycle `Terminated` transition, tmux window kill | 44 | unix socket, tmux, bus |
| 34 | `implementer_phase_complete` emission | 22 | **git** |
| 35 | ProcessExit daemon-side commit fallback (`codex.EnsureRefsTrailer` / `pi.EnsureRefsTrailer`) + no-work detection | 56 | **git**, codex/pi decision tables |
| 36 | Wait-return → terminal-event mapping | 27 | — |
| 37 | `WireSpine` for single mode | 16 | — |
| 38 | Implementer-escaped-worktree guard, inside the merge domain | 51 | **git** |
| 39 | No-commit guard | 37 | **git** |
| 40 | Dispatch-terminal classification switch: agent_completed / clean-exit / noChange-subsumed / shutdown-drain / failure-with-stderr-tail | 96 | **git**, bus |

**External-world contact count for `beadRunOne` alone: git (12 distinct places), tmux (5), ssh (5),
br (3), unix socket (3), codex CLI (1), pi CLI (1), srt (1), the event bus (~24 emission sites), and
the filesystem throughout.** Every one of the six external tools named in `CARRY-FORWARD.md` is
touched from inside this one function.

### 1d. The two dead things still in the file — BOTH DELETED, and the comments are not dead

- ~~**`activateFirstPendingGroup` (94 lines) has zero callers**~~ — **deleted 2026-07-28.** Only
  `activateFirstPendingGroupLocked` remains, and `runWorkLoop` calls it.
- ~~**`beadExplicitlyReopened` (39 lines) has zero production callers**~~ — **deleted 2026-07-28**,
  with the one test file that held it, `internal/daemon/predispatch_reopen_hkwcv_test.go`. The claim
  above said "two test files". A content search over all history finds one.

**Re-measured 2026-07-29 — the three comment blocks are NOT gravestones, and deleting them would
lose knowledge this program exists to keep.** Each was read in full. **Settled 2026-07-29 — the fact
now lives in the doc that owns it, and the disposition differs per block.** Two of the three kept a
comment in the source, so the one-line "move the fact out, then delete the copy" instruction below
was wrong as a general rule, and it is corrected here.

Note before reading further: **only two of the three are in `workloop.go`.** The Seam A split carried
both `hk-l5saf` comments into `internal/daemon/scheduler.go`.

- **The `sandboxOSTmpDirs` block — fact moved to `CARRY-FORWARD.md`, source copy DELETED.** It did
  not merely record that a function was removed. It stated why the sandbox must never grant recursive
  write access to the shared temp directory, and it named the mechanism that made the hole reachable:
  srt expands every temp-dir entry into a recursive write rule, and `os.TempDir()` falls back to the
  shared `/tmp` whenever no per-user temp dir is set. `CARRY-FORWARD.md` now carries that as a new
  **srt sandbox** section of six facts, which also holds the parts that lived nowhere else — srt's
  hardcoded `TMPDIR=/tmp/claude` for sandboxed children, the consumers that hardcode a host temp root
  instead of reading `TMPDIR` (C's `tmpfile()`, tmux's `TMUX_TMPDIR` socket), the load-7.53
  measurement, and the untested Linux case.
  **Deleting the source copy is safe because the rule already sits at the real edit site and is
  mechanical there.** `GenerateSandboxProfile` in `internal/daemon/sandboxprofile.go` rejects a
  world-shared root in `TmpDirs` and fails the launch with a named error, and the `TmpDirs` field
  comment states the same rule. Nobody was going to reintroduce a function that no longer exists, and
  the block was attached to no code. It had also decayed twice: it cited `Makefile:453` and `:465` for
  a `TMPDIR=/tmp` that the `check-short` and `check-race-full` recipes now carry, and it cited
  `TestSandboxAcceptance_WriteToMainDenied_hki0377` as its evidence, which no longer exists anywhere
  in the tree. Cited by recipe name on purpose — replacing two rotted line numbers with two fresh ones
  reproduces the defect the sentence is describing.
- **The `hk-l5saf` block — KEPT as it stands, in `internal/daemon/scheduler.go`.** It explains why
  there is deliberately **no** guard at that point: the item is already stamped and persisted, so a
  guard there would strand it. Delete the comment and the next reader re-adds the guard. §3 step 3
  states the same ordering constraint and calls it load-bearing, so the fact survives the comment —
  but only the comment says it at the place someone would edit. The comment is already six lines and
  already at that place, so there is nothing to move and nothing to shorten. **This one was never a
  writing task.**
  Two decays to fix when someone next edits that file, and they sit in BOTH `hk-l5saf` comments, not
  only the sibling. The hoisted-guard comment cites "~line 1818" for the Step-2 split gate and
  "~line 3072" for the `localInFlight` increment. The kept post-stamp comment — the "no guard here"
  one — cites "~line 3072" as well. So the block described here as needing no change does carry a
  rotted reference. That does not change the conclusion: the constraint it states is still correct, is
  still at the edit site, and is still recorded in §3 step 3. Only the pointer rotted.
- **The `hk-f38n` block — fact moved to `specs/beads-integration.md` §4.7, SHORT comment kept at the
  edit site.** It records a live false-close: a bare commit-message grep matched an old partial commit
  that carried the same issue ID, so the daemon closed a bead whose remaining work had not run. This
  is a production correctness sensor, which is a third category — phase 1's deletions covered
  prose-grepping test sensors and ticket-named test files, not sensors inside product code. Nothing
  has swept this class. The receiving home is the informative note under BI-022, which owns "git is
  authoritative for completion" and is exactly the claim the incident qualifies. The 18-line block in
  `beadRunOne` became a 9-line "do not add a pre-dispatch already-landed check here" comment that
  names the two runtime paths covering crash-restart and points at the spec note. The same warning
  now sits on `MainHistoryHasRefsTrailer` in `internal/harness/shared/refstrailer.go`, because a
  future caller reaches the primitive rather than the old edit site.
  **Open, and larger than the comment was.** Two things outlive the block. First, `specs/execution-model.md`
  EM-063 Phase 2 (the daemon's eager-refill pre-screen) and EM-064 tier 2 (the orchestrator's guard
  before submit) both still mandate the same bare match as an "already landed" test. Second, and worse,
  `autoCloseStaleBlockersOnClaimFailure` in `internal/daemon/scheduler.go` closes a blocker bead on a
  bare match with no work-presence evidence at all — the same false-close shape, live in the daemon
  today. Both are recorded in `OPEN-DEFECTS.md` and neither is fixed here: narrowing a normative test
  needs adjudication, and the scheduler belongs to another piece of work.

**Disposition, per block rather than in general:** where the fact is about the outside world and
nobody editing that line needs it in front of them, move it out and delete the source copy. Where the
comment's value is its POSITION — it stops a specific edit someone would otherwise make right there —
leave a short comment that states the constraint and points at the doc holding the rationale.

---

## 2. Where the real seams are

A seam is real when the two sides have **different lifetimes, different failure modes, different
external dependencies, and different rates of change**. Below, each proposed seam names the *data*
that crosses it, not a type.

### Seam A — Scheduler ↔ Run  ★ the only seam already load-bearing

**Data crossing:** one immutable dispatch decision — `{run_id, bead_record, queue coordinates
(name/id/group/item), per-item overrides (workflow mode, workflow ref, template params, local-only,
worker target), pre-selected worker or nil, local-slot-held bool, extra context string}`. Back:
one boolean plus a terminal summary.

**Why it is a true joint:**
- *Lifetime:* the scheduler is one goroutine for the daemon's life; a run is one goroutine for
  10–90 minutes.
- *Failure mode:* a scheduler failure wedges the fleet; a run failure reopens one bead.
- *External deps:* scheduler = br + queue.json + kerf + disk. Run = tmux + ssh + git + codex/pi +
  the hook socket. Almost disjoint.
- *Rate of change:* both high, but for different reasons — the scheduler churns on admission policy,
  the run churns on harness behaviour.

**Evidence it is real:** the goroutine boundary already exists (`workloop.go:3025`), the parameter
list is already explicit (a comment there says the explicit parameters exist *because* closure
capture was a bug), and `beadRunOne` has exactly two call sites — the goroutine and a test shim. The
egress analysis confirms it: only four symbols escape `workloop.go` into other production files.

**What is wrong with it today:** the bundle that crosses is `workLoopDeps` copied by value — all 81
fields, of which the run path uses ~25. And the file itself documents the resulting hazard
(lines 830–835): value fields mutated from a run goroutine are silent no-ops, which is why
`loopMaintenanceState` had to be lifted out. That is a design defect preserved by a comment.

### Seam B — Admission ↔ Claim ↔ Reservation

**Data crossing:** *(admission → claim)* a `DispatchCandidate` = `{queue name, queue id, group index,
item index, bead id, per-item overrides}` plus a `permit | delay(reason, wake-condition) | stop`
decision. *(claim → reservation)* `{bead id, run id, transition id}` and, durably, the pair
`(item.status = dispatched, item.run_id)` written **together**.

**Why it is a true joint:** admission is pure — it reads a snapshot and answers a question. Claim is
a single external write to `br` under a semaphore. Reservation is a single durable write to
`queue.json`. Three different failure modes: a wrong admission decision costs a tick; a failed claim
costs a retry; a torn reservation strands an item forever.

**Evidence it is real:** the pure half already exists and is already used —
`internal/orchestrator/select.go` (`SelectNextQueue`, `FleetSnapshot`) is a pure selector that
`workloop.go`'s `selectNextQueue` shell calls after projecting live state via `snapshotFleet`. That
extraction landed and works. The remaining 200 lines of gates (handler-pause, decision-required,
sentinel, greenlight, cooldown, local-cap, cross-queue-dedup, attempts-bound) are the same shape and
have not been moved.

**⚠ Today the reservation is torn, deliberately.** `runWorkLoop` writes
`Items[i].Status = Dispatched` with `RunID = &""` (a placeholder empty string), persists, then mints
the UUID, then re-persists to patch the RunID — and **both persists are non-fatal**. A crash or a
persist failure between them leaves a dispatched item with no run id. The 2026-07-24 audit flagged
exactly this (`CORE-QUEUE-PI-PLAN.md` §"Why the original index was unsafe"). It is unfixed.

### Seam C — Run plan ↔ Resources ↔ Execution ↔ Terminal (inside `beadRunOne`)

Four sub-seams, all inside one function today.

**C1 — Resolution.** *Data out:* an immutable `RunPlan` = `{workflow mode, workflow ref, agent type,
model, effort, pi profile (provider/api/base-url/key-env), active repo, parent SHA, base branch,
merge target, protect-branch set, placement intent}`. *In:* the bead record, project config, queue
defaults, daemon defaults. **True joint:** pure, no I/O except two git rev-parses and one bead-body
parse; deterministic; testable with a table. Everything downstream depends on it and nothing in it
depends on anything downstream. Rate of change is high (harness/model precedence churns constantly)
but the *shape* is stable.

**C2 — Resource acquisition.** *Data out:* a set of leases — worker slot, SSH tunnel (port +
endpoint), worktree (path + cleanup), tmux session/pane, hook session, spawn-semaphore token — each
with an idempotent release. **True joint:** every one of these is an acquire/release pair against an
external system, every one leaks if released twice or not at all, and every one has a distinct
failure mode. This is where CARRY-FORWARD facts tmux-3 (new-window can block forever), tmux-4 (global
command lock), pane-19 (ControlMaster), and codex-5 (`$CODEX_HOME` WAL) live.

The current code proves the joint by how badly it handles it: `beadRunOne` has **at least nine
`defer`s registered at seven different points**, with two of them (`relLocalSlot`, `relWorkerSlot`)
carrying mutable flags that later code flips, and comments explaining LIFO ordering constraints
between them (`hk-3hozm` hoisted one release to the top *because four early returns leaked a worker
slot forever*).

**C3 — Execution.** *Data out:* a `ModeResult` = `{success, summary, needs_attention, approve
verdict or nil, subsumed, advisory_rc, terminal node id, tip SHA}`. **True joint:** the three modes
share nothing but this result; each is a different control flow (one-shot / iterate-until-verdict /
walk-a-graph). **This is the seam that does not exist today and whose absence causes the most bugs.**
See §2b.

**C4 — Terminal.** *Data out:* the ordered effect sequence `gate → pre-merge sync → merge (with
retry) → close-or-reopen → emit run terminal → advance queue group`. **True joint:** these are the
only irreversible writes in the whole run. They must happen exactly once and in order.

**This one is already half-built and is the best structure in the repo.** `internal/runexec`
(`run.go`, `dispatch.go`, `vocab.go` — 1,158 lines) is a pure state machine over the terminal
spine; `internal/runloop/runbridge.go` + `runshell.go` are its effector shell. `beadRunOne` feeds it
classified events (`EvAgentCompleted`, `EvCleanExit`, `EvModeOutcome{success|failure|subsumed|budget}`,
`EvShutdownDrain`) and reads `bridge.Success()`. **Keep this. It is the model for the rest.**

### 2b. Where there is genuinely NO seam — and what must be decided

**No-seam #1: the mode boundary (C3). ⚠ REWRITTEN 2026-07-30 — the section below described a tree
that no longer exists, and the operator was briefed off it. What it said, and why it was wrong, is
kept at the end of this sub-section so a reader can tell decay from a claim that was never true.**

There are **two** workflow modes, not three. `core.WorkflowMode.Valid()` in
`internal/core/workflowmode.go` accepts `single` and `dot` and nothing else. `review-loop` was retired
at v0.10.0 (`specs/execution-model.md` §4.3.EM-015d) and its driver deleted in `3cec5afd7`
(2026-07-28), which removed 3,834 lines: `internal/daemon/reviewloop.go` (2,194 lines),
`internal/daemon/launchspecbuild.go`, `internal/runloop/reviewcycle/` and
`internal/runloop/continuity/`. Eight declarations that were never review-loop-specific moved to
`internal/daemon/runsupport.go` and are still called by the graph cascade.

The default is `dot`. `resolveWorkflowMode` in `internal/daemon/moderesolve.go` returns `dot` at
tier 3 (the daemon default, whose flag default is `dot`) and again at tier 4 (the hard fallback).
`single` is reachable **only** through an explicit `workflow:single` bead label, and taking it emits a
`review_bypassed` audit event. So essentially all real dispatch is graph dispatch.

**The launch step is already ONE function.** `runAgentLaunch` in `internal/daemon/agentlaunch.go`
(836 lines) owns "given a built launch spec plus artifacts, spawn the agent, prove it is alive, drive
it to a dead session, hand back the exit facts". It landed on 2026-07-29 across `6077a24dc`,
`ea96471b3`, `18d59aded`, `159b68e78`, `0743e4dec`, `8346edc2f` and `30b5cf02c`. It has exactly three
callers:

| Caller | Where | Reachable in production? |
|---|---|---|
| single-mode tail of `beadRunOne` | `internal/daemon/workloop.go` | Only via an explicit `workflow:single` bead label |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | **Yes — this is the default and carries the traffic** |
| `executeCognitionGate` | `internal/daemon/dot_gate.go` | **No. Dead twice over — see below** |

The cognition gate cannot run. `daemon.Config.CPRegistry` has **zero assignments anywhere in the
tree**, tests included, so `daemonGate.LookupGate` always reports "no registry loaded" and a gate node
returns a structural eval-failure before any launch. Reproduce it with
`grep -rnE 'CPRegistry[[:space:]]*[:=]' --include='*.go' .`, which returns nothing. **Do not
reproduce it with a bare `grep CPRegistry`** — that returns 7 hits, including the field declaration in
`daemon.go` and the read in `workloop.go`. The field exists and is read. Nothing writes it.

**And no graph the daemon runs declares a gate node. Narrowed 2026-07-30 — the evidence covers four
graphs, not the tree.** `type="gate"` appears zero times in the embedded
`internal/daemon/standard-bead.dot`, in this project's `workflow.dot`, in `sonnet-triple-review.dot`
and in `eval-bead.dot`. It is **not** absent from the tree. Measure it with
`grep -rln 'type="gate"' --include='*.dot' .`, which returns exactly one file:
`specs/examples/quality-gate-policy.dot`. That is a spec example and no run path loads it. Every
other match in the tree is a Go test fixture or prose, including this document. The conclusion is
unchanged — counting `dot_gate.go` as a live launch path overstates the live count by one — but state
the four graphs rather than the whole tree, or the next reader greps and finds the claim false.

**What is genuinely still duplicated**, after the launch collapse and measured on 2026-07-30:

- **The post-exit interpretation.** The single-mode tail (`internal/daemon/workloop.go`, ~~about 722
  lines from the mode switch to the end of `beadRunOne`~~ **740 lines / 368 code at `1e22c0141`, see
  the ⚠ in §1c**) and the graph node
  (`dispatchDotAgenticNode`, about 543 lines — **544 measured, this one was right**) each hand-roll
  the same four steps after
  `runAgentLaunch` returns: probe worktree HEAD, emit `implementer_phase_complete`, run the
  process-exit commit fallback, then decide what no-commit means. The steps agree in shape and
  disagree in detail.

  > **CORRECTED 2026-07-31 — "the same four steps" understates it.** There are 17 post-exit steps on
  > the single side and 12 on the graph side. **Six of them are shared, not four** — the four named
  > here plus `defer launch.Cleanup()` and the `ctx.Err()` check. So the split is 6 shared, 11
  > single-only, 6 graph-only. The framing hides three whole classes of divergence that sit between
  > them: the HC-065 lifecycle transition and `SetMachine` (single only), the five-way terminal
  > classification switch (single only), and the reviewer-verdict branch (graph only). **Do not read
  > the two regions as one large duplicate.** Measured from the line after `runAgentLaunch` returns,
  > single is 422 lines (193 code) and the graph is 171 (91 code), but only about 50 to 60 code lines
  > a side are genuinely paired. The full step-by-step table, with a deliberate / accidental / defect
  > verdict per row, is in [`STEP-7-MODE-BOUNDARY.md`](STEP-7-MODE-BOUNDARY.md) §2.
- **Nothing else.** The terminal spine is shared already: both arms build a `runloop.RunBridge` and
  call `WireSpine`, and `runBridgeConfig` is where the two modes' merge-retry budgets are declared
  side by side.

**The steps that differ, path by path.** Split into two groups, because they need different answers.
This split did not exist in the old text, and its absence is what made the whole set read as one bug.

*Group 1 — the spec says these two paths must differ. Do not "fix" these without amending the spec
first.* `specs/run-state-machine.md` RSM-008 reads: "The single-shot path's post-exit guards — the
escaped-worktree check and the no-commit-guard — MUST run in a `Guarding` state between `Dispatching`
and `Gating` … The review-loop and DOT paths MUST NOT enter `Guarding` (they do not run those
guards)."

| Step | single | dot graph node |
|---|---|---|
| escaped-worktree check (`emitImplementerEscapedWorktree`) | yes | no, **and RSM-008 requires no** |
| `noCommitGuardShouldReopen` | yes | no, **and RSM-008 requires no** — ~~the graph does a bare HEAD-advance compare~~ |

> **CORRECTED 2026-07-31 on three points. Verified against the code and the spec at `1e22c0141`.**
>
> 1. **"The graph does a bare HEAD-advance compare" is false.** The graph consults
>    `shared.MainHistoryHasRefsTrailer` — the same subsumption test that is the second half of
>    `noCommitGuardShouldReopen` — and then branches three ways: subsumed, iteration < 2 hard fail,
>    iteration ≥ 2 pass to the diff-hash check. It also has a `node.NonCommitting` opt-out with no
>    single-mode equivalent. So the graph runs an EQUIVALENT no-commit guard inline. **The only thing
>    it lacks is the cross-repo target** (`hk-pq3ex`). An earlier reading of this correction also
>    named the iteration-1 hard fail as missing. It is not missing — the graph implements it verbatim,
>    and it is the same branch listed two sentences above. Do not route porting work off the earlier
>    wording, or you will go looking for something that is already there.
> 2. **The RSM-008 quote above is stale.** The spec at HEAD reads "The **DOT path** MUST NOT enter
>    `Guarding`". Review-loop was removed from it by the v0.2.2 spec pass on 2026-07-30.
> 3. **"RSM-008 is stale in its own right: RSM-007 still names review-loop as a live fork" is false
>    at HEAD.** RSM-007 reads "(DOT cascade, single-shot)". Both mentions were already cleaned by
>    that same pass. **The live staleness is a different sentence:** RSM-008's parenthetical "(it
>    does not run those guards)" is now false, because the graph does run a no-commit guard. That is
>    what D2 has to edit.

One thing still follows, and it is unchanged: extending the escape check to the default path is not
obviously an improvement, because `hk-co8g8` root-caused that same check as firing on innocent runs
and killing them. Decide D2 with that fact in hand.

> **Trap, recorded 2026-07-31 (`hk-u99ha`).** `hk-co8g8`'s body in the bead ledger still describes
> the lost-commit merge race it was originally filed as. It was root-caused the same day as the
> escape guard firing on a sibling's half-finished merge, and only `OPEN-DEFECTS.md` carries the
> corrected account. The citation above is right. A reader who checks it with `br show` will find a
> body that contradicts this section and may conclude this section is wrong.

*Group 2 — nothing specifies these. They are real drift, and ~~four of the five~~ **ten of the
eighteen defects this drift has produced** leave the DEFAULT path worse off, against three that
leave single mode worse off.*

| Step | single | dot graph node |
|---|---|---|
| Pi provider profile on the launch context (`Provider` / `APIKeyEnv` / `APIKeyFile` / `BaseURL` / `API`) | yes | **no** — `hk-yo9g6`. `resolvedProfile` is resolved once in `beadRunOne` and read only by the single-mode `shared.LaunchCtx`. A Pi node on the default path launches with no provider profile. The model does reach it, because `resolvedModel` is overwritten from the profile before the mode switch |
| implementer comms presence join and leave (`emitImplPresence`) | yes | **no**. No spec requires it on either path |
| Pi stderr capture kept beside the retained worktree on failure | yes | ~~**no**~~ **half true now — see the correction below** |
| right harness for the process-exit commit fallback | yes | **no** — the graph calls `codex.EnsureRefsTrailer` for every process-exit harness, and Pi is one. Behaviour is the same, because both wrappers call the same `internal/harness/shared/refstrailer.go` primitives. Only the commit message is wrong: a Pi node's daemon-fallback commit says `feat(codex)`. **Do not "fix" this row alone — see the correction below** |
| independent tmux session, so the run survives a daemon kill and is adopted on the next boot | yes | **no** — `hk-mh3qy`. This is the one capability that makes "just delete single mode" not a one-line change. **`hk-mh3qy` is itself blocked twice — see the correction below** |
| merge retry budget of 3 and the `ChargeReviewLoopFailure` ladder | **no** | yes — deliberate, declared in `runBridgeConfig`. **Misfiled — neither fact lives in `dispatchDotAgenticNode`. See the correction below** |
| `auto_status` work-product inspection | **no** | yes — a graph node attribute, so single has no place to put it |

> **CORRECTED 2026-07-31 against `1e22c0141`. This table is right as far as it goes, and it stops far
> too early.** Full working in [`STEP-7-MODE-BOUNDARY.md`](STEP-7-MODE-BOUNDARY.md) §2 and §3.
>
> **The table is short by eleven rows.** Walking the single-mode tail line by line found **eighteen**
> capabilities that live only there. Across both §2b tables this document names six of them — the two
> guards in Group 1, and the Pi provider profile, the presence join and leave, the Pi stderr capture
> and the independent session here. The other twelve, in the eleven rows below, appear nowhere in
> this document. Worst first:
>
> | Missing row | What the DEFAULT path loses today |
> |---|---|
> | terminal classification (`handler.MapWaitReturnToTerminalEvent` + the five-way switch) | the graph decides node success on "did HEAD advance" alone and never reads the socket outcome. **A node that commits and then signals failure is recorded SUCCESS and merged** — `hk-v4wer` |
> | `bridge.Drain` on the shutdown branch | committed-but-unmerged work is dropped and re-dispatched — `hk-dk0sf` |
> | cold-start spawn semaphore | remote graph runs are ungated on concurrent cold starts — `hk-6n0z7` |
> | `transitionToTerminated` (HC-065) and `handle.SetMachine` | no terminal transition, no `lifecycle_transition`, and `stalewatch`'s silent-hang drive plus `stategather`'s lifecycle read are inert — `hk-b4xf2` |
> | `RunHandle.SetAgentType` | the bandwidth tuner's Pi filter cannot match, so Pi rate limits throttle unrelated work — `hk-a5hs1` |
> | cross-repo `activeRepo` on post-exit reads | `hk-pq3ex`. The **cascade** hardcodes `env.ProjectDir`, and the merge still gets `activeRepo` through `WireSpine` |
> | `sdHarness` | every graph run writes a `sessiondata` record with an empty harness field — `hk-bri7u` |
> | `RunHandle.Aborted()` | a never-spawned-reaper cancel is indistinguishable from a shutdown |
> | stderr tail in the failure reason | an exit −1 crash with no NDJSON leaves no diagnostic |
> | the post-mode scenario gate | the tail leaves `SkipGate` false and the DOT arm sets it true, relying on a `commit_gate` node in the graph |
> | `SkipAbortKill` / `SkipTeardown` and the shutdown early return | the other half of the independent-session row. Port the registry write without these and a bead stays `in_progress` with no reopen |
>
> **Four rows above need a caveat before anyone acts on them.**
>
> 1. **The harness row and Pi no-work detection are one row, not two.** The graph covers Pi with
>    `codex.NoWorkSuspected` **because** it always calls the codex wrapper. Single runs that detector
>    on its codex leg only, so a single-mode Pi run never gets it (`hk-3ywqv`). Add a Pi branch to the
>    graph to fix the commit message and you silently delete Pi no-work detection.
> 2. **The Pi-stderr row is half-stale.** `f6c2613fb` moved the retain-evidence fact to the launch, so
>    a failed LOCAL graph Pi run now keeps its worktree and its captured stdout. Only the post-mortem
>    `pi-stderr.log` write is still single-only. On a remote run retain-evidence still preserves
>    nothing, because the capture lands on the daemon box and the worktree is on the worker.
> 3. **The merge-retry row is misfiled.** Both facts are true and neither lives in
>    `dispatchDotAgenticNode`. `MaxMergeAttempts: 3` is declared in `runBridgeConfig(mode)` and
>    consumed by `NewRunBridge`, which `beadRunOne` calls **above** the mode switch.
>    `ChargeReviewLoopFailure` fires in `beadRunOne`'s DOT arm. So this is not node-level drift and it
>    does not move with the node. The two budgets are also different numbers: merge attempts 3,
>    `queue.MaxReviewLoopFailures` 2.
> 4. **The independent-session row names `hk-mh3qy` as the blocker, and `hk-mh3qy` is itself
>    blocked.** Step 6's map found the survive-shutdown feature does not survive: the boot orphan
>    sweep kills every run session before the adoption pass reaches it, and `WaitWithSocketGrace`
>    kills it earlier still inside the daemon's own process (`hk-jyh5t`). Porting the independent
>    session today ports a capability that is defeated twice before it can pay off.
>
> **Three rows would be better dropped than ported, and one of the three only on a condition.** The
> tail's `pi.EnsureRefsTrailer` branch — both wrappers reach the same primitives in `shared`, so fix
> the graph's one-line harness selection instead. The tail's empty `LaunchCtx.Phase`. And
> `noChangeTimeoutCh`, **but only if the structural subsumed check goes with it.** That third one is
> not "diagnostic fidelity only", although an earlier reading said so. The `case <-noChangeTimeoutCh:`
> arm carries the noChange-subsumed carve-out, the `MainHistoryHasRefsTrailer` check that closes the
> bead **approved**. Delete the channel from the tail on its own and a subsumed run takes `default:`,
> hits `failRun`, and is reopened and re-dispatched instead of closed. In the port it is safe, because
> the graph already has that check inline and already passes `nil` for the channel.
>
> **And one row would be wrong to add.** `runmerge.SnapshotUntrackedFiles` runs ABOVE the mode
> switch, so every graph run already pays for the snapshot and then discards it. Only the consumer is
> tail-only. That is shared code with a tail-only reader, not a tail-only capability.
>
> **The reframe that matters.** Ten of the filed defects harm the default path **today**. Each pays
> off whether or not the single-mode tail is ever deleted, so most of this list is not a cost of Step
> 7 — it is a backlog Step 7 happened to find, and it should be worked by harm order rather than held
> until the tail is ready to go.

> **Decision D1 — ANSWERED by the operator, 2026-07-30: collapse the duplication (option A).**
> Do not re-open this. See §7 for what option A now means in practice, which is not what the
> retired text below assumed.

<details>
<summary>Retired text of no-seam #1 (written 2026-07-27/28, false in part on the day it was written,
fully false by 2026-07-29). Kept so a reader can date the decay.</summary>

It claimed:

- Single mode was "lines 4232–5407 (~1,175 lines)". **Wrong by about 1.6x today.** The single-mode
  tail is about 722 lines. Two things shrank it: the review-loop retirement, and `756b6604c`
  (2026-07-29) which moved the dispatch scheduler out to `internal/daemon/scheduler.go`.
  `beadRunOne` spanned about 1,773 lines in total at that measurement, not 2,289. It is **1,603**
  after Step 5, and the 722 figure has not been re-derived since. See the ⚠ in §1c.
- `runReviewLoop` existed with 19 parameters. **Gone since `3cec5afd7`, 2026-07-28.**
- "Each of the three then re-implements: build launch spec → attach substrate → register hook
  session → `DispatchSegment` with 7 hooks → paste inject → wait with socket grace → probe worktree
  HEAD → force-teardown → emit presence. Four times." **False since 2026-07-29.** Every step in that
  list except "probe worktree HEAD" now happens once, inside `runAgentLaunch`.
- "DOT needs ... a ControlPoint registry." **Never true in the sense implied.** No production code
  ever set one, so the graph path has never consulted a ControlPoint registry.
- D1 asked "is one agent launch one thing, or three?" and treated the answer as unbuilt. The launch
  half of option A **shipped on 2026-07-29**, one to two days after the question was written, and the
  document was not updated.

</details>

**No-seam #2: the guards and the state machine disagree about who owns them.**

`runexec` has `ActCheckEscape` / `EvEscapeDetected` / `EvGuardsPassed`. `RunBridge.WireSpine` wires
`CheckEscape` to a constant `EvGuardsPassed`. The actual escape check and no-commit guard are
imperative in `beadRunOne` immediately before the classification switch, with a comment explaining
that they "run BEFORE the dispatch-terminal classification for EVERY class (the pre-RT7 order); by
the time the machine traverses Guarding they are known-green."

Result: the machine's `Guarding` phase is theatre for single mode, and **does not exist at all** for
DOT, because DOT reaches `WireSpine` without ever running the guard. (Corrected 2026-07-30: this
sentence used to say "for review-loop and DOT". Review-loop was deleted on 2026-07-28. The claim
holds for DOT, which is the default path, so the finding got worse, not better — the one mode without
the guards is now the one carrying the traffic.)

**⚠ Added 2026-07-30 — this section never cited the spec, and the spec disagrees with its framing.**
`specs/run-state-machine.md` RSM-008 says: "The single-shot path's post-exit guards — the
escaped-worktree check and the no-commit-guard — MUST run in a `Guarding` state between `Dispatching`
and `Gating` … ~~The review-loop and DOT paths MUST NOT enter `Guarding` (they do not run those
guards)~~ **The DOT path MUST NOT enter `Guarding` (it does not run those guards)**." So the
asymmetry is **specified**, not accidental drift, and D2 is a request to change the
spec. Two further facts belong in that decision. ~~RSM-007 and RSM-008 both still name review-loop as
a live fork, so they are stale and need editing whichever way D2 goes.~~ And `hk-co8g8` root-caused
the escaped-worktree check as firing on innocent runs and killing them, so moving it into the machine
would put a known-broken guard on the path that carries all the traffic. Fix `hk-co8g8` first.

> **CORRECTED 2026-07-31 — this is the SECOND copy of the same two stale claims, and it was missed
> when the first copy was fixed.** Read `specs/run-state-machine.md` at `1e22c0141`: RSM-008 names
> only the DOT path, and RSM-007 reads "(DOT cascade, single-shot)". The v0.2.2 spec pass on
> 2026-07-30 removed the retired `review-loop` mode from both rules and from RSM-031 and RSM-032.
> Neither rule is stale in the way this paragraph claims, and D2 does not have to edit them for that
> reason.
>
> **RSM-008 does carry one live staleness, and it is a different sentence.** The parenthetical "(it
> does not run those guards)" is now false. `dispatchDotAgenticNode` runs an equivalent no-commit
> guard inline — see the correction under the Group 1 table above. That parenthetical is what D2 has
> to edit, whichever way D2 goes.
>
> **And RSM-008 already carries its own OPEN note**, raised by the same spec pass, which records the
> guard-coverage question and refuses to settle it. D2 is the decision that note is waiting for. It
> also records the traffic split that makes the question urgent: 866 runs started on `dot` against 2
> on `single` on 2026-07-30.

> **Operator decision required (D2): do the guards belong to the machine or to the caller?**
> If the machine — then both modes get the escape check and the no-commit guard automatically, and
> behaviour *changes* for DOT (runs that pass today may start failing). If the caller — then delete
> `ActCheckEscape` from `runexec`, because it is a lie.
> This is a behaviour change either way and needs an explicit call.

**No-seam #3: `SharedHandles` is an admitted escape hatch.**

Its own godoc says adding a field to it "is not a new seam ... only a new PORT INTERFACE would be."
It carries 17 fields, of which 10 are concrete daemon pointers and raw sync primitives:
`*atomic.Int32`, `chan struct{}`, `*sync.Mutex` ×2, `map[core.BeadID]struct{}`,
`*workers.Registry`, `*handlercontract.HarnessRegistry`, `*handlercontract.AdapterRegistry`,
`*core.TransitionIDGenerator`, and a raw `WorktreeFactory` func. Anything reached through it is
untestable without the daemon.

Worse, three dependencies exist **twice** across the bundles:
- ledger: `RunPorts.Ledger` (3 methods) *and* `SharedHandles.BrAdapter` (5 methods)
- worktree: `RunPorts.Worktree` (port) *and* `SharedHandles.WorktreeFactory` (raw func)
- launch: `RunPorts.Launch` (port) *and* `RunPorts.LaunchBuilder` (the identical closure, stored twice)

**No-seam #4: `RunPorts` is constructed in three places, one of them inside the consumer.**
`deps.runPorts()` fills 5 fields; `buildRunBundles` fills `Launch` + `LaunchBuilder`; and
**`beadRunOne` itself assigns `rp.Worktree` at line 3743**, because the choice between the local
worktree factory and the SSH one is not knowable until after remote-worker selection. So `RunPorts`
crosses the seam half-valid and the consumer completes it. Any rewrite must either move worker
selection before port construction, or accept that "resources" is a separate phase that *produces*
ports rather than receiving them — which is what C1→C2 ordering says anyway.

**No-seam #5: `workLoopDeps` is not constructed, it is assembled in four stages.**
Production requires `newWorkLoopDeps` → `seedGovernorDeps` → an inline boot-seed →
`injectWorkLoopDeps`, all in `bootworkloop.go`. **~25 of the 81 fields are set after
construction.** There is no point at which the compiler can tell you the bundle is complete. And
`bootworkloop.go`'s `wireStaleWatcherReapSeams` captures a `*workLoopDeps` in a long-lived closure
that a background watchdog dereferences by value — so the bundle outlives the boot function and is
co-owned by two goroutines.

---

## 3. The order

The ordering rule is: **carve out things whose data flows one way and whose absence the compiler can
prove.** Anything requiring a behaviour decision goes late and gets flagged.

> **Where the order stands, measured 2026-07-30.** Step 0 items 1 and 2 are done and item 3 is now a
> test-repair job. Step 1 is retired. **Steps 2, 3a, 4 and 5 have LANDED**, each with the commit named
> under its own heading. **Steps 6 through 15 are NOT STARTED**, and none of them was done under
> another name — the tree was checked for each. Step 3b, step 7's first piece and step 8 are the parts
> of the early steps that remain.
>
> **Added 2026-07-30: Steps 19 and 27a, and a candidates section.** Step 0 gained two prerequisites,
> items 4 and 5. Two new steps sit after Step 16 and keep the numbers they were proposed under, so
> the numbering jumps. **A missing number between 16 and 27a is a candidate, not a lost step** — see
> §3b, "Candidate work beyond Step 16", which also records four items that need an operator decision
> because they amend `CHARTER.md`.

### Step 0 — prerequisites (already in the plan, restated because they gate everything)

**Re-measured 2026-07-29: items 1 and 2 are DONE. Items 3, 4 and 5 are outstanding, and item 4 is
operator-gated.**

1. ~~Reconcile `origin/integration/phase-reviewloop-20260725` (44 stranded commits).~~ **DONE.** Its
   contents are known, its worthwhile spec clauses were harvested by hand and checked against the code
   rather than taken on trust, and its tip is preserved at
   `origin/salvage/reviewloop-kernels-20260729`, whose tip is byte-identical to the integration tip.
   `NEXT_STEPS.md` item C carries the detail, and the harvest is settled rather than hopeful — the
   v0.9.6 changelog row in `specs/execution-model.md` records that the prose was checked against the
   code before amending, and names what was deliberately left behind.

   **Re-measured 2026-07-30: the branch and its worktree are both GONE.** This item used to end by
   saying the branch still existed and that a worktree at
   `/private/tmp/harmonik-main-integration-20260725` had to be removed before it could be deleted.
   `git ls-remote --heads origin` no longer lists `integration/phase-reviewloop-20260725`, and that
   worktree path does not exist. **The salvage tip does still exist** —
   `origin/salvage/reviewloop-kernels-20260729` is on the remote today, so the contents are still
   recoverable. Nothing here is outstanding.
2. ~~Finish the test-mass deletion, including the 4 unit-test files that bind `beadRunOne`'s
   7-parameter signature.~~ **DONE as written, but read the caveat.** Exactly **one** test file calls
   `beadRunOne` today — the shim at `internal/daemon/export_workloop_test.go`. Against one production
   caller, that is the shape §2 Seam A wants: two call sites, the dispatch goroutine and one test shim.

   ⚠ **"Two call sites" reads as more freedom to restructure than you actually have.**
   `internal/daemon/conformance_m4c7_test.go` does not merely mention `beadRunOne` — it parses the
   daemon source, finds the function's declaration, and asserts that a credential guard is a top-level
   statement dominating the launch call. That binds `beadRunOne`'s **internal statement structure**, not
   its signature, so the compiler will not warn you. Its own header records that it already broke once
   when the launch path was collapsed. Expect to update it whenever statements move inside that
   function.
3. **Make the scenario tier merge-blocking. Re-briefed 2026-07-30 — the YAML half is DONE and the
   item is now blocked on test failures, not on a config line.** The rewrite's *only* oracle is
   `internal/daemon/scenario_*` (26 files, 54 test funcs) + `test/scenario/` (11 tests, 27 s). This
   item used to read "this is one line of YAML". That line landed: `1ee9154e8` corrected the false
   claim that made the tier look green, and `7f1028316` removed `continue-on-error: true`, so the
   workflow now reports its own failures.

   **What remains is not a config change.** The tier is genuinely red — 8 deterministic failures that
   reproduce in isolation, plus load flakes that pass when run alone. Most of the 8 are stale tests
   (`hk-97gcz`); one is a real defect in the merge path (`hk-co8g8`). Separately, the workflow
   installs no `br` and declares no twin build, so about half the tier skips and a skip reads as a
   pass (`hk-ynohn`). `.github/workflows/scenario.yml` carries its own warning not to add itself to
   the required status checks until the 8 close, because doing so wedges every merge. So: close the 8,
   fix the skip, then make it required. **You still cannot validate a rewrite against a gate that
   blocks nothing** — but the work to get there is test repair, and it should be priced as such.

4. **Reconcile `main`. OPERATOR-GATED — an agent must not do this alone.** The fix needs a push to
   `origin/main`, and that is the operator's call. `origin/main` last moved on 2026-07-22 and forked
   from this branch on 2026-07-19 — `git log -1 --format=%ci $(git merge-base origin/main HEAD)`
   reads `2026-07-19 01:06:06`. Measured 2026-07-30 at `89782676d`,
   `git rev-list --left-right --count origin/main...HEAD` reads **27 on the `main` side and 532 on
   this one**. The second figure climbs with every commit that lands here, so re-measure rather than
   quote it. All 27 were read. **Two are real fixes that already reached the tip under different
   hashes** — the remote-cwd-aware direct-exec spawn (`hk-fufel`, on the tip as `3e8a96a10`) and the
   remote-cwd-aware ssh spawn (`hk-czb11`, on the tip as `942069ebf`). **The other 25 split two ways,
   and "25 probe markers" is too loose.** Eleven add a marker file and nothing else — `PROBE.md`,
   `PROBE4.md`, `CONC1.md`, `CONC3.md`, `docs/regate-seq.md` and `docs/conc-a.md` through
   `docs/conc-f.md`. The remaining **fourteen are two code commits that the probe runs re-applied
   seven times each**: `hk-4je`, strip run-context from merge, and `CHB-023`, persist
   `claude_session_id` to `Run.context`. Both subjects are settled on the tip under other hashes —
   the run-context strip landed and was later fixed and covered, and the `CHB-023` machinery was
   deliberately deleted in `60692dafe`. Confirm both before you discard the branch.

   **Three things key off that branch, which is why a stale `main` is an instrument fault and not
   housekeeping.** `make check-short` computes its lint delta with
   `golangci-lint run --new-from-rev=origin/main`. Branch protection's only required check runs there
   — `gh api repos/gregberns/harmonik/branches/main/protection` returns exactly one entry,
   `check (Tier 2)`. And the agent-worktree tool cuts every new worktree from it. **How you know it
   worked:** `git rev-list --left-right --count origin/main...HEAD` reads `0 0`, and `PROBE.md` is
   gone from the tip.

5. **Repair the measurement commands, then prune the agent worktrees.** A tree-wide
   `find . -name '*.go'` walks nine agent worktrees under `.claude/worktrees/` and counts the same
   code up to ten times. Measured today: **8,916 production Go files against a true 897, and
   2,138,555 production lines against a true 214,604**. Test files are worse — 11,074 against a true
   1,046, and 3,276,799 lines against a true 309,509. Add `-not -path './.claude/*'` to any
   `find`-based count. Two of the nine worktrees also still hold `internal/daemon/reviewloop.go`,
   deleted on 2026-07-28, so a tree-wide search finds code this program already removed. The nine
   hold about **842 MB**.

   **Check before you rewrite. Part of this is already done.** `UNWIRED-INVENTORY.md` already carries
   `-not -path './.claude/*'` on every `find` in its measurement table. The commands still without it
   are in `KEEP-DELETE.md` and `NEXT_STEPS.md`. The two tree-wide `grep -r` commands in §2b of this
   file were re-run today and give the same answer with and without the worktrees, so do not "fix"
   them for symmetry. A search for a symbol is not automatically wrong the way a file count or a line
   count is. **How you know it worked:** the documented command and the corrected command return the
   same number.

### Step 1 — free deletions — ⚠ RETIRED 2026-07-29. Nothing here is both free and outstanding.

Every item was re-checked against the tree. **All five were wrong or already satisfied**, so this step
no longer gates the structural work below and **step 2 is now the first thing to do.**

- `activateFirstPendingGroup` — **already deleted.** Only the `…Locked` variant remains, with callers.
- `beadExplicitlyReopened` + its one test file — **already deleted.** The claim above said two files.
- `RunPorts.LaunchBuilder` — **not a zero-caller duplicate.** The graph path
  (`dot_cascade_core.go`) and the cognition gate (`dot_gate.go`) both read it, and
  `internal/runloop/ports.go` documents it as the raw resolved spec builder that sub-drivers reach
  after the deps drop. Deleting it breaks two callers. The claim was never true.
- The `sandboxOSTmpDirs`, `hk-l5saf` and `hk-f38n` comment blocks — **real knowledge, not
  gravestones. DONE 2026-07-29, and the disposition was not uniform.** See §1d. One fact went to
  `CARRY-FORWARD.md` and its source copy is deleted, one fact was already in a doc and its comment
  stays untouched, and one fact went to `specs/beads-integration.md` §4.7 with a short comment kept at
  the edit site. Only one of the three was in `workloop.go` by the time the work ran, and only one of
  the three ended in a deletion.

**The general lesson, which is the part worth keeping:** decay is the smaller half of the problem here.
Only one entry — `activateFirstPendingGroup` — was true when written and then went stale. The other
four were wrong on the day they were written. `LaunchBuilder` already had two callers. The "two test
files" count was one file. The "42 lines" count was 49, and it did not even match the line range
printed beside it. And calling three blocks of live rationale "where this used to be" comments was a
misreading, not a fact that expired.

So the instruction is not only "re-derive the list before acting on it", though do that too. It is
that a list which says the compiler will prove it invites you to skip reading the thing you are about
to delete. §5 already says plan estimates do not survive contact. A deletion inventory is an estimate,
and it is one that reads as a fact.

### Step 2 — lift the cadenced maintenance out of the poll loop — ✅ **LANDED**

**✅ DONE — `internal/daemon/loopmaintenance.go`, 283 lines.** Built at `054b7bd92` and merged at
`05edae952`. The paragraph under Step 3 constraint 6 already read "Step 2 as BUILT"; only this heading
was never updated. The description below is the brief the work was done from. Read it as the design,
not as outstanding work.

Responsibilities 6–13 of `runWorkLoop` (schedule tick, coordinator reap, disk check, dashboard gate,
eager refill, sentinel observe, sentinel ACT) are already *mostly* in their own files
(`scheduletick.go`, `diskcheck_hksxlb.go`, `dashboardgate.go`, `eagerfill_em063.go`,
`internal/sentinel`). What remains in `runWorkLoop` is the cadence arithmetic and the call.

**Why first:** these have a different lifetime (periodic, not per-bead), a different failure mode
(a failed reap is logged and forgotten), and they are the only part of `runWorkLoop` that is not on
the dispatch critical path. Removing them makes the remaining loop readable.

**What crosses:** a `MaintenanceObservation` = `{disk_low bool, blocked_queues map[string]bool,
governor_signal, halt bool}`. Note the existing `loopMaintenanceState` already isolates exactly the
mutable state this needs.

**Risk:** low. One caveat — sentinel ACT mode can spawn a crew and halt the daemon. That is a
dispatch effect emitted from a maintenance pass, so the observation type must carry `halt`
explicitly rather than the maintenance code calling `exitClean` itself.

### Step 3 — the admission decision — ⚠ SPLIT REQUIRED. Re-measured 2026-07-29.

**Read this correction before the original text below.** Every one of the eight gates was read in the
source. **Four of the step's five claims are false.** Only the ordering claim holds. Do not start this
step from the original description.

**The claim, and what the code says:**

| Claim | Measured |
|---|---|
| All eight are `snapshot → allow\|delay(reason)\|stop` | **False for four.** cross-queue-dedup and attempts-bound(a) write durable state and fire the group-advance effect chain. handler-pause emits an event on its hold path, and on BOTH paths it may clear the dedup map when the pause epoch has advanced. cooldown's arm site mutates its own input. **Four fit the shape** — decision-required, sentinel-queue, local-cap and greenlight — but greenlight does not fit it *over the same snapshot*, which is the real finding. |
| None does I/O except greenlight | **False, and backwards.** handler-pause writes the event log. cross-queue-dedup and two of the three attempts bounds write `queue.json` and emit events. greenlight writes stderr exactly as decision-required and sentinel-queue do, so it is not the I/O exception — local-cap is the only gate that touches nothing, reading an atomic and sleeping. What is true, and is the point worth keeping: greenlight is the one gate that cannot run without a `br show` result in front of it. |
| ~200 lines | **305 code lines** for the eight gate bodies on both dispatch paths plus the helpers the fold must re-own (`heldDedupKey`, `emitHeldEvent`, `pruneHeldDedupOnEpochChange`, `dispatchBlocked`, `markQueueItemFailureReason`), excluding `evaluateGroupAdvanceWithOutcome`. State that region set with any raw-line figure or do not quote one — an earlier draft of this table said "435 raw" and a re-measure of the same regions gives 411. The 305 excludes the **115-code-line** pre-claim `br show` block the sequence is built around, and the step cannot be done without deciding that block's fate. |
| Low risk | **No.** The two gates that cannot be reordered are the concurrency-critical ones. See constraints 4 and 5. |
| The sequence is load-bearing, local-cap before the stamp | **True.** The only claim that survived. |

**Every gate named below lives in `internal/daemon/scheduler.go`**, moved there from `workloop.go` by the
Seam A split. Their own source comments still say `workloop.go`. None of them is a named symbol — each is
an inline statement block inside `runWorkLoop`, and its only greppable handle is its bead-tag comment.

**The eight are not a list, because they straddle a subprocess.** cooldown exists to suppress repeated
`br show` calls at poll cadence, so it must stay BEFORE the pre-claim `br show`. greenlight reads
`preClaimRecord.Labels`, which is the zero value until `ShowBead` returns, so it must stay AFTER it.
Those two requirements are directly contradictory for a single pre-claim predicate list over one
snapshot. Worse, moving greenlight earlier **compiles clean and the gate silently never fires**, because
`preClaimRecord` is declared above the block. That hazard holds for the window between the declaration
and the assignment — above the declaration it would not compile, so the trap is narrower than "anywhere
earlier" and no less real.

**The three attempts bounds, named once so the constraints below can refer to them:** **(a)** the
`Attempts++` / `max_attempts_exceeded` site inside the Phase-3 lock, **(b)** the `hk-pina9` pre-claim
`ShowBead` bound, **(c)** the br-ready `readyPathAttempts` bound.

**Ordering constraints that exist only as the current source order:**

1. cooldown before the pre-claim `br show` — else the subprocess it exists to suppress runs every tick.
2. greenlight after the pre-claim `br show` — else it reads a zero value and never fires, silently.
3. local-cap before the Phase-3 dispatch stamp (`hk-l5saf`). Its safety also depends on `localInFlight`
   not being incremented until the post-claim site, so the fold must not move that increment earlier.
   **These are two clauses and only the first is pinned.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard`
   catches the gate moving after the stamp, but it cannot catch the increment moving earlier: it preloads
   `localInFlight` to `gateMax`, so the guard reads `1 >= 1`, and hoisting the increment makes it `2 >= 1`
   — the same branch, the item still Pending, the test still green. Nothing in the tree pins the
   increment's position, and that position is the hoist's own safety argument.

   **Correction, 2026-07-30: the first clause was not pinned either, and had not been for as long as
   this machine has been low on disk.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard` did not stub the
   disk-free reading. The dispatch loop holds a tick when disk is below the watermark, and that hold
   sits BEFORE queue selection, so the fixture never reached the guard. Proven by deleting the guard
   outright: the test stayed GREEN. With a one-line disk stub added and the guard still deleted, it
   goes RED. Both clauses are pinned now — the first by the repaired fixture, the second by
   `TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount`, which always stubbed the reading and so
   never rotted. The wider finding, that the whole `internal/daemon` package is green on a machine
   above the watermark and that 32 of its 36 loop-driving fixtures share this exposure, is in
   `OPEN-DEFECTS.md`.
4. cross-queue-dedup must run inside the same write-lock hold as the stamp. The lock is what makes the
   winning queue's stamp visible. A pure pre-claim predicate cannot hold it, and without it two
   implementers run one bead again — the bug `hk-a11re` fixed.
5. attempts-bound(a) must stay fused to the stamp. `Attempts++` happens only when the item is found
   Pending under the live lock, so only real stamp attempts consume the budget. A pre-lock predicate
   either double-counts or loses that property.
6. `governor.tick` must run before the sentinel-queue gate in the same tick. This couples Step 3 to
   Step 2: either the maintenance observation carries the trip state, or the gate keeps reading the
   blocker directly. **Step 2 as BUILT keeps the direct read**, through `sentinelBlocksDispatch`, because
   the blocker is a dispatch-time question and a verdict captured during the maintenance pass would be
   answered hundreds of lines before it is used. Step 2's own description above still lists
   `governor_signal` as a field of the crossing type. That field was never added — it has no consumer in
   the loop — so read the code, not that field list.

   **This constraint protects a code shape, not a behavior — established 2026-07-29 by enumerating every
   writer, not by reading a region.** The `sentinel` subject has exactly one steady-state writer,
   `movementGovernor.onTrip`/`onClear`, on the dispatch goroutine, plus one boot-time writer with no
   reload path. `DecisionBlocker`'s mutators are declared by no interface anywhere in the repo — it is
   only ever held as a concrete type, so there is no structural back door — and `internal/sentinel`
   cannot reach it at all, because the import would be a cycle. That is why the subject constant is
   duplicated in the daemon package. So nothing between the maintenance pass and the gate can change what
   the gate returns, and a snapshot would be equivalent to today's live read.

   **With one condition that is the whole point:** `governor.tick` is the LAST statement of
   `tickBeforeSelect`, so a snapshot is equivalent only if taken AFTER it. Taken at the top of the pass,
   the daemon dispatches one extra bead on the tick a trip first fires — the same hazard
   `loopmaintenance.go` already documents for the `halt` field. So the fold may snapshot this, but where
   the snapshot sits is load-bearing.

   This constraint is now **tested**, in `internal/daemon/sentinelgate_test.go`. The seam it needed is one
   field, `WorkLoopDepsParams.GovernorState`, wired straight to `workLoopDeps.governorState`; nil keeps the
   subsystem absent, so no existing fixture changed. ACT mode was not needed — `dispatchBlocked` reduces to
   `decisionBlocker.IsQueueBlocked("sentinel")` and the trip is injected through `AddQueueBlock`. The tests
   pin the gate on BOTH dispatch paths, pin that an absent subsystem does not gate dispatch, and pin the
   ordering clause: a trip armed inside `governor.tick` gates the SAME tick. A snapshot taken at the top of
   `tickBeforeSelect` produces exactly one claim instead of zero, so the fold will hear about it.
7. The two dispatch paths order the same gates differently. The br-ready path puts attempts-bound
   before handler-pause. The queue path has no early attempts bound at all. One merged order therefore
   changes one path: today a ready bead over its budget is skipped with no held event, where the queue
   path would hold it and emit one.
8. `delay` is two different outcomes, and **the counts here were wrong on both sides until 2026-07-29**
   — this entry said twelve sleeping sites and named three no-sleep ones. Re-derived twice, by two
   methods: `runWorkLoop` has **31 outer-loop `continue` statements**, of which **26 wait** and **five do
   not**. The five: the queue bootstrap, the `hk-pina9` pre-claim `ShowBead` bound, cross-queue duplicate,
   the `hk-6pspu` max-attempts **stamp** bound, and the `hk-n91y0` claim-blocked path. Keep the word
   "stamp" — `hk-6pspu` tags two sites and the br-ready one sleeps.

   **The 26 are not one behavior, and a merged `delay` must carry a bound plus a wake-set rather than a
   duration.** The nothing-selected `continue` is a single statement with **three wait shapes**: a
   2-second `workloopSleep` when deferred items remain; a 2-second wait that also selects on the schedule
   channel when an enabled scheduled job is loaded; and `workloopIdleWait` with **no timer** when neither
   holds. Shape 2 is bounded by a flat `time.After(workloopPollInterval)` — the same 2 seconds as shape 1,
   with no next-fire-time arithmetic — so never describe it as waiting until the next scheduled job time.
   Shape 3 is untimed but wake-interruptible: the daemon parks, it does not stall, and putting a timer
   there restores the busy-poll `PL-013` forbids. By timeout semantics there are only two shapes; the
   third appears only when you count select shape. Counting immediate-continue, the loop has **four**
   distinct delay outcomes.

   That same branch has a fourth outcome that is not a wait at all: when no queues are loaded it falls
   through with neither a wait nor a `continue`.

   **Do not merge the two variants into one on the assumption that a merge is only slower.** That holds
   only toward the sleeping variant. The other direction busy-spins the `hk-403fw` cooldown — the
   `bead_claim_skipped` storm the cooldown exists to stop.

   And the sleeping direction is not free either, because the cost is not symmetric across the five.
   `evaluateGroupAdvanceWithOutcome` calls `queueStore.Wake()` unconditionally on its
   not-all-succeeded branch, so four of the five already have a wake token pending and merging them
   toward sleep costs **zero** latency, not one interval. The bootstrap site is the exception:
   `activateFirstPendingGroupLocked` writes through `LockedSetQueueByName`, which does not signal the
   wake channel, so merging **that one** site costs a full poll interval on every queue submit.

   The wake-token fact is now pinned by an executable test with no wall clock in it, so this entry no
   longer rests on reading the code.
9. decision-required and sentinel-queue are freely swappable. The only difference is the stderr string.
   This is the one pair with no real constraint.

**Nothing asserts any of this except one edge.** `TestL5saf_LocalOnlyItemNotStrandedByCapGuard` drives
a real tick and fails if local-cap moves after the stamp. Its own header records that the sibling unit
test missed the bug because it only re-stated the boolean. Five of the eight gates have no test at all —
cooldown, decision-required, sentinel-queue, greenlight and attempts-bound. The freeze gate asserts file
placement and symbol counts, not order.

That test header is itself decaying: the sibling it names, `TestSplitGate_LocalOnlyBypassFix`, was
deleted in the signature-pinning sweep, and the header still places the gate in `workloop.go`.

**And the comments that hold the reasons are already rotten. This rot is ours, and it is one commit old.**
All six line references in the gate region are wrong. Read them as symbols in
`internal/daemon/scheduler.go`: the split capacity gate is the `if` on `deps.localInFlight.Load() >=
gateMax`, the twice-cited reference is the `deps.localInFlight.Add(1)` increment, the third is the
`beadRecord = core.BeadRecord{` construction, and the fourth — also cited twice, and uncorrected until
now — is the queue-path post-claim `ShowBead` label hydration.

The `localInFlight.Add(1)` reference is the load-bearing half of the local-cap hoist argument, so the
only record of why the order is safe points at coordinates that no longer exist. **At the commit that
wrote them, two of these numbers were exact.** The Seam A split moved the code and updated none of them
— which means four of the six were broken by this program's own commit, one commit before a commit
titled "cite the symbol instead of a line number." This is a defect fixable today, not slow decay.

**The eight are also not the set.** The same loop holds **four** more admission gates: the primary split
capacity gate (`hk-hs7ex`), the disk-low gate, the no-auto-pull gate (`hk-exd7m`) and the operator-pause
gate (`hk-ry8q1`). Two more sit outside the loop: the `needs-attention` exclusion at adapter read time in
`internal/brcli/ready.go` beside greenlight, and `HandlerPauseChecker` in `internal/queue/validation.go`
at submit time, which is a sixth handler-pause site. If the goal is "the admission decision lives in one
place", name the real set first.

**Do this instead — split the step:**

- **Step 3a — ✅ LANDED.** Built at `78d966abd` and `0b7857c1b`, merged at `0e2bafbe3`. The gates now
  live in `internal/orchestrator/admission.go` (453 lines): `AdmitAtTick`, `BeforeLookup`,
  `AfterLookup` and `BeforeStamp` are the four stages, and `ErrGateInputMissing` is how a gate asked
  to run before its inputs exist fails loudly instead of reading a zero value. The brief it was built
  from follows. Read it as the design, not as outstanding work.

  Fold the genuinely pure predicates — decision-required,
  sentinel-queue, local-cap, and greenlight once the bead record is in hand — into
  `internal/orchestrator` beside `SelectNextQueue`. Encode the order as data, not scattered `if`s.

  **Re-measured 2026-07-30, and the step's own description repeats Step 3's mistake at smaller scale.**
  Three corrections, each of which changes what the work is:

  **The queue-path figure was exact and the both-paths figure was not.** 42 code lines for the four
  gates on the queue path is right, counted body-only with comments and blanks excluded. The
  both-paths figure was 66. It is **60**. The 6-line gap is the `hk-l5saf` guard counted a second
  time, on a path where it does not exist — **the br-ready path has no local-cap guard at all.** Only
  a vestigial comment on the converged path marks where the old post-stamp version sat. That is
  sound rather than a defect: the guard keys on the per-queue local-only routing field, and a
  br-ready bead comes from no queue, so the field has no meaning there. The br-ready path relies on
  the tick-level split capacity gate alone. Record it so nobody "restores symmetry" later.

  **"local-cap" names two gates, and they share a tick-local.** The split capacity gate
  (`hk-hs7ex`) runs once per tick before the path branch and is silent. The `hk-l5saf` guard runs on
  the queue path only, immediately before the dispatch stamp, and is also silent. The cap they
  compare against is derived ONCE per tick, at the first gate, and read again by the second. Fold one
  without the other and that single derivation splits in two, which changes behaviour whenever the
  concurrency controller is adjusted between the two reads. Counting the derivation and the split
  gate, the real region is **72 code lines**, not 60.

  **Greenlight is queue-path only.** The br-ready path excludes those beads earlier, at adapter read
  time in `internal/brcli/ready.go`, beside the needs-attention exclusion. That is a different
  package and a different moment, and it stays out of this step.

  **So the order cannot be one flat list here either, for the same reason it could not be in Step 3.**
  The four gates sit at four points in a tick: before the path branch, before the pre-claim `br show`,
  after it, and immediately before the dispatch stamp. Two subprocess-and-lock boundaries cut through
  them. The step is therefore to make the **stage** data as well as the order, so that a gate asked to
  run before its inputs exist FAILS LOUDLY. Today the reverse is true, and that is the whole point:
  hoisting greenlight above the `br show` compiles clean, reads a zero value, and the gate silently
  never fires.
- **Step 3b (MEDIUM risk, belongs with Step 4).** cross-queue-dedup and attempts-bound(a) are a
  reservation-transaction problem wearing a gate costume. Fold them into Step 4's single durable write,
  not into an admission list.
- **handler-pause needs its event emission split from its predicate before it can move at all.**
- **cooldown stays where it is.** It is a rate limiter on a subprocess, not an admission gate.
- **Before either half, write the tests that pin the order.** Six of the nine constraints above have
  nothing holding them.

---

**Original text, kept because the correction above is a response to it. It is the only place the eight are
named as one list. Every ⛔ sentence below is REFUTED — do not quote one as current fact.**

Fold the remaining gates (handler-pause, decision-required, sentinel-queue, greenlight, cooldown,
local-cap, cross-queue-dedup, attempts-bound) into `internal/orchestrator` beside the existing
`SelectNextQueue`. ⛔ REFUTED: ~~All of them are `snapshot → allow|delay(reason)|stop`. None of them do
I/O except `greenlight`, which reads a label already present on the pre-claim record.~~ Four fit the
shape, not eight, and greenlight is not the I/O exception.

**Why now:** the pure selector already landed there and works; this is the same shape. ⛔ REFUTED:
~~It is table-testable with no daemon.~~ Two of the eight need the live write lock.

⛔ REFUTED on risk, upheld on sequence: ~~**Risk:** low, but~~ note the *sequence* is load-bearing — the
hoisted `hk-l5saf` local-cap guard must stay **before** the dispatch stamp. Encode it as an ordered list
of predicates, not as scattered `if`s, so the ordering is data.

### Step 4 — the reservation transaction (~150 lines, MEDIUM risk — real fix, not a move)

Collapse the dispatch stamp + RunID patch into one durable write, and make persist failure fatal to
the dispatch (no claim, no launch). Today it is two non-fatal writes with an empty-string placeholder
between them.

**Why here:** it must precede any run-path work, because the run path's recovery semantics
(QM-002a on restart, `adoptLiveRunSession`) depend on what a dispatched item means.

> **D3 — ANSWERED 2026-07-30. A failed reservation write aborts the dispatch: no claim, no launch.**
> Operator: *"No more launching and hoping."* The bead stays pending and the next tick re-picks it,
> which is correct, because it never started. Retry with backoff is a later layer, not the first move —
> a full disk or a corrupt queue file does not clear on retry, and retrying quietly rebuilds the failure
> this decision removes. The error must be loud: an event, a visible marker on `queue list`, and a
> message specific enough that an agent investigates rather than shrugs.

**✅ DONE 2026-07-30 — `internal/daemon/scheduler_reservation.go`.** The dispatch reserves through
`QueueStore.Transact` in one write that sets status and RunID together. A write that does not reach
disk abandons the dispatch: no claim, no launch, item stays pending. Pinned end-to-end by
`TestReservationWriteFailure_NeverClaimsAndNeverLaunches`, which drives the real `runWorkLoop`.

Four things found on the way, none of which the step anticipated:

- **The event the spec demands could not be built.** `queue-model.md` QM-001 names
  `failed_prerequisite: queue_write_error`; `event-model.md`, which owns the event type, listed
  seven values and that was not one. Added to both the owner spec and `internal/core`.
- **QM-001 asks for three things and the code did none.** Refuse further mutations, emit, go
  degraded. `Transact` now quarantines on any I/O-failure outcome — but NOT on a pre-I/O refusal,
  which attempted no write. Getting that wrong broke an existing concurrency test.
- **A quarantined queue read as "retry later" and spun silently.** `queuewiring.ErrQueueQuarantined`
  separates "this queue is shut" from "your snapshot is stale". The report is bounded to once per
  queue or a sticky failure emits every poll interval forever.
- **14 test fixtures used non-UUID queue IDs**, which the spec forbids and the durable write rejects.
  Five tests regressed the moment dispatch used the real write. Fixed, not suppressed.

**Nothing is open. Re-checked 2026-07-30.** This paragraph used to read "Still open: D3's marker on
`queue list` (`hk-ujanf`)". That landed at `67c2e7615` and merged at `15bfdc154` — a shut queue now
says so on the read commands. Step 3b's cross-queue-dedup fold landed here via
`TransactionRequest.Precondition`, and attempts-bound(a) landed in the same reservation write.

**Original assessment, kept because the work confirmed it:**

**⚠ The premise of this step is stale — re-verified 2026-07-30. Step 4 is mostly WIRING, not writing.**

`QueueStore.Transact` in `internal/queuewiring/store.go` already implements the reservation
transaction, backed by `internal/queue/transaction.go` (1,005 lines). Verified today: **all ten
`.Transact(` call sites are in `queuewiring/store_transaction_test.go`. Production callers: zero.**
The dispatch path uses bare `queue.Persist` — ten calls in `internal/daemon/scheduler.go`.

`Transact` supplies each thing this step was going to build:

- **`Mutate func(*queue.Queue) error`** — one closure sets status *and* RunID together, so the
  empty-string placeholder stops existing. The RunID is a UUIDv7, generated locally from a timestamp
  plus randomness; nothing stops it being generated before the stamp instead of after.
- **Generation guard plus a byte-equality check on the snapshot** — a stale or concurrently-mutated
  queue rejects *before* any I/O. This is the defence against a second writer.
- **Quarantine** — refuses further mutations on a queue that failed a write, which is QM-001's
  "refuse further mutations".
- **`TransactionResult.Outcome`** — a value the caller must read, rather than an error that
  `_ =` can discard.

**What `Transact` deliberately does not do**, per its own comment: *"performs no event emission and has
no completion-receipt behavior."* So `infrastructure_unavailable{failed_prerequisite:
queue_write_error}` is still the caller's job. `internal/queue/persistence.go` says the same thing and
names the wiring point. The payload type exists in `internal/core`. **It is emitted nowhere.** That
emission, not the transaction, is the part of Step 4 that must actually be written.

Do not restate this step as "collapse two writes into one" without first re-running
`grep -rn '\.Transact(' --include='*.go' internal/ cmd/`. If production callers are still zero, the
work is to route `scheduler.go` through the owner that exists and to wire the event — not to design a
new transaction. Background: `UNWIRED-INVENTORY.md` row 5, and the 21-slice plan in
`.kerf/works/queue-transaction-contract/07-tasks.md` with one slice built.

### Step 5 — the run-plan resolver — ✅ **LANDED**

**✅ DONE — `internal/daemon/workloop_runplan.go`, 580 lines.** Built at `4070c75ed` and merged at
`44b3a7e4d`. The socket-path-length hoist named at the end of this step landed separately at
`fabc7cb21`. The brief it was built from follows. Read it as the design, not as outstanding work.

⚠ **The step left two production functions with no production caller.** `resolveBranching` and
`resolveParentCommit` in `internal/daemon/branching.go` are now reached only through the test shims in
`internal/daemon/export_branching_test.go`. `grep -rnE '\bresolveBranching\(|\bresolveParentCommit\('`
over `internal/` and `cmd/` returns the two shim lines and the two declarations, and nothing else. This
is the same shape as the Step 1 entries this map retired for being wrong, so it is recorded here rather
than asserted as free: **re-derive it before deleting anything.** Retiring the two belongs to a later
step, not to Step 5, because `branchguard_test.go` and `runplan_precedence_test.go` assert behaviour
through those shims and that behaviour has to keep a home first.

Pull C1 out of `beadRunOne`: workflow mode/ref, harness agent type, model/effort, Pi profile,
active repo, parent commit, lands-on, merge target, protect-branch check, placement intent. All
decided before any resource is acquired.

**⚠ The stated justification below was stale — corrected 2026-07-30.** This step used to be sold as
the fix for the `hk-3hozm` worker-slot leak. **That leak is already fixed.** The bead closed
2026-07-18 in commit `3a391dc9`, which hoisted the release to a top-level `defer` in `beadRunOne`
above every early return. Re-verified today: **zero returns leak a slot.** The motivation survives
unchanged — resolving before acquiring makes that class of bug unrepresentable — but this step is a
structural move, not a bug fix. Do not re-price it as remediation.

**Why here:** it is the largest decidable chunk of `beadRunOne`, and it is already contiguous. The
ten decisions occupy one unbroken block, and **nothing between the top of the function and the
worker-selection fallback acquires a resource.** The seam is real and it is clean.

**"Decidable" is the property, not "pure".** Three of the ten do I/O and must not be dressed up as
pure functions:

- **Parent commit** reads `<repo>/.harmonik/branching.yaml` and forks up to two `git rev-parse`
  calls. It is also *time-varying*: the target branch ref moves as sibling runs merge, so the same
  inputs give different answers minute to minute.
- **Lands-on** reads the same file again.
- **Model and effort** read two process environment variables at call time, deliberately, so an
  operator can retune them without a daemon restart.

What they share is that they are all settled *before anything is acquired*. That is the invariant
worth building on.

**The one structural gain: `resolveBranching` runs twice per run.** Once inside `resolveParentCommit`
(which discards `LandsOn`), then again solely to recover `LandsOn`. The code comment admits the
duplication exists to avoid widening a return type. Both calls stat and may read the same file, and
they can disagree if it changes between them. Collapse them into one call that returns what both
callers need. **The trap:** the first call failing is fatal and reopens the bead; the second failing
is non-fatal and silently skips the whole protect-branch check. Collapsing them naively changes
behaviour. This is the real correctness risk of the step — larger than the transcription risk.

**Risk:** low, provided the precedence tables are transcribed exhaustively. The precedence walks are
subtle (`hk-pkugu`: the model default must be resolved against the harness that will actually be
selected, or a Pi run asks the Pi provider for a Claude model). Write them as tables and test them
as tables.

**Two boundaries this step must not cross:**

- **Placement is not a plan output.** `SelectWorker` decides and reserves inside one mutex hold, on
  purpose. Splitting it re-opens the race it was written to close. The plan carries placement
  *intent* — local-only, worker target — and stops there.
- **The run-level model is not the last word for DOT.** The cascade recomputes an effective harness
  per node and corrects the model against it. Collapsing that into one run-level answer re-opens
  `hk-pkugu` one level down, on the node axis. Leave the cascade alone.

**Deliberately out of scope: the refusal-reporting split.** Seven refuse-before-launch returns call
`ReopenBead` directly and emit no `run_failed`. Five ride `failRun` and do. The split is not
principled — the four earliest refusals have `failRun` in scope and choose not to use it. Unifying
them would *add* events operators do not expect today, so it is a decision, not a cleanup. Record
it; do not fold it into a move.

**Found while mapping, recorded not chased:**

- **A pure check sits behind two acquisitions.** The socket path-length check is a `len()` against a
  platform constant and depends only on the project directory, yet it refuses the run *after* the
  worker slot is reserved and *after* a tunnel port is allocated. It exists because `ssh -N -R` never
  validates its local forward destination, so a too-long path lets the tunnel come up, the readiness
  gate false-green, and every hook connection get swallowed. All true, and none of it needs a worker.
  Folded into this step as its own commit.
- **The bead body is parsed three times per run** — once for the target repo, then once inside each
  of the two `resolveBranching` calls. Three parses, three different fields consumed.
- **The terminal diagnostic's model and harness are read from two different places 1,150 lines
  apart.** The model comes from the resolver. The harness is read back off the *launcher*, not from
  the resolved agent type. Nothing asserts the two agree — which is the exact shape of `hk-pkugu`,
  and it would be invisible in the log. Returning both from the resolver makes the disagreement
  checkable.
- **"Is this run local?" changes value 400 lines in.** The local-slot flag is initialised from the
  caller and then flipped when the fallback turns a local dispatch into a remote one, with a
  compensating decrement and a remote-flag write. Any future claim that placement is "decided once"
  has to reckon with this.

### Step 6 — resource leases (size NOT costed, MEDIUM risk)

C2: worker slot, tunnel, worktree, tmux session, hook session, spawn token. Each an acquire
returning a lease with one idempotent release; composed into a scope closed in reverse order.

**Mapped 2026-07-31. Read [`STEP-6-RESOURCE-LEASES.md`](STEP-6-RESOURCE-LEASES.md) before starting** —
it is the measured map and it corrects this section on four points:

- **There are nine resources, not six, in three different lifetimes.** Three of the six named above
  are two things each. A graph run holds one tunnel and one worktree while taking and releasing one
  hook session and one tmux session *per node*, so "one scope closed in reverse" describes the
  per-run set only. The per-launch set needs a nested scope.
- **Release is not a per-resource boolean.** One condition reaches four places, spelled three
  different ways, and misses two resources that need it. The disposition must be decided once for
  the run, as a value.
- **One of the three CARRY-FORWARD risks is already satisfied, one is half satisfied, one is open.**
  The tunnel readiness gate is already a connect probe — do not regress it. The kill rule holds for
  the local reaper, which targets the process group and polls it, but not for the remote one, which
  signals a bare process id and never re-resolves the pane. The bound on session creation is open:
  the independent-session and crew-session constructors do not have one.
- **~450 lines is not defensible from the map.** Re-cost it after the first two commits.

**Progress, 2026-07-31.** Three of the step's six commits have landed: the two folded-in defects
(the doomed tunnel, the unbounded session constructors) and the types themselves. The types live in
`internal/runlease` — a lease, a scope that closes in reverse and can nest, and one disposition
value per run — fenced by depguard to the standard library and itself, and made normative as
`specs/run-state-machine.md` §4a (RSM-036 … RSM-038). They are DELIBERATELY UNWIRED. What remains is
the migration of each release site onto them, the nested per-launch scope, and the two test holes.
Read `STEP-6-RESOURCE-LEASES.md` §8 before wiring: it records what the types decided, including the
one behaviour change the migration carries.

**Why here:** it depends on step 5 (placement is a plan output) and it is what unblocks step 7 (the
mode boundary needs a complete resource scope to receive).

**Risk:** medium, and concentrated in the disposition rather than in the leases. Getting a skip
predicate's polarity wrong strands a bead in progress with no live session to adopt.

### Step 7 — the mode boundary  ✅ **UNBLOCKED. D1 is answered, and half of it already shipped.**

Rewritten 2026-07-30. The old text said this step was blocked on "one `AgentTurn` primitive, or three
drivers?", and priced it at 6,459 lines across four functions. Both numbers are stale and the question
is settled.

**Done already (2026-07-29):** the launch half. `runAgentLaunch` in `internal/daemon/agentlaunch.go`
is the one launch path, and all three remaining call sites use it. A guard added there cannot fail to
reach a mode.

**What step 7 is now.** Two pieces, in order.

1. **Collapse the post-exit interpretation.** The single-mode tail and `dispatchDotAgenticNode` both
   hand-roll probe HEAD → emit `implementer_phase_complete` → process-exit commit fallback → decide
   what no-commit means. They disagree in detail, and the guard table in §2b lists exactly how. This
   is the same shape of work the launch collapse was, on a smaller surface.
2. **Make single-shot a graph and delete the single-mode tail.** A one-implementer graph
   (`start → implement → close`) expresses `workflow:single` exactly. Nothing in the graph engine
   stops it. What stops it today is that five capabilities live only in the single-mode tail and have
   no graph equivalent: the independent tmux session and its restart adoption (`hk-mh3qy`, the real
   blocker), the escaped-worktree guard, `noCommitGuardShouldReopen`, the implementer comms presence
   join and leave, and the Pi provider profile on the launch context (`hk-yo9g6`). Port those onto the
   graph node, then the tail is deletable and `core.WorkflowMode` reduces to one
   value. **Do not price this step at "722 deletable lines".** That figure came from `beadRunOne` at
   1,764 lines, Step 5 has since taken 161 lines out of the function, and nobody recorded how many of
   them left the tail. Re-derive it. See the ⚠ in §1c.

> **CORRECTED 2026-07-31 against `1e22c0141`, on four points. Working in
> [`STEP-7-MODE-BOUNDARY.md`](STEP-7-MODE-BOUNDARY.md).**
>
> 1. **All five named capabilities survive checking, and the list is short by thirteen.** Walking the
>    tail line by line found **eighteen** capabilities that live only there. Thirteen must be ported,
>    three are better dropped than ported, and one — `runmerge.SnapshotUntrackedFiles` — is not
>    tail-only at all, because it runs above the mode switch. **Ten of the defects this mapping filed
>    harm the DEFAULT path today**, so most of the port work pays off whether or not the
>    tail is ever deleted. The worst is `hk-v4wer`: the graph decides node success on "did HEAD
>    advance" alone, so a node that commits and then signals failure is recorded SUCCESS and merged.
>    See §3 of the step map for the full list.
> 2. **The price is re-derived: 740 lines, 368 of them code.** Quote **368**, not 740 — comments are
>    46% of the tail. And quote it as "code touched", never as a deletion figure, because eighteen
>    capabilities move rather than go away. The paragraph above is also wrong about why: Step 5
>    removed **none** of the tail. See the ⚠ in §1c.
> 3. **Step 7's honest scope is piece 1 only.** Piece 2 sits on top of three separate pieces of work
>    that are not this step's: decision D2 and an RSM-008 amendment (the two guards are two of the
>    five capabilities, and the spec currently FORBIDS them on the graph path), `hk-co8g8` (the
>    escape guard fires on innocent runs), and `hk-jyh5t` plus the boot orphan sweep (which defeat
>    the independent session twice before it can pay off). Porting the independent session today
>    ports a capability that does not work yet.
> 4. **One more thing must move before `core.WorkflowMode` can collapse.** `emitReviewBypassed` keys
>    on `mode == core.WorkflowModeSingle`, and EM-012a obliges the daemon to audit that bypass.
>    Collapse the enum and the obligation has nothing to fire on. Filed as `hk-pyouo`.

Do **not** count `dot_gate.go` in this step. Its cognition-gate launch is unreachable — no code sets
`daemon.Config.CPRegistry` and no graph the daemon runs declares a `type="gate"` node (see §2b).
Decide separately whether to
wire it or delete it. Migrating it costs real work and buys nothing until one of those two things is
true.

> **CORRECTED 2026-07-31 — the conclusion holds, but the two reasons are not peers.** Leg one carries
> the argument on its own: `daemon.Config.CPRegistry` is never assigned, `daemonGate.LookupGate`
> returns "not
> loaded" on a nil registry with no default and no fallback, and `dispatchDotGateNode` makes that
> lookup its FIRST step, so it returns a structural eval failure before the evaluator switch and
> `executeCognitionGate` is never constructed. Leg two is much weaker than "no graph declares a gate
> node" sounds. **An operator supplies the graph.** `beadRunOne`'s DOT case honours an absolute
> `plan.WorkflowRef` verbatim, and that value arrives from `harmonik run --workflow-ref <path>`. An
> operator can hand the daemon a graph with a gate node today, with no code change and no file in
> this repo. Leg two is a statement about the shipped file set, not about reachability. **Exclude
> `dot_gate.go` on leg one's authority alone.** If anyone ever populates `CPRegistry`, leg two buys
> nothing and the file becomes live work.
>
> **And "exclude it from this step" is not "it is free to delete".** The file is unreachable in its
> cognition half and it is not free-standing. `dispatchDotGateNode` has two non-test callers — a
> `NodeTypeGate` case in `dot_cascade_core.go` and another in `sub_workflow_runner.go` — so the
> PRODUCTION build breaks before any test does. Three test files break as well: one fails to compile
> on three exported seams, one fails to compile and also loses live coverage of the quit-on-gate-file
> paste injection, and one hard-codes the string `"dot_gate.go"` in a launch-site file list and fatals
> at run time. Removal further orphans the daemon's gate-port adapter — the adapter type, its lookup,
> the port accessor, the run-ports field, the `internal/runloop` interface, and the registry field in
> three places — and strips the only non-test caller of three pure predicates in `internal/policy`.
> **Price wiring-or-deleting as a multi-file change, not as a free removal.** Two dead test seams also
> make the gate look exercised and go in the same change — `hk-f1ymu`.

### Step 8 — the terminal spine (LAST, and mostly already done)

`beadRunOne` becomes a phase coordinator: resolve → acquire → execute → classify → feed the machine.
The machine (`internal/runexec` + `RunBridge`) already owns gate → merge → close/reopen.

**Why last:** everything depends on it, and it is the only part of the current code that is already
right. Touching it early means re-doing it.

> **⚠ Operator decision (D2) lands here**: whether the guards move into the machine's `Guarding`
> phase (behaviour change for review-loop and DOT) or the machine's escape slot is deleted.

---

## Steps 9 onward — added 2026-07-30. The step is no longer "the inside of a function"

Step 8 ends with `beadRunOne` a phase coordinator. **That is where the plan used to stop, and the core
is not done there.** Steps 0–8 carve up the inside of two functions. Everything below is about what is
*around* them: the bundle that builds a run, the packages that write the queue, and the line between
the core and everything the charter switched off.

**The §3 ordering rule stops working here, and it should be replaced rather than stretched.** "Carve
out what flows one way and whose absence the compiler can prove" needs a compiler that can prove
something. It cannot prove anything about a struct whose fields are written after it is returned, and
that shape is what steps 9–15 are about. The rule from here is: **make each unit's dependency set
declarable, then declare it.**

All numbers below measured 2026-07-30 on `15bfdc154`. Two that the rest of this section rests on:
`internal/daemon` is **42,924 production lines in 96 files** (re-measured later on 2026-07-30 — it
read 42,438 in 95 files at `15bfdc154` earlier the same day, so the package **grew** across a single
day of this program), and **106 files in `internal/` are over
400 lines and hold 49% of the tree's production code in 14% of its files**.

Steps 9–15 are the core. **Step 16 is not** — it is named because the operator asked for it, and it is
flagged as sequel work rather than quietly promoted into the core set.

### Step 9 — the live pass (~2 days, LOW risk, and it is the oracle steps 10–15 do not otherwise have)

**This is not a carve-out.** It is here because every step below changes boot-time construction, and
nothing that runs today exercises a real boot.

**What the scenario tier already covers, and what it cannot. Be precise about this, because the tier
is stronger than "unit tests" and weaker than "end to end", and both mislabels have been used.**
`internal/daemon/scenario_*` (26 files) and `test/scenario/` (5) boot the production composition root
by calling `daemon.Start` in a goroutine. Real `git` and real `br` run as subprocesses. The *agent* does
not: `Config.HandlerBinary` points at a twin and `Config.BrPath` at a wrapper. So the tier is genuine
in-process coverage of construction against a fake agent, and it should stay.

What it never does: **start the daemon as a process, run a real agent, run the supervisor, or cross a
process boundary.** A defect in the shipped binary, in supervisor revival, or in state that must
survive a restart is therefore invisible to it. Note also that `scenario_happypath_n1_test.go` pins
`WorkflowModeSingle`, a mode selected twice in the entire event log.

**The tmux substrate is the sharpest gap, and the tier says so itself.**

> ⚠ **CORRECTED 2026-07-30. This paragraph used to open "Three of the 32 scenario files set
> `Config.Substrate`", and that reads as partial coverage. It is not partial. The honest number is
> **0 of 31** scenario files exercise real tmux in a default run.** There are 31 scenario files, not
> 32 — 26 named `internal/daemon/scenario_*.go` plus 5 in `test/scenario/`, counting
> `test/scenario/scenario_stub.go` out because it is a build-tag stub with no tests. Three set
> `Config.Substrate` and **none of the three reaches real tmux unattended**: one wraps a fake adapter
> and is skipped unconditionally, one wraps a fake adapter, and the third skips unless an environment
> variable names a reachable remote host. Do not write "3 of 32". It is wrong in both numbers and it
> overstates the coverage.

The other 28 leave `Config.Substrate` nil, and `daemon.Config.Substrate`'s own doc says a nil
substrate falls back to `exec.CommandContext` — no panes. Two of the three wrap `NewTmuxSubstrate`
around a **fake adapter**. The third, `scenario_remote_substrate_t4_claude_test.go`, wraps the real
`tmux.OSAdapter{}` and is the one file in the tier that would touch real tmux — and it calls
`t.Skipf` unless `HARMONIK_T4_WORKER` names a host that answers a reachability probe, so it never
runs by default. Of the two fake-adapter files,
`scenario_launch_liveness_slotleak_hk40c3y_test.go` records the consequence in a comment: *"the
fake-substrate path cannot reach `run_completed` (no hook-bridge socket relay → agent_ready_timeout)."*
`scenario_concurrent_dispatch_vn4_hkukhzu_test.go` is skipped unconditionally for the same reason, and
its skip text names what is missing: *"agent_ready/outcome arrive over the socket, not stdout… That is
real tmux + real socket altitude."* So `tmuxsubstrate.go` (3,023 lines), `pasteinject.go` (2,691) and
the whole hook-bridge relay are untested by this tier **by construction, not by neglect** — and the one
constraint that stopped it, not touching real tmux on the shared box, no longer applies with the daemon
down and a scratch clone available.

**Two named tiers that would cover the rest are empty.** `test/crash/crash_stub.go` and
`test/integration/integration_stub.go` are 7-line build-tag stubs holding **zero test functions**, so
`check-full`'s `-tags=crash` sub-run tests nothing. Crash, SIGKILL-mid-merge and restart recovery have
no home today.

**A live pass does not duplicate this tier. It covers the half the tier cannot reach.**

**The apparatus for a real pass is already built and is not being run.** `scripts/scratch-daemon.sh`
(959 lines) starts a second, fully isolated daemon — its own clone, socket, pidfile, tmux session,
binary and bead ledger — with `init / build / up / status / down / cycle / batch / feedback`. `batch`
submits beads and writes a structured pass/fail artifact. `feedback` turns failures into beads in the
main repo. `scripts/core-loop-matrix.sh` (481 lines) drives a matrix of cells through it and folds the
results. `docs/scratch-daemon-runbook.md` documents all of it. It was used once, on 2026-07-29, for the
run recorded in §0-CORRECTION: six graph nodes, a real commit on the target branch, 2m32s.

**It is stale at its own default.** `SCRATCH_WORKFLOW_MODE` defaults to `review-loop`, and the daemon
no longer offers that mode — the flag help in `cmd/harmonik/main.go` now reads "single, dot", and
`core-loop-matrix.sh` already carries a comment working around the same default. The first thing a live
pass needs is not new code. It is a default that names a mode that exists.

**Why here and not at the end.** Putting the live pass last repeats the mistake `NEXT_STEPS.md` §5.1
documents: an oracle that is built and never run reports nothing, and a tier nobody runs is
indistinguishable from a tier that passes. Steps 10–15 change the composition root, the queue's writers
and the subsystem switches — the parts a unit test reaches least and a real boot reaches first. Run the
pass before them and it is a baseline. Run it after and it is an autopsy.

**What a pass is:** repair the default, run one bead end-to-end on the tip, keep the batch artifact,
then re-run it as the acceptance check at the end of each step below. No new tier, no new harness,
nothing to keep in sync.

**Risk:** low. The isolation is already designed for — the script refuses to target the fleet daemon
and kills only the PID named in the scratch pidfile, and only after confirming that process's command
line contains the scratch path. The real cost is agent tokens per run, which is why this is a per-step
check and not a per-commit one.

**"The daemon is off" means less than it sounds.** Measured today: no daemon process is running and no
launchd agent is loaded. Nothing gates it off in code. Turning it back on is a command, not a project.

### Step 10 — the composition root (~800 lines, MEDIUM risk — a design, not a move)

**Re-measured 2026-08-01 at `95dff0bf5`. The field count held. The line span did not, and the
step's stated precondition is refuted below.**

- `workLoopDeps` holds **81 fields** — unchanged. Method: parse `internal/daemon/workloop.go` with
  `go/ast`, find the `workLoopDeps` `TypeSpec`, sum `len(Field.Names)` over `StructType.Fields.List`.
- The declaration spans **770 lines**, not 743, of a file that is now **3,366** lines, not 3,227.
  Same `go/ast` pass, reading the `TypeSpec` start and end positions.
- `bootState` in `internal/daemon/bootstate.go` holds **22 fields** — unchanged, same method. A
  2026-07-31 handoff claimed 28. That claim is wrong. The struct has 22 named fields and no embedded
  field.
- The four-stage assembly is unchanged: **25 post-construction field writes** in `bootworkloop.go`
  and **3** in `scheduler.go`. Method: `grep -rnE '^\s*deps\.[a-zA-Z]+ *= ' internal/daemon/*.go`
  with `_test.go` files removed.

**Quote the field count, not the span.** The span read 743 on 07-28, 744 on 07-30 and 770 on 08-01.
The field count read 81 on all three days. See the §1a note for the same warning.

`workLoopDeps` holds **81 fields**, and its declaration alone spans **743 of `workloop.go`'s 3,227
lines** — more than a fifth of the file is one type. It is assembled in four stages across
`bootworkloop.go` (`buildWorkLoopDeps`, `seedGovernorDeps`, `injectWorkLoopDeps`,
`startBackgroundLoops`), with **25 post-construction field writes** there and 3 more in `scheduler.go`.
`bootState` (`internal/daemon/bootstate.go`, 22 fields) has the same shape and admits it in its own doc
comment: early phases write the fields, later phases read them, and a missed hand-off surfaces as a
nil-deref at boot.

**Why here:** §3's "What must NOT move" defers this in so many words — *"Replace it when steps 2–6 have
made most of its fields locally owned, not before."* Steps 2–6 are that work. This step is the licensed
successor to that instruction, and the reason it was deferred rather than dropped.

> **⚠ That precondition is not met, and waiting for it is waiting for nothing. Measured 2026-08-01.**
> The completed steps moved **zero** fields off the bundle. Field count by date, each read with the
> same `go/ast` pass over `git show <commit>:internal/daemon/workloop.go`:
>
> | Date | Commit | Fields | Span |
> |---|---|---:|---:|
> | 2026-07-19 | `37651569f` | 82 | 748 |
> | 2026-07-24 | `a479ddff7` | 81 | 743 |
> | 2026-07-27 | `92d81fd60` | 81 | 743 |
> | 2026-07-28 | `8ccc7a03e` | 81 | 743 |
> | 2026-07-29 | `b3d6e8cc3` | 80 | 735 |
> | 2026-07-30 | `89782676d` | 81 | 744 |
> | 2026-07-31 | `8a84ad4ab` | 81 | 770 |
> | 2026-08-01 | `95dff0bf5` | 81 | 770 |
>
> Steps 2 and 5 landed inside that window, and Step 7 landed half of itself. The net movement is one
> field, from 82 to 81, and it went off on 07-24 before either step landed. The dip to 80 on 07-29
> came back the next day.
>
> So "wait for steps 2–6 to make most of the fields locally owned" describes an outcome that has not
> started and that the completed steps do not produce. **Either this step starts on its own terms, or
> it needs a real precondition. It does not have one today.** Re-run the table before acting on it.

**Why it blocks done:** `PRINCIPLES.md` §4 asks for consumer-owned ports. An 81-field bundle threaded
through every run means no unit of the core has a declared dependency set, and validity is temporal —
which boot phase are we in — rather than something the compiler checks. It also blocks §6's "switching
one back on is a one-line change", because a subsystem's handle is a nullable field on a shared bundle
instead of its own composition.

**Risk:** medium. The bundle is captured by a background watchdog closure, so a partial cleanup
produces a bundle that is neither the old thing nor the new one — the §3 warning still holds and this
step is the only sanctioned way to discharge it. Deciding which fields belong to which extracted stage
is a design decision, not a transcription.

### Step 11 — one writer for the queue (~400 lines, MEDIUM risk — a design)

`queue.Item.Status`, `Group.Status` and `Queue.Status` are set by **direct field assignment at 27
production sites, in 9 files, across 5 packages** — `internal/queue` (`state.go`, `rpc.go`,
`resume.go`), `internal/queuewiring/operatorevents.go`, `internal/lifecycle/startup_pl005_qm002.go`,
and `internal/daemon` (`scheduler.go`, `scheduler_reservation.go`,
`perqueuespendmeter_tigaf11.go`). `queue.Persist` is called from 9 production files in 5 packages, 7 of
those sites in `scheduler.go` alone. `queuewiring.QueueStore.LockForMutation` has 18 production call
sites in 6 files. A real state machine exists — `AdvanceGroup` in `internal/queue/state.go` — but it is
one writer among many rather than **the** writer.

> **⚠ Re-measured 2026-08-01 at `95dff0bf5`. Four of the five counts above are wrong, and the file
> list is short by one. Where this note and the paragraph above disagree, this note is the later
> reading.** Method for the assignment sites:
> `grep -rnE --include='*.go' '\.Status[[:space:]]*(=[^=]|\+=)' . --exclude='*_test.go'`, then keep
> only the hits whose target resolves to `queue.Item`, `queue.Group` or `queue.Queue`.
>
> - **30 assignment sites, not 27.** Nine files, and **four** packages, not five.
>   `internal/daemon/scheduler.go` 9, `internal/lifecycle/startup_pl005_qm002.go` 7,
>   `internal/daemon/scheduler_reservation.go` 3, `internal/queue/resume.go` 3,
>   `internal/daemon/perqueuespendmeter_tigaf11.go` 2, **`internal/queue/persistence.go` 2**,
>   `internal/queuewiring/operatorevents.go` 2, `internal/queue/rpc.go` 1,
>   `internal/queue/state.go` 1. One raw grep hit in `scheduler.go` is a comment and is excluded.
> - **The file list above omits `internal/queue/persistence.go`.** It holds two writes, both
>   immediately before a `Persist` call — `CompleteAndUnlink` sets `QueueStatusCompleted` and
>   `CancelQueueOnShutdown` sets `QueueStatusCancelled`. A writer API that does not admit those two
>   sites cannot claim to be the single writer.
> - **`queue.Persist` in `scheduler.go` is 6 sites, not 7.**
>   `grep -c 'queue\.Persist(' internal/daemon/scheduler.go` → 6. The nine-file, five-package figure
>   holds only if `internal/daemon/scenariotest/concurrent_merge.go` counts as production. Strictly
>   excluding that fixture it is 8 files in 4 packages. Counting intra-package unqualified calls too
>   it is 11 files in 6 packages.
> - **`LockForMutation` is 15 call sites, not 18.**
>   `grep -rn '\.LockForMutation()' internal cmd --include='*.go' | grep -v _test |
>   grep -vE ':[[:space:]]*//' | wc -l` → 15. **The comment strip is load-bearing**: without it the
>   count is 16, because `internal/queuewiring/store.go` carries a `// lq := qs.LockForMutation()`
>   usage example. Six files, so the file count holds. Adding the two `LockForMutationView()` callers
>   in `internal/queue/rpc.go` reaches 17, still not 18.
>
> **And there is a whole class of site this step does not name: composite-literal construction.**
> **15 production sites** build an `Item{}`, `Group{}` or `Queue{}` literal with `Status:` set at
> construction — `internal/queue/rpc.go` 6, `cmd/harmonik/run.go` 3, `internal/queue/append.go` 3,
> `cmd/harmonik/run_via_daemon.go` 1, `internal/daemon/crewstart.go` 1,
> `internal/queue/validation.go` 1.
>
> **The exclusion rule is the whole content of this number, so state it or nobody reproduces 15.**
> Sweep candidates with
> `grep -rnE -A6 '(queue\.)?\b(Item|Group|Queue)\{' internal cmd --include='*.go' | grep -v _test |
> grep -E 'Status:'`, which returns 17. Then keep only literals whose type is exactly `queue.Item`,
> `queue.Group` or `queue.Queue`, or the unqualified `Item`, `Group`, `Queue` inside package `queue`.
> **Five look-alike types match the pattern and are not queue values.** `QueueSubmitResponse` in
> `internal/queue/rpc.go`, `wireGroup` in `cmd/harmonik/run_via_daemon.go` — it carries a
> `queue.GroupStatusPending` constant but is a local JSON wire struct — `StateQueue` in
> `internal/daemon/statedisk.go` and `internal/daemon/stategather.go`, `PausedQueue` in
> `internal/daemon/draindetect.go`, and `QueueItemFact`. Those last four read `q.Status` into a
> report type rather than constructing a queue value. Two real sites also set `Status` from a local
> variable rather than a constant, so a constant-matching grep drops them: `newItems[i] = Item{…}` in
> `internal/queue/append.go` and `items[j] = Item{…}` in `internal/queue/rpc.go`.
>
> A further 3 sit in the test fixture
> `internal/daemon/scenariotest/concurrent_merge.go`, which is the difference between 15 and 18.
> Six of the 15 are in `internal/queue/rpc.go`, the submit path a single-writer design has to route
> through. Two of them set `Status` from a local variable rather than a constant, so a
> constant-matching grep misses them. **Any writer API this step lands must admit these sites, or
> the queue gains a second construction-time writer the moment the first assignment site closes.**
>
> **⚠ Say whether `internal/daemon/scenariotest` is in, or nobody reproduces either figure.** It is a
> test-fixture package — its helpers take `*testing.T` and all four files import `testing` — but its
> filenames carry no `_test` suffix, so it passes every `_test.go` filter and counts as production.
> It is the sole difference between 15 and 18 composite-literal sites and between 8 and 9 files for
> `queue.Persist`. It is also a **sub-package** of `internal/daemon`, so the scope flag decides it
> too: `find internal/daemon -maxdepth 1` excludes it and a recursive `find` includes it. That is why
> the Step 15 file count reads 96 and a recursive count of the same tree reads 104 — the eight extra
> files are `scenariotest`, `bootconfig` and `router`. `scripts/runloop-emitter-gate.sh` scans
> recursively on purpose, and says so. State the depth and the fixture decision beside any count over
> this package.

**Why here:** the charter names both halves of this directly. §6's done criteria include "one explicit
state machine with a single writer", and §4 says the queue is the centre and contested effort goes
there. Step 4 landed the reservation transaction; it did not make the store the sole writer.

**Why it is a design:** collapsing writers decides who may pause a queue and when a write must be
durable. That is the same class of question as D3, and it is the shape RA-2 in `NEXT_STEPS.md` is
complaining about from the other end — a queue that enters `paused-by-failure` automatically and that
nothing in the product can leave.

**Risk:** medium. The invariant is held today by a mutex plus convention, so the failure mode of
getting this wrong is a lost status write that still compiles.

### Step 12 — finish the partition (~600 lines, LOW risk — mostly a move)

> **⚠ THE STEP 10 EDGE IS TOO COARSE, NOT ABSENT. Split this step. Verified 2026-08-01 at
> `95dff0bf5`, corrected the same day after review.**
>
> **⚠⚠ An earlier version of this note said "THIS STEP DOES NOT DEPEND ON STEP 10" and "Step 12 can
> start today". That was wrong for two of the nine consumers, and it was wrong in the direction that
> costs most — a false "you may start". It is withdrawn. What follows replaces it.** The error is
> recorded rather than deleted because this document exists to stop exactly this failure, and it
> reproduced the failure inside the fix: the deref table below was gathered correctly and then read
> against the wrong function boundaries.
>
> The edge exists because gating a consumer leaves its field nil, and a nil field consumed by a later
> boot phase is a nil-deref at boot. That reasoning is sound, and **for two of the nine it applies
> exactly as written.**
>
> **Six of the nine are clear of `workLoopDeps` and can be gated on their own.**
> `HandlerPausePolicyGoroutine`, `DaemonSpendMeter`, `PerQueueSpendMeter`,
> `QueueOperatorEventConsumer`, `ReviewGateAnomalyWatcher` and `CatBL2Handler` are local variables.
> Each is constructed, subscribed and dropped inside one function, so gating them is an
> `if enabled { ... }` wrap with no downstream consumer to protect. None appears as a `workLoopDeps`
> field and none is assigned onto a `workLoopDeps` value. Method: read the 81-field declaration, then
> check every `deps.<field> =` write in `internal/daemon` — 25 in `bootworkloop.go`, 3 in
> `scheduler.go`, plus the `newWorkLoopDeps` literal.
>
> **The other three are `bootState` fields, and being off `workLoopDeps` is not the same as being
> clear of it.** The test that matters is not "is it a field of the bundle". It is "does gating it
> force an edit inside a function Step 10 rewrites". Their production deref sites, checked against
> the function spans in `internal/daemon/bootworkloop.go`
> (`buildWorkLoopDeps` 59-95, `seedGovernorDeps` 96-130, `injectWorkLoopDeps` 152-242,
> `startBackgroundLoops` 243-300, `wireStaleWatcherReapSeams` 319-351) and in
> `internal/daemon/bootstate.go` (`wireSpendAndQueueConsumers` 146-215,
> `wireWatchersAndObservers` 216-328):
>
> | Field | Deref site | File | Enclosing function | Step 10 rewrites it? |
> |---|---|---|---|---|
> | `subscribeHub` | `Subscribe(bus)` | `bootstate.go` | `wireSpendAndQueueConsumers` | no — subsumed |
> | `subscribeHub` | `SetCommsCursorStore` | `bootsocket.go` | socket bind path | no |
> | `subscribeHub` | the `Subscribe:` op-table entry | `bootsocket.go` | socket op table | no |
> | `staleWatcher` | `Subscribe()` | `bootstate.go` | `wireWatchersAndObservers` | no — subsumed |
> | `staleWatcher` | `StartWatcher` | `bootstate.go` | `emitStartupEvents` | no |
> | `staleWatcher` | `SetForceReap` | `bootworkloop.go` | `wireStaleWatcherReapSeams` | **takes `deps`** |
> | `staleWatcher` | `SetRunProcessDead` | `bootworkloop.go` | `wireStaleWatcherReapSeams` | **takes `deps`** |
> | `quiesceArbiter` | `Subscribe(bus)` | `bootstate.go` | `wireWatchersAndObservers` | no — subsumed |
> | `quiesceArbiter` | `SetDrain` | `bootsocket.go` | socket bind path | no |
> | `quiesceArbiter` | the `SleepWake:` op-table entry | `bootsocket.go` | socket op table | no |
> | `quiesceArbiter` | `SetScheduleStore` | `bootworkloop.go` | **`injectWorkLoopDeps`** | **YES** |
> | `quiesceArbiter` | `Start` | `bootworkloop.go` | **`startBackgroundLoops`** | **YES** |
>
> **On the three `Subscribe` rows.** Each sits on the line straight after its own constructor, so an
> `if enabled { ... }` wrap around construction subsumes it and no assignment changes. They cost this
> step nothing. They are listed because **an earlier version of this table called itself the complete
> deref list and omitted all three** — the same failure that produced the retraction above, at
> smaller scale. Two of them sit inside `wireWatchersAndObservers`, which this document names as the
> Step 12 and Step 14 collision site, so they are not neutral for lane planning even though they are
> neutral for the Step 10 question. **Treat this table as the sites checked to date, not as a proof of
> completeness.**
>
> - **`QuiesceArbiter` is Step 10 work.** `SetScheduleStore` sits inside `injectWorkLoopDeps`,
>   between the `deps.scheduleWakeC` and `deps.crewHandler` writes. `Start` is the first call in
>   `startBackgroundLoops` — the function opens with `cfg := bs.cfg`, and `Start` is the statement
>   after it. Gating the arbiter off makes the field nil, so both sites need a
>   nil-guard **inside two of the four functions Step 10 rewrites**. Do not gate it in a separate
>   lane.
> - **`StaleWatcher` is Step 10 work.** `wireStaleWatcherReapSeams` takes `deps *workLoopDeps` and
>   hands the watcher a closure that captures it. That is the case §4 of this document already warned
>   about — *"the bundle is captured by a background watchdog closure, so a partial cleanup produces a
>   bundle that is neither the old thing nor the new one"*. The earlier note overrode a standing
>   warning in this same file without addressing it.
> - **`SubscribeHub` is not cleared, only unrefuted.** Its two derefs are in the socket path and
>   neither is in a function Step 10 names, so on today's reading it looks separable. But it was
>   cleared by the same claim that failed for the other two, so **check it on its own before gating
>   it**, rather than inheriting this note's confidence.
>
> The precedent cited below cuts the same way and was misread the first time: `f6408b861` gated the
> bandwidth tuner and **modified `bootworkloop.go` by +30 lines inside `startBackgroundLoops`**. A
> gating change reaching the assembly functions is the normal case, not the exception.
>
> **What is still true, and it is the useful half:** the gating idiom needs no design work. It is
> worked out twice already, and six of the nine can take it now.
>
> **The pattern is already worked out, twice.** `bandwidthTunerEnabled()` in
> `internal/daemon/bootsocket.go` is the single reading of the `bandwidth_tuner` switch, and both the
> producer seam in `wireWatchersAndObservers` and the consumer seam in `startBandwidthTunerIfEnabled`
> read that same predicate. Producer and consumer can never disagree, so "field is nil" and "deref is
> reachable" are mutually exclusive by construction — there is no nil check because none is needed.
> `newCrewIdleReaperIfEnabled` and `newBranchReapWatcherIfEnabled` in the same file show the other
> idiom: return nil when off, and nil-guard at the named site in `startBackgroundLoops`. That second
> idiom is the template for the three `bootState` members. Both idioms are in the tree now.
> **Note where the second idiom puts its nil-guard — inside `startBackgroundLoops`, one of the four
> functions Step 10 rewrites.** The template itself says this class of change reaches the assembly
> functions.
>
> **This step has two constraints, not one.** `QuiesceArbiter` and `StaleWatcher` are Step 10 work,
> per the table above. Separately, the whole step collides with Step 14 inside
> `wireWatchersAndObservers` — see the collision note below.
>
> **One addition to the list of nine, so a reader auditing "everything with no switch" is not
> surprised:** `NotifyStreamConsumer` is a tenth subsystem-ungated consumer at these seams. It is
> conditional on `cfg.NotifyStream != nil`, that is on the `--notify-stream` flag, so calling it
> ungated is arguable. `CatBL2Handler` is likewise conditional on `ProjectDir != "" && BrPath != ""`.
> Of the nine, seven are unconditional. Decide in advance whether the tenth is in scope.

`knownSubsystems` in `internal/projectconfig/subsystems.go` holds **8 names**, and all 8 are wired at
their construction seams. Against that, `bootState.wireSpendAndQueueConsumers` and
`bootState.wireWatchersAndObservers` construct and subscribe **9 bus consumers with no switch at all**:
`HandlerPausePolicyGoroutine`, `DaemonSpendMeter`, `PerQueueSpendMeter`, `QueueOperatorEventConsumer`,
`SubscribeHub`, `StaleWatcher`, `ReviewGateAnomalyWatcher`, `QuiesceArbiter`, `CatBL2Handler`. Comms,
crew, captain, keeper, live-state and subscribe — six of the names the charter puts *outside* the core
— have no switch of their own either. They ride `SubsystemSocketListener`, which is one coarse switch
over eleven things rather than a partition. And **8,678 production lines inside `internal/daemon`**
(20% of the package) are non-core by the charter's own list.

**Why here:** §6 defines done as "every other subsystem is switched off by configuration and absent at
runtime". Nine ungated consumers is nine subsystems constructed whatever the config says, so §4's
composability test fails for all nine today. Until this closes, "done" cannot be demonstrated — only
asserted.

**Why it is mostly a move:** the mechanism is right and cheap. `SubsystemsConfig.Enabled`'s own doc
states the rule correctly — gate at the construction seam, never make the subsystem inert — and the
`bandwidth_tuner` gate is a worked example of gating a pair of seams consistently. The gap is coverage,
not design. The design residue is the handful of consumers that hold queue state
(`PerQueueSpendMeter`, `QueueOperatorEventConsumer`): switching those off changes queue behaviour, so
each needs its own decision.

**Risk:** low, with one trap. `unknownYAMLKey` in `internal/projectconfig` early-returns "no unknown
keys" for any node that is not a mapping, so a YAML alias hides a typo'd key inside the `subsystems:`
block. That hole is inherited, not new, and it is now on the surface this step widens.

**Collision with Step 14, and it is real. Verified 2026-08-01, and this document did not state it.**
`bootState.wireWatchersAndObservers` in `internal/daemon/bootstate.go` carries both surfaces. This
step must wrap `NewStaleWatcher`, `NewReviewGateAnomalyWatcher`, `NewQuiesceArbiter` and
`NewCatBL2Handler` in a switch. Step 14 must rewrite two substrate capability assertions in the same
function — `cfg.Substrate.(substrateWithAdapter)`, which produces the `QuiesceArbiterConfig.Adapter`
argument, and `cfg.Substrate.(substrateDiagnosticHookSetter)`. The `substrateWithAdapter` assertion
sits **inside** the `QuiesceArbiter` construction block, so the statements this step wraps are the
statements Step 14 rewrites. Two lanes running these at once conflict on that hunk, and a careless
merge drops either the switch or the declared capability. Serialize them or give them to one lane.

**A third collision was claimed and it is not real.** A 2026-07-31 handoff put Steps 11 and 12
together in `internal/daemon/perqueuespendmeter_tigaf11.go`. Step 11's half is there:
`pauseQueueByBudget` writes `q.Status = queue.QueueStatusPausedByBudget` and
`unpauseBudgetPausedQueues` writes `q.Status = queue.QueueStatusActive`. Step 12's half is not.
`PerQueueSpendMeter` is one of the nine, but its switch goes where every other switch goes — at the
construction seam in `wireSpendAndQueueConsumers` in `bootstate.go`, with the name in
`internal/projectconfig/subsystems.go`. The precedent commit `f6408b861` gated the bandwidth tuner
without touching `bandwidthtuner.go` at all. The only shared file is `bootstate.go`, and Step 11
writes no queue status there. The one real contact is a sentence in this file's header comment that
paraphrases the transition, which Step 11 will reword as part of its own edit.

### Step 13 — the event payloads the core actually emits (~340 lines, MEDIUM risk — a design, and it carries a live defect)

**Do this part first. It is 40 lines and it is correctness, not structure. Added 2026-07-30, verified
at both sites.** Two production paths append their own envelope to the same `events.jsonl` that every
reader parses. `core.Event` (`internal/core/event.go`) declares `event_id`, `schema_version` and
`type`. Both of these write `event_type` and `emitted_at` instead:

- `internal/queue/cli/cancel.go` — the `queueCancelOperatorEvent` struct, written by
  `emitQueueCancelEvent` on `harmonik queue cancel`.
- `cmd/harmonik/handler.go` — the `handlerResumedEvent` struct, written by `emitHandlerResumedEvent`
  on `harmonik handler resume`.

A line in that shape decodes into `core.Event` with an **empty type and a zero event id**, so it is
invisible to `harmonik subscribe`, to replay, and to the jq fan-out queries the major-issue protocol
depends on. The `handler.go` case is the sharper one: `emitHandlerResumedEvent` builds a
`core.HandlerResumedPayload`, calls `Valid()` on it, refuses to emit when it fails — and then
**discards the typed payload and marshals the untyped struct instead**. The correct payload already
exists at the write site. Give both sites a `core.Event` envelope with a real event id and schema
version, and keep their payloads where they are.

> **⚠ Re-measured 2026-08-01 at `95dff0bf5`. Both counts in the paragraph below are wrong. Where
> this note and that paragraph disagree, this note is the later reading.**
>
> - **`internal/core` registers 178 event types, not 181.** Method:
>   `grep -rhoE '\bmustRegister\("[a-z0-9_.]+"' internal/core/eventreg_hqwn59.go
>   internal/core/eventreg_wkzlc.go internal/core/pertypecompat_hqwn38.go | sort -u | wc -l`. All 178
>   are distinct. The 181 comes from a looser grep that also catches the `RegisterEventTypeAtVersion`
>   call inside `RegisterEventType` itself and two doc-comment lines in `pertypecompat_hqwn38.go`.
> - **Five more types are registered outside `internal/core`**, all in `internal/workers` via
>   `core.RegisterEventType` in `breach.go`, `telemetry.go`, `health.go`, `offline.go` and
>   `tunnelfailed.go`. So the program-wide total is **183**. Say which number you mean.
> - **The "29 bare `map[string]…` payload sites" is a grep artifact, and the real figure is 7.** The
>   29 reproduces exactly with
>   `grep -rEn 'json\.Marshal\(map\[string\]' --include='*.go' internal cmd | grep -v _test.go`.
>   **27 of the 29 are false positives.** 11 in `internal/hookrelay/hookrelay.go` build the
>   hook-relay wire-protocol body, which the daemon re-types into
>   `core.AgentRateLimitStatusPayload`, `core.AgentReadyPayload` and
>   `handler.ExportedOutcomeEmittedPayload` before anything reaches the bus. 14 across
>   `cmd/harmonik` are JSON-RPC request bodies for the unix socket. One is a codex JSON-RPC params
>   map. One is `internal/testhelpers/jsonlfixture.go`, a fixture builder that survives the
>   `_test.go` filter only because of its filename. The same grep also **misses five of the seven
>   real sites**, because they bind the map to a variable or wrap it in a helper. The two sets
>   overlap in only two members.
> - **The seven genuine untyped event payloads**, with the emitting symbol:
>
>   | Symbol | File | Event type |
>   |---|---|---|
>   | `(*movementGovernor).onHalt` | `internal/daemon/movementgovernor.go` | `liveness_halt` |
>   | `emitStaleOpenBeadDetected` | `internal/daemon/eagerfill_em063.go` | `stale_open_bead_detected` |
>   | `buildWatcherFailedPayload` | `internal/handlercontract/watcher_hc011.go` | `agent_failed` — reached from six `*Watcher` call sites |
>   | `stepIdleRestartTick` | `internal/keeper/step.go` | `session_keeper_idle_crew` |
>   | `appendDecisionRequired` | `internal/sentinel/trip_ev043b.go` | `decision_required` |
>   | `appendDecisionAcknowledged` | `internal/sentinel/trip_ev043b.go` | `decision_acknowledged` |
>   | `appendLegitimateHaltAck` | `internal/sentinel/trip_ev043b.go` | `decision_acknowledged` |
>
>   The three `trip_ev043b.go` sites append straight to `events.jsonl` through the local
>   `appendEventLine`. They never reach the bus.
> - **Five of the seven shadow a payload type that already exists** — `AgentFailedPayload`,
>   `SessionKeeperIdleCrewPayload`, `DecisionRequiredPayload`, `DecisionAcknowledgedPayload`. Only
>   `liveness_halt` and `stale_open_bead_detected` have no registry entry at all. That split is the
>   useful one: five are a one-line swap to the struct sitting beside them, two need a type first.
> - **A 2026-07-31 handoff put this at 6.** It is 7. The missing one is `stepIdleRestartTick`, which
>   hides behind `mustMarshalPayload(...)` and so matches no `json.Marshal(map[string]` grep.

`internal/core` registers **181 event types** with typed payloads, and `daemon.startWithHooks` refuses
to boot if `scanRegisteredPayloadsForSecretFields` finds a secret in one. But **25 `…Payload` structs
live outside `internal/core`**, four of them in `workloop.go` — `workloopRunStartedPayload`,
`workloopRunCompletedPayload`, `beadClosedPayload`, `epicCompletedPayload` — and those, not the
registered types, are what the run path marshals. A further **29 sites marshal a bare `map[string]…`**
as a payload with no type at all.

The divergence is not cosmetic. `core.RunStartedPayload` requires `workflow_id` and
`workflow_version` and has a `Valid()`. `workloopRunStartedPayload` omits both and adds `queue_id`,
`queue_group_index`, `worker_name`, `worker_os` and `workflow_mode`. `run_started` is nonetheless
registered to `&RunStartedPayload{}`, and `internal/replay/runcheckers.go` type-switches on it — so
replay decodes the daemon's real wire bytes into a type that zeroes the fields it expects and drops the
fields that are there. `internal/daemon/runinflightreconcile_hkr73qr.go` reads the same events back
through the shadow struct, so two readers of one event disagree by construction.

**Why here:** this is §4's "consolidate by default" in its purest form — two definitions of one wire
format, both compiling, already drifted. And it blocks §6's "tests that fail when behavior breaks": a
payload change breaks a consumer with nothing red in between. `PRINCIPLES.md` §8 wants record→replay to
be the substrate, and replay is currently decoding into the wrong shape.

**Why it is a design:** the fix is a choice. Either amend the spec'd payload to the shape actually
emitted, or make the run path emit the spec'd shape — which needs a `workflow_id` the single-mode path
does not have. Either way it is a named spec amendment.

**Risk:** medium, and note that the replay mis-decode is a live correctness defect, not only a
structural one. **Recorded, not chased** — it is fixed by this step or not at all.

### Step 14 — declare substrate capability instead of asking for it (~250 lines, MEDIUM risk — a design)

`handler.Substrate` has **one** method. Around it, `internal/daemon/tmuxsubstrate.go` declares **16
capability interfaces** — `substrateWithAdapter`, `substrateWithSessionName`, `substrateWithKeepalive`,
`substrateWithSpawnCap`, `substrateWithSpawnCapSetter`, `substrateSpawnReadier`,
`substrateDiagnosticHookSetter`, `paneTargeter`, `paneCaptureAdapter`, `pasteInjecter`,
`sessionCreator`, `sessionEnsurer`, `runnerSwapper`, `crewSessionSpawner`, `crewSessionStopper`,
`runSessionSpawner` — resolved at **21 production type-assertion sites** across eleven files. The event
bus has the same shape at smaller scale: `EventBus` plus four extension interfaces probed at 6 sites.

> **⚠ Re-measured 2026-08-01 at `95dff0bf5`. The 16 interfaces are right. The site count is not.
> Where this note and the paragraph above disagree, this note is the later reading.**
>
> - **32 production type-assertion sites across 12 files, not 21 across eleven.** Method, runnable as
>   written:
>
>   ```
>   grep -rnE '\.\((substrateWithAdapter|substrateWithSessionName|substrateWithKeepalive|substrateWithSpawnCap|substrateWithSpawnCapSetter|substrateSpawnReadier|substrateDiagnosticHookSetter|paneTargeter|paneCaptureAdapter|pasteInjecter|sessionCreator|sessionEnsurer|runnerSwapper|crewSessionSpawner|crewSessionStopper|runSessionSpawner)\)' \
>     internal cmd --include='*.go' | grep -v _test | wc -l
>   ```
>
>   All 32 are comma-ok single-type assertions. There is not one type switch.
> - **The 11 missing sites are inside `internal/daemon/tmuxsubstrate.go` itself**, and 32 − 11 = 21
>   with 12 − 1 = 11 files, so the earlier count reached 21 by dropping the declaring file. Those 11
>   are the substrate interrogating its own `adapter` field, its own returned handles and its own
>   parameters: `s.adapter.(sessionEnsurer)` twice, `adapter.(sessionEnsurer)` once on a local
>   parameter, `remoteAdapter.(sessionEnsurer)` once, `s.adapter.(sessionCreator)` twice,
>   `sess.(paneTargeter)` three times, plus `p.inner.adapter.(runnerSwapper)` and
>   `p.pasteAdapter().(paneCaptureAdapter)`. **These are the sharpest evidence for this step, not a
>   rounding error.** The implementation cannot name its own capabilities either.
> - Per file: `tmuxsubstrate.go` 11, `crewstart.go` 5, `workloop.go` 3, `bootreconcile.go` 2,
>   `bootsocket.go` 2, `bootstate.go` 2, `bootworkloop.go` 2, and one each in `daemon.go`,
>   `dot_cascade_core.go`, `dot_gate.go`, `pasteinject.go`, `scheduler.go`. All 12 are in package
>   `daemon`. The interfaces are unexported, so nothing outside can hold one.
> - **The event-bus figure is 5 sites across 2 files, not 6.** The four extension interfaces are
>   `RunDrainer`, `CommsMessageEmitter`, `CommsPresenceEmitter` and `TypedEmitter`, all declared in
>   `internal/eventbus/eventbus.go`. `internal/daemon/commshandler_nbrmf.go` holds 3 and
>   `internal/daemon/bootstate.go` holds 2. **`RunDrainer` has zero production probes.** Three test
>   files in `internal/eventbus` do assert it — `busimpl_test.go`,
>   `busimpl_drainrun_race_test.go` and `busimpl_drainrun_reentrant_test.go` — so the interface is
>   exercised, just never probed in production. A grep that does not strip `//` lines also picks up
>   the usage example in each interface's own doc comment in `eventbus.go`, which adds four phantoms
>   and inflates the total to 9.
> - **The 16 interfaces are unchanged.** All 16 named above are still declared in
>   `tmuxsubstrate.go`, and it declares no others. `crewKeeperEventBus` in
>   `internal/daemon/crewstart.go` is a separate narrow interface and is not part of this set.
>
> **Collision with Step 10, and this document did not state it.**
> `bootState.injectWorkLoopDeps` in `internal/daemon/bootworkloop.go` holds both surfaces in the
> **same `if` block**. Step 10 rewrites that function's `workLoopDeps` field writes. This step
> rewrites `cfg.Substrate.(substrateSpawnReadier)` → `prober.ProbeSpawnReady(ctx)`, whose only
> purpose is to produce `deps.spawnSubstrateReadyCh`. The assertion opens the block and the field
> write closes it, with a goroutine launch between them — near, but not one statement. One lane
> deletes or relocates the field write the other lane is converting to a declared capability.
> **The second site in this file is `wireStaleWatcherReapSeams`**, which carries a
> `cfg.Substrate.(substrateWithAdapter)` assertion and which this document names nowhere else.
> `buildWorkLoopDeps` is **not** a site — it mentions neither `Substrate` nor any assertion. A lane
> told to serialize on `buildWorkLoopDeps` looks in the wrong place and misses a real site.
> Serialize Steps 10 and 14, or give them to one lane. This step also collides with Step 12 inside
> `wireWatchersAndObservers` — see the note under Step 12.

**Read the contrast, or this gets mis-applied.** The 13 one-method `Emit` interfaces re-declared across
`eventbus`, `lifecycle`, `handlercontract`, `queue`, `brcli` and `daemon` are **not** a tangle — that is
`PRINCIPLES.md` §4 working as designed, and `runloop.EmitterPort` is the documented idiom. The
difference is direction. A consumer narrowing a dependency is the principle. A consumer interrogating
an implementation to find out what it can do is the defect.

**Why here:** the core set names "harness registry + one substrate". A capability that is discovered
rather than declared cannot be switched off honestly — a missing capability fails as a silent
`ok == false` branch, which is the charter §3 prohibition on faking a degraded capability, applied to
tmux. It is also what makes a second substrate expensive: a new implementation has to satisfy 16
undeclared contracts to behave like the first.

**Risk:** medium. Every one of the 21 sites has a fallback branch today, and some of those fallbacks
are the only thing keeping a non-tmux path alive.

### Step 15 — split `internal/daemon`, and retire the freeze gates in the same change (~large, LOW risk for the move, and it is the last one)

**42,924 production lines, 96 non-test files, 193 test files** (re-measured later on 2026-07-30). The
production figures read 42,438 in 95 files at `15bfdc154` earlier the same day. The 183 test files
figure is older still, from 2026-07-29. Note the direction: this package is the one the program
exists to shrink, and it grew. It also has a fan-out of **52 internal packages**.

> **The 96 is a `-maxdepth 1` count, so it excludes the three sub-packages.** A recursive count of
> the same tree reads 104. The eight extra files are `bootconfig`, `router` and `scenariotest`, and
> `scenariotest` is a test fixture that no `_test.go` filter catches. See the caveat under Step 11.
> This step moves the sub-packages too, so cost it against 104, not 96.
It holds the scheduler, the run driver, the tmux substrate, the paste-inject watchdogs, the DOT
cascade, the boot composition root — *and* comms, crew, dashboard, decisions, subscribe, quiesce,
handler-pause, spend metering and the schedule tick.

**Why last:** the package boundary is what steps 10 and 12 produce, not a prerequisite for them.
Splitting first means guessing the boundary and moving the files twice. `internal/runloop` (2,412
lines, depguard-fenced against importing `daemon`) is the proof the split works — it stopped at the
port surface because nothing had yet made the fields locally owned.

**Why it blocks done:** §4's "segment, then stitch — not entangled-but-documented-as-separate, which is
the current state." Everything in this package can reach everything else's unexported identifiers,
which is the single fact that makes steps 10, 12 and 14 possible in the first place.

**Risk:** low by then, and high if attempted early. That asymmetry is the whole reason it is last.

**⚠ The "LOW risk — a move" price left out the gate scripts, and they are not free. Added
2026-07-30, counted by hand.** The CI gate scripts encode the current shape of `internal/daemon` as
literal symbol names and file globs, so the split breaks them. **Sixteen scripts break, not
fourteen.** The count:

- **14 grep ratchets** over `internal/daemon` symbols. Thirteen are named `*-freeze-gate.sh`
  (`crewrun`, `harnessclaude`, `harnesscodex`, `harnesspi`, `projectconfig`, `queuewiring`,
  `readywait`, `runlaunch`, `runloop`, `runmerge`, `transport`, `workersbootwire`,
  `workloop-scheduler`). The fourteenth is `runloop-emitter-gate.sh`, which does not carry the
  `freeze-gate` name and is the same mechanism — it names `internal/daemon` 40 times, more than any
  of the thirteen.
- **2 mutation harnesses** that copy named `internal/daemon/*.go` files into a fixture tree and patch
  them by hardcoded path: `readywait-freeze-gate-test.sh` and `runloop-emitter-gate-test.sh`. These
  break on a file move even if the symbol survives. `readywait-freeze-gate-test.sh` already patches
  `internal/daemon/reviewloop.go`, a file deleted on 2026-07-28, which is what this coupling looks
  like when it rots.

Reproduce the set with `ls scripts/*gate*.sh` and `grep -c 'internal/daemon' scripts/<name>.sh`.
**`scenario-gate.sh` is NOT in the count.** All six of its `internal/daemon` references are inside
`#` comment lines, so the split makes its comments lie but does not break it. Fix the comments, do
not rewrite the script.

**Two of these gates change how an earlier step must be executed, and this document did not say so.**
Both read 2026-08-01 at `95dff0bf5`. Both are green today (`bash scripts/<name>.sh`, exit 0).

- **`scripts/workloop-scheduler-freeze-gate.sh` sets a FLOOR, not a ceiling.**
  `RUNWORKLOOP_MIN_CODE_LINES=200`, and check (5) fails when `runWorkLoop` in
  `internal/daemon/scheduler.go` falls **below** 200 code lines, with blank and comment lines not
  counted. The floor exists to stop the loop body moving back into `workloop.go` behind a
  pass-through. It has the side effect that **hollowing out `runWorkLoop` far enough turns this gate
  red even when the change is correct.** `runWorkLoop` measures 739 code lines today
  (`awk '/^func runWorkLoop/,/^}/' internal/daemon/scheduler.go | grep -vcE '^\s*(//|$)'`). The
  script's own comment records 796 on 2026-07-29. So there is room to shed about three quarters of
  it before the gate fires. Any step that shrinks the scheduler loop past that point must move the
  floor in the same commit and say why. The script says this itself in its failure message. Do not
  discover it at the end of a step.
- **`scripts/runloop-emitter-gate.sh` budgets an unlisted non-test file in `internal/daemon` at zero
  — but zero of ONE thing, not zero in general.** `budget_for` returns `exact 0` for any path not in
  its two tables, and the scan is recursive over `find internal/daemon -type f -name '*.go' !
  -name '*_test.go'`, so sub-packages are covered too. What it counts is
  `FIELD_RE='deps\.bus'`: raw bus-field reads that should have gone through
  `(*workLoopDeps).emitterPort()`. The loop does `[ "$got" -eq 0 ] && continue`, so **a new
  production file in `internal/daemon` trips this gate only if it reads `deps.bus` directly.** A new
  file that emits through `EmitterPort`, or that does not emit at all, passes. A 2026-07-31 handoff
  stated this as "any new production file there trips it". That is too strong, and believing it
  would make an author avoid adding a file for no reason.

**Why the greps exist, and why they cannot simply be deleted.** `.golangci.yml` is 1,166 lines and
**1,016 of them — 87% — are the depguard block** (lines 146 to 1161). That block repeats the same
caveat in **five** places: depguard checks DIRECT imports only. The greps are the compensation for
that hole. So retiring them is not a deletion. It is a trade: each retired gate needs the depguard
rules to get stronger first, or the boundary it defended goes unguarded. Once the package boundary
is real, most of the 14 ratchets become one depguard rule per new package, which is a boundary a
linter can deny rather than a boundary a grep can approximate. Keep the gates that defend something
a linter cannot state. **Land this with the split, in the same change** — a split that leaves 16 red
gates behind is a split nobody can merge.

### Step 16 — the keeper — SEQUEL WORK, and it is separable from crew

**The operator asked for the keeper to be pulled into the program, and suspected it could not come
without the crew system. The second half is not true.** Measured, not argued:

- `internal/keeper` imports exactly five internal packages: `core`, `substrate`, `presence`,
  `dashboard`, `digest`. It does **not** import crew. `.golangci.yml` already allow-lists those five
  and denies `internal/daemon`.
- `internal/crew` imports only `internal/core`. `internal/crewrun` imports `crew`, `handler`, `queue`.
  Neither imports keeper.
- The one edge that touches crew is transitive and accidental:
  `internal/keeper/dashboardnag.go` calls `digest.LoadDashboardGateConfig`, and `internal/digest`
  separately calls `crew.List` from an unrelated file. `LoadDashboardGateConfig`'s own file imports
  nothing but the standard library and a YAML package. Move that one reader to a leaf and the edge is
  gone — which is the same work `NEXT_STEPS.md` RA-6 already decided on for other reasons.
- `specs/session-keeper.md` contains the word "crew" **zero** times.
- Only four daemon files reach into keeper at all, and every call is read-only or a name resolver:
  `crewstart.go` (`LiveKeeperPresent`), `quiesce.go` (`ResolveTmuxTarget`, `HarmonikSessionName`),
  `statedisk.go` and `stategather.go` (`ReadCtxFile`, `ReadSessionIDFile`). The crew one is already
  behind an injectable seam.

**What actually couples keeper and crew is a file protocol, not code:** the markers under
`.harmonik/keeper/` (`.managed`, the flock `.lock`, `.ctx`, `.sid`, `.idle`, `.dispatching`,
`.hold.<sid>`) plus the tmux session and window names. Freeze that set and the two can be worked
independently, in either order.

**What the workstream would have to include:** `internal/keeper` (8,163 production lines, 18 files),
its CLI (`keeper_cmd.go`, `keeper_enable_doctor_cmd.go`, `resolve_keeper_config.go` — about 3,300
lines, and where the staged config assembly actually happens), and the four embedded shell hooks (412
lines) that write the gauge the Go watcher only reads. The named tangles are `WatcherConfig` (**63
fields**, mutated in place by `applyDefaults`), `CyclerConfig` (**56 fields**, about 20 of them
function-pointer seams that duplicate the `ports.go` interfaces — two parallel seam systems over the
same behaviour), and `(*Watcher).Run` at **531 lines** inside an otherwise well-decomposed file.
`internal/keepertwin` and `internal/keepertest` bind to the pure reactor in `step.go` and are the
safety net that makes this affordable.

**Why it is marked sequel work and not folded into the core.** `CHARTER.md` §3 puts the keeper outside
the core set by operator decision, and §6 says nothing else is required to declare the core done.
Scheduling it before step 15 would be adding to the core set, which §3 says needs a reason. **The
evidence above is the reason it is now cheap and safe to schedule — it is not a reason to schedule it
early.** If the operator wants it moved ahead of step 15, that is a charter change, and it should be
made as one.

**One thing worth taking from the keeper before then, in the other direction.** `step.go` / `cycle.go` /
`shell.go` / `ports.go` are a working functional-core-and-shell split, and four files in
`internal/daemon` and `internal/runloop` already carry comments naming `internal/keeper/ports.go` as
the idiom they mirror. `PRINCIPLES.md` §9 says to find the subsystem that already embodies the target
and make the rest of the tree look like it. **That subsystem is the keeper.** Steps 10 and 14 should
read it before designing anything.

### Steps 19 and 27a — two more, and why the numbers jump

**The gap in the numbering is deliberate.** A planning pass on 2026-07-30 proposed sixteen more
steps, numbered 17 to 32. A review found the set overstated and most of it not yet ready to be a
step. Two of the sixteen were measured, small, and carry a real behavior fix, so they land here as
steps and keep the numbers they were proposed under. The rest are recorded as candidates in
"Candidate work beyond Step 16" below, under those same numbers. **A missing number in this section
is a candidate, not a lost step.**

### Step 19 — one terminal predicate — ✅ **LANDED, with one site still open.** Confirmed 2026-08-01.

> **Both halves shipped, and this document said the whole step was outstanding. One site is still
> open, so do not read this step as fully discharged.** Verified at `95dff0bf5`.
>
> - **The predicate exists.** `func (s CoarseStatus) IsTerminal() bool` is in
>   `internal/core/coarsestatus.go` and returns `Closed || Tombstone`. Landed in `8a84ad4ab`
>   ("refactor: centralize coarse terminal status", 2026-07-31), which also converted
>   `internal/daemon/scheduler.go`, `internal/lifecycle/activerun_em031a.go` and
>   `internal/lifecycle/startup_pl005_qm002.go`. Method:
>   `grep -rn 'IsTerminal()' internal cmd` with `_test.go` removed, plus
>   `git merge-base --is-ancestor 8a84ad4ab HEAD`.
> - **The behavior fix shipped too.** `maybeEmitEpicCompleted` in `internal/daemon/workloop.go` now
>   tests `!e.EndpointStatus.IsTerminal()`, so a tombstoned child no longer suppresses
>   `epic_completed` for its parent. Landed in `f7fbc7649` ("daemon: a tombstoned child no longer
>   stops the epic it belongs to", 2026-08-01), also an ancestor of HEAD.
> - **A seventh site now routes through the predicate as well** — `internal/daemon/stalewatch.go`.
> - **⚠ One site is still open, and it is a real drift hazard. Bead
>   `hk-terminal-status-literals-aynz5` holds it.**
>   `terminalStatuses := []string{"closed", "tombstone"}` in
>   `internal/lifecycle/activerun_em031a.go` is the argument list for
>   `querier.ListBeadsByStatus(ctx, string)`, so it is an enumeration and not a predicate, and
>   `IsTerminal()` cannot replace it as written. **That makes it a harder fix, not a finished one.**
>   `IsTerminal()` hardcodes `Closed || Tombstone` in its own body and this branch scan hardcodes the
>   same two names in another package, so the terminal set now has two copies and no shared source.
>   Add a third terminal status and the predicate learns it while the branch scan silently keeps
>   excluding the wrong set — a wrong exclusion set at runtime, with nothing red at compile time. The
>   bead asks for an accessor such as `core.TerminalCoarseStatuses()` with the predicate and the
>   enumeration both derived from one list. **An earlier reading of this file called the site
>   correctly left alone. That was too generous and it is withdrawn here.**
> - **On the four-outside / two-inside split:** the table below already had it right — four sites in
>   `internal/lifecycle` and two in `internal/daemon`. The "five agree, one does not" line above the
>   table is about which test each site applies, not about which package it lives in. A 2026-07-31
>   handoff read it as a location claim and reported it as an error. It was not one.

`core.CoarseStatus` has no `IsTerminal()` method, so **six production sites re-derive the test.
Five agree that terminal means `closed` or `tombstone`. One does not.**

| Site | Test |
|---|---|
| `internal/lifecycle/activerun_em031a.go` `isTerminalBeadStatus` | `Closed \|\| Tombstone` |
| `internal/lifecycle/activerun_em031a.go` — `terminalStatuses := []string{"closed", "tombstone"}` | same test, written as string literals |
| `internal/lifecycle/startup_pl005_qm002.go` `isDispLedgerClosed` | `Closed \|\| Tombstone` |
| `internal/lifecycle/startup_pl005_qm002.go` `isLedgerClosed` | `Closed \|\| Tombstone` |
| `internal/daemon/scheduler.go`, the BI-013c terminal path | `Closed \|\| Tombstone` |
| `internal/daemon/workloop.go` `maybeEmitEpicCompleted` | **`Closed` only** |

**The one that disagrees carries the cost.** `maybeEmitEpicCompleted` walks the parent's
`parent-child` edges and returns early on the first child whose `EndpointStatus != CoarseStatusClosed`.
A tombstoned child is not closed, so it never satisfies that test, and it **permanently suppresses
`epic_completed` for its parent**. That event is the captain's re-tasking signal, so one tombstoned
child silently stops a lane.

Add `IsTerminal()` to `core.CoarseStatus` and route all six sites through it. **Depends on** nothing.
Do it inside Step 11's window — it is queue-adjacent and it touches `scheduler.go`, which Step 11
also touches. **How you know it worked:** a test that tombstones one child of a two-child epic and
asserts that `epic_completed` still fires.

### Step 27a — one `daemon.Config` constructor with a required-field check (~120 lines, MEDIUM risk)

> **⚠ Re-checked 2026-08-01 at `95dff0bf5`. Every number in this step reproduces. The framing in the
> first sentence does not.**
>
> - `daemon.Config` still has **40 fields** (`go/ast` over `internal/daemon/daemon.go`).
>   `cmd/harmonik/main.go` still sets **21** and `cmd/harmonik/run.go` still sets **17**.
> - **`harmonik run` still does not boot.** `run.go` sets no `WorkflowModeDefault` anywhere
>   (`grep -n WorkflowModeDefault cmd/harmonik/run.go` returns nothing), it calls
>   `daemon.Start(runCtx, cfg)`, and `resolveBootConfig` in `internal/daemon/daemon.go` calls
>   `bootconfig.ValidateWorkflowMode`, which rejects the empty value by name in
>   `internal/daemon/bootconfig/bootconfig.go`.
> - **So "that is the second composition root" is wrong as written, and this step's own body already
>   says so four paragraphs down.** There is one live composition root, not two. The dead one is a
>   dead verb. That does not retire the step — a constructor with a required-field check is still
>   worth having, and it still has to admit the two out-of-scope assembly sites named below. It does
>   change the argument for it. The reason to do this is not "two roots disagree". It is "one root
>   has 40 fields and no constructor, and the verb beside it has been shipping broken because nothing
>   checks at construction". Brief it that way.
> - **A 2026-07-31 handoff reported the dead-verb finding as new.** It is not. This step recorded it
>   when it was written. Only the opening framing needed correcting.

Step 10 replaces the 81-field `workLoopDeps` and the 22-field `bootState`. It does not name
`daemon.Config`, and that is the second composition root.

`daemon.Config` (`internal/daemon/daemon.go`) has **40 fields**. Two production sites assemble it,
and they disagree about what a daemon needs:

| Site | Field literals |
|---|---:|
| `cmd/harmonik/main.go` | 21 |
| `cmd/harmonik/run.go` | 17 |

Count them with `grep -n 'daemon\.Config{'` and read each literal. **`run.go` omits nine fields that
`main.go` sets**: `DefaultHarness`, `WorkflowModeDefault`, `AgentReadyTimeout`,
`RemoteAgentReadyTimeout`, `KerfPath`, `CodexBinary`, `Workers`, `NoAutoPull` and
`SubscriptionTokenCeiling`. So `harmonik run` asks for a daemon with no default harness, no
workflow-mode default, no agent-ready timeouts and no worker registry. The two timeout fields fall
back to constants by their own doc comments. The rest do not.

**And `harmonik run` does not boot at all.** `run.go` calls `daemon.Start(runCtx, cfg)`, which reaches
`resolveBootConfig` in `internal/daemon/daemon.go`. That function calls
`bootconfig.ValidateWorkflowMode`, which rejects an empty `WorkflowModeDefault` with a named error —
*"WorkflowModeDefault must be set (PL-004a)"*. `run.go` never sets that field anywhere. So the second
composition root is not a degraded daemon. It is a dead verb, and it has been shipping. The boot check
is the good news here: it fails loudly and it names the field. This step's job is to make the other
eight omissions fail the same way at construction instead of one at a time at boot.

Two further assembly sites exist and are out of scope here, because neither is a production boot
path: `internal/scenario/orchdrive.go` (14 fields) and `internal/daemon/scenariotest/concurrent_merge.go`
(12). They are worth knowing about, because a constructor with a required-field check has to admit
them.

Give `daemon.Config` one constructor that names its required fields and fails loudly when one is
absent. **Depends on Step 10** — the design of that constructor is the same design question, and
doing it twice produces two answers. **How you know it worked:** `harmonik run` and `harmonik daemon`
construct the same config, and removing a required field fails at construction rather than at first
use.

### The shape of what is left, in one table

| Step | What it is | Size | Risk | Depends on | Move or design |
|---|---|---|---|---|---|
| 9 | Repair the scratch-daemon default and make a live pass the per-step acceptance check | ~2 days | low | nothing | neither — it is the oracle |
| 10 | Replace the 81-field `workLoopDeps` and the 22-field `bootState` with constructed units | ~800 lines | medium | steps 2–6 | **design** |
| 11 | Make the queue store the only writer of queue status | ~400 lines | medium | step 4 | **design** |
| 12 | Give the nine ungated bus consumers and the six unswitched subsystems their own switches | ~600 lines | low | **split** — six consumers depend on nothing, `QuiesceArbiter` and `StaleWatcher` are step 10 work, `SubscribeHub` unchecked. See Step 12 | move, with 2 decisions |
| 13 | Fix the two hand-rolled `events.jsonl` envelopes, then one definition per event payload | ~340 lines | medium | nothing | fix, then **design** + a spec amendment |
| 14 | Replace 16 substrate capability assertions with a declared contract | ~250 lines | medium | step 10 | **design** |
| 15 | Split `internal/daemon`, and retire the 16 gate scripts the split breaks | large | low for the move | steps 10, 12 | move + a linter trade |
| 16 | The keeper — separable from crew, outside the charter's core set | ~11,500 lines | medium | nothing technical | move + design |
| 19 | One `IsTerminal()` on `core.CoarseStatus`, and stop a tombstoned child suppressing `epic_completed` | — | — | — | ✅ **LANDED** `8a84ad4ab` + `f7fbc7649`, **one site open** — bead `hk-terminal-status-literals-aynz5` |
| 27a | One `daemon.Config` constructor with a required-field check | ~120 lines | medium | step 10 | **design** |

> **⚠ Two rows in this table were corrected 2026-08-01. Read the step entries, not the row.**
> Step 12's edge on Step 10 is too coarse, not absent. **Six of the nine consumers it gates are clear
> and can start. `QuiesceArbiter` and `StaleWatcher` are Step 10 work** — their derefs sit inside
> `injectWorkLoopDeps`, `startBackgroundLoops` and `wireStaleWatcherReapSeams`. `SubscribeHub` looks
> separable and has not been checked on its own. Step 12 also collides with Step 14 inside
> `wireWatchersAndObservers`, and Steps 10 and 14 collide inside `injectWorkLoopDeps`. Neither
> collision was in this table. Step 19 has landed except for one open site.

**Honest read on how much is left:** steps 9–15 are on the order of 2,400 lines of production change
over a 42,000-line package, and three of the seven are designs rather than transcriptions. The line
count is not the cost. **Steps 10, 11, 13 and 14 each need a decision before they can start**, and this
program's record is that it runs out of decisions rather than lines. D1 is the case in point in both
directions: it blocked step 7 from the day the map was written until 2026-07-30, and when it was
finally read against the code the answer turned out to be already built. **Ask whether a decision is
still open before waiting on it.**

### Where the program as scoped actually lands — and what is not costed

**A defensible end state for `internal/daemon` has not been costed. Say that plainly rather than
quoting a target.** What follows is what can be measured today.

**The package is 42,924 production lines in 96 files** (`find internal/daemon -maxdepth 1 -name
'*.go' -not -name '*_test.go'`). That total is re-derivable from the command shown. **The per-group
split below is not, and you should know that before you price anything off it.** It comes from a
file-by-file classification pass whose working artifact never reached the repo, and the numbers
appear nowhere else in `plans/`, `docs/` or `.kerf/`. Redo the classification before treating any
single group as a budget. The four core and core-adjacent survivor groups it names:

| Group | Lines |
|---|---:|
| `run` — scheduler, run driver, graph cascade, launch, run plan, harness select | 14,548 |
| `substrate` — tmux substrate, paste-inject watchdogs, heartbeat, hook relay | 5,947 |
| `boot` — `daemon.go`, bootstate, bootsocket, bootworkloop, bootreconcile, wiring log | 2,862 |
| `merge` — branching, beads merge driver, WAL checkpoint | 961 |
| **survivor total** | **24,318** |

**The nine groups with no step against them hold 18,606 lines — 43% of the package.** They are
`watchdog` (4,713), `reconcile` (3,795), `opsview` (2,583), `pause` (2,173), `socket` (2,055),
`spend` (1,008), `comms` (982), `crew` (719) and `sandbox` (578). **The two line totals add to 42,924
exactly, and the classification's file counts do not add up.** The table above shows only lines, but
the classification also assigned a file count to each of the 14 groups, and those sum to 100 against
a measured 96. Those two facts
cannot both be innocent: four double-counted files would inflate the line sum as well, so either the
line sum is a coincidence or the error is in the file column alone. Nobody has checked which.
**Reconcile it before pricing anything off the per-group counts.** The 43% share is the durable
finding here, and it survives either answer.

**Do not subtract the candidate steps' deletion estimates from 24,318.** Most of what they delete is
not in those four groups, and several of them delete nothing inside `internal/daemon` at all. Traced
by file location:

- The argv-builder and refs-trailer candidate (proposed Step 21) names
  `internal/crewrun/launchspec.go`, `internal/harness/codex/commit.go` and
  `internal/harness/pi/commit.go`. **No file it touches is in `internal/daemon`.**
- The second-merge-engine candidate (proposed Step 24) splits across
  `internal/workspace/mergedispatch_wm018a.go` and `internal/daemon/branching.go`. Only the second is
  in a survivor group, and the three landing functions in it total 94 lines.
- The substrate-coverage candidate (proposed Step 26) **adds** lines to the `substrate` group before
  it removes any.

Taken together the candidate set removes a few hundred lines from the four survivor groups, not two
thousand. **The defensible read is that 24,318 falls to roughly 23,900 in four or five packages.**
That is worth doing and it is not the same claim as halving the package. Anyone who wants a smaller
number has to cost the nine unaddressed groups, and nobody has.

### What must NOT move, and why

- **`workLoopDeps` field-by-field.** Do not "clean up" the 81-field bundle in place. It is assembled
  in four stages across `bootworkloop.go` and captured by a background watchdog closure; a partial
  cleanup produces a bundle that is neither the old thing nor the new one. Replace it when steps 2–6
  have made most of its fields locally owned, not before.
- **`WorkLoopDepsParams` (47 exported fields) and the 20 `export_*_test.go` files.** Do not attempt to
  preserve this fixture. The field count read 48 here. The struct spans lines 36–373 of
  `export_workloopdeps_test.go` with no embedded and no multi-name fields, and it holds 47.

  ⚠ **The 157-file blast radius is stale by 4.6x. Re-measured 2026-07-29: it is 34 test files.** The
  header of `export_workloopdeps_test.go` still says 157, and that number was written before the
  test-mass deletion removed ~225,000 lines. Counted today, 34 of the 183 test files in
  `internal/daemon` reference `WorkLoopDepsParams` or any of the four exported shims. Those five
  symbols are the whole surface, so the number cannot go up. The file-count claim in that header is
  now wrong and should be corrected when someone next edits it.

  **This changes a decision, not just a number.** 157 files was the stated reason a signature change
  was too expensive to contemplate and had to wait for the deletion. The deletion has happened, and
  the cost is now a fifth of what this section priced. Re-price the change before deferring it again.

---

## 3b. Candidate work beyond Step 16 — NOT YET STEPS

**Read this heading literally. Nothing below is scheduled, priced or approved.** On 2026-07-30 a
planning pass proposed extending this program from 16 steps to 32. An adversarial review of that
proposal returned **not fit to merge as written** and found the set overstated as a whole: the
end-state arithmetic ran in the program's favor, several counts did not reproduce, and four items
quietly amend `CHARTER.md`. Two items were measured, small and worth taking, and they are Steps 19
and 27a above. **Everything else is recorded here as a candidate, with what has to be settled before
it can become a step.**

Treat each entry's claim as the proposer's, not as measured fact. Where a number below was
re-measured it says so. **`CHARTER.md` §5 says to re-derive every number before acting on it, and
that rule applies to this section more than to any other part of this file.**

### The four that amend the charter — operator decision required before any of them is a step

`CHARTER.md` defines the core subsystem set and what "done" means, and it outranks a plan on intent.
These four change one or the other. **Two of the four admitted they needed a decision. Two did not,
and that is the more useful signal.** None of them is a step until the operator says so.

| # | Candidate | What it amends | Admitted it? |
|---|---|---|---|
| **27** (beyond 27a) | Split `cmd/harmonik`'s verb switch, and lift the config resolvers into their own package | Adds `cmd/harmonik` to the core set. §3 names the core subsystems and this is not among them | **No** |
| **29** | Amend `specs/` — retire the stale review-loop and cognition-loop requirements | Changes what "the spec is always right" means mid-program, and gates future deletions on a spec sweep | **No** |
| **30** | Make a missing external binary a test failure instead of a skip | Changes §6's "tests that fail when behavior breaks" from a goal into a gate, and turns roughly a hundred silent passes red at once | **No** |
| **32** | Enumerate and decide the unwired packages | §4 defers this by operator decision. Doing it is reversing that deferral | **Yes** |

Candidate 27's claim is worth stating because it is the largest single item in the proposal:
`cmd/harmonik` holds `main.run`, a flat verb switch reported as the worst function in the tree by
cognitive complexity. That is a real finding. It is still not this program's scope until the charter
says so.

### The rest, in one line each

**17 — one worktree teardown.** Claims worktree removal is implemented eight times, that four skip
the `~/.claude.json` trust garbage collection, and that two of those four are the
garbage-collection paths. Points at the filed defect `hk-bfvby`. Cost: about 90 new lines and 150
deleted, low risk. **Settle first:** whether the eight really share one contract, or whether the
promote path's single `--force` is deliberate.

**18 — one git-fact probe.** Claims six `git rev-parse HEAD` wrappers with four different
empty-result contracts, plus four `git merge-base --is-ancestor` wrappers that disagree about
whether an error means "not an ancestor". Carries a separate and sharper claim:
`MainHistoryHasRefsTrailer` hardcodes the branch `main` inside a function that takes a project
directory and never a branch, so a project with `target_branch: integration` reads the wrong branch.
Cost: about 70 lines removed plus four call-site changes, medium risk. **Settle first:** whether the
differing empty-result contracts are drift or requirements. The branch-hardcoding half may be a
defect to file rather than a step to schedule.

**20 — one crew session name, one orphan classifier.** Claims crew tmux sessions are spawned with a
`crew-` infix and looked up without it, so crews report as not alive, and that two functions named
`SweepOrphanTmuxSessions` are called twelve lines apart with only one of them applying an orphan
test. Cost: about 40 new and 50 deleted, medium risk. **Settle first:** this step kills tmux
sessions and getting it wrong reaps a live one. It needs a test that proves the classifier is the
only path to a kill, before the change and not after.

**21 — one argv builder and one refs-trailer writer.** Claims the `claude` command line is built four
ways, that two of the four drop `--model`, and that one of those two is the keeper's dead-pane
self-heal path. Folds in `codex.EnsureRefsTrailer` and `pi.EnsureRefsTrailer`. **Verified location:
every file it names is outside `internal/daemon`** — `internal/crewrun/launchspec.go`,
`internal/harness/codex/commit.go`, `internal/harness/pi/commit.go`. Cost: about 125 deleted, low
risk. **Settle first:** the four builders carry two different security postures for the
skip-permissions flag. Collapsing them picks one, and that is a decision.

**22 — one guarded "already merged, close it".** Claims three code paths decide a merge commit exists
for a bead and close it, that one checks seven guards first and two check none, and that one of the
two runs unattended and hourly. Cost: about 60 lines, medium risk. **Settle first:** depends on
candidate 18 for branch resolution. Closing a bead under a live run is the failure this guards
against, so the acceptance test has to exist first.

**23 — time as a port, on a ratchet.** Claims low adoption of the existing `substrate.ClockPort`,
eight hand-rolled poll loops, twelve hand-rolled retry loops, and one constant relation that
guarantees a false alarm — a stall threshold of 180 seconds sitting between a local agent-ready
timeout of 150 and a remote one of 210. **The counts in this area do not agree across three
independent measurements and the step cannot be priced until one command is agreed.** Two figures
did reproduce here: `internal/daemon` makes **119** direct wall-clock calls
(`find internal/daemon -maxdepth 1 -name '*.go' -not -name '*_test.go' -exec grep -hE
'time\.(Now|Since|After|Tick|NewTicker|NewTimer|Sleep|AfterFunc)\(' {} + | wc -l`), and `time.Now`
appears **237** times across `internal/` and `cmd/`
(`grep -rn 'time\.Now(' --include='*.go' internal/ cmd/ | grep -v '_test.go' | wc -l`). **Do not
quote 111 for the daemon figure — it is wrong.** The whole-tree totals proposed as 321 and 322 both
failed to reproduce. The same regex over the same scope gives 356 here. One uncancellable sleep did
verify: `internal/hookrelay/hookrelay.go` `sendToSocket` declares `wallMax = 25 * time.Second` and
retries under a bare `time.Sleep`, so it can block shutdown for 25 seconds. **Settle first:** one
agreed measurement command, then the free half — ban `time.Now` in the packages that are already
clean, so they cannot regress.

**24 — delete the second merge engine, dedupe the live one.** Claims a complete second merge engine
of about 1,048 production lines with zero non-test callers, held alive by tests, and that
`runmerge.RunBranchToTarget` and `runPromotePush` are the same routine written twice with three
drifts that each make promote worse. **This one already knows it needs a decision** —
`CHARTER.md` §4 says unwired code is not deleted by default, so the deletion half belongs to
candidate 32 above. **Settle first:** the charter deferral. The dedupe half may be separable.

**25 — the dead twin halves in the substrate.** Claims the local-versus-remote twin pattern appears
27 times with dead halves still in the tree, and that the remote fingerprint drops a progress signal
so a remote agent editing an untracked file registers as making no progress. Cost: about 145 lines,
low risk. **Settle first:** it touches `pasteinject.go`, so it has to be done inside candidate 26 or
the same 2,691 lines get read twice.

**26 — cover the substrate.** The largest candidate, and the one the review rated highest value:
6,535 production lines of tmux, paste injection and hook relay with no default-run coverage. Claims
about two weeks of one agent, high risk, and that it has a prerequisite inside itself — two
paste-inject watchdog loops that are the same loop with different constants and cannot be tested
without collapsing them first. **Settle first:** it is the only candidate that runs real tmux
unattended on the box. Decide whether that is allowed, and where, before anything else about it.

**27b and 27c — split the verb switch, lift the config resolvers.** Both fall under the charter
question in the table above. Neither is a step until that is answered.

**31 — hermetic tests, so red means something.** Claims the merge gate's `-p=1 -parallel=1` exists to
hide non-hermetic packages, that the Makefile says so in its own comment and files the follow-up as
`hk-d515w`, and that the cause is candidate 23's cause. **Settle first:** candidate 23's measurement
dispute. This one is downstream of it and cannot be priced separately.

### Three proposed changes to Steps 9, 14 and 16 — recorded, not applied

The same pass proposed re-ordering three landed steps. **None is applied here.** Each changes an
order that other work already assumes, so each needs the operator rather than an agent.

- **Step 9 — split it in two and run both halves in parallel.** The claim is that the live pass
  proves a bead ran end to end but cannot say which change broke a run, so it is half an oracle, and
  the other half is candidate 31. The correction to Step 9's substrate coverage **has** been applied
  above, because that was a wrong number and not a re-ordering.
- **Step 14 — move it after candidate 26.** The claim is that all 21 substrate type-assertion sites
  have a fallback branch, that some of those fallbacks are the only thing keeping a non-tmux path
  alive, and that nothing today exercises any of them — so landing Step 14 first is a change no
  oracle can check. **This is the strongest of the three and it depends entirely on whether
  candidate 26 happens at all.**
- **Step 16 — cut the keeper from this program.** The claim is that about half the keeper's
  configuration chain lives in `cmd/harmonik` and `internal/projectconfig`, which Step 16's stated
  scope does not cover, so the step cannot finish its own headline item. Step 16 already marks itself
  sequel work and already says the operator asked for it, so this is a request to reverse an operator
  decision. It is not an agent's to make.

### What the review said to cut outright

The same review listed six things the proposal implied and recommended against. They are recorded so
nobody re-derives them: triaging the twelve agent branches as a workstream (measured at nineteen
distinct commits, three worth having), chasing the spec orphan rate to zero (the rate rewards writing
comments, not fixing specs), enabling `dupl` / `maintidx` / `lll`, a typed-container campaign, and
extracting `internal/core`'s three impure files as its own step rather than folding it into Step 13.
And one standing restatement that grows more tempting as the core gets clean: **do not start the
dataplane.** `CHARTER.md` §3 names it as the next design question after done and says it must not be
started early.

---

## 4. What is actually load-bearing

### 4a. Must survive verbatim — external-world knowledge, cross-referenced to CARRY-FORWARD

**Timing constants.** Every one of these is CARRY-FORWARD fact 18 ("each set by a production
false-kill, several twice"). They currently live as **mutable package `var`s** in
`internal/daemon/pasteinject.go` — which is the single biggest rewrite blocker, because tests mutate
them in place:

`commitPollTimeout` (30 m), `commitHardCeiling` (90 m), `heartbeatStalenessThreshold` (8 m),
`launchHeartbeatTimeout` (180 s), `launchSuppressionCeiling` (12 m), `implementerReseedGrace` (75 s),
`postQuitKillGrace` (60 s), `noChangeKillDelay` (30 s), `briefDeliveredTimeout` (2 m),
`reviewFileTimeout` (10 m), `reviewFileHardCeiling` (60 m), `reviewerHeartbeatActiveGrace` (10 m),
`reviewFilePerKLineBudget` (10 m per 1000 lines), `reviewFilePollInterval` (2 s).

And in `internal/runlaunch/deadlines.go`: `DefaultAgentReadyTimeout` (150 s — raised 30→90→150 per
tmux fact 15), `DefaultRemoteAgentReadyTimeout` (210 s), `KillReapTimeout` (10 s).

**Keep the values. Change the mechanism**: one config struct, constructed once, threaded. That is
exactly what `_plan.md` §6 says about pasteinject's 23 globals, and it is right.

**Verification-by-observation, never by return code** (CARRY-FORWARD patterns 1 & 2). Named
symbols that implement it and must not be simplified back to a return-code check:

| Symbol | What it proves | CARRY-FORWARD fact |
|---|---|---|
| `verifySandboxEngaged` + `srtEngagementCanaryPath` | srt exits 0 without applying under fork saturation | hk-5wdon/hk-tch4t |
| `tunnelpkg.WaitWorkerSocketLive` | endpoint is *connectable*, not merely present | pane/tmux — hk-ege6 |
| `lifecycle.ValidateSocketPathLength` | `ssh -R` never validates the local forward until traffic flows | hk-ta6dg |
| `codex.NoWorkSuspected` (duration floor **AND** no-commit, conjoined) | exit 0 in 3.3–5.2 s with clean worktree = no work | codex fact 13 |
| `codex.EnsureRefsTrailer` / `pi.EnsureRefsTrailer` decision table | sandbox blocks codex's own `git commit`; codex exits 0 anyway | codex fact 3 |
| `noCommitGuardShouldReopen` | asks "did **this bead** land?" via `MainHistoryHasRefsTrailer`, not "did main move?" | hk-4ie1z |
| `runmerge.SnapshotUntrackedFiles` → `CheckMainWorkingTreeDirty` | escape detection needs a pre-run baseline | hk-ooexj |
| `pasteInjectQuitOnCommit` + `noChangeTimeoutCh` | Claude's Stop hook fires on session exit only | pane fact 13 |
| `implementerReseedGrace` late re-seed Enter | the TUI swallows keystrokes while absorbing a paste | pane facts 7, 8 |

**Concurrency and resource invariants.** Each of these is a fix for a live incident:

- `tmuxpkg.SSHRunner{Opts: ["-o","ControlMaster=no","-o","ControlPath=none"]}` — pane fact 19. Set at
  **both** worker-selection sites in `beadRunOne`.
- `agentSpawnSem`, cap 3, remote-only, released on agent_ready — bounds concurrent cold starts.
- `worktreeCreateMu` around the *whole* `git worktree add` retry loop — retrying inside a race does
  not help when the siblings are also racing.
- `mergeq` (`internal/mergeq`) as an explicit FIFO exclusion domain replacing a mutex, with the
  ownership rule that `runWorkLoop` creates-and-cancels but never re-Starts an injected queue
  (double-Start = two owners draining one channel + double `close(done)` panic).
- `cacheReapMu` RWMutex: reaper takes W for the whole `go clean -cache`, Register takes R.
- The **`-pgid` kill + probe-the-group** prescription (tmux fact 7) — "branching on a regime you
  cannot detect is the actual hazard."
- `strandedBeadHasOnDiskRun` returns **`true` on a list error** — race-conservative by design.
- `shutdownDrainTimeout` = 10 s bounded drain — without it, N concurrent beads × ~15 s each hangs
  SIGTERM.
- `closeBeadWithHistoryTrim` trimming `.beads/.br_history` before every close — br fact 9 (25 GiB
  across 15,072 snapshots filled a disk) and br fact 15 (write latency scales with ledger size).
- `queuePreClaimShowAttempts` kept **separate** from the item's persisted `Attempts` — sharing the
  budget means two transient `br` blips leave a bead with one real dispatch attempt.
- `claimSkipInProgressCooldown` (5 min) — suppresses a ~2.5 s spin loop emitting hundreds of
  `bead_claim_skipped` events.
- `d2RemoteAPIKeyRefusal` evaluated **after every spec mutation, at the launch boundary** — the
  2026-05-30 credential-leak incident.

**Structural things that are right and should be copied, not rebuilt:**
- `internal/runexec` — the pure Run state machine. The best code in the run path.
- `internal/orchestrator/select.go` — the pure queue selector with the non-resetting round-robin
  cursor (resetting it starves lexicographically-later queues).
- `internal/mergeq` — the explicit exclusion domain.
- `internal/queue/transaction.go` — named in `_plan.md` §4 as surviving; it is the model for step 4.

### 4b. Scar tissue — do not reproduce

- **Three `ShowBead` calls per queue dispatch** with three different retry semantics. One pre-claim
  read is enough; the label-hydration second read exists only because `br ready --format json` omits
  labels, which is a `br` adapter concern.
- **Two dispatch paths** (queue and br-ready) with duplicated pause/decision/attempt gates written
  twice, ~110 lines of near-copy. br-ready is a backward-compat fallback for tests.
- ~~**The placeholder-RunID two-persist reservation** (§2 Seam B). It is a bug, not a behaviour.~~
  **Removed from the tree 2026-07-30 in `b029f9ce1`.** `reserveQueueItem` writes status and Run ID
  together through `QueueStore.Transact`, so the placeholder no longer exists. Kept here as scar
  tissue because the shape is what to avoid, not because the code is still there.
- **`workLoopDeps` passed by value** into every run goroutine, with a comment explaining that value
  fields mutated there are silent no-ops. Do not carry the bundle; carry the plan.
- **The nil-means-production-default pattern** on 5 of the 9 func-typed fields
  (`launchSpecBuilder`, `worktreeFactory`, `diskFreeBytesFunc`, `goCacheCleanFunc`,
  `worktreeReclaimFunc`). `buildRunBundles` documents the hazard directly: if a test injects
  `launchSpecBuilder`, harness routing is bypassed and a codex-labelled bead silently runs under the
  Claude builder. Production defaults belong in the constructor, not in `if x == nil` at the use site.
- **`RunPorts.LaunchBuilder`** — the same closure stored twice, described in its own comment as
  replacing "the by-value raw-field smuggle."
- **`SharedHandles` as an explicit non-seam.** Ten concrete pointers and raw sync primitives crossing
  a boundary that documents itself as not being one.
- ~~**`activateFirstPendingGroup` and `beadExplicitlyReopened`** — dead, kept alive by tests~~
  **Both deleted 2026-07-28.** Neither symbol is in the tree. See §1d.
- **`resolveBranching` and `resolveParentCommit` — dead, kept alive by tests. New 2026-07-30, and this
  program made it.** Step 5 moved the last production callers into
  `internal/daemon/workloop_runplan.go`, and the two functions in `internal/daemon/branching.go` now
  have only the shims in `export_branching_test.go`. The scar tissue is that a refactor can strand a
  function without anything going red, so re-derive the caller set before deleting either. Retiring
  them belongs to a later step. See Step 5.
- **6 `//nolint` directives** (was 19, then 10), including the `//nolint:funlen,gocognit,cyclop` on
  `beadRunOne` itself. Per `NEXT_STEPS.md` §2.5 this suppression is why the complexity ratchet has
  never once fired on the largest function in the repo — it was **born over the ceiling at 119
  lines** and is **1,603** today (was 2,289).
- **1,672 lines of comment, 130 bead references** (was 3,103 and 256). Comments explaining what code
  used to be there, what a removed check did, and which ticket caused which line. In a rewrite this
  is the commit log's job. The counts halved because the review-loop deletion took the annotations
  with the code, not because anyone pruned the prose.

---

## 5. The other rot

Ranked. "Rot" = size × churn × longest-function × distinct responsibilities × mutable globals.
`workloop.go` is rank 0 and excluded.

**LOC re-measured 2026-07-30 with `wc -l`. Rank 1 is gone and three other sizes moved.** Rank 1 no
longer exists: `internal/daemon/reviewloop.go` was deleted on 2026-07-28. Rank 2's numbers moved when
its helpers split out and the launch step left it. ⚠ The commit counts and the longest-function figures
in rows 4–8 were NOT re-derived — `wc -l` does not produce them. Read those two columns as of their
2026-07-28 measurement date, not today.

| # | File | LOC | Commits | Longest function | Mutable globals | External world |
|---|---|---:|---:|---|---:|---|
| ~~1~~ | ~~`internal/daemon/reviewloop.go`~~ | ~~2,194~~ | ~~116~~ | ~~`runReviewLoop` **1,586**~~ | ~~1~~ | **DELETED 2026-07-28 (`3cec5afd7`)** |
| 2 | `internal/daemon/dot_cascade_core.go` | 1,653 at HEAD (was 1,989) | 123† | `driveDotWorkflow` **997** (+`dispatchDotAgenticNode` **543**, was 882 before the launch collapse) | 0 | tmux, claude, codex, git, ssh, fs, bus |
| 3 | `internal/daemon/pasteinject.go` | 2,691 | 56 | `pasteInjectQuitOnCommit` ~575 | **22** (was 23) | tmux, ssh, raw `exec.Command` (pgrep/ps/git), git, fs, bus |
| 4 | `internal/daemon/tmuxsubstrate.go` | 3,023 | 67 | `spawnWindowVia` 194 (76 funcs) | 1 | tmux (385 refs), ssh, `syscall.Kill` |
| 5 | `internal/keeper/watcher.go` | 2,110 | 78 | `(*Watcher).Run` 531 | 1 | fs, tmux, `exec.Command`, events.jsonl |
| 6 | `internal/eventbus/busimpl.go` | 1,647 | — | max 118 | — | fs (JSONL), 7 `go func` fan-out sites |
| 7 | `cmd/harmonik/main.go` | 1,491 (was 1,517) | 128 | `run` 1,298 | 0 | ~50 verbs, all delegated |
| 8 | `internal/daemon/stalewatch.go` | 1,189 | 15 | `(*StaleWatcher).checkRun` 347 | 6 | git rev-parse, process liveness, bus |

† `dot_cascade_core.go` shows 2 direct commits because it was recently split out of `dot_cascade.go`;
`--follow` gives 123.

Runners-up: `internal/daemon/dot_gate.go` (749 at HEAD, was 888; `executeCognitionGate` 234, was 367
— and it cannot execute in production, see §2b),
`cmd/harmonik/comms.go` (2,212, `runCommsRecvFollowIO` 300, six hand-rolled socket dial sites),
`cmd/harmonik/run.go` (928, was 942; `runBeadSubcommandIO` 701).

The `dot_cascade_core.go` longest-function numbers were both wrong when written, not merely decayed.
Measure them with `grep -n '^func ' internal/daemon/dot_cascade_core.go`, which shows the file holds
exactly two top-level functions. The "two functions are 94% of the file" conclusion below survives
the correction — the measured share is 93%.

### Genuinely part of the rotten center (ranks 1–3)

**Revised 2026-07-30. The rewrite unit is now two files, not three.** This paragraph used to read: "#1
`reviewloop.go` and #2 `dot_cascade_core.go` are the same disease as `workloop.go`, not neighbours of
it … These three files must be rewritten as one unit or not at all." `reviewloop.go` is deleted, so the
sentence cannot stand as written and the argument below applies to `dot_cascade_core.go` alone. Its
conclusion survives, narrowed to two files: **`workloop.go` and `dot_cascade_core.go` are one unit.**
The launch collapse already treated them that way and proved the point — a change made in
`agentlaunch.go` reaches both.

**`dot_cascade_core.go` is the same disease as `workloop.go`, not a neighbour of it.** It has the
worst concentration in the repo — **two functions are 93% of the file**, with no intermediate
decomposition at all. `driveDotWorkflow` at 997 lines is the second-largest function in the repo
after `beadRunOne`, and it is structurally *the same code* as `beadRunOne`'s single-mode body,
re-implemented. **These two files must be rewritten as one unit or not at all** — rewriting
`workloop.go` alone leaves half of the duplicated dispatch loop in place and guarantees the drift
continues.

The retired third file is the evidence for that claim, not a counter-example. Deleting
`reviewloop.go` removed 3,834 production lines and needed no replacement, because the graph walker
was already the general case. That is what a duplicate looks like when it is finally collapsed.

**#3 `pasteinject.go` is the hardest to move**, despite being smaller. Its 22 mutable package
globals (23 when first counted) are the empirical budgets from CARRY-FORWARD fact 18, every one of them a test seam mutated
in place. `_plan.md` §6 already has the right answer (split into `paneinject` / `agentwatchdog` /
`paneprobe`, replace the globals with a config struct, drive from fakes). The three raw
`exec.Command` calls that still bypass `CommandRunner` (`pgrep`, `ps`, `git`) are the bug class that
already cost 5 commits and are **still open**.

### Merely large, not rotten

- **#4 `tmuxsubstrate.go`** — biggest daemon file after workloop, but 76 functions and a 194-line
  max. Six cohesive things stapled together (resizable semaphore, spawn-cap admission, bounded window
  spawn, three session-naming schemes, `perRunSubstrate`, kill-with-grace local+ssh). Rot is at the
  *file* level; the seams are visible. Split it, don't rewrite it.
- **#5 `keeper/watcher.go`** — one 531-line function inside an otherwise well-decomposed file
  (33 other functions, all small). Fix the one function.
- **#6 `eventbus/busimpl.go`** — evenly decomposed, max 118 lines, one responsibility. The audit's
  concern (goroutine-per-delivery, no bounded pool) is a *contract* defect, not a structural one.
- **#7 `cmd/harmonik/main.go`** — 1,298-line flat verb switch, zero state, zero globals, hottest
  file in the repo after workloop because every new verb lands there. Cheap to split; not
  architecturally rotten. The 2026-07-24 audit's adversarial pass reached the same conclusion and
  downgraded it. **Agreed — do not put this on the rewrite list.**
- **`internal/daemon/daemon.go`** — 175 commits (2nd-highest churn in the repo) but only ~292 code
  lines (73% comment). It is the composition root; its churn is structural, not decay.
- Also checked and cleared: `internal/core/eventtype.go` (1,463 LOC, 237 code lines, one 3-line
  function — a documented const table), `internal/codexwire/codexwire.go` (1,543 LOC, 8 commits, max
  func 51), `internal/projectconfig/projectconfig.go` (2,144 LOC, flat parse-and-validate),
  `internal/queue/rpc.go` (1,662 LOC, max func 187).

---

## 6. Reconciliation with the 2026-07-24 code-health audit

The audit's *process* is dead. Its *evidence* holds up well.

### Corroborated — independently reached the same conclusion

The size column is re-measured 2026-07-30. Sizes that moved are shown as `then → now`.

| Audit claim | My reading | Verdict |
|---|---|---|
| Run graph is 105/105 P0, the top hotspot | Same | **Corroborated** |
| `runWorkLoop` ~1,667 lines, cyclomatic 266, cognitive 888 | 1,670 → **1,281**, and it moved to `internal/daemon/scheduler.go` | **Corroborated when written** |
| `beadRunOne` ~2,288, cyclomatic 217, cognitive 396 | 2,289 → **1,603** | **Corroborated when written** |
| ~~`runReviewLoop` ~1,582, cyclomatic 136~~ | **Gone.** `3cec5afd7` deleted the driver | **Moot** |
| DOT drivers ~883–1,001 | 882 / 1,002 → **543 / 997** | **Corroborated when written** |
| `workLoopDeps` is a service locator "assembled in stages; validity is temporal not compiler-enforced" | Four-stage assembly across `bootworkloop.go`; ~25 of 81 fields set post-construction. Field count re-checked at 81. The 25 is **not re-verified** | **Corroborated, and worse than stated** |
| "`runPorts()` deliberately returns required fields nil and a later assembly step fills them" | Confirmed — and the *final* step is inside `beadRunOne` itself, where it assigns `rp.Worktree` from `worktreePort(wtFactory)` | **Corroborated, and worse than stated** |
| Its proposed phase decomposition: Resolve → Prepare → Launch/Execute → Finalize → Terminal (`BR-00`…`BR-04`) | Independently arrived at the same four sub-seams (C1–C4) | **Strongly corroborated** — two independent passes, same seams |
| Its outer-loop decomposition: maintenance → dispatch-permission → dispatch-source → reservation → spawn (`WL-02A`…`WL-04`) | Same as my steps 2, 3, 4 | **Strongly corroborated** |
| Queue dispatch "persisted `dispatched` without a Run ID, treated durable-write failures as nonfatal, patched the Run ID in a second nonfatal persist, then claimed the Bead" | Was confirmed verbatim. **`b029f9ce1` fixed it on 2026-07-30**: `reserveQueueItem` in `internal/daemon/scheduler_reservation.go` now sets status and Run ID in one `QueueStore.Transact` write, and a failed write aborts the dispatch | **Corroborated, and now FIXED** |
| `tmuxsubstrate.go` P1: large, many lock domains, but splittable | Same — merely large | **Corroborated** |
| CLI router (`main.run`) downgraded: "less cognitively coupled than its raw cyclomatic number suggests" | Same | **Corroborated** |

### Disagreements — trust the current reading

1. **`workLoopDeps` field count: audit says 86, actual is 81.** Five fields were removed (including
   the `lastCoordinatorReap`/`lastDiskCheck`/`diskLow` lift to `loopMaintenanceState`). Minor, but
   it means the bundle *did* shrink slightly and the audit's number should not be quoted forward.

2. **`RunEnv` field count: audit says 30, actual is 31.** Trivial drift; noted only because the
   audit used it as a baseline metric that later cards ("meet the exact `ARCH-01` metric target")
   were meant to be scored against.

3. **The dot_cascade atom.** The handoff records the audit estimating this atom at ~1,300/~1,600 LOC.
   The real indivisible atom was **1,989** (`dot_cascade_core.go`), of which 1,884 lines were two
   functions. The audit's *per-function* numbers (883/1,001) were accurate at the time — whoever
   aggregated them into an "atom" estimate under-counted by 20–35%. **Any sizing derived from that
   estimate is wrong.**
   Re-measured 2026-07-30: the file is **1,653** lines and the two functions are 1,540 of them
   (`driveDotWorkflow` 997, `dispatchDotAgenticNode` 543). Treat **1,653** as the floor now. The
   ratio did not improve — it is still 93% of the file in two functions.

4. **The audit's framing is extraction-in-place, and that framing is now retired.** Cards `WL-02A`
   through `WL-04` and `BR-01` through `BR-04` all say "extract the X region of `workloop.go`" under
   an exclusive file lease, with a "sole `dispatch_spine` writer" serialization constraint. The
   2026-07-27 decision is that decomposition-in-place cannot save this code. **The seam *locations*
   in those cards remain valid and are the best independent corroboration available. The execution
   model — leases, worktrees, coordinator-owned index, `sol_xhigh`/`terra_high` staffing — is dead.**

5. **The audit relied on characterization harnesses (`WL-01`, `RL-01`) as the fidelity oracle.**
   Those were to be built from bead-named test files, most of which are now being deleted. The
   rewrite's oracle is the `-tags=scenario` tier, not a characterization harness. This is a real
   change of method, not a refinement.

### Invalidated since the audit

- **`internal/daemon/dot_cascade.go` no longer exists.** It was split into `dot_cascade_core.go`
  (1,653 today) + `dot_cascade_helpers.go` (1,018 today). Every audit reference to `dot_cascade.go`
  needs re-pointing, and the split did **not** reduce the atom — it moved helpers out and left the
  two giant functions intact.
- **`internal/daemon/reviewloop.go` no longer exists.** `3cec5afd7` retired review-loop mode on
  2026-07-28 and removed 3,834 production lines: the driver, `internal/daemon/launchspecbuild.go`,
  `internal/runloop/reviewcycle/`, and `internal/runloop/continuity/`. Every audit card scoped to
  `RL-*` is moot.
- **Audit finding #4 (supervisor `Stop` double-close panic) is FIXED.** `Supervisor.Stop` now uses
  `s.stopOnce.Do(func() { close(s.stopCh) })`. Do not carry this forward as an open defect.
- **`RunEnv`/`RunPorts`/`SharedHandles` field counts have drifted**, so the audit's "baseline and
  reject growth in run-bundle fields" gate (recommendation 12) has no valid baseline.

### Flagged by the audit, still unactioned

- ~~**Reservation transaction** (torn dispatched/RunID write, non-fatal persists) — open. This is my
  step 4.~~ **DONE 2026-07-30 in `b029f9ce1`.** `reserveQueueItem` routes the dispatch through
  `QueueStore.Transact`, so status and Run ID land in one durable write, and a failed write abandons
  the dispatch. Decision D3 is answered by that commit, not still pending.
- **`internal/lifecycle/branchtip_em024a.go` `WritePersistedTip` uses plain `os.WriteFile`** — no
  temp+rename, no fsync. The rewind detector therefore fails *open* after a crash or disk-full,
  which is precisely when it is needed. Confirmed still present.
- **`internal/schedule/store.go` `SuspendAllForSleep` / `writeSuspendedSet`** — disables jobs
  durably, releases the lock, then separately `os.WriteFile`s the restore set. A crash between the
  two loses the pre-sleep enabled set with no way to reconstruct it. Confirmed still present (the
  file has an atomic `os.Rename` path elsewhere at line 651 — it just isn't used here).
- **`internal/workspace/mergedispatch_wm018a.go` `DetectSquashMergeConflict`** — runs a real
  `git merge --squash` followed by unconditional `git reset --hard HEAD`, with no deadline, no
  cleanliness precondition, and no ownership lock. **Zero production callers, tests only.** Confirmed
  still present. This is a latent data-loss API sitting in the tree; it belongs in the dead-code
  deletion pass, not on a fix list.
- **`internal/eventbus/busimpl.go`** — 1,647 LOC, 7 `go func` fan-out sites, no bounded worker pool.
  Unactioned.
- **Remote process-tree cancellation** (audit #2): killing the local SSH client leaves the worker-side
  `/bin/sh -lc` and its build/test tree running. Nothing in the current plan addresses this, and
  CARRY-FORWARD tmux fact 11 ("a remote worker's pane PID does not exist in the local process table")
  is the same boundary. **A rewrite of `workloop.go` does not fix it** — it lives in
  `internal/handler/session.go` and `internal/lifecycle/tmux/runner.go`.
- **The blanket `_test\.go$` exclusion on `funlen`/`cyclop`/`gocognit`** — ~488k lines of test code
  with no complexity ceiling at all. `NEXT_STEPS.md` §2.5 repeats this. Unactioned.

---

## 7. Decisions the operator must make

| # | Decision | Blocks | Why it cannot be delegated |
|---|---|---|---|
| **D1** | ✅ **ANSWERED 2026-07-30 — collapse the duplication (option A).** See the note below for what option A turned out to mean. | Nothing. Step 7 is unblocked | — |
| **D2** | **Do the escape check and no-commit guard belong to the Run machine or to the caller?** | Rewrite step 8 | Moving them into the machine *changes behaviour* — DOT runs that pass today would start being guarded, and DOT is the default. Deleting `ActCheckEscape` admits the machine's `Guarding` phase is decorative. |
| **D3** | **Is a failed queue-reservation persist fatal to the dispatch?** | Rewrite step 4 | Correct answer is yes (no claim, no launch). But under disk pressure it converts silent inconsistency into visible dispatch stall. |
| **D4** | ✅ **MOOT 2026-07-30.** It asked whether `reviewloop.go` and `dot_cascade_core.go` were in scope with `workloop.go`. `reviewloop.go` was deleted on 2026-07-28, and the launch step of the other two was collapsed into `agentlaunch.go` on 2026-07-29, so the question answered itself by events. The surviving half of it — "are the run driver and the graph cascade one unit?" — is yes, and step 7 above now states it directly. | Nothing | — |
| **D5** | **Does the scenario tier become merge-blocking before the rewrite starts?** | Everything | **Re-briefed 2026-07-30. It is no longer one line of YAML.** That line landed (`1ee9154e8`, `7f1028316`) and the tier now reports its own failures. What is left is 8 deterministic scenario failures plus a skip that reads as a pass, and `.github/workflows/scenario.yml` warns not to make itself required until those close. The decision is therefore how much test repair to buy before the rewrite starts, not whether to flip a flag. Without an oracle the rewrite has nothing to validate against. See Step 0 item 3. |
| **D6** | **Push a reconciled `main`.** Step 0 item 4. `origin/main` is 27 commits of probe markers plus two fixes already on the tip, and three things key off it — the lint delta in `make check-short`, the one required status check, and every new agent worktree. | The measurement gate for everything | It needs a push to `origin/main`. An agent must not rewrite the branch every merge is judged against. |
| **D7** | **Do the four charter-amending candidates become steps?** They are `cmd/harmonik` beyond Step 27a, the specs pass, the missing-binary-is-a-failure pass, and the unwired enumeration. See §3b. | Nothing today. Each blocks itself | `CHARTER.md` defines the core subsystem set and what done means, and it outranks a plan on intent. Two of the four did not admit they were amending it. |

---

## 8. One-paragraph answer — REWRITTEN 2026-07-30

The remaining work is **not** "design one primitive that all modes call". That primitive exists and is
in production. `runAgentLaunch` (`internal/daemon/agentlaunch.go`, 836 lines) owns spawn, liveness
proof, drive-to-dead-session and exit facts, and a CI gate
(`scripts/readywait-freeze-gate.sh`) pins the invariant mechanically: exactly one
`runloop.DispatchSegment` may be constructed in the tree, and it must be in `agentlaunch.go`. There
are two workflow modes left, not three, and one of them is a rounding error — across the whole live
event log (through 2026-07-22) there are **866 `dot` run starts against 2 `single`**. Measure it with
`jq -r 'select(.type=="run_started") | .payload.workflow_mode' .harmonik/events/events.jsonl | sort |
uniq -c`, which also reports 1,285 run starts with no mode field at all, from the review-loop era.

> ⚠ **CORRECTED 2026-07-30. This sentence used to read "`workflow_mode` reads `dot` 5,890 times, the
> retired `review-loop` 1,635 times, and `single` twice."** Those are **event-occurrence** counts —
> `workflow_mode` rides many event types, so the same run is counted once per event it emits. Set
> beside "2,153 runs" they read as run counts and they are not. The ratio holds and the absolute
> numbers do not. Use run starts.

The remaining work is therefore the graph program:
finish moving the last shape onto the graph and delete the other. Two things stand in the way. First,
"what the exit means" is still written twice — the single-mode tail and `dispatchDotAgenticNode` each
hand-roll probe-HEAD, `implementer_phase_complete`, the process-exit commit fallback and the
no-commit decision, and they disagree in detail. Second, and worse, the drift now runs the wrong way:
four steps live only on the path used twice ever and are missing from the path that carries
everything — the Pi provider profile on the launch context (`hk-yo9g6`), implementer comms presence,
Pi stderr capture, and the independent tmux session that lets a run survive a daemon restart
(`hk-mh3qy`). Close those, express single-shot as a one-implementer graph, delete the tail, and
`workflow_mode` stops being a branch. Two carve-outs. The escaped-worktree check and
`noCommitGuardShouldReopen` are **not** in that list: `specs/run-state-machine.md` RSM-008 says the
DOT path MUST NOT run them, so aligning them is a spec amendment (D2), not a cleanup — and `hk-co8g8`
found the escape check killing innocent runs, so extending it as-is would spread a live bug. And the
cognition gate in `dot_gate.go` is not part of this either: no code sets `daemon.Config.CPRegistry`
and no graph the daemon runs declares a `type="gate"` node, so it cannot execute today. Wire it or
delete it as its own decision.

<details>
<summary>What §8 said before 2026-07-30, and why each claim was wrong.</summary>

- "**four** copies … spread across `workloop.go`, `reviewloop.go`, `dot_cascade_core.go`, and
  `dot_gate.go` — 6,459 lines in four functions." Two errors. `reviewloop.go` was deleted on
  2026-07-28 (`3cec5afd7`). And of the three that remained, the launch step was collapsed into one
  function on 2026-07-29, leaving two live post-exit copies plus one unreachable gate.
- "the sandbox gate in two of the four." Fixed on 2026-07-29 by `30b5cf02c`. There is one gate,
  `sandboxSpawnForRun`, and config is the only switch.
- "the escaped-worktree guard runs in one of the four." **Still true**, and it is the more important
  half of the finding, because the one is not the default.
- "What does not exist, and what no amount of extraction will produce, is a single primitive for
  'launch one agent and find out whether it did the work.'" False from 2026-07-29. The first half of
  that sentence — launch one agent — was built. The second half — find out whether it did the work —
  is the part still duplicated, and it is the honest statement of what is left.

</details>
