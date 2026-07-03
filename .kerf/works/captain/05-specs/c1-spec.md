# C1 — `epic_completed` event — Change Spec

> Kerf change-spec pass artifact for the **Captain & Crew** plan, component **C1**.
> An implementing agent follows this verbatim. It describes the change; the implementer
> writes the Go.

> **Line-reference correction (this pass):** the bench docs (02-analysis §A1, 03-components,
> 04-research) cite stale `internal/daemon/workloop.go` line numbers. As of HEAD `2272d9f1`
> the real refs are:
> - `emitBeadClosed` is at **`workloop.go:4020`** (bench said `:3981`; `:3981` is now
>   `emitWorkingTreeRefreshFailed`).
> - The seven `emitBeadClosed` call sites are at **`workloop.go:1813, 1875, 1973, 1986,
>   2525, 2560, 2577`** (bench said `:1784,1846,1934,1947,2486,2521,2538` — all shifted; and
>   the bench listed only 7 of the 7 but with wrong values). Each is immediately after a
>   successful `deps.brAdapter.CloseBead(...)`.
> - `brShowEdge` is at **`internal/brcli/show.go:59-62`** (bench said `:20-23`; `:20-23` is
>   the `ErrBeadNotFound` comment block). `core.DependencyEdge` is at
>   **`internal/core/dependencyedge.go:6`**.
> - `BeadClosedPayload` is at `internal/core/agentlifecyclepayloads_gjyks.go:387-395` —
>   matches the bench.
>
> **Re-verified at current HEAD:** all seven call sites and the supporting refs still
> hold — `emitBeadClosed:4020`, `brShowEdge:59-62`, `DependencyEdge:6`,
> `BeadClosedPayload:387`, `ScanAfter:312`, `registerRunLifecycle:64`, `bead_closed:80`.

---

## 1. Requirements

Carried forward from `03-components.md` C1 for traceability:

- **R-C1.1** A new `epic_completed` event type exists
  (`internal/core/eventtype.go`), with a typed payload
  `{epic_id, last_child_bead_id, closed_at}` (struct in `internal/core`), registered in
  `internal/core/eventreg_hqwn59.go`.
- **R-C1.2** At the bead-close site (`internal/daemon/workloop.go`, right after
  `emitBeadClosed`): if the just-closed bead has a parent via a `parent-child` dep, and that
  parent now has **zero remaining open children**, emit `epic_completed` carrying the parent
  (epic) id, the last child bead id, and the close time.
- **R-C1.3** The event flows through `harmonik subscribe --types epic_completed` unchanged
  (no client-side work needed — the empty-`Types`=all and explicit-type filter both already
  handle any registered type).
- **R-C1.4 (completion query, resolved by R1)** Use `br show <parent> --format json` →
  `dependents[]` with inline `status` and `dependency_type:"parent-child"` — **one call**,
  not N+1. The current adapter (`brShowEdge`) **drops** the child `status` field, so C1 must
  extend it to surface child status.
- **R-C1.5 (idempotent emit, required)** Crew / humans can `br close` out-of-band on their own
  queues — i.e. **outside the daemon's one-at-a-time merge lock** — so two siblings can race
  the "last child closed?" check. Emit MUST be **at-most-once per epic**: an emitted-epics
  guard, seeded from the event log on boot so the guarantee survives a daemon restart.

**Verifies success-criterion #4** (`01-problem-space.md`): *"When the last child bead of an
assigned epic closes, an `epic_completed` event fires carrying the epic id, and a captain
subscribed to it receives the notification."*

---

## 2. Research summary (R1 findings that drive the approach)

From `04-research/findings.md` R1, re-confirmed against HEAD:

- **One-call completion query.** `br show <parent> --format json` returns every child of the
  parent in `dependents[]`, each entry carrying its own `status` and
  `dependency_type`. The daemon never needs to fan out one `ShowBead` per child.
- **The adapter drops `status`.** `brShowEdge` (`show.go:59-62`) parses only `id` +
  `dependency_type`; `core.DependencyEdge` (`dependencyedge.go:6`) carries only
  `FromBeadID`, `ToBeadID`, `EdgeKind` — no status. So "are all children closed?" is
  unanswerable from the current `BeadRecord.Edges`. C1 adds a status field to both.
