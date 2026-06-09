# Agent Configuration

> How coding agents (Claude Code sessions primarily; Pi and others post-MVH) are configured to build harmonik consistently across many sessions. This doc is the contract between the human and any agent: agents MUST read and follow it; reviewers MUST check that new work conforms. Cross-refs: `subsystem-organization.md` (package layout), `testing.md` (test tiers), `docs/methodology/AGENT_GUIDE.md` (knowledge-base navigation), `docs/components/internal/kerf.md` (planning).

## Decisions

1. **Tiered AGENTS.md (with CLAUDE.md symlink).** Repo-root `AGENTS.md` is canonical — the single file all agents read. Repo-root `CLAUDE.md` is a symlink → `AGENTS.md`. Per-directory agent config files follow the same pattern (AGENTS.md canonical; CLAUDE.md symlink) and are created only where local rules justify it.
2. **AGENTS.md canonical across all agents.** Claude Code, Pi, Codex, or any future agent reads `AGENTS.md` (possibly via `CLAUDE.md` symlink or its own convention). Single source of truth; no mirror drift. Claude-specific content (hook references, Skill tool usage, `/Users/gb/.claude/projects/...` memory paths) lives in a `## Claude-specific` section inside `AGENTS.md`; other-agent-specific sections similarly named (`## Pi-specific`, etc.).
3. **Skills live in repo.** `.claude/skills/` at repo root. Project-specific skills are versioned and reviewed; user-global skills stay in `~/.claude/skills/`. Handler-contract skill-injection (foundation §4.11) reads from the project skills dir.
4. **Memory is user-scoped, index-driven, agent-updated.** `/Users/gb/.claude/projects/-Users-gb-github-harmonik/memory/` stays the source; agents update `MEMORY.md` index + add per-topic files when a durable preference or project fact emerges. Not every session; only on new durable content.
5. **SESSION_HANDOFF.md is the cross-session baton.** Overwritten at every session end. Git history preserves prior handoffs. Mandatory on session exit if any non-trivial work landed.
6. **Update cadence: end-of-session + end-of-kerf-pass.** Every session end runs the config-review checklist below. Every `kerf` pass advance (problem-space → decompose → research → design → spec-draft) runs a richer review that MAY update rules and skills.
7. **Review authority: reviewer subagent.** A dedicated `agent-config-reviewer` prompt (stored under `.claude/skills/agent-config-reviewer/`) runs at the cadence points above and emits a diff against the current configuration. Main agent applies or defers.
8. **CONSTITUTION.md as non-recursive trust anchor.** `CONSTITUTION.md` at the repo root enumerates the immutable foundational decisions agents must not mutate without explicit human sign-off. Edits to it require a `Constitution-Edit-Approved-By: <human-name-or-email>` commit trailer; agents MUST NOT commit edits to CONSTITUTION.md without this trailer. Listed in Protected rule files (§Protected rule files); edit surfacing in CI is automatic.

## Repo-root AGENTS.md — what it contains

Repo-root AGENTS.md stays under 120 lines (CLAUDE.md is a symlink → AGENTS.md; same content). Contents: **entry ritual** (read order `AGENT_INDEX.md` → `STATUS.md` → `TASKS.md` → `SESSION_HANDOFF.md`); **kerf planning rules** (keep current content); **the 10 locked + 4 candidate decisions** (as a list pointing to `STATUS.md`); **hard don'ts**; **pointers, not prose** (git → this doc; Go → this doc; tests → `testing.md`; layout → `subsystem-organization.md`). Per-directory or per-subsystem content does NOT belong here.

## Per-directory AGENTS.md (with CLAUDE.md symlink) — when to use

Only when a directory has rules the repo-root does not cover. Each per-dir `AGENTS.md` has a sibling `CLAUDE.md` symlink (same canonical/symlink pattern as the repo root). MVH list (create lazily as each lands): `internal/core/AGENTS.md` (no internal imports); `internal/daemon/AGENTS.md` (composition root only); `test/scenario/AGENTS.md` (scenario YAML shape + harness invocation); `specs/AGENTS.md` (normative; edit via `kerf finalize`); `.kerf/AGENTS.md` (gitignored process artifacts; not source of truth). Agents MUST NOT create these speculatively; one concrete local-only rule required.

