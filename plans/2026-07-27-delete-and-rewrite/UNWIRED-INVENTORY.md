# Partial-Feature Inventory — harmonik @ bba60dd37

Read-only audit. Target: production code that LOOKS implemented but is not reachable or not
finished. Dead code with zero callers anywhere is OUT of scope; this is work that was STARTED
AND NOT FINISHED. No compilation was performed; every claim is grep/read evidence, plus the
on-disk `events.jsonl` (~250k events) and `session-data.jsonl` (449 records) where cited.

> ## THE SCOPE LINE ABOVE WAS THE BIGGEST HOLE IN THIS AUDIT — 2026-08-04
>
> "Dead code with zero callers anywhere is OUT of scope" ruled out the class that has since caused
> the worst damage in this file. A writer with zero callers is not harmless. Its readers stay live,
> they read an empty store, and they conclude that the thing the store tracks does not exist.
> The reader is confident and it is wrong.
>
> **One commit produced two of these, and neither is visible to the audit as originally scoped.**
> Commit `e65ec5657` (2026-08-02) removed the imperative single-workflow tail from
> `internal/daemon/workloop.go`. The DOT-graph path that replaced it did not carry over two writes:
>
> 1. **`run.Write`** — the durable run-session registry under `.harmonik/runs/`, the store that lets
>    a bead-run survive a daemon restart. Now zero production callers. Five production files still
>    call `List` and `Remove`, so cross-restart run adoption is dead and
>    `strandedBeadHasOnDiskRun` is a fail-open guard that reads as fail-safe.
> 2. **`RunHandle.SetAgentType`** — the per-run harness identity. Now zero production callers. Its
>    live reader guards PI-073, a spec MUST that says a free-tier pi rate limit must never throttle
>    the paid Claude fleet. The comparison it makes can never be true.
>
> Both symbols carry a doc comment naming the caller that was deleted. Neither compiles differently,
> neither fails a test, and neither is findable by asking "is this feature finished?" — they were
> finished. See the new section **"Zero-caller writers and orphaned consumers"** below.
>
> > **REPAIRED, and this whole finding is now past tense. Read the dates.** The state described
> > above held between `e65ec5657` (2026-08-02) and `d7493e7c9`, which restored both writes and is
> > already in `HEAD`. `RunHandle.SetAgentType` has a production caller again at
> > `internal/daemon/agentlaunch.go`, and the run record is written again. Every present-tense
> > "now zero production callers" sentence in this document — here and in the sections below —
> > describes 2026-08-02 to 2026-08-06 and must be read that way. The finding is kept because the
> > CLASS is the point: a deleted call site leaves a writer that compiles, passes, and silently
> > stops feeding its readers. It was found by this audit and it was real.
>
> **Those two are not the whole of it.** A full pass over the 634 functions that no `main` can
> reach found **21 dead protections that something live still believes in** — a tmux orphan sweep
> that can never match a window and reports zero as clean, a `.gitignore` hygiene pass three
> launchers deleted their own copy of and handed to a dead function, an operator pause that does not
> survive a restart, an edge-guard wrapper the live cascade routes around, and a freeze list named
> `in_flight_at_pause` that freezes nothing. Section 4 below ranks them.
>
> **The scope line is hereby retired.** Zero-caller production code is IN scope for this audit, and
> the mirror case — a live consumer whose only producer was deleted — is the more dangerous half.
> The test that finds this class is not "is this feature finished?" It is **"does the reader of
> this signal have a producer?"**

> ## How far the tree has moved — re-pinned 2026-08-04
>
> The audit pin `bba60dd37` (2026-07-28, the merge that removed the MVH wording) is still an
> ancestor of HEAD. The tree has now moved **385 commits** since the pin, and **249 commits** since
> the 2026-07-30 staleness sweep.
>
> ```
> git merge-base --is-ancestor bba60dd37 HEAD; echo $?   # 0 — still an ancestor
> git rev-list --count bba60dd37..HEAD                   # 385  (was 122 on 2026-07-30)
> ```
>
> A second sweep on 2026-08-04 re-ran every live claim in this file at `51bd8aa84`. Results are
> written in place, same convention: old wording stays, a dated note says what moved.
>
> **What the 2026-08-04 sweep found.** Of the 38 open rows, **34 still hold**. One narrowed
> (row 5, queue transactions), one half-closed (row 38, `queue recover` landed), one got worse
> (row 36, `--codex-binary` now has zero readers anywhere rather than one wrong reader), and one
> tail claim is now false (`make build-twin-claude` is a real twin, no longer an alias). **Nothing
> in this file closed because someone wired it up.** Read a lack of a dated note as "still true".
>
> The largest single cause of movement is commit `e65ec5657` (2026-08-02), which removed about a
> thousand lines from `internal/daemon/workloop.go`. It closed no row here. It opened a new class —
> see the scope-hole box above.
>
> ### A method warning for anyone re-running this file
>
> **A Go-only grep understates config liveness.** `scripts/ops-monitor-check.sh` parses
> `.harmonik/config.yaml` directly with its own regexes, and the daemon schedules that script
> through `ensureOpsMonitorSchedule`. Three keys this file called inert are read there and nowhere
> in Go. Grep the shell and the Makefile before you call a config key dead.

> ## How far the tree has moved — measured 2026-07-30
>
> The audit pin `bba60dd37` (2026-07-28, the merge that removed the MVH wording) is still an
> ancestor of HEAD. The tree has moved **122 commits** since.
>
> ```
> git merge-base --is-ancestor bba60dd37 HEAD; echo $?   # 0 — still an ancestor
> git rev-list --count bba60dd37..HEAD                   # 122
> ```
>
> A staleness sweep on 2026-07-30 re-ran every claim in this file. The results are written in
> place. Each corrected claim keeps its old wording and gains a dated note, so you can see what
> changed. **Every number in the baseline table below moved.** Three whole findings closed
> because the code they name was deleted. The largest single cause is commit `3cec5afd7`, which
> retired review-loop mode and deleted its driver.

## Three corrections to the brief's premises — check these before scoping

**All three re-verified on 2026-07-30. All three still hold.** Line-number citations replaced with
symbol names, because the tree has moved 122 commits since the audit.

1. **`HandlerPauseController` is NOT unwired.** It is fully live daemon-side: constructed in
   `internal/daemon/bootstate.go`, persist-fn injected in `internal/daemon/bootsocket.go`, and
   threaded into the dispatch gate in `internal/daemon/bootworkloop.go`, which assigns
   `deps.handlerPauseController = cfg.HandlerPauseController`. What is unwired is the **operator
   surface** (findings 10, 11) and the **submit-time gate** (finding 11). The
   `dispatcher_backlog_held: (unavailable — HandlerPauseController not yet wired)` line in
   `cmd/harmonik/handler.go` is a **stale comment**, not a true statement, and it is still there.
   This is worse than the brief assumed, not better: the pause works, and the operator cannot see
   or clear it.
2. **`harmonik promote` really is landed**, both push-mode and PR-mode.
   `cmd/harmonik/promote_cmd.go` defines `runPromotePush` and `runPromotePR`, and
   `cmd/harmonik/main.go` dispatches `promote` to `runPromoteSubcommand`. AGENTS.md is accurate.
   Do not re-scope it.
3. **`sentinel.mode: act` really is read.** `seedGovernorDeps` in
   `internal/daemon/bootworkloop.go` copies the parsed mode into `workLoopDeps.sentinelMode`, and
   `internal/daemon/movementgovernor.go` switches on it into the act branch. An early sub-agent
   claim that it was write-only was refuted on direct inspection, and the path is unchanged.
   (Note the separate defect in the DANGEROUS table: that switch has **no `default` case**, so a
   mis-cased `"ACT"` silently does nothing.)

---

## Baseline measurements (independently computed, whole-repo)

> **Every open row of this document is still unwired, re-checked 2026-07-30. Nothing has been
> wired since the audit.** The rows that changed state are the ones already struck out, and each was
> struck out because it was wrong when written or because the code was deleted — not because someone
> connected it. Read a lack of strike-through as "still true", not as "not looked at".

**Re-measured 2026-07-30.** This audit is pinned to `bba60dd37`. That commit is an ancestor of the
current tip, and the tree has moved a long way since — `git rev-list --count bba60dd37..HEAD` gives
the distance. The package count fell because `3cec5afd7` retired review-loop mode and deleted
`internal/runloop/reviewcycle/` and `internal/runloop/continuity/` with it.

Each row now carries the command that produces it. Re-run the command rather than trusting the
number.

**Four rows were re-measured later on 2026-07-30, and all four had moved.** Production LOC, test LOC,
production file count and the bead-named file count all drifted between the two passes on the same
day. The package counts and the YAML-key count did not. The "Value @ HEAD" column below is the later
measurement and the earlier one is recorded beside it.

⚠ **Every `find`-based command in this table needs `-not -path './.claude/*'` and it did not have
it.** Agent worktrees now sit under `.claude/worktrees/`, and each one is a full checkout. Without
that exclusion the production-file count reads 8,916 instead of 897. The commands below carry the
exclusion. A number produced by the old command form is roughly ten times too large and should be
thrown away rather than reconciled.

| Measure | Value @ HEAD | earlier on 2026-07-30 | as audited | Command |
|---|---|---|---|---|
| Production Go LOC | **214,604** | 214,117 | 214,511 | `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -not -path './.claude/*' \| xargs cat \| wc -l` |
| Test Go LOC | **309,509** (1.44x production) | 307,741 | 300,627 (1.40x) | same, with `-name '*_test.go'` |
| Go packages | **102** | 102 | 113 | `go list ./... \| wc -l` |
| Packages import-reachable from any `main` | **67** | 67 | 67 | `go list -deps ./cmd/... \| grep '^github.com/gregberns/harmonik' \| sort -u \| wc -l` |
| Packages NOT import-reachable from `./cmd/...` | **35** | 35 | 46 | the two rows above, subtracted |
| Packages NOT import-reachable from ANY `main` | **32** | 32 | — | `comm -23` of `go list ./...` against `go list -deps` of every `main` package |
| Prod / test LOC in never-imported feature packages | **4,943 prod / 7,854 test** across all 9 (`hooksystem` 763/2,785, `replay` 1,091/741, `watch` 699/1,208, `structuredlog` 498/695, `twinparity` 662/702, `codexdigitaltwin` 449/262, `codexreactor` 340/388, `keepertwin` 427/272, `specaudit` 14/801) | same | 6,056 / 8,543 | `find <pkg> -name '*.go' -not -name '*_test.go' \| xargs cat \| wc -l` per package |
| Exported symbols with NO cross-package production reference | **1,337 / 3,162 (42.3%)** | same | 759 / 3,304 (23.0%) | see the method note below |
| Declared `EventType` constants not live outside the registry | **52 / 182 (28.6%)** | same | 41 / 182 (22.5%) | denominator `grep -cE '^\tEventType[A-Za-z0-9_]+' internal/core/eventtype.go`; numerator = constants with no whole-word match in a non-test file outside `eventtype.go`, `eventreg*.go`, `pertypecompat*.go`, `eventbus/busimpl.go` |
| `projectconfig` distinct YAML keys | **121** | 121 | ~122 | `grep -rhoE 'yaml:"[a-z_]+' internal/projectconfig/*.go \| sed 's/yaml:"//' \| sort -u \| wc -l` |
| Production `.go` files | **897** | 894 | 864 | `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -not -path './.claude/*' \| wc -l` |
| Production files named for a single bead ID | **130 / 897 (14.5%)** by the regex at right — NOT comparable to the audit, which did not record its pattern | 129 / 894 | 187 / 864 (22%) | `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -not -path './.claude/*' -exec basename {} \; \| grep -cE '_[a-z0-9]*[0-9][a-z0-9]*\.go$'` |

**Re-pinned 2026-08-04 at `51bd8aa84`.** Same commands, `-not -path './.claude/*'` included. The
tree grew, and the never-wired set did not shrink by even one package.

| Measure | 2026-08-04 | 2026-07-30 | direction |
|---|---|---|---|
| Production Go LOC | **220,966** | 214,604 | +6,362 |
| Test Go LOC | **330,190** (1.49x production) | 309,509 (1.44x) | test mass is growing faster than production |
| Production `.go` files | **917** | 897 | +20 |
| Go packages | **107** | 102 | +5 |
| Packages import-reachable from `./cmd/...` | **69** | 67 | +2 |
| Packages NOT import-reachable from ANY `main` | **32** | 32 | **unchanged** |
| …of which are real feature packages | **9** | 9 | **unchanged — the same nine** |
| `projectconfig` distinct YAML keys | **123** | 121 (miscounted) | see the key-count fix below |
| Production files named for a single bead ID | **128 / 917 (14.0%)** | 130 / 897 (14.5%) | flat |

The nine never-imported feature packages are still exactly `hooksystem`, `replay`, `watch`,
`structuredlog`, `twinparity`, `codexdigitaltwin`, `codexreactor`, `keepertwin` and `specaudit`.
**Nothing was wired and nothing was deleted in 249 commits.** The package count rose because new
packages were added, not because any of these were connected.

**Stronger evidence than grep is now available, and it should replace the exported-symbol row.**
`deadcode ./cmd/...` (golang.org/x/tools) does whole-program reachability from every `main`. It
reports **634 functions unreachable from any entry point** at `51bd8aa84`. That is a real call
graph, not a whole-word text match, so it does not miscount a symbol named only in a comment. The
concentration: `internal/core` 218, `internal/workspace` 69, `internal/lifecycle` 60,
`internal/daemon` 54, `internal/substrate` 23, `internal/codexdriver` 20,
`internal/handlercontract` 20. Re-run it rather than trusting the number.

**Method for the exported-symbol row (recomputed 2026-07-30).** The audit's 759 / 3,304 cannot be
reproduced, because it did not record how it counted. The figure above uses a stated, repeatable
method: collect every exported top-level `func`, method, `type`, `var` and `const` in a non-test
file as a (package-directory, symbol) pair, then mark a pair dead when the symbol gets no
whole-word match in any non-test file in a different directory. The method is conservative — a
symbol named only in a comment counts as referenced — so 42.3% is a floor. It was validated
against six symbols this file already proves unwired (`CheckWallClockOuterBound`,
`NewS02Registrar`, `TightestBudget`, `CheckBranchTipMonotonicity`,
`BuildConflictEscalationPayload`, `WriteLeaseReleasedMarker` — all six flagged) and two
known-live symbols (`queue.Persist`, `workspace.CreateWorktree` — neither flagged). **Retire the
23.0% headline.** The direction of the error is the point: the real figure is worse, not better.

**What the unreachable 32 are.** 13 `evaltasks/*` fixtures, 8 test-support packages (`codextest`,
`keepertest`, `runexectest`, `scenariotest`, `testhelpers`, `t5probe`, `t6probe`,
`workflow/scenario`), 2 `scratchpad` toys, and 9 real-feature packages: `hooksystem`, `replay`,
`watch`, `structuredlog`, `twinparity`, `codexdigitaltwin`, `codexreactor`, `keepertwin`,
`specaudit`. Only that last group is this document's subject.

---

## Ranked inventory — most consequential first

