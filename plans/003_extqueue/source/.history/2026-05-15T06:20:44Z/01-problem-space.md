# extqueue — Problem Space

## What is changing

Today, harmonik's daemon decides what work runs next by polling `br ready` and taking the first element. Selection logic lives inside the daemon, and the only external lever is mutating bead fields (priority, defer, deps). This conflates two responsibilities: the **what to run / in what order** decision, and the **how to run it** machinery.

The change moves the **selection / ordering decision out of the daemon entirely**. An external orchestrator agent owns scheduling. Harmonik becomes a pure execution engine that consumes an externally-supplied queue, runs it, and exposes status so the orchestrator can react.

## Why

1. **Separation of concerns.** Harmonik should be the execution substrate, not a project-management system. Priority semantics, "which bead matters now", testing-gate ordering, and exploratory-testing sequencing are agent-level decisions, not daemon-level.
2. **Richer ordering than `br ready` can express.** The orchestrator may want patterns the ledger can't naturally encode — e.g., "run these five beads in parallel, then a gating test bead alone, then unblock a second batch." Bead `blocks` edges can technically express this, but bending the ledger to imply scheduling intent pollutes it. Queue-level ordering keeps the ledger about what work *is* and the queue about what we want to *do now*.
3. **Observed failure case (2026-05-14).** During the Phase 2 first-demo run (`hk-09tne`), the daemon picked up `hk-1n0cw` (a P1 epic ahead of the target P2 demo bead). Restoring ordering required mutating five beads' priority/defer fields. The mutation worked but is exactly the wrong coupling — the ledger shouldn't be edited to influence the picker.

## Goals (what should be true after this change)

- Harmonik's daemon has no internal "what should run next" logic. It dispatches only what an external orchestrator has explicitly queued.
- The orchestrator submits work via a CLI (or other well-defined surface) and gets back validation results before commit.
- The queue supports richer-than-a-flat-list ordering — at minimum, an ordered sequence of *parallel groups*: within a group, beads dispatch concurrently up to `--max-concurrent`; between groups, the next group does not start until the previous one is resolved.
- The daemon publishes status the orchestrator can read: what is queued, running, completed-this-session, failed.
- The orchestrator can edit the queue (remove not-yet-running items) at any time without restarting the daemon.
- Bead `blocks` edges remain a hard execution constraint — the queue can request a bead, but the daemon won't dispatch it if its ledger blockers are open.

## Non-goals

- No project-management vocabulary inside harmonik (no "milestones", "epics-as-scheduling-objects", "sprints"). The ledger and beads' built-in semantics are the only structural inputs harmonik knows.
- No replacement for `br ready` as a tool; `br ready` remains a perfectly good CLI for an orchestrator to *query* the ledger. It just stops being the daemon's input.
- Pause / stop / kill of *running* beads is a separate concern (own design pass). This work covers only queue manipulation of not-yet-running items.
- Multi-orchestrator submission semantics (two agents racing to enqueue) is out of scope for v1. Assume a single orchestrator per daemon run.
- Auto-trust, merge-to-main (`hk-ftyvo`), and other Phase-2 plumbing remain separate works.

## Constraints

- **Bead-deps are authoritative for ledger consistency.** If the orchestrator queues bead X but X has open `blocks` edges, harmonik does not dispatch X until those resolve. The queue expresses *desire*; the ledger expresses *eligibility*.
- **Daemon owns the queue at runtime.** The CLI is a control surface that talks to the daemon; it does not mutate the queue file directly. This prevents the daemon and an external writer from racing on the on-disk representation.
- **Queue state survives daemon restart.** In-memory primary, periodic file sync (or write-through), so a daemon crash does not lose the orchestrator's plan.
- **Validation is mandatory and rejecting by default.** Submission is checked (beads exist, not already closed, not currently in a queued/running state for this daemon) before acceptance. Force/override exists but is explicit.
- **No silent precedence rules.** When queue and ledger disagree, the daemon's behavior must be predictable and observable in events.jsonl — never a silent skip.
- **Past MVH; parallelism shipped.** `--max-concurrent N` is the existing concurrency control and remains the substrate. The queue's parallel-groups concept sits *on top* of `--max-concurrent`, not replacing it (a group of 8 with `--max-concurrent 2` still runs 2 at a time within that group).

## Success criteria

The work is complete when the following statements are true:

1. A spec section in `execution-model.md` (or new `queue-model.md`) defines the queue data structure, validation rules, and dispatch semantics including parallel-groups.
2. A spec section defines the CLI control surface: submit, status, edit, with explicit error modes and authority boundaries.
3. A spec section defines daemon ↔ CLI transport (file? socket? subprocess invocation?), in-memory model, and on-disk persistence format for crash recovery.
4. `internal/daemon/workloop.go:391-421` (Ready-poll + take-first) is replaced by queue consumption — the change is described in the design pass with the deletion clearly called out.
5. An event-bus contract addition: terminal events emit on the queue's identity too (so the orchestrator can watch its own submission progress without polling status).
6. A `kerf` tasks artifact (07-tasks.md) decomposes the spec into implementable beads, with explicit ordering and any external dependencies (Phase 2 unblockers like `hk-ftyvo`).

