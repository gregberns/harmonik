# Step 10 — the composition root: the measured map, and the design it produces

Measured 2026-08-01 against `9bb93ae9ea70feabdb894d0dea464db459575273`. Every number below carries
the command or the method that produced it. Two other lanes were editing `internal/daemon` while this
was measured, so the line numbers will drift. The findings will not — they are about which function
owns a field and which side of a copy a write lands on, and that does not move by accident.

**Symbols, not line numbers.** Where this document points at code it names the file and the enclosing
function. Grep for the symbol.

**Three claims in `DECOMPOSITION-MAP.md` §Step 10 do not survive re-measurement, and one of them
changes the shape of the step.** They are marked ⚠ where they sit. The largest is §4 below: the run
path no longer takes the bundle at all, so this step is a dispatch-loop step, not a run-path step.

---

## 1. The re-measurement

### 1.1 Method

Two of the four figures below were produced by a `go/ast` pass. The source of that pass is recorded
here so a reader can rebuild it rather than trust it.

- Parse the target file with `go/parser`.
- Walk to the `*ast.TypeSpec` whose `Name.Name` is the struct name.
- Field count = the sum of `len(Field.Names)` over `StructType.Fields.List`, plus a separate count of
  entries with zero names (embedded fields).
- Declaration span = `fset.Position(ts.End()).Line - fset.Position(ts.Pos()).Line + 1`.
- File length = `fset.File(f.Pos()).LineCount()`.

The write-site figure was produced by the same AST pass over every non-`_test.go` file in
`internal/daemon` at depth 1, binding every identifier declared as `workLoopDeps` or `*workLoopDeps`
and then marking each `SelectorExpr` on that identifier as a write when it appears on the left side of
an `AssignStmt`. **The AST pass and the map's grep disagree, and the AST pass is right.** See §1.3.

`internal/daemon/scenariotest` is a sub-package, so a depth-1 walk excludes it. It holds no
`workLoopDeps` reference, so the decision does not change any number here. State it anyway, because
the same fixture is the sole difference between two of Step 11's counts.

### 1.2 The figures

| Measure | Value at `9bb93ae9e` | Map's value at `95dff0bf5` | Verdict |
|---|---:|---:|---|
| `workLoopDeps` fields | **81** | 81 | reproduces |
| `workLoopDeps` embedded fields | **0** | (not stated) | — |
| Declaration span | **770 lines** | 770 | reproduces |
| `internal/daemon/workloop.go` total | **3,431 lines** | 3,366 | grew by 65 in one day |
| `bootState` fields | **22** | 22 | reproduces |
| Post-construction writes, `bootworkloop.go` | **26** | 25 | ⚠ **map undercounts by one** |
| Post-construction writes, `scheduler.go` | **3** | 3 | reproduces |

### 1.3 ⚠ The map's write-site grep undercounts by one, and the cause is a character class

The map's stated method is:

```
grep -rnE '^\s*deps\.[a-zA-Z]+ *= ' internal/daemon/*.go   # with _test.go removed
```

That returns 25 for `bootworkloop.go`. The AST pass returns 26. The missing site is
`deps.sentinelPhase2Classes = sentinelCfg.Phase2Classes()` in `internal/daemon/bootworkloop.go`
`seedGovernorDeps`. **The field name contains a digit and `[a-zA-Z]+` does not match a digit**, so the
pattern stops at `sentinelPhase` and then fails to find ` = `. Reproduce the gap with:

```
grep -cE '^[[:space:]]*deps\.[a-zA-Z]+ *= '    internal/daemon/bootworkloop.go   # 25
grep -cE '^[[:space:]]*deps\.[a-zA-Z0-9]+ *= ' internal/daemon/bootworkloop.go   # 26
```

**So the honest total is 29 post-construction writes, not 28.** Do not repeat the 25/3 split.

### 1.4 ⚠ The 770-line span is 83 lines of code

This is the number that most changes what the step costs, and no prior pass measured it.

Method, and it deliberately names no line number, because this document says twice that line numbers
drift. Take the `TypeSpec` start and end lines from the §1.1 pass, slice `internal/daemon/workloop.go`
between them, and classify each line by its first non-space token — empty, `//`, or anything else.

```
workLoopDeps declaration:  total=770  code=83  comment=608  blank=79
```

The declaration is **79% doc comment**. "More than a fifth of the file is one type" is true by line
count and misleading as a cost estimate. The mechanical part of moving the type is 83 lines of field
declarations. The 608 comment lines are the reason the step is worth doing — they are 608 lines of
prose explaining which boot phase may write a field and which reader must nil-check it, which is the
documentation a type should have made unnecessary. They travel with the fields into whichever group
takes them. **Budget the step by the number of consumer seams, not by the 770.**

### 1.5 The precondition table, re-run

Method: for each commit, `git show <commit>:internal/daemon/workloop.go` into a temp file, then the
same `go/ast` pass.

| Date | Commit | Fields | Span | File lines |
|---|---|---:|---:|---:|
| 2026-07-19 | `37651569f` | 82 | 748 | 8,207 |
| 2026-07-24 | `a479ddff7` | 81 | 743 | 6,650 |
| 2026-07-27 | `92d81fd60` | 81 | 743 | 6,656 |
| 2026-07-28 | `8ccc7a03e` | 81 | 743 | 6,281 |
| 2026-07-29 | `b3d6e8cc3` | 80 | 735 | 3,380 |
| 2026-07-30 | `89782676d` | 81 | 744 | 3,227 |
| 2026-07-31 | `8a84ad4ab` | 81 | 770 | 3,347 |
| 2026-08-01 | `95dff0bf5` | 81 | 770 | 3,366 |
| 2026-08-01 | `9bb93ae9e` (HEAD) | **81** | **770** | **3,431** |

**Every row reproduces.** The map's table is correct as printed.

**The map's conclusion is correct and its supporting sentence is not.** "The completed steps moved
zero fields off the bundle" is stronger than a flat count can show, because a count cannot see a swap.
Diff the field-NAME sets and the churn appears:

| Window | Fields removed | Fields added |
|---|---|---|
| `37651569f` → `a479ddff7` | `codexRequireIsolationBoundary`, `goCacheCleanIntervalOverride` | `codexNoWorkDurationFloor` |
| `a479ddff7` → `9bb93ae9e` | `postAgentReadyHangTimeout` | `queueWriteErrorReported` |

So four names changed, not one. And **the direction of one change is the wrong way**:
`queueWriteErrorReported` was **added** to the bundle on 2026-07-30 at `b029f9ce1`, after the program
began. The bundle is still accreting. That strengthens the map's finding rather than weakening it.

**One real lift did happen, and it is outside the measured window.** `8b9cb2bb1`, 2026-07-14,
"RT4 drop dead deps fields + lift maint state (RSM-011)" moved `lastCoordinatorReap`, `lastDiskCheck`
and `diskLow` off `workLoopDeps` into a loop-owned struct. Step 2 (`054b7bd92`, 2026-07-29) then moved
that struct into `internal/daemon/loopmaintenance.go`. Step 2's own commit touches `workloop.go` zero
times — confirm with `git show 054b7bd92 --stat -- internal/daemon/workloop.go`, which prints nothing.
So the lift precedent exists, it works, and it predates the program.

**Verdict on the precondition.** "Wait until steps 2–6 have made most of its fields locally owned" is
not met and is not on a path to being met. **This step starts on its own terms.** Its real
precondition is §4 below, and that one is already satisfied.

---

## 2. What the bundle is actually for

Before the inventory, the shape. Five facts decide the design.

**2.1 `newWorkLoopDeps` sets 52 of the 81 fields in one composite literal.** Method:
`sed -n '/^	return workLoopDeps{$/,/^	}, nil$/p' internal/daemon/workloop.go` then count the
`field:` keys. The function is 152 lines in `internal/daemon/workloop.go`.

