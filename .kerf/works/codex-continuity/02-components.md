# Components: typed Codex continuity keeper

## Existing specification dependencies

| Area | Required result | Depends on |
|---|---|---|
| `specs/hitl-decisions.md` | An open decision remains the one durable source of a crew waiting for an operator decision. Continuity consumes that projection. It does not create a second decision block state. | Existing event model |
| `specs/agent-input.md` | The existing input port remains consumer-owned. Continuity defines its own delivery port and adapts to the input port at the outer edge. | Continuity policy |
| `specs/handler-contract.md` | Harness adapters supply delivery. Shared continuity behavior stays harness-neutral. | Agent input |
| `specs/event-model.md` | Continuity persists its own controller record and journal in the first vertical. The event model remains a consumer of derived observations. | New continuity spec |
| `specs/process-lifecycle.md` | A terminal fact is explicit and comes from the work owner or controller. A crew cannot self-record terminal state. | New continuity spec |

## New specification

### `specs/continuity-keeper.md`

This new specification owns the substrate-neutral continuity contract.

It defines:

- continuity instance identity;
- allowed work states and transition authority;
- typed turn-ended lifecycle input;
- agent declaration request and controller acceptance;
- pure policy actions;
- prompt claim, delivery, acknowledgement, retry, and cap semantics;
- source adapter and delivery adapter requirements;
- replay and crash recovery guarantees.

It does not define context compaction or Claude handoff/reset behavior.

## Implementation components

| Component | Responsibility | Depends on |
|---|---|---|
| Continuity domain | Typed identities, events, state, transitions, and pure reducer. It has no harness, text, tmux, filesystem, or clock reads. | New continuity spec |
| Continuity projection | Sole writer of accepted state and delivery journal. It replays durable events. | Domain and event model |
| Declaration command | Sends a typed agent request to the daemon. It cannot write accepted state directly. | Projection and lifecycle spec |
| Codex rollout source | Temporary attached-session adapter. It maps only lifecycle identity to turn-ended input. | Domain source contract |
| Codex notify source | Supported future adapter. It maps the documented Codex notification to the same input. | Domain source contract |
| Input delivery adapter | Uses the existing typed delivery seam. The attached-session implementation delegates to existing tmux delivery. | Agent input and handler contract |
| Daemon wiring | Binds one continuity instance to its source, projection, and delivery port. | All preceding components |

## Dependency order

1. New continuity specification and event taxonomy.
2. Domain types and pure reducer.
3. Durable projection and declaration command.
4. Delivery claim and adapter port.
5. Attached Codex rollout source and tmux delivery adapter.
6. Replay, fault, and live vertical tests.
7. Codex notify source.
8. Other harness adapters.

## Deliberate exclusions

- `internal/keeper.Watcher` remains the Claude context-reset watcher.
- `internal/codexreactor` remains the App Server event reactor. It is not the
  source for the attached rollout bridge.
- The stopped shell pilot is not an implementation component.
