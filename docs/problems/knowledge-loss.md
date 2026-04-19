---
title: "P03: Knowledge Loss Across Sessions"
status: explored
type: problem
sources: [docs/00_objective.md, refs/AlphaGo-modeled-orch-system.md]
related: [docs/problems/agent-persistence-gap.md, docs/problems/feedback-loops.md]
created: 2026-04-13
updated: 2026-04-13
---

# P03: Knowledge Loss Across Sessions

## Summary
Knowledge gained in one agent session is lost when the context window ends. Knowledge gained by one agent does not transfer to another. The result is an organization with no institutional memory -- the same mistakes get made repeatedly, the same discoveries get re-discovered, and the same context gets re-established by humans over and over.

## The Problem

Agent sessions are ephemeral by default. When a context window fills or a session terminates, everything the agent learned -- about the codebase, the problem domain, failed approaches, successful patterns -- vanishes. This creates several failure modes:

- **Intra-agent amnesia.** The same agent, in a new session, has no access to what it learned before. It will re-explore dead ends, re-discover constraints, and re-derive solutions it already found.
- **Cross-agent isolation.** What one agent learns (e.g., Claude discovers that a particular API has an undocumented rate limit) is invisible to other agents working on the same project (e.g., Codex writing integration code against that same API).
- **Pattern blindness.** When the same type of error occurs across multiple sessions -- say, a recurring test flake caused by a race condition -- no agent recognizes the pattern because no agent has seen it more than once.

The CASS memory system's three-layer approach is directly relevant here: episodic memory captures what happened in individual sessions, working memory holds active context for current tasks, and procedural memory encodes learned patterns and best practices. Without these layers, agents operate with no history.

## Why It Matters

Institutional memory is what separates a functioning organization from a collection of contractors who show up each day with no context. In software teams, this memory lives in documentation, code comments, commit history, tribal knowledge, and the heads of senior engineers. Current agent workflows have none of these accumulation mechanisms.

The cost is paid in human attention (P01): humans become the memory layer, manually transferring knowledge between sessions and agents. This is one of the most expensive and fragile forms of attention expenditure because it requires the human to both remember and correctly re-articulate technical details.

## Concrete Manifestations

- An agent spends 20 minutes understanding a module's architecture. Next session, it spends the same 20 minutes again.
- Agent A discovers that a particular test must be run with a specific environment variable. Agent B, working on the same repo an hour later, hits the same failure and spends time debugging it from scratch.
- Over ten sessions, an agent encounters the same flaky test three times and treats it as a novel failure each time.

## The CASS Memory Model

The CASS system's ACE pipeline (Generate, Reflect, Validate, Curate) provides a concrete pattern for addressing this:

- **Generate**: Capture raw observations from each session.
- **Reflect**: Identify which observations represent reusable knowledge.
- **Validate**: Verify that extracted knowledge is accurate and generalizable.
- **Curate**: Promote validated knowledge into long-term storage where future sessions can access it.

This pipeline transforms ephemeral session data into durable institutional knowledge.

## Open Questions

1. What is the right granularity for memory? Too fine (every tool call) creates noise; too coarse (session summaries) loses critical detail.
2. How do we handle memory staleness? Codebase knowledge from a month ago may be wrong today. What triggers re-validation?
3. How should cross-agent memory work in practice -- shared memory store, message passing, or a dedicated knowledge agent that other agents query?

## Cross-References
- [P01: Human Attention Scarcity](human-attention-scarcity.md) -- Humans currently serve as the memory layer
- [P02: Agent Persistence Gap](agent-persistence-gap.md) -- Persistence without memory is hollow
- [P07: Feedback Loop Absence](feedback-loops.md) -- Memory is a prerequisite for learning
