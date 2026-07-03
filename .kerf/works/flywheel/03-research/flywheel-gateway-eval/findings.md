# Research — Dicklesworthstone/flywheel_gateway as leverage source

> Component: `flywheel-gateway-eval`. Round-4. Source: sub-agent (opus) over `github.com/Dicklesworthstone/flywheel_gateway` + sibling `agentic_coding_flywheel_setup`, 2026-05-30.

## TL;DR
- **It's a Hono/Bun/Drizzle web service + React 19 dashboard.** Operator UI plane for Jeffrey's agentic-coding ecosystem. Wraps NTM/tmux/ACP/SDK driver adapters with a state machine, REST/WS API, BYOA key rotation (CAAM), context rotation, handoff, supervisor-of-helper-daemons, beads bridge. **22 ⭐, 1175 commits in 5 months (~8/day), single primary author, MIT+Rider, pre-1.0 but cohesive.** The "1.5k stars" sibling is the bootstrapper, not this repo.
- **It is ORTHOGONAL to harmonik, NOT a substitute.** It supervises *helper daemons* (`mcp-agent-mail`, `cm-server`), not coding agents themselves; agent lifecycle is delegated to drivers (Claude SDK, NTM, tmux, ACP). Its beads bridge is CRUD-pass-through, not workflow-state. No DOT/queue/node-graph. Architecture collides with harmonik's daemon-as-bead-state-owner.
- **Recommendation: option (b) — port-by-port.** Don't run it alongside harmonik (architectures clash). Don't rewrite harmonik against its bus. **Steal four specific subsystems** as spec input or modest Go ports (see §Leverage table).

## Architecture (the supervisor vs agent split)
**Two distinct lifecycle systems:**
1. **`supervisor.service.ts`** supervises **helper daemons only** (defaults: `mcp-agent-mail` 8765, `cm-server` 8766). Uses `Bun.spawn` + `onExit`, log ring buffer (1000), 5s health-check polling, restart policy (`always|on-failure|never`), `maxRestarts` + `restartDelayMs` backoff. **The harmonik-supervise analog for sidecars, NOT for claude.**
2. **`agent.ts` + `agent-state-machine.ts` + `packages/agent-drivers/`** runs coding agents — does NOT spawn processes itself, delegates to an `AgentDriver` (sdk=Claude SDK over WS / acp=Agent Communication Protocol / ntm=Named Tmux Manager with robot JSON / tmux=raw fallback). Service holds an `AbortController`-keyed monitor, consumes typed events from `drv.subscribe()`, translates ActivityState→LifecycleState. **ADR-004 explicitly rejected wrapping NTM through tmux — they wanted structured events.**

Restart-on-crash for *agents* is policy-driven via context-rotation strategies (checkpoint + relaunch), NOT the supervisor's process-restart loop. Stale-terminal cleanup runs as a periodic job (1hr TTL).

## Feature surface (50+ HTTP route files)
Capability map: agent lifecycle 8-state machine (SPAWNING→INITIALIZING→READY→EXECUTING/PAUSED→TERMINATING→TERMINATED|FAILED, terminal-set guarded, `InvalidStateTransitionError`); BYOA key rotation (`caam/` per-account routing+failover); Destructive Command Guard (Rust pre-execution hook); **context rotation** (4 health levels at **75/85/95%** + 4 strategies: `summarize_and_continue`/`fresh_start`/`checkpoint_and_restart`/`graceful_handoff`); handoffs (formal initiate→pending→transfer→complete/rejected/failed/cancelled state machine + ResourceManifest); supervisor (`Bun.spawn` + restart policy + health endpoints + exp backoff); fleet (multi-repo clone/sync/audit); beads bridge (`br.service.ts` + `bv.service.ts` thin shell wrappers, HTTP routes for ready/list/show/create/update/close + bv triage/insights/plan/graph); OTEL traces + Pino logs + cost-tracker + cost-forecast + alert adapters (Slack/Discord/Webhook); safety (approval, audit-redaction, safety-rules.engine, reservation conflicts); CASS (Cross-Agent session Search). License MIT + OpenAI/Anthropic Rider.

## Context/memory — first-class
`context-health.service.ts` 4-band model w/ graduated interventions; `context-rotation.ts` 4 strategies incl. `graceful_handoff` (new agent picks up from summary); `checkpoint.ts` + `auto-checkpoint.service.ts` + `checkpoint-compaction.service.ts` periodic + pre-rotation snapshots; `handoff-context.service.ts` packages "ResourceManifest" payloads; `tokenizer.service.ts` token counting. No CASS-style long-term memory at this layer — CASS is *cross-session* search.

