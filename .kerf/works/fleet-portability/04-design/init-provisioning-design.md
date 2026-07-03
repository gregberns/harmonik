# C1 — Init Provisioning Completeness — Design

> Pass 4 (`design`) of `fleet-portability`, component **C1**. Synthesizes the single
> design proposal, reconciled against live code (all file:line confirmed on the tree
> 2026-06-13). Inputs: `01-problem-space.md` (G1, SC1/SC4/SC5), `02-components.md` §C1,
> `03-research/init-provisioning/findings.md`.

## Goal

`harmonik init`, run from an **installed binary** on a **foreign repo** (not the harmonik
source tree), exits 0 and produces a **bootable, self-consistent** project: fleet skills +
runtime dirs + embedded AGENTS template + self-consistent render + config precedence, with
the phantom `--target-branch` guard removed. Closes G1 (full-fleet bootstrap), SC1
(`init` from installed binary provisions skills + self-consistent AGENTS.md), SC4
(no dangling links, no phantom-bead guard), SC5 (flag > config > default precedence).

---

## Decisions

### Decision (a) — Provision the 8 fleet skills + runtime dirs + mission template via a directory `embed.FS`

**Embed the 8 fleet skills and a crew-mission template into the binary as a directory
`embed.FS`, then walk-and-write them on init.** This is the directory form of the
spec-sanctioned single-file `//go:embed` already used at `standardgraph.go:26`
(`//go:embed standard-bead.dot`) — `cmd/harmonik` does not import `embed` yet, so C1
introduces the first directory embed in that package.

Rejected alternatives:
- **Copy-from-disk** fails on a foreign repo (no harmonik source tree to copy from) — this
  is exactly the F-C1.2 failure the AGENTS template already suffers.
- **Symlink into `~/.claude/skills/`** reintroduces the global-vs-project ambiguity that
  commit 7b84264d fought and breaks multi-project coexistence (G2) — two projects'
  captains would read one shared global skill set.

Embed is the only option that survives "installed binary, foreign repo."

**Rationale on the 8-skill subset:** the fleet boots from captain / crew-launch / keeper /
harmonik-dispatch / harmonik-lifecycle / agent-comms / beads-cli / major-issue-fanout.
The 3 reviewer/scaffold skills (`agent-reviewer`, `agent-config-reviewer`,
`go-subsystem-add`) are **excluded** — they are harmonik-internal and not boot-load-bearing
for the captain/crew/keeper.

**Payload — CORRECTED to ~180KB (proposal said ~165KB).** Live `du -ck` of the 8 fleet
skills is **180KB**, dominated by `captain/` at 80KB (its STARTUP.md + SHUTDOWN.md). Still
negligible binary bloat; the decision stands, but the spec/integration note must cite 180KB.

### Decision (b) — `//go:embed` the AGENTS template (fixes init_cmd.go:402 foreign-repo failure)

`renderAgentsMD` (init_cmd.go:395-421) currently `os.ReadFile`s
`<projectDir>/docs/templates/AGENTS.template.md` and **fails on any repo but the harmonik
source tree** (F-C1.2 / init_cmd.go:404-408). Replace the disk read with a read from the
same `embed.FS`:

```go
data, err := initAssets.ReadFile("assets/AGENTS.template.md")
```

Drop the `templatePath` var, the disk `os.ReadFile`, and the "should live at
docs/templates/… in the harmonik repo" hint (init_cmd.go:402-408). Substitution of
`$PROJECT_DIR` / `$TARGET_BRANCH` (init_cmd.go:411-412) is unchanged — confirmed those are
the only two template variables. The in-repo `docs/templates/AGENTS.template.md` stays the
single source of truth; a `go:generate` step copies it into `cmd/harmonik/assets/`.

### Decision (c) — Self-consistent AGENTS render: minimal scaffolds + reference pruning (NOT a knowledge-base seed)

