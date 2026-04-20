# Harmonik — Agent Instructions

## Start here

Read [AGENT_INDEX.md](AGENT_INDEX.md) first. It is the master map of the knowledge base and every document is reachable from there within two hops. Then read [STATUS.md](STATUS.md) for current project state and [TASKS.md](TASKS.md) for the active work list.

## Planning with kerf

This project uses [kerf](docs/components/internal/kerf.md) for structured planning. The project is **spec-first**: the spec describes how the system operates, and code is updated to match the spec. The `spec` jig is the default.

Before implementing non-trivial changes (new subsystems, refactors that cross subsystem boundaries, changes to cross-cutting contracts), create a kerf work:

    kerf new <codename>

This creates a work on the bench and shows the process to follow. The jig guides you through structured passes — problem space, decomposition, research, design, spec draft, integration, and tasks — with review sub-agents at each major pass.

### Key commands

    kerf new <codename>              Create a new work
    kerf show <codename>             See current state + jig instructions for next steps
    kerf status <codename>           Check current status
    kerf status <codename> <status>  Advance to next pass
    kerf shelve <codename>           Save progress when ending a session
    kerf resume <codename>           Pick up where you left off
    kerf square <codename>           Verify the work is complete
    kerf finalize <codename> --branch <name>  Package for implementation

### When to use kerf

- New subsystems, cross-cutting spec changes → `kerf new --jig spec`
- Non-trivial feature plans → `kerf new --jig plan`
- Bug investigations → `kerf new --jig bug`
- Trivial changes (typos, one-line fixes) → skip kerf

### Workflow

1. `kerf new <codename>` — read the output, it tells you exactly what to do
2. Follow each pass: write the artifacts, advance status
3. `kerf show <codename>` — if you lose context, this shows where you are
4. `kerf shelve` / `kerf resume` — for multi-session work
5. `kerf square` — verify everything is complete
6. `kerf finalize` — package into a git branch for implementation

Don't skip the planning process. Measure twice, cut once.

## Key conventions

- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live under `.kerf/{codename}/` after finalization — gitignored by default.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Ten architectural decisions** are locked in as of 2026-04-19. See [STATUS.md](STATUS.md#decisions-locked-in-2026-04-19). Reopening one requires strong new evidence.

## Don't

- Don't write code yet. Phase 0 (plan refinement → spec drafting) is active. Code begins after the bootstrap subset of tasks is identified.
- Don't reopen locked-in decisions without explicit user request.
- Don't add abstraction layers the user hasn't asked for.
- Don't skip the AGENT_INDEX → STATUS → TASKS reading order when picking up the project.
