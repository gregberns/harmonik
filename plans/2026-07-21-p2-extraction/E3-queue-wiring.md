# Unit E3 — Queue wiring → `internal/queuewiring`, behind the `internal/queue` RPC seam

**Status:** NEEDS PREPARATORY SLICE (for E3b only — **E3a is READY and ships first, unblocked**)
**Depends on:** nothing. E3a has zero dependency on E1 or E2; it can land out of order at any time.
**Size:** ~1,455 non-test LOC across 5 production files + ~2,187 test LOC across 9 test files, split into two shippable slices:
 - **E3a** — 818 non-test LOC (3 files) + 1,417 test LOC (6 files moved whole, 1 split)
 - **E3b** — 637 non-test LOC (2 files) + 576 test LOC (2 files), gated on a prep slice
**Risk:** MEDIUM — the *production* surface is a genuinely clean lift (an AST free-identifier scan over all of `package daemon` returns ZERO outbound daemon references for E3a's three files), but the *test* surface carries one hard compile break (a method declared on `QueueStore` from an in-package test file that touches unexported fields) and ~40 bare-constructor call sites in six `package daemon` test files that no `export_test.go` shim can rescue.

---

## 0. Reconciliation note — recon vs. adversarial challenge

Both inputs were re-verified against the tree before this plan was written. Where they disagreed:

| Disputed claim | Taken | Why |
|---|---|---|
| Number of new exports required (recon: 2; challenge: 1) | **1 required, 1 optional-but-do-it** | Challenge is right on the mechanics: `.golangci.yml:85` enables revive `[exported, package-comments, var-naming, error-return, error-naming, if-return]` — `unexported-return` is **not** enabled. And `export_test.go:2792` returns the interface `ExportedQueueLedger`, not the concrete type. So only `newBRQueueLedger → NewBRQueueLedger` is *forced*. We still export the type (`brQueueLedger → BRQueueLedger`) because an exported constructor returning an unexported type is a hostile API for the next reader. Counted below as **1 required + 1 hygiene**. |
| `ExportedQueueStoreSetQueue` moves to queuewiring (recon) vs. stays in daemon (challenge) | **Challenge — it STAYS** | Verified: its only caller is `internal/daemon/perqueuespendmeter_tigaf11_test.go:101`, and that test does not move until E3b. Moving the shim in E3a breaks a test E3a leaves behind. |
| `stubEventCollector` must be copied into `queuewiring/fixtures_test.go` (recon) vs. not needed (challenge) | **Challenge — do NOT copy it** | Verified: its only use in `queue_perqueue_pause_tigaf6_test.go` is line 256, inside `TestPerQueuePause_DoesNotSetGlobalFlag` — the one test that stays in daemon. It is also 55 LOC (`internal/daemon/workloop_test.go:186-240` plus the `stubEmittedEvent` companion), not ~15. Copying it would be dead code in the new package. |
| "13 daemon call sites" (recon) | **Challenge — both undercount** | Real production-file count is **8 files**; see §3b. Plus `export_test.go` (6 signature edits), 6 in-package test files (~40 sites), 3 external test files, 1 cmd file. |
| "four scenario tests reference `DaemonSpendMeter` by type name" (recon) | **Challenge — one file, two sites** | Verified: `internal/daemon/cognition_loop_scenario_hkc7lxc_test.go:181` and `:319`. Recon's E3b recipe never listed the file at all; it is listed in §4 here. |
| depguard self-import line is a no-op because `internal/queue` prefix-matches `internal/queuewiring` (challenge) | **Challenge on the mechanics, but KEEP the line** | Prefix semantics confirmed at `.golangci.yml:696-706` (`bootconfig` comment). But we are NOT putting `internal/queue` in queuewiring's allow list as a bare prefix — see §4 STEP 10, where the entries are written to be individually meaningful. The self-import line is load-bearing there. |
| "golangci-lint is NOT installed" (challenge) | **Rejected — recon's gate stands, with a correction** | `which golangci-lint` does fail, but `./.tools/golangci-lint` v2.3.0 exists (installed by `make tools`, `Makefile:618`). All lint commands in §6 use the `.tools/` path. |

**One finding neither input caught**, verified here and folded in: `internal/daemon/operatornfr_pause_inflight_hk95a2r_test.go:327-329` (a 514-LOC scenario test that STAYS in daemon) calls `daemon.ExportedNewQueueOperatorEventConsumer` and `daemon.ExportedQueueOperatorEventConsumerConfig`. Those two shims must therefore **stay in `internal/daemon/export_test.go`** (re-pointed at `queuewiring`), exactly like `ExportedNewQueueStore`. Recon's STEP 5 would have moved them and broken that test.

**Two scope corrections from the recon, both independently confirmed and both adopted:**
- `internal/daemon/dispatchsegment.go` is **not** queue wiring. "Dispatch" there is the `runexec` Dispatch state machine (RT8 launch/ready/brief segment adaptor). It has zero references to `internal/queue`. **Reassign to E5.**
- `internal/daemon/socketdispatch.go` is the socket composition root's adapter table for all 27 routable ops; only 8 route through `handleQueueOp`, and it names 16 daemon-owned handler types. **Drop from E3 permanently — it stays in daemon.**

The `_plan.md` §2 E3 bullet must be edited to reflect both before anyone is staffed on this unit.

---

## 1. What moves

### E3a — ships first, no prerequisites

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/queuestore_hkj808w.go` | 409 | `internal/queuewiring/store.go` | The in-memory name-keyed `QueueStore` + `LockedQueueStore`. Imports only `sync` + `internal/queue`. Needs the package doc comment (revive `package-comments` is on). |
| `internal/daemon/queueledger_bridge.go` | 102 | `internal/queuewiring/beadledger.go` | `brcli.Adapter → queue.BeadLedger` bridge. Imports `context`, `errors`, `internal/{brcli,core,queue}`. |
| `internal/daemon/queue_operatoreventconsumer_7urls.go` | 307 | `internal/queuewiring/operatorevents.go` | operator-pause / operator-resuming → queue active↔paused-by-drain transitions. Imports `internal/{core,eventbus,queue}`. |
| `internal/daemon/queuestore_hkj808w_test.go` | 146 | `internal/queuewiring/store_test.go` | `package daemon_test` → `package queuewiring_test`. |
| `internal/daemon/queuestore_append_lostupdate_hkb1_test.go` | 200 | `internal/queuewiring/store_append_lostupdate_test.go` | Imports `testify/require` — must be in the depguard allow list. |
| `internal/daemon/queuestore_namedqueues_hktigaf2_test.go` | 230 | `internal/queuewiring/store_namedqueues_test.go` | |
| `internal/daemon/queuestore_wakeongap_hkekj_test.go` | 234 | `internal/queuewiring/store_wakeongap_test.go` | |
| `internal/daemon/queueledger_bridge_hkdv8qv_test.go` | 186 | `internal/queuewiring/beadledger_test.go` | Imports `internal/brcli`. |
| `internal/daemon/queue_operatoreventconsumer_7urls_test.go` | 421 | `internal/queuewiring/operatorevents_test.go` | Imports `github.com/google/uuid`. |
| `internal/daemon/queue_perqueue_pause_tigaf6_test.go` | 394 | **SPLIT** — 12 of 13 decls → `internal/queuewiring/operatorevents_perqueue_pause_test.go`; `TestPerQueuePause_DoesNotSetGlobalFlag` stays | See §4 STEP 7. Imports `uuid`. |

**E3a non-test LOC leaving daemon: 818.**

### E3b — after the prep slice (§4, E3b-PREP)

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/perqueuespendmeter_tigaf11.go` | 346 | `internal/queuewiring/perqueuespendmeter.go` | Blocked today by `*RunRegistry`. |
| `internal/daemon/spendmeter_hkk3f8g.go` | 291 | `internal/queuewiring/spendmeter.go` | **Not in `_plan.md`'s candidate list — add it.** Zero outbound daemon coupling of its own; owns `bytesPerUSD` (`:78`), `defaultDailyBudgetUSD` (`:67`), `envFlywheelBudgetUSDPerDay` (`:63`), `spendMeterTodayKey` (`:289`), all consumed by `perqueuespendmeter`. Must land in the *same* PR — a duplicated budget constant would silently mis-bill on drift. |
| `internal/daemon/perqueuespendmeter_tigaf11_test.go` | 275 | `internal/queuewiring/perqueuespendmeter_test.go` | |
| `internal/daemon/spendmeter_hkk3f8g_test.go` | 301 | `internal/queuewiring/spendmeter_test.go` | |

**E3b non-test LOC leaving daemon: 637.**

### Files that STAY in `internal/daemon` — and why

| File | Why it stays |
|---|---|
| `internal/daemon/dispatchsegment.go` (295) | **Misclassified in `_plan.md`.** It is the `runexec` Dispatch state-machine adaptor. AST scan of its free identifiers returns exactly `{newRunShell, runShell, runEffectors, perRunEventTap, clockAfter, ErrSpawnCapTimeout, ErrTmuxNewWindowTimeout}` — all E4/E5 material, none queue. Zero references to `internal/queue`. **Reassign to E5.** |
| `internal/daemon/socketdispatch.go` (436) | **Misclassified in `_plan.md`.** The socket composition root's adapter table for all 27 routable ops; names 16 daemon-owned handler interfaces/types. 19 of 27 ops have nothing to do with queues; the 8 that do already route through the `QueueHandler` seam into `queue.HandlerAdapter`. Moving it would require moving `socket.go` — the daemon's wire surface. **Drop from E3 permanently.** |
| `internal/daemon/queue_operatoreventconsumer_composition_7urls_test.go` (85) | It drives `daemon.Start` / `startWithHooks` / `WithBusObserver`. It is a composition-root wiring assertion — the **only** regression guard proving the extracted consumer is still subscribed to the bus before `Seal` (EV-009). **Must NOT be deleted.** |
| `internal/daemon/operatornfr_pause_inflight_hk95a2r_test.go` (514) | Scenario test spanning operator-pause, the socket listener, and the consumer. Stays; its three `Exported*` consumer shims stay with it. |
| `internal/daemon/operatorpause.go` | `OperatorPauseController` is daemon's; only one moved-file test touched it, and that test stays behind (§4 STEP 7). |
| `internal/daemon/runregistry.go` | E5 material. E3b routes around it with a closure port, never moves it. |
| `internal/daemon/{stategather,draindetect,eagerfill_em063,statedisk}.go` | Surveyed as the only other daemon non-test files importing `internal/queue`. All are `workLoopDeps`/`snapshotFleet`-entangled (E5) or comment-only. |

---

## 2. The seam it exits behind

**No new seam is invented.** The unit exits behind the pre-existing `internal/queue` typed RPC surface:

| Interface | Defined at | Satisfied by |
|---|---|---|
| `queue.QueueSetter` | `internal/queue/rpc.go:43` | `*QueueStore` (comment at `rpc.go:47` already says "daemon.QueueStore satisfies this interface") |
| `queue.LockedQueueView` | `internal/queue/rpc.go:62` | `*LockedQueueStore` (comment at `rpc.go:63` names it) |
| `queue.MutationLocker` | `internal/queue/rpc.go:76` | `*QueueStore` via `LockForMutationView` (`queuestore_hkj808w.go:286`) |
| `queue.BeadLedger` | `internal/queue/types.go` (consumed at `rpc.go` and `daemon/workloop.go:585`) | `*brQueueLedger` |
| `eventbus.EventBus` subscription | `internal/eventbus` | `QueueOperatorEventConsumer.Subscribe`, `PerQueueSpendMeter.Subscribe` |

The seam was **built for exactly this**: `internal/queue/rpc.go:40` reads *"…in-memory QueueStore without importing internal/daemon (cycle prevention)."* The daemon already hands `*QueueStore` across it as an interface value (`bootsocket.go:163`: `queue.NewHandlerAdapter(newBRQueueLedger(...), cfg.ProjectDir, bs.qs, bs.bus)`). Extraction changes **which package declares the concrete type**, not how it is consumed.

**No new port is needed for E3a.** ZERO. This is the cleanest unit in P2 so far.

**E3b needs one new port — declare it, do not hide it.** `PerQueueSpendMeter` currently holds a `reg *RunRegistry` field and calls `reg.Get(runID).QueueName`. `RunRegistry` is E5 material and cannot move. The port is a **function value, not an interface**: `queueNameForRun func(core.RunID) (string, bool)`, injected by the daemon as a closure over `bs.sharedRunRegistry`. That is the "daemon threads effectful closures IN" idiom `_plan.md` §0 names as the established pattern (`mergeq`/`runexec`/`router`), not a new abstraction layer. It is still a delta from a pure move and **must be flagged at the review gate**.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

**E3a: ZERO rows.** An AST free-identifier scan across all 651 `.go` files of `package daemon`, resolving every package-level declaration to its defining file, returns no non-local daemon references from `queuestore_hkj808w.go`, `queueledger_bridge.go`, or `queue_operatoreventconsumer_7urls.go`. Their entire import closure is `sync`, `context`, `errors`, `encoding/json`, `fmt`, `time` + `internal/{core,queue,eventbus,brcli}`. Nothing to break.

**E3b:**

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| `RunRegistry` (`.Get` → `(*RunHandle, bool)`, read for `.QueueName`) | `internal/daemon/runregistry.go:212` (`Get`), `:32` (`RunHandle`), `:44` (`QueueName`) | `perqueuespendmeter_tigaf11.go:97` (field), `:117` (ctor param), `:174-179` (use) | **inject-as-port.** Replace the field with `queueNameForRun func(core.RunID) (string, bool)`; `bootstate.go:167` passes a closure over `bs.sharedRunRegistry`. This is the ONLY genuine outbound blocker in the whole unit. |
| `spendMeterTodayKey` | `internal/daemon/spendmeter_hkk3f8g.go:289` | `perqueuespendmeter_tigaf11.go:119, :263` | **move-too** — `spendmeter_hkk3f8g.go` goes in the same PR. |
| `bytesPerUSD` | `spendmeter_hkk3f8g.go:78` | `perqueuespendmeter_tigaf11.go:201` | **move-too. Do NOT duplicate.** It is a budget conversion constant; two copies drifting silently mis-bills. |
| `defaultDailyBudgetUSD` | `spendmeter_hkk3f8g.go:67` | `perqueuespendmeter_tigaf11.go:336, :344` | move-too |
| `envFlywheelBudgetUSDPerDay` | `spendmeter_hkk3f8g.go:63` | `perqueuespendmeter_tigaf11.go:333` | move-too |

Verified: every symbol `spendmeter_hkk3f8g.go` owns (`bytesPerUSD`, `defaultDailyBudgetUSD`, `envFlywheelBudgetUSDPerDay`, `defaultMaxRunsPerDay`, `envMaxRunsPerDay`, `daemonDailyBudgetRef`, `spendMeterTodayKey`) has **zero callers anywhere in the repo** outside the two spend-meter files.

**E3a test-side outbound (one row, and it is why a test file splits):**

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| `OperatorPauseController` (via `ExportedNewOperatorPauseController`) + `stubEventCollector` | `internal/daemon/operatorpause.go`; `internal/daemon/workloop_test.go:186` | `queue_perqueue_pause_tigaf6_test.go:255-257` — inside `TestPerQueuePause_DoesNotSetGlobalFlag` **only** | **Leave that one test function behind** in `internal/daemon`. Do not drag `operatorpause.go`; do not copy `stubEventCollector` (55 LOC) into the new package — nothing there would use it. |

### 3b. Inbound (daemon → unit)

**Symbols needing a new export: 1 required + 1 hygiene = 2 renames.**

| Current name | Proposed name | Call sites to rewrite |
|---|---|---|
| `newBRQueueLedger` (unexported func, `queueledger_bridge.go`) | `queuewiring.NewBRQueueLedger` | **REQUIRED.** `workloop.go:1175`, `bootsocket.go:163`, `bootsocket.go:171`, `export_test.go:2793` |
| `brQueueLedger` (unexported type) | `queuewiring.BRQueueLedger` | **Hygiene only** (not a compile or lint requirement — `unexported-return` is off, and `export_test.go:2792` returns the `ExportedQueueLedger` *interface*). Do it anyway so the exported constructor has an exported return type. |
| `newQueueStore` | — **no export needed** | The exported `NewQueueStore` already exists at `queuestore_hkj808w.go:93`. Switch `bootstate.go:127` from `newQueueStore()` to `queuewiring.NewQueueStore()`. |
| `QueueStore`, `LockedQueueStore`, `QueueOperatorEventConsumer`, `QueueOperatorEventConsumerConfig`, `NewQueueOperatorEventConsumer`, `NewPerQueueSpendMeter`, `PerQueueSpendMeter`, `NewDaemonSpendMeter`, `DaemonSpendMeter` | — already exported | Import + qualification churn only. |
| `cloneQueue` / `cloneGroup` / `cloneItem` / `submitWakeCBufSize` / `perQueueCounters` / `spendMeterGlobalCapUSD` | — stay unexported | Repo-wide grep: no callers outside their own file. |

**Production files needing an import + qualification (E3a) — 8 daemon files + 1 cmd file:**

| File | Sites |
|---|---|
| `internal/daemon/workloop.go` | `:567` (`queueStore *QueueStore`), `:1175` (`newBRQueueLedger`), `:1436`, `:1469`, `:6149` (`lq *LockedQueueStore`) |
| `internal/daemon/stategather.go` | `:40`, `:60` |
| `internal/daemon/draindetect.go` | `:215`, `:234` |
| `internal/daemon/bootstate.go` | `:37`, `:127`, `:173-174` |
| `internal/daemon/daemon.go` | `:406` (`Config.QueueStore`), `:679` (`loadStartupQueues` param) |
| `internal/daemon/quiesce.go` | `:183` **only**. `:427`, `:428`, `:599`, `:602` are field accesses / method calls on the config field — no edit. (The recon's one-line claim here is incomplete and would mislead a verifier.) |
| `internal/daemon/bootsocket.go` | `:163`, `:171` |
| `internal/daemon/perqueuespendmeter_tigaf11.go` | `:97` (`store *QueueStore` field), `:117` (`NewPerQueueSpendMeter` param). **This file stays in daemon through E3a** and must be requalified now. |
| `cmd/harmonik/run.go` | `:631` (`daemon.NewQueueStore()` → `queuewiring.NewQueueStore()`). `:720` (`Config{QueueStore: qs}`) needs no text change once the field type moves. |

**Deliberately NOT touched** (mention `QueueStore` in comments or through local interfaces only — verified line by line): `internal/daemon/{crewidlereap.go:65, runports.go:170, stategather.go:6, statedisk.go:57, socket.go:187, wiringlog_hk4mupj.go, operatorpause.go, composition_registry_hkndysh.go}`, all of `internal/{lifecycle,orchestrator,schedule,queue}`, `cmd/harmonik/state_cmd.go:6`.

**Test files needing edits (E3a) — this is the surface the recon never surveyed:**

| File | Package | Edits |
|---|---|---|
| `internal/daemon/export_test.go` | `daemon` | 6 signature re-types: `:184` (`WorkLoopDepsParams.QueueStore`), `:1963` (`ExportedNewPerQueueSpendMeter`), `:2000` (`ExportedQueueStoreSetQueue`), `:2261` (`ExportedNewQueueStore`), `:2295` (`ExportedQueueStoreOf`), plus deleting the three shims that move (§4 STEP 5). `:467-468` (`p.QueueStore.WakeCh()`) needs no change. |
| `internal/daemon/draindetect_test.go` | `daemon` | 23 bare `NewQueueStore()` sites |
| `internal/daemon/quiesce_sleep_veto_hkzqb3_test.go` | `daemon` | 9 bare `NewQueueStore()` sites |
| `internal/daemon/eagerfill_em063_test.go` | `daemon` | 4 bare `newQueueStore()` sites (`:111, :135, :169, :243`) + helper signature `:79 func em063FixtureDeps(t *testing.T, qs *QueueStore)` |
| `internal/daemon/quiesce_test.go` | `daemon` | `:349` bare `NewQueueStore()`, `:111` helper signature `newTestQuiesceArbiter(..., qs *QueueStore, ...)`, and **`:422` the hard compile break** (§7 R1) |
| `internal/daemon/run_session_adoption_hk78tji_test.go` | `daemon` | `:610` |
| `internal/daemon/workloop_perqueue_roundrobin_hktigaf4_test.go` | `daemon` | `:192` |
| `internal/daemon/reviewloop_budget_hkc1ah6_test.go` | `daemon_test` | `:128` return type `*daemon.QueueStore` → `*queuewiring.QueueStore` |
| `internal/daemon/perqueuespendmeter_tigaf11_test.go` | `daemon_test` | `:96` (`*daemon.QueueStore`), `:99` (`daemon.NewQueueStore()`) |
| `internal/scenario/named_queues_workers_test.go` | `scenario` | `:379`, `:432` |
| `internal/scenario/queue_lifecycle_test.go` | `scenario` | `:483` |

The ~70 other `daemon_test` files that set `QueueStore: qs` **need zero edits** — none of them names the type; they all obtain `qs` from `daemon.ExportedNewQueueStore()`, and ~16 more chain off `daemon.ExportedQueueStoreOf(deps)`. Keeping those two shims in place is the single decision that keeps the diff bounded.

---

## 4. Step-by-step recipe

All commands run **from the repo root** (`/Users/gb/github/harmonik`). Never `cd` into a worktree.

### E3a — ships as one PR, today, with no prerequisites

**STEP 0 — record the baseline.**
```bash
find internal/daemon -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1
```
Measured on `phase1-session-restart-substrate` at plan time: **134 non-test files, 58,971 non-test LOC.** Record whatever you measure; §6 compares against it.

**STEP 1 — scaffold and move the three production files.**
```bash
mkdir -p internal/queuewiring
git mv internal/daemon/queuestore_hkj808w.go              internal/queuewiring/store.go
git mv internal/daemon/queueledger_bridge.go              internal/queuewiring/beadledger.go
git mv internal/daemon/queue_operatoreventconsumer_7urls.go internal/queuewiring/operatorevents.go
```
Change `package daemon` → `package queuewiring` in all three. Add a package doc comment to `store.go` (revive `package-comments` is enabled at `.golangci.yml:85` and will fail without one):
```go
// Package queuewiring holds the daemon-side queue OWNERSHIP layer extracted out
// of internal/daemon (P2 unit E3). The queue ENGINE is already a clean leaf
// (internal/queue); this package holds the in-memory QueueStore that satisfies
// queue.QueueSetter / queue.LockedQueueView / queue.MutationLocker, the brcli →
// queue.BeadLedger bridge, and the operator pause/resume consumer. The daemon is
// the composition root and injects these; it MUST NOT be imported back.
package queuewiring
```

**STEP 2 — the exports.** In `internal/queuewiring/beadledger.go`: rename `brQueueLedger` → `BRQueueLedger` and `newBRQueueLedger` → `NewBRQueueLedger` (constructor returns `*BRQueueLedger`). Nothing else needs exporting — `QueueStore`, `LockedQueueStore`, `NewQueueStore`, `QueueOperatorEventConsumer`, `QueueOperatorEventConsumerConfig` and `NewQueueOperatorEventConsumer` are already exported; `cloneQueue`/`cloneGroup`/`cloneItem`/`submitWakeCBufSize` have no callers outside their file and stay unexported. Delete the now-orphaned unexported `newQueueStore` **only if** `NewQueueStore` no longer delegates to it — it does (`store.go:93-95`), so keep both, unexported delegate included.

**STEP 3 — rewrite the 8 daemon production files.** Add `"github.com/gregberns/harmonik/internal/queuewiring"` to each import block and qualify at every site listed in §3b. Concretely:
- `workloop.go:567` → `queueStore *queuewiring.QueueStore`; `:1175` → `queuewiring.NewBRQueueLedger(adapter)`; `:1436`, `:1469`, `:6149` → `lq *queuewiring.LockedQueueStore`.
- `stategather.go:40`, `:60`; `draindetect.go:215`, `:234` → `*queuewiring.QueueStore`.
- `bootstate.go:37` → `qs *queuewiring.QueueStore`; `:127` → `qs = queuewiring.NewQueueStore()` (swap the unexported constructor for the exported one); `:173-174` → `queuewiring.NewQueueOperatorEventConsumer(queuewiring.QueueOperatorEventConsumerConfig{...})`. `:167` and `:261` need no text change.
- `daemon.go:406` → `QueueStore *queuewiring.QueueStore`; `:679` → `qs *queuewiring.QueueStore`.
- `quiesce.go:183` → `QueueStore *queuewiring.QueueStore`. Leave `:427/:428/:599/:602` alone.
- `bootsocket.go:163`, `:171` → `queuewiring.NewBRQueueLedger(brAdapterForHandler)`.
- `perqueuespendmeter_tigaf11.go:97`, `:117` → `*queuewiring.QueueStore` (file stays in daemon).

**STEP 4 — the one CLI site.** `cmd/harmonik/run.go:631`: `qs := daemon.NewQueueStore()` → `queuewiring.NewQueueStore()`; add the import. `:720` unchanged.

**STEP 5 — split `export_test.go`, do not empty it.**

Create `internal/queuewiring/export_test.go` (`package queuewiring`) holding **only the shims whose bodies call code that moved into unexported methods**:
```go
package queuewiring

// ExportedNewBRQueueLedger, ExportedQueueLedger  (lifted from daemon/export_test.go:2785-2794)
// ExportedQueueOpConsumerHandlePauseStatus       (lifted from :2519)
// ExportedQueueOpConsumerHandleResuming          (lifted from :2527)
```
Those four are the only ones whose sole callers move (`queueledger_bridge_hkdv8qv_test.go`, `queuestore_wakeongap_hkekj_test.go`, `queue_operatoreventconsumer_7urls_test.go`, and the moving half of `queue_perqueue_pause_tigaf6_test.go`) **and** whose bodies reach unexported methods (`c.handleOperatorPauseStatus`, `c.handleOperatorResuming`) that are no longer reachable from `package daemon`. Delete them from `internal/daemon/export_test.go`.

**KEEP in `internal/daemon/export_test.go`, re-typed to `*queuewiring.…`:**
- `:2261 ExportedNewQueueStore() *queuewiring.QueueStore { return queuewiring.NewQueueStore() }` — **41 `daemon_test` files call it and none names the type; deleting it is the difference between a bounded diff and a 60-file diff.**
- `:2295 ExportedQueueStoreOf(deps workLoopDeps) *queuewiring.QueueStore` — ~16 more files chain off it.
- `:2000 ExportedQueueStoreSetQueue(s *queuewiring.QueueStore, q *queue.Queue)` — only caller is `perqueuespendmeter_tigaf11_test.go:101`, which stays until E3b.
- `:2507 type ExportedQueueOperatorEventConsumerConfig = queuewiring.QueueOperatorEventConsumerConfig` and `:2513 var ExportedNewQueueOperatorEventConsumer = queuewiring.NewQueueOperatorEventConsumer` — **`operatornfr_pause_inflight_hk95a2r_test.go:327-329` (514 LOC, stays) needs both.** Duplicate the same two shims in `queuewiring/export_test.go` for the moved tests.
- `:184 WorkLoopDepsParams.QueueStore *queuewiring.QueueStore`, `:1963 ExportedNewPerQueueSpendMeter(reg *RunRegistry, store *queuewiring.QueueStore, projectDir string)`.

**STEP 6 — move the six clean test files.**
```bash
git mv internal/daemon/queuestore_hkj808w_test.go               internal/queuewiring/store_test.go
git mv internal/daemon/queuestore_append_lostupdate_hkb1_test.go internal/queuewiring/store_append_lostupdate_test.go
git mv internal/daemon/queuestore_namedqueues_hktigaf2_test.go   internal/queuewiring/store_namedqueues_test.go
git mv internal/daemon/queuestore_wakeongap_hkekj_test.go        internal/queuewiring/store_wakeongap_test.go
git mv internal/daemon/queueledger_bridge_hkdv8qv_test.go        internal/queuewiring/beadledger_test.go
git mv internal/daemon/queue_operatoreventconsumer_7urls_test.go internal/queuewiring/operatorevents_test.go
```
In each: `package daemon_test` → `package queuewiring_test`; drop the `internal/daemon` import; rewrite every `daemon.X` → `queuewiring.X`. All six were AST-verified to reference only `Exported*` shims plus unit-owned types — nothing else in daemon.

**STEP 7 — split `queue_perqueue_pause_tigaf6_test.go` (394 LOC).**
```bash
git mv internal/daemon/queue_perqueue_pause_tigaf6_test.go \
       internal/queuewiring/operatorevents_perqueue_pause_test.go
```
Then **cut `TestPerQueuePause_DoesNotSetGlobalFlag` (`:253-269`) back out** into a new `internal/daemon/queue_perqueue_pause_globalflag_tigaf6_test.go` (`package daemon_test`). It is the only one of the seven tests that touches `daemon.ExportedNewOperatorPauseController` and `stubEventCollector`, both of which stay. Carry the file-local helpers `perQueueFixtureBus` (`:41`), `perQueueFixtureActiveQueue` (`:50`), `perQueueFixtureConsumerWithBus` (`:72`), `perQueueFixturePauseEvent` (`:85`), `perQueueFixtureResumingEvent` (`:110`) into the moved file — the staying test uses none of them. **Do not** copy `stubEventCollector` into `queuewiring`; nothing there would use it.

**STEP 8 — leave the composition test behind, deliberately.** `internal/daemon/queue_operatoreventconsumer_composition_7urls_test.go` (85 LOC) stays untouched. Say so in the PR body: it is the EV-009 regression guard proving the extracted consumer is still subscribed before `Seal`. Losing it turns a silently-unsubscribed consumer into a green build.

**STEP 9 — fix the in-package (`package daemon`) test surface.** Add the `queuewiring` import and qualify in all six files per §3b: `draindetect_test.go` (23), `quiesce_sleep_veto_hkzqb3_test.go` (9), `eagerfill_em063_test.go` (4 + helper sig `:79`), `quiesce_test.go` (1 + helper sig `:111` + the method break below), `run_session_adoption_hk78tji_test.go` (1), `workloop_perqueue_roundrobin_hktigaf4_test.go` (1).

**STEP 9b — the one hard compile break (behavior delta; declare it).** `internal/daemon/quiesce_test.go:420-428` declares a **method on the moving type from an in-package test file**, touching unexported fields:
```go
func (s *QueueStore) setQueueForTest(name string, q *queue.Queue) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.queues == nil { s.queues = make(map[string]*queue.Queue) }
	s.queues[name] = q
}
```
After the move this is a double error (method on a non-local type; unexported-field access across packages). It cannot be mechanically requalified. **Fix:** delete the helper and change the single call site `quiesce_test.go:361` to `qs.SetQueueByName("crew-paul-queue", q)`.

This is a **behavior delta inside a PR framed as a pure move**: `SetQueueByName` (`store.go:180-188`) fires the wake channel; `setQueueForTest` deliberately did not. Verified harmless for the one affected test — `TestQuiesceArbiterQueueSubmitWakesCrew` (`quiesce_test.go:358-391`) calls `arbiter.handleQueueSubmit` directly rather than waiting on `WakeCh`, so the residual signal is never observed. **Call it out explicitly in the PR description** so the review gate does not reject it as an unexplained logic change (`_plan.md` §5.1).

**STEP 10 — fix the three external test sites.** `internal/scenario/named_queues_workers_test.go:379` and `:432`, `internal/scenario/queue_lifecycle_test.go:483`: `daemon.NewQueueStore()` → `queuewiring.NewQueueStore()`, add the import. Then `internal/daemon/reviewloop_budget_hkc1ah6_test.go:128`: return type `*daemon.QueueStore` → `*queuewiring.QueueStore` (its `ExportedNewQueueStore()` calls at `:151` and `:263` are unchanged). And `internal/daemon/perqueuespendmeter_tigaf11_test.go:96`/`:99`.

**STEP 11 — add the depguard block.** Insert **immediately after** the existing `queue:` rule (`.golangci.yml:221-226`), matching the `gitprobe`/`crew` idiom verbatim:

```yaml
        # queuewiring: the daemon-side queue OWNERSHIP + operator-transition wiring
        # extracted out of internal/daemon (P2 unit E3). The queue ENGINE is already a
        # clean leaf (internal/queue); this package holds the in-memory QueueStore that
        # satisfies queue.QueueSetter / queue.LockedQueueView / queue.MutationLocker, the
        # brcli -> queue.BeadLedger bridge, and the operator pause/resume consumer. The
        # daemon is the composition root and injects these; it MUST NOT be imported back,
        # or the queue control surface silently re-couples to the 59k-LOC monolith.
        # uuid + testify are needed by the moved external test files (depguard `files:`
        # globs match _test.go too); the self-import row is the external-test-package
        # pattern (queuewiring_test importing queuewiring) — WITHOUT it the package's own
        # tests fail depguard on day one, exactly as internal/queue's 28 pre-existing
        # violations demonstrate. Rationale: plans/2026-07-21-p2-extraction/_plan.md E3.
        queuewiring:
          files: ["**/internal/queuewiring/**"]
          allow:
            - "$gostd"
            - "github.com/google/uuid"
            - "github.com/stretchr/testify/require"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/queue"
            - "github.com/gregberns/harmonik/internal/eventbus"
            - "github.com/gregberns/harmonik/internal/brcli"
            - "github.com/gregberns/harmonik/internal/queuewiring"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "queuewiring is the extracted queue-ownership layer; daemon composes it, never the reverse (P2 E3)" }
```

Note on the `internal/queuewiring` self-import row: depguard allow entries are **prefix matches** (documented at the `bootconfig` rule, `.golangci.yml:696-706`), so `internal/queue` already prefix-covers `internal/queuewiring`. The row is kept anyway so the allow list stays readable if the `internal/queue` row is ever tightened — and because `internal/queue`'s own rule proves prefix reasoning is easy to get wrong here. The `deny` row on `internal/daemon` does **not** prefix-collide with anything in this package's closure.

No `internal/daemon` allow-list change is needed: the `daemon:` rule (`.golangci.yml:675-683`) already allows the whole `github.com/gregberns/harmonik/internal/` prefix. Verified no import cycle: `internal/{brcli,eventbus,queue}` do not import `internal/daemon` (only a prose mention at `internal/queue/types.go:312`).

**STEP 12 — verify.** Run §6 in order.

---

### E3b-PREP — a separate, daemon-only PR with ZERO file moves

Break the `RunRegistry` knot first, on its own, so the move slice stays reviewable as a move.

1. In `internal/daemon/perqueuespendmeter_tigaf11.go`, replace the field `reg *RunRegistry` (`:97`) with `queueNameForRun func(core.RunID) (string, bool)` and change `NewPerQueueSpendMeter`'s first parameter (`:117`) to match.
2. In `handleBudgetAccrual` (`:171-183`), replace
   ```go
   if m.reg == nil { return nil }
   handle, ok := m.reg.Get(payload.RunID)
   if !ok { return nil }
   queueName := handle.QueueName
   if queueName == "" { return nil }
   ```
   with
   ```go
   if m.queueNameForRun == nil { return nil }
   queueName, ok := m.queueNameForRun(payload.RunID)
   if !ok { return nil }
   if queueName == "" { return nil }
   ```
   **All three early-return edge cases must be preserved verbatim**: nil resolver → nil; run not found (late-tail chunk after `Unregister`, lossy-tail OK) → nil; empty queue name (br-ready-fallback run, accrues to the global meter only) → nil.
3. `internal/daemon/bootstate.go:167`:
   ```go
   perQueueSpendMeter := NewPerQueueSpendMeter(
       func(id core.RunID) (string, bool) {
           h, ok := bs.sharedRunRegistry.Get(id)
           if !ok { return "", false }
           return h.QueueName, true
       },
       bs.qs, cfg.ProjectDir)
   ```
4. Update `internal/daemon/export_test.go:1963` (`ExportedNewPerQueueSpendMeter`) to the new first parameter.
5. Rewrite `internal/daemon/perqueuespendmeter_tigaf11_test.go` `pqRegisterRun` (`:79-91`) and `pqSetup` (`:96-105`) to use a map-backed fake resolver instead of `daemon.NewRunRegistry` / `daemon.RunHandle` / `daemon.ExportedRunRegistryRegister`.

This is the "daemon threads effectful closures IN" idiom from `_plan.md` §0 — a closure, not a new abstraction layer. Ship it green, then:

### E3b-MOVE

```bash
git mv internal/daemon/perqueuespendmeter_tigaf11.go      internal/queuewiring/perqueuespendmeter.go
git mv internal/daemon/spendmeter_hkk3f8g.go              internal/queuewiring/spendmeter.go
git mv internal/daemon/perqueuespendmeter_tigaf11_test.go internal/queuewiring/perqueuespendmeter_test.go
git mv internal/daemon/spendmeter_hkk3f8g_test.go         internal/queuewiring/spendmeter_test.go
```
Both production files **must land in the same PR** — `perqueuespendmeter` consumes `bytesPerUSD`, `defaultDailyBudgetUSD`, `envFlywheelBudgetUSDPerDay` and `spendMeterTodayKey` from `spendmeter`, and duplicating a budget constant invites silent mis-billing drift.

Then move the eight spend-meter shims out of `internal/daemon/export_test.go` (`:1919`, `:1927`, `:1935`, `:1943`, `:1953`, `:1963`, `:1971`, `:1981`, `:1991`) and `ExportedQueueStoreSetQueue` (`:2000`) into `internal/queuewiring/export_test.go`, and update:
- `internal/daemon/daemon.go:621` → `spendMeterObserver func(*queuewiring.DaemonSpendMeter)`
- `internal/daemon/testopts_test.go:94` → `WithSpendMeterObserver(fn func(*queuewiring.DaemonSpendMeter))`
- **`internal/daemon/cognition_loop_scenario_hkc7lxc_test.go:181` and `:319`** → `daemon.WithSpendMeterObserver(func(m *queuewiring.DaemonSpendMeter) {...})`. **This file is the one the recon's E3b recipe omitted.**
- `internal/daemon/bootstate.go:158`, `:167`

Expect a further **-637 non-test LOC** out of daemon.

---

## 5. Freeze tripwire

### 5a. The deny edge (machine-checked, hard CI failure)

The `deny:` row in the `queuewiring:` block of §4 STEP 11 **is** the freeze tripwire:
```yaml
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "queuewiring is the extracted queue-ownership layer; daemon composes it, never the reverse (P2 E3)" }
```
It runs under `make check-short` in `.github/workflows/ci.yml` (merge-blocking — `continue-on-error` was dropped). Per the operator-resolved decision, this is a **hard CI failure**, not a warning.

### 5b. "No new files in daemon for this concern"

depguard cannot express "no new file matching a name pattern," so this half is a grep guard. Add a `Makefile` target and wire it into `check-short`:

```make
.PHONY: freeze-check
freeze-check:  ## P2 freeze tripwire: extracted concerns may not reopen in internal/daemon
	@bad=$$(git ls-files 'internal/daemon/*queuestore*.go' \
	                    'internal/daemon/*queueledger*.go' \
	                    'internal/daemon/*queue_operatorevent*.go' \
	                    'internal/daemon/*spendmeter*.go' \
	        | grep -v '_test\.go$$' || true); \
	if [ -n "$$bad" ]; then \
	  echo "FREEZE VIOLATION (P2 E3): queue-wiring lives in internal/queuewiring, not internal/daemon:"; \
	  echo "$$bad"; exit 1; \
	fi
```
Hook it in by adding `freeze-check` to the `check-short` and `check` target prerequisites in `Makefile` (alongside the existing `fmt-check` / lint steps). Note the `grep -v '_test\.go$'` carve-out is **required and intentional**: `internal/daemon/queue_operatoreventconsumer_composition_7urls_test.go` and `internal/daemon/queue_perqueue_pause_globalflag_tigaf6_test.go` legitimately stay behind (§1, §4 STEP 7/8).

Add the `*spendmeter*` pattern only when **E3b** lands, not in E3a — until then `internal/daemon/spendmeter_hkk3f8g.go` and `perqueuespendmeter_tigaf11.go` still exist and the guard would fail its own repo.

### 5c. Boundary test (the §3.4 pre-agreed ground rule)

The depguard deny edge IS the boundary test. Scope disputes ("is this file in E3 or not?") are settled by whether the file compiles inside `internal/queuewiring` with that allow list — not by argument. Two files failed that test at plan time and are permanently out: `dispatchsegment.go` and `socketdispatch.go` (§1).

---

## 6. Verification gate

Run in this order from the repo root. Every command must pass before the next matters.

```bash
# 1. Compiles everywhere.
go build ./internal/... ./cmd/...
```
**Pass:** exit 0, no output. This is where the `quiesce_test.go:422` method break and the ~40 bare-constructor sites surface if STEP 9/9b were skipped — note `go build` does NOT compile test files, so it will *not* catch them. See step 3.

```bash
# 2. Vet.
go vet ./internal/... ./cmd/...
```
**Pass:** exit 0.

```bash
# 3. The unit + its blast radius, including all test files.
go test ./internal/queuewiring/... ./internal/queue/... ./internal/daemon/... ./internal/scenario/... ./cmd/... -count=1
```
**Pass:** all `ok`, zero `FAIL`, zero `[build failed]`. **This is the step that catches the in-package test surface.** If `internal/daemon [build failed]` appears, STEP 9/9b is incomplete.

```bash
# 4. Whole repo — no import cycle, no broken fan-in.
go test ./... -count=1
```
**Pass:** no new failures vs. the pre-change baseline. Capture the baseline before starting; the tree has known-flaky real-daemon E2E tests that are unrelated.

```bash
# 5. Race detector — non-negotiable for this unit.
go test -race ./internal/queuewiring/... ./internal/daemon/... -count=1
```
**Pass:** zero `DATA RACE`. `QueueStore`'s read accessors return deep copies to fix a real, reproducible race (hk-ri2in.4, documented at `store.go:126-136` and `:363-372`). Only `-race` catches a regression here.

```bash
# 6. depguard green — the freeze tripwire itself.
./.tools/golangci-lint run ./internal/queuewiring/... 2>&1 | grep depguard
```
**Pass: ZERO lines of output.** `golangci-lint` is NOT on `PATH`; it lives at `./.tools/golangci-lint` (v2.3.0, installed by `make tools`, `Makefile:618`). Run `make tools` first if `.tools/` is absent.

Do **not** substitute a bare `./.tools/golangci-lint run` as the pass criterion — a full run fails on ~5,666 pre-existing legacy issues (`Makefile:544`), which is why CI uses `--new-from-rev`. For the reverse direction (nothing in daemon newly violating), also run:
```bash
./.tools/golangci-lint run --new-from-rev=origin/main
./.tools/golangci-lint run ./internal/daemon/... ./cmd/... 2>&1 | grep depguard
```
**Pass:** the first is clean; the second returns no *new* lines vs. its pre-change output.

**Sanity check that the rule is actually live** (a mis-globbed `files:` pattern silently passes everything): temporarily add `_ "github.com/gregberns/harmonik/internal/daemon"` to `internal/queuewiring/store.go`, re-run command 6, confirm it FAILS with the deny message, then revert. `internal/queue`'s own rule has **28 pre-existing depguard violations** today (self-import + `github.com/google/uuid` at `rpc.go:33` and `state.go:24`) that `--new-from-rev` masks — proof that an unexercised depguard block can sit red for months unnoticed.

```bash
# 7. Bug scanner.
ubs $(git diff --name-only HEAD) $(git diff --cached --name-only)
```
**Pass:** exit 0.

```bash
# 8. Freeze guard.
make freeze-check
```
**Pass:** exit 0.

```bash
# 9. The success metric — publish this number in the bead close comment.
find internal/daemon -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -name '*.go' ! -name '*_test.go' | xargs wc -l | tail -1
```
**Expected after E3a:** 134 → **131 files**, 58,971 → **58,153 non-test LOC (−818)**.
**Expected after E3b:** 131 → **129 files**, 58,153 → **57,516 non-test LOC (−637)**.
**Combined E3: −1,455 non-test LOC, −5 files.**

---

## 7. Risks and how each is mitigated

**R1 — `internal/daemon/quiesce_test.go:422` is a hard compile break that is NOT a typo.** A method (`setQueueForTest`) is declared on `QueueStore` from an in-package test file and reaches into `s.queueMu` / `s.queues`. After the move it is a method-on-non-local-type error *and* an unexported-field-access error. The only clean fix — `qs.SetQueueByName(...)` at `:361` — is a real behavior delta (`SetQueueByName` fires the wake channel at `store.go:186`; `setQueueForTest` deliberately did not). *Mitigation:* verified harmless for the one affected test (`TestQuiesceArbiterQueueSubmitWakesCrew` calls `arbiter.handleQueueSubmit` directly and never reads `WakeCh`), and **declared explicitly in the PR body** so the pure-move review gate does not reject it as an unexplained logic change. Neither `go build` nor `go vet` catches this — only `go test`.

**R2 — the recon's "~20-file diff" framing is wrong; budget for ~25 files and ~60 edit sites.** Six `package daemon` test files call the bare identifier `NewQueueStore()`/`newQueueStore()` (~40 sites: `draindetect_test.go` 23, `quiesce_sleep_veto_hkzqb3_test.go` 9, `eagerfill_em063_test.go` 4, plus 1 each in three more). No `export_test.go` shim can rescue them — the bare identifier ceases to exist in `package daemon`. Two more in-package helper signatures are typed on the moving type (`eagerfill_em063_test.go:79`, `quiesce_test.go:111`). *Mitigation:* all enumerated in §3b and STEP 9; verification step 3 (not step 1) is the gate that proves it.

**R3 — moving the wrong `export_test.go` shim breaks a test that stays.** Two live examples: `ExportedQueueStoreSetQueue` (only caller `perqueuespendmeter_tigaf11_test.go:101`, which stays until E3b) and the pair `ExportedNewQueueOperatorEventConsumer` / `ExportedQueueOperatorEventConsumerConfig` (used by the 514-LOC `operatornfr_pause_inflight_hk95a2r_test.go:327-329`, which stays permanently). *Mitigation:* STEP 5 splits the shims by **who calls them**, not by what they wrap, and lists the exact keep/move sets. Duplicating the two consumer shims across both `export_test.go` files is correct, not redundant.

**R4 — `_plan.md`'s E3 file list is 2/6 wrong, and both errors point the same way.** A filename containing "dispatch" was read as *queue* dispatch. `dispatchsegment.go` is the `runexec` Dispatch state machine (E5); `socketdispatch.go` is the socket router table for all 27 ops. An implementer who trusts the list burns a day. *Mitigation:* §1 lists both under STAYS with the evidence; **edit `_plan.md` §2 before staffing anyone.** `_plan.md`'s advice "leave `dispatchsegment`/`socketdispatch` for the tail where it abuts E5" is wrong in kind, not just in ordering.

**R5 — a new depguard block can be born dead.** `files:` globs match `_test.go` too. The moved external tests import `github.com/google/uuid` (3 files) and `github.com/stretchr/testify/require` (1 file), and the external-test-package pattern requires a self-import row. Omit any of these and the package is red on day one. **Empirical proof this happens:** `internal/queue`'s own rule has 28 live depguard violations today, masked by `--new-from-rev`. *Mitigation:* the allow list in STEP 11 covers all four; verification step 6 runs `golangci-lint` **scoped to the package explicitly**, never via `--new-from-rev`, and includes a deliberate-violation sanity check.

**R6 — a "simplification" during the move reintroduces a data race.** `QueueStore`'s read accessors return deep copies (`cloneQueue`/`cloneGroup`/`cloneItem`) to fix a real, reproducible race documented at `store.go:126-136`. Returning the live pointer looks like an obvious cleanup and only fails under load. *Mitigation:* preserve the clone helpers byte-for-byte; verification step 5 (`-race`) is mandatory, not optional.

**R7 — the split test file breaks the "pure `git mv`" review framing.** `queue_perqueue_pause_tigaf6_test.go` cannot move whole: one of its seven tests asserts on `OperatorPauseController`, which stays. *Mitigation:* STEP 7 names the exact function and its new home; call the split out in the PR description alongside R1 so the review gate sees two declared deltas and nothing else.

**R8 — losing the composition test turns a broken wiring into a green build.** `queue_operatoreventconsumer_composition_7urls_test.go` is the only assertion that the extracted consumer is still subscribed to the bus before `Seal` (EV-009). A tidy-minded implementer will see it "belongs with the consumer" and move it — where it cannot reach `daemon.Start`. *Mitigation:* STEP 8 makes leaving it behind an explicit, justified step, not an omission.

**R9 — E3b changes a public-ish test hook signature.** `daemon.go:621` exposes `spendMeterObserver func(*DaemonSpendMeter)`, surfaced as `daemon.WithSpendMeterObserver` (`testopts_test.go:94`). Moving `DaemonSpendMeter` changes that signature. The real blast radius is **one file, two sites** (`cognition_loop_scenario_hkc7lxc_test.go:181`, `:319`) — smaller than the recon claimed, but the recon's E3b recipe never listed the file at all. *Mitigation:* enumerated in E3b-MOVE.

**R10 — `RunRegistry` is the single symbol gating half the unit.** Without the closure port, `perqueuespendmeter_tigaf11.go` and its 275-LOC test are stuck in daemon, and `spendmeter_hkk3f8g.go` is stuck with them. *Mitigation:* E3b-PREP is a self-contained daemon-only PR with zero file moves; it can be reviewed and merged independently, and **E3a does not wait on it.**

**R11 — `golangci-lint` is not on `PATH`.** `which golangci-lint` fails on this box. *Mitigation:* it is at `./.tools/golangci-lint` (v2.3.0, `Makefile:618`); every lint command in §6 uses that path, and `make tools` restores it if `.tools/` is missing. This is a path correction, not a missing prerequisite.

---

## 8. Rollback

**During E3a, before the PR merges.** The unit is a `git mv` plus mechanical requalification; nothing is destructive.
```bash
git checkout -- .golangci.yml Makefile
git rm -r --cached internal/queuewiring
rm -rf internal/queuewiring
git checkout HEAD -- internal/daemon internal/scenario cmd/harmonik
git status   # must be clean w.r.t. this unit
go build ./... && go test ./internal/daemon/... -count=1
```
Because every production move is a `git mv` with no content edit beyond the `package` clause, `git checkout HEAD -- internal/daemon` restores the three files at their original paths and contents.

**After E3a merges, if a defect surfaces.** Do **not** hand-revert. `git revert` the merge commit as a whole — the unit is one bead = one release (`_plan.md` §5.6), so the revert is atomic and the depguard block, the `Makefile` guard, the moves and the requalifications all come back together. Reverting a subset leaves `internal/queuewiring` present with a dangling depguard rule and a half-requalified daemon.

**Partial-progress rule.** If E3a stalls mid-flight, the only safe intermediate resting points are: (a) nothing moved, or (b) all three production files + all six clean test files moved and green. **Do not park with the production files moved and the in-package `package daemon` test surface unfixed** — `go build ./...` passes in that state while `go test` does not, so the tree looks healthy and is not.

**E3b abandonment.** E3b-PREP and E3b-MOVE are separate PRs by design. If E3b-MOVE goes wrong, revert it alone; E3b-PREP (the `RunRegistry` → closure port) is a strict improvement on its own — it removes a run-registry dependency from the budget meter regardless of whether the file ever leaves `internal/daemon` — and should be kept.
