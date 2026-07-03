# Problem Space — eval-harness

**Source of truth:** `plans/2026-07-02-eval-harness/DESIGN.md` (authoritative; this work is the
build-out of that design). Seeded from DESIGN.md, not a fresh conversation.

## Goal / motivation

Run a curated series of coding tasks through harmonik against a chosen model
(DGX-hosted *ornith* via the Pi harness, a Claude baseline, or any Pi provider/model) and grade
each solution **deterministically** (the task's own unit test / grep check → pass/fail) plus capture
**wall-time** from the event log, so models can be compared on **quality + speed** and, later, a
**task→model router** can be trained. The per-run record schema *is* the router's training signal.

## Who benefits

The operator choosing which model to route which class of work to (cost/speed/quality tradeoff).
Tonight's immediate consumer: an *ornith* capability probe using the 8 curated `codename:eval` task
beads already created.

## In scope

- A **results collector** (the one genuinely-new Go/script piece): read `events.jsonl` by `run_id`,
  join run wall-time + `harness_selected` + the deterministic check result, emit one flat JSON
  record per run to `eval-results.jsonl` (schema in DESIGN §1.3).
- **`eval-bead.dot`** — a new workflow file at the project dir (resolved by the `dot:eval-bead`
  label): topology `start → implement → grade(shell) → judge(non-gating) → record-pass/record-fail`,
  **no fix-loop** (measures one-shot capability). Uses only existing node/edge grammar — no daemon
  change.
- **Config / routing** for model-under-test selection: per-bead labels `workflow:dot` +
  `dot:eval-bead` + `harness:pi|claude-code`; `harnesses.pi.{provider,model,api_key_env}` points Pi
  at the model-under-test. Claude baseline needs no config.
- **Aggregation report** — `jq` group-by `(model, difficulty)` → pass-rate, median wall_time_s, mean
  judge_grade. Shell one-liner acceptable for v1.
- **Acceptance** — run the 8 curated task beads through `eval-bead.dot` on a baseline model, produce
  an `eval-results.jsonl` comparison file.

## Out of scope

- Any daemon hot-path change. The harness rides existing seams (moderesolve / harnessresolve /
  shell-node exit-code→Outcome contract).
- The `base_url` / ornith-endpoint plumbing into Pi (DESIGN O2) — that is a *separate* Pi-config
  verification gating tonight's ornith run; this build targets a baseline model so it is not blocked
  on it. The 8 task beads deliberately carry NO `harness:pi` label yet.
- An *assisted* (capped fix-loop) eval DOT — a distinct future metric, kept as a separate DOT.
- A learned router — this build only produces the training rows.

## Constraints

- **Reuse existing seams only** for DOT + routing + config; the collector is the sole new logic and
  is read-only over the log (post-run, off the hot path).
- Deterministic acceptance: each task's `grade` node reuses the exact exit-code→Outcome contract
  standard-bead relies on (exit 0 = SUCCESS, ≠0 = FAIL/deterministic).
- Tasks are self-contained (new file + own committed test), safe to re-run against any harness.

## Success criteria (concrete, verifiable)

1. A bead labelled `workflow:dot dot:eval-bead harness:claude-code` runs end-to-end through
   `eval-bead.dot` and reaches a terminal `record-pass`/`record-fail` node.
2. The collector, run over `events.jsonl`, emits one well-formed record per eval run with
   `pass`, `wall_time_s`, `model`, `harness`, `task_id`, `difficulty` populated.
3. The aggregation report groups the records by `(model, difficulty)` and prints pass-rate + median
   wall_time_s.
4. Acceptance: the 8 curated `codename:eval` beads run through `eval-bead.dot` on a baseline model
   and yield an `eval-results.jsonl` with 8 records.

## Adopted leans on open decisions (DESIGN §1.6)

- **O3 → pure post-run collector.** No shell-node file writes; the collector reads everything from
  `events.jsonl` + the check's outcome payload. Avoids concurrent-file-write footguns across
  parallel eval runs, keeps the DOT clean.
- **O1 → tamper-proof test = committed-with-task, restored-before-run.** The task's `*_test.go` is
  committed alongside the task; the `grade` node restores the test from a read-only path before
  running it, so the model-under-test cannot delete or edit it to game the check.
- **O4 → fixed judge model `claude-opus-4-8`, non-gating**, held constant across models-under-test
  so the quality axis is comparable; judge NEVER routes back.
