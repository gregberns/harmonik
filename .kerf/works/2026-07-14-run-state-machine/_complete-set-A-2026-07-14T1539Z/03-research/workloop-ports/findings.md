# 03-Research / workloop-ports — C3 (+C1) findings (workLoopDeps census)

> Pass 3 (Research), components C3 (ports) and C1 (ClockPort). Verified against the
> working tree 2026-07-14. Struct decl `internal/daemon/workloop.go:182–:942`
> (**exactly 85 fields**); ctor `newWorkLoopDeps` `:1043–:1177`; `beadRunOne`
> `:3072–:5447` takes `deps workLoopDeps` **by value**. Run-lifecycle helpers that
> also take deps by value: `reviewloop.go:200`, `dot_cascade.go:183/:1212`,
> `dot_gate.go` (gate eval), `autoCloseStaleBlockersOnClaimFailure` `:5565`,
> `productionWorktreeFactory` `:5738`. The idiom to mirror:
> `internal/keeper/ports.go` (6 named structural interfaces; `EmitterPort = Emitter`
> type alias at `ports.go:107` — zero-adaptation subsetting, directly reusable).

## 1. The cut line — RUN vs OUTER

Classified every field by read sites: **RUN** (inside beadRunOne/reviewloop/
dot_cascade/dot_gate), **OUTER** (only the poll/maintenance goroutine:
runWorkLoop, eagerfill, scheduletick, diskcheck), **BOTH**, **DEAD**.

**RUN set = 36 fields (~42%):** brAdapter, bus, intentLogDir, projectDir,
allowedRepos, brPath, handlerBinary, daemonBinaryPath, handlerArgs, handlerEnv,
brTimeoutCfg, tidGen, workflowModeDefault, runRegistry, localInFlight, hookStore,
launchSpecBuilder, worktreeFactory, mergeMu, worktreeCreateMu, agentSpawnSem,
cpRegistry, adapterRegistry, harnessRegistry, substrate, agentReadyTimeout,
remoteAgentReadyTimeout, postAgentReadyHangTimeout, projectCfg, defaultHarness,
queueStore, targetBranch, protectBranches, workerRegistry, runner, sandboxCfg.

**DEAD (2):** `h` (`:191`) — zero `deps.h` reads anywhere (set only at ctor
`:1064/:1125`); `beadAuditLogger` (`:753`) — zero reads (pre-dispatch block removed
by hk-f38n; set `:1170`). Candidate deletions, carry into design.

**OUTER-only (47):** everything else — kerfPath, followUpLedger(+Mu,+Path),
maxConcurrent, concurrencyCtrl, emittedEpics(+Mu), spawnSubstrateReadyCh,
submitWakeC, queueLedger, cancelOnQueueDrain/Exit, stopDispatchCtx,
handlerPauseController, heldEventDedup, staleBlockerCloser,
strandedInProgressResetter(+hash,+NS), operatorPauseCtrl, decisionBlocker,
noAutoPull, skipBrHistoryRotation*, scheduleStore, crewHandler, commsWhoQuerier,
commsSend, scheduleWakeC, coordinatorReap*(4), lastDiskCheck, lastGoCacheClean,
diskLow, disk*/goCache* overrides+funcs (6), cacheReapMu**, worktreeReclaimFunc,
governorState/Cfg, sentinelMode, sentinelPhase2Classes.
(* `skipBrHistoryRotation` is read at `:969` inside `closeBeadWithHistoryTrim`, a
`*workLoopDeps` method invoked FROM the run path — it rides LedgerPort with the
close method. ** `cacheReapMu` read-lock is taken in the outer Register path
`:2970–:2993` on behalf of dispatch; the run's Register call is the reader, so the
handle stays shared.)

Notable RUN-set caveats:
- `staleBlockerCloser` (`:649`) sounds ledger-ish but is OUTER-only (claim-failure
  path `:5566`) — keep OFF the runexec LedgerPort.
- `cpRegistry` (`:452`) is read only in `dot_gate.go:118/:123` — the one RUN field
  not fitting the seven proposed groupings; needs a GatePort (DOT-specific).
