# extqueue — Decomposition

Mapping the goals/decisions from `01-problem-space.md` onto concrete spec files. Each entry describes scope of change, what the spec should say after, and cross-spec dependencies.

## Spec change matrix

| # | Spec | Status | Change scope |
|---|------|--------|--------------|
| 1 | `specs/queue-model.md` | **new** | Foundational. Queue data model, group primitives, serialization, persistence, validation. |
| 2 | `specs/execution-model.md` | edit | Dispatch loop now consumes the queue, not `br ready`. Group-boundary semantics. Failure-pause behavior. |
| 3 | `specs/beads-integration.md` | edit | Demote `br ready` from "daemon input" to "tool the orchestrator uses". Ledger remains authoritative for `blocks` edges and bead status. Validation surface on submit. |
| 4 | `specs/process-lifecycle.md` | edit | New `hk queue` CLI subcommand family. Daemon-side Unix socket lifecycle: bind on start, clean shutdown, path policy, single-orchestrator-per-daemon. |
| 5 | `specs/event-model.md` | edit | New event types for queue lifecycle. Identity fields so the orchestrator can correlate events to its submission. |
| 6 | `specs/operator-nfr.md` | edit | Reconcile ON-015 ("Beads is the queue"), ON-008/ON-027 drain step 1, ON-009a needs-attention drain, ON-013a `enqueue` panic barrier, ON-018 N-1 compat (queue.json), ON-026 config inventory (retire `queue-empty re-query cadence`). |
| 7 | `specs/scenario-harness.md` | edit (small) | Scenario tests seed the queue via CLI rather than presupposing `br ready` output. Test-only impact. |
| 8 | `specs/workspace-model.md` | review only | No expected change; verify that worktree creation per-dispatch is queue-source-agnostic. |

## Per-spec requirements

### 1. `specs/queue-model.md` — NEW

The foundational spec. Other specs reference it.

After this change, the spec describes:

- **Queue identity.** A queue is owned by exactly one running daemon. There is at most one active queue per daemon at any time. Queue is mutable in place; "submit" replaces the queue, "append" extends a specific stream group.
- **Group primitives.** Two kinds: wave (closed set, parallel-bounded) and stream (open-ended ordered list, slot-streaming). Encoding: a queue is an ordered array of groups; each group is `{kind: "wave"|"stream", items: [bead_id, ...]}` or some equivalent shape.
- **Group lifecycle.** A group is in one of: `pending`, `active`, `complete-success`, `complete-with-failures`, `paused`. Transitions: pending → active when previous group is complete; active → complete-* when every dispatched item is terminal AND (for streams) the items list is empty; complete-with-failures → paused implicit; paused → pending when orchestrator calls `resume`.
- **Group-advance rule.** Next group becomes `active` only when current group is `complete-success`. `complete-with-failures` → daemon does not advance; queue is paused.
- **Stream append semantics.** Orchestrator may append items to an active or pending stream. Appended items run on slot availability. Appending to a wave is rejected. Appending to a complete group is rejected.
- **Validation rules (submit + append + dry-run).** All listed bead-ids exist in the ledger; none are already closed; none are in another group of this submission; none are currently in `in_progress` from outside this queue. Plus: each bead's `blocks-on` edges are inspected; if any blocker is in the same group, the validator surfaces a `parallelism_narrowed` notice (informational, not rejecting).
- **Relationship to `operator-nfr.md` §4.4 ON-015 ("Beads is the queue").** The existing framing treats Beads as the queue with harmonik holding a thin overlay. Under extqueue, this framing changes: **Beads is the catalog of work; the daemon's queue is the execution plan layered on top.** Ledger remains authoritative for bead identity, status, and `blocks` edges; the queue is authoritative for what runs and in what shape. The ON-015 prose must be amended to reflect this split (see entry 6 below).
- **Persistence.** The in-memory queue is the authority. The daemon writes a JSON file (`.harmonik/queue.json`) on every mutation, atomically (write-temp + rename). On daemon start, if `.harmonik/queue.json` exists, the daemon loads it and resumes. Loaded queue retains its pause/active state.
- **Serialization format.** JSON document with a small, stable schema. Versioned (`schema_version: 1`) so we can iterate.

Dependencies: none (foundational). Drives 2-6.

### 2. `specs/execution-model.md` — edit

After this change:

