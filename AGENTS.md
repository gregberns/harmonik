# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**. AGENTS.md is a ROUTER: it points you at the right contract; it does not restate one.

## Per-role load map

Each role loads only its slice. The boot runbook in each role's skill is authoritative; this is the map.

- **Captain — cold boot** (see `.claude/skills/captain/STARTUP.md`):
  1. Step 0 — identity + CWD guard.
  2. Step 0a — tier-3 `.harmonik/context/project.yaml` (phase, locked decisions, guardrails).
  3. Step 0b — tier-2 `.harmonik/context/captain-lanes.md` (lanes + epics-in-progress + parked + dated operator directives).
  4. Step 1 — `captain/SKILL.md` + the **`orchestrator-rules` skill** (standing rules) + tier-1 `HANDOFF.md` (a claim, not ground truth).
  5. Step 2 — boot digest = ground-truth; overrides every claim above. Steps 3–6 reconcile / plan / staff / arm watchers.
  - Does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, product/`docs/` knowledge base, full skill bodies. `ROADMAP.md` only on cold boot / milestone.
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch`. Does **NOT** load fleet-level state (ROADMAP, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` reading order + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`. **Deliberately three steps, not the captain's four:** `captain-lanes.md` is captain-tier — its own tier header says "LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers". Do not "fix" this to match §Start here.

## Start here

Read [PRINCIPLES.md](PRINCIPLES.md) first — the eight engineering principles this codebase is built to, and the standard any new or rewritten code is held to. Then, while the delete-and-rewrite program is active, [`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md) — the stable statement of what the program is, what the core subsystem set is, and what "done" means; it changes rarely, and it outranks any handoff on intent. It is the shortest description of what good looks like here; `AGENT_INDEX.md` summarizes it, and it is short enough to reread before writing code rather than only at boot. Then [AGENT_INDEX.md](AGENT_INDEX.md) — the master map of the knowledge base (every doc reachable within two hops), [STATUS.md](STATUS.md) for phase + locked decisions, and `HANDOFF.md` for this-session state (untracked/gitignored — present on this machine, absent on a fresh clone). A **captain** additionally reads `.harmonik/context/captain-lanes.md`, the medium-term lane/epic tracker, before `HANDOFF.md`; crews and implementer-orchestrators skip it, per the load map above.

**Launching a captain or crew?** Use the native umbrella verb — `harmonik start captain` or `harmonik start crew <name>` (keeper auto-armed; NO env var, NO script path — `--project` defaults to cwd). The old `~/.claude/captain-tools/captain-launch.sh` + `HK_PROJECT` env var are RETIRED in favor of `harmonik start captain`. Positional-XOR-flags rule (D2): the simple form is a bare name only (`start crew paul`); the moment any `--flag` appears the name must move to `--name` (mixing a bare name with flags is a hard error). `harmonik captain` / `harmonik crew start <name>` remain as back-compat aliases.

**Launching an oversight session (commodore, admiral)?** ALWAYS use `harmonik start commodore` / `harmonik start admiral` — never a bare `claude --remote-control` or `harmonik agent brief` in a hand-rolled tmux session. Those launch paths write neither the crew registry record nor the `crew-<name>`-prefixed tmux session that the daemon's boot-time orphan sweep (`RunOrphanSweep`) checks for, so the session is reaped as an orphan at the next daemon boot / supervisor revive (hk-zeo5y). `harmonik start commodore` / `admiral` always route through the same registry-protecting RPC as `harmonik crew start <name>` — the identity is the role name (no positional), so protection is automatic, not a step to remember.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `/Users/gb/github/harmonik/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (kerf-first), bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

