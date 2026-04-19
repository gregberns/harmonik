---
title: Zero Framework Cognition
status: explored
type: concept
source: https://steveyegge.com (Steve Yegge)
related: [harness-engineering.md, alphago-system.md, kilroy.md]
created: 2026-04-13
updated: 2026-04-13
---

# Zero Framework Cognition

## What It Is
An architectural principle articulated by Steve Yegge: the framework/orchestrator should contain ZERO cognition. All intelligence lives in the model; all structure lives in the orchestrator. The framework is plumbing, never brain.

## Key Concepts

### Core Thesis
Frameworks must not think. The moment an orchestrator starts making semantic judgments -- scoring outputs, ranking options, composing plans, analyzing meaning -- it becomes brittle, opinionated, and impossible to maintain. Cognition belongs to the model. Structure belongs to the framework.

### The Four-Step Flow
Every orchestrator operation should decompose into exactly four steps:
1. **Gather** raw context (I/O only -- no interpretation)
2. **Call AI** for decisions (the model thinks, the framework waits)
3. **Validate** structure (schema checks, type checks -- no semantic checks)
4. **Execute** mechanically (apply the decision, no second-guessing)

### What Orchestrators May Do
I/O operations, structural safety checks, policy enforcement (deterministic rules), mechanical transforms (format conversion, serialization), state management (read/write/transition), typed error handling (catch, classify, route).

### What Orchestrators Must NOT Do
Ranking, scoring, plan composition, semantic analysis, keyword-based routing, quality judgments, completion detection via heuristics, fallback decision trees. If it requires understanding meaning, it requires a model.

### Smart Endpoints, Dumb Pipes
Martin Fowler's microservices principle applied to AI systems. Models are smart endpoints -- they understand, reason, decide. The orchestrator is the dumb pipe -- it routes, connects, enforces structure. The pipe never inspects the payload semantically.

### Model Pyramid
Decompose work into cognitive tiers (high/medium/low complexity) and route to the smallest capable model. Critically: **AI itself labels the tiers**. The framework does not decide what is "hard" vs "easy" -- that would be cognition.

### Anti-Patterns
- Keyword matching for completion detection ("if output contains 'done'...")
- Heuristic fallback trees ("if confidence < 0.7, try simpler prompt...")
- Regex-based parsing of unstructured output
- Hardcoded scoring algorithms for output quality
- Framework-level "intelligence" that becomes the hardest code to maintain

## The Tension with Harmonik

ZFC provides a critical guardrail: resist the temptation to build "smart" routing/scoring logic in the orchestrator. But harmonik needs verifiable deterministic workflows -- state machines, transition rules, gate conditions. Are those "cognition"?

**Resolution**: Deterministic structure is **mechanism**, not cognition. A state machine that says "after code_review, go to verification" is a mechanical rule. A system that scores review quality to decide whether to proceed -- that is cognition. The distinction:

- **Mechanism** (allowed in framework): If state == X and event == Y, transition to Z. Deterministic, verifiable, no semantic judgment.
- **Cognition** (delegate to model): Is this code review good enough? Should we retry or escalate? What should happen next in this ambiguous situation?

Harmonik's architecture sits exactly at this boundary. State machines, transition tables, gate predicates, lifecycle hooks -- all mechanism. Output evaluation, plan generation, quality assessment, adaptive routing -- all cognition, all delegated to models.

## Relevance to Harmonik

- **Design guardrail**: Every time we're tempted to add "smart" logic to the orchestrator, ask: is this mechanism or cognition?
- **Model pyramid**: Harmonik's multi-model routing should use AI to classify task complexity, not hardcoded rules.
- **Four-step flow**: A useful template for every orchestrator operation.
- **Anti-pattern checklist**: A concrete list of things harmonik must never do in framework code.

The main risk: over-applying ZFC leads to an orchestrator that cannot enforce any workflow guarantees. The balance is: deterministic structure is fine, semantic judgment is not.
