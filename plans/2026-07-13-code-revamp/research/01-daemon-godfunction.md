# Dossier 01 — Daemon run-lifecycle "god function" + dependency struct

Factual anatomy of the daemon core. All references are `file:line` into the
working tree as of 2026-07-13. No verdicts, no recommendations — structure only.

Files in scope (line counts):

| File | Lines |
|------|-------|
| `internal/daemon/workloop.go` | 8,184 |
| `internal/daemon/reviewloop.go` | 2,205 |
| `internal/daemon/dot_cascade.go` | 2,625 |
| `internal/daemon/daemon.go` | 2,346 |

`workloop.go` defines **69 top-level functions** (`grep -cE '^func '`).

---

## 1. `beadRunOne` — the per-bead run function

### 1.1 Signature (17 parameters)

`internal/daemon/workloop.go:3072`

```go
func beadRunOne(ctx context.Context, deps workLoopDeps, runID core.RunID, beadRecord core.BeadRecord, queueName string, queueID *string, queueGroupIndex *int, queueItemIndex int, runSucceeded *bool, extraContext string, itemWorkflowMode string, itemWorkflowRef string, itemTemplateParams map[string]string, itemLocalOnly bool, itemWorkerTarget string, preSelectedWorker *workers.Worker, localSlotHeld bool) {
```

Parameter-by-parameter:

| # | Param | Type | Notes |
|---|-------|------|-------|
| 1 | `ctx` | `context.Context` | per-run context; cancel = shutdown OR per-run abort |
| 2 | `deps` | `workLoopDeps` | **passed BY VALUE** — the 85-field god-struct copied on every dispatch |
| 3 | `runID` | `core.RunID` | stable run identifier |
| 4 | `beadRecord` | `core.BeadRecord` | the claimed bead |
| 5 | `queueName` | `string` | source queue |
| 6 | `queueID` | `*string` | out/optional; stamped onto every terminal event |
| 7 | `queueGroupIndex` | `*int` | optional; QM-011/QM-012 |
| 8 | `queueItemIndex` | `int` | |
| 9 | `runSucceeded` | `*bool` | **OUT-PARAM** — written by the `emitDone` closure (`:3120`) for EM-015f group tracking |
| 10 | `extraContext` | `string` | |
| 11 | `itemWorkflowMode` | `string` | tier-0 per-item override (hk-hiqrl) |
| 12 | `itemWorkflowRef` | `string` | tier-0 DOT ref |
| 13 | `itemTemplateParams` | `map[string]string` | |
| 14 | `itemLocalOnly` | `bool` | |
| 15 | `itemWorkerTarget` | `string` | |
| 16 | `preSelectedWorker` | `*workers.Worker` | remote worker pre-picked by outer loop |
| 17 | `localSlotHeld` | `bool` | drives the `relLocalSlot` mutable-flag release logic |

Return: **none** (void). All outcomes are side effects: events on `deps.bus`,
bead transitions via `deps.brAdapter`, and the `*runSucceeded` out-param.

### 1.2 Size

- `beadRunOne` body spans **`:3072`–`:5438` = ~2,366 lines** (the function
  closes at `:5438`; `isWatcherErrCanceled` begins at `:5448`).
- **189** `hk-` inline annotations inside the function body (of 640 in the whole file).
- **45** `return` statements (early returns) counted in the body.
- **50** `ReopenBead` call-sites, **27** `CloseBead`/`closeBeadWithHistoryTrim`
  call-sites, **45** `emitDone(...)` call-sites — the same
  reopen/close/emit triplet is open-coded repeatedly across the terminal branches.

### 1.3 Major phases, in order (with line boundaries)

The function is NOT split into helpers for its main body; phases are marked only
by single-tab `//` comments. In order:

1. **Slot-release defer + attribution setup** `:3075`–`:3158`
   - `relLocalSlot` mutable flag + deferred `localInFlight.Add(-1)` (`:3081`).
   - `resolveOwningEpicFromRecord` → writes `handle.OwningEpicID` (`:3098`–`:3104`).
   - `emitDone` closure defined (`:3119`) — the local terminal-event wrapper that
     also writes `*runSucceeded`.

