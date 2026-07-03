# named-queues — Tasks (Pass 7)

> Independent-reviewer verdict + bead breakdown. Verified against `main` @ 550d3a78
> on 2026-05-31. Ordering: **A → B → (C ∥ D) → E**. Spec text is folded into each
> implementation bead (spec-first project; each bead lands its own QM-* delta).
>
> **Verdict: SOUND-WITH-FIXES.** The design accurately maps the code; all cited
> line numbers, the singleton QueueStore, the single `runRegistry.Len()` capacity
> gate, the per-file atomic-write dance, the QM-027 validation site, and the
> dispatcher.ts shell-outs all check out. The fixes are scoping clarifications
> folded into the beads below, not design reversals.

---

## Reviewer findings (Part 1)

**F1 — Comp B two-level gate is sound; carries the only real risk.** `workloop.go:618`
(`deps.runRegistry.Len() >= effectiveMax`) is the single global ceiling and
`workloop.go:670-744` snapshots exactly one queue. `RunHandle` (`runregistry.go:32-61`)
has **no queue-name field today**. The two-level gate is implementable two ways:
(a) add `QueueName string` to `RunHandle` and filter `Len()` by name for the
per-queue tally while bare `Len()` stays the global ceiling, or (b) a
scheduler-side tally keyed off `dispatched` items. **(a) is the lower-friction
choice** — it reuses the existing registry as the single source of in-flight
truth and avoids a second counter that can drift from reality on crash/restart.
The global `claimSem` (`workloop.go:553`) stays unchanged (SQLite-write
serializer, orthogonal). Folded into bead **NQ-B1**.

**F2 — Contention policy: CONFIRMED implementable and starvation-free.** The
orchestrator's decided policy maps cleanly onto the snapshot loop: per-queue
`workers` = cap, global `Len() < globalCap` = hard ceiling, round-robin by queue
**name** among active queues that have pending work AND are under their own cap.
Starvation-free because: (1) the global ceiling is a count, not a reservation —
no queue can hold a slot it isn't using; (2) round-robin over a name-sorted
active set guarantees every eligible queue is visited before any queue is
revisited, so a busy `main` cannot indefinitely lock out `investigate`. The one
hole to close in the bead: the loop must advance the round-robin cursor on
**every dispatch tick**, not reset to index 0 each tick — otherwise the
first-named queue (`investigate` sorts before `main`) wins every freed slot. Make
the cursor daemon-state, persisted is unnecessary (reset-on-restart is fine).
Oversubscription (Σ caps > global) is allowed and logged once as a warning at
queue-create/submit time, not per-tick. Folded into **NQ-B1** + spec rule
**QM-0xx cross-queue dispatch policy**.

**F3 — Comp A persistence migration is safe.** `Persist` (`persistence.go:67-134`)
keys its temp file on `os.Getpid()` and does marshal→fsync→rename→parent-fsync
per file. Per-queue files (`.harmonik/queues/<name>.json`) preserve the
single-writer-per-file atomic guarantee byte-for-byte — each file still has one
writer goroutine; the registry is the directory listing. **Back-compat trap
(must handle):** an existing top-level `.harmonik/queue.json` on upgrade. Startup
(`startup_pl005_qm002.go`, consumed `daemon.go:917`) must read a legacy
`queue.json` as the `main` queue exactly once and migrate it into the new layout
(read-as-main + re-persist to `queues/main.json` + archive/remove the legacy
file). Without this, an in-flight queue on a running deployment is silently
dropped on upgrade. Folded into **NQ-A2** with an explicit migration sub-task and
a scenario assertion. `QM-002a`/`QM-002b` three-way reconciliation stays
per-queue (it already reads global Beads status, so cross-queue-safe).

**F4 — `QueueStatusCancelled` drift CONFIRMED.** `types.go:54-61` defines the
fifth status; the spec QM-002 enum (`queue-model.md` §2.2) does NOT list it, and
`dispatcher.ts:156` already treats `cancelled` like `completed`. This is a real
spec-vs-code gap that predates this work. **Decision: separate reconciliation
bead** (**NQ-R1**, P2) — it should land independently of the named-queues track
so it isn't blocked behind the larger feature, but it is small and naturally
co-edits the same QM-002 enum text Comp A touches, so sequence it as a
**prerequisite of NQ-A2** (do the drift cleanup first, then amend the now-correct
enum). Keeps the spec honest before the feature piles on.

