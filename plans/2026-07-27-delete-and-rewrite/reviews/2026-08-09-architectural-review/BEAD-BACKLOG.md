# Graph-guided implementation backlog

Date: 2026-08-09

This backlog converts the first phase-2 graph findings into executable work.
It uses the current tree at `429dfbcee` for code checks.
The detector data came from `d5a12348f`.

The bead ledger is local to this machine.
This file keeps the task scope and order in tracked project history.

## Selection rules

The backlog accepts a detector finding only when the current code confirms it.
Each task must create a useful compiler edge or a pure decision boundary.
Each task must name a focused verification gate.

The backlog rejects bulk cleanup based only on counts.
It also rejects a package dependency that does not match concept ownership.

## Event vocabulary chain

Run this chain in order.
The final type change is repository-wide and must run alone.

| Order | Bead | Task | Priority |
| ---: | --- | --- | ---: |
| 1 | `hk-2bzcj` | Type the event registry API with `EventType` | P1 |
| 2 | `hk-6dk9f` | Reconcile live event declarations and compatibility rows | P1 |
| 3 | `hk-fzee7` | Replace the vacuous registry coverage test with declaration-side proof | P2, existing bead |
| 4 | `hk-2tv5o` | Change `core.Event.Type` from `string` to `EventType` | P1, serial |

One task can run beside the chain after order 1:

- `hk-fkbv2` replaces event-bus taxonomy literals with canonical event constants.

The current audit corrected the generated area-2 summary.
Five worker event types register from `internal/workers`, but lack compatibility rows.
`governor_signal` is live and unregistered.
`loop_observed_phantom_done` has no production use in the current search.

The old event-registration bead `hk-71dff` describes two earlier event types.
It remains separate because its evidence and scope do not match the current seven candidates.

## Bead-claim safety chain

Run this chain in order.
The first two tasks fix a live wrong-route risk in the reservation window.

| Order | Bead | Task | Priority |
| ---: | --- | --- | ---: |
| 1 | `hk-oo8aq` | Preserve structured `br` claim failures at the adapter boundary | P0 |
| 2 | `hk-s23kb` | Remove blocked-text routing from the dispatch reservation window | P0 |
| 3 | `hk-dstwt` | Extract a pure claim-failure disposition from the scheduler | P1 |

The adapter will still parse output from the external `br` process.
It must parse that output once and return a typed result.
The scheduler must not parse the human-readable error.

## Queue purity chain

Run these tasks in order because they overlap the queue submission files.

| Order | Bead | Task | Priority |
| ---: | --- | --- | ---: |
| 1 | `hk-7a095` | Inject one acceptance time into queue append | P1 |
| 2 | `hk-fxwmq` | Split queue-submit decisions from persistence and identity effects | P1 |

These tasks form the first phase-B pilot.
They test whether the graph detector can lead to a useful pure policy function.
They do not add a broad port interface.

## Independent small tasks

These tasks are safe, local compiler-edge changes.

| Bead | Task | Priority |
| --- | --- | ---: |
| `hk-0m1i9` | Derive queue JSON-RPC messages from typed validation reasons | P2 |
| `hk-b9s3t` | Use declared DOT parser vocabulary constants in control flow | P2 |

## Work not converted into beads

### Repeated literals

The A2 detector records one file even when a value occurs in many files.
Most findings therefore have an incomplete lock set.
Struct tags also make about 36 percent of this class false work.

Do not dispatch A2 from the generated plan.
First fix the detector in `codebase-organism` to report all files and syntax roles.

### Ambiguous or cross-package literals

Equal text does not prove equal meaning.
The test twin also needs an independent wire vocabulary in several places.

Do not dispatch these findings without a site-level ownership decision.
Prefer same-package replacements or references to packages already imported for the same concept.

### Wide structures

The B4 detector uses only a field-count threshold.
Wire records, state values, and configuration records can be wide for valid reasons.

Do not split a structure because it has twelve fields.
Require evidence that consumers receive fields they do not use.

The `RunEnv` and `SharedHandles` bundles remain strong candidates.
Do not dispatch their split while the alpha agent changes the core path.

### Bulk pure-and-effect splits

The B1 and B2 effect classifier treats some pure helpers as effects.
It also misses effects behind interfaces and function values.

Use the queue submission pilot before a wider phase-B wave.
Measure whether the extracted decision reduces dependencies and improves failure tests.

## Verification state

- Eleven new beads exist.
- Two existing beads carry updated context.
- Nine dependency edges define the three ordered chains.
- `br dep cycles` reports no active dependency cycle.
- No code implementation started in this task-planning pass.
