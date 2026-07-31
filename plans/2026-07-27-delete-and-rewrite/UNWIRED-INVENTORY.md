# Partial-Feature Inventory — harmonik @ bba60dd37

Read-only audit. Target: production code that LOOKS implemented but is not reachable or not
finished. Dead code with zero callers anywhere is OUT of scope; this is work that was STARTED
AND NOT FINISHED. No compilation was performed; every claim is grep/read evidence, plus the
on-disk `events.jsonl` (~250k events) and `session-data.jsonl` (449 records) where cited.

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

**Re-measured 2026-07-30.** This audit is pinned to `bba60dd37`. That commit is an ancestor of the
current tip, and the tree has moved a long way since — `git rev-list --count bba60dd37..HEAD` gives
the distance. The package count fell because `3cec5afd7` retired review-loop mode and deleted
`internal/runloop/reviewcycle/` and `internal/runloop/continuity/` with it.

Each row now carries the command that produces it. Re-run the command rather than trusting the
number.

| Measure | Value @ HEAD | as audited | Command |
|---|---|---|---|
| Production Go LOC | **214,117** | 214,511 | `find . -name '*.go' -not -name '*_test.go' \| xargs cat \| wc -l` |
| Test Go LOC | **307,741** (1.44x production) | 300,627 (1.40x) | same, with `-name '*_test.go'` |
| Go packages | **102** | 113 | `go list ./... \| wc -l` |
| Packages import-reachable from any `main` | **67** | 67 | `go list -deps ./cmd/... \| grep '^github.com/gregberns/harmonik' \| sort -u \| wc -l` |
| Packages NOT import-reachable from `./cmd/...` | **35** | 46 | the two rows above, subtracted |
| Packages NOT import-reachable from ANY `main` | **32** | — | `comm -23` of `go list ./...` against `go list -deps` of every `main` package |
| Prod / test LOC in never-imported feature packages | **4,943 prod / 7,854 test** across all 9 (`hooksystem` 763/2,785, `replay` 1,091/741, `watch` 699/1,208, `structuredlog` 498/695, `twinparity` 662/702, `codexdigitaltwin` 449/262, `codexreactor` 340/388, `keepertwin` 427/272, `specaudit` 14/801) | 6,056 / 8,543 | `find <pkg> -name '*.go' -not -name '*_test.go' \| xargs cat \| wc -l` per package |
| Exported symbols with NO cross-package production reference | **1,337 / 3,162 (42.3%)** | 759 / 3,304 (23.0%) | see the method note below |
| Declared `EventType` constants not live outside the registry | **52 / 182 (28.6%)** | 41 / 182 (22.5%) | denominator `grep -cE '^\tEventType[A-Za-z0-9_]+' internal/core/eventtype.go`; numerator = constants with no whole-word match in a non-test file outside `eventtype.go`, `eventreg*.go`, `pertypecompat*.go`, `eventbus/busimpl.go` |
| `projectconfig` distinct YAML keys | **121** | ~122 | `grep -rhoE 'yaml:"[a-z_]+' internal/projectconfig/*.go \| sed 's/yaml:"//' \| sort -u \| wc -l` |
| Production `.go` files | **894** | 864 | `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' \| wc -l` |
| Production files named for a single bead ID | **129 / 894 (14.4%)** by the regex at right — NOT comparable to the audit, which did not record its pattern | 187 / 864 (22%) | `find . -name '*.go' -not -name '*_test.go' -not -path './.git/*' -exec basename {} \; \| grep -cE '_[a-z0-9]*[0-9][a-z0-9]*\.go$'` |

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
| 1 | **Failing eval runs are not merged.** `eval-bead.dot` declares `close-pass` and `close-fail`; a failed grader closes without landing. | `dotTerminalNodeIsSuccess`, `internal/daemon/dot_cascade_helpers.go:716` | Terminal disposition is a single-literal denylist `terminalID != "close-needs-attention"`, not a lookup of the graph's declared terminals. | Verified directly. `close-fail` (`eval-bead.dot:29`), `record-fail`, `plan-needs-attention`, `gate_fail` all evaluate **true** → `dot_cascade_core.go:1016` → `workloop.go:4183` merges + closes. `internal/daemon/moderesolve.go` routes `codename:eval` beads here. **Count corrected 2026-07-30: 15 beads carry that label, not 55, and all 15 are closed.** The routing is an exact-match on the label, and the code comment says so ("exact-match, not prefix"). The 55 swept in `codename:eval-program` (30), `codename:evalvol-remote` (12) and `codename:eval-harness` (5), which this path does not route. The defect is real; the live exposure is not. |
| 2 | **Policy, roles, gates, guards, budgets and hooks are enforced** — control-points.md line 1158: *"No requirement is deferred."* | `core.NoOpPolicyEngine` at `cmd/harmonik/main.go:961`; `ParsePolicyDocument`, `NewS02Registrar` | The engine is constructed then **discarded on the next line** (`_ = policyEngine`). `Evaluate` returns `{Permitted: true}` unconditionally; `Registry()` returns an empty map. | `ParsePolicyDocument` has **zero references repo-wide, including tests**. `NewS02Registrar` zero callers. Only `core.PolicyDocument` construction sites are 3 literals in `workflow/loader_test.go`. `gate_allowed/denied/escalated`, `guard_failed/reordered`, `control_points_registered` never emitted. |
| 3 | **The S05 hook system delivers side-effects at-least-once (at-most-once for non-idempotent).** CP-012..CP-017, CP-040, CP-042. | `internal/hooksystem` (763 prod / 2,785 test LOC) | Whole package. Also duplicated, equally uncalled, in `core/cp017_hook_cognition_s05.go`. | **Zero non-test importers** (verified). No delivery-receipt store exists (`rg 'deliveryReceipt'` → 0 non-test). `fireMechanismHook` emits and stops: `TODO(deferred): apply the side effect`. Live `events.jsonl`: **0** `hook_fired` / `hook_failed` across ~250k events. |
| 4 | **Budget and spend ceilings deny a dispatch that would exceed the limit** (ON-045/047/048, CP-022). | `CheckBudgetAtDispatch`, `TightestBudget`, `CheckWallClockOuterBound`, `newBudgetCounterState` | Any dispatch-time budget consultation. `budget_ref` is parsed into the AST, non-empty-checked, and dropped. | All zero callers outside `internal/core`; the four `hka8bg*` files have zero tests too. What runs instead: a per-day bytes/max-runs proxy (`spendmeter_hkk3f8g.go`) + a review-retry counter. `budget_warning` has no producer — first signal is the hard stop. |
| 5 | **Every queue mutation is crash-atomic** — QM-060: *"All queue mutations MUST execute through the single QueueStore transaction owner."* | `internal/queue/transaction.go` (1,005 lines) via `queuewiring.QueueStore.Transact` | **The dispatch reservation, since 2026-07-30** — `internal/daemon/scheduler_reservation.go` `reserveQueueItem`. The remaining `queue.Persist` sites in `scheduler.go` are still bare. | **Re-counted 2026-07-30 (staleness sweep).** `.Transact(` now has **2 non-test call sites, both in `internal/daemon/scheduler_reservation.go`** — `reserveQueueItem` and `failQueueItem` — plus 17 test sites. The earlier "10 sites, all in `queuewiring/store_transaction_test.go`" and "that is ONE caller" are both superseded. Live path is still bare `queue.Persist`: **6 calls in `internal/daemon/scheduler.go`** (not 10) and **0 in `workloop.go`**. Command: `grep -rn 'queue\.Persist(' --include='*.go' . \| grep -v _test.go`. That gives 18 external call sites across 9 files; adding the in-package callers in `internal/queue/persistence.go` and `internal/queue/rpc.go` makes **11 files**, which is the number the audit reported and it still holds. Callers include RPC handlers, startup recovery, the spend meter, crew start, eager fill, and `cmd/harmonik/run.go`. `Persist` is per-file atomic but has no generation guard, no replace-intent, no archive-handoff binding, no quarantine. `persistence.go` doc-trailers itself `QM-001` — the requirement it bypasses. **Update 2026-07-30:** the dispatch stamp now goes through `Transact`, and `Transact` gained a `Precondition` hook plus quarantine-on-any-I/O-failure so it satisfies all three QM-001 obligations. Wiring the remaining `Persist` callers is what closes this row. Anything that mutates a queue whose ID is not a canonical UUIDv7 will be REJECTED before I/O — that is spec-correct and it is what broke five fixtures here. |
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
| 17 | **Token/cost accounting covers all harnesses.** | `internal/sessiondata/sessiondata.go` `readTranscript` | Codex/pi parsing (`if type != "assistant" { continue }`); `TokensTotal` is a value not a pointer, so absent data marshals as `0`, not `null`. | Live `session-data.jsonl`: **213/449 records all-zero, 63 null-cost, including 33 successful sonnet runs at `cost_usd: 0`**; `harness` empty on 448/449. The upstream data exists unread (`harness/codex/jsonlparser.go` already parses `codexTokenUsage`). The `WARNINGS (%d coverage gaps)` channel is only ever fed by a file-read error. |
| 18 | **`landing_strategy: squash\|cherry-pick` controls how work lands** (WM-019, written into every project by `harmonik init`). | `landTaskBranch`, `squashLanding`, `cherryPickLanding` | Any caller. | All zero non-test callers; `internal/runmerge/merge.go` contains no `squash` and no `cherry` — it is `git rebase` + `update-ref`. Parse, validate and three-tier resolve are all complete, so an invalid value is rejected and a valid one is ignored. Sibling keys `start_from`/`lands_on`/`protect_branches` **are** wired. |
| 19 | **Handler subprocess egress is restricted to a whitelist** (ON-025, HC-048b — the propagation half was never marked deferred). | — | The concept entirely. | `rg 'EgressWhitelist\|egress_whitelist' -g '!*_test.go'` → **0 hits**; `rg 'HC-048b' -g '!*_test.go'` → **0 hits**. `LaunchSpec` has no egress field. |
| 20 | **Skills declared on a node are resolved and provisioned** (HC-046..HC-050); `skills_provisioned` reports `rejected_skills[]`. | `handlercontract.ResolveSkill`, `ResolveAllSkills`; `LoadDotWorkflowWithPolicy` | Resolution on the live path. Both pre-exec paths pass `nil` skills. | Every real session emits a well-formed affirmative `skills_provisioned{skills: []}`. `internal/harness/claude/launchspec.go` holds `_ = ctx // reserved for future async steps (e.g. skill provisioning)`. `skills_resolved` has no producer — `core.EventTypeSkillsResolved` appears only at its own declaration, and `workflow.LoadDotWorkflowWithPolicy`, which builds the payload, has zero non-test callers. **Correction 2026-07-30:** "emitted only by the twins" is too strong. `handler.PreExecMessages` (`internal/handler/claudehandler_chb006_024.go`) does build and emit a `skills_provisioned` pre-exec message on the live path. It always carries `Skills: nil`, because both production callers — `internal/harness/claude/launchspec.go` and `internal/daemon/harnessregistry.go` — pass a literal `nil`. The finding stands; the mechanism is an empty affirmative, not an absent emitter. |
| 21 | **Forbidden `claude` flags are refused** (HC-055: seven flags MUST NOT be passed, *"would silently shadow policy"*). | `forbiddenClaudeFlags`, `internal/handler/claudehandler_chb006_024.go:53` | Four of seven — the list holds only the three CHB-007 entries. `--permission-mode bypassPermissions` in `HandlerArgs` is forwarded verbatim. | Direct read. HC-055b also diverged: `isHarmonikManagedWorktree` falls back to the **unresolved** path on `EvalSymlinks` failure and adds a `strings.Contains(canonWS, "/.harmonik/worktrees/")` fallback — any path containing that segment earns `--dangerously-skip-permissions`. |
| 22 | **A rate-limited handler auto-resumes when the window clears.** | `ClaudeCodeAdapter.Diagnose`, `internal/handler/adapter_claudecode.go` | A real health check — it returns `DiagnosticReport{Healthy: false}` **unconditionally**. | Production wires exactly this adapter (`bootsocket.go` `SetAdapter`). `handlerpause_autoresume_0otqs.go` does `if report, ok := c.runDiagnose(ctx); ok && !report.Healthy { return }` → **always abandoned**. The whole Schedule/flap-backoff/hysteresis machine is unreachable; a pause persists until manual operator action (which is finding 10). |
| 23 | **claude-code account rotation.** | `Adapter.RotateAccount` (claude/codex/pi impls) | Everything. Precisely: declared in the contract, never built, **never called** — every call site is `_test.go`. | claude → `ErrSingleAccountOnly` with `// TODO: when multi-account rotation lands...`; codex/pi → `ErrDeterministic`. Safe (fails loud) rather than fail-open. Its sibling `DetectRateLimit` is also uncalled for every harness — which strands the *working* claude retry-after parser (`adapter_claudecode.go:117-133`). |
| 24 | **Structured NDJSON logs from every subsystem, rotated at 100 MiB/24h** (ON-035, a MUST). | `internal/structuredlog` (498 prod LOC) | Adoption. | **Zero non-test importers.** 973 `log.Printf` / `fmt.Fprintf(os.Stderr, …)` sites in non-test code (`workloop.go` alone has 89). `.harmonik/logs/` is never written. |
| 25 | **Replay substrate** (`specs/replay-substrate.md`, written in runtime MUST language) and **the watch tier** (an always-on ledger + escalation engine; the `watch` skill ships in the binary). | `internal/replay` (1,091 LOC), `internal/watch` (699 LOC) | Any production importer; and for watch, a `harmonik watch` command. | Both have **zero non-test importers** (verified). There is no `watch` subcommand — only `resolve_watch_config.go`, whose `ResolveWatchTargets` and fail-loud boot gate `checkMissingWatchValues` are themselves test-only. The shipped skill instructs an agent to run something the binary cannot do. |
| 26 | **Resume-hang is bounded and recovered** (HC-070, HC-INV-008: driver emits ack on acceptance, stale on timeout). | `agent_input_acked` / `agent_input_stale` | Producers on **both** substrates. tmux: `SubmitInput` writes then `return Ack{Delivered}` with no timer. Codex: `codexdriver.Options.Emit` is never set at the composition root. | `grep -rn 'EventTypeAgentInput' --include='*.go' .` → **0 hits repo-wide** (re-verified 2026-07-30). Sharper than the audit put it: the constants do not exist at all. Registration in `internal/core/eventreg_hqwn59.go` uses bare string literals `"agent_input_acked"` / `"agent_input_stale"`. Types are registered and never published. `codexdriver` `Options.Emit` is read in `internal/codexdriver/session.go` but assigned only in `driver_test.go` — `cmd/harmonik/substrate_select.go` `codexSubstrateOptions` sets Args, Runner and Clock, never Emit. |
| 27 | **Daemon readiness contract** — `daemon_ready` after five PL-009 criteria; pre-ready requests rejected (PL-003b). | `lifecycle.ReadyCriteria`, `PreReadyGate`, `MarkReady`, `CheckRequest` | All of it. | Zero references outside their own file — **not even a test**. `daemon_ready` is registered in 4 places (`eventreg`, `busimpl` allow-list, `pertypecompat`, `eventtype.go`) and emitted nowhere. Verified. |
| 28 | **Agents report outcomes / claim beads over the daemon socket** (handler-contract.md builds a retry protocol on this). | `SocketHandlers.Request = &noopRequestHandler{}`, `bootsocket.go:300` | The implementation. `noopRequestHandler` is the **only** implementation in the repo. | Verified. `emit-outcome` and `claim-next` **are** registered in the router (`socketdispatch.go`) and reach a handler returning `errors.New("daemon: RequestHandler not wired yet")`. No client sends either op (27 distinct ops scanned). Both dereference `d.h` unguarded where every other op nil-checks. |
| 29 | **Machine-level agent ceiling** (ON-041c: env var, count file, `dispatch_deferred`, cross-daemon decrement). | — | Everything. | `rg 'HARMONIK_MACHINE_AGENT_CEILING\|machine-agent-count'` → **0 hits**. `DispatchDeferredReasonMachineCeilingExhausted` is a constant with no emitter. |
| 30 | **`kill -USR1 <pid>` resumes all paused handlers** (handler-pause.md §1.2). | `daemon.NewSignalResumeWatcher` | The goroutine. Its doc says *"Designed to run in a dedicated goroutine started by daemon.Start"*. | Zero callers. `signal.Notify(…SIGUSR1)` never runs, so the signal takes Go's default disposition. |
| 31 | **Agent manifests declare scheduled `triggers:`** (`.harmonik/agents/captain/manifest.yaml` has `{id: fleet-status, every: 6h, enabled: true}`); `harmonik agent check` reports ok. | `agentmanifest.Trigger`, `ActiveTriggers` | A scheduler. `ActiveTriggers` is filtered then **printed** — the markdown/TOON renderers are its entire lifecycle. | Also `validateManifest` never checks `Harness` against `claude\|codex\|pi` and never inspects `Cardinality` — so `cardinality: {max: 1}` does not make the captain a singleton and `harness: cladue` passes. Only `Markers.NeverEmits` and `Lifecycle.Persistent` have consumers. |
| 32 | **The structured Codex driver** (`codexdriver`, `codexinput`, `apptap`, `sessioncapture`, `codexreactor`). | `cmd/harmonik/substrate_select.go:105` | An activator. `if os.Getenv("HARMONIK_SUBSTRATE") != "codexdriver" { return tmuxSub, … }` and the **only** assignment of that var in the tree is in a `_test.go`. | Dormant by construction. `codexreactor.New()` — a finished, invariant-documented state machine — has **zero production callers**; the live output path bypasses it. NB `internal/harness/codex` (codex under tmux) is a *different* and genuinely live path. **Two small corrections, 2026-07-30.** `codexreactor.New()` is called at **8** test sites across two files (`internal/codexreactor/reactor_test.go` and `internal/codexdigitaltwin/twin_test.go`), not two. And the env gate is a predicate helper in `cmd/harmonik/substrate_select.go`, not one inline `if`. Both details; the conclusion is unchanged, and the only assignments of `HARMONIK_SUBSTRATE` in the tree are still in `_test.go`. |
| 33 | **Twin-parity gates prove the twins match the real harness.** | `internal/twinparity` `AssertStreamEquivalent` | Cross-comparison. `TestPiParityGate` passes `piSampleNDJSON(t)` as **both** twin and reference; two of three Claude sub-tests are `Assert(t, durable, durable)`. | Signature takes `testing.TB` — production reachability impossible. `make test-twin-parity-pi` asserts nothing about twin-vs-real. Fixtures carry `hand_authored: true`, `capture_date: "PLACEHOLDER-REAL-BOX"`. |
| ~~34~~ | ~~**The composition-root wiring audit catches silent drops between versions**~~ **FIXED** (`HARMONIK_DEBUG_WIRING=1`) | `bootState.wiringAudit`, `internal/daemon/wiringlog.go` | Was the derivation: a hand-maintained 37-entry `[]wiringEntry` constant that printed identically no matter what was wired, with 0 of its 35 `daemon.go:NNN` call sites still landing on the code they named (the wiring had moved to `bootstate/bootsocket/bootworkloop`). The table and its zero-consumer `ExportedCompositionRootWirings` seam are deleted; the audit now reflects over the live `bootState` and reports each of the 19 singletons as constructed or ABSENT, so a dropped singleton shows up without anyone editing the file. **Scope narrowed, deliberately:** it detects dropped *constructed values* only — a dropped `bus.Subscribe`, `StartWatcher` call, or `workLoopDeps` field is a wiring action, not a singleton, and is still undetected. |
| 35 | **Socket bind failure is reported** (PL-003 defines exit 6 for a live-daemon collision). | `startSocketListener`, `internal/daemon/bootsocket.go:297-312` | Any handling: `go func() { <-socketDone }() // drain: non-fatal`. | The daemon then runs the work loop with **no socket** — `queue submit`, `crew start`, `state`, `dashboard`, `comms`, hook-relay all silently unavailable, zero diagnostic. |
| 36 | **`--codex-binary` selects the codex executable** (its godoc: *"used when the resolved harness is core.AgentTypeCodex"*). | `daemon.Config.CodexBinary` | A reader on the documented path. `harnessregistry.go:57` hardcodes `codex.NewHarness("", "")` → bare `codex` off PATH. | The flag *does* reach the dormant codexdriver substrate (finding 32), so it appears wired. The one path it reaches is the one its docs don't describe. |
| 37 | **Crew slots are reclaimed when a crew goes idle.** | `crewrun.CrewIdleReaper.StartWatcher` | The watcher body — it is an empty function; `loop`/`scan`/`checkCrew`/`reap` are `//nolint:unused`. | Constructed fully at `bootsocket.go:252` and started at `bootworkloop.go:238`. A documented 2026-07-18 operator disable, but the consequence stands. |
| 38 | **`harmonik queue resume` recovers a paused queue.** | `queuewiring.transitionToActive` | Handling for 2 of 3 pause states. `transitionToActive` skips anything that is not `paused-by-drain`. | `paused-by-failure` (`internal/daemon/scheduler.go`, `internal/lifecycle/startup_pl005_qm002.go`) and `paused-by-budget` (`internal/daemon/perqueuespendmeter_tigaf11.go`) are both produced live. `queue.ResumeFromFailure` / `RearmFailedItems` have zero callers and there is no `queue retry` verb. `paused-by-budget` stays wedged until UTC-day rollover. **Correction 2026-07-30:** "`HandleOperatorResume` returns nil regardless" is FALSE. `OperatorPauseController.HandleOperatorResume` (`internal/daemon/operatorpause.go`) returns wrapped errors from `json.Marshal` and from `bus.Emit`; it returns an early nil only for the idempotent already-not-paused case. The effect the row is reaching for survives by a different route: the `operator_resuming` event it emits is dropped downstream by the drain-only filter in `transitionToActive`, so the socket still answers OK on a queue that stays paused. |
| ~~39~~ | ~~**Review-cycle and continuity kernels**~~ **CLOSED — the code is deleted** | ~~`internal/runloop/reviewcycle` (652), `internal/runloop/continuity` (475)~~ | — | The audit was right that both packages had zero importers of any kind. They were harvested from an abandoned branch in `1b56dafb5` on 2026-07-28 and knowingly parked. **Commit `3cec5afd7` ("daemon: retire review-loop mode and delete its driver") deleted both directories**, together with `internal/daemon/reviewloop.go`. Verify with `ls internal/runloop/reviewcycle internal/runloop/continuity` → no such directory. Nothing to scope. |
| 40 | **`harmonik harness` runs the conformance scenario suite.** | `cmd/harmonik/harness.go` | `BrPath`/`KerfPath` — no flag supplies them, and `bootworkloop.go:30` is `if bs.cfg.BrPath == "" { return nil }`, which skips the entire PL-005 work loop and is the last statement of `daemon.Start`. | The registered conformance command boots a daemon that never dispatches, while `scenarios/smoke/checkpoint-and-merge.yaml` asserts daemon-side events only the work loop can produce. |

