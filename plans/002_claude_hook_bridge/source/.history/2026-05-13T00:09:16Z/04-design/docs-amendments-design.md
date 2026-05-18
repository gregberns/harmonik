# Change Design — docs/subsystems amendments

## docs/subsystems/agent-runner.md

### Current
Line 28 (the "Hook attachment" bullet under Key Responsibilities) says: *"Configure Claude Code hooks (or equivalent) for the agent's session, connecting agent lifecycle events to the hook system (S05)."*

### Target
Replace with: *"For the claude-code agent type, hook attachment is governed by `specs/claude-hook-bridge.md`, which defines the `.claude/settings.json` materialization at workspace creation, the `harmonik hook-relay` subcommand that translates Claude hook events into harmonik progress-stream messages on the daemon socket, and the pre-generated `claude_session_id` flow that lets the daemon route hook-derived events to the correct watcher. Other agent types re-implement the bridge surface per their own bridge specs (post-MVH)."*

## docs/subsystems/hook-system.md

### Current
S05 doc describes hooks as "the bridge between probabilistic agents and deterministic workflows" with implementation options listed (Claude Code hooks, custom hook framework, hybrid).

### Target
Add a "Realization at MVH" section that names `claude-hook-bridge.md` as the normative realization for claude-code; the other implementation options remain as post-MVH evolution paths.

## Rationale

These are informative-doc updates only; no normative change. They serve to keep the discovery index (AGENT_INDEX.md) reachable from the bridge spec.
