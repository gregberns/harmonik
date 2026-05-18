# Design pointer — hooks

This file is a thin pointer. The hooks research thread did not produce a
standalone design doc; its findings were folded directly into the
consolidated master design.

- **Source research:** `../03-research/hooks/findings.md` (Claude Code hook
  events catalog: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse,
  Notification, Stop, SubagentStop, PreCompact, SessionEnd, plus the
  StopFailure error_type taxonomy).

- **Where it landed in the master design** (`claude-hook-bridge-design.md`):
  - §D3 (stdin payload schema per hook event)
  - §D11 (failure-mode classification — StopFailure error_type mapping)
  - §D12 (event-model amendment: zero new event types)
  - §D13 (handler/relay/daemon responsibility matrix)
