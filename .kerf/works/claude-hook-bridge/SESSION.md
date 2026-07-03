# Kerf Session — claude-hook-bridge

- **Codename:** claude-hook-bridge
- **Jig:** spec
- **Status:** ready (final pass — all reviewer passes complete)
- **Reviewer passes complete:** 5
- **Beads designed:** 43 (see `07-tasks.md`)

## Where the work is

This work is at the `ready` status — every pass from problem-space through tasks
has been executed and reviewed. The consolidated artifacts are:

- Problem space: `01-problem-space.md`
- Decomposition: `02-components.md`
- Research: `03-research/{hooks,session-id,settings-env,prior-art,counter-evidence}/findings.md`
- Design: `04-design/claude-hook-bridge-design.md` (master) + per-affected-spec amendment-design files
- Spec draft: `05-spec-drafts/claude-hook-bridge.md` (master) + per-affected-spec amendment files
- Integration: `06-integration.md`
- Tasks: `07-tasks.md` (43 beads)

## Note on file layout

Phase-4 (design) and Phase-5 (spec-draft) outputs are organized by **affected
spec**, not by research topic. The per-research-topic files in `04-design/` and
`05-spec-drafts/` are pointer/index stubs that route to the consolidated
master files — they exist only to satisfy `kerf square` topology checks.