**2.2 Eight fields are never assigned in production.** They are not in the literal and no production
site writes them:

`codexNoWorkDurationFloor`, `coordinatorReapInterval`, `diskCheckIntervalOverride`,
`diskFreeBytesFunc`, `diskLowWatermark`, `goCacheCleanFunc`, `launchSpecBuilder`,
`worktreeReclaimFunc`.

Six are honest test seams — a `_test.go` file sets them and the read site folds the zero to a
production default. `internal/daemon/workloop.go` says so in the field comment for `diskLowWatermark`:
*"Production leaves this zero."*

**Two are set by nothing at all, not even a test.** `codexNoWorkDurationFloor` and `diskLowWatermark`
have zero assignments across the whole tree. Check with
`grep -rn 'codexNoWorkDurationFloor' internal cmd --include='*.go'`: no hit is an assignment to the
field. The field is declared, copied into `RunEnv` by `runEnv`, and read through `env` on both the
single and the graph path, and its value is always the zero. `diskLowWatermark`'s tests drive the
package constant `diskLowWatermarkDefault`, never the field. These two are dead weight on the bundle.

**2.3 Eight fields are written more than once in production, and two of the repeats are dead stores.**

Method: union the 52 literal keys with the 29 post-construction write sites, then keep every field
whose total write count is above one. **Do not scope this to "set in the literal AND set again later".
That rule finds six and misses two**, because two of the eight are never in the literal at all.

| Field | In the literal? | Post-construction writes | Verdict |
|---|---|---:|---|
| `cancelOnQueueDrain` | yes | 1 | **dead store** |
| `handlerPauseController` | no | 2 | **dead store** |
| `emittedEpics` | yes | 1 | real two-phase |
| `emittedEpicsMu` | yes | 1 | real two-phase |
| `followUpLedger` | yes | 1 | real two-phase |
| `queueStore` | yes | 1 | real two-phase |
| `runRegistry` | yes | 1 | real two-phase |
| `mergeQ` | no | 2 | real two-phase |

- `cancelOnQueueDrain` is set from `cfg.CancelOnQueueDrain` in the literal and set again from
  `cfg.CancelOnQueueDrain` in `injectWorkLoopDeps`. **Same source, same value. The second write is
  dead.**
- `handlerPauseController` is written twice inside `injectWorkLoopDeps` — first from
  `cfg.HandlerPauseController`, then unconditionally from `bs.handlerPauseCtrl`. The comment above the
  second write admits it: *"overrides the cfg-supplied value above"*. **The first write is dead.**
- `mergeQ` is written in `injectWorkLoopDeps` only under `if bs.hooks.mergeQ != nil` (the
  `WithMergeQueue` test override) and in `runWorkLoop` only under `if deps.mergeQ == nil`. **The two
  writes never both fire, but not "by construction"** — the exclusion holds at runtime, because the
  second site nil-checks a value that reached it through the by-value copy of §2.4. Nothing in a type
  enforces it.
  **And it is not clean. `mergeQ` is one of §5.2's three divergent fields.** In production no test hook
  is set, so `injectWorkLoopDeps` writes nothing and `runWorkLoop` creates the queue **on its own
  copy**. `launchWorkLoop`'s local — the one the reap closure holds a pointer to — keeps a nil `mergeQ`
  for the daemon's whole life. Read this bullet with §5.2, not instead of it.
  > The field's own doc comment in `internal/daemon/workloop.go` says the constructor leaves it
  > non-nil in production. That is false, and the literal's inline comment fifteen lines away says the
  > opposite and is right. Filed as `hk-fxmhl`. **Not this step's work** — recorded so a reader who
  > checks the comment against this section does not conclude the section is wrong.

**The other six are real two-phase writes:** an earlier phase installs a default or a nil placeholder
and a later phase installs the boot-seeded or daemon-owned value.

**2.4 The bundle is passed BY VALUE into the loop and BY POINTER into the assembly.** `runWorkLoop` in
`internal/daemon/scheduler.go` has the signature `runWorkLoop(ctx context.Context, deps workLoopDeps)`.
`buildWorkLoopDeps` returns a `workLoopDeps` value. `injectWorkLoopDeps`, `startBackgroundLoops`,
`seedGovernorDeps` and `wireStaleWatcherReapSeams` take `*workLoopDeps`.

So there are **two live copies of the bundle** once the loop starts. `launchWorkLoop`'s local is one.
`runWorkLoop`'s parameter is the other. Reference fields (maps, pointers, channels, interfaces) are
shared. Value fields are not. `internal/daemon/loopmaintenance.go` already names this hazard in its own
doc comment: *"that bundle is copied by value into every run goroutine, where a mutation of a value
field is a silent no-op"*. §5.2 shows where the two copies diverge today.

**2.5 The bundle is reached from 59 distinct functions across 12 files.** This count is sensitive to
its rule, so here is the rule. Take every `SelectorExpr` on an identifier bound to `workLoopDeps` or
`*workLoopDeps`, keep only those whose selector name is one of the 81 fields, then count the unique
`file/function` pairs. That gives **366 field accesses — 29 writes and 337 reads — from 59 functions in
12 files**.

**Counting the bundle's METHOD selectors too gives 376 accesses from 60 functions.** The bundle
declares **eleven** methods — `runEnv`, `runPorts`, `sharedHandles`, `buildRunBundles`,
`closeBeadWithHistoryTrim`, `ledgerPort`, `emitterPort`, `mergePort`, `gatePort`, `budgetPort`,
`clockOrSystem` — and **ten of them contribute one qualifying occurrence each**. The eleventh,
`closeBeadWithHistoryTrim`, contributes zero: its only call is `l.deps.closeBeadWithHistoryTrim(...)`
in `daemonLedger.CloseBead`, a selector on a **selector** rather than on a bound identifier, which the
rule above excludes. 366 + 10 = 376.

**A third rule gives 373.** Extend rule A one hop through struct fields that hold a `*workLoopDeps` —
that is, count `l.deps.<field>` and `b.deps.<field>` inside `daemonLedger` and `daemonBudget` — and
seven more accesses appear, all in `internal/daemon/runports.go`. Those seven are real retentions and
§5.1 says why an unextended rule cannot see them.

**Nothing in the design turns on any of these three figures** — they are here to say the bundle is
reached from everywhere, and all three rules agree on that. Do not quote one without its rule.

---

## 3. The field inventory, grouped into proposed owners

### 3.1 The grouping rule

**Group by the consumer that reads the field, not by what the field is.** That is `PRINCIPLES.md` §4
applied literally: a package declares the smallest interface it needs. The data supports it. Crosstab
each of the 81 fields against the files that read it and the fields fall into clusters with almost no
overlap:

```
22 fields  read only by internal/daemon/runports.go   (the run-shell bundle constructors)
12 fields  read only by internal/daemon/scheduler.go  (the dispatch loop)
 5 fields  read only by internal/daemon/diskcheck_hksxlb.go
 4 fields  read only by internal/daemon/movementgovernor.go
 3 fields  read only by internal/daemon/eagerfill_em063.go
 3 fields  read only by internal/daemon/loopmaintenance.go
 2 fields  read only by internal/daemon/workloop.go
 1 field   read only by internal/daemon/scheduletick.go   (commsSend)
```

**Eight buckets, 52 fields.** The remaining 29 are read by two to four files each, except the two in
§3.6.

**Exactly two fields are genuinely cross-cutting.** `projectDir` is read in 10 files and `bus` in 8.
Every other field is read by at most four, and the four are almost always one cluster plus the run
shell. That is a clean partition, not a tangle. It is the single most important input to the design and
no prior pass recorded it.

### 3.2 Three owners at the top, eleven ports beneath them

