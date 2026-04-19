# Harmonik Knowledge Base Methodology

## Purpose

This knowledge base captures, organizes, and evolves ideas for building **harmonik** -- a composable agentic orchestration system. It is designed to be written, maintained, and consumed entirely by agents, with human review at key decision points.

## Principles

1. **Discoverable** -- Every document is reachable from `AGENT_INDEX.md` within two hops. Every directory has an `INDEX.md`.
2. **Decomposed** -- Large ideas are broken into composable pieces. No document tries to capture everything.
3. **Traceable** -- Every concept links back to its source. Every subsystem links to the problems it solves and the goals it serves.
4. **Evolvable** -- Documents have status fields. Ideas move from `seed` → `explored` → `decomposed` → `designed` → `implementing`. Nothing is deleted; superseded docs are marked as such.
5. **Agent-native** -- Documents use consistent frontmatter, cross-references, and structured sections that agents can parse and navigate programmatically.

## Document Structure

Every document in this knowledge base follows this structure:

```markdown
---
title: <descriptive title>
status: seed | explored | decomposed | designed | implementing | archived
type: problem | goal | concept | component | subsystem | idea | log-entry
sources: [list of source references]
related: [list of related document paths]
created: YYYY-MM-DD
updated: YYYY-MM-DD
---

# Title

## Summary
<2-3 sentence overview>

## Body
<main content, structure varies by type>

## Open Questions
<unresolved items for future exploration>

## Cross-References
<links to related documents in this knowledge base>
```

## Directory Structure

```
harmonik/
├── AGENT_INDEX.md              # Master discovery file -- start here
├── docs/
│   ├── methodology/
│   │   └── METHODOLOGY.md      # This file -- how the KB works
│   ├── problems/
│   │   └── INDEX.md            # Problem space catalog
│   ├── goals/
│   │   └── INDEX.md            # Goals catalog
│   ├── concepts/
│   │   └── INDEX.md            # External concept digests
│   ├── components/
│   │   ├── INDEX.md            # Tool/component inventory
│   │   ├── internal/           # Our tools (kerf, adze)
│   │   └── external/           # Third-party tools (ntm, agent-mail, etc.)
│   ├── subsystems/
│   │   └── INDEX.md            # System decomposition
│   ├── ideas/
│   │   └── INDEX.md            # Brainstorm ideas
│   └── log/
│       └── INDEX.md            # Collaboration log
├── refs/
│   └── AlphaGo-modeled-orch-system.md  # Deep reference documents
└── docs/
    ├── 00_objective.md         # Original objective statement
    ├── 01_architecture.md      # Architecture principles
    ├── 01_components.md        # Component references
    └── 02_spec-management.md   # Spec management references
```

## Index Files

Every directory contains an `INDEX.md` that:
- Lists all documents in the directory with one-line summaries
- Groups documents by status
- Identifies gaps (areas where documents should exist but don't yet)
- Is updated whenever a document is added, removed, or changes status

## Status Lifecycle

```
seed → explored → decomposed → designed → implementing → archived
  │                                                          ↑
  └──────────── (superseded) ────────────────────────────────┘
```

- **seed**: Initial capture. May be rough, incomplete, or speculative.
- **explored**: Research done. Sources reviewed. Key ideas extracted.
- **decomposed**: Broken into sub-problems or sub-components. Links to children.
- **designed**: Concrete approach defined. Ready for implementation planning.
- **implementing**: Active development underway.
- **archived**: Completed, superseded, or no longer relevant. Kept for reference.

## Cross-Reference Conventions

- Use relative paths from the repo root: `docs/problems/01_human-attention-scarcity.md`
- Use anchor links for sections: `docs/concepts/kilroy.md#checkpoint-and-resume`
- Tag relationships: `solves: [P01, P03]` means "addresses problems 01 and 03"
- Tag component usage: `uses: [kerf, ntm]` means "leverages these components"

## How Agents Should Work With This Knowledge Base

See `AGENT_GUIDE.md` in this directory for operational instructions.

## Evolution Process

1. **Capture**: New ideas enter as `seed` documents in `ideas/` or the appropriate directory.
2. **Research**: Agents explore seeds, add context, link sources, advance to `explored`.
3. **Decompose**: Complex ideas get broken into sub-documents, advance to `decomposed`.
4. **Design**: Concrete subsystem designs emerge, advance to `designed`.
5. **Implement**: Designed subsystems enter implementation, tracked in `log/`.
6. **Retrospect**: Completed work triggers retrospective log entries that feed back into the knowledge base.
