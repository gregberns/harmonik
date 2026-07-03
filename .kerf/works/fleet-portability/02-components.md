# Fleet Portability — Decomposition (Components / Affected Spec Areas)

> Pass 2 (`decompose`) of `fleet-portability`. Maps the four in-scope goals from
> `01-problem-space.md` to four components. The decomposition is organized **by the surface
> that has to change**, because the gaps cluster cleanly onto four launch-layer surfaces
> (init/provisioning, path & identity, multi-tenant global state, multi-repo dispatch) and
> the Go *core* is explicitly not a surface here (it is already per-project).

## 1. Decomposition strategy

Goal → component mapping is near 1:1:

| Goal (from 01) | Component |
|---|---|
| G1 full-fleet bootstrap on any repo | **C1 — Init provisioning completeness** |
| G3 path & identity de-hardcoding + G2 (session-name half) | **C2 — Path & identity parameterization** |
| G2 multi-project coexistence (global-state half) | **C3 — Multi-tenant settings & global tooling** |
| G4 cross-repo gap adopted | **C4 — Multi-repo dispatch (adopt hk-3r3)** |

The one cross-component coupling to call out: **C2 and C3 both fix "multi-project
coexistence" (G2)** — C2 owns the *tmux session-name* collision surface and C3 owns the
*global-file* collision surface (settings.json, captain-tools, /tmp). They are independent
edits (different files) but must be designed against the **same** `tmuxStartHashDir` /
`ComputeProjectHash` per-project-hash convention so the namespacing scheme is uniform.
C1 *consumes* the C2 path convention (init must hand out skills that use the de-hardcoded
convention, not the old absolute path), so **C2's path convention should be designed before /
alongside C1's provisioning.**

The Go *core* (sock/pidfile/queue/events/comms/keeper-marker + `-default`/`-flywheel`
sessions) is the **baseline**, not a component: it is already per-project and only gets
*reused* (its hash helper) by C2.

## 2. Components

### C1 — Init provisioning completeness

- **Surfaces:** `cmd/harmonik/init_cmd.go`; the embedded/disk AGENTS template
  (`docs/templates/AGENTS.template.md`); `internal/daemon/projectconfig.go` (config-precedence
  extension); the embed pattern at `internal/daemon/standardgraph.go:26`.
- **Change summary:** Make `harmonik init` produce a *bootable, self-consistent* project from
  an installed binary on a foreign repo — provisioning the fleet's skills, embedding the
  AGENTS template, rendering no dangling links, fixing the phantom-bead guard, and wiring
  config precedence.
- **Requirements (what must be true after):**
  - `init` provisions the skills the fleet boots from (captain / crew-launch / keeper /
    harmonik-dispatch / harmonik-lifecycle / agent-comms / beads-cli / major-issue-fanout, or
    a defined subset) into `.claude/skills/` — today it provisions **zero** skills.
  - The AGENTS template is shipped *in the binary* (`//go:embed`), not read from
    `<projectDir>/docs/templates/AGENTS.template.md` — so `init` succeeds from an installed
    binary on a repo that has no such template. Today it `os.ReadFile`s a project-relative
    path (init_cmd.go:402) and fails outside the harmonik source tree.
  - The rendered AGENTS.md contains no dangling links: every file it references
    (AGENT_INDEX/STATUS/TASKS/docs/specs/kerf-feedback) is either created by `init`, marked
    optional/scaffold, or removed from the foreign-repo template variant.
  - The `--target-branch` fail-closed guard (init_cmd.go:129-138) references a **real**
    tracked bead, not the phantom `hk-m8vy2` (16 refs, `br show` = not found) — create the
    bead, or replace the guard's reference.
  - Operational config keys (`max_concurrent`, `workflow_mode`, `target_branch`) resolve via
    **flag > config.yaml/branching.yaml > default**. Today `config.yaml` is read only for the
    `agents` block (`projectconfig.go`); these three keys are flag-or-default only.
- **Dependencies:** Consumes **C2**'s de-hardcoded path convention (the skills `init` hands
  out must already be path-portable). Sequence after / alongside C2's convention decision.
- **SPEC vs code:** Mostly **code** (init provisioning, embed directive, config loader) plus a
  **spec** statement of the portable-init contract. Likely touches the project-lifecycle
  (PL-series) spec and any init/setup spec.

