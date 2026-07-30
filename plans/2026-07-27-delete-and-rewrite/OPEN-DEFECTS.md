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

## One open P0 that is probably wrong

`hk-zobns` — *"Branch-protection deep guard fails open: bead merges to protected target and closes
approved."* Re-measured 2026-07-29: **the guard did not fail open.** The ref that moved was the
*unprotected* `integration` branch that the test's own bead body asks to land on; every assertion about
`main`, `origin/main` and main's reflog passed. The test's premise went stale on 2026-07-06 (`hk-lgykq`)
when merge-target resolution moved to the per-bead `lands_on` and became **stricter**.

The real cost is coverage, not safety: this was the only work-loop exercise of that backstop, so the
backstop has been unasserted for roughly three weeks. Annotated on the bead rather than re-scoped —
changing a P0's priority is the owner's call.

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

---

## The pattern worth carrying forward

Six of the eleven items above are instances of one shape: **several code paths perform the same
conceptual step, and only one of them actually applies it.** The full catalogue — seventeen instances,
with a step-by-path matrix — is in `ONE-OF-N-DRIFT.md`. The reason it matters for this program is that
the compiler is silent on every one of them: the callers still compile, the tests still pass, and the
guard simply stops being applied on four paths out of five.

That is also the argument for the launch-path collapse being the highest-value remaining move. It does
not fix these one at a time; it makes the class unable to recur, because one path cannot drift from
itself.
