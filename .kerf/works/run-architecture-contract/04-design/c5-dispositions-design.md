# C5 design — Existing-contract and LIFT dispositions

## Current state

The tree contains landed leaves, broad transitional bridges, staged unused
review kernels, compatibility-only owner APIs, a blocked dirty relocation,
and planned moves. Their status is evidence, not a target decision.

## Target state

RAC will include this exhaustive decision matrix:

| Artifact | Decision | Target use / convergence condition |
| --- | --- | --- |
| `runexec.Dispatch` | adopt | sole dispatch decision kernel; no competing machine |
| `runexec.Run` | adopt | sole run/terminal decision kernel |
| `runlaunch` leaves | adapt | remain lifecycle mechanisms behind C2; mode callers removed |
| runloop ports | adapt | split into constructor-complete consumer-owned ports |
| L1 `PerRunEventTap` | adapt | owned subscriptions only; compatibility API deleted |
| L2 post-ready wait | adopt | C2 observation leaf |
| L3 socket grace | adopt | C2 completion-observation leaf |
| L4 RunShell/DispatchSegment | adopt | bounded execution beneath modes/lifecycle |
| L5 scenario gate | adopt | pure gate leaf |
| L6 RunBridge | adapt | shrink, freeze, then delete after direct owner ports |
| L7 reviewer harness | adopt | pure review policy |
| R0 characterization/census | adopt | mandatory migration gate |
| `reviewcycle` R1 | adopt | review executor's only decision kernel |
| `continuity` R2 + vocabulary | adopt | land and consume atomically; no dormant vocabulary |
| R3 registration/subscription owners | adopt | callers retain/close handles; compatibility removed |
| R4–R9 planned boundaries | adapt | map useful ownership into C2/C3/C4 tasks; no delivery claim |
| L8 move/R10 relocation as written | retire | zero-private-dependency decomposition replaces dirty move |
| L9 move as written | retire | DOT-gate lifecycle decomposition precedes optional later move |
| L10/L11 tombstones | retire | intentionally absent; never recreate |
| L12 DOT atom move as written | retire | split traversal/node lifecycle before optional package move |
| L13 final `beadRunOne` relocation as written | retire | C1–C4 responsibility decomposition replaces relocation |
| QueueStore/CQ-02 transaction owner | adopt | sole runtime queue writer |
| bounded startup queue adapter | adopt | pre-install recovery only |
| all direct queue persistence callers | adapt | consume QueueStore transaction or disappear |

Each final spec row will also name current reachability/evidence, migration
task, target owner, and deletion test. `adopt` does not mean unchanged package
placement; `adapt` must preserve the artifact's proven semantics.

## Rationale

The choices preserve reviewed decision/mechanism value while rejecting
movement-as-decomposition. Adopting reviewcycle and continuity settles RL-01's
open choice and avoids shadow architecture.

## Requirements traceability

This matrix covers every required package, queue owner, RL-01 atom, and LIFT
slot with one allowed disposition and convergence condition.
