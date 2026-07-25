# Problem space: review-loop decoupling

## Summary

`internal/daemon/reviewloop.go` cannot complete its planned behavior-neutral move into
`internal/runloop`. A live relocation probe after LIFT L7 found 18 daemon-private symbols across
29 call sites. Those symbols are not 18 independent responsibilities: they expose five lifecycle and
I/O capabilities whose ownership is currently mixed into the daemon package:

1. per-phase interactive substrate creation and launch diagnostics;
2. post-ready brief delivery, completion watchers, and reviewer-budget artifacts;
3. session-ID capture, durable checkpoint, event publication, and protocol ACK;
4. ready/heartbeat/session lifetime;
5. reviewer harness and launch routing.

The monolithic review loop also contains deterministic cycle policy—iteration state, progress and
no-progress decisions, verdict routing, cap handling, session selection, and event payload construction—
interleaved with tmux, git, filesystem, network, goroutine, and cleanup effects.

Moving the file now would either export daemon internals, create a callback bag, or widen `RunPorts` into
another service locator. The system instead needs a functional core, explicit lifecycle ownership, and
small capability boundaries around unavoidable I/O.

## Confirmed operator intent

The operator approved this direction on 2026-07-24 and authorized autonomous progression through kerf
signoffs and implementation. The goal is architectural improvement and testability, not merely making the
compiler accept a package move.

## Goals

- Make review-cycle decisions deterministic and directly testable without tmux, git, filesystems,
  goroutines, clocks, emitters, or networks.
- Centralize I/O in phase- or capability-owned imperative shells.
- Reduce the review coordinator's daemon-private dependencies from 18 symbols to stable owner APIs or
  narrow consumer-defined ports.
- Make resource ownership explicit: sessions, hook callbacks, heartbeat loops, watchers, worktrees, and
  cleanup must have bounded lifetimes and idempotent teardown.
- Preserve local/remote placement explicitly rather than inferring it from a shared nullable runner.
- Preserve checkpoint and event ordering as contracts.
- Return to LIFT L8 only when a fresh relocation probe and behavioral parity gates prove the coordinator
  is a mechanical move.
- Improve shared dependencies in ways that also benefit single-run and DOT paths.

## Non-goals

- No generic runtime/service-locator port.
- No interface or callback per compiler error.
- No wholesale move of `pasteinject.go` or `tmuxsubstrate.go`.
- No full reducer/effect interpreter in the first implementation program.
- No DOT conversion or LIFT L12/L13 work.
- No change to watchdog timing, kill policy, routing policy, remote persistence, event order, or ACK
  behavior without an explicit reviewed contract.
- No daemon deployment in this work.

## Constraints and invariants

- `internal/runloop` must never import `internal/daemon`.
- Reviewer execution and reviewer artifacts remain box-A-local even when the implementer is remote.
- Implementer workspace I/O may be remote and must carry location explicitly.
- Current iteration cap, verdict routing, needs-attention mapping, no-progress behavior, and result payloads
  remain stable.
- Current observable event order and payloads remain stable unless a later spec explicitly changes them.
- Session capture/checkpoint behavior must model the context-commit SHA used by the no-commit baseline.
- No protocol ACK may be reordered accidentally during extraction.
- Synthetic daemon heartbeat must not masquerade as reviewer activity and indefinitely extend budgets.
- Phase helpers may not return while live resources still depend on their local worktree/session unless
  ownership is explicitly transferred in a typed handle.
- Existing locked architectural decisions remain closed.

## Success criteria

- A pure review-cycle kernel has exhaustive transition, property, and payload-parity tests.
- Ordered lifecycle traces characterize launch, ready, delivery, completion, events, and teardown.
- Each effect boundary has one cohesive responsibility and no daemon concrete types in its consumer API.
- Local and remote placement are explicit in request values and covered by conformance tests.
- Watchers and lifecycle bindings expose cancel/join or close semantics and pass race/leak tests.
- A fresh L8 relocation probe reports zero daemon-private undefined symbols.
- L8 is a mechanical move plus stable API qualification, with production composition-root wiring verified.
- L9 resumes only after L8 and the new boundaries stabilize.

## Preliminary affected specification areas

- execution model: phase lifecycle, cancellation, teardown, and placement;
- handler contract: session capabilities, ready/heartbeat semantics, protocol ACK;
- event model: ordering and payload truthfulness;
- workspace model: implementer versus reviewer artifact locality;
- control points: review verdict, iteration cap, no-progress and budget outcomes;
- process lifecycle: tmux session ownership and watcher cleanup;
- reconciliation/recovery: durable session checkpoint and context baseline.

