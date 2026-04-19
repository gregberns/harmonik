---
title: CASS (Coding Agent Session Search)
status: explored
type: component
category: external
source: https://github.com/Dicklesworthstone/coding_agent_session_search
related: [docs/components/external/cass-memory.md, docs/problems/knowledge-loss.md]
created: 2026-04-13
updated: 2026-04-13
---

# CASS (Coding Agent Session Search)

## Summary
CASS is a Rust binary that provides unified search across coding agent sessions from 18+ different agent tools. It indexes sessions from Claude Code, Codex, Cursor, Gemini, Aider, and others into a single searchable corpus using hybrid BM25 + semantic vector search. CASS turns the ephemeral outputs of agent sessions into a persistent, queryable knowledge base -- the raw material that the memory system builds on.

## Key Capabilities

### Multi-Agent Connectors
18+ connectors covering the major coding agents: Claude Code, Codex, Cursor, Gemini, Aider, and more. Each connector knows where and how the agent stores its session data, abstracting away the differences in storage format and location.

### Hybrid Search
Combines BM25 lexical search with semantic vector search, fused via Reciprocal Rank Fusion (RRF). This means searches work well for both exact terms (function names, error messages) and conceptual queries (how to handle authentication).

### Robot Mode
Structured JSON output with token-budget-aware controls designed for agent consumption. An agent querying CASS gets machine-parseable results sized to fit its context window, not human-formatted prose.

### Cross-Agent Knowledge Transfer
Patterns and solutions discovered in one agent's session are findable by any other agent. A fix developed in a Claude Code session becomes available to a Codex agent working on a related problem. This breaks down the knowledge silos between agent tools.

### Watch Mode
Live re-indexing as sessions progress. New session content is indexed incrementally, so search results include work from the current session, not just completed past sessions.

### Multi-Machine Sync
SSH-based aggregation from remote development machines. Sessions from multiple machines are indexed into a single corpus, supporting distributed development workflows.

### Analytics
Activity heatmaps, tool usage patterns, and coverage metrics. These provide observability into how agents are being used -- which tools, how often, on what types of problems.

## Integration Points for Harmonik

CASS is the **episodic memory layer**. Its role in the system:

- **Foundation for CASS Memory**: CASS provides the raw indexed sessions that CASS Memory's ACE pipeline processes into institutional knowledge. Without CASS, the memory system has no input.
- **Directly addresses P03**: Knowledge Loss Across Sessions (P03) is the core problem CASS solves. Sessions are no longer ephemeral -- they become searchable institutional history.
- **Cross-agent learning**: When multiple agents work on harmonik, patterns discovered by one agent are available to all others through CASS search. This prevents redundant problem-solving.
- **Robot mode enables agent self-service**: Agents can query past sessions without human mediation. An agent stuck on a problem can search for how similar problems were solved before, reducing the human attention cost (P01).
- **Watch mode supports real-time coordination**: Live indexing means agents can search for what other agents are currently doing, enabling a form of ambient awareness across the system.
- **Analytics feed monitoring**: Usage patterns and coverage metrics provide signals for the monitoring subsystem about how agents are performing and where effort is concentrated.

## Limitations and Gaps

- **Read-only**: CASS indexes and searches but does not interpret, summarize, or act on what it finds. The intelligence layer is CASS Memory, not CASS itself.
- **Quality varies by connector**: Some agent tools provide rich session data; others provide minimal logs. Search quality depends on the richness of the source data.
- **No filtering by outcome**: CASS indexes all sessions equally -- successful and failed. There is no built-in way to search only for sessions that achieved their goal, which would be valuable for learning.
- **Indexing latency**: While watch mode provides incremental indexing, there is still a delay between session activity and searchability. Time-critical coordination cannot depend on CASS alone.

## Open Questions

1. Should CASS index harmonik's own orchestration logs in addition to agent sessions? This would make the system's execution history searchable.
2. How should CASS search results be ranked when an agent is looking for guidance? Should recent results be preferred, or should quality/outcome-based ranking be added?
3. Can CASS's analytics capabilities serve as the foundation for harmonik's observability story, or does observability need its own dedicated component?
