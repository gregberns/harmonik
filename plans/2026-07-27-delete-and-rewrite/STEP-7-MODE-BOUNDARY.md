# Step 7 — the mode boundary: the measured map, and what it changes about the step

Measured 2026-07-31 against `1e22c0141`. Three independent mapping passes — one for the post-exit
divergence, one for the tail-only capability set, one for the `dot_gate.go` reachability claim. Every
claim the step depends on was re-verified by hand after the passes returned. Claims that did not
survive that check are marked and corrected in place.

**Then a second reviewer re-ran all three passes against the same commit, with the first pass's
conclusions withheld.** Every line count reproduced. Six claims did not survive, and each is marked
below where it sits: the "same four steps" split is 6 shared and not 4, the two post-exit regions are
not 423 lines of duplication, row P3 named a gap the graph already fills, `noChangeTimeoutCh` is not
"diagnostic fidelity only", the dropped baseline probe has a third and worse consequence, and
deleting `dot_gate.go` breaks the production build rather than no test at all. **Where this document
and the first pass disagree, this document is the later reading.**

Other agents were editing `internal/daemon/agentlaunch.go`, the three `runAgentLaunch` call sites,
`workloop.go`'s tunnel block and `workloop_runplan.go` while this was measured. Every number here is
`1e22c0141` and may be a few commits stale. The line counts will drift. The findings will not — they
are about which side of a `return` a fact sits on, and that does not move by accident.

---

## 1. The size. The tail is 740 lines, 368 of them code, and Step 5 took none of them

**The `beadRunOne` figure the map quoted was stale.** §1c's heading and §0's table both said
1,603, and both are corrected in the same commit as this file. It is **1,705** at `1e22c0141` — 815
code, 824 comment, 66 blank. The function has grown
through Step 6, which is expected: Step 6 moved release sites onto a scope and each move carried its
reasoning with it.

**The tail, measured three ways.** "The tail" has to be bracketed before it can be counted, and the
three plausible brackets give three different answers. All are at `1e22c0141`:

| Bracket | Total | Code | Comment | Blank |
|---|---:|---:|---:|---:|
| `switch workflowMode {` → end of function | 962 | 495 | 430 | 37 |
| the whole `switch` statement, header and `default:` arm and closing brace | 222 | 127 | 88 | 7 |
| the DOT arm alone | 217 | 124 | 86 | 7 |
| **switch close → end of function (the single-mode tail)** | **740** | **368** | **342** | **30** |

The last row is the one Step 7 means. The DOT arm is not deletable — it is what survives. Rows two
and three are separated on purpose: 222 was first reached by subtracting the tail from 962, which
counts the `switch` header, the `default:` arm and the closing brace as if they were DOT code. The
DOT arm measured on its own is 217.

**The number a plan should quote is 368, not 740.** Comments are 46% of the tail and they are long on
purpose. A raw line count overstates the deletable work by very close to a factor of two.

**And 368 is still an upper bound, not a cost.** §3 finds **eighteen** capabilities in the tail that
must be ported onto the graph node rather than deleted. Code that moves is not code that goes away.
Quote 368 as "code touched". **Do not quote a deletion figure at all** — the honest net is 368 minus
whatever those eighteen ports weigh once written, and they will not weigh nothing.

### 1a. Correcting §1c: Step 5 removed nothing from the tail

§1c carries a ⚠ that says the 722 figure is unsafe because "Step 5 took 161 lines out of the function,
and how many of them came out of the single-mode tail was never recorded. Do not rescale it."

**The premise is answerable, and the answer is none.** Measured across every commit that touched
`workloop.go` since the review-loop retirement:

| Commit | Date | `beadRunOne` | Tail | Tail code |
|---|---|---:|---:|---:|
| `3cec5afd7` review-loop retired | 07-28 | 2,240 | 1,178 | 552 |
| `18d59aded` single onto the one launch path | 07-29 | 1,770 | 708 | 357 |
| `756b6604c` scheduler split out | 07-29 | 1,773 | 711 | 357 |
| `acb2194d0` | 07-29 | 1,764 | 711 | 357 |
| **`4070c75ed` Step 5, the run-plan resolver** | **07-30** | **1,591** | **711** | **357** |
| `fabc7cb21` socket-path check hoisted | 07-30 | 1,603 | 711 | 357 |
| `4af0ba8c0` Step 6, the scope | 07-31 | 1,666 | 722 | 360 |
| `dc0b15d17` Step 6, the run record | 07-31 | 1,667 | 739 | 368 |
| `f6c2613fb` Step 6, the evidence fact | 07-31 | 1,705 | 740 | 368 |

Step 5 took `beadRunOne` from 1,764 to 1,591 and left the tail at exactly 711 lines and exactly 357
lines of code. **Every one of Step 5's 173 lines came from above the mode switch.** The ⚠'s worry was
the wrong worry.

**The real correction is the opposite one: the tail has GROWN, not shrunk.** It has gone 711 → 740
since Step 5, because Step 6's migration commits landed inside it. Anyone who "rescales 722 downward
for Step 5" gets a number wrong in both magnitude and direction.

**Where 722 came from is not reproducible.** At `756b6604c`, the commit the retired text in §2b names,
no natural bracket yields it: switch-intro comment 945, `switch` line 933, `default:` 774, switch
close 711, the `Single-mode dispatch` header 710. The figure was an estimate, not a measurement. It
happens to be exactly right for one commit — `4af0ba8c0`, on 2026-07-31, as the tail passed 722 going
up. That is a coincidence and not a vindication.

**For scale, the other side.** `dispatchDotAgenticNode` is 544 lines (313 code, 215 comment). §2b's
"about 543 lines" is right.