**F5 — Non-goal adherence: CLEAN.** No bead introduces a "channel" type (N4 — the
decomposition explicitly treats channel = named queue, and the glossary entry is
the only place "channel" appears, as a synonym). No bead adds per-queue spend
budgets (N2 — the global credfence spend meter stays shared; the per-queue-budget
idea is parked as the DEFERRED bead **NQ-X1**, P3). No bead builds a
flywheel-spawned sub-agent framework (N1 — Comp E routes a bead through a daemon
queue and the daemon-spawned claude is subscription-billed via credfence; there
is no new agent-spawn machinery and no new Go billing code).

**F6 — Error-code budget is TIGHT (flag for Comp D).** Comp D wants a
`queue_not_found` validation reason for `pause <name>`/`resume <name>`/`append
--queue <name>` of a nonexistent queue. The `-32010..-32019` block reserved for
queue-model has **exactly one slot left** (`-32019`, per `queue-model.md:610` +
v0.1.1 changelog line 844). Allocating it for `queue_not_found` is correct and
exhausts the block — the change-spec must note that any **future** queue-model
validation reason needs a block extension (a process-lifecycle PL-003a
amendment). Not a blocker; a documented consequence. Folded into **NQ-D1**.

**F7 — QM-061 reasoning shift (Comp A spec text).** QM-061
(`queue-model.md:736-738`) leans on QM-027 ("at most one active queue") as the
multi-orchestrator safeguard. Relaxing QM-027 to single-active-**per-name** does
NOT open multi-orchestrator (still one submitter). The Comp A spec text must
re-word QM-061's safeguard from "one queue" to "one submitter, N queues." Folded
into **NQ-A1**.

**No infeasibility found.** Every problem-space goal is buildable. G2 (per-queue
workers under a global cap) is the only goal the brief under-states in difficulty;
it gets its own L-sized bead with the policy spec rule.

---

## Bead breakdown (Part 2)

**Total: 12 beads** = 1 epic + 9 component beads (A:2, B:2, C:2, D:2, E:1) + 1
reconciliation (NQ-R1) + 1 deferred follow-up (NQ-X1). Lean: spec text folds into
each impl bead; scenario tests map 1:1 onto SC1–SC5.

All component beads carry label `codename:named-queues`. Priorities: feature
track is **P2**; the epic is **P2**; reconciliation **NQ-R1** is **P2** (small,
keeps spec honest); deferred follow-up **NQ-X1** is **P3**.

Landed-work context (NOT blockers — these are DONE): extqueue v0.1 (`hk-lj0pb`),
pilot, credfence. Cited per bead where the work touches their surface.

---

### NQ-EPIC — Multiple concurrently-active named queues
- **type:** epic · **priority:** 2 · **label:** `codename:named-queues`
- **Children:** NQ-A1, NQ-A2, NQ-B1, NQ-B2, NQ-C1, NQ-C2, NQ-D1, NQ-D2, NQ-E1, NQ-R1, NQ-X1
- **Done means:** SC1–SC5 all pass as scenario tests; `harmonik queue list` shows
  N queues active; per-queue workers sum under the global ceiling; the flywheel
  files an investigate bead to a dedicated subscription-billed queue
  fire-and-forget.

---

### Component A — Registry + persistence + name identity (foundation)

