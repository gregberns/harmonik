# Harmonik — Agent Instructions

> `CLAUDE.md` is a symlink to this file (`AGENTS.md`). They are the same content — edits to `AGENTS.md` cover both.

> Cross-project working-style rules (keep moving, delegate, plain English, compact, review gate) live in `~/.claude/CLAUDE.md`. This file adds harmonik-specific bits on top of those.

<!-- BEGIN harmonik:managed agents-router -->

## Precedence

Standing behavioral rules: the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`) is canonical. On conflict: **orchestrator-rules skill > AGENTS.md prose > per-domain skills own their detail**. Operational state lives in `.harmonik/context/` (captain tiers) and `HANDOFF.md` (this session) — **never in this file**.

AGENTS.md is a ROUTER. What belongs here is a pointer to the contract that owns a thing, plus the one line of judgment a reader needs to use the pointer. Judge this file by that test, not by a line count: if a paragraph could go and no reader would lose something the pointed-at file does not carry, move it out. Length is the symptom, so when this file grows, find the thing that grew and give it to a skill or a doc. (A hard 120-line cap used to be the test. This file has sat just over it for months, so every session-boundary review raised the same flag against the source of truth, and agents learned to read the check as decorative. `.claude/skills/agent-config-reviewer/SKILL.md` still checks the number.)

## Per-role load map

Each role loads only its slice. **This map is the reading order.** Where any other file states one, it defers here. Each role's skill stays authoritative for that role's own steps; this is the map across roles. The slices genuinely differ, on purpose: a captain does not boot-read `AGENT_INDEX.md` or `STATUS.md`, and an implementer-orchestrator has three steps where the captain has four.

- **Captain — cold boot** (see `.claude/skills/captain/STARTUP.md`):
  1. Step 0 — identity + CWD guard.
  2. Step 0a — tier-3 `.harmonik/context/project.yaml` (phase, locked decisions, guardrails).
  3. Step 0b — tier-2 `.harmonik/context/captain-lanes.md` (lanes + epics-in-progress + parked + dated operator directives).
  4. Step 1 — `captain/SKILL.md` + the **`orchestrator-rules` skill** (standing rules) + tier-1 `HANDOFF.md` (a claim, not ground truth).
  5. Step 2 — boot digest = ground-truth; overrides every claim above. Steps 3–6 reconcile / plan / staff / arm watchers.
  - Does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, product/`docs/` knowledge base, full skill bodies. `ROADMAP.md` only on cold boot / milestone.
- **Captain — keeper-restart resume (LEAN):** re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive.
- **Crew — minimal load** (see `.claude/skills/crew-launch/SKILL.md`): its mission file (`.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch` + `PRINCIPLES.md` (crews write the tests, so they need the standard the tests are held to). Does **NOT** load fleet-level state (ROADMAP, captain-lanes, project.yaml, orchestrator standing-rules, STATUS, HANDOFF, knowledge base) — scoped to ONE epic + ONE queue.
- **Assessor** (see [`roles/assessor/operating.md`](roles/assessor/operating.md) — that file is the contract): `roles/assessor/soul.md` + `operating.md` + `good-enough-principles.md` + `personality.md`, then the gate definition at `plans/2026-07-27-delete-and-rewrite/ASSESSOR-GATE.md` and the severity rubric at `plans/2026-07-06-quality-system/07-assessor-severity-framework.md`. Writes its output to a dated folder under `assessments/`, created before the gate runs, and owns the live failure corpus at [`test/exploratory/cases/`](test/exploratory/cases/) — the cases it re-runs every gate and extends before it terminates. **The assessor's distinctive job is to spin the process up and run real work through it**; a passing suite is not evidence the system does what we want, and other agents own the unit tests. The role is a document, not a process — no command starts one. Wiring only (harness, injected context) lives in `.harmonik/agents/assessor/manifest.yaml`.
- **Implementer-orchestrator (main `/session-resume`, non-captain):** `AGENT_INDEX → STATUS → HANDOFF` + the **`orchestrator-rules` skill** (standing rules) + `harmonik-dispatch`. **Three steps, not the captain's four:** `captain-lanes.md` is captain-tier — its own tier header says "LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers". Putting it back here is the recurring wrong fix.
- **Any session with no role:** `PRINCIPLES.md` → the charter → `AGENT_INDEX.md` → `STATUS.md` → `HANDOFF.md`. §Start here describes the same order in prose.

## Start here

Every role starts with the same two files. Read [PRINCIPLES.md](PRINCIPLES.md) — the nine engineering principles this codebase is built to, and the standard any new or rewritten code is held to. Then, while the delete-and-rewrite program is active, [`plans/2026-07-27-delete-and-rewrite/CHARTER.md`](plans/2026-07-27-delete-and-rewrite/CHARTER.md) — what the program is, what the core subsystem set is, and what "done" means. It changes rarely, it outranks any handoff on intent, and it is short enough to reread before writing code rather than only at boot.

What you read after those two depends on your role, and the load map above is the statement of it. A session with no role reads [AGENT_INDEX.md](AGENT_INDEX.md) — the curated map of the knowledge base — then [STATUS.md](STATUS.md) for phase and locked decisions, then `HANDOFF.md` for this-session state (untracked and gitignored, so it is present on this machine and absent from a fresh clone). A **captain** boot-reads neither `AGENT_INDEX.md` nor `STATUS.md`, and does read `.harmonik/context/captain-lanes.md`, which is captain-tier and which crews and implementer-orchestrators skip.

**Launching a captain or crew?** Use the native umbrella verb — `harmonik start captain` or `harmonik start crew <name>` (keeper auto-armed; NO env var, NO script path — `--project` defaults to cwd). The old `~/.claude/captain-tools/captain-launch.sh` + `HK_PROJECT` env var are RETIRED in favor of `harmonik start captain`. Positional-XOR-flags rule (D2): the simple form is a bare name only (`start crew paul`); the moment any `--flag` appears the name must move to `--name` (mixing a bare name with flags is a hard error). `harmonik captain` / `harmonik crew start <name>` remain as back-compat aliases.

**Launching an oversight session (commodore, admiral)?** ALWAYS use `harmonik start commodore` / `harmonik start admiral` — never a bare `claude --remote-control` or `harmonik agent brief` in a hand-rolled tmux session. Those launch paths write neither the crew registry record nor the `crew-<name>`-prefixed tmux session that the daemon's boot-time orphan sweep (`RunOrphanSweep`) checks for, so the session is reaped as an orphan at the next daemon boot / supervisor revive (hk-zeo5y). `harmonik start commodore` / `admiral` always route through the same registry-protecting RPC as `harmonik crew start <name>` — the identity is the role name (no positional), so protection is automatic, not a step to remember.

**Booting as a captain or crew?** These skills are **project-local under the repo** — read them at `/Users/gb/github/harmonik/.claude/skills/…`, NOT the global `~/.claude/skills/` (no captain/crew skill exists there). Captain: read `.claude/skills/captain/STARTUP.md` FIRST, then `SKILL.md` in that dir. Crew: read `.claude/skills/crew-launch/SKILL.md`. See also `.claude/skills/keeper` (per-session context-watcher) and `.claude/skills/harmonik-lifecycle` (supervise / promote / reconcile / init).

## Standing rules → the `orchestrator-rules` skill

Dispatch discipline (the daily loop, the HARD-RULE exceptions), priority (stated intent first, then the ledger), bead lifecycle (daemon owns terminal transitions; never pre-set in_progress), the review gate, autonomy/flow boundaries, and the major-issue fan-out trigger: all canonical in the **`orchestrator-rules` skill** (`.claude/skills/orchestrator-rules/SKILL.md`). It points to the detail-owner skills; it does not duplicate them.

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

- **Work lands on the integration branch, never on `main`.** The rule is owned by
  [`docs/foundation/project-level/build-practices.md`](docs/foundation/project-level/build-practices.md)
  §"Branch model — land on the integration branch". Read it there. It replaces the retired
  direct-to-main model.
- **Specs live in `specs/`** at the repo root. These are normative: the spec is always right, and code is expected to match it. Spec drafts produced by kerf are copied here on `kerf finalize`.
- **Kerf process artifacts** (problem space, research, design, drafts, tasks, reviews) live in the repo at **`.kerf/works/{codename}/`**. This project has already been localized (`.kerf/config.yaml` sets `storage: local`), so the repo — not the global bench — is the authoritative working directory. The bench path `~/.kerf/projects/gregberns-harmonik/` still resolves: it is a **symlink** to `.kerf/works/`, so either spelling reaches the same files. Write pass artifacts to the path `kerf new` / `kerf show` prints, or you will silently produce orphan files. **Do NOT run `kerf localize` to tidy up misplaced files** — it is not a file reconciler, it is the one-time bench→repo storage migration, and it has already been run here. There is no automated command for a misplaced artifact: move it into the work's directory by hand.
- **Knowledge base docs** (`docs/`) capture problems, goals, concepts, components, subsystems, ideas, and the collaboration log. These are inputs to kerf works; they are not themselves normative specs.
- **Role instructions live in `roles/`** at the repo root — `assessor`, `captain`, `admiral`, `lane` so far (`lane` is shared by every hand-run delivery lane). **A role is a document, not a process: nothing starts one.** An agent that is already running becomes the assessor by reading `roles/assessor/`. There is no launch command, and assuming there was one has cost real time more than once. Each role's `operating.md` marks the steps that need a live daemon with **[FLEET]**, so the same file works with harmonik running and with nothing running. `.harmonik/agents/<role>/manifest.yaml` names its folder with a `role:` key and keeps only the harmonik-side wiring; a manifest with no `role:` key reads `soul.md` and `operating.md` from its own folder as before (`crew`, `commodore`, `watch` still do). **There is one copy of a role's instructions — the one in `roles/`.** Do not add a "see roles/" stub under `.harmonik/agents/`; a stub is still a file that drifts. See [`roles/README.md`](roles/README.md).
- **The assessor's output lives in `assessments/`**, one dated folder per assessment (`YYYY-MM-DD-HHMM-<slug>`), in the same spirit as `plans/`. It is created BEFORE the gate runs and written as the work happens — a record assembled afterwards agrees with the verdict because the same mind produced both. Four files, copied from `assessments/_TEMPLATE/`: the mission, the evidence, the findings, the verdict. See [`assessments/README.md`](assessments/README.md).
- **Ten architectural decisions** are locked in as of 2026-04-19. See [STATUS.md](STATUS.md#10-locked-decisions-2026-04-19) — that section is a stub, and it names the one commit that carries the decision text. **Reopening one is the operator's call, and evidence is what earns the conversation.** They were locked because re-litigating them cost more than living with them. Bring evidence that a decision is now wrong, say what changed, and put it to the operator. That the current task would be easier the other way is not evidence, and finding good evidence is not the same as having the authority to act on it. This is the only statement of that gate in this file.
- **`.claude/skills/` is GENERATED OUTPUT for every skill that ships in the binary.** The source is `cmd/harmonik/assets/skills/<name>/` (pulled in by `//go:embed` in `cmd/harmonik/init_skill_assets.go`); `harmonik sync-assets` classifies `.claude/skills/*` as *Managed* and overwrites it from the embed, and **there is no reverse sync**. Editing only the `.claude/skills/` copy silently creates embed drift, resurfaces as a `.harmonik-new` conflict on the next sync, and is eventually reverted. **To change a shipped skill: edit the `cmd/harmonik/assets/skills/` copy, then mirror it byte-for-byte into `.claude/skills/` in the same commit** — the two paths must stay byte-identical. The shipped set is: `agent-comms`, `beads-cli`, `captain`, `crew-launch`, `harmonik-dispatch`, `harmonik-lifecycle`, `keeper`, `major-issue-fanout`, `orchestrator-rules`, `watch` (authoritative list: `ls cmd/harmonik/assets/skills/`). Everything else under `.claude/skills/` is project-authored and safe to edit in place. Each embedded skill file carries the same warning in a `<!-- SOURCE OF TRUTH: … -->` banner under its frontmatter.
- **Cite symbols, not line numbers, in skills and docs.** File-plus-line references (`thresholds.go:72`) rot within days and have repeatedly shipped as stale guidance. Write `internal/keeper/thresholds.go` `HardCeilingAbsTokens` and let the reader grep.
- **Write prose in Simplified Technical English (ASD-STE100).** Short common words, active voice, one instruction per sentence, one name per thing. The **`ste-writing` skill** (`.claude/skills/ste-writing/SKILL.md`) owns the rules — the full set, what is in scope and what is exempt, the two modes, and the self-lint. It is forward-looking and does not authorize a retrofit pass over files that predate it. STE governs the FORM of a sentence. The `no-jargon` skill governs the AUDIENCE: give what a thing is, not a bead ID or a codename as its handle. Both apply at once.
- **Write guidance as principles, not laws.** An agent treats a rule as a law — it obeys the letter and does goofy things to satisfy it; a principle gives a direction to travel and preserves judgment. When writing skills, AGENTS content, or review criteria, prefer "lean toward X because Y" / "treat Z as a smell worth investigating" over "never X" / "always Y". Reserve hard mechanical constraints for genuine safety or irreversibility.
- **Codex and Pi implement; Claude is reserved for oversight.** Claude tokens are the constrained resource and the operator is on a Codex subscription, so staff implementer crews on the Codex or Pi harness (`harmonik start crew --name <name> --harness codex` — note the D2 positional-XOR-flags rule above: with any flag present the name must move to `--name`) and keep Claude for the roles where judgment is the product — captain, admiral, reviewer. Nothing selects a harness by default, so choosing Codex is a deliberate act; the reviewer node stays on Claude in code (`cmd/harmonik/substrate_select.go` `reviewerSubstrate`). Operator surface: [`docs/codex-operator-guide.md`](docs/codex-operator-guide.md).
- **Bead label convention for kerf work codenames:** use the `codename:<name>` prefix (e.g. `codename:handler-pause`, `codename:claude-hook-bridge`). Kerf work `bead_filter` clauses must match the same form. Functional/topical labels (e.g. `queue`, `spec-drift`) remain bare — only labels whose sole purpose is to identify a kerf work codename get the prefix.