### 1b. One row of §0's headline table was never a function size

Re-measuring §0 turned up an error older than any drift. **The `runAgentLaunch` row says 836. That is
the size of `agentlaunch.go`, not the size of the function.** At `fabc7cb21`, the tip on 2026-07-30
when the row was written, the file was exactly 836 lines and the function was 535. Reproduce both
with `git show fabc7cb21:internal/daemon/agentlaunch.go | wc -l` and the same pipe through
`awk '/^func runAgentLaunch/,/^}/'`. Every other row in that table is a
function size, so the row invited a false comparison — it made the one launch path look larger than
`driveDotWorkflow` when it is a little over half of it. At `1e22c0141` the function is 558 and the
file is 859. `executeCognitionGate` is 230, not 234. `workloop.go` is 3,356 lines, not 3,227.

**Every function size and every row of the per-commit table was measured twice, by two people, with
two brackets.** The per-commit table was re-derived independently by bracketing from the closing brace
of `switch workflowMode {` to the end of the function, and it reproduced 711 / 357 at `756b6604c`,
`acb2194d0` and `4070c75ed`, and 740 / 368 at `1e22c0141`. The finding that Step 5 removed nothing
from the tail does not rest on one measurement. **The one exception is the 222-line row above**, which
was first derived by subtraction rather than measured, and is corrected there.

---

## 2. Piece 1 — the post-exit interpretation. It is not four steps, it is seventeen against twelve

§2b says the two copies "each hand-roll the same four steps": probe worktree HEAD, emit
`implementer_phase_complete`, run the process-exit commit fallback, decide what no-commit means.

**The four steps are real, they exist on both sides, and among themselves they run in the same
relative order.** That much survives. But they are a small part of 17 steps on the single side and 12
on the graph side, and the framing hides three whole classes of divergence that sit between them.

**Measure the post-exit region, not "the tail".** From the line after `runAgentLaunch` returns to the
closing brace, single is 422 lines (193 code) and the graph is 171 lines (91 code). Those two numbers
are exact and they are the right comparison boundary. **Do not call them "duplicated".** Of the 193
single-mode code lines, most have no graph counterpart at all — the escaped-worktree guard,
`WireSpine`, the shutdown-drain branch, the terminal-classification switch, the watcher-error
composition, the Pi stderr defer. **The genuinely paired code is about 50 to 60 code lines on each
side**: the phase-complete emit, the commit fallback, and the HEAD guard. The honest sentence is "two
post-exit regions, of which about a third is a divergent copy".

**Six steps are shared, not four.** `defer launch.Cleanup()` and the `ctx.Err()` check are on both
sides too. So the split is 6 shared, 11 single-only, 6 graph-only. Quoting 4 makes the divergence look
larger than it is.

### The divergence table

