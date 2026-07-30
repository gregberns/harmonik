# Open defects found during the delete-and-rewrite program

**Why this file exists.** The bead ledger is machine-local and gitignored — beads do not travel between
clones and never reach CI. A defect that lives only as a bead is invisible to everyone but the machine
that filed it. This file is the tracked record; the bead IDs are a convenience for looking up the full
detail on the machine that has the ledger, not the handle for the thing.

**Standing directive (operator, 2026-07-28):** defects found along the way are **recorded, not chased**.
Nothing here is an instruction to stop and fix it. The point of the table below is the *third* column —
several of these are eliminated as a side effect of work already planned, and knowing which ones saves
someone fixing a thing that is about to be deleted.

Found 2026-07-29 unless noted.

---

## Eliminated by planned work — do not fix these directly

> **Count correction, 2026-07-29.** These two were measured when there were **five** agent-launch sites.
> Deleting `reviewloop.go` removed two of them, so the live count is **three**: the single-mode tail in
> `workloop.go`, the graph-node path in `dot_cascade_core.go`, and the graph-gate path in `dot_gate.go`.
> The defects are unchanged in kind — the *coverage fractions* below are now 1-of-3, not 1-of-5.

| What is wrong | When it goes away | Bead |
|---|---|---|
| **The credential guard covers 1 of 3 dispatch sites, and not the default one.** `d2RemoteAPIKeyRefusal` runs only on the single-mode path. DOT is the default and carries essentially all traffic, so the 2026-05-30 credential-leak gate protects the path that has run twice ever and not the path everything uses. The conformance test repaired 2026-07-28 guards the guard's *shape*, not its *coverage* — which is why nothing caught this. | Launch-path collapse (§Next step 4) — by construction, since one launch path cannot drift from itself | `hk-z4cow` |
| **Launch-failure classification is computed by every site and consumed by none.** `classifyLaunchFailure` maps a launch error onto structural event classes; `dispatchSegmentRun.emit` switches only on two other event types and drops both structural classes into `default:`. Sites then hand-roll the same check themselves, and at least one emits nothing. The purest instance of the 1-of-N pattern in the tree. | Launch-path collapse (§Next step 4) | `hk-q15hi` |

**Five more found 2026-07-29 while mapping the three sites, all eliminated by the same collapse.** Recording
them because each is a live production defect *today*, and because they are the direct evidence for why the
collapse is worth doing — every one is a guard that exists on one path and is simply absent on another,
with the compiler silent throughout. None is being fixed on its own.

- **A cognition gate's agent-ready signal carries no run id**, so the stale-run watcher skips it, the
  "never spawned" flag never flips, and the reaper stays armed for the whole run. The other two paths were
  fixed for exactly this; the gate was missed by that sweep.
- **A gate launch that fails reports no reason.** The gate sets both classifier errors, so the machine
  knows whether the spawn pool was saturated or the terminal-window request hung — and then emits neither.
  The operator sees a failed launch with no cause.
- **Single-mode runs never disarm the never-spawned reaper.** Only the graph-node path arms the proof. A
  codex or pi run in single mode is therefore killed around the thirty-minute mark while perfectly healthy
  — the same failure already diagnosed and fixed once for the graph path, never propagated.
- **The agent-ready-timeout event reports the wrong number on remote runs.** All three sites pass the
  *local* configured timeout to a parameter that means the *effective* one, so a remote run reports a bound
  shorter than the one that actually fired.
- **Two of the three paths emit that timeout on a cancellable context**, which is precisely the context the
  reaper has already cancelled by the time the emission runs. The third deliberately uses a detached one.

## RESOLVED by the review-loop retirement — verified 2026-07-29 on `e46658ed5`

Both were cases where `runReviewLoop` was the **sole** non-test home of a behaviour, so deleting the file
would have removed it from the product entirely — and neither would have failed to compile. Third
instance of the pattern that already cost this program the crew idle-reap tests and the D2 conformance
test. Both are now closed out; kept here because the *reasoning* is the reusable part.

| What was wrong | Resolution | Bead |
|---|---|---|
| **Crash-recovery resume worked only in review-loop mode.** `persistClaudeSessionID` was review-loop-only; single-mode and DOT both captured a Claude session id and dropped it. | **Consciously retired, with evidence** — the durable write had no reader anywhere (nothing reads `context.json` back, nothing consumes the persisted event, EM-031 recovery reads branch-tip trailers instead), and the resume that *did* work read an in-memory field, not the persisted copy. So the machinery was deleted rather than ported. No production symbol survives; only comments reference it. | `hk-5sebh` |
| **The default mode treated every merge failure as terminal.** `Retryable: runmerge.IsRetryableReason` was passed to the terminal spine only by the review-loop, and a DOT failure never charged the retry budget, so the close-with-needs-attention ladder could not fire on the default mode. | **Ported to DOT.** Both now ride the DOT arm of the terminal spine in `beadRunOne` — `Retryable: runmerge.IsRetryableReason` and the `Budget.ChargeReviewLoopFailure` ladder. Single-mode still carries neither, which is acceptable only because single mode is scheduled for deletion; if that sequencing changes, this reopens. | `hk-dqmw2` |

