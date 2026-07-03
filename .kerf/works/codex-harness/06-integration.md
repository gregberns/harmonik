# 06 — Integration: composing the codex-harness change

> Plan-jig Pass 6. How the six components compose into a landable sequence, the cross-component
> interfaces, the end-to-end flow, and the rollout. The normative contract is extracted to `SPEC.md`
> (→ `specs/harness-contract.md` on finalize).

## Landing order (DAG-respecting, each step independently mergeable + green)

1. **C1 — Harness seam (no behavior change).** Land the `Harness` interface, `AgentTypeCodex`,
   `ClaudeHarness`, and the registry/`launchSpecBuilder` wiring. Claude runs byte-identically
   (golden test C1 AC1.2 + live smoke). **This is the keystone — everything else plugs into it.**
2. **C4 — Selection & config.** `ResolveHarness` + `Config.DefaultHarness` (default claude) + the DOT
   `harness` attr + bead-label/queue tiers. Still resolves to claude everywhere by default — no
   visible change, but the routing is now harness-addressable.
3. **C2 — Codex adapter.** `CodexHarness` + `buildCodexLaunchSpec` + JSONL parser + captured
   thread_id + commit-after-exit fallback. Reachable only when C4 resolves to codex (still off by
   default).
4. **C3 — Auth/billing guard.** Env strip + `forced_login_method` materialize + pre-flight assert.
   Gates every codex launch; must land before any real codex run is enabled.
5. **C5 — Workflow/review-loop integration.** Cascade + review-loop route off the resolved harness;
   the `Completion()` branch gates the `/quit`+heartbeat-staleness machinery. End-to-end codex run
   works on the twin.
6. **C6 — Migration/test + docs.** Codex twin, regression golden, scenario/exploratory tests,
   operator docs, `specs/harness-contract.md`. Proves N-1 safety; flips codex on for an opt-in queue.

Steps 1–2 are pure-claude-safe (mergeable anytime). Steps 3–5 build the codex path but keep it
default-off. Step 6 is the enable + proof.

## Cross-component interfaces (the contract glue)

| Producer | Interface | Consumer |
|---|---|---|
| C1 | `Harness` (LaunchSpec/Seed/Retask/Teardown/DetectReady/SessionIDPolicy/Completion) | C2 (codex impl), C5 (cascade calls it) |
| C1 | `AdapterRegistry.ForAgent(core.AgentType)` | C4 (after resolve), C5 |
| C4 | `ResolveHarness(HarnessSelectors) → core.AgentType` | C5 cascade |
| C2 | captured `thread_id` in run state | C2 `Retask`, C5 review-loop iter≥2 |
| C2 | `Completion()==ProcessExit` | C5 workloop branch (skip `/quit`+stale-kill) |
| C3 | env-strip + pre-flight `assertChatGPTPlan` | C2 launch path |
| C6 | codex twin (scripted JSONL + `Refs:` commit) | C2/C5 tests |

## End-to-end flow (a codex-selected bead)

```
queue submit (bead labeled harness:codex)
  └─ daemon dispatch → ResolveHarness → AgentTypeCodex → registry → CodexHarness
        ├─ C3 pre-flight: assertChatGPTPlan() — fail closed if not subscription
        ├─ worktree create (SHARED, git worktree add -b run/<id>)   ← unchanged
        ├─ CodexHarness.LaunchSpec → tmux window runs:
        │     codex exec --json --sandbox workspace-write -a never -C <wt>   (env: OPENAI_API_KEY/CODEX_API_KEY stripped)
        ├─ parse stdout JSONL → capture thread_id (thread.started); DetectReady on first event
        ├─ codex edits + (ideally) commits with Refs:<bead>; process EXITS (Completion=ProcessExit)
        │     └─ workloop waits on sess.Wait (no /quit, no stale-kill); if no trailer-commit → commit-after-exit fallback
        ├─ commit-detection (SHARED): worktree HEAD != parent? Refs:<bead> trailer?   ← unchanged
        ├─ review node → CodexHarness (or reviewer_harness override) → codex exec resume <thread_id> "<feedback>"
        │     └─ reviewer writes verdict → reviewer_verdict event (SHARED parsing)
        ├─ iter≥2 if REQUEST_CHANGES → codex exec resume <thread_id> "<feedback>"  (spawn-per-turn)
        └─ merge-one-at-a-time to target branch (SHARED, locked rebase+FF)   ← unchanged
           └─ br close <bead>   (daemon owns terminal transition)   ← unchanged
```

Everything marked SHARED is reused without modification — the seam is exactly the `CodexHarness`
methods + the `ResolveHarness`/`Completion()` branch points.

## N-1 back-compat proof (the migration story)

- No existing bead/queue/workflow carries a harness selector → `ResolveHarness` returns
  `AgentTypeClaudeCode` → `ClaudeHarness` → byte-identical launch (C1 golden + C6 R6.1 regression).
- `Config.DefaultHarness` defaults `claude`; `Config.HandlerBinary` semantics preserved.
- All additions (interface, enum value, DOT attr, bead label, queue field, config keys) are
  **additive**; nothing renamed/removed. The `Completion()` branch defaults to claude's existing
  EventStreamThenQuit path.

## Risks carried into implementation

- **R-A (billing, highest):** `codex exec` env precedence is undocumented/version-variable. Mitigation
  is defense-in-depth (C3) + the C6 MUST-TEST checklist run on the pinned codex version before any
  production codex run. Do NOT enable codex (step 6) until the checklist passes.
- **R-B (heartbeat/completion):** the `Completion()` branch must correctly bypass the
  heartbeat-staleness kill for codex, or long codex turns get killed mid-run (decompose-review B1).
  Covered by C5 AC5.2.
- **R-C (commit non-determinism):** codex may not commit / may `git add -A` extra files (#8548).
  Mitigation: commit-after-exit fallback (C2 R2.5) + worktree is the only thing staged.
- **R-D (named-queues dependency):** per-queue tier depends on `project_named_queues_work`; ship
  bead+node+global first if it hasn't landed (C4 gating note) — not a blocker.
- **R-E (reviewer verdict reliability):** codex reviewer structured-verdict path unproven (C6 R6.5);
  default reviewer_harness=claude until verified.

## Spec-first artifact

`SPEC.md` (this pass) holds the normative `Harness` contract, the selection-precedence rule, and the
billing-guard requirement. On `kerf finalize` it is copied to `specs/harness-contract.md` (project
convention: normative specs live in `specs/`).