## CLAUDE.md symlink — implementation

Repo-root `CLAUDE.md` is a symlink to `AGENTS.md`. Same for each per-directory pair. Symlinks are tracked in git (pointing at filename, not path). Implementation: `ln -sf AGENTS.md CLAUDE.md` at repo root + each dir that has an AGENTS.md. The `agent-config-reviewer` skill verifies the symlink exists and resolves on every cadence review (Tier 2) — if a symlink is broken or replaced by a regular file, it's flagged as a config violation.

## Skills

Skills live at `.claude/skills/<skill-name>/` in the repo, plus `~/.claude/skills/` for user-global. Harmonik-MVH skills (load-bearing):

| Skill | Where | Purpose |
|---|---|---|
| `beads-cli` | `.claude/skills/beads-cli/` | `br` CLI usage patterns; injected by handler-contract §4.11 into every agentic node whose workflow declares `required_skills: [beads-cli]`. |
| `kerf-workflow` | `.claude/skills/kerf-workflow/` | kerf jig advancement, review subagent invocation. Referenced from repo-root `CLAUDE.md`. |
| `go-subsystem-add` | `.claude/skills/go-subsystem-add/` | adding a new Go package under `internal/<subsystem>/`: `.golangci.yml` `depguard` rule addition (files: scope + allow matrix), core-type discipline, test scaffolding. |
| `go-test-run` | `.claude/skills/go-test-run/` | running the test tiers from `testing.md` (default, integration, scenario, crash, realagent); enforces `-race`. |
| `project-quality-gates` | `.claude/skills/project-quality-gates/` | documents `make check-fast` / `make check` / `make check-full` targets and when each runs (author iteration / WIP / declared-done). Agents MUST invoke gauntlets via this skill rather than ad-hoc `go test` / `go vet` chains. |
| `git-task-commit` | `.claude/skills/git-task-commit/` | per-node commit cadence, trailer format (`Harmonik-Run-ID`, etc.), integration-branch merge flow. |
| `spec-finalize` | `.claude/skills/spec-finalize/` | `kerf finalize` handoff: copy drafts to `specs/`, update AGENT_INDEX, open a foundation-amendment log entry if needed. |
| `agent-reviewer` | `.claude/skills/agent-reviewer/` | runs on every non-trivial commit (per `build-practices.md §Agent review on every commit`). Checks spec alignment, idiom compliance, test adequacy, unwanted-abstraction detection, bead/codename match. Emits `APPROVE` / `REQUEST_CHANGES` / `BLOCK` verdict; the non-`BLOCK` verdict lands as the commit's `Reviewed-By:` trailer. **Load-bearing; must not rot.** Tier 2 cadence explicitly checks its currency. Emits JSON verdict per `build-practices.md §Commit conventions`. Schema owned by this skill; versioned. |
| `agent-config-reviewer` | `.claude/skills/agent-config-reviewer/` | the review subagent for the cadence below; emits a diff proposing CLAUDE.md / AGENTS.md / skills updates. |
| `crew-launch` | `.claude/skills/crew-launch/` | boot context for a Captain & Crew crew orchestrator: parse handoff, join comms, mirror `--assignee` on every epic adoption (Gap 1, load-bearing), subscribe inbox with `event_id` dedupe, dispatch beads to the crew's OWN named queue (NEVER `main`), and emit the mandatory progress feed on both surfaces (comms `--topic status` + `br comments`) on the locked cadence. |

Any node whose workflow declares a skill MUST have that skill resolvable; handler fails launch otherwise (foundation §4.11).

## CONSTITUTION.md

The `CONSTITUTION.md` at repo root lists immutable project foundations. Edits require explicit human sign-off.

