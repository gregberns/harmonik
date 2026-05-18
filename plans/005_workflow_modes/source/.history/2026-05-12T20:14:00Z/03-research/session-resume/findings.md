# Research — Claude Code Session Resume Mechanism

**Research question:** Can the daemon spawn Claude Code subprocess #1 (implementer), capture its session ID, spawn a separate Claude Code subprocess #2 (reviewer) in between, and then resume subprocess #1's session with the reviewer's feedback as a new user message — all programmatically, without a TTY?

**Bottom line:** Yes. Documented CLI features cover the entire pattern. CLI flags are the canonical path; the Anthropic Agent SDK is an alternative but not required.

## Mechanism

**Capture session ID from initial run:**
```
session_id=$(claude -p "<initial task>" --output-format json | jq -r '.session_id')
```

The session ID is also persisted to disk at `~/.claude/projects/<project>/<session-id>.jsonl` (full transcript).

**Resume with new user message:**
```
claude --resume "<session-id>" -p "<reviewer-feedback>"
```

Or via stdin:
```
echo "<reviewer-feedback>" | claude --resume "<session-id>" -p
```

`--resume` (short: `-r`) takes a specific session ID. `--continue` / `-c` resumes the most-recent session in the CWD. **For the daemon use case, use `--resume` explicitly** — `-c` is ambiguous when multiple sessions exist per project.

## Key findings, mapped to design

| Design assumption | Documented behavior |
|---|---|
| Daemon captures session ID programmatically | `--output-format json` returns `session_id` field — confirmed. |
| Daemon resumes session in headless mode | `claude --resume <id> -p "..."` works without TTY — confirmed. Headless sessions resume in headless mode. |
| Session context preserved across resumes | Transcript is appended (JSONL); full conversation history retained. Auto-compaction triggers only when context window fills — unlikely within a 3-iteration loop. |
| Reviewer (subprocess #2) is a separate session, doesn't interfere | Different `claude` invocation in same CWD without `-r`/`-c` flags creates a new session — no interference with implementer's session ID. |
| Implementer sees reviewer's file changes on resume | **Likely yes; not explicitly documented.** Claude sees the CWD's current file state at resume time. The reviewer's writes to the worktree are visible. Worth verifying with a minimal fixture before depending on it. |
| Agent SDK alternative | The Anthropic Agent SDK is equivalent to `-p` mode, not a replacement. For our subprocess-orchestration pattern, CLI flags are simpler and documented. |

## Implications for spec drafts

- The Run record needs to persist the implementer's `session_id` across iterations (Execution Model C2's `context` map covers this).
- Daemon-side launch logic must pipe stdout of subprocess #1 to capture `session_id`. This is a launcher-shape requirement that should land in handler-contract (C1) or as a brcli/handler concern.
- No new spec is required to describe session-resume mechanics; it's a daemon-internal mechanism. The spec only needs to assert that ralph mode preserves implementer context across iterations and that the implementer process is the same logical session even though it is a fresh OS subprocess each resume.

## Open verification item (carry into tasks pass)

Write a small experimental fixture (one shell script) that:
1. Starts a Claude session in CWD `/tmp/x`, asks it to remember a fact, captures session_id.
2. In a separate process, modifies a file in `/tmp/x`.
3. Resumes the session and asks it to read the file and recall the fact.

Confirm both: the recalled fact survives, and the file modification is visible. If either fails, the design needs adjustment (likely: reviewer's feedback gets passed inline as part of the resume message rather than relying on the implementer to re-read files from disk).

## Sources

- Claude Code CLI reference (`--resume`, `--continue`, `--output-format`)
- Claude Code sessions documentation
- Headless-mode docs (no-TTY operation under `-p`)
- "How Claude Code Works" (context window / compaction behavior)