| Owner | Ports | Fields | Exists today? |
|---|---|---:|---|
| **A. The run shell** | `runloop.RunEnv`, `runloop.RunPorts`, `runloop.SharedHandles` | 41 | **yes — declared and depguard-fenced** |
| **B. The dispatch loop** | Capacity, QueueSurface, DispatchGates, LoopLifecycle, LedgerRepair | 18 | no |
| **C. The cadenced maintenance** | SchedulePort, CoordinatorReapPort, DiskReclaimPort, EagerRefillPort, GovernorPort | 22 | partly — `loopMaintenance` is the holder |

41 + 18 + 22 = 81.

### 3.3 Owner A — the run shell. 41 fields, and the port surface already exists

`internal/runloop/ports.go` declares `RunEnv` (30 fields), `RunPorts` (8) and `SharedHandles` (17),
plus ten narrow port interfaces (`LedgerPort`, `MergePort`, `GatePort`, `WorktreePort`, `LaunchPort`,
`BudgetPort`, `RunHandlePort`, `RunRegistryPort`, `BeadLedger`, `HookStore`). `.golangci.yml` fences
`internal/runloop` against importing `internal/daemon`, and the fence comment states the direction as
the unit's success criterion. `internal/daemon/runports.go` holds the daemon-side adapters.

**This is the exemplar `PRINCIPLES.md` §9 asks for. Do not invent a second answer to the same
question.** The rest of this design is "make the loop look like this".

The 41 fields Owner A owns, and how it reaches them:

| Port | Fields |
|---|---|
| `RunEnv` (18 read off deps) | `projectDir` `targetBranch` `brPath` `protectBranches` `allowedRepos` `workflowModeDefault` `defaultHarness` `projectCfg` `handlerBinary` `handlerArgs` `handlerEnv` `daemonBinaryPath` `intentLogDir` `agentReadyTimeout` `remoteAgentReadyTimeout` `codexNoWorkDurationFloor` `sandboxCfg` `brTimeoutCfg` |
| `RunPorts` (5 new) | `brAdapter` `bus` `mergeQ` `cpRegistry` `clock` |
| `SharedHandles` (16 new) | `runRegistry` `localInFlight` `agentSpawnSem` `workerRegistry` `queueStore` `harnessRegistry` `adapterRegistry` `hookStore` `substrate` `reviewerSubstrate` `tidGen` `emittedEpics` `emittedEpicsMu` `runner` `worktreeFactory` `worktreeCreateMu` |
| Residue still reached off `*workLoopDeps` | `launchSpecBuilder` `skipBrHistoryRotation` |

18 + 5 + 16 + 2 = 41. The residue is the whole of what keeps the run shell tied to the bundle.
`launchSpecBuilder` is threaded per-run through `launchPort`, and `skipBrHistoryRotation` is read by
`closeBeadWithHistoryTrim`, which `daemonLedger` calls. `noAutoPull` looks like it belongs here and does
not — it is read once, in `runWorkLoop`, so it is a loop field mis-shelved with the run-shell block and
it goes to Owner B.

### 3.4 Owner B — the dispatch loop. 18 fields in five ports

| Port | Fields | Why it is one thing |
|---|---|---|
| **Capacity** | `maxConcurrent` `concurrencyCtrl` | The static ceiling and the live override. Read together at every gate. Also read by eager refill. |
| **QueueSurface** | `submitWakeC` `queueLedger` | The store itself lives in `SharedHandles` already. What is left is the wake channel the loop sleeps on and the ledger the deferred-item re-evaluation needs. |
| **DispatchGates** | `handlerPauseController` `operatorPauseCtrl` `decisionBlocker` `heldEventDedup` `queueWriteErrorReported` | The **three** gates consulted before a bead is claimed, plus the two at-most-once maps that stop the gates emitting the same event twice. |
| **LoopLifecycle** | `cancelOnQueueDrain` `cancelOnQueueExit` `stopDispatchCtx` `spawnSubstrateReadyCh` | How the loop is told to stop, and the one channel it waits on before its first tick. |
| **LedgerRepair** | `staleBlockerCloser` `strandedInProgressResetter` `strandedResetProjectHash` `strandedResetDaemonNS` | Two self-healing seams and the two values that form their idempotency key. Nothing else reads any of the four. |

The five ports hold 17. Plus `noAutoPull`, moved here from §3.3, that is 18.

### 3.5 Owner C — the cadenced maintenance. 22 fields in five ports

`internal/daemon/loopmaintenance.go` already owns the cadence arithmetic and the timing state. It does
not yet own the handles. These five ports are what it should hold.

| Port | Fields | Sole reader file |
|---|---|---|
| **SchedulePort** | `scheduleStore` `scheduleWakeC` `crewHandler` `commsWhoQuerier` `commsSend` | `scheduletick.go` (`crewHandler` and `commsWhoQuerier` also read by `movementgovernor.go`) |
| **CoordinatorReapPort** | `coordinatorReapAdapter` `coordinatorReapProjectHash` `coordinatorReapInterval` | `loopmaintenance.go` — all three, nothing else |
| **DiskReclaimPort** | `diskLowWatermark` `diskCheckIntervalOverride` `diskFreeBytesFunc` `goCacheCleanFunc` `worktreeReclaimFunc` `cacheReapMu` | `diskcheck_hksxlb.go` — five exclusively. `cacheReapMu` is also taken read-side in `runWorkLoop`, which is the point of it. |
| **EagerRefillPort** | `kerfPath` `followUpLedger` `followUpLedgerMu` `followUpLedgerPath` | `eagerfill_em063.go` — all four, nothing else |
| **GovernorPort** | `governorState` `governorCfg` `sentinelMode` `sentinelPhase2Classes` | `movementgovernor.go` — all four, nothing else |

**Three of the five ports have a single reader file and no other consumer at all.** Those three
(`CoordinatorReapPort`, `EagerRefillPort`, `GovernorPort`, 11 fields between them) are the cheapest
work in the whole step and the safest place to start.

### 3.6 The two fields that do not fit a group, and why they are not forced into one

**`projectDir`** — 46 reads across 10 files. **`bus`** — 16 reads across 8 files.

Every one of the eleven ports needs one or both. There is no consumer that owns them. Forcing them
into a group would give ten consumers a dependency on a group they otherwise do not need, which is the
defect this step exists to remove, restated.

**Treat them as ambient values passed to each port's constructor.** Each port declares them as its own
field. `RunEnv` already does exactly this — it copies `ProjectDir` rather than reaching for a shared
one. The cost is eleven copies of a string and eleven copies of an interface handle. The benefit is
that no port imports another to reach them.

**Do not make either an interface with a getter and do not put them in a context.** Both moves hide the
dependency from the compiler, which is the opposite direction of travel.

### 3.7 The two fields that should be deleted rather than grouped

`codexNoWorkDurationFloor` and `diskLowWatermark` have zero assignments in the whole tree (§2.2). Their
read sites already fold zero to a package constant — `codex.NoWorkFloor` in
`internal/harness/codex/nowork.go` and `diskLowWatermarkDefault` in `internal/daemon/workloop.go`.
Deleting them removes two fields and changes no behavior. **This is not a licence to delete the other
six test seams**, which real tests set.

---

## 4. The ordering constraints, and which are real

### 4.1 ⚠ The framing correction: the run path no longer takes the bundle

`DECOMPOSITION-MAP.md` §Step 10 says *"an 81-field bundle threaded through every run"*. **It is not
threaded through a run.** Check the signature:

```
func beadRunOne(ctx context.Context, env runloop.RunEnv, rp runloop.RunPorts,
                handles runloop.SharedHandles, extraContext string,
                preSelectedWorker *workers.Worker, localSlotHeld bool) (succeeded bool)
```