2. **Resolution phase (all "sealed once at claim time")** `:3160`–`:3350`
   - workflow_mode (four-tier EM-012a) `:3160`
   - workflow_ref `:3175`
   - (model, effort) EM-012b `:3180`
   - Pi provider profile `:3210`; provider_selected emit `:3235`
   - `activeRepo` / cross-repo safelist gate `:3253`
   - `resolveParentCommit` → `headSHA` / `parentSHA` `:3302`
   - `lands_on` base branch + ProtectBranches gate `:3318`

3. **DD1 remote code-sync setup** `:3351`–`:3590`
   - worker selection (pre-selected `:3381`, fallback SelectWorker `:3393`)
   - `remoteBeadCtx` struct declared locally `:3374`
   - SSH reverse-tunnel spawn + `ensureWorkerHarmonikDir` `:3434`
   - `notifyWorkerOffline` / `preMergeSync` closures defined `:3554` / `:3567`

4. **Worktree creation (under `mergeMu`)** `:3623`–`:3730`
   - `fetchBaseOnWorker` + `git worktree add` serialised under `deps.mergeMu` `:3623`
   - `wtCleanup` / `forceTeardownSession` defers, `useIndepSession` gate `:3681`–`:3720`
   - `snapshotUntrackedFiles` baseline for escape-check `:3724`

5. **run_started emit + DOT pre-load + routed builder pre-build** `:3735`–`:3812`

6. **Mode-dispatch switch** `:3824`–`:4220`
   - `case core.WorkflowModeReviewLoop:` `:3825` → calls `runReviewLoop`
     (reviewloop.go) then does inline scenario-gate → preMergeSync → merge → close
   - `case core.WorkflowModeDot:` `:4014` → drives DOT cascade (dot_cascade.go)
   - `default:` (single mode) `:4221`

7. **Single-mode dispatch (the "Step 1–9" sub-flow)** `:4226`–`:5438`.
   These `// Step N` comments are the launch→monitor→gate→close spine:
   - **Step 1** build launch spec `:4228`; remote socket resolution `:4230`;
     srt sandbox wiring + verification `:4432`/`:4476`
   - **Step 2** register hook session `:4590`
   - **Step 3** emit pre-exec messages before Launch `:4595`
   - **Step 4** per-run tapping emitter + `agentSpawnSem` acquire `:4605`/`:4612`;
     Launch `:4662`; **Step 4a** agent-ready callback wiring `:4731`;
     **Step 4b** paste-inject `:4789`
   - **Step 5** CHB-019 heartbeat goroutine `:4791`
   - **Step 6** `waitAgentReady` (HC-056 timeout) `:4798`; **6a/6b** paste-inject `:4926`
   - **Step 7** wait for watcher / stop-hook grace `:5002`; ProcessExit commit
     fallback (codex/pi) `:5066`; **Step 7a** implementer_phase_complete `:5049`
   - **Step 8** map Wait-return to terminal event `:5111`
   - **Step 9** the terminal decision `:5116` — see 1.4 below.

### 1.4 Terminal decision (Step 9) — the four-way switch

`:5139` escape-worktree guard (held under `mergeMu`, `:5162`) →
`:5188` no-commit guard → `:5230` `switch`:

- `case term.Type == ...AgentCompleted:` `:5231` — scenario gate → preMergeSync →
  `lockedMergeRunBranchToMain` `:5252` → close or reopen.
- `case socketOutcome == nil && ei.exitCode == exitCodeClean && !watcherFailed:`
  `:5277` — pre-bridge close-on-exit-0 fallback; **byte-for-byte duplicate** of
  the merge/close block above (`:5285`–`:5324`).
