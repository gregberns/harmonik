# 1-of-N drift in the daemon run machine — measurement

Base: `e7e74214b` (worktree was at `dc2217527`, BASE_STALE → reset per instruction). No code changed.

> ## ⚠⚠ Read this FIRST — corrected again 2026-07-30. There is now ONE launch site.
>
> **The launch-path collapse this document argues for has LANDED.** `internal/daemon/agentlaunch.go`
> holds one function, `runAgentLaunch`, and its own comment reads "runAgentLaunch collapses the three
> into one" and "There is exactly one gate now". All three surviving call sites route through it:
> `internal/daemon/workloop.go` (single mode), `internal/daemon/dot_cascade_core.go` (DOT node
> dispatch), and `internal/daemon/dot_gate.go` (cognition gate). Verify with
> `grep -rn "runAgentLaunch(ctx" --include='*.go' internal/daemon/ | grep -v _test`.
>
> **Three call sites in source, two that can execute — the two counts answer different questions.**
> The third, the cognition gate in `dot_gate.go`, is dead: `daemon.Config.CPRegistry` has zero
> assignments anywhere in the tree and no graph declares a `type="gate"` node. Use three when you ask
> what routes through `runAgentLaunch`. Use two when you ask what runs. See the next banner.
>
> **So every "1 of 5" and "1 of 3" below is now "1 of 1", and the drift class this file measures is
> closed by construction rather than by counting.** Two specific consequences:
>
> - **Finding N2 is RESOLVED.** `d2RemoteAPIKeyRefusal` has exactly one production call site, inside
>   `runAgentLaunch`, so the credential guard now covers every launch including DOT — the path N2
>   says carries essentially all real traffic. Bead `hk-z4cow` predicted this in its own last line.
>   Commit `d39ca9a25` is titled "retract the five-site framing" and is an ancestor of HEAD.
> - **The sandbox gate is resolved the same way.** `sandboxSpawnForRun` also has exactly one call
>   site, inside `runAgentLaunch`, called before any session-id branching. The per-site scope
>   parameter is gone and two source-level tests in `internal/daemon/agentlaunch_scope_test.go` pin
>   it. Bead `hk-j52we` records the operator's decision to make the fix, and the fix is in the tree.
>
> The per-site detail below is kept as a dated measurement of how the drift arose. Do not use it to
> scope work. **Beads carrying the `drift-1ofn` label are largely superseded — check each against
> `runAgentLaunch` before working it.**
>
> ---
>
> ## ⚠ Earlier corrections, kept — 2026-07-30 per-finding scoring, then 2026-07-29 on `0db5dcc28`
>
> *Not superseded. The banner above says the launch class is closed. The banner below says which
> individual findings that closed and which it did not, and it carries the executable-site count.
> Read both.*
>
> **2026-07-30. The launch sites are no longer separate at all, so the "1 of N" framing has expired for
> the launch half of this document.** `runAgentLaunch` in `internal/daemon/agentlaunch.go` landed on
> 2026-07-29 and owns spawn, liveness proof, drive-to-dead-session and exit facts. Sites A, D and E all
> call it. `scripts/readywait-freeze-gate.sh` allows exactly one `runloop.DispatchSegment` in the tree,
> and it must be in that file, so the class cannot silently come back.
>
> Re-verified in the code on 2026-07-30, the collapse closed exactly what §What-this-changes predicted:
> **N1, N2, N3, N6a, N13, N17 and the teardown-ordering divergence.** What it did NOT close is also
> exactly as predicted: **N4, N5, N7, N8, N9, N11, N12, N14, N15, N16 — the mode-driver tails — and N10,
> the sub-workflow walker.** Read those rows as live. Read the launch-site rows as history.
>
> **Correction to the line above — N1 and N6 did NOT close.** A later per-finding re-check on the same
> day walked all seventeen against the tree with a stated grep and scored each one. It found N1 still
> open (`classifyLaunchFailure` still has no consumer) and N6 still open (`dot_cascade_core.go` still
> calls only `codex.EnsureRefsTrailer` for every ProcessExit harness). That table is §(c) "Recount,
> 2026-07-30 — status of all seventeen" and it is the more recent measurement, so it governs. The
> summary above ran ahead of the evidence for those two rows. It holds for N2, N3, N13 and N17.
>
> **Site count is TWO, not three.** Site E, the cognition gate in `dot_gate.go`, cannot execute:
> `daemon.Config.CPRegistry` has zero assignments anywhere in the tree and no graph declares a
> `type="gate"` node. The count on this line has read five, then three. It is two.
>
> *Earlier correction, 2026-07-29 on `0db5dcc28`, kept for provenance:* this measurement was taken when
> there were five agent-launch sites; sites B and C both lived in `reviewloop.go`, which was deleted, so
> every "1 of 5" below was really "1 of 3". The two review-loop-only findings (the crash-recovery resume
> and the merge-retry classifier) are closed out — see `OPEN-DEFECTS.md`. N17 described the review-loop
> reviewer segment and no longer applies to anything. §(a)'s driver table lists drivers that are gone.
> Re-measuring on 2026-07-29 found five further divergences, written up in `OPEN-DEFECTS.md` rather than
> backfilled here.

