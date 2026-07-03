# named-queues — Analysis (Pass 2)

> Factual map of the territory. NOT a proposal. Citations are real file paths +
> line numbers as of 2026-05-31 (`main` @ 550d3a78). Where the problem-space doc
> (`01-problem-space.md`) assumes something the code makes harder, it is flagged
> **[FRICTION]**.

---

## 0. The one structural fact that shapes everything

The entire daemon is built around **one** `*queue.Queue` held in a single
`QueueStore` (`internal/daemon/queuestore_hkj808w.go:58-66`). The QueueStore is
the composition-root singleton: it is created once in `daemon.Start`
(`internal/daemon/daemon.go:571-573`), wired into the socket handlers, the
workloop (`daemon.go:1025-1029` → `deps.queueStore = qs`), the
`QueueOperatorEventConsumer` (`daemon.go:564-566`), and the startup loader
(`daemon.go:917` `qs.SetQueue(loadedQueue)`). Everything that touches "the queue"
dereferences `qs.Queue()` and gets the **one** queue.

Multi-queue therefore is not a feature bolted onto a queue — it is a change to
the **shape of the holder**: `QueueStore.q *queue.Queue` →
`QueueStore.queues map[string]*queue.Queue` (or equivalent). Every reader/writer
of `qs` is an edit site. This is the spine of Comp A (`03-components.md`).

---

## 1. Queue identity & persistence (C1, C6, G1)

### Identity today
- The `Queue` envelope (`internal/queue/types.go:227-246`) has **no name field**.
  Identity is `QueueID` (a daemon-minted UUIDv7, `types.go:236`) — opaque,
  machine-minted, not human-supplied. The spec forbids client-supplied
  `queue_id` (QM-010, `queue-model.md:390-394`).
- There is exactly one `queue_id` in flight at a time, enforced by QM-027
  (see §5).

**[FRICTION]** G1 wants a *human-supplied name* (`main`, `investigate`). The
existing `queue_id` UUID is the wrong axis — names are a **new identity
dimension** that must be added alongside (not replacing) `queue_id`. The brief's
C1 ("add a name/identity dimension") is correct; a `Name string` field on
`Queue` is the minimal add. `queue_id` stays per-submission-instance; `name` is
the durable routing key.

### Persistence today (single-file)
- Canonical path: `.harmonik/queue.json`, computed by
  `queuePath(projectDir)` (`internal/queue/persistence.go:40-42`) →
  `projectDir + "/.harmonik/" + "queue.json"` (`persistence.go:14`).
- Atomic write: `Persist` (`persistence.go:67-134`) does the WM-026 five-step
  dance: marshal → `queue.json.tmp-<pid>` → `fsync(tmp)` → `rename(2)` →
  `fsync(parent_dir)`. **This is the single-writer atomic guarantee C6 must
  preserve.** The temp name is keyed on `os.Getpid()` (`persistence.go:87`).
- Load: `Load` (`persistence.go:145-163`) — file-absent → `(nil,nil)`;
  unparseable → `ErrCorrupt`; parses → `(&q,nil)`. Schema guard is
  `UnmarshalQueue` (`types.go:253-262`, `schemaVersion = 1`, `types.go:13`).
- Lifecycle file ops, all single-file, all need a multi-queue analogue:
  - `CompleteAndUnlink` (`persistence.go:196-208`) — QM-053: persist `completed`
    then `Unlink`.
  - `Unlink` (`persistence.go:308-328`) — QM-003 removal + parent fsync.
  - `CancelQueueOnShutdown` (`persistence.go:227-258`) — renames to
    `queue.json.cancelled-<ts>` so the next run's QM-027 guard is clear (hk-ppt32).
  - `ArchiveFailedQueue` (`persistence.go:271-299`) — renames to
    `queue.json.failed-<ts>` on paused-by-failure (hk-ly4w5).
- Startup read is wired at `internal/lifecycle/startup_pl005_qm002.go` (PL-005
  step 8a) and consumed in `daemon.go:917`.