- `default:` `:5326` — nested `select` on `noChangeTimeoutCh`:
  - `case <-noChangeTimeoutCh:` `:5331` subsumed-in-main check → close or reopen.
  - `default:` `:5355` shutdown / failure: `ctx.Err()` shutdown-drain merge
    (`:5373`) or reopen with `failReason` (`:5407`–`:5435`).

### 1.5 Deepest nesting

Max nesting is **8 leading tabs**, at `internal/daemon/workloop.go:3965`:

```go
								budgetExhausted = true
```

This sits inside the review-loop `case` (`:3825`–`:4013`) merge/budget handling.
The `default:`→`select`→`default:`→`if ctx.Err()`→`if curHeadSHA`→`if
mergeRes...`→`if closeErr`→`if errors.Is` chain in the terminal switch
(`:5355`–`:5387`) reaches comparable depth (7 tabs).

---

## 2. `workLoopDeps` — the god-struct

Definition: `internal/daemon/workloop.go:182`–`:942`.
Constructor: `newWorkLoopDeps` `:1043`–`:1206` (returns `workLoopDeps` by value).

**Field count: 85** (confirmed via Go AST walk of the `TypeSpec`).
The struct is **passed by value** into `beadRunOne` (`:3072`), `runWorkLoop`
(`:1508`), `runReviewLoop`, `driveDotWorkflow`, and every helper — so the 85-field
bundle (including live pointers to mutexes, maps, atomics, and channels) is
copied on each call, but the pointer/interface fields alias shared state.

Struct header (`:182`):

```go
// workLoopDeps bundles the injectable dependencies of the work loop.  All
// fields are required (non-nil).  Use newWorkLoopDeps to construct the
// production set from daemon.Config.
type workLoopDeps struct {
```

### 2.1 Fields grouped by concern

**Ledger / CLI adapters** — `brAdapter beadLedger` (`:185`), `brPath` (`:219`),
`kerfPath` (`:212`), `beadAuditLogger func(...)` (`:753`), `brTimeoutCfg
brcli.TimeoutConfig` (`:259`), `staleBlockerCloser lifecycle.BeadCat3cCloser`
(`:649`), `strandedInProgressResetter` (`:657`).

**Event bus / handler / harness** — `bus handlercontract.EventEmitter` (`:188`),
`h handler.Handler` (`:191`), `adapterRegistry *handlercontract.AdapterRegistry`
(`:463`), `harnessRegistry *handlercontract.HarnessRegistry` (`:480`),
`launchSpecBuilder func(...)` (`:352`), `defaultHarness core.AgentType` (`:540`),
`substrate handler.Substrate` (`:489`), `hookStore hookStoreIface` (`:342`).

**Paths / process** — `intentLogDir` (`:195`), `projectDir` (`:198`),
`allowedRepos []string` (`:204`), `handlerBinary` (`:241`), `daemonBinaryPath`
(`:247`), `handlerArgs []string` (`:251`), `handlerEnv []string` (`:256`),
`targetBranch` (`:719`), `protectBranches []string` (`:727`),
`worktreeFactory func(...)` (`:366`).

**Concurrency gating (mutable / shared)** — `runRegistry *RunRegistry` (`:288`),
`maxConcurrent int` (`:309`), `concurrencyCtrl *ConcurrencyController` (`:317`),
`localInFlight *atomic.Int32` (`:329`), `agentSpawnSem chan struct{}` (`:430`).

**Mutexes (see §3)** — `followUpLedgerMu *sync.Mutex` (`:229`),
`mergeMu *sync.Mutex` (`:384`), `worktreeCreateMu *sync.Mutex` (`:407`),
`emittedEpicsMu *sync.Mutex` (`:437`), `cacheReapMu *sync.RWMutex` (`:894`).

**Mutable maps / dedup ledgers** — `followUpLedger map[string]struct{}` (`:228`),
`followUpLedgerPath` (`:238`), `emittedEpics map[core.BeadID]struct{}` (`:436`),
`heldEventDedup map[string]struct{}` (`:639`).

