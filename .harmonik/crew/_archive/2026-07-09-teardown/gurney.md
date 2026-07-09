---
schema_version: 1
crew_name: gurney
queue: gurney-q
epic_id: eval-program
captain_name: captain
model: opus
---

# Crew mission — gurney — eval-program ws:problems (author the hard tasks)

> RE-TASKED 2026-07-03 (~17:30Z). Your prior bead (hk-z13jz, Pi base_url passthrough) is
> LANDED (commit c10c193b). New lane = the eval-program **problem set** (ws:problems), the
> parallelable P1 the admiral flagged. This runs IN PARALLEL with leto's close-out lane and
> is FILE-DISJOINT from it (you author task fixtures; leto touches reviewer/sandbox Go code).

## The lane — author the NEW HARD eval tasks
Read the spec FIRST: `plans/2026-07-03-eval-program/05-problem-set-and-tools.md` (the problem-set
design). Target set = **6 new HARD tasks + 8 existing = 14** curated coding tasks used to compare
models on time/tokens/quality.

Existing fixtures live under `evaltasks/` (e.g. `eval-expr-eval/`, `eval-interval-schedule/`,
`eval-lru-cache/`). MIRROR that structure for each new task: a self-contained task dir with the
prompt/spec + a committed test that defines the contract (exercise stub + held-out test), plus the
per-task metric fields the quality rubric needs (`expected_big_o`, `reference_line_budget` — see
WS3f hk-eval-prog-task-metric-fields-bpx4n and doc 02-quality-assessment.md).

For EACH of the 6 new tasks:
1. Author the task fixture dir under `evaltasks/` per doc 05 (hard = genuinely exercises a capable
   model: non-trivial algorithm/refactor/concurrency, unambiguous contract, deterministic test).
2. File a bead (`br create --type task --label codename:eval-program`) describing it, with the
   metric fields, so the run-matrix can dispatch it later.
3. Commit the fixture (this is authoring work, done directly — NOT through the daemon queue).

You MAY fan out sub-agents to draft candidate tasks in parallel, but YOU curate/verify each test
compiles + the contract is sound before committing. Post the task list + rationale to captain.

## Discipline
- FILE-DISJOINT from leto: do NOT touch `internal/daemon/`, reviewer/finalize code, or the sandbox
  pkg. Stay in `evaltasks/` + beads + docs. If you find yourself editing daemon Go, STOP and tell captain.
- Do NOT `br close` (daemon/captain owns terminal transitions) — though authored-task beads you create
  stay open for the run-matrix.
- Progress feed: `--topic status` to captain on boot, each task authored, the full set done, any blocker,
  ≤15-min idle tick.

## On boot
1. `harmonik comms join` + confirm identity = gurney.
2. Post a boot/re-task status to captain (`--topic status`).
3. Arm `harmonik comms recv --agent gurney --follow --json`.

## translations
ws:problems = "the eval problem set — the coding tasks models are graded on" · evaltasks/ = "where
task fixtures live" · the run-matrix = "later WS2 work that runs each task through each model" ·
close-out lane = "leto's parallel work fixing the reviewer bug + sandbox so the e2e runs clean".