`beadRunOne` is 1,769 lines and takes three declared bundles and no `workLoopDeps`.
`internal/daemon/dot_cascade_core.go` (1,736 lines, `dispatchDotAgenticNode` 617) references
`workLoopDeps` **zero times**. In the whole of `internal/daemon/workloop.go` only **three functions**
name the bundle: `newWorkLoopDeps`, which builds it, and `closeBeadWithHistoryTrim` and
`buildRunBundles`, which read fields off it. **Two of those three read fields; the third is the
constructor.** Do not write "two functions" — §2.1 already counts `newWorkLoopDeps` as the site that
sets 52 of the 81, and the two statements have to agree.

So the 81-field bundle is threaded through the **dispatch loop**, not the run. `runWorkLoop` in
`internal/daemon/scheduler.go` is 1,280 lines and holds essentially all of it.

**This is good news and it should change how the step is briefed.** The run half of Step 10 is already
done. What is left is the loop half plus the assembly. Anyone planning this step against the map's
sentence will look for the work in the wrong file.

### 4.2 What the four-stage assembly actually encodes

`launchWorkLoop` in `internal/daemon/bootworkloop.go` runs five things in order:

1. `buildWorkLoopDeps` — calls `newWorkLoopDeps`, calls `seedGovernorDeps`, boot-seeds `emittedEpics`
   and `followUpLedger`. 32 lines.
2. `injectWorkLoopDeps` — 85 lines, 19 field writes (26 in the file = 3 + 4 + 19).
3. `startBackgroundLoops` — 43 lines, zero field writes.
4. `wireStaleWatcherReapSeams` — 28 lines, zero field writes.
5. `go func() { loopDone <- runWorkLoop(ctx, deps) }()`.

**The whole assembly reads exactly three fields back out of the bundle.** That is the measurement that
decides which ordering edges are real:

| Field read | Read by | Real dependency? |
|---|---|---|
| `followUpLedgerPath` | `buildWorkLoopDeps` | **No.** It is `filepath.Join(cfg.ProjectDir, ".harmonik", followUpLedgerFileName)`. A pure function of `cfg`. |
| `daemonBinaryPath` | `injectWorkLoopDeps` (3 reads) | **No.** It is `cfg.DaemonBinaryPath` or the literal `"harmonik"`. A pure function of `cfg`. |
| `workerRegistry` | `injectWorkLoopDeps`, `startBackgroundLoops` | **Yes.** It is `workers.BuildRegistry(ctx, cfg.Workers, workerEmit)`, which only `newWorkLoopDeps` calls and which runs a boot-time health check. |

**One real intra-bundle data dependency out of three. The rest of the four-stage ordering is
incidental — it is where somebody put the line.**

### 4.3 The ordering edges that ARE real

Five, and only the last is inside the bundle.

1. **`bus.Seal()` before any post-Seal construction.** EV-009 requires every `Subscribe` call to
   precede `Seal`. `startWithHooks` in `internal/daemon/daemon.go` seals between
   `wireWatchersAndObservers` and `emitStartupEvents`. This is a protocol constraint and it is the one
   hard barrier in the boot. Nothing in this step may move a `Subscribe` across it.

2. **`constructBusAndRegistries` before everything.** It writes `bus`, `qs`, `handlerPauseCtrl`,
   `sharedRunRegistry`, `pollGate`. Every later phase reads at least one.

3. **`wireSocketListener` before `launchWorkLoop`.** `injectWorkLoopDeps` reads `bs.concurrencyCtrl`,
   `bs.opPauseCtrl`, `bs.decisionBlocker`, `bs.crewHandler`, `bs.queueHandlerAdapter`.
   `startBackgroundLoops` reads `bs.crewIdleReaper` and `bs.branchReapWatcher`. All seven are written
   in `internal/daemon/bootsocket.go`.

4. **`wireWatchersAndObservers` before `injectWorkLoopDeps`.** `bs.quiesceArbiter` and
   `bs.staleWatcher` are written there and consumed in the assembly. See §5.

5. **`newWorkLoopDeps` before `injectWorkLoopDeps`, for `workerRegistry` only.** §4.2.

**Edges 1 to 4 are `bootState` edges, not `workLoopDeps` edges.** That is the design's central
opportunity: replacing the bundle does not require re-ordering the boot. It requires the assembly
functions to take arguments instead of reaching into a shared struct.

### 4.4 The ordering constraints that are incidental

- **`seedGovernorDeps` has no ordering edge at all.** It reads `bs.cfg` and writes four fields. It
  reads nothing off `deps`. It can be a constructor that returns a `GovernorPort` value. Confirm with
  the AST reader table: `bootworkloop.go` has no `deps.` read inside `seedGovernorDeps`.
- **The `emittedEpics` boot-seed has no edge.** It reads `cfg.JSONLLogPath` and writes two fields.
- **The `followUpLedger` boot-seed has a fake edge.** §4.2.
- **`startBackgroundLoops` writes nothing.** It is not an assembly stage. It is a start-the-goroutines
  stage that happens to have been put in the assembly file. Its only bundle read is `workerRegistry`.
- **`wireStaleWatcherReapSeams` writes nothing.** It is a wiring stage. Its dependency on the assembly
  is deferred into a closure. §5.

### 4.5 The one temporal invariant that is genuinely load-bearing

`runWorkLoop` lazily initialises three fields on **its own copy** before its `for` loop:

```
internal/daemon/scheduler.go  runWorkLoop
  deps.mergeQ                 = mergeq.New(nil)          // when nil
  deps.heldEventDedup         = make(map[string]struct{}) // when nil
  deps.queueWriteErrorReported = make(map[string]struct{}) // when nil
```

These are the only three writes anywhere outside `bootworkloop.go`. They exist because the boot cannot
decide the values — `mergeQ` must be owned and cancelled by the loop, and the two maps are
single-goroutine scratch state. **They are not a construction problem. They are three fields that
belong to the loop and were parked on the bundle.** Under §3.4 they land in `DispatchGates` and the
loop's own `mergeq.Queue` handle, and the nil checks go away.

---

## 5. The watchdog-closure hazard

`DECOMPOSITION-MAP.md` §4 warns that *"the bundle is captured by a background watchdog closure, so a
partial cleanup produces a bundle that is neither the old thing nor the new one"*. A previous pass
overrode that warning rather than answering it. This section answers it.

### 5.1 The complete capture list

Method: an AST pass over every non-`_test.go` file at depth 1 in `internal/daemon`, binding every
identifier of type `workLoopDeps` or `*workLoopDeps`, then reporting every `*ast.FuncLit` whose body
references a bound identifier. Repeated for `bootState`.

**Five closures capture `workLoopDeps`. Two capture `bootState`. That is the whole set.**

| # | File | Enclosing function | What the closure is | What it reads off the bundle | Escapes? |
|---|---|---|---|---|---|
| 1 | `bootworkloop.go` | `launchWorkLoop` | `go func() { loopDone <- runWorkLoop(ctx, deps) }()` | the whole value | goroutine, lives for the daemon |
| 2 | `bootworkloop.go` | `wireStaleWatcherReapSeams` | the `SetForceReap` callback handed to `StaleWatcher` | `*deps` dereferenced at reap time, passed whole to `evaluateGroupAdvanceWithOutcome` | **yes — stored on the watcher, fires from the watcher goroutine** |
| 3 | `scheduler.go` | `runWorkLoop` | `exitClean` | `runRegistry`, `substrate`, **and the whole value, passed to `drainCancelledQueue`** | called on the loop goroutine only |
| 4 | `scheduler.go` | `runWorkLoop` | the run-adoption `go func()` | the whole value, passed to `adoptLiveRunSession` | goroutine, bounded by `wg` |
| 5 | `scheduler.go` | `runWorkLoop` | the per-run dispatch `go func(...)` | `runEnv`, `buildRunBundles`, `queueStore`, `runRegistry` | **yes — N concurrent, one per in-flight bead** |
| 6 | `bootworkloop.go` | `wireStaleWatcherReapSeams` | the `SetRunProcessDead` callback | captures `bs` and `reapAdapter`, **not `deps`** | yes |