**Queue surface** — `queueStore *QueueStore` (`:551`), `submitWakeC <-chan
struct{}` (`:563`), `queueLedger queue.BeadLedger` (`:575`),
`cancelOnQueueDrain context.CancelFunc` (`:586`), `cancelOnQueueExit
context.CancelFunc` (`:596`), `stopDispatchCtx context.Context` (`:611`),
`noAutoPull bool` (`:702`).

**Pause / gating controllers** — `handlerPauseController *HandlerPauseController`
(`:626`), `operatorPauseCtrl *OperatorPauseController` (`:685`),
`decisionBlocker *DecisionBlocker` (`:695`).

**Timeouts / config** — `workflowModeDefault core.WorkflowMode` (`:275`),
`agentReadyTimeout` (`:505`), `remoteAgentReadyTimeout` (`:515`),
`postAgentReadyHangTimeout` (`:523`), `projectCfg ProjectConfig` (`:531`),
`tidGen *core.TransitionIDGenerator` (`:263`).

**Remote worker substrate** — `workerRegistry *workers.Registry` (`:736`),
`runner tmuxpkg.CommandRunner` (`:745`).

**Periodic maintenance (mutable, work-loop-goroutine-owned, NO lock)** —
`lastCoordinatorReap time.Time` (`:826`), `lastDiskCheck time.Time` (`:834`),
`lastGoCacheClean time.Time` (`:841`), `diskLow bool` (`:849`) — plus their
tunables `coordinatorReapAdapter/Interval/ProjectHash` (`:804`/`:819`/`:811`),
`diskLowWatermark` (`:855`), `diskCheckIntervalOverride` (`:861`),
`goCacheCleanIntervalOverride` (`:867`), and the injectable funcs
`diskFreeBytesFunc` (`:874`), `goCacheCleanFunc` (`:881`), `worktreeReclaimFunc`
(`:902`).

**Stranded-reset / cross-session epoch** — `strandedResetProjectHash` (`:664`),
`strandedResetDaemonNS int64` (`:671`), `skipBrHistoryRotation bool` (`:710`).

**Schedule + crew + comms** — `scheduleStore *schedule.Store` (`:762`),
`crewHandler crewStarter` (`:771`), `commsWhoQuerier` (`:779`), `commsSend
commsSendFunc` (`:787`), `scheduleWakeC <-chan struct{}` (`:794`).

**Sentinel governor** — `governorState *sentinel.GovernorState` (`:911`),
`governorCfg sentinel.Config` (`:919`), `sentinelMode string` (`:926`),
`sentinelPhase2Classes []string` (`:933`).

**Sandbox / spawn readiness** — `sandboxCfg SandboxConfig` (`:941`),
`spawnSubstrateReadyCh <-chan struct{}` (`:497`), `cpRegistry core.Registry`
(`:452`).

### 2.2 Value-vs-pointer / mutable-state notes

- **Copied-by-value but aliasing shared state:** every `*sync.Mutex`,
  `*RunRegistry`, `*atomic.Int32`, `map`, `chan`, and interface field. Copying
  the struct does NOT clone these — the copy shares the same lock/map/atomic.
- **`godoc` "all fields required (non-nil)"** is aspirational: many fields carry
  explicit "when nil the gate is disabled" fallbacks (e.g. `handlerPauseController`
  `:614`, `operatorPauseCtrl` `:675`, `decisionBlocker` `:691`,
  `staleBlockerCloser` `:645`).
- **`lastCoordinatorReap`/`lastDiskCheck`/`lastGoCacheClean`/`diskLow`** are
  plain mutable value fields, documented as "guarded entirely by the work-loop
  goroutine — no locking needed" (`:823`, `:830`, `:837`). Because `deps` is
  copied by value into each `beadRunOne` goroutine, per-goroutine mutation of
  these is not visible to the outer loop — they are only mutated by
  `runWorkLoop` itself, not the dispatch goroutines.

---

## 3. Every mutex in the daemon core

