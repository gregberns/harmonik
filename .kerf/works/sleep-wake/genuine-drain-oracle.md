# Genuine-Drain Oracle — Design (hk-rl4b Fleet Sleep/Wake)

Status: DESIGN. Read-only research grounded. No production code touched.
Author: sleep-wake design pass, 2026-06-17.

The oracle is the load-bearing piece. SLEEP fires ONLY when work is TRULY drained.
A false "no work" sleep stalls the fleet with ready beads pending — the #1 failure mode.
FAIL-SAFE: if the oracle is UNSURE → return NOT-drained (do not sleep).

---

## 1. Contract

```
DrainState = DRAINED | HAS_WORK | UNSURE
```

`GenuineDrain(ctx) -> (DrainState, evidence)`.

`DRAINED` iff ALL of the following hold, each rigorously:

1. **All named queues terminal/empty** — every queue (main + crew named queues)
   is `completed`/`cancelled` OR has zero non-terminal items, AND no queue is in a
   `paused-by-*` state, AND no archived `.failed-*` / `.cancelled-*` queue file is
   un-reconciled.
2. **Every OPEN epic's ready children == 0** — accounting for ledger-dep gating:
   children deferred-for-ledger-dep that would become eligible if their blocker
   closed count as HAS_WORK (they are pending work, not drained).
3. **No in-flight runs** — `RunRegistry.Len() == 0` across all queues, AND no live
   worktree under `.harmonik/worktrees/` with an active run.
4. **br-ready is empty under FULL enumeration** — `br ready --limit 0` returns zero
   dispatchable beads (the daemon's current call omits `--limit`, so the oracle MUST
   NOT reuse that paginated path verbatim — see §3.1).

Any check that cannot be evaluated with confidence (br exec error, RPC timeout,
ledger lookup error, unreadable queue file) → `UNSURE` → caller treats as NOT-drained.

---

## 2. Where it lives

**`internal/daemon`**, as a daemon-side function, NOT `internal/queue`.

Rationale (code-grounded):
- It must read `RunRegistry` (`internal/daemon/runregistry.go:107`, `Len()`/`LenForQueue()`)
  — in-memory daemon state, not exposed via RPC.
- It must read `QueueStore.AllQueues()` (`internal/daemon/queuestore_hkj808w.go:199`)
  — the live multi-queue map the daemon holds.
- It must invoke `br ready` via `brcli.Adapter` and the `BeadLedger` bridge
  (`internal/daemon/queueledger_bridge.go`), both daemon-wired.
- `internal/queue` is the pure state-machine package (no br, no registry). Putting the
  oracle there would force it to depend on daemon-only seams it cannot reach.

Proposed: `internal/daemon/draindetect.go` exposing
`func (d *Daemon) GenuineDrain(ctx context.Context) (DrainResult, error)`
where `DrainResult{ State DrainState; Reasons []string; CheckedAt time.Time }`.

The queue-side primitives it reuses should be PROMOTED to exported helpers in
`internal/queue/state.go` (currently private): `GroupIsTerminal`, `ItemIsTerminal`,
`AllItemsTerminal`. A thin exported `Queue.HasPendingWork() bool` on the queue type is
the cleanest reuse seam (counts non-terminal items incl. deferred-for-ledger-dep).

---

## 3. The five false-negative defenses (each → a concrete check)

### 3.1 `br ready` pagination → use `--limit 0`
- **Source:** `internal/brcli/ready.go:91` — `a.runFormatJSON(ctx, "ready", "--sort", brReadySortPriority)`.
  NO `--limit` passed → inherits br's default pagination. Within one poll the daemon
  sees only the first page; the next tick fetches more, so DISPATCH eventually drains,
  but a single-snapshot "is it empty?" read is unsafe.
- **Defense:** the oracle calls a NEW adapter method `ReadyAll(ctx)` =
  `br ready --limit 0 --format json --sort priority` (or paginate with offset until a
  short page). Empty result from the FULL set is required; a default-paginated empty is
  NOT trusted. Matches the operator memory rule: "always `br ready --limit 0` before
  declaring a lane empty."

