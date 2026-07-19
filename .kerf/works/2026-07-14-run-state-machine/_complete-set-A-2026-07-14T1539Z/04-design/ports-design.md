# 04-Design / ports — RunPorts/RunEnv/SharedHandles + daemon ClockPort

> Component design for C3 + C1, within pins M3-D4/M3-D6. Facts cite
> `03-research/workloop-ports/findings.md` (PF). Idiom: `internal/keeper/ports.go`
> (structural narrow interfaces; `EmitterPort = Emitter` alias :107).

## 1. The three bundles (internal/daemon/runports.go)

The RUN cut is the 36-field census (PF §1) minus deletions, organized:

```go
// RunPorts — behavioral dependencies of the run shell. Narrow, structural.
type RunPorts struct {
    Ledger   LedgerPort    // brAdapter ops + closeBeadWithHistoryTrim; carries brTimeoutCfg/intentLogDir/tidGen internally
    Emitter  EmitterPort   // = handlercontract.EventEmitter subset (type alias — keeper :107 trick)
    Worktree WorktreePort  // Create(ctx, spec) (path, cleanup, error); BaseSync for remote
    Merge    MergePort     // Prepare/Commit/Submit + escape-check submit (mergeq handle)
    Launch   LaunchPort    // BuildSpec, Spawn (substrate), Registries, hookStore ops, Deliver (paste-inject today, M2 driver later), timeouts, sandbox
    Gate     GatePort      // cpRegistry eval (dot_gate only; nil ⇒ eval-failure Outcome, PF §5)
    Clock    substrate.ClockPort
}

// RunEnv — immutable per-run values (no behavior).
type RunEnv struct {
    ProjectDir, TargetBranch, BrPath   string
    ProtectBranches, AllowedRepos      []string
    WorkflowModeDefault                core.WorkflowMode
    DefaultHarness                     core.AgentType
    ProjectCfg                         ProjectConfig
    RunID …; BeadRecord …; QueueName/ID/GroupIndex/ItemIndex …; Item* overrides …
}

// SharedHandles — cross-goroutine state, shared-by-reference (PF §3).
type SharedHandles struct {
    RunRegistry  *RunRegistry
    LocalInFlight *atomic.Int32
    AgentSpawnSem chan struct{}
    Workers      *workers.Registry
    Budget       BudgetPort       // one-method wrap of queueStore review-loop-failure
                                  // budget mutation (the ONLY run-path queueStore use,
                                  // workloop.go:3947–:3978 under LockForMutation)
}
```

`beadRunOne`'s signature becomes `(ctx, env RunEnv, ports RunPorts, shared
SharedHandles) (Done, error)`-shaped — 17 params and the `*bool` out-param gone
(success is the Run terminal, M3-D8). The wrapper (workloop.go:3023) reads the
terminal for EM-015f/hk-f722.

## 2. Pinned deletions & exclusions (PF §1, §7)

- DELETE dead fields `h` (:191) and `beadAuditLogger` (:753) — zero readers.
- `staleBlockerCloser` stays outer-loop only (claim-failure path :5566).
- The 4 loop-owned value fields (lastCoordinatorReap/lastDiskCheck/
  lastGoCacheClean/diskLow, :826+) lift OUT of the by-value bundle into
  runWorkLoop-local state (PF §3 hazard: silent no-op writes if ever mutated in
  a run goroutine).
- `cacheReapMu` stays where its reader is (the outer Register path :2970–:2993);
  runexec never sees it.
- The ~47 OUTER fields remain on `workLoopDeps` for M5.

## 3. Nil-means-disabled preservation (PF §5)

Port constructors preserve today's exact defaulting: nil launchSpecBuilder →
buildClaudeLaunchSpec; nil worktreeFactory → productionWorktreeFactory; nil
workerRegistry → local; nil hookStore → skip WaitForOutcome; nil harnessRegistry
→ builder fallthrough; nil Gate → eval-failure Outcome. Adapters live where the
defaults live today; no behavior change.

## 4. ClockPort migration (C1)

- `workLoopDeps` gains `clock substrate.ClockPort`; `newWorkLoopDeps` wires
  `substrate.SystemClock{}`; tests use `substrate.FakeClock`
  (substrate/fakeclock.go) — zero new abstraction (M3-D4, mirrors P1 D4/T5).
- Migration set = the RUN-path census ONLY (PF §4): workloop.go
  :3109/:3134/:4408/:4637/:4647/:4654/:4871/:5063; reviewloop.go
  :551/:562/:568/:659/:738/:862/:1478/:1907; dot_cascade.go
  :235/:1571/:1588/:1591/:1716/:1804/:2514.
- The five `time.After` selects (workloop :4871; reviewloop :659/:738/:1478;
  dot_cascade :1716) become shell timer deadlines (ArmTimer/nearest-deadline
  wake) as they are absorbed by the Dispatch machine; until a site is absorbed,
  it uses `Clock`-based deadlines directly (behavior-identical, deterministic in
  tests).
- OUTER sites (PF §4 list) untouched — M5.

## 5. Sequencing note

C1 lands FIRST (mechanical, green, enables deterministic timeout tests), then
ports land as small per-port promotions (each a compilable, green commit:
introduce port + adapter, move call sites, delete field), then the reactor
consumes them. This is P1's T5→T6→T7 order transposed (keeper: ClockPort →
ports → Step), stated as the required order in 07-tasks.

## 6. Self-review notes

- `LaunchPort` is wide (15 fields' worth). Considered splitting Spec/Spawn/
  Hook/Deliver; kept one port because every consumer (all three modes) uses the
  cluster together (PF §2) — split later if M2's driver wants a narrower
  Deliver-only surface (it will: the M3-D11 contract names Deliver + events; the
  shell adapts).
- `BudgetPort` hides queueStore from the run path — reviewer should check the
  LockForMutation read-modify-write cannot deadlock against the outer loop
  (today's code already takes it from the run goroutine at :3948; unchanged).
- `RunEnv.ProjectCfg` carries the whole ProjectConfig; a stricter cut (just the
  model-preference slice) is possible — kept whole for M3 to avoid a config
  re-plumb; M5 narrows.