---

## What this changes about the plan (2026-07-29 synthesis)

> **Outcome recorded 2026-07-30.** The deletion this section warned about went ahead in `3cec5afd7`.
> Of the two things it named as review-loop sole homes, **one was carried across and one was lost**:
>
> - **Merge retry was carried across.** `Retryable: runmerge.IsRetryableReason` now appears in
>   `internal/daemon/workloop.go`. Verify: `grep -rn 'IsRetryableReason' --include='*.go' . | grep -v _test`.
> - **CHB-023 crash-recovery resume was lost.** `persistClaudeSessionID` and
>   `emitClaudeSessionIDPersisted` return **zero hits repo-wide**, tests included. The symbol went out
>   with the file. Whether EM-031 resume was re-homed elsewhere or simply dropped is **unverified** —
>   this sweep only established that these two symbols are gone.
>
> The review-budget ladder also survived: `ChargeReviewLoopFailure` is now called from
> `internal/daemon/workloop.go`, closing N8.

The prior list of four 1-of-N instances was a floor, not a ceiling: **seventeen** are catalogued below.
Three consequences worth reading before acting on §Next.

**1. Deleting `reviewloop.go` removes behaviour that lives nowhere else.** Two items are review-loop
SOLE homes, and both would silently leave the product:

- **CHB-023 crash-recovery resume.** `persistClaudeSessionID` is review-loop-only. Single-mode and DOT
  both capture a Claude session id and drop it. Delete the file and EM-031 resume disappears — including
  from DOT, the default mode.
- **Merge retry.** `Retryable: runmerge.IsRetryableReason` is passed to the terminal spine only by the
  review-loop. DOT and single-mode treat *every* merge failure as terminal, transient ones included.

This is the third instance of the pattern the program has already been bitten by twice (the crew
idle-reap tests and the D2 credential-guard conformance test). **Before any deletion here, ask what the
deleted path is the only home of** — the compiler will not tell you, because the callers still compile.

**2. "Delete `single` mode" (§Next step 3) has a hidden dependency.** Terminal classification
(`handler.MapWaitReturnToTerminalEvent`) has exactly one consumer in the tree, and it is the
**single-mode** path — not the review-loop, as previously recorded. Deleting single mode therefore
deletes the last consumer of the terminal-classification primitive, leaving all remaining paths guessing
by probing whether HEAD moved. Sequence step 3 *after* the launch-path collapse, or port the
classification first.

**3. The credential guard does not cover the default path.** `d2RemoteAPIKeyRefusal` runs on
single-mode only (1 of 5). DOT is the default, so the 2026-05-30 credential-leak gate does not protect
the path almost all real work takes. Recorded as a defect, not fixed here. **CLOSED 2026-07-29:** the
refusal is called once, inside `runAgentLaunch`, so every launch asks it.