- The dispatch loop's input is the queue, not `br ready`. The Ready-poll step (workloop.go:391-421) is replaced by a "pull next eligible item from active group" step. Algorithm sketch in the spec: examine active group, find an item that has reached the head of dispatch eligibility (not blocked by ledger, slot available under `--max-concurrent`), dispatch.
- Group-boundary semantics: when the active group reaches `complete-success`, advance to the next group; on `complete-with-failures`, transition queue to `paused` and emit the queue-paused event.
- Ledger-blocking inside a group: when the next item is ready but the ledger says it's blocked on another bead, the dispatcher emits `queue_item_deferred_for_ledger_dep` and waits. This serializes within a group when the ledger demands it.
- The Step-9 terminal-event logic (CloseBead vs ReopenBead on exit) is unchanged in shape, but adds: on terminal, emit `queue_item_completed` or `queue_item_failed` carrying the queue group id and item index.
- Backward compat: there is no internal mode that falls back to `br ready`. The daemon refuses to dispatch when no queue is loaded — it just polls for a queue submission.

Dependencies: queue-model (1). Cross-references: event-model (5) for new event payloads.

### 3. `specs/beads-integration.md` — edit

After this change:

- The spec acknowledges that `br ready` is no longer a daemon input. It remains a tool the orchestrator agent uses to *decide* what to queue.
- The daemon's read surface on beads is: `br show <id>` during validation (existence + status check) and watching for `blocks`-edge resolution during in-flight dispatch.
- The daemon's write surface is unchanged: ClaimBead (on dispatch), CloseBead / ReopenBead (on terminal), per existing semantics.
- A new section: "Validation contract" — what the daemon checks on `hk queue submit` (existence, status not-closed, not in another active queue group). Failure modes returned to CLI.

Dependencies: queue-model (1) for the validation rules. Light edit otherwise.

### 4. `specs/process-lifecycle.md` — edit

After this change:

- A new section on the `hk queue` CLI subcommand family. Subcommands at minimum: `submit <queue-file>`, `status`, `append <group-index> <bead-id ...>`, `remove <bead-id>`, `pause`, `resume`, `clear`, `dry-run <queue-file>`. Each subcommand's exit codes and stdout shape described.
- **Transport reuses the existing daemon socket** at `.harmonik/daemon.sock` per PL-003 + PL-003a (JSON-RPC 2.0 over NDJSON, 0600 perms, filesystem-permission auth). No new socket file is introduced.
- The PL-003a JSON-RPC method-set inventory is extended with queue methods: `queue.submit`, `queue.status`, `queue.append`, `queue.remove`, `queue.pause`, `queue.resume`, `queue.clear`, `queue.dry-run`. The existing `enqueue` operator command (named in PL-003a, ON-013a, ON-041, `harmonik attach`) is **reframed**: either retired in favor of `queue.append` to the active stream, or kept as a thin alias — design pass decides. The retire path is preferred for naming consistency; the alias path keeps backward compat with operator muscle memory.
- Behavior when daemon is not running: CLI subcommands exit non-zero with a clear "daemon not running" message. Exit-code category reuses the existing PL/ON taxonomy (no new category needed in §8 of operator-nfr).
- The `bind_socket` mechanism row in PL §4.6 already lists the io-determinism / replay-safety profile; queue methods inherit it.

Dependencies: queue-model (1) for what `submit` accepts. Cross-references operator-nfr (6) for ON-013a panic-barrier coverage of queue methods and ON-041 for the command-set inventory.

### 5. `specs/event-model.md` — edit

After this change, new event types defined (added to the registered-source matrix per existing EV-016 pattern):

- `queue_submitted` — fired on `hk queue submit` accept. Payload: queue id (UUIDv7), group count, total bead count.
- `queue_group_started` — fired when a group transitions to `active`. Payload: queue id, group index, group kind, item count.
- `queue_item_dispatched` — fired when a queued bead is claimed and the agent is launched. Payload: queue id, group index, bead id, run id.
- `queue_item_completed` — fired in tandem with `run_completed{success:true}`. Payload: queue id, group index, bead id, run id.
- `queue_item_failed` — fired in tandem with `run_completed{success:false}` or `run_failed`. Payload: queue id, group index, bead id, run id, reason summary.
- `queue_group_completed` — fired when a group reaches `complete-success` or `complete-with-failures`. Payload: queue id, group index, success count, fail count, final status.
- `queue_paused` — fired when the queue stops advancing due to a failed group. Payload: queue id, group index, fail count.
- `queue_resumed` — fired on `hk queue resume`. Payload: queue id.
- `queue_item_deferred_for_ledger_dep` — informational, fired when a queued bead can't dispatch yet because of an open ledger `blocks` edge. Payload: queue id, group index, bead id, blocker bead id.

Existing `run_started` / `run_completed` / `run_failed` events SHOULD additionally carry `queue_id` and `queue_group_index` when the run originated from a queued submission — this is the only change to existing event shapes. (Open: spec the field as optional vs required; design pass to settle.)

Dependencies: queue-model (1) for queue id semantics. Cross-references execution-model (2).

### 6. `specs/operator-nfr.md` — edit

After this change, the following ON-IDs are reconciled with extqueue semantics:

