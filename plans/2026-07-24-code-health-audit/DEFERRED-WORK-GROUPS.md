# Deferred work groups

These groups remain visible but intentionally have no detailed agent tasks until the local queue/job/Pi
contracts are clean. Their high-level records are in [`TASK-INDEX.yaml`](TASK-INDEX.yaml).

```text
LIFT file-lease release ───────────────┬─ post-LIFT graph/spec work
                                        ├─ process call-site integration
                                        └─ scheduler placement integration
local process contract ─ real SSH proof ─ remote concurrency/scale proof
runloop/substrate ports mature ─ crew seam re-evaluation ─ conditional decision ─ E2b
durable-write contract ─ sleep/wake durability
validated telemetry/tokens/weights/capacity ─ N-worker model ─ control ─ placement
```

## Crew

E2b is **deferred by the 2026-07-22 operator Option A decision**, not blocked awaiting a waiver. Revisit
after runloop/substrate ports mature. Only if current evidence still requires a new seam should a narrow
E2-P step and a new operator decision be planned.

## Remote

Prove local process ownership first: wait owner, process-tree cancellation, bounded independent cleanup,
and detached-job semantics. Then prove real SSH process trees, then integrate released call sites. Remote
concurrency rollout needs evidence from the exact wrapped-runner path.

## Scheduling

Sleep/wake durability and capacity placement are separate. Sleep/wake waits on durable-write/corruption
contracts. Capacity placement waits on validated durable telemetry, token/quota facts, work weights,
machine/model capacity, and an N-worker model; it must not act on stale or partial reports.

## Extraction and broader orchestration

Keep LIFT moves reversible and behavior-neutral, then re-measure before planning semantic graph changes.
Eventbus, supervisor, durable-write, and queue-transaction contracts can progress as separate ownership
lanes. Primary-daemon runtime proof is a verification deferral, not completion.