## Judgment calls

- **An abstraction has to name what it buys.** Name the thing and the abstraction is welcome: a second caller that exists today, a test seam the code has no other way to reach, a port a linter requires (neither `internal/runloop` nor `internal/runexec` may touch the wall clock, and `internal/runloop` meets that with an injected clock port, so the port there is not optional — `internal/runexec` meets it by reading no clock at all, so do not give that one a port), or a boundary a spec draws. [PRINCIPLES.md](PRINCIPLES.md) asks for this class of change rather than treating it as scope creep — consumer-owned ports (§4), a constructor that can refuse an invalid value (§2), collapsing two paths that agree in shape (§5). So "the bead body did not ask for it" is not on its own a reason to refuse one. The smell is an abstraction that names nothing: one caller, no test that needed it, no rule that demanded it, and a layer between them. Say what it buys in the commit body, and a reviewer can agree or disagree with a stated claim instead of guessing at intent.
- **Read your role's slice.** The per-role load map above is the reading order, and the slices differ on purpose. Read less than yours and you boot on a stale claim. Read more than yours — a captain loading the whole knowledge base, say — and you spend on reading the context the role needs for its actual job.

## Issue tracking → [`docs/beads-workflow.md`](docs/beads-workflow.md)

Issues are beads (`br`). The command surface, the workflow loop, commit-message validation, and the
UBS (Ultimate Bug Scanner) quick reference all live in
[`docs/beads-workflow.md`](docs/beads-workflow.md) — read it when you need them. The `beads-cli`
skill owns the read/write discipline. Only the traps you cannot look up stay here:

