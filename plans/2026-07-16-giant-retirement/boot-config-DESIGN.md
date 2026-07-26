# Giant retirement #1 — `boot-config` subsystem out of `startWithHooks`: design

> Read-only design pass (Plan agent, 2026-07-16). Named follow-up de-scoped from M5 per COORD c051/c052 (ruling A). Target: the FIRST giant, `startWithHooks` (`internal/daemon/daemon.go:774`, 1617 lines). Method mirrors the M5 orchestrator cut (COORD c050): design → adversarial seam review → worktree implementer → cherry-pick `-x` → re-verify green → independent agent-reviewer → trailer-stamp. **This design will be adversarially reviewed; every file:line claim below was verified against live source on 2026-07-16 at branch `phase1-session-restart-substrate`. Items I could NOT fully verify are flagged ⚑.**

---

## 0. TL;DR for the reviewer (read this first)

- **Two distinct deliverables, do not conflate them:**
  1. **`internal/bootconfig` — a new pure subsystem** (the testable seam). It absorbs the config-**resolution + validation** decisions currently inlined in `startWithHooks` (branching-defaults precedence merge, branch-protection fail-closed check, target-branch resolution, workflow-mode validation). Import edge `$gostd + internal/core`, exactly like `internal/policy` / `internal/orchestrator`. Surface is honestly **thin** (~4 pure funcs behind one umbrella `Resolve`).
  2. **Daemon-side phase-helper decomposition** (the length/complexity shrink). The 1617-line giant is a *linear boot sequence* of ~12 effectful phases. Extracting `bootconfig` alone shaves ~100 lines and a handful of branches — it does **NOT** retire the giant. Retiring it under the ceilings needs the phase helpers too. These stay in `package daemon` (composition-root wiring; not import-isolatable).
