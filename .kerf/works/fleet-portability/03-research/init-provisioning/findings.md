# Research — C1. Init provisioning completeness

> Pass 3 (`research`) of `fleet-portability`. Component C1 per `02-components.md`. Verifies the
> assessment against current code (5-agent assessment + an independent code-verification pass).
> All file:line citations confirmed against the live tree on 2026-06-13 unless flagged CORRECTED.

## Research questions

- RQ-C1.1 — What does `harmonik init` provision today, and does it provision any `.claude/skills/`?
- RQ-C1.2 — Where does `init` get its AGENTS template, and would it succeed from an installed binary on a foreign repo? What is the embed pattern to copy?
- RQ-C1.3 — What is the `--target-branch` fail-closed guard, and is its tracking bead (`hk-m8vy2`) real?
- RQ-C1.4 — Does the rendered AGENTS.md dangle references to files `init` never creates?
- RQ-C1.5 — Is `config.yaml` read for `max_concurrent` / `workflow_mode` / `target_branch`, and is there a flag > file > default precedence today?
- RQ-C1.6 — What does the spec corpus already say about init / `workflow_mode` resolution, so the design follows existing patterns?

## Findings

### F-C1.1 — `init` provisions `.harmonik/` + AGENTS.md but ZERO `.claude/skills/` (RQ-C1.1) — CONFIRMED

`cmd/harmonik/init_cmd.go` creates, on a full scan:

- `.harmonik/` + subdirs `events/`, `worktrees/`, `beads-intents/` (init_cmd.go:253-256)
- `.harmonik/config.yaml` (:316-330)
- `.harmonik/branching.yaml` (:347-361)
- `.harmonik/.gitignore` (:377-389)
- `AGENTS.md` at project root (:395-421)
- `CLAUDE.md -> AGENTS.md` symlink (:426-455)

There is **no** reference to `.claude/skills` anywhere in the file. The fleet's skills (captain / crew-launch / keeper / harmonik-dispatch / harmonik-lifecycle / agent-comms / beads-cli / major-issue-fanout) are NOT provisioned — they exist only in the harmonik repo's own `.claude/skills/` (project-local; **not** symlinked into `~/.claude/skills/`, confirmed by C2's verification). **This is the P0 gap:** a freshly-init'd foreign repo has no skills for the captain/crew/keeper to boot from.

### F-C1.2 — AGENTS template is read from a project-relative DISK path, not embedded (RQ-C1.2) — CONFIRMED (corrects "go:embed'd?" to NO)

`init_cmd.go:402` — `templatePath := filepath.Join(projectDir, "docs", "templates", "AGENTS.template.md")`, then `:404` — `data, err := os.ReadFile(templatePath)`. The error hint (:406-407) literally says "template should live at docs/templates/AGENTS.template.md in the harmonik repo." So **`init` fails when run from an installed binary on a repo lacking that template** — the path is relative to the *target project*, and only the harmonik source tree has it. This is the P1 portability blocker.

**Embed pattern to copy (confirmed):** `internal/daemon/standardgraph.go:26` is a real `//go:embed standard-bead.dot` directive embedding a single built-in asset into the binary — exactly the fallback-asset pattern the AGENTS template should adopt. The spec already relies on this embed for the canonical graph: `process-lifecycle.md:221` references "the build-embedded copy at `internal/daemon/standard-bead.dot`." So embedding init's template is **consistent with an existing, spec-sanctioned pattern**, not a new mechanism.

### F-C1.3 — `--target-branch` fail-closes to main against a PHANTOM bead (RQ-C1.3) — CONFIRMED phantom

`init_cmd.go:129-138` — fail-closed guard:

    // FAIL-CLOSED: --target-branch != "main" is not yet supported.
    // Remove this guard when hk-m8vy2 (merge-retarget) lands.
    if targetBranch != "main" {
        ... "Tracking bead: hk-m8vy2 (merge-retarget). Until it lands, --target-branch must be \"main\"." ...
        return 1
    }

`br show hk-m8vy2` -> **"Issue not found."** The ID is referenced **16x** across the codebase (init_cmd.go, setup-agent-prompt.md, harmonik-lifecycle SKILL.md) as a future marker, but the bead **does not exist**. So `init`'s portability story for work-projects (which need `--target-branch integration`, per AGENTS.md "Work-project deployment") is gated on a phantom. Note an apparent **tension with AGENTS.md**, which documents `--target-branch <branch>` as a live daemon flag and `harmonik promote` as landed — yet `init` still refuses any non-`main` target. The design must reconcile: either the merge-retarget capability already exists at the daemon (in which case `init`'s guard is stale and should be lifted) or it does not (in which case the bead must be created). **Decision input for design:** verify whether the daemon's `--target-branch integration` path actually works end-to-end; the guard may be vestigial.