**The launch-path collapse (§Next step 4) is confirmed as the highest-value move**, and by a wider
margin than the map claimed: it eliminates N1, N2, N3, N6a, N13, N17 and the teardown-ordering
divergence *by construction*, because those are all per-launch-site steps. It does not touch the
mode-driver tails (N4, N5, N7, N8, N9, N11, N12, N14, N15, N16), which need the terminal spine collapsed
as a separate second step, nor the sub-workflow walker (N10), which needs a third.

> **Outcome, recorded 2026-07-30.** The collapse landed on 2026-07-29 and this prediction held item for
> item. Point 2 above is the one that still needs acting on and has not been: `handler.MapWaitReturnToTerminalEvent`
> still has exactly one production consumer, the single-mode tail of `beadRunOne`. So "delete single
> mode" remains blocked on porting terminal classification onto the graph node, which today decides the
> same question by comparing HEAD before and after. Point 1 is closed both ways — the crash-recovery
> resume was retired with evidence rather than ported, and the merge retry was ported to DOT.

---

## (a) True driver topology

> **Recounted 2026-07-30. Both numbers in this section are now wrong.** There are **two** mode
> drivers, not three, and **one** agent-dispatch site, not five.
>
> `internal/core/workflowmode.go` now declares only `WorkflowModeSingle` and `WorkflowModeDot`, and
> its validity check is `case WorkflowModeSingle, WorkflowModeDot`. **`WorkflowModeReviewLoop` no
> longer exists as a constant.** Stale references to the name survive in comments in
> `internal/core/run.go` and in `internal/core/reviewloopevents_hk7om2q4.go`, which still declares
> review-loop event payloads for a mode that cannot be selected.
>
> The five `DispatchSegment` rows below are all gone. `grep -rn 'DispatchSegment{' --include='*.go' .`
> minus tests returns exactly one hit, in `internal/daemon/agentlaunch.go`, inside `runAgentLaunch`.
>
> File sizes also moved: `dot_cascade_core.go` is **1,653** lines, not the 1,995 quoted below.
> `dot_cascade_helpers.go` is 1,018 and `dot_gate.go` is 749. The new `agentlaunch.go` is 836.

The "four peers" framing is stale. There are **three mode drivers** and **five agent-dispatch
sites**, plus a sixth nested walker.

`beadRunOne` (`internal/daemon/workloop.go`) resolves `workflowMode` and hits a three-way
`switch`:

| mode | driver | returns via |
|---|---|---|
| ~~`WorkflowModeReviewLoop`~~ **constant deleted** | ~~`runReviewLoop` (`reviewloop.go`)~~ **file deleted** | — |
| `WorkflowModeDot` | `driveDotWorkflow` (`dot_cascade_core.go`) | `bridge.Success()` |
| default (`Single`) | inline tail of `beadRunOne` | `bridge.Success()` |

The DOT case `return`s before the single-mode tail begins, so everything textually below the switch
is path-A-exclusive by construction.

Five `runloop.DispatchSegment` construction sites — the one primitive every agent launch shares.
**All five are now one, in `runAgentLaunch`. Table kept to show how the drift arose:**

| id | var | enclosing symbol | file |
|---|---|---|---|
| A | `implSeg` | `beadRunOne` (single-mode tail) | `workloop.go` |
| B | `implSeg` | `runReviewLoop` (implementer half) | `reviewloop.go` |
| C | `revSeg` | `runReviewLoop` (reviewer half) | `reviewloop.go` |
| D | `nodeSeg` | `dispatchDotAgenticNode` | `dot_cascade_core.go` |
| E | `gateSeg` | `executeCognitionGate` (via `dispatchDotGateNode`) | `dot_gate.go` |

