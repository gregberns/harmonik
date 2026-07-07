# Agent-manifest rollout — 12h behavior-observation retro

Status: OPEN. Started 2026-07-04 ~16:00Z (deploy of daemon 81434151 / tag daemon-20260704-03,
agent-manifest T3-T12 live). Window closes ~2026-07-05 04:00Z.

## Why
The agent-manifest rollout gives each agent TYPE (captain/crew/admiral/watch) a `soul.md` identity +
`operating.md` instructions, re-read from the type folder on every boot/restart (I1 provenance).
This retro answers: **under the NEW instruction set, are agents performing well, or do the
instructions need tuning?** FOCUS: **captain + ONE fresh crew** — not the whole fleet.

## Observation subjects (must be MANIFEST-BOOTED to count)
Pre-deploy sessions (booted before 15:53Z) do NOT count — they ran the old boot path. Only agents
that booted/re-cleared AFTER the swap read from the type folders. Subjects:
- **One fresh crew** launched via `harmonik start crew <name>` post-deploy (captain to confirm name).
- **captain**, once it next keeper-/clears onto the manifest.

## Mechanism
- **Transcripts:** `~/.claude/projects/-Users-gb-github-harmonik/<session-id>.jsonl` (one JSON/line;
  schema in `plans/2026-06-25-transcript-retro-tool/PLAN.md §0.1`). The extractor
  `scripts/transcript-extract.py` is PLAN-ONLY / never built — observer agents read JSONL directly
  or build it first.
- **session-id → agent map:** `session_keeper_cycle_complete` events in `.harmonik/events/events.jsonl`
  carry `agent_name` + `new_session_id`.
- **Cadence:** admiral spawns 1-2 subagents every ~2-3h across the 12h window.

## Per-agent scoring (each subagent writes `captain.md` / `crew-<name>.md`)
1. Did identity re-pin from `soul.md` (NOT a prior handoff)? — the I1 guarantee.
2. Did the agent follow its `operating.md` (scope, dispatch discipline, review gate)?
3. Any drift / confusion / regression vs the old boot path?
4. Scoped `_skills` correct — no global skill autoload leaking in?
5. Proposed instruction edits (concrete soul.md/operating.md changes).

## Impact tracking
`IMPACT.md` rollup — before/after behavioral deltas + any instruction edits actually landed. This is
the artifact that proves whether the manifest helped or needs changing.
