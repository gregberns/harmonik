# Session

## Current pass

The work is shelved in the change-spec pass.
The problem, analysis, corrected decomposition, and research files exist.

## Decisions

- Queue state functions must return value-only event intents.
- The event bus must create event identity and envelope time.
- The event-intent migration must cover `AdvanceGroup` and `AppendItems`.
- Only the event-intent migration is unblocked.
- A completion decision must return a deeply detached queue value.
- The daemon shell must distinguish durable commit failure from cleanup failure.
- The current queue specification outranks the older completion code.

## Findings

`queue.AdvanceGroup` is not pure.
It calls `newEvent`, which creates a UUID and reads the clock.
The daemon discards those envelope fields and the event bus creates new ones.

The same hidden helper affects `AppendItems`.

The live final-completion path does not match the current queue specification.
The specification requires a completion receipt before final event observation and cleanup.

## Open questions

- Which active queue transaction work will supply the QM-001 and QM-005 final-completion contract?
- Which contract supplies the prebound completion receipt ID to the final event intent?
- Should the event-intent prerequisite land before that transaction work?
- The plan jig requires new test beads, but the loaded beads skill does not permit agents to create beads.

## Next steps

1. Review the corrected decomposition and research files.
2. Write the event-intent change spec as the only unblocked component.
3. Ask the authorized task owner to create the required scenario and exploratory beads.
4. Keep the completion decision, durability policy, and daemon shell blocked on the queue transaction and receipt contract.

## Reading order

1. `01-problem-space.md`
2. `02-analysis.md`
3. `decompose-review.md`
4. `03-components.md`
5. `04-research/event-intent/findings.md`
6. `04-research/queue-decision/findings.md`
7. `04-research/durability-shell/findings.md`