## RESOLVED by the sandbox-gate consolidation — 2026-07-29

| What was wrong | Resolution | Bead |
|---|---|---|
| **The sandbox gate never wrapped claude nodes on the default graph path.** Single mode computed the sandbox spawn unconditionally, covering both the substrate and the exec path. The graph-node path computed it only on the captured-session-id branch, so a claude node on the substrate path was never wrapped even with the backend configured and claude listed. Gates were never sandboxed at all. | **Consolidated, with the verification deferred by operator direction.** Measuring first is what made this cheap: against the live config (backend `srt`, harnesses `[pi]`) the divergence changed nothing in production, because pi captures its session id and was already sandboxed on the graph path, and claude mints its own and is not listed. So the per-site scope is gone. Every launch asks `sandboxSpawnForRun` and `sandbox.harnesses` is the only switch. Three mutation-checked tests guard it. The one live behaviour change is the build-cache redirect now reaching pi graph nodes, which is the deferred check — `NEXT_STEPS.md` item F, which also records what adding a harness to that list now costs | `hk-j52we` |

## Needs deliberate attention — nothing planned will fix these

| What is wrong | Notes | Bead |
|---|---|---|
| **A guard accuses innocent runs of escaping their worktree, and kills them.** Originally filed as a lost-commit race; **root-caused 2026-07-29 and it is neither a race nor a lost commit.** Full detail below — **still the most serious item in this file.** | Fix direction identified; `RSM-018` must be corrected in the same change | `hk-co8g8` |
| **A wave-group queue stalls for 5 minutes** whenever its lowest-indexed pending item is claimed by a sibling queue, while its other items sit ready. Self-heals, so it presents as "the queue was slow." Reaches production. Detail below. | Missing fallback, not a tuning problem | `hk-nown4` |
| **The registered bus decoder for `handler_capabilities` cannot decode what production emits** — wrong field key and `[]string` vs `[]int`. The wire path works (a different decoder agrees); it is the registered core payload type that is wrong, which breaks strict-decode replay verification. | Independent | `hk-b882r` |
| **A flapping SSH makes the remote C2 gate pass.** `runAutoStatusInspection` discards `ErrRemoteTransport`, the sentinel that exists specifically to distinguish "SSH failed, inconclusive" from "confirmed absent". The sibling reader in the same package explicitly retries on it. On the **kept** graph path — survives all Phase 3 deletions. Was the only genuine bug among 152 delta-lint findings. | Independent fix | `hk-sbd4l` |
| **Structural protocol mismatch on the 2nd and 3rd dispatch.** `TestScenario_ConcurrentMultiQueue_N2_HappyPath` fails 4/4 with `error_category=structural / sub_reason=protocol_mismatch` on a deterministic dispatch ordinal — not load. It sits on the known-flake allowlist, wrongly. | Second confirmed case of the allowlist absorbing a real defect | `hk-t2d7n` |
| **Non-single-mode runs cannot be adopted after a daemon restart.** `useIndepSession` is declared before the mode switch but assigned only in the single-mode tail, so the shared worktree-cleanup defer's guard can only be false on that one path. Review-loop and DOT runs lose their worktree on shutdown. | Needs the terminal spine collapsed — a *second* step after the launch-path collapse, not the same one | `hk-mh3qy` |
| **The Pi provider profile never reaches graph nodes or cognition gates.** The single-mode launch context carries the provider, key-env, key-file, base-URL and API fields resolved from the Pi profile; the graph-node and gate launch contexts set none of them. A Pi-harness node under the default mode is launched without its profile. | Found while mapping the launch sites. **Not** fixed by the launch-path collapse — spec construction stays at the call site by design, so this needs its own change | `hk-yo9g6` |
| **An entire tier of test failures is invisible.** `go test -tags scenario ./internal/daemon/` yields eight failures where the untagged run yields one. Root cause established: no assessment ever ran the tagged tier at all — the recipes only `go vet`ed it and the CI workflow carried `continue-on-error: true`. Full per-test disposition in `NEXT_STEPS.md` §5.2. | **The reporting half is FIXED 2026-07-29** — the flag is removed, so the tier is red and visible. §5.2's hardening remains, and half the tier still skips in CI (`hk-ynohn`) | `hk-97gcz` |
| **Two freeze gates match call sites, not declarations.** `runloop-freeze-gate.sh` and `queuewiring-freeze-gate.sh` make the `func`/`type`/`const` keyword optional in their declaration regex, so a bare call to a watched symbol reads as a declaration. Measured: a first draft of the new scheduler gate copied that pattern and raised **six false failures against a correct tree**. The two siblings have not fired only because nothing yet calls their symbols bare at line start. | A gate that cries wolf gets disabled, so this is worse than it looks. Fix: require the keyword at column 0, plus a second scan for the grouped `const (` / `type (` form. `workloop-scheduler-freeze-gate.sh` does both and is the model | `hk-freeze-gate-callsite-regex-uemrd` |
| **A reporting flag hid 20 straight real failures on every REST surface.** `continue-on-error` was documented in two files as masking only the *run* conclusion, leaving the step conclusion honest — and the nightly ops-monitor probe was built on that. False: run, step, and check-runs conclusions all read `success` after an exit 2. Only the annotations API told the truth, so the alert could never fire. | **FIXED 2026-07-29.** Flag removed, probe works unchanged, and the false comments are corrected in `scenario.yml`, `nightly-race.yml` and `ops-monitor-check.sh` | `hk-21v7c` |
| **The release-ready prompt ignores its own 30-minute cooldown.** `ops_monitor_check_test.sh` test 27e seeds the signal as alerted 6 minutes ago and expects suppression. It is sent anyway. Root cause **not** established — either it is matched against the 5-minute critical cooldown instead of the 30-minute one, or the cooldown key stops matching because the seeded key embeds the commit count. Two red assertions that were stepped over and untracked until 2026-07-29, which is the same shape as the masked tier above. | Pre-existing, confirmed by running the suite with all local changes stashed (313 passed, 2 failed, these two) | `hk-release-due-cooldown-chujb` |
| **`main`'s only required status check has failed on every run since 2026-07-17.** Branch protection requires exactly one check, `check (Tier 2)` from `ci.yml`. Five consecutive failures, latest 2026-07-22. This was invisible because the ops-monitor health probe read *the newest run of any workflow* on main — which is the Scenario tier reporting a masked success every day. | Surfaced 2026-07-29 by filtering that probe to the required check. `release_due` is gated on a green CI status, so it now correctly refuses to fire — do **not** widen the probe again to make it green | `hk-main-required-check-red-i14hq` |
| **About half the scenario tier skips in CI, and a skip reads as a pass.** Nothing installs `br` and nothing declares a twin build. Evidence: `internal/daemon` takes 69s in CI against 368s locally, and `TestThroughput_TenBeadsAtMaxFour` fails locally every time yet has never failed in 20 CI runs. | Means a **green** run on that workflow proves much less than it appears to — do not read one as the tier passing | `hk-ynohn` |