- **Edge direction (confirmed from `show.go:160-184` + the `Parent` godoc at `show.go:51-54`).**
  For the just-closed *child* bead, its parent-child link appears in the **child's
  `dependencies[]`** → parsed as `DependencyEdge{FromBeadID: child, ToBeadID: parent,
  EdgeKind: parent-child}`. For the *parent* (epic), its children appear in the parent's
  **`dependents[]`** → `DependencyEdge{FromBeadID: child, ToBeadID: parent,
  EdgeKind: parent-child}`. So: find the parent from the closed bead's outgoing
  parent-child edge; enumerate siblings from the parent's incoming parent-child edges.
- **Idempotency strategy.** After `CloseBead` + `emitBeadClosed`, re-read the parent; if all
  children are closed AND no `epic_completed` was already emitted for this epic, emit. Keep an
  emitted-epics set (in-memory), seeded by scanning `.harmonik/events/events.jsonl` for prior
  `epic_completed` on boot.
- **Reusable scan primitive.** `eventbus.ScanAfter(path, sinceID)` (`internal/eventbus/
  jsonlwriter.go:312`) already streams `core.Event` from the JSONL log; passing an empty
  `core.EventID` (zero value) scans from the beginning. This is exactly how
  `commsrecvhandler` and `subscribe` replay the log; reuse it for the boot scan rather than
  hand-rolling a reader.

---

## 3. Approach (locked decisions + rationale)

### D1 — Event type + payload (locked)

- Add `EventTypeEpicCompleted EventType = "epic_completed"` to
  `internal/core/eventtype.go`, in the §8.1-adjacent run-lifecycle const block (next to
  `EventTypeBeadClosed`, since it is emitted on the same close path).
- Add `EpicCompletedPayload` to `internal/core` (place it beside `BeadClosedPayload` in
  `agentlifecyclepayloads_gjyks.go`, its sibling on the close path):

  ```go
  type EpicCompletedPayload struct {
      EpicID          BeadID `json:"epic_id"`            // parent bead whose last child just closed
      LastChildBeadID BeadID `json:"last_child_bead_id"` // the child whose close triggered completion
      ClosedAt        string `json:"closed_at"`          // RFC3339 close time of the triggering child
  }
  func (p EpicCompletedPayload) Valid() bool {
      return p.EpicID != "" && p.LastChildBeadID != "" && p.ClosedAt != ""
  }
  ```
  - **Rationale (typed struct, not map):** every event payload in `internal/core` is a typed
    struct with a `Valid()` method and is registered via a constructor — see
    `BeadClosedPayload`, `agentlifecyclepayloads_gjyks.go:387`. Following the established
    convention keeps registry validation (EV-032/EV-034) and the redaction/seal machinery
    working with zero special-casing.
  - **Rationale (`ClosedAt` as string, RFC3339):** mirrors the surrounding payloads' string
    timestamps and avoids a `time.Time` JSON-zero-value trap; the daemon stamps it from the
    triggering child's close at emit time (same wall-clock read the helper already does).
  - **Durability class: O (ordinary).** `epic_completed` is reconstructible from the
    `bead_closed` sequence + the ledger's parent-child edges, and is a captain-coordination
    *notification*, not a terminal-state ledger landmark. (Contrast `bead_closed`, class F.)
    Record this in the `eventreg_hqwn59.go` doc comment.
- Register in `eventreg_hqwn59.go` inside `registerRunLifecycle()` (the close-path section),
  right after the `bead_closed` registration:
  ```go
  mustRegister("epic_completed", func() EventPayload { return &EpicCompletedPayload{} })
  ```

### D2 — Emit at the bead-close site (locked)

- Add a daemon-side emit helper next to `emitBeadClosed` (`workloop.go:4020`), plus a
  daemon-side payload struct beside `beadClosedPayload` (`workloop.go:3419`):
  ```go
  type epicCompletedPayload struct {
      EpicID          string `json:"epic_id"`
      LastChildBeadID string `json:"last_child_bead_id"`
      ClosedAt        string `json:"closed_at"`
  }
  ```
  (Daemon-local marshalling structs mirror `core` payloads everywhere on this path —
  `beadClosedPayload`, `workingTreeRefreshFailedPayload`, etc. — so we keep that pattern.)