Rows 1 to 5 capture `workLoopDeps`. Rows 2 and 6 capture `bootState` — row 2 reads `bs.bus`, row 6
calls `bs.probeRunProcessDead`. **That is the whole set. There is no sixth `workLoopDeps` capture and no
third `bootState` capture.**

**⚠ This table answers "which closures capture the bundle". It does NOT answer "what holds the bundle
past its frame", and the two questions are not the same.** A struct field retains a pointer just as
effectively as a closure does, and an AST closure walk cannot see it. Two types do exactly that:
`daemonLedger` and `daemonBudget` in `internal/daemon/runports.go` each declare a `deps *workLoopDeps`
field, and both are handed out as `runloop.LedgerPort` / `runloop.BudgetPort` values that outlive the
call that made them. **Those two retentions are real and this table does not contain them.** They are
stage 8's work. Anyone extending this table must extend the method too, or it will keep reporting
"complete" while a field-held pointer walks past it.

Closures 1, 3 and 4 are contained: they run before or inside `runWorkLoop`'s own lifetime and see a
consistent snapshot. Closures 2 and 5 are the hazard.

### 5.2 Closure 2 — the StaleWatcher force-reap callback

```
internal/daemon/bootworkloop.go  wireStaleWatcherReapSeams
  bs.staleWatcher.SetForceReap(func(runID core.RunID, handle *RunHandle) {
          emitRunCompleted(ctx, bs.bus, ...)
          ...
          evaluateGroupAdvanceWithOutcome(ctx, *deps, ...)
  })
```

It captures `deps *workLoopDeps` — a pointer to `launchWorkLoop`'s local variable, which Go escapes to
the heap. It dereferences that pointer **at reap time**, taking a fresh copy then. `emitRunCompleted`
reads `bs.bus` from the same closure, so it captures `bootState` too.

**What it needs to be safe, stated precisely.** Method: take the union of the field reads in
`evaluateGroupAdvanceWithOutcome`, `eagerRefillEval`, `preScreenCandidates`, `buildInQueueSet` and
`emitStaleOpenBeadDetected`, which is the whole call tree the closure reaches. The AST pass gives
**eleven fields**:

```
bus  cancelOnQueueDrain  cancelOnQueueExit  concurrencyCtrl  kerfPath  maxConcurrent
projectDir  queueLedger  queueStore  runRegistry  targetBranch
```

**Eleven fields spanning six of the eleven proposed ports, plus both ambient values.** That is the
closure's true dependency set, and nothing declares it today. Note what it does NOT include: the
follow-up-ledger fields and `brPath` are read in `stagedBeadGeneratorEval`, which `runWorkLoop` calls
directly and the reap path never reaches. A guess would have over-counted here.

**The divergence is live and it is currently benign by accident.** `runWorkLoop`'s three lazy writes
(§4.5) land on `runWorkLoop`'s copy. The reap closure dereferences `launchWorkLoop`'s copy, where all
three are still nil. So the reap path sees `mergeQ == nil`, `heldEventDedup == nil` and
`queueWriteErrorReported == nil` forever.

I checked whether that matters. It does not, today. Every read of the three is inside `runWorkLoop`'s
own call tree — `internal/daemon/scheduler.go`, `internal/daemon/workloop_handlerpause_kac8g.go`
(`emitHeldEvent`, `pruneHeldDedupOnEpochChange`) and `internal/daemon/scheduler_reservation.go`
(`reportQueueWriteError`) — and none of those three is reachable from
`evaluateGroupAdvanceWithOutcome`. **The guard that saves it is `if deps.X != nil` written three
times.** That is exactly the temporal-validity defect `PRINCIPLES.md` §4 names: whether the field is
valid depends on which copy you hold.

**What the design must do about it.** Give the reap seam a declared port containing the fields it
actually needs, built once in `wireStaleWatcherReapSeams` and captured as a value. The closure then
captures a struct whose every field is populated, and no future write to the loop's copy can silently
fail to reach it. **This is the seam to convert FIRST**, because it is the one place where a partial
cleanup produces the state the map warns about — a bundle that is neither the old thing nor the new
one.

### 5.3 Closure 5 — the per-run dispatch goroutine

```
internal/daemon/scheduler.go  runWorkLoop
  go func(runID core.RunID, beadRecord core.BeadRecord, ... ) {
          defer deps.runRegistry.Unregister(runID)
          env := deps.runEnv(runID, beadRecord, ...)
          ...
  }(...)
```

The parameter list exists to defeat loop-variable capture, and the comment above it says so. **`deps`
is still captured, not passed.** Up to `maxConcurrent` of these run at once, each holding a closure
over `runWorkLoop`'s single copy.

It is safe today for one reason: no field write happens inside the `for` loop. The three lazy writes are
all before it — confirm with the AST write list, which puts them at the top of `runWorkLoop`. So the
copy is effectively frozen once the loop starts.

**What it needs to be safe by construction rather than by inspection.** The goroutine should receive
`env`, `ports` and `handles` as parameters, built on the loop goroutine before the `go` statement.
`deps.runEnv(...)` and `deps.buildRunBundles(env)` already produce exactly those three values.
Hoisting the two calls above the `go` statement removes the capture entirely and costs nothing.
**That is a one-commit change and it is independently valuable, whether or not the rest of this step
lands.**

### 5.4 Closures over `bootState`

Only `wireStaleWatcherReapSeams` closes over `bs`, in both of its callbacks. `SetRunProcessDead`
captures `bs` and a local `reapAdapter` and calls `bs.probeRunProcessDead`, which reads `bs.cfg`. It
touches no `workLoopDeps` field, so it is clear of this step. It still needs `bs.staleWatcher` non-nil
at wire time.

---

## 6. The two consumers Step 12 could not gate

### 6.1 The three claimed sites — verified

All three reproduce exactly as `DECOMPOSITION-MAP.md` §Step 12 states.

| Claim | Verified? | Where it actually sits |
|---|---|---|
| `bs.quiesceArbiter.SetScheduleStore(...)` is inside `injectWorkLoopDeps` | **yes** | Between the `deps.scheduleWakeC` write and the `deps.crewHandler` write, exactly as the map says. |
| `bs.quiesceArbiter.Start(ctx)` opens `startBackgroundLoops` | **yes** | The function opens `cfg := bs.cfg` and `Start` is the next statement. |
| `wireStaleWatcherReapSeams` hands `StaleWatcher` a closure capturing `deps *workLoopDeps` | **yes** | `SetForceReap`. See §5.2 for the closure's full dependency set. |

One addition the map does not make: **`wireStaleWatcherReapSeams` has a second `StaleWatcher` seam,
`SetRunProcessDead`, and it does NOT capture `deps`.** Gating `StaleWatcher` off makes both callbacks
unreachable, but only one of them is a Step 10 concern.

### 6.2 What this design does about them

**`QuiesceArbiter`.** Both of its sites exist because the arbiter needs something the assembly happens
to be holding. `SetScheduleStore` needs the `schedule.Store` that `injectWorkLoopDeps` constructs three
lines earlier. `Start` needs a context.

The design removes the coupling rather than nil-guarding it. **Construct the `schedule.Store` in its
own function and hand it to both consumers.** `newScheduleStore(cfg) (*schedule.Store, error)` returns
the store. `SchedulePort` takes it. `bs.quiesceArbiter` takes it. Neither reaches into the other's
assembly. Once the store has its own constructor, gating the arbiter off is an `if enabled` around the
arbiter's own construction and `SetScheduleStore` never runs — the `newCrewIdleReaperIfEnabled` idiom
already in `internal/daemon/bootsocket.go`, applied where it belongs.

