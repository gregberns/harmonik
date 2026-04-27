# Session-Type Discriminator

> How to mechanically classify Claude Code session transcripts from `~/.claude/projects/` into planning-dialog vs autonomous-dispatch vs orchestration vs scratch, using only event-level signals. Produced from sub-phase 1B of the planning-protocols research.

## Why this exists

The first-pass classification (Phase 1A) used overall tool profile (Write/Edit/Bash-heavy vs Read/Task-heavy). That correctly distinguishes planning-WORK from implementation-WORK but misclassifies many sessions because it operates at whole-session granularity. A session can contain a rich planning dialog at its start and an autonomous implementation run at its end; tool-profile sums both and picks whichever dominates by count, usually implementation.

The discriminator below operates on dialog-density signal instead and produces a much cleaner separation.

## The filter

For each `.jsonl` event, classify as one of:

| Filter | Criteria | Kept as |
|---|---|---|
| Real human text | `type == "user" AND isSidechain == false AND content is string AND content non-empty` | Human turn |
| Tool result | `type == "user" AND content is array` (blocks contain `tool_result`) | Filtered out |
| Sub-agent sidechain | `isSidechain == true` (any type) | Filtered out |
| Main-thread assistant | `type == "assistant" AND isSidechain == false` | Agent turn (collapse into turns between human turns) |
| Everything else | `file-history-snapshot`, `system`, `last-prompt`, `permission-mode`, `queue-operation`, `attachment` | Filtered out |

Key fields on Claude Code JSONL events:
- `type` — event kind (`user`, `assistant`, `system`, etc.)
- `isSidechain` — boolean; true for sub-agent activity, false for main thread, null for metadata events
- `message.content` — string (real text) or array (tool_result blocks or assistant content blocks)
- `message.content[].type` — `text`, `tool_use`, or `tool_result` (for assistant content blocks and tool_result bundles)

## Signal: `n_human_text_turns` (ht)

Count the number of events passing the "Real human text" filter. Across the 195-session corpus from four projects:

| ht range | count | nature |
|---|---|---|
| 1 | 106 | autonomous dispatch (single-directive + long agent run) |
| 2-3 | 55 | short, mostly stubs or single-correction dispatches |
| 4-10 | 22 | light dialog, typically context-dump or short debugging |
| 11-20 | 4 | moderate dialog |
| 21+ | 8 | heavy dialog (but includes controller-orchestrator sessions) |

## Secondary signal: opening-message shape

`ht` alone over-counts: controller-orchestrator sessions have many human turns that are not planning dialog. The opening message disambiguates:

- **"You are the controller agent"** → controller orchestration (b7eca5d2, 3fb3dc80, 69050eec, 7ff17283)
- **"study specs/..." + "never ask questions"** → autonomous dispatch template (~100 secure-dev sessions)
- **"# Session Recovery Context"** → session-recovery handoff (729dad16)
- **Substantive question or framing** (e.g., "This project is coming along nicely, but I have no idea if/how it works") → planning dialog
- **Single long context-dump** with few subsequent turns but long agent responses → context-dump variant (13493c8d at 5294/1903/3441 char turns)

## Recommended classifier

```
if ht == 1:
    return "autonomous dispatch"
elif opens_with("You are the controller agent"):
    return "controller orchestration"
elif opens_with("# Session Recovery"):
    return "session-recovery handoff"
elif ht >= 15 AND opens substantively:
    return "planning dialog"
elif ht in 4..14 AND human message avg length > ~1500 chars:
    return "context-dump"
else:
    return "unclassified / scratch"
```

## Caveats observed in practice

1. **Multi-message structured directives inflate ht.** In b7eca5d2, the controller-agent directive was split across 5+ consecutive user events (each a header or bullet section), giving a misleading ht=59. Consider collapsing adjacent human events into a single logical turn when they are back-to-back with no assistant event between them.

2. **`<local-command-stdout>`, `<bash-stdout>`, `<command-name>` tags inside user content** are Claude Code injection artifacts from slash commands and command captures, not human authorship. Filterable by detecting these tag markers in content.

3. **Zero sidechain events appeared in top-10 dialog-dense sessions.** Sub-agents dispatched via the `Task` tool may not always surface as `isSidechain: true` events in the main JSONL. Verify with a session known to use `Task` heavily before trusting the sidechain filter as complete.

4. **`ht` does not correlate with planning *depth*.** A 21-turn session with quick one-line exchanges is shallower than a 4-turn session with 3000-char human messages. For deeper classification, combine `ht` with per-turn character-length averages.

## How to regenerate

The script `scripts/extract_dialog.py` applies the extraction filter. For classification across a corpus, the shell script at `/tmp/reclass.sh` (reproducible from STATUS.md history) produces a per-session TSV with ht / sidechain_count / assistant_count / size / first_msg.

## Provenance

- Signal discovered 2026-04-23 during sub-phase 1A user-correction pass.
- Applied across 195 sessions from harmonik, kerf, machine-setup, secure-dev, and Developer-secure-dev projects.
- Verified manually on: 79a42399 (planning), fa557b32 (dispatch-disguised-as-planning), a9cff2d0 (template dispatch), b7eca5d2 (controller), 729dad16 (session-recovery).
- 10 sessions extracted using this filter; output at `phases/phase-1/corpus/<project>/<session-id>.md`.
