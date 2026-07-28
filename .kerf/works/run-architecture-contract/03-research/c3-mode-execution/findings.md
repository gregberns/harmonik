# C3 research — Mode execution boundaries

## Questions

1. What is common to all mode invocations and results?
2. Which decisions remain mode-owned?
3. Where is lifecycle behavior duplicated?
4. Which package direction is viable?
5. What behavior-preserving seams already exist?

## Findings

`beadRunOne` currently selects `single`, `review-loop`, or `dot`, but also owns
worktree/worker acquisition, launch setup, merge/gate/budget processing, and
terminal effects. It is therefore neither a narrow mode dispatcher nor a
mode-neutral lifecycle owner.

### Common invocation

All modes consume sealed Run/Bead/worktree facts, resolved workflow and
harness facts, a lifecycle capability, narrow merge/gate/ledger/event ports,
and mode-specific configuration. They can return one lossless envelope
carrying the original EM-005 status
(`SUCCESS | FAIL | RETRY | PARTIAL_SUCCESS`), the daemon-authoritative
failure class on every `FAIL`, and a mode-specific terminal payload.
A handler-hint/daemon-classifier disagreement is evidence and the daemon value
wins per HC-059; it must not be collapsed before BI-010a applies.

`internal/runexec` already owns compatible pure decision vocabulary and
machines. Execution-model EM-005/EM-012a/EM-012b and run-state-machine
RSM-001–RSM-011 require sealed identity, explicit mode resolution, pure
reactors, and consumer-owned ports.

### Mode-owned policy

| Mode | Policy that must remain outside lifecycle |
| --- | --- |
| Single | one phase, independent-session survival eligibility, single result/merge interpretation |
| Review | implementer/reviewer alternation, checkpoint/continuity, verdict/retry/no-progress/budget decisions |
| DOT | graph traversal, node type dispatch, nested sub-workflows, cognition gates, per-node result routing |

`runReviewLoop` duplicates implementer/reviewer lifecycle and still owns
checkpoint, subscriptions, retries, and budget. `driveDotWorkflow` owns graph
coordination while `dispatchDotAgenticNode` duplicates phase/session
lifecycle. The DOT sub-workflow runner calls daemon-private node dispatchers,
and DOT core calls review-specific helpers; these are dependency knots, not
reasons for a reverse import.

### Viable direction and reusable seams

```text
daemon composition
  -> mode-neutral run coordinator
      -> mode executor (single | review | DOT)
          -> lifecycle port
          -> narrow mechanism ports
```

Lifecycle must not import a mode package. Mode packages must not import
daemon. Existing `runexec.Dispatch`/`Run`, `runloop.DispatchSegment`,
`RunShell`, scenario gate, reviewer-harness resolver, and `runlaunch` leaves
are reusable. `RunBridge` is transitional and must not grow.

RL-01's pure `reviewcycle` kernel and `continuity` service are staged but
unused. They are candidates for explicit adoption only as a coherent review
mode implementation; parallel replacements would create shadow policy.

## Risks and conflicts

- A generic executor with mode switches would simply rename `beadRunOne`.
- A result containing queue/Beads writers would leak terminal ownership back
  into modes.
- Moving DOT core before inverting sub-workflow dispatch and review-helper
  dependencies creates a daemon import cycle.
- Heartbeat, retry, resume, and cleanup differences cannot be flattened as
  incidental implementation detail.
- Reviewcycle and continuity vocabulary must land with their consumers or be
  retired; dormant core vocabulary is not progress.

## Pattern and decision status

Use one small executor interface and one result vocabulary, with separate
mode implementations and a lifecycle port. Mode code returns decisions and
evidence; C4 owns durable composition. `RETRY` consumed inside a review/DOT
loop is an in-mode continuation, while an exhausted/no-in-mode-retry
`FAIL{failure_class=transient}` is a terminal mode result. No unresolved
blocker prevents design.