E is the site the "four peers" count omits. `dotSubWorkflowRunner.Run` /
`dispatchSubWorkflowExpandedNode` (`sub_workflow_runner.go`) is a sixth *walker* that re-enters
D and E; it is not a sixth launch site but it is a sixth guard surface (see N10).

`internal/daemon/dot_cascade_core.go` is 1,995 lines as the brief says; `dot_cascade_helpers.go`
(1,000+) and `dot_gate.go` are part of the same driver and were not in the brief's file list.

## (b) Step × path matrix

`+` present, `-` absent, `~` partial.

| conceptual step | A single | B RL-impl | C RL-rev | D DOT node | E DOT gate |
|---|---|---|---|---|---|
| worktree setup (`wtPath`, `SnapshotUntrackedFiles`) | shared pre-switch — computed for all, consumed by A only | | | | |
| model/substrate selection | + | + | + | + (per-node override) | + |
| launch-failure sentinel fields set | + | + | + | + | + |
| launch-failure **diagnostic emit** (`EmitSpawnCapBlocked`/`EmitTmuxNewWindowTimeout`) | + | + | ~ (fabricated durations) | + | **-** |
| `classifyLaunchFailure` result consumed | **- (0 of 5)** | - | - | - | - |
| sandbox gate (`sandboxSpawnForRun`/`verifySandboxEngaged`/`sandboxWrapExecArgv`) | + | - | - | + | - |
| D2 remote-credential refusal (`d2RemoteAPIKeyRefusal`) | + | - | - | - | - |
| `SessionIDCaptured` interceptor wired | + | + | - | + | - |
| …captured id **consumed** | - (write-only chan) | + (resume) | n/a | + (spawn proof) | n/a |
| CHB-023 persist (`persistClaudeSessionID`) | - | + | - | - | - |
| never-spawned-reaper disarm (`newCapturedSpawnProof`) | - | - | - | + | - |
| heartbeat loop (`RunHeartbeatLoop`) | + | + | + | + | + |
| `ForceTeardownSession` | ~ (conditional on `useIndepSession`) | + | + | + | + (inverted LIFO vs D) |
| `WaitWithSocketGrace` — outcome kept | + | - | - | - | - |
| `WaitWithSocketGrace` — ExitInfo kept | + | + | **- (`_ = revEI`)** | + (only if `!isReviewer`) | - |
| terminal classification (`MapWaitReturnToTerminalEvent`) | + | - | - | - | - |
| HEAD-moved heuristic instead | + (also) | + | ~ | + | **- (no git evidence at all)** |
| escaped-worktree guard | + | - | - | - | - |
| no-commit guard | + | + | n/a | + | n/a |
| subsumed check (`MainHistoryHasRefsTrailer`) | + | **-** | n/a | + | n/a |
| codex/pi commit backstop (`EnsureRefsTrailer`) | + (pi **and** codex) | **-** | - | ~ (codex only) | - |
| no-work-suspected detector | + | - | - | + | - |
| post-agent-ready hang detector | - | + | - | - | - |
| iteration/traversal cap | n/a | + (`emitIterationCapHit`) | + | + (no event) | inherits |
| no-progress guard | n/a | ~ (fixup-stalled only) | n/a | + (configurable) | n/a |
| `noChangeTimeoutCh` consumed | + | - (passes nil) | n/a | - (passes nil) | n/a |
| spine `Retryable` (merge retry) | - | + | | - | |
| spine `AmendTrailers` | - | + | | + | |
| spine `CarveOut` (already-on-main) | - | - | | + | |
| review-budget charge (`ChargeReviewLoopFailure`) | - | + | | - | |
| orphan-salvage `runTipSHA` | - | - | | + | |
| independent session (`useIndepSession`) | + | - | - | - | - |

## (c) NEW 1-of-N instances