| Step | single | dot graph node | Verdict |
|---|---|---|---|
| Baseline SHA for "did HEAD advance" | `plan.ParentSHA` — the repo HEAD the worktree was cut from | `preHeadSHA`, probed by `resolveDotWorktreeHEAD` immediately before each node launches | **DELIBERATE** — a cascade needs a per-node baseline, because node N's baseline is node N−1's tip |
| Baseline probe error | n/a, there is no probe | dropped: `preHeadSHA, _ := …`. Three consequences — see the note below the table | **DEFECT** (graph) — `hk-o4sgg` |
| Post-exit probe helper | `gitprobe.ResolveWorktreeHEADVia` | `resolveDotWorktreeHEAD` | **ACCIDENTAL**, harmless. `resolveDotWorktreeHEAD` branches on `runner == nil`, and `gitprobe.ResolveWorktreeHEADVia` already does that same nil check on its first two lines. It is one implementation with a redundant shell, not two implementations |
| `implementer_phase_complete` payload | `runlaunch.EmitImplementerPhaseComplete` with exit code, stderr tail, commit-landed, duration | the identical call with the identical arguments | **no divergence** — the payload fields match exactly |
| Who gets the event | once per run | once per implementer node. Reviewer nodes emit `reviewer_verdict` instead | **DELIBERATE** |
| Emission on the abort path | the `handle.Aborted()` branch returns **above** the emit — no event | `ctx.Err()` is checked **after** the emit — the event is produced | **DEFECT** (single) — `hk-aekon`. The comment above the single-mode emit still claims it fires "regardless of how" the run exited |
| HC-065 lifecycle terminal transition | `transitionToTerminated` drives Terminating → Terminated/Failed and emits `lifecycle_transition` | never called | **DEFECT** (graph) — `hk-b4xf2`. Its own godoc says "EVERY exit path", and that is false at two scopes, not one: four earlier returns skip it inside single mode as well (`agentLaunchPrelaunchFailed`, `agentLaunchErrored`, `agentLaunchReadyTimeout`, the `Aborted()` branch) |
| `handle.SetMachine` | set in `OnLaunchedExtra` | not set, so `stalewatch.go`'s silent-hang drive and `stategather.go`'s lifecycle read are inert on the path that carries the traffic | **DEFECT** (graph) — same bead. **§2b lists neither of these two rows** |
| Commit-fallback harness wrapper | re-looks-up the harness and calls `pi.EnsureRefsTrailer` for Pi, `codex.EnsureRefsTrailer` otherwise | reuses `launch.Harness.Completion()` and always calls `codex.EnsureRefsTrailer` | **ACCIDENTAL**, cosmetic — see below |
| No-work detector | `codex.NoWorkSuspected` runs on the **codex leg only**, so a single-mode Pi run never gets it | runs for every process-exit harness, Pi included | **DEFECT** (single) — `hk-3ywqv`, and §2b does not mention it. Not an independent row: the graph covers Pi **because** it always calls the codex function, so this is downstream of the row above and not its mirror. Fix that row the obvious way and this coverage disappears |
| What "no commit" means | `noCommitGuardShouldReopen` collapses subsumption into a boolean — reopen with `no_commit_during_implementer`, or pass. The distinct subsumed outcome lives on a second site, the `noChangeTimeoutCh` branch | three-way in one place: `shared.MainHistoryHasRefsTrailer` → subsumed, iteration < 2 → hard failure, iteration ≥ 2 → SUCCESS, deferred to the diff-hash check. Plus a `node.NonCommitting` opt-out with no single equivalent | **DELIBERATE** — `NonCommitting` is WG-041 / EM-058, the iteration-≥2 pass-through is EM-015e, both cited verbatim in the code. **§2b's "bare HEAD-advance compare" is wrong.** But do not say single is "binary" either: it has the same three concepts, scattered across two sites with one collapsed to a boolean |
| Which repo the subsumption check probes | `activeRepo` — and the call site's comment states the rule: cross-repo uses `activeRepo`, local uses `env.ProjectDir` | `env.ProjectDir`, always. `driveDotWorkflow` never receives `activeRepo` | **DEFECT** (graph) — `hk-pq3ex`. On a cross-repo bead the cascade greps the wrong repo, so subsumption can never fire and a subsumed bead hard-fails at iteration 1. Say "the cascade", not "the graph": `activeRepo` does reach the DOT **merge** through `bridge.WireSpine(SpineArgs{ActiveRepo: …})`, so a cross-repo graph run merges into the right repo and probes the wrong one |
| Probe error at the no-commit decision | `curHeadErr == nil && noCommitGuardShouldReopen(…)` — a failed probe **skips the guard**, and an exit-0 run then auto-closes as success | `if headErr != nil { return error }` — hard node failure | **DEFECT** (single, fail-open) — `hk-fmere`. The graph has the right polarity here |
| Escaped-worktree check | yes, inside the merge exclusion domain | never | **DELIBERATE** per RSM-008 as written — but see §5 |
| `noCommitGuardShouldReopen` as a symbol | yes | no — **but the graph runs an equivalent guard inline** | **ACCIDENTAL** duplication, and the placement is deliberate. RSM-008's parenthetical "(it does not run those guards)" is false at HEAD |
| Pi provider profile on the launch context | set from `plan.PiProfile` | none of `Provider` / `APIKeyEnv` / `APIKeyFile` / `BaseURL` / `API` is set, and `runAgentLaunch` does not supply them | **DEFECT** (graph) — `hk-yo9g6`, re-verified at HEAD after the recent `agentlaunch.go` churn |
| `emitImplPresence` join and leave | join in `OnLaunchedExtra`, leave in a defer on every exit | never | **ACCIDENTAL** |
| Pi evidence on failure | writes `pi-stderr.log` beside the retained worktree | no stderr write — **but the worktree retention itself is now shared** | **§2b's row is half-stale** — see §7 |
| Independent tmux session | `ConfigurePerRunSubstrate` → independent spawner, run-registry write, skip predicates | passes no `ConfigurePerRunSubstrate`, and teardown is unconditional | **DELIBERATE** today, and it is the blocker — see §3 |
| Merge retry budget of 3 / `ChargeReviewLoopFailure` | no | yes — **but neither lives in `dispatchDotAgenticNode`**. `runBridgeConfig` keys on the mode at `NewRunBridge`, and the charge happens in `beadRunOne`'s DOT branch | **DELIBERATE**, but §2b files it in the wrong place |
| `auto_status` work-product inspection | no | `runAutoStatusInspection`, gated on a node attribute | **DELIBERATE** — single has nowhere to put it |
| Terminal classification | `handler.MapWaitReturnToTerminalEvent` plus a five-way switch | none — returns an outcome and lets `driveDotWorkflow` and the mode switch classify. The graph is handed a nil noChange channel, so it has no noChange-timeout class at all | **DELIBERATE** (different contracts), but it is a whole class of post-exit work the "four steps" omits |

### The dropped baseline probe error has three consequences, and the third is the worst

`hk-o4sgg` was filed on the first two. The third was found on re-check and is more damaging than
either.

**State the precondition, or a reviewer will distrust the whole row.** `resolveDotWorktreeHEAD`
errors on empty output, and the POST-exit probe hard-fails the node on error. So the bad state needs
the PRE-probe to fail while the POST-probe succeeds — a transient, most plausibly on a remote run
where the probe crosses SSH. This is not an every-run bug. Written flat as "the error is dropped" it
reads as always-on, and a reader who checks it finds the `headErr != nil` hard fail two lines below
and stops believing the rest.

1. **The no-advance guard is skipped.** With an empty baseline `postHeadSHA == preHeadSHA` cannot
   hold, so a node that did no work returns `OutcomeStatusSuccess`.
2. **The trailer amend leg is disabled, and the damage is larger than "no trailer".**
   `EnsureRefsTrailer` gates on `parentSHA != "" && curHead != parentSHA`. With an empty parent an
   agent that committed without a trailer falls past the amend leg into the dirty check, finds the
   tree clean, and returns `shared.RefsNoChange`. So the commit lands untrailered, the outcome is
   mislabelled as no-change, and that mislabel then feeds `codex.NoWorkSuspected` and produces a
   false `implementer_no_work_suspected` event on a node that did real work. Afterwards
   `shared.MainHistoryHasRefsTrailer` can never find that bead.