- **The ledger is machine-local and gitignored.** Beads do NOT travel between clones or reach CI, and
  `br sync --flush-only` stages nothing. Never "fix" this by tracking `.beads/` — a perpetually dirty
  tree trips the daemon's `implementer_escaped_worktree` detector and false-fails dispatched beads
  (refs `hk-yru`). Anything that must survive goes in a tracked doc or the commit message.
- **Priority comes from stated intent first, then from the ledger.** Work the named initiatives of
  the operator and the admiral first. They live in the active plan's order, in the dated directives
  in `captain-lanes.md`, and in the direction-log RETURN-PATH. Below that line, order the unclaimed
  backlog with `br ready --sort priority --limit 0`. Scope it to one lane with `--parent <epic_id>`,
  and use `br ready --sort oldest` to surface work that is starving. Pass `--limit 0`: `br ready`
  returns 20 rows by default and sorts by `hybrid`, so a short default listing is not evidence of a
  short backlog.
- **Kerf plans work. It does not rank work.** Use `kerf map` to see which work owns a bead and what
  context it carries. Do not take an order from `kerf next` — its score comes from graph structure
  and never reads the `br` priority field, so a P0 bead and a P3 bead come back the same, and it
  reports empty for a work that has no `bead_filter`. Ranking what matters is judgment, and a graph
  metric cannot do it for you.