> ### Recount, 2026-07-30 — status of all seventeen
>
> Every finding below was re-checked against the tree with `grep -rn '<symbol>' --include='*.go' .`
> filtered to non-test files. Verdicts:
>
> | # | Status now | Evidence |
> |---|---|---|
> | N1 | **OPEN**, but read it as 0 of 1 | `classifyLaunchFailure` still lives only in `internal/runloop/dispatchsegment.go` and still has no consumer |
> | N2 | **CLOSED** | `d2RemoteAPIKeyRefusal` has one production call site, in `runAgentLaunch` — it now covers every launch |
> | N3 | **CLOSED** | `newCapturedSpawnProof` is now called from `internal/daemon/agentlaunch.go`, not only from `dispatchDotAgenticNode` |
> | N4 | **MOOT — the behaviour is gone, not unified** | `persistClaudeSessionID` returns **zero hits repo-wide**, tests included. See the note in §What this changes. |
> | N5 | **CLOSED** | `shared.MainHistoryHasRefsTrailer` is called from `dot_cascade_core.go`, `workloop.go` (3 sites) and `scheduler.go`. The absent third path was the review-loop, which no longer exists. |
> | N6 | **OPEN — this one survived intact** | `dot_cascade_core.go` still calls only `codex.EnsureRefsTrailer` for every ProcessExit harness, while `workloop.go` branches pi-vs-codex. A Pi node on the default DOT path still gets the codex backstop. |
> | N7 | **CLOSED** | `Retryable: runmerge.IsRetryableReason` is now in `internal/daemon/workloop.go` |
> | N8 | **CLOSED** | `ChargeReviewLoopFailure` is now called from `internal/daemon/workloop.go` |
> | N9 | **OPEN**, now 1 of 2 | `noChangeTimeoutCh` is still created and selected on only in the single-mode tail of `workloop.go` |
> | N10 | **UNVERIFIED** | the sub-workflow walker was not re-checked in this sweep |
> | N11 | **MOOT** | `emitIterationCapHit` returns **zero hits repo-wide** — it went out with `reviewloop.go` |
> | N12 | **OPEN**, now 1 of 2 | `useIndepSession` is still assigned only inside the single-mode tail |
> | N13 | **CLOSED** | `runloop.WaitWithSocketGrace` has exactly one non-test call site, in `agentlaunch.go` |
> | N14 | **OPEN** | `runexec.EvEscapeDetected` still has its `ActEmit` row in `internal/runexec/run.go` and no producer; `workloop.go` still emits the event directly via `emitImplementerEscapedWorktree` |
> | N15 | **OPEN**, now 1 of 2 | `runTipSHA` is still assigned at exactly one place in `workloop.go` |
> | N16 | **UNVERIFIED** | `bridge.Start` ordering was not re-checked |
> | N17 | **MOOT** | it described the review-loop reviewer segment, which no longer exists |
>
> Of the four prior claims in §(d): claim 1 still holds in substance —
> `handler.MapWaitReturnToTerminalEvent` still has exactly one non-test call site, in `workloop.go`.
> Claim 2 still holds — `emitImplementerEscapedWorktree` is still called only from `workloop.go`.
> Claim 3 is **CLOSED**: `sandboxSpawnForRun` and `verifySandboxEngaged` each have one call site,
> both in `runAgentLaunch`. Claim 4 is **MOOT**: `emitNoProgressDetected` returns zero hits
> repo-wide, and only the live `emitDotNoProgressDetected` remains.

**N1 — `classifyLaunchFailure` has ZERO consumers (0 of 5).** `runloop.DispatchSegment`
`classifyLaunchFailure` maps a launch error onto `spawn_cap_blocked` / `tmux_new_window_timeout`;
all five sites set `SpawnCapTimeout` and `TmuxNewWindowTimeout` so the classification is correct
everywhere. `runexec.stepDispatchLaunching` turns it into `ActEmit launchFailedEventType(reason)`.
But `dispatchSegmentRun.emit` switches only on `EventTypeLaunchInitiated` and
`EventTypeAgentReadyTimeout` — both structural classes fall into `default:` and are dropped. Four
of the five sites then hand-roll `errors.Is(lErr, ErrSpawnCapTimeout)` inside their own
`OnLaunchFailed` to emit the same two events; E's `OnLaunchFailed` takes unnamed params and emits
nothing. Purest instance of the pattern in the tree.

