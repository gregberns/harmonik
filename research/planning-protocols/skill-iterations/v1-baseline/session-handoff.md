---
name: session-handoff
description: Capture a structured session handoff so the next session can resume cleanly. Writes to ./HANDOFF.md by default, or to a path passed as an argument (useful when multiple parallel work streams share a repo). Use at session end, before a long break, when context is filling up, or whenever you need to durably checkpoint state for a future agent. Pairs with /session-resume which reads the produced document.
---

# Session Handoff

## Path resolution

If invoked with a path argument (e.g., `/session-handoff research/planning-protocols/HANDOFF.md`), write to that path. Otherwise, write to `./HANDOFF.md` in the current working directory. The path may be relative or absolute.

When a path argument is used, the parallel `/session-resume` invocation must use the same path — pairs are by-path, not global. State the resolved path in your final report so the user can copy it into their next `/session-resume` invocation.

## What to produce

Produce a durable session-handoff document at the resolved path (overwriting any existing file there — confirm with the user first if one exists and you didn't write it this session).

The next session — possibly with a fresh agent and no conversation context — should be able to read the same path and resume cleanly via the `/session-resume` skill.

## Document structure (mandatory)

Use this exact structure. Section ordering and headings matter — `/session-resume` parses this shape.

```markdown
<!-- PP-TRIAL:v1 -->
# Session Handoff

> Generated <YYYY-MM-DD> by /session-handoff. Read by /session-resume.

## Intent (forward-stated)

**Purpose.** One sentence on why this work exists / what it serves. The "why."

**Key Tasks.** 3-5 bullets on what the next session should do, in priority order. Each bullet a concrete action, not a category.

**End State.** One sentence on what "done" looks like — the observable condition that means this work has succeeded.

## Autonomy Scope (for next session)

**Decide autonomously:** [list of categories the next agent should resolve without asking — naming, file layout, formatting, small refactors, tool selection among standard tools, etc.]

**Ask first:** [list of categories the next agent must escalate before acting — architectural decisions, scope changes, anything user-visible, anything irreversible, anything with cross-system impact]

If you're unsure whether something falls in "decide" vs "ask," default to "ask."

## Decisions Made This Session

Numbered list. One line each. The fact, not the reasoning. (Reasoning lives in the conversation; this is the durable record.)

## Decisions Parked

Each parked item with a severity tag:
- **routine** — can wait; low cost to revisit later
- **watch** — could become blocking if other work moves forward
- **escalate** — load-bearing; the next session should not proceed past this without resolution

Format: `- [severity] item description (one line)`

## Open Questions

Numbered list. Phrased as actual questions. Should be answerable by the user or by directed research.

## Load-Bearing Tokens

Verbatim list of the domain-specific terms used in this session — names, paths, jargon, protocol names, system component names, anything the next agent must use *the same way* this one did. The /session-resume skill will mirror these verbatim at session start to catch vocabulary drift early.

Do not paraphrase. Do not substitute synonyms. List the tokens themselves.

## Out of Scope

Explicit list of "not being built / not being decided / not in this scope." Prevents scope creep at resume.

## What the Next Session Should Start With

One paragraph. The first concrete action, in plain language. Name the file path to begin reading or the question to begin answering. Should answer "what should I do first?" without further interpretation.
```

## Behavior rules

1. **Source of truth is the conversation, not assumptions.** Pull facts from what actually happened in this session. Don't invent decisions, don't backfill missing reasoning, don't paper over open questions to make the document look complete.

2. **If a section is genuinely empty, say so.** Write `*(none)*` rather than padding. An honest empty Decisions Parked is more useful than a manufactured one.

3. **Load-bearing tokens are tokens, not phrases.** A token is a name or term that has a specific meaning in this work — `kerf`, `commanders-intent`, `Step 4.5`, `harmonik`, `~/.claude/skills/`, `Layer 1 stack`. Not full sentences. Aim for 5-20 tokens; if the list is much longer, you're paraphrasing instead of extracting.

4. **Severity tagging is judgment.** Use `escalate` for items that would block or mislead next session. Use `watch` for items adjacent to active work. Use `routine` for genuinely deferred items. Don't tag everything `escalate` to look thorough.

5. **Do not summarize the document back to the user after writing.** Report only:
   - The resolved path that was written (or updated)
   - The single most likely-to-matter parked item or open question (one line)
   - Suggest the user run `/session-resume` at the start of the next session — and if a non-default path was used, include it in the suggestion (e.g., `/session-resume research/planning-protocols/HANDOFF.md`)

6. **Do not modify any other files.** This skill writes one file: the resolved handoff path.

7. **Trial flag.** Always include `<!-- PP-TRIAL:v1 -->` as the first line. This is greppable across the user's filesystem for retrospective analysis of what got handed off.

## Failure modes to avoid

- Padding empty sections with placeholder content.
- Listing every minor decision as a "Decision Made" — only the durable, future-relevant ones.
- Putting reasoning into the Decisions section (reasoning is conversation; this file is the contract).
- Paraphrasing tokens (defeats the readback purpose at resume).
- Writing a "What the Next Session Should Start With" that requires the next session to reread the entire handoff to interpret.