Five distinct locks; production wires them in `newWorkLoopDeps`
(`:1157`–`:1166`). Test override for `mergeMu` lives on `daemonTestHooks`
(`daemon.go:619`).

| Lock | Field decl | Guards | Held across IO? |
|------|-----------|--------|-----------------|
| `mergeMu` | `workloop.go:384` | (a) the full rebase→build→push→reset→br-sync merge; (b) `fetchBaseOnWorker`+`git worktree add`; (c) escape-worktree check | **YES — network/build/git, see below** |
| `worktreeCreateMu` | `workloop.go:407` | `workspace.CreateWorktree` retry loop on remote workers | git worktree-add IO on worker (SSH) |
| `cacheReapMu` (`*sync.RWMutex`) | `workloop.go:894` | reaper takes W-lock for the whole `go clean -cache` (up to 5 min); each `Register` takes R-lock | **YES — W-lock held across `go clean -cache` (up to 5 min)** |
| `followUpLedgerMu` | `workloop.go:229` | `followUpLedger` map insert (staged-bead idempotency) | no (map op only) |
| `emittedEpicsMu` | `workloop.go:437` | `emittedEpics` map (at-most-once epic_completed) | no (map op only) |

### 3.1 `mergeMu` held across network/build/git IO — the critical case

`lockedMergeRunBranchToMain` locks `mu` and holds it (via `defer`) across the
ENTIRE `mergeRunBranchToMain` call:

`internal/daemon/workloop.go:6512`

```go
func lockedMergeRunBranchToMain(ctx context.Context, mu *sync.Mutex, projectDir string, runID core.RunID, bus handlercontract.EventEmitter, beadID core.BeadID, headSHA string, targetBranch string, protectBranches []string, brPath string) mergeOutcome {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	return mergeRunBranchToMain(ctx, projectDir, runID, bus, beadID, headSHA, targetBranch, protectBranches, brPath)
}
```

`mergeRunBranchToMain` (`:6544`) runs the following IO **while the lock is held**,
inside a retry loop `for pushAttempt := 1; pushAttempt <= maxPushAttempts (3)`
(`:6751`):

- `git worktree add` `:6630`
- `git rebase <target>` `:6679`, `:6786` (retry), + `git rebase --abort` `:6687`/`:6789`
- `git merge-base --is-ancestor` FF-check `:6755`
- `git update-ref refs/heads/<target>` `:6821`, rollbacks `:6870`/`:6906`/`:6947`
- **post-merge build gate** (`go build` + `go vet`) `:6830`–`:6889`
- **post-merge fmt-check gate** `runMergeFmtCheck` `:6889`
- **`git push origin <target>`** (network) `:6895`
- `git reset --hard HEAD` (working-tree refresh) `:7022`
- **`br sync --import-only`** `:7056`

Merge callers (all hold `mergeMu` for this whole sequence):
`workloop.go:3846`-region (review-loop), `:5252` (agent_completed), `:5302`
(auto-close), `:5374` (shutdown-drain).

Additionally `mergeMu` is acquired directly (not via the `locked…` wrapper) to
serialise **worktree creation** and the **escape-worktree check**:

- `mergeMu` around `fetchBaseOnWorker` + `git worktree add` — comment `:3623`,
  rationale hk-lt091/hk-h8u7p.
- `mergeMu` around the implementer-escaped-worktree main-repo dirty check —
  comment `:5162` (hk-zguy6): "hold mergeMu during the escape check so that a
  sibling's update-ref → reset-hard sequence cannot race."

So a single global `mergeMu` serialises: remote worktree fetch/add, the full
build+push+sync merge, and the post-run escape check — meaning at most one bead
across ALL queues can be in any of those phases at once.

### 3.2 `daemon.go` mergeMu (test hook)

`internal/daemon/daemon.go:619` is the `daemonTestHooks.mergeMu *sync.Mutex`
override field (comment `:613`), NOT a separate production lock. Production's
`mergeMu` is unconditionally set in `newWorkLoopDeps` (`workloop.go:1161`).

