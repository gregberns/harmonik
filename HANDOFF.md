<!-- PP-TRIAL:v2 2026-06-08 PM main @024129ba — CLEAN working tree, no user blocker. Canonical SHARED handoff; per-agent briefs are HANDOFF-<name>.md (read YOURS). -->

Read order (per CLAUDE.md): AGENT_INDEX.md → STATUS.md → TASKS.md. Cross-project rules: `~/.claude/CLAUDE.md`. Comms = `harmonik comms` bus — pass `--from <you>` on every send.

## Read your role file (per-thread briefs)
- **captain** — pure delegator; this session: read captain kerf work, draft delegation-discipline instructions, spin a crew agent to start a `codex-harness` kerf work (add OpenAI codex as a 2nd implementer harness; today Claude-Code-only) + try resetting crew context at 300k tokens → **`HANDOFF-captain.md`**
- **controlpoints** — Captain & Crew feature implementation → `HANDOFF-controlpoints.md`
- **flywheel** / **named-queues** → `HANDOFF-flywheel.md` / `HANDOFF-named-queues.md`

# State
Main `@024129ba`, clean tree, `0/0` origin. Verify build green before any daemon dispatch. Daemon + comms bus live. Disk: resolved earlier this session — ample free space, **no action; do not investigate or discuss it.**

# Glossary
- **captain** — comms identity of the delegator agent; also the kerf feature codename (Captain & Crew) that controlpoints implements.
- **crew agent** — a worker session captain spins up via `claude --remote-control "<name>" --session-id <uuid>`.
- **codex** — OpenAI's coding harness; the second implementer harness to add alongside Claude Code.
