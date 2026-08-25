# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**.

AGENTS.md is a ROUTER. What belongs here is a pointer to the contract that owns a thing, plus the one line of judgment a reader needs to use the pointer. Judge it by that test, not by a line count: if a paragraph could go and no reader would lose something the pointed-at file does not carry, move it out. When this file grows, find the thing that grew and give it to a skill or a doc.

## Per-role load map

Each role loads only its slice, and **this map is the reading order** — where any other file states one, it defers here. Each role's own skill stays authoritative for that role's steps; this is the map across roles. The slices differ on purpose.

- **Captain — cold boot.** Boots from `harmonik agent brief`, which injects its identity and last handoff and hands it PATHS for the rest. Then it loads four things and no more: the three tier files under `.harmonik/context/` (`project.yaml` = phase and locked decisions; `captain-lanes.md` = the lane table; `direction-log.md` = the RETURN-PATH that says what resumes in what order), `captain/STARTUP.md` (the ordered boot runbook, which owns the step order — this map does not restate it), `captain/SKILL.md`, and the **`orchestrator-rules` skill**. Ground truth is one `scripts/captain-boot-digest.sh` call, which overrides every claim in a handoff or a tier file. Does **NOT** boot-read `AGENT_INDEX.md`, `STATUS.md`, `PRINCIPLES.md`, the programme charter, the `docs/` knowledge base, or any full skill body — a captain coordinates and does not write code, so the code standard and the programme charter are on-demand reads (`CHARTER.md` §2 for the sequence, §6 for what done means).
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read the tier files + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch` + `PRINCIPLES.md` (crews write the tests, so they need the standard the tests are held to). Does **NOT** load fleet-level state (ROADMAP, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Assessor** (see [`roles/assessor/operating.md`](roles/assessor/operating.md) — that file is the contract): `roles/assessor/soul.md` + `operating.md` + `good-enough-principles.md` + `personality.md`, then the gate definition at `plans/2026-07-27-delete-and-rewrite/ASSESSOR-GATE.md` and the severity rubric at `plans/2026-07-06-quality-system/07-assessor-severity-framework.md`. Writes its output to a dated folder under `assessments/`, created before the gate runs, and owns the live failure corpus at [`test/exploratory/cases/`](test/exploratory/cases/) — the cases it re-runs every gate and extends before it terminates. **The assessor's distinctive job is to spin the process up and run real work through it**; a passing suite is not evidence the system does what we want, and other agents own the unit tests. The role is a document, not a process — no command starts one. Wiring only (harness, injected context) lives in `.harmonik/agents/assessor/manifest.yaml`.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`. **Three steps, not the captain's four:** `captain-lanes.md` is captain-tier — its own tier header says "LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers". Putting it back here is the recurring wrong fix.
- **Any session with no role:** `PRINCIPLES.md` → the charter → `AGENT_INDEX.md` → `STATUS.md` → `HANDOFF.md`. §Start here describes the same order in prose.

## Start here

