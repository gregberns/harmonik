# Dimension 5 spec — integration: harness selection & migration

> Research dimension `integration` (04-research/integration/findings.md) → implementation.
> **Authoritative detail: `C4-selection-config-spec.md` + `C5-workflow-integration-spec.md` +
> `C6-migration-test-spec.md`.** Dimension→component crosswalk + normative summary.

## Normative decision
A run selects its harness by a 4-tier precedence resolver, defaulting to claude (N-1 anchor):
```
per-bead label (harness:<x>)  >  per-queue default  >  per-node DOT attr (harness=<x>)  >  global default(claude)
```
- `Config.DefaultHarness` (default `"claude"`) feeds the global tier; `--default-harness` overrides.
- Unknown harness string at any tier, or duplicate selectors at one tier → error at resolve (fail
  closed).
- Reviewer harness defaults to the implementer's; optional `reviewer_harness` override (e.g.
  always-claude) for while codex's structured-verdict reliability is unproven.
- **Back-compat:** all additions (interface, `AgentTypeCodex`, `harness`/`reviewer_harness` DOT
  attrs, `harness:<x>` bead label, per-queue field, `DefaultHarness`/`CodexBinary`) are **additive**;
  nothing renamed; `HandlerBinary` claude semantics preserved. A no-selection run resolves to claude
  and produces byte-identical launch behavior (golden + regression).
- **Rollout:** C1+C4 (claude-safe) → C2+C3 (codex path, default-off) → C5 (routing) → C6 (enable +
  N-1 proof). Do NOT enable codex until the C3/C6 MUST-TEST checklist passes on the pinned version.

## Implementing components
- **C4** — `ResolveHarness`, config, DOT attrs, per-queue field (gated on named-queues).
- **C5** — cascade + review-loop route off the resolved harness; the `Completion()` branch.
- **C6** — codex twin, regression golden (N-1 proof), operator docs + MUST-TEST checklist,
  `specs/harness-contract.md`.

## Acceptance (summary; full AC in C4/C5/C6)
Per-tier precedence unit tests; absent-all → claude; end-to-end twin codex run traverses
`standard-bead.dot`; codex run skips `/quit`+stale-kill and completes on process exit; regression
proves no-selection = byte-identical claude.

## Risks (carried to implementation)
Billing precedence undocumented (gate on MUST-TEST); heartbeat-staleness bypass must land at
`dot_cascade.go:643`; codex commit non-determinism (commit-after-exit fallback); per-queue tier
depends on named-queues; codex reviewer-verdict reliability unproven (default reviewer=claude).
