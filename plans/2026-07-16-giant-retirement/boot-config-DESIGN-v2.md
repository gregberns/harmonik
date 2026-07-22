# Giant retirement #1 — `boot-config` out of `startWithHooks`: design v2 (FINAL)

> Revision agent, 2026-07-16, branch `phase1-session-restart-substrate`. This is the FINAL design folding **every** fix-list item from BOTH adversarial reviews (scope/altitude: APPROVE-WITH-CHANGES, F1-F6; seam-integrity: APPROVE-WITH-CHANGES, findings 1-5 + verification q4). Every file:line claim re-verified against live source before this rewrite. Supersedes `boot-config-DESIGN.md`.
>
> **Operator gate RESOLVED (operator, 2026-07-16), §0.3:** the pure config-resolution seam becomes a **daemon-internal sub-package** at `internal/daemon/bootconfig` (its own package `bootconfig`, scoped depguard row), **not** a top-level `internal/bootconfig` sibling. The sub-slice sequence is unchanged; the package path, the depguard row, and the (no) subsystem-matrix row are all fixed to the sub-package answer below.

---

## 0. Review resolution

### 0.1 Scope / altitude review (F1-F6)

| # | Finding | Resolution in v2 |
|---|---|---|
| **F1** | "Grandfathered new helper" claim is mechanically wrong; the merge gate is `--new-from-rev`, so freshly-extracted 180-300-line helpers are exactly the "new" the ratchet flags — the refactor would *add* funlen failures. | **Folded (load-bearing).** §8 now measures against the **real merge gate** (`golangci-lint run --new-from-rev=HEAD~1` / `origin/main`), NOT the release-only full run. §7 option-1 ("accept as grandfathered wiring") is **deleted** — it rests on a false premise. Every long phase helper is **sub-split under funlen-100 AND statements-60** (§6, the new load-bearing section). Goal restated: *no `//nolint:funlen/cyclop/gocognit` anywhere* — achievable now that helpers sub-split. Per-helper commitment stated in B3-B6 (§5), not left to "reviewer's call." |
| **F2** | New-subsystem justification is thin; the real question ("is this a package at all, vs a daemon-internal pure file?") is smuggled past as a bookkeeping "add the matrix row." Pure funcs in `package daemon` are already table-testable without a package boundary. | **Surfaced as the operator gate (§0.3), now RESOLVED (operator, 2026-07-16): daemon sub-package `internal/daemon/bootconfig`.** The package boundary buys the **depguard import-isolation row** (prevents future drift of config-resolution into `queue`/`branching`) without a top-level subsystem promotion. §3/§4 are fixed to the resolved sub-package answer. (The test win is real regardless.) |
| **F3** | Milestone-bar decision (old open-q3) is a naming-honesty gate, not a downstream open question. B1 alone shaves 35 lines and does NOT retire the giant; shipping it as "giant retirement #1" mislabels it. | **Folded.** §10: milestone = **B1-B6**. B1 alone is "extract the config-resolution seam," full stop — it is explicitly *not* "giant retirement." The giant is retired only when the outer shell reaches ~80 lines (post-B6). No longer an open question. |
| F4 | (positive) E/D/H seam map + value split are honest and correctly scoped. | No change. §1/§2 preserved (with the corrections F6/seam-2/seam-3 demand). |
| F5 | (positive) Sub-slice sequencing matches program method; §8 invariant catalogue is correct. | No change to method. §9 invariant catalogue **extended** (jsonlWriter defer, seam-finding-1). |
| F6 | Old open-q4 caller set overstated: real callers are `workloop.go:1180`, `export_test.go:509` (def at `workloop.go:1312`) — not `daemon.go:1649/:2302`. | **Folded.** Corrected everywhere (§0.2, §5-B1, §9). Re-verified by grep this pass. |

### 0.2 Seam-integrity review (findings 1-5 + verification q4)