### C2 — Path & identity parameterization

- **Surfaces:** the 5 shared skill files containing the hardcoded path
  (`.claude/skills/{captain,harmonik-lifecycle,harmonik-dispatch,major-issue-fanout,keeper}`);
  launch-layer tmux session naming (`internal/daemon/tmuxsubstrate.go:983` crew names;
  `internal/lifecycle/tmux/subcommand.go:182` supervisor/keeper names); the reusable hash
  helper (`internal/lifecycle/tmux/subcommand.go:229` `tmuxStartHashDir` / `core.ProjectHash`
  / PL-006a).
- **Change summary:** Replace every hardcoded `/Users/gb/github/harmonik` and every
  non-project-qualified launch-layer session name with the same per-project convention the
  core already uses, so a captain on project B reads project-B paths and project-B sessions.
- **Requirements (what must be true after):**
  - No shipped skill, doc, script, or hook stanza contains a literal
    `/Users/gb/github/harmonik` (29 hits across 5 skills today: captain ×15,
    harmonik-lifecycle ×7, harmonik-dispatch ×3, major-issue-fanout ×3, keeper ×1). They
    resolve the project root from a single documented convention — `$HARMONIK_PROJECT` (env)
    and/or the repo root — explicitly *not* `~/.claude/skills/`. This **reverses** commit
    7b84264d's "fix," which made the path unambiguous by hardcoding it.
  - Launch-layer tmux session names are project-qualified by the per-project hash:
    crew `hk-crew-<name>` → `hk-<hash6>-crew-<name>` (`crewSessionName`); supervisor
    `hk-daemon-supervise` → `hk-<hash6>-daemon-supervise` (`SupervisorSessionName`);
    keeper/captain `hk-keeper-captain` / `hk-keeper-*` → project-qualified.
  - The qualification reuses the existing helper (`tmuxStartHashDir(projectDir)` →
    `sha256(EvalSymlinks(dir))[:6]`, PL-006a / `DefaultSessionName`), not a new scheme, and
    stays consistent with the window-name sentinel discipline (`windowname.go:63`, WM-002a).
  - The harmonik deployment itself continues to boot under the new convention (it becomes
    project N=1 of the portable path, not a special case).
- **Dependencies:** Provides the path convention **C1** consumes; provides the session-naming
  half of G2 that **C3** must align its global-state namespacing with (same hash). Design
  first.
- **SPEC vs code:** **Both.** Spec: extend the session-naming contract (PL/WM series) to cover
  crew/captain/keeper/supervise (today only `-default`/`-flywheel`) and state the
  `$HARMONIK_PROJECT` skill-resolution convention. Code: the session-name builders + the
  skill-file edits (the skills are a **published contract** — the crew-name change goes
  through the review gate).

### C3 — Multi-tenant settings & global tooling

- **Surfaces:** `cmd/harmonik/keeper_enable_doctor_cmd.go:126,203-216` (writes 3 hook stanzas
  to `~/.claude/settings.json`); `~/.claude/captain-tools/` scripts (`captain-launch.sh`,
  `crewlog.sh`, referenced by `keeper_cmd.go:286` + captain skill but unversioned);
  `internal/release/lastgood.go:28` (`/tmp/hk-last-good-binary`); `scripts/hk-keeper.sh:44`
  (`/tmp/hk-daemon.log` default); the supervise script (`/tmp/hk-daemon-supervise.sh`,
  hardcoded `PROJECT`).
- **Change summary:** Make the global Claude surfaces (settings, tooling) and the `/tmp`
  globals project-namespaced so two projects' keepers/supervisors/daemons coexist instead of
  overwriting each other.
- **Requirements (what must be true after):**
  - Two projects each running `harmonik keeper enable` leave **both** keepers functional:
    their hook stanzas coexist in `~/.claude/settings.json` instead of the second overwriting
    the first. Today the merge keys on script basename, so the second enable rewrites all 3
    stanzas to the second project's `HARMONIK_PROJECT` — silently breaking the first project's
    keeper. The fix lets N per-project HARMONIK_PROJECT hooks coexist.
  - The captain-tools scripts (`captain-launch.sh`, `crewlog.sh`) are **versioned/shipped**
    (in-repo and/or provisioned by `init`) rather than living only in the operator's
    `~/.claude/captain-tools/` — a new project gets them without out-of-band copying.
  - `/tmp` globals are project-qualified: `/tmp/hk-last-good-binary`, `/tmp/hk-daemon.log`
    default, and the supervise script's hardcoded `PROJECT` no longer collide when two
    projects run fleets on one machine.