- `allowedRepos` (`:3276`), `workflowModeDefault` (`:3168`), `projectCfg`
  (`:3204/:3218/:3245`) are pure config values → a RunConfig value-bag, not ports.

## 2. Port-grouping sanity check (RUN fields)

| Port | Fields | Notes |
|---|---|---|
| **LedgerPort** | brAdapter, brTimeoutCfg, intentLogDir, tidGen, closeBeadWithHistoryTrim(+skipBrHistoryRotation) | co-travelling call cluster on every br write; close/reopen = brAdapter methods + the trim method |
| **EmitterPort** | bus | alias trick: `= handlercontract.EventEmitter` subset (keeper `ports.go:107` precedent) |
| **WorktreePort** | worktreeFactory, worktreeCreateMu, workerRegistry (base-sync path `:3391–:3581`) | projectDir is shared config, not a port method |
| **MergePort** | mergeMu, targetBranch, protectBranches, brPath | the merge invocation cluster (`:3636–:3667`, `:5169–:5174`, 5 merge calls) — becomes C2's queue handle |
| **LaunchPort** | launchSpecBuilder, substrate, harnessRegistry, adapterRegistry, defaultHarness, handlerBinary, daemonBinaryPath, handlerArgs, handlerEnv, hookStore, agentReadyTimeout, remoteAgentReadyTimeout, postAgentReadyHangTimeout, sandboxCfg, projectCfg | largest cohesive cluster; the 3 timeouts gate the launch→ready handshake |
| **ClockPort** | (no field today — see §4) | |
| **Shared handles** | runRegistry, localInFlight, agentSpawnSem, cacheReapMu | stay shared-by-reference |
| **GatePort** (new) | cpRegistry | DOT gate-node eval only |
| **RunConfig** (values) | allowedRepos, workflowModeDefault, projectDir, queueStore(budget), runner | runner overlaps WorktreePort/remote — design call |

## 3. Shared-mutable vs loop-owned value fields

**Must stay shared-by-reference:** runRegistry (`:288`), localInFlight (`:329`,
godoc `:326` "Pointer so all goroutines share the same atomic"), agentSpawnSem
(`:430`, cap 3), mergeMu (`:384`), worktreeCreateMu (`:407`), cacheReapMu (`:894`),
followUpLedger+Mu, emittedEpics+Mu, heldEventDedup, queueStore, concurrencyCtrl,
wake chans, cancel funcs, stopDispatchCtx, the controllers, scheduleStore,
workerRegistry, hookStore.

**Work-loop-goroutine-owned VALUE fields (confirming 02-components Q(b)):**
`lastCoordinatorReap` (`:826`), `lastDiskCheck` (`:834`), `lastGoCacheClean`
(`:841`), `diskLow` (`:849`) — each godoc'd "guarded entirely by the work-loop
goroutine". Because deps is copied by value at `:3072` these are already copied
per-run; never written in the run path (harmless today) — they MUST stay out of the
run ports and ideally move to loop-owned state at extraction.

## 4. `time.*` census (C1 ClockPort scope)

**RUN-path sites (ClockPort/timer-event candidates):**
- workloop.go: `:3109` Now, `:3134` Now, `:4408` Now, `:4637` Now, `:4647` Since,
  `:4654` Since, **`:4871` After**, `:5063` Since.
- reviewloop.go: `:551` Now, `:562` Since, `:568` Since, **`:659` After**,
  **`:738` After**, `:862` Since, **`:1478` After**, `:1907` Now.
- dot_cascade.go: `:235` Now, `:1571` Now, `:1588` Since, `:1591` Since,
  **`:1716` After**, `:1804` Since, `:2514` Now.

The run path uses NO `time.NewTimer/NewTicker/Sleep` — only Now/Since/After. The
five `time.After` sites are select-timeout deadlines = the strongest
timer-as-event candidates (D11 shape); Now/Since map to plain `ClockPort` reads.