## Coordination — maps POORLY to harmonik
Primitives: `reservation.service.ts` (Gas-Town-style file/resource reservations — **opposite of harmonik's worktrees-not-reservations locked decision**), `conflict.service.ts`/`git-conflict.service.ts`/`conflict-resolution.service.ts`, `handoff.service.ts` (1:1 agent transfer). **Missing vs harmonik:** no node graph, no `workflow_mode`, no dependency-driven dispatch, no `harmonik run --beads` batch. Beads routes are CRUD pass-through; they don't dispatch work, they expose state to the UI.

## Observability — finished and worth borrowing taxonomy
React 19 dashboard + TanStack Router/Query + Zustand + real-time WS w/ durable replay (migration `0011_durable_ws_replay.sql`); composable widget gallery: **ActivityFeed / AgentList / Gauge / Heatmap / Line/Bar/Pie chart / MetricCard / Table / Text** (`apps/web/src/components/dashboard/widgets/*`); cost dashboard w/ budget gauges + forecast chart; OTEL OTLP; multi-channel alerts; mobile-aware; command palette. **Most finished surface in the repo and strongest port candidate as a widget taxonomy.**

## Relationship to Jeffrey's other repos
Gateway is the **API+UI plane** of a stack of independent siblings: `ntm` (process control + robot JSON), `mcp_agent_mail` (inter-agent messaging), `cm` (Code Mapper / context manager), `bv`/`beads_rust` (the same `br` we use), `cass` (Cross-Agent Search), `caam` (account manager), `dcg` (Destructive Command Guard, Rust), `slb` (status line broker), `ru` (Repo Updater), `ubs` (Update/Backup/Snapshot), `apr`. **ACFS (`agentic_coding_flywheel_setup`, 1494 ⭐)** is the *bootstrapper* — `curl | bash` Ubuntu installer with a manifest-driven (Zod-validated YAML) generator installing 30+ tools onto a fresh VPS. ACFS enables you to run flywheel_gateway; doesn't compose with it at runtime. ACFS topics include `beads`+`bv` → explicit ecosystem positioning.

## Leverage options
| # | Action | Source | Effort | Recommend |
|---|---|---|---|---|
| 1 | **Port lifecycle state-machine vocabulary** into `internal/agent/lifecycle/` (or supervise-daemon dir) — 8 states + VALID_TRANSITIONS table + transition-history ring + `InvalidStateTransitionError`. Cleaner than harmonik's current implicit-via-events model. | `apps/gateway/src/models/agent-state.ts`, `services/agent-state-machine.ts` (~250 LOC) | 1 day Go port | **YES** |
| 2 | **Copy supervisor restart-policy semantics** for `harmonik supervise <daemon>` — health interval, policy enum, max-restarts cap, `restartDelayMs` backoff, log ring buffer, starting-timeout cancellation. Drop-in shape. | `services/supervisor.service.ts` lines ~130-310 (~600 LOC TS) | 1-2 days Go port | **YES** |
| 3 | **Adopt context-health 75/85/95 banding + 4-strategy vocab** in `specs/cognition-loop.md` (refinement of the MemGPT 70/90/100 thresholds — the 4 named strategies give us a vocabulary for what the harness does at each level, not just when). | `services/context-rotation.ts`, `context-health.service.ts` | spec-text only | **YES** (spec input) |
| 4 | **Handoff state-machine shape** as spec input when handoff work lands (initiate/pending/transfer/complete/rejected/failed/cancelled + ResourceManifest). Implementation is too TS-isms-laden to port directly; the shape is excellent. | `services/handoff.service.ts` (VALID_TRANSITIONS map at top) | spec-text only | **YES** (defer) |
| 5 | **React dashboard widget taxonomy** as starting catalog when we build the Pi-extension TUI status panel. Don't port code — reference the names. | `apps/web/src/components/dashboard/widgets/` | reference only | **YES** (reference) |
| 6 | Gateway-as-supervisor (option a) | n/a | n/a | **NO** (architectures clash) |
| 7 | CAAM / DCG / CASS / Fleet/RU / cost-tracker (multi-tenant) / AgentMail bridge / full React frontend | n/a | n/a | **NO** (different problems) |

## Bottom line
Flywheel Gateway is the *companion piece* to ACFS for Jeffrey's stack — a single-operator UI/API that wraps his named tools. **We're not in his stack and shouldn't run his gateway.** We *should* copy four well-designed pieces — the agent lifecycle state machine and the supervisor restart-policy module as Go ports; the context-health banding and the handoff phase state machine as spec input.
