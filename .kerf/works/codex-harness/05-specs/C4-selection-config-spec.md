# C4 — Selection & config surface — change spec

## Requirements (from 03-components.md)
R4.1 global default-harness config (default `claude`). R4.2 DOT node `harness`/`agent_runtime` attr.
R4.3 per-bead override + per-queue default, precedence resolver with unit tests per tier. R4.4
preserve `Config.HandlerBinary` claude semantics; codex binary resolved by the adapter.

## Research summary
`04-research/integration/findings.md`: precedence **per-bead > per-queue > per-node > global(claude)**.
Each tier reuses an existing surface — bead label (`codename:`-style bare label `harness:codex`),
named-queue config field, DOT generic attr-map (`dotparser.go:156-216`, `node.go:13-104`), and the
global `Config`. Absent all → claude (the N-1 anchor). `handler_ref` does NOT select a binary today
(`dot_cascade.go:216,802-805`); binary is `deps.handlerBinary` (default `claude`). The per-queue tier
depends on the named-queues work (`project_named_queues_work`); if not landed, ship bead+node+global
first.

## Approach
One pure resolver `ResolveHarness(sel HarnessSelectors) core.AgentType` with documented precedence:
```
func ResolveHarness(s HarnessSelectors) core.AgentType {
    if s.BeadLabel != "" { return parseHarness(s.BeadLabel) }      // harness:codex
    if s.QueueDefault != "" { return parseHarness(s.QueueDefault) }
    if s.NodeAttr != "" { return parseHarness(s.NodeAttr) }        // DOT harness=...
    if s.GlobalDefault != "" { return parseHarness(s.GlobalDefault) }
    return core.AgentTypeClaudeCode                                 // N-1 anchor
}
```
`parseHarness("codex")→AgentTypeCodex`, `("claude"|"")→AgentTypeClaudeCode`, unknown→error (fail
closed). The cascade calls `ResolveHarness`, then `AdapterRegistry.ForAgent(agentType)` (C1) to pick
the launch-spec builder + adapter. `Config.DefaultHarness` (new string, default `claude`) feeds
`GlobalDefault`. `Config.HandlerBinary` is unchanged and continues to name the claude binary; the
codex binary path is a codex-adapter concern (default `codex`, overridable via a `Config.CodexBinary`).

## Files & changes
- **NEW** `internal/daemon/harnessresolve.go` — `HarnessSelectors`, `ResolveHarness`, `parseHarness`.
- **MODIFY** `internal/daemon/daemon.go` (`Config`) — add `DefaultHarness` (default `claude`) and
  `CodexBinary` (default `codex`); CLI flag `--default-harness`.
- **MODIFY** `internal/core/node.go` + `internal/workflowvalidator/dotparser.go` — recognize the
  `harness` (alias `agent_runtime`) node attribute into the attr-map (parsing is free; add to the
  known-attr allowlist if one exists).
- **MODIFY** `internal/queue/types.go` — add an optional `harness` field to `Group`/`Item` (per-queue
  default tier). **Gated:** if the named-queues field isn't ready, leave this for a follow-up bead and
  ship the other three tiers (documented in C6).
- **MODIFY** the cascade (`dot_cascade.go:499-525`) — compute selectors, call `ResolveHarness`, route.
- **Bead-label read:** the resolver reads the bead's `harness:<x>` label from the bead record already
  available at dispatch (no schema change; `br` label convention).

## Acceptance criteria
- AC4.1 Unit tests: each precedence tier wins over lower tiers (bead>queue>node>global) and absent-all
  → `AgentTypeClaudeCode` (one test per tier + the default).
- AC4.2 `parseHarness` maps `codex`→codex, `claude`/`""`→claude, unknown→error.
- AC4.3 A DOT node with `harness=codex` resolves to the codex adapter; without it → default.
- AC4.4 `Config.DefaultHarness` defaults `claude`; `--default-harness codex` flips the global default;
  `HandlerBinary` semantics for claude unchanged.
- AC4.5 A bead labeled `harness:codex` routes to codex even under a `claude` global default.

## Verification
```
go test ./internal/daemon/... -run 'ResolveHarness|HarnessSelect'
go test ./internal/workflowvalidator/... -run 'HarnessAttr'
# explore: `harmonik queue dry-run codex-batch.json` shows the resolved harness per item
```

## Error handling / edge cases
- Conflicting selectors at the same tier (two bead labels) → error, fail closed.
- Unknown harness string anywhere → error at resolve, not at launch (fail early with the offending
  value named).
- Per-queue tier absent (named-queues not landed) → resolver simply skips that tier; no breakage.

## Migration / back-compat
All tiers additive; absent → claude. No renamed/removed config keys. `HandlerBinary` preserved (R4.4).
N-1 safe (R6.1 regression test in C6).