- New helper `maybeEmitEpicCompleted(ctx, deps, runID, closedBeadID)`:
  1. `parent, ok := closedBeadParent(...)` — look up the just-closed bead's parent-child edge.
     Resolve it from the closed bead's `ShowBead` result: scan `record.Edges` for an edge with
     `FromBeadID == closedBeadID && EdgeKind == EdgeKindParentChild` → `ToBeadID` is the parent.
     **Note:** the close path already had a `ShowBead`-free flow; this is a net-new read. The
     close path does not retain the bead record, so the helper issues `ShowBead(closedBeadID)`
     (to get the parent edge), then `ShowBead(parent)` (to enumerate siblings + statuses). Two
     `br show` calls total per close — acceptable (close is not hot-path; merges are
     serialized). If `ok == false` (no parent) → return, emit nothing.
  2. `siblings := deps.brAdapter.ShowBead(ctx, parent).Edges` filtered to
     `EdgeKind == EdgeKindParentChild && ToBeadID == parent` (the parent's children). Read each
     child's **new `ChildStatus` field** (D3). If ANY child status != closed → return (epic not
     done).
  3. All children closed → consult the emitted-epics guard (D4). If `parent` already recorded →
     return (at-most-once). Otherwise record `parent` in the guard **then** emit
     `epic_completed{epic_id: parent, last_child_bead_id: closedBeadID, closed_at: now}` via
     `deps.bus.EmitWithRunID(ctx, runID, core.EventTypeEpicCompleted, payload)`.
     - **Use `EmitWithRunID`** (not bare `Emit`): the completion is causally scoped to the run
       that closed the last child, so the envelope's `run_id` join-key is populated per EM-013
       — matching `emitWorkingTreeRefreshFailed`/`emitMergeBuildFailed`, which also use
       `EmitWithRunID`. (`emitBeadClosed` itself uses bare `Emit`; we deliberately upgrade the
       new helper to `EmitWithRunID` so subscribers can correlate.)
- Call `maybeEmitEpicCompleted(ctx, deps, runID, beadID)` immediately after each of the seven
  `emitBeadClosed(ctx, deps.bus, runID, beadID)` calls — `workloop.go:1813, 1875, 1973, 1986,
  2525, 2560, 2577`. **Rationale (all seven, not one):** every one of those sites is a genuine
  close that can be the last child of an epic (review-loop APPROVE, budget-exhausted close, DOT
  success, DOT noChange-subsumed, agent_completed, auto-close exit-0, noChange-subsumed). A
  centralized wrapper around `emitBeadClosed` would be cleaner but would require touching the
  helper's signature to thread `deps`; the implementer MAY instead introduce a tiny
  `emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)` wrapper that calls both and replace
  the seven call sites with it (preferred — single insertion point, no missed site). Either is
  acceptable; the wrapper is the lower-risk choice and is RECOMMENDED.

### D3 — `brShowEdge` status surfacing (locked)

The adapter must stop dropping the child `status`.

**`internal/brcli/show.go` — `brShowEdge` (`:59-62`)**

Before:
```go
type brShowEdge struct {
    ID             string `json:"id"`
    DependencyType string `json:"dependency_type"`
}
```
After:
```go
type brShowEdge struct {
    ID             string `json:"id"`
    DependencyType string `json:"dependency_type"`
    Status         string `json:"status"` // child/edge bead status, inline in br show dependents[]/dependencies[]
}
```

**`internal/core/dependencyedge.go` — `DependencyEdge` (`:6`)**

Before:
```go
type DependencyEdge struct {
    FromBeadID BeadID
    ToBeadID   BeadID
    EdgeKind   EdgeKind
}
```
After:
```go
type DependencyEdge struct {
    FromBeadID  BeadID
    ToBeadID    BeadID
    EdgeKind    EdgeKind
    // EndpointStatus is the coarse status of the *other* endpoint bead as reported
    // inline by `br show` in dependents[]/dependencies[]. For an incoming edge
    // (dependent) it is the child's status; for an outgoing edge (dependency) it is
    // the dependency's status. May be empty when the source did not supply a status
    // (older br, or an edge synthesized without a show call) — callers MUST treat
    // empty as "unknown", not "closed".
    EndpointStatus CoarseStatus
}
```
- **`DependencyEdge.Valid()`** is unchanged: `EndpointStatus` is additive and optional, so the
  existing two-IDs-and-kind validity rule still holds. **Do not** add `EndpointStatus` to
  `Valid()` (would break every existing edge that has no status, e.g. edges built in tests).

**`internal/brcli/show.go` — `ShowBead` edge construction (`:162-184`)**

