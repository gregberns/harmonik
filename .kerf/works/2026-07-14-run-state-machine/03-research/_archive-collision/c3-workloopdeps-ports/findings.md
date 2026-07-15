# 03-Research / C3 — workLoopDeps decomposition into ports

> Pass 3 (Research), component C3. All `file:line` verified against the current
> working tree (branch `phase1-session-restart-substrate`, 2026-07-14). Struct
> `internal/daemon/workloop.go:182-942` (**exactly 85 fields**); ctor
> `newWorkLoopDeps` :1043-1177; `beadRunOne` :3072-5447 takes `deps workLoopDeps`
> **by value**. Delegated to a fresh-context sub-agent (exhaustive `deps.<field>`
> grep + enclosing-function map + call-graph cross-check); parent owns this write.

## Research questions

1. Full field census with RUN / SHARED-GATE (SG) / MAINT / OTHER / DEAD classification (the C3 cut line).
2. Validate the dossier's candidate port groupings against actual usage.
3. The keeper D10 port idiom (the template M3 must follow).
4. Nil-means-disabled fields and their guard sites.
5. By-value copy semantics of workLoopDeps into beadRunOne.
6. The internal/queue port idiom.

## Verified call graph (the run-lifecycle boundary)

`beadRunOne` = :3072-5447. Transitive deps-taking callees (all by value): `runReviewLoop` (reviewloop.go, called :3846), `driveDotWorkflow` (dot_cascade.go:183, called :4092), `dispatchDotAgenticNode` (dot_cascade.go:1212), `dispatchDotGateNode`/`executeCognitionGate` (dot_gate.go:93/:266), `(deps *workLoopDeps).closeBeadWithHistoryTrim` (:962, 8 sites all in beadRunOne), `emitBeadClosedAndMaybeEpic`->`maybeEmitEpicCompleted` (:7606/:7617), `lockedMergeRunBranchToMain` (:6512 — takes **explicit args** mu/projectDir/bus/targetBranch/protectBranches/brPath, already port-shaped), `fetchBaseOnWorker` (codesync_rs_b8.go:73 — takes explicit `tmux.CommandRunner`, **NOT a deps field**).

**Boundary functions** (dispatch goroutine, AFTER beadRunOne returns — :3041/:3047): `evaluateGroupAdvanceWithOutcome` (:6264) and `stagedBeadGeneratorEval` (eagerfill_em063.go:389). Queue/flywheel *reactions to* run completion, not the run — stay OUTSIDE M3 run ports (reactor emits the terminal event; the shell/queue side consumes).

## Findings — the census (85 fields)

Legend: **RUN** = M3 promotes to port/reactor input; **SG** = shared-by-reference concurrency gate; **MAINT** = work-loop-goroutine-owned maintenance value; **OTHER** = dispatch-gate or sibling subsystem (M5); **DEAD** = assigned, never read in production.