- **The honest headline:** `bootconfig` gives you the *testable seam* the brief asks for; the phase helpers give you the *shrink*. Both are "retire the giant." A reviewer who scores the subsystem alone against the giant's line count will (correctly) find it insufficient — that is why the sub-slice sequence below includes the daemon-side phases.
- **Depguard edge:** `bootconfig` → `$gostd + internal/core` (proof in §4). `internal/queue`, `internal/branching`, `internal/workspace` deliberately NOT allowed — inputs are projected to primitives/`core` types at the daemon boundary (same discipline as orchestrator's enum→string projection).
- **Package name:** codename is `boot-config`; the Go package is `internal/bootconfig` (package `bootconfig`) — Go package identifiers can't contain hyphens. Flagged so the depguard `files:` glob and import path are written without the hyphen.

---

## 1. Section-by-section map of `startWithHooks` (`daemon.go:774-2390`, verified)

Line ranges are inclusive and verified against source. "E" = effect (stays in daemon shell), "D" = pure decision (candidate to move to `bootconfig`), "H" = already an extracted helper call (grouping opportunity only).

| # | Block | Lines | Kind | What it does |
|---|---|---|---|---|
| P1 | **Pidfile acquire** | 784-808 | E | mkdir `.harmonik/`, getpid/getpgrp, UUIDv7 instance ID, `lifecycle.AcquirePidfile`, `defer Release`. Guarded by `cfg.ProjectDir != ""`. |
| P2a | **Workflow-mode validation** | 820-825 | **D** | reject empty (fail-closed, hk-81n9r) + reject `!mode.Valid()`. Pure over `core.WorkflowMode`. |
| P2b | **Branching-defaults precedence merge** | 837-848 | **D** (+E load) | `branching.Load` (E, file I/O) then fill `cfg.TargetBranch`/`cfg.ProtectBranches` only if zero-value (flag > yaml > builtin). The *fill rule* is pure. |
| P2c | **Branch-protection fail-closed (hk-sul12)** | 864-872 | **D** | `resolveTargetBranch("")→"main"`; two hard-error cases (ForbidUnprotectedDefault+empty; resolved target ∈ ProtectBranches). Pure over strings/slices. |
| P2d | **Conflict-cap validation (WM-024)** | 883-887 | E→D | delegates to `workspace.ValidateConflictResolutionAttemptCap` (already pure, already in `workspace`). **Leave in workspace** — do not re-home (see §2). |
| P2e | **Project config load (EM-012b)** | 895-901 | E | `LoadProjectConfig` (file I/O) → `cfg.ProjectCfg`. Effect. |
| P3 | **Pre-flight maintenance** | 903-948 | H | WAL checkpoint, `.br_history/` rotation, restart-backoff record (`applyBootBackoff`, returns delay slept later), beads merge-driver. All already helper calls; each self-guards on `ProjectDir`/skip-flags. |
| P4 | **Registry + bus construction** | 950-1035 | E | RedactionRegistry, JSONL writer open, event-ID HWM read + generator seed, `NewBusImplWithWriterAndHWM`, QueueStore, HandlerPauseController, RunRegistry. |
| P5 | **Pre-Seal subscriber wiring** | 1037-1269 | E | ~230 lines: pausePolicy, spendMeter, perQueueSpendMeter, queueOpConsumer, notifyStream, subscribeHub, staleWatcher, reviewGateWatcher, tunerBackstop, quiesceArbiter, substrate diagnostic hooks, CatBL2Handler, busObserver. Each `X.Subscribe(bus)` before Seal (EV-009). |
| P6 | **Seal + startup events** | 1271-1346 | E | `bus.Seal()`, clock-regression `daemon_degraded`, `staleWatcher.StartWatcher`, `daemon_started`, supervisor-revival detect, `daemon_config` emit. |
| P7 | **Orphan sweep + reconcile** | 1348-1659 | E | ~310 lines: br adapter + `CheckBrVersion` handshake, raw queue.json provenance read, tmux adapter extraction, `RunOrphanSweep`, `adoptDeadRunSessions`, `reconcileOrphanedRunsOnResume` (+ cached status reader), reconciliation_started/completed, CatBL1/CatBL3 sweeps. All under one `ProjectDir != ""` guard. |
| P8 | **Session keepalive** | 1661-1676 | E | optional `go sk.RunSessionKeepalive`. |
| P9 | **Adapter registry + hook store** | 1678-1724 | E | AdapterRegistry register claude/codex/pi, seal, `ForAgent`, `SetAdapter`, `newDaemonHookStore`. |
| P10 | **Startup state loads** | 1726-1782 | E | `loadStartupQueues` (H), handler-pause persist+load, decision-ack load. |
| P11 | **Socket-listener block** | 1784-2050 | E | ~270 lines: declares shared singletons (opPauseCtrl, concurrencyCtrl, queueHandlerAdapter, drainDet, crewHandler, crewIdleReaper, branchReapWatcher), builds QueueHandler adapter, ConcurrencyController, bandwidth tuner, comms handlers, crew handler, state/dashboard builders, poll-gate, and starts `RunSocketListenerWithDashboard`. All under `ProjectDir != ""`. |
| P12 | **Boot-backoff sleep** | 2052-2059 | E | `sleepBootBackoff(ctx, bootBackoffDelay)` — deliberately AFTER socket bind (hk-uzvt9). |
| P13 | **Work-loop deps + start** | 2061-2387 | E | ~330 lines: `newWorkLoopDeps` (H), sentinel governor config, emittedEpics/follow-up-ledger boot-seed, ~20 `deps.*` injections, `quiesceArbiter.Start`, crewIdleReaper/branchReapWatcher start, reconciliation scheduler, worker-report loop, staleWatcher force-reap seams, `runWorkLoop`, block on `loopDone`. Guarded by `cfg.BrPath != ""`. |
| — | `return nil` | 2389 | | |

**Structural read:** ~1450 of the 1617 lines (P4-P13) are effectful composition-root wiring — construct singleton, `Subscribe`/`Start`, inject. The ONLY pure decision logic is P2a/P2b/P2c (~35 lines of branching, plus the 5-line `resolveTargetBranch` helper). This is why the honest split is "one thin pure subsystem + many effect phase-helpers," and why an orchestrator-style extraction could never shrink this function (COORD c051 verified the same).

**Metrics (verified):** 1617 lines; within the range: **137 `if`, 47 `for`, 1 `switch`, 6 `case`, 55 `return`**; **27** `ProjectDir/BrPath/JSONLLogPath != ""` guard branches. Current ceilings (`.golangci.yml`): `funlen` 100 lines / 60 statements; `cyclop` max-complexity 15; `gocognit` min-complexity 20 (i.e. must be ≤20 to pass clean). The function is currently **grandfathered** by `--new-from-rev` (Makefile `check-fast`/`check-short`); it carries **no `//nolint`** for funlen/cyclop/gocognit today (only two `//nolint:gosec` on mkdir perms, unrelated). ⚑ The ratchet reports issues on changed lines relative to the base rev; because complexity issues anchor on the func-decl line, a body-only edit *may* not re-trigger them — but this design targets a clean pass on a full `golangci-lint run` of the shrunk shell, **not** reliance on ratchet ambiguity, and adds no `//nolint`.

---

## 2. The seam — what MOVES vs what STAYS, and why

### Moves into `internal/bootconfig` (pure, testable)

The **config-resolution + validation decision**, currently inlined at P2a/P2b/P2c and the `resolveTargetBranch` helper. Projected to primitives/`core` types at the daemon boundary so the package never imports `daemon`, `branching`, `queue`, or `workspace`:

- `ResolveTargetBranch(flag string) string` — moves `resolveTargetBranch` (`workloop.go:1312`, pure `"" → "main"`) into `bootconfig`. Daemon re-exports or calls through.
- `MergeBranchingDefaults(in BranchingInput) BranchingResolved` — the flag>yaml>builtin fill rule (P2b, `daemon.go:842-848`): fill target/protect only when the flag value is zero. Daemon does `branching.Load` (I/O) and passes the loaded `LandsOn`/`ProtectBranches` in as plain `string`/`[]string`.
- `ValidateBranchProtection(forbidDefault bool, flagTarget, resolvedTarget string, protect []string) error` — the two hk-sul12 hard-error cases (P2c, `daemon.go:864-872`), returning the same actionable error text the current tests assert on.
- `ValidateWorkflowMode(mode core.WorkflowMode) error` — the empty-and-`!Valid()` fail-closed check (P2a). Uses `core.WorkflowMode` (allowed edge).
- **Umbrella:** `Resolve(in Input) (Resolved, error)` — composes the four above in the exact current order (mode → branching merge → target resolve → protection validate). Returns a `Resolved{TargetBranch, ProtectBranches}` the daemon writes back into `cfg`. **This single function is the testable seam**: the daemon calls it once; every validation/precedence case that today requires `daemon.Start` to boot the whole process (`daemon_branchprotection_sul12_test.go`) becomes a pure table test.

### Stays in the daemon shell (effect), with rationale

- **All file/OS I/O:** `branching.Load`, `LoadProjectConfig`, pidfile acquire, HWM read, JSONL open — I/O is effect; `bootconfig` receives already-loaded values. (Mirrors orchestrator: daemon projects under the lock, pure fn runs on the snapshot.)
- **`workspace.ValidateConflictResolutionAttemptCap` (P2d):** already pure and already lives in its owning subsystem (`workspace/conflictresolution_wm024.go:72`) with its own tests. **Do NOT re-home it** into `bootconfig` — that would duplicate tested code and blur ownership. The daemon keeps calling `workspace.Validate…` directly. (Flagged because a reviewer might expect "all config validation" in one place; the envelope discipline says a validator lives with its subsystem's semantics.)
- **`core.WorkflowMode.Valid()` (P2a):** stays in `core`; `bootconfig.ValidateWorkflowMode` *wraps* it with the empty-check + error text, it does not reimplement `.Valid()`.
- **Everything P4-P13:** singleton construction, `Subscribe`/`Seal`/`Start`, `deps.*` injection, socket bind, work-loop launch. This is composition-root wiring — it imports every subsystem and holds live handles; it is inherently un-pure and un-import-isolatable. It shrinks via **daemon-side phase helpers** (§5), not via `bootconfig`.

### Why not a bigger `bootconfig`?

A reviewer will ask "why isn't pidfile / bus-construction / orphan-sweep in `bootconfig`?" Because those are effects with live handles (`*Pidfile`, `eventbus.Bus`, `*RunRegistry`) — moving them into a `$gostd+core` package is impossible (they need `internal/eventbus`, `internal/lifecycle`, `internal/queue`). A "boot-config" package that imports half the tree is just the daemon with a new name; it buys no testability and violates the depguard discipline the whole overhaul exists to enforce. The pure config-resolution decision is the *only* honestly-isolatable seam here.

---

## 3. Package layout + public API

Scaffold per the `go-subsystem-add` skill (doc.go + bootconfig.go + _test.go; depguard entry; sorted into `.golangci.yml` between `core` and `daemon`). **Note:** `bootconfig` is not in the S01-S09 table in `subsystem-organization.md`; per the skill's "if not listed, consult spec/user" rule this is a **NEW subsystem row** — flag for the operator/spec to add it to the matrix (it slots as a leaf, same layer as `policy`/`orchestrator`, `mayDependOn = {core}`). ⚑

```
internal/bootconfig/
  doc.go                  // package doc: purity contract, mirrors policy/doc.go + orchestrator/doc.go
  bootconfig.go           // Input/Resolved structs + Resolve + the 4 pure funcs
  bootconfig_test.go      // package bootconfig_test — table tests (migrated from daemon)
```

```go
package bootconfig // imports: $gostd + internal/core ONLY

// Input is the daemon's projection of the config fields the boot-config
// decision reads. Constructed daemon-side AFTER branching.Load / LoadProjectConfig
// (the I/O), carrying only primitives + core types — never daemon.Config,
// branching.Defaults, or queue/workspace types.
type Input struct {
    WorkflowMode             core.WorkflowMode
    FlagTargetBranch         string   // cfg.TargetBranch as supplied (flag/zero)
    YAMLLandsOn              string   // branchingDefaults.LandsOn ("" if none)
    FlagProtectBranches      []string // cfg.ProtectBranches as supplied
    YAMLProtectBranches      []string // branchingDefaults.ProtectBranches
    ForbidUnprotectedDefault bool
}

// Resolved is what the daemon writes back into cfg after a successful Resolve.
type Resolved struct {
    TargetBranch    string   // post-merge, post-default ("" → "main")
    ProtectBranches []string // post-merge
}

// Resolve validates the workflow mode, applies flag>yaml>builtin branching
// precedence, resolves the target branch, and runs the hk-sul12 fail-closed
// branch-protection check — in that order. Error text matches the current
// startWithHooks messages verbatim so existing assertions and operator-facing
// diagnostics are byte-identical.
func Resolve(in Input) (Resolved, error)

// Component predicates (also exported for focused tests / reuse):
func ValidateWorkflowMode(mode core.WorkflowMode) error
func MergeBranchingDefaults(in Input) Resolved
func ResolveTargetBranch(flag string) string            // "" → "main"
func ValidateBranchProtection(forbidDefault bool, flagTarget, resolvedTarget string, protect []string) error
```

Daemon call site (replacing P2a-P2c, ~35 lines → ~10):

```go
if cfg.ProjectDir != "" {
    bd, err := branching.Load(cfg.ProjectDir)   // effect (I/O) stays
    if err != nil { return fmt.Errorf("daemon.Start: load .harmonik/branching.yaml: %w", err) }
    in.YAMLLandsOn, in.YAMLProtectBranches = bd.LandsOn, bd.ProtectBranches
}
resolved, err := bootconfig.Resolve(in)         // pure decision
if err != nil { return fmt.Errorf("daemon.Start: %w", err) }
cfg.TargetBranch, cfg.ProtectBranches = resolved.TargetBranch, resolved.ProtectBranches
```

---

## 4. Depguard edge + proof

Rule to add to `.golangci.yml` (uncommented, active, sorted after `core`, mirroring `orchestrator:` at `:601-605`):

```yaml
        bootconfig:
          files: ["**/internal/bootconfig/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
```

**Per-function edge check:**

| Fn | Reads | Needs beyond `$gostd`+`core`? |
|---|---|---|
| `ValidateWorkflowMode` | `core.WorkflowMode` + `.Valid()` | No — `core` only. ✅ |
| `MergeBranchingDefaults` | strings, `[]string` | No. ✅ |
| `ResolveTargetBranch` | string | No — `$gostd` only. ✅ |
| `ValidateBranchProtection` | strings, `[]string`, `fmt` | No. ✅ |
| `Resolve` | the above + `Input`/`Resolved` | No. ✅ |

**Proof obligation for the implementer (run after scaffolding, before the daemon rewire):**

```
go list -deps ./internal/bootconfig/ | grep gregberns/harmonik   # expect ONLY .../internal/core (+ bootconfig itself)
go tool golangci-lint run ./internal/bootconfig/...              # expect zero depguard violations
```

The edge holds **only if** the branching/workflow values are projected as primitives/`core` types at the boundary (no `branching.Defaults`, no `queue.*`, no `workspace.*` in the signature). This is the identical discipline that let `orchestrator` stay at `$gostd+core` by projecting queue enums to strings (COORD c050). ⚑ If a future need pulls `internal/branching` in, the edge widens to `$gostd + core + branching` — but nothing in the four funcs above requires it, so the tight edge is achievable now.

---

## 5. Ordered, behavior-preserving sub-slices

Each slice is independently committable, green-verifiable alone (`go build ./...`, `go vet ./...`, targeted `go test`), and independently reviewable. **STOP + review between each**, per M5 discipline. B1 is the subsystem (the named deliverable); B2-B6 are the daemon-side phase decomposition that actually retires the giant.

### B1 — `internal/bootconfig` subsystem (the testable seam) — LOWEST RISK, HIGHEST VALUE
Scaffold the package (skill steps 1-5), implement the 4 pure funcs + `Resolve`, add the depguard rule. Rewrite P2a-P2c in `startWithHooks` to `branching.Load` (effect) → `bootconfig.Resolve` (decision) → write back. **Migrate** `daemon_branchprotection_sul12_test.go` cases 1/2 and the workflow-mode-error assertions in `scenario_*_test.go` into `bootconfig_test.go` table tests; retain ONE daemon-side integration test that asserts `daemon.Start` still surfaces the resolved error end-to-end (proves the wiring, not the logic).
- **Testability rationale:** branch-protection + workflow-mode validation stop requiring a full `daemon.Start` boot; they become sub-millisecond pure table tests. This is the concrete "carve a testable seam" win.
- **Shrink:** ~35 lines + the 5-line helper leave; ~3-4 branches leave. Giant: 1617 → ~1580.

### B2 — `resolveBootConfig` daemon phase wrapper + pre-flight grouping
Extract P1 (pidfile) → `acquirePidfile(cfg) (*lifecycle.Pidfile, error)`; P3 (pre-flight maintenance) → `runBootPreflights(ctx, cfg) time.Duration` (returns backoff delay; wraps the four existing helper calls + their guards); P2e (project config load) folds into a `loadAndResolveConfig` wrapper around B1's `Resolve`. Each helper self-guards on `ProjectDir`/skip-flags so the outer function loses those branches.
- **Testability rationale:** `runBootPreflights` becomes independently table-testable for the skip-flag matrix (WAL/br-history/backoff/merge-driver on/off) without booting subscribers.
- **Shrink:** ~120 lines + ~10 branches leave the outer function.

### B3 — `wireBusAndSubscribers` phase helper
Extract P4+P5 (registry/bus construction + all pre-Seal `Subscribe` calls) into one helper returning a struct of the constructed singletons (bus, qs, registries, controllers, watchers) that later phases consume. Keep Seal in the outer function (P6) as the explicit ordering landmark, OR return an "unsealed bus + subscriber set" and seal in the helper's caller — reviewer's call; the invariant is **all Subscribe before Seal (EV-009)**.
- **Testability rationale:** subscriber-wiring completeness (the 31-point `logCompositionRoot` audit, `daemon.go:2280`) becomes assertable against the returned struct.
- **Shrink:** ~230 lines leave. Giant → ~1200.

### B4 — `runStartupReconcile` phase helper
Extract P7 (orphan sweep + reconcile + CatBL sweeps) as one `ProjectDir`-guarded helper taking the bus + adapters, returning the sweep result. Highest-line block; almost pure sequential effect with one internal guard.
- **Testability rationale:** none new (already scenario-tested), but removes ~310 lines and ~15 branches from the outer function — the single biggest cyclop/gocognit contributor.
- **Shrink:** ~310 lines leave. Giant → ~890.

### B5 — `wireSocketListener` phase helper
Extract P9-P11 (adapter registry, hook store, startup state loads, socket-listener block) into a helper that constructs the socket-facing singletons and starts `RunSocketListenerWithDashboard`, returning the handles (opPauseCtrl, concurrencyCtrl, crewHandler, drainDet, queueHandlerAdapter…) the work-loop phase needs.
- **Shrink:** ~330 lines + ~12 branches leave. Giant → ~560.

### B6 — `launchWorkLoop` phase helper + final shell reduction
Extract P13 (work-loop deps build + injections + start) into `launchWorkLoop(ctx, cfg, boot) error`. The residual `startWithHooks` becomes a linear phase sequence: `acquirePidfile → loadAndResolveConfig → runBootPreflights → wireBusAndSubscribers → seal/startupEvents → runStartupReconcile → wireSocketListener → sleepBootBackoff → launchWorkLoop`.
- **Shrink:** ~330 lines leave. Giant → **~70-90 lines** of sequential phase calls + error propagation.

Order rationale: B1 first (self-contained, delivers the named subsystem + the test win, zero coupling to later phases). B2-B6 proceed outer-boundary-inward, each removing a contiguous phase so the diff is a clean cut-and-call with no interleaving. B3/B4/B5/B6 are the heavy line movers; each is reviewed for the **shared-state threading** hazard (§8) in isolation.

---

## 6. Test strategy

**Newly unit-testable (the win):**
- `bootconfig.Resolve` + component funcs: full table coverage of workflow-mode (empty / invalid / each valid), branching precedence (flag-set vs yaml-only vs both-empty, for target and protect independently), target resolution (`""→main`, explicit), and the two hk-sul12 protection cases + happy path — all as pure `bootconfig_test.go` cases (migrated from the daemon-boot tests). Error text asserted verbatim.
- `runBootPreflights` (B2): skip-flag matrix as a package-`daemon` unit test (no bus/subscribers needed).

**Retained daemon-side effect tests (must stay green, unchanged behavior):**
- ONE end-to-end `daemon.Start` branch-protection integration test (proves resolve-error propagates through boot).
- The scenario suites exercising orphan sweep / reconcile (`scenario_orphan_sweep_*`, `scenario_reap_*`) — B4 must leave them byte-identical.
- The 31-point composition-root wiring audit (`logCompositionRoot`, `HARMONIK_DEBUG_WIRING=1`) — B3/B5 must not drop or reorder any Subscribe/Start.
- Full-boot smoke (`daemon.Start` in unit-test mode: `ProjectDir=""`, `BrPath=""`) — proves the phase helpers' guards short-circuit correctly (the many `ProjectDir != ""` paths that must remain skippable).

**Verification per slice:** `go build ./...`, `go vet ./...`, `go test ./internal/bootconfig/`, `go test ./internal/daemon/ -run '<affected>'`. ⚑ Pre-existing SSH/tmux/sandbox E2E failures are unrelated (daemon-off; they fail identically on a pristine tree per COORD c050/c052) — the implementer must diff against the pristine baseline, not assume green.

---

## 7. Target ceilings + honest expected numbers

Ceilings (`.golangci.yml`): funlen 100 lines / 60 statements; cyclop ≤15; gocognit ≤20. **Goal: the shrunk `startWithHooks` passes a full `golangci-lint run` clean, with no `//nolint` for funlen/cyclop/gocognit, and every extracted helper also under ceiling.**

| Function (post-B6) | funlen | cyclop | gocognit | Confidence |
|---|---|---|---|---|
| `startWithHooks` (shell) | ~70-90 lines ✅ | **~10-14** ⚠️ | ~10-14 ✅ | cyclop is the tight one |
| `bootconfig.Resolve` + funcs | <40 each ✅ | <10 ✅ | <10 ✅ | high |
| `runBootPreflights` (B2) | ~50 ✅ | ~8 ✅ | ~8 ✅ | high |
| `wireBusAndSubscribers` (B3) | ~180 ⚠️ | ~5 ✅ | ~5 ✅ | see note |
| `runStartupReconcile` (B4) | ~280 ⚠️ | ~18 ⚠️ | ~22 ⚠️ | see note |
| `wireSocketListener` (B5) | ~250 ⚠️ | ~12 | ~14 | see note |
| `launchWorkLoop` (B6) | ~300 ⚠️ | ~15 | ~18 | see note |

**Honest caveats a reviewer must weigh:**
- **The outer `startWithHooks` reaches all three ceilings** — it becomes a near-branch-free linear call sequence; the only branches are `if err != nil { return }` per fallible phase (~9 phases → cyclop ~10). ✅ This is the primary target and it is met.
- **The phase helpers do NOT all fit under funlen** as single functions. `wireBusAndSubscribers`, `runStartupReconcile`, `wireSocketListener`, `launchWorkLoop` are each still 180-300 lines of sequential wiring. A single extraction moves the length *out of the giant* but each helper is itself a new grandfathered-size function. ⚑ **This is the load-bearing honesty point:** you cannot get every boot phase under funlen 100 by grouping alone — some phases (P5 subscribers, P7 reconcile, P11 socket, P13 workloop) are irreducibly long linear effect sequences. Options, for the reviewer/operator to rule on:
  1. Accept the outer shell under-ceiling + the phase helpers as new grandfathered functions (they are wiring, not logic; funlen on straight-line construction is low-signal). Preferred — matches how `newWorkLoopDeps` (already ~large) is treated today.
  2. Sub-split each long phase further (e.g. `wireSubscribers` → `wireSpendMeters` + `wireWatchers` + `wireQuiesce`), pushing every helper under 100. Achievable but multiplies slice count and threading surface; diminishing returns.
- **cyclop 15 on the outer shell is achievable but sensitive to phase count.** Each fallible phase adds ~1. Keep the outer sequence to ≤14 fallible calls (group infallible/guarded phases inside helpers) or cyclop tips to 15-16. Flagged as the metric to watch during B6.
- **Do NOT claim the giant is "gone."** Post-B6 the *code* still exists (moved into 6 helpers); what's retired is the single 1617-line function and its concentration of branches. The subsystem-level win is `bootconfig` (import-isolated, unit-tested); the daemon-level win is the readable phase sequence.

---

## 8. Risks + load-bearing invariants an extraction must NOT break

1. **Boot ordering is load-bearing and encoded in comments** — the source is dense with ordering constraints that MUST survive helper extraction:
   - Branching merge (P2b) MUST run before `resolveTargetBranch` + hk-sul12 guard (comment `daemon.go:832-834`).
   - All `Subscribe(bus)` MUST precede `bus.Seal()` (EV-009; P5 before P6).
   - Pidfile acquire (P1) is first; `defer Release` must stay in `startWithHooks` scope (a `defer` inside an extracted helper releases the lock when the helper returns — **regression trap**; `acquirePidfile` must return the `*Pidfile` and the outer function owns the `defer`).
   - `sleepBootBackoff` (P12) MUST stay AFTER socket bind (hk-uzvt9; sleeping before bind false-reverts the supervisor). Extraction must not reorder it before P11.
   - Orphan sweep (P7) MUST run before `LoadQueueAtStartup` (P10, QM-002a) — comment `daemon.go:1544-1547`. B4/B5 ordering must preserve this.
   - `daemon_started` (F-class, fsynced) MUST precede the supervisor-revival scan (P6, comment `daemon.go:1321-1324`).
2. **Pidfile locking (P1):** advisory lock via `lifecycle.AcquirePidfile`; the `defer pidfile.Release()` (`daemon.go:807`) must remain in the function whose lifetime spans the whole daemon run — i.e. `startWithHooks`, not a phase helper. **Highest-severity extraction trap.**
3. **Config-validation semantics (the `bootconfig` seam):** the resolve/validate order and the exact error strings are load-bearing — operator-facing diagnostics and existing test assertions (`daemon_branchprotection_sul12_test.go`, `scenario_*`) match specific substrings (`--forbid-default-main`, `is in ProtectBranches`, `WorkflowModeDefault must be set`). `bootconfig.Resolve` must reproduce them verbatim, and the flag>yaml>builtin precedence must be byte-identical (fill only zero-value fields; never overwrite a flag-supplied value).
4. **Shared-state threading (B3-B6):** ~20 singletons constructed in early phases are consumed by later phases (bus, qs, sharedRunRegistry, handlerPauseCtrl, concurrencyCtrl, opPauseCtrl, queueHandlerAdapter, crewHandler, drainDet, deps…). Extraction must thread these explicitly (return-struct or param) — a missed hand-off is a nil-deref at boot. Recommend a package-private `bootState` struct accumulated across phases (the reviewer should scrutinize its field lifecycle: which phase writes each field, which reads it).
5. **The two-phase wiring patterns** (`tunerBackstop.SetTuner` post-Seal after pre-Seal Subscribe; `handlerPauseCtrl.SetPersistFn` after pre-Seal construct; `staleWatcher.Set{ForceReap,RunProcessDead}` in P13 after `deps` exists) — these deliberately straddle phase boundaries. Helpers must not "tidy" them into one place; the split is load-bearing for construction ordering.
6. **Unit-test mode (`ProjectDir==""` / `BrPath==""`):** 27 guard branches let `daemon.Start` run without a project dir (used by many tests). Every phase helper must preserve its guard so the no-project path still short-circuits cleanly. The full-boot smoke test (§6) is the guard.
7. **⚑ Ratchet vs full-run ambiguity:** the design targets a clean full `golangci-lint run` of the shell; it does NOT rely on `--new-from-rev` grandfathering to hide a still-oversized function. If B2-B6 cannot land the outer shell under cyclop 15 without a `//nolint`, that is a design failure to surface, not paper over — see §7 option (1)/(2).

---

## 9. Open questions a reviewer must resolve

1. **New subsystem row.** `bootconfig` is not in the `subsystem-organization.md` S01-S09 matrix. Add it as a leaf (`mayDependOn={core}`) — operator/spec sign-off, since the skill says "if not listed, stop and consult." ⚑
2. **funlen on the long phase helpers.** Accept them as grandfathered wiring functions (§7 option 1, preferred) or mandate further sub-splitting to <100 (option 2)? This sets the true scope of B3-B6.
3. **Scope of the "giant retirement" milestone.** Is B1 (the subsystem) alone the deliverable the operator wants now, with B2-B6 as a follow-on, or is the full outer-shell shrink in-scope for one milestone? B1 delivers the named `boot-config` subsystem + the test win independently; B2-B6 are the length retirement.
4. **`resolveTargetBranch` ownership.** Moving it into `bootconfig` leaves callers elsewhere (`workloop.go`, reconciliation scheduler at `daemon.go:1649`, `:2302`) importing it. Confirm all in-daemon callers switch to `bootconfig.ResolveTargetBranch` (daemon may import bootconfig freely) — or keep a thin daemon-local delegate to avoid churn. ⚑ (verify caller set with `grep -rn resolveTargetBranch`.)
