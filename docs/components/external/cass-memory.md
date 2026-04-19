---
title: CASS Memory System
status: explored
type: component
category: external
source: https://github.com/Dicklesworthstone/cass_memory_system
related: [docs/components/external/cass.md, docs/problems/knowledge-loss.md, docs/goals/learning-and-improvement-loops.md]
created: 2026-04-13
updated: 2026-04-13
---

# CASS Memory System

## Summary
CASS Memory is the institutional memory layer for agents. It transforms raw session data (indexed by CASS) into actionable knowledge through a three-layer cognitive architecture: episodic memory (raw sessions), working memory (diary entries and summaries), and procedural memory (playbook rules). The critical design choice is that the final curation stage is entirely deterministic -- no LLM in the loop -- preventing the feedback drift that plagues systems where AI evaluates its own output.

## Key Capabilities

### Three-Layer Cognitive Architecture
Episodic (raw sessions via CASS) feeds into working memory (diary entries, session summaries), which distills into procedural memory (playbook rules). Each layer has different persistence, update frequency, and consumption patterns. Agents primarily consume procedural memory; working memory is the intermediate processing layer.

### ACE Pipeline
Generate context from sessions, Reflect on patterns, Validate against evidence, Curate deterministically. This is the knowledge refinement pipeline. The pipeline stages are explicit and auditable -- you can trace any playbook rule back to the sessions that produced it.

### Confidence Scoring
Effective score combines base confidence with time decay (90-day half-life), asymmetric loss (harmful rules penalized 4x vs. helpful), and maturity multipliers. This means stale knowledge fades, dangerous knowledge is treated conservatively, and well-established knowledge is weighted appropriately.

### Maturity State Machine
Rules progress through states: candidate, established, proven, deprecated. Each transition has explicit criteria. This prevents untested rules from being treated with the same authority as battle-tested knowledge, and provides a clean mechanism for retiring knowledge that no longer applies.

### Anti-Pattern Inversion
Rules that fail in practice are automatically transformed into warnings. A rule like "always use approach X" that repeatedly leads to problems becomes "avoid approach X because..." without human intervention.

### Deterministic Curation
The Curator stage contains no LLM. It applies purely deterministic logic: confidence thresholds, maturity transitions, conflict resolution, deduplication. This is a deliberate architectural choice to prevent feedback loop drift -- the system that evaluates knowledge quality must not itself be probabilistic.

### Scoped Playbooks
Global playbooks (~/.cass-memory/) apply everywhere. Repo-level playbooks (.cass/) apply only to that project. Precedence rules handle conflicts. This means harmonik can have its own institutional knowledge that supplements (or overrides) general-purpose rules.

### Gap Analysis
Identifies underrepresented knowledge categories and directs acquisition effort. If the system has extensive knowledge about testing but little about deployment, gap analysis flags deployment as an area needing deliberate learning.

### Trauma Guard
Dangerous patterns learned from past incidents (data loss, security breaches, production outages) are flagged as high-consequence. These rules have elevated confidence and are resistant to decay, ensuring hard-won lessons are not forgotten.

## Integration Points for Harmonik

CASS Memory is the **learning and improvement layer**. Its role in the system:

- **Directly implements G04**: Learning and Improvement Loops (G04) is the goal CASS Memory exists to serve. Its three-layer architecture maps directly to the improvement loop subsystem.
- **Closes the feedback loop**: Agent sessions produce data (CASS indexes it), CASS Memory extracts knowledge, and that knowledge feeds back into agent prompts via playbook rules. This is the complete learning cycle.
- **Deterministic curation prevents drift**: The no-LLM curator stage is critical for system coherence (P04). Knowledge quality is maintained by deterministic rules, not by probabilistic judgment that could introduce subtle errors over time.
- **Scoped playbooks enable per-project learning**: Harmonik-specific knowledge lives in a harmonik-scoped playbook, separate from general knowledge. This means lessons learned building harmonik do not pollute unrelated projects.
- **Trauma guard addresses high-stakes operations**: For a system that orchestrates multiple agents across codebases, the consequences of repeating past mistakes are amplified. Trauma guard ensures catastrophic failure patterns are permanently remembered.
- **Gap analysis directs system improvement**: By identifying what the system does not know well, gap analysis can inform which types of agent work to prioritize for learning purposes.

## Limitations and Gaps

- **Depends entirely on CASS**: Without CASS providing indexed sessions, CASS Memory has no input. The quality of institutional knowledge is bounded by the quality of session indexing.
- **Latency in knowledge refinement**: The ACE pipeline is not real-time. There is a delay between a session occurring and its lessons appearing in playbook rules. Urgent lessons must be communicated through other channels.
- **Cold start problem**: A new project has no institutional knowledge. The system must accumulate sessions before playbook rules emerge, meaning early-stage projects operate without the memory advantage.
- **Rule conflicts across scopes**: When global and repo-level playbooks conflict, the precedence rules may not always produce the right answer for a specific context. Human review of conflicts may be needed.

## Open Questions

1. How should CASS Memory's playbook rules be delivered to agents at session start? Should there be a standard "context injection" protocol that all agents support?
2. Can the deterministic curator be extended to handle cross-project knowledge transfer -- promoting a repo-level rule to global scope when it proves universally applicable?
3. What is the right cadence for the ACE pipeline? Should it run after every session, on a schedule, or when triggered by specific events?