| # | Feature (what an operator/spec-reader thinks they have) | Symbol | What is missing | Evidence it is not live |
|---|---|---|---|---|
| ~~1~~ | ~~**Failing eval runs are not merged.**~~ **FIXED 2026-07-28 by `e424f6d64` (merged `ded3fe6bd`).** Re-read at HEAD 2026-07-30: `dotTerminalNodeIsSuccess` in `internal/daemon/dot_cascade_helpers.go` special-cases only WG-022's reserved pair (`close`, `close-needs-attention`) and otherwise reads the node's declared `terminal_disposition` attribute, failing closed with a named diagnostic when the graph does not say. The denylist is gone. The DANGEROUS table below already records this as fixed; this row was not updated to match. **Two 2026-07-30 measurements met here and one is stale.** The staleness sweep re-stated this row as still open, on the strength of the old single-literal denylist `terminalID != "close-needs-attention"`. That read is superseded — the denylist was already gone at HEAD when both were written, confirmed by reading `dotTerminalNodeIsSuccess` again during this merge (`grep -n 'terminal_disposition' internal/daemon/dot_cascade_helpers.go`). **The one durable finding from that sweep is a count, and it is kept:** the blast radius was overstated. `internal/daemon/moderesolve.go` routes on an exact label match, not a prefix, and the code comment says so. **15 beads carry `codename:eval`, not 55, and all 15 are closed.** The 55 swept in `codename:eval-program` (30), `codename:evalvol-remote` (12) and `codename:eval-harness` (5), which this path never routed. | — | — | — |
| 2 | **Policy, roles, gates, guards, budgets and hooks are enforced** — control-points.md line 1158: *"No requirement is deferred."* | `core.NoOpPolicyEngine` at `cmd/harmonik/main.go:961`; `ParsePolicyDocument`, `NewS02Registrar` | The engine is constructed then **discarded on the next line** (`_ = policyEngine`). `Evaluate` returns `{Permitted: true}` unconditionally; `Registry()` returns an empty map. | `ParsePolicyDocument` has **zero references repo-wide, including tests**. `NewS02Registrar` zero callers. Only `core.PolicyDocument` construction sites are 3 literals in `workflow/loader_test.go`. `gate_allowed/denied/escalated`, `guard_failed/reordered`, `control_points_registered` never emitted. |
| 3 | **The S05 hook system delivers side-effects at-least-once (at-most-once for non-idempotent).** CP-012..CP-017, CP-040, CP-042. | `internal/hooksystem` (763 prod / 2,785 test LOC) | Whole package. Also duplicated, equally uncalled, in `core/cp017_hook_cognition_s05.go`. | **Zero non-test importers** (verified). No delivery-receipt store exists (`rg 'deliveryReceipt'` → 0 non-test). `fireMechanismHook` emits and stops: `TODO(deferred): apply the side effect`. Live `events.jsonl`: **0** `hook_fired` / `hook_failed` across ~250k events. |
| 4 | **Budget and spend ceilings deny a dispatch that would exceed the limit** (ON-045/047/048, CP-022). | `CheckBudgetAtDispatch`, `TightestBudget`, `CheckWallClockOuterBound`, `newBudgetCounterState` | Any dispatch-time budget consultation. `budget_ref` is parsed into the AST, non-empty-checked, and dropped. | All zero callers outside `internal/core`; the four `hka8bg*` files have zero tests too. What runs instead: a per-day bytes/max-runs proxy (`spendmeter_hkk3f8g.go`) + a review-retry counter. `budget_warning` has no producer — first signal is the hard stop. |
| 5 | **Every queue mutation is crash-atomic** — QM-060: *"All queue mutations MUST execute through the single QueueStore transaction owner."* | `internal/queue/transaction.go` (1,005 lines) via `queuewiring.QueueStore.Transact` | **The dispatch reservation, since 2026-07-30** — `internal/daemon/scheduler_reservation.go` `reserveQueueItem`. The remaining `queue.Persist` sites in `scheduler.go` are still bare. | **Re-counted 2026-07-30 (staleness sweep).** `.Transact(` now has **2 non-test call sites, both in `internal/daemon/scheduler_reservation.go`** — `reserveQueueItem` and `failQueueItem` — plus 17 test sites. The earlier "10 sites, all in `queuewiring/store_transaction_test.go`" and "that is ONE caller" are both superseded. Live path is still bare `queue.Persist`: **6 calls in `internal/daemon/scheduler.go`** (not 10) and **0 in `workloop.go`**. Command: `grep -rn 'queue\.Persist(' --include='*.go' . \| grep -v _test.go`. That gives 18 external call sites across 9 files; adding the in-package callers in `internal/queue/persistence.go` and `internal/queue/rpc.go` makes **11 files**, which is the number the audit reported and it still holds. Callers include RPC handlers, startup recovery, the spend meter, crew start, eager fill, and `cmd/harmonik/run.go`. `Persist` is per-file atomic but has no generation guard, no replace-intent, no archive-handoff binding, no quarantine. `persistence.go` doc-trailers itself `QM-001` — the requirement it bypasses. **Update 2026-07-30:** the dispatch stamp now goes through `Transact`, and `Transact` gained a `Precondition` hook plus quarantine-on-any-I/O-failure so it satisfies all three QM-001 obligations. Wiring the remaining `Persist` callers is what closes this row. Anything that mutates a queue whose ID is not a canonical UUIDv7 will be REJECTED before I/O — that is spec-correct and it is what broke five fixtures here. **Re-measured 2026-08-04. The row narrows again and still does not close.** `Transact` now has **5** production call sites, up from 2: `reserveQueueItem` and `failQueueItem` in `internal/daemon/scheduler_reservation.go`, `commitDeferredReevaluation` in `internal/daemon/scheduler.go`, `transitionQueue` in `internal/queuewiring/operatorevents.go`, and `commitFailedRecovery` in `internal/queuewiring/store.go`. All five have live callers. Bare `queue.Persist` still has **15** external production call sites across 8 files, plus 5 in-package sites in `internal/queue/persistence.go` and `internal/queue/rpc.go`. `internal/daemon/scheduler.go` is down to 5 bare calls, and `internal/daemon/workloop.go` is still 0. |
| 6 | **Reconciliation verdicts are executed** — the daemon performs the action, emits `reconciliation_verdict_executed`, appends a trailer under a flock. RC-025a. | `daemon.ExecuteVerdict`, `internal/daemon/verdictexecutor_rc025a.go` | Any caller. | 8 call sites, all in its own `_test.go`. Sole non-test caller of `CheckVerdictStaleness`, `workspace.CaptureWIP`, `MarshalTransitionRecord`, and all of `internal/brcli/audit.go` — so those never execute either. `AcquireReconciliationLock`, `WriteVerdictAttemptAtomic`: zero callers. |
| 7 | **Worktrees take a lease lock** (WM-013a mutual exclusion); orphan sweep clears stale locks. | `workspace.WriteLeaseLockAtomic`, `WriteLeaseReleasedMarker` | Both writers. `CreateWorktree` is live and takes no lease. | Zero non-test callers. Consequences: `SweepStaleLeaseLocks.Removed` always empty → `RemoveStaleWorktrees` never runs → `daemon_orphan_sweep_completed.locks_cleared` permanently 0; every worktree lands in `NoLock` and GC falls back to an mtime heuristic. `ReleaseLeaseLock` **is** wired and unlinks without the mandated marker. |
| 8 | **Merge conflicts are re-dispatched to an implementer, capped at 3 attempts, then escalated** (WM-022..WM-024). | `ShouldDispatchConflictResolver`, `BuildConflictResolverLaunchSpec`, `BuildConflictEscalationPayload` | All of it. | All zero production callers. The one live call, `ValidateConflictResolutionAttemptCap` (`daemon.go:854`), is guarded by `if cfg.ConflictResolutionAttemptCap != 0` — a field **no caller ever sets**. `merge_conflict_escalation` never emitted. Live behaviour: `runmerge/merge.go` rebase-retries then fails the run. |
| 9 | **On restart the daemon discovers and resumes in-flight runs** (EM-031a); branch-tip monotonicity is verified (EM-024a). | `lifecycle.DiscoverActiveRuns`, `CheckBranchTipMonotonicity` | Any caller. | Zero production callers. `specs/execution-model.md` §7.4 pseudocode names the call sites explicitly; `workspace/orphansweep.go` carries a *comment* referencing the survive-check that was meant to exist. A force-pushed task branch is never detected. |
| 10 | **`harmonik handler status` / `resume` work** (HP-040, HP-060, HP-061). | `runHandlerResume`, `cmd/harmonik/handler.go` | A `handler-resume` RPC. Resume is pure file I/O; the daemon reads that file **only at startup** and overwrites it on next persist. | No pause/resume op in the socket router. **Plus a schema split-brain:** daemon writes `handlerStateSchemaVersion = 2` (`handlerpause_persist_m0k0a.go:354`), CLI rejects `> 1` (`handler.go:63,775`, exit 2) — the daemon file's own comment says "Matches ... in cmd/harmonik/handler.go" directly above `= 2`. First pause bricks both operator verbs. |
| 11 | **`queue submit` rejects beads bound to a paused handler** (HP-025 / QM-052a, RPC `-32018`). | `ValidationRequest.PauseChecker`, `internal/queue/validation.go:561` | The field is never assigned. | `rg 'PauseChecker\s*[:=]'` → 5 hits, **all in `validation_test.go`**. All three production sites omit it (`queue/rpc.go:190`, `:647`, `queue/append.go:101`). `internal/daemon/handlerpause_9hwbw.go:45` holds `var _ queue.HandlerPauseChecker = (*HandlerPauseController)(nil)` — a compile-time assertion that reads like wiring and connects nothing. |
| 12 | **Undeliverable events are captured and replayable**; consumer panics are logged (EV-011, 3 retries + backoff). | `core.NoopDeadLetterSink`, hardwired in `internal/eventbus/busimpl.go` | The real sink and the retry policy. `NewBusImplWithSink` — whose doc calls it "the preferred call site for daemon.Start" — has zero production callers, and there is no setter. | `Record` returns nil, so `recordDeadLetter`'s log never fires. `eventbus.DeadLetterReplay` ships and reads `dead-letters.jsonl`, a file nothing writes. `consumer_failed` / `dead_letter_enqueued` never emitted. `Subscription.OnPanic` is never referenced by `busimpl.go` at all. **Two corrections, 2026-07-30.** (a) The hardwiring is NOT in `internal/daemon/bootstate.go` — that file constructs the bus through `eventbus.NewBusImplWithWriterAndHWM`. All four `busimpl` constructors set `deadLetterSink: core.NoopDeadLetterSink{}` themselves. Same effect, wrong file. (b) "All five `handler.NewHandler` sites" is wrong: there is exactly **one** production site, `internal/daemon/agentlaunch.go`, plus 10 in tests. It does pass `handlercontract.NoopWatcherDeadLetter{}`, so the conclusion holds. |
| 13 | **Secrets are scrubbed from event payloads before they hit `events.jsonl`** (HC-031/HC-032). | `RedactionRegistry.RegisterPattern` | Every pattern. `bootstate.go:77` says so: *"No seed patterns — handlers call RegisterPattern when they are wired."* They never do. | Zero production callers; all six call sites are `_test.go`. `RedactionMiddleware` short-circuits to `RedactByFieldName` — a flat, **non-recursive**, top-level field-*name* match. A credential nested in an object or array is written verbatim. `redaction_failed` has no emitter. |
| 14 | **Stalled runs are detected and shown in the dashboard's bottleneck panel** (heartbeat-gap, review-stall, run-age). | `sentinel.DetectLayerA`, `internal/sentinel/layera_hkl087e.go` | Any caller. It is the sole producer of `stall_detected`. | Zero callers repo-wide (zero tests too). `dashboardgather.readActiveStalls` scans `events.jsonl` for an event nothing writes → always nil → `dashboard_cmd.go`'s `if len(ActiveStalls) > 0` unreachable. The `sentinel` package **is** live — but only its governor half. |
| 15 | **The operator dashboard shows curated lanes, throughput and gates.** | `internal/dashboard/store.go` `Write` | A writer. | `Write`/`Default` are test-only; `harmonik dashboard` has `--json|--unlock|--until|--lock` and **no set verb**; no skill/doc/spec mentions `dashboard.json`. All four curated sections are permanently empty, while `dashboardgate.go` treats absence as maximally stale and the keeper nags the operator to refresh it with a command that does not exist. |
| 16 | **`harmonik usage` reports real spend split productive vs orchestrator.** | `internal/usage/usage.go` `findOrchestratorSessions` | Project awareness and dedupe. | Hardcodes `-Users-$USER-github-harmonik`, ignoring `cfg.ProjectDir`; off-repo returns `nil,nil` → `Idle/Orchestrator: $0.0000 (0.0%)`, `ProductivePct` 100. Separately `knownSessionIDs := map[string]bool{}` is read and **never written** — `sessiondata.Record` has no session-id field — so daemon runs are billed twice. |
| 17 | **Token/cost accounting covers all harnesses.** | `internal/sessiondata/sessiondata.go` `readTranscript` | Codex/pi parsing (`if type != "assistant" { continue }`); `TokensTotal` is a value not a pointer, so absent data marshals as `0`, not `null`. | Live `session-data.jsonl`: **213/449 records all-zero, 63 null-cost, including 33 successful sonnet runs at `cost_usd: 0`**; `harness` empty on 448/449. The upstream data exists unread (`harness/codex/jsonlparser.go` already parses `codexTokenUsage`). The `WARNINGS (%d coverage gaps)` channel is only ever fed by a file-read error. **Re-verified 2026-08-04, and the empty-harness cause is now named.** `Record` gained a `Harness` field, so the schema is no longer the blocker. `internal/daemon/workloop.go` declares `sdHarness`, passes it to `sessiondata.Collect`, and never assigns it. Every live record still carries an empty harness because the variable is a zero value, not because the field is absent. |
| 18 | **`landing_strategy: squash\|cherry-pick` controls how work lands** (WM-019, written into every project by `harmonik init`). | `landTaskBranch`, `squashLanding`, `cherryPickLanding` | Any caller. | All zero non-test callers; `internal/runmerge/merge.go` contains no `squash` and no `cherry` — it is `git rebase` + `update-ref`. Parse, validate and three-tier resolve are all complete, so an invalid value is rejected and a valid one is ignored. Sibling keys `start_from`/`lands_on`/`protect_branches` **are** wired. |
| 19 | **Handler subprocess egress is restricted to a whitelist** (ON-025, HC-048b — the propagation half was never marked deferred). | — | The concept entirely. | `rg 'EgressWhitelist\|egress_whitelist' -g '!*_test.go'` → **0 hits**; `rg 'HC-048b' -g '!*_test.go'` → **0 hits**. `LaunchSpec` has no egress field. |
| 20 | **Skills declared on a node are resolved and provisioned** (HC-046..HC-050); `skills_provisioned` reports `rejected_skills[]`. | `handlercontract.ResolveSkill`, `ResolveAllSkills`; `LoadDotWorkflowWithPolicy` | Resolution on the live path. Both pre-exec paths pass `nil` skills. | Every real session emits a well-formed affirmative `skills_provisioned{skills: []}`. `internal/harness/claude/launchspec.go` holds `_ = ctx // reserved for future async steps (e.g. skill provisioning)`. `skills_resolved` has no producer — `core.EventTypeSkillsResolved` appears only at its own declaration, and `workflow.LoadDotWorkflowWithPolicy`, which builds the payload, has zero non-test callers. **Correction 2026-07-30:** "emitted only by the twins" is too strong. `handler.PreExecMessages` (`internal/handler/claudehandler_chb006_024.go`) does build and emit a `skills_provisioned` pre-exec message on the live path. It always carries `Skills: nil`, because both production callers — `internal/harness/claude/launchspec.go` and `internal/daemon/harnessregistry.go` — pass a literal `nil`. The finding stands; the mechanism is an empty affirmative, not an absent emitter. |
| 21 | **Forbidden `claude` flags are refused** (HC-055: seven flags MUST NOT be passed, *"would silently shadow policy"*). | `forbiddenClaudeFlags`, `internal/handler/claudehandler_chb006_024.go:53` | Four of seven — the list holds only the three CHB-007 entries. `--permission-mode bypassPermissions` in `HandlerArgs` is forwarded verbatim. | Direct read. HC-055b also diverged: `isHarmonikManagedWorktree` falls back to the **unresolved** path on `EvalSymlinks` failure and adds a `strings.Contains(canonWS, "/.harmonik/worktrees/")` fallback — any path containing that segment earns `--dangerously-skip-permissions`. **Re-verified 2026-08-04, and half of this row moved.** `forbiddenClaudeFlags` still holds only the three CHB-007 entries, and `CheckForbiddenFlags` does run on the live launch path, so the four missing flags are the whole of the first half. The HC-055b fallback moved file: `isHarmonikManagedWorktree` now lives in `internal/harness/claude/launchspec.go`. Both fail-open branches survive, and the segment match now also covers `.harmonik/crew-worktrees/`. **Drop the last sub-claim:** the `--permission-mode` flag literal no longer appears anywhere in the tree. |
| 22 | **A rate-limited handler auto-resumes when the window clears.** | `ClaudeCodeAdapter.Diagnose`, `internal/handler/adapter_claudecode.go` | A real health check — it returns `DiagnosticReport{Healthy: false}` **unconditionally**. | Production wires exactly this adapter (`bootsocket.go` `SetAdapter`). `handlerpause_autoresume_0otqs.go` does `if report, ok := c.runDiagnose(ctx); ok && !report.Healthy { return }` → **always abandoned**. The whole Schedule/flap-backoff/hysteresis machine is unreachable; a pause persists until manual operator action (which is finding 10). |
| 23 | **claude-code account rotation.** | `Adapter.RotateAccount` (claude/codex/pi impls) | Everything. Precisely: declared in the contract, never built, **never called** — every call site is `_test.go`. | claude → `ErrSingleAccountOnly` with `// TODO: when multi-account rotation lands...`; codex/pi → `ErrDeterministic`. Safe (fails loud) rather than fail-open. Its sibling `DetectRateLimit` is also uncalled for every harness — which strands the *working* claude retry-after parser (`adapter_claudecode.go:117-133`). |
| 24 | **Structured NDJSON logs from every subsystem, rotated at 100 MiB/24h** (ON-035, a MUST). | `internal/structuredlog` (498 prod LOC) | Adoption. | **Zero non-test importers.** 973 `log.Printf` / `fmt.Fprintf(os.Stderr, …)` sites in non-test code (`workloop.go` alone has 89). `.harmonik/logs/` is never written. |
| 25 | **Replay substrate** (`specs/replay-substrate.md`, written in runtime MUST language) and **the watch tier** (an always-on ledger + escalation engine; the `watch` skill ships in the binary). | `internal/replay` (1,091 LOC), `internal/watch` (699 LOC) | Any production importer; and for watch, a `harmonik watch` command. | Both have **zero non-test importers** (verified). There is no `watch` subcommand — only `resolve_watch_config.go`, whose `ResolveWatchTargets` and fail-loud boot gate `checkMissingWatchValues` are themselves test-only. The shipped skill instructs an agent to run something the binary cannot do. |
| 26 | **Resume-hang is bounded and recovered** (HC-070, HC-INV-008: driver emits ack on acceptance, stale on timeout). | `agent_input_acked` / `agent_input_stale` | Producers on **both** substrates. tmux: `SubmitInput` writes then `return Ack{Delivered}` with no timer. Codex: `codexdriver.Options.Emit` is never set at the composition root. | `grep -rn 'EventTypeAgentInput' --include='*.go' .` → **0 hits repo-wide** (re-verified 2026-07-30). Sharper than the audit put it: the constants do not exist at all. Registration in `internal/core/eventreg_hqwn59.go` uses bare string literals `"agent_input_acked"` / `"agent_input_stale"`. Types are registered and never published. `codexdriver` `Options.Emit` is read in `internal/codexdriver/session.go` but assigned only in `driver_test.go` — `cmd/harmonik/substrate_select.go` `codexSubstrateOptions` sets Args, Runner and Clock, never Emit. |
| 27 | **Daemon readiness contract** — `daemon_ready` after five PL-009 criteria; pre-ready requests rejected (PL-003b). | `lifecycle.ReadyCriteria`, `PreReadyGate`, `MarkReady`, `CheckRequest` | All of it. | Zero references outside their own file — **not even a test**. `daemon_ready` is registered in 4 places (`eventreg`, `busimpl` allow-list, `pertypecompat`, `eventtype.go`) and emitted nowhere. Verified. |
| 28 | **Agents report outcomes / claim beads over the daemon socket** (handler-contract.md builds a retry protocol on this). | `SocketHandlers.Request = &noopRequestHandler{}`, `bootsocket.go:300` | The implementation. `noopRequestHandler` is the **only** implementation in the repo. | Verified. `emit-outcome` and `claim-next` **are** registered in the router (`socketdispatch.go`) and reach a handler returning `errors.New("daemon: RequestHandler not wired yet")`. No client sends either op (27 distinct ops scanned). Both dereference `d.h` unguarded where every other op nil-checks. |
| 29 | **Machine-level agent ceiling** (ON-041c: env var, count file, `dispatch_deferred`, cross-daemon decrement). | — | Everything. | `rg 'HARMONIK_MACHINE_AGENT_CEILING\|machine-agent-count'` → **0 hits**. `DispatchDeferredReasonMachineCeilingExhausted` is a constant with no emitter. |
| 30 | **`kill -USR1 <pid>` resumes all paused handlers** (handler-pause.md §1.2). | `daemon.NewSignalResumeWatcher` | The goroutine. Its doc says *"Designed to run in a dedicated goroutine started by daemon.Start"*. | Zero callers. `signal.Notify(…SIGUSR1)` never runs, so the signal takes Go's default disposition. |
| 31 | **Agent manifests declare scheduled `triggers:`** (`.harmonik/agents/captain/manifest.yaml` has `{id: fleet-status, every: 6h, enabled: true}`); `harmonik agent check` reports ok. | `agentmanifest.Trigger`, `ActiveTriggers` | A scheduler. `ActiveTriggers` is filtered then **printed** — the markdown/TOON renderers are its entire lifecycle. | Also `validateManifest` never checks `Harness` against `claude\|codex\|pi` and never inspects `Cardinality` — so `cardinality: {max: 1}` does not make the captain a singleton and `harness: cladue` passes. Only `Markers.NeverEmits` and `Lifecycle.Persistent` have consumers. **Re-verified 2026-08-04. Still open.** `ActiveTriggers` has 5 non-test references and every one sits in `internal/agentmanifest/brief.go` — the filter loop, the `BootDoc` field, and the two renderers. Nothing schedules a trigger. One detail improved: `validateManifest` now rejects an EMPTY `harness`. It still does not check the value against the three known harnesses, so `harness: cladue` still passes, and `Cardinality` is named nowhere in validation. |
| 32 | **The structured Codex driver** (`codexdriver`, `codexinput`, `apptap`, `sessioncapture`, `codexreactor`). | `cmd/harmonik/substrate_select.go:105` | An activator. `if os.Getenv("HARMONIK_SUBSTRATE") != "codexdriver" { return tmuxSub, … }` and the **only** assignment of that var in the tree is in a `_test.go`. | Dormant by construction. `codexreactor.New()` — a finished, invariant-documented state machine — has **zero production callers**; the live output path bypasses it. NB `internal/harness/codex` (codex under tmux) is a *different* and genuinely live path. **Two small corrections, 2026-07-30.** `codexreactor.New()` is called at **8** test sites across two files (`internal/codexreactor/reactor_test.go` and `internal/codexdigitaltwin/twin_test.go`), not two. And the env gate is a predicate helper in `cmd/harmonik/substrate_select.go`, not one inline `if`. Both details; the conclusion is unchanged, and the only assignments of `HARMONIK_SUBSTRATE` in the tree are still in `_test.go`. |
| 33 | **Twin-parity gates prove the twins match the real harness.** | `internal/twinparity` `AssertStreamEquivalent` | Cross-comparison. `TestPiParityGate` passes `piSampleNDJSON(t)` as **both** twin and reference; two of three Claude sub-tests are `Assert(t, durable, durable)`. | Signature takes `testing.TB` — production reachability impossible. `make test-twin-parity-pi` asserts nothing about twin-vs-real. Fixtures carry `hand_authored: true`, `capture_date: "PLACEHOLDER-REAL-BOX"`. |
| ~~34~~ | ~~**The composition-root wiring audit catches silent drops between versions**~~ **FIXED** (`HARMONIK_DEBUG_WIRING=1`) | `bootState.wiringAudit`, `internal/daemon/wiringlog.go` | Was the derivation: a hand-maintained 37-entry `[]wiringEntry` constant that printed identically no matter what was wired, with 0 of its 35 `daemon.go:NNN` call sites still landing on the code they named (the wiring had moved to `bootstate/bootsocket/bootworkloop`). The table and its zero-consumer `ExportedCompositionRootWirings` seam are deleted; the audit now reflects over the live `bootState` and reports each of the 19 singletons as constructed or ABSENT, so a dropped singleton shows up without anyone editing the file. **Scope narrowed, deliberately:** it detects dropped *constructed values* only — a dropped `bus.Subscribe`, `StartWatcher` call, or `workLoopDeps` field is a wiring action, not a singleton, and is still undetected. |
| 35 | **Socket bind failure is reported** (PL-003 defines exit 6 for a live-daemon collision). | `startSocketListener`, `internal/daemon/bootsocket.go:297-312` | Any handling: `go func() { <-socketDone }() // drain: non-fatal`. | The daemon then runs the work loop with **no socket** — `queue submit`, `crew start`, `state`, `dashboard`, `comms`, hook-relay all silently unavailable, zero diagnostic. **Re-verified 2026-08-04. Still open, and narrowed by one case.** `startSocketListener` still drops the bind error into `go func() { <-socketDone }() // drain: non-fatal`. One diagnostic was added but it does not cover this row: `bindSocket` now calls `lifecycle.ValidateSocketPathLength` and warns before the bind when the path is too long. A bind that fails for any other reason, including the live-daemon collision that PL-003 defines exit 6 for, is still silent. |
| 36 | **`--codex-binary` selects the codex executable** (its godoc: *"used when the resolved harness is core.AgentTypeCodex"*). | `daemon.Config.CodexBinary` | A reader on the documented path. `harnessregistry.go:57` hardcodes `codex.NewHarness("", "")` → bare `codex` off PATH. | The flag *does* reach the dormant codexdriver substrate (finding 32), so it appears wired. The one path it reaches is the one its docs don't describe. **Re-verified 2026-08-04, and it got worse.** `daemon.Config.CodexBinary` now has **zero** readers anywhere, not one reader on the wrong path. `internal/daemon/harnessregistry.go` still builds `codex.NewHarness("", "")`. The flag reaches only `selectSubstrate` and `codexSubstrateOptions`, and the dormant-substrate gate keeps both off. The field godoc still promises the codex harness path. |
| 37 | **Crew slots are reclaimed when a crew goes idle.** | `crewrun.CrewIdleReaper.StartWatcher` | The watcher body — it is an empty function; `loop`/`scan`/`checkCrew`/`reap` are `//nolint:unused`. | Constructed fully at `bootsocket.go:252` and started at `bootworkloop.go:238`. A documented 2026-07-18 operator disable, but the consequence stands. |
| 38 | **`harmonik queue resume` recovers a paused queue.** | `queuewiring.transitionToActive` | Handling for 2 of 3 pause states. `transitionToActive` skips anything that is not `paused-by-drain`. | `paused-by-failure` (`internal/daemon/scheduler.go`, `internal/lifecycle/startup_pl005_qm002.go`) and `paused-by-budget` (`internal/daemon/perqueuespendmeter_tigaf11.go`) are both produced live. `queue.ResumeFromFailure` / `RearmFailedItems` have zero callers and there is no `queue retry` verb. `paused-by-budget` stays wedged until UTC-day rollover. **Correction 2026-07-30:** "`HandleOperatorResume` returns nil regardless" is FALSE. `OperatorPauseController.HandleOperatorResume` (`internal/daemon/operatorpause.go`) returns wrapped errors from `json.Marshal` and from `bus.Emit`; it returns an early nil only for the idempotent already-not-paused case. The effect the row is reaching for survives by a different route: the `operator_resuming` event it emits is dropped downstream by the drain-only filter in `transitionToActive`, so the socket still answers OK on a queue that stays paused. **HALF CLOSED 2026-08-04 — `paused-by-failure` now has an operator way out.** `harmonik queue recover` is a real verb. `cmd/harmonik/main.go` routes `recover` to `queuecli.RunQueueRecover`, which sends the `queue-recover` socket op, which `internal/daemon/socketdispatch.go` hands to `QueueRecoveryController` and `queuewiring.QueueStore.RecoverFailed`. That path reaches `queue.PrepareFailedRecovery`, which calls both `queue.ResumeFromFailure` and `RearmFailedItems`. **So "zero callers and no retry verb" is now FALSE — strike it.** What survives, and is what this row should now be scoped to: `transitionToActive` still clears only `paused-by-drain`, and `paused-by-budget` still un-wedges only at the UTC-day rollover in `internal/daemon/perqueuespendmeter_tigaf11.go`. Scope this row as ONE state, not three. |
| ~~39~~ | ~~**Review-cycle and continuity kernels**~~ **CLOSED — the code is deleted** | ~~`internal/runloop/reviewcycle` (652), `internal/runloop/continuity` (475)~~ | — | The audit was right that both packages had zero importers of any kind. They were harvested from an abandoned branch in `1b56dafb5` on 2026-07-28 and knowingly parked. **Commit `3cec5afd7` ("daemon: retire review-loop mode and delete its driver") deleted both directories**, together with `internal/daemon/reviewloop.go`. Verify with `ls internal/runloop/reviewcycle internal/runloop/continuity` → no such directory. Nothing to scope. Harvested and deleted the same day, so this row was stale when it was written. Its lesson is worth keeping: harvesting a kernel from an abandoned branch does not make it wired, and a "knowingly parked" note is not a plan. |
| 41 | **A graph can gate on a policy ControlPoint** — `specs/workflow-graph.md` WG-001 declares `gate` as one of four node types, WG-005 gives it an attribute set, and `internal/daemon/dot_gate.go` implements both a mechanism evaluator and a cognition evaluator that launches a real agent. **Found 2026-07-30; this row is new.** | `dispatchDotGateNode`, `buildMechanismGateEval`, `buildCognitionGateEval`, `executeCognitionGate` — `internal/daemon/dot_gate.go` (749 lines) | Two things, either of which alone makes it dead. `daemon.Config.CPRegistry` is never assigned: `grep -rnE 'CPRegistry[[:space:]]*[:=]' --include='*.go' .` returns **zero hits**, tests included, so `daemonGate.LookupGate` always reports "no registry loaded" and any gate node returns a structural eval-failure before a launch. **Use that command, not a bare `grep CPRegistry`** — the bare form returns 7 hits, including the field declaration in `daemon.go` and the read in `workloop.go`. The field is declared and read. Nothing writes it. And no graph the daemon runs asks for one. | `type="gate"` appears **zero** times in the embedded `internal/daemon/standard-bead.dot`, in this project's `workflow.dot`, in `sonnet-triple-review.dot` and in `eval-bead.dot`. It is not absent from the tree — `specs/examples/quality-gate-policy.dot` declares one, and no run path loads it. `dot_gate.go`'s own source says so about the remote path: *"the default workflow.dot uses a tool-command commit_gate, not a cognition gate, so no live remote run exercises this path today."* Real gating is done by the `commit_gate` **shell tool node**, which is a `non-agentic` node and does not touch this file. **Consequence for the rewrite:** counting `dot_gate.go` as a live launch path inflates the duplication count and prices migration work that buys nothing. Wire it or delete it — decide, do not migrate it by default. |
| 40 | **`harmonik harness` runs the conformance scenario suite.** | `cmd/harmonik/harness.go` | `BrPath`/`KerfPath` — no flag supplies them, and `bootworkloop.go:30` is `if bs.cfg.BrPath == "" { return nil }`, which skips the entire PL-005 work loop and is the last statement of `daemon.Start`. | The registered conformance command boots a daemon that never dispatches, while `scenarios/smoke/checkpoint-and-merge.yaml` asserts daemon-side events only the work loop can produce. |

