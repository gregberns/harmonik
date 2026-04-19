---
title: Component Catalog
status: explored
type: index
related: [docs/01_architecture.md, docs/01_components.md, docs/subsystems/INDEX.md]
created: 2026-04-13
updated: 2026-04-13
---

# Component Catalog

## Overview
Harmonik is composed from discrete, independently-developed tools rather than a monolithic framework. Each component owns a single concern and communicates through well-defined integration points. This catalog tracks every component -- internal and external -- along with its role in the system.

Internal components are tools we build and maintain. External components are third-party tools we depend on or integrate with.

## Component Table

| Name | Type | Purpose | Source | Integration Status |
|------|------|---------|--------|--------------------|
| [kerf](internal/kerf.md) | internal | Spec-writing and planning for AI agents | ~/github/kerf | active |
| [adze](internal/adze.md) | internal | Declarative machine configuration | ~/github/machine-setup | active |
| [NTM](external/ntm.md) | external | Named Tmux Manager -- process orchestration | github.com/Dicklesworthstone/ntm | active |
| [Agent Mail](external/agent-mail.md) | external | MCP-based agent coordination and messaging | github.com/Dicklesworthstone/mcp_agent_mail_rust | active |
| [CASS](external/cass.md) | external | Coding Agent Session Search | github.com/Dicklesworthstone/coding_agent_session_search | active |
| [CASS Memory](external/cass-memory.md) | external | Institutional memory for agents | github.com/Dicklesworthstone/cass_memory_system | active |
| [Kilroy](external/kilroy.md) | external | Graph-based workflow orchestration | github.com/danshapiro/kilroy | evaluating |

## Layer Map

Components group into functional layers. Each layer has a clear responsibility boundary:

| Layer | Components | Responsibility |
|-------|------------|----------------|
| Planning | kerf | Structured specification, task decomposition, jig-driven workflows |
| Environment | adze | Machine configuration, toolchain consistency, drift detection |
| Process Management | NTM | Agent spawning, lifecycle monitoring, rate limit recovery |
| Coordination | Agent Mail | Messaging, file reservations, build slots, identity |
| Memory | CASS, CASS Memory | Session search, institutional knowledge, learning loops |
| Workflow Execution | Kilroy | Pipeline definition, checkpoint/resume, deterministic routing |

## Integration Principles

1. **Each component is a standalone binary.** No shared libraries, no runtime coupling. Integration is through CLI invocation, MCP tools, or file-system conventions.
2. **Components are replaceable.** The system depends on the capability, not the implementation. Any component can be swapped if a better tool emerges.
3. **Communication flows through Agent Mail.** Direct inter-component calls are avoided. Agent Mail provides the coordination substrate.
4. **State lives in Git.** Components that persist state do so in Git-backed stores. This makes the system auditable and recoverable.

## Documents
- **Internal:**
  - [kerf](internal/kerf.md) -- Spec-writing and planning CLI
  - [adze](internal/adze.md) -- Declarative machine configuration
- **External:**
  - [NTM](external/ntm.md) -- Named Tmux Manager
  - [Agent Mail](external/agent-mail.md) -- MCP agent coordination
  - [CASS](external/cass.md) -- Coding Agent Session Search
  - [CASS Memory](external/cass-memory.md) -- Institutional memory system
  - [Kilroy](external/kilroy.md) -- Graph-based workflow orchestration
