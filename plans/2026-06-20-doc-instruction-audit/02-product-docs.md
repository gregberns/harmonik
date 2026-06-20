# Product-Docs Audit (root-level user-facing docs)

Audit date: 2026-06-20. Scope: the "how to USE the harmonik tool" docs at repo root,
as distinct from AGENT-OPERATING instructions. Auditor focus per brief: are they
auto-generated or hand-maintained, accurate to the current CLI, do they overlap, and
should they live at repo root vs `docs/`.

## TL;DR

- **Hand-maintained, not auto-generated.** Only CLI-REFERENCE.md carries a "Generated from
  the binary" banner; the rest are prose written by hand. The cluster was produced in a
  single doc-authoring pass on **Jun 12–13** (a "productization docs" push tied to
  `harmonik init` / operating-manual work — see AGENT_INDEX "Productization P0 gate").
- **They are a coherent, well-cross-linked SET.** README → OVERVIEW → CONCEPTS → INSTALL →
  QUICKSTART → CLI-REFERENCE → CONFIGURATION → OPERATING-GUIDE all reference each other in a
  deliberate reading order. This is the strongest argument for treating them as one unit.
- **Mostly CURRENT and accurate.** Spot-checks pass. The one real staleness is
  **CLI-REFERENCE.md's top-level command menu is incomplete** (misses `captain`, `schedule`,
  `decisions`, `sleep`, `wake`, `goal-keeper`, `project-hash`, `sentinel`).
- **They do NOT belong at repo root mixed with agent docs.** Recommendation: move the whole
  set under `docs/product/` (or `docs/guide/`), leaving README.md at root as the entry point.
  This is the clean SEPARATION the brief asks for.

## Per-file findings