Also confirmed, lower severity: `LaunchSpec.HandlerSpec` is assigned only in tests, so `runIDStr`
falls back to the constant `"unknown"` for every live handler FSM; `VerifyTwinLaunch` (HC-045
commit-hash pin) is never called; `handler-pause` HP-015 counter never resets on resume; HC-056's
default is 5× the spec (150s vs 30s, with a comment asserting it *is* the spec default); HC-004
launch idempotency is marked "NOT ENFORCED" in source while the spec still says MUST;
`workspace/doc.go` still claims the package "contains only test files" against 7.8k prod LOC;
`make build-twin-claude` is an alias for `build-twin-generic`; `internal/scratchpad` ships toy
exercises (anagram, roman numerals) inside `internal/`.

**Re-verified 2026-08-04. Seven of the eight still hold. One is now false.** `make
build-twin-claude` is NO LONGER an alias — strike it. The old twin was renamed to
`harmonik-twin-generic`, and `cmd/harmonik-twin-claude` is now a real 7,984-line Claude lifecycle
twin. Two claims sharpened. The HC-056 default moved file and got a second wrong witness: it is
`DefaultAgentReadyTimeout = 150 * time.Second` in `internal/runlaunch/deadlines.go`, the comment
asserting 30s moved to `internal/runlaunch/events.go`, and `cmd/harmonik/main.go` now disagrees
with itself — its comment says 90s and its flag help says 150s, against HC-056's 30s. And
`internal/workspace/doc.go` now says "the only non-test Go file is this doc.go" against **32**
production files and 7,782 production lines.