3. **The empty baseline poisons the quit-on-commit watchdog.** `preHeadSHA` is also what
   `dotDeliver` hands to `pasteInjectQuitOnCommit`, whose detector is `headSHA != initialSHA`. An
   empty baseline makes that true on the FIRST poll tick, so the watchdog sends `/quit` and force
   kills a claude implementer node seconds after the brief is delivered — a zero-turn run. This is
   graph-only precisely because single mode's baseline is `plan.ParentSHA`, which cannot be empty.

### The commit-fallback claim, verified

**§2b is right, and for the reason it gives.** `codex.EnsureRefsTrailer` and `pi.EnsureRefsTrailer`
are structurally identical: same guard clauses, same decision table, the same four primitives in
`internal/harness/shared/refstrailer.go`, the same literal staging pathspec, the same trailer line,
and neither overrides author or committer. The Pi outcome type is a type **alias**, not a parallel
enum. The only difference is the message prefix, so a Pi node's daemon-fallback commit on the graph
path really does say `feat(codex)` and nothing else changes.

**What §2b misses is that the same branch carries the no-work detector**, and there the drift runs the
other way. Fixing the wrong-wrapper row by adding a Pi branch to the graph will silently delete Pi
no-work detection unless the detector is carried across deliberately.

---

## 3. Piece 2 — all five capabilities are real, and the list is short by thirteen

**All five of the step's named capabilities survive checking at `1e22c0141`.** None has been fixed by
a recent commit, and each is still reachable from exactly the tail. That is the good news and it is
where the good news ends.

**Step 6's equivalent list was short by three. This one is short by thirteen.** Walking the tail
sequentially found **seventeen** further capabilities that live only there. Thirteen must be ported
before the tail can be deleted. Three are better dropped than ported. One is not tail-only at all and
the plan would have been right to leave it alone.

### The five, confirmed

| # | Capability | Proof it is still tail-only | Status |
|---|---|---|---|
| P1 | Independent tmux session and restart adoption | `ConfigurePerRunSubstrate` sets the session id and calls `runpkg.Write` — the only caller in the tree. `adoptDeadRunSessions` and `adoptLiveRunSession` are fed by that write alone | NEEDED, and gated — see §5 edge 4 |
| P2 | Escaped-worktree guard | `runmerge.CheckMainWorkingTreeDirty` and `emitImplementerEscapedWorktree` each have one call site | PORT DECISION PENDING — D2 and `hk-co8g8` |
| P3 | `noCommitGuardShouldReopen` | one call site. The graph has a partial equivalent inline. **What it lacks is the cross-repo target, and that is all** | NEEDED, partly — see the warning below |
| P4 | Implementer comms presence join and leave | `emitImplPresence` has two call sites, both in the tail. The graph passes no `OnLaunchedExtra` | NEEDED |
| P5 | Pi provider profile on the launch context | the tail's `shared.LaunchCtx` sets all five fields from `plan.PiProfile`, and the graph's sets none | NEEDED — `hk-yo9g6` |

> **Do not route porting work off row P3 as first written.** It named two gaps, and one of them is
> not a gap: the graph already implements the iteration-1 hard fail verbatim. Only the cross-repo
> target (`hk-pq3ex`) is missing. Anyone who works P3 from the earlier wording goes looking for
> something that is already there.

**Independent re-check of all eighteen: 14 verified, 4 partial, 0 refuted.** The four partials are
P3, N2, N9 and N10, and each is qualified in place below. Partial means the capability is real and
the consequence is smaller than a flat reading suggests — none of them is safe to drop for that
reason.

### Thirteen more that must be ported

Each has exactly one caller, in the tail, and a consumer that is inert without it.

| # | Capability | What is lost on the graph path today |
|---|---|---|
| N1 | `bridge.Drain` on the shutdown branch | committed-but-unmerged work is dropped and re-dispatched — `hk-dk0sf` |
| N2 | `RunHandle.Aborted()` | **PARTIAL.** Against the graph this is attribution only, because a cancelled graph node reopens either way. It would matter inside the tail, where an abort that lost `Aborted()` would fall through to the shutdown branch and drain instead of reopening |
| N3 | `RunHandle.SetAgentType` | `bandwidthTunerBackstop`'s Pi filter cannot match, so Pi rate limits throttle unrelated work — `hk-a5hs1` |
| N4 | `RunHandle.SetMachine` | `stalewatch`'s silent-hang drive and `stategather`'s dashboard read see no machine — `hk-b4xf2` |
| N5 | Cold-start spawn semaphore | remote graph runs are ungated on concurrent cold starts — `hk-6n0z7` |
| N6 | `transitionToTerminated` | no HC-065 terminal transition, no `lifecycle_transition` event — `hk-b4xf2` |
| N7 | Stop-hook terminal classification | **a node that commits then signals failure is recorded SUCCESS and merged** — `hk-v4wer` |
| N8 | Stderr tail in the failure reason | an exit −1 crash with no NDJSON leaves no diagnostic — and the asymmetry is sharper than "the graph lacks it". The tail appends the **last** 200 bytes to the reopen reason. The graph's one event fills `StderrTailHead`, which slices from the **front** despite its name. For a crash the useful bytes are at the end, so the graph omits it from the reason and captures the wrong end in the event — `hk-08n9c` |
| N9 | The post-mode scenario gate | **PARTIAL.** The tail leaves `SkipGate` false and the DOT arm sets it true and relies on a `commit_gate` node. `internal/daemon/standard-bead.dot` has that node and it runs `scripts/scenario-gate.sh`, which the file itself says mirrors `internal/daemon/scenariogate.go`. That is a deliberate mirror, not a gap. The exposure is an operator-authored graph with no gate node |
| N10 | Cross-repo `activeRepo` on post-exit reads | **PARTIAL.** `hk-pq3ex`. Only the subsumption-check hardcode has teeth. The `workspace.WorktreeRootPath` half is inert, because `isHarmonikManagedWorktree` falls back to a substring match on `/.harmonik/worktrees/` that fires whatever the argument is |
| N11 | `SkipAbortKill` / `SkipTeardown` and the shutdown early return | the other half of P1. A port that writes the registry record but drops these strands beads `in_progress` with no reopen |
| N12 | Pi post-mortem stderr write | `runAgentLaunch` publishes the capture directory and no graph symbol reads it, so a failing Pi node keeps stdout only |
| N13 | `sdHarness` | every graph run writes a `sessiondata` record with an empty harness field — `hk-bri7u`. `dashboardgather.go` groups usage and cost by a key that includes `rec.Harness`, and the graph is the production default, so effectively every real run collapses into one empty-harness bucket |