- **Dependencies:** Aligns its per-project namespacing with **C2**'s hash convention (same
  `hash6`). Independent of C1 except that `init` (C1) is the natural place to provision the
  versioned captain-tools scripts.
- **SPEC vs code:** **Both.** Spec: a multi-tenancy invariant — "harmonik's contributions to
  shared global surfaces (settings.json, captain-tools, /tmp) MUST be project-namespaced so N
  fleets coexist." Code: the keeper-enable stanza-namespacing, the scripts' /tmp paths, and
  moving captain-tools into the repo/embed.

### C4 — Multi-repo dispatch (adopt hk-3r3)

- **Surfaces:** existing **open bug bead `hk-3r3`** ("daemon: cross-repo dispatch gap — daemon
  worktree can only land harmonik-repo changes; kerf-repo fixes must be applied out-of-band",
  P2, area `internal/daemon`); the worktree-base derivation
  (`internal/daemon/claudelaunchspec.go:149-157` `WorktreeRootPath(deps.projectDir, …)` →
  `.harmonik/worktrees/` of the supervised repo only).
- **Change summary:** Name the cross-repo dispatch limitation as a known fleet boundary and
  adopt the existing bead — do **not** re-file or design the multi-repo mechanism here.
- **Requirements (what must be true after):**
  - The portability spec explicitly states the daemon lands only changes to the repo it
    supervises (worktrees are rooted at `<projectDir>/.harmonik/worktrees/`; a bead touching
    another repo cannot satisfy the managed-worktree prefix check, and no per-bead repo
    override / multi-repo config exists — confirmed: no `repo_url`/`target_repo`/`repoPath`
    field on `BeadRecord`).
  - hk-3r3 is referenced as the tracked gap and the out-of-band path is documented until it
    lands.
- **Dependencies:** None — pure documentation/adoption. The *implementation* of cross-repo
  dispatch is out of scope (own work).
- **SPEC vs code:** **Spec/doc only** — a "known boundaries" statement in the portability spec
  pointing at hk-3r3. No code in this work.

## 3. Affected spec files

This is a spec-first project, so the components land as changes to the normative specs in
`specs/`. The decomposition above names *code* surfaces (where the gaps live); the *spec*
changes are:

| Spec file (or new) | Component | Change |
|---|---|---|
| project-lifecycle (PL-series) | C1, C2 | Extend the `init` contract (provisions skills, embeds template, self-consistent render, config precedence) and the per-project session-naming contract (today only `-default`/`-flywheel`; add crew/captain/keeper/supervise qualification). Confirm exact filename in research. |
| window/session naming (WM-series, `windowname.go`-backed) | C2 | State the project-qualified launch-layer session-name scheme and the `$HARMONIK_PROJECT` skill-resolution convention. |
| multi-tenancy invariant (new section, likely in PL or a new portability spec) | C3 | "harmonik's contributions to shared global surfaces MUST be project-namespaced; N fleets coexist." |
| known-boundaries / portability spec (new or appended) | C4 | State the cross-repo dispatch boundary, reference hk-3r3. |

> **Research-pass task:** confirm the exact spec filenames in `specs/` that own the
> project-lifecycle, session-naming, and init contracts (the code cites PL-006a / WM-002a
> anchors). The decompose pass names the *surfaces*; the research pass pins the *spec files*.

## 4. Dependency map

- **C2 first** — it sets the path convention C1 consumes and the hash convention C3 aligns to.
- **C1 after/with C2** — init hands out the path-portable skills C2 defines.
- **C3 alongside C2** — independent files, but same `hash6` namespacing scheme; C1 may
  provision C3's versioned scripts.
- **C4 independent** — doc-only adoption, no ordering constraint.

## 5. Goal → area traceability

- G1 (full-fleet bootstrap) → C1 (+ C2 path convention, C3 versioned tooling). ✓
- G2 (multi-project coexistence) → C2 (session names) + C3 (global state). ✓
- G3 (path de-hardcoding) → C2. ✓
- G4 (cross-repo gap adopted) → C4. ✓

No component lacks a goal; no goal lacks a component.
