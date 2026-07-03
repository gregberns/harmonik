# C1 — Init Provisioning Completeness — spec-draft (target: `specs/process-lifecycle.md`)

> Pass 5 (`spec-draft`) of `fleet-portability`, component **C1**. Grounded in
> `04-design/init-provisioning-design.md` (all file:line confirmed on the live tree
> 2026-06-13).
> This draft is **target-spec-file-scoped**: it contains ONLY the NEW / CHANGED normative
> clauses that the integration pass (06) splices into `specs/process-lifecycle.md`. All
> unchanged spec content (every existing PL requirement, §1–§3, §5–§12) carries forward
> verbatim and is NOT re-pasted here. This is not a diff and not a full-file re-paste — it is
> the precise new normative prose, written in the existing PL heading / `Tags:` / `Axes:`
> style.
>
> **Requirement IDs minted by this draft** (both verified-free against the current file — a
> grep of `specs/process-lifecycle.md` on 2026-06-13 found no `PL-029` and no `PL-004b`; the
> current top-level max is PL-028 with the PL-028a..d suffix family):
>
> - **`PL-029` — Portable project initialization contract** (new top-level requirement; lands
>   in a new subsection **§4.11 Initialization contract** appended after §4.10, OR inside §4.10
>   Command surface — integration pass picks the home). `PL-029` is **verified-free** but the
>   final number is **VERIFY-free / integration-reconcile**: if C2's or another component's
>   draft also mints `PL-029` against this same file, integration MUST renumber one of them and
>   fix every back-reference.
> - **`PL-004b` — Operational config resolution (workflow_mode, max_concurrent, target_branch)**
>   (new requirement in **§4.1**, placed immediately after the existing **PL-004a** at line
>   ~219, before §4.2). `PL-004b` is **verified-free** and **VERIFY-free / integration-reconcile**
>   on the same collision-renumber caveat.
>
> Note for the changelog/integration pass: C2 also edits `specs/process-lifecycle.md` (its
> draft lives at `process-lifecycle-C2.md`). The two drafts touch DIFFERENT requirements
> (C1 = PL-029 + PL-004b; confirm C2's IDs at merge) — but both add to the same file, so the
> integration pass reconciles them into one updated `specs/process-lifecycle.md`.

---

## §4.1 — new requirement (insert immediately after PL-004a, before §4.2)

#### PL-004b — Operational config resolution (workflow_mode, max_concurrent, target_branch)

The daemon's three operational configuration scalars — `workflow_mode`, `max_concurrent`, and
`target_branch` — MUST each resolve through the precedence chain **explicit flag > checked-in
config file > built-in default**. "Explicit flag" means a flag the operator actually passed on
the daemon command line (detected by a was-set probe over the parsed flag set, e.g.
`flag.Visit`), NOT a flag left at its compiled-in default value; a flag whose value equals its
default but was not passed MUST be treated as absent for precedence purposes, so that an
operator who omits the flag yields to the config file rather than silently overriding it.

The config file surface for `workflow_mode` and `max_concurrent` is the per-project
`config.yaml` `daemon:` block (the same project-config surface PL-004a reads at PL-005 step 0).
The daemon MUST parse a `daemon:` mapping carrying optional `workflow_mode` (string),
`max_concurrent` (integer), and `target_branch` (string) keys. Unknown sibling keys under
`daemon:` MUST be tolerated (silently ignored) consistent with the forward-compatibility
posture of the existing `agents:` block. This requirement extends the existing project-config
loader, which until now parsed only `schema_version` and the `agents:` block and left these
three scalars flag-or-default; PL-004b makes the loader read the `daemon:` block and feed it
into the precedence chain above.

**`workflow_mode` resolution and floor.** When `--workflow-mode` was NOT explicitly passed and
the `config.yaml` `daemon.workflow_mode` value is present and non-empty, the daemon MUST use the
config value; otherwise the flag (or its default) wins. A config-supplied `workflow_mode` value
MUST be validated through the existing `core.WorkflowMode(v).Valid()` gate before use; an
invalid value MUST cause the daemon to refuse startup (fail-fast), consistent with the
malformed-config "refuse to start" contract for the project-config surface. The resolved
`workflow_mode` MUST honor the PL-004a review floor: the config surface MUST NOT be a path by
which the daemon-level default resolves to the no-review `single` shape. A `config.yaml`
`daemon.workflow_mode: single` is therefore NOT a valid daemon-level default — the floor of
PL-004a (`dot → review-loop`, NEVER `single` from the daemon-level default or built-in
fallback) governs the config-resolved value exactly as it governs the in-process
`workflow_mode_default`; the only path to `single` remains an explicit per-bead `workflow:single`
label audited via the `review_bypassed` event. The config read of `workflow_mode` is the
concrete realization of the PL-004a obligation that the field "MUST be read exactly once during
PL-005 step 0"; PL-004b implements that read. PL-004b MUST NOT contradict the PL-004a four-tier
precedence chain — it is the daemon-level (tier-3) config-file read PL-004a already mandates,
and resolution against higher-precedence per-project / per-task tiers remains owned by
[execution-model.md §4.3 EM-012a].

**`max_concurrent` resolution.** When `--max-concurrent` was NOT explicitly passed and the
`config.yaml` `daemon.max_concurrent` value is **greater than zero**, the daemon MUST use the
config value; otherwise the flag (or its default) wins. A `daemon.max_concurrent` value `≤ 0`
in config MUST be treated as "not configured" and MUST defer to the flag-or-default, mirroring
the existing `≤ 0` spawn-cap guard; a non-positive config value MUST NOT lower the effective
concurrency ceiling to a non-dispatching state.

**`target_branch` resolution.** `target_branch` MUST resolve through the existing
`branching.yaml` `lands_on` chain, NOT through a second competing surface. The authoritative
daemon read of the target branch is the `branching.yaml lands_on` → `TargetBranch` resolution
with `flag > file > default` precedence per [workflow-graph/workspace-model WM-005b] and the
hk-sul12 daemon boot guard (protect-branch / forbid-default-main fail-closed enforcement). A
`config.yaml` `daemon.target_branch` key, if present, is observability/symmetry only and MUST
defer to `branching.yaml`; the daemon MUST NOT let `config.yaml target_branch` override the
`branching.yaml`-resolved value. The fail-closed enforcement of a non-default target branch
(the branch must exist and MUST NOT be a member of `protect_branches`) is owned by the daemon's
hk-sul12 boot guard, not by this requirement.

**Resolution timing.** Flag-vs-config resolution for all three scalars MUST be performed before
the daemon's `Config` struct is constructed, so the daemon's existing validation gates operate
on already-resolved values; the daemon MUST NOT re-read these scalars after that point. The
project-config file MAY be read more than once during startup (the resolver read plus the
existing `agents:`-block read are both idempotent reads of the same file). Because `harmonik
init` writes all three keys into the project's checked-in config (PL-029), a freshly-initialized
project is fully configured by its checked-in `config.yaml` / `branching.yaml` with no
per-launch flags.

This requirement closes a pre-existing PL-004a spec/code conformance gap: PL-004a already
MANDATES the daemon-level `workflow_mode` config read "exactly once during PL-005 step 0", but
the project-config loader historically never read the field (it parsed only `schema_version` +
`agents:`), leaving the init-written `daemon:` block inert. PL-004b is the conformance fix, not
net-new behavior — the integration/changelog pass MUST record it as such.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

---

## §4.11 (NEW) Initialization contract — new requirement

> Integration-pass note: PL-029 introduces the FIRST PL requirement governing `harmonik init`
> (a grep of the current file found no existing init-contract requirement). It MAY land as a
> new subsection **§4.11 Initialization contract** appended after §4.10 (Command surface, daemon
> side), or be folded into §4.10. The integration pass chooses the section home and adds the
> §9 cross-reference entry.

#### PL-029 — Portable project initialization contract

`harmonik init`, when run from an **installed harmonik binary** against a **foreign repository**
(any git repository that is NOT the harmonik source tree), MUST exit `0` and MUST produce a
**bootable, self-consistent** harmonik project. "Bootable, self-consistent" means: the fleet
skills a captain / crew / keeper require are present, the runtime directories the launch layer
expects exist, the rendered `AGENTS.md` references only artifacts that exist after init, and
the checked-in config fully configures the daemon with no per-launch flags. `init` MUST NOT
depend on the presence of the harmonik source tree on disk; in particular it MUST NOT read any
provisioning input by path from `<projectDir>/docs/`, `<projectDir>/.claude/`, or any other
location that exists only in the harmonik source checkout.

**(a) Fleet-skill provisioning from a binary-embedded asset bundle.** `init` MUST provision
exactly the following **8 fleet skills** into the project's `.claude/skills/` directory, one
subdirectory per skill: `captain`, `crew-launch`, `keeper`, `harmonik-dispatch`,
`harmonik-lifecycle`, `agent-comms`, `beads-cli`, `major-issue-fanout`. The skill payload MUST
be read from a binary-embedded filesystem (`embed.FS`) compiled into the harmonik binary; `init`
MUST NOT copy the skills from disk and MUST NOT symlink them to a global location
(`~/.claude/skills/` or equivalent). The three reviewer / scaffold skills `agent-reviewer`,
`agent-config-reviewer`, and `go-subsystem-add` are harmonik-internal and are NOT
boot-load-bearing for the captain / crew / keeper; they MUST NOT be provisioned by `init`. The
embedded fleet-skill payload is approximately 180KB.

The embedded skill assets MUST be a `go:generate`-produced snapshot of the in-repo
`.claude/skills/` directory, kept in lockstep with the source via a CI diff-check that fails the
build when the embedded copy diverges from source. This is the same embed-with-sync-discipline
precedent the daemon already applies to the embedded canonical graph (`//go:embed
standard-bead.dot` at `internal/daemon/standardgraph.go:26`, with the sync discipline documented
at `:8-9`); the init asset bundle is the first DIRECTORY-form embed in the `cmd/harmonik`
package.

**(b) Embedded AGENTS template.** `init` MUST render the project's `AGENTS.md` from a template
read from the same binary-embedded filesystem, NOT from a disk path. (The legacy behavior, which
`os.ReadFile`s `<projectDir>/docs/templates/AGENTS.template.md` and therefore fails on every
repository except the harmonik source tree, MUST be removed.) The in-repo
`docs/templates/AGENTS.template.md` remains the single source of truth for the template; the
`go:generate` step copies it into the embedded asset bundle. `init` MUST substitute exactly two
template variables into the rendered output: `$PROJECT_DIR` and `$TARGET_BRANCH`.

**(c) Self-consistent render — minimal scaffolds + reference pruning.** The rendered `AGENTS.md`
MUST be self-consistent: every artifact it references MUST be one of — (i) created by `init`
itself, (ii) operator-owned outside the repo (`~/.claude/CLAUDE.md`), (iii) a live CLI
(`harmonik`, `br`, `kerf`), or (iv) pruned from the foreign-repo template variant. To satisfy
this, `init` MUST write **three minimal committed scaffold files** at the project root —
`AGENT_INDEX.md`, `STATUS.md`, and `TASKS.md` — each a roughly five-line stub that introduces no
onward dangling links. These three scaffolds back the template's "Start here" boot ritual; they
are committed (listed in no gitignore) and become the project's real index over time. The
embedded AGENTS template MUST be a **foreign-repo variant** whose in-repo-only references (to
harmonik's own `docs/orchestrator-rules.md`, `docs/known-workarounds.md`,
`docs/orchestration-protocol-v2.md`, `docs/components/internal/kerf.md`, the kerf-beta-feedback
/ `KERF-FEEDBACK.md` block, and the `STATUS.md#decisions-locked-in` deep link) are pruned or
reduced to a non-dangling note. Seeding a full knowledge base is explicitly OUT of scope for
`init`; the requirement is non-danglingness of the rendered links, NOT knowledge-base
completeness.

**(d) Runtime directories and gitignore.** `init` MUST create the runtime directories
`.harmonik/comms`, `.harmonik/crew`, `.harmonik/keeper`, and `.harmonik/queues`, and MUST ensure
the project's `.harmonik/.gitignore` ignores `crew/`, `keeper/`, and `queues/` (the `comms/`
directory is already ignored).