### Code-vs-spec drift to note
- The code has a **fifth** `QueueStatus`: `QueueStatusCancelled`
  (`types.go:54-61`, hk-ppt32) that the spec's QM-002 enum (`queue-model.md:70-78`)
  does NOT list. The dispatcher's TS reader already treats `cancelled` like
  `completed` (`dispatcher.ts:156`). Any spec amendment in this work should
  reconcile this (the change-spec pass owns the call; flagging here).

### What multi-queue persistence touches (C6)
Two viable shapes, both preserving per-file atomic-rename:
1. **Per-queue files**: `.harmonik/queues/<name>.json` (or `queue-<name>.json`),
   one atomic-rename per file. Single-writer is preserved trivially because each
   file still has one writer goroutine; the registry is the directory listing.
2. **Registry index + per-queue files**: an index file naming the active queues
   plus per-queue files. More moving parts; the index becomes a second atomic-write
   target and a second corruption surface.

Per-queue files (option 1) is the lower-friction match to the existing
`Persist`/`Load`/`Unlink` shape — each function gains a `name` parameter and the
path helper changes; the atomic dance is byte-for-byte reused. Back-compat (C3):
read a legacy top-level `.harmonik/queue.json` as the `main` queue on startup
(one-time migration or dual-path read).

---

## 2. Concurrency: `--max-concurrent` enforcement (C2, G2)

### Where the cap lives
- Config: `daemon.Config.MaxConcurrent` (`daemon.go:117-130`); surfaced from
  `supervise/config.go:70` (`max_concurrent` JSON field) and the
  `harmonik run --max-concurrent N` flag (hk-w3cp1).
- Enforced in `runWorkLoop` (`internal/daemon/workloop.go:532`):
  - `effectiveMax := deps.maxConcurrent` (zero → 1) (`workloop.go:537-541`).
  - **The capacity gate** (`workloop.go:617-623`):
    `if deps.runRegistry.Len() >= effectiveMax { sleep; continue }`.
    `runRegistry` (`internal/daemon/runregistry.go`) counts ALL in-flight bead
    goroutines daemon-wide — there is exactly **one** counter.
  - `claimSem := make(chan struct{}, effectiveMax)` (`workloop.go:553`) — a
    second daemon-wide ceiling that bounds simultaneous `ClaimBead` SQLite writes
    (acquire `workloop.go:1034`, release `workloop.go:1040`).

### The dispatch loop is single-queue
The workloop's queue-pull (Phase 1, `workloop.go:647-744`) snapshots **one**
queue from `deps.queueStore.Queue()`, finds **the** single active group, picks
the head eligible item, and dispatches. There is no notion of "which queue" — it
is structurally the singleton.

**[FRICTION] — this is the hardest part of G2.** The brief wants per-queue worker
counts that *sum* under a global daemon cap. The current model has:
- ONE global counter (`runRegistry.Len()`), and
- ONE queue to pull from.

Per-queue workers require the scheduler to (a) iterate over N queues, (b) track
in-flight count **per queue** (so it can stop dispatching `investigate` at its
1-worker cap while `main` keeps going at 3), AND (c) still respect the global
`runRegistry.Len() < globalCap` ceiling. The `runRegistry` either grows a
per-queue dimension (a `name → count` map) or the scheduler keeps its own
per-queue in-flight tally keyed off the queue items' `dispatched` status. The
global `claimSem` (`workloop.go:553`) can stay as-is — it is a SQLite-write
serializer, orthogonal to per-queue fairness.

The composition rule that QM-062 (`queue-model.md:740-748`) states today —
`dispatch up to min(group_pending, --max-concurrent - currently_running)` —
must become a **two-level** rule:
`min(group_pending, per_queue_workers - queue_running, global_cap - global_running)`.
This is the central spec amendment (QM-062 rewrite, see Comp B).

