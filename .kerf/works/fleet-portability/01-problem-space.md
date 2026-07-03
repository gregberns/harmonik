# Fleet Portability — Problem Space

> Pass 1 (`problem-space`) of the `fleet-portability` spec work.

## Summary

Harmonik's **Go core is already cleanly per-project** — daemon socket, pidfile lock,
queues, events.jsonl, the comms bus, keeper markers, and the daemon's own
`harmonik-<hash>-default` / `-flywheel` tmux sessions all derive from a per-project
hash (`sha256(EvalSymlinks(projectDir))[:6]`), so two projects' daemons coexist on one
machine today with **zero socket/identity/lock collision**. The gap is **entirely above
the core**, in the *launch layer* that the captain, crew, keeper, and operator drive:
hardcoded `/Users/gb/github/harmonik` paths baked into shared skills, non-project-qualified
tmux session names at the agent-launch layer (`hk-crew-<name>`, `hk-daemon-supervise`,
`hk-keeper-captain`), single-tenant global Claude settings/tooling
(`~/.claude/settings.json`, `~/.claude/captain-tools/`, `/tmp` globals), and an
incomplete `harmonik init` that provisions none of the skills and reads its AGENTS
template from a path that only exists inside the harmonik repo.

This work makes the **full fleet** (daemon + supervisor + captain + crew + keeper)
deployable on **any** repo via one documented bootstrap, and makes **multiple projects'
fleets coexist on one machine** with no collision — by parameterizing the launch layer to
the same per-project hash the core already uses, embedding/provisioning the assets `init`
hands out, and namespacing the global Claude surfaces per project. The Go core is not
re-architected; it is already right. This is a launch-layer, init-completeness, and
docs/skills portability change.

## Goals (in scope)

1. **Full-fleet bootstrap on any repo.** A freshly `harmonik init`'d repo can stand up the
   complete fleet — daemon, supervisor, captain, ≥1 crew, and keeper — from a single
   documented command sequence, with no asset that exists only inside the harmonik repo and
   no hand-editing of an absolute path. `init` provisions (or the binary embeds) every asset
   the fleet needs to boot: the skills the captain/crew/keeper read, a self-consistent
   AGENTS.md whose links resolve, and the config the daemon reads.

