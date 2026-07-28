# C3 design — Mode execution boundaries

## Current state

`beadRunOne` selects modes while also acquiring resources and applying durable
effects. Review and DOT coordinators duplicate lifecycle. DOT/sub-workflow and
review-helper edges remain daemon-private. Existing pure machines and leaves
do not yet form a complete mode contract.

## Target state

RAC will define one small behavioral boundary:

```text
ModeExecutor.Execute(ctx, ModeRequest, PhaseLifecycle, ModePorts) -> ModeResult
```

`ModeRequest` contains a sealed plan plus mode-specific immutable
configuration. `ModePorts` is a set of consumer-owned narrow interfaces for
mechanisms actually used by that mode; it never contains queue, Beads, Run
record, registry, semaphore, or terminal writers.

`ModeResult` is a lossless envelope:

```text
status: SUCCESS | FAIL | RETRY | PARTIAL_SUCCESS
failure_class: absent unless FAIL; then exactly one of
  transient | structural | deterministic | canceled
  | budget_exhausted | compilation_loop
classification:
  handler_hint?
  daemon_authoritative
  disagreement?
continuation: terminal | in-mode-retry
mode_payload: single | review | dot typed payload
needs_attention: boolean plus owning-mode reason
```

The post-classifier daemon value is authoritative under EM-005/EM-005c and
HC-059. Handler disagreement remains observable. `PARTIAL_SUCCESS` remains
distinct from `SUCCESS`. `RETRY` with `continuation=in-mode-retry` is consumed
inside the current review/DOT execution; an exhausted retry returns the
resulting terminal status and exact failure class. A transient failure with no
in-mode retry remains `FAIL/transient`, allowing BI-010a to reopen; it is not
renamed `retry`.

The envelope carries outcome evidence, merge/gate intent, and
recovery/cleanup observations, but performs no durable terminal mutation.

### Implementations

- Single executor owns one phase and its single-mode interpretation.
- Review executor adopts the reviewed `reviewcycle` decision kernel and
  `continuity` service/vocabulary together, then owns implementer/reviewer
  alternation, checkpoints, verdicts, retries, progress, and budget.
- DOT executor owns traversal and node routing. Agentic nodes use
  `PhaseLifecycle`; nested sub-workflow dispatch becomes a consumer-owned
  dispatcher port so no daemon reverse import is required.

`internal/runexec.Dispatch` and `Run`, runloop execution leaves, scenario gate,
reviewer-harness resolution, and runlaunch leaves are composed, not duplicated.
`RunBridge` is adapted only until direct narrow effect ports replace it.

### Behavioral preservation

Mode-specific resume, input content, heartbeat/progress rules, checkpoint
ordering, graph traversal, budgets, and the single detach policy stay outside
the lifecycle implementation. Extraction precedes package movement: a mode
may move only after its daemon-private dependency census is zero.

## Rationale

This separates mode cognition/policy from lifecycle mechanics and terminal
durability, while preserving existing pure kernels and avoiding a renamed god
coordinator.

## Requirements traceability

| C3 requirement | Target |
| --- | --- |
| Capabilities per mode | ModePorts and implementation list |
| Lifecycle independent of modes | one-way executor dependency |
| Preserve workflow/checkpoint/review/DOT | behavioral preservation |
| Feed terminal/recovery | lossless ModeResult |
| Preserve every failure distinction | lossless EM status/classification envelope |
| Concrete hotspot ownership | three executors plus lifecycle/C4 |
| No generic handler/durability layer | narrow ports and no writers |
