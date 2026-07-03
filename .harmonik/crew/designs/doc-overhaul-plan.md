# Documentation Audit & Overhaul — Plan (2026-06-13)

Authoritative plan from a 4-agent read-only audit. Operator directive: overhaul docs to
reflect harmonik's many new features, for **two audiences** — HUMANS (what the product is +
how to use it) and AGENTS (how to use + operate it) — including CLI help menus, README, and
"everything." Operator is OFFLINE; captain owns all decisions.

## Audit verdict
harmonik is **over-documented for agents, thin for humans**. The agent surface (9 skills,
AGENTS.md, AGENT_OPERATING_MANUAL.md, current specs, CLI `--help`) is fresh and dense; the
only human product doc is a young README. Two systemic problems: (1) the conceptual KB
(`docs/problems|goals|concepts|ideas`, `docs/components/`) is **frozen since April** and never
absorbed the comms-bus / Captain redesign; (2) a large mass of **historical artifacts**
(`docs/reviews/`, `docs/decompose-to-tasks/`, dogfood-smoke traces, TASKS.md/ROADMAP.md, the
RETIRED-but-present `AGENT_COMMS.md`) is **not quarantined**, so a new reader can't tell live
docs from the audit trail. **Much raw material already exists** — the human overhaul is mostly
*extraction + re-voicing*, not net-new research.

## TRACK A — AGENT-facing (epic hk-… "docs-agent") — correctness FIRST
Priority order — P0 are factual errors agents act on and break:
1. **P0 fix `--lane` → `--queue`** in AGENT_OPERATING_MANUAL.md (§2 + Quick-Ref table; 2 spots). `--lane` does not exist.
2. **P0 reconcile orchestrator-rules.md** — it teaches the RETIRED `harmonik run --beads` as the canonical dispatcher (lines ~15, ~71), contradicting AGENTS.md + harmonik-dispatch skill (persistent daemon + `queue submit`). Harmonize to `queue submit`; an agent following it hits exit-5 pidfile collisions.
3. **P0 fix `--beads` shorthand status** — CLI `queue --help` + harmonik-dispatch present `queue submit --beads hk-a,hk-b` as shipped; AGENTS.md §"Submitting work" still calls it in-flight gap hk-m9a7g. Verify hk-m9a7g landed; drop the stale gap note; make README/AGENTS use the `--beads` form (not hand-authored JSON).
4. **CLI help fixes** (cmd/harmonik help text): add `queue set-concurrency <n>` to `queue --help` VERBS; add `subscribe`, `comms`, `crew` to the top-level `harmonik --help` SUBCOMMANDS (all real, agent-critical, currently omitted).
5. **New skill: `keeper`** — enable/doctor/set-dispatching, warn/act thresholds, crew-restart re-hydration. #1 live operational pain (known-workarounds "keeper not deployed for crews").
6. **Coverage for `supervise` / `promote` / `reconcile` / `init`** — a skill or operating-doc section (only `--help` today).
7. **Discoverability**: AGENTS.md + AGENT_OPERATING_MANUAL.md mention captain/crew ZERO times — add "booting as captain/crew? load `.claude/skills/{captain,crew-launch}`" pointers from the entry docs + AGENT_INDEX.
8. **Skill gap-fills**: agent-comms skill add `--wake` + mention `harmonik subscribe`; crew-launch add idle-crew-wake protocol + keeper-rehydration + "don't self-quit on keeper warn"; reconcile contradictory keeper-deployed state (STARTUP assumes armed 25/30 vs captain-restart says ships without gauge).
9. **Cleanup/quarantine** (careful, last): move clearly-historical artifacts under a `docs/historical/` (or ARCHIVE note) — `reviews/`, `decompose-to-tasks/`, dogfood-smoke traces, TASKS.md/ROADMAP.md; DELETE or clearly-mark the retired `AGENT_COMMS.md`; reconcile/annotate `docs/components/` + the April conceptual KB against the comms-bus/Captain reality (annotate "as-of April, see X for current" rather than rewrite). Do NOT delete the audit trail — quarantine + label.

## TRACK B — HUMAN-facing (epic hk-… "docs-human") — new doc set
Extraction + re-voicing from existing agent docs/specs into plain human English. Target set:
- **README.md** — keep (strong); trim the JSON-submit example to the `--beads` form.
- **OVERVIEW.md** (new) — what/why/who-it's-for, honest maturity & limits (single-machine, Claude-Code-only, auto-pushes main by default), mental model + ONE diagram.
- **INSTALL.md** (new) — prerequisites w/ version checks, VERIFIED `br`/`kerf` install, pre-flight checklist.
- **QUICKSTART.md** (new) — "run your first bead" using the `--beads` form (NOT hand-authored JSON).
- **CONCEPTS.md** (new, highest human priority) — prose per concept: daemon, bead, worktree/merge, review-loop, crews, named queues, comms bus, keeper, DOT workflows. What/why/how-it-fits, plain English.
- **CLI-REFERENCE.md** (new) — every subcommand/flag/exit-code, generated from the CORRECTED `--help` (coordinate w/ Track A #4 so they agree).
- **OPERATING-GUIDE.md** (new) — day-2 runbook (deploy new binary, stop/restart daemon, drain/cancel a wedged queue, change concurrency live, start/stop crews, enable keeper, operator pause) + a human-facing troubleshooting / exit-code taxonomy (5=pidfile, 17=no daemon, …) absorbing AGENT_OPERATING_MANUAL §6 gotchas + known-workarounds.
- **CONFIGURATION.md** (new) — one table: every `.harmonik/config.yaml` + `branching.yaml` key + daemon flag + env var.

## Cross-track consistency rules
- CLI-REFERENCE.md (B) must be generated from the help text AFTER Track A #4 lands — Track B coordinates with Track A on the CLI-help fixes.
- Human CONCEPTS.md extracts from the same source as the agent skills but RE-VOICES for humans — no req-IDs/ZFC/jargon.
- New human docs are NEW files (docs/ or root) → low collision with Track A's edits to skills/AGENTS/cmd. Coordinate the PUSH window via captain (4 crews now land to main).
- The scheduler (leto/hk-0es) and HITL (paul/hk-rom) are IN FLIGHT — when they land, their concepts/CLI need doc coverage; leave placeholders + a TODO, don't block.

## Execution
Two doc crews, each fans out ≤2 worktree writing-agents, independent review per doc, cherry-pick
in a daemon lull, coordinate push windows with all crews via comms. Track A correctness fixes
(P0 #1-3) land FIRST — they actively mislead agents today.

## Execution reconciliation (hk-8us, 2026-06-13)
Item 9 listed `docs/reviews/` and `docs/decompose-to-tasks/` as MOVE candidates, but execution
**kept both in place** (banner-archived, NOT relocated to `docs/historical/`) as referenced audit
trail: `docs/reviews/` is linked from AGENT_INDEX.md, and `docs/decompose-to-tasks/` is cited by
the live normative spec `specs/beads-integration.md` — moving either would orphan those references.
Only the dogfood/smoke traces were physically moved into `docs/historical/`. See each dir's
`README.md` banner and `docs/historical/INDEX.md` §"Intentionally kept in place".
