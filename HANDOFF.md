<!-- PP-TRIAL:v2 2026-06-08 PM main @eb12eb6b — CLEAN working tree, no user blocker. You are `captain` on the comms bus (pass `--from captain`). Peer `controlpoints` is IMPLEMENTING the captain feature. -->

Read order (per CLAUDE.md): AGENT_INDEX.md → STATUS.md → TASKS.md. Cross-project rules: `~/.claude/CLAUDE.md`. **But start with step 1 below — that's your actual brief this session.**

ROLE: captain = **pure delegator**. You orchestrate and spin up crew; you do NOT implement, investigate, or read code inline (mirror the orchestrator HARD RULE in CLAUDE.md). Every substantive task goes to a crew agent or the daemon queue.

# Your plan this session (in order)

1. **Start by reading the captain kerf work** to understand the role and what controlpoints is building. Read `docs/plans/captain/` — `SESSION.md` first, then `01-problem-space.md`, `03-components.md`, `06-integration.md`; and `kerf show captain` (status ready/SQUARE, 15 beads `codename:captain`). It's the Captain & Crew design: captain (you) spins up crew agents and delegates execution; crew does the judgment-free work. **controlpoints (peer, was online) is implementing this very feature** — coordinate via `harmonik comms recv --from controlpoints --follow`; do NOT touch its `codename:captain` implementation beads.

2. **Draft your own delegation-discipline instruction set** — a short checklist you load each turn so you stay a delegator and never drift into implementing. Save it as a memory or a `docs/` note. (User: "maybe work on a set of instructions to make sure it's just delegating work out.")

3. **Spin up a new crew agent and delegate the codex-harness research.** harmonik today supports only **Claude Code** as the implementer harness when running tasks; we want to add **codex (OpenAI's harness)** as a second one. Crew mechanism = interactive `claude --remote-control "<name>" --session-id <caller-minted-uuid>` (bracketed-paste seed, `--resume` to re-task — the pinned mechanism in memory; the captain feature will automate this but isn't built yet, so use the manual path). Delegate to the crew: run `kerf new codex-harness` and do the **full research** — how harmonik launches/drives Claude today (tmux substrate, pasteinject, commit-detection, session-id), the codex CLI's equivalent surface, where the harness-abstraction seam belongs, auth/billing model, integration design. Crew produces the kerf work through its passes (problem-space → research → design → spec → tasks); it does NOT implement yet.

4. **(Bonus) Monitor that crew's context and reset it at ~300k tokens.** If you can watch the crew session's token usage and clear/reset at 300k, do it (session-keeper style — see memory `project_session_keeper_design`). If current tooling can't, leave it — the operator will handle it in the morning. State which you did in your next handoff.

# State
Main `@eb12eb6b`, clean tree. Verify build green before any daemon dispatch. Daemon + comms bus live. Disk: resolved earlier — ample free space, **no action, do not investigate or discuss it.**

# Translations glossary
- **captain** — two referents: (1) YOU, the comms-bus identity (`--from captain`); (2) the kerf WORK/feature codenamed `captain` (Captain & Crew) that controlpoints is implementing.
- **crew agent** — a worker session captain spins up via `claude --remote-control "<name>" --session-id <uuid>`; execution, not judgment.
- **codex** — OpenAI's coding harness/CLI; the second implementer harness we want harmonik to support alongside Claude Code.
- **kerf work** — a structured planning unit (`kerf new <codename>`); the planning surface for non-trivial new work.
- **controlpoints** — peer agent, implementing the captain feature.