There is **no round-robin / fairness primitive** today. The brief's N3 explicitly
defers weighted fairness. v0.1 can pick the simplest policy (e.g. iterate queues
in name order, dispatch what each is entitled to) — but it MUST be a defined
policy because with a global cap < sum-of-per-queue-workers, queues compete.
**Open question candidate** (see report).

---

## 3. Lifecycle: pause / resume (C4, G3)

### Today: global, not per-queue
- CLI: `harmonik supervise pause|resume` (`cmd/harmonik/supervise/pause.go`,
  `resume.go`). `RunPause` (`pause.go:33-70`) dials the daemon socket and sends
  `{"op":"operator-pause"}` via `sendOperatorOp` (`pause.go:97-140`). No queue
  name is sent — it is a **daemon-wide** operator op.
- Daemon side: `OperatorPauseController` (`internal/daemon/operatorpause.go:55-59`)
  flips one `paused bool` and emits `operator_pause_status{pausing|paused}`
  (`operatorpause.go:86-107`) / `operator_resuming` (`operatorpause.go:117-139`).
- Queue reaction: `QueueOperatorEventConsumer`
  (`internal/daemon/queue_operatoreventconsumer_7urls.go:67-69`) consumes those
  events and transitions **the single queue** `active ↔ paused-by-drain`
  (QM-054/QM-055). It holds one `QueueStore` (`...7urls.go:48`).
- The workloop honors pause by reading `q.Status` in Phase 1
  (`workloop.go:675-684`): on `PausedByDrain`/`PausedByFailure`/`Completed` it
  idle-waits.

### The "producer pilot landed" piece (C4)
The agent-callable operator-pause **producer** is the socket op +
`OperatorPauseController`. It landed under hk-ry8q1 (recent commit
`feat(daemon): wire QueueOperatorEventConsumer`, `25b7672e`; pause/resume CLI
files dated 2026-05-31). G3 reuses this producer **scoped per-queue**.

**[FRICTION]** Per-queue pause means the pause signal must carry a queue name.
Two clean options:
1. Add a `queue` field to the socket op payload (`{"op":"operator-pause",
   "queue":"investigate"}`) and have the controller / consumer pause only that
   named queue's status. The global (no-name) form pauses all queues (back-compat).
2. A distinct verb path (`harmonik queue pause <name>`) that maps to a new socket
   op. Either way the per-`bool` `paused` flag in `OperatorPauseController`
   (`operatorpause.go:57`) becomes per-queue state, OR pause becomes purely a
   per-queue `Queue.Status` transition (skip the controller's global bool for the
   named case). The latter is cleaner: the queue's own `paused-by-drain` status
   is already the source of truth the workloop reads (`workloop.go:675-684`); a
   per-queue pause just sets that status directly for the named queue and emits
   `queue_paused`. Note the global `IsPaused()` is also consulted by the
   execution-model br-ready fallback gate (EM-067) — that gate is the *no-queue*
   path and should stay global.

v0.1 deferred-surface note: the spec defers `queue-resume` and `queue-clear` to
v0.2 (`queue-model.md:53,800-801,803`). G3 explicitly **un-defers** per-queue
resume — this is a deliberate scope addition the change-spec must record.

---

## 4. CLI / routing (G4, C3, open-decision 3)

### Current verbs
`cmd/harmonik/main.go:203-262` dispatches `harmonik queue <verb>` to the
`internal/queue/cli` package:
- `submit` → `RunQueueSubmit` (`queue/cli/submit.go:30`) — reads a
  QueueSubmitRequest JSON doc from a file arg, sends `queue-submit` over socket.
- `append` → `RunQueueAppend` (`queue/cli/append.go`) — takes `--queue-id`,
  group index, bead IDs.
- `status` → `RunQueueStatus`.
- `dry-run` → `RunQueueDryRun`.
- `cancel` → `RunQueueCancel` (`queue/cli/cancel.go`) — no-daemon archive path.

Flag parsing is hand-rolled (`parseQueueFlags`, `queue/cli/helpers.go`); no
cobra/flag library. The socket envelope is built by `buildEnvelope("queue-submit",
queueDoc)` (`submit.go:60`).