| # | Field | Class | Evidence (enclosing fns) |
|---|---|---|---|
| 1 | brAdapter | **RUN**(+claim) | beadRunOne x37; closeBeadWithHistoryTrim; maybeEmitEpicCompleted; runWorkLoop x8 |
| 2 | bus | **RUN** | beadRunOne x50; runReviewLoop x51; dot_cascade x22; dot_gate x6 |
| 3 | h | **DEAD** | assigned :1125; zero `deps.h` reads anywhere |
| 4 | intentLogDir | **RUN** | beadRunOne x36; closeBeadWithHistoryTrim; adoptLiveRunSession |
| 5 | projectDir | **RUN**(cfg) | beadRunOne x25 + every subsystem |
| 6 | allowedRepos | **RUN** | beadRunOne:3276 only |
| 7 | kerfPath | OTHER | eagerRefillEval only |
| 8 | brPath | **RUN**(+2 sibling) | beadRunOne x5 (merge arg :3904); runWorkLoop:1855; stagedBeadGeneratorEval |
| 9-11 | followUpLedger(+Mu,+Path) | OTHER | stagedBeadGeneratorEval |
| 12 | handlerBinary | **RUN** | four run fns |
| 13 | daemonBinaryPath | **RUN** | four run fns + daemon.go |
| 14 | handlerArgs | **RUN** | four run fns |
| 15 | handlerEnv | **RUN**(+schedule) | four run fns + scheduletick x2 |
| 16 | brTimeoutCfg | **RUN** | beadRunOne x36; closeBeadWithHistoryTrim |
| 17 | tidGen | **RUN + SG** | beadRunOne x37; runWorkLoop:2774 (one shared generator, claim+run — EM-018a) |
| 18 | workflowModeDefault | **RUN** | beadRunOne:3168 only |
| 19 | runRegistry | **SG** | beadRunOne x6; runWorkLoop x6 |
| 20 | maxConcurrent | OTHER(gate) | runWorkLoop:1514 |
| 21 | concurrencyCtrl | OTHER(gate) | runWorkLoop x2 |
| 22 | localInFlight | **SG** | beadRunOne x4 (remote-fallback correction); runWorkLoop x4 |
| 23 | hookStore | **RUN** | beadRunOne x4; runReviewLoop x22; dot_cascade x11; dot_gate x11 |
| 24 | launchSpecBuilder | **RUN** | beadRunOne x7; runReviewLoop x4 |
| 25 | worktreeFactory | **RUN** | beadRunOne:3592 (nil->production/remote inline factory) |
| 26 | mergeMu | **SG(run-path only)** -> C2 absorbs | beadRunOne x15 (nil-guarded x5) |
| 27 | worktreeCreateMu | **SG(run-path only)** | beadRunOne:3602 only |
| 28 | agentSpawnSem | **SG(run-path only)** | beadRunOne:4621 x3 only |
| 29-30 | emittedEpics(+Mu) | **RUN(terminal)+SG** | maybeEmitEpicCompleted |
| 31 | cpRegistry | **RUN** | dispatchDotGateNode:118 x2 (nil->structural FAIL outcome) |
| 32 | adapterRegistry | **RUN** | beadRunOne x4; runReviewLoop x7 |
| 33 | harnessRegistry | **RUN** | beadRunOne x8; runReviewLoop x6; dot_cascade x10 |
| 34 | substrate | **RUN**(+outer probe) | beadRunOne x12; runReviewLoop x13; also exitClean windowCleaner :1634 |
| 35 | spawnSubstrateReadyCh | OTHER(boot) | runWorkLoop x2 |
| 36 | agentReadyTimeout | **RUN**(cfg) | run fns |
| 37 | remoteAgentReadyTimeout | **RUN**(cfg) | run fns |
| 38 | postAgentReadyHangTimeout | **RUN**(cfg) | runReviewLoop x3 |
| 39 | projectCfg | **RUN** | beadRunOne x3 |
| 40 | defaultHarness | **RUN** | beadRunOne x2; dot_cascade |
| 41 | queueStore | **BOUNDARY** — one true RUN write (:3947 review-loop-failure budget) + terminal group-advance; else dispatch-side | beadRunOne x2; evaluateGroupAdvance x4; runWorkLoop x12 |
| 42 | submitWakeC | OTHER(dispatch) | runWorkLoop x26 |
| 43 | queueLedger | OTHER(dispatch) | runWorkLoop:2146 |
| 44-45 | cancelOnQueueDrain/Exit | OTHER(terminal-boundary) | evaluateGroupAdvance only |
| 46 | stopDispatchCtx | OTHER(dispatch) | runWorkLoop x4 |
| 47 | handlerPauseController | OTHER(gate; **nil=disabled**) | runWorkLoop:2261,:2696 |
| 48 | heldEventDedup | OTHER(dispatch) | runWorkLoop |
| 49 | staleBlockerCloser | OTHER(claim path, NOT run) — **dossier correction** | autoCloseStaleBlockersOnClaimFailure :5566 (from runWorkLoop:2895) |
| 50-52 | strandedInProgressResetter(+hash,+NS) | OTHER(dispatch) | runWorkLoop |
| 53 | operatorPauseCtrl | OTHER(gate; nil=disabled) | runWorkLoop:1859,:1911,:2645 |
| 54 | decisionBlocker | OTHER(gate; nil=disabled) | runWorkLoop x14 (7 nil-guards) |
| 55 | noAutoPull | OTHER(dispatch) | runWorkLoop:2633 |
| 56 | skipBrHistoryRotation | **RUN**(LedgerPort cfg) | closeBeadWithHistoryTrim:969 only |
| 57 | targetBranch | **RUN**(MergePort) | beadRunOne x7 |
| 58 | protectBranches | **RUN**(MergePort) | beadRunOne x3 |
| 59 | workerRegistry | **RUN + SG**(+outer pre-select) | beadRunOne x7; runWorkLoop:3001 |
| 60 | runner | **RUN** | beadRunOne:4085 x2 only |
| 61 | beadAuditLogger | **DEAD in production** | assignment :1170 only (`beadExplicitlyReopened` has no prod caller, godoc :747) |
| 62-66 | scheduleStore/crewHandler/commsWhoQuerier/commsSend/scheduleWakeC | OTHER(schedule) | scheduletick |
| 67-69 | coordinatorReapAdapter/Hash/Interval | MAINT | runWorkLoop |
| 70 | lastCoordinatorReap | MAINT(loop-owned mutable) | runWorkLoop x2 |
| 71-73 | lastDiskCheck/lastGoCacheClean/diskLow | MAINT(mutable) | runPeriodicDiskCheck (+diskLow read by dispatch gate SAME goroutine :1727) |
| 74-78 | diskLowWatermark/diskCheckIntervalOverride/goCacheCleanIntervalOverride/diskFreeBytesFunc/goCacheCleanFunc | MAINT | diskcheck |
| 79 | cacheReapMu | **SG(dispatch-side)** — RLock at Register (:2970), WLock in reaper; **never in beadRunOne** | diskcheck x8; runWorkLoop x4 |
| 80 | worktreeReclaimFunc | MAINT | diskcheck |
| 81-84 | governorState/governorCfg/sentinelMode/sentinelPhase2Classes | OTHER(sentinel) | runWorkLoop |
| 85 | sandboxCfg | **RUN**(LaunchPort) | beadRunOne x6; dispatchDotAgenticNode x6 |