## The format check reports success when its tools are absent — found 2026-07-29

`scripts/go-format.sh` resolves its two tools with `gofumpt=${GOFUMPT:-"$repo_root/.tools/gofumpt"}` and
never checks that the path exists. Under `set -euo pipefail` a missing tool exits **127 with zero bytes
on stdout**, and bash writes its diagnostic to stderr only. Empty stdout is byte-identical to a clean
pass, so `bash scripts/go-format.sh check | tail -5` reports success in any caller that does not set
`pipefail`.

Reproduced directly: `GOFUMPT=/nonexistent/gofumpt bash scripts/go-format.sh check` exits 127 and prints
nothing to stdout. The same command piped through `tail` exits 0.

**Why it matters more than it looks.** `.tools/` is gitignored, so a fresh clone and every agent worktree
starts without it. Agents in this program run the format check and report "format check passed" from the
piped output. Three separate agents did so today in worktrees that had no `.tools/` at all. `make
fmt-check` and CI are unaffected — neither pipes, and CI runs `make tools` first — so this hides only
from the agents who report it most often.

Fix is a path-existence check that fails with a named error. **Until then, treat any bare "format check
passed" as unverified unless the exit code is shown.** This is the fourth green-signal-protecting-nothing
in this file, after the masked scenario tier, the `continue-on-error` flag that masked it, and
`make check-fast` skipping its own test step on a clean tree.

---

## Found 2026-07-29 while harvesting the three `workloop.go` comment blocks

None of these was chased. The first one is a live correctness defect and the most serious item in this
section. The rest are the same shape as the decay the harvest itself found: a pointer that was right
when it was written.

- **A stale-blocker sweep closes a bead on a bare commit-message match, with no evidence the work is
  present.** `autoCloseStaleBlockersOnClaimFailure` in `internal/daemon/scheduler.go` reaches
  `shared.MainHistoryHasRefsTrailer` for each candidate blocker and, on a match, closes that blocker
  through `SweepCloseBead`. Nothing else is checked. **This is the same false-close shape as the
  incident that removed the pre-dispatch check** (`hk-f38n`: bead `hk-cmry` closed wrongly, remaining
  work refiled as `hk-zmpd`), and it is live in the daemon today.
  Measured across all four production call sites of that primitive. Three pair the match with evidence
  that the work is genuinely absent, and only close when both agree: the no-change timeout in the run
  driver waits on `noChangeTimeoutCh`, `noCommitGuardShouldReopen` requires `curHeadSHA == parentSHA`,
  and the graph cascade requires `postHeadSHA == preHeadSHA`. This fourth site pairs it with nothing.
  It is worse than merely unpaired: the three good sites gather evidence about the same bead they then
  close, while this one reads the *dependent* bead's status and then closes a *different* bead — the
  blocker — about which it has no signal at all.
  It also directly contradicts the godoc now written on the primitive, which tells every caller to pair
  the match with work-absence evidence and never to use it as a standalone completion test. **Not
  fixed here:** `scheduler.go` belongs to another piece of work, and chasing defects is against the
  standing directive. This wants its own change.
