# named-queues — Decomposition (Pass 3)

> Components, boundaries, dependency order. Builds on `02-analysis.md`. Honors
> non-goals: NO new "channel" abstraction (a channel IS a named queue, N4); NO
> per-queue budgets in v0.1 (N2); NO flywheel sub-agent framework (N1).
> Component spine refined against the code: **A → B → (C ∥ D) → E**.

---

## Dependency graph

```
  A  (registry + persistence + name identity)   ── foundation, blocks all
  │
  ├──► B  (per-queue workers + global ceiling)   ── the hard one; depends on A
  │
  ├──► C  (per-queue lifecycle / pause-resume-stop)   ── depends on A
  │
  └──► D  (routing + CLI: --queue, list, create, pause/resume verbs)  ── depends on A
                   │
                   └──► E  (investigate handoff in bridge.ts)  ── gated behind A,B,C,D
```

**Parallelism:** C and D can land in parallel once A is in (both only depend on
A's name/registry; they touch disjoint files — C is daemon/consumer/workloop-status,
D is CLI/wire). B depends on A and is best landed before or alongside C/D because
its workloop changes overlap the dispatch path C reads. **E is strictly last** —
it needs A's persistence layout (readActiveQueue rewrite), D's `--queue` flag, and
C's per-queue pause to demonstrate SC3/SC4 end-to-end.

---

## Comp A — Multi-queue registry + persistence

**Scope.** Relax QM-027 from single-active to N-named-active. Add a human-supplied
`Name` to the `Queue` envelope. Multiplex `.harmonik/queue.json` → per-queue files
(`.harmonik/queues/<name>.json`), preserving the WM-026 atomic-rename / single-writer
guarantee per file (C1, C6, G1). Reshape `QueueStore` from one `*Queue` to a
name-keyed registry. Default queue `main` for back-compat (C3).

**Spec rules changed/added.**
- **Amend QM-027** (`queue-model.md:553-565`): single-active → **single-active-per-name**.
  Reject submit only if a queue *with the same name* is non-completed.
- **Amend QM-002 / §2.1 / §2.9** (`queue-model.md:62-68,144-146,253-273`): add
  `name` field; define per-queue file layout + legacy `queue.json`→`main` read.
- **Amend QM-003 / QM-053** (`queue-model.md:308-310,688-697`): unlink the
  per-name file.
- **New QM-0xx — Queue naming**: name charset/length, reserved `main` default,
  case rules, name as durable routing key (distinct from per-submission `queue_id`).
- **New QM-0xx — Registry enumeration**: how the daemon discovers active queues at
  startup (scan `.harmonik/queues/`) and the single-writer-per-file invariant.
- **Reconcile** the undocumented `QueueStatusCancelled` (`types.go:54-61`) into the
  QueueStatus enum while here (drift cleanup).

**Files touched.**
- `internal/queue/types.go` (Queue.Name; QueueStatus enum cleanup).
- `internal/queue/persistence.go` (path helper, Persist/Load/Unlink/
  CompleteAndUnlink/CancelQueueOnShutdown/ArchiveFailedQueue all gain a `name`).
- `internal/queue/validation.go:229-242` (per-name QM-027).
- `internal/daemon/queuestore_hkj808w.go` (the central reshape: `q *Queue` →
  `queues map[string]*Queue`; SetQueue/Queue/ClearQueue/LockForMutation gain name;
  WakeCh semantics).
- `internal/daemon/daemon.go:571-573,917` (startup load all queues into registry).
- `internal/lifecycle/startup_pl005_qm002.go` (multi-file startup read).
- Tests: `persistence_test.go`, `queuestore_hkj808w_test.go`, `validation_test.go`,
  `scenariotest.go:321` (`AssertQueueJSON` per-name variant).

**Dependencies.** extqueue v0.1 (`hk-lj0pb`) — generalizes its file/validation
surface. No dependency on pilot/credfence.

**Size: L.** Wide blast radius (every `qs` reader), but each edit is mechanical.

---

## Comp B — Per-queue workers + global ceiling

**Scope.** Each queue carries its own `max-concurrent` worker count. The daemon
scheduler enforces a two-level gate: per-queue in-flight ≤ that queue's workers,
AND sum of all in-flight ≤ the global daemon cap (C2, G2). Define the
queue-iteration policy when the global cap is the binding constraint.

**Spec rules changed/added.**
- **Rewrite QM-062** (`queue-model.md:740-748`): the composition rule becomes
  two-level: `min(group_pending, per_queue_workers - queue_running,
  global_cap - global_running)`. The global cap is the existing `--max-concurrent`
  reinterpreted as the **daemon ceiling**; per-queue workers are new.
- **New QM-0xx — Per-queue worker count**: a `workers` field on the queue
  (default value; how set at create/submit); MUST be ≤ global cap.
- **New QM-0xx — Cross-queue dispatch policy**: the deterministic policy when
  `global_cap < Σ per_queue_workers` (e.g. name-ordered round-robin; NOT weighted
  fairness — that is N3 / extqueue v0.2). Names the v0.1 simplest-correct choice.

