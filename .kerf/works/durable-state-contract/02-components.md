# Durable-state contract decomposition

## Scope map

| Spec area | Status | Requirements this work adds or reconciles | Dependencies |
| --- | --- | --- | --- |
| `specs/park-resume-protocol.md` | Existing, affected | The SLEEP/WAKE sequence must define when the pre-sleep restore set becomes durable and what failure leaves the fleet unchanged versus explicitly degraded. A crash must not leave disabled work with no reconstructable prior intent. | Shared replacement/transaction rule; schedule consumer research. |
| `specs/event-model.md` | Existing, affected | Event-log readers must distinguish a valid end-of-file boundary from malformed/torn persisted input and surface corruption to replay consumers rather than silently normalize it away. Event-log fsync policy remains owned here. | Replay consumer research; preserve event-model ownership. |
| `specs/replay-substrate.md` | Existing, affected | Replay output must account for malformed or missing persisted transitions and make a clean replay claim impossible when its source scan observed corruption. | Event-model read/error vocabulary. |
| `specs/process-lifecycle.md` | Existing, affected only if branch-tip state affects recovery | Branch-tip persistence must not treat malformed/torn stored state as an initial observation when that permits rewind protection to fail open. | Shared replacement/read-validation rule; lifecycle consumer research. |
| `specs/session-keeper.md` | Existing, affected only if session metrics are operator-visible keeper evidence | Session-data reads must expose corruption/partial-record handling rather than silently undercounting metrics used for keeper decisions. | Append-only policy and sessiondata research. |
| `specs/durable-state.md` | New | Normative cross-subsystem owner for atomic replacement, append-only record framing, sync/error vocabulary, and test evidence. It defines common behavior without owning consumer-specific schemas or event fsync policy. | Must be drafted before consumer amendments. |

## Component boundaries

### C1 — Common replacement protocol

Owns the invariants for state represented by one replaceable file: unique temporary name in the target
directory, fully checked write, file sync, close, atomic rename, parent-directory sync, and no swallowed
failure. It also distinguishes no prior file from invalid prior contents. This component must not choose
individual consumer schemas.

### C2 — Multi-record sleep/wake transaction

Classifies the schedule sleep action as a transaction spanning a disable action and a restoration
sidecar. It must state a recoverable ordering and recovery behavior for every crash boundary. It depends
on C1 but may require an explicit transaction marker rather than treating the sidecar alone as sufficient.

### C3 — Branch-tip integrity sensor

Classifies persisted branch-tip state as replacement-style evidence. It must reject or visibly escalate
malformed/torn state before evaluating monotonicity; first-observation behavior applies only to truly
absent state. It depends on C1.

### C4 — Replay corruption accounting

Classifies the event source as append-only and specifies a scan result that preserves corruption facts.
Replay may choose to stop or produce a degraded result, but it may not report a clean result after
skipping malformed source records. This depends on the event-model's log ownership and the C1 error
vocabulary, but does not transfer event fsync policy to the new spec.

### C5 — Session-data append/read visibility

Classifies metrics as append-only. It must define framing, writer coordination requirements if more than
one writer exists, and a reader result that exposes malformed/partial records. It depends on C4's
corruption-accounting vocabulary but has independent storage semantics.

## Dependency order

```text
C1 common replacement/error vocabulary
 ├─ C2 sleep/wake transaction
 └─ C3 branch-tip integrity

event-model read/error boundary ── C4 replay accounting ── C5 session-data visibility
```

`specs/durable-state.md` can be drafted after research establishes C1's replacement and append-only
boundaries. Consumer-specific amendments follow it; implementation work remains blocked until those
requirements are reviewed together.

## Goal coverage

| Problem-space goal | Components |
| --- | --- |
| Reusable atomic replacement protocol | C1, new `durable-state.md` |
| Corruption behavior | C1, C3, C4, C5 |
| Compatible ownership across consumers | C1–C5 and dependency order |
| Fault/restart evidence | C1 plus every consumer component |