The embedded template is a **foreign-repo variant**, NOT harmonik's own 382-line AGENTS.md.
The current template dead-ends a booting captain: its first instruction is "Read
AGENT_INDEX.md first" (template:14) but `init` creates none of `AGENT_INDEX.md` /
`STATUS.md` / `TASKS.md` / `docs/orchestrator-rules.md` / `docs/known-workarounds.md` /
`docs/kerf-beta-feedback.md` / `KERF-FEEDBACK.md` / `docs/orchestration-protocol-v2.md` /
`docs/components/internal/kerf.md` (all confirmed live in the 26KB template at lines
14, 16, 69, 247, 282, 307, 314, 326).

Seeding a full knowledge base is an explicit **non-goal** (01-problem-space §Non-goals).
Resolution = minimal scaffolds for the load-bearing few + pruning the rest:

1. **Ship 3 minimal scaffolds** that `init` writes at project root via a new
   `provisionScaffolds` step: `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md` — each a ~5-line
   stub with no onward dangling links. These are the three the template's "Start here" block
   (template:14) hard-points at; scaffolds are cheaper than rewriting the boot contract and
   preserve the captain's boot ritual. They are **committed** (listed in no gitignore) and
   become the project's real index over time. Idempotent (skip-unless-`--force`).
2. **Prune the rest from the foreign-repo template:** delete/replace the
   `docs/orchestrator-rules.md` + `docs/known-workarounds.md` pointer (template:16), the
   `docs/orchestration-protocol-v2.md` link (template:69), the kerf-beta-feedback /
   `KERF-FEEDBACK.md` block (template:282), the `docs/components/internal/kerf.md` deep
   links (template:247, 326), and the `STATUS.md#decisions-locked-in` deep link (template:307)
   — each becomes a one-line "see your project's docs/ if present" note or is removed.
   The "Planning with kerf" section stays (kerf is a real external tool) but loses in-repo
   doc links.

Net: every link in the rendered AGENTS.md is init-created (the 3 scaffolds, `.claude/skills/*`),
operator-owned (`~/.claude/CLAUDE.md`), a live CLI (`harmonik`/`br`/`kerf`), or removed.

### Decision (d) — REMOVE the phantom `--target-branch` guard; do NOT create the bead (critical-risk resolution)