- **`GenerateSandboxProfile` has no doc comment.** Its 19-line comment block in
  `internal/daemon/sandboxprofile.go` is separated from the function by the `worldSharedTempRoot`
  helper, so Go attaches it to nothing and the exported function godocs as bare. The comment holds the
  full `allowWrite` inventory and the world-shared-root rejection rule, so this is the most valuable
  detached comment in the file. Moving the helper above the comment fixes it.
- **Both `hk-l5saf` comments in `internal/daemon/scheduler.go` cite stale line numbers.** The hoisted
  guard comment cites "~line 1818" for the Step-2 split gate and "~line 3072" for the `localInFlight`
  increment. The post-stamp "no guard here" comment cites "~line 3072" as well. All three were
  `workloop.go` positions and none survived the Seam A split. `scheduler.go` is 2,371 lines, so 3072
  points past the end of the file. This is the exact failure the "cite symbols, not line numbers"
  convention exists to stop, and it appeared within one day of the split. Two unrelated "~line 1954"
  citations in the same file have the same problem.
- **`specs/execution-model.md` EM-063 Phase 2 and EM-064 tier 2 mandate a completion test that is
  known to produce false positives.** Both require `git log --grep "Refs: <bead_id>"` and read a match
  as "already landed" — EM-063 in the daemon's eager-refill pre-screen, EM-064 in the orchestrator's
  guard before it submits. `hk-f38n` measured that a bead worked in several parts leaves an older
  partial commit carrying the same ID, so the match fires while work is outstanding. The dispatch-time
  use of that grep was removed for exactly this reason. These two were never revisited. Recorded as a
  spec-drift item, not fixed: narrowing a normative test is an execution-model amendment and needs
  adjudication. Flagged in the new informative note under BI-022 in `specs/beads-integration.md` §4.7.
- **`make check-fast` runs no tests at all on a clean tree, and reads green.** Its final step derives
  the package list from `git diff --name-only HEAD`, so after a commit the list is empty and the step
  prints "no changed Go packages, skipping go test" and exits 0. The repo's own instruction is to run
  that gate *after* committing, which is precisely when the test step does nothing. So a green
  `check-fast` on a committed tree proves the build, the linters and the freeze gates, and proves
  nothing whatever about tests. Run `go test -short` directly against the packages you touched.
  This is the same pattern the program keeps finding — a green signal protecting nothing — and it is
  the third instance recorded in this file after the masked scenario tier and the `continue-on-error`
  reporting flag.

## One open P0 that is probably wrong

`hk-zobns` — *"Branch-protection deep guard fails open: bead merges to protected target and closes
approved."* Re-measured 2026-07-29: **the guard did not fail open.** The ref that moved was the
*unprotected* `integration` branch that the test's own bead body asks to land on; every assertion about
`main`, `origin/main` and main's reflog passed. The test's premise went stale on 2026-07-06 (`hk-lgykq`)
when merge-target resolution moved to the per-bead `lands_on` and became **stricter**.

The real cost is coverage, not safety: this was the only work-loop exercise of that backstop, so the
backstop has been unasserted for roughly three weeks. Annotated on the bead rather than re-scoped —
changing a P0's priority is the owner's call.

**Re-confirmed 2026-07-29 (late), and a warning about how to read the failure.** The repro was run again
and it does fail deterministically in about 4.4 seconds. Reading only the failure output leads to the
wrong conclusion — it says the `integration` ref moved, the bead closed, and the outcome was `approved`,
which reads exactly like a fail-open. The fixture settles it, and the test states it in its own setup
comments: the bead body sets `target_branch: integration`, the comment beside it says integration is
**NOT protected**, and the protected set passed to the run is `["main"]` alone. So the daemon merged to
an unprotected branch that the bead asked for. That is correct behavior.

The stale half is the next comment: *"the merge call uses deps.targetBranch (main), so the deep guard
fires."* Per-bead `lands_on` resolution replaced the daemon-wide target, so the merge targets
`integration` and the guard correctly does not fire. **Do not "fix" the product against this test.** Fix
the fixture — put the bead's own target in the protected set — or the backstop stays unasserted.

---

---

## The two that were root-caused, in detail