**N7 and N9 carry more than their one-line entries show.** N7 means the default path has no terminal classifier: it
decides success on "did HEAD move" alone, so a committed run that then signalled failure is merged.
N9 means a naive single-shot graph silently loses the scenario gate unless the graph carries a
`commit_gate` node. Both are the kind of thing that makes "just express single as a graph" sound
cheaper than it is.

### Three that should be dropped rather than ported — and one of the three only on a condition

| # | Capability | Why not |
|---|---|---|
| X1 | The tail's `pi.EnsureRefsTrailer` branch | both wrappers reach the same primitives. Fix the graph's one-line harness selection instead of porting a branch. The "type alias" argument is real but was stated wrong: `piRefsOutcome = shared.RefsOutcome` is not an alias of a codex type, because there is no codex outcome type — the enum moved to `shared`. `piRefsOutcome` appears in neither signature and its only non-comment consumer is a test export, which strengthens the case |
| X2 | `noChangeTimeoutCh` | **PARTIAL, not "diagnostic fidelity only" — see the warning below. Droppable only if the structural subsumed check goes with it** |
| X3 | The tail's empty `LaunchCtx.Phase` | the graph does **not** always pass `implementer-initial`. It passes one of three — `implementer-initial`, `implementer-resume`, `reviewer` — chosen by node and iteration, and the resume and reviewer values are load-bearing in `MintClaudeSessionID` and in the Stop-hook message. Empty is still harmless and still the documented single-mode value |

> **Warning on X2. An earlier draft called `noChangeTimeoutCh` "diagnostic fidelity only". That is
> wrong about the tail, and acting on it would change outcomes.** The `case <-noChangeTimeoutCh:` arm
> is what carries the noChange-subsumed carve-out — the `MainHistoryHasRefsTrailer` check that closes
> the bead **approved**. That arm is reachable: `noCommitGuardShouldReopen` returns false exactly when
> the bead's work is already on main, so a subsumed run falls through the earlier guard and lands in
> this switch. Delete the channel on its own and the same run takes `default:`, hits `failRun`, and is
> reopened and re-dispatched instead of closed approved.
>
> **In the port it is still safe to drop, for a different reason than the one given.** The graph
> already has the structural subsumed check inline and already passes `nil` for the channel. So X2 is
> droppable **if and only if** the subsumed check goes with it. Do not read the earlier wording as
> permission to delete the channel from the tail while the tail still exists.

### One the plan would have been right to leave alone

`runmerge.SnapshotUntrackedFiles` runs **above** the mode switch — 82 lines above it — so every graph
run already pays for the snapshot and then discards it. Its sole consumer is the tail's escape check.
It is shared code with a tail-only reader, not a tail-only capability. Leave it in place.

### The reverse direction, which the step does not consider at all

"Make single-shot a graph" is only safe if the graph is a superset. **It is not, but the gap is
narrower than the tail-only list and mostly deliberate.** Eighteen capabilities exist only on the
graph side. Most are graph-shaped by nature and single has nowhere to put them: reviewer machinery,
the implementer resume back-edge, the no-progress guard and its four completion exemptions, traversal
caps and cap-hit salvage, per-node harness and model overrides, shell tool nodes, consolidate-join
severity-max, node dispatch observability, graph goal injection, sub-workflow nodes, and mechanism
gate nodes. Four are not graph-shaped and single simply lacks them: the merge retry budget of 3, the
`ChargeReviewLoopFailure` ladder (single reopens forever with no budget), the orphan-salvage tip SHA,
and the `auto_status` inspection.

**One stale comment found here.** `workloop.go` calls sub-workflow dispatch "out of scope". The
sub-workflow runner is real and reachable — `newDotSubWorkflowRunner` and its expansion path in
`internal/daemon/sub_workflow_runner.go`. The comment sits inside the region this step collapses, so
a reader who trusts it sizes the work too small. Filed as `hk-l9ev1`.

---

## 4. `dot_gate.go` is genuinely unreachable — but only one of the two legs carries the argument

The Step 7 entry says not to count `dot_gate.go` because its cognition-gate launch is dead twice over.
**The conclusion holds. The two legs are not peers, and the entry presents them as if they were.**

**Leg one — `daemon.Config.CPRegistry` is never assigned. This is the whole argument, and it holds.**
The entry's reproduction command is correct and returns nothing. Every write vector the
regex would miss was checked and ruled out. `daemon.Config` carries no struct tags at all, and the
field is typed as an **interface**, so no unmarshaler can populate it. Both production `daemon.Config`
literals omit the field. The one function that takes a `*Config` would still have to spell the
assignment the regex covers. There is no reflective `Set` outside test files. Reading the path rather
than assuming it: `daemonGate.LookupGate` returns "not loaded" on a nil registry with no default and
no fallback, and `dispatchDotGateNode` makes that lookup its FIRST step and returns a structural
eval-failure before the evaluator switch. `executeCognitionGate` is never constructed.

