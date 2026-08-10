# Phase-two reconciliation

Date: 2026-08-09

The phase-two report contains detector candidates. It does not define a required cleanup list.
This document records the decision for each area against the current tree.

The detector used revision `d5a12348f`. The first implementation review used `429dfbcee`.
The implementation then moved with the integration branch.

## Status by area

| Area | Current decision | Result |
| --- | --- | --- |
| 1. Event type enum | Complete | `core.Event.Type` and the registry now use `core.EventType`. Tagged tests also compile with the typed field. |
| 2. Event registry coverage | Complete | Live worker, governor, and workflow types have payload constructors and compatibility rows. The dead phantom type is gone. |
| 3. Literal duplicates | Partial by design | The safe event, queue message, and DOT parser sites now use owned constants. Ambiguous and cross-package matches still need a site decision. |
| 4. Repeated literals | Rejected as generated work | The detector does not report the full file lock set. Struct tags also inflate this class. |
| 5. Error text control flow | Critical core slice complete | Claim refusal text is classified once at the external `br` boundary. The scheduler now uses typed errors. Other matches are tool and CLI boundaries. |
| 6. Wide bundles | Deferred | Field count alone does not prove a dependency problem. `RunEnv` and `SharedHandles` still need consumer-use evidence. |
| 7. Pure logic and effects | Pilot complete, more work remains | Queue submission and claim-failure routing now have pure decisions. The run machine and group-completion path still mix decisions and effects. |
| 8. Analysis tool gaps | Not a Harmonik change | These changes belong in `codebase-organism`. Harmonik must not depend on them until that program is stable. |

## Work that landed

The event vocabulary now has one typed path from declaration to emission.
An inventory test scans production declarations under `internal/`.
The test compares those declarations with the live registry and compatibility table.

The claim path now keeps structured failures from the external command.
The scheduler no longer routes an error because its message contains `blocked`.
A pure decision selects the claim-failure disposition.

Queue append now receives one acceptance time.
Queue submission now keeps file access, ledger access, UUID creation, and persistence in its shell.
The builder receives values and returns values.

## Work that should not become a bulk wave

Do not replace equal text across package boundaries without an ownership decision.
Equal text can represent different wire formats or domain terms.

Do not split a record because it has many fields.
First prove that a consumer receives unrelated dependencies.

Do not create ports for each direct effect call.
A port is useful only when it creates a test seam or protects an inward dependency.

## Next core sequence

1. Pass completion time into the group-completion path.
2. Separate the group-completion decision from persistence, wake, cancel, and event emission.
3. Draw the live bead state model from queue reservation through bead close.
4. Use that model to define recovery at each process-death boundary.
5. Split `RunEnv` or `SharedHandles` only when the state model shows a lifetime boundary.

This sequence does not require a new detector run.
It follows the current code and the engineering principles directly.
