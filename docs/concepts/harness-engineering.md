---
title: Harness Engineering
status: explored
type: concept
source: https://openai.com/index/harness-engineering
related: [zero-framework-cognition.md, symphony.md, alphago-system.md]
created: 2026-04-13
updated: 2026-04-13
---

# Harness Engineering

## What It Is
A methodology articulated by OpenAI describing how infrastructure improvements -- without changing the underlying model -- yield dramatic capability gains. The "harness" is everything surrounding the model: tools, constraints, conventions, CI, feedback loops.

## Key Concepts

### Harness-as-Product
The limiting factor in agentic systems is not model capability but infrastructure quality. Improving the harness (tooling, constraints, feedback) without changing the model produces outsized capability gains. The harness IS the product.

### Guides + Sensors
Two control types working together. **Feedforward controls** (guides): AGENTS.md, coding conventions, architectural specs, task templates -- prevent mistakes before they happen. **Feedback controls** (sensors): tests, linters, CI checks, review agents -- detect and correct mistakes after they occur. Both are required.

### Constrain to Empower
Strict architectural boundaries, layered dependency rules, and explicit conventions make agents MORE productive, not less. A smaller, well-defined solution space is easier to navigate than an unconstrained one. Freedom without structure produces chaos.

### Repository as Single Source of Truth
Everything agents need must be in-repo and machine-readable. No tribal knowledge, no external wikis, no "ask the team lead." If it's not in the repo, it doesn't exist for agents.

### Progressive Disclosure
Kill the 800-line AGENTS.md. Use a 100-line table of contents pointing to deeper sources. Agents (like humans) need layered information -- overview first, details on demand.

### Iterative Refinement
Every agent failure becomes a new encoded constraint. "Anytime an agent makes a mistake, engineer a solution so it never makes that mistake again." The harness improves monotonically -- failures are ratcheted into permanent fixes.

### Agent Specialization
Scoped tools and restricted access per role. An analyzer gets read-only access; a builder gets write access to specific directories; a reviewer gets diff tools and checklist templates. Specialization reduces error surface.

### Entropy Management
Background "garbage collection" agents continuously scan for pattern divergence, technical debt, and documentation staleness. This is non-optional -- without active entropy management, agent-written codebases degrade fast.

### Middleware Architecture
Composable processing layers: LocalContext (scope awareness), LoopDetection (prevent repetition), ReasoningSandwich (think-before-and-after), PreCompletionChecklist (verify before declaring done). Each layer is independent and stackable.

### Quality-Left Strategy
Fast deterministic checks run early (pre-commit hooks, linters, type checks). Expensive inferential checks run later (post-integration tests, AI-powered review). Fail fast on the cheap stuff.

### Filesystem-Backed Coordination
Git-tracked artifacts -- not conversation history -- are the coordination substrate. Agents read and write files; the filesystem is the shared state. Conversations are ephemeral; files are durable.

## Relevance to Harmonik

Harness engineering provides the **philosophy** underlying harmonik's design. Its contributions:

- **Guides + sensors**: The dual-control model maps directly to harmonik's feedforward (specs, templates, conventions) and feedback (verification agents, test gates) mechanisms.
- **Entropy management**: Harmonik must include entropy-fighting agents as first-class participants, not afterthoughts. Codebase health is not a side effect -- it requires dedicated agents.
- **Iterative refinement**: The ratchet pattern -- every failure becomes a permanent fix -- should be built into harmonik's meta-process. The system must get strictly better over time.
- **Constrain to empower**: Validates harmonik's approach of strict state machines and role-based permissions. Constraints are features, not limitations.
- **Progressive disclosure**: Harmonik's configuration and documentation should follow this pattern -- layered, navigable, not monolithic.
- **Quality-left**: Harmonik's verification pipeline should front-load cheap deterministic checks and reserve expensive AI-powered review for later stages.

The main insight: you do not need a better model to get dramatically better results. You need a better harness. This reframes harmonik's value proposition -- the system's intelligence comes from its structure, not just the models it orchestrates.