**Leg two — no graph declares a gate node. True, and much weaker than the entry implies.** The grep is
right: 39 `.dot` files in the tree, and only `specs/examples/quality-gate-policy.dot` declares
`type="gate"`. No alternate spelling exists — the parser reads `Node.Type` from the `type` attribute
alone and validates against a closed four-member enum. **But an operator supplies the graph.**
`beadRunOne`'s DOT case honours an absolute `plan.WorkflowRef` verbatim, and that value comes from
`harmonik run --workflow-ref <path>` through the queue item. An operator can hand the daemon a graph
with a gate node today, with no code change and no file in this repo. **Leg two is a statement about
the shipped file set, not about reachability.** Leg one is what stops that operator.

**Practical consequence for the step.** Leave `dot_gate.go` out, as the entry says — but on leg one's
authority only. If anyone ever populates `CPRegistry`, leg two buys nothing and the file becomes live
work.

**Leg one is stronger than the entry states.** `CPRegistry` appears in six places repo-wide and not
one of them is an assignment. **No test sets it either** — neither `Config.CPRegistry` nor
`WorkLoopDepsParams.CPRegistry` — so the injection seam that exists for it has never once been used,
although its own comment tells people to use it. Nothing outside `internal/core` ever constructs a
registry at all. State the lookup precisely, because it only holds in conjunction with that:
`daemonGate.LookupGate` returns "not loaded" **if and only if its registry is nil**, not
unconditionally.

**Sizes.** The file is **749** lines, not 721 — `UNWIRED-INVENTORY.md` row 41 already had it right.
`executeCognitionGate` is 230 lines of declaration around a 202-line body, and it is the largest
thing in the file.

### 4a. CORRECTED — "leave it out" is right, "it is a free removal" is not

An earlier draft of this section said deleting `dot_gate.go` means deleting two dead test seams and
the two `core.NodeTypeGate` arms, and that **no test breaks**. That is wrong on both halves, and it
is the part that changes what the plan should tell people.

**The PRODUCTION build breaks first, before any test.** `dispatchDotGateNode` has two non-test
callers: a `NodeTypeGate` case in `dot_cascade_core.go` and another in `sub_workflow_runner.go`.

**Three test files break, not zero.** One fails to compile on three exported seams. One fails to
compile **and** loses live coverage, because it really executes the quit-on-gate-file paste injection
under a fake clock. One hard-codes the string `"dot_gate.go"` in a list of launch-site files and
fatals at run time on a missing file. The earlier claim that "the one conformance test that mentions
the cognition gate anchors on `runAgentLaunch`" is wrong twice over: there are two such tests, and the
second names the file as a string literal.

**Removal also orphans the daemon's whole gate-port adapter** — the adapter type, its lookup, the port
accessor, the field on the run-ports struct, the interface in `internal/runloop`, and the registry
field in three places. And it strips the only non-test caller of three pure predicates in
`internal/policy`, whose own test would then cover three functions that nothing calls.

**So the honest statement is narrower than "dead code".** The cognition half is unreachable in
production. The file is not free-standing. Wiring it or deleting it is a multi-file change and must be
priced as one, not listed as a free removal. Filed as `hk-f1ymu`.

---

## 5. The hard ordering edges

Five, and only the first is in the step as written.

1. **Piece 1 before piece 2.** Collapsing the post-exit interpretation into one place is what makes
   the port in piece 2 small. Reversed, every capability gets ported into two copies and then
   deduplicated.

2. **`hk-co8g8` before the escaped-worktree guard is ported.** The guard fires on a sibling's
   half-finished merge and kills an innocent run. `OPEN-DEFECTS.md` calls it the most serious item in
   that file. Porting it onto the graph puts a known-broken guard on the path that carries all the
   traffic. RSM-008's own OPEN note says the same thing in the spec.

3. **Decision D2 before either guard moves.** The escaped-worktree check and the no-commit guard are
   two of the five capabilities, and RSM-008 currently **forbids** them on the graph path. Porting
   them is a spec amendment, not a refactor, and it cannot be done under the step's own authority.

4. **`hk-jyh5t` and the boot orphan sweep before the independent session is ported.** The step calls
   `hk-mh3qy` "the real blocker". It is — but `hk-mh3qy` is itself blocked. Step 6's map found the
   survive-shutdown feature does not survive: the boot orphan sweep kills every run session before the
   adoption pass reaches it, and `WaitWithSocketGrace` kills it earlier still, inside the daemon's own
   process. **Porting the independent session to the graph today ports a capability that is defeated
   twice before it can pay off.** Either fix the two defeats first, or port it knowing it buys
   nothing yet and say so in the commit.

5. **A new carrier for the `review_bypassed` audit before `core.WorkflowMode` reduces to one value**
   — `hk-pyouo`.
   The step's closing sentence says the enum collapses. `emitReviewBypassed` keys on
   `mode == core.WorkflowModeSingle`, and EM-012a requires the daemon to audit that bypass. Collapse
   the enum and the obligation has nothing to fire on. The audit has to move to a graph-topology
   predicate — "no reviewer node on the sole inbound edge to `close`" — or the `workflow:single` label
   has to survive as a graph selector rather than a mode. `--workflow-mode single` in
   `cmd/harmonik/run.go` and the rejection in `internal/projectconfig` are two more surfaces on the
   same thread.