**Files touched.**
- `internal/queue/types.go` (Queue.Workers field).
- `internal/daemon/workloop.go:537-541,617-623,647-744` (the capacity gate +
  queue-pull loop — iterate registry, per-queue in-flight tally, two-level gate).
- `internal/daemon/runregistry.go` (per-queue count dimension, OR scheduler-side
  tally keyed off dispatched items).
- `internal/daemon/daemon.go` (thread per-queue workers + global cap).
- `cmd/harmonik/supervise/config.go:70` / `start.go` (global cap stays
  `max_concurrent`; per-queue workers via create/submit, not a daemon-global flag).
- Tests: `concurrent_dispatch_hk012af_test.go`, `workloop_*_test.go`, new
  scenario for SC2 (3+1 workers, ≤4 sessions, never exceed global cap).

**Dependencies.** Comp A (needs the registry to iterate; needs per-queue identity).
Reuses the existing global `claimSem` (`workloop.go:553`) unchanged.

**Size: L (highest risk).** Rewrites the most load-bearing, most-tested code path
(the dispatch capacity gate) and adds a scheduling policy the spec lacks today.

---

## Comp C — Per-queue lifecycle (pause / resume / stop)

**Scope.** Pause/resume/stop a single named queue without affecting others (C4,
G3). Reuse the pilot operator-pause producer, scoped per-queue. Cleanest as a
direct per-named-queue `Queue.Status` transition (the workloop already gates on
`q.Status`, `workloop.go:675-684`), NOT by fanning the global
`OperatorPauseController` bool.

**Spec rules changed/added.**
- **Amend QM-054 / QM-055** (`queue-model.md:699-714`): `paused-by-drain` becomes
  per-queue; a named pause transitions only that queue. The global operator-pause
  (no name) still drains ALL queues (back-compat) and stays the EM-067 br-ready
  gate source.
- **Un-defer per-queue `queue-resume`** from the v0.2 surface
  (`queue-model.md:800-801`): add a normative per-queue resume
  (`paused-by-drain → active` for one named queue). This is a scope addition the
  problem-space (G3) explicitly requests; flag it for the change-spec.
- **New QM-0xx — Per-queue pause/resume/stop** semantics + a `queue_paused`
  reason value for operator-scoped-per-queue (extend the QM-056 enum
  `queue-model.md:716-722`, currently `group_failure, operator_drain`).

**Files touched.**
- `internal/daemon/queue_operatoreventconsumer_7urls.go` (react to a
  per-queue pause/resume signal; transition the named queue only).
- `internal/daemon/operatorpause.go` (either add a named-pause path or keep
  global bool for the unnamed case and route named pause straight to queue status).
- `internal/daemon/socket.go` (socket op payload gains optional `queue` field).
- `internal/daemon/workloop.go:675-684` (already per-queue once A lands; verify).
- Tests: `queue_operatoreventconsumer_7urls_test.go`, `operatorpause_ry8q1_test.go`,
  new scenario for SC3 (pause investigate, main keeps dispatching, in-flight
  reaches terminal, resume restores).

**Dependencies.** Comp A (named queues). pilot (the operator-pause producer +
agent-callable pause — hard prerequisite per problem-space).

**Size: M.**

---

## Comp D — Routing + CLI

**Scope.** `--queue <name>` on submit/append/status/cancel (default `main`);
`harmonik queue list`; `harmonik queue pause <name>` / `resume <name>`; optional
`harmonik queue create <name> --workers N` (implicit-on-submit + optional explicit
create, open-decision 3). Back-compat: bare submit → `main` (G4, C3, SC5).

**Spec rules changed/added.**
- **Amend §2.10 wire records** (`queue-model.md:193-227`): add optional
  `name`/`queue` to `QueueSubmitRequest`/`QueueAppendRequest`; absent = `main`.
  Additive-optional, no schema bump (ON-018).
- **New QM-0xx — Routing default**: absent `--queue` ⇒ `main`; implicit-create on
  first submit to an unknown name with default workers.
- **Owned by process-lifecycle §4.4** (`queue-model.md:47-48`): the new verbs
  (`list`, `pause`, `resume`, `create`) and any new socket method/error code
  (`queue not found` → `-32019`, the last reserved slot, `queue-model.md:609`) are
  process-lifecycle's surface — the change-spec must cross-file there too.

**Files touched.**
- `cmd/harmonik/main.go:203-262` (verb switch + help text; add list/pause/resume/
  create).
- `internal/queue/cli/submit.go,append.go,status.go,cancel.go,helpers.go`
  (`--queue` flag in `parseQueueFlags`; new list/pause/resume/create clients).
- `internal/queue/types.go` (wire-record `Name` field).
- `internal/daemon/socket.go` + `rpc.go` (route by name; `queue list` method).
- Tests: `internal/queue/cli/cli_test.go`, new SC1 scenario (`submit --queue
  investigate` + `submit --queue main` → `queue list` shows both `active`).