**(e) Idempotency, `--force`, and never-clobber-siblings.** Every provisioning step `init`
performs MUST be idempotent: on a re-run, each step MUST skip when its output already exists
unless `--force` is passed. `--force` MUST refresh stale embedded assets (so a binary upgrade
can refresh provisioned skills via `init --force`). The skill-provisioning step MUST write ONLY
the eight fleet-skill subdirectories under `.claude/skills/`; it MUST NOT delete, overwrite, or
otherwise touch sibling skill directories that a foreign repository may already contain.

**(f) Removal of the phantom target-branch guard.** `init` MUST NOT carry a fail-closed guard
that refuses a non-default `--target-branch` value. (The legacy guard fail-closed any
`--target-branch ≠ main` pending a bead, `hk-m8vy2`, which does not exist; the merge-retarget
capability that guard waited on has already shipped.) `init` MUST pass a supplied
`--target-branch` through to the project's `branching.yaml` `lands_on` key (via the existing
branching-config write); the REAL fail-closed enforcement of the target branch is owned by the
daemon's own boot guard (hk-sul12: `branching.yaml lands_on` → `TargetBranch` with `flag > file
> default` precedence per WM-005b, plus protect-branch / forbid-default-main enforcement). A
non-default `--target-branch` therefore requires the named branch to exist and to NOT be a
member of `protect_branches`; `init` itself does not enforce this — the daemon's boot guard
does, fail-closed, at startup. `init` MUST also scrub the stale phantom-bead references from its
own usage text and doc blocks.

Tags: mechanism
Axes: llm-freedom=full; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

> Axes note for the integration pass: `init` is operator-invoked provisioning, not a
> daemon-internal cognition path — `llm-freedom=full` reflects that the human operator drives it
> (no LLM cognition participates), while `io-determinism=deterministic` / `idempotency=idempotent`
> reflect the skip-unless-`--force` contract of (e). If the PL convention reserves
> `llm-freedom=none` for "no LLM in this path", the integration pass MAY prefer
> `llm-freedom=none` here; flagged because PL's existing requirements (all daemon-internal) use
> `none`, and `init` is the first operator-CLI requirement in this file.

---

## Cross-spec / integration notes

**(i) Embed-staleness maintenance cost.** The binary-embedded asset bundle (the 8 fleet skills +
the AGENTS template) is a `go:generate` snapshot of the in-repo `.claude/skills/` and
`docs/templates/AGENTS.template.md`. It will silently drift from source without a guard. The
mitigation is a `go:generate` regeneration step plus a CI diff-check that fails the build when
the embedded copy differs from source — the same ongoing-maintenance discipline already
documented for the embedded `standard-bead.dot` at `internal/daemon/standardgraph.go:8-9`. The
integration pass SHOULD record this as a standing maintenance note (and/or an OQ) attached to
PL-029, since it is the one recurring cost the requirement introduces.

**(ii) HARD SEQUENCING CONSTRAINT — C1 CONSUMES C2's de-hardcoded skills.** The embedded
`assets/skills/` bundle is a `go:generate` snapshot of `.claude/skills/`. As of 2026-06-13, **6
of the 8 fleet skills still contain the literal string `/Users/gb/github/harmonik`** (captain's
STARTUP.md and SHUTDOWN.md, keeper's SKILL.md, harmonik-dispatch's SKILL.md,
harmonik-lifecycle's SKILL.md, and major-issue-fanout's SKILL.md). If C1 regenerates and embeds
the assets BEFORE C2's skill-path de-hardcoding lands, `init` would ship skills with the literal
harmonik source path baked in — violating the no-hardcoded-path success criterion on every
foreign repo. Therefore **C2's skill de-hardcoding MUST land before C1 regenerates and embeds the
asset bundle.** This is a hard cross-component ordering constraint the integration pass and the
captain MUST honor: sequence C2 → regenerate assets → C1 land.

**(iii) Foreign-repo AGENTS template is review-gated content work.** Authoring the foreign-repo
variant of `docs/templates/AGENTS.template.md` (decision (c): prune the in-repo-only link sites,
keep the three scaffold-backed references) is hand-edited content, not a mechanical code change —
it is the first thing every foreign-repo captain reads. It MUST route through the review gate as
its own work item, distinct from the embed-plumbing and config-resolution code changes.
