# Dimension 2 — The codex CLI's non-interactive surface (external research, cited)

> Web-sourced; codex CLI **v0.137.0** (2026-06-04) latest at research time. Every claim cited.
> codex has two modes: the interactive **TUI** (`codex`, no subcommand) and the non-interactive
> **`codex exec`** subcommand. harmonik would use `exec`. Everything below is the `exec` path.

## 1. Non-interactive launch with a prompt seed

`codex exec` IS the headless mode (no `--quiet`/`--headless` flag):
```
codex exec "<prompt>"
codex exec -C /path/to/repo "<prompt>"      # -C/--cd sets working dir
cat prompt.txt | codex exec -                # stdin-as-prompt
npm test 2>&1 | codex exec "fix failures"    # prompt + piped stdin context
```
Sources: developers.openai.com/codex/noninteractive ; developers.openai.com/codex/cli/reference (v0.137.0).

## 2. Output format / structured JSON

`--json` (alias `--experimental-json`) prints **newline-delimited JSON events (JSONL)** to stdout.
Without it, stdout carries **only the final agent message**; progress always goes to **stderr**.
`--output-last-message <file>` / `-o` writes the final message to a file; `--output-schema <file>`
constrains the final response to a JSON Schema.

Event types: `thread.started`, `turn.started`, `turn.completed`, `turn.failed`,
`item.started|updated|completed`, `error`. Examples:
```json
{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}
{"type":"turn.completed","usage":{"input_tokens":24763,"output_tokens":122}}
{"type":"turn.failed","error":{"message":"model response stream ended unexpectedly"}}
```
`thread_id` appears **only** in `thread.started`. Items wrap `agent_message`, `command_execution`,
`file_change`, `mcp_tool_call`, etc. **Caveat:** `--json`+`--output-schema` are silently ignored
when MCP servers are active (github.com/openai/codex/issues/15451).
Sources: cli/reference ; takopi.dev exec-json-cheatsheet.

## 3. Session / resume — and the caller-minted-id question

codex calls its session a **thread**. Resume exists:
```
codex exec resume <SESSION_ID> "<followup>"
codex exec resume --last "<followup>"        # most recent in CWD; --all = any dir
```
**The caller CANNOT mint the session id up front — you can only CAPTURE the `thread_id` codex
generates** (from the `thread.started` JSONL event, `/status`, or `~/.codex/sessions/`). The
pre-seed-a-UUID feature request (claude `--session-id` analog) was raised repeatedly and
**closed 2026-05-29 as NOT_PLANNED** (#25111, prior #15271/#13242). There is also experimental
`--fork <SESSION_ID>`. Sources: issues #15271/#13242/#25111 ; cli/reference.

## 4. Approval / sandbox flags for unattended runs

Two orthogonal axes: `--sandbox {read-only|workspace-write|danger-full-access}` and
`--ask-for-approval/-a {untrusted|on-request|never}`.
- Edit files without prompts inside the project: `--sandbox workspace-write` (network still blocked
  by default).
- Fully unattended, no prompts: `--dangerously-bypass-approvals-and-sandbox` (alias `--yolo`) —
  disables BOTH; docs restrict to containers/CI/VMs.
- `--full-auto` is **deprecated** (was `-a on-request --sandbox workspace-write`); prefer
  `--sandbox workspace-write`.
- `--skip-git-repo-check` to run outside a git repo.

**For harmonik (already in an isolated git worktree):** `--sandbox workspace-write -a never` is the
natural unattended setting — file edits without prompts, no full-system access. `--yolo` is
unnecessary (and riskier) since the worktree is the sandbox boundary harmonik already wants.
Sources: agent-approvals-security ; cli/reference.

## 5. Completion signaling

In `exec` mode codex **runs to completion and EXITS** — it does NOT stay resident like the TUI:
- **Process exit code:** 0 success, non-zero on failure (primary done-signal).
- **Terminal JSON event** under `--json`: `turn.completed` (with token `usage`) on success, or
  `turn.failed`/`error` on failure.
- Without `--json`: final agent message flushed to stdout, then exit.
Sources: noninteractive ; takopi.dev cheatsheet.

## 6. Git commits

codex CAN make its own commits, but committing is a **model decision driven by prompt/AGENTS.md, not
a deterministic step**. When it commits it runs `git add -A` (footgun — has committed unrelated
files, #8548). Commit identity settable in `~/.codex/config.toml`
(`GIT_AUTHOR_NAME`/`GIT_AUTHOR_EMAIL`). **→ Do NOT rely on codex to commit; the harness should
either commit deterministically after exit, OR instruct + verify the `Refs:<bead>` trailer.**
Sources: christiantietze.de git-author post ; issue #8548.

## GAPS vs Claude Code (and harness compensation)

| Claude primitive | codex equivalent | Compensation in the codex adapter |
|---|---|---|
| **Caller-minted `--session-id <uuid>`** | **NONE** (NOT_PLANNED 2026-05-29) | Run `codex exec --json`, parse `thread.started.thread_id` from the **first** stdout line as the canonical id; map harmonik's own run UUID → captured `thread_id` in a side table; re-task via `codex exec resume <thread_id>`. |
| **Bracketed-paste re-task into a live TUI** | **NONE in exec** (`exec` exits per turn) | Each turn is a fresh `codex exec resume <thread_id> "<next prompt>"`. Multi-turn = a loop of resume calls keyed on the captured thread_id, NOT injection into a persistent process. |
| **`/quit` to end a live session** | **NONE NEEDED** — `exec` self-terminates on turn completion (exit 0) | No quit command; the daemon watches for `turn.completed`/exit code. Eliminates splash-dismiss, dual-Enter, `/quit`, and the post-quit force-kill entirely. |
| Deterministic commit boundary | Non-deterministic (model decides) | Adapter commits after exit, OR `--sandbox workspace-write` + explicit "do not commit" + commit itself with the `Refs:` trailer. |

**Net:** codex `exec` is a clean **one-shot, run-to-exit, JSONL-streaming** automation surface — a
*better* fit for a daemon-spawns-per-turn model than driving the claude TUI. The two things to give
up are (a) **pre-minting** the session id (capture-after-start instead) and (b) **live-injection
re-tasking** (resume-per-turn instead). Both shift bookkeeping from "I told it the id" to "I read
back the id" — mechanically straightforward.

### Design implication for the seam

Because `exec` exits per turn, the codex adapter's lifecycle is fundamentally **spawn-per-turn**,
not **spawn-once-and-inject** like claude. The shared spawn substrate still applies (tmux window or
even a plain subprocess — codex needs no TUI), but the harness contract must allow a harness to
implement `Retask` as "spawn a fresh `codex exec resume`" rather than "paste into the live pane."
This is the single most important shape difference the `Harness` interface must accommodate.
