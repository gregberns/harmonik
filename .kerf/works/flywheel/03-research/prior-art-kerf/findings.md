# Research — Prior art in the kerf project

> Component: `prior-art-kerf`. Source: research sub-agent over `/Users/gb/github/kerf`, 2026-05-27. Verdict: **SUBSTANTIVE prior art exists.** Greg's recollection was correct. kerf's `plans/005_work_coordination/source/` (a ~16-agent design exercise) wrote down deep thinking on most sub-topics — framed as "kerf coordinating autonomous agents," not "a forever-running agent runtime." Strong on context-mgmt-without-compaction, fixed-instructions+digest restart, handoff/continuity, role decomposition, aggressive queue-filling. Absent on prompt-caching and SDK/`claude -p`/headless/Pi.

## Most relevant files (paths relative to `/Users/gb/github/kerf`)

- **`plans/005_work_coordination/source/exploration/16_session_continuity.md`** — the "fixed instructions + small state digest" restart model in full. Core thesis: *never store what you can recompute; persist only the non-derivable digest (decisions / discoveries / warnings / unfinished work)*. Explicitly **rejects summarization/compaction** ("exactly how the telephone game starts," `:234`) and replaces it with relevance-filtering + tiered-detail + resolution-marking (`:195-231`). Key line: *"The right abstraction is not a document. It's a computed view + append-only structured log"* (`:308`).
- **`exploration/24_multi_agent_coordination.md`** — single/multi-agent role decomposition: PLANNING / ALLOCATE / EXECUTE / MERGE-TEST as **stateless loops over a filesystem "blackboard," polling not event-driven**. Load-bearing open question (`:305`): *is the dispatcher even an agent, or just `while true; do kerf next | harmonik dispatch; sleep 30; done`?* "Stateless between iterations — kerf is the memory" (`:18-22`).
- **`exploration/15_agent_protocols.md`** — what makes a protocol agent-executable across restarts: *"artifacts as state, not memory"*; context-window-exhaustion as a first-class constraint (`:62`); embed protocols in command output, not a file the agent must remember.
- **`exploration/11_factory_line.md`** + **`22_flow_dynamics.md`** — pull-based (kanban) dynamic priority recomputed on demand (`11:235-244`); the critical missing infra is **structured feedback ingestion / a high-priority feedback lane** (`22:466`).
- **`exploration/20_USER_RESPONSE.md`** — **Greg's own words**:
  - constant-loop ALLOCATE/MERGE agents "TBD" (`:15-21`);
  - unit of execution is the bead; a session = an execution session of "10-20 beads" (`:40-42`);
  - pull model / "high priority queue" via Japanese-manufacturing thinking (`:90-95`);
  - **load-bearing:** *"We DO NOT want any more than 30-40 lines of instructions. Agents are MUCH better when they have a small set of instructions, then can fetch what they need"* (`:120`) — the strongest direct statement of the fixed-instructions + fetch-on-demand model;
  - "kerf doesn't manage sessions" (`:99`); don't bake escalation policy in (`:129`).
- **Shipped surface:** `specs/sessions.md` (shelve/resume, stale-session detection, degraded-mode resume, SESSION.md template).
- **Backlog:** `plans/_backlog/010_concurrency/` (atomic claim files, `--session` / `kerf whoami`, "no lock daemon").
- **Corroborating pain:** `plans/014_process_management_reframe/_plan.md:80` — harmonik's agent observed **fanning out into new work instead of draining the queue** (the failure mode of an over-aggressive filler — relevant to G6 queue-pressure: aggressiveness must be *toward draining*, not *toward starting new things*).

## Clean negatives (genuinely new surface for flywheel)

- **No Claude Code SDK / `claude -p` / headless / Pi / respawn-on-context-fill design anywhere.** kerf is deliberately agent-runtime-agnostic and never launches/manages processes (`specs/sessions.md:7`, `plans/001_init/source/02-proposed-solution.md:28`); the only `claude --session-id` mention is incidental.
- **No prompt-caching / token-economics design** — only the adjacent "keep loaded context small/stable, recompute don't carry" principle.

## Implication for flywheel

kerf supplies the **state + priority model** (computed view + append-only log; fixed-instructions+fetch; stateless-loop-over-blackboard; kanban pull) but **always deferred the loop itself to harmonik**. The **loop mechanics, the context-recycle trigger, and the cache/token economics are the genuinely new surface** this investigation must produce. Greg's "30-40 lines of instructions, fetch the rest" rule (`20_USER_RESPONSE.md:120`) is a hard design constraint we should treat as locked input, not re-litigate.