2. **Multi-project coexistence on one machine.** Two (or N) projects each run a full fleet
   concurrently on the same machine with **zero collision** across every shared surface:
   tmux session names (crew/captain/keeper/supervise become project-qualified, joining the
   core's already-qualified `-default`/`-flywheel`), the global Claude settings file
   (per-project keeper hooks coexist instead of overwriting each other), global tooling
   (`~/.claude/captain-tools/`), and `/tmp` globals (last-good-binary, daemon log,
   supervise script). The core surfaces (sock/pidfile/queue/events/comms) already isolate —
   this work brings the *launch layer* up to the same isolation guarantee.

3. **Path & identity de-hardcoding.** No shared skill, doc, script, or hook contains a
   literal `/Users/gb/github/harmonik`. The launch layer resolves the project root and the
   project hash from a single convention (`$HARMONIK_PROJECT` / the per-project hash helper)
   so a captain booting on project B reads project-B paths. This replaces the
   7b84264d-era "fix" that hardcoded the absolute path *into* shared skills — itself the
   regression this work removes.

4. **Cross-repo dispatch gap documented & adopted.** The known limitation that the daemon's
   worktree can only land changes to the *supervised* repo (cross-repo fixes are out-of-band)
   is captured under the existing open bead **hk-3r3** rather than re-discovered. This work
   *adopts and scopes* that bead; designing the cross-repo dispatch mechanism itself may be
   deferred to its own work, but the portability spec must name it as a known fleet boundary.

## Non-goals (explicitly out of scope)

- **Re-architecting the per-project Go core.** The sock/pidfile/queue/events/comms/keeper-marker
  layer is already correctly per-project and must NOT be touched beyond reusing its existing
  hash helper. This is not a core rewrite.
- **A rewrite of the captain/crew/keeper subsystems.** Their behavior is unchanged; only the
  paths and identities they launch under become portable.
- **Building the cross-repo dispatch mechanism** (hk-3r3). This work *adopts and bounds* that
  gap; the actual multi-repo worktree/build/push routing is a separate design.
- **A hosted / multi-machine / multi-tenant control plane.** Coexistence is scoped to multiple
  projects on **one** machine, not multiple machines or a shared service.
- **Changing the daily-loop dispatch contract** (queue submit/append/subscribe, wave/stream
  semantics). Those are project-scoped already and unaffected.
- **Provisioning the full knowledge-base** (`AGENT_INDEX.md`, `STATUS.md`, `TASKS.md`,
  `docs/`, `specs/`, kerf) into every new project. `init` must produce a *self-consistent*
  AGENTS.md (no dangling links to files it didn't create) — but it is NOT in scope to seed an
  entire harmonik-grade knowledge base on a foreign repo.

## Constraints

- **Must not break the existing harmonik deployment.** The live harmonik fleet runs from
  these exact skills, session names, and global settings today. Any de-hardcoding /
  re-namespacing must keep the harmonik project itself bootable — ideally the harmonik
  deployment becomes "project N=1" of the same portable path, not a special case.
- **Preserve the per-project core contract.** Reuse `ComputeProjectHash` /
  `tmuxStartHashDir` (PL-006a, `sha256(EvalSymlinks(projectDir))[:6]`) for the new
  project-qualified launch-layer names — do NOT invent a second hashing scheme. Session-name
  changes must stay consistent with the window-name sentinel discipline (WM-002a).
- **Published crew-name / comms contracts changes go through the review gate.** Crew names
  (`hk-crew-<name>`) and comms identities are published contracts other agents and skills
  depend on. Renaming or re-namespacing them is a contract change requiring an independent
  reviewer per the project's review-gate rule.
- **Embedded vs disk assets.** Where `init` currently reads an asset from a path inside the
  harmonik repo (the AGENTS template at `docs/templates/AGENTS.template.md`), the portable
  fix must `//go:embed` it (the pattern at `internal/daemon/standardgraph.go:26`) or
  otherwise ship it in the binary — a path relative to `<projectDir>` cannot work from an
  installed binary on a foreign repo.
- **Global Claude surfaces are shared by the harness, not owned by harmonik.**
  `~/.claude/settings.json` and `~/.claude/captain-tools/` are written by Claude Code and by
  harmonik's enable/launch commands. The fix must namespace harmonik's contributions
  (keeper hook stanzas, captain-tools scripts) per project *within* those shared files/dirs,
  not assume harmonik owns them.
- **Phantom-bead guards must be reconciled.** `init` fail-closes `--target-branch` to `main`
  against bead `hk-m8vy2`, which does not exist (16 references, `br show` = not found). The
  guard's tracking reference must be made real (create the bead) or replaced, so the
  portability story isn't gated on a phantom.

## Success criteria (concrete, verifiable)

1. **One-command full-fleet boot on a fresh repo.** A repo with no prior harmonik state can
   run a single documented command sequence to stand up daemon + supervisor + captain + ≥1
   crew + keeper, with no manual absolute-path edits and no asset missing because it lived
   only in the harmonik repo. `harmonik init` on that repo exits 0 from an *installed binary*
   (not run from the harmonik source tree) and provisions the skills + a self-consistent
   AGENTS.md.

2. **Two projects, zero collision.** Two repos each running a full fleet on one machine show
   zero collision: `tmux ls` lists project-qualified crew/captain/keeper/supervise sessions
   for each project (distinct hashes); each keeper's hooks in `~/.claude/settings.json`
   point at the correct project and neither overwrites the other; the daemons already isolate
   on sock/pidfile/queue/events/comms (regression-confirm). Killing project A's fleet does not
   disturb project B's.

3. **No hardcoded harmonik path.** `grep -rn "/Users/gb/github/harmonik"` over the shipped
   skills, docs, scripts, and hook stanzas returns zero hits; the launch layer resolves the
   project root from a single documented convention (`$HARMONIK_PROJECT` / project hash).

4. **`init` produces a self-consistent project.** The AGENTS.md `init` renders contains no
   dangling links to files `init` did not create (or those links are explicitly marked
   optional/scaffold), and the `--target-branch` guard references a real, created bead (not a
   phantom).

5. **Config precedence is honored at the launch layer.** Operational knobs the fleet needs
   (`max_concurrent`, `workflow_mode`, `target_branch`) resolve via a documented
   flag > config.yaml > default precedence rather than flag-or-hardcoded-default only — so a
   new project can be configured by its checked-in `config.yaml`/`branching.yaml` without
   passing flags on every launch. (Today `config.yaml` is read only for the `agents` block;
   those three keys are flag-or-default only.)

6. **Cross-repo boundary is named.** The portability spec explicitly states the daemon lands
   only changes to its supervised repo, references hk-3r3 as the tracked gap, and documents
   the out-of-band path until that bead lands.

## Affected areas (preliminary)

- **`harmonik init`** (`cmd/harmonik/init_cmd.go`) — provisioning completeness: skills,
  embedded AGENTS template, self-consistent render, `--target-branch` guard/phantom bead,
  config-precedence wiring.
- **Shared skills** (`.claude/skills/captain`, `harmonik-lifecycle`, `harmonik-dispatch`,
  `major-issue-fanout`, `keeper`) — de-hardcode `/Users/gb/github/harmonik`.
- **Launch-layer tmux session naming** (`internal/daemon/tmuxsubstrate.go` crew names;
  `internal/lifecycle/tmux/subcommand.go` supervisor/keeper names) — project-qualify via the
  existing hash helper.
- **Keeper enable / global settings** (`cmd/harmonik/keeper_enable_doctor_cmd.go`) —
  per-project hook coexistence in `~/.claude/settings.json`.
- **Global tooling & `/tmp` globals** (`~/.claude/captain-tools/` scripts;
  `internal/release/lastgood.go`; `scripts/hk-keeper.sh`; the supervise script) — version /
  embed and project-qualify.
- **Project config loading** (`internal/daemon/projectconfig.go`) — extend beyond the
  `agents` block for the operational keys, with flag > file > default precedence.
- **Cross-repo dispatch** — adopt existing bead **hk-3r3** (`internal/daemon` worktree base
  path) as the named fleet boundary.
