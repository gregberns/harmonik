# 3 — Operator direction: the event-substrate / record-replay quality mechanism

> Captured 2026-07-13, operator's own framing (before bed). This is the concrete, grounded
> alternative to the emergent-systems abstraction — it's ordinary strong engineering, and it's
> what the adversarial panel (doc 2) was pointing at when it said "the real answer is gates,
> interfaces, and testable structure, not a sim." The open questions at the bottom are what the
> fresh agent panel is being asked to think through overnight.

## What was never done here (but done in a prior project)
The operator's Python project encoded coding principles as enforced structure: functional-
programming discipline, **hexagonal architecture**, modularity, correct interfaces, and
**no module cycles (layered architecture only)**. Harmonik never encoded these — "hoped for the
best." Linting was assumed to catch things like a 2300-line function; it didn't here. Even a
cyclomatic-complexity monitor / hard-flag-over-a-threshold "doesn't always get you what you
want" — necessary, not sufficient.

## The real mechanism: a stable event + input interface as the base layer
Make the core interactions flow through a well-defined **event/input interface**, then layer
behavior on top of it. Because the base layer is stable and observable, you can **test without
touching the real thing**, and layer in **property tests for invariants**.

Worked example — the **session-restart process** (composable `claude-handler` + `tmux-handler`):
the sequence is a stream of events —
1. token count crosses threshold → **handoff needed**
2. send the handoff message
3. detect the handoff-file **write is done**
4. wait for the **final response** (model is done working)
5. send **/clear**
6. handler **picks up the new session**
7. submit the **brief** command

"We may already have all those events." If they **stream in and are logged**, then by now there
are **hundreds/thousands of real examples** → run them through the code → **replay-test offline**
→ swap any layer for a testable fake. This is exactly the **Codex app-server plan's** pattern:
capture a raw stream first, then layer decoding, then whatever — so any layer can be swapped for
something testable. (See `plans/2026-07-11-codex-app-server-replan/` / `.kerf/works/codex-app-server/`.)

## The honest state
The system is large. It "kind of holds together" but isn't "rock solid" the way the operator
builds daily. We can rebuild a bunch of it. Session-restart is just ONE process — the same
record→replay→property-test approach should generalize (remote, agent-input, run-lifecycle).

## The open questions (for the fresh panel)
1. **Where do we start?** First self-contained vertical where record→replay→property-test gives
   the most leverage. Operator's nominee: session-restart (events likely already exist).
2. **What are the quality mechanisms** that actually enforce the principles (FP/hexagonal/
   modularity/interfaces/no-cycles) reliably — beyond linting, which is necessary-not-sufficient?
3. **THE hard one — measurement.** After a rebuild costing several days / **200–500M tokens**,
   how do we know it worked? **Compared to what?** A/B testing this seems very hard. **What do we
   measure?** Without an answer here, we can't tell if any rebuild helped.

## Status
Operator's direction of record. Fresh agent panel dispatched on Q1–Q3; synthesis to follow in
this folder. Fleet still frozen; keeper HELD; nothing dispatches.
