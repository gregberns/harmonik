# 1-of-N drift in the daemon run machine — measurement

Base: `e7e74214b` (worktree was at `dc2217527`, BASE_STALE → reset per instruction). No code changed.

> ## ⚠ Read this before using any count below — corrected 2026-07-29 on `0db5dcc28`
>
> **This measurement was taken when there were five agent-launch sites. There are now three.** Sites B and
> C both lived in `reviewloop.go`, which has since been deleted; A, D and E survive unchanged. So every
> "1 of 5" below is really **1 of 3**, and the two review-loop-only findings (the crash-recovery resume
> and the merge-retry classifier) are closed out — see `OPEN-DEFECTS.md` for how each was resolved.
>
> The measurement is left otherwise intact rather than rewritten, because the per-site detail for A, D and
> E is still accurate and expensive to re-derive. Two specific claims that DID go stale with the deletion:
> the fabricated-duration finding (N17) described the review-loop reviewer segment and no longer applies to
> anything in the tree; and §(a)'s driver table lists two drivers that no longer exist.
>
> Re-measuring the three surviving sites on 2026-07-29 found **five further divergences** this document
> does not contain — they are written up in `OPEN-DEFECTS.md`, not backfilled here, to keep this file a
> dated measurement rather than a living one.

---

## What this changes about the plan (2026-07-29 synthesis)

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
the path almost all real work takes. Recorded as a defect, not fixed here.

**The launch-path collapse (§Next step 4) is confirmed as the highest-value move**, and by a wider
margin than the map claimed: it eliminates N1, N2, N3, N6a, N13, N17 and the teardown-ordering
divergence *by construction*, because those are all per-launch-site steps. It does not touch the
mode-driver tails (N4, N5, N7, N8, N9, N11, N12, N14, N15, N16), which need the terminal spine collapsed
as a separate second step, nor the sub-workflow walker (N10), which needs a third.

---

## (a) True driver topology

The "four peers" framing is stale. There are **three mode drivers** and **five agent-dispatch
sites**, plus a sixth nested walker.

`beadRunOne` (`internal/daemon/workloop.go`) resolves `workflowMode` and hits a three-way
`switch`:

| mode | driver | returns via |
|---|---|---|
| `WorkflowModeReviewLoop` | `runReviewLoop` (`reviewloop.go`) | `bridge.Success()` |
| `WorkflowModeDot` | `driveDotWorkflow` (`dot_cascade_core.go`) | `bridge.Success()` |
| default (`Single`) | inline tail of `beadRunOne` | `bridge.Success()` |

The review-loop and DOT cases `return` before the single-mode tail begins, so everything textually
below the switch is path-A-exclusive by construction.

Five `runloop.DispatchSegment` construction sites — the one primitive every agent launch shares:

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

Eliminated by construction (all are per-launch-site steps inside the five `DispatchSegment` blocks):
N1, N2, N3, N6 (harness-selection half), N13, N17, and the `ForceTeardownSession` /
heartbeat-LIFO ordering divergence between D and E. Also the four-way duplication of
`EmitPreExecBeforeLaunch` / `EmitPreExecMessage` / `RunHeartbeatLoop` / `SetAgentReadyCallback`
wiring, and the `SkipReadyHandshake` deliver-fallback that only D performs.

**Not** fixed — these live in the mode-driver tails and the `beadRunOne` switch, not the launch
site, and need the terminal spine collapsed separately: N4 (post-launch persist), N5, N7, N8, N9,
N11, N12, N14, N15, N16, prior claims 2 and 4, and the no-commit-guard variants. N10 needs the
graph walker unified with `driveDotWorkflow`, a third piece of work.
