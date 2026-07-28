# Spec draft — input-ack semantics

The amendment is a drift correction to EXISTING requirements, so the drafted
prose lands directly in `specs/`. This file is the reviewable statement of what
each site now says. `kerf finalize` is deliberately NOT invoked (per the card).

## `specs/run-state-machine.md` — RSM-027 (amended in place, not renumbered)

> **RSM-027.** The run lifecycle MUST consume the agent-input contract
> ([agent-input.md] AIS-001, AIS-003, AIS-004, AIS-INV-001); it MUST NOT define
> its own input port, acceptance type, stale terminal, or input-ack timer.
> Specifically:
>
> - The reactor MUST request input via submit actions (a resume-seed submit and
>   a brief submit), each carrying an `InputRequest`; the shell effector MUST
>   call the agent-input port `InputPort.SubmitInput(ctx, InputRequest) (Ack,
>   error)` ([agent-input.md] AIS-001).
> - The reactor MUST honour the BINARY delivery outcome of `Ack` ([agent-input.md]
>   AIS-003). `Ack{Delivered}` records a successful handoff of the input to the
>   driver; it MUST NOT by itself advance a transition that requires positive
>   agent acceptance — it leaves positive acceptance PENDING on the correlated
>   asynchronous terminal. `Ack{Rejected}` (a protocol-level refusal —
>   structured drivers only) MUST route to the fail-closed liveness edge
>   (RSM-025). There is no `Accepted` and no `Degraded` outcome, and no
>   acceptance class or tier: a successful write on the tmux/paste path is a
>   delivery handoff, never positive acceptance.
> - The reactor MUST take the correlated asynchronous events ([agent-input.md]
>   AIS-004) as the acceptance verdict: `agent_input_acked` IS the
>   positive-acceptance signal and MUST advance the transition that the
>   `Delivered` handoff left pending; `agent_input_stale` MUST route to the
>   fail-closed liveness edge (RSM-025). The four input-seam routes are
>   therefore TOTAL — `Delivered` → await the correlated asynchronous terminal;
>   `Rejected` → fail closed; `agent_input_acked` → acceptance path;
>   `agent_input_stale` → fail closed — and none of them is a silent no-op
>   (RSM-INV-002).
> - The shell MUST convert `SubmitInput`'s synchronous `Ack` and the
>   dual-delivered durable `agent_input_acked` / `agent_input_stale` events
>   ([agent-input.md] AIS-004) into reactor events. Correlation MUST use the
>   `Ack`'s driver-internal monotonic input-sequence id (the `input_seq` wire
>   field). For one `input_seq`, `Step` MUST consume the first synchronous
>   outcome exactly once; after a `Delivered`, the FIRST correlated asynchronous
>   terminal (`agent_input_acked` or `agent_input_stale`) wins. `Step` MUST drop
>   only repeated or late observations for an already-resolved `input_seq`; it
>   MUST NOT drop the first `agent_input_acked` merely because `Delivered` was
>   already observed.
> - The bounded output-or-stale window and the acceptance definition belong to
>   the agent-input seam ([agent-input.md] AIS-INV-001); the reactor MUST NOT
>   re-implement them and MUST NOT add a second input timer of its own.
> - The per-submission output-or-stale guarantee ([agent-input.md] AIS-INV-001)
>   composes into RSM-INV-001: a `Rejected` resume seed, or a `Delivered` resume
>   seed whose correlated asynchronous terminal is `agent_input_stale`, MUST
>   feed the run's fail-closed liveness edge (RSM-025), never silence.

## `specs/run-state-machine.md` — RSM-024, first bullet

> - the agent-input output-or-stale bound on the resume seed (§9), which
>   resolves the seed submission within the agent-input bounded window
>   ([agent-input.md] AIS-INV-001; the window value is owned by the agent-input
>   seam) to `Ack{Rejected}`, or to `Ack{Delivered}` followed by its correlated
>   `agent_input_acked` or `agent_input_stale` — a `Delivered` return alone does
>   NOT resolve the seed, because it is a delivery handoff and not positive
>   acceptance;

## `specs/agent-input.md` — §3 glossary, `front-stop`

> - **front-stop** — the composition in which the synchronous `Ack` is an
>   *earlier* delivery-handoff observation placed IN FRONT of the pre-existing
>   async watchdogs (`agent_heartbeat`, commit-poll), which it does NOT remove
>   or weaken. It is not positive acceptance: that is the async
>   `agent_input_acked` event (AIS-003, AIS-004). (see §4.2)

## `specs/agent-input.md` — AIS-005

> The synchronous `Ack` MUST be an earlier **delivery-handoff observation**
> placed IN FRONT of the pre-existing async watchdogs (`agent_heartbeat` per
> [handler-contract.md HC-057], the commit-poll). `Ack{Delivered}` is NOT
> positive acceptance — positive acceptance is the async `agent_input_acked`
> event (AIS-003, AIS-004), and its absence within the bounded window reaches
> `agent_input_stale` (AIS-INV-001). The front-stop MUST NOT remove or weaken
> those watchdogs. Dissolving the kill-ladder watchdog is a downstream
> consumer's concern (the M2→M3 cut line); this spec states the front-stop
> composition and does not rebuild the watchdog in the consumer.

## `specs/handler-contract.md` — HC-070, "Front-stop composition, NOT replacement"

> The synchronous input ack is an earlier, per-input **delivery-handoff
> observation** that **composes in front of** the existing process-liveness
> signals; it is NOT positive acceptance — that is the async
> `agent_input_acked` event above. It does NOT gate first-work dispatch
> (`agent_ready` per §4.9 HC-056 / §5 HC-INV-004 still does) and does NOT
> replace the heartbeat / commit watchdogs (§4.9 HC-057, §7.1).

## `specs/handler-contract.md` — HC-056, "Front-stop composition (HC-070)"

> The per-input synchronous input ack of §4.1a HC-070 composes in front of this
> timeout as an earlier, per-input delivery-handoff observation — NOT positive
> acceptance, which is the async `agent_input_acked` event per HC-070; it does
> NOT replace or satisfy the `agent_ready` gate — first-work dispatch still
> requires `agent_ready` per HC-INV-004, and this `agent_ready` timeout remains
> the process-liveness guard for ready-state.

## `specs/handler-contract.md` — HC-057, "Front-stop composition (HC-070)"

> The per-input synchronous input ack of §4.1a HC-070 is an earlier, per-input
> delivery-handoff observation — NOT positive acceptance, which is the async
> `agent_input_acked` event per HC-070 — that composes in front of this
> heartbeat watchdog; it does NOT replace the heartbeat / silent-hang liveness
> guard (§7.1), which remains the authoritative process-liveness guard for
> extended reasoning.
