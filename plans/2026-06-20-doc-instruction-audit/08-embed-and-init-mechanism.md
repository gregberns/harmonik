# 08 — Embed & Init Mechanism (ground truth)

How harmonik embeds/scaffolds agent-operating assets, and whether a clean
"ship-with-binary + lay-down-on-init" path exists for behavioral contracts
(skills + standing rules) and operational-state tier files.

---

## 1. Embedded skill mirrors — embed source of truth

**Embed directive:** `cmd/harmonik/init_skill_assets.go:29` — `//go:embed assets`
into `var initSkillAssets embed.FS`. The embedded tree is
`cmd/harmonik/assets/` containing:
- `assets/skills/<name>/<file>` — 8 fleet skills (captain[STARTUP/SKILL/SHUTDOWN], crew-launch, keeper, harmonik-dispatch, harmonik-lifecycle, agent-comms, beads-cli, major-issue-fanout)
- `assets/templates/AGENTS.template.md` — foreign-repo AGENTS variant
- `assets/scaffolds/{AGENT_INDEX,STATUS,TASKS}.md` — minimal stubs

**Authoritative source vs. copy:** The CANONICAL source is the repo-root working
copy `.claude/skills/<name>/<file>` (`init_skill_assets.go:12`). The
`cmd/harmonik/assets/skills/...` tree is a byte-identical EMBED MIRROR. The
AGENTS template mirror is NOT byte-identical (foreign-repo variant, pruned of
in-repo refs — `init_skill_assets.go:21-23`, no guard).

**Re-sync mechanism:** No make target, no script. It is a manual `cp` enforced
by a Go **sync-guard test**: `cmd/harmonik/init_skills_sync_test.go:26`
(`TestSkillAssetsEmbedInSync`) reads each embedded file and diffs it against
`../../.claude/skills/<skill>/<file>` (line 55) — fails the build if they
diverge. Re-sync = `cp .claude/skills/<s>/<f> cmd/harmonik/assets/skills/<s>/<f>`.
(That manual cp is exactly what commit e418223b did.)

**Captain-tools (separate embed):** `cmd/harmonik/init_captaintools_assets.go:19`
`//go:embed captain-tools/captain-launch.sh` → `var captainLaunchSh []byte`.
Canonical = `scripts/captain-tools/captain-launch.sh`; mirror =
`cmd/harmonik/captain-tools/captain-launch.sh`; guard =
`cmd/harmonik/captaintools_sync_test.go:27`. NOTE: only `captain-launch.sh` is
embedded — `scripts/captain-tools/keeper-restart-verified.sh` exists but is NOT
embedded and is NOT laid down by init.

---

## 2. `harmonik init` — what it scaffolds

Source: `cmd/harmonik/init_cmd.go`, `runInit` (line 72). Steps (each
skip-if-exists unless `--force`):

| # | Output | Source |
|---|--------|--------|
| mkdir | `.harmonik/{events,worktrees,beads-intents,comms,crew,keeper,queues}/` | hardcoded list `init_cmd.go:318` |
| br init | `.beads/` DB (`br init --prefix`) | external `br` |
| config | `.harmonik/config.yaml` | hardcoded `const configYAMLContent` :370 |
| branching | `.harmonik/branching.yaml` | hardcoded `const branchingYAMLContent` :446 |
| gitignore | `.harmonik/.gitignore` | hardcoded `const` :477 |
| skills | `.claude/skills/<8 skills>/` | embedded `assets/skills` via `provisionSkills` :543 |
| captain-tools | `~/.claude/captain-tools/captain-launch.sh` | embedded `captainLaunchSh` :607 |
| scaffolds | `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md` | embedded `assets/scaffolds` :646 |
| AGENTS | `AGENTS.md` (subst `$PROJECT_DIR`,`$TARGET_BRANCH`) | embedded template `renderAgentsMD` :514 |
| symlink | `CLAUDE.md → AGENTS.md` | :672 |
| supervise | `harmonik supervise start` (unless `--no-supervise`) | external |

**Does init write `.claude/skills/`?** YES (step `provisionSkills`).
**Does init write anything under `.harmonik/context/`?** NO. The `context/`
subdir is not even created by `mkdirAll`. No `project.yaml`, no
`captain-lanes.md`, no HANDOFF, no roadmap is laid down.

**No standing-rules contract is laid down beyond AGENTS.md.** The
`~/.claude/CLAUDE.md`-style cross-project rules are not part of init.

---

## 3. Templates

- **Root `templates/`** holds exactly one file: `AGENT_OPERATING_MANUAL.template.md`
  — NOT consumed by init (init renders only the embedded AGENTS template).
- **Embedded templates:** `cmd/harmonik/assets/templates/AGENTS.template.md`
  (the only one init renders).
- **`docs/templates/AGENTS.template.md`** is the in-repo parent that the
  foreign-repo embed variant was pruned from (`init_skill_assets.go:21`).
- **config.yaml / branching.yaml** are templated, but as **hardcoded Go string
  consts** (`init_cmd.go:370`, `:446`), not files.

**`.harmonik/context/` tier files (project.yaml, captain-lanes.md): NOT templated
anywhere.** There is no project.yaml schema/template in source — they are
hand-created ad-hoc per project. The live `.harmonik/context/project.yaml` and
`captain-lanes.md` exist only because they were authored by hand in THIS repo.

---

## 4. `.harmonik/context/` consumption

**No Go code reads these files.** A grep across `cmd/` + `internal/` (excluding
worktrees) for `project.yaml`, `captain-lanes`, `.harmonik/context`,
`ContextTier` returns ZERO non-test hits. The daemon never opens them.

The ONLY consumers are the **captain skill prose**:
- `.claude/skills/captain/STARTUP.md:64` — `cat .harmonik/context/project.yaml`
- `.claude/skills/captain/STARTUP.md:72` — `cat .harmonik/context/captain-lanes.md`
- `.claude/skills/captain/SKILL.md:213` — "tier-2 file `.harmonik/context/captain-lanes.md`"

So Report 03's finding is **CONFIRMED**: tier files are read only by the captain
LLM via skill instructions (a `cat`), never by deterministic Go.

---

## Does a clean ship-and-scaffold path exist?

**Skills + standing-rules contract (behavioral): YES, exists today.** The embed
FS + `provisionSkills` + sync-guard test is a working, idempotent ship-and-lay-
down path. To relocate a standing-rules contract into the binary, add a file
under `cmd/harmonik/assets/` and a provisioning step — the pattern is already
proven (AGENTS template does exactly this).

**Tier files (operational-state): NEEDS BUILDING.** `.harmonik/context/` has
NO embed source, NO template, NO init step, and NO `mkdir`. To make
`project.yaml` / `captain-lanes.md` ship-and-scaffold you must: (a) add
canonical template files under `cmd/harmonik/assets/` (e.g.
`assets/context/{project.yaml,captain-lanes.md}` with `$PROJECT_DIR`-style
placeholders), (b) add a `provisionContext` step in `runInit` after `mkdirAll`,
(c) add `.harmonik/context/` to the `mkdirAll` list (`init_cmd.go:318`), and
(d) optionally add a sync-guard test mirroring `init_skills_sync_test.go`.
Whether they should be hardcoded consts (like config.yaml) or embedded files
(like skills) is the one design choice — embedded files match the skills pattern
and keep them editable as repo-root canonicals.
