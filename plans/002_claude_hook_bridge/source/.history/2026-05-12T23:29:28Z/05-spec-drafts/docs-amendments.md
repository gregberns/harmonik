# Doc amendments (informative; not normative specs)

## docs/subsystems/agent-runner.md

### Replace line 28

**Before:**

```
- **Hook attachment.** Configure Claude Code hooks (or equivalent) for the agent's session, connecting agent lifecycle events to the hook system (S05).
```

**After:**

```
- **Hook attachment.** For the `claude-code` agent type, hook attachment is governed by [specs/claude-hook-bridge.md](../../specs/claude-hook-bridge.md), which defines `.claude/settings.json` materialization at workspace creation, the `harmonik hook-relay` subcommand that translates Claude hook events into harmonik progress-stream messages on the daemon socket, and the pre-generated `claude_session_id` flow used for routing and `phase = implementer-resume` continuity. Other agent types re-realize the bridge surface per their own bridge specs (post-MVH).
```

## docs/subsystems/hook-system.md

### Add a "Realization at MVH" section (after "Implementation Strategy" or equivalent existing heading)

```
## Realization at MVH

For the `claude-code` agent type, the hook system is realized by [specs/claude-hook-bridge.md](../../specs/claude-hook-bridge.md). The bridge spec is the normative source for the `.claude/settings.json` shape, the relay subcommand contract, the hook-event-to-progress-message mapping, and the failure-mode classification.

The "custom hook framework" implementation option enumerated above is the post-MVH expansion path for non-Claude agent types; each future agent type (pi-code, etc.) gets its own bridge spec mirroring claude-hook-bridge.md's structure.
```

## AGENT_INDEX.md

Add a row to the spec inventory under "Specs (normative)" section listing `claude-hook-bridge` (status: draft → reviewed after Phase 3 review).
