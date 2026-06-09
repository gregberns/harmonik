<!-- PP-TRIAL:v2 2026-06-09 main @947acc87 — no user blocker. Canonical SHARED handoff; per-agent briefs are HANDOFF-<name>.md (read YOURS). ⚠️ DAEMON PINNED --max-concurrent 1 (serial): a concurrent-spawn wedge (hk-jgxqc, P1) stalls 2+ real beads dispatched together ~16min — keep dispatch SERIAL and re-run `harmonik queue set-concurrency 1` after ANY daemon restart (reverts to -c6 on keeper revive). Details: HANDOFF-named-queues.md. -->

Read order (per CLAUDE.md): AGENT_INDEX.md → STATUS.md → TASKS.md. Cross-project rules: `~/.claude/CLAUDE.md`. Comms = `harmonik comms` bus — pass `--from <you>` on every send.

## Read your role file (per-thread briefs)
- **captain** — pure delegator; this session: read captain kerf work, draft delegation-discipline instructions, spin a crew agent to start a `codex-harness` kerf work (add OpenAI codex as a 2nd implementer harness; today Claude-Code-only) + try resetting crew context at 300k tokens → **`HANDOFF-captain.md`**
- **controlpoints** — Captain & Crew feature implementation → `HANDOFF-controlpoints.md`
- **flywheel** / **named-queues** → `HANDOFF-flywheel.md` / `HANDOFF-named-queues.md`

# State
Main `@947acc87`, clean tree, `0/0` origin. Daemon + comms bus live on the hk-bfvby binary, **PINNED `--max-concurrent 1`** (re-set after any restart — it reverts to -c6 on keeper revive). The daemon spawn-stall saga is fixed in 3 layers (hk-r1rup / hk-oihnf / hk-bfvby, all landed + validated) EXCEPT a residual concurrent-real-bead wedge — **hk-jgxqc (P1)**: keep dispatch SERIAL; goroutine-dump the live daemon to root-cause (see HANDOFF-named-queues.md). Disk + the `~/.claude.json` bloat that caused the wedge: resolved — no action.

# Glossary
- **captain** — comms identity of the delegator agent; also the kerf feature codename (Captain & Crew) that controlpoints implements.
- **crew agent** — a worker session captain spins up via `claude --remote-control "<name>" --session-id <uuid>`.
- **codex** — OpenAI's coding harness; the second implementer harness to add alongside Claude Code.