Both were diagnosed 2026-07-29. **In both cases the symptom in the original filing was misleading**, and
the real defect was worse. That is worth noting as a pattern in itself: the first-order reading of a
failing test in this codebase has now been wrong three times running.

### `hk-co8g8` — the escape guard fires on a sibling's half-finished merge

**Not a merge race. No commit is lost.** The losing run is killed *before* it ever attempts its merge;
its commit sits on `refs/heads/run/<id>` and simply never becomes an ancestor of the target. That
exonerates the whole merge apparatus — `resolveMergeTips`, the fast-forward re-validation, the CAS
rollback, the retry budget, the worktree lifecycle. None of them run for the losing bead.

`beadRunOne`'s single-mode tail runs `runmerge.CheckMainWorkingTreeDirty` — a bare
`git status --porcelain` on the project root — inside an escape-check slot in the merge exclusion domain.
Both the call site and the function's own doc claim that domain makes the check race-free.
**That invariant is dead.** `runmerge.RunBranchToTarget` splits the commit phase: Phase A
`commitAdvanceRef` (inside the domain) → **domain released** → Phase B `gitPushOrigin` (outside) → Phase C
`commitFinalizeWorkingTree` (inside). Between A and C the ref has advanced but the tree has not been
refreshed, so every merged path reads dirty — and the domain is free. A sibling's escape-check lands in
that gap and reports *the other bead's file* as this run's escape.

Proven by intervention, not inference: widening the window (300 ms before push) takes losses from 1 to 3
and all three losers name the same mid-push sibling's file; closing it (moving push and finalize inside
Phase A) passes 4/4 at N=5. Minimal repro is N=4 on shipped code with no instrumentation.

**Production exposure is larger than the test's**, because Phase B there is a network push to GitHub —
hundreds of milliseconds to seconds, against single-digit milliseconds locally. A hit reopens valid
reviewed work, re-runs the agent from scratch, and emits a durable event **accusing an innocent run**.
Past `implementer_escaped_worktree` events under concurrent dispatch should be re-read as suspect.

**This was predicted and refused, then done anyway.** The M3 design pass (`M3-D5`) said splitting these
"regresses the escape invariant"; the merge-queue design flagged it as an open reviewer challenge; the
relocation's own design doc said to keep the ref-advance *and* the tree reset inside. The implementation
split them, and `M4-C5` answered only the rollback half. The commit that deleted the old path-exclusion
heuristic *also pre-registered the fallback for exactly this contingency*.

**`RSM-018` in `specs/run-state-machine.md` is now unsatisfiable as written** — it mandates exclusion
against an interval that is no longer a critical section. It must be corrected in whatever change fixes
this. Fix direction: reinstate path exclusion with a **blob compare** against the pre-merge tip, which is
the pre-registered fallback and does not relitigate the push relocation.

No existing test can catch it: the concurrent-merge test binds a merge mutex that suppresses the window,
and the nearest regression test models the pre-split atomic sequence.

### `hk-nown4` — head-of-line blocking stalls a queue for five minutes

A sibling queue wins a duplicate-bead race → this queue's pre-claim guard sees `in_progress` and arms a
**five-minute** cooldown, deferring the item → the deferral is immediately reversed on the next tick
because the item has no blocking sibling → it lands back at index 0 → `SelectNextQueue` only ever offers
`Eligible[0]` → the cooldown guard `continue`s **with no fallback to the next eligible item**.

So the queue blocks on the one item it will refuse for five minutes and never looks at the items behind
it. It self-heals, which is why it reads as "the queue was slow" rather than as a stall. The same
`continue`-without-fallback shape also guards the greenlight gate.

The cooldown that made this a five-minute stall (rather than the previous 2.5-second spin) landed
**three weeks after** the test that exposes it was written — the test's 60-second budget is 5× too short.
**Do not fix this by shortening the cooldown**; that reverts a deliberate fix instead of supplying the
missing fallback.

### Six line-number citations inside `runWorkLoop` point at nothing

Found while pinning the admission-gate order (§3 Step 3 prerequisite). The comments in
`internal/daemon/scheduler.go` `runWorkLoop` cite six approximate line numbers, and **all six are wrong**.
The Seam A split moved the loop into a new file and every number stayed behind:

| The comment says | Where the thing is now |
|---|---|
| `beadRecord` construction "below (line ~1658)" | the `beadRecord = core.BeadRecord{` assignment |
| the post-claim `ShowBead` "at ~line 1954" (twice) | the queue-path label-hydration `ShowBead` |
| "The Step-2 split gate (~line 1818)" | the Step-2 split capacity gate |
| `localInFlight` "increment at ~line 3072" (twice) | `deps.localInFlight.Add(1)` |