- **Daily loop / daemon / `queue submit` / `append` / `subscribe`:** the **harmonik-dispatch** skill. Full design: `docs/orchestration-protocol-v2.md`.
- **Monitoring the daemon** (the canonical Monitor pattern, stream-vs-wave, failure triage): the **harmonik-dispatch** skill. Manual hang-recovery: `docs/known-workarounds.md`.
- **CWD discipline** (never `cd` into a worktree; operate from repo root via `git -C` absolute paths): the **orchestrator-rules** skill.
- **Multi-agent comms** (`harmonik comms` bus; dedupe on `event_id`): the **agent-comms** skill. The `.harmonik/comms/*.md` file-outbox is RETIRED — do NOT write to those files.
- **Lifecycle** (init / supervise / reconcile / promote; work-project deployment, `branching.yaml`): the **harmonik-lifecycle** skill. integration→main is always a human PR step.
- **Redeploy the live daemon binary** (in-place swap on the running box; supervisor revival, SIGTERM-the-daemon, health-window/last-good, `daemon-YYYYMMDD-NN` tag): the runbook at [`docs/daemon-redeploy.md`](docs/daemon-redeploy.md).
- **Keeper** (per-session context-fill watcher; now incl. the `hold`/`release` co-working override that suspends the ACT/restart cutoff while WARN still fires): the **keeper** skill.
- **Disk running low** — the runbook at [`docs/disk-reclaim.md`](docs/disk-reclaim.md); check it before hand-deleting anything. **Start at its §0, the shared `~/Library/Caches` Go caches** (`go-build`, `golangci-lint`): measured the single biggest source on 2026-07-28 at 10.5 GiB, more than everything else that sweep found combined, and repeatedly skipped because "macOS-purgeable" reads as "the OS handles it". Then: `$TMPDIR` Go caches, agent session scratchpads, both `.beads/` history tiers, stale worktrees in five locations, unrotated `events.jsonl`, launchd logs.

<!-- END harmonik:managed -->

## Planning with kerf

Non-trivial changes are planned with **kerf** (spec-first; create a kerf work before new subsystems / cross-subsystem refactors / cross-cutting contracts; trivial changes skip it). The full command surface, jigs, workflow, and beta caveats are planning-agent detail — see [`docs/components/internal/kerf.md`](docs/components/internal/kerf.md) §"Commands & Workflow". The `codename:` bead-label + bench-path rules stay in "Key conventions" below.

## Key conventions

- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live in the repo at **`.kerf/works/{codename}/`**. This project has already been localized (`.kerf/config.yaml` sets `storage: local`), so the repo — not the global bench — is the authoritative working directory. The bench path `~/.kerf/projects/gregberns-harmonik/` still resolves: it is a **symlink** to `.kerf/works/`, so either spelling reaches the same files. Write pass artifacts to the path `kerf new` / `kerf show` prints, or you will silently produce orphan files. **Do NOT run `kerf localize` to tidy up misplaced files** — it is not a file reconciler, it is the one-time bench→repo storage migration, and it has already been run here. There is no automated command for a misplaced artifact: move it into the work's directory by hand.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Ten architectural decisions** are locked in as of 2026-04-19. See [STATUS.md](STATUS.md#10-locked-decisions-2026-04-19) — note that section is a stub pointing at git history for the decision text itself. Reopening one requires strong new evidence.
- **`.claude/skills/` is GENERATED OUTPUT for every skill that ships in the binary.** The source is `cmd/harmonik/assets/skills/<name>/` (pulled in by `//go:embed` in `cmd/harmonik/init_skill_assets.go`); `harmonik sync-assets` classifies `.claude/skills/*` as *Managed* and overwrites it from the embed, and **there is no reverse sync**. Editing only the `.claude/skills/` copy silently creates embed drift, resurfaces as a `.harmonik-new` conflict on the next sync, and is eventually reverted. **To change a shipped skill: edit the `cmd/harmonik/assets/skills/` copy, then mirror it byte-for-byte into `.claude/skills/` in the same commit** — the two paths must stay byte-identical. The shipped set is: `agent-comms`, `beads-cli`, `captain`, `crew-launch`, `harmonik-dispatch`, `harmonik-lifecycle`, `keeper`, `major-issue-fanout`, `orchestrator-rules`, `watch` (authoritative list: `ls cmd/harmonik/assets/skills/`). Everything else under `.claude/skills/` is project-authored and safe to edit in place. Each embedded skill file carries the same warning in a `<!-- SOURCE OF TRUTH: … -->` banner under its frontmatter.
- **Cite symbols, not line numbers, in skills and docs.** File-plus-line references (`thresholds.go:72`) rot within days and have repeatedly shipped as stale guidance. Write `internal/keeper/thresholds.go` `HardCeilingAbsTokens` and let the reader grep.
- **Write guidance as principles, not laws.** An agent treats a rule as a law — it obeys the letter and does goofy things to satisfy it; a principle gives a direction to travel and preserves judgment. When writing skills, AGENTS content, or review criteria, prefer "lean toward X because Y" / "treat Z as a smell worth investigating" over "never X" / "always Y". Reserve hard mechanical constraints for genuine safety or irreversibility.
- **Codex and Pi implement; Claude is reserved for oversight.** Claude tokens are the constrained resource and the operator is on a Codex subscription, so staff implementer crews on the Codex or Pi harness (`harmonik start crew --name <name> --harness codex` — note the D2 positional-XOR-flags rule above: with any flag present the name must move to `--name`) and keep Claude for the roles where judgment is the product — captain, admiral, reviewer. Nothing selects a harness by default, so choosing Codex is a deliberate act; the reviewer node stays on Claude in code (`cmd/harmonik/substrate_select.go` `reviewerSubstrate`). Operator surface: [`docs/codex-operator-guide.md`](docs/codex-operator-guide.md).
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:handler-pause`, `codename:claude-hook-bridge`). Kerf work `bead_filter` clauses must match the same form. Functional/topical labels (e.g. `queue`, `spec-drift`) remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Don't

- Don't reopen locked-in decisions without explicit user request.
- Don't add abstraction layers the user hasn't asked for.
- Don't skip your role's reading order when picking up the project: `PRINCIPLES → AGENT_INDEX → STATUS → HANDOFF`, plus `captain-lanes` (Step 0b) if you are the captain. See the per-role load map above.

<!-- bv-agent-instructions-v2 -->

---

## Issue tracking → [`docs/beads-workflow.md`](docs/beads-workflow.md)

Issues are beads (`br`); kerf ranks them. The command surface, the workflow loop, commit-message
validation, and the UBS (Ultimate Bug Scanner) quick reference all live in
[`docs/beads-workflow.md`](docs/beads-workflow.md) — read it when you need them. The `beads-cli`
skill owns the read/write discipline. Only the traps you cannot look up stay here:

- **The ledger is machine-local and gitignored.** Beads do NOT travel between clones or reach CI, and
  `br sync --flush-only` stages nothing. Never "fix" this by tracking `.beads/` — a perpetually dirty
  tree trips the daemon's `implementer_escaped_worktree` detector and false-fails dispatched beads
  (refs `hk-yru`). Anything that must survive goes in a tracked doc or the commit message.
- **Named initiatives outrank `kerf next`.** Work the operator's and admiral's named initiatives
  first; `kerf next` ranks the *unclaimed backlog* below that line — it is never an override.
- **Never run bare `bv`** — it launches an interactive TUI that blocks your session. `--robot-*`
  flags only, and only for graph metrics; kerf owns prioritization.
- **Commit with `git commit -F <file>`, never `-m`.** Non-trivial commits require `Reviewed-By:` and
  `Review-Verdict:` trailers (JSON, `schema_version: 1`); a `BLOCK` verdict is never committed.
  Validation is agent-enforced — git hooks are retired. `--no-verify` is forbidden.
- **Run `/check` after committing.** It judges your commit (`--new-from-rev=HEAD~1`), not the repo.
  Never gate on bare `make check` — it always exits non-zero by design.

<!-- end-bv-agent-instructions -->

> **Maintainer note — the block above is machine-regenerable.** `br agents --update` rewrites everything between the `bv-agent-instructions-v2` markers from br's generic upstream template. Three harmonik-specific corrections live inside it and WILL be reverted to upstream's (false) wording if you run that command: (1) the bead ledger is gitignored and machine-local, not "stored in `.beads/` and tracked in git"; (2) `git commit -m "..."` alone omits this repo's required trailers; (3) commit-message validation is agent-enforced via the `/check` flow — git hooks (lefthook) are retired, not "wired via `lefthook.yml`". Re-apply all three after any `br agents --update`, and diff the block before accepting the result.