**Tally:** RUN ~33 (incl. 3 boundary-flagged), SG 8 (runRegistry, localInFlight, mergeMu, worktreeCreateMu, agentSpawnSem, cacheReapMu, tidGen, emittedEpicsMu — of which mergeMu/worktreeCreateMu/agentSpawnSem are run-path-exclusive), MAINT 13, OTHER 29, **DEAD 2** (`h`, `beadAuditLogger` — delete-in-M3 candidates, shrinks census to 83).

## Candidate port map (dossier validated / adjusted)

| Port | Fields (verified) | Adjustments |
|---|---|---|
| **LedgerPort** | brAdapter, intentLogDir, brTimeoutCfg, skipBrHistoryRotation (+projectDir for trim), tidGen | **Drop `staleBlockerCloser`** (claim-failure/dispatch-side only). `beadLedger` iface (:975) is already the consumer-owned narrow interface; `closeBeadWithHistoryTrim` (:962) is the LedgerPort close method in embryo. tidGen must stay one shared instance. |
| **EmitterPort** | bus | Already `handlercontract.EventEmitter`; straight promotion (keeper `EmitterPort = Emitter` alias). Consider folding emittedEpics(+Mu) behind a `CompletionEmitter` method so the map/mutex stop travelling raw. |
| **WorktreePort** | worktreeFactory, worktreeCreateMu (+projectDir) | `fetchBaseOnWorker` is **not a deps field** (codesync_rs_b8.go:73 explicit runner) — belongs with the Worker/Remote port. Port must own BOTH local and remote construction (beadRunOne builds inline remote factory when worktreeFactory==nil && rbc!=nil, :3592-3606). |
| **MergePort** (C2's queue) | mergeMu, targetBranch, protectBranches, brPath (+projectDir, bus) | `lockedMergeRunBranchToMain` (:6512) already takes exactly these explicit args — the port is the C2 merge-queue submit surface; mergeMu disappears into it. |
| **LaunchPort** | launchSpecBuilder, harnessRegistry, substrate, handlerBinary, daemonBinaryPath, handlerArgs, handlerEnv, defaultHarness, sandboxCfg, agentSpawnSem | Consider split: **AgentWaitPort** for hookStore + adapterRegistry + agentReadyTimeout + remoteAgentReadyTimeout + postAgentReadyHangTimeout (the agent_ready/outcome wait surface, distinct from spec-building). |
| **ClockPort** (C1) | — none today | No time-source field; Duration configs become timer inputs the C4 shell arms via ClockPort. |
| **WorkerPort / RemotePort** (new) | workerRegistry, runner (+ codesync helpers) | Dossier missed; 7 beadRunOne uses. |
| **QueuePort (narrow)** | queueStore (ONE run write :3947) | Better: convert the budget write + group-advance into terminal **events** the queue side consumes — keeps queueStore out of run ports entirely. |
| **RunConfig (plain value, not a port)** | projectDir, allowedRepos, workflowModeDefault, projectCfg, brPath, timeouts | Pure data; reactor input, no interface. |
| **Stays shared-by-reference (no port)** | runRegistry, localInFlight, tidGen, cacheReapMu | (cacheReapMu is dispatch-side only — not a run port concern.) |

## Patterns to follow

**Keeper D10 idiom** (`internal/keeper/ports.go` + `.kerf/works/session-restart-substrate/04-design/00-decisions.md` §D10):
- Named narrow interfaces declared **in the consumer package** (ports.go): PanePort, GaugePort, HandoffPort, EmitterPort, ClockPort (+ one-method RespawnPort rather than bloating PanePort).
- The old config's function-fields retained as **wiring inputs**; small `fn*` adapter structs (fnPane/fnGauge/fnHandoff/fnRespawn, ports.go:117-268) fold them into ports inside the constructor (cycle.go:664-676) — every existing construction site + test fake keeps working (mechanical-green migration).
- Ports are **optional overrides** on the config (`CyclerConfig.Pane PanePort`, cycle.go:105): non-nil injected port wins; nil falls back to fn-adapter. Nil RespawnPort = dormant escalation (nil-means-disabled at the port level, not scattered nil checks).
- Reads **batched into a snapshot** (`GateSnapshot` via `GaugePort.Snapshot`) so pure `Step` never touches a port — shell samples once per tick, puts it on the event. Direct precedent for restructuring beadRunOne's scattered deps reads.
- Existing-interface reuse by aliasing: `type EmitterPort = Emitter` (ports.go:107).

**internal/queue idiom:** consumer-owned minimal interfaces beside their consumer — `BeadLedger` (validation.go:144), `HandlerPauseChecker` (validation.go:170, godoc "nil == check disabled"), `QueueSetter`/`EventEmitter` (rpc.go:56/:66, shaped to match `handlercontract.EventEmitter` structurally so the bus passes with no adapter). Godoc pins the concurrency contract + nil semantics. The daemon already has `beadLedger` (:975) for this exact struct.

## Nil-means-disabled inventory

RUN-set nil-guards: launchSpecBuilder (nil->routed builder :3797), worktreeFactory (nil->production/remote :3592), harnessRegistry (nil->claude, 12 sites), hookStore (~20 sites, wait skipped), cpRegistry (nil->structural FAIL, dot_gate:118), agentSpawnSem (:4621 remote&&non-nil), runner (:4085), workerRegistry (:3398,:3562 local-only + outer :3001), mergeMu (5 in beadRunOne). **Asymmetry to exploit:** the three headline controllers (handlerPause/operatorPause/decisionBlocker) are **entirely dispatch-side** — never enter beadRunOne, so M3 run ports don't decide their optionality (that's M5/dispatch). Inside the run path the keeper answer fits: config fallbacks (launchSpecBuilder/worktreeFactory/harnessRegistry -> fold default into the production port constructor) or feature toggles (hookStore, agentSpawnSem, cpRegistry -> keeper-style nil-port-or-no-op-adapter, decided once at wiring).

