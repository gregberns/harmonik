# Beads integration findings

## Questions

1. How does a ledger dependency affect item state?
2. Which startup repairs depend on Beads state?
3. What must the queue API not own?

## Findings

- QM-025 and §2.8 allow `pending → deferred-for-ledger-dep → pending` when an
  open Beads `blocks` edge exists or later clears.
- Deferred items are non-terminal. They block group completion, while a stream
  scan may skip them and dispatch a later pending item.
- Startup reconciliation can repair pending or deferred items to completed when
  a bead is terminal. A paused-by-drain queue can repair them to failed. These
  changes persist before mismatch events.
- BI terminal writes stay in the Beads adapter. The queue transition API owns
  queue state only.

## Patterns to follow

- Keep deferred recovery event-free.
- Keep the pre-install startup exception explicit.

## Risks

- Treating deferred as terminal can complete a group early.
- Emitting an observation before the durable repair breaks QM-063.