### F-C1.4 — Rendered AGENTS.md dangles links to files init never creates (RQ-C1.4) — CONFIRMED

The template (`docs/templates/AGENTS.template.md`) references `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md` (:14), `docs/orchestrator-rules.md`, `docs/known-workarounds.md` (:16), `docs/kerf-beta-feedback.md`, `KERF-FEEDBACK.md` (:282). `init` creates **none** of them (F-C1.1). The rendered AGENTS.md on a fresh repo therefore contains live links to nonexistent files — the very first thing a booting captain reads ("Read AGENT_INDEX.md first") dead-ends. The design must either (a) ship minimal scaffolds for the load-bearing few (AGENT_INDEX / STATUS / TASKS), or (b) produce a foreign-repo template variant whose links are all init-created or marked optional.

### F-C1.5 — config.yaml is read ONLY for the `agents` block; the 3 ops keys are flag-or-default (RQ-C1.5) — CONFIRMED

`internal/daemon/projectconfig.go` parses **only** `schema_version` (:90) and the `agents` map -> per-agent-type `model`/`effort` (:91-97). It has **no struct fields** for `max_concurrent`, `workflow_mode`, or `target_branch`. Where those resolve today:

- `max_concurrent` — CLI flag `--max-concurrent` only (run.go:113 default 1); **no** config fallback.
- `workflow_mode` — CLI flag `--workflow-mode` (run.go:118, main.go:608); **not** read from `config.yaml` by the loader. (The config template at init_cmd.go:312 lists it as a *comment* only.) NOTE: the **spec** says the daemon's `workflow_mode` field IS read from project config at bootstrap (`process-lifecycle.md:221` — "MAY carry an optional `workflow_mode` field … read exactly once during PL-005 step 0") — so there is a **spec/code gap**: the spec mandates config-file resolution that `projectconfig.go` does not implement. The design should pin this: the loader must honor the spec'd `workflow_mode` config field.
- `target_branch` — CLI flag on `init`/`run`/`reconcile`; the daemon `Config.TargetBranch` receives it from CLI, not config. (Note `.harmonik/branching.yaml` IS read for branch config per AGENTS.md, so target-branch config DOES exist via branching.yaml — the gap is that `config.yaml`/the loader and `branching.yaml` are two separate surfaces; precedence between them and the flag needs documenting.)

**Net:** there is NO uniform flag > file > default precedence for these operational keys. The design wires the loader to read them (honoring the spec'd `workflow_mode` field) and documents precedence (flag > config.yaml/branching.yaml > default).

### F-C1.6 — Spec patterns to follow (RQ-C1.6)

- The **embedded-asset** pattern is spec-sanctioned (`process-lifecycle.md:221` cites the build-embedded `standard-bead.dot`; `standardgraph.go:26` is the `//go:embed`). The AGENTS template should follow it.
- The **`workflow_mode` resolution** is already spec'd with a precise contract (`process-lifecycle.md:221`: read once at PL-005 step 0, cached as `workflow_mode_default`, default `dot`, review-floor fallback `dot -> review-loop`, NEVER `single`). C1's config-loader work must conform to this, not invent a new resolution path.
- `process-lifecycle.md` owns the daemon's **per-project file surface** (:213) — init's provisioning of `.harmonik/*` should be cross-checked against that enumerated surface so init creates exactly what the daemon expects.

## Patterns to follow

- Use `//go:embed` (standardgraph.go:26 pattern) for any asset init hands out — template, skills, scripts — so init works from an installed binary.
- Conform the config-loader extension to the **already-spec'd** `workflow_mode` resolution (process-lifecycle.md:221); do not add a parallel resolution path.
- Keep init's `.harmonik/*` provisioning aligned with the daemon's spec'd file surface (process-lifecycle.md:213).

## Risks / conflicts (flag for design)

1. **hk-m8vy2 phantom vs AGENTS.md's "live" `--target-branch`.** Reconcile whether merge-retarget already works (guard is stale -> lift) or not (create the bead). Don't leave the portability path gated on a nonexistent bead.
2. **Spec/code gap on `workflow_mode` config-file reading.** The spec mandates it (process-lifecycle.md:221); `projectconfig.go` doesn't do it. C1 closes this — but it's a pre-existing conformance bug surfaced here, flag for the integration pass.
3. **Skills-provisioning size/coupling.** Provisioning 8 skills via embed bloats the binary and couples init to skill content. Design decides: embed vs fetch-from-a-pinned-source vs a `harmonik init --with-skills` opt-in. The skills are a **published contract** (crew names etc.) — changes to them are review-gated (see C2).
4. **Foreign-repo template variant.** A self-consistent AGENTS.md for a foreign repo is a *different* render than harmonik's own. Design must decide whether init ships two templates or one parameterized template with optional sections.