**N2 — D2 remote-credential refusal is 1 of 5.** `d2RemoteAPIKeyRefusal` has exactly one call site,
in `beadRunOne`, at the post-build/pre-launch boundary. B, C, D, E build and launch specs with no
credential inspection. Since DOT is the default mode, the 2026-05-30 credential-leak gate does not
cover the default path.

**N3 — the never-spawned-reaper disarm is 1 of 5, and A wires the machinery then throws the answer
away.** `newCapturedSpawnProof` is called only from `dispatchDotAgenticNode`. `beadRunOne`'s
`implIsSessionIDCapturedWL` block wires the identical `NewSessionIDInterceptor` but its callback
body is only `capturedSessionIDCh <- id` into a buffered channel with no reader ("single-mode has
no resume reader"). The hk-47u9z comment in `dot_cascade_core.go` describes exactly the bug this
leaves live on A: for codex/pi, `agentReadySeen` never flips, the launch-stall guard degrades to an
absolute 30-minute wall-clock cap, and a healthy run is killed and mislabelled.

**N4 — CHB-023 checkpoint persist is 1 of 5.** `persistClaudeSessionID` /
`emitClaudeSessionIDPersisted` are called only from `runReviewLoop`. A and D capture a session id
and drop it. Crash-recovery resume (EM-031) is therefore review-loop-only, and the review-loop's
no-commit guard has to carry a `contextCommitSHA` baseline compensation that the other two paths
do not need *because* they never persist.

**N5 — the subsumed check is 2 of 3 modes, and the comment claims the opposite.**
`shared.MainHistoryHasRefsTrailer` runs on A (`noCommitGuardShouldReopen` + the `noChangeTimeoutCh`
branch) and on D. It is absent from `runReviewLoop`. `noCommitGuardShouldReopen`'s doc comment says
"Mirrors the review-loop guard" — the review-loop guard it names does not do this check. Under wave
dispatch a review-loop bead whose work already landed on main false-fails (the hk-4ie1z failure
mode, fixed on A and D only).

**N6 — the codex/pi commit backstop is asymmetric on two axes.** `beadRunOne` branches
`AgentTypePi → pi.EnsureRefsTrailer` else `codex.EnsureRefsTrailer`.
`dispatchDotAgenticNode` calls `codex.EnsureRefsTrailer` for **every** ProcessExit harness — a Pi
node on the default DOT path gets the codex backstop. `runReviewLoop` calls neither, so a codex/pi
implementer in review-loop mode never gets its work committed and trips its own no-commit guard.
`codex.NoWorkSuspected` / `EmitImplementerNoWorkSuspected` follow A+D, not B.

**N7 — spine policy diverges three ways.** `runloop.SpineArgs` fields, per mode:
- `Retryable: runmerge.IsRetryableReason` — **review-loop only** (its sole call site). DOT and
  single-mode treat every merge failure, including transient ones, as terminal.
- `CarveOut` (advisory-RC / already-approved-on-main → close instead of re-queue) — **DOT only**.
  A and B re-dispatch forever on `rebase_dropped_commits`.
- `AmendTrailers` — B and D, not A (A has no reviewer, so plausibly by design).
- `SkipGate` — D only (by design; DOT runs its gate inside the graph).

**N8 — the review-loop retry budget is 1 of 3.** `handles.Budget.ChargeReviewLoopFailure` /
`queue.MaxReviewLoopFailures` are charged only in the review-loop arm of `beadRunOne`'s switch. A
DOT failure never charges the budget, so the "close with needs-attention after N failures" ladder
cannot fire on the default mode.

**N9 — `noChangeTimeoutCh` is 1 of 3.** `pasteInjectQuitOnCommit` is called from A, B and D with
the same signature; B and D pass `nil` for the no-change channel. Only A selects on it (the
`noChange-subsumed` vs `noChange-timeout` discrimination in the terminal switch).

**N10 — the sub-workflow walker has none of the walker guards.** `dotSubWorkflowRunner.Run` /
`dispatchSubWorkflowExpandedNode` re-enter D and E but contain zero occurrences of
`emitNodeDispatchRequested`, `emitNodeDispatchDecided`, `incrementCapIfBounded`, diff-hash
tracking, or the no-progress guard. It hard-codes `isReviewer: false` and `isTerminalSpawn: false`.

**N11 — `iteration_cap_hit` is 1 of 2 walkers, in the other direction.** `emitIterationCapHit` fires
only in `runReviewLoop`. DOT enforces `core.Edge.TraversalCap` via `core.SelectNextEdge` and, on
hit, only builds a summary string — no `iteration_cap_hit` event anywhere in the DOT files.
Symmetric blind spot to the dead `emitNoProgressDetected`.

**N12 — `useIndepSession` is 1 of 3.** Declared before the switch, assigned only inside the
single-mode tail. The shared pre-switch worktree-cleanup defer is guarded by
`!useIndepSession || ctx.Err() == nil`, a condition that can only be false on A. Review-loop and DOT
runs lose their worktree on daemon shutdown and cannot be adopted at next boot.

**N13 — path C throws away its ExitInfo explicitly.** `_, revEI := runloop.WaitWithSocketGrace(...)`
immediately followed by `_ = revEI`. A reviewer that crashes non-zero is indistinguishable from one
that exits clean without writing a verdict. Path E discards both return values (`_, _ =`) and does
no `gitprobe` work at all — it has no evidence about its agent beyond the presence of
`gate-verdict.json`.

**N14 — `runexec.EvEscapeDetected` is a producerless vocabulary entry.** `runexec.stepRunGuarding`
has a full `EvEscapeDetected → ActEmit(EventTypeImplementerEscapedWorktree)` row, but no non-test
code in `internal/` ever feeds that event. A emits the escape event directly via
`emitImplementerEscapedWorktree` and routes the failure through the generic mode-failure edge, so
the machine's own guard row is dead.

**N15 — `runTipSHA` orphan-salvage is 1 of 3.** Declared in `beadRunOne` and read by
`emitRunTerminalEff`, but assigned at exactly one place: the DOT non-success branch. A review-loop
or single-mode run that leaves a stranded commit on the run branch emits `run_failed` with no tip
SHA.

**N16 — `bridge.Start` ordering differs.** A calls `bridge.Start(ctx, workflowMode)` *before*
launch; B and D call it *after* their driver returns, immediately before `WireSpine`. `run_started`
timing is therefore not comparable across modes.

**N17 — path C fabricates the durations it reports.** B and D pass
`Clock.Since(launchedAt)` to `EmitSpawnCapBlocked` / `EmitTmuxNewWindowTimeout`; C passes the
constants `defaultSpawnAcquireTimeout` / `defaultNewWindowTimeout`. Same event, measured on two
paths, invented on the third.

## (d) The four prior claims

1. **HALF WRONG on attribution, right on the count.** `handler.MapWaitReturnToTerminalEvent` has
   exactly one non-test call site — but it is in `beadRunOne`, the **single-mode / one-shot inline**
   path, **not** the review-loop. The review-loop is one of the four paths that throws the
   classification away. It is 1 of 5, not 1 of 4. The "guess by probing whether HEAD moved"
   description is right for B and D; E is worse than described — it does no git probe at all and
   reads only `gate-verdict.json`. Also: `socketOutcome`, the first `WaitWithSocketGrace` return,
   has the same single consumer (A).
2. **CONFIRMED, count off by one.** `runmerge.CheckMainWorkingTreeDirty` +
   `emitImplementerEscapedWorktree` fire only inside `beadRunOne`. 1 of 5. Sharper than stated: the
   baseline `runmerge.SnapshotUntrackedFiles` is taken *before* the switch, so B–E pay for the
   snapshot and never consume it.
3. **CONFIRMED, count off by one.** `sandboxSpawnForRun` / `verifySandboxEngaged` /
   `sandboxWrapExecArgv` appear in `beadRunOne` (A) and `dispatchDotAgenticNode` (D) only. 2 of 5.
   Sub-divergence inside the two: A converts a `verifySandboxEngaged` failure into a bead reopen,
   D returns a bare error.
4. **CONFIRMED.** `emitNoProgressDetected` (`reviewloop.go`) has zero callers, production or test —
   the only truly dead emitter among 46 `emit[A-Z]` declarations in `internal/daemon`.
   `emitDotNoProgressDetected` is live, called once from `driveDotWorkflow`. Deeper than the claim:
   the *guard* is 1-of-N too. DOT has a configurable no-progress guard
   (`graph.NoProgressGuard` → `noProgressGuardOff` / `noProgressGuardCap`,
   `consecutiveNoProgressCount`). The review-loop retains `state.lastDiffHash` whose own comment
   says it is kept "only for the no_progress_detected event payload", and routes its only
   HEAD-advance check into `emitReviewFixupStalled`, which requires a prior REQUEST_CHANGES
   verdict. A review-loop implementer that stalls without one is not detected at all.

Also verified as NOT drift: `reviewer_launched`, `reviewer_verdict`, `implementer_resumed` all have
DOT twins that do fire; `review_fixup_stalled` and `reviewer_budget_exceeded` are single helpers
called from both walkers.

## (e) What "collapse all launch paths into one function" fixes for free

> **This section is now a record of a completed move, not a proposal. Verified 2026-07-30.** The
> collapse landed. Read the prediction against the outcome:
>
> - **Predicted eliminated, and eliminated:** N2, N3, N13 and prior claim 3 (the sandbox gate). All
>   four now have a single call site inside `runAgentLaunch`.
> - **Eliminated, but by deletion rather than by the collapse:** N17, and N4 and N11 from the
>   "not fixed" list. Their symbols went out with `reviewloop.go` in `3cec5afd7`.
> - **Predicted eliminated and NOT eliminated:** **N1**. `classifyLaunchFailure` still has no
>   consumer. Collapsing the sites made the classification uniform; it did not connect it to
>   anything. This is the one row of the prediction that failed, and it is still open work.
> - **Predicted eliminated and NOT eliminated:** **N6**. Only the harness-selection half was in
>   scope. The codex/pi backstop asymmetry lives in the mode-driver tail, and
>   `dot_cascade_core.go` still calls only `codex.EnsureRefsTrailer`.
> - **Correctly predicted to survive:** N5, N7 and N8 were listed as needing a separate terminal-spine
>   step. They closed anyway when review-loop mode was retired. N9, N12, N14, N15 and prior claims
>   1 and 2 survive exactly as predicted, now across two mode tails rather than three.
> - **Still unverified:** N10 and N16 were not re-checked in this sweep.

Eliminated by construction (all are per-launch-site steps inside the five `DispatchSegment` blocks):
N1, N2, N3, N6 (harness-selection half), N13, N17, and the `ForceTeardownSession` /
heartbeat-LIFO ordering divergence between D and E. Also the four-way duplication of
`EmitPreExecBeforeLaunch` / `EmitPreExecMessage` / `RunHeartbeatLoop` / `SetAgentReadyCallback`
wiring, and the `SkipReadyHandshake` deliver-fallback that only D performs.

**Not** fixed — these live in the mode-driver tails and the `beadRunOne` switch, not the launch
site, and need the terminal spine collapsed separately: N4 (post-launch persist), N5, N7, N8, N9,
N11, N12, N14, N15, N16, prior claims 2 and 4, and the no-commit-guard variants. N10 needs the
graph walker unified with `driveDotWorkflow`, a third piece of work.