Also confirmed, lower severity: `LaunchSpec.HandlerSpec` is assigned only in tests, so `runIDStr`
falls back to the constant `"unknown"` for every live handler FSM; `VerifyTwinLaunch` (HC-045
commit-hash pin) is never called; `handler-pause` HP-015 counter never resets on resume; HC-056's
default is 5× the spec (150s vs 30s, with a comment asserting it *is* the spec default); HC-004
launch idempotency is marked "NOT ENFORCED" in source while the spec still says MUST;
`workspace/doc.go` still claims the package "contains only test files" against 7.8k prod LOC;
`make build-twin-claude` is an alias for `build-twin-generic`; `internal/scratchpad` ships toy
exercises (anagram, roman numerals) inside `internal/`.

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

Config keys that are parsed, validated, and then ignored — an operator sets a limit and gets none.
**Re-checked key by key on 2026-07-30. Three of the eleven are now FALSE and are struck out.**

Still ignored:

- `harnesses.pi.provider_slots` — per-provider concurrency runs **unbounded**. Parsed into
  `PiConfig.ProviderSlots`; zero readers outside `internal/projectconfig`.
- `stall_sentinel.escalation.*` and `.detection.*` — nothing detects, nothing pages, at any tier.
  `cmd/harmonik/resolve_stall_sentinel_config.go` uses them only as presence gates.
- `watch.absent_thresh_s` / `stall_ticks` — validated, never used.
- `keeper.timings.max_boot_grace_total` — the parsed field has no reader. The value keeper
  actually uses is the `2 × boot_grace` default computed in `internal/keeper/cycle.go`.
- `keeper.self_service.instruct_only_when_idle` — threaded all the way to
  `WatcherConfig.SelfServiceInstructOnlyWhenIdle`, where no conditional reads it.
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

---

## Assessment

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
without recording its pattern, and a stated regex now gives 129 of 894 (14.4%); the two are not
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