- **ON-015 "Beads is the queue; overlay schema is harmonik's half"** — amended: Beads is the catalog of work; the daemon's queue (per queue-model.md) is the execution plan. The overlay schema framing is retained for the `blocks`/`labels`/`status` fields harmonik writes; what changes is the claim that Beads holds dispatch order. Dispatch order moves to the daemon queue.
- **ON-008 / ON-027 drain step (1)** — "orchestrator stops pulling new tasks from the queue" — re-anchored. Under extqueue the daemon stops *advancing the queue* (no new dispatches; in-flight beads finish their checkpoint per existing semantics). Drain step (1) language updated to reference queue-state `paused-by-drain` rather than poll-cessation.
- **ON-009a "Needs-attention queue drain discipline"** — clarified: "queue" in this ON refers to the ledger's needs-attention beads (a Beads-side concept), not the extqueue execution queue. Add a disambiguation note so future readers don't confuse the two.
- **ON-013a "Per-command supervision"** — extended: the panic barrier MUST cover the new `queue.*` JSON-RPC methods. The `enqueue` operator-command name in the existing enumeration is updated per the entry-4 decision (retire vs alias).
- **ON-018 "N-1 compatibility window"** — the versioned-artifact enumeration adds `.harmonik/queue.json` (the persisted queue) with its own `schema_version` field per queue-model.md.
- **ON-026 / config inventory (§4.4)** — the "queue-empty re-query cadence" knob (currently listed as a `[process-lifecycle.md §4.4]` reference) is retired from the inventory because the daemon no longer polls. The slot is removed, not reassigned.
- **ON-041 command-set inventory** — extended with the `queue.*` method names. The `enqueue` row is updated per the entry-4 decision.

Dependencies: queue-model (1) for the on-disk schema name and version, process-lifecycle (4) for method names. Most edits are surgical (single-paragraph amendments).

### 7. `specs/scenario-harness.md` — edit (small)

After this change:

- Scenario tests no longer assume `br ready` is the daemon's input. The harness seeds the queue via `hk queue submit` (or a programmatic equivalent the harness uses) before letting the daemon run.
- Existing scenarios that test selection ordering are either removed (selection is no longer harmonik's job) or rewritten as queue-submission tests.

Dependencies: process-lifecycle (4) for CLI, queue-model (1) for submission format. Test-only impact.

### 8. `specs/workspace-model.md` — review only

The change to queue-driven dispatch should not affect worktree creation: each dispatch still gets a fresh worktree per run id. Verify in the design pass that nothing in workspace-model assumes the dispatch source. No edit expected.

Dependencies: none.

## Cross-spec ordering

- **Tier A (must be drafted first):** queue-model.md. Everything else references it.
- **Tier B (drafted in parallel, all depend on A):** execution-model.md, beads-integration.md, process-lifecycle.md, event-model.md, operator-nfr.md.
- **Tier C (small follow-ups, depend on B):** scenario-harness.md.
- **Tier D (review-only):** workspace-model.md.

## Goal → spec coverage

Cross-check that every goal in `01-problem-space.md` maps to at least one spec:

| Goal | Specs |
|------|-------|
| Daemon has no internal selection logic | execution-model, beads-integration |
| Orchestrator submits via CLI | process-lifecycle, queue-model |
| Queue supports ordered parallel-groups (waves + streams) | queue-model, execution-model |
| Daemon publishes status (queued/running/completed/failed) | event-model, process-lifecycle (status subcommand) |
| Orchestrator can edit queue at runtime | queue-model (append/remove), process-lifecycle (CLI) |
| Bead `blocks` edges remain authoritative | beads-integration, execution-model |
| Failures surface; no silent auto-anything | event-model, execution-model (pause semantics) |
| State survives daemon restart | queue-model (persistence section) |
| Ledger-vs-queue disagreement is observable | event-model (deferred-for-ledger-dep event), queue-model (validation) |
| Past MVH; parallelism shipped | (constraint, not a new spec section — but execution-model should remove MVH-era prose) |
| Reconcile existing ON-015 "Beads is the queue" framing | operator-nfr (entry 6), queue-model (entry 1) |
| Reuse existing daemon.sock — no second transport | process-lifecycle (entry 4, PL-003a method-set extension) |

All goals covered. No spec change exists without a goal driver.

## No-spec-change checks

Areas where the change does NOT touch, confirmed:

- The `claude-hook-bridge.md` spec — bridge runs per-bead, queue is upstream of bridge. No change.
- The `event-model.md` envelope (schema_version, event_id) — unchanged; only new event types registered.
- The `handler-contract.md` — handler doesn't know about queues; it knows about runs. No change.
- The `core/runterminalpayload.go` event shapes — payloads gain optional `queue_id`/`queue_group_index` fields; otherwise unchanged.
