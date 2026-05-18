# Research — Claude Code Hook Events (Phase 1)

## Source
- https://code.claude.com/docs/en/hooks (canonical, May 2026)

## Hook event inventory (30 total; load-bearing subset)

**Session lifecycle:**
- `SessionStart` — fires on new session/resume/clear/compact. Payload: `source ∈ {startup, resume, clear, compact}`, `model`. Matchers: same set.
- `SessionEnd` — fires on session termination. Payload: `exit_reason ∈ {clear, resume, logout, prompt_input_exit, bypass_permissions_disabled, other}`. CANNOT block.
- `Setup` — fires only on `--init-only`/`--init`/`--maintenance`. Not used at MVH.

**Turn lifecycle:**
- `Stop` — fires when Claude finishes responding. Payload: `stop_reason ∈ {end_turn, tool_use}`. CAN block (exit 2 or decision=block). Harmonik will NOT block — exit 0 from relay.
- `StopFailure` — fires on API error. Payload: `error_type ∈ {rate_limit, authentication_failed, oauth_org_not_allowed, billing_error, invalid_request, server_error, max_output_tokens, unknown}`, `error_message`. CANNOT block. Maps to `agent_rate_limited` (when `rate_limit`) and `agent_failed` (other categories).
- `UserPromptSubmit` — fires before Claude processes a user prompt. Payload: `prompt`. CAN block. Not strictly needed at MVH.

**Idle / notification (heartbeat synthesis):**
- `Notification` — fires on UI notifications. Payload: `notification_type ∈ {permission_prompt, idle_prompt, auth_success, elicitation_dialog, elicitation_complete, elicitation_response}`, `message`. CANNOT block. `idle_prompt` synthesizes `agent_heartbeat{phase=waiting_input}`.

**Context (informational):**
- `PreCompact` / `PostCompact` — no-op at MVH.

## Common input fields (all hooks)

```json
{
  "session_id": "abc123",
  "transcript_path": "/home/user/.claude/.../transcript.jsonl",
  "cwd": "/current/working/directory",
  "permission_mode": "default|plan|acceptEdits|auto|dontAsk|bypassPermissions",
  "hook_event_name": "EventName",
  "effort": { "level": "low|medium|high|xhigh|max" }
}
```

`session_id` here IS the UUID passed via `--session-id <uuid>` at launch (confirmed in /en/headless: capture session_id then `--resume "$session_id"`). Load-bearing for the pre-generated session_id design.

## Settings.json schema (canonical at .claude/settings.json)

```json
{
  "hooks": {
    "EventName": [
      {
        "matcher": "pattern",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/script",
            "args": ["arg1"],
            "timeout": 600,
            "shell": "bash"
          }
        ]
      }
    ]
  },
  "disableAllHooks": false
}
```

Hook handler types: command | http | mcp_tool | prompt | agent.

Path placeholders: ${CLAUDE_PROJECT_DIR}, ${CLAUDE_PLUGIN_ROOT}, ${CLAUDE_PLUGIN_DATA}. Also exported as env vars.

Exit codes: 0 = success/parse stdout JSON; 2 = blocking error; other = non-blocking error.

## Authority

Stop / PreCompact / UserPromptSubmit / PostToolUse / SubagentStop / ConfigChange / Elicitation CAN block (relevant: only Stop is on our path; we exit 0).
SessionEnd / SessionStart / StopFailure / Notification / FileChanged / CwdChanged / SubagentStart / Setup CANNOT block.

The bridge uses hooks OBSERVATIONALLY ONLY — never to alter Claude's behavior. Relay always exits 0.

## Risks / patterns

- **Stop fires when Claude declares the turn complete.** For reviewer phase, this is after Claude writes .harmonik/review.json and emits its final assistant message. For implementer, this is when Claude believes work is done. Map Stop → outcome_emitted with payload derived from HARMONIK_PHASE env + on-disk review.json.
- **SessionEnd cannot block, fires when Claude exits.** Map SessionEnd → agent_completed (when preceded by outcome_emitted) per HC-008a/HC-INV-006.
- **transcript_path** at ~/.claude/projects/<slug>/<session-uuid>.jsonl is the canonical Claude transcript. Maps cleanly to harmonik's HC-010 session_log_location emission. S08 (memory layer) consumes it read-only per WM-029.
- **Settings.json is read at session start** with a file-watcher for in-flight edits. Materializing at workspace creation per WM-016 ordering is correct.
- The exec form (`type=command` + `args`) avoids shell injection — preferred for relay invocation: `{"command":"harmonik","args":["hook-relay","Stop"]}`.