### Where `--queue <name>` attaches
- `submit`/`append`/`status`/`cancel`: add a `--queue <name>` flag in
  `parseQueueFlags` (or per-verb). On the wire, the JSON-RPC request records
  (`QueueSubmitRequest`, `QueueAppendRequest`, `types.go:278-311`) gain a
  `Name`/`Queue` field. **[note]** `QueueAppendRequest` currently keys on
  `queue_id` (`types.go:304`); with named queues the append target can be
  resolved by `name` instead of (or in addition to) `queue_id`.
- New verbs: `queue list` (enumerate active queues + status + worker counts) and
  `queue pause <name>` / `queue resume <name>`. These attach in the same
  `switch verb` block (`main.go:248-262`) and the help text (`main.go:210-240`).
- Optional `queue create <name> --workers N` (open-decision 3, decided: implicit
  on submit PLUS optional explicit create). Implicit-on-submit means
  `queue-submit --queue investigate` auto-creates the queue with a default worker
  count if it does not exist.

### Default queue (C3, G4)
A bare `harmonik queue submit <doc>` with no `--queue` must land in `main`. The
CLI defaults the name to `"main"` when the flag is absent; the daemon treats an
absent name on the wire as `"main"`. Existing scenario tests submit without a
name → they must keep landing in `main` and passing (SC5).

