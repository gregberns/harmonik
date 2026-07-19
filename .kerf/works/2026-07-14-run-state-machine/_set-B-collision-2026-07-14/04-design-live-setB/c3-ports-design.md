# 04-Design / C3 — workLoopDeps decomposition into ports

> Pass 4 design. Elaborates D4 (00-decisions). Research: `03-research/c3-workloopdeps-ports/findings.md`.
> Target spec: `specs/run-state-machine.md` §Run-lifecycle ports (RSM).

## Current state
`workLoopDeps` (workloop.go:182-942) is an 85-field god-struct passed BY VALUE into
`beadRunOne` (:3072) and every run helper. Its pointer/map/mutex/chan fields alias shared
cross-goroutine state; its value fields are frozen per-run copies. No consumer-owned port
boundaries exist. Two fields (`h`, `beadAuditLogger`) are dead (assigned, never read).

## Target state
- **RSM-PORT-1.** The run lifecycle consumes eight consumer-owned narrow ports, declared
  beside their consumers and satisfied structurally by the daemon shell (keeper D10 idiom):
  LedgerPort, EmitterPort, WorktreePort, MergePort (D3), LaunchPort, ClockPort (D2),
  WorkerPort, GatePort. Existing interfaces reused by alias where possible
  (`EmitterPort = handlercontract.EventEmitter`). Concrete bundle shape (internal/daemon/runports.go):

```go
// RunPorts — behavioral dependencies of the run shell. Narrow, structural.
type RunPorts struct {
    Ledger   LedgerPort    // brAdapter ops + closeBeadWithHistoryTrim; carries brTimeoutCfg/intentLogDir/tidGen
    Emitter  EmitterPort   // = handlercontract.EventEmitter subset (type alias — keeper ports.go:107)
    Worktree WorktreePort  // Create(ctx, spec) (path, cleanup, error); BaseSync for remote
    Merge    MergePort     // Prepare/Submit + escape-check submit (mergequeue handle, D3)
    Launch   LaunchPort    // BuildSpec, Spawn (substrate), Registries, hookStore ops, Submit* (M2 seam / paste-inject), timeouts, sandbox
    Gate     GatePort      // cpRegistry eval (dot_gate only; nil ⇒ eval-failure Outcome)
    Clock    substrate.ClockPort
}
// RunEnv — immutable per-run values (no behavior): ProjectDir, TargetBranch, BrPath,
//   ProtectBranches, AllowedRepos, WorkflowModeDefault, DefaultHarness, ProjectCfg,
//   RunID, BeadRecord, QueueName/ID/GroupIndex/ItemIndex, Item* overrides.
// SharedHandles — cross-goroutine state, shared-by-reference:
//   RunRegistry, LocalInFlight *atomic.Int32, AgentSpawnSem chan struct{}, Workers,
//   Budget BudgetPort (one-method wrap of the ONLY run-path queueStore use — the
//   review-loop-failure budget mutation, workloop.go:3947-3978 under LockForMutation).
```
  `beadRunOne`'s signature becomes `(ctx, env RunEnv, ports RunPorts, shared SharedHandles)
  (Done, error)`-shaped — 17 params and the `*bool` out-param gone (success is the Run
  terminal, D8); the wrapper (workloop.go:3023) reads the terminal for EM-015f/hk-f722.
- **RSM-PORT-2 (the cut line).** Only run-lifecycle fields promote to ports. Shared
  concurrency gates (runRegistry, localInFlight, tidGen, agentSpawnSem, cacheReapMu) stay
  shared-by-reference. Periodic-maintenance value fields (lastCoordinatorReap/lastDiskCheck/
  lastGoCacheClean/diskLow) lift OUT of the by-value bundle into loop-owned state. The other
  ~50 fields stay on `workLoopDeps` for M5.
- **RSM-PORT-3.** `queueStore` does NOT become a run port; its one run write (review-loop
  budget) is surfaced as a terminal-event field the dispatch side applies.
- **RSM-PORT-4.** `staleBlockerCloser` is NOT on LedgerPort (claim/dispatch-side). The two
  dead fields are deleted (census 85→83).
- **RSM-PORT-5.** Nil-means-disabled run-path fields resolve at wiring: config fallbacks
  fold defaults into the production port constructor; feature toggles use a nil-port-or-no-op
  adapter (keeper idiom). The three headline controllers (handler/operator pause,
  decisionBlocker) are dispatch-side and out of M3 run-port scope.

## Rationale
A pure `Step` cannot take an 85-field by-value bundle; the ports are the boundary C4 needs.
Each promotion is a small, green, mechanical change. The by-value copy already implies every
RUN field is immutable config or a shared handle — the decomposition preserves this (values
→ RunConfig bag; shared state → reference-typed ports) and removes the latent frozen-copy trap.

## Requirements traceability
02-components C3 → RSM-PORT-1..5. Goal "beadRunOne a thin driver" (01 §2) → RSM-PORT-1/2.

## PLANNER-RECONCILE
Eight ports (not the dossier's six); WorkerPort+GatePort added, two fields corrected, two
deleted. LaunchPort-vs-AgentWaitPort split left as one LaunchPort unless directed (D4, item 3).