In both the `item.Dependencies` loop and the `item.Dependents` loop, parse the new status into
the edge. Reuse the existing `CoarseStatus.UnmarshalText` pattern that `ShowBead` already uses
for the top-level bead status (`show.go:151-154`), but **tolerate empty**: if `dep.Status == ""`
leave `EndpointStatus` as the zero `CoarseStatus` (treated as unknown), and only `UnmarshalText`
when non-empty so a non-status edge does not become a parse error.

```go
edge := core.DependencyEdge{
    FromBeadID: id,                  // (or core.BeadID(dep.ID) in the dependents loop)
    ToBeadID:   core.BeadID(dep.ID), // (or id in the dependents loop)
    EdgeKind:   kind,
}
if dep.Status != "" {
    var st core.CoarseStatus
    if stErr := st.UnmarshalText([]byte(dep.Status)); stErr != nil {
        return core.BeadRecord{}, fmt.Errorf("brcli.ShowBead: edge %q status: %w", dep.ID, stErr)
    }
    edge.EndpointStatus = st
}
edges = append(edges, edge)
```
- **Rationale (extend the existing struct, no new method):** the bench floated "extend the edge
  struct OR add a method." A field is strictly simpler, is the convention (`DependencyEdge` is a
  flat record), and a method would still need backing state to return. Extend the struct.

The completion check in D2 reads `EndpointStatus == core.CoarseStatusClosed`. Any child with
`EndpointStatus` empty/unknown counts as **NOT closed** (fail-safe: never emit completion on
ambiguous data).

### D4 — Idempotency / at-most-once guard (locked)

- **Where it lives:** a new field on `workLoopDeps` (`workloop.go:98`), the bundle already
  threaded into every close branch:
  ```go
  // emittedEpics guards at-most-once epic_completed emission per epic id (R-C1.5).
  // Out-of-band br close (crew on own queues / humans) bypasses the daemon's
  // one-at-a-time merge lock, so two siblings can race the all-children-closed
  // check. A parent id is recorded here BEFORE the emit; a sibling that loses the
  // race sees it recorded and emits nothing. Seeded on boot from a scan of
  // events.jsonl for prior epic_completed events so the guarantee survives a
  // daemon restart. Guarded by emittedEpicsMu.
  emittedEpics   map[core.BeadID]struct{}
  emittedEpicsMu *sync.Mutex
  ```
  - **Rationale (on `workLoopDeps`, not a package global):** matches the existing rule for
    `runRegistry` ("MUST be a field on workLoopDeps — NOT a package-level variable", `:155`).
    The map is shared across all concurrent close goroutines, so it needs the mutex; reuse a
    `*sync.Mutex` set unconditionally in `newWorkLoopDeps` (same pattern as `mergeMu`,
    `daemon.go:512`).
- **Read-after-write ordering (critical — this is the race fix):** inside
  `maybeEmitEpicCompleted`, after the all-children-closed check passes, do the
  check-and-set under the lock as one critical section:
  ```
  lock emittedEpicsMu
    if parent in emittedEpics: unlock; return            // lost the race → no emit
    emittedEpics[parent] = {}                            // claim BEFORE emit
  unlock
  emit epic_completed                                    // emit OUTSIDE the lock
  ```
  - The claim (`map insert`) MUST happen **before** the `bus.Emit` and the check+insert MUST be
    atomic under the same lock acquisition, so two siblings that both observe "all children
    closed" cannot both pass the guard. The emit itself is done outside the lock to avoid
    holding the mutex across I/O.
  - **Note on the residual all-children-closed window:** the all-children-closed *query*
    (two `br show` reads) is NOT inside the lock. Two siblings can both read "all closed" and
    both reach the guard; the guard's atomic check-and-set is what makes exactly one win. That
    is the intended design — the guard, not the query, is the serialization point. Do not try
    to hold the lock across the `br show` calls (would serialize all closes and could deadlock
    against the merge mutex).