| File | Last touch | Purpose | Overlaps with | Staleness | Recommendation |
|---|---|---|---|---|---|
| README.md | 2026-06-12 | Root landing page: safety banner, what/why, doc map, prereqs, install, quickstart-in-miniature, key-concepts table, "for agent operators" section, milestones. | Heavy: duplicates QUICKSTART (steps 1–6 inline), INSTALL (prereq table), CONCEPTS (key-concepts table), OVERVIEW (what/why). | CURRENT. Accurate to CLI. Mentions OPERATING-GUIDE.md & AGENT_OPERATING_MANUAL.md — both EXIST. | **KEEP at root** (GitHub landing page must stay at root). TRIM the inline quickstart/install/concepts duplication down to links once the set moves to docs/. |
| OVERVIEW.md | 2026-06-12 | Plain-English "what it is / why / who for / mental model (ASCII diagram) / honest limits / where next." Marketing-adjacent narrative. | README "What is/Why" section; CONCEPTS (lighter). | CURRENT. "Some features still in flight… recurring-job scheduler and human-in-the-loop decision surface" is now slightly stale — both `schedule` and `decisions` ARE wired in code (main.go:550, :578). | MOVE to docs/product/. Reconcile the "in flight" line with CONCEPTS (which documents both as live). |
| CONCEPTS.md | 2026-06-13 | Vocabulary reference, one section per concept (bead, ledger, daemon, queue, worktree, review-loop, epic, crew, captain, comms, decisions, keeper, integration-branch, DOT, schedule). The deepest conceptual doc. | OVERVIEW (mental model); README key-concepts table (subset). | CURRENT and the most complete. Note: schedule section says "*Available after the next daemon rebuild*" — code IS present (schedule.go wired), so this caveat is now stale. | MOVE to docs/product/. Drop the "after next rebuild" caveat. |
| INSTALL.md | 2026-06-12 | Fresh-machine install: prereq table w/ verify commands, br/kerf install, go install, PATH, pre-flight checklist, "verified on 2026-06-12" matrix. | README prereqs/install (subset); QUICKSTART preamble. | CURRENT. Honest about unverified `cargo install` path. Go version stated as 1.25+ (README says 1.22+ — minor INCONSISTENCY between the two). | MOVE to docs/product/. Reconcile Go-version floor with README (1.22 vs 1.25). |
| QUICKSTART.md | 2026-06-12 | Shortest path: in-repo → start daemon → create bead → submit → subscribe → verify. Exit codes 17/5 called out. | README inline quickstart (near-duplicate of steps); INSTALL preamble. | CURRENT. Commands verified against code (`queue submit --beads`, `subscribe --types`, exit 17/5 all real). | MOVE to docs/product/. README's inline quickstart should defer here, not duplicate. |
| CLI-REFERENCE.md | 2026-06-13 | The one explicitly **generated-from-binary** doc (banner, 2026-06-12). Per-command sections + exit-code table. 850 lines. | None (it's the reference; others link INTO it). | **STALE in one spot.** Top-level "Subcommands listed in the menu" (line 58) lists: version, init, run, handler, queue, subscribe, comms, crew, reconcile, confirm-verdict, veto-verdict, graph, promote, release, supervise, keeper, beads-merge, smoke, tmux-start, hook-relay. **MISSING real verbs:** `captain`, `schedule`, `decisions`, `sleep`, `wake`, `goal-keeper`, `project-hash`, `sentinel`, `start`. It DOES add dedicated `decisions`/`schedule`/`comms`/`crew` sections later, so only `captain`/`sleep`/`wake`/`goal-keeper`/`project-hash`/`sentinel` are wholly undocumented. | MOVE to docs/product/. REGENERATE from current binary to pick up missing verbs (esp. `captain`). |
| CONFIGURATION.md | 2026-06-12 | Single config reference: precedence, config.yaml, branching.yaml, daemon flags, env vars (incl. credential guards & FLYWHEEL_* budget knobs). | README/QUICKSTART branch-protection snippets (subset); CONCEPTS integration-branch. | CURRENT. Flags cross-checked against code intent (--target-branch/--protect-branch/--forbid-default-main, --max-concurrent, --workflow-mode, --default-harness/--codex-binary). Credential-strip guards match the locked design. | MOVE to docs/product/. |
| BUILDING.md | 2026-05-06 | Contributor/build doc: make tools, three-tier check gauntlet (check-fast/check/check-full), declared-done ritual, commit-trailer conventions. | None (build-time, not run-time). Partly straddles agent territory (agent-review ritual, Reviewed-By trailers). | CURRENT but OLDEST (untouched in the Jun 12–13 pass — it predates the productization docs). | MOVE to docs/contributing/ or docs/product/. NOTE this is a DEVELOPER/CONTRIBUTOR doc, half-agent (it encodes the agent-review commit ritual) — borderline between the two doc families. |

## Auto-generated vs hand-maintained — verdict

- **CLI-REFERENCE.md** = semi-generated (banner: "Generated from the binary `harmonik <cmd> --help`
  on 2026-06-12; the binary is the source of truth"). It should be re-run, not hand-patched.
- **All others** = hand-written prose. No "DO NOT EDIT" / generation markers. They will drift
  with the CLI surface and need manual upkeep.

## Accuracy spot-checks (against cmd/harmonik source)

- `queue submit --beads`, `queue dry-run`, `queue status`, `subscribe --types` — all real.
- Exit codes 17 (daemon down), 5 (second-daemon pidfile collision) — match CLI-REFERENCE table.
- `harmonik init` / `init --doctor` / `init --force` — present (init_cmd.go).
- `decisions raise/wait/answer/list/withdraw` — `decisions` IS a real top-level verb
  (main.go:550, decisions.go) — CONCEPTS & CLI-REFERENCE describe it accurately.
- `schedule` — real verb (main.go:578, schedule.go). CONCEPTS' "available after next rebuild"
  and OVERVIEW's "still in flight" lines are now STALE — the code is in.
- Real top-level verbs (from main.go os.Args dispatch): version, init, run, handler, queue,
  subscribe, comms, crew, **captain**, reconcile, confirm-verdict, veto-verdict, graph, promote,
  release, supervise, keeper, **sleep**, **wake**, beads-merge, smoke, **goal-keeper**,
  **project-hash**, **sentinel**, decisions, schedule, tmux-start, hook-relay, start.
  Bolded ones are NOT in CLI-REFERENCE's menu line.

## Overlap analysis

Real overlap exists but is mostly **intentional layering** (README = teaser; OVERVIEW = narrative;
CONCEPTS = depth; QUICKSTART = do-it; INSTALL = set-up; CLI-REFERENCE/CONFIGURATION = reference).
The one avoidable duplication: **README inlines a full 6-step quickstart and a prereq table** that
exactly mirror QUICKSTART.md and INSTALL.md. When the set moves to docs/, README should shrink to
links + safety banner + 3-line elevator pitch, keeping a single source of truth per topic.

No file is DEAD. None should be ARCHIVED.

## Root vs docs/ — the key question

**These should NOT live at repo root** (except README.md). Reasons:

1. The brief's whole point is to SEPARATE product docs from agent-operating instructions. Today
   root mixes both: product docs (README, OVERVIEW, CONCEPTS, INSTALL, QUICKSTART, CLI-REFERENCE,
   CONFIGURATION, OPERATING-GUIDE, BUILDING) sit alongside agent docs (AGENTS.md/CLAUDE.md,
   AGENT_INDEX.md, AGENT_OPERATING_MANUAL.md, STATUS.md, TASKS.md, HANDOFF.md). A newcomer can't
   tell which set is for them.
2. **AGENT_INDEX.md barely references the product docs** — it links README only, and its whole map
   is the agent/knowledge-base tree under docs/. So the product set is already orphaned from the
   agent index; it is its own island that happens to share the root directory.
3. Convention: GitHub renders README.md at root regardless, so README stays. The other seven are
   pure documentation that conventionally live under docs/.

**Recommended layout:**

```
README.md                      # stays at root (GitHub landing); trimmed to links + pitch + safety
docs/product/OVERVIEW.md
docs/product/CONCEPTS.md
docs/product/INSTALL.md
docs/product/QUICKSTART.md
docs/product/CLI-REFERENCE.md  # regenerate from binary
docs/product/CONFIGURATION.md
docs/product/OPERATING-GUIDE.md
docs/contributing/BUILDING.md  # developer/build doc, half-agent — separate from end-user product docs
```

Update the relative cross-links on the move (they're all sibling-relative today, e.g.
`[CONCEPTS.md](CONCEPTS.md)`), and update README's doc-map links to `docs/product/...`.

## Separation map (so the cleanup doesn't conflate the two families)

**PRODUCT / USER docs (this audit):** README, OVERVIEW, CONCEPTS, INSTALL, QUICKSTART,
CLI-REFERENCE, CONFIGURATION, OPERATING-GUIDE. Audience = a human operator pointing the tool at
their repo. (+ BUILDING = contributor/build, adjacent family.)

**AGENT-OPERATING instructions (out of scope here — operator's focus, do not touch in this pass):**
AGENTS.md / CLAUDE.md, AGENT_INDEX.md, AGENT_OPERATING_MANUAL.md, STATUS.md, TASKS.md, HANDOFF.md,
docs/orchestrator-rules.md, docs/known-workarounds.md, the .claude/skills/* operating contracts,
specs/. Audience = the LLM agents (implementer/reviewer/captain/crew/orchestrator).

BUILDING.md is the one genuine straddler: it's a build doc (product-family) but encodes the
agent-review commit ritual (agent-family). Keep it product/contributor-side but flag the overlap.