---

## 4. The implicit state machine

There is no single explicit state enum for a run. State is spread across four
representations:

### 4.1 Formal lifecycle Machine (`hclifecycle`)

`beadRunOne` drives an `hclifecycle.Machine` through three terminal states only:
`StateTerminating`, `StateTerminated`, `StateFailed` (`grep` in workloop.go).
Transition driver: `transitionToTerminated` (`:7994`) and
`emitWorkloopLifecycleTransition` (`:8022`). Comment at `:5031` (HC-065): "Drive
StateTerminating → StateTerminated/StateFailed transitions."

### 4.2 Pre-exec lifecycle event chain (the observable "states")

Emitted on the bus BEFORE Launch by `emitPreExecBeforeLaunch` (`:5892`), in a
fixed order: **handler_capabilities → session_log_location → skills_provisioned**,
with **launch_initiated** held back and emitted only AFTER Launch returns a live
session (hk-4l7zs, `:4662`), then **agent_ready** resolved by `waitAgentReady`.
Event-type occurrence counts in workloop.go: `agent_ready` ×39,
`launch_initiated` ×11, `handler_capabilities`/`session_log_location`/
`skills_provisioned` ×1 each, `harness_selected`/`provider_selected` ×1 each.

### 4.3 Terminal-decision state (the Step-9 switch)

Discriminated in the `switch` at `workloop.go:5230` on `term.Type`,
`socketOutcome`, `ei.exitCode == exitCodeClean`, `watcherFailed`, and the
`noChangeTimeoutCh` select. Outcomes: agent_completed→merge→close; exit-0
auto-close; noChange-subsumed→close; noChange-timeout→reopen; shutdown-drain
→merge-or-reopen; failure→reopen (see §1.4).

### 4.4 Review-loop iteration state machine

`reviewLoopState` struct: `internal/daemon/reviewloop.go:135`–`:179`.
Fields: `iterationCount int` (`:138`, 1-based), `claudeSessionID` (`:142`),
`lastVerdictNotes` (`:146`), `lastDiffHash` (`:157`), `lastIterHeadSHA` (`:166`),
`priorVerdicts []workspace.ReviewTargetPriorVerdict` (`:171`), `lastVerdictFlags
[]string` (`:178`). `iterationCount`/`iteration_count` appears **108×** in
reviewloop.go — it is the de-facto loop-state variable.

### 4.5 The resume / relaunch branch (the resume-hang path)

The relaunch path is the review-loop `for {}` loop at `reviewloop.go:250`. On
**iteration ≥ 2** (`:252`):

```go
if state.iterationCount >= 2 {
	// Iteration ≥ 2: emit implementer_resumed BEFORE dispatch per EM-015d.
	priorSummary := rlTruncateUTF8(state.lastVerdictNotes, priorVerdictSummaryMaxBytes)
	emitImplementerResumed(ctx, deps.bus, runID, state.claudeSessionID, state.iterationCount, priorSummary)
}
```

Phase becomes `ReviewLoopPhaseImplementerResume` (`:276`) with
`priorClaudeSessID = &prior` (`:278`) → launches `claude --resume <uuid>`.
Pre-exec messages (handler_capabilities → session_log_location →
skills_provisioned) are emitted before Launch at `:535`
(`emitPreExecBeforeLaunch`). The documented **resume-hang** is exactly here: a
`--resume` reattach renders the welcome splash and does NOT reliably re-fire the
`SessionStart` hook, so `agent_ready` never arrives after `skills_provisioned` →
SILENCE. The mitigation is the fixed-grace fallback:

- `resumeReadyFallbackGrace = 2 * time.Second` (`reviewloop.go:110`), rationale
  comment `:85`–`:110` and `:633`–`:665` (hk-isq02): after the grace window a
  synthetic ready is emitted so the resume phase does not hang forever.
