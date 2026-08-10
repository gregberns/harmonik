# Area 2 — seven event types escape the registry, the compat table, and the test written to catch this

**Class:** A1-adjacent (declared vocabulary with no registration). **Phase:** A. **Lock unit:**
`internal/core` + the emitting package. **Status:** enumerated and verified at `d5a12348f`, 2026-08-09.

The smallest genuinely valuable task in this directory. Seven instances, each a few lines, and the payoff
is that a class of silent failure becomes a compile-or-test failure.

## The seven

`internal/core/eventtype.go` declares 181 `EventType` constants. `mustRegister` / `mustRegisterAtVersion`
across `internal/core` cover 174 of them. These seven are declared and never registered:

| constant | value |
|---|---|
| `EventTypeGovernorSignal` | `governor_signal` |
| `EventTypeLoopObservedPhantomDone` | `loop_observed_phantom_done` |
| `EventTypeResourceBreach` | `resource_breach` |
| `EventTypeWorkerOffline` | `worker_offline` |
| `EventTypeWorkerReport` | `worker_report` |
| `EventTypeWorkerTunnelFailed` | `worker_tunnel_failed` |
| `EventTypeWorkerUnhealthy` | `worker_unhealthy` |

Reproduce:

```sh
# constants
grep -c 'EventType = ' internal/core/eventtype.go                      # 181
# registrations (note BOTH forms; mustRegisterAtVersion is easy to miss)
grep -rEn 'mustRegister(AtVersion)?\(' --include='*.go' internal/core | grep -v _test.go
```

`governor_signal` is the live one: `internal/daemon/movementgovernor.go:347` emits it on every governor
evaluation and **discards the error**:

```go
	_ = dispatchGates.bus.Emit(ctx, core.EventTypeGovernorSignal, raw) //nolint:errcheck // best-effort observability emit
```

So an emit into a registry that has never heard of the type fails, silently, forever.

## Why the existing test cannot see it

`internal/core/eventtype_coverage_gjyks_test.go` exists precisely to prevent this — its comment says every
type "gets a `mustRegister` constructor and a mandatory `pertypecompat` row". It cannot catch these because
**it iterates the registered types.** A type that was never registered is not in the set the test walks, so
the test is structurally incapable of failing on the thing it was written to prevent.

That is the transferable part of this area, and it is worth more than the seven fixes: **a coverage test
must iterate the declaration side, not the registration side.** Anything else can only detect
registrations that lost their declaration, which is the harmless direction.

## What to do

Two tasks, and the second is the one that matters.

**Task A — register the seven.** Each needs a payload constructor and a `pertypecompat` row. For six of
them, decide first whether the type is *live* (something emits it) or *vestigial* (declared for a design
that never shipped). Only `governor_signal` is confirmed live. **Deleting a vestigial constant is a
legitimate outcome and is better than registering a payload nobody emits** — surface which is which rather
than mechanically registering all seven.

**Task B — invert the coverage test.** Enumerate `EventType` constants from the declaration side (a
generated list, or reflection over the constant block, or a `go:generate` step), and assert every one has a
registration and a compat row. This is what makes the class of defect impossible rather than fixed once.

**Task C — decide about the discarded emit error.** `//nolint:errcheck // best-effort observability emit`
is a defensible policy for a metrics emit and an indefensible one for the *only* signal that the registry
is wrong. At minimum the unregistered-type error should be distinguishable from a transient bus failure and
logged once. This is a judgement call for the operator, not a mechanical fix.

## Also here: six literal registrations

Still in `internal/core`, registering by string rather than by the constant that holds the string:

```
internal/core/eventreg_hqwn59.go:242   mustRegister("agent_message", ...)
internal/core/eventreg_hqwn59.go:245   mustRegister("agent_presence", ...)
internal/core/eventreg_hqwn59.go:527   mustRegister("session_keeper_config_rejected", ...)
internal/core/eventreg_hqwn59.go:567   mustRegister("agent_input_acked", ...)
internal/core/eventreg_hqwn59.go:568   mustRegister("agent_input_stale", ...)
internal/core/eventreg_wkzlc.go:19     mustRegister("run_stale", ...)
```

These are same-package A1 instances and therefore free — no new dependency, no judgement about which
concept is meant. Note that `agent_input_acked` and `agent_input_stale` are the **two findings the first
executed wave declined** (`../report.md`); the reason was that the *consumer* side would have made `core`
import a leaf package. That reason does not apply here — these are declarations inside `core` itself
referencing constants inside `core` itself. Fix them; leave the consumer sites alone.

## Gate

```sh
go test -count=1 ./internal/core/... ./internal/daemon/...
```

Green before, green after. Confirm the inverted coverage test (Task B) **fails on the pre-change tree** —
a test that passes before the fix is not testing the fix. That is the whole gate; this area is small enough
that its blast radius is two packages.
