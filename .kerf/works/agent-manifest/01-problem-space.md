# 01 — Problem space

## The problem
A role's identity and operating context are authored in **three unrelated places** with no single
source of truth (investigation 01): a skill (`.claude/skills/<role>/SKILL.md`), a per-instance
mission file (`.harmonik/crew/missions/<name>.md`), and a registry JSON (`.harmonik/crew/<name>.json`).
There is no per-agent configuration surface that injects "the right data" into each agent at launch.
Concretely, five pains:

1. **Role identity is scattered** — no durable "who am I" master. Only `captain` and `crew` are
   first-class in Go (`start.go` hardcodes exactly those two, `start.go:81/97/119`); `admiral` has
   **zero** code presence and exists purely as a mission file.
2. **Identity drifts across restarts (Xerox-of-a-Xerox).** Today the keeper re-pins identity **from
   the outgoing session's own HANDOFF** (`cycle.go:456 identityBlock`, appended to `HANDOFF-<name>.md`
   at `cycle.go:1016`). The setpoint is downstream of the thing that drifts, so each restart re-seeds
   from already-drifted state → role boundaries blur after ~6h (admiral takes on captain's work).
3. **AGENTS.md is over-shared.** ~130 of 179 lines are a legacy tail (UBS block ~49L, Beads Workflow
   ~58L) every agent reads in full though only code-committers need it (investigation 03). A queue
   worker doesn't need queue-add instructions; crew doesn't need stack-startup.
4. **No portable, declarative config.** The only launch-time injection is the crew mission paste-seed
   (`crewstart.go:447`) and `--model` (`crewstart.go:537`); captain gets **no seed paste at all**
   (investigation 02 §1). Nothing declares what each agent needs.
5. **Skills are claude-only.** Operating instructions == `.claude/skills` — a claude delivery
   mechanism, not a harness-agnostic contract. codex/pi can't consume them.

## Who benefits
- **Operators** standing up harmonik: add/rename roles (mayor, governor, a librarian) as a folder,
  no Go edit; scope each agent's knowledge so context stays lean.
- **The fleet**: identity stops eroding across restarts (structural anti-drift, not more prose).
- **Every agent**: gets exactly its declared skill+tool set, nothing leaked in.

## Success criteria
- A single git-tracked **type folder per agent type** is the immutable identity master.
- One command emits an agent's complete, correctly-ordered boot document from that folder — identity
  re-read from the master every restart, HANDOFF supplying only episodic state (kills the drift loop).
- `crew`, `admiral`, `captain`, `watch` all modeled as folders; captain stays first-class via
  `harmonik start captain` but its *content* comes from the registry.
- New type = new folder, no code change (open-ended vocabulary).
- Each agent's boot document carries only its declared skills (short-desc + pointer), not all of them.
- The command has **no side effects** — emits text only; identical output usable by claude/codex/pi.

## Out of scope (deferred)
The **publish seam** (fleet-state store, `admiral-initiatives.md` as a generated view, dashboard data)
is deferred to `plans/2026-07-03-fleet-state-and-dashboard-data/`. This work is identity + boot-context
injection only.
