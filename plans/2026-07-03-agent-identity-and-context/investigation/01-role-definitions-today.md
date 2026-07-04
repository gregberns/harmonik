# 01 — Role Definitions Today (2026-07-03)

## TL;DR
A role's identity is authored in **three unrelated places** with no single source of truth:
1. **Skill** (`.claude/skills/<role>/SKILL.md`) — durable operating context, checked into the repo,
   embedded as an init asset. Exists for: `captain`, `crew-launch`, `watch`, `keeper`,
   `orchestrator-rules` (+ agent-comms, beads-cli, etc.). **No skill for admiral, logmine, flywheel.**
2. **Mission file** (`.harmonik/crew/missions/<name>.md`) — per-*instance* identity + current
   tasking (YAML front-matter + prose). Exists for every live crew/oversight instance.
3. **Registry JSON** (`.harmonik/crew/<name>.json`) — pure session-state, no identity.

Only **captain** and **crew** are first-class in Go code. Everyone else (admiral, watch-as-mission,
logmine) is "a crew with a mission."

## First-class-ness in code — the load-bearing finding
`harmonik start <role>` (`cmd/harmonik/start.go`) hardcodes exactly **two** roles:
- `start.go:81 case "captain":` → routes to `runCaptainSubcommand` (`captain.go`).
- `start.go:97 case "crew":` → routes to `runCrewSubcommand` (`crew.go`).
- `start.go:119` — unknown role error: *"roles are: captain, crew"*.

So "`harmonik start captain` made captain a first-class citizen" is literally true in code:
dedicated launcher `captain.go` — default `--name "captain"`, dedicated tmux session. `"captain"`
literal in **17 non-test Go files**; `"keeper"` in 21, `"crew"` in 9, `"watch"` in 4, `"logmine"` in
1, **`"admiral"` in 0** — admiral has zero code presence; purely a mission-file construct launched as a crew.

`flywheel` appears in ~50 Go files but is **NOT an agent role** — it's the daemon's autonomous-fill
loop (`eagerfill_em063.go`, `.flywheel` worktree markers). Not a role peer to captain/crew.

## Where each role's identity lives

| Role | Skill | Mission | Registry JSON | Playbook/other |
|------|-------|---------|---------------|----------------|
| captain | `captain/SKILL.md` (842L), `STARTUP.md` (890L), `SHUTDOWN.md` | — (runs from skill) | — (own tmux session) | `orchestrator-rules` loaded as contract at STARTUP 1.3 |
| crew (generic) | `crew-launch/SKILL.md` (609L) | per-instance: `leto.md`, `irulan.md`, `jamis.md`, `gurney.md` | `leto.json` | — |
| admiral | **none** | `admiral.md` (226L) | `admiral.json` | `admiral-playbook.md`, `admiral-initiatives.md` |
| watch | `watch/SKILL.md` (254L) | `watch.md` (117L) + `handoffs/HANDOFF-watch.md` | — | `resolve_watch_config.go` |
| keeper | `keeper/SKILL.md` (537L) | none (Go watcher `keeper_cmd.go`, not an LLM crew) | — | infra, not an LLM role |
| logmine | **none** | `logmine.md` (99L) | — | playbook `.kerf/works/logmine/pipeline.md` |
| orchestrator (abstract) | `orchestrator-rules/SKILL.md` (234L) | — | — | shared contract captain/impl-orch/solo all load |

Templates: `_TEMPLATE-planner.md` (oversight roles like admiral), `_TEMPLATE-runner.md` (bead-dispatching crews).

## Identity vs operating-instructions vs session-state — the split (maps to operator's 3 layers)

- **Identity/mission ("who am I / what's my job"):**
  - Skills: `captain/SKILL.md §0 "What you are (and are NOT)"` (56) + `§Identity & boot` (285);
    `crew-launch §"Who you are"` (31); `watch §"Identity and startup"` (34); `orchestrator-rules
    §Identity` (29) with the canonical **"admiral directs · captain drives · crew executes"** split.
  - Mission files: front-matter + opening "You are crew **X**, owning epic **Y**… Report to **captain**."
- **Operating-instructions (how):** the bulk of every skill — boot sequence, dispatch loop,
  escalation taxonomy, progress-feed cadence, keeper-restart re-hydration.
- **Session-state:** `.harmonik/crew/<name>.json` (no identity content) + the `## Current State`
  block inside mission files (mutable, captain-edited) + `HANDOFF-*.md`.

## Duplication across roles (significant)
Every mission file re-authors the same boilerplate rather than referencing a shared source:
- The 4-step **boot ritual** (comms join + confirm identity → `br update <epic> --assignee <name>`
  → post boot status → arm `comms recv --follow --json`) is copy-pasted in `irulan.md`, `jamis.md`,
  `leto.md`, `watch.md`, `logmine.md`, both `_TEMPLATE-*` files, AND codified in `crew-launch` steps 2–6.
- `captain_name: captain` + "Report to **captain**" hardcoded in every mission front-matter.
  **`captain_name` is NOT consumed by any Go code** (0 non-test hits) — advisory YAML for the LLM
  only, so "captain" is baked into every crew's identity by convention.
- Progress-feed cadence restated in each runner mission AND in `crew-launch §Progress feed`.
- Oversight-role disclaimer ("your `-q` queue is a formality; IGNORE crew-launch's dispatch loop")
  duplicated in `admiral.md`, planner template, near-verbatim.

## Name-as-first-class-concept summary
- **captain**: first-class in code (start.go switch, dedicated captain.go, default name, own tmux
  session) AND the escalation target named in every other role's mission/skill. Strongest coupling.
- **crew**: first-class as a *launch mechanism*; the specific crew *name* is just a `--name` string
  + mission filename.
- **watch / keeper**: keeper is a Go subsystem (first-class infra, not an LLM identity); watch is
  half-and-half — skill + Go config resolver, but launched as a crew with `watch.md`.
- **admiral / logmine**: NOT first-class anywhere in code; identity exists only as a mission file
  (+ playbook). Launched via `harmonik start crew --name admiral --mission …`.

**Clearest single illustration of scattered identity:** `captain_name` in mission front-matter is
dead to Go (0 consumers) — a pure LLM-facing convention.

## Key files
- `cmd/harmonik/start.go` (role switch 77–119), `captain.go` (299/316/318), `crew.go` (D3 92–115)
- `.claude/skills/{captain,crew-launch,watch,keeper,orchestrator-rules}/SKILL.md`
- `.harmonik/crew/missions/{admiral,watch,logmine,leto,irulan,jamis,gurney}.md`, `_TEMPLATE-{planner,runner}.md`
- `.harmonik/crew/{admiral,leto}.json`, `admiral-playbook.md`, `admiral-initiatives.md`