### 3.2 Ledger-dep gating (children of OPEN epics) → count deferred + open-blocked
- **Source:** `internal/queue/state.go:290` `streamEligible` skips
  `ItemStatusDeferredForLedgerDep`; `state.go:361` `ReevaluateDeferred` un-defers when
  blockers resolve; `internal/queue/validation.go:144` `BeadLedger.BlocksEdge` /
  `LookupStatus`; bridge `internal/daemon/queueledger_bridge.go` (runs `br dep list` /
  `br show`).
- **Defense (two parts):**
  (a) **In-queue:** any item in `ItemStatusDeferredForLedgerDep` ⇒ HAS_WORK. Do NOT
      treat deferred as terminal (`itemIsTerminal` correctly excludes it — reuse that).
  (b) **In-ledger:** for every OPEN epic E (from `br list --status=open --type=epic`),
      count children C where `BlocksEdge(E, C)` is true AND C is itself otherwise ready
      (open, no other open blockers). Those are pending work hidden behind an open epic.
      If that count > 0 ⇒ HAS_WORK. This is the rigorous version of "every OPEN epic's
      ready children == 0."

### 3.3 Paused-by-failure queues → a paused/archived queue is STUCK, not drained
- **Source:** `internal/queue/types.go:51` `QueueStatusPausedByFailure`,
  `:55 PausedByDrain`, `:66 PausedByBudget`. Archive: `persistence.go:289-305` renames a
  failed queue file to `<name>.json.failed-<ts>`; `EnumerateQueueNames`
  (`persistence.go:463-485`) FILTERS OUT `.failed-*` / `.cancelled-*` / `.tmp-*`.
- **Defense:** treat ANY queue whose status is `paused-by-failure` / `paused-by-drain` /
  `paused-by-budget` as HAS_WORK (stuck ≠ drained). Additionally, scan
  `.harmonik/queues/*.json.failed-*` directly (bypassing the enumeration filter): an
  un-reconciled failed-archive ⇒ HAS_WORK (it has failure-recovery work pending). This
  is the check the daemon's normal enumeration deliberately hides.

### 3.4 In-flight runs → RunRegistry + worktree cross-check
- **Source:** `internal/daemon/runregistry.go:148` `Len()`, `:163 LenForQueue`; live
  worktrees under `.harmonik/worktrees/<run_id>`.
- **Defense:** `RunRegistry.Len() == 0` AND `git worktree list` shows no
  `.harmonik/worktrees/*` run dir with an in-progress run. The worktree cross-check
  guards a registry-vs-disk skew (a run mid-teardown). Either non-zero ⇒ HAS_WORK.

### 3.5 kerf-next-empty false-negative → never trust kerf for drain
- **Source:** kerf is invoked only via opt-in eager-refill
  (`internal/daemon/eagerfill_em063.go:328` `kerf next --format=json --only=bead`); it is
  EXTERNAL and may report empty for works lacking `bead_filter` (known beta bug).
- **Defense:** the oracle MUST NOT consult `kerf next` at all. `br ready --limit 0` is
  the authoritative dispatchable-work source; kerf-empty is never sufficient evidence of
  drain. (Documented as an explicit non-input.)

---

## 4. Fail-safe (the load-bearing invariant)

Every check is fail-CLOSED toward HAS_WORK:
- br exec error / non-zero exit / schema mismatch → `UNSURE` → NOT-drained.
- `BeadLedger` lookup error (`br dep list` / `br show` failure) → `UNSURE`.
- Unreadable queue file / RPC timeout reading `AllQueues` → `UNSURE`.
- Any queue in `active` with no eligible item BUT a non-terminal group (the rare
  "all items terminal, status not yet rolled" race) → `UNSURE`, not DRAINED.

The caller (the daemon quiesce-arbiter) sleeps ONLY on `DRAINED`. `UNSURE` and
`HAS_WORK` both keep the fleet awake. This makes the dangerous direction (false sleep)
require positive evidence of emptiness on EVERY axis; the safe direction (stay awake)
is the default on any doubt.