---

## Zero-caller writers and orphaned consumers — the class this audit originally excluded

**Added 2026-08-04.** The scope line at the top of this file ruled out "dead code with zero callers
anywhere". That was a mistake, and this section is what the exclusion was hiding.

A writer with zero callers is not the same thing as unused code. Its readers are still live. They
run, they read an empty store, and they take the empty answer as fact. The reader is not broken and
it is not skipped — it is CONFIDENT AND WRONG. That is worse than an absent feature, and it is
invisible to every test, because a test that sets up its own fixture writes the store itself.

### 1. The run-session registry is never written. Cross-restart run adoption is dead. — CONFIDENT

`internal/run/registry.go` is the durable per-run store under `.harmonik/runs/<runID>.json`. Its own
package doc says what it is for: *"so the daemon can discover and adopt surviving sessions after a
SIGKILL restart."*

**`run.Write` has zero production callers.** Commit `e65ec5657` (2026-08-02, "Remove the imperative
single-workflow tail") deleted the only one, which lived in `internal/daemon/workloop.go`. The
DOT-graph path that replaced the tail never put the write back.

> **Past tense as of 2026-08-06.** `d7493e7c9` restored the write and is in `HEAD`. This section
> describes 2026-08-02 to 2026-08-06 and is kept for the class of defect, not the live state.

```
grep -rn 'runpkg\.[A-Z]' --include='*.go' . | grep -v _test.go
```

That returns nine non-test lines. Seven are calls — five `List` and two `Remove`. The other two are
a type reference and a comment. **Not one is a `Write`.** So the directory is always empty and
every reader concludes "no run is live". Five consumers:

| Consumer | File | What it concludes | Direction |
|---|---|---|---|
| `adoptDeadRunSessions` | `internal/daemon/run_session_adoption.go` | Returns at once on `len(recs) == 0`. **A bead whose tmux session died across a daemon restart is never reset**, so it stays `in_progress` and QM-002a never reverts its queue item. | feature gone |
| `adoptLiveRunSession` (reached from `internal/daemon/scheduler.go`) | `internal/daemon/scheduler.go` | Never adopts a run that outlived the daemon. The tmux session leaks and the run is abandoned. This is EM-031a, and row 9 above records the other half of it. | feature gone |
| `strandedBeadHasOnDiskRun` | `internal/daemon/scheduler.go` | **The inverted protection.** Its doc says an on-disk record means a live monitoring goroutine exists, so the stranded-bead auto-reset must skip and not race it. Its error branch returns `true` to be race-conservative. Its success branch now always returns `false`. The guard reads as live, is careful in the branch that never runs, and protects nothing in the branch that always runs. | **fail-open** |
| `reconcileInFlightRuns` | `internal/daemon/bootreconcile.go` | Builds `liveRunBeadIDs` to EXCLUDE genuinely-live runs from the boot orphan reconcile. The set is always empty, so nothing is ever excluded. | fail-open |
| `probeRunProcessDead` | `internal/daemon/bootworkloop.go` | Always returns `false`. Its comment says "any lookup error → false (never a spurious reap)", so it fails safe, but the stale-watcher reap can now never confirm a dead process. | fail-safe |

A sixth consumer, `sentinel.ComputeSnapshot` in `internal/sentinel/signals.go`, takes
`activeRuns []run.Record` and has zero production callers of its own. It is dead code that reads a
dead store, so it does no harm today. It is worth naming because it shows the reach: the run
registry was meant to feed stall detection as well as restart adoption.

**Why this is the exact shape the scope line hid.** Every one of those five consumers is live,
tested, and correct in isolation. The defect is not in any of them. It is in a function nobody
calls, and the original scope rule sent the auditor past it by definition.

**Do not fix this by re-adding the write without deciding the contract first.** Two of the five
consumers guard against a race with `adoptLiveRunSession`. Restoring the writer restores the
hazard at the same moment it restores the guard, and `strandedBeadHasOnDiskRun` has never once run
against a non-empty registry in production.

### 2. The per-run agent type is never recorded, so the PI-073 guard cannot fire — CONFIDENT that the guard is vacuous, UNSURE that the hazard is live today

The same commit, the same shape, a different victim. `RunHandle.SetAgentType` in
`internal/daemon/runregistry.go` carries its own wiring instruction in its doc comment:

> *"SetAgentType stores the resolved agent type for this run. **Called by beadRunOne** after the
> launchSpecBuilder resolves the harness."*

`e65ec5657` deleted that call with the rest of the imperative tail. **`SetAgentType` now has zero
production callers.** Only two tests call it. So `GetAgentType()` always returns the empty
`core.AgentType("")`.

> **Past tense as of 2026-08-06.** `d7493e7c9` restored the call and is in `HEAD`:
> `internal/daemon/agentlaunch.go` calls `SetAgentType` again. This section describes
> 2026-08-02 to 2026-08-06 and is kept for the class of defect, not the live state.

The live reader is the bandwidth tuner in `internal/daemon/bandwidthtuner.go`, and it is guarding a
spec MUST:

> *"PI-073: Pi rate-limit events MUST NOT reach the global tuner — a free-tier Pi 429 must never
> throttle the paid Claude fleet."*

The guard is `if h.GetAgentType() == core.AgentTypePi { return nil }`. That comparison can never be
true, so **PI-073 is enforced nowhere.**

**What makes this one instructive is the comment right above it.** The author documented a
deliberate fail-open for two cases — registry nil, or run not found — and called it a "safe default
for non-Pi harnesses". A third case now exists that the author did not anticipate: registry wired,
run found, type EMPTY. It takes the same fall-through. The reasoned exception grew a silent third
member, and the reasoning still reads as complete.

**Where to be careful, and why this is marked UNSURE.** The only emitter of
`agent_rate_limit_status` is `internal/daemon/hookrelay_chb025.go`, which is the Claude hook
bridge. Whether a pi run can reach that emitter today is NOT established here. The tuner also only
runs when the operator passes `--subscription-token-ceiling`. So the protection is provably
vacuous, and the hazard may or may not be reachable. **Do not close this by arguing the hazard is
unreachable** — that argument depends on the Claude hook bridge staying Claude-only, which is not
what PI-073 relies on.

This also corrects a claim made elsewhere in this sweep: `RunHandle.SetAgentType` does NOT record
the per-run type. It records nothing.

### 3. Struct fields that live code reads and nothing ever assigns — CONFIDENT

These read as configuration. They are always the zero value, so the branch they gate never opens.
The pattern is worse than an unread key, because the field IS read — a grep on the identifier is
reassuring and wrong.

- `daemon.Config.CPRegistry` — declared, read in the work loop, **zero assignments repo-wide
  including tests**. Every gate node fails structurally. Already recorded as row 41.
- `daemon.Config.ConflictResolutionAttemptCap` — **zero assignments repo-wide**, so the live
  `if cfg.ConflictResolutionAttemptCap != 0` guard in `internal/daemon/daemon.go` never opens and
  `ValidateConflictResolutionAttemptCap` never runs. Already recorded as row 8.
- `sdHarness` in `internal/daemon/workloop.go` — declared, passed to `sessiondata.Collect`, never
  assigned. This is the named cause of the empty `harness` field on 448 of 449 cost records in
  row 17. The schema was fixed. The producer was not.
- `queue.ValidationRequest.PauseChecker` — the only non-test mention is a compile-time interface
  assertion. Already recorded as row 11.

**A negative result worth recording, so nobody re-runs this search.** All 40 fields of
`daemon.Config` were checked for "read in production, assigned nowhere in production". Only four
qualify. Two are the known defects above. The other two are benign and the source says so:

- `HandlerEnv` — never assigned, and `internal/daemon/dot_cascade_helpers.go` and
  `internal/harness/pi/launchspec.go` both state in-source that it is nil in production.
  `internal/daemon/bootworkloop.go` prepends `lifecycle.ProvenanceEnvVar` itself. **The field's own
  doc is nevertheless wrong** — it says "Production callers MUST inject at minimum
  HARMONIK_PROJECT_HASH", and no production caller does. The boot code compensates. Fix the doc,
  not the code.
- `ReconciliationScanCadence` — never assigned, so the RC-020a scan always falls back to the
  one-hour `ReconciliationScanCadenceDefault`. The field doc states that fallback, so the behaviour
  is intended. The only real consequence is that an operator cannot change the cadence.

So the composition root is cleaner than this document's tone implies. The rot is not spread evenly
across the config surface — it is concentrated in the two fields that gate a protection.

### 4. Nineteen more dead protections, ranked — every one is NEW

A second independent pass over the 634 unreachable functions. Findings 1 and 2 above are excluded
because they are already stated. Each row names the protection, what it fails to do, and — the part
that matters — **the live code, comment or shipped document that believes the protection is in
force.** Confidence is stated per row.

| # | Symbol / path | What silently does not happen | What points the wrong way | Mode | Sure? |
|---|---|---|---|---|---|
| 3 | `WindowName` — `internal/lifecycle/tmux/windowname.go` | No tmux window is ever named with the `hk-<hash6>-` sentinel. `launchViaSubstrate` names the window after the raw worktree path instead. | The live `SweepOrphanTmuxWindows` matches ONLY that sentinel, so it can never kill anything. Its always-zero count ships as `tmux_windows_killed` on `daemon_orphan_sweep_completed` and **reads as clean**. A comment in `internal/daemon/tmuxsubstrate.go` says its fixed name "avoids the `hk-<hash6>-` orphan-window sweep" — a sweep that cannot fire. That fixed name is the project-wide layout convention used at five other sites, so read the comment as a stale rationale, not as a bespoke dodge. | fail-open | CONFIDENT |
| 4 | `EnsureGitignoreHygiene` — `internal/workspace/gitignorehygiene.go` | The WM-013e control-plane ignore set is never added to the repo-root `.gitignore` at startup. | `CommitResidualDelta` in `internal/runmerge/worktreestate.go` reasons out loud that "`git add -A` HONORS .gitignore, and this repo's .gitignore excludes every class of daemon/runtime junk" — the exact assumption whose enforcement is dead. Three launchers DELETED their own per-launch ignore edit and handed the duty here. An un-ignored daemon file now reads as an implementer escape to the rebase guard. | fail-open | CONFIDENT |
| 5 | `StateMarkerPath` — `internal/lifecycle/daemonpaths.go` | Nothing writes or reads `.harmonik/daemon.state`, so a global operator pause lives only in memory and dies with the process. | The live br-ready auto-pull gate in `internal/daemon/scheduler.go` reads `operatorPauseCtrl.IsPaused()`, which is `false` after every supervisor revive — the unsafe branch the marker exists to prevent. PL-005 says the daemon MUST start in the persisted paused state. | fail-open | CONFIDENT |
| 6 | `ValidateAndApplyContextUpdates` — `internal/workflow/context_updates.go` | The HC-062 registered-key filter never runs, so an unregistered context key is never dropped. | The live writer `core.ApplyContextUpdates` copies EVERY key into `run.Context`, and edge guard expressions read `run.Context` — so a handler can steer routing with a key the workflow never declared. The DOT parser still reads `context_keys`, so an author believes a filter exists. | fail-open | CONFIDENT |
| 7 | `DispatchEdge`, `IdentityGuard`, `PermitGate` — `internal/core/edgecascade_em042.go` | No guard is ever applied to a candidate edge. The live cascade calls `core.SelectNextEdge` directly from `internal/workflow/dispatcher.go` and skips the only wrapper that invokes a guard. | The DOT parser stores `guard_ref` and **no consumer resolves it** — `internal/workflow/params_graph.go` dereferences it for bulk template substitution, which is not resolution. **WG-026 (Reference resolution)** says a loader MUST resolve it and fail on an unresolved reference. WG-031 is a different requirement and an earlier draft cited it by mistake. Two conformance tests certify a guard no binary calls. | fail-open | CONFIDENT |
| 8 | `ValidateSubWorkflowAcyclicity` — `internal/workflow/sub_workflow.go` | The accumulating cycle check never runs, so no reference graph is carried across nesting levels. | The live replacement `checkSubWorkflowAcyclicity` rebuilds a FRESH two-level graph per dispatch, and its own comment wrongly claims deeper cycles get caught on recursion. A three-node cycle A→B→C→A is invisible at every level, and there is no depth counter. SW-003 says this MUST fail closed. | fail-open | CONFIDENT |
| 9 | `emitImplPresence` — `internal/daemon/workloop.go` | The daemon never emits a presence beat for a run it spawns, so the `<beadID>-impl` identity never exists. | `checkCommsNameConflict` returns no warning for an identity with no presence entry, so the two-sessions-one-identity alarm can never fire. `comms who` and the digest's `comms_who` section omit every running implementer. **Narrower than an earlier draft said:** the same digest still carries `in_progress_beads`, `crews` and `tmux_fleet`, so a captain is not left blind — it is left with one channel that lies by omission. | fail-open | CONFIDENT |
| 10 | `NewSignalResumeWatcher` — `internal/daemon/handlerpause_sigusr1_bdvae.go` | Nothing calls `signal.Notify` for SIGUSR1, so `kill -USR1 <daemon pid>` does nothing at all. | **Nothing believes in it, which is why this row is ranked low, not high.** `specs/handler-pause.md` lists SIGUSR1 under §1.2 *Out of scope* as deferred, the design doc that mentions it is marked SUPERSEDED and non-normative, and `internal/core/handlerpauseevents_ifqnj.go` says `HandlerResumedBySignal` is "Not yet implemented". Row 30 above already states this correctly. | inert | CONFIDENT |
| 11 | `budgetCounterState.RehydrateAccrual` — `internal/core/budgetcounterstate_hka8bg25.go` | No accrued spend is replayed after a restart, against its own comment that starting at zero violates CP-026a. | Both live meters build day counters at zero in their constructors while the calendar-day key stays the same, so every revive grants a fresh full daily allowance. This is the named missing protection behind the already-recorded "spend cap resets on restart" row. | fail-open | CONFIDENT |
| 12 | Age-only worktree deletion — `internal/workspace/orphansweep.go` and `internal/daemon/orphansweep.go` | Because the lease-lock writer is dead (ranked row 7 of the main table), `SweepStaleLeaseLocks.Removed` is always empty, so the PID-driven deletion path and `IsLeaseLockStale` never run. | `RemoveAgedNoLockWorktrees` runs `git worktree remove --force --force` keyed on **age, not liveness** — it walks for the most-recent mtime anywhere in the tree against a seven-day threshold. The comment in `internal/daemon/orphansweep.go` calls age "a conservative proxy for liveness" for a PID probe that is unreachable, so a long-lived quiet agent can lose its worktree. **Narrower than an earlier draft said:** this is not the only worktree GC — `SweepClaudeWorktrees`, `runWorktreeReclaim`, `runmerge.RemoveWorktree` and the QM-002b reap are all live too. | fail-open | CONFIDENT |
| 13 | `ArchiveVerdict`, `ReviewVerdictArchivePath` — `internal/workspace/reviewverdict.go` | Nothing renames `.harmonik/review.json` into the per-iteration archive. | The live `WriteAgentTaskVia` path writes into every implementer-resume worktree a pointer to `.harmonik/review.iter-<N-1>.json`, and `PriorVerdictFile` is never assigned in production — so the file named is always the one nothing creates. | fail-open | CONFIDENT on the missing writer, UNSURE on cost: paste-inject separately names a real feedback file |
| 14 | `SafeBoundaryForResource` / ON-048 step 2 — `internal/core/budgetexhaustion_on048.go` | The requirement to stop the in-flight model call at the next safe boundary never runs, so an agent mid-run keeps spending after the cap trips. | The live pause path builds and persists a list named `in_flight_at_pause`, and `harmonik handler status` shows it, while `internal/daemon/handlerpause_9hwbw.go` states the freeze list is "for operator visibility only". **The name says frozen and nothing is.** | fail-open | CONFIDENT |
| 15 | `ValidateEnvelopeSchemaVersion`, `EventRegistrySealed` — `internal/core/eventregistry.go` | No binary validates an envelope schema version against the per-type registry. The only caller lives in `internal/replay`, which no `main` reaches. | `Emit` in `internal/eventbus/busimpl.go` stamps `schema_version = 1` on an unregistered type and justifies the fallback with "the registry-coverage sensor (EV-034) catches unregistered types at startup". **No such sensor exists.** | fail-open | CONFIDENT |
| 16 | `WriteReleaseClaimCheckpoint`, `ReadReleaseClaim`, `IsReleaseClaimUnusable` — `internal/core/releaseclaimcheckpoint.go` | No release claim is written before a merge, a push or a bead close, against EM-031b: *"A release operation MUST NOT begin until this checkpoint is durable."* | The live release path `runmerge.RunBranchToTarget` picks its merge target fresh on every attempt, so after a restart nothing in git records that a release had already started. | fail-open | CONFIDENT |
| 17 | `PolicyRequiresConfirmation`, `ApplyVetoPromotion` — `internal/core/verdictoverride_rc027.go` | No run reads `confirm_required`, and no run ever parks for operator confirmation. | **Three shipped artifacts assert the gate exists.** `confirm_verdict` and `veto_verdict` are registered socket ops with real subcommands, `specs/s01/reconciliation/policies/cat-6a.yaml` sets `confirm_required: true`, and the shipped investigator prompt tells the agent "the daemon will pause before executing any repair verdict, awaiting operator confirmation." `internal/daemon/verdictoverride.go` admits the gap in its own header. | fail-open by intent, masked today because the executor is also dead | CONFIDENT |
| 18 | `DefaultIdempotencyClassForNodeRole`, `idempotencyAxisMatchesClass` — `internal/core/idempotencyclass.go`, `node.go` | No dispatch, retry or re-entry decision reads a node's `idempotency_class`. | The live DOT validator `checkIdempotencyClass` enforces the attribute as REQUIRED per WG-008, so `workflow.dot` marks `implement` as `non-idempotent` and the cascade re-enters it on a reviewer back-edge **exactly as if it were idempotent.** | fail-open | CONFIDENT on the absent consumer, UNSURE which subsystem was meant to be it |
| 19 | `RecoverFailedReplaceIntent`, `InstallBoundArchiveIntent` — `internal/queue/transaction.go` | Nothing recovers a `.replace-intent` file at boot, so a crash between intent install and cleanup leaves it forever. | The live `WriteReplacement` path then returns `noReplaceRefused` on the next differing attempt, and the queue name stays wedged until an operator deletes the file by hand, with no operator-facing hint. | fail-safe but unrecoverable without help | CONFIDENT |
| 20 | `Supervisor.Stop` — `internal/supervise/supervisor.go` | `Stop` is the only path into `terminateChild`, so the IN-PROCESS bounded SIGTERM-then-SIGKILL window never runs and `Spec.StopTimeout` plus the `StopTimeoutMS` config key are both inert. | The live shim only cancels the context, and that branch sends SIGTERM then blocks on the wait channel with **no deadline**, while the PL-011 doc comment promises a bound. **Narrower than an earlier draft said:** one bounded window DOES run, out of process in `RunStop` (`cmd/harmonik/supervise/stop.go`), at a hardcoded ten seconds that ignores the config key. | fail-safe, but the in-process path can hang next to auto-revive | CONFIDENT |
| 21 | `DetectGitVersion`, `GitVersion.meetsMinimum` — `internal/workspace/gitversion.go` | No startup probe checks git is at least 2.34, so `ErrGitVersionTooOld` can never be raised. | `internal/workspace/errors.go` and `specs/workspace-model.md` BOTH say the daemon refuses to start below that floor. Three live sites lean on it. The sharp one is `HasMergeCommitForBead`, which swallows every git error as "no merge evidence" — on an old git it would reset beads whose work already landed. | fail-open at that site | **UNSURE** — no deployed box is proven to run an older git |

**Two traps in this list deserve to be read twice.**

- **`ValidateTrailerValue` / `IsKnownTrailer` (`internal/core/trailers.go`) are dead, and the
  registry beneath them is scoped to a different artifact.** The registry holds eight
  `Harmonik-`-prefixed keys and does NOT hold this repo's own mandatory `Reviewed-By` and
  `Review-Verdict`. **State the gap precisely, because an earlier draft of this row overstated it:**
  `ValidateTrailerValue` takes an already-resolved `TrailerSpec`, never a key, so it cannot by
  itself reject an unknown trailer, and no trailer lint exists anywhere in the repo to wire. The
  registry describes checkpoint-commit trailers, which are a different artifact from this repo's
  dev commits. Decide which artifact the registry is for before building a lint on it.
- **`LockFromManifest` (`cmd/harmonik/asset_reconcile.go`) is a decoy — delete it, do not wire it.**
  `sync_assets_cmd.go` explains that stamping the lock from the manifest would bury a
  `.harmonik-new` conflict forever, and deliberately uses `lockFromOutcomes` instead. This is the
  wrong answer left lying next to the right one.

**Six candidates were checked and CLEARED. Do not chase these.** `tmuxSubstrate.releaseSpawnSlot`
is not a leaking semaphore — every spawn returns its slot through a closure on both error paths.
`StartupState.AssertOrphanSweepComplete` is redundant, not missing — PL-INV-003 holds today by call
ordering on one goroutine. `queue.fsyncDir` is a duplicate, because `Persist` already fsyncs the
parent directory inline. The dead half of `sentinel/signals.go` does not starve the live movement
governor, which recomputes its own window. The `presence` liveness wrappers sit over the live
`GetState`, so liveness has one rule and it runs. `HarnessRegistry.Sealed` is an accessor, and the
seal is enforced inside the live `ForAgent`.

### 5. Orphaned consumers — a live reader with no producer at all

Section 4 is about a protection that never runs. This one is about a reader that DOES run, gets
nothing, and reports the nothing as a clean result. Found by a whole-repo census of all 181
`EventType` constants plus a file and struct-field sweep. All NEW unless marked.

**Events with a live reader and no emitter anywhere — not even in a test.**

| Signal | Live reader | What the reader concludes | Mode |
|---|---|---|---|
| `workspace_merge_status` | `NotifyStreamConsumer.handleMergeStatus`, one of four subscriptions the consumer's own `Subscribe` registers. Wired only when `cfg.NotifyStream != nil`, which happens only in `cmd/harmonik/run.go` — **`harmonik daemon` never has it** | The commit-SHA map stays empty for every run, so `handleRunCompleted` takes the `sha == ""` branch and prints `[bead] success` **with no commit**. The operator reads bare "success" as verified. Scope it to a foreground `harmonik run --notify-stream` that starts its own daemon — when a daemon is already up, `run` returns through `runBeadSubcommandViaDaemon` and emits no per-bead line at all. | fail-open |
| `bus_overflow` | The event-load path in `internal/scenario/asserteval.go`, live on `harmonik harness` | It returns a hard error on sighting the event — *"assertion completeness is defeated (SH-024 v)"*. The bus has no shed path that emits it, so a dropped or shed event becomes a **quietly passing conformance assert** instead of a refusal to judge. | fail-open |
| `session_keeper_operator_attached` | `resolveAttachedSource` and `scanSuppressionEvents` in `internal/digest/resolver.go`, reached from `Build` | `internal/keeper/cycle.go` says outright that "emitOperatorAttached stays a deliberate NO-OP". So the `operator_attached` suppression source is permanently inactive and `sentinel.attached_inactive_timeout` is inert. **Narrow the harm** — `SuppressionState` has no programmatic consumer at all, it only reaches `digest --json` output, and the sentinel's real suppression is a separate `SuppressedBy` mechanism in `internal/sentinel/governor.go`. This misinforms a reader, it does not un-quiet the sentinel. | reporting-only |
| `checkpoint_written` | `evalReadEvents` in `cmd/harmonik/eval_cmd.go` sets `commitSHA` from it | **No production emitter** — the only writers are test-side, in `internal/testhelpers/jsonlfixture.go` and four others. `CommitSHA` is empty on every eval record ever collected. Sits beside the known "eval pass comes from a node NAME" defect in the same file. | reporting-only |

**A discipline note that shaped this list.** Readers living in `internal/twinparity`, `internal/watch`
and `internal/replay` were EXCLUDED, because those packages have zero non-test importers. A dead
reader of a dead signal is not an orphaned consumer. That exclusion removed
`agent_warning_silent_hang`, `agent_resumed_after_warning`, `post_agent_ready_hang`, `metric`,
`review_loop_cycle_complete` and the `session_keeper_*` cluster.

**Files a live path reads that nothing in this repo writes.**

- `.harmonik/cognition/loop-status.json` — `ReadLoopStatus` serves `harmonik supervise status`.
  `WriteLoopStatusAtomic` sits in the same file and its doc claims "Called by the cognition loop on
  every LoopStatus transition". **That is false — it has zero production callers, and all seven
  call sites sit in two test files.** The whole ON-008a
  surface is silent, so `budget-paused` and `circuit-tripped` never reach the operator and the
  silence reads as "not paused".
- `.harmonik/agents/<type>/manifest.yaml` — parsed by `agentmanifest.Load` and `Check`.
  **`harmonik init` does not create `.harmonik/agents/`** — the embedded asset FS covers `context`,
  `scaffolds`, `scripts`, `skills` and `templates` only. In this repo the files are hand-authored
  and tracked, so the risk lands on any project bootstrapped by `init`. **State the consequence
  carefully — an earlier draft got it wrong.** The documented fail-open "a load error reads as
  non-persistent" belongs to the `PersistentType` closure in `internal/daemon/bootsocket.go`, and
  that closure is constructed at boot and **never called**, because `CrewIdleReaper.StartWatcher`
  has an empty body (the operator-directed disable already recorded as ranked row 37). So no
  persistent oversight role is currently at risk. This is a fail-open **armed and waiting** for the
  day the reaper is re-enabled, not a live one. Note also that `crew.ResolveType` and
  `resolveCrewType` only `os.Stat` the type DIRECTORY — they never parse the manifest.
- `.harmonik/cognition/notes.jsonl` — read by `readOpenNotes`, serving `harmonik digest` and the
  watch frame. The producer named in `.flywheel/README.md` is a Pi extension outside this tree,
  **and it was DELETED in `353fc3c1e`** — so this is worse than "the writer lives elsewhere", the
  writer no longer exists. Absence returns `(nil, nil)`, so both renderers print
  `=== Open notes (0) ===` and the operator reads "no unresolved notes" for a channel nothing
  writes.
- `.harmonik/context/lanes.json` — read by `DashboardBuilder.readLanes` and by
  `captainCuratedQueues` inside the live dispatch loop. The producer is the captain by hand.
  **This one is fail-open BY DESIGN and the source says so** — record it, do not "fix" it.
- `.flywheel/skills/sentinel-adversary.md` — `DefaultAdversaryMissionRelPath`, handed to the crew
  handler as `mission_path` and reached live from the movement governor. Tracked here, absent from
  the embedded assets and from `init`. The governor treats a spawn failure as non-fatal, so it
  believes it fired an independent adversary review that in fact booted with no mission. UNSURE on
  the tail.

**Fields a live guard reads that production never assigns.** These are the same class as section 3
and they are the sharpest findings in this section.

- **`core.Subscription.Since` and `OffsetCheckpointEventID`** — read by `busImpl.Seal`, whose switch
  is `Since`, then `OffsetCheckpointEventID`, then `default: continue`. **Zero assignments across
  all 19 production subscription registrations.** *(Count them by registration, not by composite
  literal — `internal/daemon/notifystream.go` registers four consumers from one
  `[]core.Subscription{}` slice header, which is why a naive grep says 17. The count said 20 until
  2026-08-06; it counted `internal/hooksystem/dispatcher.go`, which no production Go file imports,
  so it is not a production registration.)* Every consumer takes
  the `default` branch on every boot, so the EV-014d startup-replay phase is a no-op in production.
  `Subscription.OnTailTruncation` is reachable only inside that replay loop, so tail-truncation
  detection can never fire either.

  **This bears on the "spend cap resets on every restart" row, and it is a PARTIAL cause, not the
  cause.** Section 4 row 11 names `budgetCounterState.RehydrateAccrual` for the same row. Neither
  is sufficient on its own, and the two do not compose cleanly: nothing persists a checkpoint that
  could be supplied to `Since`, and `Since` is keyed on event id while the meter is keyed on the
  calendar day, so a naive replay would over-count. **Do not scope a fix from either one alone.**
- **`handlercontract.SpawnWatcherConfig.NodeType`** — read by the HC-061 guard in `SpawnWatcher`:
  `if typeOnly.Type == ProgressMsgTypeOutcomeEmitted && cfg.NodeType == core.NodeTypeSubWorkflow`.
  Both production literals live in `internal/handler/handler.go` and neither sets it, so the zero
  value is the empty string and never `"sub-workflow"`. **The spec calls this a MUST NOT, the code
  implements the refusal, and the condition can never be true.** **Rank it as dead redundancy, not
  a live hole** — a sub-workflow node never reaches `Handler.Launch` at all, because
  `dot_cascade_core.go` routes it to `newDotSubWorkflowRunner`, and HC-061 assigns enforcement to
  twin parity rather than to the watcher. It is a guard for a path that does not exist.
- **`core.Outcome.SuggestedNextIDs` and `ContextUpdates`** — read by `SelectNextEdge` and
  `ApplyContextUpdates`, which `driveDotWorkflow` reaches after every non-terminal node. **No
  production literal sets either field.** *(State it that narrowly. The live DOT path does set
  other fields — `PreferredLabel`, `PreferredLabelFlags`, `Kind`, `FailureClass`, `Notes` — so an
  earlier draft saying it builds `Status` only was wrong.)* The daemon-side decode struct carries
  `Kind`, `SubReason` and `SuggestedClass`, so there is no path from a handler's on-wire
  `suggested_next_ids` into `core.Outcome`. `handlercontract.OutcomeEmittedMsg` documents
  that the watcher MUST copy `SuggestedNextIDs` into the `core.Outcome` it returns, and nothing
  does. EM-041(c) narrowing and EM-041a context updates both run on permanently empty inputs, with
  no diagnostic.
- `daemon.SandboxProfileInput.TmpDirs` — the sole production literal in `sandboxSpawnForRun` omits
  it, so `GenerateSandboxProfile` never allows the per-run tmp dirs. **Fail-safe** — it denies
  rather than permits — but the sandbox behaves differently from its generator's stated contract.
- `daemon.AutoResumeConfig` is never constructed in production at all, so `cfg.Disabled` reads a
  zero value. This compounds the known `ClaudeCodeAdapter.Diagnose` row rather than standing alone.

**Wiring that a live caller believes.**

- **The merge gate is short-circuited, and one live graph has no gate of its own.** `beadRunOne` is
  the only production caller of `WireSpine` and sets `SpineArgs.SkipGate: true` unconditionally, so
  `RunBridge.gateHook` returns `EvGatePassed` with no check and `stepRunGating` cannot tell a real
  pass from a stub. The in-source justification is that the graph gates itself. **That holds for
  `standard-bead.dot` and fails for `no-review-bead.dot`,** which is `implement -> close` with no
  gate node and no reviewer node. `resolveWorkflow` and `resolveNoReviewWorkflow` select it for a
  queue item with `workflow: single` and for a bead with the `workflow:single` label. Such a run
  merges with **no test gate, no scenario gate and no review.** *(Not "no build gate" — an earlier
  draft said that and it is wrong. `runMergeBuildGate` in `internal/runmerge/merge.go` runs
  `go build ./...` and `go vet ./...` on every merge, plus the gofumpt and gci format gate.)* The
  label path at least calls `emitReviewBypassed` first. **The queue-item path returns earlier and
  emits no `review_bypassed` event at all.** It is not wholly unaudited — `run_started` durably
  records `workflow_id=no-review-bead`, `review_policy=no_review` and
  `workflow_selection_source=queue_item_single_mode`, and `validRunStartedPolicyBinding` enforces
  that binding. What is missing is the explicit bypass signal, not the trace.
- **The captured harness session id is thrown away and resume uses a minted one.** The
  `StdoutWrapper` closure in `runAgentLaunch` installs `NewSessionIDInterceptor` and drops the real
  session id into a channel the source itself annotates as "buffered; no site reads it back".
  Meanwhile `buildCodexRoutedLaunchSpec` mints a fresh UUIDv7 into `ClaudeSessionID`, which becomes
  `PriorSessionID`, which `internal/harness/codex/harness.go` turns into `codex exec resume <id>`
  and `internal/harness/pi/launchspec.go` into `pi --session <id>`. The cascade believes it is
  refining the same agent thread. `emitDotImplementerResumed` writes the same invented value to the
  event log. **UNSURE** whether codex or pi rejects the unknown id loudly or silently starts fresh.
- `RunEffectors.CheckEscape` is bound to a closure that returns `EvGuardsPassed` and runs no check.
  The source is honest — "Read the pass as 'no guard exists', not as 'a guard passed'". Dead today,
  because the guarding phase is entered only on events nothing produces. It becomes a live
  fail-open the moment that phase is entered.
- The `subscribe` refusal is correctly reachable, and the source records the real problem: **"Four
  of the six clients of this op do not check the response envelope, so they mis-report the
  refusal."** Those clients read a refused subscription as an empty but healthy stream. Only bites
  when an operator sets `subsystems.subscribe_hub.enabled: false`.
- `codexSubstrateOptions` returns a `*sessioncapture.Session` "so a caller MAY Close it", and
  `selectSubstrate` discards it with a blank identifier. With `HARMONIK_CAPTURE_DIR` set, the
  corpus session stays open for the daemon's whole life.

**Cleared, and recorded so nobody re-chases them.** `Harness.Seed` and `Harness.Retask` return nil
unconditionally on all three harnesses but have zero production callers. `keeper.NoopEmitter` and
`nullJSONLWriter` are nil-fallbacks production never reaches. Every keeper checker field is filled
by `applyDefaults`. `HandlerPauseController.persistFn` is constructed nil and patched by
`loadStartupState`. `Subscription.OnPanic` IS set by every production subscription — an earlier
row above says otherwise about `busimpl.go` and should be read as scoped to that file only.

**False alarms from the automated field scan — do not re-chase.** A "field never assigned" grep
flags all of these and every one is wrong: `FleetFacts.Queued` / `InProgress` / `Draft` /
`Deferred` are set through nested fields, `agentLaunchResult.SocketOutcome` by tuple assignment,
`WindowSample.MovementScore` with `+=`, the `DigestJSON` slices by tuple assignment,
`readiness.RunPlan.RepoTarget` / `RemoteWorker` / `ScratchRepo` through `fs.StringVar` pointers,
and `GCRetiredIntentsResult.Retained` with `++`.

### 6. The 634 unreachable functions, grouped — do not enumerate them

`deadcode ./cmd/...` reports 634 functions that no `main` can reach. Most are inert, and listing
them one by one would bury the few that are not. The useful shape is the grouping, because each
group is one decision, not N decisions.

| Group | Count | What it is |
|---|---:|---|
| Value types that validate themselves | **52** | A `Valid()` method on a domain type that nothing calls. This is the requirement-per-file cohort this document already names in the Assessment. The type is constructed, and its own invariant check is skipped. |
| Reconciliation verdict layer (RC-0xx) | **26** | Row 6 and its strand. `ExecuteVerdict` is the root, and everything below it dies with it. |
| `MarshalText` / `UnmarshalText` pairs | **24** | Wire-format codecs for value types that never cross a wire. They come in pairs, so this is 12 types. |
| Policy and S02 layer | **24** | Row 2. `ParsePolicyDocument`, `NewS02PolicyEngine`, `NewS02Registrar` and the validators under them. |
| Budget layer (ON-045/047/048, CP-022) | **20** | Row 4. `CheckBudgetAtDispatch`, `TightestBudget`, `CheckWallClockOuterBound` and the counter state below them. |
| Everything else | 488 | Spread thin across `internal/workspace`, `internal/lifecycle`, `internal/daemon`, `internal/substrate` and the harness adapters. |

**Read this as five decisions, not 634.** Four of the five groups have a single root — wire the root
and most of the group comes alive, delete the root and most of the group goes with it. The
`Valid()` group is the exception and it is the one that matters for the rewrite: 52 types carry an
invariant that the code never asserts, so the type system promises a guarantee the runtime does
not keep.

### 7. What is merely inert, and what points the wrong way

Rank by direction, not by size. The three questions to ask of any zero-caller finding:

1. **Is there a live reader?** No reader means inert. A reader means the defect propagates.
2. **What does the reader conclude from the absence?** "Nothing is wrong" is the dangerous answer.
3. **Fail-open or fail-safe?** `strandedBeadHasOnDiskRun` returning `false` lets a reset proceed.
   `probeRunProcessDead` returning `false` stops a reap. Same empty input, opposite direction, and
   only the first one can corrupt state.

**A caution the charter is right about, and this section obeys.** Code reached only from one test
may be UNFINISHED AND LOAD-BEARING, not dead. `internal/run/registry.go` is the proof: `Write` is
complete, correct, atomic, and has a test. It is not abandoned code to delete. It is a wire that
came loose. Do not delete a zero-caller symbol on the strength of this section alone.

**Where this section is sure, and where it is not.** Both zero-caller findings are CONFIDENT that
the symbol has no production caller, because a call-graph tool established that, not a grep, and
because each has a named live consumer. Finding 2 is deliberately marked UNSURE on a second
question: the PI-073 guard is provably vacuous, and whether a pi run can reach the one emitter of
`agent_rate_limit_status` today is NOT established here. Treat "the hazard may be unreachable" as
an open question, never as a reason to close the row.

---

## DANGEROUS — incompleteness that causes silent WRONG behavior, not absent behavior

Ranked by blast radius. These act wrong or claim success falsely.

| Risk | Symbol / path | Why it is wrong, not merely missing |
|---|---|---|
| ~~**Failing work is merged and closed green**~~ **FIXED** | `dotTerminalNodeIsSuccess`, `internal/daemon/dot_cascade_helpers.go` | Was an inverted-by-default guard: four failure-terminal spellings in-repo classified as success, and 55 live `codename:eval` beads route through it. Now reads the graph — WG-022's reserved pair is normative, any other terminal declares `terminal_disposition`, and an undeclared terminal is unclassifiable and does not merge. Owes a `specs/workflow-graph.md` amendment for the attribute (see the fixing commit). |
| **A sub-workflow's failure terminal reports SUCCESS to its parent** — *ranked above the fixed row: this one COMPOSES* | `DispatchSubWorkflow`, `internal/workflow/sub_workflow.go`; `dispatchSubWorkflowExpandedNode`, `internal/daemon/sub_workflow_runner.go` | The parent never learns which terminal the inner run reached. `terminalOutcome` is just the last dispatched node's outcome, and the expanded-node dispatcher synthesizes SUCCESS for any non-shell non-agentic node — so a sub-workflow landing on a failure terminal (`specs/examples/sub-workflow-commit-gate.dot` declares `terminal_node_ids="tests,gate_fail"`) hands SUCCESS up. The parent cascade then routes on that lie, so the false green propagates instead of stopping. Not a name comparison — the "ask the graph what this terminal means" seam is absent entirely. |
| **The eval collector's pass/fail comes from a node NAME** | `evalReadEvents` / `evalBuildRecord`, `cmd/harmonik/eval_cmd.go` | `rec.Pass` is set from whether a node literally named `judge` emitted an outcome, plus an assumption that `judge` only runs when `grade` succeeded. The same name-heuristic pattern as the fixed row, verbatim. `eval-bead.dot` now declares `terminal_disposition` on both terminals and the collector ignores it; it should read the terminal (or `run_completed` vs `run_failed`). Reporting-only — no bead transition — so lower blast radius, but it is the number the model evaluations are judged on. |
| **A graph whose gate node is not named `commit_gate` loses the F42 salvage** | `driveDotWorkflow`, `internal/daemon/dot_cascade_core.go` | The cap-hit auto-merge salvage is gated on `currentNodeID == "commit_gate"`, so committed work is stranded on any graph that names its gate anything else. Its companion predicate `graphHasReviewerNode` is deliberately name-free; only the node-ID half is stale. The name-free replacement already exists at the gate-dispatch site: `ToolCommand != "" && HandlerRef == "shell"`. A second `commit_gate` literal in the same function selects implementer feedback text only — benign, but fix both together. |
| **`terminalNodeID == "close"` re-derives polarity by name** | `beadRunOne`'s `rebase_dropped_commits` CarveOut, `internal/daemon/workloop.go` | The only consumer of `dotWorkflowResult.terminalNodeID` anywhere. On a graph whose success terminal is `close-pass` / `plan-approved` / a declared `terminal_disposition="success"` node, the carve-out does not fire and the bead is re-dispatched instead of closed. Fails in the safe direction — a missed carve-out re-queues, it never closes a false green — so it is a re-dispatch loop, not a corruption. One-line fix: use `dotResult.success`, which now means "the graph said so". |
| **Every policy decision is "allow"** | `core.NoOpPolicyEngine.Evaluate` → `{Permitted: true}`, wired at `cmd/harmonik/main.go:961` and discarded at `:962` | The adjacent comment claims *"The dispatcher always calls policyEngine.Evaluate"* — false. Gates, guards, budgets, clearances: permit-by-default, silently. |
| **A run whose hook never fired closes as success** | the auto-close case in `beadRunOne`, `internal/daemon/workloop.go` | Hook not firing, relay unable to dial, displaced `settings.json`, or an expired 3s grace are all indistinguishable from success. `internal/hookrelay/hookrelay.go` returns 0 with **no stderr line** when the `HARMONIK_*` vars are absent, so a typo'd var looks like a correctly-skipped hook — every other failure path in that file does write a diagnostic and return 1. **Correction 2026-07-30:** the guard is no longer the two-conjunct `socketOutcome == nil && ei.ExitCode == 0` the audit quoted. It now reads `socketOutcome == nil && ei.ExitCode == exitCodeClean && !watcherFailed` — a third conjunct was added and the bare `0` became a named constant. The shape survives: a nil socket outcome plus a clean exit still closes green. |
| **Role/clearance validation is never called** | `ValidateRequiredRoleDefaultSkills`, `internal/core/policydocument.go` | **Two of the three sub-claims are now FALSE — corrected 2026-07-30.** (a) The literal is no longer `"mvh-required"`. The guard now reads `if r.Status != RoleStatusRequired { continue }`, and the string `"mvh-required"` appears nowhere in `policydocument.go`. (b) `RoleStatusMVHRequired` **no longer exists** in `internal/core/role.go` — only `RoleStatusRequired` and `RoleStatusDeclaredButDeferred` remain, and the retired spelling survives only as a negative fixture in `rolevalidation_test.go`. (c) "zero tests" is FALSE: `TestValidateRequiredRoleDefaultSkills` exists. What SURVIVES, and is the whole point of the row: `ValidateRequiredRoleDefaultSkills` still has **zero non-test callers**, so CP-031 is enforced nowhere at all. The fail-open `continue` also survives — a role with an unexpected status is skipped, not rejected. |
| **A formatter crash reads as "tree is clean"** | `fmtGofumptPass`/`fmtGciPass`, `internal/runmerge/fmtgate.go` | `if err != nil \|\| strings.TrimSpace(out) == "" { return false, nil }` collapses exec failure into the clean result. Caller pushes. The file's *other* fail-open (tool absent) is disclosed; this one is not. |
| **Secrets reach the durable log** | `RedactionRegistry` empty at runtime | Value-pattern stage always has an empty pattern set; the surviving name-match is flat and non-recursive. Nested/array credentials pass through to `events.jsonl` and every subscriber. `redaction_failed` has no emitter, so a scrub failure is unobservable. |
| **Dead-letter health reports green because nothing is recorded** | `NoopDeadLetterSink` + `NoopWatcherDeadLetter` | `Watcher.DeadLetterFailures()` stays 0 by construction. A panicking or erroring subscriber produces no log, no record, no event. A **synchronous** consumer panic has no `recover` at all (`busimpl.go:523`) and takes down the daemon. |
| **Worktree mutual exclusion is claimed, not provided** | `WriteLeaseLockAtomic` never called | Detectors keyed on lock presence (WM-003a bare-worktree-no-lease, WM-013c, reconciliation Cat 6) see **every** live worktree as unleased — a detector wired to a signal nothing produces. `LeakKindLease` scans for files nothing writes. |
| **Budget exhaustion pauses the whole handler type — the spec says it MUST NOT** | `handleBudgetExhausted`, `handlerpause_policy_37zy8.go`; `policy.BudgetExhaustedTrips() → true` | HP-012 inverted. `BudgetExhaustedEventPayload.BudgetScope` **exists** and is never read. Compounded by findings 10+11: the resulting pause is un-clearable. |
| **A codex/pi rate limit pauses the claude fleet** | `NewHandlerPausePolicyGoroutine(… AgentType: core.AgentTypeClaudeCode …)`, `bootstate.go` | One instance, hardcoded; handlers ignore the event's origin (`agentType := p.cfg.AgentType`, justified in-source by *"All beads use claude-code"* — now false, since `RegisterCodex`/`RegisterPi` are both live). `ResolvedAgentType` returns the constant `claude-code`, discarding both args — which becomes **fail-open** the moment finding 11 is wired. |
| **Spend cap resets on every restart** | `internal/daemon/spendmeter_hkk3f8g.go` | `runsToday`/`bytesToday`/`exhausted` are plain struct fields with no persistence and no boot load. `harmonik supervise` auto-revives, so a crash-loop silently restores the full daily allowance. The ceiling is per-daemon-lifetime, not per-day. |
| **Cost data is fabricated, not missing** | `sessiondata.Collect`; `usage.go` | 213/449 records all-zero and 33 successful sonnet runs at `cost_usd: 0`, presented identically to genuinely-free runs. Given the standing preference to route implementers to Codex, real spend is understated by roughly half with no signal. |
| **`harmonik usage` reports 100% productive off-repo** | `findOrchestratorSessions` | `nil,nil` on a path miss → `$0.0000 (0.0%)`, indistinguishable from "you had no orchestrator sessions". |
| **File-only Pi credentials silently fall back to ambient env** | `resolvePiAPIKeyValue`, `harness/pi/launchspec.go:227-237` | Swallows the error branch and returns `os.Getenv(apiKeyEnv)` — defeating the template's stated purpose ("the daemon ambient env never carries the secret") rather than refusing. |
| **A `harness:pi` bead asks the pi provider for a Claude model** | `EnvModelKey = "HARMONIK_CLAUDE_MODEL"`, `modelpreference.go:64` | Read at tier 2.5 regardless of `agentType`. Same bug class as the already-fixed tier-3 leak (hk-pkugu), one tier above the fix. |
| **The network sandbox guard makes isolation look enforced** | `ApplyNetworkSandbox` (zero callers); `if cfg.EnableNetworkSandbox && !IsNetworkSandboxActive()` | The flag is never set true and the sole `DriveOrchestration` caller omits it, so the real, fail-closed pf/netns implementation is never installed while SH-028 reads as enforced. |
| **Parity gates cannot fail** | `internal/twinparity` | Self-comparison (above), plus three engine holes: `PayloadFields` is vacuous for any field outside the hardcoded `stablePayloadFields`; `AllowExtra` is declared and **never read in any conditional**; `run_completed.success` is not whitelisted, so a **failed run canonicalizes identically to a successful one**. |
| **The dashboard gate blocks nothing / or blocks forever** | `dashboardgate.go` `captainCuratedQueues` | `BlockedQueues` derives from `lanes.json`, currently `"lanes": []` → emits `dashboard_stale` and blocks nothing, against its own in-source fail-loud mandate. Conversely, once lanes exist, `dashboard.max_staleness` becomes a permanent dispatch halt because nothing can write the file it measures (finding 15). |
| **A typo in `sentinel.mode` silently disables the governor** | `internal/digest/sentinelconfig.go` | No enum validation. `"ACT"`, `"Act"`, `"enforce"` take neither the observe nor the act branch: no signal, no trip, no halt, no log. |
| **The sentinel's opportunity gate is latched open forever** | `buildHasUndeployedTail`, `internal/digest/builder.go` | Returns true if **any** closed bead carries a Phase-2 label — a monotonically growing set — standing in for an unimplemented verify. Once one closes, the §1.3 "MUST NOT trip without actionable work" guard is pinned. |
| **macOS process-leak sensor contributes zero leaks regardless of state** | `postsuiteleaksensor_darwin.go` `checkLeakedProcesses → nil, nil` | `HasLeaks()` returns false; SH-INV-002(i) silently does not run on this machine, and the report does not say "skipped". |
| **The import-discipline test cannot fail** | `TestBreakageAdapterIsSoleExecImporter`, `internal/brcli/breakage_test.go` | Its helper `breakageFixtureListHarmonikPackages` runs `go list -json ./...` without setting `cmd.Dir`, so `./...` resolves against the test's own package directory and enumerates only `internal/brcli` — which the loop then skips as the adapter package. **Broader than the audit said (re-verified 2026-07-30):** `internal/runmerge` imports `os/exec` in **three** non-test files (`merge.go`, `fmtgate.go`, `reviewtrailers.go`), and the carve-out map covers only `internal/handler` and `tools/forbid-import`. The test can never see any of them. |
| **Config typos are discarded in silence** | `strictDecodeKeeperBlock`, `internal/projectconfig/projectconfig.go` | Unknown-key rejection is scoped to the `keeper:` sub-node **only**. A typo in `daemon:`, `watch:`, `sandbox:`, `harnesses:`, `supervise:`, `stall_sentinel:`, `crews:` or `opsmonitor:` is dropped without a word. **Mechanism changed, 2026-07-30:** the function no longer uses `KnownFields(true)` at all. Its own comment now says "No KnownFields here — top-level tolerance must be preserved", and it calls `unknownYAMLKey`, a hand-rolled reflective walk over `rawKeeperConfig`. The audit's "applies `KnownFields(true)`" wording is stale; the scope, and therefore the defect, is unchanged. |
| **Fail-open default arms** | `watch.Classify` default → `EscalationPullDigest` (*"never wakes the captain"*); `GenuineDrain` default → `DrainStateDrained` | Both currently moot (no live caller / three-value enum) but both default toward "safe/quiet" in code that is otherwise fail-closed. |

### Re-verified row by row on 2026-08-04 at `51bd8aa84`

**Not one row above is fixed or deleted.** Every named defect survives 385 commits. What moved is
CATEGORY, not correctness: several rows decayed from "a live fail-open" into "dead code that
describes a fail-open". Say that in the row rather than deleting it, because each one re-arms the
moment its call site lands.

**The `e65ec5657` scare was a false alarm for this table.** `beadRunOne` still exists in
`internal/daemon/workloop.go`. Both rows that name it survive, one in place and one relocated.

**Three rows are now WORSE than written:**

- **Worktree leases** — the harm is bigger than "a detector sees nothing". `RunOrphanSweep` calls
  `SweepStaleLeaseLocks` and then `RemoveAgedNoLockWorktrees`. Because no lease is ever written,
  every LIVE worktree lands in `NoLock` and becomes eligible for age-based force-removal, and the
  PID-liveness branch never runs. This does not merely fail to protect. It deletes live work.
- **Cost is fabricated** — `PriceFor` returns sonnet rates as its `defaultPrice` for ANY
  unrecognized model. Codex and pi runs therefore carry an INVENTED cost with no marker, on the
  live run-terminal write path. `Record.CostUSD` became a `*float64`, but nil signals "no model
  string", not "no measurement", so an unreadable transcript with a model set still writes
  `cost_usd: 0`.
- **The import-discipline test** — `internal/runmerge` has **six** non-test `os/exec` importers, not
  three. Module-wide the test cannot see 107 non-test files across about 40 packages.

**Two rows had their direction inverted, and the correction matters:**

- **A codex/pi rate limit pauses the claude fleet** — the named harm cannot occur, because no codex
  or pi rate-limit emitter exists. The reachable harm runs the OTHER WAY: `ResolvedAgentType` feeds
  the QM-052a submit gate in `internal/queue/validation.go`, so a CLAUDE pause blocks CODEX AND PI
  submissions. The in-source excuse ("All beads use claude-code") is now false. **Do NOT reach for
  `RunHandle.SetAgentType` as the fix** — that setter has zero production callers of its own, so it
  records nothing. See finding 2 in "Zero-caller writers and orphaned consumers" above.
  *(Amended 2026-08-06: the "records nothing" half of that warning EXPIRED with `d7493e7c9`, which
  restored the caller in `internal/daemon/agentlaunch.go`. The warning itself still stands on its
  own terms — the wrong-direction harm is the reachable one, and a setter is not where that gets
  fixed — but do not repeat "zero production callers" as the reason.)*
- **The network sandbox guard** — not a guard that fails open. `ApplyNetworkSandbox` has zero
  callers anywhere, tests included, and `EnableNetworkSandbox` is never set true even in a test.
  SH-028 is unimplemented at the call site and the real pf and netns code is unreachable.

**Two rows need their CLAIM narrowed, not just dated:**

- **Sub-workflow failure terminal reports SUCCESS** — the defect is real and live, but the witness
  is wrong. `gate_fail` in `specs/examples/sub-workflow-commit-gate.dot` is a SHELL node and it
  fails correctly. The defect bites on a non-shell non-agentic terminal, which that file's own
  note D2 states. The parent now has the seam — `driveDotWorkflow` calls `dotTerminalNodeIsSuccess`
  — but `DispatchSubWorkflow` lives in `internal/workflow` and cannot see that helper.
- **Parity gates cannot fail** — the blanket claim is SUPERSEDED. A genuine cross-stream sub-test
  now runs first, and drift tests prove the engine bites. Three holes survive: `PayloadFields`
  cannot reach the canonicalizer, `AllowExtra` is read in no conditional, and `run_completed` has
  no `stablePayloadFields` entry. The stake is a weak test gate, not wrong daemon behaviour.

**One row changed shape.** *A run whose hook never fired closes as success*: the guard left
`beadRunOne` and is now `dotNodeTerminalFailure` in `internal/daemon/dot_cascade_helpers.go`,
called from `dot_cascade_core.go` twice and from `dot_gate.go`. The decision is now per-node and
the test reads `!outcomeIsAnAgentReport(socketOutcome) && exit.ExitCode == exitCodeClean &&
!watcherFailed`. **It got wider:** a payload that exists but carries no kind now counts as "nothing
reported", where the old `socketOutcome == nil` would have failed it. `hookrelay.Run` still
returns 0 with no stderr when a `HARMONIK_*` variable is absent.

**Six rows are now LATENT — the defect is dead code, not a live hazard.** Keep them, and mark them:
role/clearance validation (no production code loads a policy document at all, so the fail-open
`continue` is unreachable), the network sandbox, the budget-scope inversion (no per-run
`budget_exhausted` producer exists), the fail-open default arms, the synchronous consumer-panic
path (20 subscriber registrations, none synchronous — a trap for a future subscriber), and the
no-op policy engine. The sentinel opportunity gate is latent for a different reason: it
short-circuits to false while `phase2Classes` is empty, which it is today.

**Small corrections.** The strict-decode set is `keeper:` PLUS `subsystems:`, not keeper alone.
The drain fail-open default lives in `policy.ClassifyDrain`, not in `GenuineDrain`, and live code
calls `GatherDrainFacts` directly without crossing that bridge. The budget-pause row's
"un-clearable" clause is FALSE — `harmonik handler resume` exists — but a separate live defect
replaces it: that command edits `handler-state.json` directly and a running daemon overwrites it.
`terminalNodeID == "close"` gained an `approveVerdict != nil` disjunct, so a reviewer-APPROVE
terminal now takes the carve-out by any name, and only a success terminal reached WITHOUT a
reviewer APPROVE still misses it.

**Ranked by danger, of the rows that are live and can corrupt or lose work:**

1. Sub-workflow failure terminal reports SUCCESS — a false green propagates up the parent cascade
   and gets merged. Same class as the row already fixed at the top of this table, and it composes.
2. Formatter crash reads as clean — it sits directly in the merge push loop.
3. A run whose hook never fired closes green — now wider, per the shape change above.
4. Secrets reach `events.jsonl` — nested and array credentials, durably, with no failure signal.
5. Worktree leases are never written — the sweep force-removes live worktrees by age.
6. Spend cap resets on restart — default-on, and `supervise` restarts routinely.
7. Cost is priced at sonnet rates for unknown models.
8. The F42 salvage is keyed on the literal name `commit_gate`.
9. Dead-letter records nothing.
10. A claude pause blocks codex and pi submissions.
11. A Claude model string is sent to the pi provider.
12. A pi key read failure falls through to the ambient environment.
13. The import-discipline test is a tautology over 107 files.
14. Config typos outside `keeper:` and `subsystems:` are dropped in silence.

---

Config keys that are parsed, validated, and then ignored — an operator sets a limit and gets none.
**Re-checked key by key on 2026-07-30. Three of the eleven are now FALSE and are struck out.**

Still ignored:

- `harnesses.pi.provider_slots` — per-provider concurrency runs **unbounded**. Parsed into
  `PiConfig.ProviderSlots`; zero readers outside `internal/projectconfig`.
- `stall_sentinel.escalation.*` and `.detection.*` — nothing detects, nothing pages, at any tier.
  `cmd/harmonik/resolve_stall_sentinel_config.go` uses them only as presence gates.
- `watch.absent_thresh_s` / `stall_ticks` — validated, never used.
- `keeper.timings.max_boot_grace_total` — **re-checked 2026-07-30, and it looks wired when it is
  not.** `CyclerConfig.MaxBootGraceTotal` exists and `internal/keeper/step.go` genuinely reads it, so
  a grep on the Go identifier is reassuring. The YAML key never reaches that field: nothing in
  `cmd/harmonik` assigns it, and `internal/keeper/cycle.go` fills it with `2 × BootGracePeriod`. So
  the operator can set the key to any value and the behaviour does not change.
- `keeper.self_service.instruct_only_when_idle` — **re-checked 2026-07-30, and this one also looks
  wired.** It is threaded through `resolve_keeper_config.go` and `keeper_cmd.go` all the way to
  `WatcherConfig.SelfServiceInstructOnlyWhenIdle`. The field is assigned and never tested — no
  conditional anywhere reads it.
- `keeper.hard_ceiling.cooldown` — silently shadowed by the different key
  `keeper.cadence.hard_ceiling_cooldown`, which is what the resolver reads.
- `sandbox.network.mode` — `parseSandboxBlock` validates only `backend`. The source says the
  network mode is "stored verbatim from config for forward-compat but not further validated".

No longer true — **remove these from any scope built on this list**:

- ~~`keeper.cadence.no_gauge_backoff` (**required to boot and inert**)~~ — now live.
  `cmd/harmonik/resolve_keeper_config.go` resolves it and `keeper_cmd.go` passes it through.
- ~~`daemon.target_branch` (captain briefed on one branch, daemon merges into another)~~ — now
  read. `cmd/harmonik/captain.go` prefers `pc.Daemon.TargetBranch` when set. `branching.yaml`
  still governs the merge itself, so a mismatch remains possible, but the key is not unread.
- ~~`harnesses.pi.profiles.*.api_key_file`~~ — now live. `internal/daemon/workloop.go` sets
  `APIKeyFile` from the resolved profile, and `internal/harness/pi/harness.go` consumes it into
  `buildPiEnv`, `runPiBillingGuard` and `buildPiModelsJSON`.

One is doubtful: `watch.opsmonitor_target` is genuinely assigned in
`cmd/harmonik/resolve_watch_config.go` (not merely presence-gated). Whether anything downstream
consumes it is **unverified**. Do not count it either way without checking.

### Re-checked key by key on 2026-08-04 — and the method was wrong

**The list above was built by grepping Go, and Go is not the only reader.**
`scripts/ops-monitor-check.sh` parses `.harmonik/config.yaml` directly with its own regexes, and
`internal/daemon/opsmonitor_schedule.go` `ensureOpsMonitorSchedule` schedules that script from
`internal/daemon/bootworkloop.go`. Three keys called inert here are read there. Grep the shell and
the Makefile before you call a config key dead.

**Move OUT of "Still ignored" — these are live, and the consumer is the shell script, not Go:**

- `watch.absent_thresh_s` — read into `WATCH_ABSENT_THRESHOLD` and gates the watch-down check.
- `watch.stall_ticks` — gates the watch-stalled miss count.
- `watch.opsmonitor_target` — the doubtful one resolves LIVE. It becomes `WATCH_OPSMONITOR_TARGET`
  and is passed as `--to` on every watch-class comms send. The Go path this file looked at,
  `ResolveWatchTargets`, is the dead one.

**Move BACK INTO "Still ignored" — the 2026-07-30 strike-out was wrong:**

- `keeper.cadence.no_gauge_backoff` — the resolver and `keeper_cmd.go` do pass it through, which is
  what the sweep saw. **The destination is dead.** `internal/keeper/watcher.go` says in its own
  source that the field "is retained for config backward compatibility" and is "no longer
  consumed". `applyDefaults` fills it, and no conditional reads it. The only other use prints it on
  a status line.

**ADD to "Still ignored" — found on 2026-08-04, not in the original list:**

- `watch.staffing_starvation_grace` — **a LIMIT with no implementation.** An operator sets it
  expecting the watch to escalate after N ops-monitor digests of a ready lane with a free slot and
  no staffing action. Nothing implements it. The only Go reader is a `> 0` presence gate inside
  `checkMissingWatchValues`, which is itself test-only, and the shell script never mentions
  starvation. The staffing backstop does not exist.
- `keeper.self_service.grace_seconds` — copied to `WatcherConfig.SelfServiceGraceSeconds` and never
  read. Its own source comment admits it is "threaded for completeness".
- `keeper.warn_messages.crew_defer_text` — copied to `WatcherConfig.CrewDeferText` and re-assigned
  on reload. No selection function reads it. There is no crew analogue of `selectLeaderDeferText`.

**Confirmed still inert, unchanged:** `harnesses.pi.provider_slots` (`RunRegistry.LenForProvider`
exists but is test-only, so per-provider concurrency is still unbounded),
`stall_sentinel.escalation.*`, `stall_sentinel.detection.*`, `keeper.timings.max_boot_grace_total`,
`keeper.self_service.instruct_only_when_idle`, `keeper.hard_ceiling.cooldown`,
`sandbox.network.mode`. Confirmed now live, unchanged: `daemon.target_branch`,
`harnesses.pi.profiles.*.api_key_file` — though the reader is
`internal/daemon/dot_cascade_core.go`, not `workloop.go` as the 2026-07-30 note said.

**Two fail-loud config gates never run.** `checkMissingWatchValues` and
`ResolveStallSentinelConfig` both have test-only callers. Every "fail-loud when unset" promise in
the `watch:` and `stall_sentinel:` doc comments is unenforced. Two watch keys survive by accident,
because the shell script carries its own fail-loud check.

**One new DANGEROUS config defect — `ResolvePiConfig`'s return value is discarded.**
`cmd/harmonik/main.go` calls it as `if _, piCfgErr := ResolvePiConfig(piBlock, projectDir); …`, so
it runs for validation only. That function is the SOLE place a profile-level `api_key_file` gets
its `~` expanded, and `internal/projectconfig/projectconfig.go` says so in a comment. A profile
written as `api_key_file: ~/keys/foo` therefore reaches the launch path unexpanded. Combined with
the already-recorded fail-open in `resolvePiAPIKeyValue`, the daemon silently falls back to the
ambient environment instead of refusing — which is exactly what the template says must not happen.
The top-level `harnesses.pi.api_key_file` is safe, because `parseHarnessesBlock` expands it
separately.

**Re-pin the key count at 123, and fix the command.** The baseline table's command uses `[a-z_]+`,
which drops digits and collapses `tier1_crew`, `tier2_captain` and `tier3_operator` into one token
`tier`. With `[a-z0-9_]+` the true figure is **123 distinct keys** across 141 tag occurrences.

---

## Assessment

> ### Re-assessed 2026-08-04 — the shape held, the scope did not
>
> **In 249 commits, not one finding in this file closed because someone wired it up.** Three rows
> narrowed, one half-closed, one got worse, and two tail claims went false. Every other open row
> re-verified as written. The nine never-imported feature packages are the same nine. The 32
> unreachable packages are the same 32. Test mass grew from 1.44x production to 1.49x.
>
> **The measurement to trust from now on is `deadcode ./cmd/...`, not a grep.** It does
> whole-program reachability from every `main` and reports **634 unreachable functions**. Retire
> the exported-symbol percentage in favour of that number, because it is a call graph rather than a
> text match, and because anyone can re-run it.
>
> **The spec-orphan number is settled.** `plans/2026-07-27-delete-and-rewrite/spec_orphans.py`
> reproduces it from the repo alone: **323 orphans of 1,178 requirement IDs, 27.4%** at
> `2cf3f0254`. The 2026-07-30 measurement of 27.5% is the one this method reproduces, and it is
> the one to cite. See CHARTER.md for the method and for its three limits — this counts citation,
> not implementation.
>
> > **This paragraph said 26.1% and told the reader to drop 27.5% as an outlier. Both were
> > wrong, and by the same bug.** The script compared word-bounded against the specs but by bare
> > substring against Go, so an open question like `OQ-EM-006` marked `EM-006` implemented.
> > Matching the two sides moves the rate to 27.5%, which is the figure the paragraph wanted
> > retired. The claim that the method matched `SPEC-TRIAGE.md` on 12 of 27 rows was computed
> > with the bug and has not been recomputed; treat it as withdrawn.
>
> **The one real change to this document is its scope, and it is the largest finding of the sweep.**
> The exclusion at the top — "dead code with zero callers anywhere is OUT of scope" — hid **21 dead
> protections** plus a further set of **live readers with no producer at all**: four event types a
> live consumer subscribes to that nothing ever emits, five files a live path reads that nothing in
> this repo writes, and five guard fields that production never assigns. That is comparable in size
> to the whole ranked table above, and it was invisible by construction. See the new section
> "Zero-caller writers and orphaned consumers". The lesson generalises: **an audit that asks "is
> this finished?" will not find a wire that came loose after it was finished.** Ask instead "does
> the reader of this signal have a producer?"
>
> **Two of those trace straight back to rows already in this file.** `core.Subscription.Since` is
> assigned in none of the 19 production subscription registrations, so the EV-014d startup replay
> is a no-op — a PARTIAL cause of the spend cap resetting on every restart, alongside the missing
> `RehydrateAccrual`. Neither alone explains it, and section 5 says why they do not compose. And
> `handlercontract.SpawnWatcherConfig.NodeType` is never set, so an HC-061 MUST NOT that the code
> correctly implements can never evaluate true — dead redundancy, because the path it guards does
> not exist.
>
> **Two of the 21 must not be "fixed" by wiring them.** `LockFromManifest` is a decoy that should be
> deleted, and `sync_assets_cmd.go` explains why: stamping the lock from the manifest would bury a
> `.harmonik-new` conflict forever. And `ValidateTrailerValue` sits over a trailer registry scoped
> to checkpoint commits, not to this repo's dev commits, so the registry's purpose has to be decided
> before any lint is built on it.
>
> **A correction worth reading, because it is the shape of error this class invites.** An earlier
> draft of this section claimed `kill -USR1` kills the daemon. It does not — Go catches and ignores
> a `_SigNotify` signal that nothing registered for, and an empirical test confirmed the daemon
> survives. The draft also cited two documents as advertising SIGUSR1 as the resume verb, and both
> in fact list it under an *Out of scope* heading as deferred. **A dead protection is easy to
> over-dramatise.** Check what the believer actually says, not what its presence suggests.
>
> **Two method traps for the next sweep.** First, a Go-only grep understates config liveness —
> `scripts/ops-monitor-check.sh` reads `.harmonik/config.yaml` directly, and three keys this file
> called inert are live there. Second, a symbol EXISTING is not a symbol being CALLED, and a caller
> that is itself only reachable from tests does not close a row. Both traps produced wrong entries
> in earlier passes of this file.

**Roughly a fifth to a quarter of the declared system is in this state.** That was the audit's
headline, and it rested on four measures: 23.0% of exported symbols with no cross-package
production reference; 41 of 182 event types registered and never produced or consumed; ~16% of
config keys inert; and 46 of 113 packages unreachable from any `main`.

**All four were re-measured on 2026-07-30 and all four moved. Two moved up, one moved down.**
Exported symbols with no cross-package production reference: **42.3%** (1,337 / 3,162) by a
stated method, against an unreproducible 23.0%. Event types never live outside the registry:
**52 / 182 (28.6%)**, up from 41. Packages unreachable from any `main`: **32 of 102**, down from
46 of 113 — but that fall is deletion, not wiring, and only 9 of the 32 are real features. The
config-key figure has not been re-derived as a percentage; three of the eleven named keys are now
read, so treat ~16% as an upper bound.

The headline is therefore too low, not too high. "A fifth to a quarter" should read **a quarter to
two fifths**. The shape of the claim survives: it is not a uniform haze, it is concentrated, and
the concentration is the finding.

**The partial implementations cluster in three places.** First and largest, **the entire
policy/control-point/enforcement layer (S01/S02/S05) is unwired end to end** — policy documents
are never parsed, the ControlPoint registry is never populated, the hook subsystem has no
importer, and the composition root installs a `NoOpPolicyEngine` that returns "permitted" and is
then discarded on the very next line. Gates, guards, budgets, roles, clearances, freedom
profiles, egress, skills resolution and hook side-effects are, at runtime, collectively a no-op,
while `specs/control-points.md` asserts *"No requirement is deferred."* Second, a **requirement-
per-file cohort** across workspace-model, reconciliation and handler-contract (WM-013a, WM-019,
WM-022..024, WM-040, WM-063, RC-002a/018/019/025a/026a, HC-004/043/045/046-050/055/070): each
requirement got its own file, its own tests, and its own bead, and none got a call site. A large
share of production files are named for a single bead ID — the audit put it at 187 of 864 (22%)
without recording its pattern, and a stated regex now gives 130 of 897 (14.5%); the two are not
comparable, so use whichever you can re-run. Either way that decomposition is precisely the
mechanism by which a feature reaches "compiles, tested, bead closed" without ever being
integrated. Third, the **operator-observability surface** — dashboard, usage/cost, structured
logging, the watch tier, replay, and the Layer-A stall detector — is uniformly reader-without-
writer or writer-without-reader.

**By era there are two waves.** May–June 2026 produced `hooksystem` (763 prod / 2,785 test LOC),
`structuredlog` and `watch` — two months old, never wired. July 2026 produced the twin/reactor/
parity/replay cluster (`codexreactor`, `codexdigitaltwin`, `keepertwin`, `twinparity`, `replay`,
Jul 11–16) and, on Jul 24–28, `reviewcycle` and `continuity`. The last two were a different
category — deliberately harvested from an abandoned branch one day before this audit, knowingly
parked, not forgotten. **They are now gone: commit `3cec5afd7` deleted both packages along with
review-loop mode.** Scope only the first two waves.

**The single most dangerous artifact of the terminology removal** is not in code but in specs.
Commit `72215ba69` mechanically stripped a scope qualifier from 197 files, which turned
*"No requirement is deferred **at MVH**"* into *"No requirement is deferred."* in six reviewed
specs — control-points.md:1158, architecture.md:521, handler-contract.md:1472,
scenario-harness.md:891, workspace-model.md:1208 and reconciliation/spec.md:892. Those six
sentences are load-bearing for anyone sizing remaining work and every one of them is false at
HEAD. Related strips turned sequencing notes into permanent architectural exclusions (ON-025
egress *"deferred post-MVH"* → *"is deferred"*, hiding that its non-deferred propagation half is
also absent; CP-058 role activation; AR-025 reserved identifiers). One case runs the other way:
`beads-integration.md` still calls BI-010d deferred when it shipped. **Treat the conformance
section of every reviewed spec as unverified.** Test mass is not evidence here — the nine
never-imported feature packages carry **7,854 lines of tests over 4,943 lines of implementation**
(re-measured 2026-07-30; the audit said 8,543 over 6,056), and the parity gates that would have
caught the twins drifting compare their fixtures to themselves.

**One caution about the spec line numbers in this paragraph.** The six citations above
(`control-points.md:1158`, `architecture.md:521`, `handler-contract.md:1472`,
`scenario-harness.md:891`, `workspace-model.md:1208`, `reconciliation/spec.md:892`) were not
re-checked in the 2026-07-30 sweep and the tree has moved 122 commits. Treat them as
**unverified** and grep for the sentence, not the line.