### The flywheel/Pi dispatcher CLI dependency
`dispatcher.ts` shells out to `harmonik queue submit --json <file>`
(`dispatcher.ts:200`) and `harmonik queue append --queue-id <id> 0 <bead>`
(`dispatcher.ts:227`). These currently target the singleton. For the investigate
handoff (Comp E) the dispatcher's submit/append helpers need a `--queue
investigate` variant. Any new `--queue` flag MUST be back-compat (absent = main)
so the existing curated-refill path (`makeQueueSubmit`/`makeQueueAppend`) keeps
working unchanged.

---

## 5. The QM-027 singleton constraint (the relax target)

### Quoted (queue-model.md §6.8, lines 553-565)
> **QM-027 — Single active queue.** A `queue-submit` request MUST be rejected if
> the daemon already holds a queue object whose `status` is not `completed`.
> Returns `queue_validation_failed{reason:"queue_already_active",
> existing_queue_id, existing_status}`. `queue-submit` after a queue has reached
> `completed` and `.harmonik/queue.json` has been unlinked per QM-003 is permitted
> and begins a fresh queue.

### Where it is enforced in code
`internal/queue/validation.go:229-242`:
```go
// --- QM-027: single active queue (submit-only) ---
if !req.IsAppend {
    if req.ActiveQueue != nil && req.ActiveQueue.Status != QueueStatusCompleted {
        return []ValidationError{{ Reason: ReasonQueueAlreadyActive, ... }}, nil, nil
    }
}
```
`ReasonQueueAlreadyActive` is defined `validation.go:24`; error-code mapping
`-32010` (`errors.go:81-82`, QM-029b table `queue-model.md:601`); evaluation order
is FIRST (`queue-model.md:592`, QM-029a).

### What relaxing QM-027 touches
1. **Validation** (`validation.go:229-242`): the single-active check is replaced
   by a **per-name** check — "reject if a queue *with this name* is already active
   and not completed." The `ValidationRequest.ActiveQueue` field
   (`validation.go:186-188`) becomes "the active queue *for the target name*"
   (the daemon looks it up by name before validating). QM-026 size bound and
   QM-022 no-double-dispatch are still global (a bead in_progress in ANY queue
   blocks submit — `QM-022` already reads global Beads ledger status,
   `queue-model.md:482-491`, so this is naturally cross-queue safe).
2. **Persistence** (§1): one file → N files / registry.
3. **Workloop** (§2): one `qs.Queue()` → iterate named queues.
4. **Status reporting**: `queue-status` (`types.go:329-332`,
   `QueueStatusResponse{Queue *Queue}`) returns ONE queue. Multi-queue needs
   either a `--queue <name>` filter on status or a new `queue list` returning all.
5. **Lifecycle file archives** (`CancelQueueOnShutdown`, `ArchiveFailedQueue`):
   per-name archive paths.

`QM-061` (single-orchestrator, `queue-model.md:736-738`) explicitly leans on
QM-027 as the multi-orchestrator safeguard. Relaxing QM-027 does **not** open
multi-orchestrator (still single Pi/agent submitter); it opens multi-*queue*. The
change-spec should note that QM-061's reasoning shifts from "one queue" to "one
submitter, N queues."

---

## 6. Investigate-handoff (G5, C5, SC4) — where it plugs in

### The reflection / dispatch surface
The flywheel's post-run reflection lives in `.pi/extensions/flywheel/bridge.ts`.
The curated-dispatch path the brief names (CL-071/072/073) is the
`Dispatcher` interface (`bridge.ts:44`, implemented in `dispatcher.ts`):
- `bridge.ts:303-307` — on `run_completed` (slot freed) → `triggerEagerRefill`.
- `bridge.ts:342-364` — `triggerEagerRefill` calls `dispatcher.onSlotReleased`,
  which runs `kerf next`, screens candidates, and **appends/submits** beads
  (`dispatcher.ts:onSlotReleased`, ~`dispatcher.ts:275+`).
- `dispatcher.ts:186-217` `makeQueueSubmit` and `dispatcher.ts:219-232`
  `makeQueueAppend` are the two shell-out points to `harmonik queue
  submit|append`. `readActiveQueue` (`dispatcher.ts:140-170`) reads
  `.harmonik/queue.json` directly to find the append target.

### What is NOT there yet
There is **no "file an investigate bead" path** anywhere — neither in `bridge.ts`
nor `dispatcher.ts` nor Go. The current reflection only *refills* the existing
queue from `kerf next`; it never `br create`s a follow-up bead and never targets a
*second* queue. SC4 ("flywheel files an investigate bead and submits it to the
`investigate` queue") is **net-new behavior** to be added to the bridge, gated on
the multi-queue core (Comp A–D) existing first.

**[FRICTION]** `readActiveQueue` (`dispatcher.ts:140-170`) reads the single
`.harmonik/queue.json`. Once persistence multiplexes to per-queue files (Comp A),
this reader must learn the new layout (read `investigate.json` / enumerate the
queues dir) or the handoff appends to the wrong place. Comp E depends on Comp A's
persistence layout being settled.

### Subscription billing (C5)
The investigator must run as **daemon-managed** claude on the subscription, NOT
the Pi API key. That isolation is the `credfence` work
(commit `c1494102`, `spec(flywheel): land credfence`). The daemon spawns claude
via the handler substrate; daemon-spawned claude inherits the subscription
credential path credfence built (per MEMORY `project_flywheel_apikey_burn`). So
the investigate queue's items, once dispatched by the **daemon** workloop (not by
Pi spinning its own agent), are automatically subscription-billed — there is **no
new billing code** needed for Comp E beyond "route the bead through a daemon
queue." This is exactly why the brief insists the handoff go *through a queue*
(N1): it reuses the daemon's existing subscription-billed dispatch.

---

## 7. Constraints to preserve, conventions to follow

### Public interfaces / back-compat (C3)
- Wire records (`QueueSubmitRequest`/`Response`, `QueueAppendRequest`/`Response`,
  `types.go:278-322`) are an N-1-stable contract (ON-018, `queue-model.md:63`).
  New fields (`name`, `workers`) MUST be **additive-optional** (absent = `main` /
  default workers) so old clients and the existing dispatcher.ts keep working.
- `queue.json` schema_version stays `1` if fields are additive-optional
  (`Item.Attempts` etc. already added this way, `types.go:169-176`). A layout
  change (per-queue files) is a *path* change, not a schema-version bump, but
  startup must still read a legacy `queue.json` as `main`.

### Error handling
- Validation failures are typed JSON-RPC errors, never events (QM-028,
  `queue-model.md:567-569`); reason enum is wire-stable (`validation.go:24` etc.,
  error-code table `queue-model.md:598-610`). A new "queue not found" reason (for
  `pause <name>` of a nonexistent queue) would need a `-32019` allocation
  (currently the sole reserved slot, `queue-model.md:609`).
- Persistence I/O errors → `ErrPersistFailed` → daemon degrades (QM-001,
  `persistence.go:24-31`). Multi-file persistence keeps this contract per file.

### Testing patterns
- Scenario tests live in `internal/scenario/*_test.go` (package `scenario_test`)
  and `internal/daemon/scenario_*_test.go`. Harness:
  `internal/daemon/scenariotest/scenariotest.go` — `BootstrapFixture` (isolated
  project root), `CaptureEventStream`/`WaitForEvent`/`AssertEventSequence`
  (`scenariotest.go:101,172,239`), `AssertQueueJSON` (`scenariotest.go:321`),
  `AssertBeadStatus` (`scenariotest.go:449`). The canonical single-queue lifecycle
  test is `internal/scenario/queue_lifecycle_test.go` (T80, hk-8vokz) — the SC1–SC5
  multi-queue scenarios should follow its shape.
- Unit tests sit next to source (`validation_test.go`, `persistence_test.go`,
  `state_test.go`, `queuestore_hkj808w_test.go`).
- `AssertQueueJSON` (`scenariotest.go:321`) reads the single `queue.json` — it
  needs a per-queue variant for multi-queue scenarios.

### Recent git history (relevant)
- `25b7672e` wire QueueOperatorEventConsumer (pause/resume → queue, hk-7urls).
- `5e651c2e` drain queue to cancelled on SIGINT (hk-ppt32; the `cancelled` status).
- `430fe17d` auto-archive queue.json on paused-by-failure (hk-ly4w5).
- `803c2432` wake workloop immediately on queue-submit (hk-24xn1; the `wakeC`).
- `b81a76b3` streamEligible skips dispatched items (hk-9a27q; concurrent stream).
- `c1494102` land credfence + pilot specs (C5 credential path, C4 pilot producer).
- `1379903c` per-queue-item retry-spend budget (hk-c1ah6) — note: this is a
  per-*item* spend cap, NOT a per-*queue* budget; N2 (no per-queue budgets) is
  consistent with the current single global spend meter
  (`internal/daemon/spendmeter_hkk3f8g.go`, credfence).

---

## 8. Feasibility verdict on the problem-space assumptions

- **G1 (N named queues)** — feasible; the holder shape change (`QueueStore` map)
  is mechanical but wide (every `qs` reader is an edit site).
- **G2 (per-queue workers + global cap)** — feasible but the **highest-risk**
  piece: it rewrites the workloop scheduler's single-counter capacity gate
  (`workloop.go:617-623`) into a two-level (per-queue + global) gate AND requires
  a queue-iteration policy that does not exist today. The brief's "this really
  isn't that hard" is true for G1/G3/G4; G2 is where the code resists.
- **G3 (per-queue pause)** — feasible; cleanest as a direct per-named-queue
  `Queue.Status` transition reusing the consumer/workloop status-read path, NOT
  by per-queue-fanning the global `OperatorPauseController` bool.
- **G4 (routing)** — feasible; additive `--queue`/`name` field, default `main`.
- **G5 (investigate handoff)** — feasible but **net-new** in `bridge.ts`
  (no investigate-bead-filing exists today) and dependent on Comp A persistence
  layout (`readActiveQueue` rewrite). Subscription billing is free once the bead
  rides a daemon queue (credfence already isolates daemon-spawned claude).
- **C6 (atomic/single-writer)** — preserved trivially by per-queue files; each
  file keeps the existing `Persist` atomic dance.

No problem-space goal is infeasible. The one assumption that under-states the
work is **G2's "per-queue workers under a global cap"** — it touches the most
load-bearing, most-tested code path (the dispatch capacity gate) and introduces a
scheduling-policy decision the spec does not currently make.