Every one lands past the end of the function or in unrelated code. This is the exact rot the repo's
cite-symbols rule exists to stop, and the guidance it produces is now actively misleading: the hoisted
local-cap guard's safety argument rests on "localInFlight is not incremented until ~line 3072", so a
reader who checks that line finds no increment and cannot verify the claim. Cite the symbol.

### The admission-order constraints that no test pins, and why

`internal/daemon/admissionorder_test.go` now pins constraints 1, 2, 5 and 7 from DECOMPOSITION-MAP §3, the
second clause of 3, and half of 4 and 8. Constraint 9 has no real constraint to pin. What is left is
recorded here so the next reader does not spend the same time discovering it. **The numbers are the
plan's numbers.** Keep them aligned.

**This section carries no headline count, on purpose.** Three successive rounds of review found the count
wrong — once too low, twice stale after the list beneath it was corrected. A count is the one line most
likely to be quoted and the least likely to be re-derived, and it can disagree with the list two
paragraphs below it. A list cannot disagree with itself. So the state of each constraint is stated once,
where that constraint is discussed, and nowhere else. The map in
`internal/daemon/admissionorder_test.go` follows the same rule and is the other authority.

**Incompletely pinned: 4, 6 and 8.** Each is below. Constraint 3 is complete, and the story of how it was
nearly missed is worth keeping, because it is the reason this section stopped carrying a count.

An earlier version of this section said constraint 3 was fully pinned by
`l5saf_localonly_strand_test.go`, "re-checked for gaps and none found." That is wrong, because constraint
3 has TWO clauses and that test covers one:

- The guard's POSITION relative to the Phase-3 stamp. Pinned by that test.
- **`localInFlight` must not be incremented before the guard.** This is the hoist's own safety argument —
  the source comment says the pre-stamp read stays below `gateMax` through dispatch *because* the
  increment is post-claim. `l5saf` structurally cannot see it: it preloads the counter to `gateMax` with
  `gateMax = 1`, so the guard reads `1 >= 1`. Hoist `deps.localInFlight.Add(1)` above the guard and it
  reads `2 >= 1` — the SAME branch. The item stays pending and the test stays green. Its fixture sits on
  the saturated side of the boundary, so it cannot see the boundary move. Only two test files touch that
  seam and only `l5saf` drives the loop, so nothing in the tree pinned it.

  **Now pinned** by `TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount`, which sits one slot
  BELOW the cap, where the increment's position changes the answer.

The lesson is the same one §5 keeps teaching: a multi-clause constraint summarized as one line reads as
covered. Count the clauses, not the constraints.

**Constraint 4 — the write-lock hold is not observable through `runWorkLoop`.** The source comment says
the dedup check must run while the write lock is held so the winning queue's stamp is visible. Selection
and stamping run on ONE goroutine for the daemon's whole life, so two queues can never reach the stamp at
the same time whatever the lock does. The lock defends the stamp against the per-run goroutines that write
item status through `evaluateGroupAdvanceWithOutcome`, and no seam lets a test interleave one of those
with the stamp. The *outcome* is pinned; the lock boundary is not.

**Constraint 6 — `governor.tick` before the sentinel-queue gate cannot be reached from `daemon_test` at
all.** The gate is `loopMaintenance.sentinelBlocksDispatch`, which calls `movementGovernor.dispatchBlocked`
and returns false on a nil governor. `newMovementGovernorIfEnabled` builds one only when the
`movement_governor` subsystem is enabled AND `workLoopDeps.governorState` is non-nil. `governorState` has
no field on `WorkLoopDepsParams`, so no external test can construct a loop in which this gate can fire.

*The cheapest route, if someone wants it:* add `GovernorState` and `SentinelMode` to
`WorkLoopDepsParams`. That is a shared fixture 20-plus files bind, so it is a real edit, not a one-liner.
It does **not** require driving ACT mode. `dispatchBlocked` is only
`deps.decisionBlocker.IsQueueBlocked("sentinel")`, and `DecisionBlocker` is already an exported param with
an exported `AddQueueBlock`. So the governor needs to exist, not to trip — the trip can be injected.

*And one finding that shrinks the stake. This is a closed-world enumeration, not a survey of the
neighborhood.* The writer set for the sentinel block is provably complete:

- `DecisionBlocker`'s mutators are exactly `AddBeadBlock`, `AddQueueBlock` and `Acknowledge`.
- **No interface in the repo declares any of them.** The field is always the concrete
  `decisionBlocker *DecisionBlocker`, so there is no structural back door — nothing can substitute another
  implementation.
- The only non-test callers are `movementGovernor.onTrip` and `onClear`, both on the dispatch goroutine,
  plus boot-time `loadOneAckFile`. `LoadDecisionAckState` has exactly one caller, in `bootsocket.go`, and
  there is **no reload path**.
- `internal/sentinel` cannot reach `DecisionBlocker` at all: `internal/daemon` imports `internal/sentinel`,
  so the reverse import would be a cycle. That is why `sentinelAckRecord` duplicates the on-disk shape and
  the subject constant is spelled again in the daemon package — its own comment says so.

