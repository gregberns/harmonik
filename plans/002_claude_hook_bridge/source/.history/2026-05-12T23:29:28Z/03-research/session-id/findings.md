# Research — Claude --session-id flag and session lifecycle

## Source
- https://code.claude.com/docs/en/cli-reference (May 2026)
- https://code.claude.com/docs/en/headless

## --session-id

CONFIRMED: `--session-id <UUID>` accepts a caller-provided UUID at launch:
- Doc text: "Use a specific session ID for the conversation (must be a valid UUID)"
- Example: `claude --session-id "550e8400-e29b-41d4-a716-446655440000"`
- The UUID is the same value that appears as `session_id` in hook input JSON.
- The UUID is the same value returned in `--output-format json`'s `session_id` field and used by `--resume "$session_id"`.

This validates Design Constraint A3 (pre-generated session_id). Harmonik mints a UUIDv7 in the handler, passes it via --session-id, and that same UUID echoes back through every hook payload.

## --resume

`--resume`, `-r`: "Resume a specific session by ID or name, or show an interactive picker..."
Resumes the conversation history, system prompt, tool results, model selection etc. Combined with `--session-id`, the resumed session reuses the same UUID (i.e., the post-resume `session_id` on hook payloads is the resumed UUID, NOT a fresh one).

CAVEAT: `--fork-session` exists and "When resuming, create a new session ID instead of reusing the original (use with --resume or --continue)" — for harmonik's implementer-resume flow we want NORMAL resume (no --fork-session), so the claude_session_id is stable across iterations. Document explicitly in the spec.

## --continue

`--continue`/`-c`: loads the most recent conversation in the current directory. Not used by harmonik because:
- We do not want "most recent in cwd" semantics; we want "the specific session for this run". Always use --resume <claude_session_id>.

## Output formats and session_id in output

- `--output-format text` (default): plain text, no session_id surfaced.
- `--output-format json`: structured JSON includes `session_id` and metadata.
- `--output-format stream-json`: NDJSON, each line is a typed event. The first event is `system/init` carrying session metadata including session_id. Subsequent events all carry session_id.

## --include-hook-events (counter-evidence; see counter-evidence/findings.md)

`--include-hook-events`: "Include all hook lifecycle events in the output stream. Requires --output-format stream-json"
- This flag opens a SECOND architectural path for the bridge: instead of relying on .claude/settings.json + harmonik hook-relay subcommand, we could spawn `claude --output-format stream-json --include-hook-events` and parse the NDJSON stream directly from Claude's stdout.
- See counter-evidence/findings.md for full analysis.

## CLAUDE_CODE_SKIP_PROMPT_HISTORY / --no-session-persistence

`--no-session-persistence`: "Disable session persistence so sessions are not saved to disk and cannot be resumed. Print mode only."
- For the review-loop implementer-resume flow, harmonik MUST NOT use this flag (we depend on resume).
- The env var `CLAUDE_CODE_SKIP_PROMPT_HISTORY` does the same. The bridge MUST NOT set this env var.

## --bare

`--bare`: reduces startup time by skipping auto-discovery of hooks, skills, plugins, MCP servers, auto memory, CLAUDE.md. "Bare mode is useful for CI and scripts."

PROBLEM: --bare SKIPS HOOK AUTO-DISCOVERY. If the bridge uses .claude/settings.json hooks, --bare cannot be used. Harmonik will NOT pass --bare; settings hooks are required.

Alternative: with --bare we can pass `--settings <file>` to load specific settings explicitly. Documented as a fallback if hook auto-discovery breaks in some Claude version.

## Implications for the bridge

1. **Pre-generated session_id design works as expected.** Mint UUIDv7 in handler, pass via --session-id, observe echoed value in hook payloads.
2. **Implementer-resume is mechanical.** Handler stores claude_session_id per run; relaunches with --resume <claude_session_id> (no --fork-session). HC-006's claude_session_id LaunchSpec field is already specced; this confirms the mechanism.
3. **Reviewer launches always mint fresh UUIDs.** Already correct in HC-006 prose.
4. **Bridge cannot use --bare** (hooks would not load). Doc this constraint.
5. **--no-session-persistence MUST NOT be set.** Doc this.