The guard at init_cmd.go:129-138 is **vestigial — delete it.** This is the critical risk
(RQ-C1.3 / research risk #1). Resolved **decisively by live code**: the merge-retarget
capability the guard waits on **already shipped**.

Confirmed evidence (live tree 2026-06-13):
- `br show hk-m8vy2` → **"Issue not found."** The ID is a phantom, referenced in 3 shipped
  files (init_cmd.go, `.claude/skills/harmonik-lifecycle/SKILL.md`, `docs/setup-agent-prompt.md`).
- `daemon.go:676-687` reads `branching.yaml lands_on` into `cfg.TargetBranch` with
  flag > file > default precedence (WM-005b, hk-zl4sl — landed `0f48f44a`/`0cb68b31`).
- `daemon.go:689-711` implements full protect-branch / forbid-default-main fail-closed
  enforcement at boot (hk-sul12).
- `branching.go:121-150` resolves `start_from`/`lands_on` to the configured `targetBranch`
  (replacing literal "main"); `resolveParentCommit` (:363-374) cuts worktrees from the
  configured target; `landTaskBranch` lands onto it.
- `harmonik promote` is landed (`fd9d4c44`); AGENTS.md §"Work-project deployment" documents
  `--target-branch integration` as a **live** flag.

So `hk-m8vy2` describes work that is **already done** — creating the bead files a no-op. The
guard actively **breaks** the documented work-project deployment path (init refuses
`--target-branch integration`). **SC4 ("references a real, created bead — not a phantom") is
met by REMOVAL**, because the tracked capability is real and enforced at the daemon's own
fail-closed boot guard (hk-sul12).

Changes:
- Delete init_cmd.go:129-138 (the `if targetBranch != "main"` fail-close).
- Scrub the stale guard references: package-doc step 2 (init_cmd.go:15-16), the "FAIL-CLOSED
  constraint" doc block (init_cmd.go:35-39), the `configYAMLContent` comment (init_cmd.go:306),
  and the usage text (init_cmd.go:537).
- `init` already passes `--target-branch` through to `branching.yaml lands_on`
  (init_cmd.go:353 via `writeBranchingYAML`); the daemon's hk-sul12 guard does the real
  fail-closed enforcement at boot. Add a one-line usage note: a non-default `--target-branch`
  requires the branch to exist and not be in `protect_branches`.
- Scrub the 2 other shipped `hk-m8vy2` refs (`docs/setup-agent-prompt.md`,
  `.claude/skills/harmonik-lifecycle/SKILL.md`).

### Decision (e) — Wire `max_concurrent` / `workflow_mode` / `target_branch`: flag > config.yaml/branching.yaml > default

`projectconfig.go` parses only `schema_version` + `agents` (projectconfig.go:89-92); the 3
ops keys are flag-or-default. **The init template ALREADY writes all three under a `daemon:`
block** (init_cmd.go:304-312) — but the loader ignores them, so a freshly-init'd project's
`config.yaml` is partly inert. The spec **mandates** config resolution for `workflow_mode`
(PL-004a, process-lifecycle.md:221 — "read exactly once during PL-005 step 0"); this is a
pre-existing spec/code conformance gap C1 closes.

Mechanism — extend the existing loader + the existing precedence sites; do not invent a new path:

1. **`projectconfig.go`** — add a `daemon` block to `rawProjectConfig` and a typed accessor
   on `ProjectConfig`:
   ```go
   type rawProjectConfig struct {
       SchemaVersion int                       `yaml:"schema_version"`
       Agents        map[string]rawAgentConfig `yaml:"agents"`
       Daemon        rawDaemonConfig           `yaml:"daemon"`
   }
   type rawDaemonConfig struct {
       MaxConcurrent int    `yaml:"max_concurrent"`
       WorkflowMode  string `yaml:"workflow_mode"`
       TargetBranch  string `yaml:"target_branch"`
   }
   ```
   Expose getters `MaxConcurrent() int`, `WorkflowMode() string` (zero/`""` = "not
   configured", matching the existing `LookupAgent` "empty = defer" contract at
   projectconfig.go:114-128). `target_branch` is already resolved via `branching.yaml lands_on`
   at daemon.go:681 — to avoid two competing surfaces, **`config.yaml target_branch` defers to
   `branching.yaml`** (the daemon's authoritative read stays the branching.go chain; the getter
   exists for symmetry/observability only). Document this in the loader's package doc.

2. **`workflow_mode` precedence** — the CLI flag defaults to `dot` (main.go:616), so "operator
   passed `--workflow-mode dot`" is indistinguishable from "operator omitted it" without a
   was-set check. Resolve in **main.go**: after `flag.Parse()`, if `--workflow-mode` was not
   explicitly set (tracked via `flag.Visit`), read `ProjectCfg.WorkflowMode()` and use it; else
   the flag wins. Conform to the PL-004a floor — validate the config value through
   `core.WorkflowMode(v).Valid()` (daemon.go:662 already does this for the resolved value) and
   never accept a config that would drive `single` where the floor requires `dot → review-loop`.

3. **`max_concurrent` precedence** — `--max-concurrent` flag defaults to 1 (main.go:597). Same
   `flag.Visit` was-set check: when omitted and `config.yaml daemon.max_concurrent > 0`, use the
   config value; else flag wins. Wire the resolved value at main.go:824 (`spawnCapFromEnv`) and
   main.go:831 (`MaxConcurrent:`).

**Load-order — main.go owns flag-vs-config resolution.** `LoadProjectConfig` is called at
daemon.go:734-740, *after* the workflow-mode validation at :651-664, so the daemon can't
consult config before validating. Cleanest fix that does NOT reorder daemon.Start's gates:
**have main.go call `LoadProjectConfig(projectDir)` itself** (it already has `projectDir`) to
resolve flag-vs-config *before* constructing `daemon.Config`, then pass the resolved scalars
into the Config struct (daemon.go:826-847). The daemon's existing `LoadProjectConfig` at
:734-740 stays for the `agents`/`ProjectCfg` field — it is idempotent (a second read of the
same file). This keeps `daemon.Config` the single resolved-values struct it already is.

**Precedence rule (documented in projectconfig.go package doc + spec draft):**
`explicit flag > config.yaml/branching.yaml > built-in default`. `target_branch` resolves
through branching.yaml (existing); `workflow_mode` + `max_concurrent` resolve through
config.yaml (new). Because init already writes all three keys (init_cmd.go:300-313), a freshly
init'd project is configured by its checked-in file with no per-launch flags — satisfies SC5.

---

## Mechanism (file:line summary)

| File | Change |
|---|---|
| `cmd/harmonik/initassets.go` (NEW) | `embed.FS` for `assets/skills`, `assets/AGENTS.template.md`, `assets/crew-mission.template.md`; `go:generate` directive + CI diff-check. Use `//go:embed all:assets/skills ...` (the `all:` prefix keeps dot/`_`-prefixed entries). |
| `cmd/harmonik/assets/` (NEW, GENERATED) | `go:generate` rsyncs the 8 fleet skills + `docs/templates/AGENTS.template.md` + a crew-mission template into here — kept in lockstep with `.claude/skills/` (same discipline standardgraph.go:8-9 documents). |
| `cmd/harmonik/init_cmd.go:129-138` | DELETE the phantom `--target-branch` guard (Decision d). |
| `cmd/harmonik/init_cmd.go:15-16,35-39,306,537` | Scrub `hk-m8vy2` / FAIL-CLOSED doc blocks; add the "branch must exist + not protected" usage note. |
| `cmd/harmonik/init_cmd.go:251-266` (`mkdirAll`) | Add runtime dirs `.harmonik/{comms,crew,keeper,queues}` (the fleet's launch layer expects them). |
| `cmd/harmonik/init_cmd.go:364-374` (`harmonikGitignoreContent`) | Add `crew/ keeper/ queues/` (`comms/` already present at :373). |
| `cmd/harmonik/init_cmd.go:184` (`runInit`, after Step 9) | Insert `provisionSkills`, `provisionScaffolds`, `provisionCrewMission` steps (each skip-unless-`--force`). |
| `cmd/harmonik/init_cmd.go:402-408` (`renderAgentsMD`) | Replace disk `os.ReadFile` with `initAssets.ReadFile("assets/AGENTS.template.md")` (Decision b). |
| `internal/daemon/projectconfig.go:89-98` | Add `daemon` block (`max_concurrent`/`workflow_mode`/`target_branch`) + getters (Decision e). |
| `cmd/harmonik/main.go:596-597,615-617,824,831` | `flag.Visit` was-set check; `LoadProjectConfig` pre-Config; resolve max_concurrent + workflow_mode from config when flag omitted. |
| `docs/setup-agent-prompt.md`, `.claude/skills/harmonik-lifecycle/SKILL.md` | Scrub the 2 remaining `hk-m8vy2` refs. |
| `docs/templates/AGENTS.template.md` | Author the foreign-repo variant: prune dangling in-repo links (lines 16, 69, 247, 282, 307, 326); keep the 3 scaffold-backed refs (line 14). |

**Provisioning steps follow the existing idempotency convention** (skip-unless-`--force`,
mirroring every step in `runInit`). `--force` overwrites embedded assets — so a binary upgrade
can refresh stale skills via `init --force`. `provisionSkills` writes only the 8 fleet skill
subdirs; it never deletes or touches sibling skill dirs a foreign repo may already have.

**Spec touch (integration pass):** PL-004a conformance (config-file `workflow_mode` read —
closes the spec/code gap research risk #2); a new init-contract requirement (provisions skills +
scaffolds + runtime dirs + embedded template; flag > config > default for the 3 ops keys);
note the removal of the merge-retarget guard as now-redundant-with-hk-sul12. Likely lands in
`process-lifecycle.md` (owns the init contract + the per-project file surface at :213).

---

## Risk resolution

**Critical risk (RQ-C1.3 / research risk #1) — "is the `--target-branch` guard stale, or is
the bead real?"** Getting this wrong either files a no-op bead or ships a guard that breaks the
documented integration-branch deployment. **RESOLVED decisively by live code:** the
merge-retarget capability is fully implemented and enforced
(daemon.go:676-711 branching + protect-branch resolution; branching.go:121-150 + :363-374
cut/land on the configured target; `harmonik promote` landed `fd9d4c44`). The guard is
vestigial → **remove it and scrub the 3 phantom refs; do not create `hk-m8vy2`.** SC4 is met by
removal because the gated capability already exists and fails closed at the daemon's hk-sul12
boot guard. (Verified: `br show hk-m8vy2` = not found; 3 shipped-file refs; hk-zl4sl/hk-sul12/
hk-pk3p1 all in `git log`.)

**Secondary risks:**
- **Embed staleness** — the embedded `cmd/harmonik/assets/` silently drifts from
  `.claude/skills/` without a guard. Mitigation: `go:generate` rsync + a CI diff-check that
  fails when the embedded copy differs from source. This is the one ongoing-maintenance cost
  and must be called out in the spec draft (same risk standardgraph.go:8-9 already documents
  for `standard-bead.dot`).
- **Spec/code gap on `workflow_mode` config reading** (research risk #2) — pre-existing; C1
  closes it but the integration pass must record it as a conformance fix, not net-new behavior.
- **Invalid `workflow_mode` in config** — fail-fast through the existing `Valid()` gate
  (daemon.go:662-663); operator must fix, consistent with the malformed-config "refuse to
  start" contract (projectconfig.go:135-136).
- **`max_concurrent: 0` / negative in config** — treat as "not configured" (defer to
  flag/default), mirroring `spawnCapFromEnv`'s `<= 0` guard (main.go:920).
- **`config.yaml` with `daemon` block but unknown sub-keys** — yaml.v3 silently ignores extras
  (forward-compat, matching the existing agents-block behavior).

---

## Open / verify-first items (for the captain)

1. **HARD SEQUENCING DEPENDENCY ON C2 — strengthened from the proposal.** The embedded
   `cmd/harmonik/assets/skills/` is a `go:generate` snapshot of `.claude/skills/`. Live grep
   shows **6 of the 8 fleet skills carry the literal `/Users/gb/github/harmonik`**:
   `captain/STARTUP.md`, `captain/SHUTDOWN.md`, `keeper/SKILL.md`, `harmonik-dispatch/SKILL.md`,
   `harmonik-lifecycle/SKILL.md`, `major-issue-fanout/SKILL.md`. **C2's skill de-hardcoding MUST
   land before the assets are regenerated** — otherwise init ships skills with the literal
   harmonik path baked in, violating SC3 (no hardcoded path) on every foreign repo. This is more
   than the proposal's one-skill concern; it touches 6 of 8 and is the gating cross-component
   ordering constraint. **Recommend the captain sequence C2 → regenerate assets → C1 land.**

2. **Foreign-repo template authoring is a content task, not just a code change.** Decision (c)
   requires hand-pruning the 26KB `docs/templates/AGENTS.template.md` to a self-consistent
   foreign-repo variant (6 link sites to prune, 3 to keep). This is review-gated content work
   — the captain should dispatch it as its own bead and route the pruned template through the
   review gate (it is the first thing every foreign-repo captain reads).

3. **Crew-mission template schema** — `provisionCrewMission` ships a `mission.template.md` whose
   schema is the C3-locked captain→crew handoff format. Confirm with the captain/C3 owner that
   the template matches the current locked schema before embedding, so init doesn't ship a stale
   mission shape. (Low risk; flag for the integration pass.)

4. **Payload figure correction** — the fleet-subset embed is **180KB** (live `du -ck`), not the
   proposal's ~165KB; `captain/` alone is 80KB. Use 180KB in the spec/integration note.
