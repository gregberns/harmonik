---
title: "S08: Memory Layer"
status: seed
type: subsystem
solves: [P03, P07]
uses: [CASS]
language: Go
related: [docs/components/external/cass.md, docs/components/external/cass-memory.md, docs/subsystems/improvement-loop.md, docs/subsystems/event-bus.md, docs/subsystems/agent-runner.md]
created: 2026-04-13
updated: 2026-04-19
---

# S08: Memory Layer

## Summary
The memory layer ingests agent session logs into CASS so that future agents can search prior sessions and the improvement loop can analyze patterns. The MVH scope is intentionally minimal: just CASS, pointed at the canonical session-log directory. The richer three-layer cognitive architecture (episodic / working / procedural) is deferred until usage clarifies what curation is actually needed.

## Purpose
Agent sessions are ephemeral. When a session ends, everything the agent learned -- what worked, what failed, which approaches were tried and abandoned -- dies with it (P03). The next agent working on a related problem starts from scratch. Multiply this across hundreds of agent sessions and the system is perpetually re-learning the same lessons.

The memory layer captures what agents do and makes it searchable. CASS handles the indexing and retrieval. Future agents query CASS for relevant prior sessions when assembling context. The improvement loop (S09) reads CASS-indexed sessions to find patterns.

## Key Responsibilities (MVH scope)
- **CASS configuration and operation.** Run a CASS instance configured to index the canonical session-log directory.
- **Session-log capture orchestration.** Ensure that every agent's session log lands somewhere CASS can read. The agent runner (S04) is responsible for *producing* logs at known locations per agent type; the memory layer is responsible for ensuring CASS *consumes* them.
- **Context provision (basic).** When the agent runner assembles a prompt, query CASS for relevant prior sessions and include them in context. Initial query strategy: keyword + recency. More sophisticated retrieval is deferred.

## How Logs Flow to CASS

This is the critical detail that has to work for the memory layer to be useful at all. Agent processes do not feed the event bus directly -- their internal state and tool calls are too high-volume and live inside the agent binary's process boundary. Each agent type writes its own session log to a known filesystem location:

| Agent type | Session log location | Format |
|---|---|---|
| Claude Code | `~/.claude/projects/<project-slug>/<session-uuid>.jsonl` | Claude session JSONL |
| Pi | TBD by handler -- needs investigation | TBD |
| (future agents) | Per-handler convention | Per-handler format |

To make CASS work:

1. **Canonical aggregation directory.** Every harmonik-launched session writes to a directory CASS already knows how to read. For Claude Code that means workspaces / launch configurations are arranged so all session logs land under the same `~/.claude/projects/...` tree (not scattered across per-worktree `.claude` directories).
2. **Session UUID injection.** The agent runner pre-supplies the session UUID (or asks the agent to use a known one) at launch, so the resulting log file location is predictable.
3. **Session-log location event.** At launch, the runner emits a `session_log_location` event so consumers (memory layer, scenario harness, debugging tools) can find each session's file without having to guess.
4. **CASS pointed at the right roots.** Memory-layer config tells CASS which root directories to watch. For each agent type CASS already supports natively, this is the only configuration needed. For agent types CASS does not support, we either need a CASS extension or a translation layer (open question).

This needs to be made to work in the Phase-1 bootstrap; a self-build cycle without memory has no learning loop.

## Interfaces

**Inputs:**
- `session_log_location` events from agent runner (S04)
- Filesystem watches on the configured session-log roots
- Direct queries from the agent runner during prompt assembly

**Outputs:**
- CASS indexes maintained over the session-log corpus
- Query responses to subsystems requesting relevant prior context
- Memory-layer status events emitted to event bus (S03)

## Design Principles (MVH)
- **Start simple, expand on evidence.** Just CASS for now. Three-layer cognition (episodic / working / procedural), curation rules, knowledge maturity tracking, deterministic-curation-only -- all deferred. We adopt them when concrete usage shows we need them, not before.
- **Canonical session-log location.** The whole memory pipeline depends on session logs landing where CASS can find them. This is a deployment / configuration concern that the workspace manager and agent runner must cooperate on.
- **No in-process log capture.** Agent process logs are *not* tee'd through harmonik's event bus -- the volume is wrong and the boundary is wrong. Read them from the agent's own log files.

## Open Questions
1. Pi's session log format and location -- needs concrete investigation. If CASS doesn't natively understand Pi logs, we need either a CASS extension or a translator (and this affects the Phase-1 build order).
2. Does the workspace manager need to set environment variables (e.g., `CLAUDE_PROJECT_DIR`) so that per-worktree sessions still aggregate into the same `~/.claude/projects/...` tree, or do we accept per-worktree logs and point CASS at multiple roots?
3. When do we re-introduce the three-layer architecture (working memory summaries, procedural rules)? What concrete signal tells us the simple "just CASS" approach is no longer enough?
4. Retrieval strategy beyond keyword + recency -- when do we add semantic similarity, and what's the embedding/storage approach?
5. Retention -- session logs accumulate forever by default. Same retention question as event JSONL: indefinite, TTL, archive policy?

## Cross-References
- [S04: Agent Runner](agent-runner.md) -- emits `session_log_location` events; owns per-agent-type log conventions
- [S06: Workspace Manager](workspace-manager.md) -- workspace setup must place session logs where CASS can find them
- [S03: Event Bus](event-bus.md) -- transports `session_log_location` events; *not* the carrier for raw agent logs
- [S09: Improvement Loop](improvement-loop.md) -- reads from CASS for execution-pattern analysis
- [P03: Knowledge Loss Across Sessions](../problems/knowledge-loss.md) -- the core problem this subsystem addresses
- [P07: Feedback Loop Absence](../problems/feedback-loops.md) -- memory enables feedback loops
- [CASS component](../components/external/cass.md) -- the index/search engine this subsystem operates
- [docs/bootstrap.md](../bootstrap.md) -- step ordering for getting CASS online during Phase 1