- **Boot seeding:** in `daemon.Start` (after the JSONL writer opens, `daemon.go:738-743`, and
  before/at `newWorkLoopDeps`, `daemon.go:1391`), scan the existing log:
  ```
  seed := map[core.BeadID]struct{}{}
  for ev := range eventbus.ScanAfter(cfg.JSONLLogPath, core.EventID{}) {  // zero id = from start
      if ev.Type != string(core.EventTypeEpicCompleted) { continue }
      var p core.EpicCompletedPayload
      if json.Unmarshal(ev.Payload, &p) == nil && p.EpicID != "" {
          seed[p.EpicID] = struct{}{}
      }
  }
  ```
  Pass `seed` (and a fresh `*sync.Mutex`) into `newWorkLoopDeps` / the deps struct. When
  `cfg.JSONLLogPath == ""` (tests with no log), seed an empty map — the guard still works
  in-process for the session.
  - **Rationale (scan, not a separate persisted file):** the event log is already the durable
    record of what was emitted; a second persisted set would be a redundant store that could
    drift from the log. `ScanAfter` is the same primitive `subscribe`/`comms recv` use for
    replay, so this adds no new I/O surface. The scan is O(log size) once at boot — negligible.

---

## 4. Files & changes

| File | Create/Modify | Change |
|------|---------------|--------|
| `internal/core/eventtype.go` | Modify | Add `EventTypeEpicCompleted = "epic_completed"` const + doc comment (durability class O) in the run-lifecycle block near `EventTypeBeadClosed` (`:80`). |
| `internal/core/agentlifecyclepayloads_gjyks.go` | Modify | Add `EpicCompletedPayload` struct + `Valid()` beside `BeadClosedPayload` (`:387`). |
| `internal/core/eventreg_hqwn59.go` | Modify | Register `epic_completed` in `registerRunLifecycle()` after the `bead_closed` line (`:80`); add it to the durability-class doc comment. |
| `internal/core/dependencyedge.go` | Modify | Add `EndpointStatus CoarseStatus` field to `DependencyEdge` (`:6`); `Valid()` unchanged. |
| `internal/brcli/show.go` | Modify | Add `Status string` to `brShowEdge` (`:59-62`); parse it (tolerating empty) into `DependencyEdge.EndpointStatus` in both edge loops (`:162-184`). |
| `internal/daemon/workloop.go` | Modify | (a) Add `epicCompletedPayload` struct beside `beadClosedPayload` (`:3419`); (b) add `maybeEmitEpicCompleted` (+ optional `emitBeadClosedAndMaybeEpic` wrapper) helper near `emitBeadClosed` (`:4020`); (c) add `emittedEpics`/`emittedEpicsMu` fields to `workLoopDeps` (`:98`); (d) call the wrapper at the seven close sites (`:1813,1875,1973,1986,2525,2560,2577`). |
| `internal/daemon/daemon.go` | Modify | In `Start`, boot-scan `cfg.JSONLLogPath` via `eventbus.ScanAfter` for prior `epic_completed`, build the seed set + a `*sync.Mutex`, and thread both into `newWorkLoopDeps` (`:1391`). |
| `internal/daemon/workloop.go` (`newWorkLoopDeps`, `:493`) | Modify | Accept/set `emittedEpics` + `emittedEpicsMu` (default to empty map + fresh mutex when not provided). |
| `internal/daemon/export_test.go` (`ExportedWorkLoopDeps`, `:253`) | Modify | Default `emittedEpics` to an empty map + fresh mutex so test fixtures don't nil-panic. |
| `internal/core/epiccompleted_test.go` | **Create** | Table-driven `EpicCompletedPayload.Valid()` + registry round-trip test. |
| `internal/brcli/show_endpointstatus_test.go` | **Create** | Table-driven `ShowBead` test: a `dependents[]` entry with inline `status` populates `EndpointStatus`; empty status → unknown (zero); bad status → error. |
| `internal/daemon/epiccompleted_scenario_<bead>_test.go` | **Create** | `//go:build scenario` daemon-level test for emit + at-most-once (see §6). |

No spec-text file under `specs/` is required by C1 itself, but the implementer SHOULD add an
`epic_completed` row to `specs/event-model.md` §8 (taxonomy) + a payload-fields note, since the
event registry's doc comments reference `event-model.md §8`. Flag to operator if the §8 table is
treated as frozen.

---

## 5. Acceptance criteria (concrete, testable)