- The fallback goroutine is bounded by `resumeReadyFallbackCtx` (`:650`),
  cancelled once the ready-wait returns (`:728`, `:785`).

---

## 5. Shared mutable state threaded through `beadRunOne`

Locals/closures/pointers mutated across phases (all within one goroutine unless
noted):

- **`relLocalSlot bool`** (`:3081`) — mutated to `false` if the fallback
  SelectWorker turns a local dispatch remote; read by the exit defer (`:3082`).
- **`runTipSHA *string`** (`:3092`) — set in the DOT/failure path; read by
  `emitDone` (`:3140`) and every `emitRunCompleted`.
- **`sdModel`, `sdHarness string`** (`:3110`) — assigned after resolution, read
  by the `emitDone` sessiondata goroutine (`:3148`).
- **`emitDone` closure** (`:3119`) — captures `runSucceeded`, `runTipSHA`,
  `queueID`, `owningEpicID/Assignee`; writes `*runSucceeded` (`:3121`).
- **`owningEpicID/owningEpicAssignee`** (`:3098`) — also written onto the shared
  `RunHandle` in `deps.runRegistry` (`:3102`–`:3103`), which the StaleWatcher
  goroutine reads concurrently.
- **`rbc *remoteBeadCtx`** (declared `:3374`) — nil for local, mutated by the
  worker-selection branches; gates every remote-only block (`rbc != nil`).
- **`preMergeSync` / `notifyWorkerOffline` closures** (`:3567` / `:3554`) —
  capture `rbc` and worker state; invoked in each terminal merge branch.
- **`headSHA` / `parentSHA` / `activeRepo` / `baseBranch`** — resolved once, read
  by worktree-add, no-commit guard, and every merge call.
- **`noChangeTimeoutCh`** — declared unconditionally (comment `:4932`), selected
  on in the terminal `default:` branch (`:5331`).
- **`sess` (handler.Session)** — predeclared so `agentEndCb` can capture it by
  reference before Launch reassigns it (comment `:4546`).
- **`deps.localInFlight` (`*atomic.Int32`)** and **`deps.runRegistry`** — the two
  genuinely cross-goroutine shared mutables the function touches.

---

## 6. Concrete complexity signals

| Signal | Value | Source |
|--------|-------|--------|
| `hk-` annotations in `workloop.go` (whole file) | **640** | `grep -oE 'hk-[a-z0-9.]+' \| wc -l` |
| `hk-` annotations inside `beadRunOne` | **189** | lines 3072–5447 |
| `beadRunOne` body length | **~2,366 lines** | `:3072`–`:5438` |
| Early `return` statements in `beadRunOne` | **45** | body grep |
| `ReopenBead` call-sites in `beadRunOne` | **50** | body grep |
| `CloseBead`/`closeBeadWithHistoryTrim` call-sites | **27** | body grep |
| `emitDone(...)` call-sites | **45** | body grep |
| Deepest nesting | **8 tabs** at `:3965` (`budgetExhausted = true`) | indent scan |
| Merge/close block duplication | **4 near-identical copies** | review-loop `:3846`+, agent_completed `:5252`, auto-close `:5302`, shutdown-drain `:5374` |
| Distinct workflow-mode branches | **3** | `ReviewLoop :3825`, `Dot :4014`, `default/single :4221` |
| Top-level functions in `workloop.go` | **69** | `grep -cE '^func '` |
| `workLoopDeps` fields | **85** | Go AST walk |
| `iterationCount` references (reviewloop.go) | **108** | grep |
| Locks in daemon core | **5** (`mergeMu`, `worktreeCreateMu`, `cacheReapMu`, `followUpLedgerMu`, `emittedEpicsMu`) | §3 |

The dominant complexity driver: the launch→gate→merge→close terminal logic is
open-coded four times (once per outcome branch) rather than factored, each copy
re-deriving scenario-gate → preMergeSync → `lockedMergeRunBranchToMain` → close-or-reopen,
all under a single global `mergeMu` that spans build + network + git IO.