So in steady state there is one writer, on this goroutine, and nothing between the maintenance pass and
the gate writes the sentinel subject (the intervening work is the queue snapshot, the bootstrap, the
deferred re-evaluation, selection, the cooldown, handler-pause and decision-required).

**But note WHERE that writer sits, because it changes the conclusion's shape.** `governor.tick` is the LAST
statement of `tickBeforeSelect`. So a snapshot is equivalent to the live read only if it is taken AFTER
that call. Taken at the top of the pass, the daemon dispatches one extra bead on the tick a trip first
fires — the same one-tick-late hazard `loopmaintenance.go` already documents for `halt`, and documents as
deliberate there. The conclusion holds for the natural implementation, and it holds because of where the
snapshot sits, NOT because ordering is irrelevant.

`dispatchBlocked`'s own comment already says converting it to a snapshot is "arguable on its merits, not
obviously wrong". Read the constraint as protecting a code shape plus that one-tick edge, not a behavior
that changes today — which is why it stayed a gap rather than getting a test that would assert a
preference.

**Constraint 8 — half pinned, and the earlier reading of it was WRONG. Corrected here.**
An earlier version of this section claimed there were two no-sleep sites, that both drive the item
terminal first, that a merged variant would be "slower, never wrong", and that only a wall-clock flake
could test it. Every one of those four claims is false. Re-derived by classifying all 31 outer-loop
`continue` statements in `runWorkLoop`:

- **There are FIVE no-sleep sites, not two:** the queue bootstrap, the `hk-pina9` pre-claim `ShowBead`
  bound, the cross-queue duplicate, the `hk-6pspu` max-attempts **stamp** bound, and the `hk-n91y0`
  claim-blocked path. Twenty-six sites wait. The plan's own §3 item 8 says "twelve sites sleep" and names
  three no-sleep sites, so it undercounts on both sides.

  **`hk-6pspu` tags TWO sites** — the queue-path stamp bound, which does not sleep, and the br-ready skip
  bound, which does. Keep the word "stamp" or the bead tag alone points at both.

- **"26 sites sleep one poll interval" is loose, and the shape it hides is the one a merge would flatten.**
  One statement — the `continue` taken when `selectNextQueue` selects nothing — carries **three wait
  shapes**:

  1. a 2-second `workloopSleep` when deferred items remain;
  2. a 2-second wait that ALSO selects on the schedule wake channel, when an enabled scheduled job is
     loaded;
  3. `workloopIdleWait` with no timer at all, when neither holds.

  Shape 2 is bounded by a flat `time.After(workloopPollInterval)` — the same 2 seconds as shape 1, with no
  next-fire-time arithmetic. **Never write that it "waits until the next scheduled job time."** It does
  not compute one. Shape 3 is untimed but wake-interruptible: the daemon parks, it does not stall, and
  putting a timer there would restore the busy-poll PL-013 forbids.

  By TIMEOUT semantics there are only two shapes. Three appears only when you count select shape, and
  shape 2 is the one that disappears silently if a merge keeps only the timeout: a scheduled job would
  then wait for a queue-submit wake instead of its own channel.

  That same branch has a fourth outcome that is not a wait at all. When ZERO queues are loaded it falls
  through with neither a wait nor a `continue`, which is how the br-ready fallback is reached — and what
  the empty queue store in `TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause` relies on.

  So across the whole loop there are **four distinct delay outcomes** once immediate-continue is counted,
  not two.
- **The queue-bootstrap site does not terminalize anything** — its items stay pending. It is still sound,
  but for a different reason: that tick writes group→active, which flips its own `hasActiveGroup`
  predicate, so the next pass sees the active group and dispatches. The "they all drive the item
  terminal" reasoning does not cover it.
- **"Slower, never wrong" holds only for merging toward the SLEEPING variant.** Merging the other way
  busy-spins the `hk-403fw` cooldown — the exact `bead_claim_skipped` storm the cooldown was added to
  stop. The direction has to be stated or the conclusion is not usable.
- **It is not even slower in that direction, and a deterministic test exists.** Four of the five reach
  their `continue` through `evaluateGroupAdvanceWithOutcome`, which calls `queueStore.Wake()`
  unconditionally on the not-all-succeeded branch. `workloopSleep` selects on that same channel, so a
  sleep there returns at once: merging those four costs ZERO latency. The bootstrap site is the one
  exception — no `Wake()` fires there, so merging it would cost one poll interval on every queue submit.
  `WakeCh()` is exported, which makes the token a non-blocking-receive observable with no wall clock in
  it. `TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending` now asserts it.

What remains unpinned is only the busy-spin direction, and no test can reach a code shape that does not
exist in the tree.

### Three more found while classifying the loop's wait shapes

All pre-existing, none fixed, all recorded because each is cheap to trip over and expensive to diagnose.