**Dependencies.** Comp A (named routing). Can land parallel to C (disjoint files:
D = CLI/wire, C = daemon consumer) — but `queue pause <name>` verb (D) and the
per-queue pause semantics (C) are paired; land C's daemon side first or co-land.

**Size: M.**

---

## Comp E — Investigate handoff (gated behind A–D)

**Scope.** The flywheel post-run reflection files an `investigate` bead
(`br create`) and submits it to a dedicated `investigate` queue; a daemon-managed,
subscription-billed claude session pulls and processes it (fix or file follow-ups).
Fire-and-forget: the flywheel neither blocks on nor tracks the outcome (G5, N1, N5,
SC4).

**Spec rules changed/added.**
- Mostly **cognition-loop / flywheel-bridge spec** territory, not queue-model:
  the queue-model side is just "route a bead to a named queue" (Comp D already
  provides). One **informative note** in queue-model that the `investigate` queue
  is an ordinary named queue with no special semantics (reinforces N4).
- No per-queue budget rule (N2) — the investigate queue shares the single global
  spend meter (credfence; open-decision 1 default).

**Files touched.**
- `.pi/extensions/flywheel/bridge.ts` (new reflection path: detect follow-up →
  `br create` investigate bead → submit to `investigate` queue; net-new behavior).
- `.pi/extensions/flywheel/dispatcher.ts:140-170,186-232` (`readActiveQueue`
  must read the new per-queue layout from Comp A; `makeQueueSubmit`/
  `makeQueueAppend` gain `--queue investigate`).
- `.pi/extensions/flywheel/__tests__/bridge.test.ts` (handoff replay test for SC4).
- No new Go billing code — daemon-spawned claude is subscription-billed via the
  credfence credential path (C5); routing the bead through a daemon queue is the
  whole mechanism.

**Dependencies.** Comp A (persistence layout for `readActiveQueue`), Comp D
(`--queue investigate`), Comp C (per-queue pause for SC3-adjacent control), the
flywheel bridge. credfence (✅ landed) — subscription-billing touch-point, not a
blocker. **Strictly last.**

**Size: M.**

---

## Spec-delta sketch (seeds the change-spec pass)

**Amended QM-* rules:**
- **QM-027** (`queue-model.md:553-565`) — single-active → single-active-**per-name**
  (Comp A). The keystone relax.
- **QM-062** (`queue-model.md:740-748`) — single-level `--max-concurrent`
  composition → **two-level** (per-queue workers + global ceiling) (Comp B).
- **QM-002 / §2.1 / §2.9** (`queue-model.md:62-68,144-273`) — add `name`;
  per-queue file layout; legacy `queue.json`→`main` read (Comp A).
- **QM-003 / QM-053** (`queue-model.md:308-310,688-697`) — per-name unlink (Comp A).
- **QM-054 / QM-055** (`queue-model.md:699-714`) — per-queue `paused-by-drain`;
  un-defer per-queue resume from §A.3 (Comp C).
- **QM-056** (`queue-model.md:716-722`) — extend `QueuePauseReason` if a
  per-queue-operator reason is needed (Comp C).
- **§2.10 wire records** (`queue-model.md:193-227`) — additive-optional `name`
  (Comp D).
- **QM-061** (`queue-model.md:736-738`) — re-word: safeguard shifts from "one
  queue" to "one submitter, N queues" (Comp A).
- **§A.3 deferred surface** (`queue-model.md:796-808`) — move per-queue resume
  out of deferred; note multi-queue is now in v0.1 scope (Comp A/C).
- **QueueStatus enum** — reconcile undocumented `cancelled` (`types.go:54-61`).

**New QM-* rules needed:**
- **Queue naming** — name charset/length, reserved/default `main`, name as durable
  routing key vs. per-submission `queue_id` (Comp A).
- **Registry enumeration** — startup discovery of active queues; single-writer-
  per-file invariant (Comp A).
- **Per-queue worker count** — `workers` field, default, MUST be ≤ global cap
  (Comp B).
- **Cross-queue dispatch policy** — deterministic v0.1 policy when
  `global_cap < Σ workers` (name-ordered, NOT weighted; N3) (Comp B).
- **Per-queue pause/resume/stop** — named lifecycle semantics; relation to the
  global operator-pause / EM-067 gate (Comp C).
- **Routing default** — absent `--queue` ⇒ `main`; implicit-create-on-submit
  (Comp D).
- (Process-lifecycle §4.4, not queue-model) new verbs `list`/`pause`/`resume`/
  `create` + a `queue_not_found` validation reason at error-code `-32019`.

**Explicitly NOT added (non-goals):** no per-queue spend-budget rule (N2); no
cross-queue priority/weighting/conditional-ordering (N3, stays extqueue v0.2); no
"channel" type — channel = named queue (N4); no flywheel sub-agent framework (N1).
