# M5 Slice 2 — `internal/policy` extraction: design

> Read-only design pass (Plan agent, 2026-07-16), reviewed by implementer-orchestrator.
> Depguard `policy` edge (`.golangci.yml:450`): `$gostd` + `internal/core` ONLY (no eventbus/uuid/handlercontract/daemon).
> Discipline template = slice 1 (`internal/hook`, commit 3db50f1d) + `internal/mergeq` + `internal/runexec`.

## Decisive cut: split into sub-slices A → B → C. Do NOT do one big move.

The five candidate files are overwhelmingly effect-shell. The pure-decision harvest is small, fragmented, and owned by three different subsystems (pause controller, quiesce arbiter, gate evaluator).

- **`verdictexecutor_rc025a.go` — STRIKE.** Its pure logic already lives in `internal/core` (`ve.Valid()`, `core.CheckVerdictStaleness`, `core.PlanForVerdict`, `core.VerdictExecutionPlan`). The daemon file is 100% effect (git commits, `uuid.NewV7`, bus emits, `brcli` calls, `time.Now`). Nothing pure left to move.
- **`handlerpause_persist_m0k0a.go` — STAYS whole.** All I/O + on-disk `handler-state.json` schema (v2, HP-072). Frozen format.

### Sub-slice A (FIRST — recommended, low risk): pause predicates
- `policy.StepRateLimit(state, event, threshold) → PauseVerdict{Trip, NewState}` — the rate-limit hysteresis reducer (from `handlerpause_policy_37zy8.go`; `rateLimitHysteresisCount` + the `switch payload.Status` counter/trip test + budget-exhausted always-trip).
- `policy.BackoffDuration(AutoResumeParams{Base, Attempts, MaxBackoff}) → time.Duration` — the `after * 2^attempts` capped computation (from `handlerpause_autoresume_0otqs.go` `backoffDurationLocked`, de-`Locked`d) + `AutoResumeConfig`/`effectiveMaxBackoff` defaulting.
- Daemon boundary projects `core.AgentRateLimitStatus` → `RateLimitEvent{Cleared,Active}` BEFORE calling policy (keeps `uuid`/payload out of policy). Clock (`time.Now` for `TrippedAt`), `RunRegistry` freeze-list (`buildInFlightList`), and `Controller.Pause`/`Schedule` all STAY in the daemon. NO closure needed — value-in/value-out, the mergeq `critical func` shape.
- **Frozen-contract risk: NONE for A.** Reducer returns only `Trip`; daemon still builds the `HandlerPaused`/`Resumed` bus payloads. `queue.HandlerPauseChecker` iface intact (controller stays).
- **Tests:** split `handlerpause_policy_37zy8_test.go` — the 4 hysteresis truth-table tests migrate to `package policy_test`; `InFlightFreezeListPopulated` (RunRegistry-sourced) stays in `package daemon`. Split `handlerpause_autoresume_0otqs_test.go` — backoff-math migrates; `Schedule`/`doAutoResume`/timer/epoch cases stay.
- **Est:** impl ~90–130 LOC (`ratelimit.go` + `autoresume.go` + `doc.go`); tests ~200–280 LOC.
- **Debt shave — HONEST NOTE:** A shaves cognit on `handlerpause_policy_*` / `autoresume_*`, **NOT** on a grandfather giant. `startWithHooks` and `handleSocketConn` are UNCHANGED by A. A proves the `policy` package + hard edge; the grandfather-giant shave comes from sub-slice B.

### Sub-slice B (second, only after A lands clean): quiesce / drain classification
- `policy.ClassifyDrain(snapshot) → DrainState` + `policy.SleepVeto(snapshot) → []reason` — from `quiesce.go` `vetoCheck` strand logic + `draindetect.go` `GenuineDrain` / `stategather.go` `hasLatentWork`.
- **Structural blocker:** these want `FleetFacts`, which lives in `internal/daemon`, not `core`. Two options:
  - **B2 (recommended):** define a narrow `policy.DrainSnapshot` (the ~8 scalar counts the predicates actually read: ReadyCount, InProgressCount, RegistryRuns, LiveWorktrees, QueuedCount, PausedQueues, FailedArchives, BlockedByOpenEpic, Unsure); daemon projects `FleetFacts → DrainSnapshot` at the call site. Zero ripple.
  - **B1:** move `FleetFacts` + axis sub-types into `internal/core`. Cleaner architecturally, larger ripple (`stategather.go`/`socket.go` references).
- **This is the sub-slice that shaves toward a grandfather giant** (drain classification collapses into a `policy.` call). Higher risk → its own review + FleetFacts decision sign-off. DEFER.

### Sub-slice C (third, thin): gate predicates
- `policy.ParseGateVerdict([]byte) → core.GateAction`, `policy.MechanismDecision(bool) → core.GateAction` (true→Allow/false→Deny), `policy.GateEvalFailureOutcome(reason) → core.Outcome`, `gateExprEnv` value type — from `dot_gate.go`.
- Entangled-adjacent to the DOT-cascade launch surface (M2/orchestrator-adjacent) — keep to the 3 pure funcs, do last.

## M2/M4 coupling: verified NONE across all five candidate files.

## Recommended implementation order
1. Scaffold `internal/policy` (`doc.go` purity contract mirroring `hook/doc.go`; depguard rule already staged — no `.golangci.yml` change).
2. Sub-slice A: `policy/ratelimit.go` + `policy/autoresume.go`; rewrite the two `handlerpause_*` files as thin shells calling the pure funcs (the `hookrelay_chb025.go` pattern).
3. Migrate the pure tests to `package policy_test` (split the two test files per §A).
4. Verify: build/vet/daemon-suite green; depguard proves `policy` imports only `$gostd`+`core`; record the cognit delta on the two `handlerpause_*` files.
5. STOP and review before B/C. B carries the `FleetFacts` location decision → separate review.