`Start(ctx)` moves out of `startBackgroundLoops` into whatever function constructs the arbiter, which
is `wireWatchersAndObservers` in `internal/daemon/bootstate.go`. It cannot move there today because
`Start` must run post-Seal and `wireWatchersAndObservers` runs pre-Seal. **So this one needs a
post-Seal start hook, not a relocation.** Say that plainly: the arbiter has a two-phase lifecycle
(subscribe pre-Seal, start post-Seal) and the honest fix is a `startable` list the boot drains after
Seal, not a hard-coded call inside the work-loop assembly. `bs.staleWatcher.StartWatcher(ctx)` in
`emitStartupEvents` is the same two-phase shape and is the precedent.

**`StaleWatcher`.** Its `SetForceReap` seam is the closure of §5.2. The design gives that closure a
declared port. Once the port exists, gating the watcher off is a nil check on the watcher, not on
eleven fields reached through a captured pointer.

**Sequencing recommendation for Step 12.** Land §7 stage 2 (the reap-seam port) and §7 stage 3 (the
schedule-store constructor) first. After those two commits, `QuiesceArbiter` and `StaleWatcher` are
ordinary gate-at-the-construction-seam cases and Step 12's remaining lane can take them with the six it
already has. **Do not gate them before those two commits.** The map is right that gating them today
forces a nil-guard inside two of the four functions this step rewrites.

**On `SubscribeHub`, which the map calls "not cleared, only unrefuted".** It is clear of `workLoopDeps`:
the AST field-access table has no `subscribeHub` entry, and `bootState`'s reader table puts its five
reads in `bootsocket.go` (4) and `bootstate.go` (1). None is in a function this step rewrites. **It is
now checked, and it is clear.** It is still inside `wireWatchersAndObservers`' neighbour
`wireSpendAndQueueConsumers`, so lane-plan it against Step 14 rather than against Step 10.

---

## 7. The staged execution plan

Nine stages. Each is one commit. Each leaves the tree green and is independently reviewable. **No
stage is larger than about 120 lines.** The order is chosen so that the highest-risk seam is converted
first, while the tree still has the old shape to compare against.

Every stage's acceptance check assumes Step 9's live pass exists. Where it does not, the named unit
test is the floor, not the ceiling.

### Stage 0 — delete the two fields nothing writes

Remove `codexNoWorkDurationFloor` and `diskLowWatermark` (§3.7) and their `RunEnv` copy. The read sites
already fold zero to a constant.

**Test that proves it:** `internal/daemon/loopmaintenance_test.go`'s disk-low fixtures already drive
`diskLowWatermarkDefault` directly and must still pass unchanged. Add one test that asserts
`codex.NoWorkFloor(0)` returns `codexNoWorkDurationFloorDefault`, so the behavior the field could have
overridden is pinned by something.

**Result: 81 → 79.** This is the only stage that reduces the count without moving anything, and it is
here to make the count honest before the moves start.

### Stage 1 — remove the per-run goroutine's capture of the bundle

§5.3. Hoist `deps.runEnv(...)` and `deps.buildRunBundles(env)` above the `go` statement in
`runWorkLoop` and pass `env`, `ports` and `handles` as parameters.

**Do not stop at "the body no longer mentions `deps`". That is not enforced by anything** — `deps` stays
in scope inside `runWorkLoop`, so the next edit can reach for it again and nothing complains. **Extract
the goroutine body to a package-level function `runDispatchedBead(ctx, env, ports, handles, …)` and
have the `go` statement call that.** A package-level function cannot see `runWorkLoop`'s locals, so the
compiler refuses the regression rather than a reviewer catching it.

**Test that proves it:** dispatch two beads concurrently and assert both runs see the same
`RunEnv.ProjectDir` and distinct `RunEnv.RunID`. **An earlier draft of this stage told the implementer
to reintroduce the capture and confirm the test still passes. That is exactly backwards** — a test that
passes with the defect reintroduced is a test that defends nothing, which is `PRINCIPLES.md` §7's
first warning. The correct mutation is the opposite: build `env` with the wrong `RunID` for the second
bead and confirm the test fails. The capture itself is not what the test defends. The extraction is,
and the compiler defends that.

### Stage 2 — give the StaleWatcher reap seam a declared port

§5.2. Define `reapSeamPort` holding the eleven fields the closure's call tree reads. Build it in
`wireStaleWatcherReapSeams` from `*deps` and capture the value. `evaluateGroupAdvanceWithOutcome` and
`eagerRefillEval` take the port instead of `workLoopDeps`.

**This is the step's highest-risk commit and the reason it is second.** Everything after it is
mechanical.

**Test that proves it:** force-reap a wedged run in a queue with a two-item group and assert the group
advances. Pair the negative with positive evidence per `PRINCIPLES.md` §7 — count the calls that
reached `queue.Persist`, do not merely assert that nothing crashed. Then break it: build the port with
a nil `queueStore` and confirm the test fails.

### Stage 3 — give the schedule store its own constructor

§6.2. `newScheduleStore(cfg Config) (*schedule.Store, error)` in
`internal/daemon/bootworkloop.go`, called from `launchWorkLoop`. Both `injectWorkLoopDeps` and
`bs.quiesceArbiter.SetScheduleStore` receive the returned store.

**Test that proves it:** the existing present-but-unparseable-schedule-file fatal must still be fatal,
and it must now fail in `newScheduleStore` rather than in `injectWorkLoopDeps`.

**After this stage, tell the Step 12 lane it may proceed on `QuiesceArbiter`.**

### Stage 4 — the three single-reader ports

`CoordinatorReapPort`, `EagerRefillPort`, `GovernorPort` — 11 fields, each read by exactly one file
(§3.5). Each becomes a struct constructed from `cfg` and handed to `loopMaintenance`.

**⚠ The governor's OFF signals are not where an earlier draft of this section put them. Trace them
before writing the constructor, because the obvious reading is wrong in both directions.**

> **⚠ WITHDRAWN, and recorded rather than deleted.** A previous version of this stage said a nil
> `governorState` carries "no `.harmonik/config.yaml`", called it a second gate in front of the
> subsystem switch, and prescribed a test asserting that such a boot constructs no governor. **All of
> that is false, and that test would be RED against unmodified code.** The claim also contradicted the
> two-case enumeration in the same paragraph. It is left visible because this document has now made
> the same class of error twice — see §8.2 — and the pattern is more useful than a clean page.

**What the code does.** `deps.governorState` has **exactly one production write**, and it is
**unconditional**, at the end of `seedGovernorDeps` in `internal/daemon/bootworkloop.go`. Walk the
no-config-file case:

- `digest.LoadSentinelConfig` returns `(SentinelConfig{}, nil)` on `os.ErrNotExist`. **No error.**
- `SentinelConfig{}.GovernorConfig()` returns `ErrMissingLivenessNoProgressN` because
  `LivenessNoProgressN` is nil, so `govErr != nil`.
- The `os.Stat(configPath)` guard fails, so the fatal branch is not taken and execution **falls
  through**, leaving the comment the code already wrote: *"No config.yaml: leave governorCfg
  zero-valued (gate disabled)."*
- `deps.governorCfg`, `deps.governorState`, `deps.sentinelMode` and `deps.sentinelPhase2Classes` are
  then all assigned. **`governorState` is NON-nil in this case.**

So there are two OFF carriers and neither is the one the earlier draft named:

| Case | What actually carries OFF |
|---|---|
| `subsystems.movement_governor.enabled: false` | the subsystem switch, read twice — `seedGovernorDeps` returns early AND `newMovementGovernorIfEnabled` checks it first |
| no `.harmonik/config.yaml` | **the zero `governorCfg`**, specifically `LivenessNoProgressN == 0`. `internal/sentinel/governor.go` says so at the accessor: `// 0 = disabled; no default`, and the trip condition reads `ConsecutiveZeroCycles >= Config.LivenessNoProgressN (and N > 0)`. The governor is still **constructed and still ticks** — only the liveness gate can never fire. |