**NQ-A1 — Add queue Name identity + reshape QueueStore to a name-keyed registry**
- **type:** feature · **priority:** 2 · **component:** A
- **Blocks:** NQ-B1, NQ-C1, NQ-D1 (everything depends on A's registry shape)
- **Depends on:** NQ-R1 (do the cancelled-status spec cleanup first), NQ-A2 may
  co-land but the registry shape (this bead) should land first or together
- **Context (landed):** extqueue `hk-lj0pb` (generalizes its validation surface)
- **Spec delta:** amend **QM-027** (single-active → single-active-**per-name**);
  amend **QM-002 / §2.1** (add `name` field to Queue envelope); new **QM-0xx
  Queue naming** (charset/length, reserved/default `main`, name = durable routing
  key distinct from per-submission `queue_id`); re-word **QM-061** safeguard from
  "one queue" to "one submitter, N queues" (F7); amend **QM-022** note that
  no-double-dispatch stays global (cross-queue safe).
- **Files:** `internal/queue/types.go` (Queue.Name), `internal/queue/validation.go:229-242`
  (per-name QM-027 lookup), `internal/daemon/queuestore_hkj808w.go` (`q *Queue` →
  `queues map[string]*Queue`; SetQueue/Queue/ClearQueue/LockForMutation gain a
  name arg; WakeCh semantics preserved).
- **Done means:** `QueueStore` holds N name-keyed queues; submit to a new name
  creates it; submit to an existing non-completed name is rejected per-name; unit
  tests in `queuestore_hkj808w_test.go` + `validation_test.go` green.

**NQ-A2 — Multiplex persistence to per-queue files + legacy migration**
- **type:** feature · **priority:** 2 · **component:** A
- **Blocks:** NQ-E1 (dispatcher.ts readActiveQueue needs the settled layout)
- **Depends on:** NQ-A1 (registry shape)
- **Spec delta:** amend **§2.9** (file layout: `.harmonik/queues/<name>.json`;
  legacy `.harmonik/queue.json` → `main` one-time migration read); amend **QM-003
  / QM-053** (per-name unlink); per-name archive paths for `CancelQueueOnShutdown`
  / `ArchiveFailedQueue`; new **QM-0xx Registry enumeration** (startup scans
  `.harmonik/queues/`; single-writer-per-file invariant).
- **Files:** `internal/queue/persistence.go` (path helper + Persist/Load/Unlink/
  CompleteAndUnlink/CancelQueueOnShutdown/ArchiveFailedQueue all gain `name`),
  `internal/lifecycle/startup_pl005_qm002.go` (multi-file startup read + legacy
  migration), `internal/daemon/daemon.go:571-573,917` (load all queues into
  registry), `internal/daemon/scenariotest/scenariotest.go:321` (`AssertQueueJSON`
  per-name variant).
- **Done means:** each named queue persists to its own file with the WM-026 atomic
  dance preserved; an existing top-level `queue.json` on upgrade is read as `main`,
  migrated, and the active in-flight queue survives the upgrade (asserted in a
  unit/migration test); `persistence_test.go` green.

---

### Component B — Per-queue workers + global ceiling (the hard one)

**NQ-B1 — Two-level capacity gate + round-robin cross-queue dispatch policy**
- **type:** feature · **priority:** 2 · **component:** B
- **Depends on:** NQ-A1, NQ-A2 (needs the registry to iterate + per-queue identity)
- **Spec delta:** rewrite **QM-062** (single-level → two-level:
  `min(group_pending, per_queue_workers − queue_running, global_cap −
  global_running)`); new **QM-0xx Per-queue worker count** (`workers` field;
  default = today's `--max-concurrent`; MUST be ≤ global cap); new **QM-0xx
  Cross-queue dispatch policy** (name-ordered round-robin with a daemon-state
  cursor advancing every tick (F2); oversubscription allowed + logged once as a
  warning; explicitly NOT weighted fairness — that is N3 / extqueue v0.2).
- **Files:** `internal/queue/types.go` (Queue.Workers field), `internal/daemon/runregistry.go`
  (add `QueueName` to `RunHandle`; per-name count helper — F1 option (a)),
  `internal/daemon/workloop.go:618,670-744` (two-level gate + iterate the
  registry, round-robin cursor, per-queue in-flight tally via filtered Len()),
  `internal/daemon/daemon.go` (thread per-queue workers + global cap),
  `cmd/harmonik/supervise/config.go:70` (global cap stays `max_concurrent`;
  per-queue workers set at create/submit, NOT a daemon-global flag).
- **Context (landed):** reuses global `claimSem` (`workloop.go:553`) unchanged.
- **Done means:** with `main`=3 + `investigate`=1 workers and global cap 4, at
  most 4 claude sessions ever run; no queue is starved across a long run;
  `concurrent_dispatch_hk012af_test.go` + `workloop_*_test.go` green.

**NQ-B2 — Scenario test: per-queue workers honor caps + global ceiling (SC2)**
- **type:** task · **priority:** 2 · **component:** B
- **Depends on:** NQ-B1
- **Spec delta:** none (validates QM-062 rewrite).
- **Files:** new `internal/scenario/named_queues_workers_test.go` (shape after
  `queue_lifecycle_test.go` / T80).
- **Done means:** SC2 passes — `main`(3) + `investigate`(1) process concurrently,
  observed concurrent sessions never exceed 4, total never exceeds the global cap.

---

### Component C — Per-queue lifecycle (pause / resume / stop)

**NQ-C1 — Per-queue pause/resume/stop via direct Queue.Status transition**
- **type:** feature · **priority:** 2 · **component:** C
- **Depends on:** NQ-A1 (named queues)
- **Context (landed):** pilot (operator-pause producer + agent-callable pause —
  reused, scoped per-queue).
- **Spec delta:** amend **QM-054 / QM-055** (`paused-by-drain` becomes per-queue;
  a named pause transitions only that queue; the global no-name operator-pause
  still drains ALL queues for back-compat and stays the EM-067 br-ready gate
  source); **un-defer per-queue `queue-resume`** from §A.3 (move it out of the
  v0.2 deferred surface — this is the deliberate G3 scope addition); extend
  **QM-056** pause-reason enum if a per-queue-operator reason value is needed.
- **Files:** `internal/daemon/queue_operatoreventconsumer_7urls.go` (transition
  the named queue only), `internal/daemon/operatorpause.go` (route a named pause
  straight to that queue's status; keep the global bool for the unnamed case),
  `internal/daemon/socket.go` (op payload gains optional `queue` field),
  `internal/daemon/workloop.go:675-684` (already per-queue once A lands; verify).
- **Done means:** `harmonik queue pause investigate` halts only `investigate`'s
  dispatch; `main` keeps going; `resume` restores; the unnamed global pause still
  drains all; `queue_operatoreventconsumer_7urls_test.go` +
  `operatorpause_ry8q1_test.go` green.

**NQ-C2 — Scenario test: per-queue pause isolates one queue (SC3)**
- **type:** task · **priority:** 2 · **component:** C
- **Depends on:** NQ-C1, NQ-D1 (needs the `queue pause <name>` verb to drive it)
- **Spec delta:** none.
- **Files:** new `internal/scenario/named_queues_pause_test.go`.
- **Done means:** SC3 passes — pause `investigate`, `main` keeps dispatching, the
  in-flight investigate run reaches a terminal state, resume restores dispatch.

---

### Component D — Routing + CLI (parallel with C)

**NQ-D1 — `--queue` routing + list/pause/resume/create verbs + default main**
- **type:** feature · **priority:** 2 · **component:** D
- **Depends on:** NQ-A1 (named routing). Can land parallel to C; `queue pause`
  verb (here) pairs with C1's daemon-side pause — co-land or land C1 first.
- **Spec delta:** amend **§2.10 wire records** (additive-optional `name`/`queue`
  on `QueueSubmitRequest`/`QueueAppendRequest`; absent = `main`; no schema bump
  per ON-018); new **QM-0xx Routing default** (absent `--queue` ⇒ `main`;
  implicit-create on first submit to an unknown name with default workers);
  allocate the **last reserved error code `-32019` → `queue_not_found`** (F6) and
  note the block is now exhausted; cross-file the new verbs (`list`/`pause`/
  `resume`/`create`) into **process-lifecycle §4.4**.
- **Files:** `cmd/harmonik/main.go:203-262` (verb switch + help text — add
  list/pause/resume/create), `internal/queue/cli/{submit,append,status,cancel,helpers}.go`
  (`--queue` flag in `parseQueueFlags`; new list/pause/resume/create clients),
  `internal/queue/types.go` (wire-record `Name` field; QueueAppendRequest resolve
  by name in addition to queue_id), `internal/daemon/socket.go` + `rpc.go` (route
  by name; `queue list` method).
- **Done means:** bare `queue submit` lands in `main` (SC5); `queue submit --queue
  investigate` auto-creates; `queue list` enumerates all active queues with status
  + worker counts; `pause`/`resume <name>` reach the daemon; `cli_test.go` green.

**NQ-D2 — Scenario test: two named queues both active (SC1) + bare-submit→main (SC5)**
- **type:** task · **priority:** 2 · **component:** D
- **Depends on:** NQ-D1
- **Spec delta:** none.
- **Files:** new `internal/scenario/named_queues_routing_test.go`.
- **Done means:** SC1 passes — `submit --queue investigate` + `submit --queue
  main` → `queue list` shows BOTH `active`; SC5 passes — a bare `submit` lands in
  `main` and the existing single-queue scenario tests still pass unchanged.

---

### Component E — Investigate handoff (strictly last)

**NQ-E1 — Flywheel files investigate bead → dedicated subscription-billed queue (SC4)**
- **type:** feature · **priority:** 2 · **component:** E
- **Depends on:** NQ-A2 (persistence layout for readActiveQueue), NQ-D1 (`--queue
  investigate` flag), NQ-C1 (per-queue pause control). Strictly last.
- **Context (landed):** credfence (daemon-spawned claude is subscription-billed
  via its credential path — NO new billing code needed; routing the bead through
  a daemon queue IS the whole billing mechanism, which is exactly why the handoff
  goes through a queue per N1).
- **Spec delta:** one **informative note** in queue-model that the `investigate`
  queue is an ordinary named queue with no special semantics (reinforces N4). NO
  per-queue budget rule (N2). The substantive contract is cognition-loop /
  flywheel-bridge spec territory, not queue-model.
- **Files:** `.pi/extensions/flywheel/bridge.ts` (net-new reflection path: detect
  follow-up → `br create` investigate bead → submit to `investigate` queue),
  `.pi/extensions/flywheel/dispatcher.ts:140-170,186-232` (`readActiveQueue` reads
  the new per-queue layout; `makeQueueSubmit`/`makeQueueAppend` gain `--queue
  investigate`), `.pi/extensions/flywheel/__tests__/bridge.test.ts` (SC4 replay).
- **Done means:** SC4 passes — the flywheel files an investigate bead, submits it
  to the `investigate` queue, a daemon-managed subscription-billed claude pulls it
  and either commits a fix (`Refs:`) or files follow-up beads, and the flywheel
  neither blocks on nor tracks the outcome (fire-and-forget).

---

### Reconciliation (prerequisite to A's enum amend)

**NQ-R1 — Reconcile undocumented QueueStatusCancelled into the QM-002 enum**
- **type:** bug · **priority:** 2 · **component:** A (drift cleanup)
- **Blocks:** NQ-A1 (clean the enum text before A amends it)
- **Depends on:** nothing
- **Spec delta:** add `cancelled` to the **QM-002 §2.2 QueueStatus enum** with its
  semantics (operator-cancelled before terminal; file left on disk so next run's
  per-name QM-027 guard bypasses cleanly — `types.go:54-61`, hk-ppt32); note
  `dispatcher.ts:156` already treats it like `completed`.
- **Files:** spec only (`specs/queue-model.md` §2.2 + glossary); no code change —
  `types.go` already has the constant.
- **Done means:** the spec QueueStatus enum lists all five statuses incl.
  `cancelled`; the code-vs-spec drift is closed; no behavior change.

---

### Deferred follow-up (backlog — NOT a v0.1 blocker)

**NQ-X1 — Per-queue spend budget caps (open-decision 1, option B)**
- **type:** feature · **priority:** 3 · **component:** B-adjacent (budget)
- **Depends on:** NQ-EPIC complete (named queues must exist first)
- **Context (landed):** credfence single global spend meter
  (`internal/daemon/spendmeter_hkk3f8g.go`).
- **Spec delta (when picked up):** new per-queue budget rule layered over the
  credfence global meter; a busy `investigate` queue gets its own cap so it cannot
  starve `main`'s budget.
- **Done means (when picked up):** each named queue can carry an optional spend
  cap; the global meter remains the daemon ceiling. **DEFERRED — v0.1 ships the
  single unified global meter per N2; this bead is the documented follow-up the
  user flagged in open-decision 1.**

---

## Dependency edges (orchestrator translation aid)

```
NQ-R1  ──blocks──► NQ-A1
NQ-A1  ──blocks──► NQ-A2, NQ-B1, NQ-C1, NQ-D1
NQ-A2  ──blocks──► NQ-B1, NQ-E1
NQ-B1  ──blocks──► NQ-B2
NQ-C1  ──blocks──► NQ-C2
NQ-D1  ──blocks──► NQ-D2, NQ-C2, NQ-E1
NQ-C1  ──blocks──► NQ-E1
NQ-EPIC ──parent-of──► all NQ-* above
NQ-X1  ──depends-on──► NQ-EPIC  (deferred / P3)
```

Critical path: **NQ-R1 → NQ-A1 → NQ-A2 → NQ-B1 → … → NQ-E1.**
Parallel after A2 lands: **NQ-B1**, **NQ-C1**, **NQ-D1** can run concurrently
(B touches the workloop scheduler, C touches the daemon consumer/operatorpause, D
touches CLI/wire — disjoint files). The three scenario beads (B2/C2/D2) each
follow their feature bead. E1 is strictly last.