## Affected spec areas (preliminary)

- `specs/execution-model.md` — dispatch loop, queue model, parallel-groups semantics.
- `specs/beads-integration.md` — daemon ↔ ledger relationship; clarify that selection is no longer daemon's job.
- `specs/process-lifecycle.md` — CLI control surface (probably a new subcommand family, e.g. `hk queue {submit,status,remove,clear}`), daemon RPC channel, on-disk queue file location and format.
- `specs/event-model.md` — new events for queue-level state changes (queue_submitted, queue_item_dispatched, queue_item_completed, etc.), and ensure run-level terminal events carry enough identity for the orchestrator.
- Possibly new: `specs/queue-model.md` — if the queue model gets large enough to warrant its own spec rather than living inside execution-model.

## Decisions (locked 2026-05-14)

**D1. Two group primitives: wave and stream.** The queue is an ordered sequence of groups. Each group is one of two kinds:

- **Wave** — syntax `{a, b, c, d}`. A fixed, closed set. The daemon dispatches all members concurrently up to `--max-concurrent`. The wave is *complete* when every member has reached a terminal state. The wave's set cannot be appended to after submission; if you want more items in this position, submit a new group.
- **Stream** — syntax `[e, f, g, h]`. An ordered, open-ended sequence. The daemon dispatches as slots open (up to `--max-concurrent`), pulling the next item from the head of the stream. The orchestrator MAY append to a stream that is currently active — appended items dispatch when their turn comes. The stream is *complete* when its list is empty AND every dispatched item has reached terminal.

Streams beat waves on throughput for heterogeneous work (slot-fill is higher); waves beat streams when the orchestrator needs a hard "all of these before any of those" gate. Both kinds are necessary; the orchestrator picks per group.

**D2. Group-advance is gated on all-terminal.** The next group starts only when the current group is complete (every member terminal). This is uniform across wave and stream. No interleaving between groups.

**D3. Failure does not silently halt or auto-retry.** When any bead in a group fails, the daemon emits a `queue_item_failed` event with bead id, run id, summary. The group continues processing its remaining items (for streams: the stream keeps draining; for waves: in-flight beads finish). After the group reaches all-terminal, if any item failed, the queue **pauses** at this group boundary. The orchestrator unpauses by an explicit CLI action — typically: remove the failed bead from a future group, fix the underlying bead, or call a `resume` command. v1 does no auto-anything around failures; the orchestrator owns the recovery policy.

The user's framing for this is important: most failures in practice are non-loop-blocking and the orchestrator's job is to surface them to a human or fixer-agent, then patch and continue. The daemon's only contract is "emit the event clearly and don't move on until told."

**D4. Bead-dep interaction inside a group: honor the ledger, surface what happened.** When the orchestrator submits `{a, b}` but the ledger says `b blocks-on a`, the daemon dispatches `a` first, waits for it to close, then dispatches `b` — same group, just narrowed parallelism. To prevent silent surprises, the daemon emits a `queue_item_deferred_for_ledger_dep` (or similar) event at submit-validation time so the orchestrator sees that its requested parallelism was reduced. Validation does not reject the submission. Submit-time dry-run flag (`--dry-run`) returns the resolved plan so the orchestrator can preview.

**D5. CLI ↔ daemon: Unix domain socket.** The daemon serves a Unix socket (under `.harmonik/`, exact name TBD in design pass) speaking a request/response protocol. CLI subcommands (`hk queue submit | status | append | remove | pause | resume | dry-run`) connect, exchange a message, disconnect. The in-memory queue is the authority; the daemon write-syncs to a file (`.harmonik/queue.json` or similar) on every mutation so that a daemon restart can recover the queue. The CLI never touches the file directly — only via the socket.

This costs a socket implementation we didn't have before, but the user expects the mechanism is needed anyway, and it removes a class of staleness/race concerns that file-polling would introduce.

**D6. Keep v1 minimal; iterate.** The user explicitly asked to "get something basic up so we can start running work through it." v1 ships the smallest cohesive system that supports submit / status / append-stream / remove-from-queue / resume-after-failure-pause. Things explicitly deferred:

- Pause/stop/kill of *running* beads (separate work, separate spec pass).
- Auto-retry policies, exponential backoff, dead-letter semantics.
- Multi-orchestrator submission / queue ownership / ACLs.
- Stream priorities, weighted scheduling, fairness inside `--max-concurrent`.
- Conditional ordering ("run X only if Y succeeded").
- Queue replay / time-travel.

These are intentionally absent from v1 success criteria. The design pass should sketch a forward-compatible shape but not implement them.

## Open for later passes (not blocking)

- **Failure-surfacing channel.** The `queue_item_failed` event is the contract; how the orchestrator subscribes (events.jsonl tail vs. socket push vs. poll status) is a transport question for the design pass.
- **Socket protocol shape.** JSON-line, length-prefixed JSON, gRPC, or hand-rolled? Defer to design pass; expect to pick the boring option.
- **What "a wave" identity means in events.jsonl.** Does a wave get its own group_id event so the orchestrator can correlate completions? Probably yes; defer to design pass.