**One thing that is NOT an edge, and looks like one.** A reviewer-less `start → implement → close`
graph does not violate the review floor. WG-051 says an overriding graph is explicitly **not** subject
to WG-047–WG-050, which constrain the canonical default only. The validator will accept it. Only the
audit obligation of edge 5 stands in the way.

---

## 6. Defects found while mapping

Eighteen, all filed. Four are in the post-exit region this step collapses, so they are candidates to
fold in rather than chase separately.

**Count them carefully, because the obvious summary is wrong.** Of the eighteen, **ten are runtime
harms on the graph path**: `hk-o4sgg`, `hk-pq3ex`, `hk-v4wer`, `hk-dk0sf`, `hk-6n0z7`, `hk-a5hs1`,
`hk-b4xf2`, `hk-bri7u`, `hk-08n9c`, `hk-lprn7`. Three are single-mode harms — `hk-fmere`,
`hk-aekon`, `hk-3ywqv`. Five are records or comments rather than behaviour — `hk-u99ha`, `hk-98ab7`,
`hk-l9ev1`, `hk-f1ymu`, `hk-pyouo`. **Ten against three is still the pattern §2b noticed on a smaller
sample**, and it is worth stating plainly: single mode is the mode nobody runs and it is the mode
that got the care. Do not inflate ten to twelve by mixing this list with the twelve capability rows
§2b never names — those are a different set, and two of those twelve are qualified partial.

**The most serious one first.** `hk-v4wer` — the graph decides node success on "did HEAD advance"
alone. It never reads the socket outcome and never runs the terminal classifier. **A node that commits
and then signals failure is recorded as SUCCESS and its work is merged**, and a watcher failure
(malformed NDJSON, a panic, a line too long) leaves the graph with no signal at all. This is the path
that carries essentially all traffic.

**In the post-exit region — fold these into piece 1:**

- **`hk-o4sgg`** — the graph drops its baseline HEAD probe error, so a node that did no work records
  SUCCESS and the trailer amend leg is disabled.
- **`hk-pq3ex`** — the graph probes `env.ProjectDir` instead of `activeRepo` for the subsumption
  check, so a subsumed cross-repo bead can never be recognised and hard-fails at iteration 1.
- **`hk-fmere`** — single's no-commit guard fails open when its HEAD probe errors, and an exit-0 run
  then auto-closes as success. The graph has the right polarity at the same decision.
- **`hk-aekon`** — `implementer_phase_complete` is skipped on the single-mode abort path, and the
  comment above the emit claims the opposite.

**Capability gaps on the default path — each is its own commit, and each is also a port in §3:**

- **`hk-v4wer`** — no terminal classification on the graph path. See above.
- **`hk-dk0sf`** — graph runs never drain committed-but-unmerged work on shutdown, so the work is
  lost and re-dispatched. The tail already fixes this for single mode.
- **`hk-6n0z7`** — remote graph runs never take the cold-start spawn semaphore, which reproduces a
  known agent-ready timeout under load. **Step 6's resource map recorded this token as "per-run, but
  only for a remote single-implementer run" and read that scope as a design fact. It is not a design
  fact. It is this omission.**
- **`hk-a5hs1`** — `SetAgentType` is never called on a graph run, so the bandwidth tuner's Pi filter
  cannot match and Pi rate limits throttle unrelated work.
- **`hk-b4xf2`** — `transitionToTerminated` and `handle.SetMachine` run only in single mode, so the
  HC-065 terminal transition, the stale-watcher silent-hang drive and `stategather`'s lifecycle read
  are all inert on the default path. The godoc says "EVERY exit path".
- **`hk-3ywqv`** — Pi no-work detection never runs in single mode and runs in graph mode only by
  accident, because the graph routes Pi through the codex wrapper.
- **`hk-bri7u`** — `sessiondata` records are half-empty in both directions: graph runs record no
  harness, single runs record no commit SHA. **This is the same defect shape Step 6 just fixed for the
  Pi evidence fact** — a fact assigned on one side of a return that both modes were meant to share.
  Worth reading as a pattern rather than an incident.
- **`hk-08n9c`** — the graph's one failure event fills `StderrTailHead` from the FRONT of stderr, so
  a crash's useful bytes are never captured. The field name says tail. The code slices head.
- **`hk-lprn7`** — reviewer feedback is never delivered to a remote graph run's implementer, so a
  REQUEST_CHANGES re-dispatch repeats the same work. Independent of this step.
- **`hk-f1ymu`** — two dead test seams make the unreachable cognition gate look live.

**Records, not code:**

- **`hk-u99ha`** — `hk-co8g8`'s ledger description still describes the lost-commit merge race it was
  filed as, although it was root-caused the same day as the escape guard firing on a sibling's merge.
  `OPEN-DEFECTS.md` has the corrected account and `DECOMPOSITION-MAP.md` cites it correctly, so a
  reader who verifies the citation with `br show` finds a body that contradicts the map and may
  conclude the map is wrong. That is exactly how a correct conclusion gets distrusted.
- **`hk-98ab7`** — `core.Run`'s workflow-mode comment names the retired `review-loop` as valid and
  says the field defaults to `single`. Both have been false since v0.9.0 and v0.10.0.
- **`hk-l9ev1`** — `workloop.go` calls sub-workflow dispatch "out of scope" inside the region this
  step collapses. The sub-workflow runner is reachable.

**Recorded so a later step does not break it:**

- **`hk-pyouo`** — collapsing `core.WorkflowMode` to one value removes the only thing
  `emitReviewBypassed` keys on, so the EM-012a audit obligation disappears with no compile error and
  no failing test. Ordering edge 5.

---

## 7. What §2b and the Step 7 entry get wrong at `1e22c0141`