- **AC-1 (maps to success-criterion #4 — primary).** Closing the **last open child** of an
  epic emits **exactly one** `epic_completed` event whose `epic_id` is the parent, whose
  `last_child_bead_id` is the just-closed child, and whose `closed_at` is non-empty. A
  subscriber on `harmonik subscribe --types epic_completed` receives it once.
- **AC-2 (sibling race / duplicate close → zero extra).** Given an epic with two children where
  one closes via the daemon and the other closes out-of-band, **two close events, in any order,
  produce exactly one emit** — the at-most-once guard (D4), not the all-children-closed query,
  is the serialization point. A repeat `br close` of an already-closed last child (idempotent
  re-close) emits **zero** additional `epic_completed`. (Optional stretch: a `t.Parallel`
  goroutine race sub-test that fires both checks concurrently and asserts exactly one emit.)
- **AC-3 (not-yet-complete → no emit).** Closing a child when the parent still has ≥1 open
  child emits **zero** `epic_completed`.
- **AC-4 (no parent → no emit).** Closing a bead with no `parent-child` parent edge (a
  standalone bead, or a top-level epic itself) emits **zero** `epic_completed` and produces no
  error in the close path.
- **AC-5 (boot survives restart).** After an `epic_completed` for epic E is in the log, a fresh
  daemon boot seeds E into `emittedEpics`; a subsequent last-child re-close of E emits **zero**
  additional `epic_completed`.
- **AC-6 (status surfacing).** `ShowBead` on a parent returns `Edges` where each `parent-child`
  dependent carries a non-empty `EndpointStatus` reflecting the child's `br show` status; a
  closed child shows `CoarseStatusClosed`.
- **AC-7 (additive, non-breaking).** All existing `brcli` and `core` tests pass unchanged
  (`DependencyEdge.Valid()` semantics unchanged; existing subscribers unaffected).

---

## 6. Verification (exact commands / tests)

Per `02-analysis` conventions: table-driven Go unit tests beside the code; daemon-level
behavior gets a `//go:build scenario` test (the daemon merge gate **skips** scenario tests —
**run them yourself**).

- **Core payload + registry (unit):**
  `go test ./internal/core/ -run 'EpicCompleted|EventRegistry'`
  Asserts `Valid()` table cases and that `epic_completed` constructs from the registry.
- **Adapter status surfacing (unit):**
  `go test ./internal/brcli/ -run 'ShowBead.*Endpoint|EndpointStatus'`
  Feeds a spy `br` whose JSON has `dependents[]` with inline `status`; asserts
  `EndpointStatus` is parsed; asserts empty-status → zero/unknown; bad status → error.
- **Daemon emit + idempotency (scenario):**
  `go test -tags scenario ./internal/daemon/ -run 'EpicCompleted'`
  Boots a daemon with a stub `beadLedger` whose `ShowBead` returns a parent with one
  then-closed child; drives a close through one of the close branches; asserts exactly one
  `epic_completed` on the bus. A second sub-test drives two concurrent closes of two children
  of the same parent (one via the daemon path, one simulating out-of-band by directly invoking
  the helper) and asserts the bus sees the event **once**. A third sub-test pre-writes an
  `epic_completed` to the log, boots, and asserts the boot scan seeds the guard (re-close →
  zero).
- **Full package gates (regression):**
  `go test ./internal/core/ ./internal/brcli/ ./internal/daemon/` (non-scenario)
  to confirm AC-7 (nothing else breaks).
- **Subscribe flows for free (manual / no new code):** with a live daemon,
  `harmonik subscribe --types epic_completed --json` surfaces the event end-to-end — no
  client change (R-C1.3). Note in the bead that this is a manual confirmation, not a gated test.

---

## 7. Error handling & edge cases

- **Parent query fails (`br show <parent>` errors / `ShowBead` returns err).** Log to stderr
  (same `fmt.Fprintf(os.Stderr, ...)` convention as the close branches) and **return without
  emitting**. The close itself already succeeded and `bead_closed` already fired; a failed
  completion check must NOT fail the run or reopen the bead. Completion is recoverable: a later
  close of any sibling re-runs the check.
- **Bead has no parent.** `closedBeadParent` returns `ok=false` → return, no emit (AC-4). This
  is the common case (most beads are not epic children); the helper must be cheap on this path
  — but it still costs one `ShowBead(closedBead)`. (Acceptable; close is serialized behind the
  merge lock and is not throughput-critical.)
- **Multiple epics complete from one close-cascade.** C1 handles the closed bead's **direct**
  parent only — one epic per close. If closing the last child also makes a grandparent's last
  child (the parent epic) close, that is a *separate* `br close` of the parent and will drive
  its own completion check when it happens. C1 does NOT walk up the chain in a single close.
- **Daemon restart mid-emit.** If the daemon dies between the guard-claim and the `bus.Emit`,
  the in-memory claim is lost AND no event hit the log → on reboot the boot scan finds no
  `epic_completed` for that epic, so the guard is unseeded and a later sibling re-close re-runs
  the check and emits. This is the **at-least-once-on-crash, at-most-once-steady-state**
  posture, which is correct: a crash before the durable event is exactly when we DO want a
  retry. (If the daemon dies *after* the durable append, the boot scan seeds the guard and no
  duplicate fires.)
- **Nested epics (a child whose parent is itself a child of a grandparent epic).** **OUT OF
  SCOPE for C1** — see Open Questions. C1 fires `epic_completed` for the **direct** parent of
  the closed bead only. A grandparent epic gets its own `epic_completed` only when *its* own
  last direct child (the sub-epic bead) is closed. The captain receiving a sub-epic's
  completion is the intended Phase-1 behavior; multi-level roll-up is judgment-layer territory
  (explicitly out per `03-components.md` "Explicitly out").
- **Empty/unknown child status.** Treated as NOT closed (fail-safe) — completion only fires
  when every child reports `closed`. An older `br` that omits `status` therefore never spuriously
  fires completion; it just never fires until statuses are present.

---

## 8. Migration / back-compat

- **Event type is additive.** `epic_completed` is a brand-new registered type. Existing
  subscribers that filter `--types <list-without-epic_completed>` never see it; the empty-types
  (=all) subscribers see it as one more line — no schema change to any existing event.
- **`DependencyEdge.EndpointStatus` is additive and optional.** `Valid()` is unchanged; every
  existing construction site (tests, DOT graph code, reconciliation) compiles untouched and the
  new field defaults to the zero `CoarseStatus`. No existing consumer reads it.
- **`brShowEdge.Status` is additive parse-only.** Adding a JSON field the older `br` doesn't emit
  is harmless (`json.Unmarshal` leaves it ""); a newer `br` that does emit it now flows through.
- **No data migration, no event-log rewrite.** The boot scan is read-only over the existing
  log. A daemon that has never emitted `epic_completed` simply seeds an empty guard.
- **Spec note:** if `specs/event-model.md §8` is amended with the new row, that is an additive
  taxonomy row (EV-029 "N-1 readable" unaffected since no envelope-schema bump).

---

## OPEN QUESTIONS FOR OPERATOR

1. **Nested-epic roll-up scope.** This spec fires `epic_completed` for the **direct** parent of
   the closed bead only; a grandparent epic gets its own event only when its own last direct
   child closes (so a captain owning a multi-level epic tree gets a completion per level, not a
   single top-level one). Decompose/components mark roll-up logic as judgment-layer (out of
   scope), so C1 stays single-level. **Confirm single-level is the intended Phase-1 behavior**,
   or whether the captain context (C4) must explicitly handle receiving sub-epic completions.

2. **Guard persistence model.** The at-most-once guard is in-memory, seeded by a boot scan of
   `events.jsonl` — deliberately *not* a second persisted file (rationale §D4). The known
   consequence: a crash **between** the guard-claim and the durable `bus.Emit` yields an
   at-least-once retry on reboot (one possible duplicate per epic, only on a crash in that exact
   window). I judged that acceptable (a crash before the durable event is exactly when a retry
   is wanted). **Confirm at-least-once-on-crash is acceptable**, or whether C1 must make the
   claim itself durable (would add a persisted set + its own drift risk).

3. **`specs/event-model.md §8` amendment.** The registry doc comments reference the §8 taxonomy
   table as the source of truth for event rows + durability classes. Adding `epic_completed`
   implies adding a §8 row (durability class O). **Confirm the implementer should amend §8**
   (additive, EV-029-safe) as part of C1, or whether §8 edits are gated to a separate spec pass.

4. **Two `br show` calls per close.** The helper does `ShowBead(closedBead)` (find parent) +
   `ShowBead(parent)` (enumerate siblings). For beads with no parent that's one wasted `br show`
   on every single close. Closes are serialized behind the merge lock so throughput isn't at
   risk, but if `br show` latency is a concern at scale, an alternative is to thread the
   already-fetched `BeadRecord` from the claim/dispatch path down to the close site to skip the
   first call. **Confirm two-calls-per-close is fine for Phase 1**, or whether to plumb the
   cached record (larger change, touches the dispatch→close data flow).
