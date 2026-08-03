# Keeper research index

Status: research, with one activated implementation slice. Kerf work
[`codex-continuity`](../../../.kerf/works/codex-continuity/) is authorized for
the Codex-only window on 2026-08-02. It stays outside the active core rewrite.

The charter keeps the keeper outside the core set. The core can be complete while
the keeper is disabled. The documents here define a later, evidence-led keeper
and harness-lifecycle lane. They do not authorize a third active lane.

## What is known

- The keeper is a separate package with a useful pure cycle machine. Its outer
  watcher still joins too many jobs.
- Claude and Codex have different context and stop signals. They need separate
  adapters. They do not need separate keeper policy engines.
- A completed Codex turn does not prove that a crew should stop. Durable crew
  work state must make that decision.
- `make test-keeper-conformance` is false green. It reports success while its
  `internal/keeper` invocation runs no matching tests. This is the first repair
  when keeper work resumes.

## Reading order

1. [Current structure and modularity](01-current-structure-and-modularity.md)
2. [Reliability and test plan](02-reliability-and-test-plan.md)
3. [Codex and multi-harness design](03-codex-and-multi-harness.md)
4. [`../LANES.md`](../LANES.md) "Deferred post-core keeper and harness-lifecycle lane"

## Decided planning direction

Use this flow. Do not build a framework before both verticals prove its need.

```text
harness codec -> normalized signal -> pure keeper policy -> delivery effect
                                      |                 -> durable journal
                                      -> crew work state
```

Build the approved Codex continuity vertical first. Use the documented Stop
hook when its lifecycle contract is available. A registered rollout source is a
supervised transition source only. Then preserve one Claude vertical. Extract
common types and ports only when both verticals prove they need them. App-server
support is a separate later adapter.

## Open decisions for the future lane

- The approved continuity record needs a production implementation and replay
  proof. The current isolated domain scaffold must be revised before use.
- When can the documented Codex Stop-hook source replace the registered rollout
  source without a separate app-server process?
- Which keeper responsibilities should leave `Watcher.Run` first: decision
  reaping, dashboard nagging, or live-pane recovery?
- What is the smallest public event vocabulary for keeper decisions and outcomes?

Do not answer these by adding harness branches to the current watcher.