Corrections applied to `DECOMPOSITION-MAP.md` in the same commit as this file.

1. **"The same four steps."** The post-exit split is 6 shared, 11 single-only, 6 graph-only. The
   framing hides the lifecycle transition, the terminal-classification switch and the
   reviewer-verdict branch. It also invites a second error — reading the two regions as 422 lines of
   duplication against 171. About 50 to 60 code lines a side are genuinely paired.
2. **"The graph does a bare HEAD-advance compare."** False. The graph consults
   `shared.MainHistoryHasRefsTrailer` — the same subsumption test that is the second half of
   `noCommitGuardShouldReopen` — and then branches three ways.
3. **The RSM-008 quote is stale.** §2b quotes "The review-loop and DOT paths MUST NOT enter
   `Guarding`". The spec at HEAD says "The **DOT path** MUST NOT enter `Guarding`". It was cleaned on
   2026-07-30.
4. **"RSM-008 is stale in its own right: RSM-007 still names review-loop as a live fork."** False at
   HEAD — RSM-007 reads "(DOT cascade, single-shot)". Both mentions were already fixed. The live
   staleness is a different one: RSM-008's parenthetical "(it does not run those guards)" is now
   false, because the graph does run a no-commit guard.
5. **The merge-retry-budget row is misfiled.** Both facts are true, but neither lives in
   `dispatchDotAgenticNode`.
6. **The Pi-stderr row is half-stale.** `f6c2613fb` moved the retain-evidence fact to the launch, so a
   failed **local** graph Pi run now keeps its worktree and its captured stdout. Only the post-mortem
   `pi-stderr.log` **write** is still single-only. Say "local": on a REMOTE Pi run the capture is
   written on the daemon box while retain-evidence keeps the worker's worktree, so retain-evidence
   still preserves nothing there. That carve-out is recorded in `f6c2613fb`'s own review verdict.
7. **The wrong-wrapper row is right but incomplete** — the same branch carries the no-work detector,
   and there the graph is ahead.
8. **§1c's ⚠ has the wrong premise and points the wrong way.** Step 5 removed nothing from the tail,
   and the tail has since grown. See §1a.
9. **The line figures.** `beadRunOne` is 1,705, not 1,603. The tail is 740, not 722.
   `dispatchDotAgenticNode` is 544, and "about 543" was right. `dot_gate.go` is 749, not 721.
10. **`dot_gate.go`'s "dead twice over" overstates leg two.** An operator-supplied `--workflow-ref`
    graph can declare a gate node today. Only the unset registry stops it.
11. **§0's headline table quoted a file size in a table of function sizes.** The `runAgentLaunch`
    row's 836 is `agentlaunch.go`, not `runAgentLaunch`. See §1b. §0's own dated snapshot table
    further down also carries a stale total and a stale `beadRunOne` share, and now says so.
12. **The Step 7 entry said five capabilities block piece 2.** All five hold, and there are
    thirteen more. It also named `hk-mh3qy` as "the real blocker" without saying that `hk-mh3qy` is
    itself blocked twice over — see edge 4.
13. **The `noChangeTimeoutCh` drop is conditional, and the map now says so.** An earlier reading
    called it "diagnostic fidelity only". Dropping the channel from the tail on its own reopens a
    subsumed bead instead of closing it approved. See the X2 warning in §3.

---

## 8. Shape of the work

In order. Each is one commit, each independently reviewable.

1. **Fix the four post-exit defects** (`hk-o4sgg`, `hk-pq3ex`, `hk-fmere`, `hk-aekon`) **before**
   collapsing anything. Two of them are polarity errors that a collapse would have to pick a winner
   for, and picking silently is how the wrong one wins. Fixing them first makes the collapse a move
   rather than a decision.
2. **Collapse the post-exit interpretation** into one function both modes call, the way
   `runAgentLaunch` collapsed the launch. The deliberate rows of §2's table become parameters or
   node attributes, and the accidental rows become one behaviour. This is piece 1, and it is the whole of
   what Step 7 can do without a spec amendment.
3. **Answer D2**, and amend RSM-008 whichever way it goes. Nothing in piece 2 that touches either
   guard can start before this.
4. **Fix `hk-co8g8`** if D2 chose to extend the guards.
5. **Fix `hk-jyh5t` and the boot orphan sweep**, so the independent session is worth porting.
6. **Port the eighteen capabilities of §3 onto the graph node**, one commit each, defect-first:
   `hk-v4wer` (terminal classification), `hk-dk0sf` (shutdown drain), `hk-6n0z7` (cold-start cap),
   `hk-b4xf2` (lifecycle machine), `hk-a5hs1` (agent type), then the rest.
7. **Move the `review_bypassed` audit** onto a graph-topology predicate.
8. **Delete the tail**, collapse `core.WorkflowMode`, and retire the `--workflow-mode single` surface.

Steps 3, 4 and 5 are not this step's work in any meaningful sense — they are three separate pieces of
work the step's second half sits on top of. **Step 7's honest scope is items 1 and 2.** Item 8 is four gates away.

**The reframe that matters, and it makes the step cheaper rather than dearer.** Item 6 reads as a cost
of deleting the tail. It is not. Most of those eighteen ports are fixes for defects that harm the
default path **today**, and every one of them pays off whether or not the tail is ever deleted. So
item 6 is not blocked on items 3 to 5 and should not wait for them: start it now, ordered by harm,
and let the deletion in item 8 fall out at the end as the thing that becomes possible rather than the
thing being aimed at. Framed that way, only P1, P2 and P3 — the independent session and the two
guards — are genuinely gated, and they are the three the step already knew were hard.
