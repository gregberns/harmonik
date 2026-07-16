# M5 Slice 3 — `internal/orchestrator` extraction: design

> ## ⚑ Independent seam review addendum (2026-07-16) — 5 MANDATED FIXES before implementing
> An adversarial read-only review verified the design against source. Verdict: **safe to implement as-sequenced 3A→3B→3C, but not as-written.** Apply these before/during each sub-slice:
> 1. **3A:** project `ItemSnapshot.ItemIdx` as the **absolute index into `Group.Items`** (matching the `j` at `workloop.go:1515-1517`), NOT an index into the eligible sub-slice — silent-wrong-answer risk. And **retain** the Phase-3 claim-time re-validation (`workloop.go:2519-2558`) as the load-bearing guardrail. (The `LockedQueueByName(chosen)` re-resolve at `1499-1503` is confirmed collapsible — it runs under the same uninterrupted lock — but the Phase-3 re-check must stay.)
> 2. **3A gate correction / drop cruft:** the design mislabels the global gate. The selector reads **only** `reg.LenForQueueLocal` vs `WorkerCap` (`workloop.go:1463`); it never reads `reg.Len()`. The Step-2 gate is `deps.localInFlight.Load() >= gateMax` (`1812-1816`), a different counter/cap. So **remove `RegistrySnapshot.TotalInFlight` and `FleetSnapshot.GlobalCap` from the 3A struct** (move to 3B if eager-fill needs them). `GroupSnapshot.Kind`/`PendingCount` are 3B inputs — reserve for 3B, not 3A.
> 3. **3C:** downgrade `PlanGroupAdvance` from a monolithic planner to a **few micro-predicates** wrapped around the two in-place `queue.AdvanceGroup` effect calls (`workloop.go:6159`, `6181`). The classification is mutate→classify→mutate→classify; it cannot be one pre-pass over a status vector. Keep all `bus.Emit`/event ordering (`6187`, `6270-6276`) daemon-side. Treat the 167→120 shave as aspirational.
> 4. **Scope:** `startWithHooks`/`handleSocketConn` are **CONFIRMED out of scope** for an orchestrator extraction (boot/wiring + protocol dispatch; no work-loop brain). Surfaced to operator for M5-closure adjudication.
> 5. **Depguard:** un-comment + trim the orchestrator rule to `$gostd + core` as part of 3A; do NOT allow `internal/queue`.



> Read-only design pass (Plan agent, 2026-07-16). Template = slice 2 (`internal/policy`, `M5-SLICE2-POLICY-DESIGN.md`): pure predicates over narrow snapshot structs projected at the daemon call site; imports `$gostd + internal/core` only. Discipline template also = `internal/mergeq` / `internal/runexec` (daemon threads effects IN; subsystem never imports daemon back).

## 0. Depguard reality check (do this first)

- `.golangci.yml:579-588` orchestrator rule is **commented, not active**, and allows `$gostd, core, eventbus, policy, handler/contract, workspace, hook, adapter/br`.
- The brief's stated allow-list (`core, eventbus, memory`) is **inaccurate**: no `internal/memory` package exists (`ls internal/` confirms), and `eventbus` is not needed by any pure fn below.
- **Design target:** keep `internal/orchestrator` to `$gostd + internal/core` exactly like `internal/policy`. Every pure fn below satisfies that. Recommend un-commenting the rule but TRIMMING it to `$gostd + core` (+ `policy` only if sub-slice 3C reuses `policy.DrainSnapshot`). Broader allow-list invites leakage. **Flag for sign-off**: whoever activates the rule must reconcile the brief vs the file.

## 1. Seam map (file:line → pure decision vs effect)

### 1a. `effectiveQueueWorkers` — `workloop.go:1395`
- **Pure, trivial.** Already a one-liner delegating to `queue.DefaultWorkers(q.Workers, globalCap)`. It reads only two ints.
- **Move:** the *logic* is already in `internal/queue`. Do NOT re-home; instead the orchestrator selector takes the resolved ceiling as a snapshot field (`WorkerCap int`), computed by the daemon via `queue.DefaultWorkers` at projection time. Keeps orchestrator from importing `internal/queue` (which is NOT on the allow-list).