## By-value copy semantics

- beadRunOne (:3072), runWorkLoop (:1508), and all run helpers take `deps workLoopDeps` **by value**. Pointer exceptions: closeBeadWithHistoryTrim (`*workLoopDeps` method, read-only), diskcheck family + pruneHeldDedupOnEpochChange (`*workLoopDeps`, receiving `&deps` of **runWorkLoop's copy** :1738).
- Shallow-shared reference fields in every copy: 5 mutex **pointers** (followUpLedgerMu/mergeMu/worktreeCreateMu/emittedEpicsMu `*sync.Mutex`; cacheReapMu `*sync.RWMutex`), 3 maps, 3 channels, localInFlight `*atomic.Int32`, tidGen, runRegistry, workerRegistry, interface values, slices (aliased backing, never mutated post-construction).
- **No mutex-by-value bug today** — every mutex is a pointer, copies share the lock. Mutable value fields (lastDiskCheck/lastGoCacheClean/diskLow/lastCoordinatorReap, heldEventDedup-map-pointer) are mutated only through `&deps` of runWorkLoop's own copy and read only in that goroutine — **MAINT is goroutine-local by construction of the copy**, the strongest evidence for keeping MAINT out of run ports.
- Latent hazard: because dispatch goroutines each hold a frozen copy, any future value-field mutation meant to be seen by in-flight runs silently won't be. Port extraction removes this trap by making sharing explicit.

## Risks / conflicts

1. **Dossier correction — staleBlockerCloser** is claim-failure/dispatch-side only (runWorkLoop:2895), not run LedgerPort.
2. **Dossier correction — fetchBaseOnWorker** is not a workLoopDeps field (already parameterised). WorktreePort as sketched conflates create with remote code-sync; census points to a separate Worker/Remote grouping.
3. **queueStore leaks into the run path once** (:3947 review-loop-failure budget, NQ-B1). Narrow QueuePort with that single mutation, or (cleaner for C4) surface it as a terminal-event field the queue side applies. Same for evaluateGroupAdvance + stagedBeadGeneratorEval (dispatch goroutine after beadRunOne) — the cut line "reactor emits terminal event; queue/flywheel consume" keeps cancelOnQueue*, eagerfill, staged generator out of M3 ports.
4. **tidGen straddles the cut**: claim path (:2774) + run path share one generator (EM-018a strict monotonicity) — one shared instance whatever port owns TID minting.
5. **substrate straddles too**: run-path Launch + outer exitClean windowCleaner probe (:104-110,:1634) + session adoption — LaunchPort owns it for runs, shell keeps its own reference.
6. **handlerEnv is read by the schedule subsystem** (fireCommandAction, scheduletick.go:252) — value copy is fine, note double ownership when LaunchPort absorbs it.
7. **Two dead fields** (`h`, `beadAuditLogger`) — delete in M3, shrinks census to 83, removes false port candidates.
8. Line-range holds: struct still exactly :182-942, 85 fields.

## PLANNER-RECONCILE

- **The dossier's six candidate ports are incomplete: the census needs at least eight groupings.** Add WorkerPort/RemotePort (workerRegistry, runner, codesync — dossier missed it entirely), a GatePort or dot-cascade-shell home for cpRegistry (the one RUN field fitting none of the six), and a possible LaunchPort/AgentWaitPort split. Also two dossier fields are mis-assigned (staleBlockerCloser -> dispatch-side, fetchBaseOnWorker -> not a field). Design proceeds with the eight-grouping map above; flagged because it revises the decompose's port list, and the exact LaunchPort-vs-AgentWaitPort split is a genuine design call the planner may wish to weigh in on (kept as ONE LaunchPort in the design unless told otherwise).
