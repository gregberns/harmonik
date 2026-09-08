# Pending operator decisions — bus track

Each has a recommended default so Phase 0 (interface freeze) is NOT blocked. Phase 0 runs
in a scratch package and waits on nothing.

## B-D1 — Public name: "bus" or "kernel/fabric"?
The seed calls it the bus (`clean/transport`); the prior P1 program calls the same system
the kernel/fabric (`internal/kernel`). One system, two names from two eras.
- **Default:** public surface = **bus**, package `clean/transport`; note kernel/fabric as
  its design lineage. Override = adopt `internal/kernel`.

## B-D2 — Defer point-to-point / competing-consumer distribution from the first slice?
The three verbs (Publish/Subscribe/Request) do not cover "exactly one spoke takes the job,"
which hub/spoke needs. The review confirmed an interim pull model would violate locked C5
(no lease → a dead spoke strands the job) and C2 (no temporary-pipe-then-migrate). So
point-to-point + State + Roster now defer AS ONE coupled unit.
- **Default:** **defer** all three as a unit; the first slice is pub/sub/request + Mount +
  a cross-service test only, no distribution built. Override = design a leased point-to-point
  path up front (larger first slice).

## B-D3 — `Bus` delivery contract: at-most-once?
- **Default:** **at-most-once by contract**; durability stays a service concern (matches
  what `harmonik comms` does today and locked C-decisions). Override = inherit a durable journal (not recommended).

## B-D4 — When does the new bus absorb the existing comms plane?
Until the tracked "reconcile/absorb comms" deferred slice lands, `internal/eventbus` +
`harmonik comms` + daemon `SubscribeHub` carry ALL real traffic; the new bus carries only
stub/keeper test traffic. This is stated honestly in the plan.
- **Default:** keep the two planes side by side; schedule the absorb slice only after the
  in-mem bus is proven with a real second Service. Operator sets the trigger.