**Contents** (kept short — one-liner per item, pointer to full spec):
- The three-store model (git / Beads / JSONL) and git-wins-on-completion arbitration.
- Centralized controller (daemon owns workflow state; Gas Town polecat model rejected).
- Three-artifact separation: spec / workflow graph / bead (no "feature" primitive).
- Direct-to-main + agent-reviewer-every-commit (MVH, revisit when product has real users).
- Deterministic skeleton + probabilistic organs (daemon in Go, cognition in agents).
- The 10 locked decisions from 2026-04-19 + the 4 candidate decisions from 2026-04-20/21.
- Pointer to full spec corpus in `docs/foundation/` and `specs/` (once `kerf finalize` populates it).

**Edit discipline.**
- Agents MUST NOT commit CONSTITUTION.md edits without a `Constitution-Edit-Approved-By: <name-or-email>` commit trailer.
- The file is in Protected rule files (§Protected rule files); edits surface automatically in CI.
- A commit lacking the trailer on a `CONSTITUTION.md`-touching diff is rejected by a pre-commit hook AND flagged by post-commit CI as a red status.
- Adding items to CONSTITUTION.md is still a CONSTITUTION.md edit; requires the same sign-off.
- Removing items from CONSTITUTION.md = explicit reversal of a foundational decision. Must be accompanied by a new log entry + a kerf codename tracking the change.

**Why not-just-STATUS.md.** STATUS.md is agent-editable freely (agents update project state there all the time). CONSTITUTION.md is the narrow non-recursive slice: the things that can't quietly mutate.

## Rule categories

### Git operations