### 1b. `selectNextQueue` — `workloop.go:1429-1537` (queueSelection type `workloop.go:1372`)
- **Pure decision (→ orchestrator):** the entire candidate-filter + lexicographic sort + round-robin cursor + first-eligible-item pick. This is the crown jewel of the slice. Inputs it actually reads: per-queue {name, status==Active, blocked?, localInFlight, workerCap, active-group-index, eligible-item list}, plus `globalCap` (enforced by caller — selector reads it only via the caller gate), `rrCursor`, `blockedQueues`.
- **Effect (stays in daemon shell):** everything touching `*LockedQueueStore` / `*RunRegistry` — `lq.LockedAllQueueNames()`, `lq.LockedQueueByName()`, `reg.LenForQueueLocal()`, `queue.EligibleItems()`. These are lock-holding reads. The daemon **projects** them into a snapshot BEFORE calling the pure selector, while holding the write lock.
- **Subtlety:** current code re-resolves `q := lq.LockedQueueByName(chosen)` after picking, to survive a racing clear. In the pure model there is no re-resolve; the snapshot is a consistent point-in-time capture taken under the lock, so the racing-clear guard collapses into "snapshot was taken under lock → internally consistent." The daemon still re-validates queueID at dispatch time downstream (it already does in `evaluateGroupAdvanceWithOutcome`).

