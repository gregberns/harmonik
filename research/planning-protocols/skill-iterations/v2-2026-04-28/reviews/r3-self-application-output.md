<!-- PP-TRIAL:v2 -->

**Status.** green

**What we're doing.** Adding intermediate-status notifications to the beads task ledger so workflows can react to status transitions, not just terminal completion.

**Where it stands.**
- Decided event shape: a single generic `StatusChanged` event (with `from`, `to`, optional-but-recommended `reason`) rather than discrete per-status event types.
- `specs/beads-integration.md` §6.2 updated with the new schema; `StatusChanged` added to the event registry.
- Immediate next step: decide whether the notification handler runs sync or async — that choice determines whether multiple status changes from one workflow batch into one notification or fire one-per-change.

**Open question for the user.** Should a workflow that flips a bead through several statuses in quick succession produce one combined notification or one per change? The answer fixes whether the handler is synchronous (per-event) or asynchronous (batched).

**First files to open.**
- `specs/beads-integration.md` — §6.2 has the new `StatusChanged` schema; the surrounding event-model sections are the context for the sync/async decision.
- `specs/event-model.md` — event registry conventions; cross-check that `StatusChanged` fits the existing patterns before extending it.
- `specs/handler-contract.md` — handler execution model; this is where the sync-vs-async choice actually lands.