**`newMovementGovernorIfEnabled`'s `deps.governorState == nil` check carries neither.** The subsystem
case is already returned above it, and the no-config case leaves the pointer non-nil. Its only live
role is `cfg.ProjectDir == ""` — unit-test mode — plus test-built `workLoopDeps` literals that never
ran `seedGovernorDeps`.

**What stage 4 does about it, and it is smaller than the withdrawn version claimed.**
`newGovernorPort(cfg, daemonStartTime) (GovernorPort, bool, error)` where the `bool` reads the
subsystem switch and nothing else. `GovernorState` stays a pointer inside the port because it is
genuinely per-daemon mutable state, not a signal. **Do not move the no-config case into the bool** —
it is not an OFF decision, it is a zero-valued threshold that the sentinel package already documents
as disabled, and promoting it to a construction-time OFF would change behavior by also stopping the
observe pass and the cadence tick. `newMovementGovernorIfEnabled` loses its nil check, which is the
one real simplification here, and it must gain nothing in its place.

**Split this stage into three commits, one per port**, unless review says otherwise. They share no
field. The governor commit is the one that needs the most care.

**Test that proves it:** two tests, and both must be watched failing first.
1. The existing fail-loud-only-with-a-config.yaml behavior (`hk-drygf` FIX-B) must survive — a
   malformed `liveness_no_progress_n` with a `.harmonik/config.yaml` present is fatal, and without one
   is silent. That test is the only thing standing between this move and a daemon that refuses to boot
   on a machine with no project config.
2. **A boot with `ProjectDir` set, `movement_governor` enabled, and no `.harmonik/config.yaml` must
   still CONSTRUCT a governor, and that governor must never trip the liveness gate.** Note the shape:
   it is a pair, and the negative half is worthless alone. Prove the governor was built and ticked —
   count the observe passes — and then prove no halt or trip was raised. A test that only asserts "the
   daemon did not die" passes for free in any environment where nothing happens, which is
   `PRINCIPLES.md` §7's named trap.

**Result: 79 → 68.**

### Stage 5 — `DiskReclaimPort` and `SchedulePort`

10 fields — `DiskReclaimPort` is down to five after stage 0 took `diskLowWatermark`. Same shape as
stage 4 but each port has a second reader, so each needs the ambient `projectDir` / `bus` copy of §3.6.

**Test that proves it:** the disk-low latch must still skip bead claiming while set. Prove the
machinery ran — count probe calls — rather than asserting that no dispatch happened.

**Result: 68 → 58.**

### Stage 6 — `LedgerRepair` and `LoopLifecycle`

8 fields, both read only by `runWorkLoop`. Straight lifts.

**Result: 58 → 50.**

### Stage 7 — `Capacity`, `QueueSurface` and `DispatchGates`

10 fields, counting `noAutoPull`. This is where the three lazy initialisations (§4.5) become
loop-owned locals and the three `if deps.X != nil` guards are deleted.

**Test that proves it:** the held-event dedup must still fire at most once per (bead, epoch). Break it
by clearing the map and confirm a second `held` event appears.

**Result: 50 → 40.**

### Stage 8 — cut the run shell's last two ties, and delete the type

The remaining 40 are Owner A's, and they already reach the run shell through declared bundles. What is
left is the two adapters. **`daemonLedger` and `daemonBudget` in `internal/daemon/runports.go` each
declare a `deps *workLoopDeps` struct field.** They are not closures — §5.1's table does not contain
them, and it says so. Both are handed out as long-lived `runloop.LedgerPort` and `runloop.BudgetPort`
values, so each is a retention of the whole bundle by a type the run shell holds. **These two are the
last things keeping the bundle alive after this plan's other stages land**, which is why the type
cannot be deleted before them. Replace both with value structs holding the six fields they need (`brAdapter`,
`intentLogDir`, `brTimeoutCfg`, `projectDir`, `skipBrHistoryRotation`, `queueStore`).
`closeBeadWithHistoryTrim` becomes a function on the ledger adapter rather than a method on the bundle.

At the end of this stage `workLoopDeps` has no reader. Delete it, delete `newWorkLoopDeps`, and replace
`buildWorkLoopDeps` / `injectWorkLoopDeps` with one function that constructs the eleven ports in the
order §4.3 requires.

**Test that proves it:** the compiler. The type is gone.

### 7.1 What this plan does NOT do

**It does not touch `bootState`.** §8.2 explains why. If `bootState` is in scope for whoever executes
this, it is a separate plan and it comes after stage 8.

**It does not touch `daemon.Config`.** Step 27a owns that and depends on this one. The eleven port
constructors this plan produces are the input to that step's required-field check, so doing it after
means doing it once.

### 7.2 Blast radius on the test tier

- 8 `_test.go` files in `internal/daemon` construct a `workLoopDeps{}` literal, 14 literals in total.
  Method: `grep -rln 'workLoopDeps{' internal/daemon/*_test.go | wc -l` for the files, and
  `grep -rc 'workLoopDeps{' internal/daemon/*_test.go | awk -F: '{s+=$2} END {print s}'` for the
  literals.
- 23 `_test.go` files mention `workLoopDeps` at all.
- 6 mention `bootState{`.

**That is small.** The 800-line estimate in the map is a production-side figure and the test side does
not multiply it. The literals are the migration cost, and 14 is a morning.

---

## 8. Risks, and what I could not determine

### 8.1 Risks this plan carries

**The reap seam is the one that can lose work.** Stage 2 changes the code path that drives a wedged
run's queue group forward. Getting it wrong leaves a group stalled with no error, which is the failure
mode §5.2 says the current nil-guards are hiding. It is stage 2 rather than stage 8 for exactly that
reason — convert it while the old shape is still there to diff against.

**Two dead stores will look like behavior changes when they are removed.** §2.3, which finds **eight**
multi-write fields and marks exactly two of them dead. Removing
`deps.cancelOnQueueDrain = cfg.CancelOnQueueDrain` from `injectWorkLoopDeps` and the first
`deps.handlerPauseController` write is correct, and a reviewer who has not read §2.3 will read the
diff as dropping a wire. Cite that section in those commit bodies — and cite the whole table, because
the other six repeats are load-bearing and a reader who takes "dead store" as the pattern will delete
one of them.

**Ambient `projectDir` and `bus` copies will look like duplication.** §3.6. Eleven ports each holding
their own `projectDir` is the design, not an oversight. Charter §4's "consolidate by default" points
the other way and it is worth naming the exception in the commit: consolidating these two into a shared
holder recreates the bundle at smaller scale.

**Stage 4's governor move can refuse a boot, and it can also silently make the liveness gate live on a
machine with no project config.** Both failure directions are real and both are covered by the two
tests named in that stage. Do not let it land with only the first. **Read that stage's withdrawn note
before designing the constructor** — the obvious reading of where the OFF signal lives is wrong, and it
was wrong in this document for one round.

**⚠ Scoping "the socket subtree" to `bindSocket` reintroduces a panic.** Of the eight `bootState`
fields that go nil under `subsystems.socket_listener.enabled: false`, **seven are written inside
`bindSocket`'s subtree and the eighth is not.** `tunerBackstop` is written in
`wireWatchersAndObservers`, pre-Seal, behind its own `socketListenerEnabled() && bandwidthTunerEnabled()`
guard. Its consumer, `startBandwidthTunerIfEnabled`, calls `SetTuner`, and `SetTuner` does
`b.tuner.Store(t)` on the receiver with no nil check. The code already says so — the doc comment above
`bandwidthTunerEnabled` in `internal/daemon/bootsocket.go` reads *"That invariant is load-bearing,
because startBandwidthTunerIfEnabled calls SetTuner on the backstop, which panics on a nil receiver."*
**A refactor that treats "the socket subtree" as "whatever `bindSocket` constructs" moves seven of the
eight and leaves the producer of the eighth behind its old guard.** Nothing in this plan does that.
Anything built on top of it might.

