# Agent Guide: Working With the Harmonik Knowledge Base

## Quick Start

1. Read `AGENT_INDEX.md` at the repo root to understand what exists.
2. Read `docs/methodology/METHODOLOGY.md` to understand document conventions.
3. Navigate to the relevant directory for your task.
4. Read the directory's `INDEX.md` to find existing work and gaps.

## When Creating Documents

- Always include the frontmatter header (see METHODOLOGY.md for template).
- Update the directory's `INDEX.md` to include your new document.
- Update `AGENT_INDEX.md` if you create a new directory or major section.
- Link to related documents using relative paths from repo root.
- Capture open questions -- don't try to resolve everything in one pass.

## When Updating Documents

- Update the `updated:` date in frontmatter.
- Update `status:` if the document has progressed.
- Add to the `## Open Questions` section rather than removing them (mark resolved ones as `[RESOLVED]`).
- Update cross-references if new related documents exist.

## When Researching

- Check `docs/concepts/` first -- the source may already be digested.
- Check `docs/components/` for tool capabilities before proposing new tools.
- Log significant findings or questions in `docs/log/`.

## Naming Conventions

- Files: `kebab-case.md` (e.g., `hook-driven-behavior.md`)
- Directories: `kebab-case/`
- Problem IDs: `P##` (e.g., P01, P02)
- Goal IDs: `G##` (e.g., G01, G02)
- Subsystem IDs: `S##` (e.g., S01, S02)
- Idea IDs: `I##` (e.g., I01, I02)

## Collaboration Protocol

- Before starting work, check `docs/log/INDEX.md` for active questions or blockers.
- When you encounter a question or challenge that needs human input, add a log entry.
- When you complete significant work, add a log entry summarizing what was done.
- Reference log entries by date and title: `log/2026-04-13-initial-brainstorm.md`

## Discovery Patterns

Agents should be able to answer these questions by reading index files:

- "What problems are we solving?" → `docs/problems/INDEX.md`
- "What are we trying to achieve?" → `docs/goals/INDEX.md`
- "What external ideas are relevant?" → `docs/concepts/INDEX.md`
- "What tools do we have?" → `docs/components/INDEX.md`
- "How does the system decompose?" → `docs/subsystems/INDEX.md`
- "What ideas are in flight?" → `docs/ideas/INDEX.md`
- "What happened recently?" → `docs/log/INDEX.md`