**OUTER-only sites (NOT in C1's scope):** workloop.go `:1153` (ctor), `:1628`,
`:1717/:1719`, `:1777–:1779`, `:1842–:1844`, `:1893–:1895`, `:2242`, `:2342`,
`:2398`, `:2988`, `:5658`, `:5703`, `:5989`, `:6016`, `:6042`, `:6142`, `:6220`,
`:6304`, `:6326`, `:7404`, `:7671`, `:8032`, `:8080` (NewTicker). Note the earlier
"26 time.Now() in workloop.go" claim counts the whole file; the RUN-path subset
above is what C1 must migrate for C5's determinism (the OUTER sites are M5).

## 5. Nil-means-disabled — every run-path nil-check

launchSpecBuilder (nil→buildClaudeLaunchSpec: `:3797`, `dot_cascade.go:1304`);
worktreeFactory (nil→productionWorktreeFactory, `:3592` region); workerRegistry
(`:3398`, `:3562`); mergeMu (nil→no lock: `:3636/:3650/:3666/:5169/:5173`);
agentSpawnSem (`:4621`, remote && non-nil); runner (`:4085`); hookStore
(reviewloop `:397,:554,:623,:748,:867,:1349,:1374,:1413,:1488,:1541`; dot_cascade
`:1539,:1574,:1641,:1720,:1807`); harnessRegistry (`:3798,:4425,:4814,:5085`;
reviewloop `:377,:683,:1304`; dot_cascade `:1398,:1460,:1686,:1772,:1880`);
cpRegistry (nil→gate eval-failure Outcome, dot_gate godoc `:444`).
Outer-loop nil-checked deps (confirming they stay OUT of runexec):
handlerPauseController `:2261/:2696`, operatorPauseCtrl `:1859/:1911/:2645`,
decisionBlocker (7 sites), concurrencyCtrl `:1758`, spawnSubstrateReadyCh `:1651`,
coordinatorReapAdapter `:1717`, governorState `:1837/:1887`,
strandedInProgressResetter `:2364`, staleBlockerCloser `:5566`, crewHandler
`:1979`, commsWhoQuerier `:1981`, scheduleStore `:5697`, queueStore (10 outer +
RUN `:3947`).

## 6. `newWorkLoopDeps` (`:1043–:1177`)

Fail-closed preconditions `:1044–:1052` (BrPath, ProjectDir, registry non-nil).
Derived: brAdapter `:1057`, intentLogDir `:1062`, h `:1064`, handlerBinary default
"claude" `:1066–:1069`, daemonBinaryPath default "harmonik" `:1074–:1077`,
handlerEnv provenance-prefixed `:1094–:1097`, harnessRegistry `:1103`,
workerRegistry `:1111`, coordinatorReapAdapter from substrate `:1117–:1120`.
Ctor-allocated shared state: tidGen `:1133`, runRegistry `:1135`, localInFlight
`:1137`, followUpLedger(+Mu) `:1156/:1157`, mergeMu/worktreeCreateMu
`:1161/:1162`, agentSpawnSem cap-3 `:1163`, cacheReapMu `:1164`, emittedEpics(+Mu)
`:1165/:1166`; strandedResetDaemonNS = `time.Now().UnixNano()` `:1153`.
Populated LATER by daemon.Start (nil/absent in ctor literal): queueStore (`:1148`
comment), concurrencyCtrl, submitWakeC, scheduleWakeC, spawnSubstrateReadyCh,
stopDispatchCtx, cancelOnQueueExit, pause/decision controllers, scheduleStore,
crewHandler, comms*, coordinatorReapInterval, governor*, sentinel*,
postAgentReadyHangTimeout, heldEventDedup, disk-check fields, worktreeFactory,
launchSpecBuilder.

## 7. Design flags carried to Change Design

1. `h` + `beadAuditLogger` DEAD — delete, don't port.
2. `staleBlockerCloser` off the run LedgerPort (outer claim path).
3. `cpRegistry` → its own GatePort (or ride LaunchPort? design call — it is
   gate-eval, not launch; recommend GatePort or fold into the dot-cascade shell).
4. The 4 loop-owned value fields lift OUT of the by-value bundle at extraction.
5. The by-value copy at `:3072` already implies "every RUN field is immutable
   config or a shared handle" — the port decomposition preserves this exactly
   (values → config bags; shared state → reference-typed ports).
6. `queueStore` appears in the RUN set ONLY for the review-loop-failure budget
   (`:3947–:3978` under LockForMutation) — a candidate narrow one-method port
   (BudgetPort) rather than exposing the whole store.