- **Never run bare `bv`** — it opens an interactive TUI that holds the terminal until a human quits
  it, so an agent session stops there and nothing reports an error. Use `--robot-*` flags only, and
  only for graph metrics (`--robot-insights`, `--robot-graph`).
- **Commit with `git commit -F <file>`, never `-m`.** Non-trivial commits require `Reviewed-By:` and
  `Review-Verdict:` trailers (JSON, `schema_version: 1`); a `BLOCK` verdict is never committed.
  Validation is agent-enforced — git hooks are retired. `--no-verify` is forbidden.
  **What the trailer is for: it must never claim a review that did not happen.** Quote the reviewer's
  verdict verbatim and do not author your own approval. But if no reviewer can be reached — the
  session has no sub-agents, or it is ending — then **record that fact in the trailer and commit
  anyway**. Recording an absent review and approving your own work are different acts, and they have
  failed in the same direction before: such a trailer states that no reviewer was reached and carries
  **no** verdict of `APPROVE`. Leaving finished work uncommitted is the worse failure — a commit
  labelled "not reviewed" is a state the next person can act on, while work stranded in a worktree is
  one `checkout` from gone. Never treat "I could not get a review" as a reason to stop.
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
> generic upstream template. Harmonik's issue-tracking guidance therefore lives ABOVE the markers,
> where that command cannot reach it. It used to live inside them, and one `br agents --update` would
> have silently deleted the only statement in this repo of the bare-`bv` rule and of where priority
> comes from. After any `br agents --update`, read the regenerated block and delete each claim
> upstream makes that is false here: (1) the bead ledger is gitignored and machine-local, not "stored
> in `.beads/` and tracked in git"; (2) `git commit -m "..."` alone omits this repo's required review
> trailers; (3) commit-message validation is agent-enforced through the `/check` flow — git hooks
> (lefthook) are retired, not "wired via `lefthook.yml`"; (4) an agent never claims a bead with
> `br update --status=in_progress` and never closes one with `br close` — the daemon owns terminal
> transitions, and a bead pre-set to `in_progress` stops being dispatchable with nothing reporting an
> error; (5) `kerf next` is not the entry point for what to work on. Diff the block before you accept
> the result.
