---
title: "S08: Memory Layer"
status: seed
type: subsystem
solves: [P03, P07]
uses: [CASS, CASS Memory]
related: [docs/components/external/cass.md, docs/components/external/cass-memory.md, docs/subsystems/improvement-loop.md, docs/subsystems/event-bus.md]
created: 2026-04-13
updated: 2026-04-13
---

# S08: Memory Layer

## Summary
The memory layer stores, retrieves, and evolves institutional knowledge for the system. It implements a three-layer cognitive architecture: episodic memory (raw session records), working memory (structured summaries and diary entries), and procedural memory (actionable rules and playbook entries). Knowledge flows upward through these layers over time, from raw experience to distilled operational wisdom.

## Purpose
Agent sessions are ephemeral. When a session ends, everything the agent learned -- what worked, what failed, which approaches were tried and abandoned -- dies with it (P03). The next agent working on a related problem starts from scratch. Multiply this across hundreds of agent sessions and the system is perpetually re-learning the same lessons.

The memory layer captures what agents learn and makes it available to future agents. Before an agent starts a task, the memory layer provides relevant context: previous approaches to similar problems, known pitfalls, established patterns, and accumulated institutional knowledge. The system remembers what it has done before, and each session builds on the last.

## Key Responsibilities
- **Episodic memory (indexing).** Index all agent sessions -- inputs, outputs, tool calls, decisions, outcomes -- via CASS. This is the raw experience layer: complete, unprocessed records of what happened.
- **Working memory (summarization).** Generate structured summaries from episodic records. Working memory captures the gist of sessions: what was attempted, what succeeded, what failed, and why. Diary entries and session summaries live here.
- **Procedural memory (distillation).** Distill actionable rules from working memory patterns. "When deploying to staging, always run migration checks first" is a procedural memory entry. These are the system's operational playbook.
- **Context provision.** Before agent task execution, query the memory layer for relevant knowledge. Assemble a context package with: similar past sessions, relevant procedural rules, known constraints for the problem domain.
- **Knowledge lifecycle management.** Track knowledge through maturity stages: candidate (newly observed), established (confirmed by multiple observations), proven (validated by successful outcomes), deprecated (superseded or invalidated). Prune deprecated knowledge.

## Interfaces

**Inputs:**
- Agent session records from event bus (S03)
- Explicit knowledge contributions from agents (via agent-mail or hooks)
- Feedback signals from verifier layer (S07) -- which approaches succeeded or failed

**Outputs:**
- Context packages provided to agent runner (S04) for prompt assembly
- Knowledge queries answered for any subsystem
- Memory update events emitted to event bus (S03)
- Knowledge maturity reports for improvement loop (S09)

## Design Principles
- **Deterministic curation.** The system learns, but the integration of learning into operational rules is deterministic. No LLM in the curation loop. New procedural rules are proposed by the analysis process, reviewed through governance, and applied through version-controlled configuration. This prevents feedback drift -- where an LLM gradually shifts operational rules in unpredictable directions.
- **Three layers, not one.** Raw session logs (episodic) are too noisy for agents. Distilled rules (procedural) are too compressed to understand context. Working memory bridges the gap -- structured enough to be useful, detailed enough to explain why. All three layers serve different needs.
- **Knowledge is versioned.** Memory entries are version-controlled like code. Changes to procedural memory are reviewable, diffable, and reversible. The system's knowledge base has a clear history.

## Candidate Implementations
- **CASS + CASS Memory.** Use CASS for session indexing and search (episodic layer), CASS Memory System for structured knowledge management (working and procedural layers). Advantage: purpose-built for this exact use case. Risk: dependency on external projects' stability and API evolution.
- **Git-backed knowledge store.** Store working and procedural memory as version-controlled files (YAML, Markdown) in a dedicated knowledge repository. CASS handles episodic search. Advantage: fully auditable, uses familiar tooling. Risk: search across file-based knowledge may be slow.
- **Hybrid with embeddings.** CASS for search, git for storage, vector embeddings for semantic retrieval of relevant knowledge. Advantage: enables "find knowledge similar to this problem" queries. Risk: embedding quality and maintenance overhead.

## Open Questions
1. How do we prevent the memory layer from becoming a junk drawer of accumulated noise? What are the concrete criteria for promoting knowledge from episodic to working to procedural?
2. How should conflicting knowledge entries be handled -- when two procedural rules contradict each other because they were derived from different contexts?
3. What is the right retrieval strategy for context provision? Keyword search, semantic similarity, recency, or a weighted combination? How do we measure retrieval quality?

## Cross-References
- [S09: Improvement Loop](improvement-loop.md) -- consumes memory data, produces new procedural entries
- [S04: Agent Runner](agent-runner.md) -- receives context packages for agent prompt assembly
- [S03: Event Bus](event-bus.md) -- source of session records for episodic indexing
- [S07: Verifier Layer](verifier-layer.md) -- success/failure signals inform knowledge maturity
- [P03: Knowledge Loss Across Sessions](../problems/knowledge-loss.md) -- the core problem this subsystem addresses
- [P07: Feedback Loop Absence](../problems/feedback-loops.md) -- memory enables feedback loops
- [CASS component](../components/external/cass.md) -- episodic memory search engine