**Every role that writes code starts with the same two files.** [PRINCIPLES.md](PRINCIPLES.md) — the nine engineering principles this codebase is built to, and the standard any new or rewritten code is held to. Then, while the delete-and-rewrite program is active, [`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md) — what the program is, what the core subsystem set is, and what "done" means. It changes rarely and it outranks any handoff on intent, so reread it before writing code rather than only at boot. A **captain** writes no code and loads neither at boot; the load map above is the authority on that.

What you read after those two depends on your role, and the load map above is the statement of it. A session with no role reads [AGENT_INDEX.md](AGENT_INDEX.md) — the curated map of the knowledge base — then [STATUS.md](STATUS.md) for phase and locked decisions, then `HANDOFF.md` for this-session state (untracked and gitignored, so it is present on this machine and absent from a fresh clone). A **captain** boot-reads neither `AGENT_INDEX.md` nor `STATUS.md`, and does read `.harmonik/context/captain-lanes.md`, which is captain-tier and which crews and implementer-orchestrators skip.

**Launching a captain or crew?** Use the native umbrella verb — `harmonik start captain` or `harmonik start crew <name>` (keeper auto-armed; NO env var, NO script path — `--project` defaults to cwd). The old `~/.claude/captain-tools/captain-launch.sh` + `HK_PROJECT` env var are RETIRED in favor of `harmonik start captain`. Positional-XOR-flags rule (D2): the simple form is a bare name only (`start crew paul`); the moment any `--flag` appears the name must move to `--name` (mixing a bare name with flags is a hard error). `harmonik captain` / `harmonik crew start <name>` remain as back-compat aliases.

**Launching an oversight session (commodore, admiral)?** ALWAYS use `harmonik start commodore` / `harmonik start admiral`, never a bare `claude --remote-control` or `harmonik agent brief` in a hand-rolled tmux session. A hand-rolled session writes neither the crew registry record nor the `crew-<name>`-prefixed tmux session that the daemon's orphan sweep (`RunOrphanSweep`) checks for, so it is reaped at the next daemon boot or supervisor revive. The `start` verbs route through the same registry-protecting RPC as `harmonik crew start`, so protection is automatic rather than a step to remember.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `/Users/gb/github/harmonik/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (stated intent first, then the ledger), bead lifecycle (the daemon owns the terminal transitions of what you submit to a queue; you close what you worked by hand), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

- **Daily loop / daemon / `queue submit` / `append` / `subscribe`:** the **harmonik-dispatch** skill. Full design: `docs/orchestration-protocol-v2.md`.
- **Monitoring the daemon** (the canonical Monitor pattern, stream-vs-wave, failure triage): the **harmonik-dispatch** skill. Manual hang-recovery: `docs/known-workarounds.md`.
- **CWD discipline** (never `cd` into a worktree; operate from repo root via `git -C` absolute paths): the **orchestrator-rules** skill.
- **Multi-agent comms** (`harmonik comms` bus; dedupe on `event_id`): the **agent-comms** skill. The `.harmonik/comms/*.md` file-outbox is RETIRED — do NOT write to those files.
- **Lifecycle** (init / supervise / reconcile / promote; work-project deployment, `branching.yaml`): the **harmonik-lifecycle** skill. integration→main is always a human PR step.
- **Redeploy the live daemon binary** (in-place swap on the running box; supervisor revival, SIGTERM-the-daemon, health-window/last-good, `daemon-YYYYMMDD-NN` tag): the runbook at [`docs/daemon-redeploy.md`](docs/daemon-redeploy.md).
- **Keeper** (per-session context-fill watcher; now incl. the `hold`/`release` co-working override that suspends the ACT/restart cutoff while WARN still fires): the **keeper** skill.
- **Disk running low** — the runbook at [`docs/disk-reclaim.md`](docs/disk-reclaim.md), which sweeps in yield order: the shared `~/Library/Caches` Go caches first (its §0 — by far the biggest source, and the one people skip because "macOS-purgeable" reads as "the OS handles it"), then `$TMPDIR` Go caches, agent scratchpads, the `.beads/` history tiers, stale worktrees, unrotated `events.jsonl`, and launchd logs. Check it before hand-deleting anything.

<!-- END harmonik:managed -->

## Planning with kerf

Non-trivial changes are planned with **kerf** (spec-first; create a kerf work before new subsystems / cross-subsystem refactors / cross-cutting contracts; trivial changes skip it). The full command surface, jigs, workflow, and beta caveats are planning-agent detail — see [`docs/components/internal/kerf.md`](docs/components/internal/kerf.md) §"Commands & Workflow". The `codename:` bead-label + bench-path rules stay in "Key conventions" below.

## Key conventions

- **Work lands on the integration branch, never on `main`.** The rule is owned by
  [`docs/foundation/project-level/build-practices.md`](docs/foundation/project-level/build-practices.md)
  §"Branch model — land on the integration branch". Read it there. It replaces the retired
  direct-to-main model.
- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live in the repo at **`.kerf/works/{codename}/`**; the bench path `~/.kerf/projects/gregberns-harmonik/` is a symlink to it, so either spelling reaches the same files. Write pass artifacts to the path `kerf new` / `kerf show` prints, or you silently produce orphan files. **`kerf localize` does not tidy those up** — it is the one-time bench→repo storage migration and it has already been run here. Move a misplaced artifact by hand.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Role instructions live in `roles/`** at the repo root — `assessor`, `captain`, `admiral`, `lane` so far (`lane` is shared by every hand-run delivery lane). **A role is a document, not a process: nothing starts one**, and assuming there was a launch command has cost real time more than once — an agent already running becomes the assessor by reading `roles/assessor/`. Each role's `operating.md` marks daemon-dependent steps **[FLEET]**, so the same file works with harmonik running and with nothing running. `.harmonik/agents/<role>/manifest.yaml` points at the folder with a `role:` key and keeps only the harmonik-side wiring; a manifest with no `role:` key reads `soul.md` and `operating.md` from its own folder (`crew`, `commodore`, `watch` still do). **There is one copy of a role's instructions — the one in `roles/`.** A "see roles/" stub is still a file that drifts. See [`roles/README.md`](roles/README.md).
- **The assessor's output lives in `assessments/`**, one dated folder per assessment (`YYYY-MM-DD-HHMM-<slug>`), in the same spirit as `plans/`. It is created BEFORE the gate runs and written as the work happens — a record assembled afterwards agrees with the verdict because the same mind produced both. Four files, copied from `assessments/_TEMPLATE/`: the mission, the evidence, the findings, the verdict. See [`assessments/README.md`](assessments/README.md).
- **Ten architectural decisions are locked** as of 2026-04-19. [STATUS.md](STATUS.md#10-locked-decisions-2026-04-19) is a stub that names the commit carrying the decision text. **Reopening one is the operator's call, and evidence is what earns the conversation** — they were locked because re-litigating them cost more than living with them. That the current task would be easier the other way is not evidence, and finding good evidence is not the same as having the authority to act on it.
- **`.claude/skills/` is GENERATED OUTPUT for every skill that ships in the binary.** The source is `cmd/harmonik/assets/skills/<name>/` (pulled in by `//go:embed` in `cmd/harmonik/init_skill_assets.go`); `harmonik sync-assets` classifies `.claude/skills/*` as *Managed* and overwrites it from the embed, and **there is no reverse sync**. Editing only the `.claude/skills/` copy silently creates embed drift, resurfaces as a `.harmonik-new` conflict on the next sync, and is eventually reverted. **To change a shipped skill: edit the `cmd/harmonik/assets/skills/` copy, then mirror it byte-for-byte into `.claude/skills/` in the same commit** — the paths must stay byte-identical. **There is a THIRD copy, and it wins.** `.harmonik/agents/_skills/` is tracked and holds `agent-comms`, `beads-cli`, `crew-launch`, `harmonik-dispatch` and `boot`; `internal/agentmanifest` `resolveRef` checks `_skills/` BEFORE the type folder, so for any agent launched through a manifest that copy is the one that loads. Mirror all three or a manifest-launched agent keeps booting the old text with nothing reporting an error (hk-skills-have-three-copies-6sugz). The shipped set is: `agent-comms`, `beads-cli`, `captain`, `crew-launch`, `harmonik-dispatch`, `harmonik-lifecycle`, `keeper`, `major-issue-fanout`, `orchestrator-rules`, `watch` (authoritative list: `ls cmd/harmonik/assets/skills/`). Everything else under `.claude/skills/` is project-authored and safe to edit in place. Each embedded skill file carries the same warning in a `<!-- SOURCE OF TRUTH: … -->` banner under its frontmatter.
- **Cite symbols, not line numbers, in skills and docs.** File-plus-line references (`thresholds.go:72`) rot within days and have repeatedly shipped as stale guidance. Write `internal/keeper/thresholds.go` `HardCeilingAbsTokens` and let the reader grep.
- **Write prose in Simplified Technical English (ASD-STE100).** Short common words, active voice, one instruction per sentence, one name per thing. The **`ste-writing` skill** owns the full rule set, the scope and exemptions, the two modes, and the self-lint; it is forward-looking and does not authorize a retrofit pass over older files. STE governs the FORM of a sentence and the `no-jargon` skill governs the AUDIENCE (give what a thing is, not a bead ID or codename as its handle). Both apply at once.
- **Write guidance as principles, not laws.** An agent treats a rule as a law — it obeys the letter and does goofy things to satisfy it; a principle gives a direction to travel and preserves judgment. When writing skills, AGENTS content, or review criteria, prefer "lean toward X because Y" / "treat Z as a smell worth investigating" over "never X" / "always Y". Reserve hard mechanical constraints for genuine safety or irreversibility.
- **Codex and Pi implement; Claude is reserved for oversight.** Claude tokens are the constrained resource, so put implementer work on Codex and keep Claude for the roles where judgment is the product — captain, admiral, reviewer. **The selection is per bead, not per crew:** label the bead `harness:codex` (`br label add <id> -l harness:codex`), or set the per-queue / per-node / daemon default — the four tiers are in [`docs/codex-operator-guide.md`](docs/codex-operator-guide.md) §4. **Do NOT pass `--harness codex` to `harmonik start crew`, and do not set `harness: codex` in a mission handoff.** A crew orchestrator has no Codex substrate yet: `internal/crewrun` `BuildCrewLaunchSpec` refuses any crew harness but `claude` with "not yet supported", so `crew start` exits non-zero. Nothing selects a harness by default, so choosing Codex is deliberate; the reviewer node stays on Claude in code (`cmd/harmonik/substrate_select.go` `reviewerSubstrate`).
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:handler-pause`, `codename:claude-hook-bridge`). Kerf work `bead_filter` clauses must match the same form. Functional/topical labels (e.g. `queue`, `spec-drift`) remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Judgment calls

- **An abstraction has to name what it buys.** Name the thing and it is welcome: a second caller that exists today, a test seam the code has no other way to reach, a port a linter requires, or a boundary a spec draws. [PRINCIPLES.md](PRINCIPLES.md) asks for this class of change rather than treating it as scope creep — consumer-owned ports (§4), a constructor that can refuse an invalid value (§2), collapsing two paths that agree in shape (§5) — so "the bead body did not ask for it" is not on its own a reason to refuse one. The smell is an abstraction that names nothing: one caller, no test that needed it, no rule that demanded it, and a layer between them. Say what it buys in the commit body, and a reviewer can agree or disagree with a stated claim instead of guessing at intent.
- **Read your role's slice.** Read less than the load map gives you and you boot on a stale claim. Read more — a captain loading the whole knowledge base, say — and you spend the context the role needs for its actual job.

## Issue tracking → [`docs/beads-workflow.md`](docs/beads-workflow.md)

Issues are beads (`br`). The command surface, the workflow loop, commit-message validation, and the
UBS (Ultimate Bug Scanner) quick reference all live in
[`docs/beads-workflow.md`](docs/beads-workflow.md) — read it when you need them. The `beads-cli`
skill owns the read/write discipline. Only the traps you cannot look up stay here:

- **The ledger is machine-local and gitignored.** Beads do NOT travel between clones or reach CI, and
  `br sync --flush-only` stages nothing. Never "fix" this by tracking `.beads/` — a perpetually dirty
  tree trips the daemon's `implementer_escaped_worktree` detector and false-fails dispatched beads
  (refs `hk-yru`). Anything that must survive goes in a tracked doc or the commit message.
- **Priority** is stated intent first, then the ledger — canonical in the `orchestrator-rules`
  skill (`REFERENCE.md` §Priority). The one trap that is not lookup-able: pass `--limit 0`, because `br ready`
  returns 20 rows by default and sorts by `hybrid`, so a short default listing is not evidence of a
  short backlog.
- **Kerf plans work. It does not rank work.** `kerf map` shows which work owns a bead. Do not take
  an order from `kerf next` — its score comes from graph structure and never reads the `br` priority
  field, so a P0 and a P3 bead come back the same, and it reports empty for a work with no
  `bead_filter`.
- **Never run bare `bv`** — it opens an interactive TUI that holds the terminal until a human quits
  it, so an agent session stops there and nothing reports an error. Use `--robot-*` flags only, and
  only for graph metrics (`--robot-insights`, `--robot-graph`).
- **Commit with `git commit -F <file>`, never `-m`.** Non-trivial commits require `Reviewed-By:` and
  `Review-Verdict:` trailers (JSON, `schema_version: 1`); a `BLOCK` verdict is never committed.
  Validation is agent-enforced — git hooks are retired. `--no-verify` is forbidden.
  **The trailer must never claim a review that did not happen.** Quote the reviewer's verdict
  verbatim; do not author your own approval. But if no reviewer can be reached — no sub-agents, or
  the session is ending — **record that fact in the trailer and commit anyway**, with no verdict of
  `APPROVE`. Recording an absent review and approving your own work are different acts. Leaving
  finished work uncommitted is the worse failure: a commit labelled "not reviewed" is a state the
  next person can act on, while work stranded in a worktree is one `checkout` from gone.
  **Verify what you are about to ship, not what you edited:** a review reads the working tree and a
  commit ships the index, and nothing makes them agree. Confirm `git diff` is empty for the reviewed
  files before writing the trailer.
- **Run `/check` after committing.** Two targets and only two: `make fast` while you work, `make full`
  before anyone accepts the work. `make full` is the merge decision and it is what CI runs. It tests
  every package, it never scopes by what changed, and it never approves on a timeout, an OOM, a
  compile failure or a passing retry.

<!-- bv-agent-instructions-v2 -->
<!-- end-bv-agent-instructions -->

> **Maintainer note — the marker block above is machine-regenerable, and it is empty on purpose.**
> `br agents --update` rewrites everything between the `bv-agent-instructions-v2` markers from br's
> generic upstream template, so harmonik's guidance lives ABOVE the markers where that command
> cannot reach it. Diff the regenerated block before accepting it, and delete each claim upstream
> makes that is false here: that `.beads/` is tracked in git (it is gitignored and machine-local);
> that `git commit -m` is enough (it omits the required review trailers); that validation is wired
> via `lefthook.yml` (hooks are retired — it is agent-enforced through `/check`); that `kerf next`
> is the entry point for what to work on; and its unscoped bead-lifecycle advice. The predicate is
> **did you submit this bead to a queue?** — not "is it dispatched", which you cannot observe.
> **On a bead you submitted**, never claim with `br update --status=in_progress` (`queue submit`
> then refuses it: `bead_already_dispatched`, `-32015`, exit 1) and never `br close` it by hand
> (a close from inside a worktree leaks to the parent repo before any code lands). **A bead you
> worked by hand you close yourself**, because nothing else will: `harmonik reconcile` closes only
> beads whose commit carries a `Harmonik-Bead-ID:` trailer, and a hand commit never carries one.
