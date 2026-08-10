# Findings

## Summary

The safe event and claim work from the last pass is complete.
The next useful work is not another literal wave.
It is completion ownership and run-path decomposition.

## Critical

### C1. Queue event construction violates the pure-core boundary

`AdvanceGroup` and `AppendItems` create UUIDs and read the clock through `newEvent`.
The daemon discards the generated envelope fields.
The event bus then creates the real envelope.

This is duplicate ownership of time and identity.
It also makes queue decisions harder to replay.

Replace queue-created envelopes with detached event intents.
Keep identity and envelope time at the event bus edge.

This work is ready now.

### C2. Final queue completion has no receipt-aware transaction owner

The live path still uses `CompleteAndUnlink`.
The current specification requires a bound completion receipt before final observation and cleanup.
`QueueStore.Transact` states that it has no completion-receipt behavior.

The larger group-completion rewrite must not harden the obsolete helper.
Build the receipt-aware transaction contract first.

### C3. Dispatch still spans several durable owners

Queue reservation, bead claim, run registration, and run-session state are separate writes.
Repair remains spread across scheduler branches and startup paths.

One typed dispatch transaction and replay model is still required.
This work follows the queue completion transaction work because both change durability ownership.

## High

### H1. The daemon still owns policy and effects in the same functions

The four largest live run-path functions remain procedural state machines.
The latest changes reduced some spans.
They did not create one full lifecycle machine.

The next decomposition must extend the existing `runexec` style.
It must not create another helper layer around the old flow.

### H2. The core package boundary is still not enforceable

The current graph supports the old finding.
`internal/daemon` is large and unstable.
`internal/core` is rigid and concrete.
`make core` remains a test list rather than an import boundary.

A small composition root and import fence remain required.
Do this after transaction and lifecycle ownership are explicit.

### H3. Dependency bags still hide phase ownership

`RunEnv` has 29 fields.
`SharedHandles` has 18 fields.
The detector cannot prove that these fields are unrelated.
The state and lifetime analysis can.

Do not split these records by field count.
Split them only after each run phase has a named input and effector set.

### H4. Cross-package STATE was undercounted

The corrected graph finds three cross-package writes to `supervise` hooks.
This does not by itself make those hooks wrong.
It proves that older STATE-based measurements were incomplete.

Do not use the old zero to justify weights or boundaries.

## Medium

### M1. There are small compiler-edge cleanups, but they are not the main program

The detector found 54 same-package and unambiguous A1 sites.
Several are clear named-value uses in queue, daemon, lifecycle, and keeper code.

These can run as bounded cleanup tasks after core-path file conflicts clear.
They must not delay the event-intent or transaction work.

### M2. The remaining error-text matches are boundary-specific

Thirteen A4 sites remain.
Most parse socket, tmux, workflow-loader, or CLI behavior.

Each site needs producer ownership analysis.
Do not replace them with one global sentinel wave.

### M3. Historical hotspot output needs a current-file filter

The history tool reports deleted files in change-coupling tables.
This is useful history but weak current targeting.

This is a `codebase-organism` task.
Charlie must not edit Harmonik to address it.

## Rejected work

- Do not bulk-apply A1.
- Do not bulk-apply A2.
- Do not split every B4 record.
- Do not add one port per effect call.
- Do not start C# or tool-weight work in Harmonik.
- Do not extend `CompleteAndUnlink` as the target completion design.
- Do not split `RunEnv` before run phases and lifetimes are explicit.
