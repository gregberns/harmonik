# Change design review

Status: approved

## Scope

The review covered the typed continuity design. It checked ZFC boundaries,
delivery recovery, status request correlation, generic harness separation, and
the iteration policy.

## Review rounds

The review required four changes.

1. Attached tmux recovery now fails closed after an uncertain paste or Enter.
   It sends no second paste or Enter.
2. The existing structured `handler.InputPort` has no durable controller effect
   identifier. An ambiguous call now becomes `DeliveryUncertain`. It cannot
   retry.
3. Delivery state is a sealed transport-specific variant. The generic claim
   does not use tmux names for structured input.
4. `StatusRequest` now waits in `AwaitingDeclaration`. It settles only from an
   exact correlated typed declaration. Its deadline starts in that state.

## Result

The reviewer approved the current design. The core reads no agent prose. The
controller owns typed state and effects. Attached and structured transports use
truthful state models. The replay corpus includes the stated crash and stale
event cases.