| # | Finding | Resolution in v2 |
|---|---|---|
| **1** | [HIGH] `jsonlWriter.Close()` defer (`daemon.go:966`) has the SAME lifetime trap as pidfile, and it lives in P4 which **B3 extracts wholesale** — a defer inside the helper closes the event log *before the work loop runs*. §8 flagged only pidfile. | **Folded (load-bearing #1).** §9 risk catalogue now lists **BOTH** lifetime-spanning defers at equal HIGH severity (verified: exactly two — `:807` pidfile, `:966` jsonlWriter; the `:2044`/`:2263` defers are goroutine-local, safe). B3's contract (§5): `constructBusAndRegistries` **returns the `*eventbus.JSONLWriter`** (or an `io.Closer`); the outer `startWithHooks` owns `defer jsonlWriter.Close()`. B3 must NOT `defer Close` inside the helper. |
| **2** | [MED] Umbrella `Resolve()` reorders workflow-mode validation to run AFTER `branching.Load` I/O. Current order validates mode (`:820-825`) BEFORE Load (`:837-848`); the empty-mode misconfig (hk-81n9r) currently short-circuits before ANY I/O. | **Folded (load-bearing #2).** §2/§3 preserve the original ordering: the daemon calls **`bootconfig.ValidateWorkflowMode(cfg.WorkflowModeDefault)` BEFORE `branching.Load`**, then calls `Resolve` for the branching-merge/target/protection steps. `Resolve` still re-validates mode internally (idempotent, cheap) so the umbrella stays a correct composition — but the fail-closed mode check fires pre-I/O exactly as today. Call site rewritten in §3. |
| **3** | [MED] `ValidateBranchProtection`'s `flagTarget` param is mislabeled; case-(1) at `:865` reads `cfg.TargetBranch` **post-merge** (set at `:842-843`), not the flag value. Passing the pre-merge flag would falsely error the "flag empty, YAML supplies lands_on, ForbidUnprotectedDefault=true" case. | **Folded (load-bearing #3).** Signature renamed: **`mergedTarget`** (the post-merge, pre-resolution target — can still be `""`), and the contract is stated: case-(1) tests `forbidDefault && mergedTarget == ""`; case-(2) tests `ResolveTargetBranch(mergedTarget) ∈ protect` (`""→"main"`). The post-merge value is **threaded through** `Resolve` from `MergeBranchingDefaults` output into `ValidateBranchProtection` — see §3 signature + call flow. |
| **4** | [LOW] §9 open-q4 caller set (`daemon.go:1649/:2302`) is a factual error; overstates churn. | **Folded** (same as F6). Corrected. |
| **5** | [LOW] §6 frames P2b precedence-merge table cases as "migrated from daemon-boot tests," but no existing test hits the `branching.Load` P2b merge (sul12 tests leave `ProjectDir` unset). Coverage is **net-new**, not migrated. | **Folded.** §7 now labels precedence-merge coverage **net-new** (a genuine win); only the workflow-mode + hk-sul12 error assertions are *migrated* from the daemon-boot tests. |
| **q4** | B1's retained daemon-side integration test must be an **error case** (proves Resolve-error propagates through boot), not the happy-path `TestDaemonStart_EmitsDaemonConfig` (returns nil). B1 prose was loose. | **Folded.** §5-B1 pins it: retain ONE daemon-side integration test that asserts `daemon.Start` **surfaces a resolution error** end-to-end (an error case — e.g. the sul12 case-2 or empty-mode case). The happy-path emit test stays as-is but is NOT the propagation proof. |

### 0.3 RESOLVED — operator gate (operator, 2026-07-16)

**Question:** Is `boot-config` a new top-level `internal/` subsystem, or a daemon-internal sub-package?

**Ruling (operator, 2026-07-16): daemon-internal SUB-PACKAGE.** The seam is its own package `bootconfig` living at **`internal/daemon/bootconfig`** (a sub-package under `daemon`), with a **scoped depguard row** on `**/internal/daemon/bootconfig/**` — NOT a top-level `internal/bootconfig` sibling and NOT a `subsystem-organization.md` matrix row. This keeps the import-isolation ratchet (config-resolution stays pure over `$gostd + core`, can never pull in `queue`/`branching`) without promoting a ~120-line pure-string seam to a top-level subsystem.

The selected answer affects only three things; the rest of the design is identical:

| Aspect | Resolved: **daemon sub-package** (`internal/daemon/bootconfig`) |
|---|---|
| Package | new package `bootconfig` under `internal/daemon/`, imports `$gostd + internal/core` only |
| Depguard row | **add** the scoped `bootconfig:` row (§4), `files: ["**/internal/daemon/bootconfig/**"]` |
| Subsystem matrix | **none** — it is a daemon sub-package, not a top-level subsystem |
| Import-drift guarantee | HARD edge: config-resolution can never pull in `queue`/`branching` |
| Testability win | pure funcs, sub-millisecond table tests |

**Implementer caveat (import cycle):** `internal/daemon/bootconfig` must NOT import back into `daemon`. Watch for a cycle — if `bootconfig` needs a `daemon` type AND `daemon` needs `bootconfig`, thread a small shared type (a plain struct over `$gostd + core` primitives) rather than promoting the seam back to a top-level package. The `Input`/`Resolved` types (§3) are already primitive-only precisely to keep this edge one-way.

**Design stance:** everywhere v2 says `bootconfig.Resolve`, it is the `internal/daemon/bootconfig` package's `Resolve` — same code, same tests, same call site. The sub-slice sequence (§5) and the funlen sub-split (§6) are unaffected.

---

## 1. Section-by-section map of `startWithHooks` (`daemon.go:774-2389`, re-verified)

"E" = effect (stays in daemon shell), "D" = pure decision (candidate for the seam), "H" = already an extracted helper call.

| # | Block | Lines | Kind | What it does |
|---|---|---|---|---|
| P1 | **Pidfile acquire** | 784-808 | E | mkdir `.harmonik/`, getpid/getpgrp, UUIDv7 instance ID, `lifecycle.AcquirePidfile`, `defer Release` (`:807`). Guarded by `cfg.ProjectDir != ""`. |
| P2a | **Workflow-mode validation** | 820-825 | **D** | reject empty (fail-closed, hk-81n9r) + reject `!Valid()`. Pure over `core.WorkflowMode`. **Runs before ALL I/O today — order load-bearing (seam-2).** |
| P2b | **Branching-defaults precedence merge** | 837-848 | **D** (+E load) | `branching.Load` (E, I/O) then fill `cfg.TargetBranch`/`cfg.ProtectBranches` only if zero-value (flag > yaml > builtin). The *fill rule* is pure. |
| P2c | **Branch-protection fail-closed (hk-sul12)** | 850-872 | **D** | `resolveTargetBranch(cfg.TargetBranch)` at `:864`; case-1 `ForbidUnprotectedDefault && cfg.TargetBranch=="" ` at `:865` (**post-merge target**, seam-3); case-2 resolved ∈ ProtectBranches at `:868-871`. Pure over strings/slices. |
| P2d | **Conflict-cap validation (WM-024)** | 883-887 | E→D | delegates to `workspace.ValidateConflictResolutionAttemptCap` (already pure, already in `workspace`). **Leave in workspace** (§2). |
| P2e | **Project config load (EM-012b)** | 895-901 | E | `LoadProjectConfig` (I/O) → `cfg.ProjectCfg`. |
| P3 | **Pre-flight maintenance** | 903-948 | H | WAL checkpoint, `.br_history/` rotation, restart-backoff record (`applyBootBackoff`, returns delay slept later), beads merge-driver. All helper calls; each self-guards. |
| P4 | **Registry + bus construction** | 950-1035 | E | RedactionRegistry, **JSONL writer open + `defer Close` at `:966` (lifetime-spanning, seam-1)**, event-ID HWM read + generator seed, `NewBusImplWithWriterAndHWM` (`:1008`), QueueStore, HandlerPauseController, RunRegistry. |
| P5 | **Pre-Seal subscriber wiring** | 1037-1269 | E | ~230 lines: pausePolicy, spendMeter, perQueueSpendMeter, queueOpConsumer, notifyStream, subscribeHub, staleWatcher, reviewGateWatcher, tunerBackstop, quiesceArbiter, substrate diagnostic hooks, CatBL2Handler, busObserver. Each `X.Subscribe(bus)` before Seal (EV-009). |
| P6 | **Seal + startup events** | 1271-1346 | E | `bus.Seal()` (`:1271`), clock-regression `daemon_degraded`, `staleWatcher.StartWatcher`, `daemon_started` (`:1297`), supervisor-revival detect (`:1321`), `daemon_config` emit. |
| P7 | **Orphan sweep + reconcile** | 1348-1659 | E | ~310 lines: br adapter + `CheckBrVersion`, raw queue.json provenance read, tmux adapter extraction, `RunOrphanSweep` (`:1496`), `adoptDeadRunSessions`, `reconcileOrphanedRunsOnResume` (+ cached status reader), reconciliation events, CatBL1/CatBL3 sweeps. One `ProjectDir != ""` guard. |
| P8 | **Session keepalive** | 1661-1676 | E | optional `go sk.RunSessionKeepalive`. |
| P9 | **Adapter registry + hook store** | 1678-1724 | E | AdapterRegistry register claude/codex/pi, seal, `ForAgent`, `SetAdapter`, `newDaemonHookStore`. |
| P10 | **Startup state loads** | 1726-1782 | E | `loadStartupQueues` (H, `:1740`), handler-pause persist+load, decision-ack load. |
| P11 | **Socket-listener block** | 1784-2050 | E | ~270 lines: opPauseCtrl, concurrencyCtrl, queueHandlerAdapter, drainDet, crewHandler, crewIdleReaper, branchReapWatcher, QueueHandler adapter, ConcurrencyController, bandwidth tuner, comms handlers, crew handler, state/dashboard builders, poll-gate, `RunSocketListenerWithDashboard`. Under `ProjectDir != ""`. |
| P12 | **Boot-backoff sleep** | 2052-2059 | E | `sleepBootBackoff(ctx, bootBackoffDelay)` (`:2059`) — deliberately AFTER socket bind (`:2047`, hk-uzvt9). |
| P13 | **Work-loop deps + start** | 2061-2387 | E | ~330 lines: `newWorkLoopDeps` (H), sentinel governor config, emittedEpics/follow-up-ledger boot-seed, ~20 `deps.*` injections, `quiesceArbiter.Start`, crewIdleReaper/branchReapWatcher start, reconciliation scheduler, worker-report loop, staleWatcher force-reap seams, `runWorkLoop`, block on `loopDone`. Guarded by `cfg.BrPath != ""`. |
| — | `return nil` | 2389 | | |

**Structural read (verified):** ~1450 of 1617 lines (P4-P13) are effectful composition-root wiring. The ONLY pure decision logic is P2a/P2b/P2c (~35 lines + the 5-line `resolveTargetBranch`). Metrics: 1617 lines; 137 `if`, 47 `for`, 1 `switch`, 6 `case`, 55 `return`; 27 `ProjectDir/BrPath/JSONLLogPath != ""` guards.

**Lint gate (corrected per F1):** the merge gate is `golangci-lint run --new-from-rev=HEAD~1` (Makefile:274, check-fast) / `--new-from-rev=origin/main` (Makefile:295, check-short/CI). The **full** `golangci-lint run` is **release-only** and already red on ~5666 legacy issues (Makefile:395-400) — it is NOT a bar this refactor targets. `funlen` = 100 lines / 60 statements (`ignore-comments: true`); `cyclop` max-complexity 15; `gocognit` min-complexity 20. `startWithHooks` is currently grandfathered because it predates `origin/main`; **a freshly-extracted helper has a brand-new func-decl line and inherits NO grandfather status** — it is exactly what the ratchet flags. This is why every long phase helper must sub-split under ceiling (§6).

---

## 2. The seam — what MOVES vs what STAYS

### Moves into the seam (pure, testable) — `bootconfig.*` (package `internal/daemon/bootconfig`, §0.3 resolved)

- `ResolveTargetBranch(flag string) string` — moves `resolveTargetBranch` (`workloop.go:1312`, pure `"" → "main"`).
- `MergeBranchingDefaults(in Input) Resolved` — the flag>yaml>builtin fill rule (P2b): fill target/protect only when the flag value is zero. Daemon does `branching.Load` (I/O) and passes `LandsOn`/`ProtectBranches` in as plain `string`/`[]string`.
- `ValidateBranchProtection(forbidDefault bool, mergedTarget string, protect []string) error` — the two hk-sul12 cases (P2c), **using the post-merge target** (seam-3). Case-1: `forbidDefault && mergedTarget==""`. Case-2: `ResolveTargetBranch(mergedTarget) ∈ protect`. Error text verbatim.
- `ValidateWorkflowMode(mode core.WorkflowMode) error` — the empty-and-`!Valid()` fail-closed check (P2a). **Called by the daemon BEFORE `branching.Load` (seam-2)**, and re-composed inside `Resolve`.
- **Umbrella:** `Resolve(in Input) (Resolved, error)` — composes mode-validate → branching-merge → protection-validate, threading the merged target from the merge step into the protection check. Returns `Resolved{TargetBranch, ProtectBranches}`.

### Stays in the daemon shell (effect)

- **All file/OS I/O:** `branching.Load`, `LoadProjectConfig`, pidfile acquire, HWM read, JSONL open — effect; the seam receives already-loaded values.
- **`workspace.ValidateConflictResolutionAttemptCap` (P2d):** already pure, already in `workspace` (`conflictresolution_wm024.go:72`) with its own tests. **Do NOT re-home** — daemon keeps calling it directly.
- **`core.WorkflowMode.Valid()` (P2a):** stays in `core`; `ValidateWorkflowMode` *wraps* it, does not reimplement.
- **Everything P4-P13:** composition-root wiring — shrinks via daemon-side phase helpers (§5/§6), not via the seam.

### Ordering the seam MUST preserve (seam-2)

Current: **mode-validate (pre-I/O) → `branching.Load` (I/O) → merge → target-resolve → protection-validate.** The umbrella must not push mode-validation behind `branching.Load`. The daemon calls `ValidateWorkflowMode` *before* Load; `Resolve` handles everything from the merge onward (and re-validates mode idempotently). This keeps the common empty-mode misconfig (hk-81n9r) short-circuiting before any I/O, byte-identical to today.

---

## 3. Layout + public API

Layout per the resolved §0.3 answer — a daemon sub-package at `internal/daemon/bootconfig`:

```
internal/daemon/bootconfig/{doc.go, bootconfig.go, bootconfig_test.go}   # package bootconfig, $gostd + core
```

```go
// Resolved (§0.3): package bootconfig at internal/daemon/bootconfig
// (imports $gostd + internal/core ONLY; must NOT import daemon back — §0.3 caveat).

type Input struct {
    WorkflowMode             core.WorkflowMode
    FlagTargetBranch         string   // cfg.TargetBranch as supplied (flag/zero) — pre-merge
    YAMLLandsOn              string   // branchingDefaults.LandsOn ("" if none)
    FlagProtectBranches      []string // cfg.ProtectBranches as supplied
    YAMLProtectBranches      []string // branchingDefaults.ProtectBranches
    ForbidUnprotectedDefault bool
}

type Resolved struct {
    TargetBranch    string   // post-merge, post-default ("" → "main")
    ProtectBranches []string // post-merge
}

// Resolve applies flag>yaml>builtin branching precedence, resolves the target
// branch, and runs the hk-sul12 fail-closed protection check — threading the
// POST-MERGE target into the protection check. It ALSO re-validates the
// workflow mode as an idempotent first step; the daemon has already called
// ValidateWorkflowMode before branching.Load (see call site), so this is a
// cheap re-check that keeps Resolve a correct standalone composition.
// Error text matches the current startWithHooks messages verbatim.
func Resolve(in Input) (Resolved, error)

func ValidateWorkflowMode(mode core.WorkflowMode) error
func MergeBranchingDefaults(in Input) Resolved
func ResolveTargetBranch(flag string) string  // "" → "main"
// NOTE: mergedTarget is the POST-MERGE target (may still be ""); NOT the flag value (seam-3).
func ValidateBranchProtection(forbidDefault bool, mergedTarget string, protect []string) error
```

**Daemon call site** (replacing P2a-P2c; preserves mode-before-Load ordering, seam-2):

```go
// P2a — workflow-mode fail-closed, BEFORE any I/O (unchanged ordering, seam-2):
if err := bootconfig.ValidateWorkflowMode(cfg.WorkflowModeDefault); err != nil {
    return fmt.Errorf("daemon.Start: %w", err)
}

// P2b I/O stays daemon-side:
var in bootconfig.Input
in.WorkflowMode = cfg.WorkflowModeDefault
in.FlagTargetBranch, in.FlagProtectBranches = cfg.TargetBranch, cfg.ProtectBranches
in.ForbidUnprotectedDefault = cfg.ForbidUnprotectedDefault
if cfg.ProjectDir != "" {
    bd, err := branching.Load(cfg.ProjectDir)   // effect (I/O) stays
    if err != nil {
        return fmt.Errorf("daemon.Start: load .harmonik/branching.yaml: %w", err)
    }
    in.YAMLLandsOn, in.YAMLProtectBranches = bd.LandsOn, bd.ProtectBranches
}

// P2b-merge + P2c-protection (pure), post-merge target threaded internally:
resolved, err := bootconfig.Resolve(in)
if err != nil {
    return fmt.Errorf("daemon.Start: %w", err)
}
cfg.TargetBranch, cfg.ProtectBranches = resolved.TargetBranch, resolved.ProtectBranches
```

`Resolve` internally: `Resolved r = MergeBranchingDefaults(in)`; then `ValidateBranchProtection(in.ForbidUnprotectedDefault, r.TargetBranch /* post-merge */, r.ProtectBranches)`; then set `r.TargetBranch = ResolveTargetBranch(r.TargetBranch)` for the return. (The protection check runs on the *merged-but-unresolved* target, matching `:865`'s `cfg.TargetBranch==""` semantics; the `""→"main"` resolution for the returned value and the case-2 comparison uses `ResolveTargetBranch`.)

**JSONL writer ownership (seam-1):** unrelated to the seam, but noted here because B3 touches it — see §5-B3 / §9.

---

## 4. Depguard edge (resolved sub-package variant — §0.3)

Per the resolved §0.3 answer, add the **scoped sub-package** row to `.golangci.yml`, sorted near the `daemon:` rules (mirroring `orchestrator:` at `:601-605` and `policy:` at `:450`):

```yaml
        bootconfig:
          files: ["**/internal/daemon/bootconfig/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "bootconfig is a pure daemon sub-package; daemon depends on it, never the reverse (§0.3 import-cycle caveat)" }
```

The top-level-subsystem variant (`internal/bootconfig` sibling, no `daemon` deny) is **dropped** — the seam lives at `internal/daemon/bootconfig`.

**Per-function edge check:** all funcs read only strings, `[]string`, `core.WorkflowMode`/`.Valid()`, `fmt` → `$gostd + core`. ✅ No `daemon`/`branching`/`queue`/`workspace`. Proof obligation (run after scaffold, before rewire):

```
go list -deps ./internal/daemon/bootconfig/ | grep gregberns/harmonik   # expect ONLY .../internal/core
go tool golangci-lint run ./internal/daemon/bootconfig/...              # expect zero depguard violations
```

---

## 5. Ordered, behavior-preserving sub-slices (B1-B6 = the milestone; §10)

Each slice is independently committable, green-verifiable (`go build ./...`, `go vet ./...`, targeted `go test`), independently reviewable. **STOP + review between each.**

### B1 — the config-resolution seam (LOWEST RISK, HIGHEST VALUE) — *not itself "giant retirement" (§10)*
Scaffold the `internal/daemon/bootconfig` sub-package (doc.go + bootconfig.go + test) + the scoped depguard row (§4). Implement the 4 pure funcs + `Resolve`. Rewrite P2a-P2c per §3: **`ValidateWorkflowMode` before `branching.Load`** (seam-2), then `Resolve`. Tests:
- **Migrated** (from daemon-boot): the workflow-mode error assertions and the two hk-sul12 protection cases (`daemon_branchprotection_sul12_test.go`) → pure table tests.
- **Net-new** (seam-5, NOT migrated): the P2b flag>yaml>builtin precedence-merge cases — no existing test exercises the `branching.Load` merge (sul12 tests leave `ProjectDir` unset). This is genuine added coverage.
- **Retained integration test — pin to an ERROR case (seam-q4):** ONE daemon-side test asserting `daemon.Start` **surfaces a resolution error** end-to-end (e.g. sul12 case-2 or empty-mode). NOT the happy-path `TestDaemonStart_EmitsDaemonConfig` (returns nil) — that proves nothing about propagation.
- **Shrink:** ~35 lines + the 5-line helper leave; ~3-4 branches leave. 1617 → ~1580.

### B2 — pre-flight grouping + `loadAndResolveConfig`
Extract P1 → `acquirePidfile(cfg) (*lifecycle.Pidfile, error)` — **returns the pidfile; outer shell owns `defer pidfile.Release()`** (§9). P3 → `runBootPreflights(ctx, cfg) time.Duration` (returns the backoff delay; wraps the 4 existing helper calls + guards). P2e folds into `loadAndResolveConfig`. Each helper self-guards on `ProjectDir`/skip-flags.
- **Testability:** `runBootPreflights` becomes independently table-testable for the skip-flag matrix without booting subscribers.
- **Shrink:** ~120 lines + ~10 branches leave. All B2 helpers already ≤ ceiling (no sub-split needed — §6).

### B3 — bus + subscribers, **sub-split** (P4+P5)
Split into three sub-helpers, each under funlen-100/statements-60 (§6):
- `constructBusAndRegistries(...) (busState, *eventbus.JSONLWriter, error)` — P4. **RETURNS the `*eventbus.JSONLWriter` (seam-1); the outer shell owns `defer jsonlWriter.Close()`. Must NOT `defer Close` inside the helper** — a helper-scope defer closes the event log before the work loop runs (boot-breaking).
- `wireSpendAndQueueConsumers(bs)` — pausePolicy, spendMeter, perQueueSpendMeter, queueOpConsumer, notifyStream, subscribeHub (all `Subscribe` pre-Seal).
- `wireWatchersAndObservers(bs)` — staleWatcher, reviewGateWatcher, tunerBackstop, quiesceArbiter, substrate diagnostic hooks, CatBL2Handler, busObserver (all `Subscribe` pre-Seal).
- **Seal stays in the outer shell (P6)** as the explicit ordering landmark; invariant: all `Subscribe` before `bus.Seal()` (EV-009).
- **Testability:** subscriber-completeness (the 31-point `logCompositionRoot` audit) assertable against the returned `busState`.
- **funlen commitment:** all three leaves under ceiling — NO `//nolint` (§6).

### B4 — startup reconcile, **sub-split** (P7)
Parent `runStartupReconcile(bs)` holds the single `ProjectDir != ""` guard and calls three sub-helpers under ceiling (§6):
- `buildReconcileAdapters(...)` — br adapter + `CheckBrVersion`, raw queue.json provenance read, tmux adapter extraction.
- `runOrphanSweepAndAdopt(...)` — `RunOrphanSweep`, `adoptDeadRunSessions`, `reconcileOrphanedRunsOnResume` (+ cached status reader), reconciliation_started/completed events.
- `runCatBLSweeps(...)` — CatBL1/CatBL3 sweeps.
- Invariant: orphan sweep before `loadStartupQueues` (QM-002a) — preserved because B4 runs before B5's `loadStartupState`.
- **funlen commitment:** all three leaves + the thin parent under ceiling — NO `//nolint`.

### B5 — socket listener, **sub-split** (P9-P11)
Parent `wireSocketListener(bs)` calls sub-helpers under ceiling (§6):
- `registerAdaptersAndHookStore(bs)` — P9.
- `loadStartupState(bs)` — P10 (`loadStartupQueues`, handler-pause persist+load, decision-ack load).
- `buildSocketControllers(bs)` — opPauseCtrl, concurrencyCtrl, queueHandlerAdapter, drainDet, bandwidth tuner, ConcurrencyController.
- `buildCrewAndCommsHandlers(bs)` — crewHandler, crewIdleReaper, branchReapWatcher, comms handlers.
- `startSocketListener(bs)` — state/dashboard builders, poll-gate, `RunSocketListenerWithDashboard`.
- Under `ProjectDir != ""`. Socket goroutines (`:2045-2049`) are self-contained/drained within this block.
- **funlen commitment:** all leaves under ceiling — NO `//nolint`.

### B6 — work loop, **sub-split** (P13) + final shell reduction
Parent `launchWorkLoop(ctx, cfg, bs) error` calls sub-helpers under ceiling (§6):
- `buildWorkLoopDeps(bs)` — `newWorkLoopDeps` (H) + sentinel governor config + emittedEpics/follow-up-ledger boot-seed.
- `injectWorkLoopDeps(bs, deps)` — the ~20 `deps.*` injections + staleWatcher force-reap seams.
- `startBackgroundLoops(bs)` — `quiesceArbiter.Start`, crewIdleReaper/branchReapWatcher start, reconciliation scheduler, worker-report loop.
- Parent runs `runWorkLoop` + blocks on `loopDone`. Guarded by `cfg.BrPath != ""`.
- **Residual `startWithHooks`** becomes a linear phase sequence: `acquirePidfile → loadAndResolveConfig → runBootPreflights → constructBusAndRegistries → wireSpend… → wireWatchers… → seal/startupEvents → runStartupReconcile → wireSocketListener → sleepBootBackoff → launchWorkLoop`, owning the two lifetime defers (pidfile, jsonlWriter).
- **Shrink:** giant → **~70-90 lines** of sequential phase calls + error propagation.

**Order rationale:** B1 first (self-contained, delivers the seam + test win). B2-B6 proceed outer-boundary-inward, each removing a contiguous phase. Shared state threads via a package-private `bootState` struct (§9 risk 4) — each phase writes/reads named fields, scrutinized in isolation.

---

## 6. funlen sub-split — the load-bearing shape (F1)

**Why:** the merge gate (`--new-from-rev`) flags every new func-decl line. A single 180-300-line phase helper is a **new funlen-100 violation**. §7 old-option-1 ("grandfather them") is deleted — grandfathering only covers code predating `origin/main`. So each long phase helper is decomposed until **every** extracted function satisfies BOTH `funlen ≤ 100 lines` AND `≤ 60 statements`. **No `//nolint:funlen/cyclop/gocognit` anywhere** — shell or helpers.

**Binding constraint is statements (60), not lines (100).** Wiring is statement-dense: each `X := New…()` / `X.Subscribe(bus)` / `deps.X = y` is one statement. `ignore-comments: true` means comment lines don't count toward the 100-line ceiling, but every construction counts toward statements-60. So each leaf helper targets **~40-50 statements** (comfortably under 60), which for dense wiring is roughly 45-60 code lines. That sizing drives the granularity below.

| Phase helper (old, over-ceiling) | old size | Sub-split leaves (each ≤100 lines / ≤60 stmts) | Notes |
|---|---|---|---|
| **B3** `wireBusAndSubscribers` | ~315 (P4 ~85 + P5 ~230) | `constructBusAndRegistries` (~55 stmts) · `wireSpendAndQueueConsumers` (~45) · `wireWatchersAndObservers` (~50) | `constructBusAndRegistries` returns the JSONLWriter (seam-1). Seal in shell. |
| **B4** `runStartupReconcile` | ~280 | thin guarded parent (~10 stmts) → `buildReconcileAdapters` (~50) · `runOrphanSweepAndAdopt` (~55) · `runCatBLSweeps` (~40) | Parent owns the `ProjectDir` guard + ordering. |
| **B5** `wireSocketListener` | ~250 | thin parent → `registerAdaptersAndHookStore` (~35) · `loadStartupState` (~40) · `buildSocketControllers` (~50) · `buildCrewAndCommsHandlers` (~40) · `startSocketListener` (~35) | P11 (~270) needed the deepest split (3 leaves). |
| **B6** `launchWorkLoop` | ~300 | thin parent (runWorkLoop + block) → `buildWorkLoopDeps` (~45) · `injectWorkLoopDeps` (~55) · `startBackgroundLoops` (~45) | Parent guarded by `BrPath != ""`. |

Each leaf is also trivially under **cyclop-15** and **gocognit-20** (straight-line construction, near-zero branching). If, during implementation, a leaf's statement count creeps over ~55, split it once more along the natural sub-group boundary (e.g. `buildSocketControllers` → `buildPauseAndConcurrency` + `buildTuner`) — the sizing above leaves headroom precisely so this is a local adjustment, not a redesign.

**Cost acknowledged:** sub-splitting multiplies the shared-state threading surface (more helpers reading/writing `bootState`). That surface is exactly the §9 risk-4 hazard and is reviewed per-slice. This is the accepted price of "no `//nolint`" under the real gate.

---

## 7. Test strategy

**Newly unit-testable (the win, identical under both §0.3 options):**
- `Resolve` + component funcs: full table coverage of workflow-mode (empty / invalid / each valid), branching precedence (flag-set vs yaml-only vs both-empty, target and protect independently — **net-new coverage, seam-5**), target resolution (`""→main`, explicit), the two hk-sul12 cases + happy path — all pure table tests. Error text asserted verbatim. **Migrated:** mode + sul12 error assertions. **Net-new:** the precedence-merge cases.
- `runBootPreflights` (B2): skip-flag matrix as a package-`daemon` unit test.

**Retained daemon-side effect tests (must stay green, unchanged behavior):**
- ONE `daemon.Start` **error-case** integration test proving resolve-error propagates through boot (seam-q4).
- Orphan-sweep / reconcile scenarios (`scenario_orphan_sweep_*`, `scenario_reap_*`) — B4 leaves byte-identical.
- The 31-point composition-root wiring audit (`logCompositionRoot`, `HARMONIK_DEBUG_WIRING=1`) — B3/B5 must not drop or reorder any Subscribe/Start.
- Full-boot smoke (`daemon.Start`, `ProjectDir=""`, `BrPath=""`) — proves every phase helper's guard short-circuits.

**Per slice:** `go build ./...`, `go vet ./...`, seam tests, `go test ./internal/daemon/ -run '<affected>'`, and **`golangci-lint run --new-from-rev=origin/main`** (the real gate — assert zero new funlen/cyclop/gocognit findings). Pre-existing SSH/tmux/sandbox E2E failures are unrelated (daemon-off) — diff against the pristine baseline.

---

## 8. Ceilings + honest expected numbers (measured against the REAL gate — F1)

**Bar:** `golangci-lint run --new-from-rev=origin/main` reports **zero new** funlen/cyclop/gocognit findings on the diff. NOT the release-only full run (already red on ~5666 legacy issues). Goal: **no `//nolint:funlen/cyclop/gocognit` anywhere** — shell and every extracted/sub-split helper under ceiling.

| Function (post-B6) | funlen (≤100 ln / ≤60 stmt) | cyclop (≤15) | gocognit (≤20) | Confidence |
|---|---|---|---|---|
| `startWithHooks` (shell) | ~70-90 ln ✅ | ~10-14 ✅ | ~10-14 ✅ | cyclop is the tight one; keep ≤14 fallible phases |
| `Resolve` + 4 funcs | <40 ln each ✅ | <10 ✅ | <10 ✅ | high |
| `runBootPreflights` (B2) | ~50 ln ✅ | ~8 ✅ | ~8 ✅ | high |
| **B3 leaves** (3) | each ≤100/≤60 ✅ | <5 ✅ | <5 ✅ | sub-split per §6 |
| **B4 leaves** (3 + parent) | each ≤100/≤60 ✅ | parent ~8, leaves <5 ✅ | <8 ✅ | sub-split per §6 |
| **B5 leaves** (5) | each ≤100/≤60 ✅ | <6 ✅ | <6 ✅ | sub-split per §6 |
| **B6 leaves** (3 + parent) | each ≤100/≤60 ✅ | <6 ✅ | <8 ✅ | sub-split per §6 |

**Honest caveats:**
- The outer `startWithHooks` reaches all three ceilings — near-branch-free linear call sequence; branches are `if err != nil { return }` per fallible phase (~9 → cyclop ~10). **cyclop-15 is the sensitive one:** each fallible phase adds ~1; keep the outer sequence ≤14 fallible calls (group infallible/guarded work inside helpers) or it tips to 15-16. The metric to watch during B6.
- **Every phase helper sub-splits under ceiling (§6).** There is no "accept as grandfathered wiring" fallback — that premise was false (F1). If any leaf cannot land under ceiling without `//nolint`, that is a design failure to surface (split further), not paper over.
- **Do NOT claim the giant is "gone."** Post-B6 the *code* still exists (moved into the sub-split helpers); what's retired is the single 1617-line function and its branch concentration. The seam-level win is import-isolated + unit-tested config-resolution; the daemon-level win is the readable phase sequence.

---

## 9. Risks + load-bearing invariants an extraction must NOT break

1. **Two lifetime-spanning defers — BOTH must stay in the outer shell (seam-1 raises jsonlWriter to equal severity):**
   - `defer pidfile.Release()` (`:807`) — `acquirePidfile` (B2) returns the `*Pidfile`; shell owns the defer.
   - **`defer jsonlWriter.Close()` (`:966`)** — inside P4, which **B3 extracts**. `constructBusAndRegistries` must **return the `*eventbus.JSONLWriter`** so the shell owns the defer. A helper-scope defer closes the event log immediately after wiring, before the work loop runs — **boot-breaking**. (Verified: exactly two lifetime-spanning defers; `:2044`/`:2263` are goroutine-local, safe.) **This defer must NOT change when/whether it runs relative to today's flow: it fires at `startWithHooks` return, same as now.**
2. **Boot ordering encoded in comments — must survive extraction:**
   - **Mode-validate BEFORE `branching.Load` (seam-2)** — daemon calls `ValidateWorkflowMode` pre-I/O (§3); empty-mode misconfig (hk-81n9r) short-circuits before any I/O, byte-identical to today.
   - Branching merge (P2b) before `resolveTargetBranch` + hk-sul12 guard (`:833-834`).
   - All `Subscribe(bus)` before `bus.Seal()` (EV-009; P5 before P6; Seal kept in shell).
   - `sleepBootBackoff` (P12, `:2059`) AFTER socket bind (`:2047`, hk-uzvt9) — B5/B6 must not reorder before P11.
   - Orphan sweep (P7, `:1496`) before `loadStartupQueues` (P10, `:1740`, QM-002a) — B4 before B5.
   - `daemon_started` (P6, `:1297`) before supervisor-revival scan (`:1321`).
3. **Config-validation semantics (the seam):** resolve/validate order + exact error strings are load-bearing (operator diagnostics + assertions match `--forbid-default-main`, `is in ProtectBranches`, `WorkflowModeDefault must be set`). `Resolve` reproduces them verbatim; precedence is fill-only-zero (never overwrite a flag value). **Post-merge target threading (seam-3):** `ValidateBranchProtection` takes `mergedTarget` (post-merge, may be `""`), NOT the flag value — case-1 tests `mergedTarget==""`, case-2 tests `ResolveTargetBranch(mergedTarget) ∈ protect`. Mislabeling this as `flagTarget` would falsely error the "flag empty, YAML supplies lands_on, forbid=true" case.
4. **Shared-state threading (B3-B6, amplified by the §6 sub-split):** ~20 singletons (bus, qs, sharedRunRegistry, handlerPauseCtrl, concurrencyCtrl, opPauseCtrl, queueHandlerAdapter, crewHandler, drainDet, deps…) constructed in early phases, consumed later. Thread via a package-private **`bootState`** struct — the reviewer scrutinizes each field's lifecycle (which sub-helper writes it, which reads it). A missed hand-off is a nil-deref at boot. The sub-split multiplies this surface; it is the accepted cost of no-`//nolint` (§6).
5. **Two-phase wiring patterns** (`tunerBackstop.SetTuner` post-Seal after pre-Seal Subscribe; `handlerPauseCtrl.SetPersistFn`; `staleWatcher.Set{ForceReap,RunProcessDead}` in P13 after `deps`) — deliberately straddle phase boundaries. Sub-helpers must NOT "tidy" them into one place; the split is load-bearing for construction ordering.
6. **Unit-test mode (`ProjectDir==""` / `BrPath==""`):** 27 guards let `daemon.Start` run without a project dir. Every phase helper (and sub-helper) preserves its guard so the no-project path short-circuits. The full-boot smoke test (§7) is the guard.
7. **Lint bar is `--new-from-rev`, not the full run (F1):** if any sub-split leaf cannot land under funlen/cyclop/gocognit without `//nolint`, split further (§6) — do not add the nolint.

---

## 10. Milestone scope (F3 — resolved, not deferred)

**Milestone `giant-retirement #1` = B1-B6.** The giant is retired only when the outer `startWithHooks` reaches ~70-90 lines (post-B6). **B1 alone is NOT "giant retirement"** — it is "extract the config-resolution seam" (shaves ~35 lines, delivers the testable seam + the unit-test win). If the operator wants to ship B1 independently, it ships under that honest name, with B2-B6 (the length retirement) explicitly deferred — but B1 must not carry the "giant retirement" banner alone.

---

## 11. Residual open questions (only these — everything else is resolved above)

1. **`resolveTargetBranch` caller rewire (F6/seam-4 — corrected caller set).** After B1, in-daemon callers are `workloop.go:1180` (in `newWorkLoopDeps`) and `export_test.go:509`; def at `workloop.go:1312`. (The old doc's `daemon.go:1649/:2302` were wrong.) Under the resolved sub-package answer (§0.3), `daemon` imports `internal/daemon/bootconfig` freely and both switch to `bootconfig.ResolveTargetBranch`. Trivial — re-grep to confirm before rewiring. (The §0.3 subsystem-vs-sub-package gate is RESOLVED — daemon sub-package — and is no longer open.)