**`workloopPollInterval`'s own comment is now false.** It says the constant "is NOT used for queue-loaded
idle states, which block indefinitely via `workloopIdleWait` per PL-013". Wait shapes 1 and 2 above are
queue-loaded idle states and both use exactly this constant. The comment describes shape 3 and presents it
as the only case. A reader who trusts it will conclude the daemon never re-polls with a queue loaded, which
is wrong for two of the three shapes, and PL-013 is cited in support of the wrong scope.

**The dependency-blocked detector is a bare substring match.** `runWorkLoop` decides whether a claim
failure means "the bead has open dependencies" with
`strings.Contains(claimErrStr, "cannot claim blocked issue") || strings.Contains(claimErrStr, "blocked")`.
The second clause subsumes the first, so the specific phrase is dead code, and ANY error text containing
the word "blocked" anywhere silently changes behavior: the queue item is driven terminal through
`evaluateGroupAdvanceWithOutcome` instead of being reverted to pending and retried. A `br` wording change,
a wrapped network error, or a path with "blocked" in it is enough. This is the movement-governor shape from
§5 — a coarse signal read as intent — applied to an error string. `internal/daemon/admissionorder_test.go`
dodges it deliberately and its fake error carries a DO-NOT-SIMPLIFY note, because every test there that
counts claims across ticks depends on the retry path.

**Enabling the FIRST scheduled job does not wake a parked daemon.** In wait shape 3 the loop blocks on
`workloopIdleWait`, which selects only on the queue-submit wake and shutdown — not on the schedule wake
channel. Shape 3 is reached precisely when no enabled job exists, so the job that would move the loop to
shape 2 is the one that cannot announce itself.

**The mechanism matters, and a first draft of this entry got it wrong.** `harmonik schedule enable` does
NOT fail to signal: `runScheduleEnableDisable` calls `Store.SetEnabled`, which routes through
`Store.mutate`, which calls `signalWake` — as eight store methods do. The signal is real. It just cannot
arrive, because **the CLI is a separate process, so its wake lands on its own in-memory channel and never
on the daemon's.** `WakeCh`'s own doc already hedges with "when the daemon shares the in-memory store",
which a separate CLI process does not. Do not record this as a missing call.

So the CLI's own header claim — "a running daemon
reloads the file on its next tick and picks up the change within one poll interval" — is false in this
state: there is no next tick until a queue submit or a restart. Once one enabled job exists the loop sits
in shape 2 and later edits are picked up within 2 seconds, so this bites exactly once per daemon life, at
the moment an operator first arms a schedule. `WakeCh`'s own doc already hedges with "when the daemon
shares the in-memory store", which the CLI does not.

### The string `"sentinel"` names two different things, and one of them is a real queue

Latent, not live. Recorded because it is cheap to trip over and expensive to diagnose.

`internal/sentinel/adversary.go` sets `AdversaryQueueName = "sentinel"` — the named queue the adversary
crew member binds to, so a queue with that literal name really exists once ACT mode spawns one.
`internal/daemon/decision_block_ev043a.go` sets `sentinelSubjectIDACT = "sentinel"` — the decision-block
SUBJECT the dispatch gate asks about. Two constants, two packages, one string, two unrelated meanings.

They live in different maps today, so nothing is broken: the dashboard forcing gate's `blockedQueues` is
keyed on queue NAME and consumed by `selectNextQueue`, while `IsQueueBlocked` is keyed on subject ID.
**No code writes across them.** The hazard is that both key spaces are `map[string]…` over the same
literal, so any future code that reads one with a key from the other silently type-checks. The concrete
shape to watch: write the decision-block subject into `blockedQueues` and the adversary's own queue is
withheld from dispatch — the sentinel would gag the crew it just spawned.

Related and worth knowing: this duplication exists because `internal/sentinel` cannot import
`internal/daemon` without a cycle, which is the same reason `sentinelAckRecord` re-declares the on-disk
ack shape. So the fix is not "share the constant" — there is nowhere shared to put it that does not mean
moving one of the two. Leave it until something needs it.

---

## The pattern worth carrying forward

Most of the launch-path items above are instances of one shape: **several code paths perform the same
conceptual step, and only one of them actually applies it.** The full catalogue — seventeen instances,
with a step-by-path matrix — is in `ONE-OF-N-DRIFT.md`.

(This sentence used to read "six of the eleven items above". The count went stale the moment entries were
added above it, which is the same failure the admission-order section now avoids by carrying no count.
`ONE-OF-N-DRIFT.md` owns the number.) The reason it matters for this program is that
the compiler is silent on every one of them: the callers still compile, the tests still pass, and the
guard simply stops being applied on four paths out of five.

That is also the argument for the launch-path collapse being the highest-value remaining move. It does
not fix these one at a time; it makes the class unable to recur, because one path cannot drift from
itself.