### 8.2 What I could not determine

**Whether `bootState` should be part of this step at all. The scope decision holds. One of the reasons
I first gave for it does not, and it is corrected here.**

I wrote that `bootState` has "no field written in two phases". **That is true of assignments and false
of initialization, and an AST assignment table cannot tell the difference.** I named that exact limit
for my own call-graph trace in §5.2 and then failed to name it here. **At least eight** of the 22
fields are constructed in one phase and completed by a setter in a later one:

| Field | Constructed in | Completed later by |
|---|---|---|
| `handlerPauseCtrl` | `constructBusAndRegistries` | `SetAdapter`, `SetPersistFn` in `bootsocket.go` |
| `staleWatcher` | `wireWatchersAndObservers` | `StartWatcher` in `emitStartupEvents`; `SetForceReap`, `SetRunProcessDead` in `wireStaleWatcherReapSeams` |
| `tunerBackstop` | `wireWatchersAndObservers` | **`SetTuner` only**, in `startBandwidthTunerIfEnabled` |
| `quiesceArbiter` | `wireWatchersAndObservers` | `SetDrain`, `SetScheduleStore`, `Start` |
| `subscribeHub` | `wireSpendAndQueueConsumers` | `SetCommsCursorStore` |
| `queueHandlerAdapter` | `buildQueueHandler` | `SetWorkerToggleFunc` in `injectWorkLoopDeps` |
| `crewIdleReaper` | `buildCommsAndCrewHandlers` | `StartWatcher` in `startBackgroundLoops` |
| `branchReapWatcher` | `buildCommsAndCrewHandlers` | `StartWatcher` in `startBackgroundLoops` |

Two of the code's own comments say "Two-phase" in so many words. **So `bootState` does have a
temporal-validity problem. It is carried by setters rather than by field writes, which is why the
measurement missed it.**

**Three things about that table, and the third is the point.**

- **`tunerBackstop`'s `SetRunRegistry` is NOT a later phase.** Construction, `Subscribe` and
  `SetRunRegistry` are three consecutive statements inside one `if` block in `wireWatchersAndObservers`.
  Only `SetTuner` crosses a phase. An earlier draft listed both and was wrong.
- **`crewIdleReaper` and `branchReapWatcher` were missing.** They meet exactly the test applied to
  `staleWatcher` — assigned in one function, `StartWatcher` called in another.
- **"At least eight", not "eight", because the scan that produced this table has its own blind spot,
  and it is the same blind spot the paragraph above exists to confess.** The table was built by looking
  for `bs.<field>.Set*(` and `bs.<field>.Start*(`. That pattern cannot see a setter called on a **local
  alias before the field is assigned**, and `buildQueueHandler` contains one:
  `adapter.SetGlobalMaxConcurrent(cfg.MaxConcurrent)` runs two statements before
  `bs.queueHandlerAdapter = adapter`. **A method-call scan keyed on the field name is blind to it, just
  as the assignment table was blind to setters.** Treat this table as the sites found by one pattern,
  not as a census. Anyone completing it needs a third method, not a wider grep.

**The scope decision stands on the ground that survives.** Every `bootState` field still has exactly
one assigning writer — 20 writes for 20 assignable fields, plus `cfg` and `hooks` set in the
`&bootState{cfg: cfg, hooks: hooks}` literal — and no field is overwritten. Splitting the struct by
writer would produce seven structs and no compiler-checked improvement, because the nullability that
actually bites comes from the socket switch and the incompleteness comes from the setters, and neither
is a property of the struct's shape. **Both are better attacked as "give each two-phase member a
constructor that takes everything it needs", which is a different change from splitting a bundle, and
it is Step 12's surface more than this one's.**

**I could not determine whether the right answer is to split it, to leave it, or to make the eight
nullable members an explicit "socket subtree present" sum type.** The last is the `PRINCIPLES.md` §2
answer and it is a bigger change than this step. **Re-measure before deciding, and do not assume the
map's "same shape" sentence — or mine.**

**Whether `adapterReg` being outside the socket gate is deliberate.** `registerAdaptersAndHookStore`
runs before `bindSocketIfEnabled`, so `bs.adapterReg` survives the switch. That matters because
`newWorkLoopDeps` refuses to construct with a nil `adapterRegistry` — *"required by waitAgentReady
(hk-d8u1y deleted the nil-guard)"*. If it were inside the gate, switching the socket listener off would
stop the daemon booting. It is outside, so it does not. **I could not determine whether that is a
decision or an accident**, and it is worth a comment either way, because it is one function call away
from being a boot-breaking regression.

**Whether the 11 fields in §5.2's reap-seam dependency set are complete under all inputs.** I traced
`evaluateGroupAdvanceWithOutcome` → `eagerRefillEval` → `preScreenCandidates` → `buildInQueueSet` /
`emitStaleOpenBeadDetected` by AST. That is a static call graph over one package. **An interface
dispatch inside that tree would be invisible to it.** Re-derive the set before writing stage 2, and
build the port from the derivation rather than from this table. **The number is eleven.** §5.2, §6.2
and stage 2 all say eleven, and any other figure in any draft of this document is wrong.

**Whether the live pass of Step 9 can actually observe any of this.** Every stage's real acceptance is
"a real bead runs end to end", and I did not run one.

> **⚠ A claim that stood here was false and is corrected rather than deleted.** The first version of
> this section said `scripts/scratch-daemon.sh` defaults `SCRATCH_WORKFLOW_MODE` to `review-loop`, a
> mode the daemon no longer offers. **It does not, and it has not since 2026-07-31.**
> `grep -n 'SCRATCH_WORKFLOW_MODE' scripts/scratch-daemon.sh` returns two hits and both read `dot` —
> the usage block and `local workflow_mode="${SCRATCH_WORKFLOW_MODE:-dot}"`. It was fixed at
> `e7941bffd`, *"test: default scratch daemons to dot mode"*. `scripts/core-loop-matrix.sh` defaults
> to `dot` as well.
>
> **The claim was inherited from `DECOMPOSITION-MAP.md` §Step 9 and restated as measurement in a
> document whose whole authority is that every number carries the command that produced it.** That is
> the charter §5 pattern — *"a claim that sounds like a blocker gets repeated as one"* — reproduced
> inside the document written to stop it. It is recorded here rather than quietly fixed, because the
> lesson is the point.
>
> **`DECOMPOSITION-MAP.md` §Step 9 is stale on this and now says so** — see the dated note added to it
> in the same commit as this correction. Its sentence *"The first thing a live pass needs is not new
> code. It is a default that names a mode that exists"* has been true and discharged since 2026-07-31.

So the honest statement is the smaller one: **the scratch-daemon apparatus is not blocked on its
default.** Whether a live pass observes what these stages change is still open, and I did not test it.
If Step 9 has not landed when this starts, the named unit tests are the only oracle, and §5.2's benign
divergence is a warning about how much a unit test can miss.

### 8.3 What a reader should re-measure before acting

1. The field count and the 26/3 write split (§1.2, §1.3). They move.
2. The reap-seam dependency set (§5.2). It is the load-bearing one, and it is **eleven** fields.
3. Whether `beadRunOne` still takes `runloop.RunEnv` and not `workLoopDeps` (§4.1). The whole shape of
   the step rests on that signature.
4. Whether `dot_cascade_core.go` still holds zero `workLoopDeps` references. It is 1,736 lines and it
   is the other half of the duplication Charter §1 names, so a new reference there would be a
   regression worth stopping.
5. **Anything in this document that names a script's behavior.** §8.2's `review-loop` claim is the one
   error that got past the standard of evidence, and it got past because it was about a shell script
   rather than about Go, so no AST pass touched it. Run the script or grep it. Do not inherit it.