### 1c. group-advance — `evaluateGroupAdvanceWithOutcome` `workloop.go:6119-6285`
- **Pure decision (→ orchestrator):** the *state transition arithmetic* — given (current group statuses, the just-completed item's success flag): which group becomes terminal, whether queue→paused-by-failure, which pending group activates next, whether all-groups-succeeded (the CompleteAndUnlink trigger). This is `queue.AdvanceGroup` (already pure, in `internal/queue`) PLUS the *orchestration wrapper* around it: the "activate next pending group," "allSucceeded" scan, and "which terminal action to take" decision.
- **Effect (stays):** `lq.LockForMutation`, item status mutation, `queue.Persist`, `queue.CompleteAndUnlink`, `ClearQueueByName`, `bus.Emit`, `cancelOnQueueDrain/Exit`, `queueStore.Wake`, `eagerRefillEval`.
- **Recommended pure fn:** `orchestrator.PlanGroupAdvance(snapshot, itemIdx, success) → GroupAdvancePlan{ItemStatus, NextGroupToActivate int, QueuePausedByFailure bool, AllSucceeded bool, ...}`. Note `queue.AdvanceGroup` itself stays where it is (it needs `queue.*` types); the orchestrator plan operates on a *projected* group-status vector, and the daemon applies `AdvanceGroup` per the plan. **This is the most-coupled edge** (see §7) — the transition logic is entangled with `queue.Group` mutation and event emission; the clean pure harvest here is the "next-group-activation + terminal-classification" decision, not the whole function.

### 1d. eager-fill DECISION — `eagerRefillEval` `eagerfill_em063.go:72-199`
- **Pure decision (→ orchestrator):**
  - *deficit computation*: `available = maxConcurrent - inFlight; deficit = available - pendingCount` and the "which active stream group has a deficit" selection (`eagerfill_em063.go:92-135`).
  - *overfetch limit*: `limit = deficit * overfetchFactor` (`:143`).
  - *take-count clamp*: `survivors[:deficit]` (`:160`).
- **Effect (stays):** `LockForMutation`, `kerfNextBeads` (exec), `preScreenCandidates` (git + ledger I/O), `queue.AppendItems`, `queue.Persist`, `queueStore.Wake`.
- **Recommended pure fns:**
  - `orchestrator.EagerFillTarget(snapshot, maxConcurrent, inFlight) → (target FillTarget, ok bool)` — picks the stream group + deficit.
  - `orchestrator.OverfetchLimit(deficit int) → int` (holds `overfetchFactor=2`).
  - `orchestrator.ClampSurvivors(survivors []core.BeadID, deficit int) → []core.BeadID`.

### 1e. pre-screen DECISION — `preScreenCandidates`/`buildInQueueSet` `eagerfill_em063.go:222-285`
- **Pure decision (→ orchestrator):** the Phase-1 set-membership filter: "drop candidate if already in the in-queue set." `buildInQueueSet` builds the set from queue items with status ∈ {pending, dispatched, completed, failed}.
- **Effect (stays):** the lock-held walk that BUILDS the set; `beadLandedOnOriginMain` (git exec — Phase 2); `emitStaleOpenBeadDetected` (bus).
- **Recommended:** `orchestrator.ScreenAlreadyQueued(candidates []core.BeadID, inQueue map[core.BeadID]struct{}) []core.BeadID`. The daemon builds `inQueue` under lock (effect), passes it in. Phase-2 git stays a daemon loop calling the pure filter first.

### 1f. `runWorkLoop` — `workloop.go:1539-3105` (1566 lines)
- **Almost entirely effect-shell**: goroutine lifecycle, semaphores, ctx wiring, mergeq ownership, `exitClean`, timers, dispatch of `beadRunOne`. The pure harvest is the **per-tick dispatch decision**, which is exactly `selectNextQueue` (1b) + the eager-fill decisions (1d) already extracted. The residual loop KEEPS all concurrency machinery. Do NOT attempt to move loop control flow.
- **One additional pure candidate:** the round-robin cursor advance (`rrCursor` increment/wrap) is arithmetic — but it's a single `rrCursor++`; not worth a fn. Leave inline.

### 1g. drain-detect DECISION — `draindetect.go`
- **Already extracted in slice 2 (sub-slice B2 LANDED):** `policy.ClassifyDrain(drainSnapshot(facts))` at `draindetect.go:504`, `DrainSnapshot` projection at `:519`. **No orchestrator work here.** `GatherDrainFacts` remains the effect fact-gatherer. Slice 3 does NOT touch drain classification — it's done. (Brief lists "drain-detect DECISIONS" but they already left the shell into `internal/policy`.)

## 2. Snapshot types (exact structs, `internal/orchestrator`)

Mirror slice-2 `DrainSnapshot` narrowness: scalars + slices only, constructible from daemon state under the lock, carrying ONLY what the pure fns read. All fields use `core.BeadID` (allow-listed) or stdlib types — never `queue.*`.

```go
// QueueSnapshot is one queue's point-in-time dispatch-relevant state,
// projected under the QueueStore write lock at the top of a dispatch tick.
type QueueSnapshot struct {
    Name        string       // map key (already normalised)
    QueueID     string       // staleness guard downstream
    Active      bool         // q.Status == QueueStatusActive
    Blocked     bool         // blockedQueues[name] (dashboard forcing-gate)
    LocalInFlight int        // reg.LenForQueueLocal(name)
    WorkerCap   int          // queue.DefaultWorkers(q.Workers, globalCap) — precomputed
    LocalOnly   bool         // mirrors Queue.LocalOnly
    WorkerTarget string      // mirrors Queue.WorkerTarget
    ActiveGroup *GroupSnapshot // first active group, or nil
}

// GroupSnapshot is the active group's eligible-item head + identity.
type GroupSnapshot struct {
    GroupIndex int
    Kind       string          // "wave" | "stream" (string, not queue.GroupKind)
    Eligible   []ItemSnapshot  // queue.EligibleItems projected, order-preserved
    PendingCount int           // count of ItemStatusPending items (for eager-fill deficit)
}

// ItemSnapshot is the minimal per-item projection the selector returns as its pick.
type ItemSnapshot struct {
    ItemIdx        int          // absolute index into Group.Items (for write-back)
    BeadID         core.BeadID
    Context        string
    WorkflowMode   string
    WorkflowRef    string
    TemplateParams map[string]string
}

// RegistrySnapshot carries the two global counters the caller gate reads.
type RegistrySnapshot struct {
    TotalInFlight int  // reg.Len()   — global-cap gate
    // per-queue local counts already folded into QueueSnapshot.LocalInFlight
}

// FleetSnapshot bundles the tick input.
type FleetSnapshot struct {
    Queues    []QueueSnapshot   // one per loaded queue
    Registry  RegistrySnapshot
    GlobalCap int
    RRCursor  int
}

// Selection is the pure selector result (replaces daemon queueSelection).
type Selection struct {
    QueueName, QueueID string
    GroupIndex         int
    Item               ItemSnapshot
    LocalOnly          bool
    WorkerTarget       string
    SawNonContributing bool  // ← anyPausedOrEmpty
}

// GroupAdvancePlan (§1c) — pure transition result.
type GroupAdvancePlan struct {
    ItemStatus            string // "completed" | "failed"
    QueuePausedByFailure  bool
    NextGroupToActivate   int    // -1 = none
    AllSucceeded          bool
    // events/persist decisions stay in daemon; this carries only the classification
}

// FillTarget (§1d).
type FillTarget struct {
    QueueName, QueueID string
    GroupPos           int
    Deficit            int
}
```

**Projection point:** a single daemon-side `func snapshotFleet(lq *LockedQueueStore, reg *RunRegistry, globalCap, rrCursor int, blocked map[string]bool) orchestrator.FleetSnapshot` — the sole place `queue.*` → snapshot mapping lives (mirrors `drainSnapshot` at `draindetect.go:519`). Built under the lock, released before the pure call is used to mutate.

## 3. Proposed package API (`internal/orchestrator`)

```go
package orchestrator // imports: $gostd + internal/core ONLY

// Queue selection (§1b) — replaces selectNextQueue's pure core.
func SelectNextQueue(f FleetSnapshot) (Selection, bool)

// Per-queue worker ceiling is NOT here — daemon precomputes WorkerCap via
// queue.DefaultWorkers into the snapshot (avoids importing internal/queue).

// Group advance (§1c).
func PlanGroupAdvance(groupStatuses []string, targetGroupPos, itemIdx int, success bool) GroupAdvancePlan

// Eager-fill decisions (§1d).
func EagerFillTarget(f FleetSnapshot, maxConcurrent, inFlight int) (FillTarget, bool)
func OverfetchLimit(deficit int) int      // deficit * 2
func ClampSurvivors(survivors []core.BeadID, deficit int) []core.BeadID

// Pre-screen decision (§1e).
func ScreenAlreadyQueued(candidates []core.BeadID, inQueue map[core.BeadID]struct{}) []core.BeadID

// doc.go — purity contract mirroring hook/doc.go + policy/doc.go.
```

## 4. Import-boundary check (per fn)

| Fn | Reads | Needs beyond `$gostd`+`core`? |
|---|---|---|
| `SelectNextQueue` | snapshot structs, `sort.Strings` | No. `core.BeadID` only. ✅ |
| `PlanGroupAdvance` | `[]string` statuses, ints | No — status strings, not `queue.GroupStatus`. ✅ |
| `EagerFillTarget` | snapshot, ints | No. ✅ |
| `OverfetchLimit`/`ClampSurvivors` | ints, `[]core.BeadID` | No. ✅ |
| `ScreenAlreadyQueued` | `[]core.BeadID`, map | No. ✅ |

**Flag:** the boundary holds ONLY if snapshots use *strings* for `queue.QueueStatus`/`GroupKind`/`ItemStatus` rather than the `queue.*` typed enums. If you keep the typed enums you pull `internal/queue` into orchestrator — NOT on any allow-list. Decision: **project enums to strings at the daemon boundary** (as `QueueItemFact.Status` already does in `draindetect.go:435`). Precedent exists.

## 5. Giant-retirement plan

| Giant | file:line | current lines | post-slice ceiling |
|---|---|---|---|
| `runWorkLoop` | `workloop.go:1539-3105` | **1566** | still grandfathered; realistic target ~1450 after selector+eager-fill decisions leave. It will NOT drop under funlen=100 — it is irreducibly an effect loop. Honest note: this slice shaves *cyclomatic/cognitive* load on the dispatch decision, not raw length. |
| `evaluateGroupAdvanceWithOutcome` | `workloop.go:6119-6285` | **167** | ~120 after `PlanGroupAdvance` absorbs the next-group/all-succeeded/paused classification (~35 lines of branching → one call + apply). |
| `startWithHooks` | `daemon.go:774-2390` | **1617** | **UNCHANGED by slice 3.** It is boot/validation/wiring (pidfile, branching.yaml, config validation, registry bootstrap) — no queue/dispatch *decision* logic lives in it. Its shave belongs to a boot-config subsystem (future slice), not orchestrator. Do not claim it. |
| `handleSocketConn` | `socket.go:387-807` | **420** | pure harvest is thin: the envelope-discrimination decision (`raw["type"]` present & len>2 → hook-relay vs op-based, `socket.go:403`) is a 1-line predicate candidate — but it's request-routing (protocol dispatch, not work-loop brain). **Recommend: leave handleSocketConn to a future socket/router slice.** The big `switch req.Op` is a dispatch table of effect handlers. Flag this expectation mismatch. |

**Net:** the real, defensible giant shave this slice delivers is on `evaluateGroupAdvanceWithOutcome` (cognit) and the *dispatch decision* embedded in `runWorkLoop` (cyclop on the tick). `startWithHooks` and `handleSocketConn` do not shrink from an orchestrator extraction; claiming otherwise would be dishonest. If their shrink is a hard requirement, they need their own subsystems (boot-config; socket-router) — out of scope for `internal/orchestrator`.

## 6. Sub-slices (independently committable, ordered)

**3A (FIRST, lowest risk): queue selection.**
`orchestrator.SelectNextQueue` + `QueueSnapshot/GroupSnapshot/ItemSnapshot/FleetSnapshot/Selection` + daemon `snapshotFleet` projection. Rewrite `selectNextQueue` (`workloop.go:1429`) as a thin shell: lock → project → call → return. Migrate the selector truth-table tests (`workloop_perqueue_roundrobin_hktigaf4_test.go`, `regression_golden_no_selection_hkhwwlk_test.go`, `workloop_sibling_starvation_hkr9edj_test.go`) to `package orchestrator_test`; keep lock/registry-integration tests in `package daemon`. Proves the package + hard `$gostd+core` edge. Green-verifiable alone.

**3B (second): eager-fill + pre-screen decisions.**
`EagerFillTarget`/`OverfetchLimit`/`ClampSurvivors`/`ScreenAlreadyQueued`. Rewrite `eagerRefillEval` (`eagerfill_em063.go:72`) and `preScreenCandidates` (`:222`) as shells calling the pure fns; keep kerf-exec, git Phase-2, AppendItems, Persist. Split `eagerfill_em063_test.go`: deficit/overfetch/clamp/screen truth-tables → `orchestrator_test`; guard-gate + git + append integration stays `daemon`.

**3C (third, highest coupling): group-advance plan.**
`PlanGroupAdvance`. Rewrite the classification block inside `evaluateGroupAdvanceWithOutcome` (`workloop.go:6178-6205`) to call the pure planner; keep all mutation/persist/emit/wake. Its own review — this is the entangled edge (§7). Migrate `workloop_hk45ude_test.go` (EM-015f gate) truth-table portions.

Order rationale: 3A is self-contained and the highest-value brain; 3B depends on nothing in 3A but is I/O-adjacent; 3C last because the transition logic resists a clean cut and needs careful concurrency review. **STOP + review between each**, per slice-2 discipline.

## 7. Risks

- **Snapshot consistency vs the lock (3A, most-coupled edge):** `selectNextQueue` currently reads the live `*LockedQueueStore` *while holding the write lock*, including a defensive re-resolve after picking. Moving to a projected snapshot means the projection MUST be a single atomic capture under `LockForMutation`, and the daemon MUST NOT release the lock between snapshot and dispatch-claim if it relies on the pick still being valid. Mitigation: keep the existing downstream `QueueID` staleness re-check at claim time (already present); the snapshot only needs to be internally consistent, not still-current.
- **`PlanGroupAdvance` entanglement (3C):** the next-group-activation calls `queue.AdvanceGroup` *again* (`workloop.go:6181`) mutating the next group — the pure planner can only decide *which* group to activate; the daemon still calls `AdvanceGroup` to perform it and collect events. The pure/effect line is subtle (decide vs apply). Risk of splitting the event-emission ordering. Keep events entirely daemon-side.
- **Concurrency hazards (this is the work loop):** `rrCursor`, `heldEventDedup`, `dashboardBlockedQueues`, `claimSkipInProgressUntil`, `readyPathAttempts` are all owned single-goroutine loop state (`workloop.go:1595-1642`). The pure selector must NOT capture these by reference — pass `rrCursor` by value into `FleetSnapshot`, return the advance decision, let the loop own the increment. `selectNextQueue` is called *under the QueueStore write lock*; the pure fn must be lock-free and side-effect-free so the lock window stays tight (projection copies out, pure fn runs, lock can release). Do NOT let the pure fn hold references into `queue.Queue` (they'd be read without the lock later).
- **`LenForQueueLocal` vs `Len` asymmetry (hk-4tjt6, `workloop.go:1463`):** the per-queue cap counts LOCAL runs only; the global cap counts all. The snapshot must faithfully carry BOTH (`QueueSnapshot.LocalInFlight` + `RegistrySnapshot.TotalInFlight`). Easy to conflate → all-remote-queue starvation regression. Cover with a migrated test.
- **Expectation mismatch (surface to operator):** brief expects `startWithHooks` and `handleSocketConn` to "shrink under their ceilings" from THIS slice. They won't — neither contains work-loop/queue-selection brain. Their shrink needs separate boot-config / socket-router subsystems. Confirm scope before implementing, or the slice will be judged incomplete against a wrong yardstick.
- **Depguard activation:** the orchestrator rule is commented and its allow-list disagrees with the brief. Activating it wrong (e.g. allowing `internal/queue`) would silently permit the boundary violations this design avoids. Un-comment + trim to `$gostd + core` as part of 3A.