A second-layer fleet failsafe (independent of the oracle) lives in the wake design:
a max-sleep-duration auto-wake bounds any oracle false-positive to one sleep window.

---

## 5. RED-then-GREEN test plan

Build+test FIRST. The failing test (RED) asserts the oracle refuses to declare DRAINED
in each false-negative scenario, then the implementation makes them GREEN.

Test package: `internal/daemon` (needs registry + queuestore + a fake br adapter).

RED assertions — `TestGenuineDrain_*`, each must FAIL before impl:
1. `PaginatedReadyHidesWork` — fake br returns empty on default `ready` but non-empty on
   `ready --limit 0`; oracle MUST NOT return DRAINED. (Asserts §3.1.)
2. `DeferredLedgerDepItemIsWork` — a queue with one `deferred-for-ledger-dep` item, all
   else terminal; oracle MUST return HAS_WORK. (Asserts §3.2a.)
3. `OpenEpicWithReadyChild` — `br list --status=open --type=epic` returns E;
   `BlocksEdge(E,C)` true; C open; oracle MUST return HAS_WORK. (Asserts §3.2b.)
4. `PausedByFailureIsStuck` — a queue status `paused-by-failure`; oracle MUST return
   HAS_WORK. (Asserts §3.3.)
5. `FailedArchiveFileIsStuck` — a `.harmonik/queues/x.json.failed-<ts>` on disk; oracle
   MUST return HAS_WORK even though `EnumerateQueueNames` hides it. (Asserts §3.3.)
6. `InFlightRunBlocksDrain` — `RunRegistry.Len()==1`; oracle MUST return HAS_WORK.
   (Asserts §3.4.)
7. `BrExecErrorIsUnsure` — fake br errors; oracle MUST return UNSURE (not DRAINED).
   (Asserts §4 fail-safe.)
8. `LedgerLookupErrorIsUnsure` — `BlocksEdge` errors; oracle MUST return UNSURE.
9. `TrulyDrainedReturnsDrained` — all queues completed, registry empty,
   `ready --limit 0` empty, no open epics with ready children; oracle returns DRAINED.
   (The one positive case.)
10. `KerfNotConsulted` — fake kerf returns non-empty; oracle ignores it and returns
    DRAINED when other axes are empty (kerf is a non-input). (Asserts §3.5.)

Fakes: reuse existing `internal/queue` test fakes for `BeadLedger`; add a fake
`brcli`-shaped adapter exposing `Ready` and `ReadyAll`. RunRegistry and QueueStore have
real constructors usable in-process (no daemon boot needed) — these are unit tests, not
scenario tests, so they do NOT hit the 30-min commit budget that times out daemon-boot
scenario beads.

---

## 6. Defaults-PIN note

The oracle is a pure work-detection predicate. It introduces ZERO threshold/band knobs
and MUST NOT read or alter any keeper warn/act/force/window value (operator HARD-NO on
`internal/keeper/thresholds.go` constants). Sleep-grace aggressiveness, wake-trigger
defaults, and any band are POLICY, deferred to the captain sleep-protocol layer (§ mechanism
decomposition, marked DEFERRED). This bead ships the policy-INDEPENDENT mechanism only.

---

## 7. Anchor files (read these to implement)

- `internal/brcli/ready.go:91` — current paginated `Ready` (add `ReadyAll`/`--limit 0`).
- `internal/queue/state.go:130-140,233,290,361` — terminal/eligible/deferred helpers to
  promote-export and reuse.
- `internal/queue/types.go:42-139` — QueueStatus / GroupStatus / ItemStatus enums.
- `internal/queue/persistence.go:289-305,463-485` — failed/cancelled archive + the
  enumeration filter the oracle must bypass.
- `internal/daemon/queuestore_hkj808w.go:199` — `AllQueues()`.
- `internal/daemon/runregistry.go:148,163` — `Len()` / `LenForQueue()`.
- `internal/daemon/queueledger_bridge.go` — `BlocksEdge` / `LookupStatus` impl (`br dep
  list` / `br show`).
- `internal/queue/validation.go:144-154` — `BeadLedger` interface.