- **Commit per node.** One git commit per workflow node that produces durable state. Trailers: `Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, optional `Harmonik-Bead-ID`, `Harmonik-Schema-Version` (execution-model §2.1b). Interactive-session commits (human or agent editing outside a run) do NOT carry these trailers.
- **Branching.** Runtime workflow runs use 3-level branching: node commits → `run/<run_id>` task branch → `harmonik/integration` (workspace-model §5.8). This is runtime behavior of harmonik, NOT how harmonik-itself is built.
- **Project-level work: direct commits to `main`** (per `build-practices.md §Branch model — direct-to-main`). Ephemeral `agent/<codename>` branches allowed only for parked-across-sessions work; squash-merge back to main on resume.
- **Agent reviewer runs on every non-trivial commit** (per `build-practices.md §Agent review on every commit`). Skipping reviewer on a non-trivial commit is a process violation. Verdict lands in two commit trailers: `Reviewed-By: agent-reviewer` (presence marker) + `Review-Verdict:` (structured JSON per `build-practices.md §Commit conventions`). `BLOCK` never lands.
- **Never amend** in non-interactive workflows. Interactive sessions: amends allowed for cosmetic fixes only, never across sessions.
- **Never force-push** `main` or `harmonik/integration`. `run/*` branches may be force-pushed by their owning run only. `agent/<codename>` park-branches may be force-pushed by their owning session only.
- **Pre-commit hook runs `make check-fast`** (Tier 1 gauntlet per `quality-checks.md`): `gofumpt`/`gci` diff, `go vet ./...`, `go build`, `golangci-lint run --new-from-rev=HEAD~1` (includes `depguard` component-graph rules), `go test -short` on touched packages. Hook failure blocks commit; agents fix the root cause, they MUST NOT `--no-verify`.

### Go procedures

- **Agent-declared-done runs `make check-full` locally before every non-trivial commit and before completion is reported.** Tier 3 gauntlet (per `quality-checks.md §Three-tier identical gauntlet`) exercises integration + scenario + fast-crash tests. No commit or declaration of completeness without this passing. Post-push CI runs the identical gauntlet; a local pass predicts CI pass. If CI fails after a local pass, treat as environment drift (a bug in setup), not CI-specific behavior; fix forward with a corrective commit.
- **Adding a package.** Invoke the `go-subsystem-add` skill. It creates the `internal/<pkg>/` dir, adds the component to `.golangci.yml` `linters-settings.depguard.rules` (files: scope + allow matrix per `subsystem-organization.md`), scaffolds `doc.go` + `<pkg>_test.go`. Direct creation without skill is a review failure.
- **Core types.** Anything that crosses a subsystem boundary goes in `internal/core`; everything else stays in its owning subsystem (subsystem-organization.md §Shared types). Agents violating this are flagged by `depguard` (via `golangci-lint`).
- **Running tests.** The tiered gauntlets consolidate test tiers — agents run `make check-fast` during iteration, `make check` for work-in-progress verification, `make check-full` before declaring done. The prior `make test` / `make test-integration` / `make test-scenario` / `make test-crash` targets roll up into `make check` (unit+property) and `make check-full` (everything else). All local runs use `-race` by default. Nightly tier (`realagent`) runs only in CI.
- **Lint-clean commits.** `go vet`, `golangci-lint run` (includes `depguard` component-graph + `staticcheck` + full enabled set), `gofumpt`, `tools/go-linters/forbid-import` all MUST pass. Allowlist edits (testing.md §Libraries) require explicit justification in the commit body AND trigger the protected-rule-file flow (see below).

### Commit style

- **Subject ≤72 chars, imperative mood.** `orchestrator: add edge cascade`.
- **Body** is optional for trivial changes; required when a decision or trade-off was made. Body cites the spec section driving the change (`per execution-model §2.1c`).
- **Co-author trailer** mandatory for agent-written commits: `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>` (or the active model's trailer). Humans MAY omit.
- **Fixture refreshes** (testing.md §Fixture conventions) diff the fixture in the commit body and name the trigger (e.g., "claude-code wire format changed at 2026-05-02 capture").

### When to commit

- **Inside a run:** one commit per durable node. Non-durable nodes (evaluations, control-points) do NOT commit. The workflow engine enforces this; agents don't choose.
- **Interactive work:** commit at each coherent change-unit (≤~200 lines of diff, one logical idea). Never bundle unrelated changes.
- **Work-in-progress commits are banned on `main` / integration.** On `run/*` branches, WIP is the norm; squash is the integration merger's choice.

### Commit creation (direct-to-main)

- **No PRs at this phase** (per `build-practices.md`). MVH and post-MVH until real users adopt the product, work lands via direct commit to `main`. PR workflow returns when the product has real users or multiple human contributors.
- **Commit message** follows Conventional Commits per `build-practices.md §Commit conventions`. Subject rules unchanged; non-trivial commits include a body with Why / What / Spec alignment / Test plan / Risk sections (same information previously required in PR bodies).
- **Required trailers**: `Refs:` (bead-id or kerf-codename) for tracked work items; `Co-Authored-By:` for agent-assisted commits; on every non-trivial commit:
  - `Reviewed-By: agent-reviewer` (presence marker)
  - `Review-Verdict: {"verdict": "APPROVE|REQUEST_CHANGES", "flags": [...], "notes": "..."}` (structured JSON; see `build-practices.md §Commit conventions`)

  `BREAKING CHANGE:` footer when applicable.
- **Before commit.** `make check-full` passes AND `agent-reviewer` ran with a non-`BLOCK` verdict (APPROVE, or REQUEST_CHANGES with rationale in the commit body).
- **Run-branch merges to integration** happen via the workflow engine without a PR (runtime behavior, unchanged).

### Spec adherence

- **Specs in `specs/` are normative.** Code matches spec; spec doesn't match code. Divergence requires either (a) a spec update via kerf + log entry, or (b) a recorded deferral in the spec's `Deferred / follow-up` section.
- **Agents writing code check the spec first.** Every non-trivial commit body names the spec section(s) it implements.
- **The 10 locked decisions + 4 candidate decisions (STATUS.md §Decisions) are treated as spec.** Reopening requires explicit user request.

### Protected rule files

Cross-reference: `quality-checks.md §Protected rule files`.

- Agents MAY propose rule changes (loosenings, linter additions, coverage threshold moves) via direct commits that isolate the rule edit.
- A rule-change commit MUST cite a kerf-codename in its body explaining the change's motivation.
- Edits to `.golangci.yml`, `.depguard.yml`, `tools/go-linters/forbid-import.go`, `scripts/coverage-gate.sh`, `.github/workflows/*`, `Makefile`, `.lefthook.yml`, `CONSTITUTION.md` trigger the automatic `rule-change` CI surface (per `quality-checks.md §Agent-enforceability`). Edits to `CONSTITUTION.md` additionally require a `Constitution-Edit-Approved-By: <name-or-email>` commit trailer (in addition to the kerf-codename requirement for rule-file edits generally).
- Agents MUST NOT bundle rule changes with code changes in the same commit. One commit = one concern (rule change OR code), so the rule-change diff can be reviewed on its own merits by `agent-reviewer` and the async human reader.

## Memory system usage

The auto-memory at `/Users/gb/.claude/projects/-Users-gb-github-harmonik/memory/` uses three filename prefixes, already in practice:

- `project_harmonik_<topic>.md` — durable project facts (state model, branching model, process lifecycle, etc.).
- `feedback_<topic>.md` — user collaboration preferences (response granularity, design framing, proposal style).
- `user_profile.md` — one file.

**Agents MUST:**

1. Read `MEMORY.md` (the index) on session start, after `SESSION_HANDOFF.md`.
2. Add a new memory file when a durable preference or project-level fact emerges that future sessions will need. Update the `MEMORY.md` index in the same commit.
3. NOT re-save things already in `CLAUDE.md`, `STATUS.md`, or existing memory files. Memory is the lowest-priority store; prefer the knowledge base.
4. NOT edit existing memory files to flip a decision without explicit user sign-off.

Memory is user-scoped (not repo-scoped), so it survives repo resets but is not shared with other developers. Durable project facts that other humans need MUST also land in the repo (`docs/` or `specs/`).

## Cross-session continuity

`SESSION_HANDOFF.md` at repo root is the cross-session baton. Structure (enforced; existing file is the canonical template):

1. **Read this first** — numbered list of files.
2. **What landed this session** — 1–3 paragraphs, concrete deltas.
3. **Where the kerf work stands** — which kerf codename, what pass, next advancement.
4. **Recommended next-session flow** — 2–5 numbered steps.
5. **Open discussion threads** — carried items with IDs.
6. **Important collaboration notes** — what was reinforced this session (not a re-dump of standing rules).
7. **What should NOT be re-opened** — pointer to locked decisions + this-session additions.
8. **Files worth knowing about** — paths the next session will touch.
9. **Log entry** — link to `docs/log/<date>-<slug>.md` for this session.

Handoff MUST be written before session exit if any of: a kerf pass advanced, a spec landed, a decision was taken, or ≥5 files changed. Trivial sessions (docs typo, one-line clarification) MAY skip.

## Update cadence

The user's suggestion — "review task after every section" — becomes a two-tier cadence:

**Tier 1 — Every session end (lightweight, mandatory).**
Before writing `SESSION_HANDOFF.md`, the main agent runs a self-check:
1. Did any new rule emerge? → propose an edit to this doc.
2. Did a skill fail or gap show up? → open a TASKS.md item to author/revise the skill.
3. Did a memory preference surface? → write the memory file.
4. Did per-directory rules drift from repo-root rules? → flag.
5. **Did `make check-full` pass for any code changes this session?** Outcome recorded in `SESSION_HANDOFF.md`. Failure to record is a process violation. (Sessions that did not touch code MAY skip this item; note the skip.)
6. **Did `agent-reviewer` run on every non-trivial commit this session?** Outcome recorded via the `Reviewed-By:` commit trailer on each commit. A session with non-trivial commits lacking `Reviewed-By:` trailers is a process violation; open a TASKS.md item to retroactively review and note the gap in the session log.
Output: a short `## Config review` stanza in the session log entry.

**Tier 2 — Every kerf pass advance (heavier, reviewer subagent).**
When `kerf status <work> <next-pass>` is about to run, invoke the `agent-config-reviewer` skill. It consumes: current `CLAUDE.md`, `AGENTS.md`, this doc, the kerf work's artifacts, the last N session handoffs. It emits a diff proposing updates. Main agent applies, defers (with a TASKS.md line), or rejects with reason in the log.

**Tier 3 — Release / finalize (heaviest, explicit).**
On `kerf finalize`, spec lands in `specs/`, and the finalize skill runs `agent-config-reviewer` with a wider prompt (include the new spec). This is the moment to register new skills the spec requires (e.g., if the spec introduces a new node type, add a skill for authoring it).

**Automatic Tier 2 trigger on foundation drift.** Changes to any of `quality-checks.md` / `subsystem-organization.md` / `testing.md` / `build-practices.md` automatically trigger a Tier 2 review cycle to keep this doc's references current (skill names, make-target names, protected-file paths, toolchain versions). The reviewer subagent receives a diff of the changed foundation doc(s) alongside the current `agent-configuration.md`.

Failure mode this prevents: rules that exist only in one agent's head, rediscovered by the next agent. Failure mode this does NOT prevent: agents ignoring the config entirely — which is a process-compliance problem, addressed by P05 and by hook-enforcement post-MVH.

## ⚑ Assumptions worth user's eye

1. **⚑ AGENTS.md canonical; CLAUDE.md symlink.** Chosen over the reverse (CLAUDE.md canonical + AGENTS.md mirror) because AGENTS.md is the multi-agent-ecosystem convention — single file, no drift. Some Claude Code tooling may write to CLAUDE.md as if it were a real file; test this at bootstrap and fall back to a hand-maintained mirror if symlink handling breaks the tool.
2. **⚑ Skills live in-repo at `.claude/skills/`.** Claude Code reads this path, but it mixes harness-specific config into the source tree. Alternative: keep skills at `~/.claude/skills/` globally and inject via handler-contract's skill-search-paths. In-repo is chosen because skills are versioned code, not user preferences.
3. **⚑ `agent-config-reviewer` skill is the enforcement mechanism.** A skill reviewing the skills/rules config is recursive; the skill itself is the thing most likely to rot. Main-agent self-check at Tier 1 is the fallback.
4. **⚑ Tier 2 review at every kerf pass advance may be excessive.** Early passes (problem-space, decompose) rarely generate config changes. If Tier 2 becomes noise, gate it to passes that touched code or specs.
5. **⚑ Memory is user-scoped, repo is multi-user.** A second human working on harmonik sees `CLAUDE.md` but not the user's memory. Durable facts in memory that the team needs MUST be promoted to `docs/`. Enforcing this is on the human, not the agent.
6. **⚑ Pre-commit hook blocking `--no-verify`.** No mechanical enforcement in git itself; this is a rule agents obey. A pre-receive hook on the remote would enforce mechanically — post-MVH concern.

## Deferred / follow-up

- **Hook-based enforcement (post-MVH).** `Gas Town Hooks` (concept I01) lets us enforce rules mechanically via Claude Code hooks — e.g., "before commit, verify trailer format." Register hooks once the hook-system subsystem (S05) has a concrete spec.
- **Cross-agent skill sharing.** When Pi lands, audit which skills translate across agent types vs need per-handler variants. Initial assumption: beads-CLI is universal; go-test-run is universal; agent-config-reviewer may need per-handler system-prompt variants.
- **Skill versioning.** If a skill's semantics change, in-flight runs may have the old version. Once `required_skills[]` is versioned (handler-contract §4.11), revisit here.
- **Agent-config drift detector.** A nightly CI job that runs `agent-config-reviewer` against recent session logs and opens a PR if the config is stale. Requires CASS (S08) to be live.
- **Multi-agent concurrent editing of this doc.** When two sessions both propose config edits, merge strategy. Unlikely pre-MVH (single-run concurrency); revisit when parallel runs land.
