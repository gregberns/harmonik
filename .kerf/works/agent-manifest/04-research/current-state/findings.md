# Current-state findings (consolidation)

One-page load-bearing summary. Full probes:
`plans/2026-07-03-agent-identity-and-context/investigation/01-05*.md`.

## Where role identity lives today (01)
Authored in three unrelated places, no single source of truth: **skill**
(`.claude/skills/<role>/SKILL.md`), **mission file** (`.harmonik/crew/missions/<name>.md`), **registry
JSON** (`.harmonik/crew/<name>.json`, session-state only). Only `captain` and `crew` are first-class
in Go: `cmd/harmonik/start.go:81 case "captain"`, `:97 case "crew"`, `:119` unknown-role error names
just those two. `admiral` has **zero** code presence (mission-file construct launched as a crew).
`captain_name` in mission front-matter is consumed by **no** Go code (advisory YAML only). The 4-step
boot ritual is copy-pasted across every mission file. Canonical role split in
`orchestrator-rules §Identity`: **"admiral directs / captain drives / crew executes."**

## Launch-time injection mechanics (02)
- **Captain** (`captain.go runCaptainLaunchWithOps:315`): `ensureBootAssets` provisions
  `.claude/skills` + `AGENTS.md`; tmux launches a **bare `claude` REPL with NO seed paste**
  (`buildCaptainTmuxCmd:202`, sets `-e HARMONIK_AGENT`). The missing symmetric injection point.
- **Crew** (`crewstart.go HandleCrewStart:205`): spawns tmux then **pastes a seed** (`:502 ->
  pasteCrewMission:437/447`): "Please read <handoffPath> and run /session-resume..." env
  `HARMONIK_AGENT` + `HARMONIK_PROJECT` (`crewlaunchspec.go:124`); `--model` from `readMissionModel:556`.
- **Keeper restart** (`internal/keeper/cycle.go`): `identityBlock:456` appends a KEEPER-IDENTITY block
  **into `HANDOFF-<name>.md`** at `:1016`, then `/clear` + `/session-resume` re-read it. **This is the
  drift seam I1 targets** - identity re-pinned from the outgoing handoff, not an external master.

## Knowledge scoping / over-share (03)
`AGENTS.md` is 179 lines, self-describes as a thin ROUTER, but ~130 lines (50-179) are a legacy tail
every agent reads in full: **UBS block ~49L** (code-committers only) + **Beads Workflow ~58L**
(duplicates `beads-cli`/`harmonik-dispatch`). A real per-role load map already exists (AGENTS.md
13-26); captain/crew already skip AGENT_INDEX/STATUS/KB. Router design sound; fix = finish migration.

## Prior planning (04)
No prior plan framed the failure as time-based role-identity degradation; closest is
`admiral-framework/` - a **frame that self-reinstantiates verbatim through every keeper `/clear`**
(Part 0: "more principle-text will NOT work"). Pluggable / decouple-role-from-name is **greenfield**
(only a parked one-liner + Hermes SOUL.md adjacent art). `admiral-playbook.md` +
`admiral-initiatives.md` already instantiate the 3-layer split for one role - reference schema.

## Existing config surface to build on (05)
- `crew.Record` (`internal/crew/registry.go:38`) - file-backed per-crew record, atomic CRUD, **no
  role field**; Epic/Handle underused.
- Mission-handoff schema (`specs/crew-handoff-schema.md`, 6+1 fields) - daemon reads **only `model:`**.
- `AgentType` (`internal/core/agenttype.go:14`) = harness conformance class, orthogonal to role.
- `config.yaml harnesses.pi.*` (`resolve_pi_config.go`) = **existing precedent** for central-YAML
  per-agent-type injection, zero baked defaults, fail-loud. `watch.*_target` = per-role routing.
- **No** existing "inject the right data on launch for all agents" surface beyond `--model`, harness
  creds, and the paste-seeded mission. That gap is what this work fills.
